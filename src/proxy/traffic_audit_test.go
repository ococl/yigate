package proxy

import (
	"os"
	"path/filepath"
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
	if len(snap.Entries) != 1 {
		t.Fatalf("expected history to survive window reset, got %d entries", len(snap.Entries))
	}
	if snap.Entries[0].Total != 300 {
		t.Fatalf("expected 300 bytes after window reset, got %d", snap.Entries[0].Total)
	}
	if usage := a.UserUsage("alice"); usage != 0 {
		t.Fatalf("quota window usage should reset to 0, got %d", usage)
	}
}

func TestTrafficAccounterReconfigurePreservesHistory(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	_ = a.Record(key, 100, 200)

	a.Reconfigure(TrafficAuditConfig{
		Enabled:          true,
		PerUserQuota:     1000,
		PerUpstreamQuota: 2000,
		QuotaWindow:      Duration(2 * time.Hour),
		RetentionDays:    30,
	})
	snap := a.Snapshot()
	if len(snap.Entries) != 1 || snap.Entries[0].Total != 300 {
		t.Fatalf("history lost after reconfigure: %+v", snap.Entries)
	}
}

func TestTrafficAccounterToggleEnabledPreservesHistory(t *testing.T) {
	a := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	defer a.Stop()

	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}
	_ = a.Record(key, 100, 200)

	a.Reconfigure(TrafficAuditConfig{Enabled: false})
	a.Reconfigure(TrafficAuditConfig{Enabled: true})

	snap := a.Snapshot()
	if len(snap.Entries) != 1 || snap.Entries[0].Total != 300 {
		t.Fatalf("history lost after toggling audit: %+v", snap.Entries)
	}
	if usage := a.UserUsage("alice"); usage != 0 {
		t.Fatalf("quota usage should reset after toggling audit, got %d", usage)
	}
}

func TestTrafficAccounterPersistsHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}

	a := NewTrafficAccounterWithStore(TrafficAuditConfig{Enabled: true, RetentionDays: 30}, path)
	if err := a.Record(key, 100, 200); err != nil {
		t.Fatalf("record: %v", err)
	}
	a.Stop()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("traffic history file not written: %v", err)
	}

	b := NewTrafficAccounterWithStore(TrafficAuditConfig{Enabled: true, RetentionDays: 30}, path)
	defer b.Stop()
	snap := b.Snapshot()
	if len(snap.Entries) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(snap.Entries))
	}
	if snap.Entries[0].BytesIn != 100 || snap.Entries[0].BytesOut != 200 {
		t.Fatalf("unexpected persisted entry: %+v", snap.Entries[0])
	}
	if snap.RetentionDays != 30 {
		t.Fatalf("retention_days = %d, want 30", snap.RetentionDays)
	}
}

func TestTrafficAccounterPrunesExpiredHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	today := time.Now().Format(time.DateOnly)
	oldDay := time.Now().AddDate(0, 0, -31).Format(time.DateOnly)
	err := writeTrafficHistory(path, []trafficHistoryDiskEntry{
		{Day: oldDay, User: "old", Upstream: "up1", Target: "old.example.com", BytesIn: 1, BytesOut: 2},
		{Day: today, User: "new", Upstream: "up1", Target: "new.example.com", BytesIn: 3, BytesOut: 4},
	})
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a := NewTrafficAccounterWithStore(TrafficAuditConfig{Enabled: true, RetentionDays: 30}, path)
	defer a.Stop()
	snap := a.Snapshot()
	if len(snap.Entries) != 1 || snap.Entries[0].User != "new" {
		t.Fatalf("expired history not pruned: %+v", snap.Entries)
	}

	a.Flush()
	loaded, err := loadTrafficHistory(path)
	if err != nil {
		t.Fatalf("load after prune: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Day != today {
		t.Fatalf("expired entry still on disk: %+v", loaded)
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

	// Check that Prometheus metrics were recorded（不含高基数 target 维度）
	inVal := MetricValue("yigate_traffic_bytes_total", "direction=client_to_target", "user=alice", "upstream=up1")
	if inVal != 100 {
		t.Errorf("expected in=100, got %f", inVal)
	}
	outVal := MetricValue("yigate_traffic_bytes_total", "direction=target_to_client", "user=alice", "upstream=up1")
	if outVal != 200 {
		t.Errorf("expected out=200, got %f", outVal)
	}
}

// TestTrafficAccounterRestoresHistoryFromDiskWhenMemoryEmpty 保证统计器内存意外为空时
// 不会把已持久化的历史覆盖成空文件。
func TestTrafficAccounterRestoresHistoryFromDiskWhenMemoryEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	key := TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}

	a := NewTrafficAccounterWithStore(TrafficAuditConfig{Enabled: true, RetentionDays: 30}, path)
	if err := a.Record(key, 100, 200); err != nil {
		t.Fatalf("record: %v", err)
	}
	a.Flush()

	// 模拟内存明细丢失（例如统计器被重建或加载异常）
	a.mu.Lock()
	a.history = make(map[string]*trafficHistoryEntry)
	a.mu.Unlock()
	a.dirtyGen.Add(1)

	a.Flush()

	snap := a.Snapshot()
	if len(snap.Entries) != 1 {
		t.Fatalf("expected history restored from disk, got %d entries", len(snap.Entries))
	}
	entries, err := loadTrafficHistory(path)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(entries) != 1 || entries[0].BytesIn != 100 || entries[0].BytesOut != 200 {
		t.Fatalf("disk history was not preserved: %+v", entries)
	}
	a.Stop()
}

// TestTrafficHistoryCorruptFileBackedUp 保证无法解析的历史文件被备份而不是被静默覆盖。
func TestTrafficHistoryCorruptFileBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	a := NewTrafficAccounterWithStore(TrafficAuditConfig{Enabled: true, RetentionDays: 30}, path)
	a.Stop()

	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob backup: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 backup file, got %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(raw) != "{not-json" {
		t.Fatalf("backup content = %q", string(raw))
	}
}
