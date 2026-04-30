# P25 Cmt-3 — continuation prompt

You're continuing Phase 25 (fragmentation-aware OOF positioning, Blink-aligned port) on louis14. Cmt-1 scaffolding (`4540a8f0`) and Cmt-2 collection wiring (`3b6b0e5d`) are on disk. This is the commit that actually closes `-032`.

## Setup

- **Worktree:** `/Users/iansmith/louis14-phase-25` on `phase-25-oof-fragmentation`. Fonts already symlinked from main dir; do not re-symlink.
- **Read first:** `findings.md` § Phase 25 (canonical brief). Then skim:
  - `pkg/layout/oof_fragmentation.go` (Cmt-1 types)
  - `pkg/layout/fragment_builder.go` `PropagateOOFFragmentainerDescendants` + `Build()` `FragmentedOofData` emission (Cmt-2)
  - `pkg/layout/multicol_layout.go` `nestedDeferredOOFs` block at the top of the OOF section (Cmt-2)
- **Don't re-litigate:** the two-tier `*LayoutInputNode` collapse is settled. Map keys use `*LayoutInputNode`.
- **State of Cmt-2:** all the collection wiring is in place but **dormant**. `HasOutOfFlowFragmentainerDescendants()` never returns true yet because nothing populates `oofPositionedFragmentainerDescendants`. Cmt-3's promotion logic flips that on.

## Goal — three pieces

### 1. Promotion: regular candidate → fragmentainer descendant

Mirror `out_of_flow_layout_part.cc:589-754` (the `Run` / `LayoutOOFNodes` worklist). Specifically the conversion at `oof_positioned_node.h:266-350` where a candidate becomes a `LogicalOofNodeForFragmentation` once we know its CB is fragmented.

The conservative rule lands here, not in BLA: when the multicol's complete-path resolution is about to run AND `mla.space.HasBlockFragmentation` (the multicol is itself fragmented) AND the candidate's CB is the multicol container, divert it into `builder.AddOutOfFlowFragmentainerDescendant(...)` with:
- `Candidate` = the existing `OutOfFlowCandidate` (BreakToken initially nil; populated when the OOF resumes).
- `ContainingBlock.Fragment` = the multicol's outgoing fragment for this fragmentainer (the one currently being built).
- `ContainingBlock.Offset` = zero (multicol's content-box origin within itself).
- `FixedposContainingBlock` = inherit from the parent fragmentation root's context (Cmt-3 needs to plumb this through `MulticolWithPendingOOFs.FixedposContainingBlock`).

Once a candidate is promoted, `nestedDeferredOOFs` becomes true and the existing `multicol_layout.go` Cmt-2 branch fires: `AddMulticolWithPendingOOFs` registers the multicol; `LayoutCandidates` is skipped.

### 2. Drain pipeline: `OutOfFlowLayoutPart` extensions

Mirror `out_of_flow_layout_part.cc:589-695, 1265-1529, 2390-2630, 3143-3210`:

- `OutOfFlowLayoutPart.Run(builder)` — entry point. Calls `HandleFragmentation(builder)` first (no-op unless `IsBlockFragmentationContextRoot()`), then the existing `LayoutCandidates` loop for non-fragmented OOFs.
- `HandleFragmentation(builder)` — drains both `oofPositionedFragmentainerDescendants` (via `LayoutFragmentainerDescendants`) and `multicolsWithPendingOOFs` (via `HandleMulticolsWithPendingOOFs`). Uses `Swap*` accessors to take ownership.
- `HandleMulticolsWithPendingOOFs` + `LayoutOOFsInMulticol` — for each pending inner multicol, walks its physical fragments (the children added to the outer's builder), clones each fragmentainer into a side-builder, gathers the inner's deferred descendants, and dispatches them to `LayoutFragmentainerDescendants` against the outer's column flow.
- `LayoutFragmentainerDescendants` — per-descendant: convert stitched-coord static position → local fragmentainer offset using the CB's break-token `ConsumedBlockSize` (Phase 22 Cmt-1 wired this for the inner outer break case; verify it covers all CB types here). Then `LayoutCandidates`-equivalent against the local fragmentainer.
- `ComputeStartFragmentIndexAndRelativeOffset` — consumes `ClippedContainerBlockOffset` to clamp the start fragmentainer for OOFs inside `overflow:clip` ancestors.

### 3. Outermost fragmentation root: set the gate

Wire `builder.SetIsBlockFragmentationContextRoot()` on the **outermost** multicol's builder (the one that is NOT itself nested inside another fragmentation context). This is the only builder whose `OutOfFlowLayoutPart.Run` actually drains `HandleFragmentation`.

Detect "outermost" via `!mla.space.HasBlockFragmentation` at the multicol layout entry — the outer multicol's space does not advertise block fragmentation.

## Don't do in Cmt-3

- Cmt-4 is the re-apply of Phase 22 Cmt-B (post-loop break guard from `docs/PLAN-phase-22.md` §14.2). Don't pre-apply it.
- Don't change the existing non-nested OOF flow — `LayoutCandidates` continues to handle OOFs whose CB is not fragmented.

## Verification gate (targeted only — CLAUDE.md §4)

```bash
# 13 drivers + targets
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Cmt-3 closure target
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-032\.html$'
```

Expected: 13 drivers 13/13 · 9 prior-clip-wins 9/9 · `-033` 0% · **`-032` 0%** (this is the commit that closes it).

`-032` improving but not to 0% → the side-builder dispatch or the stitched ⇄ local conversion is off. Bisect by inspecting the diff PNG and check which fragmentainer the abspos lands in vs reference.

`-033` regression or any prior-clip-win regression → STOP, revert, investigate. Almost certainly a `SetIsBlockFragmentationContextRoot` mistake (set on the wrong builder) or a stitched-coord conversion that misuses `ConsumedBlockSize`.

Don't run broader sweeps (multicol-gate, 4-cat) until the targeted gate is green.

## After Cmt-3

If `-032` closes, expect `-011` to also close (or come close). Run the full multicol gate then to capture the ripple:

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/' 2>&1 | tail -5
```

Update `findings.md` § Phase 25 sequencing (mark Cmt-3 done, point Cmt-4 next) and `progress.md` active-queue entry. Commit those on `multicol-phase-21-24` (umbrella, in `/Users/iansmith/louis14`), not on the worktree. Then write `docs/PROMPT-phase-25-cmt-4.md`.

## Quick references

- Phase 25 brief + Blink citations: `findings.md` § Phase 25.
- Cmt-1 types: `pkg/layout/oof_fragmentation.go`.
- Cmt-2 wiring: `pkg/layout/fragment_builder.go` (accessors + `PropagateOOFFragmentainerDescendants` + `Build()` emission); `pkg/layout/block_layout.go` `PropagateOOFCandidates` tail; `pkg/layout/multicol_layout.go` `nestedDeferredOOFs` block.
- Phase 22 history (why we're here): `docs/PLAN-phase-22.md` §13–§14.
- Phase 22 Cmt-1 `ConsumedBlockSize` wiring (used by stitched-coord conversion): `pkg/layout/multicol_layout.go` `buildOuterBreakResult`.
