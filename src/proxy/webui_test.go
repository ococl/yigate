package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func gzipAsset(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestServeWebUIFileServesPrecompressedAsset(t *testing.T) {
	const content = "console.log('compressed')"
	fsys := fstest.MapFS{
		"assets/app.js.gz": {Data: gzipAsset(t, content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/assets/app.js", rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := zr.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != content {
		t.Fatalf("body = %q, want %q", body, content)
	}
}

func TestServeWebUIFileDecompressesForLegacyClient(t *testing.T) {
	const content = "console.log('plain')"
	fsys := fstest.MapFS{
		"assets/app.js.gz": {Data: gzipAsset(t, content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/assets/app.js", rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if rec.Body.String() != content {
		t.Fatalf("body = %q, want %q", rec.Body.String(), content)
	}
}

func TestServeWebUIFileFallsBackToRawAsset(t *testing.T) {
	const content = "body { color: red; }"
	fsys := fstest.MapFS{
		"assets/app.css": {Data: []byte(content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/assets/app.css", rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != content {
		t.Fatalf("body = %q, want %q", rec.Body.String(), content)
	}
}

func TestServeWebUIFileCachesHashedAssetImmutably(t *testing.T) {
	const content = "console.log('hashed')"
	fsys := fstest.MapFS{
		"assets/index-IQLevEbW.js.gz": {Data: gzipAsset(t, content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/index-IQLevEbW.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/assets/index-IQLevEbW.js", rec, req)

	want := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}

func TestServeWebUIFileRevalidatesUnhashedAsset(t *testing.T) {
	fsys := fstest.MapFS{
		"favicon.svg": {Data: []byte("<svg/>")},
	}
	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/favicon.svg", rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestServeIndexDisablesCaching(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html></html>")},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	serveIndex(fsys, rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestHasWebUIAssetsAcceptsCompressedAssets(t *testing.T) {
	fsys := fstest.MapFS{
		"assets/app.js.gz": {Data: gzipAsset(t, "console.log('ok')")},
	}
	if !hasWebUIAssets(fsys) {
		t.Fatal("compressed JS asset was not detected")
	}
}

func TestAcceptsGzipHonorsQualityZero(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, gzip;q=0")
	if acceptsGzip(req) {
		t.Fatal("gzip;q=0 must not be accepted")
	}
	req.Header.Set("Accept-Encoding", "br, gzip;q=0.5")
	if !acceptsGzip(req) {
		t.Fatal("gzip;q=0.5 should be accepted")
	}
}
