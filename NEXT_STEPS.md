# Next Steps - Louis14 Browser Engine

**Last Updated:** 2026-02-06
**Current Branch:** `multipass-integration`
**Test Suite Status:** 31/51 passing (60.8%)

---

## Session Summary - What Was Accomplished

### ✅ Task #1: Fixed before-after-floated-001.xht (COMPLETE)
- **Change:** Removed `!hasPseudo` check from multi-pass conditional (3 line changes)
- **Result:** 22.8% error → **PASS (0% error)** ✓
- **Impact:** Gained 1 test (30/51 → 31/51)
- **Commit:** `6936525 - Fix: Remove !hasPseudo check to enable multi-pass for pseudo-elements`

### ⚠️ Task #2: Improved empty-inline-002.xht (PARTIAL)
- **Change:** Added proper dimensions for empty inline elements
- **Result:** 60.3% error → 51.1% error (9.2% improvement)
- **Impact:** No test suite change (still failing, but better)
- **Commit:** `dc1fc54 - Fix: Add proper dimensions for empty inline elements`

**Key Fix:** Empty `<span></span>` elements now calculate dimensions from:
- Width: border-left + padding-left + padding-right + border-right
- Height: line-height (with em/% units resolved) + vertical border/padding

---

## Priority 1: Complete empty-inline-002.xht Fix

**Current Status:** 51.1% error remaining (was 60.3%)
**Test File:** `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht`
**What Works:** Empty span has correct dimensions (250x350px)
**What's Broken:** Layout/positioning still incorrect

### Investigation Needed

The span has correct dimensions but the overall layout is wrong. Likely issues:

1. **Container Height Problem**
   - The div containing the empty span should expand to contain it
   - Check if div2 height (400px) is accounting for span's height correctly
   - CSS 2.1 §10.6.3: Block containers must contain inline content line boxes

2. **Span Positioning**
   - Span renders at (0.0, 153.0) - verify this is correct
   - Should be positioned within div2's content area
   - Check if margin (100px) is being applied correctly

3. **Background/Border Rendering**
   - Span has `background: green` and `border: 25px solid green`
   - These should create the green overlay to cover red test areas
   - Verify background is being painted at correct coordinates

### Debug Commands

```bash
# Run the failing test
go test ./pkg/visualtest -v -run "TestWPTReftests/linebox/empty-inline-002" 2>&1 | grep "REFTEST"

# Check layout details
go test ./pkg/visualtest -v -run "TestWPTReftests/linebox/empty-inline-002" 2>&1 | grep -E "(Result: Box at|Rendering inline <span>)"

# Compare test vs reference images
open output/reftests/empty-inline-002_test.png
open output/reftests/empty-inline-002_ref.png
open output/reftests/empty-inline-002_diff.png
```

### Next Steps

1. **Analyze the visual diff** - Open the diff PNG to see where red is showing through
2. **Check container height** - Verify div2 is tall enough to contain the 350px span
3. **Verify margin application** - The span has `margin: 100px` which affects positioning
4. **Check stacking context** - div3 has `z-index: -1` and should be behind the green
5. **Review reference implementation** - Compare test vs reference structure carefully

**Files to Examine:**
- `pkg/layout/layout.go:1509-1568` - Empty inline wrapper box creation
- `pkg/layout/layout.go:2900-3100` - Block box height calculation
- Test: `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht`
- Reference: `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002-ref.xht`

---

## Priority 2: Fix Other High-Error Tests

After completing empty-inline-002, work on these failing tests in order of error percentage:

### Top 10 Failing Tests (by error %)

To find the current top failing tests:
```bash
go test ./pkg/visualtest -v -run TestWPT 2>&1 > /tmp/test_output.txt

python3 << 'EOF'
with open('/tmp/test_output.txt', 'r') as f:
    lines = f.readlines()

failures = []
for i, line in enumerate(lines):
    if '--- FAIL: TestWPTReftests/' in line:
        test_name = line.split('TestWPTReftests/')[1].split()[0]
        for j in range(i+1, min(i+10, len(lines))):
            if 'REFTEST FAIL:' in lines[j]:
                if '(' in lines[j] and '%' in lines[j]:
                    pct_str = lines[j].split('(')[1].split('%')[0]
                    try:
                        pct = float(pct_str)
                        failures.append((pct, test_name))
                    except ValueError:
                        pass
                break

failures.sort(reverse=True)
print("Top 10 failing tests by error percentage:\n")
for i, (pct, test) in enumerate(failures[:10], 1):
    print(f"{i:2}. {pct:5.1f}% - {test}")
EOF
```

