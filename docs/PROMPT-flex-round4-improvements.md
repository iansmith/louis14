# Flexbox Round 4: Top 3 Improvements

Current state: 515 pass / 229 fail (69.3% pass rate) across 744 tests.
5 failures are dynamic/JS tests (unfixable without a JS engine).

These three targets are **independent** (touch different subsystems) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Column Flex Cross-Axis Sizing and Stretch (~20 tests)

### Problem
Column flex items are not correctly sized on the cross axis (inline/width dimension). The primary issues are:
1. Stretch items in column flex don't properly account for margins when computing the stretched width
2. Non-stretch items in column flex may not be getting the correct available inline-size for their layout
3. The two-pass relayout (§9.8) may be incorrectly relaying out ALL non-stretch items instead of only those with percentage cross-sizes

### Affected Tests (~20 failures)
| Category | Count | Examples |
|----------|-------|---------|
| Column align-self | 4 | flexbox-align-self-vert-{001..004}.xhtml |
| Column RTL align | 5 | flexbox-align-self-vert-rtl-{001..005}.xhtml |
| Table flex item | 1 | flexbox-align-self-horiz-001-table.xhtml |
| Column sizing | 2 | flexbox-sizing-horiz-002.xhtml, flexbox-sizing-vert-001.xhtml |
| Stretch edge cases | 2 | flexbox_align-items-stretch-3.html, align-self-016.html |
| Column gap | 1 | flexbox-column-row-gap-004.html |
| Column definite sizes | 3 | flexbox-definite-sizes-003.html, -004.html, flexbox-definite-cross-size-constrained-percentage.html |
| Whitespace handling | 2 | flexbox-whitespace-handling-001a.xhtml, 001b.xhtml |

### Root Cause (from code analysis)

**Issue 1: buildItemConstraintSpace column path (line ~2120-2155)**

For column flex, the constraint space builder adds `crossBorderPadding()` to available inline-size:
```go
// flex_layout.go:2129
availInline := crossInlineContent + item.crossBorderPadding()
```
This adds the item's own border-padding to the available size, which is wrong. Blink passes the cross-size directly as the available inline-size. The border-padding is already handled by the child layout's geometry.

**Issue 2: Column stretch pass not handling wrapping correctly**

At line 404:
```go
crossIsFixed := !isRow && isStretch && wrapMode == "nowrap"
```
For wrapping column flex, stretch items get `crossIsFixed=false` in the first pass, causing them to fit-content. But after line cross-sizes are determined, the stretch pass should fix them. However, the stretch pass at line ~3029 computes:
```go
stretchBorderBox = line.crossSize - item.crossMarginSum()
```
This uses `line.crossSize` which for column flex is the max inline-size of items in the line. If all items fit-content'd to narrow widths, the line cross-size is small, and stretch items stretch to that small size instead of the container width.

**Issue 3: §9.8 relayout over-broad**

At line ~525, the two-pass relayout section relays out ALL non-stretch items with the definite line cross-size. This is expensive and can change items that were already correctly sized. Per Blink, only items with percentage cross-sizes or aspect ratios should be relaid out.

### What Blink Does

In Blink's `FlexLayoutAlgorithm::PlaceFlexItems()`:
1. For the initial layout, items get the container's cross-size as the available size
2. The available size does NOT include the item's own border-padding (that's the child's job)
3. For stretch items, Blink sets `is_fixed_block_size = true` with the stretched content-box size
4. For the relayout pass, Blink only relays out items that have `NeedsRelayout()` = true, which checks for percentage heights, aspect ratios, and changed cross-sizes

Key Blink source: `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`

### Fix Location

**File: `pkg/layout/flex_layout.go`**

1. **Fix `buildItemConstraintSpace` column path** (~line 2120-2155):
   - Remove the `+ item.crossBorderPadding()` from `availInline`
   - The available inline-size should be `crossInlineContent` (or `contentInlineSize` when no cross-size is fixed)
   - For non-fixed cross items, subtract only margins (not border-padding) since the child handles its own BP

2. **Fix column flex line cross-size for wrapping** (~line 350-380):
   - For wrapping column flex with `align-content: stretch`, ensure lines grow to container cross-size

