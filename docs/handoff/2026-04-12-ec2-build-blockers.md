# EC2 Build Blockers Handoff

**Date**: 2026-04-12
**Checked on**: EC2 host (`/root/stacks/nofx-stock`)
**Checked branch**: `main`
**Checked commit**: `f87cdbc`

---

## Summary

After pulling the latest main branch on EC2, I verified local buildability.

Result:
- **Go toolchain issue resolved**: Go was not installed on the EC2 host; installed **Go 1.25.3** to match `go.mod`
- **Backend still fails to compile** due to code-level references in `kernel/engine.go`
- **Frontend still fails to build** due to a few TypeScript cleanup leftovers

This document records the exact blockers so the next developer can fix them with minimal context loss.

---

## 1. Environment Note

### Original issue
The EC2 host initially had **no `go` command** available.

Confirmed before install:
```bash
which go
# (no output)

go version
# /usr/bin/bash: go: command not found
```

### Resolution already applied on host
Installed official **Go 1.25.3** to match `go.mod`:

```bash
go version
# go version go1.25.3 linux/amd64
```

So the current backend compile failure is **not** caused by missing Go anymore.

---

## 2. Backend Build Blocker

### Command used
```bash
cd /root/stacks/nofx-stock
go build ./...
```

### Result
Compilation fails in `kernel/engine.go`:

```go
# nofx/kernel
kernel/engine.go:106:29: undefined: nofxos.OIRankingData
kernel/engine.go:107:29: undefined: nofxos.NetFlowRankingData
kernel/engine.go:259:32: undefined: nofxos.NewClaw402DataClient
kernel/engine.go:261:11: client.SetClaw402 undefined (type *nofxos.Client has no field or method SetClaw402)
kernel/engine.go:489:33: e.nofxosClient.GetTopRatedCoins undefined (type *nofxos.Client has no field or method GetTopRatedCoins)
kernel/engine.go:509:35: e.nofxosClient.GetOITopPositions undefined (type *nofxos.Client has no field or method GetOITopPositions)
kernel/engine.go:533:35: e.nofxosClient.GetOILowPositions undefined (type *nofxos.Client has no field or method GetOILowPositions)
kernel/engine.go:735:55: undefined: nofxos.OIRankingData
kernel/engine.go:753:30: e.nofxosClient.GetOIRanking undefined (type *nofxos.Client has no field or method GetOIRanking)
kernel/engine.go:766:60: undefined: nofxos.NetFlowRankingData
kernel/engine.go:753:30: too many errors
```

### Current interpretation
This looks like `kernel/engine.go` still references legacy `nofxos / ranking / claw402` interfaces that are no longer present in the current fork.

The failure appears to be a **code integration mismatch**, not an environment problem.

### Recommended next check
Please verify one of these is true:
1. A required follow-up commit was not pushed yet
2. `kernel/engine.go` still contains stale references that should have been removed/refactored
3. The `nofxos` client abstraction changed, but `kernel/engine.go` was not updated accordingly

---

## 3. Backend Command Note

One command suggestion previously given was:

```bash
go build -o nofx ./...
```

That command is invalid for multi-package builds because `./...` expands to multiple packages and cannot be emitted into a single output file.

### Correct usage
To validate the whole repo:
```bash
go build ./...
```

To build the main binary only:
```bash
go build -o nofx .
```

In the current state, the repo already fails at `go build ./...`, so `go build -o nofx .` was not attempted beyond that stage.

---

## 4. Frontend Build Blockers

### Commands used
```bash
cd /root/stacks/nofx-stock/web
npm install
npm run build
```

### Result
`npm install` succeeds, but `npm run build` fails with these TypeScript errors:

```ts
src/components/charts/ComparisonChart.tsx(16,15): error TS2305: Module '"../../types"' has no exported member 'CompetitionTraderData'.

src/components/trader/AITradersPage.tsx(759,9): error TS6133: 'hasStrategies' is declared but its value is never read.
src/components/trader/AITradersPage.tsx(760,9): error TS6133: 'hasCreatedTrader' is declared but its value is never read.
src/components/trader/AITradersPage.tsx(761,9): error TS6133: 'canCreateTrader' is declared but its value is never read.

src/components/trader/ModelConfigModal.tsx(6,1): error TS6133: 'api' is declared but its value is never read.
```

### Current interpretation
These look like **small cleanup leftovers**, likely caused by earlier removal of competition-related UI and related simplification.

They do **not** currently look like fund-manager-layer logic bugs.

---

## 5. High-Level Status

### What is already confirmed
- Latest code was successfully pulled to EC2
- Host now has the correct Go version installed
- The current blockers are reproducible locally on EC2

### What is blocked right now
- Backend local compile
- Frontend production build
- Therefore the usual deployment refresh flow cannot yet complete cleanly

---

## 6. Suggested Next Actions

### Priority 1 — backend
Fix `kernel/engine.go` references to removed/missing `nofxos` ranking / claw402 APIs so that:

```bash
go build ./...
```

passes.

### Priority 2 — frontend
Fix the four TypeScript cleanup errors so that:

```bash
cd web
npm run build
```

passes.

### Priority 3 — deploy validation
After both are fixed:

```bash
cd /root/stacks/nofx-stock
go build -o nofx .
cd web && npm install && npm run build
```

Then continue with the actual service refresh path.

---

## 7. Context Preservation

This handoff is intentionally narrow:
- It only records what was observed during EC2 validation
- It does **not** speculate on the intended final fix beyond the missing/stale reference hypothesis
- No code changes were made here to patch the backend logic

The goal is to give the next developer an exact reproduction point with minimal ambiguity.
