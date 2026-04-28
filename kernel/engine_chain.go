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

	cot := strings.Builder{}

	// Step 1: macro alignment
	step1, step1Raw, err := macroAlignmentCall(ctx, mcpClient)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step1:%v", err))
	}
	fmt.Fprintf(&cot, "[step1] regime=%s direction=%s allowed=%v restricted=%v note=%s\n",
		step1.MarketRegime, step1.DirectionBias, step1.AllowedSectors, step1.RestrictedSectors, step1.SessionNote)

	// Short-circuit: direction_bias=wait → emit empty decisions, no Step 4
	if step1.DirectionBias == "wait" {
		return &FullDecision{
			Decisions:         []Decision{},
			CoTTrace:          "[chain:wait-shortcut] " + cot.String(),
			RawResponse:       step1Raw,
			Timestamp:         time.Now(),
			MacroThesisUpdate: step1.MacroThesisUpdate,
		}, nil
	}

	// Filter candidates by sector
	filtered := filterBySector(ctx.CandidateCoins, step1.AllowedSectors, step1.RestrictedSectors, engine)
	fmt.Fprintf(&cot, "[step1-filter] %d → %d candidates\n", len(ctx.CandidateCoins), len(filtered))

	if len(filtered) == 0 && len(ctx.Positions) == 0 {
		return &FullDecision{
			Decisions:         []Decision{},
			CoTTrace:          "[chain:no-candidates-after-step1] " + cot.String(),
			RawResponse:       step1Raw,
			Timestamp:         time.Now(),
			MacroThesisUpdate: step1.MacroThesisUpdate,
		}, nil
	}

	// MVP: Steps 2-3 added in subsequent commits. Pass filtered list to Step 4.
	step4, err := decisionGenerationCall(ctx, engine, mcpClient, filtered)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}

	// Attach macro_thesis_update to FullDecision (top-level field) — Step 1
	// owns the update emission per locked design.
	if step1.MacroThesisUpdate != nil {
		step4.MacroThesisUpdate = step1.MacroThesisUpdate
	}

	step4.CoTTrace = "[chain:1+4] " + cot.String() + step4.CoTTrace
	return step4, nil
}

// =============================================================================
// Step 1 — Macro Alignment
// =============================================================================

// Step1Output is the parsed JSON from macroAlignmentCall.
type Step1Output struct {
	MarketRegime      string             `json:"market_regime"`
	AllowedSectors    []string           `json:"allowed_sectors"`
	RestrictedSectors []string           `json:"restricted_sectors"`
	DirectionBias     string             `json:"direction_bias"`
	SessionNote       string             `json:"session_note"`
	MacroThesisUpdate *MacroThesisUpdate `json:"macro_thesis_update,omitempty"`
	Reasoning         string             `json:"reasoning"`
}

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
		return nil, resp, fmt.Errorf("step1 parse: %w (raw: %s)", err, truncateString(resp, 200))
	}
	if !isValidRegime(out.MarketRegime) {
		return nil, resp, fmt.Errorf("step1 invalid market_regime: %q", out.MarketRegime)
	}
	if !isValidDirectionBias(out.DirectionBias) {
		return nil, resp, fmt.Errorf("step1 invalid direction_bias: %q", out.DirectionBias)
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
		if strings.EqualFold(p.Side, "long") {
			longCount++
		} else if strings.EqualFold(p.Side, "short") {
			shortCount++
		}
	}
	if longCount > shortCount {
		netDirection = fmt.Sprintf("net_long (long=%d short=%d)", longCount, shortCount)
	} else if shortCount > longCount {
		netDirection = fmt.Sprintf("net_short (long=%d short=%d)", longCount, shortCount)
	}

	candidateSectors := summarizeCandidateSectors(ctx)

	out := PromptStep1MacroUserV1
	out = strings.ReplaceAll(out, "{{macro_thesis}}", macroThesis)
	out = strings.ReplaceAll(out, "{{net_direction}}", netDirection)
	out = strings.ReplaceAll(out, "{{position_count}}", fmt.Sprintf("%d", len(ctx.Positions)))
	out = strings.ReplaceAll(out, "{{max_positions}}", "8")
	out = strings.ReplaceAll(out, "{{sector_dist}}", summarizePositionSectors(ctx.Positions))
	out = strings.ReplaceAll(out, "{{session}}", ctx.TradingSession)
	out = strings.ReplaceAll(out, "{{scale_factor}}", fmt.Sprintf("%.2f", ctx.SessionScaleFactor))
	out = strings.ReplaceAll(out, "{{candidate_sectors}}", candidateSectors)
	return out
}

func formatMacroThesisCompact(m *MacroThesisContext) string {
	if m == nil {
		return "(nil)"
	}
	biasParts := []string{}
	for k, v := range m.SectorBias {
		biasParts = append(biasParts, fmt.Sprintf("%s=%s", k, v))
	}
	return fmt.Sprintf("regime=%s | intent=%s | sectors={%s} | risks=%v | thesis=%s",
		m.MarketRegime, m.PortfolioIntent, strings.Join(biasParts, ","),
		m.KeyRisks, truncateString(m.ThesisText, 400))
}

func summarizePositionSectors(positions []PositionInfo) string {
	if len(positions) == 0 {
		return "(empty)"
	}
	counts := map[string]int{}
	for _, p := range positions {
		counts[p.Symbol]++
	}
	var parts []string
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return strings.Join(parts, ", ")
}

func summarizeCandidateSectors(ctx *Context) string {
	if len(ctx.CandidateCoins) == 0 {
		return "(empty)"
	}
	syms := make([]string, 0, len(ctx.CandidateCoins))
	for _, c := range ctx.CandidateCoins {
		syms = append(syms, c.Symbol)
	}
	return strings.Join(syms, ", ")
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

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isValidRegime(s string) bool {
	switch s {
	case "risk_on", "neutral", "risk_off", "mixed", "cautious":
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
		// allowSet empty → allow everything not restricted
		if len(allowSet) > 0 && !allowSet[category] {
			continue
		}
		out = append(out, c)
	}
	return out
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
