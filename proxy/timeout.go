package proxy

import (
	"io"
	"net"
	"time"
)

const (
	defaultProxyDialTimeout      = 10 * time.Second
	defaultProxyHandshakeTimeout = 10 * time.Second
	defaultHTTPExchangeTimeout   = 60 * time.Second
	defaultTunnelIdleTimeout     = 5 * time.Minute
)

var (
	proxyDialTimeout      = defaultProxyDialTimeout
	proxyHandshakeTimeout = defaultProxyHandshakeTimeout
	httpExchangeTimeout   = defaultHTTPExchangeTimeout
	tunnelIdleTimeout     = defaultTunnelIdleTimeout
)

// TimeoutConfig 可配置的超时参数
type TimeoutConfig struct {
	Dial         Duration `yaml:"dial"`
	Handshake    Duration `yaml:"handshake"`
	HTTPExchange Duration `yaml:"http_exchange"`
	TunnelIdle   Duration `yaml:"tunnel_idle"`
}

// SetTimeouts 根据配置更新超时参数，未设置项保持默认值
func SetTimeouts(cfg TimeoutConfig) {
	if cfg.Dial > 0 {
		proxyDialTimeout = time.Duration(cfg.Dial)
	}
	if cfg.Handshake > 0 {
		proxyHandshakeTimeout = time.Duration(cfg.Handshake)
	}
	if cfg.HTTPExchange > 0 {
		httpExchangeTimeout = time.Duration(cfg.HTTPExchange)
	}
	if cfg.TunnelIdle > 0 {
		tunnelIdleTimeout = time.Duration(cfg.TunnelIdle)
	}
}

func resetTimeouts() {
	proxyDialTimeout = defaultProxyDialTimeout
	proxyHandshakeTimeout = defaultProxyHandshakeTimeout
	httpExchangeTimeout = defaultHTTPExchangeTimeout
	tunnelIdleTimeout = defaultTunnelIdleTimeout
}

// ProxyDialTimeout 返回当前拨号超时
func ProxyDialTimeout() time.Duration { return proxyDialTimeout }

// ProxyHandshakeTimeout 返回当前握手超时
func ProxyHandshakeTimeout() time.Duration { return proxyHandshakeTimeout }

// HTTPExchangeTimeout 返回当前 HTTP 交换超时
func HTTPExchangeTimeout() time.Duration { return httpExchangeTimeout }

// TunnelIdleTimeout 返回当前隧道空闲超时
func TunnelIdleTimeout() time.Duration { return tunnelIdleTimeout }

type deadlineReader struct {
	r       io.Reader
	conn    net.Conn
	timeout time.Duration
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	if r.timeout > 0 {
		_ = r.conn.SetReadDeadline(time.Now().Add(r.timeout))
	}
	return r.r.Read(p)
}

type deadlineWriter struct {
	w       io.Writer
	conn    net.Conn
	timeout time.Duration
}

func (w *deadlineWriter) Write(p []byte) (int, error) {
	if w.timeout > 0 {
		_ = w.conn.SetWriteDeadline(time.Now().Add(w.timeout))
	}
	return w.w.Write(p)
}

func setConnDeadline(conn net.Conn, timeout time.Duration) error {
	if timeout <= 0 {
		return conn.SetDeadline(time.Time{})
	}
	return conn.SetDeadline(time.Now().Add(timeout))
}
