package proxy

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// deadFailureThreshold 是主动探测连续失败多少次后摘除上游。
	deadFailureThreshold = 3
	// passiveFailureThreshold 是业务拨号连续失败多少次后摘除上游。
	// 业务失败与探测失败分开计数：真实业务流量里的失败可能只反映上游自身
	// 的下游链路问题（例如目标站点不可达），所以阈值远高于探测阈值，
	// 并且只统计"连不上 / 认证不上"这类节点级硬失败（见 IsNodeFailure）。
	passiveFailureThreshold = 10
	// probeFailureRetryInterval 是主动探测失败后的快速重试间隔上限。
	// 不等到下一个完整检查周期，才能在几十秒内完成阈值判定。
	probeFailureRetryInterval = 5 * time.Second
	// deadProbeBaseInterval 是 dead 上游恢复探测的初始间隔。
	deadProbeBaseInterval = 2 * time.Second
	// deadProbeMaxInterval 是 dead 上游恢复探测的最大间隔。
	deadProbeMaxInterval = 30 * time.Second
	// livenessProbeTarget 是浅探测使用的固定目标：本机 1 端口必然被拒绝，
	// 既能快速拿到上游的协议应答，又不会让上游访问自身形成自环。
	livenessProbeTarget = "127.0.0.1:1"
)

const (
	StrategyRoundRobin       = "weighted_round_robin"
	StrategyLeastConnections = "least_connections"
	StrategyConsistentHash   = "consistent_hash"
	StrategySourceIPHash     = "source_ip_hash"
)

func normalizeStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case StrategyLeastConnections:
		return StrategyLeastConnections
	case StrategyConsistentHash:
		return StrategyConsistentHash
	case StrategySourceIPHash:
		return StrategySourceIPHash
	default:
		return StrategyRoundRobin
	}
}

func validateStrategy(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", StrategyRoundRobin, StrategyLeastConnections, StrategyConsistentHash, StrategySourceIPHash:
		return nil
	default:
		return fmt.Errorf("unsupported strategy %q", value)
	}
}

// ValidateUpstreamStrategy 校验上游负载策略取值。
func ValidateUpstreamStrategy(value string) error {
	return validateStrategy(value)
}

// HealthCheckConfig 单个上游的健康检查配置。
// 未显式配置的字段继承全局默认值。
type HealthCheckConfig struct {
	Type string `yaml:"type"`
	// Target 是深度探测目标（host:port）。为空时只做协议层存活探测：
	// 上游能按协议正确应答即视为可用，不依赖外网、也不访问上游自身。
	// 配置后按代理真实能力探测一次 CONNECT（http/https/socks5 通用）。
	Target string `yaml:"target,omitempty"`
	// Path 仅用于深度探测：非空时对 http/https 上游发起 absolute-URI GET，
	// 为空时发起 CONNECT。浅探测忽略该字段。
	Path             string   `yaml:"path,omitempty"`
	ExpectedCodes    []int    `yaml:"expected_codes"`
	Interval         Duration `yaml:"interval"`
	Timeout          Duration `yaml:"timeout"`
	FailureThreshold int      `yaml:"failure_threshold"`
	SuccessThreshold int      `yaml:"success_threshold"`
}

// normalizeHealthCheckType 根据上游类型推断默认健康检查类型。
// socks5/socks5h 上游默认执行真实协议握手探测而非裸 TCP。
func normalizeHealthCheckType(value string, proxyType string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tcp", "http", "https", "tls", ProxyTypeSocks5, ProxyTypeSocks5H:
		return strings.ToLower(strings.TrimSpace(value))
	case "":
		switch proxyType {
		case ProxyTypeHTTP, ProxyTypeHTTPS:
			return "http"
		case ProxyTypeSocks5, ProxyTypeSocks5H:
			return ProxyTypeSocks5
		}
		return "tcp"
	default:
		return "tcp"
	}
}

func validateHealthCheckType(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "tcp", "http", "https", "tls", ProxyTypeSocks5, ProxyTypeSocks5H:
		return nil
	default:
		return fmt.Errorf("unsupported health check type %q", value)
	}
}

