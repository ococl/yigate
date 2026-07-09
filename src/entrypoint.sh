#!/bin/sh
set -e

mkdir -p /k5/logs
exec /usr/local/bin/k5pw -config /k5/config.yml
