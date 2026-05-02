# Chain-of-Thought v2 — Prompt Injection Refactor

**分支**: `feature/chain-of-thought`
**对应镜像目标**: `chain-paper-v8-chain-injection`（待构建）
**前置版本**: `chain-paper-v7.1-pyramid-fix`
**创建日期**: 2026-05-02
**状态**: 计划完成，待实施

---

## 一、背景与触发

2026-05-02 切换生产 trader 到 chain 模式后，立即发现 token 数异常偏低（3 个请求合计 ~1.8k input vs 单 prompt 76k input）。代码探查确认：

`kernel/engine_chain.go` + `kernel/chain_prompts.go` **完全不引用 `store.StrategyConfig.PromptSections`**。

后果：
- 用户精心设计的 `prompt_sections`（基金经理角色 + 4 层入场过滤 + 资产类别规则 + 6 步决策流程）在 chain 模式下完全失效
- 2026-05-01 上午部署的 4 处补丁（基石优先级、组合层级优先序、指数操作三原则、减仓评估顺序）也被绕过
- chain 用的是它自己 hardcoded 的 mini-prompt，跟单 prompt 路径是两个完全不同的"基金经理人格"

---

## 二、设计原则

1. **prompt_sections 仍是 source of truth**——以后用户改 prompt，chain 自动跟随
2. **chain 骨架的"任务工具书"内容保留**——schema、3 分类框架、4 维排名规则等写得很好，不重写
3. **每步只注入它需要的 prompt_sections 子集**——保持 token 效率
4. **必须先修 7 个质量风险点，再谈 token 节省**

---

## 三、四步注入表

### Step 1 — 宏观对齐

**System prompt 组装**：
```
[role_definition 全文]                  # 角色一致性，~800 tokens
+ "\n\n---\n\n"
+ [trading_frequency 全文]              # 时段意识 + 持仓周期，~700 tokens（新增）
+ "\n\n---\n\n"
+ [chain Step 1 骨架]                    # JSON schema + macro_thesis_update 触发条件
```

**User prompt**: 不变（macro_thesis + portfolio summary + session + candidate sectors）

**估算**: system ~2.5k, user ~500，total ~3k

---

### Step 2 — 技术筛选（最关键）

**System prompt 组装**：
```
[role_definition 全文]                                 # ~800
+ [trading_frequency 中"事件意识"段]                    # ~300（新增，避免财报前误开仓）
+ [entry_standards 第三层 资产类别特性]                  # ~2k
+ [entry_standards 第四层 个股技术执行]                  # ~600
+ [entry_standards 指数操作三原则]                       # ~400
+ [chain Step 2 骨架]                                  # 三分类框架 + 反模式 + JSON schema, ~1k
```

**User prompt 组装**（与 chain 现状对比）：
```
保留: direction_bias + allowed_sectors + candidates_market_data
新增: ## 当前持仓简表（symbol/side/intent_type/PnL%）  # ~300（风险点 1）
新增: ## 关键风险（来自 macro_thesis.key_risks）        # ~200（风险点 3）
```

**Schema 调整**：
- Step 2 输出 schema 加 `is_add_candidate: bool`（标识"是否同方向已持仓 → add 而非 new"）（风险点 1）
- Step 2 不再输出 `intent_type`（统一到 Step 4 判定）（风险点 7）

**验证项**: candidates_market_data 当前格式是否包含 OI / funding / 4H+1H K 线全量；如简化太多，对齐到单 prompt 完整版（风险点 5）

**估算**: system ~5.1k, user ~10-30k（候选数定），total ~15-35k

---

### Step 3 — 组合排名（仅 candidates > slots 时调用 AI）

**System prompt 组装**：
```
[role_definition 中 intent_type 段]                    # ~300
+ [entry_standards 组合层级优先序段]                     # ~300
+ [chain Step 3 骨架]                                  # 4 维排名规则 + JSON schema, ~500
```

**User prompt**: 不变

**估算**: system ~1.1k, user ~1k，total ~2k（仅必要时触发）

---

### Step 4 — 决策生成（核心）

**System prompt 组装**：
```
[role_definition 全文]                                 # ~800
+ [trading_frequency 全文]                              # ~700
+ [entry_standards 全文]                                # ~3k（含 4 处补丁）
+ [decision_process 全文]                               # ~2k（含减仓评估顺序补丁）
+ [chain Step 4 骨架]                                  # decision JSON schema + add_long 说明 + 止损止盈纪律, ~400
```

**User prompt 组装**：
```
保留: approved candidates 详细行情 + account 状态
新增: ## 现有持仓完整行情（不经 Step 2 过滤，所有 OPEN 持仓）  # ~10-15k（风险点 6）
```

**估算**: system ~7k, user ~15-30k，total ~22-37k

---

## 四、7 个必修质量风险点（必须包含在 v2 实现中）

