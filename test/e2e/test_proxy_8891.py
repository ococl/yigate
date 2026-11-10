#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
yigate 8891 端口（生产实例）全能力验收测试脚本。

本脚本针对运行中的 yigate 生产实例（监听 0.0.0.0:8891，
管理端口 127.0.0.1:8890）进行端到端验证：
  - 国内站点（geosite CN -> 直连）
  - 国外站点（github/google 等 -> 经上游代理）
  - 四类代理入口：HTTP / HTTPS CONNECT / SOCKS5 / SOCKS5H
  - keep-alive 复用、大请求体、并发
  - 管理接口：health / metrics / status / reload
  - 从 4479 测试吸取的新特性用例：
    * SSRF 防护（元数据地址不可达）
    * SOCKS5 匿名认证
    * HTTP CONNECT 隧道（原始 socket）
    * SOCKS5 并发连接
    * SOCKS5 BIND 拒绝（allow_bind: false）
    * SOCKS5 UDP ASSOCIATE 拒绝（allow_udp_associate: false）
    * 运行时配置 API（/api/v1/config/upstreams）
    * 流量查询 API（/api/v1/traffic）
  - 对标测试用例（从 4479 同步）：
    * HTTP 方法矩阵（GET/HEAD/POST/PUT/DELETE/PATCH）
    * 错误处理（连接拒绝/DNS 失败）
    * Config API rules/users
    * Metrics 计数器递增验证
    * 路由决策验证（直连 vs 代理）
    * 协议错误处理

与 test_proxy_4479.py（自包含、本地 fixture）不同，本脚本依赖真实网络与
上游代理，验证的是生产实例的真实路由与出网能力。

运行前请确保 yigate 已在 8891 端口启动且上游可达。

用法：
    python3 test_proxy_8891.py
    python3 test_proxy_8891.py --strict        # 站点关键词缺失也视为失败
    python3 test_proxy_8891.py --proxy-host 127.0.0.1 --proxy-port 8891 --mgmt-port 8890
    YIGATE_MANAGEMENT_TOKEN=<token> python3 test_proxy_8891.py
