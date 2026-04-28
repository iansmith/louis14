# CONTINUE: Phase 16.e + 18 bundled — Commit 3 HIT HARD-EXIT #1

## Status (2026-04-28)

**Hard-exit #1 hit.** Commit 3's WRITE-site flattening, executed faithfully per the brief, did NOT restore the 13/13 driver invariants. Worktree is **back at Commit 2** (`a8ea3adb`). Commit 3 attempt is preserved in worktree `git stash@{0}` for archaeology, not committed.

The brief's design assumption is incomplete. Re-read of Blink cla.cc:605-714 + 1397-1522 is required before retrying.

**Worktree:** `/Users/iansmith/louis14-phase-16e-18`, branch `phase-16e-18-walker-carrier` @ `a8ea3adb`. Clean. Stash holds Commit 3 attempt.

**Mainline (`fix/flexbox-fast` @ `36b3101c`)** — gate unchanged: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol **196/455** · spanner-frag 11/13.

## What was tried in Commit 3 attempt

Implementation faithfully followed brief items 1-6 (findings.md § "Phase 16.e + 18 BUNDLED BRIEF" line 1282) plus a self-derived flushWalker cleanup mirroring Blink cla.cc:733-738:

1. ✅ Replaced 3-slot positional `buildOuterBreakResult(prevNextColToken, partialSpannerToken)` with parameterless closure backed by `MulticolBreakTokenBuilder` (`outBuilder`). Args dropped; flat document-order accumulation.
2. ✅ Spanner content-overflow build site emits the spanner's break token directly (`outBuilder.AddBreakToken(fullResult.BreakToken)`) — preserves Node, ChildBreakTokens, ConsumedBlockSize, HasSeenAllChildren, SequenceNumber. (Brief said "{Node:spanner, ChildBreakTokens: fullResult.BreakToken.ChildBreakTokens, ConsumedBlockSize: 0}" but stripping fields breaks BLA's resume bookkeeping; preserving the original token is the correct port — matches Blink's `container_builder_.AddBreakToken(child_token)`.)
3. ✅ Combined-clip mutation site pushes the second spanner's clip token as a separate flat entry (no nested ChildBreakTokens append).
4. ✅ Mid-spanner partial-token / clip-only paths push via builder.
5. ✅ Spanner branch READ drops `entry.BreakToken.ChildBreakTokens[0/1]` peeks; uses `entry.BreakToken` directly for content-overflow resume.
6. ✅ Pure-nested-resume case removed (no nil padding emitted).
7. ✅ Added `flushWalker` helper at every early-return site mirroring Blink cla.cc:733-738 cleanup loop.

## Driver test result after Commit 3 attempt

**11/13 pass — same regressions as Commit 2.**

| Test | Commit 2 diff | Commit 3 diff | Δ |
|---|---|---|---|
| `spanner-fragmentation-001` | 0.8% | 0.8% | unchanged |
| `spanner-fragmentation-006` | 1.4% | 1.0% | marginal improvement |

`-001`: visual diff is identical between Commit 2 and Commit 3 — the failure path is invariant under WRITE-site flattening. `-006`: `flushWalker` recovered some pixels but not enough to pass.

## Why the brief's design is incomplete

The brief assumed: with flat WRITE-side encoding, the walker dispatches each spanner / column-content entry directly, `MoveToSpanner` is only called for genuinely-fresh discoveries, and the 3-slot re-detect token at slot[0] becomes redundant. **This is true only for entries that were enumerated in the previous outer column.**

The failing scenario in `spanner-fragmentation-001` exposes the gap:

- Outer column 1 layout enumerates spanner_1 (placed) and spanner_2 (clipped at outer boundary).
- The loop returns at the clip-only mid-spanner site BEFORE `layoutLine` ever reaches spanner_3 / the trailing 40px block.
- Outgoing break token in flat encoding has only `[clipToken_for_spanner_2]` (or `[..., post_spanner_2_token]` after `flushWalker` adds the MoveToSpanner-stored next_column_token).
- Outer column 2 resume: walker dispatches `clipToken_for_spanner_2` → spanner branch → clip-resume placed. THEN the walker has no driver to enumerate **un-discovered** post-spanner_2 content (spanner_3, the 40px block) — those weren't in the parent break token because the previous outer column never enumerated them.

