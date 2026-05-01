# Phase 21 — Conditional `IsMulticolContainer` clip (detailed plan)

Status: PLANNED · 2026-04-29
Hard-blocked by: Phase 22 (and a triage call on spanner-fragmentation-001/004/006).
Single brief diverge from `findings.md` § Phase 21: the implementation is a *deletion*, not a conditional. Reasoning below.

## 0. One-line summary

Remove the unconditional clip override at `pkg/render/paint_layer.go:282-285`. The remaining clip logic at lines 255-345 is already a faithful port of Blink `LayoutBox::UpdateFromStyle`'s `should_clip_overflow` predicate. The override is the only Blink diverge in that block; deleting it brings paint to Blink parity.

Closes 3 stuck tests: `multicol-gap-large-001`, `increase-prev-sibling-height`, `multicol-nested-029`.
Risks regression on 12 currently-passing tests (9 prior-clip-wins + 3 spanner-fragmentation drivers) whose underlying layout bugs the unconditional clip is currently masking.

## 1. Blink reference (verified 2026-04-29)

`third_party/blink/renderer/core/layout/layout_box.cc::UpdateFromStyle` (~line 623, current main):

```cpp
bool should_clip_overflow = (!StyleRef().IsOverflowVisibleAlongBothAxes() ||
                             ShouldApplyPaintContainment()) &&
                            RespectsCSSOverflow();
SetHasNonVisibleOverflow(should_clip_overflow);
```

Three operative facts:

1. **No multicol special case.** Searched the same file for `IsLayoutNGMultiColumnFlowThread`, `kColumnFlowThread`, `multicol`, `MultiColumn` — there is **no** branch that forces `should_clip_overflow=true` for multicol containers on its own. The clip is purely a function of user-set `overflow` + paint containment + the `RespectsCSSOverflow()` opt-out.
2. **`IsOverflowVisibleAlongBothAxes`** is `OverflowX() == kVisible && OverflowY() == kVisible`. The negation in louis14 is precisely `clipX || clipY` *after* `GetOverflowX/Y` has applied CSS Overflow Level 3's interdependency rule (if either axis is non-visible, the other resolves to `auto`).
3. **`RespectsCSSOverflow()`** returns true for ordinary block containers (the only case multicol takes). Non-block-container subclasses (input/select/textarea elements with their own scrollers) override it; none of them apply to a `columns: N` div.

So Blink's predicate, projected onto louis14, is:

```
clip iff (clipX || clipY) || hasPaintContain
       == (clipX || clipY)            // because hasPaintContain already feeds clipX and clipY
```

## 2. What louis14 currently does (post-Phase-20)

`pkg/render/paint_layer.go:255-345` (single newPaintLayer flow):

```go
overflowX := s.GetOverflowX()
overflowY := s.GetOverflowY()
hasPaintContain := s.HasPaintContainment()
clipX := overflowX == css.OverflowHidden || overflowX == css.OverflowScroll || overflowX == css.OverflowAuto || hasPaintContain
clipY := overflowY == css.OverflowHidden || overflowY == css.OverflowScroll || overflowY == css.OverflowAuto || hasPaintContain

forceBorderBoxClip := box.ClipContentToBorderBox            // CSS Tables 3 §5.4.1 row-collapse case
if forceBorderBoxClip {
    clipX = true
    clipY = true
}

// Phase 20 P20.5 — UNCONDITIONAL OVERRIDE (the Phase 21 target):
if box.IsMulticolContainer {
    clipX = true
    clipY = true
}

// ...later, clip rect built at padding-box (or border-box if forceBorderBoxClip).
```

Lines 255-259 already implement Blink's `(!IsOverflowVisibleAlongBothAxes() || ShouldApplyPaintContainment())`. The P20.5 block at 282-285 is the only piece that diverges from Blink — it forces clip irrespective of `overflow`, mirroring the original (incorrect) Phase 20 understanding that "multicol always sets HasNonVisibleOverflow=true."

## 3. Implementation diff

### 3.1 `pkg/render/paint_layer.go` (the only behaviour change)

Delete lines 282-285 + the preceding doc comment 274-281 (replace with a short note that explains the absence). Final shape:

```go
// CSS Tables 3 §5.4.1: visibility:collapse rowspan cells force border-box
// clip regardless of computed overflow. (existing block)
forceBorderBoxClip := box.ClipContentToBorderBox
if forceBorderBoxClip {
    clipX = true
    clipY = true
}

// Multicol containers do NOT force a clip on their own — Blink
// LayoutBox::UpdateFromStyle keys should_clip_overflow purely on
// user-set overflow + paint containment. The Box.IsMulticolContainer
// flag is set in layout (multicol_layout.go) and reserved for future
// per-axis or overflow-clip-margin work; it has no effect here.

// (existing Phase 16.e+18 v2 B3 / Phase 20 P20.3 comment block stays)
```

That's the entire functional change. Six deleted lines, six explanatory lines.

### 3.2 `pkg/layout/multicol_layout.go` — lightly update the comment

The existing comment at lines 1042-1070 says the flag promotes into a structural overflow clip. Update the wording to match the new reality:

> P20.5 marks this fragment as a multicol container. Originally consumed by paint to force a padding-box overflow clip; Phase 21 (2026-04-29) reverted that consumption to match Blink's `UpdateFromStyle`, which does not force the clip on multicol. Flag is retained for future per-axis or `overflow-clip-margin` work.

No code change in `multicol_layout.go`.

### 3.3 `pkg/layout/types.go` — same comment update on the `Box.IsMulticolContainer` doc.

No code change.

## 4. Why this is a deletion, not a guarded condition

The brief proposed:

```go
if box.IsMulticolContainer && (clipX || clipY || s.HasPaintContainment()) {
    clipX = true
    clipY = true
}
```

After verification, this guard is a tautology:

- `clipX || clipY` is true iff at least one axis is hidden/scroll/auto/clip *or* `hasPaintContain` (lines 258-259).
- The right-hand `s.HasPaintContainment()` is already subsumed into both `clipX` and `clipY`.

So `IsMulticolContainer && (clipX || clipY || hasPaintContain)` ≡ `IsMulticolContainer && (clipX || clipY)`. And inside that branch we set `clipX=true, clipY=true`. But if `clipX || clipY` is already true, the rect-building code at 311-344 already runs with the correct rect. The only effect of the guarded block is to widen a single-axis clip into a both-axis clip when the multicol flag is set. Blink does not do that — Blink's clipX/clipY are independent and a multicol that sets only `overflow-x: hidden` should clip only along x.

A faithful Blink port is: **remove the override entirely**, let `clipX, clipY` stand as computed at lines 258-259. This is the patch this plan proposes.

## 5. Stuck-test verification (post-deletion)

| Test | Pre-Phase-21 diff | Why dropping the override fixes it |
|---|---|---|
| `multicol-gap-large-001` | 1600 px (0.3%) | Test explicitly asserts content "extend into column-gap" and "outside the right edge of multi-column [if 'overflow' is set to 'visible']". Default overflow is visible; the unconditional clip masks legitimate overflow. After deletion, content paints freely → diff goes to 0. |
| `increase-prev-sibling-height` | 80 px | `<div style="columns:2;">` containing "PASS" with default line-height. Glyph ink-overflow above the line-box top is clipped by the padding-box-top clip. After deletion, glyphs paint freely. |
| `multicol-nested-029` | 85 px | `<div columns:1; height:10em;>` outer with `<div columns:2; line-height:0.8>` inner. `line-height:0.8` makes glyph ink overflow the line-box; the inner-multicol's unconditional clip cuts the ascenders. After deletion, glyphs paint freely. |

All three tests use default `overflow: visible`. None set explicit overflow; none set `contain: paint`. After deletion of the override, `clipX = clipY = false` for these multicol containers, and the rect-building branch at 306-344 is skipped entirely.

## 6. Regression-risk surface — currently-passing tests that depend on the clip

Verified 2026-04-29 by reading every HTML and running the suite: 12 currently-passing tests have layout that paints past the multicol's padding-box. Without the unconditional clip, they regress unless the underlying layout produces correct geometry.

### Group A — nested-multicol resume (Phase 22 covers these)

