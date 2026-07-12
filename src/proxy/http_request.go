package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

const maxBufferedRequestBody = 1 << 20 // 1 MiB

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
			Logger().Warnw("read request body failed", "error", err)
			writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		if len(bodyBytes) > maxBufferedRequestBody {
			Logger().Warnw("request body too large", "size", len(bodyBytes), "max", maxBufferedRequestBody)
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
				Logger().Warnw("set response deadline failed", "error", err)
				return
			}
			if err := resp.Write(client); err != nil {
				Logger().Warnw("write response to client failed", "error", err)
			}
			return
		}
		Logger().Warnw("socks5h request failed, fallback to routing", "error", err)
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
		Logger().Warnw("forward request failed", "target", targetHost(req), "error", err)
		status := HTTPStatusFromError(err)
		writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
		return
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set response deadline failed", "error", err)
		return
	}
	if err := resp.Write(client); err != nil {
		Logger().Warnw("write response to client failed", "error", err)
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
				Logger().Warnw("set streamed response deadline failed", "error", err)
				return
			}
			if err := resp.Write(client); err != nil {
				Logger().Warnw("write streamed response to client failed", "error", err)
			}
			return
		}
		Logger().Warnw("socks5h streamed request failed, fallback to routing", "error", err)
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
		Logger().Warnw("forward streamed request failed", "target", targetHost(req), "error", err)
		status := HTTPStatusFromError(err)
		writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
		return
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set streamed response deadline failed", "error", err)
		return
	}
	if err := resp.Write(client); err != nil {
		Logger().Warnw("write streamed response to client failed", "error", err)
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
	tried := make(map[*Upstream]struct{})
	for i := 0; i < mgr.AttemptCount(names); i++ {
		up := mgr.SelectForExcluding(names, tried)
		if up == nil {
			break
		}
		tried[up] = struct{}{}
		resetBody()
		resp, err := forwardRequest(req, up)
		if err == nil {
			router.cache.SetProxy(routeCacheKey(targetHost(req)), up.Name, router.auto.CacheTTL)
			return resp, nil
		}
		lastErr = err
		Logger().Warnw("forward via upstream failed", "upstream", up.Name, "target", targetHost(req), "error", err)
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
