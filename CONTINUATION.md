# Continuation: Fix Two Remaining CSS2 Test Failures

## Context

Louis14 is a Go browser engine. We currently pass CSS2 97/99 and CSS3 105/105 WPT reftests. Two CSS2 tests remain failing. This document provides the diagnosis and fix plan for both.

## Current Baseline

- CSS2: 97/99, CSS3: 105/105
- Go binary: `/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go`
- Failing tests (both in `pkg/visualtest/testdata/wpt-css2/`):
  1. `box-display/block-in-inline-001.xht` — 0.7% pixel diff
  2. `generated-content/before-after-display-types-001.xht` — 3.3% pixel diff

## Quick Start

```bash
# Run just the two failing tests
go clean -testcache && go test ./pkg/visualtest/ -run "TestWPTReftests$/box-display/block-in-inline-001" -v
go clean -testcache && go test ./pkg/visualtest/ -run "TestWPTReftests$/generated-content/before-after-display-types-001" -v

# Full regression check
go clean -testcache && go test ./pkg/visualtest/ -run "TestWPTReftests$" -v 2>&1 | grep -E "Summary:|FAIL"
go clean -testcache && go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests" -v 2>&1 | grep -E "Summary:|FAIL"

# Render test vs ref for visual inspection
go run cmd/l14open/main.go pkg/visualtest/testdata/wpt-css2/box-display/block-in-inline-001.xht /tmp/bii-test.png 800 600
go run cmd/l14open/main.go pkg/visualtest/testdata/wpt-css2/box-display/block-in-inline-001-ref.xht /tmp/bii-ref.png 800 600
```

---

## Test 1: block-in-inline-001 (0.7%) — SHOULD BE STRAIGHTFORWARD

### What the test does

A `<td>` with red background contains:
```html
<span class="inline" style="display:inline; background:lime; color:black">
  Line 1
  <span class="block" style="display:block; background:green">Line 2</span>
  Line 3
</span>
```

Per CSS 2.1 §9.2.1.1, the inline span is split around the block. Three anonymous blocks are created:
1. Anonymous block wrapping inline fragment "Line 1" (lime background)
2. The block element "Line 2" (green background)
3. Anonymous block wrapping inline fragment "Line 3" (lime background)

The reference uses three `<div>` elements with lime/green/lime backgrounds filling the full cell width. Both cells should look identical: no red visible.

### What's wrong (visually confirmed from diff images)

The block child "Line 2" correctly fills the full td width with green. But the anonymous inline fragments for "Line 1" and "Line 3" have **shrink-to-fit width** (only covering the text content area), leaving the red td background visible on the right side.

### Root cause

In `pkg/layout/layout_inline_multipass.go`, the `ConstructFragments` phase creates two wrapper boxes for block-in-inline splits:

**Fragment 1** (line ~1431-1444):
```go
Width: contentBeforeMaxX - span.startX,  // ← text content width, NOT full width
```

**Fragment 2** (line ~1475-1488):
```go
Width: endX - (containerBox.X + containerBox.Border.Left + containerBox.Padding.Left),  // ← content width
```

Both should be full container width. The anonymous block box wrapping an inline fragment of a split inline element must stretch to the full width of the containing block (the td), identical to a block `<div>`.

### Fix

In `pkg/layout/layout_inline_multipass.go`, in the `ConstructFragments` function, find the block-in-inline split handling (search for `hasBlockChild` around line 1408):

1. **Fragment 1** (around line 1431): Change `Width` and `X`:
   ```go
   X:     containerBox.X + containerBox.Border.Left + containerBox.Padding.Left + wrapRelX,
   Width: availableWidth,
   ```

2. **Fragment 2** (around line 1475): Change `Width`:
   ```go
   Width: availableWidth,
   ```

