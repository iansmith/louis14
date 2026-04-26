# Progress Log — css-multicol (active)

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not duplicate here.

## Archived wm work
All writing-modes progress archived to `docs/progress-wm.md`. Do not copy wm content here.

---

## Current gate (2026-04-26)

| Category | Count | Notes |
|---|---|---|
| CSS2 | **99/99** | invariant |
| css-flexbox | **626/629** | 3 pre-existing residuals |
| css-position | **92/105** | 13 pre-existing residuals; no active work |
| css-writing-modes | **781/781** | complete |
| css-multicol | **188/455 committed · 190/455 uncommitted (Phase 15 partial)** | active target; 265 failing |
| spanner-fragmentation | **12/13** | 005 pre-existing |

---

## Completed phases (brief entries)

### css-position (Phases 0–11, DONE 2026-04-21)

50 → 92 passing across 11 phases. Key commits: `d174049b` (G-TABLE-REL Part A), `ac2dc780` (section fragments), `b6ec7d3f` (§10.8.1 baseline fix), `ed16475f` (OOF re-entrance), `7e686a28` (positioned root), `01f468d9` (inline containing block), `05aff97e` (sticky zero offset), `0e1fde9f` (abs-replaced), `a7e79598` (block-in-inline %-insets + table-internals), `1bdcfc85` (::marker UA rule), `a22cfe10` (button inline-flex + flex OOF padding-box), paint-phase refactor (stack-floats-001), WPT sub preprocessor (iframe-print-001/002). Final gate: wm 781/781, CSS2 99/99, flex 626/629, position 92/105.

### Phase 12a: Fragmentation infra (DONE 2026-04-22, commit `2a0d0a07`, +1)

Blink-parity rewrite of `multicol_layout.go`: `LayoutLine()` outer stretch loop, `resolveColumnAutoBlockSize()`, `constrainColumnBlockSize()`, `BlockBreakToken` threading, inline fragmentation at column boundaries, multicol dispatch in `block_layout.go`. Driver `multicol-fill-balance-001.xht` at 0 diff. Gate: wm 781/781, CSS2 99/99, flex 626/629.

### Phase 12b: Spanners (DONE 2026-04-23, commit `931f48c5`, +13)

`MulticolPartWalker`, `ColumnSpannerPath`, `layoutSpanner`, spanner-forces-balance-on-preceding-row, ghost-row fix (3 parts), leaf-block fragmentation fixes (2 changes in `block_layout.go`), pointer-stable `groupedChildrenCache`, whitespace-only inline run suppression. All 13 `spanner-fragmentation-*` tests at 0 diff.

### Phase 12c: Nested multicol (DONE 2026-04-23, commits `cccbd05e`+`b0825367`, +22)

Balance guard fix (cla.cc:1025 parity), `BoxFragmentBuilder.PropagateSpaceShortage` + callsite in `multicol_layout.go`, resume-break emission for outer-boundary hit, resume-path wiring (`nextColToken ← colRowsResumeToken`). `MulticolBreakTokenData` row-carry deferred. Driver `multicol-nested-010.html` 6000 → 3500px.

### Phase 12d: Forced breaks (DONE 2026-04-24, commit `6483bc7d`, +2)

`break-before:column` and `break-inside:avoid-column` dispatch. `BlockBreakToken.IsForcedBreak` threading.

### Phase 12e: max-height-imposes-on-columns (DONE 2026-04-24, bundled, +1)

Driver `multicol-fill-auto-block-children-003` at 0 diff.

### Phase 12f: column-height/column-wrap (DONE 2026-04-24, commit `35ce3dda`, +6)

5 of 6 cla.cc consumption sites: LayoutLine block-size override, row-wrap loop, ConstrainColumnBlockSize upper-bound clamp, intrinsic top-off, break-token slot-layout fix. Row-gap plumbing (`GetRowGapMulticol()`). Driver `column-height-001.html` at 0 diff. Site 6 (`MulticolBreakTokenData` row-carry) deferred.

### Phase 12g: Balance-break-avoidance (DONE 2026-04-24, commit `287c9fb3`, +3)

Break-appeal propagation from the overflow path + `MinSpaceShortage` for soft-break path. Full `EarlyBreak`+`RelayoutAndBreakEarlier` NOT ported. Three `balance-break-avoidance-*` drivers at 0 diff.

### Phase 12h step 1: Ahem font loader (DONE 2026-04-24, +2)

Font URL → direct file path for WPT Ahem consumers. Unlocked several rule-paint tests.

### Phase 12h F3a–F3e: column-height cluster (DONE 2026-04-24, multiple commits, +14)

`row-gap` plumbing, `columns:N/H` shorthand + `column-height:0` zero-frag last-resort + abspos-in-multicol OOF aggregation, spanner first-row advance, Blink-parity spanner pre-snap + row-wrap first-iter guard, non-auto-column-height-triggers-multicol. multicol 154 → 168.

### Phase 12h F4: InlineBreakToken resume (DONE 2026-04-24, commit `617332ae`, +8 net)

