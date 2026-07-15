package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yigate/proxy"
)

func writeTestConfig(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestServerStartStopTLS(t *testing.T) {
	certFile, keyFile, _ := generateTestCerts(t)
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.TLS.CertFile = certFile
	cfg.TLS.KeyFile = keyFile

	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	ln := waitForFirstListener(t, s)
	addr := ln.Addr().String()
	if addr == "" {
		t.Fatal("server did not listen")
	}

	// TLS 连接应成功
	config := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("tls dial server: %v", err)
	}
	conn.Close()

	// 裸 TCP 连接不应能完成 TLS 握手，但端口应可达
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	raw.Close()

	s.Stop()
}

func TestServerTLSRequiresClientCert(t *testing.T) {
	certFile, keyFile, caFile := generateTestCerts(t)
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.TLS.CertFile = certFile
	cfg.TLS.KeyFile = keyFile
	cfg.TLS.ClientCAFile = caFile

	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()

	ln := waitForFirstListener(t, s)
	addr := ln.Addr().String()
	if addr == "" {
		t.Fatal("server did not listen")
	}

	// 未提供客户端证书，TLS 握手在连接层面成功，但首次 IO 应失败
	config := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		// 某些 Go 版本会在 Dial 时直接失败，也算验证通过
		return
	}
	defer conn.Close()
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected TLS IO to fail without client cert")
	}
}

func TestServerLoadAndApplyTLSChange(t *testing.T) {
	certFile, keyFile, _ := generateTestCerts(t)
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
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
	ln := waitForFirstListener(t, s)
	oldAddr = ln.Addr().String()
	if oldAddr == "" {
		t.Fatal("server not listening")
	}

	_ = writeTestConfig(t, dir, fmt.Sprintf(`listen: 127.0.0.1:0
tls:
  cert_file: %s
  key_file: %s
log:
  level: warn
  output: stdout
`, certFile, keyFile))
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}

	// 新 listener 可能监听在新端口，获取当前地址后再验证 TLS
	newLn := waitForFirstListener(t, s)
	newAddr := newLn.Addr().String()
	if newAddr == "" {
		t.Fatal("server not listening after reload")
	}

	config := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", newAddr, config)
	if err != nil {
		t.Fatalf("tls dial after reload: %v", err)
	}
	conn.Close()
}

func generateTestCerts(t *testing.T) (certFile, keyFile, caFile string) {
	t.Helper()
	dir := t.TempDir()

	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"YiGate Test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})
	caFile = filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, caPEM, 0644); err != nil {
		t.Fatal(err)
	}

	serverCert := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "yigate"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"yigate"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serverBytes, err := x509.CreateCertificate(rand.Reader, serverCert, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0644); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, caFile
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

