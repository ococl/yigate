package proxy

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// AccessControlConfig 定义访问控制规则。
type AccessControlConfig struct {
	AllowedClients []string `yaml:"allowed_clients"` // CIDR 列表，空表示允许所有
	RateLimit      struct {
		ConnectionsPerSecond int `yaml:"connections_per_second"` // 单个 IP 每秒新建连接数
		Burst                int `yaml:"burst"`                  // 令牌桶突发容量
	} `yaml:"rate_limit"`
	MaxConnsPerUser int `yaml:"max_conns_per_user"` // 单个认证用户最大并发连接数，0 表示不限制
}

// AccessController 在连接入口执行 IP 白名单、速率限制和用户并发限制。
type AccessController struct {
	cfg         AccessControlConfig
	allowedNets []*net.IPNet

	mu           sync.Mutex
	userConns    map[string]int
	rateLimiters map[string]*rateLimiter
}

// rateLimiter 是简单令牌桶限速器。
type rateLimiter struct {
	rate     float64
	burst    int
	tokens   float64
	last     time.Time
	lastUsed time.Time
	mu       sync.Mutex
}

func newRateLimiter(r float64, burst int) *rateLimiter {
	now := time.Now()
	return &rateLimiter{
		rate:     r,
		burst:    burst,
		tokens:   float64(burst),
		last:     now,
		lastUsed: now,
	}
}

func (l *rateLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.lastUsed = now
	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// NewAccessController 根据配置创建访问控制器。
func NewAccessController(cfg AccessControlConfig) (*AccessController, error) {
	a := &AccessController{
		cfg:          cfg,
		userConns:    make(map[string]int),
		rateLimiters: make(map[string]*rateLimiter),
	}
	for _, cidr := range cfg.AllowedClients {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed_clients CIDR %q: %w", cidr, err)
		}
		a.allowedNets = append(a.allowedNets, network)
	}
	return a, nil
}

// IsAllowedIP 检查客户端 IP 是否在白名单中。白名单为空时允许所有。
func (a *AccessController) IsAllowedIP(addr net.Addr) bool {
	if a == nil || len(a.allowedNets) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range a.allowedNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// AllowConn 对单个 IP 执行速率限制检查。
func (a *AccessController) AllowConn(addr net.Addr) bool {
	if a == nil || a.cfg.RateLimit.ConnectionsPerSecond <= 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	a.mu.Lock()
	lim, ok := a.rateLimiters[host]
	if !ok {
		burst := a.cfg.RateLimit.Burst
		if burst <= 0 {
			burst = a.cfg.RateLimit.ConnectionsPerSecond
		}
		lim = newRateLimiter(float64(a.cfg.RateLimit.ConnectionsPerSecond), burst)
		a.rateLimiters[host] = lim
	}
	a.mu.Unlock()
	return lim.allow()
}

// AcquireUserSlot 尝试为用户占用一个并发连接槽位。
func (a *AccessController) AcquireUserSlot(user string) bool {
	if a == nil || user == "" || a.cfg.MaxConnsPerUser <= 0 {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.userConns[user] >= a.cfg.MaxConnsPerUser {
		return false
	}
	a.userConns[user]++
	return true
}

// ReleaseUserSlot 释放用户的并发连接槽位。
func (a *AccessController) ReleaseUserSlot(user string) {
	if a == nil || user == "" || a.cfg.MaxConnsPerUser <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.userConns[user] > 0 {
		a.userConns[user]--
		if a.userConns[user] == 0 {
			delete(a.userConns, user)
		}
	}
}

// CleanupStaleRateLimiters 清理长时间未使用的限速器，避免内存无限增长。
// 删除超过 maxAge 未被访问的条目。
func (a *AccessController) CleanupStaleRateLimiters(maxAge time.Duration) {
	if a == nil || maxAge <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for host, lim := range a.rateLimiters {
		lim.mu.Lock()
		stale := lim.lastUsed.Before(cutoff)
		lim.mu.Unlock()
		if stale {
			delete(a.rateLimiters, host)
		}
	}
}

// StartCleanup 启动一个后台 goroutine，周期性清理过期的限速器。
func (a *AccessController) StartCleanup(interval, maxAge time.Duration) {
	if a == nil || interval <= 0 || maxAge <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			a.CleanupStaleRateLimiters(maxAge)
		}
	}()
}

// ErrAccessDenied 表示访问被拒绝。
var ErrAccessDenied = errors.New("access denied")

// ErrRateLimited 表示触发速率限制。
var ErrRateLimited = errors.New("rate limited")

// ErrUserConnsExceeded 表示用户并发连接数超限。
var ErrUserConnsExceeded = errors.New("user connection limit exceeded")
