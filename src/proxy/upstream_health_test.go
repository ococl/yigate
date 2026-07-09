package proxy

import (
	"bufio"
	"net"
	"net/http"
	"testing"
	"time"
)

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
	if !checkHTTPProtocol(conn, up) {
		t.Fatal("expected HTTP protocol check to pass")
	}
}

func TestCheckSOCKS5Protocol(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	up := &Upstream{URL: mustURL("socks5://" + proxyAddr)}
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if !checkSOCKS5Protocol(conn, up) {
		t.Fatal("expected SOCKS5 protocol check to pass")
	}
}
