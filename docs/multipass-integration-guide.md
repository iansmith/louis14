# Multi-Pass Inline Layout Integration Guide

**Project**: Louis14 Browser Engine
**Date**: 2026-02-05
**Status**: Ready for Implementation
**Estimated Impact**: 50%+ improvement on float-related WPT tests

---

## 🎯 Mission

Integrate the existing multi-pass inline layout infrastructure into the main layout engine to fix CSS 2.1 tests where floats appear after inline content in document order.

**Key Test Target**: `box-generation-001.xht` currently at 6.5% error, target: <1% error

---

## 📚 Background & Context

### What Already Exists

The multi-pass inline layout infrastructure is **fully implemented and working**:

**Location**: `pkg/layout/layout.go` lines 4173-4787

**Components**:
1. `LayoutInlineBatch()` - Entry point for batched layout (line 4173)
2. `CollectInlineItems()` - Phase 1: Flatten inline content (line 4327)
3. `BreakLines()` - Phase 2: Line breaking with float awareness (line 4439)
4. `constructLineBoxesWithRetry()` - Phase 3: Box construction with retry (line 4651)
5. `ConstructLineBoxes()` - Final box creation (line 4547)

**Architecture**: Based on Chromium's Blink LayoutNG three-phase approach:
- Collect inline items → Break lines → Construct boxes
- Retry mechanism when floats change available width

### Previous Integration Attempt (2026-02-05)

**What was tried**:
- Added `isInlineLevelNode()` helper to detect inline children
- Modified child loop in `layoutNode()` to batch consecutive inline children
- Called `LayoutInlineBatch()` for batches containing floats

**Results**:
- ✅ **Aggressive batching**: box-generation-001 improved 6.5% → 2.6% (60% improvement!)
- ❌ **Regression**: before-after-floated-001 went from PASS → 21.8% error
- ❌ **Root cause**: Pseudo-elements (::before, ::after) excluded from batching

**Why it failed**:
```
Current layout flow:
1. Generate ::before (line 722)
2. Loop through children (lines 809-1193)  ← Batching happened here
3. Generate ::after (line 1197)

Problem: Pseudo-elements positioned OUTSIDE the batch, breaking their layout
```

---

## 🚨 Critical Requirements for Success

### Must-Have

1. **Include pseudo-elements in batching**: ::before and ::after must participate in multi-pass layout
2. **No regressions**: All currently passing tests must continue to pass
3. **Incremental approach**: Changes must be testable at each step
4. **Performance acceptable**: Multi-pass adds overhead - measure it
5. **Handle all edge cases**: Block-in-inline, nested floats, etc.

### Success Criteria

- [ ] box-generation-001.xht: <1% error (currently 6.5%)
- [ ] box-generation-002.xht: <5% error (currently 11.4%)
- [ ] before-after-floated-001.xht: PASS (currently PASS, regressed to 21.8% in attempt)
- [ ] No regressions on 16 currently passing WPT tests
- [ ] Overall WPT pass rate: >40% (currently 31%)

---

## 🏗️ Recommended Architecture

### Option 1: Refactor Pseudo-Element Generation (RECOMMENDED)

**Approach**: Move pseudo-element generation INTO the child batching flow

**Changes needed**:
```go
// Current (lines 722-787):
beforeBox := le.generatePseudoElement(node, "before", ...)
for _, child := range node.Children { ... }
afterBox := le.generatePseudoElement(node, "after", ...)

// Proposed:
inlineChildren := []InlineChild{}
if beforeBox := le.generatePseudoElement(...); beforeBox != nil {
    inlineChildren = append(inlineChildren, InlineChild{Pseudo: beforeBox})
}
for _, child := range node.Children {
    inlineChildren = append(inlineChildren, InlineChild{Node: child})
}
if afterBox := le.generatePseudoElement(...); afterBox != nil {
    inlineChildren = append(inlineChildren, InlineChild{Pseudo: afterBox})
}
// Now batch ALL inline children including pseudo-elements
```

