# Proxy Tiering Rules / 代理分级规则

目标：把免费代理从“是否可用”升级为“适合什么用途、风险多高、是否值得进入主服务”的可解释分级体系。

## 设计原则

1. **先门槛，后打分**：高等级必须先通过硬性门槛，不能靠单项高分补偿关键失败。
2. **分用途建池**：HTTP-only、普通 Web、Cloudflare、AI 服务、低风险出口不是同一类需求。
3. **实时性优先**：免费代理波动大，等级必须带 `checked_at`、`ttl`、`failure_streak`。
4. **控制并发**：所有检测必须使用 bounded worker queue，不允许一次性创建几千/几万个 subprocess/task。
5. **去重出口**：大量代理可能共享同一个出口 IP；高质量池按唯一出口 IP 控制集中度。

## 检测维度

| 维度 | 字段建议 | 说明 |
|---|---|---|
| 基础连通 | `quick.ok`, `quick.failure_reason` | TCP/代理握手/HTTP 204 快速预筛 |
| 协议能力 | `capabilities[]` | HTTP GET/HEAD/POST/Range、CONNECT、HTTPS、SOCKS5 |
| 出口确认 | `exit.ip`, `exit.country`, `exit.asn` | 通过 ipify/httpbin/cf trace 确认真实出口 |
| 延迟 | `latency.p50_ms`, `latency.p90_ms` | 不能只看单次延迟 |
| 稳定性 | `stability.success_rate`, `failure_streak` | 多轮检测后的稳定程度 |
| 风控信誉 | `reputation.risk_level`, `risk_score` | IP 类型、代理/机房/mobile/hosting 标记 |
| Cloudflare | `cf.score`, `cf.level`, `cf.trace_ok` | CF 204/trace/challenge 兼容性 |
| 目标兼容 | `site_matrix` | 常见网站、严格风控网站、AI 服务网站 |
| 来源质量 | `source.exit_rate`, `source.last_success_rate` | 源级别权重，降低垃圾源噪声 |
| 出口集中度 | `exit.dup_count` | 同一出口 IP 对应代理数量，过高降级 |

## 能力标签

每个代理可以有多个 capability 标签：

| 标签 | 判定 |
|---|---|
| `socket_reachable` | quick socket/握手成功 |
| `http_basic` | HTTP 204 + HTTP GET 正常 |
| `http_methods` | GET + HEAD + POST + Range 正常 |
| `connect80` | HTTP proxy 支持 CONNECT 到 80 或等价 tunnel |
| `https_basic` | HTTPS example/ipify/httpbin 至少 2/3 成功 |
| `https_cf` | Cloudflare 204 + trace 成功 |
| `general_web` | HTTP methods + HTTPS basic + GitHub/example 等通用站点成功 |
| `strict_web` | Cloudflare challenge/严格站点矩阵达到阈值 |
| `ai_web` | AI 服务站点矩阵达到阈值，例如 OpenAI/Claude/Gemini 等 |
| `low_risk_exit` | reputation normal/mobile low risk，且非明显 proxy/hosting 风险 |

## 等级定义

### T0 Reject / 丢弃

硬性条件任一满足：

- quick 失败；
- 无法确认出口 IP；
- 需要认证但没有凭证，例如 HTTP 407；
- 连续 `failure_streak >= 3`；
- 最近检测 TTL 过期且复检失败。

用途：不进入运行池，只保留失败原因用于源质量统计。

### T1 Socket-only / 仅可抢救

门槛：

- `socket_reachable=true`；
- 但 HTTP 204、出口 IP 或 HTTPS 检测不完整。

用途：

- 暂不进入默认服务；
- 可作为低频复检池；
- 可尝试协议纠偏：HTTP/SOCKS5/SOCKS4 猜测、CONNECT-only、HTTP-only。

### T2 HTTP-only / HTTP 采集池

门槛：

- `http_basic=true`；
- `http_methods` 至少 GET/HEAD 通过；
- HTTPS 不稳定或不支持 CONNECT。

用途：

- 只跑明文 HTTP、简单 204、低价值网页采集；
- 不能作为通用浏览器/AI/HTTPS 出口。

建议 TTL：30-60 分钟。

### T3 Simple Web / 普通 Web 池

门槛：

- `http_methods=true`；
- `https_basic=true`；
- 出口 IP 已确认；
- `latency.p90_ms <= 5000`；
- `failure_streak == 0`。

用途：

- 普通 HTTPS 访问；
- 低强度 API 请求；
- 可进入隔离服务观察。

建议 TTL：15-30 分钟。

### T4 General Web Recommended / 推荐通用池

门槛：

- 满足 T3；
- `general_web=true`；
- `cf.score >= 60` 或 `cf.level in [good, excellent]`；
- `reputation.risk_level in [low, medium]`，且非明显高风险代理；
- 同一 `exit.ip` 在推荐池中数量不超过 3。

用途：

- 默认可用池；
- WebUI 展示优先；
- 可进入主服务，但需要容量上限。

建议 TTL：10-20 分钟。

### T5 Strict / AI Candidate / 高质量候选池

门槛：

- 满足 T4；
- `cf.score >= 80`；
- `reputation.risk_level=low`；
- `latency.p90_ms <= 3000`；
- 严格站点矩阵成功率 `>= 80%`；
- AI 站点矩阵成功率：
  - `>= 80%` 标记 `ai_web`；
  - `50%-79%` 标记 `ai_partial`；
- 最近两轮检测均成功。

用途：

