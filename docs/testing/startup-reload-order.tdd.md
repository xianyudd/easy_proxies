# START-1 TDD 证据：启动与配置重载的时序问题

分支：`fix/startup-reload-order-20261108`（基于 e45409b，未提交）
日期：2026-11-08
说明：本文件按用户要求记录 RED/GREEN 证据；不创建 checkpoint commit。

## 1. 现场日志线索与根因定位

现场（CST）关键事件：

- 03:00:56 `[boxmgr] sing-box instance started with 1205 nodes`（启动完成）
- 03:01:08 `reloading with ... nodes`（第一次重载开始；此时 Reload 已把 `currentBox` 置 nil）
- 03:01:10/12/14 `WARN port 24034 is still being released, retrying before reassignment...`
- 03:01:11 `free proxy cache filter reload failed: manager not started`
- 03:01:16/17 24034→24035、24036→24109 的重分配

三个可观察症状，根因都在 `internal/boxmgr/manager.go` 的生命周期实现：

1. **并发重载互相看到 `currentBox == nil`**：`Reload`（旧 199–218 行）在锁内
   把 `m.currentBox = nil` 后释放锁，随后才 close 旧实例、sleep、重建。
   另一个重载（app 后台免费代理刷新、订阅刷新、监控 server 异步重载可并发触发）
   在这段窗口内读取到 `currentBox == nil`，直接返回 `errors.New("manager not started")`
   —— 正是 03:01:11 的报错。这是"manager not started"的直接来源，
   与"是否已等到 Start 完成"无关（Start 早已返回），是**重载之间**的竞态。

2. **重载与 Close 重叠导致泄漏**：`Close`（旧 393–415 行）只关 `m.currentBox`；
   若重载已把 `currentBox` 置 nil 而 Close 先执行，Close 跳过实例关闭；重载随后
   在 `Reload` 尾部把新实例装回 `m.currentBox` —— Close 之后又出现一个
   仍在运行的实例，且永不关闭（泄漏）。

3. **端口释放等待是盲等**：`Reload` 在 `oldBox.Close()` 后固定
   `time.Sleep(500ms)`，然后构建新实例；遇到 bind 失败再用
   `3 × 200ms` 重试，最后 `reassignConflictingPort` 永久搬移节点端口
   （24034→24035→…）。`Close` 慢（03:00:41 的 hysteria2 outbound 关闭慢）时
   500ms 不够、3×200ms 也不够，于是把"内核释放窗口"误判为第三方冲突，
   触发无谓的端口重分配；重分配后的端口又写进 `m.cfg`，下一次重载得以保留，
   形成连锁搬移（24036→24109）。另外 `NormalizeWithPortMap` 为未保留端口
   的新节点选端口时同样提前观察到"不可用"。

结论：三者并非同源锁问题，但同属"生命周期层没有串行化 + 没有把端口释放
变成前置条件"。修复必须落在生命周期层，而不是加睡眠/加大 retries。

## 2. RED 证据（修复前，e45409b + 新测试）

新增回归测试：`internal/boxmgr/manager_lifecycle_test.go`
（同步点全部用 channel/封锁的 fake box，不用随机 sleep，不用真实网络）。

命令：

```bash
go test -count=1 -run 'TestReloadDuringStart|TestConcurrentReloads|TestCloseDuringReload|TestReloadClosesOld' -v ./internal/boxmgr/
```

失败输出要点（真实粘贴）：

```
[FAIL] TestReloadDuringStartWaitsForStartAndDoesNotReportNotStarted
  manager_lifecycle_test.go:122: Reload while Start was in progress returned manager not started; w...
[FAIL] TestConcurrentReloadsAreSerializedAndNeverReportNotStarted
  manager_lifecycle_test.go:176: second Reload overlapped an active reload and returned manager not...
[FAIL] TestCloseDuringReloadDoesNotLeakRunningBox
  manager_lifecycle_test.go:243: box 1 was started but never closed: leaked running instance after ...
```

即：并发重载报 `manager not started`（与线上日志逐字一致）；Close 期间的重载
把新实例装回，泄漏运行中的 box。

## 3. 修复内容

`internal/boxmgr/manager.go`：

