# Writing Modes Round 6: Top 3 Improvements

Current state: 700 pass / 87 fail (89.0% pass rate) across 787 tests in `TestWPTCSS3Reftests/css-writing-modes`.

Previous round: Round 5 launched 6 agents. Target 1 (float/clear) was killed due to drift — the agent never touched the primary file (`exclusion_space.go`). This round covers T1's full task plus the incomplete work from T5 (overconstrained rel-pos) and T3 (background-position).

These three targets are **independent** (touch different subsystems) and can be worked on in parallel by separate worktree agents.

---

## Project Rules (ALL agents MUST follow these)

1. **Foundational correctness over quick wins.** Every fix must work for ALL cases. Don't chase "nearly passing" tests. If a fix doesn't generalize, it's the wrong fix.
2. **Study Blink BEFORE writing any code.** The louis14 codebase is modeled on Blink's LayoutNG. Mirror their type names, algorithm structure, and constraint-passing patterns. Each target below includes Blink references — read them first.
3. **All tests must pass at 0% diff.** A 0.5% diff is a failure just like 28%. Never dismiss failures as "font rendering" or "anti-aliasing."
4. **Test execution discipline.** Run ONLY the specific tests listed in the Verification section — do NOT run the full writing-modes suite or broader test suites.
5. **Commit and report at each milestone.** Each target below has numbered milestones — commit after each one with a clear message describing what changed and why. Do not batch all work into a single final commit.
6. **Use `GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go`** as the Go command for all build and test operations.
7. **Never use `open`** to display image files — it disrupts the user's screen.
8. **Worktree branch discipline.** When running in a worktree, commit ONLY to your worktree branch. Never commit directly to `fix/*` or `master` branches from a worktree.

---

## Target 1: Float clear/side physical-vs-logical confusion (~21 tests)

### Problem

`float: left/right` and `clear: left/right` are **physical** CSS values per spec. But the louis14 exclusion space stores float sides using a half-logical scheme — `layoutFloat()` converts `float: left` to `FloatRight` (inline-end) in RTL, but doesn't account for vertical writing modes at all. Then `ClearanceOffset()` compares the physical `clear: left` against this half-logical stored side, producing wrong results in vertical modes. Additionally, the BFC float-avoidance check for orthogonal children compares sizes in mismatched coordinate frames.

### Affected Tests (~21 failures)

| Category | Tests | Pixel Diff |
|----------|-------|------------|
| ortho-htb-alongside-vrl-floats | 002, 006, 010, 014 | 93k–135k (19–28%) |
| contiguous-floated-table-vlr | 003, 005, 007, 009 | 5k–10k (1–2.1%) |
| contiguous-floated-table-vrl | 002, 004, 006, 008 | 5k–10k (1–2.1%) |
| float-vlr | 007, 011, 013 | 2.5k–7.5k (0.5–1.6%) |
| float-vrl | 010, 012 | 2.9k (0.6%) |
| float-lft-orthog-htb-in-vlr | 002 | 2k (0.4%) |
| float-lft-orthog-htb-in-vrl | 002 | 2k (0.4%) |
| float-lft-orthog-vlr-in-htb | 002 | 1.9k (0.4%) |
| float-lft-orthog-vrl-in-htb | 002 | 1.9k (0.4%) |

### Root Cause (from code analysis)

There are three related bugs:

**Bug 1 — Physical→logical conversion only handles RTL, not vertical WMs.**

`pkg/layout/block_layout.go`, lines 1062–1073 in `layoutFloat()`:
```go
// Current code — ONLY swaps for RTL, ignores vertical modes entirely
logicalSide := floatSide
if parentWDM.Dir == DirectionRTL {
    if floatSide == css.FloatLeft {
        logicalSide = css.FloatRight
    } else {
        logicalSide = css.FloatLeft
    }
}
```
In `vertical-rl` LTR, `float:left` stays `FloatLeft` (meaning inline-start = physical top), but physically "left" in VRL is block-end. The exclusion is stored with the wrong side.

