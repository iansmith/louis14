# Rowspan × visibility:collapse — Finish the Layout-Time Reduction

This continuation picks up after commit `d47b3e9b` ("Port Blink's
preferential priority-2 row distribution"). Two companion commits
(`e9aa884f` and `adc999bb`) wired `visibility: collapse` for table
rows and started reducing rowspan cells' block-size to the visible
spanned rows. The single outstanding failure is
`pkg/visualtest/testdata/wpt-css3/css-tables/visibility-collapse-rowspan-005.html`
(893/480000 px = 0.2%).

**Read this before anything else.** An earlier draft of this prompt
claimed Blink solves this at paint time with an overflow-clip flag on
the cell fragment. That was wrong — I verified against
`third_party/blink/renderer/core/layout/table/table_layout_utils.cc`
and it is not what Blink does. Blink reduces the cell's block-size
**entirely at layout time**. There is no paint-time clip flag in the
table paint path for this case. The remaining work in louis14 is to
finish the layout-time reduction that `adc999bb` started.

## What Blink actually does (verified)

`third_party/blink/renderer/core/layout/table/table_layout_utils.cc`,
`ComputeCellBlockSize`:

```cpp
LayoutUnit cell_block_size;
if (!rows[row_index].is_collapsed) {
  for (wtf_size_t i = 0; i < cell_block_constraint.effective_rowspan; ++i) {
    if (rows[row_index + i].is_collapsed)
      continue;
    cell_block_size += rows[row_index + i].block_size;
    if (i != 0)
      cell_block_size += border_spacing.block_size;
  }
}
```

A rowspan cell's final block-size is the sum of non-collapsed spanned
row block-sizes plus inter-row spacing between visible pairs. The
cell's fragment is then built at that reduced block-size. This is
essentially the same arithmetic louis14 did in Phase 3b in commit
`adc999bb`.

`third_party/blink/renderer/core/layout/table/table_row_layout_algorithm.cc`
sets `container_builder_.SetIsHiddenForPaint(true)` only when
`row.is_collapsed` — that's for the ROW fragment, not cells spanning
the row. louis14 achieves the same effect by simply not emitting a
row fragment for collapsed rows (commit `e9aa884f`).

`third_party/blink/renderer/core/layout/table/table_section_layout_algorithm.cc`
skips border-spacing before the first non-collapsed row. louis14's
Phase 3 has the equivalent `emittedRows` counter that gates
`blockSpacing` addition.

**Nothing in Blink's table paint path sets a fragment-level overflow
clip for rowspan cells over collapsed rows.** If you grep the
directory for `SetIsHiddenForPaint`, the one row-case hit is above;
no cell-case hit exists. If you grep for `SetClipsOverflow` in the
table layout directory, there are zero hits.

So the spec phrase "clipped at the row's edge" is implemented in
Blink as "the cell fragment is sized to the visible area" — content
that would have overflowed naturally is kept in check because the
cell is laid out *against that reduced constraint from the start*.
The cell's children (here, two `<p>` elements) see a smaller
available block-size and either shrink or overflow — and in this
test they overflow into the cell's own overflow area, which is then
clipped by the cell's rendering context only if the cell has
`overflow: hidden` in its style (which `<td>` does not, by default).

**Key realization:** the test's reference uses
`visibility: hidden` on the second `<p>`, not `display: none`. A
`visibility: hidden` element still takes layout space. So the
reference cell is laid out at its *full* natural block-size
(~130 px), with the second `<p>` present in layout but invisible.
That means the reference renders with the rowspan cell occupying
the visible row 1 extent AND the space that row 2 would have
occupied — because row 2 is collapsed to zero, the cell's natural
height flows into the vacated space, and row 3 starts below it.

Louis14 should arrive at the same end state: the rowspan cell sized
to its full natural block-size (not the visible-rows sum), with
row 3 positioned below the cell's natural bottom, because row 2 is
zero and the cell's natural extent determines the effective
block-position of row 3.

**This means commit `adc999bb` may be wrong**, or at least
incomplete. It sized the cell to the sum of visible row block-sizes
— but when the cell's own content is taller than that sum, the
spec-correct behavior (and Blink's actual behavior) is for the cell
to keep its content-driven height and push row 3 down.

## The real investigation

Before writing any code, **pin down the actual geometry**:

1. In `visibility-collapse-rowspan-005.html`:
   - Row 1's natural block-size (max of its visible cells' min
     block-size).
   - The rowspan cell's natural block-size (tall because of the two
     `<p>` elements — approximately 130 px depending on line-height).
   - Row 2's block-size is 0 (collapsed).
   - Row 3's natural block-size.

2. Predict Chrome's rendering:
   - Cell occupies rows 1 + (the zero-size row 2).
   - If the cell's intrinsic block-size > row 1's block-size, the
     cell's extent pushes row 3 down below the cell's own bottom —
     because the cell's rowspan extends through row 2 and its
     natural height exceeds the visible rows' sum.
   - Row 3 starts at: row 1 block-offset + max(row 1 block-size,
     cell natural block-size) + border-spacing × (number of
     non-collapsed inter-row spacings).

3. Render the test in a real browser once and save the reference
   image, plus dump the png louis14 currently produces. Diff them
   pixel-by-pixel visually (look at the image, don't just read the
   pixel count). The 893-pixel diff is probably a precise rectangle
   — identifying where the rectangle sits pinpoints the bug.

Only after that do you know whether the remaining gap is:

- **(a) The cell's block-size is wrong** — we clamped when we should
  not have (undo `adc999bb`'s reduction when the cell's natural
  height exceeds the visible sum), OR
