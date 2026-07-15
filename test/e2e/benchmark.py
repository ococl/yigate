#!/usr/bin/env python3
"""
benchmark.py — yigate 压力与基准测试脚本。

用法示例：
    # 直接测试目标站点
    python3 pyscripts/benchmark.py --url http://127.0.0.1:8080/ --requests 1000 --concurrency 50

    # 通过 yigate HTTP 代理
    python3 pyscripts/benchmark.py --url http://127.0.0.1:8080/ \
        --proxy http://127.0.0.1:4479 --requests 1000 --concurrency 50

    # 通过 yigate SOCKS5 代理
    python3 pyscripts/benchmark.py --url http://127.0.0.1:8080/ \
        --proxy socks5://127.0.0.1:4479 --requests 1000 --concurrency 50

    # 只测试 CONNECT 隧道（HTTPS 目标）
    python3 pyscripts/benchmark.py --url https://127.0.0.1:8443/ \
        --proxy http://127.0.0.1:4479 --requests 500 --concurrency 20
"""

import argparse
import statistics
import sys
import threading
import time
import urllib.request
from urllib.error import URLError, HTTPError
from queue import Queue


def parse_args():
    parser = argparse.ArgumentParser(description="yigate benchmark tool")
    parser.add_argument("--url", required=True, help="目标 URL，用于测试请求")
    parser.add_argument("--proxy", default="", help="代理地址，例如 http://127.0.0.1:4479 或 socks5://127.0.0.1:4479")
    parser.add_argument("--requests", type=int, default=1000, help="总请求数")
    parser.add_argument("--concurrency", type=int, default=50, help="并发连接数")
    parser.add_argument("--timeout", type=int, default=30, help="单次请求超时（秒）")
    parser.add_argument("--method", default="GET", help="HTTP 方法")
    parser.add_argument("--payload", default="", help="请求体，仅对非 GET 方法有效")
    parser.add_argument("--no-ssl-verify", action="store_true", help="忽略 HTTPS 证书验证")
    parser.add_argument("--warmup", type=int, default=10, help="预热请求数，不计入统计")
    return parser.parse_args()


def build_opener(proxy_url: str, no_ssl_verify: bool):
    handlers = []
    if proxy_url:
        scheme = proxy_url.split("://", 1)[0].lower()
        if scheme in ("http", "https"):
            handlers.append(urllib.request.ProxyHandler({"http": proxy_url, "https": proxy_url}))
        elif scheme in ("socks5", "socks5h"):
            try:
                import socks  # PySocks
            except ImportError as exc:
                raise SystemExit(
                    "SOCKS5 proxy requires PySocks. Install with: pip install pysocks"
                ) from exc
            proxy_host, proxy_port = proxy_url.split("://", 1)[1].rsplit(":", 1)
            proxy_port = int(proxy_port)
            socks.set_default_proxy(socks.SOCKS5, proxy_host, proxy_port)
            import socket
            socket.socket = socks.socksocket
        else:
            raise SystemExit(f"unsupported proxy scheme: {scheme}")

    context = None
    if no_ssl_verify:
        import ssl
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE

    if context:
        handlers.append(urllib.request.HTTPSHandler(context=context))
    return urllib.request.build_opener(*handlers)


def worker(args, opener, task_queue: Queue, latencies, errors, lock):
    while True:
        try:
            task_queue.get(timeout=1)
        except Exception:
            return

        start = time.perf_counter()
        try:
            req = urllib.request.Request(
                args.url,
                data=args.payload.encode() if args.payload else None,
                method=args.method,
            )
            with opener.open(req, timeout=args.timeout) as resp:
                _ = resp.read()
            elapsed = time.perf_counter() - start
            with lock:
                latencies.append(elapsed)
        except (URLError, HTTPError, TimeoutError) as exc:
            with lock:
                errors.append(str(exc))
        finally:
            task_queue.task_done()


def run_benchmark(args, opener, total):
    task_queue = Queue(maxsize=total)
    latencies = []
    errors = []
    lock = threading.Lock()

    threads = [
        threading.Thread(target=worker, args=(args, opener, task_queue, latencies, errors, lock))
        for _ in range(args.concurrency)
    ]
    for t in threads:
        t.start()

    start = time.perf_counter()
    for _ in range(total):
        task_queue.put(None)
    task_queue.join()
    elapsed = time.perf_counter() - start

    for t in threads:
        t.join()

    return latencies, errors, elapsed


def print_report(latencies, errors, elapsed, total):
    print("\n=== Benchmark Report ===")
    print(f"Total requests: {total}")
    print(f"Successful:     {len(latencies)}")
    print(f"Failed:         {len(errors)}")
    print(f"Duration:       {elapsed:.3f}s")
    if elapsed > 0:
        print(f"Throughput:     {len(latencies) / elapsed:.2f} req/s")

    if latencies:
        latencies_ms = sorted([x * 1000 for x in latencies])
        print(f"Latency (ms):   min={latencies_ms[0]:.2f}, max={latencies_ms[-1]:.2f}")
        print(f"Latency avg:    {statistics.mean(latencies_ms):.2f} ms")
        print(f"Latency p50:    {statistics.median(latencies_ms):.2f} ms")
        if len(latencies_ms) >= 20:
            p95_idx = int(len(latencies_ms) * 0.95)
            p99_idx = int(len(latencies_ms) * 0.99)
            print(f"Latency p95:    {latencies_ms[min(p95_idx, len(latencies_ms) - 1)]:.2f} ms")
            print(f"Latency p99:    {latencies_ms[min(p99_idx, len(latencies_ms) - 1)]:.2f} ms")
    else:
        print("No successful requests.")

    if errors:
        unique_errors = {}
        for err in errors:
            unique_errors[err] = unique_errors.get(err, 0) + 1
        print("\nError breakdown:")
        for err, count in sorted(unique_errors.items(), key=lambda x: -x[1])[:5]:
            print(f"  {count}x {err}")


def main():
    args = parse_args()
    if args.requests <= 0 or args.concurrency <= 0:
        print("requests and concurrency must be positive", file=sys.stderr)
        sys.exit(1)

    opener = build_opener(args.proxy, args.no_ssl_verify)

    if args.warmup > 0:
        print(f"Warming up with {args.warmup} requests...")
        _, _, _ = run_benchmark(args, opener, args.warmup)
        print("Warmup done.")

    print(f"Benchmarking {args.requests} requests with concurrency {args.concurrency}...")
    latencies, errors, elapsed = run_benchmark(args, opener, args.requests)
    print_report(latencies, errors, elapsed, args.requests)

    if errors and len(errors) > args.requests * 0.05:
        sys.exit(1)


if __name__ == "__main__":
    main()
