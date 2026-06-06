# Free Proxy Source UI Taste Specification

> 基于 `taste-skill` 的前端方案细化。目标不是做炫酷页面，而是把“添加免费源 → 自动筛选 → 只展示可用代理”的复杂流程做成清晰、可信、可控的设置体验。

## Design Read

Reading this as: 代理管理后台的设置页增强，为技术用户/本地运维使用，偏“可信、紧凑、工程化”的产品 UI，保留现有 Easy Proxies 控制台语言，避免营销页式视觉。

## Dials

```text
DESIGN_VARIANCE: 4
MOTION_INTENSITY: 2
VISUAL_DENSITY: 7
```

- **Variance 4**：需要稳，不要花哨；允许轻微结构变化避免表单堆砌。
- **Motion 2**：只保留保存、测试、展开折叠的轻反馈。
- **Density 7**：设置页信息密度高，但要靠分组、层级和辅助文案降低认知成本。

## 核心体验目标

用户心智应该是：

```text
我添加免费代理源
→ 系统先把它当候选源
→ 自动抓取、去重、快速筛选
→ 只有达到最低等级的代理进入节点总览
→ 我可以在节点总览用 来源=免费源 / 状态=可用 查看结果
```

不是：

```text
我添加源
→ 十万条垃圾代理直接挤进节点列表
→ 我再自己手动筛
```

## 信息架构

设置页侧边导航调整为：

```text
01 订阅
02 免费代理源
03 默认代理池
04 多端口
05 地区 / Android
06 质量检测
07 管理与日志
```

`免费代理源` 区块应位于 `订阅` 后面，因为两者都是“节点来源”。

## 页面结构

### 1. Section Header

标题：

```text
免费代理源
```

副标题：

```text
把公开代理列表作为候选源。保存并重载后，系统会先抓取、去重和预筛，只让通过最低等级的代理进入运行节点。
```

右侧主操作：

```text
[新增源]
```

不要在这个 section header 放第二个“保存”按钮，避免和页面顶部“保存设置”产生重复 CTA 意图。保存仍由页面顶部统一处理。

### 2. Summary Strip

使用 4 个紧凑状态块，和现有 settings status grid 对齐：

| Label | Value | 说明 |
|---|---:|---|
| 源数量 | `freeSources.length` | 当前配置的候选源数 |
| 启用源 | `enabled count` | `enabled !== false` 的源 |
| 最大入池 | `free_proxy_max_nodes` | 筛选后最多加入运行节点数 |
| 最低等级 | `min_tier` | 当前预筛门槛 |

注意：这些是“配置摘要”，不是实时筛选结果。不要伪造“通过率”或“可用数”，除非后端提供真实 source-level summary。

### 3. Auto Filter Controls

这一块是最关键的“智能处理”开关。结构建议：

```text
[✓] 启用自动筛选

最低等级        [HTTP 基础可用 | 普通 Web 可用]
入池上限（0=不限） [0]
最大候选数      [3000]
筛选并发        [200]
筛选超时        [2s]
```

#### 字段文案

- `启用自动筛选`
  - helper: `开启后，免费源不会直接进入运行池；只有通过预筛的代理会展示。`
- `最低等级`
  - `http_basic`: `HTTP 基础可用，仅要求 HTTP 204 探针通过。`
  - `simple_web`: `普通 Web 可用，要求 HTTP 204 + HTTPS example 通过。推荐。`
- `最大入池数`
  - helper: `最终进入运行节点的免费代理上限。建议 50-200。`
- `最大候选数`
  - helper: `每次 reload 最多预筛多少候选。源很大时可防止耗时过长。`
- `筛选并发`
  - helper: `使用有界 worker。建议 80-300；过高会占用网络和文件描述符。`
- `筛选超时`
  - helper: `单个代理探针超时，例如 2s。免费代理波动大，不建议太长。`

#### 默认值

```ts
free_proxy_filter: {
  enabled: true,
  min_tier: 'simple_web',
  workers: 200,
  timeout: '2s',
  max_candidates: 0,
  probes: {
    http: 'http://cp.cloudflare.com/generate_204',
    https: 'https://example.com/'
  }
}
free_proxy_max_nodes: 0
```

### 4. Advanced Probe Controls

探针配置不要默认铺开，否则设置页显得像调试工具。使用 `<details>`：

```text
▸ 高级探针配置
  HTTP 探针    http://cp.cloudflare.com/generate_204
  HTTPS 探针   https://example.com/
```

说明文案：

```text
默认探针适合大多数场景。只有当你的网络环境无法访问默认目标时再修改。
```

### 5. Source List

不要用超宽传统表格，也不要把每个源做成巨大卡片。推荐“可编辑行 + 移动端折叠”的结构。

桌面布局：

```text
启用  名称              URL / 文件路径                          协议     上限     操作
[✓]   thespeedx-http    https://raw.githubusercontent...        http     1000    删除
[✓]   proxifly-socks5   https://raw.githubusercontent...        socks5   500     删除
```

字段：