Both fragments should use `availableWidth` (which equals the container's content width, passed from `layoutBlockChildren`). The X position of Fragment 1 should also be the container's left content edge, not `span.startX` (which is the inline content start position).

### Verification

After the fix, the left cell in the test should show:
- Full-width lime background for "Line 1"
- Full-width green background for "Line 2"
- Full-width lime background for "Line 3"
- No red visible anywhere

Both cells should look identical to the reference (both cells = three full-width lime/green/lime divs).

### Risk

This change only affects the block-in-inline split path in inline layout. Other tests that exercise block-in-inline: `block-in-inline-003.xht` (currently passes) and `opacity-affects-block-in-inline.html` (currently passes). Run both after fixing to check for regressions.

---

## Test 2: before-after-display-types-001 (3.3%) — MORE COMPLEX

### What the test does

Nine `<div>` elements each have `::before` and `::after` pseudo-elements. Each pseudo-element has different `display` type:
- `block`, `inline`, `inline-block` (LEFT COLUMN — these PASS)
- `table`, `inline-table`, `table-row-group` (LEFT COLUMN — table FAILS, others PASS)
- `table-row`, `table-cell`, `table-caption` (RIGHT COLUMN — all FAIL)

Each pseudo-element's content is: `counter(ctr) url(32x32-image) open-quote "Before " attr(class)` (and similar for after).

The reference file uses real `<span>` elements with the same display types and content, instead of pseudo-elements.

### What's wrong (from diff image analysis)

The diff shows pixel errors concentrated in 4 areas:
1. **display:table** (left column, 4th div): The 32x32 image gets stretched differently as part of table cell width distribution. The diagonal X-pattern lines in the image are offset by a few pixels.
2. **display:table-row** (right column, 1st div): Image and text positions are shifted within the anonymous table wrapper structure.
3. **display:table-cell** (right column, 2nd div): Text "Before table-cell" and "After table-cell" is vertically offset.
4. **display:table-caption** (right column, 3rd div): Image and text positions differ significantly.

Display types that render correctly: block, inline, inline-block, inline-table, table-row-group.

### Root cause

Pseudo-element synthetic nodes (created by `createPseudoElementNode()` in `pkg/layout/pseudo_elements.go` line 433) produce slightly different table anonymous box structures compared to real HTML `<span>` elements in the reference. The issues are:

1. **display:table**: When a synthetic `<span>` with `display:table` goes through table layout, its children (text + img + text) are placed in an anonymous cell. The image stretching depends on column width distribution, which may differ between pseudo-element and real element code paths due to how `measureCellContentWidth` handles synthetic node trees vs real HTML node trees.

2. **display:table-row/cell/caption**: These table-internal display types trigger anonymous table wrapper generation (CSS 2.1 §17.2.1). The anonymous wrappers for pseudo-element synthetic nodes may have slightly different structure (missing parent relationships, different whitespace handling) than for real HTML elements.

### Investigation approach

The best approach is to compare the box trees produced by test vs reference for each failing display type:

1. Add temporary debug logging in `layoutNode` (layout_block.go) that prints box dimensions for nodes with display:table, table-row, table-cell, table-caption.

2. Render the test file and reference file separately, comparing the box tree output.

3. Identify where dimensions diverge. The root cause will be one of:
   - **Image dimension handling**: `createPseudoElementNode()` creates `<img>` nodes at line 495-502. These should get dimensions from `GetImageDimensionsWithFetcher()` in `layoutNode`. Verify the image dimensions are identical between test and ref.
   - **Anonymous table wrapper sizing**: The `buildTableInfo()` and `processTableRows()` functions in `layout_table.go` handle anonymous row/cell generation. The pseudo-element's synthetic spans may produce different cellGrid structures than real HTML spans.
   - **Table column width distribution**: `calculateColumnWidths()` distributes width based on content. If content width measurement differs for synthetic vs real nodes, column widths will differ, causing image stretching differences.

### Key files

- `pkg/layout/pseudo_elements.go:433-540` — `createPseudoElementNode()` synthetic node creation
- `pkg/layout/layout_block.go:576,594` — pseudo-element entry points
- `pkg/layout/layout_table.go:86-166` — `layoutTable()` main function
- `pkg/layout/layout_table.go:169-335` — `processTableRows()` with anonymous box generation
- `pkg/layout/layout_table.go:338-437` — `calculateColumnWidths()` column distribution

### Fix strategy

After identifying the divergence point via debug logging:

**If image dimensions differ**: Fix `createPseudoElementNode()` to set explicit width/height attributes on synthetic `<img>` nodes, matching what `layoutNode` expects.

**If anonymous table structure differs**: Fix `processTableRows()` or `buildTableInfo()` to handle synthetic pseudo-element nodes identically to real HTML nodes. The synthetic node tree from `createPseudoElementNode()` should match the HTML structure `<span>1<img/>text</span>` exactly.

**If column width distribution differs**: Fix `measureCellContentWidth()` to handle the content structure within pseudo-element synthetic nodes the same way it handles real HTML element content.

### Risk

Changes to table layout may affect other table tests. The table height distribution fix from this session (explicit height → non-explicit rows get extra space) already touches this area. Run all CSS2 tests after any changes.

---

## Key File Locations

- `pkg/layout/layout_inline_multipass.go:1408-1495` — Block-in-inline fragment creation (Test 1 fix)
- `pkg/layout/layout_inline_multipass.go:1250-1330` — Block child in ConstructFragments
- `pkg/layout/pseudo_elements.go:433-540` — `createPseudoElementNode()` (Test 2)
- `pkg/layout/layout_table.go` — Table layout, anonymous box generation (Test 2)
- `pkg/layout/layout_block.go:576,594` — Pseudo-element creation entry points
- `pkg/visualtest/reftest_runner_test.go` — Test runner (0.3% pixel threshold)

## Order of attack

1. **Fix block-in-inline-001 first** — It's a targeted 2-line fix (fragment widths).
2. **Then investigate before-after-display-types-001** — Requires debug logging to identify the divergence, then targeted fix.
3. Run full CSS2+CSS3 regression suite after each change.
