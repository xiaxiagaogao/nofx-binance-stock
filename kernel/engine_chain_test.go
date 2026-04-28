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
