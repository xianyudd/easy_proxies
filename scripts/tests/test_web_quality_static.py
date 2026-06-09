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


def test_quality_page_refreshes_cache_when_background_job_finishes():
    text = read(QUALITY_PAGE)
    assert "terminalSyncedJobId" in text
    assert "isTerminalJob(jobQuery.data)" in text
    assert "setTerminalSyncedJobId(jobId)" in text
    assert "mergeCfRows" in text
    assert "mergeRepRows" in text
    assert "cfCache.refetch()" in text
    assert "repCache.refetch()" in text
    assert "后台任务结果已同步到质量缓存" in text



def test_quality_page_normalizes_non_array_api_rows():
    text = read(QUALITY_PAGE)
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "Array.isArray(rows) ? rows : []" in text
    assert "safeRows<CloudflareResult>(d.data)" in text
    assert "safeRows<CloudflareResult>(cf.data?.data)" in text
    assert "safeRows<ReputationResult>(rep.data?.data)" in text
    assert "safeRows<QualityJobResult>(jobResults.data?.data)" in text


def test_quality_page_normalizes_active_rows_before_filter_map_spread():
    text = read(QUALITY_PAGE)
    assert "const activeCfRows = safeRows<CloudflareResult>(jobId ? jobCfRows : cfRows)" in text
    assert "const activeRepRows = safeRows<ReputationResult>(jobId ? jobRepRows : repRows)" in text
    assert "activeRepRows.flatMap" in text
    assert "activeRepRows.filter" in text
    assert "activeCfRows.filter" in text


def test_quality_page_row_key_has_stable_fallbacks_for_partial_rows():
    text = read(QUALITY_PAGE)
    assert "target_index?: number" in text
    assert "node_name?: string" in text
    assert "name?: string" in text
    assert "row.node_tag || String(row.port || '')" not in text


def test_quality_page_job_metadata_is_joined_by_stable_row_key():
    text = read(QUALITY_PAGE)
    assert "jobMetaByKey" in text
    assert "new Map(jobRows.map" in text
    assert "jobMetaByKey.get(rowKey(r))" in text
    assert "jobRows[idx]" not in text


def test_quality_page_table_row_key_does_not_depend_on_array_index():
    text = read(QUALITY_PAGE)
    assert "key: rowKey(r)" in text
    assert "key: `${r.node_tag || r.node_name || r.port || 'row'}-${idx}`" not in text


def test_quality_charts_defend_non_array_rows_props():
    text = read(ROOT / "web" / "src" / "components" / "charts" / "QualityCharts.tsx")
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "{ rows }: { rows: unknown }" in text
    assert text.count("const chartRows = safeRows") >= 3
    assert "chartRows.filter" in text
    assert "[...chartRows]" in text

def test_quality_page_sample_and_cache_refresh_exit_job_result_mode():
    text = read(QUALITY_PAGE)
    assert "setJobId('')" in text
    assert "setTerminalSyncedJobId('')" in text
    assert "setResultPage(1)" in text
    assert "CF 检测完成" in text
    assert "缓存结果已加载" in text


def test_quality_page_does_not_fallback_to_total_count_for_empty_source():
    text = read(QUALITY_PAGE)
    assert "const pipelineCount = source === 'all' ? Math.max(sourceCount, 500) : sourceCount" in text
    assert "const hasPipelineTargets = pipelineCount > 0" in text
    assert "!hasPipelineTargets" in text
    assert "Pipeline 无可扫描节点" in text
    assert "count: pipelineCount" in text
    assert "sourceCount || nodesSummary.data?.total_nodes" not in text


if __name__ == "__main__":
    test_api_client_parses_json_text_fallback_for_accepted_jobs()
    test_quality_page_validates_job_id_and_renders_job_panel_path()
    test_quality_page_uses_complete_region_options_and_labels()
    test_quality_page_caps_sync_cf_sample_count_and_explains_limit()
    test_quality_page_matches_reputation_cache_by_ip_or_exit_ip()
    test_reputation_check_supports_source_filter()
    test_quality_page_refreshes_cache_when_background_job_finishes()
    test_quality_page_normalizes_non_array_api_rows()
    test_quality_page_normalizes_active_rows_before_filter_map_spread()
    test_quality_page_row_key_has_stable_fallbacks_for_partial_rows()
    test_quality_page_job_metadata_is_joined_by_stable_row_key()
    test_quality_page_table_row_key_does_not_depend_on_array_index()
    test_quality_charts_defend_non_array_rows_props()
    test_quality_page_sample_and_cache_refresh_exit_job_result_mode()
    test_quality_page_does_not_fallback_to_total_count_for_empty_source()
