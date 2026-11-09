package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"yigate/proxy"
)

// generateManagementToken 生成一个高强度随机管理令牌（32 字节，十六进制 64 字符）。
// 用于首次生成默认配置时作为 WebUI 登录密码。
func generateManagementToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate management token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// TLSConfig 配置代理端口的 TLS 服务端终止。
// 若 CertFile/KeyFile 均非空，则代理端口会升级为 TLS 监听；
// 若 ClientCAFile 非空，则强制要求客户端提供 mTLS 证书。
type TLSConfig struct {
	CertFile     string `yaml:"cert_file"`
	KeyFile      string `yaml:"key_file"`
	ClientCAFile string `yaml:"client_ca_file"`
}

// ListenerConfig 单个代理监听端口配置。
// 多监听端口策略下，每个 listener 可拥有独立的认证、访问控制、SOCKS5 命令与 TLS 配置。
type ListenerConfig struct {
	Listen         string                    `yaml:"listen"`
	TLS            TLSConfig                 `yaml:"tls"`
	Users          map[string]string         `yaml:"users"`
	AccessControl  proxy.AccessControlConfig `yaml:"access_control"`
	Socks5         Socks5Config              `yaml:"socks5"`
	MaxConnections int                       `yaml:"max_connections"`
	TProxy         proxy.TProxyConfig        `yaml:"tproxy"`
}

// Config 代理服务器配置
type Config struct {
	Listeners           []ListenerConfig `yaml:"listeners"`
	Listen              string           `yaml:"listen"`
	Management          string           `yaml:"management"`
	ManagementAuthToken string           `yaml:"management_auth_token"`
	// 下面两个开关是显式的风险确认：默认关闭，只有确实需要匿名代理或访问私网目标时才打开。
	AllowAnonymousProxy bool                          `yaml:"allow_anonymous_proxy"`
	AllowPrivateTargets bool                          `yaml:"allow_private_targets"`
	Users               map[string]string             `yaml:"users"`
	Upstreams           []proxy.UpstreamConfig        `yaml:"upstreams"`
	Routing             proxy.RoutingConfig           `yaml:"routing"`
	DestinationPolicy   proxy.DestinationPolicyConfig `yaml:"destination_policy"`
	Timeouts            proxy.TimeoutConfig           `yaml:"timeouts"`
	Log                 proxy.LogConfig               `yaml:"log"`
	CheckInterval       int                           `yaml:"check_interval"`
	MaxConnections      int                           `yaml:"max_connections"`
	AccessControl       proxy.AccessControlConfig     `yaml:"access_control"`
	Socks5              Socks5Config                  `yaml:"socks5"`
	EnablePprof         bool                          `yaml:"enable_pprof"`
	WebUI               *WebUIConfig                  `yaml:"webui"`
	TLS                 TLSConfig                     `yaml:"tls"`
	DNS                 proxy.DNSConfig               `yaml:"dns"`
	TrafficAudit        proxy.TrafficAuditConfig      `yaml:"traffic_audit"`
	TProxy              proxy.TProxyConfig            `yaml:"tproxy"`
}

// Socks5Config 控制 SOCKS5 可选命令的可用性，默认均禁用。
type Socks5Config struct {
	AllowBind         bool `yaml:"allow_bind"`
	AllowUDPAssociate bool `yaml:"allow_udp_associate"`
}

// WebUIConfig 控制是否启用内置 Web 管理界面。
// 二进制已内置前端构建产物，可通过此开关关闭（例如使用独立部署的前端）。
// 配置项未设置时默认启用。
type WebUIConfig struct {
	Enabled bool `yaml:"enabled"`
}

const (
	defaultMaxConnections = 1024
	maxUpstreamWeight     = 10000
)

func defaultConfig() *Config {
	return &Config{
		Listen:    "127.0.0.1:8891",
		Users:     make(map[string]string),
		Upstreams: []proxy.UpstreamConfig{},
		Routing: proxy.RoutingConfig{
			Final: "auto",
			Auto: proxy.AutoConfig{
				Mode:          "direct_first",
				DirectTimeout: proxy.Duration(1500 * time.Millisecond),
				CacheTTL:      proxy.Duration(time.Hour),
			},
			GeoSite: proxy.GeoSiteConfig{
				// 路径由系统托管：未显式配置时解析为配置文件同目录下的 geosite.dat。
				AutoUpdate:     true,
				UpdateURL:      "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
				UpdateInterval: proxy.Duration(24 * time.Hour),
				UseUpstream:    true,
			},
			Socks5HRedirect: proxy.Socks5HConfig{Enabled: true},
		},
		DestinationPolicy: proxy.DestinationPolicyConfig{
			ProtectPrivate: true,
			DefaultAction:  "allow",
		},
		// DNS 默认值与文档/示例保持一致：system 解析、失败回退系统 DNS、5s 超时、IPv4 优先。
		DNS: proxy.DNSConfig{
			Type:         "system",
			Fallback:     true,
			Timeout:      proxy.DefaultDNSTimeout(),
			IPPrecedence: proxy.IPPrecedenceIPv4First,
		},
		TrafficAudit: proxy.TrafficAuditConfig{
			QuotaWindow:   proxy.Duration(time.Hour),
			RetentionDays: proxy.DefaultTrafficRetentionDays,
		},
		Log:            proxy.DefaultLogConfig(),
		CheckInterval:  30,
		MaxConnections: defaultMaxConnections,
		Socks5: Socks5Config{
			AllowBind:         false,
			AllowUDPAssociate: false,
		},
	}
}

