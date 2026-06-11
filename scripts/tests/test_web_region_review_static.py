from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
PAGE = ROOT / "web" / "src" / "pages" / "RegionReviewPage.tsx"
API = ROOT / "web" / "src" / "api" / "nodes.ts"
TYPES = ROOT / "web" / "src" / "types" / "node.ts"
ISO = ROOT / "web" / "src" / "data" / "iso3166.ts"
GO_ISO = ROOT / "internal" / "geoip" / "iso3166_data.go"
REGION_TS = ROOT / "web" / "src" / "components" / "charts" / "region.ts"


def read_page() -> str:
    return PAGE.read_text()


def test_region_review_uses_other_queue_but_manual_choices_are_concrete_only():
    text = read_page()
    assert "region: 'other'" in text
    assert "待确认节点" in text
    assert "确认地区" in text
    assert "value: 'other'" not in text
    assert "MANUAL_REGION_OPTIONS" in text
    iso = ISO.read_text()
    for code in ("us", "jp", "hk", "sg", "tw", "kr", "in", "ae", "ch", "au", "de", "gb", "ca", "fr", "vn", "ru", "ua", "tr", "ng"):
        assert f'"code": "{code}"' in iso


def test_region_review_saves_override_and_exposes_reload_flow():
    text = read_page()
    api = API.read_text()
    assert "confirmNodeRegion(tag: string, region: string)" in api
    assert "region })" in api
    assert "ConfirmNodeRegionResponse" in TYPES.read_text()
    assert "reloadCore" in text
    assert "重载入池" in text
    assert "needReload" in text


def test_frontend_and_backend_iso3166_region_codes_stay_in_sync():
    go_codes = set(re.findall(r'\{Code: "([a-z]{2})"', GO_ISO.read_text()))
    ts_codes = set(re.findall(r'"code": "([a-z]{2})"', ISO.read_text()))
    assert go_codes, "backend ISO-3166 country table should not be empty"
    assert ts_codes, "frontend ISO-3166 country table should not be empty"
    assert go_codes == ts_codes
    assert "other" not in go_codes
    assert "all" not in go_codes


def test_manual_region_options_are_all_concrete_iso_countries():
    region_ts = REGION_TS.read_text()
    manual_line = next(line for line in region_ts.splitlines() if line.startswith("export const MANUAL_REGION_OPTIONS"))
    assert "ISO3166_COUNTRIES.map" in manual_line
    assert "other" not in manual_line
    assert "all" not in manual_line
    assert "{ value: 'all'" in region_ts
    assert "{ value: 'other'" in region_ts


if __name__ == "__main__":
    test_region_review_uses_other_queue_but_manual_choices_are_concrete_only()
    test_region_review_saves_override_and_exposes_reload_flow()
    test_frontend_and_backend_iso3166_region_codes_stay_in_sync()
    test_manual_region_options_are_all_concrete_iso_countries()
