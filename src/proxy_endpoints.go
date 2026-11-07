package main

import (
	"net"
	"sort"
	"strconv"

	"yigate/proxy"
)

// buildProxyEndpoints 根据代理监听配置探测可用的连接地址。
// 通配地址（空、0.0.0.0、::）会展开为本机所有已启用网卡的 IP，
// 指定 IP/主机名时只返回该地址。多监听端口会分别列出。
func buildProxyEndpoints(listeners []ListenerConfig) []proxy.ProxyEndpoint {
	endpoints := make([]proxy.ProxyEndpoint, 0, len(listeners))
	seen := make(map[string]struct{})

	for _, listener := range listeners {
		host, portText, err := net.SplitHostPort(listener.Listen)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 || port > 65535 {
			continue
		}

		tlsEnabled := listener.TLS.CertFile != "" || listener.TLS.KeyFile != ""
		for _, item := range proxyEndpointHosts(host) {
			key := net.JoinHostPort(item.address, portText)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			endpoints = append(endpoints, proxy.ProxyEndpoint{
				Address:       item.address,
				Port:          port,
				Family:        item.family,
				InterfaceName: item.interfaceName,
				Loopback:      item.loopback,
				TLS:           tlsEnabled,
			})
		}
	}

	sort.SliceStable(endpoints, func(i, j int) bool {
		if endpoints[i].Port != endpoints[j].Port {
			return endpoints[i].Port < endpoints[j].Port
		}
		if endpoints[i].Loopback != endpoints[j].Loopback {
			return !endpoints[i].Loopback
		}
		if familyRank(endpoints[i].Family) != familyRank(endpoints[j].Family) {
			return familyRank(endpoints[i].Family) < familyRank(endpoints[j].Family)
		}
		return endpoints[i].Address < endpoints[j].Address
	})
	return endpoints
}

type proxyEndpointHost struct {
	address       string
	family        string
	interfaceName string
	loopback      bool
}

func proxyEndpointHosts(host string) []proxyEndpointHost {
	switch host {
	case "", "0.0.0.0", "::":
		hosts := interfaceProxyEndpointHosts()
		if len(hosts) == 0 {
			return []proxyEndpointHost{{address: "127.0.0.1", family: "ipv4", loopback: true}}
		}
		return hosts
	case "localhost":
		return []proxyEndpointHost{{address: "localhost", family: "hostname", loopback: true}}
	}

	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		family := "ipv6"
		if ip.To4() != nil {
			family = "ipv4"
		}
		return []proxyEndpointHost{{address: ip.String(), family: family, loopback: ip.IsLoopback()}}
	}
	return []proxyEndpointHost{{address: host, family: "hostname"}}
}

func interfaceProxyEndpointHosts() []proxyEndpointHost {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	hosts := make([]proxyEndpointHost, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			family := "ipv6"
			if ip.To4() != nil {
				family = "ipv4"
			}
			hosts = append(hosts, proxyEndpointHost{
				address:       ip.String(),
				family:        family,
				interfaceName: iface.Name,
				loopback:      ip.IsLoopback(),
			})
		}
	}
	return hosts
}

func familyRank(family string) int {
	switch family {
	case "ipv4":
		return 0
	case "hostname":
		return 1
	default:
		return 2
	}
}
