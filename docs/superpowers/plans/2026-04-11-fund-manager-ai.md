# Fund Manager AI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the trading agent from a stateless momentum-chasing bot into a stateful AI fund manager that maintains a macro thesis, manages portfolio-level risk, and pursues beta+alpha on tokenized US stock perpetual contracts.

**Architecture:** Add a persistent `macro_thesis` layer per trader so the AI remembers its market view across cycles. Extend the trading context with portfolio-level exposure metrics and the user's externally-pushed macro reports. Rewrite the system prompt to frame the AI as a fund manager rather than a scalper. Replace all hardcoded crypto-centric risk constants with configurable, stock-optimized parameters.

**Tech Stack:** Go 1.25+, GORM (SQLite), existing `store/` + `kernel/` + `trader/` packages. No new external dependencies.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `store/macro_thesis.go` | **CREATE** | MacroThesis struct + CRUD store |
| `store/strategy.go` | **MODIFY** | Add session scaling, drawdown config, symbol categories to RiskControlConfig |
| `store/position.go` | **MODIFY** | Add `intent_type` + `entry_thesis` columns to TraderPosition |
| `store/store.go` | **MODIFY** | Register MacroThesisStore in lazy init + initTables |
| `kernel/engine.go` | **MODIFY** | Extend Context + Decision + FullDecision with macro/portfolio fields; add new types |
| `trader/auto_trader_loop.go` | **MODIFY** | Enrich buildTradingContext(); process MacroThesisUpdate post-AI; read macro report file |
| `trader/auto_trader_risk.go` | **MODIFY** | Use configurable drawdown thresholds; session scale in enforcePositionValueRatio |
| `trader/auto_trader_orders.go` | **MODIFY** | Session scale gate on opens; capture + persist position intent |
| `kernel/engine_prompt.go` | **MODIFY** | Fund manager system prompt; inject macro thesis + portfolio exposure in user prompt |
| `kernel/prompt_builder.go` | **MODIFY** | Fund manager framing in fallback builder |
| `macro_reports/.gitkeep` | **CREATE** | Directory for user-pushed macro reports |

---

## Task 1: RiskControlConfig Extensions

**Files:**
- Modify: `store/strategy.go` — add fields + helper methods to `RiskControlConfig`

- [ ] **Step 1: Add fields to RiskControlConfig**

Locate the `RiskControlConfig` struct (around line 262) and add after the existing `MinConfidence` field:

```go
// --- Stock Trading Extensions ---

// Session-based risk scaling. Keys: "us_market_open", "us_pre_market",
// "us_after_hours", "us_market_closed". Values are multipliers (0.0–1.0)
// applied to both MaxLeverage and MaxPositionValueRatio.
// Defaults: open=1.0, pre=0.5, after=0.3, closed=0.05
SessionRiskScale map[string]float64 `json:"session_risk_scale,omitempty"`

// Symbol → asset category mapping for portfolio-level correlation control.
// e.g. {"NVDAUSDT":"semiconductor","QQQUSDT":"index","XAUUSDT":"commodity"}
SymbolCategories map[string]string `json:"symbol_categories,omitempty"`

// Max concurrent open positions in the SAME category AND same direction.
// 0 = disabled (no category-level limit beyond MaxPositions).
MaxSameCategoryPositions int `json:"max_same_category_positions,omitempty"`

// Trailing stop: minimum profit % before drawdown monitor activates.
// Default: 0.03 (3%). Replaces old hardcoded 5%.
DrawdownActivationProfit float64 `json:"drawdown_activation_profit,omitempty"`

// Trailing stop: close position when it retraces this % from its peak.
// Default: 0.25 (25%). Replaces old hardcoded 40%.
DrawdownCloseThreshold float64 `json:"drawdown_close_threshold,omitempty"`
```

- [ ] **Step 2: Add helper methods after EffectiveMaxPositionValueRatio()**

```go
// GetSessionRiskScale returns the risk scale factor for the given US trading session.
// Scale is multiplied against both MaxLeverage and MaxPositionValueRatio.
func (r RiskControlConfig) GetSessionRiskScale(session string) float64 {
	if r.SessionRiskScale != nil {
		if scale, ok := r.SessionRiskScale[session]; ok {
			return scale
		}
	}
	switch session {
	case "us_market_open":
		return 1.0
	case "us_pre_market":
		return 0.5
	case "us_after_hours":
		return 0.3
	default: // us_market_closed, weekend
		return 0.05
	}
}

// GetSymbolCategory returns the asset category for a symbol (empty = uncategorized).
func (r RiskControlConfig) GetSymbolCategory(symbol string) string {
	if r.SymbolCategories == nil {
		return ""
	}
	return r.SymbolCategories[symbol]
}

// EffectiveDrawdownActivationProfit returns the minimum profit % required before
// the trailing stop activates. Defaults to 0.03 (3%) if not configured.
func (r RiskControlConfig) EffectiveDrawdownActivationProfit() float64 {
	if r.DrawdownActivationProfit > 0 {
		return r.DrawdownActivationProfit
	}
	return 0.03
}

// EffectiveDrawdownCloseThreshold returns the drawdown % from peak that triggers
// emergency close. Defaults to 0.25 (25%) if not configured.
func (r RiskControlConfig) EffectiveDrawdownCloseThreshold() float64 {
	if r.DrawdownCloseThreshold > 0 {
		return r.DrawdownCloseThreshold
	}
	return 0.25
}
```

