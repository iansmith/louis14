# Prompt for Next Claude Session

⚠️ **UPDATED**: See `MULTIPASS-INTEGRATION-V2-PROMPT.md` for the complete guide.

Copy and paste this message into a new Claude Code chat:

---

Hi Claude! I need you to improve the multi-pass inline layout integration for the Louis14 browser engine.

## Quick Start

Read these files IN ORDER:
1. **`docs/NEXT-SESSION-QUICK-START.md`** - Start here (5 min read)
2. **`docs/MULTIPASS-INTEGRATION-V2-PROMPT.md`** - Complete guide (15 min read)
3. **`docs/multipass-integration-guide.md`** - Architecture reference
4. **Memory**: `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`

## Context Summary

## Background

The Louis14 browser has a fully implemented multi-pass inline layout system (based on Chromium's Blink LayoutNG) located in `pkg/layout/layout.go` lines 4173-4787. It uses a three-phase approach:
1. CollectInlineItems - flatten inline content
2. BreakLines - line breaking with retry when floats change width
3. ConstructLineBoxes - create positioned boxes

**The algorithm works** - we proved it can improve box-generation-001.xht from 6.5% error to 2.6% error (60% improvement).

## The Problem

A previous integration attempt failed because **pseudo-elements (::before and ::after) were excluded from batching**. They're generated outside the child loop (lines 722 and 1197), so when we batched inline children, pseudo-elements positioned incorrectly.

Result: before-after-floated-001.xht regressed from PASS to 21.8% error.

## What You Need to Do

**Integrate multi-pass layout by including pseudo-elements in the batching flow.**

### Architecture

The solution is to collect ALL inline content (pseudo-elements + children) into a single batch:

```go
// Instead of:
beforeBox := generatePseudoElement(...)
for child in children { layout(child) }
afterBox := generatePseudoElement(...)

// Do:
inlineChildren := []
if beforeBox := generatePseudoElement(...) {
    inlineChildren.append(beforeBox)
}
for child in children {
    inlineChildren.append(child)
}
if afterBox := generatePseudoElement(...) {
    inlineChildren.append(afterBox)
}
// Now batch them together with multi-pass
```

### Success Criteria

- [ ] box-generation-001.xht: <2% error (currently 6.5%)
- [ ] before-after-floated-001.xht: PASS maintained (currently PASS)
- [ ] No regressions on 16 currently passing WPT tests
- [ ] Overall WPT pass rate: >35% (currently 31%)
- [ ] Full test suite completes in <3 minutes (no hangs)

### Testing Commands

```bash
# Build
go build ./cmd/l14open

# Test target test
go test ./pkg/visualtest -run "box-generation-001" -v

# Test regression protection
go test ./pkg/visualtest -run "before-after-floated-001" -v

# Full WPT suite
go test ./pkg/visualtest -run TestWPTReftests -v 2>&1 | tee results.txt
```

### Key Files

- `pkg/layout/layout.go` lines 380-1240: layoutNode function (where integration happens)
- `pkg/layout/layout.go` lines 4173-4787: Multi-pass infrastructure (already working)
- `pkg/visualtest/testdata/wpt-css2/`: Test files

### Documentation

Read these files in order:
1. `docs/multipass-quick-start.md` - Overview (5 min read)
2. `docs/multipass-integration-guide.md` - Complete guide (15 min read)
3. `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` - Previous attempts and learnings

### Critical Warnings

1. **Avoid infinite loops**: Use index-based loop with proper increment/skip
2. **Update inline context**: After batching, update LineX/LineY for next content
3. **Test incrementally**: After each change, test before moving on
4. **Handle both types**: Pseudo-elements are already boxes, children are nodes

## Critical Information

**Previous attempt results (2026-02-05 session):**
- ✅ Multi-pass infrastructure works correctly
- ✅ Can improve tests when properly integrated (60% improvement shown)
- ❌ Integration broke before-after-floated-001 (PASS → 22.8% error)
- ❌ Root cause: Modified normal flow variables, causing side effects

**Current state:**
- Branch: `multipass-integration`
- Multi-pass code: Lines 4173-4787 (working, don't modify)
- Integration needed in: layoutNode function (lines 380-1300)
- Code may have WIP changes - verify baseline with `git stash` first

## Your Task

**Primary goal**: Get box-generation-001.xht from 5.4% error to <1% error

**Critical constraint**: Do NOT break before-after-floated-001.xht (must stay PASS)

**Approach**:
1. **Verify baseline first** - `git stash && go test ...`
2. Read `NEXT-SESSION-QUICK-START.md` - Understand targets and constraints
3. Read `MULTIPASS-INTEGRATION-V2-PROMPT.md` - Full context and approach
4. **Implement separate path** - Don't modify normal flow
5. **Test constantly** - After every change, check both tests
6. If before-after-floated-001 breaks, stop and fix immediately

**Key insight**: The multi-pass algorithm works perfectly. The challenge is integrating it without side effects on the normal flow. Use an early-return strategy to keep paths completely separate.

**Ready to start? Focus on box-generation-001, protect before-after-floated-001, test constantly!** 🚀

---

END OF PROMPT - Everything above this line should be pasted into the new Claude chat.
