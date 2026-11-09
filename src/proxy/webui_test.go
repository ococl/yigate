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

func TestServeWebUIFileServesPlainTextAsUTF8(t *testing.T) {
	// AI 客户端按 Content-Type 解码，缺 charset 时中文说明会变乱码
	const content = "# YiGate\n\n认证方式：Bearer Token\n"
	fsys := fstest.MapFS{
		"llmx.txt": {Data: []byte(content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/llmx.txt", nil)
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/llmx.txt", rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if rec.Body.String() != content {
		t.Fatalf("body = %q, want %q", rec.Body.String(), content)
	}
}

func TestServeWebUIFileServesCompressedPlainTextAsUTF8(t *testing.T) {
	// 构建时体积够大的文本会被预压缩，gzip 客户端走的是另一条分支
	const content = "# YiGate\n\n认证方式：Bearer Token\n"
	fsys := fstest.MapFS{
		"llmx.txt.gz": {Data: gzipAsset(t, content)},
	}
	req := httptest.NewRequest(http.MethodGet, "/llmx.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, "/llmx.txt", rec, req)

	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != content {
		t.Fatalf("body = %q, want %q", string(body), content)
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

func TestServeWebUIFileCachesDecompressedHashedAsset(t *testing.T) {
	const content = "console.log('cached')"
	name := "/assets/cache-" + t.Name() + "-AbCdEf12.js"
	fsys := fstest.MapFS{name[1:] + ".gz": {Data: gzipAsset(t, content)}}
	req := httptest.NewRequest(http.MethodGet, name, nil)
	rec := httptest.NewRecorder()

	serveWebUIFile(fsys, name, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, ok := webUIDecompressCache.Load(name[1:]); !ok {
		t.Fatalf("expected decompressed asset to be cached")
	}

	// 第二次请求命中缓存，内容保持一致
	rec2 := httptest.NewRecorder()
	serveWebUIFile(fsys, name, rec2, httptest.NewRequest(http.MethodGet, name, nil))
	if rec2.Body.String() != content {
		t.Fatalf("body = %q, want %q", rec2.Body.String(), content)
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
