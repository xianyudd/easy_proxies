from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CHARTS = ROOT / "web" / "src" / "components" / "charts" / "NodeCharts.tsx"


def read(path: Path) -> str:
    return path.read_text()


def test_traffic_chart_sanitizes_stream_values_and_counts_malformed_events():
    text = read(CHARTS)
    assert "function safeTrafficNumber" in text
    assert "Number.isFinite(value) && value >= 0" in text
    assert "const [malformedEvents, setMalformedEvents] = useState(0)" in text
    assert "setMalformedEvents(count => count + 1)" in text
    assert "up: safeTrafficNumber(data.up)" in text
    assert "down: safeTrafficNumber(data.down)" in text
    assert "流量数据异常" in text
    assert "catch {\n        setMalformedEvents(count => count + 1)\n      }" in text
    assert "Number(data.up || 0)" not in text
    assert "Number(data.down || 0)" not in text


if __name__ == "__main__":
    test_traffic_chart_sanitizes_stream_values_and_counts_malformed_events()
