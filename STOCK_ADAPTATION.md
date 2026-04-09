# Tokenized Stock Adaptation — Developer Change Log

This document describes all modifications made on top of the upstream [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx) project to adapt the platform for trading **Binance tokenized stocks** (TSLA, NVDA, XAU, QQQ, SPY, etc.) as first-class assets alongside — or instead of — crypto.

> Commit: `de66e5a feat: adapt platform for tokenized stock trading on Binance`
> Base commit (original code snapshot): `8b66eb3`

---

## Design Philosophy

The original NOFX platform was built around a **BTC/ETH vs. Altcoin two-tier risk model**, with dynamic coin selection from OI/funding-rate ranked lists.

This fork reorients the platform toward **quality, low-volatility assets** (tokenized US equities and commodities) with:

- A **static, manually curated watchlist** (no dynamic coin ranking needed)
- A **unified single-tier risk model** (no BTC/ETH vs. altcoin split)
- **US market session awareness** (pre-market / open / after-hours / closed)
- Conservative defaults appropriate for equities (5x leverage, 60% margin cap, 2.0 min risk/reward)

---

## Changed Files Summary

| File | Type | Summary |
|------|------|---------|
| `store/strategy.go` | Backend | Unified risk fields + backward compat helpers + new defaults |
| `kernel/engine.go` | Backend | Unified leverage context + trading session detection |
| `kernel/engine_analysis.go` | Backend | Simplified validation call signature |
| `kernel/engine_position.go` | Backend | Removed BTC/ETH branching, uniform position rules |
| `kernel/engine_prompt.go` | Backend | Unified risk section + SPY snapshot + session-aware guidance |
| `kernel/validate_test.go` | Test | Updated to new 4-param signature + stock symbols |
| `trader/auto_trader_loop.go` | Backend | Session detection injection into context |
| `trader/auto_trader_risk.go` | Backend | Removed `isBTCETH()`, unified ratio enforcement |
| `api/handler_exchange.go` | Backend | Single Binance entry in supported exchanges list |
| `api/handler_user.go` | Backend | Onboarding presets use unified fields |
| `api/server.go` | Backend | Default system config uses `max_leverage` |
| `api/strategy.go` | Backend | Preview response uses `EffectiveMaxLeverage()` |
| `web/src/types/strategy.ts` | Frontend | Added unified fields to `RiskControlConfig` interface |
| `web/src/components/strategy/RiskControlEditor.tsx` | Frontend | Replaced dual sliders with single unified sliders |

---

## 1. Risk Model Unification (`store/strategy.go`)

### Problem
The original model had four separate risk fields:
```go
BTCETHMaxLeverage          int
AltcoinMaxLeverage         int
BTCETHMaxPositionValueRatio float64
AltcoinMaxPositionValueRatio float64
```
Tokenized stocks don't fit either category. Forcing QQQ into the "altcoin" bucket is semantically wrong and would inherit inappropriate risk defaults.

### Change
Added two new unified fields to `RiskControlConfig`:
```go
MaxLeverage          int     `json:"max_leverage,omitempty"`
MaxPositionValueRatio float64 `json:"max_position_value_ratio,omitempty"`
```

Old fields are kept with `omitempty` (backward compatible — existing saved strategies continue to work).

Added two helper methods for all callers to use instead of direct field access:
```go
func (r RiskControlConfig) EffectiveMaxLeverage() int
func (r RiskControlConfig) EffectiveMaxPositionValueRatio() float64
```

**Fallback priority** for `EffectiveMaxLeverage()`:
1. `MaxLeverage` if > 0
2. `max(BTCETHMaxLeverage, AltcoinMaxLeverage)` if either > 0
3. Default: `5`

### New Defaults (`GetDefaultStrategyConfig()`)
| Field | Old Value | New Value |
|-------|-----------|-----------|
| Coin source | AI-ranked dynamic list | Static: `TSLAUSDT, NVDAUSDT, XAUUSDT, QQQUSDT, SPYUSDT` |
| Timeframes | `1h, 4h` | `1h, 4h, 1d` |
| Max positions | 3 | 2 |
| MaxLeverage | — (was split) | 5 |
| MaxPositionValueRatio | — (was split) | 0.5 |
| MaxMarginUsage | 0.8 | 0.6 |
| MinConfidence | 75 | 80 |
| MinRiskRewardRatio | 3.0 | 2.0 |
| OI filter | enabled | disabled |
| Funding rate filter | enabled | disabled |
| Quant data | enabled | disabled |

---

## 2. Trading Session Detection (`kernel/engine.go`)

### Added Fields to `Context` struct
```go
// Before
BTCETHLeverage int
AltcoinLeverage int

// After
MaxLeverage    int
TradingSession string  // NEW
```

### Added Functions
```go
// Returns one of: "us_market_open", "us_pre_market", "us_after_hours", "us_market_closed"
func GetUSTradingSession(utcNow time.Time) string

func TradingSessionLabel(session string) string    // English label
func TradingSessionLabelZh(session string) string  // Chinese label
```

**Session boundaries** (America/New_York):
| Session | Hours (ET) | Notes |
|---------|-----------|-------|
| `us_pre_market` | 04:00–09:30 | Reduced liquidity |
| `us_market_open` | 09:30–16:00 | Full liquidity |
| `us_after_hours` | 16:00–20:00 | Reduced liquidity |
| `us_market_closed` | 20:00–04:00 + weekends | Avoid new positions |

