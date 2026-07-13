package proxy

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Socks5/HTTP 等代理命令最终会访问客户端指定的任意目标地址。
// DestinationPolicy 在路由决策之外提供一层统一的「目标访问授权」，
// 默认拒绝回环、私网、链路本地、多播、未指定地址与云元数据等敏感地址，
// 防止 SSRF 与内网横向访问。
type DestinationPolicyConfig struct {
	// ProtectPrivate 默认拒绝受保护地址段（loopback / RFC1918 / ULA /
	// link-local / multicast / unspecified / 云元数据）。
	ProtectPrivate bool `yaml:"protect_private"`
	// DefaultAction 对非受保护、未被显式 deny 的目标采取的默认动作。
	// 取值 allow 或 deny；deny 时仅 allow_* 列表中的目标可访问。
	DefaultAction string `yaml:"default_action"`
	// DenyCIDRs 始终拒绝的 CIDR。
	DenyCIDRs []string `yaml:"deny_cidrs"`
	// AllowCIDRs 当 DefaultAction=deny 时允许访问的 CIDR。
	AllowCIDRs []string `yaml:"allow_cidrs"`
	// DenyDomains 始终拒绝的域名（后缀匹配）。
	DenyDomains []string `yaml:"deny_domains"`
	// AllowDomains 当 DefaultAction=deny 时允许访问的域名（后缀匹配）。
	AllowDomains []string `yaml:"allow_domains"`
	// DenyPorts 始终拒绝的端口。
	DenyPorts []int `yaml:"deny_ports"`
	// AllowPorts 当 DefaultAction=deny 时允许访问的端口。
	AllowPorts []int `yaml:"allow_ports"`
}

// DestinationPolicy 是编译后的目标访问策略。
type DestinationPolicy struct {
	protectPrivate bool
	defaultAction  string
	denyCIDRs      []*net.IPNet
	allowCIDRs     []*net.IPNet
	denyDomains    []string
	allowDomains   []string
	denyPorts      map[int]struct{}
	allowPorts     map[int]struct{}
}

// protectedRanges 是无论配置如何都默认拒绝的地址段。
var protectedRanges = []*net.IPNet{
	mustCIDR("127.0.0.0/8"),    // IPv4 loopback
	mustCIDR("::1/128"),        // IPv6 loopback
	mustCIDR("0.0.0.0/8"),      // unspecified
	mustCIDR("::/128"),         // unspecified (IPv6)
	mustCIDR("10.0.0.0/8"),     // RFC1918
	mustCIDR("172.16.0.0/12"),  // RFC1918
	mustCIDR("192.168.0.0/16"), // RFC1918
	mustCIDR("fc00::/7"),       // IPv6 ULA
	mustCIDR("169.254.0.0/16"), // IPv4 link-local / 云元数据 169.254.169.254
	mustCIDR("fe80::/10"),      // IPv6 link-local
	mustCIDR("224.0.0.0/4"),    // IPv4 multicast
	mustCIDR("ff00::/8"),       // IPv6 multicast
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid protected CIDR %q: %v", s, err))
	}
	return n
}

// NewDestinationPolicy 校验并编译目标访问策略。
func NewDestinationPolicy(cfg DestinationPolicyConfig) (*DestinationPolicy, error) {
	if cfg.DefaultAction != "" && cfg.DefaultAction != ActionAllow && cfg.DefaultAction != ActionDeny {
		return nil, fmt.Errorf("destination_policy.default_action must be allow or deny, got %q", cfg.DefaultAction)
	}
	defaultAction := cfg.DefaultAction
	if defaultAction == "" {
		defaultAction = ActionAllow
	}
	p := &DestinationPolicy{
		protectPrivate: cfg.ProtectPrivate,
		defaultAction:  defaultAction,
		denyPorts:      toPortSet(cfg.DenyPorts),
		allowPorts:     toPortSet(cfg.AllowPorts),
		denyDomains:    normalizeDomains(cfg.DenyDomains),
		allowDomains:   normalizeDomains(cfg.AllowDomains),
	}
	for _, c := range cfg.DenyCIDRs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			return nil, fmt.Errorf("destination_policy.deny_cidrs: %w", err)
		}
		p.denyCIDRs = append(p.denyCIDRs, n)
	}
	for _, c := range cfg.AllowCIDRs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			return nil, fmt.Errorf("destination_policy.allow_cidrs: %w", err)
		}
		p.allowCIDRs = append(p.allowCIDRs, n)
	}
	return p, nil
}

