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
import socket
import sys
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from http.cookiejar import CookieJar
from typing import Any


class RuntimeSmokeError(RuntimeError):
    pass


BASE_URL = os.environ.get("EP_SMOKE_BASE_URL", "http://127.0.0.1:19093").rstrip("/")
PASSWORD = os.environ.get("EP_SMOKE_PASSWORD", os.environ.get("WEBUI_PASSWORD", "ep123"))
TIMEOUT = float(os.environ.get("EP_SMOKE_TIMEOUT", "20"))
POLL_SECONDS = float(os.environ.get("EP_SMOKE_POLL_SECONDS", "20"))
SKIP_RELOAD = os.environ.get("EP_SMOKE_SKIP_RELOAD", "").lower() in {"1", "true", "yes"}
SKIP_FREE_PROXY_REFRESH = os.environ.get("EP_SMOKE_SKIP_FREE_PROXY_REFRESH", "").lower() in {"1", "true", "yes"}
ALLOW_NO_PASSWORD = os.environ.get("EP_SMOKE_ALLOW_NO_PASSWORD", "").lower() in {"1", "true", "yes"}
FREE_PROXY_FIXTURE = os.environ.get("EP_SMOKE_FREE_PROXY_FIXTURE", "").lower() in {"1", "true", "yes"}
ALLOW_MAIN_PORT = os.environ.get("EP_SMOKE_ALLOW_MAIN_PORT", "").lower() in {"1", "true", "yes"}
FREE_PROXY_FIXTURE_BASELINE: dict[str, Any] | None = None


def print_help() -> None:
    print(
        """Runtime API smoke checks for an already-running Easy Proxies WebUI.

Usage:
  EP_SMOKE_BASE_URL=http://127.0.0.1:20020 EP_SMOKE_PASSWORD=ep123 python3 scripts/smoke_runtime_api.py

Environment:
  EP_SMOKE_BASE_URL           WebUI base URL. Default: http://127.0.0.1:19093
  EP_SMOKE_PASSWORD           WebUI password. Default: WEBUI_PASSWORD or ep123
  EP_SMOKE_TIMEOUT            Per-request timeout seconds. Default: 20
  EP_SMOKE_POLL_SECONDS       Background operation poll budget. Default: 20
  EP_SMOKE_SKIP_RELOAD        Skip manual reload path when set to 1/true/yes
  EP_SMOKE_SKIP_FREE_PROXY_REFRESH
                               Skip manual free-proxy refresh when set to 1/true/yes
  EP_SMOKE_FREE_PROXY_FIXTURE Exercise temporary free-proxy source add/restore when set to 1/true/yes
  EP_SMOKE_ALLOW_MAIN_PORT    Allow mutating checks against port 9091 when set to 1/true/yes
"""
    )


def request(opener: urllib.request.OpenerDirector, method: str, path: str, payload: Any | None = None, *, retry_connect: bool = False) -> tuple[int, Any]:
    url = f"{BASE_URL}{path}"
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    attempts = 4 if retry_connect else 1
    for attempt in range(1, attempts + 1):
        try:
            with opener.open(req, timeout=TIMEOUT) as resp:
                body = resp.read()
                return resp.status, parse_body(resp.headers.get("Content-Type", ""), body)
        except urllib.error.HTTPError as exc:
            body = exc.read()
            return exc.code, parse_body(exc.headers.get("Content-Type", ""), body)
        except urllib.error.URLError as exc:
            if not retry_connect or attempt >= attempts:
                raise RuntimeSmokeError(f"{method} {path} connection failed: {exc}") from exc
            time.sleep(0.5 * attempt)
    raise RuntimeSmokeError(f"request retry exhausted: {method} {path}")


def make_control_opener() -> urllib.request.OpenerDirector:
    """Build an opener for WebUI control-plane requests.

    urllib honors HTTP_PROXY/HTTPS_PROXY from the environment by default. That
    is correct for normal outbound traffic, but it makes localhost smoke checks
    flaky: some proxy implementations do not understand no_proxy patterns such
    as "127.*" and return their own 502 before the request reaches WebUI.
    Control-plane probes must always talk directly to BASE_URL.
    """

    return urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPCookieProcessor(CookieJar()),
    )


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


