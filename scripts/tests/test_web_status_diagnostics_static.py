from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
STATUS_PAGE = ROOT / "web" / "src" / "pages" / "StatusPage.tsx"
DIAGNOSTICS_PAGE = ROOT / "web" / "src" / "pages" / "DiagnosticsPage.tsx"


def read(path: Path) -> str:
    return path.read_text()


def test_status_page_uses_region_labels_in_text_lists():
    text = read(STATUS_PAGE)
    assert "getNodesPage" in text
    assert "getNodes," not in text
    assert "availability: 'all'" in text
    assert "page_size: 500" in text
    assert "import { regionMeta }" in text
    assert "function regionLabel" in text
    assert "{regionLabel(r)}" in text
    assert "{regionLabel(n.region)}" in text
    assert "String(n.region||'-')" not in text


def test_diagnostics_page_surfaces_debug_and_log_query_errors():
    text = read(DIAGNOSTICS_PAGE)
    assert "QueryErrorBanner" in text
    assert 'title="运行态摘要加载失败"' in text
    assert "debug.error" in text
    assert "debug.refetch()" in text
    assert 'title="日志加载失败"' in text
    assert "logQuery.error" in text
    assert "logQuery.refetch()" in text
    assert "logQuery.isError ? 'ERROR'" not in text
    assert "debug.isError || logQuery.isError ? 'error' : 'success'" in text


def test_diagnostics_clear_logs_pauses_auto_refresh():
    text = read(DIAGNOSTICS_PAGE)
    assert "const clearLogs = () => { setAuto(false); setLogs('') }" in text
    assert "onClick={clearLogs}" in text


if __name__ == "__main__":
    test_status_page_uses_region_labels_in_text_lists()
    test_diagnostics_page_surfaces_debug_and_log_query_errors()