### Known Failing Tests (from previous sessions)

Based on git status and test output:
- `empty-inline-002.xht` - 51.1% (in progress)
- `border-005.xht` - 42.5%
- `border-006.xht` - 42.5%
- `anonymous-boxes-inheritance-001.xht` - 12.0%
- Plus 16 others with smaller errors

---

## Priority 3: Enable Multi-Pass Globally (Future Work)

**Context:** Multi-pass layout is currently only enabled in tests via `SetUseMultiPass(true)`. It's not enabled in production (l14open, l14show).

**Rationale:** The new multi-pass pipeline is more robust than the old single-pass code. Removing the `!hasPseudo` check proved this - we gained a test with no regressions.

### Steps to Enable Globally

1. **Test thoroughly** - Ensure 31/51 tests remain passing
2. **Enable in CLI tools**
   - Modify `cmd/l14open/main.go` to call `engine.SetUseMultiPass(true)`
   - Modify `cmd/l14show/main.go` to call `engine.SetUseMultiPass(true)`
3. **Remove the flag** - Once stable, remove `useMultiPass` flag and make it default
4. **Delete old code** - Remove `LayoutInlineBatch` and single-pass layout code

**Benefits:**
- Better handling of floats and inline content
- Cleaner separation of concerns (collect → break → construct)
- Foundation for advanced CSS features (vertical-align, etc.)

**Risks:**
- May expose edge cases in pseudo-element handling
- Need to verify real-world websites still render correctly

---

## Architecture Notes

### Multi-Pass Inline Layout Pipeline

**Three Phases:**
1. **Collect** (`collectInlineItemsClean`) - Flatten DOM to InlineItem list
2. **Break** (`LayoutInlineContent`) - Decide line breaks (with retry when floats change)
3. **Construct** (`constructLine`) - Create positioned fragments from items

**Key Insight:** Separation of "deciding what goes on each line" from "positioning boxes" enables retry when floats change available width.

### Empty Inline Elements

**CSS Spec Requirements:**
- CSS 2.1 §10.3.1: Inline elements have dimensions from border/padding even if empty
- CSS 2.1 §10.8.1: Empty inline elements establish line box height from line-height

**Implementation:** `pkg/layout/layout.go:1509-1568`
- Detect empty inline: OpenTag and CloseTag at same X position
- Calculate width: border-left + padding-left + padding-right + border-right
- Calculate height: line-height (resolve em/%) + vertical border/padding

### Pseudo-Element Handling

**Current State:** Pseudo-elements NOT explicitly integrated into multi-pass collection phase.

**Why It Works:** The new `LayoutInlineContentToBoxes` pipeline handles layout better than old `LayoutInlineBatch`, even without explicit pseudo-element support. The test passes because the overall layout quality is higher.

**Future Work:** Fully integrate pseudo-elements into the collection phase for proper multi-pass handling (see MEMORY.md for details).

---

## Test Commands Reference

### Run Single Test
```bash
go test ./pkg/visualtest -v -run "TestWPTReftests/<path/to/test>" 2>&1 | grep "REFTEST"
```

### Run Full Suite
```bash
go test ./pkg/visualtest -v -run TestWPT 2>&1 | grep "Summary:"
```

### Check for Regressions
```bash
# Before changes
go test ./pkg/visualtest -v -run TestWPT 2>&1 | grep "Summary:" > /tmp/before.txt

# After changes
go test ./pkg/visualtest -v -run TestWPT 2>&1 | grep "Summary:" > /tmp/after.txt

# Compare
diff /tmp/before.txt /tmp/after.txt
```

### View Test Images
```bash
# Test output vs reference
open output/reftests/<test-name>_test.png
open output/reftests/<test-name>_ref.png

# Diff (red pixels = differences)
open output/reftests/<test-name>_diff.png
```

---

## Code Structure Reference

### Key Files

**Main Layout Engine:** `pkg/layout/layout.go` (~8000 lines)
- `layoutNode()` - Main layout entry point (line ~2100)
- `LayoutInlineContentToBoxes()` - Multi-pass wrapper (line ~1273)
- `collectInlineItemsClean()` - Phase 1: Item collection (line ~887)
- `LayoutInlineContent()` - Phase 2: Line breaking (line ~600)
- `constructLine()` - Phase 3: Fragment creation (line ~931)

