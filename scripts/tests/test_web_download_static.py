from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOWNLOAD_PAGES = [
    ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx",
    ROOT / "web" / "src" / "pages" / "DiagnosticsPage.tsx",
]


def test_download_urls_are_revoked_after_click_tick():
    for path in DOWNLOAD_PAGES:
        text = path.read_text()
        assert "URL.createObjectURL" in text
        assert "window.setTimeout(() => URL.revokeObjectURL(url), 0)" in text
        assert "URL.revokeObjectURL(a.href)" not in text


if __name__ == "__main__":
    test_download_urls_are_revoked_after_click_tick()
