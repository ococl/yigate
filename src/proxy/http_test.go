package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func startTestHTTPServer(t *testing.T, router *Router) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	quit := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-quit:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				br := bufio.NewReader(c)
				HandleHTTP(c, br, nil, nil, router, nil)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		close(quit)
		ln.Close()
	}
}

func TestHTTPProxyRequest(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	addr, stop := startTestHTTPServer(t, router)
	defer stop()

	target := httptestServer(t, "hello http")

	u, _ := url.Parse("http://" + addr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://" + target)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello http" {
		t.Fatalf("body = %q, want %q", body, "hello http")
	}
}

func TestHTTPProxyCONNECT(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	addr, stop := startTestHTTPServer(t, router)
	defer stop()

	// 目标 echo 服务器
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	defer echoLn.Close()
	go func() {
		c, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		c.Write(buf[:n])
	}()
	target := echoLn.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := conn.Write([]byte("hello connect")); err != nil {
		t.Fatalf("write tunnel: %v", err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read tunnel: %v", err)
	}
	if got := string(buf[:n]); got != "hello connect" {
		t.Fatalf("tunneled = %q, want %q", got, "hello connect")
	}
}

func httptestServer(t *testing.T, response string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
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
				c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(response)) + "\r\n\r\n" + response))
			}(c)
		}
	}()
	return ln.Addr().String()
}
