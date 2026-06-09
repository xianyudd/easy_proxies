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
    assert 'node_json="$(webui_api_optional GET "/api/nodes?summary_only=true&availability=all" || true)"' in text
    assert 'settings_json="$(webui_api_optional GET "/api/settings" || true)"' in text
    assert '.nodes // []' in text
    assert 'WebUI API summary unavailable' in text


def test_isolated_foreground_run_command_exists_for_sandbox_runtime_verification():
    text = read_source()
    assert 'isolated:run                          Run isolated instance in foreground' in text
    assert 'run_service_foreground()' in text
    assert 'exec "$BIN" --config "$CONFIG_FILE"' in text
    assert 'isolated:run|service:isolated:run) EP_PROFILE=isolated; run_service_foreground ;;' in text


def test_status_fetches_summary_only_all_nodes_for_full_port_range_summary():
    text = read_source()
    assert 'node_json="$(webui_api_optional GET "/api/nodes?summary_only=true&availability=all" || true)"' in text


def test_status_prefers_total_filtered_for_all_availability_visible_count():
    text = read_source()
    assert 'visible_nodes:(.total_filtered // .visible_nodes // .total_nodes // ((.nodes // [])|length))' in text


def test_isolated_profile_uses_default_webui_password_consistently():
    text = read_source()
    assert 'WEBUI_PASSWORD="${WEBUI_PASSWORD:-runtime-partial-secret}"' in text
    assert 'WEBUI_PASSWORD="${WEBUI_PASSWORD:-}"' in text
    assert "management_password_json=\"$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' \"$WEBUI_PASSWORD\")\"" in text
    assert '  password: $management_password_json' in text


def test_start_service_runs_port_preflight_before_launching_background_process():
    text = read_source()
    assert 'preflight_ports_available' in text
    assert 'preflight_ports_available' in text[text.index('start_service()'):text.index('setsid "$BIN" --config "$CONFIG_FILE"')]


def test_isolated_port_preflight_uses_profile_configured_key_ports():
    text = read_source()
    assert 'preflight_ports()' in text
    assert 'preflight_web_port="$(webui_port)"' in text
    assert 'preflight_clash_listen="$(cfg_value management clash_api_listen || true)"' in text
    assert 'preflight_listener_port="$(configured_port listener port 2323)"' in text
    assert 'preflight_multi_base="$(configured_port multi_port base_port 24000)"' in text
    assert 'preflight_android_base="$(configured_port android_proxy base_port 13001)"' in text
    for port in ('19093', '19094', '12340', '30000', '30150'):
        assert port in text


def test_port_preflight_allows_current_profile_owner_and_fails_fast_other_owners():
    text = read_source()
    assert 'listener_line_owned_by_current_profile()' in text
    assert 'pid_matches_profile "$pid"' in text
    assert 'owned by current profile' in text
    assert '[ERROR] port conflict before start:' in text
    assert 'return 1' in text[text.index('preflight_ports_available()'):text.index('run_service_foreground()')]


def test_restart_builds_temp_binary_before_stop_and_swap():
    text = read_source()
    assert 'restart_service()' in text
    restart = text[text.index('restart_service()'):text.index('status_service()')]
    assert 'local tmp_bin="${BIN}.next.$$"' in restart
    assert 'build_service_to "$tmp_bin"' in restart
    assert 'stop_service' in restart
    assert 'mv "$tmp_bin" "$BIN"' in restart
    assert restart.index('build_service_to "$tmp_bin"') < restart.index('stop_service')
    assert 'service:restart|restart) restart_service ;;' in text
    assert 'isolated:restart|service:isolated:restart) restart_service ;;' in text


def test_pid_profile_matching_parses_config_argument_forms():
    text = read_source()
    assert 'realpath -m -- "$path"' in text
    assert 'while IFS= read -r -d' in text
    assert 'case "$arg" in' in text
    assert '--config)' in text
    assert '--config=*)' in text
    assert 'cfg_arg="${arg#--config=}"' in text
    assert 'arg_matches_path "$cfg_arg" "$CONFIG_FILE"' in text


if __name__ == "__main__":
    test_isolated_startup_does_not_require_available_nodes_by_default()
    test_readiness_warning_reports_node_availability_separately()
    test_status_uses_authenticated_optional_api_and_null_safe_jq()
    test_isolated_foreground_run_command_exists_for_sandbox_runtime_verification()
    test_status_fetches_summary_only_all_nodes_for_full_port_range_summary()
    test_status_prefers_total_filtered_for_all_availability_visible_count()
    test_isolated_profile_uses_default_webui_password_consistently()
    test_start_service_runs_port_preflight_before_launching_background_process()
    test_isolated_port_preflight_uses_profile_configured_key_ports()
    test_port_preflight_allows_current_profile_owner_and_fails_fast_other_owners()
    test_restart_builds_temp_binary_before_stop_and_swap()
    test_pid_profile_matching_parses_config_argument_forms()
