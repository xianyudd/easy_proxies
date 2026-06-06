# Easy Proxies

[简体中文](README_ZH.md)

> A sing-box based proxy pool manager -- aggregate many upstream proxy nodes into one stable, health-checked, load-balanced local proxy endpoint.

## Features

- **Three runtime modes**: `pool` (single-port load balancing), `multi-port` (one port per node), and `hybrid` (both simultaneously)
- **Wide protocol support**: VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS, SOCKS5, HTTP/HTTPS
- **Automatic health checking** with configurable failure thresholds and blacklist duration, plus manual blacklist/release from the dashboard
- **GeoIP region routing**: classify nodes by country and route traffic through a specific region via a dedicated HTTP proxy endpoint
- **Multiple node sources**: inline config, `nodes.txt` file, or subscription URLs (Base64, plain text, Clash YAML)
- **Subscription auto-refresh with hot-reload**: periodically fetches subscription updates and reloads without restart
- **WebUI dashboard**: real-time node status, traffic charts, diagnostics, log console, and full settings management
- **Management API**: RESTful endpoints for node CRUD, probing, blacklisting, subscription management, and config reload
- **Configurable DNS resolver** with fallback servers and IPv4/IPv6 strategy control
- **Log rotation**: size-based rotation with configurable backup count, age, and compression
- **Multi-platform Docker**: supports amd64 and arm64 with host networking

## Quick Start

### 1. Prepare Configuration

```bash
cp config.example.yaml config.yaml
touch nodes.txt
```

Edit `config.yaml` and add your proxy nodes (inline nodes, `nodes.txt` file, or subscription URLs).

> **Important**: `config.yaml` and `nodes.txt` MUST exist as files before starting the Docker container. If they don't exist, Docker will create them as directories, causing startup failure. Use `start.sh` to avoid this issue.

### 2. Run with Docker (Recommended)

```bash
./start.sh
# or manually:
docker compose up -d
```

### 3. Run from Source

```bash
go run ./cmd/easy_proxies --config config.yaml
```

### 4. Access WebUI

Open `http://localhost:9091` in your browser.

## Configuration

### Runtime Modes

| Mode | Description |
|------|-------------|
| `pool` | Single port proxy pool. All nodes share one port with load balancing |
| `multi-port` | One local port per node for direct access |
| `hybrid` | Both pool + multi-port simultaneously |

### Pool Scheduling

| Algorithm | Description |
|-----------|-------------|
| `sequential` | Round-robin through healthy nodes |
| `random` | Random node selection |
| `balance` | Least-connections balancing |

### Minimal Config Example

```yaml
mode: pool

listener:
  address: 0.0.0.0
  port: 2323
  username: user
  password: pass

pool:
  mode: sequential    # sequential / random / balance
  failure_threshold: 3
  blacklist_duration: 24h

management:
  enabled: true
  listen: 0.0.0.0:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""

dns:
  server: 223.5.5.5
  port: 53
  strategy: prefer_ipv4

nodes_file: nodes.txt
```

### Full Config Reference

See [config.example.yaml](config.example.yaml) for the full documented configuration with all available options.

## GeoIP Region Routing

### Overview

When GeoIP is enabled, Easy Proxies automatically classifies your proxy nodes by geographic region and provides a separate HTTP proxy endpoint that lets you route traffic through nodes in a specific country/region.

### Supported Regions

| Code | Region |
|------|--------|
| `jp` | Japan 🇯🇵 |
| `kr` | South Korea 🇰🇷 |
| `us` | United States 🇺🇸 |
| `hk` | Hong Kong 🇭🇰 |
| `tw` | Taiwan 🇹🇼 |
| `sg` | Singapore 🇸🇬 |
| `in` | India 🇮🇳 |
| `ae` | United Arab Emirates 🇦🇪 |
| `ch` | Switzerland 🇨🇭 |
| `au` | Australia 🇦🇺 |
| `de` | Germany 🇩🇪 |
| `gb` | United Kingdom 🇬🇧 |
| `ca` | Canada 🇨🇦 |
| `other` | Unclassified regions |

