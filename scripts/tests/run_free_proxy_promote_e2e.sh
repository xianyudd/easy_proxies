#!/usr/bin/env bash
# Isolated free_proxy_promote E2E against a temporary Podman container.
# Does NOT touch easy_proxies_main or its data directory.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-/tmp/easy_proxies-promote-test-artifacts-$$}"
DATA_DIR="${DATA_DIR:-/tmp/easy_proxies-promote-test-$$}"
IMAGE="${IMAGE:-localhost/easy-proxies:promote-test}"
CONTAINER="${CONTAINER:-easy_proxies_promote_test}"
PROD_CONTAINER=easy_proxies_main

# Preferred defaults (overridden if busy). Avoid production range:
# prod pool 2323, multi 24000+, monitor 9091, clash 9092.
PREFERRED_POOL_PORT="${PREFERRED_POOL_PORT:-12323}"
PREFERRED_MULTI_BASE="${PREFERRED_MULTI_BASE:-34000}"
PREFERRED_MONITOR_PORT="${PREFERRED_MONITOR_PORT:-19091}"
PREFERRED_CLASH_PORT="${PREFERRED_CLASH_PORT:-19092}"
PREFERRED_MOCK_PORT="${PREFERRED_MOCK_PORT:-${MOCK_PORT:-18081}}"

MANIFEST="$ARTIFACT_DIR/ARTIFACTS.manifest"
LOG_FILE="$ARTIFACT_DIR/e2e.log"
MOCK_LOG="$ARTIFACT_DIR/mock-proxy.log"
MOCK_PID_FILE="$ARTIFACT_DIR/mock-proxy.pid"
PORTS_FILE="$ARTIFACT_DIR/ports.env"

mkdir -p "$ARTIFACT_DIR" "$DATA_DIR" "$DATA_DIR/.cache"
chmod -R u+rwX "$ARTIFACT_DIR" "$DATA_DIR" 2>/dev/null || true
# volume-mounted config must remain writable by container uid 10001
chmod 777 "$DATA_DIR" "$DATA_DIR/.cache" 2>/dev/null || true
: >"$MANIFEST"
record() { echo "$1" >>"$MANIFEST"; }
record "$ARTIFACT_DIR"
record "$DATA_DIR"
record "$MANIFEST"
record "$LOG_FILE"
record "$MOCK_LOG"
record "$MOCK_PID_FILE"
record "$PORTS_FILE"

cleanup() {
  local code=$?
  set +e
  echo "[cleanup] stopping test resources (exit=$code)" | tee -a "$LOG_FILE"
  if [[ -f "$MOCK_PID_FILE" ]]; then
    kill "$(cat "$MOCK_PID_FILE")" 2>/dev/null || true
    # give socket a moment to release
    sleep 0.2
    rm -f "$MOCK_PID_FILE"
  fi
  podman rm -f "$CONTAINER" >/dev/null 2>&1 || true
  if [[ "${CLEAN_IMAGE:-0}" == "1" ]]; then
    podman rmi -f "$IMAGE" >/dev/null 2>&1 || true
  fi
  if [[ "${KEEP_ARTIFACTS:-0}" != "1" ]]; then
    rm -rf "$DATA_DIR" "$ARTIFACT_DIR"
  fi
  if podman inspect -f '{{.State.Running}}' "$PROD_CONTAINER" 2>/dev/null | grep -qx true; then
    echo "[cleanup] production $PROD_CONTAINER still running: OK"
  else
    echo "[cleanup] WARNING: production $PROD_CONTAINER not running" >&2
  fi
  exit "$code"
}
trap cleanup EXIT INT TERM

echo "=== free_proxy_promote E2E ===" | tee "$LOG_FILE"
echo "ROOT=$ROOT" | tee -a "$LOG_FILE"
date -Is | tee -a "$LOG_FILE"

