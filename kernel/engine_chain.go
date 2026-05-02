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
	step1, step1Raw, err := macroAlignmentCall(ctx, mcpClient, engine)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step1:%v", err))
	}
	fmt.Fprintf(&cot, "[step1] regime=%s direction=%s allowed=%v restricted=%v note=%s\n",
		step1.MarketRegime, step1.DirectionBias, step1.AllowedSectors, step1.RestrictedSectors, step1.SessionNote)

	// v2 (2026-05-02): wait/no-candidate paths must NOT short-circuit when positions exist —
	// existing positions still need hold/close/add evaluation in Step 4. Only fully short-circuit
	// when both no candidates AND no positions exist (truly nothing to decide).

	// direction_bias=wait → skip Step 2/3, jump to Step 4 with empty candidates if positions exist
	if step1.DirectionBias == "wait" {
		if len(ctx.Positions) == 0 {
			return &FullDecision{
				Decisions:         []Decision{},
				CoTTrace:          "[chain:wait-no-positions] " + cot.String(),
				RawResponse:       step1Raw,
				Timestamp:         time.Now(),
				MacroThesisUpdate: step1.MacroThesisUpdate,
			}, nil
		}
		cot.WriteString("[chain:wait-with-positions] skipping Step 2/3, jumping to Step 4 for position evaluation\n")
		return runStep4Only(ctx, mcpClient, engine, &cot, step1)
	}

	// Filter candidates by sector
	filtered := filterBySector(ctx.CandidateCoins, step1.AllowedSectors, step1.RestrictedSectors, engine)
	fmt.Fprintf(&cot, "[step1-filter] %d → %d candidates\n", len(ctx.CandidateCoins), len(filtered))

	if len(filtered) == 0 {
		if len(ctx.Positions) == 0 {
			return &FullDecision{
				Decisions:         []Decision{},
				CoTTrace:          "[chain:no-candidates-after-step1] " + cot.String(),
				RawResponse:       step1Raw,
				Timestamp:         time.Now(),
				MacroThesisUpdate: step1.MacroThesisUpdate,
			}, nil
		}
		cot.WriteString("[chain:step1-empty-with-positions] jumping to Step 4 for position evaluation\n")
		return runStep4Only(ctx, mcpClient, engine, &cot, step1)
	}

	// Step 2: technical screening
	step2Results, _, err := technicalScreeningCall(ctx, engine, mcpClient, step1, filtered)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step2:%v", err))
	}

	// v2: post-process Step 2 results to set IsAddCandidate based on existing positions.
	// AI is instructed to set this in its output, but we backstop: if a passed candidate
	// matches an existing position symbol+side, force IsAddCandidate=true so Step 4 cannot
	// miss the add path.
	openPositionsBySide := map[string]string{} // "SYMBOL_LONG" / "SYMBOL_SHORT" → "long"/"short"
	for _, p := range ctx.Positions {
		key := p.Symbol + "_" + strings.ToUpper(p.Side)
		openPositionsBySide[key] = strings.ToLower(p.Side)
	}
	for i := range step2Results {
		r := &step2Results[i]
		if !r.Pass || r.Direction == "" {
			continue
		}
		key := r.Symbol + "_" + strings.ToUpper(r.Direction)
		if _, exists := openPositionsBySide[key]; exists {
			r.IsAddCandidate = true
		}
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
	fmt.Fprintf(&cot, "[step2] %d → %d pass\n", len(filtered), len(step2Pass))
	cot.WriteString(formatStep2Detail(step2Results))

	// Deterministic code filter (sector caps, global slot cap, same-symbol dedup)
	// NOTE: codeFilterCandidates removes existing-symbol candidates (open dedup) — this is
	// fine for new opens but we WANT add candidates to survive. v2 fix: re-inject add
	// candidates after the code filter pass.
	postFilter, slots := codeFilterCandidates(ctx, engine, step2Pass)
	addCandidates := []CandidateCoin{}
	for _, r := range step2Results {
		if r.IsAddCandidate {
			for _, c := range filtered {
				if c.Symbol == r.Symbol {
					addCandidates = append(addCandidates, c)
					break
				}
			}
		}
	}
	if len(addCandidates) > 0 {
		// merge addCandidates into postFilter without duplication
		seen := map[string]bool{}
		for _, c := range postFilter {
			seen[c.Symbol] = true
		}
		for _, c := range addCandidates {
			if !seen[c.Symbol] {
				postFilter = append(postFilter, c)
			}
		}
	}
	fmt.Fprintf(&cot, "[code-filter] %d → %d (slots=%d, add_candidates=%d)\n", len(step2Pass), len(postFilter), slots, len(addCandidates))

	if len(postFilter) == 0 {
		if len(ctx.Positions) == 0 {
			return &FullDecision{
				Decisions:         []Decision{},
				CoTTrace:          "[chain:no-candidates-after-filter] " + cot.String(),
				RawResponse:       "",
				Timestamp:         time.Now(),
				MacroThesisUpdate: step1.MacroThesisUpdate,
			}, nil
		}
		cot.WriteString("[chain:no-candidates-with-positions] jumping to Step 4 for position evaluation\n")
		return runStep4Only(ctx, mcpClient, engine, &cot, step1)
	}

	// Step 3: only when over capacity
	finalCandidates := postFilter
	if len(postFilter) > slots && slots > 0 {
		step3, _, err := portfolioRankingCall(ctx, engine, mcpClient, step2Results, postFilter, slots)
		if err != nil {
			return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step3:%v", err))
		}
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
		rankedSyms := []string{}
		for _, c := range finalCandidates {
			rankedSyms = append(rankedSyms, c.Symbol)
		}
		fmt.Fprintf(&cot, "[step3] ranked %d → top %d: %s\n", len(postFilter), len(finalCandidates), strings.Join(rankedSyms, ", "))
	}

	step4, err := decisionGenerationCall(ctx, engine, mcpClient, finalCandidates)
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4:%v", err))
	}
	if step1.MacroThesisUpdate != nil {
		step4.MacroThesisUpdate = step1.MacroThesisUpdate
	}
	step4.CoTTrace = "[chain:full] " + cot.String() + step4.CoTTrace
	return step4, nil
}

