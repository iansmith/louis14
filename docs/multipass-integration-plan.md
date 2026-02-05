# Multi-Pass Inline Layout Integration Plan

**Status**: Ready for implementation
**Created**: 2026-02-05
**Goal**: Replace single-pass inline layout in layoutNode with LayoutInlineBatch

## Current State

### ✅ What's Working
- `LayoutInlineBatch()` method exists and works correctly
- Three-phase pipeline: CollectInlineItems → BreakLines → ConstructLineBoxes
- Float positioning with retry logic (2 iterations typical)
- Isolated tests pass: inline content positions after floats correctly

### 🔄 What Needs Integration
The child processing loop in `layoutNode` (lines ~787-1108) currently processes children one-by-one:
- Lines 825-890: Inline/inline-block element handling
- Lines 1033-1106: Text node handling
- Lines 890-1031: Block element handling

## Integration Strategy

### Overview
Replace one-by-one child processing with **batched inline layout**:
1. Loop through children with index
2. Detect inline runs (consecutive inline/text children)
3. Call `LayoutInlineBatch()` for each run
4. Handle block elements normally
5. Preserve existing features (block-in-inline, margin collapsing)

### Key Insight
**Don't rewrite everything** - keep block handling, margin collapsing, and fragment tracking unchanged. Only replace inline/text processing with batch calls.

## Step-by-Step Implementation

### Phase 1: Preparation (5 min)

1. **Create a test to verify current behavior**
   ```bash
   # Run box-generation-001.xht to get baseline
   make test-visual FILTER=box-generation-001
   # Note the current pixel difference (currently 6.5%)
   ```

2. **Create backup**
   ```bash
   git checkout -b multipass-integration
   cp pkg/layout/layout.go pkg/layout/layout.go.backup-$(date +%s)
   ```

### Phase 2: Code Structure Changes (30 min)

#### A. Replace child loop header (line 787)

**Current:**
```go
for _, child := range node.Children {
```

**New:**
```go
i := 0
for i < len(node.Children) {
    child := node.Children[i]
```

#### B. Add inline detection logic (after line 790)

**Insert after `if skipChildren { break }`:**

```go
// Determine if this child is inline or block
isInlineChild := false
var childStyle *css.Style

if child.Type == html.TextNode {
    isInlineChild = true
} else if child.Type == html.ElementNode {
    childStyle = computedStyles[child]
    if childStyle == nil {
        childStyle = css.NewStyle()
    }
    childDisplay := childStyle.GetDisplay()
    childFloat := childStyle.GetFloat()

    // Skip display:none entirely
    if childDisplay == css.DisplayNone {
        i++
        continue
    }

    // Inline, inline-block, and floats are processed as inline
    isInlineChild = (childDisplay == css.DisplayInline ||
                    childDisplay == css.DisplayInlineBlock ||
                    childFloat != css.FloatNone)
}
```

#### C. Add inline batch processing

**After inline detection, add:**

```go
if isInlineChild {
    // Collect consecutive inline/text children into a batch
    batchStart := i
    batchEnd := i + 1

    for batchEnd < len(node.Children) {
        nextChild := node.Children[batchEnd]
        nextIsInline := false

        if nextChild.Type == html.TextNode {
            nextIsInline = true
        } else if nextChild.Type == html.ElementNode {
            nextStyle := computedStyles[nextChild]
            if nextStyle == nil {
                nextStyle = css.NewStyle()
            }
            nextDisplay := nextStyle.GetDisplay()
            nextFloat := nextStyle.GetFloat()

            if nextDisplay == css.DisplayNone {
                batchEnd++
                continue
            }

            nextIsInline = (nextDisplay == css.DisplayInline ||
                          nextDisplay == css.DisplayInlineBlock ||
                          nextFloat != css.FloatNone)
        }

        if !nextIsInline {
            break // Hit a block, end batch
        }
        batchEnd++
    }

    // Process batch using multi-pass layout
    batch := node.Children[batchStart:batchEnd]
    batchBoxes := le.LayoutInlineBatch(
        batch,
        box,
        childAvailableWidth,
        inlineCtx.LineY,
        border,
        padding,
        computedStyles,
    )

    // Add batch results to parent
    for _, batchBox := range batchBoxes {
        // Block-in-inline fragment tracking
        if isInlineParent {
            if hasSeenBlockChild {
                batchBox.IsLastFragment = true
            } else {
                hasInlineContentBeforeBlock = true
            }

            // Update fragment region
            boxRight := batchBox.X + le.getTotalWidth(batchBox)
            boxBottom := batchBox.Y + le.getTotalHeight(batchBox)
            if boxRight > currentFragment.maxX {
                currentFragment.maxX = boxRight
            }
            if boxBottom > currentFragment.maxY {
                currentFragment.maxY = boxBottom
            }
            currentFragment.hasContent = true
        }

        box.Children = append(box.Children, batchBox)
    }

    // Update inline context after batch
    if len(batchBoxes) > 0 {
        lastBox := batchBoxes[len(batchBoxes)-1]
        inlineCtx.LineY = lastBox.Y
        inlineCtx.LineX = lastBox.X + le.getTotalWidth(lastBox)
        inlineCtx.LineHeight = le.getTotalHeight(lastBox)
    }

    // Move past this batch
    i = batchEnd
    continue
}
```

