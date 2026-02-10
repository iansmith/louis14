# Next Steps: Phase 2 Implementation + Regression Investigation

## Executive Summary

**Phase 1 (Tactical Fix) Status: ✅ COMMITTED**
- Fixed core line-height bug in multi-pass inline layout
- inline-box-001.xht: 1.9% → 0.5% error (73% improvement)
- Orange div at correct Y position (70.4)
- Test suite: 33/51 passing

**Next Steps:**
1. **Phase 2**: Implement architectural fix (separate line-height tracking)
2. **Investigate**: before-after-floated-001.xht regression (was passing, now 3.8% error)

---

## Task 1: Phase 2 - Separate Line-Height Tracking

### Overview

Replace the tactical fix with a proper architectural solution that separates:
- **Content height**: From text, images, atomic inlines
- **Line box height**: From inline element line-heights and vertical-align

This matches the CSS specification's model and sets up for future CSS 3.0 features.

### Current State (Phase 1 Tactical Fix)

**What we have:**
```go
// Track if line has content
hasContentOnLine := false

// Only advance for lines with content
if hasContentOnLine && currentLineMaxHeight > 0 {
    currentY = currentLineY + currentLineMaxHeight
} else {
    // Preserve currentLineMaxHeight for new line
}

// Set line-height at OpenTag
if frag.Style != nil {
    lineHeight := frag.Style.GetLineHeight()
    if lineHeight > currentLineMaxHeight {
        currentLineMaxHeight = lineHeight
    }
}
```

**Problems:**
- Mixes content height and line-box height in one variable
- Relies on "has content" flag to prevent wrong advancement
- Not clear what currentLineMaxHeight represents at any given time
- Fragile - easy to break with future changes

### Target State (Phase 2 Solution)

**Architecture:**
```go
// New struct to track line metrics separately
type LineMetrics struct {
    contentHeight float64  // Max height from text, images, atomics
    lineBoxHeight float64  // Min height from inline element line-heights
}

func (lm *LineMetrics) EffectiveHeight() float64 {
    return max(lm.contentHeight, lm.lineBoxHeight)
}
```

**Usage:**
```go
// Replace currentLineMaxHeight with LineMetrics
lineMetrics := &LineMetrics{}

// At OpenTag
lineHeight := frag.Style.GetLineHeight()
if lineHeight > lineMetrics.lineBoxHeight {
    lineMetrics.lineBoxHeight = lineHeight
}

// At TEXT/Atomic
if box.Height > lineMetrics.contentHeight {
    lineMetrics.contentHeight = box.Height
}

// When advancing Y
currentY += lineMetrics.EffectiveHeight()
```

### Implementation Plan

#### Step 1: Define LineMetrics Struct (30 min)

**File**: `pkg/layout/layout_inline_multipass.go`

Add after the `inlineSpan` struct definition (around line 729):

```go
// LineMetrics tracks line box height separately from content height
// This matches CSS 2.1 §10.8.1: line box height is independent of content height
type LineMetrics struct {
    // Maximum height of content on this line (text, images, atomic inlines)
    // This is the "natural" height of the tallest box
    contentHeight float64

    // Minimum height from inline element line-heights
    // This ensures line boxes have sufficient height even for small text
    lineBoxHeight float64

    // Track if line has any actual content (not just OpenTag markers)
    // Used to determine if we should advance Y for this line
    hasContent bool
}

// EffectiveHeight returns the height to use for Y advancement
// Per CSS spec: line box height is the max of content height and line-height
func (lm *LineMetrics) EffectiveHeight() float64 {
    return max(lm.contentHeight, lm.lineBoxHeight)
}

// Reset clears metrics for a new line
// preserveLineBoxHeight: if true, keeps line-box height from open inline elements
func (lm *LineMetrics) Reset(preserveLineBoxHeight bool) {
    lm.contentHeight = 0
    lm.hasContent = false
    if !preserveLineBoxHeight {
        lm.lineBoxHeight = 0
    }
}
```

#### Step 2: Replace currentLineMaxHeight Variable (1 hour)

**File**: `pkg/layout/layout_inline_multipass.go`

**Replace** (around line 724):
```go
currentLineMaxHeight := 0.0   // Track maximum height on current line
hasContentOnLine := false     // Track whether current line has actual content
```

**With**:
```go
lineMetrics := &LineMetrics{}  // Track line box metrics
```