The exclusion at line 1098 uses `logicalSide`:
```go
exclusion := Exclusion{
    ...
    Side: logicalSide,
}
```

**Bug 2 — `ClearanceOffset()` compares physical clear against half-logical float side.**

`pkg/layout/exclusion_space.go`, lines 106–113:
```go
case css.ClearLeft:
    shouldClear = e.Side == css.FloatLeft
```
`css.ClearLeft` is the physical CSS value `clear:left`. `e.Side` is the half-logical side stored by Bug 1. In vertical modes, these represent different physical edges.

**Bug 3 — BFC float-avoidance uses wrong coordinate frame for orthogonal children.**

`pkg/layout/block_layout.go`, lines 280–312: When a BFC child has a different writing mode from the parent (orthogonal), the code computes `neededInline` in the child's inline axis and compares it against `childAvailableInline - floatStartOff - floatEndOff` in the parent's inline axis. In an orthogonal relationship, these are perpendicular axes. The comparison is meaningless, causing 19–28% diffs in `ortho-htb-alongside-vrl-floats`.

The current code at lines 280–312:
```go
if isChildNewFC && (floatStartOff > 0 || floatEndOff > 0) {
    childGeomForBFC := ComputeFragmentGeometry(childStyle, childWDM)
    tmpSpace := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
        SetAvailableSize(LogicalSize{
            InlineSize: contentInlineSize,   // parent's inline
            BlockSize:  Indefinite,
        }).
        ...Build()
    if resolvedInline, ok := ResolveInlineSize(childStyle, childWDM, tmpSpace, childGeomForBFC); ok {
        neededInline := resolvedInline + childGeomForBFC.InlineBorderPadding() + childMargins.InlineSum()
        if neededInline > childAvailableInline-floatStartOff-floatEndOff {
            // push below floats
        }
    }
}
```
For orthogonal children, `neededInline` is in the child's inline dimension (e.g., vertical), while `floatStartOff`/`floatEndOff` are in the parent's inline dimension (e.g., horizontal). The comparison is in mismatched axes.

### What Blink Does (STUDY THIS FIRST)

In Blink's LayoutNG, the **entire float/exclusion system is physical**:

- `ExclusionArea` (in `exclusion_area.h`) stores `const EFloat type` using physical `EFloat::kLeft` / `EFloat::kRight` values. No logical conversion.
- `ExclusionSpace` (in `exclusion_space.cc`) maintains `left_clear_offset_` and `right_clear_offset_` — physical left and physical right. All comparisons are physical.
- `ClearanceOffset(EClear)` directly maps `kLeft` → `left_clear_offset_`, `kRight` → `right_clear_offset_`. No writing-mode conversion.
- The only logical↔physical conversion for floats happens in `ComputedStyle::Floating(TextDirection)` which resolves `float: inline-start/inline-end` to physical `kLeft`/`kRight`. Physical `float: left/right` pass through unchanged.
- Exclusion positioning uses logical coordinates — the exclusion's `InlineOffset` is logical, but `Side` is physical.

For orthogonal BFC avoidance, Blink resolves the child's size in the child's own coordinate system and compares against the available space projected onto the correct axis.

