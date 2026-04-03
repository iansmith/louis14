# Remaining Work Plan: WM + Flexbox Test Improvements

**Date**: 2026-04-03
**Branch**: `rewrite/louis13-louis14`
**Current**: WM 413/790 (52.3%), Flexbox 380/630 (60.3%)
**Previous baseline**: WM 402/790 (after Phase 1), Flexbox 380/630

---

## Principles

1. **Study Blink first** — before writing code in any area, read how Blink/Chromium handles it. Match their types, algorithms, and architectural patterns.
2. **Foundational correctness** — fix root causes that affect ALL cases, not just tests near the threshold. A 0.5% diff is a failure just like 28%.
3. **No easy-win hunting** — don't scan for tests near 0.1% to flip. Fix the underlying algorithm so entire test families pass.
4. **Checkpoint and commit** — commit after each fix, run tests, report counts before moving on.

---

## Architecture Quick Reference

WPT visual tests run against the **new engine** (`pkg/layout/` + `pkg/render/`).
The **old engine** (`louis13/pkg/layout/` + `louis13/pkg/render/`) is still used by
other code paths but is NOT exercised by the test suite. Phase 3 work focuses
entirely on the new engine.

**Old engine key files**: `louis13/pkg/layout/layout_block.go`, `layout_flex.go`, `layout_table.go`, `layout_inline_multipass.go`, `absolute_positioning.go`
**New engine key files**: `pkg/layout/block_layout.go`, `flex_layout.go`, `inline_item.go`, `bidi.go`, `engine.go`

**Test commands**:
- WM: `go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS:'`
- Flexbox: `go test -v -run TestWPTCSS3Reftests/css-flexbox ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS:'`
- Specific: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/float' ./pkg/visualtest/ -timeout 120s`

---

## Phase 1 — Critical Foundational Fixes (WM)

These fix root causes affecting large test families. Each should be done in order, with tests run after each.

### 1.1 Text Fragment Paint Order (~120 WM tests)

**Files**: `pkg/layout/engine.go` line ~232
**Blink reference**: `NGPhysicalTextFragment` never carries `position`. Only `NGPhysicalBoxFragment` can be positioned.

**Bug**: `fragmentToBox()` sets `box.Position = box.Style.GetPosition()` for ALL fragments including text. Text fragments inherit their parent's style (including `position: relative`), causing them to enter the positioned-element paint layer instead of FlowChildren.

**Fix**: Guard with fragment type check:
```go
if box.Style != nil && frag.Type != FragmentText {
    box.Position = box.Style.GetPosition()
}
```

**Verification**: Run `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced' ./pkg/visualtest/ -timeout 300s`

### 1.2 Float/Clear Physical-to-Logical Mapping (~20-34 WM tests)

**Files**: `louis13/pkg/layout/layout_block.go` (~lines 427-454), `louis13/pkg/layout/exclusion_space.go`
**Blink reference**: `ResolveFloating()` in `style_utils.cc` maps physical float values to logical `EFloat`. `NGExclusionSpace` works entirely in logical coordinates.

**Bug**: CSS `float: left`/`right` are physical values. In RTL writing modes (HTB-RTL, VRL-RTL, VLR-RTL), they must be mapped to the opposite logical side before passing to ExclusionSpace. Currently no mapping exists.

**Fix**: Create `MapPhysicalFloatToLogical(side, wdm)`:
- HTB-LTR: left=inline-start, right=inline-end (identity)
- HTB-RTL: left=inline-end, right=inline-start (swap)
- Vertical-LTR: left=inline-start (identity)
- Vertical-RTL: swap

Apply in `layoutFloat()` before ExclusionSpace and in `ClearanceOffset()` for clear values. Also verify float positioning correctness in vertical modes via `ToPhysicalOffset`.

**Verification**: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/float' ./pkg/visualtest/ -timeout 120s`

### 1.3 Bidi Mirroring in New Engine (compile fix + ~57 WM tests)

**Files**: `pkg/layout/engine.go` (~line 207), `pkg/layout/bidi_demo_test.go`
**Blink reference**: UAX#9 L4 — characters with `Bidi_Mirrored=Y` must be mirrored when resolved bidi level is odd (RTL).

**Bug**: `reverseRunes()` reverses rune order but does NOT apply UAX#9 L4 mirroring. Also, `bidiMirror()` is called in `bidi_demo_test.go` but only defined in the old engine (`louis13/`), causing a compile error.

