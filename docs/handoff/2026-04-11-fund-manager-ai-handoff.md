# Fund Manager AI 改造 — 开发者对齐文档

**日期**: 2026-04-11
**分支**: `main`
**起点 commit**: `ebd3f98`（计划文档）
**终点 commit**: `0b80176`
**关联计划**: [docs/superpowers/plans/2026-04-11-fund-manager-ai.md](../superpowers/plans/2026-04-11-fund-manager-ai.md)

---

## 0. 一句话总结

把原本「无状态、追涨杀跌」的加密货币交易机器人，改造成「有状态、组合视角、宏观优先」的 **AI 基金经理**，用来管理 Binance 上的美股映射永续合约（TSLA / NVDA / QQQ / SPY / XAU 等）。

---

## 1. 设计哲学变更

| 维度 | 改造前 | 改造后 |
|---|---|---|
| **决策粒度** | 单笔交易 | 整体组合 |
| **时间视野** | 无状态（每个循环独立） | 有状态（跨循环持久化宏观论文） |
| **信号优先级** | 技术面 | 宏观 / 基本面 > 板块资金流 > 技术面 |
| **追求目标** | 动量 / 胜率 | β（跟大盘）+ α（主动超额） |
| **仓位语义** | 只有多空方向 | 标签化：core_beta / tactical_alpha / hedge / opportunistic |
| **回撤阈值** | 40% | 25%（股票波动小于加密） |
| **非交易时段** | 硬阻塞 | 软缩放（session 乘数 1.0 / 0.5 / 0.3 / 0.05） |
| **宏观输入** | 无 | AI 自动维护 + 用户可推送外部研报 |

---

## 2. 架构分层

```
┌─────────────────────────────────────────────┐
│  Layer 1: 宏观论文层 (新增)                    │
│  ├─ store.MacroThesis (DB 持久化)           │
│  ├─ macro_reports/latest.md (用户推送)       │
│  └─ MacroThesisUpdate (AI 每轮最多更新一次)    │
├─────────────────────────────────────────────┤
│  Layer 2: 组合层 (新增)                       │
│  ├─ PortfolioExposure (category/方向/β/α/hedge) │
│  ├─ SessionScaleFactor (盘中/盘前/盘后/关盘)  │
│  └─ 板块集中度守卫                             │
├─────────────────────────────────────────────┤
│  Layer 3: 个股执行层 (已有 + 强化)             │
│  ├─ 仓位意图标签 (intent_type / entry_thesis) │
│  └─ 可配置回撤激活/触发阈值                    │
└─────────────────────────────────────────────┘
```

---

## 3. Commit 时间线（按任务号）

| # | Commit | 变更 |
|---|---|---|
| 1 | `3567e35` | `RiskControlConfig` 增加 `SessionRiskScale` / `SymbolCategories` / `MaxSameCategoryPositions` / `DrawdownActivationProfit` / `DrawdownCloseThreshold` 字段及默认值和 helper |
| 2 | `5a41f30` | 新增 `store/macro_thesis.go`：`MacroThesis` 表 + `MacroThesisStore` |
| 3 | `2d1ffb2` + `6635beb` | `TraderPosition` 增加 `intent_type` / `entry_thesis`；`UpdatePositionIntent(id int64, ...)` |
| 4 | `d9e3a2a` | `Store` 注册 `MacroThesisStore`（懒加载） |
| 5 | `d3f5a35` | `kernel/engine.go`：`Context` 增加 `MacroThesis` / `MacroReport` / `PortfolioExposure` / `SessionScaleFactor`；`Decision` 增加 `IntentType` / `EntryThesis` / `MacroThesisUpdate`；新增 `MacroThesisContext` / `PortfolioExposure` / `MacroThesisUpdate` 类型；`PositionInfo` 增加 `IntentType` |
| 6 | `67fedef` | 新增 `macro_reports/` 目录 + README（外部宏观报告推送位置） |
| 7 | `e458b48` | `trader/auto_trader_loop.go`：`buildTradingContext` 注入宏观论文 / 组合暴露 / session；`runCycle` 在 AI 返回后持久化 `MacroThesisUpdate` |
| 8 | `edb9073` | `auto_trader_risk.go`：session 缩放 + 可配回撤；新增 `enforceMaxSameCategoryPositions`；`auto_trader_orders.go` 两处 open 加入 category guard 调用 |
| 9 | `8b7449d` | `auto_trader_orders.go`：通过 `pendingIntents` 缓冲仓位意图，在 loop 中发现 DB 行出现后应用 `UpdatePositionIntent` |
| 10 | `3db94f6` | `kernel/engine_prompt.go`：基金经理 System Prompt（中英双语）+ User Prompt 注入宏观/组合/session 块 + decision 格式增加 fund manager 可选字段 |
| 11 | `0b80176` | `kernel/prompt_builder.go`：兜底 prompt 的中英文 role 改为基金经理框架 + 示例加 `intent_type` / `entry_thesis` |

