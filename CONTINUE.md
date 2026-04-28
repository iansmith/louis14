# CONTINUE: css-multicol next-step

Active operational continuation. Concise pointer for the next session.

## Where we are

Mainline `fix/flexbox-fast` @ `b251c8db`. Phase 16.e+18 v2 + multicol border-box clip residual fix + **B6 Phase 18 carrier WRITE site** landed.

- Multicol gate: **205/455** (B6 was gate-neutral; see note below).
- 13 driver invariants: **13/13** at 0 diff.
- Spanner-fragmentation cluster: **12/13** (-008 fails at 0.2%, pre-existing; outside the 13-driver subset). Earlier "13/13 cluster" claim was stale.
- 4-category invariants: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (1499/6720, 16 known-fails).

**B6 outcome (2026-04-28, `b251c8db`):** parity-correct mirror of Blink cla.cc:822-833 but gate-neutral. WRITE-site guard `shouldWrapColumns && hasRowHeight && isFirstRow && hasOuterFrag && rowHeight > outerAvailable - rowStart` never fires for the brief's named targets (multicol-nested-011..032 + multicol-fill-balance-003/-026) because they use `column-fill:auto` with default `column-wrap:auto` and auto `column-height`, so `shouldWrapColumns()` is false. Brief's "+12 to +15" expectation was mismatched to mechanism.

## Next options (operator pick one)

### Investigate `ConsumedBlockSize` chain for multicol-nested-011..032 + multicol-fill-balance-003/-026 (recommended next)

The brief's named B6 target tests need a different fix from the row-phase carrier. Likely the standard `ConsumedBlockSize` chain on the inner multicol's outgoing BlockBreakToken — when an inner multicol breaks across an outer column boundary, the resume in the next outer column needs to start at the right offset. Read `multicol-nested-011` PNG diff to characterise the failure pattern, then trace the inner multicol's resume path (incoming BlockBreakToken → MLA.Layout → blockCursor seeding).

This is the path back to the +12 to +15 gate move that B6's brief mistakenly attributed to the row-phase carrier.

### ~~B7 — drop `IsInsideColumnSpanner` clamp gate~~ (ATTEMPTED + REVERTED 2026-04-28)

Regressed `spanner-fragmentation-005` + `-006` (drivers 13/13 → 12/13; gate 205 → 203). The 16.d.1 guard is still load-bearing post-walker-port. See findings.md error-log for diagnosis. Future retry would need to first extend the spanner-resume break-chain absorption (`pendingPartialSpannerToken` / `spannerConsumed` / `pendingContentOverflow`) to consume self-fragmented descendant chains — that's a real port, not the cleanup the brief implied.

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
