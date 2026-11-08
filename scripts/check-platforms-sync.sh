#!/usr/bin/env bash
set -euo pipefail

# check-platforms-sync.sh — 防止跨平台清单在三处定义后发生漂移。
# 唯一权威来源是 scripts/platforms.sh；本脚本校验 GitHub Actions matrix 与
# Windows PowerShell 构建脚本中的清单与之一致。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
status=0

platforms_from_source() {
  sed -n '/^YIGATE_PLATFORMS=(/,/^)/p' "${REPO_ROOT}/scripts/platforms.sh" \
    | grep -Eo '^[[:space:]]*[a-z0-9]+/[a-z0-9]+(/v[0-9]+)?' \
    | tr -d '[:blank:]' | sort -u
}

platforms_from_ci() {
  sed -n '/matrix:/,/steps:/p' "${REPO_ROOT}/.github/workflows/ci.yml" \
    | grep -Eo '^[[:space:]]*-[[:space:]]*[a-z0-9]+/[a-z0-9]+(/v[0-9]+)?' \
    | sed -E 's/^[[:space:]]*-[[:space:]]*//' | sort -u
}

platforms_from_ps1() {
  grep -Eo '"[a-z0-9]+/[a-z0-9]+(/v[0-9]+)?"' "${REPO_ROOT}/scripts/build.ps1" \
    | tr -d '"' | sort -u
}

source_list="$(mktemp)"
trap 'rm -f "${source_list}"' EXIT
platforms_from_source > "${source_list}"
count="$(wc -l < "${source_list}" | tr -d ' ')"
if [[ "${count}" -ne 26 ]]; then
  echo "error: platforms.sh 应定义 26 个平台，实际 ${count} 个" >&2
  status=1
fi

for target in ci ps1; do
  if [[ "${target}" == "ci" ]]; then
    name=".github/workflows/ci.yml"
    actual="$(platforms_from_ci)"
  else
    name="scripts/build.ps1"
    actual="$(platforms_from_ps1)"
  fi
  target_count="$(echo "${actual}" | grep -c . || true)"
  if [[ "${target_count}" -ne "${count}" ]]; then
    echo "error: ${name} 平台数 ${target_count} 与 platforms.sh 的 ${count} 不一致" >&2
    status=1
  fi
  diff_output="$(diff -u "${source_list}" <(echo "${actual}") || true)"
  if [[ -n "${diff_output}" ]]; then
    echo "error: ${name} 与 platforms.sh 的平台清单不一致:" >&2
    echo "${diff_output}" >&2
    status=1
  fi
done

if [[ "${status}" -eq 0 ]]; then
  echo "platform lists in sync: ${count} platforms"
fi
exit "${status}"
