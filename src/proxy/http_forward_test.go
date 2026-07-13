package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDirectRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := directRequest(req, nil)
	if err != nil {
		t.Fatalf("direct request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestDirectRequestWithTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := directRequestWithTimeout(req, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("direct request with timeout: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

// TestDirectRequestBlockedByPolicy 验证普通 HTTP 直连转发也会经过 DestinationPolicy 校验，
// 防止 SSRF 访问内网/云元数据等受保护地址（修复：直连路径曾绕过策略检查）。
func TestDirectRequestBlockedByPolicy(t *testing.T) {
	pol, err := NewDestinationPolicy(DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{policy: pol, cache: NewRouteCache()}
	defer r.Stop()

	// 169.254.169.254 是云元数据地址，属于受保护网段，应被拦截且不应发起真实连接。
	req := httptest.NewRequest("GET", "http://169.254.169.254/latest/meta-data/", nil)
	_, err = directRequest(req, r)
	if err == nil {
		t.Fatal("expected direct request to protected metadata IP to be blocked")
	}
	if _, ok := err.(*ProxyError); !ok {
		// 至少应返回错误；类型断言仅为可选增强。
		t.Logf("blocked with error type %T: %v", err, err)
	}
}

// TestRedactURLString 验证错误日志中的上游 URL 不会泄露账号密码。
func TestRedactURLString(t *testing.T) {
	got := redactURLString("socks5://user:secret@10.0.0.1:1080")
	if strings.Contains(got, "secret") {
		t.Fatalf("redacted url still contains password: %q", got)
	}
	if !strings.Contains(got, "10.0.0.1:1080") {
		t.Fatalf("redacted url should keep host: %q", got)
	}
}

func TestViaHTTPUpstream(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	up := mgr.Select()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("via upstream"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := viaHTTPUpstream(req, up)
	if err != nil {
		t.Fatalf("via http upstream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "via upstream" {
		t.Fatalf("body = %q", body)
	}
}

func TestViaSOCKS5Upstream(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "socks5", URL: "socks5://" + proxyAddr}})
	up := mgr.Select()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("via socks5"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := viaSOCKS5Upstream(req, up)
	if err != nil {
		t.Fatalf("via socks5 upstream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "via socks5" {
		t.Fatalf("body = %q", body)
	}
}

func TestForwardRequest(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	up := mgr.Select()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("forwarded"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := forwardRequest(req, up)
	if err != nil {
		t.Fatalf("forward request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "forwarded" {
		t.Fatalf("body = %q", body)
	}
}

func TestTargetHost(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.com/path", nil)
	if got := targetHost(req); got != "example.com:443" {
		t.Fatalf("targetHost = %q, want example.com:443", got)
	}
	req = httptest.NewRequest("GET", "http://example.com:8080/path", nil)
	if got := targetHost(req); got != "example.com:8080" {
		t.Fatalf("targetHost = %q", got)
	}
}
