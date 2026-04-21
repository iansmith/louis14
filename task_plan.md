# Task Plan: Pass the entire css-position category

## Goal
All 104 tests under `pkg/visualtest/testdata/wpt-css3/css-position/` pass at 0% diff via `TestWPTCSS3Reftests/css-position`. Baseline (2026-04-21): **50 passing, 54 failing, 5 no-run**. Current (2026-04-21 post Phase 6 M6 closed): **83 passing, 22 failing**. Remaining: close 22 without regressing:

- css-writing-modes (currently 781/781 PASS — Phase 5f complete)
- CSS2 (99/99 PASS)
- css-flexbox (626/629 PASS — verified post Phase 3(c))
- css-transforms (watch, not invariant: 171/381 after Phase 3(c) percent-sentinel fix, +9 vs baseline)

## Rules & Discipline
Authoritative sources (re-read at session start):

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index.

If you find yourself about to type a project rule into this file, stop and link instead.

## Archived wm work
The css-writing-modes category is complete. Its planning/findings/progress have been moved into `docs/`:
- `docs/plan-wm.md` — full 787-test plan (Phases 0–7, foundational groupings A/B/C, all done)
- `docs/findings-wm.md` — bucket analysis, Blink entry points, post-mortems
- `docs/progress-wm.md` — session-by-session log through Group C fix

Do not duplicate wm notes here.

## Baseline snapshot (2026-04-21)
Log: `output/baselines/css-position-2026-04-21.log`
- 104 tests exercised: **50 PASS · 54 FAIL · 5 NORUN** at baseline.
- Latest (post Phase 6 M6 closed, commit `01f468d9`): **83 PASS · 22 FAIL** in this category.
- Failing test list + diffs: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`).

Highest-diff outliers (top 5 by pixel count, current state):
| % | px | test | group |
|---|---|------|-------|
| 10.4% | 50000 | `containing-block-change-scrollframe.html` | G-SCROLL (was G-CB-CHANGE) |
|  4.2% | 20000 | `containing-block-change-button.html` | G-SINGLETONS (was G-CB-CHANGE) |
|  4.2% | 20000 | `hypothetical-dynamic-change-003.html` | G-HYPO |
|  3.4% | 16308 | `sticky-top-001.html` | G-STICKY |
|  1.0% |  4672 | `position-fixed-scroll-nested-fixed.html` | G-FIXED residual (paint-clip / scrollTop) |

5 NORUN — **triaged 2026-04-21** (full table in `findings.md`):
- 4 are runner **SKIPs** ("no usable reference files found") — infrastructure gaps, not layout bugs:
  - `hypothetical-box-scroll-parent` (ref file missing from snapshot)
  - `hypothetical-box-scroll-viewport` (missing `window.scrollTo` + ref)
  - `position-absolute-multicol-001` (absolute-path ref unresolved by runner)
  - `replaced-object-backdrop` (`<object popover>` JS unsupported + absolute-path ref)
- 1 is a real **FAIL** miscounted as NORUN because the parser-error log format doesn't match our regex: `position-change.html` — HTML parser bails with `tokenizer error: expected '>' but reached EOF`. Counted as a real failure (moves to G-SINGLETONS).

**Target revision.** True runnable set = 100 tests; true failure count = 55 (54 original + position-change). The 4 SKIPs need harness / JS-engine work and are **out of scope for this plan**. Deliverable: **100/100 runnable at 0 diff** (104 total if the SKIPs are later un-skipped by separate infra work).

## Groups (root-cause-oriented, not %-diff-oriented)
Full detail in `findings.md`. 54 failing tests cluster into **11 groups** by likely shared root cause. Fix one representative test per group; if the root cause is correct, siblings fall out for free.

| # | Group | Count | % range | Estimated shape of root cause |
|---|---|---|---|---|
| 1 | ~~**G-TABLE-REL**~~ — position:relative on table-internal elements (thead/tbody/tfoot/tr/td) — **DONE** | 11 (primary) | 0.4–1.7% | Closed by commits `d174049b`, `ac2dc780`, `b6ec7d3f`. 8 `-absolute-child` variants moved to G-ABS-IN-INLINE / G-ABS-IN-TABLE. |
| 2 | **G-ABS-CENTER** — abspos centering with `margin: auto` + both-axis insets (`css-align-3` abspos sizing) | 5 | 0.3–2.1% | Abspos available-space = 2 × distance(center→closest edge); auto-margin distribution |
| 3 | **G-CB-CHANGE** — dynamic change of containing-block establishment (JS toggle of overflow/button/height) | 3 | 0.5–10.4% | Abspos children need re-resolve to new CB after JS mutation |
| 4 | **G-DYN-STATIC** — dynamic static-position re-layout (JS-triggered property flips affect float/inline/table-cell static pos) | 6 | 0.3–2.1% | Static position rectangle recomputation on relayout |
| 5 | **G-HYPO** — hypothetical position dynamic change + scroll (fixed/abs ancestor moves) | 3+2 NORUN | 2.1–4.2% | `HypotheticalBoxPosition` not recomputed when ancestor offset changes |
| 6 | **G-ROOT-FLEX-GRID** — `<html>` as position:fixed/absolute root with `display: flex|grid` | 4 | 0.8% | Root-element OOF sizing — insets must resolve against ICB even when `display` is flex/grid |
| 7 | **G-FIXED** — nested OOF re-entrance + scroll-clip escape for fixed | 2 (1 closed) | 0.5–4.2% | OOF resolver wasn't re-entrant. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` 2026-04-21. Residual on `position-fixed-scroll-nested-fixed` is paint-clip / scrollTop, not layout. |
| 8 | **G-ABS-IN-INLINE** — abspos whose containing block is an inline (CSS2 §10.1.4) | 2 | 2.3–2.9% | Inline-CB bounding box computation for abspos children |
| 9 | **G-STICKY** — `position: sticky` at scroll=0 must stay in normal flow | 1 | 3.4% | We treat sticky as relative (applies `top:10px` unconditionally); needs scroll-aware algorithm |
| 10 | **G-REPLACED** — abspos replaced elements with no intrinsic size / `max-content` sizing | 1 | 2.1% | CSS 2.2 §10.3.7 / §10.6.5 abs-replaced-width/height |
| 11 | **G-SINGLETONS** — `clear-001` (96px), `position-absolute-dynamic-list-marker` (18px), `stack-floats-001`, `position-absolute-iframe-print-001/002`, `position-relative-011/012/013` (%-top on table rows), plus 3 NORUN (`position-change`, `replaced-object-backdrop`, `position-absolute-multicol-001`) | 11 | 0.0–1.7% | Heterogeneous; attack last |

