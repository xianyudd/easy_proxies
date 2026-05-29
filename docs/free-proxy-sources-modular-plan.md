# 免费代理源（Free Proxy Sources）模块化接入实施方案

## 背景与目标

`easy_proxies` 当前已有三类节点来源：

- `nodes`：直接写在 `config.yaml` 中的内联节点。
- `nodes_file`：本地文件，每行一个代理 URI。
- `subscriptions`：订阅链接，启动或刷新时抓取并写入 `nodes.txt`。

本方案新增第四类来源：`free_proxy_sources`。目标是在不重构 `boxmgr`、`builder`、`outbound/pool` 运行内核的前提下，把“免费代理列表”作为独立来源模块接入启动加载流程。

## 设计原则

1. **来源层模块化**：新增 `internal/nodesource`，避免继续把所有来源解析逻辑堆进 `internal/config/config.go`。
2. **兼容现有配置**：`nodes`、`nodes_file`、`subscriptions` 的行为保持不变。
3. **MVP 先落地**：先支持启动时加载本地/HTTP 免费代理列表，后续再扩展 UI、API、缓存和调度。
4. **运行时输入不持久化**：免费代理源节点由外部列表提供，`SaveNodes` 不把它们写回 `nodes.txt` 或 `config.yaml`。

## 已落地范围

### 配置

新增配置：

```yaml
free_proxy_sources:
  - name: "local-free-list"
    file: free-proxies.txt
    format: txt
    enabled: true
  - name: "remote-json-list"
    url: "https://example.com/free-proxies.json"
    format: json
    timeout: 15s
    enabled: false
```

说明：

- `name`：来源名称，用于日志和追踪。
- `file`：本地来源文件；相对路径按 `config.yaml` 所在目录解析。
- `url`：远程 HTTP(S) 来源。
- `format`：`txt`、`json` 或空/`auto`。
- `enabled`：可选，默认启用。
- `timeout`：远程来源请求超时，默认 30 秒。

### 模块

新增 `internal/nodesource`：

- `SourceConfig`：来源配置结构。
- `Provider`：统一加载入口，支持本地文件和 HTTP URL。
- `ParseFreeProxyContent`：按格式解析免费代理列表。

### 支持格式

`txt`：

```text
# comment
1.2.3.4:8080
http://5.6.7.8:3128
socks5://example.com:1080
```

- `host:port` 默认补为 `http://host:port`。
- 已带 scheme 的 URI 会保留。
- 空行、`#`、`//` 注释会跳过。

`json`：

```json
[
  {"ip":"1.2.3.4","port":8080,"protocol":"https","country":"US"},
  {"host":"example.com","port":"1080","type":"socks5"},
  {"uri":"http://5.6.7.8:3128","name":"named"}
]
```

也支持对象包装：`{"proxies": [...]}`、`{"data": [...]}`、`{"items": [...]}`。

### Config 集成

`Load` 会：

1. 解析 `free_proxy_sources`；
2. 解析相对 `file` 路径；
3. 在 `normalize` 中加载各来源；
4. 转换为 `config.NodeConfig`；
5. 标记 `Source = NodeSourceFreeProxy`；
6. 与已有节点合并进入后续 builder/pool 流程。

`SaveNodes` 会跳过 `NodeSourceFreeProxy`，避免把外部免费代理源节点持久化到本地节点文件。

## 后续扩展建议

- 为 WebUI 增加来源管理页面。
- 为管理 API 增加 `GET/POST/DELETE /api/free-proxy-sources`。
- 增加来源缓存和失败退避，避免启动时远程来源不稳定影响体验。
- 将来源刷新接入现有 `subscription_refresh` 热重载链路。
- 增加质量过滤：Cloudflare 兼容性、IP reputation、延迟、可用性。
- 支持更多公共列表格式，例如 CSV、自定义字段映射、Clash provider。

## 验证

新增测试覆盖：

- 文本格式解析、注释跳过、默认 `http://` scheme。
- JSON 数组和字段映射。
- 本地文件和 HTTP provider 加载。
- `config.Load` 合并 `free_proxy_sources` 并标记 `NodeSourceFreeProxy`。
