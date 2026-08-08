package proxy

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// Dial 通过该上游连接到目标地址 target（host:port）
func (u *Upstream) Dial(target string) (net.Conn, error) {
	if u == nil {
		return nil, errors.New("nil upstream")
	}
	var conn net.Conn
	var err error
	switch u.primaryProxyType() {
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		conn, err = u.dialHTTP(target)
	case ProxyTypeSocks5, ProxyTypeSocks5H:
		conn, err = u.dialSOCKS5(target)
	default:
		return nil, fmt.Errorf("unsupported upstream type %q", u.primaryProxyType())
	}
	if err != nil {
		// 未分类的错误（裸 net.Error、读写错误等）默认归因为上游，
		// 因为拨号到上游的连接失败最可能是上游本身的问题。
		var de *DialError
		if !errors.As(err, &de) {
			err = newDialError(ErrCategoryUpstream, "dial", err)
		}
		return nil, err
	}
	return u.trackConn(conn), nil
}

// Dial 尝试通过上游建立连接，单个上游失败时尝试下一个，全部失败后回退自身直连
func (m *UpstreamManager) Dial(target string, sourceIP ...string) (net.Conn, error) {
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	conn, _, err := m.DialProxyWithSourceIP(target, sip, nil)
	if err == nil {
		return conn, nil
	}
	return DialTarget(target)
}

// DialProxy 只通过上游建立连接，names 为空时使用全部上游。
// 等价于 DialProxyWithSourceIP(target, "", names)。
func (m *UpstreamManager) DialProxy(target string, names []string) (net.Conn, *Upstream, error) {
	return m.DialProxyWithSourceIP(target, "", names)
}

// DialProxyWithSourceIP 只通过上游建立连接，names 为空时使用全部上游。
// sourceIP 用于 source_ip_hash 策略的会话保持；其他策略忽略该值。
func (m *UpstreamManager) DialProxyWithSourceIP(target, sourceIP string, names []string) (net.Conn, *Upstream, error) {
	allowed := namesToSet(names)
	attempts := len(m.upstreams)
	if allowed != nil {
		attempts = len(allowed)
	}
	// 在单次 DialProxy 调用中跟踪已尝试过的上游，避免重复选择同一失败上游。
	tried := make(map[*Upstream]struct{})
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.selectFromExcluding(allowed, tried, target, sourceIP)
		if up == nil {
			break
		}
		tried[up] = struct{}{}
		conn, err := up.Dial(target)
		if err == nil {
			// 拨号成功即视为上游恢复健康，清除 dead 状态，使故障转移后能自愈，
			// 即使关闭了主动健康检查（check_interval=0）也生效。
			m.markAlive(up)
			return conn, up, nil
		}
		lastErr = err
		Logger().Warnw("upstream dial failed", "upstream", up.Name, "target", target, "error", err)
		MetricInc("yigate_upstream_dial_failures_total", "upstream="+up.Name)
		// 只有归因于上游本身的错误才推进健康状态转换；
		// 目标不可达或上游不支持某能力不影响节点整体健康。
		if IsUpstreamError(err) {
			m.markDead(up)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no available upstream")
	}
	return nil, nil, lastErr
}

func (m *UpstreamManager) DialOne(target, name string) (net.Conn, *Upstream, error) {
	m.mu.Lock()
	up := m.byName[name]
	if up != nil && !m.isSelectable(up) {
		up = nil
	}
	m.mu.Unlock()
	if up == nil {
		return nil, nil, fmt.Errorf("upstream %q unavailable", name)
	}
	conn, err := up.Dial(target)
	if err != nil {
		Logger().Warnw("upstream dial failed", "upstream", up.Name, "target", target, "error", err)
		if IsUpstreamError(err) {
			m.markDead(up)
		}
		return nil, nil, err
	}
	// 拨号成功即视为上游恢复健康，清除 dead 状态（与 DialProxyWithSourceIP 一致）。
	m.markAlive(up)
	return conn, up, nil
}

func (u *Upstream) isSocks5() bool {
	return u != nil && u.supportsProxyType(ProxyTypeSocks5)
}