- 新增生命周期状态机 `ManagerState`（not_started / starting / running /
  reloading / closed）+ `State()` 查询 + 显式错误
  `ErrManagerNotStarted` / `ErrBoxManagerClosed`。
- 新增 `lifecycleMu sync.Mutex`，`Start` / `Reload` / `Close` 全部串行：
  - 重载不再可能看到另一半的 `currentBox==nil`；
  - `Close` 与重载互斥，Close 返回后不会再被装回新实例；
  - Start 未完成时 Reload 等待（后台刷新"启动完成前不进入"）。
- `Reload` 取消语义：baseCtx 已取消时直接恢复旧实例并返回 ctx 错误，不重建。
- 新增 `waitForListenPortsReleased`：旧实例 Close 后**轮询**等待新配置所需
  监听端口真正可绑（`config.IsPortAvailable`），有界超时
  （`defaultPortReleaseTimeout=5s`，可经 `WithPortReleaseTimeout` 配置）。
  取代固定 `Sleep(500ms)`，端口释放成为新实例启动的前置条件；
  只有超时后仍被占用的端口才进入既有的 WARN + 重试 + 重分配逻辑
  （合法语义保留：真实占用仍重试/重分配并保留 WARN）。
- `rollbackToOldConfig` 改为返回 error；回滚失败置 `StateNotStarted`，
  后续 Reload 得到明确的 `ErrManagerNotStarted`。
- `waitForHealthCheck` 支持 ctx 取消，避免关闭被启动健康检查拖死。

`internal/app/app.go`：

- 删除后台免费代理缓存过滤重载前的 `time.Sleep(3 * time.Second)`——原先用
  定时睡眠掩盖"manager 未就绪"，现在生命周期锁保证 Reload 在 Start 完成前
  不会进入，睡眠既非必需也不再能掩盖竞态。

为什么不是靠延时掩盖：没有新增任何 sleep；删除了一处 sleep；替换为
"端口确实可绑/实例确实就绪"的可验证前置条件；并发问题靠互斥状态机解决。

## 4. GREEN 证据（修复后）

同一批测试（原 RED）与新增行为测试全部通过：

```bash
go test -count=1 -v ./internal/boxmgr/   # 27 passed
--- PASS: TestReloadDuringStartWaitsForStartAndDoesNotReportNotStarted
--- PASS: TestConcurrentReloadsAreSerializedAndNeverReportNotStarted
--- PASS: TestCloseDuringReloadDoesNotLeakRunningBox
--- PASS: TestReloadClosesOldBoxBeforeBuildingNewBox
--- PASS: TestReloadBeforeStartReturnsErrManagerNotStarted
--- PASS: TestClosedManagerRejectsReload
--- PASS: TestStartFailureLeavesManagerNotStarted
--- PASS: TestFailedReloadAndRollbackLeavesNotStarted
--- PASS: TestReloadAbortsOnCancelledBaseContext
--- PASS: TestWaitForListenPortsReleasedBlocksUntilPortFrees
--- PASS: TestWaitForListenPortsReleasedHonoursCancellation
--- PASS: TestReloadRetriesThenReassignsOnPersistentPortConflict
--- PASS: TestStartRetriesAndReassignsOnPortConflict
```

（`TestReloadRetries...` 用 fake box 返回 bind error 前 4 次，走完
WARN→重试→重分配→成功，验证合法端口冲突语义仍保留。）

新增测试对端口冲突路径里对 `transientPortConflictBackoff` 临时改为 1ms，
结束后恢复，避免测试慢。

## 5. race 与整体测试

- `go test -race -count=1 -p 2 ./internal/boxmgr/...` → **27 passed**，无数据竞争。
- `go test -count=1 -p 2 -timeout 600s ./...` → **653 passed in 15 packages**。
- 基线对照：e45409b 上同样的全套 → **642 passed in 15 packages**（无失败）。
  两者差异 = 本次新增 11 个测试，无新增失败。
- `go test -race ./...`（全仓）：除 freepromote 包的**既有**测试数据竞争外均通过；
  该竞争在 e45409b 基线同样复现（`internal/freepromote/service_test.go:58`
  `fakeNodes.TriggerReload` 与测试轮询 goroutine 对同一字段无锁写读），
  与本次改动无关，属历史基线失败。

