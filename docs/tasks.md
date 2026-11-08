# 任务看板

| 编号 | 标题 | 状态 | 负责方 | 范围与验收 |
| --- | --- | --- | --- | --- |
| START-1 | 复现启动与重载争用，增加回归测试并修复 | 已完成（v1 部分验证） | startup-worker | manager not started 消失 |
| START-1B | 第二轮修复：端口搬移问题（wait 未覆盖真实绑定端口；reassign 不持久化） | 已完成并部署验证通过 | startup-worker-2 | 见下方 v2 验收记录 |
| START-2 | 核对运行二进制、源码版本与构建部署方式 | 已完成 | runtime-scout | 只读核查完成 |
| START-3 | 独立复核实现与并发、回滚测试 | 已替换为 v1/v2 部署实证验收 | 主会话 | 部署验证结论见 v2 验收 |
| START-4 | 构建、备份并更新本机服务，重启验收 | 已完成（v2 通过） | 主会话编排 | 仅重启代理服务 |

## 目标与计划

用户要求修复代理启动及配置重载的时序问题。已观察到停止旧服务时 Hysteria2 关闭慢；启动后免费代理缓存刷新出现 manager not started；热重载重试24034并重分配24034、24036。警告集中于启动窗口，服务最终可用。根因待证，不能假定三者同源。

1. 追踪应用启动、manager Start/Reload/Stop、订阅/免费代理初始化和回调顺序；以可控同步点构造复现，避免依赖随机 sleep。
2. 先记录现有逻辑的失败测试，再最小修改生命周期协调、回调注册或串行化逻辑；保留重载失败回滚和合法外部端口冲突处理。
3. 运行相关测试、竞态检测与适当整体检查；独立复核。
4. 核对并备份当前挂载的二进制，使用相同功能构建标签更新本机程序，验证服务重启与配置重载，必要时回退。

## 验收清单（v1 实测结果）

- [x] 有可执行的RED复现，不依赖随机调度或真实订阅网络。（v1 完成，13 个测试）
- [x] 对应GREEN测试通过，相关并发测试-race通过。（v1：boxmgr 27 passed + race 无新增竞争）
- [x] 启动中刷新、并发重载、关闭/取消不产生争用或死锁；重载失败回滚保留。（v1 实现；manager not started 消失）
- [ ] 运行程序版本与本次源码对应，已有热修复不丢失。（v1 已部署 f7f0a0b7；热修复在 git 724d97a 不丢）
- [x] 本机仅重启代理服务，管理HTTP200、runtime warn、真实代理请求成功。（v1 部署后 9091/9092 200、2323 open、gstatic 204）
- [ ] 新日志无 manager not started 或自身重载引起的端口重分配。（v1：manager not started 消失 ✅，但端口重分配 24034/24036 复现两次 ❌）
- [x] 线上配置指纹不变，备份与回退路径明确。（f5c30b41…不变；备份 easy_proxies.bak-lifecyclefix-20261108-050228）
- [ ] 未执行commit/push，逐文件解释最终改动。

## v1 部署实证发现（START-1B 输入）

时间线（均为 2026-11-08，容器 PID 298471，新二进制 f7f0a0b7）：
- 05:03:18 reloading 1060 nodes；05:03:22/24/26 WARN 24034 still being released；05:03:28 reassign 24034→24035（节点"🇸🇬新加坡 | 高速专线-hy2"）；05:03:30 reassign 24036→24109（"🇸🇬新加坡3 | 高速专线-hy2"）；05:03:33 completed 1060。
- 05:03:34 reloading 1046；05:03:37/39/41 再次 24034 still being released；05:03:43 再次 reassign 24034→24035；05:03:45 再次 24036→24109；05:03:48 completed 1046。同一节点两次撞同一端口。
- waitForListenPortsReleased 从未打印 "still held"（日志零命中）→ 其端口集合没包含 24034/24036。
- 重启后 ss：单一 easy_proxies PID 同时 LISTEN 24034/24035/24036/24109 → reassign 后原端口仍被绑定（多占），非残留进程。
- config.yaml 顶层 nodes 不含这两个节点（来自订阅动态注入，解析 nodes=[]）。
- IsPortAvailable = 标准 net.Listen（可靠）。
- 疑点1：configListenPorts 排除 node.Source==NodeSourceFreeProxy 且跳过 Port==0；调用时机在 NormalizeWithPortMap 之后但 portMap 可能不含 reassign 结果。
- 疑点2：reassignConflictingPort 只改内存 newCfg；app 侧调用 ReloadWithPortMap(reloadedCfg, cfg.BuildPortMap()) 用静态端口表 → 下次 reload 回到原端口 → 反复撞。
- 结论：端口搬移未解决；manager not started 已解决（v1 生命周期锁保留）。

