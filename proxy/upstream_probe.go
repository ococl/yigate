package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultUpstreamProbeDelay     = 30 * time.Second
	defaultUpstreamProbeCachePath = "upstream-types.txt"
	upstreamProbeTarget           = "example.com:80"
)

type UpstreamProbeConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Delay     Duration `yaml:"delay"`
	CachePath string   `yaml:"cache_path"`
}

type UpstreamTypeProber struct {
	cfg     UpstreamProbeConfig
	mgr     *UpstreamManager
	done    chan struct{}
	started sync.Once
	stopped sync.Once
}

type upstreamTypeCacheEntry struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Types  []string `json:"types"`
	Source string   `json:"source"`
}

func NormalizeUpstreamProbeConfig(cfg *UpstreamProbeConfig) {
	if cfg == nil {
		return
	}
	if cfg.Delay == 0 {
		cfg.Delay = Duration(defaultUpstreamProbeDelay)
	}
	if strings.TrimSpace(cfg.CachePath) == "" {
		cfg.CachePath = defaultUpstreamProbeCachePath
	}
}

func ResolveUpstreamProbePath(cfg *UpstreamProbeConfig, baseDir string) {
	if cfg == nil || cfg.CachePath == "" || filepath.IsAbs(cfg.CachePath) {
		return
	}
	cfg.CachePath = filepath.Join(baseDir, cfg.CachePath)
}

func NewUpstreamTypeProber(cfg UpstreamProbeConfig, mgr *UpstreamManager) *UpstreamTypeProber {
	NormalizeUpstreamProbeConfig(&cfg)
	return &UpstreamTypeProber{cfg: cfg, mgr: mgr, done: make(chan struct{})}
}

func (p *UpstreamTypeProber) Start() {
	if p == nil || !p.cfg.Enabled || p.mgr == nil {
		return
	}
	p.started.Do(func() {
		go p.loop()
	})
}

func (p *UpstreamTypeProber) Stop() {
	if p == nil || p.done == nil {
		return
	}
	p.stopped.Do(func() { close(p.done) })
}

func (p *UpstreamTypeProber) loop() {
	if p.loadCache() {
		return
	}
	timer := time.NewTimer(p.cfg.Delay.value(defaultUpstreamProbeDelay))
	defer timer.Stop()
	select {
	case <-p.done:
		return
	case <-timer.C:
	}
	p.probeAndSave()
}

func (p *UpstreamTypeProber) loadCache() bool {
	if strings.TrimSpace(p.cfg.CachePath) == "" {
		return false
	}
	data, err := os.ReadFile(p.cfg.CachePath)
	if err != nil {
		return false
	}
	entries, err := parseUpstreamTypeCache(data)
	if err != nil {
		return false
	}
	applied := p.mgr.applyCachedTypes(entries)
	return applied > 0 && p.mgr.allProbeTargetsResolved()
}

func parseUpstreamTypeCache(data []byte) ([]upstreamTypeCacheEntry, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var entries []upstreamTypeCacheEntry
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, err
		}
		return normalizeCacheEntries(entries), nil
	}
	return parseLegacyUpstreamTypeCache(trimmed), nil
}

func parseLegacyUpstreamTypeCache(data string) []upstreamTypeCacheEntry {
	var entries []upstreamTypeCacheEntry
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		entry := upstreamTypeCacheEntry{Name: strings.TrimSpace(parts[0])}
		for _, value := range strings.Split(parts[1], ",") {
			if typ := normalizeProxyType(value); typ != "" {
				entry.Types = append(entry.Types, typ)
			}
		}
		entries = append(entries, entry)
	}
	return normalizeCacheEntries(entries)
}

func normalizeCacheEntries(entries []upstreamTypeCacheEntry) []upstreamTypeCacheEntry {
	out := make([]upstreamTypeCacheEntry, 0, len(entries))
	for _, entry := range entries {
		types, _, err := normalizeProxyTypes(entry.Types)
		if err != nil || len(types) == 0 || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		entry.Name = strings.TrimSpace(entry.Name)
		entry.URL = strings.TrimSpace(entry.URL)
		entry.Source = strings.TrimSpace(entry.Source)
		entry.Types = types
		out = append(out, entry)
	}
	return out
}

