# P25 Cmt-2 — continuation prompt

You're continuing Phase 25 (fragmentation-aware OOF positioning, Blink-aligned port) on louis14. Cmt-1 (type scaffolding) is on disk at `4540a8f0`.

## Setup

- **Worktree:** `/Users/iansmith/louis14-phase-25` on `phase-25-oof-fragmentation`. Fonts already symlinked from main dir; do not re-symlink.
- **Read first:** `findings.md` § Phase 25 (canonical brief — verified Blink refs, sequencing, architecture decisions). Then skim `pkg/layout/oof_fragmentation.go` for the Cmt-1 types you'll be wiring.
- **Don't re-litigate:** the deliberate two-tier `*LayoutInputNode` collapse vs Blink's three-tier model is settled. See `pkg/layout/layout_input_node.go` and commit `043410b6`. Map keys use `*LayoutInputNode` everywhere.

## Goal — three pieces

### 1. `BoxFragmentBuilder` accessors

Mirror `fragment_builder.h:254-423` (the OOF-fragmentation methods live on Blink's `FragmentBuilder` parent, but `BoxFragmentBuilder` is fine for louis14).

- `AddOutOfFlowFragmentainerDescendant(d LogicalOofNodeForFragmentation)` — append to `oofPositionedFragmentainerDescendants`.
- `AddMulticolWithPendingOOFs(node *LayoutInputNode, info *MulticolWithPendingOOFs)` — insert into `multicolsWithPendingOOFs`. **Idempotent: first-write-wins.** Per `fragment_builder.cc:651-652` Blink early-returns on duplicate-key insert. Lazy-init the map on first call.
- `HasOutOfFlowFragmentainerDescendants() bool`, `HasMulticolsWithPendingOOFs() bool`.
- `SwapOutOfFlowFragmentainerDescendants(out *[]LogicalOofNodeForFragmentation)` and `SwapMulticolsWithPendingOOFs(out *map[*LayoutInputNode]*MulticolWithPendingOOFs)` — by-out-pointer swap (caller's container ends up holding what the builder had; builder's becomes empty).
- `IsBlockFragmentationContextRoot() bool` getter; `SetIsBlockFragmentationContextRoot()` setter.

### 2. Inner multicol defer in `multicol_layout.go`

Mirror `column_layout_algorithm.cc:391-414`:

```cpp
if (InvolvedInBlockFragmentation(container_builder_)) {
  FinishFragmentation(&container_builder_);
  if (container_builder_.HasOutOfFlowFragmentainerDescendants()) {
    container_builder_.AddMulticolWithPendingOOFs(Node());
  }
}
```

In louis14: when the inner multicol's `Layout()` is finishing AND `mla.space.HasBlockFragmentation` is true (we're nested) AND the builder has fragmentainer descendants, call `builder.AddMulticolWithPendingOOFs(mla.node, &MulticolWithPendingOOFs{MulticolOffset: ...})` and **skip** the existing complete-path call to `OutOfFlowLayoutPart.LayoutCandidates(...)` at `multicol_layout.go:1012-1023`.

The OOFs themselves stay on the multicol's outgoing fragment via `FragmentedOofData.OofPositionedFragmentainerDescendants`. Blink does NOT move them into the entry — the entry just says "remember to revisit this multicol later" and carries fixedpos positioning context.

### 3. `PropagateOOFFragmentainerDescendants` on builder + BLA call sites

Mirror `fragment_builder.h:373-380` + `fragment_builder.cc:589-603`:

```go
func (b *BoxFragmentBuilder) PropagateOOFFragmentainerDescendants(
    childFragment *PhysicalFragment,
    offset LogicalOffset,
    relativeOffset LogicalOffset,
    cbAdjustment layoutunit.LayoutUnit,
    containingBlock *LogicalOofContainingBlock,
    fixedposCB *LogicalOofContainingBlock,
)
```

Walk `childFragment.FragmentedOofData.OofPositionedFragmentainerDescendants` (if non-nil), bake in the offsets, append to `b.oofPositionedFragmentainerDescendants`.

Hook into the existing `inheritPropagatedOOF` flow at `block_layout.go:1940`. For Cmt-2 keep this **conservative**: only treat a candidate as "fragmented-CB" if its CB is the multicol container itself (the test cases that motivate Phase 25 — `multicol-nested-032` etc.). Other candidates stay in the existing `outOfFlowCandidates` slice. Generalise in a later commit.

## Don't do in Cmt-2

- Cmt-3 belongs to the drain pipeline: `OutOfFlowLayoutPart.Run` + `HandleFragmentation` + `HandleMulticolsWithPendingOOFs` + `LayoutOOFsInMulticol` + `LayoutFragmentainerDescendants`.
- Cmt-4 is the re-apply of Phase 22 Cmt-B (post-loop break guard from `docs/PLAN-phase-22.md` §14.2).
- Don't change the existing non-nested OOF finalize — Blink only defers when `InvolvedInBlockFragmentation`.

## Verification gate (targeted only — CLAUDE.md §4)

```bash
# 13 drivers + targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'
```

Expected: 13 drivers 13/13 · 9 prior-clip-wins 9/9 · `-033` 0% · `-032` 2.1% (unchanged — Cmt-2 must be **behaviorally inert**; the drain pipeline is Cmt-3, so wiring alone can't close `-032`).

If `-032` improves OR worsens after Cmt-2, the wiring is wrong (it should not change behavior on its own).

Driver regression → STOP, revert, investigate.

Don't run broader sweeps (multicol-gate, 4-cat). Targeted only per CLAUDE.md §4.

## After Cmt-2

Update `findings.md` § Phase 25 sequencing (mark Cmt-2 done, point Cmt-3 next) and `progress.md` active-queue entry. Commit those on `multicol-phase-21-24` (umbrella, in `/Users/iansmith/louis14`), not on the worktree. Then write `docs/PROMPT-phase-25-cmt-3.md`.

## Quick references

- Phase 25 brief + Blink citations: `findings.md` § Phase 25.
- Two-tier architecture: `pkg/layout/layout_input_node.go` + commit `043410b6`.
- Phase 22 history (why we're here): `docs/PLAN-phase-22.md` §13–§14.
- Cmt-1 types: `pkg/layout/oof_fragmentation.go` (worktree).
