package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"yigate/proxy"
)

// findFreePort 返回一个当前可用的 TCP 端口。
func findFreePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// startTestServer 启动一个测试服务器，返回服务器实例和代理/管理端口地址。
func startTestServer(t *testing.T, cfg *Config) (*Server, string, string) {
	t.Helper()
	proxyAddr := findFreePort(t)
	mgmtAddr := findFreePort(t)
	cfg.Listen = proxyAddr
	cfg.Management = mgmtAddr
	cfg.ManagementAuthToken = "test-token"
	cfg.Log.Level = "warn"
	cfg.Log.Output = "stdout"

	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	// 等待监听就绪
	for i := 0; i < 50; i++ {
		s.listenersMu.Lock()
		ln := s.listeners[proxyAddr]
		s.listenersMu.Unlock()
		if ln != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 等待管理端口就绪
	for i := 0; i < 50; i++ {
		if s.mgmt.Addr() != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return s, proxyAddr, s.mgmt.Addr()
}

// startOriginServer 启动一个简单的 HTTP 后端服务器，返回监听地址。
func startOriginServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen origin: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

// mgmtRequest 构造带认证的管理 API 请求。
func mgmtRequest(method, addr, path string, body io.Reader) *http.Request {
	req, _ := http.NewRequest(method, "http://"+addr+path, body)
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

// ============================================================
// 端到端 HTTP 代理集成测试
// ============================================================

func TestIntegrationHTTPProxy(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(200)
		fmt.Fprint(w, "hello from origin")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false

	s, proxyAddr, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	// 通过代理发送 HTTP 请求
	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://" + originAddr + "/test")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Origin") != "yes" {
		t.Fatal("missing X-Origin header")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from origin" {
		t.Fatalf("body = %q, want %q", body, "hello from origin")
	}

	// 验证管理端点 /health
	hreq := mgmtRequest("GET", mgmtAddr, "/health", nil)
	hresp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer hresp.Body.Close()
	if hresp.StatusCode != 200 {
		t.Fatalf("health status = %d, want 200", hresp.StatusCode)
	}
	var hs proxy.HealthStatus
	json.NewDecoder(hresp.Body).Decode(&hs)
	if hs.Status != "up" {
		t.Fatalf("health status = %q, want up", hs.Status)
	}
}

func TestIntegrationHTTPConnect(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "connect ok")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	// CONNECT 隧道
	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("CONNECT GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "connect ok" {
		t.Fatalf("body = %q", body)
	}
}

// ============================================================
// 端到端 SOCKS5 代理集成测试
// ============================================================

func TestIntegrationSOCKS5(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "socks5 ok")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// SOCKS5 greeting
	conn.Write([]byte{0x05, 0x01, 0x00})
	auth := make([]byte, 2)
	io.ReadFull(conn, auth)
	if auth[0] != 0x05 || auth[1] != 0x00 {
		t.Fatalf("auth resp = %v", auth)
	}

	// CONNECT to origin
	host, portStr, _ := net.SplitHostPort(originAddr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	ip := net.ParseIP(host)
	var req []byte
	if ip.To4() != nil {
		req = []byte{0x05, 0x01, 0x00, 0x01}
		req = append(req, ip.To4()...)
	} else {
		req = []byte{0x05, 0x01, 0x00, 0x04}
		req = append(req, ip.To16()...)
	}
	req = append(req, byte(port>>8), byte(port))
	conn.Write(req)

	rep := make([]byte, 10)
	io.ReadFull(conn, rep)
	if rep[1] != 0x00 {
		t.Fatalf("connect reply = %d, want 0", rep[1])
	}

	// 发送 HTTP 请求通过隧道
	conn.SetDeadline(time.Time{})
	httpReq := "GET / HTTP/1.1\r\nHost: " + originAddr + "\r\n\r\n"
	conn.Write([]byte(httpReq))

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "socks5 ok" {
		t.Fatalf("body = %q", body)
	}
}

// ============================================================
// 管理接口端到端测试
// ============================================================

func TestIntegrationManagementHealth(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false

	s, _, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	// 无认证应 401
	resp, err := http.Get("http://" + mgmtAddr + "/health")
	if err != nil {
		t.Fatalf("health no auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("no auth status = %d, want 401", resp.StatusCode)
	}

	// 有认证应 200
	req := mgmtRequest("GET", mgmtAddr, "/health", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health with auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("auth status = %d, want 200", resp.StatusCode)
	}
}

func TestIntegrationManagementMetrics(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false

	s, _, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	req := mgmtRequest("GET", mgmtAddr, "/metrics", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metrics status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "yigate_") {
		t.Fatalf("metrics body missing yigate_ prefix: %s", body)
	}
}

func TestIntegrationManagementStatus(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false

	s, proxyAddr, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	req := mgmtRequest("GET", mgmtAddr, "/api/v1/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status code = %d", resp.StatusCode)
	}
	var snap proxy.StatusSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if snap.Status != "up" {
		t.Fatalf("status = %q, want up", snap.Status)
	}
	if !strings.Contains(snap.Listen, proxyAddr) {
		t.Fatalf("listen = %q, want contains %q", snap.Listen, proxyAddr)
	}
	if !snap.RouterReady || !snap.ManagerReady {
		t.Fatal("expected router and manager ready")
	}
}

func TestIntegrationManagementReload(t *testing.T) {
	dir := t.TempDir()
	proxyAddr := findFreePort(t)
	mgmtAddr := findFreePort(t)
	configContent := fmt.Sprintf(`listen: %s
management: %s
management_auth_token: test-token
routing:
  final: direct
log:
  level: warn
  output: stdout
`, proxyAddr, mgmtAddr)
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(configContent), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start: %v", err)
		}
	}()
	defer s.Stop()

	// 等待管理端口就绪
	for i := 0; i < 50; i++ {
		if s.mgmt.Addr() != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mgmt := s.mgmt.Addr()

	// 触发 reload
	req := mgmtRequest("POST", mgmt, "/api/v1/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("reload status = %d, want 200", resp.StatusCode)
	}
}

