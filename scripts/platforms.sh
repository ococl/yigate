#!/usr/bin/env bash
# 唯一的平台清单和产物命名逻辑，供 release.sh、Makefile、CI 和 build.ps1 复用。

# 平台清单：每行 "GOOS/GOARCH[/GOARM]"
K5PW_PLATFORMS=(
  linux/amd64
  linux/arm64
  linux/386
  linux/arm/v7
  linux/riscv64
  linux/ppc64le
  linux/s390x
  linux/mips64
  linux/mips64le
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/386
  windows/arm64
  freebsd/amd64
  freebsd/386
  freebsd/arm64
  openbsd/amd64
  openbsd/arm64
  netbsd/amd64
)

# 将平台标识规范化为不含斜杠的文件名后缀。
# 例: linux/amd64 -> linux-amd64, linux/arm/v7 -> linux-armv7
normalize_platform() {
  local platform="$1"
  local goos goarch goarm
  IFS='/' read -r goos goarch goarm <<< "$platform"
  local suffix="${goos}-${goarch}"
  if [[ "$goarch" == "arm" && "$goarm" == "v7" ]]; then
    suffix="${goos}-armv7"
  fi
  echo "$suffix"
}

# 输出带版本号的产物文件名。
# 用法: output_name <version> <platform>
output_name() {
  local version="$1"
  local platform="$2"
  local suffix
  suffix="$(normalize_platform "$platform")"
  local name="k5pw-${version}-${suffix}"
  local goos
  goos="$(echo "$platform" | cut -d/ -f1)"
  if [[ "$goos" == "windows" ]]; then
    name="${name}.exe"
  fi
  echo "$name"
}

# 输出所有平台标识，每行一个。
all_platforms() {
  for p in "${K5PW_PLATFORMS[@]}"; do
    echo "$p"
  done
}

# 输出所有 Linux 平台标识（用于安装包）。
linux_platforms() {
  for p in "${K5PW_PLATFORMS[@]}"; do
    if [[ "$p" == linux/* ]]; then
      echo "$p"
    fi
  done
}

# 返回构建用的环境变量。
# 用法: build_env <platform>
# 输出: "GOOS=... GOARCH=... GOARM=..."
build_env() {
  local platform="$1"
  local goos goarch goarm
  IFS='/' read -r goos goarch goarm <<< "$platform"
  goarm="${goarm#v}"
  echo "GOOS=${goos} GOARCH=${goarch} GOARM=${goarm}"
}
