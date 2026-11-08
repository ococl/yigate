package proxy

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"
)

const (
	ActionDirect = "direct"
	ActionProxy  = "proxy"
	ActionAuto   = "auto"
	ActionDeny   = "deny"
	ActionAllow  = "allow"
)

const (
	defaultAutoMode              = "direct_first"
	defaultDirectTimeout         = 1500 * time.Millisecond
	defaultCacheTTL              = time.Hour
	defaultGeoSiteUpdateURL      = "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
	defaultGeoSiteUpdateInterval = 24 * time.Hour
)

// Duration supports Go duration strings in YAML, with bare numbers treated as seconds.
type Duration time.Duration

// MarshalYAML 输出人类可读的 duration 字符串（如 "24h"），与 UnmarshalYAML 兼容，
// 确保默认配置生成后可被原样反序列化（避免被当作纳秒整数误读）。
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || strings.TrimSpace(value.Value) == "" {
		return nil
	}
	if value.Tag == "!!int" {
		seconds, err := strconv.Atoi(value.Value)
		if err != nil {
			return err
		}
		*d = Duration(time.Duration(seconds) * time.Second)
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err == nil {
		*d = Duration(parsed)
		return nil
	}
	seconds, intErr := strconv.Atoi(value.Value)
	if intErr == nil {
		*d = Duration(time.Duration(seconds) * time.Second)
		return nil
	}
	return err
}

func (d Duration) value(def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return time.Duration(d)
}

// RoutingConfig defines Clash-like routing rules.
type RoutingConfig struct {
	Final           string        `yaml:"final"`
	GeoSite         GeoSiteConfig `yaml:"geosite"`
	Auto            AutoConfig    `yaml:"auto"`
	Socks5HRedirect Socks5HConfig `yaml:"socks5h_redirect"`
	Rules           []RouteRule   `yaml:"rules"`
	// Policy 由服务层在加载配置后注入，不通过 YAML 反序列化。
	Policy *DestinationPolicy `yaml:"-"`
}

type GeoSiteConfig struct {
	// Path 为空时由系统解析为配置文件同目录下的 geosite.dat，管理界面不再暴露该字段。
	Path           string   `yaml:"path,omitempty"`
	AutoUpdate     bool     `yaml:"auto_update"`
	UpdateURL      string   `yaml:"update_url"`
	UpdateInterval Duration `yaml:"update_interval"`
	// UseUpstream 为 true 时优先通过已配置且可用的上游代理下载数据，
	// 全部上游失败后再回退直连；网络受限环境下可显式关闭。
	UseUpstream bool `yaml:"use_upstream"`
	// UpdateToken 非空时以 Bearer 令牌向下游/上游请求 GeoSite 数据，
	// 用于从启用了只读同步的 YiGate 节点拉取数据。
	UpdateToken string `yaml:"update_token"`
	// SyncEnabled 为 true 时，其他节点可使用 SyncToken 从本节点只读同步 GeoSite 数据。
	SyncEnabled bool   `yaml:"sync_enabled"`
	SyncToken   string `yaml:"sync_token"`
}

type Socks5HConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Upstreams []string `yaml:"upstreams"`
}

type AutoConfig struct {
	Mode          string   `yaml:"mode"`
	DirectTimeout Duration `yaml:"direct_timeout"`
	CacheTTL      Duration `yaml:"cache_ttl"`
}

type RouteRule struct {
	Type      string   `yaml:"type"`
	Values    []string `yaml:"values"`
	Action    string   `yaml:"action"`
	Upstreams []string `yaml:"upstreams"`
}

type RouteDecision struct {
	Action    string
	Upstreams []string
}

type Router struct {
	final    RouteDecision
	rules    []compiledRouteRule
	geoSite  *GeoSiteMatcher
	auto     autoRuntimeConfig
	socks5h  Socks5HConfig
	cache    *RouteCache
	sfGroup  singleflight.Group
	policy   *DestinationPolicy
	resolver *DNSResolver

	// ipPrecedence 控制 DNS 解析后 IPv4/IPv6 的优先级。
	ipPrecedence string
}