3. **Optimize §9.8 relayout** (~line 525-580):
   - Only relayout items that have percentage cross-sizes or aspect ratios
   - Check `item.style.GetPercentage("width")` (for column flex cross = width)

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-sizing" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1  # full suite, check for regressions
```

---

## Target 2: Block Layout Float/BFC Interaction (~6 tests)

### Problem
Flex containers that interact with floats (as BFC elements) are not laid out correctly. Flex containers establish a Block Formatting Context (BFC), which means:
1. They should not overlap float margin boxes
2. They should respect `clear` properties
3. Negative margins on flex containers should allow overlap with floats

### Affected Tests (~6 failures)
| Category | Count | Examples |
|----------|-------|---------|
| BFC + floats | 1 | flexbox_fbfc.html |
| Clear + flex | 1 | flexbox_box-clear.html |
| Formatting interop | 1 | flexbox_flex-formatting-interop.html |
| Flex overflow | 1 | flexbox-overflow-horiz-001.html |
| Margin-border-padding | 2 | flexbox-mbp-horiz-002v.xhtml, flexbox-mbp-horiz-004.xhtml |

### Root Cause (from code analysis)

The `layoutFloat` function in `block_layout.go` has issues with RTL float positioning and BFC clearing.

**Issue 1: RTL float inline position (line ~918-950)**

The `layoutFloat` function positions floats using the `floatSide` directly without accounting for RTL direction. In RTL, `float: left` should map to inline-end, and `float: right` to inline-start. Currently:
```go
// block_layout.go:~939
if floatSide == css.FloatLeft {
    startOff, _ := es.FindAvailableInlineSize(...)
    floatInlineOffset = startOff + childMargins.InlineStart
} else {
    _, endOff := es.FindAvailableInlineSize(...)
    floatInlineOffset = contentInlineSize - endOff - floatInlineSize - childMargins.InlineEnd
}
```

The `floatSide` is a physical value (left/right) but the positioning uses logical coordinates. In RTL, the logical-to-physical mapping needs to swap.

**Issue 2: BFC elements not pushed below floats**

When a BFC element (like a flex container) doesn't fit beside a float, CSS 2.1 §9.5 says it should be pushed below the float. The code at line ~233 should handle this but may have edge cases where the BFC check is incomplete or the clearance computation is wrong.

**Issue 3: Margin collapsing through empty BFC elements**

At line ~291-301, the collapse-through check may incorrectly allow margins to collapse through elements that establish a new BFC. Per CSS 2.1 §8.3.1, BFC elements should NOT have margins collapse through them.

### What Blink Does

In Blink's `BlockLayoutAlgorithm::Layout()`:
1. Float positioning uses logical coordinates throughout - there's no physical-to-logical conversion needed because everything is logical from the start
2. BFC elements that don't fit beside floats are placed using `PositionNewFC()` which tries beside the float, then falls back to below it
3. Margin collapsing always checks `EstablishesNewFormattingContext()` to prevent collapse-through

Key Blink source: `third_party/blink/renderer/core/layout/block_layout_algorithm.cc`

### Fix Location

**File: `pkg/layout/block_layout.go`**

1. **Fix RTL float positioning** (~line 918-950):
   - Map physical float side to logical side before positioning
   - In RTL: `float: left` → inline-end, `float: right` → inline-start
   - Apply this mapping before the `if floatSide == css.FloatLeft` branch

2. **Fix BFC clearing below floats** (~line 233-280):
   - Ensure BFC elements that don't fit beside floats are pushed below them
   - Check the resolved inline-size of the BFC element against available space

3. **Fix margin collapse-through for BFC** (~line 291-301):
   - Add `!isChildNewFC` to the `collapseThrough` condition:
   ```go
   collapseThrough := childBlockSize == 0 &&
       len(childResult.Fragment.Children) == 0 &&
       childGeom.Border.BlockStart == 0 && childGeom.Border.BlockEnd == 0 &&
       childGeom.Padding.BlockStart == 0 && childGeom.Padding.BlockEnd == 0 &&
       !isChildNewFC
   ```

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_fbfc" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_box-clear" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_flex-formatting-interop" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1  # full suite regression check
cd pkg/visualtest && go test -v -run "TestListReftestResults" -count=1  # CSS2 regression check
```

---

## Target 3: Replaced Element and Intrinsic Sizing in Flex (~20 tests)

### Problem
Replaced elements (img, canvas, video, iframe, textarea, fieldset) as flex items have incorrect sizing. The intrinsic sizing info extraction and the flex algorithm's handling of aspect ratios for these elements needs fixing.

