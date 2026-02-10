# Fix: Duplicate Layout Passes in Block-in-Inline Elements

## Problem Statement

The `inline-box-001.xht` test fails with 1.9% error (3070/160000 pixels) due to duplicate layout passes creating multiple boxes for the same block-in-inline content. An inline element containing a block child is being laid out **twice**, with boxes from both passes appearing in the final render tree.

### Test Case

```html
<div id="div1" style="border: 2px solid blue; display: inline;">
    First line
    <div>Filler Text</div>  <!-- Block child with orange background -->
    Last line
</div>
```

**Expected**:
- Fragment 1: "First line" with left/top/bottom borders at Y=51.2
- Orange div: "Filler Text" at Y=70.4
- Fragment 2: "Last line" with right/top/bottom borders at Y=89.6

**Actual**:
- Fragment 1: ✅ Correct at Y=51.2
- Orange div: ❌ **TWO** boxes - one at Y=63.2 (wrong), one at Y=70.4 (correct)
- Fragment 2: ❌ Wrong at Y=101.6 (should be Y=89.6)

## Root Cause Analysis

### The Duplicate Layout Problem

Multi-pass inline layout (`LayoutInlineContentToBoxes`) is invoked **10 times** during the test, with at least two passes creating boxes for the same content:

**Call 2 (Early Pass - WRONG)**:
- Processes inline div's children: OpenTag → TEXT("First line") → BlockChild → TEXT("Last line") → CloseTag
- Uses text content height (12.0px) instead of line-height (19.2px)
- Creates orange div at Y = 51.2 + 12.0 = **63.2px** ❌
- Box gets added to parent's children via `box.Children = append(box.Children, childBoxes...)`

**Call 6 (Final Pass - CORRECT)**:
- Re-processes same content with fragment box creation
- Creates Fragment 1 at Y=51.2, orange div at Y=70.4, Fragment 2 at Y=82.4 ✅
- **ALSO** appends boxes to parent's children

Result: Parent has **duplicate children** - one set from Call 2, one from Call 6.

### Why Multiple Passes Happen

The multi-pass layout is invoked recursively:

1. **Root layout** (html/body)
2. **Paragraph layout** (the `<p>` element)
3. **Inline div layout - Call 2** (early pass, no fragment detection)
4. **Block child layout** (orange div)
5. **Inline div layout - Call 6** (final pass, creates fragments)

Each invocation calls `LayoutInlineContentToBoxes`, and each creates boxes that get appended to the parent.

### Why Line Height is Wrong in Call 2

In Call 2, when the block child is encountered, the code finalizes the current line:

```go
// Line 746-751 in layout_inline_multipass.go
if currentLineMaxHeight > 0 {
    fmt.Printf("  Finalizing current line before block: currentY %.1f, height %.1f\n",
        currentY, currentLineMaxHeight)
    currentY = currentY + currentLineMaxHeight  // Uses 12.0 instead of 19.2!
}
```

The `currentLineMaxHeight` is only tracking text content height (12.0px) because:
1. OpenTag for `<div id="div1">` doesn't update `currentLineMaxHeight` with the element's line-height
2. Line break detection resets `currentLineMaxHeight` to 0
3. TEXT fragment sets it to text content height (12.0px)
4. Block child finalization uses 12.0px instead of 19.2px

## Architectural Analysis

### Current Flow (Problematic)

```
layoutNode(inline-div)
  └─> LayoutInlineContentToBoxes (Call 2)
      ├─> Process OpenTag
      ├─> Process TEXT("First line") → creates text box
      ├─> Process BlockChild
      │   └─> layoutNode(orange-div) ← Creates box at Y=63.2
      ├─> Process TEXT("Last line") → creates text box
      └─> Returns boxes → appended to parent.Children ❌ WRONG BOXES

[Later, during recursive layout...]

layoutNode(inline-div) [called again!]
  └─> LayoutInlineContentToBoxes (Call 6)
      ├─> Detects block-in-inline pattern
      ├─> Creates Fragment 1 box
      ├─> Recursively lays out BlockChild
      │   └─> layoutNode(orange-div) ← Creates box at Y=70.4
      ├─> Creates Fragment 2 box
      └─> Returns boxes → appended to parent.Children ✅ CORRECT BOXES

Result: parent.Children has BOTH sets!
```

