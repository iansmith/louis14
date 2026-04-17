# Table Layout: Blink Alignment Refactors — Continuation

This picks up from `docs/PROMPT-table-blink-alignment.md`. Read that first
for the full background and the two target refactors (a) and (b).

## Where we left off

### Baselines (at commit `c563415b`)
- `TestWPTCSS3Reftests/css-tables`: **48/6720 passed, 61 failed**
- `TestWPTReftests` (CSS2): **92/99 passed, 6 failed**
- The three motivating tests pass:
  `flexbox-align-self-baseline-horiz-001a`,
  `flexbox-align-self-baseline-horiz-001b`,
  `flexbox-align-self-horiz-001-table`

### Work already done (uncommitted WIP on `fix/flexbox-fast`)
On this branch, the following changes are in the working tree but **not
committed** — the previous session stopped mid-refactor (a) because of a
CSS2 regression it couldn't land cleanly. You have two options:

- **Option A (recommended):** `git stash` or `git checkout -- .` to drop
  the WIP, then redo refactor (a) clean using the corrected design below.
- **Option B:** keep the WIP and patch it to fix the regression
  (see "Known regression" at the bottom of refactor (a)).

The WIP touches three files:

1. **`pkg/css/cascade.go`** — added two helpers next to the existing
   `NewAnonymousTableCellStyle`:
   - `NewAnonymousTableRowStyle(parent *Style) *Style`
   - `NewAnonymousTableRowGroupStyle(parent *Style) *Style`
   Both mirror the cell helper: `NewStyle()`, copy viewport dims, set
   `display`, copy inheritable properties. Keep these — they are correct.

2. **`pkg/layout/layout_tree_builder.go`** — added:
   - `isProperTableChild`, `isProperRowGroupChild`, `isProperRowChild`
     helpers (dispatch by `style.GetDisplay()`; `isProperTableChild` also
     recognizes `<col>`/`<colgroup>` by tag name because louis14 doesn't
     map them to `DisplayTableColumn`/`ColumnGroup` today).
   - `wrapAnonymousTableBoxes(children, parentStyle)` that, based on the
     parent's display, wraps runs of stray children in a single anonymous
     row-group / row / cell (one wrapper per run, not per child), and
     recurses inside each wrapper so the lower levels are normalized too.
     Whitespace-only stray runs are discarded.
   - A call to `wrapAnonymousTableBoxes(rawChildren, style)` inside
     `buildNode` just before `maybeWrapAnonymousBlocks`. **This placement
     is wrong** — see the "Known regression" note below.

3. **`pkg/layout/table_layout.go`** — simplified:
   - `collectRowsAndCaptions`: dropped the inline-accumulation path, the
     bare-`DisplayTableRow` branch, the bare-`DisplayTableCell` branch,
     and the `default:` stray-wrap branch. Keeps caption handling,
     col/colgroup width extraction, and row-group iteration. `childIdx` /
     `blockOffset` tracking from `c563415b` stays (removed in refactor b).
   - `buildRow`: dropped the `flushAnon` stray batching. Added a
     `singleBlockInnerChild(wrapper)` helper so when the row child is an
     anonymous cell wrapper whose sole content is one block-level element,
     `buildRow` exposes the inner block as `cell.node` / `cell.style` with
     `cell.isAnonymous = true`. This preserves the pre-refactor behavior
     where a bare `<div>` inside a `<table>` contributed its margins to
     the row height via the `cell.isAnonymous` path in `Layout()` (see
     `table_layout.go:288–395`).
   - `Layout()`: dropped the two `row.node == nil` guards (rows are now
     guaranteed-backed by a real `LayoutInputNode`). The
     `row.style != nil` guard stays (defensive, but anonymous-row wrappers
     have zero borders/padding per §17.2.1 so the block is a safe no-op).

### Post-WIP test results
- `css-tables`: 50 passed, 59 failed — **net +2, no regressions**.
- Motivating three flexbox tests: still pass at 0 diff.
- CSS2: 7 failed — **+1 regression**, the one described below.

### Known regression: `generated-content/before-after-display-types-001.xht`

The failing test has markup like
`div { ... } div:before { display: table-row-group; content: ... }` —
i.e. a **pseudo-element with display:table-row-group whose ancestor is
not a table**. Current louis14 doesn't wrap standalone misplaced table
internals in an anonymous table; it falls back to block layout. The test
and its reference both rely on that legacy fallback rendering matching.

The WIP's `wrapAnonymousTableBoxes` is called from `buildNode` at every
level, which means it fires on the **children of that pseudo-element**
even though the pseudo-element has no table ancestor. That re-wraps the
pseudo's content in anon-row + anon-cell, which then gets laid out as two
nested block containers instead of as inline content — producing the
pixel diff.

**The fix is to restrict wrapping to real table subtrees only.** Don't
call wrap from `buildNode`. Instead, run it as a post-pass from
`BuildLayoutTree` after the initial bottom-up construction, visiting only
`DisplayTable` / `DisplayInlineTable` roots and letting
`wrapAnonymousTableBoxes` recurse through proper subtree descendants:

```go
func (b *LayoutTreeBuilder) BuildLayoutTree(root *html.Node) *LayoutInputNode {
    tree := b.buildNode(root)
    b.normalizeTableSubtrees(tree)
    assignDOMIndices(tree)
    return tree
}

// normalizeTableSubtrees finds every real table / inline-table in the
// tree and normalizes its subtree per CSS 2.1 §17.2.1. Standalone
// display:table-row-group / table-row / table-cell boxes (e.g. pseudos
// outside a table) are left alone — louis14 hasn't implemented the
// reverse anonymous-table-generation rule yet, and the existing
// fallback-to-block layout is what ref tests expect.
func (b *LayoutTreeBuilder) normalizeTableSubtrees(node *LayoutInputNode) {
    if node == nil { return }
    if s := node.Style(); s != nil {
        switch s.GetDisplay() {
        case css.DisplayTable, css.DisplayInlineTable:
            node.children = b.wrapAnonymousTableBoxes(node.children, s)
            return // recursion continues inside wrapAnonymousTableBoxes
        }
    }
    for _, c := range node.children {
        b.normalizeTableSubtrees(c)
    }
}
```

…and update `wrapAnonymousTableBoxes` so that **accepted (proper)
children also get their own children recursively wrapped** — the current
WIP only recurses into synthesized anonymous wrappers, so a real `<tbody>`
child of a `<table>` doesn't get its `<tr>`s' children normalized:

```go
for _, child := range children {
    if accepts(child) {
        flush()
        if cs := child.Style(); cs != nil {
            child.children = b.wrapAnonymousTableBoxes(child.children, cs)
        }
        result = append(result, child)
    } else {
        stray = append(stray, child)
    }
}
```

Then remove the `wrapAnonymousTableBoxes(rawChildren, style)` call from
`buildNode`.

After this fix, the motivating three tests should still pass, `css-tables`
should stay at ≥50 passes, and CSS2 should return to 6 failures (the
single regression in `before-after-display-types-001.xht` is eliminated
because the pseudo-element inside a block div no longer gets its content
wrapped).

## Remaining work

### 1. Finish refactor (a)

Concretely:

1. Either rebase the WIP or start fresh. If starting fresh, re-apply the
   three style helpers and the simplified `collectRowsAndCaptions` /
   `buildRow` / `Layout()` changes from the WIP (they are correct).
2. Move `wrapAnonymousTableBoxes` off `buildNode` and call it from a new
   `normalizeTableSubtrees` post-pass in `BuildLayoutTree`, as shown
   above.
3. Make `wrapAnonymousTableBoxes` recurse into accepted children too.
4. Run verification:
   ```bash
   # Motivating tests — must pass at 0 diff:
   GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
     -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
     -v -count=1

   # css-tables — must stay ≥ 48 passes (target ≥ 50):
   GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
     -run 'TestWPTCSS3Reftests/css-tables' -count=1 -timeout 600s 2>&1 | grep Summary

   # CSS2 — must stay at 6 failures:
   GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
     -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary
   ```
5. Commit as a single commit: `Generate anonymous table boxes at tree-build time`
   with a body explaining the CSS 2.1 §17.2.1 rules, the Blink analogues
   (`LayoutTable::AddChild`, `LayoutTableSection::AddChild`,
   `LayoutTableRow::AddChild`), and why the normalization runs as a
   post-pass scoped to table roots (standalone misplaced-table pseudos
   still fall back to block layout — louis14 hasn't implemented the
   reverse direction of §17.2.1 yet).

### 2. Refactor (b) — single-pass row sizing

Unchanged from the original prompt. Break into three checkpoint commits:

- **ckpt 1**: introduce `pkg/layout/table_types.go` with `Row`, `Rows`,
  `Section` types mirroring Blink's `TableTypes::Row` / `Section`, plus
  `TableConstraintSpaceData` (immutable, threaded through
  `ConstraintSpace` via a new field and section index). No behavior
  change.
- **ckpt 2**: implement `computeRows` + `distributeTableBlockSize` against
  the new types. Port the 5-priority distribution from Blink's
  `DistributeExcessBlockSizeToRows`. Guard with a
  `useSinglePassTableSizing` bool (default `false`) and verify both paths
  produce identical output on the test suite.
- **ckpt 3**: flip the default to `true`, delete the post-append
  mutation / reposition block, delete `rowInfos.childIdx` +
  `blockOffset`, move the §17.5.4 vertical-align pass inline into row
  construction (row block-size is known from
  `TableConstraintSpaceData`, so no post-pass needed).

At every checkpoint, report `css-tables` and CSS2 pass counts. Do not
touch rowspan algorithm semantics beyond what's needed to match current
behavior; flag any rowspan gaps in commit messages.

## Principles (from CLAUDE.md, still apply)

- **Study Blink first.** Read
  `third_party/blink/renderer/core/layout/table/` for the types and
  algorithm shape (see the original prompt for specific file/line
  pointers). Mirror type names and comments with source references.
- **All tests must pass at 0% diff.** Not "nearly." The CSS2 count going
  6→7 is a failure; bringing it back to 6 is the bar for landing (a).
- **Foundational correctness.** Don't paper over the pseudo-element
  standalone-table case with a special-case — if it needs to be fixed
  properly (i.e. implement the reverse §17.2.1 anonymous-table
  generation), do so as a separate follow-up commit or file a TODO; do
  not degrade (a).
- **Run only the tests that matter** during iteration. Full-suite runs
  at commit checkpoints.
