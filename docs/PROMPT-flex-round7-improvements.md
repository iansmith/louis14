# Flexbox Round 7: Top 6 Improvements

Current state: 508 pass / 122 fail (80.6% pass rate) across 630 tests.

These six targets are designed for parallel worktree agents. Targets 1 and 2 modify completely separate files from Target 3–6. Targets 3–6 all modify `flex_layout.go` but in **non-overlapping line ranges** (separated by hundreds of lines), so git can auto-merge them.

**Recommended parallel groups:**
- **Group A (fully independent files):** Targets 1 + 2
- **Group B (flex_layout.go, non-overlapping ranges):** Targets 3 + 4 + 5 + 6

All 6 can run simultaneously.

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

## Target 1: Paint Ordering for Flex Items (~4 tests)

### Problem
Flex items with the `order` property and/or `z-index` are not painted in the correct stacking order. CSS Flexbox §4.3 states that flex items paint in `order`-modified document order, and flex items with explicit `z-index` create stacking contexts even when `position:static`.

### Affected Tests (~4 failures)

| Test | Diff |
|------|------|
| flexbox-paint-ordering-001.xhtml | 0.6% |
| flexbox-paint-ordering-002.xhtml | 2.3% |
| flexbox-order-only-flexitems.html | 0.2% |
| flexbox_direction-row-reverse.html | 3.7% |

### Root Cause (from code analysis)
In `pkg/render/paint_layer.go` (~line 800), flex items with `z-index` create stacking contexts, but the paint order doesn't account for the CSS `order` property. Per Flexbox §4.3, flex items paint in order-modified document order (items with lower `order` paint first, ties broken by DOM order). The current code uses raw DOM order.

Additionally, `flexbox_direction-row-reverse.html` tests that `row-reverse` reverses the visual order but items still paint in order-modified document order (not physical order).

### What Blink Does
In `third_party/blink/renderer/core/layout/layout_flexible_box.cc`, Blink sorts flex items by their `order` property for painting. The paint code in `box_fragment_painter.cc` iterates children in the fragment's order (which is already sorted by the flex layout algorithm).

In louis14, the flex layout already sorts items by `order` in `sortFlexItems()` (line 2822). The issue is likely in the paint layer's handling of non-positioned flex items — they should paint in the flex-modified order, not DOM order.

### Fix Location
**File:** `pkg/render/paint_layer.go`
- The `buildPaintTree` function (~line 783) processes children in DOM order. For flex containers, it should use the fragment's child order (which is already flex-sorted) rather than DOM tree order.
- Check how `PaintLayer.Children` is populated — flex item children should be in `order`-sorted sequence.

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-paint-ordering-00[12]|TestWPTCSS3Reftests/css-flexbox/flexbox-order-only-flexitems|TestWPTCSS3Reftests/css-flexbox/flexbox_direction-row-reverse" -count=1
```

---

## Target 2: Replaced Element Sizing in Flex (canvas, textarea, fieldset, iframe, video) (~10 tests)

### Problem
Replaced elements (canvas, textarea, fieldset, iframe, video) render incorrectly when used as flex items, especially in vertical (column) flex containers. These elements have specific intrinsic sizing rules that interact with the flex algorithm.

### Affected Tests (~10 failures)

| Test | Diff | Element |
|------|------|---------|
| flexbox-basic-canvas-vert-001.xhtml | 0.0% | canvas |
| flexbox-basic-canvas-vert-001v.xhtml | 0.0% | canvas |
| flexbox-basic-canvas-horiz-001v.xhtml | 0.2% | canvas |
| flexbox-basic-textarea-vert-001.xhtml | 1.3% | textarea |
| flexbox-basic-textarea-horiz-001.xhtml | 0.6% | textarea |
| flexbox-basic-fieldset-vert-001.xhtml | 0.0% | fieldset |
| flexbox-basic-iframe-vert-001.xhtml | 0.0% | iframe |
| flexbox-basic-img-vert-001.xhtml | 0.0% | img |
| flexbox-basic-video-vert-001.xhtml | 0.0% | video |
| flexbox-basic-block-vert-001v.xhtml | 0.0% | div (writing-mode) |

### Root Cause (from code analysis)
Several issues:
1. In `pkg/layout/intrinsic_sizing.go` line 29, all replaced elements (video, iframe, textarea, etc.) return the same 300×150 intrinsic size. Textarea and fieldset are NOT replaced elements — they're form controls with different intrinsic sizing rules.
2. In `pkg/layout/replaced_layout.go`, the `ComputeReplacedSize` function may not correctly handle flex constraints (main/cross size).
3. The `flexbox-basic-*-vert-001.xhtml` tests use `flex-direction: column` with various `flex` values. The flex items should grow/shrink according to flex ratios, but replaced element minimum sizing may prevent correct shrinking.

### What Blink Does
Blink has separate intrinsic sizing for each element type:
- `LayoutTextArea` returns intrinsic size based on `rows`/`cols` attributes
- `LayoutFieldSet` is not a replaced element; it uses block layout
- Canvas uses width/height HTML attributes
- iframe/video use 300×150 default

### Fix Location
**Files:** `pkg/layout/intrinsic_sizing.go`, `pkg/layout/replaced_layout.go`
- Add proper intrinsic sizing for `textarea` (based on `rows`/`cols` attributes and font size)
- Remove `textarea` and `fieldset` from the replaced element path — they should use block layout
- Verify that `isReplacedElement()` in the flex code correctly classifies these elements
- Check `pkg/layout/engine.go` for `isReplacedElement` function

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-(canvas|textarea|fieldset|iframe|img|video|block)-vert" -count=1
```

