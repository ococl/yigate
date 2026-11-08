package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	minGeoSiteDatSize      = 1024
	maxGeoSiteDatSize      = 64 << 20
	geoSiteDownloadTimeout = 5 * time.Minute

	// 缺失文件恢复参数
	maxMissingFileDownloadsPerDay = 10
	missingFileCheckInterval      = 30 * time.Minute
)

type GeoSiteUpdater struct {
	cfg           GeoSiteConfig
	mgr           *UpstreamManager
	done          chan struct{}
	downloadMu    sync.Mutex
	downloadCount int
	downloadDate  string

	// syncMu 串行化自动更新与手动更新，避免并发写同一文件。
	syncMu sync.Mutex
	// hashMu 保护内容摘要缓存，避免每次查询版本都重新读取整个数据文件。
	hashMu   sync.Mutex
	hashSum  string
	hashSize int64
	hashMod  time.Time
	// errMu 保护最近一次更新错误，用于界面回显。
	errMu   sync.RWMutex
	lastErr string
}

func NormalizeGeoSiteConfig(cfg *GeoSiteConfig) {
	if cfg == nil {
		return
	}
	if cfg.UpdateURL == "" {
		cfg.UpdateURL = defaultGeoSiteUpdateURL
	}
	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = Duration(defaultGeoSiteUpdateInterval)
	}
}

func NewGeoSiteUpdater(cfg GeoSiteConfig, mgr *UpstreamManager) *GeoSiteUpdater {
	NormalizeGeoSiteConfig(&cfg)
	return &GeoSiteUpdater{cfg: cfg, mgr: mgr, done: make(chan struct{})}
}

func (u *GeoSiteUpdater) Start(onUpdated func()) {
	if u == nil || !u.cfg.AutoUpdate || u.cfg.Path == "" {
		return
	}
	go u.loop(onUpdated)
}

func (u *GeoSiteUpdater) Stop() {
	if u == nil || u.done == nil {
		return
	}
	select {
	case <-u.done:
		return
	default:
		close(u.done)
	}
}

func (u *GeoSiteUpdater) loop(onUpdated func()) {
	interval := u.cfg.UpdateInterval.value(defaultGeoSiteUpdateInterval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-u.done:
			return
		case <-ticker.C:
			u.update(onUpdated)
		}
	}
}

func (u *GeoSiteUpdater) update(onUpdated func()) {
	changed, err := u.Sync(context.Background())
	if err != nil {
		Logger().Warnw("update geosite dat failed", "error", err)
		return
	}
	if !changed {
		Logger().Debugw("geosite dat is already up to date", "path", u.cfg.Path)
		return
	}
	Logger().Infow("geosite dat updated", "path", u.cfg.Path)
	if onUpdated != nil {
		onUpdated()
	}
}

// UpdateOnce 立即下载一次 GeoSite 数据，忽略内容是否发生变化。
func (u *GeoSiteUpdater) UpdateOnce() error {
	_, err := u.Sync(context.Background())
	return err
}

