package proxy

import (
	"io"
	"net"
	"time"
)

const (
	proxyDialTimeout      = 10 * time.Second
	proxyHandshakeTimeout = 10 * time.Second
	httpExchangeTimeout   = 60 * time.Second
	tunnelIdleTimeout     = 5 * time.Minute
)

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