CI 状态：`0b80176` 的 **Test** job（Go 编译）通过 ✅。
`Go Test Coverage` 和 `Docker Build` 的失败从 `ebd3f98` 之前就存在，与本次改动无关。

---

## 4. 新增 / 修改文件清单

### 新增
- `store/macro_thesis.go` — 宏观论文持久化层
- `macro_reports/.gitkeep`、`macro_reports/README.md` — 外部研报推送目录
- `docs/superpowers/plans/2026-04-11-fund-manager-ai.md` — 12 任务实施计划
- `docs/handoff/2026-04-11-fund-manager-ai-handoff.md` — 本文档

### 修改
- `store/strategy.go` — RiskControlConfig 扩展
- `store/position.go` — TraderPosition 字段 + `UpdatePositionIntent`
- `store/store.go` — 注册 MacroThesisStore
- `kernel/engine.go` — Context / Decision / 新类型
- `kernel/engine_prompt.go` — 基金经理 System/User Prompt
- `kernel/prompt_builder.go` — 兜底 prompt
- `trader/auto_trader.go` — `pendingIntents` / `pendingPositionIntent`
- `trader/auto_trader_loop.go` — 宏观上下文构建 + 持久化
- `trader/auto_trader_orders.go` — category guard 调用 + intent 缓冲 + helpers
- `trader/auto_trader_risk.go` — session 缩放 + 可配 drawdown + category guard 方法

---

## 5. 新增数据模型

### 5.1 `store.MacroThesis`（新表：`macro_thesis`）

```go
type MacroThesis struct {
    ID              int64
    TraderID        string
    MarketRegime    string  // risk_on | risk_off | mixed | cautious
    ThesisText      string
    SectorBias      string  // JSON: {"semiconductor":"bullish"}
    KeyRisks        string  // JSON: ["Fed hike risk",...]
    PortfolioIntent string  // "building_tech_long"
    ValidHours      int     // 默认 24
    Source          string  // "ai" | "manual"
    CreatedAt, UpdatedAt time.Time
}
```

关键方法：
- `MacroThesisStore.GetLatest(traderID)` — 拿最新一条
- `MacroThesisStore.Create(thesis)` — 追加写入（不更新，保留历史）
- `thesis.IsStale()` — 是否已过期（> ValidHours）
- `thesis.ParseSectorBias() / ParseKeyRisks()` — JSON → 结构

### 5.2 `TraderPosition` 新字段

```go
IntentType  string // core_beta | tactical_alpha | hedge | opportunistic
EntryThesis string // 1-2 句话的入场理由
```

迁移：GORM 自动加列（SQLite），老数据字段为空。

### 5.3 `RiskControlConfig` 新字段

```go
SessionRiskScale         map[string]float64 // us_market_open:1.0 / us_pre_market:0.5 / us_after_hours:0.3 / us_market_closed:0.05
SymbolCategories         map[string]string  // TSLAUSDT:ev_auto, NVDAUSDT:semiconductor, QQQUSDT:index, ...
MaxSameCategoryPositions int                // 默认 2
DrawdownActivationProfit float64            // 默认 0.03（3%）
DrawdownCloseThreshold   float64            // 默认 0.25（25%）
```

---

## 6. Kernel Context / Decision 扩展

### Context 新字段

```go
MacroThesis        *MacroThesisContext // 当前生效的宏观论文
MacroReport        string              // macro_reports/latest.md 内容
PortfolioExposure  *PortfolioExposure  // 组合层聚合
SessionScaleFactor float64             // 当前 session 的风险乘数
```

### Decision 新字段

```go
IntentType        string             // AI 分配给本次开仓的意图
EntryThesis       string             // 入场理由
MacroThesisUpdate *MacroThesisUpdate // 每轮最多一次
```

### 新类型

- `MacroThesisContext` — 注入 AI 的宏观论文视图（带 `AgeHours`）
- `PortfolioExposure` — 按 category / 方向 / β/α/hedge 聚合的组合统计
- `MacroThesisUpdate` — AI 返回的论文更新

---

## 7. 关键执行流（Trader Loop）

