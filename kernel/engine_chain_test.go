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
	if !strings.Contains(err.Error(), "engine") {
		t.Fatalf("expected error mentioning engine; got %v", err)
	}
}
