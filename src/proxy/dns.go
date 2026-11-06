package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DNS 配置类型常量。
const (
	dnsTypeSystem = "system"
	dnsTypeDoH    = "doh"
	dnsTypeDoT    = "dot"
)

// IP 优先级常量。
const (
	IPPrecedenceIPv4First = "ipv4_first"
	IPPrecedenceIPv6First = "ipv6_first"
	IPPrecedenceSystem    = "system"
)

// DNS 资源记录类型。
const (
	dnsRecordA    uint16 = 1
	dnsRecordAAAA uint16 = 28
)

// DNSConfig 配置 DNS 解析策略。
// type 为 system 时使用系统默认解析器（向后兼容）；
// type 为 doh 时通过 DNS-over-HTTPS 解析；
// type 为 dot 时通过 DNS-over-TLS 解析。
// ip_precedence 控制 DNS 解析后 IPv4/IPv6 的优先级：
//
//	"ipv4_first"（默认）：IPv4 优先，IPv6 兜底
//	"ipv6_first"：IPv6 优先，IPv4 兜底
//	"system"：保持 DNS 返回顺序
type DNSConfig struct {
	Type         string   `yaml:"type"`
	DoHURL       string   `yaml:"doh_url"`
	DoTAddr      string   `yaml:"dot_addr"`
	Fallback     bool     `yaml:"fallback"`
	Timeout      Duration `yaml:"timeout"`
	IPPrecedence string   `yaml:"ip_precedence"`
}

// dnsCacheEntry 缓存条目。
type dnsCacheEntry struct {
	addrs     []net.IPAddr
	expiresAt time.Time
}

// dnsCacheMaxEntries 是 DNS 缓存的最大条目数。
const dnsCacheMaxEntries = 10000

// dnsCacheCleanupInterval 是后台清理过期条目的间隔。
const dnsCacheCleanupInterval = 5 * time.Minute

// DNSResolver 根据配置使用系统 DNS、DoH 或 DoT 解析域名。
type DNSResolver struct {
	typ      string
	dohURL   string
	dotAddr  string
	fallback bool
	timeout  time.Duration

	httpClient *http.Client
	tlsConfig  *tls.Config

	cache    map[string]*dnsCacheEntry
	cacheMu  sync.RWMutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// defaultDNSTimeout 是 DNS 查询的默认超时时间。
const defaultDNSTimeout = 5 * time.Second

// dnsCacheTTL 是 DNS 缓存的默认 TTL。
const dnsCacheTTL = 60 * time.Second

// DefaultDNSTimeout 返回 DNS 查询的默认超时时间。
func DefaultDNSTimeout() Duration {
	return Duration(defaultDNSTimeout)
}

// NewDNSResolver 根据配置创建 DNS 解析器。
func NewDNSResolver(cfg DNSConfig) *DNSResolver {
	typ := strings.ToLower(strings.TrimSpace(cfg.Type))
	if typ == "" {
		typ = dnsTypeSystem
	}
	timeout := cfg.Timeout.value(defaultDNSTimeout)

	r := &DNSResolver{
		typ:      typ,
		dohURL:   strings.TrimSpace(cfg.DoHURL),
		dotAddr:  strings.TrimSpace(cfg.DoTAddr),
		fallback: cfg.Fallback,
		timeout:  timeout,
		cache:    make(map[string]*dnsCacheEntry),
		stopCh:   make(chan struct{}),
	}

	if typ == dnsTypeDoH || cfg.Fallback {
		r.httpClient = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		}
	}

	if typ == dnsTypeDoT {
		r.tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	if typ != dnsTypeSystem {
		r.startCacheCleanup()
	}

	return r
}

// startCacheCleanup 启动后台 goroutine 定期清理过期 DNS 缓存条目。
func (r *DNSResolver) startCacheCleanup() {
	r.stopOnce.Do(func() {
		go r.cacheCleanupLoop()
	})
}

// Stop 停止 DNS 解析器的后台清理 goroutine。
func (r *DNSResolver) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
}

