# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/) 与 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。
版本规则与发布流程见 [VERSIONING.md](VERSIONING.md)。

## [Unreleased]

## [0.8.0-beta2] - 2026-11-11

### Added

- 传输层国密适配（TLCP，GB/T 38636-2020）。listener 新增 `tls.protocol`：
  `auto`（默认，TLCP 优先并回落标准 TLS）、`gm`（强制国密）、`tls`（与旧行为一致）。
- 新增 `tls.gm_sign_cert_file` / `gm_sign_key_file` / `gm_enc_cert_file` / `gm_enc_key_file`
  与对应 `*_key_password`（PBES2-SM4 加密 PKCS#8），以及 `tls.cipher_suites`。
  标准 TLS 与 TLCP 密钥材料不能复用：TLCP 要求 SM2 双证书，标准库不支持 SM2 曲线，
  因此 `cert_file` 始终表示标准 TLS 证书。
- https 上游新增 `tls_protocol` / `tls_ca_file` / `tls_server_name` / `tls_insecure_skip_verify`。
  `auto` 会记忆协商结果并带冷却重试，避免对不支持国密的上游每条连接都多一次失败握手；
  曾经成功过的国密上游不会因单次失败被永久降级。
- `https`/`tls` 健康检查改为跟随上游协议，修复强制国密的上游被探测误判为不可用。
- 新增 `yigate gencert` 子命令：生成 SM2 根 CA、TLCP 签名证书与加密证书，
  纯 Go 实现，不依赖 openssl。
- `gencert` 新增 `-ca` / `-ca-key` / `-ca-key-password`：复用既有 SM2 CA 签发叶子证书，
  证书轮换时不必重建信任链；不覆盖原 `ca.crt`/`ca.key`，签发前校验证书与私钥配对。
- `gencert` 新增 `-days`（默认 365 天）并输出到期日期：叶子证书有效期超过签发 CA 的剩余
  有效期时自动收敛，已过期的 CA 直接拒绝签发。
- 新增 `scripts/bench-tls.sh` 与 `proxy` 包基准（完整握手、SM4 数据面吞吐），
  README 记录参考数字与解读口径；`auto` 端口另有对照基准。
- 启动与配置热加载后扫描配置中引用的证书：剩余不足 30 天输出 `certificate expires soon` 警告，
  已过期输出 `certificate has expired` 错误，附带文件、类型、CN 与剩余天数；同一文件只报一次。
- `gencert -check <file|dir>` 只读体检：打印证书 CN、有效期与剩余天数，过期或临期时退出码为 1，
  便于在轮换脚本与发布流水线里拦截。
- `GET /api/v1/status` 新增 `certificates` 字段（文件、类型、CN、到期时间、剩余天数、是否过期），
  WebUI 顶部据此显示到期横幅；`GET /api/v1/config/certificates` 列出配置目录内的证书文件。
- WebUI 上游表单的 `tls_ca_file` 改为可输入可选择的自动补全，候选来自配置目录内的证书文件。
- 新增 `docs/gm-compliance.md`：按密评口径逐项列出传输层国密的实现现状、配置项、现场取证命令
  与已知边界（含 `gm` 与 `auto` 的强制/降级差异）。
- WebUI 上游编辑弹窗新增「传输协议」（auto/gm/tls）、「CA 证书」「校验主机名」字段，
  与配置文件字段一一对应；未填写的字段保存后保持原值。
- 令牌比较摘要由 SHA-256 改为 SM3（合规口径统一，不改变安全强度）。

### Fixed

- 修复 `auto` 端口启用 mTLS 时 `client_ca_file` 只含 SM2 CA 会导致端口启动失败的问题：
  标准 TLS 侧改为逐块解析 PEM，忽略无法解析的 SM2 证书，并要求至少存在一张可用证书。
- 补充国密 mTLS 覆盖：无客户端证书、客户端证书由未知 CA 签发均握手失败，
  正确 CA 签发的 SM2 双证书可完成握手；`auto` 端口混合 CA 下标准 TLS 与 TLCP 两类 mTLS 客户端可同时工作。
- 修复管理 API 校验证书路径时以进程工作目录为基准的问题：改为按配置文件目录解析，
  避免 WebUI 以相对路径新增/编辑含证书的上游时误报文件不存在；落盘仍保留相对路径。
- WebUI E2E 补充国密表单用例：新增/编辑上游时 `tls_protocol`、`tls_ca_file`、`tls_server_name`
  正确落库并在局部保存后保留；非 https 上游强制 `gm` 时前端拦截。
- 补充出站 `auto` 的并发与健康检查覆盖：国密与标准 TLS 上游并存时协商结果按实例隔离，
  标准 TLS 上游的降级不会带偏国密上游；健康检查探测复用数据面的协商器，国密上游不会被误判不可用。

### Notes

- 管理端口不提供国密 TLS；HTTP Basic / SOCKS5 认证与 CONNECT 隧道内加密不在本程序控制范围内。
- 未内置密码机 / KMS，密评场景仍须补齐密码模块资质与密钥管理。

