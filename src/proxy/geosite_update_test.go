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
