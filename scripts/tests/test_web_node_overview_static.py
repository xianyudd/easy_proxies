from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NODE_OVERVIEW = ROOT / "web" / "src" / "pages" / "NodeOverviewPage.tsx"


def read_source() -> str:
    return NODE_OVERVIEW.read_text()


def test_node_overview_labels_all_backend_regions():
    text = read_source()
    for code in ("us", "jp", "hk", "sg", "tw", "kr", "in", "ae", "ch", "au", "de", "gb", "ca", "other"):
        assert f"{code}:" in text


def test_node_overview_reconciles_server_clamped_page_and_copies_stable_tag():
    text = read_source()
    assert "useEffect" in text
    assert "data.page !== page" in text
    assert "setPage(data.page)" in text
    assert "`tag=${node.tag || '-'}" in text
    assert "node.name || node.tag || '-'" in text


if __name__ == "__main__":
    test_node_overview_labels_all_backend_regions()
    test_node_overview_reconciles_server_clamped_page_and_copies_stable_tag()
