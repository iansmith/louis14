# Multi-Pass Inline Layout Integration - Version 2

**Objective**: Fix multi-pass integration to improve float-related WPT tests to <1% error while maintaining all currently passing tests.

---

## Current State (2026-02-05 - End of Session)

### What Exists

The multi-pass inline layout infrastructure is **fully implemented and proven to work**:

- **Location**: `pkg/layout/layout.go` lines 4173-4787
- **Architecture**: Chromium Blink LayoutNG-style three-phase approach
  1. CollectInlineItems - flatten inline content
  2. BreakLines - line breaking with retry when floats change width
  3. ConstructLineBoxes - create positioned boxes
- **Proof of concept**: Standalone tests showed 60% improvement (6.5% → 2.6%) on box-generation-001

### Current Integration Attempt

An integration was attempted that added:
- `InlineChild` abstraction to handle both nodes and pseudo-element boxes
- Modified `LayoutInlineBatch` to accept `[]InlineChild` instead of `[]*html.Node`
- Batching logic in `layoutNode` to trigger multi-pass when floats detected
- Conservative approach: only batch when NO pseudo-elements present

### Test Results - Current Branch State

**Baseline (before integration attempt):**
```bash
git stash  # Remove integration changes
go test ./pkg/visualtest -run TestWPTReftests -v
```
Results:
- box-generation-001.xht: 5.4% error
- box-generation-002.xht: 10.3% error
- before-after-floated-001.xht: **PASS** (0% error)
- Overall: Multiple tests passing, but exact count needs verification

**After integration attempt:**
```bash
git stash pop  # Apply integration changes
go test ./pkg/visualtest -run TestWPTReftests -v
```
Results:
- box-generation-001.xht: 5.6% error (slight regression)
- before-after-floated-001.xht: **22.8% error** (major regression from PASS!)
- Overall: 37/51 passing (72.5%)

### The Problem

**Integration broke a critical test**: before-after-floated-001 went from PASS → 22.8% error.

**Root cause**: Changes to `layoutNode` are affecting the normal single-pass flow even when batching isn't triggered. Specifically:
- Moving variable declarations (beforeBox, afterBox, fragment tracking) to accommodate `goto` statements
- Potential scope/lifetime issues with these variables
- Inadvertent changes to normal flow behavior

---

## Target Tests for Improvement

These are the tests that should benefit from multi-pass layout:

### Priority 1: Float Tests (No Pseudo-Elements)

**box-generation-001.xht** - PRIMARY TARGET
- Current: 5.4% error
- Target: <1% error
- Structure: Block box, inline box, floated span
- Float appears AFTER inline content in document order
- No pseudo-elements (clean test case)
- Test file: `pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht`

**box-generation-002.xht** - SECONDARY TARGET
- Current: 10.3% error
- Target: <5% error
- Similar structure to 001
- Test file: `pkg/visualtest/testdata/wpt-css2/box-display/box-generation-002.xht`

**float-no-content-beside-001.html**
- Current: 1.8% error
- Target: <1% error
- Test file: `pkg/visualtest/testdata/wpt-css2/floats/float-no-content-beside-001.html`

### Priority 2: Regression Protection

**before-after-floated-001.xht** - MUST NOT REGRESS
- Baseline: **PASS** (0% error)
- Current (with integration): 22.8% error ❌
- **CRITICAL**: This test MUST remain passing
- Has pseudo-elements with floats
- Test file: `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`

**All other passing tests** - MUST NOT REGRESS
- Use full test suite to verify no regressions
- Command: `go test ./pkg/visualtest -run TestWPTReftests -v`

---

## Success Criteria

### Minimum Requirements (Must Achieve All)

- ✅ box-generation-001.xht: <2% error (currently 5.4%)
- ✅ before-after-floated-001.xht: PASS (must maintain 0% error)
- ✅ No regressions on any currently passing tests
- ✅ Full test suite completes in <3 minutes (no hangs)

### Stretch Goals

- 🎯 box-generation-001.xht: <1% error
- 🎯 box-generation-002.xht: <5% error
- 🎯 Overall WPT pass rate improvement

---

## Testing Strategy

### Step 1: Verify Baseline

