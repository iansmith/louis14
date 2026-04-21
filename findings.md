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

### G-TABLE-REL — 11 tests — **Phase 1 DONE (2026-04-21)**

**Status:** All 11 primary `position-relative-table-*` tests PASS at 0 px diff.
- Part A (shared `AddChild` RelativeOffset) — committed `d174049b`.
- Part B (section fragments for positioned row groups) — committed `ac2dc780`.
- Inline-block baseline fix (§10.8.1 fallback) — committed `b6ec7d3f`. Two edits:
  - `table_layout.go`: removed content-box-end LastBaseline synthesis when no cell has a text baseline. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, LastBaseline is nullopt in this case; the fallback to the bottom margin edge lives at the inline-block site, not at the table.
  - `block_layout.go`: block-child baseline propagation no longer falls back from LastBaseline to Baseline. A block's last-baseline must originate from an actual line box (propagated recursively); otherwise the enclosing inline-block uses §10.8.1's bottom-margin-edge fallback at atomic-inline placement.
  - Unblocked all 12 section tests (thead/tbody/tfoot × {top,left}, tr × {top,left}) plus caption/td tests.

The 8 `-absolute-child` variants remain out of scope for Phase 1 (tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE).
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

#### Phase 4 audit (2026-04-21) — reframed as Blink-parity-first

Audit of our current OOF sizing path (`pkg/layout/out_of_flow_layout.go:82-337`):

**Blink-parity items ALREADY in louis14:**
- `LogicalStaticPosition` (`pkg/layout/static_position.go:25-29`) — fields and edge enums `StaticEdgeStart/Center/End` match Blink's `LogicalStaticPosition::{InlineEdge, BlockEdge}` 1:1.
- `LogicalInsets` (`pkg/layout/writing_mode_converter.go:241-246`) — close to Blink's `LogicalOofInsets`; carries `HasInlineStart` / `HasInlineEnd` / `HasBlockStart` / `HasBlockEnd`.
- Worklist pattern for OOF resolution (`OutOfFlowLayoutPart.LayoutCandidates`, `:58-77`) — mirrored in Phase 5 Part A, `resolvesFixed` gate and all.
- Both container + child WDM already threaded at the resolver.
- Static-position cross-WM conversion (`static_position.go:56-130`) via `ConvertToPhysical` / `ConvertToLogical`.

**Blink-parity items MISSING in louis14:**
1. **`InsetBias` enum** (kStart/kEnd/kEqual). No equivalent exists.
2. **`LogicalAlignment` struct.** `align-self` / `justify-self` are **not** read on abspos children today. Static edges are set per-FC at candidate creation but with no alignment-awareness beyond block-level default (`StaticEdgeStart`).
3. **`InsetModifiedContainingBlock` type.** Today `layoutCandidatesOnce` passes the *raw* CB size to `SetPercentageResolutionSize` (`out_of_flow_layout.go:202-206`) — so `width:50%` on an abspos child resolves against full CB instead of the IMCB.
4. **`LogicalOofDimensions` output struct.** Offsets/sizes are computed inline across `:132-310`; no reusable output shape.
5. **Center-clipping collapse** (`2 × min(static_offset, cb_size − static_offset)` in the both-insets-auto + kEqual branch). Our both-auto case (`:256-272, :300-310`) hard-codes offsets with no alignment bias or clipping.

**Scope boundaries for Phase 4** (named as known non-ports, not hidden gaps):
- Anchor positioning (`LogicalAnchorCenterPosition`, `anchor-center`) — leave `TODO(anchor-positioning)` breadcrumbs at Blink signature positions. Not in any current css-position test.
- Table-specific IMCB clamp (the table-overflow branch of `ComputeInsetModifiedContainingBlock`) — defer.
- Fragmentation column/page OOF — out of scope.

**Call-site surface the port touches:**
- `out_of_flow_layout.go:132-310` — replace entirely with `ComputeOofInlineDimensions` / `ComputeOofBlockDimensions` calls.
- `out_of_flow_layout.go:202-206` — pass IMCB size to percentage-resolution setter.
- `OutOfFlowCandidate` struct (`out_of_flow_layout.go:9-28`) — add `Alignment LogicalAlignment`.
- Candidate-creation sites: `block_layout.go:245-253` (block-level default: kStart/kStart), plus flex/grid sites that currently exist but don't propagate alignment. Grid/flex must set `StaticEdgeCenter` when parent uses center alignment per the flex static-position spec.

#### Phase 4 Commit 2 landing (2026-04-21, commit `d9f6628b`)

Wire-up complete. Closed 4 of 5 G-ABS-CENTER tests — `position-absolute-center-001/003/004/006` — all at 0 pixel diff. Residual: `position-absolute-center-002` (vertical-rl abspos inside column flex with align-items:center; 0.8% diff); `position-absolute-center-007` (`display:table` + both block insets + auto margins inside a `margin-top:-50px` wrapper; 2.1% diff). Both pushed to Commit 3.

