# EP-02 命名本地节点源 TDD 证据

## 轮次：2026-11-26 v2 四项审查修复与覆盖补测

### 当前结论与边界

v2 实现侧测试和真实 CLI 进程验证通过；本轮结束后停止写入，交主会话进行最终差异的独立复验。下方 v1 和阶段1a结论是历史记录，不作为当前版本的覆盖率或未实现清单。

本轮仅补四项 P2 和刷新/持久化行为测试，不重写架构。仍未读取生产配置、导入真实 13 条节点、使用浏览器、操作生产服务、部署或执行 Git 写操作。所有新运行使用合成节点、独立数据目录和回环临时端口，网络命名空间无外网。未联网、安装依赖或修改锁文件。

### 四项 P2：RED → GREEN

| 问题 | RED 证据 | v2 行为与回归 |
| --- | --- | --- |
| 主来源为订阅的共享节点没有本地源筛选入口 | `ep02-v2-monitor-red-valid.log` 中来源统计断言失败 | `TestSharedLocalOnlySourceStats`：保留主来源计数，额外按运行出口计入 `local_source`，同一出口多个本地引用只计一次；前端通过可执行 helper 生成「本地源」选项。 |
| 全成员表没有健康状态 | 同一日志中 `member_health` 缺失 | `TestLocalMemberHealthIncludesEverySavedMember`：按保存顺序逐个返回可用/不可用/未检测/未应用/未运行；不删除离线或没有运行出口的成员。停用来源仍由其他 owner 提供的连接显示实际运行健康。UI 复用现有 `probeNode` 检测入口，不新建测活系统。 |
| preview 缺少来源上下文共享统计 | `ep02-v2-preview-red.log`：新方法缺失的编译 RED | `TestLocalPreviewCountsOtherOwnersNotSelf`：带 source ID/revision，排除当前来源自己的 active/draft 成员，连接去重；分别返回非本地、其他已应用本地、其他草稿来源的安全数量，验证旧 revision 和不存在 ID 的拒绝。 |
| 直接 POST/PUT 非法成员只返回通用错误 | `ep02-v2-monitor-red-valid.log` 中原始行号断言失败 | `TestLocalSaveReturnsSafeOriginalLine`：BOM、注释及空行不改变第 3 行定位；typed 解析错误只返回固定安全原因和行号，内部存储异常仍脱敏。真实 HTTP E2E 复验相同契约。 |

共享数口径：`shared_count` 采用当前编辑视图（存在草稿则用草稿，否则用 active）中的其他启用本地来源，加上当前非本地 owner；每个连接最多计一次。`shared_active` 独立报告当前已应用的其他本地 owner；`shared_draft` 仅在有草稿时报告其他草稿 owner。各分项可能重叠，不能相加。关闭本来源不停止仍由其他来源提供的出口，UI 有明确提示。总览 `source_stats.local_source` 是筛选成员计数，与主来源计数可能重叠，不可再将所有来源计数之和当作出口总数。

`ep02-v2-web-red.log` 是新 helper 缺失的模块加载 RED。以下仅属测试夹具错误，不计业务 RED：首次 Go 字符串直接嵌入 BOM 导致编译错误；来源统计 fixture 原本已有订阅节点却按零基数断言；订阅刷新 fixture 使用旧解析器不接收的 HTTP 文本导致抓到零节点。最终用 rune/chr 构造真实 BOM、相对初始计数断言和合法 socks5 文本，不放松生产验证。

定向 GREEN：`ep02-v2-target-green.log`、`ep02-v2-web-green.log`、`ep02-v2-behavior-final.log`；最终同一源码版本再次完成下述全库、race、前端和真实进程验证。

### 刷新与持久化补测

- `internal/app/local_sources_refresh_test.go`：真实启动 manager；覆盖启动过滤重载和 cache refresh→auto reload 两条后台路径，验证运行节点更新但草稿不应用、不丢失；未启动 manager 时，两条路径均报告 reload 失败。未改生产定时器或增加注入框架。
- `internal/subscription/local_sources_refresh_test.go`：真实回环订阅抓取及 sing-box manager 提交；验证成功统计/cache 跟踪、inline 和草稿保留、过期订阅配置拒绝且 runtime 不变、空响应、HTTP 503 和重叠刷新跳过。
- `TestAtomicWritePrecommitErrorsAndNewFileRollback`：父目录不存在、父路径非目录、读取目标失败不会报告成功；新文件首次目录同步失败会删除候选并再次同步，删除失败则返回提交不确定。保留 v1 的旧文件恢复、权限保持、恢复失败留备份及第二次目录同步错误测试。

### 最终命令、结果与运行证据

沿用 v1 固定 Go 1.26.10、pnpm 10.33.0 和冻结依赖根；设置 `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly`，不下载。以下 Go 命令均在 `unshare -Urn`、仅启用 `lo` 的环境执行：