func toPortSet(ports []int) map[int]struct{} {
	set := make(map[int]struct{}, len(ports))
	for _, p := range ports {
		if p < 0 || p > 65535 {
			continue
		}
		set[p] = struct{}{}
	}
	return set
}

func normalizeDomains(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimPrefix(v, ".")
		v = strings.TrimSuffix(v, ".")
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func isProtectedIP(ip net.IP) bool {
	for _, n := range protectedRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Check 校验单个目标。host 为域名或 IP 字符串，ip 为已解析的 IP；
// 当目标为域名且尚未解析时 ip 可为 nil（仅做域名/端口校验）。
func (p *DestinationPolicy) Check(host string, port int, ip net.IP) error {
	if port > 0 {
		if _, deny := p.denyPorts[port]; deny {
			return deniedErr("port %d is denied", port)
		}
	}

	if ip != nil {
		if p.protectPrivate && isProtectedIP(ip) {
			return deniedErr("address %s is protected and denied by default", ip)
		}
		for _, n := range p.denyCIDRs {
			if n.Contains(ip) {
				return deniedErr("address %s is in deny list", ip)
			}
		}
		if p.defaultAction == ActionDeny {
			if _, allow := p.allowPorts[port]; allow {
				return nil
			}
			allowed := false
			for _, n := range p.allowCIDRs {
				if n.Contains(ip) {
					allowed = true
					break
				}
			}
			if !allowed {
				return deniedErr("address %s is not in allow list", ip)
			}
		}
	}

	if host != "" && net.ParseIP(host) == nil {
		lower := strings.ToLower(strings.Trim(host, "[]"))
		for _, d := range p.denyDomains {
			if domainSuffixMatch(lower, d) {
				return deniedErr("domain %s is denied", lower)
			}
		}
		if p.defaultAction == ActionDeny {
			if _, allow := p.allowPorts[port]; allow {
				return nil
			}
			allowed := false
			for _, d := range p.allowDomains {
				if domainSuffixMatch(lower, d) {
					allowed = true
					break
				}
			}
			if !allowed {
				return deniedErr("domain %s is not allowed", lower)
			}
		}
	}
	return nil
}

// CheckTarget 校验 host:port 形式的目标；未携带端口时跳过端口校验。
func (p *DestinationPolicy) CheckTarget(target string) error {
	if p == nil {
		return nil
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		// 无端口或 IPv6 未加方括号时，尝试直接作为 IP 校验。
		if ip := net.ParseIP(target); ip != nil {
			return p.Check(target, 0, ip)
		}
		return nil
	}
	port := 0
	if pp, ok := parsePort(portStr); ok {
		port = pp
	}
	return p.Check(host, port, net.ParseIP(host))
}

func deniedErr(format string, args ...interface{}) error {
	return NewError(CodeDestinationDenied, fmt.Sprintf(format, args...))
}

// dialDirectChecked 执行直连拨号，并在解析后校验实际 IP，防止 DNS rebinding 绕过策略。
func (r *Router) dialDirectChecked(target string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, WrapError(CodeProtocolError, fmt.Sprintf("invalid target %q", target), err)
	}
	if ip := net.ParseIP(host); ip != nil {
		if r.policy != nil {
			port, _ := parsePort(portStr)
			if err := r.policy.Check(host, port, ip); err != nil {
				return nil, err
			}
		}
		return DialTargetTimeout(target, ProxyDialTimeout())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, ClassifyDialError(target, err)
	}
	var lastErr error
	for _, ip := range preferIPv4(addrs) {
		if r.policy != nil {
			port, _ := parsePort(portStr)
			if err := r.policy.Check(host, port, ip); err != nil {
				lastErr = err
				continue
			}
		}
		conn, derr := DialTargetTimeout(net.JoinHostPort(ip.String(), portStr), ProxyDialTimeout())
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = NewError(CodeDestinationDenied, "no allowed address for "+target)
	}
	return nil, lastErr
}
