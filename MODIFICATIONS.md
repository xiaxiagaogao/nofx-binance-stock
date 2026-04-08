# NOFX 修改说明 / Modifications

本仓库基于 [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx) main 分支修改，标注所有在原项目基础上所做的变更。

---

## 一、Symbol 标准化修复（`market/data.go`）

**问题**：用户使用股票类资产（TSLA/NVDA/AAPL/XAU 等）在 Binance Futures 交易，系统内部用 `xyz:TSLA` 格式，但 Binance API 只认 `TSLAUSDT`。导致 klines 行情、订单下单、持仓同步全面失败。

**修复**：在 `market/data.go` 中新增 `NormalizeForExchange()` 函数，在所有 Binance 路径（klines/订单/账户查询）前将 `xyz:` 前缀符号转换为 Binance 标准格式（如 `xyz:TSLA` → `TSLAUSDT`）。共 patch 了以下文件：
- `api/handler_klines.go`
- `trader/binance/futures_positions.go`
- `trader/binance/futures_orders.go`
- `trader/binance/order_sync.go`
- `kernel/engine_analysis.go`
- 等约 10 个文件

---

## 二、MCP 响应解析修复（`mcp/client.go` + `kernel/engine_analysis.go`）

### 2.1 `extractTextContent()` — Content 数组格式兼容

**问题**：部分 LLM 模型（如 x-ai/grok-4.1-fast-reasoning）返回的 `message.content` 不是字符串而是数组（如 `[{type:"text", text:"..."}]`），导致 `ParseMCPResponseFull()` 提取为空白。

**修复**：新增 `extractTextContent()` 递归解析，支持：
- 字符串：`"text content"`
- 数组：`[{type:"text", text:"..."}]`
- 对象变体：`{text:{value:"..."}}`、`{output_text:"..."}`、`{reasoning_content:"..."}`

文件：`mcp/client.go`

### 2.2 `extractTextFromRawBody()` — 兜底解析

作为最后一层兜底，直接解析原始 HTTP response body 中的所有可能文本路径。

### 2.3 空数组 `[]` = 有效 Wait 决策

**问题**：AI 明确输出 `<decision>[]</decision>`（有意不开仓），但 parser 要求 `[]` 内必须包含 `{...}` 对象，导致空数组被判定为"解析失败"触发 SafeFallback。

**修复**：在 `kernel/engine_analysis.go` 的 `extractDecisions()` 中，判断 `jsonPart == "[]"` 时直接返回空 decisions，视为有效决策而非错误。

---

## 三、OI 过滤器禁用（`kernel/engine_analysis.go`）

**问题**：系统硬编码 `minOIThresholdMillions = 15.0`，导致新上市/映射的股票类资产（QQQ、SPY）因 OI 过低被排除在候选池外。

**修复**：`minOIThresholdMillions = 0.0`，允许手动精选的低 OI 优质资产进入决策引擎。

---

## 四、策略 4.4 Prompt 更新

### 4.1 新增「止损/止盈」语义重定义（`entry_standards`）

- **均值回归仓不给固定止损**，而是"极大缓冲保险止损"（入场价 ±15% 或 4H 结构破坏位），备注"结构止损 + 人工兜底"
- **止盈靠 4H/1D 结构破坏**，禁止因 1H RSI 偏高主动止盈
- 核心逻辑：防止被 1H 噪声扫出场

### 4.2 新增「队友意识」（`role_definition`）

AI 知道有队友（主人）共同管理账户，不需要独自 all-in 风控：
- 1H 出现普通噪音时可以 hold，不需要"解释为什么不卖"
- 核心职责：大机会入场 + 结构破坏离场，中间地带交给持仓和队友

### 4.3 持仓纪律强化（`decision_process` step 7）

- 持仓评估优先于候选评估
- 结构没破坏 → 不离场、不焦虑
- 结构彻底破坏 → 离场，不等止损被打
- 均值回归仓置信度要求从 80 → **85**

### 4.4 新增「候选评估」前提（`entry_standards` 开头）

已有持仓时，不为"第二个机会"分心；候选机会遍地有，在场仓位是第一优先级。

---

## 五、AI 模型配置更新

| 项目 | 原配置 | 现配置 |
|---|---|---|
| 模型 | `sub2api/gpt-5.4` | `x-ai/grok-4.1-fast-reasoning` |
| Provider | openai | openai |
| Base URL | `https://api.xg22.top/v1` | `https://api.qnaigc.com/v1` |

---

## 六、部署方式

所有修复通过**自定义 Docker 镜像**上线：
- 镜像名：`nofx-backend:symbolfix-test`
- 构建方式：`docker build -t nofx-backend:symbolfix-test -f docker/Dockerfile.backend .`
- 替换方式：修改 `docker-compose.yml` 中 `image` 字段，通过 `docker compose up -d` 无痛切换