## v2 部署实证（2026-11-08 06:03:40 CST 重启，二进制 c76e8b25…）

- v2 进程自启动以来统计：still_being_released 0、still_in_use_after 0、reassigning_node 0、still_held 0、manager_not_started 0；dup_uri_handled 4（重复 URI 折叠被正确改分新端口）；trying_next_port 348（配置层预分配的正常避让，非搬移）。
- 06:04:04 日志确认重复 URI 处理生效：`Port 24034 for node "🇸🇬新加坡2 | 高速专线-hy2" already used, assigning a new port`、同 24036 新加坡4。
- 两次 reload 成功（1029、1026 节点）；真实代理 gstatic 204；端点 9091/9092/2323 均 200/open。
- 多占消除：v1 时代 24034+24035 并存，v2 下 24030-24040 均为同一 pid 正常单端口监听，24035 不再单独多占；24109 属正常节点（香港动态家宽-02），非搬移残留。
- 配置指纹 f5c30b41… 不变；服务 NRestarts=0、systemd running。
- 部署二进制 f7f0a0b7(v1) → c76e8b25(v2)；备份 easy_proxies.bak-v1-20261108 与 easy_proxies.bak-lifecyclefix-20261108-050228；未回退。
- 仍未 commit/push；分支 fix/startup-reload-order-20261108 工作区含 v1+v2 改动与新增测试。

## START-5 部署实证（builder 日志降级，2026-11-08 06:32:56 重启，二进制 a0705f33…）

- 目标：消除每次构建/重载约 3 行/节点（~1200 节点 → 3600+ 行）的低信息密度日志。
- 改动（`internal/builder/builder.go`，+31/−5）：`debugLogLevel()` 判定；`printProxyLinks` 非 debug/trace 只打一行摘要（pool/hybrid `🌐 Pool Entry Point:`、multi-port/hybrid `🔌 Multi-Port Entry Points: N nodes`）；`⏳ Building nodes... %d/%d` 进度行门控到 debug。
- 保留未动：所有 ⚠️/❌ 警告错误（GeoIP 失败、node build failed、XHTTP 回退、ECH cache miss、prefetch 失败）、低频状态（GeoIP loaded、区域分布、upstream detour、ech_cache 聚合行）。
- 测试：新 `internal/builder/builder_log_test.go` 3 个；全套 663 passed、builder -race 37 passed（干净快照独立复验）。
- 部署实证：备份 `easy_proxies.bak-builderlog-20261108`（原 c76e8b25）；替换 a0705f33；系统 d restart 后 NRestarts=0。
- 启动窗口 journal：907 行（含 348 行 config 端口避让警告 = v2 已知正常 trying_next_port，非搬移）；`📡 Proxy Links:` 页头 0、每节点 HTTP/SOCKS5 0；摘要各 1 次（单次构建 ✅）。
- 端点 9091/9092 200、2323 open、真实代理 gstatic 204；config 指纹 f5c30b41… 不变。
- 工作区：`M internal/builder/builder.go` + `?? internal/builder/builder_log_test.go`；未 commit/push。

## START-6 部署实证（Go 1.26.10 工具链安全升级，2026-11-08 07:14:02 重启，二进制 a9925c33…）

- 来源：cherry-pick 上游 jasonwong1991/easy_proxies `9f6ccf9`（等价应用，本地基线 go 1.24.1 → 1.26.10，Docker `golang:1.24` → `1.26.10`）；commit `6c7d69c` 已推送 origin main。
- 目的：闭合 GO-2026-6809（`crypto/x509` 中间证书不信任，仅 1.26.10 修复，release gate govulncheck 可触达）。
- 验证：Go 1.26.10 工具链经 goproxy.cn 可下载；干净快照 663 passed；构建产物 `a9925c33…`（50.4MB）。
- 部署：备份 `a0705f33` → `easy_proxies.bak-go124-20261108`；替换 a9925c33；重启后 NRestarts=0、PID 608132。
- 验收：`go version -m` 确认 buildinfo `go1.26.10`；9091/9092 200、2323 open、真实代理 gstatic 204；config 指纹 f5c30b41… 不变；启动窗口 journal 879 行（每节点横幅 0、摘要 1），err/warning 级仅 6 行。
- 工作区整洁：main = 6c7d69c，已推送；无遗留改动。

