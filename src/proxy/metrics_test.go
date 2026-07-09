package proxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsIncAddSetValue(t *testing.T) {
	MetricSet("k5pw_requests_total", 0, "protocol=http")
	MetricInc("k5pw_requests_total", "protocol=http")
	MetricAdd("k5pw_requests_total", 2, "protocol=http")
	MetricSet("k5pw_connections_active", 5)

	if got := MetricValue("k5pw_requests_total", "protocol=http"); got != 3 {
		t.Fatalf("requests = %v, want 3", got)
	}
	if got := MetricValue("k5pw_connections_active"); got != 5 {
		t.Fatalf("connections = %v, want 5", got)
	}
	if got := MetricValue("k5pw_missing"); got != 0 {
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
	MetricInc("k5pw_requests_total", "protocol=http")
	MetricSet("k5pw_connections_active", 1)

	var buf bytes.Buffer
	ExposeMetrics(&buf)
	out := buf.String()
	if !strings.Contains(out, "k5pw_requests_total") {
		t.Fatalf("output missing requests metric: %s", out)
	}
	if !strings.Contains(out, "k5pw_connections_active") {
		t.Fatalf("output missing connections metric: %s", out)
	}
}

func TestMatchingKeys(t *testing.T) {
	keys := matchingKeys("k5pw_requests_total")
	found := false
	for _, k := range keys {
		if k == "k5pw_requests_total{protocol=http}" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matching keys = %#v", keys)
	}
}
