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
**Phase 3 G-DYN-STATIC — CLOSED 2026-04-21 (all 6/6).** Parts (a)+(b)+(d) landed earlier; Part (c) (`table-cell`) closed 2026-04-21 with two independent fixes.

- **Part (a) `inline_layout.go`** splits `InlineItemOutOfFlow` capture by specified display: inline-level → `(inlinePos, blockOffset)`; block-level with prior in-flow on the line → `(0, blockOffset + lineHeight)` at end-of-line; block-level with no prior in-flow → `(0, blockOffset)` immediately. Mirrors Blink's `InlineLayoutAlgorithm::HandleOutOfFlowPositioned` reading `line_box_.LineBoxBlockEnd()` at time-of-encounter. New helper `isInlineLevelDisplay` mirrors `ComputedStyle::IsOriginalDisplayInlineType`. Closes `inline` (2.1% → 0%). Commit `233d408f`.
- **Part (b) `block_layout.go`** detects inline-level abspos (`isInlineLevelDisplay(childStyle.GetDisplay())`) and queries `exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)`; uses the returned inline-start offset directly as `InlineOffset`. Closes `floats-001/002/003/004`. Commit `d250c5cf`.
- **Part (c) `block_layout.go` + `pkg/css/style.go`** — orphan `display:table-cell` vertical-align shift applied to both in-flow children and OOF candidates when `space.TableSectionData == nil`; transform parser swapped from sign-sentinel percent encoding to explicit `IsPercent []bool`. Closes `table-cell` (2.1% → 0%); bonus +9 css-transforms. Commit `5399d328`.
- **Part (d) RTL** closed incidentally by (b) — `ExclusionSpace` already uses `PhysicalFloatToExclusionSide`-normalised sides, so `FindAvailableInlineSize` is direction-agnostic. `floats-004` (RTL) passes without any dedicated RTL capture logic.

Gates: wm 781/781 ✓, CSS2 99/99 ✓ after both (a) and (b). (a)'s `hasInflowOnLine` refinement was necessary to avoid a 4-test regression in orthogonal-float wm tests whose reference HTML places a block-level abspos as the first child of an inline FC.

**Phase 5 G-FIXED Part A — OOF resolver re-entrance landed 2026-04-21.** `OutOfFlowLayoutPart.LayoutCandidates` rewritten as a worklist loop (mirroring Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`), now consumes `childResult.PropagatedOOFCandidates` from each laid-out OOF candidate. Added `resolvesFixed bool` on the part to select ICB / containment / transform CB sites that absorb fixed; ordinary positioned sites return unresolved fixed to caller for further propagation. Updated all 7 call sites (block, flex, grid, multicol, table). Closes `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0% PASS); reduces `position-fixed-scroll-nested-fixed` from 4.2% → 1.0% (residual is paint-clip / scrollTop, deferred to G-SCROLL). Net: css-position **62 PASS / 42 FAIL** (was 50/54 at the 2026-04-21 baseline). wm 781/781 ✓, CSS2 ✓, flexbox 626/629 ✓ (no regression).

**Phase 2 (G-CB-CHANGE) closed 2026-04-21 as a no-op — group dissolved.** Audit found that our test harness already re-layouts from scratch after JS, so Blink's `RemovePositionedObjects` invalidation pattern doesn't apply. The 3 tests fail for unrelated foundational reasons and have been re-grouped (see findings.md "G-CB-CHANGE — Phase 2 audit invalidated"):
- `absolute-pos-box-inside-fixed-pos-box-with-changing-height` → G-FIXED **(closed by Phase 5 Part A)**
- `containing-block-change-button` → G-SINGLETONS (button vertical-centering)
- `containing-block-change-scrollframe` → new G-SCROLL (needs `Element.scrollTop` + overflow:hidden scroll paint)

**Phase 1 (G-TABLE-REL) DONE 2026-04-21.** All 11 primary `position-relative-table-*` tests PASS at 0 px diff. Commits `d174049b` (Part A), `ac2dc780` (Part B), `b6ec7d3f` (§10.8.1 fix).

