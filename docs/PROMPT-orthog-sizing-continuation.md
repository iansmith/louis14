# Continuation Prompt: Remaining Orthogonal Sizing Failures (VLR + VRL)

## Task

Fix the remaining 16 failing `sizing-orthog-htb-in-*` WPT tests. The orthogonal block sizing infrastructure is correct — the test layouts produce the right widths. The failures are all in **reference rendering**: the references use complex table-based HTML with `td { display: block }`, and our engine renders them at slightly wrong positions or widths.

**Current: 22/38 pass (VLR: 16/20, VRL: 6/18). Target: all 38.**

## What Was Already Fixed (Don't Re-Investigate)

1. **Line-height in `computeLineMetrics`** (`inline_layout.go:619-648`): Now distributes CSS `line-height` half-leading into ascent/descent. This makes `<p>` line boxes 20px (not 16px) in VLR, so the `<p>` block contribution is 16+20+16=52px, matching the reference's 52px table cells.

2. **Anonymous table-cell wrapping** (`table_layout.go:322-396`): `buildRow()` now wraps non-table-cell children (like `<td style="display:block">`) in anonymous table-cell boxes. Whitespace-only text nodes are skipped. This makes the reference's td#data render as a table cell between td#before and td#after.

3. **Table `box-sizing: border-box`** (`cascade.go:288`): UA stylesheet sets `box-sizing: border-box` on `<table>`. This makes `width: 672px` with `padding: 84px` give content=504px, matching how browsers interpret table width.

4. **Logical margin resolution** (`cascade.go:900-1026`): `resolveLogicalBoxProperties` maps `_margin-block-start/end` to correct physical margins based on writing-mode. For VLR `<p>`: block margins = margin-left=16px + margin-right=16px.

## Remaining Failing Tests — Two Groups

### Group 1: VLR failures (6 tests)

| Test | Parent width | Body margins | Text type | Reference | Diff |
|------|-------------|-------------|-----------|-----------|------|
| 001 | auto | 100px L/R | long | 001-ref (calc, padding:84px) | 3.8% |
| 003 | auto | 100px L/R | short (15ch) | 003-ref (15ch, padding:84px) | 0.3% |
| 009 | 400px | 100px L/R | short (15ch) | 003-ref (same as 003) | 0.3% |
| 013 | auto | none | long | 013-ref (calc, no padding) | 3.4% |
| 015 | auto | none | short (15ch) | 015-ref (15ch, no padding) | 0.3% |
| 021 | 400px | none | short (15ch) | 015-ref (same as 015) | 0.3% |

**Key observation**: The 0.3% diffs (003, 009, 015, 021) are very close to passing. These use `width: 15ch` on td#data and have small text. The 3.4-3.8% diffs (001, 013) have long text with different wrapping widths between test and reference.

### Group 2: VRL failures (10 tests)

