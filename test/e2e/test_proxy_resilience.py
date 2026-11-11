#!/usr/bin/env python3
"""YiGate 代理稳健性的黑盒端到端验收。

与 Go 单元测试不同，这里启动真实的 yigate 进程，用真实的 TCP/UDP 客户端
从外部验证协议行为与故障场景：

1. 基本代理能力（SOCKS5 / HTTP CONNECT）
2. UDP ASSOCIATE 在握手超时之后仍然存活
3. UDP 目标会话数有上限（fd 不随目标数线性增长）
4. 故障风暴下热路径告警被限频
5. 流量指标不含高基数 target 标签
6. 半关闭后对端回写的响应完整送达
7. 级联 BIND 失败后不对客户端重复回复
8. fd 耗尽时 Accept 退避、进程不死、释放后可恢复

用法：
    python3 test/e2e/test_proxy_resilience.py --bin build/yigate-e2e

环境变量：
    YIGATE_E2E_SLOW=1  额外执行需要等待 30s 以上的慢速用例（级联 BIND 第二响应超时）。
"""

import argparse
import os
import resource
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

PROXY_PORT = int(os.environ.get("YIGATE_E2E_LISTEN_PORT", "14481"))
MGMT_PORT = int(os.environ.get("YIGATE_E2E_MGMT_PORT", "14490"))
TOKEN = "yigate-resilience-e2e-token"
HANDSHAKE_TIMEOUT_SECONDS = 2

RESULTS = []


def record(name, ok, detail=""):
    RESULTS.append((name, ok, detail))
    mark = "PASS" if ok else "FAIL"
    print(f"[{mark}] {name}" + (f" - {detail}" if detail else ""), flush=True)


# ---------------------------------------------------------------------------
# 测试替身：echo 服务、读到 EOF 才回写的服务、假 SOCKS5 上游
# ---------------------------------------------------------------------------


def start_tcp_server(handler):
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", 0))
    srv.listen(64)
    addr = srv.getsockname()

    def loop():
        while True:
            try:
                conn, _ = srv.accept()
            except OSError:
                return
            threading.Thread(target=handler, args=(conn,), daemon=True).start()

    threading.Thread(target=loop, daemon=True).start()
    return srv, addr


def start_echo_server():
    def handler(conn):
        with conn:
            while True:
                data = conn.recv(65535)
                if not data:
                    return
                conn.sendall(data)

    return start_tcp_server(handler)


def start_eof_responder(response=b"response-after-client-eof"):
    def handler(conn):
        with conn:
            while True:
                if not conn.recv(65535):
                    break
            conn.sendall(response)

    return start_tcp_server(handler)


def start_udp_echo_server():
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("127.0.0.1", 0))
    addr = sock.getsockname()

    def loop():
        while True:
            try:
                data, src = sock.recvfrom(65535)
            except OSError:
                return
            sock.sendto(data, src)

    threading.Thread(target=loop, daemon=True).start()
    return sock, addr


def start_fake_socks5_upstream(hold=False):
    """完成握手与第一次 BIND 响应。

    hold=False：随后直接断开，用于验证"已回复后不再回退重发响应"。
    hold=True ：保持连接但永不给第二次响应，用于验证第二响应超时。
    """

    def handler(conn):
        with conn:
            try:
                head = recv_exact(conn, 2)
                recv_exact(conn, head[1])
                conn.sendall(b"\x05\x00")
                req = recv_exact(conn, 4)
                atyp = req[3]
                if atyp == 1:
                    recv_exact(conn, 4)
                elif atyp == 3:
                    recv_exact(conn, recv_exact(conn, 1)[0])
                elif atyp == 4:
                    recv_exact(conn, 16)
                recv_exact(conn, 2)
                if req[1] == 0x02:  # BIND
                    conn.sendall(b"\x05\x00\x00\x01\x7f\x00\x00\x01\x1f\x90")
                    if hold:
                        time.sleep(120)
            except OSError:
                pass

    return start_tcp_server(handler)


