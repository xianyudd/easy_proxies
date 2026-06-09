from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / "smoke_runtime_api.py"


def read_source() -> str:
    return SRC.read_text()


def test_smoke_script_checks_auth_settings_reload_and_free_proxy_paths():
    text = read_source()
    assert "RuntimeSmokeError" in text
    assert '"POST", "/api/auth"' in text
    assert "check_extractor_paths" in text
    assert '"/api/extractor?region=all&mode=multi-port' in text
    assert '"/api/extractor?region=all&mode=pool' in text
    assert '"/api/extractor?region=all&mode=android' in text
    assert 'invalid extractor request should fail with structured error' in text
    assert '"GET", "/api/settings"' in text
    assert '"PUT", "/api/settings"' in text
    assert '"POST", "/api/reload"' in text
    assert '"/api/reload/status"' in text
    assert 'manual reload did not finish within poll window' in text
    assert '"POST", "/api/free-proxy/refresh"' in text
    assert '"/api/free-proxy/refresh/status"' in text
    assert "same-value save unexpectedly triggered reload/refresh" in text
    assert "check_debug_and_logs" in text
    assert '"/api/debug?summary_only=true"' in text
    assert '"/api/debug"' in text
    assert '"/api/logs"' in text
    assert '"nodes" not in summary' in text
    assert 'invalid debug summary_only should fail with structured error' in text
    assert "check_quality_paths" in text
    assert '"/api/cloudflare/cache"' in text
    assert '"/api/reputation/cache"' in text
    assert '"invalid_source"' in text
    assert '"use_background"' in text
    assert '"POST", "/api/quality/jobs"' in text
    assert 'quality job response missing job_id' in text


def test_smoke_script_retries_reload_status_during_control_plane_rebind():
    text = read_source()
    assert "retry_connect" in text
    assert "urllib.error.URLError" in text
    assert 'request(opener, "GET", "/api/reload/status", retry_connect=True)' in text
    assert "time.sleep(0.5 * attempt)" in text


def test_smoke_script_waits_for_webui_ready_before_checks():
    text = read_source()
    assert "wait_for_webui_ready" in text
    assert '"GET", "/api/auth/status", retry_connect=True' in text
    assert "WebUI did not become ready" in text
    assert "wait_for_webui_ready()" in text


def test_smoke_script_checks_port_continuity_after_reload():
    text = read_source()
    assert 'check_port_continuity' in text
    assert 'fetch_all_nodes' in text
    assert 'page_size = 500' in text
    assert 'has_next' in text
    assert 'Port holes are allowed' in text
    assert 'gaps={missing}' in text
    assert 'duplicate ports' in text


def test_smoke_script_checks_auth_negative_paths_by_default():
    text = read_source()
    assert 'EP_SMOKE_ALLOW_NO_PASSWORD' in text
    assert 'check_auth_negative_paths' in text
    assert 'check_auth_status_probe' in text
    assert '"/api/auth/status"' in text
    assert 'auth status probe should not return 401' in text
    assert 'unauthenticated settings access should be rejected' in text
    assert 'wrong password should be rejected' in text


def test_smoke_script_can_exercise_local_free_proxy_fixture_safely():
    text = read_source()
    assert 'EP_SMOKE_FREE_PROXY_FIXTURE' in text
    assert 'check_free_proxy_refresh_with_fixture' in text
    assert 'tempfile.TemporaryDirectory' in text
    assert 'restore_settings' in text
    assert 'finally:' in text
    assert 'local-smoke-free-proxy' in text
    assert 'cache file should contain accepted fixture proxies' in text


def test_smoke_script_fixture_verifies_auto_reload_nodes_enter_runtime():
    text = read_source()
    assert "fetch_nodes_summary" in text
    assert "wait_for_reload_settled" in text
    assert '"auto_reload": True' in text
    assert "fixture free proxy nodes did not enter runtime" in text
    assert "fixture runtime loaded" in text
    assert "restored runtime did not return to baseline" in text


def test_smoke_script_uses_env_configurable_base_url_and_password():
    text = read_source()
    assert 'EP_SMOKE_BASE_URL' in text
    assert 'EP_SMOKE_PASSWORD' in text
    assert 'runtime-partial-secret' in text


def test_smoke_script_refuses_main_port_without_explicit_override():
    text = read_source()
    assert "assert_safe_smoke_target" in text
    assert "EP_SMOKE_ALLOW_MAIN_PORT" in text
    assert "urllib.parse.urlparse(BASE_URL)" in text
    assert "parsed.port == 9091" in text
    assert "Refusing to run mutating smoke checks against port 9091" in text
    assert "assert_safe_smoke_target()" in text


def test_smoke_script_checks_config_node_crud_without_auto_reload():
    text = read_source()
    assert "check_config_node_crud" in text
    assert '"POST", "/api/nodes/config"' in text
    assert '"GET", "/api/nodes/config"' in text
    assert '"PUT", f"/api/nodes/config/{urllib.parse.quote(name)}"' in text
    assert '"DELETE", f"/api/nodes/config/{urllib.parse.quote(updated)}"' in text
    assert 'create config node should require reload' in text
    assert 'config-nodes: create/update/list/delete require manual reload and cleaned up' in text


if __name__ == "__main__":
    test_smoke_script_checks_auth_settings_reload_and_free_proxy_paths()
    test_smoke_script_retries_reload_status_during_control_plane_rebind()
    test_smoke_script_waits_for_webui_ready_before_checks()
    test_smoke_script_checks_config_node_crud_without_auto_reload()
    test_smoke_script_checks_port_continuity_after_reload()
    test_smoke_script_checks_auth_negative_paths_by_default()
    test_smoke_script_can_exercise_local_free_proxy_fixture_safely()
    test_smoke_script_fixture_verifies_auto_reload_nodes_enter_runtime()
    test_smoke_script_uses_env_configurable_base_url_and_password()
    test_smoke_script_refuses_main_port_without_explicit_override()