### Changed

- 发布通道新增预发布版：`vX.Y.Z-<prerelease>`（如 `v0.8.0-beta1`）走 `v*` 流水线，
  产物与正式版同规格，但 Release 标记 `preRelease`、镜像只推版本 tag 不移动 `:latest`。
  详见 `VERSIONING.md`。

## [0.7.0] - 2026-11-11

本次为代理稳健性与界面易用性版本。修掉 9 处热路径故障（fd 耗尽忙循环、UDP 关联被握手超时误杀、级联 BIND 挂起与重复响应、半关闭截断、告警风暴等），`/health` 改为匿名探活，流量指标去掉高基数标签；WebUI 上游编辑弹窗改为「基本信息 / 健康检查」双 Tab。

### Added

- 上游健康检查支持 `health_check.target`（`host:port`）。填写后进入"深度探测"：
  http/https 真实发起一次 `CONNECT`（配合 `path` 时改用 absolute-URI `GET`），
  socks5/socks5h 真实发起一次 `CONNECT`，按 `expected_codes`（默认 2xx）判定，
  用于验证上游真的能把流量带出去。留空则保持"存活探测"，不依赖外网。
- 上游状态接口新增 `passive_failures`，用于区分业务拨号失败与主动探测失败。

### Fixed

- 修复 HTTP 健康检查的非法探测形态：原实现发 `GET / HTTP/1.1` 且把上游自身
  地址当 `Host`，既不是合法的正向代理请求，又可能让上游访问自身形成自环；
  在禁止私网目标的部署下会稳定收到 `403`，导致节点被永久判死。
  现改为对上不可达的本机端口发起 `CONNECT`，只验证协议层是否正常应答。
- 业务拨号失败不再与主动探测共用同一计数器：改为独立的被动熔断（阈值 10），
  并且只统计连接/TLS/认证/握手阶段的"节点级硬失败"。目标站点不可达、
  SOCKS5 返回 `0x01`（上游自身链路失败）、`503`/`429` 负载保护都不再摘除节点，
  避免把链路抖动放大成整节点停用。
- 被摘除的节点在没有任何存活上游时改走 half-open 兜底，不再直接返回
  "无可用上游"；兜底拨号成功会立即恢复节点，`check_interval: 0`（关闭主动
  健康检查）也不会再出现"一次误判即永久停用"。
- 修复上游恢复后 `dead_since` 未清零的问题。
- `socks5h_redirect.enabled: true` 但配置里没有可用 SOCKS5 上游时，按"未开启"
  处理：请求直接走普通路由，不再每个请求都白试一次并打出
  `no available socks5 upstream` 告警。启动/热加载时会记录一条 info 说明该状态。
- 监听 `Accept` 失败改为指数退避（5ms 起步、封顶 1s，成功后重置）。fd 耗尽
  （EMFILE/ENFILE）时 `Accept` 会立即持续报错，原先的不退避重试会形成忙循环，
  把 CPU 打满并刷爆日志，进程在故障期失去自愈能力。
- 修复 UDP ASSOCIATE 被握手超时误杀：控制连接在进入中继前清除握手 deadline，
  不再把"握手超时"当成"客户端离开"；中继停止时立即关闭 UDP 监听 socket，
  退出不再等满 30s 读超时。
- UDP 目标会话数加上限（单关联 128，满了淘汰最久未使用会话）。原先每个目标
  独占一个 socket 与 goroutine 且只有 90s 空闲回收，多目标 UDP 流量可在回收前
  耗尽 fd；会话活跃时间刷新也从遍历全表的 O(n) 改为按 key 的 O(1)。
- 级联 BIND 等待上游第二次回复增加 30s 超时（与本地 BIND 等待 Accept 对齐），
  上游不推送时不再让 goroutine 与客户端连接永久挂起。
- 级联 BIND / UDP ASSOCIATE 在已经回复过客户端后不再回退本地实现，避免同一
  连接收到两次响应、拿到两个不同监听地址；级联 UDP 的控制连接改为 defer 关闭，
  成功路径不再泄漏。
- 修复半关闭截断：TLS 等不支持 `CloseRead` 的连接不再退化为整体 `Close`，
  对端读到 EOF 后才回写的响应现在能完整送达。
- 周期恢复探测与手动全量巡检共用同一并发上限（8），上游同时到期时一轮探测
  不再瞬间打出大量连接与业务流量争抢 fd。

### Changed

- 构建链与 CI 统一升级到 Go 1.27.4，修掉标准库漏洞 GO-2026-6859
  （`net/http.MaxBytesReader` 头部竞态）。镜像、`go.mod`、发布流水线同步调整。
