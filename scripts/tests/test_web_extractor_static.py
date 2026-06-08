from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EXTRACTOR_PAGE = ROOT / "web" / "src" / "pages" / "ExtractorPage.tsx"


def read_source() -> str:
    return EXTRACTOR_PAGE.read_text()


def test_extractor_run_and_copy_handles_mutate_async_errors():
    text = read_source()
    assert "const runAndCopy = async () =>" in text
    assert "try {" in text
    assert "await mutation.mutateAsync(params)" in text
    assert "catch (e)" in text
    assert "生成并复制失败" in text


def test_extractor_copy_and_download_guard_empty_results():
    text = read_source()
    assert "请先生成代理" in text
    assert "copyAll" in text
    assert "download" in text
    assert "URL.revokeObjectURL" in text


if __name__ == "__main__":
    test_extractor_run_and_copy_handles_mutate_async_errors()
    test_extractor_copy_and_download_guard_empty_results()