Two gates in `block_layout.go` and `inline_layout.go` changed from `InlineItemStartIndex > 0` to `(InlineItemStartIndex > 0 || InlineTextOffset > 0)`, matching Blink's `InlineBreakToken.start_` carrying both item_index and text_offset. `multicol-rule-large-001` PASS at 0 diff (was 13.1%). 4 margin-family regressions accepted (pre-existing bug exposed, deferred).

### Phase 12h F5: Post-spanner continuation row (DONE 2026-04-24, +3)

When `lineOffset > 0` (continuation row) + `MinSpaceShortage > 0` + `HasSeenAllChildren=true` + no `ChildBreakTokens`, set `hasViolatingBreak=true` to force stretch loop. Closed `multicol-list-item-003/004/005`.

### Phase 12h F2 partial: ClipBlockAxisOnly (DONE 2026-04-24, +1)

`PhysicalFragment.ClipBlockAxisOnly bool` + `Box.ClipBlockAxisOnly bool` threaded through `engine.go`. Column fragmentainers now allow inline overflow while clipping block overflow. `multicol-nested-010` 4500 → 3500px.

### Phase 12h F1: @font-face + bidi-level shaping (DONE 2026-04-25, +2 wm)

Three-layer fix: (1) `RegisterBuffer(family, variant, []byte)` on `mazarin/textshape.GlyphProvider` interface + both backends; layout engine gains `CurrentProvider()` so layout and renderer share one provider; `pkg/text/fontcache.go` rewrite drops temp-file caching. (2) `canMergeShapingContext` in `line_breaker.go` now requires equal `BidiLevel`; `isBidiBoundary` breaks at `unicode-bidi != normal` span tags. (3) `applyCrossSpanKerning` gated on level-0 items only. wm 779 → 781/781.

### Phase 12h.6: GapGeometry + baseline + list-marker protocol (DONE 2026-04-26, 3 commits)

**Track B** (`af2bbb77`): `PropagateBaselineFromChild` — `firstBaseline`/`lastBaseline` fields on `BoxFragmentBuilder` + `LayoutResult`; min/max semantics; two callsites (per-column + spanner commit). **Track C** (`b66e7dba`): `UnpositionedListMarker` 4-callsite protocol in `MulticolLayoutAlgorithm` (constructor, end-of-Layout fallback, first-column attempt, spanner attempt). New `pkg/layout/unpositioned_list_marker.go`. **Track A** (`058a5442`): `GapGeometry` + `GapDecorationsPainter` — new `pkg/layout/gap_geometry.go` with `CrossGap`/`MainGap`/`SpannerMainGapType` types; fields wired through `BoxFragmentBuilder`→`PhysicalFragment`→`Box`→`PaintLayer`; `drawColumnRules` updated to consume `GapGeometry.CrossGaps`. Gate: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol 188/455 · spanner-frag 12/13.

---

## Phase 13: LayoutUnit precision discipline (CLOSED 2026-04-26)

Ten sub-phases landed in 16 commits. All gate invariants held at every checkpoint. Key commits:

| Sub-phase | Commits | Description |
|---|---|---|
| 13a | `3897b43e` | `pkg/geometry/layoutunit`: LayoutUnit scalar type, 16 unit tests |
| 13b | `20f25053` | `pkg/geometry`: composites (LogicalOffset/Size/Rect, PhysicalOffset/Size/Rect, WritingModeConverter), 11 unit tests |
| 13c | `6e689d8e`/`4dc4ac0b`/`912c03fa` | PhysicalFragment.RelativeOffset + ChildLink.Offset + Size migrated |
| 13d | `7d64570a`–`7db1f2fd` (5 commits) | ConstraintSpace + BlockBreakToken + ExclusionSpace precision fields |
| 13e | `ff45432a` | `ResolvePercent` helper; 11 call sites routed through it |
| 13e′ | `ae7d60ed`/`b5d1e6c1`/this | `ResolveInlineSize`/`ResolveBlockSize`/Min/Max return-type promotions |
| 13f | `c5c9b67c`/`76ef4cb4` | Text-shaping boundary: `MeasureText` → LayoutUnit, `ShapeAdvances` → ShapeCumulative |
| 13g.1 | `776ae6d5` | `SnapSizeToPixel`/`SnapSizeToPixelAllowingZero` in geometry package |
| 13g.2–13g.4 | `050cf822`+2 | Migrated drawBorders inner-corner, pixelSnap helper, background-origin switch |
| 13h | — | Verification + cleanup; math.Round audit; Phase 13 CLOSED |

Gate at close: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 179/455. Rounding-mode discovery: `FromFloat64Trunc` regressed multicol 179→172 (IEEE 754 double vs float32 boundary noise); switched to `FromFloat64Round`.

---

## Phase 14: Fragmentation fixes (DONE/DEFERRED 2026-04-26)

### Phase 14a: IFC fragmentation guard (DONE, commit `87d06be5`)

Three-part fix (see task_plan.md). Closed 4 F4 regressions. multicol 179 → 186. Gate: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 186/455.

### Phase 14b: Nested multicol leaf-frag defer (DONE, 2026-04-26)

