# Flexbox Round 5: Top 5 Improvements

Current state: 503 pass / 126 fail (80% pass rate) across 629 tests.

These five targets are ranked by foundational impact. **Targets 1, 2, and 5 all modify `flex_layout.go` and CANNOT be parallelized with each other.** Pick ONE of {1, 2, 5} to run in parallel with Targets 3 and 4, which are fully independent.

Recommended parallel group: **Target 1 + Target 3 + Target 4**

---

## Target 1: Baseline Alignment and Synthesis (~20 tests)

### Problem
Flex container baseline computation has multiple bugs: last baseline is set identically to first baseline, last baseline synthesis uses wrong edge (block-start instead of block-end), vertical writing mode baselines are completely disabled with no fallback, and container baseline export doesn't use the correct item from the first flex line.

### Affected Tests (~20 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| align-self: baseline (horizontal) | 6 | flexbox-align-self-baseline-horiz-001a/001b/003/006/007/008 |
| Container baseline export | 4 | flexbox-baseline-align-self-baseline-horiz-001, flexbox-baseline-multi-item-horiz-001a/001b, flexbox-baseline-multi-line-horiz-002 |
| Multi-line baseline (vertical) | 2 | flexbox-baseline-multi-line-vert-001/002 |
| Baseline synthesis | 2 | baseline-synthesis-002, baseline-synthesis-vert-lr-line-under |
| Collapsed item baseline | 1 | flexbox-collapsed-item-baseline-001 |
| Fieldset baseline | 1 | fieldset-baseline-alignment |
| align-self horiz (table) | 1 | flexbox-align-self-horiz-001-table |
| align-self horiz misc | 2 | flexbox-align-self-horiz-002, align-self-016 |
| align-content wrap | 1 | flexbox-align-self-stretch-vert-002 |

Total pixel diff: ~130,000+

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`**

1. **Lines 1107-1150 (container baseline export):** `SetLastBaseline` is set to the same value as `SetBaseline` (line 1148). Per CSS Flexbox §4.2, the container's last baseline should come from the **last non-collapsed flex item** in the **last flex line**, not the first.

2. **Lines 864-871 (last baseline synthesis):** When synthesizing a last baseline for items without one (`!hasBaseline && lb <= 0`), the code synthesizes at "block-start" but CSS Flexbox §9.4 requires synthesized last baselines at the **block-end margin edge**.

3. **Lines 838-840 (vertical writing mode guard):**
   ```go
   canSynthesize := isRow && !wdm.IsVertical()
   ```
   This completely disables baseline synthesis for vertical writing modes with no fallback. Items in vertical writing modes that participate in baseline alignment get no baseline at all.

4. **Lines 493-542 (baseline line sizing):** The shared baseline computation doesn't properly track which items participate in first vs. last baseline groups within a flex line.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- Container baseline is derived from the first item in the first line that participates in baseline alignment (`CalculateBaseline()`)
- Last baseline comes from the last item in the last line
- Synthesized baselines use `SynthesizedBaselineFromBorderBox()` which returns block-end for last baseline
- Vertical writing modes use the central baseline (computed from text metrics and text-orientation)
- See: `FlexLayoutAlgorithm::CalculateBaseline()` and `NGBoxFragment::BaselineForHorizontalWritingMode()`

### Fix Location

**`pkg/layout/flex_layout.go`:**
- Lines 1107-1150: Rewrite container baseline export to track first-in-first-line and last-in-last-line items separately
- Lines 864-871: Fix synthesis direction — last baseline should synthesize at block-end margin edge (item cross offset + item cross size including margins)
- Lines 838-840: Add central baseline fallback for vertical writing modes, or at minimum synthesize at the alphabetic baseline position
- Lines 493-542: Track first/last baseline groups per line correctly

**`pkg/layout/block_layout.go`:**
- Lines 576-595: Verify baseline export propagation is correct for flex item children (should be fine, but verify)

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/baseline-synthesis" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/fieldset-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-collapsed-item-baseline" -count=1
# Regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS"
```

---

## Target 2: Vertical/Column Flex Direction and Writing Modes (~25 tests)

