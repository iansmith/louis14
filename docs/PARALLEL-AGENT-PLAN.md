# Parallel Agent Work Plan: WM + Flexbox Test Improvements

**Date**: 2026-04-02
**Branch**: `rewrite/louis13-louis14`
**Baseline**: WM 327/790 (41.4%), Flexbox 380/630 (60.3%)

---

## Architecture Quick Reference

All layout follows Blink/LayoutNG patterns:
- `ConstraintSpace` (immutable input from parent) → `LayoutAlgorithm.Layout()` → `LayoutResult` (immutable output)
- Coordinates are logical (InlineSize/BlockSize) during layout, physical during paint
- `WritingDirectionMode` = WritingMode + Direction; conversions via `WritingModeConverter`
- `ExclusionSpace` tracks floats (immutable, returns new copy on Add)
- `BoxFragmentBuilder` accumulates children, `Build()` converts logical→physical

**Key files**: `block_layout.go`, `flex_layout.go`, `table_layout.go`, `out_of_flow_layout.go`, `constraint_space.go`, `fragment_geometry.go`, `exclusion_space.go`, `min_max_sizing.go`, `writing_mode_converter.go`, `layout_result.go`

**Test commands**:
- WM only: `go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5`
- Flexbox only: `go test -v -run TestWPTCSS3Reftests/css-flexbox ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5`
- Specific test: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-justify-content' ./pkg/visualtest/ -timeout 120s`
- Build check: `go build ./...`

---

## Agent 1: Flex Justify-Content + Alignment Keywords

**Files**: `pkg/layout/flex_layout.go`
**Functions**: `computeItemMainOffsets` (lines 1439-1476), `getAlignSelf` (lines 1683-1698), cross-axis alignment (lines 567-582)
**Expected improvement**: +20-25 flexbox tests

### Bug 1A: Overflow fallback (lines 1439-1441)
Current code clamps `freeSpace` to 0 when items overflow. This makes `flex-end` and `center` behave like `flex-start` on overflow, and `space-around`/`space-evenly` don't fall back to `center`.

**Fix**: Remove the `freeSpace < 0 → 0` clamp. Add per-keyword fallback:
```go
if freeSpace < 0 {
    switch justifyContent {
    case "space-between":
        justifyContent = "flex-start"
    case "space-around", "space-evenly":
        justifyContent = "center"
    }
    // flex-end and center keep negative freeSpace
}
```

### Bug 1B: `self-start`/`self-end` resolution (lines 567-582)
Currently treated as synonyms for `flex-start`/`flex-end`. Per spec, they're relative to the **item's own writing mode**. For an item with `writing-mode: vertical-lr` in an HTB container, `self-end` = item's block-end = physical left = cross-start.

**Fix**: When align-self is `self-start`/`self-end`, check if the item's block direction aligns with or opposes the container's cross direction. If they oppose, swap the meaning.

### Bug 1C: `normal`/`initial` align-self (lines 1688-1696)
Neither `"normal"` nor `"initial"` appears in the switch. `normal` in flex = `stretch`. `initial` = `auto` (falls through to align-items).

**Fix**: Add cases for `"normal"` (→ `"stretch"`) and `"initial"` (→ fall through to align-items).

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-justify-content' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-align-self' ./pkg/visualtest/ -timeout 120s`

---

## Agent 2: Flex Baseline Alignment

**Files**: `pkg/layout/flex_layout.go`, `pkg/layout/layout_result.go`, `pkg/layout/fragment_builder.go`, `pkg/layout/block_layout.go`
**Expected improvement**: +15-20 flexbox tests

### Bug 2A: `last baseline` unrecognized (flex_layout.go lines 1655-1661, 1684-1698)
`getAlignItems` and `getAlignSelf` don't recognize `"last baseline"` or `"first baseline"`. They fall through to `"stretch"`.

**Fix**: Add `"first baseline"` → return `"baseline"`, `"last baseline"` → return `"last-baseline"`.

### Bug 2B: No baseline synthesis (flex_layout.go lines 572-578)
When `item.baseline == 0`, item falls back to flex-start. Per CSS Flexbox §9.9 + CSS Writing Modes §4.3.2, items without a natural baseline should have a **synthesized** baseline at margin-box bottom edge.

**Fix**: After layout (around line 275), if `result.Baseline == 0`, set `item.baseline = item.crossSize` (border-box bottom). For items with `overflow != visible`, also synthesize from border-box bottom.

### Bug 2C: Baseline groups don't affect line cross-size (flex_layout.go lines 313-319)
Per CSS Flexbox §9.4 step 8, the line cross-size for baseline-aligned items = `max_above_baseline + max_below_baseline`.

**Fix**: In the §9.4 loop (lines 261-319), compute separate `baselineMaxAbove` and `baselineMaxBelow` for baseline items:
```go
above := item.crossMarginStart() + item.baseline
below := item.crossSize + item.crossMarginEnd() - item.baseline
baselineMaxAbove = max(baselineMaxAbove, above)
baselineMaxBelow = max(baselineMaxBelow, below)
```
Set `lineCrossMax = max(lineCrossMax, baselineMaxAbove + baselineMaxBelow)`.

### Bug 2D: No `LastBaseline` field (layout_result.go line 26)
Add `LastBaseline float64` to `LayoutResult` and `BoxFragmentBuilder`. Block layout sets it to the last line box's baseline. Flex layout uses it for `last-baseline` alignment.

**Blink reference**: In Blink, `NGLayoutResult` has both `FirstBaseline()` and `LastBaseline()`. The last baseline is set by `NGBlockLayoutAlgorithm::Layout()` to the last line box's baseline position. This is propagated up through nested layouts.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline' ./pkg/visualtest/ -timeout 120s`