# Reserve free TCP ports by actually binding (ss/netlink may be unavailable).
# Excludes known production ports.
eval "$(
  PREFERRED_POOL_PORT="$PREFERRED_POOL_PORT" \
  PREFERRED_MULTI_BASE="$PREFERRED_MULTI_BASE" \
  PREFERRED_MONITOR_PORT="$PREFERRED_MONITOR_PORT" \
  PREFERRED_CLASH_PORT="$PREFERRED_CLASH_PORT" \
  PREFERRED_MOCK_PORT="$PREFERRED_MOCK_PORT" \
  python3 - <<'PY'
import os
import socket
import sys

BLOCKED = {
    2323, 9091, 9092,
    *range(24000, 24150),
    *range(13000, 13020),
}

def can_bind(port: int, host: str = "0.0.0.0") -> bool:
    if port in BLOCKED or port < 1024 or port > 65535:
        return False
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        s.bind((host, port))
        return True
    except OSError:
        return False
    finally:
        s.close()

def pick(preferred: int, start: int, end: int) -> int:
    for p in [preferred, *range(start, end + 1)]:
        if can_bind(p):
            return p
    raise SystemExit(f"no free port in {start}-{end} (preferred={preferred})")

pool = pick(int(os.environ["PREFERRED_POOL_PORT"]), 12000, 12999)
multi = pick(int(os.environ["PREFERRED_MULTI_BASE"]), 34000, 34900)
# multi needs a small contiguous block for a few promoted listeners
for attempt in range(50):
    base = multi + attempt
    if all(can_bind(base + i) for i in range(5)):
        multi = base
        break
else:
    raise SystemExit("no free multi-port base with 5 contiguous free ports")
monitor = pick(int(os.environ["PREFERRED_MONITOR_PORT"]), 19000, 19199)
clash = pick(int(os.environ["PREFERRED_CLASH_PORT"]), 19200, 19399)
mock = pick(int(os.environ["PREFERRED_MOCK_PORT"]), 18080, 18200)

# Ensure uniqueness across chosen ports
chosen = [pool, multi, multi + 1, multi + 2, multi + 3, multi + 4, monitor, clash, mock]
if len(set(chosen)) != len(chosen):
    raise SystemExit(f"port collision among chosen: {chosen}")

print(f"POOL_PORT={pool}")
print(f"MULTI_BASE={multi}")
print(f"MONITOR_PORT={monitor}")
print(f"CLASH_PORT={clash}")
print(f"MOCK_PORT={mock}")
PY
)"

export POOL_PORT MULTI_BASE MONITOR_PORT CLASH_PORT MOCK_PORT
cat >"$PORTS_FILE" <<PORTS
POOL_PORT=$POOL_PORT
MULTI_BASE=$MULTI_BASE
MONITOR_PORT=$MONITOR_PORT
CLASH_PORT=$CLASH_PORT
MOCK_PORT=$MOCK_PORT
PORTS
echo "[ports] pool=$POOL_PORT multi=$MULTI_BASE+ monitor=$MONITOR_PORT clash=$CLASH_PORT mock=$MOCK_PORT" | tee -a "$LOG_FILE"

# Production snapshot
PROD_STATUS="$(podman inspect -f '{{.State.Status}}' "$PROD_CONTAINER" 2>/dev/null || echo missing)"
echo "[prod] $PROD_CONTAINER status=$PROD_STATUS" | tee -a "$LOG_FILE"
if [[ "$PROD_STATUS" != "running" ]]; then
  echo "ERROR: production container not running; abort to avoid ambiguity" | tee -a "$LOG_FILE"
  exit 3
fi

# Local mock HTTP forward proxy (enough for generate_204 probes)
# MOCK_PORT must be exported BEFORE starting python (heredoc is quoted).
export MOCK_PORT
python3 - <<'PY' >"$MOCK_LOG" 2>&1 &
import http.client
import os
import select
import socket
import socketserver
from http.server import BaseHTTPRequestHandler
from urllib.parse import urlparse

