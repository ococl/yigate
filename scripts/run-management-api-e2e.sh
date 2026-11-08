#!/usr/bin/env bash
set -euo pipefail

# 在临时目录启动一个隔离的 YiGate 实例，运行 test/e2e/test_management_api.py，
# 退出时清理进程与临时配置，避免 CRUD 用例污染仓库内的 test/e2e/config_webui.yml。
#
# 用法：
#   make api-e2e
#   bash scripts/run-management-api-e2e.sh
#
# 可覆盖的环境变量：YIGATE_API_E2E_BIN / YIGATE_API_E2E_CONFIG /
# YIGATE_API_E2E_LISTEN_PORT / YIGATE_API_E2E_MGMT_PORT / YIGATE_API_E2E_TOKEN

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${YIGATE_API_E2E_BIN:-${REPO_ROOT}/build/yigate-e2e}"
SRC_CONFIG="${YIGATE_API_E2E_CONFIG:-${REPO_ROOT}/test/e2e/config_webui.yml}"
LISTEN_PORT="${YIGATE_API_E2E_LISTEN_PORT:-4481}"
MGMT_PORT="${YIGATE_API_E2E_MGMT_PORT:-8895}"
TOKEN="${YIGATE_API_E2E_TOKEN:-yigate-webui-e2e-token}"
BASE="http://127.0.0.1:${MGMT_PORT}"

if [[ ! -x "${BIN}" ]]; then
  echo "error: management API E2E binary not found: ${BIN}" >&2
  echo "       run 'make api-e2e' or set YIGATE_API_E2E_BIN" >&2
  exit 1
fi

if [[ ! -f "${SRC_CONFIG}" ]]; then
  echo "error: management API E2E config not found: ${SRC_CONFIG}" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required to run the management API tests" >&2
  exit 1
fi

WORKDIR="$(mktemp -d -t yigate-api-e2e-XXXXXX)"
PID=""

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    kill "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT INT TERM

CONFIG="${WORKDIR}/config.yml"
cp "${SRC_CONFIG}" "${CONFIG}"
# 用独立端口运行，避免与本机已启动的实例冲突。
sed -i -E \
  -e "s#^listen: .*#listen: \"127.0.0.1:${LISTEN_PORT}\"#" \
  -e "s#^management: .*#management: \"127.0.0.1:${MGMT_PORT}\"#" \
  "${CONFIG}"

echo "starting isolated instance on ${BASE} (config: ${CONFIG})"
"${BIN}" -config "${CONFIG}" run >"${WORKDIR}/server.log" 2>&1 &
PID=$!

ready=0
for _ in $(seq 1 150); do
  if ! kill -0 "${PID}" 2>/dev/null; then
    echo "error: instance exited during startup" >&2
    cat "${WORKDIR}/server.log" >&2
    exit 1
  fi
  if python3 - "${BASE}/health" "${TOKEN}" >/dev/null 2>&1 <<'PY'
import sys
import urllib.error
import urllib.request

req = urllib.request.Request(
    sys.argv[1], headers={"Authorization": f"Bearer {sys.argv[2]}"}
)
try:
    with urllib.request.urlopen(req, timeout=2) as resp:
        sys.exit(0 if resp.status == 200 else 1)
except (urllib.error.URLError, OSError):
    sys.exit(1)
PY
  then
    ready=1
    break
  fi
  sleep 0.2
done

if [[ "${ready}" -ne 1 ]]; then
  echo "error: instance did not become ready within 30s" >&2
  cat "${WORKDIR}/server.log" >&2
  exit 1
fi

set +e
YIGATE_API_BASE="${BASE}" \
YIGATE_API_TOKEN="${TOKEN}" \
YIGATE_API_CONFIG="${CONFIG}" \
  python3 "${REPO_ROOT}/test/e2e/test_management_api.py"
status=$?
set -e

if [[ "${status}" -ne 0 ]]; then
  echo "--- server log (tail) ---" >&2
  tail -n 50 "${WORKDIR}/server.log" >&2
fi

exit "${status}"
