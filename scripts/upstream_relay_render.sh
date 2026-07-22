#!/usr/bin/env bash
# Render sing-box upstream relay config from an EP node tag (or raw vmess URI).
# Usage:
#   scripts/upstream_relay_render.sh <tag>
#   scripts/upstream_relay_render.sh --uri 'vmess://uuid@host:port?...'
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA="${UPSTREAM_DATA:-$HOME/.local/share/easy_proxies-upstream}"
LISTEN_HOST="${LISTEN_HOST:-127.0.0.1}"
LISTEN_PORT="${LISTEN_PORT:-17890}"
EP_API="${EP_API:-http://127.0.0.1:9091}"
PASS_FILE="${PASS_FILE:-/tmp/easy_proxies_admin_password.txt}"

mkdir -p "$DATA"

auth() {
  local pass
  if [[ -f "$PASS_FILE" ]]; then
    pass="$(cat "$PASS_FILE")"
  else
    pass="${EP_ADMIN_PASSWORD:?set EP_ADMIN_PASSWORD or $PASS_FILE}"
  fi
  curl -fsS -X POST "$EP_API/api/auth" -H 'Content-Type: application/json' \
    -d "{\"password\":\"$pass\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])'
}

fetch_node_by_tag() {
  local token="$1" tag="$2" page=1
  while (( page <= 30 )); do
    local json
    json="$(curl -fsS -H "Authorization: Bearer $token" \
      "$EP_API/api/nodes?availability=all&page=$page&page_size=100")"
    python3 - "$tag" <<'PY' <<<"$json"
import json,sys
tag=sys.argv[1]
d=json.load(sys.stdin)
for n in d.get("nodes") or []:
    if n.get("tag")==tag and n.get("uri"):
        print(json.dumps(n, ensure_ascii=False))
        raise SystemExit(0)
if not d.get("has_next"):
    raise SystemExit(1)
raise SystemExit(2)
PY
    local rc=$?
    if [[ $rc -eq 0 ]]; then return 0; fi
    if [[ $rc -eq 1 ]]; then return 1; fi
    page=$((page+1))
  done
  return 1
}

render() {
  local uri="$1" meta_json="${2:-{}}"
  python3 - "$uri" "$LISTEN_HOST" "$LISTEN_PORT" "$DATA" "$meta_json" <<'PY'
import json, sys
from urllib.parse import urlparse, parse_qs, unquote
uri, host, port, data_dir, meta_raw = sys.argv[1:6]
meta = json.loads(meta_raw) if meta_raw else {}

def parse_vmess(uri: str) -> dict:
    assert uri.startswith("vmess://"), "only vmess share links supported in this helper"
    rest = uri[8:]
    # human form: uuid@host:port?query#name
    if "@" in rest.split("?", 1)[0]:
        main, _, frag = rest.partition("#")
        userhost, _, query = main.partition("?")
        uuid, _, hostport = userhost.partition("@")
        if ":" in hostport and not hostport.startswith("["):
            h, p = hostport.rsplit(":", 1)
            port_i = int(p)
        else:
            h, port_i = hostport, 443
        qs = parse_qs(query)
        g = lambda k, d="": (qs.get(k) or [d])[0]
        return {
            "add": h, "port": port_i, "id": uuid,
            "aid": int(g("aid", "0") or 0),
            "scy": g("scy") or g("security") or "auto",
            "net": g("type", "tcp"),
            "host": g("host", ""),
            "path": unquote(g("path", "/")),
            "tls": g("tls") or ( "tls" if g("security") == "tls" else ""),
            "sni": g("sni") or g("host") or h,
            "ps": unquote(frag) if frag else "",
        }
    raise SystemExit("unsupported vmess encoding (need uuid@host form)")

vm = parse_vmess(uri)
outbound = {
    "type": "vmess",
    "tag": "node",
    "server": vm["add"],
    "server_port": int(vm["port"]),
    "uuid": vm["id"],
    "security": vm.get("scy") or "auto",
    "alter_id": int(vm.get("aid") or 0),
}
if vm.get("net") == "ws":
    outbound["transport"] = {
        "type": "ws",
        "path": vm.get("path") or "/",
        "headers": {"Host": vm.get("host") or vm["add"]},
    }
if str(vm.get("tls") or "").lower() in ("tls", "true", "1", "reality"):
    outbound["tls"] = {
        "enabled": True,
        "server_name": vm.get("sni") or vm.get("host") or vm["add"],
        "insecure": True,
    }

cfg = {
    "log": {"level": "info", "timestamp": True},
    "inbounds": [{
        "type": "mixed",
        "tag": "in-mixed",
        "listen": host,
        "listen_port": int(port),
    }],
    "outbounds": [outbound, {"type": "direct", "tag": "direct"}],
    "route": {"final": "node"},
}
open(f"{data_dir}/config.json", "w", encoding="utf-8").write(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n")
meta_out = {
    **meta,
    "listen": f"{host}:{port}",
    "upstream_for_ep": f"socks5://{host}:{port}",
    "server": vm["add"],
    "server_port": vm["port"],
    "ps": vm.get("ps"),
}
open(f"{data_dir}/node.json", "w", encoding="utf-8").write(json.dumps(meta_out, ensure_ascii=False, indent=2) + "\n")
print(json.dumps(meta_out, ensure_ascii=False, indent=2))
PY
}

case "${1:-}" in
  --uri)
    render "${2:?uri required}"
    ;;
  ""|-h|--help)
    sed -n '1,12p' "$0"
    exit 0
    ;;
  *)
    token="$(auth)"
    node_json="$(fetch_node_by_tag "$token" "$1" || true)"
    if [[ -z "${node_json:-}" ]]; then
      echo "node tag not found: $1" >&2
      exit 1
    fi
    uri="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["uri"])' <<<"$node_json")"
    meta="$(python3 -c 'import json,sys; n=json.load(sys.stdin); print(json.dumps({k:n.get(k) for k in ("tag","name","region","port","last_latency_ms","source")}))' <<<"$node_json")"
    render "$uri" "$meta"
    ;;
esac

echo "Rendered $DATA/config.json"
if command -v sing-box >/dev/null 2>&1 || [[ -x "$HOME/.local/bin/sing-box" ]]; then
  SB="${SING_BOX:-$HOME/.local/bin/sing-box}"
  "$SB" check -c "$DATA/config.json"
fi
