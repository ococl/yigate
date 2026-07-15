package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"k5pw/proxy"
)

// configEditor 实现 proxy.ConfigEditor 接口。
// 它通过原子性地修改配置文件并触发热加载来工作。
type configEditor struct {
	cfgPath string
	apply   func() error
	applyMu sync.Mutex
}

// errNotFound 表示资源不存在，用于区分 404 和 400 响应。
var errNotFound = errors.New("not found")

// newConfigEditor 创建配置编辑器。
// apply 回调用于触发热加载（通常是 Server.LoadAndApply）。
func newConfigEditor(cfgPath string, apply func() error) *configEditor {
	return &configEditor{
		cfgPath: cfgPath,
		apply:   apply,
	}
}

// loadRawConfig 从磁盘加载配置，不展开环境变量，不校验。
// 用于读取可安全修改的原始配置（保留 ${ENV_VAR} 占位符）。
func (e *configEditor) loadRawConfig() (*Config, error) {
	data, err := os.ReadFile(e.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if cfg.Upstreams == nil {
		cfg.Upstreams = []proxy.UpstreamConfig{}
	}
	if cfg.Routing.Rules == nil {
		cfg.Routing.Rules = []proxy.RouteRule{}
	}
	return cfg, nil
}

// saveAndApply 保存配置到磁盘并触发热加载。
// 使用文件锁保证并发安全，使用临时文件+原子重命名保证写入安全。
func (e *configEditor) saveAndApply(cfg *Config) error {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(e.cfgPath)
	tmp, err := os.CreateTemp(dir, ".k5pw-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // 清理可能残留的临时文件

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, e.cfgPath); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}

	if e.apply != nil {
		if err := e.apply(); err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
	}
	return nil
}

// validateUpstream 校验上游配置的基本合法性。
func validateUpstream(up proxy.UpstreamConfig) error {
	if strings.TrimSpace(up.URL) == "" {
		return errors.New("url must not be empty")
	}
	if up.Weight < 0 {
		return errors.New("weight must be >= 0")
	}
	if up.Weight > maxUpstreamWeight {
		return fmt.Errorf("weight must be <= %d", maxUpstreamWeight)
	}
	if err := proxy.ValidateProxyTypes(up.Types); err != nil {
		return err
	}
	return nil
}

func (e *configEditor) ListUpstreams() []proxy.UpstreamConfig {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	return cfg.Upstreams
}

func (e *configEditor) AddUpstream(up proxy.UpstreamConfig) error {
	if err := validateUpstream(up); err != nil {
		return err
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(up.Name)
	if name == "" {
		name = fmt.Sprintf("upstream-%d", len(cfg.Upstreams)+1)
		up.Name = name
	}
	for _, existing := range cfg.Upstreams {
		if existing.Name == name {
			return fmt.Errorf("upstream %q already exists", name)
		}
	}
	cfg.Upstreams = append(cfg.Upstreams, up)
	return e.saveAndApply(cfg)
}

func (e *configEditor) UpdateUpstream(name string, up proxy.UpstreamConfig) error {
	if err := validateUpstream(up); err != nil {
		return err
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	found := false
	for i, existing := range cfg.Upstreams {
		if existing.Name == name {
			up.Name = name
			cfg.Upstreams[i] = up
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: upstream %q not found", errNotFound, name)
	}
	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveUpstream(name string) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	found := false
	for i, existing := range cfg.Upstreams {
		if existing.Name == name {
			cfg.Upstreams = append(cfg.Upstreams[:i], cfg.Upstreams[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: upstream %q not found", errNotFound, name)
	}
	// 同时清理路由规则中对该上游的引用
	for i := range cfg.Routing.Rules {
		filtered := make([]string, 0, len(cfg.Routing.Rules[i].Upstreams))
		for _, ref := range cfg.Routing.Rules[i].Upstreams {
			if strings.TrimSpace(ref) != name {
				filtered = append(filtered, ref)
			}
		}
		cfg.Routing.Rules[i].Upstreams = filtered
	}
	// 清理 socks5h_redirect 中的引用
	socksRefs := cfg.Routing.Socks5HRedirect.Upstreams
	filteredSocks := make([]string, 0, len(socksRefs))
	for _, ref := range socksRefs {
		if strings.TrimSpace(ref) != name {
			filteredSocks = append(filteredSocks, ref)
		}
	}
	cfg.Routing.Socks5HRedirect.Upstreams = filteredSocks
	return e.saveAndApply(cfg)
}

func (e *configEditor) ListRules() []proxy.RouteRule {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	return cfg.Routing.Rules
}

func (e *configEditor) AddRule(rule proxy.RouteRule) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	cfg.Routing.Rules = append(cfg.Routing.Rules, rule)
	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveRule(index int) error {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(cfg.Routing.Rules) {
		return fmt.Errorf("%w: rule index %d out of range (0-%d)", errNotFound, index, len(cfg.Routing.Rules)-1)
	}
	cfg.Routing.Rules = append(cfg.Routing.Rules[:index], cfg.Routing.Rules[index+1:]...)
	return e.saveAndApply(cfg)
}

func (e *configEditor) ListUsers() []string {
	cfg, err := e.loadRawConfig()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(cfg.Users))
	for name := range cfg.Users {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	// 多 listener 模式下聚合各 listener 的用户
	for i := range cfg.Listeners {
		for name := range cfg.Listeners[i].Users {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				result = append(result, name)
			}
		}
	}
	return result
}

func (e *configEditor) AddUser(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username must not be empty")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password must not be empty")
	}
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if _, exists := cfg.Users[username]; exists {
		return fmt.Errorf("user %q already exists", username)
	}
	cfg.Users[username] = password
	// 多 listener 模式下同步添加到所有 listener
	for i := range cfg.Listeners {
		if cfg.Listeners[i].Users == nil {
			cfg.Listeners[i].Users = make(map[string]string)
		}
		cfg.Listeners[i].Users[username] = password
	}
	return e.saveAndApply(cfg)
}

func (e *configEditor) RemoveUser(username string) error {
	username = strings.TrimSpace(username)
	cfg, err := e.loadRawConfig()
	if err != nil {
		return err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	found := false
	if _, exists := cfg.Users[username]; exists {
		delete(cfg.Users, username)
		found = true
	}
	// 多 listener 模式下同步从所有 listener 删除
	for i := range cfg.Listeners {
		if cfg.Listeners[i].Users == nil {
			continue
		}
		if _, exists := cfg.Listeners[i].Users[username]; exists {
			delete(cfg.Listeners[i].Users, username)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: user %q not found", errNotFound, username)
	}
	return e.saveAndApply(cfg)
}
