package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k5pw/proxy"
)

// Config 代理服务器配置
type Config struct {
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
	proxy.NormalizeGeoSiteConfig(&cfg.Routing.GeoSite)
	proxy.NormalizeLogConfig(&cfg.Log)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
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

func validateConfig(cfg *Config) error {
	if strings.TrimSpace(cfg.Listen) == "" {
		return fmt.Errorf("listen must not be empty")
	}
	if _, err := net.ResolveTCPAddr("tcp", cfg.Listen); err != nil {
		return fmt.Errorf("listen %q is not a valid TCP address: %w", cfg.Listen, err)
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

	return nil
}
