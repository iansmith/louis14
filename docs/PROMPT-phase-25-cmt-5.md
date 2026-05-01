# P25 Cmt-5 — investigation-driven plan (rewritten 2026-04-30)

You're continuing Phase 25 (fragmentation-aware OOF positioning, Blink-aligned port). Cmt-1 (`4540a8f0`), Cmt-2 (`3b6b0e5d`), Cmt-3a (`8a9226ef`), Cmt-3b (`7ed644d7`), Cmt-4 (`e84d3ece`) landed. Cmt-4 was gate-neutral. This prompt supersedes the original Cmt-5 plan with concrete fixes derived from a four-agent Blink + local code investigation.

## Setup

- **Worktree:** `/Users/iansmith/louis14-phase-25` on `phase-25-oof-fragmentation`. Currently at `e84d3ece` (Cmt-4). Fonts symlinked.
- **Read first:** `findings.md` § Phase 25 sequencing (Cmt-4 entry has the diagnostic table that motivates Cmt-5).
- **The full investigation transcript** is implicit in the four sub-fixes below. Each fix has cited Blink references.

## Why Cmt-4 alone didn't close targets

Phase 14b's defer at `multicol_layout.go:423-442` returns a 0-height fragment for inner multicols whose declared height exceeds the outer fragmentainer's remaining space (with `column-fill:auto`). For `-011` and `-032`, this fires immediately, the walker loop never runs, and Cmt-4's post-loop break guard never gets the chance to fire. Re-applying Cmt-A (narrowing Phase 14b to `FragmentainerOffset > 0`) lets `-011`/`-032` reach Cmt-3+Cmt-4, closing them to **1.0% (5,000 px)** but regressing `-033` by **200 px**.

The 1.0% residual on `-011`/`-032` and the 200 px on `-033` are NOT Phase 14b problems — they're four distinct upstream bugs. Cmt-5 fixes them in a four-step sequence (a, b, c, d), then narrows Phase 14b as Cmt-5e.

## Diagnostic data table (must hold throughout Cmt-5)

| State | -010 | -011 | -032 | -033 |
|---|---|---|---|---|
| Cmt-4 (current HEAD) | 0/0 ✓ | 10000 px (2.1%) | 10000 px (2.1%) | 0/0 ✓ |
| Cmt-4 + Cmt-A (Phase 14b → FragOff>0) | 0/0 ✓ | 5000 px (1.0%) | 5000 px (1.0%) | 200 px |
| Cmt-4 + Phase 14b removed entirely | 3500 px ✗ | 5000 px (1.0%) | 5000 px (1.0%) | 200 px |

Each Cmt-5 sub-step must keep `-010` at 0/0 and not regress driver/prior-clip-wins gates.

## Visual diagnoses

**`-032`** (4-stripe pattern under Cmt-A: GREEN-RED-GREEN-RED of 25 wide each in the 100×100 wrapper): Cmt-3's drain places abspos pieces correctly at `innerCols[0]` (inner col-1 in outer col-1) and `innerCols[2]` (inner col-1 in outer col-2) — but skips `innerCols[1]` and `innerCols[3]` (inner col-2 fragments). Cause: inner col-2 has no in-flow content, so its column fragment is emitted with `BlockSize=0`. The drain's slicing loop in `oof_fragmentation_drain.go:159-180` hits `if availInThisCol <= 0 { continue }` (line 162-164) and skips without slicing.

**`-011`** (top half green, bottom half wrapper-red under Cmt-A): inner places top 50 of the contain:size box in outer col-1, but outer col-2 is empty. The green child's `BlockBreakToken` chaining isn't propagating through the inner multicol's outgoing `BlockBreakToken`. Compounded by louis14's incorrect `IsMonolithic`-on-`contain:size` deviation.

**`-033`** (two thin black strips ~5×40 in the diff PNG under Cmt-A): ~200 px alignment misalignment around the abspos position. Likely either column-gap percentage rounding or stitched-offset arithmetic in `oof_fragmentation_drain.go`'s outer-content-box-offset accumulation.

**`-010`** (red square mid + red strip bottom-left under Phase 14b removed): inner-2 multicol partially fragments instead of deferring (Phase 14b previously prevented this). Inner-2 has red bg; its fragments paint red where contain:size green should fill. Same root cause as `-011`.

## Blink research summary (verified against Chromium source)

