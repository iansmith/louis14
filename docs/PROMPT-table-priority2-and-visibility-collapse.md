# Table Layout Follow-ups: Priority-2 Distribution + Rowspan × visibility:collapse

This continuation picks up after commit `9af135b1` ("Place rowspan cells
across spanned rows"). Two independent gaps remain after the rowspan
feature landed:

- **Part 1 — Port Blink's preferential priority-2 distribution.** The
  current `distributeTableBlockSizeToRows` priority-2 bucket grants
  excess block-size evenly across rowspan-originator rows. Blink's
  `DistributeExcessBlockSizeToRows` does this preferentially based on
  per-rowspan-cell deficit. The shortcut survives because no test in
  the current corpus exposes the difference; it is technical debt
  flagged in `9af135b1`'s commit body.
- **Part 2 — Wire `visibility: collapse` for rows under a rowspan
  cell.** The single failing rowspan test
  (`visibility-collapse-rowspan-005.html`, 893/480000 px = 0.2%) needs
  the CSS Tables 3 §3.5 rule: a row with `visibility: collapse` is
  removed from layout, but cells whose rowspan overlaps it continue to
  render — clipped at the collapsed row's edge.

The two pieces are independent. **Recommended order: Part 2 first.**
It moves the visible test count and exercises real layout behavior;
Part 1 is a refactor of an internal distribution helper that today no
test touches and so cannot be verified end-to-end (only by unit
inspection). Doing the visibility-collapse work first means Part 1's
follow-up-only nature is undisturbed by any churn.

---

## Baseline

Commits on `fix/flexbox-fast` (verified):

- `e4238dd3` — Parse rowspan attribute and track cell slot grid (Rowspan A)
- `74744de8` — Distribute rowspan cell minimums to spanned rows (Rowspan B)
- `9af135b1` — Place rowspan cells across spanned rows (Rowspan C)

Test counts at `9af135b1`:

- `TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|001b|horiz-001-table)` — 3/3 pass
- `TestWPTCSS3Reftests/css-tables` — 51/6720 passed, 58 failed
- `TestWPTReftests` (CSS2) — 93/99 passed, 6 failed

Targeted rowspan spot-check at `9af135b1`:

```
border-collapse-rowspan-cell.html ........... PASS
rowspan-cell-border-after-color.html ........ PASS
zero-rowspan-001.html ....................... PASS
zero-rowspan-002.html ....................... PASS
visibility-collapse-rowspan-005.html ........ FAIL  (target of Part 2)
```

---

## Part 1 — Port Blink's preferential priority-2 distribution

### Current state

`distributeTableBlockSizeToRows` in `pkg/layout/table_layout.go`
(currently at line 2176; the priority-2 block at line 2244) handles the
five-priority excess distribution from CSS 2.1 §17.5.3 (Blink's
`DistributeExcessBlockSizeToRows`, `table_layout_utils.cc` ~L1095).
Priority 2 today reads:

```go
// Priority 2 — rowspan originators.
// Blink (~L1201): evenly distribute remaining excess across rows
// that originate a rowspan > 1 cell. We do not currently track
// individual rowspan cells' minimum contribution here (ckpt 2
// matches current behavior); Blink's DistributeRowspanCellToRows
// is a separate pre-pass. So this bucket simply grants priority
// for any remaining excess to rowspan-originator rows.
if remaining > 0 && len(rowspanIdx) > 0 {
    per := remaining / float64(len(rowspanIdx))
    accum := 0.0
    for k, i := range rowspanIdx {
        var share float64
        if k == len(rowspanIdx)-1 {
            share = remaining - accum
        } else {
            share = per
            accum += share
        }
        rows[i].BlockSize += share
    }
    remaining = 0
}
```

Two problems with this:

1. **Even-split is wrong.** Blink does not split evenly across rowspan
   originators — it splits in proportion to each rowspan cell's
   *unmet* minimum (the deficit that priority-1 percent distribution
   could not satisfy). Two rowspan cells, one with a 100px deficit
   and one with a 10px deficit, must not get equal shares of the
   excess.
2. **Bucket consumes all remaining excess.** `remaining = 0` after
   priority 2 starves priorities 3–5 of any excess whenever any
   rowspan-originator row exists. That's also wrong: priority 2 only
   absorbs as much as the rowspan deficits demand; surplus flows to
   priority 3 (unconstrained non-empty rows) per Blink.

### What Blink does

Read before coding (per CLAUDE.md §2):

- `third_party/blink/renderer/core/layout/table/table_layout_utils.cc`
  — `DistributeExcessBlockSizeToRows` (~L1095). The rowspan-originators
  branch is approximately L1201–L1252. Pay attention to the comment
  block describing the per-cell deficit accumulation.
- `third_party/blink/renderer/core/layout/table/table_layout_utils.cc`
  — `DistributeRowspanCellToRows` (~L1606). louis14 already mirrors
  this as `distributeRowspanCellToRows` at table_layout.go:2100. The
  priority-2 algorithm reuses the same "current vs minimum" deficit
  computation per cell.

Algorithm sketch (mirrors Blink):

For each rowspan cell c with originator row R, span s, minimum
block-size m:

1. Compute `current = Σ rows[R..R+s-1].BlockSize + (s-1)*rowSpacing`
   *as it stands at the start of priority 2* (after priorities 1 has
   run, before priority 2 mutations begin).
2. `cellDeficit = max(0, m - current)`.
3. Track a **per-row deficit attribution** `rowDeficit[i]` = sum of
   the proportional share of each cell's deficit attributable to row i
   (proportional to row i's share of the cell's spanned rows: even
   split when all rows are zero; row-block-size-proportional when at
   least one is non-zero — same rule as the existing
   `distributeRowspanCellToRows` helper).
4. After accumulating across all rowspan cells:
   - `totalDeficit = Σ rowDeficit[i]`.
   - If `totalDeficit == 0`, priority 2 is a no-op; `remaining` is
     untouched.
   - If `totalDeficit > 0`, distribute `min(remaining, totalDeficit)`
     in proportion to `rowDeficit[i]` across all rows in
     `rowspanIdx`.
   - Decrement `remaining` by the distributed amount (do **not** set
     to zero).

The "deficit" is what the rowspan cell still wants beyond what
priorities-0-and-1 + intrinsic sizing already gave it. Once each
rowspan cell's deficit is satisfied, further excess flows to
priority 3.

### Concrete work

1. **Plumb per-cell minimums into `distributeTableBlockSizeToRows`.**
   The function today takes `(rows TableTypesRows, excess float64)`.
   It needs the rowspan-cell list with `(rowIndex, rowSpan, minBlock)`
   triples, since the per-cell deficit is the input to the
   preferential split. Two reasonable shapes:

   - Add a new struct field on `TableTypesRow` that lists incoming
     rowspan cells, populated during Phase 1 measurement. Cleanest
     conceptually but mutates the struct API.
   - Pass the rowspan list as a third parameter:
     `distributeTableBlockSizeToRows(rows, excess, rowspanCells)`.
     Smaller diff. Recommended.

   Either way, define a small struct in table_layout.go alongside
   `TableTypesRow`:

   ```go
   type rowspanCellInfo struct {
       rowIndex int
       rowSpan  int
       minBlock float64
   }
   ```

   In `Layout()`, build the slice from `measured[].cells` (every cell
   with `cell.rowSpan > 1`) and pass it through.

2. **Replace the priority-2 block.** Sketch:

   ```go
   // Priority 2 — rowspan originators (Blink-style preferential).
   // Each rowspan cell wants Σ spanned rows ≥ its minimum block-size.
   // Compute the per-row contribution to each cell's deficit, then
   // distribute min(remaining, totalDeficit) in proportion. Surplus
   // flows to priority 3.
   if remaining > 0 && len(rowspanCells) > 0 {
       rowDeficit := make([]float64, len(rows))
       totalDeficit := 0.0
       for _, rc := range rowspanCells {
           end := rc.rowIndex + rc.rowSpan
           if end > len(rows) {
               end = len(rows)
           }
           span := end - rc.rowIndex
           if span <= 1 {
               continue
           }
           current := 0.0
           for i := rc.rowIndex; i < end; i++ {
               current += rows[i].BlockSize
           }
           current += float64(span-1) * rowSpacing
           if rc.minBlock <= current {
               continue
           }
           cellDeficit := rc.minBlock - current
           // Per-row attribution: proportional when any spanned row
           // is non-zero, else even.
           total := 0.0
           for i := rc.rowIndex; i < end; i++ {
               total += rows[i].BlockSize
           }
           if total > 0 {
               for i := rc.rowIndex; i < end; i++ {
                   share := cellDeficit * rows[i].BlockSize / total
                   rowDeficit[i] += share
                   totalDeficit += share
               }
           } else {
               per := cellDeficit / float64(span)
               for i := rc.rowIndex; i < end; i++ {
                   rowDeficit[i] += per
                   totalDeficit += per
               }
           }
       }

       if totalDeficit > 0 {
           toDistribute := remaining
           if toDistribute > totalDeficit {
               toDistribute = totalDeficit
           }
           accum := 0.0
           lastIdx := -1
           for i := range rows {
               if rowDeficit[i] > 0 {
                   lastIdx = i
               }
           }
           for i := range rows {
               if rowDeficit[i] <= 0 {
                   continue
               }
               var share float64
               if i == lastIdx {
                   share = toDistribute - accum
               } else {
                   share = toDistribute * rowDeficit[i] / totalDeficit
                   accum += share
               }
               rows[i].BlockSize += share
           }
           remaining -= toDistribute
       }
   }
   ```

   Note: priority 2 must NOT set `remaining = 0`. Surplus flows to
   priority 3.

3. **Pass `rowSpacing` into `distributeTableBlockSizeToRows`** —
   `rowSpacing` is needed for the `current` computation (same rule as
   `distributeRowspanCellToRows`). Add a fourth parameter.

4. **Fold `rowspanIdx` removal** — the legacy `rowspanIdx` set used
   for the even-split is no longer needed; the new code reads from
   `rowspanCells` directly. Remove its construction in the participating-
   row enumeration (lines ~1959–1978 today).

### Verification

Required — all must match baseline:

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -v -count=1

GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables' -count=1 -timeout 600s 2>&1 | grep Summary

GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary

# Targeted rowspan spot-check (must all PASS):
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables/(border-collapse-rowspan-cell|rowspan-cell-border-after-color|zero-rowspan-001|zero-rowspan-002)' \
  -v -count=1
```

Targets:

- Motivating flexbox three: pass at 0 diff.
- css-tables: ≥ 51/6720 (must not regress from `9af135b1`).
- CSS2: 93/99 (must not regress).
- Targeted rowspan four: all pass.

Because no current css-tables test exposes the priority-2 algorithmic
difference, success is "no regression." If the new code accidentally
fixes a previously-failing test, great; if it regresses any of the
51, the per-cell deficit computation has a bug — diagnose before
shipping.

### Commit

One commit:

```
Port Blink's preferential priority-2 row distribution
```

Body should explain:

- The bug (even-split + total-consumption) the new algorithm fixes.
- The Blink reference (`DistributeExcessBlockSizeToRows`,
  `table_layout_utils.cc` ~L1201).
- That priority 2 no longer zeros `remaining`; surplus flows to
  priority 3.
- Confirmed test counts.

### Risks

- **`rowspanIdx` removal**: priority-2's `rowspanIdx` was the only
  user of the `HasRowspanStart` flag in the participating-row loop.
  `HasRowspanStart` is still set on `TableTypesRow` and is used by
  the legacy code path comments — leave the flag in place even if
  unused after this change, to keep `TableTypesRow` field-compatible
  with Blink's `Row` struct.
- **Last-row remainder accounting** for the priority-2 split must
  use the *last index with `rowDeficit > 0`*, not the last spanned
  row. Otherwise a row with zero deficit absorbs the remainder.
- **Cells whose span clamps to ≤ 1** (rowspan extends past end of
  table) should fall through to "originating row gets the cell's full
  block-size" — the existing `distributeRowspanCellToRows` already
  handles this; the new priority-2 code must match (skip via `if span
  <= 1 { continue }`).

---

## Part 2 — Wire `visibility: collapse` for rows under a rowspan cell

### Current state

`TableTypesRow.IsCollapsed` exists (in `pkg/layout/table_types.go`)
but is never set: nothing reads `row.style.GetVisibility()` and maps
`visibility: collapse` to `IsCollapsed = true`. As a result:

- `<tr style="visibility: collapse">` rows render at their normal
  block-size, not zero.
- Rowspan cells that intersect a collapsed row are unaffected.

The single failing rowspan test
(`pkg/visualtest/testdata/wpt-css3/css-tables/visibility-collapse-rowspan-005.html`)
contains:

```html
<table>
  <tr><td>R1L</td><td>R1C</td>
      <td rowspan=2 style="width:100px">
        <p>Supersuperlongword</p>
        <p>row with lots and lots of text</p>
      </td></tr>
  <tr style="visibility: collapse"><td>R2L</td><td>R2C</td></tr>
  <tr><td>R3L</td><td>R3C</td><td>R3R</td></tr>
</table>
```

The reference renders the rowspan cell with the second `<p>` clipped
to the visible (row-1-only) portion (it uses
`visibility: hidden` on the second paragraph as a layout-equivalent
proxy). The cell still occupies the geometric extent of row 1 only.

### What CSS Tables 3 §3.5 says

From <https://drafts.csswg.org/css-tables-3/#visibility-collapse-cell-rendering>:

> If the visibility of a row is collapse, the row must not be
> displayed. The space the row would have taken up is removed from
> the table; the cells whose row span overlaps the collapsed row
> remain displayed but must be clipped at the row's edge.

Two distinct effects:

1. **Row removal.** A collapsed row contributes 0 to the table's
   block-size. Cells *originating* in the collapsed row are not
   rendered.
2. **Rowspan clipping.** A cell spanning the collapsed row keeps its
   originating-row placement and its span continues structurally,
   but the visible portion is clipped to non-collapsed segments.

### What Blink does

Read before coding (per CLAUDE.md §2):

- `third_party/blink/renderer/core/layout/table/table_layout_algorithm_types.h`
  — `TableTypes::Row::is_collapsed` field. louis14 mirrors this in
  `TableTypesRow.IsCollapsed`.
- `third_party/blink/renderer/core/layout/table/table_layout_algorithm.cc`
  — `ComputeRows()` sets `is_collapsed` from the row style's
  `visibility` property.
- `third_party/blink/renderer/core/layout/table/table_section_layout_algorithm.cc`
  — the row loop skips collapsed rows for fragment construction.
- `third_party/blink/renderer/core/layout/table/table_layout_utils.cc`
  — `DistributeExcessBlockSizeToRows` already gates each priority
  bucket on `!IsCollapsed` (louis14 mirrors this).

### Concrete work

Order matters — each step depends on the previous.

1. **Set `IsCollapsed` on `TableTypesRow`.** In `computeRows`
   (`pkg/layout/table_layout.go`), check the row's style:

   ```go
   if r.row != nil && r.row.style != nil {
       if r.row.style.GetVisibility() == css.VisibilityCollapse {
           rows[i].IsCollapsed = true
       }
   }
   ```

   Verify `css.VisibilityCollapse` exists in `pkg/css`. If not, add
   the enum value (mirrors Blink's `EVisibility::kCollapse`).

2. **Force `BlockSize = 0` for collapsed rows.** The row's intrinsic
   contribution must be zero regardless of cell content, because
   collapsed rows occupy zero space. Either:
   - In `computeRows`, after computing the intrinsic, override:
     `if rows[i].IsCollapsed { rows[i].BlockSize = 0 }`.
   - Or in the Phase 1 measure loop, skip the `rowHeight` accumulation
     for collapsed rows. The first option is cleaner because Phase 1
     should still measure cells (the rowspan cells need their
     intrinsic for distribution; non-rowspan cells in a collapsed row
     are simply not displayed).

3. **Skip collapsed rows in Phase 3.** The row loop in `Layout()`
   (around line 536 today) appends a row fragment per row. For
   collapsed rows:
   - Do **not** add the row fragment to the table builder.
   - Do **not** advance `blockOffset` (rowHeight is 0, so the
     advance is already a no-op, but skip the inter-row spacing
     too — `if rowIdx > 0 && blockSpacing > 0 { blockOffset += blockSpacing }`
     should be skipped or emit zero).
   - Cells originating in the collapsed row (rowSpan == 1) are
     simply not emitted. Cells with `rowSpan > 1` originating in a
     collapsed row are an edge case — Blink treats the cell as
     not displayed at all (the originating row's collapse propagates
     to its cells). Implement that: `if rowIsCollapsed { continue }`
     before the deferred-rowspan append.

4. **Pre-compute spanned-block excluding collapsed rows for rowspan
   cells.** In Phase 3b (the rowspan-cell emission loop, around line
   683 today), compute `spannedBlock` as before but the sum naturally
   excludes collapsed rows since their `BlockSize == 0`. Keep the
   inter-row spacing add only between **visible** spanned rows:

   ```go
   spannedBlock := 0.0
   visibleSpanCount := 0
   for i := rowIdx; i < end; i++ {
       if spRows[i].IsCollapsed {
           continue
       }
       spannedBlock += spRows[i].BlockSize
       visibleSpanCount++
   }
   if visibleSpanCount > 1 && blockSpacing > 0 {
       spannedBlock += float64(visibleSpanCount-1) * blockSpacing
   }
   ```

   This is the "clipped at collapsed-row edge" geometric effect: the
   cell's stretched fragment is exactly the visible area.

5. **Decide on cell clipping vs cell-size reduction.** Per spec the
   cell is "clipped at the row's edge" — its declared structural
   size still spans the original rows, but rendered painting clips
   to visible segments. louis14 has no clipping primitive in the
   fragment model; the simplest correct behavior is to size the
   fragment to the *visible* spanned area only. That matches the
   reference (which uses `visibility: hidden` on the overflow
   paragraph). Step 4 already does this.

   Verify the test's reference renders identically: both render with
   the rowspan cell sized to row 1 only (single-row height), with
   row 2 absent, and row 3 below at the post-row-1 + post-row-3
   offset.

6. **Excess distribution already handles `IsCollapsed`.** The
   participating-row enumeration in `distributeTableBlockSizeToRows`
   (line ~1962) reads `if rows[i].IsCollapsed { continue }` already.
   No change needed there.

### Verification

Required — all must match or improve the baseline:

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables/visibility-collapse-rowspan-005' \
  -v -count=1
```

Target: PASS at 0 diff.

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables/.*visibility-collapse.*' \
  -v -count=1
```

Inspect each — there are several visibility-collapse tests in the
corpus (visibility-collapse-row-001, -002, etc.). Some may already
pass (without rowspan they exercise plain row removal); some may
already fail and remain failing if they depend on other missing
features. **Any currently-passing visibility-collapse test that
regresses is a bug** — diagnose and fix before shipping.

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

- Motivating flexbox three: pass.
- css-tables: ≥ 52/6720 (the target test should flip pass; ideally
  more if other visibility-collapse tests also flip).
- CSS2: 93/99 (must not regress).

### Commit granularity

One commit if visibility-collapse on plain rows already worked (just
the rowspan-intersection piece is new). Two commits if `IsCollapsed`
was never wired at all and step 1+2 represent the bulk of the
behavior change:

- **`Wire visibility: collapse for table rows`** — steps 1, 2, 3.
  Should make any visibility-collapse-row-NNN.html tests that don't
  involve rowspan flip pass.
- **`Clip rowspan cells at collapsed-row edges`** — steps 4, 5.
  Targets visibility-collapse-rowspan-005.html.

Each commit must compile, pass the motivating flexbox three, and not
regress `css-tables` / CSS2 counts.

### Risks

- **`computeRows` operates on `tableRowIntrinsic`, not directly on
  the table row's style.** Confirm the intrinsic struct already
  carries `row` (yes — `tableRowIntrinsic.row *tableRow`), and that
  `tableRow` carries `style`. If not, plumb it through.
- **`row.style` may be nil for anonymous row wrappers.** Anonymous
  rows from CSS 2.1 §17.2.1 normalization don't have user CSS;
  `visibility: collapse` only applies when `row.style != nil`.
- **`visibility: collapse` on a section / row group is also
  spec-defined** (CSS 2.1 §11.1.1, CSS Tables 3 §3.5). Out of scope
  for this prompt — louis14 has no row-group fragment yet.
- **Multi-row collapse.** A rowspan cell crossing TWO collapsed
  rows in a 4-row span should have its visible area equal to only
  the two non-collapsed rows. Step 4's loop handles this correctly.
- **Border-collapse interaction.** The test has
  `border-collapse: collapse` and `border: 1px solid blue`. The
  collapsed row's borders interact with neighboring rows'. Verify
  the rendered borders match the reference; if not, a separate
  border-collapse-aware fix is needed (file as a follow-up, not in
  this commit).

---

## Principles (from CLAUDE.md — apply to both parts)

- **Study Blink first.** Read the referenced files before coding.
  Match type names, algorithm structure, and comment references.
- **All tests must pass at 0% diff.** A 0.5% diff is a failure, not
  acceptable.
- **Foundational correctness.** No point fixes. If
  visibility-collapse exposes an unrelated bug, file it separately —
  do not paper over.
- **Run only the tests that matter** during iteration. Full-suite
  runs at commit checkpoints.
- **Worktree discipline.** If delegated to a worktree agent, the
  baseline must be pushed first, and the agent must commit to its
  own branch — never to `fix/flexbox-fast` or `master`.

---

## What to report back

After Part 1: commit SHA, the three test counts, and confirmation
that priorities 3-5 still receive surplus excess (no participating-
row test should regress).

After Part 2: commit SHA(s), the css-tables delta, and the list of
visibility-collapse tests with current pass/fail. If
`visibility-collapse-rowspan-005.html` still fails after the work,
include a one-line diagnosis (what feature is still missing).