def assert_safe_smoke_target() -> None:
    parsed = urllib.parse.urlparse(BASE_URL)
    if parsed.port == 9091 and not ALLOW_MAIN_PORT:
        raise RuntimeSmokeError(
            "Refusing to run mutating smoke checks against port 9091. "
            "Use the isolated WebUI port 19093, or set EP_SMOKE_ALLOW_MAIN_PORT=1 explicitly."
        )


def wait_for_webui_ready() -> None:
    opener = make_control_opener()
    deadline = time.time() + POLL_SECONDS
    last_error: Exception | None = None
    while time.time() < deadline:
        try:
            code, payload = request(opener, "GET", "/api/auth/status", retry_connect=True)
            if code == 200 and isinstance(payload, dict):
                return
            last_error = RuntimeSmokeError(f"unexpected ready probe HTTP {code}: {payload!r}")
        except urllib.error.URLError as exc:
            last_error = exc
        time.sleep(0.5)
    raise RuntimeSmokeError(f"WebUI did not become ready within {POLL_SECONDS}s: {last_error!r}")


def login(opener: urllib.request.OpenerDirector) -> None:
    code, payload = request(opener, "POST", "/api/auth", {"password": PASSWORD})
    require(code == 200, f"auth failed HTTP {code}: {payload!r}")
    require(isinstance(payload, dict), f"auth returned non-object payload: {payload!r}")
    require(payload.get("token") or payload.get("no_password") or payload.get("message"), f"auth payload missing expected fields: {payload!r}")
    print(f"auth: HTTP {code} {payload.get('message', '')}")


def check_auth_status_probe(opener: urllib.request.OpenerDirector | None = None, *, expected_authenticated: bool) -> None:
    probe = opener or make_control_opener()
    code, payload = request(probe, "GET", "/api/auth/status")
    require(code == 200, f"auth status probe should not return 401 or other errors, got HTTP {code}: {payload!r}")
    require(isinstance(payload, dict), f"auth status returned non-object payload: {payload!r}")
    actual = bool(payload.get("authenticated"))
    require(actual is expected_authenticated, f"auth status authenticated={actual}, want {expected_authenticated}: {payload!r}")
    print(f"auth-status: authenticated={actual}")


def check_auth_negative_paths() -> None:
    if ALLOW_NO_PASSWORD:
        print("auth-negative: skipped by EP_SMOKE_ALLOW_NO_PASSWORD")
        return
    check_auth_status_probe(expected_authenticated=False)
    anonymous = make_control_opener()
    code, payload = request(anonymous, "GET", "/api/settings")
    require(code == 401, f"unauthenticated settings access should be rejected, got HTTP {code}: {payload!r}")
    wrong = make_control_opener()
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
    check_auth_status_probe(opener, expected_authenticated=True)
    print("settings: same-value save did not trigger reload/refresh or invalidate session")
    return settings


def save_settings(opener: urllib.request.OpenerDirector, settings: dict[str, Any]) -> dict[str, Any]:
    code, saved = request(opener, "PUT", "/api/settings", settings)
    require(code == 200 and isinstance(saved, dict), f"PUT /api/settings failed HTTP {code}: {saved!r}")
    return saved


def restore_settings(opener: urllib.request.OpenerDirector, original: dict[str, Any]) -> None:
    saved = save_settings(opener, original)
    error = saved.get("free_proxy_refresh_error") or saved.get("reload_error")
    require(not error, f"restoring settings reported error: {saved!r}")
    print("settings: restored original runtime configuration")




def tcp_connects(host: str, port: int, timeout: float = 0.3) -> bool:
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except OSError:
        return False



def wait_for_tcp_port(host: str, port: int, label: str, *, seconds: float = 15) -> None:
    deadline = time.time() + seconds
    while time.time() < deadline:
        if tcp_connects(host, port):
            return
        time.sleep(0.5)
    raise RuntimeSmokeError(f"{label} port {host}:{port} did not start listening within {seconds}s")


def multi_port_wait_seconds() -> float:
    return max(45.0, POLL_SECONDS * 3)


