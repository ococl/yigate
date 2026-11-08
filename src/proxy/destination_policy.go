package proxy

import (
	"context"
	"fmt"
	"net"
	"strconv"
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
	// AllowPorts 当 DefaultAction=deny 时额外要求的端口白名单（必须与地址白名单同时满足）。
	AllowPorts []int `yaml:"allow_ports"`
	// ResolveBeforeUpstream 开启后，经上游转发的目标域名先在网关侧解析并逐个校验 IP，
	// 防止域名指向内网/元数据的 SSRF。默认 false：socks5h 等场景刻意让上游解析域名。
	ResolveBeforeUpstream bool `yaml:"resolve_before_upstream"`
}

// DestinationPolicy 是编译后的目标访问策略。
type DestinationPolicy struct {
	protectPrivate       bool
	defaultAction        string
	resolveBeforeUpstream bool
	denyCIDRs            []*net.IPNet
	allowCIDRs           []*net.IPNet
	denyDomains          []string
	allowDomains         []string
	denyPorts            map[int]struct{}
	allowPorts           map[int]struct{}
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
	mustCIDR("100.64.0.0/10"),  // CGNAT
	mustCIDR("192.0.0.0/24"),   // IETF 协议保留
	mustCIDR("198.18.0.0/15"),  // 基准测试保留
	mustCIDR("240.0.0.0/4"),    // IPv4 保留地址
	mustCIDR("2001::/32"),      // Teredo
	mustCIDR("2002::/16"),      // 6to4
	mustCIDR("64:ff9b::/96"),   // NAT64
	mustCIDR("2001:db8::/32"),  // IPv6 文档地址
	// 不加入 ::ffff:0:0/96：Go 的 IPNet.Contains 会把它规范化成 0.0.0.0/0，
	// 导致所有 IPv4 都被误判为受保护；IPv4-mapped 地址已由上面的 IPv4 段覆盖。
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid protected CIDR %q: %v", s, err))
	}
	return n
}

// parseIPLiteral 解析标准 net.ParseIP 不接受、但系统解析器仍会还原为 IPv4 的
// 字面量（十进制整数、十六进制、八进制及 a.b / a.b.c 简写）。这类写法若被
// 当成域名放行，会在解析后指向回环/内网地址，形成 SSRF 绕过。
func parseIPLiteral(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	if host == "" || strings.Contains(host, ":") {
		return nil
	}
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return nil
	}
	nums := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil
		}
		base := 10
		digits := part
		if strings.HasPrefix(strings.ToLower(digits), "0x") {
			base, digits = 16, digits[2:]
		} else if len(digits) > 1 && digits[0] == '0' {
			base, digits = 8, digits[1:]
		}
		v, err := strconv.ParseUint(digits, base, 32)
		if err != nil {
			return nil
		}
		nums[i] = v
	}
	var addr uint32
	switch len(nums) {
	case 1:
		if nums[0] > 0xFFFFFFFF {
			return nil
		}
		addr = uint32(nums[0])
	case 2:
		if nums[0] > 0xFF || nums[1] > 0xFFFFFF {
			return nil
		}
		addr = uint32(nums[0])<<24 | uint32(nums[1])
	case 3:
		if nums[0] > 0xFF || nums[1] > 0xFF || nums[2] > 0xFFFF {
			return nil
		}
		addr = uint32(nums[0])<<24 | uint32(nums[1])<<16 | uint32(nums[2])
	case 4:
		for _, v := range nums {
			if v > 0xFF {
				return nil
			}
		}
		addr = uint32(nums[0])<<24 | uint32(nums[1])<<16 | uint32(nums[2])<<8 | uint32(nums[3])
	default:
		return nil
	}
	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
}

