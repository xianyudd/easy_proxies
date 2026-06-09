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
    assert "const debugNodes = Array.isArray(d.nodes) ? d.nodes : [];" in text
    assert "tb.innerHTML = debugNodes.map(n =>" in text
    assert "window._debugNodes = debugNodes;" in text
    assert "allNodesCache = data.nodes || [];" not in text
    assert "configNodes = data.nodes || [];" not in text
    assert "(d.nodes||[]).map" not in text
    assert "window._debugNodes = d.nodes || [];" not in text


if __name__ == "__main__":
    test_nodes_page_query_preserves_availability_all()
    test_nodes_api_sanitizes_response_shape_before_pages_render()
    test_legacy_webui_defends_non_array_node_payloads()