def wait_for_listening_multi_port(opener: urllib.request.OpenerDirector, host: str, *, seconds: float | None = None) -> int:
    wait_seconds = multi_port_wait_seconds() if seconds is None else seconds
    deadline = time.time() + wait_seconds
    last_ports: list[int] = []
    while time.time() < deadline:
        code, page = request(opener, "GET", "/api/nodes?page=1&page_size=50&sort=port")
        if code == 200 and isinstance(page, dict):
            ports = [int(node.get("port") or 0) for node in (page.get("nodes") or []) if int(node.get("port") or 0) > 0]
            last_ports = ports[:]
            for port in ports:
                if tcp_connects(host, port):
                    return port
        time.sleep(0.5)
    raise RuntimeSmokeError(f"no listening multi-port endpoint found within {wait_seconds}s; last_ports={last_ports[:10]}")


def curl_proxy_status(proxy_url: str, *, timeout: float = 6) -> tuple[int, str]:
    proc = subprocess.run(
        [
            "curl",
            "--noproxy",
            "",
            "-x",
            proxy_url,
            "-s",
            "-o",
            os.devnull,
            "-w",
            "%{http_code} %{errormsg}",
            "--max-time",
            str(timeout),
            "http://cp.cloudflare.com/generate_204",
        ],
        capture_output=True,
        text=True,
        timeout=timeout + 2,
    )
    parts = proc.stdout.strip().split(maxsplit=1)
    code = int(parts[0]) if parts and parts[0].isdigit() else 0
    return code, proc.stdout.strip()


def proxy_auth_url(scheme: str, host: str, port: int, username: str | None = None, password: str | None = None) -> str:
    auth = ""
    if username is not None:
        auth = f"{urllib.parse.quote(username, safe='')}:{urllib.parse.quote(password or '', safe='')}@"
    return f"{scheme}://{auth}{host}:{port}"


def check_proxy_auth_runtime(opener: urllib.request.OpenerDirector, settings: dict[str, Any]) -> None:
    listener = settings.get("listener") or {}
    multi_port = settings.get("multi_port") or {}
    geoip = settings.get("geoip") or {}
    listener_user = str(listener.get("username") or "")
    listener_pass = str(listener.get("password") or "")
    multi_user = str(multi_port.get("username") or "")
    multi_pass = str(multi_port.get("password") or "")
    checked: list[str] = []

    if listener_user and int(listener.get("port") or 0):
        host = str(listener.get("address") or "127.0.0.1")
        port = int(listener.get("port"))
        wait_for_tcp_port(host, port, "pool")
        for label, proxy in (
            ("pool_bad", proxy_auth_url("http", host, port, "bad", "bad")),
            ("pool_noauth", proxy_auth_url("http", host, port)),
        ):
            code, out = curl_proxy_status(proxy)
            require(code == 407, f"{label} should be rejected with 407, got {out!r}")
            checked.append(label)
        good_code, good_out = curl_proxy_status(proxy_auth_url("http", host, port, listener_user, listener_pass))
        require(good_code != 407, f"pool_good should pass auth gate, got {good_out!r}")
        checked.append("pool_good")

    if multi_user:
        host = str(multi_port.get("address") or "127.0.0.1")
        port = wait_for_listening_multi_port(opener, host)
        for label, proxy in (
            ("multi_bad", proxy_auth_url("http", host, port, "bad", "bad")),
            ("multi_noauth", proxy_auth_url("http", host, port)),
        ):
            code, out = curl_proxy_status(proxy)
            require(code == 407, f"{label} should be rejected with 407, got {out!r}")
            checked.append(label)
        good_code, good_out = curl_proxy_status(proxy_auth_url("http", host, port, multi_user, multi_pass))
        require(good_code != 407, f"multi_good should pass auth gate, got {good_out!r}")
        checked.append("multi_good")

    if listener_user and bool(geoip.get("enabled")) and int(geoip.get("port") or 0):
        host = str(geoip.get("listen") or listener.get("address") or "127.0.0.1")
        port = int(geoip.get("port"))
        wait_for_tcp_port(host, port, "geoip")
        for label, proxy in (
            ("geoip_bad", proxy_auth_url("http", host, port, "bad", "bad")),
            ("geoip_noauth", proxy_auth_url("http", host, port)),
        ):
            code, out = curl_proxy_status(proxy)
            require(code == 407, f"{label} should be rejected with 407, got {out!r}")
            checked.append(label)
        good_code, good_out = curl_proxy_status(proxy_auth_url("http", host, port, listener_user, listener_pass))
        require(good_code != 407, f"geoip_good should pass auth gate, got {good_out!r}")
        checked.append("geoip_good")
        region_code, region_out = curl_proxy_status(proxy_auth_url("http", host, port, f"{listener_user}-us", listener_pass))
        require(region_code != 407, f"geoip_region_good should pass auth gate with username suffix, got {region_out!r}")
        checked.append("geoip_region_good")

    if checked:
        print(f"proxy-auth: checked {', '.join(checked)}")
    else:
        print("proxy-auth: skipped (no configured proxy credentials/ports)")


