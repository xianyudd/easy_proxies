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
PASSWORD = os.environ.get("EP_SMOKE_PASSWORD", os.environ.get("WEBUI_PASSWORD", "runtime-partial-secret"))
TIMEOUT = float(os.environ.get("EP_SMOKE_TIMEOUT", "20"))
POLL_SECONDS = float(os.environ.get("EP_SMOKE_POLL_SECONDS", "20"))
SKIP_RELOAD = os.environ.get("EP_SMOKE_SKIP_RELOAD", "").lower() in {"1", "true", "yes"}
ALLOW_NO_PASSWORD = os.environ.get("EP_SMOKE_ALLOW_NO_PASSWORD", "").lower() in {"1", "true", "yes"}
FREE_PROXY_FIXTURE = os.environ.get("EP_SMOKE_FREE_PROXY_FIXTURE", "").lower() in {"1", "true", "yes"}
ALLOW_MAIN_PORT = os.environ.get("EP_SMOKE_ALLOW_MAIN_PORT", "").lower() in {"1", "true", "yes"}


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
                raise
            time.sleep(0.5 * attempt)
    raise RuntimeSmokeError(f"request retry exhausted: {method} {path}")


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
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
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
    probe = opener or urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
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


def save_settings(opener: urllib.request.OpenerDirector, settings: dict[str, Any]) -> dict[str, Any]:
    code, saved = request(opener, "PUT", "/api/settings", settings)
    require(code == 200 and isinstance(saved, dict), f"PUT /api/settings failed HTTP {code}: {saved!r}")
    return saved


def restore_settings(opener: urllib.request.OpenerDirector, original: dict[str, Any]) -> None:
    saved = save_settings(opener, original)
    error = saved.get("free_proxy_refresh_error") or saved.get("reload_error")
    require(not error, f"restoring settings reported error: {saved!r}")
    print("settings: restored original runtime configuration")


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
    code, invalid = request(opener, "GET", "/api/debug?summary_only=maybe")
    require(code == 400 and isinstance(invalid, dict) and invalid.get("code") == "invalid_bool", f"invalid debug summary_only should fail with structured error, got HTTP {code}: {invalid!r}")
    print("debug/logs: summary/full/logs and structured invalid bool ok")



def check_config_node_crud(opener: urllib.request.OpenerDirector) -> None:
    suffix = str(int(time.time() * 1000))
    name = f"smoke-node-{suffix}"
    updated = f"smoke-node-updated-{suffix}"

    def cleanup(node_name: str) -> None:
        code, _ = request(opener, "DELETE", f"/api/nodes/config/{urllib.parse.quote(node_name)}")
        require(code in {200, 404}, f"cleanup config node {node_name} failed HTTP {code}")

    cleanup(name)
    cleanup(updated)
    try:
        create_payload = {"name": name, "uri": "http://127.0.0.1:1", "port": 0}
        code, created = request(opener, "POST", "/api/nodes/config", create_payload)
        require(code == 200 and isinstance(created, dict), f"create config node failed HTTP {code}: {created!r}")
        require(created.get("need_reload") is True, f"create config node should require reload: {created!r}")
        require((created.get("node") or {}).get("name") == name, f"created node name mismatch: {created!r}")

        update_payload = {"name": updated, "uri": "http://127.0.0.1:2", "port": 0}
        code, changed = request(opener, "PUT", f"/api/nodes/config/{urllib.parse.quote(name)}", update_payload)
        require(code == 200 and isinstance(changed, dict), f"update config node failed HTTP {code}: {changed!r}")
        require(changed.get("need_reload") is True, f"update config node should require reload: {changed!r}")
        require((changed.get("node") or {}).get("name") == updated, f"updated node name mismatch: {changed!r}")

        code, listed = request(opener, "GET", "/api/nodes/config")
        require(code == 200 and isinstance(listed, dict), f"list config nodes failed HTTP {code}: {listed!r}")
        nodes = listed.get("nodes") or []
        require(any(isinstance(node, dict) and node.get("name") == updated for node in nodes), f"updated config node not listed: {listed!r}")

        code, deleted = request(opener, "DELETE", f"/api/nodes/config/{urllib.parse.quote(updated)}")
        require(code == 200 and isinstance(deleted, dict), f"delete config node failed HTTP {code}: {deleted!r}")
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
    })
    if code == 409 and isinstance(job, dict) and job.get("code") == "active_job":
        print("quality: create job skipped because another job is active")
        return
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
    require(not missing, f"missing ports in reported range {first}-{last}: {missing}")
    require(len(ports) == expected_total, f"port count does not match node total: ports={len(ports)} total={expected_total}")
    print(f"ports: contiguous unique range {first}-{last} count={len(ports)}")


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


def wait_for_free_proxy_refresh(opener: urllib.request.OpenerDirector, context: str) -> dict[str, Any]:
    deadline = time.time() + POLL_SECONDS
    while time.time() < deadline:
        code, current = request(opener, "GET", "/api/free-proxy/refresh/status")
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
    with tempfile.TemporaryDirectory(prefix="easy-proxies-smoke-") as tmp:
        tmp_path = Path(tmp)
        source_path = tmp_path / "local-free-proxies.txt"
        cache_path = tmp_path / "cache.txt"
        fixture_uris = [
            "http://127.0.0.1:18080",
            "http://127.0.0.1:18081",
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
            "auto_reload": False,
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
        print(f"free-proxy-fixture: succeeded accepted={status.get('accepted')} cache={cache_path}")


def main() -> int:
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
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
            except RuntimeSmokeError as exc:
                print(f"SMOKE RESTORE FAILED: {exc}", file=sys.stderr)
                exit_code = 1
    if exit_code == 0:
        print("runtime API smoke: ok")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
