# Next Session: Implement Layout Cache to Fix Duplicate Layouts

## Context

You've diagnosed a critical bug in the multi-pass inline layout system where the same DOM node is being laid out multiple times, creating duplicate boxes in the render tree. This causes `inline-box-001.xht` to fail with 1.9% error.

## Your Task

Implement **Phase 1: Layout Cache** from `/docs/INLINE-BOX-DUPLICATE-LAYOUT-FIX.md` to prevent duplicate layouts.

## Problem Summary

**Current behavior**:
- Inline div containing block child gets laid out in Call 2 (creates orange div at Y=63.2 - WRONG)
- Same element gets laid out again in Call 6 (creates fragment boxes and orange div at Y=70.4 - CORRECT)
- Both sets of boxes end up in the render tree
- Result: Orange div appears in two places, fragments at wrong positions

**Root cause**: No mechanism to detect and prevent duplicate layouts of the same DOM node.

**Solution**: Add a simple layout cache that maps DOM nodes to their laid-out boxes.

## Implementation Steps

### Step 1: Add layoutCache field to LayoutEngine

**File**: `pkg/layout/types.go`

Find the `LayoutEngine` struct and add:

```go
type LayoutEngine struct {
    viewport       ViewportSize
    floats         []FloatInfo
    absoluteBoxes  []*Box
    stylesheets    []*css.Stylesheet
    useMultiPass   bool
    imageFetcher   images.ImageFetcher

    // NEW: Layout cache to prevent duplicate layouts
    // Maps DOM nodes to their final laid-out boxes
    // Cleared at the start of each document layout
    layoutCache    map[*html.Node]*Box
}
```

### Step 2: Initialize cache at layout start

**File**: `pkg/layout/layout_main.go`

In the `Layout()` function, add at the very beginning (after the debug print):

```go
func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
    fmt.Println("DEBUG: Layout() called - code is running!")

    // Initialize layout cache to prevent duplicate layouts
    le.layoutCache = make(map[*html.Node]*Box)
    fmt.Println("DEBUG: Initialized layout cache")

    // ... rest of existing code ...
}
```

### Step 3: Add cache check to layoutNode

**File**: Find where `layoutNode` is defined (likely `pkg/layout/layout_block.go`)

At the **very start** of the `layoutNode` function (before any other logic), add:

```go
func (le *LayoutEngine) layoutNode(
    node *html.Node,
    x, y, availableWidth float64,
    computedStyles map[*html.Node]*css.Style,
    parent *Box,
) *Box {
    // CACHE CHECK: Return cached box if this node was already laid out
    if cachedBox, exists := le.layoutCache[node]; exists {
        nodeID := "no-id"
        if id, ok := node.Attributes["id"]; ok {
            nodeID = id
        }
        fmt.Printf("LAYOUT CACHE HIT: Reusing box for <%s id='%s'> at cached position (%.1f, %.1f)\n",
            node.TagName, nodeID, cachedBox.X, cachedBox.Y)
        return cachedBox
    }

    // ... EXISTING layoutNode code continues here ...
```

### Step 4: Store box in cache before returning

Find the **end of layoutNode** where it returns the box. Just before `return box`, add:

```go
    // ... existing layout logic ...

    // CACHE STORE: Save this box to prevent re-layout
    le.layoutCache[node] = box
    nodeID := "no-id"
    if id, ok := node.Attributes["id"]; ok {
        nodeID = id
    }
    fmt.Printf("LAYOUT CACHE STORE: Cached box for <%s id='%s'> at (%.1f, %.1f)\n",
        node.TagName, nodeID, box.X, box.Y)

    return box
}
```

**IMPORTANT**: Make sure to add this to **ALL** return statements in layoutNode, including:
- Early returns for `display: none`
- Returns for text nodes
- Returns for table elements
- Any other early exit paths

### Step 5: Test inline-box-001

Run the test and check results:

```bash
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v 2>&1 | tee test-output.txt
```

**What to look for**:

1. **Debug output should show**:
   ```
   LAYOUT CACHE STORE: Cached box for <div id='div1'> at (0.0, 51.2)
   LAYOUT CACHE HIT: Reusing box for <div id='div1'> at cached position (0.0, 51.2)
   ```

2. **Orange div should appear only once**:
   ```bash
   grep "Drawing <div>.*orange" test-output.txt
   ```
   Should show only ONE orange div, at Y=70.4 (not Y=63.2)