def check_status_endpoints(opener: urllib.request.OpenerDirector) -> None:
    for path in ("/api/reload/status", "/api/free-proxy/refresh/status", "/api/nodes?summary_only=true"):
        code, payload = request(opener, "GET", path)
        require(code == 200 and isinstance(payload, dict), f"GET {path} failed HTTP {code}: {payload!r}")
        print(f"{path}: HTTP {code}")


def check_debug_and_logs(opener: urllib.request.OpenerDirector) -> None:
    code, summary = request(opener, "GET", "/api/debug?summary_only=true")
    require(code == 200 and isinstance(summary, dict), f"GET debug summary failed HTTP {code}: {summary!r}")
    require("nodes" not in summary, f"debug summary should not include full nodes payload: {summary!r}")
    require("node_count" in summary and "success_rate" in summary, f"debug summary missing expected fields: {summary!r}")
    code, debug = request(opener, "GET", "/api/debug")
    require(code == 200 and isinstance(debug, dict), f"GET debug failed HTTP {code}: {debug!r}")
    require(isinstance(debug.get("nodes"), list), f"debug payload missing nodes list: {debug!r}")
    code, logs = request(opener, "GET", "/api/logs")
    require(code == 200 and isinstance(logs, dict), f"GET logs failed HTTP {code}: {logs!r}")
    require(isinstance(logs.get("logs"), str), f"logs payload missing logs string: {logs!r}")
    code, limited_logs = request(opener, "GET", "/api/logs?lines=20")
    require(code == 200 and isinstance(limited_logs, dict), f"GET limited logs failed HTTP {code}: {limited_logs!r}")
    limited_content = limited_logs.get("logs")
    require(isinstance(limited_content, str), f"limited logs payload missing logs string: {limited_logs!r}")
    require(len(limited_content.splitlines()) <= 20, f"limited logs returned more than 20 lines: {len(limited_content.splitlines())}")
    code, invalid_lines = request(opener, "GET", "/api/logs?lines=bad")
    require(code == 400 and isinstance(invalid_lines, dict) and invalid_lines.get("code") == "invalid_lines", f"invalid log lines should fail with structured error, got HTTP {code}: {invalid_lines!r}")
    code, invalid = request(opener, "GET", "/api/debug?summary_only=maybe")
    require(code == 400 and isinstance(invalid, dict) and invalid.get("code") == "invalid_bool", f"invalid debug summary_only should fail with structured error, got HTTP {code}: {invalid!r}")
    print("debug/logs: summary/full/logs, limited logs, and structured invalid params ok")