func (r *DNSResolver) cacheCleanupLoop() {
	ticker := time.NewTicker(dnsCacheCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.evictExpired()
		case <-r.stopCh:
			return
		}
	}
}

// evictExpired 删除所有过期的缓存条目。
func (r *DNSResolver) evictExpired() {
	now := time.Now()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	for host, entry := range r.cache {
		if now.After(entry.expiresAt) {
			delete(r.cache, host)
		}
	}
}

// LookupIPAddr 解析域名返回 IP 地址列表。
// 对于 DoH/DoT 模式，失败且 fallback 启用时会回退到系统 DNS。
func (r *DNSResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r == nil || r.typ == dnsTypeSystem {
		return net.DefaultResolver.LookupIPAddr(ctx, host)
	}

	// 检查缓存
	if addrs, ok := r.cacheGet(host); ok {
		return addrs, nil
	}

	// 设置查询超时
	queryCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	addrs, err := r.resolveSecure(queryCtx, host)
	if err != nil && r.fallback {
		Logger().Warnw("secure DNS failed, falling back to system DNS", "host", host, "type", r.typ, "error", err)
		addrs, err = net.DefaultResolver.LookupIPAddr(ctx, host)
	}

	if err == nil && len(addrs) > 0 {
		r.cacheSet(host, addrs)
	}

	return addrs, err
}

// resolveSecure 通过 DoH 或 DoT 解析域名。
func (r *DNSResolver) resolveSecure(ctx context.Context, host string) ([]net.IPAddr, error) {
	switch r.typ {
	case dnsTypeDoH:
		return r.resolveDoH(ctx, host)
	case dnsTypeDoT:
		return r.resolveDoT(ctx, host)
	default:
		return nil, fmt.Errorf("unsupported DNS type %q", r.typ)
	}
}

// resolveDoH 通过 DNS-over-HTTPS 解析域名。
func (r *DNSResolver) resolveDoH(ctx context.Context, host string) ([]net.IPAddr, error) {
	// 并行查询 A 和 AAAA 记录
	type result struct {
		addrs []net.IPAddr
		err   error
	}
	aCh := make(chan result, 1)
	aaaaCh := make(chan result, 1)

	go func() {
		addrs, err := r.dohQuery(ctx, host, dnsRecordA)
		aCh <- result{addrs, err}
	}()
	go func() {
		addrs, err := r.dohQuery(ctx, host, dnsRecordAAAA)
		aaaaCh <- result{addrs, err}
	}()

	var addrs []net.IPAddr
	var firstErr error

	aRes := <-aCh
	if aRes.err == nil {
		addrs = append(addrs, aRes.addrs...)
	} else {
		firstErr = aRes.err
	}

	aaaaRes := <-aaaaCh
	if aaaaRes.err == nil {
		addrs = append(addrs, aaaaRes.addrs...)
	} else if firstErr == nil {
		firstErr = aaaaRes.err
	}

	if len(addrs) > 0 {
		return addrs, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("no addresses for %s", host)
}

// dohQuery 发送单个 DoH 查询。
func (r *DNSResolver) dohQuery(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	query := buildDNSQuery(host, qtype)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.dohURL, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}

	return parseDNSResponse(body)
}

