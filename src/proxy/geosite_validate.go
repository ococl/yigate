package proxy

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// forbiddenGeoSiteRoots 是 GeoSite 数据路径绝不能指向的系统目录。
var forbiddenGeoSiteRoots = []string{"/etc", "/proc", "/sys", "/dev", "/boot", "/root"}

// metadataDomainSuffixes 是云厂商元数据服务的域名别名。
// 经上游转发时网关看不到最终 IP，仅靠 IP 段无法拦住这些别名，
// 因此在目标策略里按域名后缀一并拒绝。
var metadataDomainSuffixes = []string{
	"metadata.google.internal",
	"metadata.goog",
	"metadata.aws",
	"instance-data",
	"metadata.azure.internal",
	"metadata.packet.net",
}

func isMetadataDomain(host string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	for _, suffix := range metadataDomainSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// isLocalOnlyIP 判断是否为本机/链路本地/多播地址。
// 与 isProtectedIP 不同，这里不拒绝 RFC1918 与 ULA：
// 内网代理上游、多节点 GeoSite 同步都依赖私网地址，属于合法部署形态。
func isLocalOnlyIP(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast())
}

// ValidateGeoSiteUpdateURL 校验 GeoSite 更新地址。
// 管理 API 可修改该地址，若不限制主机，等于给持管理令牌者一个可用的 SSRF 入口。
// 内网主机名（多节点同步场景）允许，回环、未指定、链路本地与元数据别名禁止。
func ValidateGeoSiteUpdateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "${") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("routing.geosite.update_url is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("routing.geosite.update_url must be an HTTP(S) URL")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("routing.geosite.update_url must include a host")
	}
	return validateRemoteHost(host)
}

// validateRemoteHost 校验允许出站的远程主机字面量。
func validateRemoteHost(host string) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("host %q is a loopback alias", host)
	}
	if isMetadataDomain(lower) {
		return fmt.Errorf("host %q is a cloud metadata alias", host)
	}
	if ip := parseIPLiteral(host); ip != nil && isLocalOnlyIP(ip) {
		return fmt.Errorf("host %q is a local or link-local address", host)
	}
	return nil
}

// ValidateUpstreamHost 校验上游代理目标的主机，防止把网关配置成内网/元数据跳板。
// 私网上游（RFC1918 / ULA）是合法的内网部署形态，不在拒绝范围内。
func ValidateUpstreamHost(host string) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.Contains(host, "${") {
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("host %q is a loopback alias", host)
	}
	if isMetadataDomain(lower) {
		return fmt.Errorf("host %q is a cloud metadata alias", host)
	}
	// 本机与私网上游都是常见合法部署（本地 SOCKS5、内网二级代理），
	// 只拒绝未指定、链路本地与多播地址以及云元数据别名。
	if ip := parseIPLiteral(host); ip != nil && isUnsafeUpstreamIP(ip) {
		return fmt.Errorf("host %q is not a valid upstream address", host)
	}
	return nil
}

func isUnsafeUpstreamIP(ip net.IP) bool {
	return ip != nil && (ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast())
}

// RedactURL 去掉 userinfo、查询参数与片段，用于日志、错误信息和管理 API 回显。
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.ForceQuery = false
	return u.String()
}

// ValidateGeoSiteDataPath 限制运行时配置 API 可设置的数据路径：
// 只允许落在配置文件所在目录或其父目录内（默认 ../data/geosite.dat），
// 并拒绝指向系统敏感目录或目录本身，避免管理 API 变成任意文件读写入口。
func ValidateGeoSiteDataPath(cfgPath, candidate string) error {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || strings.Contains(candidate, "${") {
		return nil
	}
	base, err := filepath.Abs(filepath.Dir(cfgPath))
	if err != nil {
		return fmt.Errorf("routing.geosite.path: resolve config dir: %w", err)
	}
	target := candidate
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	for _, root := range forbiddenGeoSiteRoots {
		if target == root || strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("routing.geosite.path must not point to a protected system directory")
		}
	}
	root := filepath.Dir(base)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("routing.geosite.path must stay within the runtime data directory")
	}
	if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
		return fmt.Errorf("routing.geosite.path must be a regular file")
	}
	return nil
}
