import subprocess
import textwrap
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
SETTINGS_PAGE = ROOT / "web" / "src" / "pages" / "SettingsPage.tsx"
SETTINGS_SAVE_PAYLOAD = ROOT / "web" / "src" / "pages" / "settingsSavePayload.ts"
SETTINGS_TYPES = ROOT / "web" / "src" / "types" / "settings.ts"
SERVER = ROOT / "internal" / "monitor" / "server.go"


def read(path: Path) -> str:
    return path.read_text()


def test_settings_page_does_not_overwrite_dirty_draft_on_refetch():
    text = read(SETTINGS_PAGE)
    assert "settingsDirty" in text
    assert "subsDirty" in text
    assert "if (!settingsDirty) {" in text
    assert "setDraft(settings.data)" in text
    assert "if (!subsDirty) setSubs(listValue(settings.data.subscriptions))" in text
    assert "后台状态刷新不会覆盖当前表单草稿" in text
    assert "const updateDirtyState = (nextDraft: SettingsResponse" in text
    assert "isSettingsDraftDirty(nextDraft, settings.data)" in text
    assert "Boolean(passwordDraft.trim()) || clearPassword" in text
    assert "if (!settingsDirty) {" in text
    assert "if (subsDirty) saveSubscriptions()" in text
    assert "if (!subsDirty) return" in text
    assert "disabled={save.isPending || saveSub.isPending || settingsUnavailable || !hasUnsavedChanges}" in text
    assert "disabled={saveSub.isPending || settingsUnavailable || !subsDirty}" in text


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
    assert "function settingsSectionFromHash(hash: string): SettingsSectionId | ''" in page
    assert "if (id === 'settings' || id === '') return 'subscriptions'" in page
    assert "return settingsSectionFromHash(window.location.hash) || 'subscriptions'" in page
    assert "syncHashSection" in page
    assert "const id = settingsSectionFromHash(window.location.hash)" in page
    assert "window.addEventListener('hashchange', syncHashSection)" in page
    assert "window.removeEventListener('hashchange', syncHashSection)" in page
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


def test_settings_page_explains_disabled_free_proxy_sources_and_validates_only_enabled_rows():
    text = read(SETTINGS_PAGE)
    assert "freeSourcesEnabledCount" in text
    assert "freeProxyHasNoEnabledSources" in text
    assert "const freeSourcesEnabledCount = freeSources.filter(src => src.enabled !== false).length" in text
    assert "<span>启用源</span><strong>{freeSourcesEnabledCount}</strong>" in text
    assert "<span>启用源</span><strong>{Number(freeRefresh?.enabled_sources" not in text
    assert "当前免费代理源都未启用，手动刷新不会下载任何源" in text
    assert "启用至少一个源并保存后，系统才会后台下载、筛选、写缓存并按配置自动重载" in text
    assert "freeProxyRefreshNeedsSavedDraft" in text
    assert "手动刷新只读取后端已保存配置" in text
    assert "freeProxyHasNoEnabledSources || freeProxyRefreshNeedsSavedDraft" in text
    assert "src.enabled !== false && !String(src.url || src.file || '').trim()" in text


def test_settings_page_can_batch_enable_or_disable_free_proxy_sources():
    text = read(SETTINGS_PAGE)
    assert "const enableAllFreeSources = () => updateDraft" in text
    assert "freeSources.map(item => ({...item, enabled: true}))" in text
    assert "const disableAllFreeSources = () => updateDraft" in text
    assert "freeSources.map(item => ({...item, enabled: false}))" in text
    assert "const resetFreeSourcesToDefaultEnabled = () => updateDraft" in text
    assert "delete next.enabled" in text
    assert "启用全部" in text
    assert "全部停用" in text


def test_settings_page_has_sticky_unsaved_action_bar():
    text = read(SETTINGS_PAGE)
    css = read(ROOT / "web" / "src" / "styles" / "globals.css")
    assert "settings-sticky-actions" in text
    assert "aria-label=\"未保存设置操作条\"" in text
    assert "const resetDrafts = () =>" in text
    assert "放弃更改" in text
    assert ".settings-sticky-actions" in css
    assert "position: fixed" in css
    assert "默认启用" in text
    assert "disabled={!freeSources.length || freeSourcesEnabledCount === freeSources.length}" in text
    assert "disabled={!freeSources.length || freeSourcesEnabledCount === 0}" in text


