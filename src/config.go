package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"yigate/proxy"
)

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
	Listeners           []ListenerConfig              `yaml:"listeners"`
	Listen              string                        `yaml:"listen"`
	Management          string                        `yaml:"management"`
	ManagementAuthToken string                        `yaml:"management_auth_token"`
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
				AutoUpdate:     true,
				UpdateURL:      "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
				UpdateInterval: proxy.Duration(24 * time.Hour),
			},
			Socks5HRedirect: proxy.Socks5HConfig{Enabled: true},
		},
		DestinationPolicy: proxy.DestinationPolicyConfig{
			ProtectPrivate: true,
			DefaultAction:  "allow",
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
func LoadConfig(path string) (*Config, error) {
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
	if err := expandConfigEnv(cfg); err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(path)
	proxy.ResolveRoutingPaths(&cfg.Routing, baseDir)
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
		if cfg.Listeners[i].Users == nil {
			cfg.Listeners[i].Users = make(map[string]string)
		}
		resolveTLSPaths(&cfg.Listeners[i].TLS, baseDir)
	}
	return nil
}

func expandConfigEnv(cfg *Config) error {
	if cfg.ManagementAuthToken != "" {
		expanded, err := expandEnvStrict("management_auth_token", cfg.ManagementAuthToken)
		if err != nil {
			return err
		}
		cfg.ManagementAuthToken = strings.TrimSpace(expanded)
		if cfg.ManagementAuthToken == "" {
			return fmt.Errorf("management_auth_token must not be empty")
		}
	}
	for user, pass := range cfg.Users {
		if strings.TrimSpace(user) == "" {
			return fmt.Errorf("users contains empty username")
		}
		expanded, err := expandEnvStrict(fmt.Sprintf("users[%q]", user), pass)
		if err != nil {
			return err
		}
		expanded = strings.TrimSpace(expanded)
		if expanded == "" {
			return fmt.Errorf("users[%q] password must not be empty", user)
		}
		cfg.Users[user] = expanded
	}
	for i := range cfg.Upstreams {
		expanded, err := expandEnvStrict(fmt.Sprintf("upstreams[%d].url", i), cfg.Upstreams[i].URL)
		if err != nil {
			return err
		}
		cfg.Upstreams[i].URL = strings.TrimSpace(expanded)
	}
	if err := expandTLSConfigEnv(&cfg.TLS); err != nil {
		return err
	}
	for i := range cfg.Listeners {
		if err := expandListenerUsersEnv(&cfg.Listeners[i], i); err != nil {
			return err
		}
		if err := expandTLSConfigEnv(&cfg.Listeners[i].TLS); err != nil {
			return fmt.Errorf("listeners[%d].tls: %w", i, err)
		}
	}
	return nil
}

func expandListenerUsersEnv(lc *ListenerConfig, idx int) error {
	for user, pass := range lc.Users {
		if strings.TrimSpace(user) == "" {
			return fmt.Errorf("listeners[%d].users contains empty username", idx)
		}
		expanded, err := expandEnvStrict(fmt.Sprintf("listeners[%d].users[%q]", idx, user), pass)
		if err != nil {
			return err
		}
		expanded = strings.TrimSpace(expanded)
		if expanded == "" {
			return fmt.Errorf("listeners[%d].users[%q] password must not be empty", idx, user)
		}
		lc.Users[user] = expanded
	}
	return nil
}

