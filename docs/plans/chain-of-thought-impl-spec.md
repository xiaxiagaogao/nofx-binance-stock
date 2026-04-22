# 链式推理实现规格书

**分支**: `feature/chain-of-thought`  
**状态**: 代码探查完成，待动工  
**对应计划书**: `docs/plans/chain-of-thought-agent.md`

---

## 一、现有架构（单次调用）

### 调用链

```
trader/auto_trader_loop.go
  └── runCycle()
        └── buildTradingContext()          // 收集账户、持仓、行情、宏观论文等所有数据
        └── GetFullDecisionWithStrategy()  // ← 单次 LLM 调用，完成所有判断
              └── fetchMarketDataWithStrategy()  // 拉取行情
              └── engine.BuildSystemPrompt()     // 构建 system prompt
              └── engine.BuildUserPrompt()       // 构建 user prompt（含全部数据）
              └── mcpClient.CallWithMessages()   // ← 实际 AI API 调用（仅此一次）
              └── parseFullDecisionResponse()    // 解析 JSON 决策
```

### 关键文件

| 文件 | 职责 |
|---|---|
| `trader/auto_trader_loop.go` | 主循环，调用入口 |
| `kernel/engine_analysis.go` | `GetFullDecisionWithStrategy()` 实现 |
| `kernel/engine_prompt.go` / `prompt_builder.go` | System/User prompt 构建 |
| `kernel/engine.go` | `Context`、`Decision`、`FullDecision` 类型定义 |
| `mcp/client.go` | AI API 客户端，`CallWithMessages(systemPrompt, userPrompt string) (string, error)` |
| `store/strategy.go` | 策略配置结构体，风控参数 |

### 核心类型

```go
// 所有数据容器（AI调用前已填充完毕）
type Context struct {
    Account            AccountInfo
    Positions          []PositionInfo
    CandidateCoins     []CandidateCoin
    MarketDataMap      map[string]*market.Data
    MacroThesis        *MacroThesisContext
    PortfolioExposure  *PortfolioExposure
    TradingSession     string
    SessionScaleFactor float64
    // ... 更多字段
}

// AI 决策输出
type FullDecision struct {
    SystemPrompt        string
    UserPrompt          string
    CoTTrace            string
    Decisions           []Decision
    RawResponse         string
    AIRequestDurationMs int64
}

// 单条交易指令
type Decision struct {
    Symbol          string
    Action          string   // open_long / open_short / close_long / close_short / hold / wait
    Leverage        int
    PositionSizeUSD float64
    StopLoss        float64
    TakeProfit      float64
    Confidence      int
    Reasoning       string
    IntentType      string   // core_beta / tactical_alpha / hedge / opportunistic
    EntryThesis     string
    MacroThesisUpdate *MacroThesisUpdate
}
```

---

## 二、链式推理架构（目标）

### 核心思路

复用同一份 `Context`（数据只拉取一次），对 `mcpClient.CallWithMessages()` 发起 5 次串行调用，每次只做一件事，前一步的输出作为下一步的输入。最终产出与现有 `FullDecision` 格式完全兼容。

### 调用链（升级后）

```
runCycle()
  └── buildTradingContext()                    // 不变
  └── if EnableChainOfThought:
        GetFullDecisionChained()               // ← 新函数（新文件）
          ├── Step1: macroAlignmentCall()      // AI调用 #1 — 宏观对齐
          ├── Step2: technicalScreeningCall()  // AI调用 #2 — 技术筛选
          ├── Step3: portfolioReviewCall()     // AI调用 #3 — 组合审查
          ├── Step4: decisionGenerationCall()  // AI调用 #4 — 决策生成
          └── Step5: riskVerificationCall()    // AI调用 #5 — 风险验证
      else:
        GetFullDecisionWithStrategy()          // 现有逻辑，完全不变
```

---

## 三、五步接口设计

### Step 1 — 宏观对齐

**System prompt**: 简短的基金经理角色定义  
**User prompt 输入**:
```json
{
  "macro_thesis": {
    "market_regime": "neutral",
    "portfolio_intent": "selective_long",
    "sector_bias": {"semiconductor": "bullish", "energy": "bearish"},
    "key_risks": ["中东冲突升级", "通胀超预期"],
    "thesis_text": "..."
  },
  "current_session": "us_market_open"
}
```
**输出 schema**:
```json
{
  "allowed_sectors": ["semiconductor", "index", "tech_mega"],
  "restricted_sectors": ["energy"],
  "direction_bias": "long_preferred",
  "session_note": "正式开盘，可正常建仓",
  "reasoning": "..."
}
```

---

### Step 2 — 技术筛选