```sh
"$GO" test -tags with_clash_api \
  -skip '^TestFetchECHConfigList_Live$' -count=1 -timeout=240s -p=2 \
  -coverpkg=./internal/app,./internal/boxmgr,./internal/builder,./internal/config,./internal/monitor,./internal/outbound/pool,./internal/subscription \
  -coverprofile=/tmp/ep02-v2-all.cover ./...

"$GO" test -tags with_clash_api -race -count=1 -timeout=240s -p=2 \
  ./internal/app ./internal/config ./internal/boxmgr ./internal/subscription ./internal/monitor

"$GO" build -tags 'with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api' \
  -o /tmp/ep02-v2-isolated-app ./cmd/easy_proxies
```

- 非 short 全库、五包 race 和生产 tags 构建均 exit 0。只精确跳过外网依赖 `TestFetchECHConfigList_Live`。
- 前端隔离副本 `/tmp/ep02-web-v2-ifx3qd0w`：typecheck、build、`node --test tests/*.test.mjs` 均 exit 0；**15/15**（本地源 7 项、既有订阅归属 8 项），无 skip。仍有大于 500 kB chunk 的构建提示：echarts 1,126.69 kB、主 JS 1,203.67 kB；本轮未做拆包。
- 前端只执行类型检查、构建、纯函数测试及组件/页面源码接线断言。没有 React 渲染、浏览器交互或前端语句覆盖报告；不能据此宣称组件/API/页面执行覆盖。前端产物与 CLI 后端分开验证，不冒称已组装或部署新前端发布包。

真实 CLI 验证：

```sh
unshare -Urn -- sh -c 'ip link set lo up && python3 -I /tmp/ep02-v2-process.py \
  /tmp/ep02-v2-isolated-app \
  /home/jason/.local/state/easy-proxies-ep02-evidence-20261126/process-v2'
```

结果 exit 0、`real-process-result.json` 为 PASS。实际启动的二进制 SHA-256：`b5888893a5d7d74823c4ff915a08eb97d8f50a0ecc8f1569ef58610ce9060c35`。通过真实管理 HTTP 验证：

1. POST/PUT 含 BOM、注释和空行时，第 3 行错误有安全行号/原因；共享预览排除自身；草稿全成员保留且无运行出口显示未应用。
2. 普通重载和重启不应用草稿；显式应用后重启保留完整成员、筛选统计与健康状态。
3. 停用并应用后，无运行出口成员为未运行，而仍由 inline 提供的共享成员继续显示运行健康；删除最后来源、应用并重启后没有残留草稿。

driver 用 SIGTERM 停止并等待自己启动的 PID。Go 真实 HTTP E2E 同时保留认证、严格请求、冲突、settings 提交、替换及 lifecycle 断言，并在全库与 race 中执行。

### 完整七包覆盖复算

使用 `/tmp/ep02-v2-all.cover`，跨包重复区块按任一命中合并，以含既有 EP-01 dirty 的开工冻结工作树为基线。插入/替换行只要与 cover 区块相交，就统计该完整区块，因此是「变更相交区块覆盖」，不是精确新增语句覆盖；不测量纯删除行，也不含前端。

| 生产变化文件 | 命中/总语句 | 比例 |
| --- | ---: | ---: |
| `internal/app/app.go` | 5/5 | 100.00% |
| `internal/boxmgr/local_sources.go` | 187/207 | 90.34% |
| `internal/boxmgr/manager.go` | 60/63 | 95.24% |
| `internal/builder/builder.go` | 6/6 | 100.00% |
| `internal/config/config.go` | 92/99 | 92.93% |
| `internal/config/local_source_protocols.go` | 104/114 | 91.23% |
| `internal/config/local_source_vless.go` | 23/26 | 88.46% |
| `internal/config/local_sources.go` | 94/99 | 94.95% |
| `internal/config/local_sources_state.go` | 185/224 | 82.59% |
| `internal/config/local_sources_validate.go` | 15/18 | 83.33% |
| `internal/monitor/manager.go` | 8/8 | 100.00% |
| `internal/monitor/server.go` | 68/69 | 98.55% |
| `internal/monitor/server_local_sources.go` | 165/192 | 85.94% |
| `internal/outbound/pool/pool.go` | 9/9 | 100.00% |
| `internal/subscription/manager.go` | 31/31 | 100.00% |
| **合计 15 文件** | **1052/1170** | **89.91%** |

独立 QA 的 v1 七包同口径为 936/1075（87.07%）；v2 新增代码使分母也增加，不能只比较命中数。app 变更从 0/5 到 5/5，subscription 从 11/31 到 31/31。完整函数覆盖另计：`startFreeProxyCacheRefresh` 81.8%、`doRefresh` 98.2%、`PreviewLocalSource` 92.9%、`localSourceMemberHealth` 100%。

