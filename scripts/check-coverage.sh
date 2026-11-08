#!/usr/bin/env bash
set -euo pipefail

# check-coverage.sh — Go 测试覆盖率下限门禁。
# 用法: check-coverage.sh [阈值百分比] [coverage.out 路径]
# 当前基线约 71.7%，v1.0 目标是 80%（提升后再调高阈值）。

THRESHOLD="${1:-70}"
PROFILE="${2:-src/coverage.out}"

if [[ ! -f "${PROFILE}" ]]; then
  echo "error: 找不到覆盖率文件 ${PROFILE}" >&2
  exit 1
fi

# CI 镜像中 go 通常在 PATH；本地/最小化镜像里可能只存在于标准安装位置。
GO_BIN="${GO:-go}"
if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  for candidate in /usr/local/go/bin/go /opt/toolchain/go/bin/go; do
    if [[ -x "${candidate}" ]]; then
      GO_BIN="${candidate}"
      break
    fi
  done
fi

# cover -func 需要在 Go module 根目录解析 profile 中的包名。
profile_dir="$(cd "$(dirname "${PROFILE}")" && pwd)"
profile_name="$(basename "${PROFILE}")"
total="$(cd "${profile_dir}" && "${GO_BIN}" tool cover -func="${profile_name}" | awk '/^total:/ { gsub("%", "", $3); print $3; exit }')"
if [[ -z "${total}" ]]; then
  echo "error: 无法从 ${PROFILE} 解析总覆盖率" >&2
  exit 1
fi

if ! awk -v c="${total}" -v t="${THRESHOLD}" 'BEGIN { exit !(c + 0 >= t + 0) }'; then
  echo "error: 覆盖率 ${total}% 低于阈值 ${THRESHOLD}%" >&2
  exit 1
fi

echo "coverage ${total}% >= ${THRESHOLD}%"