| Test | Multicol shape | Why it currently over-paints |
|---|---|---|
| `multicol-breaking-002` | outer `height:100; columns:4`; inner `height:300; columns:2` | Inner doesn't resume across outer columns (Phase 22). Excess inner content paints past outer's `height:100`. |
| `multicol-breaking-nobackground-002` | same as above without inner background | same |
| `multicol-fill-balance-nested-000` | nested `columns:2; column-fill:auto; height:100` outer / inner `columns:2` | Inner over-paints; ref expects clean green square. |
| `multicol-nested-015` | outer `columns:2; height:100`; inner `columns:5` with `break-before:avoid` | Nested-resume + break-violation handling. |
| `multicol-nested-021` | outer `columns:2; height:100`; inner `columns:2` with multi-row content | Nested-resume across outer column boundary. |
| `multicol-nested-026` | nested `columns:2; column-fill:auto` with overflowing content | Same shape as -021. |
| `multicol-nested-028` | outer `columns:4; height:100`; inner `columns:2; min-height:125` | min-height + nested + break-inside:avoid. |

All 7 are exactly the cluster Phase 22 targets (the brief explicitly names `multicol-nested-011..032` + `multicol-fill-balance-003/-026`; the 015/021/026/028 set is a subset of that range).

### Group B — float / clear under multicol (Phase 22-adjacent)

| Test | Shape | Failure |
|---|---|---|
| `nested-after-float-clearance` | `columns:4; height:100; width:100` with `float:left; height:200` followed by `clear:left; columns:2; height:200` | Float over-paints past 100-px outer. May or may not be fixed by Phase 22; could need a separate fix. |

### Group C — list markers in multicol

| Test | Shape | Failure |
|---|---|---|
| `multicol-list-item-001` | `<ul column-width:5em>` with 10 list items, ahem font, each marker `"X"` | Need to read the diff post-Phase-21 to see which side over-paints. List-item markers placed outside the column padding-box may be the cause. |

### Group D — spanner descendants over-painting (NOT covered by Phase 22)

| Test | Shape | Failure |
|---|---|---|
| `spanner-fragmentation-001` | `columns:2; height:100` outer with `column-span:all` whose descendants exceed the spanner's declared height | Walker-port residual: descendants of a partially-laid-out spanner over-paint past the multicol's box. Driver invariant. |
| `spanner-fragmentation-004` | similar, `columns:2; height:100` with two `column-span:all` blocks | same |
| `spanner-fragmentation-006` | `columns:4; height:100` with three `column-span:all` blocks | same |

These are listed as the "13 driver invariants" today and are passing because of the unconditional clip. Phase 22 does NOT fix them — they're a separate "spanner-overflow placement" problem, most plausibly addressed by Phase 23 (`FinishFragmentation` port) which is the right area for clamping per-fragment block-size.

## 7. Sequencing

```
Phase 22  ──→  re-run 9 prior-clip-wins (still pass with clip)
                 │
                 ▼
            Phase 21 deletion  ──→  run 3 stuck tests (close at 0)
                                   ──→  re-run 9 prior-clip-wins (must still pass at 0)
                                   ──→  re-run spanner-fragmentation-001/004/006
                                          │
                                          ├─ all pass: ship
                                          └─ regress: triage (next §)
```

Phase 21 cannot land before Phase 22 — confirmed by reading all 9 prior-clip-win HTMLs. Six of them are exactly the nested-resume failure mode Phase 22 is sized for; one (`-028`) is a min-height variant of the same; one (`nested-after-float-clearance`) is an adjacent float-clear case.

After Phase 22, before applying Phase 21, sweep: run gate `211 + N`, run all 9 prior-clip-wins. They must all pass with the clip still in place — that confirms Phase 22 fixed the underlying layout, with the clip currently doing nothing useful for them.

## 8. If spanner-fragmentation-001/004/006 regress

The brief offers two paths. Recommendation: **Path B (clean Blink parity, fix in follow-up).**

Path A — narrower predicate (rejected). E.g. add a `box.HasSpannerDescendants` flag and re-install the clip when set. Reasons against:
- Adds Blink-diverging code on the paint side.
- Hides a layout bug in spanner-descendant placement.
- CLAUDE.md §1: "no point fixes — pick a category and solve it completely."
- The next person to touch paint_layer.go has another override to reason about.

Path B — accept the regression, fix in follow-up. Reasons for:
- Phase 21 stays a clean Blink-parity port.
- Spanner-descendant over-paint is a real layout bug; clip was masking, not fixing.
- Phase 23 (`FinishFragmentation` port) is the correct site for the fix — it touches per-fragment block-size clamping, which is exactly what spanner descendants need. Spanner-overflow placement folds into Phase 23 naturally.
- 13-driver count drops 13/13 → 10/13 temporarily, but no pre-existing wins are lost in the long run; Phase 23 is queued anyway.

