package proxy

import (
	"net/http"
)

// SettingsView 是运行时全局设置的只读视图（时间字段统一转换为秒）。
type SettingsView struct {
	Listen              string `json:"listen"`
	Management          string `json:"management"`
	ManagementTokenSet  bool   `json:"management_token_set"`
	WebUIEnabled        bool   `json:"webui_enabled"`
	CheckIntervalSec    int    `json:"check_interval_seconds"`
	MaxConnections      int    `json:"max_connections"`
	EnablePprof         bool   `json:"enable_pprof"`
	Socks5AllowBind     bool   `json:"socks5_allow_bind"`
	Socks5AllowUDPAssoc bool   `json:"socks5_allow_udp_associate"`

	RoutingFinal           string  `json:"routing_final"`
	AutoMode               string  `json:"auto_mode"`
	AutoDirectTimeoutSec   float64 `json:"auto_direct_timeout_seconds"`
	AutoCacheTTLSec        float64 `json:"auto_cache_ttl_seconds"`
	GeoSitePath            string  `json:"geosite_path"`
	GeoSiteAutoUpdate      bool    `json:"geosite_auto_update"`
	GeoSiteUseUpstream     bool    `json:"geosite_use_upstream"`
	GeoSiteUpdateURL       string  `json:"geosite_update_url"`
	GeoSiteUpdateIntervalH float64 `json:"geosite_update_interval_hours"`
	GeoSiteUpdateTokenSet  bool    `json:"geosite_update_token_set"`
	GeoSiteSyncEnabled     bool    `json:"geosite_sync_enabled"`
	GeoSiteSyncTokenSet    bool    `json:"geosite_sync_token_set"`
	Socks5HRedirectEnabled bool    `json:"socks5h_redirect_enabled"`

	DNSType       string  `json:"dns_type"`
	DoHURL        string  `json:"doh_url"`
	DoTAddr       string  `json:"dot_addr"`
	DNSFallback   bool    `json:"dns_fallback"`
	DNSTimeoutSec float64 `json:"dns_timeout_seconds"`
	IPPrecedence  string  `json:"ip_precedence"`

	DialTimeoutSec         float64 `json:"dial_timeout_seconds"`
	HandshakeTimeoutSec    float64 `json:"handshake_timeout_seconds"`
	HTTPExchangeTimeoutSec float64 `json:"http_exchange_timeout_seconds"`
	TunnelIdleTimeoutSec   float64 `json:"tunnel_idle_timeout_seconds"`

	ProtectPrivate bool     `json:"protect_private"`
	DefaultAction  string   `json:"default_action"`
	DenyCIDRs      []string `json:"deny_cidrs"`
	AllowCIDRs     []string `json:"allow_cidrs"`
	DenyDomains    []string `json:"deny_domains"`
	AllowDomains   []string `json:"allow_domains"`
	DenyPorts      []int    `json:"deny_ports"`
	AllowPorts     []int    `json:"allow_ports"`

	AllowedClients     []string `json:"allowed_clients"`
	RateLimitPerSecond int      `json:"rate_limit_connections_per_second"`
	RateLimitBurst     int      `json:"rate_limit_burst"`
	MaxConnsPerUser    int      `json:"max_conns_per_user"`

	TrafficAuditEnabled  bool    `json:"traffic_audit_enabled"`
	PerUserQuota         int64   `json:"per_user_quota"`
	PerUpstreamQuota     int64   `json:"per_upstream_quota"`
	QuotaWindowSec       float64 `json:"quota_window_seconds"`
	TrafficRetentionDays int     `json:"traffic_retention_days"`

	LogLevel string `json:"log_level"`
}

