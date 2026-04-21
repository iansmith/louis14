# Findings & Decisions — css-position category

## Rules pointer
Do not restate project rules here. They live in:
- `/Users/iansmith/louis14/CLAUDE.md`
- `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`

Findings should assume those rules are already loaded in context.

## Archived wm work
All writing-modes category findings — 787 tests, bidi root-causes, orthogonal sizing, Phase 5f Groups A/B/C — have been moved to `docs/findings-wm.md`. Do not duplicate here.

## Requirements
- 104 css-position tests actually exercised by `TestWPTCSS3Reftests/css-position`.
- Goal: all 104 pass at 0 diff. Baseline 50/104 (48%) → close 54 failures + 5 NORUN.
- Do not regress: css-writing-modes (781/781), CSS2 (99/99), css-flexbox (~621/629).

## Phase 0 Baseline (complete — 2026-04-21)
Raw log: `output/baselines/css-position-2026-04-21.log`
Parsed list: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`)

### Overall
- 104 tests run · **50 PASS · 54 FAIL · 5 NORUN**
- Highest diff: `containing-block-change-scrollframe.html` (10.4% / 50000 px).
- Lowest diff (still failing): `position-absolute-dynamic-list-marker.html` (0.0% / 18 px) — likely a 1-pixel geometric slip, not visible fuzz.

### NORUN triage — **DONE 2026-04-21**
Ran each test individually with `-v` to see what the runner actually emitted. Four of the five are SKIP (runner reports "no usable reference files found"); the fifth is a real FAIL masquerading as NORUN because the parser-error log format doesn't match our grep pattern.

| Test | Runner output | Root cause | Category |
|---|---|---|---|
| `hypothetical-box-scroll-parent.html` | `no usable reference files found` → SKIP | `hypothetical-box-scroll-parent-ref.html` is missing from our WPT snapshot (only the test file is on disk). | Infrastructure gap — not a layout bug |
| `hypothetical-box-scroll-viewport.html` | JS error `TypeError: Object has no member 'scrollTo'`, then `no usable reference files found` → SKIP | `window.scrollTo` unimplemented in our JS engine; ref file may also be missing. | JS engine gap + possibly infra |
| `position-absolute-multicol-001.html` | `no usable reference files found` → SKIP | Test uses `<link rel="match" href="/css/reference/pass_if_pass_below.html">` — absolute WPT-server path. A copy exists at `pkg/visualtest/testdata/wpt-css3/css-position/pass_if_pass_below.html`, but our runner doesn't resolve absolute refs. | Infrastructure gap |
| `position-change.html` | `parse error: tokenizer error: expected '>' but reached EOF` → **FAIL** | Our HTML parser bails on this file. Counted NORUN by parser only because FAIL was emitted on a different log line than the parser regex matches. | Real layout/parser bug |
| `replaced-object-backdrop.html` | JS error `TypeError: Value is not an object: undefined`, then `no usable reference files found` → SKIP | Uses `<object popover="auto">` + JS; unsupported DOM API. Ref file `/css/reference/green.html` also absolute-path. | JS engine + infra |

**Planning consequences.**
- **4 SKIPs, 1 real FAIL.** True failure count is **55 FAIL** (54 + position-change) across 100 runnable tests; 4 are skipped for non-layout reasons.
- **Infra fixes unlock 4 tests for free** if the root causes are fixed. Two sub-fixes needed: (a) resolve absolute WPT-server ref paths against category dir + category-dir `reference/`; (b) copy/link `hypothetical-box-scroll-parent-ref.html` from WPT upstream.
- **Target: 100/100 runnable (104/104 if SKIPs are converted to runnable first).** Decision: treat the 4 SKIPs as out-of-scope for the css-position plan (they need harness + JS-engine work, not layout fixes). Phase 0 counts **55 to close**, not 59.

## Group breakdown (54 fails + 5 NORUN = 59)

Grouped by hypothesised shared root cause, not by diff %. Largest-cluster-first.

### G-TABLE-REL — 16 tests — **Phase 1 target**
```
position-relative-table-thead-top.html       1.2%
position-relative-table-thead-left.html      1.2%
position-relative-table-tfoot-top.html       1.2%
position-relative-table-tfoot-left.html      1.2%
position-relative-table-tbody-top.html       1.2%
position-relative-table-tbody-left.html      1.2%
position-relative-table-tr-top.html          1.7%
position-relative-table-tr-left.html         1.7%
position-relative-table-tfoot-top-absolute-child.html  1.7%
position-relative-table-tr-top-absolute-child.html     1.0%
position-relative-table-tr-left-absolute-child.html    1.0%
position-relative-table-thead-top-absolute-child.html  1.0%
position-relative-table-thead-left-absolute-child.html 1.0%
position-relative-table-tfoot-left-absolute-child.html 1.0%
position-relative-table-tbody-top-absolute-child.html  1.0%
position-relative-table-tbody-left-absolute-child.html 1.0%
position-relative-table-td-top.html          0.6%
position-relative-table-td-left.html         0.6%
```
**Hypothesis.** `pkg/layout/table_layout.go` has no `PositionRelative`/`PositionSticky` branch. `block_layout.go:928-939`, `flex_layout.go:1821-1832`, `grid_layout.go:395-403`, and `inline_layout.go:1122/1286/1401` all set `Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)` when the style's position is relative/sticky. The table algorithm emits fragments but never calls this.

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/layout/box_fragment_builder.cc` — `BoxFragmentBuilder::AddChild()`. Relative offset is applied at the *fragment-builder level*, uniform across all display types (block/flex/grid/table). The pseudo-code is:

    ```cc
    if (box_child.Style().GetPosition() == EPosition::kRelative) {
      relative_offset =
          ComputeRelativeOffsetForBoxFragment(
              box_child, GetWritingDirection(), child_available_size_);
    }
    AddChildInternal(&child, child_offset + *relative_offset);
    ```

  Because Blink funnels every AddChild through this path, tables inherit the behaviour for free.
