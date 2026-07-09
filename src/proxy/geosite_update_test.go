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