## Attack order (foundational impact ÷ effort)

Research insights from Blink study (2026-04-21) reshape the ordering — **G-DYN-STATIC is a prerequisite for both G-ABS-CENTER and G-HYPO**, and the IMCB machinery is shared between G-ABS-CENTER and G-HYPO.

1. ~~**G-TABLE-REL (11 primary tests).**~~ **Done 2026-04-21** — commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Relative offset moved into shared `BoxFragmentBuilder.AddChild`; positioned thead/tbody/tfoot emit section fragments; inline-block §10.8.1 last-baseline fallback corrected.
2. ~~**G-CB-CHANGE (3 tests).**~~ **Dissolved 2026-04-21** (audit no-op) — our harness already does fresh relayout post-JS. Tests reassigned to G-FIXED / G-SINGLETONS / G-SCROLL.
3. ~~**G-DYN-STATIC (6 tests).**~~ **Done 2026-04-21** — commits `233d408f` (a), `d250c5cf` (b+d), `5399d328` (c) (orphan-cell vertical-align at `block_layout.go` + transform percent-sentinel fix at `pkg/css/style.go`). Original "rebuild via `OutOfFlowPositionedDescendants` list" hypothesis was invalidated — our harness already relays out fresh; the real bugs were per-FC static-position computation at each capture site.
4. **G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests).** Both depend on `ComputeUnclampedIMCBInOneAxis` / `ResizeIMCBInOneAxis` in a new `pkg/layout/absolute_utils.go`. The hypothetical-box tests *are* the both-insets-auto branch — they may pass for free once IMCB lands. Verify after the IMCB commit and split if needed. **Now unblocked — G-DYN-STATIC prerequisite satisfied.**
5. **G-ROOT-FLEX-GRID + G-FIXED (5 tests).** Blink research **deferred** to phase start — study `layout_view.cc` root-element specials + nested-fixed scroll offset at that point.
6. **G-ABS-IN-INLINE (2 tests).** New `pkg/layout/inline_containing_block.go` mirroring `InlineContainingBlockUtils::ComputeInlineContainerGeometry` — union rects of first + last line-boxes.
7. **G-STICKY (1 test).** Minimum viable: at layout, sticky boxes get zero offset; `sticky-top-001` naturally passes. Full `StickyPositionScrollingConstraints` can wait until a scroll-based sticky test appears.
8. **G-REPLACED (1 test).** Blink research **deferred** to phase start — CSS 2.2 §10.3.7 / §10.6.5 abs-replaced sizing.
9. **G-SINGLETONS (11 tests, includes `position-change`).** Sweep last; some (e.g. `position-relative-011/012/013`) are expected to close when G-TABLE-REL lands.

Each group runs through the same discipline loop (CLAUDE.md §1–§4):
1. **Study Blink.** Read the relevant Blink file (entry points in `findings.md`). No code before this step.
2. **Pick a representative failing test.** Read test + ref HTML; identify the DOM/CSS invariant being exercised.
3. **Instrument.** Use `/tmp/scanimg.go`-style pixel tools when visual delta is ambiguous; stderr-log layout metrics when geometric.
4. **Write the fix.** Narrow as possible. Mirror Blink's type names and algorithm order.
5. **Verify target + regression-adjacent.** Run only the failing group + 1–2 adjacent groups (CLAUDE.md §4).
6. **Regression sweep before commit.** wm full + CSS2 full + flex spot-check.

## Phases

### Phase 0: Baseline — **DONE 2026-04-21**
- [x] Fresh baseline run: `output/baselines/css-position-2026-04-21.log`
- [x] Parse into failing list: `/tmp/css-position-fails.tsv`
- [x] Group by root cause: 11 groups, see `findings.md`
- [x] Triage the 5 NORUN entries → 4 SKIP (infra), 1 real FAIL (`position-change.html`). Details in `findings.md` "NORUN triage".
- [x] Blink research for 7 groups (G-TABLE-REL, G-CB-CHANGE, G-DYN-STATIC, G-ABS-CENTER, G-HYPO, G-STICKY, G-ABS-IN-INLINE) — see `findings.md` per-group "Blink entry points" sections.

### Phase 1: G-TABLE-REL (11 primary tests) — **DONE 2026-04-21**
- [x] Blink research: relative offset is applied in `BoxFragmentBuilder::AddChild` via `ComputeRelativeOffsetForBoxFragment`. Fragment-builder-level, not algorithm-level.
- [x] Decision (2026-04-21, user-directed): do what Blink does — push the check into our shared `BoxFragmentBuilder.AddChild`. Parent's content-box size is the CB for percentage resolution.
- [x] Scope narrowed after isolated test run (2026-04-21): `td-top` / `td-left` diffs were a *baseline* bug, not a RelativeOffset bug. The green cell box was already shifted correctly; residual was a ~4px text-offset below the table.
- [x] Part A — RelativeOffset at shared `AddChild`. Committed `d174049b`. Removed duplicate tail blocks in block/flex/grid/inline layout algorithms.
- [x] Part B — emit section fragments (thead/tbody/tfoot) in `table_layout.go` so positioned sections have a fragment to attach to. Committed `ac2dc780`.
- [x] §10.8.1 fix — inline-block last-baseline. Committed `b6ec7d3f`. Two edits: stop synthesizing table LastBaseline at content-box block-end when no cell has a text baseline, and stop propagating `childResult.Baseline` as `LastBaseline` in block_layout. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, a block has a last-baseline only if a line-box descendant provides one; otherwise the inline-block's §10.8.1 bottom-margin-edge fallback fires at atomic-inline placement.
- [x] Regression sweeps held: wm 781/781, CSS2 99/99.
- [x] Result: all 11 primary `position-relative-table-*` tests pass at 0 px diff. 8 `-absolute-child` variants remain out of scope (tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE).