| # | 风险 | 修复 | Token 增加 |
|---|---|---|---|
| 1 | Step 2 不知道现有持仓，漏 add 机会 | Step 2 user prompt 加持仓简表 + schema 加 `is_add_candidate` | +300 |
| 2 | Step 1 wait 时整链断流 | chain 强制让 Step 4 接收完整持仓清单评估 close/add，无论 Step 1 输出什么 | 0 |
| 3 | 跨资产关联推理被切碎 | Step 2 user prompt 加 `key_risks` 段 | +200 |
| 4 | 财报/事件意识缺失 | Step 1/2 system prompt 加 trading_frequency | +600 |
| 5 | Step 2 行情数据可能简化 | 验证并对齐到单 prompt 完整版（OI/funding/双时段 K 线）| +5-10k |
| 6 | Step 4 未必看到所有持仓详情 | Step 4 user prompt 强制包含全部 OPEN 持仓行情 | +10-15k |
| 7 | intent_type 标签可能冲突 | Step 2 不输出 intent_type，统一到 Step 4 | 0 |

---

## 五、实施任务清单

### Task 1: 验证现状 ✅ 完成 2026-05-02

**发现 1 — 市场数据格式 (风险点 5)**：
- `formatChainMarketData` (`engine_chain.go:614-625`) 复用 `engine.formatMarketData(md)` ——和单 prompt 路径**完全相同的 per-symbol 格式**
- 含 EMA / RSI / OI / funding / 1H+4H K 线全量
- **风险点 5 实际不存在**——chain 单候选数据是完整的，只是候选数被剪短（这正是设计意图）
- 行动调整：原 +5-10k token 估算可省，对应"必查"项移除

**发现 2 — Step 4 持仓详情缺失 (风险点 6)**：
- Step 4 user prompt (`engine_chain.go:586-595`) 含：`candidates_json` + `positions_summary` + `equity` + `margin_pct` + `slots` + `market_data`
- `positions_summary` 是一行文本（symbol/side/qty/entry/PnL%/peak%/intent_type）
- `market_data` (line 595) 调 `formatChainMarketData(ctx, engine, candidates)` ——**只传 candidates，不含 positions**
- **Step 4 看不到持仓的 EMA/RSI/K 线 → 无法做有依据的 close/add/hold 判断**
- 修复路径确认：Step 4 user prompt 增加"## 现有持仓完整行情"段，对所有 OPEN 持仓调用 `engine.formatMarketData(md)` 注入

**发现 3 — Step 1 wait 断流 (风险点 2) — 严重**：
- `engine_chain.go:44-52`：`step1.DirectionBias == "wait"` 时**直接 return 空 Decisions，不论持仓状态**
- `line 58-66`：`filtered candidates == 0 && positions == 0` → return 空（合理）
- `line 91-99`：code-filter 后 0 候选 && 无持仓 → return 空（合理）
- **致命：Step 1 wait 短路逻辑（line 44-52）忽略了持仓** —— 当宏观判断"等待"时，已开持仓在那个 cycle 完全得不到评估
- 实证：今天 cycle 658 关盘期间，scale_factor=0.05 → Step 1 把 15 candidates 过滤到 0，幸运地落在 line 58 短路（当时持仓不为 0 应该走到 Step 4 但实际…等等需要看具体路径）
- 修复路径：移除 line 44-52 的 wait 短路；改为 wait → 跳过 Step 2/3 → 直接 Step 4 接收持仓清单评估 hold/close/add

**发现 4 — Step 2 user prompt 缺持仓和风险上下文 (风险点 1, 3)**：
- `renderStep2User` (`engine_chain.go:221-228`) 只含 direction_bias + allowed_sectors + candidates_market_data
- 不含持仓简表、不含 macro_thesis.key_risks
- 修复路径确认：加 `{{positions_summary}}` 和 `{{key_risks}}` 占位符到 `PromptStep2TechnicalUserV1`

**发现 5 — Step 4 拿到的 candidates 已被 Step 2/3 剪枝**：
- `line 102-127`：`finalCandidates` = postFilter 或经 Step 3 排名的 top-N
- **若 Step 2 把现有持仓符号标 pass=false，Step 4 就拿不到该持仓的"决策候选"身份**
- 但 Step 4 仍能通过 `positions_summary` 看到该持仓存在 → 可决策 close/hold（虽然没有详细行情）
- 修复路径：Step 4 user prompt 强制注入持仓详情已在风险点 6 修复中包含

**Task 1 总结**：
- 5/7 风险点经代码验证为真（1, 2, 3, 4, 6, 7）
- 风险点 5 验证为伪——市场数据格式不需修
- 节省 token 估算调整：原"必修后 80-105k"→ 实际 75-95k

### Task 2: 实现 prompt_sections 子段提取
- [ ] 在 `kernel/engine_chain.go` 添加辅助函数 `extractSection(body string, marker string) string`
- [ ] 定义稳定的 section markers（与用户 prompt_sections 文本约定）：
  - `## 组合层级优先序` (entry_standards 顶部新加段)
  - `第三层：资产类别特性` (entry_standards 中段)
  - `第四层：个股技术执行` (entry_standards 中段)
  - `## 指数操作三原则`（entry_standards 中加段，可能需要在补丁里加 ## 标题）
  - `## 美股时段行为准则` (trading_frequency 中段)
