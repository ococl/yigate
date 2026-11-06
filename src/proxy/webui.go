package proxy

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
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
	fileServer := http.FileServer(http.FS(sub))
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
		f, err := sub.Open(strings.TrimPrefix(clean, "/"))
		if err != nil {
			// 静态资源缺失时返回真正的 404，避免浏览器把 HTML 当 JS/CSS 解析；
			// 仅无扩展名的前端路由回退到 index.html。
			if path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex(sub, w, r)
			return
		}
		_ = f.Close()
		fileServer.ServeHTTP(w, r)
	}), nil
}

// hasWebUIAssets 判断嵌入的前端产物是否包含可用的 JS 资源。
// 仅存在占位 index.html 时视为未构建前端。
func hasWebUIAssets(sub fs.FS) bool {
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
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
