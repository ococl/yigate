#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
k5pw 代理服务器全能力测试脚本

覆盖能力：
- TCP 端口连通性
- HTTP/HTTPS 代理（HTTP GET/POST、CONNECT 隧道）
- SOCKS5 代理（CONNECT、BIND、UDP ASSOCIATE）
- SOCKS5 远程 DNS（域名地址类型）
- SOCKS5 匿名认证协商
- 直连/代理/auto 路由分流（外部站点验证）
- 上游级联失败的本地回退（BIND/UDP ASSOCIATE  implicitly 验证）

运行前请确保 k5pw 已监听 127.0.0.1:4479。
"""

import socket
import threading
import time
import struct
import requests
import sys
import urllib3
from urllib.parse import urlparse

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

PROXY_HOST = "127.0.0.1"
PROXY_PORT = 4479

# 国内站点：默认配置走 direct（geosite CN / ip_cidr）
DOMESTIC_SITES = [
    {"name": "百度", "url": "https://www.baidu.com", "keyword": "百度一下", "expected": "direct"},
    {"name": "腾讯QQ", "url": "https://www.qq.com", "keyword": "腾讯", "expected": "direct"},
    {"name": "淘宝", "url": "https://www.taobao.com", "keyword": "淘宝", "expected": "direct"},
    {"name": "京东", "url": "https://www.jd.com", "keyword": "京东", "expected": "direct"},
]

# 国外站点：默认配置走 proxy（domain_suffix / 默认 auto）
FOREIGN_SITES = [
    {"name": "Google", "url": "https://www.google.com", "keyword": "Google", "expected": "proxy"},
    {"name": "GitHub", "url": "https://github.com", "keyword": "GitHub", "expected": "proxy"},
    {"name": "YouTube", "url": "https://www.youtube.com", "keyword": "YouTube", "expected": "proxy"},
]

TEST_SITES = DOMESTIC_SITES + FOREIGN_SITES


class Colors:
    OK = "\033[92m"
    WARN = "\033[93m"
    FAIL = "\033[91m"
    INFO = "\033[94m"
    RESET = "\033[0m"


class TestResult:
    def __init__(self, name, ok, message="", details=""):
        self.name = name
        self.ok = ok
        self.message = message
        self.details = details

    def __str__(self):
        if self.ok:
            color = Colors.OK
            status = "OK"
        elif self.message.startswith("WARN"):
            color = Colors.WARN
            status = "WARN"
        else:
            color = Colors.FAIL
            status = "FAIL"
        detail = f" | {self.details}" if self.details else ""
        return f"{color}{status}{Colors.RESET}  {self.name:<40} {self.message}{detail}"


def print_header(title):
    print(f"\n{Colors.INFO}{'='*60}")
    print(f" {title}")
    print(f"{'='*60}{Colors.RESET}")


# ---------------------------------------------------------------------------
# 通用网络工具
# ---------------------------------------------------------------------------

def resolve_ipv4(host):
    """解析域名到 IPv4，失败返回 None。"""
    try:
        return socket.getaddrinfo(host, None, socket.AF_INET)[0][4][0]
    except Exception:
        return None


def tcp_echo_server():
    """返回一个本地 TCP echo 服务器 (socket, addr)。"""
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", 0))
    s.listen(1)
    return s, s.getsockname()


def udp_echo_server():
    """返回一个本地 UDP echo 服务器 (socket, addr)。"""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.bind(("127.0.0.1", 0))
    return s, s.getsockname()


def run_in_thread(fn, *args, **kwargs):
    t = threading.Thread(target=fn, args=args, kwargs=kwargs, daemon=True)
    t.start()
    return t


# ---------------------------------------------------------------------------
# SOCKS5 原始协议实现
# ---------------------------------------------------------------------------

class SOCKS5RawClient:
    """最小 SOCKS5 客户端，支持 CONNECT/BIND/UDP ASSOCIATE。"""

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
        ip = socket.inet_pton(socket.AF_INET, host) if not force_domain else None
        if ip is not None:
            return bytes([0x01]) + ip + struct.pack(">H", port)
        try:
            ip6 = socket.inet_pton(socket.AF_INET6, host)
            return bytes([0x04]) + ip6 + struct.pack(">H", port)
        except OSError:
            host_b = host.encode()
            return bytes([0x03, len(host_b)]) + host_b + struct.pack(">H", port)

    def request(self, cmd, target, force_domain=False):
        """发送 SOCKS5 请求，返回回复中的地址字符串。"""
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
        atyp = header[3]
        if atyp == 0x01:
            addr_b = self._read_exact(4)
            host = socket.inet_ntop(socket.AF_INET, addr_b)
        elif atyp == 0x03:
            l = self._read_exact(1)[0]
            host = self._read_exact(l).decode()
        elif atyp == 0x04:
            addr_b = self._read_exact(16)
            host = socket.inet_ntop(socket.AF_INET6, addr_b)
        else:
            raise ConnectionError(f"unsupported reply atyp {atyp}")
        port = struct.unpack(">H", self._read_exact(2))[0]
        return f"{host}:{port}"


def build_socks5_udp_header(target, payload=b""):
    """构造 SOCKS5 UDP 请求头 + 负载。"""
    host, port_s = target.rsplit(":", 1)
    port = int(port_s)
    try:
        ip = socket.inet_pton(socket.AF_INET, host)
        header = bytes([0x00, 0x00, 0x00, 0x01]) + ip
    except OSError:
        host_b = host.encode()
        header = bytes([0x00, 0x00, 0x00, 0x03, len(host_b)]) + host_b
    header += struct.pack(">H", port)
    return header + payload


def parse_socks5_udp_header(data):
    """解析 SOCKS5 UDP 头，返回 (target, frag, payload_offset, ok)。"""
    if len(data) < 10 or data[0] != 0x00 or data[1] != 0x00:
        return None, 0, 0, False
    frag = data[2]
    atyp = data[3]
    off = 4
    if atyp == 0x01:
        if len(data) < off + 4 + 2:
            return None, 0, 0, False
        host = socket.inet_ntop(socket.AF_INET, data[off:off+4])
        off += 4
    elif atyp == 0x03:
        if len(data) < off + 1:
            return None, 0, 0, False
        l = data[off]
        off += 1
        if len(data) < off + l + 2:
            return None, 0, 0, False
        host = data[off:off+l].decode()
        off += l
    elif atyp == 0x04:
        if len(data) < off + 16 + 2:
            return None, 0, 0, False
        host = socket.inet_ntop(socket.AF_INET6, data[off:off+16])
        off += 16
    else:
        return None, 0, 0, False
    port = struct.unpack(">H", data[off:off+2])[0]
    off += 2
    return f"{host}:{port}", frag, off, True


def addr_to_tuple(addr_str):
    """将 host:port 字符串转为 (host, port) 元组。"""
    host, port_s = addr_str.rsplit(":", 1)
    if host.startswith("[") and host.endswith("]"):
        host = host[1:-1]
    return host, int(port_s)


# ---------------------------------------------------------------------------
# 测试用例
# ---------------------------------------------------------------------------

def test_tcp_connect():
    try:
        with socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=5):
            return TestResult("TCP 端口连通", True, "端口可连接")
    except Exception as e:
        return TestResult("TCP 端口连通", False, f"连接失败: {e}")


def test_http_get():
    try:
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = requests.get(
            "http://httpbin.org/get",
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        return TestResult("HTTP 代理 GET", True, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("HTTP 代理 GET", False, str(e))


def test_http_post():
    try:
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        payload = {"k5pw": "test"}
        r = requests.post(
            "http://httpbin.org/post",
            json=payload,
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        ok = payload["k5pw"] in r.text
        return TestResult("HTTP 代理 POST", ok, f"HTTP {r.status_code}", "" if ok else "回显缺少 payload")
    except Exception as e:
        return TestResult("HTTP 代理 POST", False, str(e))


def test_http_connect():
    """通过 HTTP CONNECT 建立到 https://www.baidu.com 的隧道。"""
    try:
        proxies = {"https": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = requests.get(
            "https://www.baidu.com",
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
            verify=False,
        )
        keyword_found = "baidu" in r.text.lower() or r.status_code == 200
        return TestResult("HTTP CONNECT 隧道", keyword_found, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("HTTP CONNECT 隧道", False, str(e))


def test_socks5_connect_domain():
    """SOCKS5 CONNECT 域名地址类型，验证远程 DNS 与 CONNECT 能力。"""
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, "www.baidu.com:80", force_domain=True)
        c.sock.sendall(b"GET / HTTP/1.1\r\nHost: www.baidu.com\r\nConnection: close\r\n\r\n")
        c.sock.shutdown(socket.SHUT_WR)
        data = b""
        while True:
            chunk = c.sock.recv(4096)
            if not chunk:
                break
            data += chunk
        c.close()
        ok = data.startswith(b"HTTP/1.1 200") or data.startswith(b"HTTP/1.0 200") or b"baidu" in data.lower()
        return TestResult("SOCKS5 CONNECT 域名", ok, "HTTP 200" if ok else "未收到 200")
    except Exception as e:
        return TestResult("SOCKS5 CONNECT 域名", False, str(e))


def test_socks5_connect_ip():
    """SOCKS5 CONNECT IPv4 目标，触发 socks5h_redirect 的 IP 嗅探/重定向路径。"""
    try:
        # Cloudflare 1.1.1.1:443，忽略证书校验，只验证 CONNECT 成功与 TLS 握手完成
        ip = resolve_ipv4("one.one.one.one")
        if not ip:
            ip = "1.1.1.1"
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, f"{ip}:443")
        import ssl
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        with ctx.wrap_socket(c.sock, server_hostname="") as ssock:
            ssock.sendall(b"GET / HTTP/1.1\r\nHost: one.one.one.one\r\nConnection: close\r\n\r\n")
            data = ssock.recv(4096)
        ok = len(data) > 0
        return TestResult("SOCKS5 CONNECT IPv4", ok, "TLS 握手完成" if ok else "无数据")
    except Exception as e:
        return TestResult("SOCKS5 CONNECT IPv4", False, str(e))


