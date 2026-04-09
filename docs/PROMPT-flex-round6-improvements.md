# Flexbox Round 6: Top 5 Improvements

Current state: 395 pass / 101 fail (79.6% pass rate) across 496 tests.

These five targets are ranked by foundational impact. Targets 1, 3, and 4 all modify `flex_layout.go` but in **non-overlapping line ranges** (separated by hundreds of lines), so git can auto-merge them. Target 2 overlaps with Target 1 in the alignment code (lines 886-984) and **must run after Target 1 merges**.

**Recommended parallel group: Targets 1 + 3 + 4 + 5** (four agents).
**Sequential after merge: Target 2** (depends on Target 1's changes to the alignment switch).

## IMPORTANT: Test Efficiency Rules

- **Do NOT run the full flex test suite** (`TestWPTCSS3Reftests/css-flexbox`) during development. Only run it once at the very end, after all targets are complete.
- **Run only the specific tests** for the feature you're implementing. Each target below lists its exact verification commands.
- This saves significant time — the full suite takes ~45 seconds per run.

## IMPORTANT: Project Rules (from CLAUDE.md)

- **Foundational correctness over quick wins**: Every fix must work for ALL cases. Don't chase easy wins.
- **Study Blink BEFORE writing code**: Look at Blink/Chromium's implementation first. Mirror their types, algorithm structure, and constraint-passing patterns.
- **All tests must pass at 0% diff**: A 0.5% diff is a failure. Never dismiss failures as font rendering or anti-aliasing.
- **Never use `open`** to display files from agents.
- **Commit and report at each milestone**, not just at the end.
- **NEVER commit outside your worktree.** When running in a worktree, commit ONLY to your worktree branch. Never commit to fix/* or master branches from a worktree. If you are unsure whether you are in a worktree, check your current directory — if it is NOT `~/louis14`, you are in a worktree.

---

## Target 1: Baseline Alignment Pipeline (~13 tests)

### Problem
Flex baseline alignment has multiple bugs: the first-pass cross-size computation excludes items with `baseline == 0` via an incorrect `> 0` guard, baseline synthesis is too restrictive (only horizontal row flex), and the container baseline export logic doesn't follow Blink's priority ordering.

### Affected Tests (~13 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| align-self: baseline (horizontal) | 6 | flexbox-align-self-baseline-horiz-001a/001b/003/006/007/008 |
| Container baseline (inline-flex) | 2 | flexbox-baseline-multi-item-horiz-001a/001b |
| Multi-line baseline | 3 | flexbox-baseline-multi-line-horiz-002, -vert-001, -vert-002 |
| Baseline + align-self | 1 | flexbox-baseline-align-self-baseline-horiz-001 |
| Collapsed item baseline | 1 | flexbox-collapsed-item-baseline-001 |

### Root Cause (from code analysis)

**Bug 1 — `item.baseline > 0` guard (line 499 of `flex_layout.go`):**
```go
if selfAlign == "baseline" && item.baseline > 0 && baselineParallel {
```
An item whose first-line baseline is exactly 0 (text ascent at the top of its border-box) is excluded from baseline participation in the line cross-size calculation. Blink has no magnitude check — only `alignment == kBaseline`. This causes the line cross-size to be underestimated when such items participate.

**Bug 2 — `canSynthesize` restriction (line 846):**
```go
canSynthesize := isRow && !wdm.IsVertical()
```
Baseline synthesis is disabled for all non-horizontal-row cases. Blink always synthesizes via `FirstBaselineOrSynthesize(font_baseline)`. This means column flex items and vertical-writing-mode items never get synthesized baselines, causing them to fall back to `flex-start` instead of proper baseline alignment.

**Bug 3 — No baseline group concept:**
Blink uses `BaselineGroup` (`kMajor`/`kMinor`) which accounts for `is_wrap_reverse_` and axis orientation. The current code uses a single `sharedBaseline` per line with no distinction. For `flex-wrap: wrap-reverse`, items in the minor baseline group should use `crossMarginEnd + baseline` instead of `crossMarginStart + baseline`.

### What Blink Does
In Blink's `flex_layout_algorithm.cc`:
- `DetermineBaselineGroup()` assigns each item to kMajor or kMinor based on wrap-reverse and writing mode
- `FirstBaselineOrSynthesize(font_baseline)` always returns a usable baseline
- `BaselineAccumulator` tracks major/minor groups separately with priority ordering
- Container baseline search uses `kMajor > kMinor > fallback` priority

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Line 499**: Remove the `item.baseline > 0` guard. Change to:
   ```go
   if selfAlign == "baseline" && baselineParallel {
       bl := item.baseline
       if !item.hasBaseline {
           bl = 0 // synthesize at block-start
       }
   ```

2. **Line 846**: Expand synthesis to all cases:
   ```go
   canSynthesize := true // Always synthesize per CSS Align §4.4
   ```

3. **Lines 855-868**: Fix the `bl > 0 || item.hasBaseline` guard to always participate when `selfAlign == "baseline"`:
   ```go
   if selfAlign == "baseline" && baselineParallel {
       bl := item.baseline
       if !item.hasBaseline {
           bl = 0
       }
       b := item.crossMarginStart() + bl
       if b > sharedBaseline {
           sharedBaseline = b
       }
       hasBaselineItem = true
   }
   ```

4. **Lines 1113-1192**: Follow Blink's priority for container baseline: prefer baseline-aligned items, then fall back to first non-collapsed item.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-collapsed-item-baseline" -count=1
```

Check for regressions in nearby passing tests:
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-horiz" -count=1
```

---

## Target 2: Column Flex Alignment, RTL, and Wrapping (~19 tests)

**NOTE: This target shares the alignment switch (lines 886-1027) with Target 1. Run this AFTER Target 1 merges.**

### Problem
Column flex containers with `align-self` produce large pixel diffs (6-11%), indicating items are positioned at fundamentally wrong cross-axis offsets. RTL column flex is also broken. Column flex wrapping (`flex-wrap: wrap` with `flex-direction: column`) misplaces items into wrong columns.

### Affected Tests (~19 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| align-self in column flex | 4 | flexbox-align-self-vert-001/002/003/004 |
| align-self in column RTL | 5 | flexbox-align-self-vert-rtl-001/002/003/004/005 |
| Column flex stretch | 1 | flexbox-align-self-stretch-vert-002 |
| Column flex wrapping | 2 | flexbox-flex-wrap-vert-001/002 |
| Writing mode interactions | 3 | flexbox-writing-mode-013/014/015 |
| align-self with tables | 1 | flexbox-align-self-horiz-001-table |
| align-self horiz misc | 1 | flexbox-align-self-horiz-002 |
| Column fragmentation | 2 | flexbox-break-request-vert-001a/001b (tentative) |

### Root Cause (from code analysis)

**Bug 1 — Column flex cross-offset placement (lines 1020-1027):**
```go
if isRow {
    inlineOff = item.mainOffset
    blockOff = item.crossOffset + item.crossMarginStart()
} else {
    inlineOff = item.crossOffset + item.crossMarginStart()
    blockOff = item.mainOffset
}
```
For column flex, the `crossOffset` is a logical inline offset. For RTL containers, `InlineOffset=0` should map to the physical right edge via the fragment builder's logical-to-physical conversion. The issue may be that `crossOffset` is being computed as a physical offset already (from the alignment switch) rather than a logical one. Trace through `flexbox-align-self-vert-rtl-001.xhtml` with debug logging to find where the physical/logical mismatch occurs.

**Bug 2 — `selfStartIsCrossStart` for column flex with RTL (lines 78-103):**
For HTB RTL column flex, `containerCrossStart = physicalInlineStart(containerWDM) = sideRight`. The function correctly identifies that `self-start == cross-start` for same-WDM items. But the alignment switch at line 938 uses this to position `self-start` items. Verify that the `crossFreeForAlign` computation at line 912 is correct for column flex where the cross axis is the inline axis.

**Bug 3 — Column flex wrapping cross-size computation:**
For `flexbox-flex-wrap-vert-001`, the flex container has `width:12px; height:100px`. Items wrapping to new columns should be placed at increasing inline offsets. The `computeAlignContent` function distributes cross-axis space across lines. Verify that `align-content: stretch` (default) distributes the 12px inline width correctly across wrapped columns.

### What Blink Does
Blink's `FlexLayoutAlgorithm::PlaceFlexItems()` always works in logical coordinates relative to the container's writing mode. The `LogicalOffset` for each item is converted to physical by the fragment builder, which correctly handles RTL. Key: Blink never manually adjusts for RTL in the flex algorithm — the builder handles it.

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Lines 886-980**: Trace through the alignment switch with a column RTL test case. Enable `flexDebug` and compare the `crossOffset` values against expected physical positions. The likely fix is ensuring `crossOffset` is always logical (0 = inline-start of container) and the builder handles RTL conversion.

2. **Lines 60-147**: Verify axis mapping functions produce correct results for all column + RTL + vertical WM combinations. Add test cases for the specific failing combinations.

3. **Lines 2279-2372** (`buildItemConstraintSpace`): For column flex wrapping, verify that `crossInlineContent` is set correctly for each wrapped line.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert-rtl" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-wrap-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-writing-mode-01" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-horiz" -count=1
```

---

## Target 3: Automatic Minimum Size (~11 tests)

### Problem
The `flexItemMinMain` function (§4.5 automatic minimum size) has bugs in: (1) the content suggestion for replaced elements in row flex returning 0, (2) the specified size suggestion not resolving `calc()` heights, and (3) the overflow-x/y propagation check. These cause flex items to shrink below their minimum content size or fail to clamp at the specified size.

### Affected Tests (~11 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| min-height: auto (column) | 4 | flexbox-min-height-auto-001/002c/003/004 |
| min-width: auto (row, images) | 2 | flexbox-min-width-auto-005/006 |
| Minimum height flex items | 3 | flex-minimum-height-flex-items-019/023/030 |
| Minimum width flex items | 1 | flex-minimum-width-flex-items-016 |
| max-height: min-content | 1 | flex-item-max-height-min-content |

### Root Cause (from code analysis)

**Bug 1 — Content suggestion for replaced elements in row flex (line 3037):**
```go
mm := computeContentMinMaxSizes(fla.ctx, child, minContentSpace)
contentSuggestion = mm.MinContent
```
`computeContentMinMaxSizes` may return `MinContent = 0` for replaced elements (images) because they have no block-level children — the inline min-content computation doesn't account for the element's intrinsic width. For an `<img>` with intrinsic width 40px, the content suggestion should be 40px but may be returning 0.

**Bug 2 — Specified size suggestion with `calc()` (line 3084):**
```go
if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
    specifiedSuggestion = explicit
}
```
`ResolveBlockSize` calls `style.GetLength("height")`. For `height: calc(10% + 50px)` in a column flex container with indefinite block-size, the percentage can't resolve, so `GetLength` returns `false`. This means the specified size suggestion is missing, and `autoMin = contentSuggestion` (80px from child) without the 50px cap. Per the spec, the specified size suggestion should use the *resolved* flex basis when it was derived from the main-size property.

**Bug 3 — Content suggestion in column flex may double-count border/padding (line 3063):**
```go
contentSuggestion = result.IntrinsicBlockSize
```
`IntrinsicBlockSize` from block layout is `blockCursor`, which is the content-area height. But `flexItemMinMain` is supposed to return the content-box minimum. If the item has border/padding, and `IntrinsicBlockSize` already includes the child's height but not the item's own border/padding, this is correct. However, if the child has margins that collapse into `IntrinsicBlockSize`, the value could be inflated. Verify with `flexbox-min-height-auto-004` where the `.xvisible` item's content suggestion appears too large.

**Bug 4 — overflow-x propagation check (line 3022):**
For `flexbox-min-height-auto-004`, items with `overflow-x: hidden` should have auto-min disabled (return 0). The code checks both `GetOverflowX()` and `GetOverflowY()`. Verify that `css.Style.GetOverflowX()` correctly returns the propagated value (e.g., `overflow-x:hidden` forces `overflow-y` to `auto`, making it scrollable too).

### What Blink Does
In Blink's `FlexLayoutAlgorithm::ComputeAutomaticMinSize()`:
- Content suggestion for replaced elements uses `ComputeReplacedSize()` with indefinite constraints, not `ComputeMinMaxSizes`
- Specified size suggestion uses the resolved flex base size (already resolved from CSS properties), not re-resolving the raw CSS property
- Overflow check uses `IsScrollContainer()` which checks the computed overflow after cross-axis propagation

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Lines 3028-3038**: For replaced elements in row flex, use `GetIntrinsicSizingInfo` to get the intrinsic width directly, rather than `computeContentMinMaxSizes`:
   ```go
   if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
       info := GetIntrinsicSizingInfo(fla.ctx, child)
       if info.HasWidth {
           contentSuggestion = info.Width
       }
   } else {
       mm := computeContentMinMaxSizes(...)
       contentSuggestion = mm.MinContent
   }
   ```

2. **Lines 3070-3088**: Use the already-resolved `flexBasis` parameter as the specified size suggestion when the flex basis was derived from the main-size property (not from `flex-basis: content` or `flex-basis: <length>`). Pass an additional flag indicating whether flexBasis came from the main-size property.

3. **Lines 3039-3065**: Verify that `IntrinsicBlockSize` for column flex content suggestion doesn't include child margins or the item's own border/padding. Add debug logging for `flexbox-min-height-auto-004`.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-height-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-width-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-max-height" -count=1
```

Check for regressions:
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-basis" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-shrink" -count=1
```

---

## Target 4: Intrinsic Container Sizing + flex-basis:content (~10 tests)

### Problem
The flex container's intrinsic sizing algorithm (`measureFlexMinMax` in `min_max_sizing.go`) and the `flex-basis: content` resolution in actual layout (`resolveFlexBasis` in `flex_layout.go`) have bugs that cause incorrect container widths and incorrect item sizing when `flex-basis: content` is used.

### Affected Tests (~10 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Container max-content | 1 | flex-container-max-content-001 |
| Container min-content | 1 | flex-container-min-content-001 |
| flex-basis: content (column) | 4 | flexbox-flex-basis-content-002a/002b/003a/003b |
| Sizing with MBP | 2 | flexbox-sizing-horiz-002, flexbox-sizing-vert-001 |
| Definite cross-size percentages | 1 | flexbox-definite-cross-size-constrained-percentage |
| Definite sizes | 1 | flexbox-definite-sizes-003 or 004 |

### Root Cause (from code analysis)

**Bug 1 — `flex-basis: content` includes explicit width in contributions (line 384-394 of `min_max_sizing.go`):**
```go
minContrib := contentMM.MinContent
maxContrib := contentMM.MaxContent
if hasExplicit {
    if explicit > minContrib { minContrib = explicit }
    if explicit > maxContrib { maxContrib = explicit }
}
```
When `basisIsContent == true` (line 400-403), the code uses `minContrib`/`maxContrib` which already had `explicit` width mixed in at lines 387-394. Per CSS Flexbox §4.2, `flex-basis: content` means the flex base size comes from the content, and the explicit width should be **ignored** for main-axis sizing in the intrinsic algorithm too. The fix: compute separate `contentMinContrib`/`contentMaxContrib` without the explicit width for the `basisIsContent` path.

**Bug 2 — `flex-basis: content` in column flex resolveFlexBasis (line 1485 of `flex_layout.go`):**
```go
if basisVal == "auto" || basisVal == "content" {
    if basisVal == "auto" {
        // Try explicit main-size...
    }
    // Falls through to content sizing...
}
```
For `flex-basis: content` in column flex, the code correctly skips the explicit main-size check (line 1490 only runs for `"auto"`). However, the content-sizing fallback at the end of this block does a full layout to determine the content block-size. Verify that this layout correctly ignores the item's own `height` property by using `IsContentSuggestionLayout(true)` in the constraint space.

**Bug 3 — Conservative algorithm edge cases:**
For `flex-container-max-content-001`, the conservative algorithm at lines 420-438 checks `cantGrowMin`/`cantShrinkMin` to decide between hypothetical size and CSS Sizing-3 contribution. For items with `flex: auto` (grow=1, shrink=1), the hypothetical size should never be used since the item can always flex. Verify that the `flex` shorthand correctly resolves `flex: auto` to `grow=1, shrink=1, basis=auto`.

### What Blink Does
In Blink's `FlexLayoutAlgorithm::ComputeNextIntrinsicSizeContribution()`:
- The content-based and explicit-based contributions are computed separately
- When `is_content_basis`, only the content contribution is used (explicit width is ignored)
- The conservative algorithm uses `CanFlexGrow()`/`CanFlexShrink()` checks that account for clamped values

### Fix Location

**File: `pkg/layout/min_max_sizing.go`**

1. **Lines 384-403**: Split the contribution computation into content-only and explicit-included paths:
   ```go
   contentMinContrib := contentMM.MinContent
   contentMaxContrib := contentMM.MaxContent
   minContrib := contentMinContrib
   maxContrib := contentMaxContrib
   if hasExplicit {
       if explicit > minContrib { minContrib = explicit }
       if explicit > maxContrib { maxContrib = explicit }
   }
   if basisIsContent {
       childMin = contentMinContrib + outerExtra  // NOT minContrib
       childMax = contentMaxContrib + outerExtra  // NOT maxContrib
   } else { ... }
   ```

**File: `pkg/layout/flex_layout.go`**

2. **Lines 1485-1600** (`resolveFlexBasis`): For `flex-basis: content` in column flex, ensure the content-sizing layout uses `IsContentSuggestionLayout(true)` so the item's explicit `height` is ignored. Read the current fallback code path (after the `auto` check) and verify it produces content-only block size.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-container-m" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-basis-content" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-sizing" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-definite" -count=1
```

Check for regressions:
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-basis" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_flex-" -count=1
```

---

## Target 5: Replaced Element Sizing in Flex + Aspect Ratio (~16 tests)

### Problem
Replaced elements (canvas, img, video, iframe, fieldset, textarea) as flex items have consistent small rendering differences in normal mode and significant layout errors in orthogonal (writing-mode) variants. Additionally, images with intrinsic aspect ratios compute incorrect transferred size suggestions when `min-height` or `box-sizing` is involved.

### Affected Tests (~16 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Basic replaced horizontal | 6 | flexbox-basic-canvas/img/video/fieldset/iframe/textarea-horiz-001 |
| Basic replaced vertical | 6 | flexbox-basic-canvas/img/video/fieldset/iframe/textarea-vert-001 |
| Writing-mode variants | 2 | flexbox-basic-block-horiz-001v, -vert-001v |
| Aspect ratio img row | 4 | flex-aspect-ratio-img-row-004/006/007/015 |
| Transferred sizes + padding | 2 | flex-item-transferred-sizes-padding-border/content-sizing |

### Root Cause (from code analysis)

**Bug 1 — Consistent 385-pixel diff at max-diff 111 across all basic-* tests:**
All non-v basic tests (canvas, img, video, fieldset, iframe) share the identical diff pattern: exactly 385 pixels at max-diff 111 (not 255). This indicates a sub-pixel positioning difference in the dotted border rendering — the flex container's cross-axis alignment places the first item at a fractionally different y-coordinate than the reference's inline-flow positioning. The reference uses `line-height: 8px` on the container, which affects inline replaced element positioning but should NOT affect flex item positioning (flex items are blockified). The issue is in `replaced_layout.go`'s `ComputeReplacedSize` where the interaction between `line-height` and flex cross-axis sizing may produce different results.

**Bug 2 — Writing-mode variant items (v-tests) get wrong cross-size:**
For `flexbox-basic-block-horiz-001v.xhtml`, items have `writing-mode: vertical-lr`, making them orthogonal flex items. The item's inline-size (physical width) is the flex cross-dimension, but `buildItemConstraintSpace` at line ~2322 doesn't set `IsFixedInlineSize` for the cross axis when the item is orthogonal. This causes the item to size to its intrinsic inline-size (e.g., 300px for canvas) instead of the flex-resolved cross-size.

**Bug 3 — Aspect ratio image min-width with `min-height` (flex-aspect-ratio-img-row-004/006/007):**
These tests set `min-height` on an image in a row flex container. The transferred size suggestion should transfer `min-height` through the aspect ratio to compute a minimum width. The current code at lines 3119-3137 of `flex_layout.go` checks `min-cross-size` as a fallback for `crossContentSize`, but only when no explicit cross-size exists. For images with BOTH a `min-height` and the container's definite height, the `min-height` constraint should still participate in the transferred size calculation.

**Bug 4 — Transferred sizes with `box-sizing` (flex-item-transferred-sizes-padding-*):**
When computing the transferred size suggestion for an item with `box-sizing: border-box`, the padding/border must be subtracted before applying the aspect ratio, then re-added. The current code at lines 3153-3167 converts `crossContentSize` directly through the ratio without adjusting for `box-sizing`.

### What Blink Does
In Blink:
- `ComputeReplacedSize()` uses the flex-resolved sizes via `IsFixedInlineSize`/`IsFixedBlockSize` flags on the constraint space — it doesn't re-derive sizes from CSS properties
- For orthogonal items, `BuildSpaceForFlexItem()` sets `is_fixed_inline_size` for the cross axis when the item's inline axis is the container's cross axis
- `ComputeAutomaticMinSize()` uses `ComputeMinAndMaxContentContribution()` which accounts for `box-sizing` when computing transferred sizes

### Fix Location

**File: `pkg/layout/replaced_layout.go`**

1. Verify that `ComputeReplacedSize` correctly uses `IsFixedInlineSize`/`IsFixedBlockSize` from the constraint space for flex items. The existing code at lines 42-57 checks these flags. Trace through a basic-canvas test to verify the constraint space has these flags set correctly.

**File: `pkg/layout/intrinsic_sizing.go`**

2. Verify that `GetIntrinsicSizingInfo` returns correct natural dimensions and aspect ratios for all replaced element types (canvas, video, img, iframe). Ensure the aspect ratio is always set when natural width and height are both available.

**File: `pkg/layout/flex_layout.go` (lines 2279-2372, `buildItemConstraintSpace`)**

3. For orthogonal flex items in row flex, the cross axis is the item's inline axis. When the cross-size is definite (from stretching or explicit size), set `IsFixedInlineSize(true)` on the item's constraint space:
   ```go
   if mainIsItemInline {
       // Normal: cross = block. Already handled.
   } else {
       // Orthogonal: cross = item's inline axis. Fix the inline size.
       if crossIsFixed {
           csb.SetIsFixedInlineSize(true)
       }
   }
   ```

4. **Lines 3153-3167**: Account for `box-sizing: border-box` in the transferred size suggestion by subtracting border/padding from `crossContentSize` before the ratio calculation, then adding main-axis border/padding back.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-canvas" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-img" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-block" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio-img-row" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-transferred" -count=1
```

Check for regressions:
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -count=1
```

---

## Independence Check

| File | Target 1 | Target 2 | Target 3 | Target 4 | Target 5 |
|------|----------|----------|----------|----------|----------|
| `flex_layout.go` (395-548) | **Yes** | - | - | - | - |
| `flex_layout.go` (846-984) | **Yes** | **Yes** | - | - | - |
| `flex_layout.go` (1010-1027) | - | **Yes** | - | - | - |
| `flex_layout.go` (1113-1192) | **Yes** | - | - | - | - |
| `flex_layout.go` (1464-1600) | - | - | - | **Yes** | - |
| `flex_layout.go` (2279-2372) | - | **Yes** | - | - | **Yes** |
| `flex_layout.go` (2944-3276) | - | - | **Yes** | - | **Yes** |
| `min_max_sizing.go` | - | - | - | **Yes** | - |
| `replaced_layout.go` | - | - | - | - | **Yes** |
| `intrinsic_sizing.go` | - | - | - | - | **Yes** |
| `fragment_geometry.go` | - | - | **Yes** | - | - |

**Conflict analysis:**
- **Targets 1 & 2 CONFLICT** at lines 846-984 (alignment switch). Run Target 2 AFTER Target 1 merges.
- **Targets 3 & 5 share** `flex_layout.go` but in non-overlapping line ranges (2944-3276 vs 2279-2372 and 3119-3167). The Target 5 changes at lines 3119-3167 are WITHIN Target 3's range. **Resolution:** Target 5's aspect-ratio fix (lines 3119-3167) should be handled by Target 3 instead. Target 5 should focus on `replaced_layout.go`, `intrinsic_sizing.go`, and the orthogonal `buildItemConstraintSpace` fix (lines 2279-2372).
- **All other pairs** are non-overlapping and safe for parallel execution.

**Recommended execution order:**
1. **Parallel batch (4 agents):** Targets 1, 3, 4, 5
2. **After merge:** Target 2

## Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Key Blink files:
  - `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`
  - `third_party/blink/renderer/core/layout/flex/layout_flexible_box.cc`
  - Search at `https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/flex/`
- **Commit and report at each milestone** (don't batch everything to the end).
- **Run only target-specific tests** during development. Do NOT run the full flex suite until all targets are done.
- **NEVER commit outside your worktree.** If your directory is NOT `~/louis14`, you are in a worktree — commit only to your worktree branch. Never commit to fix/*, master, or any non-worktree branch.
- **Never use `open`** to display files.
