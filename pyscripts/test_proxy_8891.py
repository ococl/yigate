#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
k5pw 8891 端口全能力验收测试脚本。

本脚本是对 test_proxy_4479.py 的薄包装，仅将默认端口改为 8891，
从而与 4479 测试保持逻辑一致，避免重复维护两份代码。

运行前请确保 k5pw 已监听 127.0.0.1:8891。

用法：
    python3 test_proxy_8891.py
    python3 test_proxy_8891.py --strict
"""

import sys

try:
    import test_proxy_4479
except ImportError as e:
    print(f"导入测试模块失败: {e}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    extra = sys.argv[1:]
    sys.argv = ["test_proxy_8891.py", "--port", "8891"] + extra
    test_proxy_4479.main()
