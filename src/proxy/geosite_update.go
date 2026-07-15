package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	if err := u.UpdateOnce(); err != nil {
		Logger().Warnw("update geosite dat failed", "error", err)
		return
	}
	Logger().Infow("geosite dat updated", "path", u.cfg.Path)
	if onUpdated != nil {
		onUpdated()
	}
}

func (u *GeoSiteUpdater) UpdateOnce() error {
	if u == nil {
		return nil
	}
	if u.cfg.Path == "" {
		return fmt.Errorf("routing.geosite.path is required for auto update")
	}
	url := u.cfg.UpdateURL
	if url == "" {
		url = defaultGeoSiteUpdateURL
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "yigate-geosite-updater/1.0")

	resp, err := u.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}
	return writeGeoSiteAtomically(u.cfg.Path, resp.Body)
}

func (u *GeoSiteUpdater) doRequest(req *http.Request) (*http.Response, error) {
	if u.mgr != nil && u.mgr.AttemptCount(nil) > 0 {
		return u.requestViaAutoProxy(req)
	}
	client := &http.Client{Timeout: geoSiteDownloadTimeout}
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
	if up.isSocks5() {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return up.DialSOCKS5H(addr)
		}
	} else {
		transport.Proxy = http.ProxyURL(up.urlForType(up.primaryProxyType()))
	}
	client := &http.Client{Transport: transport, Timeout: geoSiteDownloadTimeout}
	return client.Do(req)
}

func writeGeoSiteAtomically(path string, body io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(f, io.LimitReader(body, maxGeoSiteDatSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if written > maxGeoSiteDatSize {
		os.Remove(tmp)
		return fmt.Errorf("downloaded geosite dat exceeds maximum size: %d bytes", maxGeoSiteDatSize)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if written < minGeoSiteDatSize {
		os.Remove(tmp)
		return fmt.Errorf("downloaded geosite dat is too small: %d bytes", written)
	}
	return os.Rename(tmp, path)
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