1. **Phase 14b has NO Blink analogue.** cla.cc clamps `column_size.block_size = available_outer_space` per row (line 945) and lets `BlockBreakToken.ConsumedBlockSize` carry resume. louis14's `constrainColumnBlockSize` already does this clamping (multicol_layout.go:1957). Phase 14b is pure short-circuit / louis14-only deviation.
2. **`contain: size` does NOT make a box monolithic in Blink.** `IsMonolithic` is set only when `IsBlockFragmentationForcedOff` (scrollable overflow, replaced content). louis14 incorrectly sets it for `contain:size` at `block_layout.go:85-87`.
3. **OOF inner-col enumeration uses a flat list with `ClampedToValidFragmentainerCapacity`** (oof_part.cc:3148) which floors fragmentainer block-size to a positive value, NOT zero. That's why Blink doesn't suffer the `availInThisCol <= 0` skip.
4. **Inner multicol resume threads `BlockBreakToken.ChildBreakTokens` end-to-end** for in-flow content. Inner column's break_token's `child_break_tokens` must include each unfinished child with `consumed_block_size` set.
5. **`tallest_unbreakable_block_size_` and `minimum_column_block_size` retry already exist** in louis14 (`PropagateTallestUnbreakableBlockSize` + Phase 16.c.1 column regrowth at `multicol_layout.go:1417-1462`). Not the gap.

## Cmt-5 sub-step sequence

### Cmt-5a — remove `contain:size` IsMonolithic deviation

**File:** `pkg/layout/block_layout.go:85-87`.

Delete or gate this block:
```go
if bla.style != nil && bla.style.HasSizeContainment() {
    builder.SetIsMonolithic(true)
}
```

Per Blink (`fragmentation_utils.cc::SetupFragmentBuilderForFragmentation`), `IsMonolithic` is set based on `IsBlockFragmentationForcedOff`, NOT on `contain:size`. The contain:size box should fragment normally via plain `BlockBreakToken` chaining.

**Risk:** other tests in the suite may currently pass because of this deviation. Run the multicol gate after this commit to catch regressions. If many tests regress, scope the change tighter (e.g., only remove inside fragmentation contexts, or keep it for `auto`-height contain:size where intrinsic-block-size matters).

**Validation:**
- Drivers + prior-clip-wins must hold.
- `-010` must hold at 0/0.
- `-011` may not change (Phase 14b still defers it; the contain:size change only matters once content actually reaches column fragmentation).

### Cmt-5b — wire `BlockBreakToken.ChildBreakTokens` through inner multicol break

**Files:** `multicol_layout.go::buildOuterBreakResult` (around line 496-537), and the per-column BLA result handling.

When the inner multicol emits its outgoing `BlockBreakToken` (in `buildOuterBreakResult`), the token's `ChildBreakTokens` must include each unfinished child's break token with the child's `ConsumedBlockSize` set so the resumed inner can continue placing it.

Currently `buildOuterBreakResult` populates `ChildBreakTokens: outBuilder.Children()` from `outBuilder` which collects spanner/column resume info — verify whether per-column BLA child break tokens (the ones that say "this in-flow descendant has consumed N px and should resume") are forwarded too. If not, propagate them.

Reference: Blink's `column_layout_algorithm.cc` `LayoutChildren`+`AddBreakToken` cluster around lines 605-714, plus `buildOuterBreakResult`'s analogue `ToBoxFragment` post-loop wiring.

**Validation:**
- `-011` should improve from 5000 px under Cmt-A to closer to 0 (assuming Cmt-5a is also applied locally for testing).
- `-010` must hold at 0/0.

### Cmt-5c — drain inner col-2 zero-block-size fix

**File:** `pkg/layout/oof_fragmentation_drain.go`.

The slicing loop at lines 159-180 skips inner col-2 fragments because they arrive with `BlockSize=0` (no in-flow content). Blink uses `ClampedToValidFragmentainerCapacity` (oof_part.cc:3148) which floors to a positive value.

Two fix options:
1. **Capture nominal column block-size** at `walkForInnerColumns` time (lines 220-235): instead of using `inner.Fragment.Size.HeightF64()`, look up the inner multicol's declared/balanced column-block-size and use that. This requires plumbing the column-block-size from the inner multicol's layout into the drain.
2. **Floor `availInThisCol`** in the slicing loop (lines 161-165): when `availInThisCol <= 0` and we have remaining content, treat the column as having the SIBLING column's nominal size (peek at the matched-pair inner col-1 fragment in the same outer column). Simpler but less Blink-aligned.