def check_config_node_crud(opener: urllib.request.OpenerDirector) -> None:
    suffix = str(int(time.time() * 1000))
    name = f"smoke-node-{suffix}"
    updated = f"smoke-node-updated-{suffix}"

    def cleanup(node_name: str) -> None:
        code, _ = request(opener, "DELETE", f"/api/nodes/config/{urllib.parse.quote(node_name)}", retry_connect=True)
        require(code in {200, 404}, f"cleanup config node {node_name} failed HTTP {code}")

    cleanup(name)
    cleanup(updated)
    try:
        create_payload = {"name": name, "uri": "http://127.0.0.1:1", "port": 0}
        code, created = request(opener, "POST", "/api/nodes/config", create_payload, retry_connect=True)
        require(code == 200 and isinstance(created, dict), f"create config node failed HTTP {code}: {created!r}")
        require(created.get("need_reload") is True, f"create config node should require reload: {created!r}")
        require((created.get("node") or {}).get("name") == name, f"created node name mismatch: {created!r}")

        update_payload = {"name": updated, "uri": "http://127.0.0.1:2", "port": 0}
        code, changed = request(opener, "PUT", f"/api/nodes/config/{urllib.parse.quote(name)}", update_payload, retry_connect=True)
        require(code == 200 and isinstance(changed, dict), f"update config node failed HTTP {code}: {changed!r}")
        require(changed.get("need_reload") is True, f"update config node should require reload: {changed!r}")
        require((changed.get("node") or {}).get("name") == updated, f"updated node name mismatch: {changed!r}")

        code, listed = request(opener, "GET", "/api/nodes/config", retry_connect=True)
        require(code == 200 and isinstance(listed, dict), f"list config nodes failed HTTP {code}: {listed!r}")
        nodes = listed.get("nodes") or []
        require(any(isinstance(node, dict) and node.get("name") == updated for node in nodes), f"updated config node not listed: {listed!r}")

        code, deleted = request(opener, "DELETE", f"/api/nodes/config/{urllib.parse.quote(updated)}", retry_connect=True)
        require(code in {200, 404}, f"delete config node failed HTTP {code}: {deleted!r}")
        if code == 200:
            require(isinstance(deleted, dict), f"delete config node returned non-object payload: {deleted!r}")
            require(deleted.get("need_reload") is True, f"delete config node should require reload: {deleted!r}")
        print("config-nodes: create/update/list/delete require manual reload and cleaned up")
    finally:
        cleanup(name)
        cleanup(updated)

def check_quality_paths(opener: urllib.request.OpenerDirector) -> None:
    for path in ("/api/cloudflare/cache", "/api/reputation/cache"):
        code, payload = request(opener, "GET", path)
        require(code == 200 and isinstance(payload, dict), f"GET {path} failed HTTP {code}: {payload!r}")
        require(isinstance(payload.get("data"), list), f"{path} payload missing data list: {payload!r}")
    invalid_source_paths = (
        "/api/cloudflare/check?region=all&mode=multi-port&count=1&source=bad_source",
        "/api/reputation/check?region=all&mode=multi-port&count=1&source=bad_source",
    )
    for path in invalid_source_paths:
        code, payload = request(opener, "GET", path)
        require(code == 400 and isinstance(payload, dict) and payload.get("code") == "invalid_source", f"{path} should reject invalid source, got HTTP {code}: {payload!r}")
    limit_paths = (
        "/api/cloudflare/check?region=all&mode=multi-port&count=51",
        "/api/reputation/check?region=all&mode=multi-port&count=6",
    )
    for path in limit_paths:
        code, payload = request(opener, "GET", path)
        require(code == 400 and isinstance(payload, dict) and payload.get("code") == "use_background", f"{path} should require background scan, got HTTP {code}: {payload!r}")

    code, job = request(opener, "POST", "/api/quality/jobs", {
        "kind": "pipeline",
        "region": "all",
        "mode": "multi-port",
        "source": "subscription",
        "count": 1,
        "include_unavailable": True,
        "replace": True,
    })
    require(code == 202 and isinstance(job, dict), f"POST quality job failed HTTP {code}: {job!r}")
    job_id = str(job.get("job_id") or "")
    require(job_id, f"quality job response missing job_id: {job!r}")
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", f"/api/quality/jobs/{urllib.parse.quote(job_id)}")
        require(code == 200 and isinstance(current, dict), f"GET quality job failed HTTP {code}: {current!r}")
        state = str(current.get("status", ""))
        if state in {"completed", "failed", "cancelled"}:
            require(state == "completed", f"quality job did not complete successfully: {current!r}")
            code, results = request(opener, "GET", f"/api/quality/jobs/{urllib.parse.quote(job_id)}/results?page=1&page_size=20")
            require(code == 200 and isinstance(results, dict) and isinstance(results.get("data"), list), f"GET quality job results failed HTTP {code}: {results!r}")
            print(f"quality: job completed id={job_id} results={len(results.get('data') or [])}")
            return
        time.sleep(0.5)
    raise RuntimeSmokeError(f"quality job {job_id} did not finish within poll window")


