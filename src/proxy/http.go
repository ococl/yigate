package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

// responseBody 包装上游/目标返回的响应体，关闭时同时关闭底层连接。
type responseBody struct {
	io.ReadCloser
	conn  net.Conn
	bytes int64
}

// Read 统计响应体字节数，用于流量审计。
func (b *responseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes += int64(n)
	return n, err
}

func (b *responseBody) Close() error {
	err := b.ReadCloser.Close()
	if closeErr := b.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

// maxHTTPHeaderBytes 限制单个 HTTP 请求头部的最大字节数，防止通过超大请求头耗尽内存（DoS）。
const maxHTTPHeaderBytes = 1 << 20 // 1 MiB

// errHTTPHeaderTooLarge 表示请求头部超过允许的最大字节数。
var errHTTPHeaderTooLarge = errors.New("http request header too large")

// maxHeaderConnReader 在读请求头部阶段限制累计读取字节数，头部读完（done=true）后解除限制，
// 不影响请求体读取与同一连接上的后续请求（HTTP/1.1 keep-alive）。
type maxHeaderConnReader struct {
	br    *bufio.Reader // 可能包含协议嗅探预读的字节
	conn  net.Conn
	limit int64
	used  int64
	done  bool
}

func (h *maxHeaderConnReader) Read(p []byte) (int, error) {
	if h.done {
		return h.readUnlimited(p)
	}
	if h.used >= h.limit {
		return 0, errHTTPHeaderTooLarge
	}
	// 先消费嗅探 reader 中已缓冲的字节
	if h.br != nil && h.br.Buffered() > 0 {
		n, err := h.br.Read(p)
		if n > 0 {
			h.used += int64(n)
		}
		return n, err
	}
	limit := h.limit - h.used
	if int64(len(p)) > limit {
		p = p[:limit]
	}
	n, err := h.conn.Read(p)
	if n > 0 {
		h.used += int64(n)
	}
	return n, err
}

func (h *maxHeaderConnReader) readUnlimited(p []byte) (int, error) {
	if h.br != nil && h.br.Buffered() > 0 {
		return h.br.Read(p)
	}
	return h.conn.Read(p)
}

// HandleHTTP 处理 HTTP/HTTPS 代理请求。
// 同一客户端 TCP 连接支持 HTTP/1.1 keep-alive，可串行处理多个请求；
// 请求头受 maxHTTPHeaderBytes 上限保护。
func HandleHTTP(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router, access *AccessController) {
	defer client.Close()
	MetricInc("yigate_requests_total", "protocol=http")

	hr := &maxHeaderConnReader{br: clientReader, conn: client, limit: maxHTTPHeaderBytes}
	reqReader := bufio.NewReader(hr)

	for {
		// 每轮请求重新开启头部大小限制（上一轮 body/响应已不受限）。
		hr.done = false
		hr.used = 0

		req, err := http.ReadRequest(reqReader)
		if err != nil {
			if err != io.EOF {
				Logger().Warnw("http read request failed", "error", err)
			}
			return
		}
		// 头部读取完成，解除头部大小限制，避免影响 body 与后续 keep-alive 请求。
		hr.done = true

		if !serveOneHTTPRequest(client, req, reqReader, users, mgr, router, access) {
			return
		}
	}
}

// serveOneHTTPRequest 处理单个 HTTP 请求。
// 返回 true 表示客户端连接可继续用于下一个请求（keep-alive），false 表示应关闭连接。
func serveOneHTTPRequest(client net.Conn, req *http.Request, reqReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router, access *AccessController) bool {
	defer req.Body.Close()

	user, ok := HTTPAuthOK(req.Header, users)
	if !ok {
		writeResponse(client, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\n\r\n")
		return false
	}
	if access != nil && user != "" {
		if !access.AcquireUserSlot(user) {
			Logger().Warnw("http user connection limit exceeded", "user", user)
			writeResponse(client, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
			return false
		}
		defer access.ReleaseUserSlot(user)
	}

	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set http deadline failed", "error", err)
		return false
	}

	if req.Method == "CONNECT" {
		// 必须沿用读取请求所用的 reqReader（而非原始 clientReader），
		// 否则本层 bufio 预读的 CONNECT 之后的字节会丢失，导致 HTTPS 隧道损坏。
		handleConnect(client, reqReader, req.Host, users, mgr, router, user)
		return false
	}

	return handleHTTPRequest(client, req, mgr, router, clientSourceIP(client), user)
}

func handleConnect(client net.Conn, clientReader *bufio.Reader, target string, users map[string]string, mgr *UpstreamManager, router *Router, user string) {
	sourceIP := clientSourceIP(client)
	targetConn, err := dialTargetForRequest(target, mgr, router, sourceIP)
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
	Tunnel(client, clientReader, targetConn, newTrafficKey(user, targetConn, target), getGlobalTrafficAccounter())
}

func dialTargetForRequest(target string, mgr *UpstreamManager, router *Router, sourceIP ...string) (net.Conn, error) {
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	if router != nil && router.Socks5HEnabled() {
		conn, err := router.DialSocks5H(target, mgr, sip)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, ErrSocks5HNoUpstream) {
			// 没有可用 SOCKS5 上游时按未开启处理，静默回退，不产生告警噪音。
			Logger().Warnw("socks5h redirect failed, fallback to routing", "target", target, "error", err)
		}
	}
	return router.Dial(target, mgr, sip)
}

func writeResponse(w io.Writer, statusLine string) {
	if _, err := w.Write([]byte(statusLine)); err != nil {
		Logger().Warnw("write response failed", "error", err)
	}
}
