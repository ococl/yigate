package proxy

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// geoSiteAPIPrefix 是 GeoSite 管理接口的路径前缀。
const geoSiteAPIPrefix = "/api/v1/geosite"

// GeoSiteAccess 由宿主注入，用于读取 GeoSite 状态、触发更新并暴露数据文件。
// 所有方法必须线程安全。
type GeoSiteAccess interface {
	// Info 返回当前数据文件的版本信息与更新配置。
	Info() GeoSiteInfo
	// Update 立即从 update_url 拉取一次数据，返回最新状态与内容是否变化。
	Update(ctx context.Context) (GeoSiteInfo, bool, error)
	// DatPath 返回数据文件路径（可能不存在）。
	DatPath() string
	// SyncConfig 返回本节点对外提供的只读同步配置。
	SyncConfig() GeoSiteSyncConfig
}

// GeoSiteUpdateResponse 是手动更新的响应体。
type GeoSiteUpdateResponse struct {
	Status  string      `json:"status"`
	Changed bool        `json:"changed"`
	Info    GeoSiteInfo `json:"info"`
}

// SetGeoSiteAccess 注入 GeoSite 访问实现（宿主在启动时调用一次即可）。
func (m *ManagementServer) SetGeoSiteAccess(access GeoSiteAccess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.geoSiteAccess = access
}

func (m *ManagementServer) geoSiteAccessSafe() GeoSiteAccess {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.geoSiteAccess
}

// handleGeoSiteAPI 处理 /api/v1/geosite/* 请求：
//
//	GET  /api/v1/geosite       查看版本与同步配置
//	POST /api/v1/geosite/update 立即从 update_url 更新数据
//	GET  /api/v1/geosite/dat    下载数据文件（供下游节点同步，支持条件请求）
func (m *ManagementServer) handleGeoSiteAPI(w http.ResponseWriter, r *http.Request) {
	MetricInc("yigate_management_requests_total", "path="+geoSiteAPIPrefix)
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, geoSiteAPIPrefix), "/")
	switch rest {
	case "":
		m.handleGeoSiteInfo(w, r)
	case "update":
		m.handleGeoSiteUpdate(w, r)
	case "dat":
		m.handleGeoSiteDat(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "unknown geosite API path")
	}
}

func (m *ManagementServer) handleGeoSiteInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access := m.geoSiteAccessSafe()
	if access == nil {
		writeJSONError(w, http.StatusNotImplemented, "geosite API not configured")
		return
	}
	if !m.requireGeoSiteReadAuth(w, r, access) {
		return
	}
	writeJSON(w, http.StatusOK, access.Info())
}

func (m *ManagementServer) handleGeoSiteUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access := m.geoSiteAccessSafe()
	if access == nil {
		writeJSONError(w, http.StatusNotImplemented, "geosite API not configured")
		return
	}
	// 写操作始终要求管理令牌，只读同步令牌不能触发更新。
	if !m.requireAuth(w, r) {
		return
	}
	info, changed, err := access.Update(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, GeoSiteUpdateResponse{Status: "ok", Changed: changed, Info: info})
}

func (m *ManagementServer) handleGeoSiteDat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access := m.geoSiteAccessSafe()
	if access == nil {
		writeJSONError(w, http.StatusNotImplemented, "geosite API not configured")
		return
	}
	if !m.requireGeoSiteReadAuth(w, r, access) {
		return
	}
	path := access.DatPath()
	if path == "" {
		writeJSONError(w, http.StatusNotFound, "geosite path is not configured")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "geosite data file not found")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		writeJSONError(w, http.StatusNotFound, "geosite data file not found")
		return
	}

	info := access.Info()
	etag := ""
	if info.SHA256 != "" {
		etag = `"` + info.SHA256 + `"`
		w.Header().Set("ETag", etag)
	}
	if info.Version != "" {
		w.Header().Set("X-GeoSite-Version", info.Version)
	}
	if info.SHA256 != "" {
		w.Header().Set("X-GeoSite-SHA256", info.SHA256)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Disposition", `attachment; filename="geosite.dat"`)

	// 条件请求：下游按 ETag 判断是否已是最新，避免重复下载整包数据。
	if etag != "" && etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, "geosite.dat", stat.ModTime(), file)
}

// requireGeoSiteReadAuth 允许管理令牌或只读同步令牌访问读接口。
func (m *ManagementServer) requireGeoSiteReadAuth(w http.ResponseWriter, r *http.Request, access GeoSiteAccess) bool {
	if sync := access.SyncConfig(); sync.Enabled && sync.Token != "" {
		if headerTokenMatches(r, sync.Token) {
			return true
		}
	}
	return m.requireAuth(w, r)
}

// headerTokenMatches 以常量时间比较请求中的 Bearer 令牌。
func headerTokenMatches(r *http.Request, token string) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if provided == "" || len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

// etagMatches 判断 If-None-Match 是否命中当前 ETag（支持弱校验与列表形式）。
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
