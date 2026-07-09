#!/bin/sh
set -e

# 确保挂载目录对 k5pw 用户可写
mkdir -p /k5/logs
chown -R k5pw:k5pw /k5

# 以非 root 用户运行主进程
exec su-exec k5pw /usr/local/bin/k5pw -config /k5/config.yml
