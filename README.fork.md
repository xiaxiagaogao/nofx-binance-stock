# nofx-binance-stock — Fork README

This is a **private fork** of [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx), stripped down and adapted for trading **Binance tokenized stocks** (TSLA, NVDA, XAU, QQQ, SPY) using the original AI agent chain.

> For a line-by-line change log of every modification from upstream, see [`STOCK_ADAPTATION.md`](./STOCK_ADAPTATION.md).

---

## What This Fork Is

The upstream project is a general-purpose AI trading platform supporting 10+ exchanges and dynamic crypto coin selection. This fork:

- **Removes** all non-Binance exchange adapters (Bybit, OKX, Bitget, Gate, KuCoin, Indodax, Hyperliquid, Aster, Lighter)
- **Removes** crypto-specific data providers (CoinAnk, Hyperliquid provider, Alpaca, TwelveData)
- **Removes** dynamic coin selection (AI500 ranking, OI Top/Low, NetFlow, funding rate filters)
- **Removes** claw402/x402 payment gateway and USDC wallet
- **Keeps** the full AI agent chain: analyze → decide → order → close
- **Keeps** grid trading module (deferred decision)
- **Adds** US market session awareness (pre-market / open / after-hours / closed)
- **Adds** unified single-tier risk model (replaces BTC/ETH vs. Altcoin split)

The trading philosophy: tokenized US stocks and commodities on Binance are **quality, moderate-volatility assets** — not the "wild dog" behavior of crypto. The goal is stable beta returns with alpha pursuit, using moderate leverage and disciplined position sizing.

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.25.3, Gin HTTP framework |
| Frontend | React 18, TypeScript, Vite |
| Database | SQLite (via GORM) |
| AI | Any OpenAI-compatible API (Claude, DeepSeek, GPT-4, etc.) |
| Exchange | Binance Futures only (`fapi.binance.com`) |
| Deployment | Docker + docker-compose |

---

## Project Structure

```
.
├── main.go                    # Entry point
├── api/                       # HTTP handlers (Gin routes)
│   ├── server.go              # Route registration, middleware
│   ├── handler_trader.go      # Trader CRUD
│   ├── handler_exchange.go    # Exchange account management
│   ├── handler_klines.go      # K-line data endpoint (Binance native)
│   ├── handler_onboarding.go  # First-run setup wizard
│   └── strategy.go            # Strategy preview/test
├── kernel/                    # AI decision engine
│   ├── engine.go              # Context struct, trading session detection
│   ├── engine_analysis.go     # Parse AI response → Decision[]
│   ├── engine_position.go     # Decision validation (unified risk)
│   ├── engine_prompt.go       # System + user prompt construction
│   └── grid_engine.go         # Grid trading AI (kept, see note)
├── trader/
│   ├── binance/               # Binance Futures implementation
│   ├── auto_trader.go         # AutoTrader struct, lifecycle
│   ├── auto_trader_loop.go    # Main trading loop, session injection
│   ├── auto_trader_risk.go    # Position size enforcement, drawdown monitor
│   ├── auto_trader_decision.go# Decision → order execution
│   └── auto_trader_grid.go    # Grid trading state machine (kept)
├── store/                     # Data persistence (SQLite/GORM)
│   ├── strategy.go            # StrategyConfig struct + defaults
│   └── exchange.go            # Exchange credentials storage
├── market/                    # Market data utilities
│   ├── api_client.go          # Binance Futures klines client
│   ├── data.go                # Market data aggregation
│   └── data_klines.go         # Kline fetch + indicator calculation
├── provider/
│   └── nofxos/                # NofxOS quant data API (coin-level data)
├── manager/                   # Trader lifecycle manager
├── telegram/                  # Telegram bot integration
└── web/                       # React frontend
    └── src/
        ├── types/strategy.ts  # RiskControlConfig interface
        └── components/strategy/
            └── RiskControlEditor.tsx  # Unified leverage/ratio sliders
```

---

## Key Concepts

### Strategy Config (`store/strategy.go`)

All trading behavior is controlled by `StrategyConfig` stored as JSON in the database. The config is loaded per-trader at runtime.

**Default coin list** (static, manually curated):
```
TSLAUSDT, NVDAUSDT, XAUUSDT, QQQUSDT, SPYUSDT
```

**Risk control** — unified single-tier (no BTC/ETH vs. altcoin split):
```go
type RiskControlConfig struct {
    MaxLeverage          int     `json:"max_leverage"`           // Primary field
    MaxPositionValueRatio float64 `json:"max_position_value_ratio"` // Primary field

    // Deprecated — kept for backward compat with old saved strategies
    BTCETHMaxLeverage           int     `json:"btc_eth_max_leverage,omitempty"`
    AltcoinMaxLeverage          int     `json:"altcoin_max_leverage,omitempty"`
    BTCETHMaxPositionValueRatio  float64 `json:"btc_eth_max_position_value_ratio,omitempty"`
    AltcoinMaxPositionValueRatio float64 `json:"altcoin_max_position_value_ratio,omitempty"`
}
```

