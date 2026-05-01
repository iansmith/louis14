# Findings & Decisions — css-multicol (active)

## Rules pointer

Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not restate them here.

## Archive pointers

- **Historical multicol detail** — `docs/findings-multicol-archive.md`. All Phase 12–20 sub-phase content, retired Phase 16/16.e+18/v1/v2 briefs, the Phase 20 BRIEF (now LANDED), pre-Phase-21 error-log entries, full ColumnLayoutAlgorithm pseudocode, key Blink data-structure tables.
- **Writing-modes work** — `docs/findings-wm.md` (closed 781/781).
- **CSS Position work** — closed 2026-04-21 at 92/105; commit refs in archive.
- **Other archives in `docs/`** — `plan-CSS2-*`, `plan-wm*`, `PROMPT-*`, etc.

## Current state (2026-05-01 — Cmt-5a + Cmt-5b landed (gate-neutral; -011 closure still blocked by Phase 21))

| Suite             | Passing | Notes |
|-------------------|---------|-------|
| CSS 2.1 reftests  | 99/99   | invariant |
| css-flexbox       | 626/629 | three pre-existing residuals (auto-margins-001, content-height-with-scrollbars, flexbox-align-self-vert-004) |
| css-position      | 92/105  | thirteen pre-existing residuals; no active work |
| css-writing-modes | 781/781 | closed |
| css-multicol (master) | 212/455 | Phase 22 Cmt-1 (`75182bd6`) closed `broken-column-rule-1`. Phase 25 PARKED 2026-05-01 at Cmt-5d (`e8f25761` on `phase-25-oof-fragmentation`); gate-neutral. Cmt-5a + 5b landed 2026-05-01 on `phase-25-cmt-5ab-retry` (gate-neutral 214/455; not yet merged to master). Cmt-5e + Phase 21 still required for `-011`/`-032`/`-033` visual closure. |
| css-multicol (phase-25-cmt-5ab-retry) | 214/455 | Cmt-5a (`1ca0904d`) + real Cmt-5b (`e10ff3a2`) on top of Cmt-5d. Drivers 14/15 (`-032` pre-existing). Prior-clip-wins 9/9. Spanner-fragmentation 12/13 (`-008` pre-existing). Under Cmt-A locally: `-010`/`-032`/`-033` PASS, `-011` 5000 px (clip-blocked, awaits Phase 21). Cmt-5e safe to apply (no `-032` poisoning). |
| 13 driver invariants | 13/13 | column-height-001/010/017/026/027, multicol-nested-030/031, spanner-fragmentation-001/004/006, multicol-rule-nested-balancing-004, nested-floated-multicol-with-monolithic-child, nested-past-fragmentation-line |
| 14 driver invariants (phase-25-cmt-5ab-retry) | 14/15 | Same set + multicol-nested-032/033 (032 is pre-existing failure at 2.1%); all others pass |
| spanner-fragmentation cluster | 12/13 | -008 fails 0.2%; pre-existing |

## Phase summary (one line per phase; full detail in archive)

