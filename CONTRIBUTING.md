# 贡献指南

感谢你关注 YiGate。本指南说明如何本地开发、提交规范与发布流程。

## 项目边界

YiGate 是 **合规、轻量、可运维** 的统一代理网关。我们 **明确不接受** 任何混淆协议、流量伪装、非合规隧道 / 传输方式相关的功能或代码。提交涉及此类需求的 PR 将不会被合并。

## 开发环境

- Go 1.21（依赖安装优先使用 `GOPROXY=https://goproxy.cn,direct`）
- Node 20+（仅前端 `web/`）
- 推荐在 Linux 下进行透明代理（TPROXY）相关开发

### 后端

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o yigate .
./yigate -config data/config.yml run
```

### 前端（独立项目）

```bash
cd web
npm install
npm run dev      # 开发服务器，代理 /api 到 127.0.0.1:8890
npm run build    # 输出到 web/dist/
```

前端改动若要进入二进制，需将 `web/dist/` 内容拷贝到 `src/proxy/webui_dist/` 后重新编译 Go。

## 代码约定

- 改动在当前工作区原地完成，避免无关重构；保持 PR 小而聚焦。
- 优先复用既有模块（路由、上游、DNS、审计），避免重复实现。
- Go 代码通过 `go vet ./...` 与 `gofmt` 检查。
- 监听 / socket 选项等平台差异，使用 build tag 拆分文件（如 `server_unix.go` / `server_windows.go`），勿在单一文件中用 `runtime.GOOS` 绕过。
- 不要提交真实代理密码、管理 Token、上游账号或私有地址；凭据一律通过 `${ENV_VAR}` 注入。
- `.codebuddy/` 为项目相关数据目录，请勿删除或提交其内容。

## 测试要求

- 修改后端后运行：

  ```bash
  cd src && GOPROXY=https://goproxy.cn,direct go test ./...
  ```

- 仅改动 `src/proxy` 时至少运行 `go test ./proxy`；涉及配置、启动、热加载或管理接口时运行全部测试。
- 前端：`cd web && npm run type-check && npm run build`。
- 协议入口改动需覆盖 HTTP、CONNECT、SOCKS5 三类路径；路由改动补充 `router` / `route_cache` / `socks5h` 测试；上游改动关注优先级、权重、健康检查与指定上游白名单。

## 提交规范

采用 Conventional Commits，消息使用 **中文** 类型前缀便于团队阅读：

- `feat:` 新功能
- `fix:` 缺陷修复
- `refactor:` 重构（不含行为变化）
- `docs:` 文档
- `test:` 测试
- `chore:` 构建 / 工具 / 杂项

示例：

```
fix: 修复热加载时监听地址切换导致的 address already in use

refactor: 将默认配置路径从 config/ 迁移到 data/config.yml
```

提交信息应说明 **为什么** 改，而非仅描述改了什么。

## PR 流程

1. 从 `main` 或 `dev` 切出特性分支。
2. 完成开发与测试，确保 `go vet` 与测试通过。
3. 提交到 `main` / `dev` 的 PR 会触发 CNB 的 `pr-check`（lint + test），未通过不予合并。
4. 在 PR 描述中说明动机、改动范围与验证方式。

## 发布

- 预览版由 `dev` / `main` 分支推送自动构建（详见 `VERSIONING.md`）。
- 正式版由 `vX.Y.Z` 标签推送触发，请先确认测试与 changelog 就绪。
- 发布构建脚本：`./scripts/release.sh X.Y.Z`；平台清单见 `scripts/platforms.sh`。

## 行为准则

保持尊重与专业。对合规边界有疑问时，优先选择“不实现非合规能力”。