---

## Agent 3: Flex Automatic Minimum Sizing (§4.5)

**Files**: `pkg/layout/flex_layout.go`
**Functions**: `flexItemMinMain` (lines 1854-1931)
**Expected improvement**: +15-20 flexbox tests

### Bug 3A: Missing `specified size suggestion` (lines 1890-1903)
Row flex returns just `mm.MinContent`. Should cap by the item's explicit `width` property.

**Fix** (row flex, around line 1899):
```go
autoMin := mm.MinContent
// Specified size suggestion: cap by explicit width
if specWidth := resolveExplicitMainSize(style, childWDM, space); specWidth >= 0 {
    autoMin = min(autoMin, specWidth)
}
```

### Bug 3B: Missing `transferred size suggestion` (lines 1890-1903)
For items with intrinsic aspect ratios, should compute main-size from cross-size via ratio, clamped by min/max-cross.

**Fix**: Check `GetIntrinsicSizingInfo()` for replaced elements. If `HasAspectRatio`, compute `transferred = crossSize * aspectRatio`, clamped by min/max-cross. Then `autoMin = min(autoMin, transferred)`.

### Bug 3C: Column flex caps by `flexBasis` instead of specified height (lines 1919-1926)
Per §4.5, the specified size suggestion is the CSS `height` property, NOT `flex-basis`. An item with `flex-basis: 0; height: 100px` should have specified suggestion = 100px.

**Fix**: Replace `flexBasis` cap with explicit CSS `height` check. Remove the flex-basis cap entirely.

### Bug 3D: Overflow check only examines shorthand (lines 1882-1888)
Only checks `style.Get("overflow")`. Should check per-axis: `overflow-x` for row flex, `overflow-y` for column.

**Fix**: Check the main-axis overflow: `overflow-x` for row flex (or `overflow` fallback), `overflow-y` for column.

### Blink reference
CSS Flexbox §4.5 algorithm:
1. automatic minimum size = min(content-based minimum, specified size suggestion, transferred size suggestion)
2. content-based minimum = min-content size in main axis
3. specified size suggestion = definite preferred main size (CSS width/height, NOT flex-basis)
4. transferred size suggestion = main size from definite cross-size via aspect-ratio
5. if `overflow` in main axis is not `visible` → clamp to 0

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flex-minimum' ./pkg/visualtest/ -timeout 120s`

---

## Agent 4: Flex Aspect Ratio + Replaced Elements

**Files**: `pkg/layout/flex_layout.go`
**Functions**: `resolveFlexBasis` (lines 935-1041), `stretchFlexItems` (lines 1976-2044), cross-size derivation (lines 289-311, 314)
**Expected improvement**: +20-30 flexbox tests

### Bug 4A: Stretch doesn't recompute main-size via aspect-ratio
After stretching cross-size of an item with `aspect-ratio`, the main-size stays at whatever was resolved in flex distribution (often 0 for empty elements).

**Fix**: In `stretchFlexItems` (around line 2032), after stretching cross-size:
```go
if ar := getAspectRatio(item); ar > 0 {
    item.resolvedMain = stretchedCross * ar // (or / ar, depending on orientation)
}
```

### Bug 4B: Line cross-size uses pre-correction variable (line 314)
After aspect-ratio corrects `item.crossSize` (lines 289-311), line 314 still uses the old `itemCross` variable.

**Fix**: Change line 314 from `outerCross := itemCross + item.crossMarginSum()` to `outerCross := item.crossSize + item.crossMarginSum()`.

### Bug 4C: `resolveFlexBasis` doesn't check intrinsic aspect ratio (line 965)
Only checks `style.GetAspectRatio()` (CSS property). For `<img>` with intrinsic ratio but no CSS `aspect-ratio`, the transferred size isn't computed.

**Fix**: Also check `GetIntrinsicSizingInfo()` for the aspect ratio when the item is a replaced element.

### Bug 4D: Border-box aspect-ratio
When `box-sizing: border-box`, the aspect ratio applies to the border-box, not content-box. Stretch computation and aspect-ratio transfer must account for this.

**Blink reference**: Blink's `NGFlexLayoutAlgorithm::StretchAlgorithm()` re-layouts items after stretching and recomputes main-size for aspect-ratio items using `ComputeReplacedSize()` which handles box-sizing correctly.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/aspect-ratio' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-replaced' ./pkg/visualtest/ -timeout 120s`

---

## Agent 5: WM Relative Positioning + CSS Clip Rect

**Files**: `pkg/layout/block_layout.go` (lines 356-377), `pkg/render/paint_layer.go`, `pkg/css/style.go`
**Expected improvement**: +25-30 WM tests

