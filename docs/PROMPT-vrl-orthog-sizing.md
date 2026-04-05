# Continuation Prompt: 3 Remaining VRL Orthogonal Sizing Failures

## Task

Fix the 3 remaining `sizing-orthog-htb-in-vrl-*` WPT tests. The VLR counterparts of all 3 pass
pixel-perfect, so the issue is specific to VRL (vertical-rl) block direction handling.

**Current: 35/38 pass. Target: 38/38.**

## What Was Already Fixed (Don't Re-Investigate)

1. **Default font-family: serif** (`cascade.go:18-25`): UA stylesheet sets `font-family: serif` on
   `<html>`, resolving to Liberation Serif (tabular digits, ch = 0.5em). Fixed the 0.3% ch-unit tests.

2. **Font-family-aware measurement** (`line_breaker.go`, `inline_layout.go`, `render/`): CSS
   font-family is threaded through text measurement via `resolveFontPath()` and
   `FontPathForFamily()`. Both layout and rendering use the correct font.

3. **calc() with percentage resolution** (`fragment_geometry.go:ResolveInlineSize`,
   `css/style.go:resolveLengthOrPercent`): `calc(52px + 100% + 52px)` now correctly resolves the
   `%` term using the containing block's inline-size as the percentage base (was resolving `%` to 0).

## Remaining Failing Tests

| Test | Diff | Parent width | Body margins | Font | Text type | VLR sibling |
|------|------|-------------|-------------|------|-----------|-------------|
| vrl-008 | 1.8% | 400px | 100px L/R | monospace | non-breaking word (50 chars) | vlr-008 PASS |
| vrl-013 | 3.4% | auto | 0px L/R | serif (default) | long wrapping text (01..00) | vlr-013 PASS |
| vrl-020 | 1.6% | 400px | 0px L/R | monospace | non-breaking word (50 chars) | vlr-020 PASS |

## Visual Analysis

All 3 failures show the same pattern: a **~16–20px horizontal shift** between test and reference.
Everything else (text content, wrapping, blue border, column order) matches.

### Test vrl-008 (1.8%, 8609 pixels)
- **Test**: "Sentence after." at x≈100, blue box x≈120–700, "Sentence before." at x≈720
- **Ref**: Same content but shifted ~16px rightward
- Body margins: 100px L/R. Container: 400px explicit width. Font: monospace.
- Text: `01020304050607080910111213141516171819202122232425` (non-breaking, 50 chars, single line)

### Test vrl-013 (3.4%, 16344 pixels)
- **Test**: "Sentence after." at left, blue box extends to right edge, no "Sentence before." visible
- **Ref**: Blue box wraps within bounds, "Sentence before." visible on the right side (at left of viewport)
- Body margins: 0px L/R. Container: auto width. Font: default (serif).
- Text: `01 02 03 ... 99 00` (wrapping, long)
- **Reference uses `direction: rtl` on html** and `direction: ltr` on table — unique among the 3

### Test vrl-020 (1.6%, 7472 pixels)
- **Test**: Same layout as 008 but no body margins
- **Ref**: Shifted ~16px rightward
- Body margins: 0px L/R. Container: 400px explicit width. Font: monospace.

## Root Cause Hypothesis

In VRL, block-start = RIGHT, block-end = LEFT (right-to-left block progression).
The `<p>` first child has `margin-block-start: 1em = 16px`, which maps to `margin-right` in VRL.

The VLR siblings pass because VLR block-start = LEFT, and the margin propagation/collapsing
logic in `block_layout.go` was developed for left-to-right block progression. In VRL, the
propagated block-start margin should shift content from the RIGHT edge, but it may be shifting
from the LEFT edge instead (or not shifting at all), producing the ~16px offset.

### Specific areas to investigate:

1. **Parent-child margin collapsing in VRL** (`block_layout.go:95-101, 257-280`):
   `canPropagateTop` and `propagatedTopMargin` handle block-start margin propagation. In VRL,
   block-start = right. Check that the propagated margin is correctly applied to the right side
   of the parent (not the left side).