### Phase 2: G-CB-CHANGE (3 tests) — **plan invalidated 2026-04-21; group dissolved**
- [x] Blink research: `StyleDifference::NeedsPositionedLayout` + `LayoutBlock::RemovePositionedObjects(stay_within)`.
- [x] Audit (2026-04-21): our `pkg/visualtest/helpers.go:85-102` already runs `engine2 := layout.NewLayoutEngine(...)` from-scratch on the post-JS DOM. There is no caching to invalidate. JS mutations land correctly (`fixed.style.height = "300px"` → inline-style attr `"height: 300px"`, pass-2 sees it). The Blink invalidation pattern doesn't apply.
- [x] Per-test triage (see `findings.md` "G-CB-CHANGE — Phase 2 audit invalidated"):
  - `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5%) → **G-FIXED** (positioned-fragment box-tree gap; pos:fixed/absolute boxes absent from box tree).
  - `containing-block-change-button` (4.2%) → **G-SINGLETONS** (`<button>` vertical-centering rendering bug).
  - `containing-block-change-scrollframe` (10.4%) → new **G-SCROLL** sub-group (needs `Element.scrollTop` setter + `overflow:hidden` scroll paint).
- [x] Phase 2 closed as a no-op — no code changes needed for "invalidation". Move on to next phase per revised attack order.

### Phase 3: G-DYN-STATIC (6 tests) — **DONE 2026-04-21**
- [x] Blink research: static position NOT cached; rebuilt each pass via `LayoutResult::OutOfFlowPositionedDescendants` list.
- [x] Audit (2026-04-21): we already rebuild every pass via fresh `engine2`. Original "add OutOfFlowPositionedDescendants list" hypothesis is a no-op. Real root causes are per-FC COMPUTATION bugs in static-position capture sites. See `findings.md` "G-DYN-STATIC — Phase 3 hypothesis invalidated".
- [x] **(a) `inline_layout.go:682-694`** — split by child's `display`. Block-level abspos → `(0, lineBlockEnd)` when in-flow content precedes on the line, `(0, blockOffset)` otherwise; inline-level abspos → `(inlinePos, blockOffset)`. Helper `isInlineLevelDisplay` mirrors Blink's `ComputedStyle::IsOriginalDisplayInlineType`. `hasInflowOnLine` flag mirrors `line_box_.LineBoxBlockEnd()` at time-of-encounter so the first-child-block-level case (no prior in-flow) stays at `blockOffset`. Closes `inline` (2.1% → 0%). wm 781/781 ✓, CSS2 99/99 ✓. Commit `233d408f`.
- [x] **(b) `block_layout.go:217-237`** — for inline-level abspos children, query `exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)` and use the returned inline-start offset directly as `InlineOffset` (no bfcInlineOrigin subtraction — floats are stored with LOCAL inline offsets, matching how inline_layout's line-start recomputation uses them). Closes `floats-001` (0.7% → 0%), `floats-002` (0.3% → 0%), `floats-003` (0.3% → 0%), AND `floats-004` RTL (0.7% → 0%). Turns out (d) is covered by the shared exclusion-space path (`PhysicalFloatToExclusionSide` already flips for RTL; the query is direction-agnostic). wm 781/781 ✓, CSS2 99/99 ✓. Commit `d250c5cf`.
- [x] **(c) orphan table-cell (NOT the originally-planned site).** Investigation: target test uses `display:table-cell` with **no table ancestor**. `normalizeTableSubtrees` in `layout_tree_builder.go` doesn't wrap it (reverse §17.2.1 is unimplemented), so layout dispatches to `block_layout.go`, not `table_layout.go`. Two fixes: (i) orphan-cell vertical-align in `block_layout.go` (applied to in-flow children + OOF candidates, guarded by `space.TableSectionData == nil`); (ii) transform parser percent-sentinel fix in `pkg/css/style.go` + `pkg/render/paint_layer.go` (added `IsPercent []bool` on `Transform`; widened `GetIndividualTranslate` signature to return explicit percent flags; updated 3 `louis13/` callers). Closes `table-cell` (2.1% → 0%) and +9 css-transforms for free. **Uncommitted** pending user review.
- [x] **(d) RTL-direction awareness** — **NO-OP, closed by (b)**. `floats-004` passes because `exclusionSpace.FindAvailableInlineSize` operates on `PhysicalFloatToExclusionSide`-normalised sides (ExclusionInlineStart = visual-start regardless of direction). No separate edge-annotation flip needed on capture.
- [ ] **Tech debt**: proper-table path vertical-align on propagated OOF candidates. Structural design (OOF-candidate `vaBlockShift` during row sweep in `table_layout.go`) drafted but dropped from this phase because the `contentBlockSize` pre-stretch change regressed 3 wm orthogonal-writing-mode tests. Revisit when a test requires vertical-align centering of abspos inside a real `<table><td>`.
- [x] Per-site commits with wm 781/781 + CSS2 99/99 regression gate after each.
- [x] Representative drivers: `inline` (2.1%) for (a); `floats-001` (0.7%) for (b); `table-cell` (2.1%) for (c); `floats-004` (0.7%) for (d).

### Phase 4: G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests) — reframed 2026-04-21 as Blink-parity-first
**Goal.** Port Blink's OOF sizing layer (`absolute_utils.cc`) to louis14 at function/type/algorithm parity. Closing the 8 target tests is a *verification* that the port is correct, not the target. Reframing driven by CLAUDE.md §2 — "study Blink, mirror names and structure" — and the concern that point-fixing the 8 tests would let our OOF sizing code diverge further from Blink before we port it cleanly.

**Blink-parity items our code is missing today** (audit 2026-04-21):
1. `InsetBias` enum (kStart/kEnd/kEqual).
2. `LogicalAlignment` struct (carries `align-self`/`justify-self` to the resolver). `align-self`/`justify-self` are NOT currently consulted for abspos.
3. `InsetModifiedContainingBlock` type (CB size minus insets; used for percentage resolution of the abspos child — today we pass the raw CB size to `SetPercentageResolutionSize`).
4. `LogicalOofDimensions` output struct (inset + size + margins, capturing the resolved rect).
5. Center-clipping collapse in `ComputeUnclampedIMCBInOneAxis` (the `2 × min(static, cb − static)` rule in the both-insets-auto + kEqual branch). Never exercised today because no candidate site sets `StaticEdgeCenter`.

**Existing Blink-parity items** (reuse):
- `LogicalStaticPosition` with `InlineEdge/BlockEdge` (`StaticEdgeStart/Center/End`) — already 1:1 with Blink. `pkg/layout/static_position.go:25-29`.
- `LogicalInsets` with `HasInlineStart/...` flags — close to Blink's `LogicalOofInsets`; we will reuse.
- OOF resolver worklist pattern — mirrored in Phase 5 Part A (`out_of_flow_layout.go:58-77`).
- Both container + child WDM already threaded (`out_of_flow_layout.go:37-39, 89, 103`).

**Explicit scope boundaries — NOT ported in Phase 4** (named now so later Blink-parity work doesn't find hidden gaps):
- Anchor positioning (`LogicalAnchorCenterPosition`, `anchor-center` alignment). No WPT tests in css-position exercise it. Leave `TODO(anchor-positioning)` breadcrumbs where Blink's signatures take anchor params.
- Table-specific IMCB clamp in `ComputeInsetModifiedContainingBlock`'s table-overflow branch. Skip until a css-tables test requires it.
- Fragmentation column/page context for OOF. Out of scope.

#### Commit 1 — Pure module (types + algorithmic functions, no wiring) — **DONE 2026-04-21, commit `a3c8db38`**
- [x] Created `pkg/layout/absolute_utils.go` (612 lines):
  - Types: `InsetBias` (enum), `ItemPosition` (enum), `AlignmentData`, `LogicalAlignment`, `LogicalOofInsets` (+ `LogicalInsets.AsOofInsets()`), `InsetModifiedContainingBlock`, `LogicalOofDimensions`.
  - Pure functions (Blink naming): `GetAlignmentInsetBias`, `axesOppose`, `ComputeUnclampedIMCBInOneAxis` (including center-clipping collapse `2 × min(static, cb − static)`), `ResizeIMCBInOneAxis`, `ComputeUnclampedIMCB`, `BiasFromStaticEdge`, `ComputeMargins`, `ComputeInsets`.
  - `ComputeOofInlineDimensions` / `ComputeOofBlockDimensions` deferred to Commit 2 (need layout-engine context).
  - No integration; no existing caller changes.
- [x] `absolute_utils_test.go`: 16 unit tests — three IMCB branches, ResizeIMCB, ComputeMargins, GetAlignmentInsetBias, ComputeInsets end-overflow fallback. All pass.
- [x] **Gate:** compiles; 16/16 new unit tests pass; no wm/CSS2/flex impact (dead code pending Commit 2).

#### Commit 2 — Wire resolver + alignment (Blink-parity behavior change) — **landed 2026-04-21**
- [x] `out_of_flow_layout.go` `layoutCandidatesOnce`: rewritten to use `ComputeUnclampedIMCB` + `ComputeMargins` + `ComputeInsets`. Static position shifted into CB-padding-box on input (`+ containingBlockPadding.Start`) and back to CB-content-box on output (`- containingBlockPadding.Start`).
- [x] Pre-layout fixed-size on both axes when both insets specified + child size is auto: `IMCB.size - non-auto-margins - child-BP`.
- [x] Indefinite-cbBlock fallback (preserves prior per-case formulas for the block axis when IMCB math isn't meaningful).
- [x] `OutOfFlowCandidate.Alignment LogicalAlignment` field added; zero value (ItemPositionNormal) yields BiasStart → compatibility with existing callers that don't populate it yet.
- [x] Flex OOF static-edge derived from parent's `justify-content` (main) + `align-items` (cross) via new helpers `flexOOFStaticMain` / `flexOOFStaticCross`. Mapped back to inline/block based on row-vs-column. (`flex_layout.go`.)
- [x] `absolute_utils.go` both-auto `BiasEqual` branch arms `defaultInsetBias = BiasStart` so the default-overflow fallback snaps centered abspos to the start edge when overflowing (mirrors Blink).
- [x] `ComputeUnclampedIMCB` propagates static-center overflow flag (both insets auto + `StaticEdgeCenter`) into `InsetModifiedContainingBlock.InlineHasDefaultAlignmentOverflow` / `BlockHasDefaultAlignmentOverflow`.
- [x] Propagated OOF candidates from a laid-out OOF ancestor: coordinate-translate their `StaticPosition.Offset` from the ancestor's content-box into the CB's content-box (add `finalInlineOffset + parentBP.InlineStart`, symmetric for block). Cross-WM physical-round-trip when `childWDM != wdm`. Mirrors `block_layout.go` `PropagateOOFCandidates`.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (+0 vs post-Phase-3 baseline). css-position **68 → 74** (+6). Closes `position-absolute-center-001/003/004/006` (G-ABS-CENTER) + `hypothetical-dynamic-change-001/002` (G-HYPO).
- [ ] Residual (3 of 8 targets) pushed to Commit 3: `position-absolute-center-002` (vertical-rl + column flex + align-items:center), `position-absolute-center-007` (`display:table` with auto margins + both insets + `margin-top:-50px`), `hypothetical-dynamic-change-003` (position:relative ancestor's left-offset must propagate into fixed descendant's static position).

#### Commit 3 — Residual 3 tests — **landed 2026-04-21**
- [x] `hypothetical-dynamic-change-003`: `block_layout.go` PropagateOOFCandidates now adds the positioned ancestor's `RelativeOffset` to each propagated candidate's `StaticPosition.Offset` so the fixed descendant's static position reflects the ancestor's `left`. Mirrors Blink's `OutOfFlowLayoutPart::PropagateOOFPositionedInfo` carrying `RelativeOffset`.
- [x] `position-absolute-center-002`: removed legacy `_writing-mode-inherited` early-return in `pkg/css/cascade.go` and `pkg/css/style.go` `resolveLogicalSizeProperties`. The skip was a louis13 artifact tied to a `transformToVerticalRL` post-pass that doesn't exist in louis14; it caused inline descendants of a `vertical-rl` container to keep `inline-size` mapped to physical `width` instead of `height`. +1 target test, +19 other CSS3 tests, zero regressions.
- [x] `position-absolute-center-007`: `out_of_flow_layout.go` now gates the IMCB stretched-fit path on `isNonStretchableDisplay(childStyle)` — tables (`display:table` / `display:inline-table`) keep intrinsic sizing and let auto margins absorb leftover space per CSS 2 §17.5 + Align 3 §8.2. Mirrors Blink's `node.IsTable()` gate in `absolute_utils.cc` `ComputeOof{Block,Inline}Dimensions`.
- [x] Paint-ordering regression (flex): `paint_layer.go` `sortZLists` preserves order-modified paint for flex items. DOMIndex-sort of AutoZero (added for hypothetical-003) broke flex paint ordering because flex items can hoist to an enclosing stacking context. Fix: skip the DOMIndex sort whenever any AutoZero entry `IsFlexItem()`.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓, css-position 74 → 77 (+3). All 3 residual tests at 0 diff.

**Representatives:** `position-absolute-center-001.html` (0.4%, drives Commits 2-3), `position-absolute-center-007.html` (2.1%, most likely to exercise center-clipping), `hypothetical-dynamic-change-001.html` (G-HYPO verification).

### Phase 5: G-ROOT-FLEX-GRID + G-FIXED (5 tests, 1 closed)
- [x] **G-FIXED Part A — OOF resolver re-entrance.** `OutOfFlowLayoutPart.LayoutCandidates` was dropping `childResult.PropagatedOOFCandidates`. Mirrored Blink's `OutOfFlowLayoutPart::LayoutOOFNodes` worklist pattern. Returns unresolved fixed candidates to caller; new `resolvesFixed` flag selects ICB / transform-or-containment-CB sites that absorb fixed. Updated all 7 call sites. Closes `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5% → 0%); reduces `position-fixed-scroll-nested-fixed` (4.2% → 1.0%). Residual diff is paint-time scroll/clipping (fixed escaping `overflow:auto`), not layout — defer.
- [x] **G-ROOT-FLEX-GRID (4 tests).** Blink research (2026-04-21): `layout_view.cc` has no special ICB-level IMCB short-circuit; `LayoutView::LayoutRoot` builds a viewport-sized fixed constraint space, then the root `<html>` is discovered as OOF in the LayoutView's in-flow pass and routed through `OutOfFlowLayoutPart::LayoutOOFNodes` → `absolute_utils.cc`'s `ComputeOof{Inline,Block}Dimensions`. With both insets specified + `align-self: normal`, the auto length resolves to `Length::Stretch()` against `imcb.InlineSize()` — box fills IMCB instead of shrinkwrapping.
- [x] Implementation (commit `7e686a28`): new `pkg/layout/positioned_root.go` with `buildRootConstraintSpace` + `resolvePositionedRootOffset`. When the root is `position:absolute/fixed`, pre-layout the root against an IMCB-derived constraint space (fixed inline/block size when both insets are specified + size is auto), then post-layout compute the final physical offset via `ComputeMargins` + `ComputeInsets` + WritingModeConverter. `engine.go` Layout() and `layoutNestedDocument()` both route through the helpers; non-positioned roots keep the existing viewport-stretched path verbatim.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (unchanged). All 4 G-ROOT-FLEX-GRID tests at 0 diff: `position-absolute-root-element-flex`, `position-absolute-root-element-grid`, `position-fixed-root-element-flex`, `position-fixed-root-element-grid`.
- [x] Regression + commit (`7e686a28` code, `6c53f52d` planning).
- [ ] G-FIXED residual: scroll-clip escape for fixed inside `overflow:auto` scrollable, plus `Element.scrollTop` JS setter (overlaps G-SCROLL).

