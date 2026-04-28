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

**Commit 3's premise — that WRITE-side flattening alone restores 13/13 — is wrong.**

### Path A spike result (2026-04-28) — Path A is NOT mechanical

Operator-requested empirical test: in worktree at Commit 2 baseline (`a8ea3adb`), comment out the `ClipBlockAxisOnly` setter at `multicol_layout.go:1273` and run the 13 drivers. Spike B added the Commit 3 attempt diff on top.

| Test | Cmt 2 baseline | Spike A: Cmt 2 + clip OFF | Spike B: Cmt 3 + clip OFF |
|---|---|---|---|
| column-height-001/010/017 | PASS×3 | PASS×3 | PASS×3 |
| column-height-026 | PASS | PASS | **FAIL 1.0%** |
| column-height-027 | PASS | PASS | **FAIL 0.5%** |
| multicol-nested-030 | PASS | PASS | **FAIL 1.0%** |
| multicol-nested-031 | PASS | PASS | **FAIL 1.0%** |
| multicol-rule-nested-balancing-004 | PASS | PASS | PASS |
| nested-past-fragmentation-line | PASS | PASS | PASS |
| nested-floated-multicol-with-monolithic-child | PASS | **FAIL 0.2%** | **FAIL 0.2%** |
| spanner-fragmentation-001 | FAIL 0.8% | FAIL 0.8% | FAIL 0.8% |
| spanner-fragmentation-004 | PASS | **FAIL 1.0%** | **FAIL 1.0%** |
| spanner-fragmentation-006 | FAIL 1.4% | FAIL 1.5% | FAIL 1.4% |
| **PASS COUNT** | **11/13** | **9/13** | **5/13** |

**What the spike tells us:**

1. **16.d.1 partially closed the upstream gap.** All 5 `column-height-001..017` and `multicol-rule-nested-balancing-004` + `nested-past-fragmentation-line` are clip-independent now. That's a big improvement vs the 2026-04-27 16.c.2 attempt 2 (which broke ALL 13 drivers). Progress.md's claim "no longer load-bearing for any of the 13 driver tests" is half-true — true for 9/13, false for the other 4.

2. **Path A is NOT mechanical.** Removing the clip alone breaks 2 NEW tests (`spanner-fragmentation-004`, `nested-floated-multicol-with-monolithic-child`) and slightly worsens `-006`. Path A would land at 9/13 baseline, still −2 from Commit 2's 11/13.

3. **The walker port has an INDEPENDENT regression vector.** Commit 3 on top of clip removal regresses 4 ADDITIONAL tests (`column-height-026/027`, `multicol-nested-030/031`) that pass under both Commit 2 baseline AND Spike A. These tests went through 16.d.1's per-fragment clamp path, which apparently relies on break-token shape that the walker WRITE-flat changes — possibly the per-fragment clamp inspects the resumed break token chain, and flat encoding hands it different state than positional. This is not a clip issue; it's a walker port bug.

### Revised recommendation

**The bundled phase as currently designed cannot reach 13/13 without a deeper redesign.** Three findings to fold into a corrected brief:

(a) The walker WRITE-flat encoding interacts with 16.d.1's per-fragment clamp in a way that breaks 4 currently-passing tests. Spike B identified the cluster (`column-height-026/027` + `multicol-nested-030/031`) but not the mechanism. **First debugging step before any retry: trace the break-token chain on `column-height-026` under Commit 2 vs Spike B and identify where 16.d.1's clamp diverges.** Without this, no port (Path A or otherwise) lands at 13/13.

(b) After that mechanism is understood and fixed, Path A's residual cost is 2 tests (`spanner-fragmentation-004`, `nested-floated-multicol-with-monolithic-child`) — these likely need either Phase 16.d.2/3 (`TallestUnbreakableBlockSize` carrier, queued at task_plan.md:42) or a narrower clip predicate. Either way, 16.c.2 retry #3 is NOT the mechanical removal commit the brief described.

(c) **Path B (walker-with-clip-shim) — re-evaluate, don't reject outright.** I called it concept-incoherent earlier because it re-introduces the re-detect step. But the spike shows the walker port itself isn't yielding the predicted simplification benefits — there are at least 4 walker-flat-specific regressions to fix anyway. If those fixes inevitably re-introduce some BLA-driven post-spanner content discovery, Path B's "emit clip + column-content driver" is a smaller perturbation than redesigning the walker. Worth a serious look before committing to (a)+(b).

### What NOT to do

- Do **not** retry Commit 3 by piling predicates on the spanner branch. The spike shows the regression vector is upstream of the spanner branch — it's break-token shape divergence with 16.d.1's clamp.
- Do **not** treat 16.c.2 retry #3 as "mechanical." It's not. Update the brief and any continuation note that uses that framing.
- Do **not** assume `progress.md` line 19 ("ClipBlockAxisOnly is no longer load-bearing for any of the 13 driver tests") was a comprehensive claim. It was tested only with the clip in tree. Spike A is the actual test.

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
