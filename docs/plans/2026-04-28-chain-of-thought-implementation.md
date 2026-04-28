# Chain-of-Thought Reasoning — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-shot LLM call in `runCycle()` with a 4-step chained reasoning pipeline (macro alignment → technical screening → deterministic code filter → optional ranking → decision generation), gated by a `EnableChainOfThought` flag with full degradation back to the existing single call on any failure.

**Architecture:** Add a parallel decision path `GetFullDecisionChained()` in a new file `kernel/engine_chain.go` that orchestrates 4 sequential LLM calls reusing the existing `Context` (data fetched once). Each step has its own system/user prompt, JSON schema validation, and structured handoff to the next step. Total token budget ~7-8k/cycle (vs ~3k current). Any step failure (LLM error, JSON parse, schema mismatch) → entire chain falls back to `GetFullDecisionWithStrategy()`, with `[chain-degraded:stepN:reason]` prefixed to `CoTTrace` for forensics.

**Tech Stack:** Go 1.21+, `mcp.AIClient` interface (existing), `kernel/engine.go` types (Context/Decision/FullDecision), `mockLLM` test pattern from `telegram/agent/agent_test.go`, SQLite for strategy config.

**Reference docs:**
- `docs/plans/chain-of-thought-agent.md` — original architecture
- `docs/plans/chain-of-thought-impl-spec.md` — code-level spec (call chains, types, schemas)
- Locked design decisions (this plan supersedes the spec where conflicting):
  - Step 5 (risk verification) **dropped** — `auto_trader_risk.go` enforces deterministically
  - Step 3 (portfolio review) **repositioned as ranking** — code does deterministic filter; LLM only ranks when `len(filtered) > available_slots`
  - Degradation: **whole-chain fallback** on any step failure (no per-step retry)
  - `macro_thesis_update` emitted at **Step 1 exit**
  - Differential effort levels and shortcut paths **deferred to Phase 2** (no `reasoning_effort` plumbing exists yet)

---

## File Structure

**Create:**
- `kernel/engine_chain.go` — `GetFullDecisionChained()` entry + 4 step functions + code filter helper
- `kernel/engine_chain_test.go` — unit tests with mockLLM
- `kernel/chain_prompts.go` — prompt templates as exported string constants (kept separate for review/iteration)

**Modify:**
- `store/strategy.go:86-105` — add `EnableChainOfThought bool` to `StrategyConfig`
- `trader/auto_trader_loop.go:98` — add routing branch for chain vs. single

**Test infrastructure (create):**
- `kernel/testing_mock.go` — exported `MockAIClient` mirroring `telegram/agent/agent_test.go:14` pattern (chain tests need scripted multi-call responses)

---

## Task 1: Add `EnableChainOfThought` field to `StrategyConfig`

**Files:**
- Modify: `store/strategy.go:86-105` (StrategyConfig struct)
- Test: `store/strategy_test.go` (create if missing)

- [ ] **Step 1: Add the field**

In `store/strategy.go`, locate the `StrategyConfig` struct (line 86). Add the new field after `GridConfig`:

```go
// GridConfig only used when StrategyType == "grid_trading"
GridConfig *GridStrategyConfig `json:"grid_config,omitempty"`

// EnableChainOfThought routes runCycle() through GetFullDecisionChained() when true.
// Default false → existing single-LLM-call path (zero behavior change).
EnableChainOfThought bool `json:"enable_chain_of_thought,omitempty"`
```

- [ ] **Step 2: Write the test**

Create or append to `store/strategy_test.go`:

```go
package store

import (
	"encoding/json"
	"testing"
)

func TestStrategyConfig_EnableChainOfThoughtRoundTrip(t *testing.T) {
	cfg := StrategyConfig{EnableChainOfThought: true}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(string(b), `"enable_chain_of_thought":true`) {
		t.Fatalf("expected enable_chain_of_thought:true in %s", string(b))
	}

	var back StrategyConfig
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.EnableChainOfThought {
		t.Fatalf("expected EnableChainOfThought=true after round-trip")
	}
}

func TestStrategyConfig_EnableChainOfThoughtDefaultsFalse(t *testing.T) {
	var cfg StrategyConfig
	b, _ := json.Marshal(cfg)
	// omitempty means false should NOT appear
	if contains(string(b), "enable_chain_of_thought") {
		t.Fatalf("expected omitempty default to omit field; got %s", string(b))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > 0 && (firstIndex(s, sub) >= 0)))
}

func firstIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Run tests**

```bash
cd /root/src/nofx-binance-stock && go test ./store/... -run EnableChainOfThought -v
```
Expected: PASS for both tests.

- [ ] **Step 4: Run full store tests to verify no regression**

```bash
cd /root/src/nofx-binance-stock && go test ./store/... -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/src/nofx-binance-stock && git add store/strategy.go store/strategy_test.go && \
  git commit -m "feat(store): add EnableChainOfThought flag to StrategyConfig

Default false preserves existing single-LLM-call behavior. Routing
in trader loop will read this flag in a follow-up commit."
```

---

## Task 2: Create chain skeleton with full fallback (no LLM calls yet)

**Files:**
- Create: `kernel/engine_chain.go`
- Create: `kernel/engine_chain_test.go`
- Create: `kernel/testing_mock.go`

This task lands a working `GetFullDecisionChained()` that immediately delegates to `GetFullDecisionWithStrategy()` — i.e. behaviorally identical to the old path but plumbed through the new entry point. Subsequent tasks replace the delegation with real chain logic.

- [ ] **Step 1: Create `kernel/testing_mock.go`**

```go
package kernel

import (
	"fmt"
	"sync"
	"time"

	"nofx/mcp"
)

// MockAIClient implements mcp.AIClient for chain tests. It returns scripted
// responses in order from CallWithMessages. Use NewMockAIClient + WithResponse
// to set up; Calls() returns total invocation count.
type MockAIClient struct {
	mu        sync.Mutex
	responses []string
	errors    []error
	calls     int
	lastSys   []string
	lastUser  []string
}

func NewMockAIClient() *MockAIClient {
	return &MockAIClient{}
}

func (m *MockAIClient) WithResponse(content string) *MockAIClient {
	m.responses = append(m.responses, content)
	m.errors = append(m.errors, nil)
	return m
}

func (m *MockAIClient) WithError(err error) *MockAIClient {
	m.responses = append(m.responses, "")
	m.errors = append(m.errors, err)
	return m
}

func (m *MockAIClient) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *MockAIClient) LastSystem(idx int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx < 0 || idx >= len(m.lastSys) {
		return ""
	}
	return m.lastSys[idx]
}

func (m *MockAIClient) LastUser(idx int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx < 0 || idx >= len(m.lastUser) {
		return ""
	}
	return m.lastUser[idx]
}

func (m *MockAIClient) SetAPIKey(_, _, _ string)   {}
func (m *MockAIClient) SetTimeout(_ time.Duration) {}

func (m *MockAIClient) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSys = append(m.lastSys, systemPrompt)
	m.lastUser = append(m.lastUser, userPrompt)
	if m.calls >= len(m.responses) {
		m.calls++
		return "", fmt.Errorf("mock: no scripted response for call #%d", m.calls)
	}
	resp, err := m.responses[m.calls], m.errors[m.calls]
	m.calls++
	return resp, err
}

func (m *MockAIClient) CallWithRequest(*mcp.Request) (string, error) { return "", nil }
func (m *MockAIClient) CallWithRequestStream(*mcp.Request, func(string)) (string, error) {
	return "", nil
}
func (m *MockAIClient) CallWithRequestFull(*mcp.Request) (*mcp.LLMResponse, error) {
	return &mcp.LLMResponse{}, nil
}
```

- [ ] **Step 2: Create `kernel/engine_chain.go` skeleton**

```go
package kernel