func waitForFirstListener(t *testing.T, s *Server) net.Listener {
	t.Helper()
	for i := 0; i < 50; i++ {
		s.listenersMu.Lock()
		for _, ln := range s.listeners {
			s.listenersMu.Unlock()
			return ln
		}
		s.listenersMu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not listen")
	return nil
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
	ln := waitForFirstListener(t, s)
	addr := ln.Addr().String()

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
	s.listenersMu.Lock()
	s.listeners[cfg.Listen] = ln
	s.listenersMu.Unlock()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.handle(conn, cfg.Listen)
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
	s.listenersMu.Lock()
	s.listeners[cfg.Listen] = ln
	s.listenersMu.Unlock()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.handle(conn, cfg.Listen)
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

	if !s.acquireConnSlot(c1, 0) {
		t.Fatal("expected acquire with no limit to pass")
	}
	s.releaseConnSlot(c1)

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
	if !s2.acquireConnSlot(c2, 1) {
		t.Fatal("expected first acquire to pass")
	}
	c3, _ := net.Pipe()
	defer c3.Close()
	if s2.acquireConnSlot(c3, 1) {
		t.Fatal("expected second acquire to fail")
	}
	s2.releaseConnSlot(c2)
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
	cfg.Management = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.Stop()
	snapshot := s.statusSnapshot()
	if snapshot.Status != "up" {
		t.Fatalf("expected status up, got %q", snapshot.Status)
	}
	if snapshot.Listen != "127.0.0.1:4479" {
		t.Fatalf("expected listen 127.0.0.1:4479, got %q", snapshot.Listen)
	}
	if snapshot.Management != "127.0.0.1:0" {
		t.Fatalf("expected management 127.0.0.1:0, got %q", snapshot.Management)
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
	ln := waitForFirstListener(t, s)
	oldAddr = ln.Addr().String()
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

	newLn := waitForFirstListener(t, s)
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

func TestServerLoadAndApplyManagementPortChange(t *testing.T) {
	dir := t.TempDir()
	mgmtLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mgmtAddr := mgmtLn.Addr().String()
	mgmtLn.Close()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: `+mgmtAddr+`
management_auth_token: token
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

	_ = writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: token
log:
  level: warn
  output: stdout
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}
	newAddr := s.mgmt.Addr()
	if newAddr == "" || newAddr == oldAddr {
		t.Fatalf("management addr = %q, old = %q", newAddr, oldAddr)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+newAddr+"/health", nil)
	req.Header.Set("Authorization", "Bearer token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("new management health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestServerLoadAndApplyManagementFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: old-token
routing:
  final: direct
log:
  level: warn
  output: stdout
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	oldState := s.currentRuntime()
	oldMgmtAddr := s.managementServer().Addr()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_ = writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: `+occupied.Addr().String()+`
management_auth_token: new-token
routing:
  final: proxy
log:
  level: warn
  output: stdout
`)
	if err := s.LoadAndApply(); err == nil {
		t.Fatal("expected management listener conflict")
	}
	if s.currentRuntime() != oldState {
		t.Fatal("runtime state changed after failed reload")
	}
	if got := s.managementServer().Addr(); got != oldMgmtAddr {
		t.Fatalf("management addr changed to %q, want %q", got, oldMgmtAddr)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+oldMgmtAddr+"/health", nil)
	req.Header.Set("Authorization", "Bearer old-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("old management server unavailable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("old management status = %d", resp.StatusCode)
	}
}

func TestServerConnCountAcrossLimitReload(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.MaxConnections = 1
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	c1, peer1 := net.Pipe()
	defer peer1.Close()
	if !s.acquireConnSlot(c1, s.maxConns.Load()) {
		t.Fatal("first connection rejected")
	}
	if got := s.connCount.Load(); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	s.maxConns.Store(0)
	c2, peer2 := net.Pipe()
	defer peer2.Close()
	if !s.acquireConnSlot(c2, s.maxConns.Load()) {
		t.Fatal("unlimited connection rejected")
	}
	if got := s.connCount.Load(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	s.releaseConnSlot(c1)
	s.maxConns.Store(1)
	c3, peer3 := net.Pipe()
	defer peer3.Close()
	if s.acquireConnSlot(c3, s.maxConns.Load()) {
		t.Fatal("connection should be rejected while one remains active")
	}
	s.releaseConnSlot(c2)
	if got := s.connCount.Load(); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
}

func TestServerShutdownClosesActiveConnectionsOnTimeout(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn, peer := net.Pipe()
	defer peer.Close()
	if !s.acquireConnSlot(conn, 0) {
		t.Fatal("connection rejected")
	}
	go func() {
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		s.releaseConnSlot(conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	if got := s.connCount.Load(); got != 0 {
		t.Fatalf("active count = %d, want 0", got)
	}
}

func TestServerLoadAndApplyRuntimeComponents(t *testing.T) {
	dir := t.TempDir()
	oldLog := filepath.Join(dir, "old.log")
	newLog := filepath.Join(dir, "new.log")
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
log:
  level: warn
  output: file
  file: `+oldLog+`
routing:
  final: direct
timeouts:
  dial: 1s
access_control:
  allowed_clients:
    - 127.0.0.1/32
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

	_ = writeTestConfig(t, dir, `listen: 127.0.0.1:0
log:
  level: warn
  output: file
  file: `+newLog+`
upstreams:
  - name: up1
    url: http://127.0.0.1:1
routing:
  final: proxy
timeouts:
  dial: 2s
access_control:
  allowed_clients:
    - 10.0.0.0/8
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}
	if len(s.Config().Upstreams) != 1 || s.Manager().SelectFor([]string{"up1"}) == nil {
		t.Fatal("expected upstream manager reloaded")
	}
	if got := s.Router().Decide("example.com:80").Action; got != "proxy" {
		t.Fatalf("routing final = %q, want proxy", got)
	}
	if proxy.ProxyDialTimeout() != 2*time.Second {
		t.Fatalf("dial timeout = %v, want 2s", proxy.ProxyDialTimeout())
	}
	if len(s.Config().Listeners) == 0 {
		t.Fatal("expected listeners after reload")
	}
	listenerAddr := s.Config().Listeners[0].Listen
	if s.currentRuntime().listeners[listenerAddr].access.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}) {
		t.Fatal("expected reloaded access control to deny 127.0.0.1")
	}
	proxy.Logger().Warn("new logger active")
	proxy.SyncLogger()
	if _, err := os.Stat(newLog); err != nil {
		t.Fatalf("expected new log file: %v", err)
	}
}

