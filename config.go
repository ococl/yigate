package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"k5pw/proxy"
)

// Config 代理服务器配置
type Config struct {
	Listen         string                 `yaml:"listen"`
	Users          map[string]string      `yaml:"users"`
	Upstreams      []proxy.UpstreamConfig `yaml:"upstreams"`
	CheckInterval  int                    `yaml:"check_interval"`
	MaxConnections int                    `yaml:"max_connections"`
}

const (
	defaultMaxConnections = 1024
	maxUpstreamWeight     = 10000
)

func defaultConfig() *Config {
	return &Config{
		Listen:         "127.0.0.1:8891",
		Users:          make(map[string]string),
		Upstreams:      []proxy.UpstreamConfig{},
		CheckInterval:  30,
		MaxConnections: defaultMaxConnections,
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
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = defaultMaxConnections
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateConfig(cfg *Config) error {
	if strings.TrimSpace(cfg.Listen) == "" {
		return fmt.Errorf("listen must not be empty")
	}
	if cfg.CheckInterval < 0 {
		return fmt.Errorf("check_interval must be >= 0")
	}
	if cfg.MaxConnections < 0 {
		return fmt.Errorf("max_connections must be >= 0")
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
	}
	return nil
}
