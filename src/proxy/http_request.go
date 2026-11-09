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

// handleHTTPRequest 处理单个非 CONNECT 的 HTTP 请求，将响应写回客户端。
// 返回 true 表示可保持连接（keep-alive），false 表示应关闭连接。
func handleHTTPRequest(client net.Conn, req *http.Request, mgr *UpstreamManager, router *Router, sourceIP string, user string) bool {
	if err := rejectRequestTarget(router, req); err != nil {
		writePolicyDeniedResponse(client, targetHost(req), err)
		return false
	}
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")

	// Chunked 或超大请求体无法缓冲重试，直接透传。
	if isChunkedRequest(req) || req.ContentLength < 0 || req.ContentLength > maxBufferedRequestBody {
		return handleStreamedRequest(client, req, mgr, router, sourceIP, user)
	}
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(req.Body, maxBufferedRequestBody+1))
		if err != nil {
			Logger().Warnw("read request body failed", "error", err)
			writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return false
		}
		if len(bodyBytes) > maxBufferedRequestBody {
			Logger().Warnw("request body too large", "size", len(bodyBytes), "max", maxBufferedRequestBody)
			writeResponse(client, "HTTP/1.1 413 Payload Too Large\r\n\r\n")
			return false
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
				return false
			}
			if err := resp.Write(client); err != nil {
				Logger().Warnw("write response to client failed", "error", err)
				return false
			}
			recordHTTPTraffic(user, req, resp, int64(len(bodyBytes)))
			return !req.Close
		}
		Logger().Warnw("socks5h request failed, fallback to routing", "error", err)
	}
	decision := router.Decide(targetHost(req))
	switch decision.Action {
	case ActionDirect:
		resetBody()
		resp, err = directRequest(req, router)
	case ActionProxy:
		resp, err = requestViaProxy(req, mgr, router, decision.Upstreams, resetBody, sourceIP)
	default:
		resp, err = requestAuto(req, mgr, router, decision, resetBody, sourceIP)
	}
	if err != nil {
		Logger().Warnw("forward request failed", "target", targetHost(req), "error", err)
		status := HTTPStatusFromError(err)
		writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
		return false
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set response deadline failed", "error", err)
		return false
	}
	if err := resp.Write(client); err != nil {
		Logger().Warnw("write response to client failed", "error", err)
		return false
	}
	recordHTTPTraffic(user, req, resp, int64(len(bodyBytes)))
	return !req.Close
}

// recordHTTPTraffic 记录普通 HTTP 请求的流量。
// 上游名称来自上游连接上的标记，直连请求为空。
func recordHTTPTraffic(user string, req *http.Request, resp *http.Response, reqBytes int64) {
	accounter := getGlobalTrafficAccounter()
	if accounter == nil {
		return
	}
	var bytesOut int64
	upstream := ""
	if rb, ok := resp.Body.(*responseBody); ok {
		bytesOut = rb.bytes
		upstream = upstreamNameOf(rb.conn)
	}
	if reqBytes < 0 {
		reqBytes = 0
	}
	key := TrafficKey{User: user, Upstream: upstream, Target: targetHost(req)}
	if err := accounter.Record(key, reqBytes, bytesOut); err != nil {
		Logger().Warnw("traffic quota exceeded", "user", user, "upstream", upstream, "target", key.Target, "error", err)
	}
}

// rejectRequestTarget 在协议分支之前统一执行目标访问策略。
// 路由决策只决定走直连还是上游，不能替代访问授权；若只检查 Dial/CONNECT 路径，
// 普通 HTTP 请求经上游转发时会绕过 deny_* 与 default_action=deny。
func rejectRequestTarget(router *Router, req *http.Request) error {
	if router == nil || req == nil {
		return nil
	}
	target := targetHost(req)
	if target == "" {
		return nil
	}
	return router.CheckTarget(target)
}

func writePolicyDeniedResponse(client net.Conn, target string, err error) {
	Logger().Warnw("request target denied by policy", "target", target, "error", err)
	status := HTTPStatusFromError(err)
	writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
}

func isChunkedRequest(req *http.Request) bool {
	for _, enc := range req.TransferEncoding {
		if strings.EqualFold(enc, "chunked") {
			return true
		}
	}
	return false
}