- `third_party/blink/renderer/core/layout/relative_utils.cc` — `ComputeRelativeOffsetForBoxFragment`. Resolves `top/right/bottom/left` (unit %, length) against the child's available size, applies the writing-direction axis flip.
- `third_party/blink/renderer/core/layout/table/table_layout_algorithm.cc` — NG table algorithm; it never has to touch relative offsets itself.

**Our mirror.** `pkg/layout/table_layout.go` goes around our `AddChild` equivalent. There are two concrete add-sites:
- Line 685: `rowBuilder.AddChild(cellFrag, LogicalOffset{…})` for cells.
- Line 735: `builder.AddChild(rowResult.Fragment, LogicalOffset{…})` for rows/sections.

Neither consults the child's position property. `block_layout.go:928-964`, `flex_layout.go:1821-1832`, and `grid_layout.go:395-403` all do. The fix is to apply `computeRelativeOffset` at both add-sites (or, preferably, push the check down into the shared fragment-builder `AddChild` so every future layout gets it automatically — this matches Blink's design and avoids the same bug recurring).

**Open question (now answered):** our table algorithm *does* emit per-row and per-cell fragments (row fragments are built by `rowBuilder` and then added to the table), so the `RelativeOffset` goes on each intermediate fragment at its add-site. No fragment-construction surgery required.

