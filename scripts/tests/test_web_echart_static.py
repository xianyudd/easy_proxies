from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ECHART = ROOT / "web" / "src" / "components" / "charts" / "EChart.tsx"


def test_echart_handles_missing_resize_observer():
    text = ECHART.read_text()
    assert "typeof ResizeObserver !== 'undefined'" in text
    assert "const observer = typeof ResizeObserver" in text
    assert "observer?.observe(ref.current)" in text
    assert "observer?.disconnect()" in text
    assert "window.setTimeout(resize, 0)" in text


if __name__ == "__main__":
    test_echart_handles_missing_resize_observer()
