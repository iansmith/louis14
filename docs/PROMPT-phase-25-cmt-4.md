# P25 Cmt-4 — continuation prompt

You're continuing Phase 25 (fragmentation-aware OOF positioning, Blink-aligned port) on louis14. Cmt-1 scaffolding (`4540a8f0`), Cmt-2 collection wiring (`3b6b0e5d`), Cmt-3a promotion + propagation foundation (`8a9226ef`), and Cmt-3b drain pipeline (`7ed644d7`) are on disk. This is the commit that re-applies Phase 22 Cmt-B and unblocks `-011` / `-032` closure.

## Setup

- **Worktree:** `/Users/iansmith/louis14-phase-25` on `phase-25-oof-fragmentation`. Fonts already symlinked from main dir; do not re-symlink.
- **Read first:** `findings.md` § Phase 25 (canonical brief, now with Cmt-3a/Cmt-3b status), then `docs/PLAN-phase-22.md` §14.2-14.4 (the Cmt-B attempt + why it regressed `-032`). The OOF-on-fragmentation gap that previously regressed `-032` is now closed by Cmt-3 — Cmt-B's break-emission logic can land cleanly on top.
- **Verify on disk before doing anything else:**
  - `pkg/layout/oof_fragmentation_drain.go` exists (Cmt-3b's drain).
  - `multicol_layout.go` has `builder.SetIsBlockFragmentationContextRoot()` near top of `Layout()` and `mla.HandleOofFragmentation(builder)` just before `builder.Build()` at line ~1095.
  - `block_layout.go` `inheritPropagatedOOF` is invoked when child carries `FragmentedOofData` (lines ~845-849 and ~1002-1006), not only on regular candidates.

## What this commit does

Re-apply the post-loop break guard from `docs/PLAN-phase-22.md` §14.2. This is the canonical Blink-aligned check that emits a break token when the inner multicol exits its walker loop "complete" (all content placed) while the outer fragmentainer is exhausted and the inner's explicit block-size is still un-consumed.

The guard goes in `multicol_layout.go` after the post-loop top-off, somewhere in the body of `Layout()` that runs *after* `layoutLine` returns and before the outgoing fragment is built. The exact insertion point should be where Cmt-B was previously placed — search the file for `mla.remainingContentBlockSize` to orient (it's read at the top of `Layout` on line ~290 and re-read by anything that needs the post-fragment-consumption tail).

The guard itself, verbatim from §14.3 of the plan:

```go
// Outer fragmentainer exhausted but explicit content block-size remains.
// Mirrors Blink's post-LayoutChildren remaining_content_block_size_ break check.
if hasOuterFrag && hasExplicitBlock && blockCursor >= outerAvailable &&
    blockCursor < mla.remainingContentBlockSize {
    return buildOuterBreakResult()
}
```

`hasOuterFrag` and `hasExplicitBlock` are already in scope at the multicol's `Layout()` body. `blockCursor` and `outerAvailable` are also there. `mla.remainingContentBlockSize` is the field set on line ~290 of multicol_layout.go (do not recompute — use the field). `buildOuterBreakResult()` is the function that builds the inner's outgoing fragment with the right `BreakToken`.

Don't re-introduce any of the conservative narrowings that Cmt-A explored (positioned-only, has-OOF-only, etc.). Cmt-B's premise is uniform: if outer space is exhausted and the inner has more declared block-size to consume, emit a break. Period.

## Why this works now (when Cmt-B alone didn't)

Per `docs/PLAN-phase-22.md` §14.4, the `-032` regression on Cmt-B came from the OOF path: when the inner multicol broke, its OOF descendants bubbled upward as raw `outOfFlowCandidates`, which the outer's BLA then re-staged with the wrong CB and double-propagated. With Cmt-3:

- Promotion at the CB's BLA converts the abspos to a `LogicalOofNodeForFragmentation` *before* it can be raw-propagated.
- The descendant rides the inner's break via `FragmentedOofData` → `MulticolsWithPendingOOFs`.
- The drain at the outer fragmentation context root resolves the OOF against the inner's full column flow once both inner-multicol fragments exist.

So the inner can break cleanly across outer columns and the OOFs land in the right inner column fragmentainers.

## Verification gate (targeted only — CLAUDE.md §4)

```bash
# 13 drivers + targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Cmt-4 closure targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-(011|032)\.html$'
```

Expected: 13 drivers 13/13 · 9 prior-clip-wins 9/9 · `-033` 0% · **`-011` 0% · `-032` 0%**.

`-032` close but not 0%:
- Inspect the diff PNG: which fragmentainer is the abspos missing from?
- Check `oof_fragmentation_drain.go` `findInnerColumnFragmentainers` — is it walking far enough? Are there 4 inner cols enumerated for `-032`?
- Check `layoutFragmentainerDescendant` — is `startInline` correct? Is `cbBlock` the abscb's content-box block-size (50, not the OOF's)?

`-011` close but not 0%:
- `-011` has no OOF — improvement is purely from Cmt-B's break emission.
- If 1.0% remaining as in the original Cmt-B attempt: per §14.8 of the plan, two hypotheses (`contain:size` content fragmentation through resume; inner row-stride misalignment on resume). Trace the column-content's `BlockSizeForFragmentation` and `mla.rowHeight()` on the resumed pass.

`-033` regression or any prior-clip-win regression → STOP, revert, investigate. Most likely cause: the post-loop guard fires on a path it shouldn't (e.g., for a multicol whose break should have come from inside the loop). Check `hasOuterFrag` / `hasExplicitBlock` gating; check whether the inner's `remainingContentBlockSize` correctly reflects content already consumed in prior outer fragmentainers.

Don't run broader sweeps until the targeted gate is green.

## After Cmt-4

If `-011` and `-032` close, run the full multicol gate to capture the broader ripple:

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/' 2>&1 | tail -5
```

Phase 22's original Cmt-1 closure target list (per `PLAN-phase-22.md` §0) was `multicol-nested-{011,013,014,016,017,018,019,020,022,023,024,027,032}` + `multicol-fill-balance-026` — many of those may now close (Cmt-1 set up the `ConsumedBlockSize` chain; Cmt-3 the OOF pipeline; Cmt-4 the break guard). Capture which closed and which didn't.

If a meaningful set closes (>5 net), that's likely the natural end of Phase 25's main thrust. If only 2-3 close (`-011` + `-032` + maybe one ripple), expect Cmt-5+ to chase the residuals — likely IMCB / inset-based OOF sizing for descendants whose CSS doesn't have explicit width/height (Cmt-3 only handles the explicit case via `resolveExplicitOOFSize`).

Update `findings.md` § Phase 25 sequencing (mark Cmt-4 done, log multicol-gate ripple, decide if Cmt-5 is needed) and commit on `multicol-phase-21-24` (umbrella, in `/Users/iansmith/louis14`), not on the worktree. Then write `docs/PROMPT-phase-25-cmt-5.md` if needed.

## Quick references

- Cmt-B verbatim + tradeoff analysis: `docs/PLAN-phase-22.md` §14.2-14.4.
- Phase 25 brief + Cmt-3a/Cmt-3b status: `findings.md` § Phase 25.
- Cmt-3 drain entry point: `pkg/layout/oof_fragmentation_drain.go`
  (`MulticolLayoutAlgorithm.HandleOofFragmentation`).
- Phase 22 Cmt-1 `ConsumedBlockSize` wiring (still needed): `pkg/layout/multicol_layout.go` `buildOuterBreakResult`.