def recv_exact(conn, n):
    buf = b""
    while len(buf) < n:
        chunk = conn.recv(n - len(buf))
        if not chunk:
            raise OSError("connection closed")
        buf += chunk
    return buf


# ---------------------------------------------------------------------------
# 真实客户端
# ---------------------------------------------------------------------------


def socks5_addr(host, port):
    try:
        packed = socket.inet_aton(host)
        return b"\x01" + packed + struct.pack(">H", port)
    except OSError:
        raw = host.encode()
        return b"\x03" + bytes([len(raw)]) + raw + struct.pack(">H", port)


def socks5_connect(proxy_addr, cmd, host, port, timeout=5):
    conn = socket.create_connection(proxy_addr, timeout=timeout)
    conn.settimeout(timeout)
    conn.sendall(b"\x05\x01\x00")
    if recv_exact(conn, 2) != b"\x05\x00":
        raise AssertionError("greeting refused")
    conn.sendall(b"\x05" + bytes([cmd]) + b"\x00" + socks5_addr(host, port))
    head = recv_exact(conn, 4)
    atyp = head[3]
    if atyp == 1:
        bind_host = socket.inet_ntoa(recv_exact(conn, 4))
    elif atyp == 3:
        bind_host = recv_exact(conn, recv_exact(conn, 1)[0]).decode()
    else:
        bind_host = socket.inet_ntop(socket.AF_INET6, recv_exact(conn, 16))
    bind_port = struct.unpack(">H", recv_exact(conn, 2))[0]
    return conn, head[1], (bind_host, bind_port)


def udp_packet(host, port, payload):
    return b"\x00\x00\x00" + socks5_addr(host, port) + payload


def read_udp_payload(data):
    atyp = data[3]
    off = 4
    if atyp == 1:
        off += 4
    elif atyp == 3:
        off += 1 + data[4]
    else:
        off += 16
    return data[off + 2 :]


# ---------------------------------------------------------------------------
# 被测进程
# ---------------------------------------------------------------------------


def write_config(workdir, upstream_url="", listen_port=PROXY_PORT, mgmt_port=MGMT_PORT, handshake_seconds=HANDSHAKE_TIMEOUT_SECONDS, log_output="file"):
    upstreams = ""
    if upstream_url:
        upstreams = f"""
upstreams:
  - name: fake
    url: "{upstream_url}"
    enabled: true
"""
    config = f"""
listeners:
  - listen: "127.0.0.1:{listen_port}"
    socks5:
      allow_bind: true
      allow_udp_associate: true
management: "127.0.0.1:{mgmt_port}"
management_auth_token: "{TOKEN}"
allow_anonymous_proxy: true
allow_private_targets: true
destination_policy:
  protect_private: false
  default_action: allow
check_interval: 0
{upstreams}
routing:
  final: direct
  socks5h_redirect:
    enabled: false
  geosite:
    auto_update: false
traffic_audit:
  enabled: true
timeouts:
  handshake: {handshake_seconds}s
log:
  level: warn
  format: json
  output: {log_output}
  file: "{workdir}/yigate.log"
  rotation: size
  max_size: 20
  max_age: 1
  max_backups: 1
"""
    path = os.path.join(workdir, "config.yml")
    with open(path, "w") as fh:
        fh.write(config)
    return path


def start_process(binary, config, workdir, fd_limit=None):
    def limit_fds():
        if fd_limit:
            resource.setrlimit(resource.RLIMIT_NOFILE, (fd_limit, fd_limit))

    proc = subprocess.Popen(
        [binary, "-config", config, "run"],
        cwd=workdir,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.STDOUT,
        preexec_fn=limit_fds if fd_limit else None,
    )
    deadline = time.time() + 10
    while time.time() < deadline:
        if proc.poll() is not None:
            raise AssertionError(f"yigate exited early with code {proc.returncode}")
        try:
            socket.create_connection(("127.0.0.1", PROXY_PORT), timeout=0.3).close()
            return proc
        except OSError:
            time.sleep(0.1)
    raise AssertionError("yigate did not start listening in time")


