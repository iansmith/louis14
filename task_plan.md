# Task Plan: css-multicol (active) → fragmentation fixes

## Current focus (2026-04-28 — B6 LANDED parity-correct but gate-neutral; gate 205/455, 13/13 drivers)

**B6 (Phase 18 `ConsumedRowBlockSize` carrier WRITE site) DONE 2026-04-28 (`b251c8db`).** Mirrors Blink cla.cc:822-833. 13 drivers held at 13/13; multicol gate unchanged (205/455). The brief's "+12 to +15 tests" expectation was mismatched: target tests (`multicol-nested-011..032`, `multicol-fill-balance-003/-026`) all use `column-fill:auto` + default `column-wrap:auto` + auto `column-height`, so `shouldWrapColumns()` is false and the carrier WRITE-site guard never fires. Those tests need a different fix — likely the standard `ConsumedBlockSize` chain on the inner multicol's outgoing BlockBreakToken. Tracked as a new follow-up below.

**Phase 16.e+18 v2 + multicol border-box clip residual fix DONE 2026-04-28.** v2 bundle merged via `--no-ff` from `phase-16e-18-walker-carrier`; worktree removed, branch deleted. Plus follow-up commit `3389efe7` closes all 3 v2 residuals.

Net effect: **multicol gate 196 → 205/455 (+9)**, **13 drivers 11/13 → 13/13 (all passing)**, 4-category invariants unchanged.

What landed (architectural, from v2 bundle):
- Walker port (`MulticolPartWalker` mirrors Blink cla.cc:41-223) — flat document-order break-token encoding by Node identity. Replaces 3-slot positional encoding.
- `ClipBlockAxisOnly` workaround removed (Phase 12h F2 partial retired).
- `TallestUnbreakableBlockSize` carrier (Phase 16.d.2/3 in v2 brief; mirrors Blink fragmentation_utils.cc + box_fragment_builder.cc + cla.cc:1879-1948 + 1706-1712).
- `PhysicalFragment.IsMonolithic` flag (sources: `contain:size` + spanners with content > declared height).
- `contentNode` pointer cache (fixes a latent walker-dispatch bug).

What landed (residual fix, commit `3389efe7`):
- Multicol container's children clip to border-box when `finalBlockSize > 0`. Reuses `ClipContentToBorderBox` flag.
- All 3 driver residuals closed (the earlier float-margin / spanner-placement diagnoses were wrong — all 3 were paint-overflow-past-multicol).
- Trade-off: 6 minor regressions (operator-approved); net +6 multicol gate (199 → 205).

**Note:** `column-height-008` hangs at clean baseline (10m timeout). Pre-existing issue, not caused by v2.

**Just completed:** Phase 16.d.1 + spanner-frag-006 fix (commits `a6446061` + `c40b4b56` + docs `50de102c` + `68b74171` + `65ae87df`). All 13 driver tests PASS at 0 diff. Multicol gate 167 → 192 (+25), spanner-fragmentation 7 → 11 (+4).

**Re-sequencing rationale (2026-04-27).** The original CONTINUE-16e.md plan to do the spanner walker port immediately was reviewed and re-sequenced. Key reasons:

1. **The failed minimal-port attempt** (findings.md § "Attempted 2026-04-27 (minimal-port path) — reverted") showed the 6 entangled sites are more interlocked than text-reading reveals — a 6-site coordinated rewrite carries real risk of finding a 7th and 8th invariant the same way.
2. **Phase 17 is well-scoped, low-risk, and ~5 tests.** Single function rewrite (`resolveColumnAutoBlockSize` at `multicol_layout.go:1382-1455`), Blink algorithm fully detailed in findings.md § Phase 17. Tractable in a focused session on mainline.
3. **Phase 18 explicitly needs the same `MulticolBreakTokenData` carrier** that the 16.e walker port should add. Doing 16.e now WITHOUT the carrier means re-touching the same 6 sites again when 18 lands — strictly more work than bundling them.
4. **The "acceptable temporary regressions" policy in the original plan** committed broken intermediate states to mainline (steps 3-4 of the implementation outline). High-risk multi-commit refactors belong in a worktree, not on `fix/flexbox-fast`.
5. **Marginal multicol-gate gain from 16.e+16.c.2 alone is small** (+4 tests, 192 → 196) vs Phase 17 (+5 tests, 192 → 197+). Phase 18 (~15 tests) is the bigger gate-mover and shares carrier work with 16.e.

**Just completed:** Phase 17 — Forced-break balance. Mainline. ContentRuns measure-pass loop + fragment extension (Change 3, scoped to `IsInsideBalancedColumns && hasAutoColumnHeight`) + forced-break-during-measure propagation (Change 2) + HasForcedBreak overflow-path fix (Change 4). multicol 192 → 196 (+4). Primary target `multicol-fill-balance-041` PASS. Gate: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol **196/455** · spanner-frag 11/13.

**Then queued (in order):**