### G-ABS-CENTER — 5 tests
```
position-absolute-center-001.html   0.4%
position-absolute-center-002.html   0.8%
position-absolute-center-003.html   0.3%
position-absolute-center-004.html   0.3%
position-absolute-center-007.html   2.1%
```
**What the tests exercise.** `position: absolute` with either `margin: auto` + both insets, or `justify-content: center` on a flex container, combined with CSS Align 3 abspos sizing (available space = 2 × distance from center of static-position rectangle to closest CB edge).

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/layout/absolute_utils.cc` + `.h` — all OOF sizing. Key functions and structs:
  - `ComputeUnclampedIMCBInOneAxis` (line 128): core per-axis IMCB computation with three branches — both insets auto (static-position rectangle becomes the available space), one inset auto (margin resolves against IMCB), both specified (IMCB is trivial, margin split in the leftover).
  - `ComputeUnclampedIMCB` (line 196): wraps both axes.
  - `ResizeIMCBInOneAxis` (line 108): applies alignment bias (kStart/kEnd/kEqual) to distribute leftover space after sizing.
  - `GetAlignmentInsetBias` (line 51): converts `align-self`/`justify-self`/`auto-margins` to bias enum. `center` = kEqual; both-auto-margins = kEqual; both-auto-insets = kEqual *and* the IMCB equals the static-position rect.
  - `ComputeMargins`, `ComputeInsets`: translate the `auto` combinations into concrete values.
  - `ComputeOofInlineDimensions`, `ComputeOofBlockDimensions`: the callers for `OutOfFlowLayoutPart`.
  - Structs: `InsetModifiedContainingBlock`, `LogicalOofInsets`, `LogicalOofDimensions`, `LogicalStaticPosition`, `LogicalAlignment`.
- `third_party/blink/renderer/core/layout/out_of_flow_layout_part.cc` — dispatch into the above.

**Algorithm summary.** For each axis:
1. Build `InsetModifiedContainingBlock` = CB with any specified insets subtracted; both-auto insets leave the CB unchanged but replace the rectangle with the static-position rect.
2. Size the box inside the IMCB (intrinsic / stretch depending on `width`/`height` computed value).
3. `ResizeIMCBInOneAxis` distributes leftover space using the alignment inset bias — kStart sticks to the start edge, kEnd to the end edge, kEqual splits (true centering, or auto-margin distribution).
4. For **center alignment** specifically, the available size collapses to `2 × min(static_offset, cb_size − static_offset)` so the box stays centered around the static position *without* escaping the CB — this is the spec's "clipping" rule.

**Spec:** <https://drafts.csswg.org/css-align-3/#abspos-sizing>.

**Our mirror target.** New file `pkg/layout/absolute_utils.go` (or extend existing OOF plumbing) with `InsetModifiedContainingBlock`, `ComputeUnclampedIMCBInOneAxis`, `ResizeIMCBInOneAxis`, and the two `ComputeOof*Dimensions` entry points. Name types and functions identically to Blink's to keep the translation reviewable.

**Shared dependency:** `LogicalStaticPosition` is consumed by this machinery — any fix here interlocks with G-DYN-STATIC (which owns static-position rebuilding) and G-HYPO (which uses the both-auto-insets branch).

### G-CB-CHANGE — 3 tests
```
containing-block-change-scrollframe.html               10.4%
containing-block-change-button.html                    4.2%
absolute-pos-box-inside-fixed-pos-box-with-changing-height.html  0.5%
```
**What they exercise.** JS mutates a property that establishes a new containing block — `overflow: hidden` on a div, or insertion of a button — after the page has laid out. Abspos children must re-resolve to the new CB.

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/layout/layout_object.cc` (5432 lines). Key path on style change:
  - `StyleDidChange` → detects a position/containment-establishing change via `StyleDifference::NeedsPositionedLayout` (set by `ComputedStyle::VisualInvalidationDiff` when `overflow`, `position`, `contain`, `transform`, `will-change`, etc. change in ways that affect CB resolution).
  - `MarkParentForSpannerOrOutOfFlowPositionedChange` (line 1640) → notifies the container chain.
  - `MarkContainerChainForLayout` (line 1546) → bubbles dirtiness up.
  - `ContainerForAbsolutePosition`, `CanContainAbsolutePositionObjects` → pick the right new CB given the post-change style.
- `third_party/blink/renderer/core/layout/layout_block.cc` (651 lines):
  - `LayoutBlock::StyleDidChange` (line 113) → the block-specific path.
  - `RemovePositionedObjects(LayoutObject* stay_within)` (line 298) → walks the `positioned_objects_` set, for each abspos child whose CB is no longer `this`, removes it from the old CB's tracked list and re-inserts it at the new CB.