- CI 编排改为并行：交叉编译与打包并行化，单测与构建同时跑；镜像构建直接复用
  已产出的二进制（不再在镜像里重跑 `npm ci` 与 `go build`），Go 构建缓存、
  lint 工具与 npm 缓存跨构建复用。
  每日构建从约 11 分钟降到 1 分钟出头：只出 6 个主流平台（全平台仍随正式版发布），
  lint / race / 漏洞扫描改由 PR 门禁负责，正式发布保留全部门禁、并行执行。
  浏览器 E2E（Playwright）移出 CI，改由本地 `make web-e2e` 执行。
- `yigate_traffic_bytes_total` 指标去掉高基数 `target` 标签，只保留
  `direction/user/upstream`；目标级明细仍由流量 history 与审计接口提供。
  原先 `target` 随访问目标数增长，打满注册表上限后新组合被静默丢弃。
- Accept 失败、UDP 中继/会话读写失败、流量审计记录失败等热路径告警改为按
  固定类别限频（每秒一条），被抑制条数以 `suppressed=N` 附在下一条日志里，
  故障风暴下不再刷爆日志。

- `GET /health` 改为匿名探活：不再要求 `management_auth_token`，只返回纯文本 `UP`，
  且不再回显监听地址 / 上游数量 / 就绪状态，router 未就绪时也不再返回 `503`。
  进程存活与代理可用性是两件事，就绪判定请使用需认证的 `GET /api/v1/status`；
  依赖原 JSON 响应或 `503` 语义的外部脚本需要调整。
- 健康检查收进镜像 `HEALTHCHECK`（匿名探测 `/health`），`docker-compose.yml` 与
  `deploy/docker/compose.*.yml` 不再重复定义，也不再从 `config.yml` 解析令牌。
- WebUI 登录校验与 Dashboard 改用 `/api/v1/status`，不再依赖 `/health`。
- 健康检查失败后按 5s 快速重试（不超过检查间隔），不必等满一个周期才凑够阈值；
  恢复探测退避加入 ±20% 抖动，避免多节点同时探测。
- 监听连接数达到上限时不再静默关闭连接：SOCKS5 返回 `05 00` 后回通用失败，
  HTTP 返回 `503` + `Retry-After`，让对端（尤其是健康探测）能区分"节点繁忙"
  与"节点故障"；过载拒绝路径自身用信号量限流，超出时仍直接关闭。
- BIND / UDP ASSOCIATE 的单次失败不再立即摘除整个节点，只降级对应能力。
- WebUI 上游编辑弹窗改为「基本信息 / 健康检查」双 Tab，弹窗宽度 720px → 800px；
  「启用」与「类型」合并到同一行，窄屏（`xs`）自动堆叠。提交校验失败时自动切到
  出错的那个 Tab，提示不会再落在看不见的分组里。

## [0.6.0] - 2026-11-10

本次为上游凭据与用户治理版本。上游支持独立用户名密码（密码可引用 `${ENV}`），用户可以启用/禁用，删除或禁用最后一个启用用户时明确回落为匿名访问；AI 助手文档路径统一为 `llms.txt`，WebUI E2E 全面改用 `data-testid` 定位。

### Added

- 上游支持独立 `username` / `password` 认证字段，不必再把凭据拼进 `url`；`password`
  支持 `${ENV}` 占位符避免明文落盘。列表接口只返回 `username` 与 `has_password`，
  密码永不下发，编辑其他字段也不会抹掉已配置的密码。两个字段必须成对出现，
  且不能与 URL 内嵌 userinfo 同时配置（同时出现会在加载期报错）。
- 用户启用/禁用：`users` 支持 `{password, enabled}` 映射写法，旧的 `username: password`
  标量写法继续兼容；禁用用户不参与代理认证，全部禁用等价于匿名代理。
- `PUT /api/v1/config/users/{username}` 支持只传 `enabled` 切换用户状态，
  `GET /api/v1/config/users` 返回每个用户的 `enabled`。

### Changed

- 删除或禁用最后一个启用用户时，接口返回 `409`（`code=last_active_user`）而不是直接报错；
  WebUI 会提示「配置将回落为匿名访问」，用户确认后带 `?force=true` 重试，
  服务端写入 `allow_anonymous_proxy: true` 让配置自洽生效。批量删除同此语义。
- 配置保存改为在校验副本上执行，单监听配置不会再被改写成 `listeners` 数组形态。
- 帮助菜单的 `llmx.txt` 更名为 `llms.txt`（AI 助手约定的标准路径），页面路由同步改为
  `/docs/llms`，原始地址为 `/llms.txt`；旧路径不再保留别名。

- WebUI E2E 改为 `data-testid` 定位：页面根节点 `page-<route>`、侧栏 `nav-<route>`、
  表格行内操作 `row-delete` / `row-edit` / `row-move-up` / `row-move-down`，登录、分页、
  空状态、重试按钮等也都有稳定钩子；文案断言统一引用 `web/e2e/support.ts` 导出的词条，
  五个 spec 的重复登录逻辑收敛为 `login()`。
- `npm run check` 新增门禁：`web/e2e` 里出现中文字面量（注释与标注 `copy-ok` 的后端正文除外）
  直接失败，避免界面文案再次变成选择器。

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