### Problem
Column-direction flex containers have multiple issues: cross-axis stretching is only applied for single-line (`nowrap`) containers but not for wrapping column flex, baseline synthesis is completely disabled for vertical writing modes, and wrapping column flex items aren't properly constrained to their flex line's cross-size.

### Affected Tests (~25 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Column align-self (vert) | 4 | flexbox-align-self-vert-001/002/003/004 |
| Column align-self RTL | 5 | flexbox-align-self-vert-rtl-001/002/003/004/005 |
| Column replaced elements | 6 | flexbox-basic-*-vert-001 (canvas, fieldset, iframe, img, textarea, video) |
| Vertical block/canvas variants | 2 | flexbox-basic-block-vert-001v, flexbox-basic-canvas-vert-001v |
| Column wrapping | 2 | flexbox-flex-wrap-vert-001/002 |
| Writing modes | 3 | flexbox-writing-mode-013/014/015 |
| Column break requests | 4 | flexbox-break-request-vert-001a/001b/002a/002b |
| Column sizing | 1 | flexbox-sizing-vert-001 |
| Column aspect ratio | 4 | flex-aspect-ratio-img-column-008/010/012/018 |
| flex-direction modify | 1 | flex-direction-modify |

Total pixel diff: ~250,000+

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`**

1. **Lines 402-404 (cross-axis stretch in column flex):**
   ```go
   crossIsFixed := !isRow && isStretch && wrapMode == "nowrap"
   ```
   This only applies fixed cross-size for **single-line** column flex. In **wrapping** column flex (`flex-wrap: wrap`), stretch items are NOT given a fixed cross-size in the first pass, causing items to not fill the flex line's cross extent.

2. **Lines 838-840 (baseline synthesis guard):**
   ```go
   canSynthesize := isRow && !wdm.IsVertical()
   ```
   For column flex (`!isRow`), `canSynthesize` is always false. Items that need synthesized baselines for alignment get nothing. The writing-mode-013/014/015 tests specifically test mixed writing modes in flex containers where baseline synthesis is essential.

3. **Lines 1018-1021 (physical positioning):**
   For column flex, `inlineOff = item.crossOffset + item.crossMarginStart()` and `blockOff = item.mainOffset`. If crossOffset doesn't account for the line's accumulated cross position in wrapped mode, items end up overlapping or misaligned.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- Column flex wrapping properly tracks each flex line's cross-size and applies it to stretch items via `ResolveFlexItemCrossSize()`
- For each wrapped line, `line.cross_size` is the resolved cross-size of that specific line
- Stretch items in wrapped column flex receive `line.cross_size - margins - border_padding` as their fixed inline-size
- Writing mode handling uses `IsParallelWritingMode()` to determine when block/inline axes align between container and item
- See: `FlexLayoutAlgorithm::LayoutFlexItems()`, `FlexLine::ComputeLineItemsPosition()`

### Fix Location

**`pkg/layout/flex_layout.go`:**
- Lines 402-404: Remove the `wrapMode == "nowrap"` guard, or implement a two-pass approach where stretch items in wrapped column flex get the line's resolved cross-size in the second pass
- Lines 838-840: Enable baseline synthesis for column flex (at minimum for the non-vertical-writing-mode case where `isRow` is false but the writing mode is horizontal)
- Lines 970-990: Verify that cross-axis alignment offsets account for the line's accumulated cross position in wrapped column flex
- Lines 1018-1021: Verify physical offset computation for column flex with wrapping

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic.*vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-wrap-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-writing-mode" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio-img-column" -count=1
# Regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS"
```

---

## Target 3: Replaced Element Sizing in Flex (~15 tests)

### Problem
Replaced elements (canvas, img, iframe, textarea, video, fieldset) as flex items have incorrect sizing. The `contain: size` path doesn't preserve aspect ratio from HTML attributes, and there are border/padding interaction issues when flex sets `IsFixedInlineSize` on replaced elements.

