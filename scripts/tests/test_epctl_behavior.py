import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EPCTL = ROOT / "epctl.sh"


def run_bash(script: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", "-lc", script],
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )


def shell_quote(value: str) -> str:
    return "'" + value.replace("'", "'\"'\"'") + "'"


def pid_match_script(args: list[str], expected_config: str = "/tmp/epctl-test.yaml") -> str:
    quoted_args = " ".join(shell_quote(arg) for arg in args)
    return f"""
set -euo pipefail
tmp="$(mktemp -d)"
pid="$$"
mkdir -p "$tmp/$pid"
python3 - "$tmp/$pid/cmdline" {quoted_args} <<'PY'
import sys
path = sys.argv[1]
args = sys.argv[2:]
with open(path, 'wb') as fh:
    fh.write(b'\\0'.join(arg.encode() for arg in args) + b'\\0')
PY
EPCTL_LIB_ONLY=1
EPCTL_PROC_ROOT="$tmp"
BIN="/tmp/epctl-bin"
CONFIG_FILE={shell_quote(expected_config)}
source {shell_quote(str(EPCTL))}
pid_matches_profile "$pid"
"""


def preflight_script(listener_line: str, current_profile_owner: bool = False) -> str:
    escaped_line = shell_quote(listener_line)
    proc_setup = ""
    if current_profile_owner:
        escaped_line = '"LISTEN 0 4096 127.0.0.1:19093 0.0.0.0:* users:((\\"easy\\",pid=${pid},fd=7))"'
        proc_setup = """
pid="$$"
mkdir -p "$tmp/$pid"
python3 - "$tmp/$pid/cmdline" /tmp/epctl-bin --config=/tmp/epctl-test.yaml <<'PY'
import sys
path = sys.argv[1]
args = sys.argv[2:]
with open(path, 'wb') as fh:
    fh.write(b'\\0'.join(arg.encode() for arg in args) + b'\\0')
PY
"""
    return f"""
set -euo pipefail
tmp="$(mktemp -d)"
{proc_setup}
EPCTL_LIB_ONLY=1
EPCTL_PROC_ROOT="$tmp"
BIN="/tmp/epctl-bin"
CONFIG_FILE="/tmp/epctl-test.yaml"
WEBUI_URL="http://127.0.0.1:19093"
source {shell_quote(str(EPCTL))}
preflight_ports() {{ printf 'webui\\t127.0.0.1\\t19093\\n'; }}
port_listener_lines() {{ echo {escaped_line}; }}
set +e
preflight_ports_available
code="$?"
echo "exit=$code"
exit "$code"
"""


def test_pid_matches_profile_accepts_equals_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config=/tmp/epctl-test.yaml"]))
    assert result.returncode == 0, result.stderr


def test_pid_matches_profile_accepts_separate_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config", "/tmp/epctl-test.yaml"]))
    assert result.returncode == 0, result.stderr


def test_pid_matches_profile_rejects_different_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config=/tmp/other.yaml"]))
    assert result.returncode != 0


def test_find_service_pids_uses_profile_parser_and_proc_root():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
matched="$$"
other="5678"
mkdir -p "$tmp/$matched" "$tmp/$other"
python3 - "$tmp/$matched/cmdline" /tmp/epctl-bin --verbose --config=/tmp/epctl-test.yaml <<'PY'
import sys
path = sys.argv[1]
args = sys.argv[2:]
with open(path, 'wb') as fh:
    fh.write(b'\\0'.join(arg.encode() for arg in args) + b'\\0')
PY
python3 - "$tmp/$other/cmdline" /tmp/epctl-bin --config=/tmp/other.yaml <<'PY'
import sys
path = sys.argv[1]
args = sys.argv[2:]
with open(path, 'wb') as fh:
    fh.write(b'\\0'.join(arg.encode() for arg in args) + b'\\0')
PY
EPCTL_LIB_ONLY=1
EPCTL_PROC_ROOT="$tmp"
BIN="/tmp/epctl-bin"
CONFIG_FILE="/tmp/epctl-test.yaml"
source {shell_quote(str(EPCTL))}
out="$(find_service_pids)"
if [ "$out" != "$matched" ]; then
  echo "expected $matched, got $out" >&2
  exit 1
fi
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr


def test_listener_line_owner_label_reports_unknown_without_pid():
    script = f"""
set -euo pipefail
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
listener_line_owner_label 'LISTEN 0 4096 127.0.0.1:19093 0.0.0.0:*'
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "unknown-no-pid" in result.stdout


def test_listener_line_owner_label_reports_pid_list():
    script = f"""
set -euo pipefail
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
listener_line_owner_label 'LISTEN users:(("easy",pid=123,fd=7),("easy",pid=456,fd=8))'
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "pid=123,456" in result.stdout


def test_preflight_allows_listener_owned_by_current_profile():
    script = preflight_script("", current_profile_owner=True)
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "owned by current profile" in result.stdout


def test_preflight_rejects_other_visible_owner():
    result = run_bash(preflight_script('LISTEN 0 4096 127.0.0.1:19093 0.0.0.0:* users:(("other",pid=1,fd=7))'))
    assert result.returncode != 0
    assert "port conflict before start" in result.stderr
    assert "owner=pid=1" in result.stderr


def test_preflight_rejects_unknown_owner_without_pid():
    result = run_bash(preflight_script('LISTEN 0 4096 127.0.0.1:19093 0.0.0.0:*'))
    assert result.returncode != 0
    assert "port conflict before start" in result.stderr
    assert "unknown-no-pid" in result.stderr


def test_start_service_failure_removes_stale_pid_file():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
bin="$tmp/fail-fast-bin"
cat >"$bin" <<'SH'
#!/usr/bin/env sh
exit 0
SH
chmod +x "$bin"
CONFIG_FILE="$tmp/config.yaml"
touch "$CONFIG_FILE"
BIN="$bin"
LOG_FILE="$tmp/service.log"
PID_FILE="$tmp/service.pid"
WEBUI_URL="http://127.0.0.1:19093"
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
preflight_ports_available() {{ :; }}
clean_proxy_env() {{ :; }}
set +e
( start_service )
code="$?"
set -e
if [ "$code" -eq 0 ]; then
  echo "start_service unexpectedly succeeded" >&2
  exit 1
fi
if [ -e "$PID_FILE" ]; then
  echo "stale pid file was not removed" >&2
  exit 1
fi
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr


def test_start_service_fails_when_control_plane_not_ready_but_preserves_running_pid():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
bin="$tmp/sleep-bin"
cat >"$bin" <<'SH'
#!/usr/bin/env sh
sleep 30
SH
chmod +x "$bin"
CONFIG_FILE="$tmp/config.yaml"
touch "$CONFIG_FILE"
BIN="$bin"
LOG_FILE="$tmp/service.log"
PID_FILE="$tmp/service.pid"
WEBUI_URL="http://127.0.0.1:19093"
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
preflight_ports_available() {{ :; }}
clean_proxy_env() {{ :; }}
wait_for_service_ready() {{
  echo "[WARN] simulated control plane not ready" >&2
  return 1
}}
set +e
( start_service )
code="$?"
set -e
pid="$(cat "$PID_FILE" 2>/dev/null || true)"
if [ -n "$pid" ]; then
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
fi
if [ "$code" -eq 0 ]; then
  echo "start_service unexpectedly succeeded when control plane was not ready" >&2
  exit 1
fi
if [ -z "$pid" ]; then
  echo "running pid file should be preserved for cleanup/status" >&2
  exit 1
fi
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "service process started but control plane is not ready" in result.stdout + result.stderr


def test_restart_service_removes_temp_binary_when_stop_fails():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
BIN="$tmp/easy-proxies"
CONFIG_FILE="$tmp/config.yaml"
LOG_FILE="$tmp/service.log"
PID_FILE="$tmp/service.pid"
WEBUI_URL="http://127.0.0.1:19093"
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
build_service_to() {{
  printf 'temporary binary' >"$1"
}}
stop_service() {{
  return 1
}}
start_service() {{
  echo "start_service should not run after stop failure" >&2
  return 1
}}
set +e
restart_service
code="$?"
set -e
if [ "$code" -eq 0 ]; then
  echo "restart_service unexpectedly succeeded" >&2
  exit 1
fi
if ls "$tmp"/easy-proxies.next.* >/dev/null 2>&1; then
  echo "temporary binary was not removed" >&2
  exit 1