// Sync 从 update_url 拉取 GeoSite 数据，返回内容是否发生变化。
// 支持 If-None-Match 条件请求与 Bearer 令牌，便于下游从其他节点只读同步；
// 内容未变化时返回 304 且不重写文件，避免触发不必要的路由重载。
func (u *GeoSiteUpdater) Sync(ctx context.Context) (bool, error) {
	if u == nil {
		return false, nil
	}
	if strings.TrimSpace(u.cfg.Path) == "" {
		return false, fmt.Errorf("routing.geosite.path is required for update")
	}
	u.syncMu.Lock()
	defer u.syncMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	// url 变量统一保存脱敏地址，用于元数据、错误信息与管理 API 回显；
	// 实际请求仍使用含凭据的 rawURL，避免把 userinfo/token 写进日志。
	rawURL := u.UpdateURLOrDefault()
	url := RedactURL(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		u.setLastError(err)
		return false, err
	}
	req.Header.Set("User-Agent", "yigate-geosite-updater/1.1")
	if token := strings.TrimSpace(u.cfg.UpdateToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	meta, hasMeta := loadGeoSiteMeta(u.cfg.Path)
	// 仅在同一下载源下沿用条件请求头，避免切换数据源后错误复用旧 ETag。
	if hasMeta && meta.SourceURL == url {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		} else if meta.SHA256 != "" {
			req.Header.Set("If-None-Match", `"`+meta.SHA256+`"`)
		}
	}

	resp, err := u.doRequest(req)
	if err != nil {
		u.setLastError(err)
		return false, err
	}
	defer resp.Body.Close()

	now := time.Now().UTC()
	if resp.StatusCode == http.StatusNotModified {
		meta.LastCheckedAt = now
		meta.SourceURL = url
		_ = saveGeoSiteMeta(u.cfg.Path, meta)
		u.setLastError(nil)
		return false, nil
	}
	if resp.StatusCode/100 != 2 {
		err := fmt.Errorf("download %s returned %s", url, resp.Status)
		u.setLastError(err)
		return false, err
	}

	previous := ""
	if hasMeta {
		previous = meta.SHA256
	}
	if previous == "" {
		if sum, _, _, err := u.fileSHA256(); err == nil {
			previous = sum
		}
	}

	sum, written, err := writeGeoSiteAtomicallyHashed(u.cfg.Path, resp.Body)
	if err != nil {
		u.setLastError(err)
		return false, err
	}
	if stat, statErr := os.Stat(u.cfg.Path); statErr == nil {
		u.primeHashCache(sum, written, stat.ModTime())
	}
	newMeta := geoSiteMeta{
		SHA256:        sum,
		Size:          written,
		UpdatedAt:     now,
		SourceURL:     url,
		ETag:          strings.TrimSpace(resp.Header.Get("ETag")),
		LastModified:  strings.TrimSpace(resp.Header.Get("Last-Modified")),
		LastCheckedAt: now,
	}
	if err := saveGeoSiteMeta(u.cfg.Path, newMeta); err != nil {
		Logger().Warnw("save geosite meta failed", "error", err)
	}
	u.setLastError(nil)
	return sum != previous, nil
}

// geoSiteRedirectPolicy 限制 GeoSite 下载的重定向：每一跳都要重新通过主机校验
// （只校验初始 URL 挡不住 302 到元数据地址），跨主机跳转时移除更新令牌。
func geoSiteRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return fmt.Errorf("too many geosite download redirects")
	}
	if err := ValidateGeoSiteUpdateURL(req.URL.String()); err != nil {
		return fmt.Errorf("geosite download redirect rejected: %w", err)
	}
	if len(via) > 0 && !strings.EqualFold(via[0].URL.Host, req.URL.Host) {
		req.Header.Del("Authorization")
	}
	return nil
}

func (u *GeoSiteUpdater) doRequest(req *http.Request) (*http.Response, error) {
	if u.mgr != nil && u.mgr.AttemptCount(nil) > 0 {
		return u.requestViaAutoProxy(req)
	}
	client := &http.Client{Timeout: geoSiteDownloadTimeout, CheckRedirect: geoSiteRedirectPolicy}
	return client.Do(req)
}

func (u *GeoSiteUpdater) requestViaAutoProxy(req *http.Request) (*http.Response, error) {
	var lastErr error
	tried := make(map[*Upstream]struct{})
	for i := 0; i < u.mgr.AttemptCount(nil); i++ {
		up := u.mgr.SelectForExcluding(nil, tried)
		if up == nil {
			break
		}
		tried[up] = struct{}{}
		cloned := req.Clone(req.Context())
		resp, err := requestViaUpstreamClient(cloned, up)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		Logger().Warnw("download geosite via upstream failed", "upstream", up.Name, "error", err)
		// 下载 geosite 失败不应影响上游健康状态，可能是目标 URL 不可达
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available upstream")
	}
	return nil, lastErr
}

