# P25 Cmt-5 — continuation prompt

You're continuing Phase 25 (fragmentation-aware OOF positioning, Blink-aligned port). Cmt-1 scaffolding (`4540a8f0`), Cmt-2 collection (`3b6b0e5d`), Cmt-3a promotion (`8a9226ef`), Cmt-3b drain (`7ed644d7`), and Cmt-4 post-loop break guard (`e84d3ece`) are landed on the worktree. Cmt-4 was gate-neutral — the post-loop guard never got the chance to fire on `-011`/`-032` because **Phase 14b's defer (`pkg/layout/multicol_layout.go:423-442`) returns a 0-height fragment before the walker loop runs**. Cmt-5 reshapes Phase 14b.

## Setup

- **Worktree:** `/Users/iansmith/louis14-phase-25` on `phase-25-oof-fragmentation`. Fonts symlinked. Currently at `e84d3ece`.
- **Read first:** `findings.md` § Phase 25 sequencing (Cmt-4 entry has the diagnostic table); `docs/PLAN-phase-22.md` §14.8–14.9 (the Cmt-A `-033` regression); `docs/PROMPT-phase-25-cmt-4.md` for the Cmt-B context.
- **Verify on disk:** `multicol_layout.go` should have the Cmt-4 guard between the OOF block and `builder.SetSize(...)` (search for "Phase 25 Cmt-4"). Phase 14b is unchanged at lines 423-442.

## What Cmt-4 measured (the gap Cmt-5 must close)

With Cmt-4 alone: `-011`/`-032` both at 2.1% baseline, `-033` 0/0 px. Phase 14b prevents Cmt-4 from firing on these targets.

With Cmt-4 + Cmt-A re-applied (Phase 14b narrowed to `mla.space.FragmentainerOffset > 0`):
- `-011`: 1.0% (5,000 px) — same residual as the original Phase 22 Cmt-A+Cmt-B measurement
- `-032`: 1.0% (5,000 px) — improved from Phase 22's 4.2% regression; Cmt-3's OOF pipeline is doing its job
- `-033`: 0.0% (200 px) — **REGRESSION**, down from Phase 22's 400 px. Cmt-3 reduced the magnitude but did not eliminate it.

The 200 px `-033` regression blocks Cmt-A from being landed. Cmt-5 must either eliminate that 200 px gap (so Cmt-A can land) or find a smarter Phase 14b condition.

## Cmt-5 sub-problem (a): close the `-033` 200 px regression under Cmt-A

`-033`'s test (`multicol-nested-033.html`) and reference (`multicol-nested-033-ref.html`) are nearly identical — the test uses `column-gap: 30%` which should resolve to 15px, the ref uses `column-gap: 15px` directly. Both have:
- outer 100×100 columns:2 column-fill:auto column-gap:0
- inner 100% × 200 columns:2 column-fill:auto with the resolved column-gap
- abscb position:relative 100% × 50
- abspos top:0 left:0 100% × 400 background:green

Currently (with original Phase 14b) both render identically — the test passes. Under Cmt-A both still hit the walker loop, but render with a 200 px diff (50×4 area, suggesting one row-or-column-strip mis-positioned). Diff PNGs at:
- `output/reftests/multicol-nested-033_diff.png`
- `output/reftests/multicol-nested-033_test.png`
- `output/reftests/multicol-nested-033_ref.png`

