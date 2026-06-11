from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATATABLE = ROOT / "web" / "src" / "components" / "ui" / "DataTable.tsx"


def test_datatable_renders_empty_state_for_empty_children_array():
    text = DATATABLE.read_text()
    assert "Array.isArray(children) ? children.length > 0 : Boolean(children)" in text
    assert "hasRows ? children" in text
    assert "empty-cell" in text
    assert "children ||" not in text


if __name__ == "__main__":
    test_datatable_renders_empty_state_for_empty_children_array()