#### Step 3: Update OpenTag Processing (15 min)

**File**: `pkg/layout/layout_inline_multipass.go`

**Find** (around line 844-856):
```go
// FIX: Track inline element's line-height contribution
if frag.Style != nil {
    lineHeight := frag.Style.GetLineHeight()
    if lineHeight > currentLineMaxHeight {
        currentLineMaxHeight = lineHeight
        fmt.Printf("  OpenTag <%s>: Set currentLineMaxHeight to %.1f (line-height)\n",
            frag.Node.TagName, lineHeight)
    }
}
```

**Replace with**:
```go
// Track inline element's line-height contribution to line box
if frag.Style != nil {
    lineHeight := frag.Style.GetLineHeight()
    if lineHeight > lineMetrics.lineBoxHeight {
        lineMetrics.lineBoxHeight = lineHeight
        fmt.Printf("  OpenTag <%s>: Set line-box height to %.1f (line-height)\n",
            frag.Node.TagName, lineHeight)
    }
}
```

#### Step 4: Update TEXT/Content Processing (15 min)

**Find** (around line 1163-1173):
```go
// Track maximum height on this line
fmt.Printf("  Box.Height=%.1f, currentLineMaxHeight before=%.1f\n", box.Height, currentLineMaxHeight)
if box.Height > currentLineMaxHeight {
    currentLineMaxHeight = box.Height
}

// Mark that this line has actual content
if frag.Type == FragmentText || frag.Type == FragmentAtomic || frag.Type == FragmentBlockChild {
    hasContentOnLine = true
}
```

**Replace with**:
```go
// Track content height and mark that line has content
fmt.Printf("  Box.Height=%.1f, content=%.1f, lineBox=%.1f\n",
    box.Height, lineMetrics.contentHeight, lineMetrics.lineBoxHeight)

if frag.Type == FragmentText || frag.Type == FragmentAtomic || frag.Type == FragmentBlockChild {
    lineMetrics.hasContent = true
    if box.Height > lineMetrics.contentHeight {
        lineMetrics.contentHeight = box.Height
    }
}

fmt.Printf("  Line metrics: content=%.1f, lineBox=%.1f, effective=%.1f, hasContent=%v\n",
    lineMetrics.contentHeight, lineMetrics.lineBoxHeight,
    lineMetrics.EffectiveHeight(), lineMetrics.hasContent)
```

#### Step 5: Update Line Break Detection (30 min)

**Find** (around line 1143-1162):
```go
if frag.Position.Y != currentLineY {
    // Advance currentY past the previous line
    // FIX: Only advance if the previous line had actual content
    if hasContentOnLine && currentLineMaxHeight > 0 {
        fmt.Printf("  Line break: advancing by %.1f\n", currentLineMaxHeight)
        currentY = currentLineY + currentLineMaxHeight
        lastFinalizedLineHeight = currentLineMaxHeight
        currentLineMaxHeight = 0
        hasContentOnLine = false
    } else if currentLineMaxHeight > 0 {
        fmt.Printf("  Line break: preserving line-height %.1f\n", currentLineMaxHeight)
    }
    currentLineY = frag.Position.Y
}
```

**Replace with**:
```go
if frag.Position.Y != currentLineY {
    // Advance currentY past the previous line
    effectiveHeight := lineMetrics.EffectiveHeight()

    if lineMetrics.hasContent && effectiveHeight > 0 {
        fmt.Printf("  Line break: Y %.1f → %.1f, advancing by %.1f (content=%.1f, lineBox=%.1f)\n",
            currentLineY, frag.Position.Y, effectiveHeight,
            lineMetrics.contentHeight, lineMetrics.lineBoxHeight)
        currentY = currentLineY + effectiveHeight
        lastFinalizedLineHeight = effectiveHeight
        lineMetrics.Reset(false) // Clear both content and line-box height
    } else if effectiveHeight > 0 {
        fmt.Printf("  Line break: Y %.1f → %.1f, NO content - preserving lineBox=%.1f\n",
            currentLineY, frag.Position.Y, lineMetrics.lineBoxHeight)
        lineMetrics.Reset(true) // Preserve line-box height from open inlines
    }
    currentLineY = frag.Position.Y
}
```

#### Step 6: Update Block Child Finalization (15 min)

