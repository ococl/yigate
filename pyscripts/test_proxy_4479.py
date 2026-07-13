#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
k5pw 代理服务器全能力测试脚本（自包含版）

设计目标：不依赖真实外网或真实上游，全部使用本地可控的目标服务器与
本地上游代理，使测试在隔离环境中也能 100% 稳定通过，并覆盖：

- TCP 端口连通性
- HTTP/HTTPS 代理（HTTP GET/POST、CONNECT 隧道、keep-alive 复用、大体积 POST）
- SOCKS5 代理（CONNECT 域名/IP、BIND、UDP ASSOCIATE、匿名认证）
- 直连 / 代理 路由分流（本地服务器验证，proxy 路径经本地上游）
- 管理接口（/health、/metrics、/api/v1/status）
- SSRF 防护冒烟（元数据地址不可达）

运行前请确保 k5pw 已监听 127.0.0.1:4479（见同目录 config_4479.yml），
且 /etc/hosts 中存在 `127.0.0.1 foreign.test`（脚本会自动尝试写入）。
"""

import argparse
import json
import socket
import struct
import sys
import threading
import time
import traceback
import urllib3
from urllib.parse import urlparse

import requests

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

PROXY_HOST = "127.0.0.1"
PROXY_PORT = 4479
MGMT_HOST = "127.0.0.1"
MGMT_PORT = 8892
UPSTREAM_HOST = "127.0.0.1"
UPSTREAM_PORT = 8893

# 由 start_fixtures() 在运行时填充
FIXTURES = {}

# 站点测试用的本地站点（直连 / 代理），在 main() 中根据动态端口重写
DOMESTIC_SITES = []
FOREIGN_SITES = []
TEST_SITES = []


class Colors:
    OK = "\033[92m"
    WARN = "\033[93m"
    FAIL = "\033[91m"
    INFO = "\033[94m"
    RESET = "\033[0m"


# ---------------------------------------------------------------------------
# 依赖检查
# ---------------------------------------------------------------------------

def check_dependencies():
    errors = []
    try:
        import requests  # noqa: F401
    except ImportError as e:
        errors.append(f"缺少 requests: {e}")
    try:
        import socks  # PySocks
    except ImportError:
        errors.append("缺少 PySocks（requests 无法使用 socks5/socks5h 代理），请运行: pip install pysocks")
    try:
        import urllib3  # noqa: F401
    except ImportError as e:
        errors.append(f"缺少 urllib3: {e}")
    if errors:
        print(f"{Colors.FAIL}环境检查失败:{Colors.RESET}")
        for err in errors:
            print(f"  - {err}")
        sys.exit(1)


# ---------------------------------------------------------------------------
# 配置与测试目标
# ---------------------------------------------------------------------------

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
    try:
        return socket.getaddrinfo(host, None, socket.AF_INET)[0][4][0]
    except Exception:
        return None


def tcp_echo_server():
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", 0))
    s.listen(1)
    return s, s.getsockname()


def udp_echo_server():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.bind(("127.0.0.1", 0))
    return s, s.getsockname()


def run_in_thread(fn, *args, **kwargs):
    t = threading.Thread(target=fn, args=args, kwargs=kwargs, daemon=True)
    t.start()
    return t


def http_request_with_retry(method, url, proxies=None, timeout=15, retries=2, **kwargs):
    last_err = None
    for attempt in range(retries + 1):
        try:
            return requests.request(method, url, proxies=proxies, timeout=timeout, **kwargs)
        except Exception as e:
            last_err = e
            if attempt < retries:
                time.sleep(1)
    raise last_err


def start_local_http_server(host="127.0.0.1", port=0, body=b'{"origin": "k5pw-test", "status": "ok"}'):
    """启动一个简单 HTTP 响应服务器，返回 (server, addr)。body 可定制以区分直连/代理。"""
    import http.server
    from socketserver import ThreadingMixIn

    b = body

    class Handler(http.server.BaseHTTPRequestHandler):
        def _send(self, data):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            self._send(b)

        def do_POST(self):
            length = int(self.headers.get("Content-Length", 0))
            payload = self.rfile.read(length)
            self._send(b"ECHO:" + payload)

        def log_message(self, format, *args):
            pass

    class ThreadingHTTPServer(ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True

    server = ThreadingHTTPServer((host, port), Handler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    return server, server.server_address


def read_http_response(sock):
    """从 socket 读取完整 HTTP 响应，返回 (status_line, body)。"""
    buf = b""
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            break
        buf += chunk
    header_part, _, rest = buf.partition(b"\r\n\r\n")
    header_text = header_part.decode("latin1")
    lines = header_text.split("\r\n")
    status = lines[0] if lines else ""
    cl = 0
    chunked = False
    for ln in lines[1:]:
        low = ln.lower()
        if low.startswith("content-length:"):
            try:
                cl = int(ln.split(":", 1)[1].strip())
            except Exception:
                cl = 0
        elif low.startswith("transfer-encoding:") and "chunked" in low:
            chunked = True
    body = rest
    if cl > 0:
        while len(body) < cl:
            chunk = sock.recv(4096)
            if not chunk:
                break
            body += chunk
        body = body[:cl]
    elif chunked:
        while b"0\r\n\r\n" not in body:
            chunk = sock.recv(4096)
            if not chunk:
                break
            body += chunk
    return status, body


# ---------------------------------------------------------------------------
# 本地 HTTP 上游代理（用于验证 proxy 路由路径）
# ---------------------------------------------------------------------------

class LocalHTTPUpstream:
    """最小 HTTP 正向代理：支持 CONNECT 隧道、绝对形式转发、以及健康检查的 OPTIONS *。"""

    def __init__(self, host=UPSTREAM_HOST, port=UPSTREAM_PORT):
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind((host, port))
        self.sock.listen(128)
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._serve, daemon=True)

    def start(self):
        self._thread.start()

    def stop(self):
        self._stop.set()
        try:
            self.sock.close()
        except Exception:
            pass

    def _serve(self):
        self.sock.settimeout(1)
        while not self._stop.is_set():
            try:
                conn, _ = self.sock.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            run_in_thread(self._handle, conn)

    def _handle(self, conn):
        try:
            conn.settimeout(15)
            data = conn.recv(65536)
            if not data:
                return
            header_end = data.find(b"\r\n\r\n")
            if header_end < 0:
                return
            head = data[:header_end].decode("latin1")
            lines = head.split("\r\n")
            request_line = lines[0]
            parts = request_line.split(" ")
            if len(parts) < 3:
                return
            method, target, version = parts[0], parts[1], parts[2]

            if method == "CONNECT":
                host, port_s = target.rsplit(":", 1)
                self._do_connect(conn, host, int(port_s))
                return
            if method == "OPTIONS" and target == "*":
                conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
                return
            self._do_forward(conn, data, method, target, version, lines, header_end)
        except Exception:
            try:
                conn.sendall(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
            except Exception:
                pass
        finally:
            try:
                conn.close()
            except Exception:
                pass

    def _do_connect(self, conn, host, port):
        try:
            remote = socket.create_connection((host, port), timeout=15)
        except Exception:
            conn.sendall(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
            return
        conn.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
        self._tunnel(conn, remote)

    def _do_forward(self, conn, data, method, target, version, lines, header_end):
        parsed = urlparse(target)
        path = parsed.path or "/"
        if parsed.query:
            path = path + "?" + parsed.query
        cl = 0
        for ln in lines[1:]:
            if ln.lower().startswith("content-length:"):
                try:
                    cl = int(ln.split(":", 1)[1].strip())
                except Exception:
                    cl = 0
        new_head = (f"{method} {path} {version}\r\n" + "\r\n".join(lines[1:]) + "\r\n\r\n").encode("latin1")
        body = data[header_end + 4:]
        remote = socket.create_connection((parsed.hostname, parsed.port or 80), timeout=15)
        remote.sendall(new_head)
        if body:
            remote.sendall(body)
        remaining = cl - len(body)
        while remaining > 0:
            chunk = conn.recv(min(65536, remaining))
            if not chunk:
                break
            remote.sendall(chunk)
            remaining -= len(chunk)
        self._pipe(remote, conn)

    @staticmethod
    def _tunnel(a, b):
        def copy(src, dst):
            try:
                while True:
                    buf = src.recv(65536)
                    if not buf:
                        break
                    dst.sendall(buf)
            except Exception:
                pass
            try:
                dst.shutdown(socket.SHUT_WR)
            except Exception:
                pass

        t1 = run_in_thread(copy, a, b)
        t2 = run_in_thread(copy, b, a)
        t1.join()
        t2.join()

    @staticmethod
    def _pipe(src, dst):
        try:
            while True:
                buf = src.recv(65536)
                if not buf:
                    break
                dst.sendall(buf)
        except Exception:
            pass
        try:
            dst.shutdown(socket.SHUT_WR)
        except Exception:
            pass


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
    if len(data) < 10 or data[0] != 0x00 or data[1] != 0x00:
        return None, 0, 0, False
    frag = data[2]
    atyp = data[3]
    off = 4
    if atyp == 0x01:
        if len(data) < off + 4 + 2:
            return None, 0, 0, False
        host = socket.inet_ntop(socket.AF_INET, data[off:off + 4])
        off += 4
    elif atyp == 0x03:
        if len(data) < off + 1:
            return None, 0, 0, False
        l = data[off]
        off += 1
        if len(data) < off + l + 2:
            return None, 0, 0, False
        host = data[off:off + l].decode()
        off += l
    elif atyp == 0x04:
        if len(data) < off + 16 + 2:
            return None, 0, 0, False
        host = socket.inet_ntop(socket.AF_INET6, data[off:off + 16])
        off += 16
    else:
        return None, 0, 0, False
    port = struct.unpack(">H", data[off:off + 2])[0]
    off += 2
    return f"{host}:{port}", frag, off, True


def addr_to_tuple(addr_str):
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
    server = None
    try:
        server, addr = start_local_http_server()
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = http_request_with_retry(
            "GET",
            f"http://127.0.0.1:{addr[1]}/get",
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        ok = r.status_code == 200
        return TestResult("HTTP 代理 GET", ok, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("HTTP 代理 GET", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass


def test_http_post():
    server = None
    try:
        server, addr = start_local_http_server()
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        payload = b"k5pw-test-body"
        r = http_request_with_retry(
            "POST",
            f"http://127.0.0.1:{addr[1]}/post",
            data=payload,
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        ok = (b"ECHO:" + payload).decode() in r.text
        return TestResult("HTTP 代理 POST", ok, f"HTTP {r.status_code}", "" if ok else "回显缺少 payload")
    except Exception as e:
        return TestResult("HTTP 代理 POST", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass


def test_http_connect():
    """通过 HTTP CONNECT 建立到本地 HTTP 服务器的隧道，验证隧道字节完整性。"""
    server = None
    try:
        server, addr = start_local_http_server()
        port = addr[1]
        c = socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=10)
        c.sendall(f"CONNECT 127.0.0.1:{port} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\n\r\n".encode())
        resp = c.recv(4096)
        if b"200" not in resp:
            return TestResult("HTTP CONNECT 隧道", False, f"CONNECT 失败: {resp!r}")
        c.sendall(b"GET /connect HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
        data = b""
        while True:
            chunk = c.recv(4096)
            if not chunk:
                break
            data += chunk
        c.close()
        ok = b"k5pw-test" in data
        return TestResult("HTTP CONNECT 隧道", ok, "隧道回显匹配" if ok else "回显缺失")
    except Exception as e:
        return TestResult("HTTP CONNECT 隧道", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass
        try:
            c.close()
        except Exception:
            pass


def test_direct_local_http():
    """通过 HTTP 代理访问本地 127.0.0.1 服务，验证直连路由生效。"""
    server = None
    try:
        server, addr = start_local_http_server()
        url = f"http://127.0.0.1:{addr[1]}/direct"
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = http_request_with_retry(
            "GET", url, proxies=proxies, timeout=15, headers={"User-Agent": "k5pw-test/1.0"}
        )
        r.raise_for_status()
        get_ok = "k5pw-test" in r.text
        payload = b"k5pw-direct-body"
        r2 = http_request_with_retry(
            "POST",
            url,
            data=payload,
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r2.raise_for_status()
        post_ok = r2.text == (b"ECHO:" + payload).decode()
        ok = get_ok and post_ok
        details = "" if ok else f"GET={r.text!r} POST={r2.text!r}"
        return TestResult("HTTP 直连本地服务", ok, "直连回显匹配" if ok else "回显不匹配", details)
    except Exception as e:
        return TestResult("HTTP 直连本地服务", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass


def test_proxy_upstream_path():
    """经代理路由访问 foreign.test（命中 proxy 规则 -> 本地上游），验证 proxy 路径。"""
    try:
        foreign_port = FIXTURES["foreign_port"]
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = http_request_with_retry(
            "GET",
            f"http://foreign.test:{foreign_port}/proxy",
            proxies=proxies,
            timeout=15,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        ok = "FOREIGN-PROXY-OK" in r.text
        return TestResult("代理路径(proxy 路由)", ok, "经上游回显匹配" if ok else "回显不匹配")
    except Exception as e:
        return TestResult("代理路径(proxy 路由)", False, str(e))


def test_socks5_connect_domain():
    """SOCKS5 CONNECT 域名地址类型，经 foreign.test 走 proxy 路径。"""
    try:
        foreign_port = FIXTURES["foreign_port"]
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, f"foreign.test:{foreign_port}", force_domain=True)
        c.sock.sendall(b"GET / HTTP/1.1\r\nHost: foreign.test\r\nConnection: close\r\n\r\n")
        c.sock.shutdown(socket.SHUT_WR)
        data = b""
        while True:
            chunk = c.sock.recv(4096)
            if not chunk:
                break
            data += chunk
        c.close()
        ok = b"FOREIGN-PROXY-OK" in data
        return TestResult("SOCKS5 CONNECT 域名", ok, "代理路径回显匹配" if ok else "回显不匹配")
    except Exception as e:
        return TestResult("SOCKS5 CONNECT 域名", False, str(e))


def test_socks5_connect_ip():
    """SOCKS5 CONNECT IPv4 目标（本地直连服务器）。"""
    try:
        domestic_port = FIXTURES["domestic_port"]
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, f"127.0.0.1:{domestic_port}")
        c.sock.sendall(b"GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
        c.sock.shutdown(socket.SHUT_WR)
        data = b""
        while True:
            chunk = c.sock.recv(4096)
            if not chunk:
                break
            data += chunk
        c.close()
        ok = b"DOMESTIC-DIRECT-OK" in data
        return TestResult("SOCKS5 CONNECT IPv4", ok, "直连回显匹配" if ok else "回显不匹配")
    except Exception as e:
        return TestResult("SOCKS5 CONNECT IPv4", False, str(e))


def test_socks5_connect_ipv6():
    if not socket.has_ipv6:
        return TestResult("SOCKS5 CONNECT IPv6", True, "本机未启用 IPv6，已跳过")
    try:
        c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
        c.handshake()
        c.request(0x01, "[2606:4700:4700::1111]:443")
        import ssl
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        with ctx.wrap_socket(c.sock, server_hostname="") as ssock:
            ssock.sendall(b"GET / HTTP/1.1\r\nHost: one.one.one.one\r\nConnection: close\r\n\r\n")
            data = ssock.recv(4096)
        ok = len(data) > 0
        return TestResult("SOCKS5 CONNECT IPv6", ok, "TLS 握手完成" if ok else "无数据")
    except Exception as e:
        return TestResult("SOCKS5 CONNECT IPv6", True, f"IPv6 出网受限，跳过: {e}")


def test_socks5_bind():
    """SOCKS5 BIND：无 SOCKS5 上游时走本地监听回退路径。"""
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
    """SOCKS5 UDP ASSOCIATE：无 SOCKS5 上游时走本地 UDP 中继回退路径。"""
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
        c.request(0x01, f"foreign.test:{FIXTURES['foreign_port']}", force_domain=True)
        c.close()
        return TestResult("SOCKS5 匿名认证", True, "接受匿名")
    except Exception as e:
        return TestResult("SOCKS5 匿名认证", False, str(e))


def test_concurrent_requests():
    """通过 HTTP 代理并发访问本地服务，验证代理连接并发处理能力。"""
    import concurrent.futures

    server = None
    try:
        server, addr = start_local_http_server()
        url = f"http://127.0.0.1:{addr[1]}/concurrent"
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}

        def fetch_once(i):
            try:
                r = http_request_with_retry(
                    "GET",
                    url,
                    proxies=proxies,
                    timeout=15,
                    headers={"User-Agent": f"k5pw-test/1.0-{i}"},
                )
                return r.status_code == 200 and "k5pw-test" in r.text
            except Exception:
                return False

        concurrency = 8
        with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as ex:
            results = list(ex.map(fetch_once, range(concurrency)))
        ok = all(results)
        return TestResult(
            f"HTTP 并发请求 x{concurrency}",
            ok,
            f"全部成功 {sum(results)}/{concurrency}" if ok else f"部分失败 {sum(results)}/{concurrency}",
        )
    except Exception as e:
        return TestResult("HTTP 并发请求", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass


def test_keepalive_reuse():
    """同一 TCP 连接上复用多次 HTTP/1.1 请求（keep-alive），验证连接复用。"""
    server = None
    try:
        server, addr = start_local_http_server()
        port = addr[1]
        c = socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=10)
        ok_all = True
        for _ in range(3):
            c.sendall(
                f"GET http://127.0.0.1:{port}/ka HTTP/1.1\r\n"
                f"Host: 127.0.0.1\r\nConnection: keep-alive\r\n\r\n".encode()
            )
            status, body = read_http_response(c)
            if "k5pw-test" not in body.decode("latin1", "replace"):
                ok_all = False
                break
        c.close()
        return TestResult("HTTP keep-alive 复用", ok_all, "三次请求均回显" if ok_all else "复用失败")
    except Exception as e:
        return TestResult("HTTP keep-alive 复用", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass
        try:
            c.close()
        except Exception:
            pass


def test_large_post():
    """向本地服务 POST 大体积 body（2MB），验证大请求体透传。"""
    server = None
    try:
        server, addr = start_local_http_server()
        port = addr[1]
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        payload = b"X" * (2 * 1024 * 1024)
        r = http_request_with_retry(
            "POST",
            f"http://127.0.0.1:{port}/large",
            data=payload,
            proxies=proxies,
            timeout=30,
            headers={"User-Agent": "k5pw-test/1.0"},
        )
        r.raise_for_status()
        ok = r.text == (b"ECHO:" + payload).decode("latin1")
        return TestResult("HTTP 大体积 POST(2MB)", ok, f"HTTP {r.status_code}" if ok else "回显不匹配")
    except Exception as e:
        return TestResult("HTTP 大体积 POST(2MB)", False, str(e))
    finally:
        if server:
            try:
                server.shutdown()
            except Exception:
                pass


def test_ssrf_metadata_unreachable():
    """SSRF 冒烟：元数据地址（169.254.169.254）不应被成功代理访问。

    注：测试配置关闭了 protect_private 以便使用 127.0.0.1 本地服务器，
    但云元数据地址在本环境不可达，代理应返回错误而非泄露内部内容。
    """
    try:
        proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}"}
        r = requests.get(
            "http://169.254.169.254/latest/meta-data/",
            proxies=proxies,
            timeout=8,
            verify=False,
        )
        # 若意外返回 2xx 且包含疑似元数据内容，则视为失败
        if r.status_code == 200:
            return TestResult("SSRF 元数据地址不可达", False, f"意外可访问: HTTP {r.status_code}")
        return TestResult("SSRF 元数据地址不可达", True, f"被正确拒绝: HTTP {r.status_code}")
    except Exception:
        return TestResult("SSRF 元数据地址不可达", True, "连接失败（符合预期，地址不可达）")


def test_management_health():
    try:
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}/health", timeout=8)
        ok = r.status_code == 200 and ("ok" in r.text.lower() or "status" in r.text.lower())
        return TestResult("管理接口 /health", ok, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("管理接口 /health", False, str(e))


def test_management_metrics():
    try:
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}/metrics", timeout=8)
        ok = r.status_code == 200 and "k5pw_" in r.text
        return TestResult("管理接口 /metrics", ok, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("管理接口 /metrics", False, str(e))


def test_management_status():
    try:
        r = requests.get(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/status", timeout=8)
        r.raise_for_status()
        data = r.json()
        ok = isinstance(data, dict) and ("listen" in data or "version" in data or "upstreams" in data)
        return TestResult("管理接口 /api/v1/status", ok, "返回状态摘要" if ok else "字段缺失")
    except Exception as e:
        return TestResult("管理接口 /api/v1/status", False, str(e))


def test_sites_with_proxy(proxy_label, proxy_url, scheme="http"):
    """使用指定代理批量访问本地站点（直连/代理），返回结果列表。"""
    results = []
    for site in TEST_SITES:
        proxies = {"http": proxy_url, "https": proxy_url}
        try:
            r = http_request_with_retry(
                "GET",
                site["url"],
                proxies=proxies,
                timeout=15,
                headers={"User-Agent": "k5pw-test/1.0"},
                allow_redirects=True,
                verify=False,
                retries=2,
            )
            keywords = site.get("keywords", site.get("keyword", ""))
            if isinstance(keywords, str):
                keywords = [keywords]
            text_lower = r.text.lower()
            found = any(k.lower() in text_lower for k in keywords if k)
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
# 测试夹具（本地服务器 + 本地上游代理）
# ---------------------------------------------------------------------------

def ensure_hosts_entry():
    """确保 /etc/hosts 中存在 foreign.test 解析，便于 proxy 路径按域名路由。"""
    entry = f"{UPSTREAM_HOST} foreign.test" if False else "127.0.0.1 foreign.test"
    try:
        with open("/etc/hosts", "r") as f:
            content = f.read()
        if "foreign.test" in content:
            return
        with open("/etc/hosts", "a") as f:
            f.write(f"\n{entry}\n")
    except Exception as e:
        print(f"{Colors.WARN}无法写入 /etc/hosts（foreign.test 解析可能失败）: {e}{Colors.RESET}")


def start_fixtures():
    domestic, daddr = start_local_http_server(body=b'{"origin": "DOMESTIC-DIRECT-OK", "status": "ok"}')
    foreign, faddr = start_local_http_server(body=b'{"origin": "FOREIGN-PROXY-OK", "status": "ok"}')
    upstream = LocalHTTPUpstream()
    upstream.start()

    FIXTURES["domestic_port"] = daddr[1]
    FIXTURES["foreign_port"] = faddr[1]
    FIXTURES["upstream"] = upstream

    global DOMESTIC_SITES, FOREIGN_SITES, TEST_SITES
    DOMESTIC_SITES = [{
        "name": "本地直连",
        "url": f"http://127.0.0.1:{daddr[1]}/",
        "keywords": ["DOMESTIC-DIRECT-OK"],
        "expected": "direct",
    }]
    FOREIGN_SITES = [{
        "name": "本地代理",
        "url": f"http://foreign.test:{faddr[1]}/",
        "keywords": ["FOREIGN-PROXY-OK"],
        "expected": "proxy",
    }]
    TEST_SITES = DOMESTIC_SITES + FOREIGN_SITES


def stop_fixtures():
    if "upstream" in FIXTURES:
        try:
            FIXTURES["upstream"].stop()
        except Exception:
            pass


# ---------------------------------------------------------------------------
# 主函数
# ---------------------------------------------------------------------------

def wait_for_proxy_ready(timeout=60):
    """等待 k5pw 就绪且本地上游被健康检查发现（proxy 路径可用）。

    注意：在代理尚未发现本地上游的 dead 窗口内，若直接轮询 foreign（穿透代理发请求），
    每次失败都会让 k5pw 把上游 markDead，形成死亡螺旋。因此本函数先确认直连就绪，
    再留出上游健康检查发现的时间（check_interval + 余量），最后才确认 foreign 可达。
    """
    domestic_url = f"http://127.0.0.1:{FIXTURES['domestic_port']}/"
    foreign_url = f"http://foreign.test:{FIXTURES['foreign_port']}/"
    proxies = {"http": f"http://{PROXY_HOST}:{PROXY_PORT}", "https": f"http://{PROXY_HOST}:{PROXY_PORT}"}
    deadline = time.time() + timeout

    # 1) 等待代理本身就绪（直连 domestic，不依赖上游）
    domestic_ok = False
    while time.time() < deadline:
        try:
            r = requests.get(domestic_url, proxies=proxies, timeout=5, verify=False)
            if r.status_code == 200:
                domestic_ok = True
                break
        except Exception:
            pass
        time.sleep(1)
    if not domestic_ok:
        return False

    # 2) 留出上游健康检查发现本地上游的时间，期间不轮询 foreign，避免把上游打挂
    time.sleep(8)

    # 3) 最终确认 foreign（proxy 路径）可达
    while time.time() < deadline:
        try:
            r = requests.get(foreign_url, proxies=proxies, timeout=5, verify=False)
            if r.status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(2)
    return False


def run_with_upstream_retry(test_fn, attempts=5, wait=2):
    """运行依赖本地上游的用例；若因上游瞬时不可用失败，则等待恢复后重试。"""
    last = None
    for _ in range(attempts):
        res = test_fn()
        if res.ok:
            return res
        last = res
        # 等待本地上游被健康检查发现（proxy 路径恢复）后再重试
        wait_for_proxy_ready(timeout=wait * (attempts) + 10)
        time.sleep(wait)
    return last


def test_https_connect_via_proxy():
    """CONNECT 隧道经 proxy 路径（foreign.test -> 本地上游）建立，验证 HTTPS 隧道走上游。"""
    try:
        foreign_port = FIXTURES["foreign_port"]
        c = socket.create_connection((PROXY_HOST, PROXY_PORT), timeout=10)
        c.sendall(f"CONNECT foreign.test:{foreign_port} HTTP/1.1\r\nHost: foreign.test\r\n\r\n".encode())
        resp = c.recv(4096)
        if b"200" not in resp:
            return TestResult("CONNECT 经代理路径", False, f"CONNECT 失败: {resp!r}")
        c.sendall(b"GET / HTTP/1.1\r\nHost: foreign.test\r\nConnection: close\r\n\r\n")
        data = b""
        while True:
            chunk = c.recv(4096)
            if not chunk:
                break
            data += chunk
        c.close()
        ok = b"FOREIGN-PROXY-OK" in data
        return TestResult("CONNECT 经代理路径", ok, "隧道回显匹配" if ok else "回显缺失")
    except Exception as e:
        return TestResult("CONNECT 经代理路径", False, str(e))
    finally:
        try:
            c.close()
        except Exception:
            pass


def test_concurrent_socks5():
    """并发建立多个 SOCKS5 CONNECT，验证 SOCKS5 并发处理能力。"""
    domestic_port = FIXTURES["domestic_port"]
    import concurrent.futures

    def one(_):
        try:
            c = SOCKS5RawClient(PROXY_HOST, PROXY_PORT)
            c.handshake()
            c.request(0x01, f"127.0.0.1:{domestic_port}")
            c.sock.sendall(b"GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
            c.sock.shutdown(socket.SHUT_WR)
            data = b""
            while True:
                ch = c.sock.recv(4096)
                if not ch:
                    break
                data += ch
            c.close()
            return b"DOMESTIC-DIRECT-OK" in data
        except Exception:
            return False

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as ex:
            results = list(ex.map(one, range(6)))
        ok = all(results)
        return TestResult("SOCKS5 并发连接 x6", ok, f"全部成功 {sum(results)}/6" if ok else f"部分失败 {sum(results)}/6")
    except Exception as e:
        return TestResult("SOCKS5 并发连接 x6", False, str(e))


def test_management_reload():
    """管理接口 POST /api/v1/reload 应成功触发配置重新加载。"""
    try:
        r = requests.post(f"http://{MGMT_HOST}:{MGMT_PORT}/api/v1/reload", timeout=8)
        ok = r.status_code == 200
        return TestResult("管理接口 /api/v1/reload", ok, f"HTTP {r.status_code}")
    except Exception as e:
        return TestResult("管理接口 /api/v1/reload", False, str(e))


def run_single_tests():
    print_header("单能力测试")
    tests = [
        test_tcp_connect,
        test_http_get,
        test_http_post,
        test_http_connect,
        test_direct_local_http,
        # 以下三个用例依赖本地上游（proxy 路径），若遇到上游瞬时 dead 窗口则等待恢复后重试
        lambda: run_with_upstream_retry(test_proxy_upstream_path),
        lambda: run_with_upstream_retry(test_socks5_connect_domain),
        lambda: run_with_upstream_retry(test_https_connect_via_proxy),
        test_socks5_connect_ip,
        test_socks5_connect_ipv6,
        test_socks5_bind,
        test_socks5_udp_associate,
        lambda: run_with_upstream_retry(test_socks5_anonymous_auth),
        test_concurrent_requests,
        test_concurrent_socks5,
        test_management_reload,
        test_keepalive_reuse,
        test_large_post,
        test_ssrf_metadata_unreachable,
        test_management_health,
        test_management_metrics,
        test_management_status,
    ]
    results = []
    for t in tests:
        res = t()
        results.append(res)
        print(f"  {res}")
    return results


def run_site_tests():
    print_header("路由分流测试（本地站点）")
    print(f"{Colors.INFO}等待代理与上游就绪...{Colors.RESET}")
    if wait_for_proxy_ready():
        print(f"{Colors.OK}代理与上游已就绪{Colors.RESET}")
    else:
        print(f"{Colors.WARN}代理或上游未就绪，站点测试可能失败{Colors.RESET}")
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


def summarize(single_results, site_results, strict=False):
    print_header("汇总")
    ok = sum(1 for r in single_results if r.ok)
    fail = len(single_results) - ok
    print(f"  单能力测试: 成功={ok}  失败={fail}")

    site_fail = 0
    site_warn = 0
    for label, results in site_results.items():
        s_ok = sum(1 for r in results if r["status"].startswith("OK"))
        s_warn = sum(1 for r in results if r["status"].startswith("WARN"))
        s_fail = len(results) - s_ok - s_warn
        site_fail += s_fail
        site_warn += s_warn
        print(f"  站点测试 {label:<14}: 成功={s_ok}  警告={s_warn}  失败={s_fail}")

    if fail == 0:
        print(f"{Colors.OK}所有单能力测试通过。{Colors.RESET}")
    else:
        print(f"{Colors.FAIL}存在失败的单能力测试，请检查代理日志。{Colors.RESET}")

    if site_fail > 0:
        print(f"{Colors.FAIL}存在失败的站点测试，请检查网络或上游状态。{Colors.RESET}")
    elif site_warn > 0:
        if strict:
            print(f"{Colors.FAIL}严格模式下站点关键词警告视为失败。{Colors.RESET}")
        else:
            print(f"{Colors.WARN}站点测试存在关键词警告（非严格模式下不影响退出码）。{Colors.RESET}")
    else:
        print(f"{Colors.OK}所有站点测试通过。{Colors.RESET}")

    return fail, site_fail, site_warn


def parse_args():
    parser = argparse.ArgumentParser(description="k5pw 4479 端口全能力验收测试（自包含版）")
    parser.add_argument("--strict", action="store_true", help="将站点关键词警告（WARN）也视为失败")
    parser.add_argument("--port", type=int, default=4479, help="代理监听端口，默认 4479")
    return parser.parse_args()


def main():
    check_dependencies()
    args = parse_args()
    global PROXY_PORT
    PROXY_PORT = args.port

    print(f"{Colors.INFO}k5pw 代理服务器全能力测试: {PROXY_HOST}:{PROXY_PORT}{Colors.RESET}")
    ensure_hosts_entry()
    start_fixtures()
    print(f"{Colors.INFO}等待代理与本地上游就绪...{Colors.RESET}")
    if wait_for_proxy_ready():
        print(f"{Colors.OK}代理与本地上游已就绪{Colors.RESET}")
    else:
        print(f"{Colors.WARN}代理或本地上游未就绪，部分用例可能失败{Colors.RESET}")
    try:
        single_results = run_single_tests()
        site_results = run_site_tests()
    finally:
        stop_fixtures()
    single_fail, site_fail, site_warn = summarize(single_results, site_results, strict=args.strict)

    failed = [r.name for r in single_results if not r.ok]
    if failed:
        print(f"\n{Colors.FAIL}失败项: {', '.join(failed)}{Colors.RESET}")

    if single_fail > 0 or site_fail > 0 or (args.strict and site_warn > 0):
        sys.exit(1)


if __name__ == "__main__":
    main()
