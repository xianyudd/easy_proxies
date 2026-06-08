from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
QUALITY_PAGE = ROOT / "web" / "src" / "pages" / "QualityPage.tsx"
CLIENT = ROOT / "web" / "src" / "api" / "client.ts"
REPUTATION_API = ROOT / "web" / "src" / "api" / "reputation.ts"
REPUTATION_TYPES = ROOT / "web" / "src" / "types" / "reputation.ts"


def read(path: Path) -> str:
    return path.read_text()


def test_api_client_parses_json_text_fallback_for_accepted_jobs():
    text = read(CLIENT)
    assert "parseTextPayload(await res.text())" in text
    assert "JSON.parse(trimmed)" in text


def test_quality_page_validates_job_id_and_renders_job_panel_path():
    text = read(QUALITY_PAGE)
    assert "startQualityJob" in text
    assert "mutation.mutateAsync()" in text
    assert "后台任务响应缺少 job_id" in text
    assert "setJobId(job.job_id)" in text
    assert "后台质量检测任务" in text


def test_quality_page_uses_complete_region_options_and_labels():
    text = read(QUALITY_PAGE)
    assert "REGION_META" in text
    assert "QUALITY_REGION_OPTIONS" in text
    assert "regionMeta(region).label" in text
    assert "${item.row.region || '-'}:${item.row.port || '-'}" not in text


def test_quality_page_caps_sync_cf_sample_count_and_explains_limit():
    text = read(QUALITY_PAGE)
    assert "Math.min(50, Math.max(1, count))" in text
    assert "max={50}" in text
    assert "同步 CF 抽样最多 50 个节点" in text


def test_quality_page_matches_reputation_cache_by_ip_or_exit_ip():
    page = read(QUALITY_PAGE)
    types = read(REPUTATION_TYPES)
    assert "ip?: string" in types
    assert "function reputationExitIp" in page
    assert "row.exit_ip || row.ip" in page
    assert "reputationExitIp(r)" in page


def test_reputation_check_supports_source_filter():
    text = read(REPUTATION_API)
    assert "source = 'all'" in text
    assert "q.set('source', source)" in text


if __name__ == "__main__":
    test_api_client_parses_json_text_fallback_for_accepted_jobs()
    test_quality_page_validates_job_id_and_renders_job_panel_path()
    test_quality_page_uses_complete_region_options_and_labels()
    test_quality_page_caps_sync_cf_sample_count_and_explains_limit()
    test_quality_page_matches_reputation_cache_by_ip_or_exit_ip()
    test_reputation_check_supports_source_filter()