**Test Infrastructure:** `pkg/visualtest/`
- `reftest_runner_test.go` - Main test runner
- `testdata/wpt-css2/` - W3C CSS 2.1 test suite
- `helpers.go` - Test helpers (sets `SetUseMultiPass(true)`)

### Multi-Pass Data Structures

**InlineItem** - Abstract representation of inline content
```go
type InlineItem struct {
    Type   InlineItemType  // Text, Float, OpenTag, CloseTag, etc.
    Node   *html.Node
    Style  *css.Style
    Text   string  // For text items
    Width  float64
    Height float64
}
```

**Fragment** - Positioned piece of content ready for rendering
```go
type Fragment struct {
    Type     FragmentType  // Text, Float, Inline, BlockChild
    Node     *html.Node
    Style    *css.Style
    Position Position  // Already positioned!
    Size     Size
}
```

**Box** - Final rendering unit (what gets drawn)
```go
type Box struct {
    Node   *html.Node
    Style  *css.Style
    X, Y   float64  // Position
    Width, Height float64  // Content dimensions
    Border, Padding, Margin BoxEdge
    Children []*Box
}
```

---

## Memory/Context Reference

**Auto Memory:** `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`

Key sections:
- Multi-Pass !hasPseudo Fix (2026-02-06) - This session's work
- Inline Width Calculation Bug (2026-02-05) - Background
- Multi-Pass Architecture (2026-02-05) - Design rationale

**Continuation Prompts:**
- `CONTINUATION_PROMPT.md` - Documents from previous session (now SUCCESS status)

---

## Git Workflow

**Current Branch:** `multipass-integration`

### Create Commit
```bash
git add -A
git commit -m "<type>: <short description>

<detailed explanation>

Changes:
- file:line - what changed

Results:
- test: before → after

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

### Push to Remote (when ready)
```bash
git push origin multipass-integration
```

### Create PR (when tests pass)
```bash
gh pr create --title "Fix: Multi-pass layout improvements" --body "$(cat <<'EOF'
## Summary
- Fixed before-after-floated-001.xht (22.8% → PASS)
- Improved empty-inline-002.xht (60.3% → 51.1%)
- Test suite: 30/51 → 31/51 passing

## Changes
1. Removed !hasPseudo check to enable multi-pass for pseudo-elements
2. Added proper dimensions for empty inline elements

## Test Results
[Paste test output]

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Success Criteria

### For Completion of Current Work

- [ ] empty-inline-002.xht: Error < 5% (currently 51.1%)
- [ ] Test suite: ≥ 31/51 passing (no regressions)
- [ ] Code committed and documented

### For Multi-Pass Integration Complete

- [ ] Test suite: ≥ 35/51 passing (70% pass rate)
- [ ] Multi-pass enabled globally in CLI tools
- [ ] Old single-pass code removed
- [ ] All commits squashed and PR created

---

## Common Pitfalls & Lessons Learned

1. **Don't guess at solutions** - The continuation plan expected 5 complex tasks, but removing one guard check was sufficient. Measure twice, cut once.

2. **em units need context** - `css.ParseLength("1em")` returns 16px (default), not the contextual value. Always compute em/% relative to font-size.

3. **Empty inline elements are real** - CSS requires them to have dimensions from border/padding and establish line box height, even with no content.

4. **Test incrementally** - Run tests after each change to catch regressions early.

5. **Multi-pass is robust** - The new pipeline handles edge cases better than old code, even without explicit feature support.

---

## Questions to Resolve

1. **Why is empty-inline-002 still at 51% error?**
   - Span dimensions are correct (250x350)
   - Need to investigate positioning, container height, z-index stacking

2. **Should we integrate pseudo-elements into collection phase?**
   - Current: Pseudo-elements handled outside multi-pass
   - Works for now, but may need proper integration for complex cases

3. **When to enable multi-pass globally?**
   - Need confidence that edge cases are handled
   - Consider a feature flag or gradual rollout

---

## Contact & Resources

- **Project:** Louis14 Browser Engine
- **Repository:** `/Users/iansmith/louis14`
- **Test Suite:** W3C CSS 2.1 Reftests
- **Documentation:** https://www.w3.org/TR/CSS21/

---

**Ready to Continue?**

1. Start with Priority 1 (complete empty-inline-002 fix)
2. Use debug commands to investigate remaining 51% error
3. Test incrementally and commit progress
4. Update this document with findings