- `third_party/blink/renderer/core/style/computed_style.cc` — `VisualInvalidationDiff` sets the `NeedsPositionedLayout` bit.

**Algorithm summary.** This is an *invalidation-only* story, not a sizing story. When JS mutates a property that changes CB establishment:
1. `VisualInvalidationDiff` sees the delta and sets `StyleDifference::NeedsPositionedLayout`.
2. `StyleDidChange` on the affected object calls the old CB's `RemovePositionedObjects(stay_within=nil)` so every abspos descendant is detached from its stale tracking list.
3. Each detached abspos child is re-inserted into the new CB via the normal "find my CB and register with it" path that runs when they next lay out.
4. `MarkContainerChainForLayout` forces the enclosing chain to relayout, which reruns the OOF pass and places the children against the new CB.

**Our gap.** Grep our tree for "RemovePositionedObjects" / "positioned_objects_" / "StyleDifference". If these don't exist, our OOF children are re-laid out against the *stale* CB after a style change — exactly what `containing-block-change-scrollframe` exposes at 10.4% diff.

**Our mirror target.** The hooks must live wherever we currently run style recalc → layout: add a `PositionedDescendants` set on each containing-block-capable fragment result, and a `RemovePositionedObjects` step in style-did-change. Mirror Blink's names.

**Related:** overlaps with G-DYN-STATIC (both require style-change to re-trigger OOF layout) but is distinct — G-CB-CHANGE is about *which CB* the child belongs to; G-DYN-STATIC is about *which static position* inside the unchanged CB.

### G-DYN-STATIC — 6 tests
```
position-absolute-dynamic-static-position-floats-001.html   0.7%
position-absolute-dynamic-static-position-floats-002.html   0.3%
position-absolute-dynamic-static-position-floats-003.html   0.3%
position-absolute-dynamic-static-position-floats-004.html   0.7%
position-absolute-dynamic-static-position-inline.html       2.1%
position-absolute-dynamic-static-position-table-cell.html   2.1%
```
**What they exercise.** JS flips a property (float insertion, `display: inline → block`, table-cell vertical-align interaction) that changes the abspos child's static position. Triggers re-layout; the new static position must be picked up.

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/layout/out_of_flow_layout_part.cc`:
  - `OutOfFlowLayoutPart::SetupNodeInfo` — gathers CB info per candidate.
  - `LayoutCandidates` — main loop that consumes pending OOF candidates.
  - `CalculateOffset` / `TryCalculateOffset` — wrap `absolute_utils.cc`.
- `third_party/blink/renderer/core/layout/oof_positioned_node.h`:
  - `struct LogicalStaticPosition` (line 240): `{LogicalOffset offset; LogicalDirection inline_edge; LogicalDirection block_edge;}`.
  - `OofPositionedNode<LogicalOffset, LogicalStaticPosition>` — the per-candidate record.
  - `LayoutResult::OutOfFlowPositionedDescendants()` — the list that bubbles candidates up the fragment tree.

**Key insight.** Blink does **not cache** the static position. Every layout pass rebuilds the OOF-descendants list on each `LayoutResult`, and it bubbles up to the nearest containing block that can establish an OOF CB. At CB time, static positions are fresh — so JS-driven changes that alter static position (new float, display flip, table-cell vertical-align shift) show up "for free" provided the enclosing layout is rerun.

**Our gap.** Our OOF path likely caches something (result fragments, cached inset resolution, or the abspos child's earlier static position). The four `floats-00*` tests, `inline`, and `table-cell` tests all point at a single missing fixture: the static-position record must be rebuilt every pass, not memoised.

**Our mirror target.** An `OutOfFlowPositionedDescendants` field on `LayoutResult` (or its equivalent), carrying `{node, static_position, inline_container}` records. Remove any static-position caching; always rebuild from current layout state.

**Prerequisite for G-HYPO.** The hypothetical-box algorithm reads static position via this same path, so G-DYN-STATIC must land before G-HYPO can be fully correct.

### G-HYPO — 3 FAIL + 2 NORUN
```
hypothetical-dynamic-change-001.html   2.1%  (fixed-pos ancestor moves)
hypothetical-dynamic-change-002.html   2.1%
hypothetical-dynamic-change-003.html   4.2%
hypothetical-box-scroll-parent.html    NORUN
hypothetical-box-scroll-viewport.html  NORUN
```
**What they exercise.** CSS Position 3 hypothetical-box algorithm: `position: absolute` with auto-left/auto-right uses the parent's in-flow position. When the ancestor itself moves (via JS), the child's hypothetical position must re-derive.

**Blink entry points (studied 2026-04-21):**
- **Shares the IMCB machinery with G-ABS-CENTER.** There is *no* separate `HypotheticalFragment` in Blink — the "hypothetical box" position *is* the value produced by the both-insets-auto branch of `ComputeUnclampedIMCBInOneAxis` in `absolute_utils.cc`, which equals the static-position rectangle. The algorithm reads from `LogicalStaticPosition` (from G-DYN-STATIC) and produces the IMCB used for sizing.
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc` — `PrepareLayout` hands the current static position along to `OutOfFlowLayoutPart` via the OOF-descendants list.
- Spec: <https://drafts.csswg.org/css-position/#size-and-position-details>.

