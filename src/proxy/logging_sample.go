package proxy

import (
	"sync"
	"time"
)

// LogSampler 按 key 对高频告警限频。
//
// 故障期最不缺的就是重复告警：fd 耗尽时每个 Accept 都报错、UDP 目标不可达时
// 每个会话都报错。这些日志本身就是放大器——刷爆磁盘、拖慢 IO，还会把真正
// 重要的第一条错误淹掉。采样器在固定窗口内每个 key 只放行一条，窗口内被抑制
// 的条数会附带在下一次输出里（suppressed=N），因此"出了什么问题、有多频繁"
// 两样信息都不丢。
type LogSampler struct {
	mu         sync.Mutex
	window     time.Duration
	maxEntries int
	entries    map[string]*logSampleEntry
}

type logSampleEntry struct {
	windowStart time.Time
	suppressed  int
}

// maxLogSampleKeys 限制采样表的 key 数量。调用方应使用固定的热点类别作 key
// （例如 udp-session-read），不要把目标地址、错误文本放进去。
const maxLogSampleKeys = 1024

func NewLogSampler(window time.Duration) *LogSampler {
	if window <= 0 {
		window = time.Second
	}
	return &LogSampler{
		window:     window,
		maxEntries: maxLogSampleKeys,
		entries:    make(map[string]*logSampleEntry),
	}
}

// allow 返回本次是否应输出，以及需要附带报告的被抑制条数。
func (s *LogSampler) allow(key string, now time.Time) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if ok && now.Sub(entry.windowStart) < s.window {
		entry.suppressed++
		return false, 0
	}
	suppressed := 0
	if ok {
		suppressed = entry.suppressed
		entry.windowStart = now
		entry.suppressed = 0
		return true, suppressed
	}
	if len(s.entries) >= s.maxEntries {
		// 先清理已经过期的窗口；仍然超限时放行日志，
		// 宁可多打一条也不能因为采样表满而丢错误。
		for k, e := range s.entries {
			if now.Sub(e.windowStart) >= s.window {
				delete(s.entries, k)
			}
		}
		if len(s.entries) >= s.maxEntries {
			return true, 0
		}
	}
	s.entries[key] = &logSampleEntry{windowStart: now}
	return true, 0
}

// Warnw 在采样通过后输出 warn 日志。
func (s *LogSampler) Warnw(key, msg string, kv ...interface{}) {
	emit, suppressed := s.allow(key, time.Now())
	if !emit {
		return
	}
	if suppressed > 0 {
		kv = append(kv, "suppressed", suppressed)
	}
	Logger().Warnw(msg, kv...)
}

// sampledLogs 是进程级默认采样器：同一热点每秒最多一条。
var sampledLogs = NewLogSampler(time.Second)

// LogWarnSampled 对热路径告警做限频，key 使用固定热点类别。
func LogWarnSampled(key, msg string, kv ...interface{}) {
	sampledLogs.Warnw(key, msg, kv...)
}
