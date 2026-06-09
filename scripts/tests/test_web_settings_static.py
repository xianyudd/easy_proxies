from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
SETTINGS_PAGE = ROOT / "web" / "src" / "pages" / "SettingsPage.tsx"
SETTINGS_TYPES = ROOT / "web" / "src" / "types" / "settings.ts"
SERVER = ROOT / "internal" / "monitor" / "server.go"


def read(path: Path) -> str:
    return path.read_text()


def test_settings_page_does_not_overwrite_dirty_draft_on_refetch():
    text = read(SETTINGS_PAGE)
    assert "settingsDirty" in text
    assert "subsDirty" in text
    assert "if (!settingsDirty) setDraft(settings.data)" in text
    assert "if (!subsDirty) setSubs(listValue(settings.data.subscriptions))" in text
    assert "后台状态刷新不会覆盖当前表单草稿" in text


def test_settings_page_tracks_reload_and_free_proxy_refresh_status():
    text = read(SETTINGS_PAGE)
    assert "getReloadStatus" in text
    assert "getFreeProxyRefreshStatus" in text
    assert "freeProxyRefreshTitle" in text
    assert "freeProxyRefreshDescription" in text
    assert "free_proxy_refresh_started" in text
    assert "reload_started" in text


def test_settings_page_surfaces_pending_free_proxy_refresh():
    page = read(SETTINGS_PAGE)
    types = read(SETTINGS_TYPES)
    assert "refresh_pending?: boolean" in types
    assert "pending_requested_by?: string" in types
    assert "free_proxy_refresh_pending?: boolean" in types
    assert "free_proxy_refresh_status?.refresh_pending" in page
    assert "free_proxy_refresh_pending" in page
    assert "新配置刷新已排队" in page
    assert "pending_requested_by" in page


def test_settings_section_hashes_route_and_scroll_reliably():
    app = read(APP)
    page = read(SETTINGS_PAGE)
    for section in ("#subscriptions", "#free-proxy", "#pool", "#multi-port", "#routing", "#quality-check", "#management"):
        assert f"['{section}', 'settings']" in app
    assert "scrollToHashSection" in page
    assert "window.addEventListener('hashchange', scrollToHashSection)" in page
    assert "window.removeEventListener('hashchange', scrollToHashSection)" in page
    assert "document.getElementById(id)?.scrollIntoView" in page


def test_disabled_only_free_proxy_source_changes_do_not_trigger_refresh():
    text = read(SERVER)
    assert "freeProxySignatureChanged := oldFreeProxySignature != freeProxyRefreshSignature(s.cfgSrc)" in text
    assert "oldFreeProxyRefreshable := freeProxyRefreshable(s.cfgSrc)" in text
    assert "needFreeProxyRefresh := freeProxySignatureChanged && freeProxyRefreshable(s.cfgSrc)" in text
    assert "needReload = oldFreeProxyRefreshable" in text
    assert "if freeProxySignatureChanged" in text


def test_settings_page_refreshes_runtime_node_caches_after_background_changes():
    text = read(SETTINGS_PAGE)
    assert "useQueryClient" in text
    assert "queryClient.invalidateQueries({ queryKey:['nodes-page'] })" in text
    assert "queryClient.invalidateQueries({ queryKey:['nodes-summary'] })" in text
    assert "queryClient.invalidateQueries({ queryKey:['nodes'] })" in text
    assert "queryClient.invalidateQueries({ queryKey:['status-nodes-all'] })" in text
    assert "refreshRuntimeNodeCaches" in text


def test_settings_page_polls_subscription_refresh_and_refreshes_node_caches():
    text = read(SETTINGS_PAGE)
    assert "subscriptionRefreshState" in text
    assert "subscriptionRefreshObservedRunning" in text
    assert "refetchInterval: subscriptionRefreshState === 'refreshing' ? 800 : false" in text
    assert "setSubscriptionRefreshState('refreshing')" in text
    assert "setSubscriptionRefreshObservedRunning(false)" in text
    assert "nodes_modified" in text
    assert "refreshRuntimeNodeCaches()" in text


def test_settings_page_defends_non_array_quality_cache_rows():
    text = read(SETTINGS_PAGE)
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "const cfRows = safeRows<CloudflareResult>(cfCache.data?.data)" in text
    assert "const repRows = safeRows<ReputationResult>(repCache.data?.data)" in text
    assert "cfCache.data?.data || []" not in text
    assert "repCache.data?.data || []" not in text


def test_management_rebound_url_hint_is_guarded():
    text = read(SETTINGS_PAGE)
    assert "function buildManagementRedirectUrl" in text
    assert "function isSafeManagementRedirectTarget" in text
    assert "try {" in text
    assert "new URL(hint, window.location.href)" in text
    assert "target.protocol === 'http:' || target.protocol === 'https:'" in text
    assert "target.hostname === window.location.hostname" in text
    assert "['127.0.0.1', 'localhost', '::1'].includes(target.hostname)" in text
    assert "if (!isSafeManagementRedirectTarget(target)) return ''" in text
    assert "catch" in text
    assert "管理端口已热切换，但后端返回的跳转地址无效" in text
    assert "target.href" in text


def test_settings_page_defends_non_array_free_proxy_refresh_sources():
    text = read(SETTINGS_PAGE)
    assert "type FreeProxyRefreshSource" in text
    assert "const freeRefreshSourceRows = safeRows<FreeProxyRefreshSource>(freeRefresh?.sources)" in text
    assert "freeRefreshSourceRows.length" in text
    assert "freeRefreshSourceRows.map(src =>" in text
    assert "freeRefresh?.sources?.length" not in text
    assert "freeRefresh.sources.map" not in text


if __name__ == "__main__":
    test_settings_page_does_not_overwrite_dirty_draft_on_refetch()
    test_settings_page_tracks_reload_and_free_proxy_refresh_status()
    test_settings_page_surfaces_pending_free_proxy_refresh()
    test_settings_section_hashes_route_and_scroll_reliably()
    test_disabled_only_free_proxy_source_changes_do_not_trigger_refresh()
    test_settings_page_refreshes_runtime_node_caches_after_background_changes()
    test_settings_page_polls_subscription_refresh_and_refreshes_node_caches()
    test_settings_page_defends_non_array_quality_cache_rows()
    test_management_rebound_url_hint_is_guarded()
    test_settings_page_defends_non_array_free_proxy_refresh_sources()