`atomicConfigWriteWithSync` 函数仍只有 **67.2%**，不是 80%。本轮覆盖了可自然触发的读取/父路径错误及新文件回滚；未覆盖的低层失败包括 Stat 异常、CreateTemp、Chmod、Write、文件 Sync、Close 及提交 Rename 失败分支（完整区块位置在归档 JSON）。没有为提高数字引入通用文件系统注入层，不将文件级 82.59% 解释为该函数达标。

### v2 证据身份与剩余边界

稳定目录：`/home/jason/.local/state/easy-proxies-ep02-evidence-20261126/implementation-handoff-v2/`。v1 `implementation-handoff/` 和 `process-final/` 未覆盖。

- `source-manifest.json` SHA-256：`d19fc3706dd037db57de63a03b19403cc88c6da0907c6b65e42a6556fd8e35e5`；覆盖源码、测试和构建输入。本证据文档另行复制并纳入归档总清单，避免文档自引用 hash。
- `changes-vs-ep01-baseline.json`：40 个源码/测试变化文件及相对冻结基线的快照/diff。`diff-vs-v1/` 另保留本轮增量。
- `preserved-ep01-files.json`：仍有 8 个 EP-01 文件与开工基线完全一致；NodeOverview 在 v2 为共享筛选接入而改变，不再沿用 v1 的「9 个全未变」说法。
- `lockfiles.json`、`baseline-integrity.json`、`tested-frontend-binding.json`：依赖清单/锁文件不变、原基线未改、前端副本与当前源码逐项一致。
- `logs/`、完整 cover profile、函数覆盖、逐块未命中列表、`process/`、driver 和归档脚本可供复验；最终归档 `manifest.sha256` 标识所有证据。

仍需明确：格式预览不是所有内核选项的完整验证，应用时对本地构建失败整次拒绝；替换 diff 仍由客户端计算；原 admin 节点导出 URI 契约未重写；订阅缓存失败后的整个 runtime 事务恢复、所有共享配置并发路径、free cache 启动配置引用和历史 uint16 分配边界未全面重构。四项 P2 的实现侧回归通过，不代替主会话对最终版本的独立复验。

---

## 历史 v1：2026-11-26 整体实现与审查修复交接

### 当前结论与边界

实现侧自测已完成，源码停止修改，交独立 QA；此记录不代替独立验收或生产部署批准。

本轮完成命名来源的创建、文本/文件快照导入、格式预览、替换差异与确认、改名、整组启停、确认删除，以及草稿保存和显式应用。来源清单保留合法但离线的成员；多来源相同连接只创建一个运行出口，同时保存来源归属。普通重载、订阅刷新、免费源刷新和进程重启使用已应用来源，不把草稿自动变成运行配置。

- 仅使用合成节点、独立临时数据目录和回环临时端口；运行置于无外网网络命名空间。
- 未读取生产配置、未导入真实 13 条节点、未操作生产服务、未部署、未用浏览器。
- 未联网或增加依赖，未改 Go/前端依赖清单和锁文件，未提交、推送或改变 Git 分支。
- 原有 EP-01 未提交改动保留；差异以开工时冻结工作树为基线，而非直接把整个 HEAD diff 归于 EP-02。

### 稳定证据与源码身份

证据根目录：`/home/jason/.local/state/easy-proxies-ep02-evidence-20261126/`。

- `implementation-handoff/source-manifest.json`：测试交接时源码、测试和构建输入的 SHA-256 清单。清单本身 SHA-256：`394f0e18144e88a5b3b7dd4419a749208eef28cec12d1719e58980332fe4431f`。
- `implementation-handoff/changes-vs-ep01-baseline.json`：36 个源码/测试变化文件，注明新增、修改及是否原本已有 EP-01 dirty。允许编辑的本证据文档另行归档。
- `implementation-handoff/source/`、`diff/`：这些文件的源码快照与相对冻结基线的逐文件 patch。
- `implementation-handoff/baseline-integrity.json`：冻结基线与开工哈希逐项一致。
- `implementation-handoff/preserved-ep01-files.json`：9 个既有 EP-01 源码/测试文件完全未变；其中包含 NodeOverview、Settings、样式和订阅归属测试，不能计作本轮新增。
- `implementation-handoff/lockfiles.json`：`go.mod`、`go.sum`、`web/package.json`、`web/pnpm-lock.yaml` 与基线相同。
- `implementation-handoff/tested-frontend-binding.json`：当前前端源码/测试与实际执行构建及测试的隔离副本逐文件一致。
- `implementation-handoff/logs/`：下述 RED/GREEN 原始日志。`changed-block-coverage.json` 保留覆盖口径及未覆盖区块。
- `process-final/real-process-result.json`、`real-process.log`：进程级 HTTP 生命周期结果与应用日志。测试进程已通过 SIGTERM 清理。

