package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxBufferedRequestBody = 1 << 20 // 1 MiB

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
func HandleHTTP(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router) {
	defer client.Close()

	req, err := http.ReadRequest(clientReader)
	if err != nil {
		log.Printf("http read request: %v", err)
		return
	}
	defer req.Body.Close()

	if !HTTPAuthOK(req.Header, users) {
		writeResponse(client, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\n\r\n")
		return
	}

	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		log.Printf("set http deadline: %v", err)
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
		log.Printf("connect to %s failed: %v", target, err)
		writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		log.Printf("set connect client deadline: %v", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, TunnelIdleTimeout()); err != nil {
		log.Printf("set connect target deadline: %v", err)
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
		log.Printf("socks5h redirect to %s failed, fallback to routing: %v", target, err)
	}
	return router.Dial(target, mgr)
}

func handleHTTPRequest(client net.Conn, req *http.Request, mgr *UpstreamManager, router *Router) {
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")

	// Chunked 或超大请求体无法缓冲重试，直接透传。
	if isChunkedRequest(req) || req.ContentLength < 0 || req.ContentLength > maxBufferedRequestBody {
		handleStreamedRequest(client, req, mgr, router)
		return
	}
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(req.Body, maxBufferedRequestBody+1))
		if err != nil {
			log.Printf("read request body: %v", err)
			writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		if len(bodyBytes) > maxBufferedRequestBody {
			log.Printf("request body too large: %d bytes", len(bodyBytes))
			writeResponse(client, "HTTP/1.1 413 Payload Too Large\r\n\r\n")
			return
		}
	}
	resetBody := func() {
		req.TransferEncoding = nil
		req.ContentLength = int64(len(bodyBytes))
		if len(bodyBytes) == 0 {
			req.Body = http.NoBody
			req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	var resp *http.Response
	var err error
	if router.Socks5HEnabled() {
		resp, err = requestViaSocks5H(req, mgr, router, resetBody)
		if err == nil {
			defer resp.Body.Close()
			if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
				log.Printf("set response deadline: %v", err)
				return
			}
			if err := resp.Write(client); err != nil {
				log.Printf("write response to client: %v", err)
			}
			return
		}
		log.Printf("socks5h request failed, fallback to routing: %v", err)
	}
	decision := router.Decide(targetHost(req))
	switch decision.Action {
	case ActionDirect:
		resetBody()
		resp, err = directRequest(req)
	case ActionProxy:
		resp, err = requestViaProxy(req, mgr, router, decision.Upstreams, resetBody)
	default:
		resp, err = requestAuto(req, mgr, router, resetBody)
	}
	if err != nil {
		log.Printf("forward request: %v", err)
		writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		log.Printf("set response deadline: %v", err)
		return
	}
	if err := resp.Write(client); err != nil {
		log.Printf("write response to client: %v", err)
	}
}

func isChunkedRequest(req *http.Request) bool {
	for _, enc := range req.TransferEncoding {
		if strings.EqualFold(enc, "chunked") {
			return true
		}
	}
	return false
}

// 处理无法缓冲重试的流式请求（chunked 或超大请求体）。
func handleStreamedRequest(client net.Conn, req *http.Request, mgr *UpstreamManager, router *Router) {
	var resp *http.Response
	var err error
	if router.Socks5HEnabled() {
		resp, err = requestViaSocks5H(req, mgr, router, func() {})
		if err == nil {
			defer resp.Body.Close()
			if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
				log.Printf("set streamed response deadline: %v", err)
				return
			}
			if err := resp.Write(client); err != nil {
				log.Printf("write streamed response to client: %v", err)
			}
			return
		}
		log.Printf("socks5h streamed request failed, fallback to routing: %v", err)
	}

	decision := router.Decide(targetHost(req))
	switch decision.Action {
	case ActionDirect:
		resp, err = directRequest(req)
	case ActionProxy:
		resp, err = requestViaProxyNoRetry(req, mgr, router, decision.Upstreams)
	default:
		resp, err = directRequestWithTimeout(req, router.auto.DirectTimeout)
		if err != nil {
			resp, err = requestViaProxyNoRetry(req, mgr, router, nil)
		}
	}
	if err != nil {
		log.Printf("forward streamed request: %v", err)
		writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		log.Printf("set streamed response deadline: %v", err)
		return
	}
	if err := resp.Write(client); err != nil {
		log.Printf("write streamed response to client: %v", err)
	}
}

func requestViaProxyNoRetry(req *http.Request, mgr *UpstreamManager, router *Router, names []string) (*http.Response, error) {
	up := mgr.SelectFor(names)
	if up == nil {
		return nil, errors.New("no available upstream")
	}
	resp, err := forwardRequest(req, up)
	if err == nil {
		router.cache.SetProxy(routeCacheKey(targetHost(req)), up.Name, router.auto.CacheTTL)
	}
	return resp, err
}

