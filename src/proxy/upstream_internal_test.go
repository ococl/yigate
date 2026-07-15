package proxy

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"testing"
)

func TestNormalizeProxyTypes(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"valid", []string{"http", "SOCKS5", " Https "}, []string{"http", "socks5", "https"}, false},
		{"dup", []string{"http", "http"}, []string{"http"}, false},
		{"invalid", []string{"ftp"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := normalizeProxyTypes(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok && len(tc.in) > 0 {
				t.Fatal("expected ok=true")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestValidateProxyTypes(t *testing.T) {
	if err := ValidateProxyTypes([]string{"http", "socks5"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProxyTypes([]string{"ws"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpstreamPrimaryProxyType(t *testing.T) {
	u := &Upstream{Types: []string{"socks5", "http"}}
	if got := u.primaryProxyType(); got != "socks5" {
		t.Fatalf("expected socks5, got %q", got)
	}

	u = &Upstream{URL: mustParseURL(t, "http://127.0.0.1:8080")}
	if got := u.primaryProxyType(); got != "http" {
		t.Fatalf("expected http, got %q", got)
	}

	var nilUp *Upstream
	if got := nilUp.primaryProxyType(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestUpstreamURLForType(t *testing.T) {
	u := &Upstream{URL: mustParseURL(t, "http://127.0.0.1:8080")}
	got := u.urlForType("https")
	if got.Scheme != "https" || got.Host != "127.0.0.1:8080" {
		t.Fatalf("unexpected url: %v", got)
	}

	if u.urlForType("") == nil {
		t.Fatal("expected non-nil url")
	}

	var nilUp *Upstream
	if nilUp.urlForType("http") != nil {
		t.Fatal("expected nil")
	}
}

func TestUpstreamWithProxyType(t *testing.T) {
	u := &Upstream{Name: "u", URL: mustParseURL(t, "http://127.0.0.1:8080")}
	got := u.withProxyType("https")
	if got.URL.Scheme != "https" {
		t.Fatalf("expected https, got %s", got.URL.Scheme)
	}
	if got.Name != "u" {
		t.Fatal("name mismatch")
	}
}

func TestUpstreamSupportsProxyType(t *testing.T) {
	u := &Upstream{Types: []string{"socks5h"}}
	if !u.supportsProxyType("socks5") {
		t.Fatal("expected support socks5 when socks5h listed")
	}
	if !u.supportsProxyType("socks5h") {
		t.Fatal("expected support socks5h")
	}
	if u.supportsProxyType("http") {
		t.Fatal("expected no http support")
	}

	var nilUp *Upstream
	if nilUp.supportsProxyType("http") {
		t.Fatal("expected nil upstream unsupported")
	}
}

func TestProxyAddress(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"https://example.com", "example.com:443"},
		{"socks5://example.com", "example.com:1080"},
		{"http://example.com", "example.com:80"},
	}
	for _, tc := range cases {
		u := mustParseURL(t, tc.url)
		if got := proxyAddress(u); got != tc.want {
			t.Fatalf("proxyAddress(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestTLSConfigForHost(t *testing.T) {
	cfg := tlsConfigForHost("example.com:443")
	if cfg.ServerName != "example.com" {
		t.Fatalf("expected example.com, got %q", cfg.ServerName)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatal("expected TLS 1.2 min")
	}

	cfg2 := tlsConfigForHost("127.0.0.1:443")
	if cfg2.ServerName != "" {
		t.Fatalf("expected empty SNI for IP, got %q", cfg2.ServerName)
	}

	cfg3 := tlsConfigForHost("badaddr")
	if cfg3.ServerName != "badaddr" {
		t.Fatalf("expected badaddr, got %q", cfg3.ServerName)
	}
}

func TestProxyCredentials(t *testing.T) {
	u := &Upstream{URL: mustParseURL(t, "http://user:pass@127.0.0.1:8080")}
	got := proxyCredentials(u)
	if got == "" {
		t.Fatal("expected credentials")
	}
	if proxyCredentials(nil) != "" {
		t.Fatal("expected empty for nil")
	}
}

func TestUpstreamManagerMarkDead(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "dead", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(3600)
	defer mgr.StopHealthCheck()

	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}
	// 连续失败达到阈值才标记 dead，避免单次抖动误判。
	for i := 0; i < deadFailureThreshold; i++ {
		mgr.markDead(up)
	}
	if !up.dead {
		t.Fatal("expected upstream marked dead")
	}
	// 标记 dead 后应调度恢复探测
	if up.nextProbeAt.IsZero() {
		t.Fatal("expected nextProbeAt set after markDead")
	}
	if up.consecutiveDead != 1 {
		t.Fatalf("expected consecutiveDead=1, got %d", up.consecutiveDead)
	}
	// markAlive 恢复
	mgr.markAlive(up)
	if up.dead {
		t.Fatal("expected upstream alive after markAlive")
	}
	if up.failures != 0 || up.consecutiveDead != 0 {
		t.Fatal("expected counters reset after markAlive")
	}
}

// TestMarkDeadFailureThreshold 验证单次失败不标记 dead。
func TestMarkDeadFailureThreshold(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(3600)
	defer mgr.StopHealthCheck()
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}
	mgr.markDead(up)
	if up.dead {
		t.Fatal("expected NOT dead after single failure")
	}
	if up.failures != 1 {
		t.Fatalf("expected failures=1, got %d", up.failures)
	}
}

func TestUpstreamDialOne(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, up, err := mgr.DialOne(target, "http")
	if err != nil {
		t.Fatalf("DialOne: %v", err)
	}
	if up == nil || up.Name != "http" {
		t.Fatal("expected upstream http")
	}
	conn.Close()

	_, _, err = mgr.DialOne(target, "missing")
	if err == nil {
		t.Fatal("expected error for missing upstream")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestPickConsistentHashDeterministic(t *testing.T) {
	us := []*Upstream{
		{Name: "up1", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
		{Name: "up2", URL: mustParseURL(t, "http://h2:8080"), Weight: 1},
		{Name: "up3", URL: mustParseURL(t, "http://h3:8080"), Weight: 1},
	}
	// 同一 key 应始终映射到同一上游
	first := pickConsistentHash(us, "example.com")
	for i := 0; i < 10; i++ {
		got := pickConsistentHash(us, "example.com")
		if got != first {
			t.Fatalf("consistent hash not deterministic: first=%s got=%s", first.Name, got.Name)
		}
	}
}

func TestPickConsistentHashDistribution(t *testing.T) {
	us := []*Upstream{
		{Name: "up1", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
		{Name: "up2", URL: mustParseURL(t, "http://h2:8080"), Weight: 1},
		{Name: "up3", URL: mustParseURL(t, "http://h3:8080"), Weight: 1},
	}
	counts := make(map[string]int)
	total := 3000
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("target-%d.example.com", i)
		picked := pickConsistentHash(us, key)
		counts[picked.Name]++
	}
	// 每个上游应分到大约 1/3 的流量，允许 ±20% 偏差
	expected := total / len(us)
	tolerance := expected / 5
	for _, name := range []string{"up1", "up2", "up3"} {
		c := counts[name]
		if c < expected-tolerance || c > expected+tolerance {
			t.Errorf("upstream %s got %d, expected ~%d (±%d)", name, c, expected, tolerance)
		}
	}
}

func TestPickConsistentHashStability(t *testing.T) {
	// 一致性哈希的核心特性：移除一个上游时，只有映射到该上游的 key 需要迁移
	us3 := []*Upstream{
		{Name: "up1", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
		{Name: "up2", URL: mustParseURL(t, "http://h2:8080"), Weight: 1},
		{Name: "up3", URL: mustParseURL(t, "http://h3:8080"), Weight: 1},
	}
	us2 := []*Upstream{
		{Name: "up1", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
		{Name: "up2", URL: mustParseURL(t, "http://h2:8080"), Weight: 1},
	}

	total := 1000
	migrated := 0
	stayed := 0
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("target-%d.example.com", i)
		before := pickConsistentHash(us3, key)
		after := pickConsistentHash(us2, key)
		if before.Name == "up3" {
			// 映射到被移除上游的 key 必须迁移
			migrated++
		} else {
			// 映射到保留上游的 key 应保持不变
			if before.Name == after.Name {
				stayed++
			} else {
				t.Errorf("key %q was on %s, moved to %s after removing up3 (should have stayed)",
					key, before.Name, after.Name)
			}
		}
	}
	if migrated == 0 {
		t.Error("expected some keys to migrate from removed upstream")
	}
	if stayed == 0 {
		t.Error("expected some keys to stay on remaining upstreams")
	}
	// 大部分 key 应保持不变
	if stayed < total/2 {
		t.Errorf("expected at least half keys to stay, got %d/%d", stayed, total)
	}
}

func TestPickConsistentHashEmptyKey(t *testing.T) {
	us := []*Upstream{
		{Name: "up1", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
		{Name: "up2", URL: mustParseURL(t, "http://h2:8080"), Weight: 1},
	}
	// 空 key 应回退到加权轮询
	got := pickConsistentHash(us, "")
	if got == nil {
		t.Fatal("expected non-nil upstream for empty key")
	}
}

func TestPickConsistentHashSingleUpstream(t *testing.T) {
	us := []*Upstream{
		{Name: "only", URL: mustParseURL(t, "http://h1:8080"), Weight: 1},
	}
	got := pickConsistentHash(us, "any-key")
	if got == nil || got.Name != "only" {
		t.Fatalf("expected only, got %v", got)
	}
}