def stop_process(proc):
    if proc and proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()


def read_log(workdir):
    path = os.path.join(workdir, "yigate.log")
    if not os.path.exists(path):
        return ""
    with open(path, errors="replace") as fh:
        return fh.read()


def count_fds(proc):
    return len(os.listdir(f"/proc/{proc.pid}/fd"))


# ---------------------------------------------------------------------------
# 验收用例
# ---------------------------------------------------------------------------


def case_basic_proxy(proxy_addr, echo_addr):
    conn, rep, _ = socks5_connect(proxy_addr, 0x01, *echo_addr)
    assert rep == 0, f"SOCKS5 CONNECT refused: {rep}"
    conn.sendall(b"hello")
    assert recv_exact(conn, 5) == b"hello"
    conn.close()

    http = socket.create_connection(proxy_addr, timeout=5)
    http.sendall(f"CONNECT {echo_addr[0]}:{echo_addr[1]} HTTP/1.1\r\nHost: {echo_addr[0]}\r\n\r\n".encode())
    assert b" 200 " in http.recv(128)
    http.sendall(b"world")
    assert recv_exact(http, 5) == b"world"
    http.close()
    return True, "SOCKS5 与 HTTP CONNECT 均可转发"


def case_udp_outlives_handshake(proxy_addr, udp_echo_addr):
    ctrl, rep, bind = socks5_connect(proxy_addr, 0x03, "0.0.0.0", 0)
    assert rep == 0, f"UDP ASSOCIATE refused: {rep}"
    relay = ("127.0.0.1", bind[1])
    client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    client.bind(("127.0.0.1", 0))
    client.settimeout(2)
    # 覆盖握手超时的 3 倍时长。
    rounds = HANDSHAKE_TIMEOUT_SECONDS * 3
    for i in range(rounds):
        payload = f"ping-{i}".encode()
        client.sendto(udp_packet(*udp_echo_addr, payload), relay)
        data, _ = client.recvfrom(65535)
        assert read_udp_payload(data) == payload, f"第 {i} 次 UDP echo 失败"
        time.sleep(1)
    client.close()
    ctrl.close()
    return True, f"UDP 关联在 {rounds}s（握手超时 {HANDSHAKE_TIMEOUT_SECONDS}s）后仍可转发"


def case_udp_cap_and_log_sampling(proc, proxy_addr, workdir):
    ctrl, rep, bind = socks5_connect(proxy_addr, 0x03, "0.0.0.0", 0)
    assert rep == 0
    relay = ("127.0.0.1", bind[1])
    client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    client.bind(("127.0.0.1", 0))
    before = count_fds(proc)
    for i in range(192):
        client.sendto(udp_packet("127.0.0.1", 23000 + i, b"x"), relay)
    time.sleep(1.5)
    grew = count_fds(proc) - before
    log = read_log(workdir)
    warnings = log.count("udp session read failed")
    client.close()
    ctrl.close()
    assert grew <= 160, f"fd 增长 {grew}，会话上限未生效"
    assert warnings > 0, "没有保留任何 UDP 会话告警"
    assert warnings <= 5, f"热路径告警未限频：{warnings} 行"
    return True, f"192 个目标 fd 增量 {grew}（上限 128），告警 {warnings} 行"