All 10 VRL sibling tests fail. The VRL reference files differ from VLR in:
- `left: calc(100% - ...)` positioning (right-to-left block direction)
- Cell order is reversed (td#after first, td#before last)
- `writing-mode: vertical-rl` on the `<p>` elements
- Systematic ~32px offset between test and reference (from margin collapsing)

## Root Cause Analysis

### Why the 0.3% tests fail (003, 009, 015, 021)

These references use `width: 15ch` on td#data. Example (003-ref):
```css
table { width: calc(136px + 3px + 15ch + 3px + 136px); padding: 0px 84px; }
td#data { border: blue solid 3px; display: block; width: 15ch; }
```

The visual diff shows test and reference are nearly identical — both render "Sentence before.", the short blue box, and "Sentence after." at approximately the same positions. The remaining ~0.3% diff is likely from:

1. **`ch` unit resolution**: The `ch` unit is the width of the "0" character. If our `ch` measurement differs slightly from what the reference author assumed, the td#data width differs by a few pixels, shifting "Sentence after." position.

2. **Table padding interaction with box-sizing**: The `width: calc(136px + 3px + 15ch + 3px + 136px)` is a precise pixel calculation. If the `ch` resolution or calc rounding differs even by 1px, the column distribution changes.

**Debugging approach**: Add `fmt.Printf` in `table_layout.go computeColumnWidths` to print the final column widths for the reference table, and compare with the expected widths from the calc expression. Check the `ch` unit value produced by our font measurement.

### Why the 3.4-3.8% tests fail (001, 013)

These references use `width: calc(... + 100% + ...)` (or `100vw`) to set the table width. The table is `position: absolute`.

Test 001 reference:
```css
html { width: calc(136px + 100vw + 136px); }
table { position: absolute; padding: 0px 84px; width: calc(136px + 100% + 136px); }
```

Test 013 reference (simpler):
```css
html { width: calc(52px + 100vw + 52px); }
table { position: absolute; width: calc(52px + 100% + 52px); }
```

The `100%` in the table width resolves against the containing block of the absolutely-positioned table. Since neither html nor body has `position` set, the containing block is the ICB (800×600). So `100%` = 800px.

**The critical issue**: In the test rendering, the body has block margins (100px or 8px) that shift content to the right. In the reference, the absolutely-positioned table starts at the ICB origin (x=0 + padding). This creates a systematic horizontal offset between test and reference.

For test 013 (no body margins in CSS, but UA 8px body margin):
- Test: content starts at x=8 (body margin-left = 8px in VLR block-start)
- Reference: table at x=0 (position:absolute, no left offset)
- Result: 8px shift → ~3.4% diff

For test 001 (100px body margins):
- Test: content starts at x=100 (body margin-left)
- Reference: table padding-left=84 → content at x=84
- Result: 16px shift (100-84=16) → ~3.8% diff

The 16px = 100px (body margin) - 84px (table padding). The reference author designed: 84px padding + 52px cell = 136px, to equal body margin (100px) + p block contribution (36px?). But with our 52px `<p>` (20px line + 32px margins), the math is: 100 + 52 = 152, not 136. So the reference expects a 36px `<p>` contribution, not 52px.

Wait — 136 = 100 (body margin) + 36. What's 36? It's 20px (line) + 16px (one margin only, not two). This suggests the reference author expected only ONE margin on the `<p>`, not two. In real browsers, the `<p>`'s block-start margin (16px) likely **collapses with the parent div's block-start**. So the `<p>` effective contribution is: 0 (collapsed start margin) + 20 (line) + 16 (end margin) = 36px.

**The bug**: In VLR, the `<p>` first child's block-start margin should collapse with the parent div's block-start edge (since the div has no border/padding). We implement this (canPropagateTop logic in block_layout.go:98-101), but the propagated margin may be getting resolved at a different position than the reference expects.

**Debugging approach**:
1. Trace the block layout of the containing div in VLR for test 001/013. Print `blockCursor`, child positions, and margin collapsing state for each child.
2. Compare: p#before contribution (should be 36px total: 0 collapsed + 20 line + 16 margin), ortho block (800px), p#after contribution (36px: 16 margin + 20 line + 0 propagated).
3. The total should be 36 + 800 + 36 = 872. With body margin 100px each side: 100 + 872 + 100 = 1072 = the reference's html width (136+800+136).

### Why VRL tests fail

The VRL references use `left: calc(100% - ...)` to position the table from the right side. In VRL mode:
- Block direction is right-to-left
- block-start = right, block-end = left
- Children stack from right to left

The VRL test layout positions content starting from the right (x=800-margin). The reference positions the table with an explicit `left` offset. Any margin collapsing or positioning difference between the VRL test and the HTB reference creates a systematic shift.

The VRL failures mirror the VLR failures but with added complexity from the right-to-left block direction. Fix the VLR issues first, then the same principles should help VRL.

## Key Files

| File | Lines | What |
|------|-------|------|
| `inline_layout.go` | 619-648 | computeLineMetrics — line-height half-leading (done) |
| `table_layout.go` | 322-396 | buildRow — anonymous cell wrapping (done) |
| `table_layout.go` | 395-520 | computeColumnWidths — column width distribution |
| `cascade.go` | 285-290 | UA table box-sizing (done) |
| `block_layout.go` | 95-101 | canPropagateTop — parent-child margin collapsing |
| `block_layout.go` | 257-280 | Child positioning with margin collapsing |
| `fragment_geometry.go` | 102-115 | ResolveMargins — margin resolution |
| `css/style.go` | 4033-4053 | GetLineHeight() |
| `css/cascade.go` | 900-1026 | resolveLogicalBoxProperties |

## Test Commands

```bash
# All orthogonal sizing tests
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s

# Specific failing test
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog-htb-in-vlr-003' ./pkg/visualtest/ -timeout 30s

# Full WM suite (current: ~536/787)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes' ./pkg/visualtest/ -timeout 600s 2>&1 | grep 'Summary'

# CSS2 regression check (current: ~88/99)
go test -v -run 'TestWPTReftests$' ./pkg/visualtest/ -timeout 300s 2>&1 | grep 'Summary'
```

## Recommended Attack Order

1. **Fix the 0.3% tests first** (003, 009, 015, 021). These are closest to passing. Debug the `ch` unit resolution and check if column width rounding in the reference matches. A 1-2px fix could pass all four.

2. **Fix margin collapsing for `<p>` in VLR** (001, 013). The `<p>` first-child block-start margin should collapse with the parent div, making the `<p>` effective contribution 36px (not 52px). Trace through the margin collapsing logic in block_layout.go to see if the collapsing is happening correctly and the cursor positions match the reference.

3. **Fix VRL tests** after VLR is correct. The same margin and positioning fixes should apply; the VRL references just mirror the layout.

## Important Note on Regressions

The line-height fix caused ~49 WM test regressions (585→536) and ~7 CSS2 regressions (95→88). These tests were accidentally passing with incorrect line-box heights (font-size instead of line-height). The fix is spec-correct per CSS 2.1 §10.8.1. Do NOT revert it.
