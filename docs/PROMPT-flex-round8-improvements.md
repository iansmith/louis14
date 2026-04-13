# Flexbox Round 8: Top 6 Failure Categories

Current state: 529 pass / 100 fail (84% pass rate) across 629 flex tests.

## IMPORTANT: File-Overlap Constraints

Almost all flex failures trace back to `pkg/layout/flex_layout.go`. Only Targets 5 and 6 touch separate files and can be developed as parallel worktree agents. Targets 1-4 all modify `flex_layout.go` and **must be done serially** in a single session (or single agent) to avoid merge conflicts.

**Recommended approach:**
- Targets 5 + 6: launch as parallel worktree agents (separate files)
- Targets 1-4: work serially in the main session on `flex_layout.go`

---

## Target 1: Baseline Alignment (~12 tests) — `flex_layout.go`

### Problem
Baseline alignment in horizontal flex containers doesn't correctly handle multi-line text items, last-baseline alignment, or baseline synthesis. Items with different content types (single-line text, multi-line, super/subscript, larger fonts) don't align correctly on shared baselines.

### Affected Tests (~12 failures)
| Test Pattern | Count | Description |
|---|---|---|
| flexbox-align-self-baseline-horiz-* | 5 | First/last baseline with mixed content |
| flexbox-baseline-multi-item-horiz-* | 2 | Multiple baseline items in a line |
| flexbox-baseline-multi-line-* | 3 | Baseline in multi-line flex |
| align-items-baseline-overflow-* | 1 | Baseline with overflow:hidden |
| fieldset-baseline-alignment | 1 | Baseline of fieldset elements |

### Root Cause (from code analysis)
The baseline alignment code in `flex_layout.go` has several issues:

1. **§9.4 line cross-size** (lines 488-543): Baseline items track ascent/descent but Blink uses **two baseline groups** (major and minor) per line. Our code uses a single shared baseline, which doesn't correctly handle `last baseline` items alongside `baseline` items in the same line.

2. **§9.9 positioning** (lines 987-1017): The `last baseline` case at line 1004 doesn't have the `isRow &&` guard that `baseline` has. Also, when `hasLastBaselineItem` is false, it falls back to `flex-end` but should fall back based on the baseline group.

3. **Baseline synthesis** (line 882-885): `canSynthesize` is set to `isRow && !wdm.IsVertical()` which is correct for horizontal, but items without a natural baseline (e.g., empty divs) get `bl=0` which positions them at cross-start instead of synthesizing to block-end.

### What Blink Does
Blink's `flex_layout_algorithm.cc` uses `BaselineAccumulator` with **four values per line**: `max_major_ascent`, `max_major_descent`, `max_minor_ascent`, `max_minor_descent`. Items are assigned to major or minor baseline groups via `DetermineBaselineGroup()`. The line cross-size = `max(major_ascent + major_descent, minor_ascent + minor_descent)`.

For baseline synthesis, Blink's `LogicalBoxFragment::SynthesizedBaseline()` returns `block_size` for alphabetic baseline (block-end edge) when no natural baseline is available. `FirstBaseline()` returns `nullopt` for orthogonal writing modes, forcing synthesis.

