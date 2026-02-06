# Louis14 Browser Engine - CSS Layout Fix Continuation

## Project Context
Louis14 is a browser rendering engine built from scratch in Go. Current focus: improving CSS layout correctness by passing the W3C CSS 2.1 test suite (WPT reftests).

**Current Status**: 33/51 tests passing (64.7%)

## Recent Progress (Just Completed)
1. **inline-block-baseline-001**: Fixed from 100% error → 0.6% by implementing content rendering for inline-blocks in multi-pass layout
2. **cascade-009a**: Fixed from 99.9% error → PASSING by implementing `:first-letter` pseudo-element support in multi-pass layout

## Multi-Pass Layout Architecture
Louis14 uses a Blink LayoutNG-style three-phase inline layout:

### Phase 1: CollectInlineItems
- **Location**: `pkg/layout/layout.go` ~line 6640+ (function: `collectInlineItems`)
- **Purpose**: Flatten DOM to sequential InlineItem list
- **What it does**: Traverse children, create InlineItem for each (text, inline element, inline-block, float, block child)
- **Key point**: This is where :first-letter support was added (lines 6647-6747)

### Phase 2: BreakLines
- **Location**: `pkg/layout/layout.go` ~line 6880+ 
- **Purpose**: Decide what items go on each line (with RETRY when floats change constraints)
- **Output**: LineBreakResult for each line

### Phase 3: ConstructLineBoxes
- **Location**: `pkg/layout/layout.go` ~line 7120+ (function: `constructLineBoxesWithRetry`)
- **Purpose**: Create positioned Box fragments from line breaking results
- **Key point**: This is where inline-block content rendering was added (lines 7233-7254)

## Your Task: Fix the Highest Error Rate Test

### Test to Fix: **empty-inline-002** (37.9% error - 60,632/160,000 pixels differ)

**Test Location**: 
- Test: `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht`
- Reference: `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002-ref.xht`
- Rendered outputs: `output/reftests/empty-inline-002_*.png`

### Steps to Follow

1. **Read and understand the test**
   ```bash
   # View the test files
   Read pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht
   Read pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002-ref.xht
   
   # View rendered outputs
   Read output/reftests/empty-inline-002_test.png
   Read output/reftests/empty-inline-002_ref.png
   Read output/reftests/empty-inline-002_diff.png
   ```

2. **Identify the issue**
   - Compare test vs reference visually
   - Look for what CSS feature is being tested (check meta name="assert" in the test)
   - Identify what's rendering incorrectly

3. **Find the relevant code**
   - If it's about inline box sizing/positioning: check multi-pass layout phases
   - If it's about empty inline handling: look for whitespace/empty element handling
   - Use `Grep` to search for relevant patterns in `pkg/layout/layout.go`

4. **Implement the fix**
   - Follow existing patterns (see inline-block and :first-letter fixes as examples)
   - For Phase 1 changes: modify `collectInlineItems` 
   - For Phase 3 changes: modify `constructLineBoxesWithRetry`
   - Test incrementally with: `go test ./pkg/visualtest -run TestWPTReftests 2>&1 | grep "empty-inline-002"`

5. **Verify the fix**
   ```bash
   # Run the specific test
   go test ./pkg/visualtest -run TestWPTReftests 2>&1 | grep -B 1 "empty-inline-002"
   
   # Check overall pass rate
   go test ./pkg/visualtest -run TestWPTReftests 2>&1 | tail -5
   ```

## Key Architecture Patterns

### Pattern 1: Adding new InlineItem types (Phase 1)
```go
// In collectInlineItems, around line 6647-6747
if node.Type == html.TextNode {
    // Check for special styling (e.g., :first-letter)
    if specialCondition {
        // Create special item with custom styling
        specialStyle := css.ComputePseudoElementStyle(...)
        item := &InlineItem{
            Type: InlineItemText,
            Style: specialStyle,
            ...
        }
        state.Items = append(state.Items, item)
    }
}
```

### Pattern 2: Handling items during line construction (Phase 3)
```go
// In constructLineBoxesWithRetry, around line 7233+
switch item.Type {
case InlineItemAtomic:
    // Recursively layout content
    atomicBox := le.layoutNode(item.Node, currentX, line.Y, item.Width, computedStyles, parent)
    if atomicBox != nil {
        // Apply positioning/alignment
        le.applyVerticalAlign(atomicBox, line.Y, line.LineHeight)
        boxes = append(boxes, atomicBox)
        currentX += le.getTotalWidth(atomicBox)
    }
}
```

## Important Files
- **Main layout**: `pkg/layout/layout.go` (~7,300 lines)
- **CSS cascade**: `pkg/css/cascade.go`
- **CSS styles**: `pkg/css/style.go`
- **Test runner**: `pkg/visualtest/reftest_runner_test.go`

## Tools Available
- Go binary: `/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go`
- Render tool: `go run cmd/l14open/main.go <input.html> <output.png> [width] [height]`
- Test analysis: `go run analyze-test-results.go` (shows sorted error rates)

## Success Criteria
- Reduce empty-inline-002 error from 37.9% to <5% (or passing)
- No regressions in other tests
- Overall pass rate increases from 33/51

## Memory Notes Location
Key learnings are documented in:
- `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`
- Update this with any new insights about the architecture or common patterns

Good luck! The multi-pass architecture is working well - inline-blocks and :first-letter are now functional. Focus on understanding what empty-inline-002 is testing and apply similar patterns.