- **B6 — Phase 18 `MulticolBreakTokenData.ConsumedRowBlockSize` carrier WRITE site** — **DONE (`b251c8db`, gate-neutral).** Mirrors Blink cla.cc:822-833 verbatim. WRITE-site guard `shouldWrapColumns && hasRowHeight && isFirstRow && hasOuterFrag && rowHeight > outerAvailable - rowStart` never fires for the brief's named target tests because they all use `column-fill:auto` with default `column-wrap:auto` and auto `column-height`. Code-correct scaffold for any future column-wrap:wrap test; expected gate move did not materialise.
- **Investigate `ConsumedBlockSize` chain for multicol-nested-011..032 + multicol-fill-balance-003/-026** (NEW follow-up). The brief's named B6 target tests need a different fix from the row-phase carrier. Likely the standard `ConsumedBlockSize` on the inner multicol's outgoing BlockBreakToken — when an inner multicol breaks across an outer column boundary, the resume in the next outer column needs to start at the right offset. Read `multicol-nested-011` PNG diff to characterise the failure pattern, then trace the inner multicol's resume path (incoming BlockBreakToken → MLA.Layout → blockCursor seeding).
- ~~**B7 — drop `IsInsideColumnSpanner` clamp gate**~~ — **ATTEMPTED + REVERTED 2026-04-28** (no commit). Regressed `spanner-fragmentation-005` (NEW fail) + `-006` (NEW fail at 0.1%). Drivers 13/13 → 12/13; gate 205 → 203. The 16.d.1 guard is still load-bearing — the walker port (Phase 16.e) didn't change the spanner resume mechanism's break-token expectations, so removing the clamp guard exposes the original failure mode (spanner descendants self-fragment, producing break-token chains the resume mechanism doesn't consume). See findings.md error-log for diagnosis.

  **A real B7 retry needs to first extend `pendingPartialSpannerToken` / `spannerConsumed` / `pendingContentOverflow` to absorb descendant break-chains — that's a port, not a cleanup.** Re-scope the retry as "spanner-resume break-chain absorption" (substantial work, comparable in size to v2 B5 walker WRITE-flat) before touching the clamp gate again. Until that port lands, the `IsInsideColumnSpanner` guard remains the correct mechanism and should not be removed.
- ~~**Reclaim border-box-clip regressions** (narrower-gate approach)~~ — **REPLACED by Phase 20** below. Investigation 2026-04-28 found two non-monotonic gate variations (`hasExplicitBlock`, `hasExplicitBlock || IsFixedBlockSize`) — both gain some tests + lose others. Root cause: the broad clip is masking layout/paint bugs that gate-tweaking can't fix. Blink research found the fundamental mechanism is paint-property-tree OverflowClip on the multicol container, with `BoxType=kColumnBox` driving painter decisions. See findings.md error-log + Phase 20 brief.

- **Phase 20 — Multicol overflow clip Blink port** (NEW, recommended next major work). Replace ad-hoc `ClipContentToBorderBox`-on-multicol with a proper Blink-aligned port:
  1. `BoxType` enum on `PhysicalFragment` (kNormalBox, kColumnBox).
  2. Set kColumnBox on column fragments in MLA.layoutLine.
  3. Painter recognises kColumnBox (display-item-fragment scope, no clip).
  4. Column-rule painter uses kColumnBox child extents instead of multicol full-content extent (closes flex-flexitems-2 paint overflow).
  5. Multicol container OverflowClip via paint-property-tree-equivalent path; remove ClipContentToBorderBox from multicol layout (closes 6 regressions, retains 9 wins).
  6. TallestUnbreakable for atomic inlines (closes inline-block-and-column-span-all).
  Worktree work, ~6 commits. Multicol gate target: 205 → 211+ (+6 to +9). Brief in findings.md § "Phase 20 BRIEF". Continuation prompt in `CONTINUE-20.md`.
- **Option 1 — Finish FinishFragmentation port** (drop the `len(children) == 0` leaf-only gate in 16.d.1 + delete or shrink the parent-side children-loop overflow path in `block_layout.go:1001-1196` to the cases Blink handles there — IFC breaks, forced breaks). Larger structural change. The merged walker port should clean up the prior break-token misalignment that blocked this earlier.
- **Phase 19** — span-all-children-height 002-013 (T4, 12 tests, MIXED). 7 sub-clusters. Brief: findings.md § Phase 19.

css-multicol is the active layout-feature track at **205/455 committed**. Recent commits: Phase 15 partial (`4875da5b`, +2: test 001), Phase 16.a BFC filtering (`d42e3cf2`, +2: tests -002, -004), Phase 16.b BSFF row-advance (`a375cb45`, +3 targets but −25 net to gate), Phase 16.c.1 column regrowth port (`2aa01920`, gate-neutral), Phase 16.d.1 per-fragment block-size clamp (`a6446061` + `c40b4b56`, +25 multicol / +4 spanner-fragmentation), Phase 17 forced-break balance (`b13b52b2`, +4), **Phase 16.e+18 v2 bundle merge (`00c0d197`, +3) + multicol border-box clip residual fix (`3389efe7`, +6)**.

Phase 16.c.2 was attempted twice and rolled back both times. v1 brief framed retry #3 as a mechanical post-bundle commit; v2 brief (Option A) inlines clip removal as B3 of the bundle, AFTER B1+B2 port `TallestUnbreakable` to close the upstream gap that 16.d.1 partially addressed. The 2026-04-28 Path A spike showed retry #3 is NOT mechanical without B2's carrier work (Spike A: 9/13).

**Phase ordering (revised 2026-04-27, post-CONTINUE-16e review):**
1. **Phase 16.a** — DONE (`d42e3cf2`, +2). Spanner BFC filtering: `IsValidColumnSpannerInTree` parity. Tests `-002, -004` PASS.
2. **Phase 16.b** — DONE (`a375cb45`, +3 targets / −25 net). BSFF row-advance + spanner WDM/leaf/margin polish; `multicol-span-all-006, -007, -008` PASS at 0 diff. Kept narrowed `ClipBlockAxisOnly`; the regression cluster turned out to have a deeper root cause (see 16.c.2 retro).
3. **Phase 16.c.1** — DONE (`2aa01920`, gate-neutral). Column regrowth port from `column_layout_algorithm.cc:1099-1124` with `BreakToken == nil && BSFF > fragH` carrier gate. Verified `multicol-nested-010` PASS, multicol gate unchanged. Setup for future 16.c.2.
4. **Phase 16.c.2** — ROLLED BACK 2026-04-27 (twice). The 2026-04-28 Path A spike confirmed: clip removal alone (Spike A) regresses -004 + nested-floated, requiring `TallestUnbreakable` carrier (Phase 16.d.2/3) to close the upstream gap. Inlined as **v2 B3** in the bundled phase, AFTER B1+B2 carrier port. No longer "queued mechanical retry" — it's part of the bundle.
5. **Phase 16.d.1** — DONE (`a6446061` + `c40b4b56`, **+25 multicol / +4 spanner-fragmentation**). Per-fragment block-size clamp + `LayoutResult.DidBreakSelf` carrier in `block_layout.go`, plus `ConstraintSpace.IsInsideColumnSpanner` flag set by `MulticolLayoutAlgorithm.layoutSpanner` / `layoutSpannerInFrag` and propagated through child constraint spaces. True-leaf gate (no DOM children, no column-span:all, hasExplicitBlock, !IsBlockSizeOverride, !IsInsideColumnSpanner, FragmentainerBlockSize > 0, !IsInitialColumnBalancingPass). All 13 driver tests PASS at 0 diff without the clip.
6. **Phase 16.d.2/3** — FOLDED INTO v2 B1+B2. `LayoutResult.TallestUnbreakableBlockSize` carrier + `BreakBeforeChildIfNeeded` propagation for `ShouldAvoidBreakInside` children + `SetupFragmentation` border/padding contribution + box_fragment_builder child-result propagation. Mirrors Blink fragmentation_utils.cc:1105-1113 + 510-514, box_fragment_builder.cc:566-569, cla.cc:1879-1948. Hooks already TODO'd at multicol_layout.go:1576+1601. Originally "may be redundant"; the 2026-04-28 spike confirmed it's load-bearing for `-004` and `nested-floated` (and possibly more) once the clip is removed.
7. **Phase 17** — **DONE (+4 multicol, 192→196).** Forced-break balance. `resolveColumnAutoBlockSize` replaced with ContentRuns measure-pass loop; Change 3 fragment extension gated on `IsInsideBalancedColumns && hasAutoColumnHeight()`; Change 2 forced-break-during-measure; Change 4 HasForcedBreak overflow-path.
8. **Phase 16.e + 18 bundled v2** — **DONE (merged 2026-04-28, `00c0d197`).** Walker port + ClipBlockAxisOnly removal + TallestUnbreakable carrier + IsMonolithic flag + contentNode pointer cache. Multicol gate 196 → 199 (+3); 13 drivers 11/13 → 10/13 (3 residuals).
9. **Multicol border-box clip residual fix** — **DONE (`3389efe7`, 2026-04-28).** Multicol container clips children to border-box when block-size > 0. Closes all 3 v2 driver residuals at 0 diff. Multicol gate 199 → 205 (+6 net; +12 -6). 13 drivers 10/13 → 13/13.
10. **B6 / B7 / clip-regression-reclaim** — see "Then queued" above.
11. **Option 1 — Finish FinishFragmentation port** (QUEUED). Drop the leaf-only gate in 16.d.1 + delete/shrink the parent-side overflow path in `block_layout.go:1001-1196`. Worktree.
12. **Phase 19** — span-all-children-height 002-013 (T4, 12 tests, MIXED). 7 sub-clusters. Brief: `findings.md` § Phase 19.

**Gate invariants (post-B6):** CSS2 99/99 · flex 626/629 · css-position 92/105 · wm 781/781 · multicol **205/455** · 13 driver invariants **13/13** · spanner-fragmentation cluster **12/13** (-008 0.2%, pre-existing; outside the 13-driver subset).

**Gate target after B7:** unchanged from B6 unless `IsInsideColumnSpanner` removal frees a few additional cases. The follow-up `ConsumedBlockSize`-chain investigation is the path back to the +12 to +15 gate move that B6's brief mistakenly attributed to the row-phase carrier.

---

## css-multicol Phase 12 — PARTIAL (188/455)

### Completed milestones (one-liners)

| Milestone | Commit | Net | Gate |
|---|---|---|---|
| 12a: Blink-parity fragmentation infra (LayoutLine outer stretch loop) | `2a0d0a07` | +1 | 95/458 |
| 12b: All spanner-fragmentation-* tests | `931f48c5` | +13 | 108/458 |
| 12c: Nested multicol infra (PropagateSpaceShortage, resume-break) | `cccbd05e`+`b0825367` | +22 | 130/458 |
| 12d: Forced-break + break-inside:avoid-column | `6483bc7d` | +2 | 132/458 |
| 12e: max-height-imposes-on-columns | bundled | +1 | 133/458 |
| 12f: column-height/column-wrap (5 cla.cc sites, row-gap) | `35ce3dda` | +6 | 139/458 |
| 12g: balance-break-avoidance (break-appeal propagation) | `287c9fb3` | +3 | 142/458 |
| 12h step 1: Ahem font loader | `356a8b19` | +2 | 144/455 |
| F3a–F3e: row-gap, spanner row-advance, Blink-parity row-snap | multiple | +14 | 158/455 |
| F4: InlineBreakToken resume (item_index OR text_offset) | `617332ae` | +8 net | 166/455 |
| F5: Continuation-row terminal-shortage (list-item-003/004/005) | separate | +3 | 169/455 |
| F2 partial: ClipBlockAxisOnly | separate | +1 | 170/455 |
| F1: @font-face layout-time registration + bidi-level shape segmentation | `41b674ef`+mazzy | wm +2 | 170/455 |
| 12h step 4: GetColumnRuleWidth em-base fix | separate | +19 | 189/455 |
| 12h.6: PropagateBaselineFromChild + UnpositionedListMarker + GapGeometry | `af2bbb77`+`b66e7dba`+`058a5442` | +0 visible | 188/455 |
| 14a: IFC guard + empty-child overflow | `87d06be5` | +7 | 186/455 |
| 14b: nested multicol defer via BlockSizeForFragmentation | `7b7b500b` | +2 | 188/455 |

### Phase 12h.6 — DONE (2026-04-26)

Foundational Blink-parity abstractions. Track B (`af2bbb77`): `PropagateBaselineFromChild` on column + spanner commits. Track C (`b66e7dba`): `UnpositionedListMarker` 4-callsite protocol (structural no-op; markers still paint-time). Track A (`058a5442`): `GapGeometry` + `GapDecorationsPainter` — `pkg/layout/gap_geometry.go`, cross/main gap tracking in layout loop, `drawColumnRules` updated to use `CrossGaps`. Net +0 visible tests. Gate: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 188/455.

### Open items

**F2 phase 2 (OPEN).** `multicol-nested-010` cluster (~7 tests). Nested multicol leaf is split across inner sub-cols instead of placed only in inner sub-col 1's continuation. Requires Blink research on inner-multicol child-break-token forwarding before coding.

**F3 residuals (OPEN, 19 tests).** Largest: `column-height-013` (6500px), `column-wrap-no-constraints-002` (6000px), `column-height-006` (5250px), nowrap cluster (`-005/-011/-030` ~5000px each). `column-wrap:nowrap` overflow requires a paint-layer change to let overflow columns paint past the declared border-box. `column-height-024` class needs a live-Blink build trace.

**F4 regressions — CLOSED by Phase 14a.** `multicol-inherit-001`, `multicol-margin-001`, `multicol-margin-child-001`, `multicol-nested-margin-001` all pass since commit `87d06be5` (IFC guard fix).

**`multicol-rule-stacking-001` (OPEN, 32px).** Near-pass; column count now correct. Sub-pixel rule geometry difference remains.

---

## Phase 15: PercentageResolutionBlockSize for multicol children (IN PROGRESS)

**Root cause.** Percentage-height children of a multicol container with explicit height were resolving against the column height (colBlockSize) instead of the container's content-box height. Three fix sites in `multicol_layout.go`:
1. `createConstraintSpaceForColumn`: `PercentageResolutionSize.BlockSize` was `colBlockSize`; changed to `containerPercentResolutionBlockSize`.
2. `resolveColumnAutoBlockSize`: measurement pass used `AvailableSize = Indefinite`; when container has explicit height, switched to `AvailableSize = containerHeight` + `IsBlockSizeOverride + IsFixedBlockSize` (no `IsContentSuggestionLayout`) so percentage heights resolve during balance estimation.
3. `layoutSpanner`: was `AvailableSize = Indefinite` and `PercentageResolutionSize = 0`; changed both to `containerPercentResolutionBlockSize`.
4. `childPercResolutionBlockSize` (block_layout.go): when `IsBlockSizeOverride && isAnonymous`, return `space.PercentageResolutionSize.BlockSize` (the container height) instead of `explicitBlockSize` (the column height).

**Status.**
- Test 001 (`multicol-span-all-children-height-001`): **PASS** (0 diff). Block1=50px, spanner=100px, block2=50px all resolve correctly.
- Tests 002–013: still failing. Mixed root causes (see below). NOT yet committed.

**Remaining test breakdown (002–013):**

| Test | Diff | Root cause (working theory) |
|---|---|---|
| 002 | 1.3% | Spanner/block2 height difference — under investigation |
| 003 | 1.9% | Same cluster |
| 004a | 26.8% | Fixed-height container split by column-span (no % children) |
| 004b | 15.5% | Same |
| 005 | 8.3% | column-fill:auto + fixed-height container split by column-span |
| 006 | 15.0% | Border on fixed-height container split by column-span |
| 007 | 0.4% | Nested multicol with column-span child (fixed px heights) |
| 008 | 13.3% | Auto-height container split by column-span, border |
| 009 | 2.1% | Negative margin-top on spanner |
| 010 | 2.1% | Span inside inline-level container |
| 011 | 0.3% | Overflow columns with column-span |
| 012 | 1.9% | Absolute-positioned child inside multicol + column-span |
| 013 | 0.6% | Multicol inside fixed-height wrapper |

Investigation paused to update tracking files. Resume with test 002 root cause.

---

## Phase 14: fragmentation fixes

| Sub-phase | Status | Notes |
|---|---|---|
| 14a IFC guard | **DONE** (`87d06be5`) | multicol 179→186; closed 4 F4 regressions |
| 14b nested leaf-frag | **DONE** (2026-04-26) | multicol 186→188; `BlockSizeForFragmentation` signal |
| 14c clear-001 | **PERMANENTLY DEFERRED** | CoreText font metrics mismatch; no targeted fix possible |

---

## Phase 16: Spanner BFC filtering (T1) → Row-advance + paint discipline

### Phase 16.a — DONE (commit `d42e3cf2`, +2)

**Landed:** Blink-parity `IsValidColumnSpannerInTree` predicate chain. Helpers `isSelfValidColumnSpanner` + `shouldPreventColumnSpannerDescendants` in `block_layout.go:2185-2248`. New `ConstraintSpace.ColumnSpannerDescendantsBlocked` flag propagated to child spaces. Gate at `block_layout.go:379-384` requires both helpers + flag negation.

**Closed tests:** `multicol-span-all-002, -004` (table-caption + BFC sampler). Gate: 190 → 192/455.

### Phase 16.b — DONE (commit `a375cb45`, +3 targets / −25 net)

**Landed:** BSFF row-advance + spanner polish in `multicol_layout.go` and `block_layout.go`.

- `block_layout.go`: populated `BlockSizeForFragmentation` at 4 BLA return sites (spanner early return, nested ColumnSpannerPath propagation, MinSpaceShortage return, zero-fragment break return). Added nested-spanner ColumnSpannerPath propagation so a grandchild spanner detected by an inner BLA bubbles up to the multicol algorithm.
- `multicol_layout.go`: `spannerLeafNode` traversal (for nested spanner extraction), spanner constraint-space uses spanner's own WDM (`NewWritingDirectionMode(spanner.Style())`) for RTL parity, spanner margin-block-start/end via `ResolveMargins` (Blink cla.cc:1441), `maxColHeight` uses BSFF, row-advance cap conditioned on `!hasSpannerDetector`, narrowed `ClipBlockAxisOnly` predicate, cross-gap loop only adds a gap between adjacent columns that both have content, cross-gap padding only when at least one actual cross gap exists.

**Closed tests:** `multicol-span-all-006, -007, -008` PASS at 0 diff. `column-height-001` continues to PASS (was already at baseline).

**Regression cluster (recovery target for 16.c):** `column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`, `as-column-flex-item`, `column-span-none-001`, `column-wrap-no-constraints-001`, `equal-gap-and-rule`, `orthogonal-writing-mode-spanner`, plus a handful of one-offs. Multicol gate 192 → 167; spanner-fragmentation 12/13 → 7/13.

**Why the regressions:** `ClipBlockAxisOnly` was kept (just narrowed). Phase 16.c's Blink research (`box_fragment_painter.cc:1080-1114`) confirms Blink has no per-column paint clip at all; any predicate diverges from Blink and lights up cases where a non-spanner column legitimately overflows colBlockSize.

### Phase 16.c.1 — DONE (commit `2aa01920`, gate-neutral)

Ported `column_layout_algorithm.cc:1099-1124` regrowth into `layoutLine`: new `minimumColumnBlockSize` parameter, threaded through `constrainColumnBlockSize` as a floor applied after upper clamps (Blink-parity via `available_outer_space = std::max(minimum, FragmentainerSpaceLeftForChildren() - line_offset)`). After the inner column loop, when nested in a column fragmentainer and any column shows true monolithic overflow (`BreakToken == nil && BSFF > fragH`), tail-recurse with the floor raised. The `BreakToken == nil` gate was the carrier-specific refinement: BSFF in louis14 includes trailing fragmented content, so without that gate the regrowth fired on `multicol-nested-030/031` (break-inside:avoid violated, content fragmented across 4 cols) and collapsed them into a single oversized column. With the gate, regrowth is correctly silent on those tests.

Verified: `multicol-nested-010` PASS, multicol gate 167/455 unchanged from baseline. The commit is a setup commit for future 16.c.2 (clip removal) — currently nothing exercises the regrowth path because `ClipBlockAxisOnly` continues to satisfy nested-multicol containment.

### Phase 16.c.2 — ATTEMPTED, ROLLED BACK (2026-04-27)

Removed `ClipBlockAxisOnly` setter (`multicol_layout.go`) + paint-side branch (`paint_layer.go`). Result: net **−8 multicol** (167 → 159), spanner-fragmentation unchanged at 7/13. **Reverted both files** per the brief's "STOP, ROLLBACK, do NOT chase the regression with a new predicate" guidance.

| Effect | Tests | Notes |
|---|---|---|
| 16.b cluster recovered | **0** | column-height-003/004, multicol-list-item-003/004/005, multicol-fill-balance-005/018/024, spanner-fragmentation-{000,002,008,010,012}, multicol-nested-{015,026,028}, change-fragmentainer-size-{001,002,003} all stay failing — root cause is upstream of the paint clip. |
| Newly broken (clip was load-bearing) | **13** | `column-height-001/010/017/026/027` (column-wrap:wrap monolithic), `multicol-nested-030/031` (break-inside:avoid), `spanner-fragmentation-001/004/006`, `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`. |
| Recovered (clip was actively breaking) | **5** | `increase-prev-sibling-height`, `inline-block-and-column-span-all`, `multicol-fill-balance-032`, `multicol-nested-029`, `multicol-zero-height-002`. |

**Diagnosis.** The brief assumed that removing `ClipBlockAxisOnly` would recover the 16.b cluster because Blink has no per-column clip. The first half is right (Blink has no clip — verified via `box_fragment_painter.cc:1080-1114`, `layout_box.cc:4002-4016`). The second half is wrong: the 16.b cluster's failure mode is unrelated to paint. The newly-broken cluster reveals where the clip is actually load-bearing — `column-wrap:wrap` and `break-inside:avoid` paths place a monolithic block at full size in every column-fragment and rely on the per-column clip to cap visible extent at `colBlockSize`. Blink fragments such a block at column boundaries instead, so each column-fragment shows a different slice.

**See `findings.md` § "Phase 16.c.2 attempt — what we learned"** for the complete retrospective and pointer to Phase 16.d.

### Phase 16.d — PROPOSED (prerequisite for re-attempting 16.c.2; research DONE 2026-04-27)

Research commit (research-only) reads `box_fragment_painter.cc:1080-1114`, `block_break_token.h:96-106`, `box_fragment_builder.cc:519-569`, `fragmentation_utils.cc:510-1113`, `column_layout_algorithm.cc:1879-1948`. Resolves Hypothesis A (paint-clip) vs B (multi-fragment slicing) — **B is correct**, but the brief's proposed mechanism (`MonolithicOverflow` carrier) is wrong. `MonolithicOverflow` in Blink is **strictly print-only** (gated by `IsPaginated()`) and would not fire on any multicol test. The actual mechanism is **regular CSS block fragmentation via `DidBreakSelf` + `BlockBreakToken.ConsumedBlockSize`**, plus `TallestUnbreakableBlockSize` for `break-inside:avoid` content.

Three independent sub-fixes (revised plan in `findings.md` § "Phase 16.d Blink research"):
- **16.d.1** — Per-fragment block-size clamp in `block_layout.go` final-size computation. Add `LayoutResult.DidBreakSelf bool`. When `space.HasBlockFragmentation && finalBlockSize > spaceLeft`, clamp to spaceLeft and emit a continuation `BlockBreakToken` with updated `ConsumedBlockSize`. Mirrors Blink `FinishFragmentation` (fragmentation_utils.cc:636-657). Should recover most of the 13 driver tests (5 column-height + 3 spanner-fragmentation + several misc, all of which use ordinary blocks, not `break-inside:avoid`).
- **16.d.2** — `LayoutResult.TallestUnbreakableBlockSize float64` + `IsInitialColumnBalancingPass` setter in `fragmentation_utils.go.BreakBeforeChildIfNeeded` for `break-inside:avoid` children + builder propagation. Required for `multicol-nested-030/031` only.
- **16.d.3** — Floor `constrainColumnBlockSize` by `tallestUnbreakableBlockSize` (sibling to Phase 16.c.1's `minimumColumnBlockSize` floor). Mirrors `cla.cc:1942-1948`.

Once 16.d.1+16.d.2+16.d.3 land, retry 16.c.2 (clip removal) — it should then be net-positive (every fragment sized to its fragmentainer at layout time, the per-column clip is redundant).

---

## Phase 17: Forced-break balance (T2, ~5 tests)

**Targets.** `multicol-fill-balance-040` family (`-038, -039, -040, -041`) + subset of `-029..-036`. Multicol with N forced `break-before:column` children must balance to `max(N+1, K)` columns at content-determined height.

**Blink reference (full brief in `findings.md` § Phase 17).**
- Algorithm location: `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:1734-1934` (`ResolveColumnAutoBlockSizeInternal`).
- Key types: `struct ContentRun { content_block_size; implicit_breaks_assumed_count; ColumnBlockSize() }`, `class ContentRuns { runs_; AddRun; DistributeImplicitBreaks(K); TallestColumnBlockSize() }`.
- Measure-pass loop: keep calling `Layout()` while `BreakToken` exists; append per-slice block-size to `runs`; count `forcedBreakCount`. After loop, `DistributeImplicitBreaks(numCols)` adds implicit breaks onto tallest run until `total >= numCols`.
- Expansion math: `max(N+1, K)`, achieved by failing acceptance test `actual_column_count <= used_column_count_` and exiting via cla.cc:1211 safety valve (`if numCols <= forcedBreakCount+1: break`).

**Louis14 gap.**
- `multicol_layout.go:1382-1455` (`resolveColumnAutoBlockSize`) does single `Layout()` call + `ceil(total/numCols)`. NO measure-pass loop, NO `ContentRun`/`ContentRuns`, NO forced-break counting during measurement.
- Outer stretch loop at `multicol_layout.go:1031-1206` is correct: counts `forcedBreakCount` (line 1042/1112), exits at `numCols <= forcedBreakCount+1` (line 1180-1183 mirrors Blink cla.cc:1211).
- Forced-break propagation already complete: `LayoutResult.HasForcedBreak` (`layout_result.go:106`), `BlockBreakToken.IsForcedBreak`, `BreakBeforeChildIfNeeded` returns `BrokeBefore`.

**Louis14 fix.** Replace body of `resolveColumnAutoBlockSize` with Blink-parity loop (~120 lines):

```go
type contentRun struct { contentBlockSize float64; implicitBreaksAssumed int }
func (r contentRun) columnBlockSize() float64 {
    return math.Ceil(r.contentBlockSize / float64(r.implicitBreaksAssumed+1))
}

runs := []contentRun{}
breakToken := childBreakToken
forcedBreaks := 0
for {
    space := buildMeasureSpace(...) // IsInitialColumnBalancingPass=true
    result := layoutElement(ctx, multicolChild, space)
    if forcedBreaks < numCols { runs = append(runs, contentRun{result.BlockSizeForFragmentation}) }
    if result.HasForcedBreak { forcedBreaks++ }
    if result.BreakToken == nil { break }
    breakToken = result.BreakToken
}
if balanceColumns { distributeImplicitBreaks(runs, numCols) }
return constrainColumnBlockSize(tallestColumnBlockSize(runs), lineOffset, availableOuter)
```

**Test order.** `-040` (canonical) → `-039`/`-038`/`-041` → cluster sweep.

**Tractability.** Medium. One function rewrite. Verify `BlockSizeForFragmentation` vs `IntrinsicBlockSize` semantics (Blink uses former for trailing-margin accounting).

---

## Phase 18: Nested multicol break-token forwarding (T3, ~15 tests)

**Targets.** `multicol-nested-011..032` cluster + `multicol-fill-balance-003, -026`. Inner multicol must break and resume in next outer column instead of spilling sideways.

**Blink reference (full brief in `findings.md` § Phase 18).**
- Data structure: `struct MulticolBreakTokenData { LayoutUnit consumed_row_block_size; }` (file: `third_party/blink/renderer/core/layout/multicol_break_token_data.h`).
- Polymorphic carrier: `BreakTokenAlgorithmData` GC'd base with `DataType` enum (`kMulticolData`, etc.). Stored on `BlockBreakToken::data_`. Accessor `TokenData()`. Builder: `BoxFragmentBuilder::SetBreakTokenData(...)`.
- Write site (cla.cc:~1374): when `ShouldWrapColumns() && HasRowHeight() && is_first_row && HasKnownFragmentainerBlockSize() && overflow > 0`, emit `MulticolBreakTokenData(RowHeight() - overflow)`.
- Read site (cla.cc:~2122 `OffsetInCurrentRow`): adds `data->consumed_row_block_size` to `line_offset` before modulo.

**Louis14 state.**
- `pkg/layout/break_token.go` is a flat record. NO polymorphic carrier exists.
- `multicol_layout.go:43-46` `consumedRowBlockSize` field exists on `MulticolLayoutAlgorithm` but is hard-coded to 0 at line 292 (comment: "wired from the break token when 12f.6 lands").
- `offsetInCurrentRow` (line 187-197) ALREADY consumes `mla.consumedRowBlockSize` — read side plumbed; write side missing.
- `multicol_layout.go:504-512` (`buildOuterBreakResult`) emits outer break-token but never seeds `consumed_row_block_size`.
- Phase 14b defer hook only handles `column-fill: auto + explicit height`; doesn't handle balanced or partial-row case.

**Louis14 fix (5 steps, ~150 lines).**
1. Add `MulticolBreakTokenData *MulticolData` field on `BlockBreakToken` in `pkg/layout/break_token.go`.
2. Write site in walker loop at `multicol_layout.go:542` (`needsRowAdvance`): when row partially fits, compute `paintedAmount = rowHeight - overflow`, attach via new `buildOuterBreakResultWithRowCarry(paintedAmount)`.
3. Read site at `multicol_layout.go:289-292`: replace `mla.consumedRowBlockSize = 0` with read from `mla.space.BreakToken.MulticolData.ConsumedRowBlockSize`.
4. Verify outer per-column break-token chain at lines 1046/1143 preserves the field (should be automatic).
5. Generalize Phase 14b condition at line 381 from `columnFill == "auto"` to also handle `"balance"` once row-carry write is in place.

**Test order.** `multicol-nested-011` (simplest single overflow) → confirm round-trip → sweep `-012..-032` → `multicol-fill-balance-003, -026`.

**Tractability.** Hard. Largest of the three. Touches break-token type + 5 sites in `multicol_layout.go` + Phase 14b condition. Carrier shape decision (typed pointer vs polymorphic interface) deferred — typed pointer is minimal-blast-radius.

---

## Phase 19: span-all-children-height 002-013 (T4, 12 tests)

**Targets.** Tests 002-013 in `multicol-span-all-children-height` cluster. Phase 15 closed 001 only; 002-013 are heterogeneous (7 sub-clusters with different root causes). Treat as a series of small fixes, not a monolithic phase.

**Sub-cluster decomposition (full per-test detail in `findings.md` § Phase 19).**

| Sub-cluster | Tests | Root cause | Fix site | Tract. |
|---|---|---|---|---|
| A | 002, 003 | Post-spanner remaining-height not updated correctly | `multicol_layout.go` Layout() ~1050-1100 | Med |
| B | 004a, 004b, 005, 006, 008 | Fixed-height-block-split-by-spanner doesn't distribute height across sections (NEW logic, NOT a Phase 15 refinement) | New `distributeHeightAcrossSpannerSections()` at `multicol_layout.go:~1100-1200` | Hard |
| C | 007 | Sub-pixel rounding in nested multicol spanner geometry | `layoutSpanner()` containerPercentResolutionBlockSize propagation when nested | Easy |
| D | 009, 010 | Negative margin-top on spanner uses column height | `layoutSpanner()` margin resolution | Med |
| E | 011 | Overflow column boundary rounding | gap geometry / paint boundary calc | Easy |
| F | 012 | Abspos uses fragmentainer size as containing block | `createConstraintSpaceForColumn` abspos handling | Med |
| G | 013 | Multicol-in-fixed-wrapper doesn't use own explicit height | `Layout()` containerPercentResolutionBlockSize init | Easy |

**Recommended attack order.** B (5 tests, dominant gain) → A (2 tests) → D (2 tests) → C/E/G (bundle, 3 tests) → F (1 test).

**Tractability.** Mixed. B is the dominant target.

---

## css-position (92/105 — effectively complete)

13 pre-existing residuals remain; no active work. Groups:
- 8 G-ABS-IN-TABLE (`position-relative-table-*-absolute-child`) — abspos in positioned table-internals
- 3 G-SEMI-REPLACED (`position-absolute-semi-replaced-stretch-*`) — abspos stretch on button/input/other
- `clear-001.xht` — permanently deferred (see Phase 14c)
- `containing-block-change-scrollframe.html` — needs `Element.scrollTop` JS setter + overflow scroll paint

---

## css-flexbox (626/629 — watch invariant)

Three pre-existing residuals; no active work.

| Test | Diff | Research done | Root cause |
|---|---|---|---|
| `auto-margins-001.html` | ~1024px (0.2%) | Yes (2026-04-21) | VRL cross-axis auto-margin resolution; `getItemAutoMargins` loses item-vs-container WM distinction |
| `content-height-with-scrollbars.html` | ~69200px (14.4%) | Yes (2026-04-21) | `classicScrollbarWidth()` returns 0 for `"auto"`; Blink default is 15px |
| `flexbox-align-self-vert-004.xhtml` | ~3664px (0.8%) | Yes (2026-04-21) | Column-direction baseline synthesis path disagrees between accumulation and placement passes |

---

## Deferred / out-of-scope

| Item | Notes |
|---|---|
| `column-wrap:nowrap` overflow columns | Paint-layer change to allow overflow columns past declared border-box |
| `MulticolBreakTokenData` row-carry | Safe default `consumedRowBlockSize=0` until needed |
| `drawColumnRules` content-area `render.go:2931-2933` | math.Round not migrated to SnapSizeToPixel |
| F1c paint-side shape sharing | `ShapeResult::CopyRange`; needed for non-level-0 cross-span kerning |
| `openFont` signature cleanup | `FontPathToFamilyVariant` + `resolveFamily` path fallback still present |
| G-ABS-IN-TABLE (8 tests) | abspos children of positioned thead/tbody/tfoot/tr |
| G-SEMI-REPLACED (3 tests) | abspos stretch on button/input/other |
| G-SCROLL (1 test) | `Element.scrollTop` JS setter + overflow:hidden scroll paint |
| spanner-fragmentation-005 | Pre-existing residual since Phase 12b |
| Anchor positioning | No WPT tests exercise it |
| StickyPositionScrollingConstraints | Scroll-time wiring deferred until scroll tests appear |

---

## Rules & Discipline

Authoritative sources (re-read at session start):

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index.

**Per-target discipline (CLAUDE.md recap):**
1. Read Blink source BEFORE writing code.
2. Run only the 1–4 driver tests during feature work; gate sweep (all 6 invariants) before each commit.
3. Sub-pixel diffs (even 0.1%) are real bugs — fix at source.
4. If gate sweep regresses: STOP, ROLLBACK, re-read Blink before re-attempting.

## Archived wm work

css-writing-modes is complete (781/781). Planning/findings/progress archived to `docs/plan-wm.md`, `docs/findings-wm.md`, `docs/progress-wm.md`. Do not duplicate here.

## Test command templates

```bash
# Single multicol test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/<name>' -v

# Full build check
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...

# Gate sweep invariants (run each separately)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -v              # CSS2: expect 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'  # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox'        # expect >=626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position'       # expect 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol'       # current: 167/455 at HEAD `2aa01920` (post-16.c.1)
```