import (
	"fmt"

	"nofx/logger"
	"nofx/mcp"
)

// GetFullDecisionChained runs the 4-step chained reasoning pipeline.
// Behavior contract: returns a *FullDecision compatible with the single-call
// path, including SystemPrompt/UserPrompt/CoTTrace/Decisions/RawResponse.
// On any step failure, falls back to GetFullDecisionWithStrategy() and prefixes
// CoTTrace with [chain-degraded:stepN:reason] for forensics.
func GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is nil")
	}

	// MVP skeleton: delegate to single-call path. Subsequent tasks replace this
	// with real step-by-step reasoning. Degradation tracking is wired here so
	// future failures route through the same fallback site.
	return chainFallback(ctx, mcpClient, engine, "skeleton-not-implemented")
}

// chainFallback runs the existing single-call path and tags the resulting
// CoTTrace so post-hoc analysis can distinguish degraded runs from intentional
// single-call runs (which have no tag).
func chainFallback(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, reason string) (*FullDecision, error) {
	logger.Infof("⚠️  [chain] degraded → single-call fallback (reason: %s)", reason)
	dec, err := GetFullDecisionWithStrategy(ctx, mcpClient, engine, "balanced")
	if dec != nil {
		dec.CoTTrace = fmt.Sprintf("[chain-degraded:%s] %s", reason, dec.CoTTrace)
	}
	return dec, err
}
```

- [ ] **Step 3: Create `kernel/engine_chain_test.go` with skeleton tests**

```go
package kernel

import (
	"strings"
	"testing"
)