Timezone loading uses `time.LoadLocation("America/New_York")` with UTC-4 fallback.

---

## 3. Position Validation Simplification (`kernel/engine_position.go`)

### Function Signature Change
```go
// Before (6 params, asset-class branching)
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLev int, altLev int, btcEthRatio float64, altRatio float64) error

// After (4 params, unified)
func validateDecisions(decisions []Decision, accountEquity float64, maxLeverage int, maxPosRatio float64) error
```

Same change applies to `validateDecision()`.

### Behavioral Changes
- Removed `isBTCETH()` classification logic entirely
- Removed BTC/ETH-specific minimum position size ($60 USDT) — now uniform $12 USDT for all assets
- `MinRiskRewardRatio` default: `3.0` → `2.0` (appropriate for equity-like instruments)

---

## 4. Auto-Trader Loop (`trader/auto_trader_loop.go`)

### Leverage Retrieval
```go
// Before
btcEthLeverage := strategyConfig.RiskControl.BTCETHMaxLeverage
altcoinLeverage := strategyConfig.RiskControl.AltcoinMaxLeverage

// After
maxLeverage := strategyConfig.RiskControl.EffectiveMaxLeverage()
```

### Session Injection
```go
now := time.Now().UTC()
tradingSession := kernel.GetUSTradingSession(now)
logger.Infof("📅 [%s] Trading session: %s", at.name, kernel.TradingSessionLabel(tradingSession))

ctx := kernel.Context{
    MaxLeverage:    maxLeverage,
    TradingSession: tradingSession,
    // ...
}
```

---

## 5. Auto-Trader Risk Enforcement (`trader/auto_trader_risk.go`)

Removed `isBTCETH()` function. `enforcePositionValueRatio()` now calls:
```go
maxRatio := riskControl.EffectiveMaxPositionValueRatio()
```
No more asset-class branching. All positions use the same ratio limit.

---

## 6. Prompt Changes (`kernel/engine_prompt.go`)

### System Prompt
- Role description: `"cryptocurrency trading AI"` → `"quantitative trading AI for tokenized stocks and commodities on Binance Futures"`
- Risk parameters section: replaced dual BTC/ETH + Altcoin risk blocks with a single unified block
- JSON output example symbols: `BTCUSDT / ETHUSDT` → `TSLAUSDT / XAUUSDT`

### User Prompt
- Market snapshot section: "BTC市场快照" → "SPY市场快照" (SPY as macro reference)
- Added trading session context block with per-session risk guidance:
  - `us_market_open`: normal parameters apply
  - `us_pre_market` / `us_after_hours`: flag reduced liquidity, tighten stops
  - `us_market_closed`: strongly discourage new position opens
- Indicator description: "AI500 / OI_Top filter tags" → "Static asset watchlist (manually curated)"

> **Note**: These are *default* prompt changes only. Strategies that define their own `prompt_sections` in JSON (e.g., Strategy 4.4) override these defaults entirely at runtime. Prompt tuning per-strategy is deferred to P3.

---

## 7. API Layer (`api/`)

### `handler_exchange.go` — Supported Exchanges
```go
// Before: 11 exchanges (Hyperliquid, OKX, Bybit, etc.)
// After: Single entry
{ExchangeType: "binance", Name: "Binance Futures (Stocks & Crypto)", Type: "cex"}
```

### `server.go` — System Config Default
```go
// Before
"btc_eth_leverage": 10, "altcoin_leverage": 5

// After
"max_leverage": 5
```

### `handler_user.go` — Onboarding Presets
```go
// Conservative
MaxLeverage: 3, MaxPositionValueRatio: 0.5

// Aggressive
MaxLeverage: 5, MaxPositionValueRatio: 1.0
```

### `strategy.go` — Preview Response
```go
// config_summary now uses:
"max_leverage": req.Config.RiskControl.EffectiveMaxLeverage()
```

---

## 8. Frontend (`web/src/`)

### `types/strategy.ts`
Added to `RiskControlConfig` interface:
```typescript
max_leverage?: number;
max_position_value_ratio?: number;
```
Old `btc_eth_*` and `altcoin_*` fields made `optional` (backward compat for saved configs).

### `components/strategy/RiskControlEditor.tsx`
- Replaced two leverage sliders (BTC/ETH + Altcoin) with **one unified slider**
  - Binds to: `config.max_leverage ?? config.btc_eth_max_leverage ?? 5`
  - Range: 1–10x
- Replaced two position ratio sliders with **one unified slider**
  - Binds to: `config.max_position_value_ratio ?? config.altcoin_max_position_value_ratio ?? 0.5`
  - Range: 0.1–3.0

---

## Backward Compatibility

Existing strategy JSON configs that use old fields (`btc_eth_max_leverage`, `altcoin_max_leverage`, etc.) **continue to work without modification**. The `EffectiveMaxLeverage()` / `EffectiveMaxPositionValueRatio()` helpers read old fields as fallback.

Strategy 4.4 (in `/strategy-44-backup/`) was the primary test case — it uses old field names and is fully compatible without any config migration.

---

## What Was NOT Changed (Deferred)

| Area | Reason |
|------|--------|
| Per-strategy prompt tuning (P3) | Requires live trading feedback first |
| Correlation constraints between positions | Handled via position sizing (QQQ as macro hedge) |
| OI/liquidity thresholds per asset | To be tuned via backtesting |
| Non-Binance exchange support | Out of scope for this fork |
