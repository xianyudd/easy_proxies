#!/usr/bin/env python3
"""Classify free proxies by real HTTP/HTTPS behavior.

The script intentionally shells out to curl because curl's proxy handling is the
same tool operators usually use to consume exported local proxies. It avoids
library-specific proxy behavior when diagnosing HTTP-only / CONNECT / HTTPS
capabilities.
"""
from __future__ import annotations

import argparse
import asyncio
import json
import statistics
import time
from dataclasses import dataclass, asdict
from pathlib import Path
from typing import Iterable

OK_CODES = {"200", "204", "206", "301", "302"}


@dataclass(frozen=True)
class Probe:
    name: str
    url: str
    method: str = "GET"
    expect: tuple[str, ...] = ("200",)
    insecure: bool = False
    proxytunnel: bool = False
    headers: tuple[str, ...] = ()
    data: str | None = None


PROBES = [
    Probe("http_cf204", "http://cp.cloudflare.com/generate_204", expect=("204",)),
    Probe("http_httpbin_ip", "http://httpbin.org/ip", expect=("200",)),
    Probe("http_example", "http://example.com/", expect=("200",)),
    Probe("http_neverssl", "http://neverssl.com/", expect=("200", "301", "302")),
    Probe("head_example", "http://example.com/", method="HEAD", expect=("200",)),
    Probe("post_httpbin", "http://httpbin.org/post", method="POST", data="easy_proxies=1", expect=("200",)),
    Probe("range_example", "http://example.com/", headers=("Range: bytes=0-99",), expect=("206", "200")),
    Probe("connect80_example", "http://example.com/", proxytunnel=True, expect=("200",)),
    Probe("https_example", "https://example.com/", expect=("200",)),
    Probe("https_cf_trace", "https://www.cloudflare.com/cdn-cgi/trace", expect=("200",)),
    Probe("https_httpbin_ip", "https://httpbin.org/ip", expect=("200",)),
    Probe("https_ipify", "https://api.ipify.org?format=json", expect=("200",)),
    Probe("https_github", "https://github.com/", expect=("200", "301", "302")),
]

PROBE_BY_NAME = {p.name: p for p in PROBES}


def load_proxies(path: Path, limit: int = 0) -> list[str]:
    seen = set()
    out: list[str] = []
    for line in path.read_text().splitlines():
        proxy = line.strip()
        if not proxy or proxy.startswith("#") or proxy in seen:
            continue
        seen.add(proxy)
        out.append(proxy)
        if limit > 0 and len(out) >= limit:
            break
    return out


def curl_cmd(proxy: str, probe: Probe, timeout: float) -> list[str]:
    cmd = [
        "curl",
        "--noproxy",
        "*",
        "--max-time",
        str(timeout),
        "-sS",
        "--max-filesize",
        "262144",
        "-w",
        "\n__EASY_PROXY_META__%{http_code} %{time_total}",
        "-x",
        proxy,
    ]
    if probe.insecure:
        cmd.append("-k")
    if probe.proxytunnel:
        cmd.append("--proxytunnel")
    if probe.method == "HEAD":
        cmd.append("-I")
    elif probe.method != "GET":
        cmd += ["-X", probe.method]
    for header in probe.headers:
        cmd += ["-H", header]
    if probe.data is not None:
        cmd += ["--data", probe.data]
    cmd.append(probe.url)
    return cmd


def validate_body(name: str, body: str, code: str) -> bool:
    if code == "000":
        return False
    if name == "http_cf204":
        return code == "204"
    if name in {"http_example", "https_example", "range_example", "connect80_example"}:
        return "Example Domain" in body or code in {"206", "301", "302"}
    if name == "http_neverssl":
        return "NeverSSL" in body or "neverSSL" in body or code in {"301", "302"}
    if name in {"http_httpbin_ip", "https_httpbin_ip"}:
        return "origin" in body and "{" in body
    if name == "post_httpbin":
        return "origin" in body and ("form" in body or "data" in body)
    if name == "https_cf_trace":
        return "ip=" in body and ("loc=" in body or "colo=" in body)
    if name == "https_ipify":
        return "ip" in body and "{" in body
    if name == "https_github":
        return "GitHub" in body
    return True


