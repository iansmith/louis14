# Plan: Fix All Tests to Pass at 0.1% Threshold

**Goal**: Change `MaxDifferentPercent` from 0.3 to 0.1 and get ALL tests passing.
**Viewport**: 800×600 = 480,000 pixels. 0.1% = 480 pixels max different.

## Overview

There are **15 tests** that fail at the 0.1% threshold:
- **4 currently failing** (real rendering bugs): grid + media query tests
- **11 borderline** (0.104%–0.139%): mostly text anti-aliasing diffs

---

## Step 0: Change the Threshold

**File**: `pkg/visualtest/reftest_runner_test.go`, line 204

Change:
```go
opts.MaxDifferentPercent = 0.3 // Allow up to 0.3% different pixels
```
To:
```go
opts.MaxDifferentPercent = 0.1 // Allow up to 0.1% different pixels
```

Do this LAST, after fixing all the tests. Run tests at 0.3% during development to avoid noise.

---

## TIER 1: Real Rendering Bugs (4 tests, priority fixes)

### Test 1: `css-grid/fr-unit.html` (1.2% diff, 5551 pixels)

**What the test does**: Grid with `grid-template-columns: 100px 1fr` and `grid-template-rows: 30px 1fr` in a 400×100px container. Cell4 (col 2, row 2) should be 300×70px with green background.

**What we render**: The row heights are wrong. The `1fr` row is only as tall as its text content (~20px) instead of filling the remaining 70px (100 - 30 = 70).

**Root cause**: The grid container has `height: 100%`. In `layoutGridContainer()` (grid.go, line 73):
```go
if h, ok := style.GetLength("height"); ok {
    containerHeight = h
    hasExplicitHeight = true
}
```
`GetLength("height")` calls `ParseLengthFull("100%", ...)`. **`ParseLengthFull` does NOT handle percentage values** — it returns `(0, false)`. So `hasExplicitHeight` stays `false`, and `containerHeight` stays `0`. When `resolveTrackSizes` runs for rows with `hasDefiniteSize=false`, fr tracks don't get distributed.

**Fix** (in `pkg/layout/grid.go`, function `layoutGridContainer`, around lines 70-76):

After the existing `GetLength("height")` check, add a percentage fallback:
```go
// Calculate container content height
var containerHeight float64
hasExplicitHeight := false
if h, ok := style.GetLength("height"); ok {
    containerHeight = h
    hasExplicitHeight = true
} else if pct, ok := style.GetPercentage("height"); ok && pct > 0 {
    // Resolve percentage height against parent's content height
    // The parent must have a definite height for this to work
    if parent != nil && parent.Height > 0 {
        parentContentHeight := parent.Height - parent.Padding.Top - parent.Padding.Bottom -
            parent.Border.Top - parent.Border.Bottom
        containerHeight = pct * parentContentHeight / 100
        hasExplicitHeight = true
    }
}
```

**Why this works**: The grid's parent `#container` has `height: 100px` (explicit). So `parent.Height` = 100. Then 100% of 100 = 100px. The grid gets `containerHeight=100` and `hasExplicitHeight=true`. The `1fr` row resolves to 100 - 30 = 70px.

**Verification**: After fixing, cell4 should be 300×70px green. The reference uses a table with matching dimensions.

---

### Test 2: `css-grid/fr-unit-with-percentage.html` (1.5% diff, 7128 pixels)

**What the test does**: Grid with `grid-template-columns: 1fr 75%` and `grid-template-rows: 1fr 70%` in a 400×100px container. Cell4 should be 300×70px green.

**What we render**: The grid tracks are completely wrong. The percentage tracks (`75%`, `70%`) are silently dropped because `parseGridTracks()` can't parse them.

**Root cause**: In `parseGridTracks()` (style.go, ~line 3490), percentage values like `"75%"` fall through to the final `ParseLength(part)` call, which returns `(0, false)` because `ParseLengthFull` doesn't handle `%`. The track is silently skipped — not added to the tracks array at all.

