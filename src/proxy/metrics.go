package proxy

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// MetricType 表示指标类型。
type MetricType string

const (
	MetricCounter MetricType = "counter"
	MetricGauge   MetricType = "gauge"
)

// MetricDesc 描述一个指标。
type MetricDesc struct {
	Name   string
	Help   string
	Type   MetricType
	Labels []string
}

// metricsMaxEntries 是指标注册表中允许的最大条目数。
// 超过此上限后，新的标签组合不再创建条目，防止高基数标签导致内存无限增长。
const metricsMaxEntries = 10000

// Metrics 是一个轻量级的 Prometheus 格式指标注册表。
// 所有指标线程安全，通过 key（name + 标签）存储为 float64。
type Metrics struct {
	mu     sync.RWMutex
	values map[string]float64
	descs  []MetricDesc
}

var (
	// registry 是全局指标注册表。
	registry = &Metrics{values: make(map[string]float64)}
)

func init() {
	// 预注册核心指标
	RegisterMetric(MetricDesc{
		Name: "yigate_connections_active",
		Help: "Current number of active downstream connections.",
		Type: MetricGauge,
	})
	RegisterMetric(MetricDesc{
		Name: "yigate_connections_total",
		Help: "Total number of downstream connections accepted.",
		Type: MetricCounter,
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_requests_total",
		Help:   "Total number of requests handled by protocol.",
		Type:   MetricCounter,
		Labels: []string{"protocol"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_routing_decisions_total",
		Help:   "Total number of routing decisions by action.",
		Type:   MetricCounter,
		Labels: []string{"action"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_upstream_dial_failures_total",
		Help:   "Total number of upstream dial failures by upstream name.",
		Type:   MetricCounter,
		Labels: []string{"upstream"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_upstream_health_status",
		Help:   "Current health status of each upstream (1=alive, 0=dead).",
		Type:   MetricGauge,
		Labels: []string{"upstream"},
	})
	RegisterMetric(MetricDesc{
		Name: "yigate_cache_hits_total",
		Help: "Total number of route cache hits.",
		Type: MetricCounter,
	})
	RegisterMetric(MetricDesc{
		Name: "yigate_cache_misses_total",
		Help: "Total number of route cache misses.",
		Type: MetricCounter,
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_tunnel_transferred_bytes_total",
		Help:   "Total number of bytes transferred through tunnels.",
		Type:   MetricCounter,
		Labels: []string{"direction"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_tunnel_packets_total",
		Help:   "Total number of read operations through tunnels.",
		Type:   MetricCounter,
		Labels: []string{"direction"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_management_requests_total",
		Help:   "Total number of requests to management endpoints.",
		Type:   MetricCounter,
		Labels: []string{"path"},
	})
	RegisterMetric(MetricDesc{
		Name:   "yigate_traffic_bytes_total",
		Help:   "Total bytes transferred by user and upstream.",
		Type:   MetricCounter,
		Labels: []string{"direction", "user", "upstream"},
	})
}

// RegisterMetric 注册一个指标描述。
func RegisterMetric(desc MetricDesc) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.descs = append(registry.descs, desc)
}

// MetricInc 将指定指标增加 1。
func MetricInc(name string, labels ...string) {
	registry.metricAdd(name, 1, labels...)
}

// MetricAdd 将指定指标增加 value。
func MetricAdd(name string, value float64, labels ...string) {
	registry.metricAdd(name, value, labels...)
}

func (m *Metrics) metricAdd(name string, value float64, labels ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	if _, exists := m.values[key]; !exists {
		if len(m.values) >= metricsMaxEntries {
			// 超过上限，仅更新已有条目，不创建新条目
			return
		}
	}
	m.values[key] += value
}

// MetricSet 将 gauge 类型的指标设置为 value。
func MetricSet(name string, value float64, labels ...string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	key := metricKey(name, labels)
	registry.values[key] = value
}

// MetricValue 返回当前指标值。
func MetricValue(name string, labels ...string) float64 {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.values[metricKey(name, labels)]
}

// ExposeMetrics 以 Prometheus exposition 格式输出所有指标。
func ExposeMetrics(w io.Writer) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	for _, desc := range registry.descs {
		fmt.Fprintf(w, "# HELP %s %s\n", desc.Name, desc.Help)
		fmt.Fprintf(w, "# TYPE %s %s\n", desc.Name, desc.Type)
		keys := matchingKeys(desc.Name)
		for _, key := range keys {
			value := registry.values[key]
			fmt.Fprintf(w, "%s %g\n", key, value)
		}
		// 如果没有任何标签样本，输出一个无标签的 0 值占位
		if len(desc.Labels) == 0 && len(keys) == 0 {
			fmt.Fprintf(w, "%s 0\n", desc.Name)
		}
	}
}

func metricKey(name string, labels []string) string {
	if len(labels) == 0 {
		return name
	}
	return fmt.Sprintf("%s{%s}", name, strings.Join(labels, ","))
}

func matchingKeys(name string) []string {
	var keys []string
	for key := range registry.values {
		if key == name {
			keys = append(keys, key)
			continue
		}
		if strings.HasPrefix(key, name+"{") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
