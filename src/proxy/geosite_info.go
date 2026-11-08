package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// geoSiteMetaSuffix 是 GeoSite 数据文件旁路元数据的后缀。
	geoSiteMetaSuffix = ".meta.json"
	// geoSiteMetaSchema 是元数据文件的结构版本。
	geoSiteMetaSchema = 1
	// geoSiteVersionDisplayLen 是回显用短版本号长度（SHA-256 前 12 位）。
	geoSiteVersionDisplayLen = 12
	// geoSiteDatDownloadPath 是下游同步的固定下载路径。
	GeoSiteDatDownloadPath = "/api/v1/geosite/dat"
)

// geoSiteMeta 记录 GeoSite 数据文件的来源与版本，落盘为 <path>.meta.json。
// 仅保存下载/同步得到的元数据；用户手工放置的文件没有该文件，版本号按内容计算。
type geoSiteMeta struct {
	Schema        int       `json:"schema"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	UpdatedAt     time.Time `json:"updated_at"`
	SourceURL     string    `json:"source_url,omitempty"`
	ETag          string    `json:"etag,omitempty"`
	LastModified  string    `json:"last_modified,omitempty"`
	LastCheckedAt time.Time `json:"last_checked_at"`
}

// GeoSiteInfo 是 GeoSite 数据的版本与配置视图，供管理 API 与 WebUI 回显。
type GeoSiteInfo struct {
	Path                  string     `json:"path"`
	DownloadPath          string     `json:"download_path"`
	Exists                bool       `json:"exists"`
	Size                  int64      `json:"size"`
	Version               string     `json:"version"`
	SHA256                string     `json:"sha256"`
	UpdatedAt             *time.Time `json:"updated_at,omitempty"`
	SourceURL             string     `json:"source_url,omitempty"`
	LastCheckedAt         *time.Time `json:"last_checked_at,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
	AutoUpdate            bool       `json:"auto_update"`
	UpdateURL             string     `json:"update_url"`
	UpdateIntervalSeconds int64      `json:"update_interval_seconds"`
	UpdateTokenSet        bool       `json:"update_token_set"`
	SyncEnabled           bool       `json:"sync_enabled"`
	SyncTokenSet          bool       `json:"sync_token_set"`
}

// GeoSiteSyncConfig 描述本节点对外提供的只读同步能力。
type GeoSiteSyncConfig struct {
	Enabled bool
	Token   string
}

func geoSiteMetaPath(datPath string) string {
	if datPath == "" {
		return ""
	}
	return datPath + geoSiteMetaSuffix
}

// loadGeoSiteMeta 读取数据文件旁的元数据，缺失或损坏时返回 false。
func loadGeoSiteMeta(datPath string) (geoSiteMeta, bool) {
	var meta geoSiteMeta
	path := geoSiteMetaPath(datPath)
	if path == "" {
		return meta, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return meta, false
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return meta, false
	}
	if meta.Schema != geoSiteMetaSchema {
		return meta, false
	}
	return meta, true
}