## START-7 部署实证（上游可靠性小修复移植，2026-11-08 08:35:17 重启，二进制 2c9df1be…）

- 内容：等价移植上游 3 个修复（commit `b9271b2`，已推送 origin main）——socks5h 协议支持、VLESS packetEncoding 白名单 + `callWithBoxNew` panic 防护、WebUI 长名/URI 截断。
- 独立代码审查：审查代理发现 `isValidNodeURI` 漏 socks5h（WebUI 手工添加被拒），已修复；recover 测试改为直接覆盖 `callWithBoxNew` 真实路径。
- 验证：全量 669 passed（+6 新测试）、race 通过；快照 669 passed。
- 部署：备份 `a9925c33` → `easy_proxies.bak-pre-port-20261108`；替换 2c9df1be；NRestarts=0、PID 776255。
- 验收：9091/9092 200（9092 启动整合期约 20s 内一次 000 后恢复）、2323 open、真实代理 gstatic 204；buildinfo go1.26.10；启动窗口 `Multi-Port` 摘要 7 次（启动期免费代理刷新链触发 7 次构建，每次仅 1 行摘要，08:36:13 后收敛为 0 reload）、warn/err 6 行；每节点横幅 0。
- config 指纹：**由 f5c30b41… 变为 5bc26e28…，发生在 08:15:52（本次重启前 20 分钟，属上次部署运行期的应用自动持久化写入），本次部署前后未再变**；结构核验正常、reload 成功、配置有效；我方未改任何业务配置。
- 工作区：main = b9271b2（+ 本看板 commit）已推送；无遗留。

## START-8 部署实证（上游 d423519 并发订阅刷新移植，2026-11-08 10:04:16 重启，二进制 f94af15c…）

- 内容：等价移植上游 d423519（commit `41a089c`，已推送 origin main）——并发订阅抓取（`fetch_concurrency` 默认 16/上限 32）、订阅 URL 与节点双重去重、抓取错误与日志脱敏、响应体 10 MiB 上限、缓存回退统一为 `ShouldUseCachedSubscriptionNodes`、Clash http/https 代理条目、outbound/pool 注册日志汇总。
- 独立代码审查：两轮；第二轮结论“通过”。审查发现已处理——`url.Parse` 错误改走脱敏、并发测试空断言改为真实 token + `redacted=1` 断言、helper 导出并由 manager 复用、补 manager 级缓存回退测试（全空 / 部分失败缩水两分支）。
- 验证：全量 675 passed、`-race` 通过；gofmt / vet 干净。
- 部署：备份 `2c9df1be` → `easy_proxies.bak-pre-d423519-20261108`；替换 f94af15c（运行中文件报 `Text file busy`，改用 `.new` + `mv` 原子替换并校验哈希）；重启后 NRestarts=0、PID 942178。
- 验收：`go version -m` 确认 go1.26.10、模块伪版本含 `41a089c`；9091/9092 200、2323 open、真实代理 gstatic 204（预热后 0.28s）；启动窗口 `Multi-Port` 摘要 15 次（免费代理刷新链触发）、warn/err 15 行（403 订阅、free-proxy promote shortfall、提升节点健康检查拒绝等既有噪音）、panic/fatal 0；每节点横幅 0（该注册路径在本部署未触发，与旧版一致）。
- config 指纹：5bc26e28… → fbc7386a…（写入发生在 10:05:11 启动整合期，属应用自身持久化；未经我方编辑、无 WebUI 操作；端点/节点/端口均无变化）。
- 遗留观察（非本次引入）：启动期串行订阅路径仍以原文 URL 写日志，运行期并发抓取路径已脱敏；建议后续单独修复。
- 工作区：main = 41a089c（+ 本看板 commit）已推送；无遗留。

## 约束

- 不改用户节点、订阅、端口、日志阈值和其他业务配置；不打印凭据。
- 不触碰 Windows Redis、SearXNG、WSL 网络配置。
- 新增注释优先中文；测试输出用英文。
- 用户未要求 commit/push，不创建提交；RED/GREEN证据记录到 docs/testing/startup-reload-order.tdd.md。
- 仓库开始于干净 main（e45409b）；本次工作分支 fix/startup-reload-order-20261108。
- 部署前线上config.yaml SHA256：f5c30b41a3227dbcff81075cbaa41cbf5b918657debd8f46edb09ab25f3387bd（仅记录指纹，不记录私有配置内容）。部署后复核不变。
- 部署前服务active，NRestarts=0；WSL systemd running。
