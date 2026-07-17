#!/usr/bin/env bash
# Long-running isolated free_proxy_promote soak test.
# Does NOT touch easy_proxies_main or its data directory.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$$}"
ARTIFACT_DIR="${ARTIFACT_DIR:-/tmp/easy_proxies-promote-soak-artifacts-$RUN_ID}"
DATA_DIR="${DATA_DIR:-/tmp/easy_proxies-promote-soak-$RUN_ID}"
IMAGE="${IMAGE:-localhost/easy-proxies:promote-test}"
BASE_IMAGE="${BASE_IMAGE:-localhost/easy-proxies:main}"
CONTAINER="${CONTAINER:-easy_proxies_promote_soak}"
PROD_CONTAINER="${PROD_CONTAINER:-easy_proxies_main}"

# Duration / sampling
DURATION_SEC="${DURATION_SEC:-1800}"   # default 30m
SAMPLE_SEC="${SAMPLE_SEC:-30}"
BOOT_WAIT_SEC="${BOOT_WAIT_SEC:-90}"

# Preferred ports (auto-fallback if busy). Avoid production ranges.
PREFERRED_POOL_PORT="${PREFERRED_POOL_PORT:-12323}"
PREFERRED_MULTI_BASE="${PREFERRED_MULTI_BASE:-34000}"
PREFERRED_MONITOR_PORT="${PREFERRED_MONITOR_PORT:-19091}"
PREFERRED_CLASH_PORT="${PREFERRED_CLASH_PORT:-19092}"
PREFERRED_MOCK_PORT="${PREFERRED_MOCK_PORT:-18080}"
PREFERRED_MOCK_BAD_PORT="${PREFERRED_MOCK_BAD_PORT:-18081}"

PROD_PORTS=(2323 9091 9092 24000)

MANIFEST="$ARTIFACT_DIR/ARTIFACTS.manifest"
LOG_FILE="$ARTIFACT_DIR/soak.log"
SAMPLE_CSV="$ARTIFACT_DIR/samples.csv"
MOCK_LOG="$ARTIFACT_DIR/mock-proxy.log"
MOCK_PID_FILE="$ARTIFACT_DIR/mock-proxy.pid"
REPORT="$ARTIFACT_DIR/SOAK_REPORT.md"
PORTS_FILE="$ARTIFACT_DIR/ports.env"

mkdir -p "$ARTIFACT_DIR" "$DATA_DIR" "$DATA_DIR/.cache"
chmod 777 "$DATA_DIR" "$DATA_DIR/.cache" 2>/dev/null || true
: >"$MANIFEST"
record() { echo "$1" >>"$MANIFEST"; }
for p in "$ARTIFACT_DIR" "$DATA_DIR" "$MANIFEST" "$LOG_FILE" "$SAMPLE_CSV" "$MOCK_LOG" "$MOCK_PID_FILE" "$REPORT" "$PORTS_FILE"; do
  record "$p"
done

log() { echo "[$(date -Is)] $*" | tee -a "$LOG_FILE"; }

