package proxy

import (
	"errors"
	"testing"
	"time"
)

func TestDialSocks5H(t *testing.T) {
	router, err := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Socks5HRedirect: Socks5HConfig{
			Enabled:   true,
			Upstreams: []string{"socks"},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "socks", URL: "socks5://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}

	target := startMockTarget(t)
	conn, err := router.DialSocks5H(target, mgr)
	if err != nil {
		t.Fatalf("DialSocks5H: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hi" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}

	// 第二次应该命中缓存
	conn2, err := router.DialSocks5H(target, mgr)
	if err != nil {
		t.Fatalf("DialSocks5H cached: %v", err)
	}
	conn2.Close()
}

func TestDialSocks5HDisabled(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	if _, err := router.DialSocks5H("example.com:80", nil); err == nil {
		t.Fatal("expected error when disabled")
	}
}

// TestSocks5HNoUpstreamsFastReturn 验证无可用上游时立即返回错误，
// 不经历超时等待（全局熔断器已移除，改由每上游健康状态管理）。
func TestSocks5HNoUpstreamsFastReturn(t *testing.T) {
	router, err := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Socks5HRedirect: Socks5HConfig{
			Enabled: true,
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// 无可用上游的 manager
	mgr, err := NewUpstreamManager(nil)
	if err != nil {
		t.Fatal(err)
	}
	// 调用应立即返回错误，不经过上游超时等待
	before := time.Now()
	_, err = router.DialSocks5H("example.com:80", mgr)
	elapsed := time.Since(before)
	if err == nil {
		t.Fatal("expected error with no upstreams")
	}
	if !errors.Is(err, ErrSocks5HNoUpstream) {
		t.Fatalf("expected ErrSocks5HNoUpstream, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("no-upstream return took too long: %v", elapsed)
	}
}

// TestSocks5HEnabledWithoutUpstreamTreatedAsDisabled 验证配置开启但没有任何
// 可用 SOCKS5 上游时，socks5h 重定向等价于未开启：调用方直接走普通路由，
// 既不做无谓尝试也不产生告警噪音。
func TestSocks5HEnabledWithoutUpstreamTreatedAsDisabled(t *testing.T) {
	router, err := NewRouter(RoutingConfig{
		Final:           ActionAuto,
		Socks5HRedirect: Socks5HConfig{Enabled: true},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// 未注入判定时按配置字面值生效。
	if !router.Socks5HEnabled() {
		t.Fatal("expected enabled by config before wiring")
	}

	router.SetSocks5HAvailable(func(names []string) bool { return false })
	if router.Socks5HEnabled() {
		t.Fatal("expected socks5h redirect treated as disabled without upstream")
	}
	if _, err := router.DialSocks5H("example.com:80", nil); !errors.Is(err, ErrSocks5HNoUpstream) {
		t.Fatalf("expected ErrSocks5HNoUpstream, got %v", err)
	}

	router.SetSocks5HAvailable(func(names []string) bool { return true })
	if !router.Socks5HEnabled() {
		t.Fatal("expected enabled once a socks5 upstream is available")
	}
}

// TestSocks5HEnabledWiringCountsUpstreams 验证装配用的判定直接反映
// UpstreamManager 里启用的 SOCKS5 上游数量。
func TestSocks5HEnabledWiringCountsUpstreams(t *testing.T) {
	router, err := NewRouter(RoutingConfig{
		Final:           ActionAuto,
		Socks5HRedirect: Socks5HConfig{Enabled: true},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := NewUpstreamManager(nil)
	if err != nil {
		t.Fatal(err)
	}
	onlyHTTP, err := NewUpstreamManager([]UpstreamConfig{{Name: "h", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	withSocks, err := NewUpstreamManager([]UpstreamConfig{{Name: "s", URL: "socks5h://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	disabledSocks, err := NewUpstreamManager([]UpstreamConfig{{Name: "s", URL: "socks5h://127.0.0.1:1", Enabled: &disabled}})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		mgr  *UpstreamManager
		want bool
	}{
		{"empty", empty, false},
		{"http-only", onlyHTTP, false},
		{"disabled-socks", disabledSocks, false},
		{"with-socks", withSocks, true},
	} {
		mgr := tc.mgr
		router.SetSocks5HAvailable(func(names []string) bool {
			return mgr != nil && mgr.Socks5AttemptCount(names) > 0
		})
		if got := router.Socks5HEnabled(); got != tc.want {
			t.Errorf("%s: Socks5HEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}
