# Fix: Multi-Pass Line-Height Calculation Bug in inline-box-001.xht

## Executive Summary

The `inline-box-001.xht` test fails with 1.9% error (3070/160000 pixels) because the multi-pass inline layout calculates the wrong Y position for block children of inline elements. The orange div is rendered at Y=63.2 instead of Y=70.4 - a 7.2px error caused by using text content height (12.0px) instead of line-height (19.2px).

**Key Finding**: This is NOT a duplicate layout problem. Each DOM node is laid out exactly once. The issue is a line-height calculation bug in the multi-pass inline layout code.

## Investigation Results

### Test vs Reference Rendering

**Test file (WRONG)**:
```
Step 3: Blocks:
  - <body> at (0.0, 0.0)
  - <p> at (0.0, 16.0)
  - <div> at (0.0, 63.2) bg=orange ❌ WRONG Y POSITION
```

**Reference file (CORRECT)**:
```
Step 3: Blocks:
  - <body> at (0.0, 0.0)
  - <p> at (0.0, 16.0)
  - <div> at (0.0, 70.4) bg=orange ✓ CORRECT Y POSITION
```

**Difference**: 7.2px = 19.2px (line-height) - 12.0px (text content height)

### Layout Cache Investigation

To verify if duplicate layouts were occurring, I implemented a layout cache that maps DOM nodes to their boxes. Results:

**Test file layout**:
```
LAYOUT CACHE STORE: Cached box for <p id='no-id'> at (0.0, 16.0)
LAYOUT CACHE STORE: Cached box for <div id='no-id'> at (0.0, 63.2)  ← Orange div, ONCE
LAYOUT CACHE STORE: Cached box for <body id='no-id'> at (0.0, 0.0)
LAYOUT CACHE STORE: Cached box for <html id='no-id'> at (0.0, 0.0)
```

**Conclusion**: The orange div is laid out **exactly once** at Y=63.2. No duplicate layouts occur.

### Multi-Pass Invocations

Multi-pass inline layout (`LayoutInlineContentToBoxes`) is invoked 4 times in the test file:
1. `<body>` - has block children and inline children
2. `<p>` - has inline children only
3. Unknown element
4. `<div>` - the inline div processing

Only ONE of these invocations processes the inline `div#div1`, and it creates the orange div at Y=63.2 (wrong).

## Root Cause Analysis

### The Test Case Structure

```html
<p>Test passes if...</p>
<div id="div1" style="display: inline; border: 2px solid blue;">
    First line
    <div>Filler Text</div>  <!-- Orange div, should be at Y=70.4 -->
    Last line
</div>
```

**Expected rendering**:
- Fragment 1: "First line" with left/top/bottom borders at Y=51.2
- Orange div: "Filler Text" at Y=70.4 (51.2 + 19.2 line-height = 70.4)
- Fragment 2: "Last line" with right/top/bottom borders at Y=89.6

**Actual rendering**:
- Fragment 1: "First line" at Y=51.2 ✓
- Orange div: "Filler Text" at Y=63.2 ❌ (51.2 + 12.0 text height = 63.2)
- Fragment 2: "Last line" at Y=89.6 ✓

### The Bug

When multi-pass inline layout encounters the block child (orange div), it finalizes the current line and advances Y:

```go
// Line 746-751 in layout_inline_multipass.go (approximate)
if currentLineMaxHeight > 0 {
    fmt.Printf("  Finalizing current line before block: currentY %.1f, height %.1f\n",
        currentY, currentLineMaxHeight)
    currentY = currentY + currentLineMaxHeight  // ← BUG: Uses 12.0 instead of 19.2!
}
```

The `currentLineMaxHeight` is tracking text content height (12.0px) instead of the inline element's line-height (19.2px).

### Why Line-Height is Wrong

When processing inline content, `currentLineMaxHeight` should track the maximum of:
1. Text fragment heights (font-size + line-height)
2. Inline box line-heights (from `line-height` CSS property)
3. Atomic inline heights (images, inline-blocks)

Currently, it only tracks text content height, missing the inline element's line-height contribution.

### CSS Specification

CSS 2.1 §10.8.1:
> "The height of the line box is the distance from the top of the highest box to the bottom of the lowest box."

For the inline `div#div1`:
- Font size: ~12px (default)
- Line-height: 19.2px (1.6 ratio, browser default)
- The line box containing "First line" should be 19.2px tall, not 12.0px

## Solution

### Fix currentLineMaxHeight Tracking

**File**: `pkg/layout/layout_inline_multipass.go`

#### Step 1: Track inline element line-heights at OpenTag

When an OpenTag is encountered, update `currentLineMaxHeight` with the element's line-height:

