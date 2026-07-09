package proxy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ProxyTypeHTTP    = "http"
	ProxyTypeHTTPS   = "https"
	ProxyTypeSocks5  = "socks5"
	ProxyTypeSocks5H = "socks5h"
)

func normalizeProxyTypes(values []string) ([]string, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	seen := make(map[string]struct{}, len(values))
	types := make([]string, 0, len(values))
	for _, value := range values {
		typ := normalizeProxyType(value)
		if typ == "" {
			return nil, true, fmt.Errorf("unsupported proxy type %q", value)
		}
		if _, ok := seen[typ]; ok {
			continue
		}
		seen[typ] = struct{}{}
		types = append(types, typ)
	}
	return types, true, nil
}

func normalizeProxyType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProxyTypeHTTP:
		return ProxyTypeHTTP
	case ProxyTypeHTTPS:
		return ProxyTypeHTTPS
	case ProxyTypeSocks5:
		return ProxyTypeSocks5
	case ProxyTypeSocks5H:
		return ProxyTypeSocks5H
	default:
		return ""
	}
}

func (u *Upstream) proxyTypes() []string {
	if u == nil {
		return nil
	}
	u.typesMu.RLock()
	defer u.typesMu.RUnlock()
	return append([]string(nil), u.Types...)
}

func (u *Upstream) supportsProxyType(typ string) bool {
	if u == nil {
		return false
	}
	typ = normalizeProxyType(typ)
	if typ == "" {
		return false
	}
	u.typesMu.RLock()
	defer u.typesMu.RUnlock()
	for _, candidate := range u.Types {
		if candidate == typ || (typ == ProxyTypeSocks5 && candidate == ProxyTypeSocks5H) {
			return true
		}
	}
	return false
}

func (u *Upstream) primaryProxyType() string {
	if u == nil {
		return ""
	}
	types := u.proxyTypes()
	if len(types) > 0 {
		return types[0]
	}
	if u.URL != nil {
		return normalizeProxyType(u.URL.Scheme)
	}
	return ""
}

func (u *Upstream) urlForType(typ string) *url.URL {
	if u == nil || u.URL == nil {
		return nil
	}
	proxyURL := *u.URL
	if typ = normalizeProxyType(typ); typ != "" {
		proxyURL.Scheme = typ
	}
	return &proxyURL
}

func (u *Upstream) withProxyType(typ string) *Upstream {
	proxyURL := u.urlForType(typ)
	if proxyURL == nil {
		return u
	}
	return &Upstream{Name: u.Name, URL: proxyURL}
}

func ValidateProxyTypes(values []string) error {
	_, _, err := normalizeProxyTypes(values)
	return err
}

// UpstreamConfig 单个上游代理配置
type UpstreamConfig struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Types    []string `yaml:"types"`
	Priority int      `yaml:"priority"`
	Weight   int      `yaml:"weight"`
}

// Upstream 表示一个已解析的上游代理
type Upstream struct {
	Name     string
	URL      *url.URL
	Types    []string
	Priority int
	Weight   int

	typesMu sync.RWMutex
	current int
	dead    bool
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

func (c *bufferedConn) CloseRead() error {
	if cr, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return cr.CloseRead()
	}
	return nil
}

// UpstreamManager 管理多上游、优先级、负载均衡与健康检查
type UpstreamManager struct {
	upstreams     []*Upstream
	byName        map[string]*Upstream
	groups        map[int][]*Upstream
	order         []int
	mu            sync.Mutex
	done          chan struct{}
	wg            sync.WaitGroup
	checkInterval int
}