### Fix Location
`pkg/layout/flex_layout.go`:
- Lines 393-543: Add major/minor baseline groups to line cross-size computation
- Lines 876-921: Refactor shared baseline calculation with two groups
- Lines 987-1017: Fix baseline positioning to use correct group
- Lines 1156-1235: Fix container baseline reporting

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -count=1
```

---

## Target 2: Cross-Axis Alignment in Column Flex (~10 tests) — `flex_layout.go`

### Problem
Column flex containers with various `align-self` values (flex-start, flex-end, center, stretch, baseline, inherit) don't position items correctly in the cross (inline) axis, especially with RTL direction.

### Affected Tests (~10 failures)
| Test Pattern | Count | Description |
|---|---|---|
| flexbox-align-self-vert-001/002/003/004 | 4 | Column flex cross-axis alignment |
| flexbox-align-self-vert-rtl-001/002/003/004/005 | 5 | Column flex + direction:rtl |
| flexbox-align-self-horiz-001-table | 1 | Table item cross-axis alignment |

### Root Cause (from code analysis)
In column flex, the cross axis is the inline axis. The reference file `flexbox-align-self-vert-001-ref.xhtml` shows that:
- `flex-start` → float:left (inline-start)
- `flex-end` → float:right (inline-end)
- `center` → margin:auto (centered)
- `baseline` → float:left (falls back to flex-start in column)
- `stretch` → width:100%
- `inherit` → inherits from parent's align-self

The `selfStartIsCrossStart` function (used for self-start/self-end) may have direction/writing-mode interaction issues. Also, cross-axis sizing for items in column flex with explicit `width` may not shrink-wrap correctly.

### What Blink Does
Blink's `ResolvedAlignSelf()` (flex_layout_algorithm.cc ~line 260) resolves alignment values:
- **`flex-start`/`flex-end` are NOT direction-sensitive**. Blink works entirely in logical coordinates; the physical RTL flipping happens later during `LogicalOffset` → `PhysicalOffset` conversion.
- **`start`/`end` map directly to `flex-start`/`flex-end`** — they are NOT direction-aware in flex context. The spec says start/end refer to the cross-axis start/end of the flex container's writing mode, which equals flex-start/flex-end.
- **`self-start`/`self-end`** use `LogicalToLogical` to compare the **child's** writing direction against the **container's**. For column flex (cross = inline), it uses `InlineStart()`/`InlineEnd()`. If child is RTL and container is LTR, `self-start` maps to `flex-end`.
- **`wrap-reverse`** flips `flex-start` ↔ `flex-end` as a final step.
- **`align-self: inherit`** is resolved during style computation (not layout) — the computed value of the parent's `align-self` is inherited.
- **Baseline in column flex**: Blink still computes baseline groups and positions items by ascent/descent even in column flex. The `BaselineAscent()` function works regardless of flex direction.

### Fix Location
`pkg/layout/flex_layout.go`:
- Lines 954-984: Verify `start`/`end` map to `flex-start`/`flex-end` (not direction-aware). Check `self-start`/`self-end` use child vs container WDM comparison.
- Cross-axis sizing: verify items respect their computed inline-size in column flex
- Verify `getAlignSelf` correctly resolves `inherit` to parent's `align-self` value (this should be handled by CSS style resolution, not layout code)

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-horiz-001-table" -count=1
```

---

## Target 3: Aspect Ratio / Transferred Size Suggestion (~6 tests) — `flex_layout.go`

### Problem
The transferred size suggestion for flex items with intrinsic aspect ratios doesn't correctly use "automatic preferred physical width" as a definite value. For column flex with an SVG image that has only a viewBox (no explicit dimensions), the intrinsic width should be treated as definite for computing the transferred size.

### Affected Tests (~6 failures)
| Test | Diff | Description |
|---|---|---|
| aspect-ratio-intrinsic-size-007.html | 54.7% | Transferred size with SVG viewBox-only img |
| aspect-ratio-intrinsic-size-001/002/008/009/010 | 0.2-1.0% | Various aspect ratio scenarios |
| flex-aspect-ratio-img-column-010/012/018 | 2-3% | Column flex with aspect-ratio images |
| flex-aspect-ratio-img-row-007/015 | 2-3% | Row flex with constrained images |
| fit-content-item-001.html | 7.1% | Fit-content sizing for flex items |

### Root Cause (from code analysis)
In `flexItemMinMain()` (line 3338-3443):
- Lines 3351-3361: Only check explicit CSS sizes (`ResolveInlineSize`/`ResolveBlockSize`) for cross-content-size
- For a column flex with `<img src="viewbox-only.svg">`, the SVG has aspect ratio 2:1 but NO intrinsic dimensions (only viewBox)
- Our code skips to the `hasDefiniteCross` container fallback (line 3414) which uses the container's cross-size, not the replaced element's resolved size

The spec says (§4.5): "an automatic preferred physical width is always considered definite whenever computing the transferred size suggestion."

### What Blink Does
**Critical**: Blink's `IsContainerCrossSizeDefinite()` (flex_layout_algorithm.cc ~line 635) **always returns true for column flex**:
```cpp
if (is_column_)
  return true;
```
This implements the spec rule that the automatic preferred physical width is always definite. The cross-size (inline/width) in column flex is treated as definite regardless of whether the container or item has an explicit width.

For SVGs with only viewBox (no explicit width/height), Blink's `ComputeNormalizedNaturalSize()` (length_utils.cc ~line 947) does NOT fall back to 300x150 when an aspect ratio exists — instead it uses the available inline size from the constraint space. The transferred main-axis size = available_inline_size / aspect_ratio.

So for column flex: the container's available inline size becomes the cross-size, and transferred = cross-size / aspect-ratio.

