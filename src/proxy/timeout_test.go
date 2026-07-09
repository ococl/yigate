package proxy

import (
	"bytes"
	"net"
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
