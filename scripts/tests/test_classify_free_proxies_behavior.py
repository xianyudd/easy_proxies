import asyncio
import importlib.util
import os
import signal
import stat
import sys
import tempfile
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "classify_free_proxies.py"
spec = importlib.util.spec_from_file_location("classify_free_proxies", SCRIPT)
mod = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = mod
spec.loader.exec_module(mod)


def pid_exists(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    return True


def install_fake_curl(tmp: Path) -> Path:
    pid_file = tmp / "curl.pid"
    fake = tmp / "curl"
    fake.write_text(f"#!/bin/sh\necho $$ > {pid_file}\nsleep 30\n")
    fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
    os.environ["PATH"] = f"{tmp}:{os.environ['PATH']}"
    return pid_file


async def timeout_case() -> None:
    with tempfile.TemporaryDirectory() as d:
        pid_file = install_fake_curl(Path(d))
        result = await mod.run_probe("http://127.0.0.1:1", mod.PROBES[0], 0.05, asyncio.Semaphore(1))
        assert result["error"] == "timeout"
        pid = int(pid_file.read_text())
        await asyncio.sleep(0.05)
        assert not pid_exists(pid), f"fake curl pid {pid} still exists after timeout cleanup"


async def cancellation_case() -> None:
    with tempfile.TemporaryDirectory() as d:
        pid_file = install_fake_curl(Path(d))
        task = asyncio.create_task(mod.run_probe("http://127.0.0.1:1", mod.PROBES[0], 10, asyncio.Semaphore(1)))
        for _ in range(100):
            if pid_file.exists():
                break
            await asyncio.sleep(0.01)
        assert pid_file.exists(), "fake curl did not start"
        pid = int(pid_file.read_text())
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass
        await asyncio.sleep(0.05)
        assert not pid_exists(pid), f"fake curl pid {pid} still exists after cancellation cleanup"


def ipify_body_validation_case() -> None:
    assert mod.validate_body("https_ipify", '{"ip":"203.0.113.10"}', "200")
    assert mod.validate_body("https_ipify", '{"ip":"2001:db8::1"}', "200")
    assert not mod.validate_body("https_ipify", '{"message":"ip ok"}', "200")
    assert not mod.validate_body("https_ipify", '{"ip":"not-an-ip"}', "200")
    assert not mod.validate_body("https_ipify", 'ip=203.0.113.10', "200")


if __name__ == "__main__":
    ipify_body_validation_case()
    asyncio.run(timeout_case())
    asyncio.run(cancellation_case())