### Bug 5A: Relative positioning percentage resolution (block_layout.go lines 357-360)
`cbWidth` = `AvailableSize.InlineSize`, `cbHeight` = `AvailableSize.BlockSize`. These are logical. But `top`/`right`/`bottom`/`left` are physical properties that resolve percentages against physical dimensions.

**Fix**: Convert to physical before resolving:
```go
physCB := ToPhysicalSize(LogicalSize{
    InlineSize: bla.space.AvailableSize.InlineSize,
    BlockSize:  bla.space.AvailableSize.BlockSize,
}, wdm.WM)
offset := bla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
```
The dx/dy application is already correct (physical offsets → physical RelativeOffset).

### Bug 5B: CSS `clip: rect()` not implemented
`GetClipRect()` exists in css/style.go but is never consumed by paint code. The clip tests assume rect() clipping works.

**Fix**: In the paint layer or renderer, when an element has `position: absolute` and `clip: rect()`:
1. Check `style.GetClipRect()` — it returns (top, right, bottom, left) in physical pixels
2. Apply as a clipping rectangle relative to the element's border-box origin
3. Values remain physical regardless of writing mode (CSS Writing Modes §7.6)

**Blink reference**: In Blink, `clip: rect()` is resolved as physical offsets from the element's border-box. `LayoutObject::ClipRect()` returns a `PhysicalRect`. Writing modes do NOT affect clip values — they are "purely physical" per the spec.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/box-offsets-rel-pos' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/clip-' ./pkg/visualtest/ -timeout 120s`

---

## Agent 6: WM Percentage Margins + Padding

**Files**: `pkg/layout/block_layout.go` (line 153), `pkg/layout/fragment_geometry.go`, `pkg/css/style.go`
**Expected improvement**: +16 WM tests

### Bug 6A: Margins resolved in child's WDM instead of parent's (block_layout.go line 153)
```go
childMargins := ResolveMargins(childStyle, childWDM, childAvailableInline)
```
The margins position the child within the parent's coordinate system. They should be in the **parent's** logical space.

**Fix**: Use the parent's WDM:
```go
childMargins := ResolveMargins(childStyle, wdm, childAvailableInline)
```
Note: `wdm` is the parent's WritingDirectionMode, already available in scope.

**Important caveat**: Verify this doesn't break HTB tests. In HTB-only scenarios, parent WDM = child WDM, so the change is a no-op. But for orthogonal children, this correctly maps the child's physical margins to the parent's logical system.

### Bug 6B: Percentage padding not resolved (css/style.go)
`GetPadding()` uses `getLengthOrZero()` which ignores percentage values. Per CSS 2.1 §8.4, padding percentages resolve against the containing block's inline-size.

**Fix**: Add `GetPaddingForWidth(containingBlockWidth float64)` method, similar to `GetAllMarginsForWidth`. Update `ComputeFragmentGeometry()` to pass the percentage resolution base to the padding resolver.

**Blink reference**: In Blink, `ComputedStyle::PaddingTop()` returns a `Length` that may be a percentage. The percentage is resolved during layout by `ResolveInlineLength()` against the containing block's inline-size. All four padding percentages resolve against the same dimension (the containing block's inline-size), even for top/bottom padding. This is the same behavior as margins.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/percent' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/margin' ./pkg/visualtest/ -timeout 120s`

---

## Agent 7: WM Float/Clear Mapping

**Files**: `pkg/layout/block_layout.go` (lines 427-454), `pkg/layout/exclusion_space.go`
**Expected improvement**: +20-34 WM tests

### Current behavior analysis
The CSS `float: left`/`right` values are physical. In vertical writing modes, they need to be mapped to the logical float side before being stored in the ExclusionSpace.

Per CSS Writing Modes Level 3: `float: left` maps to **line-left**, and `float: right` maps to **line-right**. In vertical modes, line-left = top = inline-start (for LTR). So:
- HTB-LTR: `float:left` → inline-start, `float:right` → inline-end ✓ (current works)
- VRL/VLR-LTR: `float:left` → inline-start (top), `float:right` → inline-end (bottom) ✓ (current works accidentally)
- VRL/VLR-RTL: `float:left` → inline-end, `float:right` → inline-start ✗ (current broken)

The same mapping applies to `clear: left`/`clear: right`.

### Fix
Create a mapping function:
```go
func MapPhysicalFloatToLogical(side css.FloatType, wdm WritingDirectionMode) css.FloatType {
    if wdm.IsHorizontal() {
        if wdm.IsLTR() {
            return side // left=inline-start, right=inline-end
        }
        // HTB-RTL: left=inline-end, right=inline-start
        if side == css.FloatLeft { return css.FloatRight }
        return css.FloatLeft
    }
    // Vertical modes: line-left=top=inline-start(LTR)/inline-end(RTL)
    if wdm.IsLTR() {
        return side // left→top→inline-start
    }
    if side == css.FloatLeft { return css.FloatRight }
    return css.FloatLeft
}
```
Apply this in `layoutFloat()` before passing to `ExclusionSpace`. Same for `clear` in `ClearanceOffset()`.

**Also investigate**: Whether float *positioning* is correct in vertical modes. The float gets a logical offset from `FindFloatPosition()`. In vertical modes, the float's block-offset = physical X, inline-offset = physical Y. Verify `Build()` converts these correctly via `ToPhysicalOffset`.

**Blink reference**: Blink's `ResolveFloating()` in `style_utils.cc` maps physical float values to logical `EFloat` before use. The `NGExclusionSpace` works entirely in logical coordinates.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/float' ./pkg/visualtest/ -timeout 120s`