**Fix**:
1. Define `bidiMirror()` in the new engine's `pkg/layout/` package (port from `louis13/pkg/layout/layout_inline_multipass.go` line ~4470)
2. Replace `reverseRunes()` with `reverseAndMirrorRunes()` that applies mirroring during reversal
3. Or better: integrate with `golang.org/x/text/unicode/bidi` package for comprehensive mirroring

**Verification**: `go build ./...` (fix compile), then `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 300s`

### 1.4 Table Cell Size Lookup Physical-to-Logical (~15-25 WM tests)

**Files**: `louis13/pkg/layout/layout_table.go` (~line 318)
**Blink reference**: `NGTableCellNode::ComputeMinMaxSizes()` uses `ResolveInlineSize()` which automatically selects the correct property based on writing mode.

**Bug**: `cell.style.GetLength("width")` always reads CSS `width` for column sizing. In vertical modes, the inline-axis property (column sizing) should come from CSS `height`.

**Fix**: Map all physical property lookups to logical based on WDM:
```go
prop := "width"
if wdm.IsVertical() { prop = "height" }
```
Apply to ALL physical property lookups in table_layout.go: width, min-width, max-width, height, min-height, max-height.

**Verification**: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/row-progression' ./pkg/visualtest/ -timeout 120s`

---

## Phase 2 — Writing Mode Correctness ✅ (3/4 completed, +11 WM tests)

**Results**: WM 413/790 (52.3%), Flexbox 380/630 (60.3%) — no regressions

### 2.1 Relative Positioning Percentage Resolution ✅ (+1 WM)

**Files**: `pkg/layout/block_layout.go` (~line 357-362)
**Blink reference**: `top`/`bottom` resolve against physical CB height; `left`/`right` against physical CB width. These are physical properties regardless of writing mode.

**Bug**: `cbWidth` uses `AvailableSize.InlineSize` (logical). In vertical modes, InlineSize is the physical height, so `left: 50%` resolves against the wrong dimension.

**Fix**: Convert logical available size to physical before resolving percentage offsets:
```go
physCB := ToPhysicalSize(LogicalSize{InlineSize: ..., BlockSize: ...}, wdm.WM)
offset := style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
```

### 2.2 CSS clip: rect() Implementation ✅ (+8 WM)

**Files**: `pkg/css/style.go` (GetClipRect exists), `pkg/render/paint_layer.go` or `pkg/render/render.go`
**Blink reference**: `clip: rect()` values are physical offsets from the element's border-box. Writing modes do NOT affect clip values (CSS Writing Modes §7.6).

**Bug**: `GetClipRect()` exists but is never consumed by paint code.

**Fix**: In the paint/render path, when an element has `position: absolute` and `clip: rect()`:
1. Read `style.GetClipRect()` → (top, right, bottom, left) physical offsets
2. Apply as a clipping rectangle relative to the element's border-box origin
3. Use `gg.DrawRectangle()` + `gg.Clip()` before painting children

### 2.3 Percentage Padding Resolution ✅ (+2 WM)

**Files**: `pkg/css/style.go`, `louis13/pkg/layout/fragment_geometry.go`
**Blink reference**: `ComputedStyle::PaddingTop()` returns a `Length` that may be a percentage, resolved by `ResolveInlineLength()` against the containing block's inline-size. All four padding percentages resolve against the same dimension.

**Bug**: `GetPadding()` uses `getLengthOrZero()` which ignores percentage values.