func (m *UpstreamManager) applyCachedTypes(entries []upstreamTypeCacheEntry) int {
	if m == nil || len(entries) == 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	applied := 0
	for _, entry := range entries {
		up := m.byName[entry.Name]
		if up == nil || up.ExplicitTypes {
			continue
		}
		if entry.URL != "" && up.URL.String() != entry.URL {
			continue
		}
		if entry.Source != "" && entry.Source != "probe" && entry.Source != "cache" {
			continue
		}
		up.setCachedTypes(entry.Types)
		applied++
	}
	return applied
}

func (m *UpstreamManager) allProbeTargetsResolved() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, up := range m.upstreams {
		if !up.ExplicitTypes && up.typeSource() == "url" {
			return false
		}
	}
	return true
}

func (p *UpstreamTypeProber) probeAndSave() {
	entries := p.probeAll()
	if len(entries) == 0 || strings.TrimSpace(p.cfg.CachePath) == "" {
		return
	}
	if err := writeUpstreamTypeCache(p.cfg.CachePath, entries); err != nil {
		fmt.Printf("write upstream type cache: %v\n", err)
	}
}

func (p *UpstreamTypeProber) probeAll() []upstreamTypeCacheEntry {
	ups := p.mgr.snapshotUpstreams()
	entries := make([]upstreamTypeCacheEntry, 0, len(ups))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, up := range ups {
		up := up
		wg.Add(1)
		go func() {
			defer wg.Done()
			types, source := up.proxyTypes(), up.typeSource()
			if !up.ExplicitTypes && source == "url" {
				types = probeUpstreamTypes(up)
				if len(types) > 0 {
					up.setDetectedTypes(types)
					source = "probe"
				} else {
					types = up.proxyTypes()
				}
			}
			mu.Lock()
			entries = append(entries, upstreamTypeCacheEntry{Name: up.Name, URL: up.URL.String(), Types: types, Source: source})
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (m *UpstreamManager) snapshotUpstreams() []*Upstream {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Upstream(nil), m.upstreams...)
}

func probeUpstreamTypes(up *Upstream) []string {
	var types []string
	if up == nil || up.URL == nil {
		return nil
	}
	if probeHTTPType(up.URL, false) {
		types = append(types, ProxyTypeHTTP)
	}
	if probeHTTPType(up.URL, true) {
		types = append(types, ProxyTypeHTTPS)
	}
	if probeSOCKS5Type(up) {
		types = append(types, ProxyTypeSocks5, ProxyTypeSocks5H)
	}
	if len(types) == 0 {
		types = up.proxyTypes()
	}
	return types
}

func probeHTTPType(proxyURL *url.URL, tlsProxy bool) bool {
	if proxyURL == nil {
		return false
	}
	probeURL := *proxyURL
	if tlsProxy {
		probeURL.Scheme = ProxyTypeHTTPS
	} else {
		probeURL.Scheme = ProxyTypeHTTP
	}
	conn, err := dialProxyConn(&probeURL, proxyDialTimeout)
	if err != nil {
		return false
	}
	if err := setConnDeadline(conn, proxyHandshakeTimeout); err != nil {
		conn.Close()
		return false
	}
	defer conn.Close()
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", upstreamProbeTarget, upstreamProbeTarget)
	if _, err := conn.Write([]byte(req)); err != nil {
		return false
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusProxyAuthRequired
}

func probeSOCKS5Type(up *Upstream) bool {
	if up == nil || up.URL == nil {
		return false
	}
	probeURL := *up.URL
	probeURL.Scheme = ProxyTypeSocks5
	probe := &Upstream{Name: up.Name, URL: &probeURL}
	return probe.dialSOCKS5Probe(upstreamProbeTarget) == nil
}

func writeUpstreamTypeCache(path string, entries []upstreamTypeCacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (u *Upstream) dialSOCKS5Probe(target string) error {
	conn, err := u.dialSOCKS5(target)
	if err != nil {
		return err
	}
	return conn.Close()
}