Key Blink source files to study:
- `third_party/blink/renderer/core/layout/exclusions/exclusion_area.h`
- `third_party/blink/renderer/core/layout/exclusions/exclusion_space.h` and `.cc`
- `third_party/blink/renderer/core/layout/unpositioned_float.h` — `IsLineLeft(TextDirection)` / `IsLineRight(TextDirection)`
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc` — `HandleFloat()`

### Implementation Steps and Milestones

**Milestone 1: Make exclusion sides consistently physical** (commit after this)

1. In `layoutFloat()` (`block_layout.go:1062–1073`), **remove the RTL direction swap entirely**. Store `floatSide` directly as the exclusion's `Side` — it's already physical (`css.FloatLeft` = physical left, `css.FloatRight` = physical right).

2. Now `FindAvailableInlineSize()` (`exclusion_space.go:57–93`) is broken — it assumes `FloatLeft` = inline-start and `FloatRight` = inline-end. It needs a `WritingDirectionMode` parameter so it can correctly map physical float sides to inline-start/inline-end offsets.

   Add the WDM parameter to `ExclusionSpace` or pass it per-call to `FindAvailableInlineSize`. The logic:
   ```
   // Determine if physical-left maps to inline-start or inline-end
   // In HTB LTR: left = inline-start ✓
   // In HTB RTL: left = inline-end (swap)
   // In VRL LTR: left = block-end → neither inline-start nor inline-end
   //             (in VRL, floats occupy the block axis, so float:left = block-end)
   //             For inline positioning: the float still needs to know whether
   //             to stack from inline-start or inline-end.
   ```

   Actually, the key insight is: in Blink, `float: left/right` in ANY writing mode positions the float at the physical left/right edge. But the inline-start/inline-end question is about which inline edge the float occupies. In vertical modes, floats still occupy inline edges (top/bottom for VRL/VLR), with `float:left` going to line-left and `float:right` going to line-right.

   CSS Writing Modes §6.2 defines line-left and line-right:
   - HTB: line-left = physical left, line-right = physical right
   - VRL: line-left = physical top, line-right = physical bottom
   - VLR: line-left = physical top, line-right = physical bottom
   
   So `float:left` always goes to line-left, `float:right` to line-right. Line-left/right map to inline-start/end based on direction:
   - LTR: line-left = inline-start, line-right = inline-end
   - RTL: line-left = inline-end, line-right = inline-start

   This means the current physical→logical conversion needs to map through line-left/right:
   ```go
   func isInlineStartFloat(physicalSide css.FloatType, wdm WritingDirectionMode) bool {
       // float:left → line-left. float:right → line-right.
       // LTR: line-left = inline-start → float:left = inline-start
       // RTL: line-left = inline-end → float:left = inline-end
       if wdm.Dir == DirectionLTR {
           return physicalSide == css.FloatLeft
       }
       return physicalSide == css.FloatRight
   }
   ```

   Wait — this is exactly the current RTL swap logic! The problem isn't the inline-start/end mapping, it's that in vertical modes, "left" maps to a different physical edge for line-left. Let me re-read CSS Writing Modes §6.2...

   Actually `float: left` is defined as "the element generates a box that is floated to the line-left side" (CSS 2.1 §9.5.1, modified by CSS Writing Modes). Line-left in VRL = physical top = inline-start (in LTR). So `float:left` in VRL LTR = inline-start. The current RTL-only swap gives the right answer here too (LTR → no swap → FloatLeft stays, meaning inline-start). 

   But then `ClearanceOffset()` is checking `css.ClearLeft == e.Side == css.FloatLeft`. If the float was `float:left` in VRL and we stored it as `FloatLeft` (inline-start after the RTL swap that didn't fire because it's LTR), then `clear:left` matches. But `clear:left` is physical-left. In VRL, physical-left is block-end, which has nothing to do with inline-start. So the clear doesn't work correctly.

   **The fix is**: `ClearanceOffset()` needs to know the writing mode so it can convert physical `clear:left` to the correct exclusion side. Alternatively, store the exclusion side as a dedicated inline-start/inline-end enum (not reusing `css.FloatLeft`/`css.FloatRight`), and convert `clear:left/right` through the same line-left/right mapping.

3. Define a new enum for exclusion sides to eliminate confusion:
   ```go
   type ExclusionSide int
   const (
       ExclusionInlineStart ExclusionSide = iota
       ExclusionInlineEnd
   )
   ```
   Store this in `Exclusion.Side` instead of `css.FloatType`. Then:
   - In `layoutFloat()`: convert physical `css.FloatLeft`/`css.FloatRight` to `ExclusionInlineStart`/`ExclusionInlineEnd` using `isInlineStartFloat()`.
   - In `FindAvailableInlineSize()`: match against `ExclusionInlineStart`/`ExclusionInlineEnd` (no change in logic).
   - In `ClearanceOffset()`: convert physical `css.ClearLeft`/`css.ClearRight` through the same `isInlineStartFloat` mapping, then match against the stored side.

4. Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(float-vlr|float-vrl)" -count=1
```

