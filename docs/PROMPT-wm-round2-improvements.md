# Writing Modes Round 2: Top 3 Improvements + CSS2.1 Fixes

Current state: 586 pass / 202 fail (74.4% pass rate) across 788 writing-modes tests.
CSS2.1 state: 88 pass / 11 fail (89%) across 99 tests.

These four targets are **independent** (touch different subsystems) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Float Dimension Transposition (~37 tests)

### Problem

When laying out floats in vertical writing modes, the code wraps the child float's `PhysicalFragment` using the **parent's** `WritingDirectionMode` to read its logical dimensions. This is wrong — it should use the **child fragment's own** `WritingDirection`. This causes the float's inline-size and block-size to be transposed (swapped) when the child has a different writing mode from the parent.

### Affected Tests (~37 failures)

| Category | Fail Count |
|----------|-----------|
| float-vlr (003,005,007,009,011,013) | 6 |
| float-vrl (002,004,006,008,010,012) | 6 |
| float-shrink-to-fit-vlr (003,005,007,009) | 4 |
| float-shrink-to-fit-vrl (002,004,006,008,016) | 5 |
| float-clear-vlr-009, float-clear-vrl-008 | 2 |
| float-contiguous-vlr-011, float-contiguous-vrl-010 | 2 |
| float-lft-orthog-* (4 tests) | 4 |
| float-rgt-orthog-* (4 tests) | 4 |
| contiguous-floated-table-* (8 tests) | 8 |
| clearance-calculations-vrl-* (4 tests) | 4 |
| ortho-htb-alongside-vrl-floats-* (4 tests) | 4 |

### Root Cause (from code analysis)

In `pkg/layout/block_layout.go` line 634, inside `layoutFloat()`:
```go
childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)
```

This wraps the child's physical fragment using the **parent's** WDM. When converting physical dimensions (width × height) to logical (inline × block), using the wrong coordinate system causes the axes to swap. For example, a 50px-wide × 100px-tall float in horizontal-tb, interpreted through a vertical-lr parent's WDM, gets InlineSize=100 (should be 50) and BlockSize=50 (should be 100).

The same bug exists at line 241 for non-float block children:
```go
childLogical := NewLogicalFragment(wdm, childResult.Fragment)
```

This cascades through the entire float pipeline:
1. `floatInlineSize`/`floatBlockSize` computed with swapped values (lines 637-638)
2. `FindFloatPosition()` called with wrong `floatBlockSize` (line 646)
3. `FindAvailableInlineSize()` uses wrong dimensions (lines 652, 655)
4. Exclusion box added with transposed sizes (lines 669-670)
5. All subsequent content positioned relative to wrong exclusion

### What Blink Does

In Blink, each `PhysicalFragment` carries its own `WritingDirection`. When reading a fragment's intrinsic dimensions (to compute margin-box size, exclusion area, etc.), Blink always uses the fragment's own writing direction, not the parent's. The parent's WDM is only used for positioning the child within the parent's coordinate system.

### Fix Location

**Primary fix** — `pkg/layout/block_layout.go` line 634:
```go
// Change:
childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)
// To:
childLogical := NewLogicalFragment(childResult.Fragment.WritingDirection, childResult.Fragment)
```

**Secondary fix** — `pkg/layout/block_layout.go` line 241 (same pattern for block children):
```go
// Change:
childLogical := NewLogicalFragment(wdm, childResult.Fragment)
// To:
childLogical := NewLogicalFragment(childResult.Fragment.WritingDirection, childResult.Fragment)
```

**CAUTION**: After fixing the dimension reading, the **positioning** of the child within the parent still needs the parent's WDM. The `LogicalOffset` passed to `AddChild` and the exclusion's `InlineOffset`/`BlockOffset` are in the parent's logical coordinate system. Make sure only the dimension reading changes, not the offset computation.