### Fix Location
`pkg/layout/flex_layout.go`:
- The `hasDefiniteCross` flag (used in flex-basis and min-size calculations) should be **always true for column flex**, matching Blink's `IsContainerCrossSizeDefinite()`. Currently it's derived from the container's block-size for row or inline-size for column — but for column flex, the cross axis IS the inline axis, which is always definite.
- Lines 3338-3443: The transferred size suggestion should use the container's inline-size (available cross-size) as the definite cross-size for column flex items, since the "automatic preferred physical width" is always definite.
- Also check `flexBasisMainSize()` (lines 1574-1580) for similar aspect-ratio fallback handling.
- Check whether `hasDefiniteCross` is computed correctly for column flex near the top of `Layout()` — it should be true whenever `contentInlineSize` is definite.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio-intrinsic-size" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/fit-content-item" -count=1
```

---

## Target 4: Row-Reverse and Fragment Ordering (~4 tests) — `flex_layout.go`

### Problem
Row-reverse item ordering may store children in DOM order rather than visual order in the fragment tree, causing incorrect paint ordering.

### Affected Tests (~4 failures)
| Test | Diff | Description |
|---|---|---|
| flexbox_direction-row-reverse.html | 3.7% | Row-reverse ordering with Ahem font |
| flexbox-collapsed-item-horiz-002/003 | 0.3-0.4% | Visibility:collapse in flex |
| flexbox_flex-formatting-interop.html | var | Flex formatting context interop |

### Root Cause
Row-reverse test uses `<ul>` and `<li>` elements with Ahem font. The reference shows letters should appear in reversed order.

### What Blink Does
Blink processes items in document order, then calls `ApplyReversals()` which reverses `item_indices` within each `FlexLine`. **Children are stored in the fragment tree in visual (reversed) order**, not DOM order. This is critical because `paintOrderChildren()` in `paint_layer.go` returns `box.Children` directly — if Children are in DOM order rather than visual order, paint ordering will be wrong.

For `list-style: none`, Blink completely suppresses marker content generation — `ListStyleType()` returns null. There is zero residual space. No special flex-specific suppression is needed.

### Fix Location
`pkg/layout/flex_layout.go`:
- Where the flex algorithm builds the output fragment/box and appends children: verify children are appended in **visual order** (order-modified, then reversed for row-reverse/column-reverse/wrap-reverse), NOT in DOM order.
- Search for where `box.Children` or fragment children are populated — likely near the end of `Layout()` where the `FragmentBuilder` assembles the result.
- `ApplyReversals()` equivalent: after positioning, reverse the child list if `reverseMain` is true.

**This fix also affects Target 5 (paint ordering)** — if children are stored in visual order, both row-reverse AND paint-ordering tests may start passing.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_direction-row-reverse" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-collapsed-item" -count=1
```

---

## Target 5: Paint Ordering (2 tests) — `paint_layer.go` (+ depends on Target 4)

### Problem
Flex items with `order` and `z-index` set don't paint in the correct order. CSS Flexbox §4.3 specifies that flex items paint in order-modified document order (not DOM order), and items with explicit `z-index` create stacking contexts.

### Affected Tests (2 failures)
| Test | Diff | Description |
|---|---|---|
| flexbox-paint-ordering-001.xhtml | 3.5% | Basic paint ordering with z-index on flex items |
| flexbox-paint-ordering-002.xhtml | 2.3% | Complex order+z-index interaction |

### Root Cause (from code analysis)
`pkg/render/paint_layer.go` lines 791-840:
- `paintOrderChildren()` (line 795) returns `box.Children` directly for flex containers
- Lines 827-839: Flex items with explicit z-index create stacking contexts

**Blink research confirms our `paint_layer.go` implementation already matches Blink's structure correctly.** The paint layer code itself is likely fine. The root cause is more likely in **Target 4** — if `box.Children` are stored in DOM order instead of order-modified document order by `flex_layout.go`, then `paintOrderChildren()` returns the wrong order.

Additional checks needed in `paint_layer.go`:
- Verify `HasExplicitZIndex()` correctly returns false for `z-index: auto` (the initial value for flex items) vs true for `z-index: 0`
- Check if negative z-index items paint behind the container's own background/border

### What Blink Does
Blink classifies flex items without explicit z-index as "non-stacked pseudo stacking contexts" — they paint atomically in order-modified document order via FlowChildren. Items WITH explicit z-index go into NegativeZ/AutoZero/PositiveZ z-order lists. The fragment tree already stores children in visual order (after `ApplyReversals()`), so no re-sorting is needed at paint time.

### Fix Location
`pkg/render/paint_layer.go`:
- Lines 795-803: Verify `box.Children` are in order-modified document order (this is a flex_layout.go responsibility — see Target 4)
- Lines 827-840: Verify the z-index stacking logic is correct

