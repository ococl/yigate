package proxy

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestRewriteSOCKS5TargetFromHTTPHost(t *testing.T) {
	initial := []byte("GET / HTTP/1.1\r\nHost: www.google.com\r\nUser-Agent: k5pw-test\r\n\r\n")
	got := rewriteSOCKS5TargetFromInitial("142.250.72.14:80", initial)
	if got != "www.google.com:80" {
		t.Fatalf("rewriteSOCKS5TargetFromInitial() = %q", got)
	}
}

func TestRewriteSOCKS5TargetFromTLSSNI(t *testing.T) {
	initial := tlsClientHello(t, "www.youtube.com")
	got := rewriteSOCKS5TargetFromInitial("142.250.72.14:443", initial)
	if got != "www.youtube.com:443" {
		t.Fatalf("rewriteSOCKS5TargetFromInitial() = %q", got)
	}
}

func TestPreferIPv4(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("::1")},
		{IP: net.ParseIP("127.0.0.1")},
	}
	got := preferIPv4(addrs)
	if len(got) != 2 || got[0].Equal(net.ParseIP("127.0.0.1")) {
		// ok
	} else {
		t.Fatalf("preferIPv4 = %#v", got)
	}
}

func TestShouldSniffSOCKS5Target(t *testing.T) {
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "up", URL: "socks5://127.0.0.1:1080"}})
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect, Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"up"}}})
	if !shouldSniffSOCKS5Target("127.0.0.1:443", mgr, router) {
		t.Fatal("expected sniff for IP target")
	}
	if shouldSniffSOCKS5Target("example.com:443", mgr, router) {
		t.Fatal("expected no sniff for domain")
	}
	if shouldSniffSOCKS5Target("127.0.0.1:443", mgr, nil) {
		t.Fatal("expected no sniff when router nil")
	}
}

func TestHasEnoughInitialBytes(t *testing.T) {
	if hasEnoughInitialBytes(nil) {
		t.Fatal("expected false for nil")
	}
	tls := tlsClientHello(t, "example.com")
	if !hasEnoughInitialBytes(tls) {
		t.Fatal("expected true for complete TLS ClientHello")
	}
	http := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	if !hasEnoughInitialBytes(http) {
		t.Fatal("expected true for HTTP request")
	}
	if !hasEnoughInitialBytes([]byte{'?'}) {
		t.Fatal("expected true for non-HTTP/non-TLS")
	}
}

func TestSniffHTTPHost(t *testing.T) {
	if got := sniffHTTPHost([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")); got != "example.com" {
		t.Fatalf("sniffHTTPHost = %q", got)
	}
	if got := sniffHTTPHost([]byte("not http")); got != "" {
		t.Fatalf("sniffHTTPHost = %q", got)
	}
}

func TestWriteSOCKS5ConnectReply(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	go func() {
		defer c2.Close()
		writeSOCKS5ConnectReply(c2, 0x00)
	}()
	buf := make([]byte, 10)
	c1.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c1.Read(buf); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		t.Fatalf("reply = %#v", buf[:10])
	}
}

func tlsClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tls.Client(client, &tls.Config{ServerName: serverName, InsecureSkipVerify: true}).Handshake()
	}()
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8192)
	n, err := server.Read(buf)
	client.Close()
	<-done
	if err != nil {
		t.Fatalf("read TLS ClientHello: %v", err)
	}
	return buf[:n]
}