| 字段 | 控件 | 默认值 | 规则 |
|---|---|---|---|
| enabled | checkbox | true | 关闭后保留配置但不抓取 |
| name | input | `new-free-source` | 必填或可由 URL 派生 |
| url/file | input | 空 | 输入 http(s) 开头则保存为 `url`，否则保存为 `file` |
| default_scheme | select | `http` | `http` / `socks5` |
| max_nodes | number | 0 | 每个源解析上限；0 表示全量解析 |
| 删除 | danger button | - | 只删除源配置，不影响已运行节点直到 reload |

### 6. Empty / Loading / Error States

`taste-skill` 强调不要只有成功态。这里至少需要：

#### Empty

```text
暂无免费代理源
添加 GitHub raw、远程文本列表或本地文件。保存并重载后，系统会自动筛选，通过后才进入节点总览。
[新增源]
```

#### Loading

设置页加载时：

- summary strip 用骨架块或显示 `加载中...`。
- source list 不显示空态，避免误导用户以为配置为空。

#### Save Error

Toast 可以保留，但字段错误应尽量 inline：

- URL/file 都为空：`请填写 URL 或文件路径。`
- `workers` 超出范围：`并发建议 1-800。`
- `timeout` 格式错误：`请输入 Go duration，例如 2s、1500ms。`

### 7. Save / Reload Flow

保存后 API 返回 `need_reload: true`。UI 应显示一个 section-level note 或 toast：

```text
设置已保存。需要重载核心后，新的免费源和筛选策略才会生效。
[立即重载]
```

如果已有 `reloadCore()` API，应在设置页顶部或免费源区块内提供轻量按钮。不要自动 reload，避免用户还在继续编辑其他设置。

### 8. Node Overview Integration

节点总览页已经有：

```text
来源 = 免费源
状态 = 可用 / 不可用 / 未检测
```

新增体验建议：

- 免费源 summary 卡点击后跳转/设置筛选：`source=free_proxy`。
- 如果 free_proxy source_stats 为 0，但 settings 有启用源，显示提示：`免费源已配置，但当前没有通过筛选的运行节点。请检查筛选等级或源质量。`

不要在节点总览重复配置免费源，避免配置入口分散。

## Component Design Rules

### Label Rules

- 所有输入必须 label above input。
- placeholder 只做示例，不充当 label。
- helper text 使用 muted 小字，放在控件下方或 group note 中。

### Button Rules

- 主操作只有一个：页面顶部 `保存设置`。
- section 级新增：`新增源`。
- 危险操作：`删除`，使用 danger style。
- 不使用两个含义相同的按钮，例如同时出现 `保存设置` 和 `保存免费源`。

### Density Rules

- Summary 用 4 个块。
- 策略控件用 2-3 列 grid。
- 探针配置默认折叠。
- 源行桌面一行展示，移动端变成 stacked form。

### Color / Shape Rules

- 使用现有 Easy Proxies token：`--panel`, `--border`, `--primary`, `--muted`。
- 不引入新 accent 色。
- 不使用紫色/蓝色 glow。
- radius 继续沿用现有 `var(--radius)` / input 11-12px 体系。

## Implementation Tasks for UI Refinement

### Task A: Replace rough inline source row with semantic CSS classes

Current rough implementation uses inline style on `.subscription-item` for grid columns. Replace with classes:

```tsx
<div className="free-source-row">
```

CSS target:

```css
.free-source-row {
  display: grid;
  grid-template-columns: 34px minmax(120px, .8fr) minmax(260px, 1.7fr) minmax(96px, .45fr) minmax(88px, .4fr) auto;
  gap: 10px;
  align-items: start;
}

@media (max-width: 900px) {
  .free-source-row {
    grid-template-columns: 1fr;
  }
}
```

### Task B: Move probes into advanced details

Current rough implementation always shows HTTP/HTTPS probes. Replace with:

```tsx
<details className="raw-editor free-proxy-advanced">
  <summary>高级探针配置</summary>
  ...
</details>
```

### Task C: Add inline validation helpers

Before save, compute:

```ts
const freeProxyIssues = freeSources
  .map((src, idx) => ({ idx, issue: !src.url && !src.file ? '请填写 URL 或文件路径' : '' }))
  .filter(x => x.issue)
```

At minimum show a warning note in section. Later can block save.

### Task D: Add reload prompt after save

When save succeeds and response has `need_reload`, show a persistent note or toast with reload action.

### Task E: Add source presets later, not now

Possible presets:

- TheSpeedX HTTP
- TheSpeedX SOCKS5
- proxifly HTTP
- proxifly SOCKS5

But do not ship presets in the first UI pass unless backend has enough guardrails, because public sources can be noisy.

## Acceptance Checklist

- [ ] `free_proxy_sources` editable in Settings UI.
- [ ] Auto-filter controls are visible and understandable.
- [ ] HTTP/HTTPS probe inputs are hidden under advanced section.
- [ ] Source rows are readable at desktop and usable on mobile.
- [ ] Empty state explains what to do.
- [ ] Save does not duplicate CTA intent.
- [ ] Settings API persists sources/filter/max nodes.
- [ ] Reload applies filter before nodes enter runtime list.
- [ ] Node Overview can filter `source=free_proxy`.
- [ ] `pnpm -C web build` passes.
- [ ] `go test ./internal/nodesource ./internal/config ./internal/monitor` passes.
