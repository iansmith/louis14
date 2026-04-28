# CONTINUE: Phase 16.e + 18 v2 — Path X DONE; +2 multicol gate; PAUSED at 10/13 drivers

Operational continuation for the post-hard-exit-B3 plan. After v2 hit hard exit B3 at 10/13 (3 residuals all involving monolithic content), the operator approved options 1+2+3 in sequence with a pause before option 4 (revert B3). All three landed; operator-mandated pause now in effect.

## Result

Driver count stayed at **10/13** across all three steps. Residuals are unchanged in count but evolved in diff:

| Test | B3 baseline | B2.5 | B2.6 | B5 (current) |
|---|---|---|---|---|
| `nested-floated-multicol-with-monolithic-child` | 0.2% FAIL | 0.2% FAIL | 0.2% FAIL | 0.2% FAIL |
| `spanner-fragmentation-004` | 2.1% FAIL | 2.1% FAIL | 2.1% FAIL | **1.0% FAIL** |
| `spanner-fragmentation-006` | 0.2% FAIL | 0.2% FAIL | 0.2% FAIL | 0.3% FAIL |
| (other 10) | PASS | PASS | PASS | PASS |

`-004` improved meaningfully under B5 (walker WRITE flat). The walker port is mechanically beneficial; it just doesn't close the residual fully. `-006` slightly worsened (sub-pixel) under B5 — same residual, slight rendering shift.

4-category invariants intact at every step: CSS2 99/99 + flex 626/629 + pos 92/105 + wm 781/781 = 1499/6720 (16 known-fails matching pre-existing gate).

## Tracking files

- **`findings.md`** — authoritative reference. Read these sections in order:
  - "Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)" — the v2 design.
  - "Phase 16.d Blink research (2026-04-27)" — verbatim Blink references for `TallestUnbreakable` carrier + `MonolithicOverflow` (print-only) + ColumnLayoutAlgorithm consumer at cla.cc:1879-1948.
  - Error log entries 2026-04-28 — Cmt 3 v1 hard-exit, Path A spike, v2 redesign, Step 0/B0, B1+B2+B3 hard-exit. The B3 entry includes the 3-residual diagnosis.
- **`progress.md`** — § "Active phase" tracks the current state at-a-glance.
- **`task_plan.md`** — § "Current focus" + "Phase ordering" tracks the v2 commit sequence and folded-in phases (16.c.2, 16.d.2/3).
- **`CONTINUE-18.md`** — previous operational continuation; superseded by this file but preserved for context. Hard exit B3 → next-step options remain valid reference.

## Worktree state

