# Root Cause Analysis: inline-box-001.xht Line-Height Bug

## Part A: Why Reference File Works (Y=70.4 ✓)

### Reference HTML Structure

```html
<p>Test passes if...</p>
<div><span id="top">First line</span></div>     ← Block div with inline span
<div id="middle">Filler Text</div>              ← Orange div
<div><span id="bottom">Last line</span></div>   ← Block div with inline span
```

**Key**: Three separate BLOCK divs, not one inline div containing a block.

### Reference Layout Flow

```
1. Paragraph:
   - Y = 0.0
   - Height = 19.2 (line-height)
   - Total with margins = 51.2 (16 + 19.2 + 16)
   - Advances currentY: 0.0 → 51.2

2. First div (<div><span>First line</span></div>):
   - Y = 51.2 (starts after paragraph)
   - Uses NORMAL BLOCK LAYOUT
   - Content height = 19.2 (span's line-height applies to div's content)
   - Advances currentY: 51.2 → 70.4 ✓

3. Orange div (middle):
   - Y = 70.4 ✓ CORRECT
   - Starts immediately after first div
```

**Why it works**: The first div is a block element containing an inline span. Normal block layout calculates the div's content height as 19.2px (the line-height of its inline content).

---

## Part A: Why Test File Fails (Y=63.2 ❌)

### Test HTML Structure

```html
<p>Test passes if...</p>
<div id="div1" style="display: inline; border: 2px solid blue;">
    First line
    <div>Filler Text</div>  ← Block child INSIDE inline parent
    Last line
</div>
```

**Key**: One INLINE div containing a block div (block-in-inline).

### Test Layout Flow

```
1. Paragraph:
   - Y = 0.0
   - Height = 19.2
   - Total with margins = 51.2
   - Advances currentY: 0.0 → 51.2

2. Inline div with "First line" text:
   - Y = 51.2
   - Uses MULTI-PASS INLINE LAYOUT
   - TEXT(" First line ") processed
   - Line finalized with height = 12.0 ❌ (text content height, NOT line-height!)
   - Advances currentY: 51.2 + 12.0 = 63.2 ❌

3. Orange div (block child of inline div):
   - Y = 63.2 ❌ WRONG (should be 70.4)
   - Started 7.2px too early (19.2 - 12.0 = 7.2)
```

**Why it fails**: Multi-pass inline layout finalizes the line containing "First line" using text content height (12.0px) instead of the inline div's line-height (19.2px).

---

## The Smoking Gun

### Debug Output Comparison

**Reference file** (correct):
```
[Fragment 1] 0: TEXT("First line")
  [Fragment 1] TotalHeight: 19.2 ← Uses line-height
  Advancing currentY: 51.2 → 70.4 ✓
```

**Test file** (wrong):
```
[Fragment 2] 0: TEXT(" First line ")
  Finalizing current line before block: currentY 51.2, height 12.0 ← Uses text height!
  [Fragment 3] Orange div starts at currentY=63.2 ❌
```

### The Bug Location

File: `pkg/layout/layout_inline_multipass.go`, around line 746:

```go
if currentLineMaxHeight > 0 {
    fmt.Printf("  Finalizing current line before block: currentY %.1f, height %.1f\n",
        currentY, currentLineMaxHeight)
    currentY = currentY + currentLineMaxHeight  // Uses currentLineMaxHeight = 12.0 ❌
}
```