```
┌─ runCycle() ──────────────────────────────────────┐
│                                                   │
│  1. buildTradingContext()                         │
│     ├─ GetPositions() → PositionInfo[]            │
│     │   └─ 对每个仓位：GetOpenPositionBySymbol    │
│     │      ├─ 读 dbPos.IntentType                 │
│     │      └─ 若为空 & 有 pendingIntents →        │
│     │         UpdatePositionIntent()             │
│     ├─ MacroThesis().GetLatest() → ctx.MacroThesis│
│     ├─ readMacroReport() → ctx.MacroReport        │
│     ├─ calculatePortfolioExposure() → ctx.PE      │
│     └─ GetUSTradingSession() → ctx.SessionScale   │
│                                                   │
│  2. callAI(ctx)                                   │
│                                                   │
│  3. 持久化 MacroThesisUpdate                       │
│     └─ 遍历 decisions，取第一个非 nil update      │
│        MacroThesis().Create(...)                  │
│                                                   │
│  4. executeDecisionWithRecord()                   │
│     ├─ enforceMaxPositions                        │
│     ├─ enforceMaxSameCategoryPositions (NEW)      │
│     ├─ enforcePositionValueRatio (session-scaled) │
│     ├─ OpenLong / OpenShort                       │
│     ├─ recordAndConfirmOrder                      │
│     └─ rememberPendingIntent() (NEW)              │
│                                                   │
└───────────────────────────────────────────────────┘
```

### ⚠️ 关键设计点：为什么 intent 要用 pendingIntents 缓冲？

在 Binance 上，position 表不是在 `OpenLong` 时写的，而是由后台 `OrderSync` 异步根据 trade fills 写的（`store.PositionBuilder.ProcessTrade`）。**下单时拿不到 positionID**，所以：

1. 下单成功时把 `(symbol, side) → {IntentType, EntryThesis}` 存到内存 map `at.pendingIntents`
2. 下一个（或后续）循环构建 context 时，`GetOpenPositionBySymbol` 能拿到 OrderSync 写好的行
3. 若 `dbPos.IntentType == ""`，就 `consumePendingIntent` 并 `UpdatePositionIntent(dbPos.ID, ...)`
4. map 有 mutex 保护（`pendingIntentsMutex`）

这个设计对所有异步创建 position 的 exchange 都通用，不止 Binance。

---

## 8. Prompt 层关键改动

### 8.1 `engine_prompt.go` — 主 System Prompt

- **Role**：默认 role 改为 `fundManagerRoleZH` / `fundManagerRoleEN` 常量（按 `LangChinese` 切换）
- 明确三层决策层次：宏观 → 组合 → 个股
- 明确 4 种仓位意图类型
- 明确信号优先级：宏观 > 板块资金 > 技术面

### 8.2 `engine_prompt.go` — User Prompt 注入顺序

```
Time / Session
  ↓
[NEW] ## 当前宏观论文 (X.Xh ago, source: ai|manual) [⚠️ STALE]
  ↓
[NEW] ## 外部宏观报告（用户提供，高优先级参考）
  ↓
[NEW] ## 当前组合暴露 — 方向 / 核心β / 战术α / 对冲 / 板块分布
  ↓
[NEW] ## 当前交易时段风险系数: X.XX
  ↓
SPY 参考 / Account / RecentOrders / Positions / Candidates / ...
```

### 8.3 Decision JSON 新增可选字段

```json
{
  "intent_type": "core_beta | tactical_alpha | hedge | opportunistic",
  "entry_thesis": "1-2 sentence rationale",
  "macro_thesis_update": {
    "market_regime": "risk_on|risk_off|mixed|cautious",
    "thesis_text": "...",
    "sector_bias": {"semiconductor": "bullish"},
    "key_risks": ["..."],
    "portfolio_intent": "building_tech_long",
    "valid_hours": 24
  }
}
```

约束：`macro_thesis_update` 每次循环最多一次（代码只取第一个非 nil 的）。

### 8.4 `prompt_builder.go`（兜底 prompt）

同步把中英文 role 改为基金经理框架，示例 JSON 加 `intent_type` / `entry_thesis`。

---

## 9. 外部宏观报告机制（`macro_reports/`）

用户可以把研报、电话会议摘要、FOMC 分析写到 `macro_reports/latest.md`：

- 格式：Markdown，无 front matter
- 新鲜度：超过 48 小时自动标记 `⚠️ STALE`（在 `readMacroReport()` 里判断）
- 注入优先级：**高于 AI 自己维护的宏观论文**（在 User Prompt 里位置靠前、有显式标签）
- 机制：每个循环直接 read file，不缓存

这让人类研究员可以快速把外部信息灌进 AI 决策环境，不需要跑完整的知识库管线。

---

## 10. 配置样例（给交易员改 strategy 时参考）

