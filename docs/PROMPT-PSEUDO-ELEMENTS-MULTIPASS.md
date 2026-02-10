# Completing Pseudo-Element Integration for Multi-Pass Layout

## Context

You've partially fixed pseudo-element generation for multi-pass inline layout. Pseudo-elements (::before, ::after) are NOW being generated and content (counters, images, quotes, text) is visible, but **positioning is incorrect**.

### Current Status (Commit: 39905d7)

**✅ What Works:**
- Pseudo-elements are generated before/after multi-pass layout call
- Content renders: counters, images, quotes, text, attr() all visible
- Test error increased 3.8% → 22.4% (more content = more pixels differ)

**❌ What's Broken:**
- Positioning: Pseudo-elements not integrated into multi-pass coordinate space
- Float handling: Floated pseudo-elements don't participate in float positioning
- Architecture: Current fix is bolted on, not properly integrated

### Test Case
**File:** `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`
- **Current:** 22.4% error (content visible but mispositioned)
- **Goal:** PASS (0% error)
- **Test has:** 4 divs with floated ::before/::after containing counters, images, quotes, text

---

## Problem Analysis

### Issue 1: Coordinate Space Mismatch

**Current code** (pkg/layout/layout_block.go:449-488):
```go
// Generate ::before at initial position
lineX := box.X + border.Left + padding.Left
lineY := childY
beforeBox := le.generatePseudoElement(node, "before", lineX, lineY, ...)

// Run multi-pass (which repositions all content)
inlineLayoutResult = le.LayoutInlineContentToBoxes(node.Children, ...)

// Generate ::after at "final" position
afterBox := le.generatePseudoElement(node, "after",
    finalInlineCtx.LineX, finalInlineCtx.LineY, ...)

// Combine: beforeBoxes + childBoxes + afterBoxes
childBoxes = append(beforeBoxes, childBoxes...)
childBoxes = append(childBoxes, afterBoxes...)
```

**Problems:**
1. `beforeBox` uses initial (lineX, lineY), but multi-pass may adjust all coordinates
2. `finalInlineCtx` may not represent where ::after should go (e.g., if last child is block-level)
3. Pseudo-element boxes are created independently, not integrated into multi-pass flow
4. Float positioning happens separately from multi-pass float handling

### Issue 2: Float Integration

**Current float handling:**
```go
if beforeFloat != css.FloatNone {
    le.addFloat(beforeBox, beforeFloat, beforeBox.Y)
}
```

**Problems:**
1. Float is added to global registry, but multi-pass doesn't know about it during layout
2. Multi-pass calculates line positions based on floats it knows about
3. Pseudo-element floats added BEFORE multi-pass should affect children
4. Pseudo-element floats added AFTER multi-pass don't affect anything

### Issue 3: Architectural Mismatch

**Single-pass approach** (layout_inline_singlepass.go:115):
- Generates ::before
- Updates `inlineCtx` (LineX, LineY) based on ::before size
- Lays out children with updated context
- Generates ::after at final context position

