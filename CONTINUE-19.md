# CONTINUE: Phase 16.e + 18 v2 — B2.5 → SetupFragmentation → B5

Operational continuation for the post-hard-exit-B3 plan. After v2 hit hard exit B3 at 10/13 (3 residuals all involving monolithic content), the operator approved options 1+2+3 in sequence with a pause before option 4 (revert B3). This file is the implementation outline; it should be kept current as each step lands.

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

## The three steps

### Step 1: B2.5 — monolithic detection (with `-004` trace pre-step)

**Pre-step (diagnostic, no commit): trace `-004`'s regression.** Spike A (Cmt 2 + clip OFF, no carrier) had `-004` at 1.0%. B3 (clip OFF + B0+B1+B2 carrier) has it at 2.1%. The test uses `column-fill:auto` + explicit height — should NOT trigger `IsInitialColumnBalancingPass`, so B2's carrier should be a no-op. Yet the diff worsened.

Hypotheses to falsify:
- B2's BLA child loop unconditionally propagates `childResult.TallestUnbreakableBlockSize` even when result is 0 — but should be no-op since 0 is filtered. Verify by tracing.
- B2's `ShouldAvoidBreakInside` check fires on a child that doesn't have break-inside:avoid, but the child has some other style that resolves to "avoid" we didn't expect. Verify with style trace.
- The change is unrelated to B2 — maybe B0's cache fix or something else slightly perturbs `-004`'s layout in a way the clip used to mask. Verify by running Cmt 2 + B0 + clip OFF (no B1/B2) and checking `-004`.

If the pre-step shows B2 isn't actively wrong on `-004` (it's a residual gap, not a bug), proceed to monolithic detection. If B2 IS actively wrong, fix the bug first.

**Then: monolithic detection.**

Files:
- `pkg/layout/layout_result.go` — add `IsMonolithic bool` field on `PhysicalFragment` next to `ClipContentToBorderBox`.
- `pkg/layout/box_fragment_builder.go` (= fragment_builder.go) — add `SetIsMonolithic(bool)` accessor; populate on `Build()`.
- Population sites:
  - `block_layout.go` — set when `style.GetContain()` ∈ {"size", "strict"} for `nested-floated`'s contain:size box.
  - `multicol_layout.go:layoutSpanner` / `layoutSpannerInFrag` — set when the spanner's measured content height exceeds its declared/clamped height (implicit monolithic for column-balance) for `-004` / `-006`.
  - (Defer replaced-element detection — img/video/canvas — until a test exercises it.)
- `pkg/layout/fragmentation_utils.go` — extend `ShouldAvoidBreakInside`:
  ```go
  func ShouldAvoidBreakInside(space ConstraintSpace, layoutResult *LayoutResult) bool {
      if layoutResult == nil || layoutResult.Fragment == nil {
          return false
      }
      if layoutResult.Fragment.IsMonolithic {
          return true
      }
      breakInside := "auto"
      if s := layoutResult.Fragment.Style; s != nil {
          breakInside = s.GetBreakInside()
      }
      return IsAvoidBreakValue(space, breakInside)
  }
  ```
  Mirrors Blink: `result.GetPhysicalFragment().IsMonolithic() || IsAvoidBreakValue(space, ResolvedBreakInside(result))`.

Verification: build clean. 13 drivers PASS at 0 diff (target: closes the 3 residuals). 4-category invariants intact.

Hard exit: if any of the 3 residuals stays at fail, document which carrier hop is still missing. If a previously-passing test regresses, monolithic detection fires too eagerly — narrow the population sites.

### Step 2: SetupFragmentation border/padding contribution

Mirror Blink fragmentation_utils.cc:510-514. Find the equivalent setup site in louis14 (likely `block_layout.go` near BLA's Layout entry, where the constraint space is decoded).

```go
if bla.space.IsInitialColumnBalancingPass {
    // Border + padding block-start and block-end are themselves "unbreakable" —
    // the column box must be at least as tall as those edges.
    builder.PropagateTallestUnbreakableBlockSize(borderPadding.BlockStart)
    builder.PropagateTallestUnbreakableBlockSize(borderPadding.BlockEnd)
}
```

Likely doesn't help the 3 residuals (none have meaningful borders on the affected nodes), but ~10 lines to verify.

Verification: build clean. 13 drivers PASS at 0 diff. No regressions vs Step 1 baseline. Drop with no behavioral change confirms the brief's prediction.

### Step 3: Proceed to B5 — walker WRITE flat

Apply the Cmt 3 v1 stash with reconciliation:
1. `git stash apply stash@{0}` will conflict in `multicol_layout.go` because B3 deleted the clip-only mid-spanner block (lines 786-790 in pre-B3) that the stash modified.
2. Reconcile: drop the stash's `walker.Next()` + clip-token push at the clip-only site (the entire block is gone post-B3). The `pendingContentOverflow` combined-clip handling also simplifies (no second clip can occur with no clip path).
3. Remaining stash content applies cleanly: `buildOuterBreakResult` rewrite, content-overflow flat emission, `AddBreakBeforeChild` substitutions, `flushWalker` cleanup, post-loop pendingContentOverflow handler.

Verification: 13 drivers at the post-Step-2 baseline (target 13/13; minimum: the post-B3 floor). 4-category invariants intact. Build clean.

Hard exit: drivers regress below post-Step-2 baseline → walker WRITE flat exposes a bug B0+B2+B2.5+SetupFragmentation didn't catch. STOP, diagnose, do NOT pile predicates.

## After Step 3 — pause for review

Per operator: "after you finish with number three we should see where we are." Do NOT proceed to B6 (Phase 18 carrier) or B7 (drop IsInsideColumnSpanner gate) without re-engaging.

## Operational reminders

- All commits land on the worktree branch `phase-16e-18-walker-carrier`. Do NOT commit code to `fix/flexbox-fast` from a worktree; mainline only gets docs commits.
- Each step verifies independently: build clean + 13 drivers + 4-category invariants. If any step's verification fails, STOP — don't roll forward into the next step.
- CLAUDE.md rules apply: study Blink first; all tests at 0 diff; no chasing easy wins; foundational correctness over quick wins.
- After each step, update `progress.md` § "Active phase" and `findings.md` error log to reflect what landed and what's residual.
- Update this file as steps land — mark each section as DONE / IN PROGRESS / TBD with the commit hash + driver result.