---

## Target 3: Align-Self Cross-Axis Positioning in Column Flex + RTL (~18 tests)

### Problem
`align-self` values (flex-start, flex-end, center, baseline, stretch, self-start, self-end) position items incorrectly in `flex-direction: column` containers, and RTL direction variants have additional issues.

### Affected Tests (~18 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| Column flex align-self (LTR) | 4 | flexbox-align-self-vert-001/002/003/004 |
| Column flex align-self (RTL) | 5 | flexbox-align-self-vert-rtl-001/002/003/004/005 |
| Baseline alignment (horiz) | 6 | flexbox-align-self-baseline-horiz-001a/001b/003/006/007/008 |
| Table flex item align-self | 1 | flexbox-align-self-horiz-001-table |
| Stretch edge case | 1 | flexbox-align-self-stretch-vert-002 |
| Self-start/self-end | 1 | flexbox-align-self-horiz-002 |

### Root Cause (from code analysis)
In `flex_layout.go` lines 886-980, the cross-axis alignment switch handles various `align-self` values. Several potential issues:

1. **`self-start` / `self-end` resolution** (lines 930-945): The `selfStartIsCrossStart` function (line 78) determines whether self-start maps to cross-start. For column flex with RTL, the cross axis is the inline axis, so self-start should map to inline-start (right edge in RTL). The current implementation may not correctly handle all WDM combinations.

2. **Baseline alignment in column flex** (lines 948-961): Baseline alignment in column flex requires the item's block axis to be perpendicular to the container's cross axis. The `baselineParallel` check (line 497/853) may be incorrect for some writing mode combinations.

3. **RTL with column flex**: In a column flex container with `direction: rtl`, the cross axis is inline (right-to-left). `flex-start` should map to the physical right edge, but the positioning at line 977 (`itemCrossOffset = crossStart`) may not account for this because the fragment builder handles the LTR→RTL flip via `ToPhysicalOffset`.

### What Blink Does
In `flex_layout_algorithm.cc`, Blink's `GiveItemsFinalPositionAndSize()` computes cross-axis positions using `ContentAlignmentNormalBehavior()` and handles each alignment value. For RTL column flex, Blink uses `IsInlineStartForChild()` which properly accounts for the item's writing mode relative to the container's.

