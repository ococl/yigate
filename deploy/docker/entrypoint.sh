#!/bin/sh
set -e

# 以 /var/lib/yigate 为工作目录，确保配置中相对路径（data/、logs/）
# 与 docker-compose 的 volumes 挂载点一致。
cd /var/lib/yigate

DATA_DIR="/var/lib/yigate/data"
LOGS_DIR="/var/lib/yigate/logs"
CONFIG_FILE="${DATA_DIR}/config.yml"

# bind mount 的目录所有者通常是宿主机用户，容器内 yigate 用户可能无写权限。
# 仅修正挂载点 data/logs 的属主（read_only: true 下不能 chown 只读层父目录，否则 EROFS）。
if [ "$(id -u)" = "0" ]; then
  chown -R yigate:yigate "${DATA_DIR}" "${LOGS_DIR}" 2>/dev/null || true
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