**Milestone 2: Fix ClearanceOffset WM mapping** (commit after this)

1. In `ClearanceOffset()` (`exclusion_space.go:98–121`), add a `WritingDirectionMode` parameter. Convert physical `css.ClearLeft`/`css.ClearRight` to inline-start/end before matching:
   ```go
   func (es *ExclusionSpace) ClearanceOffset(clearType css.ClearType, currentBlockOffset float64, wdm WritingDirectionMode) float64 {
       ...
       switch clearType {
       case css.ClearLeft:
           // clear:left clears line-left floats
           if isInlineStartFloat(css.FloatLeft, wdm) {
               shouldClear = e.Side == ExclusionInlineStart
           } else {
               shouldClear = e.Side == ExclusionInlineEnd
           }
       ...
       }
   }
   ```

2. Update all callers of `ClearanceOffset()` to pass the WDM. There are ~3 call sites in `block_layout.go` (search for `ClearanceOffset`).

3. Similarly update `FindFloatPosition()` if it calls `FindAvailableInlineSize`.

4. Run clearance tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous-floated-table" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-lft-orthog" -count=1
```

**Milestone 3: Fix orthogonal BFC float-avoidance** (commit after this)

1. In `block_layout.go` (lines 280–312), when checking if an orthogonal BFC child fits beside floats:
   - The child's `neededInline` is in the child's inline axis. For an orthogonal child, this maps to the parent's block axis.
   - The float offsets (`floatStartOff`, `floatEndOff`) are in the parent's inline axis.
   - For orthogonal children, the child's inline dimension is independent of the parent's inline float intrusions — the child can always fit horizontally beside the floats because it extends in the perpendicular direction.
   - But the child's block dimension (parent's inline) is constrained by floats. The child's block size with orthogonal is effectively the available inline space minus float offsets.
   
   The fix: when the child is orthogonal, skip the "push below floats" check for the inline axis. Instead, the constraint space builder should reduce the child's available block size (which maps to the parent's inline axis) by the float offsets.

   Actually, study how Blink handles this. In Blink's `HandleNewFormattingContext()` in `block_layout_algorithm.cc`, when the child is orthogonal, the available size and float avoidance logic accounts for the axis swap.

2. Run orthogonal float tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ortho-htb-alongside-vrl-floats" -count=1
```