def test_settings_page_defends_non_array_free_proxy_refresh_sources():
    text = read(SETTINGS_PAGE)
    assert "type FreeProxyRefreshSource" in text
    assert "const freeRefreshSourceRows = safeRows<FreeProxyRefreshSource>(freeRefresh?.sources)" in text
    assert "freeRefreshSourceRows.length" in text
    assert "freeRefreshSourceRows.map(src =>" in text
    assert "freeRefresh?.sources?.length" not in text
    assert "freeRefresh.sources.map" not in text


def test_settings_page_infers_socks5_default_scheme_for_free_proxy_source_display():
    text = read(SETTINGS_PAGE)
    helper = read(SETTINGS_SAVE_PAYLOAD)
    assert "function freeSourceDefaultScheme" in helper
    assert "hint.includes('socks5') ? 'socks5' : 'http'" in helper
    assert "value={freeSourceDefaultScheme(src)}" in text
    assert "未显式设置时会根据源名/URL 中的 socks5 自动显示" in text
    assert "value={src.default_scheme || 'http'}" not in text
    assert "default_scheme: freeSourceDefaultScheme(src)" in helper


def test_free_proxy_sources_use_readable_card_layout():
    page = read(SETTINGS_PAGE)
    css = read(ROOT / "web" / "src" / "styles" / "globals.css")
    assert "free-source-card-head" in page
    assert "free-source-card-grid" in page
    assert "free-source-url-field" in page
    assert "free-source-list-note" in page
    assert "{Number(src.max_nodes || 0) > 0 ? `最多 ${src.max_nodes} 条` : '全量解析'}" in page
    assert ".free-source-card-head" in css
    assert ".free-source-card-grid" in css
    assert "minmax(340px, 2fr)" in css
    assert ".free-source-url-field" in css
    assert ".free-source-card-grid .settings-helper-text" in css


def test_settings_page_uses_focused_section_layout_for_long_content():
    page = read(SETTINGS_PAGE)
    css = read(ROOT / "web" / "src" / "styles" / "globals.css")
    assert "type SettingsSectionId" in page
    assert "SETTINGS_SECTIONS" in page
    assert "activeSettingsSection" in page
    assert "settings-focus-strip" in page
    assert "当前只展示一个设置分区" in page
    assert "sectionClass('subscriptions'" in page
    assert "gridClass('pool', 'multi-port')" in page
    assert "gridClass('routing', 'quality-check')" in page
    assert "settings-section-hidden" in css
    assert "settings-card-grid-hidden" in css
    assert "settings-anchor-nav a.active" in css
    assert "aria-current={activeSettingsSection === section.id ? 'page' : undefined}" in page
    assert "grid-template-columns: repeat(2, minmax(0, 1fr))" in css
    assert ".settings-anchor-nav span:not(.nav-kicker)" in css


def test_settings_management_password_is_write_only_in_ui():
    page = read(SETTINGS_PAGE)
    helper = read(SETTINGS_SAVE_PAYLOAD)
    server = read(SERVER)
    assert '"password":     ""' in server
    assert '"password_set": strings.TrimSpace(cfg.Management.Password) != ""' in server
    assert "managementPasswordDraft" in page
    assert "managementPasswordClear" in page
    assert "updateDirtyState(draft, value, false)" in page
    assert "const nextPasswordDraft = value ? '' : managementPasswordDraft" in page
    assert "updateDirtyState(draft, nextPasswordDraft, value)" in page
    assert "function normalizeManagementForSave" in helper
    assert "next.clear_password = true" in helper
    assert "delete next.clear_password" in helper
    assert "delete next.password" in helper
    assert "delete next.password_set" in helper
    assert "新管理 password（留空保持不变）" in page
    assert "清空管理密码" in page
    assert "密码不会从接口回显" in page
    assert "buildSettingsSavePayload" in page
    assert "managementPasswordDraft" in page
    assert "String(mgmt.password||'')" not in page


