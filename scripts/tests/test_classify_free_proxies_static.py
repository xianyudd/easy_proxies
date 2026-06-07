from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / "classify_free_proxies.py"


def read_source() -> str:
    return SRC.read_text()


def test_timeout_kills_and_reaps_subprocess():
    text = read_source()
    timeout_block = text.split("except asyncio.TimeoutError:", 1)[1].split("return {\"code\": \"000\"", 1)[0]
    assert "proc.kill()" in timeout_block
    assert "await proc.communicate()" in timeout_block


def test_cancellation_kills_and_reaps_subprocess():
    text = read_source()
    cancel_block = text.split("except asyncio.CancelledError:", 1)[1].split("raise", 1)[0]
    assert "proc.kill()" in cancel_block
    assert "await asyncio.shield(proc.communicate())" in cancel_block


def test_concurrency_has_hard_cap():
    text = read_source()
    assert "MAX_CONCURRENCY = 80" in text
    assert "min(args.concurrency, MAX_CONCURRENCY)" in text


if __name__ == "__main__":
    test_timeout_kills_and_reaps_subprocess()
    test_cancellation_kills_and_reaps_subprocess()
    test_concurrency_has_hard_cap()
