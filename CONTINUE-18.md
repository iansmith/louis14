# CONTINUE: Phase 16.e + 18 bundled v2 — Step 0 + B0 done; B1 next

## Authoritative brief

**v2 brief (Option A — clip-removal-first):** `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)". Read the entire v2 section before any code change. v1 brief is preserved in findings.md but marked SUPERSEDED — do not implement against v1.

## State (2026-04-28, post-Step-0 + B0)

**Worktree:** `/Users/iansmith/louis14-phase-16e-18`, branch `phase-16e-18-walker-carrier`. Clean.
- HEAD: `fdb9343a` (B0 — contentNode pointer cache fix; Step 0 finding).
- Previous commits: `a8ea3adb` (Cmt 2 — walker READ + positional WRITE), `43ec8c66` (Cmt 1 — schema + scaffold).
- Stash `stash@{0}`: Cmt 3 attempt (walker WRITE flat). Reused at B5 with reconciliation.
- Driver result with B0: **13/13 at 0 diff** (Cmt 2 + cache fix). The -001/-006 regressions Cmt 2 introduced are clear of cache.

**Mainline:** `fix/flexbox-fast`. Gate unchanged until worktree merges: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol 196/455 · spanner-frag 11/13.

## v2 sequence

| # | Scope | Status |
|---|---|---|
| ~~Step 0~~ | DIAGNOSTIC: trace `column-height-026` break-token chain at Cmt 2 vs Spike B | **DONE.** Hypothesis confirmed — contentNode pointer instability in walker dispatch. See findings.md error log entry "v2 Step 0 diagnostic + B0 cache fix". |
| ~~B0~~ | contentNode pointer cache (1 field + 5 lines) | **DONE** on worktree `fdb9343a`. 11/13 → 13/13 at Cmt 2 baseline. |
| **B1 (NEXT)** | `TallestUnbreakableBlockSize` field on `LayoutResult` + `PropagateTallestUnbreakableBlockSize` on builder | 13/13 drivers, gate unchanged |
| B2 | Wire propagation: `BreakBeforeChildIfNeeded` + `SetupFragmentation` + child-result propagation; populate `tallestUnbreakable` at multicol_layout.go:1601 | 13/13 drivers, gate ≥ 196 |
| B3 | Mechanical `ClipBlockAxisOnly` removal (setter + paint branch + struct fields + propagation) | 13/13 drivers (B0 + B2 should close all clip-dependent paths) |
| B4 | Re-verify walker READ (Cmt 2 already landed) | post-B3 baseline holds |
| B5 | Walker WRITE flat (Cmt 3 stash + B3 reconciliation) | 13/13 at 0 diff |
| B6 | Phase 18 `ConsumedRowBlockSize` carrier WRITE site | 13/13 + multicol gate 196 → 211+ |
| B7 | Drop `IsInsideColumnSpanner` clamp gate | 13/13, spanner-frag-006 holds |
| B8 | Full gate sweep + merge worktree → `fix/flexbox-fast` | gate target met |

## Step 0 result (DONE 2026-04-28)

**Hypothesis confirmed and B0 fix landed.** The divergence between Spike A (passing column-height-026) and Spike B (failing 1.0%) traced to a single root cause: `MulticolPartWalker` dispatches by `child.Node == multicolContainer` pointer equality, but `MulticolLayoutAlgorithm.Layout()` allocates a fresh `contentNode := &LayoutInputNode{...}` on every call. Outer column 1's inner-multicol Layout emits a break token whose `outBT[0].Node` points to col-1's contentNode; outer column 2's inner-multicol Layout builds the walker with col-2's contentNode (different pointer). The walker's identity check fails, falls through to the spanner branch, and mis-dispatches every column-content resume as a spanner. The clip-OFF path makes this fault visible (Spike B 5/13); the clip path masked it (Cmt 2 baseline 11/13 with -001 / -006 still showing through).

Spike A passed because Cmt 2's positional WRITE puts col-rows resume at slot[2] with slot[0]=nil. Walker iter 1 sees child[0]=nil → empty entry → column-content with nextColToken=nil → `layoutLine` runs FRESH, re-laying out the 400h block from zero. Outer col 2 visually duplicates outer col 1's content — coincidentally matching the reference. Cmt 2 was always semantically broken (post-Cmt-1); just visually masked by the fresh-layout coincidence on this specific test.

Cache fix: 1 struct field on `LayoutInputNode` + 5 lines of cache logic in `MulticolLayoutAlgorithm.Layout`. **13 driver tests: 11/13 → 13/13.** Both -001 (0.8%) and -006 (1.4%) close at 0 diff. Walker dispatch now correct under both positional WRITE (Cmt 2) and flat WRITE (Cmt 3).

Cache fix is **B0** — landed on worktree `fdb9343a`.

Full Step 0 matrix recorded in findings.md error log entry "v2 Step 0 diagnostic + B0 cache fix" (2026-04-28):

| Config | Cache OFF | Cache ON |
|---|---|---|
| Cmt 2 + clip ON (baseline) | 11/13 | **13/13** |
| Cmt 2 + clip OFF (Spike A) | 9/13 | 10/13 |
| Cmt 3 + clip ON | 11/13 | **13/13** |
| Cmt 3 + clip OFF (Spike B) | 5/13 | 10/13 |

Three residuals at clip-OFF (-004, -006, nested-floated) correlate with clip handling, not WRITE model — these are the genuine 16.d.2/3 carrier work. v2 B1+B2 target them.

`column-height-008` hangs at clean baseline `a8ea3adb` regardless of cache fix (10m timeout). Pre-existing; not caused by cache. Track separately.

## Next concrete action: B1

`TallestUnbreakableBlockSize` field on `LayoutResult` + `PropagateTallestUnbreakableBlockSize` on `BoxFragmentBuilder`. NOT yet wired (B2 does that).

Files:
- `pkg/layout/layout_result.go` — add `TallestUnbreakableBlockSize float64` field.
- `pkg/layout/box_fragment_builder.go` — add `PropagateTallestUnbreakableBlockSize(LayoutUnit)` method.
- `pkg/layout/multicol_layout.go:1576+1601` — TODOs already in place. Don't populate yet (B2 wires propagation).

Verification: build clean. 13 drivers PASS at 0 diff. Multicol gate ≥ 196.

Hard exit: any of 13 drivers regresses → field/method definition is incompatible with existing code. STOP and diagnose.

## Tracking files

- `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 ..." — authoritative.
- `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF (prep complete 2026-04-28) — SUPERSEDED" — v1 preserved for archaeology.
- `findings.md` § "Phase 16.d Blink research (2026-04-27)" — still current; cited by v2 B1/B2.
- `findings.md` § Error Log entries 2026-04-28 — three entries: Cmt 3 hard-exit, Path A spike, v2 redesign.
- `progress.md` § "Active phase" — points to v2.
- `task_plan.md` § "Current focus" — points to v2.

## Operational reminders

- Worktree work commits to `phase-16e-18-walker-carrier`, NOT `fix/flexbox-fast`.
- Mainline updates are docs-only.
- Always commit + push before launching any sub-agents (worktrees start from HEAD).
- CLAUDE.md rules: study Blink first, all tests at 0 diff, no chasing easy wins.
- Each B-commit verifies independently; if a B-commit's hard exit fires, STOP and engage operator. Do not pile predicates.
