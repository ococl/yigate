import type { ApiGroup } from './types'

export const extraGroups: ApiGroup[] = [
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
          request: 'curl http://127.0.0.1:8890/api/v1/config/users',
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
          request: `curl -X POST http://127.0.0.1:8890/api/v1/config/users \\
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
          { code: 400, description: '用户名已存在' },
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
          request: 'curl -X DELETE http://127.0.0.1:8890/api/v1/config/users/alice',
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
      {
        id: 'users-update',
        method: 'PUT',
        path: '/api/v1/config/users/{username}',
        summary: '修改用户密码',
        description: '修改指定用户的代理认证密码，新连接立即使用新密码。',
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
        requestBody: `{
  "password": "new-secret"
}`,
        responseBody: `{
  "status": "ok",
  "username": "alice"
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8890/api/v1/config/users/alice \\
  -H "Content-Type: application/json" \\
  -d '{"password": "new-secret"}'`,
          response: `{
  "status": "ok",
  "username": "alice"
}`,
        },
        statusCodes: [
          { code: 200, description: '修改成功' },
          { code: 400, description: '密码为空' },
          { code: 404, description: '用户不存在' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'settings',
    title: '全局设置',
    description: '读取与修改全局运行设置。时间字段统一以秒为单位；修改会持久化并热加载。',
    endpoints: [
      {
        id: 'settings-get',
        method: 'GET',
        path: '/api/v1/config/settings',
        summary: '读取全局设置',
        description:
          '返回路由、DNS、超时、目标策略、访问控制、流量审计、日志级别等全局设置。多监听端口模式下 listen 为逗号拼接的实际监听地址，socks5/访问控制/并发上限返回首个端口的值；management_token_set 表示是否已配置管理令牌（不回显明文）。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        responseBody: `{
  "listen": "127.0.0.1:4479,127.0.0.1:4480",
  "management": "127.0.0.1:8892",
  "management_token_set": true,
  "webui_enabled": true,
  "routing_final": "auto",
  "auto_mode": "direct_first",
  "dns_type": "system",
  "dial_timeout_seconds": 10,
  "traffic_audit_enabled": true,
  "log_level": "info"
}`,
        example: {
          request: 'curl http://127.0.0.1:8890/api/v1/config/settings',
          response: `{
  "routing_final": "auto",
  "auto_mode": "direct_first",
  "dns_type": "system",
  "dial_timeout_seconds": 10,
  "traffic_audit_enabled": true,
  "log_level": "info"
}`,
        },
        statusCodes: [
          { code: 200, description: '返回设置视图' },
          { code: 401, description: '未认证' },
        ],
      },
      {
        id: 'settings-update',
        method: 'PUT',
        path: '/api/v1/config/settings',
        summary: '修改全局设置',
        description:
          '部分更新全局设置，仅提交需要修改的字段。校验失败时不会写入配置文件，运行时保持原配置。webui_enabled、enable_pprof、management_auth_token 可热加载；management_auth_token 传空字符串表示清除，但管理端口为非回环地址时会被拒绝。listen/management 属于启动参数，需重启生效。',
        auth: true,
        authNote: '当 management_auth_token 非空时需要认证',
        params: [],
        requestBody: `{
  "routing_final": "proxy",
  "dns_type": "doh",
  "doh_url": "https://dns.example/dns-query",
  "traffic_audit_enabled": true,
  "traffic_retention_days": 30,
  "geosite_auto_update": true,
  "geosite_use_upstream": true,
  "geosite_update_url": "http://node-a:8892/api/v1/geosite/dat",
  "geosite_update_token": "<上游同步令牌>",
  "geosite_sync_enabled": true,
  "webui_enabled": false,
  "management_auth_token": "new-strong-token",
  "log_level": "debug"
}`,
        responseBody: `{
  "status": "ok"
}`,
        example: {
          request: `curl -X PUT http://127.0.0.1:8890/api/v1/config/settings \\
  -H "Content-Type: application/json" \\
  -d '{"log_level": "debug"}'`,
          response: `{
  "status": "ok"
}`,
        },
        statusCodes: [
          { code: 200, description: '修改成功并已热加载' },
          { code: 400, description: '校验失败或热加载失败（已回滚）' },
          { code: 401, description: '未认证' },
        ],
      },
    ],
  },
  {
    key: 'geosite',
    title: 'GeoSite 数据',
    description:
      'GeoSite 域名库的版本回显、手动更新与下游同步。数据文件为 v2fly dlc.dat（GeoSiteList protobuf）。',
    endpoints: [
      {
        id: 'geosite-info',
        method: 'GET',
        path: '/api/v1/geosite',
        summary: '查看 GeoSite 版本与同步配置',
        description:
          '返回数据文件路径、是否存在、大小、SHA-256 与短版本号、数据更新时间、来源地址、上次检查时间、最近一次更新错误，以及自动更新、上游代理与下游同步配置。数据路径由系统托管（配置文件同目录的 geosite.dat），不可通过管理接口修改。令牌在本响应中只回显是否已设置；同步令牌明文可通过 GET /api/v1/geosite/sync-token 读取（仅管理令牌）。',
        auth: true,
        authNote: '需要管理令牌；开启 sync_enabled 后也可使用只读同步令牌',
        params: [],
        responseBody: `{
  "path": "/var/lib/yigate/data/geosite.dat",
  "download_path": "/api/v1/geosite/dat",
  "exists": true,
  "size": 4832910,
  "version": "8f3c1a02b7d9",
  "sha256": "8f3c1a02b7d9...",
  "updated_at": "2026-11-07T02:10:11Z",
  "source_url": "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
  "last_checked_at": "2026-11-07T02:10:11Z",
  "auto_update": true,
  "use_upstream": true,
  "update_url": "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
  "update_interval_seconds": 86400,
  "update_token_set": false,
  "sync_enabled": true,
  "sync_token_set": true
}`,
        example: {
          request: `curl http://127.0.0.1:8892/api/v1/geosite \\
  -H "Authorization: Bearer <token>"`,
          response: `{
  "path": "/var/lib/yigate/data/geosite.dat",
  "exists": true,
  "version": "8f3c1a02b7d9",
  "auto_update": true,
  "sync_enabled": true
}`,
        },
        statusCodes: [
          { code: 200, description: '返回版本与配置' },
          { code: 401, description: '未认证' },
          { code: 501, description: 'GeoSite 接口未初始化' },
        ],
      },
      {
        id: 'geosite-update',
        method: 'POST',
        path: '/api/v1/geosite/update',
        summary: '立即更新 GeoSite 数据',
        description:
          '立即从 routing.geosite.update_url 拉取一次数据：携带 Bearer 更新令牌，并带 If-None-Match 条件请求头。内容未变化时返回 changed=false；变化后会原子替换文件并热重载路由，无需重启。该操作不能被只读同步令牌调用。',
        auth: true,
        authNote: '始终要求管理令牌',
        params: [],
        responseBody: `{
  "status": "ok",
  "changed": true,
  "info": {
    "path": "/var/lib/yigate/data/geosite.dat",
    "exists": true,
    "version": "1a2b3c4d5e6f",
    "size": 4840221,
    "updated_at": "2026-11-07T03:20:00Z"
  }
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8892/api/v1/geosite/update \\
  -H "Authorization: Bearer <token>"`,
          response: `{
  "status": "ok",
  "changed": true,
  "info": { "version": "1a2b3c4d5e6f", "exists": true }
}`,
        },
        statusCodes: [
          { code: 200, description: '更新完成（changed 表示内容是否变化）' },
          { code: 401, description: '未认证' },
          { code: 500, description: '下载或热重载失败' },
        ],
      },
      {
        id: 'geosite-sync-token-get',
        method: 'GET',
        path: '/api/v1/geosite/sync-token',
        summary: '读取下游同步令牌',
        description:
          '返回当前只读同步令牌明文，供 WebUI 默认掩码展示与复制。响应带 Cache-Control: no-store；该操作不能被只读同步令牌调用。',
        auth: true,
        authNote: '始终要求管理令牌',
        params: [],
        responseBody: `{
  "status": "ok",
  "token": "64 位十六进制随机串",
  "info": {
    "path": "/var/lib/yigate/data/geosite.dat",
    "exists": true,
    "sync_token_set": true
  }
}`,
        example: {
          request: `curl http://127.0.0.1:8892/api/v1/geosite/sync-token \\
  -H "Authorization: Bearer <token>"`,
          response: `{
  "status": "ok",
  "token": "9f2c..."
}`,
        },
        statusCodes: [
          { code: 200, description: '返回当前令牌（可能为空字符串）' },
          { code: 401, description: '未认证或使用只读同步令牌' },
        ],
      },
      {
        id: 'geosite-sync-token',
        method: 'POST',
        path: '/api/v1/geosite/sync-token',
        summary: '重新生成下游同步令牌',
        description:
          '由系统生成新的强随机只读同步令牌并立即写回配置热加载，响应返回新令牌明文。WebUI 的「重新生成」按钮即调用该接口。该操作不能被只读同步令牌调用。',
        auth: true,
        authNote: '始终要求管理令牌',
        params: [],
        responseBody: `{
  "status": "ok",
  "token": "64 位十六进制随机串",
  "info": {
    "path": "/var/lib/yigate/data/geosite.dat",
    "exists": true,
    "sync_token_set": true
  }
}`,
        example: {
          request: `curl -X POST http://127.0.0.1:8892/api/v1/geosite/sync-token \\
  -H "Authorization: Bearer <token>"`,
          response: `{
  "status": "ok",
  "token": "9f2c...",
  "info": { "sync_token_set": true }
}`,
        },
        statusCodes: [
          { code: 200, description: '生成成功，返回新令牌明文' },
          { code: 401, description: '未认证或使用只读同步令牌' },
          { code: 500, description: '写入配置或热加载失败' },
        ],
      },
      {
        id: 'geosite-dat',
        method: 'GET',
        path: '/api/v1/geosite/dat',
        summary: '下载 GeoSite 数据（下游同步）',
        description:
          '返回原始 dlc.dat 文件，供下游节点同步。响应包含 ETag（内容 SHA-256）、X-GeoSite-Version 与 X-GeoSite-SHA256；支持 If-None-Match 条件请求，数据未变化时返回 304。下游把 routing.geosite.update_url 指向该地址，并把只读同步令牌填入 update_token 即可自动同步。',
        auth: true,
        authNote: '管理令牌或 sync_token 只读令牌',
        params: [],
        responseBody: 'geosite.dat 二进制内容（Content-Type: application/octet-stream）',
        example: {
          request: `curl -o geosite.dat http://node-a:8892/api/v1/geosite/dat \\
  -H "Authorization: Bearer <sync-token>"

# 再次请求会携带 ETag，未变化时返回 304
curl -i http://node-a:8892/api/v1/geosite/dat \\
  -H "Authorization: Bearer <sync-token>" \\
  -H 'If-None-Match: "8f3c1a02b7d9..."'`,
          response: `HTTP/1.1 200 OK
ETag: "8f3c1a02b7d9..."
X-GeoSite-Version: 8f3c1a02b7d9
Content-Length: 4832910`,
        },
        statusCodes: [
          { code: 200, description: '返回数据文件' },
          { code: 304, description: '内容未变化（If-None-Match 命中）' },
          { code: 401, description: '未认证或同步未开启' },
          { code: 404, description: '数据文件不存在' },
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
          request: 'curl http://127.0.0.1:8890/debug/pprof/',
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
          request: 'curl -o cpu.prof "http://127.0.0.1:8890/debug/pprof/profile?seconds=30"',
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
          request: 'curl -o heap.prof http://127.0.0.1:8890/debug/pprof/heap',
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
          request: 'curl -o goroutine.prof http://127.0.0.1:8890/debug/pprof/goroutine',
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
          request: 'curl http://127.0.0.1:8890/debug/pprof/trace?seconds=5',
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