func expandTLSConfigEnv(tlsCfg *TLSConfig) error {
	fields := map[string]*string{
		"tls.cert_file":      &tlsCfg.CertFile,
		"tls.key_file":       &tlsCfg.KeyFile,
		"tls.client_ca_file": &tlsCfg.ClientCAFile,
	}
	for field, ptr := range fields {
		if *ptr == "" {
			continue
		}
		expanded, err := expandEnvStrict(field, *ptr)
		if err != nil {
			return err
		}
		*ptr = strings.TrimSpace(expanded)
		if *ptr == "" {
			return fmt.Errorf("%s must not be empty", field)
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
		if _, err := os.Stat(tlsCfg.CertFile); err != nil {
			return fmt.Errorf("cert_file %q: %w", tlsCfg.CertFile, err)
		}
		if _, err := os.Stat(tlsCfg.KeyFile); err != nil {
			return fmt.Errorf("key_file %q: %w", tlsCfg.KeyFile, err)
		}
	}
	if hasClientCA {
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
		if _, err := net.ResolveTCPAddr("tcp", cfg.Listen); err != nil {
			return fmt.Errorf("listen %q is not a valid TCP address: %w", cfg.Listen, err)
		}
	}
	seenListeners := make(map[string]struct{}, len(cfg.Listeners))
	for i, lc := range cfg.Listeners {
		if strings.TrimSpace(lc.Listen) == "" {
			return fmt.Errorf("listeners[%d].listen must not be empty", i)
		}
		if _, err := net.ResolveTCPAddr("tcp", lc.Listen); err != nil {
			return fmt.Errorf("listeners[%d].listen %q is not a valid TCP address: %w", i, lc.Listen, err)
		}
		if _, exists := seenListeners[lc.Listen]; exists {
			return fmt.Errorf("listeners[%d].listen %q is duplicated", i, lc.Listen)
		}
		seenListeners[lc.Listen] = struct{}{}
		if err := validateTLSConfig(&lc.TLS); err != nil {
			return fmt.Errorf("listeners[%d].tls: %w", i, err)
		}
		if lc.TProxy.Enabled {
			if !proxy.IsTProxySupported() {
				return fmt.Errorf("listeners[%d].tproxy: transparent proxy is only supported on Linux", i)
			}
			mode := lc.TProxy.NormalizedMode()
			if mode != "redirect" && mode != "tproxy" {
				return fmt.Errorf("listeners[%d].tproxy.mode: unsupported mode %q, must be \"redirect\" or \"tproxy\"", i, lc.TProxy.Mode)
			}
			if lc.TLS.CertFile != "" || lc.TLS.KeyFile != "" {
				return fmt.Errorf("listeners[%d].tproxy: TLS is not supported with transparent proxy", i)
			}
		}
		if _, err := proxy.NewAccessController(lc.AccessControl); err != nil {
			return fmt.Errorf("listeners[%d].access_control: %w", i, err)
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
	if _, err := proxy.NewDestinationPolicy(cfg.DestinationPolicy); err != nil {
		return fmt.Errorf("destination_policy: %w", err)
	}
	if cfg.Routing.Auto.DirectTimeout < 0 {
		return fmt.Errorf("routing.auto.direct_timeout must be >= 0")
	}
	if cfg.Routing.Auto.CacheTTL < 0 {
		return fmt.Errorf("routing.auto.cache_ttl must be >= 0")
	}
	if cfg.Routing.GeoSite.UpdateInterval <= 0 {
		return fmt.Errorf("routing.geosite.update_interval must be > 0")
	}
	if cfg.Management != "" && !isLoopbackListen(cfg.Management) && cfg.ManagementAuthToken == "" {
		return fmt.Errorf("management_auth_token is required when management listens on a non-loopback address")
	}
	if cfg.EnablePprof && cfg.ManagementAuthToken == "" {
		// pprof 端点会暴露堆栈、内存与 goroutine 等敏感信息，免认证时仅允许绑定回环地址。
		if cfg.Management != "" && !isLoopbackListen(cfg.Management) {
			return fmt.Errorf("management_auth_token is required when enable_pprof is true and management listens on a non-loopback address")
		}
	}
	if err := proxy.ValidateLogConfig(cfg.Log); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	for i, up := range cfg.Upstreams {
		if strings.TrimSpace(up.URL) == "" {
			return fmt.Errorf("upstreams[%d].url must not be empty", i)
		}
		if up.Weight < 0 {
			return fmt.Errorf("upstreams[%d].weight must be >= 0", i)
		}
		if up.Weight > maxUpstreamWeight {
			return fmt.Errorf("upstreams[%d].weight must be <= %d", i, maxUpstreamWeight)
		}
		if err := proxy.ValidateProxyTypes(up.Types); err != nil {
			return fmt.Errorf("upstreams[%d].types: %w", i, err)
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

	// 校验路由规则引用的上游名是否存在
	for i, rule := range cfg.Routing.Rules {
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

	if _, err := proxy.NewAccessController(cfg.AccessControl); err != nil {
		return fmt.Errorf("access_control: %w", err)
	}

	if err := proxy.ValidateDNSConfig(cfg.DNS); err != nil {
		return fmt.Errorf("dns: %w", err)
	}

	if cfg.TrafficAudit.Enabled {
		if cfg.TrafficAudit.PerUserQuota < 0 {
			return fmt.Errorf("traffic_audit.per_user_quota must be >= 0")
		}
		if cfg.TrafficAudit.PerUpstreamQuota < 0 {
			return fmt.Errorf("traffic_audit.per_upstream_quota must be >= 0")
		}
		if cfg.TrafficAudit.QuotaWindow < 0 {
			return fmt.Errorf("traffic_audit.quota_window must be >= 0")
		}
	}

	return nil
}
