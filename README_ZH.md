# Easy Proxies

[English](README.md) | 简体中文

Easy Proxies 是一个基于 sing-box 的代理池管理工具。

目标是把大量上游节点统一成稳定的本地 HTTP/SOCKS5 代理入口，同时支持按节点独立端口访问。

## 当前能力

- 运行模式：`pool`、`multi-port`、`hybrid`。
- 实际构建的上游协议：`vmess`、`vless`、`trojan`、`ss/shadowsocks`、`hysteria2/hy2`、`socks5/socks`、`http/https`、`anytls`、`tuic`。
- 节点来源：
  - `config.yaml` 的 `nodes`
  - `nodes_file`（每行一个 URI）
  - `subscriptions`（支持 Base64/纯文本/Clash YAML 解析）
- 自动健康检查、失败熔断和黑名单恢复。
- Web 管理面板 + API：
  - 节点状态/探测/导出
  - **手动拉黑/解封节点**
  - 动态设置（`external_ip`、`probe_target`、`skip_cert_verify`、`geoip`）
  - 节点配置增删改查 + 重载
  - 订阅状态查询 + 手动刷新 + **保存即时生效**
  - **实时日志控制台**（最近 1000 行，WebSocket 流式传输）
- 新增可配置 DNS 解析器（对 VMess 域名节点非常关键）。
- 可选 GeoIP 标记（支持 JP/KR/US/HK/TW/SG 地域分区，可在 WebUI 中开关，支持自动更新和热重载）。
- **可配置日志轮转**，支持大小限制、备份数量和压缩。

## 快速开始

### 1）准备配置

```bash
cp config.example.yaml config.yaml
cp nodes.example nodes.txt
```

编辑 `config.yaml`，并配置节点来源（`nodes.txt` / `subscriptions` / `nodes`）。

### 2）启动

推荐使用本地控制脚本：

```bash
./epctl.sh service:start
./epctl.sh service:status
```

Docker：

```bash
./start.sh
# 或
docker compose up -d
```

本地运行：

```bash
go run ./cmd/easy_proxies -config config.yaml
```

前端开发：

```bash
./epctl.sh web:dev
```

## 最小配置示例（Pool）

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

## DNS 配置说明

`dns` 会同时影响 sing-box DNS 客户端和 VMess 域名拨号解析：

```yaml
dns:
  server: 223.5.5.5
  fallback_servers:    # 备用 DNS 服务器（主 DNS 解析失败时使用）
    - 8.8.8.8
    - 1.1.1.1
  port: 53
  strategy: prefer_ipv4
```

`strategy` 可选值：

- `as_is`
- `prefer_ipv4`
- `prefer_ipv6`
- `ipv4_only`
- `ipv6_only`

如果日志中出现 `lookup <domain>: empty result`，请优先检查该 DNS 配置是否可达且策略合理。

## 运行模式

- `pool`：所有节点共享一个本地 HTTP/SOCKS5 入口。
- `multi-port`：每个节点一个独立本地 HTTP/SOCKS5 端口。
- `hybrid`：同时启用 pool + multi-port。

## 节点来源行为

- 配置了 `subscriptions` 时：
  - 会抓取订阅节点并追加到运行节点列表
  - `nodes_file` 作为订阅节点写入路径
  - 启动阶段不再从 `nodes_file` 读取节点
- `free_proxy_sources` 是免费代理候选源，会在订阅源 / 节点文件 / 内联节点之后处理；开启 `free_proxy_filter.enabled=true` 后，系统会先抓取、去重并用有界 worker 自动预筛，只有达到 `min_tier` 的代理才会作为 `source=free_proxy` 运行节点展示。`http_basic` 要求 HTTP 204 探针通过；`simple_web` 还要求 HTTPS example 探针通过。默认每个源会全量下载/解析并进入后台候选处理；`free_proxy_max_nodes: 0` 表示最终运行入池也不额外截断；只有填写正数时才限制最终入池数量；单源 `max_nodes: 0` 表示该源全量解析，可用正数 `max_nodes`/`max_bytes` 做显式保护；也可以在设置页“免费代理源”中添加/启用/删除源。
- `nodes`（内联节点）只要存在就会参与运行。

## 协议支持注意事项

运行时真正支持的协议：

- `vmess`
- `vless`
- `trojan`
- `ss` / `shadowsocks`
- `hysteria2` / `hy2`
- `socks5` / `socks`
- `http` / `https`
- `anytls`
- `tuic`

订阅解析阶段可能识别到更多 URI 前缀（兼容输入），但不在上述列表中的协议会在构建阶段被跳过。

## 管理 API（核心）

- `POST /api/auth`
- `GET|PUT /api/settings`
- `GET /api/nodes`
- `POST /api/nodes/{tag}/probe`
- `POST /api/nodes/{tag}/release`
- `POST /api/nodes/{tag}/blacklist`
- `POST /api/nodes/probe-all`（SSE）
- `POST /api/quality/jobs`（创建后台 CF / IP 信誉 / 组合质量检测任务）
- `GET /api/quality/jobs/{id}`（查询后台任务进度和汇总）
- `GET /api/quality/jobs/{id}/results`（分页读取后台任务结果）
- `POST /api/quality/jobs/{id}/cancel`（取消后台任务）
- `GET /api/cloudflare/check`（CF 兼容性检测；大规模扫描可加 `background=true`）
- `GET|POST|DELETE /api/cloudflare/cache`
- `GET /api/reputation/check`（IP 信誉检测；大规模扫描可加 `async=true`）
- `GET|POST|DELETE /api/reputation/cache`
- `GET /api/reputation/ip`
- `GET /api/export`
- `GET|PUT /api/subscription/config`
- `GET|POST /api/subscription/status|refresh`
- `GET|POST|PUT|DELETE /api/nodes/config[...]`
- `POST /api/reload`

`management.password` 为空时，Web/API 不要求登录。

## 重要运行说明

- 重载（`/api/reload` 或订阅刷新）会中断现有连接。
- Settings API 会把配置写回 `config.yaml`；部分设置需要重载后才能完全生效。
- 省略项默认值可在 `internal/config/config.go` 中查看。
- 日志轮转通过 `log` 配置段设置；当 `output: file` 时，日志同时写入控制台和文件，并自动轮转。

## WebUI 页面

访问地址默认是 `http://127.0.0.1:9091`。

主要页面：

- `代理提取`：按地区、协议格式和数量提取代理，并支持复制。
- `节点总览`：展示全部节点，支持地区、可用性、延迟筛选和排序。
- `节点质量`：进入页面自动加载缓存；全量扫描和失败重试通过后台任务执行，结果服务端分页返回，避免 6000+ 节点时阻塞页面；展示 CF 评分、IP 风险和综合质量排名。后端也支持通过 `quality_check` 配置定时刷新质量缓存。
- `运行状态`：展示节点状态、实时流量和可切换时间尺度的带宽图。
- `系统设置`：维护配置并写回 `config.yaml`。
- `日志诊断`：查看日志、运行状态和诊断信息。

## 更新日志

详见 [CHANGELOG.md](CHANGELOG.md)。

## 开发验证

```bash
go test ./...
```

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=jasonwong1991/easy_proxies&type=Date)](https://star-history.com/#jasonwong1991/easy_proxies&Date)

## 许可证

MIT License
