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
	"sort"
	"strconv"
	"sync"
	"time"
)

// UpstreamConfig 单个上游代理配置
type UpstreamConfig struct {
	URL      string `yaml:"url"`
	Priority int    `yaml:"priority"`
	Weight   int    `yaml:"weight"`
}

// Upstream 表示一个已解析的上游代理
type Upstream struct {
	URL      *url.URL
	Priority int
	Weight   int

	mu      sync.Mutex
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

// UpstreamManager 管理多上游、优先级、负载均衡与健康检查
type UpstreamManager struct {
	upstreams []*Upstream
	groups    map[int][]*Upstream
	order     []int
	mu        sync.RWMutex
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewUpstreamManager 根据配置创建上游管理器
func NewUpstreamManager(configs []UpstreamConfig) (*UpstreamManager, error) {
	m := &UpstreamManager{
		groups: make(map[int][]*Upstream),
	}
	for _, c := range configs {
		if c.URL == "" {
			continue
		}
		u, err := url.Parse(c.URL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream %q: %w", c.URL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
			return nil, fmt.Errorf("unsupported upstream scheme %q in %q", u.Scheme, c.URL)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("upstream host missing in %q", c.URL)
		}
		up := &Upstream{
			URL:      u,
			Priority: c.Priority,
			Weight:   c.Weight,
		}
		if up.Weight <= 0 {
			up.Weight = 1
		}
		m.upstreams = append(m.upstreams, up)
		m.groups[c.Priority] = append(m.groups[c.Priority], up)
	}
	for p := range m.groups {
		m.order = append(m.order, p)
	}
	sort.Ints(m.order)
	return m, nil
}

// Select 按优先级选择可用上游，同优先级加权轮询
// 所有上游均不可用时返回 nil，表示回退到自身直连
func (m *UpstreamManager) Select() *Upstream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.order {
		group := m.groups[p]
		alive := make([]*Upstream, 0, len(group))
		for _, u := range group {
			if !u.dead {
				alive = append(alive, u)
			}
		}
		if len(alive) == 0 {
			continue
		}
		return pickWeighted(alive)
	}
	return nil
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
		u.mu.Lock()
		u.current += u.Weight
		cur := u.current
		u.mu.Unlock()

		if cur > max {
			max = cur
			chosen = i
		}
	}

	us[chosen].mu.Lock()
	us[chosen].current -= total
	us[chosen].mu.Unlock()
	return us[chosen]
}

// StartHealthCheck 启动定时健康检查
func (m *UpstreamManager) StartHealthCheck(interval int) {
	if interval <= 0 {
		interval = 30
	}
	m.StopHealthCheck()
	m.done = make(chan struct{})
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				m.checkAll()
			}
		}
	}()
}