**NOTE**: This target likely depends on Target 4's fix (storing children in visual order). If Target 4 is fixed first, these tests may pass without any paint_layer.go changes. Consider testing after Target 4 before making paint_layer.go changes.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-paint-ordering" -count=1
```

---

## Target 6: Float/BFC Interaction (2 tests) — `block_layout.go` ONLY

### Problem
Flex containers establish a new BFC (block formatting context). When preceded by a float, a flex container should either:
1. Be placed beside/below the float (BFC float avoidance) if it doesn't fit
2. Clear the float if `clear: both` is set

### Affected Tests (2 failures)
| Test | Diff | Description |
|---|---|---|
| flexbox_fbfc.html | 5.4% | Flex container (width:80%) should avoid float (width:25%) — can't fit beside it, so goes below |
| flexbox_box-clear.html | 2.8% | Flex container with `clear:both` should position below float |

### Root Cause (from code analysis)
`pkg/layout/block_layout.go` already has BFC float-avoidance code:
- Lines 264-267: `isChildNewFC` correctly identifies flex containers via `createsFormattingContext()`
- Lines 288-320: Pre-layout check pushes BFC child below floats if inline-size doesn't fit
- Lines 385-427: Post-layout check for shrink-to-fit BFC children

The issue may be in how the float exclusion space or clearance offset interacts with BFC children. In `flexbox_fbfc.html`, the flex container has `width: 80%` and the float has `width: 25%` — together they exceed 100%, so the flex must go below. Verify that:
1. The float's margin box is properly recorded in the exclusion space
2. The available inline size check at line 315 correctly accounts for border-box sizing
3. The clear property on the flex container in `flexbox_box-clear.html` is processed at line 238

### What Blink Does
Blink's `HandleNewFormattingContext` in `block_layout_algorithm.cc`:
1. Queries `ExclusionSpace::AllLayoutOpportunities()` for available rectangles
2. Iterates opportunities (sorted by block-start position)
3. Lays out the child in each opportunity until one fits
4. Clear is resolved before `HandleNewFormattingContext` — it adjusts the starting position

### Fix Location
`pkg/layout/block_layout.go`:
- Lines 238-257: Clear property handling
- Lines 288-320: BFC float avoidance (pre-layout check)
- Lines 385-427: BFC float avoidance (post-layout check)
- May also need to check `exclusion_space.go` for correct float margin-box recording

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_fbfc" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox_box-clear" -count=1
```

---

## Independence Check

| | flex_layout.go | paint_layer.go | block_layout.go |
|---|---|---|---|
| Target 1 (Baseline) | **Yes** | - | - |
| Target 2 (Column Align) | **Yes** | - | - |
| Target 3 (Aspect Ratio) | **Yes** | - | - |
| Target 4 (Fragment Order) | **Yes** | - | - |
| Target 5 (Paint Order) | - | **Maybe** | - |
| Target 6 (Float/BFC) | - | - | **Yes** |

**Targets 1-4 ALL touch `flex_layout.go` — they CANNOT be parallelized.**
**Target 5 likely depends on Target 4's fix — test paint ordering AFTER Target 4 is done.**
**Target 6 touches only `block_layout.go` and CAN run as a parallel agent.**

### Recommended Execution Plan
1. Launch Target 6 (block_layout.go) as a parallel worktree agent — it's fully independent
2. Work on Targets 1-4 serially in the main session, in this priority order:
   - Target 3 (Aspect Ratio) — highest single-test pixel diff, Blink research reveals clear fix (`hasDefiniteCross` always true for column flex)
   - Target 4 (Fragment Order) — may fix both row-reverse AND paint-ordering tests
   - Target 1 (Baseline) — most foundational, affects 12+ tests
   - Target 2 (Column Align) — 10 tests, may share root cause with baseline
3. After Target 4, re-test paint ordering — if still failing, investigate `paint_layer.go`

## Tests That Cannot Be Fixed (JS-dependent)
These tests require JavaScript execution and will always fail in a static renderer:
- flex-direction-modify.html (dynamic JS)
- anonymous-flex-item-001.html (DOM manipulation)
- anonymous-flex-item-003.html (DOM manipulation)

## Tests That Cannot Be Fixed (Feature gaps)
These tests require scrollbar width modeling:
- content-height-with-scrollbars.html
- cross-axis-scrollbar.html

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area.
- **Commit and report at each milestone** (don't batch everything to the end).
- **Regression constraint**: After changes, the flex test suite must remain at 529+ passing. Run: `go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 | grep -e "PASS:" -e "FAIL:" | awk '{print $2}' | sort | uniq -c`
- **Target 5**: Do NOT launch as a separate agent — it likely depends on Target 4. Test after Target 4 is done.
- **Target 6 agent**: Only modify `pkg/layout/block_layout.go` and related files (`exclusion_space.go`). Do NOT touch flex_layout.go.