- [ ] **Step 3: Update GetDefaultStrategyConfig() with stock-optimized defaults**

Find `GetDefaultStrategyConfig()` and inside the `RiskControl` block, add after the existing fields:

```go
SessionRiskScale: map[string]float64{
    "us_market_open":   1.0,
    "us_pre_market":    0.5,
    "us_after_hours":   0.3,
    "us_market_closed": 0.05,
},
SymbolCategories: map[string]string{
    "TSLAUSDT": "ev_auto",
    "NVDAUSDT": "semiconductor",
    "XAUUSDT":  "commodity",
    "QQQUSDT":  "index",
    "SPYUSDT":  "index",
    "CLUSDT":   "commodity",
    "METAUSDT": "tech_mega",
    "AMAZUSDT": "tech_mega",
    "GOOGLUSDT": "tech_mega",
    "INTCUSDT": "semiconductor",
    "MUUSDT":   "semiconductor",
    "TSMUUSDT": "semiconductor",
    "SNDKUSDT": "semiconductor",
},
MaxSameCategoryPositions: 2,
DrawdownActivationProfit:  0.03,
DrawdownCloseThreshold:    0.25,
```

- [ ] **Step 4: Verify the file compiles**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./store/...
```
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add store/strategy.go
git commit -m "feat(risk): add session scaling, symbol categories, configurable drawdown to RiskControlConfig"
```

---

## Task 2: MacroThesis Store

**Files:**
- Create: `store/macro_thesis.go`

- [ ] **Step 1: Create store/macro_thesis.go**

```go
package store

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// MacroThesis stores the AI fund manager's current macroeconomic market thesis per trader.
// A new record is created each time the AI updates its view; history is preserved.
type MacroThesis struct {
	ID              uint      `gorm:"primaryKey;autoIncrement"`
	TraderID        string    `gorm:"not null;index"`
	MarketRegime    string    `gorm:"not null"` // risk_on | risk_off | mixed | cautious
	ThesisText      string    `gorm:"not null;type:text"`
	SectorBias      string    `gorm:"type:text"` // JSON: {"semiconductor":"bullish","index":"neutral"}
	KeyRisks        string    `gorm:"type:text"` // JSON: ["Fed rate hike risk","earnings miss"]
	PortfolioIntent string    // e.g. "building_tech_long", "defensive_hedged", "reducing_exposure"
	ValidHours      int       `gorm:"default:24"`
	Source          string    `gorm:"default:'ai'"` // "ai" | "manual"
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (MacroThesis) TableName() string { return "macro_thesis" }

// MacroThesisStore handles persistence of macro thesis records.
type MacroThesisStore struct {
	db *gorm.DB
}

// NewMacroThesisStore creates a new store and auto-migrates the table.
func NewMacroThesisStore(db *gorm.DB) *MacroThesisStore {
	db.AutoMigrate(&MacroThesis{})
	return &MacroThesisStore{db: db}
}

// initTables ensures the table exists (called from store.go initTables chain).
func (s *MacroThesisStore) initTables() error {
	return s.db.AutoMigrate(&MacroThesis{})
}

// GetLatest returns the most recent thesis for a trader, or nil if none exists.
func (s *MacroThesisStore) GetLatest(traderID string) (*MacroThesis, error) {
	var thesis MacroThesis
	result := s.db.Where("trader_id = ?", traderID).
		Order("created_at DESC").
		First(&thesis)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get macro thesis: %w", result.Error)
	}
	return &thesis, nil
}

// Create persists a new thesis record (history is kept; no in-place updates).
func (s *MacroThesisStore) Create(thesis *MacroThesis) error {
	if err := s.db.Create(thesis).Error; err != nil {
		return fmt.Errorf("create macro thesis: %w", err)
	}
	return nil
}

// IsStale returns true if the thesis has exceeded its valid_hours window.
func (t *MacroThesis) IsStale() bool {
	return time.Since(t.UpdatedAt) > time.Duration(t.ValidHours)*time.Hour
}

// ParseSectorBias deserializes the JSON sector bias field.
func (t *MacroThesis) ParseSectorBias() map[string]string {
	if t.SectorBias == "" {
		return nil
	}
	var out map[string]string
	json.Unmarshal([]byte(t.SectorBias), &out) //nolint:errcheck
	return out
}

// ParseKeyRisks deserializes the JSON key risks field.
func (t *MacroThesis) ParseKeyRisks() []string {
	if t.KeyRisks == "" {
		return nil
	}
	var out []string
	json.Unmarshal([]byte(t.KeyRisks), &out) //nolint:errcheck
	return out
}

// EncodeSectorBias serializes a sector bias map to a JSON string for storage.
func EncodeSectorBias(bias map[string]string) string {
	if bias == nil {
		return ""
	}
	b, _ := json.Marshal(bias)
	return string(b)
}

// EncodeKeyRisks serializes a key risks slice to a JSON string for storage.
func EncodeKeyRisks(risks []string) string {
	if risks == nil {
		return ""
	}
	b, _ := json.Marshal(risks)
	return string(b)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./store/...
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add store/macro_thesis.go
git commit -m "feat(store): add MacroThesis store for persistent AI macro thesis"
```

