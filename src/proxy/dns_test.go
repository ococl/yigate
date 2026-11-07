package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildDNSQuery(t *testing.T) {
	query := buildDNSQuery("example.com", dnsRecordA)
	if len(query) < 17 { // 12 header + 5 "example" + 4 "com" + 1 root + 4 type/class
		t.Fatalf("query too short: %d bytes", len(query))
	}
	// Header: ID(2) + Flags(2) + QDCOUNT(2) + ANCOUNT(2) + NSCOUNT(2) + ARCOUNT(2) = 12
	if len(query) < 12 {
		t.Fatal("header missing")
	}
	qdcount := binary.BigEndian.Uint16(query[4:6])
	if qdcount != 1 {
		t.Fatalf("QDCOUNT = %d, want 1", qdcount)
	}
	// Check QTYPE (last 4 bytes before end: QTYPE(2) + QCLASS(2))
	qtype := binary.BigEndian.Uint16(query[len(query)-4 : len(query)-2])
	if qtype != dnsRecordA {
		t.Fatalf("QTYPE = %d, want %d", qtype, dnsRecordA)
	}
	qclass := binary.BigEndian.Uint16(query[len(query)-2:])
	if qclass != 1 {
		t.Fatalf("QCLASS = %d, want 1 (IN)", qclass)
	}
}

func TestBuildDNSQueryTrailingDot(t *testing.T) {
	q1 := buildDNSQuery("example.com", dnsRecordA)
	q2 := buildDNSQuery("example.com.", dnsRecordA)
	if len(q1) != len(q2) {
		t.Fatalf("trailing dot should not change query length: %d vs %d", len(q1), len(q2))
	}
}

func TestParseDNSResponseA(t *testing.T) {
	// 构造一个包含 A 记录的 DNS 响应
	query := buildDNSQuery("example.com", dnsRecordA)
	response := buildDNSResponse(query, dnsRecordA, net.IPv4(93, 184, 216, 34))

	addrs, err := parseDNSResponse(response)
	if err != nil {
		t.Fatalf("parseDNSResponse error: %v", err)
	}
	if len(addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(addrs))
	}
	expected := net.IPv4(93, 184, 216, 34)
	if !addrs[0].IP.Equal(expected) {
		t.Fatalf("address = %s, want %s", addrs[0].IP, expected)
	}
}

func TestParseDNSResponseAAAA(t *testing.T) {
	query := buildDNSQuery("example.com", dnsRecordAAAA)
	ipv6 := net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")
	response := buildDNSResponse(query, dnsRecordAAAA, ipv6)

	addrs, err := parseDNSResponse(response)
	if err != nil {
		t.Fatalf("parseDNSResponse error: %v", err)
	}
	if len(addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(addrs))
	}
	if !addrs[0].IP.Equal(ipv6) {
		t.Fatalf("address = %s, want %s", addrs[0].IP, ipv6)
	}
}

func TestParseDNSResponseMultiple(t *testing.T) {
	query := buildDNSQuery("example.com", dnsRecordA)
	ips := []net.IP{
		net.IPv4(1, 2, 3, 4),
		net.IPv4(5, 6, 7, 8),
		net.IPv4(9, 10, 11, 12),
	}
	response := buildDNSResponseMulti(query, dnsRecordA, ips)

	addrs, err := parseDNSResponse(response)
	if err != nil {
		t.Fatalf("parseDNSResponse error: %v", err)
	}
	if len(addrs) != 3 {
		t.Fatalf("expected 3 addresses, got %d", len(addrs))
	}
	for i, expected := range ips {
		if !addrs[i].IP.Equal(expected) {
			t.Fatalf("address[%d] = %s, want %s", i, addrs[i].IP, expected)
		}
	}
}

func TestParseDNSResponseRcodeError(t *testing.T) {
	query := buildDNSQuery("nonexistent.example", dnsRecordA)
	// 构造 rcode=3 (NXDOMAIN) 的响应
	response := make([]byte, 12)
	copy(response, query[:12])
	// Set QR=1, RD=1, RA=1, RCODE=3
	flags := uint16(0x8183)
	binary.BigEndian.PutUint16(response[2:4], flags)

	_, err := parseDNSResponse(response)
	if err == nil {
		t.Fatal("expected error for NXDOMAIN")
	}
	if !strings.Contains(err.Error(), "rcode") {
		t.Fatalf("error should mention rcode, got: %v", err)
	}
}