### Why This Happens

**Missing deduplication mechanism**: When `LayoutInlineContentToBoxes` is called multiple times for the same DOM node, each call appends its boxes to the parent. There's no mechanism to:
1. Detect that a node has already been laid out
2. Clear previous layout results before new ones
3. Mark which pass is "authoritative"

## Solution Architecture

### Option 1: Layout Caching with Node-to-Box Mapping (RECOMMENDED)

**Approach**: Track which DOM nodes have been laid out and reuse their boxes instead of creating new ones.

**Implementation**:

1. **Add layout cache to LayoutEngine**:
```go
type LayoutEngine struct {
    // ... existing fields ...
    layoutCache map[*html.Node]*Box  // Maps DOM nodes to their final layout boxes
}
```

2. **Check cache before layout**:
```go
func (le *LayoutEngine) layoutNode(...) *Box {
    // Check if already laid out
    if cachedBox, exists := le.layoutCache[node]; exists {
        fmt.Printf("CACHE HIT: Reusing box for <%s>\n", node.TagName)
        return cachedBox
    }

    // ... perform layout ...

    // Store in cache
    le.layoutCache[node] = box
    return box
}
```

3. **Clear cache between layout passes** (if needed):
```go
func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
    le.layoutCache = make(map[*html.Node]*Box)  // Fresh cache per document
    // ... rest of layout ...
}
```

**Pros**:
- ✅ Simple to implement
- ✅ Prevents all duplicate layouts, not just block-in-inline
- ✅ Performance improvement (skips redundant layout calculations)
- ✅ Preserves existing multi-pass architecture

**Cons**:
- ⚠️ May mask bugs where layout should run multiple times
- ⚠️ Needs careful cache invalidation strategy

### Option 2: Fix currentLineMaxHeight Tracking

**Approach**: Ensure `currentLineMaxHeight` correctly tracks inline element line-heights across line breaks.

**Implementation**:

1. **Update OpenTag to set currentLineMaxHeight**:
```go
if isOpenTag {
    span := &inlineSpan{...}
    inlineStack = append(inlineStack, span)

    // NEW: Track inline element's contribution to line height
    if frag.Style != nil {
        lineHeight := frag.Style.GetLineHeight()
        if lineHeight > currentLineMaxHeight {
            currentLineMaxHeight = lineHeight
        }
    }
}
```

2. **Restore inline contributions after line breaks**:
```go
if frag.Position.Y != currentLineY {
    // ... advance currentY ...
    currentLineMaxHeight = 0

    // NEW: Restore contributions from active inline elements
    for _, span := range inlineStack {
        if span.style != nil {
            spanLineHeight := span.style.GetLineHeight()
            if spanLineHeight > currentLineMaxHeight {
                currentLineMaxHeight = spanLineHeight
            }
        }
    }
}
```

**Pros**:
- ✅ Fixes the specific line-height calculation bug
- ✅ Doesn't change overall architecture

**Cons**:
- ❌ **Doesn't solve duplicate layout problem** - still have two boxes!
- ❌ Complex interaction between line break detection and block finalization
- ❌ Risk of double-counting Y advancements

**Verdict**: This approach fixes a symptom but not the disease.

### Option 3: Pass-Level Coordination

**Approach**: Mark layout passes as "preliminary" vs "final" and only keep boxes from final passes.

**Implementation**:

1. **Add pass tracking**:
```go
type LayoutEngine struct {
    layoutPassLevel int  // 0 = final pass, >0 = preliminary
}
```

2. **Mark preliminary passes**:
```go
func (le *LayoutEngine) layoutNode(...) *Box {
    // Increase pass level for recursive layouts
    le.layoutPassLevel++
    defer func() { le.layoutPassLevel-- }()

    // ... layout logic ...
}
```

3. **Only commit boxes from final pass**:
```go
if le.layoutPassLevel == 0 {
    box.Children = append(box.Children, childBoxes...)  // Only at final level
}
```

