from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EXTRACTOR_PAGE = ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx"
EXTRACTOR_STORE = ROOT / "web" / "src" / "store" / "extractorStore.ts"
EXTRACTOR_TYPES = ROOT / "web" / "src" / "types" / "extractor.ts"
FORMAT_RULES = ROOT / "web" / "src" / "components" / "extractor" / "formatRules.ts"
EXTRACTOR_API = ROOT / "web" / "src" / "api" / "extractor.ts"


def read(path: Path) -> str:
    return path.read_text()


def test_extractor_api_sends_boolean_reveal_values():
    text = read(EXTRACTOR_API)
    assert "reveal: params.reveal ? 'true' : 'false'" in text
    assert "reveal: params.reveal ? '1' : '0'" not in text


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
    assert "const meta = safeObject(data);" in text
    assert "const safeEntries = safeArray(entries);" in text
    assert "if (!safeEntries.length)" in text
    assert "const previewEntries = safeEntries.slice(0, 20);" in text
    assert "if (safeEntries.length > 20)" in text
    assert "meta.mode || 'extractor'" in text
    assert "meta.masked ? '复制脱敏' : '复制本条'" in text
    assert "meta.effective_format || 'unknown'" in text
    assert "entries.slice(0, 20)" not in text
    assert "entries.length > 20" not in text


def test_extractor_regions_are_iso_driven_not_legacy_union():
    types = read(EXTRACTOR_TYPES)
    rules = read(FORMAT_RULES)
    assert "export type ExtractorRegion = string" in types
    assert "REGION_OPTIONS" in rules
    assert "REGION_OPTIONS.map" in rules
    assert "'all'|'us'|'jp'" not in types


if __name__ == "__main__":
    test_extractor_api_sends_boolean_reveal_values()
    test_extractor_run_and_copy_handles_mutate_async_errors()
    test_extractor_copy_and_download_guard_empty_results()
    test_extractor_response_arrays_are_normalized_before_render_and_copy()
    test_legacy_extractor_cards_normalizes_entries_by_contract()
    test_extractor_regions_are_iso_driven_not_legacy_union()
