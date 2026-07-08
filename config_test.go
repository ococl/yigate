package main

import (
	"os"
	"testing"

	"k5pw/proxy"
)

func TestLoadConfigWithRouting(t *testing.T) {
	cfg, err := LoadConfig("config.yml")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Routing.Final != "auto" {
		t.Fatalf("routing final = %q, want %q", cfg.Routing.Final, "auto")
	}
	if cfg.Routing.GeoSite.Path == "" {
		t.Fatal("routing geosite path was not loaded")
	}
	if !cfg.Routing.GeoSite.AutoUpdate {
		t.Fatal("routing geosite auto update was not enabled")
	}
	if cfg.Routing.GeoSite.UpdateURL == "" {
		t.Fatal("routing geosite update url was not defaulted")
	}
	if cfg.Routing.GeoSite.UpdateURL != "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat" {
		t.Fatalf("routing geosite update url = %q", cfg.Routing.GeoSite.UpdateURL)
	}
	if len(cfg.Upstreams) == 0 || len(cfg.Upstreams[0].Types) == 0 {
		t.Fatalf("upstream types were not loaded: %#v", cfg.Upstreams)
	}
	if !cfg.UpstreamTypeProbe.Enabled {
		t.Fatal("upstream type probe was not enabled")
	}
	if cfg.UpstreamTypeProbe.CachePath == "" {
		t.Fatal("upstream type probe cache path was not loaded")
	}
	if !cfg.Routing.Socks5HRedirect.Enabled {
		t.Fatal("routing socks5h redirect was not enabled")
	}
	if len(cfg.Routing.Rules) == 0 {
		t.Fatal("routing rules were not loaded")
	}
}

func TestConfigGeoSiteCNRouting(t *testing.T) {
	if _, err := os.Stat("geosite.dat"); err != nil {
		t.Skip("geosite.dat is not available")
	}

	cfg, err := LoadConfig("config.yml")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	router, err := proxy.NewRouter(cfg.Routing)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	if got := router.Decide("www.qq.com:443").Action; got != proxy.ActionDirect {
		t.Fatalf("www.qq.com action = %q, want %q", got, proxy.ActionDirect)
	}
	if got := router.Decide("www.baidu.com:443").Action; got != proxy.ActionDirect {
		t.Fatalf("www.baidu.com action = %q, want %q", got, proxy.ActionDirect)
	}
	if got := router.Decide("github.com:443").Action; got != proxy.ActionProxy {
		t.Fatalf("github.com action = %q, want %q", got, proxy.ActionProxy)
	}
}