**Fix**: Add `GetPaddingForWidth(containingBlockWidth float64)` method, similar to `GetAllMarginsForWidth`. All four padding values resolve against the same width (the containing block's inline-size). Update `ComputeFragmentGeometry()` to pass the percentage resolution base.

### 2.4 Orthogonal Min/Max Sizing via Layout ⏳ (deferred — causes -2 regression)

**Files**: `pkg/layout/min_max_sizing.go` (~line 230)
**Blink reference**: `NGBlockNode::ComputeMinMaxSizes()` handles orthogonal children via `NGOrthogonalWritingModeRootInlineSize()` which performs an actual layout to get the child's block-size as the parent's inline-size contribution.

**Bug**: `measureBlockMinMax` computes the child's intrinsic inline-size, but for orthogonal children the parent needs the child's block-size. This requires laying out the child.

**Fix**: When child is orthogonal, build a constraint space with the orthogonal fallback, lay out the child, and use the resulting block-size as both min and max contribution.

---

## Phase 3 — Structural WM Investment (New Engine)

Phases 1–2 fixed individual bugs but exposed that the remaining ~377 WM failures
aren't amenable to point fixes. They stem from three structural gaps in the new
engine that affect whole test families. This phase invests in making the new
engine's block layout algorithm genuinely writing-mode-aware, so future work
(flex, table, inline) inherits correct coordinate handling automatically.

**Blink reference**: Study `NGBlockLayoutAlgorithm::Layout()`,
`NGOutOfFlowLayoutPart`, and `NGBoxFragmentBuilder::ToPhysicalOffset()`.

### 3.1 Whole-Document Vertical Mode — Abspos & Static Positioning

**Symptom**: All 8 remaining clip-rect failures (`clip-rect-vlr-011` through
`clip-rect-vrl-016`) render the clipped content correctly but position surrounding
text wrong. The root `<html>` has `writing-mode: vertical-lr`, so the *entire*
document is in a vertical formatting context. Text, static-position abspos anchors,
and block flow all need to follow the vertical block direction.

**Files**: `pkg/layout/block_layout.go` (child positioning loop),
`pkg/layout/out_of_flow_layout.go` (static-position resolution)

**Investigation steps**:
1. Study Blink's `NGBlockLayoutAlgorithm::HandleNewFormattingContext()` and
   `NGOutOfFlowLayoutPart::ComputeStaticPosition()` for how the static position
   is computed when the containing block's writing-mode is vertical.
2. Compare our `BlockLayoutAlgorithm.Layout()` child-positioning cursor
   (`blockCursor`) — does it advance in the correct physical direction for
   vertical modes? For VRL the block direction is physical right-to-left; for
   VLR it's physical left-to-right.
3. Check that `out_of_flow_layout.go` converts the static position from logical
   to physical correctly when the containing block is vertical.

**Expected scope**: Touches the child loop in `block_layout.go` and the
static-position helpers in `out_of_flow_layout.go`. Should not change the
constraint-space or fragment-builder APIs.

**Verification**: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/clip-rect-vlr-01[1-7]' ./pkg/visualtest/`
plus `abs-pos-non-replaced-v*` test families.

### 3.2 Orthogonal Min/Max Sizing with Layout Cache

**Symptom**: Phase 2.4 attempted to lay out orthogonal children during min/max
computation but caused a -2 regression, likely from unbounded recursion or
missing cycle-breaking. 71/74 `sizing-orthog-*` tests still fail.

**Files**: `pkg/layout/min_max_sizing.go`, new file `pkg/layout/layout_cache.go`

**Blink reference**: `NGBlockNode::ComputeMinMaxSizes()` calls
`NGOrthogonalWritingModeRootInlineSize()` which:
- Performs a layout of the orthogonal child in a separate constraint space.
- Caches the result so repeat queries don't re-layout.
- Uses `NGLayoutCacheStatus` to detect cycles and fall back to the
  orthogonal fallback size (ICB cross-size) when recursion is detected.

**Investigation steps**:
1. Add a per-layout-pass cache keyed on `(LayoutInputNode, ConstraintSpace)` →
   `LayoutResult`. A simple `map` on `LayoutContext` suffices.
2. In `measureBlockMinMax`, when a child is orthogonal:
   a. Check the cache for an existing result.
   b. If absent, set a "computing" sentinel to detect cycles.
   c. Lay out the child; store the result.
   d. Use the child's physical block-size as the parent's inline contribution.
   e. If a cycle is detected, fall back to the orthogonal fallback size.
3. Verify no infinite loops on the `sizing-orthog-*` tests before measuring
   pass/fail counts.

**Expected scope**: New ~50-line cache in `LayoutContext`, modification to
`measureBlockMinMax` and potentially `measureFlexMinMax`.

**Verification**: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s`

### 3.3 Writing-Mode-Aware Child Positioning in Block Layout

**Symptom**: Many `abs-pos-non-replaced-v*`, `block-flow-direction-*`, and
`vertical-*` tests fail because children are positioned using physical
X/Y offsets that assume horizontal-tb. In vertical modes the block cursor
should advance along the physical X axis (right-to-left for VRL,
left-to-right for VLR), not the Y axis.

**Files**: `pkg/layout/block_layout.go` (main child loop),
`pkg/layout/fragment_builder.go` (`Build()` → `ToPhysicalOffset`)

**Blink reference**: `NGBlockLayoutAlgorithm` positions every child at a
*logical* `(InlineOffset, BlockOffset)`. `NGBoxFragmentBuilder::Build()`
converts these to physical `(x, y)` via `ToPhysicalOffset()` using the
writing-mode converter. The converter accounts for both the writing-mode
and direction of the containing block.

**Investigation steps**:
1. Audit every place `BlockLayoutAlgorithm` produces an offset. Are all
   offsets expressed in logical coordinates? Or do some assume physical?
2. Check `BoxFragmentBuilder.Build()` → does `ToPhysicalOffset` receive
   the correct outer-size and inner-size for the conversion?
3. Look at the `abs-pos-non-replaced-vlr-*` and `block-flow-direction-vlr-*`
   test families. Categorize failures: wrong X? wrong Y? wrong size?
   This narrows which coordinate conversion is broken.

**Expected scope**: Likely a small number of places in `block_layout.go`
where offsets are built in the wrong coordinate frame, plus possible fixes
in `fragment_builder.go` for the physical conversion step.

**Verification**: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/block-flow-direction' ./pkg/visualtest/ -timeout 120s`
and `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-v' ./pkg/visualtest/ -timeout 120s`

---

## Phase 4 — Flex Spec Compliance

### 4.1 Justify-Content Overflow Fallback

**Files**: `louis13/pkg/layout/layout_flex.go` (~line 1439-1441)
**Blink reference**: CSS Flexbox §8.2 — overflow alignment fallbacks.

**Bug**: freeSpace clamped to 0 on overflow. Should preserve negative freeSpace for `flex-end` and `center`, and add per-keyword fallbacks:
- `space-between` → `flex-start`
- `space-around`, `space-evenly` → `center`

### 4.2 Flex §4.5 Automatic Minimum Sizing (3 remaining bugs)

**Files**: `louis13/pkg/layout/layout_flex.go`, function `flexItemMinMain`

a) **Specified size suggestion**: Cap `autoMin` by explicit CSS width (row) or height (column), NOT by flex-basis.
b) **Transferred size suggestion**: For items with intrinsic aspect ratio, compute main-size from cross-size via ratio.
c) **Per-axis overflow check**: Check `overflow-x` for row flex, `overflow-y` for column (not just shorthand `overflow`).

