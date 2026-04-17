# Table Layout: Checkpoint 3 + Rowspan Feature Completion

This continuation picks up after commit `ea08da28` ("Wire single-pass row
sizing into Layout (ckpt 2/3)"). Two independent pieces of work remain:

- **Ckpt 3 of refactor (b)** — complete the migration to the single-pass
  sizing pipeline built in ckpt 2 and delete the legacy
  build-then-mutate-then-reposition flow.
- **Rowspan feature completion** — louis14 currently parses `colspan`
  but not `rowspan`, and the sizing algorithm has no cell-slot tracker
  or rowspan-distribution pre-pass. Blink's model handles this; we do
  not.

The two pieces are independent. **Recommended order: ckpt 3 first.** It is
narrowly scoped, parity-verified, and the test surface it affects is
known. Rowspan adds a real feature and requires parser + placement +
sizing work; it is cleaner to layer it on top of a migrated pipeline than
to do it against the now-legacy path.

---

## Baseline

Commits on `fix/flexbox-fast` (verified):

- `ba8cad09` — Generate anonymous table boxes at tree-build time (refactor a)
- `ee4d3f29` — Introduce TableTypes + TableConstraintSpaceData (ckpt 1/3)
- `ea08da28` — Wire single-pass row sizing into Layout (ckpt 2/3)

Test counts (all three commits pass these at 0 diff):

- `TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|001b|horiz-001-table)` — 3/3 pass
- `TestWPTCSS3Reftests/css-tables` — 50/6720 passed, 59 failed
- `TestWPTReftests` — 93/99 passed, 6 failed

Ckpt 2 also verified locally with `useSinglePassTableSizing=true` and
`debugVerifySinglePassParity=true`: no parity panic on any css-tables
test, meaning the legacy and single-pass paths produce byte-equal row
block-sizes across every test that exercises redistribution.

---

## Part 1 — Checkpoint 3 of 3 (refactor b)

### Goal

Flip the single-pass sizing path on by default and delete the legacy
build-then-mutate-then-reposition code. After ckpt 3 the
`TableLayoutAlgorithm.Layout()` flow is:

1. Collect rows / captions (unchanged from refactor a).
2. Lay out cells once to obtain intrinsics.
3. `computeRows` → `TableTypesRows` with intrinsic sizes.
4. `distributeTableBlockSizeToRows` mutates the Rows vector in place.
5. `buildTableConstraintSpaceData` produces an immutable
   `*TableConstraintSpaceData`.
6. Fragment construction reads final block-sizes directly from the Rows
   vector — no post-append mutation, no `rowInfos.childIdx` /
   `blockOffset` tracking.
7. CSS 2.1 §17.5.4 vertical-align runs inline during row construction
   (row height is already known from the Rows vector).

### Concrete work

1. **Flip the flag.** Edit `pkg/layout/table_layout.go`:
   ```go
   useSinglePassTableSizing = true
   ```
   Leave `debugVerifySinglePassParity = false` — parity is already
   established and the legacy path is being removed.

2. **Delete the legacy auto-height redistribution block.** In
   `Layout()`, search for the comment "Legacy path" introduced in ckpt
   2 and remove the entire `legacyHeights` / `legacyHasExplicit` /
   `legacyAutoCount` / `legacyPerRow` computation. The surviving path
   should compute `spRows` / `distributeTableBlockSizeToRows` directly
   and copy `spRows[i].BlockSize` into whatever structure the
   reposition sweep reads. The parity verifier
   (`verifySinglePassParity`) becomes dead code — delete it too.

3. **Delete `rowInfos.childIdx` / `blockOffset`.** The reposition sweep
   that uses them (search for "Reposition row fragments with adjusted
   heights" in `Layout()`) must be replaced by building rows at final
   size on the first pass. Concretely:

   - Before the cell-layout loop, run `computeRows` +
     `distributeTableBlockSizeToRows` using the **intrinsic** row
     heights collected from a lightweight pre-pass. This matches
     Blink's ordering: `TableLayoutAlgorithm::ComputeRows()`
     (table_layout_algorithm.cc ~L2073) runs before `GenerateFragment`
     (~L1533).
   - Then the cell-layout loop lays out each cell at
     `rows[rowIdx].BlockSize` and the row fragment is appended at its
     final size — no revisit.

   The `rowInfos` struct itself may survive (it still holds
   `cellAligns`, `rowBaseline`, etc.) but the `childIdx` and
   `blockOffset` fields can go.

4. **Move §17.5.4 vertical-align inline.** The current code has a
   post-append pass that computes cell vertical offsets after row
   heights stabilize (search for "§17.5.4" or "verticalAlign" in
   `Layout()`). Because row block-size is now known when each cell
   fragment is built, compute the cell's vertical offset at cell build
   time — no post-pass. Mirrors Blink's `TableSectionLayoutAlgorithm`
   row loop (`table_section_layout_algorithm.cc:65–84`).

5. **Thread `TableConstraintSpaceData` into sub-spaces.** Currently
   `buildTableConstraintSpaceData` is called but the result is
   discarded. Wire it into the cell / row / section constraint spaces
   via the fields added in ckpt 1:

   - `ConstraintSpace.TableSectionData = data`
   - `ConstraintSpace.TableSectionIndex = <section index>`

   Mirrors Blink's
   `TableLayoutAlgorithm::CreateConstraintSpaceData()`
   (table_layout_algorithm.cc ~L979–1044) threaded through
   `TableSectionConstraintSpaceBuilder`. Today the cell layout doesn't
   need to read from it — but threading it through unblocks future
   features (percentages that resolve against the section, rowspan
   distribution that reads final row sizes during cell placement).

6. **Blink references** (read before coding per CLAUDE.md §2):
   - `third_party/blink/renderer/core/layout/table/table_layout_algorithm.cc`
     — `Layout()` ~L2285, `ComputeRows()` ~L2073,
     `CreateConstraintSpaceData()` ~L979
   - `third_party/blink/renderer/core/layout/table/table_section_layout_algorithm.cc`
     — the row loop at L65–84 shows the "read-from-constraint-space"
     pattern for row block-size and offset accumulation
   - `third_party/blink/renderer/core/layout/table/table_layout_algorithm_types.h`
     — `TableTypes::Row` / `Section` definitions

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
```

Targets:

- Motivating flexbox three: pass at 0 diff.
- css-tables: ≥ 50 passed, 59 failed (must not regress from ckpt 2).
- CSS2: 93 passed, 6 failed (must not regress).

### Commit

One commit, message style:

```
Retire legacy table row-sizing path (ckpt 3/3)
```

Body should explain:

- The flag flip and the legacy code deletion.
- Which files changed and why (`table_layout.go`; `rowInfos` field
  reduction; §17.5.4 move).
- That `TableConstraintSpaceData` is now threaded through sub-spaces
  (with reference to the Blink analogue).
- Confirmed test counts.

### Risks

- The reposition sweep uses `blockOffset` not only for row heights but
  also for cell vertical positioning within the row. Before deleting
  it, audit every read of `rowInfos[i].blockOffset` to confirm they
  all have an equivalent in the single-pass flow.
- Caption layout (`collectRowsAndCaptions` returns captions separately)
  is independent of row sizing but shares the block-offset accumulator
  in `Layout()`. Make sure captions still place correctly after the
  loop restructuring.
- `ConstraintSpace` is a value type — threading `TableSectionData`
  through sub-spaces requires either extending the builder
  (`NewConstraintSpaceBuilder`) or setting the field post-`.Build()`.
  Check existing builder patterns before choosing.

---

## Part 2 — Rowspan Feature Completion

### Current gap

louis14 recognises the HTML `colspan` attribute on `<td>` / `<th>` and
threads it into `tableCell.colSpan`, but:

1. `buildRow` never reads the `rowspan` attribute. `tableCell.rowSpan`
   is hard-coded to `1`
   (`pkg/layout/table_layout.go:1020` — search for `rowSpan:     1,`).
2. There is no slot-grid / placement tracker. CSS 2.1 §17.5 requires
   that a rowspan-originator cell in row R occupies the same column
   index in rows R+1 … R+rowspan-1, and subsequent cells in those rows
   shift right to avoid it.
3. The 5-priority distribution's priority-2 bucket
   (`HasRowspanStart`, `distributeTableBlockSizeToRows` in
   `table_layout.go`) is wired but never fires because item (1) means
   `rowSpan > 1` never holds. Priority 2 is currently a degenerate
   even-distribution — Blink's algorithm is richer (see below).
4. No `DistributeRowspanCellToRows` pre-pass. Blink distributes a
   rowspan cell's minimum block-size across the rows it spans before
   the main 5-priority distribution runs, because a rowspan cell
   contributes to multiple rows' intrinsic sizes — not just one.

### What Blink does

- **Parse.** `HTMLTableCellElement::parseAttribute` / `rowSpanForBindings`
  (Blink's HTML layer) reads `rowspan` as an integer (default 1, clamped
  to [1, 65534] — WHATWG HTML §4.9.11).
- **Place.** `LayoutTableSection` maintains a `grid_` — a 2D array of
  cell slots — during cell placement. A rowspan-originator cell
  reserves `rowspan × colspan` slots; subsequent rows lay cells into
  the first unoccupied slot. See
  `third_party/blink/renderer/core/layout/table/layout_table_section.h`
  and `LayoutTableSection::AddChild` / `LayoutTableSection::EnsureRows`.
- **Distribute rowspan minimums.** `DistributeRowspanCellToRows`
  (`core/layout/table/table_layout_utils.cc` ~L1606) takes each
  rowspan cell's minimum block-size and distributes it across the rows
  it spans in proportion to each row's current block-size (or evenly
  if zero). This runs **before** the 5-priority distribution.
- **Distribute excess across rowspan originators.** Priority 2 of
  `DistributeExcessBlockSizeToRows` (table_layout_utils.cc ~L1201) is
  not simply "even across originator rows"; it preferentially grows
  the rows whose rowspan cells are under-served. Read the block
  around `DistributeExcessBlockSizeToRowsRowspanStart` if Blink has a
  helper, or walk the comment block carefully.

### Work to do

Order matters — each step depends on the previous.

1. **Parser.** In `buildRow`
   (`pkg/layout/table_layout.go`, search for `colspan`), add the
   symmetric `rowspan` read:
   ```go
   rowSpan := 1
   if child.DOMNode != nil {
       if rs, ok := child.DOMNode.GetAttribute("rowspan"); ok {
           if v := parseIntAttr(rs); v > 0 {
               rowSpan = v
           }
       }
   }
   ```
   Clamp to a reasonable upper bound (Blink uses 65534). Emit the
   value into `tableCell.rowSpan`.

2. **Slot grid during `collectRowsAndCaptions` / row construction.**
   A rowspan cell in row R at column C reserves (R, C) … (R+s-1, C).
   Subsequent cells in rows R+1 … R+s-1 must see column C as
   occupied and advance their column cursor past it. Implement a
   simple 2D `map[int]map[int]bool` or a dense `[][]bool` in the
   table section (or in `collectRowsAndCaptions` as a local). Assign
   `tableCell.colIndex` from the slot grid, not from a monotonically
   increasing counter.

   Blink analogue: `LayoutTableSection::AddChild` +
   `LayoutTableSection::EnsureRows` +
   `LayoutTableSection::AppendCell`.

3. **`DistributeRowspanCellToRows` pre-pass.** Add a helper in
   `table_layout.go` (or `table_layout_distribute.go` if the file is
   getting unwieldy) that walks rowspan-originator cells and
   contributes their min block-size to the rows they span. Call it
   from `computeRows` (or immediately after) so that by the time
   `distributeTableBlockSizeToRows` runs, row block-sizes already
   reflect rowspan minimums.

   Algorithm sketch (port from Blink's
   `DistributeRowspanCellToRows`):
   - For each rowspan cell with min block-size `m` spanning rows
     R … R+s-1:
   - Compute `current = Σ rows[R..R+s-1].BlockSize` plus row spacing
     between them.
   - If `m > current`, increase each spanned row's `BlockSize` to
     absorb the deficit. Distribute in proportion to current size, or
     evenly if all are zero.

4. **Fragment construction.** The cell-layout loop must place a
   rowspan cell fragment across the block-offset range of its spanned
   rows (not just its originating row). Its final block-size is
   `Σ rows[R..R+s-1].BlockSize + (s-1)*rowSpacing`. Mirrors Blink's
   `TableSectionLayoutAlgorithm` row-loop cell offset calculation.

5. **Priority 2 of the 5-priority distribution.** Replace the current
   even-across-rowspan-originators shortcut in
   `distributeTableBlockSizeToRows` with Blink's preferential
   algorithm. Read `DistributeExcessBlockSizeToRows` in
   `table_layout_utils.cc` at the rowspan-originators branch and port
   faithfully. If porting is too invasive for one commit, split:
   keep the current even shortcut for ckpt 3, and port the richer
   algorithm in a follow-up commit — flag the gap in the commit
   message.

### Verification

Same three commands as above. Additional targeted tests to spot-check
rowspan handling:

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables/.*rowspan.*' -count=1 -timeout 600s \
  -v 2>&1 | tail -40
```

Targets:

- Motivating flexbox three: still pass.
- css-tables: strictly **> 50 passed** once rowspan lands (rowspan
  tests previously failed at intrinsic-height-too-small; they should
  flip to pass).
- CSS2: 93 passed, 6 failed (unrelated; must not regress).

Any test that was passing before and regresses after rowspan lands is
a bug in the rowspan implementation — do not ship rowspan until
flat-or-better on the full `css-tables` run.

### Commit granularity

Split into at least two commits, preferably three:

- **parse + place:** `Parse rowspan attribute and track cell slot grid`
- **size minimums:** `Distribute rowspan cell minimums to spanned rows`
- **distribute excess / fragment placement:** `Place rowspan cells across spanned rows` (or split further if it helps review)

Each commit must compile, pass the motivating flexbox three, and not
regress `css-tables` / CSS2 counts.

---

## Principles (from CLAUDE.md — apply to both parts)

- **Study Blink first.** Read the referenced files before coding.
  Match type names, algorithm structure, and comment references.
- **All tests must pass at 0% diff.** A 0.5% diff is a failure, not
  acceptable.
- **Foundational correctness.** No point fixes. If rowspan exposes an
  unrelated bug, file it separately — do not paper over.
- **Run only the tests that matter** during iteration. Full-suite runs
  at commit checkpoints.
- **Worktree discipline.** If delegated to a worktree agent, the
  baseline must be pushed first, and the agent must commit to its own
  branch — never to `fix/flexbox-fast` or `master`.

---

## What to report back

After ckpt 3: the commit SHA, the three test counts, and confirmation
that the legacy path / `rowInfos.childIdx` / `blockOffset` are gone.

After rowspan: the commit SHAs, css-tables delta (expect
`50 + N / 6720` where N is the number of previously-failing rowspan
tests now passing), and a list of any still-failing rowspan tests
with a one-line diagnosis each.