PORT = int(os.environ.get("MOCK_PORT", "18081"))

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        print("[mock]", self.address_string(), fmt % args, flush=True)

    def do_CONNECT(self):
        host, _, port = self.path.partition(":")
        port = int(port or "443")
        try:
            upstream = socket.create_connection((host, port), timeout=8)
        except Exception as exc:
            self.send_error(502, str(exc))
            return
        self.send_response(200, "Connection Established")
        self.end_headers()
        self.connection.setblocking(False)
        upstream.setblocking(False)
        sockets = [self.connection, upstream]
        try:
            while True:
                r, _, x = select.select(sockets, [], sockets, 30)
                if x or not r:
                    break
                for s in r:
                    other = upstream if s is self.connection else self.connection
                    data = s.recv(65536)
                    if not data:
                        return
                    other.sendall(data)
        finally:
            upstream.close()

    def do_GET(self):
        self._proxy()

    def do_HEAD(self):
        self._proxy()

    def do_POST(self):
        self._proxy()

    def _proxy(self):
        url = self.path
        if url.startswith("http://") or url.startswith("https://"):
            parsed = urlparse(url)
            host = parsed.hostname
            port = parsed.port or (443 if parsed.scheme == "https" else 80)
            path = parsed.path or "/"
            if parsed.query:
                path += "?" + parsed.query
            scheme = parsed.scheme
        else:
            host = self.headers.get("Host", "127.0.0.1")
            if ":" in host:
                host, port_s = host.rsplit(":", 1)
                port = int(port_s)
            else:
                port = 80
            path = url
            scheme = "http"
        try:
            if scheme == "https":
                conn = http.client.HTTPSConnection(host, port, timeout=8)
            else:
                conn = http.client.HTTPConnection(host, port, timeout=8)
            body_len = int(self.headers.get("Content-Length", "0") or 0)
            body = self.rfile.read(body_len) if body_len else None
            headers = {k: v for k, v in self.headers.items() if k.lower() not in {"proxy-connection", "connection", "content-length"}}
            conn.request(self.command, path, body=body, headers=headers)
            resp = conn.getresponse()
            data = resp.read()
            self.send_response(resp.status, resp.reason)
            for k, v in resp.getheaders():
                if k.lower() in {"transfer-encoding", "connection"}:
                    continue
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)
            conn.close()
        except Exception as exc:
            self.send_error(502, str(exc))

class ThreadingTCPServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True
    daemon_threads = True

httpd = ThreadingTCPServer(("127.0.0.1", PORT), Handler)
print(f"mock http proxy on 127.0.0.1:{PORT}", flush=True)
httpd.serve_forever()
PY
echo $! >"$MOCK_PID_FILE"
sleep 0.5
if ! kill -0 "$(cat "$MOCK_PID_FILE")" 2>/dev/null; then
  echo "mock proxy failed to start" | tee -a "$LOG_FILE"
  cat "$MOCK_LOG" | tee -a "$LOG_FILE"
  exit 4
fi
# Confirm mock log shows bound port
if ! python3 -c 'import pathlib,sys; t=pathlib.Path(sys.argv[1]).read_text(errors="ignore"); sys.exit(0 if f"mock http proxy on 127.0.0.1:{sys.argv[2]}" in t else 1)' "$MOCK_LOG" "$MOCK_PORT"; then
  echo "mock proxy did not bind expected port ${MOCK_PORT}" | tee -a "$LOG_FILE"
  cat "$MOCK_LOG" | tee -a "$LOG_FILE"
  exit 4
fi
echo "[mock] http proxy 127.0.0.1:$MOCK_PORT pid=$(cat "$MOCK_PID_FILE")" | tee -a "$LOG_FILE"

# Seed free proxy list + config
cat >"$DATA_DIR/free-proxies.txt" <<FREE
http://127.0.0.1:${MOCK_PORT}
http://127.0.0.1:1
FREE
record "$DATA_DIR/free-proxies.txt"