**ALWAYS start by verifying the baseline without integration changes:**

```bash
# Stash any work in progress
git stash

# Build and test baseline
go build ./cmd/l14open

# Test critical targets
go test ./pkg/visualtest -run "TestWPTReftests/box-display/box-generation-001" -v
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# Full suite
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee baseline-results.txt
grep "REFTEST PASS" baseline-results.txt | wc -l
grep "REFTEST FAIL" baseline-results.txt | wc -l
```

### Step 2: Incremental Testing During Development

After each change:

```bash
# Quick compile check
go build ./cmd/l14open

# Test target (should improve)
go test ./pkg/visualtest -run "TestWPTReftests/box-display/box-generation-001" -v

# Test regression protection (must not break)
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

### Step 3: Full Validation

Before committing:

```bash
# Full WPT suite
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee final-results.txt

# Count results
echo "Passing: $(grep -c 'REFTEST PASS' final-results.txt)"
echo "Failing: $(grep -c 'REFTEST FAIL' final-results.txt)"

# Check specific targets
grep "box-generation-001\|before-after-floated-001" final-results.txt
```

### Step 4: Visual Inspection

For failed tests, inspect output images:

```bash
# Output images are in output/reftests/
open output/reftests/box-generation-001_test.png
open output/reftests/box-generation-001_ref.png
open output/reftests/box-generation-001_diff.png
```

---

## Key Learnings - What NOT to Do

### ❌ Don't Modify Normal Flow Variables

**Problem**: Moving variable declarations to accommodate `goto` affects normal flow.

**Example of what went wrong:**
```go
// BAD: Moved declarations before batching code
var beforeBox, afterBox *Box
isInlineParent := display == css.DisplayInline
// ... batching code ...
goto afterNormalInlineFlow

// Normal flow uses these variables but they're now in wrong scope
```

**Solution**: Keep normal flow completely untouched. Add batching as an early-return path.

### ❌ Don't Generate Pseudo-Elements for Detection

**Problem**: Generating pseudo-elements to check if they exist causes duplication/interference.

**Example of what went wrong:**
```go
// BAD: Generate to check, then generate again in normal flow
beforeBox = generatePseudoElement(...)
if beforeBox != nil {
    // Skip batching
}
// Normal flow also generates: creates duplicate or misses processing
```

**Solution**: Check for pseudo-elements using CSS computed styles, not by generating them.

### ❌ Don't Batch Pseudo-Elements Yet

**Problem**: Pseudo-elements have complex generated content (counters, images, quotes, attr values) already laid out.

**Why it fails**: Treating them as atomic items in multi-pass loses ability to interact with floats correctly.

**Solution**: Focus on tests WITHOUT pseudo-elements first. Batching pseudo-elements requires deeper refactoring.

---

## Recommended Approach

### Option 1: Minimal Non-Invasive Integration (RECOMMENDED)

Add batching as a completely separate code path that doesn't touch normal flow:

```go
func (le *LayoutEngine) layoutNode(...) {
    // ALL existing variable declarations stay here unchanged
    // Don't move anything

    // NEW: Check if we should use multi-pass (BEFORE any normal flow)
    if shouldUseMultiPassLayout(node, display, computedStyles) {
        return le.layoutNodeWithMultiPass(node, box, ...)
    }

    // ALL existing normal flow code unchanged below
    // No goto, no moved variables, no modifications
}

func (le *LayoutEngine) shouldUseMultiPassLayout(node, display, computedStyles) bool {
    // Only batch if:
    // 1. Display is block or inline
    // 2. NO pseudo-elements (check computed styles)
    // 3. Has floats in children

    // Don't generate anything, just check
    if display != css.DisplayBlock && display != css.DisplayInline {
        return false
    }

    // Check for pseudo-elements using computed styles (don't generate)
    if nodeStyle := computedStyles[node]; nodeStyle != nil {
        if content, ok := nodeStyle.Get("content"); ok && content != "" && content != "none" {
            // Has ::before or ::after content - skip batching
            return false
        }
    }

    // Check if any children have floats
    for _, child := range node.Children {
        if hasFloatsRecursive(child, computedStyles) {
            return true
        }
    }

    return false
}

