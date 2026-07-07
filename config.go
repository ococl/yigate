package main

import (
	"os"

	"gopkg.in/yaml.v3"
	"k5pw/proxy"
)

// Config 代理服务器配置
type Config struct {
	Listen        string                 `yaml:"listen"`
	Users         map[string]string      `yaml:"users"`
	Upstreams     []proxy.UpstreamConfig `yaml:"upstreams"`
	CheckInterval int                    `yaml:"check_interval"`
}

// LoadConfig 加载 YAML 配置文件，文件不存在时返回默认配置
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Listen:        ":8891",
		Users:         make(map[string]string),
		Upstreams:     []proxy.UpstreamConfig{},
		CheckInterval: 30,
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	if err := dec.Decode(cfg); err != nil {
		return nil, err
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]string)
	}
	if cfg.Upstreams == nil {
		cfg.Upstreams = []proxy.UpstreamConfig{}
	}
	return cfg, nil
}