### Phase 1 summary
- Part A: `BoxFragmentBuilder.AddChild` computes RelativeOffset for any child whose `Style.GetPosition()` is relative/sticky and whose RelativeOffset is still zero. `SetChildAvailableSize` wired through block/flex/grid/inline/table.
- Part B: positioned row groups emit a section `PhysicalBoxFragment` via a per-section `BoxFragmentBuilder`, added to the main table builder on boundary crossings and at end-of-loop. Non-positioned groups unchanged.
- Inline-block baseline fix: table no longer synthesizes LastBaseline at content-box block-end when no cell has a text baseline; block no longer propagates Baseline-as-LastBaseline. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, a block container has a LastBaseline only if a descendant line box provides one. The enclosing inline-block's §10.8.1 bottom-margin-edge fallback lives at atomic-inline placement (`inline_layout.go`). Without this fix the 2-row table's synthesized baseline (=100) propagated through `<div>` wrappers up to the `.group` inline-block, shifting the line box below it ~4px too high.
- Regression gates: wm 781/781 ✓, CSS2 99/99 ✓.
- Known limitations: 8 `-absolute-child` variants still failing at 1.0–1.7% — abspos descendants in a positioned section/cell. Not Phase 1 scope; tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE.

### Next
**G-DYN-STATIC fully closed.** IMCB (Phase 4, G-ABS-CENTER + G-HYPO bundled) is now unblocked — static-position inputs are Blink-faithful across all FCs.

**G-FIXED Part B residual + adjacent groups** still outstanding. `position-fixed-scroll-nested-fixed` still fails at 1.0% — the inner fixed paints but is clipped by the outer `overflow:auto` and lacks `Element.scrollTop` honoring. Both belong to scroll/paint, not OOF layout. Defer until G-SCROLL is opened.

Adjacent verifications run earlier: 8 `position-relative-table-*-absolute-child` tests are still at 1.0% — different root cause (G-ABS-IN-INLINE / G-ABS-IN-TABLE). 4 `position-{fixed,absolute}-root-element-{flex,grid}` tests also still 0.8% — distinct G-ROOT-FLEX-GRID issue.

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
| 2026-04-21 | css-position (post OOF re-entrance fix) | 62 | 42 | — | +12 vs baseline. `absolute-pos-box-inside-fixed-pos-box-with-changing-height` 0.5% → 0% PASS. `position-fixed-scroll-nested-fixed` 4.2% → 1.0% (paint-clip residual). |
| 2026-04-21 | css-writing-modes (post OOF re-entrance fix) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | CSS2 (post OOF re-entrance fix) | 99 | 0 | 0 | Gate held. |
| 2026-04-21 | css-flexbox (post OOF re-entrance fix) | 626 | 3 | 0 | No regression vs ≥621 baseline; 3 unrelated pre-existing failures. |
| 2026-04-21 | css-writing-modes (post Phase 3(a)) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | CSS2 (post Phase 3(a)) | 99 | 0 | 0 | Gate held. |
| 2026-04-21 | `position-absolute-dynamic-static-position-*` (10 tests, post Phase 3(a)) | 5 | 5 | 0 | `inline` now PASS. Remaining: 3× `floats-00{1,2,3}` → Part (b), `floats-004` → Part (d), `table-cell` → Part (c). |
| 2026-04-21 | css-writing-modes (post Phase 3(b)) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | CSS2 (post Phase 3(b)) | 99 | 0 | 0 | Gate held. |
| 2026-04-21 | `position-absolute-dynamic-static-position-*` (10 tests, post Phase 3(b)+(d)) | 9 | 1 | 0 | +4 from (b): floats-001/002/003/004. Only `table-cell` (2.1%) remains. |
| 2026-04-21 | `position-absolute-dynamic-static-position-table-cell` (post Phase 3(c)) | 1 | 0 | 0 | 0 diff, max 0. G-DYN-STATIC 6/6 closed. |
| 2026-04-21 | css-writing-modes (post Phase 3(c)) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | css-position (post Phase 3(c)) | 68 | 36 | — | +1 vs 62 baseline (target test). |
| 2026-04-21 | css-transforms (post Phase 3(c) transform parser fix) | 171 | 210 | — | +9 vs 162 baseline (percent-sentinel fix unlocks other translate cases). |
| 2026-04-21 | css-flexbox (post Phase 3(c)) | 626 | 3 | 0 | No regression. Same 3 pre-existing failures. |
| 2026-04-21 | css-position (post Phase 4 Commit 2 IMCB wire-up) | 74 | 30 | — | +6 vs 68. Closes 4 G-ABS-CENTER (001/003/004/006) + 2 G-HYPO (hypothetical-dynamic-change-001/002). Residual: center-002, center-007, hypothetical-003 → Commit 3. |
| 2026-04-21 | css-writing-modes (post Commit 2) | 781 | 0 | 0 | Gate held. |
| 2026-04-21 | CSS2 (post Commit 2) | 99 | 0 | 0 | Gate held. |
| 2026-04-21 | css-flexbox (post Commit 2) | 626 | 3 | 0 | Same 3 pre-existing; no regression. |