**Find** (around line 746-760):
```go
if hasContentOnLine && currentLineMaxHeight > 0 {
    fmt.Printf("  Finalizing line before block: %.1f\n", currentLineMaxHeight)
    currentY = currentY + currentLineMaxHeight
    lastFinalizedLineHeight = currentLineMaxHeight
    currentLineMaxHeight = 0
} else if currentLineMaxHeight > 0 {
    fmt.Printf("  Skipping finalization: no content\n")
}
hasContentOnLine = false
```

**Replace with**:
```go
effectiveHeight := lineMetrics.EffectiveHeight()

if lineMetrics.hasContent && effectiveHeight > 0 {
    fmt.Printf("  Finalizing line before block: advancing by %.1f (content=%.1f, lineBox=%.1f)\n",
        effectiveHeight, lineMetrics.contentHeight, lineMetrics.lineBoxHeight)
    currentY = currentY + effectiveHeight
    lastFinalizedLineHeight = effectiveHeight
} else if effectiveHeight > 0 {
    fmt.Printf("  Skipping line finalization: no content (lineBox=%.1f)\n",
        lineMetrics.lineBoxHeight)
}
lineMetrics.Reset(false) // Clear for content after block child
```

#### Step 7: Update lastFinalizedLineHeight Usage (15 min)

Search for all uses of `lastFinalizedLineHeight` and update to use the effective height concept.

#### Step 8: Test and Validate (30 min)

**Test 1: inline-box-001.xht**
```bash
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v
```

**Expected**:
- Orange div at Y=70.4 ✓
- Error ≤ 0.5%
- Debug shows: "content=12.0, lineBox=19.2, effective=19.2"

**Test 2: Full suite**
```bash
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"
```

**Expected**:
- 33+/51 passing (no regressions from Phase 1)

**Test 3: before-after-floated-001.xht**
```bash
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

**Expected**:
- Should PASS (if regression was caused by Phase 1 tactical fix)

### Expected Benefits

1. **Correctness**: Matches CSS specification model
2. **Clarity**: Clear separation of concerns
3. **Maintainability**: Easy to understand and debug
4. **Future-proof**: Supports CSS 3.0 features:
   - `line-height-step`
   - `leading-trim`
   - Flexbox/Grid baseline alignment
   - Vertical writing modes

### Success Criteria

- [ ] LineMetrics struct defined with clear documentation
- [ ] All uses of currentLineMaxHeight replaced
- [ ] inline-box-001.xht: error ≤ 0.5%
- [ ] No regressions in test suite (33+/51 passing)
- [ ] Code is clearer and more maintainable than Phase 1
- [ ] Debug output clearly shows content vs line-box heights

---

## Task 2: Investigate before-after-floated-001.xht Regression

### Background

**MEMORY.md states:**
> before-after-floated-001.xht: FAIL (22.8%) → PASS ✓

But after Phase 1 tactical fix:
> before-after-floated-001.xht: FAIL (3.8%)

**Question**: Did Phase 1 break this test, or was it already broken?

### Investigation Steps

#### Step 1: Verify Baseline (10 min)

```bash
# Revert Phase 1 tactical fix
git stash

# Test without our changes
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v 2>&1 | grep "REFTEST"

# Restore changes
git stash pop
```

**If PASS without our changes**: Phase 1 broke it (proceed to Step 2)
**If FAIL without our changes**: Already broken (check MEMORY.md date)

#### Step 2: Identify What Changed (30 min)

**Compare behavior with/without Phase 1:**

```bash
# With Phase 1
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v 2>&1 > with-phase1.log

# Without Phase 1 (after git stash)
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v 2>&1 > without-phase1.log

