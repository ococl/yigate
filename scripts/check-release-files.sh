#!/usr/bin/env bash
set -euo pipefail

# 校验 release/ 目录中的文件集合与预期白名单完全一致。
# 用法: check-release-files.sh <version>
#
# 预期产物: 每个平台一个压缩归档 (tar.gz/zip) + SBOM + SHA256SUMS
# 不含裸二进制和隐藏文件 (除 .version 外)。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
SCRIPTS_DIR="${REPO_ROOT}/scripts"
source "${SCRIPTS_DIR}/platforms.sh"

VERSION="$1"
status=0

# 构建预期文件集合: 每个平台一个归档
expected=()
for platform in $(all_platforms); do
  suffix="$(normalize_platform "${platform}")"
  goos="$(echo "$platform" | cut -d/ -f1)"
  if [[ "$goos" == "windows" ]]; then
    expected+=("yigate-${VERSION}-${suffix}.zip")
  else
    expected+=("yigate-${VERSION}-${suffix}.tar.gz")
  fi
done
# SBOM 是正式发布的必选产物
expected+=("yigate-${VERSION}.spdx.json")
expected+=("SHA256SUMS")

# 获取实际文件集合 (排除隐藏文件)
if [[ ! -d "${RELEASE_DIR}" ]]; then
  echo "error: release 目录不存在" >&2
  exit 1
fi

actual=()
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  actual+=("$f")
done < <(cd "${RELEASE_DIR}" && find . -maxdepth 1 -type f ! -name '.*' -printf '%P\n' | sort)

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

# 禁止符号链接
if find "${RELEASE_DIR}" -maxdepth 1 -type l | grep -q .; then
  echo "error: release 目录包含符号链接" >&2
  status=1
fi

# SHA256SUMS 必须覆盖 SBOM，且所有条目必须能通过校验
if [[ -f "${RELEASE_DIR}/SHA256SUMS" ]]; then
  if ! grep -q "yigate-${VERSION}.spdx.json" "${RELEASE_DIR}/SHA256SUMS"; then
    echo "error: SHA256SUMS 未包含 SBOM" >&2
    status=1
  fi
  if ! (cd "${RELEASE_DIR}" && sha256sum -c SHA256SUMS >/dev/null); then
    echo "error: SHA256SUMS 校验失败" >&2
    status=1
  fi
fi

if [[ $status -eq 0 ]]; then
  echo "OK: 正式制品文件集合检查通过"
fi
exit $status
