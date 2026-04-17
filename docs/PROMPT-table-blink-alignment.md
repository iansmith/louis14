# Table Layout: Blink Alignment Refactors

## Background

Commit `c563415b` ("Fix row-height redistribution for anonymous table rows") fixed a
bug where CSS 2.1 §17.5.3 row-height redistribution silently skipped anonymous
rows. The fix was a minimum-surface-area patch: record each row's fragment
index and block offset in `rowInfos` at append time, then iterate `rowInfos`
during redistribution and the §17.5.4 vertical-align pass.

The fix is correct but structurally different from Blink. Research into
Blink's table layout (third_party/blink/renderer/core/layout/table/) showed
two architectural patterns louis14 does not have:

1. **Anonymous wrapping is normalized at tree-construction time.** By the time
   `LayoutTableSection` / `LayoutTableRow` algorithms run, every stray child
   has been wrapped in a real anonymous `LayoutTableSection` / `LayoutTableRow`
   via `CreateAnonymousWithParent`. The layout code has zero "is this an
   anonymous row" branches — anonymous and authored rows are indistinguishable.

2. **Row sizing is single-pass via `TableTypes::Rows` + `TableConstraintSpaceData`.**
   Blink computes all row block-sizes into a `Vector<TableTypes::Row>` **before**
   any row fragment is built, runs redistribution directly on that vector, then
   threads the finalized vector into each section/row's constraint space via an
   immutable `TableConstraintSpaceData`. `GenerateFragment()` then builds
   fragments sequentially, reading final sizes from the constraint-space data.
   No "build-then-mutate-then-reposition" step exists — and therefore no class
   of bug where fragment repositioning silently misses some rows.

Our current `table_layout.go`:
- Wraps nothing at tree-construction time; `collectRowsAndCaptions` builds
  in-memory row descriptors during `Layout()`.
- Builds row fragments optimistically at intrinsic height, appends them to
  `builder.children`, then revisits `builder.children` to reposition + resize.
- Only handles auto-height rows in redistribution (Blink has a 5-priority
  distribution: percent → rowspan originators → unconstrained non-empty →
  empty → proportional fallback).

This prompt covers **two independent refactors**. Do them in order: (a) first,
because it simplifies (b); then (b). Commit each separately.

---

## Refactor (a): Anonymous Table Box Generation at Tree-Construction Time

### Goal

Match CSS 2.1 §17.2.1 + Blink's pattern: when the DOM tree is walked into a
`LayoutInputNode` tree, insert anonymous `display: table-row-group`, `table-row`,
and `table-cell` boxes around stray children so that by the time
`TableLayoutAlgorithm.Layout()` runs, every child of a table/table-row-group is
a row (authored or anonymous), every child of a row is a cell (authored or
anonymous), and every child of a cell is what CSS 2.1 §17.2.1 calls an
"in-flow box".

### What Blink does

- `LayoutTable::AddChild` (in `core/layout/layout_table.cc`): if the new child is
  not a section (`LayoutTableSection`) or a caption, wrap it in an anonymous
  `LayoutTableSection` via `LayoutTableSection::CreateAnonymousWithParent`.
- `LayoutTableSection::AddChild` (in `core/layout/layout_table_section.cc`):
  if the new child is not a `LayoutTableRow`, wrap it in an anonymous
  `LayoutTableRow` via `LayoutTableRow::CreateAnonymousWithParent`.
- `LayoutTableRow::AddChild`: same pattern for cells.

All three anonymous wrappers inherit the minimal style needed (display value,
writing-mode, direction) from the parent.

### Louis14 current state

- `pkg/layout/table_layout.go` has `collectRowsAndCaptions` (around the top of
  `Layout()`) that iterates children at layout time and synthesizes in-memory
  row/cell records. For stray text or block content, it creates a row with
  `row.node == nil` and `rowBuilder` without `SetLayoutNode`. This was the
  direct cause of the anonymous-row redistribution bug.
- The equivalent of `AddChild` in louis14 is in `pkg/layout/builder/` (or
  wherever the `LayoutInputNode` tree is built from the DOM). Find the file
  that currently walks the DOM + style tree and emits `LayoutInputNode`s —
  that's where wrapping belongs.

### Work to do

1. **Study Blink's `LayoutTable::AddChild` and `LayoutTableSection::AddChild`
   first** before writing code. Mirror the wrapping rules precisely. Don't
   invent our own.
2. **Find louis14's tree-build code path.** Likely places:
   - `pkg/layout/input_node.go` or similar
   - `pkg/layout/tree_builder.go` if it exists
   - The code that turns DOM + CSS style into `LayoutInputNode`s
   Grep for "DisplayTable" / "DisplayTableRow" / call sites of `ComputeStyle`
   during tree building.
