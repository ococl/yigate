package proxy

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// deadFailureThreshold 是将上游标记为 dead 前所需的连续失败次数。
	deadFailureThreshold = 3
	// deadProbeBaseInterval 是 dead 上游恢复探测的初始间隔。
	deadProbeBaseInterval = 2 * time.Second
	// deadProbeMaxInterval 是 dead 上游恢复探测的最大间隔。
	deadProbeMaxInterval = 30 * time.Second
)

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
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Types    []string `yaml:"types"`
	Priority int      `yaml:"priority"`
	Weight   int      `yaml:"weight"`
}

// Upstream 表示一个已解析的上游代理
type Upstream struct {
	Name     string
	URL      *url.URL
	Types    []string
	Priority int
	Weight   int

	typesMu sync.RWMutex
	current int
	dead    bool

	// 失败计数与恢复探测相关字段，由 UpstreamManager.mu 保护。
	failures        int       // 连续失败次数
	deadSince       time.Time // 最近一次被标记 dead 的时间
	nextProbeAt     time.Time // 下次允许主动探测恢复的时间（指数退避）
	consecutiveDead int       // 连续处于 dead 的探测周期数（用于计算退避）
}

// UpstreamManager 管理多上游、优先级、负载均衡与健康检查
type UpstreamManager struct {
	upstreams     []*Upstream
	byName        map[string]*Upstream
	groups        map[int][]*Upstream
	order         []int
	mu            sync.Mutex
	done          chan struct{}
	wg            sync.WaitGroup
	checkInterval int
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
		byName: make(map[string]*Upstream),
		groups: make(map[int][]*Upstream),
	}
	for i, c := range configs {
		if c.URL == "" {
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
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", i+1)
		}
		if _, exists := m.byName[name]; exists {
			return nil, fmt.Errorf("duplicate upstream name %q", name)
		}
		types, _, err := normalizeProxyTypes(c.Types)
		if err != nil {
			return nil, fmt.Errorf("upstreams[%d].types: %w", i, err)
		}
		if len(types) == 0 {
			types = []string{normalizeProxyType(u.Scheme)}
		}
		up := &Upstream{
			Name:     name,
			URL:      u,
			Types:    types,
			Priority: c.Priority,
			Weight:   c.Weight,
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

// Select 按优先级选择可用上游，同优先级加权轮询
// 所有上游均不可用时返回 nil。
func (m *UpstreamManager) Select() *Upstream {
	return m.selectFrom(nil)
}

func (m *UpstreamManager) selectFrom(allowed map[string]struct{}) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !m.isSelectable(u) {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			alive = append(alive, u)
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
}

// selectFromExcluding 类似 selectFrom，但跳过 exclude 集合中的上游，
// 用于在单次 DialProxy 调用中避免重复选择已失败的上游。
func (m *UpstreamManager) selectFromExcluding(allowed map[string]struct{}, exclude map[*Upstream]struct{}) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !m.isSelectable(u) {
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
			alive = append(alive, u)
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
}

func (m *UpstreamManager) SelectFor(names []string) *Upstream {
	return m.selectFrom(namesToSet(names))
}

// SelectForExcluding 类似 SelectFor，但跳过 exclude 集合中的上游，
// 用于在重试循环中避免重复选择同一失败上游。
func (m *UpstreamManager) SelectForExcluding(names []string, exclude map[*Upstream]struct{}) *Upstream {
	return m.selectFromExcluding(namesToSet(names), exclude)
}

func (m *UpstreamManager) SelectSocks5For(names []string) *Upstream {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !m.isSelectable(u) || !u.supportsProxyType(ProxyTypeSocks5) {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			alive = append(alive, u)
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
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

// isSelectable 判断上游是否可被选中用于本次拨号尝试。
// 未标记 dead 的始终可选；已 dead 但退避期已过的也允许被选中，
// 以便通过一次真实请求探测其恢复（自愈）。这样即使关闭主动健康检查
// （check_interval=0），基于实时拨号失败的故障转移与恢复仍然有效。
func (m *UpstreamManager) isSelectable(up *Upstream) bool {
	if up == nil {
		return false
	}
	if !up.dead {
		return true
	}
	if up.nextProbeAt.IsZero() {
		return true
	}
	return time.Now().After(up.nextProbeAt)
}

// markDead 记录一次失败。连续失败达到阈值才标记 dead，
// 并按指数退避调度下次主动恢复探测。
func (m *UpstreamManager) markDead(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up.failures++
	if up.failures < deadFailureThreshold {
		// 未达阈值，暂不标记 dead，但记录下次可快速重试的时间。
		return
	}
	if !up.dead {
		up.dead = true
		up.deadSince = time.Now()
		up.consecutiveDead = 0
	}
	up.consecutiveDead++
	// 指数退避：base * 2^(n-1)，上限 maxInterval。
	backoff := deadProbeBaseInterval * time.Duration(1<<uint(up.consecutiveDead-1))
	if backoff > deadProbeMaxInterval {
		backoff = deadProbeMaxInterval
	}
	up.nextProbeAt = time.Now().Add(backoff)
}

// markDeadImmediate 立即将上游标记为 dead（跳过失败计数阈值）。
// 用于 BIND/UDP 等特殊命令——上游对这些命令的失败通常意味着不支持，
// 不应反复尝试导致请求挂起。
func (m *UpstreamManager) markDeadImmediate(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !up.dead {
		up.dead = true
		up.deadSince = time.Now()
		up.consecutiveDead = 0
	}
	up.failures = deadFailureThreshold
	up.consecutiveDead++
	backoff := deadProbeBaseInterval * time.Duration(1<<uint(up.consecutiveDead-1))
	if backoff > deadProbeMaxInterval {
		backoff = deadProbeMaxInterval
	}
	up.nextProbeAt = time.Now().Add(backoff)
}

// markAlive 重置失败计数并清除 dead 状态。
func (m *UpstreamManager) markAlive(up *Upstream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up.failures = 0
	up.dead = false
	up.consecutiveDead = 0
	up.nextProbeAt = time.Time{}
}
