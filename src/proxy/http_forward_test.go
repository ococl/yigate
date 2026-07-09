package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDirectRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := directRequest(req)
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
	resp, err := directRequestWithTimeout(req, 5*time.Second)
	if err != nil {
		t.Fatalf("direct request with timeout: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
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