func TestParseDNSResponseTooShort(t *testing.T) {
	_, err := parseDNSResponse([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short response")
	}
}

func TestSkipDNSName(t *testing.T) {
	msg := buildDNSQuery("www.example.com", dnsRecordA)
	// Skip header (12 bytes), then parse the name
	name, pos, err := skipDNSName(msg, 12)
	if err != nil {
		t.Fatalf("skipDNSName error: %v", err)
	}
	if name != "www.example.com" {
		t.Fatalf("name = %q, want %q", name, "www.example.com")
	}
	// After name, should be QTYPE(2) + QCLASS(2) = 4 bytes
	if pos+4 != len(msg) {
		t.Fatalf("pos = %d, expected %d", pos, len(msg)-4)
	}
}

func TestSkipDNSNameCompressionPointer(t *testing.T) {
	// 构造一个使用压缩指针的响应
	query := buildDNSQuery("example.com", dnsRecordA)
	response := buildDNSResponse(query, dnsRecordA, net.IPv4(1, 2, 3, 4))

	// Answer section starts after header(12) + question
	// Question: name + QTYPE(2) + QCLASS(2)
	_, qEnd, err := skipDNSName(response, 12)
	if err != nil {
		t.Fatalf("skip question name error: %v", err)
	}
	answerStart := qEnd + 4

	// The answer name should be a compression pointer (0xC0 0x0C)
	name, pos, err := skipDNSName(response, answerStart)
	if err != nil {
		t.Fatalf("skip answer name error: %v", err)
	}
	if name != "example.com" {
		t.Fatalf("compressed name = %q, want %q", name, "example.com")
	}
	// After pointer, should be TYPE(2) + CLASS(2) + TTL(4) + RDLENGTH(2) = 10 bytes
	if pos+10 > len(response) {
		t.Fatalf("pos %d too close to end %d", pos, len(response))
	}
}

func TestDNSResolverSystemType(t *testing.T) {
	r := NewDNSResolver(DNSConfig{Type: "system"})
	if r.typ != dnsTypeSystem {
		t.Fatalf("typ = %q, want %q", r.typ, dnsTypeSystem)
	}
	// System type should delegate to net.DefaultResolver
	// Just verify it doesn't panic with a nil resolver behavior
	if r.httpClient != nil {
		t.Fatal("system resolver should not create HTTP client")
	}
}

func TestDNSResolverDefaultType(t *testing.T) {
	r := NewDNSResolver(DNSConfig{})
	if r.typ != dnsTypeSystem {
		t.Fatalf("default typ = %q, want %q", r.typ, dnsTypeSystem)
	}
}

func TestDNSResolverDoHType(t *testing.T) {
	r := NewDNSResolver(DNSConfig{
		Type:   "doh",
		DoHURL: "https://dns.google/dns-query",
	})
	if r.typ != dnsTypeDoH {
		t.Fatalf("typ = %q, want %q", r.typ, dnsTypeDoH)
	}
	if r.httpClient == nil {
		t.Fatal("DoH resolver should create HTTP client")
	}
}

func TestDNSResolverDoTType(t *testing.T) {
	r := NewDNSResolver(DNSConfig{
		Type:    "dot",
		DoTAddr: "dns.google:853",
	})
	if r.typ != dnsTypeDoT {
		t.Fatalf("typ = %q, want %q", r.typ, dnsTypeDoT)
	}
	if r.tlsConfig == nil {
		t.Fatal("DoT resolver should create TLS config")
	}
}

func TestDNSResolverCache(t *testing.T) {
	r := NewDNSResolver(DNSConfig{Type: "system"})
	host := "cache-test.example.com"
	addrs := []net.IPAddr{{IP: net.IPv4(1, 2, 3, 4)}}
	r.cacheSet(host, addrs)

	got, ok := r.cacheGet(host)
	if !ok {
		t.Fatal("cache miss after set")
	}
	if len(got) != 1 || !got[0].IP.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Fatalf("cached addrs = %v, want [1.2.3.4]", got)
	}
}

func TestDNSResolverCacheExpiry(t *testing.T) {
	r := NewDNSResolver(DNSConfig{Type: "system"})
	host := "expiry-test.example.com"
	addrs := []net.IPAddr{{IP: net.IPv4(1, 2, 3, 4)}}
	r.cacheSet(host, addrs)

	// Manually expire
	r.cacheMu.Lock()
	r.cache[host].expiresAt = time.Now().Add(-time.Second)
	r.cacheMu.Unlock()

	_, ok := r.cacheGet(host)
	if ok {
		t.Fatal("cache should be expired")
	}
}