type autoRuntimeConfig struct {
	Mode          string
	DirectTimeout time.Duration
	CacheTTL      time.Duration
}

type compiledRouteRule struct {
	typ      string
	values   []string
	regexps  []*regexp.Regexp
	cidrs    []*net.IPNet
	decision RouteDecision
}

func ResolveRoutingPaths(cfg *RoutingConfig, baseDir string) {
	if cfg == nil || cfg.GeoSite.Path == "" || filepath.IsAbs(cfg.GeoSite.Path) {
		return
	}
	cfg.GeoSite.Path = filepath.Join(baseDir, cfg.GeoSite.Path)
}

// ManagedGeoSitePath 返回系统托管的 GeoSite 数据文件路径：
// 与运行配置同目录的 geosite.dat（原生运行与 Docker 布局一致）。
func ManagedGeoSitePath(configDir string) string {
	return filepath.Join(configDir, "geosite.dat")
}

// ResolveGeoSiteDataPath 在未配置路径时填充系统托管路径，
// 数据文件位置不再由管理界面或用户输入决定。
func ResolveGeoSiteDataPath(cfg *GeoSiteConfig, configDir string) {
	if cfg == nil || strings.TrimSpace(cfg.Path) != "" {
		return
	}
	cfg.Path = ManagedGeoSitePath(configDir)
}

func NewRouter(cfg RoutingConfig, dnsCfg DNSConfig) (*Router, error) {
	finalAction := normalizeAction(cfg.Final)
	if finalAction == "" {
		finalAction = ActionAuto
	}
	if !validAction(finalAction) {
		return nil, fmt.Errorf("routing.final has unsupported action %q", cfg.Final)
	}

	codes := geositeCodes(cfg.Rules)
	var matcher *GeoSiteMatcher
	if len(codes) > 0 {
		if strings.TrimSpace(cfg.GeoSite.Path) == "" {
			Logger().Warnw("routing.geosite.path is empty but geosite rules are used, geosite matching disabled")
		} else {
			var err error
			matcher, err = LoadGeoSiteDat(cfg.GeoSite.Path, codes)
			if err != nil {
				Logger().Warnw("load geosite dat failed, geosite matching disabled until file is available",
					"path", cfg.GeoSite.Path, "error", err)
			}
		}
	}

	r := &Router{
		final:   RouteDecision{Action: finalAction},
		geoSite: matcher,
		policy:  cfg.Policy,
		auto: autoRuntimeConfig{
			Mode:          normalizeAutoMode(cfg.Auto.Mode),
			DirectTimeout: cfg.Auto.DirectTimeout.value(defaultDirectTimeout),
			CacheTTL:      cfg.Auto.CacheTTL.value(defaultCacheTTL),
		},
		socks5h: Socks5HConfig{
			Enabled:   cfg.Socks5HRedirect.Enabled,
			Upstreams: normalizeUpstreamNames(cfg.Socks5HRedirect.Upstreams),
		},
		cache:        NewRouteCache(),
		resolver:     NewDNSResolver(dnsCfg),
		ipPrecedence: normalizeIPPrecedence(dnsCfg.IPPrecedence),
	}

	for i, rule := range cfg.Rules {
		compiled, err := compileRouteRule(rule)
		if err != nil {
			return nil, fmt.Errorf("routing.rules[%d]: %w", i, err)
		}
		r.rules = append(r.rules, compiled)
	}
	return r, nil
}

