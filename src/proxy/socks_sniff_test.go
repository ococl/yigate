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

func TestSOCKS5AddressBytesIPv6(t *testing.T) {
	addr, err := socks5AddressBytes("2001:db8::1")
	if err != nil {
		t.Fatalf("socks5AddressBytes() error = %v", err)
	}
	if len(addr) != 1+net.IPv6len || addr[0] != 0x04 {
		t.Fatalf("IPv6 SOCKS5 addr = %#v", addr)
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