**Problem**: `currentLineMaxHeight` is 12.0 (text content height) instead of 19.2 (inline element's line-height).

---

## Part B: Multi-Pass Y Positioning Architecture

### Overview

Multi-pass inline layout tracks vertical position through several variables:

1. **`currentY`** - Absolute Y position in the document (tracked incrementally)
2. **`currentLineY`** - Y position of the current line being processed
3. **`currentLineMaxHeight`** - Maximum height of content on the current line
4. **`lastFinalizedLineHeight`** - Height of the last finalized line

### The Y Advancement Algorithm

```
For each fragment:
  1. Check if fragment is on a new line (frag.Position.Y != currentLineY)
     - If yes: Advance currentY by previous line's height
               currentY = currentLineY + currentLineMaxHeight
               Reset currentLineMaxHeight = 0

  2. Process fragment:
     - TEXT: Set currentLineMaxHeight = max(current, text.Height)
     - OpenTag: ??? (Currently does nothing)
     - BlockChild: Finalize current line first, then layout block

  3. Advance to next fragment
```

### The Line-Height Problem

#### What Should Happen

When an inline element (like `<div style="display:inline">`) opens:

1. **OpenTag** should contribute its line-height to `currentLineMaxHeight`
2. **TEXT** content inside should use the larger of:
   - Its own content height (12.0px)
   - The parent inline's line-height (19.2px)
3. **BlockChild** finalization should use the full line-height (19.2px)

CSS 2.1 §10.8.1:
> "The height of a line box is the distance from the top of the highest box to the bottom of the lowest box."

For an inline box with `line-height: 19.2px` containing text with `font-size: 12px`:
- Line box height = 19.2px (the inline element's line-height)
- NOT 12.0px (the text content height)

#### What Actually Happens

```
Timeline of currentLineMaxHeight:

1. [OpenTag <div>]
   - No effect on currentLineMaxHeight
   - currentLineMaxHeight = 0

2. [TEXT "First line"]
   - Sets currentLineMaxHeight = text.Height = 12.0
   - Overwrites any potential line-height contribution

3. [BlockChild encountered]
   - Finalizes line with currentLineMaxHeight = 12.0 ❌
   - Advances currentY by 12.0 instead of 19.2
```

### Why My Fix Failed

#### Attempt 1: Set currentLineMaxHeight at OpenTag

```go
// At OpenTag (line ~838)
if frag.Style != nil {
    lineHeight := frag.Style.GetLineHeight()
    if lineHeight > currentLineMaxHeight {
        currentLineMaxHeight = lineHeight  // Set to 19.2
    }
}
```

**Problem**: This sets currentLineMaxHeight=19.2, but then:

1. **Line break detected** (from paragraph to inline div)
   - Advances currentY using the 19.2: `currentY = 51.2 + 19.2 = 70.4`
   - Resets `currentLineMaxHeight = 0`

2. **TEXT processed**
   - Sets `currentLineMaxHeight = 12.0`

3. **BlockChild finalization**
   - Uses `currentLineMaxHeight = 12.0`
   - Advances: `currentY = 70.4 + 12.0 = 82.4` ❌

**Result**: Orange div at Y=82.4 (12.4px too high) - WORSE than original!

#### Attempt 2: Restore line-height after line breaks

```go
// After resetting currentLineMaxHeight = 0
for _, span := range inlineStack {
    if span.style != nil {
        spanLineHeight := span.style.GetLineHeight()
        if spanLineHeight > currentLineMaxHeight {
            currentLineMaxHeight = spanLineHeight  // Restore to 19.2
        }
    }
}
```

**Problem**: This restored currentLineMaxHeight=19.2 after the line break, but:

1. **Line break** advances by 19.2: `currentY = 70.4`
2. **Restoration** sets `currentLineMaxHeight = 19.2`
3. **TEXT** sets `currentLineMaxHeight = 12.0` ❌ (TEXT overwrites it!)
4. **BlockChild** uses 12.0 again

**Result**: Same as Attempt 1 - no improvement.

---

## The Real Problem: OpenTag vs. TEXT Timing

### Core Issue

The multi-pass code processes fragments in this order:

```
1. OpenTag <div>     ← Should set line-height
2. TEXT "First..."   ← Overwrites with content height
3. BlockChild        ← Uses whatever currentLineMaxHeight is
```

**Conflict**: OpenTag sets line-height (19.2), but TEXT immediately overwrites it (12.0).

### Why This Happens

Looking at the TEXT processing code (around line 1159-1161):

```go
fmt.Printf("  Box.Height=%.1f, currentLineMaxHeight before=%.1f\n",
    box.Height, currentLineMaxHeight)
if box.Height > currentLineMaxHeight {
    currentLineMaxHeight = box.Height  // Overwrites if bigger
}
```

The check `if box.Height > currentLineMaxHeight` should prevent overwriting:
- If `currentLineMaxHeight = 19.2` (from OpenTag)
- And `box.Height = 12.0` (TEXT content)
- Then `12.0 > 19.2` is FALSE, so it shouldn't overwrite

**But the debug shows**:
```
Box.Height=12.0, currentLineMaxHeight before=0.0
```

So `currentLineMaxHeight = 0.0` when TEXT is processed, NOT 19.2!

### Why currentLineMaxHeight is 0.0

Between OpenTag and TEXT, there's a **line break detection**:

```go
// Line break detected: Y 51.2 → 0.0 (height 19.2)
if frag.Position.Y != currentLineY {
    // Advance past previous line
    currentY = currentLineY + currentLineMaxHeight  // Uses 19.2
    currentLineY = frag.Position.Y
    currentLineMaxHeight = 0  // ← RESET TO ZERO
}
```

So the flow is:
1. OpenTag sets `currentLineMaxHeight = 19.2` ✓
2. Line break detected, resets `currentLineMaxHeight = 0` ❌
3. TEXT sees `currentLineMaxHeight = 0`, sets it to 12.0
4. BlockChild uses 12.0 ❌

---

## The Line Break Mystery

### Why Does Line Break Detect a Change?

Debug output shows:
```
[Fragment 1] OpenTag: <div>
  Position: (0.0, 0.0), CurrentX: 0.0

[Fragment 2] 0: TEXT(" First line ")
  Line break detected: Y 51.2 → 0.0 (height 19.2)
```

The TEXT fragment has `Position.Y = 0.0`, which differs from `currentLineY = 51.2`, triggering line break detection.

**Question**: Why is `frag.Position.Y = 0.0`?

### Fragment Y Positions from Line Breaking Phase

The line breaking phase (Phase 2) creates fragments with Y positions, but these are **relative to the start of the inline content**, not absolute document positions.

From `ConstructFragmentsFromItems()` (around line 558):
```go
frag := &Fragment{
    Type:     FragmentText,
    Position: Position{X: leftOffset, Y: line.Y},  // line.Y from line breaking
    Size:     Size{Width: textWidth, Height: textHeight},
}
```

The `line.Y` is calculated during line breaking, which doesn't know:
- Where the parent block starts in the document
- The heights of previous elements

So fragments start with Y=0 for the first line, and the multi-pass phase is supposed to correct them to absolute positions using `currentY`.

### The Correction Mechanism

Around line 1163-1168:
```go
// CRITICAL FIX: Use currentY instead of frag.Position.Y
// After block children, frag.Position.Y is wrong because BreakLines
// doesn't know block heights. We track actual Y in currentY.
if box.Y != currentY {
    fmt.Printf("  ⚠️  Correcting Y: %.1f → %.1f (currentY)\n", box.Y, currentY)
    box.Y = currentY
}
```

This corrects fragment Y positions from relative to absolute.

**But**: The line break detection (line 1137) runs BEFORE this correction, comparing uncorrected fragment Y with currentLineY.

---

## Architecture Analysis Summary

### The Multi-Pass Y Tracking Flow

```
Phase 1: Line Breaking
├─> Creates fragments with RELATIVE Y positions (line.Y)
├─> Doesn't know parent block's Y position
└─> Doesn't know previous blocks' heights

Phase 2: Fragment Processing (LayoutInlineContentToBoxes)
├─> Tracks ABSOLUTE Y position in currentY
├─> Detects line breaks by comparing frag.Position.Y with currentLineY
│   └─> Problem: Comparing relative vs absolute positions!
├─> Advances currentY based on currentLineMaxHeight
├─> Corrects fragment Y positions to absolute (currentY)
└─> Returns boxes with correct absolute positions
```

### The Line-Height Tracking Flow

```
Current Implementation:
OpenTag → (does nothing)
TEXT → Sets currentLineMaxHeight = text.Height
BlockChild → Uses currentLineMaxHeight (wrong!)

Desired Implementation:
OpenTag → Sets currentLineMaxHeight = line-height
TEXT → Keeps max(current, text.Height)
BlockChild → Uses currentLineMaxHeight (correct!)

But: Line break detection resets currentLineMaxHeight between OpenTag and TEXT
```

### The Vicious Cycle

1. OpenTag sets `currentLineMaxHeight = 19.2` ✓
2. Line break detected because `frag.Position.Y (0.0) != currentLineY (51.2)`
3. Line break handler:
   - Advances `currentY` using 19.2: `51.2 + 19.2 = 70.4`
   - Resets `currentLineMaxHeight = 0`
4. TEXT sets `currentLineMaxHeight = 12.0`
5. BlockChild advances using 12.0: `70.4 + 12.0 = 82.4` ❌

**The cycle**: Any line-height set at OpenTag gets used for PREVIOUS line advancement, then reset, never applying to the CURRENT line.

---

## Why This is Hard to Fix

### Constraint 1: Line Break Detection

Line break detection compares fragment Y positions (relative, from line breaking) with currentLineY (absolute, tracked in multi-pass).

This mismatch is unavoidable because:
- Line breaking phase doesn't know absolute positions
- Multi-pass phase needs to detect when fragments move to new lines

### Constraint 2: Line-Height Timing

Line-height needs to be known BEFORE line is finalized, but:
- OpenTag appears before TEXT on the same line
- Line break detection happens between OpenTag and TEXT
- Line break resets currentLineMaxHeight

### Constraint 3: Multiple Sources of Height

`currentLineMaxHeight` must track the maximum of:
- Inline element line-heights (e.g., 19.2px from `<div style="display:inline">`)
- Text content heights (e.g., 12.0px from "First line")
- Atomic inline heights (images, inline-blocks)

The current code only tracks TEXT heights, missing inline element contributions.

---

## Potential Solutions

### Solution 1: Don't Reset Line-Height at Line Breaks (Complex)

**Idea**: Only reset `currentLineMaxHeight` if there are no open inline elements.

```go
if frag.Position.Y != currentLineY {
    currentY = currentLineY + currentLineMaxHeight
    currentLineY = frag.Position.Y

    // Don't reset if inline elements are still open
    if len(inlineStack) == 0 {
        currentLineMaxHeight = 0
    } else {
        // Keep highest line-height from open inlines
        currentLineMaxHeight = 0
        for _, span := range inlineStack {
            if span.style != nil {
                lineHeight := span.style.GetLineHeight()
                if lineHeight > currentLineMaxHeight {
                    currentLineMaxHeight = lineHeight
                }
            }
        }
    }
}
```

**Problem**: This still advances currentY by 19.2 during line break (step above), then keeps currentLineMaxHeight=19.2, so BlockChild would advance AGAIN by 19.2, giving Y=89.6 (same issue as my Attempt 2).

### Solution 2: Track Line-Height Separately (Medium)

**Idea**: Track inline element line-heights separately from content heights.

```go
type LineHeightTracker struct {
    contentMaxHeight float64  // From text, images, etc.
    lineBoxMinHeight float64  // From inline element line-heights
}

// Effective height is the maximum
effectiveHeight := max(contentMaxHeight, lineBoxMinHeight)
```

**Pros**: Clear separation of concerns
**Cons**: Requires refactoring all currentLineMaxHeight usage

### Solution 3: Two-Pass Within Multi-Pass (Complex)

**Idea**: First pass collects line-heights from all fragments on a line, second pass applies them.

**Pros**: Correct handling of all cases
**Cons**: Significant architectural change, may need retry logic

### Solution 4: Fix Fragment Y Positions in Line Breaking (Major)

**Idea**: Make line breaking phase output absolute Y positions instead of relative.

**Pros**: Eliminates the relative/absolute mismatch
**Cons**: Line breaking phase would need to know parent block Y and previous block heights - requires passing more context

---

## Recommendation

The cleanest fix is **Solution 2: Track Line-Height Separately**, but it requires careful refactoring.

A simpler tactical fix: **Don't advance currentY during line break if the line had no actual content** (only OpenTag markers). This would prevent the double-advancement issue.

```go
// Add flag to track if line had content
hasLineContent := false

// When processing TEXT
hasLineContent = true

// During line break
if frag.Position.Y != currentLineY {
    if hasLineContent && currentLineMaxHeight > 0 {
        currentY = currentLineY + currentLineMaxHeight
    }
    currentLineY = frag.Position.Y
    currentLineMaxHeight = 0
    hasLineContent = false
}
```

This would prevent advancing by 19.2 for a line that only has an OpenTag (no actual text yet).

---

## Key Takeaways

1. **Reference works because**: It uses simple block layout, not block-in-inline
2. **Test fails because**: Multi-pass uses text content height (12.0) instead of line-height (19.2)
3. **Root cause**: Line break detection resets `currentLineMaxHeight` between OpenTag and TEXT
4. **Why fixes failed**: Line-height gets used for PREVIOUS line, then reset before CURRENT line
5. **Core issue**: Mismatch between when line-height is set (OpenTag) and when it's needed (BlockChild)

The bug is not a simple missing assignment - it's an architectural timing issue in how multi-pass tracks line heights across line breaks.