def case_metrics_no_target():
    req = urllib.request.Request(
        f"http://127.0.0.1:{MGMT_PORT}/metrics",
        headers={"Authorization": f"Bearer {TOKEN}"},
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        body = resp.read().decode()
    lines = [l for l in body.splitlines() if l.startswith("yigate_traffic_bytes_total")]
    assert lines, "未找到流量指标样本"
    bad = [l for l in lines if "target=" in l]
    assert not bad, f"指标仍带 target 标签：{bad[0]}"
    return True, f"{len(lines)} 条流量指标样本均不含 target 标签"


def case_half_close(proxy_addr, responder_addr):
    conn, rep, _ = socks5_connect(proxy_addr, 0x01, *responder_addr)
    assert rep == 0
    conn.sendall(b"request-body")
    conn.shutdown(socket.SHUT_WR)
    conn.settimeout(5)
    data = b""
    while True:
        chunk = conn.recv(4096)
        if not chunk:
            break
        data += chunk
    conn.close()
    assert data == b"response-after-client-eof", f"响应被截断：{data!r}"
    return True, "CloseWrite 后完整收到对端回写的响应"


def case_bind_single_reply(proxy_addr):
    conn, rep, _ = socks5_connect(proxy_addr, 0x02, "198.51.100.7", 8080)
    assert rep == 0, f"第一次 BIND 响应异常：{rep}"
    conn.settimeout(5)
    extra = conn.recv(64)
    conn.close()
    assert extra == b"", f"收到重复响应：{extra!r}"
    return True, "级联失败后直接 EOF，没有第二次响应"


def case_accept_backoff(binary):
    workdir = tempfile.mkdtemp(prefix="yigate-fd-e2e-")
    # 空连接必须能一直保持住，才会真正耗尽 fd；握手超时调长。
    # fd 耗尽时 lumberjack 连日志文件都打不开，该用例必须用 stdout 捕获日志。
    config = write_config(workdir, listen_port=PROXY_PORT + 100, mgmt_port=MGMT_PORT + 100,
                          handshake_seconds=300, log_output="stdout")
    proc = None
    held = []
    try:
        proc = subprocess.Popen(
            [binary, "-config", config, "run"],
            cwd=workdir,
            stdout=open(os.path.join(workdir, "stdout.log"), "w"),
            stderr=subprocess.STDOUT,
            preexec_fn=lambda: resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256)),
        )
        addr = ("127.0.0.1", PROXY_PORT + 100)
        deadline = time.time() + 10
        while time.time() < deadline:
            try:
                socket.create_connection(addr, timeout=0.3).close()
                break
            except OSError:
                time.sleep(0.1)
        # 保持大量空连接，耗尽进程 fd。
        for _ in range(400):
            try:
                held.append(socket.create_connection(addr, timeout=0.2))
            except OSError:
                break
        time.sleep(3)
        with open(os.path.join(workdir, "stdout.log"), errors="replace") as fh:
            log = fh.read()
        assert proc.poll() is None, f"fd 耗尽时进程退出 rc={proc.returncode}"
        assert "accept error, backing off" in log, (
            f"未观察到 Accept 退避日志（保持连接 {len(held)} 个，fd={count_fds(proc)}），"
            f"目录：{os.listdir(workdir)}，日志尾部：{log[-500:]}"
        )
        warn_lines = log.count("accept error, backing off")
        assert warn_lines <= 10, f"退避/限频未生效：{warn_lines} 行 accept 告警"
        for s in held:
            s.close()
        held = []
        time.sleep(1)
        conn = socket.create_connection(addr, timeout=5)
        conn.close()
        assert proc.poll() is None
        return True, f"fd 耗尽时进程存活并退避（{warn_lines} 行告警），释放后恢复服务"
    finally:
        for s in held:
            s.close()
        stop_process(proc)
        if os.environ.get("YIGATE_E2E_KEEP") != "1":
            shutil.rmtree(workdir, ignore_errors=True)
        else:
            print(f"fd-case workdir kept: {workdir}")


