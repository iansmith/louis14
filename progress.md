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
**Phase 1 (G-TABLE-REL) DONE 2026-04-21.** All 11 primary `position-relative-table-*` tests PASS at 0 px diff. Commits `d174049b` (Part A), `ac2dc780` (Part B), `a8bb41e6` (interim), plus the LastBaseline §10.8.1 fix (pending commit).

### Phase 1 summary
- Part A: `BoxFragmentBuilder.AddChild` computes RelativeOffset for any child whose `Style.GetPosition()` is relative/sticky and whose RelativeOffset is still zero. `SetChildAvailableSize` wired through block/flex/grid/inline/table.
- Part B: positioned row groups emit a section `PhysicalBoxFragment` via a per-section `BoxFragmentBuilder`, added to the main table builder on boundary crossings and at end-of-loop. Non-positioned groups unchanged.
- Inline-block baseline fix: table no longer synthesizes LastBaseline at content-box block-end when no cell has a text baseline; block no longer propagates Baseline-as-LastBaseline. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, a block container has a LastBaseline only if a descendant line box provides one. The enclosing inline-block's §10.8.1 bottom-margin-edge fallback lives at atomic-inline placement (`inline_layout.go`). Without this fix the 2-row table's synthesized baseline (=100) propagated through `<div>` wrappers up to the `.group` inline-block, shifting the line box below it ~4px too high.
- Regression gates: wm 781/781 ✓, CSS2 99/99 ✓.
- Known limitations: 8 `-absolute-child` variants still failing at 1.0–1.7% — abspos descendants in a positioned section/cell. Not Phase 1 scope; tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE.

### Next
Pick the next phase per the attack order: G-CB-CHANGE (3 tests, invalidation-only) or G-DYN-STATIC (6 tests, prerequisite for G-ABS-CENTER + G-HYPO).

## Test Results
| Date | Scope | Pass | Fail | NORUN | Notes |
|------|-------|------|------|-------|-------|
| 2026-04-21 | css-position (TestWPTCSS3Reftests) — baseline | 50 | 54 | 5 | Fresh run post-Phase 5f. Log: `output/baselines/css-position-2026-04-21.log`. |
| 2026-04-21 | css-writing-modes (invariant, post-Part-B) | 781 | 0 | 0 | Gate held post-`ac2dc780`. |
| 2026-04-21 | CSS2 (TestWPTReftests, invariant, post-Part-B) | 99 | 0 | 0 | Gate held post-`ac2dc780`. |
| 2026-04-21 | `position-relative-table-*` (16 Phase 1 primary) | 0 | 16 | 0 | All fail at identical 3099px / 0.6% — downstream text-offset bug (task #4), not G-TABLE-REL. Green box visually correct. |
| 2026-04-21 | `position-relative-table-*` (11 primary, post §10.8.1 fix) | 11 | 0 | 0 | Phase 1 DONE. All `-absolute-child` variants (8) still failing — out of Phase 1 scope. |
| 2026-04-21 | css-writing-modes (post §10.8.1 fix) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | CSS2 (post §10.8.1 fix) | 99 | 0 | 0 | Gate held. |

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
Blink study for G-TABLE-REL is complete. User directed (2026-04-21): do what Blink does — push the `RelativeOffset` check into the shared `BoxFragmentBuilder.AddChild`.

Code audit revealed the fix is **two-part**:
- **Part A — shared AddChild** (Blink's design). Centralize `RelativeOffset` computation in `BoxFragmentBuilder.AddChild`. Remove the duplicated tail blocks from `block_layout.go:929-940`, `flex_layout.go:1821-1832`, `grid_layout.go:395-403`, and 3 sites in `inline_layout.go`. Fixes `tr-*` tests (4).
- **Part B — section fragments**. Today `table_layout.go:1105-1129` buckets thead/tbody/tfoot rows but emits NO section fragment — rows go straight into the table builder. `position: relative` on `<thead>` has nowhere to attach. Blink emits section PhysicalBoxFragments (structural-only, no NGLayoutResult). We must mirror. Fixes `thead-*`/`tbody-*`/`tfoot-*` (12 tests).

**Open question** → **ANSWERED** (2026-04-21 isolated run + debug print):
- td-top: `bla.style.GetPosition()` returns `PositionRelative` for `display: table-cell` and `result.Fragment.RelativeOffset` is correctly set to `(0, 100)`. The green cell box renders at the **correct** shifted position in test.png (verified against ref.png).
- The 3099-pixel diff is text `"You should see a green box above..."` ~5px off vertically below the table. This means the table's total content block-size is slightly off — a row-height / baseline issue, NOT a relative-offset issue.
- Verdict: `td-top` / `td-left` are **NOT** G-TABLE-REL. Revised Phase 1 scope: 4 `tr-*` tests (Part A fixes) + 12 section tests (Part B fixes) = 16 tests. `td-*` moves to G-SINGLETONS (task #4).

### Part A design (ready to implement)
**Blink pattern.** `BoxFragmentBuilder::AddChild` inspects `box_child.Style().GetPosition()`. If relative/sticky, calls `ComputeRelativeOffsetForBoxFragment(box_child, writing_direction, child_available_size_)`. Parent builder owns `child_available_size_`.

**Our mirror.**
1. Add `childAvailableSize LogicalSize` + `SetChildAvailableSize(LogicalSize)` to `BoxFragmentBuilder`.
2. Each layout algorithm calls `builder.SetChildAvailableSize(computedChildCBSize)` before adding children. (For block/flex/grid/inline: this is the parent's content size available to children. For table: same — rowBuilder gets the row's inline-size / indefinite block.)
3. `AddChild` reads `fragment.Style.GetPosition()`; if relative/sticky, computes `RelativeOffset` using `childAvailableSize` as CB and sets on `fragment`.
4. Delete the tail blocks at `block_layout.go:929-940`, `flex_layout.go:1821-1832`, `grid_layout.go:395-403`, plus three inline sites (`inline_layout.go:1122/1286/1401`).

**Risk.** Removing 7 tail-block sites in one go risks regressing wm (781/781) and CSS2 (99/99). Mitigation: land Part A as one commit with *both* the centralization and the deletion, run full wm + CSS2 regression in a single gate. If anything regresses, the check is symmetric with the deleted sites so debugging is local.

### Part B design (ready to implement after Part A)
Mirror Blink's table section fragments. Today `table_layout.go:1105-1129` concatenates thead/body/footer rows into one flat list; we must instead emit one `sectionBuilder` per group (thead, each tbody, tfoot), each holding the rows of that group, then addChild the section fragment to the table builder. Blink treats these as "structural-only" — they still carry a Style and are real PhysicalBoxFragments, they just have no per-section layout algorithm.

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