**Pros**:
- ✅ Explicitly tracks which pass is authoritative
- ✅ Could support multi-level layout strategies

**Cons**:
- ❌ Complex to determine what is "preliminary" vs "final"
- ❌ May break legitimate multi-pass scenarios
- ❌ Requires extensive testing

## Recommended Implementation Plan

### Phase 1: Implement Layout Cache (Option 1)

**Priority**: HIGH
**Effort**: Low (2-3 hours)
**Risk**: Low

#### Step 1: Add cache infrastructure

**File**: `pkg/layout/types.go`

```go
type LayoutEngine struct {
    // ... existing fields ...

    // layoutCache maps DOM nodes to their laid-out boxes to prevent duplicate layouts
    // Cleared at the start of each document layout
    layoutCache map[*html.Node]*Box
}
```

#### Step 2: Initialize cache

**File**: `pkg/layout/layout_main.go`

```go
func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
    fmt.Println("DEBUG: Layout() called - code is running!")

    // PHASE 1: Initialize layout cache to prevent duplicate layouts
    le.layoutCache = make(map[*html.Node]*Box)

    // ... rest of existing code ...
}
```

#### Step 3: Check cache in layoutNode

**File**: `pkg/layout/layout_block.go` (or wherever layoutNode is defined)

```go
func (le *LayoutEngine) layoutNode(
    node *html.Node,
    x, y, availableWidth float64,
    computedStyles map[*html.Node]*css.Style,
    parent *Box,
) *Box {
    // CACHE CHECK: Return cached box if already laid out
    if cachedBox, exists := le.layoutCache[node]; exists {
        fmt.Printf("LAYOUT CACHE HIT: Reusing box for <%s id='%s'>\n",
            node.TagName, node.Attributes["id"])
        return cachedBox
    }

    // ... existing layout logic ...

    // CACHE STORE: Save box before returning
    le.layoutCache[node] = box
    fmt.Printf("LAYOUT CACHE STORE: Cached box for <%s id='%s'>\n",
        node.TagName, node.Attributes["id"])

    return box
}
```

#### Step 4: Test and validate

**Test**: `inline-box-001.xht`

Expected outcome:
- Orange div laid out only ONCE at correct Y position
- No duplicate boxes in render tree
- Error drops from 1.9% to <0.3%

**Validation checklist**:
- [ ] Orange div appears only once (check debug output)
- [ ] Fragment 1 at Y=51.2 ✓
- [ ] Orange div at Y=70.4 ✓
- [ ] Fragment 2 at Y=89.6 ✓
- [ ] No regressions in other tests

#### Step 5: Document cache behavior