2. **`WritingModeConverter.ToPhysicalOffset`** (`writing_mode_converter.go:146-157`):
   For VRL+LTR: `X = outerW - block - innerW`, `Y = inline`. This converts logical block offset
   → physical x. Verify that the propagated margin strut is accounted for in the `outerW`
   calculation or the block offset.

3. **Fragment builder's physical conversion** (`fragment_builder.go:140-168`):
   Children's logical offsets are converted to physical using `conv.ToPhysicalOffset(child.offset,
   childPhysSize)`. The converter's `outerSize` is the CONTENT-BOX size. If the propagated
   margin changes the effective content box but the converter still uses the original size,
   children would be mis-positioned in VRL.

4. **VRL-013 specifically**: The reference uses `html { direction: rtl }` and
   `table { direction: ltr }`. This affects the static position of the absolutely positioned
   table. Check that our abspos layout handles the `direction` property on the containing block
   correctly when computing the static position.

## Key Files

| File | Lines | What |
|------|-------|------|
| `block_layout.go` | 95-101 | canPropagateTop — parent-child margin collapsing |
| `block_layout.go` | 257-280 | Child positioning with margin collapsing |
| `block_layout.go` | 317-330 | Final block-size computation |
| `fragment_builder.go` | 140-168 | Logical→physical offset conversion via WritingModeConverter |
| `writing_mode_converter.go` | 146-157 | VRL ToPhysicalOffset: `X = outerW - block - innerW` |
| `writing_mode.go` | 70-73 | IsFlippedBlocks() — true for VRL and sideways-rl |
| `static_position.go` | 186, 269 | IsFlippedBlocks adjustments for abspos static position |
| `out_of_flow_layout.go` | 40-120 | Abspos layout — insets, static position, constraint eq |
| `fragment_geometry.go` | 120-180 | ResolveInlineSize — calc() with percentage |
| `css/cascade.go` | 910-1030 | resolveLogicalBoxProperties — margin mapping for VRL |

## Test Commands

```bash
# All 3 failing VRL tests
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog-htb-in-vrl-(008|013|020)' ./pkg/visualtest/ -timeout 60s

# Their passing VLR siblings (for comparison)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog-htb-in-vlr-(008|013|020)' ./pkg/visualtest/ -timeout 60s

# Full orthogonal sizing suite (current: 35/38)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s

# Full WM suite (current: 538/787)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes' ./pkg/visualtest/ -timeout 600s 2>&1 | grep 'Summary'

# CSS2 regression check (current: 88/99)
go test -v -run 'TestWPTReftests$' ./pkg/visualtest/ -timeout 300s 2>&1 | grep 'Summary'
```

## Debugging Approach

1. **Add debug prints** to `block_layout.go` child positioning loop (lines 257-280). For each
   child with an id, print: tag, id, wdm, blockOffset, blockSize, blockCursor, marginBS, marginBE,
   propagatedTopMargin. Compare VRL-013 output vs VLR-013 output. The block offsets should be
   symmetric — what's offset 36 in VLR should mirror to the right in VRL.

   Use `child.TagName()` and `child.DOMNode.Attributes["id"]` to filter.

2. **Check the physical positions** in the final fragment. After `Build()` in the fragment builder,
   print the physical X/Y of each child. In VRL, children placed at logical block-offset 36 should
   map to physical X = contentWidth - 36 - childWidth.

3. **Compare with the reference table layout**. The reference table is absolutely positioned and
   uses `left: calc(100% - ...)` in tests 008/020, and `direction: rtl` + no explicit `left` in
   test 013. Print the abspos element's resolved position from `out_of_flow_layout.go`.

## Important: Regression Safety

- VLR tests must stay at 20/20 (all pass)
- Full WM suite must stay ≥ 538/787
- CSS2 must stay at 88/99
