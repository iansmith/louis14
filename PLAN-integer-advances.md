# Plan: Integer Pixel Quantization of Character Advance Widths

## Motivation

CSS reftest comparisons between table-based layout and block-based layout fail with
~344px of sub-pixel anti-aliasing noise even though the correct color/content is
rendered. The root cause: our engine accumulates **fractional character advance widths**
throughout the layout and rendering pipeline.

Real browsers (Blink, WebKit, Gecko) quantize each character advance to an integer
pixel value via font hinting / grid fitting. With integer advances everywhere:

- `advance("Filler")` = integer N
- Column width measured from "Filler" = N (exact, no rounding needed)
- Cell 2 starts at integer N
- Reference "Filler Text" continuous string: T is at N + advance(" ") = N + M (integer)
- Test cell " Text" separate string: T is at N + advance(" ") = N + M (integer)
- **They match exactly. Zero pixel diff.**

Without integer quantization, `advance("Filler")` = 31.283. The reference DrawString
call accumulates through "Filler " with freetype's internal fractional position state,
while the test starts a fresh DrawString call at 31.283. Freetype's sub-pixel hinting
differs between the two paths, producing slightly different coverage patterns. The
diffs are tiny (~0.07%) but non-zero.

## Current Data Flow

```
MeasureTextWithStyle("Filler", 16, ...) → 31.283 (float64)
    ↓
InlineItem.Width = 31.283
    ↓
currentX += 31.283  (LayoutInlineContentToBoxes)
    ↓
box.X = 31.283  (fractional)
    ↓
DrawString("Filler", 31.283, baseline)  (render.go:2291)
    ↓
freetype renders with sub-pixel coverage at fractional boundary
```

The same fractional value propagates to:
- Table column width measurement (measureTextContentRecursive)
- Cell X positions (positionTableCells: currentX += columnWidths[i])
- Text box X positions (currentX in LayoutInlineContentToBoxes)
- DrawString X coordinates in drawText

## The Fix

Round the return value of `MeasureTextWithStyle` (and `MeasureTextWithWeight`) to the
nearest integer pixel. This is the single source of truth — every consumer automatically
gets integer values, and all downstream accumulation stays on the integer grid.

**File:** `pkg/text/measure.go`

The base `MeasureText` function (which both public functions call) should apply the
rounding. Alternatively, apply it in each public function.

### Width: `math.Round(w)`

Rounds to nearest integer. Correct choice because:
- `math.Ceil` always rounds up → wider boxes → unnecessary line breaks
- `math.Floor` always rounds down → narrower boxes → words that should wrap don't
- `math.Round` minimizes error (max 0.5px off per character)

### Height: `math.Round(h)`

Also round height. Line box heights should be integer pixels for clean vertical
alignment. With fractional height, inline element baselines may land at fractional Y
coordinates, causing the same anti-aliasing issue in the vertical direction.

Exception: do not allow rounded height to be less than 1px (prevent collapsing).

### Minimum: 1px

If a glyph rounds to 0px wide (extremely small font sizes), clamp to 1px to avoid
zero-width items that could cause divide-by-zero or infinite loops.

## Code Change

In `pkg/text/measure.go`, locate the `MeasureText` function (the base implementation
called by both public functions) and apply rounding before return:

```go
// Before (current):
func MeasureText(s string, fontSize float64, ...) (float64, float64) {
    // ... setup ...
    w, h := dc.MeasureString(s)
    return w, h
}

// After:
func MeasureText(s string, fontSize float64, ...) (float64, float64) {
    // ... setup ...
    w, h := dc.MeasureString(s)
    w = math.Round(w)
    if w < 1 && len(s) > 0 {
        w = 1
    }
    h = math.Round(h)
    if h < 1 {
        h = 1
    }
    return w, h
}
```

If the rounding is applied in the base `MeasureText` function, the two wrapper
functions `MeasureTextWithStyle` and `MeasureTextWithWeight` inherit the fix
automatically with no further changes.

## Cascade of Effects

Once MeasureTextWithStyle returns integer widths:

| Location | Effect |
|---|---|
| `InlineItem.Width` | Always integer |
| `currentX` in LayoutInlineContentToBoxes | Always integer after each step |
| `box.X` for text boxes | Always integer |
| `textX` in drawText | Always integer → DrawString at integer X |
| `drawX` in letter-spacing loop | Integer + integer advances → stays integer |
| `measureCellContentWidth` | Returns integer → column widths already integer |
| `positionTableCells` currentX | Integer (no column-width snapping needed) |
| Tab stop positions | Integer |
| Float positions (box.X from layout) | Integer |

The column-width snapping workaround attempted in the previous session becomes
unnecessary — column widths are already integers since they come from MeasureTextWithStyle.

## Expected Test Impact

### Fixes:
- `color-applies-to-001/002`: 344px → 0px (table cell vs div text X alignment)
- Any other tests where table/block layout compares text at the same logical position
- Potentially several of the 200px "background" and "positioning" tests if they involve
  text near fractional boundaries

### Potential Regressions:
- Tests that currently render at 0px diff may shift by ±1px if a specific fractional
  advance was the "right" answer for some carefully crafted reference
- Ahem font tests: Ahem advance widths at common sizes (16px, 20px, etc.) are already
  near-integer, so rounding should have minimal effect
- Proportional font tests: these are the ones most affected by the change

### Strategy:
1. Apply the change to `pkg/text/measure.go`
2. Run `go test ./pkg/visualtest/...` with verbose output
3. Compare before/after: new tests at 0px (wins) vs tests that regressed (new non-zero diffs)
4. For any regressions: render the test+ref pair, inspect the shift
5. If a regression is a 1px shift at a boundary, update the reference to match the new
   (correct, integer) rendering
6. Goal: all 426 tests (CSS2 + CSS3) still passing at ≤0.1% threshold, with color-applies-to at 0px

## Risks and Mitigations

**Risk:** Some test that relies on sub-pixel text positioning breaks.
**Mitigation:** The test suite catches regressions immediately. Any test that was passing
at 0px diff before and now has N px diff after needs investigation. If N ≤ 2px and it's
clearly a ±0.5px rounding shift (not a rendering correctness issue), the reference can
be updated to match the integer-snapped rendering.

**Risk:** Rounding `h` causes line heights to change, shifting all text in the file by 1px.
**Mitigation:** Inspect the actual `h` value returned by gg for common fonts/sizes. If
`h` is already very close to an integer (e.g., 19.2 → 19), the visual difference is
minimal. Run tests and inspect any Y-axis regressions.

**Risk:** `math.Round` of `w` for a multi-character string is not the same as summing
`math.Round` of each individual character.
**Note:** This is correct and expected. We measure words as complete strings (not
character-by-character), so `MeasureText("Filler", ...)` returns the advance of the
whole word, which we round once. This is the same as what a browser does with hinted
fonts — the word advance is the sum of hinted per-character advances, but we can
approximate it by rounding the word advance directly.

**Risk:** Very short strings or single characters round to unexpected values.
**Mitigation:** The 1px minimum prevents collapse. Single character rounding is
predictable (e.g., an 8px-wide 'A' stays 8px).

## Files to Modify

1. **`pkg/text/measure.go`** — Apply `math.Round` to width and height in the base
   `MeasureText` function (or equivalently in both `MeasureTextWithStyle` and
   `MeasureTextWithWeight`). Add `"math"` import if not already present.

That is the only file that needs to change. No other files require modification.

## Verification

```bash
# Before change: note color-applies-to diff counts
go test ./pkg/visualtest/... -v 2>&1 | grep "color-applies-to"

# Apply change to pkg/text/measure.go

# After change: confirm color-applies-to → 0px, all tests still pass
go clean -testcache && go test ./pkg/visualtest/... 2>&1 | tail -2
go test ./pkg/visualtest/... -v 2>&1 | grep -E "different: [1-9]" | wc -l
```

Success criteria:
- All 426 tests still PASS (no new FAILs)
- `color-applies-to-001/002` diff: 344px → 0px (or near-0)
- Total count of non-zero-diff tests does not increase