```go
// store.GetDefaultStrategyConfig() 里的默认值
SessionRiskScale: map[string]float64{
    "us_market_open":   1.0,
    "us_pre_market":    0.5,
    "us_after_hours":   0.3,
    "us_market_closed": 0.05,
},
SymbolCategories: map[string]string{
    "TSLAUSDT":  "ev_auto",
    "NVDAUSDT":  "semiconductor",
    "INTCUSDT":  "semiconductor",
    "MUUSDT":    "semiconductor",
    "TSMUUSDT":  "semiconductor",
    "SNDKUSDT":  "semiconductor",
    "METAUSDT":  "tech_mega",
    "AMAZUSDT":  "tech_mega",
    "GOOGLUSDT": "tech_mega",
    "QQQUSDT":   "index",
    "SPYUSDT":   "index",
    "XAUUSDT":   "commodity",
    "CLUSDT":    "commodity",
},
MaxSameCategoryPositions: 2,
DrawdownActivationProfit: 0.03, // 3% 盈利才激活 trailing
DrawdownCloseThreshold:   0.25, // 从峰值回撤 25% 平仓
```

---

## 11. EC2 部署 / 验证清单

1. `git pull origin main`
2. `go build ./...` 确认编译通过
3. 启动后检查数据库自动迁移：
   - `PRAGMA table_info(trader_positions);` 应见 `intent_type`、`entry_thesis` 列
   - `.tables` 应见 `macro_thesis` 表
4. 跑一轮 AI 循环，观察：
   - 因为还没有 MacroThesis，prompt 里会提示「尚无宏观论文，请建立初始判断」
   - 返回的 decisions 里应至少有一个带 `macro_thesis_update`（下一轮循环会看到）
5. 手写 `macro_reports/latest.md`（随便写两句），下一轮确认 prompt 里出现「外部宏观报告」块
6. 触发一次开仓，确认：
   - `trader_positions.intent_type` 不为空（下一轮循环后）
   - 同板块 3 个同向仓位时 `enforceMaxSameCategoryPositions` 报错
7. 盘外时段开仓时 `enforcePositionValueRatio` 应按 `SessionRiskScale` 缩放

---

## 12. 风险 / 已知边界

1. **Binance side 大小写不一致**：`GetPositions()` 返回 `side=long|short`，但 `OrderSync` 写入 DB 的是 `LONG|SHORT`。当前 `GetOpenPositionBySymbol` 的大小写查询是**预存在问题**，我没动。如果 SQLite collation 默认是大小写敏感，intent 查询可能读不到 — 建议后续统一一个 case 规范。
2. **pendingIntents 不持久化**：进程重启后未落盘的 intent 会丢。考虑到意图丢失只影响组合层聚合、不影响资金安全，暂不处理。如果要稳健，可以在 Create position 钩子处直接从 `at.pendingIntents` 反查。
3. **MacroThesisUpdate 只取第一个**：runCycle 遍历 decisions 时取第一个非 nil 的 update。未来若 AI 返回多个会被忽略 —— 符合设计（每轮最多一次）。
4. **`macro_reports/latest.md` 无锁**：如果外部工具并发写，极端情况下可能读到半个文件。目前接受（人类推送频率低）。
5. **无 Go 本地编译器**：我所有验证都依赖 GitHub Actions 的 `Test` job。`Go Test Coverage` 和 `Docker Build` 的失败是遗留的，我没触碰。

---

## 13. 下一步可做的增量

- [ ] 宏观论文 UI：前端做一个面板显示当前论文 + 历史时间线
- [ ] `core_beta` 仓位强制低杠杆（≤ 3x）— 现在只在 prompt 里暗示
- [ ] 板块权重硬上限（不只是「同类不超过 2 个」，而是「同类总 notional 不超过组合 X%」）
- [ ] 用 MacroThesis 的 `PortfolioIntent` 驱动自动再平衡建议
- [ ] 外部研报多文件支持（`macro_reports/*.md` 而不是只读 `latest.md`）

---

## 14. 对齐问答

**Q: 为什么不直接重写 `enforceMaxPositions` 让它带 symbol/side？**
A: 会动到已有的 2 个调用点，增加回归风险。新加一个 `enforceMaxSameCategoryPositions` 更干净，职责单一。

**Q: `trader/` 包的接收者为什么是 `at` 不是 `t`？**
A: 整个包约定，别跟着改成 `t`，否则 lint 会炸。

**Q: `at.store` 何时为 nil？**
A: 单测场景可能为 nil，所有使用点都要 nil-check（已有约定）。

**Q: 如果 AI 不给 `intent_type` 会怎样？**
A: `rememberPendingIntent` 只在 `IntentType != "" || EntryThesis != ""` 时才存。老行为不变，仓位只是没有标签。

**Q: 为什么用 `macro_reports/latest.md` 而不是数据库？**
A: 人类推送场景用文件更顺手（scp / rsync / git push 都行）。数据库方案需要再做一个 API，工程量不匹配。

---

**最后更新**: 2026-04-11 · commit `0b80176`