### 实现影响面

| 模块 | 本轮行为 |
| --- | --- |
| `internal/config/local_source*.go` | 纯协议结构校验、限制和脱敏错误；来源模型、revision、active/draft、来源组合；稳定 sidecar 锁与原子更新。 |
| `internal/config/config.go` | 保存时展开原始归属；保留 active/draft；启动和重载端口分配同时跳过来源保留端口与已占用端口。 |
| `internal/boxmgr/local_sources.go`、`manager.go` | 配置提交互斥、最新快照、明确 revision 应用、失败恢复；订阅和免费源抓取后按最新状态组合提交。 |
| `internal/subscription/manager.go`、`internal/app/app.go` | 后台来源提交不再用旧全量配置覆盖最新本地来源/inline。 |
| `internal/builder/builder.go` | 带本地来源的成员构建失败时整次拒绝，避免跳过坏成员后仍报告成功；旧订阅宽松行为保留。 |
| `internal/monitor/server_local_sources.go`、`server.go` | admin-only CRUD/preview/apply，严格 JSON、no-store、跨源保护、revision 冲突；运行列表支持本地来源筛选和名称搜索。 |
| `internal/monitor/manager.go`、`internal/outbound/pool/pool.go` | 传播本地来源 ID/name；Register 更新 entry 字段时同步持 entry 锁。 |
| `web/src/components/nodes/LocalSourcesSection.tsx` 及辅助文件 | 来源编辑、差异预览和独立替换确认；改名/启停无需重传成员；待应用状态以服务端 revision 为准。 |

原始非本地成员使用内部、扁平的 `NodeConfig.Origins` 保存，禁止嵌套，不序列化到 API/YAML；本地归属另用只含 ID/name 的引用。`InlineNodes` 仍有记录与深拷贝，但当前组合和保存以 Origins 为依据。这与「保存原始成员、运行时去重」目标对应，不将 InlineNodes 声称为唯一输入。

### RED → GREEN 与独立审查回归

下列 RED 均保留原日志，最终 GREEN 由后面的同一版本全库和 race 结果承接。

| 问题/测试 | RED 证据 | 修复与 GREEN 断言 |
| --- | --- | --- |
| 基础来源重复、显式端口冲突、重启复用本地保留端口、禁用本地后免费源接管 | `ep02-review-red-valid.log` | `TestLocalCompositionBaseDuplicatesAndPortConflicts`、`TestLoadReservesLocalMemberPorts`、`TestDisabledLocalAllowsFreeSourceTakeover`；组合去重并保留归属，端口冲突整次拒绝，先重建免费来源再合并。 |
| 原子写目录 Sync 失败 | `ep02-atomic-red.log` 是故障注入接口尚未实现的编译 RED，不能冒充已执行的 I/O 失败 | `TestAtomicWriteDirectorySyncFailureRestoresOriginal` 验证原配置恢复，错误不能伪装成成功。 |
| 恢复时 0400 权限变宽、rollback rename 失败仍删除恢复文件 | `ep02-recovery-red.log` | `TestAtomicRecoveryPreservesPermissionsAndFailedRollback`；恢复文件权限不放宽，恢复失败保留文件并返回明确不确定状态。 |
| 恢复后第二次目录 Sync 失败 | `ep02-second-sync-red.log` | `TestAtomicRecoverySecondSyncFailureIsNotSuccess`；两次同步都可注入，旧内容已恢复但同步失败仍返回错误。 |
| A：跳过外部占用后又撞上本地保留端口 | `ep02-review-followup-red.log` | `TestLocalPortsSkipOccupiedAndReservedTogether`，multi-port/hybrid × 启动/重载四路径真实监听回归。 |
| B：inline/订阅共享连接保存后清空订阅缓存 | 同上 | `TestSharedInlineSubscriptionSurvivesSaveAndOfflineRestart`；内部原始归属展开后分别保存，离线缓存重启仍保留两个 owner。 |
| 订阅撤回误删 inline/local 或残留旧订阅名 | 对应回归测试随整体测试执行 | `TestSharedOriginsAfterSubscriptionWithdrawal`；订阅撤回、重复组合、保存、重启后仍为一个出口，inline/local 保留，旧订阅归属消失。 |
| 坏本地成员被 builder 跳过 | `ep02-builder-red.log` | `TestLocalMemberBuildFailureIsAtomicAndRedacted`；构建整次失败且错误不含连接信息。 |
| 本地来源筛选 400、搜索来源名没有结果 | `ep02-filters-red-valid.log` | `TestLocalSourceOverviewFilterAndSearch`；本地主来源和共享本地引用都能筛出，并支持来源名搜索。 |
| 元数据更新被当作空导入拒绝 | `ep02-metadata-red.log` | `TestLocalSourceMetadataOnlyPreservesMembers`；省略 content 保留原成员，显式空白替换仍拒绝。 |
| 替换缺少新增/移除/保留与独立确认 | `ep02-replace-red.log` | 新 helper 的缺失实现 RED；前端差异计算、过期预览和组件接线测试 GREEN。 |
| 普通重载未重新读取免费源 | `ep02-free-reload-red.log` | `TestOrdinaryReloadRefreshesFreeSourcesWithoutApplyingDraft`；刷新免费源并保留未应用草稿。 |
| Register 与 sweep 竞态 | `ep02-race-v2.log` | Register 写 entry.info/governance 时持 entry.mu，锁序保持 manager→entry；最终 race 复验无报告。 |
| 配置快照与 settings 写竞态 | `ep02-settings-race-red.log` | `TestConfigSnapshotCoordinatesSettingsEdits`；公共快照也持 commitMu，已持锁路径用私有 helper 避免自死锁。 |
| 持久化失败和运行恢复失败 | 最终故障注入测试 | `TestLocalApplyPersistenceFailureAndRollback`；通过 lock 路径替换为目录触发真实保存错误，检查旧 runtime 恢复、草稿保留和恢复失败错误。 |

