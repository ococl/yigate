#!/usr/bin/env bash
#
# yigate Linux 原生一键启动脚本（非 Docker）
#
# 功能：
#   1. 若缺失则从显式发布地址下载二进制（已存在 ./ygate 或 /usr/local/bin/yigate 时跳过）
#   2. 若缺失则自动生成默认配置文件（data/config.yml）
#   3. 前台启动 yigate，Ctrl+C 退出
#   4. 支持 install 子命令注册为 systemd 服务
#
# 用法：
#   ./scripts/quickstart-linux.sh              # 前台运行（使用本地二进制）
#   YIGATE_RELEASE_URL=https://.../yigate-1.2.3-linux-amd64.tar.gz \
#     ./scripts/quickstart-linux.sh            # 下载指定发布归档并校验 SHA256
#   ./scripts/quickstart-linux.sh install      # 安装为系统服务并启动
#   ./scripts/quickstart-linux.sh uninstall    # 卸载系统服务
#
set -euo pipefail

# ---------- 可配置变量 ----------
YIGATE_BIN_DIR="${YIGATE_BIN_DIR:-/usr/local/bin}"
YIGATE_DATA_DIR="${YIGATE_DATA_DIR:-/var/lib/yigate}"
YIGATE_CONFIG="${YIGATE_CONFIG:-$PWD/data/config.yml}"
YIGATE_LOG_DIR="${YIGATE_LOG_DIR:-/var/log/yigate}"
# 指向具体发布资产（.tar.gz），不是仓库或 Releases 首页。
YIGATE_RELEASE_URL="${YIGATE_RELEASE_URL:-}"

# 根据系统架构选择发布包
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) PKG_ARCH="amd64" ;;
  aarch64|arm64) PKG_ARCH="arm64" ;;
  *) echo "不支持的架构: $ARCH" >&2; exit 1 ;;
esac

log() { echo "[quickstart] $*"; }

# ---------- 步骤 1：确保二进制存在 ----------
ensure_binary() {
  if [ -x "$PWD/yigate" ]; then
    YIGATE_BIN="$PWD/yigate"
    log "使用本地二进制: $YIGATE_BIN"
    return
  fi
  if [ -x "$YIGATE_BIN_DIR/yigate" ]; then
    YIGATE_BIN="$YIGATE_BIN_DIR/yigate"
    log "使用系统二进制: $YIGATE_BIN"
    return
  fi

  if [ -z "$YIGATE_RELEASE_URL" ]; then
    echo "未找到 yigate 二进制。" >&2
    echo "请从 CNB Release 下载对应 linux-${PKG_ARCH} 归档并放到当前目录，或设置：" >&2
    echo "  YIGATE_RELEASE_URL=https://.../yigate-<version>-linux-${PKG_ARCH}.tar.gz" >&2
    exit 1
  fi

  log "下载发布归档: $YIGATE_RELEASE_URL"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  local url_path="${YIGATE_RELEASE_URL%%\?*}"
  local asset
  asset="$(basename "$url_path")"
  local sums_url="${url_path%/*}/SHA256SUMS"

  curl -fSL "$YIGATE_RELEASE_URL" -o "$tmp/yigate.tar.gz" || {
    echo "下载失败: $YIGATE_RELEASE_URL" >&2
    exit 1
  }

  if curl -fsSL "$sums_url" -o "$tmp/SHA256SUMS"; then
    local expected actual
    expected="$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$tmp/SHA256SUMS")"
    if [ -z "$expected" ]; then
      echo "SHA256SUMS 中未找到 $asset" >&2
      exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$tmp/yigate.tar.gz" | awk '{print $1}')"
    else
      actual="$(shasum -a 256 "$tmp/yigate.tar.gz" | awk '{print $1}')"
    fi
    if [ "$expected" != "$actual" ]; then
      echo "SHA256 校验失败: $asset" >&2
      exit 1
    fi
    log "SHA256 校验通过"
  elif [ "${YIGATE_ALLOW_UNVERIFIED_DOWNLOAD:-0}" != "1" ]; then
    echo "无法下载 SHA256SUMS，拒绝安装未校验归档。" >&2
    echo "如确认来源可信，可显式设置 YIGATE_ALLOW_UNVERIFIED_DOWNLOAD=1。" >&2
    exit 1
  fi

  tar -xzf "$tmp/yigate.tar.gz" -C "$tmp"
  local install_path="$YIGATE_BIN_DIR/yigate"
  if [ "$(id -u)" -ne 0 ]; then
    install_path="$PWD/yigate"
  else
    install -d "$YIGATE_BIN_DIR"
  fi
  install -m 0755 "$tmp/yigate" "$install_path"
  YIGATE_BIN="$install_path"
  log "已安装到 $YIGATE_BIN"
}

