package proxy

import "testing"

func TestNewUpstreamManagerAcceptsSocks5H(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "remote-dns", URL: "socks5h://127.0.0.1:1080"}})
	if err != nil {
		t.Fatalf("NewUpstreamManager() error = %v", err)
	}
	if got := mgr.Socks5AttemptCount(nil); got != 1 {
		t.Fatalf("Socks5AttemptCount() = %d, want 1", got)
	}
	if up := mgr.SelectSocks5For(nil); up == nil || up.Name != "remote-dns" {
		t.Fatalf("SelectSocks5For() = %#v, want remote-dns", up)
	}
}

func TestNewUpstreamManagerRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewUpstreamManager([]UpstreamConfig{{URL: "ftp://127.0.0.1:21"}})
	if err == nil {
		t.Fatal("NewUpstreamManager() accepted unsupported scheme")
	}
}
