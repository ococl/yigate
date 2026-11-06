package proxy

import (
	"runtime"
	"sync"
	"time"
)

var (
	processStart = time.Now()

	buildInfoMu  sync.RWMutex
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    = "unknown"
)

// SetBuildInfo 由 main 在启动时注入版本信息，供管理接口展示。
func SetBuildInfo(version, commit, built string) {
	buildInfoMu.Lock()
	defer buildInfoMu.Unlock()
	if version != "" {
		buildVersion = version
	}
	if commit != "" {
		buildCommit = commit
	}
	if built != "" {
		buildTime = built
	}
}

// BuildInfo 返回版本信息。
func BuildInfo() (version, commit, built string) {
	buildInfoMu.RLock()
	defer buildInfoMu.RUnlock()
	return buildVersion, buildCommit, buildTime
}

// VersionInfo 是 /api/v1/version 的响应体。
type VersionInfo struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuildTime     string `json:"build_time"`
	GoVersion     string `json:"go_version"`
	Platform      string `json:"platform"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// CurrentVersionInfo 构造当前进程的版本信息。
func CurrentVersionInfo() VersionInfo {
	v, c, b := BuildInfo()
	return VersionInfo{
		Version:       v,
		Commit:        c,
		BuildTime:     b,
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		UptimeSeconds: int64(time.Since(processStart).Seconds()),
	}
}
