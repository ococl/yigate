#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
k5pw 8891 端口（dist 生产实例）全能力验收测试脚本。

本脚本针对运行中的 dist 实例（dist/k5/config.yml，监听 0.0.0.0:8891，
上游 socks5h://10.2.3.150:7897，管理端口 127.0.0.1:8890）进行端到端验证：
  - 国内站点（geosite CN -> 直连）
  - 国外站点（github/google 等 -> 经上游 7897 代理）
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

与 test_proxy_4479.py（自包含、本地 fixture）不同，本脚本依赖真实网络与
上游 7897，验证的是 dist 实例的真实路由与出网能力。

运行前请确保：
  1) dist 容器已用最新镜像启动（docker compose -f dist/docker-compose.yml up -d）
  2) 上游 10.2.3.150:7897 可达

用法：
    python3 test_proxy_8891.py
    python3 test_proxy_8891.py --strict        # 站点关键词缺失也视为失败
    python3 test_proxy_8891.py --proxy-port 8891 --mgmt-port 8890
"""

import argparse
import concurrent.futures
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
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}{path}", timeout=8)
        return r.status_code, r.text
    except Exception as e:  # noqa: BLE001
        return None, str(e)


def test_management_health():
    code, text = _mgmt_get("/health")
    ok = code == 200 and "up" in text
    return TestResult("管理接口 /health", ok, f"HTTP {code}" if ok else f"失败 HTTP {code} {text[:40]}")


def test_management_metrics():
    code, text = _mgmt_get("/metrics")
    ok = code == 200 and "k5pw" in text
    return TestResult("管理接口 /metrics", ok, f"HTTP {code} len={len(text)}" if ok else f"失败 HTTP {code}")


def test_management_status():
    code, text = _mgmt_get("/api/v1/status")
    ok = code == 200 and "listen" in text
    return TestResult("管理接口 /api/v1/status", ok, f"HTTP {code}" if ok else f"失败 HTTP {code}")


def test_management_reload():
    try:
        r = requests.post(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/reload", timeout=8)
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
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/config/upstreams", timeout=8)
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
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/traffic", timeout=8)
        ok = r.status_code == 200
        if ok:
            data = r.json()
            return TestResult("流量 API /traffic", True, f"HTTP {r.status_code} keys={list(data.keys()) if isinstance(data, dict) else 'list'}")
        return TestResult("流量 API /traffic", False, f"HTTP {r.status_code}")
    except Exception as e:  # noqa: BLE001
        return TestResult("流量 API /traffic", False, str(e))


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
    global PROXY_PORT, MGMT_PORT
    parser = argparse.ArgumentParser(description="k5pw 8891 dist 实例全能力验收测试")
    parser.add_argument("--strict", action="store_true", help="站点关键词缺失也视为失败")
    parser.add_argument("--proxy-port", type=int, default=8891, help="代理端口，默认 8891")
    parser.add_argument("--mgmt-port", type=int, default=8890, help="管理端口，默认 8890")
    parser.add_argument("--proxy-host", default="127.0.0.1", help="代理主机，默认 127.0.0.1")
    args = parser.parse_args()
    PROXY_PORT = args.proxy_port
    MGMT_PORT = args.mgmt_port

    print(f"{Colors.INFO}k5pw dist 实例全能力测试: {PROXY_HOST}:{PROXY_PORT} "
          f"(管理 {MGMT_HOST}:{MGMT_PORT}){Colors.RESET}")

    # 0) 前置连通性检查
    try:
        with socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=5):
            print(f"{Colors.OK}代理端口 {PROXY_HOST}:{PROXY_PORT} 连通{Colors.RESET}")
    except Exception as e:  # noqa: BLE001
        print(f"{Colors.FAIL}代理端口不通: {e}{Colors.RESET}")
        print("请先启动 dist 实例：docker compose -f dist/docker-compose.yml up -d")
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