测试夹具问题没有作为业务 RED 计数：过滤测试最初写错字段名；订阅缓存 fixture 最初使用非法 ID；权限故障最初用 chmod，但 user namespace 中权限能力令其不失败；嵌套恢复最初重复注入。均修正夹具，不通过放松生产行为或删除断言规避。

`server_settings_free_proxy_test.go` 追加 cleanup 等待已启动的异步缓存 worker，避免 TempDir 清理期间继续写目录；`manager_portmap_regression_test.go` 更新为新需求要求的相同连接单出口，不再索引已去重的第二个元素。

### 最终验证命令与结果

固定 Go 1.26.10 和 pnpm 10.33.0。Go 环境使用既有隔离依赖目录：

```sh
ROOT=/home/jason/.local/state/easy-proxies-ep02-install-wEP7qu
GO=/home/jason/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.linux-amd64/bin/go
export GOMODCACHE="$ROOT/gomod" GOCACHE="$ROOT/gocache"
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly
```

以下 Go 测试/进程命令均在 `unshare -Urn` 且仅启用 `lo` 的环境执行；没有 DNS 或公网连通权限。

```sh
"$GO" test -tags with_clash_api \
  -skip '^TestFetchECHConfigList_Live$' -count=1 -timeout=240s -p=2 \
  -coverpkg=./internal/config,./internal/boxmgr,./internal/monitor,./internal/builder \
  -coverprofile=/tmp/ep02-cover-final.out ./...

"$GO" test -tags with_clash_api -race -count=1 -timeout=240s -p=2 \
  ./internal/config ./internal/boxmgr ./internal/subscription ./internal/monitor

"$GO" build -tags 'with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api' \
  -o /tmp/ep02-isolated-app ./cmd/easy_proxies
```

- 非 short 全库测试：exit 0，所有有测试包通过，日志 `ep02-all-final.log`。只精确跳过依赖外网 DNS 的 `TestFetchECHConfigList_Live`，未删测试、未把全部集成测试按 short 跳过。
- 四包 race：exit 0，含真实 sing-box HTTP E2E，日志 `ep02-race-final.log`。
- 应用二进制：构建 exit 0，日志 `ep02-build.log`。二进制 SHA-256：`76eab068f97e96996e28a73f8b0a288adb84ca6561517d7c520e6a380818dbcd`。
- `git diff --check`：exit 0。

前端隔离副本 `/tmp/ep02-web-release-x9uimkly` 复用已冻结 node_modules，无安装动作：

```sh
PNPM=/home/jason/.local/state/easy-proxies-ep02-install-wEP7qu/tools/pnpm-10.33.0/package/bin/pnpm.cjs
node "$PNPM" typecheck
node "$PNPM" build
node --test tests/*.test.mjs
```

三步均 exit 0。node:test 共 13/13 通过，其中本地来源 5 项，既有订阅归属 8 项；没有跳过。构建仍有大于 500 kB 的 chunk 提示：echarts 约 1,126.69 kB、主 JS 约 1,202.47 kB（未压缩传输体积）；不是构建失败，本轮未扩展做拆包。

前端验证仅为类型检查、构建、纯函数与组件源码接线断言；未运行浏览器、React 渲染或视觉交互验收。隔离前端产物与 CLI 后端二进制分别验证，不能当作新前端已嵌入并部署的完整发布包。

### 实际进程运行证据

