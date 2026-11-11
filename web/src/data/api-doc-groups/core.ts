import type { ApiGroup } from './types'

export const coreGroups: ApiGroup[] = [
  {
    key: 'system',
    title: '系统接口',
    description: '健康检查、指标、状态和热加载等系统级管理接口。',
    endpoints: [
      {
        id: 'health',
        method: 'GET',
        path: '/health',
        summary: '健康检查（匿名）',
        description:
          '进程探活端点，无需认证，仅返回纯文本 UP，用于容器 healthcheck 与负载均衡探测。它只表示管理端口已在响应，不代表代理功能可用；就绪与上游状态请查 /api/v1/status。',
        auth: false,
        params: [],
        responseBody: 'UP',
        example: {
          request: 'curl http://127.0.0.1:8890/health',
          response: 'UP',
        },
        statusCodes: [{ code: 200, description: '进程存活，返回纯文本 UP' }],
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
          request: 'curl http://127.0.0.1:8890/metrics',
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
        description:
          '返回进程运行状态摘要，包含监听地址、管理端口、上游数量、pprof 与内置 WebUI 开关，以及自动探测的可用代理地址等。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "status": "up",
  "listen": "0.0.0.0:1080",
  "management": "127.0.0.1:8890",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "pprof_enabled": false,
  "webui_enabled": true,
  "proxy_endpoints": [
    {
      "address": "192.168.1.10",
      "port": 1080,
      "family": "ipv4",
      "interface_name": "eth0"
    },
    {
      "address": "127.0.0.1",
      "port": 1080,
      "family": "ipv4",
      "interface_name": "lo",
      "loopback": true
    }
  ],
  "timestamp": "2025-01-01T00:00:00Z"
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/status',
          response: `{
  "status": "up",
  "listen": "0.0.0.0:1080",
  "management": "127.0.0.1:8890",
  "upstream_count": 3,
  "router_ready": true,
  "manager_ready": true,
  "pprof_enabled": false,
  "webui_enabled": true,
  "proxy_endpoints": [
    {
      "address": "192.168.1.10",
      "port": 1080,
      "family": "ipv4",
      "interface_name": "eth0"
    },
    {
      "address": "127.0.0.1",
      "port": 1080,
      "family": "ipv4",
      "interface_name": "lo",
      "loopback": true
    }
  ],
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
          request: 'curl -X POST http://127.0.0.1:8890/api/v1/reload',
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
          '返回最近保留周期内的流量审计快照，按 user/upstream/target 三维聚合字节数。流量按自然日持久化，默认保留 30 天；热加载配置与重启不会清空历史。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "enabled": true,
  "retention_days": 30,
  "entries": [
    {
      "user": "alice",
      "upstream": "us-east-1",
      "target": "example.com",
      "bytes_in": 1024,
      "bytes_out": 2048,
      "total": 3072
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/traffic',
          response: `{
  "enabled": true,
  "retention_days": 30,
  "entries": [
    {
      "user": "alice",
      "upstream": "us-east-1",
      "target": "example.com",
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
      {
        id: 'version',
        method: 'GET',
        path: '/api/v1/version',
        summary: '版本信息',
        description: '返回网关版本、提交、构建时间、Go 版本与运行时长。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "version": "0.0.0-beta",
  "commit": "abcdef0",
  "build_time": "2025-01-01T00:00:00Z",
  "go_version": "go1.22.12",
  "platform": "linux/amd64",
  "uptime_seconds": 3600
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/version',
          response: `{
  "version": "0.0.0-beta",
  "commit": "abcdef0",
  "build_time": "2025-01-01T00:00:00Z",
  "go_version": "go1.22.12",
  "platform": "linux/amd64",
  "uptime_seconds": 3600
}`,
        },
        statusCodes: [
          { code: 200, description: '返回版本信息' },
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
        description:
          '返回所有上游代理配置列表。URL 已脱敏，密码永不下发；配置了独立认证凭据时只回显 username 与 has_password。enabled=false 表示已暂停调度与健康检查。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "upstreams": [
    {
      "name": "us-east-1",
      "url": "socks5://127.0.0.1:1080",
      "username": "proxy-user",
      "has_password": true,
      "enabled": true,
      "types": ["socks5"],
      "priority": 1,
      "weight": 1,
      "strategy": "weighted_round_robin",
      "health_check": { "type": "http", "target": "", "path": "", "expected_codes": [], "interval_seconds": 10, "timeout_seconds": 5, "failure_threshold": 3, "success_threshold": 2 }
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/config/upstreams',
          response: `{
  "upstreams": [
    {
      "name": "us-east-1",
      "url": "socks5://127.0.0.1:1080",
      "username": "proxy-user",
      "has_password": true,
      "enabled": true,
      "types": ["socks5"],
      "priority": 1,
      "weight": 1,
      "strategy": "weighted_round_robin",
      "health_check": { "type": "http", "target": "", "path": "", "expected_codes": [], "interval_seconds": 10, "timeout_seconds": 5, "failure_threshold": 3, "success_threshold": 2 }
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
          '添加一个新的上游代理。请求体最大 1 MiB，严格模式（DisallowUnknownFields）。name 不能与已有上游重复。认证凭据用 username/password 独立字段传入，两者必须成对出现，且不能与 URL 内嵌 userinfo 同时配置；password 支持 ${ENV} 环境变量注入。enabled 省略时默认启用。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "name": "my-upstream",
  "url": "socks5://127.0.0.1:1080",
  "username": "proxy-user",
  "password": "\${PROXY_PASSWORD}",
  "types": ["socks5"],
  "priority": 1,
  "weight": 1,
  "strategy": "weighted_round_robin"
}`,
        responseBody: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/upstreams \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "my-upstream",
    "url": "socks5://127.0.0.1:1080",
    "types": ["socks5"],
    "priority": 1,
    "weight": 1,
    "strategy": "weighted_round_robin"
  }'`,
          response: `{
  "status": "ok",
  "name": "my-upstream"
}`,
        },
        statusCodes: [
          { code: 201, description: '添加成功' },
          { code: 400, description: '请求体格式错误或字段校验失败' },
          { code: 400, description: '上游名称已存在或配置校验失败' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'upstreams-update',
        method: 'PUT',
        path: '/api/v1/config/upstreams/{name}',
        summary: '更新上游',
        description:
          '更新指定上游代理配置。priority、weight、enabled 为可选字段（指针类型），未提供时保持原值；enabled=false 会立即暂停该上游的流量调度与健康检查。请求体中 name 非空且与路径中旧名称不同时执行改名，并同步更新路由规则中对旧名称的引用。username/password 同样按“未提供即保留”处理，传空字符串表示清除凭据；两者必须成对，且不能与 URL 内嵌 userinfo 同时存在。',
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
  "username": "proxy-user",
  "password": "\${PROXY_PASSWORD}",
  "types": ["socks5"],
  "priority": 2,
  "weight": 3,
  "strategy": "weighted_round_robin"
}`,
        responseBody: `{
  "status": "ok",
  "name": "my-upstream-new"
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8890/api/v1/config/upstreams/my-upstream \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "my-upstream-new",
    "url": "socks5://127.0.0.1:1081",
    "types": ["socks5"],
    "priority": 2,
    "weight": 3,
    "strategy": "weighted_round_robin"
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
          request: 'curl -X POST http://127.0.0.1:8890/api/v1/config/upstreams/my-upstream/check',
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
          { code: 400, description: '检查请求无法执行（如目标地址非法）' },
          { code: 404, description: '上游不存在，错误码 upstream_not_found' },
          { code: 409, description: '上游已禁用，不会参与检查，错误码 upstream_disabled' },
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
          request: 'curl -X POST http://127.0.0.1:8890/api/v1/config/upstreams/check',
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
          request: 'curl -X DELETE http://127.0.0.1:8890/api/v1/config/upstreams/my-upstream',
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
      {
        id: 'upstreams-batch-delete',
        method: 'POST',
        path: '/api/v1/config/upstreams/batch-delete',
        summary: '批量删除上游',
        description:
          '一次请求删除多个上游：整批只写一次配置文件、只热加载一次，并同步清理路由规则与 socks5h_redirect 中对这些上游的引用。不存在的名称不会中断整批，会出现在 failed 里。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "names": ["us-east-1", "us-west-1"]
}`,
        responseBody: `{
  "status": "ok",
  "deleted": ["us-east-1", "us-west-1"],
  "failed": [
    { "target": "not-exist", "error": "upstream not found" }
  ]
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/upstreams/batch-delete \\
  -H "Content-Type: application/json" \\
  -d '{"names": ["us-east-1", "us-west-1"]}'`,
          response: `{
  "status": "ok",
  "deleted": ["us-east-1", "us-west-1"],
  "failed": []
}`,
        },
        statusCodes: [
          { code: 200, description: '请求已处理，逐项结果见 deleted / failed' },
          { code: 400, description: '请求体格式错误或 names 为空' },
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
        description:
          '返回所有路由规则列表，按配置顺序排列。disabled 为 true 表示该规则保留但不参与匹配。',
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
      "upstreams": ["us-east-1"],
      "disabled": true
    }
  ]
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/config/rules',
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
          '在规则列表末尾追加一条路由规则。支持的规则类型：domain、domain_suffix、domain_keyword、domain_regex、ip_cidr、geosite、match。支持的动作：direct（只直连）、proxy（只走上游）、direct_first（直连优先）、proxy_first（代理优先）、auto（沿用全局 auto_mode）、deny（拒绝，match 类型不可用）。',
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
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/rules \\
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
          request: 'curl -X DELETE http://127.0.0.1:8890/api/v1/config/rules/0',
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
      {
        id: 'rules-batch-delete',
        method: 'POST',
        path: '/api/v1/config/rules/batch-delete',
        summary: '批量删除路由规则',
        description:
          '一次请求删除多条规则，整批只写一次配置文件、只热加载一次。indexes 一律按删除前的原始位置解释，服务端内部从大到小处理，因此调用方不必自己倒序，也不会因为边删边漂移删错行。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "indexes": [0, 2]
}`,
        responseBody: `{
  "status": "ok",
  "deleted": ["0", "2"],
  "failed": [
    { "target": "9", "error": "rule index 9 out of range (0-2)" }
  ]
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/rules/batch-delete \\
  -H "Content-Type: application/json" \\
  -d '{"indexes": [0, 2]}'`,
          response: `{
  "status": "ok",
  "deleted": ["0", "2"],
  "failed": []
}`,
        },
        statusCodes: [
          { code: 200, description: '请求已处理，逐项结果见 deleted / failed' },
          { code: 400, description: '请求体格式错误或 indexes 为空' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-batch-toggle',
        method: 'POST',
        path: '/api/v1/config/rules/batch-toggle',
        summary: '批量启用/禁用路由规则',
        description:
          '一次请求切换多条规则的 disabled 状态，整批只写一次配置文件、只热加载一次。indexes 按调用前的原始位置解释，越界索引记入 failed，其余项照常生效。禁用的规则保留在配置中但不参与匹配。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "indexes": [0, 2],
  "disabled": true
}`,
        responseBody: `{
  "status": "ok",
  "updated": ["0", "2"],
  "failed": [
    { "target": "9", "error": "rule index 9 out of range (0-2)" }
  ]
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/rules/batch-toggle \\
  -H "Content-Type: application/json" \\
  -d '{"indexes": [0, 2], "disabled": true}'`,
          response: `{
  "status": "ok",
  "updated": ["0", "2"]
}`,
        },
        statusCodes: [
          { code: 200, description: '请求已处理，逐项结果见 updated / failed' },
          { code: 400, description: '请求体格式错误或 indexes 为空' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-update',
        method: 'PUT',
        path: '/api/v1/config/rules/{index}',
        summary: '更新路由规则',
        description:
          '按索引替换一条路由规则。写入前会校验规则类型、动作、匹配值（正则/CIDR）以及引用的上游是否存在；校验失败时配置文件保持不变。',
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
        requestBody: `{
  "type": "domain_suffix",
  "values": ["example.com"],
  "action": "proxy",
  "upstreams": ["us-east-1"]
}`,
        responseBody: `{
  "status": "ok"
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8890/api/v1/config/rules/0 \\
  -H "Content-Type: application/json" \\
  -d '{
    "type": "domain_suffix",
    "values": ["example.com"],
    "action": "proxy",
    "upstreams": ["us-east-1"]
  }'`,
          response: `{
  "status": "ok"
}`,
        },
        statusCodes: [
          { code: 200, description: '更新成功' },
          { code: 400, description: '规则校验失败' },
          { code: 404, description: '索引超出范围' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-replace',
        method: 'PUT',
        path: '/api/v1/config/rules',
        summary: '整体替换路由规则',
        description:
          '用请求体中的列表整体替换现有规则（文本视图保存使用）。服务端先校验全部规则再落盘：任一条非法都不会写入，成功后只写一次配置文件、只热加载一次。rules 字段必须出现，传空数组表示清空全部规则。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "rules": [
    { "type": "geosite", "values": ["cn"], "action": "direct" },
    { "type": "geosite", "values": ["geolocation-!cn"], "action": "proxy" },
    { "type": "match", "action": "proxy" }
  ]
}`,
        responseBody: `{
  "status": "ok",
  "count": 3
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8890/api/v1/config/rules \\
  -H "Content-Type: application/json" \\
  -d '{"rules":[{"type":"geosite","values":["cn"],"action":"direct"}]}'`,
          response: `{
  "status": "ok",
  "count": 1
}`,
        },
        statusCodes: [
          { code: 200, description: '替换成功' },
          { code: 400, description: 'rules 缺失或任一规则校验失败' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'rules-move',
        method: 'POST',
        path: '/api/v1/config/rules/{index}/move',
        summary: '调整规则顺序',
        description: '将指定规则上移或下移一位。规则按顺序匹配，顺序会影响最终路由结果。',
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
        requestBody: `{
  "direction": "up"
}`,
        responseBody: `{
  "status": "ok"
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/rules/1/move \\
  -H "Content-Type: application/json" \\
  -d '{"direction": "up"}'`,
          response: `{
  "status": "ok"
}`,
        },
        statusCodes: [
          { code: 200, description: '调整成功' },
          { code: 400, description: 'direction 非法或已到边界' },
          { code: 404, description: '索引超出范围' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
]