Added `BlockSizeForFragmentation float64` to `LayoutResult`. `multicol_layout.go` returns 0-height fragment with the field set when nested + column-fill:auto + explicit height + insufficient outer space. `fragmentation_utils.go` `BreakBeforeChildIfNeeded` breaks before when `BlockSizeForFragmentation > spaceLeft`. Extended `collapseThrough` guard. `multicol-nested-010` 3500px → 0px. Gate: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol **188/455** (+2) · spanner-frag 12/13.

### Phase 14c: clear-001 (PERMANENTLY DEFERRED, 2026-04-26)

Root cause: orange=95 rows is physically impossible with any consistent standard paint algorithm on a 96px element. The reference uses macOS Chrome CoreText font metrics (Times New Roman 18.5px line-box) producing an analytically unexplained paint asymmetry. No targeted fix exists.

---

## Phase 15: PercentageResolutionBlockSize (PARTIAL, 2026-04-26, not yet committed)

Cluster: `multicol-span-all-children-height-001` through `-013` (13 tests). Root cause: percentage-height children of a multicol container with explicit height were resolving against the column height instead of the container's content-box height.

Four fix sites identified and implemented:
1. `createConstraintSpaceForColumn`: `SetPercentageResolutionSize` now uses `containerPercentResolutionBlockSize` (container content-box height) instead of `colBlockSize`.
2. `resolveColumnAutoBlockSize`: when container has explicit height, balance measurement uses `AvailableSize = containerHeight + IsBlockSizeOverride + IsFixedBlockSize` so percentage heights resolve during estimation.
3. `layoutSpanner`: `AvailableSize.BlockSize` and `PercentageResolutionSize.BlockSize` both set to `containerPercentResolutionBlockSize`.
4. `childPercResolutionBlockSize` (block_layout.go): `IsBlockSizeOverride && isAnonymous` branch returns container height from `PercentageResolutionSize.BlockSize`.

New field: `containerPercentResolutionBlockSize float64` on `MulticolLayoutAlgorithm`. Set in `Layout()` from `explicitBlockSize` (content-box) when `hasExplicitBlock`, else `Indefinite`.

**Gate verified:** CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 (no regressions from these changes). multicol 190/455 (+2: tests 001 + one cascade gain).

**Test 001** (`multicol-span-all-children-height-001`): PASS at 0 diff.

**Tests 002–013**: heterogeneous cluster (7 sub-clusters with different root causes). Deferred to Phase 19 — see task_plan.md and findings.md § Phase 19 brief for sub-cluster decomposition.

**Decision:** commit Phase 15 partial (gate +2) before proceeding to Phase 16.

---

## Phase 16-19 plan (research complete 2026-04-26, briefs in findings.md)

Detailed Blink-parity analysis for the next four phases now lives in `findings.md` § "Phase 16+ Blink research briefs". Each brief includes Blink source citations (file:line), our current code state, implementation plan, test driver order, and tractability rating.

| Phase | Target | Tests | Tract. | Brief location |
|---|---|---|---|---|
| 16 | Spanner BFC filtering | ~6 | Easy | `findings.md` § Phase 16 brief |
| 17 | Forced-break balance (Blink ContentRuns/DistributeImplicitBreaks) | ~5 | Med | `findings.md` § Phase 17 brief |
| 18 | Nested multicol MulticolBreakTokenData row-carry | ~15 | Hard | `findings.md` § Phase 18 brief |
| 19 | span-all-children-height 002-013 (7 sub-clusters) | 12 | Mixed | `findings.md` § Phase 19 brief |

Total addressable: ~38 tests across Phases 16-19.

---

## Open residuals

### css-multicol F2 phase 2 (OPEN)

`multicol-nested-010` cluster (~7 tests). Nested leaf fragment spans inner sub-cols 1+2 instead of sub-col 1 only (continuation across outer col boundary). Requires Blink research before coding.

### css-multicol F3 residuals (OPEN, 19 tests)

`column-height-013` (6500px), `column-wrap-no-constraints-002` (6000px), `column-height-006` (5250px), nowrap cluster (~5000px each). `column-wrap:nowrap` overflow needs paint-layer change. `column-height-024` class needs live-Blink build trace.

### css-multicol F4 regressions — CLOSED (commit `87d06be5`, Phase 14a)

`multicol-inherit-001`, `multicol-margin-001`, `multicol-margin-child-001`, `multicol-nested-margin-001` all pass.

### css-multicol `multicol-rule-stacking-001` (OPEN, 32px)

Near-pass; column count now correct. Small rule geometry residual.

### css-position residuals (13 tests, no active work)

8 G-ABS-IN-TABLE, 3 G-SEMI-REPLACED, `clear-001` (permanently deferred), `containing-block-change-scrollframe` (needs JS scrollTop + overflow scroll paint).

### css-flexbox residuals (3 tests, no active work)

`auto-margins-001` (VRL cross-axis auto-margin), `content-height-with-scrollbars` (classicScrollbarWidth=0), `flexbox-align-self-vert-004` (column-direction baseline synthesis). Blink research done 2026-04-21; see task_plan.md Phase 11.

---

## Error Log

*(Add entries as failures are diagnosed — format: date, symptom, root cause, fix or status)*