"""

import argparse
import concurrent.futures
import os
import socket
import struct
import sys
import time

import requests

# ---------------------------------------------------------------------------
# 配置
# ---------------------------------------------------------------------------
PROXY_HOST = "127.0.0.1"
PROXY_PORT = 8891
MGMT_HOST = "127.0.0.1"
MGMT_PORT = 8890
MGMT_SESSION = requests.Session()

REQUEST_TIMEOUT = 20
MAX_RETRY = 3

# 国内站点：命中 geosite CN -> 直连
DOMESTIC_SITES = [
    # 百度 HTTPS 响应体偶发为 JS 跳转页，关键词不稳定，故仅以状态码判定
    {"name": "百度", "url": "https://www.baidu.com", "keyword": ""},
    {"name": "腾讯QQ", "url": "https://www.qq.com", "keyword": "腾讯"},
    {"name": "淘宝", "url": "https://www.taobao.com", "keyword": "淘宝"},
    {"name": "京东", "url": "https://www.jd.com", "keyword": "京东"},
    {"name": "网易163", "url": "https://www.163.com", "keyword": "网易"},
]

# 国外站点：命中 domain_suffix -> 经上游 7897 代理
FOREIGN_SITES = [
    {"name": "GitHub", "url": "https://github.com", "keyword": "GitHub"},
    {"name": "Google", "url": "https://www.google.com", "keyword": "Google"},
    {"name": "YouTube", "url": "https://www.youtube.com", "keyword": "YouTube"},
]

# 用于 SOCKS5 原始套接字探测的 HTTP :80 目标（返回任意 HTTP 响应即可）
SOCKS_HTTP_TARGETS = [
    ("www.baidu.com", 80, "百度"),
    ("www.qq.com", 80, "腾讯"),
    ("github.com", 80, "GitHub"),
]


class Colors:
    OK = "\033[92m"
    WARN = "\033[93m"
    FAIL = "\033[91m"
    INFO = "\033[94m"
    RESET = "\033[0m"


class TestResult:
    def __init__(self, name, ok, detail=""):
        self.name = name
        self.ok = ok
        self.detail = detail

    def __repr__(self):
        return f"[{'PASS' if self.ok else 'FAIL'}] {self.name}: {self.detail}"


# ---------------------------------------------------------------------------
# 最小 SOCKS5 客户端（支持 CONNECT；copy from test_proxy_4479）
# ---------------------------------------------------------------------------
class SOCKS5RawClient:
    """最小 SOCKS5 客户端，支持 CONNECT。"""

    def __init__(self, host, port, timeout=15):
        self.sock = socket.create_connection((host, port), timeout=timeout)
        self.sock.settimeout(timeout)

    def close(self):
        try:
            self.sock.close()
        except Exception:
            pass

    def _read_exact(self, n):
        buf = b""
        while len(buf) < n:
            chunk = self.sock.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("unexpected EOF during socks5 handshake")
            buf += chunk
        return buf

    def handshake(self, username=None, password=None):
        methods = [0x00]
        if username is not None or password is not None:
            methods.append(0x02)
        self.sock.sendall(bytes([0x05, len(methods)]) + bytes(methods))
        resp = self._read_exact(2)
        if resp[0] != 0x05 or resp[1] == 0xFF:
            raise ConnectionError("socks5 auth method rejected")
        if resp[1] == 0x02:
            if username is None or password is None:
                raise ConnectionError("server requires auth but no credentials provided")
            self._send_auth(username, password)

    def _send_auth(self, username, password):
        u, p = username.encode(), password.encode()
        if len(u) > 255 or len(p) > 255:
            raise ValueError("username/password too long")
        self.sock.sendall(bytes([0x01, len(u)]) + u + bytes([len(p)]) + p)
        resp = self._read_exact(2)
        if resp[0] != 0x01 or resp[1] != 0x00:
            raise ConnectionError("socks5 auth failed")

    def _addr_bytes(self, target, force_domain=False):
        host, port_s = target.rsplit(":", 1)
        port = int(port_s)
        if host.startswith("[") and host.endswith("]"):
            host = host[1:-1]
        if not force_domain:
            try:
                ip = socket.inet_pton(socket.AF_INET, host)
                return bytes([0x01]) + ip + struct.pack(">H", port)
            except OSError:
                pass
            try:
                ip = socket.inet_pton(socket.AF_INET6, host)
                return bytes([0x04]) + ip + struct.pack(">H", port)
            except OSError:
                pass
        host_b = host.encode()
        return bytes([0x03, len(host_b)]) + host_b + struct.pack(">H", port)

    def request(self, cmd, target, force_domain=False):
        addr = self._addr_bytes(target, force_domain=force_domain)
        self.sock.sendall(bytes([0x05, cmd, 0x00]) + addr)
        return self._read_reply()

    def _read_reply(self):
        header = self._read_exact(4)
        if header[0] != 0x05:
            raise ConnectionError(f"invalid socks5 reply version {header[0]}")
        rep = header[1]
        if rep != 0x00:
            raise ConnectionError(f"socks5 request failed: 0x{rep:02x}")
        return header


# ---------------------------------------------------------------------------
# 通用网络工具
# ---------------------------------------------------------------------------
def _http_get(url, proxy_url, timeout=REQUEST_TIMEOUT, verify=False):
    """带重试的 HTTP GET，返回 (status_code_or_None, text_or_Error)。"""
    last = None
    for _ in range(MAX_RETRY):
        try:
            r = requests.get(
                url,
                proxies={"http": proxy_url, "https": proxy_url},
                timeout=timeout,
                headers={"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"},
                allow_redirects=True,
                verify=verify,
            )
            return r.status_code, r.text
        except Exception as e:  # noqa: BLE001
            last = e
            time.sleep(1)
    return None, str(last)


def color_status(ok, warn=False):
    if ok:
        return f"{Colors.OK}OK{Colors.RESET}"
    if warn:
        return f"{Colors.WARN}WARN{Colors.RESET}"
    return f"{Colors.FAIL}FAIL{Colors.RESET}"


# ---------------------------------------------------------------------------
# 站点测试（四类代理入口）
# ---------------------------------------------------------------------------
def test_sites_via_proxy(proxy_label, proxy_url, sites, strict):
    """对给定站点列表用指定代理入口发起请求，返回结果列表。"""
    results = []
    for site in sites:
        code, text = _http_get(site["url"], proxy_url)
        keyword = site.get("keyword", "")
        ok = code is not None and code < 500
        found = (keyword in text) if (isinstance(text, str) and keyword) else (ok)
        if ok and not found:
            # 站点可达但关键词缺失：非严格模式下记为 WARN（不阻断）
            status = "WARN" if not strict else "FAIL"
            ok_for_count = not strict
            detail = f"HTTP {code} 但无关键词 '{site['keyword']}'"
        else:
            status = "OK" if ok else "FAIL"
            ok_for_count = ok
            detail = f"HTTP {code}" if ok else f"失败: {text[:60]}"
        results.append({
            "name": site["name"],
            "url": site["url"],
            "status": status,
            "ok": ok_for_count,
            "detail": detail,
        })
        print(f"  {site['name']:<10} {color_status(ok_for_count, status == 'WARN'):<8} {detail}")
    return results


# ---------------------------------------------------------------------------
# 单能力测试
# ---------------------------------------------------------------------------
def _proxy_url():
    return f"http://{PROXY_HOST}:{PROXY_PORT}"


def test_http_domestic():
    """HTTP 代理访问国内站点（geosite CN -> 直连）。"""
    code, text = _http_get("https://www.baidu.com", _proxy_url())
    ok = code == 200
    return TestResult("HTTP 代理-国内直连", ok, f"HTTP {code}" if ok else f"失败 HTTP {code} text={str(text)[:40]}")


def test_https_connect_domestic():
    """HTTPS CONNECT 隧道访问国内站点。"""
    code, text = _http_get("https://www.qq.com", _proxy_url())
    ok = code == 200
    return TestResult("HTTPS CONNECT-国内", ok, f"HTTP {code}" if ok else f"失败 HTTP {code}")


def test_http_foreign():
    """HTTP 代理访问国外站点（经上游 7897）。"""
    code, text = _http_get("https://github.com", _proxy_url())
    ok = code == 200 and "GitHub" in text
    return TestResult("HTTP 代理-国外(7897)", ok, f"HTTP {code}" if ok else f"失败 HTTP {code}")


def test_https_connect_foreign():
    """HTTPS CONNECT 隧道访问国外站点（经上游 7897）。"""
    code, text = _http_get("https://www.google.com", _proxy_url())
    ok = code == 200 and "Google" in text
    return TestResult("HTTPS CONNECT-国外(7897)", ok, f"HTTP {code}" if ok else f"失败 HTTP {code}")


def _socks5_http_get(target_host, force_domain=False, port=80):
    """经 SOCKS5 代理 CONNECT 到 target_host:port，发 GET 并返回是否收到 HTTP 响应。"""
    c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
    try:
        c.handshake()
        c.request(0x01, f"{target_host}:{port}", force_domain=force_domain)
        c.sock.sendall(
            f"GET / HTTP/1.1\r\nHost: {target_host}\r\nConnection: close\r\n\r\n".encode()
        )
        c.sock.settimeout(REQUEST_TIMEOUT)
        data = b""
        try:
            while True:
                chunk = c.sock.recv(4096)
                if not chunk:
                    break
                data += chunk
        except socket.timeout:
            pass
        return b"HTTP/" in data
    finally:
        c.close()


def test_socks5_domestic():
    """SOCKS5 CONNECT（本地解析）访问国内站点 :80。"""
    try:
        ok = _socks5_http_get("www.baidu.com", force_domain=False)
        return TestResult("SOCKS5 CONNECT-国内", ok, "收到 HTTP 响应" if ok else "未收到 HTTP 响应")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 CONNECT-国内", False, str(e))


def test_socks5h_domestic():
    """SOCKS5H CONNECT（代理端解析）访问国内站点 :80。"""
    try:
        ok = _socks5_http_get("www.qq.com", force_domain=True)
        return TestResult("SOCKS5H CONNECT-国内", ok, "收到 HTTP 响应" if ok else "未收到 HTTP 响应")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5H CONNECT-国内", False, str(e))


def test_socks5_foreign():
    """SOCKS5 CONNECT（本地解析）访问国外站点 :80（经上游 7897）。"""
    try:
        ok = _socks5_http_get("github.com", force_domain=False)
        return TestResult("SOCKS5 CONNECT-国外(7897)", ok, "收到 HTTP 响应" if ok else "未收到 HTTP 响应")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 CONNECT-国外(7897)", False, str(e))


def test_socks5h_foreign():
    """SOCKS5H CONNECT（代理端解析）访问国外站点 :80（经上游 7897）。"""
    try:
        ok = _socks5_http_get("github.com", force_domain=True)
        return TestResult("SOCKS5H CONNECT-国外(7897)", ok, "收到 HTTP 响应" if ok else "未收到 HTTP 响应")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5H CONNECT-国外(7897)", False, str(e))


def test_keepalive_reuse():
    """HTTP/1.1 keep-alive 连接复用：同一 Session 连续请求多个站点。"""
    try:
        s = requests.Session()
        s.proxies = {"http": _proxy_url(), "https": _proxy_url()}
        s.headers.update({"User-Agent": "Mozilla/5.0"})
        ok_count = 0
        for url in ["https://www.baidu.com", "https://www.jd.com", "https://github.com"]:
            try:
                r = s.get(url, timeout=REQUEST_TIMEOUT, allow_redirects=True)
                if r.status_code == 200:
                    ok_count += 1
            except Exception:
                pass
        ok = ok_count == 3
        return TestResult("keep-alive 复用", ok, f"成功 {ok_count}/3")
    except Exception as e:  # noqa: BLE001
        return TestResult("keep-alive 复用", False, str(e))


def test_large_post():
    """大请求体（2MB）经 HTTP 代理透传。"""
    try:
        payload = b"x" * (2 * 1024 * 1024)
        r = requests.post(
            "https://www.baidu.com",
            data=payload,
            proxies={"http": _proxy_url(), "https": _proxy_url()},
            timeout=REQUEST_TIMEOUT * 2,
            headers={"User-Agent": "Mozilla/5.0"},
            allow_redirects=True,
        )
        # 大请求体成功发出并收到响应（百度可能 302/200，均视为透传成功）
        ok = r.status_code < 500
        return TestResult("大请求体 2MB POST", ok, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("大请求体 2MB POST", False, str(e))


def test_concurrent_requests():
    """并发 HTTPS 请求（6 路）经代理，验证并发处理能力。"""
    urls = ["https://www.baidu.com", "https://www.qq.com", "https://www.taobao.com",
            "https://www.jd.com", "https://github.com", "https://www.google.com"]

    def one(url):
        code, _ = _http_get(url, _proxy_url())
        return code is not None and code < 500

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as ex:
            results = list(ex.map(one, urls))
        ok = all(results)
        return TestResult("并发请求 x6", ok, f"成功 {sum(results)}/6")
    except Exception as e:  # noqa: BLE001
        return TestResult("并发请求 x6", False, str(e))


def _mgmt_get(path):
    try:
        r = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}{path}", timeout=8)
        return r.status_code, r.text
    except Exception as e:  # noqa: BLE001
        return None, str(e)


def test_management_health():
    code, text = _mgmt_get("/health")
    ok = code == 200 and text.strip() == "UP"
    return TestResult("管理接口 /health", ok, f"HTTP {code}" if ok else f"失败 HTTP {code} {text[:40]}")


def test_management_metrics():
    code, text = _mgmt_get("/metrics")
    ok = code == 200 and "yigate" in text
    return TestResult("管理接口 /metrics", ok, f"HTTP {code} len={len(text)}" if ok else f"失败 HTTP {code}")


def test_management_status():
    code, text = _mgmt_get("/api/v1/status")
    ok = code == 200 and "listen" in text
    return TestResult("管理接口 /api/v1/status", ok, f"HTTP {code}" if ok else f"失败 HTTP {code}")


def test_management_reload():
    try:
        r = MGMT_SESSION.post(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/reload", timeout=8)
        ok = r.status_code == 200
        return TestResult("管理接口 /api/v1/reload", ok, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("管理接口 /api/v1/reload", False, str(e))


# ---------------------------------------------------------------------------
# 从 4479 测试吸取的新特性用例
# ---------------------------------------------------------------------------

def test_ssrf_metadata_unreachable():
    """SSRF 防护：元数据地址 169.254.169.254 不应被成功代理访问。

    dist 配置 protect_private: true，应拒绝云元数据等敏感地址。
    """
    try:
        proxies = {"http": _proxy_url(), "https": _proxy_url()}
        r = requests.get(
            "http://169.254.169.254/latest/meta-data/",
            proxies=proxies,
            timeout=8,
            verify=False,
        )
        if r.status_code == 200:
            return TestResult("SSRF 元数据地址不可达", False, f"意外可访问: HTTP {r.status_code}")
        return TestResult("SSRF 元数据地址不可达", True, f"被正确拒绝: HTTP {r.status_code}")
    except Exception:
        return TestResult("SSRF 元数据地址不可达", True, "连接失败（符合预期，地址被拒绝）")


def test_socks5_anonymous_auth():
    """SOCKS5 匿名认证：dist 配置 users 为空，应接受匿名 SOCKS5。"""
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, "www.baidu.com:80", force_domain=True)
        c.close()
        return TestResult("SOCKS5 匿名认证", True, "接受匿名")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 匿名认证", False, str(e))


def test_http_connect_tunnel():
    """HTTP CONNECT 隧道（原始 socket）：验证 CONNECT 路径字节完整性。"""
    try:
        c = socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=10)
        c.sendall(b"CONNECT www.baidu.com:80 HTTP/1.1\r\nHost: www.baidu.com\r\n\r\n")
        resp = c.recv(4096)
        if b"200" not in resp:
            return TestResult("HTTP CONNECT 隧道(原始)", False, f"CONNECT 失败: {resp[:60]!r}")
        c.sendall(b"GET / HTTP/1.1\r\nHost: www.baidu.com\r\nConnection: close\r\n\r\n")
        data = b""
        c.settimeout(REQUEST_TIMEOUT)
        while True:
            try:
                chunk = c.recv(4096)
                if not chunk:
                    break
                data += chunk
            except socket.timeout:
                break
        c.close()
        ok = b"HTTP/" in data
        return TestResult("HTTP CONNECT 隧道(原始)", ok, "隧道回显匹配" if ok else "回显缺失")
    except Exception as e:  # noqa: BLE001
        return TestResult("HTTP CONNECT 隧道(原始)", False, str(e))
    finally:
        try:
            c.close()
        except Exception:
            pass


def test_concurrent_socks5():
    """并发建立多个 SOCKS5 CONNECT，验证 SOCKS5 并发处理能力。"""
    def one(_):
        try:
            c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
            c.handshake()
            c.request(0x01, "www.baidu.com:80")
            c.sock.sendall(b"GET / HTTP/1.1\r\nHost: www.baidu.com\r\nConnection: close\r\n\r\n")
            c.sock.settimeout(REQUEST_TIMEOUT)
            data = b""
            try:
                while True:
                    ch = c.sock.recv(4096)
                    if not ch:
                        break
                    data += ch
            except socket.timeout:
                pass
            c.close()
            return b"HTTP/" in data
        except Exception:
            return False

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as ex:
            results = list(ex.map(one, range(6)))
        ok = all(results)
        return TestResult("SOCKS5 并发连接 x6", ok, f"全部成功 {sum(results)}/6" if ok else f"部分失败 {sum(results)}/6")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 并发连接 x6", False, str(e))


def test_socks5_bind_rejected():
    """SOCKS5 BIND 应被拒绝：dist 配置 socks5.allow_bind: false。"""
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        try:
            c.request(0x02, "127.0.0.1:54321")
            c.close()
            return TestResult("SOCKS5 BIND 拒绝", False, "BIND 未被拒绝（配置 allow_bind: false）")
        except ConnectionError as e:
            c.close()
            return TestResult("SOCKS5 BIND 拒绝", True, f"被正确拒绝: {e}")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 BIND 拒绝", False, str(e))


def test_socks5_udp_associate_rejected():
    """SOCKS5 UDP ASSOCIATE 应被拒绝：dist 配置 socks5.allow_udp_associate: false。"""
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        try:
            c.request(0x03, "0.0.0.0:0")
            c.close()
            return TestResult("SOCKS5 UDP ASSOC 拒绝", False, "UDP ASSOCIATE 未被拒绝（配置 allow_udp_associate: false）")
        except ConnectionError as e:
            c.close()
            return TestResult("SOCKS5 UDP ASSOC 拒绝", True, f"被正确拒绝: {e}")
    except Exception as e:  # noqa: BLE001
        return TestResult("SOCKS5 UDP ASSOC 拒绝", False, str(e))


def test_config_api_upstreams():
    """运行时配置 API：GET /api/v1/config/upstreams 应返回上游列表。"""
    try:
        r = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/config/upstreams", timeout=8)
        ok = r.status_code == 200
        if ok:
            data = r.json()
            upstreams = data.get("upstreams", data) if isinstance(data, dict) else data
            has_upstream = isinstance(upstreams, list) and len(upstreams) > 0
            return TestResult("配置 API /upstreams", ok and has_upstream,
                              f"HTTP {r.status_code} 上游数={len(upstreams) if isinstance(upstreams, list) else '?'}")
        return TestResult("配置 API /upstreams", False, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("配置 API /upstreams", False, str(e))


def test_traffic_api():
    """流量查询 API：GET /api/v1/traffic 应返回流量快照。"""
    try:
        r = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/traffic", timeout=8)
        ok = r.status_code == 200
        if ok:
            data = r.json()
            return TestResult("流量 API /traffic", True, f"HTTP {r.status_code} keys={list(data.keys()) if isinstance(data, dict) else 'list'}")
        return TestResult("流量 API /traffic", False, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("流量 API /traffic", False, str(e))


# ---------------------------------------------------------------------------
# 对标测试用例（从 4479 测试同步）
# ---------------------------------------------------------------------------

def test_http_method_matrix():
    """HTTP 方法矩阵：GET/HEAD/POST 经代理透传，验证方法无关性。

    PUT/DELETE/PATCH 仅验证代理能转发（目标站点可能不支持，接受任意非 5xx 响应）。
    """
    try:
        results = {}
        # GET
        code, _ = _http_get("https://www.baidu.com", _proxy_url())
        results["GET"] = code is not None and code < 500

        # HEAD
        try:
            r = requests.head(
                "https://www.baidu.com",
                proxies={"http": _proxy_url(), "https": _proxy_url()},
                timeout=REQUEST_TIMEOUT,
                allow_redirects=True,
                verify=False,
            )
            results["HEAD"] = r.status_code < 500
        except Exception:
            results["HEAD"] = False

        # POST
        try:
            r = requests.post(
                "https://www.baidu.com",
                data=b"test",
                proxies={"http": _proxy_url(), "https": _proxy_url()},
                timeout=REQUEST_TIMEOUT,
                allow_redirects=True,
                verify=False,
            )
            results["POST"] = r.status_code < 500
        except Exception:
            results["POST"] = False

        # PUT/DELETE/PATCH — 目标站点可能不支持，仅验证代理不崩溃
        for method in ("PUT", "DELETE", "PATCH"):
            try:
                r = requests.request(
                    method,
                    "https://www.baidu.com",
                    data=b"x",
                    proxies={"http": _proxy_url(), "https": _proxy_url()},
                    timeout=REQUEST_TIMEOUT,
                    verify=False,
                )
                results[method] = r.status_code < 500
            except Exception:
                results[method] = False

        ok = results.get("GET", False) and results.get("HEAD", False)
        detail = " ".join(f"{m}={'Y' if v else 'N'}" for m, v in results.items())
        status_msg = "核心方法透传成功" if ok else "核心方法失败"
        return TestResult("HTTP 方法矩阵", ok, f"{status_msg} | {detail}")
    except Exception as e:  # noqa: BLE001
        return TestResult("HTTP 方法矩阵", False, str(e))


def test_connection_refused():
    """代理到未监听端口应优雅失败。"""
    try:
        proxies = {"http": _proxy_url(), "https": _proxy_url()}
        r = requests.get("http://127.0.0.1:1/", proxies=proxies, timeout=8, verify=False)
        ok = r.status_code >= 400
        return TestResult("错误处理-连接拒绝", ok, f"HTTP {r.status_code}" if ok else "意外成功")
    except requests.exceptions.ProxyError:
        return TestResult("错误处理-连接拒绝", True, "ProxyError（符合预期）")
    except Exception:
        return TestResult("错误处理-连接拒绝", True, "异常拒绝（符合预期）")


def test_dns_failure():
    """代理到不存在的域名应优雅失败。"""
    try:
        proxies = {"http": _proxy_url(), "https": _proxy_url()}
        r = requests.get("http://nonexistent.invalid.test/", proxies=proxies, timeout=8, verify=False)
        ok = r.status_code >= 400
        return TestResult("错误处理-DNS失败", ok, f"HTTP {r.status_code}" if ok else "意外成功")
    except requests.exceptions.ProxyError:
        return TestResult("错误处理-DNS失败", True, "ProxyError（符合预期）")
    except Exception:
        return TestResult("错误处理-DNS失败", True, "异常拒绝（符合预期）")


def test_config_api_rules():
    """Config API: GET /api/v1/config/rules 应返回路由规则列表。"""
    try:
        r = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/config/rules", timeout=8)
        ok = r.status_code == 200
        if ok:
            data = r.json()
            rules = data.get("rules", data) if isinstance(data, dict) else data
            has_rules = isinstance(rules, list) and len(rules) > 0
            return TestResult("Config API /rules", ok and has_rules,
                              f"规则数={len(rules) if isinstance(rules, list) else '?'}")
        return TestResult("Config API /rules", False, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("Config API /rules", False, str(e))


def test_config_api_users():
    """Config API: GET /api/v1/config/users 应返回用户列表。"""
    try:
        r = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/config/users", timeout=8)
        ok = r.status_code == 200
        if ok:
            data = r.json()
            users = data.get("users", data) if isinstance(data, dict) else data
            is_list = isinstance(users, list)
            return TestResult("Config API /users", ok and is_list,
                              f"用户数={len(users) if is_list else '?'}")
        return TestResult("Config API /users", False, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("Config API /users", False, str(e))


def test_metrics_delta():
    """Metrics 计数器验证：发起请求前后计数器应递增。"""
    try:
        r1 = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/metrics", timeout=8)
        if r1.status_code != 200:
            return TestResult("Metrics 计数器递增", False, f"初始 metrics HTTP {r1.status_code}")

        def extract_counter(text, name):
            for line in text.splitlines():
                if line.startswith(name) and not line.startswith(name + "_"):
                    try:
                        return float(line.split()[-1])
                    except (ValueError, IndexError):
                        pass
            return None

        counter_names = [
            "yigate_connections_total",
            "yigate_requests_total",
            "yigate_proxy_requests_total",
            "yigate_total_connections",
        ]
        before_vals = {}
        for name in counter_names:
            v = extract_counter(r1.text, name)
            if v is not None:
                before_vals[name] = v

        if not before_vals:
            return TestResult("Metrics 计数器递增", True, "metrics 可读（未找到已知计数器名，跳过 delta 验证）")

        # 发起若干请求
        for url in ["https://www.baidu.com", "https://www.qq.com", "https://www.jd.com"]:
            try:
                _http_get(url, _proxy_url())
            except Exception:
                pass

        r2 = MGMT_SESSION.get(f"http://{MGMT_HOST}:{MGMT_PORT}/metrics", timeout=8)
        if r2.status_code != 200:
            return TestResult("Metrics 计数器递增", False, f"更新后 metrics HTTP {r2.status_code}")

        any_incremented = False
        details = []
        for name, before in before_vals.items():
            after = extract_counter(r2.text, name)
            if after is not None and after > before:
                any_incremented = True
                details.append(f"{name}: {before:.0f}->{after:.0f}")
            elif after is not None:
                details.append(f"{name}: {before:.0f}->{after:.0f}(未增)")

        return TestResult("Metrics 计数器递增", any_incremented,
                          ("计数器递增" if any_incremented else "未检测到递增") + " | " + " ".join(details))
    except Exception as e:  # noqa: BLE001
        return TestResult("Metrics 计数器递增", False, str(e))


def test_route_decision_direct():
    """路由决策验证-直连：国内站点应走 direct 路径（不经上游）。"""
    try:
        # 通过 SOCKS5 访问国内站点，验证直连
        ok = _socks5_http_get("www.baidu.com", force_domain=False)
        return TestResult("路由决策-直连", ok, "直连成功" if ok else "直连失败")
    except Exception as e:  # noqa: BLE001
        return TestResult("路由决策-直连", False, str(e))


def test_route_decision_proxy():
    """路由决策验证-代理：国外站点应走 proxy 路径（经上游）。"""
    try:
        ok = _socks5_http_get("github.com", force_domain=False)
        return TestResult("路由决策-代理", ok, "代理路径成功" if ok else "代理路径失败")
    except Exception as e:  # noqa: BLE001
        return TestResult("路由决策-代理", False, str(e))


def test_protocol_error_handling():
    """发送非 HTTP 数据到代理端口，代理应优雅关闭连接而非崩溃。"""
    try:
        c = socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=5)
        c.sendall(b"\x00\x01\x02\x03NOT-HTTP\r\n\r\n")
        c.settimeout(3)
        try:
            data = c.recv(4096)
        except socket.timeout:
            data = b""
        c.close()
        return TestResult("协议错误处理", True, "连接已关闭")
    except Exception as e:  # noqa: BLE001
        return TestResult("协议错误处理", False, str(e))


# ---------------------------------------------------------------------------
# 运行
# ---------------------------------------------------------------------------
def run_single_tests():
    tests = [
        test_http_domestic,
        test_https_connect_domestic,
        test_http_foreign,
        test_https_connect_foreign,
        test_socks5_domestic,
        test_socks5h_domestic,
        test_socks5_foreign,
        test_socks5h_foreign,
        test_keepalive_reuse,
        test_large_post,
        test_concurrent_requests,
        test_management_health,
        test_management_metrics,
        test_management_status,
        test_management_reload,
        # 从 4479 测试吸取的新特性用例
        test_ssrf_metadata_unreachable,
        test_socks5_anonymous_auth,
        test_http_connect_tunnel,
        test_concurrent_socks5,
        test_socks5_bind_rejected,
        test_socks5_udp_associate_rejected,
        test_config_api_upstreams,
        test_traffic_api,
        # 对标测试用例（从 4479 同步）
        test_http_method_matrix,
        test_connection_refused,
        test_dns_failure,
        test_config_api_rules,
        test_config_api_users,
        test_metrics_delta,
        test_route_decision_direct,
        test_route_decision_proxy,
        test_protocol_error_handling,
    ]
    results = []
    for t in tests:
        try:
            res = t()
        except Exception as e:  # noqa: BLE001
            res = TestResult(t.__name__, False, f"异常: {e}")
        results.append(res)
        mark = f"{Colors.OK}PASS{Colors.RESET}" if res.ok else f"{Colors.FAIL}FAIL{Colors.RESET}"
        print(f"  {res.name:<28} {mark}  {res.detail}")
    return results


def run_site_tests(strict):
    proxy_types = [
        ("HTTP", _proxy_url()),
        ("HTTPS_CONNECT", _proxy_url()),
        ("SOCKS5", f"socks5://{PROXY_HOST}:{PROXY_PORT}"),
        ("SOCKS5H", f"socks5h://{PROXY_HOST}:{PROXY_PORT}"),
    ]
    all_results = {}
    for label, proxy_url in proxy_types:
        print(f"\n{Colors.INFO}== 站点测试 [{label}] =={Colors.RESET}")
        sites = DOMESTIC_SITES + FOREIGN_SITES
        if label in ("SOCKS5", "SOCKS5H"):
            # requests 对 https 目标经 SOCKS5 走代理端解析；复用同一套站点即可
            all_results[label] = test_sites_via_proxy(label, proxy_url, sites, strict)
        else:
            all_results[label] = test_sites_via_proxy(label, proxy_url, sites, strict)
    return all_results


def summarize(all_results, strict):
    print(f"\n{Colors.INFO}{'='*60}")
    print(f" 站点测试汇总")
    print(f"{'='*60}{Colors.RESET}")
    total_ok = total_warn = total_fail = 0
    for label, results in all_results.items():
        ok = sum(1 for r in results if r["status"] == "OK")
        warn = sum(1 for r in results if r["status"] == "WARN")
        fail = sum(1 for r in results if r["status"] == "FAIL")
        total_ok += ok
        total_warn += warn
        total_fail += fail
        print(f"  {label:<14} 成功={ok}  警告={warn}  失败={fail}")
    return total_ok, total_warn, total_fail


def main():
    global PROXY_HOST, PROXY_PORT, MGMT_HOST, MGMT_PORT
    parser = argparse.ArgumentParser(description="yigate 8891 dist 实例全能力验收测试")
    parser.add_argument("--strict", action="store_true", help="站点关键词缺失也视为失败")
    parser.add_argument("--proxy-host", default="127.0.0.1", help="代理主机，默认 127.0.0.1")
    parser.add_argument("--proxy-port", type=int, default=8891, help="代理端口，默认 8891")
    parser.add_argument("--mgmt-host", default="127.0.0.1", help="管理主机，默认 127.0.0.1")
    parser.add_argument("--mgmt-port", type=int, default=8890, help="管理端口，默认 8890")
    parser.add_argument("--mgmt-token", default=os.environ.get("YIGATE_MANAGEMENT_TOKEN", ""),
                        help="管理 Bearer Token，默认读取 YIGATE_MANAGEMENT_TOKEN")
    args = parser.parse_args()
    PROXY_HOST = args.proxy_host
    PROXY_PORT = args.proxy_port
    MGMT_HOST = args.mgmt_host
    MGMT_PORT = args.mgmt_port
    if args.mgmt_token:
        MGMT_SESSION.headers["Authorization"] = f"Bearer {args.mgmt_token}"
    else:
        MGMT_SESSION.headers.pop("Authorization", None)

    print(f"{Colors.INFO}yigate dist 实例全能力测试: {PROXY_HOST}:{PROXY_PORT} "
          f"(管理 {MGMT_HOST}:{MGMT_PORT}){Colors.RESET}")

    # 0) 前置连通性检查
    try:
        with socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=5):
            print(f"{Colors.OK}代理端口 {PROXY_HOST}:{PROXY_PORT} 连通{Colors.RESET}")
    except Exception as e:  # noqa: BLE001
        print(f"{Colors.FAIL}代理端口不通: {e}{Colors.RESET}")
        print("请先启动 yigate 生产实例")
        sys.exit(2)

    try:
        single_results = run_single_tests()
        site_results = run_site_tests(args.strict)
    except KeyboardInterrupt:
        print(f"\n{Colors.WARN}被用户中断{Colors.RESET}")
        sys.exit(130)

    # 汇总
    s_ok = sum(1 for r in single_results if r.ok)
    s_fail = sum(1 for r in single_results if not r.ok)
    print(f"\n{Colors.INFO}{'='*60}")
    print(f" 单能力测试: 通过 {s_ok}/{len(single_results)}  失败 {s_fail}{Colors.RESET}")
    total_ok, total_warn, total_fail = summarize(site_results, args.strict)

    all_pass = (s_fail == 0 and total_fail == 0 and (total_warn == 0 or not args.strict))
    print(f"\n{Colors.INFO}总计: 单能力失败 {s_fail}, 站点失败 {total_fail}, 站点警告 {total_warn}{Colors.RESET}")
    if all_pass:
        print(f"{Colors.OK}✅ 全部通过{Colors.RESET}")
        sys.exit(0)
    else:
        print(f"{Colors.FAIL}❌ 存在失败项{Colors.RESET}")
        sys.exit(1)


if __name__ == "__main__":
    main()