# ---------- 步骤 2：确保配置存在 ----------
ensure_config() {
  if [ -f "$YIGATE_CONFIG" ]; then
    log "使用现有配置: $YIGATE_CONFIG"
    return
  fi
  mkdir -p "$(dirname "$YIGATE_CONFIG")"
  "$YIGATE_BIN" -version >/dev/null 2>&1 || true
  # 通过首次运行自动生成默认配置
  "$YIGATE_BIN" -config "$YIGATE_CONFIG" run >/dev/null 2>&1 &
  local pid=$!
  sleep 1
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  if [ -f "$YIGATE_CONFIG" ]; then
    log "已生成默认配置: $YIGATE_CONFIG"
  else
    echo "无法自动生成配置文件" >&2
    exit 1
  fi
}

# ---------- 前台运行 ----------
run_foreground() {
  ensure_binary
  ensure_config
  log "启动 yigate（配置: $YIGATE_CONFIG）"
  log "代理端口: 127.0.0.1:8891  管理/WebUI: 127.0.0.1:8890"
  exec "$YIGATE_BIN" -config "$YIGATE_CONFIG" run
}

# ---------- systemd 安装 ----------
install_service() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "安装 systemd 服务需要 root 权限" >&2
    exit 1
  fi
  ensure_binary

  # 安装模式默认把配置放进 /var/lib/yigate，避免服务用户无法访问用户主目录。
  if [ "$YIGATE_CONFIG" = "$PWD/data/config.yml" ]; then
    YIGATE_CONFIG="$YIGATE_DATA_DIR/data/config.yml"
  fi

  if ! id -u yigate >/dev/null 2>&1; then
    useradd --system --home "$YIGATE_DATA_DIR" --shell /usr/sbin/nologin yigate
  fi
  install -d -m 0750 -o yigate -g yigate "$YIGATE_DATA_DIR" "$YIGATE_LOG_DIR" "$(dirname "$YIGATE_CONFIG")"

  ensure_config
  chown yigate:ygate "$YIGATE_CONFIG"
  chmod 0600 "$YIGATE_CONFIG"

  local unit="/etc/systemd/system/yigate.service"
  log "写入 systemd unit: $unit"
  cat > "$unit" <<EOF
[Unit]
Description=YiGate Unified Proxy Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$YIGATE_BIN -config $YIGATE_CONFIG run
WorkingDirectory=$(dirname "$YIGATE_CONFIG")
Restart=on-failure
RestartSec=5
User=ygate
Group=ygate
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=$YIGATE_DATA_DIR $YIGATE_LOG_DIR $(dirname "$YIGATE_CONFIG")

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now yigate
  log "已安装并启动 yigate 服务"
  systemctl status yigate --no-pager || true
}

uninstall_service() {
  if [ -f /etc/systemd/system/yigate.service ]; then
    systemctl disable --now yigate 2>/dev/null || true
    rm -f /etc/systemd/system/yigate.service
    systemctl daemon-reload
    log "已卸载 yigate 服务"
  else
    log "未找到 yigate systemd 服务"
  fi
}

case "${1:-}" in
  install)   install_service ;;
  uninstall) uninstall_service ;;
  "")        run_foreground ;;
  *)         echo "用法: $0 [install|uninstall]" >&2; exit 1 ;;
esac