func compileRouteRule(rule RouteRule) (compiledRouteRule, error) {
	typ := normalizeRuleType(rule.Type)
	if typ == "" {
		return compiledRouteRule{}, fmt.Errorf("type must not be empty")
	}
	action := normalizeAction(rule.Action)
	if action == "" {
		action = ActionAuto
	}
	if !validAction(action) {
		return compiledRouteRule{}, fmt.Errorf("unsupported action %q", rule.Action)
	}
	if action == ActionDeny && typ == "match" {
		return compiledRouteRule{}, fmt.Errorf("deny action is not allowed with type match")
	}
	compiled := compiledRouteRule{
		typ: typ,
		decision: RouteDecision{
			Action:    action,
			Upstreams: normalizeUpstreamNames(rule.Upstreams),
		},
	}

	switch typ {
	case "domain", "domain_suffix", "domain_keyword":
		for _, v := range rule.Values {
			v = normalizeDomainRuleValue(v)
			if v != "" {
				compiled.values = append(compiled.values, v)
			}
		}
	case "geosite":
		for _, v := range rule.Values {
			v = normalizeGeoSiteCode(v)
			if v != "" {
				compiled.values = append(compiled.values, v)
			}
		}
	case "domain_regex":
		for _, v := range rule.Values {
			re, err := regexp.Compile(v)
			if err != nil {
				return compiledRouteRule{}, fmt.Errorf("compile regex %q: %w", v, err)
			}
			compiled.regexps = append(compiled.regexps, re)
		}
	case "ip_cidr":
		for _, v := range rule.Values {
			_, network, err := net.ParseCIDR(strings.TrimSpace(v))
			if err != nil {
				return compiledRouteRule{}, fmt.Errorf("parse cidr %q: %w", v, err)
			}
			compiled.cidrs = append(compiled.cidrs, network)
		}
	case "match":
		// MATCH does not need values.
	default:
		return compiledRouteRule{}, fmt.Errorf("unsupported type %q", rule.Type)
	}
	return compiled, nil
}

func (r *Router) Decide(target string) RouteDecision {
	if r == nil {
		MetricInc("yigate_routing_decisions_total", "action=auto")
		return RouteDecision{Action: ActionAuto}
	}
	host := normalizeTargetHost(target)
	ip := net.ParseIP(host)
	for _, rule := range r.rules {
		if rule.match(host, ip, r.geoSite) {
			MetricInc("yigate_routing_decisions_total", "action="+rule.decision.Action)
			return rule.decision
		}
	}
	MetricInc("yigate_routing_decisions_total", "action="+r.final.Action)
	return r.final
}

// CheckTarget 对所有需要访问目标地址的路径执行统一的目标访问策略检查。
// 路由决策（Decide）只决定走直连还是上游，不能替代访问授权；协议层在
// 建立任何连接之前都应调用本方法，避免经上游转发时绕过策略。
func (r *Router) CheckTarget(target string) error {
	if r == nil || r.policy == nil {
		return nil
	}
	if err := r.policy.CheckTarget(target); err != nil {
		return err
	}
	if !r.policy.ShouldResolveBeforeUpstream() {
		return nil
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(target))
	if err != nil || parseIPLiteral(host) != nil {
		// 无端口或字面 IP：策略层已完成校验，不需要再解析。
		return nil
	}
	port := 0
	if parsed, ok := parsePort(portStr); ok {
		port = parsed
	}
	// 经上游转发时目标由上游解析，网关默认看不到最终 IP；开启严格模式后
	// 在网关侧预解析并逐个校验，防止域名指向内网或云元数据。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := r.lookupIPAddr(ctx, host)
	if err != nil {
		return deniedErr("upstream target %q could not be resolved for policy checks", host)
	}
	for _, addr := range addrs {
		if err := r.policy.Check(host, port, addr.IP); err != nil {
			return err
		}
	}
	return nil
}

func (r *Router) Dial(target string, mgr *UpstreamManager, sourceIP ...string) (net.Conn, error) {
	decision := r.Decide(target)
	if decision.Action == ActionDeny {
		return nil, NewError(CodeDestinationDenied, "destination denied by route rule: "+target)
	}
	if err := r.CheckTarget(target); err != nil {
		return nil, err
	}
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	switch decision.Action {
	case ActionDirect:
		return r.dialDirectChecked(target)
	case ActionProxy:
		conn, _, err := r.dialProxy(target, mgr, decision.Upstreams, sip)
		return conn, err
	default:
		return r.dialAuto(target, mgr, sip)
	}
}

