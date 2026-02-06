# Multi-Pass Integration - Quick Start

**TL;DR**: Integrate multi-pass inline layout by including pseudo-elements in batching

## 🎯 The Problem

```
box-generation-001.xht has this structure:
<div>Block box</div>
<span>Inline box</span>           ← positions first
<span style="float:left">Float</span>  ← appears later but should affect previous line
```

Single-pass layout can't handle this. Multi-pass layout CAN (we proved it: 6.5% → 2.6%).

## 🚨 Why Previous Attempt Failed

```
Current layout flow:
1. Generate ::before         ← OUTSIDE batching
2. Layout children           ← Batching here
3. Generate ::after          ← OUTSIDE batching

Result: Pseudo-elements can't interact with floats correctly
before-after-floated-001.xht: PASS → 21.8% error
```

## ✅ The Solution

**Include pseudo-elements IN the batch:**

```go
// Collect ALL inline content (pseudo-elements + children)
inlineChildren := []InlineChild{
    {PseudoBox: beforeBox},        // Include ::before
    {Node: child1},                 // Regular children
    {Node: child2},
    {PseudoBox: afterBox},         // Include ::after
}

// Now batch them together with multi-pass
if batchHasFloats(inlineChildren) {
    boxes := le.LayoutInlineBatch(inlineChildren, ...)
}
```

## 📋 Implementation Steps

1. **Create abstraction** for pseudo-elements + children (1 hour)
2. **Refactor child loop** to include pseudo-elements (2 hours)
3. **Update LayoutInlineBatch** to handle both (1 hour)
4. **Test incrementally** at each step (2 hours)
5. **Run full suite** and debug (1-2 hours)

**Total estimate: 6-8 hours**

## 🎯 Success Criteria

- box-generation-001: 6.5% → <2% (70%+ improvement)
- before-after-floated-001: PASS (maintained)
- No other regressions
- Tests complete in <3 minutes

## 🚀 Quick Commands

```bash
# Build
go build ./cmd/l14open

# Test target
./l14open pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht /tmp/test.png 400 400

# Test regression
./l14open pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht /tmp/test.png 400 400

# Full suite
go test ./pkg/visualtest -run TestWPTReftests -v > results.txt
grep "SUMMARY\|Total:\|Passed:" results.txt
```

## 📚 Full Documentation

See `docs/multipass-integration-guide.md` for complete guide.

## 💡 Key Insight

> The multi-pass algorithm works perfectly. The blocker is purely architectural - pseudo-elements need to be in the batch. Fix that, and you fix the tests.

**You got this!** 🎉
