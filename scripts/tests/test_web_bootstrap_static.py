from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MAIN = ROOT / "web" / "src" / "main.tsx"


def read(path: Path) -> str:
    return path.read_text()


def test_main_reports_clear_error_when_root_container_missing():
    text = read(MAIN)
    assert "const rootElement = document.getElementById('root')" in text
    assert "if (!rootElement)" in text
    assert "Easy Proxies WebUI failed to start" in text
    assert "createRoot(rootElement).render" in text
    assert "document.getElementById('root')!" not in text


if __name__ == "__main__":
    test_main_reports_clear_error_when_root_container_missing()
