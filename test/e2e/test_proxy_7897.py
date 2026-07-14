#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
代理服务器测试脚本
测试代理对 HTTP/HTTPS/SOCKS 代理的支持，并访问多个国内网站。
"""

import socket
import time
import requests
from urllib.parse import urlparse


PROXY_HOST = "10.2.3.150"
PROXY_PORT = 7897

# 国内测试站点
DOMESTIC_SITES = [
    {"name": "百度", "url": "https://www.baidu.com", "keyword": "百度一下"},
    {"name": "腾讯QQ", "url": "https://www.qq.com", "keyword": "腾讯"},
    {"name": "淘宝", "url": "https://www.taobao.com", "keyword": "淘宝"},
    {"name": "京东", "url": "https://www.jd.com", "keyword": "京东"},
    {"name": "163网易", "url": "https://www.163.com", "keyword": "网易"},
]

# 国外测试站点
FOREIGN_SITES = [
    {"name": "Google", "url": "https://www.google.com", "keyword": "Google"},
    {"name": "GitHub", "url": "https://github.com", "keyword": "GitHub"},
    {"name": "YouTube", "url": "https://www.youtube.com", "keyword": "YouTube"},
    {"name": "Amazon", "url": "https://www.amazon.com", "keyword": "Amazon"},
]

TEST_SITES = DOMESTIC_SITES + FOREIGN_SITES


class Colors:
    OK = "\033[92m"
    WARN = "\033[93m"
    FAIL = "\033[91m"
    INFO = "\033[94m"
    RESET = "\033[0m"


def print_header(title):
    print(f"\n{Colors.INFO}{'='*60}")
    print(f" {title}")
    print(f"{'='*60}{Colors.RESET}")


def check_proxy_tcp_connect(host, port, timeout=5):
    """先测试 TCP 端口是否通。"""
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True, "TCP 连通"
    except socket.timeout:
        return False, "TCP 连接超时"
    except ConnectionRefusedError:
        return False, "TCP 连接被拒绝"
    except Exception as e:
        return False, f"TCP 连接失败: {e}"


def test_http_proxy(site, proxy_url, timeout=15):
    """使用 HTTP/HTTPS 代理访问站点。"""
    proxies = {
        "http": proxy_url,
        "https": proxy_url,
    }
    start = time.time()
    try:
        resp = requests.get(
            site["url"],
            proxies=proxies,
            timeout=timeout,
            headers={
                "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
            },
            allow_redirects=True,
        )
        elapsed = (time.time() - start) * 1000
        resp.raise_for_status()
        keyword = site.get("keyword", "")
        body = resp.text
        found = keyword in body
        status = "OK" if found else "WARN(无关键词)"
        return status, resp.status_code, round(elapsed, 2), len(body), None
    except requests.exceptions.ProxyError as e:
        return "FAIL", None, None, None, f"代理错误: {e}"
    except requests.exceptions.ConnectTimeout as e:
        return "FAIL", None, None, None, f"连接超时: {e}"
    except requests.exceptions.ReadTimeout as e:
        return "FAIL", None, None, None, f"读取超时: {e}"
    except requests.exceptions.ConnectionError as e:
        return "FAIL", None, None, None, f"连接错误: {e}"
    except requests.exceptions.RequestException as e:
        return "FAIL", None, None, None, f"请求异常: {e}"


def test_socks_proxy(site, proxy_url, timeout=15):
    """使用 SOCKS 代理访问站点。"""
    proxies = {
        "http": proxy_url,
        "https": proxy_url,
    }
    start = time.time()
    try:
        resp = requests.get(
            site["url"],
            proxies=proxies,
            timeout=timeout,
            headers={
                "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
            },
            allow_redirects=True,
        )
        elapsed = (time.time() - start) * 1000
        resp.raise_for_status()
        keyword = site.get("keyword", "")
        body = resp.text
        found = keyword in body
        status = "OK" if found else "WARN(无关键词)"
        return status, resp.status_code, round(elapsed, 2), len(body), None
    except requests.exceptions.ProxyError as e:
        return "FAIL", None, None, None, f"代理错误: {e}"
    except requests.exceptions.ConnectTimeout as e:
        return "FAIL", None, None, None, f"连接超时: {e}"
    except requests.exceptions.ReadTimeout as e:
        return "FAIL", None, None, None, f"读取超时: {e}"
    except requests.exceptions.ConnectionError as e:
        return "FAIL", None, None, None, f"连接错误: {e}"
    except requests.exceptions.RequestException as e:
        return "FAIL", None, None, None, f"请求异常: {e}"


def color_status(status):
    if status.startswith("OK"):
        return f"{Colors.OK}{status}{Colors.RESET}"
    if status.startswith("WARN"):
        return f"{Colors.WARN}{status}{Colors.RESET}"
    return f"{Colors.FAIL}{status}{Colors.RESET}"


def run_tests(proxy_label, proxy_url, test_func):
    print_header(f"测试代理类型: {proxy_label} ({proxy_url})")
    results = []
    for site in TEST_SITES:
        status, code, elapsed, size, err = test_func(site, proxy_url)
        results.append({
            "name": site["name"],
            "url": site["url"],
            "status": status,
            "code": code,
            "elapsed": elapsed,
            "size": size,
            "err": err,
        })
        code_str = str(code) if code is not None else "-"
        elapsed_str = f"{elapsed}ms" if elapsed is not None else "-"
        size_str = f"{size}B" if size is not None else "-"
        err_str = f" | {err}" if err else ""
        print(f"  {site['name']:<12} {color_status(status):<18} HTTP={code_str:<6} 耗时={elapsed_str:<10} 大小={size_str:<10}{err_str}")
    return results


def summarize(all_results):
    print_header("汇总")
    for proxy_label, results in all_results.items():
        ok = sum(1 for r in results if r["status"].startswith("OK"))
        warn = sum(1 for r in results if r["status"].startswith("WARN"))
        fail = sum(1 for r in results if r["status"].startswith("FAIL"))
        print(f"  {proxy_label:<12} 成功={ok}  警告={warn}  失败={fail}")


def main():
    print(f"{Colors.INFO}代理服务器测试: {PROXY_HOST}:{PROXY_PORT}{Colors.RESET}")
    ok, msg = check_proxy_tcp_connect(PROXY_HOST, PROXY_PORT)
    if ok:
        print(f"{Colors.OK}TCP 端口连通: {PROXY_HOST}:{PROXY_PORT}{Colors.RESET}")
    else:
        print(f"{Colors.FAIL}TCP 端口不通: {msg}{Colors.RESET}")
        print("端口都不通，后续代理测试大概率也会失败，但脚本仍会继续尝试。")

    all_results = {}

    # 1. HTTP 代理 (HTTP 目标)
    all_results["HTTP"] = run_tests("HTTP", f"http://{PROXY_HOST}:{PROXY_PORT}", test_http_proxy)

    # 2. HTTPS 代理 (HTTPS 目标，CONNECT 隧道)
    all_results["HTTPS"] = run_tests("HTTPS", f"http://{PROXY_HOST}:{PROXY_PORT}", test_http_proxy)

    # 3. SOCKS5 代理（本地 DNS 解析）
    all_results["SOCKS5"] = run_tests("SOCKS5", f"socks5://{PROXY_HOST}:{PROXY_PORT}", test_socks_proxy)

    # 4. SOCKS5H 代理（代理端 DNS 解析）
    all_results["SOCKS5H"] = run_tests("SOCKS5H", f"socks5h://{PROXY_HOST}:{PROXY_PORT}", test_socks_proxy)

    summarize(all_results)


if __name__ == "__main__":
    main()
