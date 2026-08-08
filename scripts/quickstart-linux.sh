#!/usr/bin/env bash
#
# yigate Linux 原生一键启动脚本（非 Docker）
#
# 功能：
#   1. 若缺失则自动下载/构建二进制（已存在 ./yigate 或 /usr/local/bin/yigate 时跳过）
#   2. 若缺失则自动生成默认配置文件（data/config.yml）
#   3. 前台启动 yigate，Ctrl+C 退出
#   4. 支持 install 子命令注册为 systemd 服务
#
# 用法：
#   ./scripts/quickstart-linux.sh              # 前台运行（自动准备二进制与配置）
#   ./scripts/quickstart-linux.sh install      # 安装为系统服务并启动
#   ./scripts/quickstart-linux.sh uninstall    # 卸载系统服务
#   YIGATE_VERSION=v1.2.0 ./scripts/quickstart-linux.sh   # 指定发布版本
#
set -euo pipefail

# ---------- 可配置变量 ----------
YIGATE_VERSION="${YIGATE_VERSION:-latest}"
YIGATE_BIN_DIR="${YIGATE_BIN_DIR:-/usr/local/bin}"
YIGATE_DATA_DIR="${YIGATE_DATA_DIR:-/var/lib/yigate}"
YIGATE_CONFIG="${YIGATE_CONFIG:-$PWD/data/config.yml}"
YIGATE_LOG_DIR="${YIGATE_LOG_DIR:-/var/log/yigate}"
GITHUB_REPO="${GITHUB_REPO:-yi-team/yigate}"

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

  log "未找到 yigate 二进制，尝试下载 $YIGATE_VERSION ..."
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  local ver="$YIGATE_VERSION"
  if [ "$ver" = "latest" ]; then
    ver="$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" \
      | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
    [ -n "$ver" ] || { echo "无法解析 latest 版本" >&2; exit 1; }
  fi
  log "下载版本: $ver"

  local asset="yigate-${ver#v}-linux-${PKG_ARCH}.tar.gz"
  local url="https://github.com/$GITHUB_REPO/releases/download/$ver/$asset"
  curl -fSL "$url" -o "$tmp/yigate.tar.gz" || {
    echo "下载失败: $url" >&2; exit 1;
  }
  tar -xzf "$tmp/yigate.tar.gz" -C "$tmp"
  install -m 0755 "$tmp/yigate" "$YIGATE_BIN_DIR/yigate"
  YIGATE_BIN="$YIGATE_BIN_DIR/yigate"
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
  # 通过首次运行自动生成：先尝试用占位路径触发生成逻辑
  # yigate 在 -config 指向不存在文件时会自动写入默认配置
  if "$YIGATE_BIN" -config "$YIGATE_CONFIG" run >/dev/null 2>&1 & then
    local pid=$!
    sleep 1
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
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
  ensure_binary
  ensure_config
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
Restart=on-failure
RestartSec=5
User=root
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

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