**Pros**:
- Pseudo-elements naturally participate in multi-pass
- Clear, understandable flow
- Minimal changes to existing multi-pass code

**Cons**:
- Requires new `InlineChild` abstraction
- More invasive changes to layoutNode
- Need to handle pseudo-element vs real node differences

### Option 2: Extend Multi-Pass to Handle Pseudo-Elements

**Approach**: Modify `CollectInlineItems()` to accept pseudo-element boxes

**Changes needed**:
```go
state := &InlineLayoutState{...}

// Add pseudo-elements as special items
if beforeBox != nil {
    state.Items = append(state.Items, &InlineItem{
        Type: InlineItemPseudoElement,
        Box: beforeBox,
        ...
    })
}

// Collect from children
for _, child := range children {
    le.CollectInlineItems(child, state, computedStyles)
}

// Add after pseudo-element
if afterBox != nil {
    state.Items = append(state.Items, &InlineItem{
        Type: InlineItemPseudoElement,
        Box: afterBox,
        ...
    })
}
```

**Pros**:
- Less invasive to layoutNode structure
- Pseudo-elements treated as special inline items
- Cleaner separation of concerns

**Cons**:
- Adds complexity to InlineItem type system
- Need to handle pre-laid-out boxes vs nodes
- May complicate line breaking logic

### Option 3: Full Inline Layout Rewrite (FUTURE)

**Approach**: Replace entire inline layout with multi-pass throughout

**Not recommended for this iteration** - too large a change. Document as future work.

---

## 📋 Implementation Plan

### Phase 1: Preparation (1-2 hours)

1. **Read all relevant code**:
   - `pkg/layout/layout.go` lines 380-1200 (layoutNode)
   - `pkg/layout/layout.go` lines 4173-4787 (multi-pass infrastructure)
   - Pseudo-element generation (lines 722-787, 1197-1240)

2. **Run baseline tests**:
   ```bash
   go test ./pkg/visualtest -run TestWPTReftests -v > baseline-results.txt
   ```
   Document: Which tests pass, which fail, by how much

3. **Create feature branch**:
   ```bash
   git checkout -b multipass-integration-v2
   ```

### Phase 2: Architecture Setup (2-3 hours)

**Choose Option 1 (Recommended) or Option 2**

#### For Option 1 (Refactor Pseudo-Element Generation):

1. **Create InlineChild abstraction**:
   ```go
   type InlineChildType int
   const (
       InlineChildNode InlineChildType = iota
       InlineChildPseudoElement
   )

   type InlineChild struct {
       Type InlineChildType
       Node *html.Node        // For Type == InlineChildNode
       PseudoBox *Box          // For Type == InlineChildPseudoElement
       PseudoType string       // "before" or "after"
   }
   ```

2. **Extract pseudo-element generation**:
   ```go
   func (le *LayoutEngine) generatePseudoElementForBatching(
       node *html.Node,
       pseudoType string,
       x, y, availableWidth float64,
       computedStyles map[*html.Node]*css.Style,
       parent *Box,
   ) *Box {
       // Return the generated pseudo-element box (already laid out)
       // Similar to existing generatePseudoElement but designed for batching
   }
   ```

3. **Test**: Ensure pseudo-element generation still works in isolation

#### For Option 2 (Extend Multi-Pass):

1. **Add InlineItemPseudoElement type**:
   ```go
   const (
       InlineItemText InlineItemType = iota
       InlineItemOpenTag
       InlineItemCloseTag
       InlineItemAtomic
       InlineItemFloat
       InlineItemControl
       InlineItemPseudoElement  // NEW
   )
   ```

2. **Extend InlineItem struct**:
   ```go
   type InlineItem struct {
       Type        InlineItemType
       Node        *html.Node
       Box         *Box        // NEW: For pre-laid-out pseudo-elements
       PseudoType  string      // NEW: "before" or "after"
       // ... existing fields
   }
   ```

3. **Test**: Ensure InlineItem can hold pseudo-element boxes

### Phase 3: Integration (3-4 hours)

