#!/usr/bin/env bash
set -euo pipefail

# 为 Playwright 提供独立 WebUI 测试后端。该进程由 Playwright webServer 管理。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${YIGATE_E2E_BIN:-${REPO_ROOT}/build/yigate-e2e}"
CONFIG="${YIGATE_E2E_CONFIG:-${REPO_ROOT}/test/e2e/config_webui.yml}"

if [[ ! -x "${BIN}" ]]; then
  echo "error: WebUI E2E binary not found: ${BIN}" >&2
  echo "       run 'make web-e2e' or set YIGATE_E2E_BIN" >&2
  exit 1
fi

if [[ ! -f "${CONFIG}" ]]; then
  echo "error: WebUI E2E config not found: ${CONFIG}" >&2
  exit 1
fi

exec "${BIN}" -config "${CONFIG}" run