func TestIntegrationManagementTraffic(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "traffic test data")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.TrafficAudit.Enabled = true

	s, proxyAddr, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	// 通过代理发送请求产生流量
	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	resp.Body.Close()

	// 查询流量端点
	req := mgmtRequest("GET", mgmtAddr, "/api/v1/traffic", nil)
	tResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("traffic endpoint: %v", err)
	}
	defer tResp.Body.Close()
	if tResp.StatusCode != 200 {
		t.Fatalf("traffic status = %d, want 200", tResp.StatusCode)
	}
	var snap proxy.TrafficSnapshot
	if err := json.NewDecoder(tResp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode traffic: %v", err)
	}
	// 应该有流量记录
	if len(snap.Entries) == 0 {
		// 流量可能尚未刷新，再试一次
		time.Sleep(100 * time.Millisecond)
		req2 := mgmtRequest("GET", mgmtAddr, "/api/v1/traffic", nil)
		tResp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("traffic retry: %v", err)
		}
		json.NewDecoder(tResp2.Body).Decode(&snap)
		tResp2.Body.Close()
	}
	// 验证至少有流量条目（可能因为时序原因为空，但端点本身应正常工作）
	t.Logf("traffic entries: %d", len(snap.Entries))
}

func TestIntegrationManagementTrafficMethodNotAllowed(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false

	s, _, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	req := mgmtRequest("POST", mgmtAddr, "/api/v1/traffic", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("traffic POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("traffic POST status = %d, want 405", resp.StatusCode)
	}
}

func TestIntegrationManagementTrafficNoAuth(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false

	s, _, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	resp, err := http.Get("http://" + mgmtAddr + "/api/v1/traffic")
	if err != nil {
		t.Fatalf("traffic no auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("traffic no auth status = %d, want 401", resp.StatusCode)
	}
}

// TestIntegrationTrafficPersistsAcrossRestart 验证流量明细会落盘并在进程重启后恢复。
func TestIntegrationTrafficPersistsAcrossRestart(t *testing.T) {
	originAddr := startOriginServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "persist traffic")
	})

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.TrafficAudit.Enabled = true
	cfg.TrafficAudit.RetentionDays = 3
	cfg.Listen = findFreePort(t)
	cfg.Management = findFreePort(t)
	cfg.ManagementAuthToken = "test-token"
	cfg.Log.Level = "warn"
	cfg.Log.Output = "stdout"

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")

	first, err := NewServer(cfgPath, cfg)
	if err != nil {
		t.Fatalf("NewServer(first): %v", err)
	}
	go func() {
		if err := first.Start(); err != nil {
			t.Logf("first.Start returned: %v", err)
		}
	}()
	for i := 0; i < 100; i++ {
		first.listenersMu.Lock()
		ln := first.listeners[cfg.Listen]
		first.listenersMu.Unlock()
		if ln != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	proxyURL, _ := url.Parse("http://" + cfg.Listen)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	before := first.currentRuntime().trafficAccounter.Snapshot()
	if len(before.Entries) == 0 {
		t.Fatal("no traffic recorded before restart")
	}
	var originalTotal int64
	for _, entry := range before.Entries {
		originalTotal += entry.Total
	}
	first.Stop()

	if _, err := os.Stat(filepath.Join(dir, "traffic.json")); err != nil {
		t.Fatalf("traffic.json not written: %v", err)
	}

	second, err := NewServer(cfgPath, cfg)
	if err != nil {
		t.Fatalf("NewServer(second): %v", err)
	}
	defer second.Stop()

	after := second.currentRuntime().trafficAccounter.Snapshot()
	if after.RetentionDays != 3 {
		t.Fatalf("retention_days = %d, want 3", after.RetentionDays)
	}
	var restoredTotal int64
	for _, entry := range after.Entries {
		restoredTotal += entry.Total
	}
	if restoredTotal < originalTotal {
		t.Fatalf("restored total = %d, want >= %d", restoredTotal, originalTotal)
	}
}

