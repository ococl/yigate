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

const (
	// DefaultTrafficRetentionDays 是流量明细的默认保留天数。
	DefaultTrafficRetentionDays = 30
	// maxTrafficHistoryEntries 限制持久化流量明细的最大条目数，防止高基数目标撑大内存与磁盘。
	maxTrafficHistoryEntries = 100000
	trafficFlushInterval     = 15 * time.Second
)

// TrafficAuditConfig 定义流量审计与配额配置。
type TrafficAuditConfig struct {
	Enabled          bool     `yaml:"enabled"`
	PerUserQuota     int64    `yaml:"per_user_quota"`     // 每用户流量配额（字节），0 表示不限制
	PerUpstreamQuota int64    `yaml:"per_upstream_quota"` // 每上游流量配额（字节），0 表示不限制
	QuotaWindow      Duration `yaml:"quota_window"`       // 配额统计窗口，默认 1 小时
	RetentionDays    int      `yaml:"retention_days"`     // 流量明细保留天数，默认 30 天
}

// TrafficKey 标识一条流量统计维度。
type TrafficKey struct {
	User     string
	Upstream string
	Target   string // 目标域名
}

// trafficHistoryEntry 记录某个自然日内单个维度的累计流量。
type trafficHistoryEntry struct {
	day      string
	key      TrafficKey
	bytesIn  atomic.Int64 // client -> target
	bytesOut atomic.Int64 // target -> client
}

// TrafficAccounter 统计按 user/upstream/target 维度的流量，并执行配额检查。
//
// 配额窗口与展示数据分离：配额窗口到期只重置配额计数器，持久化历史不会清空；
// 流量明细按自然日聚合，默认保留 30 天，热加载配置时复用同一个实例。
type TrafficAccounter struct {
	enabled          atomic.Bool
	perUserQuota     atomic.Int64
	perUpstreamQuota atomic.Int64
	quotaWindow      atomic.Int64
	retentionDays    atomic.Int64
	storePath        string
	now              func() time.Time

	mu            sync.RWMutex
	history       map[string]*trafficHistoryEntry
	userBytes     map[string]*atomic.Int64
	upstreamBytes map[string]*atomic.Int64

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	configCh  chan struct{}
	flushMu   sync.Mutex
	dirtyGen  atomic.Uint64
	flushedAt atomic.Uint64
	wg        sync.WaitGroup
}

// NewTrafficAccounter 根据配置创建内存流量统计器。
func NewTrafficAccounter(cfg TrafficAuditConfig) *TrafficAccounter {
	return newTrafficAccounter(cfg, "")
}

// NewTrafficAccounterWithStore 根据配置创建带磁盘持久化的流量统计器。
func NewTrafficAccounterWithStore(cfg TrafficAuditConfig, storagePath string) *TrafficAccounter {
	return newTrafficAccounter(cfg, storagePath)
}

func newTrafficAccounter(cfg TrafficAuditConfig, storagePath string) *TrafficAccounter {
	window := time.Duration(cfg.QuotaWindow)
	if window <= 0 {
		window = time.Hour
	}
	retention := cfg.RetentionDays
	if retention <= 0 {
		retention = DefaultTrafficRetentionDays
	}
	a := &TrafficAccounter{
		storePath:     storagePath,
		now:           time.Now,
		history:       make(map[string]*trafficHistoryEntry),
		userBytes:     make(map[string]*atomic.Int64),
		upstreamBytes: make(map[string]*atomic.Int64),
		stopCh:        make(chan struct{}),
		configCh:      make(chan struct{}, 1),
	}
	a.enabled.Store(cfg.Enabled)
	a.perUserQuota.Store(cfg.PerUserQuota)
	a.perUpstreamQuota.Store(cfg.PerUpstreamQuota)
	a.quotaWindow.Store(int64(window))
	a.retentionDays.Store(int64(retention))

	if storagePath != "" {
		entries, err := loadTrafficHistory(storagePath)
		if err != nil {
			// 解析失败时先备份原文件，避免下一次 Flush 用空数据覆盖已有记录。
			if backup, backupErr := backupTrafficHistory(storagePath); backupErr == nil {
				Logger().Errorw("load traffic history failed, backup created",
					"path", storagePath, "backup", backup, "error", err)
			} else {
				Logger().Errorw("load traffic history failed", "path", storagePath, "error", err)
			}
		} else {
			a.mergeHistoryLocked(entries)
			if pruned := a.pruneHistoryLocked(a.now()); pruned > 0 {
				a.dirtyGen.Add(1)
			}
			Logger().Infow("traffic history loaded", "path", storagePath, "entries", len(a.history))
		}
	}

	a.Start()
	return a
}