`ep02-real-process.py` 在独立目录真正启动 `easy_proxies -config ...`，通过 HTTP 驱动，不用 fake box，不以 import/单测冒充启动；每次停止均对自己启动的 PID 发送 SIGTERM 并等待退出。

```sh
unshare -Urn -- sh -c 'ip link set lo up && python3 -I /tmp/ep02-real-process.py \
  /tmp/ep02-isolated-app \
  /home/jason/.local/state/easy-proxies-ep02-evidence-20261126/process-final'
```

最终 exit 0，JSON 状态 PASS，验证：

1. 保存草稿后普通重载、停止/重启进程都不自动应用。
2. 显式应用 revision 1 后停止/重启恢复 active；离线成员仍在完整成员清单；共享出口与本地筛选/来源名搜索正确。
3. 不传成员文本完成改名/停用，应用 revision 2；删除最后来源仍待应用；应用 revision 3，停止/重启后来源为空且没有残留草稿。

首轮脚本把既有普通 `POST /api/reload` 成功码误写为 202，实际为 200，保留 `ep02-process-smoke.log`；仅纠正脚本断言后复验通过。新本地来源 apply 的异步成功码仍是 202。

另有 Go 真实 HTTP 生命周期测试覆盖匿名 401/read 403、错误 method、严格 body、stale revision、缺失 ID、settings 提交、替换和应用状态绑定；最终全库与 race 均再次执行。

### 覆盖口径

最终 profile 对四个核心包跨包插桩，重复区块按是否有任一命中合并。7 个新增本地来源生产文件语句覆盖如下：

| 文件 | 覆盖/总语句 | 覆盖率 |
| --- | ---: | ---: |
| `internal/boxmgr/local_sources.go` | 135/151 | 89.40% |
| `internal/config/local_source_protocols.go` | 104/114 | 91.23% |
| `internal/config/local_source_vless.go` | 23/26 | 88.46% |
| `internal/config/local_sources.go` | 92/97 | 94.85% |
| `internal/config/local_sources_state.go` | 181/224 | 80.80% |
| `internal/config/local_sources_validate.go` | 15/18 | 83.33% |
| `internal/monitor/server_local_sources.go` | 136/159 | 85.53% |
| 合计 | 686/789 | 86.95% |

相对冻结的开工工作树计算插入/替换行，再统计与这些行相交的完整 Go cover 区块；因此旧文件区块可能包括未变化的语句，这个指标是「变更相交区块覆盖」，不等同于精确增量语句覆盖。5 个已插桩旧生产文件结果：manager 60/63、builder 6/6、config 92/99、monitor manager 8/8、monitor server 64/65。加上新文件，共 916/1030（88.93%）。

边界必须同时报告：

- `internal/app/app.go`、`internal/outbound/pool/pool.go`、`internal/subscription/manager.go` 未在该 profile 的 coverpkg 中，未量化其变更覆盖；有编译、包测试/调用链验证，不冒充覆盖率已达 80%。
- 文件级达到 80% 不代表每个函数都达到。`atomicConfigWriteWithSync` 最终函数语句覆盖仍为 **61.2%**；已覆盖首/二次目录 Sync、恢复失败保留备份及权限恢复，其他文件系统错误分支没有全部注入。
- 前端 13 项测试未产生语句覆盖率；源码接线测试也不等同于浏览器 E2E。

### 独立 QA 需要继续判断的限制

1. preview 校验基础结构，不穷尽 sing-box 所有 transport/security/Reality 选项；应用时 builder 对本地成员失败整次拒绝。前端替换差异在客户端计算，preview API 不提供服务端 diff。
2. 全成员对话框保留离线成员，但没有逐成员健康列，需到总览查看。共享本地引用已可筛选/搜索；`source_stats` 仍按主 Source 计数，不能把共享引用的统计宣称与筛选完全一致。
3. 原订阅缓存写入失败后的 runtime 事务恢复、本地来源提交之外的全部共享配置并发路径未全面重构；免费缓存抓取入口仍有启动配置引用，运行提交使用 manager 最新快照。
4. 原 admin `/api/nodes` 仍遵循可见 URI 的既有导出契约；Origins 从不输出，新来源列表只给摘要，admin 详情才读成员。不把这次改动声称为全局 URI 隐藏策略。
5. 本轮修复端口「外部占用→来源保留」跳过问题，没有全面改写既有 uint16 分配器所有边界。

---

# 历史阶段1a记录（保留原始结论及当时范围）

## 轮次：2026-11-26 VLESS 结构校验切片（UUID userinfo / 主机 / 端口 / 控制字符）

### RED（新增测试先行失败）

新文件 `internal/config/local_source_vless_test.go`（合成数据，UUID 恒为 `11111111-1111-4111-8111-111111111111`）：表驱动 16 例 + 整次导入失败 1 例。命令（允许清单精确形式）：

