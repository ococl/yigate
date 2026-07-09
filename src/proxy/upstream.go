package proxy

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
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
		expandedURL := os.ExpandEnv(c.URL)
		u, err := url.Parse(expandedURL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream %q: %w", c.URL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
			return nil, fmt.Errorf("unsupported upstream scheme %q in %q", u.Scheme, c.URL)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("upstream host missing in %q", c.URL)
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
			if u.dead {
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

func (m *UpstreamManager) SelectFor(names []string) *Upstream {
	return m.selectFrom(namesToSet(names))
}

func (m *UpstreamManager) SelectSocks5For(names []string) *Upstream {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if u.dead || !u.supportsProxyType(ProxyTypeSocks5) {
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
	if up == nil || up.dead || !up.supportsProxyType(ProxyTypeSocks5) {
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

func (m *UpstreamManager) markDead(up *Upstream) {
	m.mu.Lock()
	if m.checkInterval <= 0 {
		m.mu.Unlock()
		return
	}
	up.dead = true
	m.mu.Unlock()
}
