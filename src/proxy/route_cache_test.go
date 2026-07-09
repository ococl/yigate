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

func TestTargetHostKeepsExplicitPort(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com:8080/path", nil)
	if got := targetHost(req); got != "example.com:8080" {
		t.Fatalf("targetHost() = %q, want %q", got, "example.com:8080")
	}
}
