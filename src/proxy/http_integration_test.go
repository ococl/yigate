package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func startMockHTTPOrigin(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
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
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				body, _ := io.ReadAll(req.Body)
				resp := "HTTP/1.1 200 OK\r\nContent-Length: " + itoa(len(body)) + "\r\n\r\n" + string(body)
				c.Write([]byte(resp))
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestHandleHTTPChunkedRequest(t *testing.T) {
	origin := startMockHTTPOrigin(t)
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})

	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), nil, mgr, router, nil)
	}()

	req := "POST http://" + origin + "/ HTTP/1.1\r\n" +
		"Host: " + origin + "\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"\r\n" +
		"5\r\nhello\r\n" +
		"0\r\n\r\n"
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestHandleHTTPAuthFailed(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})
	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), map[string]string{"user": "pass"}, mgr, router, nil)
	}()

	req := "GET http://example.com/ HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Proxy-Authorization: Basic dXNlcjp3cm9uZw==\r\n" + // user:wrong
		"\r\n"
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	line, _ := br.ReadString('\n')
	if !strings.Contains(line, "407") {
		t.Fatalf("expected 407, got %q", line)
	}
}

func TestHandleHTTPConnect(t *testing.T) {
	target := startMockTarget(t)
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})
	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), nil, mgr, router, nil)
	}()

	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	line, _ := br.ReadString('\n')
	if !strings.Contains(line, "200") {
		t.Fatalf("expected 200, got %q", line)
	}
	c1.Write([]byte("hello"))
	buf := make([]byte, 64)
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := c1.Read(buf)
	if string(buf[:n]) != "hello" {
		t.Fatalf("unexpected echo %q", buf[:n])
	}
}

func TestHandleHTTPSocks5HRequest(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "socks5", URL: "socks5://" + proxyAddr}})
	router, _ := NewRouter(RoutingConfig{
		Final:           ActionDirect,
		Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"socks5"}},
		Auto:            AutoConfig{CacheTTL: Duration(time.Hour)},
	}, DNSConfig{})

	origin := startMockHTTPOrigin(t)
	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), nil, mgr, router, nil)
	}()

	req := "GET http://" + origin + "/ HTTP/1.1\r\nHost: " + origin + "\r\n\r\n"
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHandleHTTPLargeRequestBodyStreamed(t *testing.T) {
	origin := startMockHTTPOrigin(t)
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})

	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), nil, mgr, router, nil)
	}()

	body := strings.Repeat("x", maxBufferedRequestBody+1024)
	req := "POST http://" + origin + "/ HTTP/1.1\r\n" +
		"Host: " + origin + "\r\n" +
		"Content-Length: " + itoa(len(body)) + "\r\n" +
		"\r\n" + body
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(got) != len(body) || string(got[:16]) != body[:16] {
		t.Fatalf("large streamed body len = %d, want %d", len(got), len(body))
	}
}

func TestHandleHTTPSocks5HFailureFallsBackToRouting(t *testing.T) {
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "bad-socks", URL: "socks5://127.0.0.1:1"}})
	router, _ := NewRouter(RoutingConfig{
		Final:           ActionDirect,
		Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"bad-socks"}},
	}, DNSConfig{})

	origin := startMockHTTPOrigin(t)
	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		HandleHTTP(c2, bufio.NewReader(c2), nil, mgr, router, nil)
	}()

	req := "POST http://" + origin + "/ HTTP/1.1\r\n" +
		"Host: " + origin + "\r\n" +
		"Content-Length: 8\r\n" +
		"\r\n" +
		"fallback"
	c1.Write([]byte(req))
	br := bufio.NewReader(c1)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "fallback" {
		t.Fatalf("status/body = %d/%q", resp.StatusCode, body)
	}
}