func (le *LayoutEngine) layoutNodeWithMultiPass(node, box, ...) *Box {
    // Complete multi-pass layout for this node
    // Collect children into InlineChild array
    // Call LayoutInlineBatch
    // Return fully laid out box
}
```

**Benefits:**
- Normal flow completely untouched
- Clear separation of concerns
- Easy to debug and verify
- Can be disabled with a single condition

### Option 2: Fix Current Integration

If you want to fix the current approach:

1. **Revert variable moves**: Put all variable declarations back where they were in normal flow
2. **Avoid goto**: Use early return instead of goto to skip normal flow
3. **Don't generate pseudo-elements early**: Check for them without generating
4. **Test incrementally**: After each fix, test before-after-floated-001 to ensure no regression

---

## Debugging Tips

### If before-after-floated-001 Regresses

1. **Check variable scope**: Are beforeBox/afterBox being used correctly in normal flow?
2. **Check for duplication**: Is pseudo-element generation happening twice?
3. **Check fragment tracking**: Are isInlineParent, currentFragment, etc. still working?
4. **Add logging**: Print when batching is triggered vs skipped
5. **Compare outputs**: Diff the test output images between baseline and current

### If box-generation-001 Doesn't Improve

1. **Verify batching triggers**: Add logging to confirm multi-pass is being used
2. **Check float detection**: Ensure hasFloatsRecursive finds the floated span
3. **Check line breaking**: Verify BreakLines is accounting for floats
4. **Check box construction**: Verify ConstructLineBoxes positions content correctly
5. **Test retry logic**: Ensure retry mechanism triggers when floats change width

### If Tests Hang

1. **Check for infinite loops**: Especially in index-based loops
2. **Check retry limit**: LayoutInlineBatch has max 3 retries
3. **Add timeout**: `go test -timeout 5m ...`
4. **Check float list**: Ensure floats aren't growing unbounded

---

## Quick Reference Commands

```bash
# Build
go build ./cmd/l14open

# Single test
go test ./pkg/visualtest -run "TestWPTReftests/box-display/box-generation-001" -v

# Full suite
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee results.txt

# Count results
grep -c "REFTEST PASS" results.txt
grep -c "REFTEST FAIL" results.txt

# Check specific tests
grep "box-generation-001\|before-after-floated-001\|box-generation-002" results.txt

# Compare with baseline
git stash
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee baseline.txt
git stash pop
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee current.txt
diff baseline.txt current.txt

# Visual inspection
open output/reftests/*_diff.png
```

---

## Expected Outcome

After successful integration:

**Target Test Results:**
- box-generation-001.xht: **<1% error** (from 5.4%)
- box-generation-002.xht: **<5% error** (from 10.3%)
- before-after-floated-001.xht: **PASS** (maintained)
- float-no-content-beside-001.html: **<1% error** (from 1.8%)

**Overall Impact:**
- Several additional float-related tests passing
- No regressions on any existing passing tests
- Proof that multi-pass integration works for real WPT tests

---

## Files to Focus On

**Main integration point:**
- `pkg/layout/layout.go` lines 380-1300 (layoutNode function)

**Multi-pass infrastructure (already working, don't modify):**
- `pkg/layout/layout.go` lines 4173-4787

**Test files:**
- `pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht`
- `pkg/visualtest/testdata/wpt-css2/box-display/box-generation-002.xht`
- `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`
- `pkg/visualtest/testdata/wpt-css2/floats/float-no-content-beside-001.html`

**Documentation:**
- `docs/multipass-quick-start.md` - Overview
- `docs/multipass-integration-guide.md` - Detailed guide
- `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` - Learnings

---

## Ready to Start?

1. ✅ Verify baseline results first
2. ✅ Read this entire document
3. ✅ Understand the target tests and success criteria
4. ✅ Choose an integration approach (Option 1 recommended)
5. ✅ Test incrementally - never make multiple changes without testing
6. ✅ Focus on box-generation-001 (no pseudo-elements) first
7. ✅ Protect before-after-floated-001 (must not regress)

**The multi-pass infrastructure works. The challenge is integration without side effects. You can do this!** 🚀
