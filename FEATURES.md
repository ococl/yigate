# YiGate 功能清单

本文档汇总 YiGate 当前已实现的核心能力。功能随版本演进，请以代码与 `src/config.sample.yml` 为准。

## 1. 协议接入

- **单端口多协议**：同一监听端口自动识别并分流以下三类流量：
  - HTTP 普通代理（`CONNECT` 以外的方法）
  - HTTPS 隧道（`CONNECT` 方法）
  - SOCKS5（含协商、命令分发、IPv6 回复）
- **SOCKS5 扩展**：
  - BIND（仅 SOCKS5 上游）
  - UDP ASSOCIATE（仅 SOCKS5 上游）
  - socks5h 远端域名解析（域名发往上由侧解析，降低本地 DNS 泄露）
  - 首包 SNI / HTTP Host 嗅探恢复域名（`socks_sniff.go`，含 IPv6 处理）
- **TLS 终止**：listener 层统一包装 TLS，支持服务端证书与客户端 CA（mTLS），协议层无感知。

## 2. 路由

- **规则类型**：
  - `domain`：精确域名
  - `domain_suffix`：后缀匹配
  - `domain_keyword`：关键字包含
  - `domain_regex`：正则匹配
  - `ip_cidr`：IPv4 / IPv6 CIDR
  - `geosite`：GeoSite 数据匹配（数据版本、手动更新与多节点同步见下）
  - `match`：通用匹配表达式
- **GeoSite 数据管理**：
  - 独立的「GeoSite 数据」页面：版本（内容 SHA-256）、大小、更新时间、来源地址与上次检查时间回显。
  - 手动更新：按条件请求（`ETag`/`If-None-Match`）拉取，内容未变化返回 304 且不触发重载；变化后原子替换并热重载。
  - 下游同步：`GET /api/v1/geosite/dat` 提供只读下载，`sync_token` 仅授予读权限；下游把 `update_url` 指向该地址、`update_token` 填同步令牌即可自动跟随。
- **动作**：
  - `direct`：只直连（失败不回落）
  - `proxy`：只走上游（失败不回落）
  - `direct_first`：直连优先，失败回退上游
  - `proxy_first`：代理优先，失败回退直连
  - `auto`：沿用全局 `routing.auto.mode`，按策略依次尝试并缓存成功结果
  - `deny`：拒绝该目标
- **规则匹配**：`routing.rules` 按序匹配，首个命中生效；未命中时由 `routing.final` 兜底。
- **规则停用**：每条规则可单独启用/禁用（`disabled: true`），禁用后保留在配置中但不参与匹配；WebUI 支持行内开关、勾选批量启用/禁用，文本视图用行首 `!` 表示禁用。
- **路由缓存**：命中结果可缓存，降低重复解析开销。

## 3. 上游与负载

- **类型**：`http` / `https` / `socks5` / `socks5h`。
- **选择策略**：
  - `weighted_round_robin`：加权轮询
  - `least_connections`：最少连接
  - `consistent_hash`：一致性哈希
  - `source_ip_hash`：源 IP 哈希
- **优先级与权重**：支持优先级分组、权重、dead 节点标记。
- **健康检查**：TCP / HTTP / HTTPS / TLS 四种探测，异常节点自动摘除与恢复。
- **指定上游白名单**：可按规则限定可用上游集合。

## 4. DNS

- 内置 **DoH / DoT** 解析器，可 fallback 到系统 DNS。
- TTL 缓存，减少重复查询。
- `ip_precedence`：`ipv4_first`（默认）/ `ipv6_first` / `system`，控制地址优先级。

## 5. 透明代理（仅 Linux）

- 模式：`redirect`（iptables REDIRECT）/ `tproxy`（TPROXY）。
- `SO_ORIGINAL_DST` 还原原始目标；`IP_TRANSPARENT` 透明转发。
- `scripts/` 下提供 iptables 与 nftables 示例规则。
- 非 Linux 平台提供桩函数，调用即返回错误。

## 6. 访问控制与安全

- **客户端访问控制**：CIDR 白名单、单 IP 限速、用户并发限制。
- **身份认证**：`users` 定义凭据；为空则允许匿名。
- **SSRF 防护**：`destination_policy.protect_private` 默认开启，拒绝回环 / 私网 / 链路本地 / 云元数据等敏感目标，独立于路由作为最后防线。
- **管理接口保护**：`management_auth_token` 启用 Bearer 鉴权；非回环暴露时强制要求。

## 7. 配置与运维

- **YAML 配置**：`config.sample.yml` 为完整示例。
- **环境变量展开**：`${ENV_NAME}` 严格展开，缺失 / 空值导致启动失败（避免泄露凭据）。
- **热加载**：`fsnotify` 监听变更，原子替换组件；监听地址 / TLS / 上游 / 规则可热更新，先关旧 listener 再建新。
- **运行时配置 API**：增删查改 upstream / rules / users，原子写入 YAML，保留 `${ENV_VAR}` 占位符。
- **system service**：`kardianos/service` 支持 install / uninstall / start / stop / restart。

## 8. 可观测性

- `GET /health`：进程探活（匿名，返回纯文本 `UP`；不代表代理功能可用）。
- `GET /metrics`：Prometheus 文本指标，前缀 `yigate_`，含流量审计指标。
- `GET /api/v1/status`：运行摘要。
- `GET /api/v1/traffic`：流量统计快照。
- `/debug/pprof/*`：仅 `enable_pprof: true` 时启用（敏感入口）。

## 9. 流量审计与配额

- 按 `user` / `upstream` / `target` 三维统计收发字节与速率。
- 流量按自然日持久化到 `traffic.json`，默认保留 30 天并自动清理；热加载与重启不清空。
- 支持配额限制，超出可拒绝或限速。
- 隧道超时与流量统计封装（`timeout.go`）。

## 10. 管理与 Web 界面

- 内嵌 Vue 3 + antdv-next 管理界面，随二进制启动，无需额外部署。
- 提供上游、规则、用户的可视化查看与运行时变更。
- 路由规则支持可视化与文本双视图：文本按「类型,匹配值|匹配值,动作[,上游]」逐行编辑，保存时整体替换、一次落盘。
- 界面支持简体中文与 English 切换：语言记录在浏览器本地，未选择时默认简体中文；API 文档页的接口正文与示例仍为中文。
- 前端开发服务器代理 `/api` 到 Go 管理端口 `127.0.0.1:8890`。

## 11. 部署形态

- **原生二进制**：`data/config.yml` + `logs/`，便于与容器共享目录布局。
- **Docker**：`docker-compose.yml` 一键部署，挂载 `./data` 与 `./logs`。
- **systemd**：`deploy/systemd/` 提供 unit 与安装脚本。
- **跨平台构建**：`scripts/platforms.sh` 定义 26 个目标（Linux / macOS / Windows / FreeBSD / OpenBSD / NetBSD，含 arm / mips / riscv / loong64 等）。

## 12. 明确不支持

- 任何混淆协议、流量伪装、非合规隧道 / 传输方式。
- BIND / UDP ASSOCIATE 对 HTTP / HTTPS 上游（仅 SOCKS5 上游支持）。
- TPROXY 在非 Linux 平台（桩函数返回错误）。
