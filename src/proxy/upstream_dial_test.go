package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startMockHTTPProxy 启动一个最小 HTTP CONNECT/转发代理服务器，返回监听地址。
func startMockHTTPProxy(t *testing.T) string {
	return startMockHTTPProxyWithOptions(t, false, "")
}

func startMockHTTPSProxy(t *testing.T) string {
	return startMockHTTPProxyWithOptions(t, true, "")
}

func startMockHTTPProxyWithAuth(t *testing.T, expectedAuth string) string {
	return startMockHTTPProxyWithOptions(t, false, expectedAuth)
}

func startMockHTTPProxyWithOptions(t *testing.T, tlsEnabled bool, expectedAuth string) string {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if expectedAuth != "" && req.Header.Get("Proxy-Authorization") != expectedAuth {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		if req.Method == http.MethodConnect {
			target, err := net.Dial("tcp", req.Host)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			defer target.Close()
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, brw, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			if _, err := brw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				return
			}
			if err := brw.Flush(); err != nil {
				return
			}
			go func() { _, _ = ioCopy(target, brw) }()
			_, _ = ioCopy(conn, target)
			return
		}

		target, err := net.Dial("tcp", req.Host)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer target.Close()
		out := req.Clone(req.Context())
		out.RequestURI = ""
		if err := out.Write(target); err != nil {
			return
		}
		resp, err := http.ReadResponse(bufio.NewReader(target), req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	if tlsEnabled {
		ts := httptest.NewTLSServer(handler)
		t.Cleanup(ts.Close)
		return strings.TrimPrefix(ts.URL, "https://")
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return strings.TrimPrefix(ts.URL, "http://")
}

func startMockSocks5Proxy(t *testing.T) string {
	t.Helper()
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				HandleSOCKS5(c, br, nil, nil, router, nil, false, false)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func startMockTarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				c.Write(buf[:n])
			}(c)
		}
	}()
	return ln.Addr().String()
}

func ioCopy(dst io.Writer, src io.Reader) (int64, error) {
	var n int64
	buf := make([]byte, 4096)
	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, _ := dst.Write(buf[:nr])
			n += int64(nw)
		}
		if err != nil {
			return n, err
		}
	}
}

func TestUpstreamDialHTTP(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via http proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamDialHTTPS(t *testing.T) {
	proxyAddr := startMockHTTPSProxy(t)
	oldTLSConfig := tlsConfigForHost
	tlsConfigForHost = func(addr string) *tls.Config {
		cfg := defaultTLSConfigForHost(addr)
		cfg.InsecureSkipVerify = true
		return cfg
	}
	t.Cleanup(func() { tlsConfigForHost = oldTLSConfig })

	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "https", URL: "https://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via https proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("secure")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "secure" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamDialHTTPWithProxyAuthorization(t *testing.T) {
	wantAuth := "Basic " + proxyCredentials(&Upstream{URL: mustParseURL(t, "http://user:pass@127.0.0.1:8080")})
	proxyAddr := startMockHTTPProxyWithAuth(t, wantAuth)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http-auth", URL: "http://user:pass@" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via authenticated http proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("auth")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "auth" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamDialSOCKS5(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "socks5", URL: "socks5://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via socks5 proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamDialSOCKS5WithAuthentication(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, map[string]string{"user": "pass"})
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "socks5-auth", URL: "socks5://user:pass@" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via authenticated socks5 proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("socks-auth")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "socks-auth" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamManagerDialAndFallback(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, err := mgr.Dial(target)
	if err != nil {
		t.Fatalf("dial fallback: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamManagerDialFallsBackToDirectAfterUpstreamFailure(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "bad", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	MetricSet("k5pw_upstream_dial_failures_total", 0, "upstream=bad")
	target := startMockTarget(t)
	conn, err := mgr.Dial(target)
	if err != nil {
		t.Fatalf("dial fallback: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("fallback")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "fallback" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
	if got := MetricValue("k5pw_upstream_dial_failures_total", "upstream=bad"); got < 1 {
		t.Fatalf("expected upstream failure metric, got %v", got)
	}
}
