#!/usr/bin/env python3
"""YiGate 管理 API 回归测试。

覆盖鉴权矩阵、静态 WebUI 响应头、健康/指标/状态端点、upstreams 与 rules
的增删改查及校验、users、settings、geosite、reload 等方法与状态码契约。

只依赖 Python 标准库，默认连接 ``scripts/run-management-api-e2e.sh`` 启动的
临时实例；也可直接对任意已运行实例执行：

    YIGATE_API_BASE=http://127.0.0.1:8894 python3 test/e2e/test_management_api.py

注意：本脚本会写入配置（随后由调用方删除临时实例），不要指向生产实例。
暴力破解封禁用例会封禁来源 IP 数分钟，仅在 RUN_BAN=1 时执行。
"""

from __future__ import annotations

import json
import os
import re
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("YIGATE_API_BASE", "http://127.0.0.1:8894").rstrip("/")
TOKEN = os.environ.get("YIGATE_API_TOKEN", "yigate-webui-e2e-token")
CONFIG_PATH = os.environ.get("YIGATE_API_CONFIG", "")
RUN_BAN = os.environ.get("RUN_BAN") == "1"
TIMEOUT = float(os.environ.get("YIGATE_API_TIMEOUT", "30"))

AUTH = {"Authorization": f"Bearer {TOKEN}"}
results: list[tuple[str, bool, str]] = []
skipped: list[str] = []


class Response:
    def __init__(self, status: int, headers, body: bytes) -> None:
        self.status_code = status
        self.headers = {k.lower(): v for k, v in (headers or {}).items()}
        self.body = body

    @property
    def text(self) -> str:
        return self.body.decode("utf-8", "replace")

    def json(self):
        try:
            return json.loads(self.body)
        except ValueError:
            return {}