### Phase 6: G-ABS-IN-INLINE (2 tests) — **DONE 2026-04-21**
- [x] Blink research: `inline_containing_block_utils.cc` — union of first + last line-box fragment rects.
- [x] New `pkg/layout/inline_containing_block.go`: `ComputeInlineContainerGeometry` + `BuildPositionedInlineMap` + `InlineCBLogical`. Walks line-box fragments (transparently descends anonymous block continuations from block-in-inline splits), unions first-line and last-line fragment rects for the target inline's DOM node, converts to logical via the block's writing-mode converter.
- [x] Wire into OOF pass. `inline_layout.go` tags `InlineItemOutOfFlow.InlineContainer` via `BuildPositionedInlineMap` (position:fixed excluded — CB is viewport, not inline). `block_layout.go` resolves geometry before OOF layout; when the inline produced no line-box fragments (CSS 2.1 §9.4.2 line-box suppression for OOF-only lines) the candidate is routed as a regular non-inline-CB candidate. `out_of_flow_layout.go` tracks `cbOriginInBuilder` and subtracts it from static-position offsets so IMCB math runs in CB coords, then adds it back at `AddChild` time. `layout_tree_builder.go` emits an empty leading continuation for positioned inlines with block-in-inline splits when trailing inline content exists, so the start line-box union rect covers the span's start.
- [x] Regression + commit: M6 landed 2026-04-21 via commit `01f468d9`.

