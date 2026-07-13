# 项目特性

## 代理协议

- HTTP、HTTPS CONNECT、SOCKS5 共用单个监听端口。
- 自动识别下游代理协议。
- 支持 HTTP 普通请求代理转发。
- 支持 HTTPS CONNECT 隧道转发。
- 支持 SOCKS5 CONNECT 命令。
- 支持 SOCKS5 BIND 命令。
- 支持 SOCKS5 UDP ASSOCIATE 命令。
- 支持匿名代理访问。
- 支持 HTTP Basic 下游认证。
- 支持 SOCKS5 用户名密码认证。
- 支持 HTTP 上游代理。
- 支持 HTTPS 上游代理。
- 支持 SOCKS5 上游代理。
- 支持 SOCKS5H 远端域名解析。
- 支持 HTTP、HTTPS、SOCKS5 上游代理认证。
- 支持为上游声明可用代理类型。
- 支持多上游代理配置。
- 支持上游代理命名引用。
- 支持上游优先级选择。
- 支持同优先级上游加权轮询。
- 支持上游健康检查。
- 支持在健康检查启用时对失败上游临时摘除与自动恢复。
- 支持规则指定上游白名单。
- 支持直连路由动作。
- 支持代理路由动作。
- 支持自动路由动作。
- 支持 direct_first 自动策略。
- 支持 proxy_first 自动策略。
- 支持自动路由结果缓存。
- 支持域名精确匹配规则。
- 支持域名后缀匹配规则。
- 支持域名关键词匹配规则。
- 支持域名正则匹配规则。
- 支持 IP CIDR 匹配规则（含 IPv6 CIDR）。
- 支持 GEOSITE 匹配规则。
- 支持 match 兜底规则。
- 支持 geosite.dat 本地加载。
- 支持 geosite.dat 自动更新。
- 支持 socks5h 重定向优先转发。
- 支持 socks5h 重定向失败后回退到常规路由策略。
- 支持在 socks5h_redirect 启用时通过 TLS SNI 恢复 SOCKS5 IP 目标域名。
- 支持在 socks5h_redirect 启用时通过 HTTP Host 恢复 SOCKS5 IP 目标域名。
- 支持 SOCKS5 BIND 上游级联。
- 支持 SOCKS5 BIND 上游级联失败后回退本地监听。
- 支持 SOCKS5 UDP ASSOCIATE 上游级联。
- 支持 SOCKS5 UDP ASSOCIATE 上游级联失败后回退本地 UDP 中继。
- 支持 chunked HTTP 请求流式转发。
- 支持大请求体 HTTP 流式转发。
- 支持可缓冲 HTTP 请求体的上游失败重试。
- 支持统一错误码分类。
- 支持 HTTP 错误响应状态映射。
- 支持 SOCKS5 标准回复码映射。
- 支持 SOCKS5 IPv6 地址类型（0x04）请求解析与回复。

## TLS 服务端终止

- 支持代理端口 HTTPS / SOCKS5 over TLS。
- 支持 mTLS 客户端认证（client_ca_file）。
- 支持全局和 per-listener 级别 TLS 配置。
- 支持热加载时 TLS 配置变更切换 listener。

## 负载均衡增强

- 支持最少连接（least_connections）策略。
- 支持一致性哈希（consistent_hash）会话保持。
- 支持源 IP 哈希（source_ip_hash）会话保持。
- 支持熔断与失败转移优化。

## 健康检查增强

- 支持 TCP 健康检查。
- 支持 HTTP 健康检查（自定义路径与预期响应码）。
- 支持 HTTPS 健康检查。
- 支持 TLS 握手健康检查。
- 支持检查间隔、超时、失败阈值 per-upstream 配置。

## 多监听端口策略

- 支持多个 listener 配置，每个端口独立规则/上游/认证策略。
- 支持 per-listener TLS 配置。
- 支持 per-listener 访问控制。
- 支持 per-listener SOCKS5 命令配置。
- 支持 per-listener 最大连接数。
- 向后兼容单端口 listen 配置。
- 支持热加载时按 listener 实例比较与切换。

## DNS 安全解析

- 内置 DoH（DNS-over-HTTPS）解析器。
- 内置 DoT（DNS-over-TLS）解析器。
- 支持并行查询 A 和 AAAA 记录。
- 支持 fallback 到系统 DNS。
- 内置 60 秒 TTL DNS 结果缓存。
- 支持 IPv4/IPv6 优先级策略（ipv4_first/ipv6_first/system）。
- 支持 DNS wire format 编解码（含压缩指针）。

## 运行时配置 API

- 支持通过 REST API 动态增删查改 upstream。
- 支持通过 REST API 动态增删查改路由规则。
- 支持通过 REST API 动态增删查改用户。
- 变更持久化到 YAML 配置文件。
- 支持原子写入（临时文件 + rename）。
- 保留环境变量占位符（${ENV_VAR}）。
- 上游 URL 脱敏输出。
- 请求体严格校验未知字段。
- 删除上游时自动清理路由规则和 socks5h_redirect 引用。
- 所有端点要求管理 Token 认证。

## 流量审计与配额

