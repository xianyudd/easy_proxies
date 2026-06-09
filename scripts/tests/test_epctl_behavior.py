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


def test_pid_matches_profile_accepts_equals_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config=/tmp/epctl-test.yaml"]))
    assert result.returncode == 0, result.stderr


def test_pid_matches_profile_accepts_separate_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config", "/tmp/epctl-test.yaml"]))
    assert result.returncode == 0, result.stderr


def test_pid_matches_profile_rejects_different_config_argument():
    result = run_bash(pid_match_script(["/tmp/epctl-bin", "--config=/tmp/other.yaml"]))
    assert result.returncode != 0


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


if __name__ == "__main__":
    test_pid_matches_profile_accepts_equals_config_argument()
    test_pid_matches_profile_accepts_separate_config_argument()
    test_pid_matches_profile_rejects_different_config_argument()
    test_listener_line_owner_label_reports_unknown_without_pid()
    test_listener_line_owner_label_reports_pid_list()