// saveGeoSiteMeta 原子写入元数据文件，失败不影响主流程。
func saveGeoSiteMeta(datPath string, meta geoSiteMeta) error {
	path := geoSiteMetaPath(datPath)
	if path == "" {
		return fmt.Errorf("geosite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	meta.Schema = geoSiteMetaSchema
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// DatPath 返回当前 GeoSite 数据文件路径（可能不存在）。
func (u *GeoSiteUpdater) DatPath() string {
	if u == nil {
		return ""
	}
	return u.cfg.Path
}

// SyncConfig 返回本节点对外提供的只读同步配置。
func (u *GeoSiteUpdater) SyncConfig() GeoSiteSyncConfig {
	if u == nil {
		return GeoSiteSyncConfig{}
	}
	return GeoSiteSyncConfig{
		Enabled: u.cfg.SyncEnabled,
		Token:   strings.TrimSpace(u.cfg.SyncToken),
	}
}

// UpdateURLOrDefault 返回实际使用的更新地址。
func (u *GeoSiteUpdater) UpdateURLOrDefault() string {
	if u == nil {
		return ""
	}
	if url := strings.TrimSpace(u.cfg.UpdateURL); url != "" {
		return url
	}
	return defaultGeoSiteUpdateURL
}

// Info 汇总当前 GeoSite 数据文件的版本信息与更新配置。
func (u *GeoSiteUpdater) Info() GeoSiteInfo {
	if u == nil {
		return GeoSiteInfo{}
	}
	info := GeoSiteInfo{
		Path:                  u.cfg.Path,
		DownloadPath:          GeoSiteDatDownloadPath,
		AutoUpdate:            u.cfg.AutoUpdate,
		UpdateURL:             RedactURL(u.UpdateURLOrDefault()),
		UpdateIntervalSeconds: int64(time.Duration(u.cfg.UpdateInterval).Seconds()),
		UpdateTokenSet:        strings.TrimSpace(u.cfg.UpdateToken) != "",
		SyncEnabled:           u.cfg.SyncEnabled,
		SyncTokenSet:          strings.TrimSpace(u.cfg.SyncToken) != "",
		LastError:             u.LastError(),
	}
	meta, hasMeta := loadGeoSiteMeta(u.cfg.Path)
	if hasMeta {
		info.SourceURL = meta.SourceURL
		if !meta.LastCheckedAt.IsZero() {
			checked := meta.LastCheckedAt.UTC()
			info.LastCheckedAt = &checked
		}
	}
	if u.cfg.Path == "" {
		return info
	}
	stat, err := os.Stat(u.cfg.Path)
	if err != nil || stat.IsDir() {
		return info
	}
	info.Exists = true
	info.Size = stat.Size()

	updatedAt := stat.ModTime().UTC()
	if hasMeta && !meta.UpdatedAt.IsZero() {
		updatedAt = meta.UpdatedAt.UTC()
	}
	info.UpdatedAt = &updatedAt

	sha := ""
	if hasMeta && meta.SHA256 != "" && meta.Size == stat.Size() {
		sha = meta.SHA256
		u.primeHashCache(sha, stat.Size(), stat.ModTime())
	}
	if sha == "" {
		if sum, _, _, err := u.fileSHA256(); err == nil {
			sha = sum
		}
	}
	info.SHA256 = sha
	if len(sha) > geoSiteVersionDisplayLen {
		info.Version = sha[:geoSiteVersionDisplayLen]
	} else {
		info.Version = sha
	}
	return info
}

// LastError 返回最近一次自动/手动更新失败的原因（仅内存态）。
func (u *GeoSiteUpdater) LastError() string {
	if u == nil {
		return ""
	}
	u.errMu.RLock()
	defer u.errMu.RUnlock()
	return u.lastErr
}

func (u *GeoSiteUpdater) setLastError(err error) {
	if u == nil {
		return
	}
	u.errMu.Lock()
	defer u.errMu.Unlock()
	if err == nil {
		u.lastErr = ""
		return
	}
	u.lastErr = err.Error()
}

func (u *GeoSiteUpdater) primeHashCache(sum string, size int64, modTime time.Time) {
	u.hashMu.Lock()
	defer u.hashMu.Unlock()
	u.hashSum = sum
	u.hashSize = size
	u.hashMod = modTime
}

// fileSHA256 计算数据文件内容摘要，按 (size, modtime) 缓存避免重复读取。
func (u *GeoSiteUpdater) fileSHA256() (string, int64, time.Time, error) {
	stat, err := os.Stat(u.cfg.Path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	u.hashMu.Lock()
	defer u.hashMu.Unlock()
	if u.hashSum != "" && u.hashSize == stat.Size() && u.hashMod.Equal(stat.ModTime()) {
		return u.hashSum, u.hashSize, u.hashMod, nil
	}
	f, err := os.Open(u.cfg.Path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, time.Time{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	u.hashSum = sum
	u.hashSize = stat.Size()
	u.hashMod = stat.ModTime()
	return sum, stat.Size(), stat.ModTime(), nil
}