```go
// Around line 838 (in OpenTag processing block)
if isOpenTag {
    span := &inlineSpan{
        node:  frag.Node,
        style: frag.Style,
    }
    inlineStack = append(inlineStack, span)

    // NEW: Track inline element's contribution to line height
    if frag.Style != nil {
        lineHeight := frag.Style.GetLineHeight()
        if lineHeight > currentLineMaxHeight {
            currentLineMaxHeight = lineHeight
            fmt.Printf("  OpenTag <%s>: Updated currentLineMaxHeight to %.1f (line-height)\n",
                frag.Node.TagName, lineHeight)
        }
    }
}
```

#### Step 2: Restore inline stack line-heights after line breaks

When a line break occurs (new Y position detected), restore contributions from active inline elements:

```go
// Around line 900 (in line break detection)
if frag.Position.Y != currentLineY {
    // ... existing line finalization code ...
    currentLineMaxHeight = 0

    // NEW: Restore line-height contributions from inline stack
    for _, span := range inlineStack {
        if span.style != nil {
            spanLineHeight := span.style.GetLineHeight()
            if spanLineHeight > currentLineMaxHeight {
                currentLineMaxHeight = spanLineHeight
                fmt.Printf("  Line break: Restored line-height %.1f from inline stack <%s>\n",
                    spanLineHeight, span.node.TagName)
            }
        }
    }
}
```

### Why This Works

1. **OpenTag contribution**: When `<div id="div1">` is opened, its line-height (19.2px) is recorded
2. **Line advancement**: When the block child is encountered, currentY advances by 19.2px instead of 12.0px
3. **Correct positioning**: Orange div positioned at Y = 51.2 + 19.2 = 70.4 ✓

## Implementation Plan

### Phase 1: Add Line-Height Tracking to OpenTag

**Priority**: HIGH
**Effort**: 30 minutes
**Risk**: Low

1. Find OpenTag processing in `layout_inline_multipass.go` (around line 838)
2. Add line-height tracking after `inlineStack = append(...)`
3. Add debug output to verify line-height is captured

**Expected result**: Debug output shows "Updated currentLineMaxHeight to 19.2"

### Phase 2: Test Basic Case

**Priority**: HIGH
**Effort**: 10 minutes

```bash
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v
```

**Expected**: Orange div at Y=70.4 instead of Y=63.2

**If still wrong**: Proceed to Phase 3

### Phase 3: Add Line-Height Restoration After Line Breaks

**Priority**: MEDIUM
**Effort**: 20 minutes
**Risk**: Low

Only needed if Phase 2 doesn't fully fix the issue. This handles cases where line breaks occur within inline elements.

### Phase 4: Validate Full Test Suite

**Priority**: HIGH
**Effort**: 5 minutes

```bash
go test ./pkg/visualtest -run "TestWPTReftests" -v
```

**Expected**: 35+/51 tests passing (up from 34/51), no regressions

## Expected Outcomes

### Immediate (Phase 1-2)

- ✅ Orange div positioned at Y=70.4 (correct)
- ✅ `inline-box-001.xht` error: 1.9% → <0.3% (PASS)
- ✅ Test suite: 34/51 → 35/51 passing

### Additional Benefits

- ✅ Correct line-height handling for all inline elements
- ✅ Foundation for complex inline layouts (nested inlines, mixed content)
- ✅ CSS spec compliance for line box height calculation

## Testing Strategy

### Test 1: inline-box-001.xht (Primary)

**Before**:
```
DEBUG DRAW: Drawing <div> at (0.0,63.2) size 192.0x19.2 bg=orange
REFTEST FAIL: 3070/160000 pixels differ (1.9%)
```

**After Phase 1**:
```
DEBUG DRAW: Drawing <div> at (0.0,70.4) size 192.0x19.2 bg=orange
REFTEST PASS
```

**Validation**:
- [ ] Orange div at Y=70.4
- [ ] Fragment 1 (borders) at Y=51.2
- [ ] Fragment 2 (borders) at Y=89.6
- [ ] No pixel differences (<0.3%)

### Test 2: Debug Output

Check that line-height is correctly tracked:

```bash
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v 2>&1 | grep "line-height\|currentLineMaxHeight"
```

**Expected**:
```
OpenTag <div>: Updated currentLineMaxHeight to 19.2 (line-height)
Finalizing current line before block: currentY 51.2, height 19.2
```

### Test 3: Full Test Suite

```bash
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | tail -20
```

**Expected**: No regressions, 35+/51 passing

## Debugging Tips

### If orange div still at wrong Y

1. **Check debug output**: Verify line-height is being captured
   ```bash
   grep "OpenTag.*div.*line-height" test-output.txt
   ```