---

## Agent 8: WM Orthogonal Flow Sizing

**Files**: `pkg/layout/block_layout.go` (lines 69-77), `pkg/layout/min_max_sizing.go` (lines 230+), `pkg/layout/constraint_space.go`
**Expected improvement**: +20-40 WM tests

### Bug 8A: Missing min-block-size in orthogonal available size (block_layout.go lines 69-77)
Only checks `ResolveMaxBlockSize` when parent's block-size is indefinite. Should also check min-block-size.

**Fix**:
```go
orthogonalAvailableBlock := childAvailableBlock
if childAvailableBlock == Indefinite {
    if maxBlock, hasMax := ResolveMaxBlockSize(...); hasMax {
        orthogonalAvailableBlock = maxBlock
    }
}
// Also apply min-block-size
minBlock := ResolveMinBlockSize(...)
if minBlock > 0 && (orthogonalAvailableBlock == Indefinite || minBlock > orthogonalAvailableBlock) {
    orthogonalAvailableBlock = minBlock
}
```

### Bug 8B: No ICB clamp on large parent block-size
When the parent's resolved block-size exceeds the ICB, the orthogonal child's available inline-size should be clamped. Tests verify this with scrollers.

**Fix**: After computing `orthogonalAvailableBlock`, clamp to ICB:
```go
if orthogonalAvailableBlock != Indefinite && orthogonalAvailableBlock > icbFallback {
    orthogonalAvailableBlock = icbFallback
}
```

### Bug 8C: Orthogonal min/max sizing wrong (min_max_sizing.go lines 230-267)
`measureBlockMinMax` computes the child's intrinsic **inline-size**, but for orthogonal children the parent's inline-size contribution is the child's **block-size**. This requires actually laying out the orthogonal child to get its block-size.

**Fix**: In `measureBlockMinMax`, when the child is orthogonal:
```go
if space.WritingDirection.IsOrthogonalTo(childWDM) {
    childSpace := NewConstraintSpaceBuilder(space.WritingDirection, childWDM, true).
        SetOrthogonalFallbackInlineSize(fallback).
        SetAvailableSize(space.AvailableSize).
        Build()
    result := layoutElement(ctx, child, childSpace)
    childLogical := NewLogicalFragment(space.WritingDirection, result.Fragment)
    contribution := childLogical.InlineSize() // child's block-size in parent's frame
    childMin = contribution
    childMax = contribution
}
```