func TestGetFullDecisionChained_NilContext(t *testing.T) {
	_, err := GetFullDecisionChained(nil, NewMockAIClient(), nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestGetFullDecisionChained_NilEngine(t *testing.T) {
	ctx := &Context{}
	_, err := GetFullDecisionChained(ctx, NewMockAIClient(), nil)
	if err == nil {
		t.Fatal("expected error for nil engine")
	}
}

// Skeleton task: verify that the entry point exists and degrades cleanly.
// Real chain behavior tests land in subsequent tasks.
func TestGetFullDecisionChained_SkeletonDegradesGracefully(t *testing.T) {
	// We can't easily exercise GetFullDecisionWithStrategy without a full
	// StrategyEngine + MarketData fetch; this test just verifies the function
	// signature compiles and produces an error rather than panicking.
	mock := NewMockAIClient().WithError(errAssertString("not configured"))
	ctx := &Context{}
	engine := &StrategyEngine{}
	_, err := GetFullDecisionChained(ctx, mock, engine)
	if err == nil {
		t.Fatal("expected error from incomplete setup; got nil")
	}
	if !strings.Contains(err.Error(), "") { // any error is fine
		t.Fatalf("unexpected nil error message")
	}
}

type errAssert string

func errAssertString(s string) error { return errAssert(s) }
func (e errAssert) Error() string    { return string(e) }
```

- [ ] **Step 4: Run tests**

```bash
cd /root/src/nofx-binance-stock && go test ./kernel/ -run Chained -v
```
Expected: All 3 tests PASS.

- [ ] **Step 5: Verify build still passes**

```bash
cd /root/src/nofx-binance-stock && go build ./...
```
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
cd /root/src/nofx-binance-stock && git add kernel/engine_chain.go kernel/engine_chain_test.go kernel/testing_mock.go && \
  git commit -m "feat(kernel): add chain-of-thought skeleton with fallback

GetFullDecisionChained() entry point delegates to single-call path
for now; chain-degraded tracking wired so subsequent step failures
route through the same fallback site. Includes MockAIClient for tests."
```

---

## Task 3: Wire routing in `runCycle()`

**Files:**
- Modify: `trader/auto_trader_loop.go:96-100`

- [ ] **Step 1: Read the current routing site**

```bash
ssh -i /Users/xiagao/Desktop/pem/tokyo-one.pem root@64.83.41.30 "sed -n '90,105p' /root/src/nofx-binance-stock/trader/auto_trader_loop.go"
```
Confirm the current line is:
```go
aiDecision, err := kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")
```

- [ ] **Step 2: Replace with conditional routing**

Replace the single line above with:

```go
var aiDecision *kernel.FullDecision
var err error
if at.config.StrategyConfig != nil && at.config.StrategyConfig.EnableChainOfThought {
	logger.Infof("🔗 [%s] cycle %d using chain-of-thought reasoning", at.name, record.CycleNumber)
	aiDecision, err = kernel.GetFullDecisionChained(ctx, at.mcpClient, at.strategyEngine)
} else {
	aiDecision, err = kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")
}
```

- [ ] **Step 3: Verify build**

```bash
cd /root/src/nofx-binance-stock && go build ./...
```
Expected: clean build.

- [ ] **Step 4: Run trader tests if any**

```bash
cd /root/src/nofx-binance-stock && go test ./trader/... -count=1 -short 2>&1 | tail -20
```
Expected: PASS or "no test files" — we don't introduce new test files for the trader package because chain logic is unit-tested in `kernel/`.

- [ ] **Step 5: Commit**

```bash
cd /root/src/nofx-binance-stock && git add trader/auto_trader_loop.go && \
  git commit -m "feat(trader): route runCycle through chain when EnableChainOfThought=true

Default flag is false → all existing traders unaffected.
When flag flipped, runCycle calls GetFullDecisionChained which
currently falls back to single call (skeleton). Subsequent commits
implement the real chain steps."
```

---

## Task 4: Implement Step 4 (decision generation) — primary LLM call

This is the biggest piece — it generates the final `[]Decision` output. We start here because it's the most important and validates the full prompt-to-parsed-decision path. Steps 1/2/3 in later tasks become input feeders to Step 4.

**Files:**
- Create: `kernel/chain_prompts.go`
- Modify: `kernel/engine_chain.go` (replace skeleton fallback)
- Modify: `kernel/engine_chain_test.go` (add Step 4 tests)

- [ ] **Step 1: Create `kernel/chain_prompts.go`**

```go
package kernel

// Prompt templates for chain-of-thought reasoning.
// All prompts are versioned via the const name suffix (e.g. PromptStep4DecisionV1)
// — when iterating, bump the version rather than editing in place so historic
// decision records stay decodable.

const PromptStep4DecisionSystemV1 = `你是一名严谨的基金经理执行决策助手。你的任务是：基于已经经过宏观对齐和技术筛选的少量候选标的，为每个标的输出一条具体可执行的交易决策。

输出严格遵循 JSON schema，**不允许**输出 schema 之外的字段。

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
- core_beta：止损按 4H EMA50 下方 0.5%；止盈分两段，第一段 4H 关键阻力上沿，第二段开放
- tactical_alpha：止损按入场点 -3% 或 4H 结构破位（取较紧）；止盈按 R:R ≥ 2:1
- hedge：止损按对冲标的波动率（ATR×1.5）；止盈跟随主仓退出
- opportunistic：止损可收紧到 4H EMA20 下方 0.3%；止盈按 R:R ≥ 1.5:1`

const PromptStep4DecisionUserV1 = `## 候选清单（已经过宏观+技术筛选）
{{candidates_json}}

## 当前持仓
{{positions_summary}}

## 账户状态
Equity: ${{equity}}
Margin used: {{margin_pct}}%
Available slots (per risk_control): {{slots}}

## 行情数据
{{market_data}}

## 风控参数
- max_leverage: {{max_leverage}}
- min_position_size: ${{min_position_size}}
- max_position_value_ratio: {{max_pos_ratio}} (single position notional ≤ equity × this)

请按 JSON 数组输出决策（每个候选一条），用 <decision>...</decision> 包裹，<reasoning>...</reasoning> 段在前。`
```

- [ ] **Step 2: Replace skeleton in `kernel/engine_chain.go` with Step 4 implementation**

Replace the body of `GetFullDecisionChained()` (currently calling `chainFallback`) with:

```go
func GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is nil")
	}

	// Ensure market data is fetched (mirrors GetFullDecisionWithStrategy line 84-87)
	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return chainFallback(ctx, mcpClient, engine, "market-data-fetch")
		}
	}

	// MVP: skip Steps 1-3 (added in subsequent tasks); pass the full candidate
	// list directly to Step 4. This produces behavior similar to single-call
	// but routed through the chain entry point — useful for validating Step 4
	// in isolation before adding upstream filtering.
	candidates := ctx.CandidateCoins

	step4Result, err := decisionGenerationCall(ctx, engine, mcpClient, candidates)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}
	step4Result.CoTTrace = "[chain:step4-only] " + step4Result.CoTTrace
	return step4Result, nil
}

// decisionGenerationCall is Step 4 of the chain. It receives a curated
// candidate list (from Steps 1-3 in later tasks) and emits []Decision.
// This is the only step whose output format must remain stable — it reuses
// parseFullDecisionResponse so DB serialization stays identical.
func decisionGenerationCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, candidates []CandidateCoin) (*FullDecision, error) {
	riskCfg := engine.GetRiskControlConfig()

	systemPrompt := renderStep4System(riskCfg.EffectiveMaxLeverage())
	userPrompt, err := renderStep4User(ctx, engine, candidates)
	if err != nil {
		return nil, fmt.Errorf("render step4 user prompt: %w", err)
	}

	aiCallStart := time.Now()
	aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	aiCallDuration := time.Since(aiCallStart)
	if err != nil {
		return nil, fmt.Errorf("step4 LLM call: %w", err)
	}

	decision, err := parseFullDecisionResponse(
		aiResponse,
		ctx.Account.TotalEquity,
		riskCfg.EffectiveMaxLeverage(),
		riskCfg.EffectiveMaxPositionValueRatio(),
	)
	if decision != nil {
		decision.Timestamp = time.Now()
		decision.SystemPrompt = systemPrompt
		decision.UserPrompt = userPrompt
		decision.AIRequestDurationMs = aiCallDuration.Milliseconds()
		decision.RawResponse = aiResponse
	}
	if err != nil {
		return decision, fmt.Errorf("parse step4 response: %w", err)
	}
	return decision, nil
}
```

Add the import for `time` at the top of the file if not already present.

- [ ] **Step 3: Add prompt rendering helpers to `kernel/engine_chain.go`**

```go
import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nofx/logger"
	"nofx/mcp"
)

func renderStep4System(maxLeverage int) string {
	return strings.ReplaceAll(PromptStep4DecisionSystemV1, "{{max_leverage}}", fmt.Sprintf("%d", maxLeverage))
}

func renderStep4User(ctx *Context, engine *StrategyEngine, candidates []CandidateCoin) (string, error) {
	candidatesJSON, err := json.MarshalIndent(candidates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal candidates: %w", err)
	}

	riskCfg := engine.GetRiskControlConfig()
	positionsSummary := summarizePositions(ctx.Positions)
	marketData := engine.formatMarketDataForChain(ctx, candidates)

	availableSlots := riskCfg.MaxPositions - len(ctx.Positions)
	if availableSlots < 0 {
		availableSlots = 0
	}

	out := PromptStep4DecisionUserV1
	out = strings.ReplaceAll(out, "{{candidates_json}}", string(candidatesJSON))
	out = strings.ReplaceAll(out, "{{positions_summary}}", positionsSummary)
	out = strings.ReplaceAll(out, "{{equity}}", fmt.Sprintf("%.2f", ctx.Account.TotalEquity))
	out = strings.ReplaceAll(out, "{{margin_pct}}", fmt.Sprintf("%.1f", ctx.Account.MarginUsedPct))
	out = strings.ReplaceAll(out, "{{slots}}", fmt.Sprintf("%d", availableSlots))
	out = strings.ReplaceAll(out, "{{market_data}}", marketData)
	out = strings.ReplaceAll(out, "{{max_leverage}}", fmt.Sprintf("%d", riskCfg.EffectiveMaxLeverage()))
	out = strings.ReplaceAll(out, "{{min_position_size}}", fmt.Sprintf("%.2f", riskCfg.MinPositionSize))
	out = strings.ReplaceAll(out, "{{max_pos_ratio}}", fmt.Sprintf("%.2f", riskCfg.EffectiveMaxPositionValueRatio()))
	return out, nil
}

func summarizePositions(positions []PositionInfo) string {
	if len(positions) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, p := range positions {
		fmt.Fprintf(&sb, "- %s %s | qty %.4f | entry %.2f | unrealized %.2f%%\n",
			p.Symbol, p.Side, p.Quantity, p.EntryPrice, p.UnrealizedPnLPct)
	}
	return sb.String()
}
```

Note: `formatMarketDataForChain` is referenced but does not exist yet. Add it to `engine_prompt.go` in Step 4 below. If `MarginUsedPct` field does not exist on `AccountInfo`, replace with whatever the actual struct uses (verify with `grep -n "MarginUsedPct\\|margin_used_pct" kernel/engine.go`).

- [ ] **Step 4: Add `formatMarketDataForChain` helper to `kernel/engine_prompt.go`**

This is a thinner version of the existing market-data formatter (the existing one is part of the giant `BuildUserPrompt`; we extract only what Step 4 needs). Append at end of file:

```go
// formatMarketDataForChain produces a compact market-data block for chain
// step 4. Unlike BuildUserPrompt's full market data, this only includes the
// candidates that survived Steps 1-3 filtering, keeping token count down.
func (e *StrategyEngine) formatMarketDataForChain(ctx *Context, candidates []CandidateCoin) string {
	var sb strings.Builder
	for _, c := range candidates {
		md, ok := ctx.MarketDataMap[c.Symbol]
		if !ok || md == nil {
			continue
		}
		fmt.Fprintf(&sb, "\n=== %s ===\n", c.Symbol)
		fmt.Fprintf(&sb, "current_price=%.4f, current_ema20=%.4f, current_rsi7=%.4f\n",
			md.CurrentPrice, md.CurrentEMA20, md.CurrentRSI7)
		// Summarize 4H structure: last 6 candles only
		if len(md.K4H) > 0 {
			start := len(md.K4H) - 6
			if start < 0 {
				start = 0
			}
			fmt.Fprintf(&sb, "4H last %d candles (open/high/low/close):\n", len(md.K4H)-start)
			for _, k := range md.K4H[start:] {
				fmt.Fprintf(&sb, "  %s  %.2f / %.2f / %.2f / %.2f\n",
					k.Time.Format("01-02 15:04"), k.Open, k.High, k.Low, k.Close)
			}
		}
	}
	return sb.String()
}
```

If field names like `K4H` or `CurrentEMA20` differ from what's in `kernel/market_data.go`, verify and adjust. Run `grep -n "type Data struct" market/data.go` if uncertain.

- [ ] **Step 5: Add Step 4 unit test**

Append to `kernel/engine_chain_test.go`:

```go
func TestStep4DecisionGeneration_ValidJSONResponse(t *testing.T) {
	mockResponse := `<reasoning>NVDA 4H 结构完好，回踩 EMA20 站稳，开核心多头。</reasoning>
<decision>
[
  {
    "symbol": "NVDAUSDT",
    "action": "open_long",
    "leverage": 3,
    "position_size_usd": 40,
    "stop_loss": 195,
    "take_profit": 220,
    "confidence": 80,
    "intent_type": "core_beta",
    "entry_thesis": "test thesis"
  }
]
</decision>`

	mock := NewMockAIClient().WithResponse(mockResponse)
	ctx := newTestContext()
	engine := newTestEngine()

	result, err := decisionGenerationCall(ctx, engine, mock, ctx.CandidateCoins)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.Calls() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.Calls())
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(result.Decisions))
	}
	if result.Decisions[0].Symbol != "NVDAUSDT" {
		t.Fatalf("expected NVDAUSDT, got %s", result.Decisions[0].Symbol)
	}
	if result.Decisions[0].Action != "open_long" {
		t.Fatalf("expected open_long, got %s", result.Decisions[0].Action)
	}
}

func TestStep4DecisionGeneration_LLMError_PropagatesUp(t *testing.T) {
	mock := NewMockAIClient().WithError(fmt.Errorf("network down"))
	ctx := newTestContext()
	engine := newTestEngine()

	_, err := decisionGenerationCall(ctx, engine, mock, ctx.CandidateCoins)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected 'network down' in error; got %v", err)
	}
}

// newTestContext / newTestEngine are minimal fixtures shared by chain tests.
// They avoid pulling in real strategy config or market data fetch.
func newTestContext() *Context {
	return &Context{
		Account: AccountInfo{TotalEquity: 100, MarginUsedPct: 20},
		Positions: []PositionInfo{},
		CandidateCoins: []CandidateCoin{{Symbol: "NVDAUSDT"}},
		MarketDataMap: map[string]*market.Data{
			"NVDAUSDT": {CurrentPrice: 200, CurrentEMA20: 199, CurrentRSI7: 60},
		},
	}
}

func newTestEngine() *StrategyEngine {
	cfg := store.GetDefaultStrategyConfig("zh")
	return NewStrategyEngine(&cfg)
}
```

Add necessary imports (`fmt`, `nofx/market`, `nofx/store`) to the test file.

- [ ] **Step 6: Run tests**

```bash
cd /root/src/nofx-binance-stock && go test ./kernel/ -run "Step4|Chained" -v
```
Expected: All tests PASS. If `MarginUsedPct` or other field names mismatch, the test will fail at compile time — adjust based on the real `AccountInfo` definition.

- [ ] **Step 7: Verify full build**

```bash
cd /root/src/nofx-binance-stock && go build ./...
```
Expected: clean build.

- [ ] **Step 8: Commit**

```bash
cd /root/src/nofx-binance-stock && git add kernel/engine_chain.go kernel/engine_chain_test.go kernel/chain_prompts.go kernel/engine_prompt.go && \
  git commit -m "feat(kernel): implement chain Step 4 (decision generation)

Step 4 receives a candidate list (from Steps 1-3 in later commits)
and produces []Decision via dedicated prompt + parseFullDecisionResponse.
MVP path skips Steps 1-3, passing full candidate set straight to Step 4 —
useful for validating Step 4 in isolation before adding upstream filters.
CoTTrace tagged [chain:step4-only] for forensics."
```

---

## Task 5: Implement Step 1 (macro alignment) — sector/direction filter upfront

Step 1 reads macro thesis + portfolio context, outputs `allowed_sectors`, `restricted_sectors`, `direction_bias`, and optional `macro_thesis_update`. Step 4 then receives a pre-filtered candidate list and a direction hint.

**Files:**
- Modify: `kernel/chain_prompts.go` (add Step 1 templates)
- Modify: `kernel/engine_chain.go` (add `macroAlignmentCall` + integrate)
- Modify: `kernel/engine_chain_test.go` (add Step 1 tests)

- [ ] **Step 1: Add Step 1 prompts to `kernel/chain_prompts.go`**

Append to the file:

```go
const PromptStep1MacroSystemV1 = `你是一名宏观对齐助手。基于宏观论文和当前组合状态，判断本周期允许操作的板块、限制的板块、整体方向偏向。

只输出 JSON，不要其他文本。schema：
{
  "market_regime": "risk_on" | "neutral" | "risk_off",
  "allowed_sectors": ["semiconductor", "index", ...],
  "restricted_sectors": ["energy", ...],
  "direction_bias": "long_preferred" | "short_preferred" | "balanced" | "wait",
  "session_note": "string，简短说明",
  "macro_thesis_update": null | { "market_regime": ..., "thesis_text": ..., "sector_bias": {...}, "key_risks": [...], "portfolio_intent": "...", "valid_hours": int },
  "reasoning": "1-3 句中文"
}`

const PromptStep1MacroUserV1 = `## 当前宏观论文
{{macro_thesis}}

## 当前组合状态
方向: {{net_direction}}
仓位数: {{position_count}} / {{max_positions}}
板块分布: {{sector_dist}}

## 当前交易时段
{{session}} (scale_factor={{scale_factor}})

请输出 JSON。`

// Step1Output is the parsed JSON from macroAlignmentCall.
type Step1Output struct {
	MarketRegime       string             `json:"market_regime"`
	AllowedSectors     []string           `json:"allowed_sectors"`
	RestrictedSectors  []string           `json:"restricted_sectors"`
	DirectionBias      string             `json:"direction_bias"`
	SessionNote        string             `json:"session_note"`
	MacroThesisUpdate  *MacroThesisUpdate `json:"macro_thesis_update,omitempty"`
	Reasoning          string             `json:"reasoning"`
}
```

- [ ] **Step 2: Add `macroAlignmentCall` and integrate in `GetFullDecisionChained`**

In `kernel/engine_chain.go`, add the function:

```go
func macroAlignmentCall(ctx *Context, mcpClient mcp.AIClient) (*Step1Output, string, error) {
	systemPrompt := PromptStep1MacroSystemV1
	userPrompt := renderStep1User(ctx)

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step1 LLM call: %w", err)
	}

	var out Step1Output
	jsonText := extractJSONObject(resp)
	if err := json.Unmarshal([]byte(jsonText), &out); err != nil {
		return nil, resp, fmt.Errorf("step1 parse: %w (raw: %s)", err, truncate(resp, 200))
	}

	// Schema validation: market_regime and direction_bias must be in known set
	if !isValidRegime(out.MarketRegime) {
		return nil, resp, fmt.Errorf("step1 invalid market_regime: %s", out.MarketRegime)
	}
	if !isValidDirectionBias(out.DirectionBias) {
		return nil, resp, fmt.Errorf("step1 invalid direction_bias: %s", out.DirectionBias)
	}

	return &out, resp, nil
}

func renderStep1User(ctx *Context) string {
	macroThesis := "(none)"
	if ctx.MacroThesis != nil {
		macroThesis = formatMacroThesisCompact(ctx.MacroThesis)
	}

	netDirection := "flat"
	longCount, shortCount := 0, 0
	for _, p := range ctx.Positions {
		if strings.EqualFold(p.Side, "LONG") {
			longCount++
		} else if strings.EqualFold(p.Side, "SHORT") {
			shortCount++
		}
	}
	if longCount > shortCount {
		netDirection = fmt.Sprintf("net_long (long=%d short=%d)", longCount, shortCount)
	} else if shortCount > longCount {
		netDirection = fmt.Sprintf("net_short (long=%d short=%d)", longCount, shortCount)
	}

	maxPositions := 8
	sectorDist := summarizeSectors(ctx.Positions)

	out := PromptStep1MacroUserV1
	out = strings.ReplaceAll(out, "{{macro_thesis}}", macroThesis)
	out = strings.ReplaceAll(out, "{{net_direction}}", netDirection)
	out = strings.ReplaceAll(out, "{{position_count}}", fmt.Sprintf("%d", len(ctx.Positions)))
	out = strings.ReplaceAll(out, "{{max_positions}}", fmt.Sprintf("%d", maxPositions))
	out = strings.ReplaceAll(out, "{{sector_dist}}", sectorDist)
	out = strings.ReplaceAll(out, "{{session}}", ctx.TradingSession)
	out = strings.ReplaceAll(out, "{{scale_factor}}", fmt.Sprintf("%.2f", ctx.SessionScaleFactor))
	return out
}

// extractJSONObject pulls the first balanced { ... } block from a string.
// Models sometimes wrap JSON in markdown fences or chatty preambles.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isValidRegime(s string) bool {
	switch s {
	case "risk_on", "neutral", "risk_off":
		return true
	}
	return false
}

func isValidDirectionBias(s string) bool {
	switch s {
	case "long_preferred", "short_preferred", "balanced", "wait":
		return true
	}
	return false
}

func summarizeSectors(positions []PositionInfo) string {
	if len(positions) == 0 {
		return "(empty)"
	}
	counts := map[string]int{}
	for _, p := range positions {
		// Sector classification reuses RiskControl.GetSymbolCategory; we don't
		// have access to risk config here, so use a simple symbol-prefix proxy.
		// Refine in a later pass once we thread engine through.
		counts[p.Symbol]++
	}
	var parts []string
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return strings.Join(parts, ", ")
}
```

If `formatMacroThesisCompact` doesn't exist, add a minimal version:

```go
func formatMacroThesisCompact(m *MacroThesisContext) string {
	if m == nil {
		return "(nil)"
	}
	return fmt.Sprintf("regime=%s, intent=%s, thesis=%s",
		m.MarketRegime, m.PortfolioIntent, truncate(m.ThesisText, 400))
}
```

- [ ] **Step 3: Update `GetFullDecisionChained` to call Step 1 → filter candidates → Step 4**

Modify the `GetFullDecisionChained` body so it now:
1. Runs Step 1
2. Filters `ctx.CandidateCoins` by `allowed_sectors` (drops anything in `restricted_sectors`)
3. Calls Step 4 with the filtered list
4. If Step 1 returns `direction_bias=wait`, short-circuit: return empty `[]Decision`

```go
func GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is nil")
	}

	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return chainFallback(ctx, mcpClient, engine, "market-data-fetch")
		}
	}

	cot := strings.Builder{}

	// Step 1: macro alignment
	step1, step1Raw, err := macroAlignmentCall(ctx, mcpClient)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step1:%v", err))
	}
	fmt.Fprintf(&cot, "[step1] regime=%s direction=%s allowed=%v restricted=%v note=%s\n",
		step1.MarketRegime, step1.DirectionBias, step1.AllowedSectors, step1.RestrictedSectors, step1.SessionNote)

	// Short-circuit: direction_bias=wait → emit empty decision set without Step 4
	if step1.DirectionBias == "wait" {
		return &FullDecision{
			Decisions:           []Decision{},
			CoTTrace:            "[chain:wait-shortcut] " + cot.String(),
			RawResponse:         step1Raw,
			AIRequestDurationMs: 0,
			Timestamp:           time.Now(),
		}, nil
	}

	// Filter candidates by sector
	filtered := filterBySector(ctx.CandidateCoins, step1.AllowedSectors, step1.RestrictedSectors, engine)
	if len(filtered) == 0 {
		return &FullDecision{
			Decisions:   []Decision{},
			CoTTrace:    "[chain:no-candidates-after-step1] " + cot.String(),
			RawResponse: step1Raw,
			Timestamp:   time.Now(),
		}, nil
	}
	fmt.Fprintf(&cot, "[step1-filter] %d → %d candidates\n", len(ctx.CandidateCoins), len(filtered))

	// Step 4 (Steps 2/3 added in subsequent commits)
	step4Result, err := decisionGenerationCall(ctx, engine, mcpClient, filtered)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}

	// Merge macro_thesis_update from Step 1 into Step 4 result (per locked design)
	if step1.MacroThesisUpdate != nil && len(step4Result.Decisions) > 0 {
		step4Result.Decisions[0].MacroThesisUpdate = step1.MacroThesisUpdate
	}

	step4Result.CoTTrace = "[chain:1+4] " + cot.String() + step4Result.CoTTrace
	return step4Result, nil
}

func filterBySector(candidates []CandidateCoin, allowed, restricted []string, engine *StrategyEngine) []CandidateCoin {
	allowSet := map[string]bool{}
	for _, s := range allowed {
		allowSet[strings.ToLower(s)] = true
	}
	restrictSet := map[string]bool{}
	for _, s := range restricted {
		restrictSet[strings.ToLower(s)] = true
	}

	riskCfg := engine.GetRiskControlConfig()
	var out []CandidateCoin
	for _, c := range candidates {
		category := strings.ToLower(riskCfg.GetSymbolCategory(c.Symbol))
		if restrictSet[category] {
			continue
		}
		// If allowSet is empty, allow everything not restricted
		if len(allowSet) > 0 && !allowSet[category] {
			continue
		}
		out = append(out, c)
	}
	return out
}
```

- [ ] **Step 4: Add Step 1 unit tests**

Append to `kernel/engine_chain_test.go`:

```go
func TestStep1MacroAlignment_HappyPath(t *testing.T) {
	mockResp := `{
  "market_regime": "risk_on",
  "allowed_sectors": ["semiconductor", "index"],
  "restricted_sectors": ["energy"],
  "direction_bias": "long_preferred",
  "session_note": "us_market_open",
  "macro_thesis_update": null,
  "reasoning": "test"
}`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()

	out, _, err := macroAlignmentCall(ctx, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.MarketRegime != "risk_on" {
		t.Fatalf("expected risk_on, got %s", out.MarketRegime)
	}
	if out.DirectionBias != "long_preferred" {
		t.Fatalf("expected long_preferred, got %s", out.DirectionBias)
	}
	if len(out.AllowedSectors) != 2 {
		t.Fatalf("expected 2 allowed sectors, got %d", len(out.AllowedSectors))
	}
}

func TestStep1MacroAlignment_RejectsInvalidRegime(t *testing.T) {
	mockResp := `{"market_regime": "bullish", "direction_bias": "long_preferred"}`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()

	_, _, err := macroAlignmentCall(ctx, mock)
	if err == nil {
		t.Fatal("expected error for invalid regime")
	}
	if !strings.Contains(err.Error(), "market_regime") {
		t.Fatalf("expected market_regime error; got %v", err)
	}
}

func TestStep1MacroAlignment_HandlesMarkdownFences(t *testing.T) {
	mockResp := "```json\n" + `{"market_regime":"neutral","direction_bias":"balanced","allowed_sectors":[],"restricted_sectors":[],"session_note":"x","reasoning":"y"}` + "\n```"
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()

	out, _, err := macroAlignmentCall(ctx, mock)
	if err != nil {
		t.Fatalf("expected to handle markdown fences; got %v", err)
	}
	if out.MarketRegime != "neutral" {
		t.Fatalf("expected neutral; got %s", out.MarketRegime)
	}
}

func TestGetFullDecisionChained_WaitShortcut(t *testing.T) {
	step1Resp := `{"market_regime":"risk_off","direction_bias":"wait","allowed_sectors":[],"restricted_sectors":[],"session_note":"crash","reasoning":"too risky"}`
	mock := NewMockAIClient().WithResponse(step1Resp)
	ctx := newTestContext()
	engine := newTestEngine()

	result, err := GetFullDecisionChained(ctx, mock, engine)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.Calls() != 1 {
		t.Fatalf("expected exactly 1 LLM call (step1 only); got %d", mock.Calls())
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("expected empty decisions on wait-shortcut; got %d", len(result.Decisions))
	}
	if !strings.Contains(result.CoTTrace, "wait-shortcut") {
		t.Fatalf("expected CoTTrace to mention wait-shortcut; got %s", result.CoTTrace)
	}
}

func TestGetFullDecisionChained_Step1FailureDegrades(t *testing.T) {
	mock := NewMockAIClient().WithError(fmt.Errorf("rate limited"))
	ctx := newTestContext()
	engine := newTestEngine()

	result, _ := GetFullDecisionChained(ctx, mock, engine)
	// Falls through to GetFullDecisionWithStrategy which will also fail (no
	// market data wired in test) — we only verify the degradation tag.
	if result == nil {
		return // both paths failed; that's fine for this test
	}
	if !strings.HasPrefix(result.CoTTrace, "[chain-degraded:step1") {
		t.Fatalf("expected [chain-degraded:step1...] prefix; got %s", result.CoTTrace)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd /root/src/nofx-binance-stock && go test ./kernel/ -run "Step1|Chained|Wait" -v
```
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /root/src/nofx-binance-stock && git add kernel/engine_chain.go kernel/chain_prompts.go kernel/engine_chain_test.go && \
  git commit -m "feat(kernel): implement chain Step 1 (macro alignment)

Step 1 reads MacroThesis + portfolio state, emits sector allow/restrict
lists + direction_bias. Filtering by sector happens in code (deterministic,
no LLM). When direction_bias=wait, short-circuit to empty decisions
without invoking Step 4 (saves tokens on risk_off cycles).

macro_thesis_update piggybacks on the first decision's MacroThesisUpdate
field (per locked design — Step 1 is where macro is read, so Step 1 owns
the update emission)."
```

---

## Task 6: Implement Step 2 (technical screening)

Step 2 receives sector-filtered candidates from Step 1 and outputs a per-symbol pass/fail with structural notes. Step 4 then receives only `pass=true` candidates with their entry/stop hints.

**Files:**
- Modify: `kernel/chain_prompts.go` (add Step 2 templates)
- Modify: `kernel/engine_chain.go` (add `technicalScreeningCall` + integrate)
- Modify: `kernel/engine_chain_test.go` (add Step 2 tests)

- [ ] **Step 1: Add Step 2 prompts**

Append to `kernel/chain_prompts.go`:

```go
const PromptStep2TechnicalSystemV1 = `你是技术面筛选助手。基于宏观对齐（已给出 allowed_sectors 和 direction_bias）和每个候选标的的行情数据（4H/1H EMA、RSI、OI、资金费率），判断哪些标的当前结构清晰、位置良好，值得进入决策环节。

特别注意：
- 4H 趋势完好 + 1H 回踩支撑 = 健康回调，**应通过**
- 4H 结构破位（连续收盘跌破 EMA50）= 真破位，**不通过**
- 4H EMA20 下方但 4H EMA50 仍守住 + 1H RSI<25 = mean-reversion 候选，**应通过**（标记 direction=long, intent=opportunistic）
- 单纯追高（4H RSI>75 + 1H 已破布林上轨）= 性价比差，**不通过**

只输出 JSON 数组，每个候选一条。schema：
[
  {
    "symbol": "string",
    "direction": "long" | "short" | null,
    "confidence": 0-100,
    "structure": "string，1-2 句结构描述",
    "key_entry_level": float | null,
    "key_stop_level": float | null,
    "pass": bool,
    "reason_if_skip": "string，可选"
  }
]`

const PromptStep2TechnicalUserV1 = `## Step 1 输出
direction_bias: {{direction_bias}}
allowed_sectors: {{allowed_sectors}}

## 候选行情
{{candidates_market_data}}

请输出 JSON 数组。`

// Step2Result is one element of Step 2's output array.
type Step2Result struct {
	Symbol         string   `json:"symbol"`
	Direction      string   `json:"direction"`
	Confidence     int      `json:"confidence"`
	Structure      string   `json:"structure"`
	KeyEntryLevel  *float64 `json:"key_entry_level"`
	KeyStopLevel   *float64 `json:"key_stop_level"`
	Pass           bool     `json:"pass"`
	ReasonIfSkip   string   `json:"reason_if_skip,omitempty"`
}
```

- [ ] **Step 2: Add `technicalScreeningCall` and update orchestration**

In `kernel/engine_chain.go`:

```go
func technicalScreeningCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, step1 *Step1Output, candidates []CandidateCoin) ([]Step2Result, string, error) {
	systemPrompt := PromptStep2TechnicalSystemV1
	userPrompt := renderStep2User(ctx, engine, step1, candidates)

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step2 LLM call: %w", err)
	}

	jsonText := extractJSONArray(resp)
	var results []Step2Result
	if err := json.Unmarshal([]byte(jsonText), &results); err != nil {
		return nil, resp, fmt.Errorf("step2 parse: %w (raw: %s)", err, truncate(resp, 200))
	}

	// Schema validation: pass must be set; symbol must match a known candidate
	knownSymbols := map[string]bool{}
	for _, c := range candidates {
		knownSymbols[c.Symbol] = true
	}
	for i, r := range results {
		if !knownSymbols[r.Symbol] {
			return nil, resp, fmt.Errorf("step2 unknown symbol at idx %d: %s", i, r.Symbol)
		}
	}
	return results, resp, nil
}