cleanup() {
  local code=$?
  set +e
  log "[cleanup] exit=$code"
  if [[ -f "$MOCK_PID_FILE" ]]; then
    kill "$(cat "$MOCK_PID_FILE")" 2>/dev/null || true
    sleep 0.2
    rm -f "$MOCK_PID_FILE"
  fi
  if podman inspect "$CONTAINER" >/dev/null 2>&1; then
    podman logs "$CONTAINER" >"$ARTIFACT_DIR/container.full.log" 2>&1 || true
    podman stats --no-stream "$CONTAINER" >"$ARTIFACT_DIR/final-stats.txt" 2>&1 || true
  fi
  podman rm -f "$CONTAINER" >/dev/null 2>&1 || true
  if [[ "${CLEAN_IMAGE:-0}" == "1" ]]; then
    podman rmi -f "$IMAGE" >/dev/null 2>&1 || true
  fi
  # generate report if samples exist
  if [[ -f "$SAMPLE_CSV" ]]; then
    python3 - "$SAMPLE_CSV" "$REPORT" "$LOG_FILE" "$DURATION_SEC" <<'PY' || true
import csv, pathlib, sys, statistics
from collections import Counter
csv_path, report_path, log_path, duration = sys.argv[1:5]
rows=[]
with open(csv_path, newline="") as f:
    r=csv.DictReader(f)
    for row in r:
        rows.append(row)
lines=[
  "# free_proxy_promote soak report",
  "",
  f"- duration_sec: {duration}",
  f"- samples: {len(rows)}",
]
if rows:
    def nums(key):
        out=[]
        for row in rows:
            try: out.append(float(row[key]))
            except Exception: pass
        return out
    mem=nums("mem_mb"); cpu=nums("cpu_pct"); prom=nums("promoted_count"); avail=nums("available_count")
    def span(xs):
        if not xs: return "n/a"
        return f"min={min(xs):.2f} max={max(xs):.2f} avg={statistics.mean(xs):.2f} last={xs[-1]:.2f}"
    lines += [
      f"- promoted_count: {span(prom)}",
      f"- available_count: {span(avail)}",
      f"- mem_mb: {span(mem)}",
      f"- cpu_pct: {span(cpu)}",
      f"- prod_running_always: {all(r.get('prod_running')=='1' for r in rows)}",
      f"- prod_port_ok_always: {all(r.get('prod_ports_ok')=='1' for r in rows)}",
      f"- test_container_running_always: {all(r.get('ctr_running')=='1' for r in rows)}",
      f"- probe_ok_ratio: {sum(1 for r in rows if r.get('pool_probe_ok')=='1')}/{len(rows)}",
      f"- api_ok_ratio: {sum(1 for r in rows if r.get('api_ok')=='1')}/{len(rows)}",
      f"- promote_log_events_seen_last: {rows[-1].get('promote_events','')}",
      f"- demote_log_events_seen_last: {rows[-1].get('demote_events','')}",
      "",
      "## Pass criteria",
    ]
    ok = (
      all(r.get('prod_running')=='1' for r in rows)
      and all(r.get('prod_ports_ok')=='1' for r in rows)
      and all(r.get('ctr_running')=='1' for r in rows)
      and any(float(r.get('promoted_count') or 0) >= 1 for r in rows)
      and sum(1 for r in rows if r.get('pool_probe_ok')=='1') >= max(1, len(rows)//2)
    )
    lines.append(f"- overall: {'PASS' if ok else 'FAIL'}")
else:
    lines.append("- overall: FAIL (no samples)")
pathlib.Path(report_path).write_text("\n".join(lines)+"\n")
print("\n".join(lines))
PY
  fi
  if [[ "${KEEP_ARTIFACTS:-1}" != "1" ]]; then
    rm -rf "$DATA_DIR" "$ARTIFACT_DIR"
  else
    log "[cleanup] kept artifacts: $ARTIFACT_DIR"
  fi
  if podman inspect -f '{{.State.Running}}' "$PROD_CONTAINER" 2>/dev/null | grep -qx true; then
    log "[cleanup] production $PROD_CONTAINER still running: OK"
  else
    log "[cleanup] WARNING: production $PROD_CONTAINER not running"
  fi
  exit "$code"
}
trap cleanup EXIT INT TERM

log "=== free_proxy_promote SOAK ==="
log "ROOT=$ROOT RUN_ID=$RUN_ID DURATION_SEC=$DURATION_SEC SAMPLE_SEC=$SAMPLE_SEC"
log "ARTIFACT_DIR=$ARTIFACT_DIR DATA_DIR=$DATA_DIR"

# Pick free ports
eval "$(
  PREFERRED_POOL_PORT="$PREFERRED_POOL_PORT" \
  PREFERRED_MULTI_BASE="$PREFERRED_MULTI_BASE" \
  PREFERRED_MONITOR_PORT="$PREFERRED_MONITOR_PORT" \
  PREFERRED_CLASH_PORT="$PREFERRED_CLASH_PORT" \
  PREFERRED_MOCK_PORT="$PREFERRED_MOCK_PORT" \
  PREFERRED_MOCK_BAD_PORT="$PREFERRED_MOCK_BAD_PORT" \
  python3 - <<'PY'
import os, socket

BLOCKED = {2323, 9091, 9092, *range(24000, 24150), *range(13000, 13020)}

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
    raise SystemExit(f"no free port in {start}-{end}")

pool = pick(int(os.environ["PREFERRED_POOL_PORT"]), 12000, 12999)
multi = pick(int(os.environ["PREFERRED_MULTI_BASE"]), 34000, 34900)
for attempt in range(80):
    base = multi + attempt
    if all(can_bind(base + i) for i in range(8)):
        multi = base
        break
else:
    raise SystemExit("no free multi-port base")
monitor = pick(int(os.environ["PREFERRED_MONITOR_PORT"]), 19000, 19199)
clash = pick(int(os.environ["PREFERRED_CLASH_PORT"]), 19200, 19399)
mock = pick(int(os.environ["PREFERRED_MOCK_PORT"]), 18080, 18200)
mock_bad = pick(int(os.environ["PREFERRED_MOCK_BAD_PORT"]), 18201, 18300)
chosen = [pool, multi, monitor, clash, mock, mock_bad]
if len(set(chosen)) != len(chosen):
    raise SystemExit(f"port collision: {chosen}")
print(f"POOL_PORT={pool}")
print(f"MULTI_BASE={multi}")
print(f"MONITOR_PORT={monitor}")
print(f"CLASH_PORT={clash}")
print(f"MOCK_PORT={mock}")
print(f"MOCK_BAD_PORT={mock_bad}")
PY
)"
export POOL_PORT MULTI_BASE MONITOR_PORT CLASH_PORT MOCK_PORT MOCK_BAD_PORT
cat >"$PORTS_FILE" <<EOF
POOL_PORT=$POOL_PORT
MULTI_BASE=$MULTI_BASE
MONITOR_PORT=$MONITOR_PORT
CLASH_PORT=$CLASH_PORT
MOCK_PORT=$MOCK_PORT
MOCK_BAD_PORT=$MOCK_BAD_PORT
EOF
log "[ports] pool=$POOL_PORT multi=$MULTI_BASE+ monitor=$MONITOR_PORT mock=$MOCK_PORT mock_bad=$MOCK_BAD_PORT"