func TestDNSResolverCacheMaxEntries(t *testing.T) {
	r := NewDNSResolver(DNSConfig{Type: "system"})
	// 填满缓存到上限
	for i := 0; i < dnsCacheMaxEntries; i++ {
		host := fmt.Sprintf("host%d.example.com", i)
		r.cacheSet(host, []net.IPAddr{{IP: net.IPv4(1, 2, 3, byte(i%256))}})
	}
	if len(r.cache) != dnsCacheMaxEntries {
		t.Fatalf("expected %d entries, got %d", dnsCacheMaxEntries, len(r.cache))
	}

	// 再添加一个新条目，由于所有条目都未过期，应被拒绝
	r.cacheSet("overflow.example.com", []net.IPAddr{{IP: net.IPv4(9, 9, 9, 9)}})
	if len(r.cache) > dnsCacheMaxEntries {
		t.Fatalf("cache should not exceed max entries, got %d", len(r.cache))
	}
	if _, ok := r.cacheGet("overflow.example.com"); ok {
		t.Fatal("overflow entry should not have been added")
	}
}

func TestDNSResolverCacheEvictExpired(t *testing.T) {
	r := NewDNSResolver(DNSConfig{Type: "system"})
	// 添加一些过期和未过期的条目
	r.cacheSet("expired1.example.com", []net.IPAddr{{IP: net.IPv4(1, 1, 1, 1)}})
	r.cacheSet("expired2.example.com", []net.IPAddr{{IP: net.IPv4(2, 2, 2, 2)}})
	r.cacheSet("alive.example.com", []net.IPAddr{{IP: net.IPv4(3, 3, 3, 3)}})

	// 手动使前两个过期
	r.cacheMu.Lock()
	r.cache["expired1.example.com"].expiresAt = time.Now().Add(-time.Second)
	r.cache["expired2.example.com"].expiresAt = time.Now().Add(-time.Second)
	r.cacheMu.Unlock()

	// evictExpired 应删除过期条目
	r.evictExpired()

	r.cacheMu.RLock()
	count := len(r.cache)
	_, hasAlive := r.cache["alive.example.com"]
	r.cacheMu.RUnlock()

	if count != 1 {
		t.Fatalf("expected 1 entry after eviction, got %d", count)
	}
	if !hasAlive {
		t.Fatal("alive entry should still exist")
	}
}

func TestDNSResolverStop(t *testing.T) {
	r := NewDNSResolver(DNSConfig{
		Type:    "doh",
		DoHURL:  "https://dns.google/dns-query",
		Timeout: Duration(5 * time.Second),
	})
	// Stop should not panic and should be idempotent
	r.Stop()
	r.Stop() // idempotent
}

func TestDNSResolverSystemTypeNoCleanup(t *testing.T) {
	// System type should not start cache cleanup goroutine
	r := NewDNSResolver(DNSConfig{Type: "system"})
	// stopCh should still be created (for safe Stop call)
	r.Stop()
}

func TestValidateDNSConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DNSConfig
		wantErr bool
	}{
		{"empty defaults to system", DNSConfig{}, false},
		{"system", DNSConfig{Type: "system"}, false},
		{"doh valid", DNSConfig{Type: "doh", DoHURL: "https://dns.google/dns-query"}, false},
		{"doh missing url", DNSConfig{Type: "doh"}, true},
		{"doh non-https", DNSConfig{Type: "doh", DoHURL: "http://dns.google/dns-query"}, true},
		{"dot valid", DNSConfig{Type: "dot", DoTAddr: "dns.google:853"}, false},
		{"dot missing addr", DNSConfig{Type: "dot"}, true},
		{"invalid type", DNSConfig{Type: "invalid"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDNSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDNSResolverDoHWithMockServer(t *testing.T) {
	// 启动一个模拟 DoH 服务器
	var queryCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/dns-query", func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/dns-message" {
			t.Errorf("Content-Type = %q, want application/dns-message", r.Header.Get("Content-Type"))
		}
		body := make([]byte, 512)
		n, _ := r.Body.Read(body)
		query := body[:n]

		// 解析查询域名
		_, _, err := skipDNSName(query, 12)
		if err != nil {
			t.Errorf("skipDNSName error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 返回 A 记录 1.2.3.4
		response := buildDNSResponse(query, dnsRecordA, net.IPv4(1, 2, 3, 4))
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(response)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()

	dohURL := fmt.Sprintf("http://%s/dns-query", ln.Addr().String())

	r := NewDNSResolver(DNSConfig{
		Type:    "doh",
		DoHURL:  dohURL,
		Timeout: Duration(5 * time.Second),
	})

	ctx := context.Background()
	addrs, err := r.LookupIPAddr(ctx, "test.example.com")
	if err != nil {
		t.Fatalf("LookupIPAddr error: %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("no addresses returned")
	}
	found := false
	for _, addr := range addrs {
		if addr.IP.Equal(net.IPv4(1, 2, 3, 4)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("1.2.3.4 not in results: %v", addrs)
	}

	// 第二次查询应命中缓存
	countBefore := queryCount.Load()
	_, _ = r.LookupIPAddr(ctx, "test.example.com")
	countAfter := queryCount.Load()
	if countAfter != countBefore {
		t.Fatalf("cache miss: query count went from %d to %d", countBefore, countAfter)
	}
}

func TestDNSResolverFallback(t *testing.T) {
	// DoH 服务器不可达，fallback 到系统 DNS
	r := NewDNSResolver(DNSConfig{
		Type:     "doh",
		DoHURL:   "https://127.0.0.1:1/dns-query", // 不可达地址
		Fallback: true,
		Timeout:  Duration(1 * time.Second),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用一个应该能解析的域名
	addrs, err := r.LookupIPAddr(ctx, "localhost")
	if err != nil {
		// 在某些环境下 localhost 可能不解析，跳过
		t.Skipf("localhost resolution failed (environment): %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("fallback returned no addresses")
	}
}

func TestDNSResolverNoFallback(t *testing.T) {
	r := NewDNSResolver(DNSConfig{
		Type:     "doh",
		DoHURL:   "https://127.0.0.1:1/dns-query", // 不可达地址
		Fallback: false,
		Timeout:  Duration(1 * time.Second),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.LookupIPAddr(ctx, "test.example.com")
	if err == nil {
		t.Fatal("expected error when DoH fails and fallback is disabled")
	}
}

// buildDNSResponse 构造一个包含单个记录的 DNS 响应。
func buildDNSResponse(query []byte, rtype uint16, ip net.IP) []byte {
	return buildDNSResponseMulti(query, rtype, []net.IP{ip})
}

// buildDNSResponseMulti 构造包含多个记录的 DNS 响应。
func buildDNSResponseMulti(query []byte, rtype uint16, ips []net.IP) []byte {
	var buf []byte
	// Header: copy ID from query, set QR=1, RD=1, RA=1, RCODE=0
	header := make([]byte, 12)
	copy(header[:2], query[:2])                               // ID
	binary.BigEndian.PutUint16(header[2:4], 0x8180)           // Flags: QR=1, RD=1, RA=1, RCODE=0
	binary.BigEndian.PutUint16(header[4:6], 1)                // QDCOUNT
	binary.BigEndian.Uint16(header[6:8])                      // ANCOUNT (set below)
	binary.BigEndian.PutUint16(header[8:10], 0)               // NSCOUNT
	binary.BigEndian.PutUint16(header[10:12], 0)              // ARCOUNT
	binary.BigEndian.PutUint16(header[6:8], uint16(len(ips))) // ANCOUNT
	buf = append(buf, header...)

	// Question section: copy from query (name + QTYPE + QCLASS)
	_, qEnd, err := skipDNSName(query, 12)
	if err != nil {
		panic(err)
	}
	questionEnd := qEnd + 4 // QTYPE + QCLASS
	buf = append(buf, query[12:questionEnd]...)

	// Answer section
	for _, ip := range ips {
		// Name: compression pointer to offset 12
		buf = append(buf, 0xC0, 0x0C)
		// TYPE
		typeBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(typeBuf, rtype)
		buf = append(buf, typeBuf...)
		// CLASS = IN
		classBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(classBuf, 1)
		buf = append(buf, classBuf...)
		// TTL = 300
		ttlBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(ttlBuf, 300)
		buf = append(buf, ttlBuf...)
		// RDLENGTH
		var rdlength uint16
		var rdata []byte
		switch rtype {
		case dnsRecordA:
			rdlength = 4
			rdata = ip.To4()
		case dnsRecordAAAA:
			rdlength = 16
			rdata = ip.To16()
		}
		rdlenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(rdlenBuf, rdlength)
		buf = append(buf, rdlenBuf...)
		buf = append(buf, rdata...)
	}

	return buf
}