#### D. Update block handling

**Replace the large if/else block (lines 791-1032) with:**

```go
// Block element - process normally
if child.Type == html.ElementNode {
    if childStyle == nil {
        childStyle = computedStyles[child]
        if childStyle == nil {
            childStyle = css.NewStyle()
        }
    }

    // Block-in-inline tracking
    if isInlineParent && hasInlineContentBeforeBlock {
        if currentFragment.hasContent {
            completedFragments = append(completedFragments, currentFragment)
        }
        hasSeenBlockChild = true
        box.IsFirstFragment = true
    }

    // Finish current inline line
    if len(inlineCtx.LineBoxes) > 0 {
        strutHeight := style.GetLineHeight()
        if strutHeight > inlineCtx.LineHeight {
            inlineCtx.LineHeight = strutHeight
        }
        childY = inlineCtx.LineY + inlineCtx.LineHeight
        inlineCtx.LineBoxes = make([]*Box, 0)
        inlineCtx.LineHeight = 0
    } else {
        childY = inlineCtx.LineY
    }

    // Layout the block child
    childBox := le.layoutNode(
        child,
        box.X + border.Left + padding.Left,
        childY,
        childAvailableWidth,
        computedStyles,
        box,
    )

    // Rest of block handling (positioning, margin collapsing, etc.)
    // KEEP ALL EXISTING BLOCK HANDLING CODE (lines 960-1022)
    // Don't modify this - it's complex and works correctly
}

i++ // Move to next child
```

#### E. Remove old inline/text handling

**Delete these sections:**
- Lines 825-890: Old inline element handling
- Lines 1033-1106: Old text node handling

Keep the structure but replace with the batch processing above.

### Phase 3: Testing (15 min)

#### Test 1: Compile
```bash
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/gofmt -w pkg/layout/layout.go
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go build ./pkg/layout
```

Expected: Clean build, no errors

#### Test 2: Isolated test
```bash
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go run /tmp/test-batch-integration.go
```

Expected: Float at X=0, inline content at X≥100

#### Test 3: Visual test
```bash
make test-visual FILTER=box-generation-001
```

Expected: Improvement from 6.5% (ideally to <5%)

#### Test 4: Regression check
```bash
make test-visual FILTER=before-after-floated-001
```

Expected: Still at 0.1% (no regression)

### Phase 4: Debug Common Issues (if needed)

#### Issue 1: Compilation errors
- **Symptom**: Undefined variables, syntax errors
- **Cause**: Incomplete replacement, missing variable definitions
- **Fix**: Check that all references to old inline handling are removed
- **Tool**: Use gofmt to identify syntax issues

#### Issue 2: Inline content at wrong X position
- **Symptom**: Content overlapping or positioned at X=0
- **Cause**: LineX/currentX not updated correctly after batch
- **Fix**: Verify the "Update inline context after batch" section
- **Debug**: Add temporary fmt.Printf to show LineX before/after batch

#### Issue 3: Missing boxes in output
- **Symptom**: Fewer boxes than expected
- **Cause**: Batch processing skipping some children
- **Fix**: Check that `i = batchEnd` is correct and loop continues
- **Debug**: Add fmt.Printf showing batch range (batchStart, batchEnd)

#### Issue 4: Floats not positioned
- **Symptom**: Inline content not wrapping around floats
- **Cause**: Floats not being collected as InlineItemFloat
- **Fix**: Verify childFloat check includes all float values
- **Debug**: Check CollectInlineItems is being called for floated elements

## Rollback Plan

If integration fails:

```bash
# Restore from backup
cp pkg/layout/layout.go.backup-* pkg/layout/layout.go
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go build ./pkg/layout

# Or revert git branch
git checkout master
git branch -D multipass-integration
```

## Success Criteria

- ✅ Code compiles cleanly
- ✅ box-generation-001.xht improves (target: <5% from 6.5%)
- ✅ before-after-floated-001.xht maintains <1% (no regression)
- ✅ No crashes or panics in visual tests
- ✅ Inline content correctly wraps around floats

## Notes

### Why This Approach?

1. **Minimal changes**: Only replace inline/text processing, keep block handling
2. **Incremental**: Can test after each phase
3. **Preserves features**: Block-in-inline, margin collapsing unchanged
4. **Clear rollback**: Backup and git branch for safety

### What Could Go Wrong?

1. **Block-in-inline fragments**: The fragment tracking might need adjustment if batch processing changes box structure
2. **Margin collapsing**: Should be unaffected (only touches block elements)
3. **Performance**: Retry loop could slow down complex layouts (monitor test times)

### Next Steps After Integration

1. Run full visual test suite
2. Fix any regressions
3. Update memory.md with integration learnings
4. Create PR or commit to master
5. Consider refactoring layout.go into multiple files (task #15)

## References

- LayoutInlineBatch: `pkg/layout/layout.go` lines 4160-4250
- Current child loop: `pkg/layout/layout.go` lines 787-1108
- Float retry fix: commit a2485d6
- Memory documentation: `.claude/projects/.../memory/MEMORY.md`
