from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ECHART = ROOT / "web" / "src" / "components" / "charts" / "EChart.tsx"
NODE_CHARTS = ROOT / "web" / "src" / "components" / "charts" / "NodeCharts.tsx"


def test_echart_handles_missing_resize_observer():
    text = ECHART.read_text()
    assert "typeof ResizeObserver !== 'undefined'" in text
    assert "const observer = typeof ResizeObserver" in text
    assert "observer?.observe(ref.current)" in text
    assert "observer?.disconnect()" in text
    assert "window.setTimeout(resize, 0)" in text


def test_node_charts_defend_non_array_nodes_props():
    text = NODE_CHARTS.read_text()
    assert "function safeRows<T>(rows: unknown): T[]" in text
    assert "export function RegionAvailabilityChart({ nodes }: { nodes: unknown })" in text
    assert "const chartNodes = safeRows<NodeSnapshot>(nodes)" in text
    assert "for (const node of chartNodes)" in text
    assert "export function LatencyTopChart({ nodes }: { nodes: unknown })" in text
    assert "export function FailureRankChart({ nodes }: { nodes: unknown })" in text
    assert "nodes.filter" not in text

if __name__ == "__main__":
    test_echart_handles_missing_resize_observer()
    test_node_charts_defend_non_array_nodes_props()
