# Multi-Pass Integration - Quick Start for Next Session

**TL;DR**: Integrate multi-pass layout to fix float tests, especially box-generation-001 (5.4% → <1%).

---

## 🎯 Goal

Get box-generation-001.xht to **<1% error** without breaking before-after-floated-001.xht (must stay PASS).

---

## 📊 Current Baseline (IMPORTANT: Verify First!)

```bash
# ALWAYS verify baseline before starting
git stash  # Clear any WIP
go build ./cmd/l14open
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee baseline.txt
```

**Expected baseline:**
- box-generation-001.xht: ~5.4% error
- before-after-floated-001.xht: **PASS**
- Overall: Check pass count

---

## 🧪 Test After Each Change

```bash
# Build
go build ./cmd/l14open

# Target (should improve)
go test ./pkg/visualtest -run "TestWPTReftests/box-display/box-generation-001" -v

# Regression check (MUST NOT BREAK)
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v

# If before-after-floated-001 breaks, STOP and fix before continuing
```

---

## ✅ Success Criteria

**Must achieve ALL:**
- box-generation-001: <2% error (stretch: <1%)
- before-after-floated-001: **PASS** (no regression!)
- No other test regressions
- Tests complete in <3 minutes

---

## 🚨 Critical Warnings

### ❌ DON'T:
1. **Don't modify normal flow variables** - Will break before-after-floated-001
2. **Don't generate pseudo-elements for detection** - Causes duplication
3. **Don't batch pseudo-elements yet** - Too complex, causes regressions
4. **Don't use goto** - Causes variable scope issues

### ✅ DO:
1. **Test incrementally** - After every change
2. **Verify baseline first** - Always start clean
3. **Focus on box-generation-001** - No pseudo-elements, clean test case
4. **Keep normal flow untouched** - Add batching as separate path

---

## 🛠️ Recommended Approach

Add batching as a **completely separate early-return path**:

```go
func (le *LayoutEngine) layoutNode(...) *Box {
    // Existing code unchanged...

    // NEW: Check if multi-pass should be used
    if shouldUseMultiPass(node, display, computedStyles) {
        return le.layoutNodeMultiPass(node, box, ...)
    }

    // ALL existing normal flow code unchanged
    // Don't move any variables
    // Don't modify any logic
}

func shouldUseMultiPass(node, display, computedStyles) bool {
    // Only if: block/inline + NO pseudo-elements + has floats
    // Don't generate anything - just check
}

func (le *LayoutEngine) layoutNodeMultiPass(node, box, ...) *Box {
    // Separate function for multi-pass layout
    // Call LayoutInlineBatch
    // Return fully laid out box
}
```

**Why this works:**
- Normal flow 100% untouched
- No variable scope issues
- Easy to debug
- Can be toggled on/off

---

## 🐛 If Things Go Wrong

### before-after-floated-001 Breaks?
1. You modified normal flow - revert
2. You generated pseudo-elements early - don't do that
3. You moved variable declarations - put them back

### box-generation-001 Doesn't Improve?
1. Is batching being triggered? Add logging
2. Is the float being detected? Check hasFloatsRecursive
3. Is multi-pass actually running? Verify LayoutInlineBatch is called

### Tests Hang?
1. Infinite loop in batching code
2. Check retry limit (max 3)
3. Use `go test -timeout 5m`

---

## 📁 Key Files

**Integration point:**
- `pkg/layout/layout.go` lines 380-1300 (layoutNode)

**Multi-pass (working, don't change):**
- `pkg/layout/layout.go` lines 4173-4787

**Test files:**
- `pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht`
- `pkg/visualtest/testdata/wpt-css2/generated-content/before-after-floated-001.xht`

**Full details:**
- `docs/MULTIPASS-INTEGRATION-V2-PROMPT.md`

---

## 🚀 Checklist

Before you start:
- [ ] Read this document
- [ ] Verify baseline: `git stash && go test ...`
- [ ] Understand target: box-generation-001 <1%
- [ ] Understand constraint: before-after-floated-001 must PASS
- [ ] Know the approach: separate path, don't touch normal flow

During development:
- [ ] Test after EVERY change
- [ ] Check both target and regression tests
- [ ] If regression appears, stop and fix immediately

Before committing:
- [ ] Run full test suite
- [ ] Verify no regressions
- [ ] Document results

---

**You got this! Focus on box-generation-001 first, keep normal flow pristine, test constantly.** 💪
