from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SETTINGS_PAGE = ROOT / "web" / "src" / "pages" / "SettingsPage.tsx"
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


def test_disabled_only_free_proxy_source_changes_do_not_trigger_refresh():
    text = read(SERVER)
    assert "freeProxySignatureChanged := oldFreeProxySignature != freeProxyRefreshSignature(s.cfgSrc)" in text
    assert "needFreeProxyRefresh := freeProxySignatureChanged && hasEnabledFreeProxySourceConfigs(s.cfgSrc.FreeProxySources)" in text
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


if __name__ == "__main__":
    test_settings_page_does_not_overwrite_dirty_draft_on_refetch()
    test_settings_page_tracks_reload_and_free_proxy_refresh_status()
    test_disabled_only_free_proxy_source_changes_do_not_trigger_refresh()
    test_settings_page_refreshes_runtime_node_caches_after_background_changes()
    test_settings_page_polls_subscription_refresh_and_refreshes_node_caches()