func requestViaSocks5H(req *http.Request, mgr *UpstreamManager, router *Router, resetBody func()) (*http.Response, error) {
	resetBody()
	conn, err := router.DialSocks5H(targetHost(req), mgr)
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, HTTPExchangeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

func requestAuto(req *http.Request, mgr *UpstreamManager, router *Router, resetBody func()) (*http.Response, error) {
	target := targetHost(req)
	key := routeCacheKey(target)
	if entry, ok := router.cache.Get(key); ok {
		switch entry.Action {
		case ActionDirect:
			resetBody()
			resp, err := directRequestWithTimeout(req, router.auto.DirectTimeout)
			if err == nil {
				return resp, nil
			}
			router.cache.Delete(key)
		case ActionProxy:
			resp, err := requestViaProxy(req, mgr, router, []string{entry.UpstreamName}, resetBody)
			if err == nil {
				return resp, nil
			}
			router.cache.Delete(key)
		}
	}

	if router.auto.Mode == "proxy_first" {
		resp, err := requestViaProxy(req, mgr, router, nil, resetBody)
		if err == nil {
			return resp, nil
		}
		resetBody()
		resp, err = directRequestWithTimeout(req, router.auto.DirectTimeout)
		if err == nil {
			router.cache.SetDirect(key, router.auto.CacheTTL)
		}
		return resp, err
	}

	resetBody()
	resp, directErr := directRequestWithTimeout(req, router.auto.DirectTimeout)
	if directErr == nil {
		router.cache.SetDirect(key, router.auto.CacheTTL)
		return resp, nil
	}
	resp, proxyErr := requestViaProxy(req, mgr, router, nil, resetBody)
	if proxyErr == nil {
		return resp, nil
	}
	return nil, fmt.Errorf("direct failed: %v; proxy failed: %w", directErr, proxyErr)
}

func requestViaProxy(req *http.Request, mgr *UpstreamManager, router *Router, names []string, resetBody func()) (*http.Response, error) {
	var lastErr error
	for i := 0; i < mgr.AttemptCount(names); i++ {
		up := mgr.SelectFor(names)
		if up == nil {
			break
		}
		resetBody()
		resp, err := forwardRequest(req, up)
		if err == nil {
			router.cache.SetProxy(routeCacheKey(targetHost(req)), up.Name, router.auto.CacheTTL)
			return resp, nil
		}
		lastErr = err
		log.Printf("forward via upstream %s failed: %v", up.Name, err)
		if !isTargetError(err) {
			mgr.markDead(up)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no available upstream")
	}
	return nil, lastErr
}

func isTargetError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "target unreachable") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused")
}

func forwardRequest(req *http.Request, up *Upstream) (*http.Response, error) {
	switch up.primaryProxyType() {
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		return viaHTTPUpstream(req, up)
	case ProxyTypeSocks5, ProxyTypeSocks5H:
		return viaSOCKS5Upstream(req, up)
	}
	return nil, errors.New("unsupported upstream type")
}

func viaHTTPUpstream(req *http.Request, up *Upstream) (*http.Response, error) {
	conn, err := dialRaw(up)
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, HTTPExchangeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, true, up); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

func viaSOCKS5Upstream(req *http.Request, up *Upstream) (*http.Response, error) {
	target := targetHost(req)
	if target == "" {
		return nil, errors.New("missing target host")
	}
	conn, err := up.Dial(target)
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, HTTPExchangeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

func directRequest(req *http.Request) (*http.Response, error) {
	return directRequestWithDeadlines(req, ProxyDialTimeout(), HTTPExchangeTimeout(), HTTPExchangeTimeout())
}

func directRequestWithTimeout(req *http.Request, timeout time.Duration) (*http.Response, error) {
	return directRequestWithDeadlines(req, timeout, timeout, HTTPExchangeTimeout())
}

func directRequestWithDeadlines(req *http.Request, dialTimeout, headerTimeout, bodyTimeout time.Duration) (*http.Response, error) {
	target := targetHost(req)
	if target == "" {
		return nil, errors.New("missing target host")
	}
	conn, err := DialTargetTimeout(target, dialTimeout)
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, headerTimeout); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := setConnDeadline(conn, bodyTimeout); err != nil {
		resp.Body.Close()
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

func targetHost(req *http.Request) string {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	port := "80"
	if req.URL != nil && req.URL.Scheme == "https" {
		port = "443"
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.JoinHostPort(host, port)
}

func dialRaw(up *Upstream) (net.Conn, error) {
	return dialProxyConn(up.urlForType(up.primaryProxyType()), ProxyDialTimeout())
}

func writeHTTPRequest(w io.Writer, req *http.Request, viaUpstream bool, up *Upstream) error {
	out := req.Clone(req.Context())
	out.RequestURI = ""
	out.Header = req.Header.Clone()
	out.Body = req.Body
	out.GetBody = req.GetBody
	if viaUpstream && up != nil {
		setProxyAuthorization(out.Header, up)
		return out.WriteProxy(w)
	}
	return out.Write(w)
}

func writeResponse(w io.Writer, statusLine string) {
	if _, err := w.Write([]byte(statusLine)); err != nil {
		log.Printf("write response: %v", err)
	}
}
