#!/usr/bin/env bash
set -euo pipefail

# 确保 E2E/截图环境安装了中文字体，否则 Chromium 会把中文渲染成方框。
# 已安装时直接跳过；无法安装时只告警，不阻塞测试（无 root 的开发者机器）。

if command -v fc-list >/dev/null 2>&1 && fc-list :lang=zh 2>/dev/null | grep -q .; then
  echo "e2e fonts: CJK font already installed"
  exit 0
fi

SUDO=""
if [[ "${EUID}" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    SUDO="sudo -n"
  else
    echo "e2e fonts: warning: no root privileges, cannot install CJK fonts" >&2
    echo "e2e fonts: install 'fonts-noto-cjk' manually to keep screenshots readable" >&2
    exit 0
  fi
fi

run() {
  # shellcheck disable=SC2086
  $SUDO "$@"
}

if command -v apt-get >/dev/null 2>&1; then
  echo "e2e fonts: installing fonts-noto-cjk via apt-get"
  run apt-get update -qq
  run env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends fonts-noto-cjk
elif command -v dnf >/dev/null 2>&1; then
  echo "e2e fonts: installing Noto CJK via dnf"
  run dnf install -y google-noto-sans-cjk-ttc-fonts || run dnf install -y google-noto-cjk
elif command -v yum >/dev/null 2>&1; then
  echo "e2e fonts: installing Noto CJK via yum"
  run yum install -y google-noto-sans-cjk-ttc-fonts || run yum install -y google-noto-cjk
elif command -v apk >/dev/null 2>&1; then
  echo "e2e fonts: installing font-noto-cjk via apk"
  run apk add --no-cache font-noto-cjk
else
  echo "e2e fonts: warning: unsupported package manager, install Noto CJK fonts manually" >&2
  exit 0
fi

if command -v fc-cache >/dev/null 2>&1; then
  run fc-cache -f >/dev/null 2>&1 || true
fi

if command -v fc-list >/dev/null 2>&1 && fc-list :lang=zh 2>/dev/null | grep -q .; then
  echo "e2e fonts: CJK font installed"
else
  echo "e2e fonts: warning: CJK font still missing, screenshots may show boxes" >&2
fi