def test_socks5_bind():
    """SOCKS5 BIND：本地监听回退路径（上游级联失败会回退到本地）。"""
    # target 仅用于 IP 白名单校验，端口不会被实际连接；使用 loopback 上任意端口
    target = "127.0.0.1:54321"
    bound_addr_holder = [None]
    peer_ready = threading.Event()
    result_holder = [None]

    def connect_peer():
        peer_ready.wait(timeout=10)
        if not bound_addr_holder[0]:
            return
        try:
            peer = socket.create_connection(addr_to_tuple(bound_addr_holder[0]), timeout=10)
            data = peer.recv(1024)
            peer.sendall(data)
            peer.close()
            result_holder[0] = ("ok", data)
        except Exception as e:
            result_holder[0] = ("err", str(e))

    try:
        connector = run_in_thread(connect_peer)

        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        bound_addr = c.request(0x02, target)
        bound_addr_holder[0] = bound_addr
        peer_ready.set()

        # 读取第二次回复（目标连接到达通知）
        c._read_reply()

        c.sock.sendall(b"HELLO_BIND")
        resp = c.sock.recv(1024)
        c.close()

        connector.join(timeout=15)
        ok = resp == b"HELLO_BIND" and result_holder[0] and result_holder[0][1] == b"HELLO_BIND"
        return TestResult("SOCKS5 BIND", ok, "数据往返成功" if ok else "回显不匹配")
    except Exception as e:
        return TestResult("SOCKS5 BIND", False, str(e))