### Affected Tests (~15 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Horizontal replaced elements | 5 | flexbox-basic-canvas/fieldset/iframe/img/video-horiz-001 |
| Horizontal variants | 2 | flexbox-basic-block-horiz-001v, flexbox-basic-canvas-horiz-001v |
| Textarea elements | 2 | flexbox-basic-textarea-horiz-001, flexbox-basic-textarea-vert-001 |
| Canvas contain:size | 1 | canvas-contain-size |
| Scrollbar content height | 1 | content-height-with-scrollbars |
| Aspect ratio intrinsic | 3 | aspect-ratio-intrinsic-size-001/002/005 |
| Transferred sizes padding | 2 | flex-item-transferred-sizes-padding-border/content-sizing |

Total pixel diff: ~50,000+

### Root Cause (from code analysis)

**File: `pkg/layout/replaced_layout.go`**

1. **Lines 174-182 (contain:size path):** When `contain: size` is set, the code resolves explicit inline/block sizes but **skips `ComputeReplacedSize()` entirely** (line 184). This means the aspect ratio from HTML `width`/`height` attributes is NOT applied during size resolution. For `canvas-contain-size.html`, the canvas has `width=20 height=20` (aspect ratio 1:1) and CSS `width: 100px`, but containment bypasses the aspect ratio calculation.

2. **Lines 43-49 (flex constraint interaction):** When `IsFixedInlineSize` is true (flex sets this for stretched items):
   ```go
   explicitInline = space.AvailableSize.InlineSize - geom.InlineBorderPadding()
   ```
   The border-padding subtraction assumes the flex algorithm passes border-box sizes, but if there's a mismatch in box-sizing interpretation between flex and replaced layout, the sizing will be off by the border+padding amount.

3. **Lines 198-220 (ComputeReplacedSize aspect ratio clamping):** The CSS 2.1 §10.3.2 constraint resolution algorithm may not correctly handle the case where flex provides both a fixed inline-size AND the element has an intrinsic aspect ratio — the cross-axis size should be derived from the aspect ratio, not from the intrinsic height.

**File: `pkg/layout/intrinsic_sizing.go`**

4. **Canvas intrinsic sizing:** The `getCanvasIntrinsicInfo()` function returns default 300x150 when no HTML attributes are set, but may not correctly handle the case where only one dimension is specified via HTML attributes.

### What Blink Does

In Blink's `ReplacedLayoutAlgorithm::Layout()`:
- Replaced elements always go through `ComputeReplacedSize()` regardless of containment
- For `contain: size`, the intrinsic size is treated as 0x0 but the aspect ratio from HTML attributes is preserved
- The constraint space from flex includes exact content-box dimensions (after subtracting border+padding in `FlexLayoutAlgorithm::BuildSpaceForFlexItem()`)
- See: `ReplacedLayoutAlgorithm::ComputeReplacedSize()` and `LayoutReplaced::IntrinsicSizingInfo()`

### Fix Location

**`pkg/layout/replaced_layout.go`:**
- Lines 174-182: Don't skip `ComputeReplacedSize()` for `contain: size`. Instead, pass intrinsic sizes as 0x0 but preserve the aspect ratio
- Lines 43-49: Verify box-sizing interpretation matches what flex passes
- Lines 198-220: Ensure aspect ratio clamping works when flex provides fixed inline-size