```
go test -count=1 -timeout=180s -p=2 ./internal/config
```

结果：73 passed, 10 failed（既有 73 项全部保持通过，10 项失败全部来自新 VLESS 测试）

失败项（现有 `validateLocalSourceURI` 仅查 host 非空，均被接受为节点）：`missing userinfo`、`empty uuid`、`non-uuid userinfo`、`uuid with password`、`port zero`、`port 65536`、`truncated uuid`、`wrong hyphen positions` 共 8 例单测，加 `TestParseLocalSourceNodesVLESSStructuralValidation` 整体与 `TestParseLocalSourceNodesVLESSWholeImportFailsOnBadLine`（line 2 坏行未被报，导入未失败）。有效 RED 确认。

### GREEN（最小实现）

- 新文件 `internal/config/local_source_vless.go`（仅标准库 fmt/net/url/strconv）：
  - `validateVLESSURI(parsed *url.URL, lineNo int)`：userinfo 必须存在且为裸 UUID（有密码即拒）；显式端口必须可解析且在 1..65535（省略端口不拒，当前 builder 缺省 443）；userinfo/host/query/fragment 各部件不得含 C0 控制字符或 DEL。错误只含原始行号与固定原因，绝不含 URI 或凭据。
  - `isCanonicalUUID`：36 字符、连字符在 8/13/18/23、其余为 hex（大小写均接受，共 16 字节 hex）。
  - `containsDangerousControlChars`：< 0x20 或 == 0x7f。
- `internal/config/local_sources.go` `validateLocalSourceURI`：先取 scheme（复用 `localSourceURIScheme`，未知协议仍走原固定脱敏路径），scheme 为 vless 时调用 `validateVLESSURI`。其他协议（VMess/SS/HY2 等）行为不变。

命令与结果：

```
go test -count=1 -timeout=180s -p=2 ./internal/config        # 83 passed（10 项 RED 全部转绿）
gofmt -w internal/config                                      # 完成
go test -count=1 -timeout=180s -p=2 ./internal/config        # 83 passed（gofmt 后复跑）
go test -count=1 -timeout=180s -p=2 -race ./internal/config  # 83 passed
go test -count=1 -timeout=180s -p=2 -cover ./internal/config # 83 passed
```

### 覆盖说明

`go test -cover` 输出为包级百分比，未提取仅新增函数（`validateVLESSURI`、`isCanonicalUUID`、`containsDangerousControlChars`）的语句覆盖明细；不冒充为新增逻辑覆盖率。IPv6 host（`[::1]:443`）与省略端口两例为通过路径断言。

### 范围与边界

- 新增 `internal/config/local_source_vless_test.go`、`internal/config/local_source_vless.go`，编辑 `internal/config/local_sources.go` 及本文件；保留全部 EP-01 与此前 EP-02 改动，未触碰 builder、config.go、全局 NodeKey。
- 未提交、未联网、未绕过任何权限；无权限拒绝发生。
- 已知缺口：本切片仅 VLESS 结构校验。VMess / SS / HY2 / 其他协议的逐字段严格校验、来源组合、草稿持久化整合均未完成，留待后续轮次；不将本切片宣称整个阶段或功能完成。

---

## 轮次：2026-11-26 修复独立审查问题3/4（大小写冲突整次拒绝 + 未知协议固定脱敏原因）

### RED（复现既有失败，run-3320b7f5 已验证）

命令（允许清单精确形式）：

```
go test -count=1 -timeout=180s -p=2 ./internal/config
```

结果：62 passed, 3 failed（与控制文件记录完全一致，本轮复现确认）

1. `TestParseLocalSourceNodesRejectsCredentialCaseCollision` — `credential case collision must reject the whole import, got nodes=1`（同键合成密码仅大小写不同，被旧小写去重静默合并）。
2. `TestParseLocalSourceNodesRejectsHostCaseKeyConflict` — `host-case key conflict must be rejected, got nodes=1`（主机 Node.test/NODE.TEST 同键被静默合并）。
3. `TestParseLocalSourceNodesUnknownSchemeDoesNotEchoPrefix` — `unknown-scheme error echoes input prefix "alice"`（错误回显 `://` 前任意前缀，含潜在凭据）。

### GREEN（最小实现）

`internal/config/local_sources.go`：

