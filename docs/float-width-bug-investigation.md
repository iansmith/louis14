# Float Width Calculation Bug - Investigation & Fix

## Current Status: Bug Identified, Ready to Fix

### Test Failure
- **Test**: `inline-formatting-context-002.xht`
- **Status**: 51/54 WPT CSS2 tests passing (2.0% pixel diff on this test)
- **Root Cause**: Float width miscalculation in REFERENCE rendering, not test rendering

### Safari Verification (CRITICAL)
Safari renders both test and reference identically with the black stripe immediately after the paragraph. This confirms:
- ✅ Our **test** rendering at Y=51.2 is CORRECT
- ❌ Our **reference** rendering at Y=151.2 is WRONG (should be 51.2)

### The Bug

**File**: `pkg/layout/layout_block.go` lines 1505-1511
**Function**: Float positioning in Phase 5

The floated div (with `padding-left: 100px`) has its width calculated incorrectly:
- **Actual calculation**: `floatWidth = 238px` (box.Width=138px + padding.Left=100px)
- **Should be**: `floatWidth = 169px` (content=69px + padding.Left=100px)
- **Problem**: Content width is being DOUBLED from 69px to 138px

This causes `getFloatDropY()` to think the float doesn't fit (238px > 169px available), so it drops the float by incrementing Y by 1px on each attempt until hitting maxAttempts (100), resulting in final Y=151.2 instead of Y=51.2.

### Debug Output Added (Still in Code)

**File**: `pkg/layout/layout_block.go` line ~1506
```go
fmt.Printf("DEBUG FLOAT PHASE5: floatTotalWidth=%.1f (box.Width=%.1f, padding.L=%.1f...)\n", ...)
```

**File**: `pkg/layout/floats.go` line ~165
```go
fmt.Printf("DEBUG getFloatDropY: startY=%.1f, floatWidth=%.1f, availableWidth=%.1f\n", ...)
```

### Test HTML Files
Created for Safari verification:
- `/tmp/inline-test.html` - nested inline divs with margin-left
- `/tmp/inline-ref.html` - floated div with padding-left

Both render identically in Safari at the correct position.

## How to Reproduce the Bug

```bash
go build -o /tmp/l14test cmd/l14open/main.go
/tmp/l14test pkg/visualtest/testdata/wpt-css2/linebox/inline-formatting-context-002-ref.xht /tmp/ref-debug.png 2>&1 | grep "DEBUG FLOAT"
```

Expected output showing the bug:
```
DEBUG FLOAT-Y [div]: initial y=51.2 (after margin.Top=0.0), padding={T:0.0 R:0.0 B:0.0 L:100.0}
DEBUG FLOAT PHASE5: box.Y before getFloatDropY = 51.2
DEBUG FLOAT PHASE5: floatTotalWidth=238.0 (box.Width=138.0, padding.L=100.0, padding.R=0.0...)
DEBUG getFloatDropY: startY=51.2, floatWidth=238.0, availableWidth=169.0
DEBUG FLOAT PHASE5: floatY from getFloatDropY = 151.2
```

## Where to Fix

The bug is in **shrink-to-fit width calculation for floats**.

### Investigation Path

1. **Find where `box.Width` is set for floats**
   - Look in `pkg/layout/layout_block.go` around Phase 3-4 (auto-height/width calculation)
   - The float div undergoes multi-pass inline layout which returns with correct content
   - But then `box.Width` ends up as 138px instead of 69px

2. **Check the inline layout result**
   - The debug shows: `DEBUG: Returning InlineLayoutResult: currentY=51.2, currentLineMaxH=19.2, lastFinalizedH=0.0, finalH=19.2, boxes=2`
   - The span inside has width 69px, wrapper box also 69px
   - Need to check how this gets converted to `box.Width` for the float container

3. **Likely culprit**: Lines 1300-1400 in `layout_block.go`
   - Auto-width calculation for shrink-to-fit contexts (floats)
   - May be adding content width + wrapper width instead of just content width
   - Or may be adding padding twice

### Key Code Locations

**Float positioning**: `pkg/layout/layout_block.go:1501-1529`
```go
if floatType != css.FloatNone && position == css.PositionStatic {
    floatTotalWidth := le.getTotalWidth(box)  // Returns 238px (WRONG)
    floatY = le.getFloatDropY(floatType, floatTotalWidth, box.Y, availableWidth)
    box.Y = floatY  // Ends up at 151.2 instead of 51.2
```

**Float drop logic**: `pkg/layout/floats.go:164-200`
```go
func (le *LayoutEngine) getFloatDropY(...) float64 {
    // Tries to fit floatWidth (238px) in availableWidth (169px)
    // Fails, increments Y by 1px per attempt × 100 attempts
    // Returns ~151.2
}
```

**Width calculation**: Search for where `box.Width` is set after inline layout
- Likely in `layout_block.go` around line 800-1400
- Look for shrink-to-fit, auto-width, or float-specific width logic

## Fix Strategy

1. Find where `box.Width=138px` is being set for the float div
2. Determine why it's 138px instead of 69px (content width)
3. Possible causes:
   - Content width (69px) + padding (69px)? NO - padding is 100px
   - Content width × 2? Maybe counting wrapper + content?
   - MaxX - MinX calculation including padding twice?
4. Fix the calculation to properly compute content-only width
5. Verify `getTotalWidth()` then adds padding/borders/margins correctly

## Testing the Fix

```bash
# Build
go build -o /tmp/l14test cmd/l14open/main.go

# Test reference (should show Y=51.2 everywhere)
/tmp/l14test pkg/visualtest/testdata/wpt-css2/linebox/inline-formatting-context-002-ref.xht /tmp/ref-fixed.png 2>&1 | grep -E "box.Width|floatTotalWidth|floatY"

# Run full test suite
go test -v ./pkg/visualtest -run "TestWPTReftests$" 2>&1 | tail -20

# Should show 52/54 or better (one more passing)
```

## Other Context

### Unrelated Changes Made
- Added Y adjustment tracking for block children in `constructLineBoxesWithRetry` (lines 2580-2583, 2820-2835)
  - This was for a different hypothesized issue
  - Turned out the test rendering was already correct
  - These changes don't affect the float bug but are harmless

### Note on Test Structure
- Test uses: nested inline `<div>`s with `margin-left: 100px`
- Reference uses: floated `<div>` with `padding-left: 100px`
- Both should produce identical output per CSS spec
- Our test rendering is correct, reference is wrong due to float width bug

## Clean Up After Fix

Once fixed, remove debug Printf statements from:
- `pkg/layout/layout_block.go:1506-1508`
- `pkg/layout/floats.go:165-175`

## Success Criteria

✅ Float positioned at Y=51.2 (matching Safari)
✅ `floatTotalWidth=169.0` (not 238.0)
✅ `box.Width=69.0` (not 138.0)
✅ Test passes: 52/54 WPT CSS2 tests
✅ Reference and test images match (0% diff)
