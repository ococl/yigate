# k5pw 配置说明（Schema）

本文档精确描述 `k5pw` 配置文件中所有字段的含义、类型、默认值与校验规则。建议结合 `src/config.sample.yml` 阅读。

## 约定

- 字段类型中的 `Duration` 兼容两种写法：
  - 纯数字，表示秒，例如 `check_interval: 30` 表示 30 秒。
  - Go duration 字符串，例如 `1500ms`、`2s`、`1h`、`24h`。
- 路径字段的相对路径会相对于**配置文件所在目录**解析。
- 所有环境变量注入均使用 `${ENV_NAME}` 语法，配置加载时由 `os.ExpandEnv` 统一替换。

---

## 根字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `listen` | string | 是 | `127.0.0.1:8891` | 代理监听地址。必须能被 `net.ResolveTCPAddr` 解析。 |
| `management` | string | 否 | 空 | 管理端口监听地址，例如 `127.0.0.1:8892`。配置后提供 `/health` 与 `/metrics`。空表示不启用。 |
| `enable_pprof` | bool | 否 | `false` | 是否在管理端口注册 `/debug/pprof` 系列路由。仅当 `management` 非空时生效。 |
| `users` | map[string]string | 否 | `{}` | 用户名密码映射。留空表示匿名，同时禁用 HTTP Basic 与 SOCKS5 用户名密码认证。 |
| `upstreams` | [Upstream](#upstream)[] | 否 | `[]` | 上游代理列表。 |
| `routing` | [Routing](#routing) | 否 | 见 [Routing 默认值](#routing-默认值) | 路由与分流规则。 |
| `timeouts` | [Timeouts](#timeouts) | 否 | 见 [Timeouts 默认值](#timeouts-默认值) | 超时参数。 |
| `log` | [Log](#log) | 否 | 见 [Log 默认值](#log-默认值) | 日志配置。 |
| `access_control` | [AccessControl](#accesscontrol) | 否 | 空 | 访问控制：IP 白名单、速率限制、单用户并发限制。 |
| `check_interval` | int / Duration | 否 | `30` | 上游健康检查周期，秒。`0` 表示禁用健康检查。 |
| `max_connections` | int | 否 | `1024` | 全局最大并发连接数。`< 0` 会拒绝启动，`0` 会被重置为默认值。 |

---

## Upstream

单个上游代理配置。

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `name` | string | 否 | `upstream-1` | 上游名称，用于路由规则 `upstreams` 字段引用。空时自动生成 `upstream-i`。 |
| `url` | string | 是 | 无 | 上游地址。支持 `http`、`https`、`socks5`、`socks5h` scheme。认证信息强烈建议使用 `${ENV}` 注入。 |
| `types` | string[] | 否 | URL 的 scheme | 可用代理类型。支持 `http`、`https`、`socks5`、`socks5h`。可覆盖 URL scheme 的能力。 |
| `priority` | int | 否 | `0` | 优先级，数值越小越优先。 |
| `weight` | int | 否 | `1` | 同优先级下的平滑加权轮询权重。范围 `0~10000`。 |

**校验规则**

- `url` 不能为空。
- `weight` 必须 `>= 0` 且 `<= 10000`。
- `types` 若有值，每个元素必须是支持的类型之一。

---

## Routing

### 字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `final` | string | 否 | `auto` | 兜底动作：`direct`、`proxy`、`auto`。 |
| `geosite` | [GeoSite](#geosite) | 否 | 见下文 | GEOSITE 数据库加载与自动更新。 |
| `auto` | [Auto](#auto) | 否 | 见下文 | `auto` 动作参数。 |
| `socks5h_redirect` | [Socks5HRedirect](#socks5hredirect) | 否 | 见下文 | 优先通过 SOCKS5 上游以域名方式转发。 |
| `rules` | [Rule](#rule)[] | 否 | `[]` | 路由规则，按顺序匹配，命中第一条即返回。 |

### Routing 默认值

```yaml
routing:
  final: auto
  geosite:
    auto_update: true
    update_url: "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
    update_interval: 24h
  auto:
    mode: direct_first
    direct_timeout: 1500ms
    cache_ttl: 1h
  socks5h_redirect:
    enabled: true
```

---

## GeoSite

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `path` | string | 否 | 空 | `geosite.dat` 文件路径。相对路径按配置文件目录解析。 |
| `auto_update` | bool | 否 | `true` | 是否启动自动更新。 |
| `update_url` | string | 否 | v2fly `dlc.dat` | 数据库下载地址。 |
| `update_interval` | Duration | 否 | `24h` | 自动更新周期。 |

**校验规则**

- 若配置了 `geosite` 类型规则且 `path` 非空，启动时会尝试加载文件；文件不存在且未开启自动更新时可能拒绝启动。

---

## Auto

`auto` 动作参数。

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `mode` | string | 否 | `direct_first` | `direct_first` 先尝试直连，失败再代理；`proxy_first` 相反。 |
| `direct_timeout` | Duration | 否 | `1500ms` | `direct_first` 模式下直连快速探测超时。 |
| `cache_ttl` | Duration | 否 | `1h` | 自动选择结果缓存有效期。 |

**校验规则**

- `mode` 必须是 `direct_first` 或 `proxy_first`。

---

## Socks5HRedirect

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `enabled` | bool | 否 | `true` | 是否开启 socks5h 重定向。 |
| `upstreams` | string[] | 否 | 空 | 可选的 SOCKS5 上游名称白名单；空表示使用全部 `socks5`/`socks5h` 上游。 |

**校验规则**

- `upstreams` 中每个名称必须存在于全局 `upstreams` 列表中。

---

## Rule

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `type` | string | 是 | 无 | 规则类型，见下表。 |
| `values` | string[] | 是 | 无 | 匹配值列表。 |
| `action` | string | 是 | 无 | 动作：`direct`、`proxy`、`auto`。 |
| `upstreams` | string[] | 否 | 空 | 动作 `proxy` 或 `auto` 时可选的白名单上游名称列表。 |

**支持的 `type`**

| 类型 | 说明 | 示例 |
|---|---|---|
| `domain` | 完整域名匹配。 | `["example.com"]` |
| `domain_suffix` | 域名后缀匹配，`github.com` 匹配 `github.com` 和 `api.github.com`。 | `["github.com"]` |
| `domain_keyword` | 域名包含关键词。 | `["google"]` |
| `domain_regex` | 域名正则匹配。 | `["^.*\\.internal$"]` |
| `ip_cidr` | 目标为 IP 时匹配 CIDR。 | `["10.0.0.0/8"]` |
| `geosite` | 匹配 `geosite.dat` 中的规则集 code。 | `["CN"]` |
| `match` | 无条件匹配，通常用作兜底规则。 | `[]` |

**校验规则**

- `type` 必须是上表之一。
- `action` 必须是 `direct`、`proxy`、`auto` 之一。
- `upstreams` 中每个名称必须存在于全局 `upstreams` 列表中。

---

## Timeouts

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `dial` | Duration | 否 | `10s` | 目标拨号超时。 |
| `handshake` | Duration | 否 | `10s` | 协议握手超时（HTTP/SOCKS5 首包）。 |
| `http_exchange` | Duration | 否 | `60s` | 普通 HTTP 请求交换超时。 |
| `tunnel_idle` | Duration | 否 | `5m` | CONNECT / SOCKS5 隧道空闲超时。 |

---

## Log

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `level` | string | 否 | `info` | 日志级别：`debug`、`info`、`warn`、`error`、`fatal`、`panic`。 |
| `format` | string | 否 | `json` | 输出格式：`json` 或 `console`。 |
| `output` | string | 否 | `stdout` | 输出目标：`stdout`、`stderr`、`file`。 |
| `file` | string | 否 | 空 | `output: file` 时必填。日志文件路径。 |
| `rotation` | string | 否 | `size` | 滚动模式：`size` 或 `daily`。 |
| `max_size` | int | 否 | `100` | `size` 模式下单文件大小上限（MB）。 |
| `max_age` | int | 否 | `7` | 历史日志保留天数，至少为 1。 |
| `max_backups` | int | 否 | `10` | `size` 模式下最多保留的滚动文件数。 |
| `compress` | bool | 否 | `true` | 是否对历史日志进行 gzip 压缩。 |

**校验规则**

- `output: file` 时 `file` 不能为空。
- `level` 必须是有效日志级别。
- `format` 必须是 `json` 或 `console`。
- `rotation` 必须是 `size` 或 `daily`。

---

## AccessControl

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `allowed_clients` | string[] | 否 | `[]` | 客户端 CIDR 白名单，空表示允许所有。 |
| `rate_limit` | object | 否 | 见下 | 单 IP 新建连接速率限制。 |
| `rate_limit.connections_per_second` | int | 否 | `0` | 每秒新建连接数，`0` 表示不限速。 |
| `rate_limit.burst` | int | 否 | `0` | 令牌桶突发容量，`0` 时使用 `connections_per_second`。 |
| `max_conns_per_user` | int | 否 | `0` | 单个认证用户最大并发连接数，`0` 表示不限制。 |

**校验规则**

- `allowed_clients` 每项必须是合法 CIDR。
- `connections_per_second` 与 `burst` 为负数时会被视为关闭限速。

---

## 最小可运行配置

```yaml
listen: "0.0.0.0:4479"
```

此配置以匿名模式监听 `0.0.0.0:4479`，所有流量直连，无上游、无规则、无日志文件。

## 生产推荐配置

```yaml
listen: "0.0.0.0:8891"
management: "127.0.0.1:8892"
upstreams:
  - name: "sg"
    url: "socks5://${UPSTREAM_USER}:${UPSTREAM_PASS}@sg.example.com:1080"
    types: ["socks5", "socks5h"]
    priority: 1
    weight: 5
routing:
  final: auto
  rules:
    - type: ip_cidr
      values: ["10.0.0.0/8", "192.168.0.0/16"]
      action: direct
log:
  output: file
  file: "logs/k5pw.log"
  rotation: daily
  max_age: 30
access_control:
  allowed_clients: ["10.0.0.0/8"]
```

---

## 版本历史

配置格式变更请参考 [`CHANGELOG.md`](../CHANGELOG.md)。
