# Flexbox Round 2: Top 5 Improvements

Current state: 498 pass / 132 fail (79.0% pass rate) across 630 tests.
Test command: `cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1`

Previous round: Round 1 fixed baseline/collapsed items (Target 1), partial intrinsic sizing (Target 2 — incomplete), and inline-block baseline + negative half-leading (Target 3).

These five targets are organized by root cause. Targets 1, 2, and 3 touch **different primary files** and can run in parallel. Targets 4 and 5 also touch `flex_layout.go` (like Target 3) so they must be sequenced after Target 3 or combined with it.

---

## Target 1: XHTML Reference Inline Layout for Replaced Elements (~20 tests)

### Problem
Many failing XHTML tests compare flex layout output against reference files that use `display: inline-block` divs or inline replaced elements (canvas, img) inside normal-flow containers. The reference files render incorrectly in our engine, causing visual mismatches even when the flex side is correct.

Round 1 Target 3 fixed empty inline-block baselines and negative half-leading, but the remaining failures involve **non-empty inline-block divs** and **inline replaced elements** (canvas, img, video) interacting with line-height, vertical-align, and writing modes.

### Affected Tests (~20 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-basic-canvas-horiz/vert | 4 | 001, 001v (horizontal + vertical) |
| flexbox-basic-img-horiz/vert | 2 | horiz-001, vert-001 |
| flexbox-basic-video-horiz/vert | 2 | horiz-001, vert-001 |
| flexbox-basic-textarea-horiz/vert | 2 | horiz-001, vert-001 |
| flexbox-basic-iframe-horiz/vert | 2 | horiz-001, vert-001 |
| flexbox-basic-fieldset-horiz/vert | 2 | horiz-001, vert-001 |
| flexbox-basic-block-horiz/vert | 2 | 001v (writing-mode variants) |
| flexbox-whitespace-handling | 2 | 001a, 001b |
| flexbox-justify-content-horiz | ~2 | 001b (line-height:0 ref pattern) |

### Root Cause (from code analysis)

**File: `pkg/layout/inline_layout.go`**

The `computeLineMetricsEx` function (lines 985-1080) handles atomic inlines in line box computation. There are two issues:

**Issue 1 — Lines 1008-1011**: The `isInlineBlockLike` check requires `display == DisplayInlineBlock || DisplayInlineFlex || DisplayTable || DisplayInlineTable`. Inline replaced elements like `<canvas>`, `<img>`, `<video>` have display:inline (not inline-block), so they fall through to the generic `else` branch at line 1061 which simply sets `maxAscent = blockSize`. This is correct for bottom-baseline alignment but doesn't account for `vertical-align` values or margin contributions.

**Issue 2 — Writing-mode variants**: The "001v" tests (flexbox-basic-block-horiz-001v, canvas-horiz-001v) use `writing-mode: vertical-lr` on flex items. The reference files use inline-block divs with the same writing modes. Inline-block baseline computation in vertical writing modes needs to use the central baseline (line 1050-1059), but the condition `centralBaseline` may not be correctly derived for inline-block children that have their own writing mode different from the containing block.

**Issue 3 — Line-height interaction**: The reference file flexbox-basic-canvas-horiz-001-ref.xhtml uses `line-height: 8px` with 22px-tall inline canvas elements. The strut contributes 8px line-height but the replaced element should dominate. If the strut's half-leading computation doesn't properly reduce the strut contribution, the line box is too tall.

### What Blink Does

In Blink's `inline_box_state.cc`:
- `InlineBoxState::ComputeTextMetrics()` applies half-leading (including negative) to the strut's ascent/descent
- `InlineBoxState::EnsureTextMetrics()` handles the interaction between strut and inline replaced elements
- `NGInlineLayoutAlgorithm::PlaceAtomicInline()` computes the replaced element's contribution to the line box, accounting for vertical-align and margins
- For vertical writing modes, `InlineBoxState::AccumulateUsedFonts()` switches to central baseline alignment

