# CONTINUE: css-multicol next-step

Active operational continuation. Concise pointer for the next session.

## Where we are

Mainline `fix/flexbox-fast` post-Phase-20 merge. Phase 20 (multicol overflow clip Blink-aligned port) landed via `--no-ff` from `phase-20-overflow-clip` worktree (7 commits P20.1–P20.7).

- Multicol gate: **211/455** (+6 from prior 205, hits Phase 20 brief's minimum target).
- 13 driver invariants: **13/13** at 0 diff.
- Spanner-fragmentation cluster: **12/13** (-008 fails at 0.2%, pre-existing; outside the 13-driver subset).
- 4-category invariants: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (1499/6720, 16 known-fails).

**Phase 20 outcome (2026-04-28):** structural Blink port. Replaced the ad-hoc `ClipContentToBorderBox`-on-multicol mechanism (`3389efe7`) with the proper machinery: `BoxType` enum on `PhysicalFragment`, `BoxTypeColumn` tagging in MLA, `IsMulticolContainer` flag + paint-side padding-box overflow clip (mirrors Blink `LayoutBox::OverflowClipRect`), `drawColumnRules` derives rule extents from column fragmentainers, atomic-inline lines + floats with `break-inside:avoid` propagate as `TallestUnbreakable`. Reclaims: `multicol-fill-balance-032/034/035/036`, `multicol-span-all-margin-nested-001`, `inline-block-and-column-span-all`. All 9 prior clip-wins held.

## Next options (operator pick one)

### Phase 20 follow-up — gate `IsMulticolContainer` clip on user-set overflow

P20.5's clip is unconditional but Blink's `LayoutBox::UpdateFromStyle` only applies it when `!IsOverflowVisibleAlongBothAxes() || ShouldApplyPaintContainment()`. Three known-stuck tests share this root cause:

- `multicol-gap-large-001` (0.3%) — content expected to overflow visibly past multicol's right edge.
- `increase-prev-sibling-height` (80 px) — glyph ascenders cut by clip.
- `multicol-nested-029` (85 px) — same in nested multicol with `line-height:0.8`.

Trade-off: gating the clip on `overflow != visible` would regress the 9 prior-clip-wins (which currently rely on the clip masking layout bugs in nested-multicol resume / spanner overflow). Pre-requisite work: `ConsumedBlockSize` chain (below) + spanner-overflow placement, then unconditionally remove the clip.

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

### Option 1 — Finish FinishFragmentation port (larger)

Drop the leaf-only gate in 16.d.1 + delete or shrink the parent-side children-loop overflow path in `block_layout.go:1001-1196`. The merged walker port should clean up the prior break-token misalignment that blocked this earlier. Worktree work.

### Phase 19 — span-all-children-height 002-013

12 tests, 7 sub-clusters. Brief: `findings.md` § Phase 19.

## Tracking files

- `progress.md` — current gate + Active phase narrative.
- `task_plan.md` — phase ordering + queued work.
- `findings.md` — authoritative briefs + chronological error log.
- `CONTINUE-20.md` — HISTORICAL (Phase 20 landed 2026-04-28).
- `CONTINUE-18.md`, `CONTINUE-19.md` — historical.

## Rules pointer

`CLAUDE.md` (project) + `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` (session memory). Key rules:
- Study Blink first; all tests at 0 diff; no chasing easy wins.
- High-risk multi-commit refactors → worktree.
- Mainline merges only after gate verification.
