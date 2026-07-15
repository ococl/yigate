package proxy

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMetricsIncAddSetValue(t *testing.T) {
	MetricSet("yigate_requests_total", 0, "protocol=http")
	MetricInc("yigate_requests_total", "protocol=http")
	MetricAdd("yigate_requests_total", 2, "protocol=http")
	MetricSet("yigate_connections_active", 5)

	if got := MetricValue("yigate_requests_total", "protocol=http"); got != 3 {
		t.Fatalf("requests = %v, want 3", got)
	}
	if got := MetricValue("yigate_connections_active"); got != 5 {
		t.Fatalf("connections = %v, want 5", got)
	}
	if got := MetricValue("yigate_missing"); got != 0 {
		t.Fatalf("missing = %v, want 0", got)
	}
}

func TestMetricKey(t *testing.T) {
	if got := metricKey("name", nil); got != "name" {
		t.Fatalf("metricKey = %q", got)
	}
	if got := metricKey("name", []string{"a=1", "b=2"}); got != "name{a=1,b=2}" {
		t.Fatalf("metricKey = %q", got)
	}
}

func TestExposeMetrics(t *testing.T) {
	MetricInc("yigate_requests_total", "protocol=http")
	MetricSet("yigate_connections_active", 1)

	var buf bytes.Buffer
	ExposeMetrics(&buf)
	out := buf.String()
	if !strings.Contains(out, "yigate_requests_total") {
		t.Fatalf("output missing requests metric: %s", out)
	}
	if !strings.Contains(out, "yigate_connections_active") {
		t.Fatalf("output missing connections metric: %s", out)
	}
}

func TestMatchingKeys(t *testing.T) {
	keys := matchingKeys("yigate_requests_total")
	found := false
	for _, k := range keys {
		if k == "yigate_requests_total{protocol=http}" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matching keys = %#v", keys)
	}
}

func TestMetricsMaxEntries(t *testing.T) {
	// 使用独立的 Metrics 实例避免污染全局 registry
	m := &Metrics{values: make(map[string]float64)}

	// 填满到上限
	for i := 0; i < metricsMaxEntries; i++ {
		m.metricAdd("test_metric", 1, "idx="+strconv.Itoa(i))
	}
	if len(m.values) != metricsMaxEntries {
		t.Fatalf("expected %d entries, got %d", metricsMaxEntries, len(m.values))
	}

	// 新的标签组合不应创建新条目
	m.metricAdd("test_metric", 1, "idx=new")
	if len(m.values) != metricsMaxEntries {
		t.Fatalf("expected entries to stay at %d, got %d", metricsMaxEntries, len(m.values))
	}

	// 已有条目仍可更新
	m.metricAdd("test_metric", 5, "idx=0")
	if got := m.values["test_metric{idx=0}"]; got != 6 {
		t.Fatalf("expected existing entry updated to 6, got %v", got)
	}
}

func TestTunnelMetrics(t *testing.T) {
	MetricSet("yigate_tunnel_transferred_bytes_total", 0, "direction=client_to_target")
	MetricSet("yigate_tunnel_transferred_bytes_total", 0, "direction=target_to_client")
	MetricSet("yigate_tunnel_packets_total", 0, "direction=client_to_target")
	MetricSet("yigate_tunnel_packets_total", 0, "direction=target_to_client")

	client, proxySide := net.Pipe()
	targetSide, target := net.Pipe()
	done := make(chan struct{})
	go func() {
		Tunnel(proxySide, nil, targetSide, TrafficKey{}, nil)
		close(done)
	}()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	target.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := target.Read(buf)
	if err != nil || string(buf[:n]) != "ping" {
		t.Fatalf("target read = %q, %v", buf[:n], err)
	}
	if _, err := target.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = client.Read(buf)
	if err != nil || string(buf[:n]) != "pong" {
		t.Fatalf("client read = %q, %v", buf[:n], err)
	}
	client.Close()
	target.Close()
	<-done

	if got := MetricValue("yigate_tunnel_transferred_bytes_total", "direction=client_to_target"); got < 4 {
		t.Fatalf("client_to_target bytes = %v, want >= 4", got)
	}
	if got := MetricValue("yigate_tunnel_transferred_bytes_total", "direction=target_to_client"); got < 4 {
		t.Fatalf("target_to_client bytes = %v, want >= 4", got)
	}
	if got := MetricValue("yigate_tunnel_packets_total", "direction=client_to_target"); got < 1 {
		t.Fatalf("client_to_target packets = %v, want >= 1", got)
	}
	if got := MetricValue("yigate_tunnel_packets_total", "direction=target_to_client"); got < 1 {
		t.Fatalf("target_to_client packets = %v, want >= 1", got)
	}
}