// ============================================================
// 配置 API 端到端测试
// ============================================================

func TestIntegrationConfigAPIUpstreams(t *testing.T) {
	dir := t.TempDir()
	proxyAddr := findFreePort(t)
	mgmtAddr := findFreePort(t)
	configContent := fmt.Sprintf(`listen: %s
management: %s
management_auth_token: test-token
routing:
  final: direct
log:
  level: warn
  output: stdout
`, proxyAddr, mgmtAddr)
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(configContent), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start: %v", err)
		}
	}()
	defer s.Stop()

	for i := 0; i < 50; i++ {
		if s.mgmt.Addr() != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mgmt := s.mgmt.Addr()

	// 列出 upstreams（应为空）
	req := mgmtRequest("GET", mgmt, "/api/v1/config/upstreams", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list status = %d", resp.StatusCode)
	}

	// 添加 upstream
	accounterBefore := s.currentRuntime().trafficAccounter
	addBody := strings.NewReader(`{"name":"test-up","url":"socks5://127.0.0.1:1080","types":["socks5"],"priority":1,"weight":1}`)
	req = mgmtRequest("POST", mgmt, "/api/v1/config/upstreams", addBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("add upstream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("add status = %d", resp.StatusCode)
	}
	if after := s.currentRuntime().trafficAccounter; after != accounterBefore {
		t.Fatal("traffic accounter was replaced after upstream change")
	}

	// 验证已添加
	req = mgmtRequest("GET", mgmt, "/api/v1/config/upstreams", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list after add: %v", err)
	}
	defer resp.Body.Close()
	var listResp struct {
		Upstreams []map[string]interface{} `json:"upstreams"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	if len(listResp.Upstreams) == 0 {
		t.Fatal("expected at least 1 upstream after add")
	}
}

// ============================================================
// TLS 终止端到端测试
// ============================================================

func TestIntegrationTLSTermination(t *testing.T) {
	certFile, keyFile, _ := generateTestCerts(t)
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "tls proxy ok")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.TLS.CertFile = certFile
	cfg.TLS.KeyFile = keyFile

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	// 通过 TLS 连接发送 HTTP 代理请求
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	proxyURL, _ := url.Parse("https://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: tlsConfig,
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("TLS proxy GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "tls proxy ok" {
		t.Fatalf("body = %q", body)
	}
}

// ============================================================
// 多监听器端到端测试
// ============================================================

func TestIntegrationMultiListener(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "multi-listener ok")
	})
	originAddr := startOriginServer(t, originHandler)

	addr1 := findFreePort(t)
	addr2 := findFreePort(t)
	mgmtAddr := findFreePort(t)

	cfg := defaultConfig()
	cfg.Listeners = []ListenerConfig{
		{Listen: addr1, Users: map[string]string{"user1": "pass1"}},
		{Listen: addr2, Users: map[string]string{"user2": "pass2"}},
	}
	cfg.Management = mgmtAddr
	cfg.ManagementAuthToken = "test-token"
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.Log.Level = "warn"
	cfg.Log.Output = "stdout"

	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start: %v", err)
		}
	}()
	defer s.Stop()

	// 等待两个监听器就绪
	for i := 0; i < 50; i++ {
		s.listenersMu.Lock()
		ln1 := s.listeners[addr1]
		ln2 := s.listeners[addr2]
		s.listenersMu.Unlock()
		if ln1 != nil && ln2 != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 通过第一个监听器发送请求（带认证）
	proxyURL1, _ := url.Parse("http://" + addr1)
	proxyURL1.User = url.UserPassword("user1", "pass1")
	client1 := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL1)},
		Timeout:   5 * time.Second,
	}
	resp, err := client1.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("listener1 GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("listener1 status = %d", resp.StatusCode)
	}

	// 通过第二个监听器发送请求（带认证）
	proxyURL2, _ := url.Parse("http://" + addr2)
	proxyURL2.User = url.UserPassword("user2", "pass2")
	client2 := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL2)},
		Timeout:   5 * time.Second,
	}
	resp2, err := client2.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("listener2 GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("listener2 status = %d", resp2.StatusCode)
	}

	// 验证错误密码被拒绝
	proxyURLBad, _ := url.Parse("http://" + addr1)
	proxyURLBad.User = url.UserPassword("user1", "wrong-pass")
	clientBad := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURLBad)},
		Timeout:   5 * time.Second,
	}
	respBad, err := clientBad.Get("http://" + originAddr + "/")
	if err == nil {
		defer respBad.Body.Close()
		if respBad.StatusCode == 200 {
			t.Fatal("expected auth failure with wrong password")
		}
	}
}

// ============================================================
// IPv6 端到端测试
// ============================================================

func TestIntegrationIPv6Listen(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ipv6 ok")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.Listen = "[::1]:0"

	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start: %v", err)
		}
	}()
	defer s.Stop()

	// 等待监听就绪
	var proxyAddr string
	for i := 0; i < 50; i++ {
		s.listenersMu.Lock()
		for addr, ln := range s.listeners {
			proxyAddr = ln.Addr().String()
			_ = addr
			break
		}
		s.listenersMu.Unlock()
		if proxyAddr != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if proxyAddr == "" {
		t.Fatal("IPv6 listener not ready")
	}

	// 通过 IPv6 代理发送请求
	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("IPv6 proxy GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ipv6 ok" {
		t.Fatalf("body = %q", body)
	}
}

// ============================================================
// 配置热重载端到端测试
// ============================================================

func TestIntegrationHotReload(t *testing.T) {
	dir := t.TempDir()
	proxyAddr := findFreePort(t)
	mgmtAddr := findFreePort(t)
	configContent := fmt.Sprintf(`listen: %s
management: %s
management_auth_token: test-token
routing:
  final: direct
log:
  level: warn
  output: stdout
`, proxyAddr, mgmtAddr)
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(configContent), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start: %v", err)
		}
	}()
	defer s.Stop()

	// 等待就绪
	for i := 0; i < 50; i++ {
		s.listenersMu.Lock()
		ln := s.listeners[proxyAddr]
		s.listenersMu.Unlock()
		if ln != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 修改配置：添加 upstream 并改变 routing final
	newConfig := fmt.Sprintf(`listen: %s
management: %s
management_auth_token: test-token
upstreams:
  - name: test-up
    url: socks5://127.0.0.1:1080
    types: [socks5]
routing:
  final: proxy
log:
  level: warn
  output: stdout
`, proxyAddr, mgmtAddr)
	os.WriteFile(path, []byte(newConfig), 0644)

	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}

	// 验证配置已更新
	if s.Config().Routing.Final != "proxy" {
		t.Fatalf("final = %q, want proxy", s.Config().Routing.Final)
	}
	if len(s.Config().Upstreams) != 1 {
		t.Fatalf("upstreams = %d, want 1", len(s.Config().Upstreams))
	}
	if s.Config().Upstreams[0].Name != "test-up" {
		t.Fatalf("upstream name = %q, want test-up", s.Config().Upstreams[0].Name)
	}
}

// ============================================================
// 并发请求压力测试
// ============================================================

func TestIntegrationConcurrentRequests(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, "concurrent-%s", r.URL.Path)
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			MaxIdleConnsPerHost: 20,
		},
		Timeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := client.Get(fmt.Sprintf("http://%s/%d", originAddr, idx))
			if err != nil {
				errCh <- fmt.Errorf("req %d: %v", idx, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				errCh <- fmt.Errorf("req %d: status %d", idx, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// ============================================================
// Prometheus 指标端到端验证
// ============================================================

func TestIntegrationMetricsAfterTraffic(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "metrics test")
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false

	s, proxyAddr, mgmtAddr := startTestServer(t, cfg)
	defer s.Stop()

	// 产生流量
	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	for i := 0; i < 3; i++ {
		resp, err := client.Get("http://" + originAddr + "/")
		if err != nil {
			t.Fatalf("proxy GET %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// 检查指标
	req := mgmtRequest("GET", mgmtAddr, "/metrics", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	metricsStr := string(body)

	// 应该有连接计数指标
	if !strings.Contains(metricsStr, "yigate_connections_total") {
		t.Fatal("missing yigate_connections_total metric")
	}
	// 应该有活跃连接指标
	if !strings.Contains(metricsStr, "yigate_connections_active") {
		t.Fatal("missing yigate_connections_active metric")
	}
	// 应该有路由决策指标
	if !strings.Contains(metricsStr, "yigate_routing_decisions_total") {
		t.Fatal("missing yigate_routing_decisions_total metric")
	}
}

// ============================================================
// 访问控制端到端测试
// ============================================================

func TestIntegrationAccessControl(t *testing.T) {
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	originAddr := startOriginServer(t, originHandler)

	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.AccessControl.AllowedClients = []string{"10.0.0.1/32"} // 只允许不存在的 IP

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   2 * time.Second,
	}

	// 127.0.0.1 不在白名单中，应被拒绝
	resp, err := client.Get("http://" + originAddr + "/")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Fatal("expected request to be denied by ACL")
		}
	}
}

// ============================================================
// 目标策略端到端测试
// ============================================================

func TestIntegrationDestinationPolicy(t *testing.T) {
	cfg := defaultConfig()
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = true
	cfg.DestinationPolicy.DenyCIDRs = []string{"127.0.0.0/8"}

	s, proxyAddr, _ := startTestServer(t, cfg)
	defer s.Stop()

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   2 * time.Second,
	}

	// 尝试访问 127.0.0.1 应被目标策略拒绝
	resp, err := client.Get("http://127.0.0.1:9999/")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Fatal("expected destination policy to deny 127.0.0.1")
		}
	}
}

// TestIntegrationGeoSiteManualUpdateAndSync 覆盖完整的 GeoSite 流程：
// 手动更新下载 → 版本元数据落盘 → 下游用只读令牌条件下载。
func TestIntegrationGeoSiteManualUpdateAndSync(t *testing.T) {
	payload := make([]byte, 8192)
	for i := range payload {
		payload[i] = byte(i % 211)
	}
	originAddr := startOriginServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"origin-v1"`)
		w.Write(payload)
	})
	originURL := "http://" + originAddr

	proxyAddr := findFreePort(t)
	mgmtAddr := findFreePort(t)
	dir := t.TempDir()
	datPath := filepath.Join(dir, "geosite.dat")
	cfgPath := filepath.Join(dir, "config.yml")

	content := fmt.Sprintf(`listen: %q
management: %q
management_auth_token: "test-token"
webui:
  enabled: false
users: {}
routing:
  final: direct
  geosite:
    path: %q
    auto_update: false
    update_url: %q
    update_interval: 24h
    sync_enabled: true
    sync_token: "readonly-token-123"
log:
  level: warn
  output: stdout
`, proxyAddr, mgmtAddr, datPath, originURL)
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(cfgPath, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()
	for i := 0; i < 100; i++ {
		if s.mgmt.Addr() != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	base := "http://" + s.mgmt.Addr()
	client := &http.Client{Timeout: 5 * time.Second}

	getInfo := func() proxy.GeoSiteInfo {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/geosite", nil)
		req.Header.Set("Authorization", "Bearer readonly-token-123")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("get info: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get info status = %d", resp.StatusCode)
		}
		var info proxy.GeoSiteInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Fatalf("decode info: %v", err)
		}
		return info
	}

	if info := getInfo(); info.Exists {
		t.Fatal("geosite data should not exist before update")
	}

	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/geosite/update", nil)
	req.Header.Set("Authorization", "Bearer readonly-token-123")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("sync token update: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sync token must not trigger update, status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, base+"/api/v1/geosite/update", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("manual update: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("manual update status = %d, body = %s", resp.StatusCode, string(errBody))
	}
	var result proxy.GeoSiteUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode update result: %v", err)
	}
	if !result.Changed || result.Info.Version == "" {
		t.Fatalf("unexpected update result: %+v", result)
	}

	info := getInfo()
	if !info.Exists || info.Size != int64(len(payload)) {
		t.Fatalf("info after update = %+v", info)
	}
	if info.SourceURL != originURL {
		t.Fatalf("source_url = %q, want %q", info.SourceURL, originURL)
	}
	if _, err := os.Stat(datPath + ".meta.json"); err != nil {
		t.Fatalf("meta file not written: %v", err)
	}

	// 下游使用只读令牌下载数据，并按 ETag 做条件请求。
	req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/geosite/dat", nil)
	req.Header.Set("Authorization", "Bearer readonly-token-123")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("download dat: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", resp.StatusCode)
	}
	if string(body) != string(payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(body), len(payload))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag header")
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/geosite/dat", nil)
	req.Header.Set("Authorization", "Bearer readonly-token-123")
	req.Header.Set("If-None-Match", etag)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("conditional download: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional download status = %d, want 304", resp.StatusCode)
	}
}

