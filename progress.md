# Progress Log — css-position category

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and in auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not duplicate them here.

## Archived wm work
Phase 5f of the css-writing-modes effort is complete (commit `9913a9e4`, 2026-04-21). All 781 wm tests now PASS at 0 pixel diff. The full session history — phases 0 through 7 plus foundational Groups A/B/C — has been archived to:

- `docs/plan-wm.md`
- `docs/findings-wm.md`
- `docs/progress-wm.md`

Do not copy old wm content back into this file. If a wm regression is discovered during css-position work, link to the relevant archived section rather than duplicating.

## Current Phase
**Phase 0 complete** (baseline + groupings + Blink research + NORUN triage). **Phase 1 (G-TABLE-REL) next.**

## Test Results
| Date | Scope | Pass | Fail | NORUN | Notes |
|------|-------|------|------|-------|-------|
| 2026-04-21 | css-position (TestWPTCSS3Reftests) — baseline | 50 | 54 | 5 | Fresh run post-Phase 5f. Log: `output/baselines/css-position-2026-04-21.log`. |
| 2026-04-21 | css-writing-modes (invariant) | 781 | 0 | 0 | Phase 5f complete. |

## Invariants (must stay green)
| Category | Count | Last verified |
|---|---|---|
| css-writing-modes | 781/781 | 2026-04-21 (post-9913a9e4) |
| CSS2 (TestWPTReftests) | 99/99 | 2026-04-21 (pre-Phase-1 baseline implicit) |
| css-flexbox | ≥621/629 | 2026-04-20 (stale — re-verify after Phase 1) |

## Session: 2026-04-21

### Phase 0: Baseline & grouping — **DONE**
- Fresh css-position baseline: 50 PASS / 54 FAIL / 5 NORUN (no change from stale 2026-04-20 baseline — css-position was not improved by any of the §9 recovery / wm fixes, despite flex+position combined showing +5 earlier).
- Grouped 54 failures + 5 NORUN into 11 clusters by shared root-cause hypothesis (see `findings.md`).
- Largest cluster: **G-TABLE-REL (16 tests)** — `table_layout.go` has no relative-position branch; `block/flex/grid/inline_layout.go` all do. This is the cleanest single-root-cause unlock.
- Tracking docs restructured: wm content archived to `docs/plan-wm.md` / `docs/findings-wm.md` / `docs/progress-wm.md`; top-level `task_plan.md` / `findings.md` / `progress.md` now focus exclusively on css-position.

### Phase 0b: Blink research + NORUN triage — **DONE 2026-04-21**

**Blink research completed for 7 of 10 groups.** Findings written per-group in `findings.md`; summary:
- **G-TABLE-REL:** Relative offset is applied in `BoxFragmentBuilder::AddChild` via `ComputeRelativeOffsetForBoxFragment`. Fragment-builder-level design — tables inherit for free in Blink. Our mirror: push the check into our shared `AddChild` equivalent.
- **G-CB-CHANGE:** Invalidation-only. `StyleDifference::NeedsPositionedLayout` + `LayoutBlock::RemovePositionedObjects(stay_within)` in `layout_object.cc` / `layout_block.cc`.
- **G-DYN-STATIC:** Static position NOT cached. Rebuilt each pass via `LayoutResult::OutOfFlowPositionedDescendants` list. Prerequisite for G-ABS-CENTER / G-HYPO.
- **G-ABS-CENTER:** `absolute_utils.cc` IMCB machinery (`ComputeUnclampedIMCBInOneAxis`, `ResizeIMCBInOneAxis`, alignment inset bias).
- **G-HYPO:** IS the both-insets-auto branch of IMCB — shares all machinery with G-ABS-CENTER. May pass for free once G-ABS-CENTER lands.
- **G-STICKY:** Layout-time constraints + scroll-time offset via `StickyPositionScrollingConstraints`. Minimum viable fix: zero offset when natural flow satisfies threshold (true for `sticky-top-001` at scroll=0).
- **G-ABS-IN-INLINE:** `inline_containing_block_utils.cc` — union of first line-box + last line-box fragment rects is the inline CB for abspos children.

**Deferred Blink research** (will be done at phase start): G-ROOT-FLEX-GRID, G-FIXED, G-REPLACED.

**NORUN triage** (`findings.md` "NORUN triage — DONE"):
- 4 SKIPs (runner "no usable reference files found") — infra/JS gaps, out of scope for this layout plan.
- 1 real FAIL miscounted as NORUN (`position-change.html`, HTML parser error). Moves into G-SINGLETONS.
- Revised target: **100/100 runnable**, not 104 (4 SKIPs would need harness + JS-engine work).

**Plan restructuring:**
- Attack order: G-TABLE-REL → G-CB-CHANGE → G-DYN-STATIC → G-ABS-CENTER+G-HYPO → … (G-DYN-STATIC now precedes IMCB work).
- G-ABS-CENTER + G-HYPO bundled into one phase (shared IMCB).

### Phase 1 preparation
Blink study for G-TABLE-REL is complete. Ready to code: push the `RelativeOffset` check into the shared `AddChild` (preferred) or patch `table_layout.go:685` and `:735` directly.

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | css-position category, Phase 0 + Phase 0b done (Blink research + NORUN triage). Phase 1 (G-TABLE-REL, 16 tests) next. |
| Where am I going? | 100/100 runnable css-position at 0 diff (4 SKIPs out of scope for layout plan). |
| What's the goal? | All runnable css-position tests at 0 diff; wm 781/781 and CSS2 99/99 must hold. |
| What have I learned? | Blink applies relative offsets at `BoxFragmentBuilder::AddChild` — fragment-builder-level, uniform across display types. Our mirror should push the check down into the shared AddChild. IMCB machinery in `absolute_utils.cc` is shared between G-ABS-CENTER and G-HYPO. Static position is never cached in Blink. G-CB-CHANGE is invalidation-only. |
| What have I done? | Phase 5f (wm) complete. Fresh css-position baseline captured. Failures grouped into 11 clusters. Attack order set. Tracking docs restructured. Blink research completed for 7 of 10 groups. NORUN triage done. |

## Error Log
*(populated as work progresses)*

---
*Update after each phase, after each milestone, or when a regression is discovered.*