---

## Task 3: Position Intent Fields

**Files:**
- Modify: `store/position.go` — add `IntentType` + `EntryThesis` to `TraderPosition`

- [ ] **Step 1: Add fields to TraderPosition struct**

Locate the `TraderPosition` struct in `store/position.go`. Add after the `CloseReason` field:

```go
// Position intent set by AI fund manager at entry time
IntentType  string `gorm:"column:intent_type"`       // core_beta | tactical_alpha | hedge | opportunistic
EntryThesis string `gorm:"column:entry_thesis;type:text"` // AI's reasoning for entering this position
```

- [ ] **Step 2: Add UpdatePositionIntent method to PositionStore**

Add this method to `store/position.go` after the existing Update methods:

```go
// UpdatePositionIntent stores the intent type and entry thesis for an open position.
// Called after the AI opens a new position with intent metadata.
func (s *PositionStore) UpdatePositionIntent(positionID uint, intentType, entryThesis string) error {
	result := s.db.Model(&TraderPosition{}).
		Where("id = ?", positionID).
		Updates(map[string]interface{}{
			"intent_type":  intentType,
			"entry_thesis": entryThesis,
		})
	if result.Error != nil {
		return fmt.Errorf("update position intent: %w", result.Error)
	}
	return nil
}
```

- [ ] **Step 3: Verify compilation (GORM will auto-migrate on next run)**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./store/...
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add store/position.go
git commit -m "feat(store): add intent_type and entry_thesis fields to TraderPosition"
```

---

## Task 4: Register MacroThesisStore in store.go

**Files:**
- Modify: `store/store.go`

- [ ] **Step 1: Add macroThesis field to Store struct**

Locate the `Store` struct (around line 15) and add `macroThesis` to the sub-stores block:

```go
macroThesis     *MacroThesisStore
```

Keep the single `mu sync.RWMutex` — it already covers all sub-stores.

- [ ] **Step 2: Add MacroThesis() accessor method**

Add after the `AICharge()` method (around line 292):

```go
// MacroThesis gets macro thesis storage.
func (s *Store) MacroThesis() *MacroThesisStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.macroThesis == nil {
		s.macroThesis = NewMacroThesisStore(s.gdb)
	}
	return s.macroThesis
}
```

- [ ] **Step 3: Register in initTables()**

Inside `initTables()`, add after the AICharge line:

```go
if err := s.MacroThesis().initTables(); err != nil {
    return fmt.Errorf("failed to initialize macro thesis tables: %w", err)
}
```

- [ ] **Step 4: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./store/...
```
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add store/store.go
git commit -m "feat(store): register MacroThesisStore in Store"
```

---

## Task 5: Extend kernel/engine.go Types

**Files:**
- Modify: `kernel/engine.go` — add new types + extend Context and Decision structs

- [ ] **Step 1: Add new types after the existing type block (after line ~177)**

Insert these new types before the `StrategyEngine` struct definition:

```go
// MacroThesisContext is the AI's current macro thesis injected into each trading cycle.
type MacroThesisContext struct {
	MarketRegime    string            `json:"market_regime"`             // risk_on|risk_off|mixed|cautious
	ThesisText      string            `json:"thesis_text"`
	SectorBias      map[string]string `json:"sector_bias,omitempty"`     // {"semiconductor":"bullish"}
	KeyRisks        []string          `json:"key_risks,omitempty"`       // ["Fed hike risk"]
	PortfolioIntent string            `json:"portfolio_intent"`          // "building_tech_long"
	AgeHours        float64           `json:"age_hours"`                 // hours since last update
	Source          string            `json:"source"`                    // "ai" | "manual"
}

// PortfolioExposure aggregates portfolio-level risk metrics for the AI's awareness.
type PortfolioExposure struct {
	CategoryBreakdown map[string]float64 `json:"category_breakdown"` // category→ total USD notional
	NetLongUSD        float64            `json:"net_long_usd"`
	NetShortUSD       float64            `json:"net_short_usd"`
	NetDirection      string             `json:"net_direction"` // "net_long"|"net_short"|"balanced"
	CoreBetaUSD       float64            `json:"core_beta_usd"`
	TacticalAlphaUSD  float64            `json:"tactical_alpha_usd"`
	HedgeUSD          float64            `json:"hedge_usd"`
}