Key: Blink explicitly tracks whether each inline item uses alphabetic or central baseline, and computes line box contributions accordingly. Our code has a single `centralBaseline` bool for the entire line box computation, which doesn't handle mixed writing modes within a single line.

### Fix Location

**File: `pkg/layout/inline_layout.go`**

1. **Lines 1008-1066**: Rework the atomic inline handling to properly handle inline replaced elements (not just inline-block). Inline replaced elements should be treated as atomic inlines for line box purposes regardless of their computed display value.

2. **Lines 985-999**: The vertical-align:top/bottom handling for atomic inlines should correctly place them after computing the baseline-based line height, not just track their max height.

3. **Central baseline per-item**: Instead of a single `centralBaseline` for the whole line, determine per-item whether it uses central or alphabetic baseline based on the item's writing mode vs the line's writing mode.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-canvas" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-img" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-block" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-whitespace" -count=1
# CRITICAL: Inline changes affect ALL tests. Run broad regression:
cd pkg/visualtest && go test -v -run "TestWPTReftests" -count=1 2>&1 | grep -c "REFTEST PASS"
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -c "REFTEST PASS"
# Full flex regression:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 498
```

---

## Target 2: Content-Based Flex Sizing — Round 1 Target 2 Continuation (~11 tests)

### Problem
This is the unfinished work from Round 1 Target 2. The agent committed a partial fix (d912e86d) that added flex container routing in `computeContentMinMaxSizes` and improved child measurement in `measureFlexMinMax`, but hit its rate limit before completing the work. The key remaining issue is that `flex-basis: content` only works for **row flex** — column flex is missing.

### Affected Tests (~11 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-flex-basis-content (row) | ~4 | 001a, 001b, 003a, 003b |
| flexbox-flex-basis-content (column) | ~4 | 002a, 002b, 004a, 004b |
| flex-container-min/max-content | 2 | min-content-001, max-content-001 |
| flex-item-content-is-min-width-max-content | 1 | — |

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`, line 1414**

The `flex-basis: content` path only handles row flex:
```go
if basisVal == "content" && isRow {
    return fla.itemContentMaxMainSize(child, style, childWDM, parentWDM,
        contentInlineSize)
}
return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
    contentInlineSize, isRow)
```

The `&& isRow` condition causes column flex to fall through to `itemMaxContentMainSize`, which uses `ComputeMinMaxSizes` that respects the item's explicit CSS main-size. Per spec, `flex-basis: content` MUST ignore the item's CSS main-size and use content-based sizing.

**File: `pkg/layout/min_max_sizing.go`**

The `measureFlexMinMax` function (lines 162+) may need additional improvements for how it measures child items' contributions when the container itself is being intrinsically sized. The partial fix added flex routing in `computeContentMinMaxSizes` and improved child measurement, but the algorithm for column flex intrinsic sizing (cross-axis = inline) needs verification.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- `NGFlexLayoutAlgorithm::ComputeNextFlexLine()` handles `flex-basis: content` by calling `ComputeMinAndMaxContentContributionForSelf()` which uses content-based sizing regardless of flex direction
- The content measurement path ignores the item's explicit inline-size or block-size depending on which is the main axis
- For column flex, the main axis is block, so content sizing measures the item's natural block-size (height) from layout, ignoring any explicit CSS height

In Blink's `min_max_sizes_utils.cc`:
- `ComputeMinAndMaxContentContribution()` handles flex containers by calling into `NGFlexLayoutAlgorithm::ComputeMinMaxSizes()`
- The flex min/max algorithm sums child contributions along the main axis (row) or takes the max (column)

### Fix Location

**File: `pkg/layout/flex_layout.go`, line 1414**

Remove the `&& isRow` restriction:
```go
if basisVal == "content" {
    return fla.itemContentMaxMainSize(child, style, childWDM, parentWDM,
        contentInlineSize)
}
```