Always use the helper methods — they handle the fallback automatically:
```go
riskConfig.EffectiveMaxLeverage()          // new field → old fields → default 5
riskConfig.EffectiveMaxPositionValueRatio() // new field → old fields → default 2.0
```

**Strategy 4.4** (production strategy in `/strategy-44-backup/`) uses old field names — fully backward compatible without migration.

---

### Trading Session Detection (`kernel/engine.go`)

Injected into every AI decision cycle via `Context.TradingSession`:

```go
func GetUSTradingSession(utcNow time.Time) string
// Returns: "us_market_open" | "us_pre_market" | "us_after_hours" | "us_market_closed"
```

| Session | ET Hours | AI Guidance |
|---------|----------|-------------|
| `us_market_open` | 09:30–16:00 | Normal parameters |
| `us_pre_market` | 04:00–09:30 | Reduced liquidity warning |
| `us_after_hours` | 16:00–20:00 | Reduced liquidity warning |
| `us_market_closed` | 20:00–04:00 + weekends | Avoid new positions |

Timezone: `America/New_York` with UTC-4 fallback.

---

### AI Agent Loop (`trader/auto_trader_loop.go`)

Every scan interval:
1. Fetch market data for each coin in the static list
2. Build `kernel.Context` (klines, indicators, session, equity, positions)
3. Send to AI model via MCP
4. Parse response → `[]Decision`
5. Validate decisions (`kernel/engine_position.go`)
6. Execute orders (`trader/auto_trader_decision.go`)
7. Risk enforcement: position size cap, drawdown monitor

---

### Kline Data Source

**Before (upstream)**: CoinAnk third-party API  
**After (this fork)**: Native Binance Futures API

```go
// market/api_client.go
func (c *APIClient) GetKlines(symbol, interval string, limit int) ([]Kline, error)
// → GET https://fapi.binance.com/fapi/v1/klines
```

---

## Active Strategy: 4.4

Located in `/strategy-44-backup/strategy_44_backup.json`. Key params:

| Param | Value |
|-------|-------|
| Coins | TSLA, NVDA, XAU, QQQ, SPY, AAPL |
| Timeframes | 1h (50 bars), 4h (20 bars), 1d |
| Indicators | EMA 21/55, RSI 14, BOLL 20, Volume |
| Max positions | 2 |
| Leverage | 5x (via `btc_eth_max_leverage` for compat) |
| Min confidence | 80% |
| Min R:R ratio | 2.0 |
| Margin cap | 60% |

Strategy 4.4 defines its own `prompt_sections` in JSON, which override all default prompts at runtime. Changes to default prompts in `engine_prompt.go` do not affect live trading with this strategy.

---

## Deployment

### Prerequisites

- Docker + docker-compose
- Binance Futures API key (with futures trading permissions)
- AI model API key (OpenAI-compatible)

### Environment Variables (`.env`)

```env
# AI model
AI_MODEL_API_KEY=your_key
AI_MODEL_BASE_URL=https://api.openai.com/v1   # or any compatible endpoint

# Database
DB_PATH=./data/nofx.db

# Security
ENCRYPTION_KEY=your_32_byte_key

# Optional: Telegram notifications
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
```

### Start

```bash
git clone https://github.com/xiaxiagaogao/nofx-binance-stock.git
cd nofx-binance-stock
cp .env.example .env  # edit with your keys
docker-compose up -d
```

Frontend: `http://localhost:3000`  
Backend API: `http://localhost:8080`

### Update (EC2)

```bash
git pull origin main
docker-compose down && docker-compose up -d --build
```

---

## CI/CD

GitHub Actions (`.github/workflows/`) runs on every push to `main`:
1. Build frontend Docker image (amd64 + arm64)
2. Build backend Docker image (amd64 + arm64)
3. Push to GitHub Container Registry (`ghcr.io/xiaxiagaogao/nofx-binance-stock`)

If backend build fails, check Go compilation errors — most common cause is a deleted package still referenced in an import.

---

## What Has NOT Been Changed (Deferred)

| Area | Status |
|------|--------|
| Per-strategy prompt tuning | P3 — needs live trading data first |
| Grid trading module | Kept, decision deferred |
| Correlation constraints between positions | Handled via QQQ as macro hedge in position sizing |
| OI / liquidity thresholds per asset | To be tuned via backtesting |
| Risk params fine-tuning | To be tuned via backtesting |

---

## Files Worth Reading First

If you're new to this codebase, start here:

1. `STOCK_ADAPTATION.md` — every change from upstream, file by file
2. `store/strategy.go` — the central config data model
3. `kernel/engine.go` — the Context struct and session detection
4. `kernel/engine_prompt.go` — what the AI actually sees
5. `trader/auto_trader_loop.go` — the main trading loop
6. `strategy-44-backup/strategy_44_backup.json` — the live strategy config