3. **Add anonymous wrapping in three places** — analogues of the three
   `AddChild` hooks:
   - At a `display: table` / `display: inline-table` parent: wrap non-row-group,
     non-caption, non-column-group children in an anonymous
     `display: table-row-group`. (Note: anonymous row-groups wrap runs of stray
     children, not individual children — one anonymous row-group can contain
     multiple anonymous rows.)
   - At a `display: table-row-group` / `-header-group` / `-footer-group` parent:
     wrap non-row children in an anonymous `display: table-row`.
   - At a `display: table-row` parent: wrap non-cell children in an anonymous
     `display: table-cell`.
4. **Anonymous box style.** Set only the minimal properties: `display`, inherited
   writing-mode and direction, and nothing else (per CSS 2.1 §17.2.1 the
   anonymous box has no borders, padding, or backgrounds). Verify by checking
   Blink's `CreateAnonymousWithParent` — it uses `ComputedStyle::CreateAnonymousStyleWithDisplay`.
5. **Delete the in-layout synthesis.** Once wrapping happens at tree build,
   `collectRowsAndCaptions` no longer needs to handle stray content —
   every child it sees is already a row. The `row.node == nil` / "anonymous row"
   branches in `collectRowsAndCaptions` should all go away. The `childIdx` /
   `blockOffset` tracking added in `c563415b` should **stay** for now (it's still
   correct for either representation); refactor (b) will remove the need for it.
6. **Verify generation doesn't break non-table content.** Anonymous wrapping
   only fires for direct children of table boxes — make sure nothing spills
   over to normal block/inline content.

### Verification

```bash
# The tests that motivated c563415b must still pass:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -v -count=1

# Broader table-related coverage (check for regressions):
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables' -count=1 -timeout 600s 2>&1 | tail -5

# CSS2 suite should stay at 6 failures:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary
```

### Risks

- If the DOM walker is shared with non-table content, wrapping rules can leak.
  Dispatch strictly on parent `display`, not on child type.
- Anonymous boxes need unique-enough identity (for layout caching, fragment
  parents, etc.) but no DOM node. Check how louis14's `LayoutInputNode` handles
  `DOMNode == nil` today.
- Runs of stray children should collapse into a **single** anonymous wrapper,
  not one wrapper per child. E.g. three bare text nodes directly inside a
  `<table>` produce **one** anonymous row-group containing one anonymous row
  containing one anonymous cell, not three of each.

### Commit

One commit. Message style: `Generate anonymous table boxes at tree-build time`
with a body explaining the CSS 2.1 §17.2.1 rules and the Blink analogues.

---

## Refactor (b): Single-Pass Row Sizing via `TableTypes.Rows` + `TableConstraintSpaceData`

### Goal

Replace the current build-then-mutate-then-reposition flow with Blink's
single-pass model. After refactor (b), `TableLayoutAlgorithm.Layout()` looks
like:

1. Compute a `[]TableTypes.Row` with **finalized** block-sizes (intrinsic
   sizing + distribution already applied).
2. Freeze it into an immutable `TableConstraintSpaceData`.
3. Walk sections/rows once, building fragments at their final sizes and
   offsets. No mutation of `builder.children`. No repositioning. No
   `childIdx` / `blockOffset` tracking.

### What Blink does

Key files and functions (all under `third_party/blink/renderer/core/layout/table/`):

- **`table_layout_algorithm_types.h`**: `TableTypes::Row` struct (fields:
  `block_size`, `start_cell_index`, `cell_count`, `baseline`, `percent`,
  `is_constrained`, `has_rowspan_start`, `is_collapsed`). `using Rows =
  Vector<Row>`.
- **`table_constraint_space_data.h`**: `TableConstraintSpaceData` —
  ref-counted, immutable, contains `Vector<Section>` and `Vector<Row>` plus
  column data. Threaded into each section/row's constraint space.
