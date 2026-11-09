package proxy

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed all:webui_dist
var webUIDistFS embed.FS

//go:embed webui_fallback.html
var webUIFallbackHTML []byte

var (
	webUIEnabledMu sync.RWMutex
	webUIEnabled   = true
)

// SetWebUIEnabled 设置是否启用内置 WebUI。默认启用。
func SetWebUIEnabled(enabled bool) {
	webUIEnabledMu.Lock()
	defer webUIEnabledMu.Unlock()
	webUIEnabled = enabled
}

// WebUIEnabled 返回当前是否启用内置 WebUI。
func WebUIEnabled() bool {
	webUIEnabledMu.RLock()
	defer webUIEnabledMu.RUnlock()
	return webUIEnabled
}

// webUIEmbedPrefix 是 go:embed 嵌入的前端资源在 embed.FS 中的根目录名。
const webUIEmbedPrefix = "webui_dist"

// prefixedFS 将 embed.FS 中 webUIEmbedPrefix 子目录包装为根文件系统，
// 避免 fs.Sub 在某些 embed.FS 实现上的兼容性问题。
type prefixedFS struct {
	base fs.FS
}

func (p prefixedFS) Open(name string) (fs.File, error) {
	if name == "." {
		name = webUIEmbedPrefix
	} else {
		name = webUIEmbedPrefix + "/" + strings.TrimPrefix(name, "/")
	}
	return p.base.Open(name)
}

func (p prefixedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		name = webUIEmbedPrefix
	} else {
		name = webUIEmbedPrefix + "/" + strings.TrimPrefix(name, "/")
	}
	return fs.ReadDir(p.base, name)
}

// WebUIFileSystem 返回嵌入的前端静态文件系统（根目录即为 webui_dist 内容）。
func WebUIFileSystem() (fs.FS, error) {
	return prefixedFS{base: webUIDistFS}, nil
}

// WebUIHandler 返回一个 SPA 静态文件处理器。
// 非 /api 路径且无法匹配静态资源时，回退到 index.html 以支持前端路由。
func WebUIHandler() (http.Handler, error) {
	sub, err := WebUIFileSystem()
	if err != nil {
		return nil, err
	}
	hasAssets := hasWebUIAssets(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		// 不拦截 API 与管理端点、pprof 等敏感路径（已由 mux 优先注册）
		if strings.HasPrefix(clean, "/api/") ||
			strings.HasPrefix(clean, "/debug/") ||
			clean == "/health" || clean == "/metrics" {
			http.NotFound(w, r)
			return
		}
		// 未集成前端产物时显示构建提示页，避免直接返回空白页。
		if !hasAssets {
			if path.Ext(clean) != "" && clean != "/index.html" {
				http.NotFound(w, r)
				return
			}
			serveFallback(w)
			return
		}
		if clean == "/" || clean == "/index.html" {
			serveIndex(sub, w, r)
			return
		}
		if _, err := sub.Open(strings.TrimPrefix(clean, "/")); err != nil {
			if _, gzErr := sub.Open(strings.TrimPrefix(clean, "/") + ".gz"); gzErr != nil {
				// 静态资源缺失时返回真正的 404，避免浏览器把 HTML 当 JS/CSS 解析；
				// 仅无扩展名的前端路由回退到 index.html。
				if path.Ext(clean) != "" {
					http.NotFound(w, r)
					return
				}
				serveIndex(sub, w, r)
				return
			}
		}
		serveWebUIFile(sub, clean, w, r)
	}), nil
}

// serveWebUIFile 提供静态资源；构建时压缩的资源以 .gz 形式嵌入。
// 支持 gzip 的客户端直接接收压缩内容，其他客户端由服务端解压，保证兼容性。
func serveWebUIFile(sub fs.FS, name string, w http.ResponseWriter, r *http.Request) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" || strings.HasSuffix(name, ".gz") {
		http.NotFound(w, r)
		return
	}

	if compressed, err := fs.ReadFile(sub, name+".gz"); err == nil {
		w.Header().Set("Vary", "Accept-Encoding")
		setWebUIContentType(w, name)
		setWebUICacheControl(w, name)
		if acceptsGzip(r) {
			w.Header().Set("Content-Encoding", "gzip")
			http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(compressed))
			return
		}
		raw, err := decompressWebUIAsset(name, compressed)
		if err != nil {
			http.Error(w, "webui asset unavailable", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(raw))
		return
	}

	data, err := fs.ReadFile(sub, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	setWebUIContentType(w, name)
	setWebUICacheControl(w, name)
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(data))
}

// immutableAssetPattern 匹配 Vite 构建产物中带内容哈希的文件名，
// 例如 assets/index-IQLevEbW.js。这类资源内容随哈希变化，可安全长期缓存。
var immutableAssetPattern = regexp.MustCompile(`^assets/.+-[A-Za-z0-9_-]{8,}\.(js|css|woff2?|ttf|otf|svg|png|jpe?g|gif|webp|avif|ico|map)$`)

// 非 gzip 客户端需要服务端解压；缓存少量哈希资源，避免每次请求重复解压。
const maxDecompressCacheEntries = 64

var (
	webUIDecompressCache sync.Map // name -> []byte
	webUIDecompressCount atomic.Int64
)

// decompressWebUIAsset 解压构建期 gzip 资源，并缓存带哈希的产物。
func decompressWebUIAsset(name string, compressed []byte) ([]byte, error) {
	cacheable := immutableAssetPattern.MatchString(name)
	if cacheable {
		if cached, ok := webUIDecompressCache.Load(name); ok {
			return cached.([]byte), nil
		}
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(zr)
	closeErr := zr.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if cacheable && webUIDecompressCount.Load() < maxDecompressCacheEntries {
		if _, loaded := webUIDecompressCache.LoadOrStore(name, raw); !loaded {
			webUIDecompressCount.Add(1)
		}
	}
	return raw, nil
}

// setWebUICacheControl 为静态资源设置缓存策略：
// 带哈希的产物长期 immutable，其余文件（favicon 等）使用 no-cache 以便升级后及时更新。
func setWebUICacheControl(w http.ResponseWriter, name string) {
	if immutableAssetPattern.MatchString(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

func setWebUIContentType(w http.ResponseWriter, name string) {
	ext := path.Ext(name)
	// Go 内置 mime 表不含 .txt，精简容器里也常没有 /etc/mime.types；
	// 显式带上 charset，避免中文在浏览器和 AI 客户端里被按 latin-1 解码。
	if ext == ".txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		return
	}
	if contentType := mime.TypeByExtension(ext); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		parts := strings.Split(strings.TrimSpace(encoding), ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			parameter = strings.TrimSpace(parameter)
			if !strings.HasPrefix(parameter, "q=") {
				continue
			}
			if value, err := strconv.ParseFloat(strings.TrimPrefix(parameter, "q="), 64); err == nil {
				quality = value
			}
		}
		if quality > 0 {
			return true
		}
	}
	return false
}

// hasWebUIAssets 判断嵌入的前端产物是否包含可用的 JS 资源。
// 仅存在占位 index.html 时视为未构建前端。
func hasWebUIAssets(sub fs.FS) bool {
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".js.gz") {
			return true
		}
	}
	return false
}

// serveIndex 写入 index.html 内容。入口页不缓存，避免升级后引用旧的资源哈希。
func serveIndex(sub fs.FS, w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "webui not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// serveFallback 返回前端未构建提示页。
func serveFallback(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(webUIFallbackHTML)
}