**Verification:** css-position **81 → 83** (+2 of 2 targets closed). `position-absolute-in-inline-003.html` and `position-absolute-in-inline-004.html` both pass at 0 diff.

**Gates:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (pre-existing residuals unchanged), absolute-tables 14/14 ✓, position-relative-003/004/005 ✓ (no regression), position-relative-002/011/013 baseline unchanged.

### Phase 7: G-STICKY (1 test) — minimum viable
- [x] Blink research: `sticky_position_scrolling_constraints.h` + `ComputeStickyPositionConstraints`; scroll-time offset, not layout-time.
- [ ] Short-circuit: for `position: sticky` at layout, emit zero `RelativeOffset` when the natural flow satisfies the threshold (at scroll=0 this is always true for sticky-top-001).
- [ ] Note: full `StickyPositionScrollingConstraints` deferred until scroll-based sticky tests appear.
- [ ] Regression + commit.

### Phase 8: G-REPLACED (1 test)
- [ ] **Blink research (deferred from Phase 0):** CSS 2.2 §10.3.7 / §10.6.5 abs-replaced width/height with `max-content`.
- [ ] Implement + verify.
- [ ] Regression + commit.

### Phase 9: G-SINGLETONS (11 tests, includes `position-change`)
- [ ] Per-test triage; many may already be closed by earlier phases.
- [ ] `position-change.html` — fix HTML parser to not bail on `expected '>' but reached EOF`.