def test_socks5_udp_associate():
    """SOCKS5 UDP ASSOCIATE：本地 UDP 中继回退路径。"""
    server, server_addr = udp_echo_server()

    def echo_loop():
        try:
            while True:
                server.settimeout(1)
                data, addr = server.recvfrom(65535)
                server.sendto(data, addr)
        except socket.timeout:
            pass
        except Exception:
            pass

    try:
        run_in_thread(echo_loop)

        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        relay_addr = c.request(0x03, "0.0.0.0:0")

        udp_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        udp_sock.settimeout(10)
        target = f"127.0.0.1:{server_addr[1]}"
        packet = build_socks5_udp_header(target, b"HELLO_UDP")
        udp_sock.sendto(packet, addr_to_tuple(relay_addr))

        resp, _ = udp_sock.recvfrom(65535)
        parsed_target, frag, off, ok = parse_socks5_udp_header(resp)
        payload = resp[off:]
        c.close()
        udp_sock.close()

        success = ok and frag == 0 and payload == b"HELLO_UDP"
        return TestResult(
            "SOCKS5 UDP ASSOCIATE",
            success,
            "UDP 回显成功" if success else f"解析失败/负载不匹配/frag={frag}",
        )
    except Exception as e:
        return TestResult("SOCKS5 UDP ASSOCIATE", False, str(e))
    finally:
        try:
            server.close()
        except Exception:
            pass


def test_socks5_anonymous_auth():
    """SOCKS5 匿名认证：配置无用户时应接受匿名。"""
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, "www.baidu.com:80", force_domain=True)
        c.close()
        return TestResult("SOCKS5 匿名认证", True, "接受匿名")
    except Exception as e:
        return TestResult("SOCKS5 匿名认证", False, str(e))


