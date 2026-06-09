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


def test_status_page_defends_non_array_nodes_payload():
    text = read(STATUS_PAGE)
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "const data = safeRows<NodeSnapshot>(nodes.data?.nodes)" in text
    assert "nodes.data?.nodes || []" not in text


def test_status_page_sanitizes_summary_records():
    text = read(STATUS_PAGE)
    assert "function safeRecord(value: unknown): Record<string, number>" in text
    assert "const regionHealthy = safeRecord(summaryData?.region_healthy)" in text
    assert "const regionStats = safeRecord(summaryData?.region_stats)" in text
    assert "Object.values(regionHealthy).reduce((sum, count) => sum + safeCount(count), 0)" in text
    assert "Object.entries(Object.keys(regionStats).length ? regionStats : data.reduce" in text
    assert "Object.values(summaryData?.region_healthy || {})" not in text


if __name__ == "__main__":
    test_status_page_sanitizes_numeric_summary_fields()
    test_status_page_defends_non_array_nodes_payload()
    test_status_page_sanitizes_summary_records()