### Phase 10: Delivery
- [ ] Confirm 100/100 runnable at 0 diff (104 total if SKIPs are later un-skipped by separate infra work).
- [ ] Confirm wm 781/781, CSS2 99/99, flex unchanged.
- [ ] Final session log summary.

### Phase 11: css-flexbox watch-category residuals (3 tests)
Parallel / tail-end track. The css-flexbox suite is a *watch* invariant (must stay ≥621 PASS), not part of the css-position delivery goal. Three residuals have sat at the same counts since Phase 1; document them here so the scoped Blink research doesn't get lost. Pick up after css-position 100/100 is delivered — or opportunistically if a css-position fix happens to touch the relevant flex paths.

#### 11a — `auto-margins-001.html` (0.2% diff, ~1024 px)
- [ ] **Blink research (done 2026-04-21):** sub-case 2 (the `writing-mode: vertical-rl` flex container + `<p style="margin:auto">OK</p>`) is the offender — VRL centering rather than VRL block-start flush. Sub-case 1 (HTB) and the 3-concentric-circles sub-case render identically modulo AA noise.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc` — `ApplyReversals`, `AlignFlexLines`, the auto-margin block inside item placement (search `auto_margins` / `ResolveAutoMargins`). `third_party/blink/renderer/core/layout/flex/flex_item.cc` — `HasAutoMarginsInCrossAxis` and cross-axis stretch suppression. `third_party/blink/renderer/core/layout/geometry/writing_mode_converter.{h,cc}` — physical↔logical mapping reference.
- [ ] **louis14 touchpoints:** `pkg/layout/flex_layout.go` — `getItemAutoMargins` (~lines 4043-4086) and the cross-axis auto-margin resolution (~lines 1318-1360 / §8.1 block ~line 2086).
- [ ] **Hypothesis:** `getItemAutoMargins` loses the item-vs-container writing-mode distinction (`mainIsItemInline`) for the cross axis in VRL. When the container is a flex *row* (main=inline, cross=block) in VRL, the physical `margin-top/bottom` of the `<p>` should map through the container's logical converter; today the mapping centers instead of leaving block-start flush. Gate the cross-axis stretch-suppression on both-auto cross margins via the correct logical-axis conversion.
- [ ] **Gate:** target passes at 0 diff; wm 781/781 and CSS2 99/99 hold; no regression to other flex tests.

#### 11b — `content-height-with-scrollbars.html` (14.4% diff, ~69200 px) — classic-scrollbar reservation
- [ ] **Blink research (done 2026-04-21):** this is not a flex bug — it is a platform/layout gap. WPT Chromium reference PNGs are generated with *classic* (space-taking) scrollbars at 15px; louis14's `classicScrollbarWidth()` in `pkg/layout/fragment_geometry.go:141` returns **0** for the default `"auto"` scrollbar-width, so `overflow: scroll` elements reserve zero space. The `FragmentGeometry.Scrollbar` / `BorderBoxPadding` / `Inline/BlockScrollbarSum` pipeline is already threaded through all layout algorithms — the single defect is the constant.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/layout_box.h` — `VerticalScrollbarWidth()` / `HorizontalScrollbarHeight()`. `third_party/blink/renderer/platform/scroll/scrollbar_theme_aura.cc` — `ScrollbarThickness()` returns 15px (classic default). `third_party/blink/renderer/core/layout/ng/ng_box_fragment_builder.cc` — `SetScrollbar()` populates `FragmentGeometry::scrollbar` from `ComputeScrollbars()`. `ng_length_utils.cc` — `CalculateBoxSizes` subtracts scrollbar from available inline/block size.
- [ ] **louis14 touchpoints:** `pkg/layout/fragment_geometry.go` — `classicScrollbarWidth()`. Change the `"auto"` return from `0` to `15` when the element reserves a classic scrollbar; keep `10` for `thin`, `0` for `none`. No other code changes needed.
- [ ] **Scope boundary:** layout-only reservation; we do not paint the scrollbar chrome (matches existing louis14 rendering model).
- [ ] **Regression risk:** any existing test implicitly passing because louis14 ignored scrollbar reservation will shift. Candidate fallout in `cross-axis-scrollbar.html`, `contain-size-scrollbars-002.html`, `scrollable-overflow-transform-unreachable-region.html`, plus non-flex `overflow: scroll` tests across css2 and css-position. Triage in the same commit; do not split.
- [ ] **Gate:** target + candidate-fallout tests all at 0 diff; wm 781/781 and CSS2 99/99 hold; css-flexbox stays ≥626 (no regressions).

