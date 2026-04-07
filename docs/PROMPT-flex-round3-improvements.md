# Flex Round 3: Top 6 Improvements

Current state: 500 pass / 129 fail (79.5% pass rate) across 629 flexbox WPT tests.

These six targets address the highest-impact root causes in the flex layout engine.
**Four can run in parallel in Wave 1; the remaining two in Wave 2.**

---

## Target 1: Replaced Element Flex Sizing in Row Flex (~18 tests)

### Problem
Replaced elements (img, canvas, video, iframe, textarea) render at their full
intrinsic size inside row flex containers instead of being sized by flex
distribution. Images that should be ~75px wide appear at 200+px.

### Visual Evidence
`flexbox-basic-img-horiz-001.xhtml` (22.2% diff): Test shows large green
rectangles filling most of the page. Reference shows compact blue bars with
correct flex-distributed widths. The images are at intrinsic size instead of
the flex-basis computed from `flex: 5` / `flex: 3`.

### Affected Tests (~18 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| flexbox-basic-img-* | 3 | 22.2% |
| flexbox-basic-canvas-* | 5 | 9.2% |
| flexbox-basic-video-* | 2 | 9.2% |
| flexbox-basic-iframe-* | 2 | 9.2% |
| flexbox-basic-textarea-* | 2 | 10.4% |
| flexbox-basic-block-*v | 2 | 3.4% |
| flexbox-basic-fieldset-* | 2 | 0.1% |

### Root Cause (from code analysis)
In `pkg/layout/flex_layout.go`, `resolveFlexBasis()` (lines 1363-1420):

When `flex-basis: auto` with no explicit CSS width, the code falls through
to `itemMaxContentMainSize()` (line 1419), which calls
`ComputeMinMaxSizes()` in `min_max_sizing.go`. For replaced elements, this
returns the intrinsic width (e.g., 200px for an image). This becomes the
flex-basis.

But the tests use `flex: 5` which means `flex-grow:5; flex-shrink:1;
flex-basis:auto`. With `auto` basis, the spec says to use the item's
`width` property if set; otherwise use the content size. For replaced
elements without explicit width, the content size IS the intrinsic size.

The REAL problem is that these tests set `min-width: 0` on the flex items,
which should allow them to shrink below their intrinsic size. But with a
200px flex-basis and only 196px of container space, flex distribution gives
each item its proportional share. The test expects items to grow FROM ZERO
(flex-basis: 0 when `flex: N`), not from intrinsic size.

Wait -- `flex: 5` is shorthand for `flex: 5 1 0%`. The flex-basis is 0%,
NOT auto. The parsing of `flex: N` shorthand needs verification.

**Check first**: Read `pkg/css/style.go` and verify how `flex: 5` is parsed.
If `flex: 5` correctly sets flex-basis to 0%, then the bug is in
`resolveFlexBasis()` not handling `0%` correctly for replaced elements. If
`flex: 5` incorrectly sets flex-basis to `auto`, that's the parsing bug.

### What Blink Does
In Blink's `ComputeFlexBasisForChild()`:
- `flex: N` shorthand sets flex-basis to 0% (CSS spec requirement)
- 0% resolves to 0px when the container has a definite inline size
- Items then grow from 0 proportionally via flex-grow

### Fix Location
1. **First**: Verify flex shorthand parsing in `pkg/css/style.go` for
   `flex: N` -- should produce `flex-basis: 0%`
2. **Then**: In `resolveFlexBasis()` (flex_layout.go:1363-1420), verify
   that 0% basis resolves to 0px, not to intrinsic size