**What shipped in Commit 2:**
1. `layoutCandidatesOnce` now uses `ComputeUnclampedIMCB` → `ComputeMargins` → `ComputeInsets`. Static positions shifted into CB-padding-box (`+ containingBlockPadding.Start`) on input and back to CB-content-box on output (`- containingBlockPadding.Start`). IMCB size feeds percentage resolution inside the constraint space build.
2. Pre-layout fixed-size (both axes) when both insets specified and the child's size is auto: `IMCB.size - non-auto-margins - child-BP`.
3. `OutOfFlowCandidate.Alignment LogicalAlignment` added. Zero value (ItemPositionNormal) yields BiasStart — preserves behavior for sites that don't yet populate it (block, grid, inline, table).
4. Flex OOF capture derives `StaticPosition.InlineEdge` / `.BlockEdge` from the container's `justify-content` (main) + `align-items` (cross) via new `flexOOFStaticMain` / `flexOOFStaticCross` helpers. Main→inline/block mapping is row-vs-column.
5. `absolute_utils.go` both-auto BiasEqual branch arms `defaultInsetBias = BiasStart` so overflowing centered abspos snap to the start edge (Blink parity).
6. `ComputeUnclampedIMCB` propagates a static-center overflow flag (both insets auto + `StaticEdgeCenter`) into `InsetModifiedContainingBlock.InlineHasDefaultAlignmentOverflow` / `BlockHasDefaultAlignmentOverflow` so the default-overflow fallback fires for statics too, not just alignment.
7. Indefinite-cbBlock fallback preserves per-case formulas for the block axis when IMCB math isn't meaningful.

**Deferred to Commit 3.** `center-002`: probe vertical-rl cross-axis sizing under column flex — suspect `flexOOFStaticCross` misses a writing-mode conversion between the container's align-items axis (parent WDM) and the child's abspos static-edge axis (child WDM). `center-007`: probe `display:table` intrinsic sizing — `width:100px` is specified but block-axis is intrinsic; verify that IMCB's both-insets-specified + auto-block path passes through to the child with the correct available-block so table sizing picks 100px.

### G-CB-CHANGE — 3 tests — **Phase 2 audit invalidated the grouping (2026-04-21)**
```
containing-block-change-scrollframe.html               10.4%
containing-block-change-button.html                    4.2%
absolute-pos-box-inside-fixed-pos-box-with-changing-height.html  0.5%
```
**Audit finding (2026-04-21).** The Blink "invalidation-only" model does not apply to our codebase. Our harness (`pkg/visualtest/helpers.go:85-102`) **already** throws away `engine1` and runs `engine2 := layout.NewLayoutEngine(...)` from scratch on the post-JS DOM. That's the moral equivalent of `RemovePositionedObjects` + relayout — there is no caching to invalidate. JS mutations *do* land on the DOM (verified: `fixed.style.height = "300px"` writes `"height: 300px"` to the inline-style attribute and pass-2 sees it).

The 3 tests fail for **heterogeneous, non-CB-change** reasons:

1. **`absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5%)** — our layout output box-tree is missing `position:fixed` boxes. Debug dump showed `<div style="position:absolute">` collapsed to `0×0` with no children rendered, and the inner `#fixed` box absent entirely. Likely a foundational gap: positioned-fragment propagation into the principal box tree (or render walk skipping `OutOfFlow` lists). Not a "CB change" issue.

2. **`containing-block-change-button` (4.2%)** — confounded with a `<button>` vertical-centering rendering bug. The reference renders an in-flow `<div>` inside `<button id=button>` with `padding:0` and expects browser default behaviour (vertical-center its 100×100 child in the 400×400 button → green box at viewport (50,200)). Our reference render shows green at viewport (50,50) — i.e. button is NOT vertical-centering content. Until that is fixed, the test cannot pass even with perfect CB-change handling.

3. **`containing-block-change-scrollframe` (10.4%)** — needs *two* unimplemented features: `Element.scrollTop` JS setter (not present in `pkg/js`), and `overflow:hidden` paint-time scroll honoring. Without `scrollTop`, the bottom-`#bottom` div sits at viewport y=800 (off-screen) and the abspos sits at viewport y=500 (clipped by `overflow:hidden`). Both green boxes invisible. Still not a "CB change" issue.