// StopHealthCheck 停止健康检查
func (m *UpstreamManager) StopHealthCheck() {
	if m.done != nil {
		close(m.done)
		m.wg.Wait()
		m.done = nil
	}
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
	host := u.URL.Host
	var conn net.Conn
	var err error
	if u.URL.Scheme == "https" {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.DialTimeout("tcp", host, 5*time.Second)
	}
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Dial 通过该上游连接到目标地址 target（host:port）
func (u *Upstream) Dial(target string) (net.Conn, error) {
	switch u.URL.Scheme {
	case "http", "https":
		return u.dialHTTP(target)
	case "socks5":
		return u.dialSOCKS5(target)
	}
	return nil, fmt.Errorf("unsupported scheme %q", u.URL.Scheme)
}

// Dial 尝试通过上游建立连接，单个上游失败时尝试下一个，全部失败后回退自身直连
func (m *UpstreamManager) Dial(target string) (net.Conn, error) {
	for i := 0; i < len(m.upstreams); i++ {
		up := m.Select()
		if up == nil {
			break
		}
		conn, err := up.Dial(target)
		if err == nil {
			return conn, nil
		}
		log.Printf("upstream %s dial %s failed: %v", up.URL.Host, target, err)
		m.mu.Lock()
		up.dead = true
		m.mu.Unlock()
	}
	return DialTarget(target)
}

// dialHTTP 通过 HTTP/HTTPS 代理的 CONNECT 建立隧道
func (u *Upstream) dialHTTP(target string) (net.Conn, error) {
	var conn net.Conn
	var err error
	if u.URL.Scheme == "https" {
		conn, err = tls.Dial("tcp", u.URL.Host, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.Dial("tcp", u.URL.Host)
	}
	if err != nil {
		return nil, err
	}

	creds := ""
	if user := u.URL.User; user != nil {
		username := user.Username()
		password, _ := user.Password()
		creds = base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	}

	var authLine string
	if creds != "" {
		authLine = "Proxy-Authorization: Basic " + creds + "\r\n"
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, authLine)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		conn.Close()
		return nil, fmt.Errorf("upstream CONNECT failed: %s", resp.Status)
	}
	// 消费并关闭可能存在的响应体，确保缓冲区后续读取的是隧道数据
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return &bufferedConn{Conn: conn, r: br}, nil
}

// dialSOCKS5 通过 SOCKS5 上游连接到目标
func (u *Upstream) dialSOCKS5(target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("tcp", u.URL.Host, 5*time.Second)
	if err != nil {
		return nil, err
	}

	username, password := "", ""
	if user := u.URL.User; user != nil {
		username = user.Username()
		password, _ = user.Password()
	}

	authMethod := byte(0x00)
	if username != "" {
		authMethod = 0x02
	}

	// greeting
	if _, err := conn.Write([]byte{0x05, 0x01, authMethod}); err != nil {
		conn.Close()
		return nil, err
	}

	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		conn.Close()
		return nil, err
	}
	if buf[0] != 0x05 {
		conn.Close()
		return nil, errors.New("invalid socks5 version")
	}
	if buf[1] == 0x02 {
		if err := socks5Auth(conn, username, password); err != nil {
			conn.Close()
			return nil, err
		}
	} else if buf[1] != 0x00 {
		conn.Close()
		return nil, errors.New("unsupported socks5 auth method")
	}

	// request
	ip := net.ParseIP(host)
	var req []byte
	if ip4 := ip.To4(); ip4 != nil {
		req = []byte{0x05, 0x01, 0x00, 0x01}
		req = append(req, ip4...)
	} else if ip6 := ip.To16(); ip6 != nil {
		req = []byte{0x05, 0x01, 0x00, 0x04}
		req = append(req, ip6...)
	} else {
		req = []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
		req = append(req, []byte(host)...)
	}
	req = append(req, byte(portNum>>8), byte(portNum))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	// response: VER REP RSV ATYP BND.ADDR BND.PORT
	resp := make([]byte, 4)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[0] != 0x05 {
		conn.Close()
		return nil, errors.New("invalid socks5 response version")
	}
	if resp[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 request failed: %d", resp[1])
	}
	if resp[2] != 0x00 {
		conn.Close()
		return nil, errors.New("invalid socks5 reserved byte")
	}
	// skip bound address
	switch resp[3] {
	case 0x01: // IPv4
		_, err = io.ReadFull(conn, make([]byte, 4+2))
	case 0x03: // domain
		l := make([]byte, 1)
		_, err = io.ReadFull(conn, l)
		if err == nil {
			_, err = io.ReadFull(conn, make([]byte, int(l[0])+2))
		}
	case 0x04: // IPv6
		_, err = io.ReadFull(conn, make([]byte, 16+2))
	default:
		conn.Close()
		return nil, errors.New("unsupported socks5 address type")
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func socks5Auth(conn net.Conn, username, password string) error {
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

// DialTarget 直接连接到目标
func DialTarget(target string) (net.Conn, error) {
	return net.DialTimeout("tcp", target, 10*time.Second)
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
		io.Copy(targetConn, clientReader)
		if c, ok := targetConn.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, targetConn)
		if c, ok := clientConn.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	wg.Wait()
}