def check_extractor_paths(opener: urllib.request.OpenerDirector) -> None:
    cases = [
        ("/api/extractor?region=all&mode=multi-port&format=host_port_user_pass&count=3&reveal=true", "multi-port", 3),
        ("/api/extractor?region=all&mode=pool&format=http_url&count=1&reveal=false", "pool", 1),
        ("/api/extractor?region=all&mode=android&format=adb_command&count=1&reveal=true", "android", 1),
    ]
    for path, want_mode, want_count in cases:
        code, payload = request(opener, "GET", path)
        require(code == 200 and isinstance(payload, dict), f"GET {path} failed HTTP {code}: {payload!r}")
        entries = payload.get("entries")
        require(isinstance(entries, list), f"extractor payload missing entries for {path}: {payload!r}")
        require(str(payload.get("mode")) == want_mode, f"extractor mode mismatch for {path}: {payload!r}")
        require(len(entries) == want_count, f"extractor output count mismatch for {path}: entries={len(entries)} payload={payload!r}")
    code, payload = request(opener, "GET", "/api/extractor?region=moon&mode=multi-port&format=json&count=1&reveal=true")
    require(code == 400 and isinstance(payload, dict) and payload.get("code") == "invalid_region", f"invalid extractor request should fail with structured error, got HTTP {code}: {payload!r}")
    print("extractor: multi-port/pool/android and structured error paths ok")


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
        code, current = request(opener, "GET", "/api/reload/status", retry_connect=True)
        require(code == 200 and isinstance(current, dict), f"GET reload status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"succeeded", "failed"}:
            require(state != "failed", f"manual reload failed: {current!r}")
            print(f"manual-reload: succeeded duration_ms={current.get('duration_ms')}")
            return
        time.sleep(0.5)
    raise RuntimeSmokeError("manual reload did not finish within poll window")


def wait_for_reload_settled(opener: urllib.request.OpenerDirector, context: str) -> dict[str, Any]:
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", "/api/reload/status", retry_connect=True)
        require(code == 200 and isinstance(current, dict), f"{context} reload status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"idle", "succeeded", "failed"} and not current.get("reload_pending"):
            require(state != "failed", f"{context} reload failed: {current!r}")
            return current
        time.sleep(0.5)
    raise RuntimeSmokeError(f"{context} reload did not settle within poll window")


def fetch_nodes_summary(opener: urllib.request.OpenerDirector) -> dict[str, Any]:
    code, payload = request(opener, "GET", "/api/nodes?summary_only=true&availability=all", retry_connect=True)
    require(code == 200 and isinstance(payload, dict), f"GET nodes summary failed HTTP {code}: {payload!r}")
    return payload


def wait_for_free_proxy_runtime_count(opener: urllib.request.OpenerDirector, expected: int, context: str) -> dict[str, Any]:
    deadline = time.time() + POLL_SECONDS
    last_summary: dict[str, Any] | None = None
    while time.time() < deadline:
        last_summary = fetch_nodes_summary(opener)
        free_nodes = int((last_summary.get("source_stats") or {}).get("free_proxy") or 0)
        if free_nodes == expected:
            return last_summary
        time.sleep(0.5)
    raise RuntimeSmokeError(f"{context} free proxy runtime count did not reach {expected}: last={last_summary!r}")


def fetch_all_nodes(opener: urllib.request.OpenerDirector) -> tuple[list[dict[str, Any]], int]:
    page = 1
    page_size = 500
    nodes: list[dict[str, Any]] = []
    expected_total = 0
    while True:
        path = f"/api/nodes?availability=all&page={page}&page_size={page_size}"
        code, payload = request(opener, "GET", path)
        require(code == 200 and isinstance(payload, dict), f"GET {path} failed HTTP {code}: {payload!r}")
        page_nodes = payload.get("nodes")
        require(isinstance(page_nodes, list), f"nodes payload missing list for {path}: {payload!r}")
        nodes.extend(node for node in page_nodes if isinstance(node, dict))
        expected_total = int(payload.get("total_filtered") or payload.get("total_nodes") or len(nodes))
        if not payload.get("has_next"):
            break
        page += 1
        require(page <= 1000, "node pagination exceeded safety limit")
    return nodes, expected_total


def check_port_continuity(opener: urllib.request.OpenerDirector) -> None:
    nodes, expected_total = fetch_all_nodes(opener)
    ports = [int(node.get("port")) for node in nodes if node.get("port")]
    require(len(ports) == len(set(ports)), f"duplicate ports detected: count={len(ports)} unique={len(set(ports))}")
    if not ports:
        print("ports: no node ports reported")
        return
    first = min(ports)
    last = max(ports)
    missing = [port for port in range(first, last + 1) if port not in set(ports)]
    # Port holes are allowed: the runtime deliberately skips occupied ports and
    # preserves existing node port assignments across reloads to avoid churn.
    # Stability here means no duplicate exposed ports and no lost nodes.
    require(len(ports) == expected_total, f"port count does not match node total: ports={len(ports)} total={expected_total}")
    gap_note = f" gaps={missing}" if missing else " contiguous"
    print(f"ports: unique range {first}-{last} count={len(ports)}{gap_note}")


def check_free_proxy_refresh(opener: urllib.request.OpenerDirector) -> None:
    if SKIP_FREE_PROXY_REFRESH:
        print("free-proxy-refresh: skipped by EP_SMOKE_SKIP_FREE_PROXY_REFRESH")
        return
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
        code, current = request(opener, "GET", "/api/free-proxy/refresh/status", retry_connect=True)
        require(code == 200 and isinstance(current, dict), f"GET free refresh status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"succeeded", "failed"}:
            require(state != "failed", f"free proxy refresh failed: {current!r}")
            accepted = int(current.get("accepted") or 0)
            if accepted > 0:
                wait_for_reload_settled(opener, "free-proxy-refresh")
                wait_for_free_proxy_runtime_count(opener, accepted, "free-proxy-refresh")
            print(f"free-proxy-refresh: succeeded accepted={current.get('accepted')} cache_updated={current.get('cache_updated')}")
            return
        time.sleep(0.5)
    raise RuntimeSmokeError("free proxy refresh did not finish within poll window")


def wait_for_free_proxy_refresh(opener: urllib.request.OpenerDirector, context: str) -> dict[str, Any]:
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", "/api/free-proxy/refresh/status", retry_connect=True)
        require(code == 200 and isinstance(current, dict), f"GET free refresh status failed HTTP {code}: {current!r}")
        state = str(current.get("state", ""))
        if state in {"succeeded", "failed", "idle", "disabled"}:
            require(state == "succeeded", f"{context} free proxy refresh did not succeed: {current!r}")
            return current
        time.sleep(0.5)
    raise RuntimeSmokeError(f"{context} free proxy refresh did not finish within poll window")


def check_free_proxy_refresh_with_fixture(opener: urllib.request.OpenerDirector, original: dict[str, Any]) -> None:
    if not FREE_PROXY_FIXTURE:
        print("free-proxy-fixture: skipped by default")
        return
    global FREE_PROXY_FIXTURE_BASELINE
    with tempfile.TemporaryDirectory(prefix="easy-proxies-smoke-") as tmp:
        baseline = fetch_nodes_summary(opener)
        FREE_PROXY_FIXTURE_BASELINE = baseline
        baseline_total = int(baseline.get("total_nodes") or 0)
        baseline_free = int((baseline.get("source_stats") or {}).get("free_proxy") or 0)
        tmp_path = Path(tmp)
        source_path = tmp_path / "local-free-proxies.txt"
        cache_path = tmp_path / "cache.txt"
        fixture_uris = [
            "http://127.0.0.1:18080",
            "http://127.0.0.1:18081",
            "socks5://127.0.0.1:18082",
        ]
        source_path.write_text("\n".join(fixture_uris) + "\n", encoding="utf-8")
        fixture_settings = dict(original)
        fixture_settings["free_proxy_sources"] = [{
            "name": "local-smoke-free-proxy",
            "enabled": True,
            "file": str(source_path),
            "url": "",
            "format": "txt",
            "default_scheme": "http",
            "max_nodes": 0,
            "max_bytes": 0,
        }]
        fixture_settings["free_proxy_cache"] = {
            **dict(original.get("free_proxy_cache") or {}),
            "enabled": True,
            "path": str(cache_path),
            "refresh_on_start": False,
            "auto_reload": True,
            "workers": 1,
            "max_age": "1ns",
        }
        fixture_settings["free_proxy_filter"] = {
            **dict(original.get("free_proxy_filter") or {}),
            "enabled": False,
        }
        fixture_settings["free_proxy_max_nodes"] = 0

        saved = save_settings(opener, fixture_settings)
        require(saved.get("free_proxy_refresh_needed"), f"fixture settings should trigger free proxy refresh: {saved!r}")
        require(saved.get("free_proxy_refresh_started"), f"fixture free proxy refresh did not start: {saved!r}")
        status = wait_for_free_proxy_refresh(opener, "fixture")
        require(int(status.get("accepted") or 0) == len(fixture_uris), f"fixture accepted count mismatch: {status!r}")
        require(cache_path.exists(), f"fixture cache was not written: {cache_path}")
        cached = cache_path.read_text(encoding="utf-8")
        missing = [uri for uri in fixture_uris if uri not in cached]
        require(not missing, f"cache file should contain accepted fixture proxies, missing={missing}, content={cached!r}")
        wait_for_reload_settled(opener, "fixture")
        loaded_summary = wait_for_free_proxy_runtime_count(opener, len(fixture_uris), "fixture")
        total_nodes = int(loaded_summary.get("total_nodes") or 0)
        require(total_nodes >= baseline_total + len(fixture_uris) - baseline_free, f"fixture runtime total did not include fixture nodes: baseline={baseline!r} loaded={loaded_summary!r}")
        print(f"free-proxy-fixture: succeeded accepted={status.get('accepted')} cache={cache_path}")
        print(f"fixture runtime loaded: free_proxy={((loaded_summary or {}).get('source_stats') or {}).get('free_proxy')} total={loaded_summary.get('total_nodes') if loaded_summary else '-'}")


def wait_for_restored_nodes_summary(opener: urllib.request.OpenerDirector, baseline: dict[str, Any]) -> None:
    baseline_total = int(baseline.get("total_nodes") or 0)
    baseline_free = int((baseline.get("source_stats") or {}).get("free_proxy") or 0)
    deadline = time.time() + POLL_SECONDS
    last_summary: dict[str, Any] | None = None
    while time.time() < deadline:
        last_summary = fetch_nodes_summary(opener)
        free_nodes = int((last_summary.get("source_stats") or {}).get("free_proxy") or 0)
        total_nodes = int(last_summary.get("total_nodes") or 0)
        if free_nodes == baseline_free and total_nodes == baseline_total:
            return
        time.sleep(0.5)
    raise RuntimeSmokeError(f"restored runtime did not return to baseline: baseline={baseline!r} last={last_summary!r}")


def main() -> int:
    if any(arg in {"-h", "--help"} for arg in sys.argv[1:]):
        print_help()
        return 0

    opener = make_control_opener()
    original_settings: dict[str, Any] | None = None
    exit_code = 0
    try:
        assert_safe_smoke_target()
        wait_for_webui_ready()
        check_auth_negative_paths()
        login(opener)
        check_auth_status_probe(opener, expected_authenticated=True)
        original_settings = check_same_value_save(opener)
        check_status_endpoints(opener)
        check_debug_and_logs(opener)
        check_config_node_crud(opener)
        check_quality_paths(opener)
        check_extractor_paths(opener)
        check_proxy_auth_runtime(opener, original_settings)
        check_manual_reload(opener)
        check_port_continuity(opener)
        check_free_proxy_refresh(opener)
        check_free_proxy_refresh_with_fixture(opener, original_settings)
    except RuntimeSmokeError as exc:
        print(f"SMOKE FAILED: {exc}", file=sys.stderr)
        exit_code = 1
    finally:
        if original_settings is not None and FREE_PROXY_FIXTURE:
            try:
                restore_settings(opener, original_settings)
                wait_for_reload_settled(opener, "restore")
                if FREE_PROXY_FIXTURE_BASELINE is not None:
                    wait_for_restored_nodes_summary(opener, FREE_PROXY_FIXTURE_BASELINE)
            except RuntimeSmokeError as exc:
                print(f"SMOKE RESTORE FAILED: {exc}", file=sys.stderr)
                exit_code = 1
    if exit_code == 0:
        print("runtime API smoke: ok")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
