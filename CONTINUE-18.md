# CONTINUE: Phase 16.e + 18 bundled — MERGED 2026-04-28 (historical)

**This file is HISTORICAL.** Superseded by `CONTINUE-19.md` (also historical) and the merge into `fix/flexbox-fast` on 2026-04-28. See `progress.md` § "Active phase" and `task_plan.md` § "Current focus" for current state.

The content below is preserved for archaeology — captured the v1 hard-exit signal and the operational continuation through B3.

---

# CONTINUE: Phase 16.e + 18 bundled v2 — B1+B2+B3 done; HARD EXIT B3 at 10/13

## Authoritative brief

**v2 brief (Option A — clip-removal-first):** `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)". Read the entire v2 section before any code change. v1 brief is preserved in findings.md but marked SUPERSEDED — do not implement against v1.

## State (2026-04-28, post-B1+B2+B3 hard exit)

**Worktree:** `/Users/iansmith/louis14-phase-16e-18`, branch `phase-16e-18-walker-carrier`. Clean.
- HEAD: `f97e4ac0` (B3 — clip removal; HARD EXIT signal).
- Previous: `f513f338` (B2 — TallestUnbreakable wiring) · `8e2aa078` (B1 — carrier scaffold) · `fdb9343a` (B0 — contentNode cache) · `a8ea3adb` (Cmt 2) · `43ec8c66` (Cmt 1).
- Stash `stash@{0}`: Cmt 3 attempt (walker WRITE flat). Reused at B5 if/when v2 unblocks.
- Driver result post-B3: **10/13.** Below the v2 brief's 11/13 hard-exit threshold.
- 4-category invariants intact: CSS2 99/99 + flex 626/629 + pos 92/105 + wm 781/781 = 1499/6720 (16 known-fails matching pre-existing gate).

**Mainline:** `fix/flexbox-fast`. Gate unchanged until worktree merges.

## v2 sequence

| # | Scope | Status |
|---|---|---|
| ~~Step 0~~ | DIAGNOSTIC | DONE. Hypothesis confirmed — contentNode pointer instability. |
| ~~B0~~ | contentNode pointer cache | DONE on `fdb9343a`. 11/13 → 13/13. |
| ~~B1~~ | TallestUnbreakable field + builder method | DONE on `8e2aa078`. 13/13. |
| ~~B2~~ | Wire propagation (BreakBeforeChildIfNeeded + BLA child loop + multicol consumer). Skipped: SetupFragmentation border/padding + monolithic detection in ShouldAvoidBreakInside | DONE on `f513f338`. 13/13. |
| ~~B3~~ | Mechanical clip removal | DONE on `f97e4ac0`. **HARD EXIT — 10/13.** Three residuals (-004, -006, nested-floated). |
| **B2.5 / pause / accept (operator decision)** | See "Hard exit B3 — next-step options" below | TBD |
| B4 | Re-verify walker READ | TBD post-decision |
| B5 | Walker WRITE flat | TBD post-decision |
| B6 | Phase 18 `ConsumedRowBlockSize` carrier WRITE site | TBD post-decision |
| B7 | Drop `IsInsideColumnSpanner` clamp gate | TBD post-decision |
| B8 | Full gate sweep + merge | TBD post-decision |

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

## Hard exit B3 — next-step options (operator decision required)

v2 brief mandates STOP at <11/13 post-B3. We're at 10/13. Three residuals:

| Test | Diff | Pattern |
|---|---|---|
| `nested-floated-multicol-with-monolithic-child` | 0.2% | float with `contain:size` 100h box containing 90h green |
| `spanner-fragmentation-004` | 2.1% | 50h spanner containing 200h of children; **regressed** vs Spike A's 1.0% |
| `spanner-fragmentation-006` | 0.2% | similar to -004; closer to passing |

All 3 involve content that's "monolithic" — but B2's `ShouldAvoidBreakInside` only checks the style-level `break-inside:avoid` property. Blink's also checks `result.GetPhysicalFragment().IsMonolithic()`. louis14 doesn't have that flag yet.

### Options

**1. B2.5 — extend with monolithic detection (recommended).** Add `IsMonolithic bool` on `PhysicalFragment`. Populate from:
- `Style.GetContain() == "size"` (or `style:size` / `style:strict`) — for `nested-floated`.
- Spanners whose explicit height < their measured content height — for `-004`/`-006`.
- (Eventually) replaced elements (img, video, canvas, iframe).

Update `ShouldAvoidBreakInside` to return true for monolithic fragments. Mirrors Blink. Estimated 30-50 lines.

**Critical pre-step:** trace `-004`'s regression (1.0% → 2.1% under B2). It went *worse* with B2 even though `column-fill:auto` + explicit height should put it outside `IsInitialColumnBalancingPass`. Suggests B2's wiring may be misfiring on this test. Diagnose before extending.

**2. SetupFragmentation border/padding contribution.** Skipped from B2; mirrors Blink fragmentation_utils.cc:510-514. Likely doesn't help the 3 residuals (none have meaningful borders) but ~10 lines to verify.

**3. Accept 10/13; proceed to B5.** Document residuals as deferred. B5 walker WRITE flat builds on 10/13 baseline. B7 (drop IsInsideColumnSpanner gate) might close some residuals via 16.d.1's clamp on spanner children. Risk: -004's regression suggests something actively wrong; compounding it in B5 is dangerous.

**4. Revert B3; pause v2.** Restore clip workaround. Worktree returns to 13/13 baseline. Walker port can't complete (clip-only mid-spanner code path returns).

### Recommendation

Option 1, with the trace pre-step. The residuals are foundationally about monolithic content; B2 is incomplete without the IsMonolithic clause. Trace -004 first to rule out a B2 wiring bug, then add monolithic detection per Blink.

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
