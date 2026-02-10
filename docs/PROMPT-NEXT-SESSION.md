# Next Session Prompt

## Context

You've successfully implemented **Phase 1 (Tactical Fix)** for the multi-pass inline layout line-height bug:
- ✅ Fixed inline-box-001.xht: 1.9% → 0.5% error (73% improvement)
- ✅ Orange div at correct Y position (70.4 instead of 63.2)
- ✅ Committed as WIP
- ⚠️ Potential regression: before-after-floated-001.xht (3.8% error)

## Your Tasks

### Task 1: Implement Phase 2 - Separate Line-Height Tracking (2-3 hours)

**Goal**: Replace the tactical fix with a proper architectural solution.

**What to do**:
1. Read `/docs/NEXT-STEPS-PHASE-2.md` - complete implementation guide
2. Create `LineMetrics` struct to separate:
   - `contentHeight` (from text/images)
   - `lineBoxHeight` (from inline element line-heights)
3. Replace all uses of `currentLineMaxHeight` with `lineMetrics`
4. Test: inline-box-001.xht should remain ≤0.5% error

**Why**: Phase 1 is fragile and mixes concerns. Phase 2 matches CSS spec and supports future features (flexbox baseline alignment, CSS 3.0 line-height features).

**Files to modify**:
- `pkg/layout/layout_inline_multipass.go` (follow step-by-step guide in docs)

---

### Task 2: Investigate before-after-floated-001.xht Regression (1-2 hours)

**Goal**: Determine if Phase 1 broke this test (should be PASS per MEMORY.md) and fix if so.

**What to do**:
1. Verify baseline: Does test pass WITHOUT Phase 1? (`git stash`, test, `git stash pop`)
2. If Phase 1 broke it:
   - Compare debug output with/without Phase 1
   - Identify what changed (Y positions, line-heights)
   - Form hypothesis (see Task 2 in NEXT-STEPS-PHASE-2.md)
   - Fix and validate
3. If already broken: Document in known issues

**Hypotheses to check**:
- Hypothesis A: `hasContentOnLine` doesn't handle pseudo-elements correctly
- Hypothesis B: Line-height preservation interferes with float positioning
- Hypothesis C: OpenTag line-height tracking affects floated pseudo-elements

---

## Success Criteria

- [ ] Phase 2 implemented: `LineMetrics` struct in use
- [ ] inline-box-001.xht: ≤0.5% error (no regression)
- [ ] before-after-floated-001.xht: PASS (regression fixed) or explained
- [ ] Test suite: 34+/51 passing (up from 33)
- [ ] Code cleaner than Phase 1 (separate concerns)
- [ ] MEMORY.md updated with Phase 2 entry

---

## Quick Start Commands

```bash
# Check current state
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v

# Implement Phase 2 (follow NEXT-STEPS-PHASE-2.md)
# ... make changes ...

# Test Phase 2
go test ./pkg/visualtest -run "TestWPTReftests/linebox/inline-box-001" -v
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"

# Investigate regression
go test ./pkg/visualtest -run "TestWPTReftests/generated-content/before-after-floated-001" -v
```

---

## Key Documents

1. **`docs/NEXT-STEPS-PHASE-2.md`** - Complete implementation guide with code snippets
2. **`docs/INLINE-BOX-001-ROOT-CAUSE-ANALYSIS.md`** - Architectural deep-dive
3. **`docs/INVESTIGATION-SUMMARY.md`** - What we learned from Phase 1

---

## Note

The hard part (understanding the problem) is done. Phase 2 is clean refactoring following a clear plan. Estimated 4-5 hours total for both tasks. You've got this! 🚀