func requestViaUpstreamClient(req *http.Request, up *Upstream) (*http.Response, error) {
	transport := &http.Transport{}
	// 每次下载都新建 Transport，若不显式关闭空闲连接会一直残留；
	// CloseIdleConnections 只影响空闲连接，不会打断响应体仍在使用的连接。
	defer transport.CloseIdleConnections()
	if up.isSocks5() {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			conn, err := up.DialSOCKS5H(addr)
			if err != nil {
				return nil, err
			}
			// SOCKS5 握手本身不可取消，拨号后立即复核 ctx，避免把已取消请求继续发出。
			if err := ctx.Err(); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return conn, nil
		}
	} else {
		transport.Proxy = http.ProxyURL(up.urlForType(up.primaryProxyType()))
	}
	client := &http.Client{Transport: transport, Timeout: geoSiteDownloadTimeout, CheckRedirect: geoSiteRedirectPolicy}
	return client.Do(req)
}

// writeGeoSiteAtomicallyHashed 以原子方式写入数据文件，同时返回内容的 SHA-256 与字节数。
func writeGeoSiteAtomicallyHashed(path string, body io.Reader) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", 0, err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", 0, err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(body, maxGeoSiteDatSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return "", 0, copyErr
	}
	if written > maxGeoSiteDatSize {
		os.Remove(tmp)
		return "", 0, fmt.Errorf("downloaded geosite dat exceeds maximum size: %d bytes", maxGeoSiteDatSize)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", 0, closeErr
	}
	if written < minGeoSiteDatSize {
		os.Remove(tmp)
		return "", 0, fmt.Errorf("downloaded geosite dat is too small: %d bytes", written)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

// IsFileAvailable returns true if the geosite dat file exists and has a valid size.
func (u *GeoSiteUpdater) IsFileAvailable() bool {
	if u == nil || u.cfg.Path == "" {
		return false
	}
	info, err := os.Stat(u.cfg.Path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() >= minGeoSiteDatSize
}

// StartMissingFileRecovery starts a background loop that periodically checks
// whether geosite.dat is available. If the file appears (user-provided) or is
// auto-downloaded (max maxMissingFileDownloadsPerDay times per day, requires
// auto_update), it triggers onUpdated to reload the router and restore geosite
// matching.
func (u *GeoSiteUpdater) StartMissingFileRecovery(onUpdated func()) {
	if u == nil || u.cfg.Path == "" {
		return
	}
	go u.missingFileLoop(onUpdated)
}

func (u *GeoSiteUpdater) missingFileLoop(onUpdated func()) {
	ticker := time.NewTicker(missingFileCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-u.done:
			return
		case <-ticker.C:
			if u.IsFileAvailable() {
				Logger().Infow("geosite dat file is now available, reloading", "path", u.cfg.Path)
				if onUpdated != nil {
					onUpdated()
				}
				return
			}
			if !u.cfg.AutoUpdate {
				continue
			}
			if !u.canDownloadToday() {
				Logger().Warnw("geosite dat auto-download daily limit reached",
					"limit", maxMissingFileDownloadsPerDay)
				continue
			}
			u.recordDownload()
			if err := u.UpdateOnce(); err != nil {
				Logger().Warnw("geosite dat auto-download failed", "error", err)
				continue
			}
			Logger().Infow("geosite dat auto-download succeeded", "path", u.cfg.Path)
			if onUpdated != nil {
				onUpdated()
			}
			return
		}
	}
}

func (u *GeoSiteUpdater) canDownloadToday() bool {
	u.downloadMu.Lock()
	defer u.downloadMu.Unlock()
	today := time.Now().Format("2006-01-02")
	if u.downloadDate != today {
		u.downloadDate = today
		u.downloadCount = 0
	}
	return u.downloadCount < maxMissingFileDownloadsPerDay
}

func (u *GeoSiteUpdater) recordDownload() {
	u.downloadMu.Lock()
	defer u.downloadMu.Unlock()
	today := time.Now().Format("2006-01-02")
	if u.downloadDate != today {
		u.downloadDate = today
		u.downloadCount = 0
	}
	u.downloadCount++
}
