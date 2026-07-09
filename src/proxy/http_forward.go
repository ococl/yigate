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