- **css-position phases 0–11** — DONE 2026-04-21 (92/105). See archive.
- **Phase 12 (a–h.6)** — Multicol fragmentation infra: BlockBreakToken threading, ColumnSpannerPath, nested multicol, forced breaks, column-height/wrap, balance-break-avoidance, Ahem font, F1–F5 residual fixes. Multicol 94 → 188.
- **Phase 13 (a–h)** — LayoutUnit precision discipline. Closed 2026-04-26.
- **Phase 14a/b/c** — IFC fragmentation guard (+4); nested leaf-frag deferral (+2); clear-001 permanently deferred. Multicol 179 → 188.
- **Phase 15** — PercentageResolutionBlockSize for multicol children. multicol-span-all-children-height-001 closed.
- **Phase 16** — Spanner BFC filtering + 16.d.1 leaf clamp + 16.d.2/3 TallestUnbreakable carrier. Multicol 188 → 192. 13 drivers 11/13.
- **Phase 17** — Forced-break balance (Blink ContentRuns measure-pass). Multicol 192 → 196.
- **Phase 16.e + 18 v2 BUNDLE** — Walker port, ClipBlockAxisOnly removal, IsMonolithic flag, contentNode pointer cache. Merge `00c0d197`. Multicol 196 → 199. Then `3389efe7` broad ClipContentToBorderBox closed all 3 driver residuals: 199 → 205. 13 drivers 13/13.
- **B6 (Phase 18 ConsumedRowBlockSize WRITE-site)** — Landed `b251c8db`, gate-neutral; brief target was wrong, see archive.
- **Phase 20** — Multicol overflow clip Blink-aligned port. BoxType enum + IsMulticolContainer flag + atomic-inline + float-break-inside:avoid TallestUnbreakable propagation. Multicol 205 → 211 (+6, hits brief target). 13 drivers 13/13. Six reclaims (multicol-fill-balance-032/034/035/036, multicol-span-all-margin-nested-001, inline-block-and-column-span-all). All 9 prior-clip-wins held. Three brief diverges documented (Blink's clip is conditional not unconditional; rect is padding-box not content-box; reclaim diff numbers were fonts-artifact stale). Worktree commits P20.1–P20.7 + tracking updates.

## Active phases (21–25)

The five phases below are the planned work going forward. Each is sized to one focused effort. Phases 21 and 25 are linked: Phase 25 (fragmentation-aware OOF positioning) fixes the layout bug that blocks Phase 21's clip-condition fix. Phase 22 partially landed 2026-04-29 (gate-neutral foundations Cmt-1 + Cmt-A); the closure work it scoped was deeper than the brief assumed and was reassigned to Phase 25.

---

### Phase 21 — Conditional `IsMulticolContainer` clip (Blink-parity overflow gating)

**Goal.** Close the three Phase 20 stuck tests by gating P20.5's overflow clip on user-set `overflow != visible`, mirroring Blink's `LayoutBox::UpdateFromStyle`.

**Why this is needed.** Phase 20's `IsMulticolContainer` flag forces a padding-box clip on every multicol fragment unconditionally. Verified against Blink (`layout_box.cc` ~947):

```cpp
bool should_clip_overflow = (!StyleRef().IsOverflowVisibleAlongBothAxes() ||
                             ShouldApplyPaintContainment()) &&
                            RespectsCSSOverflow();
```

Blink only sets `HasNonVisibleOverflow()=true` when the user-set `overflow` is non-visible on at least one axis (or paint containment applies). Multicol does not force the clip on its own.

**Stuck tests this closes.**

| Test | Diff | Symptom |
|------|------|---------|
| `multicol-gap-large-001` | 0.3% | Test explicitly expects content to overflow visibly past multicol's right edge. |
| `increase-prev-sibling-height` | 80 px | Glyph ascenders extending above line-box top get clipped. |
| `multicol-nested-029` | 85 px | Same as above in nested multicol with `line-height:0.8`. |

**Hard-blocker.** All 9 prior-clip-wins (`multicol-breaking-002`, `multicol-breaking-nobackground-002`, `multicol-fill-balance-nested-000`, `multicol-list-item-001`, `multicol-nested-015/021/026/028`, `nested-after-float-clearance`) currently rely on the unconditional clip masking layout bugs that produce paint-overflow past the multicol's box. None of them set `overflow:hidden`, so removing the unconditional clip regresses them — UNLESS the underlying layout bugs are fixed first. Specifically:

- `multicol-breaking-002`: inner `height:300px` multicol nested in outer `height:100px` × 4 cols. Inner-multicol resume across outer columns is wrong (Phase 22 issue).
- `spanner-fragmentation-004/006`: walker-port residual where descendants of a partially-laid-out spanner over-paint past the multicol's box.
- `nested-after-float-clearance`: column-fill:auto + 4 narrow cols + 200h float. Distribution under-resolves; clip masks the over-paint.

**Sequencing.** Phase 25 must land first (the nested-multicol resume bug actually requires fragmentation-aware OOF positioning, not just the `ConsumedBlockSize` chain that Phase 22's foundations wired). Spanner-overflow placement is a separate item not in scope here — for now, Phase 21 may need to keep the clip on the spanner-fragmentation cluster via some narrower predicate, or accept those regressions and chase them in a follow-up.

**Implementation sketch.** In `pkg/render/paint_layer.go`, change the P20.5 site:

```go
if box.IsMulticolContainer {
    clipX = true
    clipY = true
}
```

to something like:

```go
if box.IsMulticolContainer && (clipX || clipY || s.HasPaintContainment()) {
    // multicol structurally needs the clip only when user-set overflow
    // is non-visible on at least one axis, or paint containment applies.
}
```

…where `clipX/clipY` already reflect user-set overflow above. With user-set `overflow: visible` (default), the clip is omitted; with `overflow:hidden`, the existing two-axis clip computation runs as today. The `IsMulticolContainer` flag itself stays — it documents the structural fact and can be used by future per-axis or `overflow-clip-margin` work.

**Verification gate.**
- 13 driver invariants 13/13 at 0 diff.
- 9 prior-clip-wins still pass (assuming Phase 25 + spanner-overflow placement landed).
- Three stuck tests close at 0 diff.
- Multicol gate: 211 → 214+ (+3 minimum, possibly more if other tests reclaim).
- 4-cat invariants unchanged.

**Hard exits.**
- Driver invariant regression → pause and discuss.
- Any of the 9 prior-clip-wins regress → Phase 25 / spanner-overflow placement isn't done; reorder.

**Files touched.** `pkg/render/paint_layer.go` (single site). May also want a small docstring update in `pkg/layout/multicol_layout.go` where `IsMulticolContainer` is set.

**Estimated commits.** 1, mainline (low risk after Phase 25).

---

### Phase 22 — Nested-multicol resume `ConsumedBlockSize` chain (LANDING Cmt-1 only, 2026-04-30)

**Status.** Cmt-1 (`2a822b9d`) landing as the sole Phase 22 deliverable. Cmt-A was attempted (committed as `3c17b1da` on the worktree) but rejected: full-suite verification revealed it introduces a 0.1% (400 px) regression on `multicol-nested-033` — Blink-alignment claim was weak and the regression confirms Phase 14b's defer at `FragmentainerOffset==0` was load-bearing in ways the original Cmt-A rationale didn't account for.

- **Cmt-1 (LANDING, `2a822b9d`)** — wires `ConsumedBlockSize = previously_consumed + final_block_size` on `buildOuterBreakResult`'s outgoing `BlockBreakToken`. Fixes the silent zero in the only token-emitter that omitted this field. Mirrors Blink's `FinishFragmentation` primary clause. Phase 25 will read this field directly when translating stitched-coord OOF static positions.

  Gate impact (full sweep, 2026-04-30): multicol gate **211 → 212** (+1 reclaim of `multicol-breaking/broken-column-rule-1.html`). 13 drivers 13/13. 9 prior-clip-wins 9/9. Spanner-fragmentation 12/13. 4-cat invariants (CSS2 99/99, css-flexbox 626/629, css-position 92/105, css-writing-modes 781/781) intact.

- **Cmt-A (REJECTED, `3c17b1da`)** — narrowed Phase 14b defer-gate to `FragmentainerOffset > 0`. Bisect: Cmt-1 alone passes `-033` at 0% diff; Cmt-1 + Cmt-A fails `-033` at 0.1% (400 px). The cluster-closure target (`-011`/`-032`) was not met by Cmt-A in any case — the actual fix needs fragmentation-aware OOF positioning (Phase 25). Cmt-A stays on the worktree branch as a documented exploration; Phase 25 will re-derive the right Phase 14b shape from scratch.

**Why the closures didn't land.** With Cmt-1 + Cmt-A applied (during the worktree exploration), the inner multicol enters the walker loop and could in principle emit a correct break token. But there's still a missing post-loop check: when the inner exits "all content placed" while the outer fragmentainer is exhausted and explicit block-size remains, no break is emitted. A candidate Cmt-B (post-loop guard using `mla.remainingContentBlockSize`) was tried in worktree on top of Cmt-1+Cmt-A: improved `-011` from 1.6% → 1.0% but regressed `-032` from 3.1% → 4.2%. Reverted. Cmt-A itself was later also rejected after the full-suite sweep showed the `-033` regression.

The `-032` regression comes from the OOF path: an inner positioned multicol with an abspos descendant currently positions OOF only on the complete path (`multicol_layout.go:1012-1023` calls `OutOfFlowLayoutPart.LayoutCandidates(...)` with `containingBlockSize.BlockSize = finalBlockSize`). The break path doesn't have a fragmentation-aware OOF analogue; making the inner break shifts the OOF responsibility upward to a containing block that isn't actually the OOF's CB. Closing the OOF gap requires Blink's full pattern (deferred descendants on builder, `LayoutOOFsInMulticol` at the fragmentation-context root, stitched-coord static positions, per-fragmentainer dispatcher). That is Phase 25.

**Files touched.** `pkg/layout/multicol_layout.go` (Cmt-1 only: 11 insertions, 3 deletions in `buildOuterBreakResult`).

**Worktree.** `multicol-phase-22` (kept open with Cmt-1 + Cmt-A; only Cmt-1 is being merged forward — see Phase 22 commit shape in `docs/PLAN-phase-22.md` §14.9).

**Reference.** Full Cmt-B attempt log + Blink OOF research + Phase 25 design in `docs/PLAN-phase-22.md` §14.

---

### Phase 23 — Finish FinishFragmentation port

**Goal.** Remove the `len(children) == 0` leaf-only gate in 16.d.1 per-fragment block-size clamp; delete or shrink the parent-side children-loop overflow path in `block_layout.go:1001-1196` to the cases Blink actually handles there (IFC breaks, forced breaks).

**Why it's needed.** louis14's current 16.d.1 clamp only fires on leaf fragments because the parent-side overflow path was load-bearing for non-leaf cases, and removing the leaf gate without touching the parent path produced break-token misalignment. The walker port (Phase 16.e) cleaned up the break-token model; the parent-side path is now the obvious place to retire, replaced by Blink's `FinishFragmentation` flow.

**Sequencing.** Independent of Phase 21/22; can land at any time. May benefit from Phase 22 having fixed the nested-resume chain (one less complication during testing).

**Implementation sketch.**

1. In `pkg/layout/block_layout.go`, walk the children-loop overflow handling (~lines 1001-1196). Identify cases handled there: IFC breaks, forced breaks, BlockSizeForFragmentation defer, post-spanner cleanup. For each, decide whether Blink's `FinishFragmentation` (`fragmentation_utils.cc`) covers it or whether louis14 needs a parallel path.
2. In `pkg/layout/fragmentation_utils.go`, port any missing pieces from Blink's `FinishFragmentation`. Mirror the Blink function structure: monotonicity of `ConsumedBlockSize`, fragment block-size clamping to fragmentainer remaining space, `DidBreakSelf` emission.
3. In `pkg/layout/block_layout.go`, remove the leaf-only gate (`len(children) == 0`) on the 16.d.1 clamp. Now the clamp can fire on any block.
4. Delete or shrink the parent-side overflow path so it doesn't double-clamp.

**Verification gate.**
- 13 driver invariants 13/13 at 0 diff.
- Spanner-fragmentation cluster ≥ 12/13.
- All Phase 17 tests still pass.
- 4-cat invariants unchanged.
- Multicol gate: stable or +N (this is primarily a structural cleanup; gate movement is incidental).

**Hard exits.**
- Driver regression → pause and discuss; the parent-side path was load-bearing in some way the Blink port missed.
- Spanner cluster regression → same.

**Files touched.** `pkg/layout/block_layout.go`, `pkg/layout/fragmentation_utils.go`, possibly `pkg/layout/break_token.go`.

**Worktree.** Yes (structural refactor).

**Estimated commits.** 4–8.

---

### Phase 24 — span-all-children-height cluster (002–013)

**Goal.** Close the 12 `multicol-span-all-children-height-002` through `-013` tests across their 7 sub-clusters. Brief lives in the archive (Phase 19 brief, written 2026-04-26 from end-to-end Blink research).

**Why it's queued last.** This is a feature-completion track for percentage-height descendants of multicol containers. The seven sub-clusters share a common theme — `containerPercentResolutionBlockSize` propagation — but the specific failure modes differ enough that each sub-cluster needs its own analysis. Phase 15 closed test `-001`; Phase 24 picks up where 15 stopped.

**Sub-clusters (from archive Phase 19 brief).** Each sub-cluster groups tests by failure shape:

1. Tests with floats inside spanners (height-002).
2. Tests with abspos descendants of spanners (height-003, -004a, -004b).
3. Tests with column-fill-auto + max-height (height-005, -006).
4. Tests with explicit column-height + spanner combinations (height-007, -008).
5. Tests with nested multicol + spanner (height-009, -010).
6. Tests with spanner inside flex container (height-011, -012).
7. Test with spanner-baseline edge case (height-013).

**Implementation approach.** Run each sub-cluster, capture diff PNGs, confirm or refine the brief's classification. For each sub-cluster, port the corresponding Blink mechanism. Worktree per sub-cluster (or per-sub-cluster commits within one worktree).

**Verification gate.**
- 13 driver invariants 13/13 at 0 diff.
- Per sub-cluster: target tests close; sweep the multicol gate for unintended movement.
- 4-cat invariants unchanged.
- Multicol gate: 211 → 220+ (depends on overlap with Phase 22 closures).

**Hard exits.**
- Driver regression → pause.

**Files touched.** Likely `pkg/layout/multicol_layout.go` (constraint-space construction for spanners + columns), `pkg/layout/block_layout.go` (`childPercResolutionBlockSize`), `pkg/layout/inline_layout.go` for percentage-height edge cases.

**Worktree.** Yes (multi-cluster feature work).

**Estimated commits.** 8–12 (one per sub-cluster + connective tissue).

---

### Phase 25 — Fragmentation-aware OOF positioning (Blink-aligned port) — Cmt-5a + 5b landed; Cmt-5e DEFERRED (waits on Phase 21 for visual closure)

**Goal.** Port Blink's nested-multicol OOF pipeline. Closes `multicol-nested-011, -032`, OOF portion of `fill-balance-026`. Unblocks Phase 21.

**Why.** Phase 22's Cmt-1 fixed the `ConsumedBlockSize` chain but isn't enough. Inner positioned multicol with abspos descendants must defer OOF layout to the outer fragmentation root (Blink pattern). louis14's `OutOfFlowLayoutPart` is currently one-shot; `BlockBreakToken` has no OOF slot; `PhysicalFragment` has no `FragmentedOofData`. The Phase 22 Cmt-B post-loop break guard regressed `-032` because it bypassed the complete-path OOF positioning at `multicol_layout.go:1012-1023` without a fragmentation-aware substitute.

**Blink reference (verified 2026-04-30 via direct source-fetch).** All paths in `third_party/blink/renderer/core/layout/`.
- `column_layout_algorithm.cc:391-414` — inner multicol defers via `container_builder_.AddMulticolWithPendingOOFs(Node())` when `InvolvedInBlockFragmentation` and has pending OOFs. Does NOT run OOF layout itself.
- `out_of_flow_layout_part.cc:589-695` — `Run()` calls `HandleFragmentation` first; latter is no-op unless at fragmentation-context root.
- `out_of_flow_layout_part.cc:1265-1529` — `HandleMulticolsWithPendingOOFs` + `LayoutOOFsInMulticol` walk the inner multicol's physical fragments, clone fragmentainers into a side builder, gather OOFs, run `LayoutFragmentainerDescendants` against the outer's flow.
- `out_of_flow_layout_part.cc:2416-2596` — stitched ⇄ local conversion via CB break-token's `ConsumedBlockSize`.
- `out_of_flow_layout_part.cc:3143-3210` — `ComputeStartFragmentIndexAndRelativeOffset` (consumes `ClippedContainerBlockOffset`).
- `oof_positioned_node.h:30-86, 171-235, 266-350, 366-408` — `OofContainingBlock`, `OofPositionedNode` (base), `LogicalOofNodeForFragmentation`, `FragmentedOofData`.
- `block_break_token.h:124-137, 212-237, 259-268` — `oof_start_offset_` (single `LogicalOffset` field); `MutableForOofFragmentation` mutator.
- `fragment_builder.h:254-423` — `Add*`, `Propagate*`, `Swap*`, `Has*`, `IsBlockFragmentationContextRoot` (parent of `BoxFragmentBuilder`; in louis14 these go on `BoxFragmentBuilder` directly).

**Architecture decisions (locked).**
1. **Two-tier collapse.** louis14's `*LayoutInputNode` plays both Blink's `LayoutInputNode` (input cursor) and `LayoutBox` (persistent layout-tree object) roles — see `pkg/layout/layout_input_node.go` (commit `043410b6` on `phase-25-oof-fragmentation`). Map keys use `*LayoutInputNode` everywhere; do NOT introduce a `LayoutBox` type.
2. **Logical, not Physical.** Carry OOF data in logical coordinates throughout (Blink converts logical→physical at fragment-finalization time). Acceptable for HTB-only; revisit if vertical writing modes need to traverse fragmentation contexts.

**Verified must-haves for the type design** (from rigorous Blink source-fetch verification, supersedes the first agent's high-level summary):
- `LogicalOofContainingBlock`: include `ClippedContainerBlockOffset` (optional `LayoutUnit`) — consumed by `ComputeStartFragmentIndexAndRelativeOffset` for OOFs inside `overflow:clip` ancestors.
- `MulticolWithPendingOOFs.FixedposContainingBlock`: full `LogicalOofContainingBlock`, not bare offset (fragment pointer + relative offset + clipped offset + spanner flag are all consulted).
- `LogicalOofInlineContainer`: value type, not pointer (zero == absent).
- `OutOfFlowCandidate`: add `*BlockBreakToken` (Blink's `OofPositionedNode::break_token_` — used for `OofBlockStartOffset` on resume) and `RequiresContentBeforeBreaking bool`.
- `AddMulticolWithPendingOOFs`: idempotent (first-write-wins, per `fragment_builder.cc:651`).

**Sequencing.**
1. **Cmt-1 (scaffolding) — DONE 2026-04-30 (`4540a8f0`).** New file `pkg/layout/oof_fragmentation.go` with the verified types; field additions on `BoxFragmentBuilder`, `BlockBreakToken` (`OofStartOffset LogicalOffset`), `PhysicalFragment` (`FragmentedOofData *FragmentedOofData`), `OutOfFlowCandidate` (`BreakToken *BlockBreakToken`, `RequiresContentBeforeBreaking bool`). Build clean; gate-neutral (drivers 13/13, prior-clip-wins 9/9, `-033` 0%, `-032` 2.1% baseline).
2. **Cmt-2 (collection wiring) — DONE 2026-04-30 (`3b6b0e5d`).** `BoxFragmentBuilder` accessors (`AddOutOfFlowFragmentainerDescendant`, `AddMulticolWithPendingOOFs` idempotent + lazy-init, `Has*`/`Swap*`, `IsBlockFragmentationContextRoot` getter+setter). `Build()` emits `FragmentedOofData` on the outgoing fragment when the deferred lists are non-empty. `PropagateOOFFragmentainerDescendants` on builder; BLA's `PropagateOOFCandidates` walks `childFragment.FragmentedOofData` and forwards descendants + pending-multicol entries upward. Inner multicol's `Layout()` registers itself in the pending-multicols map and skips the complete-path `LayoutCandidates` when nested + has deferred descendants. Behaviorally inert: `HasOutOfFlowFragmentainerDescendants()` never fires until Cmt-3 lands the OOF-promotion logic that populates the list. Verification: 13 drivers 13/13, `-033` 0%, `-032` 2.1% (unchanged), 9 prior-clip-wins 9/9.
3. **Cmt-3 (OOF layout pipeline) — DONE 2026-04-30 (`8a9226ef` Cmt-3a + `7ed644d7` Cmt-3b).**
   - Cmt-3a: promotion at OOF resolution sites (BLA + multicol) when `bla.space.HasBlockFragmentation`. Static-position preserved CB-relative; CB fragment pointer left nil for parent's propagator. Multicol forwards `FragmentedOofData` from each per-column BLA result. Fix `PropagateOOFFragmentainerDescendants` coordinate semantics (StaticPosition CB-relative; CB.Offset accumulates; CB.Fragment falls back to childFragment when nil — Blink-aligned per `fragment_builder.cc:945-986`). Outermost multicol calls `SetIsBlockFragmentationContextRoot()`. NOTE: deviates from `PROMPT-phase-25-cmt-3.md` "not in BLA" — verified against Blink that promotion fires at any CB fragmented in block flow, not just multicol containers (out_of_flow_layout_part.cc:1158-1170).
   - Cmt-3b: drain pipeline in new file `pkg/layout/oof_fragmentation_drain.go`. `MulticolLayoutAlgorithm.HandleOofFragmentation` invoked just before outer's `Build()`. Walks `builder.children` to enumerate inner multicol column fragmentainers; resolves OOF dimensions via explicit width/height; slices block extent across fragmentainers; emits synthesized `PhysicalFragment` pieces at outer-content-box positions. Mirrors Blink's `HandleFragmentation` / `HandleMulticolsWithPendingOOFs` / `LayoutOOFsInMulticol` / `LayoutFragmentainerDescendants` / `ComputeStartFragmentIndexAndRelativeOffset` (out_of_flow_layout_part.cc:589-695, 1265-1529, 1531-1758, 3143-3210). Also fixes BLA's child-result handler (block_layout.go lines 845-849, 1002-1006) to fire `inheritPropagatedOOF` when child carries `FragmentedOofData` (was gated only on regular candidates — broke at first promotion site without this).
   - Verification: 13 drivers 13/13 · 9 prior-clip-wins 9/9 · `-033` 0% · `-032` 2.1% (unchanged baseline). `-032` does NOT close in Cmt-3 alone — Phase 14b's deferral (`multicol_layout.go:424-443`) still pushes the inner multicol entirely to outer col 1 instead of fragmenting across both. Cmt-4 unblocks this.
   - **Deferred to follow-up commits** (not in Cmt-3): side-builder fragment-rebuild path (Blink `LayoutOOFsInMulticol` rebuilds the inner mc's outgoing fragments with extra OOF children attached to its column boxes; louis14 currently emits as outer-builder siblings, paint-equivalent for simple cases). IMCB / inset-based sizing for promoted descendants. Vertical writing modes for the drain offset arithmetic.
4. **Cmt-4 (re-apply Cmt-B) — DONE 2026-04-30 (`e84d3ece`).** Post-loop break guard from `docs/PLAN-phase-22.md` §14.3 placed in `multicol_layout.go` after the OOF promotion + pending-multicol registration so the outgoing break fragment carries `FragmentedOofData` for the outer drain. Verification: drivers 14/15 (no regression vs Cmt-3 baseline), prior-clip-wins 9/9, `-033` 0/0 px, `-011`/`-032` both 2.1% (unchanged baseline). **Closure NOT achieved** — Phase 14b's defer (`multicol_layout.go:423-442`) fires for both targets (column-fill:auto, outerAvailable < explicitBlockSize) and returns a 0-height fragment BEFORE the walker loop runs, so Cmt-B's post-loop guard never gets the chance to fire. Diagnostic: with Phase 14b narrowed to `FragmentainerOffset > 0` (Cmt-A re-applied), `-011`/`-032` close to 1.0% (5,000 px each, matching original §14.1 measurement) but `-033` regresses 200 px (down from §14.1's 400 px — Cmt-3 reduced the regression magnitude but didn't eliminate it). Cmt-A reverted; Cmt-5 needed.
5. **Cmt-5 (four-step closure plan) — IN PROGRESS.** Continuation prompt: `docs/PROMPT-phase-25-cmt-5.md` (rewritten 2026-04-30 from a four-agent Blink + local-code investigation). The original "narrow Phase 14b" Cmt-5 plan was wrong — narrowing alone leaves four distinct upstream bugs unfixed. Sequence:
   - **Cmt-5c — DONE 2026-04-30 (`7dc344b4`).** Inner per-column loop in `multicol_layout.go:1355` was breaking early at `colBreakToken == nil` when in-flow content fit in fewer than `numCols` columns. For nested multicols whose row had deferred OOF descendants, this dropped the empty trailing inner-column fragmentainers — Cmt-3's drain needs them as slicing targets. Added a `rowHasDeferredOOFs` sticky flag (set when any per-column result carries `FragmentedOofData`); when in fragmentation context, the loop continues emitting empty trailing columns. Mirrors Blink's flat `limited_multicol_container_builder.Children()` enumeration (oof_part.cc:1320-1373). Validation: gate-neutral on Cmt-4 HEAD (Phase 14b firing); under Cmt-A locally `-032` closes from 5000 px to 0/0.
   - **Cmt-5d — DONE 2026-04-30 (`e8f25761`).** `GetColumnGapMulticol()` did not handle bare percentage values (only resolved via calc/min/max). `column-gap:30%` on a 50px-wide multicol fell through to `1em` default (16px) instead of resolving to 15px. Added `GetColumnGapMulticolWithBase(percentBase float64)` that resolves bare percentages against the supplied base; `multicol_layout.go:273` now passes `contentInlineSize`. Validation: gate-neutral on Cmt-4 HEAD; under Cmt-A locally `-033` closes from 200 px to 0/0; full multicol gate 212/455 unchanged.
   - **Cmt-5a (`contain:size` IsMonolithic deviation) — DONE 2026-05-01 (`1ca0904d` on `phase-25-cmt-5ab-retry`).** First attempt by a Sonnet sub-agent was ATTEMPTED + REVERTED (regressed 212→177/455) because it landed without Cmt-5b. This second attempt landed WITH Cmt-5b (see below). Narrowed from unconditional to float-only: `contain:size` non-float blocks with explicit height now fragment normally (Blink-correct per `fragmentation_utils.cc::SetupFragmentBuilderForFragmentation`). `contain:size` floats retain `IsMonolithic=true` for balance-pass correctness (multicol-fill-balance-034/035/036). Gate: 214/455 post-5a+5b (up from 212 baseline). Cite: `block_layout.go:94-97`.
   - **Cmt-5b (BlockBreakToken child-chaining) — DONE 2026-05-01 (`e10ff3a2` on `phase-25-cmt-5ab-retry`).** First Sonnet sub-agent's Cmt-5b (`57bb8967`) was reverted (`dcd6f40d`) — it set `HasSeenAllChildren=true` unconditionally when `outBuilder.IsEmpty()`, which regressed `-032` under Cmt-A from PASS to 5000 px (OOF resume short-circuited). Real Cmt-5b adds the same flag but **gated on `!builder.HasOutOfFlowFragmentainerDescendants()`**: the flag fires only when there are no deferred OOF descendants needing resume. The fix addresses the post-loop-guard case where inner col-N+1 cleanly absorbs the descendant's continuation (e.g., contain-size's bottom 50px in `-011`'s inner col-2), leaving inner-mc's `outBuilder` empty even though declared block-size is not yet satisfied. Without the flag set, `NewMulticolPartWalker` on resume re-runs in-flow children from scratch. Cite: `block_break_token.h:120-160` (Blink's `has_seen_all_children_` field). Note: this commit fixes chain semantics but does NOT close `-011` to 0/0 — that still requires Cmt-5e + Phase 21 (P20.5 clip is currently truncating contain-size's overflow, which is what `-011` actually depends on for full-wrapper green coverage). Under Cmt-A locally: `-032`/`-033`/`-010` PASS, `-011` stays 5000 px (clip-blocked, NOT regressed). Bisect+investigation log: `docs/PROMPT-phase-25-cmt-5b-continuation.md`, `docs/cmt5b-trace-stash.patch`.
   - **Cmt-5e (Phase 14b narrowing — the actual Cmt-A) — DEFERRED.** ONLY after 5a+5b merge to master and Cmt-5e is re-validated. With the upstream bugs fixed, narrowing Phase 14b to `FragmentainerOffset > 0` should close `-011`/`-032`/`-033` to 0/0 while preserving `-010`. Phase 14b stays as a louis14-only short-circuit; full removal would require porting Blink's tallest_unbreakable + minimum_column_block_size retry mechanisms — both ALREADY exist in louis14 (`fragment_builder.go:301-336`, `multicol_layout.go:1417-1462`), but they don't fire on these tests because the gap isn't monolithic-overflow shaped.

**Status (2026-05-01).** Cmt-5a (`1ca0904d`) + real Cmt-5b (`e10ff3a2`) both landed on `phase-25-cmt-5ab-retry`. Gate 212→214/455 (gate-neutral vs Cmt-5a alone — Cmt-5b adds chain semantics but no new closures). Drivers 14/15, prior-clip-wins 9/9, spanner-frag 12/13, 4-cat invariants intact. Not yet merged to master. Worktree `/Users/iansmith/louis14-p25-cmt5ab` HEAD at `e10ff3a2`. **Cmt-5e (Cmt-A permanent) is now safe to apply** — Cmt-5b's OOF gate prevents the `-032` poisoning that the previous attempt would have caused. **`-011` visual closure still requires Phase 21** to gate the unconditional multicol clip; the layout/chain bugs are now fixed but the contain-size's overflow is currently truncated at outer-mc's padding box.

**Verification gate (for Cmt-5e).** 14/15 drivers (032 pre-existing) · 9 prior-clip-wins 9/9 · `-011`/`-032`/`-033` close under Cmt-A · spanner-fragmentation ≥12/13 · 4-cat invariants intact.

**Worktree.** `phase-25-cmt-5ab-retry` from `e8f25761` (Cmt-5d). At `/Users/iansmith/louis14-p25-cmt5ab`. Currently at `e10ff3a2` (real Cmt-5b on top of revert-of-agent's-Cmt-5b on top of Cmt-5a on top of Cmt-5d). The reverted agent attempt `57bb8967` is preserved in git history for reference.

**Estimated commits remaining.** 1–2 (Cmt-5e + possibly Cmt-6 residuals).

---

## Open residuals (deferred / out-of-scope)

| Item | Status | Notes |
|------|--------|-------|
| `clear-001.xht` (96px) | PERMANENTLY DEFERRED | CoreText font metrics mismatch; no targeted fix exists. |
| `column-wrap:nowrap` overflow columns | DEFERRED | Needs paint-layer change to allow ink-overflow past border-box. |
| `MulticolBreakTokenData` row-carry (Site 5) | DEFERRED | `consumedRowBlockSize=0` is a safe default; B6 covers WRITE-site for the small fraction of tests that need it. |
| F1c paint-side shape sharing | DEFERRED | `ShapeResult::CopyRange`; needed for cross-span kerning on non-level-0 items. |
| `openFont` signature cleanup | DEFERRED | `FontPathToFamilyVariant` + `resolveFamily` path fallback still present. |
| G-ABS-IN-TABLE (8 tests) | DEFERRED | abspos children of positioned thead/tbody/tfoot/tr. |
| G-SEMI-REPLACED (3 tests) | DEFERRED | abspos stretch on button/input/other. |
| G-SCROLL (1 test) | DEFERRED | `Element.scrollTop` JS setter + overflow:hidden scroll paint. |
| spanner-fragmentation-005 | DEFERRED | Pre-existing residual since Phase 12b. |
| spanner-fragmentation-008 | DEFERRED | 0.2% diff outside the 13-driver subset. |
| css-flexbox 11a/11b/11c | DEFERRED | Three pre-existing failures at 626/629. |
| `column-height-024` class | DEFERRED | Needs live-Blink build trace. |
| Anchor positioning | OUT OF SCOPE | No WPT tests exercise it. |
| StickyPositionScrollingConstraints | DEFERRED | Scroll-time wiring deferred until scroll tests appear. |
| HTML email rendering investigation | TRACKED (not top priority) | Real marketing/transactional HTML emails render badly even ignoring image fetch. Likely-impact areas: tables (deeply nested + colspan/rowspan), `<img>` sizing + `data:`/`cid:` sources, `background-image`, inline-CSS edge cases, quirks-mode HTML parsing, font loading. Multicol is NOT email-relevant. Triage gate: get a sample failing email + diagnose visually before scoping. Park decision for Phase 25: land Cmt-5a/b if the agent succeeds, defer Cmt-5e + Phase 21 indefinitely. |
| `drawColumnRules` content-area `math.Round` | DEFERRED | Not migrated to `SnapSizeToPixel`. |

## Key data structures (reference)

| Name | Role |
|------|------|
| `ColumnLayoutAlgorithm` (Blink) / `MulticolLayoutAlgorithm` (louis14) | Multicol algorithm; produces column + spanner fragments. |
| `BlockLayoutAlgorithm` with `BoxTypeColumn` | Per-column layout; reports shortage, forced-break, spanner-path, break-appeal. |
| `BlockBreakToken` | Continuation handle: `ConsumedBlockSize`, `IsBreakBefore`, `IsForcedBreak`, child tokens. |
| `ConstraintSpace` multicol flags | `BlockFragmentationType`, `HasKnownFragmentainerBlockSize`, `IsInitialColumnBalancingPass`, `IsInsideBalancedColumns`, `IsInColumnBfc`, `MinBreakAppeal`, `FragmentainerOffset`, `FragmentainerBlockSize`. |
| `ColumnSpannerPath` | Linked list multicol-container → innermost spanner. |
| `MulticolBreakTokenData.ConsumedRowBlockSize` | Row-carry across outer fragmentainers (B6, currently no-op for column-fill:auto). |
| `GapGeometry` + `MainGap`/`CrossGap` | Gap decoration geometry for painting. |
| `BreakAppeal` | `LastResort < ViolatingOrphansAndWidows < ViolatingBreakAvoid < Perfect`. |
| `PhysicalFragment.BoxType` (P20.1) | `BoxTypeNormal`, `BoxTypeColumn`. Paint-time fragmentainer detection. |
| `PhysicalFragment.IsMulticolContainer` (P20.5) | Marks the multicol fragment so paint can install the structural overflow clip. |
| `PhysicalFragment.IsMonolithic` | Set by `contain:size`, spanners with content > declared height. Consumed by `ShouldAvoidBreakInside`. |

Full Blink pseudocode for `ColumnLayoutAlgorithm::Layout` lives in the archive (`docs/findings-multicol-archive.md`).

---

## Error log

Format: date · symptom · root cause · fix or status. Recent entries only; 2026-04-28 and earlier entries are in the archive.

(No new entries since Phase 20 LANDED; full Phase 20 LANDED notes — including the four brief-diverges that came out of P20 — are in the archive.)
