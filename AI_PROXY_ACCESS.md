# AI 代理获取说明

本文档给本地 AI Agent / 自动化脚本使用，目标是**稳定、安全地从 easy_proxies 获取本地代理入口**。

安全约束：

- 不读取、不输出订阅 URL。
- 不在日志中打印真实密码。
- 默认优先使用无认证本地端口或 WebUI 提取 API。
- 需要真实凭据时，只从本地运行时配置或 `/api/extractor?reveal=true` 显式获取。

---

## 1. 判断服务是否可用

```bash
curl -sI http://127.0.0.1:9091 | head
```

期望：

```text
HTTP/1.1 200 OK
```

也可以使用项目脚本：

```bash
./epctl.sh service:status
```

---

## 2. 常用本地代理入口

### 默认代理池

适合普通 curl / Python requests / 浏览器代理。

```text
http://<USERNAME>:<PASSWORD>@127.0.0.1:2323
socks5://<USERNAME>:<PASSWORD>@127.0.0.1:2323
```

说明：

- `2323` 是全局池，不保证固定国家。
- 认证信息来自本地配置，不要硬编码到代码库。

### GeoIP 地区入口

适合按国家/地区走池。

```text
http://<USERNAME>-jp:<PASSWORD>@127.0.0.1:1221
http://<USERNAME>-us:<PASSWORD>@127.0.0.1:1221
http://<USERNAME>-hk:<PASSWORD>@127.0.0.1:1221
```

支持区域：

```text
us jp hk sg tw kr in ae ch au de gb ca other
```

### Android 无认证地区端口

适合 Android 全局代理、ADB reverse、不支持用户名密码的客户端。

```text
us     13001
jp     13002
hk     13003
sg     13004
tw     13005
kr     13006
in     13007
ae     13008
au     13010
other  13011
de     13012
ca     13014
gb     13015
ch     13019
```

示例：日本无认证代理：

```text
http://127.0.0.1:13002
```

---

## 3. 使用 WebUI API 获取代理

推荐 AI Agent 使用 `/api/extractor`，不要自己解析订阅。

### 获取日本 multi-port 节点列表

```bash
curl -s 'http://127.0.0.1:9091/api/extractor?region=jp&mode=multi-port&format=json&count=10'
```

### 获取日本无认证 Android 入口

```bash
curl -s 'http://127.0.0.1:9091/api/extractor?region=jp&mode=android&format=host_port&count=1'
```

### 获取默认池入口

```bash
curl -s 'http://127.0.0.1:9091/api/extractor?region=all&mode=pool&format=http_url&count=1'
```

### 获取地区池完整 URL

```bash
curl -s 'http://127.0.0.1:9091/api/extractor?region=jp&mode=geoip&format=http_url&count=1'
```

---

## 4. 推荐格式

### curl

默认池：

```bash
curl -x 'http://<USERNAME>:<PASSWORD>@127.0.0.1:2323' https://api.ipify.org
```

日本地区池（通过用户名后缀选区，例如 `<USERNAME>-jp`）：

```bash
curl -x 'http://<USERNAME>-jp:<PASSWORD>@127.0.0.1:1221' https://api.ipify.org
```

Android 无认证日本端口：

```bash
curl -x 'http://127.0.0.1:13002' https://api.ipify.org
```

### Python requests

```python
proxies = {
    "http": "http://<USERNAME>:<PASSWORD>@127.0.0.1:2323",
    "https": "http://<USERNAME>:<PASSWORD>@127.0.0.1:2323",
}
```

地区池（通过用户名后缀选区，例如 `<USERNAME>-jp`）：

```python
proxies = {
    "http": "http://<USERNAME>-jp:<PASSWORD>@127.0.0.1:1221",
    "https": "http://<USERNAME>-jp:<PASSWORD>@127.0.0.1:1221",
}
```

### 指纹浏览器

优先使用 multi-port 单节点：

```text
host:port:username:password
username:password@host:port
http://username:password@host:port
socks5://username:password@host:port
```

从 API 获取：

```bash
curl -s 'http://127.0.0.1:9091/api/extractor?region=jp&mode=multi-port&format=host_port_user_pass&count=10&reveal=true'
```

注意：只有明确需要导入时才使用 `reveal=true`。

---

## 5. Android / ADB 配置

Android 全局代理不能直接使用需要认证的 multi-port 端口。必须使用无认证地区端口。

日本示例：

```bash
adb -s <SERIAL> reverse --remove-all
adb -s <SERIAL> reverse tcp:13002 tcp:13002
adb -s <SERIAL> shell settings put global http_proxy 127.0.0.1:13002
adb -s <SERIAL> shell settings put global global_http_proxy_host 127.0.0.1
adb -s <SERIAL> shell settings put global global_http_proxy_port 13002
```

清除：

```bash
adb -s <SERIAL> shell settings put global http_proxy :0
adb -s <SERIAL> reverse --remove-all
```

---

## 6. 选择优质节点的流程

AI Agent 推荐按以下顺序：

1. 调 `/api/nodes` 获取节点状态。
2. 过滤：
   - `available == true`
   - `region == 目标地区`
   - `port > 0`
3. 按 `last_latency_ms` 升序。
4. 对前 3-5 个端口做实际请求测试。
5. 选择成功率最高、平均延迟最低的端口。

示例：

```bash
curl -s http://127.0.0.1:9091/api/nodes \
  | jq -r '.nodes[]
    | select(.available == true and .region == "jp" and .port > 0)
    | [.port, .last_latency_ms, .name]
    | @tsv' \
  | sort -k2,2n
```

---

## 7. 故障排查

### WebUI 不通

```bash
./epctl.sh service:status
./epctl.sh service:start
```

### 某地区端口不通

```bash
./epctl.sh proxy:test jp
```

### Android 没网

检查：

```bash
adb devices -l
adb reverse --list
adb shell settings get global http_proxy
```

常见原因：

- 没有建立 `adb reverse`。
- 手机代理误设成认证端口，例如 `240xx`。
- 设置了 `127.0.0.1:240xx`，但该端口在手机本机不存在。
- 目标地区池暂无健康节点。

---

## 8. 不要做的事

- 不要解析或打印订阅 URL。
- 不要把真实用户名密码写进 Git。
- 不要把 `config.yaml`、`nodes.txt` 提交。
- 不要让 Android 全局代理直接使用 `240xx` 认证端口。
- 不要假设 `2323` 是某个国家；它是全局池入口。
