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


if __name__ == "__main__":
    test_isolated_startup_does_not_require_available_nodes_by_default()
    test_readiness_warning_reports_node_availability_separately()
