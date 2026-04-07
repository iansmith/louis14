# Flexbox Round 1: Top 3 Improvements

Current state: 496 pass / 133 fail (78.9% pass rate) across 629 tests.
Test command: `cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1`

These three targets are **independent** (touch different source files) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Flex Baseline Alignment (~19 tests)

### Problem
Baseline alignment for flex items has multiple issues:
1. **Empty inline-block baseline fallback**: When computing the flex container's baseline from items that have no line boxes (empty blocks), the code falls back to font metrics instead of using the bottom margin edge per CSS 2.1 §10.8.1.
2. **Collapsed items in baseline calculations**: Items with `visibility:collapse` should contribute to cross-size calculations but should NOT participate in determining the shared baseline for item positioning.
3. **Container baseline for column flex**: The container's reported baseline always uses `firstItem.baseline`, but for column flex the first item's main-axis offset should be included correctly and baseline-aligned items need proper handling.

### Affected Tests (~19 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-baseline-multi-item-horiz | 2 | 001a, 001b |
| flexbox-baseline-multi-item-vert | 2 | 001a, 001b |
| flexbox-baseline-multi-line-horiz | 1 | 002 |
| flexbox-baseline-multi-line-vert | 2 | 001, 002 |
| flexbox-baseline-align-self-baseline | 2 | horiz-001, vert-001 |
| flexbox-collapsed-item-baseline | 1 | 001 |
| flexbox-align-self-baseline-horiz | 6 | 001a, 001b, 003, 006, 007, 008 (xhtml) |
| baseline-synthesis | 2 | 002, vert-lr-line-under |
| fieldset-baseline-alignment | 1 | 001 |

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`**

**Issue 1 — Lines 449-459**: Baseline items include collapsed items in the ascent/descent tracking:
```go
if selfAlign == "baseline" && item.baseline > 0 {
    // No check for item.collapsed!
    ascent := item.crossMarginStart() + item.baseline
    descent := outerCross - ascent
    ...
}
```

**Issue 2 — Lines 787-806**: The shared baseline computation for positioning also doesn't filter collapsed items:
```go
for _, item := range line.items {
    selfAlign := fla.getAlignSelf(item.style, alignItems)
    if selfAlign == "baseline" && item.baseline > 0 {
        // Should also check !item.collapsed here
        b := item.crossMarginStart() + item.baseline
        ...
    }
}
```

**Issue 3 — Lines 1026-1038**: Container baseline always uses `firstItem.baseline` even when the first item is collapsed or doesn't participate in baseline alignment. Per CSS Flexbox §4.2, the container's baseline should come from the first baseline-participating item:
```go
if len(lines) > 0 && len(lines[0].items) > 0 {
    firstItem := lines[0].items[0] // Should prefer first baseline-aligned item
    ...
}
```

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- `NGFlexLayoutAlgorithm::LayoutInternal()` tracks baseline items separately from collapsed items
- `NGFlexLine::ComputeLineCrossSize()` explicitly skips collapsed items when computing the shared baseline
- `NGFlexLayoutAlgorithm::PropagateBaselinesToContainer()` iterates items to find the first baseline-participating item, not just the first item
- Collapsed items participate in cross-size computation (for gap preservation) but are excluded from baseline sharing groups

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Lines 449 and 460**: Add `!item.collapsed` guard:
```go
if selfAlign == "baseline" && item.baseline > 0 && !item.collapsed {
```

2. **Lines 789-790**: Add `!item.collapsed` guard to the positioning baseline computation:
```go
if selfAlign == "baseline" && item.baseline > 0 && !item.collapsed {
```

3. **Lines 1026-1038**: Find the first non-collapsed baseline-participating item for container baseline:
```go
// Find first baseline-participating item (non-collapsed, with baseline alignment or first visible)
var baselineItem *flexItem
for _, item := range lines[0].items {
    if item.collapsed { continue }
    selfAlign := fla.getAlignSelf(item.style, alignItems)
    if selfAlign == "baseline" && item.baseline > 0 {
        baselineItem = item
        break
    }
    if baselineItem == nil {
        baselineItem = item // fallback to first non-collapsed item
    }
}
```

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-collapsed-item" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/baseline-synthesis" -count=1
# Full regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 496 (current passing count)
```

---

## Target 2: Content-Based Flex Sizing (min/max-content) (~11 tests)

### Problem
When computing intrinsic sizes for flex containers and flex items with `flex-basis: content`, the `computeContentMinMaxSizes` function in `min_max_sizing.go` has issues:
1. It doesn't have a flex-container path — flex containers whose children are measured for `flex-basis: content` don't get the flex-specific `measureFlexMinMax` treatment.
2. The `measureFlexMinMax` function uses `ComputeMinMaxSizes` (which respects explicit CSS width) instead of `computeContentMinMaxSizes` (which ignores it) for child items, meaning items with both explicit width and flex-basis:content get the wrong size.

### Affected Tests (~11 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-flex-basis-content | 8 | 001a/b, 002a/b, 003a/b, 004a/b |
| flex-container-max-content | 1 | 001 |
| flex-container-min-content | 1 | 001 |
| flex-item-content-is-min-width-max-content | 1 | — |

### Root Cause (from code analysis)

**File: `pkg/layout/min_max_sizing.go`**

**Issue 1 — Lines 119-157** (`computeContentMinMaxSizes`): This function is supposed to compute content-based sizes ignoring explicit CSS inline-size. But it lacks a flex container path:
```go
func computeContentMinMaxSizes(...) MinMaxSizes {
    // ...
    var result MinMaxSizes
    if hasOnlyInlineChildren(node) {
        result = measureInlineMinMax(node, ctx, space)
    } else {
        result = measureBlockMinMax(node, ctx, space)
    }
    // No flex path! Flex containers with flex children get measureBlockMinMax
    // which treats flex items as block children, ignoring flex sizing.
```

Compare with `ComputeMinMaxSizes` (line 44) which correctly routes to `measureFlexMinMax`.

**Issue 2 — Lines 302-305** (`measureFlexMinMax`): Child item sizes use `ComputeMinMaxSizes` which respects explicit CSS width:
```go
if !transferred {
    childMM := ComputeMinMaxSizes(ctx, child, childSpace) // Respects CSS width
    childMin = childMM.MinContent + childBP + childMargins.InlineSum()
```

For `flex-basis: content` items, this should use content-based sizing that ignores the item's explicit width/height in the main axis direction.

### What Blink Does

In Blink's `min_max_sizes_utils.cc`:
- `ComputeMinAndMaxContentContributionForSelf()` distinguishes between items with explicit main-size and those with `flex-basis: content`
- For `flex-basis: content`, it calls `ComputeMinAndMaxContentContribution()` which bypasses the explicit size short-circuit
- `NGFlexLayoutAlgorithm::ComputeNextFlexLine()` uses `ChildMinMaxSizesForMinContentContribution()` which correctly handles the content keyword

### Fix Location

**File: `pkg/layout/min_max_sizing.go`**

1. **Line ~133**: Add flex container routing to `computeContentMinMaxSizes`:
```go
var result MinMaxSizes
display := style.GetDisplay()
if display == css.DisplayFlex || display == css.DisplayInlineFlex {
    result = measureFlexMinMax(node, ctx, space)
} else if hasOnlyInlineChildren(node) {
    result = measureInlineMinMax(node, ctx, space)
} else {
    result = measureBlockMinMax(node, ctx, space)
}
```

2. **Lines 302-306**: Consider whether `measureFlexMinMax` child measurement should use content-based sizing. This is more nuanced — when a flex container is being measured for its OWN intrinsic size, child items' intrinsic contributions should use their CSS width normally (the "content" keyword only applies when the item's own flex-basis is "content", not when measuring the container).

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-basis-content" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-container-m" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-content" -count=1
# Full regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 496
```

---

## Target 3: Inline-Block Baseline for Empty Boxes (~30+ tests)

### Problem
Many WPT flex tests use reference files (`.xhtml`) that render expected output using `display: inline-block` children with `line-height: 0`. Our inline layout computes incorrect baselines for empty inline-block elements, causing reference files to render with inflated line-box heights. This makes correct flex output appear different from the (incorrectly rendered) reference.

The root bug: when an inline-block has no line boxes (empty content), CSS 2.1 §10.8.1 says "the baseline is the bottom margin edge of the box." Our code instead falls back to font metrics, which gives a baseline ~80% up from the bottom of the font-size (e.g., 13px for 16px font), causing the line box to be taller than the inline-block's actual height.

### Affected Tests (~30+ failures)

These xhtml tests all use inline-block reference patterns and have CORRECT flex output that doesn't match the incorrectly-rendered reference:

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-justify-content-horiz | 5 | 001a, 001b, 002, 004, 005 |
| flexbox-align-self-vert | 4 | 001, 002, 003, 004 |
| flexbox-align-self-vert-rtl | 5 | 001, 002, 003, 004, 005 |
| flexbox-align-self-horiz | 3 | 001-table, 002, 004 |
| flexbox-basic-*-horiz/vert | ~14 | canvas, img, textarea, video, iframe, block variants |
| flexbox-mbp-horiz | 2 | 002v, 004 |
| flexbox-sizing-horiz/vert | 4 | 001, 002 (both) |
| flexbox-whitespace-handling | 2 | 001a, 001b |

Note: Some of these also have real flex bugs (e.g., baseline alignment in column flex). The inline-block fix addresses the reference-side rendering.

### Root Cause (from code analysis)

**File: `pkg/layout/inline_layout.go`**

**Lines 1001-1019** in `computeLineMetricsEx`: When processing atomic inlines (inline-block), the baseline fallback for empty boxes uses font metrics instead of the bottom edge:

```go
atomicBaseline := r.LayoutResult.LastBaseline  // 0 for empty inline-blocks
if display == css.DisplayInlineFlex && r.LayoutResult.Baseline > 0 {
    atomicBaseline = r.LayoutResult.Baseline
}
if isInlineBlockLike && (atomicBaseline > 0 || !centralBaseline) {
    var ibAscent float64
    if atomicBaseline > 0 {
        ibAscent = atomicBaseline
    } else if centralBaseline {
        ibAscent = blockSize / 2
    } else {
        // BUG: Uses font metrics instead of bottom margin edge!
        fontPath := resolveFontPath(r.Item.Style, fonts)
        fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
        ibAscent = text.FontAscentFromFont(fontSize, fontPath)
    }
```

When `atomicBaseline` is 0 (empty inline-block), the `else` branch uses font ascent (~13px for 16px font) as the baseline. But CSS says the baseline should be the bottom margin edge, which means `ibAscent = blockSize` (e.g., 10px).

**Impact**: For a 10px-tall inline-block with 16px font:
- Our code: ibAscent=13, descent=max(0, 10-13)=0, total line height=13
- Correct: ibAscent=10, descent=0, total line height=10
- Every reference container is 3px too tall, cascading down the page

There's also a secondary issue on **lines 923-929 and 953-959**: negative half-leading from `line-height: 0` is clamped to 0:
```go
halfLeading := (lineHt - (ascent + descent)) / 2
if halfLeading > 0 {  // BUG: Should allow negative half-leading
    ascent += halfLeading
    descent += halfLeading
}
```

Per CSS 2.1 §10.8.1, negative half-leading reduces the inline box's contribution. With `line-height: 0`, half-leading is very negative and should reduce the text/strut contribution to 0, letting inline-block heights dominate. By clamping to 0, the strut remains at full font metrics.

### What Blink Does

In Blink's `inline_box_state.cc`:
- `InlineBoxState::ComputeTextMetrics()` correctly applies negative half-leading, reducing ascent/descent when line-height < font-size
- `InlineBoxState::AccumulateUsedFonts()` and `ComputeMetrics()` handle the strut with the container's line-height, allowing it to be 0
- For atomic inlines (inline-blocks), `NGInlineLayoutAlgorithm::PlaceAtomicInline()` uses the atomic's `LastBaseline()` which returns the bottom margin edge for boxes without line boxes
- `NGBlockNode::UseLastBaselineForInlineBaseline()` returns true when the box has no line boxes, causing the baseline to be at the bottom

### Fix Location

**File: `pkg/layout/inline_layout.go`**

1. **Lines 1017-1019**: When `atomicBaseline == 0` for a non-empty inline-block (has explicit height), use `blockSize` as the baseline:
```go
} else {
    // CSS 2.1 §10.8.1: empty inline-block's baseline is its bottom margin edge.
    ibAscent = blockSize
}
```

2. **Lines 925-929 and 955-959**: Allow negative half-leading to reduce contributions:
```go
halfLeading := (lineHt - (ascent + descent)) / 2
// Allow negative half-leading per CSS 2.1 §10.8.1
ascent += halfLeading
descent += halfLeading
if ascent < 0 { ascent = 0 }
if descent < 0 { descent = 0 }
```

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-justify-content-horiz" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert-001" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-canvas" -count=1
# CRITICAL: Also run CSS2 and writing-modes tests to check for regressions:
cd pkg/visualtest && go test -v -run "TestWPTReftests" -count=1 2>&1 | grep -c "REFTEST PASS"
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -c "REFTEST PASS"
# Inline-block changes affect ALL tests, not just flexbox!
```

---

## Independence Check

| | flex_layout.go | min_max_sizing.go | inline_layout.go |
|---|---|---|---|
| Target 1 (Baseline) | **Yes** | - | - |
| Target 2 (Content sizing) | - | **Yes** | - |
| Target 3 (Inline-block baseline) | - | - | **Yes** |

All three targets touch different source files and can be developed independently.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Key Blink files:
  - Flex: `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`
  - Inline: `third_party/blink/renderer/core/layout/inline/inline_box_state.cc`
  - Min/max: `third_party/blink/renderer/core/layout/min_max_sizes_utils.cc`
- **Commit and report at each milestone** (don't batch everything to the end).
- **Regression constraints**: Current passing count is 496 flex tests. After your fix, this number must NOT decrease. Run the full flex test suite and report the new pass count.
- **Target 3 is high-risk**: Inline-block baseline changes affect ALL tests (CSS2, writing-modes, flexbox, etc.). Run broad regression tests before committing.
- **Do not chase individual tests**: Fix the root cause algorithm, verify it works for ALL affected tests, and check for regressions.
