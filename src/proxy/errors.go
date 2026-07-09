package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
)

// ErrorCode 是 k5pw 内部统一错误码，用于日志、指标和客户端响应分类。
type ErrorCode string

const (
	CodeDNSFailure         ErrorCode = "DNS_FAILURE"
	CodeConnectionRefused  ErrorCode = "CONNECTION_REFUSED"
	CodeConnectionTimeout  ErrorCode = "CONNECTION_TIMEOUT"
	CodeNetworkUnreachable ErrorCode = "NETWORK_UNREACHABLE"
	CodeProtocolError      ErrorCode = "PROTOCOL_ERROR"
	CodeAuthFailure        ErrorCode = "AUTH_FAILURE"
	CodeUpstreamFailure    ErrorCode = "UPSTREAM_FAILURE"
	CodeTargetUnreachable  ErrorCode = "TARGET_UNREACHABLE"
	CodeInternalError      ErrorCode = "INTERNAL_ERROR"
)

// ProxyError 是带错误码的统一错误类型。
type ProxyError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *ProxyError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 返回底层错误，支持 errors.Is/As 链式判断。
func (e *ProxyError) Unwrap() error { return e.Cause }

// NewError 创建一个新的 ProxyError。
func NewError(code ErrorCode, msg string) error {
	return &ProxyError{Code: code, Message: msg}
}

// WrapError 用统一错误码包装一个底层错误。
func WrapError(code ErrorCode, msg string, cause error) error {
	return &ProxyError{Code: code, Message: msg, Cause: cause}
}

// ErrorCodeFrom 返回 err 对应的错误码；如果不是 ProxyError 则返回空字符串。
func ErrorCodeFrom(err error) ErrorCode {
	var pe *ProxyError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// ClassifyNetError 根据标准 net 错误分类为内部错误码。
func ClassifyNetError(err error) ErrorCode {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// DNS 相关错误
	if strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "DNS") ||
		strings.Contains(msg, "lookup") && strings.Contains(msg, "failed") {
		return CodeDNSFailure
	}

	// 连接被拒绝
	if errors.Is(err, syscall.ECONNREFUSED) ||
		strings.Contains(msg, "connection refused") {
		return CodeConnectionRefused
	}

	// 连接超时
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CodeConnectionTimeout
	}
	if errors.Is(err, syscall.ETIMEDOUT) ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") {
		return CodeConnectionTimeout
	}

	// 网络不可达
	if errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "host is unreachable") {
		return CodeNetworkUnreachable
	}

	// 目标不可达（来自 SOCKS5 上游的回复码）
	if strings.Contains(msg, "target unreachable") ||
		strings.Contains(msg, "no route to host") {
		return CodeTargetUnreachable
	}

	if errors.Is(err, os.ErrDeadlineExceeded) {
		return CodeConnectionTimeout
	}

	return CodeInternalError
}

// ClassifyDialError 对拨号结果进行分类，返回统一错误。
func ClassifyDialError(target string, err error) error {
	if err == nil {
		return nil
	}
	code := ClassifyNetError(err)
	if code == "" {
		code = CodeInternalError
	}
	return WrapError(code, fmt.Sprintf("dial %s failed", target), err)
}

// SOCKS5ReplyFromError 将内部错误映射为 SOCKS5 标准回复码（REP）。
func SOCKS5ReplyFromError(err error) byte {
	if err == nil {
		return 0x00
	}
	code := ErrorCodeFrom(err)
	if code == "" {
		code = ClassifyNetError(err)
	}
	switch code {
	case CodeAuthFailure:
		return 0x02 // connection not allowed by ruleset
	case CodeNetworkUnreachable:
		return 0x03 // network unreachable
	case CodeDNSFailure, CodeTargetUnreachable:
		return 0x04 // host unreachable
	case CodeConnectionRefused:
		return 0x05 // connection refused
	case CodeConnectionTimeout:
		return 0x06 // TTL expired / timeout
	case CodeProtocolError, CodeUpstreamFailure:
		return 0x01 // general SOCKS server failure
	default:
		return 0x01
	}
}

// HTTPStatusFromError 将内部错误映射为 HTTP 响应状态码。
func HTTPStatusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code := ErrorCodeFrom(err)
	if code == "" {
		code = ClassifyNetError(err)
	}
	switch code {
	case CodeAuthFailure:
		return http.StatusProxyAuthRequired
	case CodeConnectionTimeout:
		return http.StatusGatewayTimeout
	case CodeConnectionRefused, CodeDNSFailure, CodeNetworkUnreachable, CodeTargetUnreachable, CodeUpstreamFailure, CodeProtocolError:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

// HTTPStatusText 返回状态码文本，兼容标准库未覆盖的自定义状态码。
func HTTPStatusText(code int) string {
	text := http.StatusText(code)
	if text != "" {
		return text
	}
	return "Error"
}
