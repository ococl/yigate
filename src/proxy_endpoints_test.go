package main

import "testing"

func TestBuildProxyEndpointsSpecificIPv4(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{{Listen: "127.0.0.1:4479"}})
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(endpoints))
	}
	got := endpoints[0]
	if got.Address != "127.0.0.1" || got.Port != 4479 || got.Family != "ipv4" || !got.Loopback {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
}

func TestBuildProxyEndpointsSpecificIPv6(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{{Listen: "[::1]:4480"}})
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(endpoints))
	}
	got := endpoints[0]
	if got.Address != "::1" || got.Port != 4480 || got.Family != "ipv6" || !got.Loopback {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
}

func TestBuildProxyEndpointsHostnameAndTLS(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{{
		Listen: "proxy.example.com:8443",
		TLS:    TLSConfig{CertFile: "server.crt", KeyFile: "server.key"},
	}})
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(endpoints))
	}
	got := endpoints[0]
	if got.Address != "proxy.example.com" || got.Family != "hostname" || !got.TLS {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
}

func TestBuildProxyEndpointsWildcardDetectsLocalAddresses(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{{Listen: "0.0.0.0:4479"}})
	if len(endpoints) == 0 {
		t.Fatal("expected at least one detected endpoint")
	}
	for _, endpoint := range endpoints {
		if endpoint.Port != 4479 {
			t.Fatalf("unexpected port: %+v", endpoint)
		}
	}
}

func TestBuildProxyEndpointsSkipsInvalidListen(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{
		{Listen: "not-a-listen-address"},
		{Listen: "127.0.0.1:0"},
	})
	if len(endpoints) != 0 {
		t.Fatalf("endpoints = %d, want 0", len(endpoints))
	}
}

func TestBuildProxyEndpointsKeepsMultiplePorts(t *testing.T) {
	endpoints := buildProxyEndpoints([]ListenerConfig{
		{Listen: "127.0.0.1:4479"},
		{Listen: "127.0.0.1:4480"},
	})
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(endpoints))
	}
	if endpoints[0].Port != 4479 || endpoints[1].Port != 4480 {
		t.Fatalf("unexpected order: %+v", endpoints)
	}
}