// mergeHistoryLocked 把磁盘明细合并进内存：同一天同一组合的条目累加，
// 其余条目原样恢复，用于启动加载与防止历史被空数据覆盖。
func (a *TrafficAccounter) mergeHistoryLocked(entries []trafficHistoryDiskEntry) int {
	if len(entries) == 0 {
		return 0
	}
	merged := 0
	for _, item := range entries {
		if item.BytesIn < 0 || item.BytesOut < 0 {
			continue
		}
		key := TrafficKey{User: item.User, Upstream: item.Upstream, Target: item.Target}
		mapKey := trafficHistoryKey(item.Day, key)
		if existing := a.history[mapKey]; existing != nil {
			existing.bytesIn.Add(item.BytesIn)
			existing.bytesOut.Add(item.BytesOut)
			continue
		}
		entry := &trafficHistoryEntry{day: item.Day, key: key}
		entry.bytesIn.Store(item.BytesIn)
		entry.bytesOut.Store(item.BytesOut)
		a.history[mapKey] = entry
		merged++
	}
	return merged
}

// Start 启动配额窗口重置与持久化 goroutine。
func (a *TrafficAccounter) Start() {
	if a == nil {
		return
	}
	a.startOnce.Do(func() {
		a.wg.Add(2)
		go a.windowLoop()
		go a.storeLoop()
	})
}

// Stop 停止后台任务并把尚未落盘的流量写入磁盘。
func (a *TrafficAccounter) Stop() {
	if a == nil {
		return
	}
	a.stopOnce.Do(func() {
		close(a.stopCh)
	})
	a.wg.Wait()
	a.Flush()
}

// Reconfigure 在不丢失历史流量的前提下更新审计配置。
func (a *TrafficAccounter) Reconfigure(cfg TrafficAuditConfig) {
	if a == nil {
		return
	}
	window := time.Duration(cfg.QuotaWindow)
	if window <= 0 {
		window = time.Hour
	}
	retention := cfg.RetentionDays
	if retention <= 0 {
		retention = DefaultTrafficRetentionDays
	}

	if a.enabled.Swap(cfg.Enabled) != cfg.Enabled {
		// 启停审计只重置配额窗口，不清空已持久化的历史流量。
		a.resetWindow()
	}
	a.perUserQuota.Store(cfg.PerUserQuota)
	a.perUpstreamQuota.Store(cfg.PerUpstreamQuota)
	if a.quotaWindow.Swap(int64(window)) != int64(window) {
		// 只有配额窗口变化时才重置计时器，避免普通上游编辑推迟窗口到期。
		select {
		case a.configCh <- struct{}{}:
		default:
		}
	}
	if a.retentionDays.Swap(int64(retention)) != int64(retention) {
		a.dirtyGen.Add(1)
	}
	// 配置变更（上游增删、启停等）时落盘一次，避免热加载后进程异常退出丢失最近明细。
	a.Flush()
}

