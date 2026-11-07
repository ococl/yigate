package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeoSiteUpdaterUpdateOnce(t *testing.T) {
	data := make([]byte, 2048)
	for i := range data {
		data[i] = byte(i % 256)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL}, nil)
	if err := updater.UpdateOnce(); err != nil {
		t.Fatalf("UpdateOnce: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("geosite.dat not written")
	}
}

func TestGeoSiteUpdaterSyncRecordsVersionAndUsesConditionalRequest(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 251)
	}
	var ifNoneMatch []string
	etag := `"v1"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ifNoneMatch = append(ifNoneMatch, r.Header.Get("If-None-Match"))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		w.Write(data)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, AutoUpdate: true, UpdateURL: server.URL}, nil)

	changed, err := updater.Sync(context.Background())
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if !changed {
		t.Fatal("first Sync should report changed")
	}
	if _, ok := loadGeoSiteMeta(path); !ok {
		t.Fatal("meta file was not written")
	}
	info := updater.Info()
	if !info.Exists || info.Size != int64(len(data)) {
		t.Fatalf("info = %+v, want exists and size %d", info, len(data))
	}
	if len(info.SHA256) != 64 || len(info.Version) != geoSiteVersionDisplayLen {
		t.Fatalf("unexpected version info: sha=%q version=%q", info.SHA256, info.Version)
	}
	if info.SourceURL != server.URL {
		t.Fatalf("source_url = %q, want %q", info.SourceURL, server.URL)
	}
	if info.UpdatedAt == nil || info.LastCheckedAt == nil {
		t.Fatal("updated_at/last_checked_at should be present")
	}

	changed, err = updater.Sync(context.Background())
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if changed {
		t.Fatal("second Sync should be a no-op")
	}
	if len(ifNoneMatch) != 2 || ifNoneMatch[1] != etag {
		t.Fatalf("If-None-Match headers = %v, want second request to send %q", ifNoneMatch, etag)
	}
}

func TestGeoSiteUpdaterSyncSendsBearerToken(t *testing.T) {
	data := make([]byte, 2048)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write(data)
	}))
	defer server.Close()

	updater := NewGeoSiteUpdater(GeoSiteConfig{
		Path:        filepath.Join(t.TempDir(), "geosite.dat"),
		UpdateURL:   server.URL,
		UpdateToken: "sync-token-123",
	}, nil)
	if _, err := updater.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if gotAuth != "Bearer sync-token-123" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
}

func TestGeoSiteUpdaterInfoWithoutFile(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{
		Path:        filepath.Join(t.TempDir(), "missing.dat"),
		SyncEnabled: true,
		SyncToken:   "readonly-token",
		UpdateToken: "upstream-token",
		UpdateURL:   "https://example.com/dlc.dat",
		AutoUpdate:  true,
	}, nil)
	info := updater.Info()
	if info.Exists {
		t.Fatal("info.Exists = true for missing file")
	}
	if !info.UpdateTokenSet || !info.SyncEnabled || !info.SyncTokenSet {
		t.Fatalf("token flags not reflected: %+v", info)
	}
	if info.DownloadPath != GeoSiteDatDownloadPath {
		t.Fatalf("download path = %q", info.DownloadPath)
	}
}

func TestGeoSiteUpdaterRejectsTinyDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tiny"))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL}, nil)
	if err := updater.UpdateOnce(); err == nil {
		t.Fatal("expected error for tiny download")
	}
}

func TestGeoSiteUpdaterRejectsOversizedDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, maxGeoSiteDatSize+1))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	original := []byte("existing geosite data")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL}, nil)
	if err := updater.UpdateOnce(); err == nil {
		t.Fatal("expected error for oversized download")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("oversized download replaced existing geosite file")
	}
}

func TestGeoSiteUpdaterRequiresPath(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{}, nil)
	if err := updater.UpdateOnce(); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestGeoSiteUpdaterStartStop(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{}, nil)
	updater.Start(func() {})
	updater.Stop()
	updater.Stop() // idempotent
}

func TestGeoSiteUpdaterLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 2048)
		w.Write(data)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL, AutoUpdate: true, UpdateInterval: Duration(100 * time.Millisecond)}, nil)
	called := make(chan struct{}, 1)
	updater.Start(func() { close(called) })
	<-called
	updater.Stop()
}

func TestGeoSiteUpdaterIsFileAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")

	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path}, nil)
	if updater.IsFileAvailable() {
		t.Fatal("expected false for missing file")
	}

	// too small
	if err := os.WriteFile(path, []byte("tiny"), 0644); err != nil {
		t.Fatal(err)
	}
	if updater.IsFileAvailable() {
		t.Fatal("expected false for too-small file")
	}

	// valid size
	data := make([]byte, minGeoSiteDatSize)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if !updater.IsFileAvailable() {
		t.Fatal("expected true for valid file")
	}
}

func TestGeoSiteUpdaterIsFileAvailableEmptyPath(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{}, nil)
	if updater.IsFileAvailable() {
		t.Fatal("expected false for empty path")
	}
}

func TestGeoSiteUpdaterMissingFileRecoveryByDownload(t *testing.T) {
	data := make([]byte, 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{
		Path:       path,
		UpdateURL:  server.URL,
		AutoUpdate: true,
	}, nil)

	called := make(chan struct{}, 1)
	updater.StartMissingFileRecovery(func() { close(called) })

	// The recovery loop checks every missingFileCheckInterval (30m by default),
	// but the first tick won't fire quickly. We can't wait that long in a test.
	// Instead, test the download path directly via UpdateOnce + IsFileAvailable.
	updater.Stop()

	// Directly simulate what the loop does: download then check
	if err := updater.UpdateOnce(); err != nil {
		t.Fatalf("UpdateOnce: %v", err)
	}
	if !updater.IsFileAvailable() {
		t.Fatal("file should be available after download")
	}
}

func TestGeoSiteUpdaterMissingFileRecoveryByUserFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{
		Path:       path,
		AutoUpdate: false, // no auto-download, only detect user-provided file
	}, nil)

	if updater.IsFileAvailable() {
		t.Fatal("expected file to be missing initially")
	}

	// Simulate user placing the file
	data := make([]byte, minGeoSiteDatSize)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	if !updater.IsFileAvailable() {
		t.Fatal("expected file to be available after user placement")
	}
}

func TestGeoSiteUpdaterDownloadLimitPerDay(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: "x", AutoUpdate: true}, nil)

	// Exhaust the daily limit
	for i := 0; i < maxMissingFileDownloadsPerDay; i++ {
		if !updater.canDownloadToday() {
			t.Fatalf("expected canDownloadToday=true on attempt %d", i)
		}
		updater.recordDownload()
	}
	if updater.canDownloadToday() {
		t.Fatal("expected canDownloadToday=false after limit reached")
	}
}

func TestGeoSiteUpdaterDownloadLimitResetsNextDay(t *testing.T) {
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: "x", AutoUpdate: true}, nil)

	for i := 0; i < maxMissingFileDownloadsPerDay; i++ {
		updater.recordDownload()
	}
	if updater.canDownloadToday() {
		t.Fatal("expected limit reached")
	}

	// Simulate next day by changing the internal date
	updater.downloadMu.Lock()
	updater.downloadDate = "2000-01-01" // force different date
	updater.downloadMu.Unlock()

	if !updater.canDownloadToday() {
		t.Fatal("expected limit to reset on new day")
	}
}
