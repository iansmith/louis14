# Findings & Decisions — css-multicol (active) → fragmentation fixes

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`.

## Archived wm work
All writing-modes findings moved to `docs/findings-wm.md`. Do not duplicate here.

---

## Completed phases (summary)

**css-position (Phases 0–11, DONE 2026-04-21).** 92/105 passing; 13 pre-existing residuals (8 G-ABS-IN-TABLE, 3 G-SEMI-REPLACED, `clear-001`, `containing-block-change-scrollframe`). All code in git; commits range from `d174049b` (Phase 1) through `a22cfe10` + paint-phase refactor. No active work on css-position.

**Phase 13: LayoutUnit precision discipline (CLOSED 2026-04-26).** 13a–13h all landed. New `pkg/geometry/layoutunit` package (scalar `LayoutUnit{raw int32}`, 6 frac bits, saturating arithmetic, explicit rounding constructors). `pkg/geometry` composites (`LogicalOffset/Size/Rect`, `PhysicalOffset/Size/Rect`, `WritingModeConverter`). Every `PhysicalFragment` coordinate field and every plan-named `ConstraintSpace`/`BlockBreakToken`/`ExclusionSpace` precision field migrated to LayoutUnit. Length/percentage resolution flows through `ResolvePercent`. Text-shaping boundary: `MeasureText` returns `LayoutUnit`, `ShapeAdvances` returns `ShapeCumulative`. Paint-time `SnapSizeToPixel` + `SnapSizeToPixelAllowingZero` added to geometry package (Blink `>4-raw` thin-line clause). Key commits: `3897b43e` (13a), `20f25053` (13b), `6e689d8e`/`4dc4ac0b`/`912c03fa` (13c), `7d64570a`–`7db1f2fd` (13d), `ff45432a` (13e), `c5c9b67c`/`76ef4cb4` (13f), `050cf822` (13g.2), `776ae6d5` (13g.1). Gate at close: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 179/455. Open follow-ups: `drawColumnRules` content-area origin at `render.go:2931-2933` (not migrated); `clear-001` (permanently deferred — see Phase 14c).

**Phase 14a: IFC fragmentation guard (DONE 2026-04-26, commit `87d06be5`).** Three-part fix: (1) `inline_layout.go:963` `blockOffset > 0` → `fragmentainerOffset+blockOffset > 0`; (2) `block_layout.go` `collapseThrough` requires `childResult.BreakToken == nil`; (3) `block_layout.go` else-if for 0-height IFC break. Closed 4 F4 regressions. multicol 179 → 186.

**Phase 14b: Nested multicol leaf-frag (DONE 2026-04-26).** `multicol-nested-010.html` failed because inner multicol was being partially laid out in an outer column with only 20px remaining, instead of being deferred entirely. Fix: when nested `column-fill:auto` + explicit height + insufficient outer space + fresh layout, return a 0-height fragment with `BlockSizeForFragmentation = explicitBlockSize + BP`. `fragmentation_utils.go` `BreakBeforeChildIfNeeded` breaks before when `BlockSizeForFragmentation > spaceLeft`. Extended `collapseThrough` guard with `BlockSizeForFragmentation == 0`. multicol 186 → 188. Gate: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 188/455.

**Phase 14c: clear-001 (PERMANENTLY DEFERRED 2026-04-26).** Root cause fully traced: orange=95 rows is physically impossible to achieve with any consistent standard paint algorithm applied to a 96px element. The reference was generated with macOS Chrome CoreText font metrics (Times New Roman giving 18.5px line-box height) plus an analytically unexplained paint asymmetry for the clear element. No targeted fix exists without matching macOS CoreText metrics.

---

## Key Blink multicol findings (summary)

`ColumnLayoutAlgorithm` (`column_layout_algorithm.{h,cc}`, ~2124 lines) is the sole NG multicol algorithm. It produces `kColumnBox` fragments (columns) and spanner fragments as children of the multicol container fragment. The legacy `LayoutMultiColumnFlowThread`/`MultiColumnFragmentainerGroup` etc. have been entirely removed from Blink.

**Core loop structure.** `Layout()` calls `LayoutChildren()` which drives `LayoutFragmentationContext()` (one column row) in a do-while loop that advances past each `ColumnSpannerPath`. `LayoutFragmentationContext` drives `LayoutLine()` in a row-wrap do-while. `LayoutLine()` contains the outer stretch loop (`do { ... } while (true)`) and the inner per-column loop. The per-column loop produces columns via `BlockLayoutAlgorithm` with `SetBoxType(kColumnBox)`, threading `column_break_token` via `params.break_token` for each successive column. `MinimalSpaceShortage` is collected per-column via `UpdateMinimalSpaceShortage`, and the outer stretch loop grows `column_size.block` by `max(0, minimal_space_shortage)` until either acceptance or `ConstrainColumnBlockSize` caps it. Acceptance condition: `!has_violating_break && actual_column_count <= used_column_count && (!column_break_token || spanner_hit)`.

**ColumnSpannerPath** is a GC'd linked list from multicol container to innermost spanner. Built bottom-up by `BlockLayoutAlgorithm` when it encounters `child.IsColumnSpanAll()`. `LayoutChildren` dispatches via `GetSpannerFromPath(path)` + `walker.MoveToSpanner(spanner_node, next_column_token)`. `LayoutSpanner()` (cla.cc:1397-1522) runs the spanner with the full multicol container inline-size, commits via `AddResult(*result, offset)`, then calls `PropagateBaselineFromChild()`. After a spanner, `LayoutFragmentationContext` re-enters `LayoutLine` with the stored `next_column_token` to balance the next column row independently.

**GapGeometry.** Column rules are no longer painted by `ColumnRulePainter`; they are unified gap decorations via `GapDecorationsPainter`. `ColumnLayoutAlgorithm` builds a `GapGeometry` of type `kMultiColumn` (`cross_gaps_`, `main_gaps_`, `content_inline_offsets_`, `content_block_offsets_`, `columns_per_row_`) and stores it via `container_builder_.SetGapGeometry(...)`. `UpdateCrossGapSegmentStates()` flags cross-gaps as blocked/empty/flanked based on spanner adjacency.

**PropagateBaselineFromChild** (cla.cc:1655-1677) collects the multicol baseline: `first_baseline = min(block_offset + fragment.FirstBaseline())` across all column and spanner commits. `SetUseLastBaselineForInlineBaseline()` always called.

**UnpositionedListMarker protocol.** A list marker whose `outside` box hasn't found a baseline is carried as `UnpositionedListMarker` on the builder. Four callsites in `ColumnLayoutAlgorithm`: (1) constructor (inherits from parent if multicol is inside list-item); (2) after each column row's first column (column-to-baseline alignment); (3) after spanner commit (spanner may claim unclaimed marker); (4) `PositionAnyUnclaimedListMarker` at Layout() end (fallback against multicol container).

**Break token slot layout.** `BlockBreakToken` carries `ChildBreakTokens` for each descendant that was mid-layout. `IsCausedByColumnSpanner()` flag on outgoing break tokens tells the resumed `BlockLayoutAlgorithm` to suppress `discard_margins` (so post-spanner trailing inline items aren't swallowed). `InlineBreakToken` carries both `start_.item_index` AND `start_.text_offset`; both must be checked for non-zero to determine whether to resume.

**Nested fragmentation shortage propagation.** When nested inside balanced columns (`IsInsideBalancedColumns()`) and not in the initial balancing pass, `container_builder_.PropagateSpaceShortage(minimal_space_shortage)` propagates outward. Outer's stretch loop then grows both outer and inner columns. `available_outer_space = max(minimum_column_block_size, FragmentainerSpaceLeftForChildren() - line_offset)` caps inner columns.

**Row-wrap (column-height / column-wrap).** CSS Multicol L2 §4.2. Five consumption sites in cla.cc: (1) `LayoutLine` block-size override when `HasRowHeight()` (cla.cc:858-875); (2) row-wrap loop in `LayoutFragmentationContext` advancing `line_offset += RowHeight()` (cla.cc:789-836); (3) `ConstrainColumnBlockSize` clamps stretch candidate by `RemainingRowHeightAtOffset()` (cla.cc:1974-1977); (4) intrinsic block-size top-off when non-auto column-height (cla.cc:342-356); (5) `MulticolBreakTokenData{consumed_row_block_size}` for outer-fragmentainer row-carry (cla.cc:2087-2093). Our implementation: sites 1-4 ported in Phase 12f. Site 5 (`MulticolBreakTokenData` row-carry) deferred — safe default is `consumedRowBlockSize=0`.

---

## Phase 12: css-multicol — PARTIAL (188/455, 267 failing)

Entry baseline: 94/458 (2026-04-21). Current: 188/455. Active track.

### Completed sub-phases

| Sub-phase | Commit | Net gain | Notes |
|---|---|---|---|
| 12a | `2a0d0a07` | +1 | Blink-parity fragmentation infra: LayoutLine outer stretch loop, BlockBreakToken threading, shortage reporting, ResolveColumnAutoBlockSize |
| 12b | `931f48c5` | +13 | All 13 spanner-fragmentation-* tests pass; ColumnSpannerPath, LayoutSpanner, spanner-resume |
| 12c | `cccbd05e`+`b0825367` | +22 | Nested multicol: initial-balancing override guard, PropagateSpaceShortage, resume-break emission |
| 12d | `6483bc7d` | +2 | Forced-break + break-inside:avoid-column dispatch |
| 12e | bundled | +1 | max-height-imposes-on-columns (multicol-fill-auto-block-children-003) |
| 12f | `35ce3dda` | +6 | column-height + column-wrap: 5 cla.cc sites, row-gap plumbing |
| 12g | `287c9fb3` | +3 | balance-break-avoidance: break-appeal propagation + MinSpaceShortage for soft-break path |
| 12h step 1 | `356a8b19` | +2 | Ahem font loader (font URL → direct file path) |
| 12h F3a-F3e | multiple | +14 | row-gap, columns shorthand, zero-frag, abspos aggregation, spanner row-advance, Blink-parity row-snap |
| 12h F4 | `617332ae` | +8 net | InlineBreakToken resume: gate on `(item_index>0 || text_offset>0)` |
| 12h F5 | separate | +3 | Continuation-row terminal-shortage detection (list-item-003/004/005) |
| 12h F2 partial | separate | +1 | ClipBlockAxisOnly: block-axis-only clip on column fragmentainers |
| 12h F1 | `41b674ef`+mazzy | +2 wm | @font-face layout-time provider registration + bidi-level shape segmentation |
| 12h.6 | 3 commits | +9 | multicol 179→188; spanner row-gap sequencing, abspos-in-multicol, inline-IFC guard |

### Phase 12 open residuals

**F2 second root cause (OPEN).** `multicol-nested-010` cluster (~7 tests): nested multicol leaf fragment is split across inner sub-cols instead of placed only in inner sub-col 1's continuation. Blink places the leaf only in sub-col 1's continuation across the outer column boundary (20px in col-1, 80px in col-2, all in inner sub-col 1). Fix site: inner-multicol placement around child-break-token forwarding in nested multicol.

**F3 residuals (OPEN, 19 tests).** Largest: `column-height-013` (6500px), `column-wrap-no-constraints-002` (6000px), `column-height-006` (5250px), nowrap cluster (`column-height-005/011/030` ~5000px each). `column-wrap:nowrap` overflow requires paint-layer change to allow overflow columns past the declared border-box. `column-height-024` class needs live-Blink build trace.

**F4 regressions — 4 margin-family tests (OPEN).** `multicol-inherit-001`, `multicol-margin-001`, `multicol-margin-child-001`, `multicol-nested-margin-001`. Root: outer `block_layout.go` doesn't break-before an anonymous block when it overflows the column; instead emits a mid-text InlineBreakToken; post-F4 the resume correctly picks up the partial, revealing previously-hidden content. Real fix: break-before the anon block entirely when it won't fit.

**`multicol-rule-stacking-001` near-pass (OPEN).** 32px diff after F4. Column count correct; small rule geometry difference remains.

---

## Phase 14b research: nested leaf-frag — Blink citations

The fix defers the inner multicol to the next outer column via `BlockSizeForFragmentation`. Key Blink citations:

- `LayoutResult::BlockSizeForFragmentation()` (`layout_result.h`) — "I need this much space." When set and exceeds fragmentainer space, `BreakBeforeChildIfNeeded` (`fragmentation_utils.cc`) returns `BreakStatusBrokeBefore`.
- `block_layout_algorithm.cc` — when `BlockSizeForFragmentation > spaceLeft` and there's container separation (a child already placed, or block-start BP consumed), the block algorithm breaks before the child.

---

## Deferred / out-of-scope items

| Item | Status | Notes |
|---|---|---|
| `clear-001.xht` (96px) | PERMANENTLY DEFERRED | CoreText font metrics mismatch; no targeted fix |
| `column-wrap:nowrap` overflow columns | DEFERRED | Needs paint-layer change to allow ink-overflow past border-box |
| `MulticolBreakTokenData` row-carry (Site 5) | DEFERRED | `consumedRowBlockSize=0` is safe default |
| `drawColumnRules` content-area origin `render.go:2931-2933` | DEFERRED | math.Round not migrated to SnapSizeToPixel |
| F1c paint-side shape sharing | DEFERRED | `ShapeResult::CopyRange`; needed for cross-span kerning on non-level-0 items |
| `openFont` signature cleanup | DEFERRED | `FontPathToFamilyVariant` + `resolveFamily` path fallback still present |
| G-ABS-IN-TABLE (8 tests) | DEFERRED | abspos children of positioned thead/tbody/tfoot/tr |
| G-SEMI-REPLACED (3 tests) | DEFERRED | abspos stretch on button/input/other |
| G-SCROLL (1 test) | DEFERRED | Needs `Element.scrollTop` JS setter + overflow:hidden scroll paint |
| spanner-fragmentation-005 | DEFERRED | Pre-existing residual since Phase 12b |
| css-flexbox 11a/11b/11c | DEFERRED | Three pre-existing failures at 626/629 |
| `column-height-024` class | DEFERRED | Needs live-Blink build trace |
| Anchor positioning | OUT OF SCOPE | No WPT tests exercise it |

---

## Full ColumnLayoutAlgorithm pseudocode (Blink-parity reference)

```
ColumnLayoutAlgorithm::Layout():                                 // cla.cc:266
  row_gap_size       = ResolveRowGapForMulticol(style, avail.block)
  used_column_count  = ResolveUsedColumnCount(style, avail.inline)
  combined_col_isize = avail.inline - gap_sum_within_content_box
  inline_stride      = combined_col_isize + gap_sum_until_overflow
  is_constrained_by_outer = space.HasKnownFragmentainerBlockSize()
  container_builder.SetIsBlockFragmentationContextRoot()
  intrinsic_block_size = BorderScrollbarPadding.block_start

  status = LayoutChildren()   // drives LayoutFragmentationContext → LayoutLine
  if status == kNeedsEarlierBreak:
      return RelayoutAndBreakEarlier<ColumnLayoutAlgorithm>(early_break)

  if non-auto column-height:
      intrinsic_block_size += clamp(RemainingRowHeightAtOffset(...), 0, outer_left)
  intrinsic_block_size += BorderScrollbarPadding.block_end

  block_size = ComputeBlockSizeForFragment(...)
  if nested: FinishFragmentation(container_builder)
  if gap_rule: build GapGeometry; container_builder.SetGapGeometry(...)
  return container_builder.ToBoxFragment()

