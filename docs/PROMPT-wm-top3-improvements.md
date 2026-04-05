# Writing Modes: Top 3 Independent Improvement Targets

Current state: 564 pass / 447 fail (55.8% pass rate) across 1011 writing-modes tests.

These three targets are **independent** (touch different subsystems) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Float Physical-to-Logical Direction Mapping (~40 tests)

### Problem

CSS `float: left`/`right` are **always physical** (left/right edge in the viewport), but the layout code passes these values directly to `ExclusionSpace` as if they were logical inline-start/end directions. In vertical writing modes, the mapping between physical left/right and logical start/end changes:

- `horizontal-tb`: left=inline-start, right=inline-end (current code assumes this always)
- `vertical-lr`: left=block-start, right=block-end (floats should position on inline-start/end, not block edges)
- `vertical-rl`: left=block-end, right=block-start

### Affected Tests (~40 failures)

| Category | Fail/Total | Pass% |
|----------|-----------|-------|
| float-vlr | 6/7 | 14% |
| float-vrl | 6/6 | 0% |
| float-shrink-to-fit-vlr | 4/4 | 0% |
| float-shrink-to-fit-vrl | 4/4 | 0% |
| contiguous-floated-table-vlr | 4/4 | 0% |
| contiguous-floated-table-vrl | 4/4 | 0% |
| float-contiguous-vlr | 2/6 | 66% |
| float-contiguous-vrl | 2/6 | 66% |
| ortho-htb-alongside-vrl-floats | 4/4 | 0% |
| clearance-calculations-vrl | 4/4 | 0% |

### Root Cause (from code analysis)

In `pkg/layout/block_layout.go` lines 568-642, `layoutFloat()`:
```go
floatSide := childStyle.GetFloat()  // Returns FloatLeft or FloatRight (PHYSICAL)
floatBlockOffset := es.FindFloatPosition(floatSide, ...)  // Passes physical as if logical
```

And in `pkg/layout/exclusion_space.go` `FindAvailableInlineSize()`:
```go
if e.Side == css.FloatLeft {
    // Comment says "start-side float" but FloatLeft is PHYSICAL, not logical start
    startOffset = endEdge
}
```

### What Blink Does

In Blink, `float: left/right` is converted to the **logical float placement** based on the containing block's writing mode. In `LayoutBlockFlow::InsertFloatingObject()`, the float's logical side is determined by mapping the physical CSS property through the containing block's `WritingMode` and `Direction`. The ExclusionSpace then operates entirely in logical coordinates.

Specifically, in vertical writing modes with `direction: ltr`:
- `float: left` in VLR = float to **inline-start** (top in physical)
- `float: right` in VLR = float to **inline-end** (bottom in physical)

The float is positioned along the **inline axis**, not the block axis. The current code incorrectly associates `left`/`right` with inline-start/end without checking writing mode.

### Fix Location

- `pkg/layout/block_layout.go` — `layoutFloat()`: Convert `css.FloatLeft`/`css.FloatRight` to logical `InlineStart`/`InlineEnd` based on the parent's WDM before passing to ExclusionSpace.
- `pkg/layout/exclusion_space.go` — `FindFloatPosition()`, `FindAvailableInlineSize()`: Ensure the `Side` field uses logical values, not physical. May need a new `LogicalFloatSide` type.
- `pkg/layout/block_layout.go` — `clearType` handling: `clear: left`/`right` also needs physical-to-logical mapping.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/clearance" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ortho-htb-alongside" -count=1
```

Also regression check:
```bash
go test -v -run "TestListReftestResults" -count=1  # Must stay >= 89/99
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/margin-collapse" -count=1  # Must stay 20/20
```

---

## Target 2: Orthogonal Flow Available-Size Resolution (~26 tests)

### Problem

CSS Writing Modes §10.3.2 defines complex rules for how orthogonal children (different writing mode from parent) determine their available inline-size when the parent's block-size is indefinite. The current code only checks the immediate parent's `max-height`, but the spec requires:

1. Walk up the ancestor chain to find the nearest **scroller** (`overflow` != `visible`) with a definite block-size
2. Use that scroller's block-size as the available block for the orthogonal child
3. If no scroller is found, fall back to the ICB (viewport) size
4. `max-height` alone (without `overflow`) should NOT be used as a constraint

### Affected Tests (~26 failures)

| Category | Fail/Total | Pass% |
|----------|-----------|-------|
| available-size | 19/23 | 17% |
| orthogonal-root-resize-icb | 7/7 | 0% |

### Root Cause (from code analysis)

In `pkg/layout/block_layout.go` lines 69-77:
```go
orthogonalAvailableBlock := childAvailableBlock
if childAvailableBlock == Indefinite {
    if maxBlock, hasMax := ResolveMaxBlockSize(bla.style, wdm, bla.space, geom); hasMax {
        orthogonalAvailableBlock = maxBlock  // BUG: uses max-height even without overflow
    }
}
```

This only checks the immediate parent's max-height. Per the spec, it should:
1. Not use max-height when parent has `overflow: visible`
2. Walk up the ancestor chain looking for a scroller with definite size
3. Properly fall back to ICB when no suitable ancestor exists

The 4 passing tests (001, 003, 011, 022) happen to work because they have a scroller with fixed height as the immediate parent, or the max-height shortcut works accidentally.

### What Blink Does

In Blink, `ComputeOrthogonalAvailableSize()` walks the ancestor constraint space chain. Each ancestor checks:
1. If it has `overflow` != `visible` and a definite block-size → use that as the constraint
2. If it has a definite block-size but no scrolling → continue to parent
3. At the root → use ICB

The available size for orthogonal children is propagated via the `ConstraintSpace`, not computed locally. Blink stores an `OrthogonalFallbackInlineSize` in the constraint space that ancestors can override.

### Fix Location

- `pkg/layout/block_layout.go` — Replace the simple max-height check with proper scroller detection. The ConstraintSpace could carry an `OrthogonalContainerBlockSize` field that ancestors populate when they have `overflow` != `visible` and a definite block-size.
- `pkg/layout/constraint_space.go` — Add field for orthogonal available block from nearest scroller ancestor.
- The `orthogonal-root-resize-icb` tests may also need iframe/nested document support fixes.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/available-size" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-root" -count=1
```