- 严格风控站点；
- AI 服务候选出口；
- 用户手动优先选择。

建议 TTL：5-15 分钟。

## 分数模型

最终等级由硬门槛决定，分数只在同等级内排序。

```text
base = 0
+ quick_ok                         10
+ exit_ip_confirmed                15
+ http_methods                     15
+ https_basic                      15
+ general_web                      10
+ cf_score * 0.20                  0-20
+ reputation_bonus                 -30..15
+ latency_bonus                    -20..10
+ stability_bonus                  -30..15
+ source_quality_bonus             -10..10
- exit_dup_penalty                 0..20
```

### 风险分

| 条件 | 加减分 |
|---|---:|
| normal low risk | +15 |
| mobile low risk | +12 |
| medium risk | -10 |
| hosting/proxy risk | -20 |
| high risk | -30 |

### 延迟分

| p90 延迟 | 加减分 |
|---|---:|
| <= 1000ms | +10 |
| <= 3000ms | +5 |
| <= 5000ms | 0 |
| > 5000ms | -10 |
| timeout/error | -20 |

### 稳定性分

| 最近 N 轮成功率 | 加减分 |
|---|---:|
| >= 95% | +15 |
| >= 80% | +8 |
| >= 60% | 0 |
| < 60% | -20 |
| failure_streak >= 2 | -30 |

## 稳定 API 字段决策

这些值作为后续实现的稳定 JSON 合约。

### Tier JSON 值

| 等级 | JSON 值 | 默认池 | 说明 |
|---|---|---|---|
| T0 | `reject` | `reject_pool` | 不可用或硬门槛失败 |
| T1 | `rescue` | `rescue_pool` | 可抢救但不可默认使用 |
| T2 | `http_only` | `http_pool` | 只适合 HTTP-only 任务 |
| T3 | `simple_web` | `web_pool` | 普通 Web/HTTPS 可用 |
| T4 | `recommended` | `recommended_pool` | 默认推荐池 |
| T5 | `premium` | `strict_pool` 或 `ai_pool` | 严格风控/AI 候选 |

### Result 新字段

```json
{
  "tier": "recommended",
  "tier_score": 86,
  "pool": "recommended_pool",
  "capabilities": ["http_methods", "https_basic", "https_cf", "general_web", "low_risk_exit"],
  "tier_reasons": ["quick_ok", "exit_ip_confirmed", "cf_good", "risk_low"]
}
```

字段规则：

- `tier`：单值，表示最终等级。
- `tier_score`：0-100，只用于同等级内排序。
- `pool`：单值，表示默认进入哪个池。
- `capabilities`：多值，表示能力标签。
- `tier_reasons`：多值，解释为什么进入该等级或为什么被拒绝。

### API 过滤语义

结果分页接口后续支持：

```text
GET /api/quality/jobs/{id}/results?tier=recommended&page=1&page_size=100
GET /api/quality/jobs/{id}/results?pool=ai_pool&page=1&page_size=100
GET /api/quality/jobs/{id}/results?capability=https_cf&page=1&page_size=100
```

语义：

- `tier` 是精确匹配。
- `pool` 是精确匹配。
- `capability` 是包含匹配。
- 多个过滤条件之间是 AND。
- 默认不带过滤参数时保持旧行为，仍按 `target_index` 稳定分页。

### 兼容性决策

- 旧 API 客户端忽略新增字段即可，不破坏现有响应。
- 现有 `recommend: true/false` 保留，规则改为 `tier in [recommended, premium]`。
- 现有 `final_score` 保留，后续可映射为 `tier_score`，但前端优先显示 `tier_score`。
- 没有足够字段判断时，宁可降级，不做乐观推荐。

## 当前扫描结果映射

基于 `/tmp/easy-proxies-current-free-scan` 的现有结果：

| 阶段 | 数量 | 建议等级 |
|---|---:|---|
| 原始候选 | 111,319 | 未分级 |
| quick_ok | 29,831 | T1 起步 |
| exit_ip_ok | 205 | T2/T3 候选 |
| normal_low_risk | 34 | T4 候选 |
| mobile_low_risk | 78 | T4 候选 |
| risk_proxy_or_hosting | 93 | 最高 T3，默认不推荐 |

优先导入策略：

```text
T4 候选 = normal_low_risk + mobile_low_risk = 112 个
```

不建议直接导入：

- 111,319 原始候选；
- 29,831 quick_ok；
- 93 个 risk_proxy_or_hosting 默认只做备用/低优先级。

## 池化策略

| 池 | 来源等级 | 服务用途 |
|---|---|---|
| `reject_pool` | T0 | 不导入，只统计 |
| `rescue_pool` | T1 | 低频复检、协议纠偏 |
| `http_pool` | T2 | HTTP-only 任务 |
| `web_pool` | T3 | 普通 Web 任务 |
| `recommended_pool` | T4 | 默认推荐、可进入主服务 |
| `strict_pool` | T5 strict | 严格风控网站 |
| `ai_pool` | T5 ai_web/ai_partial | AI 服务网站 |

## 运行保护规则

1. quick 预筛使用 socket/Go HTTP client，不使用大规模 curl subprocess。
2. curl 仅用于少量深度兼容测试，concurrency 默认 40，上限 80。
3. worker queue 必须边生产边消费，不允许一次性创建所有 asyncio tasks。
4. 每个代理每轮检测最多一个 active probe。
5. 6000+ 全量检测必须走后台 job + 分页结果。
6. 主服务导入前先在隔离配置验证。
