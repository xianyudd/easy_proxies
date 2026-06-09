from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EXTRACTOR_PAGE = ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx"
EXTRACTOR_STORE = ROOT / "web" / "src" / "store" / "extractorStore.ts"


def read(path: Path) -> str:
    return path.read_text()


def test_extractor_run_and_copy_handles_mutate_async_errors():
    text = read(EXTRACTOR_PAGE)
    assert "const runAndCopy = async () =>" in text
    assert "try {" in text
    assert "await mutation.mutateAsync(params)" in text
    assert "catch (e)" in text
    assert "生成并复制失败" in text


def test_extractor_copy_and_download_guard_empty_results():
    text = read(EXTRACTOR_PAGE)
    assert "请先生成代理" in text
    assert "copyAll" in text
    assert "download" in text
    assert "URL.revokeObjectURL" in text


def test_extractor_response_arrays_are_normalized_before_render_and_copy():
    store = read(EXTRACTOR_STORE)
    page = read(EXTRACTOR_PAGE)
    assert "function safeArray<T>(value: unknown): T[]" in store
    assert "entries: safeArray<ExtractorEntry>(data.entries)" in store
    assert "warnings: safeArray<string>(data.warnings)" in store
    assert "const generatedEntries = safeArray<ExtractorEntry>(data.entries)" in page
    assert "entriesToText(generatedEntries)" in page
    assert "data.entries || []" not in page


def test_legacy_extractor_cards_normalizes_entries_by_contract():
    legacy = ROOT / "internal" / "monitor" / "assets" / "index.html"
    text = legacy.read_text()
    assert "const safeEntries = safeArray(entries);" in text
    assert "if (!safeEntries.length)" in text
    assert "const previewEntries = safeEntries.slice(0, 20);" in text
    assert "if (safeEntries.length > 20)" in text
    assert "entries.slice(0, 20)" not in text
    assert "entries.length > 20" not in text


if __name__ == "__main__":
    test_extractor_run_and_copy_handles_mutate_async_errors()
    test_extractor_copy_and_download_guard_empty_results()
    test_extractor_response_arrays_are_normalized_before_render_and_copy()
    test_legacy_extractor_cards_normalizes_entries_by_contract()