### Configuration

```yaml
geoip:
  enabled: true
  database_path: "./GeoLite2-Country.mmdb"
  listen: "0.0.0.0"          # defaults to listener.address if omitted
  port: 1221                  # defaults to listener.port if omitted
  auto_update_enabled: true   # auto-update the GeoIP database
  auto_update_interval: 24h   # check interval
```

The GeoIP router reuses the `listener.username` and `listener.password` for proxy authentication.

Key behaviors:
- The GeoIP database (MaxMind GeoLite2-Country) is **auto-downloaded** on first startup
- Auto-update is enabled by default (checks every 24h) with hot-reload -- no restart needed
- Node region classification happens automatically during startup and on every reload
- Nodes whose IP cannot be resolved or looked up are placed in the `other` category

### How to Use

The GeoIP router is an HTTP proxy that listens on its own port. You select a region by adding a path prefix to your request.

#### HTTP Requests

Format: `http://<geoip_host>:<geoip_port>/<region>/`

```bash
# Route through Japanese nodes
curl -x http://user:pass@localhost:1221/jp/ http://example.com

# Route through US nodes
curl -x http://user:pass@localhost:1221/us/ http://example.com

# Route through Hong Kong nodes
curl -x http://user:pass@localhost:1221/hk/ http://example.com

# Route through Singapore nodes
curl -x http://user:pass@localhost:1221/sg/ http://example.com

# No region prefix = use global pool (all nodes)
curl -x http://user:pass@localhost:1221/ http://example.com
```

#### HTTPS Requests (CONNECT Tunnel)

For HTTPS, the region prefix goes before the target host in the CONNECT request:

```bash
# Route HTTPS through Japanese nodes
https_proxy=http://user:pass@localhost:1221/jp/ curl https://www.google.com

# Route HTTPS through US nodes
https_proxy=http://user:pass@localhost:1221/us/ curl https://www.google.com

# No region prefix = use global pool
https_proxy=http://user:pass@localhost:1221/ curl https://www.google.com
```

#### Using with Applications

**Environment variables:**

```bash
# Use Japanese nodes for all traffic
export http_proxy=http://user:pass@your-server:1221/jp/
export https_proxy=http://user:pass@your-server:1221/jp/

# Use global pool (all nodes)
export http_proxy=http://user:pass@your-server:1221/
export https_proxy=http://user:pass@your-server:1221/
```

**Browser proxy extensions (SwitchyOmega, FoxyProxy, etc.):**

- Protocol: HTTP
- Server: your-server-ip
- Port: 1221
- Username/Password: as configured in `listener`
- For region-specific routing: set the proxy URL path to include the region prefix (e.g., `/jp/`)

**Python requests:**

```python
import requests

proxies = {
    "http": "http://user:pass@your-server:1221/jp/",
    "https": "http://user:pass@your-server:1221/jp/",
}
r = requests.get("http://example.com", proxies=proxies)
```

**Go net/http:**

```go
proxyURL, _ := url.Parse("http://user:pass@your-server:1221/jp/")
client := &http.Client{
    Transport: &http.Transport{
        Proxy: http.ProxyURL(proxyURL),
    },
}
resp, err := client.Get("http://example.com")
```

### How It Works

1. On startup, each node's server IP is resolved and looked up in the MaxMind GeoLite2-Country database
2. Nodes are grouped into per-region pools (`pool-jp`, `pool-kr`, `pool-us`, etc.) with independent health checking
3. The GeoIP router listens on its own port and inspects the request path for a region prefix
4. Matching requests are routed through the corresponding region pool; unmatched requests use the global pool
5. Each region pool uses the same scheduling algorithm configured in the `pool` section
6. DNS lookup results are cached to avoid repeated resolution on reload

## Supported Protocols