cat >"$DATA_DIR/nodes.txt" <<'NODES'
# e2e nodes file (promoted nodes will be written here)
NODES
record "$DATA_DIR/nodes.txt"

cat >"$DATA_DIR/config.yaml" <<CFG
mode: hybrid
listener:
  address: 0.0.0.0
  port: ${POOL_PORT}
  username: testuser
  password: testpass
multi_port:
  address: 0.0.0.0
  base_port: ${MULTI_BASE}
  username: mpuser
  password: mppass
pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 5m
management:
  enabled: true
  listen: 0.0.0.0:${MONITOR_PORT}
  clash_api_listen: 127.0.0.1:${CLASH_PORT}
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""
dns:
  server: 1.1.1.1
  port: 53
  strategy: prefer_ipv4
geoip:
  enabled: false
android_proxy:
  enabled: false
subscription_refresh:
  enabled: false
quality_check:
  enabled: false
nodes_file: nodes.txt
free_proxy_max_nodes: 5
free_proxy_filter:
  enabled: false
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: e2e-local
    file: free-proxies.txt
    format: txt
    enabled: true
    max_nodes: 5
free_proxy_promote:
  enabled: true
  interval: 30s
  batch_size: 1
  max_promoted: 3
  max_latency_ms: 5000
  min_success_count: 1
  require_cloudflare: false
  demote_on_fail: true
  name_prefix: "free-promoted-"
log_level: info
log:
  output: stdout
nodes: []
CFG
record "$DATA_DIR/config.yaml"

# Build feature binary on host, then thin-layer onto production runtime image.
# Avoids short-name registry resolution issues (golang:1.24 / debian) during E2E.
echo "[build] compile feature binary" | tee -a "$LOG_FILE"
BUILD_START=$(date +%s)
BIN_OUT="$ARTIFACT_DIR/easy_proxies"
BUILD_TAGS="${BUILD_TAGS:-with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api}"
(
  cd "$ROOT"
  CGO_ENABLED=0 go build -tags "$BUILD_TAGS" -o "$BIN_OUT" ./cmd/easy_proxies
) >>"$LOG_FILE" 2>&1
record "$BIN_OUT"

BASE_IMAGE="${BASE_IMAGE:-localhost/easy-proxies:main}"
if ! podman image exists "$BASE_IMAGE"; then
  echo "ERROR: base image $BASE_IMAGE missing; cannot layer test binary" | tee -a "$LOG_FILE"
  exit 9
fi

CONTAINERFILE="$ARTIFACT_DIR/Containerfile.promote-test"
cat >"$CONTAINERFILE" <<EOF
FROM $BASE_IMAGE
COPY easy_proxies /usr/local/bin/easy_proxies
EOF
record "$CONTAINERFILE"

echo "[build] thin image $IMAGE from $BASE_IMAGE" | tee -a "$LOG_FILE"
podman build   -t "$IMAGE"   -f "$CONTAINERFILE"   "$ARTIFACT_DIR" >>"$LOG_FILE" 2>&1
echo "[build] done in $(( $(date +%s) - BUILD_START ))s" | tee -a "$LOG_FILE"

# Run test container (host network for realistic bind; ports isolated)
podman rm -f "$CONTAINER" >/dev/null 2>&1 || true
podman run -d \
  --name "$CONTAINER" \
  --network host \
  --security-opt label=disable \
  -v "$DATA_DIR:/etc/easy_proxies:Z" \
  -e TZ=Asia/Shanghai \
  "$IMAGE" \
  --config /etc/easy_proxies/config.yaml | tee -a "$LOG_FILE"
record "container:$CONTAINER"

