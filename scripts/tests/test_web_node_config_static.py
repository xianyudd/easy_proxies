from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
SIDEBAR = ROOT / "web" / "src" / "components" / "layout" / "Sidebar.tsx"
STORE = ROOT / "web" / "src" / "store" / "appStore.ts"
API = ROOT / "web" / "src" / "api" / "configNodes.ts"
PAGE = ROOT / "web" / "src" / "pages" / "NodeConfigPage.tsx"


def read(path: Path) -> str:
    return path.read_text()


def test_node_config_page_is_routable_from_react_shell():
    app = read(APP)
    sidebar = read(SIDEBAR)
    store = read(STORE)
    assert "NodeConfigPage" in app
    assert "activeTab === 'config'" in app
    assert "#config" in app
    assert "节点配置" in sidebar
    assert "ServerCog" in sidebar
    assert "'config'" in store


def test_node_config_api_client_covers_crud_and_reload():
    api = read(API)
    assert "getConfigNodes" in api
    assert "createConfigNode" in api
    assert "updateConfigNode" in api
    assert "deleteConfigNode" in api
    assert "reloadCore" in api
    assert "getReloadStatus" in api
    assert "'/api/nodes/config'" in api
    assert "`/api/nodes/config/${encodeURIComponent(name)}`" in api
    assert "'/api/reload'" in api
    assert "'/api/reload/status'" in api


def test_node_config_page_handles_crud_need_reload_and_reload_polling():
    page = read(PAGE)
    assert "useQuery({ queryKey: ['config-nodes']" in page
    assert "need_reload" in page
    assert "setNeedReload(true)" in page
    assert "reloadStatus" in page
    assert "refetchInterval: reloadState === 'reloading' ? 800 : false" in page
    assert "节点已保存" in page
    assert "节点已删除" in page
    assert "重载已在后台启动" in page
    assert "QueryErrorBanner" in page
    assert "confirmDeleteName" in page
    assert "确认删除" in page
    assert 'aria-label="节点名称"' in page
    assert 'aria-label="节点 URI"' in page
    assert 'aria-label="固定端口"' in page
    assert "uri" in page and "name" in page


def test_node_config_page_filters_paginates_and_wraps_long_uri():
    page = read(PAGE)
    assert "searchTerm" in page
    assert "sourceFilter" in page
    assert "filteredRows" in page
    assert "pagedRows" in page
    assert "Pagination" in page
    assert "pageSizeOptions" in page
    assert "setPage(1)" in page
    assert 'aria-label="搜索节点配置"' in page
    assert 'aria-label="筛选节点来源"' in page
    assert "node-config-uri" in page


def test_node_config_reload_refreshes_runtime_node_caches():
    page = read(PAGE)
    assert "queryClient.invalidateQueries({ queryKey: ['nodes-page'] })" in page
    assert "queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })" in page


def test_node_config_page_does_not_show_zero_counts_during_initial_load():
    page = read(PAGE)
    assert "const nodesLoadingWithoutData" in page
    assert "const countText" in page
    assert "countText(rows.length)" in page
    assert "countText(editableRows.length)" in page
    assert "countText(filteredRows.length)" in page
    assert "const listSubtitle = nodesLoadingWithoutData ? '加载中...' : `共 ${rows.length} 条" in page
    assert "{listSubtitle}" in page


def test_node_config_page_defends_non_array_nodes_payload():
    page = read(PAGE)
    assert "function safeRows<T>(rows: unknown): T[]" in page
    assert "const rows = useMemo(() => safeRows<ConfigNode>(nodesQuery.data?.nodes), [nodesQuery.data?.nodes])" in page
    assert "nodesQuery.data?.nodes || []" not in page


if __name__ == "__main__":
    test_node_config_page_is_routable_from_react_shell()
    test_node_config_api_client_covers_crud_and_reload()
    test_node_config_page_handles_crud_need_reload_and_reload_polling()
    test_node_config_page_filters_paginates_and_wraps_long_uri()
    test_node_config_reload_refreshes_runtime_node_caches()
    test_node_config_page_does_not_show_zero_counts_during_initial_load()
    test_node_config_page_defends_non_array_nodes_payload()
