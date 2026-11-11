#!/usr/bin/env bash
set -euo pipefail

# 代理稳健性的黑盒 E2E 验收：编译真实二进制，启动真实进程，
# 用真实 TCP/UDP 客户端验证故障场景（UDP 生命周期、会话上限、
# 日志限频、半关闭、级联 BIND、fd 耗尽退避等）。
#
# 用法：
#   bash scripts/run-proxy-resilience-e2e.sh
#
# 可覆盖的环境变量：
#   YIGATE_RESILIENCE_E2E_BIN   被测二进制（默认 build/yigate-e2e，不存在则编译）
#   GO_BIN                      go 命令路径（默认 go）
#   YIGATE_E2E_LISTEN_PORT      代理监听端口（默认 14481）
#   YIGATE_E2E_MGMT_PORT        管理端口（默认 14490）
#   YIGATE_E2E_SLOW=1           额外执行 30s 以上的慢速用例
#   YIGATE_E2E_KEEP=1           保留临时配置与日志目录

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${YIGATE_RESILIENCE_E2E_BIN:-${REPO_ROOT}/build/yigate-e2e}"
GO_BIN="${GO_BIN:-go}"

if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required" >&2
  exit 1
fi

# 验收必须针对当前代码，默认每次重新编译；复用已有二进制可显式跳过。
if [[ "${YIGATE_RESILIENCE_E2E_SKIP_BUILD:-0}" != "1" || ! -x "${BIN}" ]]; then
  echo "building ${BIN}"
  mkdir -p "$(dirname "${BIN}")"
  (cd "${REPO_ROOT}/src" && "${GO_BIN}" build -o "${BIN}" .)
fi

exec python3 "${REPO_ROOT}/test/e2e/test_proxy_resilience.py" --bin "${BIN}"