PROD_STATUS="$(podman inspect -f '{{.State.Status}}' "$PROD_CONTAINER" 2>/dev/null || echo missing)"
log "[prod] $PROD_CONTAINER status=$PROD_STATUS"
if [[ "$PROD_STATUS" != "running" ]]; then
  log "ERROR: production not running; abort"
  exit 3
fi

# Dual mock proxies: good (forward) + bad (always 502)
export MOCK_PORT MOCK_BAD_PORT
python3 - <<'PY' >"$MOCK_LOG" 2>&1 &
import http.client, os, select, socket, socketserver, threading
from http.server import BaseHTTPRequestHandler
from urllib.parse import urlparse

GOOD = int(os.environ["MOCK_PORT"])
BAD = int(os.environ["MOCK_BAD_PORT"])

class GoodHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, fmt, *args):
        print("[mock-good]", self.address_string(), fmt % args, flush=True)
    def do_CONNECT(self):
        host, _, port = self.path.partition(":")
        port = int(port or "443")
        try:
            up = socket.create_connection((host, port), timeout=8)
        except Exception as e:
            self.send_error(502, str(e)); return
        self.send_response(200, "Connection Established"); self.end_headers()
        self.connection.setblocking(False); up.setblocking(False)
        socks = [self.connection, up]
        try:
            while True:
                r, _, x = select.select(socks, [], socks, 30)
                if x or not r: break
                for s in r:
                    o = up if s is self.connection else self.connection
                    data = s.recv(65536)
                    if not data: return
                    o.sendall(data)
        finally:
            up.close()
    def do_GET(self): self._proxy()
    def do_HEAD(self): self._proxy()
    def do_POST(self): self._proxy()
    def _proxy(self):
        url = self.path
        if url.startswith("http://") or url.startswith("https://"):
            parsed = urlparse(url)
            host = parsed.hostname
            port = parsed.port or (443 if parsed.scheme == "https" else 80)
            path = parsed.path or "/"
            if parsed.query: path += "?" + parsed.query
            scheme = parsed.scheme
        else:
            host = self.headers.get("Host", "127.0.0.1")
            if ":" in host:
                host, ps = host.rsplit(":", 1); port = int(ps)
            else:
                port = 80
            path = url; scheme = "http"
        try:
            conn = http.client.HTTPSConnection(host, port, timeout=8) if scheme == "https" else http.client.HTTPConnection(host, port, timeout=8)
            bl = int(self.headers.get("Content-Length", "0") or 0)
            body = self.rfile.read(bl) if bl else None
            headers = {k: v for k, v in self.headers.items() if k.lower() not in {"proxy-connection", "connection", "content-length"}}
            conn.request(self.command, path, body=body, headers=headers)
            resp = conn.getresponse(); data = resp.read()
            self.send_response(resp.status, resp.reason)
            for k, v in resp.getheaders():
                if k.lower() in {"transfer-encoding", "connection"}: continue
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(data))); self.end_headers()
            if self.command != "HEAD": self.wfile.write(data)
            conn.close()
        except Exception as e:
            self.send_error(502, str(e))

class BadHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, fmt, *args):
        print("[mock-bad]", self.address_string(), fmt % args, flush=True)
    def do_CONNECT(self):
        self.send_error(502, "intentionally bad")
    def do_GET(self):
        self.send_error(502, "intentionally bad")
    def do_HEAD(self):
        self.send_error(502, "intentionally bad")
    def do_POST(self):
        self.send_error(502, "intentionally bad")

class T(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True
    daemon_threads = True

good = T(("127.0.0.1", GOOD), GoodHandler)
bad = T(("127.0.0.1", BAD), BadHandler)
print(f"mock good on 127.0.0.1:{GOOD}", flush=True)
print(f"mock bad on 127.0.0.1:{BAD}", flush=True)
threading.Thread(target=good.serve_forever, daemon=True).start()
bad.serve_forever()
PY
echo $! >"$MOCK_PID_FILE"
sleep 0.6
if ! kill -0 "$(cat "$MOCK_PID_FILE")" 2>/dev/null; then
  log "mock failed"; cat "$MOCK_LOG"; exit 4
fi
log "[mock] good=$MOCK_PORT bad=$MOCK_BAD_PORT pid=$(cat "$MOCK_PID_FILE")"

cat >"$DATA_DIR/free-proxies.txt" <<FREE
http://127.0.0.1:${MOCK_PORT}
http://127.0.0.1:${MOCK_BAD_PORT}
http://127.0.0.1:1
FREE
record "$DATA_DIR/free-proxies.txt"
cat >"$DATA_DIR/nodes.txt" <<'NODES'
# soak nodes file
NODES
record "$DATA_DIR/nodes.txt"

# Promote interval 1m so soak sees multiple cycles; demote on fail optional.
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
  blacklist_duration: 2m
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
free_proxy_max_nodes: 10
free_proxy_filter:
  enabled: false
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: soak-local
    file: free-proxies.txt
    format: txt
    enabled: true
    max_nodes: 10
free_proxy_promote:
  enabled: true
  interval: 1m
  batch_size: 1
  max_promoted: 5
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

# Build binary + thin image if needed
log "[build] compile binary"
BIN_OUT="$ARTIFACT_DIR/easy_proxies"
BUILD_TAGS="${BUILD_TAGS:-with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api}"
(
  cd "$ROOT"
  CGO_ENABLED=0 go build -tags "$BUILD_TAGS" -o "$BIN_OUT" ./cmd/easy_proxies
) >>"$LOG_FILE" 2>&1
record "$BIN_OUT"
if ! podman image exists "$BASE_IMAGE"; then
  log "ERROR: base image $BASE_IMAGE missing"; exit 9
fi
CONTAINERFILE="$ARTIFACT_DIR/Containerfile.promote-soak"
cat >"$CONTAINERFILE" <<EOF
FROM $BASE_IMAGE
COPY easy_proxies /usr/local/bin/easy_proxies
EOF
record "$CONTAINERFILE"
log "[build] thin image $IMAGE"
podman build -t "$IMAGE" -f "$CONTAINERFILE" "$ARTIFACT_DIR" >>"$LOG_FILE" 2>&1

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

log "[boot] waiting up to ${BOOT_WAIT_SEC}s for first promote"
BOOT_DEADLINE=$(( $(date +%s) + BOOT_WAIT_SEC ))
PROMOTED=0
while (( $(date +%s) < BOOT_DEADLINE )); do
  if podman logs "$CONTAINER" 2>&1 | python3 -c 'import sys; t=sys.stdin.read(); sys.exit(0 if "created promoted node" in t or ("promoted node " in t and "port=" in t) else 1)'; then
    PROMOTED=1; log "[boot] promote observed"; break
  fi
  if curl -fsS "http://127.0.0.1:${MONITOR_PORT}/api/nodes" 2>/dev/null | python3 -c 'import sys,json
d=json.load(sys.stdin)
sys.exit(0 if any(str(n.get("name","")).startswith("free-promoted-") for n in d.get("nodes",[])) else 1)'; then
    PROMOTED=1; log "[boot] API promote observed"; break
  fi
  if ! podman inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true; then
    log "[boot] container exited early"; podman logs "$CONTAINER" 2>&1 | tail -80 | tee -a "$LOG_FILE"; exit 5
  fi
  sleep 5
done
if [[ "$PROMOTED" != "1" ]]; then
  log "ERROR: no promote during boot window"; podman logs "$CONTAINER" 2>&1 | tail -100 | tee -a "$LOG_FILE"; exit 7
fi

echo "ts,elapsed_s,ctr_running,prod_running,prod_ports_ok,api_ok,promoted_count,available_count,total_nodes,multi_open,pool_probe_ok,mem_mb,cpu_pct,promote_events,demote_events,nodes_txt_has_promoted" >"$SAMPLE_CSV"

START_TS=$(date +%s)
END_TS=$(( START_TS + DURATION_SEC ))
SAMPLE_N=0
log "[soak] start duration=${DURATION_SEC}s sample every ${SAMPLE_SEC}s"

while (( $(date +%s) < END_TS )); do
  SAMPLE_N=$((SAMPLE_N+1))
  NOW=$(date +%s)
  ELAPSED=$((NOW-START_TS))

  python3 - "$CONTAINER" "$PROD_CONTAINER" "$MONITOR_PORT" "$POOL_PORT" "$MULTI_BASE" "$SAMPLE_CSV" "$ELAPSED" "$DATA_DIR/nodes.txt" <<'PY' >>"$LOG_FILE"
import csv, json, pathlib, socket, subprocess, sys, time, urllib.request

ctr, prod, mon, pool, multi, csv_path, elapsed, nodes_txt = sys.argv[1:9]
mon=int(mon); pool=int(pool); multi=int(multi); elapsed=int(elapsed)

def running(name):
    try:
        out=subprocess.check_output(["podman","inspect","-f","{{.State.Running}}",name], text=True, stderr=subprocess.DEVNULL).strip()
        return out=="true"
    except Exception:
        return False

def port_open(port):
    s=socket.socket(); s.settimeout(0.5)
    try:
        return s.connect_ex(("127.0.0.1", port))==0
    finally:
        s.close()

def probe_pool():
    proxy=f"http://testuser:testpass@127.0.0.1:{pool}"
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({"http":proxy,"https":proxy}))
    try:
        with opener.open("http://cp.cloudflare.com/generate_204", timeout=10) as r:
            return r.status in (200,204)
    except Exception:
        return False

