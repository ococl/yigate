# 版本与发布规范

YiGate 使用 **CNB CI**（`.cnb.yml`）驱动的自动化发布。二进制跨平台归档与 Docker 镜像同时发布，互为补充。

## 版本号方案

版本号由 `scripts/version.sh` 根据 CI 上下文自动推导：

| 构建来源 | 版本号格式 | 说明 |
|----------|------------|------|
| `vX.Y.Z` 标签推送 | `X.Y.Z` | 正式版（去除 `v` 前缀，须为 `x.y.z`） |
| `main` 分支推送 | `0.0.0-beta.<short-sha>` | Beta 构建（校验 + 归档 + `:main` 镜像） |
| 其他（本地 / 未匹配） | `0.0.0-dev.<short-sha>` | 开发版 |

- `X.Y.Z` 为语义化版本；`short-sha` 为 7 位 commit 短哈希。
- 本地构建（非 CI）回退到 `git rev-parse --short HEAD`。

## 分支与产物映射

| 触发 | 流水线 | 产物 |
|------|--------|------|
| `main` 分支 push | beta | 全平台二进制归档 + SBOM + SHA256SUMS + Docker 镜像 `:main`（覆盖式） |
| `v*` 标签 push | formal-release | 全平台二进制归档 + SBOM + SHA256SUMS + Docker 镜像 `:latest` 与 `:X.Y.Z` |
| PR to `main`/`dev` | pr-check | lint + test + race + 前端校验（不发布） |

> 注：`main` 分支每次提交均触发 Beta 构建，推送覆盖式 `:main` Docker 镜像。正式镜像 `:latest` / `:X.Y.Z` 统一由 `v*` 标签流水线发布。

> 正式版产物按版本号归档并挂载到对应 Release；日常 Beta 构件挂在 commit 上，不创建 Release。

## Docker 镜像标签

- `main` 分支：`registry/...:main`（Beta 构建，覆盖式）
- `v*` 标签：`registry/...:latest` 与 `registry/...:X.Y.Z`

镜像基于 `golang:1.27.3` 多阶段构建，运行阶段为 `alpine:3.24.4`，使用非 root 的 `yigate` 用户，由 `entrypoint.sh` 在 root 启动时降权，工作目录 `/var/lib/yigate`，挂载 `data` 与 `logs`。

## 发布流程

### 正式版（手动打标签触发）

1. 在 `main` 上完成功能与回归测试（`go test ./...`、`make web-e2e`）。
2. 更新 `CHANGELOG.md`（CI 另通过 `cnbcool/changelog` 生成 `LATEST_CHANGELOG.md` 作为 Release 说明）。
3. 打标签并推送：`git tag vX.Y.Z && git push origin vX.Y.Z`。
4. CI 执行发布门禁（版本格式 / 工作区干净 / tag 指向 HEAD）→ lint → golangci-lint → test → race → govulncheck → 前端类型检查/单测/审计 → 配置安全检查 → 构建（含 WebUI 嵌入和 SBOM）→ 浏览器 E2E → 产物校验 → 构建并推送镜像 → 创建 Release（`latest: true`）→ 上传附件。

正式流水线先创建 Release 再上传附件（`cnbcool/attachments` 依赖已存在的 Release）；附件上传失败会直接使流水线失败，不会产生空发布。

### Beta 构建（main 分支自动触发）

- `main` 每次提交都会构建全平台归档、SBOM 和 `:main` 镜像，不创建 Git Release。
- 需要对外发布的版本必须走 `v*` 标签流水线。

## 产物校验

- 每次构建生成 SBOM（`yigate-X.Y.Z.spdx.json`），正式发布必须包含。
- 每次发布生成 `SHA256SUMS`，覆盖全部归档和 SBOM；`scripts/check-release-files.sh` 会实际执行 `sha256sum -c`。
- `scripts/check-release-configs.sh` 同时检查 `deploy/config` 与 `deploy/docker` 模板，拒绝固定占位凭据和空的 LAN 认证配置。
- 跨平台目标清单见 `scripts/platforms.sh`（当前 26 个平台）。

## GitHub 镜像同步

CNB 是唯一发布通道，GitHub（`https://github.com/ococl/yigate`）只是镜像仓，**不运行任何 CI**：
`.github/workflows/release.yml` 已移除，`ci.yml` 只保留 `workflow_dispatch` 手动入口。

| 触发 | 同步内容 |
|------|----------|
| `main` 分支 push | 仅 `main` 分支代码（镜像滞后不阻塞本仓构建，允许失败） |
| `v*` 标签 push | `main` 代码 + 全部标签 + Release（含变更日志）+ 归档 / SBOM / `SHA256SUMS` 附件 |

同步由 `scripts/sync-github.sh` 完成（仅依赖 `git` 与 `curl`，直连 GitHub REST API）：

```bash
# 全量同步：代码 + 标签 + Release + 附件 + 仓库简介/topics
GITHUB_TOKEN=xxx bash scripts/sync-github.sh all
# 只同步某一类
GITHUB_TOKEN=xxx bash scripts/sync-github.sh code|release|meta
# 预演，不写远端
DRY_RUN=1 GITHUB_TOKEN=xxx bash scripts/sync-github.sh all
```

- 幂等：Release 已存在则复用并刷新说明；同名附件先删后传，重复执行不会产生重复项。
- 流水线中 `GITHUB_TOKEN` 通过仓库环境变量（或密钥仓库 `imports`）注入，不入库、不落日志。
- token 为 fine-grained PAT，需要 `Contents: Read and write`；同步仓库简介与 topics 另需
  `Administration: Read and write`（缺失时仅告警跳过，不影响代码与产物同步）。

## 回滚

- 正式版标签不可变，回滚请发布新的 `vX.Y.Z` 或重新指向历史提交。
- 历史二进制和 SBOM 可从对应 Release 附件获取。
