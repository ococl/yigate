package proxy

import (
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
		UpdateURL:   server.URL,
		AutoUpdate:  true,
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
		AutoUpdate:  false, // no auto-download, only detect user-provided file
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