Also check: are there other places in `layoutFloat()` that use `parentWDM` when they should use the child's WDM? Grep for `NewLogicalFragment` across the codebase and audit each call site.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/clearance" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ortho-htb-alongside" -count=1
```

Regression checks:
```bash
go test -v -run "TestListReftestResults" -count=1  # Must stay >= 88/99
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/margin-collapse" -count=1  # Must stay 20/20
```

---

## Target 2: Block-Level Unicode-Bidi Injection (~10-14 tests)

### Problem

CSS Writing Modes §2.2 specifies that when a **block container** element has `unicode-bidi` set to `bidi-override`, `isolate-override`, or `plaintext`, it should affect the bidi resolution of its inline content. The current code only injects bidi control characters for **inline elements** (via `injectBidiControlChars` in `inline_item.go` line 215), but never for the block container itself.

### Affected Tests (~10-14 failures)

| Category | Fail Count |
|----------|-----------|
| block-override-001, 002, 003, 004 | 4 |
| block-override-isolate-001, 002, 003, 004 | 4 |
| block-plaintext-003, 006 | 2 |
| direction-vlr-003, 005, direction-vrl-002, 004 | 4 (possibly related) |

### Root Cause (from code analysis)

In `pkg/layout/inline_item.go`, `collectInlinesRecursive()` (line 208-243), bidi control chars are injected only for inline elements:
```go
// Line 208-215: Inline element (span, em, a, etc.) — emit open/close tags.
// CSS Writing Modes §2.2: Inject Unicode bidi control characters
// for elements with unicode-bidi set.
injectBidiControlChars(childStyle, text, true /* isOpen */)
```

But when `layoutInlineChildren()` in `inline_layout.go` (line 58) starts processing a block container's inline content, it calls `CollectInlines(bla.node)` (line 65) which collects children but never checks the block container's own `unicode-bidi` property.

The block container's `unicode-bidi` is only checked for `plaintext` (line 83):
```go
if bidi, ok := bla.style.Get("unicode-bidi"); ok && bidi == "plaintext" {
    isPlaintext = true
}
```

This handles the `plaintext` case for paragraph-level direction detection, but does **not** inject the actual bidi control characters (FSI/PDI) into the text content. And `bidi-override` / `isolate-override` on block containers is completely unhandled.

### What Blink Does

In Blink's `InlineItemsBuilder`, when building inline items for a block container, the block container's own `unicode-bidi` and `direction` properties are checked first. If the block has `bidi-override`, opening control characters (LRO/RLO) are prepended to the text content before any child items, and closing characters (PDF) are appended after. The UAX#9 bidi algorithm then processes these control characters to force the correct visual ordering.

For `plaintext`, Blink injects FSI at the start and PDI at the end of each paragraph, allowing per-paragraph first-strong-character direction detection.

### Fix Location

**Primary fix** — `pkg/layout/inline_layout.go`, in `layoutInlineChildren()` after `CollectInlines()` returns (line 65) and before bidi resolution (line 87):

1. Check if `bla.style` has `unicode-bidi` set to `bidi-override`, `isolate-override`, or `embed`
2. If so, **prepend** the appropriate opening control character(s) to `itemsData.TextContent`
3. **Append** the closing control character(s) after all content
4. Adjust all existing item `StartOffset`/`EndOffset` values by the number of prepended characters

The character mapping already exists in `injectBidiControlChars()` (inline_item.go line 363). Either refactor that function to also work for block containers, or duplicate the mapping logic.

**For plaintext specifically**: The current code already calls `ResolveBidiLevelsPlaintext()` which handles per-paragraph direction. But FSI/PDI injection at block boundaries may still be needed for correct paragraph isolation.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/block-override" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/block-plaintext" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/direction-v" -count=1
```

Regression checks:
```bash
go test -v -run "TestListReftestResults" -count=1  # Must stay >= 88/99
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/margin-collapse" -count=1  # Must stay 20/20
```

---

## Target 3: Percent Margin/Padding in Orthogonal Flows (~12 tests)

### Problem

CSS 2.1 §8.3 and §8.4 state that percentage margins and padding **always** resolve against the containing block's **inline-size** (width in horizontal-tb), regardless of the child's writing mode. But when the child is orthogonal to the parent, `SetPercentageResolutionSize()` in `constraint_space.go` swaps the inline and block axes — which is correct for available-size but **incorrect** for percentage resolution.

### Affected Tests (~12 failures)

| Category | Fail Count |
|----------|-----------|
| percent-margin-vlr-003, 005, 007 | 3 |
| percent-margin-vrl-002, 004, 006 | 3 |
| percent-padding-vlr-003, 005, 007 | 3 |
| percent-padding-vrl-002, 004, 006 | 3 |