def stats(name):
    try:
        out=subprocess.check_output(["podman","stats","--no-stream","--format","{{.MemUsage}}\t{{.CPUPerc}}",name], text=True, stderr=subprocess.DEVNULL).strip()
        # e.g. 6.345MB / 16.76GB \t 0.22%
        mem_s, cpu_s = out.split("\t")
        mem=mem_s.split("/")[0].strip()
        # parse MB/GB/kB
        u=mem[-2:].upper() if mem[-1:].upper()=="B" else mem[-1:].upper()
        # better: endswith
        val=0.0
        m=mem.upper().replace(" ","")
        if m.endswith("GIB") or m.endswith("GB"):
            val=float(m.rstrip("GIB").rstrip("GB"))*1024
        elif m.endswith("MIB") or m.endswith("MB"):
            val=float(m.rstrip("MIB").rstrip("MB"))
        elif m.endswith("KIB") or m.endswith("KB"):
            val=float(m.rstrip("KIB").rstrip("KB"))/1024
        elif m.endswith("B"):
            val=float(m[:-1])/1024/1024
        cpu=float(cpu_s.strip().rstrip("%") or 0)
        return val, cpu
    except Exception:
        return 0.0, 0.0

def logs_count(name, needles):
    try:
        out=subprocess.check_output(["podman","logs",name], text=True, stderr=subprocess.STDOUT)
    except Exception:
        return 0
    return sum(out.count(n) for n in needles)