// MacroThesisUpdate is the AI's proposed update to its macro thesis.
// Returned as an optional field inside a Decision; persisted after each cycle.
type MacroThesisUpdate struct {
	MarketRegime    string            `json:"market_regime"`
	ThesisText      string            `json:"thesis_text"`
	SectorBias      map[string]string `json:"sector_bias,omitempty"`
	KeyRisks        []string          `json:"key_risks,omitempty"`
	PortfolioIntent string            `json:"portfolio_intent"`
	ValidHours      int               `json:"valid_hours"` // 0 = use default (24h)
}
```

- [ ] **Step 2: Extend the Context struct**

Find the `Context` struct (around line 90). Add these fields at the end of the struct:

```go
// Fund manager extensions
MacroThesis        *MacroThesisContext `json:"macro_thesis,omitempty"`
MacroReport        string              `json:"macro_report,omitempty"`    // user-pushed external report
PortfolioExposure  *PortfolioExposure  `json:"portfolio_exposure,omitempty"`
SessionScaleFactor float64             `json:"session_scale_factor"`      // current session's risk multiplier
```

- [ ] **Step 3: Extend the Decision struct**

Find the `Decision` struct (around line 114). Add these fields at the end:

```go
// Fund manager extensions (optional, AI may or may not include these)
IntentType        string             `json:"intent_type,omitempty"`         // core_beta|tactical_alpha|hedge|opportunistic
EntryThesis       string             `json:"entry_thesis,omitempty"`        // why entering this position
MacroThesisUpdate *MacroThesisUpdate `json:"macro_thesis_update,omitempty"` // proposed thesis update
```

- [ ] **Step 4: Extend FullDecision struct**

Find `FullDecision` struct (around line 138). Add:

```go
MacroThesisUpdate *MacroThesisUpdate `json:"macro_thesis_update,omitempty"`
```

- [ ] **Step 5: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./kernel/...
```
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add kernel/engine.go
git commit -m "feat(kernel): extend Context/Decision/FullDecision with macro thesis and portfolio exposure types"
```

---

## Task 6: Create macro_reports Directory

**Files:**
- Create: `macro_reports/.gitkeep`
- Create: `macro_reports/README.md`

- [ ] **Step 1: Create the directory and files**

```bash
mkdir -p /Users/xiagao/Desktop/nofx-binance-stock-main/macro_reports
touch /Users/xiagao/Desktop/nofx-binance-stock-main/macro_reports/.gitkeep
```

- [ ] **Step 2: Create README.md**

Create `macro_reports/README.md`:

```markdown
# Macro Reports

Place your macro analysis reports here as `latest.md`.

The trading engine reads `latest.md` at the start of each cycle and injects it
into the AI's context as a high-priority signal. Reports older than 48 hours
are still used but flagged as stale.

## Format

Plain Markdown. Example:

```
# Macro Report — 2026-04-11

## Market Regime
Risk-on. Fed signaled pause. SPY holding above 200 DMA.

## Sector Bias
- Semiconductor: Bullish (NVDA earnings beat, AI capex cycle intact)
- Index (QQQ/SPY): Neutral-Bullish (tech leadership continuing)
- Gold (XAU): Neutral (DXY range-bound, no safe-haven bid)

## Key Risks
- FOMC minutes release Thu 14:00 ET
- NVDA earnings next week

## Portfolio Guidance
Prefer long semiconductor + index exposure. Light gold hedge acceptable.
Avoid heavy short exposure until FOMC clears.
```
```

- [ ] **Step 3: Commit**

```bash
git add macro_reports/
git commit -m "feat: add macro_reports/ directory for user-pushed macro analysis"
```

---

## Task 7: Enrich buildTradingContext() and Process MacroThesisUpdate

**Files:**
- Modify: `trader/auto_trader_loop.go`

This is the central wiring task. Three additions:
1. Load macro thesis from DB + read macro report file → populate `ctx.MacroThesis`, `ctx.MacroReport`
2. Calculate portfolio exposure → populate `ctx.PortfolioExposure`
3. After AI returns, extract MacroThesisUpdate from decisions → persist to DB

- [ ] **Step 1: Add readMacroReport() helper function**

Add this function before `buildTradingContext()` in `auto_trader_loop.go`:

```go
// readMacroReport reads the user-pushed macro report from macro_reports/latest.md.
// Returns empty string if the file does not exist; logs a warning if older than 48h.
func readMacroReport() string {
	path := "macro_reports/latest.md"
	info, err := os.Stat(path)
	if err != nil {
		return "" // file not present — normal
	}
	age := time.Since(info.ModTime())
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	if age > 48*time.Hour {
		// Still inject but mark as stale so AI is aware
		return fmt.Sprintf("[STALE — %s old]\n\n%s", age.Round(time.Hour).String(), content)
	}
	return content
}
```

Make sure `"os"` and `"strings"` and `"fmt"` are in the import block (they likely already are; add if missing).

- [ ] **Step 2: Add calculatePortfolioExposure() helper**

Add before `buildTradingContext()`:

```go
// calculatePortfolioExposure aggregates portfolio-level risk metrics from open positions.
func calculatePortfolioExposure(positions []kernel.PositionInfo, riskConfig store.RiskControlConfig) *kernel.PortfolioExposure {
	if len(positions) == 0 {
		return nil
	}
	exp := &kernel.PortfolioExposure{
		CategoryBreakdown: make(map[string]float64),
	}
	for _, p := range positions {
		notional := p.Quantity * p.MarkPrice
		cat := riskConfig.GetSymbolCategory(p.Symbol)
		if cat == "" {
			cat = "other"
		}
		exp.CategoryBreakdown[cat] += notional

		if strings.ToLower(p.Side) == "long" {
			exp.NetLongUSD += notional
		} else {
			exp.NetShortUSD += notional
		}

		switch p.IntentType {
		case "core_beta":
			exp.CoreBetaUSD += notional
		case "tactical_alpha":
			exp.TacticalAlphaUSD += notional
		case "hedge":
			exp.HedgeUSD += notional
		}
	}
	net := exp.NetLongUSD - exp.NetShortUSD
	switch {
	case net > exp.NetLongUSD*0.2:
		exp.NetDirection = "net_long"
	case net < -exp.NetShortUSD*0.2:
		exp.NetDirection = "net_short"
	default:
		exp.NetDirection = "balanced"
	}
	return exp
}
```

Note: `kernel.PositionInfo` needs an `IntentType string` field — add it in Task 5's Step 2 was missed; add `IntentType string \`json:"intent_type,omitempty"\`` to `PositionInfo` in `kernel/engine.go`.

