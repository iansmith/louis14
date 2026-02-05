# Investigation: before-after-floated-001.xht Test Failure

**Test Status:** 31.9% failure (improved from 39.6%)
**Date:** 2026-02-05

## Executive Summary

I've identified the root causes of the test failure and made one critical fix. Two remaining issues need refactoring investments to fully resolve.

## Problems Identified & Status

### 1. ✅ FIXED: Images Not Loading (Critical)

**Cause:** The renderer wasn't configured with an image fetcher, so images in pseudo-element content couldn't be loaded during rendering.

**Fix:** Added `renderer.SetImageFetcher(fetcher)` in `pkg/visualtest/helpers.go`

**Impact:** Reduced failure from 39.6% to 31.9% (7.7% improvement)

**Commit:** 32df24c

---

### 2. 🔴 OPEN: Float Drop Logic for Pseudo-Elements (Major - ~20-25% impact)

**Problem:** Floated ::after pseudo-elements are being dropped to the next line when they should remain inline.

**Example from test:**
```
Current:  ::before at y=6, ::after at y=38 (dropped)
Expected: ::before at y=6, ::after at y=6 (inline)
```

**Root Cause:** The `getFloatDropY()` function in `pkg/layout/layout.go:2007` is designed for block-level floats. It checks if a float fits within available width and drops it to the next line if it doesn't. However, floated pseudo-elements in inline formatting contexts should:
1. Position inline at the current line position
2. Allow overflow if needed
3. Not trigger line drops

**Current Code Path (lines 1036-1073):**
```go
// Phase 11: Generate ::after pseudo-element
afterBox := le.generatePseudoElement(node, "after", inlineCtx.LineX, inlineCtx.LineY, ...)
if afterFloat != css.FloatNone {
    floatWidth := le.getTotalWidth(afterBox)
    floatY := le.getFloatDropY(afterFloat, floatWidth, inlineCtx.LineY, childAvailableWidth)  // ← PROBLEM
    // ... positioning logic using floatY ...
}
```

**Refactoring Options:**

**Option A: Skip drop logic for pseudo-element floats (Recommended)**
```go
// For pseudo-element floats, position inline without dropping
floatY := inlineCtx.LineY  // Use current line, don't drop
```

**Pros:**
- Simple, minimal change
- Aligns with CSS inline formatting context behavior
- No risk of breaking other float logic

**Cons:**
- Might need similar fix for ::before

**Option B: Add context parameter to getFloatDropY**
```go
func (le *LayoutEngine) getFloatDropY(..., allowOverflow bool) float64 {
    if allowOverflow {
        return startY  // Don't drop for inline context
    }
    // ... existing logic ...
}
```

**Pros:**
- Handles both pseudo-elements uniformly
- Explicit about inline vs block float behavior

**Cons:**
- More invasive change
- Need to identify all call sites

**Recommendation:** Option A for ::after (and ::before if needed). This is the minimal, safe change that directly addresses the problem.

---

### 3. 🔴 OPEN: Whitespace Trimming (Minor - ~2-3% impact)

**Problem:** "Inner" text is being trimmed when it should preserve surrounding whitespace.

**Example:**
```
Current:  'Inner' (38px wide, no spaces)
Expected: ' Inner ' (47px wide, with leading/trailing spaces)
```

**Root Cause:** The `layoutTextNode()` function (pkg/layout/layout.go:1678-1727) has whitespace trimming logic for block containers. This may be incorrectly trimming spaces in inline contexts.

**Investigation Needed:**
1. Check if trimming logic is triggering for inline parents
2. Verify whitespace preservation rules for inline formatting contexts
3. Test if issue affects other tests

**Potential Fix Area:** Lines 1691-1723 in `pkg/layout/layout.go`

---

## Implementation Priority

### High Priority: Fix Float Drop Logic
**Estimated Impact:** 20-25% failure reduction
**Complexity:** Low (single line change)
**Risk:** Low (localized to pseudo-element float positioning)

**Implementation:**
1. Change line 1041 in `pkg/layout/layout.go` from:
   ```go
   floatY := le.getFloatDropY(afterFloat, floatWidth, inlineCtx.LineY, childAvailableWidth)
   ```
   to:
   ```go
   // Pseudo-element floats position inline, don't drop to new line
   floatY := inlineCtx.LineY
   ```

2. Test with `before-after-floated-001.xht`

3. Apply same fix to ::before if needed (around line 640)

### Medium Priority: Fix Whitespace Handling
**Estimated Impact:** 2-3% failure reduction
**Complexity:** Medium (need to understand trim conditions)
**Risk:** Medium (whitespace changes can affect many tests)

**Investigation:**
1. Add debug logging to see why "Inner" text is trimmed
2. Check if parent display type is being detected correctly
3. Verify inline formatting context identification

### Combined Impact Estimate
With both fixes: **Expected final result: 5-10% failure**

Remaining differences likely due to:
- Font rendering variations
- Anti-aliasing differences
- Sub-pixel positioning
- Browser-specific layout quirks

---

## Test Details

**Test File:** `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`

**Test Content:** 4 divs with different float combinations:
1. ::before left, ::after left
2. ::before left, ::after right
3. ::before right, ::after left
4. ::before right, ::after right

**Pseudo-element Content:**
- `::before`: counter(ctr) + image + open-quote + "Before " + attr(class)
- `::after`: counter(ctr) + image + "After " + attr(class) + close-quote

**Features Tested:**
- ✅ Counters (counter-reset, counter-increment, counter())
- ✅ Images in content (url())
- ✅ Quotes (open-quote, close-quote)
- ✅ Attributes (attr(class))
- ✅ Float positioning (float: left/right on pseudo-elements)
- ⚠️ Float drop behavior (broken)
- ⚠️ Whitespace preservation (broken)

---

## Recommendations

1. **Implement float drop fix** (Option A) - High ROI, low risk
2. **Run full reftest suite** after fix to ensure no regressions
3. **Investigate whitespace trimming** with targeted test cases
4. **Consider adding a dedicated test** for floated pseudo-elements to prevent regressions

## Related Code Sections

**Float Positioning:**
- `pkg/layout/layout.go:1036-1076` - ::after float positioning
- `pkg/layout/layout.go:634-648` - ::before float positioning
- `pkg/layout/layout.go:2007-2043` - getFloatDropY function

**Whitespace Handling:**
- `pkg/layout/layout.go:1678-1727` - layoutTextNode trimming logic

**Image Loading:**
- `pkg/visualtest/helpers.go:21-53` - RenderHTMLToFileWithBase (fixed)
- `pkg/images/loader.go:205-213` - GetImageDimensionsWithFetcher

---

## Next Steps

1. Implement float drop fix for ::after pseudo-elements
2. Test and verify 20-25% improvement
3. Apply same fix to ::before if needed
4. Investigate and fix whitespace trimming
5. Final reftest validation
