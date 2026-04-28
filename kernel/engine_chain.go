package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nofx/logger"
	"nofx/mcp"
)

// GetFullDecisionChained runs the chained reasoning pipeline.
// Behavior contract: returns a *FullDecision compatible with the single-call
// path (SystemPrompt/UserPrompt/CoTTrace/Decisions/RawResponse populated).
// On any step failure, falls back to GetFullDecisionWithStrategy() and
// prefixes CoTTrace with [chain-degraded:reason] for forensics.
func GetFullDecisionChained(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is nil")
	}

	// Ensure market data fetched (mirrors GetFullDecisionWithStrategy).
	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return chainFallback(ctx, mcpClient, engine, "market-data-fetch")
		}
	}

	// MVP: Steps 1-3 added in subsequent commits. For now, pass full candidate
	// list straight to Step 4. This validates Step 4 in isolation.
	candidates := ctx.CandidateCoins

	step4, err := decisionGenerationCall(ctx, engine, mcpClient, candidates)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}
	step4.CoTTrace = "[chain:step4-only] " + step4.CoTTrace
	return step4, nil
}

// chainFallback runs the existing single-call path and tags CoTTrace so post-
// hoc analysis can distinguish degraded runs from intentional single-call runs.
func chainFallback(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, reason string) (*FullDecision, error) {
	logger.Infof("⚠️  [chain] degraded → single-call fallback (reason: %s)", reason)
	dec, err := GetFullDecisionWithStrategy(ctx, mcpClient, engine, "balanced")
	if dec != nil {
		dec.CoTTrace = fmt.Sprintf("[chain-degraded:%s] %s", reason, dec.CoTTrace)
	}
	return dec, err
}

// decisionGenerationCall is Step 4 of the chain. Receives a curated candidate
// list (from Steps 1-3 in later commits) and emits []Decision via the existing
// parseFullDecisionResponse so DB serialization stays identical.
func decisionGenerationCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, candidates []CandidateCoin) (*FullDecision, error) {
	riskCfg := engine.GetRiskControlConfig()

	systemPrompt := renderStep4System(riskCfg.EffectiveMaxLeverage())
	userPrompt, err := renderStep4User(ctx, engine, candidates)
	if err != nil {
		return nil, fmt.Errorf("render step4 prompt: %w", err)
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

func renderStep4System(maxLeverage int) string {
	return strings.ReplaceAll(PromptStep4DecisionSystemV1, "{{max_leverage}}", fmt.Sprintf("%d", maxLeverage))
}

func renderStep4User(ctx *Context, engine *StrategyEngine, candidates []CandidateCoin) (string, error) {
	candidatesJSON, err := json.MarshalIndent(candidates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal candidates: %w", err)
	}

	riskCfg := engine.GetRiskControlConfig()
	availableSlots := riskCfg.MaxPositions - len(ctx.Positions)
	if availableSlots < 0 {
		availableSlots = 0
	}

	out := PromptStep4DecisionUserV1
	out = strings.ReplaceAll(out, "{{candidates_json}}", string(candidatesJSON))
	out = strings.ReplaceAll(out, "{{positions_summary}}", summarizePositions(ctx.Positions))
	out = strings.ReplaceAll(out, "{{equity}}", fmt.Sprintf("%.2f", ctx.Account.TotalEquity))
	out = strings.ReplaceAll(out, "{{margin_pct}}", fmt.Sprintf("%.1f", ctx.Account.MarginUsedPct))
	out = strings.ReplaceAll(out, "{{slots}}", fmt.Sprintf("%d", availableSlots))
	out = strings.ReplaceAll(out, "{{max_leverage}}", fmt.Sprintf("%d", riskCfg.EffectiveMaxLeverage()))
	out = strings.ReplaceAll(out, "{{min_position_size}}", fmt.Sprintf("%.2f", riskCfg.MinPositionSize))
	out = strings.ReplaceAll(out, "{{max_pos_ratio}}", fmt.Sprintf("%.2f", riskCfg.EffectiveMaxPositionValueRatio()))
	out = strings.ReplaceAll(out, "{{market_data}}", formatChainMarketData(ctx, engine, candidates))
	return out, nil
}

func summarizePositions(positions []PositionInfo) string {
	if len(positions) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, p := range positions {
		fmt.Fprintf(&sb, "- %s %s | qty %.4f | entry %.2f | unrealized %.2f%% | peak %.2f%% | intent %s\n",
			p.Symbol, p.Side, p.Quantity, p.EntryPrice, p.UnrealizedPnLPct, p.PeakPnLPct, p.IntentType)
	}
	return sb.String()
}

// formatChainMarketData formats market data for chain steps. Reuses the
// existing engine.formatMarketData() per-symbol helper but limits to the
// curated candidate list (vs. BuildUserPrompt which dumps all candidates).
func formatChainMarketData(ctx *Context, engine *StrategyEngine, candidates []CandidateCoin) string {
	var sb strings.Builder
	for _, c := range candidates {
		md, ok := ctx.MarketDataMap[c.Symbol]
		if !ok || md == nil {
			fmt.Fprintf(&sb, "\n=== %s === (no data)\n", c.Symbol)
			continue
		}
		sb.WriteString(engine.formatMarketData(md))
	}
	return sb.String()
}