LayoutLine(next_column_token, line_offset, ...):                 // cla.cc:858
  column_size.block = initial-from-column-height-or-remaining
  balance_columns   = (column-fill:balance) or (nested and outer in initial balancing pass)
  if balance_columns or indefinite:
      column_size.block = ResolveColumnAutoBlockSize(...)

  do:                                 // outer stretch loop
      minimal_space_shortage = kIndefiniteSize
      column_break_token     = next_column_token
      actual_column_count    = 0
      has_violating_break    = false

      do:                             // inner per-column loop
          child_space = CreateConstraintSpaceForFragmentainer(
              parent_space, kFragmentColumn, column_size, ...)
          result = BlockLayoutAlgorithm(params).Layout()  // kColumnBox
          UpdateMinimalSpaceShortage(result.MinimalSpaceShortage(), ...)
          if result.GetColumnSpannerPath(): break
          has_violating_break |= result.GetBreakAppeal() != kBreakAppealPerfect
          column_break_token = result.fragment.GetBreakToken()
          if column_break_token and actual_column_count >= used_column_count: break
      while column_break_token

      if not balance_columns: break

      accepted = !has_violating_break
                 && actual_column_count <= used_column_count
                 && (!column_break_token || spanner_hit)
      if accepted: break

      new_col_bsize = column_size.block + max(0, minimal_space_shortage)
      new_col_bsize = ConstrainColumnBlockSize(new_col_bsize, ...)
      if new_col_bsize <= column_size.block:
          if IsInsideBalancedColumns and not InitialPass:
              container_builder.PropagateSpaceShortage(minimal_space_shortage)
          break
      column_size.block = new_col_bsize
  while true

  for result_with_offset in new_columns:
      container_builder.AddChild(column, offset)
      PropagateBaselineFromChild(column, offset.block)
