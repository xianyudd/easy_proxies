# Easy Proxies 外部 SDK 接入

当前仓库提供两个 SDK：

```text
sdk/typescript
sdk/python
```

它们适合 Node.js / Python 服务、脚本、Agent 或前端管理工具从 Easy Proxies 获取代理、读取节点状态和质量缓存。

## TypeScript 安装

在其他项目中使用本地路径安装：

```bash
npm install /path/to/easy_proxies/sdk/typescript
```

如果发布到私有 npm registry，可以直接安装包名：

```bash
npm install @easy-proxies/sdk
```

## Python 安装

在 Python 项目中使用本地路径安装：

```bash
pip install /path/to/easy_proxies/sdk/python
```

开发模式：

```bash
pip install -e /path/to/easy_proxies/sdk/python
```

## TypeScript 快速获取代理

```ts
import { EasyProxiesClient } from '@easy-proxies/sdk'

// 推荐：长期 API Key（config management.api_keys）
const client = new EasyProxiesClient({
  baseUrl: 'http://127.0.0.1:9091',
  apiKey: process.env.EASY_PROXIES_API_KEY,
})

const proxies = await client.getProxyUrls({
  region: 'jp',
  mode: 'multi-port',
  count: 5,
  reveal: true,
})

console.log(proxies)
```

## Python 快速获取代理

```python
from easy_proxies_sdk import EasyProxiesClient
import os

client = EasyProxiesClient(
    base_url="http://127.0.0.1:9091",
    api_key=os.environ.get("EASY_PROXIES_API_KEY"),
)

proxies = client.get_proxy_urls(
    region="jp",
    mode="multi-port",
    count=5,
    reveal=True,
)

print(proxies)
```

配合 `requests` 使用：

```python
import requests
from easy_proxies_sdk import EasyProxiesClient

client = EasyProxiesClient("http://127.0.0.1:9091")
proxy = client.get_proxy_urls(region="jp", mode="multi-port", count=1, reveal=True)[0]

res = requests.get(
    "https://api.ipify.org",
    proxies={"http": proxy, "https": proxy},
    timeout=15,
)

print(res.text)
```

## 获取结构化代理

```ts
const entries = await client.getProxyEntries({
  region: 'us',
  mode: 'multi-port',
  count: 10,
  reveal: true,
})

for (const entry of entries) {
  console.log(entry.node_name, entry.url)
}
```

## 读取节点和质量缓存

```ts
const nodes = await client.getNodes()
const cf = await client.getCloudflareCache()
const reputation = await client.getReputationCache()

console.log(nodes.total_nodes, cf.count, reputation.count)
```

## 触发质量扫描

```ts
await client.checkCloudflare({
  region: 'all',
  count: 500,
  includeUnavailable: true,
})

await client.checkReputation({
  region: 'all',
  count: 500,
  includeUnavailable: true,
})
```

## 管理端鉴权

### 推荐：API Key

```yaml
# config.yaml
management:
  password: "webui-secret"
  api_keys:
    - name: app-read
      key: "epk_xxx"
      role: read   # read | admin
      enabled: true
```

```ts
const client = new EasyProxiesClient({
  baseUrl: 'http://127.0.0.1:9091',
  apiKey: process.env.EASY_PROXIES_API_KEY,
})
// 无需 login
const proxies = await client.getProxyUrls({ reveal: true })
```

```python
client = EasyProxiesClient(
    base_url="http://127.0.0.1:9091",
    api_key=os.environ["EASY_PROXIES_API_KEY"],
)
proxies = client.get_proxy_urls(reveal=True)
```

`read` 只能拉代理/查状态；`admin` 可写（reload、settings、probe…）。请求头：`X-API-Key` 或 `Authorization: Bearer <key>`。

### 兼容：WebUI 密码 + session

TypeScript:

```ts
const client = new EasyProxiesClient({
  baseUrl: 'http://127.0.0.1:9091',
  password: process.env.EASY_PROXIES_PASSWORD,
})

await client.login()
const proxies = await client.getProxyUrls({ reveal: true })
```

Python:

```python
import os
from easy_proxies_sdk import EasyProxiesClient

client = EasyProxiesClient(
    base_url="http://127.0.0.1:9091",
    password=os.environ["EASY_PROXIES_PASSWORD"],
)

client.login()
proxies = client.get_proxy_urls(reveal=True)
```

## 常用参数

- `region`: `all`, `jp`, `us`, `hk`, `sg`, `tw`, `kr`, `au`, `gb`, `other`
- `mode`: `pool`, `multi-port`, `geoip`, `android`
- `count`: 返回数量，后端最大 500
- `reveal`: 是否返回真实代理用户名和密码

## 安全建议

- 其他项目和 Easy Proxies 在同机时，优先使用 `127.0.0.1:9091`。
- 跨机器：设 password + 只发 `role: read` key；`external_ip` 填公网地址；前挂 TLS。
- 配置了 `api_keys` 后，匿名访问管理 API 一律 401。
- 不要公网暴露无密码/无 key 的 `9091`。
- 只有确实需要代理凭据时才传 `reveal: true`。