| Protocol | URI Schemes | Transport |
|----------|-------------|-----------|
| VLESS | `vless://` | TCP, WS, HTTP/2, gRPC, HTTPUpgrade; TLS/Reality/uTLS |
| VMess | `vmess://` | WS, HTTP/2, gRPC, HTTPUpgrade; TLS/uTLS |
| Trojan | `trojan://` | WS, HTTP/2, gRPC, HTTPUpgrade; TLS/Reality/uTLS |
| Shadowsocks | `ss://` | Direct; SIP002 format |
| Hysteria2 | `hysteria2://`, `hy2://` | QUIC-based |
| TUIC | `tuic://` | QUIC-based |
| AnyTLS | `anytls://` | TLS |
| SOCKS5 | `socks5://`, `socks://` | Direct |
| HTTP | `http://`, `https://` | Direct |

## Node Sources

### Inline Nodes

```yaml
nodes:
  - uri: "vless://uuid@server:443?security=tls&type=ws&path=/path#Name"
```

### Nodes File

```yaml
nodes_file: nodes.txt
```

One proxy URI per line. Lines starting with `#` are comments.

### Subscriptions

```yaml
subscriptions:
  - "https://provider.example/api?token=xxx"

subscription_refresh:
  enabled: true
  interval: 1h
```

Supports Base64, plain text, and Clash YAML formats. When subscriptions are configured, fetched nodes are written to `nodes_file`. Subscription changes trigger automatic hot-reload without restart.

### Free Proxy Sources

```yaml
free_proxy_max_nodes: 500
free_proxy_sources:
  - name: "local-free-list"
    file: free-proxies.txt
    format: txt
    default_scheme: http
    enabled: true
    max_nodes: 300
    max_bytes: 2097152
  - name: "remote-json-list"
    url: "https://example.com/free-proxies.json"
    format: json
    timeout: 15s
    enabled: false
    max_nodes: 300
    max_bytes: 2097152
```

Free proxy sources are loaded after inline/file/subscription nodes, deduplicated by URI, and capped before activation. If any free source is enabled and `free_proxy_max_nodes` is unset, the safe default is `500`; set `-1` only if you explicitly want unlimited activation. They are runtime inputs and are **not** written back to `nodes.txt` or `config.yaml` by the settings save path.

Supported MVP formats:

- `txt`: one proxy per line, accepting either `host:port` (defaults to `http://`, or `default_scheme` when set) or a full `http://`, `https://`, `socks5://`, or `socks://` URI.
- `json`: arrays or wrapped objects (`proxies`, `data`, or `items`) with either `uri` / `url` fields, or `ip` / `host` / `address` + `port` + optional `protocol` / `type`.

Relative `file` paths are resolved relative to `config.yaml`.

## WebUI Dashboard

Access at `http://your-server:9091` (configurable via the `management` section).

Features:

- **Dashboard**: Real-time node status, traffic charts with selectable time windows, region availability, latency monitoring
- **Node Overview**: Full node table with region/status/latency filters, sorting, refresh, and proxy copy actions
- **Node Quality**: Background Cloudflare compatibility and IP reputation jobs for large node lists, cached results, failed-node retry, server-side result pagination, and composite quality ranking
- **Node Config**: Add/edit/delete inline nodes and subscription URLs
- **Diagnostics**: Connectivity testing and node state export
- **Console**: Real-time application logs (last 1000 lines, WebSocket streaming)
- **Settings**: All configuration options editable from the browser, changes persist to `config.yaml`

When `management.password` is empty, authentication is bypassed.

