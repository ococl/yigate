package proxy

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TrafficAuditConfig 定义流量审计与配额配置。
type TrafficAuditConfig struct {
	Enabled          bool     `yaml:"enabled"`
	PerUserQuota     int64    `yaml:"per_user_quota"`     // 每用户流量配额（字节），0 表示不限制
	PerUpstreamQuota int64    `yaml:"per_upstream_quota"` // 每上游流量配额（字节），0 表示不限制
	QuotaWindow      Duration `yaml:"quota_window"`       // 配额统计窗口，默认 1 小时
}

// TrafficKey 标识一条流量统计维度。
type TrafficKey struct {
	User     string
	Upstream string
	Target   string // 目标域名
}

// trafficEntry 记录单个维度的流量统计。
type trafficEntry struct {
	bytesIn     atomic.Int64 // client -> target
	bytesOut    atomic.Int64 // target -> client
	windowStart time.Time
}

// maxTrafficEntries 限制单个配额窗口内 entries map 的最大条目数，
// 防止高流量代理在窗口周期内积累过多唯一目标导致内存无限增长。
const maxTrafficEntries = 50000

// TrafficAccounter 统计按 user/upstream/target 维度的流量，并执行配额检查。
type TrafficAccounter struct {
	enabled          bool
	perUserQuota     int64
	perUpstreamQuota int64
	quotaWindow      time.Duration

	mu      sync.RWMutex
	entries map[string]*trafficEntry

	// 聚合计数器（按 user / upstream）
	userBytes     map[string]*atomic.Int64
	upstreamBytes map[string]*atomic.Int64

	// 配额窗口重置
	resetOnce sync.Once
	stopCh    chan struct{}
}

// NewTrafficAccounter 根据配置创建流量统计器。
func NewTrafficAccounter(cfg TrafficAuditConfig) *TrafficAccounter {
	window := time.Duration(cfg.QuotaWindow)
	if window <= 0 {
		window = time.Hour
	}
	a := &TrafficAccounter{
		enabled:          cfg.Enabled,
		perUserQuota:     cfg.PerUserQuota,
		perUpstreamQuota: cfg.PerUpstreamQuota,
		quotaWindow:      window,
		entries:          make(map[string]*trafficEntry),
		userBytes:        make(map[string]*atomic.Int64),
		upstreamBytes:    make(map[string]*atomic.Int64),
		stopCh:           make(chan struct{}),
	}
	if cfg.Enabled {
		a.Start()
	}
	return a
}

// Start 启动配额窗口重置 goroutine。
func (a *TrafficAccounter) Start() {
	if a == nil || !a.enabled {
		return
	}
	a.resetOnce.Do(func() {
		go a.windowLoop()
	})
}

// Stop 停止配额窗口重置。
func (a *TrafficAccounter) Stop() {
	if a == nil {
		return
	}
	select {
	case <-a.stopCh:
		// already closed
	default:
		close(a.stopCh)
	}
}

func (a *TrafficAccounter) windowLoop() {
	ticker := time.NewTicker(a.quotaWindow)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.resetWindow()
		case <-a.stopCh:
			return
		}
	}
}

func (a *TrafficAccounter) resetWindow() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = make(map[string]*trafficEntry)
	a.userBytes = make(map[string]*atomic.Int64)
	a.upstreamBytes = make(map[string]*atomic.Int64)
}

// trafficKeyStr 将 TrafficKey 转为 map key。
func trafficKeyStr(k TrafficKey) string {
	return k.User + "\x00" + k.Upstream + "\x00" + k.Target
}

// Record 记录流量并检查配额。返回 ErrQuotaExceeded 如果超出配额。
func (a *TrafficAccounter) Record(k TrafficKey, bytesIn, bytesOut int64) error {
	if a == nil || !a.enabled {
		return nil
	}
	total := bytesIn + bytesOut
	if total <= 0 {
		return nil
	}

	keyStr := trafficKeyStr(k)

	a.mu.RLock()
	entry, ok := a.entries[keyStr]
	a.mu.RUnlock()
	if !ok {
		a.mu.Lock()
		entry, ok = a.entries[keyStr]
		if !ok {
			if len(a.entries) >= maxTrafficEntries {
				// 超过上限时不再创建新条目，仅更新已有条目，
				// 防止高流量代理在窗口内 entries 无限增长。
				a.mu.Unlock()
				return a.recordAggregates(k, bytesIn, bytesOut, total)
			}
			entry = &trafficEntry{windowStart: time.Now()}
			a.entries[keyStr] = entry
		}
		a.mu.Unlock()
	}
	entry.bytesIn.Add(bytesIn)
	entry.bytesOut.Add(bytesOut)

	return a.recordAggregates(k, bytesIn, bytesOut, total)
}