**Milestone 4: Run all affected tests together** (commit if adjustments needed)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(ortho-htb-alongside-vrl|contiguous-floated-table|float-v|float-lft-orthog)" -count=1
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(ortho-htb-alongside-vrl|contiguous-floated-table|float-v|float-lft-orthog)" -count=1
```

### Primary files
- `pkg/layout/exclusion_space.go` (primary — ExclusionSide enum, FindAvailableInlineSize, ClearanceOffset)
- `pkg/layout/block_layout.go` lines 240–312 (BFC avoidance) and 1012–1101 (layoutFloat)

---

## Target 2: Overconstrained relative positioning and normal-flow margin resolution (~10 tests)

### Problem

The T5 agent rewrote `computeRelativeOffset()` in round 5, but 6 overconstrained-rel-pos tests and 2 normal-flow-overconstrained tests still fail, plus 2 box-offsets-rel-pos tests. The remaining issues are: (1) `computeRelativeOffset()` has subtle sign errors for some writing mode + direction + both-insets-specified combinations, and (2) the overconstrained inline-margin recalculation (CSS 2.1 §10.3.3) is missing for vertical writing modes.

### Affected Tests (~10 failures)

| Category | Tests | Pixel Diff |
|----------|-------|------------|
| overconstrained-rel-pos-ltr-left-right-vrl | 004 | 6400 (1.3%) |
| overconstrained-rel-pos-ltr-top-bottom-vrl | 002 | 6400 (1.3%) |
| overconstrained-rel-pos-rtl-left-right-vlr | 009 | 6400 (1.3%) |
| overconstrained-rel-pos-rtl-left-right-vrl | 008 | 6400 (1.3%) |
| overconstrained-rel-pos-rtl-top-bottom-vlr | 007 | 6400 (1.3%) |
| overconstrained-rel-pos-rtl-top-bottom-vrl | 006 | 6400 (1.3%) |
| normal-flow-overconstrained-vrl | 002, 004 | 6400 (1.3%) |
| box-offsets-rel-pos-vlr | 005 | 2510 (0.5%) |
| box-offsets-rel-pos-vrl | 004 | 2510 (0.5%) |

### Root Cause (from code analysis)

**Bug A — computeRelativeOffset() overconstrained case is incomplete.**

The current implementation at `pkg/layout/block_layout.go:807–907` handles the "start wins" rule per-axis per-writing-mode. But when BOTH insets on an axis are specified, the overconstrained resolution should apply — the "losing" side's value is forced to be the negative of the "winning" side's value.

CSS Writing Modes §7.1 maps the overconstrained rule to logical axes:
- Inline axis: inline-start wins, inline-end is set to `-inline-start`
- Block axis: block-start wins, block-end is set to `-block-start`

The current code only uses the winning side and ignores the other. But in the overconstrained case, both values are specified, and the result should be: `offset = winning_inset_value` (not `-losing_inset_value`). The current code handles this correctly for the winning side — the issue is elsewhere.

Actually, let me re-examine. The overconstrained-rel-pos tests have both left and right (or both top and bottom) specified. In CSS 2.1 §9.4.3:
- Both left+right specified, direction=LTR: `left` wins, effectively `dx = left` (and `right` is ignored / treated as `-left`)
- Both left+right specified, direction=RTL: `right` wins, effectively `dx = -right`

The current code for VRL LTR:
```go
case WritingModeVerticalRL, WritingModeSidewaysRL:
    // Block axis = horizontal: right = block-start in vrl, always wins.
    if offset.HasRight {
        dx = -offset.Right
    } else if offset.HasLeft {
        dx = offset.Left
    }
