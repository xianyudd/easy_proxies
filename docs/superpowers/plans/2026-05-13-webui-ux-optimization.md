# WebUI UX Optimization Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 easy_proxies WebUI 从“功能堆叠型后台”优化成“代理平台式操作台”，让用户能快速判断节点质量、提取可用代理、复制导入格式，并减少误操作。

**Architecture:** 保持现有单文件 WebUI 架构 `internal/monitor/assets/index.html`，优先做局部组件化 CSS/JS 重构，不引入前端构建链。后端只在现有 API 无法支撑 UI 状态时补最小接口或字段，避免大改服务结构。

**Tech Stack:** Go management API + embedded static HTML/CSS/Vanilla JS + existing local APIs (`/api/status`, `/api/extractor`, `/api/cloudflare/*`, `/api/reputation/*`, `/api/settings`)。

---

## Current Problems

1. **导航和信息架构不清晰**
   - 现在页面包含监控、节点、代理提取、IP 信誉、CF 评分、日志、设置，但缺少统一任务流。
   - 用户常见目标是“拿一条能用的代理”，入口应该更突出，而不是埋在功能页里。

2. **代理提取页功能多但操作路径偏乱**
   - 快捷卡片、参数区、结果区已经存在，但视觉优先级不够明确。
   - “地区 / 模式 / 格式 / 数量 / 真实密码”之间的依赖关系没有强提示。
   - Android、GeoIP、multi-port 混在同一提取流程里，用户容易选错格式。

3. **质量检测结果和提取动作没有闭环**
   - CF 评分、IP 信誉、节点健康状态分散在不同页。
   - 用户看到“JP 优质节点”后，不能直接一键提取该节点代理。

4. **商业代理平台感不足**
   - 缺少“推荐场景卡片”：指纹浏览器、Android、curl/Python、地区池。
   - 缺少“优质节点榜单”、“最近可用出口”、“失败原因聚合”。
   - 结果区更像文本工具，不像代理平台的提取面板。

5. **敏感信息展示逻辑需要更清楚**
   - 当前需求是必须显示真实密码，但 UI 仍应明确“本地页面 / 显示真实凭据”。
   - 最终输出、日志、文档不能打印真实订阅 URL 或凭据。

---

## File Structure

### Modify
- `internal/monitor/assets/index.html`
  - WebUI 结构、CSS、JS 全部在这里。
  - 本计划只做局部重排和小函数拆分，不迁移到框架。

- `internal/monitor/server.go`
  - 仅在需要给 UI 补充聚合数据时修改。
  - 优先复用现有 API，不新增重复接口。

- `PROXY_EXTRACTOR.md`
  - 更新新的提取页使用说明。

- `CF_SCORE.md`
  - 更新 CF 评分页面和“一键复制代理”说明。

- `IP_REPUTATION.md`
  - 如果信誉页接入提取动作，补充说明。

### Optional Create
- `docs/ui-review-checklist.md`
  - 人工验收清单：页面是否能用、复制格式是否正确、是否泄露敏感信息。

---

## UX Direction

采用“工业化代理控制台”风格：
- 深色为主，强调数据密度和操作效率。
- 卡片更大、更少、更明确，每张卡只承载一个任务。
- 主色用于“提取/复制”，弱色用于“检测/查看”。
- 页面顶部给出当前服务状态、可用节点数、推荐入口。
- 每个结果都提供“一键复制”和“复制导入命令”。

---

## Chunk 1: 页面信息架构重排

### Task 1: 调整导航顺序和页面命名

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 让最常用功能更靠前：代理提取、质量检测、监控、节点配置、日志、设置。

- [ ] Step 1: 修改侧边栏排序
  - 推荐顺序：
    1. 代理提取
    2. 质量检测
    3. 监控看板
    4. 节点配置
    5. Android 代理
    6. 控制台日志
    7. 系统设置

- [ ] Step 2: 合并“IP 信誉”和“CF 评分”为“质量检测”一级页
  - 页面内部用 tab 或 segmented control 分为：
    - 节点评分
    - CF 兼容
    - IP 信誉
  - 如果本阶段不做合并，至少在两个页面顶部互相提供跳转按钮。

- [ ] Step 3: 默认打开代理提取页
  - 用户打开 WebUI 后直接看到可复制代理入口。
  - 监控看板保留，但不作为默认页。

- [ ] Step 4: 验证
  ```bash
  curl --noproxy '*' -s http://127.0.0.1:9091/ | grep -n '代理提取\|质量检测\|监控看板'
  ```