// resolveDoT 通过 DNS-over-TLS 解析域名。
func (r *DNSResolver) resolveDoT(ctx context.Context, host string) ([]net.IPAddr, error) {
	// 并行查询 A 和 AAAA 记录
	type result struct {
		addrs []net.IPAddr
		err   error
	}
	aCh := make(chan result, 1)
	aaaaCh := make(chan result, 1)

	go func() {
		addrs, err := r.dotQuery(ctx, host, dnsRecordA)
		aCh <- result{addrs, err}
	}()
	go func() {
		addrs, err := r.dotQuery(ctx, host, dnsRecordAAAA)
		aaaaCh <- result{addrs, err}
	}()

	var addrs []net.IPAddr
	var firstErr error

	aRes := <-aCh
	if aRes.err == nil {
		addrs = append(addrs, aRes.addrs...)
	} else {
		firstErr = aRes.err
	}

	aaaaRes := <-aaaaCh
	if aaaaRes.err == nil {
		addrs = append(addrs, aaaaRes.addrs...)
	} else if firstErr == nil {
		firstErr = aaaaRes.err
	}

	if len(addrs) > 0 {
		return addrs, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("no addresses for %s", host)
}

// dotQuery 发送单个 DoT 查询。
func (r *DNSResolver) dotQuery(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	d := net.Dialer{Timeout: r.timeout}
	rawConn, err := d.DialContext(ctx, "tcp", r.dotAddr)
	if err != nil {
		return nil, err
	}

	conn := tls.Client(rawConn, r.tlsConfig.Clone())
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(r.timeout)); err != nil {
		return nil, err
	}

	// TLS 握手
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, err
	}

	query := buildDNSQuery(host, qtype)

	// DoT 使用 2 字节长度前缀
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, uint16(len(query)))
	if _, err := conn.Write(lenBuf); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	// 读取响应长度
	respLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, respLenBuf); err != nil {
		return nil, err
	}
	respLen := binary.BigEndian.Uint16(respLenBuf)
	if respLen == 0 || respLen > 65535 {
		return nil, fmt.Errorf("invalid DoT response length %d", respLen)
	}

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, err
	}

	return parseDNSResponse(respBuf)
}

// cacheGet 从缓存中获取 DNS 结果。
func (r *DNSResolver) cacheGet(host string) ([]net.IPAddr, bool) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	entry, ok := r.cache[host]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.addrs, true
}

// cacheSet 将 DNS 结果存入缓存。当缓存超过上限时淘汰过期条目。
func (r *DNSResolver) cacheSet(host string, addrs []net.IPAddr) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if len(r.cache) >= dnsCacheMaxEntries {
		// 淘汰过期条目
		now := time.Now()
		for h, entry := range r.cache {
			if now.After(entry.expiresAt) {
				delete(r.cache, h)
			}
		}
		// 如果仍然超限，不再添加新条目以防止无限增长
		if len(r.cache) >= dnsCacheMaxEntries {
			return
		}
	}
	r.cache[host] = &dnsCacheEntry{
		addrs:     addrs,
		expiresAt: time.Now().Add(dnsCacheTTL),
	}
}

// buildDNSQuery 构造 DNS 查询报文（wire format）。
func buildDNSQuery(domain string, qtype uint16) []byte {
	var buf bytes.Buffer

	// Header (12 bytes)
	id := uint16(time.Now().UnixNano() & 0xFFFF)
	binary.Write(&buf, binary.BigEndian, id)             // ID
	binary.Write(&buf, binary.BigEndian, uint16(0x0100)) // Flags: RD=1
	binary.Write(&buf, binary.BigEndian, uint16(1))      // QDCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // ANCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // NSCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // ARCOUNT

	// Question: QNAME
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		domain = "."
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 {
			label = label[:63]
		}
		buf.WriteByte(byte(len(label)))
		buf.WriteString(label)
	}
	buf.WriteByte(0) // root label

	// Question: QTYPE + QCLASS
	binary.Write(&buf, binary.BigEndian, qtype)     // QTYPE
	binary.Write(&buf, binary.BigEndian, uint16(1)) // QCLASS = IN

	return buf.Bytes()
}

