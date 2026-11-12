package proxy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// dialHTTP 通过 HTTP/HTTPS 代理的 CONNECT 建立隧道
func (u *Upstream) dialHTTP(target string) (net.Conn, error) {
	conn, err := dialProxyConnWith(u, u.urlForType(u.primaryProxyType()), ProxyDialTimeout())
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, ProxyHandshakeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}

	creds := proxyCredentials(u)

	var authLine string
	if creds != "" {
		authLine = "Proxy-Authorization: Basic " + creds + "\r\n"
	}

	connectReq := &http.Request{Method: http.MethodConnect}
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, authLine)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		conn.Close()
		return nil, classifyHTTPConnectStatus(resp.StatusCode, resp.Status)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return &bufferedConn{Conn: conn, r: br}, nil
}

func setProxyAuthorization(header http.Header, up *Upstream) {
	creds := proxyCredentials(up)
	if creds == "" {
		return
	}
	header.Set("Proxy-Authorization", "Basic "+creds)
}

func proxyCredentials(up *Upstream) string {
	if up == nil || up.URL == nil || up.URL.User == nil {
		return ""
	}
	username := up.URL.User.Username()
	password, _ := up.URL.User.Password()
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

// clientTLSOptions 返回该上游的出站 TLS 材料。
func (u *Upstream) clientTLSOptions() ClientTLSOptions {
	if u == nil {
		return ClientTLSOptions{Protocol: TLSProtocolTLS}
	}
	return ClientTLSOptions{
		Protocol:           u.TLSProtocol,
		CAFile:             u.TLSRootCAFile,
		ServerName:         u.TLSServerName,
		InsecureSkipVerify: u.TLSInsecureSkipVerify,
	}
}

// dialProxyConnWith 按上游配置的传输层协议拨号：https 上游走 TLS/TLCP 协商，
// 其余 scheme 仍是明文 TCP。
func dialProxyConnWith(up *Upstream, u *url.URL, timeout time.Duration) (net.Conn, error) {
	addr := proxyAddress(u)
	dialer := &net.Dialer{Timeout: timeout}
	if u.Scheme != "https" {
		return dialer.Dial("tcp", addr)
	}
	if up == nil {
		return tls.DialWithDialer(dialer, "tcp", addr, tlsConfigForHost(addr))
	}
	return up.tlsNeg.dial(addr, up.clientTLSOptions(), dialer)
}

var tlsConfigForHost = defaultTLSConfigForHost

func defaultTLSConfigForHost(addr string) *tls.Config {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	// TLS SNI 不支持 IP 地址，对 IP 目标不发送 ServerName
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if net.ParseIP(host) == nil {
		cfg.ServerName = host
	}
	return cfg
}

func proxyAddress(u *url.URL) string {
	if _, _, err := net.SplitHostPort(u.Host); err == nil {
		return u.Host
	}
	port := "80"
	switch u.Scheme {
	case "https":
		port = "443"
	case "socks5", "socks5h":
		port = "1080"
	}
	return net.JoinHostPort(u.Hostname(), port)
}
