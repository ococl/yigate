# k5pw 产品化修复计划

**版本**：1.0  
**日期**：2026-07-09  
**范围**：基于 `k5pw` 产品化审计报告，覆盖安全、可靠性、可维护性、测试、部署运维、文档与产品化六个维度。  
**目标**：在对外发布或大规模内部推广前，将项目从产品化成熟度 6.5/10 提升至 8.5/10 以上。

---

## 1. 修复原则

1. **安全第一**：所有可能泄露凭据、扩大攻击面或导致数据泄露的问题优先处理。
2. **可验证**：每个修复项必须对应可运行的测试、检查脚本或验收标准。
3. **向后兼容**：配置格式与核心行为尽量保持兼容，破坏性变更纳入 2.0 规划。
4. **小步快跑**：按 P0 → P1 → P2 分阶段落地，每阶段结束后发布一个 minor 版本。
5. **文档先行**：变更在实现前先在 README / AGENTS / CHANGELOG 中说明。

---

## 2. 问题清单与修复计划

### 2.1 P0：必须解决（对外发布前必须完成）

| 编号 | 问题 | 影响 | 修复方案 | 验收标准 | 责任人 | 参考文件 |
|---|---|---|---|---|---|---|
| P0-01 | 测试覆盖率极低：main 8.3%，proxy 31.0% | 回归风险高，重构困难 | 为核心模块补充单元测试与集成测试；使用 mock 网络连接测试错误分支 | `go test -cover ./...` 整体 ≥ 60%，proxy 包 ≥ 70% | TBD | `proxy/*_test.go` |
| P0-02 | 缺少进程自身健康检查端点 | 容器编排无法做存活/就绪探针 | 新增管理接口（可复用 `listen` 或独立管理端口），提供 `/health` 返回 JSON 状态 | 通过 `curl http://127.0.0.1:管理端口/health` 返回 200 与组件状态 | TBD | `server.go` |
| P0-03 | 缺少监控指标（Metrics） | 无法监控延迟、流量、上游状态 | 接入 Prometheus 格式 metrics：连接数、请求数、路由命中率、上游存活状态、延迟分位 | 提供 `/metrics` 端点，至少暴露 10 个核心指标 | TBD | `proxy/metrics.go` 新增 |
| P0-04 | 上游凭据示例仍以明文 URL 展示 | 用户易误提交密码，泄露风险 | 所有示例（README、AGENTS、config.sample.yml）全部使用 `${ENV}` 形式；代码中保持 `os.ExpandEnv` 并支持 `K5PW_UPSTREAM_*` 前缀环境变量注入 | 无 `user:pass@` 形式的示例；文档提供环境变量注入指引 | TBD | `README.md`、`config.sample.yml` |
| P0-05 | Docker 镜像使用 `alpine:latest` 且以 root 运行 | 基础镜像漂移、运行权限过大 | 固定为 `alpine:3.20`（或当时最新 LTS 小版本）；添加非 root 用户 `k5pw`；限制容器权限 | Dockerfile 构建通过，容器内进程非 root | TBD | `Dockerfile` |
| P0-06 | 配置校验不完整：未校验上游名引用、listen 格式 | 配置错误导致运行时异常 | 在 `validateConfig` 中校验：① `listen` 可被 `net.Listen` 解析；② 规则 `upstreams` 引用必须存在于全局 `upstreams` | 非法配置启动时返回明确错误并退出 | TBD | `config.go` |
| P0-07 | 全局 logger 在热加载时被替换，存在潜在并发问题 | 日志可能丢失或引用已关闭 writer | 将 logger 改为不可变引用 + writer 池模式；热加载时只切换 writer 引用，不替换 logger 指针 | 并发测试无 panic，热加载 100 次无日志丢失 | TBD | `proxy/logging.go` |
| P0-08 | 未提供 systemd 服务模板与安装脚本 | 物理机/虚拟机部署不标准 | 提供 `dist/k5pw.service` 模板与 `install.sh` 一键安装脚本 | 在 systemd 系统上执行安装脚本后 `systemctl start k5pw` 成功 | TBD | `dist/k5pw.service`、`dist/install.sh` |

### 2.2 P1：强烈建议（建议 P0 完成后一个月内完成）