**Fix** (two parts):

**Part A**: Add a `Percent` field to the `GridTrack` struct (style.go, ~line 3352):
```go
type GridTrack struct {
    Size       float64
    Auto       bool
    Fr         float64
    Percent    float64 // NEW: percentage value (e.g., 75 for "75%")
    IsMinMax   bool
    MinSize    float64
    MaxFr      float64
    MaxSize    float64
    MaxAuto    bool
    MinContent bool
    MaxContent bool
}
```

**Part B**: Handle percentage tokens in `parseGridTracks()` (style.go, ~line 3488). Add this check BEFORE the final `ParseLength` fallback:
```go
} else if strings.HasSuffix(part, "%") {
    numStr := strings.TrimSuffix(part, "%")
    if pct, err := strconv.ParseFloat(numStr, 64); err == nil {
        tracks = append(tracks, GridTrack{Percent: pct})
    }
} else if size, ok := ParseLength(part); ok {
```

**Part C**: Handle percentage tracks in `resolveTrackSizes()` (grid.go, ~line 391). Add a new case in the first pass:
```go
} else if t.Percent > 0 {
    // Percentage track: resolve against container size
    if hasDefiniteSize {
        sizes[i] = t.Percent * containerSize / 100
    }
    usedSpace += sizes[i]
```

This must come BEFORE the `t.Fr > 0` check. Percentage tracks consume space from the container, reducing what's available for fr tracks. Insert it after the `t.IsMinMax` block:

```go
for i, t := range tracks {
    if t.IsMinMax {
        // ... existing minmax handling ...
    } else if t.Percent > 0 {
        // Percentage track: resolve against container size
        if hasDefiniteSize {
            sizes[i] = t.Percent * containerSize / 100
        }
        usedSpace += sizes[i]
    } else if t.Fr > 0 {
        totalFr += t.Fr
    } else if t.Auto || t.MinContent || t.MaxContent {
        // ... existing auto handling ...
    } else {
        // ... existing fixed handling ...
    }
}
```

**Why this works**: `75%` of 400px = 300px for column 2. Remaining = 400 - 300 = 100px for `1fr` column 1. Similarly, `70%` of 100px = 70px for row 2. Remaining = 100 - 70 = 30px for `1fr` row 1.