// =============================================================================
// Step 2 — Technical Screening
// =============================================================================

// Step2Result is one element of the Step 2 output array.
type Step2Result struct {
	Symbol         string   `json:"symbol"`
	Direction      string   `json:"direction"`
	Confidence     int      `json:"confidence"`
	Structure      string   `json:"structure"`
	KeyEntryLevel  *float64 `json:"key_entry_level"`
	KeyStopLevel   *float64 `json:"key_stop_level"`
	Pass           bool     `json:"pass"`
	IsAddCandidate bool     `json:"is_add_candidate,omitempty"` // v2: marks "same-side position already open → use add_long/add_short"
	ReasonIfSkip   string   `json:"reason_if_skip,omitempty"`
}

// formatStep2Detail renders per-symbol pass/skip verdicts with structure +
// reason for embedding in cot_trace. Used to validate pullback-long entries.
func formatStep2Detail(results []Step2Result) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[step2:detail]\n")
	for _, r := range results {
		if r.Pass {
			entry := "?"
			if r.KeyEntryLevel != nil {
				entry = fmt.Sprintf("%.4f", *r.KeyEntryLevel)
			}
			stop := "?"
			if r.KeyStopLevel != nil {
				stop = fmt.Sprintf("%.4f", *r.KeyStopLevel)
			}
			dir := r.Direction
			if dir == "" {
				dir = "-"
			}
			tag := ""
			if r.IsAddCandidate {
				tag = " [add]"
			}
			fmt.Fprintf(&sb, "  ✓ %s %s conf=%d%s \"%s\" entry=%s stop=%s\n",
				r.Symbol, dir, r.Confidence, tag, r.Structure, entry, stop)
		} else {
			reason := r.ReasonIfSkip
			if reason == "" {
				reason = r.Structure
			}
			if reason == "" {
				reason = "(no reason)"
			}
			fmt.Fprintf(&sb, "  ✗ %s skip=\"%s\"\n", r.Symbol, reason)
		}
	}
	return sb.String()
}

func technicalScreeningCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, step1 *Step1Output, candidates []CandidateCoin) ([]Step2Result, string, error) {
	ps := engine.config.PromptSections
	systemPrompt := renderChainSystemPrompt(
		PromptStep2TechnicalSystemV2,
		ps.RoleDefinition,
		ps.TradingFrequency,
		ps.EntryStandards,
	)
	userPrompt := renderStep2User(ctx, engine, step1, candidates)

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step2 LLM call: %w", err)
	}

	jsonText := extractChainJSONArray(resp)
	var results []Step2Result
	if err := json.Unmarshal([]byte(jsonText), &results); err != nil {
		return nil, resp, fmt.Errorf("step2 parse: %w (raw: %s)", err, truncateString(resp, 200))
	}

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
	marketData := formatChainMarketData(ctx, engine, candidates)
	keyRisks := "(none)"
	if ctx.MacroThesis != nil && len(ctx.MacroThesis.KeyRisks) > 0 {
		keyRisks = "- " + strings.Join(ctx.MacroThesis.KeyRisks, "\n- ")
	}
	out := PromptStep2TechnicalUserV2
	out = strings.ReplaceAll(out, "{{direction_bias}}", step1.DirectionBias)
	out = strings.ReplaceAll(out, "{{allowed_sectors}}", strings.Join(step1.AllowedSectors, ", "))
	out = strings.ReplaceAll(out, "{{positions_summary}}", summarizePositions(ctx.Positions))
	out = strings.ReplaceAll(out, "{{key_risks}}", keyRisks)
	out = strings.ReplaceAll(out, "{{candidates_market_data}}", marketData)
	return out
}

// =============================================================================
// Code filter (deterministic, no LLM) — applies risk-control rules upfront
// =============================================================================

// codeFilterCandidates applies deterministic risk-control filters: per-sector
// position caps, global position cap, existing same-symbol dedup.
// Returns: (filtered_candidates, available_slots).
func codeFilterCandidates(ctx *Context, engine *StrategyEngine, candidates []CandidateCoin) ([]CandidateCoin, int) {
	riskCfg := engine.GetRiskControlConfig()

	sectorCount := map[string]int{}
	openSymbols := map[string]bool{}
	for _, p := range ctx.Positions {
		cat := strings.ToLower(riskCfg.GetSymbolCategory(p.Symbol))
		sectorCount[cat]++
		openSymbols[p.Symbol] = true
	}

	availableSlots := riskCfg.MaxPositions - len(ctx.Positions)
	if availableSlots < 0 {
		availableSlots = 0
	}

	out := []CandidateCoin{}
	for _, c := range candidates {
		if openSymbols[c.Symbol] {
			continue
		}
		cat := strings.ToLower(riskCfg.GetSymbolCategory(c.Symbol))
		cap := riskCfg.GetCategoryMaxPositions(cat)
		if cap > 0 && sectorCount[cat] >= cap {
			continue
		}
		out = append(out, c)
	}
	return out, availableSlots
}

// =============================================================================
// Step 3 — Portfolio Ranking (conditional)
// =============================================================================

// Step3Output is the parsed JSON from portfolioRankingCall.
type Step3Output struct {
	Ranked    []string `json:"ranked"`
	TopN      int      `json:"top_n"`
	Reasoning string   `json:"reasoning"`
}

func portfolioRankingCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, step2Results []Step2Result, candidates []CandidateCoin, slots int) (*Step3Output, string, error) {
	ps := engine.config.PromptSections
	systemPrompt := renderChainSystemPrompt(
		PromptStep3RankingSystemV2,
		ps.RoleDefinition,
		ps.EntryStandards,
	)

	// Send the technical screening results for the relevant candidates
	relevantSymbols := map[string]bool{}
	for _, c := range candidates {
		relevantSymbols[c.Symbol] = true
	}
	relevantResults := []Step2Result{}
	for _, r := range step2Results {
		if relevantSymbols[r.Symbol] {
			relevantResults = append(relevantResults, r)
		}
	}

	candidatesJSON, _ := json.MarshalIndent(relevantResults, "", "  ")
	userPrompt := PromptStep3RankingUserV1
	userPrompt = strings.ReplaceAll(userPrompt, "{{candidates_json}}", string(candidatesJSON))
	userPrompt = strings.ReplaceAll(userPrompt, "{{positions_summary}}", summarizePositions(ctx.Positions))
	userPrompt = strings.ReplaceAll(userPrompt, "{{slots}}", fmt.Sprintf("%d", slots))

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step3 LLM call: %w", err)
	}

	var out Step3Output
	if err := json.Unmarshal([]byte(extractChainJSONObject(resp)), &out); err != nil {
		return nil, resp, fmt.Errorf("step3 parse: %w (raw: %s)", err, truncateString(resp, 200))
	}
	if out.TopN > slots {
		out.TopN = slots
	}
	if out.TopN < 0 {
		out.TopN = 0
	}
	return &out, resp, nil
}