## Management API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/auth` | POST | Login with password |
| `/api/settings` | GET, PUT | Read/update settings |
| `/api/nodes` | GET | List all nodes with status |
| `/api/nodes/{tag}/probe` | POST | Test node connectivity |
| `/api/nodes/{tag}/blacklist` | POST | Manually blacklist a node |
| `/api/nodes/{tag}/release` | POST | Release node from blacklist |
| `/api/nodes/probe-all` | POST | Probe all nodes (SSE stream) |
| `/api/quality/jobs` | POST | Create a background Cloudflare/reputation/combined quality job |
| `/api/quality/jobs/{id}` | GET | Read background quality job progress and summary |
| `/api/quality/jobs/{id}/results` | GET | Page through stable quality job results |
| `/api/quality/jobs/{id}/cancel` | POST | Cancel an active quality job |
| `/api/cloudflare/check` | GET | Run Cloudflare compatibility checks; add `background=true` for async jobs |
| `/api/cloudflare/cache` | GET, POST, DELETE | Read or clear Cloudflare compatibility cache |
| `/api/reputation/check` | GET | Run IP reputation checks for nodes; add `async=true` for async jobs |
| `/api/reputation/cache` | GET, POST, DELETE | Read or clear IP reputation cache |
| `/api/reputation/ip` | GET | Check reputation for a single IP address |
| `/api/export` | GET | Export node configuration |
| `/api/subscription/config` | GET, PUT | Manage subscription URLs |
| `/api/subscription/status` | GET | Check subscription status |
| `/api/subscription/refresh` | POST | Trigger manual refresh |
| `/api/nodes/config` | GET, POST, PUT, DELETE | CRUD for node config |
| `/api/reload` | POST | Reload sing-box instance |

## Docker Deployment

### docker-compose.yml

The default setup uses host networking (recommended for automatic port management). Volumes mount `config.yaml` and `nodes.txt`:

```yaml
services:
  easy_proxies:
    image: ghcr.io/jasonwong1991/easy_proxies:latest
    container_name: easy_proxies
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./config.yaml:/etc/easy_proxies/config.yaml
      - ./nodes.txt:/etc/easy_proxies/nodes.txt
      - ./logs:/app/logs
```

### Important Notes

- **Create config files first**: `config.yaml` and `nodes.txt` must exist as files before running `docker compose up`. Use `./start.sh` which handles this automatically.
- **Permissions**: Files need write permission for WebUI settings to persist (`chmod 666 config.yaml nodes.txt`).
- **Multi-platform**: Supports amd64 and arm64 architectures.
- **Reload**: `/api/reload` and subscription refresh will interrupt active connections.

### Ports

| Port | Usage |
|------|-------|
| 2323 | Pool proxy entry (pool/hybrid mode) |
| 9091 | WebUI and Management API |
| 1221 | GeoIP region router (when enabled, configurable) |
| 24000+ | Multi-port mode (one per node) |

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history.

## Development

```bash
go test ./...
```

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=jasonwong1991/easy_proxies&type=Date)](https://star-history.com/#jasonwong1991/easy_proxies&Date)

## License

MIT License

## React WebUI

新 WebUI 位于 `web/`，使用 Vite + React + TypeScript。构建产物输出到 `internal/monitor/assets/dist/`，Go 管理服务会优先服务 dist；如果 dist 不存在，则回退到 legacy WebUI。

常用命令：

```bash
./epctl.sh service:start
./epctl.sh service:status
./epctl.sh web:dev
./epctl.sh web:typecheck
./epctl.sh web:build
./epctl.sh restart
```

主要页面：

- `代理提取`：按地区、协议格式和数量提取代理。
- `节点总览`：展示全部节点，支持地区、可用性、延迟筛选和排序。
- `节点质量`：自动加载 CF/IP 信誉缓存；全量扫描和失败重试通过后台任务执行，结果服务端分页返回，避免 6000+ 节点时阻塞页面；也可通过 `quality_check` 配置定时刷新质量缓存。
- `运行状态`：展示节点状态、实时流量和可切换时间尺度的带宽图。
- `系统设置`：维护配置并写回 `config.yaml`。
- `日志诊断`：查看日志和诊断信息。

详细说明见 `WEB_UI_ARCHITECTURE.md`。

## Verify Zo Computer API Token

Use the standalone Node.js script to verify that a Zo Computer API access token can call `POST https://api.zo.computer/zo/ask`.

```bash
export ZO_API_TOKEN="zo_sk_xxx"
node verify_zo_token.mjs
```

The script reads the token from `ZO_API_TOKEN` and never prints the token.