echo "[wait] service boot + first promote cycle (~180s)" | tee -a "$LOG_FILE"
PROMOTED=0
for i in $(seq 1 36); do
  sleep 5
  if podman logs "$CONTAINER" 2>&1 | python3 -c 'import sys; t=sys.stdin.read(); sys.exit(0 if ("created promoted node" in t or ("promoted node " in t and "port=" in t)) else 1)'; then
    PROMOTED=1
    echo "[wait] promote log signal at iteration $i" | tee -a "$LOG_FILE"
    break
  fi
  if curl -fsS "http://127.0.0.1:${MONITOR_PORT}/api/nodes" 2>/dev/null | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin)
except Exception:
 raise SystemExit(1)
sys.exit(0 if any(str(n.get("name","")).startswith("free-promoted-") or n.get("source")=="nodes_file" for n in d.get("nodes",[])) else 1)'; then
    PROMOTED=1
    echo "[wait] API shows promoted/nodes_file node at iteration $i" | tee -a "$LOG_FILE"
    break
  fi
  if ! podman inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true; then
    echo "[error] test container exited early" | tee -a "$LOG_FILE"
    podman logs "$CONTAINER" 2>&1 | tail -80 | tee -a "$LOG_FILE"
    exit 5
  fi
done

echo "[check] container logs (tail)" | tee -a "$LOG_FILE"
podman logs "$CONTAINER" 2>&1 | tail -120 | tee -a "$LOG_FILE"
echo "[check] nodes.txt" | tee -a "$LOG_FILE"
cat "$DATA_DIR/nodes.txt" | tee -a "$LOG_FILE"

API_CODE=$(curl -s -o "$ARTIFACT_DIR/api-nodes.json" -w '%{http_code}' "http://127.0.0.1:${MONITOR_PORT}/api/nodes" || true)
record "$ARTIFACT_DIR/api-nodes.json"
echo "[check] GET /api/nodes => $API_CODE" | tee -a "$LOG_FILE"

python3 - "$ARTIFACT_DIR/api-nodes.json" "$DATA_DIR/nodes.txt" "$MOCK_PORT" <<'PY' | tee -a "$LOG_FILE"
import json, pathlib, sys
api_path, nodes_path, mock_port = sys.argv[1:4]
raw = pathlib.Path(api_path).read_text(errors="ignore") if pathlib.Path(api_path).exists() else "{}"
try:
    data = json.loads(raw or "{}")
except Exception as e:
    print(f"api json parse error: {e}")
    data = {}
nodes = data.get("nodes") or []
print(f"api_nodes_count={len(nodes)} source_stats={data.get('source_stats')}")
promoted = [n for n in nodes if str(n.get("name", "")).startswith("free-promoted-") or n.get("source") == "nodes_file"]
print(f"promoted_or_file_nodes={len(promoted)}")
for n in promoted:
    print(
        f"  name={n.get('name')} port={n.get('port')} source={n.get('source')} "
        f"available={n.get('available')} uri={n.get('uri')}"
    )
nodes_txt = pathlib.Path(nodes_path).read_text(errors="ignore") if pathlib.Path(nodes_path).exists() else ""
print("nodes.txt=" + repr(nodes_txt.strip()))
print("promote_name_present=1" if ("free-promoted-" in nodes_txt or any(str(n.get("name","")).startswith("free-promoted-") for n in nodes)) else "promote_name_present=0")
print(f"promoted_ports={[n.get('port') for n in promoted if n.get('port')]}")
if f"127.0.0.1:{mock_port}" not in nodes_txt:
    print("WARN: mock URI not found in nodes.txt")
PY

python3 - <<PY | tee -a "$LOG_FILE" || true
import socket
ports = [${POOL_PORT}, ${MONITOR_PORT}, ${MULTI_BASE}, ${MULTI_BASE}+1, ${MOCK_PORT}, 2323, 9091]
for p in ports:
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.settimeout(0.5)
    try:
        r = s.connect_ex(("127.0.0.1", p))
        print(f"port {p}: {'open' if r == 0 else f'closed({r})'}")
    finally:
        s.close()
PY

podman stats --no-stream "$CONTAINER" | tee -a "$LOG_FILE" || true