## 6. 覆盖率（boxmgr，go test -coverprofile + go tool cover -func）

| 函数 | 覆盖率 |
|---|---|
| setState / setStateAfterFailedStart | 100% |
| recoverAfterFailedReload | 100% |
| portsStillBound | 100% |
| waitForListenPortsReleased | 92.9% |
| configListenPorts | 81.0% |
| rollbackToOldConfig | 78.3% |
| Start | 73.9% |
| Reload | 78.8% |
| Close | 70.6% |

说明：核心新增路径（状态机错误分支、端口释放等待、回滚失败、取消）高于 80%。
`Start` / `Reload` / `Close` 低于 80% 的剩余分支为需要真实 sing-box /
GeoIP 数据库 / Android 路由/健康检查窗口才触发的分支（如 `cfg.GeoIP.Enabled`
的 router 启停、`startAndroidProxyRouters`、`waitForHealthCheck` 成功路径、
重载成功后的 `monitorMgr.ClearSource` 等），用 fake box 无法在包内稳定覆盖；
未编造覆盖数字。

## 7. 已知限制

- `lifecycleMu` 在同一把锁上串行 Start/Reload/Close：一次重载期间 Close 会
  等待其结束（应用层已有 30s 关闭超时包裹）；极端慢的旧实例 Close 可能让
  排队重载等待较久，这是"端口释放作为前置条件"的代价，符合修复目标。
- 全仓 `-race` 仍受 freepromote 既有测试竞争影响（基线同样失败），不在本次
  范围内修复。
- 未部署、未重启服务、未改真实配置、未 commit/push、未切分支。

---

# START-1B 第二轮修复证据：端口搬移（wait 未覆盖真实绑定端口 + reassign 不持久化）

日期：2026-11-08，分支不变（`fix/startup-reload-order-20261108`），未提交。
v1 生命周期锁成果全部保留（manager not started 消失），本轮的改动只补充
端口映射/分配层。

## 8. 根因

v1 部署实证的两个现象：

1. `waitForListenPortsReleased` 从不打印 "still held"，但 Start 仍对 24034/24036
   `bind: address already in use`，且每次重载都撞同一端口、搬移后又搬回原端口。
2. 重载完成后同一 PID 同时 LISTEN 24034/24035/24036/24109（多占端口）。

代码走查结论（`internal/boxmgr/manager.go`、`internal/config/config.go`）：

- **冲突是本配置内部的自相冲突**，不是与外部进程的冲突：`NormalizeWithPortMap`
  第一遍按 `portMap[nodeKey]` 赋保留端口，而 `NodeKey` 基于 `canonicalNodeURI`，
  会剥离 `#name` 片段并小写化。订阅里同一服务器两条不同命名（如题目现场的
  "🇸🇬新加坡 | 高速专线-hy2" 与 "🇸🇬新加坡3 | 高速专线-hy2"）折叠成同一个 key，
  两条节点都被赋到同一端口 24034/24036 → sing-box 配置里两个 inbound 同端口，
  Start 必失败。OS 层面端口是空的，所以 `waitForListenPortsReleased` 必然静默
  —— 与零命中证据一致。
- `reassignConflictingPort` 只搬第一个命中的节点：搬完一个，另一个仍留在原端口
  → 重试成功后新实例同时监听原端口与新端口（多占端口证据）；且搬移结果只写进
  内存 `newCfg`，app 侧下一次热重载用的是 `cfg.BuildPortMap()`（启动时 cfg 副本，
  静态端口表），不含搬移结果 → 下一次重载又回到 24034 → 反复撞同一端口。
- `configListenPorts` 排除了 `Source==free_proxy` 且 `Port==0` 直接跳过，集合不是
  "sing-box 实际绑定端口全集"。

## 9. RED 证据（修复前）

新增 `internal/boxmgr/manager_portmap_regression_test.go`，5 个测试全红：

```bash
go test -count=1 -run 'TestConfigListenPortsCoversAllNodePortsIncludingFreeProxy|TestReloadWithStalePortMapKeepsReassignedPort|TestReloadCycleWithDuplicateURIStopsColliding|TestReassignConflictingPortMovesEveryNodeOnThePort|TestNormalizeWithPortMapKeepsDuplicateURIsOnDistinctPorts' -v ./internal/boxmgr/
```

