#!/bin/sh
set -e

CONFIG_DIR="/var/lib/yigate/config"
DATA_DIR="/var/lib/yigate/data"
CONFIG_FILE="${CONFIG_DIR}/config.yml"

# 首次启动时从模板初始化运行配置
if [ ! -f "$CONFIG_FILE" ]; then
  TEMPLATE="${YIGATE_TEMPLATE:-local}"
  case "$TEMPLATE" in
    lan)
      SOURCE="/etc/yigate/config.lan.yml"
      ;;
    *)
      SOURCE="/etc/yigate/config.local.yml"
      ;;
  esac
  if [ -f "$SOURCE" ]; then
    cp "$SOURCE" "$CONFIG_FILE"
    echo "Initialized config from template: $TEMPLATE"
  fi
fi

# GeoSite 数据目录初始化
if [ ! -f "${DATA_DIR}/geosite.dat" ] && [ -f "/etc/yigate/geosite.dat" ]; then
  cp /etc/yigate/geosite.dat "${DATA_DIR}/geosite.dat"
fi

exec /usr/local/bin/yigate -config "$CONFIG_FILE"