3. Check `clampMainSizeWithMin()` (line 1633) for interaction with
   min-width:0

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-img" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-canvas" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-textarea" -count=1
```

---

## Target 2: Aspect-Ratio Transferred Size Suggestion (~13 tests)

### Problem
In column flex containers, replaced elements (especially SVGs with viewBox)
render at their full intrinsic height instead of having their height
constrained by the transferred size suggestion from their definite width.

### Visual Evidence
`aspect-ratio-intrinsic-size-007.html` (57.2% diff): Test shows a green
rectangle filling ~60% of the viewport. Reference shows a thin green bar at
the top (~120px tall). The SVG (viewBox 1000x500, aspect ratio 2:1) should
be constrained by its width.

### Affected Tests (~13 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| aspect-ratio-intrinsic-size-* | 4 | 57.2% |
| flex-aspect-ratio-img-column-* | 4 | 3.3% |
| flex-aspect-ratio-img-row-* | 5 | 2.6% |

### Root Cause (from code analysis)
In `pkg/layout/flex_layout.go`, `flexItemMinMain()` (lines 2679-2777):

The transferred size suggestion requires a definite cross-size to compute
the main-size via aspect ratio. Lines 2708-2720 only check for:
1. Explicit CSS cross-size on the item (lines 2698-2706)
2. Container's definite cross-size with stretch (lines 2710-2720)

**Missing**: Per CSS Sizing-3 Section 4.8.2, a replaced element's automatic
preferred physical width/height is ALWAYS considered definite for the
purpose of transferred size suggestion. The code never falls back to the
intrinsic cross-size of the replaced element itself.

For `aspect-ratio-intrinsic-size-007`: column flex, cross=inline. The SVG
has intrinsic width 1000px (from viewBox). This width should be treated as
definite for transferred size, giving height = 1000/2 = 500px. But the code
skips transferred entirely because `crossContentSize` stays at -1.

Similarly in `resolveFlexBasis()` (lines 1394-1408): the aspect-ratio
fallback only checks CSS `aspect-ratio` property, NOT intrinsic aspect ratio
of replaced elements.

### What Blink Does
Blink's `FlexLayoutAlgorithm::ComputeFlexBasisForChild()` treats replaced
elements' intrinsic sizes as definite for transferred size computation.
The automatic preferred size (from intrinsic dimensions) always counts as
"definite" when computing the transferred size suggestion.

### Fix Location
1. `pkg/layout/flex_layout.go` lines 2708-2720: After the existing
   `hasDefiniteCross` check, add a fallback for replaced elements:
   ```go
   // If no explicit cross-size and container isn't definite,
   // but it's a replaced element, use intrinsic cross-size as always-definite.
   if crossContentSize < 0 && isReplaced {
       info := GetIntrinsicSizingInfo(child, fla.ctx)
       if mainIsItemInline {
           crossContentSize = info.IntrinsicHeight
       } else {
           crossContentSize = info.IntrinsicWidth
       }
   }
   ```
2. Same fix at lines 2758-2768 (CSS aspect-ratio section).
3. Consider similar fix in `resolveFlexBasis()` lines 1394-1408.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio-intrinsic" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -count=1
```

---

## Target 3: align-self Cross-Axis Positioning (~13 tests)

### Problem
Items with various `align-self` values (flex-end, center, self-start,
self-end) are positioned at slightly wrong cross-axis offsets in flex
containers with margins, borders, and padding on items.

### Visual Evidence
`flexbox-align-self-horiz-002.xhtml` (18.7% diff): Items have correct
alignment direction but are shifted by a few pixels. The reference uses
floated divs with `position: relative; top: Npx` to achieve exact
positioning. Our output has subtle offset errors across all items.

### Affected Tests (~13 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| flexbox-align-self-horiz-* | 3 | 18.7% |
| flexbox-align-self-vert-* | 4 | 11.7% |
| flexbox-align-self-vert-rtl-* | 5 | 10.6% |
| align-self-016 | 1 | 4.2% |

### Root Cause (investigation needed)
The cross-axis alignment code is in `pkg/layout/flex_layout.go` lines
826-897. The builder adds `crossMarginStart()` at line 942:
```go
blockOff = item.crossOffset + item.crossMarginStart()
```

