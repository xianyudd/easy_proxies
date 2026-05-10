# Proxy Extractor 使用说明

## 功能概览

WebUI 新增了一个本地代理提取页面：**代理提取 / Proxy Extractor**。

它的目标是提供类似代理平台的“提取代理”体验，但仍然基于你当前这台机器上已经运行的 Easy Proxies：

- 默认代理池入口（pool endpoint）
- GeoIP 地域入口（geoip region endpoint）
- 多端口独立节点入口（multi-port node list）
- Android 全局代理入口（android global proxy）

默认情况下，页面**脱敏显示密码**；只有在你显式勾选后，才允许复制真实凭据。

---

## 页面入口

打开 WebUI：

```text
http://localhost:9091
```

左侧导航中进入：

- **代理提取**

---

## 页面参数说明

### 1. 区域选择

支持：

- `all`
- `us`
- `jp`
- `hk`
- `sg`
- `tw`
- `kr`
- `other`

### 2. 模式选择

#### pool endpoint
输出默认代理池入口。

适合：
- curl
- Python requests
- 系统全局代理
- 长期统一代理出口

#### geoip region endpoint
输出 GeoIP 路由入口，例如 `/us/`、`/jp/`。

适合：
- 按国家路由的请求
- 需要“美国池 / 日本池 / 香港池”这种入口时

注意：
- 这种模式带 URL 路径
- 只能导出为完整 URL 形式

#### multi-port node list
输出每个节点对应的本地独立端口。

适合：
- 指纹浏览器
- 浏览器代理扩展
- 需要稳定使用单个节点的场景
- 多账号并行使用不同端口

> **指纹浏览器优先推荐 multi-port 模式**。

#### android global proxy
输出 Android 全局代理适用的本地无认证端口。

适合：
- `adb reverse` + `settings put global http_proxy`
- Android 真机全局 HTTP 代理
- 不支持用户名密码的导入场景

说明：
- 该模式会按 `android_proxy.region_ports` 和 `android_proxy.base_port` **智能计算端口**
- 如果某个国家配置了独立端口覆盖，提取器会优先使用覆盖值
- 推荐格式是 `host:port` 或 `ADB 命令`

---

## 支持的导出格式

### 1. `host:port:username:password`
示例：

```text
127.0.0.1:24000:<USERNAME>:<PASSWORD>
```

### 2. `username:password@host:port`
示例：

```text
<USERNAME>:<PASSWORD>@127.0.0.1:24000
```

### 3. `http://username:password@host:port`
示例：

```text
http://<USERNAME>:<PASSWORD>@127.0.0.1:24000
```

### 4. `host:port:username:password[refresh_url]{remark}`
示例：

```text
127.0.0.1:24000:<USERNAME>:<PASSWORD>{JP-Node-01}
```

如果没有 `refresh_url`，则不会输出 `[]` 部分。

### 5. `username:password@host:port[refresh_url]{remark}`
示例：

```text
<USERNAME>:<PASSWORD>@127.0.0.1:24000{JP-Node-01}
```

### 6. JSON
适合程序读取。

### 7. `host:port`
适合：
- Android 全局代理
- 不支持认证字段的导入工具

### 8. `adb shell settings put global http_proxy host:port`
适合：
- 直接复制到命令行执行
- `adb reverse` 场景

### 9. `http://host:port`
适合无认证 HTTP 代理导入。

### 10. `socks5://username:password@host:port`
适合支持 SOCKS5 URI 的代理工具。

### 11. `socks5://host:port`
适合无认证 SOCKS5 URI 导入。

### 12. `host,port,username,password`
适合 CSV 风格批量导入。

### 13. `host|port|username|password`
适合部分批量工具的分隔符导入。

### 14. `curl -x http://username:password@host:port`
适合直接复制到命令行测试。

### 15. Python requests JSON
适合程序读取：

```json
{
  "http": "http://<USERNAME>:<PASSWORD>@127.0.0.1:24000",
  "https": "http://<USERNAME>:<PASSWORD>@127.0.0.1:24000"
}
```

### 16. Clash YAML object
适合转成 Clash 配置片段。

---

## 重要格式说明

### GeoIP 地域入口为什么只能用完整 URL？

因为这类入口带路径，例如：

```text
http://<USERNAME>:<PASSWORD>@127.0.0.1:1221/us/
```

而以下格式无法表达 `/us/` 这种路径：

- `host:port:username:password`
- `username:password@host:port`

因此在 geoip 模式下，如果你选了不兼容的格式，系统会自动切换为完整 URL 格式。

---

## 推荐使用方式

### curl
优先：
- `pool endpoint`
- `http://username:password@host:port`

### Python requests
优先：
- `pool endpoint`
- `http://username:password@host:port`

### 浏览器代理扩展
优先：
- `multi-port node list`
- `username:password@host:port`
或
- `host:port:username:password`

### 指纹浏览器
优先：
- **multi-port node list**

推荐格式：
- `host:port:username:password`
- `username:password@host:port`

不建议优先用：
- pool endpoint
- geoip endpoint

因为指纹浏览器通常更适合稳定单节点，而不是共享池。

### Android 全局代理
优先：
- **android global proxy**

推荐格式：
- `host:port`
- `adb shell settings put global http_proxy host:port`

典型流程：

```bash
adb reverse tcp:13001 tcp:13001
adb shell settings put global http_proxy 127.0.0.1:13001
```

如果不走 `adb reverse`，则把 `127.0.0.1` 改成电脑局域网 IP。

---

## 安全行为

### 默认脱敏
页面默认不会直接展示真实密码，而是显示：

```text
***
```

### 复制真实凭据
如果你勾选了“显示真实凭据”：

- 生成结果会包含真实密码
- 点击复制时会再次确认

### 不会暴露订阅 URL
提取器不会返回原始订阅链接。

---

## 常见问题排查

### 1. 生成结果为空
检查：

- 服务是否启动
- 当前模式是否已启用
- 目标区域是否有可用节点

### 2. GeoIP 模式没有结果
检查：

- `geoip.enabled` 是否启用
- GeoIP 数据库是否已下载完成
- 当前区域是否存在健康节点

### 3. multi-port 结果为空
检查：

- 当前运行模式是否为 `multi-port` 或 `hybrid`
- `multi_port.base_port` 是否已配置
- 节点是否已成功映射端口

### 4. 复制后无法使用
检查：

- 是否选择了适合客户端的格式
- 是否需要完整 URL
- 客户端是否支持用户名/密码认证
- 是否误把带路径的 geoip 代理用成了不支持路径的格式

### 5. 指纹浏览器该选什么
建议：

- 模式：`multi-port node list`
- 区域：按需选 `us/jp/hk/sg/...`
- 格式：
  - `host:port:username:password`
  - 或 `username:password@host:port`

---

## 本地调试建议

每次改配置后，可以先看：

```bash
./check_proxy_pool.sh
```

如果默认池、GeoIP、多端口都正常，再去 WebUI 中使用代理提取页面。
