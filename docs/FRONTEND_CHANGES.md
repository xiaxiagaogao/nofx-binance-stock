# Frontend Changes — Binance Stock Adaptation

This document describes all frontend modifications made to align with the Binance-only tokenized stock backend. For backend changes, see [`STOCK_ADAPTATION.md`](../STOCK_ADAPTATION.md).

---

## Deployment Note: Why UI Looks Unchanged After `git pull`

The frontend is compiled at Docker build time (`npm run build` → static files in nginx). Pulling new code does **not** automatically update what's running.

**Correct update sequence on EC2:**

```bash
git pull origin main
docker-compose build --no-cache nofx-frontend
docker-compose up -d
```

Or force full recreate:

```bash
git pull origin main && docker-compose up -d --build --force-recreate
```

Also clear browser cache (Cmd+Shift+R / Ctrl+Shift+R) after deploying — nginx serves static files that browsers aggressively cache.

---

## Changed Files

### 1. `web/src/components/trader/ExchangeConfigModal.tsx`

**Before**: Two-step flow — step 1 picks from a grid of 10 exchange types (7 CEX + 3 DEX), step 2 shows exchange-specific credential form with different fields per exchange.

**After**: Single-step form showing Binance credentials only.

**Removed:**
- `SUPPORTED_EXCHANGE_TEMPLATES` array (10 entries → 1)
- Step indicator / step navigation UI
- Exchange type picker grid (CEX section + DEX section)
- `ExchangeCard` sub-component
- `StepIndicator` sub-component
- All DEX-specific form fields:
  - Passphrase (OKX / Bitget / KuCoin)
  - Hyperliquid wallet address
  - Aster user / signer / private key
  - Lighter wallet address / API key private key / API key index
- Testnet toggle (Hyperliquid-specific)
- `TwoStageKeyModal` import
- Per-exchange state variables: `passphrase`, `testnet`, `asterUser`, `asterSigner`, `asterPrivateKey`, `hyperliquidWalletAddr`, `lighterWalletAddr`, `lighterApiKeyPrivateKey`, `lighterApiKeyIndex`

**Kept:**
- Exchange name input
- API Key input
- Secret Key input
- Enabled toggle
- `WebCryptoEnvironmentCheck` (security environment check)

**`onSave` callback signature**: Unchanged for parent compatibility. The exchange type is now hardcoded to `'binance'` in `handleSubmit`.

---

### 2. `web/src/components/strategy/CoinSourceEditor.tsx`

**Before**: Tab picker with 4 modes — Static / AI500 / OI Top / OI Low (and a Mixed mode combining them). Each mode had its own configuration section with limits, filters, and NofxOS API toggles.

**After**: Always shows static coin list only. No tabs, no toggles.

**Removed:**
- `sourceTypes` array (4 entries: static / ai500 / oi_top / oi_low)
- `getMixedSummary()` function
- `NofxOSBadge` component
- Tab/picker grid UI
- AI500 configuration section (limit input, NofxOS badge)
- OI Top configuration section (limit, duration, type filters)
- OI Low configuration section
- Mixed mode configuration section
- All conditional rendering guards (`config.source_type === 'ai500'` etc.)
- Unused imports: `Database`, `TrendingUp`, `TrendingDown`, `List`, `Zap`, `Shuffle`, `NofxSelect`

**Kept:**
- Static coin list: add/remove individual coin inputs
- Excluded coins list: add/remove exclusion inputs
- All `onChange` calls now always include `source_type: 'static'`

**Placeholder text updated:**
| Location | Before | After |
|----------|--------|-------|
| Static coins input | `BTC, ETH, SOL...` | `TSLA, NVDA, XAU, QQQ, SPY...` |
| Excluded coins input | `BTC, ETH, DOGE...` | `TSLA, NVDA, XAU...` |

---

### 3. `web/src/components/strategy/RiskControlEditor.tsx`

*(Changed in an earlier pass — documented here for completeness)*

**Before**: Two leverage sliders (BTC/ETH leverage + Altcoin leverage) and two position ratio sliders.

**After**: One unified leverage slider, one unified position ratio slider.

**Field bindings:**

| Control | Reads from | Writes to |
|---------|-----------|-----------|
| Leverage slider | `config.max_leverage ?? config.btc_eth_max_leverage ?? 5` | `max_leverage` |
| Position ratio slider | `config.max_position_value_ratio ?? config.altcoin_max_position_value_ratio ?? 0.5` | `max_position_value_ratio` |

The fallback to deprecated fields ensures old saved strategies (e.g., Strategy 4.4 using `btc_eth_max_leverage`) display correctly without migration.

---

### 4. `web/src/components/strategy/GridConfigEditor.tsx`

**Before**: Default symbol `BTCUSDT`, dropdown options: BTCUSDT, ETHUSDT, SOLUSDT, BNBUSDT, XRPUSDT, DOGEUSDT.

**After**: Default symbol `TSLAUSDT`, dropdown options: TSLAUSDT, NVDAUSDT, XAUUSDT, QQQUSDT, SPYUSDT.

Only the `defaultGridConfig.symbol` and the symbol dropdown array were changed. All grid logic, parameters, and styling are untouched.

---

### 5. `web/src/components/strategy/PromptSectionsEditor.tsx`

**Before**: Default `role_definition` prompt starts with `# 你是专业的加密货币交易AI`

**After**: `# 你是专业的量化交易AI`

One line change. Only affects the default template shown to new users — existing strategies that have saved custom prompts are unaffected.

---

### 6. `web/src/pages/StrategyMarketPage.tsx`

**Before**: Strategy card displays leverage from deprecated field:
```tsx
strategy.config.risk_control.btc_eth_max_leverage
```

**After**: Reads unified field with backward-compat fallback:
```tsx
(strategy.config.risk_control.max_leverage ?? strategy.config.risk_control.btc_eth_max_leverage)
```

---

### 7. `web/src/pages/AITradersPage.tsx`

**Before**: Two separate error cases for leverage validation:
```
trader.create.invalid_btc_eth_leverage  →  "BTC/ETH 杠杆倍数需要在 1 到 50 倍之间"
trader.create.invalid_altcoin_leverage  →  "山寨币杠杆倍数需要在 1 到 20 倍之间"
```

**After**: Both cases fall through to one unified message:
```
"杠杆倍数需要在 1 到 10 倍之间。" / "Leverage must be between 1x and 10x."
```

---

## TypeScript Types (`web/src/types/strategy.ts`)

*(Changed in earlier pass — documented here for completeness)*

Added to `RiskControlConfig` interface:
```typescript
max_leverage?: number;               // New unified field
max_position_value_ratio?: number;   // New unified field
```

Existing deprecated fields made optional (not removed — needed for reading old saved configs):
```typescript
btc_eth_max_leverage?: number;
altcoin_max_leverage?: number;
btc_eth_max_position_value_ratio?: number;
altcoin_max_position_value_ratio?: number;
```

---

## What Was NOT Changed

| Component | Reason |
|-----------|--------|
| `web/src/i18n/strategy-translations.ts` | Deprecated `btcEthLeverage` / `altcoinLeverage` keys kept — removing would break any component still reading them during the transition |
| `web/src/components/strategy/PromptSectionsEditor.tsx` (most of it) | Only the default role string changed; all prompt section structure and editing UI kept |
| Grid trading UI (`GridConfigEditor.tsx`, `GridRiskPanel.tsx`) | Grid trading module kept (deferred decision) |
| Telegram / notification UI | Unrelated to exchange adaptation |
