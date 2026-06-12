from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
SETTINGS = ROOT / "web" / "src" / "pages" / "SettingsPage.tsx"
QUALITY = ROOT / "web" / "src" / "pages" / "QualityPage.tsx"
SIDEBAR = ROOT / "web" / "src" / "components" / "layout" / "Sidebar.tsx"
CSS = ROOT / "web" / "src" / "styles" / "globals.css"


def read(path: Path) -> str:
    return path.read_text()


def test_main_hash_routes_have_layout_targets():
    app = read(APP)
    sidebar = read(SIDEBAR)
    for hash_route in ("#extractor", "#overview", "#region-review", "#unclassified", "#config", "#quality", "#status", "#settings", "#diagnostics"):
        assert hash_route in app
    for label in ("代理提取", "节点总览", "待确认节点", "节点配置", "节点质量", "运行状态", "系统设置", "日志诊断"):
        assert label in sidebar


def test_settings_layout_is_section_focused_and_responsive():
    page = read(SETTINGS)
    css = read(CSS)
    assert "SETTINGS_SECTIONS" in page
    assert "activeSettingsSection" in page
    assert "settings-focus-strip" in page
    assert "当前只展示一个设置分区" in page
    for section in ("subscriptions", "free-proxy", "pool", "multi-port", "routing", "quality-check", "management"):
        assert f"id=\"{section}\"" in page or f"id='{section}'" in page or f"id=\"{section}\"" in css or section in page
    assert "settings-section-hidden" in css
    assert "settings-card-grid-hidden" in css
    assert "settings-anchor-nav" in css
    assert "@media (max-width: 980px)" in css
    assert "grid-template-columns: repeat(2, minmax(0, 1fr))" in css
    assert ".settings-anchor-nav span:not(.nav-kicker)" in css


def test_settings_management_password_field_is_write_only_layout():
    page = read(SETTINGS)
    assert "新管理 password（留空保持不变）" in page
    assert "清空管理密码" in page
    assert "密码不会从接口回显" in page
    assert "autoComplete={type === 'password' ? label.includes('新管理') ? 'new-password' : 'current-password'" in page
    assert "String(mgmt.password||'')" not in page


def test_quality_table_uses_horizontal_scroll_instead_of_page_overflow():
    page = read(QUALITY)
    css = read(CSS)
    assert "className=\"quality-table\"" in page
    assert "scroll={{ x: 1260 }}" in page
    assert ".quality-table .ant-table-container" in css
    assert "overflow-x: auto" in css


def test_mobile_shell_uses_grid_nav_without_document_overflow():
    css = read(CSS)
    assert "@media (max-width: 980px)" in css
    assert ".sidebar" in css
    mobile = css.split("@media (max-width: 640px)", 1)[1]
    assert "grid-template-columns: repeat(4, minmax(0, 1fr))" in mobile
    assert "overflow: visible" in mobile
    assert ".settings-anchor-nav" in css
    assert "grid-template-columns: repeat(2, minmax(0, 1fr))" in mobile


def test_mobile_primary_nav_fits_all_actions_at_phone_width():
    css = read(CSS)
    mobile = css.split("@media (max-width: 640px)", 1)[1]
    assert ".nav button" in mobile
    assert "min-width: 0" in mobile
    assert "min-height: 68px" in mobile
    assert "justify-items: center" in mobile
    assert "white-space: normal" in mobile
    assert "grid-template-columns: repeat(4, minmax(0, 1fr))" in mobile


if __name__ == "__main__":
    test_main_hash_routes_have_layout_targets()
    test_settings_layout_is_section_focused_and_responsive()
    test_settings_management_password_field_is_write_only_layout()
    test_quality_table_uses_horizontal_scroll_instead_of_page_overflow()
    test_mobile_shell_uses_grid_nav_without_document_overflow()
    test_mobile_primary_nav_fits_all_actions_at_phone_width()
