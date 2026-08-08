package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// StartHealthCheck 启动定时健康检查。
// alive 上游按 interval 检查；dead 上游由独立的恢复探测器以更短间隔（2s）
// 按指数退避调度检测，避免恢复延迟过大。
// 若某个上游配置了 health_check.interval，则按该上游自己的间隔检查。
func (m *UpstreamManager) StartHealthCheck(interval int) {
	// interval <= 0 表示停止健康检查：先停旧再直接返回。
	m.StopHealthCheck()
	if interval <= 0 {
		return
	}

	m.mu.Lock()
	m.checkInterval = interval
	m.done = make(chan struct{})
	done := m.done
	now := time.Now()
	for _, u := range m.upstreams {
		u.nextCheckAt = now
	}
	m.wg.Add(2)
	m.mu.Unlock()

	minInterval := m.minCheckInterval(interval)

	// 常规健康检查：按最小间隔触发，但只检查已到期的上游。
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(minInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				m.checkDue(interval)
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

// minCheckInterval 计算健康检查的最小触发间隔。
func (m *UpstreamManager) minCheckInterval(globalInterval int) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	min := time.Duration(globalInterval) * time.Second
	if min <= 0 {
		min = 2 * time.Second
	}
	for _, u := range m.upstreams {
		iv := u.HealthCheckInterval()
		if iv > 0 && iv < min {
			min = iv
		}
	}
	if min < 500*time.Millisecond {
		min = 500 * time.Millisecond
	}
	return min
}

// checkDue 检查已到期的上游。
func (m *UpstreamManager) checkDue(globalInterval int) {
	now := time.Now()
	var toCheck []*Upstream
	m.mu.Lock()
	globalIv := time.Duration(globalInterval) * time.Second
	for _, u := range m.upstreams {
		if u.dead {
			continue
		}
		iv := u.HealthCheckInterval()
		if iv <= 0 {
			iv = globalIv
		}
		if !now.Before(u.nextCheckAt) {
			toCheck = append(toCheck, u)
			u.nextCheckAt = now.Add(iv)
		}
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, u := range toCheck {
		wg.Add(1)
		go func(u *Upstream) {
			defer wg.Done()
			alive := m.check(u)
			if alive {
				m.markAlive(u)
			} else {
				m.markDead(u)
			}
			m.mu.Lock()
			dead := u.dead
			m.mu.Unlock()
			status := 1.0
			if dead {
				status = 0
				Logger().Warnw("upstream dead", "upstream", u.URL.Host, "name", u.Name)
			}
			MetricSet("yigate_upstream_health_status", status, "upstream="+u.Name)
		}(u)
	}
	wg.Wait()
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

// probeDeadUpstreams 检查 nextProbeAt 已到期的 open 上游。
func (m *UpstreamManager) probeDeadUpstreams() {
	now := time.Now()
	type probeLease struct {
		up         *Upstream
		generation uint64
	}
	leases := make([]probeLease, 0)
	m.mu.Lock()
	upstreams := append([]*Upstream(nil), m.upstreams...)
	m.mu.Unlock()
	for _, up := range upstreams {
		if generation, ok := m.beginProbe(up, now); ok {
			leases = append(leases, probeLease{up: up, generation: generation})
		}
	}

	var wg sync.WaitGroup
	for _, lease := range leases {
		wg.Add(1)
		go func(lease probeLease) {
			defer wg.Done()
			alive := m.check(lease.up)
			recovered := m.applyProbeResult(lease.up, lease.generation, alive, time.Now())
			if recovered {
				Logger().Infow("upstream recovered (probe)", "upstream", lease.up.URL.Host, "name", lease.up.Name)
			}
			m.mu.Lock()
			dead := lease.up.dead
			m.mu.Unlock()
			status := 1.0
			if dead {
				status = 0
			}
			MetricSet("yigate_upstream_health_status", status, "upstream="+lease.up.Name)
		}(lease)
	}
	wg.Wait()
}

func (m *UpstreamManager) check(u *Upstream) bool {
	timeout := u.HealthCheckTimeout()
	addr := proxyAddress(u.URL)

	switch u.HealthCheckType() {
	case "tcp":
		conn, err := dialProxyConn(u.URL, timeout)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	case "tls":
		return checkTLSUpstream(addr, timeout)
	case "http", "https":
		conn, err := dialProxyConn(u.URL, timeout)
		if err != nil {
			return false
		}
		defer conn.Close()
		return checkHTTPProtocol(conn, u, timeout)
	case ProxyTypeSocks5, ProxyTypeSocks5H:
		conn, err := dialProxyConn(u.URL, timeout)
		if err != nil {
			return false
		}
		defer conn.Close()
		return checkSOCKS5Protocol(conn, u, timeout)
	default:
		conn, err := dialProxyConn(u.URL, timeout)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
}

func checkTLSUpstream(addr string, timeout time.Duration) bool {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfigForHost(addr))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkHTTPProtocol(conn net.Conn, up *Upstream, timeout time.Duration) bool {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	defer conn.SetDeadline(time.Time{})

	creds := proxyCredentials(up)
	auth := ""
	if creds != "" {
		auth = "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	path := up.HealthCheckPath()
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", path, up.URL.Host, auth)
	if _, err := conn.Write([]byte(req)); err != nil {
		return false
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	codes := up.HealthCheckExpectedCodes()
	if len(codes) == 0 {
		return resp.StatusCode/100 == 2
	}
	for _, code := range codes {
		if resp.StatusCode == code {
			return true
		}
	}
	return false
}

func checkSOCKS5Protocol(conn net.Conn, up *Upstream, timeout time.Duration) bool {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	defer conn.SetDeadline(time.Time{})

	user, pass := "", ""
	if up.URL != nil && up.URL.User != nil {
		user = up.URL.User.Username()
		pass, _ = up.URL.User.Password()
	}

	// 协商认证方法：无认证 (0x00) 和用户名/密码 (0x02)。
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
	if resp[0] != 0x05 {
		return false
	}
	switch resp[1] {
	case 0x00:
		// 无需认证，协议握手成功。
		return true
	case 0x02:
		// 用户名/密码认证 (RFC 1929)。
		if user == "" && pass == "" {
			return false
		}
		authReq := []byte{0x01, byte(len(user))}
		authReq = append(authReq, []byte(user)...)
		authReq = append(authReq, byte(len(pass)))
		authReq = append(authReq, []byte(pass)...)
		if _, err := conn.Write(authReq); err != nil {
			return false
		}
		var authResp [2]byte
		if _, err := io.ReadFull(conn, authResp[:]); err != nil {
			return false
		}
		return authResp[0] == 0x01 && authResp[1] == 0x00
	case 0xff:
		// 服务器不接受任何支持的认证方法。
		return false
	default:
		return false
	}
}
