package proxy

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"testing"
	"time"
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

// markDeadForTest 直接把上游置为 dead，供状态机相关用例使用。
func markDeadForTest(t *testing.T, mgr *UpstreamManager, up *Upstream) {
	t.Helper()
	mgr.mu.Lock()
	mgr.markDeadLocked(up, time.Now())
	mgr.mu.Unlock()
}

// TestDeadUpstreamIsOnlyHalfOpenFallback 验证被摘除的节点不再参与常规调度，
// 但在没有任何存活节点时仍会作为兜底被尝试（避免 100% 失败且无法自愈）。
func TestDeadUpstreamIsOnlyHalfOpenFallback(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "u1", URL: "http://127.0.0.1:1", Priority: 1},
		{Name: "u2", URL: "http://127.0.0.1:2", Priority: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	dead := mgr.byName["u1"]
	markDeadForTest(t, mgr, dead)

	// 仍有存活节点时，dead 节点不得被选中。
	for i := 0; i < 20; i++ {
		if got := mgr.Select(); got == nil || got.Name != "u2" {
			t.Fatalf("expected alive u2 selected, got %v", got)
		}
	}

	// 只剩 dead 节点时退化为 half-open 兜底，而不是直接返回 nil。
	markDeadForTest(t, mgr, mgr.byName["u2"])
	if got := mgr.Select(); got == nil {
		t.Fatal("expected half-open fallback when no alive upstream remains")
	}
}

func TestProbeLeaseIsSingleFlightAndRejectsStaleResult(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u1", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	markDeadForTest(t, mgr, up)
	mgr.mu.Lock()
	up.nextProbeAt = time.Now().Add(-time.Second)
	mgr.mu.Unlock()
	generation, ok := mgr.beginProbe(up, time.Now())
	if !ok {
		t.Fatal("expected first probe lease")
	}
	if _, ok := mgr.beginProbe(up, time.Now()); ok {
		t.Fatal("second concurrent probe lease must be rejected")
	}
	mgr.applyProbeResult(up, generation+1, true, time.Now())
	if !up.dead {
		t.Fatal("stale probe result must not revive upstream")
	}
	mgr.applyProbeResult(up, generation, true, time.Now())
	if up.dead {
		t.Fatal("current successful probe must revive upstream")
	}
}

func TestProbeBackoffIsCappedWithoutOverflow(t *testing.T) {
	got := probeBackoff(1000)
	lower := deadProbeMaxInterval - deadProbeMaxInterval/5
	upper := deadProbeMaxInterval + deadProbeMaxInterval/5
	if got < lower || got > upper {
		t.Fatalf("probeBackoff(1000) = %v, want within [%v, %v]", got, lower, upper)
	}
}