**输入**: Step1 输出 + 候选标的行情（EMA、RSI、OI、资金费率、K线结构）  
**输出 schema**:
```json
[
  {
    "symbol": "NVDAUSDT",
    "direction": "long",
    "confidence": 78,
    "structure": "4H EMA55 上方，1H回踩支撑",
    "key_entry_level": 890.0,
    "key_stop_level": 860.0,
    "pass": true
  },
  {
    "symbol": "METAUSDT",
    "direction": "none",
    "confidence": 45,
    "structure": "区间中部，无位置优势",
    "pass": false
  }
]
```

---

### Step 3 — 组合审查

**输入**: Step2 通过列表 + 当前持仓明细（仓位数、板块分布、保证金占比）  
**输出 schema**:
```json
{
  "available_slots": 2,
  "approved": ["NVDAUSDT", "QQQUSDT"],
  "rejected": [
    {"symbol": "INTCUSDT", "reason": "semiconductor 板块已达上限 2/2"}
  ],
  "reasoning": "..."
}
```

---

### Step 4 — 决策生成

**输入**: Step3 approved 列表 + 对应行情数据  
**输出**: 与现有 `[]Decision` 格式完全一致（直接复用现有解析器）

---

### Step 5 — 风险验证

**输入**: Step4 决策列表 + 风控参数（max_leverage、max_margin_usage、category_max_positions 等）  
**输出 schema**:
```json
[
  {
    "symbol": "NVDAUSDT",
    "status": "approved",
    "decision": { /* 原决策，不变 */ }
  },
  {
    "symbol": "QQQUSDT",
    "status": "adjusted",
    "note": "position_size_usd 从 60 调整为 40，保证金占比超限",
    "decision": { /* 调整后的决策 */ }
  }
]
```

---

## 四、改动范围

### 新增文件

**`kernel/engine_chain.go`**
- `GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error)`
- 内含 5 个 step 函数，每个独立的 system/user prompt 构建 + AI 调用 + JSON 解析
- 任意 step 失败 → 降级到 `GetFullDecisionWithStrategy()`，保证不中断

### 修改文件

**`store/strategy.go`** — `StrategyConfig` 新增字段:
```go
type StrategyConfig struct {
    // ... 现有字段
    EnableChainOfThought bool `json:"enable_chain_of_thought,omitempty"` // 默认 false
}
```

**`trader/auto_trader_loop.go`** — `runCycle()` 中路由:
```go
var aiDecision *kernel.FullDecision
var err error

if at.config.StrategyConfig != nil && at.config.StrategyConfig.EnableChainOfThought {
    logger.Infof("🔗 [%s] Chain-of-thought mode enabled", at.name)
    aiDecision, err = kernel.GetFullDecisionChained(ctx, at.mcpClient, at.strategyEngine)
} else {
    aiDecision, err = kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")
}
```

---

## 五、兼容性与降级

- `EnableChainOfThought` 默认 `false`，现有所有 trader 行为零变化
- 任一 step AI 调用失败或 JSON 解析失败 → 自动降级回单次调用 + 写入日志
- Step4 输出复用现有 `parseFullDecisionResponse()`，决策格式不变
- `FullDecision` 结构不变，DB 存储、前端展示均无需修改

---

## 六、Token 消耗估算

| 模式 | Tokens/周期 | 开盘(20min) | 关盘(120min) |
|---|---|---|---|
| 现有单次 | ~3,000 | ~90次/天 = 270k | ~12次/晚 = 36k |
| 链式5步 | ~10,000 | ~90次/天 = 900k | ~12次/晚 = 120k |

开盘时段 token 消耗约增加 3x，需确认 AI 模型的费率是否可接受。  
可通过适当增大 `us_market_open` 间隔（如 30min）来控制成本。

---

## 七、验证方案

1. 本分支开发完成，本地单元测试通过
2. EC2 新建独立 trader 实例，开启 `enable_chain_of_thought: true`，小仓位（$10 起）并行跑
3. 原 trader 不动，继续跑单次模式作为对照组
4. 运行 4 周，对比：胜率、平均持仓周期、最大回撤、日均 AI 消耗
5. 数据支持后提 PR 合并到 main

---

## 八、待决定（需开发者协商）

- [ ] Step1-5 各步的完整 prompt 文本（当前文档仅定义了输入/输出 schema）
- [ ] Step2 行情数据精简方案（避免单步 token 过大）
- [ ] 降级日志的存储字段（建议复用 `CoTTrace` 字段标注 `[chain-degraded]`）
- [ ] 是否在前端 UI 暴露 `enable_chain_of_thought` 开关

---

*规格书生成于 2026-04-20，基于代码探查结果。对应 feature/chain-of-thought 分支。*
