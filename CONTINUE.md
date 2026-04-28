# CONTINUE: css-multicol next-step

Active operational continuation. Concise pointer for the next session.

## Where we are

Mainline `fix/flexbox-fast` @ `b85d2d77`. Phase 16.e+18 v2 + multicol border-box clip residual fix both landed.

- Multicol gate: **205/455** (+9 vs pre-v2 196).
- 13 driver invariants: **13/13** at 0 diff.
- Spanner-fragmentation: **13/13** at 0 diff.
- 4-category invariants: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (1499/6720, 16 known-fails).

## Next options (operator pick one)

### B6 — Phase 18 `ConsumedRowBlockSize` carrier WRITE site (recommended next)

Mirrors Blink `column_layout_algorithm.cc:822-833`. Read site already plumbed at `multicol_layout.go:294-306`; the WRITE site is the only piece missing. Targets multicol-nested-011 + multicol-nested-012..032 + multicol-fill-balance-003/-026. **Multicol gate target: 205 → 217+ (+12 to +15).**

Brief: `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2" (the v2 bundle merged but B6 was deferred). Implementation pattern: at the row-advance failure branch in `MulticolLayoutAlgorithm.Layout`, when `shouldWrapColumns && hasRowHeight && isFirstRow && hasOuterFrag`, attach `MulticolData{ConsumedRowBlockSize: paintedAmount}` to the outgoing break token.

**Hard exit:** `multicol-nested-010` regresses → row-carry fires in wrong condition.

### B7 — drop `IsInsideColumnSpanner` clamp gate

Removes Phase 16.d.1's spanner-descendant gate. Should be straightforward post-walker-port. **Hard exit:** `spanner-fragmentation-006` regresses → Phase 16.d.1 gate is still load-bearing; revert.

Sequence with B6: B6 first (independent). B7 after B6 lands (separate verification).

### Reclaim multicol border-box clip regressions

Six tests regressed under `3389efe7` (multicol border-box clip): two meaningful (`inline-block-and-column-span-all` 1.5%, `multicol-fill-balance-032` 1.4%), four ≤ 0.3%. Investigate a narrower clip-gate. Candidates:
- Clip only when there's at least one spanner in the multicol.
- Clip only when content's block-extent exceeds the box (`blockCursor > finalBlockSize` proxy).
- Clip only when an `IsMonolithic` child was placed.

Read the regression PNGs first (`output/reftests/inline-block-and-column-span-all_*.png`) to understand the failure pattern before touching the gate.

### Option 1 — Finish FinishFragmentation port (larger)

Drop the leaf-only gate in 16.d.1 + delete or shrink the parent-side children-loop overflow path in `block_layout.go:1001-1196`. The merged walker port should clean up the prior break-token misalignment that blocked this earlier. Worktree work.

### Phase 19 — span-all-children-height 002-013

12 tests, 7 sub-clusters. Brief: `findings.md` § Phase 19.

## Tracking files

- `progress.md` — current gate + Active phase narrative.
- `task_plan.md` — phase ordering + queued work.
- `findings.md` — authoritative briefs + chronological error log.
- `CONTINUE-18.md`, `CONTINUE-19.md` — historical (v1 hard-exit, v2 operational continuation). Marked HISTORICAL at top.

## Rules pointer

`CLAUDE.md` (project) + `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` (session memory). Key rules:
- Study Blink first; all tests at 0 diff; no chasing easy wins.
- High-risk multi-commit refactors → worktree (B6 likely small enough for mainline; Option 1 is worktree).
- Mainline merges only after gate verification.