fi
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr


def test_status_reports_stale_unverified_pid_file_when_not_running():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
CONFIG_FILE="$tmp/config.yaml"
BIN="$tmp/easy-proxies"
LOG_FILE="$tmp/service.log"
PID_FILE="$tmp/service.pid"
WEBUI_URL="http://127.0.0.1:19093"
printf '999999\\n' >"$PID_FILE"
EPCTL_LIB_ONLY=1
source {shell_quote(str(EPCTL))}
clean_proxy_env() {{ :; }}
webui_http_code() {{ echo 000; }}
find_service_pids() {{ :; }}
configured_port() {{ echo "$3"; }}
cfg_value() {{ return 1; }}
webui_port() {{ echo 19093; }}
status_service
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "stale/unverified pid file=999999" in result.stdout


def test_status_summarizes_key_port_ownership():
    script = f"""
set -euo pipefail
tmp="$(mktemp -d)"
pid="$$"
mkdir -p "$tmp/$pid"
python3 - "$tmp/$pid/cmdline" /tmp/epctl-bin --config=/tmp/epctl-test.yaml <<'PY'
import sys
path = sys.argv[1]
args = sys.argv[2:]
with open(path, 'wb') as fh:
    fh.write(b'\\0'.join(arg.encode() for arg in args) + b'\\0')
PY
CONFIG_FILE="/tmp/epctl-test.yaml"
BIN="/tmp/epctl-bin"
LOG_FILE="$tmp/service.log"
PID_FILE="$tmp/service.pid"
WEBUI_URL="http://127.0.0.1:19093"
EPCTL_LIB_ONLY=1
EPCTL_PROC_ROOT="$tmp"
source {shell_quote(str(EPCTL))}
clean_proxy_env() {{ :; }}
webui_http_code() {{ echo 000; }}
find_service_pids() {{ :; }}
configured_port() {{
  case "$1.$2" in
    listener.port) echo 12340 ;;
    multi_port.base_port) echo 30000 ;;
    android_proxy.base_port) echo 30150 ;;
    geoip.port) echo 12341 ;;
    *) echo "$3" ;;
  esac
}}
cfg_value() {{
  if [ "$1.$2" = "management.clash_api_listen" ]; then
    echo "127.0.0.1:19094"
    return 0
  fi
  return 1
}}
webui_port() {{ echo 19093; }}
port_listener_lines() {{
  case "$1" in
    19093) echo "LISTEN 0 4096 127.0.0.1:19093 0.0.0.0:* users:((\\"easy\\",pid=${{pid}},fd=7))" ;;
    19094) echo "LISTEN 0 4096 127.0.0.1:19094 0.0.0.0:* users:((\\"other\\",pid=1,fd=7))" ;;
    12340) echo "LISTEN 0 4096 127.0.0.1:12340 0.0.0.0:*" ;;
    *) : ;;
  esac
}}
status_service
"""
    result = run_bash(script)
    assert result.returncode == 0, result.stderr
    assert "Port status (key ports):" in result.stdout
    assert "webui 127.0.0.1:19093 current-profile" in result.stdout
    assert "clash 127.0.0.1:19094 other-owner" in result.stdout
    assert "listener 127.0.0.1:12340 unknown-owner" in result.stdout
    assert "multi_base 127.0.0.1:30000 free" in result.stdout


if __name__ == "__main__":
    test_pid_matches_profile_accepts_equals_config_argument()
    test_pid_matches_profile_accepts_separate_config_argument()
    test_pid_matches_profile_rejects_different_config_argument()
    test_find_service_pids_uses_profile_parser_and_proc_root()
    test_listener_line_owner_label_reports_unknown_without_pid()
    test_listener_line_owner_label_reports_pid_list()
    test_preflight_allows_listener_owned_by_current_profile()
    test_preflight_rejects_other_visible_owner()
    test_preflight_rejects_unknown_owner_without_pid()
    test_start_service_failure_removes_stale_pid_file()
    test_start_service_fails_when_control_plane_not_ready_but_preserves_running_pid()
    test_restart_service_removes_temp_binary_when_stop_fails()
    test_status_reports_stale_unverified_pid_file_when_not_running()
    test_status_summarizes_key_port_ownership()
