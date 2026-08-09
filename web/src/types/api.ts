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

export interface StatusSnapshot {
  status: string
  listen: string
  management: string
  upstream_count: number
  router_ready: boolean
  manager_ready: boolean
  pprof_enabled: boolean
  ready?: boolean
  upstreams?: UpstreamStatus[]
  timestamp: string
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
  entries: TrafficEntrySnapshot[]
}

export interface UpstreamInfo {
  name: string
  url: string
  types: string[]
  priority: number
  weight: number
  strategy: string
  health_check_type: string
}

export interface UpstreamListResponse {
  upstreams: UpstreamInfo[]
}

export interface AddUpstreamRequest {
  name: string
  url: string
  types: string[]
  priority: number
  weight: number
  strategy: string
}

export interface UpdateUpstreamRequest {
  name?: string // 非空且与原名称不同时执行改名
  url: string
  types: string[]
  priority?: number
  weight?: number
  strategy: string
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

export interface AddRuleRequest {
  type: string
  values: string[]
  action: string
  upstreams: string[]
}

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

export interface ApiResult {
  status: string
  name?: string
  username?: string
  index?: string
  error?: string
}