**Blink reference**: In Blink's `NGBlockNode::ComputeMinMaxSizes()`, orthogonal children are handled by `NGOrthogonalWritingModeRootInlineSize()` which performs an actual layout of the child to determine its block-size, then uses that as the inline-size contribution to the parent. This is the correct approach per CSS Writing Modes §7.3.1.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/available-size' ./pkg/visualtest/ -timeout 120s`

---

## Agent 9: Abs-Pos Paint Order + Vertical Page Layout

**Files**: `pkg/layout/engine.go` (lines 231-233, 200-213), `pkg/render/paint_layer.go`
**Expected improvement**: +100-120 WM tests (abs-pos-non-replaced-v* suite)

### CRITICAL CONSTRAINT — Follow Blink's Paint Architecture

In Blink/LayoutNG, paint ordering is governed by `PaintLayerPainter` and `NGBoxFragmentPainter`.
The key invariant is: **text fragments are NEVER positioned elements.** CSS `position` is a
non-inherited property that applies to boxes, not text runs. Blink's `NGPhysicalTextFragment`
does not carry a `position` property at all — only `NGPhysicalBoxFragment` does. Our engine
violates this invariant.

### Root Cause 1 (~120 tests): Text fragments corrupt paint order

**The bug**: In `engine.go:232`, `fragmentToBox()` sets `box.Position = box.Style.GetPosition()`
for ALL fragments, including `FragmentText` text runs. Text fragments get their parent element's
complete style (set at `inline_layout.go:446`: `Style: r.Item.Style`). When the parent div has
`position: relative`, the text run inherits `position: relative`.

**The consequence**: In `paint_layer.go:281`, text boxes with `position: relative` are classified
as positioned elements (`isPositioned = true`). They enter `AutoZero` of the root stacking context
instead of `FlowChildren`. This causes text to paint AFTER abs-pos elements, overwriting the
abs-pos element's green content with the containing block's red background.

**Evidence from rendered output**: vlr-003 passes (text and abs-pos don't overlap). vlr-015 fails
(text fragment overlaps abs-pos span; text paints over it due to incorrect paint ordering).

**The fix** — In `engine.go`, around line 231-233:
```go
// Current (buggy):
if box.Style != nil {
    box.Position = box.Style.GetPosition()

// Fixed — text fragments should never be positioned:
if box.Style != nil && frag.Type != FragmentText {
    box.Position = box.Style.GetPosition()
```

**Blink justification**: `NGPhysicalTextFragment` in Blink does not inherit layout-affecting properties
like `position` from its parent inline node. Only `NGPhysicalBoxFragment` can be a positioned element.
This is because CSS `position` is explicitly "Applies to: all elements" (boxes), not text runs.

### Root Cause 2 (~32 tests): Vertical root mode page layout differences

These tests set `writing-mode: vertical-lr` on the `<html>` element. The entire page uses vertical
block flow: `<p>`, `<div>` etc. stack left-to-right. The reference files expect a specific visual
output based on how body margins, `<p>` sizing, and element stacking work in vertical mode.

The issue is likely in how body/p element sizing works when the root is vertical. Specifically:
- The `<p>` containing the PASS image may size differently in vertical mode
- Body margins in vertical mode may not be applied correctly
- The containing block div's position shifts as a consequence

**Investigation approach**: After fixing Root Cause 1, re-run all abs-pos tests. Many of the 32
"html writing-mode" tests may now pass since the paint order was the dominant issue. For any
remaining failures, compare the test's rendered output to the reference pixel-by-pixel and trace
the position differences through the layout tree.

### Root Cause 3 (~28 tests): ICB abs-pos RTL static position

For tests with `direction: rtl` and the containing block being the ICB, the abs-pos element's
static position is miscalculated. The static position in RTL should be computed from the
inline-end edge, not inline-start.

**Blink reference**: In Blink's `NGOutOfFlowLayoutPart`, when computing the static position for
RTL, the inline offset is measured from the inline-end of the containing block. The static
position represents where the element would have been in normal flow. In RTL, normal flow
starts from the right edge.

**Fix approach**: In `out_of_flow_layout.go`, when the containing block has `direction: rtl`,
the static inline offset should be flipped: `staticInlineOffset = cbInlineSize - staticInlineOffset - childInlineSize`.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-vlr' ./pkg/visualtest/ -timeout 300s 2>&1 | tail -10`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-vrl' ./pkg/visualtest/ -timeout 300s 2>&1 | tail -10`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-icb' ./pkg/visualtest/ -timeout 300s 2>&1 | tail -10`

### Checkpoint structure
1. **Checkpoint 1**: Fix Root Cause 1 (text fragment position), compile, run abs-pos tests. Report pass counts.
2. **Checkpoint 2**: Investigate remaining failures from Root Cause 2. Fix if tractable. Report.
3. **Checkpoint 3**: Fix Root Cause 3 (ICB RTL static position). Report final counts.

---

## Agent 10: Table Layout in Vertical Writing Modes

**Files**: `pkg/layout/table_layout.go` (all), `pkg/css/style.go`
**Expected improvement**: +15-25 WM tests

### CRITICAL CONSTRAINT — Follow Blink's Table Layout Architecture

In Blink's `NGTableLayoutAlgorithm`, ALL table layout uses logical coordinates. Physical CSS
properties (border-left, width, etc.) are mapped to logical equivalents (inline-start border,
inline-size, etc.) BEFORE any layout computation. This is the fundamental principle. Our table
layout already uses logical coordinates for row/cell placement, but fails to map physical CSS
property lookups to logical equivalents.

### Issue 10A (impacts many tests): Cell explicit-size uses physical "width"

**The bug**: `table_layout.go:318` — `cell.style.GetLength("width")` always reads CSS `width`
for column sizing. In vertical modes, CSS `width` is the block-axis property. The inline-axis
property (column sizing) should come from CSS `height`.

**Blink approach**: Blink's `NGTableCellNode::ComputeMinMaxSizes()` uses `ResolveInlineSize()`
which automatically checks the correct property based on writing mode.

**The fix**:
```go
// Current:
if w, ok := cell.style.GetLength("width"); ok && w > 0 {

// Fixed:
prop := "width"
if wdm.IsVertical() {
    prop = "height"
}
if w, ok := cell.style.GetLength(prop); ok && w > 0 {
```

Also check for ALL other physical property lookups in `table_layout.go` and fix them similarly:
- `min-width` → inline-size min
- `max-width` → inline-size max
- Any `height` used for block sizing → check if it should be `width` in vertical modes

### Issue 10B (~16 tests): No border-collapse implementation

**What Blink does** — `NGTableBorders::ComputeTableBorders()`:
1. Iterate all cells, rows, row-groups, columns, column-groups, and the table itself
2. For each element, map its physical borders (border-left, border-right, etc.) to logical
   borders (block-start, block-end, inline-start, inline-end) using the table's `WritingDirectionMode`
3. For each cell edge, resolve conflicts using CSS 2.1 §17.6.2.1 priority:
   - `hidden` wins over everything
   - If neither is `hidden`, the wider border wins
   - If same width, style priority: double > solid > dashed > dotted > ridge > outset > groove > inset > none
   - If same width and style, element priority: cell > row > row-group > column > column-group > table
4. Store the winning border for each cell edge
5. Adjust the cell's content area: half the winning border is inside the cell, half outside
6. The table's border-box = grid area + half the outer borders

**Implementation approach for our engine**:
1. Add a `resolveCollapsedBorders()` function that implements the conflict resolution
2. The function should take all cells and the table's WDM
3. Map physical borders to logical FIRST using `ToLogicalEdges(physEdges, wdm)` from
   `writing_mode_converter.go:192`
4. Compare adjacent cell borders logically
5. Store winning borders on cell fragments for painting
6. Adjust cell content areas by the collapsed border widths

### Issue 10C (~4 tests): No caption support

**What Blink does**: Captions are collected separately from rows during table child traversal.
They are laid out as block-level boxes. `caption-side: top` places the caption at block-start
(above the grid in HTB, to the left in VLR, to the right in VRL). `caption-side: bottom` places
at block-end. The table's total block-size = caption block-size + grid block-size.

**Implementation approach**:
1. In `collectRows()`, detect `DisplayTableCaption` children and collect into a separate list
2. In `Layout()`, lay out captions as block-level boxes using `BlockLayoutAlgorithm`
3. Position based on `caption-side` — this is already a logical property (block-start/block-end)
4. Include caption dimensions in total table block-size

### Issue 10D: No border-spacing

**What Blink does**: CSS `border-spacing` values are physical (horizontal, vertical). They are
mapped to logical: inline-spacing (between cells in a row) and block-spacing (between rows).
In HTB: inline=horizontal, block=vertical. In vertical modes: inline=vertical, block=horizontal.
Spacing is added between adjacent cells/rows AND at the table edges.

**Implementation**: Read `GetBorderSpacing()`/`GetBorderSpacingV()` from style. Map physical to
logical based on WDM. Add spacing in cell positioning loop and row positioning loop.

### Issue 10E: No rowspan support

**Current state**: `rowSpan` field exists on `tableCell` but is always set to 1.

**Implementation**: Read `rowspan` attribute. During layout, track which cells span rows. The
spanned rows' total height must accommodate the cell's content. Position rowspan cells at their
starting row's block offset, extending through the span.

### Checkpoint structure
1. **Checkpoint 1**: Fix 10A (cell size lookup) — simplest, highest-impact fix. Compile, run tests.
2. **Checkpoint 2**: Add border-spacing (10D). Compile, run tests.
3. **Checkpoint 3**: Add caption support (10C). Compile, run tests.
4. **Checkpoint 4**: Implement border-collapse (10B) if time permits — this is the most complex.
5. **Checkpoint 5**: Add rowspan (10E) if time permits.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/border-conflict' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/contiguous-floated-table' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/caption-side' ./pkg/visualtest/ -timeout 120s`
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/row-progression' ./pkg/visualtest/ -timeout 120s`

---

## Agent 11: Bidi Mirroring + Unicode-Bidi Control Characters

**Files**: `pkg/layout/engine.go` (lines 200-213), `pkg/layout/inline_item.go` (lines 194-221)
**Expected improvement**: +40-57 WM tests

### CRITICAL CONSTRAINT — Follow UAX#9 and Blink's Bidi Architecture

Bidi support in Blink is split across several components:
1. `NGInlineNode::CollectInlines()` — injects Unicode bidi control characters for CSS `unicode-bidi`
2. `NGBidiParagraph` — runs the UAX#9 algorithm (via ICU) to assign bidi levels
3. `NGLineBreaker` — shapes text runs
4. `NGInlineLayoutAlgorithm::PlaceItems()` — reorders items for display
5. Text painting — applies UAX#9 L4 bidi mirroring

Our engine follows this same split (CollectInlines → ResolveBidiLevels → LineBreaker → paint).
The issue is that steps 1 and 5 are incomplete.

**IMPORTANT**: The bidi test failures are NOT specific to vertical writing modes. They affect ALL
writing modes. The tests happen to be in the css-writing-modes directory because they test the
interaction of `direction`/`unicode-bidi` with writing modes, but the underlying bugs are in
the core bidi pipeline.

### Root Cause 1 (PRIMARY, ~57 tests): Missing UAX#9 L4 bidi mirroring

**The bug**: In `engine.go:207`, `reverseRunes(text)` reverses rune order for RTL text runs but
does NOT apply character mirroring. UAX#9 rule L4 states:

> "A character is depicted by a mirrored glyph if and only if the resolved directionality of
> that character is R, and the Bidi_Mirrored property value of that character is Y."

Characters that must be mirrored when their resolved bidi level is odd (RTL):
- `>` (U+003E) ↔ `<` (U+003C)
- `(` (U+0028) ↔ `)` (U+0029)
- `[` (U+005B) ↔ `]` (U+005D)
- `{` (U+007B) ↔ `}` (U+007D)
- `«` (U+00AB) ↔ `»` (U+00BB)
- Plus ~300 other Unicode paired characters (full list in Unicode BidiMirroring.txt)

**Evidence**: In test `bidi-embed-001`, our engine renders `> a > ℵ >` where the reference shows
`> a < ℵ >`. The `>` between `a` and `ℵ` resolves as RTL (bidi level 1) and should be mirrored to `<`.

**Pixel diff pattern**: All diffs have max diff 255 (black vs white). Pixel counts cluster in
multiples of ~61 pixels (area of one glyph at the test's font size). This confirms the issue is
individual character rendering, not layout.

**Blink approach**: Blink applies bidi mirroring in `ShapeResult::ApplyTextReorder()` and in the
font shaping pipeline. When a character's resolved bidi level is odd (RTL), the shaping engine
checks Unicode's `Bidi_Mirrored` property and substitutes the mirror glyph.

**The fix** — In `engine.go`, replace `reverseRunes(text)` with `reverseAndMirrorRunes(text)`:

```go
// Add a bidi mirror map (covering the ~20 most common pairs is sufficient for WPT tests):
var bidiMirrorMap = map[rune]rune{
    '(': ')', ')': '(',
    '[': ']', ']': '[',
    '{': '}', '}': '{',
    '<': '>', '>': '<',
    '\u00AB': '\u00BB', '\u00BB': '\u00AB', // « »
    '\u2039': '\u203A', '\u203A': '\u2039', // ‹ ›
    '\u2045': '\u2046', '\u2046': '\u2045', // ⁅ ⁆
    '\u207D': '\u207E', '\u207E': '\u207D', // ⁽ ⁾
    '\u208D': '\u208E', '\u208E': '\u208D', // ₍ ₎
    '\u2308': '\u2309', '\u2309': '\u2308', // ⌈ ⌉
    '\u230A': '\u230B', '\u230B': '\u230A', // ⌊ ⌋
    '\u2329': '\u232A', '\u232A': '\u2329', // 〈 〉
    '\u27E8': '\u27E9', '\u27E9': '\u27E8', // ⟨ ⟩
}

func reverseAndMirrorRunes(s string) string {
    runes := []rune(s)
    for i, j := 0, len(runes)-1; i <= j; i, j = i+1, j-1 {
        // Mirror both runes
        ri, rj := runes[i], runes[j]
        if m, ok := bidiMirrorMap[ri]; ok { ri = m }
        if m, ok := bidiMirrorMap[rj]; ok { rj = m }
        runes[i], runes[j] = rj, ri
    }
    return string(runes)
}
```

Alternatively, use the `golang.org/x/text/unicode/bidi` package which may provide mirroring
functions. Check if `bidi.Properties` has a `IsMirrored()` method and a mirror lookup.

### Root Cause 2 (SECONDARY): Missing CSS `unicode-bidi` control character injection

**The bug**: When `CollectInlines()` encounters an element with `unicode-bidi: embed/isolate/override/
isolate-override`, it should inject Unicode bidi control characters into the text content:

| CSS `unicode-bidi` | CSS `direction` | Open character | Close character |
|---------------------|-----------------|----------------|-----------------|
| `embed` | `rtl` | RLE (U+202B) | PDF (U+202C) |
| `embed` | `ltr` | LRE (U+202A) | PDF (U+202C) |
| `isolate` | `rtl` | RLI (U+2067) | PDI (U+2069) |
| `isolate` | `ltr` | LRI (U+2066) | PDI (U+2069) |
| `bidi-override` | `rtl` | RLO (U+202E) | PDF (U+202C) |
| `bidi-override` | `ltr` | LRO (U+202D) | PDF (U+202C) |
| `isolate-override` | `rtl` | RLI + RLO | PDF + PDI |
| `isolate-override` | `ltr` | LRI + LRO | PDF + PDI |
| `plaintext` | — | FSI (U+2068) | PDI (U+2069) |

**Blink approach**: In `NGInlineNode::CollectInlines()`, when processing open/close tags,
Blink calls `InlineItemsBuilder::InsertBidiOverride()` / `InsertBidiIsolate()` which inject
the appropriate control characters into the text content. The existing UAX#9 bidi paragraph
algorithm then processes these characters to determine embedding levels.

**Implementation in our engine**: In `inline_item.go`, function `collectInlinesRecursive`,
when emitting `InlineItemOpenTag` (around line 195) and `InlineItemCloseTag` (around line 218):

```go
// At open tag:
if bidi := childStyle.Get("unicode-bidi"); bidi != "" && bidi != "normal" {
    dir := childStyle.GetDirection()
    switch bidi {
    case "embed":
        if dir == "rtl" { text.WriteRune('\u202B') } else { text.WriteRune('\u202A') }
    case "isolate":
        if dir == "rtl" { text.WriteRune('\u2067') } else { text.WriteRune('\u2066') }
    case "bidi-override":
        if dir == "rtl" { text.WriteRune('\u202E') } else { text.WriteRune('\u202D') }
    case "isolate-override":
        if dir == "rtl" { text.WriteRune('\u2067'); text.WriteRune('\u202E') }
        else { text.WriteRune('\u2066'); text.WriteRune('\u202D') }
    case "plaintext":
        text.WriteRune('\u2068')
    }
}

// At close tag:
if bidi := style.Get("unicode-bidi"); bidi != "" && bidi != "normal" {
    switch bidi {
    case "embed", "bidi-override":
        text.WriteRune('\u202C') // PDF
    case "isolate":
        text.WriteRune('\u2069') // PDI
    case "isolate-override":
        text.WriteRune('\u202C'); text.WriteRune('\u2069') // PDF + PDI
    case "plaintext":
        text.WriteRune('\u2069') // PDI
    }
}
```

The injected control characters will be processed by the existing `ResolveBidiLevels()` function
which uses `golang.org/x/text/unicode/bidi` — the bidi algorithm already handles these characters
correctly. The key insight is that we just need to inject them; the algorithm does the rest.

### Checkpoint structure
1. **Checkpoint 1**: Implement bidi mirroring (Root Cause 1). Compile, run bidi tests. Report pass counts.
2. **Checkpoint 2**: Implement unicode-bidi control character injection (Root Cause 2). Compile, run tests. Report.
3. **Checkpoint 3**: Run full WM test suite to check for regressions. Report final counts.

### Test verification
Run: `go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 300s 2>&1 | tail -10`

---

## Merge Strategy

Merge order (least to most likely to conflict with earlier merges):

**Phase 1 — WM fixes (block_layout.go + other files):**
1. **Agent 9** (abs-pos paint order) — touches engine.go, minimal conflict risk. MERGE FIRST because it fixes ~120 tests with a 1-line change.
2. **Agent 11** (bidi mirroring) — touches engine.go + inline_item.go, independent from layout changes
3. **Agent 5** (rel-pos + clip) — touches block_layout.go minimally + render/
4. **Agent 7** (float/clear) — touches block_layout.go + exclusion_space.go
5. **Agent 6** (% margins/padding) — touches block_layout.go + fragment_geometry.go
6. **Agent 8** (orthogonal sizing) — touches block_layout.go + min_max_sizing.go
7. **Agent 10** (table layout) — touches table_layout.go, independent from block_layout changes

**Phase 2 — Flex fixes (flex_layout.go):**
8. **Agent 1** (flex justify + alignment) — flex_layout.go only
9. **Agent 3** (flex min sizing) — flex_layout.go only
10. **Agent 4** (flex aspect ratio) — flex_layout.go only
11. **Agent 2** (flex baseline) — flex_layout.go + layout_result.go + others

After each merge:
1. `go build ./...` — verify compilation
2. Run full WM + flexbox suites — verify no regressions
3. Record pass/fail counts

---

## Coordinator Goals

The coordinator's job is:

1. **Launch all 11 agents** in isolated worktrees
2. **Monitor completion** — agents report back when done
3. **Evaluate results** — for each agent:
   - Did it compile? (`go build ./...`)
   - Did it run the targeted tests? What were pass/fail counts?
   - Did it introduce regressions in other test suites?
   - Did it stay close to the Blink approach?
4. **Merge in order** — follow the merge strategy above
5. **Resolve conflicts** — most will be mechanical (adjacent line changes in shared files)
6. **Run full suites** after all merges to get final counts
7. **Write summary** — before/after for each agent's contribution

### Acceptance criteria for each agent's work:
- Compiles cleanly (`go build ./...`)
- Targeted tests improve (pass count goes up)
- No regression in other tests (overall pass count doesn't drop)
- Code follows existing patterns (ConstraintSpace/LayoutResult/LogicalFragment/etc.)
- Changes are minimal and focused (no unnecessary refactoring)

---

## Continuation Prompt

If the coordinator session is restarted, use the following prompt:

---

**Continuation prompt for coordinator:**

I am coordinating 11 parallel agents working on WM and flexbox test improvements for the louis14 browser engine. Each agent runs in an isolated worktree on its own branch.

**Baseline**: WM 327/790 (41.4%), Flexbox 380/630 (60.3%)
**Branch**: `rewrite/louis13-louis14`

**Agents and their branches** (check `git branch -a` and `.claude/worktrees/` for actual branch names):

| Agent | Task | Files | Status |
|-------|------|-------|--------|
| 1 | Flex justify-content + alignment keywords | flex_layout.go | ? |
| 2 | Flex baseline alignment | flex_layout.go, layout_result.go, fragment_builder.go, block_layout.go | ? |
| 3 | Flex automatic minimum sizing (§4.5) | flex_layout.go | ? |
| 4 | Flex aspect-ratio + replaced elements | flex_layout.go | ? |
| 5 | WM relative positioning + CSS clip rect | block_layout.go, render/paint_layer.go | ? |
| 6 | WM percentage margins/padding | block_layout.go, fragment_geometry.go, css/style.go | ? |
| 7 | WM float/clear mapping | block_layout.go, exclusion_space.go | ? |
| 8 | WM orthogonal flow sizing | block_layout.go, min_max_sizing.go | ? |
| 9 | Abs-pos paint order + vertical page layout | engine.go, paint_layer.go | ? |
| 10 | Table layout in vertical writing modes | table_layout.go, css/style.go | ? |
| 11 | Bidi mirroring + unicode-bidi control chars | engine.go, inline_item.go | ? |

**To check agent status**:
1. Look in `.claude/worktrees/` for worktree directories
2. Check git branches: `git branch --list 'agent-*'`
3. Try to read the worktree's latest changes

**To merge completed work** (in order — this order matters!):

Phase 1 — WM fixes:
1. Agent 9 (abs-pos paint order — biggest single win, ~120 tests)
2. Agent 11 (bidi mirroring — ~57 tests)
3. Agent 5 (rel-pos + clip)
4. Agent 7 (float/clear)
5. Agent 6 (% margins/padding)
6. Agent 8 (orthogonal sizing)
7. Agent 10 (table layout)

Phase 2 — Flex fixes:
8. Agent 1 (justify + alignment)
9. Agent 3 (min sizing)
10. Agent 4 (aspect ratio)
11. Agent 2 (baseline — most files touched, merge last)

For each merge:
```bash
git merge --no-ff <agent-branch>
go build ./...
go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5
go test -v -run TestWPTCSS3Reftests/css-flexbox ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5
```

**Acceptance**: Each merge must compile and not regress overall test counts. If a merge causes regressions, investigate before proceeding.

**Full plan details**: See `docs/PARALLEL-AGENT-PLAN.md`

---