// Policy 返回当前生效的目标访问策略（可能为 nil）。
func (r *Router) Policy() *DestinationPolicy {
	if r == nil {
		return nil
	}
	return r.policy
}

// Resolver 返回路由器关联的 DNS 解析器（可能为 nil，表示使用系统默认）。
func (r *Router) Resolver() *DNSResolver {
	if r == nil {
		return nil
	}
	return r.resolver
}

// IPPrecedence 返回 IP 优先级策略（ipv4_first / ipv6_first / system）。
func (r *Router) IPPrecedence() string {
	if r == nil {
		return IPPrecedenceIPv4First
	}
	return r.ipPrecedence
}

// Start 启动路由器的后台任务（当前为路由缓存过期清理）。
// 由服务在创建路由器后立即调用；测试通常不调用，以避免泄漏 goroutine。
func (r *Router) Start() {
	if r == nil {
		return
	}
	if r.cache != nil {
		r.cache.StartCleanup(defaultRouteCacheCleanupInterval)
	}
}

// Stop 释放路由器持有的后台资源（如路由缓存的过期清理 goroutine）。
// 应在热加载替换旧路由器或进程关闭时调用。
func (r *Router) Stop() {
	if r == nil {
		return
	}
	if r.cache != nil {
		r.cache.Stop()
	}
	// DNS 解析器在非 system 模式下持有后台缓存清理 goroutine，
	// 热加载替换路由器时必须一并停止，否则每次重载都会泄漏一个 goroutine。
	if r.resolver != nil {
		r.resolver.Stop()
	}
}

func (r *Router) dialAuto(target string, mgr *UpstreamManager, sourceIP string) (net.Conn, error) {
	key := routeCacheKey(target)
	if entry, ok := r.cache.Get(key); ok {
		switch entry.Action {
		case ActionDirect:
			conn, err := r.dialDirectChecked(target)
			if err == nil {
				return conn, nil
			}
			r.cache.Delete(key)
		case ActionProxy:
			conn, _, err := r.dialProxy(target, mgr, []string{entry.UpstreamName}, sourceIP)
			if err == nil {
				return conn, nil
			}
			r.cache.Delete(key)
		}
	}

	// 缓存未命中：用 singleflight 合并对同一目标的探测决策，
	// 避免高并发下每个请求都独立经历完整的直连超时 + 代理尝试。
	result, err, _ := r.sfGroup.Do(key, func() (interface{}, error) {
		// 再次检查缓存，可能在等待期间已被其他请求填充。
		if entry, ok := r.cache.Get(key); ok {
			return entry, nil
		}
		if r.auto.Mode == "proxy_first" {
			conn, up, dialErr := r.dialProxy(target, mgr, nil, sourceIP)
			if dialErr == nil {
				_ = conn.Close()
				r.cache.SetProxy(key, up.Name, r.auto.CacheTTL)
				return RouteCacheEntry{Action: ActionProxy, UpstreamName: up.Name}, nil
			}
			conn, directErr := r.dialDirectChecked(target)
			if directErr == nil {
				_ = conn.Close()
				r.cache.SetDirect(key, r.auto.CacheTTL)
				return RouteCacheEntry{Action: ActionDirect}, nil
			}
			return nil, fmt.Errorf("proxy failed: %v; direct failed: %w", dialErr, directErr)
		}
		// direct_first（默认）
		conn, directErr := r.dialDirectChecked(target)
		if directErr == nil {
			_ = conn.Close()
			r.cache.SetDirect(key, r.auto.CacheTTL)
			return RouteCacheEntry{Action: ActionDirect}, nil
		}
		conn, up, proxyErr := r.dialProxy(target, mgr, nil, sourceIP)
		if proxyErr == nil {
			_ = conn.Close()
			r.cache.SetProxy(key, up.Name, r.auto.CacheTTL)
			return RouteCacheEntry{Action: ActionProxy, UpstreamName: up.Name}, nil
		}
		return nil, fmt.Errorf("direct failed: %v; proxy failed: %w", directErr, proxyErr)
	})
	if err != nil {
		return nil, err
	}
	entry, ok := result.(RouteCacheEntry)
	if !ok {
		return nil, fmt.Errorf("invalid route probe result %T", result)
	}
	switch entry.Action {
	case ActionDirect:
		return r.dialDirectChecked(target)
	case ActionProxy:
		if entry.UpstreamName != "" {
			conn, _, err := r.dialProxy(target, mgr, []string{entry.UpstreamName}, sourceIP)
			return conn, err
		}
	}
	return nil, fmt.Errorf("unsupported cached route action %q", entry.Action)
}