// LoadConfig 加载并校验 YAML 配置文件。
// 启动阶段对未展开的环境变量保持严格：缺失即失败，避免带着半份凭据上线。
func LoadConfig(path string) (*Config, error) {
	return loadConfig(path, nil)
}

// LoadConfigForReload 用于热加载：当某个密钥字段引用的环境变量已不可用时，
// 沿用当前运行时已经生效的值，避免管理接口保存或 GeoSite 更新后因为无关字段整体失败。
// previous 为空时行为与 LoadConfig 完全一致。
func LoadConfigForReload(path string, previous *Config) (*Config, error) {
	return loadConfig(path, previous)
}

func loadConfig(path string, previous *Config) (*Config, error) {
	cfg := defaultConfig()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if cfg.Upstreams == nil {
		cfg.Upstreams = []proxy.UpstreamConfig{}
	}
	if err := expandConfigEnv(cfg, previous); err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(path)
	proxy.ResolveRoutingPaths(&cfg.Routing, baseDir)
	proxy.ResolveGeoSiteDataPath(&cfg.Routing.GeoSite, baseDir)
	resolveTLSPaths(&cfg.TLS, baseDir)
	proxy.NormalizeGeoSiteConfig(&cfg.Routing.GeoSite)
	proxy.NormalizeLogConfig(&cfg.Log)
	if err := normalizeListeners(cfg, baseDir); err != nil {
		return nil, err
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// normalizeListeners 将旧版单 listen 配置转换为多 listener 模型。
// 若 listeners 为空，则使用全局 listen/users/access_control/socks5/tls/max_connections 生成一个默认 listener。
func normalizeListeners(cfg *Config, baseDir string) error {
	if len(cfg.Listeners) == 0 {
		users := cfg.Users
		if users == nil {
			users = make(map[string]string)
		}
		cfg.Listeners = []ListenerConfig{{
			Listen:         cfg.Listen,
			TLS:            cfg.TLS,
			Users:          users,
			AccessControl:  cfg.AccessControl,
			Socks5:         cfg.Socks5,
			MaxConnections: cfg.MaxConnections,
			TProxy:         cfg.TProxy,
		}}
		return nil
	}

	for i := range cfg.Listeners {
		// 顶层 users/access_control 在多 listener 模式下作为“默认值”：
		// 仅当该 listener 未显式配置时继承，避免全局用户被静默忽略导致端口变成匿名代理。
		// 显式写 users: {} 表示该端口匿名，不继承。
		if cfg.Listeners[i].Users == nil {
			if len(cfg.Users) > 0 {
				users := make(map[string]string, len(cfg.Users))
				for name, pass := range cfg.Users {
					users[name] = pass
				}
				cfg.Listeners[i].Users = users
			} else {
				cfg.Listeners[i].Users = make(map[string]string)
			}
		}
		if isZeroAccessControl(cfg.Listeners[i].AccessControl) {
			cfg.Listeners[i].AccessControl = cfg.AccessControl
		}
		resolveTLSPaths(&cfg.Listeners[i].TLS, baseDir)
	}
	return nil
}

// isZeroAccessControl 判断访问控制是否完全未配置（用于继承顶层默认值）。
func isZeroAccessControl(cfg proxy.AccessControlConfig) bool {
	return len(cfg.AllowedClients) == 0 &&
		cfg.RateLimit.ConnectionsPerSecond == 0 &&
		cfg.RateLimit.Burst == 0 &&
		cfg.MaxConnsPerUser == 0
}

func expandConfigEnv(cfg *Config, previous *Config) error {
	if cfg.ManagementAuthToken != "" {
		fallback := ""
		if previous != nil {
			fallback = previous.ManagementAuthToken
		}
		expanded, err := expandSecretEnv("management_auth_token", cfg.ManagementAuthToken, fallback)
		if err != nil {
			return err
		}
		cfg.ManagementAuthToken = expanded
		if cfg.ManagementAuthToken == "" {
			return fmt.Errorf("management_auth_token must not be empty")
		}
	}
	for user, pass := range cfg.Users {
		if strings.TrimSpace(user) == "" {
			return fmt.Errorf("users contains empty username")
		}
		fallback := ""
		if previous != nil {
			fallback = previous.Users[user]
		}
		expanded, err := expandSecretEnv(fmt.Sprintf("users[%q]", user), pass, fallback)
		if err != nil {
			return err
		}
		if expanded == "" {
			return fmt.Errorf("users[%q] password must not be empty", user)
		}
		cfg.Users[user] = expanded
	}
	for i := range cfg.Upstreams {
		fallback := previousUpstreamURL(previous, i, cfg.Upstreams[i].Name)
		expanded, err := expandSecretEnv(fmt.Sprintf("upstreams[%d].url", i), cfg.Upstreams[i].URL, fallback)
		if err != nil {
			return err
		}
		cfg.Upstreams[i].URL = expanded
	}
	// GeoSite 同步令牌支持 ${ENV} 注入，避免在配置文件中保存明文。
	for _, item := range []struct {
		name     string
		value    *string
		fallback string
	}{
		{"routing.geosite.update_token", &cfg.Routing.GeoSite.UpdateToken, previousGeoSiteToken(previous, "update")},
		{"routing.geosite.sync_token", &cfg.Routing.GeoSite.SyncToken, previousGeoSiteToken(previous, "sync")},
	} {
		if strings.TrimSpace(*item.value) == "" {
			continue
		}
		expanded, err := expandSecretEnv(item.name, *item.value, item.fallback)
		if err != nil {
			return err
		}
		*item.value = expanded
	}
	if err := expandTLSConfigEnv(&cfg.TLS, previousTLS(previous)); err != nil {
		return err
	}
	for i := range cfg.Listeners {
		if err := expandListenerUsersEnv(&cfg.Listeners[i], i, previousListener(previous, i)); err != nil {
			return err
		}
		if err := expandTLSConfigEnv(&cfg.Listeners[i].TLS, previousListenerTLS(previous, i)); err != nil {
			return fmt.Errorf("listeners[%d].tls: %w", i, err)
		}
	}
	return nil
}

func previousTLS(previous *Config) *TLSConfig {
	if previous == nil {
		return nil
	}
	return &previous.TLS
}

func previousListener(previous *Config, idx int) *ListenerConfig {
	if previous == nil || idx < 0 || idx >= len(previous.Listeners) {
		return nil
	}
	return &previous.Listeners[idx]
}

func previousListenerTLS(previous *Config, idx int) *TLSConfig {
	if lc := previousListener(previous, idx); lc != nil {
		return &lc.TLS
	}
	return nil
}

func previousUpstreamURL(previous *Config, idx int, name string) string {
	if previous == nil {
		return ""
	}
	name = strings.TrimSpace(name)
	if name != "" {
		for _, up := range previous.Upstreams {
			if strings.TrimSpace(up.Name) == name {
				return up.URL
			}
		}
	}
	if idx >= 0 && idx < len(previous.Upstreams) {
		return previous.Upstreams[idx].URL
	}
	return ""
}

func previousGeoSiteToken(previous *Config, kind string) string {
	if previous == nil {
		return ""
	}
	if kind == "update" {
		return previous.Routing.GeoSite.UpdateToken
	}
	return previous.Routing.GeoSite.SyncToken
}

// expandSecretEnv 展开密钥字段；热加载时若环境变量缺失则沿用上次生效值。
// 启动阶段没有 previous，缺失仍然直接报错。
func expandSecretEnv(field, value, fallback string) (string, error) {
	expanded, err := expandEnvStrict(field, value)
	if err == nil {
		return strings.TrimSpace(expanded), nil
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "", err
	}
	proxy.Logger().Warnw(
		"environment variable unavailable during config reload, keeping previous value",
		"field", field, "error", err,
	)
	return fallback, nil
}

func expandListenerUsersEnv(lc *ListenerConfig, idx int, previous *ListenerConfig) error {
	for user, pass := range lc.Users {
		if strings.TrimSpace(user) == "" {
			return fmt.Errorf("listeners[%d].users contains empty username", idx)
		}
		fallback := ""
		if previous != nil {
			fallback = previous.Users[user]
		}
		expanded, err := expandSecretEnv(fmt.Sprintf("listeners[%d].users[%q]", idx, user), pass, fallback)
		if err != nil {
			return err
		}
		if expanded == "" {
			return fmt.Errorf("listeners[%d].users[%q] password must not be empty", idx, user)
		}
		lc.Users[user] = expanded
	}
	return nil
}

func expandTLSConfigEnv(tlsCfg *TLSConfig, previous *TLSConfig) error {
	type fieldRef struct {
		name  string
		value *string
		prev  string
	}
	refs := []fieldRef{
		{"tls.cert_file", &tlsCfg.CertFile, ""},
		{"tls.key_file", &tlsCfg.KeyFile, ""},
		{"tls.client_ca_file", &tlsCfg.ClientCAFile, ""},
	}
	if previous != nil {
		refs[0].prev = previous.CertFile
		refs[1].prev = previous.KeyFile
		refs[2].prev = previous.ClientCAFile
	}
	for _, ref := range refs {
		if *ref.value == "" {
			continue
		}
		expanded, err := expandSecretEnv(ref.name, *ref.value, ref.prev)
		if err != nil {
			return err
		}
		*ref.value = expanded
		if *ref.value == "" {
			return fmt.Errorf("%s must not be empty", ref.name)
		}
	}
	return nil
}

func expandEnvStrict(field, value string) (string, error) {
	var missing []string
	expanded := os.Expand(value, func(name string) string {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			missing = append(missing, name)
			return ""
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s references unset or empty environment variable %s", field, strings.Join(missing, ","))
	}
	return expanded, nil
}

func resolveTLSPaths(tlsCfg *TLSConfig, baseDir string) {
	if !filepath.IsAbs(tlsCfg.CertFile) && tlsCfg.CertFile != "" {
		tlsCfg.CertFile = filepath.Join(baseDir, tlsCfg.CertFile)
	}
	if !filepath.IsAbs(tlsCfg.KeyFile) && tlsCfg.KeyFile != "" {
		tlsCfg.KeyFile = filepath.Join(baseDir, tlsCfg.KeyFile)
	}
	if !filepath.IsAbs(tlsCfg.ClientCAFile) && tlsCfg.ClientCAFile != "" {
		tlsCfg.ClientCAFile = filepath.Join(baseDir, tlsCfg.ClientCAFile)
	}
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isPlaceholderCredential(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, prefix := range []string{"__replace_", "change-me", "local-admin-token"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// weakCredentialValues 是常见的弱口令/示例令牌。占位符前缀由 isPlaceholderCredential 覆盖。
var weakCredentialValues = map[string]struct{}{
	"admin": {}, "admin123": {}, "password": {}, "pass": {}, "123456": {},
	"12345678": {}, "123456789": {}, "111111": {}, "abc123": {}, "qwerty": {},
	"letmein": {}, "changeme": {}, "secret": {}, "token": {}, "default": {},
	"root": {}, "guest": {}, "test": {}, "test123": {}, "yigate": {},
}

// validateCredentialStrength 拒绝占位符与常见弱口令。
// 空值与尚未展开的 ${ENV} 占位符交给加载阶段处理，这里不重复报错。
func validateCredentialStrength(field, value string) error {
	v := strings.TrimSpace(value)
	if v == "" || containsEnvPlaceholder(v) {
		return nil
	}
	if isPlaceholderCredential(v) {
		return fmt.Errorf("%s still uses a placeholder; replace it with a strong random value", field)
	}
	if _, weak := weakCredentialValues[strings.ToLower(v)]; weak {
		return fmt.Errorf("%s is a commonly used weak credential", field)
	}
	return nil
}

func validateTLSConfig(tlsCfg *TLSConfig) error {
	hasCert := strings.TrimSpace(tlsCfg.CertFile) != ""
	hasKey := strings.TrimSpace(tlsCfg.KeyFile) != ""
	hasClientCA := strings.TrimSpace(tlsCfg.ClientCAFile) != ""

	if hasCert != hasKey {
		return fmt.Errorf("tls.cert_file and tls.key_file must be both provided or both empty")
	}
	if hasClientCA && !hasCert {
		return fmt.Errorf("tls.client_ca_file requires tls.cert_file and tls.key_file")
	}
	if hasCert {
		if !containsEnvPlaceholder(tlsCfg.CertFile) {
			if _, err := os.Stat(tlsCfg.CertFile); err != nil {
				return fmt.Errorf("cert_file %q: %w", tlsCfg.CertFile, err)
			}
		}
		if !containsEnvPlaceholder(tlsCfg.KeyFile) {
			if _, err := os.Stat(tlsCfg.KeyFile); err != nil {
				return fmt.Errorf("key_file %q: %w", tlsCfg.KeyFile, err)
			}
		}
	}
	if hasClientCA && !containsEnvPlaceholder(tlsCfg.ClientCAFile) {
		if _, err := os.Stat(tlsCfg.ClientCAFile); err != nil {
			return fmt.Errorf("client_ca_file %q: %w", tlsCfg.ClientCAFile, err)
		}
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if err := validateTLSConfig(&cfg.TLS); err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	if len(cfg.Listeners) == 0 {
		if strings.TrimSpace(cfg.Listen) == "" {
			return fmt.Errorf("listen must not be empty")
		}
		if !containsEnvPlaceholder(cfg.Listen) {
			if _, err := net.ResolveTCPAddr("tcp", cfg.Listen); err != nil {
				return fmt.Errorf("listen %q is not a valid TCP address: %w", cfg.Listen, err)
			}
		}
	}
	seenListeners := make(map[string]struct{}, len(cfg.Listeners))
	for i, lc := range cfg.Listeners {
		if strings.TrimSpace(lc.Listen) == "" {
			return fmt.Errorf("listeners[%d].listen must not be empty", i)
		}
		if !containsEnvPlaceholder(lc.Listen) {
			if _, err := net.ResolveTCPAddr("tcp", lc.Listen); err != nil {
				return fmt.Errorf("listeners[%d].listen %q is not a valid TCP address: %w", i, lc.Listen, err)
			}
		}
		if _, exists := seenListeners[lc.Listen]; exists {
			return fmt.Errorf("listeners[%d].listen %q is duplicated", i, lc.Listen)
		}
		seenListeners[lc.Listen] = struct{}{}
		if err := validateTLSConfig(&cfg.Listeners[i].TLS); err != nil {
			return fmt.Errorf("listeners[%d].tls: %w", i, err)
		}
		// 非回环监听且既无认证也无客户端白名单时，等于对外开放匿名代理。
		// 显式设置 allow_anonymous_proxy: true 才能绕过这道基线（例如容器内监听 0.0.0.0
		// 但宿主机仅映射回环地址的本地开发模板）。
		if !cfg.AllowAnonymousProxy && !containsEnvPlaceholder(lc.Listen) &&
			!isLoopbackListen(lc.Listen) && len(lc.Users) == 0 && len(cfg.Users) == 0 &&
			len(lc.AccessControl.AllowedClients) == 0 && len(cfg.AccessControl.AllowedClients) == 0 &&
			!lc.TProxy.Enabled {
			return fmt.Errorf("listeners[%d].listen %q is reachable from the network without authentication or a client allowlist; configure users/allowed_clients, bind to loopback, or set allow_anonymous_proxy: true", i, lc.Listen)
		}
		if lc.TProxy.Enabled {
			// 透明代理的客户端不会发送代理认证头，配了 users 只会造成「以为有认证」的开放代理。
			if len(lc.Users) > 0 {
				return fmt.Errorf("listeners[%d].tproxy: transparent proxy cannot enforce proxy authentication; remove users or disable tproxy", i)
			}
			if !proxy.IsTProxySupported() {
				return fmt.Errorf("listeners[%d].tproxy: transparent proxy is only supported on Linux", i)
			}
			if err := validateTProxyMode(lc.TProxy.Mode); err != nil {
				return fmt.Errorf("listeners[%d].tproxy.mode: %w", i, err)
			}
			if lc.TLS.CertFile != "" || lc.TLS.KeyFile != "" {
				return fmt.Errorf("listeners[%d].tproxy: TLS is not supported with transparent proxy", i)
			}
		}
		if _, err := proxy.NewAccessController(lc.AccessControl); err != nil {
			return fmt.Errorf("listeners[%d].access_control: %w", i, err)
		}
		if err := validateAccessControl(lc.AccessControl); err != nil {
			return fmt.Errorf("listeners[%d].access_control: %w", i, err)
		}
		if lc.MaxConnections < 0 {
			return fmt.Errorf("listeners[%d].max_connections must be >= 0", i)
		}
	}

	if cfg.CheckInterval < 0 {
		return fmt.Errorf("check_interval must be >= 0")
	}
	if cfg.MaxConnections < 0 {
		return fmt.Errorf("max_connections must be >= 0")
	}
	if err := proxy.ValidateTimeoutConfig(cfg.Timeouts); err != nil {
		return fmt.Errorf("timeouts: %w", err)
	}
	// 显式关闭私网防护属于高风险决策：保留启动期可观测告警，但不阻断
	// 本地测试与特殊部署；运行时配置 API 另有严格拦截，防止被静默关闭。
	if !cfg.DestinationPolicy.ProtectPrivate {
		proxy.Logger().Warnw("SSRF protection disabled: destination_policy.protect_private is false",
			"allow_private_targets", cfg.AllowPrivateTargets)
	}
	if _, err := proxy.NewDestinationPolicy(cfg.DestinationPolicy); err != nil {
		return fmt.Errorf("destination_policy: %w", err)
	}
	if err := validateRoutingConfig(cfg.Routing); err != nil {
		return err
	}
	if cfg.Management != "" && !containsEnvPlaceholder(cfg.Management) {
		if _, err := net.ResolveTCPAddr("tcp", cfg.Management); err != nil {
			return fmt.Errorf("management %q is not a valid TCP address: %w", cfg.Management, err)
		}
	}
	if cfg.Management != "" && !isLoopbackListen(cfg.Management) && cfg.ManagementAuthToken == "" {
		return fmt.Errorf("management_auth_token is required when management listens on a non-loopback address")
	}
	if cfg.EnablePprof && cfg.ManagementAuthToken == "" {
		// pprof 暴露堆栈、内存与 goroutine，即使只绑回环也不允许匿名读取。
		return fmt.Errorf("management_auth_token is required when enable_pprof is true")
	}
	if err := validateCredentialStrength("management_auth_token", cfg.ManagementAuthToken); err != nil {
		return err
	}
	for username, password := range cfg.Users {
		if err := validateCredentialStrength(fmt.Sprintf("proxy user %q password", username), password); err != nil {
			return err
		}
	}
	for i, lc := range cfg.Listeners {
		for username, password := range lc.Users {
			if err := validateCredentialStrength(fmt.Sprintf("listeners[%d] user %q password", i, username), password); err != nil {
				return err
			}
		}
	}
	if err := proxy.ValidateLogConfig(cfg.Log); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	for i, up := range cfg.Upstreams {
		if err := validateUpstreamConfig(up); err != nil {
			return fmt.Errorf("upstreams[%d]: %w", i, err)
		}
	}

	// 收集有效的上游名称（空名会按 NewUpstreamManager 规则生成 upstream-i）
	upstreamNames := make(map[string]struct{}, len(cfg.Upstreams))
	for i, up := range cfg.Upstreams {
		name := strings.TrimSpace(up.Name)
		if name == "" {
			name = fmt.Sprintf("upstream-%d", i+1)
		}
		if _, exists := upstreamNames[name]; exists {
			return fmt.Errorf("upstreams[%d].name duplicates upstream %q", i, name)
		}
		upstreamNames[name] = struct{}{}
	}

	// 校验路由规则引用的上游名是否存在。
	// 禁用规则不参与匹配，允许它指向暂不存在的上游（例如上游正在重建），
	// 重新启用时仍会走完整校验。
	for i, rule := range cfg.Routing.Rules {
		if rule.Disabled {
			continue
		}
		for j, name := range rule.Upstreams {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := upstreamNames[name]; !ok {
				return fmt.Errorf("routing.rules[%d].upstreams[%d] references unknown upstream %q", i, j, name)
			}
		}
	}

	// 校验 socks5h_redirect 白名单中的上游名是否存在
	for i, name := range cfg.Routing.Socks5HRedirect.Upstreams {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := upstreamNames[name]; !ok {
			return fmt.Errorf("routing.socks5h_redirect.upstreams[%d] references unknown upstream %q", i, name)
		}
	}

	// 规则的类型/动作/正则/CIDR 在加载期一并校验，避免启动后才由路由器报错。
	// 含 ${ENV} 占位符的规则跳过校验（运行时配置 API 读取的是未展开的原始 YAML）。
	for i, rule := range cfg.Routing.Rules {
		if ruleHasEnvPlaceholder(rule) {
			continue
		}
		if err := validateRouteRule(rule, cfg.Upstreams); err != nil {
			return fmt.Errorf("routing.rules[%d]: %w", i, err)
		}
	}

	if _, err := proxy.NewAccessController(cfg.AccessControl); err != nil {
		return fmt.Errorf("access_control: %w", err)
	}
	if err := validateAccessControl(cfg.AccessControl); err != nil {
		return fmt.Errorf("access_control: %w", err)
	}

	if err := proxy.ValidateDNSConfig(cfg.DNS); err != nil {
		return fmt.Errorf("dns: %w", err)
	}

	// 配额即使未启用也校验，避免关闭开关期间写入非法值后无法再次启用。
	if cfg.TrafficAudit.PerUserQuota < 0 {
		return fmt.Errorf("traffic_audit.per_user_quota must be >= 0")
	}
	if cfg.TrafficAudit.PerUpstreamQuota < 0 {
		return fmt.Errorf("traffic_audit.per_upstream_quota must be >= 0")
	}
	if cfg.TrafficAudit.QuotaWindow < 0 {
		return fmt.Errorf("traffic_audit.quota_window must be >= 0")
	}
	if cfg.TrafficAudit.RetentionDays < 0 {
		return fmt.Errorf("traffic_audit.retention_days must be >= 0")
	}
	if cfg.TrafficAudit.RetentionDays > 3650 {
		return fmt.Errorf("traffic_audit.retention_days must be <= 3650")
	}

	return nil
}

// validateTProxyMode 校验透明代理模式（大小写不敏感）。
func validateTProxyMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "redirect", "tproxy":
		return nil
	default:
		return fmt.Errorf("unsupported mode %q, must be \"redirect\" or \"tproxy\"", mode)
	}
}

// validateAccessControl 校验访问控制的数值边界。负数会被运行时当作“不限制”，
// 容易让用户误以为已经生效，因此在配置层直接拒绝。
func validateAccessControl(cfg proxy.AccessControlConfig) error {
	if cfg.RateLimit.ConnectionsPerSecond < 0 {
		return fmt.Errorf("rate_limit.connections_per_second must be >= 0")
	}
	if cfg.RateLimit.Burst < 0 {
		return fmt.Errorf("rate_limit.burst must be >= 0")
	}
	if cfg.MaxConnsPerUser < 0 {
		return fmt.Errorf("max_conns_per_user must be >= 0")
	}
	return nil
}

// validateRoutingConfig 校验路由相关枚举与路径配置。
func validateRoutingConfig(cfg proxy.RoutingConfig) error {
	switch final := strings.ToLower(strings.TrimSpace(cfg.Final)); final {
	case "", proxy.ActionDirect, proxy.ActionProxy, proxy.ActionAuto, proxy.ActionDeny:
	default:
		return fmt.Errorf("routing.final must be direct, proxy, auto, or deny, got %q", cfg.Final)
	}
	switch mode := strings.ToLower(strings.TrimSpace(cfg.Auto.Mode)); mode {
	case "", "direct_first", "proxy_first":
	default:
		return fmt.Errorf("routing.auto.mode must be direct_first or proxy_first, got %q", cfg.Auto.Mode)
	}
	if cfg.Auto.DirectTimeout < 0 {
		return fmt.Errorf("routing.auto.direct_timeout must be >= 0")
	}
	if cfg.Auto.CacheTTL < 0 {
		return fmt.Errorf("routing.auto.cache_ttl must be >= 0")
	}
	if cfg.GeoSite.UpdateInterval <= 0 {
		return fmt.Errorf("routing.geosite.update_interval must be > 0")
	}
	if cfg.GeoSite.AutoUpdate {
		url := strings.TrimSpace(cfg.GeoSite.UpdateURL)
		if url == "" {
			return fmt.Errorf("routing.geosite.update_url must not be empty when auto_update is true")
		}
		// 统一走共享校验：限制协议与目标主机，错误信息中不回显可能带凭据的完整 URL。
		if err := proxy.ValidateGeoSiteUpdateURL(url); err != nil {
			return err
		}
	}
	// 对外提供 GeoSite 同步时必须配置只读令牌，避免数据端点变成匿名可读。
	if cfg.GeoSite.SyncEnabled {
		token := strings.TrimSpace(cfg.GeoSite.SyncToken)
		if token == "" {
			return fmt.Errorf("routing.geosite.sync_token must not be empty when sync_enabled is true")
		}
		if len(token) < 8 {
			return fmt.Errorf("routing.geosite.sync_token must be at least 8 characters")
		}
		if err := validateCredentialStrength("routing.geosite.sync_token", token); err != nil {
			return err
		}
	}
	if token := strings.TrimSpace(cfg.GeoSite.UpdateToken); token != "" {
		if err := validateCredentialStrength("routing.geosite.update_token", token); err != nil {
			return err
		}
	}
	return nil
}

// ruleHasEnvPlaceholder 判断规则中是否仍含未展开的 ${ENV} 占位符。
func ruleHasEnvPlaceholder(rule proxy.RouteRule) bool {
	if containsEnvPlaceholder(rule.Type) || containsEnvPlaceholder(rule.Action) {
		return true
	}
	for _, v := range rule.Values {
		if containsEnvPlaceholder(v) {
			return true
		}
	}
	for _, v := range rule.Upstreams {
		if containsEnvPlaceholder(v) {
			return true
		}
	}
	return false
}

// containsEnvPlaceholder 判断字段值中是否仍保留 ${ENV} 占位符。
// 运行时配置 API 读取的是未展开的原始 YAML，校验时需要跳过这类值的解析。
func containsEnvPlaceholder(v string) bool {
	return strings.Contains(v, "${")
}

// validateUpstreamConfig 校验单个上游配置：URL 结构、权重、类型、负载策略与健康检查。
func validateUpstreamConfig(up proxy.UpstreamConfig) error {
	rawURL := strings.TrimSpace(up.URL)
	if rawURL == "" {
		return fmt.Errorf("url must not be empty")
	}
	if containsEnvPlaceholder(rawURL) {
		// 占位符未展开时无法完整解析：scheme 若为字面量仍做校验，其余部分留待展开后再校验。
		if parts := strings.SplitN(rawURL, "://", 2); len(parts) == 2 && !containsEnvPlaceholder(parts[0]) {
			if !isSupportedUpstreamScheme(parts[0]) {
				return fmt.Errorf("url scheme must be http, https, socks5, or socks5h, got %q", parts[0])
			}
		}
	} else {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("url is invalid: %w", err)
		}
		if !isSupportedUpstreamScheme(parsed.Scheme) {
			return fmt.Errorf("url scheme must be http, https, socks5, or socks5h, got %q", parsed.Scheme)
		}
		if parsed.Host == "" {
			return fmt.Errorf("url must include host:port")
		}
		// 管理 API 可改上游地址，必须拒绝元数据别名与链路本地目标，
		// 否则上游配置就成了访问内网/云元数据的跳板。
		if err := proxy.ValidateUpstreamHost(parsed.Hostname()); err != nil {
			return fmt.Errorf("url host: %w", err)
		}
		if port := parsed.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return fmt.Errorf("url port must be between 1 and 65535, got %q", port)
			}
		}
	}
	if up.Weight < 0 {
		return fmt.Errorf("weight must be >= 0")
	}
	if up.Weight > maxUpstreamWeight {
		return fmt.Errorf("weight must be <= %d", maxUpstreamWeight)
	}
	if err := proxy.ValidateProxyTypes(up.Types); err != nil {
		return fmt.Errorf("types: %w", err)
	}
	if err := proxy.ValidateUpstreamStrategy(up.Strategy); err != nil {
		return fmt.Errorf("strategy: %w", err)
	}
	if err := proxy.ValidateHealthCheckConfig(up.HealthCheck); err != nil {
		return fmt.Errorf("health_check: %w", err)
	}
	return nil
}

func isSupportedUpstreamScheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case proxy.ProxyTypeHTTP, proxy.ProxyTypeHTTPS, proxy.ProxyTypeSocks5, proxy.ProxyTypeSocks5H:
		return true
	default:
		return false
	}
}

