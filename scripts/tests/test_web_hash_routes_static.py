from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
SIDEBAR = ROOT / "web" / "src" / "components" / "layout" / "Sidebar.tsx"


def test_nodes_hash_routes_to_node_overview():
    text = APP.read_text()
    assert "['#nodes', 'overview']" in text
    assert "['#overview', 'overview']" in text
    assert "{activeTab === 'overview' && <NodeOverviewPage />}" in text


def test_node_overview_sidebar_keeps_stable_tab_id():
    text = SIDEBAR.read_text()
    assert "['overview', List, '节点总览']" in text
    assert "overview: 'nodes'" in text
    assert "`#${tabHashes[id]}`" in text
    assert "setActive(id)" in text


def test_region_review_hash_routes_and_sidebar_entry():
    app = APP.read_text()
    sidebar = SIDEBAR.read_text()
    assert "['#region-review', 'review']" in app
    assert "['#unclassified', 'review']" in app
    assert "{activeTab === 'review' && <RegionReviewPage />}" in app
    assert "['review', MapPin, '待确认节点']" in sidebar
    assert "review: 'region-review'" in sidebar


if __name__ == "__main__":
    test_nodes_hash_routes_to_node_overview()
    test_node_overview_sidebar_keeps_stable_tab_id()
    test_region_review_hash_routes_and_sidebar_entry()
