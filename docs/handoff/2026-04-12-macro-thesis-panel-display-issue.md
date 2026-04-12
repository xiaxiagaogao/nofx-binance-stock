# MacroThesisPanel Display Issue Handoff

**Date**: 2026-04-12  
**Branch**: `main`  
**Context**: Fund-manager layer frontend verification on EC2

---

## Summary

The fund-manager data pipeline is now working end-to-end:

- backend compiles
- frontend builds
- `/api/macro-thesis` returns data
- `/api/portfolio-exposure` returns data
- backend logs confirm macro thesis generation

However, the **frontend presentation of `MacroThesisPanel` is currently poor and not product-grade**.

This is a **display / UI-expression issue**, not a core data-chain failure.

---

## What Was Observed

On the dashboard, the panel currently renders content similar to:

- `fundManager.macroThesis`
- `0.1h · ai`
- `fundManager.regime`
- `risk_off`
- `fundManager.thesis`
- full thesis paragraph text
- `fundManager.intent`
- `preserve_cash`
- `fundManager.sectorBias`
- `commodities: neutral`
- `index: cautious`
- `semiconductor: neutral`
- `fundManager.keyRisks`
- `low liquidity traps`
- `overnight gaps`

---

## Problem Statement

### 1. Translation keys are leaking into the UI
Parts of the panel still display raw i18n keys directly instead of final user-facing labels, for example:

- `fundManager.macroThesis`
- `fundManager.regime`
- `fundManager.thesis`
- `fundManager.intent`
- `fundManager.sectorBias`
- `fundManager.keyRisks`

This means the data is present, but the display layer is not resolving all labels cleanly.

### 2. The panel is rendering too much raw information at once
The current UI presentation feels like direct object dump / low-level debug output rather than a refined dashboard module.

Observed characteristics:
- thesis text is shown in a long full paragraph
- sector bias is listed item by item
- risk items are listed directly
- label/content hierarchy is weak
- visual density is high
- the module demands too much attention relative to the rest of the dashboard

### 3. It visually clashes with the main dashboard layout
The current block feels heavy and crowded when placed alongside:
- account summary
- chart area
- positions
- decisions
- history

The problem is not that the module exists, but that its current expression is too raw for the surrounding page.

---

## Important Clarification

This is **not** a backend-chain failure.

The underlying feature appears to be functioning:
- macro thesis generation is happening
- thesis is being returned by API
- fund-manager layer is alive

The issue is specifically that the current `MacroThesisPanel` presentation is still too close to internal/debug structure and not yet aligned with dashboard readability.

---

## Current Conclusion

The current state of `MacroThesisPanel` should be understood as:

- **feature works**
- **data exists**
- **presentation is not yet acceptable**

More specifically, the visible issues are:
- label/i18n leakage
- overly dense full-text output
- weak visual hierarchy
- poor fit with the rest of dashboard composition

---

## Verification Basis

This conclusion is based on:
- live dashboard inspection on EC2 preview/runtime
- successful API responses from `macro-thesis` / `portfolio-exposure`
- backend runtime logs showing macro thesis update events

So the next work here is purely on **frontend display quality and presentation structure**, not on re-debugging the fund-manager data path itself.
