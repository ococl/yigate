# 版本管理方案

YiGate 采用三分支版本管理机制，通过 CNB CI 自动化驱动测试、构建和发布。

## 分支策略

| 分支 | 用途 | 版本前缀 | 发布类型 |
|------|------|----------|----------|
| `dev` | 日常开发 | `0.0.0-dev.<commit>` | 早鸟预览版 (pre-release) |
| `main` | 集成稳定 | `0.0.0-beta.<commit>` | Beta 版 (pre-release) |
| `v*` tag | 正式发布 | `X.Y.Z` (取自 tag) | 正式版 (release) |

## 工作流

```
dev (日常开发)
 │  push → CI: lint + test + build → 早鸟预览版
 │
 ▼  PR 合并
main (集成稳定)
 │  push → CI: lint + test + build → Beta 版
 │
 ▼  打 tag (vX.Y.Z)
tag_push → CI: lint + test + build + verify + release → 正式版
```

### 1. dev 分支 — 早鸟预览版

- 每次 push 触发完整 CI 流程
- 版本号: `0.0.0-dev.<commit_short>` (如 `0.0.0-dev.abc12345`)
- 创建/更新滚动 pre-release: `dev-latest`
- 用途: 内部测试和早期预览

### 2. main 分支 — Beta 版

- 每次 push 触发完整 CI 流程
- 版本号: `0.0.0-beta.<commit_short>` (如 `0.0.0-beta.abc12345`)
- 创建/更新滚动 pre-release: `beta-latest`
- 用途: 接近正式发布的公开测试版

### 3. v* Tag — 正式版

- 推送 `vX.Y.Z` 格式的 tag 触发正式发布流程
- 版本号: `X.Y.Z` (从 tag 名提取，去掉 `v` 前缀)
- 创建正式 Release，标记为 latest
- 上传全部跨平台制品和校验和
- 用途: 生产环境部署

## 版本号规则

- 遵循 [Semantic Versioning](https://semver.org/) `MAJOR.MINOR.PATCH` 格式
- Tag 格式: `vX.Y.Z` (如 `v1.2.0`)
- dev/beta 版本号包含短 commit SHA，确保可追溯

## CI 流程详情

所有分支触发相同的完整流程:

1. **lint** — `go vet ./...` 静态检查
2. **test** — `go test ./...` 单元测试
3. **build** — 跨平台编译 (20 个目标平台) + Linux 打包 + 校验和
4. **release** — 创建 CNB Release (仅 tag_push 为正式版)

正式版额外步骤:

5. **verify** — 校验制品文件集合完整性
6. **changelog** — 自动生成变更日志
7. **upload** — 上传全部制品到 Release

## 如何发布正式版

```bash
# 1. 确保 main 分支代码已测试通过
git checkout main
git pull origin main

# 2. 创建版本 tag
git tag v1.2.0
git push origin v1.2.0

# 3. CNB CI 自动触发正式发布流程
#    构建完成后在仓库 Release 页面查看制品
```

## 版本号注入

版本号通过 Go ldflags 注入到二进制文件:

```
go build -ldflags "-s -w \
  -X main.version=<version> \
  -X main.commit=<commit> \
  -X main.buildTime=<build_time>"
```

运行 `yigate -version` 可查看版本信息:

```
yigate version 1.2.0 (commit abc12345, built 2025-07-15T10:00:00Z)
```

## 相关文件

| 文件 | 说明 |
|------|------|
| `.cnb.yml` | CNB CI/CD 流水线配置 |
| `scripts/version.sh` | 版本号确定逻辑 |
| `scripts/ci-build.sh` | CI 构建脚本 |
| `scripts/release.sh` | 本地正式发布脚本 (从已有 tag 构建) |
| `scripts/platforms.sh` | 跨平台构建配置 |
| `Makefile` | 本地构建入口 |
