package kernel

import (
	"fmt"
	"strings"
	"testing"

	"nofx/market"
	"nofx/store"
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
	if !strings.Contains(err.Error(), "engine") {
		t.Fatalf("expected error mentioning engine; got %v", err)
	}
}

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
	ctx := newChainTestContext()
	engine := newChainTestEngine()

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
	ctx := newChainTestContext()
	engine := newChainTestEngine()

	_, err := decisionGenerationCall(ctx, engine, mock, ctx.CandidateCoins)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected 'network down' in error; got %v", err)
	}
}

func TestStep4UserPrompt_ContainsAccountState(t *testing.T) {
	ctx := newChainTestContext()
	ctx.Account.TotalEquity = 131.50
	ctx.Account.MarginUsedPct = 22.3
	engine := newChainTestEngine()

	prompt, err := renderStep4User(ctx, engine, ctx.CandidateCoins)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "131.50") {
		t.Fatalf("expected equity 131.50 in prompt; got %s", prompt[:200])
	}
	if !strings.Contains(prompt, "22.3") {
		t.Fatalf("expected margin pct 22.3 in prompt")
	}
}

// =============================================================================
// Step 1 — Macro Alignment tests
// =============================================================================

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
	ctx := newChainTestContext()

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
	mockResp := `{"market_regime":"bullish","direction_bias":"long_preferred","allowed_sectors":[],"restricted_sectors":[],"session_note":"x","reasoning":"y"}`
	mock := NewMockAIClient().WithResponse(mockResp)
	ctx := newChainTestContext()

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
	ctx := newChainTestContext()

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
	ctx := newChainTestContext()
	engine := newChainTestEngine()

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

func TestFilterBySector_RestrictedSectorRemoved(t *testing.T) {
	engine := newChainTestEngine()
	candidates := []CandidateCoin{
		{Symbol: "NVDAUSDT"}, // semiconductor
		{Symbol: "QQQUSDT"},  // index
		{Symbol: "CLUSDT"},   // energy
	}
	out := filterBySector(candidates, nil, []string{"energy"}, engine)
	for _, c := range out {
		if c.Symbol == "CLUSDT" {
			t.Fatalf("expected CLUSDT (energy) to be filtered out; got %v", out)
		}
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 survivors; got %d", len(out))
	}
}

func TestFilterBySector_AllowListEnforced(t *testing.T) {
	engine := newChainTestEngine()
	candidates := []CandidateCoin{
		{Symbol: "NVDAUSDT"}, // semiconductor
		{Symbol: "QQQUSDT"},  // index
	}
	out := filterBySector(candidates, []string{"semiconductor"}, nil, engine)
	if len(out) != 1 || out[0].Symbol != "NVDAUSDT" {
		t.Fatalf("expected only NVDA to survive allow-list; got %v", out)
	}
}

// newChainTestContext / newChainTestEngine are minimal shared fixtures.
func newChainTestContext() *Context {
	return &Context{
		Account: AccountInfo{TotalEquity: 100, MarginUsedPct: 20, AvailableBalance: 80},
		Positions: []PositionInfo{},
		CandidateCoins: []CandidateCoin{{Symbol: "NVDAUSDT", Sources: []string{"ai500"}}},
		MarketDataMap: map[string]*market.Data{
			"NVDAUSDT": {Symbol: "NVDAUSDT", CurrentPrice: 200, CurrentEMA20: 199, CurrentRSI7: 60},
		},
	}
}

func newChainTestEngine() *StrategyEngine {
	cfg := store.GetDefaultStrategyConfig("zh")
	return NewStrategyEngine(&cfg)
}