| 编号 | 问题 | 影响 | 修复方案 | 验收标准 | 责任人 | 参考文件 |
|---|---|---|---|---|---|---|
| P1-01 | 配置热加载非原子，可能处于半生效状态 | 新旧配置混用导致行为异常 | 构建新的 `runtimeContext`（含 manager、router、logger、timeouts），原子替换后再停旧组件 | 热加载测试：旧连接完成前不关闭，新连接使用新配置 | TBD | `server.go` |
| P1-02 | SOCKS5/HTTP 错误码不精确 | 用户无法判断失败原因 | 将内部错误分类为 DNS、拒绝、超时、协议错误，映射到 SOCKS5 标准回复码和 HTTP 精确状态码 | 错误场景单元测试覆盖主要返回码 | TBD | `proxy/socks.go`、`proxy/http.go` |
| P1-03 | 缺少访问控制：无 IP 白名单、速率限制、连接数限制 | 易被滥用或遭受暴力破解 | 新增配置：`allowed_clients`（CIDR 列表）、`rate_limit`、单用户并发连接数 | 不在白名单的客户端被拒绝；超限请求触发 429/503 | TBD | `config.go`、`proxy/server_access.go` 新增 |
| P1-04 | 缺少版本与构建信息注入 | 生产环境无法定位二进制版本 | 在 `main.go` 中声明 `version`、`commit`、`buildTime` 变量，通过 `go build -ldflags` 注入 | `k5pw -version` 输出版本、commit、构建时间 | TBD | `main.go` |
| P1-05 | 部分核心函数过长（`upstream.go` 近 1000 行，`http.go` 近 500 行） | 可读性差、测试难 | 按职责拆分为 `upstream_selector.go`、`upstream_health.go`、`upstream_dial.go`、`http_connect.go` 等 | 每个 Go 文件控制在 400 行以内，且 `go test` 通过 | TBD | `proxy/upstream.go`、`proxy/http.go` |
| P1-06 | 缺少 pprof 调试能力 | 线上问题定位困难 | 在管理端口可选启用 `/debug/pprof`（默认关闭，配置开启） | 开启后可通过 `go tool pprof` 采集 CPU/内存 | TBD | `server.go` 管理端口 |
| P1-07 | `dailyRotateWriter` 压缩失败无告警 | 日志压缩失败时静默丢弃 | 压缩失败时写入日志并返回错误；增加 `Sync()` 正确性测试 | 压缩失败场景可观测，单测覆盖 | TBD | `proxy/logging.go` |
| P1-08 | 缺少统一错误类型与错误码 | 监控与排障依赖字符串匹配 | 定义 `proxy/errors.go` 错误类型，带 `Code` 字段；错误码用于日志与 metrics | 主要错误路径可通过类型断言判断 | TBD | `proxy/errors.go` 新增 |
| P1-09 | 缺少连接与流量统计 | 无法做容量规划 | 在 `Tunnel` 中接入流量计数器，暴露 bytes/packets 指标 | metrics 中可见 `k5pw_transferred_bytes_total` | TBD | `proxy/upstream.go`、`proxy/metrics.go` |

### 2.3 P2：产品化增强（建议三个月内完成）

| 编号 | 问题 | 影响 | 修复方案 | 验收标准 | 责任人 | 参考文件 |
|---|---|---|---|---|---|---|
| P2-01 | 缺少 CHANGELOG 与版本发布管理 | 用户无法追踪变更 | 新增 `CHANGELOG.md`，使用 Git tags 与 GitHub Releases 发布预编译二进制 | 每次合并修复计划后更新 CHANGELOG，tag 命名规范 | TBD | `CHANGELOG.md` |
| P2-02 | 缺少性能与压力测试 | 无法确认容量上限 | 增加 `go test -bench` 与 `pyscripts/benchmark.py` 压测脚本（并发连接、吞吐、延迟） | 发布前跑通基准测试并给出推荐配置 | TBD | `pyscripts/benchmark.py` 新增 |
| P2-03 | 配置 schema 描述不完整 | 新用户上手成本高 | 提供 JSON Schema 或更详细的字段说明；配置加载失败时给出字段级错误提示 | README 链接 schema，非法配置错误信息精确到字段 | TBD | `docs/config_schema.md` 新增 |
| P2-04 | 文档缺少故障排查手册 | 运维效率低 | 编写 `docs/troubleshooting.md`，覆盖：直连/代理判断、日志解读、上游 dead 排查、抓包建议 | 手册覆盖 5 个以上常见场景 | TBD | `docs/troubleshooting.md` 新增 |
| P2-05 | README 与 AGENTS 部分信息不一致 | 文档可信度下降 | 统一两处文档，明确热加载行为、端口变更影响、Docker 使用限制 | 文档无矛盾，关键限制用统一措辞 | TBD | `README.md`、`AGENTS.md` |
| P2-06 | 缺少多平台预编译二进制 | 安装门槛高 | 通过 GitHub Actions / 本地脚本构建 Linux/Windows/macOS 多架构二进制 | release 页面提供多平台下载 | TBD | `.github/workflows/release.yml` 或 `scripts/build.sh` |
| P2-07 | 缺少 API/CLI 管理接口 | 运行时无法查看状态 | 管理端口提供 JSON API：查看配置摘要、上游状态、缓存命中率、手动触发热加载 | 提供 `GET /api/v1/status` 与 `POST /api/v1/reload` | TBD | 管理端口模块 |

