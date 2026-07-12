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

// StartHealthCheck 启动定时健康检查。
// alive 上游按 interval 检查；dead 上游由独立的恢复探测器以更短间隔（2s）
// 按指数退避调度检测，避免恢复延迟过大。
func (m *UpstreamManager) StartHealthCheck(interval int) {
	if interval <= 0 {
		return
	}
	m.StopHealthCheck()

	m.mu.Lock()
	m.checkInterval = interval
	m.done = make(chan struct{})
	done := m.done
	m.wg.Add(2)
	m.mu.Unlock()

	// 常规健康检查：按配置间隔检查所有上游。
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

	// 恢复探测器：以 2s 间隔探测到期的 dead 上游。
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				m.probeDeadUpstreams()
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
				u.failures = 0
				u.dead = false
				u.consecutiveDead = 0
				u.nextProbeAt = time.Time{}
			} else if !u.dead {
				// alive 上游重置失败计数
				if alive {
					u.failures = 0
				} else {
					u.failures++
					if u.failures >= deadFailureThreshold {
						u.dead = true
						u.deadSince = time.Now()
						u.consecutiveDead = 1
						u.nextProbeAt = time.Now().Add(deadProbeBaseInterval)
						Logger().Warnw("upstream dead", "upstream", u.URL.Host, "name", u.Name)
					}
				}
			}
			status := 0.0
			if alive || !u.dead {
				status = 1.0
			}
			MetricSet("k5pw_upstream_health_status", status, "upstream="+u.Name)
			m.mu.Unlock()

		}(u)
	}
	wg.Wait()
}

// probeDeadUpstreams 检查 nextProbeAt 已到期的 dead 上游，成功则恢复。
func (m *UpstreamManager) probeDeadUpstreams() {
	now := time.Now()
	var toProbe []*Upstream
	m.mu.Lock()
	for _, u := range m.upstreams {
		if u.dead && !u.nextProbeAt.IsZero() && now.After(u.nextProbeAt) {
			toProbe = append(toProbe, u)
		}
	}
	m.mu.Unlock()

	for _, u := range toProbe {
		alive := m.check(u)
		m.mu.Lock()
		if alive {
			Logger().Infow("upstream recovered (probe)", "upstream", u.URL.Host, "name", u.Name)
			u.failures = 0
			u.dead = false
			u.consecutiveDead = 0
			u.nextProbeAt = time.Time{}
			MetricSet("k5pw_upstream_health_status", 1.0, "upstream="+u.Name)
		} else {
			// 仍未恢复，调度下次探测（指数退避）
			u.consecutiveDead++
			backoff := deadProbeBaseInterval * time.Duration(1<<uint(u.consecutiveDead-1))
			if backoff > deadProbeMaxInterval {
				backoff = deadProbeMaxInterval
			}
			u.nextProbeAt = time.Now().Add(backoff)
			MetricSet("k5pw_upstream_health_status", 0.0, "upstream="+u.Name)
		}
		m.mu.Unlock()
	}
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
