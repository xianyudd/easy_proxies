from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NODES_API = ROOT / "web" / "src" / "api" / "nodes.ts"


def read_source() -> str:
    return NODES_API.read_text()


def test_nodes_page_query_preserves_availability_all():
    text = read_source()
    assert "value === 'all' && key !== 'availability'" in text
    assert "search.set(key, String(value))" in text


if __name__ == "__main__":
    test_nodes_page_query_preserves_availability_all()