**Note**: This test also needs the percentage-height fix from Test 1 (the grid's height is `100%`).

---

### Test 3: `css-grid/layout-algorithm/grid-template-flexible-rerun-track-sizing.html` (91.5% diff)

**What the test does**: An `inline-grid` with `minmax(0, .5fr)` columns and rows. Contains a 200×200 item (with 100×100 green background-size) and an abspos red element at z-index:-1.

**Expected**: A 100×100 green square with no red visible. The inline-grid should be sized by its content (200×200). The abspos at 200% = 400×400 should be behind the green at z-index:-1.

**What we render**: A massive red rectangle covering most of the viewport. The grid is 800px wide (full available width) instead of 200px.

**Root cause**: In `layoutGridContainer()` (grid.go, lines 42-51):
```go
if w, ok := style.GetLength("width"); ok {
    containerWidth = w
    hasExplicitWidth = true
} else {
    containerWidth = availableWidth - margin.Left - ...
    hasExplicitWidth = true // <-- BUG: available width acts as definite for grid
}
```
Line 50: `hasExplicitWidth = true` is set even when the grid has NO explicit width. For `display: inline-grid`, the container should be intrinsically sized (shrink-to-fit around content), NOT use the full available width.

With `hasExplicitWidth=true` and `containerWidth=800`, the `minmax(0, .5fr)` track resolves as:
- totalFr = 0.5, remaining = 800
- frSize = 800 / 0.5 = 1600
- track size = 0 + 0.5 * 1600 = 800px
The grid is 800px wide, and the abspos at 200% = 1600px covers the whole viewport red.

**Fix** (in `pkg/layout/grid.go`, lines 42-51):

For `inline-grid`, don't treat available width as definite:
```go
var containerWidth float64
hasExplicitWidth := false
isInlineGrid := style.GetDisplay() == css.DisplayInlineGrid

if w, ok := style.GetLength("width"); ok {
    containerWidth = w
    hasExplicitWidth = true
} else if isInlineGrid {
    // Inline-grid: intrinsically sized, don't use available width as definite
    // We'll determine width from content after Phase 1
    containerWidth = availableWidth // use as upper bound for layout
    hasExplicitWidth = false
} else {
    containerWidth = availableWidth - margin.Left - margin.Right -
        padding.Left - padding.Right - border.Left - border.Right
    hasExplicitWidth = true // block-level grid fills available width
}
```

Then after Phase 2 (resolveTrackSizes), for inline-grid without explicit width, use the actual content width:
```go
resolvedColSizes := resolveTrackSizes(columnTracks, autoColSizes, containerWidth, columnGap, hasExplicitWidth, ...)

// For inline-grid without explicit width, shrink-wrap to content
actualContentWidth := sumTracks(resolvedColSizes, columnGap)
if isInlineGrid && !hasExplicitWidth {
    containerWidth = actualContentWidth
    // Note: might need to re-resolve if any tracks depended on container width
}
```

**Key CSS spec detail**: For `minmax(0, .5fr)` in an intrinsically-sized container, the spec (CSS Grid §7.2.3) says: when the available space is indefinite, tracks with `fr < 1` are sized as `fr * (max-content / fr)`, clamped. For a single `minmax(0, .5fr)` track with a 200px item, the hypothetical fr-size = 200 / 0.5 = 400, track size = min(0.5 * 400, 200) = 200. So the track should be 200px.

**Simpler approach**: Since `hasExplicitWidth = false` for inline-grid, `resolveTrackSizes` won't distribute fr tracks (the `totalFr > 0 && hasDefiniteSize` check fails). The tracks will use their auto sizes from Phase 1. The 200×200 item gives autoColSizes[0] = 200, autoRowSizes[0] = 200. But the track is `minmax(0, .5fr)`, not `auto`, so the auto size won't be used...

**Better fix**: For minmax tracks with fr max in the first pass of resolveTrackSizes, when `hasDefiniteSize` is false, use the auto content size (clamped to min):
```go
if t.IsMinMax && t.MaxFr > 0 {
    if hasDefiniteSize {
        sizes[i] = t.MinSize
        usedSpace += sizes[i]
        totalFr += t.MaxFr
    } else {
        // Indefinite container: use auto content size, clamped to min
        sizes[i] = autoSizes[i]
        if sizes[i] < t.MinSize {
            sizes[i] = t.MinSize
        }
        usedSpace += sizes[i]
    }
}
```
And similarly for plain fr tracks when indefinite:
```go
} else if t.Fr > 0 {
    if hasDefiniteSize {
        totalFr += t.Fr
    } else {
        // Indefinite: fr track uses auto content size
        sizes[i] = autoSizes[i]
        usedSpace += sizes[i]
    }
}
```

Then for inline-grid, set `containerWidth = actualContentWidth` after resolving.

**Files to modify**: `pkg/layout/grid.go` (layoutGridContainer + resolveTrackSizes)

---

### Test 4: `css-media-queries/mq-calc-007.html` (2.6% diff, 12327 pixels)

**What the test does**: Sets `:root { font-size: 30000px }`, then uses `@media (min-width: calc(1px + 1rem))` to make a div green. The `<p>` has explicit `font-size: 16px`.

**Expected**: A 100×100 green square with the test description text.

**What we render**: Completely blank/white page. Nothing visible at all.

**Root cause analysis**: There are likely TWO issues:

**Issue A — Blank page from huge root font-size**: The `:root { font-size: 30000px }` causes `<body>` to inherit `font-size: 30000px`. The body's `line-height: normal` = ~36000px. Whitespace text nodes between block elements in the body (e.g., newline between `</p>` and `<div>`) may be laid out as inline content with this enormous line-height, pushing all visible content far below the 600px viewport.

**Investigation approach for Issue A**:
1. Check how `layoutBlockLevelChildren` (in `layout_block.go`) handles mixed block-element and text-node children
2. Look for whether whitespace-only text nodes between block elements create line boxes
3. The fix is to skip whitespace-only text nodes when they appear as siblings of block-level elements (CSS spec: "A sequence of white space between block-level boxes is collapsed to nothing")
4. **Key location**: `pkg/layout/layout_block.go` — the function that iterates over a node's children and decides whether to do block or inline layout

**Alternative cause for Issue A**: Maybe the `<body>` element itself gets a huge minimum height from its font-size. Check if any code sets body height based on font-size or line-height.

**Quick test**: Create a simplified test without the media query to isolate:
```html
<style>:root { font-size: 30000px; } p { font-size: 16px; }</style>
<p>Hello</p>
<div style="width:100px;height:100px;background:green"></div>
```
Render this with l14open. If it's also blank, the issue is the huge font-size layout, not the media query.

**Issue B — Media query calc evaluation**: The media query `calc(1px + 1rem)` is evaluated by `parseMediaLength` → `EvalCalcWithPercent("1px + 1rem", 16.0, 0)`. In the calc evaluator, `1rem` is parsed by `ParseLengthWithFontSize("1rem", 16.0)` → `ParseLengthFull("1rem", 16.0, 0, 0)` → returns `1 * 16.0 = 16.0` (line 291, hardcoded 16px for rem). So `calc(1px + 1rem) = 17px`. The viewport is 800px > 17px, so the media query SHOULD match. **This part is likely correct**, but verify.

**Fix for Issue A** (most likely fix, in `pkg/layout/layout_block.go`):

Find where block-level layout iterates over children. Before processing a text node child, check if it's whitespace-only AND has block-level siblings. If so, skip it.

```go
// Skip whitespace-only text nodes between block-level elements
if child.Type == html.TextNode && strings.TrimSpace(child.Data) == "" {
    // Check if any sibling is block-level
    hasBlockSibling := false
    for _, sib := range node.Children {
        if sib.Type == html.ElementNode {
            sibStyle := computedStyles[sib]
            if sibStyle != nil && isBlockLevelDisplay(sibStyle.GetDisplay()) {
                hasBlockSibling = true
                break
            }
        }
    }
    if hasBlockSibling {
        continue // skip whitespace between blocks
    }
}
```

**Files to modify**: `pkg/layout/layout_block.go`

---

## TIER 2: Borderline Anti-Aliasing Tests (11 tests, 0.104%–0.139%)

These tests all look visually identical between test and reference. The pixel differences are purely text anti-aliasing at character edges. The diffs range from 499 to 667 pixels (just above the 480-pixel threshold at 0.1%).

### Common characteristics:
- All diffs are in text rendering (description paragraphs)
- The test and reference have slightly different DOM structures for the same text
- Our text engine renders the same characters with slightly different anti-aliasing depending on the surrounding inline boxes, line break positions, etc.

### Approach: Fix the 4 Tier 1 bugs first, then investigate each borderline test

For each, the investigation is:
1. Render both test and reference
2. Generate a diff image to see WHERE the pixels differ
3. If the diff is in the description paragraph text, try one of:
   - **Option A**: Check if the test/ref description text has a different DOM structure and try to make our text rendering more consistent
   - **Option B**: Write a custom reference that matches our rendering (only if the test is our own, NOT upstream WPT)
   - **Option C**: Accept with a slightly higher per-test tolerance (add per-test override)

### Specific tests:

#### CSS2 borderline tests (6):

**5. `floats/float-nowrap-3.html`** — 667 pixels (0.139%)
- Visual: Text "Some text that overflows my parent." looks identical
- Diff: The test has slightly different word spacing between "overflows" and "my" — the gap is wider in the test. This could be a real float-interaction text layout issue (the "nowrap" context may affect word boundaries).
- **Investigate**: Look at how the float's no-wrap context affects text layout. The test may have an element with `white-space: nowrap` that causes different text measurement than the reference's plain text.

**6. `positioning/absolute-non-replaced-height-006.xht`** — 600 pixels (0.125%)
- Visual: Blue rectangle in black-bordered box. Identical layouts.
- Diff: Text anti-aliasing in the description paragraph
- **Likely fix**: Text rendering only — may be hard to fix below threshold

**7. `linebox/inline-box-001.xht`** — 583 pixels (0.121%)
- Visual: "First line" / "Filler Text" (orange) / "Last line" with blue borders. Identical.
- Diff: Text anti-aliasing
- **Likely fix**: Text rendering only

**8. `positioning/absolute-non-replaced-height-003.xht`** — 576 pixels (0.120%)
- Visual: Identical blue rectangle in black box
- Diff: Text anti-aliasing in description
- **Likely fix**: Text rendering only

**9. `text/word-spacing-001.xht`** — 544 pixels (0.113%)
- Visual: Two black squares. The spacing between them differs slightly between test and ref.
- **Investigate**: This may be a real `word-spacing` rendering issue. The test applies `word-spacing` between two inline elements. Check if our word-spacing implementation adds the correct spacing between inline boxes.
- **Key file**: `pkg/layout/layout_inline_multipass.go` — look at how word-spacing is applied between inline items

**10. `box-display/block-in-inline-001.xht`** — 499 pixels (0.104%)
- Visual: Left box missing "Line 1" and "Line 3" text (green background only, text invisible or cut off). Right box correct.
- **Root cause**: Block-in-inline Fragment1/Fragment2 boxes have text that renders at incorrect positions or gets clipped. The split-inline rendering path uses a different code path than normal block layout.
- **Investigate**: In `layout_inline_multipass.go`, check how Fragment1 and Fragment2 boxes position their text content. The text boxes inside fragments may have wrong Y coordinates.
- **Key file**: `pkg/layout/layout_inline_multipass.go` (around lines 1600-1700, Fragment1/Fragment2 creation)

#### CSS3 borderline tests (5):

**11. `css-outline/outline-basic-001.html`** — 630 pixels (0.131%)
- Visual: Green square with blue outline. Identical.
- Diff: Text anti-aliasing in description paragraph
- **Likely fix**: Text rendering only

**12. `css-floats-clear/clear-003.xht`** — 576 pixels (0.120%)
- Diff: Text anti-aliasing
- **Likely fix**: Text rendering only

**13. `css-text-indent/text-indent-percentage-001.xht`** — 544 pixels (0.113%)
- Diff: Text anti-aliasing
- **Likely fix**: Text rendering only

**14. `css-vertical-align-length/vertical-align-length-001.html`** — 544 pixels (0.113%)
- Diff: Text anti-aliasing
- **Likely fix**: Text rendering only

**15. `css-outline/outline-style-012.html`** — 520 pixels (0.108%)
- Diff: Text anti-aliasing
- **Likely fix**: Text rendering only

---

## Strategy for Tier 2 Anti-Aliasing Tests

Most of the borderline tests (6, 7, 8, 11, 12, 13, 14, 15) have diffs purely from text anti-aliasing in description paragraphs that appear in both test and reference. The descriptions often contain `<strong>` tags and other inline elements that cause slightly different text rendering paths.

### Root cause analysis: Why does the same text render differently?

The test and reference have the SAME description text (e.g., "Test passes if there is a green square") but in slightly different DOM structures. Common differences:
- Test has `<strong>no red</strong>`, reference might have `<b>no red</b>` or `<em>text</em>`
- Test has additional HTML structure that affects text line breaking
- Different namespace handling (XHTML vs HTML5) may produce different text nodes

Our text engine measures and renders text per-inline-box. When the inline box structure differs slightly (e.g., "no " + "red" as two boxes vs. "no red" as one box), the sub-pixel accumulation during glyph positioning creates slightly different anti-aliased pixel values.

### Potential fix: Improve text rendering consistency

**Location**: `pkg/render/render.go`, function `drawText()`

The text rendering uses the `gg` library which applies anti-aliasing. Differences arise when:
1. Text is split at different positions into different boxes
2. The starting X coordinate has different fractional values
3. Font kerning differs between runs

**Potential approach**: Snap text X coordinates to integer values before rendering. This would reduce sub-pixel anti-aliasing differences. In `drawText()`, round the X coordinate:
```go
x := math.Round(box.X + offsetX)
y := math.Round(box.Y + offsetY + baseline)
```

If X/Y coordinates are already being rounded, the issue may be in how different inline box structures accumulate width. Look at `ConstructFragments` in `layout_inline_multipass.go` for sub-pixel width accumulation.

### Fallback: Per-test tolerance

If some borderline tests can't be fixed below 0.1%, the test runner can support per-test tolerance overrides. In `reftest_runner_test.go`, add a map:
```go
var perTestTolerance = map[string]float64{
    "floats/float-nowrap-3.html": 0.15,
    // etc.
}
```

But this should be a last resort — try to fix the rendering first.

---

## Execution Order

1. **Fix Test 2 first** (fr-unit-with-percentage) — requires adding `Percent` field to GridTrack
2. **Fix Test 1** (fr-unit) — requires percentage height resolution in grid.go
3. **Fix Test 3** (grid-template-flexible) — requires inline-grid intrinsic sizing
4. **Fix Test 4** (mq-calc-007) — requires investigating blank page from huge font-size
5. **Run tests at 0.3%** — verify Tier 1 tests now pass
6. **Investigate Tier 2** — render diff images, identify fixable issues
7. **Fix word-spacing-001** and **block-in-inline-001** if real bugs found
8. **Reduce anti-aliasing diffs** — try integer coordinate snapping
9. **Change threshold to 0.1%** — final step
10. **Run full test suite** — verify all pass

---

## Files Summary

| File | Changes |
|------|---------|
| `pkg/css/style.go` | Add `Percent` field to `GridTrack` struct; handle `%` in `parseGridTracks()` |
| `pkg/layout/grid.go` | Handle percentage tracks in `resolveTrackSizes()`; resolve percentage height in `layoutGridContainer()`; inline-grid intrinsic sizing |
| `pkg/layout/layout_block.go` | Skip whitespace text nodes between block-level elements (for mq-calc-007) |
| `pkg/render/render.go` | (Maybe) Integer snap text coordinates to reduce anti-aliasing diffs |
| `pkg/layout/layout_inline_multipass.go` | (Maybe) Fix block-in-inline Fragment text positioning; word-spacing |
| `pkg/visualtest/reftest_runner_test.go` | Change `MaxDifferentPercent` from 0.3 to 0.1 |

---

## Testing Commands

```bash
# Run all CSS2 tests
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go clean -testcache && \
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ -run TestCSSRefTests -v -timeout 600s

# Run all CSS3 tests
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go clean -testcache && \
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ -run TestCSS3RefTests -v -timeout 600s

# Render a specific test to inspect visually
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go run ./cmd/l14open <test.html> /tmp/test.png 800 600
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go run ./cmd/l14open <ref.html> /tmp/ref.png 800 600

# Quick compile check
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go build ./...
```

## Critical Warnings

1. **NEVER use fuzzy matching** (FuzzyRadius must stay 0) — per CLAUDE.md policy
2. **Test at 0.3% first** before lowering threshold — avoid chasing anti-aliasing while fixing real bugs
3. **Don't break existing passing tests** — run the full suite after every change
4. **Grid changes are high-risk** — the resolveTrackSizes function is used by ALL grid tests. Adding percentage support could break existing grid tests if not done carefully. Always run the full suite.
5. **The block-in-inline Fragment1/Fragment2 code is fragile** — previous sessions needed multiple fixes here. Be very careful with changes in layout_inline_multipass.go around lines 1600-1700.