PROD_STATUS2="$(podman inspect -f '{{.State.Status}}' "$PROD_CONTAINER" 2>/dev/null || echo missing)"
echo "[prod] after test: $PROD_STATUS2" | tee -a "$LOG_FILE"
if [[ "$PROD_STATUS2" != "running" ]]; then
  echo "ERROR: production impacted" | tee -a "$LOG_FILE"
  exit 6
fi
if ! python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(1); r=s.connect_ex(("127.0.0.1",9091)); s.close(); sys.exit(0 if r==0 else 1)'; then
  echo "ERROR: production management port 9091 not reachable" | tee -a "$LOG_FILE"
  exit 6
fi

if [[ "$PROMOTED" != "1" ]]; then
  echo "ERROR: promotion did not complete within timeout" | tee -a "$LOG_FILE"
  podman logs "$CONTAINER" 2>&1 | python3 -c 'import sys
for l in sys.stdin:
    low=l.lower()
    if any(k in low for k in ["free-proxy","promote","error","panic"]):
        print(l, end="")
' | tee -a "$LOG_FILE" || true
  exit 7
fi

if ! python3 -c 'import pathlib,sys; t=pathlib.Path(sys.argv[1]).read_text(errors="ignore"); sys.exit(0 if sys.argv[2] in t else 1)' "$DATA_DIR/nodes.txt" "127.0.0.1:${MOCK_PORT}"; then
  echo "ERROR: promoted upstream URI not persisted to nodes.txt" | tee -a "$LOG_FILE"
  exit 8
fi

if ! python3 - "$ARTIFACT_DIR/api-nodes.json" "$DATA_DIR/nodes.txt" <<'PY'
import json, pathlib, sys
api = pathlib.Path(sys.argv[1])
nodes_txt = pathlib.Path(sys.argv[2]).read_text(errors="ignore")
name_in_file = "free-promoted-" in nodes_txt
name_in_api = False
if api.exists():
    try:
        data = json.loads(api.read_text())
        name_in_api = any(str(n.get("name", "")).startswith("free-promoted-") for n in data.get("nodes", []))
    except Exception:
        pass
raise SystemExit(0 if (name_in_file or name_in_api) else 1)
PY
then
  echo "ERROR: free-promoted name not found in API or nodes.txt" | tee -a "$LOG_FILE"
  exit 8
fi

MP_OK=0
for p in "$MULTI_BASE" "$((MULTI_BASE+1))" "$((MULTI_BASE+2))"; do
  if python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(0.5); r=s.connect_ex(("127.0.0.1", int(sys.argv[1]))); s.close(); raise SystemExit(0 if r==0 else 1)' "$p"; then
    MP_OK=1
    echo "[check] multi-port listener open on $p" | tee -a "$LOG_FILE"
    break
  fi
done
if [[ "$MP_OK" != "1" ]]; then
  echo "ERROR: multi-port listener not open on ${MULTI_BASE}+" | tee -a "$LOG_FILE"
  exit 10
fi

POOL_PROBE=$(python3 - <<PY
import urllib.request
proxy = "http://testuser:testpass@127.0.0.1:${POOL_PORT}"
handlers = urllib.request.ProxyHandler({"http": proxy, "https": proxy})
opener = urllib.request.build_opener(handlers)
try:
    with opener.open("http://cp.cloudflare.com/generate_204", timeout=12) as resp:
        print(resp.status)
except Exception as e:
    print(f"ERR:{e}")
PY
)
echo "[check] pool probe via mock => $POOL_PROBE" | tee -a "$LOG_FILE"
if [[ "$POOL_PROBE" != "204" && "$POOL_PROBE" != "200" ]]; then
  echo "WARN: pool probe did not return 204/200 (got $POOL_PROBE)" | tee -a "$LOG_FILE"
fi

echo "=== E2E PASSED ===" | tee -a "$LOG_FILE"
