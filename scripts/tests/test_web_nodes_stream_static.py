from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NODES_API = ROOT / "web" / "src" / "api" / "nodes.ts"


def test_probe_all_uses_fetch_stream_instead_of_json_api_client():
    text = NODES_API.read_text()
    assert "export async function probeAllNodesStream" in text
    assert "fetch(path, { method: 'POST', credentials: 'same-origin' })" in text
    assert "return res" in text
    assert "new ApiError" in text
    assert "api.post('/api/nodes/probe-all')" not in text


if __name__ == "__main__":
    test_probe_all_uses_fetch_stream_instead_of_json_api_client()
