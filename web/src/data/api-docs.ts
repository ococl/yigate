export interface ApiParam {
  name: string
  type: string
  required: boolean
  description: string
}

export interface ApiExample {
  request?: string
  response: string
}

export interface ApiEndpoint {
  id: string
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  summary: string
  description: string
  auth: boolean
  authNote?: string
  params: ApiParam[]
  requestBody?: string
  responseBody: string
  example: ApiExample
  statusCodes: { code: number; description: string }[]
}

export interface ApiGroup {
  key: string
  title: string
  description: string
  endpoints: ApiEndpoint[]
}

export const apiGroups: ApiGroup[] = [
  {
    key: 'system',
    title: '系统接口',
    description: '健康检查、指标、状态和热加载等系统级管理接口。',
    endpoints: [
      {
        id: 'health',
        method: 'GET',
        path: '/health',
        summary: '健康检查',
        description:
          '返回网关健康状态。当 router_ready 或 manager_ready 为 false 时返回 HTTP 503，适用于负载均衡健康探测。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "status": "up",
  "listen": "127.0.0.1:1080",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "timestamp": "2025-01-01T00:00:00Z"
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/health',
          response: `{
  "status": "up",
  "listen": "127.0.0.1:1080",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "timestamp": "2025-01-01T00:00:00Z"
}`,
        },
        statusCodes: [
          { code: 200, description: '网关健康' },
          { code: 503, description: 'router 或 manager 未就绪' },
          { code: 401, description: '未认证（需 Bearer Token）' },
        ],
      },
      {
        id: 'metrics',
        method: 'GET',
        path: '/metrics',
        summary: 'Prometheus 指标',
        description:
          '返回 Prometheus 文本格式指标，指标前缀为 yigate_。可直接接入 Prometheus 抓取。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `# HELP yigate_connections_total Total connections
# TYPE yigate_connections_total counter
yigate_connections_total 12345
# HELP yigate_bytes_total Total bytes transferred
# TYPE yigate_bytes_total counter
yigate_bytes_total{direction="in"} 1073741824
yigate_bytes_total{direction="out"} 2147483648
...`,
        example: {
          request: 'curl http://127.0.0.1:8081/metrics',
          response: `# HELP yigate_connections_total Total connections
# TYPE yigate_connections_total counter
yigate_connections_total 12345
...`,
        },
        statusCodes: [
          { code: 200, description: '返回 text/plain 指标文本' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'status',
        method: 'GET',
        path: '/api/v1/status',
        summary: '运行状态摘要',
        description: '返回进程运行状态摘要，包含监听地址、管理端口、上游数量、pprof 状态等。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "status": "up",
  "listen": "127.0.0.1:1080",
  "management": "127.0.0.1:8081",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "pprof_enabled": false,
  "timestamp": "2025-01-01T00:00:00Z"
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/api/v1/status',
          response: `{
  "status": "up",
  "listen": "127.0.0.1:1080",
  "management": "127.0.0.1:8081",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "pprof_enabled": false,
  "timestamp": "2025-01-01T00:00:00Z"
}`,
        },
        statusCodes: [
          { code: 200, description: '返回状态摘要 JSON' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'reload',
        method: 'POST',
        path: '/api/v1/reload',
        summary: '手动热加载配置',
        description:
          '触发配置文件热加载。重新读取 YAML 配置文件并原子替换运行时组件（路由、上游、DNS 等）。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "status": "ok"
}`,
        example: {
          request: 'curl -X POST http://127.0.0.1:8081/api/v1/reload',
          response: `{
  "status": "ok"
}`,
        },
        statusCodes: [
          { code: 200, description: '热加载成功' },
          { code: 501, description: 'reload 未配置（无回调）' },
          { code: 500, description: '热加载失败，返回 error 字段' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'traffic',
        method: 'GET',
        path: '/api/v1/traffic',
        summary: '流量审计快照',
        description:
          '返回流量审计快照，按 user/upstream/target 三维统计字节数。需启用 traffic_audit 配置。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "entries": [
    {
      "user": "alice",
      "upstream": "us-east-1",
      "target": "example.com:443",
      "bytes_in": 1024,
      "bytes_out": 2048,
      "total": 3072
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/api/v1/traffic',
          response: `{
  "entries": [
    {
      "user": "alice",
      "upstream": "us-east-1",
      "target": "example.com:443",
      "bytes_in": 1024,
      "bytes_out": 2048,
      "total": 3072
    }
  ]
}`,
        },
        statusCodes: [
          { code: 200, description: '返回流量快照 JSON' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'upstreams',
    title: '上游管理',
    description: '运行时增删改查上游代理配置。所有变更持久化到 YAML 配置文件。',
    endpoints: [
      {
        id: 'upstreams-list',
        method: 'GET',
        path: '/api/v1/config/upstreams',
        summary: '列出所有上游',
        description: '返回所有上游代理配置列表。URL 已脱敏，不含凭证信息。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "upstreams": [
    {
      "name": "us-east-1",
      "url": "socks5://127.0.0.1:1080",
      "types": ["socks5"],
      "priority": 1,
      "weight": 1,
      "strategy": "round_robin",
      "health_check_type": "http"
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/api/v1/config/upstreams',
          response: `{
  "upstreams": [
    {
      "name": "us-east-1",
      "url": "socks5://127.0.0.1:1080",
      "types": ["socks5"],
      "priority": 1,
      "weight": 1,
      "strategy": "round_robin",
      "health_check_type": "http"
    }
  ]
}`,
        },
        statusCodes: [
          { code: 200, description: '返回上游列表' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-add',
        method: 'POST',
        path: '/api/v1/config/upstreams',
        summary: '添加上游',
        description:
          '添加一个新的上游代理。请求体最大 1 MiB，严格模式（DisallowUnknownFields）。name 不能与已有上游重复。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "name": "my-upstream",
  "url": "socks5://127.0.0.1:1080",
  "types": ["socks5"],
  "priority": 1,
  "weight": 1,
  "strategy": "round_robin"
}`,
        responseBody: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8081/api/v1/config/upstreams \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "my-upstream",
    "url": "socks5://127.0.0.1:1080",
    "types": ["socks5"],
    "priority": 1,
    "weight": 1,
    "strategy": "round_robin"
  }'`,
          response: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        },
        statusCodes: [
          { code: 201, description: '添加成功' },
          { code: 400, description: '请求体格式错误或字段校验失败' },
          { code: 409, description: '上游名称已存在' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-update',
        method: 'PUT',
        path: '/api/v1/config/upstreams/{name}',
        summary: '更新上游',
        description:
          '更新指定上游代理配置。priority 和 weight 为可选字段（指针类型），未提供时保持原值。请求体中 name 非空且与路径中旧名称不同时执行改名，并同步更新路由规则中对旧名称的引用。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [
          {
            name: 'name',
            type: 'string (path)',
            required: true,
            description: '上游原名称，URL 路径参数',
          },
        ],
        requestBody: `{
  "name": "my-upstream-new",
  "url": "socks5://127.0.0.1:1081",
  "types": ["socks5"],
  "priority": 2,
  "weight": 3,
  "strategy": "round_robin"
}`,
        responseBody: `{
  "status": "ok",
  "name": "my-upstream-new"
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8081/api/v1/config/upstreams/my-upstream \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "my-upstream-new",
    "url": "socks5://127.0.0.1:1081",
    "types": ["socks5"],
    "priority": 2,
    "weight": 3,
    "strategy": "round_robin"
  }'`,
          response: `{
  "status": "ok",
  "name": "my-upstream-new"
}`,
        },
        statusCodes: [
          { code: 200, description: '更新成功' },
          { code: 400, description: '请求体格式错误或 name 已被占用' },
          { code: 404, description: '上游不存在' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-check',
        method: 'POST',
        path: '/api/v1/config/upstreams/{name}/check',
        summary: '手动检查单个上游',
        description: '立即对指定上游执行一次健康检查，返回 alive/dead 结果，并同步更新运行时上游状态。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [
          {
            name: 'name',
            type: 'string (path)',
            required: true,
            description: '上游名称，URL 路径参数',
          },
        ],
        responseBody: `{
  "result": {
    "name": "my-upstream",
    "alive": true,
    "checked_at": "2025-01-01T00:00:00Z"
  }
}`,
        example: {
          request: 'curl -X POST http://127.0.0.1:8081/api/v1/config/upstreams/my-upstream/check',
          response: `{
  "result": {
    "name": "my-upstream",
    "alive": true,
    "checked_at": "2025-01-01T00:00:00Z"
  }
}`,
        },
        statusCodes: [
          { code: 200, description: '检查完成（无论 alive 与否）' },
          { code: 400, description: '上游不存在或检查失败' },
          { code: 503, description: '上游管理器未就绪' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-check-all',
        method: 'POST',
        path: '/api/v1/config/upstreams/check',
        summary: '一键巡检所有上游',
        description: '并发对所有上游执行一次健康检查，返回每个上游的 alive/dead 结果，并同步更新运行时状态。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "results": [
    {
      "name": "my-upstream",
      "alive": true,
      "checked_at": "2025-01-01T00:00:00Z"
    }
  ]
}`,
        example: {
          request: 'curl -X POST http://127.0.0.1:8081/api/v1/config/upstreams/check',
          response: `{
  "results": [
    {
      "name": "my-upstream",
      "alive": true,
      "checked_at": "2025-01-01T00:00:00Z"
    }
  ]
}`,
        },
        statusCodes: [
          { code: 200, description: '巡检完成' },
          { code: 503, description: '上游管理器未就绪' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-delete',
        method: 'DELETE',
        path: '/api/v1/config/upstreams/{name}',
        summary: '删除上游',
        description: '删除指定上游代理配置。name 通过 URL 路径指定。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [
          {
            name: 'name',
            type: 'string (path)',
            required: true,
            description: '上游名称，URL 路径参数',
          },
        ],
        responseBody: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        example: {
          request: 'curl -X DELETE http://127.0.0.1:8081/api/v1/config/upstreams/my-upstream',
          response: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        },
        statusCodes: [
          { code: 200, description: '删除成功' },
          { code: 404, description: '上游不存在' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'rules',
    title: '路由规则管理',
    description: '运行时增删查路由规则。规则按顺序匹配，首个命中规则生效。',
    endpoints: [
      {
        id: 'rules-list',
        method: 'GET',
        path: '/api/v1/config/rules',
        summary: '列出所有路由规则',
        description: '返回所有路由规则列表，按配置顺序排列。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "rules": [
    {
      "index": 0,
      "type": "domain",
      "values": ["example.com"],
      "action": "proxy",
      "upstreams": ["us-east-1"]
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/api/v1/config/rules',
          response: `{
  "rules": [
    {
      "index": 0,
      "type": "domain",
      "values": ["example.com"],
      "action": "proxy",
      "upstreams": ["us-east-1"]
    }
  ]
}`,
        },
        statusCodes: [
          { code: 200, description: '返回规则列表' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-add',
        method: 'POST',
        path: '/api/v1/config/rules',
        summary: '添加路由规则',
        description:
          '在规则列表末尾追加一条路由规则。支持的规则类型：domain、domain_suffix、domain_keyword、domain_regex、ip_cidr、geosite、match。支持的动作：direct、proxy、auto。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "type": "domain",
  "values": ["example.com", "*.example.com"],
  "action": "proxy",
  "upstreams": ["us-east-1"]
}`,
        responseBody: `{
  "status": "ok"
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8081/api/v1/config/rules \\
  -H "Content-Type: application/json" \\
  -d '{
    "type": "domain",
    "values": ["example.com", "*.example.com"],
    "action": "proxy",
    "upstreams": ["us-east-1"]
  }'`,
          response: `{
  "status": "ok"
}`,
        },
        statusCodes: [
          { code: 201, description: '添加成功' },
          { code: 400, description: '请求体格式错误或字段校验失败' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-delete',
        method: 'DELETE',
        path: '/api/v1/config/rules/{index}',
        summary: '删除路由规则',
        description: '按索引删除路由规则。index 为从 0 开始的整数，通过 URL 路径指定。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [
          {
            name: 'index',
            type: 'integer (path)',
            required: true,
            description: '规则索引，从 0 开始',
          },
        ],
        responseBody: `{
  "status": "ok",
  "index": "0"
}`,
        example: {
          request: 'curl -X DELETE http://127.0.0.1:8081/api/v1/config/rules/0',
          response: `{
  "status": "ok",
  "index": "0"
}`,
        },
        statusCodes: [
          { code: 200, description: '删除成功' },
          { code: 404, description: '索引超出范围' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'users',
    title: '用户管理',
    description: '运行时增删查代理用户。用户列表不返回密码。',
    endpoints: [
      {
        id: 'users-list',
        method: 'GET',
        path: '/api/v1/config/users',
        summary: '列出所有用户',
        description: '返回所有代理用户列表，仅包含用户名，不含密码。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "users": [
    {
      "username": "alice"
    },
    {
      "username": "bob"
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8081/api/v1/config/users',
          response: `{
  "users": [
    { "username": "alice" },
    { "username": "bob" }
  ]
}`,
        },
        statusCodes: [
          { code: 200, description: '返回用户列表' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'users-add',
        method: 'POST',
        path: '/api/v1/config/users',
        summary: '添加用户',
        description: '添加一个新的代理用户。username 不能与已有用户重复。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "username": "alice",
  "password": "secret123"
}`,
        responseBody: `{
  "status": "ok",
  "username": "alice"
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8081/api/v1/config/users \\
  -H "Content-Type: application/json" \\
  -d '{
    "username": "alice",
    "password": "secret123"
  }'`,
          response: `{
  "status": "ok",
  "username": "alice"
}`,
        },
        statusCodes: [
          { code: 201, description: '添加成功' },
          { code: 400, description: '请求体格式错误' },
          { code: 409, description: '用户名已存在' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'users-delete',
        method: 'DELETE',
        path: '/api/v1/config/users/{username}',
        summary: '删除用户',
        description: '删除指定代理用户。username 通过 URL 路径指定。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [
          {
            name: 'username',
            type: 'string (path)',
            required: true,
            description: '用户名，URL 路径参数',
          },
        ],
        responseBody: `{
  "status": "ok",
  "username": "alice"
}`,
        example: {
          request: 'curl -X DELETE http://127.0.0.1:8081/api/v1/config/users/alice',
          response: `{
  "status": "ok",
  "username": "alice"
}`,
        },
        statusCodes: [
          { code: 200, description: '删除成功' },
          { code: 404, description: '用户不存在' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'pprof',
    title: '性能分析 (pprof)',
    description:
      'Go pprof 性能分析端点。仅在配置 enable_pprof: true 时启用。属于敏感入口，不应默认暴露到公网。',
    endpoints: [
      {
        id: 'pprof-index',
        method: 'GET',
        path: '/debug/pprof/',
        summary: 'pprof 首页',
        description: 'pprof 索引页面，列出所有可用的 profile 类型。',
        auth: true,
        authNote: '始终要求认证（通过 authenticatedHandler 包装）',
        params: [],
        responseBody: `text/html — pprof 索引页面`,
        example: {
          request: 'curl http://127.0.0.1:8081/debug/pprof/',
          response: `Types of profiles available:
Count Profile
...`,
        },
        statusCodes: [
          { code: 200, description: '返回 pprof 索引 HTML' },
          { code: 401, description: '未认证' },
          { code: 404, description: 'pprof 未启用' },
        ],
      },
      {
        id: 'pprof-profile',
        method: 'GET',
        path: '/debug/pprof/profile',
        summary: 'CPU Profile',
        description:
          '采集 CPU profile。可使用 go tool pprof 分析。支持 seconds 参数控制采集时长。',
        auth: true,
        authNote: '始终要求认证',
        params: [
          {
            name: 'seconds',
            type: 'number (query)',
            required: false,
            description: 'CPU 采样时长（秒），默认 30',
          },
        ],
        responseBody: `binary/pprof — CPU profile 数据`,
        example: {
          request: 'curl -o cpu.prof "http://127.0.0.1:8081/debug/pprof/profile?seconds=30"',
          response: `Binary pprof data (use "go tool pprof cpu.prof" to analyze)`,
        },
        statusCodes: [
          { code: 200, description: '返回 CPU profile 二进制数据' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'pprof-heap',
        method: 'GET',
        path: '/debug/pprof/heap',
        summary: '堆内存分析',
        description: '采集堆内存分配 profile。可使用 go tool pprof 分析。',
        auth: true,
        authNote: '始终要求认证',
        params: [],
        responseBody: `binary/pprof — heap profile 数据`,
        example: {
          request: 'curl -o heap.prof http://127.0.0.1:8081/debug/pprof/heap',
          response: `Binary pprof data (use "go tool pprof heap.prof" to analyze)`,
        },
        statusCodes: [
          { code: 200, description: '返回 heap profile 二进制数据' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'pprof-goroutine',
        method: 'GET',
        path: '/debug/pprof/goroutine',
        summary: 'Goroutine 堆栈',
        description: '采集所有 goroutine 的堆栈信息。',
        auth: true,
        authNote: '始终要求认证',
        params: [],
        responseBody: `binary/pprof — goroutine profile 数据`,
        example: {
          request: 'curl -o goroutine.prof http://127.0.0.1:8081/debug/pprof/goroutine',
          response: `Binary pprof data`,
        },
        statusCodes: [
          { code: 200, description: '返回 goroutine profile 数据' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'pprof-other',
        method: 'GET',
        path: '/debug/pprof/{type}',
        summary: '其他 pprof 端点',
        description:
          '其他可用的 pprof 端点：cmdline（命令行）、symbol（符号表）、trace（执行追踪）、allocs（内存分配采样）、block（阻塞分析）、mutex（互斥锁分析）、threadcreate（线程创建分析）。',
        auth: true,
        authNote: '始终要求认证',
        params: [
          {
            name: 'type',
            type: 'string (path)',
            required: true,
            description: 'profile 类型：cmdline, symbol, trace, allocs, block, mutex, threadcreate',
          },
        ],
        responseBody: `取决于具体 profile 类型`,
        example: {
          request: 'curl http://127.0.0.1:8081/debug/pprof/trace?seconds=5',
          response: `Binary trace data`,
        },
        statusCodes: [
          { code: 200, description: '返回对应 profile 数据' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
]

export const authInfo = {
  method: 'Bearer Token',
  header: 'Authorization: Bearer <token>',
  configField: 'management_auth_token',
  description:
    '当 management_auth_token 配置为空时，所有管理接口允许匿名访问。配置非空时，所有请求必须携带 Authorization: Bearer <token> 头。管理端口绑定非回环地址时必须配置 management_auth_token。',
  bruteForceProtection: '单 IP 连续认证失败 10 次后封禁 5 分钟',
  unauthorizedResponse: {
    status: 401,
    headers: { 'WWW-Authenticate': 'Bearer realm="yigate-management"' },
    body: '{"status":"unauthorized"}',
  },
}
