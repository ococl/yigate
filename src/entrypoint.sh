#!/bin/sh
set -e

exec /usr/local/bin/k5pw -config "${K5PW_CONFIG:-/etc/k5pw/config.yml}"
