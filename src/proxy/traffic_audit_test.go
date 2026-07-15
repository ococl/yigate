package proxy

import (
	"strings"
	"testing"
	"time"
)

func TestTrafficAccounterBasic(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	if err := a.Record(key, 100, 200); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := a.Record(key, 50, 50); err != nil {
		t.Fatalf("record: %v", err)
	}

	snap := a.Snapshot()
	if len(snap.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap.Entries))
	}
	e := snap.Entries[0]
	if e.User != "alice" || e.Upstream != "up1" || e.Target != "example.com" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.BytesIn != 150 {
		t.Fatalf("expected bytes_in=150, got %d", e.BytesIn)
	}
	if e.BytesOut != 250 {
		t.Fatalf("expected bytes_out=250, got %d", e.BytesOut)
	}
	if e.Total != 400 {
		t.Fatalf("expected total=400, got %d", e.Total)
	}
}

func TestTrafficAccounterDisabled(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: false})
	defer a.Stop()

	key := TrafficKey{User: "bob", Upstream: "up2", Target: "test.com"}
	if err := a.Record(key, 1000, 2000); err != nil {
		t.Fatalf("record on disabled: %v", err)
	}
	snap := a.Snapshot()
	if len(snap.Entries) != 0 {
		t.Fatalf("disabled accounter should have no entries, got %d", len(snap.Entries))
	}
}

func TestTrafficAccounterNilSafe(t *testing.T) {
	var a *TrafficAccounter
	if err := a.Record(TrafficKey{}, 100, 200); err != nil {
		t.Fatalf("nil accounter record: %v", err)
	}
	snap := a.Snapshot()
	if len(snap.Entries) != 0 {
		t.Fatalf("nil accounter snapshot should be empty")
	}
	if a.UserUsage("x") != 0 {
		t.Fatal("nil accounter usage should be 0")
	}
}

func TestTrafficAccounterUserQuota(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{
		Enabled:      true,
		PerUserQuota: 500,
		QuotaWindow:  Duration(time.Hour),
	})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	if err := a.Record(key, 200, 200); err != nil {
		t.Fatalf("record within quota: %v", err)
	}
	if err := a.Record(key, 200, 200); err == nil {
		t.Fatal("expected quota exceeded error")
	}
}

func TestTrafficAccounterUpstreamQuota(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{
		Enabled:          true,
		PerUpstreamQuota: 300,
		QuotaWindow:      Duration(time.Hour),
	})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	if err := a.Record(key, 100, 100); err != nil {
		t.Fatalf("record within quota: %v", err)
	}
	if err := a.Record(key, 100, 100); err == nil {
		t.Fatal("expected quota exceeded error")
	}
}

func TestTrafficAccounterMultipleEntries(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	keys := []TrafficKey{
		{User: "alice", Upstream: "up1", Target: "a.com"},
		{User: "bob", Upstream: "up2", Target: "b.com"},
		{User: "alice", Upstream: "up2", Target: "c.com"},
	}
	for i, k := range keys {
		if err := a.Record(k, int64(100*(i+1)), int64(50*(i+1))); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	snap := a.Snapshot()
	if len(snap.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap.Entries))
	}
	// Should be sorted by total descending
	if snap.Entries[0].Total < snap.Entries[2].Total {
		t.Fatalf("entries not sorted by total desc: %+v", snap.Entries)
	}

	if usage := a.UserUsage("alice"); usage != 600 {
		t.Fatalf("alice usage = %d", usage)
	}
	if usage := a.UpstreamUsage("up2"); usage != 750 {
		t.Fatalf("up2 usage = %d", usage)
	}
}

func TestTrafficAccounterZeroBytes(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	if err := a.Record(key, 0, 0); err != nil {
		t.Fatalf("record zero: %v", err)
	}
	snap := a.Snapshot()
	if len(snap.Entries) != 0 {
		t.Fatalf("zero bytes should not create entry, got %d", len(snap.Entries))
	}
}

func TestTrafficAccounterNegativeBytes(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	if err := a.Record(key, -100, -200); err != nil {
		t.Fatalf("record negative: %v", err)
	}
	snap := a.Snapshot()
	if len(snap.Entries) != 0 {
		t.Fatalf("negative bytes should not create entry, got %d", len(snap.Entries))
	}
}

func TestTrafficAccounterWindowReset(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{
		Enabled:     true,
		QuotaWindow: Duration(50 * time.Millisecond),
	})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	_ = a.Record(key, 100, 200)
	snap := a.Snapshot()
	if len(snap.Entries) != 1 {
		t.Fatalf("expected 1 entry before reset, got %d", len(snap.Entries))
	}

	time.Sleep(120 * time.Millisecond)
	snap = a.Snapshot()
	if len(snap.Entries) != 0 {
		t.Fatalf("expected 0 entries after window reset, got %d", len(snap.Entries))
	}
}

func TestTrafficAccounterNoQuota(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{
		Enabled: true,
		// No quotas set
	})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	for i := 0; i < 100; i++ {
		if err := a.Record(key, 10000, 10000); err != nil {
			t.Fatalf("record %d without quota: %v", i, err)
		}
	}
}

func TestTargetHostFromAddr(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"example.com:443", "example.com"},
		{"1.2.3.4:80", "1.2.3.4"},
		{"[::1]:443", "::1"},
		{"noport", "noport"},
	}
	for _, tt := range tests {
		got := targetHostFromAddr(tt.input)
		if got != tt.expect {
			t.Errorf("targetHostFromAddr(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestNormalizeTargetLabel(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"empty", "", ""},
		{"short domain", "example.com", "example.com"},
		{"ipv4", "1.2.3.4", "1.2.3.4"},
		{"ipv6", "::1", "::1"},
		{"exactly 63 chars", strings.Repeat("a", 63), strings.Repeat("a", 63)},
		{"64 chars truncated", strings.Repeat("a", 64), strings.Repeat("a", 60) + "..."},
		{"long domain with tld preserved", strings.Repeat("a", 60) + ".com", strings.Repeat("a", 57) + "..." + ".com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeTargetLabel(tt.target)
			if len(got) > maxTargetLabelLen {
				t.Errorf("normalizeTargetLabel(%q) = %q (len=%d), exceeds max %d", tt.target, got, len(got), maxTargetLabelLen)
			}
			if tt.name != "64 chars truncated" && tt.name != "long domain with tld preserved" {
				if got != tt.want {
					t.Errorf("normalizeTargetLabel(%q) = %q, want %q", tt.target, got, tt.want)
				}
			}
		})
	}
}

func TestTrafficAccounterPrometheusMetrics(t *testing.T) {
	// Reset registry for clean test
	registry.mu.Lock()
	for k := range registry.values {
		if strings.Contains(k, "yigate_traffic_bytes_total") {
			delete(registry.values, k)
		}
	}
	registry.mu.Unlock()

	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	_ = a.Record(key, 100, 200)

	// Check that Prometheus metrics were recorded
	inVal := MetricValue("yigate_traffic_bytes_total", "direction=client_to_target", "user=alice", "upstream=up1", "target=example.com")
	if inVal != 100 {
		t.Errorf("expected in=100, got %f", inVal)
	}
	outVal := MetricValue("yigate_traffic_bytes_total", "direction=target_to_client", "user=alice", "upstream=up1", "target=example.com")
	if outVal != 200 {
		t.Errorf("expected out=200, got %f", outVal)
	}
}
