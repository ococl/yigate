#!/bin/sh
set -e

CONFIG_DIR="/var/lib/k5pw/config"
DATA_DIR="/var/lib/k5pw/data"
CONFIG_FILE="${CONFIG_DIR}/config.yml"

# 首次启动时从模板初始化运行配置
if [ ! -f "$CONFIG_FILE" ]; then
  TEMPLATE="${K5PW_TEMPLATE:-local}"
  case "$TEMPLATE" in
    lan)
      SOURCE="/etc/k5pw/config.lan.yml"
      ;;
    *)
      SOURCE="/etc/k5pw/config.local.yml"
      ;;
  esac
  if [ -f "$SOURCE" ]; then
    cp "$SOURCE" "$CONFIG_FILE"
    echo "Initialized config from template: $TEMPLATE"
  fi
fi

# GeoSite 数据目录初始化
if [ ! -f "${DATA_DIR}/geosite.dat" ] && [ -f "/etc/k5pw/geosite.dat" ]; then
  cp /etc/k5pw/geosite.dat "${DATA_DIR}/geosite.dat"
fi

exec /usr/local/bin/k5pw -config "$CONFIG_FILE"