Add comments explaining:
- When cache is cleared (per document)
- Why caching is safe (nodes don't change during layout)
- How to debug cache misses

### Phase 2: Fix Line Height Tracking (Optional Cleanup)

**Priority**: MEDIUM
**Effort**: Low (1-2 hours)
**Risk**: Low

Even with caching preventing duplicate layouts, the line-height calculation should be correct for future-proofing.

#### Step 1: Update currentLineMaxHeight at OpenTag

**File**: `pkg/layout/layout_inline_multipass.go`

Add after line 838 (in OpenTag block):

```go
// Track inline element's line-height contribution
if frag.Style != nil {
    lineHeight := frag.Style.GetLineHeight()
    if lineHeight > currentLineMaxHeight {
        currentLineMaxHeight = lineHeight
        fmt.Printf("  OpenTag: Updated currentLineMaxHeight to %.1f\n", lineHeight)
    }
}
```

This ensures that even in preliminary passes (if they somehow still happen), the line height will be correct.

### Phase 3: Add Defensive Checks

**Priority**: LOW
**Effort**: Low (1 hour)
**Risk**: Very Low

Add validation to catch unexpected duplicate layouts:

```go
func (le *LayoutEngine) layoutNode(...) *Box {
    if cachedBox, exists := le.layoutCache[node]; exists {
        // Defensive check: warn if trying to layout with different parameters
        if cachedBox.Y != y || cachedBox.X != x {
            fmt.Printf("⚠️  WARNING: Layout cache hit but position differs!\n")
            fmt.Printf("    Cached: (%.1f, %.1f), Requested: (%.1f, %.1f)\n",
                cachedBox.X, cachedBox.Y, x, y)
        }
        return cachedBox
    }
    // ...
}
```

## Testing Strategy

### Test 1: inline-box-001.xht (Primary)

**Before**: 1.9% error (3070/160000 pixels)
**After**: <0.3% error (should PASS)

**What to check**:
- Orange div appears at Y=70.4 only (not Y=63.2)
- Fragment 1 at Y=51.2 with left/top/bottom borders
- Fragment 2 at Y=89.6 with right/top/bottom borders
- Debug output shows only ONE layout per node

### Test 2: Full Test Suite

Run all 51 tests to ensure no regressions:

```bash
go test ./pkg/visualtest -run "TestWPTReftests" -v
```

**Expected**: 34/51 or better (no regressions from current baseline)

### Test 3: Debug Output Validation

Check debug output for cache behavior:

```bash
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v 2>&1 | grep "CACHE"
```

Should see:
- `LAYOUT CACHE STORE` for first layout
- `LAYOUT CACHE HIT` for subsequent attempts
- NO duplicate box creation

## Expected Outcomes

### Immediate (Phase 1)

- ✅ `inline-box-001.xht` passes (error <0.3%)
- ✅ No duplicate boxes in render tree
- ✅ 10-20% performance improvement (skipped redundant layouts)
- ✅ 35+/51 tests passing (up from 34/51)

### Medium Term (Phase 2)

- ✅ Line-height calculation correct even without cache
- ✅ Code more maintainable and easier to debug
- ✅ Foundation for future layout optimizations

### Long Term

- ✅ Potential to cache sub-tree layouts for performance
- ✅ Cleaner separation between layout passes
- ✅ Better handling of dynamic content updates (future feature)

## Risk Analysis

### Low Risk

**Layout cache** is inherently safe because:
- DOM nodes are immutable during layout
- Each document gets fresh cache
- Cache key is just the node pointer (simple, fast)
- Easy to disable for debugging (just comment out cache check)

### Potential Issues

1. **Cache key collisions**: Not possible with pointer-based keys
2. **Stale cache**: Cleared per document, can't go stale
3. **Memory usage**: Negligible (one pointer per laid-out node)
4. **Position changes**: Defensive checks catch this

## Alternative Approaches Considered

### ❌ Modify box.Children appending logic

**Why rejected**: Would require tracking which boxes are "preliminary" vs "final" across the entire codebase. Too invasive.

### ❌ Single-pass-only mode

**Why rejected**: Multi-pass is needed for complex layouts (floats, block-in-inline, etc.). Can't disable it.

### ❌ Clear children before layout

**Why rejected**: Would break legitimate cases where children are added incrementally.

## Success Criteria

### Must Have (Phase 1)

- [x] Document created with implementation plan
- [ ] Layout cache implemented in types.go
- [ ] Cache check added to layoutNode
- [ ] inline-box-001.xht passes (<0.3% error)
- [ ] No regressions in other tests (34+/51 passing)

### Nice to Have (Phase 2)

- [ ] Line-height tracking fixed
- [ ] Defensive validation added
- [ ] Performance benchmarks show improvement

### Metrics

**Before**:
- inline-box-001.xht: 1.9% error
- 34/51 tests passing (66.7%)
- ~10 layout passes per test

**After Phase 1**:
- inline-box-001.xht: <0.3% error (PASS)
- 35+/51 tests passing (68.6%+)
- ~5-7 layout passes per test (cache hits reduce work)

**After Phase 2**:
- inline-box-002.xht: Improved (relative positioning)
- Code quality: Higher maintainability score
- Debug output: Cleaner, easier to trace

## Conclusion

The layout cache approach (Option 1) is the **clear winner**:
- Simple to implement
- Low risk
- Solves root cause (duplicate layouts)
- Performance bonus
- Foundation for future optimizations

The implementation can be done incrementally with clear validation at each step. Phase 1 alone should get `inline-box-001.xht` passing and improve overall test pass rate.
