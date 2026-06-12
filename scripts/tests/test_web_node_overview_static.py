from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NODE_OVERVIEW = ROOT / "web" / "src" / "pages" / "NodeOverviewPage.tsx"
ISO = ROOT / "web" / "src" / "data" / "iso3166.ts"
CSS = ROOT / "web" / "src" / "styles" / "globals.css"


def read_source() -> str:
    return NODE_OVERVIEW.read_text()


def test_node_overview_labels_all_backend_regions():
    text = read_source()
    iso = ISO.read_text()
    assert "regionMeta(code).label" in text
    for code in ("us", "jp", "hk", "sg", "tw", "kr", "in", "ae", "ch", "au", "de", "gb", "ca", "fr", "vn", "ru", "ua", "tr", "ng"):
        assert f'"code": "{code}"' in iso


def test_node_overview_reconciles_server_clamped_page_and_copies_stable_tag():
    text = read_source()
    assert "useEffect" in text
    assert "data.page !== page" in text
    assert "setPage(data.page)" in text
    assert "`tag=${node.tag || '-'}" in text
    assert "node.name || node.tag || '-'" in text


def test_node_overview_defends_non_array_nodes_payload():
    text = read_source()
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "const rows = safeRows<NodeSnapshot>(data?.nodes)" in text
    assert "data?.nodes || []" not in text


def test_node_overview_sanitizes_summary_records():
    text = read_source()
    assert "function safeRecord(value: unknown): Record<string, number>" in text
    assert "safeRecord(data?.region_stats)" in text
    assert "safeRecord(data?.source_stats)" in text
    assert "safeRecord(data?.region_healthy)" in text
    assert "Object.values(regionHealthy).reduce((sum, n) => sum + n, 0)" in text
    assert "Object.values(data?.region_healthy || {})" not in text


def test_node_overview_has_mobile_card_list_instead_of_dense_table_only():
    text = read_source()
    css = CSS.read_text()
    assert "overview-table-view" in text
    assert "overview-mobile-list" in text
    assert "className=\"node-card\"" in text
    assert "node-card-meta" in text
    assert ".overview-table-view" in css
    assert ".overview-mobile-list" in css
    assert "display: none" in css
    assert "display: grid" in css
    assert "window.innerWidth <= 640 ? 50 : 100" in text


if __name__ == "__main__":
    test_node_overview_labels_all_backend_regions()
    test_node_overview_reconciles_server_clamped_page_and_copies_stable_tag()
    test_node_overview_defends_non_array_nodes_payload()
    test_node_overview_sanitizes_summary_records()
    test_node_overview_has_mobile_card_list_instead_of_dense_table_only()
