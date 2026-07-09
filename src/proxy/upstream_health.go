package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// StartHealthCheck 启动定时健康检查
func (m *UpstreamManager) StartHealthCheck(interval int) {
	if interval <= 0 {
		return
	}
	m.StopHealthCheck()

	m.mu.Lock()
	m.checkInterval = interval
	m.done = make(chan struct{})
	done := m.done
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				m.checkAll()
			}
		}
	}()
}

// StopHealthCheck 停止健康检查
func (m *UpstreamManager) StopHealthCheck() {
	m.mu.Lock()
	done := m.done
	if done == nil {
		m.mu.Unlock()
		return
	}
	m.done = nil
	close(done)
	m.mu.Unlock()

	m.wg.Wait()
}

func (m *UpstreamManager) checkAll() {
	var wg sync.WaitGroup
	for _, u := range m.upstreams {
		wg.Add(1)
		go func(u *Upstream) {
			defer wg.Done()
			alive := m.check(u)
			m.mu.Lock()
			if u.dead && alive {
				Logger().Infow("upstream recovered", "upstream", u.URL.Host, "name", u.Name)
			} else if !u.dead && !alive {
				Logger().Warnw("upstream dead", "upstream", u.URL.Host, "name", u.Name)
			}
			u.dead = !alive
			status := 0.0
			if alive {
				status = 1.0
			}
			MetricSet("k5pw_upstream_health_status", status, "upstream="+u.Name)
			m.mu.Unlock()

		}(u)
	}
	wg.Wait()
}

func (m *UpstreamManager) check(u *Upstream) bool {
	conn, err := dialProxyConn(u.URL, 5*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	switch u.primaryProxyType() {
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		return checkHTTPProtocol(conn, u)
	case ProxyTypeSocks5, ProxyTypeSocks5H:
		return checkSOCKS5Protocol(conn, u)
	default:
		return true
	}
}

func checkHTTPProtocol(conn net.Conn, up *Upstream) bool {
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return false
	}
	defer conn.SetDeadline(time.Time{})

	creds := proxyCredentials(up)
	auth := ""
	if creds != "" {
		auth = "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	req := fmt.Sprintf("OPTIONS * HTTP/1.1\r\nHost: %s\r\n%s\r\n", up.URL.Host, auth)
	if _, err := conn.Write([]byte(req)); err != nil {
		return false
	}

	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(line, "HTTP/1.")
}

func checkSOCKS5Protocol(conn net.Conn, up *Upstream) bool {
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return false
	}
	defer conn.SetDeadline(time.Time{})

	user, pass := "", ""
	if up.URL != nil && up.URL.User != nil {
		user = up.URL.User.Username()
		pass, _ = up.URL.User.Password()
	}

	methods := []byte{0x00}
	if user != "" || pass != "" {
		methods = append(methods, 0x02)
	}
	greeting := []byte{0x05, byte(len(methods))}
	greeting = append(greeting, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return false
	}

	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return false
	}
	return resp[0] == 0x05 && resp[1] != 0xff
}
