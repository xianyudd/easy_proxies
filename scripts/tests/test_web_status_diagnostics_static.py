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


def test_status_page_fetches_all_node_pages_for_large_free_source_pools():
    text = read(STATUS_PAGE)
    assert "async function getAllStatusNodes" in text
    assert "has_next" in text
    assert "page += 1" in text
    assert "page_size: STATUS_PAGE_SIZE" in text
    assert "page_size: 500" not in text


def test_diagnostics_page_sanitizes_log_and_debug_payload_shapes():
    text = read(DIAGNOSTICS_PAGE)
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "function safeText(value: unknown)" in text
    assert "const safeLogs = safeText(logQuery.data?.logs)" in text
    assert "setLogs(safeLogs)" in text
    assert "Array.isArray(debugData.nodes)" not in text
    assert "const debugNodes = useMemo<Record<string, unknown>[]>(() => safeRows<Record<string, unknown>>(debugData.nodes), [debugData])" in text
    assert "String(logQuery.data.logs || '')" not in text
    assert "String(result.data?.logs || '')" not in text


def test_diagnostics_page_exposes_filter_level_download_and_clear_controls():
    text = read(DIAGNOSTICS_PAGE)
    assert 'aria-label="筛选日志关键词"' in text
    assert 'aria-label="筛选日志级别"' in text
    assert "LOG_LEVELS.map" in text
    assert "setLevelFilter(event.target.value as LogFilter)" in text
    assert "const download = () =>" in text
    assert "a.download = 'easy_proxies.log'" in text
    assert "URL.revokeObjectURL(url)" in text
    assert "const clearFilters = () =>" in text
    assert "当前筛选没有匹配日志" in text


if __name__ == "__main__":
    test_status_page_uses_region_labels_in_text_lists()
    test_status_page_fetches_all_node_pages_for_large_free_source_pools()
    test_diagnostics_page_surfaces_debug_and_log_query_errors()
    test_diagnostics_clear_logs_pauses_auto_refresh()
    test_diagnostics_page_exposes_filter_level_download_and_clear_controls()
    test_diagnostics_page_sanitizes_log_and_debug_payload_shapes()
