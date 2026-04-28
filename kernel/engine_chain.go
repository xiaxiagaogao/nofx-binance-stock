package kernel

import (
	"fmt"

	"nofx/logger"
	"nofx/mcp"
)

// GetFullDecisionChained runs the chained reasoning pipeline.
// Behavior contract: returns a *FullDecision compatible with the single-call
// path, including SystemPrompt/UserPrompt/CoTTrace/Decisions/RawResponse.
// On any step failure, falls back to GetFullDecisionWithStrategy() and prefixes
// CoTTrace with [chain-degraded:reason] for forensics.
func GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is nil")
	}

	// MVP skeleton: delegate to single-call path. Subsequent commits replace
	// this with real step-by-step reasoning. Degradation tracking is wired
	// here so future failures route through the same fallback site.
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