Then verify `itemContentMaxMainSize` works correctly for column flex (main = block axis). If it currently only computes inline-direction content sizes, it needs a block-direction path that lays out the item with indefinite block-size and returns the resulting block-size.

**File: `pkg/layout/min_max_sizing.go`**

Review `measureFlexMinMax` (line 162+) for correctness:
1. Column flex min/max-content should take the max of items' cross (inline) contributions
2. Row flex should sum items' main (inline) contributions  
3. Child measurement should use `computeContentMinMaxSizes` (not `ComputeMinMaxSizes`) when computing content contributions

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-basis-content" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-container-m" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-content" -count=1
# Full regression check:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 498
```

---

## Target 3: Aspect Ratio & Transferred Sizes in Flex (~14 tests)

### Problem
Flex items with aspect-ratio (both CSS `aspect-ratio` property and intrinsic ratios from replaced elements like `<img>`) compute incorrect sizes when border-box sizing, padding, or min/max constraints interact with the transferred size suggestion.

### Affected Tests (~14 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flex-aspect-ratio-img-row | 5 | 004, 006, 007, 015, 017 |
| flex-aspect-ratio-img-column | 4 | 008, 010, 012, 018 |
| aspect-ratio-intrinsic-size | 4 | 001, 002, 005, 007 |
| flex-item-transferred-sizes-padding | 2 | border-sizing, content-sizing |

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`, lines 2630-2760**

The transferred size suggestion computation in `flexItemMinMain` has several issues:

**Issue 1 — Box-sizing interaction (lines 2661-2667)**: When computing the cross-content-size for the transferred suggestion, the code always subtracts `childGeom.BlockBorderPadding()` from the container cross-size. But for items with `box-sizing: border-box`, the explicit cross-size CSS property is already a border-box value. The subtraction should only happen when converting to content-box for the aspect ratio computation:

```go
// Current (always subtracts border/padding):
crossContentSize = containerCrossSize - childGeom.BlockBorderPadding() - crossMargins

// Correct: for explicit cross-size with border-box, the CSS value IS border-box,
// so subtract border/padding to get content-box for ratio transfer.
// For stretched items, containerCrossSize is the border-box, same treatment.
```

The issue is more subtle: when the cross-size comes from an **explicit CSS property** (lines 2649-2656), it's already resolved via `ResolveBlockSize`/`ResolveInlineSize` which returns content-box. So the code should NOT further subtract border/padding in that case. But when the cross-size comes from stretching (lines 2661-2667), `containerCrossSize` is the container's content-box cross-size, and the item needs its own border/padding subtracted to get its content-box cross-size.

**Issue 2 — Lines 2691-2727**: The CSS `aspect-ratio` property path duplicates the same box-sizing confusion. Both the replaced-element path and the CSS-aspect-ratio path need consistent treatment.

**Issue 3 — Min/max constraint interaction**: After computing the transferred suggestion, the min/max clamping (lines 2756-2780) should account for the aspect ratio preserving both dimensions. Currently it clamps the main axis independently.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- `ComputeTransferredMinMaxInlineSizes()` computes min/max sizes transferred through aspect ratio
- It explicitly converts between border-box and content-box using `ComputeMinSizeAutoForReplacedItem()` 
- The key insight: Blink always works in content-box for ratio math, converting at the boundaries
- `ComputeMinAndMaxContentContributionForSelf()` handles the transferred size contribution, accounting for padding/border before and after ratio application

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Lines 2647-2657**: When cross-size comes from explicit CSS, `ResolveBlockSize`/`ResolveInlineSize` already returns content-box values. Do NOT further subtract border/padding here.

2. **Lines 2661-2667**: When cross-size comes from stretching, correctly subtract the ITEM's border/padding (not the container's). Current code does this correctly.

3. **Lines 2694-2718**: Same fixes for the CSS `aspect-ratio` path.