- 按用户、上游、目标域名三维统计流量。
- 支持每用户流量配额限制。
- 支持每上游流量配额限制。
- 支持可配置的配额统计窗口。
- 通过 /api/v1/traffic 端点查询流量快照。
- 通过 Prometheus 指标暴露流量统计。
- 支持热加载时正确停止旧统计器。

## 透明代理 / TPROXY

- Linux 下支持 TPROXY 透明代理模式。
- 支持 redirect 模式（SO_ORIGINAL_DST）。
- 支持 tproxy 模式（IP_TRANSPARENT，保留源 IP）。
- 支持 IPv4 和 IPv6 透明代理。
- 提供 iptables REDIRECT 配置示例脚本。
- 提供 iptables TPROXY 配置示例脚本（含策略路由）。
- 提供 nftables 配置示例脚本。
- 支持 per-listener TPROXY 配置。
- 非 Linux 平台有桩函数返回错误。

## IPv6 支持

- 监听地址支持 IPv6（[::]:port 双栈，[::1]:port 单栈）。
- DNS 解析支持 IPv6（A + AAAA 并行查询）。
- 路由规则支持 IPv6 CIDR。
- SOCKS5 支持 IPv6 地址类型。
- 目标策略保护 IPv6 敏感地址段（::1/128、::/128、fc00::/7、fe80::/10、ff00::/8）。
- 访问控制支持 IPv6 CIDR 白名单。
- SOCKS5 回复根据连接地址族选择 IPv4/IPv6 类型。

## 目标地址访问策略

- SSRF 防护：拒绝回环、RFC1918 私网、ULA、链路本地、元数据等敏感地址。
- 支持 deny/allow CIDR 列表。
- 支持 deny/allow 域名列表。
- 支持 deny/allow 端口列表。
- 支持 default_action 配置（allow/deny）。
- 直连路径在 DNS 解析后再次校验真实 IP。

## 配置管理

- 支持配置文件相对路径解析。
- 支持 YAML 配置未知字段校验。
- 支持环境变量注入管理 Token、用户密码和上游地址。
- 支持严格校验缺失或空值环境变量。
- 支持上游引用合法性校验。
- 支持 SOCKS5H 重定向上游引用校验。
- 支持上游名称重复校验。
- 支持上游权重范围校验。
- 支持路由规则 CIDR 合法性校验（含 IPv6）。
- 支持路由规则正则表达式合法性校验。
- 支持访问控制 CIDR 合法性校验。
- 支持 Go duration 字符串时长配置。
- 支持裸数字按秒解析的时长配置。
- 支持 DNS 配置校验（type/doh_url/dot_addr/ip_precedence）。
- 支持 TPROXY 配置校验（平台支持、模式有效性、TLS 不兼容）。
- 支持目标策略配置校验。

## 热加载

- 支持配置热加载。
- 支持监听地址热切换。
- 支持管理端口热切换。
- 支持管理认证 Token 热切换。
- 支持 pprof 开关热切换。
- 支持热加载上游、路由、日志、超时与访问控制配置。
- 支持热加载 TLS 配置。
- 支持热加载 DNS 配置。
- 支持热加载流量审计配置。
- 支持热加载目标策略配置。
- 支持热加载 TPROXY 配置。
- 支持手动触发配置重载。

## 管理接口

- 支持独立管理端口。
- 支持健康检查接口。
- 支持运行状态接口。
- 支持 Prometheus 指标接口。
- 支持管理接口 POST 触发配置重载。
- 支持流量统计查询接口。
- 支持运行时配置 API（增删查改 upstream/规则/用户）。
- 支持可选 pprof 调试接口。
- 支持管理接口 Bearer Token 认证。
- 支持公网管理端口强制认证校验。

## 访问控制

- 支持客户端 CIDR 白名单（含 IPv6）。
- 支持单 IP 连接速率限制。
- 支持单用户并发连接限制。
- 支持全局最大连接数限制。
- 支持 per-listener 最大连接数限制。

## 指标

- 支持隧道流量字节指标。
- 支持隧道读写次数指标。
- 支持连接数指标。
- 支持请求数指标。
- 支持路由决策指标。
- 支持上游拨号失败指标。
- 支持上游健康状态指标。
- 支持路由缓存命中指标。
- 支持路由缓存未命中指标。
- 支持管理接口请求指标。
- 支持流量审计指标（k5pw_traffic_bytes_total）。

## 日志

- 支持结构化 JSON 日志。
- 支持 console 日志格式。
- 支持 stdout、stderr、文件日志输出。
- 支持按大小滚动日志。
- 支持按日期滚动日志。
- 支持历史日志 gzip 压缩。
- 支持日志保留周期配置。
- 支持日志备份数量配置。
- 支持日志级别配置。

## 部署与运维

- 支持前台运行模式。
- 支持系统服务运行模式。
- 支持 systemd 服务安装与启停。
- 支持 systemd 安装脚本。
- 支持 Docker 多阶段构建。
- 支持 Docker Compose 运行。
- 支持非 root 容器用户运行。
- 支持多平台发布构建脚本。
- 支持版本号、commit、构建时间输出。
- 支持服务优雅停止并等待活跃连接结束。
- 提供 TPROXY iptables/nftables 部署示例脚本。