// NewDestinationPolicy 校验并编译目标访问策略。
func NewDestinationPolicy(cfg DestinationPolicyConfig) (*DestinationPolicy, error) {
	defaultAction := strings.ToLower(strings.TrimSpace(cfg.DefaultAction))
	if defaultAction != "" && defaultAction != ActionAllow && defaultAction != ActionDeny {
		return nil, fmt.Errorf("destination_policy.default_action must be allow or deny, got %q", cfg.DefaultAction)
	}
	if defaultAction == "" {
		defaultAction = ActionAllow
	}
	denyPorts, err := toPortSet("deny_ports", cfg.DenyPorts)
	if err != nil {
		return nil, err
	}
	allowPorts, err := toPortSet("allow_ports", cfg.AllowPorts)
	if err != nil {
		return nil, err
	}
	// 严格模式下端口白名单只是附加约束，单独配置会静默拒绝全部流量，直接报错更清晰。
	if defaultAction == ActionDeny && len(allowPorts) > 0 && len(cfg.AllowCIDRs) == 0 && len(cfg.AllowDomains) == 0 {
		return nil, fmt.Errorf("destination_policy.allow_ports requires allow_cidrs or allow_domains when default_action=deny")
	}
	denyDomains := normalizeDomains(cfg.DenyDomains)
	if cfg.ProtectPrivate {
		// 经上游解析时网关看不到最终 IP，元数据服务别名必须在域名层就并入拒绝列表。
		denyDomains = append(denyDomains, metadataDomainSuffixes...)
	}
	p := &DestinationPolicy{
		protectPrivate:        cfg.ProtectPrivate,
		defaultAction:         defaultAction,
		resolveBeforeUpstream: cfg.ResolveBeforeUpstream,
		denyPorts:             denyPorts,
		allowPorts:            allowPorts,
		denyDomains:           denyDomains,
		allowDomains:          normalizeDomains(cfg.AllowDomains),
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

// toPortSet 将端口列表编译为集合。越界端口会直接报错而不是静默丢弃，
// 避免用户以为规则已生效但实际未加载。
func toPortSet(field string, ports []int) (map[int]struct{}, error) {
	set := make(map[int]struct{}, len(ports))
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("%s contains invalid port %d (must be 1-65535)", field, p)
		}
		set[p] = struct{}{}
	}
	return set, nil
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
// deny_* 与 protect_private 永远优先于 allow_*；default_action=deny 时
// allow_ports 是附加约束，且必须同时命中地址或域名白名单。
func (p *DestinationPolicy) Check(host string, port int, ip net.IP) error {
	if p == nil {
		return nil
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	literal := parseIPLiteral(host)
	if ip == nil {
		ip = literal
	}
	isDomain := host != "" && literal == nil

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
	}

	lower := strings.ToLower(host)
	if isDomain {
		for _, d := range p.denyDomains {
			if domainSuffixMatch(lower, d) {
				return deniedErr("domain %s is denied", lower)
			}
		}
	}

	if p.defaultAction != ActionDeny {
		return nil
	}

	if len(p.allowPorts) > 0 {
		if _, ok := p.allowPorts[port]; !ok {
			return deniedErr("port %d is not in allow list", port)
		}
	}

	allowed := false
	if ip != nil {
		for _, n := range p.allowCIDRs {
			if n.Contains(ip) {
				allowed = true
				break
			}
		}
	}
	// 即使调用方已提供解析后的 IP，域名白名单仍然参与判断。
	if !allowed && isDomain {
		for _, d := range p.allowDomains {
			if domainSuffixMatch(lower, d) {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return deniedErr("target %s is not in allow list", host)
	}
	return nil
}

// ShouldResolveBeforeUpstream 表示经上游转发前是否需要网关侧预解析并校验目标 IP。
func (p *DestinationPolicy) ShouldResolveBeforeUpstream() bool {
	return p != nil && p.resolveBeforeUpstream
}

// CheckTarget 校验 host:port 形式的目标；未携带端口时按裸 host 校验，不再静默放行。
func (p *DestinationPolicy) CheckTarget(target string) error {
	if p == nil {
		return nil
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(target))
	if err != nil {
		return p.Check(target, 0, nil)
	}
	port := 0
	if pp, ok := parsePort(portStr); ok {
		port = pp
	}
	return p.Check(host, port, nil)
}

func deniedErr(format string, args ...interface{}) error {
	return NewError(CodeDestinationDenied, fmt.Sprintf(format, args...))
}

// lookupIPAddr 使用路由器关联的 DNS 解析器解析域名。
// 若解析器为 nil 则回退到系统默认解析器。
func (r *Router) lookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r != nil && r.resolver != nil {
		return r.resolver.LookupIPAddr(ctx, host)
	}
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// dialDirectChecked 执行直连拨号，并在解析后校验实际 IP，防止 DNS rebinding 绕过策略。
func (r *Router) dialDirectChecked(target string) (net.Conn, error) {
	return r.dialDirectCheckedTimeout(target, ProxyDialTimeout())
}

// dialDirectCheckedTimeout 与 dialDirectChecked 行为一致，但允许调用方指定拨号超时。
// 它在 DNS 解析后逐个校验实际 IP，防止通过 DNS rebinding 绕过 DestinationPolicy，
// 这也是直连转发（普通 HTTP 代理请求）复用同一防护路径的关键。
func (r *Router) dialDirectCheckedTimeout(target string, dialTimeout time.Duration) (net.Conn, error) {
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
		return DialTargetTimeout(target, dialTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := r.lookupIPAddr(ctx, host)
	if err != nil {
		return nil, ClassifyDialError(target, err)
	}
	var lastErr error
	for _, ip := range sortAddrsByPrecedence(addrs, r.ipPrecedence) {
		if r.policy != nil {
			port, _ := parsePort(portStr)
			if err := r.policy.Check(host, port, ip); err != nil {
				lastErr = err
				continue
			}
		}
		conn, derr := DialTargetTimeout(net.JoinHostPort(ip.String(), portStr), dialTimeout)
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