1. **Modify layoutNode child loop** (starting around line 809):
   ```go
   // Collect ALL inline children including pseudo-elements
   inlineChildren := le.collectInlineChildrenWithPseudos(
       node,
       inlineCtx,
       childAvailableWidth,
       computedStyles,
       box,
   )

   // Batch consecutive inline children
   for i := 0; i < len(inlineChildren); i++ {
       // Find batch
       batchEnd := findInlineBatchEnd(inlineChildren, i)
       batch := inlineChildren[i:batchEnd]

       // Check if batch needs multi-pass (has floats)
       if batchHasFloats(batch) {
           boxes := le.LayoutInlineBatch(batch, ...)
           // Add boxes to parent
           i = batchEnd - 1
           continue
       }

       // Otherwise use old single-pass for this child
       // ...
   }
   ```

2. **Implement helper functions**:
   - `collectInlineChildrenWithPseudos()` - Gather children + pseudo-elements
   - `findInlineBatchEnd()` - Find end of inline sequence
   - `batchHasFloats()` - Check if batch contains floats

3. **Update LayoutInlineBatch** to handle InlineChild/pseudo-elements:
   ```go
   func (le *LayoutEngine) LayoutInlineBatch(
       children []InlineChild,  // Changed from []*html.Node
       box *Box,
       // ... other params
   ) []*Box {
       // Handle both regular children and pseudo-elements
   }
   ```

4. **Test after each change**:
   ```bash
   # After each modification:
   go build ./cmd/l14open
   ./l14open testdata/phase5/float-left.html /tmp/test.png 800 600

   # Run specific test:
   go test ./pkg/visualtest -run "box-generation-001" -v
   ```

### Phase 4: Testing & Debugging (2-4 hours)

1. **Test incrementally**:
   ```bash
   # Simple float test
   ./l14open testdata/phase5/float-left.html /tmp/float-left.png 800 600

   # Target test
   ./l14open pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht /tmp/box-gen.png 400 400

   # Pseudo-element test
   ./l14open pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht /tmp/before-after.png 400 400
   ```

2. **Run full WPT suite**:
   ```bash
   go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee test-results.txt
   ```

3. **Compare results**:
   ```bash
   # Count improvements
   grep "REFTEST PASS" test-results.txt | wc -l
   grep "REFTEST FAIL" test-results.txt | wc -l

   # Check specific tests
   grep "box-generation-001\|before-after-floated-001" test-results.txt
   ```

4. **Debug failures**:
   - Open generated vs reference images in `output/reftests/`
   - Add debug logging if needed (but remove before commit)
   - Check inline context updates after batching
   - Verify pseudo-elements are included in batching

### Phase 5: Refinement (1-2 hours)

1. **Performance check**:
   ```bash
   # Benchmark critical paths
   go test -bench=. -run=^$ ./pkg/layout

   # Profile if needed
   go test -cpuprofile=cpu.prof ./pkg/visualtest -run TestWPTReftests
   go tool pprof cpu.prof
   ```

2. **Code cleanup**:
   - Remove debug code
   - Add comments explaining batching logic
   - Ensure consistent naming
   - Update any affected helper functions

3. **Edge case handling**:
   - Test with nested floats
   - Test with block-in-inline
   - Test with multiple pseudo-elements
   - Test with empty content

### Phase 6: Documentation & Commit (30 min)

1. **Update MEMORY.md**:
   ```markdown
   ## Multi-Pass Integration Success (2026-02-XX)

   ### Approach
   [Describe which option you chose and why]

   ### Results
   - box-generation-001: 6.5% → X% (Y% improvement)
   - before-after-floated-001: PASS (maintained)
   - Overall WPT: 31% → Z% pass rate

   ### Key Changes
   - [List files modified]
   - [Describe architecture]

   ### Lessons
   - [What worked well]
   - [What was tricky]
   ```