---

## Chunk 2: 代理提取页改成平台式操作台

### Task 2: 新增“场景入口卡片”

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 用户不需要理解技术模式，先按使用场景选择。

- [ ] Step 1: 在代理提取页顶部新增 4 个大卡片
  1. **指纹浏览器代理**
     - 默认：multi-port
     - 格式：`host:port:username:password`
     - 数量：10
     - 动作：随机 10 条、复制一条、下载 TXT

  2. **地区池代理**
     - 默认：geoip
     - 格式：完整 URL
     - 地区：美国、日本、香港、新加坡、英国、德国、印度、阿联酋、瑞士、澳大利亚
     - 动作：复制 US、复制 JP、更多地区

  3. **脚本 / curl / Python**
     - 默认：pool endpoint
     - 格式：`http://username:password@host:port`
     - 动作：复制代理 URL、复制 curl 命令、复制 Python requests 示例

  4. **Android 全局代理**
     - 默认：android no-auth port
     - 格式：`host:port` 或 adb 命令
     - 动作：复制 adb reverse 命令、复制设置代理命令、清除代理命令

- [ ] Step 2: 每张卡片显示“推荐用途 / 输出格式 / 是否固定端口 / 是否地区池”

- [ ] Step 3: 卡片按钮只调用现有 `runQuickExtract()` / `quickCopy()`，不新增重复逻辑。

- [ ] Step 4: 验证
  - 点击每张卡片的主按钮。
  - 确认结果区有输出。
  - 确认真实密码显示，但终端和日志不打印真实密码。

### Task 3: 高级参数区改成“抽屉/折叠”

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 默认降低复杂度，高级用户仍能完整控制。

- [ ] Step 1: 把高级参数默认折叠为“自定义提取参数”。
- [ ] Step 2: 展开后显示区域、模式、格式、数量、真实密码开关。
- [ ] Step 3: 根据模式动态禁用不适用格式：
  - geoip 带 `/us/` 路径，只允许完整 URL / curl / JSON。
  - android 只允许 host:port / http://host:port / adb_command。
  - multi-port 允许所有 host-port 类导入格式。
- [ ] Step 4: 禁用时显示原因，不要静默改值。

### Task 4: 结果区升级为“代理列表卡片 + 文本输出”

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 同时满足单条复制和批量导入。

- [ ] Step 1: 每条代理生成一个结果卡片，包含：
  - 地区
  - 节点名 / remark
  - 本地端口
  - 格式
  - 复制按钮
  - curl 按钮（适用时）

- [ ] Step 2: 文本框保留在卡片下方，用于批量复制。

- [ ] Step 3: 增加顶部结果摘要：
  - 输出数量
  - 模式
  - 地区
  - 格式
  - 是否真实密码

- [ ] Step 4: 下载 TXT 使用当前文本框内容，不重新请求接口。

---

## Chunk 3: 质量检测和代理提取打通

### Task 5: CF 评分表增加“一键提取该节点”

**Files:**
- Modify: `internal/monitor/assets/index.html`
- Optional Modify: `internal/monitor/server.go`

**目标:** 从评分结果直接拿代理。

- [ ] Step 1: 在 CF 评分表操作列保留：
  - 复制代理
  - 复制 curl
  - 提取到代理页

- [ ] Step 2: “提取到代理页”行为：
  - 切换到代理提取页。
  - 设置模式为 multi-port。
  - 设置地区为节点地区。
  - 设置数量为 1。
  - 结果区输出该端口代理。

- [ ] Step 3: 过滤器补充：
  - 只看优秀
  - 只看可用
  - 只看指定国家
  - 延迟小于 1000ms

- [ ] Step 4: 验证
  - 完整扫描后，选一个 JP 优秀节点，点击复制代理。
  - 用 `curl -x` 验证能访问 `https://api.ipify.org`。

### Task 6: IP 信誉页增加“低风险节点提取”

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 把信誉检测结果转成可用代理清单。

- [ ] Step 1: 表格操作列增加：
  - 复制代理
  - 加入推荐
  - 排除高风险

- [ ] Step 2: 增加“只导出低风险节点”按钮。

- [ ] Step 3: 如果 API 暂无端口映射，前端根据 multi-port base_port + index 生成本地代理。

---

## Chunk 4: Android 操作逻辑独立化

### Task 7: 新增 Android 代理页或代理提取内的独立模块

**Files:**
- Modify: `internal/monitor/assets/index.html`
- Update: `LOCAL_USAGE.md`
- Update: `AI_PROXY_ACCESS.md`

