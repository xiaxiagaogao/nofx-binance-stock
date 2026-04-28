package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrategyConfig_EnableChainOfThoughtRoundTrip(t *testing.T) {
	cfg := StrategyConfig{EnableChainOfThought: true}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"enable_chain_of_thought":true`) {
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
	if strings.Contains(string(b), "enable_chain_of_thought") {
		t.Fatalf("expected omitempty default to omit field; got %s", string(b))
	}
}