#### 11c — `flexbox-align-self-vert-004.xhtml` (0.8% diff, ~3664 px)
- [ ] **Blink research (done 2026-04-21):** two candidate roots, in priority order:
  1. `align-self: baseline` in the column-direction container. `flex_layout.go:1403-1433` (placement) synthesizes `bl = 0` for column-sameWM baseline — inline-start edge — bypassing `resolvedFirstBaseline`. But the first-pass line-cross accumulation at `flex_layout.go:822-871` still routes baseline items through `resolvedFirstBaseline`, which for `baselineParallel=false` returns `crossSize` as the synthetic baseline — inflating `maxAscent = crossMarginStart + crossSize`. Line-cross and per-item offset disagree.
  2. `align-self: stretch` with items wider than the 4px container. `stretchFlexItems` (`flex_layout.go:4643`) clamps `stretchBorderBox` to 0 when `line.crossSize - crossMarginSum < 0`, then re-lays out the item at 0 content-width. Blink's `DetermineUsedCrossSize` takes the max of the stretched size and the item's min-content, so an item with `width:50px` keeps 50px.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc` — `PlaceFlexItems`, `DetermineUsedCrossSize`, `BaselineAscent`, `ApplyFinalAlignmentAndReversals`. `third_party/blink/renderer/core/layout/flex/baseline_utils.{h,cc}` — `DetermineBaselineWritingMode`, `DetermineBaselineGroup` (~lines 51-89), `SynthesizedBaseline`.
- [ ] **louis14 touchpoints:** `pkg/layout/flex_layout.go` — unify the two baseline-resolution paths (first-pass accumulation at ~:819-871 vs placement at ~:1403-1433) so column-sameWM synthesizes at inline-start in *both*. In `stretchFlexItems` (~:4607+), honor min-content / explicit `width` when line cross-size is smaller than the item. `resolvedFirstBaseline` at ~:309 needs a column-sameWM caller contract or a dedicated helper so accumulation uses `bl = 0` matching placement.
- [ ] **Hypothesis:** unify column-sameWM baseline synthesis to inline-start in both the accumulation and placement paths; in stretch, clamp to `max(line.crossSize, itemMinContent/explicitCross)` instead of zero. Fixes the residual pixels.
- [ ] **Gate:** target passes at 0 diff; wm 781/781 and CSS2 99/99 hold; css-flexbox stays ≥626.

## Milestones (commit + report after each)
Counts are against **runnable tests (100)**; 4 SKIPs excluded.

- **M1:** G-TABLE-REL closed → +11 primary (50 → 61). **Achieved 2026-04-21** via commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Verified at re-baseline post OOF re-entrance: also closed `position-relative-012` (was conjectured). 8 `-absolute-child` variants still failing at 1.0% — distinct root cause, deferred to G-ABS-IN-INLINE / G-ABS-IN-TABLE.
- **M2:** ~~G-CB-CHANGE~~ — group dissolved 2026-04-21. Tests reassigned to G-FIXED / G-SINGLETONS / G-SCROLL.
- **M3:** G-DYN-STATIC closed → +6 (→ 68). **Achieved 2026-04-21** (Parts a+b+d via commits `233d408f`, `d250c5cf`; Part c via commit `5399d328` — orphan-cell vertical-align + transform percent-sentinel fix). Bonus: +9 css-transforms (162 → 171).
- **M4:** G-ABS-CENTER + G-HYPO combined (IMCB) → +8 (→ 77). **Achieved 2026-04-21** via Phase 4 Commits 1 (`a3c8db38`), 2 (`d9f6628b`), and 3. Group closed.
- **M5a:** G-FIXED Part A — OOF resolver re-entrance. **Achieved 2026-04-21** via commit `ed16475f`. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (62 PASS total). Reduced `position-fixed-scroll-nested-fixed` 4.2% → 1.0% (residual paint-clip).
- **M5b:** G-ROOT-FLEX-GRID closed → +4 (77 → 81). **Achieved 2026-04-21** via commit `7e686a28` (new `pkg/layout/positioned_root.go` routing positioned `<html>` through IMCB sizing). G-FIXED Part B (paint-clip / scrollTop) overlaps G-SCROLL, still open.
- **M6:** G-ABS-IN-INLINE closed → +2 (81 → 83). **Achieved 2026-04-21** via commit `01f468d9`. New `pkg/layout/inline_containing_block.go` mirrors Blink's `InlineContainingBlockUtils::ComputeInlineContainerGeometry` (start/end fragment union rects, transparent walk through anonymous-block continuations). OOF resolver routed through inline-CB sizing with `cbOriginInBuilder` tracking; `InlineContainer` stamped on OOF items via `BuildPositionedInlineMap` (position:fixed excluded — viewport CB). Empty-leading-continuation emitted in `layout_tree_builder.go` for positioned inlines with block-in-inline splits to keep the first-line union rect anchored at the span's start.
- **M7:** G-STICKY + G-REPLACED closed → +2 (→ ~84).
- **M8:** G-SINGLETONS (including `position-change`) + G-SCROLL swept → 100/100 runnable.
- **M9 (parallel track):** Phase 11 flex residuals swept → css-flexbox 629/629. Independent of css-position delivery; pick up opportunistically or after M8.

## Test command templates
```
# Single test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position/<name>' -v

# Whole category (sanctioned baseline; don't rerun casually)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position' -v 2>&1 | tee output/baselines/css-position-<date>.log