// parseDNSResponse 解析 DNS 响应报文，返回 IP 地址列表。
func parseDNSResponse(msg []byte) ([]net.IPAddr, error) {
	if len(msg) < 12 {
		return nil, fmt.Errorf("DNS response too short")
	}

	flags := binary.BigEndian.Uint16(msg[2:4])
	rcode := flags & 0x0F
	if rcode != 0 {
		return nil, fmt.Errorf("DNS response rcode %d", rcode)
	}

	qdcount := binary.BigEndian.Uint16(msg[4:6])
	ancount := binary.BigEndian.Uint16(msg[6:8])

	pos := 12

	// 跳过 Question 段
	for i := 0; i < int(qdcount); i++ {
		_, newPos, err := skipDNSName(msg, pos)
		if err != nil {
			return nil, err
		}
		pos = newPos + 4 // QTYPE + QCLASS
	}

	// 解析 Answer 段
	var addrs []net.IPAddr
	for i := 0; i < int(ancount); i++ {
		_, newPos, err := skipDNSName(msg, pos)
		if err != nil {
			return nil, err
		}
		pos = newPos
		if pos+10 > len(msg) {
			return nil, fmt.Errorf("DNS answer header truncated")
		}
		rtype := binary.BigEndian.Uint16(msg[pos : pos+2])
		rdlength := binary.BigEndian.Uint16(msg[pos+8 : pos+10])
		pos += 10
		if pos+int(rdlength) > len(msg) {
			return nil, fmt.Errorf("DNS rdata truncated")
		}
		switch rtype {
		case dnsRecordA:
			if rdlength == 4 {
				ip := net.IPv4(msg[pos], msg[pos+1], msg[pos+2], msg[pos+3])
				addrs = append(addrs, net.IPAddr{IP: ip})
			}
		case dnsRecordAAAA:
			if rdlength == 16 {
				ip := make(net.IP, 16)
				copy(ip, msg[pos:pos+16])
				addrs = append(addrs, net.IPAddr{IP: ip})
			}
		}
		pos += int(rdlength)
	}

	return addrs, nil
}

// skipDNSName 跳过 DNS 名称（处理压缩指针），返回名称和下一个位置。
func skipDNSName(msg []byte, pos int) (string, int, error) {
	var name strings.Builder
	jumped := false
	nextPos := pos

	for {
		if pos >= len(msg) {
			return "", 0, fmt.Errorf("DNS name truncated")
		}
		b := msg[pos]
		if b == 0 {
			pos++
			if !jumped {
				nextPos = pos
			}
			break
		}
		if b&0xC0 == 0xC0 {
			// 压缩指针
			if pos+1 >= len(msg) {
				return "", 0, fmt.Errorf("DNS pointer truncated")
			}
			if !jumped {
				nextPos = pos + 2
				jumped = true
			}
			pos = int(b&0x3F)<<8 | int(msg[pos+1])
			continue
		}
		// 标签
		pos++
		if pos+int(b) > len(msg) {
			return "", 0, fmt.Errorf("DNS label truncated")
		}
		if name.Len() > 0 {
			name.WriteByte('.')
		}
		name.Write(msg[pos : pos+int(b)])
		pos += int(b)
	}

	return name.String(), nextPos, nil
}

// ValidateDNSConfig 校验 DNS 配置。
func ValidateDNSConfig(cfg DNSConfig) error {
	// 校验 ip_precedence（与 type 无关，始终检查）
	prec := strings.ToLower(strings.TrimSpace(cfg.IPPrecedence))
	if prec != "" && prec != IPPrecedenceIPv4First && prec != IPPrecedenceIPv6First && prec != IPPrecedenceSystem {
		return fmt.Errorf("dns.ip_precedence must be ipv4_first, ipv6_first, or system, got %q", cfg.IPPrecedence)
	}

	typ := strings.ToLower(strings.TrimSpace(cfg.Type))
	if typ == "" {
		return nil // 默认 system
	}
	switch typ {
	case dnsTypeSystem:
		return nil
	case dnsTypeDoH:
		if strings.TrimSpace(cfg.DoHURL) == "" {
			return fmt.Errorf("dns.doh_url is required when dns.type is doh")
		}
		if !strings.HasPrefix(cfg.DoHURL, "https://") {
			return fmt.Errorf("dns.doh_url must be an HTTPS URL")
		}
	case dnsTypeDoT:
		if strings.TrimSpace(cfg.DoTAddr) == "" {
			return fmt.Errorf("dns.dot_addr is required when dns.type is dot")
		}
	default:
		return fmt.Errorf("dns.type must be system, doh, or dot, got %q", typ)
	}
	return nil
}
