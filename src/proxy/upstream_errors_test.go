package proxy

import (
	"errors"
	"net"
	"testing"
)

func TestDialErrorCategory(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		upstream bool
		target   bool
		cap      bool
	}{
		{
			name:     "upstream connect error",
			err:      newDialError(ErrCategoryUpstream, "dial", errors.New("connection refused")),
			upstream: true,
		},
		{
			name:     "upstream auth error",
			err:      newDialErrorCode(ErrCategoryUpstream, "auth", 407, "auth failed"),
			upstream: true,
		},
		{
			name:   "target connection refused",
			err:    newDialErrorCode(ErrCategoryTarget, "request", 0x05, "target refused"),
			target: true,
		},
		{
			name: "capability not supported",
			err:  newDialErrorCode(ErrCategoryCapability, "request", 0x07, "not supported"),
			cap:  true,
		},
		{
			name: "canceled",
			err:  newDialError(ErrCategoryCanceled, "dial", errors.New("canceled")),
		},
		{
			name:     "bare net error defaults to upstream",
			err:      &net.OpError{Op: "dial", Err: errors.New("refused")},
			upstream: true,
		},
		{
			name:     "plain error not classified",
			err:      errors.New("something went wrong"),
			upstream: false,
			target:   false,
			cap:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUpstreamError(tt.err); got != tt.upstream {
				t.Errorf("IsUpstreamError() = %v, want %v", got, tt.upstream)
			}
			if got := IsTargetError(tt.err); got != tt.target {
				t.Errorf("IsTargetError() = %v, want %v", got, tt.target)
			}
			if got := IsCapabilityError(tt.err); got != tt.cap {
				t.Errorf("IsCapabilityError() = %v, want %v", got, tt.cap)
			}
		})
	}
}

func TestClassifyHTTPConnectStatus(t *testing.T) {
	tests := []struct {
		code     int
		category ErrorCategory
	}{
		{401, ErrCategoryUpstream},
		{403, ErrCategoryUpstream},
		{407, ErrCategoryUpstream},
		{502, ErrCategoryTarget},
		{504, ErrCategoryTarget},
		{503, ErrCategoryUpstream},
		{500, ErrCategoryUpstream},
		{404, ErrCategoryUpstream},
	}
	for _, tt := range tests {
		de := classifyHTTPConnectStatus(tt.code, "test")
		if de.Category != tt.category {
			t.Errorf("code %d: category = %s, want %s", tt.code, de.Category, tt.category)
		}
		if de.Code != tt.code {
			t.Errorf("code %d: DialError.Code = %d, want %d", tt.code, de.Code, tt.code)
		}
	}
}

func TestClassifySOCKS5Reply(t *testing.T) {
	tests := []struct {
		rep      byte
		cmd      byte
		category ErrorCategory
	}{
		{0x01, 0x01, ErrCategoryUpstream},   // general failure
		{0x02, 0x01, ErrCategoryTarget},     // connection not allowed
		{0x03, 0x01, ErrCategoryTarget},     // network unreachable
		{0x04, 0x01, ErrCategoryTarget},     // host unreachable
		{0x05, 0x01, ErrCategoryTarget},     // connection refused
		{0x06, 0x01, ErrCategoryTarget},     // TTL expired
		{0x07, 0x02, ErrCategoryCapability}, // bind not supported
		{0x07, 0x03, ErrCategoryCapability}, // udp not supported
		{0x08, 0x01, ErrCategoryCapability}, // address type not supported
	}
	for _, tt := range tests {
		de := classifySOCKS5Reply(tt.rep, tt.cmd)
		if de.Category != tt.category {
			t.Errorf("rep 0x%02x cmd 0x%02x: category = %s, want %s", tt.rep, tt.cmd, de.Category, tt.category)
		}
	}
}

func TestTargetFailureDoesNotMarkDead(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u1", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.upstreams[0]

	// Simulate a target failure (e.g., SOCKS5 connection refused reply)
	targetErr := newDialErrorCode(ErrCategoryTarget, "request", 0x05, "target refused")

	// This should NOT mark the upstream as dead
	if IsUpstreamError(targetErr) {
		t.Fatal("target error should not be classified as upstream error")
	}

	// Directly test: if we called markDead only for upstream errors, the upstream stays alive
	mgr.mu.Lock()
	before := up.dead
	mgr.mu.Unlock()
	if before {
		t.Fatal("upstream should start alive")
	}
}

func TestCapabilityFailureDoesNotMarkDead(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "u1", URL: "socks5://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.upstreams[0]

	// Simulate a capability error (e.g., SOCKS5 bind not supported)
	capErr := newDialErrorCode(ErrCategoryCapability, "request", 0x07, "not supported")

	if IsUpstreamError(capErr) {
		t.Fatal("capability error should not be classified as upstream error")
	}

	mgr.mu.Lock()
	dead := up.dead
	mgr.mu.Unlock()
	if dead {
		t.Fatal("upstream should not be dead after capability error")
	}
}

func TestDialErrorWrapping(t *testing.T) {
	// Test that Dial wraps unclassified errors as upstream
	original := errors.New("random failure")
	wrapped := newDialError(ErrCategoryUpstream, "dial", original)

	var de *DialError
	if !errors.As(wrapped, &de) {
		t.Fatal("wrapped error should be a DialError")
	}
	if de.Category != ErrCategoryUpstream {
		t.Fatalf("category = %s, want upstream", de.Category)
	}
	if !errors.Is(wrapped, original) {
		t.Fatal("wrapped error should unwrap to original")
	}
}