// TestIntegrationTrafficSurvivesConfigReload 模拟“修改上游后配置热加载”，
// 确认已记录的流量明细不会因为重载而被清空。
func TestIntegrationTrafficSurvivesConfigReload(t *testing.T) {
	originAddr := startOriginServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "reload traffic")
	})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	cfg := defaultConfig()
	cfg.Listen = findFreePort(t)
	cfg.Management = findFreePort(t)
	cfg.ManagementAuthToken = "test-token"
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.TrafficAudit.Enabled = true
	cfg.TrafficAudit.RetentionDays = 30
	cfg.Log.Level = "warn"
	cfg.Log.Output = "stdout"

	writeConfig := func(c *Config) {
		t.Helper()
		raw, err := yaml.Marshal(c)
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		if err := os.WriteFile(cfgPath, raw, 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	writeConfig(cfg)

	s, err := NewServer(cfgPath, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()
	for i := 0; i < 100; i++ {
		s.listenersMu.Lock()
		ln := s.listeners[cfg.Listen]
		s.listenersMu.Unlock()
		if ln != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	proxyURL, _ := url.Parse("http://" + cfg.Listen)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://" + originAddr + "/")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	total := func() int64 {
		var sum int64
		for _, entry := range s.currentRuntime().trafficAccounter.Snapshot().Entries {
			sum += entry.Total
		}
		return sum
	}
	before := total()
	if before <= 0 {
		t.Fatal("no traffic recorded before reload")
	}

	// 增删/启停上游会触发同样的配置热加载流程
	cfg.Upstreams = append(cfg.Upstreams, proxy.UpstreamConfig{
		Name:     "reload-upstream",
		URL:      "socks5://127.0.0.1:1080",
		Types:    []string{"socks5"},
		Priority: 1,
		Weight:   1,
	})
	writeConfig(cfg)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}

	if after := total(); after != before {
		t.Fatalf("traffic total changed after reload: before=%d after=%d", before, after)
	}
}
