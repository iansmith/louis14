# CONTINUE: Phase 16.e + 18 bundled — MulticolPartWalker + MulticolBreakTokenData

## Session summary

Phase 17 (forced-break balance ContentRuns) is **COMPLETE** and committed.

**Gate at close:** CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol **196/455** · spanner-frag 11/13.

**Tests gained in Phase 17:** `multicol-fill-balance-040/041`, `multicol-nested-column-rule-003`, `spanner-in-child-after-parallel-flow-004`.

## Next phase

**Phase 16.e + 18 bundled — MulticolPartWalker port + MulticolBreakTokenData carrier.**

This is a HIGH-RISK, multi-commit refactor. **DO IN A WORKTREE.**

### Why bundled

Phase 16.e (Blink's `MulticolPartWalker`) and Phase 18 (`MulticolBreakTokenData` row-carry) both require changing the shape of `BlockBreakToken` for multicol children:
- 16.e flattens the positional encoding: `ChildBreakTokens` dispatched by `Node` instead of a position-in-child-list integer index
- 18 adds a polymorphic data field to `BlockBreakToken` carrying `MulticolBreakTokenData{consumed_row_block_size, row_break_token}` 

Doing them separately means touching the same 6 entangled sites twice. Bundle them.

### What this unlocks

Phase 16.c.2 retry #3 becomes mechanical once the walker lands (delete `ClipBlockAxisOnly` everywhere — ~3-4 tests).

Phase 18's row-carry enables nested multicol where content wraps across outer fragmentainer boundaries (~15 tests).

### Blink citations

**MulticolPartWalker** (`column_layout_algorithm.cc:1256-1390`):
- Flat `ChildBreakTokens` dispatched by `Node` pointer identity
- `MulticolPartWalker::IsAt(node)` / `AdvanceTo(node)` / `Next()`
- Replaces our current position-index approach in `findChildBreakToken`

**MulticolBreakTokenData** (`multicol_break_token_data.h`):
- `struct MulticolBreakTokenData { LayoutUnit consumed_row_block_size; BlockBreakToken* row_break_token; }`
- Stored on `BlockBreakToken::multicol_data_` (optional, nullptr for non-multicol)
- Used in `cla.cc:2087-2093` to carry row position across outer fragmentainer boundaries

### Pre-work required

1. Write the "Phase 16.e + 18 bundled brief" in `findings.md` before starting (as noted in task_plan.md)
2. Study the 6 entangled sites in `column_layout_algorithm.cc` that the minimal-port attempt identified
3. Work in a worktree: `git worktree add ../phase-18-worktree fix/phase-18`

### Key files to study first

- `third_party/blink/renderer/core/layout/column_layout_algorithm.cc` lines 1256-1390 (MulticolPartWalker)
- `third_party/blink/renderer/core/layout/multicol_break_token_data.h`
- `pkg/layout/multicol_layout.go` (our current ChildBreakToken threading)
- `pkg/layout/break_token.go` (BlockBreakToken struct)
- `findings.md` § "Phase 16.e Blink-parity spanner-content fragmentation" (existing brief)
- `findings.md` § "Phase 18 brief — Nested multicol MulticolBreakTokenData row-carry"

### Gate targets after this phase

- multicol: **~215+/455** (+15-20 from row-carry + clip removal)
- spanner-frag: **~12+/13**

### Invariant driver tests (13/13 must pass at 0 diff throughout)

column-height-001/010/017/026/027, multicol-nested-030/031, spanner-fragmentation-001/004/006, multicol-rule-nested-balancing-004, nested-floated-multicol-with-monolithic-child, nested-past-fragmentation-line.

Run: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-multicol/(column-height-001|column-height-010|column-height-017|column-height-026|column-height-027|multicol-nested-030|multicol-nested-031|spanner-fragmentation-001|spanner-fragmentation-004|spanner-fragmentation-006|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)"`
