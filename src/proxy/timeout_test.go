package proxy

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTimeoutGetters(t *testing.T) {
	t.Cleanup(resetTimeouts)
	SetTimeouts(TimeoutConfig{
		Dial:         Duration(2 * time.Second),
		Handshake:    Duration(3 * time.Second),
		HTTPExchange: Duration(4 * time.Second),
		TunnelIdle:   Duration(5 * time.Second),
	})
	if ProxyDialTimeout() != 2*time.Second {
		t.Fatalf("dial timeout = %v", ProxyDialTimeout())
	}
	if ProxyHandshakeTimeout() != 3*time.Second {
		t.Fatalf("handshake timeout = %v", ProxyHandshakeTimeout())
	}
	if HTTPExchangeTimeout() != 4*time.Second {
		t.Fatalf("http timeout = %v", HTTPExchangeTimeout())
	}
	if TunnelIdleTimeout() != 5*time.Second {
		t.Fatalf("tunnel timeout = %v", TunnelIdleTimeout())
	}
}

func TestSetTimeoutsResetsZeroToDefault(t *testing.T) {
	t.Cleanup(resetTimeouts)
	SetTimeouts(TimeoutConfig{
		Dial:         Duration(30 * time.Second),
		Handshake:    Duration(30 * time.Second),
		HTTPExchange: Duration(30 * time.Second),
		TunnelIdle:   Duration(30 * time.Minute),
	})
	// 用户把配置改回 0（表示使用内置默认值），应恢复到默认而不是沿用旧值。
	SetTimeouts(TimeoutConfig{})
	if ProxyDialTimeout() != defaultProxyDialTimeout {
		t.Fatalf("dial timeout = %v, want %v", ProxyDialTimeout(), defaultProxyDialTimeout)
	}
	if ProxyHandshakeTimeout() != defaultProxyHandshakeTimeout {
		t.Fatalf("handshake timeout = %v, want %v", ProxyHandshakeTimeout(), defaultProxyHandshakeTimeout)
	}
	if HTTPExchangeTimeout() != defaultHTTPExchangeTimeout {
		t.Fatalf("http timeout = %v, want %v", HTTPExchangeTimeout(), defaultHTTPExchangeTimeout)
	}
	if TunnelIdleTimeout() != defaultTunnelIdleTimeout {
		t.Fatalf("tunnel timeout = %v, want %v", TunnelIdleTimeout(), defaultTunnelIdleTimeout)
	}
}

func TestSetTimeoutsPartialKeepsDefaults(t *testing.T) {
	t.Cleanup(resetTimeouts)
	SetTimeouts(TimeoutConfig{Dial: Duration(3 * time.Second)})
	if ProxyDialTimeout() != 3*time.Second {
		t.Fatalf("dial timeout = %v", ProxyDialTimeout())
	}
	if HTTPExchangeTimeout() != defaultHTTPExchangeTimeout {
		t.Fatalf("http timeout = %v, want default %v", HTTPExchangeTimeout(), defaultHTTPExchangeTimeout)
	}
}

func TestSetConnDeadline(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	if err := setConnDeadline(c1, 0); err != nil {
		t.Fatal(err)
	}
	if err := setConnDeadline(c1, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineReaderWriter(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	dr := &deadlineReader{r: c1, conn: c1, timeout: time.Hour, metricDirection: "test"}
	dw := &deadlineWriter{w: c2, conn: c2, timeout: time.Hour}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 5)
		if _, err := dr.Read(buf); err != nil || !bytes.Equal(buf, []byte("hello")) {
			t.Errorf("read %q err %v", buf, err)
		}
	}()

	if _, err := dw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	<-done
}

// TestDeadlineReaderQuotaExceeded 验证当流量配额超限时，deadlineReader.Read 返回错误，
// 从而使 io.Copy 终止隧道转发。
func TestDeadlineReaderQuotaExceeded(t *testing.T) {
	// 初始化 logger 避免空指针
	if err := InitLogger(LogConfig{Level: "info", Output: "stdout"}); err != nil {
		t.Fatal(err)
	}

	accounter := NewTrafficAccounter(TrafficAuditConfig{
		Enabled:      true,
		PerUserQuota: 100,
		QuotaWindow:  Duration(time.Hour),
	})
	defer accounter.Stop()

	key := TrafficKey{User: "quotauser", Upstream: "up1", Target: "example.com"}

	// 先消耗接近配额的流量
	if err := accounter.Record(key, 90, 0); err != nil {
		t.Fatalf("pre-record within quota: %v", err)
	}

	// 创建 pipe 模拟连接
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	dr := &deadlineReader{
		r:              c1,
		conn:           c1,
		timeout:        time.Hour,
		trafficKey:     key,
		accounter:      accounter,
		auditDirection: "in",
	}

	// 从另一端写入 50 字节，这将使总流量超过 100 字节配额
	go func() {
		c2.Write([]byte("0123456789")) // 10 bytes, total will be 100, still ok
	}()

	buf := make([]byte, 10)
	n, err := dr.Read(buf)
	if err != nil {
		t.Fatalf("first read within quota should succeed: n=%d err=%v", n, err)
	}
	if n != 10 {
		t.Fatalf("expected 10 bytes, got %d", n)
	}

	// 再写 50 字节，总流量 150 > 100 配额，应返回错误
	go func() {
		c2.Write(bytes.Repeat([]byte("x"), 50))
	}()

	buf2 := make([]byte, 50)
	n2, err2 := dr.Read(buf2)
	// 配额超限应返回错误
	if err2 == nil {
		t.Fatalf("expected quota exceeded error, got nil (n=%d)", n2)
	}
	if !strings.Contains(err2.Error(), "quota") {
		t.Fatalf("expected quota error, got: %v", err2)
	}
}