```

## Key data structures (reference)

| Name | Role |
|---|---|
| `ColumnLayoutAlgorithm` | The sole multicol algorithm; produces column + spanner fragments |
| `BlockLayoutAlgorithm` (kColumnBox) | Per-column layout; reports shortage, forced-break, spanner-path, break-appeal |
| `BlockBreakToken` | Continuation handle; `ConsumedBlockSize`, `IsBreakBefore`, `IsForcedBreak`, `IsCausedByColumnSpanner`, child tokens, `BreakTokenAlgorithmData` |
| `ConstraintSpace` multicol flags | `BlockFragmentationType`, `HasKnownFragmentainerBlockSize`, `IsInitialColumnBalancingPass`, `IsInsideBalancedColumns`, `IsInColumnBfc`, `MinBreakAppeal`, `FragmentainerOffset`, `FragmentainerBlockSize` |
| `ColumnSpannerPath` | GC'd linked list to first spanner |
| `MulticolBreakTokenData` | `LayoutUnit consumed_row_block_size` (row-carry across outer fragmentainers) |
| `GapGeometry` + `MainGap`/`CrossGap` | Gap decoration geometry for painting |
| `BreakAppeal` | `LastResort < ViolatingOrphansAndWidows < ViolatingBreakAvoid < Perfect` |

## Error Log

*(Add entries as failures are diagnosed — format: date, symptom, root cause, fix or status)*