### Fix Location
**File:** `pkg/layout/flex_layout.go`
- Lines 78-110: `selfStartIsCrossStart` function
- Lines 846-884: Shared baseline computation
- Lines 886-980: Cross-axis alignment switch

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-(vert|baseline-horiz|horiz)" -count=1
```

---

## Target 4: Min-Size Auto and Transferred Size Suggestion (~18 tests)

### Problem
The automatic minimum size calculation (CSS Flexbox §4.5) has bugs in several areas: content suggestion for block-axis items, transferred size suggestion with aspect ratios, and interaction with overflow properties.

### Affected Tests (~18 failures)

| Category | Count | Example Tests |
|----------|-------|---------------|
| min-height:auto | 4 | flexbox-min-height-auto-001/002c/003/004 |
| min-width:auto | 2 | flexbox-min-width-auto-005/006 |
| flex min-height items | 3 | flex-minimum-height-flex-items-019/023/030 |
| flex min-width items | 3 | flex-minimum-width-flex-items-001/003/016 |
| Aspect ratio intrinsic | 7 | aspect-ratio-intrinsic-size-001/002/005/007/008/009/010 |
| Transferred sizes | 2 | flex-item-transferred-sizes-padding-border/content-sizing |
| Max-height min-content | 1 | flex-item-max-height-min-content |

### Root Cause (from code analysis)
In `flex_layout.go`, the `flexItemMinMain` function (lines 2960-3295) implements §4.5:

1. **Content suggestion block-axis** (lines 3063-3087): When computing the content suggestion for column flex, the `colMinSpace` constraint space sets `BlockSize: Indefinite` but doesn't provide the correct `PercentageResolutionSize.BlockSize`. This means percentage-height descendants can't resolve, leading to incorrect content-based min-height.

2. **Transferred size suggestion scope** (lines 3113-3188): The transferred size suggestion is only computed for replaced elements (`isReplacedElement`). Per the spec, non-replaced elements with `aspect-ratio` CSS property should also get a transferred size suggestion. The code at lines 3195-3240 handles CSS `aspect-ratio`, but the `min-cross-size` fallback logic may be incorrect.

3. **Replaced element content suggestion** (line 3085): For replaced elements, `lf.BlockSize() - childGeom.BlockBorderPadding()` is used. But for images with aspect ratios, the content suggestion should be `min(content, transferred)`, and the content size for an image is its intrinsic height, not the layout-computed height.

### What Blink Does
In `flex_layout_algorithm.cc`, `ComputeMinAndMaxContentContribution()` computes the min/max content sizes. `ComputeAutomaticMinimumSize()` handles the §4.5 algorithm. Blink computes:
- Content size suggestion via `ComputeMinContentContribution()`
- Specified size suggestion from the preferred size
- Transferred size suggestion via aspect ratio and definite cross-size
Then combines: `min(specified, max(content, transferred))` for non-replaced, `min(specified, min(content, transferred))` for replaced.

### Fix Location
**File:** `pkg/layout/flex_layout.go`
- Lines 2960-3295: `flexItemMinMain` function
- Lines 2885-2958: `flexItemExplicitMin` function

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/(flexbox-min-(height|width)-auto|flex-minimum-(height|width)-flex-items|aspect-ratio-intrinsic-size|flex-item-transferred-sizes|flex-item-max-height)" -count=1
```

---

## Target 5: Flex Basis Content & Definite Size Resolution (~10 tests)

### Problem
`flex-basis: content` doesn't properly resolve to the item's max-content size. Additionally, nested flex containers don't correctly resolve percentage sizes when the flex container's cross-size becomes definite through the flex algorithm.

### Affected Tests (~10 failures)

| Test | Diff | Feature |
|------|------|---------|
| flexbox-flex-basis-content-002a.html | 0.5% | flex-basis:content in column |
| flexbox-flex-basis-content-002b.html | 0.5% | flex-basis:content via shorthand |
| flexbox-flex-basis-content-003a.html | 1.0% | flex-basis:content = max-content |
| flexbox-flex-basis-content-003b.html | 1.0% | flex-basis:content = max-content |
| flexbox-definite-sizes-003.html | 1.2% | nested flex + max-height |
| flexbox-definite-sizes-004.html | 1.2% | nested flex + max-height |
| flexbox-definite-cross-size-constrained-percentage.html | 0.2% | % height definite |
| flexbox-sizing-horiz-002.xhtml | 1.1% | auto-height + min/max |
| flexbox-sizing-vert-001.xhtml | 1.0% | auto-height column |
| flex-direction-modify.html | 9.7% | dynamic flex-direction change |

### Root Cause (from code analysis)
In `flex_layout.go`, the `resolveFlexBasis` function (line 1467) handles `flex-basis` resolution. When `flex-basis: content`, the basis should be the item's max-content size in the main axis. The current implementation may fall through to auto sizing instead of computing max-content.

For definite sizes, CSS Flexbox §9.8 states that "If a single-line flex container has a definite cross size, the automatic preferred outer cross size of any stretched flex items is the flex container's inner cross size." This definiteness should propagate to percentage-height descendants.

### What Blink Does
In `flex_layout_algorithm.cc`, `ComputeFlexBasisForChild()` handles `flex-basis: content` by computing the max-content size via `ComputeMaxContentContribution()`. The definiteness flag is set via `SetIsInitialBlockSizeIndefinite(false)` when the flex line's cross-size is known.