## Invariants (must stay green)
| Category | Count | Last verified |
|---|---|---|
| css-writing-modes | 781/781 | 2026-04-21 (post Phase 3(c)) |
| CSS2 (TestWPTReftests) | 99/99 | 2026-04-21 (post Phase 3(b)) |
| css-flexbox | 626/629 | 2026-04-21 (post Phase 3(c)) |
| css-transforms (watch, not invariant) | 171/381 | 2026-04-21 (post Phase 3(c), +9 vs baseline) |

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

## Session: 2026-04-21 (OOF re-entrance)

### Phase 2 audit + dissolution
Audit (`pkg/visualtest/helpers.go:85-102`) showed our harness already runs `engine2 := layout.NewLayoutEngine(...)` from-scratch on the post-JS DOM — moral equivalent of Blink's `RemovePositionedObjects` + relayout. JS mutations land correctly. Group dissolved; tests reassigned by actual root cause:
- `absolute-pos-box-inside-fixed-pos-box-with-changing-height` → G-FIXED
- `containing-block-change-button` → G-SINGLETONS (`<button>` vertical-centering)
- `containing-block-change-scrollframe` → new G-SCROLL

### Phase 5 G-FIXED scoping
Read `pkg/layout/out_of_flow_layout.go` end-to-end. Confirmed bug at line 177: `childResult := layoutElement(...)` → `builder.AddChild(...)` with no handling of `childResult.PropagatedOOFCandidates`. Verified block/flex/grid/multicol/table propagation is correct in their respective formatting contexts; the hole is exclusively in the OOF resolver. Both G-FIXED tests share this root cause.

### Phase 5 G-FIXED Part A — OOF resolver re-entrance (commit `ed16475f`)
Mirrored Blink's `OutOfFlowLayoutPart::LayoutOOFNodes` worklist pattern:
- Wrapped per-candidate iteration in a worklist loop. After each `layoutElement(child)` and `builder.AddChild(...)`, drained `childResult.PropagatedOOFCandidates`.
- Added `resolvesFixed bool` field on `OutOfFlowLayoutPart`. ICB / containment-CB / transform-CB sites set it true (descendants are appended to worklist for resolution by this CB). Ordinary positioned sites set it false (descendants are returned to caller as unresolved fixed for further propagation).
- Changed `LayoutCandidates` return type to `[]OutOfFlowCandidate`.
- Updated all 7 call sites (block × 3, flex, grid × 2, multicol, table). Positioned-only callers append the return value into their `propagatedOOF` list.