// NewUpstreamManager 根据配置创建上游管理器
func NewUpstreamManager(configs []UpstreamConfig) (*UpstreamManager, error) {
	m := &UpstreamManager{
		byName: make(map[string]*Upstream),
		groups: make(map[int][]*Upstream),
	}
	for i, c := range configs {
		if c.URL == "" {
			continue
		}
		expandedURL := os.ExpandEnv(c.URL)
		u, err := url.Parse(expandedURL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream %q: %w", c.URL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
			return nil, fmt.Errorf("unsupported upstream scheme %q in %q", u.Scheme, c.URL)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("upstream host missing in %q", c.URL)
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", i+1)
		}
		if _, exists := m.byName[name]; exists {
			return nil, fmt.Errorf("duplicate upstream name %q", name)
		}
		types, _, err := normalizeProxyTypes(c.Types)
		if err != nil {
			return nil, fmt.Errorf("upstreams[%d].types: %w", i, err)
		}
		if len(types) == 0 {
			types = []string{normalizeProxyType(u.Scheme)}
		}
		up := &Upstream{
			Name:     name,
			URL:      u,
			Types:    types,
			Priority: c.Priority,
			Weight:   c.Weight,
		}
		if up.Weight <= 0 {
			up.Weight = 1
		}
		m.upstreams = append(m.upstreams, up)
		m.byName[name] = up
		m.groups[c.Priority] = append(m.groups[c.Priority], up)
	}
	for p := range m.groups {
		m.order = append(m.order, p)
	}
	sort.Ints(m.order)
	return m, nil
}

// Select 按优先级选择可用上游，同优先级加权轮询
// 所有上游均不可用时返回 nil。
func (m *UpstreamManager) Select() *Upstream {
	return m.selectFrom(nil)
}

func (m *UpstreamManager) selectFrom(allowed map[string]struct{}) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if u.dead {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			alive = append(alive, u)
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
}

func (m *UpstreamManager) SelectFor(names []string) *Upstream {
	return m.selectFrom(namesToSet(names))
}

func (m *UpstreamManager) SelectSocks5For(names []string) *Upstream {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if u.dead || !u.supportsProxyType(ProxyTypeSocks5) {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[u.Name]; !ok {
					continue
				}
			}
			alive = append(alive, u)
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
}

func (m *UpstreamManager) AttemptCount(names []string) int {
	if len(names) == 0 {
		return len(m.upstreams)
	}
	return len(names)
}

func (m *UpstreamManager) Socks5AttemptCount(names []string) int {
	allowed := namesToSet(names)
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, up := range m.upstreams {
		if !up.supportsProxyType(ProxyTypeSocks5) {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[up.Name]; !ok {
				continue
			}
		}
		count++
	}
	return count
}

func (m *UpstreamManager) Socks5ByName(name string) *Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	up := m.byName[name]
	if up == nil || up.dead || !up.supportsProxyType(ProxyTypeSocks5) {
		return nil
	}
	return up
}

// pickWeighted 平滑加权轮询
func pickWeighted(us []*Upstream) *Upstream {
	total := 0
	for _, u := range us {
		total += u.Weight
	}
	if total == 0 {
		return us[0]
	}

	chosen := 0
	max := -1
	for i, u := range us {
		u.current += u.Weight
		if u.current > max {
			max = u.current
			chosen = i
		}
	}

	us[chosen].current -= total
	return us[chosen]
}

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
				log.Printf("upstream %s recovered", u.URL.Host)
			} else if !u.dead && !alive {
				log.Printf("upstream %s dead", u.URL.Host)
			}
			u.dead = !alive
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

// Dial 通过该上游连接到目标地址 target（host:port）
func (u *Upstream) Dial(target string) (net.Conn, error) {
	if u == nil {
		return nil, errors.New("nil upstream")
	}
	switch u.primaryProxyType() {
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		return u.dialHTTP(target)
	case ProxyTypeSocks5, ProxyTypeSocks5H:
		return u.dialSOCKS5(target)
	}
	return nil, fmt.Errorf("unsupported upstream type %q", u.primaryProxyType())
}

// Dial 尝试通过上游建立连接，单个上游失败时尝试下一个，全部失败后回退自身直连
func (m *UpstreamManager) Dial(target string) (net.Conn, error) {
	conn, _, err := m.DialProxy(target, nil)
	if err == nil {
		return conn, nil
	}
	return DialTarget(target)
}

// DialProxy 只通过上游建立连接，names 为空时使用全部上游。
func (m *UpstreamManager) DialProxy(target string, names []string) (net.Conn, *Upstream, error) {
	allowed := namesToSet(names)
	attempts := len(m.upstreams)
	if allowed != nil {
		attempts = len(allowed)
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.selectFrom(allowed)
		if up == nil {
			break
		}
		conn, err := up.Dial(target)
		if err == nil {
			return conn, up, nil
		}
		lastErr = err
		log.Printf("upstream %s dial %s failed: %v", up.Name, target, err)
		m.markDead(up)
	}
	if lastErr == nil {
		lastErr = errors.New("no available upstream")
	}
	return nil, nil, lastErr
}