func (a *TrafficAccounter) windowLoop() {
	defer a.wg.Done()
	for {
		window := time.Duration(a.quotaWindow.Load())
		if window <= 0 {
			window = time.Hour
		}
		timer := time.NewTimer(window)
		select {
		case <-timer.C:
			a.resetWindow()
		case <-a.configCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-a.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (a *TrafficAccounter) storeLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(trafficFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.Flush()
		case <-a.stopCh:
			return
		}
	}
}

func (a *TrafficAccounter) resetWindow() {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 仅重置配额窗口计数器，持久化历史继续保留。
	for _, usage := range a.userBytes {
		usage.Store(0)
	}
	for _, usage := range a.upstreamBytes {
		usage.Store(0)
	}
}

func (a *TrafficAccounter) retention() int {
	days := a.retentionDays.Load()
	if days <= 0 {
		return DefaultTrafficRetentionDays
	}
	return int(days)
}

func trafficHistoryKey(day string, k TrafficKey) string {
	return day + "\x00" + trafficKeyStr(k)
}

// trafficKeyStr 将 TrafficKey 转为 map key。
func trafficKeyStr(k TrafficKey) string {
	return k.User + "\x00" + k.Upstream + "\x00" + k.Target
}

// Record 记录流量并检查配额。返回 ErrQuotaExceeded 如果超出配额。
func (a *TrafficAccounter) Record(k TrafficKey, bytesIn, bytesOut int64) error {
	if a == nil || !a.enabled.Load() {
		return nil
	}
	if bytesIn < 0 || bytesOut < 0 {
		return nil
	}
	total := bytesIn + bytesOut
	if total <= 0 {
		return nil
	}

	a.recordHistory(k, bytesIn, bytesOut)

	return a.recordAggregates(k, bytesIn, bytesOut, total)
}

func (a *TrafficAccounter) recordHistory(k TrafficKey, bytesIn, bytesOut int64) {
	day := a.now().Format(time.DateOnly)
	mapKey := trafficHistoryKey(day, k)

	a.mu.RLock()
	entry := a.history[mapKey]
	a.mu.RUnlock()
	if entry == nil {
		a.mu.Lock()
		entry = a.history[mapKey]
		if entry == nil {
			if len(a.history) >= maxTrafficHistoryEntries {
				a.pruneHistoryLocked(a.now())
			}
			if len(a.history) >= maxTrafficHistoryEntries {
				a.mu.Unlock()
				return
			}
			entry = &trafficHistoryEntry{day: day, key: k}
			a.history[mapKey] = entry
		}
		a.mu.Unlock()
	}

	entry.bytesIn.Add(bytesIn)
	entry.bytesOut.Add(bytesOut)
	a.dirtyGen.Add(1)
}

// recordAggregates 更新用户和上游配额计数器以及 Prometheus 指标。
// 返回配额超限错误（如果有）。
func (a *TrafficAccounter) recordAggregates(k TrafficKey, bytesIn, bytesOut, total int64) error {
	perUserQuota := a.perUserQuota.Load()
	perUpstreamQuota := a.perUpstreamQuota.Load()

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
		if perUserQuota > 0 && ub.Load() > perUserQuota {
			return fmt.Errorf("%w: user %q: %d > %d", ErrQuotaExceeded, k.User, ub.Load(), perUserQuota)
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
		if perUpstreamQuota > 0 && ub.Load() > perUpstreamQuota {
			return fmt.Errorf("%w: upstream %q: %d > %d", ErrQuotaExceeded, k.Upstream, ub.Load(), perUpstreamQuota)
		}
	}

	// 更新 Prometheus 指标（target 标签做归一化以限制基数）
	normalizedTarget := normalizeTargetLabel(k.Target)
	MetricAdd("yigate_traffic_bytes_total", float64(bytesIn), "direction=client_to_target", "user="+k.User, "upstream="+k.Upstream, "target="+normalizedTarget)
	MetricAdd("yigate_traffic_bytes_total", float64(bytesOut), "direction=target_to_client", "user="+k.User, "upstream="+k.Upstream, "target="+normalizedTarget)

	return nil
}

// pruneHistoryLocked 删除超过保留天数的明细，调用方必须持有 a.mu 写锁。
func (a *TrafficAccounter) pruneHistoryLocked(now time.Time) int {
	days := a.retention()
	cutoff := now.AddDate(0, 0, -(days - 1)).Format(time.DateOnly)
	removed := 0
	for key, entry := range a.history {
		if entry.day < cutoff {
			delete(a.history, key)
			removed++
		}
	}
	return removed
}

// Flush 将当前流量明细原子写入磁盘。没有变化时不会重复写。
func (a *TrafficAccounter) Flush() {
	if a == nil || a.storePath == "" {
		return
	}
	a.flushMu.Lock()
	defer a.flushMu.Unlock()

	// 内存中没有任何明细但磁盘上仍有历史时，先合并回来再写盘，
	// 避免统计器重建或加载异常时用空数据把已持久化的记录清空。
	a.mu.RLock()
	empty := len(a.history) == 0
	a.mu.RUnlock()
	if empty && a.dirtyGen.Load() != a.flushedAt.Load() {
		if diskEntries, err := loadTrafficHistory(a.storePath); err == nil && len(diskEntries) > 0 {
			a.mu.Lock()
			if merged := a.mergeHistoryLocked(diskEntries); merged > 0 {
				a.dirtyGen.Add(1)
				Logger().Warnw("traffic history was empty in memory, restored from disk",
					"path", a.storePath, "entries", merged)
			}
			a.mu.Unlock()
		}
	}

	a.mu.Lock()
	if pruned := a.pruneHistoryLocked(a.now()); pruned > 0 {
		a.dirtyGen.Add(1)
	}
	gen := a.dirtyGen.Load()
	if gen == a.flushedAt.Load() {
		a.mu.Unlock()
		return
	}

	entries := make([]trafficHistoryDiskEntry, 0, len(a.history))
	for _, entry := range a.history {
		entries = append(entries, trafficHistoryDiskEntry{
			Day:      entry.day,
			User:     entry.key.User,
			Upstream: entry.key.Upstream,
			Target:   entry.key.Target,
			BytesIn:  entry.bytesIn.Load(),
			BytesOut: entry.bytesOut.Load(),
		})
	}
	a.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Day != entries[j].Day {
			return entries[i].Day < entries[j].Day
		}
		if entries[i].User != entries[j].User {
			return entries[i].User < entries[j].User
		}
		if entries[i].Upstream != entries[j].Upstream {
			return entries[i].Upstream < entries[j].Upstream
		}
		return entries[i].Target < entries[j].Target
	})

	if err := writeTrafficHistory(a.storePath, entries); err != nil {
		Logger().Warnw("write traffic history failed", "path", a.storePath, "error", err)
		return
	}
	a.flushedAt.Store(gen)
}

