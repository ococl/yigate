#!/bin/sh
set -e

# 以 /var/lib/yigate 为工作目录，确保配置中相对路径（data/、logs/）
# 与 docker-compose 的 volumes 挂载点一致。
cd /var/lib/yigate

CONFIG_DIR="/var/lib/yigate/config"
DATA_DIR="/var/lib/yigate/data"
LOGS_DIR="/var/lib/yigate/logs"
CONFIG_FILE="${CONFIG_DIR}/config.yml"

# bind mount 的目录所有者通常是宿主机用户，容器内 yigate 用户可能无写权限。
# entrypoint 以 root 运行时修正 /var/lib/yigate 下所有目录的属主，确保日志/数据可写。
if [ "$(id -u)" = "0" ]; then
  chown -R yigate:yigate /var/lib/yigate
fi

# 若存在打包的 GeoSite 数据种子，复制到 data 目录。
if [ ! -f "${DATA_DIR}/geosite.dat" ] && [ -f "/etc/yigate/geosite.dat" ]; then
  cp /etc/yigate/geosite.dat "${DATA_DIR}/geosite.dat"
  if [ "$(id -u)" = "0" ]; then
    chown yigate:yigate "${DATA_DIR}/geosite.dat"
  fi
fi

# 降权到 yigate 运行主程序，避免以 root 长期运行。
if [ "$(id -u)" = "0" ]; then
  exec su -s /bin/sh yigate -c "exec /usr/local/bin/yigate -config \"$CONFIG_FILE\" run"
else
  exec /usr/local/bin/yigate -config "$CONFIG_FILE" run
fi