func TestServerStartMultipleListeners(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.Listeners = []ListenerConfig{
		{Listen: "127.0.0.1:0"},
		{Listen: "127.0.0.2:0"},
	}
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() {
		if err := s.Start(); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()
	defer s.Stop()

	addrs := make(map[string]bool)
	for i := 0; i < 50 && len(addrs) < 2; i++ {
		s.listenersMu.Lock()
		for _, ln := range s.listeners {
			addrs[ln.Addr().String()] = true
		}
		s.listenersMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if len(addrs) != 2 {
		t.Fatalf("expected 2 listeners, got %d", len(addrs))
	}

	for addr := range addrs {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		conn.Close()
	}
}

func TestServerMultiListenerHotReload(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: token
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

	oldAddr := waitForFirstListener(t, s).Addr().String()

	_ = writeTestConfig(t, dir, `listeners:
  - listen: 127.0.0.1:0
  - listen: 127.0.0.2:0
management: 127.0.0.1:0
management_auth_token: token
log:
  level: warn
  output: stdout
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}

	var ln1, ln2 net.Listener
	for i := 0; i < 50 && (ln1 == nil || ln2 == nil); i++ {
		s.listenersMu.Lock()
		ln1 = s.listeners["127.0.0.1:0"]
		ln2 = s.listeners["127.0.0.2:0"]
		s.listenersMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if ln1 == nil || ln2 == nil {
		t.Fatalf("expected 2 listeners after reload, got ln1=%v ln2=%v", ln1, ln2)
	}

	// 原 listener 配置未变，应被复用或至少仍在监听
	if ln1.Addr().String() != oldAddr && ln2.Addr().String() != oldAddr {
		t.Fatalf("old listen address %s should still be active", oldAddr)
	}

	for _, ln := range []net.Listener{ln1, ln2} {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial %s: %v", ln.Addr().String(), err)
		}
		conn.Close()
	}
}

func TestServerMultiListenerPerListenerUsers(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listeners:
  - listen: 127.0.0.1:0
    users:
      alice: alice-pass
  - listen: 127.0.0.2:0
    users:
      bob: bob-pass
management: 127.0.0.1:0
management_auth_token: token
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

	addrs := make(map[string]bool)
	for i := 0; i < 50 && len(addrs) < 2; i++ {
		s.listenersMu.Lock()
		for _, ln := range s.listeners {
			addrs[ln.Addr().String()] = true
		}
		s.listenersMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if len(addrs) != 2 {
		t.Fatalf("expected 2 listeners, got %d", len(addrs))
	}

	// 验证每个 listener 的运行时上下文都有独立的用户配置
	rt := s.currentRuntime()
	if rt == nil || len(rt.listeners) != 2 {
		t.Fatal("expected 2 listener runtimes")
	}
	for addr, lr := range rt.listeners {
		if len(lr.config.Users) == 0 {
			t.Fatalf("listener %s has no users", addr)
		}
	}
}

