from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
STATUS_PAGE = ROOT / "web" / "src" / "pages" / "StatusPage.tsx"


def read(path: Path) -> str:
    return path.read_text()


def test_status_page_sanitizes_numeric_summary_fields():
    text = read(STATUS_PAGE)
    assert "function safeCount" in text
    assert "function safeRate" in text
    assert "Number.isFinite(value) && value >= 0" in text
    assert "Math.min(100, value)" in text
    assert "safeCount(count)" in text
    assert "safeCount(summaryData?.total_nodes" in text
    assert "safeCount(n.active_connections)" in text
    assert "safeRate(debug.data?.success_rate)" in text
    assert "Number(count || 0)" not in text
    assert "Number(summaryData?.total_nodes ||" not in text
    assert "Number(debug.data?.success_rate || 0)" not in text


if __name__ == "__main__":
    test_status_page_sanitizes_numeric_summary_fields()
