#!/usr/bin/env python3
"""Runtime API smoke checks for an already-running Easy Proxies WebUI.

This intentionally exercises the high-risk control-plane path without changing
runtime configuration values:
- login/auth gate
- settings read
- same-value settings save (must not trigger reload/refresh)
- reload status endpoint
- free-proxy refresh endpoint when no enabled sources, or safe start response
- nodes summary endpoint
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from http.cookiejar import CookieJar
from typing import Any


class RuntimeSmokeError(RuntimeError):
    pass


BASE_URL = os.environ.get("EP_SMOKE_BASE_URL", "http://127.0.0.1:19093").rstrip("/")
PASSWORD = os.environ.get("EP_SMOKE_PASSWORD", os.environ.get("WEBUI_PASSWORD", "runtime-partial-secret"))
TIMEOUT = float(os.environ.get("EP_SMOKE_TIMEOUT", "20"))
POLL_SECONDS = float(os.environ.get("EP_SMOKE_POLL_SECONDS", "20"))
SKIP_RELOAD = os.environ.get("EP_SMOKE_SKIP_RELOAD", "").lower() in {"1", "true", "yes"}
ALLOW_NO_PASSWORD = os.environ.get("EP_SMOKE_ALLOW_NO_PASSWORD", "").lower() in {"1", "true", "yes"}


def request(opener: urllib.request.OpenerDirector, method: str, path: str, payload: Any | None = None) -> tuple[int, Any]:
    url = f"{BASE_URL}{path}"
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with opener.open(req, timeout=TIMEOUT) as resp:
            body = resp.read()
            return resp.status, parse_body(resp.headers.get("Content-Type", ""), body)
    except urllib.error.HTTPError as exc:
        body = exc.read()
        return exc.code, parse_body(exc.headers.get("Content-Type", ""), body)


def parse_body(content_type: str, body: bytes) -> Any:
    text = body.decode("utf-8", errors="replace")
    if "application/json" in content_type or text.lstrip().startswith(("{", "[")):
        try:
            return json.loads(text)
        except json.JSONDecodeError:
            return text
    return text


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeSmokeError(message)


def login(opener: urllib.request.OpenerDirector) -> None:
    code, payload = request(opener, "POST", "/api/auth", {"password": PASSWORD})
    require(code == 200, f"auth failed HTTP {code}: {payload!r}")
    require(isinstance(payload, dict), f"auth returned non-object payload: {payload!r}")
    require(payload.get("token") or payload.get("no_password") or payload.get("message"), f"auth payload missing expected fields: {payload!r}")
    print(f"auth: HTTP {code} {payload.get('message', '')}")


def check_auth_negative_paths() -> None:
    if ALLOW_NO_PASSWORD:
        print("auth-negative: skipped by EP_SMOKE_ALLOW_NO_PASSWORD")
        return
    anonymous = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
    code, payload = request(anonymous, "GET", "/api/settings")
    require(code == 401, f"unauthenticated settings access should be rejected, got HTTP {code}: {payload!r}")
    wrong = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
    code, payload = request(wrong, "POST", "/api/auth", {"password": f"{PASSWORD}-wrong"})
    require(code == 401, f"wrong password should be rejected, got HTTP {code}: {payload!r}")
    print("auth-negative: unauthenticated settings and wrong password rejected")


def check_same_value_save(opener: urllib.request.OpenerDirector) -> dict[str, Any]:
    code, settings = request(opener, "GET", "/api/settings")
    require(code == 200 and isinstance(settings, dict), f"GET /api/settings failed HTTP {code}: {settings!r}")
    code, saved = request(opener, "PUT", "/api/settings", settings)
    require(code == 200 and isinstance(saved, dict), f"PUT /api/settings failed HTTP {code}: {saved!r}")
    triggered = [
        key for key in (
            "need_reload",
            "reload_started",
            "free_proxy_refresh_needed",
            "free_proxy_refresh_started",
            "subscription_refresh_started",
            "management_rebound",
        )
        if saved.get(key)
    ]
    require(not triggered, f"same-value save unexpectedly triggered reload/refresh: {triggered} payload={saved!r}")
    print("settings: same-value save did not trigger reload/refresh")
    return settings


def check_status_endpoints(opener: urllib.request.OpenerDirector) -> None:
    for path in ("/api/reload/status", "/api/free-proxy/refresh/status", "/api/nodes?summary_only=true"):
        code, payload = request(opener, "GET", path)
        require(code == 200 and isinstance(payload, dict), f"GET {path} failed HTTP {code}: {payload!r}")
        print(f"{path}: HTTP {code}")


def check_manual_reload(opener: urllib.request.OpenerDirector) -> None:
    if SKIP_RELOAD:
        print("manual-reload: skipped by EP_SMOKE_SKIP_RELOAD")
        return
    code, payload = request(opener, "POST", "/api/reload")
    require(code == 200 and isinstance(payload, dict), f"POST /api/reload failed HTTP {code}: {payload!r}")
    status = payload.get("reload_status") if isinstance(payload.get("reload_status"), dict) else {}
    started = bool(payload.get("started"))
    pending = bool(status.get("reload_pending"))
    require(started or pending, f"manual reload neither started nor queued: {payload!r}")
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", "/api/reload/status")
        require(code == 200 and isinstance(current, dict), f"GET reload status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"succeeded", "failed"}:
            require(state != "failed", f"manual reload failed: {current!r}")
            print(f"manual-reload: succeeded duration_ms={current.get('duration_ms')}")
            return
        time.sleep(0.5)
    raise RuntimeSmokeError("manual reload did not finish within poll window")


def check_free_proxy_refresh(opener: urllib.request.OpenerDirector) -> None:
    code, payload = request(opener, "POST", "/api/free-proxy/refresh")
    require(code == 200 and isinstance(payload, dict), f"POST /api/free-proxy/refresh failed HTTP {code}: {payload!r}")
    status = payload.get("status") if isinstance(payload.get("status"), dict) else {}
    started = bool(payload.get("started"))
    if not started:
        require(str(status.get("state", "")) in {"idle", "disabled", "running", "succeeded"}, f"unexpected free refresh idle status: {payload!r}")
        print(f"free-proxy-refresh: not started ({payload.get('message', '')})")
        return
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", "/api/free-proxy/refresh/status")
        require(code == 200 and isinstance(current, dict), f"GET free refresh status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"succeeded", "failed"}:
            require(state != "failed", f"free proxy refresh failed: {current!r}")
            print(f"free-proxy-refresh: succeeded accepted={current.get('accepted')} cache_updated={current.get('cache_updated')}")
            return
        time.sleep(0.5)
    raise RuntimeSmokeError("free proxy refresh did not finish within poll window")


def main() -> int:
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
    try:
        check_auth_negative_paths()
        login(opener)
        check_same_value_save(opener)
        check_status_endpoints(opener)
        check_manual_reload(opener)
        check_free_proxy_refresh(opener)
    except RuntimeSmokeError as exc:
        print(f"SMOKE FAILED: {exc}", file=sys.stderr)
        return 1
    print("runtime API smoke: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