### Verification
- `absolute-pos-box-inside-fixed-pos-box-with-changing-height`: 0.5% → **0% PASS**.
- `position-fixed-scroll-nested-fixed`: 4.2% → 1.0% (still failing). Inner fixed now paints, but is being clipped by outer `overflow:auto` and lacks `Element.scrollTop=200` honoring. Both are paint/scroll territory, not OOF layout. Deferred to G-SCROLL / paint-time work (G-FIXED Part B).
- Adjacent sweep ruled out (different root causes): 8 `position-relative-table-*-absolute-child` still 1.0% (G-ABS-IN-INLINE/TABLE); 4 `position-{fixed,absolute}-root-element-{flex,grid}` still 0.8% (G-ROOT-FLEX-GRID).
- Full css-position: 50 → 62 PASS (+12). The +12 comprises the 1 G-FIXED close plus 11 carried over from Phase 1 commits since the 2026-04-21 baseline (10 `position-relative-table-*` primary + `position-relative-012`).

### Gates held
- wm 781/781 ✓
- CSS2 99/99 ✓
- flexbox 626/629 ✓ (no regression vs ≥621 baseline; 3 unrelated pre-existing failures: `auto-margins-001`, `content-height-with-scrollbars`, `flexbox-align-self-vert-004`)

### Next
Phase 3 **G-DYN-STATIC** (6 tests). Foundational: rebuild static position every pass via `OutOfFlowPositionedDescendants` list on `LayoutResult`. Prerequisite for Phase 4 (IMCB / G-ABS-CENTER + G-HYPO).

### Phase 3 audit (2026-04-21) — planned hypothesis INVALIDATED
Instrumented the `inline` test (`position-absolute-dynamic-static-position-inline`, 2.1%) and confirmed:
- `helpers.go:85-102` already uses fresh `engine2` on post-JS DOM. No static-position caching.
- JS mutation `target.style.display='block'` DOES reach the 2nd layout pass: post-JS `ComputedStyles()[target].GetDisplay()` returns `block`.
- Yet test renders target beside inline-block (display-inline static position) rather than below (display-block static position).
- Diagnosis: the 2nd pass correctly sees `display:block`, but `inline_layout.go:682-694` captures static as `(inlinePos, blockOffset)` regardless of whether the abspos child is inline-level or block-level.

Additional ocular proof from `floats-001` test.png: target (40×80 green) is placed at CB content-origin `(0, 0)`, overlapping the float, instead of `(40, 0)` beside the float. Confirms `block_layout.go:226` hardcodes `InlineOffset: 0` without float awareness.

Revised Phase 3: **no `LayoutResult` schema change** (we already rebuild every pass). Instead, 4 per-formatting-context point fixes mirroring Blink's per-FC OOF handling:
1. `inline_layout.go:682-694` and `:497-509`: split by `display` — block-level abspos → `(0, lineBlockEnd)`; inline-level → `(inlinePos, lineBlockStart)`.
2. `block_layout.go:217-237`: for inline-level abspos, compute float-aware `InlineOffset` from the exclusion space at `blockCursor`.
3. `table_layout.go` / table-cell path: apply vertical-align to static-position block-offset.
4. RTL direction awareness on capture (floats-004).

See `findings.md` "G-DYN-STATIC — 6 tests — Phase 3 hypothesis invalidated" for detail.

Paused before coding to let the user confirm the revised approach.

### Phase 3 (a) inline_layout.go — DONE 2026-04-21, commit `233d408f`
- Added `isInlineLevelDisplay` helper mirroring `ComputedStyle::IsOriginalDisplayInlineType`.
- Capture loop now splits on specified `display`; inline-level OOF emits at `(inlinePos, blockOffset)` immediately; block-level OOF with preceding in-flow on the line is deferred and emitted at `(0, blockOffset + lineHeight)` after `createLineBoxEx` finalises line height; block-level OOF with no preceding in-flow emits immediately at `(0, blockOffset)`.
- Refinement that avoided a 4-test wm regression: initial attempt emitted ALL block-level OOFs at `(0, blockOffset + lineHeight)`. That broke `float-{lft,rgt}-orthog-v{lr,rl}-in-htb-002/003` whose REFERENCE HTML has a block-level `position:absolute` div as the first child of an inline FC. Blink reads `line_box_.LineBoxBlockEnd()` at time-of-encounter (not end-of-line), so the `hasInflowOnLine` flag must gate the deferred path.
- `inline` test closed (2.1% → 0%). wm 781/781 ✓, CSS2 99/99 ✓.