// recordAggregates 更新用户和上游聚合计数器以及 Prometheus 指标。
// 返回配额超限错误（如果有）。
func (a *TrafficAccounter) recordAggregates(k TrafficKey, bytesIn, bytesOut, total int64) error {
	// 更新用户聚合
	if k.User != "" {
		a.mu.RLock()
		ub, ok := a.userBytes[k.User]
		a.mu.RUnlock()
		if !ok {
			a.mu.Lock()
			ub, ok = a.userBytes[k.User]
			if !ok {
				ub = &atomic.Int64{}
				a.userBytes[k.User] = ub
			}
			a.mu.Unlock()
		}
		ub.Add(total)
		if a.perUserQuota > 0 && ub.Load() > a.perUserQuota {
			return fmt.Errorf("user %q quota exceeded: %d > %d", k.User, ub.Load(), a.perUserQuota)
		}
	}

	// 更新上游聚合
	if k.Upstream != "" {
		a.mu.RLock()
		ub, ok := a.upstreamBytes[k.Upstream]
		a.mu.RUnlock()
		if !ok {
			a.mu.Lock()
			ub, ok = a.upstreamBytes[k.Upstream]
			if !ok {
				ub = &atomic.Int64{}
				a.upstreamBytes[k.Upstream] = ub
			}
			a.mu.Unlock()
		}
		ub.Add(total)
		if a.perUpstreamQuota > 0 && ub.Load() > a.perUpstreamQuota {
			return fmt.Errorf("upstream %q quota exceeded: %d > %d", k.Upstream, ub.Load(), a.perUpstreamQuota)
		}
	}

	// 更新 Prometheus 指标（target 标签做归一化以限制基数）
	normalizedTarget := normalizeTargetLabel(k.Target)
	MetricAdd("yigate_traffic_bytes_total", float64(bytesIn), "direction=client_to_target", "user="+k.User, "upstream="+k.Upstream, "target="+normalizedTarget)
	MetricAdd("yigate_traffic_bytes_total", float64(bytesOut), "direction=target_to_client", "user="+k.User, "upstream="+k.Upstream, "target="+normalizedTarget)

	return nil
}

// TrafficSnapshot 是流量统计快照。
type TrafficSnapshot struct {
	Entries []TrafficEntrySnapshot `json:"entries"`
}

type TrafficEntrySnapshot struct {
	User     string `json:"user"`
	Upstream string `json:"upstream"`
	Target   string `json:"target"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
	Total    int64  `json:"total"`
}

// Snapshot 返回当前流量统计快照。
func (a *TrafficAccounter) Snapshot() TrafficSnapshot {
	if a == nil {
		return TrafficSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	entries := make([]TrafficEntrySnapshot, 0, len(a.entries))
	for keyStr, entry := range a.entries {
		parts := strings.SplitN(keyStr, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		bi := entry.bytesIn.Load()
		bo := entry.bytesOut.Load()
		entries = append(entries, TrafficEntrySnapshot{
			User:     parts[0],
			Upstream: parts[1],
			Target:   parts[2],
			BytesIn:  bi,
			BytesOut: bo,
			Total:    bi + bo,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Total > entries[j].Total
	})
	return TrafficSnapshot{Entries: entries}
}

// UserUsage 返回指定用户的当前窗口流量总量。
func (a *TrafficAccounter) UserUsage(user string) int64 {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if ub, ok := a.userBytes[user]; ok {
		return ub.Load()
	}
	return 0
}

// UpstreamUsage 返回指定上游的当前窗口流量总量。
func (a *TrafficAccounter) UpstreamUsage(upstream string) int64 {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if ub, ok := a.upstreamBytes[upstream]; ok {
		return ub.Load()
	}
	return 0
}

// ErrQuotaExceeded 表示流量配额超出。
var ErrQuotaExceeded = fmt.Errorf("traffic quota exceeded")

// globalTrafficAccounter 是全局流量统计器，由 server.go 通过 SetGlobalTrafficAccounter 设置。
var globalTrafficAccounter *TrafficAccounter

// SetGlobalTrafficAccounter 设置全局流量统计器。
func SetGlobalTrafficAccounter(a *TrafficAccounter) {
	globalTrafficAccounter = a
}

// targetHostFromAddr 从 host:port 地址中提取域名部分用于流量统计。
func targetHostFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// normalizeTargetLabel 归一化目标域名用于 Prometheus 标签，防止高基数。
// 截断超长域名，IP 地址保持原样。
const maxTargetLabelLen = 63

func normalizeTargetLabel(target string) string {
	if target == "" {
		return ""
	}
	// IP 地址直接返回（基数可控）
	if net.ParseIP(target) != nil {
		return target
	}
	if len(target) <= maxTargetLabelLen {
		return target
	}
	// 保留域名后缀（TLD 部分），截断中间
	dotIdx := strings.LastIndex(target, ".")
	if dotIdx > 0 && len(target)-dotIdx <= maxTargetLabelLen/2 {
		suffix := target[dotIdx:]
		prefix := target[:maxTargetLabelLen-len(suffix)-3]
		return prefix + "..." + suffix
	}
	return target[:maxTargetLabelLen-3] + "..."
}