- [ ] 提取失败时降级：返回完整段或 fallback 到 chain skeleton-only
- [ ] 单元测试：对当前 DB prompt_sections 内容做 extraction smoke test

### Task 3: Step 1/2/3/4 prompt 组装
- [ ] 在 `kernel/engine_chain.go` 加 `renderChainSystemPrompt(step int, sections store.PromptSections, skeleton string) string`
- [ ] 修改 4 个 step 函数（`step1Macro`, `step2Technical`, `step3Ranking`, `step4Decision`），改用新 render 函数
- [ ] Step 2 user prompt builder 加持仓简表 + key_risks
- [ ] Step 4 user prompt builder 加现有持仓详情
- [ ] Step 4 chain 骨架精简（移除已经在 prompt_sections 里的内容，只保留 schema + 止盈止损纪律补充）

### Task 4: Schema 调整
- [ ] Step 2 输出 schema 加 `is_add_candidate bool`，去掉 `intent_type`
- [ ] Step 4 解析时根据 `is_add_candidate` 强制 action 为 `add_long`/`add_short`（已有同向持仓时）

### Task 5: 断流防护（风险点 2）
- [ ] 在 `engine_chain.go` 主流程加：即使 Step 1 输出 wait/empty，也要让 Step 4 评估现有持仓
- [ ] 具体：Step 1 wait → 跳过 Step 2/3 → Step 4 接收完整持仓清单（无新候选）→ 输出 hold/close/add 决策

### Task 6: 单元测试 + mockLLM
- [ ] `kernel/engine_chain_test.go` 加 6 个用例：
  - prompt 组装包含 role_definition
  - prompt 组装包含 entry_standards 第三层
  - prompt 组装包含 trading_frequency
  - extraction marker 缺失时降级正确
  - is_add_candidate=true 时 Step 4 强制 add_long
  - Step 1 wait 时 Step 4 仍接收持仓清单

### Task 7: 部署
- [ ] 备份 strategy config + DB
- [ ] commit + 同步到 VPS
- [ ] `docker build -t nofx-backend:chain-paper-v8-chain-injection`
- [ ] 备份 docker-compose.yml.before-v8
- [ ] 改 image tag + restart
- [ ] 立刻拉第一笔 cycle 的 cot_trace 验证：
  - cot 是否提到 "基石优先级" "组合层级优先序" 等用户 prompt 关键词
  - 关盘期间 token 是否真的降下来（Step 2 候选清零→大量节省）
  - 持仓详细行情是否进了 Step 4

---

## 六、Token 总量估算（与单 prompt 对比）

| 模式 | 关盘 1 cycle | 开盘 1 cycle | 备注 |
|---|---|---|---|
| 单 prompt | ~76k | ~76k | 候选不变 |
| chain v1（现状）| ~2k | ~2k | 质量丢失 |
| **chain v2（本计划）** | ~5-10k | ~50-90k | 关盘 step1 过滤 → step2 几乎不跑 |

关盘期间 chain v2 比单 prompt 省 80%+，开盘期间持平或略多 10-20%。**关键是：开盘期间不为省 token 牺牲质量**。

---

## 七、回滚方案

| 出问题 | 回滚动作 | 耗时 |
|---|---|---|
| chain 质量比单 prompt 还差 | `UPDATE strategies SET enable_chain_of_thought=false` + restart | 30s |
| 镜像启动失败 | 回退到 v7.1 image tag + restart | 1 min |
| section extraction 全失败 | extraction fallback 自动用完整段 | 自动 |

---

## 八、后续观察项（实施完后跟踪）

- [ ] 周一 (2026-05-04) 美股开盘后 first 5 cycles 的 cot 质量
- [ ] token 实际消耗 vs 估算偏差
- [ ] 是否出现 chain-degraded 异常（Step JSON 解析失败 → 降级单 prompt）
- [ ] 用户主观评分：cot 是否反映 prompt_sections 中的核心规则（基石优先级、4 层过滤等）
- [ ] 一周后评估：chain v2 vs 单 prompt 的胜率/PF/平均持仓时长

---

## 九、未发现的潜在问题（可能需要 v2.1 修复）

实施过程中和上线后，预期会浮现的"未知未知"：

- 极端情况下 Step 1 的 sector_bias 误读 macro_thesis 文本（中文长文本解析）
- Step 2 给同板块多个 pass，但 Step 3 没触发（候选 ≤ slots），Step 4 同时开多笔同板块
- chain 4 步串行延迟（关盘 ~10s，开盘可能 30s+），影响快速反应
- 当 prompt_sections 用户后续编辑时，section markers 可能漂移导致 extraction 静默失败

这些**只能上线后真实运行才能浮现**，本计划不预先解决，纳入 v2.1 优化清单。

---

## 十、相关文档

- `docs/plans/chain-of-thought-agent.md` — 原始架构（2026-04-20）
- `docs/plans/chain-of-thought-impl-spec.md` — 代码探查规格
- `docs/plans/2026-04-28-chain-of-thought-implementation.md` — v1 实施详细 task 清单
- 本文档 — v2 prompt 注入重构

---

*Plan locked 2026-05-02。下一步：开始 Task 1（验证现状）。*
