package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

type responseBody struct {
	io.ReadCloser
	conn net.Conn
}

func (b *responseBody) Close() error {
	err := b.ReadCloser.Close()
	if closeErr := b.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

// HandleHTTP 处理 HTTP/HTTPS 代理请求
// 该方法会复用 clientReader 中已经预读的字节
func HandleHTTP(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router, access *AccessController) {
	defer client.Close()
	MetricInc("k5pw_requests_total", "protocol=http")

	req, err := http.ReadRequest(clientReader)
	if err != nil {
		Logger().Warnw("http read request failed", "error", err)
		return
	}
	defer req.Body.Close()

	user, ok := HTTPAuthOK(req.Header, users)
	if !ok {
		writeResponse(client, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\n\r\n")
		return
	}
	if access != nil && user != "" {
		if !access.AcquireUserSlot(user) {
			Logger().Warnw("http user connection limit exceeded", "user", user)
			writeResponse(client, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
			return
		}
		defer access.ReleaseUserSlot(user)
	}

	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set http deadline failed", "error", err)
		return
	}

	if req.Method == "CONNECT" {
		handleConnect(client, clientReader, req.Host, users, mgr, router)
		return
	}

	handleHTTPRequest(client, req, mgr, router)
}

func handleConnect(client net.Conn, clientReader *bufio.Reader, target string, users map[string]string, mgr *UpstreamManager, router *Router) {
	targetConn, err := dialTargetForRequest(target, mgr, router)
	if err != nil {
		Logger().Warnw("connect failed", "target", target, "error", err)
		status := HTTPStatusFromError(err)
		writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
		return
	}
	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set connect client deadline failed", "error", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set connect target deadline failed", "error", err)
		targetConn.Close()
		return
	}
	writeResponse(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	Tunnel(client, clientReader, targetConn)
}

func dialTargetForRequest(target string, mgr *UpstreamManager, router *Router) (net.Conn, error) {
	if router != nil && router.Socks5HEnabled() {
		conn, err := router.DialSocks5H(target, mgr)
		if err == nil {
			return conn, nil
		}
		Logger().Warnw("socks5h redirect failed, fallback to routing", "target", target, "error", err)
	}
	return router.Dial(target, mgr)
}

func writeResponse(w io.Writer, statusLine string) {
	if _, err := w.Write([]byte(statusLine)); err != nil {
		Logger().Warnw("write response failed", "error", err)
	}
}