---

## 3. 阶段规划与里程碑

### 阶段一：安全与可观测性（P0，预计 2-3 周）

- [x] P0-04 上游凭据环境变量化与文档更新
- [x] P0-05 Docker 安全加固
- [x] P0-06 配置校验增强
- [x] P0-02 进程健康检查端点
- [x] P0-03 Prometheus 监控指标
- [x] P0-08 systemd 服务模板与安装脚本
- [x] P0-07 全局 logger 并发安全改进

**里程碑**：`v1.1.0` — 达到基础生产部署标准。

### 阶段二：健壮性与工程化（P1，预计 3-4 周）

- [x] P0-01 测试覆盖率提升至 60%+（当前整体 60.0%，proxy 包 71.1%）
- [x] P1-01 配置原子热加载
- [x] P1-02 精确错误码与日志
- [x] P1-03 访问控制
- [x] P1-04 版本与构建信息注入
- [x] P1-05 核心文件拆分
- [x] P1-06 可选 pprof 端点
- [x] P1-07 日志压缩失败告警
- [x] P1-08 统一错误类型
- [x] P1-09 流量统计指标

**里程碑**：`v1.2.0` — 具备企业内网推广条件。

### 阶段三：产品化与生态（P2，预计 4-6 周）

- [x] P2-01 CHANGELOG 与 release 流程
- [x] P2-02 性能基准测试
- [x] P2-03 配置 schema 文档
- [x] P2-04 故障排查手册
- [x] P2-05 文档一致性梳理
- [x] P2-06 多平台二进制发布
- [x] P2-07 管理 API

**里程碑**：`v1.3.0` — 对外开源/发布候选版本。

---

## 4. 验收标准汇总

1. **安全**：无 `user:pass@` 形式示例；Docker 非 root 运行；配置校验不通过则拒绝启动。
2. **测试**：`go test ./...` 通过，整体覆盖率 ≥ 60%，proxy 包 ≥ 70%。
3. **可观测**：提供 `/health` 和 `/metrics` 端点，metrics 不少于 10 个核心指标。
4. **部署**：提供 systemd 模板与安装脚本，Docker 构建可一键启动。
5. **文档**：README / AGENTS 无矛盾，新增 CHANGELOG、troubleshooting 手册、config schema。
6. **版本**：`k5pw -version` 可输出版本、commit、构建时间。

---

## 5. 风险与依赖

| 风险 | 影响 | 缓解措施 |
|---|---|---|
| 重构 `upstream.go` 与 `http.go` 可能引入回归 | 高 | 先补充测试再重构；分小步提交；每次重构后跑 `go test` 与 `test_proxy_4479.py` |
| 引入管理端口可能与代理端口冲突 | 中 | 默认管理端口不启用，或绑定 `127.0.0.1`；配置项明确隔离 |
| 环境变量注入与 URI 编码兼容性 | 低 | 对 URL 中特殊字符做编码校验与明确文档说明 |
| 第三方依赖（Prometheus client）增加体积 | 低 | 仅在需要时引入，评估是否使用最小实现或可选编译标签 |
| 热加载原子化改动较大 | 中 | 先出设计文档，再实现；保留旧实现作为 fallback |

---

## 6. 备注

- 本计划与 `docs/implementation_plan.md` 的关系：
  - `implementation_plan.md` 记录的是**已完成/本轮迭代**的功能实现计划（如 SOCKS5 BIND/UDP、协议级健康检查、环境变量凭据、热加载监听切换）。
  - 本计划记录的是**审计后发现**的、仍需修复的产品化问题。
- 每项任务建议拆分为独立 Git issue / PR，并在本文件中更新状态。
- 建议在每次阶段里程碑后重新运行产品化审计，验证成熟度提升。