- **Path:** `/Users/iansmith/louis14-phase-16e-18`.
- **Branch:** `phase-16e-18-walker-carrier`.
- **HEAD:** `f97e4ac0` (B3 — clip removal). Below sits B2 (`f513f338`) → B1 (`8e2aa078`) → B0 (`fdb9343a`) → Cmt 2 (`a8ea3adb`) → Cmt 1 (`43ec8c66`).
- **Stash `stash@{0}`:** Cmt 3 v1 attempt (walker WRITE flat). Reused at B5 with reconciliation against B3's deletions.
- **Driver result post-B3:** 10/13. Residuals: `nested-floated-multicol-with-monolithic-child` (0.2%), `spanner-fragmentation-004` (2.1% — regressed from Spike A's 1.0%), `spanner-fragmentation-006` (0.2%).
- **Mainline gate:** unchanged until v2 B8 merge. CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol 196/455 · spanner-frag 11/13.

## The three steps (all DONE)

### Step 1: B2.5 — monolithic detection — DONE on `da5730b8`

**Pre-step trace finding (no commit, in detached HEAD at fdb9343a):** ran `-004` under "B0 cache fix + clip OFF, no B1/B2" — got **2.1% FAIL**, identical to B3's diff. Confirmed B2's wiring is NOT actively wrong on `-004`. The regression vs Spike A (1.0%) traces to the cache fix (B0) exposing -004's true non-clip layout that the broken walker dispatch was coincidentally masking. Real residual; proceed to monolithic detection.

**Implemented:**
- `PhysicalFragment.IsMonolithic` bool field (layout_result.go).
- `BoxFragmentBuilder.SetIsMonolithic` accessor + Build()-site population (fragment_builder.go).
- BLA Layout entry: `SetIsMonolithic(true)` when `style.HasSizeContainment()` (block_layout.go).
- MLA `layoutSpanner` / `layoutSpannerInFrag`: `markSpannerMonolithicIfOverflowed` post-layout helper sets fragment.IsMonolithic when `IntrinsicBlockSize > fragment.BlockSize()` (multicol_layout.go).
- `ShouldAvoidBreakInside` extended with `Fragment.IsMonolithic` short-circuit (fragmentation_utils.go).
- `CalculateUnbreakableBlockSize` extended: when monolithic and intrinsic > fragment block-size, use intrinsic (fragmentation_utils.go).

**Detection verified to fire** via temporary trace prints (subsequently removed). For `-006`: intrinsic=370 vs frag=10 → monolithic. For `nested-floated`: contain:size on div → monolithic.

**But driver count stayed 10/13.** Diagnosis (full text in B2.5 commit message `da5730b8`):

(a) `-004` / `-006` (spanners): louis14's `resolveColumnAutoBlockSize` measure pass exits at `spannerPath` BEFORE laying out the spanner. The spanner fragment never enters BLA's child loop, so its `IsMonolithic` doesn't propagate via the parent-side `TallestUnbreakable` hook. Blink's measure pass DOES lay out spanners; closing this requires extending `resolveColumnAutoBlockSize` to layout the spanner when spannerPath is detected — a structural change beyond v2 brief's scope.

(b) `nested-floated`: float has `columns:1; column-fill:auto`. Float's `balanceColumns` is false (column-fill:auto + non-fragmented context bypasses both the explicit-balance check and the implicit "outer-fragmentation-without-known-block-size" check). Without `IsInitialColumnBalancingPass`, the carrier propagation never fires for the float's contain:size child. Closing requires either widening `balanceColumns` for floats or a non-balancing-pass carrier path.

The B2.5 infrastructure is correct; the residual gaps are upstream of the carrier (measure-pass spanner handling + balanceColumns scope).

### Step 2: SetupFragmentation border/padding contribution — DONE on `3b3b4208`

Added BLA Layout-entry hook propagating `geom.Border.BlockStart + geom.Padding.BlockStart` and `geom.Border.BlockEnd + geom.Padding.BlockEnd` as unbreakable floors during initial column-balancing pass. Mirrors Blink fragmentation_utils.cc:510-514.

Driver result: 10/13 — UNCHANGED. As predicted in the brief, none of the 3 residual tests have meaningful borders on the affected nodes. Hook wired for completeness; future bordered-multicol tests will pick it up automatically.

### Step 3: B5 — walker WRITE flat — DONE on `33afa6fa`

Applied the v1 Cmt 3 attempt (preserved as `git stash@{0}` since hard-exit 2026-04-28). Stash auto-merged cleanly with B3 + B2.5 + B2.6 — git's three-way merge dropped the stash's modifications to the clip-only mid-spanner block (deleted by B3) automatically.

Walker WRITE flat replaces the 3-slot positional outgoing break-token encoding with a flat document-order list. `buildOuterBreakResult` is parameterless and consumes a `MulticolBreakTokenBuilder` accumulator; spanner content-overflow emits the spanner's break token directly (no wrapper); break-before-spanner uses `outBuilder.AddBreakBeforeChild`; `flushWalker` mirrors Blink's cla.cc:733-738 cleanup loop.

Driver result: 10/13 — same count, but `-004` IMPROVED 2.1% → 1.0% (the walker port's flat token shape + flushWalker behave better on this test under no clip). `-006` slightly worsened sub-pixel 0.2% → 0.3%. nested-floated unchanged.

The improvement on `-004` confirms the walker port is mechanically correct and contributes positively. Cmt 3 stash is consumed; can be dropped from `git stash list`.

## PAUSE for review (current state)

Per operator: "after you finish with number three we should see where we are." Do NOT proceed to B6 (Phase 18 carrier) or B7 (drop IsInsideColumnSpanner gate) without re-engaging.

### What's landed (worktree commits)

| Commit | Step | Result |
|---|---|---|
| `33afa6fa` | B5 walker WRITE flat | 10/13 (-004 1.0%, -006 0.3%, nested-floated 0.2%) |
| `3b3b4208` | B2.6 SetupFragmentation border/padding | 10/13 (no change) |
| `da5730b8` | B2.5 monolithic detection | 10/13 (no change; infrastructure correct, upstream gaps) |
| `f97e4ac0` | B3 clip removal | 10/13 (HARD EXIT) |
| `f513f338` | B2 wire TallestUnbreakable | 13/13 |
| `8e2aa078` | B1 TallestUnbreakable scaffold | 13/13 |
| `fdb9343a` | B0 contentNode pointer cache | 13/13 |
| `a8ea3adb` | Cmt 2 walker READ | 11/13 |
| `43ec8c66` | Cmt 1 schema + scaffold | 13/13 |

### Two clearly-defined paths to close the residuals

**Path X: extend the measure pass to layout spanners.** Closes `-004` / `-006`. Structural change to `resolveColumnAutoBlockSize` — when `spannerPath` is detected during the measure pass, layout the spanner via `layoutSpanner` and propagate its TallestUnbreakable contribution. Estimated 30-50 lines + careful trace.

**Path Y: widen `balanceColumns` for non-fragmented float multicols.** Closes `nested-floated`. Smaller change to the `balanceColumns` decision in `multicol_layout.go` Layout entry — also balance when the multicol is laid out inside a float without outer fragmentation. Estimated 10-20 lines + check it doesn't regress fixed-height float multicols elsewhere.

Both paths are upstream-architectural rather than walker-related. Neither is in the v2 brief's stated scope; both would be follow-on commits (B2.7? B2.8?) before B6+.

### Alternative: accept 10/13 and proceed

If the operator accepts 10/13 as the post-B3 floor, B6 (Phase 18 ConsumedRowBlockSize carrier WRITE site) is the natural next step. Targets `multicol-nested-011` and the multicol-nested 012-032 cluster + multicol-fill-balance-003/-026. Multicol gate target post-B6: 196 → 211+ (+15 from Phase 18 cluster). The 3 residuals stay deferred.

## Path X — DONE on `2d6822b3`

Operator approved fixing upstream architectural gaps. Path X implemented; Path Y as I framed it doesn't actually apply.

### Path X: nested-balancing TallestUnbreakable propagation

Researched Blink's `column_layout_algorithm.cc:1535-1734` (`ResolveColumnAutoBlockSizeInternal`). Key Blink reference (cla.cc:1706-1712):

```cpp
if (GetConstraintSpace().IsInitialColumnBalancingPass()) {
  // Nested column balancing. Our outer fragmentation context is in its
  // initial balancing pass, so it also wants to know the largest
  // unbreakable block-size.
  container_builder_.PropagateTallestUnbreakableBlockSize(
      tallest_unbreakable_block_size_);
}
```

Implementation:
- `MulticolLayoutAlgorithm.lastMeasuredTallestUnbreakable` field carries the accumulator across the `resolveColumnAutoBlockSize` → `MLA.Layout` boundary.
- `resolveColumnAutoBlockSize` stores the value before returning.
- `MLA.Layout`'s exit max()s onto `result.TallestUnbreakableBlockSize` when `mla.space.IsInitialColumnBalancingPass`.

**Multicol gate: 197 → 199 (+2).** Tests gained: `multicol-span-all-list-item-001/002` (both nested-multicol with break-inside:avoid where outer balances). 13 drivers: 10/13 unchanged. 4-category invariants intact. No regressions.

### Path X attempt 1 (reverted): spanner layout during measure pass

Earlier tried also laying out the spanner inside `resolveColumnAutoBlockSize` when `spannerPath` was detected, contributing its `IntrinsicBlockSize` to `tallestUnbreakable`. Conceptually targeted -004 / -006.

Reverted because the extra layout call regressed `spanner-fragmentation-000/002/010` — side effects on the spanner's resume state that the main layout pass relies on. Caused net -1 multicol gate vs +2 with the narrowed Path X.

The spanner-content-overflow residuals on -004 / -006 need a different mechanism, probably at the spanner-placement layer rather than the measure-pass layer. Possibly during `LayoutSpanner`: when the spanner has explicit height < content height, propagate up through a different carrier, or place subsequent siblings past content rather than past box. Out of scope for v2 bundled — separate phase.

### Path Y reframed (NOT what I originally proposed)

Originally framed as "widen `balanceColumns` for floats" — wrong diagnosis. Visual inspection of `nested-floated`'s 0.2% diff: the float's `margin-top:10` isn't being honored — float starts at y=0 instead of y=10. So the contain:size 100h box overlaps the absolute green strip rather than appearing below it, leaving y=90..100 transparent (= red showing through).

This is a float-margin-collapse bug specific to floats inside multicol. Not a `balanceColumns` issue. Not a `TallestUnbreakable` issue. Different fix scope entirely; tracked separately as a non-v2 issue.

## Status: PAUSED for review

Worktree at `2d6822b3` (Path X). Three residuals remain on the 13 drivers:

| Test | Diff | Diagnosed cause |
|---|---|---|
| `nested-floated-multicol-with-monolithic-child` | 0.2% | Float `margin-top:10` not honored inside multicol — separate float-margin issue |
| `spanner-fragmentation-004` | 1.0% | Spanner content-overflow visual issue at placement layer — different mechanism needed |
| `spanner-fragmentation-006` | 0.3% | Same family as -004 |

None close cleanly within v2's scope. Each needs targeted work in a different layer. Operator decision: continue with B6 (Phase 18 carrier — targets +15 gate) accepting the 3 residuals, or detour to fix one of the three issues first.

### After Path X — where we are

| Worktree commit | Step | Multicol gate | 13 drivers |
|---|---|---|---|
| `2d6822b3` | Path X (outer propagation) | **199** | 10/13 |
| `33afa6fa` | B5 walker WRITE flat | 197 | 10/13 |
| `3b3b4208` | B2.6 SetupFragmentation contribution | 197 | 10/13 |
| `da5730b8` | B2.5 monolithic detection | 197 | 10/13 |
| `f97e4ac0` | B3 clip removal | 196 | 10/13 |
| `f513f338` | B2 wire carrier | 196 | 13/13 |
| `8e2aa078` | B1 carrier scaffold | 196 | 13/13 |
| `fdb9343a` | B0 contentNode cache | 196 | 13/13 |
| Mainline pre-merge | (gate as-is) | **196** | 11/13 |

Worktree gate is +3 vs mainline; drivers are 10/13 vs mainline 11/13. Net trade: -1 driver test, +3 multicol gate. Plus structural improvements (clip removed, carrier in place, walker WRITE flat).

## Operational reminders

- All commits land on the worktree branch `phase-16e-18-walker-carrier`. Do NOT commit code to `fix/flexbox-fast` from a worktree; mainline only gets docs commits.
- Each step verifies independently: build clean + 13 drivers + 4-category invariants. If any step's verification fails, STOP — don't roll forward into the next step.
- CLAUDE.md rules apply: study Blink first; all tests at 0 diff; no chasing easy wins; foundational correctness over quick wins.
- After each step, update `progress.md` § "Active phase" and `findings.md` error log to reflect what landed and what's residual.
- Update this file as steps land — mark each section as DONE / IN PROGRESS / TBD with the commit hash + driver result.