```

When both `HasLeft` and `HasRight` are true: `dx = -offset.Right` (right wins). But CSS 2.1 §9.4.3 says for overconstrained left/right: direction=LTR → left wins. The current code makes right always win in VRL regardless of direction.

The confusion is: CSS 2.1 §9.4.3 uses direction to resolve left/right overconstrained. But CSS Writing Modes §7.1 maps this to "inline-start wins." In VRL:
- left/right are on the block axis (horizontal)  
- top/bottom are on the inline axis (vertical)

So CSS 2.1's "left wins in LTR for horizontal overconstrained" gets mapped to:
- In HTB: left/right overconstrained → inline axis → direction-based → LTR: left wins
- In VRL: left/right overconstrained → block axis → block-start always wins → right wins (regardless of direction)

This means the current code IS correct: in VRL, right always wins for left/right overconstrained. So the bug must be elsewhere.

**Investigate the actual test failure.** Read the test `overconstrained-rel-pos-ltr-left-right-vrl-004.xht` to understand what it expects, and check our rendering output. The 6400px diff (1.3%) at 800x600 = 480000 pixels means ~6400 pixels are wrong, suggesting an 80x80 box is displaced by one box-width.

One likely issue: the test positions an element using position:relative with both left and right set in VRL. Our computeRelativeOffset returns the correct dx, but the element's normal-flow position in VRL might be wrong (the "starting position" before applying the relative offset). If the element is an 80x80 box and the entire box is in the wrong place, that's ~6400 pixels.

**Bug B — Missing overconstrained inline-margin recalculation (CSS 2.1 §10.3.3).**

In `pkg/layout/block_layout.go` around line 382, the inline positioning code:
```go
childInlineOffset := childMargins.InlineStart + floatStartOff
```

CSS 2.1 §10.3.3: "If all of the above have a computed value other than 'auto', the values are said to be 'over-constrained' and one of the used values will have to be different from its computed value. If the 'direction' property of the containing block has the value 'ltr', the specified value of 'margin-right' is ignored and the value is calculated so as to make the equality true."

In logical terms: if the inline-size is explicit and both inline-start and inline-end margins are specified (not auto), the inline-end margin is adjusted. The current code doesn't do this adjustment. In horizontal-tb this is usually invisible because the default width:auto prevents overconstrained. But in vertical modes, the block axis maps differently and explicit sizes trigger this more often.

**Bug C — box-offsets-rel-pos tests.**

`box-offsets-rel-pos-vlr-005.xht` and `box-offsets-rel-pos-vrl-004.xht` test relative positioning offsets at 2510px diff (0.5%). These use specific top/right/bottom/left offsets to position blue squares at corners of a yellow box. The 0.5% diff suggests small positional errors in the relative offset computation. Check if the offsets are being applied to the correct physical edge.

### What Blink Does (STUDY THIS FIRST)

Blink resolves relative offsets purely physically in `ComputeRelativeOffset()` (`layout_box_utils.cc`):

```cpp
if (IsLtr(direction)) {
    if (has_left) dx = left;
    else if (has_right) dx = -right;
} else {
    if (has_right) dx = -right;
    else if (has_left) dx = left;
}
if (has_top) dy = top;
else if (has_bottom) dy = -bottom;
```

**No logical conversion at all.** Physical left/right → physical dx. Physical top/bottom → physical dy. Writing mode is irrelevant. Only direction matters for left/right priority.

For overconstrained inline margins, Blink's `ResolveInlineMargins()` in `block_layout_algorithm.cc`:
```cpp
LayoutUnit inline_end = child_available_size - margins.inline_start -
                        margins.inline_end - inline_size;
if (inline_end != 0)
    margins.inline_end += inline_end;
```

Key sources:
- `third_party/blink/renderer/core/layout/layout_box_utils.cc` — `ComputeRelativeOffset()`
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc` — `ResolveInlineMargins()`

### Implementation Steps and Milestones

**Milestone 1: Simplify `computeRelativeOffset()` to Blink's purely physical approach** (commit after this)

Replace the current per-writing-mode switch (lines 807–907) with Blink's approach:

```go
func computeRelativeOffset(offset css.PositionOffset, wdm WritingDirectionMode) PhysicalOffset {
    var dx, dy float64

    // CSS 2.1 §9.4.3: direction determines which horizontal offset wins.
    // These are always physical — writing mode is irrelevant.
    if wdm.Dir == DirectionLTR {
        if offset.HasLeft {
            dx = offset.Left
        } else if offset.HasRight {
            dx = -offset.Right
        }
    } else {
        if offset.HasRight {
            dx = -offset.Right
        } else if offset.HasLeft {
            dx = offset.Left
        }
    }

    // top always wins over bottom (physical vertical, no WM dependency)
    if offset.HasTop {
        dy = offset.Top
    } else if offset.HasBottom {
        dy = -offset.Bottom
    }

    return PhysicalOffset{X: dx, Y: dy}
}
```