### 4.3 Flex Aspect-Ratio (2 remaining bugs)

**Files**: `louis13/pkg/layout/layout_flex.go`

a) **Intrinsic aspect ratio in resolveFlexBasis**: Check `GetIntrinsicSizingInfo()` for replaced elements, not just CSS `aspect-ratio` property.
b) **Border-box aspect-ratio**: When `box-sizing: border-box`, the ratio applies to the border-box. Account for border+padding in aspect-ratio transfers.

### 4.4 Full Baseline Alignment (4 bugs — most complex flex task)

**Files**: `louis13/pkg/layout/layout_flex.go`, `pkg/layout/layout_result.go`, `pkg/layout/fragment_builder.go`, `louis13/pkg/layout/layout_block.go`
**Blink reference**: `NGLayoutResult` has `FirstBaseline()` and `LastBaseline()`.

a) Add `LastBaseline float64` field to `LayoutResult` and `BoxFragmentBuilder`
b) Synthesize baseline from border-box bottom when `result.Baseline == 0`
c) Track `baselineMaxAbove` and `baselineMaxBelow` separately for baseline-aligned items in §9.4 loop
d) Set `lineCrossMax = max(lineCrossMax, baselineMaxAbove + baselineMaxBelow)`

---

## Phase 5 — Table Layout (Complex, Incremental)

### 5.1 Border-Spacing in Vertical Modes

Map physical `border-spacing` (horizontal, vertical) to logical (inline, block) based on WDM. Add spacing in cell/row positioning loops.

### 5.2 Caption Support

Detect `DisplayTableCaption` children in `collectRows()`. Lay out as block-level boxes. Position based on `caption-side` (logical property: block-start/block-end).

### 5.3 Border-Collapse

Implement CSS 2.1 §17.6.2.1 conflict resolution:
- `hidden` wins over everything
- Wider border wins
- Style priority: double > solid > dashed > dotted > ridge > outset > groove > inset > none
- Element priority: cell > row > row-group > column > column-group > table

### 5.4 Rowspan Support

Read `rowspan` attribute. Track spanning cells during layout. Position at starting row's block offset, extending through the span.

---

## Estimated Impact

| Phase | Items | Est. Test Gain |
|-------|-------|---------------|
| Phase 1 (4 items) | 4 fixes | ✅ +72 WM |
| Phase 2 (3 items + 1 deferred) | 3 fixes | ✅ +11 WM |
| Phase 3 (3 structural items) | 3 fixes | +40-80 WM (high leverage) |
| Phase 4 (4 flex items) | 4 fixes | +20-40 flex |
| Phase 5 (4 table items) | 4 fixes | +15-25 WM |
| **Total remaining** | **11 items** | **+75-145 WM, +20-40 flex** |