- [ ] **Step 3: Inject macro data in buildTradingContext()**

Inside `buildTradingContext()`, after the existing context population (after positions are set), add:

```go
// --- Fund Manager Extensions ---

// 1. Load persisted macro thesis
if thesis, err := t.store.MacroThesis().GetLatest(t.config.ID); err == nil && thesis != nil {
    age := time.Since(thesis.UpdatedAt).Hours()
    ctx.MacroThesis = &kernel.MacroThesisContext{
        MarketRegime:    thesis.MarketRegime,
        ThesisText:      thesis.ThesisText,
        SectorBias:      thesis.ParseSectorBias(),
        KeyRisks:        thesis.ParseKeyRisks(),
        PortfolioIntent: thesis.PortfolioIntent,
        AgeHours:        age,
        Source:          thesis.Source,
    }
    if thesis.IsStale() {
        logger.Debugf("[%s] macro thesis is stale (%.1fh old), AI will be asked to update", t.config.Name, age)
    }
}

// 2. Read user-pushed macro report
ctx.MacroReport = readMacroReport()

// 3. Calculate portfolio exposure
riskCfg := store.RiskControlConfig{}
if t.config.StrategyConfig != nil {
    riskCfg = t.config.StrategyConfig.RiskControl
}
ctx.PortfolioExposure = calculatePortfolioExposure(ctx.Positions, riskCfg)

// 4. Session scale factor
session := kernel.GetUSTradingSession(time.Now().UTC())
ctx.SessionScaleFactor = riskCfg.GetSessionRiskScale(session)
```

- [ ] **Step 4: Populate IntentType in PositionInfo**

In `buildTradingContext()`, where positions are mapped from DB to `kernel.PositionInfo`, add:

```go
posInfo.IntentType = dbPos.IntentType
```

(Find the loop that populates `ctx.Positions` and add this line.)

- [ ] **Step 5: Process MacroThesisUpdate after AI response**

In `runCycle()`, after `kernel.GetFullDecisionWithStrategy()` returns and decisions are available, add:

```go
// Persist any macro thesis update proposed by the AI
for _, d := range fullDecision.Decisions {
    if d.MacroThesisUpdate != nil {
        u := d.MacroThesisUpdate
        validHours := u.ValidHours
        if validHours <= 0 {
            validHours = 24
        }
        thesis := &store.MacroThesis{
            TraderID:        t.config.ID,
            MarketRegime:    u.MarketRegime,
            ThesisText:      u.ThesisText,
            SectorBias:      store.EncodeSectorBias(u.SectorBias),
            KeyRisks:        store.EncodeKeyRisks(u.KeyRisks),
            PortfolioIntent: u.PortfolioIntent,
            ValidHours:      validHours,
            Source:          "ai",
        }
        if err := t.store.MacroThesis().Create(thesis); err != nil {
            logger.Warnf("[%s] failed to save macro thesis update: %v", t.config.Name, err)
        } else {
            logger.Infof("[%s] macro thesis updated: regime=%s intent=%s",
                t.config.Name, u.MarketRegime, u.PortfolioIntent)
        }
        break // only one thesis update per cycle
    }
}
```

- [ ] **Step 6: Ensure imports are present**

Make sure these are in the import block of `auto_trader_loop.go`:
- `"os"`
- `"nofx/store"` (already present)
- `"nofx/kernel"` (already present)

- [ ] **Step 7: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./trader/...
```
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add trader/auto_trader_loop.go
git commit -m "feat(trader): enrich buildTradingContext with macro thesis, portfolio exposure, session scale; process MacroThesisUpdate post-AI"
```

---

## Task 8: Session Scaling + Configurable Drawdown in auto_trader_risk.go

**Files:**
- Modify: `trader/auto_trader_risk.go`

- [ ] **Step 1: Replace hardcoded drawdown constants with config values**

Find `checkPositionDrawdown()`. It currently has hardcoded values like `0.40` and `0.05`. Replace them:

```go
// Before (hardcoded):
if peakPnLPct > 0.05 && drawdownFromPeak >= 0.40 {

// After (from config):
riskCfg := t.config.StrategyConfig.RiskControl
activationProfit := riskCfg.EffectiveDrawdownActivationProfit()
closeThreshold := riskCfg.EffectiveDrawdownCloseThreshold()
if peakPnLPct > activationProfit && drawdownFromPeak >= closeThreshold {
```

- [ ] **Step 2: Apply session scale in enforcePositionValueRatio()**

Find `enforcePositionValueRatio()`. After computing `equity` and `ratio`, add session scaling:

```go
// Session-based risk scaling: reduce position limit outside US market hours
session := kernel.GetUSTradingSession(time.Now().UTC())
scale := t.config.StrategyConfig.RiskControl.GetSessionRiskScale(session)
maxPositionValue := equity * ratio * scale
```

Replace any existing `maxPositionValue := equity * ratio` line with the above.

- [ ] **Step 3: Add category-based limit in enforceMaxPositions()**

Find `enforceMaxPositions()`. After the existing total positions check, add:

```go
// Category-based limit: prevent over-concentration in a single asset class + direction
maxSameCat := t.config.StrategyConfig.RiskControl.MaxSameCategoryPositions
if maxSameCat > 0 && symbol != "" {
    category := t.config.StrategyConfig.RiskControl.GetSymbolCategory(symbol)
    if category != "" {
        sameCount := 0
        for _, p := range openPositions {
            if t.config.StrategyConfig.RiskControl.GetSymbolCategory(p.Symbol) == category &&
                strings.EqualFold(p.Side, side) {
                sameCount++
            }
        }
        if sameCount >= maxSameCat {
            return fmt.Errorf("max same-category positions reached: %d/%d in category '%s' (%s)",
                sameCount, maxSameCat, category, side)
        }
    }
}
```

Note: `enforceMaxPositions()` needs to accept `symbol` and `side` parameters to enable this check. If the current signature is `enforceMaxPositions(openPositions []...)`, update the callers in `auto_trader_orders.go` to pass `symbol` and `side`.

Signature change:
```go
// Before:
func (t *AutoTrader) enforceMaxPositions(openPositions []store.TraderPosition) error

// After:
func (t *AutoTrader) enforceMaxPositions(openPositions []store.TraderPosition, symbol, side string) error
```

Update callers in `auto_trader_orders.go`:
```go
// In executeOpenLongWithRecord:
if err := t.enforceMaxPositions(openPositions, decision.Symbol, "long"); err != nil {

// In executeOpenShortWithRecord:
if err := t.enforceMaxPositions(openPositions, decision.Symbol, "short"); err != nil {
```

- [ ] **Step 4: Add "strings" import if not present**

```go
import "strings"
```

- [ ] **Step 5: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./trader/...
```
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add trader/auto_trader_risk.go trader/auto_trader_orders.go
git commit -m "feat(risk): session-scaled position limits, configurable drawdown thresholds, category concentration guard"
```

---

## Task 9: Capture Position Intent in auto_trader_orders.go

**Files:**
- Modify: `trader/auto_trader_orders.go`

- [ ] **Step 1: Capture intent after successful long open**

In `executeOpenLongWithRecord()`, after the position is successfully created in the DB and `positionID` is available, add:

```go
// Persist AI-assigned position intent if provided
if decision.IntentType != "" {
    if err := t.store.Position().UpdatePositionIntent(positionID, decision.IntentType, decision.EntryThesis); err != nil {
        logger.Warnf("[%s] failed to store position intent: %v", t.config.Name, err)
        // Non-fatal: position is open, intent tracking is best-effort
    }
}
```

- [ ] **Step 2: Same for executeOpenShortWithRecord()**

Add the identical block in `executeOpenShortWithRecord()` after its position creation.