If Path B is taken, Phase 21 lands with a documented note: "spanner-fragmentation-001/004/006 temporarily regress; tracked under Phase 23."

If the user disagrees and prefers Path A, the cleanest formulation is to add a `box.IsMulticolContainerWithBrokenSpannerDescendants` gated to exactly the broken layout shape, applied at paint time. That's still Blink-diverging; recommend against it.

## 9. Verification gate (per CLAUDE.md §4)

Order matters; do NOT skip ahead. Each step is a separate test invocation.

### 9.1 Pre-flight (before editing)

Confirm the gate is at the documented baseline:

```bash
# 13-driver invariants
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'
# Expect: 13/13 pass, 0 diff.

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'
# Expect: 9/9 pass, 0 diff.

# 3 stuck tests
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-gap-large-001|increase-prev-sibling-height|multicol-nested-029)\.(html|xht)$'
# Expect: 3 fail at 1600px / 80px / 85px.
```

### 9.2 Apply the deletion + comment updates

Edit `pkg/render/paint_layer.go:282-285` (delete the block + its 274-281 comment).
Update comments in `pkg/layout/multicol_layout.go:1042-1070` and `pkg/layout/types.go:97-105`.

### 9.3 Build

```bash
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...
```

### 9.4 Targeted re-run (the 12 + 3)

Run all 15 tests from §9.1 again. Expected outcomes:

- 3 stuck tests: PASS at 0 diff.
- 9 prior-clip-wins: PASS at 0 diff (Phase 22 must already have made the underlying layout correct).
- 3 spanner-fragmentation drivers: PASS at 0 diff if the underlying spanner-descendant placement is also correct; FAIL otherwise. Discuss before proceeding to §9.5.

If any of the 9 prior-clip-wins regress, STOP — Phase 22 didn't fully cover the case; revert and investigate.
If only the 3 spanner-fragmentation drivers regress, decide Path A vs B per §8.

### 9.5 Full multicol gate sweep

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol' 2>&1 | tail -30
```

Expected: 211 + Phase-22-deltas + 3 (the 3 stuck tests) ± any unexpected reclaims/regressions. Examine each unexpected delta individually.

### 9.6 4-cat gate sweep

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests'                       # CSS2 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-flexbox'        # 626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-position'       # 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes'  # 781/781
```

Expected: all four invariant. If any other category regresses, paint clip is leaking into non-multicol code paths — investigate before commit. (Unlikely; the only changed branch was multicol-flagged, and `IsMulticolContainer` is only set by `multicol_layout.go`.)

## 10. Commit shape

Single mainline commit on `master` (no worktree — change is six lines in one file).

Commit message draft:

> P21: drop unconditional multicol overflow clip (Blink parity)
>
> Phase 20 P20.5 force-set HasNonVisibleOverflow=true on every multicol
> fragment. Blink LayoutBox::UpdateFromStyle does not — the clip is purely
> a function of user-set overflow + paint containment. paint_layer.go
> already implements that predicate at lines 255-259; the override at
> 282-285 was the only Blink-diverge in the block. This commit removes it.
>
> Closes 3 stuck tests (multicol-gap-large-001, increase-prev-sibling-
> height, multicol-nested-029) which all use default overflow:visible.
>
> Box.IsMulticolContainer flag stays for future per-axis /
> overflow-clip-margin work; it just no longer drives a clip.
>
> Multicol gate: 211 → 214+ depending on Phase 22 / spanner-fragmentation
> outcome.

## 11. Hard exits

- Driver invariant regression beyond the 3 spanner-fragmentation drivers → STOP, revert, investigate. Phase 22 left coverage gaps.
- Any 4-cat gate regression → STOP, revert, investigate.
- Any of the 9 prior-clip-wins regress → STOP. The Phase 22 prerequisite was incomplete; do NOT mask via narrow predicate.
- Spanner-fragmentation-001/004/006 regress → discuss with user. Default recommendation is Path B (accept, fold into Phase 23).

## 12. Files touched (final)

| File | Change |
|---|---|
| `pkg/render/paint_layer.go` | Delete 274-285 (12 lines: 8 comment + 4 code), replace with 4-line note. |
| `pkg/layout/multicol_layout.go` | Comment edit at 1042-1070; no code change. |
| `pkg/layout/types.go` | Comment edit at 97-105; no code change. |

Estimated lines changed: ~30 (mostly comment).
Estimated commits: 1.
Worktree: no.