// ValidateHealthCheckConfig 校验健康检查配置的边界合法性。
func ValidateHealthCheckConfig(hc HealthCheckConfig) error {
	if err := validateHealthCheckType(hc.Type); err != nil {
		return err
	}
	if hc.Interval < 0 {
		return fmt.Errorf("health_check.interval must be non-negative")
	}
	if hc.Timeout < 0 {
		return fmt.Errorf("health_check.timeout must be non-negative")
	}
	if hc.FailureThreshold < 0 {
		return fmt.Errorf("health_check.failure_threshold must be non-negative")
	}
	if hc.SuccessThreshold < 0 {
		return fmt.Errorf("health_check.success_threshold must be non-negative")
	}
	t := strings.ToLower(strings.TrimSpace(hc.Type))
	if t == "http" || t == "https" {
		if hc.Path != "" && !strings.HasPrefix(hc.Path, "/") {
			return fmt.Errorf("health_check.path must start with /")
		}
	}
	if hc.Target != "" {
		host, port, err := net.SplitHostPort(strings.TrimSpace(hc.Target))
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("health_check.target must be host:port")
		}
	}
	for _, code := range hc.ExpectedCodes {
		if code < 100 || code >= 600 {
			return fmt.Errorf("health_check.expected_codes contains invalid status code %d", code)
		}
	}
	return nil
}

const (
	ProxyTypeHTTP    = "http"
	ProxyTypeHTTPS   = "https"
	ProxyTypeSocks5  = "socks5"
	ProxyTypeSocks5H = "socks5h"
)

func normalizeProxyTypes(values []string) ([]string, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	seen := make(map[string]struct{}, len(values))
	types := make([]string, 0, len(values))
	for _, value := range values {
		typ := normalizeProxyType(value)
		if typ == "" {
			return nil, true, fmt.Errorf("unsupported proxy type %q", value)
		}
		if _, ok := seen[typ]; ok {
			continue
		}
		seen[typ] = struct{}{}
		types = append(types, typ)
	}
	return types, true, nil
}

func normalizeProxyType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProxyTypeHTTP:
		return ProxyTypeHTTP
	case ProxyTypeHTTPS:
		return ProxyTypeHTTPS
	case ProxyTypeSocks5:
		return ProxyTypeSocks5
	case ProxyTypeSocks5H:
		return ProxyTypeSocks5H
	default:
		return ""
	}
}

func (u *Upstream) proxyTypes() []string {
	if u == nil {
		return nil
	}
	u.typesMu.RLock()
	defer u.typesMu.RUnlock()
	return append([]string(nil), u.Types...)
}

func (u *Upstream) supportsProxyType(typ string) bool {
	if u == nil {
		return false
	}
	typ = normalizeProxyType(typ)
	if typ == "" {
		return false
	}
	u.typesMu.RLock()
	defer u.typesMu.RUnlock()
	for _, candidate := range u.Types {
		if candidate == typ || (typ == ProxyTypeSocks5 && candidate == ProxyTypeSocks5H) {
			return true
		}
	}
	return false
}

func (u *Upstream) primaryProxyType() string {
	if u == nil {
		return ""
	}
	types := u.proxyTypes()
	if len(types) > 0 {
		return types[0]
	}
	if u.URL != nil {
		return normalizeProxyType(u.URL.Scheme)
	}
	return ""
}

func (u *Upstream) urlForType(typ string) *url.URL {
	if u == nil || u.URL == nil {
		return nil
	}
	proxyURL := *u.URL
	if typ = normalizeProxyType(typ); typ != "" {
		proxyURL.Scheme = typ
	}
	return &proxyURL
}

func (u *Upstream) withProxyType(typ string) *Upstream {
	proxyURL := u.urlForType(typ)
	if proxyURL == nil {
		return u
	}
	return &Upstream{Name: u.Name, URL: proxyURL}
}

func ValidateProxyTypes(values []string) error {
	_, _, err := normalizeProxyTypes(values)
	return err
}