func extractChainJSONArray(s string) string {
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

func macroAlignmentCall(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine) (*Step1Output, string, error) {
	ps := engine.config.PromptSections
	systemPrompt := renderChainSystemPrompt(
		PromptStep1MacroSystemV2,
		ps.RoleDefinition,
		ps.TradingFrequency,
	)
	userPrompt := renderStep1User(ctx)

	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, resp, fmt.Errorf("step1 LLM call: %w", err)
	}

	var out Step1Output
	jsonText := extractChainJSONObject(resp)
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

// extractChainJSONObject pulls the first balanced { ... } block from a string.
// Models sometimes wrap JSON in markdown fences or chatty preambles.
func extractChainJSONObject(s string) string {
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

// renderChainSystemPrompt assembles a chain step's system prompt by prepending
// the user's prompt_sections (basket of customizable Chinese fund-manager content
// stored in DB strategy.prompt_sections) before the step-specific skeleton.
//
// Sections are appended in the order given, separated by horizontal rules so
// each chunk is visually distinct in the rendered prompt. Empty sections are
// skipped. The skeleton (V2 template containing only step-specific schema/task
// instructions) goes last.
//
// This is the v2 chain mechanism (2026-05-02 refactor) — without injection,
// chain runs in a parallel prompt universe that ignores user customizations.
func renderChainSystemPrompt(skeleton string, sections ...string) string {
	var sb strings.Builder
	for _, s := range sections {
		if strings.TrimSpace(s) == "" {
			continue
		}
		sb.WriteString(s)
		sb.WriteString("\n\n---\n\n")
	}
	sb.WriteString(skeleton)
	return sb.String()
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

// runStep4Only invokes only Step 4 with empty candidates. Used in v2 (2026-05-02
// refactor) when Step 1 says wait or Step 1/2 produce zero candidates BUT existing
// positions still need evaluation (hold/close/add). This prevents the "chain断流"
// bug where wait responses skipped position management entirely.
func runStep4Only(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, cot *strings.Builder, step1 *Step1Output) (*FullDecision, error) {
	step4, err := decisionGenerationCall(ctx, engine, mcpClient, []CandidateCoin{})
	if err != nil {
		return chainFallback(ctx, mcpClient, engine, fmt.Sprintf("step4-only:%v", err))
	}
	if step1 != nil && step1.MacroThesisUpdate != nil {
		step4.MacroThesisUpdate = step1.MacroThesisUpdate
	}
	step4.CoTTrace = "[chain:step4-only] " + cot.String() + step4.CoTTrace
	return step4, nil
}

// decisionGenerationCall is Step 4 of the chain. Receives a curated candidate
// list (from Steps 1-3 in later commits) and emits []Decision via the existing
// parseFullDecisionResponse so DB serialization stays identical.
func decisionGenerationCall(ctx *Context, engine *StrategyEngine, mcpClient mcp.AIClient, candidates []CandidateCoin) (*FullDecision, error) {
	riskCfg := engine.GetRiskControlConfig()
	ps := engine.config.PromptSections

	skeleton := strings.ReplaceAll(PromptStep4DecisionSystemV2, "{{max_leverage}}", fmt.Sprintf("%d", riskCfg.EffectiveMaxLeverage()))
	systemPrompt := renderChainSystemPrompt(
		skeleton,
		ps.RoleDefinition,
		ps.TradingFrequency,
		ps.EntryStandards,
		ps.DecisionProcess,
	)
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

func renderStep4User(ctx *Context, engine *StrategyEngine, candidates []CandidateCoin) (string, error) {
	var candidatesText string
	if len(candidates) == 0 {
		candidatesText = "[]  // 本周期 Step 1-3 未筛出新机会"
	} else {
		b, err := json.MarshalIndent(candidates, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal candidates: %w", err)
		}
		candidatesText = string(b)
	}

	riskCfg := engine.GetRiskControlConfig()
	availableSlots := riskCfg.MaxPositions - len(ctx.Positions)
	if availableSlots < 0 {
		availableSlots = 0
	}

	out := PromptStep4DecisionUserV2
	out = strings.ReplaceAll(out, "{{candidates_json}}", candidatesText)
	out = strings.ReplaceAll(out, "{{positions_summary}}", summarizePositions(ctx.Positions))
	out = strings.ReplaceAll(out, "{{equity}}", fmt.Sprintf("%.2f", ctx.Account.TotalEquity))
	out = strings.ReplaceAll(out, "{{margin_pct}}", fmt.Sprintf("%.1f", ctx.Account.MarginUsedPct))
	out = strings.ReplaceAll(out, "{{slots}}", fmt.Sprintf("%d", availableSlots))
	out = strings.ReplaceAll(out, "{{max_leverage}}", fmt.Sprintf("%d", riskCfg.EffectiveMaxLeverage()))
	out = strings.ReplaceAll(out, "{{min_position_size}}", fmt.Sprintf("%.2f", riskCfg.MinPositionSize))
	out = strings.ReplaceAll(out, "{{max_pos_ratio}}", fmt.Sprintf("%.2f", riskCfg.EffectiveMaxPositionValueRatio()))
	out = strings.ReplaceAll(out, "{{candidates_market_data}}", formatChainMarketData(ctx, engine, candidates))
	out = strings.ReplaceAll(out, "{{positions_market_data}}", formatChainPositionsMarketData(ctx, engine))
	return out, nil
}

// formatChainPositionsMarketData renders full per-symbol market data for every
// OPEN position. Used by Step 4 to give the AI rich context for hold/close/add
// decisions on existing positions (vs. just a one-line summary). Reuses the
// same engine.formatMarketData() helper that single-prompt path uses.
func formatChainPositionsMarketData(ctx *Context, engine *StrategyEngine) string {
	if len(ctx.Positions) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, p := range ctx.Positions {
		md, ok := ctx.MarketDataMap[p.Symbol]
		if !ok || md == nil {
			fmt.Fprintf(&sb, "\n=== %s === (no market data, only summary above)\n", p.Symbol)
			continue
		}
		sb.WriteString(engine.formatMarketData(md))
	}
	return sb.String()
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