func (m *UpstreamManager) DialOne(target, name string) (net.Conn, *Upstream, error) {
	m.mu.Lock()
	up := m.byName[name]
	if up != nil && up.dead {
		up = nil
	}
	m.mu.Unlock()
	if up == nil {
		return nil, nil, fmt.Errorf("upstream %q unavailable", name)
	}
	conn, err := up.Dial(target)
	if err != nil {
		log.Printf("upstream %s dial %s failed: %v", up.Name, target, err)
		m.markDead(up)
		return nil, nil, err
	}
	return conn, up, nil
}

func (m *UpstreamManager) markDead(up *Upstream) {
	m.mu.Lock()
	if m.checkInterval <= 0 {
		m.mu.Unlock()
		return
	}
	up.dead = true
	m.mu.Unlock()
}

func (u *Upstream) isSocks5() bool {
	return u != nil && u.supportsProxyType(ProxyTypeSocks5)
}

func namesToSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	return set
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
	return conn, listenAddr, nil
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
	return conn, udpAddr, nil
}

// DialSOCKS5Bind 按优先级选择 SOCKS5 上游建立 BIND，失败时尝试下一个，全部失败返回错误。
func (m *UpstreamManager) DialSOCKS5Bind(target string, names []string) (net.Conn, string, *Upstream, error) {
	attempts := m.Socks5AttemptCount(names)
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.SelectSocks5For(names)
		if up == nil {
			break
		}
		conn, addr, err := up.DialSOCKS5Bind(target)
		if err == nil {
			return conn, addr, up, nil
		}
		lastErr = err
		log.Printf("upstream %s bind %s failed: %v", up.Name, target, err)
		m.markDead(up)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream")
	}
	return nil, "", nil, lastErr
}

// DialSOCKS5UDP 按优先级选择 SOCKS5 上游建立 UDP ASSOCIATE，失败时尝试下一个，全部失败返回错误。
func (m *UpstreamManager) DialSOCKS5UDP(target string, names []string) (net.Conn, *net.UDPAddr, *Upstream, error) {
	attempts := m.Socks5AttemptCount(names)
	var lastErr error
	for i := 0; i < attempts; i++ {
		up := m.SelectSocks5For(names)
		if up == nil {
			break
		}
		conn, addr, err := up.DialSOCKS5UDP(target)
		if err == nil {
			return conn, addr, up, nil
		}
		lastErr = err
		log.Printf("upstream %s udp associate %s failed: %v", up.Name, target, err)
		m.markDead(up)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream")
	}
	return nil, nil, nil, lastErr
}