ctr_ok = 1 if running(ctr) else 0
prod_ok = 1 if running(prod) else 0
prod_ports_ok = 1 if all(port_open(p) for p in (2323,9091,9092,24000)) else 0
api_ok=0; promoted=0; available=0; total=0
try:
    with urllib.request.urlopen(f"http://127.0.0.1:{mon}/api/nodes", timeout=5) as r:
        data=json.load(r)
    api_ok=1
    nodes=data.get("nodes") or []
    total=len(nodes)
    for n in nodes:
        if str(n.get("name","")).startswith("free-promoted-") or n.get("source")=="nodes_file":
            promoted += 1
        if n.get("available"):
            available += 1
except Exception:
    pass
multi_open = 1 if any(port_open(multi+i) for i in range(0,5)) else 0
pool_ok = 1 if probe_pool() else 0
mem, cpu = stats(ctr)
promote_events = logs_count(ctr, ["created promoted node", "promoting "])
demote_events = logs_count(ctr, ["demoted ", "quality fail "])
nodes_has = 1 if "free-promoted-" in pathlib.Path(nodes_txt).read_text(errors="ignore") else 0

row=[
    time.strftime("%Y-%m-%dT%H:%M:%S"),
    elapsed, ctr_ok, prod_ok, prod_ports_ok, api_ok,
    promoted, available, total, multi_open, pool_ok,
    f"{mem:.2f}", f"{cpu:.2f}", promote_events, demote_events, nodes_has
]
with open(csv_path, "a", newline="") as f:
    csv.writer(f).writerow(row)
print(
    f"[sample] t={elapsed}s ctr={ctr_ok} prod={prod_ok}/{prod_ports_ok} "
    f"api={api_ok} prom={promoted} avail={available}/{total} multi={multi_open} "
    f"probe={pool_ok} mem={mem:.1f}MB cpu={cpu:.2f}% events={promote_events}/{demote_events}"
)
# hard fail if production impacted
if prod_ok != 1 or prod_ports_ok != 1:
    raise SystemExit(66)
if ctr_ok != 1:
    raise SystemExit(67)
PY
  rc=$?
  if [[ $rc -eq 66 ]]; then
    log "ERROR: production isolation broken"; exit 6
  fi
  if [[ $rc -eq 67 ]]; then
    log "ERROR: soak container died"; podman logs "$CONTAINER" 2>&1 | tail -100 | tee -a "$LOG_FILE"; exit 5
  fi
  if [[ $rc -ne 0 ]]; then
    log "WARN: sample script rc=$rc (continuing)"
  fi

  # sleep remaining sample interval
  sleep "$SAMPLE_SEC"
done

log "[soak] finished samples=$SAMPLE_N"
# final functional assertions
python3 - "$SAMPLE_CSV" <<'PY'
import csv, sys
rows=list(csv.DictReader(open(sys.argv[1])))
assert rows, "no samples"
assert all(r["prod_running"]=="1" for r in rows), "prod not always running"
assert all(r["prod_ports_ok"]=="1" for r in rows), "prod ports degraded"
assert all(r["ctr_running"]=="1" for r in rows), "ctr not always running"
assert any(float(r["promoted_count"])>=1 for r in rows), "never promoted"
probe_ok=sum(1 for r in rows if r["pool_probe_ok"]=="1")
assert probe_ok >= max(1, len(rows)//2), f"probe ok too low: {probe_ok}/{len(rows)}"
print(f"assertions_ok samples={len(rows)} probe_ok={probe_ok}")
PY

log "=== SOAK PASSED ==="