### Root Cause (from code analysis)

In `pkg/layout/constraint_space.go` lines 134-144:
```go
func (b *ConstraintSpaceBuilder) SetPercentageResolutionSize(size LogicalSize) *ConstraintSpaceBuilder {
    if b.parallel {
        b.space.PercentageResolutionSize = size
    } else {
        // Orthogonal child: swap axes
        b.space.PercentageResolutionSize = LogicalSize{
            InlineSize: size.BlockSize,
            BlockSize:  size.InlineSize,
        }
    }
    return b
}
```

When `b.parallel` is false (orthogonal child), this swaps InlineSize and BlockSize. This makes sense for **available-size** (the child's inline axis corresponds to the parent's block axis), but for **percentage resolution**, the spec says percentages always resolve against the containing block's inline-size in the **containing block's** coordinate system.

Example: Parent is horizontal-tb with width=200px. Child is vertical-lr with `margin-left: 50%`. The margin should be 50% of 200px = 100px. But after the swap, the child's PercentageResolutionSize.InlineSize gets the parent's BlockSize (which may be 0/auto), and the margin resolves to 0.

### What Blink Does

In Blink, the `ConstraintSpace` stores the percentage resolution size in the **child's** coordinate system, but for percentage margins/padding, Blink always uses the containing block's inline-size. The percentage base for margins and padding is explicitly stored separately and does NOT undergo axis swapping for orthogonal children.

### Fix Location

**Option A** (recommended): Add a separate field to `ConstraintSpace` for the percentage resolution base that does NOT get axis-swapped:

1. `pkg/layout/constraint_space.go` — Add a `PercentageResolutionInlineSize` field (a simple float64) that always stores the containing block's inline-size without swapping.
2. `pkg/layout/constraint_space.go` — Add `SetPercentageResolutionInlineSize(v float64)` to the builder.
3. `pkg/layout/block_layout.go` line 216 — Set this field to `contentInlineSize`.
4. `pkg/layout/fragment_geometry.go` — `ResolveMargins()` and padding resolution should use this non-swapped value instead of `PercentageResolutionSize.InlineSize`.

**Option B**: Don't swap `PercentageResolutionSize` for orthogonal children. But this may break other uses of `PercentageResolutionSize` that expect it in the child's coordinate system.

