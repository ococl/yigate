package proxy

import (
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
	})
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
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	if _, err := router.DialSocks5H("example.com:80", nil); err == nil {
		t.Fatal("expected error when disabled")
	}
}
