# IP 信誉检查 / IP Reputation

`easy_proxies` 增加了本地 IP 信誉检查能力，用来判断当前代理节点的真实出口 IP 是否更像住宅、机房、代理/VPN 或高风险出口。

安全边界：

- 不读取或展示订阅 URL。
- 不在 API 响应里返回代理密码。
- 不硬编码第三方 API key。
- 默认只通过本地 WebUI 管理接口访问。

## WebUI 使用

打开 WebUI：

```text
http://127.0.0.1:9091
```

进入：

```text
IP 信誉
```

推荐用法：

1. 地区选择 `日本(jp)`、`美国(us)` 或 `全部(all)`。
2. 模式保持 `multi-port 独立节点`。
3. 数量选择 `5` 或 `10`。
4. 点击 `开始检查`。

页面会显示：

- 节点名
- 本地端口
- 出口 IP
- 国家 / 地区
- ASN / ISP
- 风险等级
- 是否命中缓存
- 检测耗时

## API 用法

### 检查单个 IP

```bash
curl -s 'http://127.0.0.1:9091/api/reputation/ip?ip=1.1.1.1'
```

### 检查某个地区的 multi-port 节点

```bash
curl -s 'http://127.0.0.1:9091/api/reputation/check?region=jp&mode=multi-port&count=10'
```

### 查看缓存

```bash
curl -s 'http://127.0.0.1:9091/api/reputation/cache'
```

## CLI 用法

```bash
./epctl.sh reputation:check jp
./epctl.sh reputation:check all 10
./epctl.sh reputation:cache
```

如果 WebUI 设置了密码，可以通过环境变量传入，脚本不会打印密码：

```bash
WEBUI_PASSWORD='***' ./epctl.sh reputation:check jp 10
```

## 风险评分

当前基础版使用本地启发式评分：

| 信号 | 加分 |
|---|---:|
| ASN/ISP/Org 命中 hosting/cloud/datacenter 关键词 | +30 |
| Provider 判断 proxy | +40 |
| Provider 判断 VPN | +40 |
| Provider 判断 Tor | +80 |
| 节点地区和出口国家不一致 | +20 |
| 检测失败 | +50 |
| 延迟超过 3000ms | +10 |

等级：

```text
0-30   low
31-70  medium
71-100 high
```

## Provider

基础版内置：

- `ip-api.com`
- `ipwho.is`

后续可以扩展：

- AbuseIPDB
- IPQualityScore
- Scamalytics
- MaxMind

## 缓存

默认使用内存 TTL 缓存，避免频繁请求第三方接口。缓存内容只包含出口 IP 画像，不包含代理密码或订阅信息。

## 常见问题

### 检查失败

先确认服务运行：

```bash
./epctl.sh service:status
```

再确认节点可用：

```bash
./epctl.sh proxy:test jp
```

### 结果都是 high

常见原因：

- 出口是云服务器 / 机房 ASN。
- Provider 判断为 proxy/VPN。
- 节点标记地区和真实出口国家不一致。

### 为什么优先检查 multi-port？

multi-port 是“一端口一节点”，适合判断具体节点质量。pool/geoip 是聚合入口，出口可能变化，不适合给单个节点打稳定信誉分。
