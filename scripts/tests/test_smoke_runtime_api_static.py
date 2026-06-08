from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / "smoke_runtime_api.py"


def read_source() -> str:
    return SRC.read_text()


def test_smoke_script_checks_auth_settings_reload_and_free_proxy_paths():
    text = read_source()
    assert "RuntimeSmokeError" in text
    assert '"POST", "/api/auth"' in text
    assert '"GET", "/api/settings"' in text
    assert '"PUT", "/api/settings"' in text
    assert '"POST", "/api/reload"' in text
    assert '"/api/reload/status"' in text
    assert 'manual reload did not finish within poll window' in text
    assert '"POST", "/api/free-proxy/refresh"' in text
    assert '"/api/free-proxy/refresh/status"' in text
    assert "same-value save unexpectedly triggered reload/refresh" in text


def test_smoke_script_checks_port_continuity_after_reload():
    text = read_source()
    assert 'check_port_continuity' in text
    assert 'fetch_all_nodes' in text
    assert 'page_size = 500' in text
    assert 'has_next' in text
    assert 'missing ports' in text
    assert 'duplicate ports' in text


def test_smoke_script_checks_auth_negative_paths_by_default():
    text = read_source()
    assert 'EP_SMOKE_ALLOW_NO_PASSWORD' in text
    assert 'check_auth_negative_paths' in text
    assert 'unauthenticated settings access should be rejected' in text
    assert 'wrong password should be rejected' in text


def test_smoke_script_uses_env_configurable_base_url_and_password():
    text = read_source()
    assert 'EP_SMOKE_BASE_URL' in text
    assert 'EP_SMOKE_PASSWORD' in text
    assert 'runtime-partial-secret' in text


if __name__ == "__main__":
    test_smoke_script_checks_auth_settings_reload_and_free_proxy_paths()
    test_smoke_script_checks_port_continuity_after_reload()
    test_smoke_script_checks_auth_negative_paths_by_default()
    test_smoke_script_uses_env_configurable_base_url_and_password()