def test_settings_save_payload_omits_unchanged_free_proxy_config(tmp_path):
    entry = tmp_path / "settings_save_payload_check.mjs"
    bundle = tmp_path / "settings_save_payload_check.bundle.mjs"
    entry.write_text(textwrap.dedent(f"""
        import assert from 'node:assert/strict'
        import {{ buildSettingsSavePayload }} from {str(SETTINGS_SAVE_PAYLOAD)!r}

        const server = {{
          quality_check: {{ enabled:false, count:500, interval:'1h0m0s', region:'all' }},
          management: {{ listen:'127.0.0.1:19093', password:'', password_set:true }},
          subscriptions: ['https://sub.example/a'],
          free_proxy_sources: [
            {{ name:'socks-list', url:'https://example.test/socks5.txt', file:'', format:'txt', enabled:true, timeout:'8s', max_nodes:0, max_bytes:0 }}
          ],
          free_proxy_filter: {{ enabled:true, min_tier:'http_basic', workers:200, timeout:'2s', max_candidates:0, max_probe_candidates:12000, probes:{{ http:'http://cp.cloudflare.com/generate_204', https:'https://example.com/' }} }},
          free_proxy_cache: {{ enabled:true, path:'/tmp/free.txt', refresh_on_start:false, auto_reload:true, workers:8, max_age:'6h0m0s' }},
          free_proxy_max_nodes: 0,
        }}
        const draftQualityOnly = JSON.parse(JSON.stringify(server))
        draftQualityOnly.quality_check.count = 499
        const payload = buildSettingsSavePayload({{
          draft: draftQualityOnly,
          serverSettings: server,
          management: draftQualityOnly.management,
          managementPasswordDraft: '',
          managementPasswordClear: false,
          subscriptions: server.subscriptions,
        }})
        assert.equal(payload.quality_check.count, 499)
        assert.equal(Object.hasOwn(payload, 'free_proxy_sources'), false)
        assert.equal(Object.hasOwn(payload, 'free_proxy_filter'), false)
        assert.equal(Object.hasOwn(payload, 'free_proxy_cache'), false)
        assert.equal(Object.hasOwn(payload, 'free_proxy_max_nodes'), false)

        const draftSourceChanged = JSON.parse(JSON.stringify(server))
        draftSourceChanged.free_proxy_sources[0].default_scheme = 'socks5'
        const payload2 = buildSettingsSavePayload({{
          draft: draftSourceChanged,
          serverSettings: server,
          management: draftSourceChanged.management,
          managementPasswordDraft: '',
          managementPasswordClear: false,
          subscriptions: server.subscriptions,
        }})
        assert.equal(Object.hasOwn(payload2, 'free_proxy_sources'), true)
        assert.equal(payload2.free_proxy_sources[0].default_scheme, 'socks5')

        import {{ isSettingsDraftDirty, isSubscriptionDraftDirty }} from {str(SETTINGS_SAVE_PAYLOAD)!r}
        assert.equal(isSubscriptionDraftDirty('https://a.example/sub\\nhttps://b.example/sub', ['https://a.example/sub', 'https://b.example/sub']), false)
        assert.equal(isSubscriptionDraftDirty('https://a.example/sub\\nhttps://b.example/sub\\n', ['https://a.example/sub', 'https://b.example/sub']), true)
        assert.equal(isSubscriptionDraftDirty('  https://a.example/sub  \\nhttps://b.example/sub  ', ['https://a.example/sub', 'https://b.example/sub']), false)
        assert.equal(isSubscriptionDraftDirty('https://a.example/sub\\nhttps://b.example/sub\\nhttps://c.example/sub', ['https://a.example/sub', 'https://b.example/sub']), true)

        const draftWithFreeSourceAddedAndRemoved = JSON.parse(JSON.stringify(server))
        draftWithFreeSourceAddedAndRemoved.free_proxy_sources.push({{ name:'new-free-source', enabled:true, url:'', format:'text', default_scheme:'http' }})
        draftWithFreeSourceAddedAndRemoved.free_proxy_sources.pop()
        assert.equal(isSettingsDraftDirty(draftWithFreeSourceAddedAndRemoved, server), false)
        const draftWithFreeSourceAdded = JSON.parse(JSON.stringify(server))
        draftWithFreeSourceAdded.free_proxy_sources.push({{ name:'new-free-source', enabled:true, url:'https://example.test/proxies.txt', format:'text', default_scheme:'http' }})
        assert.equal(isSettingsDraftDirty(draftWithFreeSourceAdded, server), true)
    """))
    subprocess.run([
        "node",
        "-e",
        (
            "const esbuild=require('./web/node_modules/esbuild');"
            f"esbuild.buildSync({{entryPoints:['{entry}'], bundle:true, platform:'node', format:'esm', outfile:'{bundle}', logLevel:'silent'}})"
        ),
    ], cwd=ROOT, check=True)
    subprocess.run(["node", str(bundle)], cwd=ROOT, check=True)


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
    test_settings_page_explains_disabled_free_proxy_sources_and_validates_only_enabled_rows()
    test_settings_page_can_batch_enable_or_disable_free_proxy_sources()
    test_settings_page_defends_non_array_free_proxy_refresh_sources()
    test_settings_page_infers_socks5_default_scheme_for_free_proxy_source_display()
    test_free_proxy_sources_use_readable_card_layout()
    test_settings_page_uses_focused_section_layout_for_long_content()
    test_settings_management_password_is_write_only_in_ui()
    test_settings_save_payload_omits_unchanged_free_proxy_config(Path("/tmp"))