### Fix Location
**File:** `pkg/layout/flex_layout.go`
- Lines 1467-1782: `resolveFlexBasis` function (flex-basis:content handling)
- Lines 573-625: §9.8 second layout pass (definiteness propagation)

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/(flexbox-flex-basis-content|flexbox-definite-sizes|flexbox-definite-cross|flexbox-sizing-(horiz|vert)|flex-direction-modify)" -count=1
```

---

## Target 6: Align-Content Multi-Line + Visibility:Collapse (~12 tests)

### Problem
`align-content` distribution in multi-line (wrapping) flex containers has issues with `stretch` fallback behavior. Also, `visibility: collapse` on flex items should create "struts" that preserve the line's cross-size but the collapsed item occupies zero main-axis space.

### Affected Tests (~12 failures)

| Test | Diff | Feature |
|------|------|---------|
| align-content-007.htm | 10.3% | align-content:stretch fallback |
| align-content-004.htm | 2.3% | align-content:space-between |
| flexbox-flex-wrap-vert-001.html | 0.6% | flex-wrap in column |
| flexbox-flex-wrap-vert-002.html | 0.2% | min-height + flex-wrap |
| flexbox-collapsed-item-horiz-002.html | 0.3% | collapse strut migration |
| flexbox-collapsed-item-horiz-003.html | 0.1% | strut after line stretch |
| flexbox-break-request-vert-001a.html | 0.5% | page-break in column flex |
| flexbox-break-request-vert-001b.html | 0.5% | page-break in column flex |
| flexbox-break-request-vert-002a.html | 0.5% | page-break in column flex |
| flexbox-break-request-vert-002b.html | 0.5% | page-break in column flex |
| flexbox_align-items-stretch-3.html | 1.5% | stretch + flex base size |
| justify-content_space-between-003.tentative.html | 2.1% | space-between + reverse |

### Root Cause (from code analysis)
1. **align-content:stretch fallback** (lines 2577-2589 in `computeAlignContent`): The `stretch` case distributes extra space to lines but uses `freeSpace / float64(len(lines))`. Per CSS Align §5.3, when the total line cross-sizes exceed the container cross-size (negative free space), the fallback alignment is `flex-start`, not `stretch`. The current code may not handle this correctly.

2. **align-content:stretch for single-line wrapping** (lines 694-697): The single-line wrapping stretch is handled separately, but `align-content-007.htm` tests the fallback behavior when align-content:stretch has no definite cross-size.

3. **visibility:collapse strut** (line 2390-2397 in `computeItemMainOffsets`): Collapsed items are positioned but their strut cross-size (which should equal their pre-collapse cross-size) may not be used to expand the line's cross-size.

4. **flex-wrap: wrap in column flex** (lines 360-371): When `flex-direction: column` with `flex-wrap: wrap`, the container's block-size determines the wrap boundary. The current code checks for `max-height` but may not correctly use `height` when definite.

### What Blink Does
Blink's `HandleAlignContentStretch()` distributes extra space equally among lines. For collapsed items, `UpdateCollapsedFlexItemGeometry()` sets the strut dimensions. For column wrap, the wrap boundary comes from the resolved block-size.

### Fix Location
**File:** `pkg/layout/flex_layout.go`
- Lines 2501-2611: `computeAlignContent` function
- Lines 2378-2498: `computeItemMainOffsets` (collapsed item handling)
- Lines 360-371: Column wrap boundary
- Lines 686-698: Single-line wrapping stretch

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox/(align-content-00[47]|flexbox-flex-wrap-vert|flexbox-collapsed-item-horiz|flexbox-break-request-vert|flexbox_align-items-stretch-3|justify-content_space-between-003)" -count=1
```

---

## Independence Check

| File | Target 1 | Target 2 | Target 3 | Target 4 | Target 5 | Target 6 |
|------|----------|----------|----------|----------|----------|----------|
| `pkg/render/paint_layer.go` | **Yes** | - | - | - | - | - |
| `pkg/layout/intrinsic_sizing.go` | - | **Yes** | - | - | - | - |
| `pkg/layout/replaced_layout.go` | - | **Yes** | - | - | - | - |
| `pkg/layout/engine.go` | - | Maybe | - | - | - | - |
| `flex_layout.go` lines 78-110 | - | - | **Yes** | - | - | - |
| `flex_layout.go` lines 360-700 | - | - | - | - | Maybe | **Yes** |
| `flex_layout.go` lines 846-980 | - | - | **Yes** | - | - | - |
| `flex_layout.go` lines 1467-1782 | - | - | - | - | **Yes** | - |
| `flex_layout.go` lines 2378-2498 | - | - | - | - | - | **Yes** |
| `flex_layout.go` lines 2501-2611 | - | - | - | - | - | **Yes** |
| `flex_layout.go` lines 2885-3295 | - | - | - | **Yes** | - | - |

**Targets 1 and 2** are fully file-independent. **Targets 3–6** all touch `flex_layout.go` but in non-overlapping line ranges — git auto-merge will succeed. **Target 5 and 6** both touch the 360-700 range (§9.4 layout passes); if both modify lines in that range, merge manually.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area.
- **Commit and report at each milestone** (don't batch everything to the end).
- Build command: `GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go build ./...`
- Run only the specific tests listed in each target's Verification section during development.
- After all fixes are complete and merged, run the full regression check:
  ```bash
  cd pkg/visualtest && GOTOOLCHAIN=auto $HOME/sdk/go1.25.5/bin/go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | tail -5
  ```