// dialHTTP 通过 HTTP/HTTPS 代理的 CONNECT 建立隧道
func (u *Upstream) dialHTTP(target string) (net.Conn, error) {
	conn, err := dialProxyConn(u.urlForType(u.primaryProxyType()), ProxyDialTimeout())
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
		return nil, fmt.Errorf("upstream CONNECT failed: %s", resp.Status)
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

func dialProxyConn(u *url.URL, timeout time.Duration) (net.Conn, error) {
	addr := proxyAddress(u)
	dialer := &net.Dialer{Timeout: timeout}
	if u.Scheme == "https" {
		return tls.DialWithDialer(dialer, "tcp", addr, tlsConfigForHost(addr))
	}
	return dialer.Dial("tcp", addr)
}

func tlsConfigForHost(addr string) *tls.Config {
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

// dialSOCKS5 通过 SOCKS5 上游连接到目标
func (u *Upstream) dialSOCKS5(target string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddress(u.urlForType(ProxyTypeSocks5)), ProxyDialTimeout())
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, ProxyHandshakeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := socks5ClientHandshake(conn, u); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := socks5SendRequest(conn, 0x01, target); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func socks5Auth(conn net.Conn, username, password string) error {
	if len(username) > 255 || len(password) > 255 {
		return errors.New("socks5 username/password too long")
	}
	buf := make([]byte, 0, 513)
	buf = append(buf, 0x01, byte(len(username)))
	buf = append(buf, []byte(username)...)
	buf = append(buf, byte(len(password)))
	buf = append(buf, []byte(password)...)
	if _, err := conn.Write(buf); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}
	if resp[0] != 0x01 || resp[1] != 0x00 {
		return errors.New("socks5 auth failed")
	}
	return nil
}

// socks5ClientHandshake 完成 SOCKS5 方法协商与用户名密码认证。
func socks5ClientHandshake(conn net.Conn, u *Upstream) error {
	user := ""
	pass := ""
	if u.URL != nil && u.URL.User != nil {
		user = u.URL.User.Username()
		pass, _ = u.URL.User.Password()
	}
	methods := []byte{0x00}
	if user != "" || pass != "" {
		if len(user) > 255 || len(pass) > 255 {
			return errors.New("socks5 credentials too long")
		}
		methods = append(methods, 0x02)
	}
	greeting := []byte{0x05, byte(len(methods))}
	greeting = append(greeting, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return err
	}
	var methodResp [2]byte
	if _, err := io.ReadFull(conn, methodResp[:]); err != nil {
		return err
	}
	if methodResp[0] != 0x05 || methodResp[1] == 0xff {
		return errors.New("socks5 auth method rejected")
	}
	if methodResp[1] == 0x02 {
		if err := socks5Auth(conn, user, pass); err != nil {
			return err
		}
	}
	return nil
}

// socks5SendRequest 向已认证连接的 SOCKS5 服务器发送 cmd 请求并读取第一次回复。
// 返回回复中的地址字符串以及错误。
func socks5SendRequest(conn net.Conn, cmd byte, target string) (string, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return "", fmt.Errorf("invalid target port %q", portStr)
	}
	addr, err := socks5AddressBytes(host)
	if err != nil {
		return "", err
	}
	packet := []byte{0x05, cmd, 0x00}
	packet = append(packet, addr...)
	packet = append(packet, byte(port>>8), byte(port))
	if _, err := conn.Write(packet); err != nil {
		return "", err
	}

	var reply [4]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return "", err
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		if reply[0] == 0x05 && reply[1] >= 0x01 && reply[1] <= 0x06 {
			return "", fmt.Errorf("socks5 target unreachable: 0x%02x", reply[1])
		}
		return "", fmt.Errorf("socks5 upstream protocol failed: 0x%02x", reply[1])
	}
	replyHost, err := readSOCKS5AddressValue(conn, reply[3])
	if err != nil {
		return "", err
	}
	replyPort, err := readSOCKS5PortValue(conn)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(replyHost, strconv.Itoa(replyPort)), nil
}

// DialTarget 直接连接到目标
func DialTarget(target string) (net.Conn, error) {
	return DialTargetTimeout(target, ProxyDialTimeout())
}

func DialTargetTimeout(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", target, timeout)
}

// Tunnel 在客户端与目标连接之间双向转发数据
// clientReader 使用 bufio.Reader 是为了保留已经预读的字节
func Tunnel(clientConn net.Conn, clientReader *bufio.Reader, targetConn net.Conn) {
	defer targetConn.Close()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var src io.Reader
		if clientReader != nil {
			src = clientReader
		} else {
			src = clientConn
		}
		dr := &deadlineReader{r: src, conn: clientConn, timeout: TunnelIdleTimeout()}
		dst := &deadlineWriter{w: targetConn, conn: targetConn, timeout: TunnelIdleTimeout()}
		_, _ = io.Copy(dst, dr)
		closeWrite(targetConn)
		closeRead(clientConn)
	}()

	go func() {
		defer wg.Done()
		src := &deadlineReader{r: targetConn, conn: targetConn, timeout: TunnelIdleTimeout()}
		dst := &deadlineWriter{w: clientConn, conn: clientConn, timeout: TunnelIdleTimeout()}
		_, _ = io.Copy(dst, src)
		closeWrite(clientConn)
		closeRead(targetConn)
	}()

	wg.Wait()
}

func closeWrite(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = c.CloseWrite()
		return
	}
	_ = conn.Close()
}

func closeRead(conn net.Conn) {
	if c, ok := conn.(interface{ CloseRead() error }); ok {
		_ = c.CloseRead()
		return
	}
	_ = conn.Close()
}