- [ ] **Step 3: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./trader/...
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add trader/auto_trader_orders.go
git commit -m "feat(trader): persist position intent (core_beta/tactical_alpha/hedge) after open"
```

---

## Task 10: Fund Manager System Prompt Rewrite (kernel/engine_prompt.go)

**Files:**
- Modify: `kernel/engine_prompt.go`

This is the most impactful change. The goal is to replace the scalper/momentum framing with a fund manager framing. We modify `BuildSystemPrompt()` and `BuildUserPrompt()`.

- [ ] **Step 1: Replace the role definition section in BuildSystemPrompt()**

Find where the role prompt is built (look for `"你是一个专业的量化交易"` or `"You are a professional quantitative trading"`). Replace the role definition with:

**For Chinese (zh):**
```go
const fundManagerRoleZH = `你是一位管理美股映射永续合约投资组合的AI基金经理。

## 你的核心职责
你管理的不是一笔笔独立的交易，而是一个有机的投资组合。每一个操作都必须服务于整体组合的目标：
**在控制风险的前提下，通过 β 暴露捕获市场涨幅，通过主动管理获取超额 α。**

## 决策层次
1. **宏观判断优先**：首先评估当前宏观环境和你的市场论文（thesis）是否仍然成立。如有必要，更新你的宏观判断。
2. **组合层面次之**：评估当前持仓的整体结构——β 暴露、板块集中度、多空方向。
3. **个股执行最后**：在前两层框架内，决定对具体标的的操作。

## 仓位类型
- **core_beta**：核心指数仓，跟踪大盘方向（如 QQQ/SPY）。持有时间较长，杠杆较低。
- **tactical_alpha**：战术仓，利用板块或个股的短期机会超额获利（如 NVDA 财报前布局）。
- **hedge**：对冲仓，降低整体组合净风险（如科技多配 + 黄金多配）。
- **opportunistic**：纯机会仓，基于明确催化剂的短线操作。

## 信号优先级
宏观/基本面信号 > 板块资金流向 > 技术面信号
技术分析是执行参考，不是开仓的主要依据。

## 关于宏观论文更新
如果你认为当前宏观环境发生了重要变化，在你的某一个决策对象里加入 macro_thesis_update 字段来更新你的判断。每次循环最多更新一次。`
```

**For English (en):**
```go
const fundManagerRoleEN = `You are an AI fund manager operating a portfolio of tokenized US stock perpetual contracts.

## Core Mandate
You do not manage individual trades in isolation — you manage a living portfolio.
Every action must serve the portfolio's dual objective:
**Capture market beta returns while generating excess alpha through active management.**

## Decision Hierarchy
1. **Macro thesis first:** Assess whether your current market thesis still holds. Update it if material conditions have changed.
2. **Portfolio structure second:** Evaluate the aggregate — beta exposure, sector concentration, net direction (long/short).
3. **Individual execution last:** Within the macro + portfolio framework, decide actions on specific instruments.

## Position Intent Types
- **core_beta:** Index-tracking positions (QQQ/SPY). Longer hold, lower leverage.
- **tactical_alpha:** Sector or single-name positions exploiting a specific catalyst or mispricing.
- **hedge:** Positions that reduce portfolio net risk (e.g., adding XAU when long tech-heavy).
- **opportunistic:** Short-duration, catalyst-driven positions.

## Signal Priority
Macro/fundamental signals > Sector flow signals > Technical signals.
Technical analysis informs entry/exit timing — it is NOT the primary basis for position decisions.

## Macro Thesis Updates
If you believe a material macro shift has occurred, include a macro_thesis_update field in one of your decisions. At most one update per cycle.`
```

- [ ] **Step 2: Add macro thesis and portfolio exposure to BuildUserPrompt()**

Find `BuildUserPrompt()` (or the equivalent function that formats the user-facing prompt). After the trading session line and before the account info section, inject the macro context:

```go
// --- Macro Context (Fund Manager Layer) ---
if ctx.MacroThesis != nil {
    th := ctx.MacroThesis
    staleNote := ""
    if th.AgeHours > float64(24) {
        staleNote = fmt.Sprintf(" [⚠️ STALE — %.0fh old, consider updating]", th.AgeHours)
    }
    sb.WriteString(fmt.Sprintf("\n## 当前宏观论文 (%.1fh ago, source: %s)%s\n", th.AgeHours, th.Source, staleNote))
    sb.WriteString(fmt.Sprintf("市场环境: %s\n", th.MarketRegime))
    sb.WriteString(fmt.Sprintf("论文: %s\n", th.ThesisText))
    if th.PortfolioIntent != "" {
        sb.WriteString(fmt.Sprintf("组合意图: %s\n", th.PortfolioIntent))
    }
    if len(th.SectorBias) > 0 {
        sb.WriteString("板块偏向:\n")
        for sector, bias := range th.SectorBias {
            sb.WriteString(fmt.Sprintf("  - %s: %s\n", sector, bias))
        }
    }
    if len(th.KeyRisks) > 0 {
        sb.WriteString("关键风险:\n")
        for _, r := range th.KeyRisks {
            sb.WriteString(fmt.Sprintf("  - %s\n", r))
        }
    }
} else {
    sb.WriteString("\n## 宏观论文\n尚无宏观论文。请在本次决策中通过 macro_thesis_update 建立你的初始判断。\n")
}

if ctx.MacroReport != "" {
    sb.WriteString(fmt.Sprintf("\n## 外部宏观报告（用户提供，高优先级参考）\n%s\n", ctx.MacroReport))
}

if ctx.PortfolioExposure != nil {
    pe := ctx.PortfolioExposure
    sb.WriteString(fmt.Sprintf("\n## 当前组合暴露\n方向: %s (多头: $%.0f | 空头: $%.0f)\n",
        pe.NetDirection, pe.NetLongUSD, pe.NetShortUSD))
    sb.WriteString(fmt.Sprintf("核心β仓: $%.0f | 战术α仓: $%.0f | 对冲仓: $%.0f\n",
        pe.CoreBetaUSD, pe.TacticalAlphaUSD, pe.HedgeUSD))
    if len(pe.CategoryBreakdown) > 0 {
        sb.WriteString("板块分布:\n")
        for cat, usd := range pe.CategoryBreakdown {
            sb.WriteString(fmt.Sprintf("  - %s: $%.0f\n", cat, usd))
        }
    }
}