func TestMarkAliveRequiresConfiguredSuccessThreshold(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{
		Name: "u1", URL: "http://127.0.0.1:1",
		HealthCheck: HealthCheckConfig{SuccessThreshold: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	markDeadForTest(t, mgr, up)
	mgr.markAlive(up)
	if !up.dead {
		t.Fatal("one success must not revive an upstream with success_threshold=2")
	}
	mgr.markAlive(up)
	if up.dead {
		t.Fatal("two consecutive successes must revive upstream")
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
		mgr.markProbeFailure(up)
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
	mgr.markProbeFailure(up)
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

func TestCapabilityFilteringForBindAndUDP(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "s1", URL: "socks5://127.0.0.1:1", Types: []string{"socks5"}},
		{Name: "s2", URL: "socks5://127.0.0.1:2", Types: []string{"socks5"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initially both should be selectable for bind/udp
	up1 := mgr.SelectSocks5ForBind(nil)
	if up1 == nil {
		t.Fatal("expected a bind-capable upstream")
	}

	// Mark s1 as bind-unsupported
	s1 := mgr.upstreams[0]
	mgr.markCapabilityUnsupported(s1, "bind")

	// s1 should be filtered out from bind selection
	up2 := mgr.SelectSocks5ForBind(nil)
	if up2 == nil {
		t.Fatal("expected another bind-capable upstream after filtering s1")
	}
	if up2.Name == "s1" {
		t.Fatal("s1 should be filtered out from bind selection")
	}

	// s1 should still be selectable for regular CONNECT
	up3 := mgr.SelectSocks5For(nil)
	if up3 == nil {
		t.Fatal("expected a socks5 upstream for connect")
	}

	// Mark s1 as udp-unsupported too
	mgr.markCapabilityUnsupported(s1, "udp_associate")
	up4 := mgr.SelectSocks5ForUDP(nil)
	if up4 == nil {
		t.Fatal("expected a udp-capable upstream")
	}
	if up4.Name == "s1" {
		t.Fatal("s1 should be filtered out from udp selection")
	}

	// Reset bind capability for s1
	mgr.markCapabilitySupported(s1, "bind")
	up5 := mgr.SelectSocks5ForBind(nil)
	if up5 == nil {
		t.Fatal("expected s1 to be selectable for bind after reset")
	}
}

func TestCapabilityErrorDoesNotKillUpstream(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "s1", URL: "socks5://127.0.0.1:1", Types: []string{"socks5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s1 := mgr.upstreams[0]

	// Simulate capability error
	capErr := newDialErrorCode(ErrCategoryCapability, "request", 0x07, "bind not supported")
	mgr.markCapabilityUnsupported(s1, "bind")

	// Upstream should NOT be dead
	mgr.mu.Lock()
	dead := s1.dead
	bindUnsupported := s1.bindUnsupported
	mgr.mu.Unlock()

	if dead {
		t.Fatal("upstream should not be dead after capability error")
	}
	if !bindUnsupported {
		t.Fatal("bind capability should be marked unsupported")
	}

	// Should still be selectable for CONNECT
	up := mgr.SelectSocks5For(nil)
	if up == nil {
		t.Fatal("upstream should still be selectable for connect")
	}

	// Should NOT be selectable for BIND
	up = mgr.SelectSocks5ForBind(nil)
	if up != nil {
		t.Fatal("upstream should not be selectable for bind")
	}

	_ = capErr
}

func TestMergeRuntimeStatePreservesUnchangedUpstreams(t *testing.T) {
	oldMgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "u1", URL: "http://127.0.0.1:8080", Weight: 2},
		{Name: "u2", URL: "socks5://127.0.0.1:1080", Types: []string{"socks5"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mark u1 as dead with some failures
	oldMgr.mu.Lock()
	u1 := oldMgr.upstreams[0]
	u1.dead = true
	u1.failures = 3
	u1.consecutiveDead = 2
	u1.bindUnsupported = true
	oldMgr.mu.Unlock()

	// Create new manager with same upstreams but different weight
	newMgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "u1", URL: "http://127.0.0.1:8080", Weight: 5}, // weight changed
		{Name: "u2", URL: "socks5://127.0.0.1:1080", Types: []string{"socks5"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	newMgr.MergeRuntimeState(oldMgr)

	newMgr.mu.Lock()
	nu1 := newMgr.upstreams[0]
	nu2 := newMgr.upstreams[1]
	nu1Dead := nu1.dead
	nu1Failures := nu1.failures
	nu1ConsecutiveDead := nu1.consecutiveDead
	nu1BindUnsupported := nu1.bindUnsupported
	nu2Dead := nu2.dead
	nu2Weight := nu2.Weight
	newMgr.mu.Unlock()

	// u1 runtime state should be preserved even though weight changed
	if !nu1Dead {
		t.Fatal("u1 dead state should be preserved after merge")
	}
	if nu1Failures != 3 {
		t.Fatalf("u1 failures = %d, want 3", nu1Failures)
	}
	if nu1ConsecutiveDead != 2 {
		t.Fatalf("u1 consecutiveDead = %d, want 2", nu1ConsecutiveDead)
	}
	if !nu1BindUnsupported {
		t.Fatal("u1 bindUnsupported should be preserved")
	}
	// u2 should be unchanged (alive)
	if nu2Dead {
		t.Fatal("u2 should be alive")
	}
	_ = nu2Weight
}

func TestMergeRuntimeStateDoesNotPreserveChangedUpstreams(t *testing.T) {
	oldMgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "u1", URL: "http://127.0.0.1:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mark u1 as dead
	oldMgr.mu.Lock()
	oldMgr.upstreams[0].dead = true
	oldMgr.upstreams[0].failures = 5
	oldMgr.mu.Unlock()

	// Create new manager with changed URL (different identity)
	newMgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "u1", URL: "http://127.0.0.1:9090"}, // URL changed
	})
	if err != nil {
		t.Fatal(err)
	}

	newMgr.MergeRuntimeState(oldMgr)

	newMgr.mu.Lock()
	nu1Dead := newMgr.upstreams[0].dead
	nu1Failures := newMgr.upstreams[0].failures
	newMgr.mu.Unlock()

	// URL changed → identity mismatch → state NOT preserved
	if nu1Dead {
		t.Fatal("changed upstream should NOT preserve dead state")
	}
	if nu1Failures != 0 {
		t.Fatalf("changed upstream failures = %d, want 0", nu1Failures)
	}
}

func TestMergeRuntimeStateNilOld(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u1", URL: "http://127.0.0.1:8080"}})
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic
	mgr.MergeRuntimeState(nil)
}

// TestPassiveBreakerThresholdAndNodeFailureFilter 验证业务失败单独计数、
// 阈值更高，并且只有节点级硬失败才计入。
func TestPassiveBreakerThresholdAndNodeFailureFilter(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	for i := 0; i < passiveFailureThreshold-1; i++ {
		mgr.markPassiveFailure(up)
	}
	if up.dead {
		t.Fatalf("expected still alive after %d passive failures", passiveFailureThreshold-1)
	}
	mgr.markPassiveFailure(up)
	if !up.dead {
		t.Fatalf("expected dead after %d passive failures", passiveFailureThreshold)
	}

	// 连不上 / 认证失败 → 计入。
	if !IsNodeFailure(newDialErrorCode(ErrCategoryUpstream, "dial", 0, "dial refused")) {
		t.Fatal("expected dial failure to be a node failure")
	}
	if !IsNodeFailure(newDialErrorCode(ErrCategoryUpstream, "auth", 407, "auth failed")) {
		t.Fatal("expected auth failure to be a node failure")
	}
	// 协议协商之后的失败可能来自上游自身链路，不能据此摘节点。
	if IsNodeFailure(newDialErrorCode(ErrCategoryUpstream, "request", 0x01, "general failure")) {
		t.Fatal("expected SOCKS5 general failure NOT to be a node failure")
	}
	if IsNodeFailure(newDialErrorCode(ErrCategoryTarget, "connect", 502, "target error")) {
		t.Fatal("expected target error NOT to be a node failure")
	}
	if IsNodeFailure(newDialErrorCode(ErrCategoryBusy, "connect", 503, "busy")) {
		t.Fatal("expected busy NOT to be a node failure")
	}
	if IsUpstreamError(newDialErrorCode(ErrCategoryBusy, "connect", 503, "busy")) {
		t.Fatal("expected busy NOT to be an upstream error")
	}
}

// TestDeadUpstreamRevivedByBusinessDial 验证 half-open 兜底：全部摘除时业务
// 拨号仍会尝试，成功后立即恢复，即使关闭了主动健康检查。
func TestDeadUpstreamRevivedByBusinessDial(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	markDeadForTest(t, mgr, up)
	if mgr.Select() == nil {
		t.Fatal("expected half-open fallback to return the only upstream")
	}

	target := startMockTarget(t)
	conn, got, err := mgr.DialOne(target, "http")
	if err != nil {
		t.Fatalf("DialOne on dead upstream: %v", err)
	}
	conn.Close()
	if got == nil || got.dead {
		t.Fatal("expected dead upstream revived by successful business dial")
	}
}