# Compare
diff with-phase1.log without-phase1.log | grep -E "Finalizing|Line break|OpenTag.*Set"
```

**Look for:**
- Different Y positions for elements
- Different line-height values being used
- Different advancement calculations

#### Step 3: Analyze Test Structure (20 min)

Read the test HTML:
```bash
cat pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht
```

**Identify:**
- Does it use `::before` or `::after` with floats?
- Are there inline elements with line-heights?
- Are there block-in-inline structures?

#### Step 4: Hypothesis Formation (15 min)

**Possible causes:**

**Hypothesis A**: Our `hasContentOnLine` flag incorrectly considers pseudo-elements as "no content"
- Pseudo-elements should count as content
- Check if `FragmentType` for pseudo-elements is being handled

**Hypothesis B**: Preserving line-height across line breaks interferes with float positioning
- Floats might create implicit line breaks
- Line-height preservation might advance Y incorrectly

**Hypothesis C**: OpenTag line-height tracking interacts badly with floated pseudo-elements
- Floated elements should not contribute to line-height
- Check if we're setting lineBoxHeight for floated elements

#### Step 5: Debug Output Analysis (30 min)

Add targeted debug output:

```go
// In before-after-floated-001 test, add:
if frag.Node != nil && strings.Contains(getNodeName(frag.Node), "before") {
    fmt.Printf("  🔍 BEFORE PSEUDO: type=%v, Y=%.1f, hasContent=%v, lineMetrics=%+v\n",
        frag.Type, frag.Position.Y, lineMetrics.hasContent, lineMetrics)
}
```

Run and analyze where the regression occurs.

#### Step 6: Fix (time varies)

Based on hypothesis:

**If Hypothesis A**: Add pseudo-element types to hasContent check
```go
if frag.Type == FragmentText || frag.Type == FragmentAtomic ||
   frag.Type == FragmentBlockChild || isPseudoElement(frag) {
    lineMetrics.hasContent = true
}
```

**If Hypothesis B**: Don't preserve line-height when floats are involved
```go
hasFloatsOnLine := false // track this
lineMetrics.Reset(!hasFloatsOnLine) // Only preserve if no floats
```

**If Hypothesis C**: Don't set lineBoxHeight for floated elements
```go
if frag.Style != nil {
    floatType := frag.Style.GetFloat()
    if floatType == css.FloatNone { // Only if not floated
        lineHeight := frag.Style.GetLineHeight()
        if lineHeight > lineMetrics.lineBoxHeight {
            lineMetrics.lineBoxHeight = lineHeight
        }
    }
}
```

#### Step 7: Validate Fix (15 min)

```bash
# Test the specific regression
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# Ensure inline-box-001 still works
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v

# Check full suite
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"
```

**Success criteria:**
- before-after-floated-001.xht: PASS
- inline-box-001.xht: ≤ 0.5% error
- Test suite: 34+/51 passing (gained one test)

### Expected Outcomes

**Best case**: Fix regression while maintaining inline-box-001 improvement
- Result: 34/51 tests passing

**Good case**: Understand root cause, document limitation
- Result: 33/51 tests passing, but know why

**Acceptable case**: Regression is pre-existing (not caused by Phase 1)
- Result: 33/51 tests passing, add to known issues list

---

## Task 3: Update MEMORY.md

After completing Tasks 1 and 2, update MEMORY.md with:

```markdown
## Phase 2: Separate Line-Height Tracking (CRITICAL - 2026-XX-XX) ✓

### Problem
Phase 1 tactical fix worked but was fragile - mixed content height and line-box height
in one variable (currentLineMaxHeight), relying on hasContentOnLine flag to prevent
wrong advancement.

### Solution
Implemented LineMetrics struct with separate tracking:
- contentHeight: From text, images, atomic inlines
- lineBoxHeight: From inline element line-heights
- EffectiveHeight(): max(contentHeight, lineBoxHeight)

### Results
- inline-box-001.xht: Remains at ≤0.5% error ✓
- before-after-floated-001.xht: Regression fixed (if applicable)
- Code is cleaner and more maintainable
- Foundation for CSS 3.0 features

### Files Modified
- pkg/layout/layout_inline_multipass.go: LineMetrics implementation

### Lessons
- Proper separation of concerns prevents subtle bugs
- CSS spec has separate concepts for good reason
- Clear data structures make code self-documenting
```

---

## Quick Start for Next Session

```bash
# 1. Check current state
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v

# 2. Start with Phase 2 implementation
# Follow steps 1-8 above

# 3. Then investigate regression
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# 4. Validate complete solution
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | tail -60
```

## Time Estimate

- **Phase 2 Implementation**: 2-3 hours
- **Regression Investigation**: 1-2 hours
- **Total**: ~4-5 hours for complete solution

## Files to Review

Before starting:
1. `docs/INLINE-BOX-001-ROOT-CAUSE-ANALYSIS.md` - Understand the architecture
2. `pkg/layout/layout_inline_multipass.go` - Current implementation
3. This document - Implementation plan

Good luck! The hard part (understanding the problem) is done. Phase 2 is just clean refactoring.
