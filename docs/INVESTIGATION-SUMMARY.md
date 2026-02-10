# Investigation Summary: inline-box-001.xht Bug

## What We Discovered

The original diagnostic documents (`INLINE-BOX-DUPLICATE-LAYOUT-FIX.md` and `NEXT-SESSION-PROMPT.md`) were **based on incorrect assumptions**. Here's what actually happened:

### Original Hypothesis (WRONG)
- Multiple layout passes create duplicate boxes for the same content
- Solution: Add layout cache to prevent duplicate layouts

### Actual Reality (CORRECT)
- **NO duplicate layouts occur** - each node is laid out exactly once
- The problem: Multi-pass calculates **wrong Y position** (63.2 instead of 70.4)
- Root cause: Using text content height (12.0px) instead of line-height (19.2px)

## Evidence

### Layout Cache Experiment

I implemented the full layout cache as described in the original documents:
1. Added `layoutCache map[*html.Node]*Box` to LayoutEngine
2. Checked cache at start of layoutNode()
3. Stored boxes in cache before return

**Results**:
```
LAYOUT CACHE STORE: Cached box for <div id='no-id'> at (0.0, 63.2)  ← Orange div
```

**No cache hits!** The orange div was laid out exactly **once** at Y=63.2.

### Rendering Comparison

**Test file**: Orange div at Y=63.2 (wrong)
**Reference file**: Orange div at Y=70.4 (correct)
**Difference**: 7.2px = 19.2 - 12.0 (line-height vs text height)

## Corrected Understanding

### The Real Bug

File: `pkg/layout/layout_inline_multipass.go` (around line 746)

When multi-pass encounters a block child inside an inline element:
1. It finalizes the current line
2. Advances currentY by `currentLineMaxHeight`
3. **BUG**: `currentLineMaxHeight` = 12.0 (text content) instead of 19.2 (line-height)

### The Real Fix

Track the inline element's line-height when processing OpenTag:

```go
// At OpenTag (around line 838)
if isOpenTag {
    span := &inlineSpan{node: frag.Node, style: frag.Style}
    inlineStack = append(inlineStack, span)

    // NEW: Track line-height
    if frag.Style != nil {
        lineHeight := frag.Style.GetLineHeight()
        if lineHeight > currentLineMaxHeight {
            currentLineMaxHeight = lineHeight
        }
    }
}
```

## Next Steps

See the new diagnostic document: **`INLINE-BOX-001-LINE-HEIGHT-BUG.md`**

This document contains:
- ✅ Correct root cause analysis
- ✅ Evidence from investigation
- ✅ Exact fix with code locations
- ✅ Implementation plan (3 phases, ~1 hour total)
- ✅ Testing strategy
- ✅ Expected outcomes

## Why the Original Analysis Was Wrong

### What Led to the Mistake

1. **Document source**: The original diagnostic assumed 10 invocations of multi-pass creating duplicate boxes
2. **Actual behavior**: Only 4 invocations per file, no duplicates
3. **Changed codebase**: Code may have changed since the diagnostic was written

### What the Cache Revealed

The cache experiment was **valuable** even though it didn't fix the bug:
- Proved definitively that no duplicates exist
- Showed the orange div is laid out once at wrong position
- Redirected investigation to Y position calculation

## Time Investment

- **Investigation**: ~90 minutes (cache implementation + testing)
- **Discovery**: Cache showed no duplicates, wrong Y position
- **Analysis**: 7.2px difference → line-height bug
- **Documentation**: 30 minutes (new diagnostic)

**Total**: ~2 hours to find correct root cause

## Files Created/Modified

### New Files
- ✅ `docs/INLINE-BOX-001-LINE-HEIGHT-BUG.md` - Correct diagnostic
- ✅ `docs/INVESTIGATION-SUMMARY.md` - This summary

### Modified Files (Reverted)
- ❌ `pkg/layout/types.go` - Cache field (reverted)
- ❌ `pkg/layout/layout_main.go` - Cache init (reverted)
- ❌ `pkg/layout/layout_block.go` - Cache check/store (reverted)

### Obsolete Files
- ⚠️  `docs/INLINE-BOX-DUPLICATE-LAYOUT-FIX.md` - Based on wrong assumption
- ⚠️  `docs/NEXT-SESSION-PROMPT.md` - Based on wrong assumption

## Key Takeaways

1. **Question assumptions**: Just because a document says something doesn't mean it's current
2. **Instrument the code**: The cache experiment gave us hard data
3. **Look for patterns**: 7.2px = 19.2 - 12.0 was a smoking gun
4. **Verify, don't assume**: We thought there were duplicates - there weren't

## Ready to Fix?

The fix is straightforward and low-risk:
- **5 lines of code** in one location
- **30 minutes** to implement and test
- **Expected result**: Test passes, no regressions

Would you like me to proceed with implementing the line-height fix?