To reproduce: re-apply the Cmt-A narrowing to `multicol_layout.go:423` (add `&& mla.space.FragmentainerOffset > 0` to the gate), then run:

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-033\.html$'
```

Hypotheses for the 200 px (in priority order):
1. **Column-gap resolution path differs between abspos placement and column-content placement.** The test uses 30% which resolves through one path; the ref uses 15px directly. If Cmt-3's `oof_fragmentation_drain.go` resolves OOF dimensions/positions using a different gap value than the column-content layout, abspos would land in the wrong column. Trace `mla.columnGapSize` in the inner multicol vs the gap used in `LayoutFragmentainerDescendant`.
2. **Inner row-stride misalignment on resume** when the inner is now allowed to fragment in outer col-1 (instead of being deferred). Check `mla.rowHeight()` and `offsetInCurrentRow(blockCursor)` on the resumed pass.
3. **Drain re-emits abspos in two outer fragments** (once on inner col-1's outer-col-1 slice, once on inner col-1's outer-col-2 slice). Check `oof_fragmentation_drain.go` `findInnerColumnFragmentainers` for double-counting.

## Cmt-5 sub-problem (b): once (a) holds, narrow Phase 14b

Once `-033` holds at 0/0 under Cmt-A, re-apply Cmt-A on top of Cmt-4:

```go
if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
    columnFill == "auto" && outerAvailable < explicitBlockSize &&
    mla.space.FragmentainerOffset > 0 {
```

Per §14.9: "the defer at FragmentainerOffset==0 was carrying load that the current code doesn't otherwise provide." Cmt-3 has now provided most of that load (the OOF case). Cmt-5(a) closes the residual 200 px. After (b), `-011`/`-032` should reach 1.0%.

## Cmt-5 sub-problem (c) [STRETCH / probably Cmt-6]: residual 1.0% on `-011`/`-032`

The 1.0% (5,000 px) residual under Cmt-A+Cmt-B is **not** OOF-related — it shows on `-011` which has no OOF child. Per `PLAN-phase-22.md` §14.8, two hypotheses:

1. **`contain:size` content fragmentation through resume.** `-011`'s green box is `contain:size; width:400%; height:100px`. It's supposed to be column-fragmented into 4 strips of 25×100, then row-paginated by the inner multicol into 8 outer-column slices. Our column-content layout under the resumed inner may not be correctly carrying `BlockSizeForFragmentation` / monolithic-overflow state across the inner's break.
2. **Inner row-stride misalignment on resume** when resumed in outer col-2 with `ConsumedBlockSize=50` — `mla.rowHeight()` and `offsetInCurrentRow(blockCursor)` may produce columns that misalign with the inner's first-pass columns.

If sub-problem (a)'s investigation already touches these code paths, fold the closure into Cmt-5. Otherwise defer to Cmt-6.

## Verification gate (per CLAUDE.md §4 — targeted only)

```bash
# 13 drivers + targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Closure targets + Cmt-A regression test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-(011|032|033)\.html$'
```

Expected after Cmt-5(a)+(b): drivers 14/15 (or 15/15 if `-032` closes), prior-clip-wins 9/9, `-033` 0/0 px, `-011`/`-032` ≤1.0% (closing further is Cmt-6 territory).

`-010` (multicol-nested-010) is **not** in the gate above but is the original Phase 14b reference test. Run it explicitly when modifying Phase 14b:

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-010\.html$'
```

If `-010` regresses, the Phase 14b reshape is wrong — the FragmentainerOffset=0 defer was specifically protecting `-010`.

## After Cmt-5

If `-011`/`-032` improve (even partially), update `findings.md` § Phase 25 Cmt-5 entry and write `docs/PROMPT-phase-25-cmt-6.md` for the residual closure (sub-problem (c) if not folded in). Commit on `multicol-phase-21-24` (umbrella, in `/Users/iansmith/louis14`), not on the worktree.

## Quick references

- Cmt-4 commit: `e84d3ece` on `phase-25-oof-fragmentation` worktree.
- Phase 14b: `pkg/layout/multicol_layout.go:423-442`.
- Cmt-A original (rejected on Phase 22): `3c17b1da` on `multicol-phase-22`.
- Cmt-B original analysis: `docs/PLAN-phase-22.md` §14.2-14.4.
- Cmt-A regression analysis: `docs/PLAN-phase-22.md` §14.8-14.9.
- Cmt-3 drain: `pkg/layout/oof_fragmentation_drain.go`.