def case_bind_second_reply_timeout(binary):
    """慢速用例：上游不给第二次响应时，客户端必须在超时被断开，而非永久挂起。"""
    workdir = tempfile.mkdtemp(prefix="yigate-bind-timeout-e2e-")
    hold_srv, hold_addr = start_fake_socks5_upstream(hold=True)
    proc = None
    try:
        config = write_config(
            workdir,
            upstream_url=f"socks5://{hold_addr[0]}:{hold_addr[1]}",
            listen_port=PROXY_PORT + 200,
            mgmt_port=MGMT_PORT + 200,
        )
        proc = subprocess.Popen(
            [binary, "-config", config, "run"],
            cwd=workdir,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.STDOUT,
        )
        addr = ("127.0.0.1", PROXY_PORT + 200)
        deadline = time.time() + 10
        while time.time() < deadline:
            try:
                socket.create_connection(addr, timeout=0.3).close()
                break
            except OSError:
                time.sleep(0.1)
        conn, rep, _ = socks5_connect(addr, 0x02, "198.51.100.7", 8080, timeout=60)
        assert rep == 0, f"第一次 BIND 响应异常：{rep}"
        start = time.time()
        conn.settimeout(60)
        extra = conn.recv(64)
        elapsed = time.time() - start
        conn.close()
        assert extra == b"", f"等待第二响应期间收到异常数据：{extra!r}"
        assert 20 <= elapsed <= 50, f"第二响应等待 {elapsed:.1f}s，未按 30s 超时断开"
        return True, f"上游沉默时在 {elapsed:.1f}s 按超时断开（阈值 30s）"
    finally:
        stop_process(proc)
        hold_srv.close()
        shutil.rmtree(workdir, ignore_errors=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bin", required=True, help="yigate 二进制路径")
    args = parser.parse_args()
    binary = os.path.abspath(args.bin)
    if not os.path.exists(binary):
        print(f"error: binary not found: {binary}", file=sys.stderr)
        return 2

    workdir = tempfile.mkdtemp(prefix="yigate-resilience-e2e-")
    echo_srv, echo_addr = start_echo_server()
    udp_srv, udp_echo_addr = start_udp_echo_server()
    eof_srv, eof_addr = start_eof_responder()
    upstream_srv, upstream_addr = start_fake_socks5_upstream()
    proc = None
    try:
        config = write_config(workdir, upstream_url=f"socks5://{upstream_addr[0]}:{upstream_addr[1]}")
        proc = start_process(binary, config, workdir)
        proxy_addr = ("127.0.0.1", PROXY_PORT)

        cases = [
            ("基本代理转发", lambda: case_basic_proxy(proxy_addr, echo_addr)),
            ("UDP 关联超过握手超时仍存活", lambda: case_udp_outlives_handshake(proxy_addr, udp_echo_addr)),
            ("UDP 会话上限与告警限频", lambda: case_udp_cap_and_log_sampling(proc, proxy_addr, workdir)),
            ("流量指标无 target 高基数标签", case_metrics_no_target),
            ("半关闭后响应完整", lambda: case_half_close(proxy_addr, eof_addr)),
            ("级联 BIND 不重复回复", lambda: case_bind_single_reply(proxy_addr)),
        ]
        for name, fn in cases:
            try:
                _, detail = fn()
                record(name, True, detail)
            except Exception as exc:  # noqa: BLE001 - 验收脚本需要汇总所有失败
                record(name, False, str(exc))

        try:
            _, detail = case_accept_backoff(binary)
            record("fd 耗尽时 Accept 退避且可恢复", True, detail)
        except Exception as exc:  # noqa: BLE001
            record("fd 耗尽时 Accept 退避且可恢复", False, str(exc))

        if os.environ.get("YIGATE_E2E_SLOW") == "1":
            try:
                _, detail = case_bind_second_reply_timeout(binary)
                record("级联 BIND 第二响应超时断开", True, detail)
            except Exception as exc:  # noqa: BLE001
                record("级联 BIND 第二响应超时断开", False, str(exc))
        else:
            print("[SKIP] 级联 BIND 第二响应超时（30s）；设 YIGATE_E2E_SLOW=1 可执行该慢速用例")
    finally:
        stop_process(proc)
        for srv in (echo_srv, udp_srv, eof_srv, upstream_srv):
            srv.close()
        if os.environ.get("YIGATE_E2E_KEEP") != "1":
            shutil.rmtree(workdir, ignore_errors=True)
        else:
            print(f"workdir kept: {workdir}")

    failed = [r for r in RESULTS if not r[1]]
    print(f"\n== 验收结果：{len(RESULTS) - len(failed)}/{len(RESULTS)} 通过 ==")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
