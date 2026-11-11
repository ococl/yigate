package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// checkAllForTest 重置所有上游的 nextCheckAt 计时器并立即执行 checkDue，
// 用于测试中强制检查所有上游而无需等待检查间隔。
func (m *UpstreamManager) checkAllForTest(globalInterval int) {
	m.mu.Lock()
	for _, u := range m.upstreams {
		u.nextCheckAt = time.Time{}
	}
	m.mu.Unlock()
	m.checkDue(globalInterval)
}

func TestUpstreamHealthCheck(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "s5", URL: "socks5://" + proxyAddr, Priority: 1, Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(1)
	defer mgr.StopHealthCheck()

	time.Sleep(1200 * time.Millisecond)
	if up := mgr.Select(); up == nil || up.dead {
		t.Fatal("expected upstream alive after health check")
	}
}

func TestUpstreamHealthStatusMetric(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "alive", URL: "socks5://" + proxyAddr},
		{Name: "dead", URL: "socks5://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	MetricSet("yigate_upstream_health_status", -1, "upstream=alive")
	MetricSet("yigate_upstream_health_status", -1, "upstream=dead")

	// 连续失败达到阈值才标记 dead。
	for i := 0; i < deadFailureThreshold; i++ {
		mgr.checkAllForTest(30)
	}

	if got := MetricValue("yigate_upstream_health_status", "upstream=alive"); got != 1 {
		t.Fatalf("alive health metric = %v, want 1", got)
	}
	if got := MetricValue("yigate_upstream_health_status", "upstream=dead"); got != 0 {
		t.Fatalf("dead health metric = %v, want 0", got)
	}
	if up := mgr.byName["dead"]; up == nil || !up.dead {
		t.Fatal("expected dead upstream marked dead")
	}
}

func TestCheckHTTPProtocol(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, _ := http.ReadRequest(br)
				if req == nil {
					return
				}
				c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
			}(conn)
		}
	}()

	up := &Upstream{URL: mustURL("http://" + ln.Addr().String())}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if !checkHTTPProtocol(conn, up, 5*time.Second) {
		t.Fatal("expected HTTP protocol check to pass")
	}
}

func TestCheckHTTPProtocolCustomPathAndCodes(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, _ := http.ReadRequest(br)
				if req == nil {
					return
				}
				if req.URL.Path == "/health" {
					c.Write([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
				} else {
					c.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
				}
			}(conn)
		}
	}()

	up := &Upstream{
		URL: mustURL("http://" + ln.Addr().String()),
		HealthCheck: HealthCheckConfig{
			Type:          "http",
			Target:        ln.Addr().String(),
			Path:          "/health",
			ExpectedCodes: []int{204},
			Timeout:       Duration(5 * time.Second),
		},
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if !checkHTTPProtocol(conn, up, 5*time.Second) {
		t.Fatal("expected custom path/code check to pass")
	}
}

func TestCheckTLSProtocol(t *testing.T) {
	certFile, keyFile, _ := generateTestCerts(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				c.Read(buf)
			}(conn)
		}
	}()

	old := tlsConfigForHost
	tlsConfigForHost = func(addr string) *tls.Config { return &tls.Config{InsecureSkipVerify: true} }
	defer func() { tlsConfigForHost = old }()

	up := &Upstream{URL: mustURL("https://" + ln.Addr().String())}
	if !checkTLSUpstream(ln.Addr().String(), 5*time.Second) {
		t.Fatal("expected TLS check to pass")
	}
	_ = up
}

