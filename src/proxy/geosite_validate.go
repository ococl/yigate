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

// ValidateGeoSiteUpdateURL 校验 GeoSite 更新地址。
// 管理 API 可修改该地址，若不限制主机，等于给持管理令牌者一个可用的 SSRF 入口。
// 内网主机名（多节点同步场景）允许，回环、未指定与链路本地地址禁止。
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
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("routing.geosite.update_url must not target loopback host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("routing.geosite.update_url must not target local or link-local address")
		}
	}
	return nil
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