Check that `css.PositionOffset` has the required fields (`HasLeft`, `Left`, `HasRight`, `Right`, `HasTop`, `Top`, `HasBottom`, `Bottom`). Read `pkg/css/style.go` to find the struct definition and adapt if the field names differ.

Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/overconstrained-rel-pos" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/box-offsets-rel-pos" -count=1
```

**Milestone 2: Implement overconstrained inline-margin recalculation** (commit after this)

In `pkg/layout/block_layout.go`, after the inline positioning code (around line 386), when neither inline margin is auto and the child has a definite inline-size:

```go
// CSS 2.1 §10.3.3: If inline-size + margins + border + padding > container,
// adjust the inline-end margin.
if !autoInlineStart && !autoInlineEnd {
    // Check if childResult has a definite inline size
    totalUsed := childLogical.InlineSize() + childMargins.InlineSum()
    overflow := totalUsed - contentInlineSize
    if overflow > 0 {
        // Overconstrained: shrink inline-end margin
        childMargins.InlineEnd -= overflow
        childInlineOffset = childMargins.InlineStart + floatStartOff
    }
}
```

Note: You need to find where `autoInlineStart`/`autoInlineEnd` flags are available. Search for how margins are resolved in the block layout's child positioning code. The `ResolveMargins` function returns resolved values but may not preserve auto flags. You may need to check the raw margin values. Read `pkg/css/style.go` for `GetMargin()` and `pkg/layout/margins.go` for `ResolveMargins`.

Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/normal-flow-overconstrained" -count=1
```

**Milestone 3: Run all affected tests** (commit if adjustments needed)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(overconstrained-rel-pos|normal-flow-overconstrained|box-offsets-rel-pos)" -count=1
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(overconstrained-rel-pos|normal-flow-overconstrained|box-offsets-rel-pos)" -count=1
```

### Primary files
- `pkg/layout/block_layout.go` lines 380–400 (inline margin resolution) and 765–907 (computeRelativeOffset)

---

## Target 3: Background-position in vertical writing modes with auto-width root (~3 tests)

### Problem

The T3 agent in round 5 fixed the canvas background positioning area to use viewport bounds (`isFixed || layer.PaintsCanvasBackground`), which fixed 4 `background-size-document-root-vrl` tests. However, 3 `background-position-vrl` tests still fail at 12.5% (60,000px). These tests have `writing-mode: vertical-rl` on the root element with `width: auto`, causing the root's box to be narrower than the viewport. Although `PaintsCanvasBackground=true` is set and the positioning area uses viewport bounds, the background-position is still computed incorrectly.

### Affected Tests (3 failures)

| Category | Tests | Pixel Diff |
|----------|-------|------------|
| background-position-vrl | 018, 020, 022 | 60,000 (12.5%) each |

### Root Cause (from code analysis)

The test `background-position-vrl-018.xht` has:
- `html { writing-mode: vertical-rl; width: auto; background-image: url("100x100-red.png"); background-position: left top; background-repeat: repeat-y; }`
- A green overlay div positioned to cover where the red image should be

The test asserts: "background-position: left top will make background-image start at left side of document root element because background properties should not be affected by vertical writing-mode."

The `PaintsCanvasBackground` fix at line 1509 of `render.go` uses viewport bounds for the origin. So `background-position: left top` should place the image at (0, 0) of the viewport. The green overlay at `right: 273px; width: 100px` should cover it.

The 60,000px diff (12.5%) suggests the background image is NOT at the expected position, or the green overlay is mispositioned.

**Likely root causes to investigate:**

1. **Root element box position in VRL.** In `writing-mode: vertical-rl`, the root element's block direction is right-to-left. With `width: auto`, the root element shrinks to content width. The root's box might start from the right edge of the viewport (block-start). Check if the root element's physical X position is correct — it should start from the right and extend leftward.

2. **Absolute positioning of green overlay.** The green overlay uses `position: absolute; right: 273px; top: 0; height: 100%; width: 100px`. Its containing block is the ICB. If the ICB's dimensions or coordinate system is wrong in VRL, the green overlay is mispositioned. Specifically, does `right: 273px` resolve correctly against the viewport in VRL?

3. **Background painting vs. element layout mismatch.** Even with `PaintsCanvasBackground=true`, the origin uses the viewport's (0,0). But if the root element in VRL has its box starting at x = (viewport-width - content-width), the background-image tiling might start from the box's position rather than the viewport's origin. Check if `drawBackgroundImageLayer` uses the box position for the initial tile placement rather than the origin.

4. **The `background-repeat: repeat-y` calculation.** After the origin is set to viewport bounds, the tiling code computes tile positions. If the initial tile position is computed from the box rather than the origin, tiling would start from the wrong location.

### What Blink Does (STUDY THIS FIRST)

In Blink's `BackgroundImageGeometry::Calculate()` (`background_image_geometry.cc`):
- When painting the canvas background for the root element, the positioning area is explicitly the ICB.
- Background-position percentages and keywords resolve against this ICB-sized positioning area.
- The tile phase (where the repeating pattern starts) is computed from the ICB origin, not the element's box origin.
- CSS Backgrounds Level 3 §7.6 (Purely Physical Mappings): "background-position, background-origin, and background-clip are purely physical and remain unaffected by writing-mode."

Key source: `third_party/blink/renderer/core/paint/background_image_geometry.cc` — `Calculate()` and `SetPositioningArea()`.

### Implementation Steps and Milestones

**Milestone 1: Diagnose the exact failure** (commit after this if code changes needed)

1. Read the failing test HTML files (018, 020, 022) to understand the exact expected layout.
2. Run the test and examine the generated test/ref/diff PNGs in `output/reftests/`:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl-018" -count=1
```
3. Compare the test and reference PNGs visually (read them with Read tool). Identify what's different — is the red image in the wrong position? Is the green overlay in the wrong position? Or both?