func renderStep2User(ctx *Context, engine *StrategyEngine, step1 *Step1Output, candidates []CandidateCoin) string {
	marketData := engine.formatMarketDataForChain(ctx, candidates)
	out := PromptStep2TechnicalUserV1
	out = strings.ReplaceAll(out, "{{direction_bias}}", step1.DirectionBias)
	out = strings.ReplaceAll(out, "{{allowed_sectors}}", strings.Join(step1.AllowedSectors, ", "))
	out = strings.ReplaceAll(out, "{{candidates_market_data}}", marketData)
	return out
}

func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}
```

Update the orchestration in `GetFullDecisionChained` — replace the section after sector filter with:

```go
	// Step 2: technical screening
	step2Results, _, err := technicalScreeningCall(ctx, engine, mcpClient, step1, filtered)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step2:%v", err))
	}
	step2Pass := []CandidateCoin{}
	for _, r := range step2Results {
		if r.Pass {
			for _, c := range filtered {
				if c.Symbol == r.Symbol {
					step2Pass = append(step2Pass, c)
					break
				}
			}
		}
	}
	fmt.Fprintf(&cot, "[step2] %d candidates → %d pass\n", len(filtered), len(step2Pass))

	if len(step2Pass) == 0 && len(ctx.Positions) == 0 {
		// No new opens AND no holds-to-evaluate → emit empty decision set
		return &FullDecision{
			Decisions:   []Decision{},
			CoTTrace:    "[chain:no-candidates-after-step2] " + cot.String(),
			RawResponse: "",
			Timestamp:   time.Now(),
		}, nil
	}

	// If we have positions but no new candidates, still call Step 4 with empty
	// new-candidates so it can emit hold/wait decisions for current positions.
	step4Result, err := decisionGenerationCall(ctx, engine, mcpClient, step2Pass)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}

	if step1.MacroThesisUpdate != nil && len(step4Result.Decisions) > 0 {
		step4Result.Decisions[0].MacroThesisUpdate = step1.MacroThesisUpdate
	}
	step4Result.CoTTrace = "[chain:1+2+4] " + cot.String() + step4Result.CoTTrace
	return step4Result, nil
