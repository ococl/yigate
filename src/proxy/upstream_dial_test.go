package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// startMockHTTPProxy 启动一个最小 HTTP CONNECT/转发代理服务器，返回监听地址。
func startMockHTTPProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
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
				if req.Method == http.MethodConnect {
					target, err := net.Dial("tcp", req.Host)
					if err != nil {
						c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
						return
					}
					defer target.Close()
					c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
					go func() { _, _ = ioCopy(target, br) }()
					_, _ = ioCopy(c, target)
					return
				}
				// 普通 HTTP 转发
				target, err := net.Dial("tcp", req.Host)
				if err != nil {
					c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer target.Close()
				if err := req.Write(target); err != nil {
					return
				}
				resp, err := http.ReadResponse(bufio.NewReader(target), req)
				if err != nil {
					return
				}
				defer resp.Body.Close()
				resp.Write(c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func startMockSocks5Proxy(t *testing.T) string {
	t.Helper()
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				HandleSOCKS5(c, br, nil, nil, router, nil)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func startMockTarget(t *testing.T) string {
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
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				c.Write(buf[:n])
			}(c)
		}
	}()
	return ln.Addr().String()
}

func ioCopy(dst io.Writer, src io.Reader) (int64, error) {
	var n int64
	buf := make([]byte, 4096)
	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, _ := dst.Write(buf[:nr])
			n += int64(nw)
		}
		if err != nil {
			return n, err
		}
	}
}

func TestUpstreamDialHTTP(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via http proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamDialSOCKS5(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "socks5", URL: "socks5://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}

	target := startMockTarget(t)
	conn, err := up.Dial(target)
	if err != nil {
		t.Fatalf("dial via socks5 proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestUpstreamManagerDialAndFallback(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, err := mgr.Dial(target)
	if err != nil {
		t.Fatalf("dial fallback: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}
