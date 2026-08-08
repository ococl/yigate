# 版本与发布规范

YiGate 使用 **CNB CI**（`.cnb.yml`）驱动的自动化发布。二进制跨平台归档与 Docker 镜像同时发布，互为补充。

## 版本号方案

版本号由 `scripts/version.sh` 根据 CI 上下文自动推导：

| 构建来源 | 版本号格式 | 说明 |
|----------|------------|------|
| `vX.Y.Z` 标签推送 | `X.Y.Z` | 正式版（去除 `v` 前缀，须为 `x.y.z`） |
| `dev` 分支推送 | `0.0.0-dev.<short-sha>` | 早鸟预览版（pre-release） |
| `main` 分支推送 | `0.0.0-beta.<short-sha>` | Beta 构建（仅校验 + 归档，不发布滚动 tag / 镜像） |
| 其他（本地 / 未匹配） | `0.0.0-dev.<short-sha>` | 开发版 |

- `X.Y.Z` 为语义化版本；`short-sha` 为 7 位 commit 短哈希。
- 本地构建（非 CI）回退到 `git rev-parse --short HEAD`。

## 分支与产物映射

| 触发 | 流水线 | 滚动标签 | 产物 |
|------|--------|----------|------|
| `dev` 分支 push | early-bird-preview | `dev-latest`（pre-release） | 全平台二进制归档 + SHA256SUMS |
| `main` 分支 push | beta | —（不发布滚动 tag / 镜像） | 全平台二进制归档 + SHA256SUMS（附件桶标签 `main`） |
| `v*` 标签 push | formal-release | 正式 Release（`latest`） | 全平台二进制归档 + SHA256SUMS + Docker 镜像 `:latest` 与 `:X.Y.Z` |
| PR to `main`/`dev` | pr-check | — | lint + test（质量守护，不发布） |

> 注：main 分支不再自动打 `beta-latest` 滚动 git 标签、不再推送 `:beta-latest` Docker 镜像。滚动预览标签仅保留 `dev-latest`；正式镜像统一由 `v*` 标签流水线发布 `:latest` / `:X.Y.Z`。

> 二进制产物每次发布前通过 `cnbcool/attachments` 的 `type: DELETE` 清空旧附件，避免累积。

## Docker 镜像标签

- `dev` 分支：`registry/...:dev-latest`
- `v*` 标签：`registry/...:latest` 与 `registry/...:X.Y.Z`

> main 分支不再推送 Docker 镜像（无 `:beta-latest`）。

镜像基于 `golang:1.21` 多阶段构建，运行阶段使用非 root 的 `yigate` 用户，由 `entrypoint.sh` 在 root 启动时降权，工作目录 `/var/lib/yigate`，挂载 `data` 与 `logs`。

## 发布流程

### 正式版（手动打标签触发）

1. 在 `main` 上完成功能与回归测试（`go test ./...`、`web` 构建）。
2. 更新 `CHANGELOG`（CI 通过 `cnbcool/changelog` 自动生成 `LATEST_CHANGELOG.md`）。
3. 打标签并推送：`git tag vX.Y.Z && git push origin vX.Y.Z`。
4. CI 执行 lint → test → 构建（含 WebUI 嵌入）→ `check-release-files.sh` 校验 → 生成 changelog → 创建 Release（`latest: true`）→ 上传附件 → 构建并推送镜像。

### 预览版（分支推送自动触发）

- 推送到 `dev` / `main` 即自动构建预览版：仅 `dev` 更新 `dev-latest` 滚动标签与镜像；`main` 仅做校验与归档，不发布滚动标签 / 镜像。正式镜像只在 `v*` 标签推送时发布。

## 产物校验

- 每次发布生成 `SHA256SUMS`，列出全部归档哈希。
- `scripts/check-release-files.sh` 在正式版构建时校验产物完整性与命名规范。
- 跨平台目标清单见 `scripts/platforms.sh`（当前 28 个平台）。

## 回滚

- 滚动标签 `dev-latest` 通过 `git tag -f` 前移，旧版本可由历史 Release 归档获取。main 分支不再使用滚动标签。
- 正式版标签不可变，回滚请发布新的 `vX.Y.Z` 或重新指向历史提交。