def test_sites_with_proxy(proxy_label, proxy_url, scheme="http"):
    """使用指定代理批量访问测试站点，返回结果列表。"""
    results = []
    for site in TEST_SITES:
        # 测试 URL 都是 https://，因此无论入口代理类型是 http/socks5/socks5h，都需要为 https 设置代理
        proxies = {"http": proxy_url, "https": proxy_url}
        try:
            r = requests.get(
                site["url"],
                proxies=proxies,
                timeout=15,
                headers={"User-Agent": "k5pw-test/1.0"},
                allow_redirects=True,
                verify=False,
            )
            keyword = site.get("keyword", "")
            found = keyword in r.text
            status = "OK" if found else "WARN(无关键词)"
            results.append({
                "name": site["name"],
                "expected": site.get("expected", "unknown"),
                "status": status,
                "code": r.status_code,
            })
        except Exception as e:
            results.append({
                "name": site["name"],
                "expected": site.get("expected", "unknown"),
                "status": f"FAIL: {e}",
                "code": None,
            })
    return results


# ---------------------------------------------------------------------------
# 主函数
# ---------------------------------------------------------------------------

def wait_for_upstream_recovery(timeout=45):
    """等待上游从 BIND/UDP 测试中恢复。代理健康检查周期默认 30s，这里最多轮询 45s。"""
    probe_url = "https://github.com"
    proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}", "https": f"http://{PROXY_HOST}:{PROXY_PORT}"}
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            r = requests.get(probe_url, proxies=proxies, timeout=10, verify=False)
            if r.status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(2)
    return False


def run_single_tests():
    print_header("单能力测试")
    tests = [
        test_tcp_connect,
        test_http_get,
        test_http_post,
        test_http_connect,
        test_socks5_connect_domain,
        test_socks5_connect_ip,
        test_socks5_bind,
        test_socks5_udp_associate,
        test_socks5_anonymous_auth,
    ]
    results = []
    for t in tests:
        res = t()
        results.append(res)
        print(f"  {res}")
    return results


def run_site_tests():
    print_header("路由分流测试（外部站点）")
    print(f"{Colors.INFO}等待上游健康检查恢复...{Colors.RESET}")
    if wait_for_upstream_recovery():
        print(f"{Colors.OK}上游已恢复{Colors.RESET}")
    else:
        print(f"{Colors.WARN}上游尚未恢复，站点测试可能因无可用上游而失败{Colors.RESET}")
    all_results = {}
    for label, url in [
        ("HTTP", f"http://{PROXY_HOST}:{PROXY_PORT}"),
        ("HTTPS_CONNECT", f"http://{PROXY_HOST}:{PROXY_PORT}"),
        ("SOCKS5", f"socks5://{PROXY_HOST}:{PROXY_PORT}"),
        ("SOCKS5H", f"socks5h://{PROXY_HOST}:{PROXY_PORT}"),
    ]:
        scheme = "https" if label == "HTTPS_CONNECT" else "http"
        print(f"\n{Colors.INFO}代理类型: {label} ({url}){Colors.RESET}")
        site_results = test_sites_with_proxy(label, url, scheme=scheme)
        all_results[label] = site_results
        for r in site_results:
            expected = r.get("expected", "?")
            if r["status"].startswith("OK"):
                color = Colors.OK
            elif r["status"].startswith("WARN"):
                color = Colors.WARN
            else:
                color = Colors.FAIL
            print(f"  {r['name']:<12} expected={expected:<8} {color}{r['status']}{Colors.RESET}  code={r['code']}")
    return all_results


def summarize(single_results, site_results):
    print_header("汇总")
    ok = sum(1 for r in single_results if r.ok)
    fail = len(single_results) - ok
    print(f"  单能力测试: 成功={ok}  失败={fail}")
    for label, results in site_results.items():
        s_ok = sum(1 for r in results if r["status"].startswith("OK"))
        s_warn = sum(1 for r in results if r["status"].startswith("WARN"))
        s_fail = len(results) - s_ok - s_warn
        print(f"  站点测试 {label:<14}: 成功={s_ok}  警告={s_warn}  失败={s_fail}")
    print()
    if fail == 0:
        print(f"{Colors.OK}所有单能力测试通过。{Colors.RESET}")
    else:
        print(f"{Colors.FAIL}存在失败的单能力测试，请检查代理日志。{Colors.RESET}")


def main():
    print(f"{Colors.INFO}k5pw 代理服务器全能力测试: {PROXY_HOST}:{PROXY_PORT}{Colors.RESET}")
    print(f"{Colors.INFO}依赖: requests 需支持 socks5/socks5h（通常已安装 PySocks）{Colors.RESET}")

    single_results = run_single_tests()
    site_results = run_site_tests()
    summarize(single_results, site_results)

    failed = [r.name for r in single_results if not r.ok]
    if failed:
        print(f"\n{Colors.FAIL}失败项: {', '.join(failed)}{Colors.RESET}")
        sys.exit(1)


if __name__ == "__main__":
    main()