4. **Lines 2738-2754**: After computing `transferredSuggestion` (which is a content-box main-axis size), add back border/padding before comparing with `specifiedSuggestion` if the specified suggestion is in border-box terms.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio-intrinsic" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-transferred" -count=1
# Full regression:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 498
```

---

## Target 4: Automatic Minimum Size (min-width:auto / min-height:auto) (~9 tests)

### Problem
The automatic minimum size algorithm (`flexItemMinMain` in `flex_layout.go`) has issues with percentage resolution for stretched items and with transferred size suggestions when the item's cross-size comes from stretching.

### Affected Tests (~9 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flex-minimum-height-flex-items | 3 | 019, 023, 030 |
| flex-minimum-width-flex-items | 1 | 016 |
| flexbox-min-height-auto | 2 | 001, 002c |
| flexbox-min-width-auto | 3 | 002b, 005, 006 |

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`, lines 2520-2780**

**Issue 1 — Percentage resolution for stretched items (lines 2573-2600)**: When computing the content size suggestion for block-axis min-auto, the constraint space uses `containerInlineSize` for percentage resolution. But per CSS Flexbox §9.8.3, a stretched flex item's cross-size becomes definite and should be the percentage resolution basis for its descendants. Currently, items with `height: 100%` children resolve against the container, not the item's stretched size.

```go
// Current (line 2583-2588):
containerInlineSize := space.AvailableSize.InlineSize
colMinSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
    SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
    // ...
```

The issue: for column flex, the content suggestion layout doesn't know the item's stretched cross-size (which is inline in column flex). So percentage-width children of the flex item resolve incorrectly.

**Issue 2 — Transferred suggestion with stretched cross (lines 2661-2667)**: The transferred size suggestion uses `containerCrossSize` when the item would be stretched. But the item's actual stretched cross-size may differ from the container cross after accounting for margins, borders, and padding. This is the same issue as Target 3 but specifically in the min-auto context.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- `ComputeAutomaticMinimumSize()` explicitly passes the item's resolved cross-size (after stretching) into the content-size computation
- `ChildMinMaxSizesForMinContentContribution()` uses the item's definite cross-size as the available space, not the container's
- The percentage resolution basis for stretched items is set to the item's actual stretched size via the constraint space builder

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Lines 2580-2600**: Pass the item's stretched cross-size (if applicable) as the available inline-size when computing content suggestion in column flex:
```go
// If the item is stretched and cross = inline, use its resolved cross-size
// as the available inline-size for the content suggestion layout.
availableInline := containerInlineSize
if !mainIsItemInline && hasDefiniteCross {
    stretchedCross := containerCrossSize - childGeom.InlineBorderPadding() - crossMargins
    if stretchedCross > 0 {
        availableInline = stretchedCross
    }
}
```

