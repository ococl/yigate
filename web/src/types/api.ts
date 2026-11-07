// API 响应类型定义

export interface HealthStatus {
  status: string
  listen: string
  upstream_count: number
  router_ready: boolean
  manager_ready: boolean
  timestamp: string
}

export interface UpstreamStatus {
  name: string
  url: string
  types: string[]
  dead: boolean
  dead_since?: string
  failures: number
  successes: number
  consecutive_dead: number
  next_probe_at?: string
  next_check_at?: string
  last_check_at?: string
  bind_unsupported: boolean
  udp_unsupported: boolean
  active_conns: number
}

export interface ProxyEndpoint {
  address: string
  port: number
  family?: string
  interface_name?: string
  loopback?: boolean
  tls?: boolean
}

export interface StatusSnapshot {
  status: string
  listen: string
  management: string
  upstream_count: number
  router_ready: boolean
  manager_ready: boolean
  pprof_enabled: boolean
  webui_enabled?: boolean
  ready?: boolean
  upstreams?: UpstreamStatus[]
  proxy_endpoints?: ProxyEndpoint[]
  version: string
  commit: string
  build_time: string
  go_version: string
  uptime_seconds: number
  timestamp: string
}

export interface VersionInfo {
  version: string
  commit: string
  build_time: string
  go_version: string
  platform: string
  uptime_seconds: number
}

export interface TrafficEntrySnapshot {
  user: string
  upstream: string
  target: string
  bytes_in: number
  bytes_out: number
  total: number
}

export interface TrafficSnapshot {
  enabled: boolean
  retention_days: number
  entries: TrafficEntrySnapshot[]
}

export interface HealthCheckConfig {
  type: string
  path: string
  expected_codes: number[]
  interval_seconds: number
  timeout_seconds: number
  failure_threshold: number
  success_threshold: number
}

export interface UpstreamInfo {
  name: string
  url: string
  enabled: boolean
  types: string[]
  priority: number
  weight: number
  strategy: string
  health_check: HealthCheckConfig
}

export interface UpstreamListResponse {
  upstreams: UpstreamInfo[]
}

export interface AddUpstreamRequest {
  name: string
  url: string
  enabled?: boolean
  types: string[]
  priority: number
  weight: number
  strategy: string
  health_check?: HealthCheckConfig
}

export interface UpdateUpstreamRequest {
  name?: string // 非空且与原名称不同时执行改名
  url?: string // 留空表示沿用现有 URL（避免把脱敏后的 URL 写回配置）
  enabled?: boolean
  types?: string[]
  priority?: number
  weight?: number
  strategy?: string
  health_check?: HealthCheckConfig
}

export interface UpstreamCheckResult {
  name: string
  alive: boolean
  error?: string
  checked_at: string
}

export interface UpstreamCheckResponse {
  result: UpstreamCheckResult
}

export interface UpstreamCheckAllResponse {
  results: UpstreamCheckResult[]
}

export interface RuleInfo {
  index: number
  type: string
  values: string[]
  action: string
  upstreams: string[]
}

export interface RuleListResponse {
  rules: RuleInfo[]
}

export interface RuleRequest {
  type: string
  values: string[]
  action: string
  upstreams: string[]
}

export type AddRuleRequest = RuleRequest

export interface UserInfo {
  username: string
}

export interface UserListResponse {
  users: UserInfo[]
}

export interface AddUserRequest {
  username: string
  password: string
}

export interface SettingsView {
  listen: string
  management: string
  management_token_set: boolean
  webui_enabled: boolean
  check_interval_seconds: number
  max_connections: number
  enable_pprof: boolean
  socks5_allow_bind: boolean
  socks5_allow_udp_associate: boolean

  routing_final: string
  auto_mode: string
  auto_direct_timeout_seconds: number
  auto_cache_ttl_seconds: number
  geosite_path: string
  geosite_auto_update: boolean
  geosite_update_url: string
  geosite_update_interval_hours: number
  geosite_update_token_set: boolean
  geosite_sync_enabled: boolean
  geosite_sync_token_set: boolean
  socks5h_redirect_enabled: boolean

  dns_type: string
  doh_url: string
  dot_addr: string
  dns_fallback: boolean
  dns_timeout_seconds: number
  ip_precedence: string

  dial_timeout_seconds: number
  handshake_timeout_seconds: number
  http_exchange_timeout_seconds: number
  tunnel_idle_timeout_seconds: number

  protect_private: boolean
  default_action: string
  deny_cidrs: string[]
  allow_cidrs: string[]
  deny_domains: string[]
  allow_domains: string[]
  deny_ports: number[]
  allow_ports: number[]

  allowed_clients: string[]
  rate_limit_connections_per_second: number
  rate_limit_burst: number
  max_conns_per_user: number

  traffic_audit_enabled: boolean
  per_user_quota: number
  per_upstream_quota: number
  quota_window_seconds: number
  traffic_retention_days: number

  log_level: string
}

// 可写字段：listen/management 为启动参数不可在线修改，management_token_set 为只读状态；
// management_auth_token 仅用于设置新令牌（留空表示不修改），因此单独声明。
export interface SettingsPatch extends Partial<Omit<SettingsView, 'listen' | 'management' | 'management_token_set'>> {
  management_auth_token?: string
  geosite_update_token?: string
  geosite_sync_token?: string
}

export interface ApiResult {
  status: string
  name?: string
  username?: string
  index?: string
  error?: string
}

// GeoSiteInfo 描述 GeoSite 数据文件的版本与更新/同步配置。
export interface GeoSiteInfo {
  path: string
  download_path: string
  exists: boolean
  size: number
  version: string
  sha256: string
  updated_at?: string
  source_url?: string
  last_checked_at?: string
  last_error?: string
  auto_update: boolean
  update_url: string
  update_interval_seconds: number
  update_token_set: boolean
  sync_enabled: boolean
  sync_token_set: boolean
}

export interface GeoSiteUpdateResult {
  status: string
  changed: boolean
  info: GeoSiteInfo
}
