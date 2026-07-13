package proxy

import (
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

const (
	defaultProxyDialTimeout      = 10 * time.Second
	defaultProxyHandshakeTimeout = 10 * time.Second
	defaultHTTPExchangeTimeout   = 60 * time.Second
	defaultTunnelIdleTimeout     = 5 * time.Minute
)

var timeouts atomic.Value

func init() {
	timeouts.Store(timeoutConfig{
		dial:      defaultProxyDialTimeout,
		handshake: defaultProxyHandshakeTimeout,
		http:      defaultHTTPExchangeTimeout,
		tunnel:    defaultTunnelIdleTimeout,
	})
}

type timeoutConfig struct {
	dial      time.Duration
	handshake time.Duration
	http      time.Duration
	tunnel    time.Duration
}

// TimeoutConfig 可配置的超时参数
type TimeoutConfig struct {
	Dial         Duration `yaml:"dial"`
	Handshake    Duration `yaml:"handshake"`
	HTTPExchange Duration `yaml:"http_exchange"`
	TunnelIdle   Duration `yaml:"tunnel_idle"`
}

func ValidateTimeoutConfig(cfg TimeoutConfig) error {
	values := []struct {
		name  string
		value Duration
	}{
		{"dial", cfg.Dial},
		{"handshake", cfg.Handshake},
		{"http_exchange", cfg.HTTPExchange},
		{"tunnel_idle", cfg.TunnelIdle},
	}
	for _, item := range values {
		if item.value < 0 {
			return fmt.Errorf("%s must be >= 0", item.name)
		}
	}
	return nil
}

// SetTimeouts 根据配置更新超时参数，未设置项保持当前值
func SetTimeouts(cfg TimeoutConfig) {
	cur := timeouts.Load().(timeoutConfig)
	if cfg.Dial > 0 {
		cur.dial = time.Duration(cfg.Dial)
	}
	if cfg.Handshake > 0 {
		cur.handshake = time.Duration(cfg.Handshake)
	}
	if cfg.HTTPExchange > 0 {
		cur.http = time.Duration(cfg.HTTPExchange)
	}
	if cfg.TunnelIdle > 0 {
		cur.tunnel = time.Duration(cfg.TunnelIdle)
	}
	timeouts.Store(cur)
}

func resetTimeouts() {
	timeouts.Store(timeoutConfig{
		dial:      defaultProxyDialTimeout,
		handshake: defaultProxyHandshakeTimeout,
		http:      defaultHTTPExchangeTimeout,
		tunnel:    defaultTunnelIdleTimeout,
	})
}

// ProxyDialTimeout 返回当前拨号超时
func ProxyDialTimeout() time.Duration { return timeouts.Load().(timeoutConfig).dial }

// ProxyHandshakeTimeout 返回当前握手超时
func ProxyHandshakeTimeout() time.Duration { return timeouts.Load().(timeoutConfig).handshake }

// HTTPExchangeTimeout 返回当前 HTTP 交换超时
func HTTPExchangeTimeout() time.Duration { return timeouts.Load().(timeoutConfig).http }

// TunnelIdleTimeout 返回当前隧道空闲超时
func TunnelIdleTimeout() time.Duration { return timeouts.Load().(timeoutConfig).tunnel }

type deadlineReader struct {
	r               io.Reader
	conn            net.Conn
	timeout         time.Duration
	metricDirection string
	trafficKey      TrafficKey
	accounter       *TrafficAccounter
	auditDirection  string // "in" = client_to_target, "out" = target_to_client
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	if r.timeout > 0 {
		_ = r.conn.SetReadDeadline(time.Now().Add(r.timeout))
	}
	n, err := r.r.Read(p)
	if n > 0 && r.metricDirection != "" {
		MetricAdd("k5pw_tunnel_transferred_bytes_total", float64(n), "direction="+r.metricDirection)
		MetricInc("k5pw_tunnel_packets_total", "direction="+r.metricDirection)
	}
	if n > 0 && r.accounter != nil {
		if r.auditDirection == "in" {
			_ = r.accounter.Record(r.trafficKey, int64(n), 0)
		} else {
			_ = r.accounter.Record(r.trafficKey, 0, int64(n))
		}
	}
	return n, err
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