// DefaultConfigYAML 返回带有注释的默认配置 YAML 字符串。
// 若 generateToken 为真，则为 management_auth_token 生成一个高强度随机令牌，
// 用于首次启动即开启 WebUI 认证（无需用户手动预设密码）。
// YIGATE_DEFAULT_LISTEN 和 YIGATE_DEFAULT_MANAGEMENT 仅覆盖首次生成配置的监听地址，
// 便于容器显式监听所有接口，不影响已有配置，也不改变原生运行的安全默认值。
func DefaultConfigYAML(generateToken bool) (string, error) {
	cfg := defaultConfig()
	cfg.Management = "127.0.0.1:8890"
	if listen := strings.TrimSpace(os.Getenv("YIGATE_DEFAULT_LISTEN")); listen != "" {
		cfg.Listen = listen
	}
	if management := strings.TrimSpace(os.Getenv("YIGATE_DEFAULT_MANAGEMENT")); management != "" {
		cfg.Management = management
	}
	// 容器内必须监听所有接口，而 Compose 默认只把端口映射到宿主机回环。
	// 默认生成器据此显式声明匿名代理风险确认；手写配置仍受启动期基线约束。
	if !isLoopbackListen(cfg.Listen) {
		cfg.AllowAnonymousProxy = true
	}
	if generateToken {
		token, err := generateManagementToken()
		if err != nil {
			return "", err
		}
		cfg.ManagementAuthToken = token
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	header := "# yigate 默认配置（自动生成）\n" +
		"# 修改后可通过 POST /api/v1/reload 或 SIGHUP 热加载。\n" +
		"# 详见文档：https://cnb.cool/yi.team/yigate\n\n"
	return header + string(data), nil
}

// WriteDefaultConfigIfMissing 若配置文件不存在则写入默认配置。
// 首次生成时会自动注入一个高强度随机 management_auth_token，使 WebUI 默认开启认证。
// 返回 (written bool, generatedToken string, err error)：
//   - written 为 true 表示本次生成了新文件；
//   - generatedToken 仅在 written 为 true 且成功生成随机令牌时非空，供调用方提示用户。
func WriteDefaultConfigIfMissing(path string) (bool, string, error) {
	if _, err := os.Stat(path); err == nil {
		return false, "", nil
	} else if !os.IsNotExist(err) {
		return false, "", err
	}

	content, err := DefaultConfigYAML(true)
	if err != nil {
		return false, "", err
	}
	// 确保父目录存在
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
			return false, "", mkErr
		}
	}
	if wErr := os.WriteFile(path, []byte(content), 0600); wErr != nil {
		return false, "", fmt.Errorf("write default config to %s failed: %w (check that the directory is mounted writable)", path, wErr)
	}
	// 从落盘内容中解析出刚生成的令牌（用户预置场景不会走到这里）。
	var parsed Config
	if uErr := yaml.Unmarshal([]byte(content), &parsed); uErr == nil {
		return true, parsed.ManagementAuthToken, nil
	}
	return true, "", nil
}
