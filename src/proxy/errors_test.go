package proxy

import (
	"errors"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
)

func TestProxyError(t *testing.T) {
	base := errors.New("underlying")
	err := WrapError(CodeDNSFailure, "lookup failed", base)
	if got := err.Error(); got != "[DNS_FAILURE] lookup failed: underlying" {
		t.Fatalf("unexpected error string: %s", got)
	}
	if !errors.Is(err, base) {
		t.Fatal("expected errors.Is to match underlying")
	}
	if ErrorCodeFrom(err) != CodeDNSFailure {
		t.Fatalf("expected DNS_FAILURE, got %s", ErrorCodeFrom(err))
	}
}

func TestNewError(t *testing.T) {
	err := NewError(CodeInternalError, "oops")
	if got := err.Error(); got != "[INTERNAL_ERROR] oops" {
		t.Fatalf("unexpected error string: %s", got)
	}
}

func TestClassifyNetError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorCode
	}{
		{"nil", nil, ""},
		{"dns", errors.New("lookup example.com failed: no such host"), CodeDNSFailure},
		{"refused", syscall.ECONNREFUSED, CodeConnectionRefused},
		{"refused string", errors.New("connection refused"), CodeConnectionRefused},
		{"timeout net.Error", &timeoutError{}, CodeConnectionTimeout},
		{"timeout syscall", syscall.ETIMEDOUT, CodeConnectionTimeout},
		{"timeout string", errors.New("i/o timeout"), CodeConnectionTimeout},
		{"deadline", errors.New("deadline exceeded"), CodeConnectionTimeout},
		{"deadline os", os.ErrDeadlineExceeded, CodeConnectionTimeout},
		{"net unreachable", syscall.ENETUNREACH, CodeNetworkUnreachable},
		{"host unreachable", syscall.EHOSTUNREACH, CodeNetworkUnreachable},
		{"network unreachable string", errors.New("network is unreachable"), CodeNetworkUnreachable},
		{"target unreachable", errors.New("target unreachable"), CodeTargetUnreachable},
		{"generic", errors.New("something else"), CodeInternalError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyNetError(tc.err)
			if got != tc.want {
				t.Fatalf("ClassifyNetError(%v) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyDialError(t *testing.T) {
	if ClassifyDialError("target", nil) != nil {
		t.Fatal("expected nil for nil error")
	}
	err := ClassifyDialError("1.2.3.4:80", syscall.ECONNREFUSED)
	if ErrorCodeFrom(err) != CodeConnectionRefused {
		t.Fatalf("expected connection refused, got %s", ErrorCodeFrom(err))
	}
}

func TestSOCKS5ReplyFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want byte
	}{
		{"nil", nil, 0x00},
		{"auth", NewError(CodeAuthFailure, "auth"), 0x02},
		{"network unreachable", NewError(CodeNetworkUnreachable, "unreachable"), 0x03},
		{"dns", NewError(CodeDNSFailure, "dns"), 0x04},
		{"target unreachable", NewError(CodeTargetUnreachable, "target"), 0x04},
		{"refused", NewError(CodeConnectionRefused, "refused"), 0x05},
		{"timeout", NewError(CodeConnectionTimeout, "timeout"), 0x06},
		{"protocol", NewError(CodeProtocolError, "proto"), 0x01},
		{"upstream", NewError(CodeUpstreamFailure, "upstream"), 0x01},
		{"internal", NewError(CodeInternalError, "internal"), 0x01},
		{"net refused", syscall.ECONNREFUSED, 0x05},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SOCKS5ReplyFromError(tc.err); got != tc.want {
				t.Fatalf("SOCKS5ReplyFromError(%v) = 0x%02x, want 0x%02x", tc.err, got, tc.want)
			}
		})
	}
}

func TestHTTPStatusFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"auth", NewError(CodeAuthFailure, "auth"), http.StatusProxyAuthRequired},
		{"timeout", NewError(CodeConnectionTimeout, "timeout"), http.StatusGatewayTimeout},
		{"refused", NewError(CodeConnectionRefused, "refused"), http.StatusBadGateway},
		{"dns", NewError(CodeDNSFailure, "dns"), http.StatusBadGateway},
		{"net unreachable", NewError(CodeNetworkUnreachable, "unreachable"), http.StatusBadGateway},
		{"target unreachable", NewError(CodeTargetUnreachable, "target"), http.StatusBadGateway},
		{"upstream", NewError(CodeUpstreamFailure, "upstream"), http.StatusBadGateway},
		{"protocol", NewError(CodeProtocolError, "proto"), http.StatusBadGateway},
		{"generic", errors.New("generic"), http.StatusBadGateway},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPStatusFromError(tc.err); got != tc.want {
				t.Fatalf("HTTPStatusFromError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestHTTPStatusText(t *testing.T) {
	if HTTPStatusText(http.StatusOK) != "OK" {
		t.Fatal("expected OK")
	}
	if HTTPStatusText(999) != "Error" {
		t.Fatal("expected Error for unknown status")
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}
