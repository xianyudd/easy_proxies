from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TOPBAR = ROOT / "web" / "src" / "components" / "layout" / "Topbar.tsx"


def read_source() -> str:
    return TOPBAR.read_text()


def test_topbar_sanitizes_summary_counts_and_region_healthy_shape():
    text = read_source()
    assert "function safeCount" in text
    assert "function safeRecord" in text
    assert "safeRecord(data?.region_healthy)" in text
    assert "Object.values(regionHealthy).reduce((sum, n) => sum + safeCount(n), 0)" in text
    assert "Object.values(data?.region_healthy || {})" not in text


if __name__ == "__main__":
    test_topbar_sanitizes_summary_counts_and_region_healthy_shape()
