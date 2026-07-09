package proxy

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRouteCacheExpiresEntries(t *testing.T) {
	cache := NewRouteCache()
	cache.SetDirect("example.com", time.Nanosecond)
	time.Sleep(time.Millisecond)
	if _, ok := cache.Get("example.com"); ok {
		t.Fatal("expired cache entry was returned")
	}
}

func TestTargetHostAddsDefaultHTTPPort(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/path", nil)
	if got := targetHost(req); got != "example.com:80" {
		t.Fatalf("targetHost() = %q, want %q", got, "example.com:80")
	}
}

func TestRouteCacheGetSetDelete(t *testing.T) {
	cache := NewRouteCache()
	if _, ok := cache.Get("missing"); ok {
		t.Fatal("expected miss")
	}
	cache.SetDirect("d", time.Hour)
	if entry, ok := cache.Get("d"); !ok || entry.Action != ActionDirect {
		t.Fatal("expected direct hit")
	}
	cache.SetProxy("p", "up", time.Hour)
	if entry, ok := cache.Get("p"); !ok || entry.Action != ActionProxy || entry.UpstreamName != "up" {
		t.Fatal("expected proxy hit")
	}
	cache.SetProxy("empty", "", time.Hour)
	if _, ok := cache.Get("empty"); ok {
		t.Fatal("expected proxy with empty upstream not stored")
	}
	cache.Delete("d")
	if _, ok := cache.Get("d"); ok {
		t.Fatal("expected deleted")
	}
}

func TestRouteCacheCapEviction(t *testing.T) {
	cache := NewRouteCacheWithCap(2)
	cache.SetDirect("a", time.Hour)
	cache.SetDirect("b", time.Hour)
	cache.SetDirect("c", time.Hour)
	if _, ok := cache.Get("a"); ok {
		t.Fatal("expected oldest evicted")
	}
}