The OLD (pre-Commit-2) 3-slot encoding solved this via slot[0] = `beforeSpannerToken = {Node: contentNode, ChildBreakTokens: [{Node: spanner_2, IsBreakBefore: true}]}` — a column-content driver that, when fed to layoutLine on resume, drove BLA to re-enumerate from spanner_2 onward, naturally discovering spanner_3 / the block via spannerPath returns.

In Blink, this works because Blink doesn't have louis14's "clip-only mid-spanner" path — Blink would have emitted a break-before-spanner_2 in the previous outer column (push spanner_2 entirely to the next column), and on resume the walker entry for spanner_2 + parent BLA's child-enumeration discovers post-spanner content via the standard child-by-child resume.

louis14's `ClipBlockAxisOnly` workaround (queued for removal in Phase 16.c.2 retry #3) is the reason this gap exists. The walker-port-without-clip-removal cannot be both flat AND correct on its own.

## What this means for the bundled phase

**Commit 3's premise — that WRITE-side flattening alone restores 13/13 — is wrong.** Two paths forward, neither is "pile predicates":

**Path A (recommended): Reorder the bundled phase.** Move Phase 16.c.2 retry #3 (mechanical `ClipBlockAxisOnly` removal) **inline with Commit 3**, not after Commit 6. Without the clip workaround, the clip-only mid-spanner site disappears, and break-before-spanner becomes the only spanner-overflow path — which is what the walker model is designed for. Risk: removing the clip currently regresses some tests; previously rolled back twice (Phase 16.c.2 attempts 1, 2).

**Path B: Walker-with-clip-shim.** Keep `ClipBlockAxisOnly` but emit a column-content driver token alongside the clip token in the flat encoding (e.g., `[clipToken_for_spanner, {Node: contentNode, ChildBreakTokens: [{Node: spanner, IsBreakBefore: true}]}]`). On resume, the walker dispatches the clip first, then the column-content driver re-enters layoutLine which re-detects the spanner via spannerPath — but the spanner is already-clip-placed, so we'd need a "skip already-placed spanner" mechanism. **This re-introduces the re-detect step the walker port was supposed to eliminate.** Concept-incoherent; reject.

## Operational state

- **Worktree:** `/Users/iansmith/louis14-phase-16e-18` @ `a8ea3adb` (clean). Commits 1-2 intact.
- **Stash:** `git stash@{0}` holds the Commit 3 attempt diff (multicol_layout.go: +135/-95). Recover with `git stash show -p stash@{0}` if useful for Path B exploration.
- **Driver invariants:** 11/13 (Commit 2 baseline). spanner-fragmentation-001 0.8%, -006 1.4%.

## Next steps (DO NOT execute without re-confirming with operator)

1. Re-read Blink cla.cc:605-714 + 1397-1522 + the Phase 16.c.2 attempt-1/2 rollback notes (findings.md § Phase 16.c.2 retros).
2. Decide between Path A (clip removal inline) vs deeper redesign.
3. Update findings.md § "Phase 16.e + 18 BUNDLED BRIEF" to correct the assumption that WRITE-side flattening alone restores 13/13.

## Hard exit conditions (still in force)

1. Commit 3 doesn't restore Commit 2's regressions → **TRIGGERED 2026-04-28**. STOP, re-read Blink, do not pile predicates.
2. Commit 5 regresses spanner-frag-006 → revert Commit 5; the 16.d.1 gate is still load-bearing.
3. Commit 4 regresses multicol-nested-010 → row-carry write fires in wrong condition.
4. Multicol gate drops below 196 at any commit other than Commit 2 → STOP.

## Operational reminders

- Worktree work commits to `phase-16e-18-walker-carrier`, NOT `fix/flexbox-fast`.
- Mainline updates are docs-only (this file, progress.md, task_plan.md, findings.md).
- CLAUDE.md rules: study Blink first, all tests at 0 diff, no chasing easy wins.