func (r *Router) dialProxy(target string, mgr *UpstreamManager, names []string, sourceIP string) (net.Conn, *Upstream, error) {
	key := routeCacheKey(target)
	if len(names) == 0 {
		if entry, ok := r.cache.Get(key); ok && entry.Action == ActionProxy && entry.UpstreamName != "" {
			if conn, up, err := mgr.DialOne(target, entry.UpstreamName); err == nil {
				return conn, up, nil
			}
			r.cache.Delete(key)
		}
	}
	conn, up, err := mgr.DialProxyWithSourceIP(target, sourceIP, names)
	if err == nil && up != nil {
		r.cache.SetProxy(key, up.Name, r.auto.CacheTTL)
	}
	return conn, up, err
}

func (rule compiledRouteRule) match(host string, ip net.IP, matcher *GeoSiteMatcher) bool {
	switch rule.typ {
	case "domain":
		for _, value := range rule.values {
			if host == value {
				return true
			}
		}
	case "domain_suffix":
		for _, value := range rule.values {
			if domainSuffixMatch(host, value) {
				return true
			}
		}
	case "domain_keyword":
		for _, value := range rule.values {
			if strings.Contains(host, value) {
				return true
			}
		}
	case "domain_regex":
		for _, re := range rule.regexps {
			if re.MatchString(host) {
				return true
			}
		}
	case "ip_cidr":
		if ip == nil {
			return false
		}
		for _, network := range rule.cidrs {
			if network.Contains(ip) {
				return true
			}
		}
	case "geosite":
		if matcher == nil || ip != nil {
			return false
		}
		for _, code := range rule.values {
			if matcher.Match(code, host) {
				return true
			}
		}
	case "match":
		return true
	}
	return false
}

func routeCacheKey(target string) string {
	return normalizeTargetHost(target)
}

func normalizeTargetHost(target string) string {
	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[] ")
	host = strings.TrimSuffix(host, ".")
	return host
}

func normalizeRuleType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "-", "_")
	return v
}

func normalizeAction(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func validAction(action string) bool {
	return action == ActionDirect || action == ActionProxy || action == ActionAuto || action == ActionDeny
}

func normalizeAutoMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "proxy_first" {
		return v
	}
	return defaultAutoMode
}

func normalizeIPPrecedence(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case IPPrecedenceIPv6First, IPPrecedenceSystem:
		return v
	default:
		return IPPrecedenceIPv4First
	}
}

func normalizeDomainRuleValue(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "+.")
	v = strings.TrimPrefix(v, ".")
	v = strings.TrimSuffix(v, ".")
	return v
}

func normalizeGeoSiteCode(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "GEOSITE:")
	return strings.TrimSpace(v)
}

func normalizeUpstreamNames(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func domainSuffixMatch(host, suffix string) bool {
	return host == suffix || strings.HasSuffix(host, "."+suffix)
}

func geositeCodes(rules []RouteRule) []string {
	seen := make(map[string]struct{})
	var codes []string
	for _, rule := range rules {
		if normalizeRuleType(rule.Type) != "geosite" {
			continue
		}
		for _, value := range rule.Values {
			code := normalizeGeoSiteCode(value)
			if code == "" {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			codes = append(codes, code)
		}
	}
	return codes
}

// HasGeoSiteRules returns true if any routing rule uses the geosite type.
func HasGeoSiteRules(rules []RouteRule) bool {
	return len(geositeCodes(rules)) > 0
}