func TestHealthCheckPerUpstreamFailureThreshold(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "dead", URL: "http://127.0.0.1:1", HealthCheck: HealthCheckConfig{FailureThreshold: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.checkAllForTest(30)
	if mgr.byName["dead"].dead {
		t.Fatal("expected dead upstream not marked dead after 1 failure")
	}
	mgr.checkAllForTest(30)
	if !mgr.byName["dead"].dead {
		t.Fatal("expected dead upstream marked dead after 2 failures")
	}
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

func TestCheckSOCKS5ProtocolCompletesUsernamePasswordAuthentication(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	up := &Upstream{URL: &url.URL{Scheme: "socks5", Host: "proxy:1080", User: url.UserPassword("alice", "secret")}}
	serverDone := make(chan bool, 1)
	go func() {
		var greeting [4]byte
		if _, err := io.ReadFull(server, greeting[:]); err != nil {
			serverDone <- false
			return
		}
		if !bytes.Equal(greeting[:], []byte{0x05, 0x02, 0x00, 0x02}) {
			serverDone <- false
			return
		}
		if _, err := server.Write([]byte{0x05, 0x02}); err != nil {
			serverDone <- false
			return
		}
		wantAuth := append([]byte{0x01, 0x05}, []byte("alice")...)
		wantAuth = append(wantAuth, 0x06)
		wantAuth = append(wantAuth, []byte("secret")...)
		auth := make([]byte, len(wantAuth))
		if _, err := io.ReadFull(server, auth); err != nil || !bytes.Equal(auth, wantAuth) {
			serverDone <- false
			return
		}
		_, err := server.Write([]byte{0x01, 0x00})
		serverDone <- err == nil
	}()

	if !checkSOCKS5Protocol(client, up, time.Second) {
		t.Fatal("authenticated SOCKS5 protocol check must succeed")
	}
	if !<-serverDone {
		t.Fatal("health probe did not complete expected username/password exchange")
	}
}

func TestCheckSOCKS5ProtocolNoAuthSucceeds(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	up := &Upstream{URL: &url.URL{Scheme: "socks5", Host: "proxy:1080"}}
	serverDone := make(chan bool, 1)
	go func() {
		var greeting [3]byte
		if _, err := io.ReadFull(server, greeting[:]); err != nil {
			serverDone <- false
			return
		}
		if !bytes.Equal(greeting[:], []byte{0x05, 0x01, 0x00}) {
			serverDone <- false
			return
		}
		_, err := server.Write([]byte{0x05, 0x00})
		serverDone <- err == nil
	}()

	if !checkSOCKS5Protocol(client, up, time.Second) {
		t.Fatal("no-auth SOCKS5 protocol check must succeed")
	}
	if !<-serverDone {
		t.Fatal("server did not receive expected no-auth greeting")
	}
}

func TestCheckSOCKS5ProtocolBadCredentialsFails(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	up := &Upstream{URL: &url.URL{Scheme: "socks5", Host: "proxy:1080", User: url.UserPassword("alice", "wrong")}}
	serverDone := make(chan bool, 1)
	go func() {
		var greeting [4]byte
		io.ReadFull(server, greeting[:])
		server.Write([]byte{0x05, 0x02})
		auth := make([]byte, 16)
		n, _ := server.Read(auth)
		_ = n
		server.Write([]byte{0x01, 0x01}) // auth failure
		serverDone <- true
	}()

	if checkSOCKS5Protocol(client, up, time.Second) {
		t.Fatal("bad credentials must cause SOCKS5 protocol check to fail")
	}
	<-serverDone
}

func TestValidateHealthCheckConfig(t *testing.T) {
	valid := []HealthCheckConfig{
		{},
		{Type: "socks5", Interval: Duration(time.Second), Timeout: Duration(time.Second), FailureThreshold: 1, SuccessThreshold: 2},
		{Type: "http", Path: "/health", ExpectedCodes: []int{200, 204}},
	}
	for _, config := range valid {
		if err := ValidateHealthCheckConfig(config); err != nil {
			t.Fatalf("ValidateHealthCheckConfig(%+v): %v", config, err)
		}
	}
	invalid := []HealthCheckConfig{
		{Type: "smtp"},
		{Interval: Duration(-time.Second)},
		{Timeout: Duration(-time.Second)},
		{FailureThreshold: -1},
		{SuccessThreshold: -1},
		{Type: "http", Path: "health"},
		{Type: "http", ExpectedCodes: []int{99}},
		{Type: "http", ExpectedCodes: []int{600}},
	}
	for _, config := range invalid {
		if err := ValidateHealthCheckConfig(config); err == nil {
			t.Fatalf("ValidateHealthCheckConfig(%+v) unexpectedly succeeded", config)
		}
	}
}

func TestNormalizeHealthCheckTypeDefaultsSocks5(t *testing.T) {
	if got := normalizeHealthCheckType("", ProxyTypeSocks5); got != "socks5" {
		t.Fatalf("expected socks5 default for socks5 upstream, got %q", got)
	}
	if got := normalizeHealthCheckType("", ProxyTypeSocks5H); got != "socks5" {
		t.Fatalf("expected socks5 default for socks5h upstream, got %q", got)
	}
	if got := normalizeHealthCheckType("", ProxyTypeHTTP); got != "http" {
		t.Fatalf("expected http default for http upstream, got %q", got)
	}
	if got := normalizeHealthCheckType("", ""); got != "tcp" {
		t.Fatalf("expected tcp default, got %q", got)
	}
}

func TestStartHealthCheckZeroStopsExistingChecker(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u1", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(1)
	mgr.StartHealthCheck(0) // must stop existing checker without panic
	// Verify done is nil (stopped)
	mgr.mu.Lock()
	done := mgr.done
	mgr.mu.Unlock()
	if done != nil {
		t.Fatal("expected health check stopped after StartHealthCheck(0)")
	}
}

// TestCheckHTTPProtocolLivenessAcceptsBusyResponses 验证浅探测只关心
// "上游是否按协议应答"：403/502/503 等说明节点在服务，不应判死；
// 401/407 说明凭据不可用，仍判为不可用。
func TestCheckHTTPProtocolLivenessAcceptsBusyResponses(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{200, true},
		{403, true},
		{502, true},
		{503, true},
		{401, false},
		{407, false},
	}
	for _, tc := range cases {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		status := http.StatusText(tc.code)
		go func(ln net.Listener, code int, status string) {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			br := bufio.NewReader(conn)
			req, err := http.ReadRequest(br)
			if err != nil {
				return
			}
			if req.Method != http.MethodConnect {
				// 浅探测必须是合法的 CONNECT 请求，而不是 origin-form GET。
				conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
				return
			}
			conn.Write([]byte("HTTP/1.1 " + strconv.Itoa(code) + " " + status + "\r\nContent-Length: 0\r\n\r\n"))
		}(ln, tc.code, status)

		up := &Upstream{
			URL:         mustURL("http://" + ln.Addr().String()),
			HealthCheck: HealthCheckConfig{Type: "http", Timeout: Duration(2 * time.Second)},
		}
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			ln.Close()
			t.Fatal(err)
		}
		got := checkHTTPProtocol(conn, up, 2*time.Second)
		conn.Close()
		ln.Close()
		if got != tc.want {
			t.Errorf("status %d: alive = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// TestCheckHTTPProtocolDeepTarget 验证配置 target 后按真实 CONNECT 结果判定。
func TestCheckHTTPProtocolDeepTarget(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				if req.Method == http.MethodConnect && req.Host == "target.example:443" {
					c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
					return
				}
				c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			}(conn)
		}
	}()

	ok := &Upstream{
		URL: mustURL("http://" + ln.Addr().String()),
		HealthCheck: HealthCheckConfig{
			Type: "http", Target: "target.example:443", Timeout: Duration(2 * time.Second),
		},
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if !checkHTTPProtocol(conn, ok, 2*time.Second) {
		conn.Close()
		t.Fatal("expected deep probe to pass on 200 Connection Established")
	}
	conn.Close()

	bad := &Upstream{
		URL: mustURL("http://" + ln.Addr().String()),
		HealthCheck: HealthCheckConfig{
			Type: "http", Target: "blocked.example:443", Timeout: Duration(2 * time.Second),
		},
	}
	conn, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if checkHTTPProtocol(conn, bad, 2*time.Second) {
		t.Fatal("expected deep probe to fail on 502")
	}
}

// TestCheckSOCKS5ProtocolDeepTarget 验证配置 target 后真实发起 CONNECT。
func TestCheckSOCKS5ProtocolDeepTarget(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	targetLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetLn.Close()

	reachable := &Upstream{
		URL:         mustURL("socks5://" + proxyAddr),
		HealthCheck: HealthCheckConfig{Type: "socks5", Target: targetLn.Addr().String()},
	}
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	if !checkSOCKS5Protocol(conn, reachable, 3*time.Second) {
		conn.Close()
		t.Fatal("expected deep socks5 probe to pass for reachable target")
	}
	conn.Close()

	unreachable := &Upstream{
		URL:         mustURL("socks5://" + proxyAddr),
		HealthCheck: HealthCheckConfig{Type: "socks5", Target: "127.0.0.1:1"},
	}
	conn, err = net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if checkSOCKS5Protocol(conn, unreachable, 3*time.Second) {
		t.Fatal("expected deep socks5 probe to fail for unreachable target")
	}
}

// TestCheckHTTPProtocolDeepTargetDoesNotBlockOnTunnel 验证深探测在拿到
// "200 Connection Established" 后立即返回，不会去读隧道 body 卡到超时。
func TestCheckHTTPProtocolDeepTargetDoesNotBlockOnTunnel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			br := bufio.NewReader(c)
			if _, err := http.ReadRequest(br); err != nil {
				c.Close()
				continue
			}
			c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			// 故意保持隧道打开，模拟真实代理不断开连接。
			time.AfterFunc(3*time.Second, func() { c.Close() })
		}
	}()

	up := &Upstream{
		URL: mustURL("http://" + ln.Addr().String()),
		HealthCheck: HealthCheckConfig{
			Type: "http", Target: "example.com:443", Timeout: Duration(2 * time.Second),
		},
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	if !checkHTTPProtocol(conn, up, 2*time.Second) {
		t.Fatal("expected deep probe to pass")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("deep probe blocked for %v on an open tunnel", elapsed)
	}
}