**`pkg/layout/intrinsic_sizing.go`:**
- Review `getCanvasIntrinsicInfo()` for correctness with partial HTML attribute specification

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/canvas-contain-size" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/content-height-with-scrollbars" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio-intrinsic-size" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-transferred-sizes" -count=1
# Regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS"
```

---

## Target 4: Flex Container Intrinsic Sizing (~10 tests)

### Problem
The flex container's own intrinsic size computation (`measureFlexMinMax()`) incorrectly handles items with content-based width keywords (min-content, max-content, fit-content) as their explicit width, and the contribution logic for items with `flex-grow: 0` doesn't properly resolve flex-basis when the item's main-size is itself content-dependent.

### Affected Tests (~10 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Container min/max-content | 2 | flex-container-max-content-001, flex-container-min-content-001 |
| Fit-content items | 1 | fit-content-item-001 |
| Item min-width keywords | 2 | flex-item-content-is-min-width-max-content, flex-item-min-width-min-content |
| Item max-height min-content | 1 | flex-item-max-height-min-content |
| Dynamic sizing | 2 | dynamic-isize-change-001, dynamic-isize-change-004 |
| Dyn resize | 1 | flexbox-dyn-resize-001 |
| Column gap percentage | 1 | flexbox-column-row-gap-004 |

Total pixel diff: ~85,000+

### Root Cause (from code analysis)

**File: `pkg/layout/min_max_sizing.go`**

1. **Lines 323-328 (flex-grow=0 contribution):** When `flex-grow == 0` and `basisIsContent == true`, the item contributes its full `contentMax` for max-content and `contentMin` for min-content. However, if the item has an explicit `width: min-content` or `width: max-content`, the `resolveFlexBasisForIntrinsic()` function at line 316 returns `basisIsContent = true` because the width resolves to a content-dependent value — but then the contribution should use that resolved keyword value, not the raw content intrinsic size of the children.

2. **Lines 361-372 (flex-shrink contribution):** Similar issue — when `flex-shrink > 0` and the base size is content-dependent, the shrink contribution doesn't account for content-based width keywords on the item itself.

3. **Lines 244-249 (available size for intrinsic computation):** When computing content min/max sizes for items, the available inline size passed to the item's own `ComputeMinMaxSizes()` is the container's available size, which may be 0 or indefinite. This is correct for max-content (use unconstrained) but may be wrong for min-content (should use 0).

4. **Line 433 (`resolveFlexBasisForIntrinsic`):** Returns `0, true` when flex-basis is auto and there's no explicit main-size. This doesn't distinguish between "no explicit size" and "explicit size is a content keyword."

### What Blink Does

In Blink's `FlexLayoutAlgorithm::ComputeMinMaxSizes()`:
- Each item's contribution is computed via `FlexItem::MinMaxContentContribution()`
- For items with `flex-basis: auto`, resolves to the item's `main_axis_border_padding + main_axis_scrollbar_inline_size + main_content_size`
- Content-based main-size keywords (min-content, max-content) are resolved by calling the item's own `ComputeMinMaxSizes()` and selecting the appropriate value
- The flex-grow/shrink weighting follows CSS Flexbox §9.9.1 exactly
- See: `FlexLayoutAlgorithm::ComputeMinMaxSizes()`, `FlexItem::MinMaxContentContribution()`

### Fix Location

**`pkg/layout/min_max_sizing.go`:**
- Lines 316-340: After `resolveFlexBasisForIntrinsic()`, check if the item's explicit main-size is a content keyword (min-content, max-content, fit-content). If so, resolve it to the actual content min/max value rather than treating it as fully content-dependent.
- Lines 361-372: Apply the same fix for shrink contributions.
- Line 433: Distinguish between "no explicit size" and "explicit content keyword" in the return value.
- Lines 244-249: Ensure min-content computation passes the correct available size (0 for min-content, unconstrained for max-content).

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-container" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/fit-content-item" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-content-is" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-min-width-min" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-max-height" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/dynamic-isize" -count=1
# Regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS"
```

---

## Target 5: Min-Height/Width Auto and Minimum Size (~12 tests)

### Problem
The automatic minimum size algorithm (CSS Flexbox §4.5) has edge cases around percentage-based children not contributing to min-content, wrapping interactions with min-height auto, and aspect-ratio transferred suggestion computation when the cross-size comes from stretching rather than explicit declaration.

### Affected Tests (~12 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Min-height auto | 4 | flexbox-min-height-auto-001/002c/003/004 |
| Min-width auto | 2 | flexbox-min-width-auto-005/006 |
| Minimum height items | 3 | flex-minimum-height-flex-items-019/023/030 |
| Minimum width items | 1 | flex-minimum-width-flex-items-016 |
| Definite sizes | 2 | flexbox-definite-sizes-003/004 |