Option 1 is preferred. Find where the inner multicol records its column-block-size (search for `colBlockSize` setting and `BoxTypeColumn` emission). Surface it on the column fragment or via a side-channel.

**Validation:**
- `-032` should close from 5000 px under Cmt-A to 0/0 (assuming Cmt-5a if needed).
- Drivers + prior-clip-wins must hold.

### Cmt-5d — `-033` 200 px alignment fix

**File:** likely `pkg/layout/oof_fragmentation_drain.go`.

The 200 px diff is two thin vertical strips around the abspos position. Suspected causes:
1. **Column-gap percentage resolution disagreement.** -033 uses `column-gap: 30%` (resolves to 15px on inner 50px wide). Reference uses `15px` directly. If the drain reads column-gap differently from column-content layout (e.g., resolving against the WRONG percentage base — outer 100 instead of inner 50), the abspos shifts.
2. **Stitched-offset arithmetic.** The OuterContentBoxOffset accumulation in `walkForInnerColumns` (lines 217-233) might lose 1-2 px when computing the column box's outer-content-box-offset across the inner multicol's offset hierarchy.

Diagnostic approach:
- Print debug info on each `innerCols[k]` entry's `OuterContentBoxOffset` and compare against expected (which can be hand-computed from -033's geometry).
- If column-gap is the issue, audit the gap value used in inner column position vs column-gap CSS resolution.

**Validation:**
- `-033` must close from 200 px under Cmt-A to 0/0 (assuming a-c).
- Drivers + prior-clip-wins must hold.

### Cmt-5e — narrow Phase 14b (Cmt-A re-application)

After a/b/c/d are landed and verified to close `-011`, `-032`, `-033` under Cmt-A, narrow Phase 14b's gate at `multicol_layout.go:423`:

```go
if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
    columnFill == "auto" && outerAvailable < explicitBlockSize &&
    mla.space.FragmentainerOffset > 0 {
```

This lets `-011`/`-032`/`-033` fragment in place via Cmt-3+Cmt-4 while preserving Phase 14b's protection for `-010` (where inner-2's `FragmentainerOffset > 0` after the 100-tall green box).

**Validation gate (final):**
```bash
# 13 drivers + targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Closure targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-(010|011|032|033)\.html$'

# Full multicol sweep (only after closure)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/' 2>&1 | tail -5
```

Expected after Cmt-5a–e: 13 drivers 13/13 (or 14/15 with -032 closing → 15/15), 9 prior-clip-wins 9/9, all closure targets at 0/0.

## Sub-step ordering and validation strategy

Each sub-step a–d should be implemented and committed separately on the worktree, with each commit landing gate-neutral against current HEAD (Phase 14b firing). To validate each fix actually works, the implementer must temporarily apply Cmt-A locally, run the targeted tests, then revert Cmt-A before committing.

c and d both touch `oof_fragmentation_drain.go` — sequence them or do them together in one commit.

a and b are coupled (neither alone closes -011) — sequence them or do them together.

## Worktree commit order

1. Cmt-5c (drain) — own commit on `phase-25-oof-fragmentation`
2. Cmt-5d (-033 alignment) — own commit
3. Cmt-5a (contain:size IsMonolithic) — own commit
4. Cmt-5b (BlockBreakToken chaining) — own commit
5. Cmt-5e (Phase 14b narrow) — own commit, validates the whole chain

Each commit message must include the targeted test results (before/after) and confirmation that drivers + prior-clip-wins held.

## Tracker updates

After each successful Cmt-5x commit on the worktree, update `findings.md` § Phase 25 Cmt-5 entry on `multicol-phase-21-24` (umbrella, in `/Users/iansmith/louis14`).

## Quick references

- Cmt-4 commit: `e84d3ece` on `phase-25-oof-fragmentation` worktree.
- Cmt-3 drain: `pkg/layout/oof_fragmentation_drain.go` (Cmt-3b at `7ed644d7`).
- Phase 14b: `pkg/layout/multicol_layout.go:423-442`.
- Cmt-A original (rejected on Phase 22): `3c17b1da` on `multicol-phase-22`.
- Per-row clamp: `pkg/layout/multicol_layout.go::constrainColumnBlockSize` line 1939, gating at line 1957.
- `tallest_unbreakable` propagation: `pkg/layout/fragment_builder.go:301-336`, `multicol_layout.go:88-95, 1161-1164`.
- Phase 16.c.1 column regrowth (cla.cc:1099-1124 equivalent): `multicol_layout.go:1417-1462`.
