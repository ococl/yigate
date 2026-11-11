package proxy

import (
	"fmt"
	"testing"
	"time"
)

func TestLogSamplerSuppressesWithinWindow(t *testing.T) {
	s := NewLogSampler(time.Second)
	base := time.Now()

	if emit, _ := s.allow("k", base); !emit {
		t.Fatal("first occurrence should be emitted")
	}
	for i := 0; i < 50; i++ {
		if emit, _ := s.allow("k", base.Add(10*time.Millisecond)); emit {
			t.Fatalf("occurrence %d inside the window should be suppressed", i)
		}
	}
	// 窗口结束后放行，并如实报告被抑制的条数。
	emit, suppressed := s.allow("k", base.Add(1100*time.Millisecond))
	if !emit {
		t.Fatal("first occurrence after the window should be emitted")
	}
	if suppressed != 50 {
		t.Fatalf("expected suppressed=50, got %d", suppressed)
	}
	// 其它 key 互不影响。
	if emit, _ := s.allow("other", base.Add(20*time.Millisecond)); !emit {
		t.Fatal("independent key should be emitted")
	}
}

func TestLogSamplerDoesNotDropLogsWhenTableFull(t *testing.T) {
	s := NewLogSampler(time.Second)
	s.maxEntries = 4
	base := time.Now()
	for i := 0; i < 4; i++ {
		s.allow(fmt.Sprintf("k%d", i), base)
	}
	// 采样表已满且都在窗口内：放行，不能因为采样而丢错误。
	if emit, _ := s.allow("overflow", base); !emit {
		t.Fatal("log must not be dropped when the sample table is full")
	}
	// 过期窗口会被清理，之后可以重新记录新 key。
	if emit, _ := s.allow("after-expiry", base.Add(2*time.Second)); !emit {
		t.Fatal("log should be emitted after old windows expire")
	}
	if got := len(s.entries); got > 4 {
		t.Fatalf("expired entries were not reclaimed: %d entries", got)
	}
}
