package proxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

// ErrUpstreamNotFound 表示指定名称的上游不在运行时调度池中（不存在或已禁用）。
var ErrUpstreamNotFound = errors.New("upstream not found")

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
				m.markProbeFailure(u)
				// 一次失败后按更短的间隔重试，避免必须等满一个检查周期
				// 才凑够失败阈值（例如 30s 周期 × 3 次 = 90s 才发现）。
				m.scheduleQuickRetry(u, globalIv)
			}
			m.mu.Lock()
			u.lastCheckAt = time.Now()
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

// scheduleQuickRetry 在探测失败后提前下一次检查时间（仅针对仍未摘除的节点，
// 已摘除节点由恢复探测器接管）。
func (m *UpstreamManager) scheduleQuickRetry(u *Upstream, globalIv time.Duration) {
	iv := u.HealthCheckInterval()
	if iv <= 0 {
		iv = globalIv
	}
	if iv <= probeFailureRetryInterval {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.dead {
		return
	}
	next := time.Now().Add(probeFailureRetryInterval)
	if u.nextCheckAt.IsZero() || next.Before(u.nextCheckAt) {
		u.nextCheckAt = next
	}
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

// UpstreamCheckResult 表示一次手动健康检查结果。
type UpstreamCheckResult struct {
	Name      string    `json:"name"`
	Alive     bool      `json:"alive"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// CheckUpstream 立即对指定名称的上游执行一次健康检查，并同步更新其 alive/dead 状态。
// 该检查不影响定时健康检查调度，适合 WebUI 手动触发或单点巡检。
func (m *UpstreamManager) CheckUpstream(name string) (UpstreamCheckResult, error) {
	// 合并同一上游的并发检查：定时任务、一键巡检与手动点击可能同时触发。
	value, err, _ := m.checkSF.Do(name, func() (any, error) {
		return m.checkUpstreamOnce(name)
	})
	if err != nil {
		return UpstreamCheckResult{}, err
	}
	return value.(UpstreamCheckResult), nil
}

func (m *UpstreamManager) checkUpstreamOnce(name string) (UpstreamCheckResult, error) {
	m.mu.Lock()
	u := m.byName[name]
	m.mu.Unlock()
	if u == nil {
		return UpstreamCheckResult{}, fmt.Errorf("%w: %q", ErrUpstreamNotFound, name)
	}

	alive := m.check(u)
	if alive {
		m.markAlive(u)
	} else {
		m.markProbeFailure(u)
	}

	checkedAt := time.Now()
	m.mu.Lock()
	u.lastCheckAt = checkedAt
	dead := u.dead
	consecutiveDead := u.consecutiveDead
	m.mu.Unlock()

	status := 1.0
	if dead {
		status = 0
	}
	MetricSet("yigate_upstream_health_status", status, "upstream="+u.Name)

	res := UpstreamCheckResult{
		Name:      u.Name,
		Alive:     !dead,
		CheckedAt: checkedAt,
	}
	if dead {
		res.Error = fmt.Sprintf("unhealthy (consecutive dead: %d)", consecutiveDead)
	}
	return res, nil
}

// CheckAllUpstreams 立即对所有上游执行一次并发健康检查，并同步更新各自状态。
// 返回结果按上游名称排序，便于前端展示巡检报告。
func (m *UpstreamManager) CheckAllUpstreams() []UpstreamCheckResult {
	m.mu.Lock()
	ups := append([]*Upstream(nil), m.upstreams...)
	m.mu.Unlock()

	results := make([]UpstreamCheckResult, len(ups))
	var wg sync.WaitGroup
	// 限制并发探测数量，避免上游很多时瞬间耗尽文件描述符。
	limit := make(chan struct{}, 8)
	for i, u := range ups {
		wg.Add(1)
		go func(idx int, up *Upstream) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			res, _ := m.CheckUpstream(up.Name)
			results[idx] = res
		}(i, u)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
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
			lease.up.lastCheckAt = time.Now()
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

// checkHTTPProtocol 探测 HTTP 代理上游。
//
// 两种模式：
//   - 浅探测（未配置 health_check.target）：向 127.0.0.1:1 发起 CONNECT。
//     目标必然立即失败，探测目的只是验证上游能按 HTTP 代理协议正确应答，
//     既不依赖外网，也不会把上游自身地址当目标而形成自环。
//     任何合法响应（含 403/502/503）都说明节点在服务；401/407 表示凭据
//     不可用，仍判为不可用。
//   - 深探测（配置了 target）：真实发起一次 CONNECT（或 absolute-URI GET，
//     当同时配置了 path），按 expected_codes 判定，默认 2xx 为可用。
func checkHTTPProtocol(conn net.Conn, up *Upstream, timeout time.Duration) bool {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	target := up.HealthCheckTarget()
	path := up.HealthCheckPath()
	deep := target != ""
	if !deep {
		target = livenessProbeTarget
	}

	creds := proxyCredentials(up)
	auth := ""
	if creds != "" {
		auth = "Proxy-Authorization: Basic " + creds + "\r\n"
	}

	var req string
	reqMethod := http.MethodConnect
	if deep && path != "" {
		// 正向代理只接受 absolute-URI 形式，origin-form 会被视为非法请求。
		req = fmt.Sprintf("GET http://%s%s HTTP/1.1\r\nHost: %s\r\n%sConnection: close\r\n\r\n", target, path, target, auth)
		reqMethod = http.MethodGet
	} else {
		req = fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, auth)
	}
	if _, err := conn.Write([]byte(req)); err != nil {
		return false
	}

	br := bufio.NewReader(conn)
	// 传入请求方法，ReadResponse 才能识别 CONNECT 200 是隧道响应而不是
	// 无长度 body，避免后续读取卡到超时。
	resp, err := http.ReadResponse(br, &http.Request{Method: reqMethod})
	if err != nil {
		return false
	}
	// 不断开 body：探测只关心状态码，连接由调用方关闭，
	// 强行 Close 会在隧道场景下阻塞到超时。
	status := resp.StatusCode

	// 认证失败意味着这组凭据用不了，属于节点不可用。
	if status == http.StatusProxyAuthRequired || status == http.StatusUnauthorized {
		return false
	}
	if codes := up.HealthCheckExpectedCodes(); len(codes) > 0 {
		for _, code := range codes {
			if status == code {
				return true
			}
		}
		return false
	}
	if deep {
		return status/100 == 2
	}
	// 浅探测：能正确应答协议就说明节点在服务，负载保护返回的 503 不算故障。
	return true
}

func checkSOCKS5Protocol(conn net.Conn, up *Upstream, timeout time.Duration) bool {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

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
		if authResp[0] != 0x01 || authResp[1] != 0x00 {
			return false
		}
	case 0xff:
		// 服务器不接受任何支持的认证方法。
		return false
	default:
		return false
	}

	// 深探测：真实发起一次 CONNECT，验证上游能把流量带出去。
	// 未配置 target 时到此即完成"协议层存活"判定。
	if target := up.HealthCheckTarget(); target != "" {
		if _, err := socks5SendRequest(conn, 0x01, target); err != nil {
			return false
		}
	}
	return true
}