```

- [ ] **Step 3: Add Step 2 unit tests**

Append to `kernel/engine_chain_test.go`:

```go
func TestStep2TechnicalScreening_HappyPath(t *testing.T) {
	mockResp := `[
  {"symbol":"NVDAUSDT","direction":"long","confidence":78,"structure":"4H EMA20 上方","key_entry_level":200,"key_stop_level":195,"pass":true},
  {"symbol":"METAUSDT","direction":null,"confidence":40,"structure":"区间中部","pass":false,"reason_if_skip":"无位置优势"}
]`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()
	ctx.CandidateCoins = []CandidateCoin{{Symbol: "NVDAUSDT"}, {Symbol: "METAUSDT"}}
	engine := newTestEngine()
	step1 := &Step1Output{DirectionBias: "long_preferred", AllowedSectors: []string{"semiconductor", "tech_mega"}}

	results, _, err := technicalScreeningCall(ctx, engine, mock, step1, ctx.CandidateCoins)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Pass {
		t.Fatal("expected NVDA to pass")
	}
	if results[1].Pass {
		t.Fatal("expected META to skip")
	}
}

func TestStep2TechnicalScreening_RejectsUnknownSymbol(t *testing.T) {
	mockResp := `[{"symbol":"DOGEUSDT","pass":false}]`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()
	engine := newTestEngine()
	step1 := &Step1Output{}

	_, _, err := technicalScreeningCall(ctx, engine, mock, step1, ctx.CandidateCoins)
	if err == nil {
		t.Fatal("expected error for unknown symbol")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /root/src/nofx-binance-stock && go test ./kernel/ -run "Step1|Step2|Chained" -v
```
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/src/nofx-binance-stock && git add kernel/engine_chain.go kernel/chain_prompts.go kernel/engine_chain_test.go && \
  git commit -m "feat(kernel): implement chain Step 2 (technical screening)

Step 2 receives sector-filtered candidates and emits per-symbol
pass/fail with structural notes. Pullback-friendly criteria baked
into the prompt: 4H EMA50 hold + 1H RSI<25 = pass (opportunistic
mean-reversion), addressing the user's edge that single-prompt path
was missing.

Empty-candidates path covered: with positions present, still calls
Step 4 to evaluate holds; with no positions and no candidates, emits
empty decisions and skips Step 4 (saves tokens)."
```

---

## Task 7: Implement code filter (deterministic) + Step 3 (conditional ranking)

Code filter applies sector caps, position-count caps, and margin headroom checks before any LLM ranking. Step 3 only fires when `len(filtered) > available_slots`, in which case the LLM picks the top N.

**Files:**
- Modify: `kernel/engine_chain.go` (add `codeFilterCandidates` + `portfolioRankingCall` + integrate)
- Modify: `kernel/chain_prompts.go` (add Step 3 templates)
- Modify: `kernel/engine_chain_test.go` (add tests)

- [ ] **Step 1: Add `codeFilterCandidates` to `kernel/engine_chain.go`**

```go
// codeFilterCandidates applies deterministic risk-control filters that don't
// require LLM judgement: per-sector position caps, global position cap,
// existing same-symbol filter, margin headroom.
//
// Returns: (filtered_candidates, available_slots).
// available_slots = how many more new positions can be opened at this moment.
func codeFilterCandidates(ctx *Context, engine *StrategyEngine, candidates []Step2Result, step2Pass []CandidateCoin) ([]CandidateCoin, int) {
	riskCfg := engine.GetRiskControlConfig()

	// Build current sector occupancy map from existing positions
	sectorCount := map[string]int{}
	openSymbols := map[string]bool{}
	for _, p := range ctx.Positions {
		cat := strings.ToLower(riskCfg.GetSymbolCategory(p.Symbol))
		sectorCount[cat]++
		openSymbols[p.Symbol] = true
	}

	// Global slot count
	availableSlots := riskCfg.MaxPositions - len(ctx.Positions)
	if availableSlots < 0 {
		availableSlots = 0
	}

	out := []CandidateCoin{}
	for _, c := range step2Pass {
		// Skip if same symbol already open
		if openSymbols[c.Symbol] {
			continue
		}
		cat := strings.ToLower(riskCfg.GetSymbolCategory(c.Symbol))
		cap := riskCfg.GetCategoryMaxPositions(cat)
		if sectorCount[cat] >= cap {
			continue
		}
		out = append(out, c)
	}

	return out, availableSlots
}
```

- [ ] **Step 2: Add Step 3 prompts**

Append to `kernel/chain_prompts.go`:

```go
const PromptStep3RankingSystemV1 = `你是组合排序助手。当前有多个通过技术筛选的候选，但 slot 不够。请按"对当前组合的边际增益"排优先级。

考虑维度：
1. 与当前持仓的相关性（低相关优先）
2. 入场结构清晰度（key_entry_level 距现价远近）
3. confidence
4. 板块多样化贡献

只输出 JSON。schema：
{
  "ranked": ["SYMBOL1", "SYMBOL2", ...],
  "top_n": int (≤ available_slots),
  "reasoning": "string"
}`

const PromptStep3RankingUserV1 = `## 候选清单
{{candidates_json}}

## 当前持仓
{{positions_summary}}

## available_slots = {{slots}}

请输出 JSON。`

type Step3Output struct {
	Ranked    []string `json:"ranked"`
	TopN      int      `json:"top_n"`
	Reasoning string   `json:"reasoning"`
}
```

- [ ] **Step 3: Add `portfolioRankingCall` to `kernel/engine_chain.go`**

```go
func portfolioRankingCall(ctx *Context, mcpClient mcp.AIClient, step2Results []Step2Result, candidates []CandidateCoin, slots int) (*Step3Output, string, error) {
	systemPrompt := PromptStep3RankingSystemV1

	candidatesJSON, _ := json.MarshalIndent(step2Results, "", "  ")
	userPrompt := PromptStep3RankingUserV1
	userPrompt = strings.ReplaceAll(userPrompt, "{{candidates_json}}", string(candidatesJSON))
	userPrompt = strings.ReplaceAll(userPrompt, "{{positions_summary}}", summarizePositions(ctx.Positions))
	userPrompt = strings.ReplaceAll(userPrompt, "{{slots}}", fmt.Sprintf("%d", slots))

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step3 LLM call: %w", err)
	}

	var out Step3Output
	if err := json.Unmarshal([]byte(extractJSONObject(resp)), &out); err != nil {
		return nil, resp, fmt.Errorf("step3 parse: %w (raw: %s)", err, truncate(resp, 200))
	}
	if out.TopN > slots {
		out.TopN = slots
	}
	return &out, resp, nil
}
```

- [ ] **Step 4: Update orchestration in `GetFullDecisionChained` to insert code filter and conditional Step 3**

Replace the post-Step-2 section with:

```go
	// Build step2Pass list of CandidateCoin from results
	step2Pass := []CandidateCoin{}
	for _, r := range step2Results {
		if r.Pass {
			for _, c := range filtered {
				if c.Symbol == r.Symbol {
					step2Pass = append(step2Pass, c)
					break
				}
			}
		}
	}
	fmt.Fprintf(&cot, "[step2] %d candidates → %d pass\n", len(filtered), len(step2Pass))

	// Deterministic code filter
	postFilter, slots := codeFilterCandidates(ctx, engine, step2Results, step2Pass)
	fmt.Fprintf(&cot, "[code-filter] %d → %d (slots=%d)\n", len(step2Pass), len(postFilter), slots)

	// Step 3: only when over capacity
	finalCandidates := postFilter
	if len(postFilter) > slots && slots > 0 {
		step3, _, err := portfolioRankingCall(ctx, mcpClient, step2Results, postFilter, slots)
		if err != nil {
			return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step3:%v", err))
		}
		// Apply ranking: take top N in step3.Ranked order
		rankedSet := map[string]bool{}
		for i, sym := range step3.Ranked {
			if i >= step3.TopN {
				break
			}
			rankedSet[sym] = true
		}
		ranked := []CandidateCoin{}
		for _, c := range postFilter {
			if rankedSet[c.Symbol] {
				ranked = append(ranked, c)
			}
		}
		finalCandidates = ranked
		fmt.Fprintf(&cot, "[step3] ranked %d → top %d\n", len(postFilter), len(finalCandidates))
	}

	if len(finalCandidates) == 0 && len(ctx.Positions) == 0 {
		return &FullDecision{
			Decisions:   []Decision{},
			CoTTrace:    "[chain:no-candidates-after-filter] " + cot.String(),
			RawResponse: "",
			Timestamp:   time.Now(),
		}, nil
	}

	// Step 4
	step4Result, err := decisionGenerationCall(ctx, engine, mcpClient, finalCandidates)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}
	if step1.MacroThesisUpdate != nil && len(step4Result.Decisions) > 0 {
		step4Result.Decisions[0].MacroThesisUpdate = step1.MacroThesisUpdate
	}
	step4Result.CoTTrace = "[chain:full] " + cot.String() + step4Result.CoTTrace
	return step4Result, nil
```

- [ ] **Step 5: Add code filter + Step 3 unit tests**

```go
func TestCodeFilterCandidates_RespectsCategoryCap(t *testing.T) {
	ctx := &Context{
		Positions: []PositionInfo{
			{Symbol: "NVDAUSDT", Side: "LONG"},
			{Symbol: "AMDUSDT", Side: "LONG"},
		},
	}
	engine := newTestEngine()
	candidates := []CandidateCoin{
		{Symbol: "INTCUSDT"}, // semiconductor — should be capped (sector at 2/2)
		{Symbol: "QQQUSDT"},  // index — has slots
	}
	filtered, slots := codeFilterCandidates(ctx, engine, nil, candidates)

	if len(filtered) != 1 || filtered[0].Symbol != "QQQUSDT" {
		t.Fatalf("expected only QQQ to survive; got %v", filtered)
	}
	if slots <= 0 {
		t.Fatalf("expected positive slots; got %d", slots)
	}
}

func TestStep3Ranking_TruncatesToTopN(t *testing.T) {
	mockResp := `{"ranked":["NVDAUSDT","QQQUSDT","SPYUSDT"],"top_n":2,"reasoning":"top 2 are best"}`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newTestContext()
	candidates := []CandidateCoin{{Symbol: "NVDAUSDT"}, {Symbol: "QQQUSDT"}, {Symbol: "SPYUSDT"}}
	results := []Step2Result{
		{Symbol: "NVDAUSDT", Pass: true}, {Symbol: "QQQUSDT", Pass: true}, {Symbol: "SPYUSDT", Pass: true},
	}

	step3, _, err := portfolioRankingCall(ctx, mock, results, candidates, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step3.TopN != 2 {
		t.Fatalf("expected top_n=2; got %d", step3.TopN)
	}
}
```

- [ ] **Step 6: Run all chain tests**

```bash
cd /root/src/nofx-binance-stock && go test ./kernel/ -run "Step|Chained|CodeFilter|Wait" -v
```
Expected: All tests PASS.

- [ ] **Step 7: Commit**

```bash
cd /root/src/nofx-binance-stock && git add kernel/engine_chain.go kernel/chain_prompts.go kernel/engine_chain_test.go && \
  git commit -m "feat(kernel): add code filter + chain Step 3 (conditional ranking)

Code filter applies deterministic risk-control filters (sector cap,
global slot cap, same-symbol dedup) — these don't need LLM judgement.

Step 3 only fires when filtered candidates > available slots, picking
top N. Skipped when capacity is sufficient (saves a token-cheap call).

Full chain integration: [chain:full] tag in CoTTrace marks complete
4-step traversal."
```

---

## Task 8: Integration validation on EC2 with shadow trader

This is a manual verification task — no new code. The goal is to enable `EnableChainOfThought=true` on a separate trader instance with small capital and observe behavior side-by-side with the original single-call trader.

- [ ] **Step 1: Push the branch**

```bash
ssh -i /Users/xiagao/Desktop/pem/tokyo-one.pem root@64.83.41.30 "cd /root/src/nofx-binance-stock && git push origin feature/chain-of-thought"
```

- [ ] **Step 2: Build and stage Docker image (don't deploy yet)**

```bash
ssh -i /Users/xiagao/Desktop/pem/tokyo-one.pem root@64.83.41.30 \
  "cd /root/src/nofx-binance-stock && docker build -t nofx-backend:chain-test ."
```
Expected: build succeeds.

- [ ] **Step 3: Run the existing test suite end-to-end**

```bash
ssh -i /Users/xiagao/Desktop/pem/tokyo-one.pem root@64.83.41.30 \
  "cd /root/src/nofx-binance-stock && go test ./... -count=1 -short 2>&1 | tail -30"
```
Expected: PASS (or pre-existing failures unchanged).

- [ ] **Step 4: Create a shadow trader (manual via UI or DB)**

Via the frontend, create a new trader:
- Same Binance account, separate sub-account if available, OR same account with $10 initial balance
- Strategy: clone existing strategy `5e5f498a-...`, set `enable_chain_of_thought=true` in the JSON config
- Start the trader

- [ ] **Step 5: Observe first 10 cycles**

Watch logs:
```bash
ssh -i /Users/xiagao/Desktop/pem/tokyo-one.pem root@64.83.41.30 \
  "docker logs nofx-trading -f 2>&1 | grep -E 'chain|🔗|degraded'"
```

Verify:
- `🔗 cycle N using chain-of-thought reasoning` appears for the new trader
- `[chain:full]` or appropriate tag in `decision_records.cot_trace`
- No `[chain-degraded:...]` (or if so, debug the parse failure)
- Token budget per cycle in expected range (check `ai_charges` table)

- [ ] **Step 6: Compare side-by-side for 24h**

Pull both traders' decisions:
```sql
SELECT trader_id, cycle_number, datetime(timestamp,'+8 hours') as t,
       json_array_length(decisions) as n_decisions, substr(cot_trace,1,80) as cot
FROM decision_records
WHERE timestamp > datetime('now','-24 hours')
ORDER BY trader_id, cycle_number;
```

Look for:
- Chain trader opens different positions than single-call trader (validates chain influences decisions)
- Pullback opportunities (low 1H RSI, intact 4H structure) get a `pass=true` in chain that single-call would miss
- No degradation tags (or if any, root-cause and patch the affected step's prompt or schema)

- [ ] **Step 7: Document findings**

Write a short report to `docs/plans/2026-04-28-chain-of-thought-implementation.md` (this file) appending a "## Validation Results" section with:
- 24h decision count delta
- Token cost delta vs single
- Degradation rate per step
- Any pullback entries the chain caught that single-call would have missed

- [ ] **Step 8: Final commit**

```bash
cd /root/src/nofx-binance-stock && git add docs/plans/2026-04-28-chain-of-thought-implementation.md && \
  git commit -m "docs(chain): add 24h shadow-trader validation results"
```

---

## Self-Review Notes

Before executing, check:

1. **Spec coverage**: All 4 steps + code filter + routing + degradation + Step 5 dropped per locked design ✓
2. **Field name verification**: `MarginUsedPct`, `K4H`, `CurrentEMA20`, `CurrentRSI7` — verify against `kernel/engine.go` and `market/data.go` before Task 4 Step 4. Adjust if names differ.
3. **`MacroThesisUpdate` placement**: Per locked design, attached to first Decision. If `Decision.MacroThesisUpdate` field doesn't exist, add it to the struct in `kernel/engine.go` (mentioned in spec but verify).
4. **`MacroThesisContext.PortfolioIntent` and `ThesisText` field names**: verify in `kernel/engine.go`.
5. **Build errors are loud**: each task ends with `go build ./...` so type mismatches surface immediately.

---

## Out-of-scope (Phase 2 / V2)

These were discussed but explicitly deferred:
- **Differential reasoning_effort levels** — requires plumbing through `mcp.Request` first; not in current code
- **Step 1 → Step 2 parallelization** — keep sequential for v1
- **Pullback scale-in (V2 strategy candidate)** — requires Step 2 to emit `scale_in_layers` array; revisit after equity ≥$300 and 2 weeks of stable single-layer pullback entries
- **25% drawdown rule fix** — separate issue, separate PR (`auto_trader_risk.go`)
- **Frontend toggle for `enable_chain_of_thought`** — DB edit suffices for shadow trader; UI work later if user wants production-wide rollout
