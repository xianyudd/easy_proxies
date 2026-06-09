from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LEGACY_INDEX = ROOT / "internal" / "monitor" / "assets" / "index.html"
NODES_API = ROOT / "web" / "src" / "api" / "nodes.ts"


def read_source() -> str:
    return NODES_API.read_text()


def test_nodes_page_query_preserves_availability_all():
    text = read_source()
    assert "value === 'all' && key !== 'availability'" in text
    assert "search.set(key, String(value))" in text


def test_nodes_api_sanitizes_response_shape_before_pages_render():
    text = read_source()
    assert "function safeNodes" in text
    assert "Array.isArray(value)" in text
    assert "function safeRecord" in text
    assert "typeof value !== 'object'" in text
    assert "function safeCount" in text
    assert "Number.isFinite(value) && value >= 0" in text
    assert "nodes: safeNodes(source.nodes)" in text
    assert "region_stats: safeRecord(source.region_stats)" in text
    assert "source_stats: safeRecord(source.source_stats)" in text
    assert "return Array.isArray(data) ? safeNodes(data) : safeNodes(data.nodes)" in text
    assert "data.nodes || []" not in text
    assert "nodes: data.nodes || []" not in text


def test_legacy_webui_defends_non_array_node_payloads():
    text = LEGACY_INDEX.read_text()
    assert "allNodesCache = Array.isArray(data.nodes) ? data.nodes : [];" in text
    assert "configNodes = Array.isArray(data.nodes) ? data.nodes : [];" in text
    assert "const debugNodes = safeArray(d.nodes).filter(n => n && typeof n === 'object');" in text
    assert "tb.innerHTML = debugNodes.map(n =>" in text
    assert "window._debugNodes = debugNodes;" in text
    assert "allNodesCache = data.nodes || [];" not in text
    assert "configNodes = data.nodes || [];" not in text
    assert "(d.nodes||[]).map" not in text
    assert "window._debugNodes = d.nodes || [];" not in text


def test_legacy_webui_defends_non_array_result_payloads():
    text = LEGACY_INDEX.read_text()
    assert "function safeArray(value)" in text
    assert "const extractorEntries = safeArray(data.entries);" in text
    assert "const warnings = safeArray(data.warnings);" in text
    assert "renderExtractorCards(extractorEntries, data);" in text
    assert "formatExtractorOutput(extractorEntries, data.effective_format);" in text
    assert "renderCloudflareRows(safeArray(data.data));" in text
    assert "const rows = safeArray(data.data);" in text
    assert "renderReputationRows(safeArray(data.data));" in text
    assert "const rows = safeArray(data.data).map(r => ({ result: r }));" in text
    assert "data.entries || []" not in text
    assert "data.warnings || []" not in text
    assert "data.data || []" not in text
    assert "(data.data || []).map" not in text


def test_legacy_webui_defends_non_array_subscription_and_cached_rows():
    text = LEGACY_INDEX.read_text()
    assert "const subscriptions = safeArray(sd.subscriptions);" in text
    assert "document.getElementById('settingSubURLs').value = subscriptions.join('\\n');" in text
    assert "_savedSubSnapshot = JSON.stringify({urls: subscriptions.join('\\n')" in text
    assert "cloudflareLastData = safeArray(rows).filter(item => item && typeof item === 'object');" in text
    assert "cloudflareVisibleData = safeArray(cloudflareLastData).filter" in text
    assert "reputationLastData = safeArray(rows).filter(item => item && typeof item === 'object');" in text
    assert "(sd.subscriptions || []).join" not in text
    assert "cloudflareLastData || []" not in text
    assert "reputationLastData = rows || []" not in text


def test_legacy_webui_defends_non_array_debug_timeline():
    text = LEGACY_INDEX.read_text()
    assert "const timeline = safeArray(n.timeline).filter(t => t && typeof t === 'object');" in text
    assert "const tl = timeline.map(t =>" in text
    assert "(n.timeline||[]).map" not in text


def test_legacy_webui_defends_non_array_debug_and_export_state():
    text = LEGACY_INDEX.read_text()
    assert "const debugNodes = safeArray(window._debugNodes);" in text
    assert "debugNodes.forEach(n =>" in text
    assert "const failSorted = debugNodes.filter" in text
    assert "const visibleRows = safeArray(cloudflareVisibleData);" in text
    assert "const cfRows = visibleRows.length ? visibleRows : safeArray(cloudflareLastData);" in text
    assert "JSON.stringify(cfRows, null, 2)" in text
    assert "cfRows.forEach(item =>" in text
    assert "const repRows = safeArray(reputationLastData);" in text
    assert "JSON.stringify(repRows, null, 2)" in text
    assert "repRows.forEach(item =>" in text
    assert "window._debugNodes.forEach" not in text
    assert "window._debugNodes.filter" not in text
    assert "reputationLastData.forEach" not in text
    assert "function cloudflareVisibleItem(idx)" in text
    assert "return safeArray(cloudflareVisibleData)[idx];" in text
    assert "const item = cloudflareVisibleItem(idx);" in text
    assert "cloudflareVisibleData[idx]" not in text
    assert "cloudflareVisibleData.length ? cloudflareVisibleData : cloudflareLastData" not in text
    assert "(cloudflareVisibleData.length ? cloudflareVisibleData : cloudflareLastData).forEach" not in text


if __name__ == "__main__":
    test_nodes_page_query_preserves_availability_all()
    test_nodes_api_sanitizes_response_shape_before_pages_render()
    test_legacy_webui_defends_non_array_node_payloads()
    test_legacy_webui_defends_non_array_result_payloads()
    test_legacy_webui_defends_non_array_subscription_and_cached_rows()
    test_legacy_webui_defends_non_array_debug_timeline()
    test_legacy_webui_defends_non_array_debug_and_export_state()