# Regression sweeps
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'     # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests'                           # CSS2: expect 99/99
```

## Key Questions
1. ~~Does `table_layout.go` emit intermediate fragments?~~ **Answered (2026-04-21):** yes, it emits per-row and per-cell fragments at `table_layout.go:685` and `:735`. The fix targets those add-sites (or better, the shared `AddChild` below them, matching Blink's `BoxFragmentBuilder::AddChild`).
2. Are the 6 `dynamic-static-position-*` failures all driven by static-position caching, or do the `inline`/`table-cell`/`floats` branches each need separate plumbing? Blink evidence says "all driven by the same cached-vs-rebuilt static position." Confirm with Phase 3 representative.
3. **G-STICKY scope:** minimum viable is zero offset at scroll=0 — sufficient for `sticky-top-001`. Full `StickyPositionScrollingConstraints` is deferred until scroll-based tests appear.
4. **New:** is the hypothetical-box algorithm already folded into the IMCB both-auto-insets branch, or does it need separate plumbing? Blink evidence says it is the same branch. If Phase 4 makes the hypothetical tests pass for free, G-HYPO collapses into G-ABS-CENTER.

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Attack G-TABLE-REL first | 16 tests, one likely root cause, cleanest unlock. Foundational correctness §1. |
| Prefer pushing relative-offset check into shared `AddChild` | Mirrors Blink's `BoxFragmentBuilder::AddChild`; prevents the same class of bug recurring in future layouts. |
| NORUN — 4 SKIPs out of scope, 1 real FAIL folded into G-SINGLETONS | Triage 2026-04-21: SKIPs are harness/JS gaps; target is 100/100 runnable. |
| Bundle G-ABS-CENTER + G-HYPO into one phase | Both use the same Blink IMCB machinery; the hypothetical-box algorithm IS the both-insets-auto branch. |
| G-DYN-STATIC precedes G-ABS-CENTER/G-HYPO | The IMCB reads `LogicalStaticPosition`; without rebuild-per-pass, dynamic inputs won't flow through. |
| G-CB-CHANGE is invalidation-only → group dissolved | Audit 2026-04-21: harness already does fresh re-layout post-JS, so Blink's invalidation pattern is a no-op for us. Tests reassigned by actual root cause. |
| G-FIXED OOF re-entrance via worklist loop, not single-pass | Mirrors Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`. New `resolvesFixed` flag distinguishes ICB / containment / transform CB sites (absorb fixed) from ordinary positioned sites (return unresolved fixed to caller). |
| Split G-FIXED into Part A (re-entrance, layout) and Part B (paint-clip / scrollTop) | Re-entrance closed 1 of 2 cleanly; the residual is squarely in paint/scroll, not OOF layout. Don't conflate — Part B will be picked up alongside G-SCROLL. |
| G-STICKY minimum viable acceptable | Full constraint machinery is overkill for the one failing test; flag as tech debt for when scroll-based sticky tests appear. |
| ExclusionSpace stores LOCAL inline offsets (not BFC-absolute) | Learned 2026-04-21 while fixing Phase 3(b). Readers must NOT subtract `bfcInlineOrigin` from `FindAvailableInlineSize` results. Invariant holds for any caller in the same enclosing block that owns the exclusion space. Full write-up in `findings.md` "Coordinate-system notes". |
| Inline-FC OOF block-end read "at time of encounter" | Mirrors Blink's `line_box_.LineBoxBlockEnd()` semantics. `hasInflowOnLine` incremental flag is the correct primitive; deferred emission at end-of-line without this gate regresses orthogonal-float wm tests. |
| Transform parser uses explicit `IsPercent []bool`, not sign sentinel | Sign-based percent sentinel (`result := -percent`) collided with legitimate negative pixel lengths. `translate: 0 -50px` was rendering as `+50px`. Fixed by storing percent-ness per component; widened `GetIndividualTranslate` signature. +1 css-position + 9 css-transforms for free. |
| Orphan `display:table-cell` gets vertical-align at `block_layout.go`, not via §17.2.1 anon-wrapping | Reverse §17.2.1 anonymous-table generation is unimplemented, so orphan cells dispatch to block layout. Adding a guarded vertical-align shift at the block-layout end-of-pass is cheaper (and bounded) than implementing reverse §17.2.1. `TableSectionData == nil` guard prevents double-shift on the proper-table path. |
| Do not run the full css-position category more than once per milestone | CLAUDE.md §4 — broad runs only at baselines and milestone verifications. |
| css-writing-modes stays at 781/781 as an invariant | Phase 5f is complete; any regression in wm reverts the commit. |

## Deferred / parked work (may not be needed; capture so we don't lose it)

### Proper-table-path vertical-align on propagated OOF candidates
**Context.** During Phase 3(c) I drafted a `table_layout.go` change that (i) recomputed `contentBlockSize` from the cell's pre-stretch intrinsic content + box-model (instead of the post-stretch `cellLogical.BlockSize()`), and (ii) extracted `vaBlockShift` into a variable so it could be added to `PropagatedOOFCandidates[].StaticPosition.Offset.BlockOffset` during the per-row sweep (matching Blink's `TableCellLayoutAlgorithm` applying `intrinsic_padding_before` before OOF propagation).

**Why it didn't ship.** The Phase 3(c) target test turned out to use *orphan* `display:table-cell` (no table ancestor) and never exercised the proper-table path. Worse, the `contentBlockSize` pre-stretch change regressed 3 wm orthogonal-writing-mode tests (`box-offsets-rel-pos-vlr-005`, `box-offsets-rel-pos-vrl-004`, `orthogonal-cell-001`) — the pre-stretch shape interacts badly with orthogonal cells where `cellBlockForRow` reads the cell's *inline* size as the row's block size.

**When to revisit.** The moment a test requires vertical-align centering of an abspos descendant inside a real `<table><td>...`. Candidate triggers: future G-HYPO tests using `<td>`, any css-tables-3 test with abspos + vertical-align inside a cell.

**What to redo.**
1. Re-apply the `vaBlockShift` extraction + `PropagatedOOFCandidates` shift in `table_layout.go` (structurally it's correct — mirrors Blink).
2. Debug the `contentBlockSize` shape separately against `orthogonal-cell-001` before landing. The fix likely needs to detect orthogonal cells and use a different pre-stretch quantity (cell's inline-size in its own WDM rather than block-size). Do not ship one without the other.
3. Drop the `TableSectionData == nil` guard in `block_layout.go` only if the proper-table path handles the shift — otherwise keep both paths.

**Pointer.** See `findings.md` § "G-DYN-STATIC — (c) table-cell" → "Not shipped — proper-table-path vertical-align capture" for the exact diff that was dropped.

## Notes
- `output/baselines/` holds raw logs; parse scripts live in `/tmp/` and are regenerated per session.
- Current branch: `fix/flexbox-fast`. Master is the delivery target.
