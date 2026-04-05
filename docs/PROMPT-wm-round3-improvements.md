# CSS Writing Modes Round 3: Top 3 Improvements

Current state: 378 pass / 116 fail (76.5% pass rate) across 494 tests.
Test command: `cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1`

These three targets are **independent** (touch different source files) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Overconstrained Relative Positioning in Vertical Writing Modes (~14 tests)

### Problem

Relative positioning uses physical "left wins over right" and "top wins over bottom" rules regardless of writing mode. The CSS spec (CSS Positioned Layout 3) says the **start** side wins, not the left side. In vertical writing modes, left/right map to the block axis and top/bottom map to the inline axis, so the overconstrained resolution must happen in logical coordinates.

### Affected Tests (~14 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| overconstrained-rel-pos-*-vrl/vlr-* | 6 | Overconstrained left+right or top+bottom in vertical modes |
| normal-flow-overconstrained-vrl/vlr-* | 3 | Overconstrained margins in normal flow |
| box-offsets-rel-pos-vrl/vlr-* | 2 | Relative position offsets in vertical modes |
| logical-physical-mapping-001 | 1 | Logical-to-physical property mapping |
| logical-props-002/003/004 | 3 | Logical properties (margin-block-start, etc.) |

### Root Cause (from code analysis)

In `pkg/layout/block_layout.go:490-518`, the relative positioning code treats physical offsets directly:

```go
// Current broken code:
offset := bla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
var dx, dy float64
// Left wins over right.  ← WRONG: should be "start wins over end"
if offset.HasLeft {
    dx = offset.Left
} else if offset.HasRight {
    dx = -offset.Right
}
// Top wins over bottom.  ← WRONG: same issue
if offset.HasTop {
    dy = offset.Top
} else if offset.HasBottom {
    dy = -offset.Bottom
}
result.Fragment.RelativeOffset = PhysicalOffset{X: dx, Y: dy}
```

The identical bug exists in `pkg/layout/flex_layout.go:747-767`.

### What Blink Does

Blink's `ComputeRelativeOffset` in `relative_utils.cc`:

1. Resolves physical insets (left/right/top/bottom) against physical CB dimensions
2. Applies overconstrained conflict resolution: if neither is set → both = 0; if one missing → set to negation of other; if both set → keep both
3. Maps to a `LogicalOffset(inline, block)` via a writing-mode switch that picks exactly **one** value per axis:

| Writing Mode | LTR | RTL |
|---|---|---|
| horizontal-tb | `(left, top)` | `(right, top)` |
| vertical-rl / sideways-rl | `(top, right)` | `(bottom, right)` |
| vertical-lr | `(top, left)` | `(bottom, left)` |
| sideways-lr | `(bottom, left)` | `(top, left)` |

This implements "start wins" because the selected value is always the **inline-start** and **block-start** side.

### Fix Location

**File 1: `pkg/layout/block_layout.go:490-518`**
**File 2: `pkg/layout/flex_layout.go:747-767`**

The existing `PhysicalInsetsToLogical()` function in `pkg/layout/writing_mode_converter.go:251` already handles all writing modes correctly (including RTL direction flipping). Use it:

```go
// Proposed fix pattern:
offset := bla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
logical := PhysicalInsetsToLogical(offset, wdm)

// Inline-start wins over inline-end (CSS spec "start wins" rule)
var inlineOffset float64
if logical.HasInlineStart {
    inlineOffset = logical.InlineStart
} else if logical.HasInlineEnd {
    inlineOffset = -logical.InlineEnd
}

// Block-start wins over block-end
var blockOffset float64
if logical.HasBlockStart {
    blockOffset = logical.BlockStart
} else if logical.HasBlockEnd {
    blockOffset = -logical.BlockEnd
}

// Convert logical offset back to physical for the fragment
logicalOff := LogicalOffset{InlineOffset: inlineOffset, BlockOffset: blockOffset}
conv := NewConverter(wdm, ToPhysicalSize(builder.size, wdm.WM))
physOff := conv.ToPhysicalOffset(logicalOff, PhysicalSize{})
result.Fragment.RelativeOffset = physOff
```