**Key consideration**: The `PercentageResolutionSize` is used for both margin/padding (which should NOT swap) and block-size percentage resolution (which SHOULD swap). This is why a separate field may be needed.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/percent-margin" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/percent-padding" -count=1
```

Regression checks:
```bash
go test -v -run "TestListReftestResults" -count=1  # Must stay >= 88/99
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/margin-collapse" -count=1  # Must stay 20/20
```

---

## Target 4: CSS 2.1 Reftest Fixes (~11 tests)

### Problem

11 CSS 2.1 reftests are failing across 7 distinct root cause groups. The highest-impact fixes are: generated content display types (2 tests, 97K pixels), percentage height resolution (3-4 tests), and background positioning (2 tests).

### Affected Tests (11 failures)

| Test | Pixels | % | Root Cause Group |
|------|--------|---|-----------------|
| generated-content/before-after-display-types-001.xht | 61,343 | 12.8% | A: Pseudo-element display |
| generated-content/before-after-floated-001.xht | 35,649 | 7.4% | A: Pseudo-element display |
| positioning/absolute-non-replaced-height-002.xht | 10,100 | 2.1% | B: Abs-pos auto height |
| backgrounds/background-position-bottom-001.xht | 3,200 | 0.7% | C: Background position |
| backgrounds/background-image-repeat-x-001.xht | 3,000 | 0.6% | C: Background position |
| stacking-context/opacity-affects-block-in-inline.html | 3,072 | 0.6% | D: Opacity stacking |
| linebox/inline-box-002.xht | 748 | 0.2% | E: Block-in-inline |
| display/anonymous-block-001.html | 200 | 0.0% | F: Anonymous blocks |
| sizing/percentage-height-001.html | 100 | 0.0% | G: Percentage height |
| overflow/overflow-hidden-002.xht | 100 | 0.0% | G: Overflow clip |
| box-display/anonymous-boxes-inheritance-001.xht | 100 | 0.0% | G: Anonymous boxes |

### Root Cause Group A: Pseudo-Element Display Types (2 tests, ~97K pixels)

**The problem**: CSS 2.1 §9.7 requires that floated elements have their display blockified. When `::before`/`::after` pseudo-elements have `float: left/right`, their display must be forced to `block`. Also, pseudo-elements with various display types (table, inline-table, table-cell, etc.) need proper handling.

**Root cause location**: `pkg/layout/layout_tree_builder.go`, `createPseudoElement()` function (around lines 267-343). The function reads `display` and handles `block`/`inline` cases, but doesn't check if `float` is set and blockify accordingly.

**Fix**: After computing the pseudo-element's style, check `pseudoStyle.GetFloat() != css.FloatNone`. If floated, ensure the display is blockified to `block`. Also verify that table-related display types for pseudo-elements are handled (may need to create anonymous table wrappers).

### Root Cause Group B: Absolute Positioning Auto Height (1 test, 10K pixels)

**The problem**: An absolutely positioned element with `height: auto` should size based on content per CSS 2.1 §10.6.4. The test expects a 100×100 blue square but gets wrong height.

**Root cause location**: `pkg/layout/out_of_flow_layout.go` — the auto-height resolution algorithm for abs-pos elements may not correctly compute content-derived height.

**Fix**: Verify the §10.6.4 algorithm is correctly implemented: when top+height+bottom are over-constrained or height is auto, resolve height from content, then solve for the remaining auto value.

### Root Cause Group C: Background Positioning (2 tests, 6K pixels)

**The problem**: `background-position: bottom` should position the background image at the bottom of the element. The `repeat-x` test expects a 15px blue stripe at the top.

**Root cause location**: `pkg/render/render.go`, `drawBackgroundImage()` function, and `pkg/css/style.go`, `BackgroundPosition` parsing. The "bottom" keyword should resolve to Y=100% offset.

**Fix**: Verify that `background-position: bottom` parses correctly and that the Y-offset calculation `(containerHeight - imageHeight) * percentage` works.

### Root Cause Group D-G: Smaller Fixes

- **D: Opacity stacking context** (1 test): Block-in-inline with opacity should create stacking context. Check `layout_tree_builder.go` blockification and `paint_layer.go`.
- **E: Block-in-inline positioning** (1 test): Relative positioning on inline containing blocks. Check `expandInlineWithBlockChildren()`.
- **F: Anonymous blocks** (1 test): Anonymous block generation with mixed inline/block children.
- **G: Percentage height / overflow** (2-3 tests): `height: 50%` on child of `height: 200px` parent. Check that `explicitBlockSize` in `block_layout.go` line 218 correctly propagates to `PercentageResolutionSize.BlockSize`.

### Approach

Focus on the highest-impact groups first:
1. **Group A** (pseudo-element blockification) — 2 tests, ~97K pixels, biggest visual impact
2. **Group C** (background positioning) — 2 tests, likely a parsing/calculation bug
3. **Group G** (percentage height) — 2-3 tests, likely a simple fix
4. Remaining groups as time allows

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestListReftestResults" -count=1  # Target: improve from 88/99
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/margin-collapse" -count=1  # Must stay 20/20
```

---

## Independence Check

| | block_layout.go | exclusion_space.go | constraint_space.go | inline_layout.go | inline_item.go | fragment_geometry.go | layout_tree_builder.go | render/ |
|---|---|---|---|---|---|---|---|---|
| Target 1 (Floats) | layoutFloat + line 241 | indirect | - | - | - | - | - | - |
| Target 2 (Bidi) | - | - | - | bidi section (lines 65-98) | Yes | - | - | - |
| Target 3 (Percent) | - | - | Yes | - | - | Yes | - | - |
| Target 4 (CSS2.1) | - | - | - | - | - | - | Yes | Yes |

All four targets touch different subsystems and can be developed independently.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area.
- **Commit and report at each milestone** (don't batch everything to the end).
- All 20 margin-collapse tests must continue to pass.
- CSS2.1 tests must remain >= 88/99 (and Target 4 should improve this).
- Run the specific category tests AND regression tests before reporting success.
- For Target 1, be especially careful that only dimension reading changes, not offset/positioning logic.
- For Target 3, make sure the fix doesn't break non-orthogonal percentage resolution.