- **`table_layout_algorithm.cc`**:
  - `TableLayoutAlgorithm::ComputeRows()` (~L2073): walks sections, aggregates
    per-row minimum via `ComputeMinimumRowBlockSize`.
  - `TableLayoutAlgorithm::Layout()` (~L2285) calls `ComputeRows()` first,
    then `DistributeTableBlockSizeToSections` (~L2128: comment "Redistribute
    CSS table block size if necessary"), then `GenerateFragment()` (~L1533)
    to build section fragments.
  - `CreateConstraintSpaceData()` (~L979–1044) packs finalized sizes into
    `TableConstraintSpaceData`.
- **`table_layout_utils.cc`**:
  - `ComputeMinimumRowBlockSize` (~L315).
  - `DistributeExcessBlockSizeToRows` (~L1095) — 5-priority distribution:
    1. Percent rows
    2. Rowspan originators
    3. Unconstrained non-empty rows
    4. Empty rows
    5. Proportional fallback
  - `DistributeRowspanCellToRows` (~L1606),
    `DistributeSectionFixedBlockSizeToRows` (~L1618),
    `DistributeTableBlockSizeToSections` (~L1625).
- **`table_section_layout_algorithm.cc`**: row loop L65–84 — reads `Row.block_size`
  from the constraint space and accumulates `offset.block_offset += fragment.BlockSize()`.

### Work to do

1. **Study Blink's five distribution priorities and the `TableTypes::Row` struct
   before writing code.** Our current redistribution only handles step 3
   (unconstrained auto-height rows) — steps 1, 2, 4, 5 are missing. Mirror the
   struct field names (`BlockSize`, `StartCellIndex`, `CellCount`, `Baseline`,
   `Percent`, `IsConstrained`, `HasRowspanStart`, `IsCollapsed`).
2. **Introduce `pkg/layout/table_types.go`** with `Row`, `Rows`, and
   `Section` types mirroring Blink's.
3. **Introduce `TableConstraintSpaceData`** (immutable, ref-counted — in Go,
   use a pointer to a struct with only exported-ish unexported fields set
   during construction; document that callers must not mutate). Thread it into
   `ConstraintSpace` (new field `TableSectionData *TableConstraintSpaceData`
   + `TableSectionIndex int`).
4. **Replace the two-pass flow in `TableLayoutAlgorithm.Layout()`:**
   - Phase 1 (new): `computeRows` walks sections/rows/cells to build `Rows`
     with intrinsic `block_size` (via cell min/max block-size analog to
     `ComputeMinimumRowBlockSize`).
   - Phase 2 (new): `distributeTableBlockSize` runs the 5-priority distribution
     against `Rows`. Port the algorithm from `DistributeExcessBlockSizeToRows`.
   - Phase 3 (new): build `TableConstraintSpaceData`.
   - Phase 4 (refactored): walk sections/rows, build fragments at finalized
     sizes. Remove `rowInfos.childIdx` / `blockOffset` (no longer needed),
     remove the post-append mutation/reposition blocks that commit
     `c563415b` added, remove the §17.5.4 vertical-align pass's index-based
     lookup (cells can be aligned inline during row construction because
     row height is already known).
5. **Preserve the existing vertical-align semantics** (§17.5.4). In Blink this
   happens during row fragment construction, not as a post-pass, because the
   row's final block-size is already known from `TableConstraintSpaceData`.
6. **Handle rowspan properly.** Our current code's handling of rowspans is
   limited; Blink's `DistributeRowspanCellToRows` (table_layout_utils.cc
   ~L1606) shows the correct algorithm. Don't expand scope here beyond
   matching current behavior unless the WPT suite catches new regressions —
   call out any rowspan gaps in commit messages.

### Verification

```bash
# The tests motivating c563415b must still pass:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -v -count=1

# Full flexbox suite — must stay >= 589 passes:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox' -count=1 -timeout 600s 2>&1 | grep Summary

# Tables suite — record baseline before starting; must not regress:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-tables' -count=1 -timeout 600s 2>&1 | grep Summary

# CSS2 suite — must stay at 6 failures:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary
```

### Risks

- Row sizing touches every table test. Record a full baseline (pass/fail
  lists, not just counts) for `css-tables`, `css-flexbox`, and `TestWPTReftests`
  before starting. Diff after each checkpoint commit.
- `TableConstraintSpaceData` adds a field to `ConstraintSpace`. Audit every
  `ConstraintSpace` construction site to ensure non-table callers don't pass
  stale/incorrect data.
- If refactor (a) hasn't landed yet, anonymous-row handling in `computeRows`
  needs the same `row.node == nil` guards currently in
  `collectRowsAndCaptions`. This is why (a) comes first — after (a), every row
  is a real row and `computeRows` can iterate uniformly.

### Commit granularity

Break this into at least three checkpoint commits:

1. Introduce `TableTypes` + `TableConstraintSpaceData` types. No behavior change;
   existing code paths still run. Types just exist.
2. Implement `computeRows` + `distributeTableBlockSize` against the new types.
   Do **not** yet replace the old redistribution code — build the new logic in
   parallel, guard it with a `useSinglePassTableSizing` bool (default false),
   and verify both paths produce the same numbers on current tests.
3. Flip the default to true, delete the old post-append mutation code, delete
   the `childIdx` / `blockOffset` tracking, and verify the test suite holds.

Report test suite pass counts at every checkpoint.

---

## General rules (from CLAUDE.md)

- **Study Blink first.** Before writing any code in either refactor, read the
  referenced Blink files. Match their type names, algorithm structure, and
  comments (with source file references).
- **All tests must pass at 0% diff.** Small pixel diffs are failures, not
  acceptable.
- **Foundational correctness.** Don't cherry-pick easy wins. If refactor (a)
  exposes a bug that's not in scope, file it as a follow-up — don't paper
  over it.
- **Commit before agent launches.** If either refactor is delegated to a
  worktree agent, ensure the agent commits at each checkpoint and reports
  test counts each time.
- **Run only the tests that matter** during development. Full-suite runs
  belong at commit checkpoints, not between every edit.