// TrafficSnapshot 是流量统计快照。
type TrafficSnapshot struct {
	Enabled       bool                   `json:"enabled"`
	RetentionDays int                    `json:"retention_days"`
	Entries       []TrafficEntrySnapshot `json:"entries"`
}

type TrafficEntrySnapshot struct {
	User     string `json:"user"`
	Upstream string `json:"upstream"`
	Target   string `json:"target"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
	Total    int64  `json:"total"`
}

// Snapshot 返回保留周期内的累计流量统计快照。
func (a *TrafficAccounter) Snapshot() TrafficSnapshot {
	if a == nil {
		return TrafficSnapshot{}
	}
	retention := a.retention()
	cutoff := a.now().AddDate(0, 0, -(retention - 1)).Format(time.DateOnly)
	aggregated := make(map[string]*TrafficEntrySnapshot)

	a.mu.RLock()
	for _, entry := range a.history {
		if entry.day < cutoff {
			continue
		}
		key := trafficKeyStr(entry.key)
		item, ok := aggregated[key]
		if !ok {
			item = &TrafficEntrySnapshot{
				User:     entry.key.User,
				Upstream: entry.key.Upstream,
				Target:   entry.key.Target,
			}
			aggregated[key] = item
		}
		item.BytesIn += entry.bytesIn.Load()
		item.BytesOut += entry.bytesOut.Load()
		item.Total = item.BytesIn + item.BytesOut
	}
	a.mu.RUnlock()

	entries := make([]TrafficEntrySnapshot, 0, len(aggregated))
	for _, item := range aggregated {
		entries = append(entries, *item)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Total > entries[j].Total
	})
	return TrafficSnapshot{
		Enabled:       a.enabled.Load(),
		RetentionDays: retention,
		Entries:       entries,
	}
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
// 热加载会与请求协程并发替换和读取，必须使用原子指针避免数据竞争。
var globalTrafficAccounter atomic.Pointer[TrafficAccounter]

// SetGlobalTrafficAccounter 设置全局流量统计器。
func SetGlobalTrafficAccounter(a *TrafficAccounter) {
	globalTrafficAccounter.Store(a)
}

func getGlobalTrafficAccounter() *TrafficAccounter {
	return globalTrafficAccounter.Load()
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
