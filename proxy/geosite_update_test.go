package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGeoSiteUpdaterUpdateOnce(t *testing.T) {
	payload := bytes.Repeat([]byte{0x0a, 0x01, 'x'}, 400)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL}, nil)
	if err := updater.UpdateOnce(); err != nil {
		t.Fatalf("UpdateOnce() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("updated file stat: %v", err)
	}
	if info.Size() != int64(len(payload)) {
		t.Fatalf("updated file size = %d, want %d", info.Size(), len(payload))
	}
}

func TestGeoSiteUpdaterRejectsTinyDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("too small"))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "geosite.dat")
	updater := NewGeoSiteUpdater(GeoSiteConfig{Path: path, UpdateURL: server.URL}, nil)
	if err := updater.UpdateOnce(); err == nil {
		t.Fatal("UpdateOnce() accepted a tiny download")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("geosite file should not exist, stat err = %v", err)
	}
}