- `localSourceURIScheme`：未知协议错误改为固定安全原因 `unsupported protocol`，不再用 `%q` 回显 scheme 前缀（该前缀用户可控、可能含凭据）。
- `dedupeLocalSourceNodes` 签名改为返回 `([]LocalSourceNode, []LocalSourceDuplicate, error)`：`seen` 由"仅小写 key"改为 `map[key]connectionText`，保存去 fragment 后的原始连接文本（保留大小写）。连接文本完全相同 → 按重复去重并报告原始输入行号；key 相同但连接文本不同 → 整次拒绝，报脱敏冲突错误（含原始行号，不含凭据/URI）。
- `ParseLocalSourceNodes`：dedupe 出错时返回空 `LocalSourceParseResult{}` 与该错误，不丢弃部分结果给调用方。
- 全局 `canonicalNodeURI` / `NodeKey` 行为未改（config.go 未触碰）。

命令与结果：

```
go test -count=1 -timeout=180s -p=2 ./internal/config        # 65 passed（RED 3 项转绿）
go test -count=1 -timeout=180s -p=2 -race ./internal/config  # 65 passed
go test -count=1 -timeout=180s -p=2 -cover ./internal/config # 65 passed
gofmt -w internal/config                                      # 完成后复跑普通测试 65 passed
```

### 覆盖说明

`go test -cover` 输出为包级百分比，未提取仅新增/修改函数（`dedupeLocalSourceNodes` 冲突分支、`localSourceURIScheme` 固定原因路径）的语句覆盖明细；不冒充为新增逻辑覆盖率。

### 范围与边界

- 仅编辑 `internal/config/local_sources.go` 及本文件；测试沿用上轮已验证的回归断言，未修改任何测试期望。未触碰 EP-01 dirty 文件、config.go、builder、全局 NodeKey。
- 未提交、未联网、未绕过任何权限；无权限拒绝发生。
- 阶段1a剩余缺口（独立审查问题1/2/5 已在上轮解决，问题清单中严格协议校验逐字段验证仍属后续轮次）。

---

## 轮次：2026-11-25 修复独立审查问题5/6（行号保留 + 总量测试panic）

### RED（复现既有失败）

命令（允许清单精确形式）：

```
go test -count=1 -timeout=180s -p=2 ./internal/config
```

结果：44 passed, 2 failed（与上一轮 run-c6d08feb 日志一致，本轮复现确认）

1. `TestParseLocalSourceNodesDedupsWithinSource` — `duplicates = [{Line:2 …}]`，期望 Line=3（重复报告用过滤后节点序号，丢失原始输入行号）。
2. `TestValidateLocalSourcesLimits` — `panic: runtime error: makeslice: len out of range`（测试自身算术缺陷：`5000 - 500*49 + 1 = -4500` 导致负数 make）。

补充 RED（修复实现前新增测试先行失败）：

```
go test -count=1 -timeout=180s -p=2 ./internal/config
```

结果：60 passed, 2 failed

- 新增 `TestParseLocalSourceNodesDuplicateLineNumbersSkipCommentsAndBlanks`（BOM/CRLF/空行/注释混排，重复在原始第6、8行）：`duplicate lines = 2,3, want original input lines 6,8` — 有效功能缺失 RED。
- 原 dedup 测试仍 Line=2 期望 3。

测试侧修正（非弱化）：总量测试按审查指示改为 10 源 × 500 条 = 5000 边界通过 + 追加 1 条禁用源 → 5001 整体拒绝（禁用源计入总量）；替换原 panic 算术。

### GREEN（最小实现）

`internal/config/local_sources.go`：

- `ParseLocalSourceNodes` 维护 `nodeLines []int` 记录每个节点的原始输入行号（注释/空行计入）。
- `dedupeLocalSourceNodes(nodes, nodeLines)` 签名增加行号参数；重复报告 `Line` 取原始输入行号，不再用过滤后序号。
- 去重语义不变：`canonicalNodeURI`（去 fragment + 小写）相同即重复，保留首次出现，报告不含凭据。

命令与结果：

```
go test -count=1 -timeout=180s -p=2 ./internal/config        # 62 passed
go test -count=1 -timeout=180s -p=2 -race ./internal/config  # 62 passed
go test -count=1 -timeout=180s -p=2 -cover ./internal/config # 62 passed
gofmt -w internal/config                                      # 完成后复跑普通测试 62 passed
```

### 覆盖说明

`go test -cover` 输出为包级百分比，本轮未提取仅新增函数（`dedupeLocalSourceNodes`、`ParseLocalSourceNodes` 行号路径）的语句覆盖明细；按控制文件要求不冒充为新增逻辑覆盖率。已知未覆盖/未做项：本轮范围仅问题5/6，问题1–4（BOM字符核对、严格协议校验、大小写凭据冲突、未知协议脱敏）留待下一轮，阶段1a未全部完成。

### 范围与边界

- 仅编辑 `internal/config/local_sources.go`、`internal/config/local_sources_test.go` 及本文件；未触碰 EP-01 dirty 文件、builder、全局 NodeKey。
- 未提交、未联网、未绕过任何权限；无权限拒绝发生。
