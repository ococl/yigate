package proxy

import (
	"errors"
	"fmt"
	"net"
)

// ErrorCategory 表示拨号错误的归因类别。
type ErrorCategory string

const (
	// ErrCategoryUpstream：上游连接、TLS、认证或协议本身的失败。
	// 此类错误应推进上游健康状态转换。
	ErrCategoryUpstream ErrorCategory = "upstream"

	// ErrCategoryTarget：目标不可达、目标拒绝或目标超时。
	// 此类错误不应影响上游健康状态。
	ErrCategoryTarget ErrorCategory = "target"

	// ErrCategoryCapability：上游不支持特定命令（如 BIND/UDP）。
	// 仅影响对应能力，不关闭整个节点。
	ErrCategoryCapability ErrorCategory = "capability"

	// ErrCategoryCanceled：调用方主动取消。
	ErrCategoryCanceled ErrorCategory = "canceled"

	// ErrCategoryUnknown：无法确定归因的未知错误。
	// 保守处理，不直接推进熔断。
	ErrCategoryUnknown ErrorCategory = "unknown"
)

// DialError 包装拨号过程中的结构化错误信息。
type DialError struct {
	Category ErrorCategory // 错误归因类别
	Err      error         // 原始错误
	Code     int           // HTTP 状态码或 SOCKS5 REP 码（0 表示不适用）
	Phase    string        // 失败阶段：dial / tls / auth / connect / request
}

func (e *DialError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s:%s] %v", e.Category, e.Phase, e.Err)
	}
	return fmt.Sprintf("[%s:%s] code=%d", e.Category, e.Phase, e.Code)
}

func (e *DialError) Unwrap() error {
	return e.Err
}

// newDialError 创建一个结构化拨号错误。
func newDialError(cat ErrorCategory, phase string, err error) *DialError {
	return &DialError{Category: cat, Phase: phase, Err: err}
}

// newDialErrorCode 创建一个带状态码的结构化拨号错误。
func newDialErrorCode(cat ErrorCategory, phase string, code int, msg string) *DialError {
	return &DialError{Category: cat, Phase: phase, Code: code, Err: errors.New(msg)}
}

// IsUpstreamError 判断错误是否归因于上游本身（连接、TLS、认证、协议）。
func IsUpstreamError(err error) bool {
	var de *DialError
	if errors.As(err, &de) {
		return de.Category == ErrCategoryUpstream
	}
	// 未分类的错误（裸 net.Error 等）默认视为上游错误，
	// 因为拨号到上游的连接失败最可能是上游问题。
	var netErr net.Error
	return errors.As(err, &netErr)
}

// IsTargetError 判断错误是否归因于目标端。
func IsTargetError(err error) bool {
	var de *DialError
	if errors.As(err, &de) {
		return de.Category == ErrCategoryTarget
	}
	return false
}

// IsCapabilityError 判断错误是否归因于上游能力不支持。
func IsCapabilityError(err error) bool {
	var de *DialError
	if errors.As(err, &de) {
		return de.Category == ErrCategoryCapability
	}
	return false
}

// classifyHTTPConnectStatus 根据 HTTP CONNECT 响应状态码分类错误。
func classifyHTTPConnectStatus(statusCode int, status string) *DialError {
	switch {
	case statusCode == 401 || statusCode == 407:
		return newDialErrorCode(ErrCategoryUpstream, "auth", statusCode, "upstream CONNECT auth failed: "+status)
	case statusCode == 403:
		return newDialErrorCode(ErrCategoryUpstream, "connect", statusCode, "upstream CONNECT forbidden: "+status)
	case statusCode == 502 || statusCode == 504:
		// 502 Bad Gateway / 504 Gateway Timeout 通常表示上游无法到达目标。
		return newDialErrorCode(ErrCategoryTarget, "connect", statusCode, "upstream CONNECT target error: "+status)
	case statusCode == 503:
		return newDialErrorCode(ErrCategoryUpstream, "connect", statusCode, "upstream CONNECT unavailable: "+status)
	case statusCode/100 == 5:
		return newDialErrorCode(ErrCategoryUpstream, "connect", statusCode, "upstream CONNECT server error: "+status)
	case statusCode/100 == 4:
		return newDialErrorCode(ErrCategoryUpstream, "connect", statusCode, "upstream CONNECT client error: "+status)
	default:
		return newDialErrorCode(ErrCategoryUnknown, "connect", statusCode, "upstream CONNECT failed: "+status)
	}
}

// classifySOCKS5Reply 根据 SOCKS5 REP 码分类错误。
func classifySOCKS5Reply(rep byte, cmd byte) *DialError {
	cmdName := "connect"
	switch cmd {
	case 0x02:
		cmdName = "bind"
	case 0x03:
		cmdName = "udp_associate"
	}
	switch rep {
	case 0x01:
		// general SOCKS server failure → 上游
		return newDialErrorCode(ErrCategoryUpstream, "request", int(rep),
			fmt.Sprintf("socks5 %s general server failure: 0x%02x", cmdName, rep))
	case 0x07:
		// command not supported → 能力
		return newDialErrorCode(ErrCategoryCapability, "request", int(rep),
			fmt.Sprintf("socks5 %s not supported: 0x%02x", cmdName, rep))
	case 0x08:
		// address type not supported → 能力
		return newDialErrorCode(ErrCategoryCapability, "request", int(rep),
			fmt.Sprintf("socks5 %s address type not supported: 0x%02x", cmdName, rep))
	case 0x02, 0x03, 0x04, 0x05, 0x06:
		// connection not allowed / network unreachable / host unreachable /
		// connection refused / TTL expired → 目标
		return newDialErrorCode(ErrCategoryTarget, "request", int(rep),
			fmt.Sprintf("socks5 %s target unreachable: 0x%02x", cmdName, rep))
	default:
		return newDialErrorCode(ErrCategoryUnknown, "request", int(rep),
			fmt.Sprintf("socks5 %s protocol error: 0x%02x", cmdName, rep))
	}
}