// TestProbeDeadUpstreamsRespectsConcurrencyLimit 验证周期恢复探测与手动巡检
// 共用同一并发上限：6 个上游同时到期、上限为 2 时，服务端看到的并发峰值不得超过 2。
func TestProbeDeadUpstreamsRespectsConcurrencyLimit(t *testing.T) {
	var cur, peak atomic.Int64
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				now := cur.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				defer cur.Add(-1)
				br := bufio.NewReader(c)
				if _, err := http.ReadRequest(br); err != nil {
					return
				}
				// 让每次探测持续一段时间，才能真实暴露并发度。
				time.Sleep(250 * time.Millisecond)
				_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
			}(c)
		}
	}()

	const upstreamCount = 6
	configs := make([]UpstreamConfig, 0, upstreamCount)
	for i := 0; i < upstreamCount; i++ {
		configs = append(configs, UpstreamConfig{
			Name: fmt.Sprintf("u%d", i),
			URL:  "http://" + ln.Addr().String(),
			HealthCheck: HealthCheckConfig{
				Type: "http", Timeout: Duration(2 * time.Second),
			},
		})
	}
	m, err := NewUpstreamManager(configs)
	if err != nil {
		t.Fatal(err)
	}
	m.probeSem = make(chan struct{}, 2)
	m.mu.Lock()
	for _, up := range m.upstreams {
		up.dead = true
		up.nextProbeAt = time.Now().Add(-time.Second)
	}
	m.mu.Unlock()

	m.probeDeadUpstreams()
	if got := peak.Load(); got > 2 {
		t.Fatalf("probe concurrency peak %d exceeds limit 2", got)
	}
	if peak.Load() == 0 {
		t.Fatal("no probe reached the upstream, test is not exercising anything")
	}
}