// UpstreamConfig 单个上游代理配置
type UpstreamConfig struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	// Username/Password 是上游代理的独立认证凭据。
	// 与 URL 内嵌的 userinfo 二选一：配置了独立凭据时会覆盖 URL 中的 userinfo。
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	// Enabled 控制该上游是否参与流量调度与健康检查。
	// nil 表示未配置，按启用处理，兼容旧配置文件。
	Enabled     *bool             `yaml:"enabled,omitempty"`
	Types       []string          `yaml:"types"`
	Priority    int               `yaml:"priority"`
	Weight      int               `yaml:"weight"`
	Strategy    string            `yaml:"strategy"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
}

// HasCredentials 返回是否配置了独立的上游认证凭据。
func (c UpstreamConfig) HasCredentials() bool {
	return c.Username != "" || c.Password != ""
}

// IsEnabled 返回上游是否启用。未显式配置 enabled 时默认启用。
func (c UpstreamConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// Upstream 表示一个已解析的上游代理
type Upstream struct {
	Name        string
	URL         *url.URL
	Types       []string
	Priority    int
	Weight      int
	Strategy    string
	HealthCheck HealthCheckConfig

	typesMu sync.RWMutex
	current int
	dead    bool

	// 失败计数与恢复探测相关字段，由 UpstreamManager.mu 保护。
	failures        int       // 主动探测连续失败次数（达到 health_check.failure_threshold 摘除）
	passiveFailures int       // 业务拨号连续硬失败次数（达到 passiveFailureThreshold 摘除）
	deadSince       time.Time // 最近一次被标记 dead 的时间
	nextProbeAt     time.Time // 下次允许主动探测恢复的时间（指数退避）
	nextCheckAt     time.Time // 下次健康检查时间（用于 per-upstream interval）
	lastCheckAt     time.Time // 最近一次健康检查完成的时间（内存态，用于展示）
	consecutiveDead int       // 连续处于 dead 的探测周期数（用于计算退避）
	probeGeneration uint64    // 当前探测 generation
	probeInFlight   bool      // 是否已有探测正在执行
	successes       int       // 连续成功次数

	// 能力状态：true 表示上游不支持该能力（如 BIND、UDP ASSOCIATE）。
	// 由 UpstreamManager.mu 保护。默认 false（未知但允许尝试）。
	bindUnsupported bool
	udpUnsupported  bool

	// activeConns 用于 least_connections 策略的当前活动连接计数。
	activeConns atomic.Int64
}

// countedConn 包装 net.Conn，在关闭时回调减少活动连接计数。
type countedConn struct {
	net.Conn
	onClose func()
	once    sync.Once
}

func (c *countedConn) Close() error {
	if c.onClose != nil {
		c.once.Do(c.onClose)
	}
	return c.Conn.Close()
}

// trackConn 增加上游活动连接计数并返回包装后的连接。
func (u *Upstream) trackConn(conn net.Conn) net.Conn {
	if conn == nil || u == nil {
		return conn
	}
	u.activeConns.Add(1)
	return &countedConn{
		Conn: conn,
		onClose: func() {
			u.activeConns.Add(-1)
		},
	}
}

// ActiveConns 返回上游当前活动连接数（用于测试与指标）。
func (u *Upstream) ActiveConns() int64 {
	if u == nil {
		return 0
	}
	return u.activeConns.Load()
}

// UpstreamManager 管理多上游、优先级、负载均衡与健康检查
type UpstreamManager struct {
	upstreams     []*Upstream
	byName        map[string]*Upstream
	checkSF       singleflight.Group
	groups        map[int][]*Upstream
	order         []int
	mu            sync.Mutex
	done          chan struct{}
	wg            sync.WaitGroup
	checkInterval int
	// probeSem 统一限制主动探测的并发度：周期恢复探测与手动全量巡检共用，
	// 避免上游较多时一轮探测瞬间打出大量连接、与业务流量争抢 fd。
	probeSem chan struct{}
}

// redactURLString 返回去除用户凭证（userinfo）的 URL 字符串，用于日志与错误信息，
// 避免把代理账号密码泄露到日志文件。
func redactURLString(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<redacted>"
	}
	return u.Redacted()
}

// NewUpstreamManager 根据配置创建上游管理器
func NewUpstreamManager(configs []UpstreamConfig) (*UpstreamManager, error) {
	m := &UpstreamManager{
		byName:   make(map[string]*Upstream),
		groups:   make(map[int][]*Upstream),
		probeSem: make(chan struct{}, maxConcurrentProbes),
	}
	for i, c := range configs {
		if c.URL == "" {
			continue
		}
		// 已禁用的上游不会进入调度池，也不参与健康检查；
		// 配置本身仍保留，便于通过 WebUI 开关随时恢复。
		if !c.IsEnabled() {
			continue
		}
		// config.go 已用严格的环境展开处理过 URL（缺失/空值会启动失败），
		// 这里直接使用，避免二次非严格展开把字面量 ${...} 静默清空成非法 URL。
		u, err := url.Parse(c.URL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream %q: %w", redactURLString(c.URL), err)
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
			return nil, fmt.Errorf("unsupported upstream scheme %q in %q", u.Scheme, redactURLString(c.URL))
		}
		if u.Host == "" {
			return nil, fmt.Errorf("upstream host missing in %q", redactURLString(c.URL))
		}
		// 独立凭据统一注入 URL 的 userinfo，后续 SOCKS5/HTTP/健康检查
		// 三条链路都从 u.URL.User 读取，无需各自处理两套来源。
		if c.HasCredentials() {
			u.User = url.UserPassword(c.Username, c.Password)
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", i+1)
		}
		if _, exists := m.byName[name]; exists {
			return nil, fmt.Errorf("duplicate upstream name %q", name)
		}
		if err := ValidateHealthCheckConfig(c.HealthCheck); err != nil {
			return nil, fmt.Errorf("upstreams[%d].health_check: %w", i, err)
		}
		types, _, err := normalizeProxyTypes(c.Types)
		if err != nil {
			return nil, fmt.Errorf("upstreams[%d].types: %w", i, err)
		}
		if err := validateStrategy(c.Strategy); err != nil {
			return nil, fmt.Errorf("upstreams[%d].strategy: %w", i, err)
		}
		if len(types) == 0 {
			types = []string{normalizeProxyType(u.Scheme)}
		}
		primaryType := normalizeProxyType(u.Scheme)
		hc := c.HealthCheck
		hc.Type = normalizeHealthCheckType(hc.Type, primaryType)
		if hc.Path == "" {
			hc.Path = "/"
		}
		if len(hc.ExpectedCodes) == 0 {
			if hc.Type == "http" || hc.Type == "https" {
				hc.ExpectedCodes = []int{200}
			}
		}
		if hc.Timeout <= 0 {
			hc.Timeout = Duration(5 * time.Second)
		}
		if hc.FailureThreshold <= 0 {
			hc.FailureThreshold = deadFailureThreshold
		}
		up := &Upstream{
			Name:        name,
			URL:         u,
			Types:       types,
			Priority:    c.Priority,
			Weight:      c.Weight,
			Strategy:    normalizeStrategy(c.Strategy),
			HealthCheck: hc,
		}
		if up.Weight <= 0 {
			up.Weight = 1
		}
		m.upstreams = append(m.upstreams, up)
		m.byName[name] = up
		m.groups[c.Priority] = append(m.groups[c.Priority], up)
	}
	for p := range m.groups {
		m.order = append(m.order, p)
	}
	sort.Ints(m.order)
	return m, nil
}

func selectKey(strategy, target, sourceIP string) string {
	switch strategy {
	case StrategySourceIPHash:
		return sourceIP
	case StrategyConsistentHash:
		return target
	default:
		return target
	}
}

// Select 按优先级选择可用上游，同优先级按配置策略选择。
// 所有上游均不可用时返回 nil。
func (m *UpstreamManager) Select() *Upstream {
	return m.selectFrom(nil, "", "")
}

func (m *UpstreamManager) selectFrom(allowed map[string]struct{}, target, sourceIP string) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectLocked(allowed, nil, target, sourceIP)
}

// selectFromExcluding 类似 selectFrom，但跳过 exclude 集合中的上游，
// 用于在单次 DialProxy 调用中避免重复选择已失败的上游。
func (m *UpstreamManager) selectFromExcluding(allowed map[string]struct{}, exclude map[*Upstream]struct{}, target, sourceIP string) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectLocked(allowed, exclude, target, sourceIP)
}

// selectLocked 是上游选择的核心实现，调用方需持有 m.mu。
//
// 优先在存活节点中按优先级与策略选择；没有任何存活节点时退化为
// half-open 兜底，仍按优先级尝试被摘除的节点。理由：熔断状态只是
// 推断结果，宁可让请求失败在真实的连接错误上，也不要因为状态推断
// 导致 100% 失败；且兜底尝试成功后可以立即通过 markAlive 恢复节点，
// 这样即便关闭了主动健康检查也不会出现"永久摘除"。
func (m *UpstreamManager) selectLocked(allowed map[string]struct{}, exclude map[*Upstream]struct{}, target, sourceIP string) *Upstream {
	if up := m.pickLocked(allowed, exclude, target, sourceIP, false); up != nil {
		return up
	}
	return m.pickLocked(allowed, exclude, target, sourceIP, true)
}

// pickLocked 在候选集中按优先级分组选择上游；includeDead 为 true 时
// 允许选中已摘除节点（half-open 兜底）。
func (m *UpstreamManager) pickLocked(allowed map[string]struct{}, exclude map[*Upstream]struct{}, target, sourceIP string, includeDead bool) *Upstream {
	for _, p := range m.order {
		group := m.groups[p]
		candidates := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !includeDead && !m.isSelectable(u) {
				continue
			}
			if exclude != nil {
				if _, ok := exclude[u]; ok {
					continue
				}
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			candidates = append(candidates, u)
		}
		if len(candidates) == 0 {
			continue
		}
		strategy := groupStrategy(candidates)
		return pick(candidates, selectKey(strategy, target, sourceIP))
	}
	return nil
}

func (m *UpstreamManager) SelectFor(names []string) *Upstream {
	return m.selectFrom(namesToSet(names), "", "")
}

// SelectForExcluding 类似 SelectFor，但跳过 exclude 集合中的上游，
// 用于在重试循环中避免重复选择同一失败上游。
func (m *UpstreamManager) SelectForExcluding(names []string, exclude map[*Upstream]struct{}) *Upstream {
	return m.selectFromExcluding(namesToSet(names), exclude, "", "")
}

func (m *UpstreamManager) SelectSocks5For(names []string) *Upstream {
	return m.selectSocks5Internal(names, "", false, false)
}

// SelectSocks5ForWithSourceIP 类似 SelectSocks5For，但支持按源 IP 会话保持。
func (m *UpstreamManager) SelectSocks5ForWithSourceIP(names []string, sourceIP string) *Upstream {
	return m.selectSocks5Internal(names, sourceIP, false, false)
}

// SelectSocks5ForBind 选择支持 BIND 能力的 SOCKS5 上游。
func (m *UpstreamManager) SelectSocks5ForBind(names []string) *Upstream {
	return m.selectSocks5Internal(names, "", true, false)
}

// SelectSocks5ForBindWithSourceIP 选择支持 BIND 能力的 SOCKS5 上游，按源 IP 会话保持。
func (m *UpstreamManager) SelectSocks5ForBindWithSourceIP(names []string, sourceIP string) *Upstream {
	return m.selectSocks5Internal(names, sourceIP, true, false)
}

// SelectSocks5ForUDP 选择支持 UDP ASSOCIATE 能力的 SOCKS5 上游。
func (m *UpstreamManager) SelectSocks5ForUDP(names []string) *Upstream {
	return m.selectSocks5Internal(names, "", false, true)
}

// SelectSocks5ForUDPWithSourceIP 选择支持 UDP ASSOCIATE 能力的 SOCKS5 上游，按源 IP 会话保持。
func (m *UpstreamManager) SelectSocks5ForUDPWithSourceIP(names []string, sourceIP string) *Upstream {
	return m.selectSocks5Internal(names, sourceIP, false, true)
}

// selectSocks5Internal 统一的 SOCKS5 上游选择逻辑。
// requireBind / requireUDP 为 true 时过滤掉已知不支持该能力的上游。
func (m *UpstreamManager) selectSocks5Internal(names []string, sourceIP string, requireBind, requireUDP bool) *Upstream {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	// 与 selectLocked 一致：没有存活节点时退化为 half-open 兜底，
	// 避免全部摘除后出现 100% 失败且无法自愈。
	if up := m.pickSocks5Locked(allowed, sourceIP, requireBind, requireUDP, false); up != nil {
		return up
	}
	return m.pickSocks5Locked(allowed, sourceIP, requireBind, requireUDP, true)
}

func (m *UpstreamManager) pickSocks5Locked(allowed map[string]struct{}, sourceIP string, requireBind, requireUDP, includeDead bool) *Upstream {
	for _, p := range m.order {
		group := m.groups[p]
		candidates := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !u.supportsProxyType(ProxyTypeSocks5) {
				continue
			}
			if !includeDead && !m.isSelectable(u) {
				continue
			}
			if requireBind && u.bindUnsupported {
				continue
			}
			if requireUDP && u.udpUnsupported {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			candidates = append(candidates, u)
		}
		if len(candidates) == 0 {
			continue
		}
		strategy := groupStrategy(candidates)
		return pick(candidates, selectKey(strategy, "", sourceIP))
	}
	return nil
}

// markCapabilityUnsupported 标记上游不支持指定能力。
func (m *UpstreamManager) markCapabilityUnsupported(up *Upstream, capability string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil {
		return
	}
	switch capability {
	case "bind":
		up.bindUnsupported = true
	case "udp_associate":
		up.udpUnsupported = true
	}
}

// markCapabilitySupported 标记上游支持指定能力（成功后重置）。
func (m *UpstreamManager) markCapabilitySupported(up *Upstream, capability string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil {
		return
	}
	switch capability {
	case "bind":
		up.bindUnsupported = false
	case "udp_associate":
		up.udpUnsupported = false
	}
}

// identity 返回上游的身份标识，用于热加载时判断节点是否变化。
// 标识包含名称、标准化 URL（scheme+host+port+userinfo）和协议类型。
func (u *Upstream) identity() string {
	if u == nil {
		return ""
	}
	urlStr := ""
	if u.URL != nil {
		urlStr = u.URL.Scheme + "://" + u.URL.Host
		if u.URL.User != nil {
			urlStr += "@" + u.URL.User.String()
		}
	}
	types := ""
	u.typesMu.RLock()
	for _, t := range u.Types {
		types += t + ","
	}
	u.typesMu.RUnlock()
	return u.Name + "|" + urlStr + "|" + types
}

// MergeRuntimeState 从旧 manager 复制匹配上游的运行时状态。
// 身份标识（名称+URL+协议类型）相同的上游视为同一节点，保留其
// dead 状态、失败计数、退避调度、能力状态和活动连接计数。
// 仅修改 newMgr 中的上游；oldMgr 不被修改。
func (m *UpstreamManager) MergeRuntimeState(oldMgr *UpstreamManager) {
	if oldMgr == nil {
		return
	}
	oldMgr.mu.Lock()
	defer oldMgr.mu.Unlock()
	oldMap := make(map[string]*Upstream, len(oldMgr.upstreams))
	for _, u := range oldMgr.upstreams {
		oldMap[u.identity()] = u
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, nu := range m.upstreams {
		ou, ok := oldMap[nu.identity()]
		if !ok {
			continue
		}
		nu.dead = ou.dead
		nu.failures = ou.failures
		nu.passiveFailures = ou.passiveFailures
		nu.deadSince = ou.deadSince
		nu.nextProbeAt = ou.nextProbeAt
		nu.nextCheckAt = now // 立即触发一次检查
		nu.lastCheckAt = ou.lastCheckAt
		nu.consecutiveDead = ou.consecutiveDead
		nu.successes = ou.successes
		nu.bindUnsupported = ou.bindUnsupported
		nu.udpUnsupported = ou.udpUnsupported
		nu.activeConns.Store(ou.activeConns.Load())
	}
}

// UpstreamStatuses 返回所有上游节点的运行时状态快照。
func (m *UpstreamManager) UpstreamStatuses() []UpstreamStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]UpstreamStatus, 0, len(m.upstreams))
	for _, u := range m.upstreams {
		urlStr := ""
		if u.URL != nil {
			urlStr = u.URL.Scheme + "://" + u.URL.Host
		}
		u.typesMu.RLock()
		types := append([]string(nil), u.Types...)
		u.typesMu.RUnlock()
		result = append(result, UpstreamStatus{
			Name:            u.Name,
			URL:             urlStr,
			Types:           types,
			Dead:            u.dead,
			DeadSince:       u.deadSince,
			Failures:        u.failures,
			PassiveFailures: u.passiveFailures,
			Successes:       u.successes,
			ConsecutiveDead: u.consecutiveDead,
			NextProbeAt:     u.nextProbeAt,
			NextCheckAt:     u.nextCheckAt,
			LastCheckAt:     u.lastCheckAt,
			BindUnsupported: u.bindUnsupported,
			UDPUnsupported:  u.udpUnsupported,
			ActiveConns:     u.activeConns.Load(),
		})
	}
	return result
}

// HasAliveUpstreams 返回是否存在至少一个非 dead 的上游。
func (m *UpstreamManager) HasAliveUpstreams() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.upstreams {
		if m.isSelectable(u) {
			return true
		}
	}
	return false
}

func (m *UpstreamManager) AttemptCount(names []string) int {
	if len(names) == 0 {
		return len(m.upstreams)
	}
	return len(names)
}

func (m *UpstreamManager) Socks5AttemptCount(names []string) int {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, up := range m.upstreams {
		if !up.supportsProxyType(ProxyTypeSocks5) {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[up.Name]; !ok {
				continue
			}
		}
		count++
	}
	return count
}

func (m *UpstreamManager) Socks5ByName(name string) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	up := m.byName[name]
	if up == nil || !m.isSelectable(up) || !up.supportsProxyType(ProxyTypeSocks5) {
		return nil
	}
	return up
}

func groupStrategy(us []*Upstream) string {
	if len(us) == 0 {
		return StrategyRoundRobin
	}
	return us[0].Strategy
}

// pick 按组策略选择上游。
func pick(us []*Upstream, key string) *Upstream {
	if len(us) == 0 {
		return nil
	}
	switch groupStrategy(us) {
	case StrategyLeastConnections:
		return pickLeastConnections(us)
	case StrategyConsistentHash, StrategySourceIPHash:
		return pickConsistentHash(us, key)
	default:
		return pickWeighted(us)
	}
}

func pickLeastConnections(us []*Upstream) *Upstream {
	var best *Upstream
	var bestCount int64 = -1
	for _, u := range us {
		n := u.activeConns.Load()
		if best == nil || n < bestCount {
			best = u
			bestCount = n
		}
	}
	return best
}

// consistentHashRingSize 是每个上游在哈希环上的虚拟节点数。
const consistentHashRingSize = 150

// pickConsistentHash 使用哈希环（虚拟节点）实现一致性哈希。
// 添加或移除上游时，只有部分 key 会重新映射，保证会话保持的稳定性。
func pickConsistentHash(us []*Upstream, key string) *Upstream {
	if key == "" {
		return pickWeighted(us)
	}
	if len(us) == 1 {
		return us[0]
	}

	// 构建哈希环：对每个上游创建多个虚拟节点
	type ringEntry struct {
		hash uint32
		idx  int
	}
	ring := make([]ringEntry, 0, len(us)*consistentHashRingSize)
	for i, u := range us {
		for j := 0; j < consistentHashRingSize; j++ {
			h := fnv.New32a()
			h.Write([]byte(u.Name))
			h.Write([]byte{byte(j >> 24), byte(j >> 16), byte(j >> 8), byte(j)})
			ring = append(ring, ringEntry{hash: h.Sum32(), idx: i})
		}
	}

	// 排序哈希环
	sort.Slice(ring, func(i, j int) bool {
		return ring[i].hash < ring[j].hash
	})

	// 计算key的哈希，在环上顺时针查找最近的虚拟节点
	h := fnv.New32a()
	h.Write([]byte(key))
	keyHash := h.Sum32()

	// 二分查找第一个 >= keyHash 的节点
	lo, hi := 0, len(ring)
	for lo < hi {
		mid := (lo + hi) / 2
		if ring[mid].hash < keyHash {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(ring) {
		lo = 0 // 环绕到环的起点
	}
	return us[ring[lo].idx]
}

// pickWeighted 平滑加权轮询
func pickWeighted(us []*Upstream) *Upstream {
	total := 0
	for _, u := range us {
		total += u.Weight
	}
	if total == 0 {
		return us[0]
	}

	chosen := 0
	max := -1
	for i, u := range us {
		u.current += u.Weight
		if u.current > max {
			max = u.current
			chosen = i
		}
	}

	us[chosen].current -= total
	return us[chosen]
}

// isSelectable 判断上游是否可被选中用于业务拨号。
// dead 上游不再参与常规调度，只在没有任何存活节点时作为 half-open 兜底被尝试。
func (m *UpstreamManager) isSelectable(up *Upstream) bool {
	return up != nil && !up.dead
}

// probeBackoff 返回 capped exponential backoff，避免大位移和 Duration 溢出。
// 结果附带 ±20% 抖动，避免多个 dead 节点在同一时刻同步探测。
func probeBackoff(consecutiveDead int) time.Duration {
	if consecutiveDead <= 1 {
		return withProbeJitter(deadProbeBaseInterval)
	}
	backoff := deadProbeBaseInterval
	for i := 1; i < consecutiveDead; i++ {
		if backoff >= deadProbeMaxInterval/2 {
			return withProbeJitter(deadProbeMaxInterval)
		}
		backoff *= 2
	}
	if backoff > deadProbeMaxInterval {
		backoff = deadProbeMaxInterval
	}
	return withProbeJitter(backoff)
}

func withProbeJitter(d time.Duration) time.Duration {
	delta := d / 5
	if delta <= 0 {
		return d
	}
	return d - delta + time.Duration(rand.Int64N(int64(2*delta)+1))
}

// markDeadLocked 将上游置为 dead 并安排恢复探测；调用方需持有 m.mu。
func (m *UpstreamManager) markDeadLocked(up *Upstream, now time.Time) {
	if up == nil {
		return
	}
	up.successes = 0
	up.passiveFailures = 0
	if !up.dead {
		up.dead = true
		up.deadSince = now
		up.consecutiveDead = 0
		up.probeGeneration++
		up.probeInFlight = false
		MetricInc("yigate_upstream_state_transitions_total", "upstream="+up.Name, "from=alive", "to=dead")
	}
	up.consecutiveDead++
	up.nextProbeAt = now.Add(probeBackoff(up.consecutiveDead))
}

// markProbeFailure 记录一次主动探测失败。连续失败达到阈值才摘除节点。
func (m *UpstreamManager) markProbeFailure(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil {
		return
	}
	up.successes = 0
	if up.dead {
		// 已摘除的节点由恢复探测器统一推进计数与退避。
		return
	}
	up.failures++
	if up.failures < up.HealthCheckFailureThreshold() {
		return
	}
	m.markDeadLocked(up, time.Now())
}

// markPassiveFailure 记录一次业务拨号失败。只有节点级硬失败才会计入
// （调用方用 IsNodeFailure 过滤），且阈值远高于主动探测，避免把零星
// 的目标不可达误判为上游故障。
func (m *UpstreamManager) markPassiveFailure(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil || up.dead {
		return
	}
	up.passiveFailures++
	if up.passiveFailures < passiveFailureThreshold {
		return
	}
	m.markDeadLocked(up, time.Now())
}

func (m *UpstreamManager) markAliveLocked(up *Upstream) bool {
	if up == nil {
		return false
	}
	up.failures = 0
	up.passiveFailures = 0
	up.successes++
	if up.successes < up.HealthCheckSuccessThreshold() {
		return false
	}
	wasDead := up.dead
	up.dead = false
	up.deadSince = time.Time{}
	up.successes = 0
	up.consecutiveDead = 0
	up.nextProbeAt = time.Time{}
	if wasDead {
		MetricInc("yigate_upstream_state_transitions_total", "upstream="+up.Name, "from=dead", "to=alive")
	}
	return wasDead
}

// markAlive 记录一次成功；达到恢复阈值后才重新进入业务池。
func (m *UpstreamManager) markAlive(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markAliveLocked(up)
}

// beginProbe 为到期的 open 上游获取单飞探测租约。
func (m *UpstreamManager) beginProbe(up *Upstream, now time.Time) (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil || !up.dead || up.probeInFlight || up.nextProbeAt.IsZero() || now.Before(up.nextProbeAt) {
		return 0, false
	}
	up.probeGeneration++
	up.probeInFlight = true
	return up.probeGeneration, true
}

// applyProbeResult 仅应用当前租约的结果，防止旧探测覆盖较新的状态。
func (m *UpstreamManager) applyProbeResult(up *Upstream, generation uint64, alive bool, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up == nil || !up.probeInFlight || up.probeGeneration != generation {
		return false
	}
	up.probeInFlight = false
	if alive {
		MetricInc("yigate_upstream_probe_total", "upstream="+up.Name, "result=success")
		return m.markAliveLocked(up)
	}
	MetricInc("yigate_upstream_probe_total", "upstream="+up.Name, "result=failure")
	up.successes = 0
	up.consecutiveDead++
	up.nextProbeAt = now.Add(probeBackoff(up.consecutiveDead))
	return false
}

// HealthCheckType 返回上游的健康检查类型。
func (u *Upstream) HealthCheckType() string {
	if u == nil || u.HealthCheck.Type == "" {
		return "tcp"
	}
	return u.HealthCheck.Type
}

// HealthCheckTimeout 返回上游健康检查超时。
func (u *Upstream) HealthCheckTimeout() time.Duration {
	if u == nil || u.HealthCheck.Timeout <= 0 {
		return 5 * time.Second
	}
	return time.Duration(u.HealthCheck.Timeout)
}

// HealthCheckTarget 返回深度探测目标（host:port）。
// 为空表示只做协议层存活探测，不访问任何外部目标。
func (u *Upstream) HealthCheckTarget() string {
	if u == nil {
		return ""
	}
	return strings.TrimSpace(u.HealthCheck.Target)
}

// HealthCheckPath 返回深度探测的 HTTP 路径。
// 为空表示使用 CONNECT 探测；非空表示对 target 发起 absolute-URI GET。
func (u *Upstream) HealthCheckPath() string {
	if u == nil {
		return ""
	}
	return strings.TrimSpace(u.HealthCheck.Path)
}

// HealthCheckExpectedCodes 返回显式配置的预期响应码；未配置时返回 nil，
// 由探测逻辑按"存活探测"或"深度探测"各自的默认语义判定。
func (u *Upstream) HealthCheckExpectedCodes() []int {
	if u == nil || len(u.HealthCheck.ExpectedCodes) == 0 {
		return nil
	}
	return u.HealthCheck.ExpectedCodes
}

// HealthCheckInterval 返回上游健康检查间隔，0 表示使用全局间隔。
func (u *Upstream) HealthCheckInterval() time.Duration {
	if u == nil || u.HealthCheck.Interval <= 0 {
		return 0
	}
	return time.Duration(u.HealthCheck.Interval)
}

// HealthCheckFailureThreshold 返回上游健康检查失败阈值。
func (u *Upstream) HealthCheckFailureThreshold() int {
	if u == nil || u.HealthCheck.FailureThreshold <= 0 {
		return deadFailureThreshold
	}
	return u.HealthCheck.FailureThreshold
}

// HealthCheckSuccessThreshold 返回 open 上游恢复所需的连续成功次数。
func (u *Upstream) HealthCheckSuccessThreshold() int {
	if u == nil || u.HealthCheck.SuccessThreshold <= 0 {
		return 1
	}
	return u.HealthCheck.SuccessThreshold
}

func (m *UpstreamManager) SelectForWithSourceIP(names []string, sourceIP string) *Upstream {
	return m.selectFrom(namesToSet(names), "", sourceIP)
}

// SelectForExcludingWithSourceIP 类似 SelectForExcluding，但支持按源 IP 会话保持。
func (m *UpstreamManager) SelectForExcludingWithSourceIP(names []string, exclude map[*Upstream]struct{}, sourceIP string) *Upstream {
	return m.selectFromExcluding(namesToSet(names), exclude, "", sourceIP)
}

// DialWithSourceIP 尝试通过上游建立连接，支持源 IP 会话保持。
func (m *UpstreamManager) DialWithSourceIP(target, sourceIP string) (net.Conn, error) {
	conn, _, err := m.DialProxyWithSourceIP(target, sourceIP, nil)
	if err == nil {
		return conn, nil
	}
	return DialTarget(target)
}

// clientSourceIP 从连接地址中提取客户端源 IP。
func clientSourceIP(conn net.Conn) string {
	if conn == nil {
		return ""
	}
	addr := conn.RemoteAddr()
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		// 可能不是 host:port 格式，直接返回字符串
		return addr.String()
	}
	return host
}