**Algorithm summary.** When both `left` and `right` (resp. `top` and `bottom`) resolve to `auto` on an abspos element:
1. The static position rectangle is read from the current layout (NOT a cached one).
2. The IMCB in that axis collapses to the static-position rect.
3. Sizing + alignment bias proceed as in G-ABS-CENTER.
When a fixed-pos ancestor moves via JS, step 1 naturally picks up the new value provided the enclosing layout runs again — which is exactly what G-DYN-STATIC guarantees.

**Our mirror target.** Same as G-ABS-CENTER — once IMCB is implemented and static position is rebuilt every pass, the `hypothetical-dynamic-change-00*` tests will resolve.

**Prerequisite chain:** G-DYN-STATIC (rebuild static position) → G-ABS-CENTER (IMCB+alignment) → G-HYPO (both-auto-insets branch). If IMCB lands first the hypothetical tests may already pass; re-check before starting Phase 4.

### G-ROOT-FLEX-GRID — 4 tests
```
position-fixed-root-element-flex.html    0.8%
position-fixed-root-element-grid.html    0.8%
position-absolute-root-element-flex.html 0.8%
position-absolute-root-element-grid.html 0.8%
```
**What they exercise.** `<html>` element with `position: fixed|absolute` and `display: flex|grid`. Insets define the box size relative to ICB; not a shrink-to-fit.

**Blink entry point:** `layout_view.cc` + flex/grid root-element special-cases.

### G-FIXED — 1 test
```
position-fixed-scroll-nested-fixed.html   4.2%
```
Nested `position: fixed` inside a scrolling fixed container; inner fixed should escape the scroller and paint above.