**Planning consequence.** Phase 2 as originally designed (mirror Blink's `NeedsPositionedLayout` + `RemovePositionedObjects`) is a **no-op** for our codebase. Each test should be reclassified into its real category. Provisional re-grouping:
- `absolute-pos-box-inside-fixed-pos-box-with-changing-height` → **G-FIXED** (positioned-fragment box-tree gap, overlaps with the existing `position-fixed-scroll-nested-fixed` test).
- `containing-block-change-button` → **G-SINGLETONS** (`<button>` vertical-centering bug).
- `containing-block-change-scrollframe` → new sub-group **G-SCROLL** (needs `Element.scrollTop` setter + `overflow:hidden` scrolling paint). May share with `hypothetical-box-scroll-*` (currently listed NORUN due to `window.scrollTo`).

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

### G-DYN-STATIC — 6 tests — **CLOSED 2026-04-21 (Parts a+b+c+d)**
```
position-absolute-dynamic-static-position-floats-001.html   0.7% → 0% ✓ (b)
position-absolute-dynamic-static-position-floats-002.html   0.3% → 0% ✓ (b)
position-absolute-dynamic-static-position-floats-003.html   0.3% → 0% ✓ (b)
position-absolute-dynamic-static-position-floats-004.html   0.7% → 0% ✓ (b,d)
position-absolute-dynamic-static-position-inline.html       2.1% → 0% ✓ (a)
position-absolute-dynamic-static-position-table-cell.html   2.1% → 0% ✓ (c)
```
**What they exercise.** JS flips a property (float insertion, `display: inline → block`, table-cell vertical-align interaction) that changes the abspos child's static position. Triggers re-layout; the new static position must be picked up.

**Audit finding (2026-04-21):** the original plan's hypothesis ("static position is cached; add `OutOfFlowPositionedDescendants` rebuild") is **WRONG** for our codebase. Like G-CB-CHANGE, we already rebuild every pass — `pkg/visualtest/helpers.go:85-102` uses a fresh `engine2 := layout.NewLayoutEngine(...)` on the post-JS DOM, no caching. Confirmed by instrumenting the `inline` test: post-JS, `target.style.display='block'` reaches `computed display: block` in the 2nd pass's `ComputedStyles()`, but the RENDERING still placed target beside the inline-block (where display:inline static position points) instead of below (where display:block belongs). The bug was purely in how we COMPUTE static position per-FC, not in whether we recompute.

#### Per-site root causes and fixes

**(a) inline_layout.go — DONE 2026-04-21, commit `233d408f`.**
- *Bug:* line loop captured OOF candidates at `(inlinePos, blockOffset)` regardless of the child's originally-specified `display`.
- *Fix:* splits on `isInlineLevelDisplay(style.GetDisplay())` (new helper mirroring Blink's `ComputedStyle::IsOriginalDisplayInlineType`):
  - inline-level abspos → `(inlinePos, blockOffset)` (emitted immediately).
  - block-level abspos preceded by in-flow content on the line → deferred to `(0, blockOffset + lineHeight)` after `createLineBoxEx` finalises the line height.
  - block-level abspos NOT preceded by any in-flow content → emitted immediately at `(0, blockOffset)`.
- *Refinement that blocked the first attempt:* initially I emitted ALL block-level OOFs at `(0, blockOffset + lineHeight)`. That regressed 4 orthogonal-float wm tests (`float-{lft,rgt}-orthog-v{lr,rl}-in-htb-002/003`) whose REFERENCE HTML places `<div id="orthog-vert" position:absolute>` as the first child of an inline FC. Those tests had passed in the pre-fix state because the old code emitted at `(inlinePos=0, blockOffset=0)`. My "always use lineHeight" version captured at `(0, 40)`, moving the abspos 40px down. The fix was to mirror Blink's `line_box_.LineBoxBlockEnd()` which is read AT THE TIME OF ENCOUNTER — if no in-flow content has been placed yet on the line, `LineBoxBlockEnd() == 0`, not `lineHeight`. The `hasInflowOnLine` flag accumulates this as the loop iterates.
- *Result:* `inline` (2.1% → 0%), wm 781/781 preserved.

**(b) block_layout.go — DONE 2026-04-21, commit `d250c5cf`.**
- *Bug:* block-FC abspos hardcoded `InlineOffset: 0`, ignoring float exclusions and inline-level-abspos semantics.
- *Fix:* when `isInlineLevelDisplay(childStyle.GetDisplay())` is true, query `exclusionSpace.FindAvailableInlineSize(bfcBlockOrigin + staticBlockOffset, 0, bfcContainerInlineSize)` and use the returned inline-start consumption directly as `InlineOffset`.
- *Result:* `floats-001` (0.7% → 0%), `floats-002` (0.3% → 0%), `floats-003` (0.3% → 0%), `floats-004` RTL (0.7% → 0%).
- *Subtlety learned while debugging:* my first attempt subtracted `bfcInlineOrigin` from the query result (I'd assumed `FindAvailableInlineSize` returned BFC-absolute offsets). The target then rendered at local `(22, 0)` instead of `(40, 0)`. See "ExclusionSpace coordinate-system note" below.

**(c) table-cell — DONE 2026-04-21.**
- *Test:* `position-absolute-dynamic-static-position-table-cell` (2.1% → 0%).
- *Scenario:* abspos inside `display:table-cell; vertical-align:middle` with post-JS `translate:0 -50px; top:auto`.
- *Expected:* cell's vertical-align centers the hypothetical (anonymous) box vertically within the cell; the abspos static-position block-offset reflects that centering; target then paints 50px above.
- **Two-part bug.** The original plan hypothesised a single fix at a table-cell capture site. Actual investigation (instrumentation + pixel scanner) found two independent bugs, both needed:
  1. **Orphan `display: table-cell` doesn't go through `table_layout.go`.** The test's `<div style="display:table-cell">` has no `<table>` ancestor, so `normalizeTableSubtrees` in `layout_tree_builder.go` doesn't wrap it (reverse §17.2.1 anonymous-table generation is unimplemented). Layout dispatches to `block_layout.go`, which had no vertical-align handling. The proper-table path in `table_layout.go` was already correct for this phase.
  2. **Transform parser percent-sentinel collision.** `parseTransformValue` encoded percentages by sign-flipping the number (`result := -percent`), intending sign as a percent-vs-length sentinel. A legitimate negative pixel length `-50px` was stored as `-50.0`, then re-interpreted at paint time as `-50%` → resolved to `+50px`. `translate: 0 -50px` rendered as `+50px`, flipping the sign of every negative-pixel translate.
- *Fix 1 — orphan-cell vertical-align (block_layout.go):* after layout, if `bla.style.GetDisplay() == css.DisplayTableCell && bla.space.TableSectionData == nil && finalBlockSize > intrinsicBlockSize`, compute `vaShift` from `vertical-align` (`middle` → half the surplus, `bottom` → full surplus) and add it to both `builder.children[i].offset.BlockOffset` and `builder.outOfFlowCandidates[i].StaticPosition.Offset.BlockOffset`. `TableSectionData == nil` guard ensures the proper-table path (when it eventually needs the same behaviour) isn't double-shifted.
- *Fix 2 — transform parser (pkg/css/style.go):* replaced the sign-sentinel with an explicit `IsPercent []bool` on the `Transform` struct. New signatures:
  - `parseTransformValue(val string) (value float64, isPercent bool, ok bool)`
  - `GetIndividualTranslate() (tx float64, ty float64, txPercent bool, tyPercent bool, ok bool)`
  - `Transform.Values` pairs with `Transform.IsPercent` by index; paint-time resolvers (`pkg/render/paint_layer.go` shorthand + individual cases) read `IsPercent[i]` instead of checking sign. Percent values are resolved as `(v / 100) * boxDim`.
  - Migrated 3 `louis13/` callers to the new signature (louis13 shares the same module).
- *Not shipped — proper-table-path vertical-align capture.* First attempt also touched `table_layout.go`: changed `contentBlockSize` from post-stretch cellLogical.BlockSize() to pre-stretch `IntrinsicBlockSize + borders + paddings`, and applied `vaBlockShift` to propagated OOF candidates. Dropped because (i) the target test doesn't exercise the proper-table path, and (ii) the `contentBlockSize` change regressed 3 wm tests (`box-offsets-rel-pos-vlr-005`, `box-offsets-rel-pos-vrl-004`, `orthogonal-cell-001`). Will revisit when a test actually exercises vertical-align centering of abspos descendants inside a real `<table><td>...` — the structural pattern (va-shift applied to OOF candidates during row sweep) is correct but needs the `contentBlockSize` shape debugged against orthogonal writing-mode cases before landing.
- *Verification:* target test passes at 480000/480000 pixels, max diff 0. wm 781/781 held. css-position 67 → 68 PASS (+1). css-transforms 162 → 171 PASS (+9, from the percent-sentinel fix correcting other translate cases). css-flexbox 626/629 unchanged (3 pre-existing failures).

**(d) RTL awareness — CLOSED INCIDENTALLY by (b).**
- Initial concern: `floats-004` is the RTL variant and I expected a separate `direction`-aware flip of the inline edge annotation.
- Actual finding: `ExclusionSpace` already uses `PhysicalFloatToExclusionSide`-normalised sides. `ExclusionInlineStart` means "visual-start in the direction of content flow", so `FindAvailableInlineSize(...)` returns the correct inline-start consumption for both LTR and RTL floats. The query in (b) is direction-agnostic.

#### Coordinate-system notes (learned 2026-04-21 while fixing (b))

The `ExclusionSpace` comment claims floats are stored "BFC-relative". **In practice floats are stored with LOCAL inline offsets** — the offset recorded at `Exclusion.InlineOffset` is what `floatInlineOffset` computed in `layoutFloat`, which is measured from the enclosing block's content-box inline-start (NOT from the BFC root's content-box inline-start). `FindAvailableInlineSize`'s `containerInlineSize` parameter is only used for END-side float consumption (`containerInlineSize - e.InlineOffset`); start-side consumption (`e.InlineOffset + e.InlineSize`) ignores it.

This means:
- Callers in the same enclosing block that owns the exclusion space can use the returned inline-start value directly as a local offset. This is what the in-flow inline-layout line-start recomputation does (`inline_layout.go` around line 820) — it adds `bfcInlineOrigin` to build a BFC value for clarity, then subtracts it again to land on `lineInlineOffset = local`.
- The Phase 3(b) capture site can use the value directly without any translation.
- A float that crosses nesting levels (e.g. a float placed inside a non-BFC child, queried from an ancestor) is a known inconsistency but does not affect the current tests.

This invariant is not documented in the `ExclusionSpace` file; it's implicit in how `layoutFloat` and the line-start recomputation currently pair up. Do NOT add a `- bfcInlineOrigin` correction to readers — it will silently offset by the parent's border/padding. If we ever normalise the exclusion space to BFC-absolute coords, readers AND writers must be updated together.

#### Blink entry points (re-validated 2026-04-21)
- `third_party/blink/renderer/core/layout/inline/inline_layout_algorithm.cc`:
  - `HandleOutOfFlowPositioned` — splits on `style.IsOriginalDisplayInlineType()`:
    - Inline-level: `(current_inline_cursor, line_block_start)`.
    - Block-level: `(0, line_box_.LineBoxBlockEnd())` — block-end read at the time of encounter, NOT at end-of-line (key subtlety; see refinement under (a) above).
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc`:
  - Abspos is handled in `HandleOutOfFlowPositioned`; for inline-level display, the hypothetical inline box's line-start is used (equivalent to our `FindAvailableInlineSize` return).
- `third_party/blink/renderer/core/layout/table/table_cell_layout_algorithm.cc`: `intrinsic_padding_before` (vertical-align translation) is applied before OOF propagation.
- `third_party/blink/renderer/core/layout/out_of_flow_layout_part.cc`: already mirrored in `pkg/layout/out_of_flow_layout.go` (Phase 5 G-FIXED Part A).

#### Key insights (corrected 2026-04-21, with (a)+(b) hindsight)
- **The bug class is per-FC capture, not cache invalidation.** Every FC that emits OOF candidates needs its own Blink-faithful computation of the static position. `OutOfFlowPositionedDescendants` as a LayoutResult field would be a no-op because our harness already re-lays out fresh.
- **Blink's "at time of encounter" contract matters.** Whenever a handler reads a line-level or flow-level metric (like `LineBoxBlockEnd()`), the value at the time of handling — not at end-of-pass — is what matters. Incremental tracking (the `hasInflowOnLine` flag) is the right primitive, not deferred post-processing.
- **RTL is often free if your physical-to-logical normalisation is already push-down.** `PhysicalFloatToExclusionSide` normalises at write time, so readers get direction-agnostic results. When a capture site regresses in RTL, the fix is usually in the normalisation layer, not at the capture site.
- **Check the exclusion-space coordinate system before subtracting origins.** Our exclusions are stored with local inline offsets. A naive "translate to local" step was the difference between 22px and 40px in Phase 3(b) debugging.

#### Remaining and downstream
- G-DYN-STATIC is now fully closed (6/6). All per-FC capture sites compute static position correctly.
- **Prerequisite for G-HYPO satisfied.** The hypothetical-box algorithm reads static position via this same path. IMCB work (Phase 4) can proceed with confidence that static-position inputs are Blink-faithful across inline / block / table-cell formatting contexts.
- **Tech debt recorded for proper-table path.** When a future test exercises vertical-align centering of abspos descendants inside a real `<table><td>`, revisit `table_layout.go`'s OOF-candidate shift — the structure is designed but was dropped because the `contentBlockSize` pre-stretch change regressed 3 wm tests in orthogonal writing modes.

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

#### Phase 4 Commit 2 results (2026-04-21, commit `d9f6628b`)

`hypothetical-dynamic-change-001` and `-002` now PASS at 0 pixel diff. Closed by two changes in Commit 2, not by the IMCB port alone:
- Flex container's `justify-content: center` + `align-items: center` now populate `StaticPosition.InlineEdge`/`BlockEdge` on propagated OOF candidates (was `StaticEdgeStart`).
- Propagated OOF candidates from a laid-out OOF ancestor had their `StaticPosition.Offset` in the ancestor's content-box coordinates, but `layoutCandidatesOnce` was re-adding them to the worklist without translating to the CB's content-box. Fix: shift by `(finalInlineOffset + parentBP.InlineStart, finalBlockOffset + parentBP.BlockStart)` — mirrors `block_layout.go`'s `PropagateOOFCandidates`.

**Residual: `hypothetical-dynamic-change-003` (4.2%).** Different root cause — `position: relative` ancestor's visual `left:100px` must propagate into the fixed descendant's static position when the descendant is OOF-resolved at the ICB. Today our normal-flow capture records the relative ancestor's in-flow position (0, 0); the relative offset is applied at paint time via `fragment.RelativeOffset` and never reaches the OOF worklist. Blink computes the "accumulated container offset" during `PropagateOOFPositionedInfo` and includes the ancestor's relative translation. **Fix scope:** during OOF propagation in `block_layout.go` `PropagateOOFCandidates`, when the containing `childResult.Fragment` has a non-zero `RelativeOffset`, add that offset (in parent's logical axes) to `adj.StaticPosition.Offset` before appending. Pushed to Commit 3.

### G-ROOT-FLEX-GRID — 4 tests
```
position-fixed-root-element-flex.html    0.8%
position-fixed-root-element-grid.html    0.8%
position-absolute-root-element-flex.html 0.8%
position-absolute-root-element-grid.html 0.8%
```
**What they exercise.** `<html>` element with `position: fixed|absolute` and `display: flex|grid`. Insets define the box size relative to ICB; not a shrink-to-fit.

**Blink entry point:** `layout_view.cc` + flex/grid root-element special-cases.

### G-FIXED — 2 tests (1 closed 2026-04-21)
```
absolute-pos-box-inside-fixed-pos-box-with-changing-height.html  0.5% → 0% PASS  (closed)
position-fixed-scroll-nested-fixed.html                          4.2% → 1.0%      (paint-clip residual)
```

**Status (2026-04-21).** Foundational OOF re-entrance fix landed. Closes test #1; reduces test #2 from 4.2% to 1.0%. The remaining 1.0% is paint/scroll territory (fixed must escape `overflow:auto` clip and `outer.scrollTop=200` requires JS scrollTop setter), not OOF layout — pushed to G-SCROLL / paint-time work.

**Root cause (was; fixed 2026-04-21).** Single foundational bug: `OutOfFlowLayoutPart.LayoutCandidates` (`pkg/layout/out_of_flow_layout.go:177`) called `layoutElement(child)` to lay out each OOF candidate, then added the child's fragment to the builder — but **silently dropped** `childResult.PropagatedOOFCandidates`. Any OOF descendant of an OOF candidate was lost.

**Fix shape applied.** `LayoutCandidates` rewritten as worklist loop mirroring Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`. After each child layout, `childResult.PropagatedOOFCandidates` is partitioned: at sites that act as the CB for fixed (root, transform/containment CB) the descendants are appended to the worklist and resolved by the same CB; at ordinary positioned sites only absolute is resolvable here, so fixed is returned to the caller for further propagation. Added `resolvesFixed bool` field on `OutOfFlowLayoutPart`; updated all 7 call sites in block/flex/grid/multicol/table layout. Method now returns `[]OutOfFlowCandidate` (unresolved fixed) which positioned callers append into their own propagated-fixed list.

Block/flex/grid/table layout algorithms all propagate correctly via `result.PropagatedOOFCandidates` (verified — see refs in `block_layout.go:526,587,914`, `flex_layout.go:737,982,1123,1817`, `grid_layout.go:314,391`, `table_layout.go:785,789,956,960,1099`, `multicol_layout.go:271`). Re-collection is implemented in formatting-context parents. The hole is exclusively in `LayoutCandidates` — the OOF resolution loop that ought to be re-entrant.

**Per-test trace.**

1. **`position-fixed-scroll-nested-fixed`** (4.2%):
   - `<div id=outer>` is `position:fixed` → propagated up to root.
   - Root's `OutOfFlowLayoutPart.LayoutCandidates` lays out `outer` via `layoutElement`.
   - Inside `outer`'s block layout, the inner `<div style="position:fixed">` propagates up out of `outer` (because `outer` is positioned, the code at `block_layout.go:879-903` correctly propagates fixed candidates upward).
   - Inner fixed lands on `outer`'s `LayoutResult.PropagatedOOFCandidates`.
   - `LayoutCandidates` ignores that, attaches only `outer`'s fragment, never resolves inner fixed against ICB.
   - **Test image**: red 100×100 outer visible, inner green 200×100 missing entirely.

2. **`absolute-pos-box-inside-fixed-pos-box-with-changing-height`** (0.5%):
   - `<div style="position:absolute">` propagates up.
   - Its layout produces propagated `<div id=fixed>` (fixed inside abspos parent → propagates further).
   - When `<div id=fixed>` finally resolves at root and is laid out via `layoutElement`, its child `.box` (also abspos) propagates as `PropagatedOOFCandidates` on `#fixed`'s result. `LayoutCandidates` drops it.
   - Verified by debug box-tree dump: post-layout principal boxes show only `<html>` and the abspos wrapper at `0×0`; `#fixed` and `.box` absent.

**Likely wider impact.** This bug surfaces whenever an OOF box has OOF descendants. Expected affected tests (subset of css-position failures, conjecture pending verification):
- `position-fixed-scroll-nested-fixed` ✓ confirmed
- `absolute-pos-box-inside-fixed-pos-box-with-changing-height` ✓ confirmed
- Possibly the 8 `position-relative-table-*-absolute-child` variants (currently classified G-ABS-IN-INLINE / G-ABS-IN-TABLE) — though those have a different setup
- Possibly several `position-fixed-root-element-{flex,grid}` and `position-absolute-root-element-{flex,grid}` (G-ROOT-FLEX-GRID), if those involve nested OOFs

**Blink reference.** Blink's `OutOfFlowLayoutPart::LayoutOOFNodes` is the recursive entry point. After laying out each OOF candidate, it inspects the produced fragment for descendant OOFs (via `LayoutResult::OutOfFlowPositionedDescendants()`) and either:
- Re-runs OOF layout for absolute descendants whose CB is the just-laid-out box (the box is positioned, so it's the new CB).
- Continues propagating fixed descendants up to the ICB resolution.
The control structure is a worklist loop, not a single pass.

**Fix shape (proposed, ready to implement).** In `LayoutCandidates`:
1. After `childResult := layoutElement(child, childSpace)`, partition `childResult.PropagatedOOFCandidates`:
   - **Absolute candidates** with CB = the just-laid-out child → resolve them inline by spinning up a new `OutOfFlowLayoutPart` with `child`'s fragment geometry as the CB.
   - **Fixed candidates** → if we're at the root (ICB), resolve them in this same pass; otherwise return them on the result so the calling formatting context can re-propagate.
2. Make `LayoutCandidates` return a `[]OutOfFlowCandidate` of unresolved-fixed candidates, so the root's call (block_layout.go:858) can iterate until empty.
3. Add a guard against infinite loops (cycle in OOF propagation should be impossible per spec, but a depth limit costs nothing).

**Scope to confirm before coding.** Suggest a quick sweep: run the 8 `*-absolute-child` table variants and the 4 `position-{fixed,absolute}-root-element-{flex,grid}` after the fix; if many close, this single foundational fix could close 10+ tests in one commit.

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
Updated 2026-04-21 post Phase 4 Commit 2 (IMCB wire-up + flex alignment capture + propagated-OOF coordinate translation).

| Cluster | Status | Closed | Remaining | Cumulative passing |
|---|---|---|---|---|
| G-TABLE-REL | DONE (Phase 1) | 11 + position-relative-012 | 8 `-absolute-child` (moved to G-ABS-IN-INLINE/TABLE) | 62 |
| G-FIXED | Part A done (Phase 5a) | 1 | 1 (paint-clip residual, → G-SCROLL) | — |
| G-DYN-STATIC | DONE (Phase 3) | 6 | 0 | 68 |
| G-ABS-CENTER | **Phase 4 Commit 2 partial** | 4 | 1 (`center-002` vertical-rl, `center-007` display:table → Commit 3) | 72 |
| G-HYPO | **Phase 4 Commit 2 partial** | 2 | 1 (`hypothetical-003` relative-offset propagation) + 2 NORUN | **74** |
| G-ROOT-FLEX-GRID | open | 0 | 4 | — |
| G-ABS-IN-INLINE | open | 0 | 2 + 8 table abs-child variants | — |
| G-STICKY | open | 0 | 1 | — |
| G-REPLACED | open | 0 | 1 | — |
| G-SCROLL | open | 0 | 1 (`containing-block-change-scrollframe`) + G-FIXED Part B | — |
| G-SINGLETONS | open | 0 | 11 | — |
| **Total** | — | **24** | **30 (+ 4 SKIPs out of scope)** | **74 / 100 runnable** |

## Blink study checklist (before Phase 1 code)
- [ ] Read `ng_table_layout_algorithm.cc` for fragment emission order.
- [ ] Read `ComputeRelativeOffset` (likely `layout_object.cc` or `ng_relative_utils.cc`).
- [ ] Find where Blink applies `RelativeOffset` to table sections/rows/cells — `PaintLayer`? Fragment construction?
- [ ] Confirm whether Blink applies relative offsets to `<caption>` (none of our failing tests use caption, so this is a bounds check only).

## Test Results
| Scope | Test count | Baseline | Current (2026-04-21) | Target |
|---|---|---|---|---|
| css-position (TestWPTCSS3Reftests) | 104 | 50 PASS / 54 FAIL / 5 NORUN | **74 PASS / 30 FAIL** (post Phase 4 Commit 2) | 100 PASS (4 SKIPs out of scope) |
| css-writing-modes (invariant) | 781 | 781 PASS | 781 PASS | 781 PASS |
| CSS2 (invariant) | 99 | 99 PASS | 99 PASS | 99 PASS |
| css-flexbox (watch) | 629 | 621 PASS | 626 PASS / 3 FAIL | ≥621 |
| css-transforms (watch) | 381 | 162 PASS | **171 PASS / 210 FAIL** (+9 from percent-sentinel fix) | improve opportunistically |

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Start with G-TABLE-REL | Highest single-root-cause yield (16 tests); `table_layout.go` clearly missing the branch. |
| Treat NORUN as failing | CLAUDE.md §3 — all tests must pass; cannot silently drop. |
| Do not run css-position category in full except at milestone verifications | CLAUDE.md §4 — only failing-test + adjacent runs during feature work. |
| Preserve wm invariants as hard gate | Phase 5f complete; any wm regression reverts the offending commit. |

## Issues Encountered (for this category)

### IMCB center-clipping default-overflow must fire for statics too (fixed 2026-04-21)
Phase 4 Commit 2 debugging, `position-absolute-center-001`. After wiring up the IMCB the test still failed at 0.4% — the 100px-wide abspos inside a 40px flex main-size landed at `(freeSpace / 2)` instead of the start edge. Blink's center-clipping collapse `2 × min(static, cb − static)` produces a zero-size IMCB for this case (static=20, cb=40 → 2×20=40, then clipped-symmetric gives 0 because the child overflows by 60). The BiasEqual branch then split the remaining negative free space equally, centering the overflow — wrong.

Fix: the both-auto BiasEqual branch now emits `defaultOut, hasDefaultOut = BiasStart, true`, and `ComputeUnclampedIMCB` propagates a static-center overflow flag so the default-overflow fallback in `ComputeInsets` fires whenever `StaticEdgeCenter` is the static bias and both insets are auto — not only when alignment is center. Mirrors Blink's arm-the-fallback-on-any-center-source behavior.

### Propagated OOFs from an ancestor OOF need coordinate translation (fixed 2026-04-21)
Phase 4 Commit 2 debugging, `hypothetical-dynamic-change-001`. When `LayoutCandidates` lays out an OOF ancestor (e.g. a fixed container), the child's normal-flow pass produces `PropagatedOOFCandidates` whose `StaticPosition.Offset` is in the ancestor's content-box coordinates. `layoutCandidatesOnce` was appending them to the worklist as-is, so they'd be re-processed as if they were already in the CB's content-box — placing the descendant at the ancestor's origin instead of at the ancestor's resolved position within the CB.

Fix: drain moved to after `finalInlineOffset` / `finalBlockOffset` are computed. Each propagated candidate's offset is shifted by `(finalInlineOffset + parentBP.InlineStart, finalBlockOffset + parentBP.BlockStart)`. Cross-WM physical round-trip applied when `childWDM != wdm`. Mirrors `block_layout.go`'s `PropagateOOFCandidates` — same invariant (candidate static positions are always CB-content-box-relative on the worklist).

### Transform parser — percent-vs-length sign-sentinel collision (fixed 2026-04-21)
While debugging Phase 3(c) (`position-absolute-dynamic-static-position-table-cell`) I confirmed via instrumentation that layout was correct (static block-offset = 50, no positioning insets). Pixel-scanning the test output showed the target rendering 100px *below* its expected location — the translate `0 -50px` was being applied as `+50px`.

Root cause: `parseTransformValue` in `pkg/css/style.go` used negative numbers as a sentinel for percentage values (`result := -percent`). A legitimate `-50px` pixel length also encodes negatively, so `paint_layer.go` misread it as `-50%` and resolved it to `+50px`. Sign-flipped every negative-pixel translate.

Fix: added `IsPercent []bool` to the `Transform` struct. Widened signatures:
- `parseTransformValue(val string) (value float64, isPercent bool, ok bool)`
- `GetIndividualTranslate() (tx, ty float64, txPercent, tyPercent, ok bool)`

Paint-time resolvers now read `IsPercent[i]` per component instead of sign-checking. Same pattern works for both shorthand `translate()` and individual `translate` property.

Updated callers in `pkg/render/paint_layer.go` plus 3 `louis13/` sites (`stacking.go`, `containing_block.go`, `render.go` — louis13 shares the module).

Net: +1 Phase 3(c) target test, +9 css-transforms tests closed for free (other negative-pixel translate cases). Zero regressions in wm / CSS2 / flex.

## Notes
- Attack order is **not** by % diff. Shared-root-cause grouping is prioritised (CLAUDE.md §1).
- Every group's first step is Blink study (CLAUDE.md §2). Do not start coding a group without that.
- Each phase commits at completion of the group plus passing regression sweeps. Intermediate milestones can commit WIP if blocked, but must note the blocker.