// DialSOCKS5H 通过 SOCKS5 上游以域名地址类型连接目标，避免本地 DNS 解析。
func (u *Upstream) DialSOCKS5H(target string) (net.Conn, error) {
	if !u.isSocks5() {
		return nil, fmt.Errorf("upstream %q is not socks5", u.Name)
	}
	return u.dialSOCKS5(target)
}

// DialSOCKS5Bind 通过 SOCKS5 上游建立 BIND 请求，返回到上游的 TCP 连接和上游监听地址。
func (u *Upstream) DialSOCKS5Bind(target string) (net.Conn, string, error) {
	if !u.isSocks5() {
		return nil, "", fmt.Errorf("upstream %q is not socks5", u.Name)
	}
	conn, err := net.DialTimeout("tcp", proxyAddress(u.urlForType(ProxyTypeSocks5)), ProxyDialTimeout())
	if err != nil {
		return nil, "", err
	}
	if err := setConnDeadline(conn, ProxyHandshakeTimeout()); err != nil {
		conn.Close()
		return nil, "", err
	}
	if err := socks5ClientHandshake(conn, u); err != nil {
		conn.Close()
		return nil, "", err
	}
	listenAddr, err := socks5SendRequest(conn, 0x02, target)
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	_ = conn.SetDeadline(time.Time{})
	return u.trackConn(conn), listenAddr, nil
}

// DialSOCKS5UDP 通过 SOCKS5 上游建立 UDP ASSOCIATE 请求，返回到上游的 TCP 控制连接和上游 UDP 监听地址。
func (u *Upstream) DialSOCKS5UDP(target string) (net.Conn, *net.UDPAddr, error) {
	if !u.isSocks5() {
		return nil, nil, fmt.Errorf("upstream %q is not socks5", u.Name)
	}
	conn, err := net.DialTimeout("tcp", proxyAddress(u.urlForType(ProxyTypeSocks5)), ProxyDialTimeout())
	if err != nil {
		return nil, nil, err
	}
	if err := setConnDeadline(conn, ProxyHandshakeTimeout()); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := socks5ClientHandshake(conn, u); err != nil {
		conn.Close()
		return nil, nil, err
	}
	udpAddrStr, err := socks5SendRequest(conn, 0x03, target)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	udpAddr, err := net.ResolveUDPAddr("udp", udpAddrStr)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return u.trackConn(conn), udpAddr, nil
}

// DialSOCKS5Bind 按优先级选择支持 BIND 的 SOCKS5 上游，失败时尝试下一个，全部失败返回错误。
func (m *UpstreamManager) DialSOCKS5Bind(target string, names []string) (net.Conn, string, *Upstream, error) {
	attempts := m.Socks5AttemptCount(names)
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.SelectSocks5ForBind(names)
		if up == nil {
			break
		}
		conn, addr, err := up.DialSOCKS5Bind(target)
		if err == nil {
			m.markCapabilitySupported(up, "bind")
			return conn, addr, up, nil
		}
		lastErr = err
		Logger().Warnw("upstream bind failed", "upstream", up.Name, "target", target, "error", err)
		if IsCapabilityError(err) {
			m.markCapabilityUnsupported(up, "bind")
		} else if IsUpstreamError(err) {
			m.markDeadImmediate(up)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream for bind")
	}
	return nil, "", nil, lastErr
}

// DialSOCKS5UDP 按优先级选择支持 UDP ASSOCIATE 的 SOCKS5 上游，失败时尝试下一个，全部失败返回错误。
func (m *UpstreamManager) DialSOCKS5UDP(target string, names []string) (net.Conn, *net.UDPAddr, *Upstream, error) {
	attempts := m.Socks5AttemptCount(names)
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.SelectSocks5ForUDP(names)
		if up == nil {
			break
		}
		conn, addr, err := up.DialSOCKS5UDP(target)
		if err == nil {
			m.markCapabilitySupported(up, "udp_associate")
			return conn, addr, up, nil
		}
		lastErr = err
		Logger().Warnw("upstream udp associate failed", "upstream", up.Name, "target", target, "error", err)
		if IsCapabilityError(err) {
			m.markCapabilityUnsupported(up, "udp_associate")
		} else if IsUpstreamError(err) {
			m.markDeadImmediate(up)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream for udp associate")
	}
	return nil, nil, nil, lastErr
}
