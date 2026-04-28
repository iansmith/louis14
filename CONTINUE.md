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

### Investigate `ConsumedBlockSize` chain for multicol-nested-011..032 + multicol-fill-balance-003/-026 (in progress)

**First pass (2026-04-28) reverted; see findings.md error-log for full diagnosis.** Quick recap:

- Visual: `multicol-nested-011` test image is fully RED — inner multicol places no content. Setup has outer `column-height:50`, inner `height:100`; every outer column is too small for the inner.
- Phase 14b's defer is futile for this class (every outer column has the same shortcoming, defer loops forever).
- Tried gating Phase 14b on `FragmentainerBlockSize >= explicitBlockSize` so it only defers when the next fresh column would fit. Result: non-monotonic — `-011` improved 2.1%→1.6%, `-032` worsened 2.1%→3.1%, gate-neutral overall.
- Real bug: the inner multicol's outgoing `BlockBreakToken` doesn't drive a correct resume in the next outer column. The defer-vs-fragment decision is downstream of that.

**Next-step recipe:** trace the inner multicol's outgoing `BlockBreakToken.ConsumedBlockSize` through the outer multicol's `colBreakToken` plumbing (~`multicol_layout.go:1069-1156`) into the resumed inner's `MLA.Layout` (`:294-302`, where `remainingContentBlockSize` subtracts `BreakToken.ConsumedBlockSize`). Verify each hop. The most likely break is step 1 (inner not setting `ConsumedBlockSize` on its outgoing fragment) or step 3 (resumed inner not seeding `blockCursor` from it).

This is the path back to the +12 to +15 gate move that B6's brief mistakenly attributed to the row-phase carrier.

### ~~B7 — drop `IsInsideColumnSpanner` clamp gate~~ (ATTEMPTED + REVERTED 2026-04-28)

Regressed `spanner-fragmentation-005` + `-006` (drivers 13/13 → 12/13; gate 205 → 203). The 16.d.1 guard is still load-bearing post-walker-port. See findings.md error-log for diagnosis. Future retry would need to first extend the spanner-resume break-chain absorption (`pendingPartialSpannerToken` / `spannerConsumed` / `pendingContentOverflow`) to consume self-fragmented descendant chains — that's a real port, not the cleanup the brief implied.

### ~~Reclaim multicol border-box clip regressions~~ → REPLACED by **Phase 20** (recommended next)

The narrow-gate approach was investigated 2026-04-28 and found non-monotonic — gate-tweaking can't fix the regressions cleanly. Blink research traced the fundamental mechanism (paint-property-tree OverflowClip + `BoxType=kColumnBox`) and revealed louis14's `ClipContentToBorderBox` flag is conceptually right but the implementation is too crude. **Phase 20 is the proper fix.** Brief in `findings.md` § "Phase 20 BRIEF". Continuation prompt in `CONTINUE-20.md`. Worktree work, ~6 commits. Gate target: 205 → 211+.

### Option 1 — Finish FinishFragmentation port (larger)

Drop the leaf-only gate in 16.d.1 + delete or shrink the parent-side children-loop overflow path in `block_layout.go:1001-1196`. The merged walker port should clean up the prior break-token misalignment that blocked this earlier. Worktree work.

### Phase 19 — span-all-children-height 002-013

12 tests, 7 sub-clusters. Brief: `findings.md` § Phase 19.

## Tracking files

- `progress.md` — current gate + Active phase narrative.
- `task_plan.md` — phase ordering + queued work.
- `findings.md` — authoritative briefs + chronological error log.
- **`CONTINUE-20.md` — active continuation prompt for Phase 20 (multicol overflow clip Blink-aligned port). Use this for the next multicol push.**
- `CONTINUE-18.md`, `CONTINUE-19.md` — historical (v1 hard-exit, v2 operational continuation). Marked HISTORICAL at top.

## Rules pointer

`CLAUDE.md` (project) + `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` (session memory). Key rules:
- Study Blink first; all tests at 0 diff; no chasing easy wins.
- High-risk multi-commit refactors → worktree (B6 likely small enough for mainline; Option 1 is worktree).
- Mainline merges only after gate verification.
