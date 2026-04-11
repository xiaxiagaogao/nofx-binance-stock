# Macro Reports

Place your macro analysis reports here as `latest.md`.

The trading engine reads `latest.md` at the start of each cycle and injects
it into the AI's context as a high-priority signal. Reports older than 48
hours are still used but flagged as stale.

## Format

Plain Markdown. Example:

```markdown
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

## Workflow

1. Generate the report externally (e.g. scheduled workflow, manual research)
2. Write / overwrite `macro_reports/latest.md`
3. The next trading cycle will automatically pick it up and inject it into the AI prompt

The AI fund manager uses this report as a **high-priority macro signal**, and combines it with its own persisted macro thesis stored in the database.
