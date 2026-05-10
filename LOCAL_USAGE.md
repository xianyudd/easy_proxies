# Easy Proxies 本地长期代理池使用说明

> 本文档面向当前这台机器上的本地使用方式，避免重复踩坑。

## 1. 当前推荐启动方式

如果你在 Docker Desktop / WSL 环境中使用，优先用显式端口映射：

```bash
docker compose -f docker-compose.desktop.yml up -d
```

原因：默认 `docker-compose.yml` 使用 `network_mode: host`，在 Docker Desktop / WSL 下可能出现容器已启动但本机无法访问 WebUI/代理端口的问题。

## 2. 启动服务

### 推荐方式（当前环境）

```bash
docker compose -f docker-compose.desktop.yml up -d
```

### 首次或需要重新拉镜像时

```bash
docker compose -f docker-compose.desktop.yml pull
docker compose -f docker-compose.desktop.yml up -d
```

## 3. 停止服务

```bash
docker compose -f docker-compose.desktop.yml down
```

## 4. 打开 WebUI

浏览器打开：

```text
http://localhost:9091
```

如果打不开，先检查容器状态：

```bash
docker ps
```

## 5. 使用默认代理池

当前默认池入口是本地 HTTP/SOCKS 混合入口，建议优先按 HTTP 代理使用。

示例（请把账号密码替换成你自己的，本示例已脱敏）：

```bash
curl -x http://<USERNAME>:<PASSWORD>@127.0.0.1:2323 https://api.ipify.org
```

也可以在系统或应用里配置：

- 主机：`127.0.0.1`
- 端口：`2323`
- 用户名：`<USERNAME>`
- 密码：`<PASSWORD>`

## 6. 使用地域代理

只有在 `geoip.enabled: true` 且已配置 GeoIP 监听端口时，这些入口才可用。

格式：

```text
http://<USERNAME>:<PASSWORD>@127.0.0.1:<GEOIP_PORT>/<REGION>/
```

示例：

- US：`/us/`
- JP：`/jp/`
- HK：`/hk/`
- SG：`/sg/`

验证示例：

```bash
curl -x http://<USERNAME>:<PASSWORD>@127.0.0.1:<GEOIP_PORT>/us/ https://api.ipify.org
```

如果当前没启用 GeoIP，则这些入口不存在。

## 7. 使用多端口代理

只有在以下条件成立时，多端口入口才可用：

- `mode` 是 `multi-port` 或 `hybrid`
- `multi_port.base_port` 已配置

启用后，每个节点会分配一个独立端口，例如：

```text
127.0.0.1:24000
127.0.0.1:24001
127.0.0.1:24002
```

如果配置了认证，那么多端口入口同样需要带认证信息。

验证示例：

```bash
curl -x http://<USERNAME>:<PASSWORD>@127.0.0.1:24000 http://cp.cloudflare.com/generate_204 -I
```

## 8. 自检脚本

仓库根目录已提供：

```bash
./check_proxy_pool.sh
```

它会：

- 测试默认代理池
- 测试常见 GeoIP 地域入口（如果已启用）
- 测试多端口入口示例（如果已启用）
- 输出出口 IP 和国家（若查询接口未限流）
- 不打印你的代理密钥或订阅地址

## 9. 综合控制脚本

仓库根目录提供：

```bash
./epctl.sh
```

常用命令：

```bash
./epctl.sh start
./epctl.sh stop
./epctl.sh restart
./epctl.sh status
./epctl.sh logs 100
./epctl.sh logs-follow
./epctl.sh test jp
./epctl.sh adb-set jp
./epctl.sh adb-status
./epctl.sh adb-clear
```

说明：

- `status` 会显示 WebUI、监听端口、节点统计和地区分布
- `test <region>` 会测试 Android 无认证地区端口，并带重试
- `adb-set <region>` 会设置 `adb reverse` 和 Android 全局代理
- 默认 ADB 设备是 `192.168.1.118:5555`，可用 `ADB_SERIAL=...` 覆盖
- 脚本不会打印订阅地址或代理密码

## 10. 常见问题排查

### WebUI 打不开

先看容器是否在运行：

```bash
docker ps
```

再看日志：

```bash
docker logs --tail 100 easy_proxies_desktop
```

### 容器启动了，但 `localhost:9091` 访问不到

在 Docker Desktop / WSL 下，优先使用：

```bash
docker compose -f docker-compose.desktop.yml up -d
```

不要优先使用 `network_mode: host` 版本。

### 默认代理池有端口，但请求失败

检查：

1. 当前节点健康检查是否有可用节点
2. WebUI 中节点是否大量被拉黑
3. 订阅是否刷新成功
4. 本地是否把请求又套进了别的代理环境变量

建议测试时临时取消环境变量：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
```

### GeoIP 地域代理不可用

通常原因：

- `geoip.enabled` 没开
- 没有配置 GeoIP 监听端口
- 当前地域没有健康节点

### 多端口代理不可用

通常原因：

- 当前 `mode` 不是 `multi-port` / `hybrid`
- 没有配置 `multi_port.base_port`
- 节点数量变化后端口分配发生变化

## 11. 建议的长期使用方式

如果你的目标是“长期稳定像代理平台一样本地使用”，建议：

1. 保持 Docker 常驻
2. 开启订阅自动刷新
3. 平时通过 WebUI 观察节点健康和黑名单情况
4. 日常使用默认代理池，只有在明确需要时再启用 GeoIP 或多端口
5. 每次改配置后运行：

```bash
./check_proxy_pool.sh
```