4. Add temporary debug logging to `drawBackgroundImageLayer()` to print the origin values (originX, originY, originW, originH) and the tile position calculations. Check if the viewport bounds are being used correctly.

**Milestone 2: Fix the root cause** (commit after this)

Based on diagnosis, the fix likely involves one of:

A. **If the background tiling starts from the box origin instead of the viewport origin:** The tiling code (lines 1540+) computes `bgPosX`/`bgPosY` — check if these are relative to the origin or the box. They should be relative to the origin (viewport) when `PaintsCanvasBackground=true`.

B. **If the root element's layout position is wrong in VRL:** The root element in VRL should have its box starting from the right edge. Check how `layoutRoot()` or the initial block size is computed. The root element's ICB should give it the full viewport width, but `width: auto` in VRL might shrink it.

C. **If the absolutely positioned green overlay is mispositioned:** The green overlay's `right: 273px` should resolve against the viewport width. If the containing block for absolute elements in the root is the root's content box (not the ICB), the green overlay would be mispositioned.

**Milestone 3: Run all affected tests**
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
```

Verify that the 15 currently-passing background-position-vrl tests (002–016) don't regress.

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
```

### Primary files
- `pkg/render/render.go` lines 1486–1650 (drawBackgroundImageLayer)
- Possibly `pkg/layout/block_layout.go` root layout code (BUT only for diagnosis — changes here overlap with Target 1/2, so coordinate carefully)

---

## Independence Check

| | exclusion_space.go | block_layout.go | render.go |
|---|---|---|---|
| Target 1 (float/clear) | **Primary** | layoutFloat (1012–1101), BFC avoidance (240–312) | - |
| Target 2 (rel-pos/margins) | - | computeRelativeOffset (765–907), inline margins (380–400) | - |
| Target 3 (background-pos) | - | - | **Primary** (1486–1650) |

**Target 1 and Target 2** both touch `block_layout.go` but at well-separated line ranges (240–312 + 1012–1101 vs 380–400 + 765–907). These are different functions with no shared state. Merge conflicts are unlikely.

**Target 3** touches only `render.go` — fully independent from Targets 1 and 2.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area.
- **Commit and report at each milestone** (don't batch everything to the end).
- **Regression constraints:** After fixing your target, verify that other writing-mode tests you didn't touch haven't regressed. Run a quick spot-check of 2–3 nearby tests.
- **If `pkg/layout/block_layout.go` needs changes**, be extremely precise about which functions/line ranges you modify to minimize merge conflicts with the other agent.
- **Do NOT run the full writing-modes test suite** — only run the tests listed in each target's Verification section.