### Phase 3 (c) table-cell — DONE 2026-04-21, commit `5399d328`
Target test `position-absolute-dynamic-static-position-table-cell` (2.1% → 0%). Investigation revealed the original plan's fix site was wrong: this test uses **orphan** `display:table-cell` (no `<table>` ancestor), which bypasses `table_layout.go` entirely. `normalizeTableSubtrees` in `layout_tree_builder.go` doesn't wrap it (reverse §17.2.1 anonymous-table generation is unimplemented), so the cell falls through to `block_layout.go`.

Instrumentation confirmed the static-position capture at the cell was correct (block-offset = 50) once vertical-align was honoured. Pixel-scanner output then pointed at a separate transform rendering issue.

**Two fixes landed:**
1. **Orphan-cell vertical-align (`pkg/layout/block_layout.go`).** After layout, if `style.GetDisplay() == DisplayTableCell && space.TableSectionData == nil && finalBlockSize > intrinsicBlockSize`, compute the `vertical-align` shift (`middle` → half surplus, `bottom` → full surplus) and apply it to both in-flow children and propagated OOF candidates. `TableSectionData == nil` keeps the proper-table path unaffected.
2. **Transform parser percent-sentinel (`pkg/css/style.go` + `pkg/render/paint_layer.go`).** Removed the sign-sentinel encoding (`result := -percent`) that collided with legitimate negative pixel lengths. Added `IsPercent []bool` on `Transform`; widened `GetIndividualTranslate` to return explicit percent flags. Updated 3 `louis13/` callers for the new signature.

**Investigated but dropped:** an edit to `table_layout.go` (pre-stretch `contentBlockSize` + OOF-candidate `vaBlockShift` for the proper-table path). Not needed for the target test, and the `contentBlockSize` change regressed 3 wm orthogonal-writing-mode tests (`box-offsets-rel-pos-vlr-005`, `box-offsets-rel-pos-vrl-004`, `orthogonal-cell-001`). The structural design is correct but the `contentBlockSize` shape needs re-debugging against orthogonal cases before it can land. Filed as tech debt.

Gates: wm 781/781 ✓, css-position 67 → 68 (+1), css-transforms 162 → 171 (+9 free wins from percent fix), css-flexbox 626/629 ✓.