### Affected Tests (~20 failures)
| Category | Count | Examples |
|----------|-------|---------|
| Horizontal replaced | 7 | flexbox-basic-{img,canvas,video,iframe,textarea,fieldset,block}-horiz-001*.xhtml |
| Vertical replaced | 7 | flexbox-basic-{img,canvas,video,iframe,textarea,fieldset,block}-vert-001*.xhtml |
| Aspect ratio row | 5 | flex-aspect-ratio-img-row-{004,006,007,015,017}.html |
| Aspect ratio column | 4 | flex-aspect-ratio-img-column-{008,010,012,018}.html |
| Intrinsic sizing | 3 | aspect-ratio-intrinsic-size-{001,002,005}.html |

### Root Cause (from code analysis)

**Issue 1: Textarea intrinsic sizing (intrinsic_sizing.go)**

Textareas have a default intrinsic size that should be based on their `rows` and `cols` attributes (or defaults: 2 rows, 20 cols). Currently, textarea sizing may not account for the cols/rows-based intrinsic dimensions, causing them to collapse or expand incorrectly in flex.

**Issue 2: Canvas/iframe/video intrinsic size (intrinsic_sizing.go)**

These elements have default intrinsic sizes per HTML spec:
- canvas: 300×150
- iframe: 300×150 (with scrollbars)  
- video: 300×150 (no poster), poster dimensions (with poster)

If these defaults aren't returned by `GetIntrinsicSizingInfo()`, the flex algorithm can't compute correct aspect ratios or flex-basis values.

**Issue 3: Aspect ratio with padding percentages (replaced_layout.go)**

Several aspect-ratio tests involve images with percentage padding (e.g., `padding: 10%`). Padding percentages resolve against the containing block's inline-size. When an image flex item has `padding: 10%`, the padding must be resolved before computing the aspect-ratio-derived size. The current `ComputeReplacedSize()` may not account for this correctly.

**Issue 4: flex-basis:content for replaced elements**

When `flex-basis: content` is used with a replaced element, the flex base size should be the element's intrinsic main-axis dimension (ignoring CSS width/height). The current `resolveFlexBasis` may use the CSS-resolved size instead of the intrinsic size.

### What Blink Does

In Blink:
1. `IntrinsicSizingInfo` struct carries `size`, `aspect_ratio`, and `has_width`/`has_height` flags
2. `LayoutReplaced::ComputeIntrinsicSizingInfo()` returns the intrinsic dimensions for each element type
3. The flex algorithm uses `IntrinsicSize()` to get the content-based size for `flex-basis: content`
4. Padding percentages on replaced elements are resolved using `ResolvePercentage(container_inline_size)`

Key Blink sources:
- `third_party/blink/renderer/core/layout/layout_replaced.cc`
- `third_party/blink/renderer/core/layout/intrinsic_sizing_info.h`

### Fix Location

**File: `pkg/layout/intrinsic_sizing.go`**

1. **Fix textarea intrinsic sizing** - add rows/cols-based default sizing:
   - Default: 20 cols × 2 rows at average character width
   - Use font metrics to compute character width if available

2. **Verify canvas/iframe/video defaults** - ensure 300×150 default is returned

**File: `pkg/layout/replaced_layout.go`**

3. **Fix aspect ratio with padding percentages** - ensure padding is resolved against the container inline-size before computing the aspect-ratio-derived dimension

4. **Fix `ComputeReplacedSize` for flex context** - when called from flex layout, ensure the available size correctly reflects the flex constraints

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-img" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-canvas" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio-intrinsic" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1  # full suite regression check
```

---

## Independence Check

| | flex_layout.go | block_layout.go | intrinsic_sizing.go | replaced_layout.go |
|---|---|---|---|---|
| Target 1 (Column Flex) | **Yes** | - | - | - |
| Target 2 (BFC/Float) | - | **Yes** | - | - |
| Target 3 (Replaced Elements) | - | - | **Yes** | **Yes** |

All three targets touch different source files and can be developed independently.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Search chromium.googlesource.com for the relevant algorithms.
- **Commit and report at each milestone** (don't batch everything to the end). Create a commit after each meaningful fix that passes at least some new tests.
- **Run the full flex suite after each change** to check for regressions: `cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1`
- **Also run CSS2 regression check** for Target 2 (block_layout changes): `cd pkg/visualtest && go test -v -run "TestListReftestResults" -count=1`
- **Never use `open` to display files** — it disrupts the user's screen.
- **When running in a worktree**, commit ONLY to your worktree branch. Never commit directly to fix/* or master.
- **All tests must pass at 0% diff**. Don't dismiss small diffs as acceptable.
- **Foundational correctness over test counts**: Every change must make the codebase structurally more correct, even if it causes temporary regressions in other tests. But always verify with the full suite.
- **No speculative changes**: Only modify code paths that are exercised by failing tests. Don't refactor working code.