---

## Target 3: Inline-Block Baseline Alignment in Vertical Modes (~14 tests)

### Problem

In vertical writing modes, inline-block elements should use the **central baseline** (center of the inline extent) instead of the font ascent/descent baseline used in horizontal modes. The current code always uses font ascent for baseline alignment, regardless of writing mode.

### Affected Tests (~14 failures)

| Category | Fail/Total | Pass% |
|----------|-----------|-------|
| inline-block-alignment | 6/6 | 0% |
| baseline-inline-non-replaced | 4/4 | 0% |
| inline-table-alignment | 4/4 | 0% |

### Root Cause (from code analysis)

In `pkg/layout/inline_layout.go` lines 689-711 (metrics computation):
```go
ibAscent := text.FontAscentFromFont(fontSize, fontPath)
totalAscent := r.Margins.BlockStart + ibAscent
// Always uses font ascent, regardless of writing mode!
```

And in positioning (lines 546-573):
```go
ibAscent := text.FontAscent(fontSize, bold, italic, mono, ahem)
blockPos = maxAscent - ibAscent  // Assumes font ascent works in vertical
```

The `wdm` (WritingDirectionMode) is available in `computeLineMetrics()` at line 616 but **never consulted** for baseline selection.

### What Blink Does

CSS Writing Modes 3 §4.3 specifies:
- In **horizontal writing modes**: use alphabetic baseline (font ascent/descent)
- In **vertical writing modes**: use **central baseline** = `blockSize / 2`

In Blink, `InlineItem::ComputeBoxMetrics()` checks the writing mode to select the appropriate baseline type. For vertical modes, the central baseline is `border_box_block_size / 2` (half the inline-block's block extent). This places the inline-block centered on the line's central axis.

### Fix Location

- `pkg/layout/inline_layout.go` — `computeLineMetrics()`: When `wdm.IsVertical()`, compute inline-block baseline as `blockSize / 2` instead of font ascent.
- `pkg/layout/inline_layout.go` — Positioning code (lines 546-573): Same adjustment for vertical modes.
- May also need to adjust `maxAscent`/`maxDescent` computation for text runs in vertical modes to use central baseline.

### Verification

```bash
cd pkg/visualtest
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/inline-block-alignment" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/baseline-inline" -count=1
go test -v -run "TestWPTCSS3Reftests/css-writing-modes/inline-table-alignment" -count=1
```

---

## Independence Check

| | block_layout.go | exclusion_space.go | constraint_space.go | inline_layout.go | text/measure.go |
|---|---|---|---|---|---|
| Target 1 (Floats) | layoutFloat section | Yes | - | - | - |
| Target 2 (Sizing) | orthogonal sizing section | - | Yes | - | - |
| Target 3 (Baseline) | - | - | - | Yes | Maybe |

All three targets touch different subsystems and can be developed independently.

## IMPORTANT: Agent Guidelines

- Study Blink's approach before writing code.
- Commit and report at each milestone (don't batch everything to the end).
- All 20 margin-collapse tests must continue to pass.
- CSS2.1 tests must remain >= 89/99.
- Run the specific category tests AND regression tests before reporting success.