### Phase 3 (b) block_layout.go + (d) RTL — DONE 2026-04-21, commit `d250c5cf`
- Block-FC abspos now queries `exclusionSpace.FindAvailableInlineSize(bfcBlockOrigin + staticBlockOffset, 0, bfcContainerInlineSize)` when the abspos child is inline-level per its specified display; the returned inline-start consumption is used directly as `InlineOffset`.
- Debugging surfaced an **ExclusionSpace coordinate-system invariant** not documented in the code: floats are stored with LOCAL inline offsets (from the enclosing block's content-box inline-start), not BFC-absolute. First attempt subtracted `bfcInlineOrigin` from the query result and landed the target at local `(22, 0)` instead of `(40, 0)`. Removed the subtraction. Full write-up in `findings.md` "Coordinate-system notes".
- RTL (`floats-004`) closed INCIDENTALLY: `ExclusionSpace` normalises physical sides through `PhysicalFloatToExclusionSide` at write time, so `FindAvailableInlineSize` is direction-agnostic. No separate (d) change needed.
- Closes `floats-001/002/003` and `floats-004` (RTL). wm 781/781 ✓, CSS2 99/99 ✓.

### Session learnings (2026-04-21, distilled)
1. **Per-FC capture, not cache invalidation.** `G-DYN-STATIC` tests aren't about re-layout; they're about each FC's static-position computation matching Blink's per-FC logic.
2. **"At time of encounter" metrics matter.** Inline FC reads `LineBoxBlockEnd()` when an OOF is encountered, not at end-of-line. Incremental tracking (`hasInflowOnLine`) is the right primitive.
3. **ExclusionSpace stores LOCAL inline offsets.** Readers must use returned offsets directly; don't translate by `bfcInlineOrigin`. The in-flow inline-layout line-start code happens to add-then-subtract origin for clarity; new readers should skip both steps.
4. **RTL often free via push-down normalisation.** `PhysicalFloatToExclusionSide` already flips at write, so readers don't need a direction branch.

### Phase 4 Commit 1 — pure IMCB module (absolute_utils.go) — DONE 2026-04-21, commit `a3c8db38`
Ported Blink's `absolute_utils.cc` IMCB machinery at type/function parity. New `pkg/layout/absolute_utils.go` (612 lines) contains:
- **Types (Blink-named):** `InsetBias` (BiasStart/End/Equal), `ItemPosition` (normal/auto/start/end/center/self-start/self-end/flex-start/flex-end/stretch/baseline/last-baseline/left/right), `AlignmentData`, `LogicalAlignment`, `LogicalOofInsets` (+ `LogicalInsets.AsOofInsets()` helper), `InsetModifiedContainingBlock` (with `InlineSize()/BlockSize()/Size()` methods + has-auto-inset flags + safe/default biases + default-overflow flags), `LogicalOofDimensions`.
- **Functions (Blink-named):** `GetAlignmentInsetBias`, `axesOppose`, `ComputeUnclampedIMCBInOneAxis` (three branches including center-clipping collapse `2 × min(static, cb − static)`), `ResizeIMCBInOneAxis`, `ComputeUnclampedIMCB`, `BiasFromStaticEdge`, `ComputeMargins`, `ComputeInsets`.
- `ComputeOofInlineDimensions` / `ComputeOofBlockDimensions` deferred to Commit 2 (need layout-engine context).

`absolute_utils_test.go`: 16 unit cases — three IMCB branches (start/end/center-clipping/symmetric/perfectly-centered), ResizeIMCB (start/end/equal/negative), ComputeMargins (both-auto positive/negative, one-auto, has-auto-inset gate), GetAlignmentInsetBias (core + safe), ComputeInsets (center, auto-margin passthrough, end-overflow fallback). **All 16 pass.**

Gates: no existing callers modified; no integration with layout engine yet. wm / CSS2 / flex all unchanged (module is dead code pending Commit 2). Pre-existing `TestBlockLayout_FloatLeft` failure in `exclusion_space_test.go` is unrelated (confirmed by running before `absolute_utils.go` was created).

### Phase 4 Commit 2 — wire OOF resolver with IMCB (in flight 2026-04-21)
Rewrote `OutOfFlowLayoutPart.layoutCandidatesOnce` to use the IMCB module from Commit 1:
- Static position shifted into CB-padding-box on input and back to CB-content-box on output.
- Pre-layout fixed-size when both insets specified + auto size; otherwise pass through and let the child size itself.
- Post-layout resolution via `ComputeMargins` + `ComputeInsets`, reading IMCB's default / safe / alignment biases.
- `cbBlock != Indefinite` guard — block axis falls back to simple per-case formulas when IMCB math isn't meaningful.
- `OutOfFlowCandidate.Alignment LogicalAlignment` added (zero-value BiasStart → backwards-compatible).
- Flex OOF static-edge derived from parent's `justify-content` / `align-items` via new `flexOOFStaticMain` / `flexOOFStaticCross` helpers.
- Added `defaultInsetBias = BiasStart` in `absolute_utils.go`'s both-auto BiasEqual branch (Blink-parity: overflow-centered abspos snaps to start).
- `ComputeUnclampedIMCB` now propagates a static-center overflow flag so the default-overflow fallback fires for uncentered statics too.
- Propagated OOF candidates from a laid-out OOF ancestor get their static positions translated from the ancestor's content-box into the CB's content-box (new drain at end of per-candidate loop, with cross-WM physical round-trip). Mirrors `block_layout.go` `PropagateOOFCandidates`.

**Verification:** css-position **68 → 74** (+6 of 8 targets closed). Closed: `position-absolute-center-001/003/004/006` (G-ABS-CENTER), `hypothetical-dynamic-change-001/002` (G-HYPO). Residual (3) pushed to Commit 3 — see `task_plan.md` Phase 4 Commit 2 entry.

**Gates:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (0 regression). Pre-existing `TestBlockLayout_FloatLeft` unit test failure confirmed unrelated (stashed + reproduced).

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | css-position category, **74/100 runnable PASS**. Phase 1 (G-TABLE-REL) DONE; Phase 2 dissolved; Phase 3 (G-DYN-STATIC) DONE; Phase 5 G-FIXED Part A DONE. **Phase 4 Commit 1 landed (`a3c8db38`). Phase 4 Commit 2 (IMCB wire-up + flex alignment + propagated-OOF translation) staged** — closes 6 of 8 targets (001/003/004/006, hypothetical-001/002). Remaining 3 residuals → Commit 3. |
| Where am I going? | 100/100 runnable css-position at 0 diff (4 SKIPs out of scope for layout plan). |
| What's the goal? | All runnable css-position tests at 0 diff; wm 781/781, CSS2 99/99, flex ≥621 must hold. |
| What have I learned? | Relative offsets belong at `BoxFragmentBuilder.AddChild` (shared across display types). Per §10.8.1 / Blink's `LayoutBox::LastBaselineForInlineBlock`, a block's LastBaseline must originate from a line-box descendant. IMCB machinery in `absolute_utils.cc` is shared between G-ABS-CENTER and G-HYPO. Static position is never cached in Blink. G-CB-CHANGE is invalidation-only and turned out to be a no-op for our harness (we already do fresh re-layout post-JS). **OOF resolution must be re-entrant** (Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`): after laying out an OOF child, drain `PropagatedOOFCandidates` and continue resolving. ICB / containment / transform CB sites absorb fixed; ordinary positioned sites return unresolved fixed to caller. **Orphan `display:table-cell` bypasses `table_layout.go`** — falls through to `block_layout.go` via unimplemented reverse §17.2.1 anonymous-table generation; needs its own vertical-align handling at the block-layout site. **Transform parser must not use sign as a percent/length sentinel** — negative pixel lengths encode negatively and will be misread as percent. Use explicit `IsPercent []bool`. |
| What have I done? | Phase 5f (wm) complete. css-position baseline captured. Failures grouped. Attack order set. Blink research for 7/10 groups. NORUN triage done. **Phase 1 (G-TABLE-REL) closed** — commits `d174049b`, `ac2dc780`, `b6ec7d3f`. **Phase 2 (G-CB-CHANGE) dissolved** as no-op. **Phase 5 G-FIXED Part A closed** — commit `ed16475f`, OOF resolver re-entrance. **Phase 3 G-DYN-STATIC closed (6/6)** — commits `233d408f` (a), `d250c5cf` (b)+(d), `5399d328` (c) landing orphan-cell vertical-align + transform percent-sentinel fix. **Phase 4 Commit 1 landed** — commit `a3c8db38`, pure IMCB module. **Phase 4 Commit 2 staged** — OOF resolver rewritten to use IMCB + `ComputeMargins` + `ComputeInsets`, `OutOfFlowCandidate.Alignment` field added, flex candidate sites now populate `StaticPosition` from `justify-content`/`align-items`, propagated-OOF coordinate translation fix, overflow-centered abspos snaps to start. Closes 6 of 8 Phase 4 targets (4 G-ABS-CENTER + 2 G-HYPO). Net: 50 → 74 PASS. wm 781/781, CSS2 99/99, flex 626/629 all gates held. Bonus +9 in css-transforms from the Phase 3(c) percent-sentinel fix. |

## Error Log
*(populated as work progresses)*

---
*Update after each phase, after each milestone, or when a regression is discovered.*