// SettingsPatch 是全局设置的部分更新请求。
// 指针为 nil 表示不修改；切片为 nil 表示不修改，空数组表示清空。
type SettingsPatch struct {
	CheckIntervalSec    *int    `json:"check_interval_seconds"`
	MaxConnections      *int    `json:"max_connections"`
	EnablePprof         *bool   `json:"enable_pprof"`
	WebUIEnabled        *bool   `json:"webui_enabled"`
	ManagementAuthToken *string `json:"management_auth_token"`
	Socks5AllowBind     *bool   `json:"socks5_allow_bind"`
	Socks5AllowUDPAssoc *bool   `json:"socks5_allow_udp_associate"`

	RoutingFinal           *string  `json:"routing_final"`
	AutoMode               *string  `json:"auto_mode"`
	AutoDirectTimeoutSec   *float64 `json:"auto_direct_timeout_seconds"`
	AutoCacheTTLSec        *float64 `json:"auto_cache_ttl_seconds"`
	GeoSitePath            *string  `json:"geosite_path"`
	GeoSiteAutoUpdate      *bool    `json:"geosite_auto_update"`
	GeoSiteUseUpstream     *bool    `json:"geosite_use_upstream"`
	GeoSiteUpdateURL       *string  `json:"geosite_update_url"`
	GeoSiteUpdateIntervalH *float64 `json:"geosite_update_interval_hours"`
	GeoSiteUpdateToken     *string  `json:"geosite_update_token"`
	GeoSiteSyncEnabled     *bool    `json:"geosite_sync_enabled"`
	GeoSiteSyncToken       *string  `json:"geosite_sync_token"`
	Socks5HRedirectEnabled *bool    `json:"socks5h_redirect_enabled"`

	DNSType       *string  `json:"dns_type"`
	DoHURL        *string  `json:"doh_url"`
	DoTAddr       *string  `json:"dot_addr"`
	DNSFallback   *bool    `json:"dns_fallback"`
	DNSTimeoutSec *float64 `json:"dns_timeout_seconds"`
	IPPrecedence  *string  `json:"ip_precedence"`

	DialTimeoutSec         *float64 `json:"dial_timeout_seconds"`
	HandshakeTimeoutSec    *float64 `json:"handshake_timeout_seconds"`
	HTTPExchangeTimeoutSec *float64 `json:"http_exchange_timeout_seconds"`
	TunnelIdleTimeoutSec   *float64 `json:"tunnel_idle_timeout_seconds"`

	ProtectPrivate *bool    `json:"protect_private"`
	DefaultAction  *string  `json:"default_action"`
	DenyCIDRs      []string `json:"deny_cidrs"`
	AllowCIDRs     []string `json:"allow_cidrs"`
	DenyDomains    []string `json:"deny_domains"`
	AllowDomains   []string `json:"allow_domains"`
	DenyPorts      []int    `json:"deny_ports"`
	AllowPorts     []int    `json:"allow_ports"`

	AllowedClients     []string `json:"allowed_clients"`
	RateLimitPerSecond *int     `json:"rate_limit_connections_per_second"`
	RateLimitBurst     *int     `json:"rate_limit_burst"`
	MaxConnsPerUser    *int     `json:"max_conns_per_user"`

	TrafficAuditEnabled  *bool    `json:"traffic_audit_enabled"`
	PerUserQuota         *int64   `json:"per_user_quota"`
	PerUpstreamQuota     *int64   `json:"per_upstream_quota"`
	QuotaWindowSec       *float64 `json:"quota_window_seconds"`
	TrafficRetentionDays *int     `json:"traffic_retention_days"`

	LogLevel *string `json:"log_level"`
}

// handleSettingsAPI 处理 GET/PUT /api/v1/config/settings。
func (m *ManagementServer) handleSettingsAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor) {
	MetricInc("yigate_management_requests_total", "path=/api/v1/config/settings")
	switch r.Method {
	case http.MethodGet:
		view, err := editor.GetSettings()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)

	case http.MethodPut:
		var patch SettingsPatch
		if err := decodeJSON(r, &patch); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := editor.UpdateSettings(patch); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	default:
		w.Header().Set("Allow", "GET, PUT")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleCertificatesAPI 处理 GET /api/v1/config/certificates。
// 只列出配置目录内的证书文件名，供 WebUI 的 CA 路径输入做补全；
// 目录由编辑器固定，不接受调用方指定，避免变成任意目录探测接口。
func (m *ManagementServer) handleCertificatesAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor) {
	MetricInc("yigate_management_requests_total", "path=/api/v1/config/certificates")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	files, err := editor.ListCertificateFiles()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"files": files})
}
