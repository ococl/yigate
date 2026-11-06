package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"yigate/proxy"
)

// configEditor 实现 proxy.ConfigEditor 接口。
// 它通过原子性地修改配置文件并触发热加载来工作。
type configEditor struct {
	cfgPath string
	apply   func() error
	applyMu sync.Mutex
}

// errNotFound 表示资源不存在，用于区分 404 和 400 响应。
var errNotFound = proxy.ErrNotFound

// newConfigEditor 创建配置编辑器。
// apply 回调用于触发热加载（通常是 Server.LoadAndApply）。
func newConfigEditor(cfgPath string, apply func() error) *configEditor {
	return &configEditor{
		cfgPath: cfgPath,
		apply:   apply,
	}
}

// loadRawConfig 从磁盘加载配置，不展开环境变量，不校验。
// 用于读取可安全修改的原始配置（保留 ${ENV_VAR} 占位符）。
// 以默认配置为基底反序列化，避免用户手写的精简配置在写回时丢失默认值
// （例如 routing.geosite.update_interval 变成 0 导致下次启动失败）。
func (e *configEditor) loadRawConfig() (*Config, error) {
	data, err := os.ReadFile(e.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfg := defaultConfig()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		// 空文件视为默认配置
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse config file: %w", err)
		}
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if cfg.Upstreams == nil {
		cfg.Upstreams = []proxy.UpstreamConfig{}
	}
	if cfg.Routing.Rules == nil {
		cfg.Routing.Rules = []proxy.RouteRule{}
	}
	normalizeZeroDefaults(cfg)
	return cfg, nil
}

// normalizeZeroDefaults 把“零值表示继承默认”的字段转换为具体默认值。
// 旧版本写回配置时曾把未配置的超时写成 0，会让管理界面展示为 0 秒，
// 这里在读取时补齐，确保界面展示的是实际生效的数值。
func normalizeZeroDefaults(cfg *Config) {
	if cfg.Timeouts.Dial <= 0 || cfg.Timeouts.Handshake <= 0 ||
		cfg.Timeouts.HTTPExchange <= 0 || cfg.Timeouts.TunnelIdle <= 0 {
		def := proxy.DefaultTimeoutConfig()
		if cfg.Timeouts.Dial <= 0 {
			cfg.Timeouts.Dial = def.Dial
		}
		if cfg.Timeouts.Handshake <= 0 {
			cfg.Timeouts.Handshake = def.Handshake
		}
		if cfg.Timeouts.HTTPExchange <= 0 {
			cfg.Timeouts.HTTPExchange = def.HTTPExchange
		}
		if cfg.Timeouts.TunnelIdle <= 0 {
			cfg.Timeouts.TunnelIdle = def.TunnelIdle
		}
	}
	if cfg.DNS.Timeout <= 0 {
		cfg.DNS.Timeout = proxy.DefaultDNSTimeout()
	}
	if strings.TrimSpace(cfg.DNS.Type) == "" {
		cfg.DNS.Type = "system"
	}
	if strings.TrimSpace(cfg.DNS.IPPrecedence) == "" {
		cfg.DNS.IPPrecedence = "ipv4_first"
	}
	if cfg.TrafficAudit.QuotaWindow <= 0 {
		cfg.TrafficAudit.QuotaWindow = proxy.Duration(time.Hour)
	}
}