async def run_probe(proxy: str, probe: Probe, timeout: float, sem: asyncio.Semaphore) -> dict:
    async with sem:
        start = time.perf_counter()
        proc = await asyncio.create_subprocess_exec(
            *curl_cmd(proxy, probe, timeout),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        try:
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout + 1.5)
        except asyncio.TimeoutError:
            with contextlib.suppress(ProcessLookupError):
                proc.kill()
            return {"code": "000", "ok": False, "latency_ms": round((time.perf_counter() - start) * 1000), "error": "timeout"}
        text = stdout.decode(errors="replace")
        marker = "\n__EASY_PROXY_META__"
        body = text
        meta = ""
        if marker in text:
            body, meta = text.rsplit(marker, 1)
        parts = meta.strip().split()
        code = parts[0] if parts else "000"
        curl_time = None
        if len(parts) > 1:
            try:
                curl_time = float(parts[1])
            except ValueError:
                curl_time = None
        latency_ms = round((curl_time if curl_time is not None else time.perf_counter() - start) * 1000)
        err = stderr.decode(errors="replace").strip()
        body_ok = validate_body(probe.name, body, code)
        return {"code": code, "ok": code in probe.expect and body_ok, "body_ok": body_ok, "latency_ms": latency_ms, "error": err[:300]}


# contextlib import kept late in file to make the top-level dependency list small.
import contextlib  # noqa: E402


async def classify_proxy(proxy: str, probes: list[Probe], timeout: float, sem: asyncio.Semaphore) -> dict:
    results = {}
    for probe in probes:
        results[probe.name] = await run_probe(proxy, probe, timeout, sem)
    return {"proxy": proxy, "probes": results, "classes": classes_for(results)}


def ok(results: dict, name: str) -> bool:
    return bool(results.get(name, {}).get("ok"))


def classes_for(results: dict) -> list[str]:
    http_basic = ok(results, "http_cf204") and ok(results, "http_httpbin_ip") and ok(results, "http_example")
    http_methods = http_basic and ok(results, "head_example") and ok(results, "post_httpbin") and ok(results, "range_example")
    connect80 = ok(results, "connect80_example")
    simple_https = ok(results, "https_example") and ok(results, "https_cf_trace") and ok(results, "https_httpbin_ip")
    exit_ip_https = ok(results, "https_ipify")
    complex_https = ok(results, "https_github")

    classes = []
    if http_basic:
        classes.append("http_basic")
    if http_methods:
        classes.append("http_methods")
    if connect80:
        classes.append("connect80")
    if simple_https:
        classes.append("simple_https")
    if exit_ip_https:
        classes.append("exit_ip_https")
    if complex_https:
        classes.append("complex_https")

    if http_methods and simple_https and exit_ip_https and complex_https:
        classes.append("general_web")
    elif http_methods and simple_https:
        classes.append("simple_web_scraping")
    elif http_methods:
        classes.append("http_only_scraping")
    elif http_basic:
        classes.append("http_basic_only")
    else:
        classes.append("reject")
    return classes


def pct(values: list[int], p: float) -> int | None:
    if not values:
        return None
    values = sorted(values)
    k = (len(values) - 1) * p / 100
    f = int(k)
    c = min(f + 1, len(values) - 1)
    if f == c:
        return round(values[f])
    return round(values[f] * (c - k) + values[c] * (k - f))


