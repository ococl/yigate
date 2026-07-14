#!/usr/bin/env bash
set -euo pipefail

# 校验 release/ 目录中的文件集合与预期白名单完全一致。
# 用法: check-release-files.sh <version>

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
SCRIPTS_DIR="${REPO_ROOT}/scripts"
source "${SCRIPTS_DIR}/platforms.sh"

VERSION="$1"
status=0

# 构建预期文件集合
expected=()
for platform in $(all_platforms); do
  expected+=("$(output_name "${VERSION}" "${platform}")")
done
for platform in $(linux_platforms); do
  suffix="$(normalize_platform "${platform}")"
  expected+=("k5pw-${VERSION}-${suffix}.tar.gz")
done
expected+=("k5pw-${VERSION}.spdx.json")
expected+=("SHA256SUMS")

# 获取实际文件集合
if [[ ! -d "${RELEASE_DIR}" ]]; then
  echo "error: release 目录不存在" >&2
  exit 1
fi

actual=()
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  actual+=("$f")
done < <(cd "${RELEASE_DIR}" && find . -maxdepth 1 -type f -printf '%P\n' | sort)

# 比较
expected_sorted=($(printf '%s\n' "${expected[@]}" | sort))

if [[ ${#actual[@]} -ne ${#expected_sorted[@]} ]]; then
  echo "error: 文件数量不匹配（实际 ${#actual[@]}，预期 ${#expected_sorted[@]}）" >&2
  status=1
fi

for i in "${!expected_sorted[@]}"; do
  if [[ "${actual[$i]:-}" != "${expected_sorted[$i]}" ]]; then
    echo "error: 文件不匹配: 实际='${actual[$i]:-}' 预期='${expected_sorted[$i]}'" >&2
    status=1
  fi
done

# 禁止符号链接和隐藏文件
if find "${RELEASE_DIR}" -maxdepth 1 -type l | grep -q .; then
  echo "error: release 目录包含符号链接" >&2
  status=1
fi

if find "${RELEASE_DIR}" -maxdepth 1 -name '.*' -not -name '.' | grep -q .; then
  echo "error: release 目录包含隐藏文件" >&2
  status=1
fi

if [[ $status -eq 0 ]]; then
  echo "OK: 正式制品文件集合检查通过"
fi
exit $status
