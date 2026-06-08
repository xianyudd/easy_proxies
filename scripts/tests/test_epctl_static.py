from pathlib import Path

SRC = Path(__file__).resolve().parents[2] / "epctl.sh"


def read_source() -> str:
    return SRC.read_text()


def test_isolated_startup_does_not_require_available_nodes_by_default():
    text = read_source()
    assert 'min_available="${EP_READY_MIN_AVAILABLE:-0}"' in text
    assert 'min_available=1' not in text


def test_readiness_warning_reports_node_availability_separately():
    text = read_source()
    assert 'node availability below threshold' in text
    assert 'service ready: webui=' in text


def test_status_uses_authenticated_optional_api_and_null_safe_jq():
    text = read_source()
    assert 'webui_api_optional()' in text
    assert 'node_json="$(webui_api_optional GET "/api/nodes" || true)"' in text
    assert 'settings_json="$(webui_api_optional GET "/api/settings" || true)"' in text
    assert '.nodes // []' in text
    assert 'WebUI API summary unavailable' in text


def test_isolated_foreground_run_command_exists_for_sandbox_runtime_verification():
    text = read_source()
    assert 'isolated:run                          Run isolated instance in foreground' in text
    assert 'run_service_foreground()' in text
    assert 'exec "$BIN" --config "$CONFIG_FILE"' in text
    assert 'isolated:run|service:isolated:run) EP_PROFILE=isolated; run_service_foreground ;;' in text


if __name__ == "__main__":
    test_isolated_startup_does_not_require_available_nodes_by_default()
    test_readiness_warning_reports_node_availability_separately()
    test_status_uses_authenticated_optional_api_and_null_safe_jq()
    test_isolated_foreground_run_command_exists_for_sandbox_runtime_verification()