2. **Lines 2661-2667**: Ensure the transferred suggestion uses the correct item cross-size (same fix as Target 3, Issue 2).

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-height-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-width-auto" -count=1
# Full regression:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 498
```

---

## Target 5: Column Flex Alignment & Vertical Writing Modes (~14 tests)

### Problem
Cross-axis alignment (align-self) in vertical/column flex containers produces incorrect positioning, particularly with RTL direction and different writing modes on flex items. The alignment algorithm doesn't correctly map logical alignment values (self-start, self-end) to physical positions when the flex container and items have different writing modes.

### Affected Tests (~14 failures)

| Category | Count | Examples |
|----------|-------|---------|
| flexbox-align-self-vert | 4 | 001, 002, 003, 004 |
| flexbox-align-self-vert-rtl | 5 | 001, 002, 003, 004, 005 |
| flexbox-align-self-horiz | 3 | 001-table, 002, 004 |
| align-content | 2 | 004, 007 |

### Root Cause (from code analysis)

**File: `pkg/layout/flex_layout.go`, lines 820-899**

**Issue 1 — selfStartIsCrossStart (lines 853-867)**: The `selfStartIsCrossStart` function determines whether an item's "self-start" maps to the container's cross-start. For column flex with RTL items, this mapping is critical but may be incorrect when item writing-mode differs from container writing-mode.

**Issue 2 — Reference rendering**: Many of these tests use XHTML reference files with complex inline-block patterns to simulate flex alignment. The reference files for flexbox-align-self-vert use `.centerParent` wrappers with `display: inline-block` and `text-align: center`. If inline-block rendering has issues (see Target 1), these failures may partially resolve when Target 1 is fixed.

**Issue 3 — align-content with wrap-reverse (align-content-007.htm)**: The `align-content: stretch` with `flex-wrap: wrap-reverse` may not correctly reverse the cross-axis line ordering before distributing space.

### What Blink Does

In Blink's `flex_layout_algorithm.cc`:
- `CrossAxisAlignmentForChild()` computes the resolved alignment, accounting for writing mode differences between container and item
- `PhysicalToLogical()` converts physical alignment directions based on the item's own writing direction, not the container's
- `IsStartMarginCrossAxisAuto()` and `IsEndMarginCrossAxisAuto()` use the item's writing mode for margin resolution

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **selfStartIsCrossStart function**: Audit this function to ensure it correctly handles all combinations of container/item writing modes and direction values. The self-start/self-end alignment should be relative to the ITEM's writing mode, while flex-start/flex-end are relative to the CONTAINER's.

2. **Lines 820-899**: Verify that the alignment algorithm uses the correct cross-axis free space when the container has wrap-reverse (the cross-start and cross-end are swapped).

3. **align-content for wrap-reverse**: Ensure cross-axis line distribution accounts for the reversed order.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-horiz" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/align-content" -count=1
# Full regression:
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "REFTEST PASS"
# Must be >= 498
```

---

## Independence Check

| | inline_layout.go | min_max_sizing.go | flex_layout.go (§4.5 aspect) | flex_layout.go (§4.5 min-auto) | flex_layout.go (§8 align) |
|---|---|---|---|---|---|
| Target 1 (Inline ref) | **Yes** | - | - | - | - |
| Target 2 (Content sizing) | - | **Yes** | - (line 1414 only) | - | - |
| Target 3 (Aspect ratio) | - | - | **Yes** (lines 2630-2760) | - | - |
| Target 4 (Min-auto) | - | - | shared (lines 2661-2667) | **Yes** (lines 2570-2600) | - |
| Target 5 (Alignment) | - | - | - | - | **Yes** (lines 820-900) |

**Parallelizable groups:**
- **Group A** (fully independent): Targets 1, 2, and 5 touch different files/sections
- **Group B** (must sequence): Targets 3 and 4 overlap in the transferred size suggestion code (lines 2630-2760). Implement Target 3 first, then Target 4 can build on those fixes.

Recommended parallel launch: **Targets 1, 2, and 5** as three agents. Then a follow-up round for Targets 3+4 combined.

---

## IMPORTANT: Agent Guidelines

- **Read CLAUDE.md first** — it contains non-negotiable project rules about foundational correctness, studying Blink first, and test expectations.
- **Study Blink's approach** before writing code in any new area. Key Blink files:
  - Flex: `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`
  - Inline: `third_party/blink/renderer/core/layout/inline/inline_box_state.cc`
  - Min/max: `third_party/blink/renderer/core/layout/min_max_sizes_utils.cc`
  - Replaced: `third_party/blink/renderer/core/layout/layout_replaced.cc`
- **Commit and report at each milestone** (don't batch everything to the end).
- **When running in a worktree** (any directory that is NOT ~/louis14), commit ONLY to your worktree branch. Never commit directly to fix/* or master branches.
- **Regression constraints**: Current passing count is 498 flex tests. After your fix, this number must NOT decrease.
- **Target 1 is high-risk**: Inline layout changes affect ALL tests (CSS2, writing-modes, flexbox, etc.). Run broad regression tests before committing.
- **Do not chase individual tests**: Fix the root cause algorithm, verify it works for ALL affected tests, and check for regressions.