def call(method: str, path: str, headers=None, json_body=None, raw: bool = False) -> Response:
    hdrs = dict(headers or {})
    data = None
    if json_body is not None:
        data = json.dumps(json_body).encode("utf-8")
        hdrs.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(BASE + path, data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            return Response(resp.status, resp.headers, resp.read())
    except urllib.error.HTTPError as exc:
        return Response(exc.code, exc.headers, exc.read())


def check(name: str, cond, detail="") -> None:
    ok = bool(cond)
    results.append((name, ok, str(detail)))
    print(("PASS " if ok else "FAIL ") + name + (f" :: {detail}" if detail and not ok else ""))


def skip(name: str, reason: str) -> None:
    skipped.append(name)
    print(f"SKIP {name} :: {reason}")


def get(path: str, **kw):
    return call("GET", path, **kw)


def auth_get(path: str):
    return get(path, headers=AUTH)


def main() -> int:
    # ---------- 鉴权矩阵 ----------
    anon = get("/api/v1/status")
    check("auth: 无 Token 访问 /api/v1/status 返回 401", anon.status_code == 401, anon.status_code)
    bad = get("/api/v1/status", headers={"Authorization": "Bearer wrong"})
    check("auth: 错误 Token 返回 401", bad.status_code == 401, bad.status_code)
    for path in ["/health", "/metrics", "/api/v1/traffic", "/api/v1/config/upstreams", "/api/v1/geosite"]:
        resp = get(path)
        check(f"auth: 匿名 {path} -> 401", resp.status_code == 401, resp.status_code)

    # ---------- 静态 WebUI ----------
    resp = get("/login")
    check(
        "webui: /login SPA 回退 200 html",
        resp.status_code == 200 and "text/html" in resp.headers.get("content-type", ""),
        (resp.status_code, resp.headers.get("content-type")),
    )
    check("webui: 安全头 nosniff", resp.headers.get("x-content-type-options") == "nosniff",
          resp.headers.get("x-content-type-options"))
    check("webui: 安全头 X-Frame-Options", resp.headers.get("x-frame-options") == "DENY",
          resp.headers.get("x-frame-options"))
    check("webui: HTML 不允许缓存", "no-cache" in resp.headers.get("cache-control", ""),
          resp.headers.get("cache-control"))

    hashed_assets = re.findall(r'(?:src|href)="(/assets/[^"]+)"', resp.text)
    check("webui: HTML 引用带哈希的静态资源", len(hashed_assets) > 0, len(hashed_assets))
    if hashed_assets:
        asset = get(hashed_assets[0])
        cache_control = asset.headers.get("cache-control", "")
        check(
            "webui: 哈希资源长期 immutable 缓存",
            asset.status_code == 200
            and "immutable" in cache_control
            and "max-age=31536000" in cache_control,
            (asset.status_code, cache_control),
        )

    resp = get("/assets/not-exist.js")
    check("webui: 缺失静态资源返回 404", resp.status_code == 404, resp.status_code)

    # ---------- 基础信息 ----------
    resp = auth_get("/health")
    health = resp.json()
    check("health: 200 且 status=up", resp.status_code == 200 and health.get("status") == "up",
          (resp.status_code, health))
    check("health: 含 router/manager ready", "router_ready" in health and "manager_ready" in health, health)

    resp = auth_get("/metrics")
    check("metrics: 200 且含 yigate_ 指标", resp.status_code == 200 and "yigate_" in resp.text, resp.status_code)
    check("metrics: no-store 缓存头", resp.headers.get("cache-control") == "no-store",
          resp.headers.get("cache-control"))

    resp = auth_get("/api/v1/status")
    status = resp.json()
    required = [
        "version",
        "uptime_seconds",
        "upstream_count",
        "listen",
        "management",
        "pprof_enabled",
        "webui_enabled",
    ]
    check("status: 200 且含关键字段", resp.status_code == 200 and all(k in status for k in required),
          list(status)[:12])

    resp = auth_get("/api/v1/version")
    check("version: 200 且含 version", resp.status_code == 200 and "version" in resp.json(), resp.text[:120])

    resp = auth_get("/api/v1/traffic")
    check("traffic: 200 且含 enabled/entries",
          resp.status_code == 200 and "enabled" in resp.json() and "entries" in resp.json(), resp.text[:200])

    for path in ["/health", "/api/v1/status", "/api/v1/traffic"]:
        resp = call("POST", path, headers=AUTH)
        check(f"method: POST {path} -> 405", resp.status_code == 405, resp.status_code)

    # ---------- upstreams CRUD ----------
    resp = auth_get("/api/v1/config/upstreams")
    check("upstreams: 列表结构", resp.status_code == 200 and "upstreams" in resp.json(), resp.text[:120])

    payload = {"name": "qa-up", "url": "http://127.0.0.1:18080", "types": ["http"], "priority": 2, "weight": 3}
    resp = call("POST", "/api/v1/config/upstreams", headers=AUTH, json_body=payload)
    check("upstreams: 新增成功", resp.status_code in (200, 201), (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/upstreams", headers=AUTH, json_body=payload)
    check("upstreams: 重名新增被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/upstreams", headers=AUTH,
                json_body={"name": "bad", "url": "ftp://x", "types": ["http"]})
    check("upstreams: 非法协议被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/upstreams", headers=AUTH,
                json_body={"name": "bad2", "url": "http://x:1", "types": ["quic"]})
    check("upstreams: 非法类型被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))

    resp = auth_get("/api/v1/config/upstreams")
    listed = [u for u in resp.json().get("upstreams", []) if u.get("name") == "qa-up"]
    check("upstreams: 新增项出现在列表", len(listed) == 1, resp.json().get("upstreams"))

    resp = call("PUT", "/api/v1/config/upstreams/qa-up", headers=AUTH, json_body={"enabled": False})
    check("upstreams: 局部更新 enabled=false", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = auth_get("/api/v1/config/upstreams")
    listed = [u for u in resp.json().get("upstreams", []) if u.get("name") == "qa-up"]
    check("upstreams: enabled 已生效", listed and listed[0].get("enabled") is False, listed)

    resp = call("PUT", "/api/v1/config/upstreams/qa-up", headers=AUTH,
                json_body={"name": "qa-up2", "enabled": True})
    check("upstreams: 重命名成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))

    resp = call("POST", "/api/v1/config/upstreams/qa-up2/check", headers=AUTH)
    check("upstreams: 单项检查返回结果", resp.status_code == 200 and "result" in resp.json(),
          (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/upstreams/check", headers=AUTH)
    check("upstreams: 全量巡检返回 results", resp.status_code == 200 and "results" in resp.json(),
          (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/upstreams/not-exist/check", headers=AUTH)
    check("upstreams: 检查不存在项 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:200]))
    check("upstreams: 不存在项返回错误码", resp.json().get("code") == "upstream_not_found", resp.text[:200])

    resp = call("DELETE", "/api/v1/config/upstreams/qa-up2", headers=AUTH)
    check("upstreams: 删除成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("DELETE", "/api/v1/config/upstreams/qa-up2", headers=AUTH)
    check("upstreams: 重复删除 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:120]))

    # ---------- rules CRUD ----------
    resp = auth_get("/api/v1/config/rules")
    check("rules: 列表结构", resp.status_code == 200 and "rules" in resp.json(), resp.text[:120])

    rule = {"type": "domain_suffix", "values": ["qa.example"], "action": "direct"}
    resp = call("POST", "/api/v1/config/rules", headers=AUTH, json_body=rule)
    check("rules: 新增成功", resp.status_code in (200, 201), (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/rules", headers=AUTH,
                json_body={"type": "nope", "values": ["x"], "action": "direct"})
    check("rules: 非法类型被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/rules", headers=AUTH,
                json_body={"type": "domain", "values": ["x"], "action": "nope"})
    check("rules: 非法动作被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/rules", headers=AUTH,
                json_body={"type": "domain", "values": [], "action": "direct"})
    check("rules: 空 values 被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))

    resp = auth_get("/api/v1/config/rules")
    count = len(resp.json().get("rules", []))
    check("rules: 新增项已入库", count == 1, count)

    resp = call("PUT", "/api/v1/config/rules/0", headers=AUTH,
                json_body={"type": "domain_keyword", "values": ["qa"], "action": "proxy"})
    check("rules: 更新成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("PUT", "/api/v1/config/rules/99", headers=AUTH, json_body=rule)
    check("rules: 越界更新 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:120]))
    resp = call("POST", "/api/v1/config/rules/0/move", headers=AUTH, json_body={"direction": "down"})
    check("rules: 末项下移 -> 400", resp.status_code == 400, (resp.status_code, resp.text[:120]))
    resp = call("POST", "/api/v1/config/rules/0/move", headers=AUTH, json_body={"direction": "sideways"})
    check("rules: 非法方向 -> 400", resp.status_code == 400, (resp.status_code, resp.text[:120]))
    resp = call("DELETE", "/api/v1/config/rules/0", headers=AUTH)
    check("rules: 删除成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("DELETE", "/api/v1/config/rules/0", headers=AUTH)
    check("rules: 重复删除 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:120]))

    # ---------- users CRUD ----------
    resp = auth_get("/api/v1/config/users")
    check("users: 列表结构", resp.status_code == 200 and "users" in resp.json(), resp.text[:120])
    resp = call("POST", "/api/v1/config/users", headers=AUTH,
                json_body={"username": "qa-user", "password": "p@ss1234"})
    check("users: 新增成功", resp.status_code in (200, 201), (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/users", headers=AUTH,
                json_body={"username": "qa-user", "password": "p@ss1234"})
    check("users: 重名新增被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("POST", "/api/v1/config/users", headers=AUTH,
                json_body={"username": "qa-user2", "password": ""})
    check("users: 空密码被拒绝", resp.status_code == 400, (resp.status_code, resp.text[:200]))
    resp = call("PUT", "/api/v1/config/users/qa-user", headers=AUTH, json_body={"password": "newp@ss5678"})
    check("users: 改密成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("PUT", "/api/v1/config/users/not-exist", headers=AUTH, json_body={"password": "x12345"})
    check("users: 改密不存在用户 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:120]))
    resp = call("DELETE", "/api/v1/config/users/qa-user", headers=AUTH)
    check("users: 删除成功", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("DELETE", "/api/v1/config/users/qa-user", headers=AUTH)
    check("users: 重复删除 -> 404", resp.status_code == 404, (resp.status_code, resp.text[:120]))

    # ---------- settings ----------
    resp = auth_get("/api/v1/config/settings")
    check("settings: 读取结构", resp.status_code == 200 and "webui_enabled" in resp.json(), resp.text[:200])
    settings_text = json.dumps(resp.json(), ensure_ascii=False)
    check("settings: 敏感字段不泄漏 Token",
          "management_auth_token" not in settings_text or "****" in settings_text, list(resp.json())[:20])
    resp = call("PUT", "/api/v1/config/settings", headers=AUTH, json_body={})
    check("settings: 空 patch 幂等 200", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("PUT", "/api/v1/config/settings", headers=AUTH, json_body={"webui_enabled": True})
    check("settings: 显式置真 200", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = call("PUT", "/api/v1/config/settings", headers=AUTH, json_body={"unknown_field": 1})
    check("settings: 未知字段被忽略或 400", resp.status_code in (200, 400), resp.status_code)

    # ---------- geosite ----------
    resp = auth_get("/api/v1/geosite")
    check("geosite: 信息 200", resp.status_code == 200, (resp.status_code, resp.text[:200]))
    resp = auth_get("/api/v1/geosite/sync-token")
    check("geosite: 同步令牌 200", resp.status_code == 200, (resp.status_code, resp.text[:160]))
    resp = auth_get("/api/v1/geosite/dat")
    check("geosite: dat 返回文件或明确错误", resp.status_code in (200, 404, 409), resp.status_code)
    resp = call("POST", "/api/v1/geosite/update", headers=AUTH)
    check("geosite: update 返回结构化结果",
          "status" in resp.text or "error" in resp.text.lower(), (resp.status_code, resp.text[:120]))
    resp = auth_get("/api/v1/geosite/unknown")
    check("geosite: 未知子路径 -> 404", resp.status_code == 404, resp.status_code)

    # ---------- pprof（E2E 配置未启用） ----------
    resp = auth_get("/debug/pprof/")
    check("pprof: 未启用时不可达", resp.status_code == 404, resp.status_code)

    # ---------- reload ----------
    resp = call("POST", "/api/v1/reload", headers=AUTH)
    check("reload: 手动重载 200", resp.status_code == 200 and resp.json().get("status") == "ok",
          (resp.status_code, resp.text[:160]))
    resp = auth_get("/api/v1/reload")
    check("reload: GET -> 405", resp.status_code == 405, resp.status_code)

    # ---------- 暴力破解封禁（会封禁来源 IP 数分钟，默认关闭） ----------
    if RUN_BAN:
        for _ in range(10):
            get("/health", headers={"Authorization": "Bearer wrong"})
        banned = get("/health", headers=AUTH)
        check("auth: 连续失败后正确 Token 也被临时封禁", banned.status_code == 401, banned.status_code)
    else:
        skip("auth: 暴力破解临时封禁", "需要 RUN_BAN=1（会封禁来源 IP 数分钟）")

    # ---------- 配置持久化 ----------
    if CONFIG_PATH and os.path.isfile(CONFIG_PATH):
        with open(CONFIG_PATH, encoding="utf-8") as fh:
            config_text = fh.read()
        check("persist: 配置写回 YAML 且保留 Token",
              TOKEN in config_text and "management_auth_token" in config_text, config_text[:80])
    else:
        skip("persist: 配置写回 YAML", "未设置可读的 YIGATE_API_CONFIG")

    failed = [name for name, ok, _ in results if not ok]
    passed = len(results) - len(failed)
    print(f"\n{passed}/{len(results)} passed, {len(skipped)} skipped")
    if failed:
        print("FAILED:", failed)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