失败输出要点（摘自修复前真实运行，括号内为解读）：

```
[FAIL] TestConfigListenPortsCoversAllNodePortsIncludingFreeProxy
  configListenPorts missing port 30300 (got map[2323:true 30234:true 30236:true])   ← free_proxy 独立 Port 被排除
[FAIL] TestReloadWithStalePortMapKeepsReassignedPort
  second reload started the box 6 times; a preserved port must build cleanly once   ← 旧端口表把搬移结果退回原端口
[FAIL] TestReloadCycleWithDuplicateURIStopsColliding
  round1 reload started the box 5 times; duplicate ports must be resolved before build
[FAIL] TestReassignConflictingPortMovesEveryNodeOnThePort
  node "b" still on the conflicting port 30260
[FAIL] TestNormalizeWithPortMapKeepsDuplicateURIsOnDistinctPorts
  duplicate URIs collapsed onto the same port 30270; sing-box would bind it twice and fail
```

## 10. 修复内容

- `internal/config/config.go` `NormalizeWithPortMap` 第一遍：端口已被使用（同配置
  其他节点、hybrid 共享池 Listener.Port）时不再强制赋给该节点，跳过交给第二遍
  分配新端口 → 一个配置里不可能出现两个同端口 inbound（新增中文注释）。
- `internal/boxmgr/manager.go` `configListenPorts`：去掉 `Source==free_proxy` 排除，
  凡 `Port>0` 都计入（含 free_proxy），宁可多等也不漏。
- `internal/boxmgr/manager.go` `reassignConflictingPort`：把占用冲突端口的节点
  **全部**搬到不同可用端口（不再只搬第一个）。
- `internal/boxmgr/manager.go` `ReloadWithPortMap`：先以 `m.CurrentPortMap()`（含
  运行时搬移结果）合并调用方端口表，当前表为准、旧表只补新 key → 下一次重载
  不再退回搬移前端口。
- `internal/app/app.go`：两处热重载从 `cfg.BuildPortMap()` 改为 `boxMgr.CurrentPortMap()`
  —— 应用启动时持有的 cfg 副本是静态端口表，搬移后不会更新，必须从 manager 读。
- 第三处核对（Android/GeoIP 路由端口）：已在 lifecycleMu 内先 stop 再重建，且
  `RegionPortsAvoiding` 按节点端口避让，无独立未加锁绑定路径；无需改动。

## 11. GREEN 证据与整体

```bash
go test -count=1 -run '<同上5个>' -v ./internal/boxmgr/   # 5 passed
go test -count=1 ./internal/boxmgr/                         # 32 passed
go test -race -count=1 ./internal/boxmgr/...                # 32 passed
go test -count=1 -p 2 -timeout 600s ./...                   # 660 passed in 15 packages
go test -race -count=1 ./internal/config/ ./internal/app/   # 38 passed in 2 packages
```

覆盖率（新增/改动函数，boxmgr）：`ReloadWithPortMap` 81.8%、
`reassignConflictingPort` 82.6%、`configListenPorts` 85.7%、
`waitForListenPortsReleased` 92.9%、`portsStillBound` 100%。

全仓 `-race`：仅 freepromote 既有 2 个测试数据竞争失败（`service_test.go:58`
`fakeNodes.TriggerReload` 与测试轮询 goroutine 无锁写读），v1 文档已记录且
本轮 diff 未触碰 `internal/freepromote/`，属历史基线失败。

## 12. 部署后应观察

- 无新增 WARN：`port %d is still being released` 与 `reassigning node ... to port`
  不再出现在同一节点同端口上反复出现（重载前后各一次以内属正常）。
- `waitForListenPortsReleased` 超时告警文案：`port(s) [...] still held after ...`
  只应在真实第三方占用时出现。
- `NormalizeWithPortMap` 新增告警：`Port %d for node ... already used, assigning a new port`
  出现即表示存在重复 URI 被折叠，属于期望内的自动处理。
- 多占端口检查：`ss -ltnp | grep easy_proxies`（或按 PID），同一端口不应同时
  出现在重载前后的新老配置里；单 PID 不应同时 LISTEN 一对"被搬移"的端口。
- 未部署、未重启服务、未改真实配置、未 commit/push、未切分支。