The convention is that `crossOffset` is the margin-box start position
(line 847 comment: "crossOffset stores the position BEFORE crossMarginStart
is added by the builder"). Algebraically the flex-end/center/etc formulas
appear correct for margin-box positioning.

**Investigation needed**: The 18.7% diff is too large for just rounding
errors. The agent should:
1. Add debug logging to print actual cross offsets vs expected
2. Compare with reference values (e.g., flex-end `top: 172px`, center
   `top: 86px` from the reference HTML)
3. Check if the `stretch` case is computing the wrong height (line 2869,
   `stretchFlexItems()`) -- stretched items affect line cross-size
4. Check if `align-items: stretch` (default) is being applied when it
   shouldn't be (items with explicit height should NOT stretch)
5. Check if dotted border rendering affects border-box size calculations

The test uses items with asymmetric margins (1px/3px), borders (2px/4px),
and padding (3px/5px). The cross-size calculation must handle these
correctly.

### What Blink Does
Blink computes cross-axis alignment in
`FlexLayoutAlgorithm::ApplyAlignItems()`. The key is that free space is
computed as `line_cross_size - child_outer_cross_size` where outer includes
margins. The offset is then added to the margin-start, giving the
border-box position.

### Fix Location
- `pkg/layout/flex_layout.go` lines 826-897 (alignment switch)
- Possibly `stretchFlexItems()` lines 2869-2951
- Possibly `hasExplicitCrossSize()` line 2837

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-horiz-002" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-vert-001" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/align-self-016" -count=1
```

---

## Target 4: Baseline Synthesis for Flex Items (~16 tests)

### Problem
Baseline-aligned flex items fall back to flex-start positioning instead of
actually aligning on their text baselines. Items with `align-self: baseline`
should shift down so their first-line baselines align, but they all sit at
the top of the flex line.

### Visual Evidence
`flexbox-align-self-baseline-horiz-001a.xhtml` (5.3% diff): Test shows
small items ("blk_1line", "blk_2lines", "supersub") aligned at the top of
the container. Reference shows them shifted down so their text baselines
align with the bottom of the big cyan "3lines" text.

### Affected Tests (~16 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| flexbox-align-self-baseline-horiz-* | 6 | 9.9% |
| flexbox-baseline-multi-line-* | 3 | 0.4% |
| flexbox-baseline-multi-item-* | 4 | 0.1% |
| baseline-synthesis-* | 2 | 1.4% |
| flexbox-baseline-align-self-baseline-* | 1 | 0.5% |

### Root Cause (from code analysis)
In `pkg/layout/flex_layout.go`, the baseline participation check at multiple
locations requires `item.baseline > 0`:

**Line 455** (cross-size determination):
```go
if selfAlign == "baseline" && item.baseline > 0 && baselineParallel {
```

**Line 800** (shared baseline computation):
```go
if selfAlign == "baseline" && item.baseline > 0 && baselineParallel {
```

**Line 871** (baseline alignment positioning):
```go
if hasBaselineItem && item.baseline > 0 {
```

**Line 1047** (container baseline):
```go
if selfAlign == "baseline" && item.baseline > 0 && baselineParallel {
```

The check `item.baseline > 0` prevents items with zero baseline from
participating. But per CSS Flexbox Section 9.4:
- Block elements with no inline content have baseline = 0 from the layout
  engine
- Replaced elements have no text baseline (baseline = 0)
- These items should still participate via **baseline synthesis** (the
  baseline is synthesized at the block-end edge of the border-box)

**Additionally**: `block_layout.go` (around line 566-577) only sets the
baseline when `firstLineAscent > 0`. And `replaced_layout.go` never calls
SetBaseline at all. So replaced elements and empty blocks always have
baseline = 0.

### What Blink Does
Blink synthesizes baselines per CSS Inline Layout Module Level 3:
- If no first-line baseline exists, synthesize at the block-end edge
- For replaced elements: synthesize at the bottom margin edge
- The check is on baseline *participation* (alignment value), not baseline
  *value*

### Fix Location
1. **`pkg/layout/flex_layout.go`**: Change all `item.baseline > 0` checks
   to allow zero baselines. Add synthesis logic: when baseline == 0 and
   the item participates in baseline alignment, synthesize baseline =
   item.crossSize (bottom of border-box).
   - Lines 455, 800, 871, 1047: Remove `> 0` requirement or add synthesis
2. **`pkg/layout/block_layout.go`**: Around line 577, add baseline
   synthesis when `firstLineAscent == 0` -- set baseline to the block-end
   edge of the content box.
3. **`pkg/layout/replaced_layout.go`**: In `Layout()` (line 160+), call
   `SetBaseline()` with the bottom of the replaced element's border-box.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/baseline-synthesis" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -count=1
```

---

## Target 5: justify-content Positioning with Margins/Borders (~8 tests)

### Problem
Items are positioned at slightly wrong main-axis offsets in flex containers,
especially when items have margins, borders, and padding. The overall
structure is correct but positions are off by small amounts.

### Visual Evidence
`flexbox-justify-content-horiz-002.xhtml` (11.3% diff): Both test and
reference show the same general layout (rows of colored items), but item
positions and possibly sizes differ subtly across all 24 flex containers
on the page.

### Affected Tests (~8 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| flexbox-justify-content-horiz-* | 4 | 13.1% |
| justify-content-* | 4 | 2.1% |

### Root Cause (investigation needed)
The `computeItemMainOffsets()` function (lines 2060-2179) appears correct
on paper. The agent investigating justify-content found no obvious bugs in
the distribution algorithm.

**Investigation needed**: The agent should:
1. Check if `totalItemSize` (line 2082) correctly accounts for borders and
   padding on items. The items use `flex: 0 10px` meaning flex-basis is
   10px. Is this a content-box or border-box value? Check how flex-basis
   interacts with `box-sizing`.
2. Check if `mainMarginStart()` and `mainMarginEnd()` return the correct
   values for items with asymmetric margins.
3. Compare actual computed positions (add debug prints) with reference
   positions (the reference uses `position: relative; left: Npx`).
4. Check `justify-content: left` and `justify-content: right` keyword
   resolution (lines 690-726).
5. Run the simple tests (justify-content-002/004/005) which have only 100
   pixels different -- these are easier to debug.

### What Blink Does
Blink's `DistributeItemsInMainAxis()` computes:
```
free_space = content_main_size - sum(item_outer_main_sizes)
```
Where `item_outer_main_size = margin + border + padding + content_size`.
Flex-basis is always a content-box value unless box-sizing: border-box
applies to the flex item.

### Fix Location
- `pkg/layout/flex_layout.go` lines 2060-2179 (`computeItemMainOffsets`)
- Possibly `resolveFlexBasis()` lines 1342-1534 if the basis-to-size
  conversion has a box-model error

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-justify-content" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/justify-content" -count=1
```

---

## Target 6: min-width:auto Transferred Size for Replaced Elements (~13 tests)

### Problem
Replaced elements in zero-width flex containers are rendered too large
because the automatic minimum size (min-width: auto) doesn't properly
compute the transferred size suggestion from the element's aspect ratio.

### Visual Evidence
`flexbox-min-width-auto-002b.html` (5.9% diff): Test shows three large
navy-blue squares (~100px each). Reference shows three tiny navy squares
(~30px each). The images have `min-height: 30px` which should constrain
their width via aspect ratio, but min-width:auto isn't using the transferred
size to limit the width.

### Affected Tests (~13 failures)
| Pattern | Count | Max Diff |
|---------|-------|----------|
| flex-minimum-width-flex-items-* | 3 | 6.2% |
| flexbox-min-width-auto-* | 3 | 5.9% |
| flex-item-min-width-* | 1 | 2.1% |
| flex-minimum-height-flex-items-* | 3 | 2.1% |
| flex-item-transferred-sizes-* | 2 | 1.9% |
| flex-item-max-height-* | 1 | 2.1% |

### Root Cause (from code analysis)
In `pkg/layout/flex_layout.go`, `flexItemMinMain()` (lines 2572-2831):

**Bug 1** (lines 2708-2720): The transferred size suggestion for replaced
elements only activates when the container has a definite cross-size
(`hasDefiniteCross`). For a zero-width row flex container, the cross-axis
is block (vertical). If the container has no explicit height, transferred
size is never computed.

But the item has `min-height: 30px`. The explicit min-cross-size should
be usable as the cross constraint for transferred size computation. The
code at line 2698 tries to get explicit cross-size but only checks
`height`/`width`, NOT `min-height`/`min-width`.

**Bug 2** (lines 2632-2638): When computing content suggestion for block
main axis, `PercentageResolutionSize.BlockSize` is NOT set in the
constraint space. This means percentage heights in descendants resolve
against 0/indefinite, which is wrong when the flex item has an explicit
height.

### What Blink Does
Blink's `ComputeMinSizeAutoForFlexItem()`:
- Computes the transferred size suggestion by checking both explicit size
  AND explicit min-size on the cross axis
- Uses the min-cross-size as a lower bound for the cross constraint
- Properly sets percentage resolution sizes when computing content suggestion

### Fix Location
1. `pkg/layout/flex_layout.go` lines 2698-2720: When computing
   `crossContentSize` for transferred suggestion, also check min-cross-size:
   ```go
   // Check explicit cross min-size as fallback for transferred suggestion
   if crossContentSize < 0 {
       minCross := resolveMinCrossSize(style, childWDM, space, childGeom)
       if minCross > 0 {
           crossContentSize = minCross
       }
   }
   ```
2. Lines 2632-2638: Set `PercentageResolutionSize.BlockSize` to the item's
   resolved block-size when computing content suggestion.

### Verification
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flexbox-min-width-auto" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum-width" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox/flex-item-transferred" -count=1
```

---

## Parallelism Analysis

### File Overlap Matrix

| Target | flex_layout.go lines | Other files |
|--------|---------------------|-------------|
| 1: Replaced flex-basis | 1342-1534 | min_max_sizing.go, css/style.go |
| 2: Aspect-ratio transferred | 2679-2777 | intrinsic_sizing.go |
| 3: align-self positioning | 826-897, 2869-2951 | — |
| 4: Baseline synthesis | 380-557, 785-888, 1032-1066 | block_layout.go, replaced_layout.go |
| 5: justify-content | 2060-2179 | — |
| 6: min-width:auto | 2572-2638 | — |

### Conflict Pairs
- **Targets 3 & 4 CONFLICT**: Both touch lines 785-888 (cross-axis alignment with baseline)
- **Targets 2 & 6 CONFLICT**: Both touch flexItemMinMain() function (2572-2777)

### Recommended Execution

**Wave 1 (4 parallel agents):**
- Agent A: Target 1 (Replaced flex-basis) — lines 1342-1534
- Agent B: Target 3 (align-self positioning) — lines 826-897
- Agent C: Target 5 (justify-content) — lines 2060-2179
- Agent D: Target 2 (Aspect-ratio transferred) — lines 2679-2777

**Wave 2 (2 parallel agents, after Wave 1 merges):**
- Agent E: Target 4 (Baseline synthesis) — lines 380-888, 1032-1066 + block_layout.go + replaced_layout.go
- Agent F: Target 6 (min-width:auto) — lines 2572-2638

Wave 2 agents don't conflict with each other; they only conflict with
Wave 1 agents in their respective regions.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Search
  chromium.googlesource.com for `flex_layout_algorithm.cc` and the relevant
  section.
- **Commit and report at each milestone** (don't batch everything to the end).
- **Run regression checks**: After fixing target tests, run the FULL flex
  test suite to check for regressions:
  ```bash
  cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-flexbox" -count=1 2>&1 | grep -c "PASS:"
  ```
  Current baseline: 500 passing. Do not reduce this number.
- **Never use `open` to display files** -- it disrupts the user's screen.
- **When running in a worktree**, commit ONLY to your worktree branch.
  Never commit directly to fix/* or master branches from a worktree.
- **Add debug logging temporarily** to verify your fix, then remove it
  before final commit.
- For targets that say "investigation needed", spend time understanding the
  bug BEFORE writing code. Generate debug images, compare pixel positions,
  read the CSS spec sections referenced in the test files.