2. **Create commit**:
   ```bash
   git add pkg/layout/layout.go
   git commit -m "Integrate multi-pass inline layout with pseudo-element support

   - Refactored pseudo-element generation to participate in inline batching
   - box-generation-001: 6.5% → X% error (Y% improvement)
   - before-after-floated-001: maintained PASS
   - Overall WPT pass rate: 31% → Z%

   The multi-pass layout uses a three-phase approach (collect, break, construct)
   with retry logic when floats change available width. Pseudo-elements are now
   included in the batching flow, allowing them to interact correctly with floats.

   Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
   ```

---

## ⚠️ Known Pitfalls & How to Avoid Them

### Pitfall 1: Infinite Loops

**Problem**: Index-based loop without proper increment
```go
// BAD:
for i := 0; i < len(children); i++ {
    if shouldBatch(children[i]) {
        goto normalProcessing  // Skips increment!
    }
}
```

**Solution**: Always increment or skip properly
```go
// GOOD:
for i := 0; i < len(children); i++ {
    if shouldBatch {
        // Process batch
        i = batchEnd - 1  // Skip to end, loop will increment
        continue
    }
    // Process normally
}
```

### Pitfall 2: Stale Inline Context

**Problem**: Inline context not updated after batching
```go
// BAD:
boxes := LayoutInlineBatch(...)
// inlineCtx unchanged - ::after will position wrong
```

**Solution**: Update inline context to end of batch
```go
// GOOD:
boxes := LayoutInlineBatch(...)
if len(boxes) > 0 {
    lastBox := boxes[len(boxes)-1]
    inlineCtx.LineX = lastBox.X + lastBox.Width
    inlineCtx.LineY = lastBox.Y
    inlineCtx.LineHeight = lastBox.Height
}
```

### Pitfall 3: Mixing Pre-Laid-Out and Not-Yet-Laid-Out Content

**Problem**: Pseudo-elements are already boxes, children are nodes

**Solution**: Create clear abstraction (InlineChild) or handle both cases in CollectInlineItems

### Pitfall 4: Float Detection Missing Edge Cases

**Problem**: Only checking immediate children for floats
```go
// BAD:
hasFloats := child.Style.GetFloat() != FloatNone
```

**Solution**: Check ALL children in batch, including nested
```go
// GOOD:
func batchHasFloats(batch []InlineChild) bool {
    for _, child := range batch {
        if hasFloatsRecursive(child) {
            return true
        }
    }
    return false
}
```

### Pitfall 5: Not Testing Incrementally

**Problem**: Making all changes then testing

**Solution**: Test after EACH modification:
- After adding abstraction → test compiles
- After modifying loop → test simple case
- After adding batching → test target test
- After handling pseudo-elements → test pseudo-element test
- Finally → full test suite

---

## 🧪 Testing Strategy

### Test Pyramid

**Level 1: Unit Tests** (Quick feedback)
```bash
# Test specific rendering
./l14open testdata/phase5/float-left.html /tmp/test.png 800 600
./l14open /tmp/simple-test.html /tmp/simple.png 200 200

# Should complete in <1 second
```

**Level 2: Target Tests** (Primary metrics)
```bash
# These should show improvement
go test ./pkg/visualtest -run "box-generation-001" -v
go test ./pkg/visualtest -run "box-generation-002" -v
go test ./pkg/visualtest -run "before-after-floated-001" -v

# Should complete in <10 seconds
```

**Level 3: Full Suite** (Regression detection)
```bash
# Run full WPT suite
go test ./pkg/visualtest -run TestWPTReftests -v

# Should complete in <3 minutes
# Watch for: new failures, timeout/hangs
```

### Regression Checklist

Before committing, verify:
- [ ] Simple float tests still work
- [ ] Pseudo-element tests don't regress
- [ ] No infinite loops or hangs
- [ ] Performance acceptable (full suite <3 min)
- [ ] No new compiler warnings

---

## 📊 Expected Results

### Baseline (Current)
- box-generation-001: 6.5% error
- box-generation-002: 11.4% error
- before-after-floated-001: PASS
- Overall WPT: 16/51 passing (31%)