2. **Check line finalization**: Verify correct height is used
   ```bash
   grep "Finalizing current line before block" test-output.txt
   ```

3. **Inspect currentLineMaxHeight**: Add more debug output to trace value changes

### If test regresses

1. **Check line-height values**: Ensure we're not double-counting
2. **Verify line break logic**: Make sure restoration doesn't duplicate heights
3. **Test simpler cases**: Create minimal test with just inline + block child

## Code Locations

### File: `pkg/layout/layout_inline_multipass.go`

**OpenTag processing** (around line 838):
- Currently: Creates inlineSpan and pushes to stack
- Add: Track line-height contribution to currentLineMaxHeight

**Block child handling** (around line 746):
- Currently: Finalizes line with currentLineMaxHeight
- Already correct: Uses currentLineMaxHeight for Y advancement

**Line break detection** (around line 900):
- Currently: Resets currentLineMaxHeight to 0
- Add: Restore line-heights from inline stack

## Alternative Approaches Considered

### ❌ Layout Cache (Attempted)

**Why rejected**: Investigation showed no duplicate layouts occur. The cache prevented legitimate re-layouts but didn't fix the Y position calculation.

**Evidence**: With cache disabled, each node still laid out exactly once.

### ❌ Modify Y after layout

**Why rejected**: Adjusting positions after layout is fragile and doesn't address root cause. The line-height should be correct from the start.

### ❌ Use fragment heights

**Why rejected**: Fragments track text content height (12.0px), not line box height (19.2px). We need the CSS line-height property.

## Success Criteria

### Must Have

- [ ] OpenTag line-height tracking implemented
- [ ] `inline-box-001.xht` passes (<0.3% error)
- [ ] Orange div at Y=70.4 (correct position)
- [ ] No regressions (34+/51 tests passing)

### Nice to Have

- [ ] Line break restoration implemented (Phase 3)
- [ ] Comprehensive debug output for line-height tracking
- [ ] Documentation of line box height algorithm

### Metrics

**Before**:
- inline-box-001.xht: 1.9% error (3070 pixels)
- Orange div Y: 63.2 (wrong by 7.2px)
- Pass rate: 34/51 (66.7%)

**After**:
- inline-box-001.xht: <0.3% error (PASS)
- Orange div Y: 70.4 (correct)
- Pass rate: 35+/51 (68.6%+)

## Lessons Learned

### Investigation Process

1. **Verify assumptions**: The original diagnostic assumed duplicate layouts, but investigation proved this wrong
2. **Use instrumentation**: Layout cache experiment revealed the true behavior
3. **Compare test vs reference**: Showed same code produces different results, pointing to input-dependent bug

### Root Cause Analysis

1. **Look for magnitude clues**: 7.2px difference = 19.2 - 12.0 immediately suggested line-height vs content height
2. **Check CSS specs**: Confirmed that line boxes should use line-height, not text content height
3. **Trace the flow**: Identified exact location where height is used for Y advancement

### Fix Strategy

1. **Minimal change**: Add line-height tracking at OpenTag (5 lines of code)
2. **Incremental testing**: Test after each phase before adding more complexity
3. **Debug output**: Add logging to verify behavior without relying on final rendering

## References

- CSS 2.1 §10.8.1: Line height calculations
- CSS 2.1 §9.2.1.1: Anonymous inline boxes
- Chromium LayoutNG: Line box height algorithm
- Firefox nsLineLayout: Line height tracking

## Appendix: Test HTML

```html
<!DOCTYPE html>
<html>
<head>
    <style>
        #div1 {
            border: 2px solid blue;
            display: inline;
        }
        div div {
            background: orange;
            width: 2in;
        }
    </style>
</head>
<body>
    <p>Test passes if there are blue borders around the top, left and bottom but not the right side of the text "First line", and borders around the top, right, bottom but not the left of the text "Last line".</p>
    <div id="div1">
        First line
        <div>Filler Text</div>
        Last line
    </div>
</body>
</html>
```

## Appendix: Investigation Timeline

1. **Assumption**: Duplicate layouts causing duplicate boxes
2. **Implementation**: Added layout cache to prevent duplicates
3. **Result**: No cache hits - no duplicates were occurring!
4. **Discovery**: Orange div only laid out once, but at wrong position
5. **Analysis**: Y=63.2 vs Y=70.4 → 7.2px = line-height discrepancy
6. **Root Cause**: Multi-pass using text content height instead of line-height
7. **Solution**: Track line-height at OpenTag processing

**Time spent**: ~90 minutes investigation, 30 minutes for fix (estimated)
**Key insight**: Question your assumptions - verify with data!