**Note:** The `ToPhysicalOffset` needs the outer size (the fragment's own size) for the coordinate conversion. Use the fragment's size. For the inner size, pass `PhysicalSize{}` since we're converting an offset (delta), not a child position. However, verify this produces correct results — you may need to adjust based on how `ToPhysicalOffset` works for offset deltas vs. child positions.

**Alternative approach** (simpler, matches Blink's switch directly): Instead of using PhysicalInsetsToLogical + convert back, directly compute `dx, dy` with a switch on `wdm.WM` and `wdm.Dir`, picking the correct physical inset for each axis:

```go
var dx, dy float64
switch wdm.WM {
case WritingModeHorizontalTB:
    if wdm.Dir == DirectionLTR {
        dx, dy = resolveInset(offset.Left, offset.HasLeft, offset.Right, offset.HasRight),
                 resolveInset(offset.Top, offset.HasTop, offset.Bottom, offset.HasBottom)
    } else {
        dx, dy = resolveInset(-offset.Right, offset.HasRight, -offset.Left, offset.HasLeft),
                 resolveInset(offset.Top, offset.HasTop, offset.Bottom, offset.HasBottom)
    }
// ... cases for each writing mode
}
```

Choose whichever approach is cleanest and produces correct results for all writing modes.

### Verification

```bash
# Run the specific overconstrained tests
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/overconstrained" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/normal-flow-overconstrained" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/box-offsets-rel-pos" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/logical-p" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Target 2: Tables in Vertical Writing Modes (~34 tests)

### Problem

The table layout algorithm (`table_layout.go`) treats all tables as horizontal-tb. It does not:
1. Apply border-spacing with dimension mapping for vertical modes
2. Handle captions at all (no `DisplayTableCaption` processing)
3. Implement border-collapse conflict resolution in logical coordinates

### Affected Tests (~34 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| border-conflict-element-vlr/vrl-* | 12 | Border collapse conflict resolution in vertical tables |
| contiguous-floated-table-vlr/vrl-* | 8 | Floated tables in vertical modes |
| ch-units-vrl-* | 8 | ch units on table elements in vertical modes |
| border-spacing-vlr/vrl-* | 4 | Border spacing in vertical tables |
| caption-side-vlr/vrl-* | 4 | Caption positioning in vertical tables |
| row-progression-vlr/vrl-* | 2 | Row progression direction |

### Root Cause (from code analysis)

**1. No caption handling** — `collectRows()` at `table_layout.go:261-324` skips `DisplayTableCaption` entirely. No caption-related code exists in the file despite `GetCaptionSide()` being available in the style system.

**2. No border-spacing** — The table layout never calls `GetBorderSpacing()` or `GetBorderSpacingV()`. In vertical modes, these values need dimension mapping: the CSS physical horizontal spacing becomes logical vertical spacing and vice versa.

**3. No border-collapse conflict resolution** — The `border-conflict-element` tests require implementing CSS 2.1 §17.6.2.1 border conflict resolution algorithm, which must work in logical coordinates.

**4. ch-units on table elements** — These tests use `ch` units on table rows/columns with `writing-mode: vertical-rl`. The table layout doesn't properly propagate writing mode to table sub-elements, so ch-based sizing resolves incorrectly.

### What Blink Does

Blink's table layout follows the pattern: **store physical, convert to logical at the layout boundary, do all layout math in logical coordinates.**

| Concept | Physical Storage | Logical Access |
|---|---|---|
| Border spacing | `HorizontalBorderSpacing`, `VerticalBorderSpacing` | `TableBorderSpacing()` returns `LogicalSize` via `ToLogicalSize()` |
| Caption side | `kTop`, `kBottom` | Treated as block-start / block-end |
| Border edges | `kTop`, `kRight`, `kBottom`, `kLeft` | `PhysicalToLogical<EdgeSide>` maps to block-start/end, inline-start/end |

For border-spacing dimension mapping:
```cpp
// Blink uses ToLogicalSize to swap dimensions for vertical modes:
// horizontal-tb: inline=width(horizontal spacing), block=height(vertical spacing)
// vertical-*:    inline=height(vertical spacing), block=width(horizontal spacing)
```

For captions, Blink lays them out in order:
1. `caption-side: top` captions → laid out **before** table grid (at block-start)
2. Table grid (sections/rows/cells)
3. `caption-side: bottom` captions → laid out **after** table grid (at block-end)

### Fix Location

**File: `pkg/layout/table_layout.go`**

**Fix 1: Add border-spacing support**

In the `Layout()` method, after computing column widths and before laying out rows, add border spacing between columns and rows. Create a helper that returns logical border spacing based on writing mode:

```go
func (tla *TableLayoutAlgorithm) logicalBorderSpacing(wdm WritingDirectionMode) (inlineSpacing, blockSpacing float64) {
    hSpacing := tla.style.GetBorderSpacing()   // physical horizontal
    vSpacing := tla.style.GetBorderSpacingV()   // physical vertical
    if wdm.WM == WritingModeHorizontalTB {
        return hSpacing, vSpacing   // inline=horizontal, block=vertical
    }
    return vSpacing, hSpacing       // vertical modes: inline=vertical, block=horizontal
}
```

Then apply `inlineSpacing` between columns in the column width computation and `blockSpacing` between rows in the row layout loop.

**Fix 2: Add caption handling**

In `collectRows()`, also collect caption children. In `Layout()`, lay out captions before/after the table grid based on `GetCaptionSide()` (treating "top" as block-start, "bottom" as block-end).

**Fix 3: Add border-collapse conflict resolution**

Implement the CSS 2.1 §17.6.2.1 algorithm using logical edge sides. Use `PhysicalInsetsToLogical` patterns to map cell borders to logical coordinates before conflict resolution.

### Verification

```bash
# Table-specific tests
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/border-conflict" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/border-spacing" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/caption-side" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous-floated-table" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ch-units" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/row-progression" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Target 3: Inline Baseline Alignment in Vertical Writing Modes (~10 tests)

### Problem

Atomic inline elements (images, inline-blocks, inline-tables) don't use the correct baseline in vertical writing modes. Per CSS Writing Modes 3 §4.3, vertical modes with `text-orientation: mixed` (default) use the **central baseline** as the dominant baseline. The current code either bottom-aligns or uses alphabetical baselines for non-inline-block atomic inlines.

### Affected Tests (~10 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| inline-table-alignment-002/003/004/005 | 4 | Inline table central baseline in vertical-rl |
| baseline-inline-replaced-002/003 | 2 | Image central baseline in vertical-rl |
| inline-block-alignment-006/007 | 2 | Inline-block alphabetical baseline with text-orientation:sideways |
| horizontal-rule-vlr-003/vrl-002 | 2 | HR element baseline/sizing in vertical modes |

### Root Cause (from code analysis)

In `pkg/layout/inline_layout.go:604-659`, the `createLineBoxEx` function handles atomic inlines (InlineItemAtomicInline). The baseline logic has two problems:

**Problem 1: Replaced elements (images) always bottom-align instead of using central baseline**

```go
// Current code at line 644-647:
} else {
    // Default: bottom-align to baseline.
    blockPos = maxAscent - blockSize  // ← WRONG for vertical modes
}
```

In vertical writing modes with central baseline, replaced elements should be centered on the baseline: `blockPos = maxAscent - blockSize/2`. The `centralBaseline` flag is available but not checked in this branch.

**Problem 2: Inline tables fall into the "else" branch**

Inline tables are not `DisplayInlineBlock`, so they skip the inline-block branch (line 632-643) and fall through to the bottom-align default. They should use central baseline alignment in vertical modes, similar to inline-blocks.

**Problem 3: text-orientation:sideways should use alphabetical baseline**

The test `inline-block-alignment-006` has `text-orientation: sideways` in `vertical-rl`. With sideways text orientation, the alphabetical baseline should be dominant (not central). The `UsesCentralBaselineWithStyle()` method at `writing_mode.go:104` should handle this, but verify it's being called for the inline-block baseline calculation too.

### What Blink Does

Blink's inline layout uses `ComputeBaseline()` which checks the writing mode's dominant baseline:
- vertical-rl/vertical-lr with text-orientation: mixed/upright → central baseline
- vertical-rl/vertical-lr with text-orientation: sideways → alphabetical baseline
- horizontal-tb → alphabetical baseline

For replaced elements, Blink computes the baseline as the center of the element's margin box when central baseline is dominant: `baseline = margin_block_start + block_size / 2`.

For inline-tables, Blink uses the first/last row's baseline if available, otherwise the element's center for central baseline mode.

### Fix Location

**File: `pkg/layout/inline_layout.go`**, in the `createLineBoxEx` function, the atomic inline handling section (~lines 604-659).

**Fix 1: Central baseline for replaced elements**

At line 644-647, replace the bottom-align default with central baseline awareness:

```go
} else {
    if centralBaseline {
        // CSS Writing Modes 3 §4.3: central baseline = center of element
        blockPos = maxAscent - blockSize/2
    } else {
        // Alphabetical baseline: bottom-align
        blockPos = maxAscent - blockSize
    }
}
```

**Fix 2: Inline table baseline alignment**

Add a check for `DisplayInlineTable` alongside `DisplayInlineBlock` at line 632, or restructure the switch to handle all atomic inlines that can have baselines:

```go
display := css.DisplayInline
if r.Item.Style != nil {
    display = r.Item.Style.GetDisplay()
}

if (display == css.DisplayInlineBlock || display == css.DisplayInlineTable) &&
    (r.Item.Style.GetOverflow() == css.OverflowVisible || display == css.DisplayInlineTable) {
    // Use last baseline if available
    var ibAscent float64
    if r.LayoutResult.LastBaseline > 0 {
        ibAscent = r.LayoutResult.LastBaseline
    } else if centralBaseline {
        ibAscent = blockSize / 2
    } else {
        // ... alphabetical fallback
    }
    blockPos = maxAscent - ibAscent
} else {
    // Replaced elements and other atomic inlines
    if centralBaseline {
        blockPos = maxAscent - blockSize/2
    } else {
        blockPos = maxAscent - blockSize
    }
}
```

**Fix 3: Verify UsesCentralBaselineWithStyle handles text-orientation:sideways**

In `pkg/layout/writing_mode.go:104`, ensure `UsesCentralBaselineWithStyle` returns `false` when `text-orientation` is `sideways` (alphabetical baseline should be used).

### Verification

```bash
# Inline alignment tests
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/inline-block-alignment" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/inline-table-alignment" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/baseline-inline-replaced" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/horizontal-rule" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Independence Check

| | block_layout.go | flex_layout.go | table_layout.go | inline_layout.go | writing_mode.go |
|---|---|---|---|---|---|
| Target 1 (Overconstrained) | **Yes** | **Yes** | - | - | Read-only |
| Target 2 (Tables) | - | - | **Yes** | - | Read-only |
| Target 3 (Inline baselines) | - | - | - | **Yes** | Possibly |

All three targets touch different primary source files and can be developed independently.

Note: If Target 3 needs to modify `writing_mode.go`, it should only touch `UsesCentralBaselineWithStyle()`. Target 1 only reads `WritingDirectionMode` fields — no conflict.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Key Blink files:
  - Target 1: `relative_utils.cc` — `ComputeRelativeOffset` function
  - Target 2: `table_layout_algorithm.cc` — caption handling, border spacing
  - Target 3: `inline_layout_algorithm.cc` — atomic inline baseline computation
- **Commit and report at each milestone** (don't batch everything to the end).
- **Run the full writing-modes test suite** after each change to check for regressions. The baseline is 378 pass / 116 fail. Any regression below 378 passes must be investigated.
- **Do NOT modify files outside your target's scope** — the three agents run in parallel.
- **Read test HTML files** to understand what each test expects before implementing fixes.
- When in doubt about logical/physical coordinate mapping, refer to `pkg/layout/writing_mode_converter.go` which has correct implementations for all writing modes.
