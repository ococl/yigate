# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/) 与 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。
版本规则与发布流程见 [VERSIONING.md](VERSIONING.md)。

## [0.3.0] - 2026-11-08

本次为安全与工程质量治理版本，行为变化集中在使用了目标访问白名单与运行时部署的场景。

### Security

- **第二轮强化 · 启动期基线**：非回环监听强制要求 `users` 或 `allowed_clients`、
  `enable_pprof` 一律要求管理令牌（即使只绑回环）、占位符与常见弱口令在配置加载
  和管理 API 添加用户时均被拒绝；新增 `allow_anonymous_proxy` / `allow_private_targets`
  两个显式风险确认开关，容器默认配置与本地匿名模板已显式声明。
- **第二轮强化 · SSRF**：内置云元数据服务域名别名拒绝；上游 URL 主机校验；
  GeoSite 下载重定向逐跳重新校验并在跨主机跳时剥离更新令牌；
  新增可选 `destination_policy.resolve_before_upstream: true`，在把域名交给上游解析前
  先由网关预解析并逐个校验 IP。
- **第二轮强化 · 管理接口**：请求体改用 `http.MaxBytesReader` 并统一返回 413、
  补 `ReadHeaderTimeout`、安全响应头覆盖全部管理响应且 API 响应 `no-store`、
  5xx 不再回显内部绝对路径、认证失败与配置写操作均留下带来源 IP 的审计日志。
- **第二轮强化 · 凭据回显**：`GET /api/v1/config/settings` 回显 GeoSite 更新地址时脱敏；
  发布配置检查补上此前漏检的 `local-only-token`。`govulncheck` 实测无已知漏洞。
- 目标访问策略（`destination_policy`）现在在普通 HTTP 请求进入任何转发分支前统一执行，
  覆盖 `final=proxy`、`auto` 的代理路径、`socks5h_redirect` 与 SOCKS5 UDP 级联，修复了策略可被绕过的问题。
- `default_action: deny` 下 `allow_ports` 改为附加约束，必须与 `allow_cidrs` / `allow_domains` 同时满足；
  仅配置 `allow_ports` 会在配置加载时报错，不再静默放行或拒绝全部流量。
- 补全受保护网段（CGNAT、IPv6 过渡地址、保留与文档地址），并识别非标准 IPv4 字面量，
  避免其被当作域名放行；已解析 IP 时域名白名单仍参与判断。
- 运行配置文件写回与回滚一律收紧为 0600；首次启动不再把管理令牌明文写入 stderr/journal。
- 管理接口与 GeoSite 同步接口的令牌比较改为定长摘要后常量时间比较，不再泄露令牌长度。
- GeoSite 数据路径限制在运行数据目录内、更新地址禁止回环与链路本地目标，
  错误信息与管理 API 回显统一脱敏；WebUI 静态资源补充安全响应头并明确匿名只读边界。
- TPROXY 与代理用户认证不再允许共存（透明代理无法执行认证），配置加载时直接报错。
- 拒绝 SOCKS5 域名中的控制字符；DNS 压缩指针增加环检测、事务 ID 改用加密安全随机数。
- 部署实例恢复安全默认值：代理端口只绑回环、启用认证与私网防护、凭据改由 0600 环境变量文件注入。

### Fixed

- 修复 `quickstart-linux.sh install` 生成 `User=ygate` 导致服务无法启动的问题，并补齐 systemd 加固项。
- 修复 DNS 解析器 `Stop()` 因复用同一个 `sync.Once` 而从不关闭清理 goroutine 的问题；
  热加载替换路由器时一并停止解析器。
- 修复多监听端口共用全局连接计数导致的配额挤占，改为按监听地址独立计算。
- 热加载创建监听/管理端口失败时回滚已启动的后台组件；日志退休 writer 不再无限累积；
  GeoSite 下载关闭空闲连接并响应请求取消。
- 配置回滚失败不再被静默丢弃；配额错误可被 `errors.Is(err, ErrQuotaExceeded)` 识别。
- 管理 `WriteTimeout` 调整为 120s，避免 pprof CPU profile 被提前切断。
- 修复热加载时密钥类环境变量缺失导致保存设置或 GeoSite 更新返回 500
  （如 `management_auth_token references unset or empty environment variable`）：
  热加载沿用当前生效值并记录告警，启动期仍保持严格校验。
- 修复 HTTP（非安全上下文）部署下整站复制按钮失效：统一使用 Clipboard API +
  `execCommand` 兼容回退。

### Changed

- GeoSite 数据路径改由系统托管（配置文件同目录的 `geosite.dat`），管理界面与
  API 不再接受用户指定。
- 下游同步令牌由系统随机生成：勾选「允许下游同步」时若令牌为空立即生成，
  保存时再兜底一次；令牌在 WebUI 中默认掩码显示、可直接复制，并支持
  `GET /api/v1/geosite/sync-token` 随时读取与一键重新生成；管理令牌在设置页也可直接生成。
- GeoSite 更新优先使用已配置且可用的上游代理，全部失败后回退直连，并新增
  `routing.geosite.use_upstream` 开关。
- GeoSite「更新来源」支持一键恢复系统默认值（官方更新地址、24 小时间隔、
  自动更新与上游代理开启），恢复后点击「保存」生效。
- `make build` 不再污染被跟踪的 `webui_dist/index.html`；该占位页被移除，仅保留
  `PLACEHOLDER.md`，所有构建产物被忽略。
- 开发代理默认端口统一为 8890；Go / Node 版本要求在文档、`go.mod` 与 `engines` 间对齐。
- CNB 与 GitHub Actions 复用同一套发布门禁、配置检查与平台清单一致性检查；
  PR 门禁补齐类型检查、源码规范检查与静态检查。
- 前端新增统一 API 错误契约、403 分支、事件驱动的登出（不再硬刷新）、统一格式化函数与源码规范检查。

## [0.2.0] - 2026-11-07

- 单端口 HTTP / HTTPS CONNECT / SOCKS5 / SOCKS5h / TPROXY 统一网关。
- 上游优先级与权重、健康检查、故障转移、GeoSite 分流与数据同步。
- 内置 WebUI（12 个视图）、管理 API、指标、流量审计与配额。
- 26 平台交叉编译、Docker / systemd / compose 部署、SBOM 与校验和。
