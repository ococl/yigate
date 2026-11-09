# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/) 与 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。
版本规则与发布流程见 [VERSIONING.md](VERSIONING.md)。

## [0.5.0] - 2026-11-09

本次为路由控制与界面本地化版本。规则新增优先型动作与停用开关，管理界面支持中英文切换。

### Added

- 规则级 `direct_first` / `proxy_first` 动作：命中后先按指定方向尝试，失败再回落到另一边，
  探测结果与 `auto` 共用缓存；规则级动作会覆盖全局 `routing.auto.mode`。
  `direct` / `proxy` 仍表示「只走一边、失败不回落」。
- 规则停用（`disabled: true`）：规则保留在配置中但不参与匹配，重新启用不必重填匹配值；
  加载期跳过禁用规则的上游引用校验（例如上游正在重建），启用时恢复完整校验。
- `POST /api/v1/config/rules/batch-toggle`：批量启用/禁用规则，一次校验、一次落盘、一次热加载，
  越界索引记入 `failed` 且不影响其它项。
- `PUT /api/v1/config/rules`：整体替换规则列表（WebUI 文本视图保存使用），
  一次校验、一次落盘、一次热加载。
- WebUI 中英文切换：接入 vue-i18n，公共词条、侧边栏、路由标题与全部页面文案抽到
  `web/src/locales`，中英两份保持同一 key 结构；语言记录在浏览器本地，未选择时固定简体中文，
  切换后同步 `<html lang>`。
- 前端新增词条用例：扫描源码里的 `t()` 字面量，引用不存在的 key 在单测阶段直接失败。

### Changed

- WebUI 列定义改为 `computed`，切换语言后表头、章节标题与校验提示即时刷新；
  `formatUptime`、`ApiError` 兜底文案与 `CodeBlock` / `LoadError` 的复制、重试文案统一走 `t()`。
  API 文档页的接口正文与示例仍是中文，与界面框架解耦，留待后续单独处理。

### Fixed

- 按钮不再在两个汉字之间自动插入空格：文案改由词条渲染后，是否插空格取决于词条节点
  相邻的换行，同一个按钮的可访问名称会随写法变化（「登 录」 vs 「登录」）。统一关闭
  antdv 的 `autoInsertSpace`，按钮显示的内容与词条完全一致，WebUI E2E 的全量用例恢复通过。

## [0.4.0] - 2026-11-09

本次为 WebUI 体验与批量运维版本。管理 API 有两处行为变化：上游健康检查的错误语义更精确，
GeoSite 路径改由系统托管；其余为新增能力与修复。

### Added

- WebUI 批量删除：上游、规则、用户列表支持多选后一次性删除，新增
  `POST /api/v1/config/{upstreams,rules,users}/batch-delete`，
  一批改动只写一次配置文件、只热加载一次，逐项返回 `deleted` 与 `failed`；
  失败项在界面上保留选中，可直接重试。
- WebUI 快捷键：设置页 `Ctrl/Cmd+S` 保存，流量页与 API 文档页 `/` 聚焦搜索框。
- 帮助菜单新增 `llmx.txt`：`/llmx.txt` 匿名可读，内容覆盖认证、先读后写、端点速查、
  常见任务与安全边界，供 AI 助手直接读取；页面展示同一份文件，不另存副本。
- WebUI 移动端完整适配：列表卡片化、本地分页、表单与操作条贴合窄屏。
- 未知路由展示 404 页面，路由标题随页面切换。
- 表单字段内联校验并自动定位出错章节；表单标签与控件建立 `for/id` 关联，
  图标按钮与开关补齐可访问名称。
- GeoSite 同步令牌改由系统生成，更新时优先使用已配置的上游代理。

### Changed

- `POST /api/v1/config/upstreams/{name}/check` 在目标不存在或已禁用时分别返回
  404 `upstream_not_found` 与 409 `upstream_disabled`，不再统一报 400。
- GeoSite 数据路径由系统托管，不再允许在界面或配置中指定；热加载时密钥类环境变量缺失
  不再导致保存设置失败。
- 带哈希的静态资源启用长缓存，WebUI 体积预算与缓存头纳入 CI 校验。
- API 文档示例修正：`strategy` 改为后端真实枚举 `weighted_round_robin`，
  `health_check_type` 改为实际存在的 `health_check` 对象；新增单测把示例取值钉在后端枚举上。

### Fixed

- 配置写回前对临时文件 `fsync`，避免掉电后留下已改名但内容为空的配置。
- 上游检查去重限流，静态资源解压结果缓存，新增流量丢弃指标。
- WebUI：空列表不再撑满视口、暗色选中项与状态色对比度不足、窄屏统计卡换行、
  分页越界回落、轮询失败刷屏、删除防重复提交、仪表盘误报「异常」、
  设置页加载失败不再展示空表单、规则/用户/流量补齐空态与重试入口。

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