def latency_summary(records: list[dict], probe_name: str) -> dict:
    vals = [int(r["probes"].get(probe_name, {}).get("latency_ms", 0)) for r in records if r["probes"].get(probe_name, {}).get("ok")]
    if not vals:
        return {"n": 0}
    return {"n": len(vals), "min": min(vals), "p50": pct(vals, 50), "p90": pct(vals, 90), "max": max(vals), "avg": round(statistics.mean(vals))}


def write_outputs(records: list[dict], out_dir: Path) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "classification.jsonl").write_text("\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n")
    headers = ["proxy"] + [p.name for p in PROBES] + ["classes"]
    lines = ["\t".join(headers)]
    for r in records:
        row = [r["proxy"]]
        row += [r["probes"].get(p.name, {}).get("code", "") for p in PROBES]
        row.append(",".join(r["classes"]))
        lines.append("\t".join(row))
    (out_dir / "classification.tsv").write_text("\n".join(lines) + "\n")

    class_counts: dict[str, int] = {}
    for r in records:
        for cls in r["classes"]:
            class_counts[cls] = class_counts.get(cls, 0) + 1
    for cls in sorted(class_counts):
        proxies = [r["proxy"] for r in records if cls in r["classes"]]
        (out_dir / f"{cls}.txt").write_text("\n".join(proxies) + ("\n" if proxies else ""))

    probe_counts = {}
    for probe in PROBES:
        dist: dict[str, int] = {}
        for r in records:
            code = r["probes"].get(probe.name, {}).get("code", "000")
            dist[code] = dist.get(code, 0) + 1
        probe_counts[probe.name] = {
            "ok": sum(1 for r in records if r["probes"].get(probe.name, {}).get("ok")),
            "codes": dict(sorted(dist.items(), key=lambda kv: (-kv[1], kv[0]))),
            "latency_ms": latency_summary(records, probe.name),
        }
    summary = {"total": len(records), "classes": dict(sorted(class_counts.items())), "probes": probe_counts}
    (out_dir / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(summary, ensure_ascii=False, indent=2))


async def main_async(args: argparse.Namespace) -> int:
    probes = PROBES
    if args.probes:
        wanted = set(args.probes.split(","))
        probes = [p for p in PROBES if p.name in wanted]
        missing = wanted - {p.name for p in probes}
        if missing:
            raise SystemExit(f"unknown probes: {', '.join(sorted(missing))}")
    proxies = load_proxies(args.input, args.limit)
    sem = asyncio.Semaphore(args.concurrency)
    records = []
    total = len(proxies)
    started = time.time()
    for idx, proxy in enumerate(proxies, 1):
        # Schedule in batches so stdout progress remains bounded and memory use predictable.
        records.append(proxy)
    batch_size = max(args.concurrency * 2, 1)
    classified: list[dict] = []
    for offset in range(0, total, batch_size):
        batch = proxies[offset : offset + batch_size]
        classified.extend(await asyncio.gather(*(classify_proxy(p, probes, args.timeout, sem) for p in batch)))
        if not args.quiet:
            elapsed = time.time() - started
            print(f"progress {min(offset+len(batch), total)}/{total} elapsed={elapsed:.1f}s", flush=True)
    write_outputs(classified, args.output_dir)
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Classify free proxies by HTTP/HTTPS behavior")
    parser.add_argument("-i", "--input", type=Path, required=True, help="Input proxy list, one proxy URL per line")
    parser.add_argument("-o", "--output-dir", type=Path, required=True, help="Directory for classification outputs")
    parser.add_argument("-c", "--concurrency", type=int, default=80)
    parser.add_argument("-t", "--timeout", type=float, default=6.0)
    parser.add_argument("--limit", type=int, default=0, help="Limit proxies for sampling; 0 means all")
    parser.add_argument("--probes", default="", help="Comma-separated probe names to run; default runs all")
    parser.add_argument("--quiet", action="store_true")
    return parser.parse_args()


def main() -> int:
    return asyncio.run(main_async(parse_args()))


if __name__ == "__main__":
    raise SystemExit(main())