Total pixel diff: ~55,000+

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`**

1. **Lines 2881-2905 (content suggestion for block-axis items):** When computing content suggestion (min-content in main axis), the code uses `IsContentSuggestionLayout = true` in the constraint space. This suppresses CSS block-size to get intrinsic content height. However, for items whose children use percentage heights, the percentage resolves against the item's height — but during content suggestion layout, the item has no definite height, so percentage children resolve to 0. This means the content suggestion underestimates the actual minimum.

2. **Lines 2931-3063 (transferred suggestion with stretching):** The transferred suggestion uses the cross-size to derive a main-size via aspect ratio. For stretched items, it uses `containerCrossSize`, but doesn't subtract the item's cross-axis border+padding before applying the aspect ratio. This inflates the transferred suggestion.

3. **Lines 3065-3089 (suggestion combination):** For non-replaced elements, the combination is `max(content, transferred)` capped by specified. This is per spec, but if the content suggestion is wrong (issue 1) or transferred suggestion is wrong (issue 2), the final minimum is wrong.

4. **Lines 2984-2992 (cross-size for transferred suggestion):** When the item is stretched and the container has a definite cross-size, the code uses:
   ```go
   crossForTransfer = containerCrossSize - item.crossMarginStart() - item.crossMarginEnd()
   ```
   This doesn't subtract border+padding (only margins), but the transferred suggestion should use the content-box cross-size.

### What Blink Does

In Blink's `FlexLayoutAlgorithm::ComputeMinMaxMainSize()`:
- Content suggestion: uses `ComputeMinAndMaxContentContribution()` with `SizeType::kContent`
- The constraint space for content suggestion explicitly sets percentage resolution size to indefinite
- Transferred suggestion: uses `ComputeTransferredMinSize()` which computes content-box cross-size (subtracting all border+padding+scrollbar) before applying aspect ratio
- See: `FlexLayoutAlgorithm::ComputeMinMaxMainSize()`, `FlexItem::ComputeTransferredMinSize()`

### Fix Location

**`pkg/layout/flex_layout.go`:**
- Lines 2881-2905: Ensure percentage resolution is properly set to indefinite in the content suggestion constraint space
- Lines 2984-2992: Subtract border+padding (not just margins) from cross-size before applying aspect ratio for transferred suggestion
- Lines 2931-3063: Review transferred suggestion for edge cases with explicit cross-size vs. stretched cross-size
- Lines 3065-3089: Verify combination logic handles the fixed transferred suggestion

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-height-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-width-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum-height" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum-width" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-definite-sizes" -count=1
# Regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS"
```

---

## Independence Check

| | flex_layout.go | block_layout.go | replaced_layout.go | intrinsic_sizing.go | min_max_sizing.go |
|---|---|---|---|---|---|
| Target 1 (Baseline) | **Yes** (lines 493-542, 830-878, 942-972, 1107-1150) | Yes (lines 576-595) | - | - | - |
| Target 2 (Vert/Column) | **Yes** (lines 402-404, 838-840, 1018-1021) | - | - | - | - |
| Target 3 (Replaced) | - | - | **Yes** | **Yes** | - |
| Target 4 (Intrinsic) | - | - | - | - | **Yes** |
| Target 5 (Min Auto) | **Yes** (lines 2820-3117) | - | - | - | - |

**Targets 1, 2, and 5 overlap on `flex_layout.go`** and cannot run in parallel with each other.

**Valid parallel groups of 3:**
- **Option A (recommended):** Target 1 + Target 3 + Target 4
- **Option B:** Target 2 + Target 3 + Target 4
- **Option C:** Target 5 + Target 3 + Target 4

Target 1 is recommended as the flex_layout.go representative because baseline alignment is the most foundational fix — it affects how flex containers participate in their parent's layout.

## IMPORTANT: Agent Guidelines

- **Foundational correctness over quick wins.** Every fix must work for ALL cases. Don't chase near-passing tests or easy wins. If a fix doesn't generalize, it's the wrong fix.
- **Study Blink's approach** before writing code in any new area. Key source: `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`
- **Commit and report at each milestone** (don't batch everything to the end).
- Run the full flexbox suite (`TestWPTCSS3Reftests/css-flexbox`) before and after changes to catch regressions.
- Current passing count is **503**. Any change that reduces this number is a regression and must be investigated.
- When modifying baseline code, also run writing-modes tests: `TestWPTCSS3Reftests/css-writing-modes`
- When modifying replaced element code, also run CSS2 tests: `TestWPTReftests` and `TestListReftestResults`
