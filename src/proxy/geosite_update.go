package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	minGeoSiteDatSize      = 1024
	geoSiteDownloadTimeout = 5 * time.Minute
)

type GeoSiteUpdater struct {
	cfg  GeoSiteConfig
	mgr  *UpstreamManager
	done chan struct{}
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
	req.Header.Set("User-Agent", "k5pw-geosite-updater/1.0")

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
	for i := 0; i < u.mgr.AttemptCount(nil); i++ {
		up := u.mgr.SelectFor(nil)
		if up == nil {
			break
		}
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
	written, copyErr := io.Copy(f, io.LimitReader(body, 64<<20))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
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
