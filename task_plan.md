# Task Plan: css-multicol (active) → fragmentation fixes

## Current focus (2026-04-27 — Phase 16.c queued, regression recovery)

css-multicol is the active layout-feature track at **167/455 committed** (gate 2026-04-27 post-Phase-16.b). Phase 16.b shipped at commit `a375cb45` with the BSFF row-advance fix (closing `-006, -007, -008` at 0 diff) but introduced ~25 regressions because `ClipBlockAxisOnly` was kept (just narrowed) — a workaround that has no Blink analog. Recent commits: Phase 15 partial (`4875da5b`, +2: test 001), Phase 16.a BFC filtering (`d42e3cf2`, +2: tests -002, -004), Phase 16.b BSFF row-advance (`a375cb45`, +3 targets but −25 net to gate).

**Phase 16.c is the active sub-phase** — port Blink's column-regrowth (`column_layout_algorithm.cc:1099-1124`) to handle nested-multicol overflow at layout time, then **remove `ClipBlockAxisOnly` entirely** (Blink does not clip per-column; see `box_fragment_painter.cc:1080-1114`). Targets the regression cluster from 16.b: `column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`. Detailed plan + Blink quotes in `findings.md` § "Phase 16.c brief (post-a375cb45)" — read **before coding**.

**Phase ordering (revised 2026-04-27):**
1. **Phase 16.a** — DONE (`d42e3cf2`, +2). Spanner BFC filtering: `IsValidColumnSpannerInTree` parity. Tests `-002, -004` PASS.
2. **Phase 16.b** — DONE (`a375cb45`, +3 targets / −25 net). BSFF row-advance + spanner WDM/leaf/margin polish; `multicol-span-all-006, -007, -008` PASS at 0 diff. Trade-off: kept `ClipBlockAxisOnly` (narrowed predicate) which doesn't match Blink and hit the regression cluster recovered by 16.c.
3. **Phase 16.c** — QUEUED. Port column regrowth from `column_layout_algorithm.cc:1099-1124` to `LayoutLine`, then remove `ClipBlockAxisOnly` entirely. Two-step plan; verify `multicol-nested-010` passes after step 1 BEFORE doing step 2. Gate target: 167 → 192+ (recover 16.b regression net).
4. **Phase 17** — Forced-break balance (T2, ~5 tests, MEDIUM). Rewrites `resolveColumnAutoBlockSize` (`multicol_layout.go:1396`) with Blink-parity `ContentRun`/`ContentRuns`/`DistributeImplicitBreaks` measure-pass loop. Brief: `findings.md` § Phase 17.
5. **Phase 18** — Nested multicol break-token forwarding (T3, ~15 tests, HARD). Adds `MulticolBreakTokenData` carrier on `BlockBreakToken`. Brief: `findings.md` § Phase 18.
6. **Phase 19** — span-all-children-height 002-013 (T4, 12 tests, MIXED). 7 sub-clusters. Brief: `findings.md` § Phase 19.

**Gate invariants (committed at HEAD `a375cb45`):** CSS2 99/99 · flex 626/629 · css-position 92/105 · wm 781/781 · multicol **167/455** · spanner-fragmentation 7/13.

**Gate invariants (target after 16.c):** CSS2 99/99 · flex 626/629 · css-position 92/105 · wm 781/781 · multicol **192+/455** · spanner-fragmentation 12/13.

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

### Phase 16.c — QUEUED (regression recovery via Blink-parity column regrowth)

**Targets.** Recover the regression cluster above (~25 multicol tests + 5 spanner-fragmentation). Drivers: `multicol-nested-010` (load-bearing risk — see step 1), then `column-height-003`, `spanner-fragmentation-000`, `multicol-list-item-003`, `multicol-fill-balance-005`, `multicol-nested-015`.

**Blink reference (full quotes in `findings.md` § Phase 16.c brief).**
- `box_fragment_painter.cc:1080-1114` — fragmentainer paint dispatch has zero clip recorder.
- `layout_box.cc:4002-4016` — `ComputeOverflowClipAxes` has no multicol special case; multicol containers default to `overflow:visible` and don't clip.
- `column_layout_algorithm.cc:1099-1124` — when an inner column's `BlockEndScrollableOverflow()` exceeds the outer fragmentainer's remaining space, Blink sets `minimum_column_block_size = block_end_overflow` and tail-recurses into `LayoutLine`. Layout-time regrowth is the entirety of nested-multicol containment.
- `column_layout_algorithm.cc:1936-1968` (`ConstrainColumnBlockSize`) — "the only thing we need to worry about here is to not overflow the multicol container."

**Two-step plan (full detail in `findings.md` § Phase 16.c brief):**

| Sub-phase | Change | Site | Risk |
|---|---|---|---|
| **16.c.1** | Port Blink column regrowth: in `LayoutLine`, when nested fragmentation context + `lineOffset + max(BSFF) > FragmentainerSpaceLeftForChildren()` + `max(BSFF) > columnSize.BlockSize`, set `minimumColumnBlockSize` and tail-recurse into `LayoutLine`. New optional `minimumColumnBlockSize` parameter on `LayoutLine`; `ConstrainColumnBlockSize` uses it as a lower bound. Try `BlockSizeForFragmentation` as the carrier first; only add `LayoutResult.BlockEndScrollableOverflow` if a specific test requires it. | `multicol_layout.go` | Medium-high; recursion shape is load-bearing. **Verify `multicol-nested-010` PASSES before doing 16.c.2.** |
| **16.c.2** | Remove `ClipBlockAxisOnly = true` at `multicol_layout.go:1218-1232` (the `shouldClip` block). Remove the paint-side `blockAxisOnlyClip` branch at `paint_layer.go:279-296`. Optional cleanup commit later: remove `ClipBlockAxisOnly` field from `PhysicalFragment`, `Box`, and the `engine.go:332` propagation. | `multicol_layout.go`, `paint_layer.go` | Low if 16.c.1 passes `multicol-nested-010`. Mechanical otherwise. |

**Anti-patterns (DO NOT):**
- Don't keep `ClipBlockAxisOnly` with yet-another predicate. Blink has no fragmentainer-level clip; any predicate is a hack.
- Don't add a new `LayoutResult` field unless `BlockSizeForFragmentation` is verifiably insufficient.
- Don't port the regrowth without the recursion. `ConstrainColumnBlockSize` clamps; `LayoutLine` recurses. Both are needed.
- Don't run the full WPT sweep during sub-phase iteration. Per CLAUDE.md, only the 1-4 driver tests; gate-sweep before commit.

**Test order.**
1. After 16.c.1: run `multicol-nested-010` only. Expected: PASS.
2. After 16.c.1: also run `-005, -006, -007, -008` and `column-height-001` (Phase 16.b drivers). Expected: still PASS.
3. After 16.c.2: run regression cluster (one driver per category). Expected: most return to PASS.
4. Full multicol gate sweep before commit. Expected: ≥192/455.
5. If `multicol-nested-010` fails after 16.c.1: do NOT proceed to 16.c.2. Re-investigate the regrowth carrier.

**Gate target.** 167 → 192+/455 (recover 16.b regression net). Spanner-fragmentation 7/13 → 12/13. Best case: 195+/455 because the three 16.b targets stay passing on top of the recovered baseline.

**Tractability.** Medium-high. The regrowth port is small (~50 lines) but touches `LayoutLine`'s recursion shape. The `ClipBlockAxisOnly` removal is mechanical once regrowth is in.

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
  -run 'TestWPTCSS3Reftests/css-multicol'       # active target: 167/455 at HEAD; 192+/455 after 16.c
```