- **(b) Row 3's position is wrong** — cell's height is fine but
  `blockOffset` after row 1 doesn't account for the cell's natural
  extent extending into zero-height row 2.

Blink's `ComputeCellBlockSize` tells us Blink reduces unconditionally
— it doesn't max against the cell's natural height. But the table's
section layout then computes row offsets from the row block-sizes,
which for row 2 is zero — so row 3 starts at row 1's bottom + 0.
Then the rowspan cell fragment (reduced in block-size) is placed at
row 1's origin with its reduced block-size. Its children
(`<p>` elements) laid out with the reduced block-size constraint
still overflow naturally.

So why does Chrome's render match the reference? Only two
possibilities:

1. **The reference with `visibility: hidden` second `<p>` renders
   the same as a cell that had only the first `<p>` visible, because
   Chrome clips overflow content in table cells by default** — even
   though `<td>` doesn't have `overflow: hidden` in its UA stylesheet.
2. **The reference's `visibility: hidden` second `<p>` still occupies
   space, row 3 is pushed down, and the test (with visible second
   `<p>`) similarly pushes row 3 down but then the second `<p>`'s
   text paints into row 3's vertical space**. This would mean
   louis14's 893-pixel diff is the text of the second `<p>` painting
   where row 3 is supposed to be — i.e., row 3 is correctly placed
   in both renders, but the second `<p>`'s text overlaps row 3's
   background in louis14 and doesn't in Chrome.

If (2) is the case, then Chrome does have a paint-time suppression
mechanism for the overflow portion. That wasn't visible in the files
I searched. The next investigation step is:

```
WebFetch third_party/blink/renderer/core/layout/table/layout_table_cell.cc
       third_party/blink/renderer/core/paint/table_cell_painter.cc
       third_party/blink/renderer/core/paint/table_row_painter.cc
```

And search specifically for "collapsed" and "overflow" in those files.

If none of them have collapsed-row-aware clipping logic, then (1) is
the case and Chrome clips table cell overflow by default. Check the
UA stylesheet `third_party/blink/renderer/core/html/resources/html.css`
for any `overflow` declaration on `table`, `td`, `th`, `tr`.

**Do not write code until this is resolved.** The earlier round of
changes shipped a fix (`adc999bb`) based on an incorrect mental
model. Don't repeat that.

## Hypothesis-driven steps

Once the investigation above narrows the cause:

### If the cell's block-size should NOT be clamped

- Revert or adjust the `adc999bb` spannedBlock computation. Size the
  cell fragment to its natural intrinsic block-size, same as a
  non-rowspan cell. The zero-block-size collapsed row contributes
  nothing to `blockOffset` in Phase 3 (already the case), so row 3
  starts at the correct offset naturally.
- Confirm Phase 3b doesn't also clamp the cell fragment's physical
  size elsewhere.

### If Chrome clips table cell overflow by default

- This is a UA-stylesheet gap, not a table-layout gap. Check
  `pkg/css/ua_stylesheet.go` (or equivalent) for the UA rules on
  `td`, `th`, `table`. Compare against Blink's `html.css` for
  `display: table-cell` elements.
- If louis14 is missing a `overflow: hidden` (or similar) on table
  cells, the fix is in the UA stylesheet, not the layout algorithm.
- Beware of regressions: many existing table tests rely on cell
  content being visible outside the cell (CSS 2.1 §17.5.1 says
  this is UA-dependent). Run the full `css-tables` suite after the
  UA rule change.

### If there's a third case

If neither (1) nor (2) matches, diff the louis14 render against
Chrome's render pixel-by-pixel and identify the exact rectangle
that differs. Consider: border-collapse interaction, cell baseline
alignment shifts caused by a collapsed row, or anonymous-cell
block-offset bugs.

## Verification

Same commands as previous rounds:

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables/visibility-collapse-rowspan-005' \
  -v -count=1
```

Target: **PASS at 0 diff**.

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -count=1

GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables' -count=1 -timeout 600s 2>&1 | grep Summary

GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary
```

Targets:

- Motivating flexbox three: 3/3 PASS.
- css-tables: ≥ 53/6720 (rowspan-005 flips).
- CSS2: 93/99 (must not regress).

If the fix is in the UA stylesheet (case 2), also run a broader
regression check, since UA changes affect every test:

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -count=1 -timeout 1800s 2>&1 | tail -40
```

## Principles (from CLAUDE.md)

- **Study Blink first.** The earlier round shipped `adc999bb` based
  on a mental model that wasn't verified against Blink source. Don't
  repeat that. Before writing code, `WebFetch` the relevant Blink
  files and quote what they actually do.
- **Foundational correctness.** If the investigation reveals that
  `adc999bb` or `e9aa884f` has a defect in reasoning (e.g., clamping
  when it shouldn't), fix the foundational issue — do not paper
  over with a clipping layer.
- **All tests must pass at 0% diff.** 893 px is not close enough.
  The fix must reduce it to 0.
- **Run only the tests that matter** during iteration. Full-suite
  runs only when the UA stylesheet changes or at commit checkpoints.

## What to report back

- The investigation finding: is the missing piece (a) unclamp the
  cell's block-size, (b) UA stylesheet `overflow` rule on table
  cells, or (c) something else. Quote the Blink code / Chrome
  rendering evidence that pinned it down.
- Commit SHA.
- The three test counts.
- Confirmation that `visibility-collapse-rowspan-005.html` is at 0
  diff. If not, the residual diff and the remaining suspect.