### Target (After Integration)
- box-generation-001: <1% error (90%+ improvement)
- box-generation-002: <5% error (55%+ improvement)
- before-after-floated-001: PASS (maintained)
- Overall WPT: >20/51 passing (>40%)

### Acceptable Compromise
- box-generation-001: <2% error (70%+ improvement)
- box-generation-002: <8% error (30%+ improvement)
- before-after-floated-001: PASS (maintained)
- Overall WPT: >18/51 passing (>35%)

If you can't achieve these targets, **stop and document why** rather than shipping a regression.

---

## 🔍 Debugging Tips

### If Tests Hang

1. Check for infinite loops in index-based iteration
2. Add timeout: `go test -timeout 5m ...`
3. Look for missing loop increments
4. Check if `batchEnd` calculation is correct

### If Positioning is Wrong

1. Open diff images: `open output/reftests/box-generation-001_*.png`
2. Check inline context updates after batching
3. Verify float offsets are applied correctly
4. Ensure pseudo-elements are in the right batch

### If Tests Regress

1. Compare before/after images in `output/reftests/`
2. Check if pseudo-elements are positioned
3. Verify non-float inline content uses old path
4. Test with batching disabled to isolate issue

### Debug Logging (Remove Before Commit)

```go
// Temporary debug logging
fmt.Printf("BATCH: Found %d inline children, %d have floats\n",
    len(batch), countFloats(batch))
fmt.Printf("INLINE CTX: LineX=%.1f LineY=%.1f after batch\n",
    inlineCtx.LineX, inlineCtx.LineY)
```

---

## 📚 Reference Materials

### Key Files
- `pkg/layout/layout.go`: Main layout engine
- `pkg/visualtest/reftest_runner_test.go`: WPT test runner
- `pkg/visualtest/testdata/wpt-css2/`: Test files

### CSS 2.1 Spec References
- [9.4.1 Block formatting contexts](https://www.w3.org/TR/CSS21/visuren.html#block-formatting)
- [9.5 Floats](https://www.w3.org/TR/CSS21/visuren.html#floats)
- [9.2.1.1 Anonymous inline boxes](https://www.w3.org/TR/CSS21/visuren.html#anonymous-inline-boxes)

### Browser Implementation References
- **Blink LayoutNG**: Three-phase inline layout (CollectInlines → LineBreaker → InlineLayoutAlgorithm)
- **Gecko**: Retry mechanisms (RedoNoPull, RedoMoreFloats, RedoNextBand)

### Previous Attempts
- See `MEMORY.md` section "Multi-Pass Integration Attempt (2026-02-05)"
- Session summary: `/tmp/session-summary-2026-02-05.md`

---

## 🎯 Success Definition

You will know you've succeeded when:

1. ✅ box-generation-001.xht improves by >50% (6.5% → <3%)
2. ✅ before-after-floated-001.xht remains PASS
3. ✅ No regressions on currently passing tests
4. ✅ Overall WPT pass rate increases to >35%
5. ✅ Full test suite completes without hangs
6. ✅ Code is clean, documented, and maintainable

If you achieve this, **you've successfully integrated multi-pass inline layout!** 🎉

---

## 💬 Final Notes

**This is achievable!** The previous attempt proved the multi-pass algorithm works (60% improvement achieved). The only blocker was architectural - pseudo-elements weren't included in batching.

**Key to success**: Include pseudo-elements in the inline batching flow from the start. Don't try to bolt them on afterwards.

**If you get stuck**: Document what you tried, what results you got, and update MEMORY.md. Even a failed attempt with good documentation is valuable for the next person.

**Good luck!** You have everything you need to succeed. The multi-pass infrastructure is solid - you just need to integrate it properly. 🚀

---

## ✅ Pre-Flight Checklist

Before starting, verify:
- [ ] You've read this entire document
- [ ] You understand the previous failure (pseudo-elements)
- [ ] You've chosen an architecture (Option 1 or 2)
- [ ] You have a testing strategy
- [ ] You know the success criteria
- [ ] You're ready to test incrementally

**Now go make it happen!** 💪
