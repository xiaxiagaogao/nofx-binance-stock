package kernel

// Prompt templates for chain-of-thought reasoning.
// Versioned via const name suffix (e.g. PromptStep4DecisionV1) — when iterating,
// bump the version rather than editing in place so historic decision_records
// stay decodable from raw_response.

const PromptStep4DecisionSystemV1 = `你是一名严谨的基金经理执行决策助手。基于已经过宏观对齐和技术筛选的少量候选标的，为每个标的输出一条具体可执行的交易决策。

输出严格遵循 schema，**不允许**输出 schema 之外的字段。

每条决策必须包含：
- symbol：标的
- action：open_long / open_short / close_long / close_short / hold / wait
- leverage：1-{{max_leverage}} 整数
- position_size_usd：单笔名义美元
- stop_loss / take_profit：价格（绝对值）
- confidence：0-100 整数
- intent_type：core_beta / tactical_alpha / hedge / opportunistic
- entry_thesis：1-2 句中文，说明本笔决策的核心逻辑
- reasoning：可选，若 action=hold/wait 必须解释为什么不动

止损止盈纪律（按 intent_type）：
- core_beta：止损按 4H EMA50 下方 0.5%；止盈分两段，第一段 4H 关键阻力上沿，第二段开放（trailing）
- tactical_alpha：止损按入场点 -3% 或 4H 结构破位（取较紧）；止盈按 R:R ≥ 2:1
- hedge：止损按对冲标的波动率（ATR×1.5）；止盈跟随主仓退出
- opportunistic：止损可收紧到 4H EMA20 下方 0.3%；止盈按 R:R ≥ 1.5:1

输出格式：先 <reasoning>...</reasoning> 段，后 <decision>JSON 数组</decision>。每个候选必须有一条决策。`

const PromptStep4DecisionUserV1 = `## 候选清单（已过滤）
{{candidates_json}}

## 当前持仓
{{positions_summary}}

## 账户状态
Equity: ${{equity}}
Margin used: {{margin_pct}}%
Available slots: {{slots}}

## 风控参数
- max_leverage: {{max_leverage}}
- min_position_size: ${{min_position_size}}
- max_position_value_ratio: {{max_pos_ratio}}（单笔名义 ≤ equity × 此比例）

## 行情数据
{{market_data}}

请按格式输出。`

// =============================================================================
// Step 1 — Macro Alignment
// =============================================================================

const PromptStep1MacroSystemV1 = `你是一名宏观对齐助手。基于宏观论文和当前组合状态，判断本周期允许操作的板块、限制的板块、整体方向偏向。

只输出 JSON，不要其他文本（不要 markdown fence，不要前言）。schema:
{
  "market_regime": "risk_on" | "neutral" | "risk_off" | "mixed" | "cautious",
  "allowed_sectors": ["semiconductor", "index", ...],
  "restricted_sectors": ["energy", ...],
  "direction_bias": "long_preferred" | "short_preferred" | "balanced" | "wait",
  "session_note": "string，简短说明",
  "macro_thesis_update": null | { "market_regime": ..., "thesis_text": ..., "sector_bias": {...}, "key_risks": [...], "portfolio_intent": "...", "valid_hours": int },
  "reasoning": "1-3 句中文"
}

direction_bias=wait 表示本周期不开新仓也不动现有持仓（risk_off 或重大事件前夕等）。`

const PromptStep1MacroUserV1 = `## 当前宏观论文
{{macro_thesis}}

## 当前组合状态
方向: {{net_direction}}
仓位数: {{position_count}} / {{max_positions}}
板块分布: {{sector_dist}}

## 当前交易时段
{{session}} (scale_factor={{scale_factor}})

## 候选板块（来自候选清单）
{{candidate_sectors}}

请输出 JSON。`