**Multi-pass approach** (current WIP):
- Generates ::before independently
- Multi-pass lays out children (doesn't know about ::before)
- Generates ::after independently
- Appends all boxes together (no coordinate integration)

---

## Solution: Three Options

### Option A: Post-Process Positioning (Quick Fix, 2-3 hours)

**Approach:** Generate pseudo-elements, then adjust their positions based on multi-pass result.

**Steps:**
1. After multi-pass returns, inspect first child box position for ::before placement
2. Inspect last child box position for ::after placement
3. Adjust pseudo-element coordinates to match multi-pass coordinate space
4. Handle floats by inserting them at correct positions in the flow

**Pros:** Minimal code change, keeps current architecture
**Cons:** Still a bolt-on, may break with future multi-pass changes

### Option B: Pre-Process as Synthetic Nodes (Medium, 4-6 hours)

**Approach:** Create synthetic HTML nodes for pseudo-elements that multi-pass can process.

**Steps:**
1. Before calling `LayoutInlineContentToBoxes`, create synthetic nodes:
   ```go
   syntheticBefore := &html.Node{
       Type:     html.ElementNode,
       TagName:  "::before",
       // Store pseudo-element content/style
   }
   ```
2. Insert into children: `[syntheticBefore] + node.Children + [syntheticAfter]`
3. Modify multi-pass to recognize synthetic pseudo-element nodes
4. Generate boxes inside multi-pass pipeline at correct positions

**Pros:** Properly integrated, follows multi-pass architecture
**Cons:** Requires modifying multi-pass pipeline, more invasive

### Option C: Fragment-Level Integration (Proper, 6-8 hours)

**Approach:** Add pseudo-element support directly in multi-pass fragment generation.

**Steps:**
1. Modify `LayoutInlineContent` (fragment generation) to handle pseudo-elements
2. Add `FragmentPseudoBefore` and `FragmentPseudoAfter` types
3. Generate pseudo-element fragments at start/end of fragment list
4. Convert fragments to boxes with correct positions (already handles this)

**Pros:** Clean architecture, matches CSS spec, future-proof
**Cons:** Largest code change, requires deep multi-pass understanding

---

## Recommended Approach: Option A (Post-Process)

Start with Option A to get tests passing, document limitations, plan Option C for future.

### Implementation Steps (2-3 hours)

#### Step 1: Analyze Multi-Pass Output (30 min)

Before adjusting positions, understand what multi-pass returns:

```go
// After multi-pass call, examine result
fmt.Printf("=== Multi-pass result analysis ===\n")
fmt.Printf("ChildBoxes count: %d\n", len(inlineLayoutResult.ChildBoxes))
if len(inlineLayoutResult.ChildBoxes) > 0 {
    firstBox := inlineLayoutResult.ChildBoxes[0]
    lastBox := inlineLayoutResult.ChildBoxes[len(inlineLayoutResult.ChildBoxes)-1]
    fmt.Printf("First child box: (%.1f, %.1f)\n", firstBox.X, firstBox.Y)
    fmt.Printf("Last child box: (%.1f, %.1f)\n", lastBox.X, lastBox.Y)
}
fmt.Printf("FinalInlineCtx: LineX=%.1f, LineY=%.1f\n",
    finalInlineCtx.LineX, finalInlineCtx.LineY)
```

**Run test and examine:**
- Where are child boxes positioned?
- What coordinate space are they in (absolute or relative)?
- Where should ::before and ::after go?

#### Step 2: Position ::before Correctly (45 min)

**Strategy:** Place ::before at the START of the first line.

```go
// Generate ::before AFTER multi-pass to use correct coordinates
var beforeBoxes []*Box
if len(inlineLayoutResult.ChildBoxes) > 0 {
    // Use first child's Y position as baseline
    firstChildY := inlineLayoutResult.ChildBoxes[0].Y

    // For floated ::before, position at line start
    lineX := box.X + border.Left + padding.Left

    // Check for left floats that would affect LineX
    leftOffset, _ := le.getFloatOffsets(firstChildY)
    lineX += leftOffset

    beforeBox := le.generatePseudoElement(node, "before", lineX, firstChildY,
        childAvailableWidth, computedStyles, box)

    if beforeBox != nil {
        beforeBoxes = append(beforeBoxes, beforeBox)

        // Register float BEFORE combining boxes
        beforeFloat := beforeBox.Style.GetFloat()
        if beforeFloat != css.FloatNone {
            le.addFloat(beforeBox, beforeFloat, beforeBox.Y)

            // CRITICAL: Adjust all child boxes if ::before is left-floated
            if beforeFloat == css.FloatLeft {
                beforeWidth := le.getTotalWidth(beforeBox)
                for _, childBox := range inlineLayoutResult.ChildBoxes {
                    if childBox.Y == firstChildY {
                        childBox.X += beforeWidth
                    }
                }
            }
        }
    }
}
```

**Test after this step:**
```bash
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

**Expected:** ::before content visible at correct position, ::after still wrong.

#### Step 3: Position ::after Correctly (45 min)

**Strategy:** Place ::after at the END of the last line.

```go
// Generate ::after at end of last line
var afterBoxes []*Box
if len(inlineLayoutResult.ChildBoxes) > 0 {
    lastBox := inlineLayoutResult.ChildBoxes[len(inlineLayoutResult.ChildBoxes)-1]

    // Position after last content
    afterX := lastBox.X + lastBox.Width + lastBox.Padding.Right + lastBox.Border.Right + lastBox.Margin.Right
    afterY := lastBox.Y

    // Handle case where last child is block (::after should be on new line)
    if lastBox.Style != nil && lastBox.Style.GetDisplay() == css.DisplayBlock {
        afterX = box.X + border.Left + padding.Left
        afterY = lastBox.Y + le.getTotalHeight(lastBox)
    }

    afterBox := le.generatePseudoElement(node, "after", afterX, afterY,
        childAvailableWidth, computedStyles, box)

    if afterBox != nil {
        afterBoxes = append(afterBoxes, afterBox)

        afterFloat := afterBox.Style.GetFloat()
        if afterFloat != css.FloatNone {
            le.addFloat(afterBox, afterFloat, afterBox.Y)
        }
    }
}
```

**Test after this step:**
```bash
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

**Expected:** Both ::before and ::after at approximately correct positions.

#### Step 4: Handle Float Positioning (1 hour)

**Problem:** Floated pseudo-elements should position relative to container, not inline flow.

**Fix floated ::before:**
```go
if beforeFloat == css.FloatLeft {
    // Left float: position at left edge + existing left floats
    leftOffset, _ := le.getFloatOffsets(firstChildY)
    beforeBox.X = box.X + border.Left + padding.Left + leftOffset
} else if beforeFloat == css.FloatRight {
    // Right float: position at right edge - float width - existing right floats
    _, rightOffset := le.getFloatOffsets(firstChildY)
    floatWidth := le.getTotalWidth(beforeBox)
    beforeBox.X = box.X + box.Width - border.Right - padding.Right - rightOffset - floatWidth
}
```

**Fix floated ::after:**
```go
if afterFloat == css.FloatLeft {
    leftOffset, _ := le.getFloatOffsets(afterY)
    afterBox.X = box.X + border.Left + padding.Left + leftOffset
} else if afterFloat == css.FloatRight {
    _, rightOffset := le.getFloatOffsets(afterY)
    floatWidth := le.getTotalWidth(afterBox)
    afterBox.X = box.X + box.Width - border.Right - padding.Right - rightOffset - floatWidth
}
```

**Test after this step:**
```bash
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

**Expected:** Significant error reduction, possibly passing.

#### Step 5: Debug and Refine (30 min)

If test still fails, add debug output to compare with reference:

```go
fmt.Printf("=== Pseudo-element positions ===\n")
if beforeBox != nil {
    fmt.Printf("::before: (%.1f, %.1f) float=%v\n",
        beforeBox.X, beforeBox.Y, beforeBox.Style.GetFloat())
}
if afterBox != nil {
    fmt.Printf("::after: (%.1f, %.1f) float=%v\n",
        afterBox.X, afterBox.Y, afterBox.Style.GetFloat())
}
```

Compare with reference rendering to identify remaining issues.

---

## Testing Strategy

### Primary Test
```bash
# Run main test
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# Check error percentage
# Target: < 1% (acceptable due to anti-aliasing)
# Current: 22.4%
```

### Regression Tests
```bash
# Ensure Phase 2 still works
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v

# Check full suite
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"

# Target: 34+/51 passing (gain 1 test from fixing pseudo-elements)
```

### Visual Inspection
```bash
# View test output
open output/reftests/before-after-floated-001_test.png
open output/reftests/before-after-floated-001_ref.png
open output/reftests/before-after-floated-001_diff.png

# Expected in test output:
# - 4 green-bordered divs
# - Each with floated ::before (counter "1", image, quote, text)
# - Each with floated ::after (counter "2", image, text, quote)
# - Floats positioned left or right as specified
# - "Inner" text between pseudo-elements
```

---

## Success Criteria

- [ ] before-after-floated-001.xht: PASS or < 1% error
- [ ] All pseudo-element content visible (counters, images, quotes, text)
- [ ] Floated ::before positioned correctly (left or right)
- [ ] Floated ::after positioned correctly (left or right)
- [ ] "Inner" text flows around floats correctly
- [ ] Test suite: 34+/51 passing (no regressions)
- [ ] inline-box-001.xht: Still ≤ 0.5% error

---

## Future Work (Option C - Proper Integration)

After Option A is working, plan for proper integration:

1. **Design document**: How pseudo-elements fit into fragment pipeline
2. **Fragment types**: Add FragmentPseudoBefore/After to fragment.go
3. **Collection phase**: Generate pseudo-element fragments in LayoutInlineContent
4. **Line breaking**: Ensure pseudo-elements participate in line breaking
5. **Box construction**: Convert pseudo-element fragments to boxes

**Estimated effort:** 6-8 hours for complete integration

---

## Key Files

**Current code:**
- `pkg/layout/layout_block.go`: Lines 449-488 (WIP pseudo-element generation)
- `pkg/layout/pseudo_elements.go`: generatePseudoElement() function
- `pkg/layout/layout_inline_multipass.go`: Multi-pass pipeline

**Single-pass reference:**
- `pkg/layout/layout_inline_singlepass.go`: Lines 115, 556 (working implementation)

**Test:**
- `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`

---

## Common Issues

### Issue: Pseudo-elements overlap with content
**Cause:** Not accounting for float width when positioning children
**Fix:** Adjust child X positions after adding floated ::before

### Issue: ::after at wrong Y position
**Cause:** Using last child's Y when last child is block-level
**Fix:** Check child display type, add block height for block children

### Issue: Floats don't affect layout
**Cause:** addFloat() called after children are positioned
**Fix:** Add floats BEFORE processing children, or adjust children after

### Issue: Images not showing
**Cause:** Image paths not resolved, or box dimensions = 0
**Fix:** Check ImagePath is set, dimensions loaded from image fetcher

---

## Quick Start Commands

```bash
# 1. Check current state
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v 2>&1 | grep "REFTEST"

# 2. Implement Option A steps 1-5 in pkg/layout/layout_block.go

# 3. Test after each step
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# 4. Visual inspection
open output/reftests/before-after-floated-001_test.png

# 5. Full regression test
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | tail -60
```

---

## Notes

- Current commit: 39905d7 (WIP pseudo-element generation)
- MEMORY.md updated with investigation findings
- Content generation works, positioning is the only remaining issue
- Estimated 2-3 hours to complete Option A
- Test was passing before multi-pass became default (see MEMORY.md 2026-02-06)

Good luck! The hard part (understanding the problem) is done. This is implementation work following a clear plan. 🚀
