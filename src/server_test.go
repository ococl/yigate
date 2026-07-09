package main

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestNewServer(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.Config() != cfg {
		t.Fatal("Config() mismatch")
	}
	if s.Manager() == nil || s.Router() == nil {
		t.Fatal("manager/router not ready")
	}
}

func TestServerStartStop(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	// 等待服务器监听并获取实际地址
	var addr string
	for i := 0; i < 50; i++ {
		s.lnMu.Lock()
		ln := s.ln
		s.lnMu.Unlock()
		if ln != nil {
			addr = ln.Addr().String()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not listen")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	conn.Close()

	s.Stop()
}

func TestServerHandleHTTP(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	s.ln = ln

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.handle(conn)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode == 0 {
		t.Fatal("expected status code")
	}
}

func TestServerLoadAndApply(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `
listen: 127.0.0.1:0
log:
  level: info
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}
	if s.Config() == nil {
		t.Fatal("config not loaded")
	}
}
func TestServerHandleSOCKS5(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	s.ln = ln

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.handle(conn)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// SOCKS5 auth greeting
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var authResp [2]byte
	if _, err := conn.Read(authResp[:]); err != nil {
		t.Fatalf("read auth: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth resp = %#v", authResp)
	}

	// CONNECT to a dummy address, it will fail but should not panic
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x00}
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write connect: %v", err)
	}
}

func TestServerConnLimit(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.MaxConnections = 0
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	c1, _ := net.Pipe()
	defer c1.Close()

	if !s.acquireConnSlot(c1) {
		t.Fatal("expected acquire with no limit to pass")
	}
	s.releaseConnSlot()

	cfg2 := defaultConfig()
	cfg2.Listen = "127.0.0.1:0"
	cfg2.MaxConnections = 1
	s2, err := NewServer("", cfg2)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s2 == nil {
		t.Fatalf("s2 is nil")
	}
	c2, _ := net.Pipe()
	defer c2.Close()
	if !s2.acquireConnSlot(c2) {
		t.Fatal("expected first acquire to pass")
	}
	c3, _ := net.Pipe()
	defer c3.Close()
	if s2.acquireConnSlot(c3) {
		t.Fatal("expected second acquire to fail")
	}
	s2.releaseConnSlot()
}

func TestServerApplyConfig(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.Management = "127.0.0.1:0"
	cfg.EnablePprof = true
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.mgmt == nil {
		t.Fatal("expected management server")
	}
	if !s.mgmt.PprofEnabled() {
		t.Fatal("expected pprof enabled")
	}
}

func TestServerWatchConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
log:
  level: info
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := s.watchConfig(); err != nil {
		t.Fatalf("watchConfig: %v", err)
	}
	defer s.Stop()

	// trigger a reload by touching the file
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	// wait for debounce
	time.Sleep(500 * time.Millisecond)
}

func TestServerStatusSnapshot(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:4479"
	cfg.Management = "127.0.0.1:8892"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	snapshot := s.statusSnapshot()
	if snapshot.Status != "up" {
		t.Fatalf("expected status up, got %q", snapshot.Status)
	}
	if snapshot.Listen != "127.0.0.1:4479" {
		t.Fatalf("expected listen 127.0.0.1:4479, got %q", snapshot.Listen)
	}
	if snapshot.Management != "127.0.0.1:8892" {
		t.Fatalf("expected management 127.0.0.1:8892, got %q", snapshot.Management)
	}
	if !snapshot.RouterReady || !snapshot.ManagerReady {
		t.Fatal("expected router and manager ready")
	}
}

func TestServerReloadViaManagementAPI(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: test-token
log:
  level: info
  output: stdout
`)
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
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()

	var mgmtAddr string
	for i := 0; i < 50; i++ {
		if addr := s.mgmt.Addr(); addr != "" {
			mgmtAddr = addr
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mgmtAddr == "" {
		t.Fatal("management server not ready")
	}

	req, _ := http.NewRequest(http.MethodPost, "http://"+mgmtAddr+"/api/v1/reload", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected reload 200, got %d", resp.StatusCode)
	}
}

func TestServerLoadAndApplyListenChange(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
log:
  level: warn
  output: stdout
`)
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
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()

	var oldAddr string
	for i := 0; i < 50; i++ {
		s.lnMu.Lock()
		ln := s.ln
		s.lnMu.Unlock()
		if ln != nil {
			oldAddr = ln.Addr().String()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if oldAddr == "" {
		t.Fatal("server not listening")
	}

	_ = writeTestConfig(t, dir, `listen: 0.0.0.0:0
management: 127.0.0.1:0
log:
  level: warn
  output: stdout
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}

	s.lnMu.Lock()
	newLn := s.ln
	s.lnMu.Unlock()
	if newLn == nil {
		t.Fatal("new listener is nil")
	}
	if newLn.Addr().String() == oldAddr {
		t.Fatalf("expected new listen address, got same %s", oldAddr)
	}
	if s.Config().Listen != "0.0.0.0:0" {
		t.Fatalf("expected config listen 0.0.0.0:0, got %q", s.Config().Listen)
	}
}

func TestServerLoadAndApplyManagementAuthAndPprofChange(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: old-token
enable_pprof: false
log:
  level: warn
  output: stdout
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.Stop()
	oldAddr := s.mgmt.Addr()
	if oldAddr == "" {
		t.Fatal("management server not ready")
	}
	if s.mgmt.AuthToken() != "old-token" || s.mgmt.PprofEnabled() {
		t.Fatal("unexpected initial management state")
	}

	_ = writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: new-token
enable_pprof: true
log:
  level: warn
  output: stdout
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}
	newAddr := s.mgmt.Addr()
	if newAddr == "" {
		t.Fatal("management server not restarted")
	}
	if s.mgmt.AuthToken() != "new-token" {
		t.Fatalf("auth token = %q, want new-token", s.mgmt.AuthToken())
	}
	if !s.mgmt.PprofEnabled() {
		t.Fatal("expected pprof enabled after reload")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+newAddr+"/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer new-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pprof request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pprof status = %d, want 200", resp.StatusCode)
	}
}