// saveAndApply 保存配置到磁盘并触发热加载。
// 写入失败或热加载失败时回滚到写入前的内容，避免磁盘上残留无法生效的配置。
func (e *configEditor) saveAndApply(cfg *Config) error {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()

	if err := validateConfig(cfg); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	original, readErr := os.ReadFile(e.cfgPath)
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(e.cfgPath); statErr == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(e.cfgPath)
	tmp, err := os.CreateTemp(dir, ".yigate-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, e.cfgPath); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}

	if e.apply != nil {
		if err := e.apply(); err != nil {
			if readErr == nil {
				// 回滚配置内容并尽力恢复运行时状态
				if writeErr := os.WriteFile(e.cfgPath, original, mode); writeErr == nil {
					_ = e.apply()
				}
			}
			return fmt.Errorf("apply config: %w", err)
		}
	}
	return nil
}

// validateUpstream 校验上游配置的基本合法性。
func validateUpstream(up proxy.UpstreamConfig) error {
	if strings.TrimSpace(up.URL) == "" {
		return errors.New("url must not be empty")
	}
	parsed, err := url.Parse(strings.TrimSpace(up.URL))
	if err != nil {
		return fmt.Errorf("url is invalid: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("url scheme must be http, https, socks5, or socks5h, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("url must include host:port")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("url port must be between 1 and 65535, got %q", port)
		}
	}
	if up.Weight < 0 {
		return errors.New("weight must be >= 0")
	}
	if up.Weight > maxUpstreamWeight {
		return fmt.Errorf("weight must be <= %d", maxUpstreamWeight)
	}
	if err := proxy.ValidateProxyTypes(up.Types); err != nil {
		return err
	}
	if err := proxy.ValidateUpstreamStrategy(up.Strategy); err != nil {
		return err
	}
	if err := proxy.ValidateHealthCheckConfig(up.HealthCheck); err != nil {
		return err
	}
	return nil
}

// allowedRuleTypes 与 proxy.CompileRouter 支持的规则类型保持一致。
var allowedRuleTypes = map[string]struct{}{
	"domain":         {},
	"domain_suffix":  {},
	"domain_keyword": {},
	"domain_regex":   {},
	"ip_cidr":        {},
	"geosite":        {},
	"match":          {},
}

var allowedRuleActions = map[string]struct{}{
	proxy.ActionDirect: {},
	proxy.ActionProxy:  {},
	proxy.ActionAuto:   {},
	proxy.ActionDeny:   {},
}

// validateRouteRule 在写入前校验路由规则，避免把非法规则持久化到磁盘。
func validateRouteRule(rule proxy.RouteRule, upstreams []proxy.UpstreamConfig) error {
	typ := strings.ToLower(strings.TrimSpace(rule.Type))
	if _, ok := allowedRuleTypes[typ]; !ok {
		return fmt.Errorf("unsupported rule type %q", rule.Type)
	}
	action := strings.ToLower(strings.TrimSpace(rule.Action))
	if action == "" {
		action = proxy.ActionAuto
	}
	if _, ok := allowedRuleActions[action]; !ok {
		return fmt.Errorf("unsupported rule action %q", rule.Action)
	}
	if action == proxy.ActionDeny && typ == "match" {
		return errors.New("deny action is not allowed with type match")
	}

	values := make([]string, 0, len(rule.Values))
	for _, v := range rule.Values {
		v = strings.TrimSpace(v)
		if v != "" {
			values = append(values, v)
		}
	}
	if typ != "match" && len(values) == 0 {
		return fmt.Errorf("rule type %q requires at least one value", typ)
	}
	switch typ {
	case "domain_regex":
		for _, v := range values {
			if _, err := regexp.Compile(v); err != nil {
				return fmt.Errorf("invalid domain_regex %q: %w", v, err)
			}
		}
	case "ip_cidr":
		for _, v := range values {
			if _, _, err := net.ParseCIDR(v); err != nil {
				return fmt.Errorf("invalid ip_cidr %q: %w", v, err)
			}
		}
	}

	known := make(map[string]struct{}, len(upstreams))
	for i := range upstreams {
		name := strings.TrimSpace(upstreams[i].Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", i+1)
		}
		known[name] = struct{}{}
	}
	for _, ref := range rule.Upstreams {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := known[ref]; !ok {
			return fmt.Errorf("rule references unknown upstream %q", ref)
		}
	}
	return nil
}

func (e *configEditor) ListUpstreams() []proxy.UpstreamConfig {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	return cfg.Upstreams
}

func (e *configEditor) AddUpstream(up proxy.UpstreamConfig) error {
	up.Name = strings.TrimSpace(up.Name)
	if err := validateUpstream(up); err != nil {
		return err
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name := up.Name
	if name == "" {
		name = nextUpstreamName(cfg.Upstreams)
		up.Name = name
	}
	for _, existing := range cfg.Upstreams {
		if existing.Name == name {
			return fmt.Errorf("upstream %q already exists", name)
		}
	}
	cfg.Upstreams = append(cfg.Upstreams, up)
	return e.saveAndApply(cfg)
}

// nextUpstreamName 生成不与现有上游冲突的默认名称。
func nextUpstreamName(ups []proxy.UpstreamConfig) string {
	used := make(map[string]struct{}, len(ups))
	for _, up := range ups {
		used[strings.TrimSpace(up.Name)] = struct{}{}
	}
	for i := len(ups) + 1; ; i++ {
		name := fmt.Sprintf("upstream-%d", i)
		if _, ok := used[name]; !ok {
			return name
		}
	}
}

func (e *configEditor) UpdateUpstream(name string, up proxy.UpstreamConfig) error {
	if err := validateUpstream(up); err != nil {
		return err
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	newName := strings.TrimSpace(up.Name)
	if newName == "" {
		newName = name
	}

	// 如果要改名，先校验新名称未被占用
	if newName != name {
		for _, existing := range cfg.Upstreams {
			if existing.Name == newName {
				return fmt.Errorf("upstream %q already exists", newName)
			}
		}
	}

	found := false
	for i, existing := range cfg.Upstreams {
		if existing.Name == name {
			up.Name = newName
			cfg.Upstreams[i] = up
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: upstream %q not found", errNotFound, name)
	}

	// 改名时同步更新路由规则与 socks5h_redirect 中对旧名称的引用
	if newName != name {
		for i := range cfg.Routing.Rules {
			for j, ref := range cfg.Routing.Rules[i].Upstreams {
				if strings.TrimSpace(ref) == name {
					cfg.Routing.Rules[i].Upstreams[j] = newName
				}
			}
		}
		for i, ref := range cfg.Routing.Socks5HRedirect.Upstreams {
			if strings.TrimSpace(ref) == name {
				cfg.Routing.Socks5HRedirect.Upstreams[i] = newName
			}
		}
	}

	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveUpstream(name string) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	found := false
	for i, existing := range cfg.Upstreams {
		if existing.Name == name {
			cfg.Upstreams = append(cfg.Upstreams[:i], cfg.Upstreams[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: upstream %q not found", errNotFound, name)
	}
	// 同时清理路由规则中对该上游的引用
	for i := range cfg.Routing.Rules {
		filtered := make([]string, 0, len(cfg.Routing.Rules[i].Upstreams))
		for _, ref := range cfg.Routing.Rules[i].Upstreams {
			if strings.TrimSpace(ref) != name {
				filtered = append(filtered, ref)
			}
		}
		cfg.Routing.Rules[i].Upstreams = filtered
	}
	// 清理 socks5h_redirect 中的引用
	socksRefs := cfg.Routing.Socks5HRedirect.Upstreams
	filteredSocks := make([]string, 0, len(socksRefs))
	for _, ref := range socksRefs {
		if strings.TrimSpace(ref) != name {
			filteredSocks = append(filteredSocks, ref)
		}
	}
	cfg.Routing.Socks5HRedirect.Upstreams = filteredSocks
	return e.saveAndApply(cfg)
}

func (e *configEditor) ListRules() []proxy.RouteRule {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	return cfg.Routing.Rules
}

func (e *configEditor) AddRule(rule proxy.RouteRule) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if err := validateRouteRule(rule, cfg.Upstreams); err != nil {
		return err
	}
	cfg.Routing.Rules = append(cfg.Routing.Rules, rule)
	return e.saveAndApply(cfg)
}

func (e *configEditor) UpdateRule(index int, rule proxy.RouteRule) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(cfg.Routing.Rules) {
		return fmt.Errorf("%w: rule index %d out of range", errNotFound, index)
	}
	if err := validateRouteRule(rule, cfg.Upstreams); err != nil {
		return err
	}
	cfg.Routing.Rules[index] = rule
	return e.saveAndApply(cfg)
}

func (e *configEditor) MoveRule(index int, direction string) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(cfg.Routing.Rules) {
		return fmt.Errorf("%w: rule index %d out of range", errNotFound, index)
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up":
		if index == 0 {
			return errors.New("rule is already at the top")
		}
		cfg.Routing.Rules[index-1], cfg.Routing.Rules[index] = cfg.Routing.Rules[index], cfg.Routing.Rules[index-1]
	case "down":
		if index == len(cfg.Routing.Rules)-1 {
			return errors.New("rule is already at the bottom")
		}
		cfg.Routing.Rules[index], cfg.Routing.Rules[index+1] = cfg.Routing.Rules[index+1], cfg.Routing.Rules[index]
	default:
		return fmt.Errorf("direction must be up or down, got %q", direction)
	}
	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveRule(index int) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(cfg.Routing.Rules) {
		if len(cfg.Routing.Rules) == 0 {
			return fmt.Errorf("%w: no rules configured", errNotFound)
		}
		return fmt.Errorf("%w: rule index %d out of range (0-%d)", errNotFound, index, len(cfg.Routing.Rules)-1)
	}
	cfg.Routing.Rules = append(cfg.Routing.Rules[:index], cfg.Routing.Rules[index+1:]...)
	return e.saveAndApply(cfg)
}

func (e *configEditor) ListUsers() []string {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(cfg.Users))
	for name := range cfg.Users {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	// 多 listener 模式下聚合各 listener 的用户
	for i := range cfg.Listeners {
		for name := range cfg.Listeners[i].Users {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				result = append(result, name)
			}
		}
	}
	sort.Strings(result)
	return result
}

func (e *configEditor) AddUser(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username must not be empty")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password must not be empty")
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if _, exists := cfg.Users[username]; exists {
		return fmt.Errorf("user %q already exists", username)
	}
	cfg.Users[username] = password
	// 多 listener 模式下同步添加到所有 listener
	for i := range cfg.Listeners {
		if cfg.Listeners[i].Users == nil {
			cfg.Listeners[i].Users = make(map[string]string)
		}
		cfg.Listeners[i].Users[username] = password
	}
	return e.saveAndApply(cfg)
}

func (e *configEditor) UpdateUser(username, password string) error {
	username = strings.TrimSpace(username)
	if strings.TrimSpace(password) == "" {
		return errors.New("password must not be empty")
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	found := false
	if _, exists := cfg.Users[username]; exists {
		cfg.Users[username] = password
		found = true
	}
	for i := range cfg.Listeners {
		if cfg.Listeners[i].Users == nil {
			continue
		}
		if _, exists := cfg.Listeners[i].Users[username]; exists {
			cfg.Listeners[i].Users[username] = password
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: user %q not found", errNotFound, username)
	}
	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveUser(username string) error {
	username = strings.TrimSpace(username)
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	found := false
	if _, exists := cfg.Users[username]; exists {
		delete(cfg.Users, username)
		found = true
	}
	// 多 listener 模式下同步从所有 listener 删除
	for i := range cfg.Listeners {
		if cfg.Listeners[i].Users == nil {
			continue
		}
		if _, exists := cfg.Listeners[i].Users[username]; exists {
			delete(cfg.Listeners[i].Users, username)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: user %q not found", errNotFound, username)
	}
	return e.saveAndApply(cfg)
}

// GetSettings 返回全局设置视图。
func (e *configEditor) GetSettings() (proxy.SettingsView, error) {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return proxy.SettingsView{}, err
	}
	webuiEnabled := true
	if cfg.WebUI != nil {
		webuiEnabled = cfg.WebUI.Enabled
	}
	view := proxy.SettingsView{
		Listen:              cfg.Listen,
		Management:          cfg.Management,
		ManagementTokenSet:  strings.TrimSpace(cfg.ManagementAuthToken) != "",
		WebUIEnabled:        webuiEnabled,
		CheckIntervalSec:    cfg.CheckInterval,
		MaxConnections:      cfg.MaxConnections,
		EnablePprof:         cfg.EnablePprof,
		Socks5AllowBind:     cfg.Socks5.AllowBind,
		Socks5AllowUDPAssoc: cfg.Socks5.AllowUDPAssociate,

		RoutingFinal:           cfg.Routing.Final,
		AutoMode:               cfg.Routing.Auto.Mode,
		AutoDirectTimeoutSec:   time.Duration(cfg.Routing.Auto.DirectTimeout).Seconds(),
		AutoCacheTTLSec:        time.Duration(cfg.Routing.Auto.CacheTTL).Seconds(),
		GeoSitePath:            cfg.Routing.GeoSite.Path,
		GeoSiteAutoUpdate:      cfg.Routing.GeoSite.AutoUpdate,
		GeoSiteUpdateURL:       cfg.Routing.GeoSite.UpdateURL,
		GeoSiteUpdateIntervalH: time.Duration(cfg.Routing.GeoSite.UpdateInterval).Hours(),
		Socks5HRedirectEnabled: cfg.Routing.Socks5HRedirect.Enabled,

		DNSType:       cfg.DNS.Type,
		DoHURL:        cfg.DNS.DoHURL,
		DoTAddr:       cfg.DNS.DoTAddr,
		DNSFallback:   cfg.DNS.Fallback,
		DNSTimeoutSec: time.Duration(cfg.DNS.Timeout).Seconds(),
		IPPrecedence:  cfg.DNS.IPPrecedence,

		DialTimeoutSec:         time.Duration(cfg.Timeouts.Dial).Seconds(),
		HandshakeTimeoutSec:    time.Duration(cfg.Timeouts.Handshake).Seconds(),
		HTTPExchangeTimeoutSec: time.Duration(cfg.Timeouts.HTTPExchange).Seconds(),
		TunnelIdleTimeoutSec:   time.Duration(cfg.Timeouts.TunnelIdle).Seconds(),

		ProtectPrivate: cfg.DestinationPolicy.ProtectPrivate,
		DefaultAction:  cfg.DestinationPolicy.DefaultAction,
		DenyCIDRs:      cfg.DestinationPolicy.DenyCIDRs,
		AllowCIDRs:     cfg.DestinationPolicy.AllowCIDRs,
		DenyDomains:    cfg.DestinationPolicy.DenyDomains,
		AllowDomains:   cfg.DestinationPolicy.AllowDomains,
		DenyPorts:      cfg.DestinationPolicy.DenyPorts,
		AllowPorts:     cfg.DestinationPolicy.AllowPorts,

		AllowedClients:     cfg.AccessControl.AllowedClients,
		RateLimitPerSecond: cfg.AccessControl.RateLimit.ConnectionsPerSecond,
		RateLimitBurst:     cfg.AccessControl.RateLimit.Burst,
		MaxConnsPerUser:    cfg.AccessControl.MaxConnsPerUser,

		TrafficAuditEnabled: cfg.TrafficAudit.Enabled,
		PerUserQuota:        cfg.TrafficAudit.PerUserQuota,
		PerUpstreamQuota:    cfg.TrafficAudit.PerUpstreamQuota,
		QuotaWindowSec:      time.Duration(cfg.TrafficAudit.QuotaWindow).Seconds(),

		LogLevel: cfg.Log.Level,
	}
	return view, nil
}

// UpdateSettings 应用全局设置的部分更新。
func (e *configEditor) UpdateSettings(patch proxy.SettingsPatch) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}

	if patch.CheckIntervalSec != nil {
		cfg.CheckInterval = *patch.CheckIntervalSec
	}
	if patch.MaxConnections != nil {
		cfg.MaxConnections = *patch.MaxConnections
	}
	if patch.EnablePprof != nil {
		cfg.EnablePprof = *patch.EnablePprof
	}
	if patch.Socks5AllowBind != nil {
		cfg.Socks5.AllowBind = *patch.Socks5AllowBind
	}
	if patch.Socks5AllowUDPAssoc != nil {
		cfg.Socks5.AllowUDPAssociate = *patch.Socks5AllowUDPAssoc
	}
	if patch.RoutingFinal != nil {
		cfg.Routing.Final = strings.TrimSpace(*patch.RoutingFinal)
	}
	if patch.AutoMode != nil {
		cfg.Routing.Auto.Mode = strings.TrimSpace(*patch.AutoMode)
	}
	if patch.AutoDirectTimeoutSec != nil {
		cfg.Routing.Auto.DirectTimeout = durationFromSeconds(*patch.AutoDirectTimeoutSec)
	}
	if patch.AutoCacheTTLSec != nil {
		cfg.Routing.Auto.CacheTTL = durationFromSeconds(*patch.AutoCacheTTLSec)
	}
	if patch.GeoSitePath != nil {
		cfg.Routing.GeoSite.Path = strings.TrimSpace(*patch.GeoSitePath)
	}
	if patch.GeoSiteAutoUpdate != nil {
		cfg.Routing.GeoSite.AutoUpdate = *patch.GeoSiteAutoUpdate
	}
	if patch.GeoSiteUpdateURL != nil {
		cfg.Routing.GeoSite.UpdateURL = strings.TrimSpace(*patch.GeoSiteUpdateURL)
	}
	if patch.GeoSiteUpdateIntervalH != nil {
		cfg.Routing.GeoSite.UpdateInterval = durationFromSeconds(*patch.GeoSiteUpdateIntervalH * 3600)
	}
	if patch.Socks5HRedirectEnabled != nil {
		cfg.Routing.Socks5HRedirect.Enabled = *patch.Socks5HRedirectEnabled
	}
	if patch.DNSType != nil {
		cfg.DNS.Type = strings.ToLower(strings.TrimSpace(*patch.DNSType))
	}
	if patch.DoHURL != nil {
		cfg.DNS.DoHURL = strings.TrimSpace(*patch.DoHURL)
	}
	if patch.DoTAddr != nil {
		cfg.DNS.DoTAddr = strings.TrimSpace(*patch.DoTAddr)
	}
	if patch.DNSFallback != nil {
		cfg.DNS.Fallback = *patch.DNSFallback
	}
	if patch.DNSTimeoutSec != nil {
		cfg.DNS.Timeout = durationFromSeconds(*patch.DNSTimeoutSec)
	}
	if patch.IPPrecedence != nil {
		cfg.DNS.IPPrecedence = strings.ToLower(strings.TrimSpace(*patch.IPPrecedence))
	}
	if patch.DialTimeoutSec != nil {
		cfg.Timeouts.Dial = durationFromSeconds(*patch.DialTimeoutSec)
	}
	if patch.HandshakeTimeoutSec != nil {
		cfg.Timeouts.Handshake = durationFromSeconds(*patch.HandshakeTimeoutSec)
	}
	if patch.HTTPExchangeTimeoutSec != nil {
		cfg.Timeouts.HTTPExchange = durationFromSeconds(*patch.HTTPExchangeTimeoutSec)
	}
	if patch.TunnelIdleTimeoutSec != nil {
		cfg.Timeouts.TunnelIdle = durationFromSeconds(*patch.TunnelIdleTimeoutSec)
	}
	if patch.ProtectPrivate != nil {
		cfg.DestinationPolicy.ProtectPrivate = *patch.ProtectPrivate
	}
	if patch.DefaultAction != nil {
		cfg.DestinationPolicy.DefaultAction = strings.ToLower(strings.TrimSpace(*patch.DefaultAction))
	}
	if patch.DenyCIDRs != nil {
		cfg.DestinationPolicy.DenyCIDRs = patch.DenyCIDRs
	}
	if patch.AllowCIDRs != nil {
		cfg.DestinationPolicy.AllowCIDRs = patch.AllowCIDRs
	}
	if patch.DenyDomains != nil {
		cfg.DestinationPolicy.DenyDomains = patch.DenyDomains
	}
	if patch.AllowDomains != nil {
		cfg.DestinationPolicy.AllowDomains = patch.AllowDomains
	}
	if patch.DenyPorts != nil {
		cfg.DestinationPolicy.DenyPorts = patch.DenyPorts
	}
	if patch.AllowPorts != nil {
		cfg.DestinationPolicy.AllowPorts = patch.AllowPorts
	}
	if patch.AllowedClients != nil {
		cfg.AccessControl.AllowedClients = patch.AllowedClients
	}
	if patch.RateLimitPerSecond != nil {
		cfg.AccessControl.RateLimit.ConnectionsPerSecond = *patch.RateLimitPerSecond
	}
	if patch.RateLimitBurst != nil {
		cfg.AccessControl.RateLimit.Burst = *patch.RateLimitBurst
	}
	if patch.MaxConnsPerUser != nil {
		cfg.AccessControl.MaxConnsPerUser = *patch.MaxConnsPerUser
	}
	if patch.TrafficAuditEnabled != nil {
		cfg.TrafficAudit.Enabled = *patch.TrafficAuditEnabled
	}
	if patch.PerUserQuota != nil {
		cfg.TrafficAudit.PerUserQuota = *patch.PerUserQuota
	}
	if patch.PerUpstreamQuota != nil {
		cfg.TrafficAudit.PerUpstreamQuota = *patch.PerUpstreamQuota
	}
	if patch.QuotaWindowSec != nil {
		cfg.TrafficAudit.QuotaWindow = durationFromSeconds(*patch.QuotaWindowSec)
	}
	if patch.LogLevel != nil {
		cfg.Log.Level = strings.ToLower(strings.TrimSpace(*patch.LogLevel))
	}

	return e.saveAndApply(cfg)
}

// durationFromSeconds 将秒转换为 proxy.Duration。
func durationFromSeconds(seconds float64) proxy.Duration {
	if seconds <= 0 {
		return 0
	}
	return proxy.Duration(time.Duration(seconds * float64(time.Second)))
}