sb.WriteString(fmt.Sprintf("\n## 当前交易时段风险系数: %.2f\n", ctx.SessionScaleFactor))
if ctx.SessionScaleFactor < 0.5 {
    sb.WriteString("⚠️ 低流动性时段：建议减小仓位规模，避免开新的主要仓位。\n")
}
```

Use `strings.Builder` (`sb`) matching the existing code pattern. Adapt variable names to match what the function already uses.

- [ ] **Step 3: Update the output format instructions to include new optional fields**

In the JSON format section (where symbol/action/leverage etc. are documented), add:

```
// Optional fund manager fields (include when relevant):
"intent_type": "core_beta|tactical_alpha|hedge|opportunistic",
"entry_thesis": "why you are entering this specific position",
"macro_thesis_update": {           // include ONCE per cycle if thesis needs updating
  "market_regime": "risk_on|risk_off|mixed|cautious",
  "thesis_text": "your updated macro view",
  "sector_bias": {"semiconductor": "bullish", "index": "neutral"},
  "key_risks": ["risk1", "risk2"],
  "portfolio_intent": "building_tech_long",
  "valid_hours": 24
}
```

- [ ] **Step 4: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./kernel/...
```
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add kernel/engine_prompt.go
git commit -m "feat(prompt): rewrite system prompt to fund manager framing; inject macro thesis + portfolio exposure into user prompt"
```

---

## Task 11: Update Fallback Prompt Builder (kernel/prompt_builder.go)

**Files:**
- Modify: `kernel/prompt_builder.go`

- [ ] **Step 1: Replace role definition in BuildSystemPrompt()**

Find the Chinese system prompt role definition. Replace:
```
你是一个专业的量化交易AI助手，负责分析市场数据并做出交易决策。
```
With:
```
你是一位管理美股映射永续合约投资组合的AI基金经理。你的目标是在控制风险的前提下，通过β暴露捕获市场涨幅，通过主动管理获取超额α。宏观判断优先，技术分析是执行参考，不是开仓主因。
```

Find the English system prompt role definition. Replace:
```
You are a professional quantitative trading AI assistant responsible for analyzing market data and making trading decisions.
```
With:
```
You are an AI fund manager operating a portfolio of tokenized US stock perpetual contracts. Your objective is to capture market beta while generating alpha through active management. Macro thesis takes priority; technical analysis informs execution timing, not primary entry decisions.
```

- [ ] **Step 2: Add intent_type to the decision format example**

In the JSON output format section (both Chinese and English), add `"intent_type"` as an optional field to the example:

```json
{
  "symbol": "NVDAUSDT",
  "action": "open_long",
  "leverage": 5,
  "position_size_usd": 500,
  "stop_loss": 850,
  "take_profit": 1050,
  "confidence": 78,
  "intent_type": "tactical_alpha",
  "entry_thesis": "Semiconductor capex cycle intact; NVDA testing support after 8% pullback",
  "reasoning": "..."
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./kernel/...
```
Expected: no output.

- [ ] **Step 4: Full project build check**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./...
```
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add kernel/prompt_builder.go
git commit -m "feat(prompt): update fallback prompt builder with fund manager framing"
```

---

## Task 12: Final Integration Verification + Push

- [ ] **Step 1: Full build**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go build ./...
```
Expected: no output (zero compile errors).

- [ ] **Step 2: Run existing tests**

```bash
cd /Users/xiagao/Desktop/nofx-binance-stock-main
go test ./... 2>&1 | head -60
```
Expected: any existing tests pass; no new failures introduced.

- [ ] **Step 3: Verify macro_thesis table migration works**

The table will be auto-migrated on first run. Verify the SQL is clean by inspecting the struct tags in `store/macro_thesis.go` — all fields must have valid GORM tags.

- [ ] **Step 4: Verify new intent_type / entry_thesis columns in position**

GORM AutoMigrate in `store/position.go` `InitTables()` will add the two new columns to `trader_positions` on first startup. Confirm `InitTables()` calls `db.AutoMigrate(&TraderPosition{})`.

- [ ] **Step 5: Git push**

```bash
git push origin main
```

---

## Self-Review Checklist

### Spec Coverage
- [x] Stateful macro thesis (persistent across cycles) — Tasks 2, 4, 7
- [x] Portfolio-level exposure (category breakdown, net direction) — Tasks 1, 5, 7
- [x] Session-based risk scaling (not hard block) — Tasks 1, 8
- [x] Configurable drawdown thresholds (stock-optimized 3%/25%) — Tasks 1, 8
- [x] Symbol category system (semiconductor, index, commodity, etc.) — Tasks 1, 8
- [x] Category concentration limit (max N same-category+direction) — Task 8
- [x] Position intent tagging (core_beta/tactical_alpha/hedge) — Tasks 3, 9
- [x] User-pushed macro report injection — Tasks 6, 7
- [x] Fund manager system prompt (macro-first, portfolio-level) — Tasks 10, 11
- [x] AI decision → macro thesis update loop (AI proposes → stored → injected next cycle) — Tasks 5, 7
- [x] Mixed mode (AI auto-updates thesis + user can push manual report) — Tasks 6, 7

### Type Consistency
- `MacroThesisUpdate` defined in `kernel/engine.go` → used in `Decision` struct → persisted via `store.MacroThesis` in `auto_trader_loop.go` ✓
- `PortfolioExposure` defined in `kernel/engine.go` → populated in `auto_trader_loop.go` → formatted in `engine_prompt.go` ✓
- `MacroThesisContext` defined in `kernel/engine.go` → loaded from `store.MacroThesis` in `auto_trader_loop.go` → formatted in `engine_prompt.go` ✓
- `enforceMaxPositions(positions, symbol, side)` — signature updated in `auto_trader_risk.go`; callers updated in `auto_trader_orders.go` ✓
- `store.EncodeSectorBias` / `store.EncodeKeyRisks` defined in `store/macro_thesis.go` → called in `auto_trader_loop.go` ✓
