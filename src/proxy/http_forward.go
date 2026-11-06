package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

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

func directRequest(req *http.Request, router *Router) (*http.Response, error) {
	return directRequestWithDeadlines(req, ProxyDialTimeout(), HTTPExchangeTimeout(), HTTPExchangeTimeout(), router)
}

func directRequestWithTimeout(req *http.Request, timeout time.Duration, router *Router) (*http.Response, error) {
	return directRequestWithDeadlines(req, timeout, timeout, HTTPExchangeTimeout(), router)
}

func directRequestWithDeadlines(req *http.Request, dialTimeout, headerTimeout, bodyTimeout time.Duration, router *Router) (*http.Response, error) {
	target := targetHost(req)
	if target == "" {
		return nil, errors.New("missing target host")
	}
	// 直连转发必须经过 DestinationPolicy 校验（含 DNS rebinding 防护），
	// 与 CONNECT 路径使用同一拨号实现，避免普通 HTTP 请求绕过内网/SSRF 防护。
	var conn net.Conn
	var err error
	if router != nil {
		conn, err = router.dialDirectCheckedTimeout(target, dialTimeout)
	} else {
		conn, err = DialTargetTimeout(target, dialTimeout)
	}
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
	rawConn, err := dialRaw(up)
	if err != nil {
		return nil, err
	}
	conn := tagUpstream(rawConn, up.Name)
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
	rawConn, err := up.Dial(target)
	if err != nil {
		return nil, err
	}
	conn := tagUpstream(rawConn, up.Name)
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