// handleStreamedRequest 处理无法缓冲重试的流式请求（chunked 或超大请求体）。
// 返回 true 表示可保持连接（keep-alive），false 表示应关闭连接。
func handleStreamedRequest(client net.Conn, req *http.Request, mgr *UpstreamManager, router *Router, sourceIP string, user string) bool {
	if err := rejectRequestTarget(router, req); err != nil {
		writePolicyDeniedResponse(client, targetHost(req), err)
		return false
	}
	var resp *http.Response
	var err error
	if router.Socks5HEnabled() {
		resp, err = requestViaSocks5H(req, mgr, router, func() {})
		if err == nil {
			defer resp.Body.Close()
			if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
				Logger().Warnw("set streamed response deadline failed", "error", err)
				return false
			}
			if err := resp.Write(client); err != nil {
				Logger().Warnw("write streamed response to client failed", "error", err)
				return false
			}
			recordHTTPTraffic(user, req, resp, req.ContentLength)
			return !req.Close
		}
		Logger().Warnw("socks5h streamed request failed, fallback to routing", "error", err)
	}

	decision := router.Decide(targetHost(req))
	switch decision.Action {
	case ActionDirect:
		resp, err = directRequest(req, router)
	case ActionProxy:
		resp, err = requestViaProxyNoRetry(req, mgr, router, decision.Upstreams, sourceIP)
	default:
		if router.autoMode(decision) == "proxy_first" {
			resp, err = requestViaProxyNoRetry(req, mgr, router, decision.Upstreams, sourceIP)
			if err != nil {
				resp, err = directRequest(req, router)
			}
			break
		}
		resp, err = directRequestWithTimeout(req, router.auto.DirectTimeout, router)
		if err != nil {
			resp, err = requestViaProxyNoRetry(req, mgr, router, decision.Upstreams, sourceIP)
		}
	}
	if err != nil {
		Logger().Warnw("forward streamed request failed", "target", targetHost(req), "error", err)
		status := HTTPStatusFromError(err)
		writeResponse(client, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, HTTPStatusText(status)))
		return false
	}
	defer resp.Body.Close()
	if err := setConnDeadline(client, HTTPExchangeTimeout()); err != nil {
		Logger().Warnw("set streamed response deadline failed", "error", err)
		return false
	}
	if err := resp.Write(client); err != nil {
		Logger().Warnw("write streamed response to client failed", "error", err)
		return false
	}
	recordHTTPTraffic(user, req, resp, req.ContentLength)
	return !req.Close
}

func requestViaProxyNoRetry(req *http.Request, mgr *UpstreamManager, router *Router, names []string, sourceIP string) (*http.Response, error) {
	up := mgr.SelectForWithSourceIP(names, sourceIP)
	if up == nil {
		return nil, errors.New("no available upstream")
	}
	resp, err := forwardRequest(req, up)
	if err == nil {
		router.cache.SetProxy(routeCacheKey(targetHost(req)), up.Name, router.auto.CacheTTL)
	}
	return resp, err
}

func requestAuto(req *http.Request, mgr *UpstreamManager, router *Router, decision RouteDecision, resetBody func(), sourceIP string) (*http.Response, error) {
	target := targetHost(req)
	key := routeCacheKey(target)
	if entry, ok := router.cache.Get(key); ok {
		switch entry.Action {
		case ActionDirect:
			resetBody()
			resp, err := directRequestWithTimeout(req, router.auto.DirectTimeout, router)
			if err == nil {
				return resp, nil
			}
			router.cache.Delete(key)
		case ActionProxy:
			resp, err := requestViaProxy(req, mgr, router, []string{entry.UpstreamName}, resetBody, sourceIP)
			if err == nil {
				return resp, nil
			}
			router.cache.Delete(key)
		}
	}

	if router.autoMode(decision) == "proxy_first" {
		resp, err := requestViaProxy(req, mgr, router, decision.Upstreams, resetBody, sourceIP)
		if err == nil {
			return resp, nil
		}
		resetBody()
		resp, err = directRequestWithTimeout(req, router.auto.DirectTimeout, router)
		if err == nil {
			router.cache.SetDirect(key, router.auto.CacheTTL)
		}
		return resp, err
	}

	resetBody()
	resp, directErr := directRequestWithTimeout(req, router.auto.DirectTimeout, router)
	if directErr == nil {
		router.cache.SetDirect(key, router.auto.CacheTTL)
		return resp, nil
	}
	resp, proxyErr := requestViaProxy(req, mgr, router, decision.Upstreams, resetBody, sourceIP)
	if proxyErr == nil {
		return resp, nil
	}
	return nil, fmt.Errorf("direct failed: %v; proxy failed: %w", directErr, proxyErr)
}

func requestViaProxy(req *http.Request, mgr *UpstreamManager, router *Router, names []string, resetBody func(), sourceIP string) (*http.Response, error) {
	var lastErr error
	tried := make(map[*Upstream]struct{})
	for i := 0; i < mgr.AttemptCount(names); i++ {
		up := mgr.SelectForExcludingWithSourceIP(names, tried, sourceIP)
		if up == nil {
			break
		}
		tried[up] = struct{}{}
		resetBody()
		resp, err := forwardRequest(req, up)
		if err == nil {
			mgr.markAlive(up)
			router.cache.SetProxy(routeCacheKey(targetHost(req)), up.Name, router.auto.CacheTTL)
			return resp, nil
		}
		lastErr = err
		Logger().Warnw("forward via upstream failed", "upstream", up.Name, "target", targetHost(req), "error", err)
		if IsUpstreamError(err) {
			mgr.markDead(up)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no available upstream")
	}
	return nil, lastErr
}