### G-ABS-IN-INLINE — 2 tests
```
position-absolute-in-inline-003.html   2.9%
position-absolute-in-inline-004.html   2.3%
```
**What they exercise.** Inline as containing block for abspos descendants. Spec: <https://www.w3.org/TR/css-position-3/#def-cb> + CSS 2.1 §10.1.4 ("if the element is inline-level, the containing block depends on the `direction` property of the container").

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/layout/inline/inline_containing_block_utils.cc` (230 lines) + `.h`.
- Core functions:
  - `InlineContainingBlockUtils::ComputeInlineContainerGeometry(InlineContainingBlockMap*, BoxFragmentBuilder*)` (line 115) — entry point used by the block algorithm when it sees an abspos child whose CB is an inline.
  - `ComputeInlineContainerGeometryForFragmentainer` (line 170) — paginated variant.
  - `GatherInlineContainerFragmentsFromItems<Items>` (line 29) — walks inline fragment items, collects union rects.
- Struct `InlineContainingBlockGeometry { start_fragment_union_rect, end_fragment_union_rect, relative_offset, is_hidden_for_paint }`.

**Algorithm summary.** The inline CB for an abspos child is a bounding box composed of:
1. Union of all fragment rects of the inline container on its **first** line-box → `start_fragment_union_rect`.
2. Union of all fragment rects on its **last** line-box → `end_fragment_union_rect`.
3. The CB passed to OOF sizing is the axis-aligned bounding box of these two rects.
4. `direction` of the container picks which edge is the inline-start for inset resolution (CSS 2.1 §10.1.4).

**Our mirror target.** New file `pkg/layout/inline_containing_block.go`:
- Function `computeInlineContainerGeometry(fragmentTree, inlineNode) -> InlineContainingBlockGeometry`.
- Call from the OOF pass whenever the child's CB is an inline.
- The two failing tests (`position-absolute-in-inline-003/004`) both rely on the correct start/end line handling — once the union-rect logic is in, they resolve.

### G-STICKY — 1 test
```
sticky-top-001.html   3.4%
```
**What it exercises.** `position: sticky; top: 10px` in the middle of content at scroll=0 should stay in normal flow (offset 0), NOT offset by 10px.

**Current behavior.** Our code treats sticky identically to relative (`block_layout.go:929`, etc.), applying `computeRelativeOffset` unconditionally. At scroll=0, the top inset is applied, giving wrong result.

**Blink entry points (studied 2026-04-21):**
- `third_party/blink/renderer/core/page/scrolling/sticky_position_scrolling_constraints.h` (NOT under `core/layout` — important). Struct `StickyPositionScrollingConstraints` holds `PerAxisData { min_inset, max_inset, scroll_container_relative_containing_block_range, scroll_container_relative_sticky_box_range, constraining_range, sticky_offset }` for each axis.
- `third_party/blink/renderer/core/layout/layout_box_model_object.cc`:
  - `ComputeStickyPositionConstraints` (line 528) — runs at layout time, captures scroll-invariant geometry (min/max inset thresholds, sticky box range, CB range).
  - `StickyContainer()` (line 523) — locates the nearest scroll container.
  - `ClearStickyConstraints` — invalidation on geometry change.
- `StickyPositionScrollingConstraints::ComputeStickyOffset(scroll_position, scroll_axes)` — runs at **scroll time**, not layout time. Slides the box until the inset threshold is satisfied, clamped to the CB range.

**Key insight: layout produces a box at the same position as a `position: relative` with zero offset.** Sticky offsets are scroll-time updates, not layout-time offsets. At `scroll=0`, a sticky-top:10px box whose natural flow position already sits ≥10px below the scroll container's top edge yields `sticky_offset = 0`.

**Our current behavior.** `block_layout.go:929` (and peers) apply `computeRelativeOffset` to sticky boxes during layout, unconditionally adding the `top:10px` inset. This makes `sticky-top-001.html` fail at 3.4% — the box appears 10px lower than reference at scroll=0.

**Our mirror target.** New file `pkg/layout/sticky.go`:
- Struct mirroring `StickyPositionScrollingConstraints { min_inset, max_inset, sticky_box_range, cb_range }` per axis.
- Layout-time: compute constraints, **do not apply any offset**. Fragment's `RelativeOffset` stays zero for sticky.
- Scroll-time: `ComputeStickyOffset(scroll_position)` updates the fragment/paint offset.

**Minimum viable fix for sticky-top-001 only.** Short-circuit: treat `position: sticky` as `relative` *but* gate the offset on whether the natural flow satisfies the threshold. At scroll=0 with natural top ≥ inset_top, emit zero offset. This fixes the one failing test without building the full constraint machinery; flag as tech debt until scroll-based tests appear.

### G-REPLACED — 1 test
```
position-absolute-replaced-no-intrinsic-size.tentative.html   2.1%
```
`<img>` with `position: absolute; top:0; bottom:0; height: max-content; width: 100px; margin: auto` on an SVG with `viewBox='0 0 50 50'`. CSS 2.2 §10.3.7 / §10.6.5.

### G-SINGLETONS — 11 tests (includes 3 NORUN)
```
position-relative-001.html                          1.0%  non-table % top/left
position-relative-002.html                          1.0%
position-relative-011.html                          0.4%  tbody %-top shouldn't resolve
position-relative-012.html                          0.4%  tbody position:relative + top:100%
position-relative-013.html                          0.4%  td position:relative + top:100%
stack-floats-001.xht                                1.7%  CSS 2.1 §9.9 stacking order
position-absolute-iframe-print-001.sub.html         0.3%  abspos iframe in pagination
position-absolute-iframe-print-002.sub.html         0.3%
clear-001.xht                                       0.0%  96 px; CSS 2.1 §9.5 clear
position-absolute-dynamic-list-marker.html          0.0%  18 px; ::marker + abspos
position-change.html                                NORUN
replaced-object-backdrop.html                       NORUN
position-absolute-multicol-001.html                 NORUN
```
Mixed shapes; likely several independent root causes. Sweep last.

**Note:** `position-relative-011/012/013` are table-related (`%-top` on `<tr>`/`<tbody>`/`<td>` under position:relative) — they may share a root cause with G-TABLE-REL. If so, closing Phase 1 may also close them. Verify in Phase 1's regression sweep.

## Super-cluster counts
| Cluster | Count | Cumulative if closed |
|---|---|---|
| G-TABLE-REL | 16 | 66 |
| G-DYN-STATIC + G-CB-CHANGE | 9 | 75 |
| G-ABS-CENTER | 5 | 80 |
| G-HYPO | 5 | 85 |
| G-ROOT-FLEX-GRID + G-FIXED | 5 | 90 |
| G-ABS-IN-INLINE | 2 | 92 |
| G-STICKY | 1 | 93 |
| G-REPLACED | 1 | 94 |
| G-SINGLETONS | 11 | 105 (capped at 104 in practice) |
| **Total** | **54 + 5 NORUN** | **104 if all close** |

## Blink study checklist (before Phase 1 code)
- [ ] Read `ng_table_layout_algorithm.cc` for fragment emission order.
- [ ] Read `ComputeRelativeOffset` (likely `layout_object.cc` or `ng_relative_utils.cc`).
- [ ] Find where Blink applies `RelativeOffset` to table sections/rows/cells — `PaintLayer`? Fragment construction?
- [ ] Confirm whether Blink applies relative offsets to `<caption>` (none of our failing tests use caption, so this is a bounds check only).

## Test Results
| Scope | Test count | Baseline | Target |
|---|---|---|---|
| css-position (TestWPTCSS3Reftests) | 104 | 50 PASS / 54 FAIL / 5 NORUN | 104 PASS |
| css-writing-modes (invariant) | 781 | 781 PASS | 781 PASS |
| CSS2 (invariant) | 99 | 99 PASS | 99 PASS |
| css-flexbox (watch) | ~629 | 621 PASS (as of 2026-04-20 baseline) | ≥621 |

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Start with G-TABLE-REL | Highest single-root-cause yield (16 tests); `table_layout.go` clearly missing the branch. |
| Treat NORUN as failing | CLAUDE.md §3 — all tests must pass; cannot silently drop. |
| Do not run css-position category in full except at milestone verifications | CLAUDE.md §4 — only failing-test + adjacent runs during feature work. |
| Preserve wm invariants as hard gate | Phase 5f complete; any wm regression reverts the offending commit. |

## Issues Encountered (for this category)
*(populated as work progresses)*

## Notes
- Attack order is **not** by % diff. Shared-root-cause grouping is prioritised (CLAUDE.md §1).
- Every group's first step is Blink study (CLAUDE.md §2). Do not start coding a group without that.
- Each phase commits at completion of the group plus passing regression sweeps. Intermediate milestones can commit WIP if blocked, but must note the blocker.
