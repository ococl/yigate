package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testManagementToken = "admin-token"

type stubGeoSiteAccess struct {
	info        GeoSiteInfo
	sync        GeoSiteSyncConfig
	datPath     string
	updateInfo  GeoSiteInfo
	changed     bool
	updateErr   error
	updateCalls int
}

func (s *stubGeoSiteAccess) Info() GeoSiteInfo {
	return s.info
}

func (s *stubGeoSiteAccess) Update(context.Context) (GeoSiteInfo, bool, error) {
	s.updateCalls++
	return s.updateInfo, s.changed, s.updateErr
}

func (s *stubGeoSiteAccess) DatPath() string {
	return s.datPath
}

func (s *stubGeoSiteAccess) SyncConfig() GeoSiteSyncConfig {
	return s.sync
}

func newGeoSiteTestServer(access GeoSiteAccess) *ManagementServer {
	m := NewManagementServer(func() HealthStatus { return HealthStatus{Status: "up"} }, false, testManagementToken)
	if access != nil {
		m.SetGeoSiteAccess(access)
	}
	return m
}

func geoSiteRequest(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestGeoSiteAPIInfoRequiresAuth(t *testing.T) {
	m := newGeoSiteTestServer(&stubGeoSiteAccess{info: GeoSiteInfo{Path: "/tmp/x.dat"}})
	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGeoSiteAPIInfoAllowsSyncToken(t *testing.T) {
	access := &stubGeoSiteAccess{
		info: GeoSiteInfo{Path: "/tmp/geosite.dat", Exists: true, Version: "abcdef123456"},
		sync: GeoSiteSyncConfig{Enabled: true, Token: "readonly-token"},
	}
	m := newGeoSiteTestServer(access)
	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite", "readonly-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var info GeoSiteInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.Version != "abcdef123456" {
		t.Fatalf("version = %q", info.Version)
	}
}

func TestGeoSiteAPIInfoRejectsSyncTokenWhenDisabled(t *testing.T) {
	m := newGeoSiteTestServer(&stubGeoSiteAccess{
		info: GeoSiteInfo{Path: "/tmp/geosite.dat"},
		sync: GeoSiteSyncConfig{Enabled: false, Token: "readonly-token"},
	})
	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite", "readonly-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGeoSiteAPIUpdateRequiresManagementToken(t *testing.T) {
	access := &stubGeoSiteAccess{
		info:       GeoSiteInfo{Path: "/tmp/geosite.dat"},
		sync:       GeoSiteSyncConfig{Enabled: true, Token: "readonly-token"},
		datPath:    "/tmp/geosite.dat",
		updateInfo: GeoSiteInfo{Path: "/tmp/geosite.dat", Version: "new-version"},
		changed:    true,
	}
	m := newGeoSiteTestServer(access)

	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodPost, "/api/v1/geosite/update", "readonly-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sync token must not update: status = %d", rec.Code)
	}
	if access.updateCalls != 0 {
		t.Fatal("update should not be called with sync token")
	}

	rec = httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodPost, "/api/v1/geosite/update", testManagementToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp GeoSiteUpdateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" || !resp.Changed || resp.Info.Version != "new-version" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestGeoSiteAPIDatServesConditionalDownload(t *testing.T) {
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 97)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	access := &stubGeoSiteAccess{
		info:    GeoSiteInfo{Path: path, Exists: true, SHA256: hex.EncodeToString(sum[:]), Version: "v1"},
		datPath: path,
	}
	m := newGeoSiteTestServer(access)

	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite/dat", testManagementToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag header")
	}
	if got := rec.Body.Len(); got != len(content) {
		t.Fatalf("body size = %d, want %d", got, len(content))
	}
	if rec.Header().Get("X-GeoSite-Version") != "v1" {
		t.Fatalf("X-GeoSite-Version = %q", rec.Header().Get("X-GeoSite-Version"))
	}

	req := geoSiteRequest(http.MethodGet, "/api/v1/geosite/dat", testManagementToken)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 response should have empty body, got %d bytes", rec.Body.Len())
	}
}

func TestGeoSiteAPIDatMissingFile(t *testing.T) {
	m := newGeoSiteTestServer(&stubGeoSiteAccess{datPath: filepath.Join(t.TempDir(), "missing.dat")})
	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite/dat", testManagementToken))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGeoSiteAPIMethodAndPathValidation(t *testing.T) {
	m := newGeoSiteTestServer(&stubGeoSiteAccess{info: GeoSiteInfo{Path: "/tmp/geosite.dat"}})

	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodPut, "/api/v1/geosite", testManagementToken))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}

	rec = httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite/unknown", testManagementToken))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGeoSiteAPIWithoutAccess(t *testing.T) {
	m := newGeoSiteTestServer(nil)
	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodGet, "/api/v1/geosite", testManagementToken))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestETagMatches(t *testing.T) {
	cases := []struct {
		header string
		etag   string
		want   bool
	}{
		{`"abc"`, `"abc"`, true},
		{`W/"abc"`, `"abc"`, true},
		{`"x", "abc"`, `"abc"`, true},
		{"*", `"abc"`, true},
		{`"other"`, `"abc"`, false},
		{"", `"abc"`, false},
	}
	for _, c := range cases {
		if got := etagMatches(c.header, c.etag); got != c.want {
			t.Fatalf("etagMatches(%q, %q) = %v, want %v", c.header, c.etag, got, c.want)
		}
	}
}

// TestGeoSiteAPIUpdateWithoutPath 未配置数据路径属于配置问题，应返回 400 而不是 500。
func TestGeoSiteAPIUpdateWithoutPath(t *testing.T) {
	access := &stubGeoSiteAccess{info: GeoSiteInfo{}, datPath: ""}
	m := newGeoSiteTestServer(access)

	rec := httptest.NewRecorder()
	m.handleGeoSiteAPI(rec, geoSiteRequest(http.MethodPost, "/api/v1/geosite/update", testManagementToken))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if access.updateCalls != 0 {
		t.Fatal("update should not run when path is not configured")
	}
}
