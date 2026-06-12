from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOWNLOAD_PAGES = [
    ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx",
    ROOT / "web" / "src" / "pages" / "DiagnosticsPage.tsx",
]
EXTRACTOR_PAGE = ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx"
DIAGNOSTICS_PAGE = ROOT / "web" / "src" / "pages" / "DiagnosticsPage.tsx"


def test_download_urls_are_revoked_after_click_tick():
    for path in DOWNLOAD_PAGES:
        text = path.read_text()
        assert "URL.createObjectURL" in text
        assert "window.setTimeout(() => URL.revokeObjectURL(url), 0)" in text
        assert "URL.revokeObjectURL(a.href)" not in text


def test_downloads_have_stable_user_facing_filenames_and_text_blobs():
    extractor = EXTRACTOR_PAGE.read_text()
    diagnostics = DIAGNOSTICS_PAGE.read_text()
    assert "type:'text/plain;charset=utf-8'" in extractor
    assert "a.download = 'proxy_extractor.txt'" in extractor
    assert "type:'text/plain'" in diagnostics
    assert "a.download = 'easy_proxies.log'" in diagnostics


if __name__ == "__main__":
    test_download_urls_are_revoked_after_click_tick()
    test_downloads_have_stable_user_facing_filenames_and_text_blobs()