3. **Error should drop**:
   ```
   REFTEST FAIL: 3070/160000 pixels differ (1.9%)  ← BEFORE
   REFTEST PASS                                     ← AFTER (ideal)
   ```
   Or at least significantly reduced error (<1%)

### Step 6: Run full test suite

```bash
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | tail -50
```

Check the summary line:
```
Summary: X/51 passed, Y failed
```

**Expected**: 35+/51 passing (up from 34/51 baseline)

**Critical**: NO REGRESSIONS - if any previously passing tests now fail, investigate cache issues.

## Validation Checklist

- [ ] `layoutCache` field added to `LayoutEngine` struct
- [ ] Cache initialized in `Layout()` function
- [ ] Cache check added at start of `layoutNode()`
- [ ] Cache store added before all `return box` statements
- [ ] inline-box-001.xht error reduced (<1% or PASS)
- [ ] Debug output shows cache hits
- [ ] No regressions in other tests (34+/51 passing)

## Debugging Tips

### If cache doesn't work (still see duplicates)

1. **Check if layoutNode has multiple return paths**: Add cache store to ALL returns
2. **Verify cache is initialized**: Should see "Initialized layout cache" in debug output
3. **Check node pointer equality**: The cache uses pointer comparison, so same node = same pointer

### If tests regress

1. **Cache might be too aggressive**: Some nodes might legitimately need re-layout
2. **Add defensive check**: Compare cached position with requested position
3. **Consider selective caching**: Only cache nodes that are known to cause duplicates

### If still not passing

The layout cache fixes the **duplicate layout** problem. If the test still fails:
- The orange div might still be at the wrong Y position (line-height calculation bug)
- In that case, proceed to **Phase 2** of the fix document
- Or investigate other positioning issues

## Expected Behavior After Fix

### inline-box-001.xht should show:

**Rendering** (from debug output):
```
STEP 3: Blocks (3 elements):
  - <body> at (0.0, 0.0)
  - <p> at (0.0, 16.0)
  - <div> at (0.0, 70.4) bg=orange  ← Only ONE orange div, at correct Y!

STEP 5: Inlines (X elements):
  - Fragment 1 (div#div1) at (0.0, 51.2) with left/top/bottom borders
  - Fragment 2 (div#div1) at (0.0, 89.6) with right/top/bottom borders
```

**Layout cache debug**:
```
LAYOUT CACHE STORE: Cached box for <p id='no-id'> at (0.0, 16.0)
LAYOUT CACHE STORE: Cached box for <div id='div1'> at (0.0, 51.2)
LAYOUT CACHE STORE: Cached box for <div id='no-id'> at (0.0, 70.4)  ← Orange div
LAYOUT CACHE HIT: Reusing box for <div id='div1'> at cached position (0.0, 51.2)  ← Second attempt blocked!
```

## If You Get Stuck

### Problem: Can't find where to add cache check

**Solution**: Search for `func (le *LayoutEngine) layoutNode`:
```bash
grep -n "func.*layoutNode" pkg/layout/*.go
```

### Problem: Don't know which return statements need cache store

**Solution**: Search for all returns in layoutNode:
```bash
grep -n "return.*box" pkg/layout/layout_block.go | grep -v "Parent"
```

Each `return box` or `return childBox` needs the cache store before it.

### Problem: Tests still failing with same error

**Solution**:
1. Verify cache is actually being used (check debug output)
2. If cache hits are happening but test still fails, the problem might be:
   - Wrong Y position in the FIRST layout (need Phase 2 fix)
   - Different issue entirely (check pixel diff)

## Success Criteria

### Minimum Success
- [ ] inline-box-001.xht error reduced from 1.9% to <1%
- [ ] Debug output shows cache hits preventing duplicate layouts
- [ ] No test regressions

### Full Success
- [ ] inline-box-001.xht PASSES (<0.3% error)
- [ ] 35+/51 tests passing (improvement from 34/51)
- [ ] Orange div appears only once in rendering
- [ ] Fragment 1 at Y=51.2, orange div at Y=70.4, Fragment 2 at Y=89.6

## After Completion

If this fixes the test:
1. Commit the changes with message: "Fix: Add layout cache to prevent duplicate layouts of DOM nodes"
2. Update MEMORY.md with the fix
3. Consider implementing Phase 2 (line-height tracking) as cleanup

If this partially fixes but doesn't fully pass:
1. Document what improved (error percentage reduction)
2. Proceed to Phase 2: Fix line-height tracking
3. The cache is still valuable - keep it even if more work is needed

Good luck! This is a clean, architectural fix that will improve both correctness and performance. 🚀