**目标:** 解决 Android 用户不知道该用 `127.0.0.1`、电脑 LAN IP、还是 `adb reverse` 的问题。

- [ ] Step 1: 新增 Android 卡片，显示三种模式：
  1. adb reverse 模式：手机填 `127.0.0.1:PORT`
  2. 局域网直连模式：手机填 `电脑局域网IP:PORT`
  3. 清除代理：`adb shell settings put global http_proxy :0`

- [ ] Step 2: 每个地区生成三条命令：
  - reverse
  - set proxy
  - clear proxy

- [ ] Step 3: 提醒：手机的 `127.0.0.1` 是手机自己，不是电脑；没有 adb reverse 时不能直接用电脑本机 127.0.0.1。

- [ ] Step 4: 验证命令不打印真实密码，因为 Android no-auth 端口不需要密码。

---

## Chunk 5: 样式系统收敛

### Task 8: 统一卡片尺寸、按钮层级、表格密度

**Files:**
- Modify: `internal/monitor/assets/index.html`

**目标:** 让页面看起来一致，减少“临时拼出来”的感觉。

- [ ] Step 1: 增加统一 CSS token：
  - `--radius-card`
  - `--card-padding`
  - `--grid-gap`
  - `--accent-blue`
  - `--accent-green`
  - `--accent-orange`

- [ ] Step 2: 统一 `.panel`, `.stat-panel`, `.extractor-preset-card`, `.cf-card`, `.reputation-card` 的圆角、阴影、padding。

- [ ] Step 3: 统一按钮类型：
  - primary：提取/复制
  - secondary：查看/检测
  - danger：清空/删除

- [ ] Step 4: 所有快捷卡片同一行高度一致。

- [ ] Step 5: 移动端检查：
  - 侧边栏可折叠或至少不挤压内容。
  - 卡片一列显示。

---

## Chunk 6: 验收和安全检查

### Task 9: 自动化/手动验证

**Files:**
- Modify: none unless bugs found

- [ ] Step 1: Go 测试
  ```bash
  GOCACHE=/tmp/easy_proxies-gocache go test ./...
  ```

- [ ] Step 2: 构建
  ```bash
  GOCACHE=/tmp/easy_proxies-gocache go build -tags "with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api" -o ./easy_proxies_local ./cmd/easy_proxies
  ```

- [ ] Step 3: 启动本地二进制
  ```bash
  setsid -f ./easy_proxies_local --config config.yaml >/tmp/easy_proxies.run.log 2>&1 < /dev/null
  ```

- [ ] Step 4: 页面内容检查
  ```bash
  curl --noproxy '*' -s http://127.0.0.1:9091/ | grep -n '代理提取\|指纹浏览器\|Android\|质量检测'
  ```

- [ ] Step 5: API 检查
  ```bash
  curl --noproxy '*' -s 'http://127.0.0.1:9091/api/extractor?region=jp&mode=multi-port&format=host_port_user_pass&count=3' | jq '.count,.metadata'
  ```

- [ ] Step 6: 敏感信息检查
  ```bash
  # 按本地已知敏感关键词检查；不要把真实订阅 URL、token 或密码写入计划/文档/提交信息。
  grep -R "<KNOWN_SUBSCRIPTION_DOMAIN_OR_TOKEN>" -n --exclude=config.yaml --exclude=nodes.txt --exclude=easy_proxies_local . || true
  ```

- [ ] Step 7: 浏览器人工验收
  - 默认打开代理提取页。
  - 指纹浏览器随机 10 条可复制。
  - JP 地区池可复制。
  - Android JP 命令可复制。
  - CF 优秀节点可直接复制代理。
  - 文档没有泄露订阅地址和真实凭据。

---

## Recommended Execution Order

1. 先做 Chunk 2：代理提取页，因为这是用户最高频入口。
2. 再做 Chunk 5：统一视觉和卡片尺寸，快速改善观感。
3. 再做 Chunk 3：质量检测和提取联动，提升实际使用效率。
4. 再做 Chunk 4：Android 独立化，减少网络配置误解。
5. 最后做 Chunk 1：导航重排。默认页切换要谨慎，避免用户找不到旧功能。

---

## Non-goals

- 不引入 React/Vue/Vite。
- 不重写后端管理 API。
- 不改现有代理端口。
- 不删除现有 WebUI 功能。
- 不在最终输出、日志或文档中暴露订阅 URL、真实账号密码。
