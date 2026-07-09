package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestViaProxy(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	router, _ := NewRouter(RoutingConfig{Final: ActionProxy})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxied"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := requestViaProxy(req, mgr, router, nil, func() {})
	if err != nil {
		t.Fatalf("requestViaProxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "proxied" {
		t.Fatalf("body = %q", body)
	}
}

func TestRequestViaProxyNoRetry(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	router, _ := NewRouter(RoutingConfig{Final: ActionProxy})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxied"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := requestViaProxyNoRetry(req, mgr, router, nil)
	if err != nil {
		t.Fatalf("requestViaProxyNoRetry: %v", err)
	}
	defer resp.Body.Close()
}

func TestRequestAutoDirectFirst(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	router, _ := NewRouter(RoutingConfig{Final: ActionAuto})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("auto"))
	}))
	defer ts.Close()

	req := httptest.NewRequest("GET", ts.URL+"/path", nil)
	resp, err := requestAuto(req, mgr, router, func() {})
	if err != nil {
		t.Fatalf("requestAuto: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "auto" {
		t.Fatalf("body = %q", body)
	}
}

func TestRequestViaProxyRetriesBufferedBody(t *testing.T) {
	badLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer badLn.Close()
	go func() {
		conn, err := badLn.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	badProxy := badLn.Addr().String()
	goodProxy := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "bad", URL: "http://" + badProxy, Priority: 1},
		{Name: "good", URL: "http://" + goodProxy, Priority: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(3600)
	defer mgr.StopHealthCheck()
	router, _ := NewRouter(RoutingConfig{Final: ActionProxy})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "retry-body" {
			t.Fatalf("body = %q, want retry-body", body)
		}
		w.Write([]byte("retried"))
	}))
	defer ts.Close()

	bodyBytes := []byte("retry-body")
	req := httptest.NewRequest("POST", ts.URL+"/path", strings.NewReader(string(bodyBytes)))
	resetBody := func() {
		req.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(string(bodyBytes))), nil }
		req.ContentLength = int64(len(bodyBytes))
	}
	resp, err := requestViaProxy(req, mgr, router, nil, resetBody)
	if err != nil {
		t.Fatalf("requestViaProxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "retried" {
		t.Fatalf("response body = %q", body)
	}
}

func TestIsTargetError(t *testing.T) {
	if !isTargetError(errorFor("target unreachable")) {
		t.Fatal("expected target error")
	}
	if !isTargetError(errorFor("connection refused")) {
		t.Fatal("expected connection refused as target error")
	}
	if isTargetError(nil) {
		t.Fatal("expected nil not target error")
	}
}

func errorFor(msg string) error {
	return &mockError{msg}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }
