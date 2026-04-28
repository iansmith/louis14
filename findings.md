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

**F4 regressions — CLOSED by Phase 14a (commit `87d06be5`).** `multicol-inherit-001`, `multicol-margin-001`, `multicol-margin-child-001`, `multicol-nested-margin-001` all pass. Root was `IFC fragmentainerOffset` guard (inline_layout.go:963) and `collapseThrough` requiring `BreakToken == nil`.

**`multicol-rule-stacking-001` near-pass (OPEN).** 32px diff after F4. Column count correct; small rule geometry difference remains.

---

## Phase 15 research: PercentageResolutionBlockSize for multicol children

**Root cause.** Three interconnected bugs caused percentage-height children of a multicol container with explicit height to resolve against the wrong base:

1. **`createConstraintSpaceForColumn`**: `SetPercentageResolutionSize.BlockSize` was set to `colBlockSize` (the column height). CSS spec §11.1.1 and Blink both use the *containing block's* content-box height. Fix: use `containerPercentResolutionBlockSize`.

2. **`resolveColumnAutoBlockSize` balance estimate**: With `AvailableSize = Indefinite`, `ResolveBlockSize`'s `!IsBlockSizeIndefinite()` gate (checks `AvailableSize.BlockSize < 0`) returned true → all percentage heights resolved to auto → balance estimate was text-height only (~6px) instead of the correct value. Fix: when container has explicit height, set `AvailableSize = containerHeight` + `IsBlockSizeOverride + IsFixedBlockSize`. Cannot also set `IsContentSuggestionLayout` because it's checked first in `CalculateInitialFragmentGeometry` and suppresses the override.

3. **`layoutSpanner`**: Spanner's constraint space had `AvailableSize = Indefinite` and `PercentageResolutionSize = 0`. With Indefinite AvailableSize, `ResolveBlockSize` returns auto for any `height: X%` on the spanner. Fix: set both `AvailableSize.BlockSize` and `PercentageResolutionSize.BlockSize` to `containerPercentResolutionBlockSize`.

4. **`childPercResolutionBlockSize` (block_layout.go)**: When the anonymous multicol content block is laid out with `IsBlockSizeOverride = true`, `CalculateInitialFragmentGeometry` sets `borderBoxBlock = AvailableSize.BlockSize = colBlockSize`. The old code returned `explicitBlockSize = colBlockSize` for children. Fix: when `IsBlockSizeOverride && isAnonymous`, read `PercentageResolutionSize.BlockSize` from the space (which is now `containerPercentResolutionBlockSize`) and return that.

**`ResolveBlockSize` percentage gate.** Uses `!space.IsBlockSizeIndefinite()` which tests `AvailableSize.BlockSize < 0`. Attempted to change this to check `PercentageResolutionSize.BlockSize >= 0` but caused flex regressions: flex_layout.go sets `PercentageResolutionSize.BlockSize = 0` for indefinite cross-size, and `0 >= 0 = true` would resolve percentages against 0px incorrectly. Reverted.

**New struct field.** `containerPercentResolutionBlockSize float64` on `MulticolLayoutAlgorithm`. Set in `Layout()` to `explicitBlockSize` when `hasExplicitBlock`, else `Indefinite`. All call sites in the layout loop use `mla.containerPercentResolutionBlockSize` directly.

**Test 001 result.** `multicol-span-all-children-height-001`: PASS at 0 diff. block1=50%=100px, spanner=50%=100px, block2=50%=100px in a 200px article.

**Tests 002–013.** Still failing. Mix of different failure categories (see task_plan.md Phase 15). Investigation ongoing.

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

---

## Phase 16+ Blink research briefs (2026-04-26)

The next three phases are scoped from end-to-end Blink research. Each brief below is detailed enough to drive implementation directly. Source citations are absolute Chromium paths.

### Phase 16 brief — Spanner BFC filtering (target T1, originally ~6 tests)

**Status (2026-04-27).** Phase 16 BFC-filtering committed as `d42e3cf2` (+2: `-002, -004` PASS). Phase 16 also got an uncommitted nested-spanner-propagation patch (block_layout.go ~668-737, multicol_layout.go ~592-609 + spannerLeafNode helper) and inner-loop reorder (multicol_layout.go ~1100-1136). The four remaining tests (`-005, -006, -007, -008`) **share a single new root cause** — pre-spanner column row block-advance is pinned to colBlockSize, clipping monolithic content. Detailed continuation plan in **§ Phase 16 continuation (post-d42e3cf2)** below.

**Failing tests.** `multicol-span-all-002, -004, -005, -006, -007, -008`. All check that `column-span: all` is *ignored* when an ancestor between the candidate and the multicol container blocks spanners (different BFC, monolithic, table, fixed-pos-container).

**Blink predicate chain.** `LayoutBox::IsColumnSpanAll()` returns true only when:

```
column-span == all
AND IsValidColumnSpannerInTree()
     = IsInsideMulticol()
       AND IsSelfValidColumnSpanner()       // candidate-side check
       AND DoesAncestryAllowColumnSpanner() // ancestor walk
```

Source files:
- `third_party/blink/renderer/core/layout/layout_box.h:2356-2362` — `IsColumnSpanAll()`
- `third_party/blink/renderer/core/layout/layout_box.cc:2956-2966` — `IsValidColumnSpannerInTree`
- `third_party/blink/renderer/core/layout/layout_box.cc:2968-2985` — `IsSelfValidColumnSpanner` (candidate side)
- `third_party/blink/renderer/core/layout/layout_box.cc:2987-3001` — `DoesAncestryAllowColumnSpanner` (the walk)
- `third_party/blink/renderer/core/layout/layout_box.cc:3003-3030` — `ShouldPreventColumnSpannerDescendants` (per-ancestor blocker)

**Candidate-side check (`IsSelfValidColumnSpanner`).** Disqualifies if:
- `column-span` ≠ `all`
- `ShouldBeHandledAsInline(style)` (display: inline / inline-block contextually)
- `ShouldBeHandledAsFloating(style)` (float ≠ none and not also out-of-flow)
- `ToPositionedState(style.GetPosition()) == kIsOutOfFlowPositioned` (position: absolute|fixed)

**Ancestor walk (`DoesAncestryAllowColumnSpanner`).** Walks `Parent()->EnclosingBox()` upward via `ContainingBlock()` (not parent chain — *containing block* chain). For each ancestor:
- If `IsMulticolContainer()` → return true.
- If `ShouldPreventColumnSpannerDescendants()` → return false.
- Otherwise continue walking.

**Per-ancestor blocker (`ShouldPreventColumnSpannerDescendants`).** Returns true when:
1. The ancestor is itself a `column-span:all` spanner (no nested spanners in same multicol context).
2. The ancestor is not a `LayoutBlockFlow` (tables, table-rows, table-cells, table-captions, buttons, fieldsets, list-item-markers).
3. `block_flow->IsMonolithic()` — inline, semi-replaced, unsplittable scrolling overflow, writing-mode roots, printed fixed-position under LayoutView, size-containment, `<frameset>`, `line-clamp`, scroll-marker-group.
4. `block_flow->CreatesNewFormattingContext()` — `inline-block`, `inline-flex`, `inline-grid`, flex/grid items, `flow-root`, `overflow ≠ visible`, columns themselves, `contain: layout|paint|content`, multicol-flow-thread roots, MathML.
5. `block_flow->CanContainFixedPositionObjects()` — transforms, `will-change: transform`, `filter`, `backdrop-filter`, `contain: paint|layout`.

**Louis14 current state — single classification site.** `pkg/layout/block_layout.go:377-379`:

```go
if bla.space.HasBlockFragmentation &&
    bla.space.BlockFragmentationType == FragmentColumn &&
    childStyle.GetColumnSpan() == "all" {
```

This implements only `column-span == all && IsInsideMulticol()`. Both `IsSelfValidColumnSpanner` and `DoesAncestryAllowColumnSpanner` are entirely missing.

**Reusable helpers already in louis14.**
- `pkg/layout/block_layout.go:2030-2090` — `createsFormattingContext()` already covers `display: flow-root`, `float`, `position: absolute|fixed`, `overflow ≠ visible` (with body propagation rule), `inline-block`, flex variants, grid variants, `display: table`/`inline-table`, layout/paint containment. **Reusable as-is for §3 condition C-4.** Missing: transforms / `will-change: transform` / `filter` (Blink's `CanContainFixedPositionObjects`).
- `pkg/layout/layout_result.go:115-132` — `ColumnSpannerPath` type. Mirrors Blink's `column_spanner_path.h`. **No changes needed.**
- `pkg/layout/multicol_layout.go:559, 568, 1097, 1160, 1259, 1287-1306` — multiple consumers of `result.ColumnSpannerPath`. Pure consumers; trust the path. **No changes needed.**

**Implementation plan.** Add two helpers beside `createsFormattingContext` in `block_layout.go`, then gate the existing classification site:

```go
// Mirrors Blink LayoutBox::IsSelfValidColumnSpanner (layout_box.cc:2968).
// Disqualifies inline / float / out-of-flow candidates.
func isSelfValidColumnSpanner(style *css.Style) bool { ... }

// Mirrors Blink LayoutBox::DoesAncestryAllowColumnSpanner +
// ShouldPreventColumnSpannerDescendants (layout_box.cc:2987 / :3003).
// Walks containing-block chain from `child` toward the multicol container,
// returning false if any ancestor: is itself a spanner, is not a block-flow
// (tables/buttons/fieldsets/captions), creates a new BFC, has a transform/
// will-change/filter, or is monolithic.
func ancestorAllowsColumnSpanner(child, multicol *LayoutInputNode) bool { ... }
```

Then replace the gate at `block_layout.go:377-379`:

```go
if bla.space.HasBlockFragmentation &&
    bla.space.BlockFragmentationType == FragmentColumn &&
    isSelfValidColumnSpanner(childStyle) &&
    ancestorAllowsColumnSpanner(child, bla.node) {
```

**Test order.** `-004` (inline-block, overflow:hidden, grid, flex parents — exercises every CreatesNewFormattingContext case) → `-005` (display:grid/flex on the spanner itself — adds candidate-side `IsSelfValidColumnSpanner` requirement) → `-002` (table-caption — non-block-flow ancestor) → `-006`/`-007`/`-008` (details/body-as-multicol/bidi corner cases — should pass once over-eager activation stops).

**Tractability.** Easy. Single fix site. Two helpers. ~80 lines. Existing `createsFormattingContext` does most of the work.

---

### Phase 16 continuation (post-d42e3cf2) — Pre-spanner column-row block-advance

**Audience.** Sonnet (or anyone) following up on the four remaining tests `-005, -006, -007, -008`. Read this entire section before touching any code. The previous Phase 16 brief above only fixed BFC filtering; the failures here have an **independent** root cause.

#### Diagnostic data (collected 2026-04-27 via temporary `println` in `multicol_layout.go:1108`)

For `multicol-span-all-008.html` — `<article column-count:3 width:400>` containing `<div>block1</div>`, `<h3 dir=rtl column-span:all>spanner</h3>`, `<div>block2</div>`. The body BLA reports per-column for the **pre-spanner row's outer-stretch first iteration**:

```
col=0: colBlockSize=6  colHeight=6  shortage=10.9375  appeal=Perfect(3)  spanner=false  intrinsic=16.9375
col=1: colBlockSize=6  colHeight=6  shortage=0        appeal=Perfect(3)  spanner=true   intrinsic=0
```

Then the post-spanner row is balanced separately (a second `layoutLine` invocation):
```
col=0: shortage=10.94  spanner=false  intrinsic=16.94    ← stretch fires here
col=1: shortage=0      spanner=false  intrinsic=0
[stretch loop iteration 2]
col=0: colBlockSize=16.9375  colHeight=16.9375  shortage=0
[accepted]
```

**Two facts the trace pins down:**

1. **Acceptance short-circuits when `lastHasSpanner=true`** even though col=0 reported `shortage=10.94`. The pre-spanner row is committed at `colBlockSize=6`. The post-spanner row stretches correctly because `lastHasSpanner=false` there.
2. **`BreakAppeal` stays `Perfect`** in col=0. The body BLA accepts block1's monolithic overflow at `block_layout.go:996-1007` (overflow path: `blockCursor=16.94 > fragEnd=6 → shortage=10.94 set, but no `worstAppeal` degradation because no `break-inside:avoid`).

#### What this means for the four failing tests

All four tests fail with the **same shape**: the pre-spanner column row gets `colBlockSize=ceil(intrinsic/numCols)` (≈6px when intrinsic≈16.94 and numCols=3), `IsBlockSizeOverride+IsFixedBlockSize` pins each column fragment to that height, and `ClipBlockAxisOnly` (set in `multicol_layout.go:1085`) then clips paint to that height. Pre-spanner content (`block1`, `summary`, etc.) renders as a tiny vertical sliver. The spanner and post-spanner content render correctly because the post-spanner row balances independently and its stretch loop is not short-circuited.

| Test | Distinguishing detail | Common root cause |
|---|---|---|
| -005 | 5 cases stacked: table/grid/flex/fieldset/details as direct-child spanners. Case 1 (table) renders correctly; cases 2-5 (grid/flex/fieldset/details) all show "block1" sliver. | Same as -008. (Case 1 likely passes because table layout produces non-zero intrinsic min height for the row regardless of balance estimate.) |
| -006 | `<details open>` IS the multicol container. `<summary>`, `<h3 spanner>`, `<div>block</div>`. Summary renders as sliver. | Same. (No special details/summary issue — summary just behaves as the pre-spanner content in this case.) |
| -007 | Spanner is `<h3>` wrapped in `<div>` (grandchild of multicol body). Nested propagation already applied. Block1 still renders as sliver. | Same. |
| -008 | Spanner is direct child + bidi-override + dir=rtl. Block1 renders as sliver. | Same. |

#### Blink reference — what we need to mirror

`column_layout_algorithm.cc` lines 1300-1370 (search "max_column_height" / "row_block_size"). After laying out all columns of a row, Blink computes the **row's block-advance** using each column's *intrinsic* (content-driven) block-size when content overflowed monolithically — **not** the pinned column fragment block-size. The relevant Blink call shape:

```cpp
LayoutUnit max_column_block_size;
for (const auto& column_result : column_results) {
  // BlockSizeForFragmentation = the size the column's CONTENT actually
  // consumed, including monolithic overflow that the fragment's own
  // block-size couldn't represent (because of fixed_block_size).
  LayoutUnit content_size = BlockSizeForFragmentation(
      *column_result, GetConstraintSpace().GetWritingDirection());
  max_column_block_size = std::max(max_column_block_size, content_size);
}
// Row advance = max content size, not max fragment size.
intrinsic_block_size += max_column_block_size;
```

`BlockSizeForFragmentation` (free function in `block_layout_algorithm_utils.cc`) returns the fragment's own block-size **PLUS** any monolithic overflow that didn't fit the fragmentainer constraint. For our case it would return 16.94 (block1's natural size), not 6 (the column fragment's pinned size).

In Blink, the column fragment **paints overflowing content visually** (no per-column paint-time clip — see `BoxFragmentPainter::PaintBlockChild` fragmentainer branch). So when the row advance uses 16.94, the spanner is placed at y=16.94 below block1's full visual extent. The reference renders correctly.

#### Louis14's three-part bug

| # | File:line | Current behaviour | What Blink does |
|---|---|---|---|
| 1 | `multicol_layout.go:1247` | `h := col.fragment.BlockSize()` — uses the pinned 6px column fragment size | Uses `BlockSizeForFragmentation(col.result)` — content-driven (16.94) |
| 2 | `multicol_layout.go:1299-1301` | Caps `maxColHeight` to `colBlockSize` (6px) — undoing any future fix to #1 | No such cap; row advance is allowed to exceed `column_size.block` when monolithic content overflowed |
| 3 | `multicol_layout.go:1085` (writes `colFrag.ClipBlockAxisOnly = true`) + paint `paint_layer.go:279` | Clips column fragmentainer paint to its block extent | No per-column paint clip |

Each bug compounds the others: #3 hides the issue visually even when #1+#2 produce correct row advance; #2 prevents #1 from advancing the row past the pinned fragment height; #1 alone yields the wrong number even without the cap.

#### Implementation plan — three sub-phases, in this order

**16.b.1 — Add `BlockSizeForFragmentation` field on `LayoutResult` (LayoutResult is the right carrier).**

Mirror Blink's `LayoutResult::BlockSizeForFragmentation()`. We already have a field with this name (`pkg/layout/layout_result.go:91-100`) but its current use is **Phase 14b's nested-multicol-defer signal**. Re-read its doc comment: "size the parent should use when deciding whether to break before this child... defaults to 0 (use the fragment's own block-size)". The semantic is exactly what Blink uses — we can reuse it directly. **Do not introduce a new field.**

In `block_layout.go`, populate `result.BlockSizeForFragmentation` whenever the BLA accepts monolithic overflow (i.e. the `block_layout.go:1007` overflow path AND the `block_layout.go:996` "child fit but overflowed" cases AND the spanner-early-returns at lines 423-432 + 668-737). The value to set is `max(blockCursor, fragmentBlockSize)` — the larger of "what content actually consumed" and "what the fragment is sized to".

Concretely, after `result := builder.Build()` in each of those return sites, before `return result`, add:
```go
if intrinsicBlock > NewLogicalFragment(wdm, result.Fragment).BlockSize() {
    result.BlockSizeForFragmentation = intrinsicBlock
}
```

(The intrinsic-block local variable is already in scope at all four sites.)

**Verify** by writing a temporary `println` in the multicol inner column loop printing `result.BlockSizeForFragmentation` and confirming col=0 of -008's pre-spanner row reports 16.94.

**16.b.2 — Use `BlockSizeForFragmentation` for row block-advance.**

In `multicol_layout.go:1247`, change:
```go
h := NewLogicalFragment(wdm, col.fragment).BlockSize()
```
to:
```go
h := NewLogicalFragment(wdm, col.fragment).BlockSize()
if col.result != nil && col.result.BlockSizeForFragmentation > h {
    h = col.result.BlockSizeForFragmentation
}
```

(`col.result` is already on the per-column anonymous struct — see `multicol_layout.go:1031, 1099`.)

Then **delete** the cap at `multicol_layout.go:1299-1301`:
```go
// Cap row advance to colBlockSize when finite: columns placed with
// fragmentation don't advance the cursor beyond the column height.
if colBlockSize != Indefinite && maxColHeight > colBlockSize {
    maxColHeight = colBlockSize
}
```

This cap was inserted to prevent runaway advances in fragmentation contexts where a child returned a fragment larger than colBlockSize (Phase 12a era). With #16.b.1's `BlockSizeForFragmentation` properly populated, the value naturally reflects content needs and shouldn't be capped.

**Risk surface for 16.b.2:** the cap-removal might regress tests where a column fragment legitimately exceeds colBlockSize for a reason OTHER than monolithic overflow. Run the full multicol suite after this step and compare against the 192/455 baseline. Likely candidates for regression: `multicol-fill-auto-*`, `column-height-*`, and the `multicol-nested-*` cluster.

**16.b.3 — Re-evaluate `ClipBlockAxisOnly` for column fragmentainers.**

Once 16.b.1 + 16.b.2 land, the row advance is correct (16.94 for -008's pre-spanner row), so the spanner is placed at y=16.94 and there's no visual overlap. But block1 inside col=0 is still positioned within a 6px-tall column fragment. With `ClipBlockAxisOnly` set on `colFrag` at `multicol_layout.go:1085`, the column fragment clips block1 to 6px — wrong.

Blink does not set any per-column paint clip (per the existing comment block at `multicol_layout.go:1067-1083`). Our `ClipBlockAxisOnly` was added in **Phase 12h F2 partial** specifically to clip overflow in the **outer column** of nested-multicol layout (`multicol-nested-010` 4500→3500px). It was never the right answer for general single-level columns.

**The fix:** narrow the condition for setting `ClipBlockAxisOnly`. It should only apply when the column has a NESTED multicol child whose own size demands clipping at the outer column boundary. Concretely:

```go
// Replace the unconditional set at multicol_layout.go:1083-1086:
skipBlockClip := colBlockSize == 0 && !mla.hasAutoColumnHeight()
if colBlockSize != Indefinite && !skipBlockClip {
    colFrag.ClipBlockAxisOnly = true
}
```

with a narrower predicate: only set `ClipBlockAxisOnly` when the column contains a nested multicol fragment whose own height is constrained by an explicit `column-height` or `height` AND that nested fragment exceeds the outer column. For now (16.b.3 minimal), simply **remove** the `ClipBlockAxisOnly = true` line entirely and re-check `multicol-nested-010` — it didn't pass with the clip and likely won't regress significantly without it. Document the removal as "Phase 12h F2 partial reverted; superseded by Blink-parity row advance in Phase 16.b.2".

**Risk surface for 16.b.3:** anything that relied on column-block-axis clipping. Search the test suite for tests that use `column-fill:auto` with leaf children taller than the column. Most are in the `multicol-fill-auto-*` and `column-height-*` clusters.

**16.b.4 — Revisit acceptance condition `lastHasSpanner` short-circuit (validation, not necessarily a fix).**

After 16.b.1-3, the `lastHasSpanner=true` branch in the acceptance condition (`multicol_layout.go:1187-1192`) might still need attention. Per Blink's pseudocode in this file (§ Full ColumnLayoutAlgorithm pseudocode line 184), the acceptance is:
```
accepted = !has_violating_break
           && actual_column_count <= used_column_count
           && (!column_break_token || spanner_hit)
```

If 16.b.1-3 fix the four tests without modifying acceptance, leave it alone. **Do not change the acceptance condition unless tests still fail after 16.b.1-3.** If they do, the most likely correct change is to make the BLA's overflow path at `block_layout.go:1007` set `worstAppeal = BreakAppealLastResort` when both `mustBreakBefore=true` (i.e. shortage > 0 because `blockCursor > fragEnd`) AND `refuseBreakBefore=true` (column was fresh, can't push child to next column). That mirrors Blink's `MovePastBreakpoint` returning a degraded appeal for last-resort overflow acceptance.

**Test order.**
1. After 16.b.1: confirm `BlockSizeForFragmentation=16.94` reaches the multicol inner loop for -008's pre-spanner col=0. No tests should change yet (consumer isn't using the field).
2. After 16.b.2: re-run `-005, -006, -007, -008`. Expected: spanner positioned at y=16.94. Visual overlap of block1 with column fragment gone, but block1 still clipped to 6px.
3. After 16.b.3: re-run `-005, -006, -007, -008`. Expected: all four PASS at 0 diff. Then run full multicol suite to find regressions.
4. If regressions exist: see 16.b.3 risk surface. May need to re-add narrowed ClipBlockAxisOnly.
5. If -005 case 1 (table) was the only one passing before and is now broken: investigate table layout's interaction with `BlockSizeForFragmentation` reporting.
6. If -006 still fails after 16.b.3: investigate `<details>/<summary>` UA element rendering separately (not in this sub-phase scope).

**Tractability.** Medium. Three small focused changes, but each touches a load-bearing spot. The `ClipBlockAxisOnly` removal is the riskiest — be prepared to narrow rather than remove.

**Gate target.** 192 → 196 (+4) if all four pass cleanly. Worst case 192 → 195 if -005 partially passes (table case stays passing, others pass).

**Anti-patterns to avoid.**

- **Don't add a new acceptance-bypass clause** like "if shortage > 0, override lastHasSpanner". The trace shows this would over-stretch; the issue isn't the stretch loop, it's row advance + paint clip.
- **Don't introduce `consumed_column_block_size` or similar new fields** unless 16.b.4 absolutely requires it. The existing `BlockSizeForFragmentation` is the right carrier.
- **Don't try to fix -006 by hacking the `<details>` UA stylesheet.** If -006 still fails after 16.b.3, the residual is a separate `<details>/<summary>` UA element issue and belongs in a different phase.
- **Don't run the full WPT sweep during sub-phase iteration.** Per CLAUDE.md, only run the 1-4 driver tests; gate-sweep before commit.

---

### Phase 16.c brief (post-a375cb45) — Remove ClipBlockAxisOnly + port column regrowth

**Audience.** Anyone following up on the ~25 multicol regressions left by Phase 16.b. Read this section before touching code. Phase 16.b shipped the row-advance fix that made `-006/-007/-008` pass, but it kept `ClipBlockAxisOnly` (just narrowed) and hit a regression cluster across `column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`, etc. (multicol gate 192 → 167; spanner-fragmentation 12/13 → 7/13.)

#### Why the narrowed predicate failed

Phase 16.b kept `ClipBlockAxisOnly` set on most column fragmentainers via `shouldClip := col.result == nil || BSFF == 0 || !hasAutoColumnHeight()` (multicol_layout.go:1217-1232). The hypothesis was "narrow it to skip pre-spanner columns and explicit-column-height columns; keep clip everywhere else for safety." Reality: any predicate that keeps a per-column block-axis paint clip diverges from Blink, because Blink has **no per-column paint clip at all**. The narrowed predicate lit up regressions wherever a non-spanner column legitimately overflows colBlockSize (CSS Multicol L2 row-wrap, monolithic fragments, list-item markers, nested multicol carry-over) — the Phase 12h F2-partial workaround was never aligned with Blink's painter.

#### Authoritative Blink references (research 2026-04-27)

**1. `BoxFragmentPainter::PaintBlockChild` — fragmentainer branch has zero clip.**
File: `third_party/blink/renderer/core/paint/box_fragment_painter.cc:1080-1114`.

```cpp
if (box_child_fragment.IsFragmentainerBox()) {
  PhysicalOffset child_offset = paint_offset + child.offset;
  // ...display-item-scope comment...
  unsigned identifier = FragmentainerUniqueIdentifier(box_child_fragment);
  ScopedDisplayItemFragment scope(paint_info.context, identifier);
  BoxFragmentPainter(box_child_fragment)
      .PaintObject(paint_info, child_offset);
  return;
}
```

`ScopedDisplayItemFragment` is a paint-cache identifier, not a clip recorder. No `ClipBlock`, `BlockAxisClip`, `OverflowClip`, `BoxClipper`, or `ScopedPaintChunkProperties` wraps the fragmentainer paint. Same is true at the floating-children dispatch (~line 1195).

**2. `LayoutBox::ComputeOverflowClipAxes` — multicol containers default to `overflow:visible`.**
File: `third_party/blink/renderer/core/layout/layout_box.cc:4002-4016`.

```cpp
OverflowClipAxes LayoutBox::ComputeOverflowClipAxes() const {
  NOT_DESTROYED();
  if (ShouldApplyPaintContainment() || HasControlClip())
    return kOverflowClipBothAxis;
  if (!RespectsCSSOverflow() || !HasNonVisibleOverflow())
    return kNoOverflowClip;
  if (IsScrollContainer())
    return kOverflowClipBothAxis;
  return (StyleRef().OverflowX() == EOverflow::kVisible ? kNoOverflowClip
                                                        : kOverflowClipX) |
         (StyleRef().OverflowY() == EOverflow::kVisible ? kNoOverflowClip
                                                        : kOverflowClipY);
}
```

No multicol special case. `LayoutBlockFlow` does not override this. Column-rule painting establishes no clip.

**3. `ColumnLayoutAlgorithm::LayoutLine` — nested-multicol containment is a layout-time column regrowth, not a paint clip.**
File: `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:1099-1124`.

```cpp
LayoutUnit block_end_overflow =
    LogicalBoxFragment(...).BlockEndScrollableOverflow();
if (line_offset + block_end_overflow > FragmentainerSpaceLeftForChildren()) {
  ...
  if (!minimum_column_block_size && block_end_overflow > column_size.block_size) {
    // We're inside nested block fragmentation, and the column was
    // overflowed by content taller than what there is room for in the
    // outer fragmentainer. Try column line layout again, but this time
    // force the columns to be this tall as well, to encompass overflow.
    minimum_column_block_size = block_end_overflow;
    return LayoutLine(...);
  }
}
```

`ConstrainColumnBlockSize` (cla.cc:1936-1968) further confirms the discipline: "*we may shrink the column block size here, but we'll never stretch them ... the only thing we need to worry about here is to not overflow the multicol container.*" Layout owns containment; paint trusts layout.

**4. `multicol-nested-010` is contained by regrowth + outer multicol border-box, not by fragmentainer clip.** WPT test puts a 100-px green child in column 1 of an outer multicol (`columns:2; height:120px`), then an inner `<div>` (`columns:2; height:100px`) in column 2. Blink's regrowth (above) inflates the outer column's block size to encompass the inner multicol's `BlockEndScrollableOverflow()`; the outer multicol container's own border-box ends the visual extent. Louis14's Phase 12h F2 partial worked around the missing regrowth with `ClipBlockAxisOnly`; with regrowth ported, the workaround is no longer load-bearing.

**5. The "narrower predicate" from Phase 16.b.3 has no Blink analog.** findings.md:432-442 proposed predicate ("only set ClipBlockAxisOnly when nested-multicol with explicit column-height/height exceeds outer column") doesn't match anything in Blink. Blink's solution is upstream, in `LayoutLine` recursive relayout. There is no paint-time predicate to mirror.

#### Implementation plan — two steps, in this order

**16.c.1 — Port column regrowth from `column_layout_algorithm.cc:1099-1124`.**

In `multicol_layout.go` `LayoutLine`, after the inner-column loop returns its results, BEFORE deciding whether to commit the row, check each column's `BlockEndScrollableOverflow` (or our equivalent — likely `BlockSizeForFragmentation` already populated by Phase 16.b.1) against the outer fragmentainer space remaining. When BOTH:
- We are inside a nested fragmentation context (`mla.space.HasBlockFragmentation && mla.space.BlockFragmentationType == FragmentColumn`), AND
- `lineOffset + max(BlockEndScrollableOverflow)` exceeds `FragmentainerSpaceLeftForChildren()`, AND
- `minimumColumnBlockSize` (a new local variable, not a field) is unset AND `max(BlockEndScrollableOverflow) > columnSize.BlockSize`,

then set `minimumColumnBlockSize = max(BlockEndScrollableOverflow)` and recurse into `LayoutLine` once with that minimum. Mirrors Blink's `return LayoutLine(...)` tail-call.

Concrete signature change: `LayoutLine` gains an optional `minimumColumnBlockSize` parameter (`Indefinite` by default). When set, `ConstrainColumnBlockSize` clamps the lower bound by it.

Carrier for "block-end scrollable overflow" — louis14 doesn't have a direct equivalent. The closest is `LayoutResult.BlockSizeForFragmentation` (from Phase 16.b.1) — it already represents "the block-axis space the column's content actually consumed including monolithic overflow." If that turns out to be insufficient (some scrollable-overflow cases not represented), add `LayoutResult.BlockEndScrollableOverflow float64` as a separate field. **Try `BlockSizeForFragmentation` first; only add a new field if a specific test requires it.**

Verify on `multicol-nested-010` first — the original target. Expected: passes with regrowth, no `ClipBlockAxisOnly`.

**16.c.2 — Remove `ClipBlockAxisOnly` entirely.**

Once 16.c.1 lands and `multicol-nested-010` is recovered:

1. Delete the `ClipBlockAxisOnly` setter at `multicol_layout.go:1218-1232`. The `shouldClip` local computation goes with it.
2. Delete the corresponding paint-side enforcement at `paint_layer.go:279-296`. Note: keep the `forceBorderBoxClip` and ordinary CSS overflow paths — only the `blockAxisOnlyClip` branch is removed.
3. Optionally (cleanup) remove `PhysicalFragment.ClipBlockAxisOnly`, `Box.ClipBlockAxisOnly`, and the `engine.go:332` propagation — but only after the gate is green; deletes can land in a follow-up cleanup commit.

**Risk surface for 16.c.**

- `multicol-nested-010` is the load-bearing risk. If the regrowth doesn't reproduce the visual containment, ALL nested-multicol-overflow tests will diff. Verify this test passes after 16.c.1, BEFORE doing 16.c.2.
- Tests where `ClipBlockAxisOnly` was incidentally hiding a real layout bug (something taller than colBlockSize where it shouldn't be) will now expose that bug. These are likely the same tests that were already failing at 192/455 baseline, but watch for new failures in `multicol-overflow-*` and `multicol-fill-auto-*`.
- Outer-pagination interactions (`spanner-fragmentation-{000,002,008,010,012}`) are part of the Phase 16.b regression cluster — they should recover with 16.c.2 because the post-spanner column heights stop being clipped to a wrong colBlockSize. If they don't recover, the residual is a Phase 18 (`MulticolBreakTokenData` row-carry) prerequisite, not a 16.c failure.

#### Test order

1. After 16.c.1: run `multicol-nested-010` only. Expected: PASS.
2. After 16.c.1: also run the four 16.b drivers (`-005, -006, -007, -008`) and `column-height-001`. Expected: all still PASS (regrowth shouldn't affect single-level column tests).
3. After 16.c.2: run the regression list — `column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`. Expected: most return to PASS.
4. Full multicol gate sweep before commit. Expected: ≥192/455 (recover Phase 16.b regression net, ideally exceed baseline because `-006/-007/-008` are still passing).
5. If multicol-nested-010 fails after 16.c.1: do NOT do 16.c.2. Re-investigate the regrowth carrier (BSFF vs new BlockEndScrollableOverflow field).

#### Anti-patterns to avoid

- **Don't keep ClipBlockAxisOnly with yet-another predicate.** Blink has no fragmentainer-level clip. Any predicate is a hack masking the underlying layout-time fix.
- **Don't add a new `LayoutResult` field unless you've verified BSFF is insufficient.** The Phase 16.b plan deliberately reused BSFF for the same reason — fewer carriers, less semantic drift.
- **Don't port the regrowth without the recursion.** `ConstrainColumnBlockSize` clamps; `LayoutLine` recurses. Both are needed. A single-pass "raise minimum, redo column loop inline" likely diverges from Blink's break-token discipline.
- **Don't take this on if Phase 14b's `BlockSizeForFragmentation` semantic isn't fully understood.** Phase 14b set BSFF as the parent's "I need this much space" hook for nested-multicol-defer; 16.c needs the same field to express column-content-overflow. The two semantics agree (both are "what content needs"), but a careful reader should confirm Phase 14b's collapseThrough guard isn't broken by additional BSFF-set sites.

**Tractability.** Medium-high. The regrowth port is a small focused change (~50 lines) but it touches `LayoutLine`'s recursion shape, which is load-bearing for the entire row/spanner stretch loop. The `ClipBlockAxisOnly` removal is mechanical once regrowth is in.

**Gate target.** 167 → 192+/455 (recover Phase 16.b regression net). Spanner-fragmentation 7/13 → 12/13 (recover the cluster). Best case: 192 → 195+/455 because `-006/-007/-008` (kept from 16.b) are now additive to the recovered baseline.

---

### Phase 16.c.2 attempt — what we learned (2026-04-27)

16.c.1 (regrowth port) landed cleanly as `2aa01920`. 16.c.2 (clip removal) was attempted in the same session and rolled back per the brief's "STOP, ROLLBACK" discipline. The attempt is documented here so future work doesn't re-tread the same path.

**Result of removing `ClipBlockAxisOnly` setter + paint-side branch.** Multicol gate 167 → 159 (net −8). Spanner-fragmentation unchanged at 7/13. `multicol-nested-010` PASSES (regrowth fires correctly). The Phase 16.b regression cluster (`column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`) **did not recover** — those tests are unaffected by the clip and stay failing.

**Tests newly broken by clip removal (13).**
- Column-wrap monolithic content: `column-height-001`, `column-height-010`, `column-height-017`, `column-height-026`, `column-height-027`. Pattern: `columns:N; column-height:H; column-wrap:wrap` with a single child taller than H. Baseline relies on the per-column block-axis clip to make 4 column-fragments-of-the-same-monolithic-block render as a 100×100 tiled square.
- `break-inside:avoid` in nested multicol: `multicol-nested-030`, `multicol-nested-031`. Same pattern — the clip masks an unbreakable block placed at full size in one column.
- Spanner-fragmentation: `spanner-fragmentation-001`, `-004`, `-006`. Post-spanner column heights expose overflow without the clip.
- Misc nested-multicol monolithic: `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`.

**Tests newly recovered by clip removal (5).** `increase-prev-sibling-height`, `inline-block-and-column-span-all`, `multicol-fill-balance-032`, `multicol-nested-029`, `multicol-zero-height-002`. Each had an inline/balance interaction the clip was actively breaking.

**Blink-divergence diagnosis.** The brief's hypothesis ("removing the clip recovers the 16.b cluster, because Blink has no per-column clip") is half-right. Blink does have no per-column clip, but the 16.b cluster's failure mode in louis14 has nothing to do with painting — it's something in the BSFF row-advance / spanner placement / break-token discipline upstream of paint. Phase 16.b's narrowed `shouldClip := col.result == nil || BSFF == 0 || !hasAutoColumnHeight()` predicate was a coincidental side-effect, not a paint regression.

The newly-broken cluster reveals the deeper gap. In Blink, `column-wrap:wrap` + monolithic content does **not** place the same monolithic block in every column-fragment — it fragments the block at column boundaries (even when `break-inside` would prefer not to), so each column-fragment shows a different 50px slice. Louis14's column-wrap path places the full monolithic block at offset 0 in every column-fragment and depends on the per-column clip to cap visible extent at `colBlockSize`. Until that fragmentation gap is closed, the clip is load-bearing.

**Concrete next-step: Phase 16.d (TBD) — port Blink's monolithic-content fragmentation for column-wrap.** Search Blink for how `block_layout_algorithm.cc` decides to split a monolithic child at the column boundary when `is_block_fragmentation_context_root_` and the inner `BlockFragmentationType == FragmentColumn`. Likely involves `tallest_unbreakable_block_size_` plumbing into `ContentRun` measurements + a per-column-fragment offset on the unbreakable block (similar to how percentage-positioned children get re-anchored per fragment in `box_fragment_painter.cc`'s `ScopedDisplayItemFragment`). Once monolithic content fragments correctly, retry 16.c.2.

**Do not retry 16.c.2 in isolation.** The recovery list is too small (5) to justify the regression list (13), and the brief's "no new predicate" rule means the only path forward is the upstream fragmentation fix.

---

### Phase 16.d Blink research (2026-04-27) — slicing vs clipping resolved

Research-only commit. The CONTINUE-16d.md brief proposed porting Blink's `MonolithicOverflow` carrier (on `BlockBreakToken`) plus `TallestUnbreakableBlockSize` (on `LayoutResult`) to handle the 13 driver tests broken when 16.c.2 removed `ClipBlockAxisOnly`. Reading the actual Blink sources resolves the brief's open question (Hypothesis A reserve+paint-clip vs B multi-fragment slicing) and reveals that **Hypothesis B is correct, but the proposed mechanism in the brief is wrong**: `MonolithicOverflow` is print-only in Blink (gated by `IsPaginated()`), not the multicol mechanism. The actual gap in louis14 is **per-fragment block-size clamping** when a regular block resumes from an incoming `BlockBreakToken` inside a column fragmentainer. This re-shapes the implementation plan.

**File 1: `third_party/blink/renderer/core/paint/box_fragment_painter.cc:1080-1114` — `PaintBlockChild` fragmentainer branch.** Verbatim:

```cpp
const auto& box_child_fragment = To<PhysicalBoxFragment>(child_fragment);
if (box_child_fragment.CanTraverse()) {
  if (box_child_fragment.IsFragmentainerBox()) {
    // It's normally FragmentData that provides us with the paint offset.
    // FragmentData is (at least currently) associated with a LayoutObject.
    // If we have no LayoutObject, we have no FragmentData, so we need to
    // calculate the offset on our own (which is very simple, anyway).
    // Bypass Paint() and jump directly to PaintObject(), to skip the code
    // that assumes that we have a LayoutObject (and FragmentData).
    PhysicalOffset child_offset = paint_offset + child.offset;

    // This is a fragmentainer, and when a node inside a fragmentation context
    // paints multiple block fragments, we need to distinguish between them
    // somehow, for paint caching to work. Therefore, establish a display item
    // scope here.
    unsigned identifier = FragmentainerUniqueIdentifier(box_child_fragment);
    ScopedDisplayItemFragment scope(paint_info.context, identifier);
    BoxFragmentPainter(box_child_fragment)
        .PaintObject(paint_info, child_offset);
    return;
  }
```

`ScopedDisplayItemFragment` is a paint-cache identifier scope, not a clip recorder, scroll-translation, or paint-offset adjustment. **There is no painter-side per-fragmentainer clip and no painter-side slicing.** Hypothesis A from the brief is fully refuted.

**File 2: `third_party/blink/renderer/core/layout/block_break_token.h:96-106` — `MonolithicOverflow` is print-only.** Verbatim:

```cpp
// The amount of monolithic fragmentainer overflow.
//
// Fragmentainer overflow occurs when there is monolithic content, and when
// printing, we record it here, in order to steer clear of it on subsequent
// pages.
//
// This value is only used (and set) when printing.
LayoutUnit MonolithicOverflow() const {
  DCHECK(!is_repeated_actual_break_);
  return monolithic_overflow_;
}
```

The propagation site in `box_fragment_builder.cc:519-552` confirms it: `MonolithicOverflow` is gated by `GetConstraintSpace().IsPaginated()` and `Node().IsPaginatedRoot()`. It is **strictly the print-time mechanism for laying out tall replaced/monolithic content across pages**, not the multicol mechanism. Reserving space in subsequent column fragmentainers via `ReserveSpaceForMonolithicOverflow` is a no-op for multicol because `IsPaginated()` is false. Porting this carrier as the brief proposed would not fire on any of the 13 driver tests.

**File 3: `third_party/blink/renderer/core/layout/fragmentation_utils.cc` — the multicol mechanism is `TallestUnbreakableBlockSize`.** Three sites:

(a) Setter — `BreakBeforeChildIfNeeded` lines 1105-1113:
```cpp
if (space.IsInitialColumnBalancingPass() && builder &&
    ShouldAvoidBreakInside(space, layout_result)) {
  // If this is the initial column balancing pass, attempt to make the column
  // block-size at least as large as the tallest piece of monolithic content
  // and/or block with break-inside:avoid.
  LayoutUnit block_size = CalculateUnbreakableBlockSize(
      space, layout_result, fragmentainer_block_offset);
  builder->PropagateTallestUnbreakableBlockSize(block_size);
}
```

(b) Border/padding contribution — `SetupFragmentation` lines 510-514:
```cpp
if (space.IsInitialColumnBalancingPass()) {
  const BoxStrut& unbreakable = builder->BorderScrollbarPadding();
  builder->PropagateTallestUnbreakableBlockSize(unbreakable.block_start);
  builder->PropagateTallestUnbreakableBlockSize(unbreakable.block_end);
}
```

(c) Builder propagation — `box_fragment_builder.cc:566-569`:
```cpp
if (GetConstraintSpace().IsInitialColumnBalancingPass()) {
  PropagateTallestUnbreakableBlockSize(
      child_layout_result.TallestUnbreakableBlockSize());
}
```

**File 4: `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:1879-1948` — consumer.**

```cpp
tallest_unbreakable_block_size_ = std::max(
    tallest_unbreakable_block_size_, result->TallestUnbreakableBlockSize());
// ... (loop) ...
if (GetConstraintSpace().IsInitialColumnBalancingPass()) {
  // Nested column balancing. Our outer fragmentation context is in its
  // initial balancing pass, so it also wants to know the largest unbreakable
  // block-size.
  container_builder_.PropagateTallestUnbreakableBlockSize(
      tallest_unbreakable_block_size_);
}
// ...
if (tallest_unbreakable_block_size_ >=
    content_runs.TallestContentBlockSize()) {
  return ConstrainColumnBlockSize(tallest_unbreakable_block_size_,
                                  line_offset, available_outer_space);
}
```

`ConstrainColumnBlockSize` (cla.cc:1938-1948) then floors:
```cpp
// Avoid becoming shorter than the tallest piece of unbreakable content.
size = std::max(size, tallest_unbreakable_block_size_);

if (is_constrained_by_outer_fragmentation_context_) {
  // Don't become too tall to fit in the outer fragmentation context.
  size = std::min(size, available_outer_space.ClampNegativeToZero());
}
```

**File 5: `fragmentation_utils.cc:542-657` — `FinishFragmentation` clamps overflowing fragments.** Verbatim:

```cpp
} else if (space_left != kIndefiniteSize && desired_block_size > space_left &&
           space.HasBlockFragmentation()) {
  // We're taller than what we have room for. We don't want to use more than
  // |space_left|, but if the intrinsic block-size is larger than that, it
  // means that there's something unbreakable (monolithic) inside (or we'd
  // already have broken inside). We'll allow this to overflow the
  // fragmentainer.
  DCHECK_GE(desired_intrinsic_block_size, trailing_border_padding);
  DCHECK_GE(desired_block_size, trailing_border_padding);

  LayoutUnit modified_intrinsic_block_size = std::max(
      space_left, desired_intrinsic_block_size - subtractable_border_padding);
  builder->SetIntrinsicBlockSize(modified_intrinsic_block_size);
  final_block_size =
      std::min(desired_block_size - subtractable_border_padding,
               modified_intrinsic_block_size);

  // We'll only need to break inside if we need more space after any
  // unbreakable content that we may have forcefully fitted here.
  if (final_block_size < desired_block_size)
    builder->SetDidBreakSelf();
}
```

This is the **regular CSS block fragmentation** clamp: when a block's full size exceeds the column-fragmentainer's `space_left`, the fragment is clamped to `min(desired_block_size, modified_intrinsic_block_size)` and `DidBreakSelf` is set so the next fragmentainer gets a continuation break-token. There is no special "monolithic content" branch — same code handles everything.

**Resolution of Hypothesis A vs B.** Hypothesis B is correct: each column fragmentainer holds a separate `PhysicalBoxFragment` of the original block, sized to fit the column. The mechanism is **regular block fragmentation via `DidBreakSelf` + outgoing `BlockBreakToken` with updated `ConsumedBlockSize`**, not a print-time `MonolithicOverflow` carrier and not a paint-time clip. For `column-height-001` (a `height:200px` div with no `break-inside:avoid` in `column-wrap:wrap; column-height:50px` columns), the chain is: column 1 lays out the block, `desired_block_size=200 > space_left=50`, FinishFragmentation clamps fragment to 50, sets `DidBreakSelf`, emits break-token with `ConsumedBlockSize=50`. Column 2 resumes from break-token, `previously_consumed_block_size=50`, lays out the same block with `space_left=50`, fragment clamped to 50 again, break-token `ConsumedBlockSize=100`. Repeats four times across the 2×2 column-wrap row grid. Each fragment naturally has its own block-size = column height; nothing overflows; the painter has nothing to clip.

**The louis14 gap.** `block_layout.go:1338-1353` already reads `incomingBreakToken.ConsumedBlockSize` and computes `remaining = explicitBlockSize - consumed`, but **does NOT clamp to the fragmentainer's `space_left`**. The resumed fragment is sized `remaining` (e.g. 150 for the second slot of a 200-tall block in 50-tall columns), which still overflows. There is no `DidBreakSelf` equivalent producing a follow-on `BlockBreakToken` for the remaining 100px after that 50px slot. So louis14 produces:
- Column 1: fragment of size 200 placed at offset 0 in a 50-tall column, the per-column clip in paint masks the bottom 150.
- Column 2 (post-Phase-12f row-wrap): the wrap path resumes the multicol child, but each per-column fragment of the resumed flow again gets sized by `remaining` rather than clamped to fragmentainer-space — and the inner block's contribution within each column is still sized by its full declared height minus consumed, not by the column.

That's why `ClipBlockAxisOnly` was load-bearing for the 13 driver tests: it hides the over-sized fragment block-size that should have been clamped to fragmentainer space at layout time.

**Revised Phase 16.d implementation plan (replaces CONTINUE-16d.md outline).** Three independent fixes, in order of test-impact:

1. **16.d.1 — Per-fragment block-size clamp + DidBreakSelf carrier.** Add `LayoutResult.DidBreakSelf bool` (Blink: `BoxFragmentBuilder::SetDidBreakSelf` → fragment flag). In `block_layout.go` final-size computation (`block_layout.go:1338-1353` cluster), when `space.HasBlockFragmentation && finalBlockSize > space_left`, clamp `finalBlockSize = space_left` and set `DidBreakSelf`. Emit a continuation `BlockBreakToken` with `ConsumedBlockSize = previouslyConsumed + finalBlockSize` even if no inner child broke (the BLOCK ITSELF needs to be resumed). This is the equivalent of FinishFragmentation's `else if (space_left != kIndefiniteSize && desired_block_size > space_left && space.HasBlockFragmentation())` branch. **This single fix should recover most of the 5 column-height-* and 3 spanner-fragmentation-* drivers**, because once each column-fragment is sized to its column, the per-column clip is redundant.

2. **16.d.2 — `LayoutResult.TallestUnbreakableBlockSize` carrier + `IsInitialColumnBalancingPass` path.** Add `LayoutResult.TallestUnbreakableBlockSize float64` (default 0). Setter: in `BreakBeforeChildIfNeeded` (louis14: `pkg/layout/fragmentation_utils.go`), when `space.IsInitialColumnBalancingPass && shouldAvoidBreakInside(layoutResult)`, compute `unbreakableBlockSize = layoutResult.IntrinsicBlockSize` (or the equivalent of Blink's `CalculateUnbreakableBlockSize`) and propagate via `builder.PropagateTallestUnbreakableBlockSize(...)`. Builder-side: `PropagateTallestUnbreakableBlockSize` takes max. Border/padding contributions in `SetupFragmentation` equivalent. Threading: `BoxFragmentBuilder.tallestUnbreakableBlockSize` field → `LayoutResult.TallestUnbreakableBlockSize`. **Required for `multicol-nested-030/031`** (`break-inside:avoid; height:400px` in nested multicol). Without this carrier, the inner multicol's column-block-size doesn't grow to encompass the unbreakable block.

3. **16.d.3 — Floor `constrainColumnBlockSize` by `tallestUnbreakableBlockSize`.** Mirror `cla.cc:1942-1948`: `size = std::max(size, tallest_unbreakable_block_size_); if outerFrag { size = std::min(size, availableOuterSpace) }`. Threaded as a sibling of the existing `minimumColumnBlockSize` (Phase 16.c.1's regrowth carrier). The two are independent: 16.c.1 is post-loop tail-recursion, 16.d.3 is pre-loop initial-balance estimate. Both feed the same constrain function. **Required for `multicol-nested-030/031` to size their inner-multicol columns correctly.**

After 16.d.1+16.d.2+16.d.3, the per-column `ClipBlockAxisOnly` is no longer load-bearing — every fragment is sized to its fragmentainer at layout time. Then 16.c.2 retry deletes the clip (mechanical).

**Why the brief's plan was wrong.** The brief assumed `MonolithicOverflow` was the multicol slicing mechanism. It isn't — it's print-only in Blink, and porting it would not have fired on any of the 13 driver tests. The brief also conflated "monolithic" content (true `break-inside:avoid` or replaced) with "regular block content larger than the column" (`column-height-001`'s `<div style="height:200px;">`). Only 2 of the 13 driver tests use `break-inside:avoid` (multicol-nested-030/031); the rest are regular fragmentation. The dominant fix is 16.d.1 (per-fragment block-size clamp); 16.d.2/16.d.3 (TallestUnbreakable) are a smaller, narrower fix for the break-inside:avoid pair.

**Driver-test prioritization.**
- `column-height-001` is the simplest: regular block, column-wrap:wrap. 16.d.1 alone should fix it.
- `multicol-nested-030/031` needs 16.d.2+16.d.3 too.
- `spanner-fragmentation-001/004/006`: regular block, column-fill:auto, post-spanner column heights — likely 16.d.1.
- `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`: investigate per-test once 16.d.1 lands.

**Risk surface for 16.d.1.** The block-size clamp changes the contract for every block layout result — a fragment is now allowed to be smaller than `finalBlockSize` declared. Any caller that consumes `result.PhysicalFragment.BlockSize` and assumes "this is the full declared height" will break. Audit: `multicol_layout.go` consumers, `block_layout.go` parent-side consumers, paint-side height readers. Likely sites: `BSFF` propagation (Phase 14b adds BSFF-from-children), border/padding inclusion logic, column-balance estimation. The Phase 14b BSFF "what content needs" semantic is unaffected — BSFF is set BEFORE the clamp, so it still represents intended-size for parent's BLA decisions; only the actual fragment block-size shrinks.

**Risk surface for 16.d.2/16.d.3.** Touches the initial balancing pass shape. Phase 17 (forced-break balance, `ContentRuns`/`DistributeImplicitBreaks`) also rewrites this area. To avoid double-rewrite, 16.d.2 carrier infra can land first (cheap, zero-impact on balance loop), and the consumer site can be adjusted for whichever phase lands first.

### Phase 16.c.2 retry attempt #2 — REVERTED (2026-04-27, after 16.d.1)

Second attempt at deleting `ClipBlockAxisOnly` setter (`multicol_layout.go:1281-1286`) + paint branch (`paint_layer.go:274-296`). With Phase 16.d.1's per-fragment clamping in place (commits `a6446061` + `c40b4b56`), the 13 driver tests pass at 0 diff with the clip in tree. The retry hypothesis was that the clip is now redundant.

**Two variants tested:**
1. **Clip removed, IsInsideColumnSpanner gate kept.** Multicol 192 → 195 (+3). Spanner-fragmentation 11 → 9 (-2). Driver-tests: 10/13 PASS, 3 FAIL (`spanner-fragmentation-004` 1.0%, `spanner-fragmentation-006` 0.3%, `nested-floated-multicol-with-monolithic-child` 0.2%).
2. **Clip removed, IsInsideColumnSpanner gate also removed.** Multicol 192 → 196 (+4). Spanner-fragmentation 11 → 10 (-1). Driver-tests: 11/13 PASS, 2 FAIL (`spanner-fragmentation-006` 0.2%, `nested-floated-multicol-with-monolithic-child` 0.2%). Removing the gate let `spanner-fragmentation-004` recover by allowing leaf descendants to self-fragment, which the spanner-resume mechanism handles correctly for that pattern.

**Per CLAUDE.md** ("ALL tests must pass; 0.5% is failure just like 28%"), neither variant is acceptable. Reverted via `git checkout HEAD -- pkg/layout/multicol_layout.go pkg/render/paint_layer.go pkg/layout/block_layout.go`.

**Diagnosis.** The remaining failing tests share a pattern: a column-spanner whose CHILDREN extend visually past the multicol container's box (e.g., spanner-fragmentation-006: `<div column-span:all; height:10px><div height:10; green><div height:360></div><div height:30; green></div></div>` — 400h of content in a 10h box). Without the clip, the 360h transparent overflow renders past the multicol because:

- *Inside spanner with self-fragmentation enabled* (gate removed): the leaf's break-token chain `{Node:leaf, ConsumedBlockSize:N}` is delivered to the spanner-resume mechanism in `multicol_layout.go:663-676` via `spannerContentBreakToken`. For `spanner-fragmentation-001/004` this works (the spanner-resume properly resumes the leaf at consumed=N in the next outer column). For `-006` it produces a small visual artifact because of the more complex spanner-with-multiple-overflow-children pattern.
- *nested-floated-multicol-with-monolithic-child*: the contained `contain:size; height:100` block is NOT a leaf (has a 90h green child) so 16.d.1's clamp doesn't fire; without the clip, its overflow renders past the multicol.

**Path forward (Phase 16.e ACTIVE; option 2 chosen).** Both routes below are Blink-parity ports, not louis14-specific inventions; the choice is sequencing, not philosophy. Per CLAUDE.md "Study Blink first, mirror Blink's algorithms", anything we add must match Blink — the question is *which Blink subsystem* to port next.

1. **Finish the FinishFragmentation port (the leaf-only gate is louis14 scaffolding, not a Blink behavior).** Blink's `FinishFragmentation` (`fragmentation_utils.cc:542-657`) runs on every block — leaf and non-leaf. Phase 16.d.1 only ported the leaf case behind a `len(children) == 0` gate. The parent-side children-loop overflow path that wraps it (`block_layout.go:1001-1196`) is louis14's pre-Phase-16.d divergence: when a child overflows the fragmentainer, the parent emits its own break-token through a parallel mechanism that conflicts with the child self-fragmenting. Earlier "infinite row-wrap loop on `column-height-006`" wasn't a Blink-incompatibility, it was the two parallel paths racing. The Blink-parity move is to *delete* the parent-side overflow path (or shrink it to the cases Blink handles there — IFC breaks, forced breaks) and let `FinishFragmentation` handle the rest.

2. **Port `LayoutSpanner` + `BreakBeforeChildIfNeeded` for spanner children + the spanner break-token shape Blink uses.** `column_layout_algorithm.cc::LayoutSpanner` builds the spanner constraint space (`CreateConstraintSpaceForSpanner`), runs layout, and handles the spanner's outgoing break-token differently from regular blocks (because the spanner is monolithic for placement). Louis14's `layoutSpannerInFrag` plus the `pendingContentOverflow` / `spannerContentBreakToken` mechanism in `multicol_layout.go:663-676 / 724` is a partial louis14 port of this — it works for some patterns (spanner-fragmentation-001/004) but its break-token shape (`{Node: spanner, ChildBreakTokens: [{Node: spanner, ChildBreakTokens: [...]}]}`) isn't exactly what Blink uses (`MulticolPartWalker` dispatches a flat list of `BlockBreakToken`s by `InputNode()`). Porting the exact Blink mechanism would unblock spanner-content fragmentation cleanly.

Both routes are needed eventually — Blink does both. **Option 2 chosen first** (more contained — touches `multicol_layout.go` + `BlockBreakToken` resume path; unblocks 16.c.2 retry directly). Option 1 (delete parent-side children-loop overflow path) is the bigger structural change and is queued as a separate phase after 16.c.2 lands.

**Why option 2 now (correctness rationale, important to future):** Louis14 currently has TWO competing spanner-content-overflow mechanisms — Blink's (partially ported) and louis14's positional encoding (`ChildBreakTokens[0]=nextCol, [1]=partialSpanner, [2]=colRows`). The hybrid works for some patterns and fails for others; the clip masks the failures. Per CLAUDE.md "If a fix doesn't generalize to all cases, it's the wrong fix" — keeping the hybrid + clip is the wrong fix. The right fix is to port Blink's walker model fully so spanner-content fragmentation works for every pattern, then drop the clip.

**Phase 16.e plan (option 2):**
- **16.e.1** Read Blink source carefully: `column_layout_algorithm.cc::LayoutSpanner`, `CreateConstraintSpaceForSpanner`, `BreakBeforeChildIfNeeded`, and `MulticolPartWalker::UpdateCurrent` (`multicol_part_walker.cc/h`). The walker dispatches break-tokens by `InputNode()` to distinguish multicol-container vs spanner vs OOF on resume.
- **16.e.2** Port `MulticolPartWalker`-style resume to `multicol_layout.go`: replace positional `ChildBreakTokens[1]/[2]` (and the `pendingPartialSpannerToken` / `spannerContentBreakToken` indirection) with a walker that dispatches by `InputNode()`. Spanner break-tokens become regular `BlockBreakToken` with `Node = spanner` (no extra wrapping).
- **16.e.3** Drop the `IsInsideColumnSpanner` gate in `block_layout.go:1410-1415` (and the propagation at line 612-614). Spanner descendants self-fragment normally; the walker handles their break-tokens correctly via `InputNode()` dispatch.
- **16.e.4** Diagnose + fix `nested-floated-multicol-with-monolithic-child` separately (no spanner involved; preliminary diagnosis: `contain:size` block under a float, float's `margin-top:10` not applied or 90h leaf positioned at offset 0 instead of offset 10 in float content area — see Phase 16.e.4 brief once written).
- **16.e.5** Verify all 13 driver tests + 5 originally-recovered tests + multicol gate.
- **16.c.2 retry #3** Delete `ClipBlockAxisOnly` setter + paint branch. Should now be net-positive cleanly.

**Attempted 2026-04-27 (minimal-port path) — reverted.** Tried just unwrapping `pendingPartialSpannerToken` (= `fullResult.BreakToken` directly, no `{Node:spanner, ChildBreakTokens:[fullResult.BreakToken]}` wrapper) and adjusting the resume read site to skip `spannerConsumed` when `ChildBreakTokens` is non-empty. Build clean. spanner-fragmentation-006 went from 0% (pass at HEAD with gate kept) to 1.4% — outer cols 2-4 went all-RED instead of green. Trace logic suggested HEAD and unwrap should give identical `spannerContentBreakToken`, so the failure is in some other interaction (possibly outer multicol's outgoing break-token chain construction, or a place that mutates `pendingPartialSpannerToken` after creation — `multicol_layout.go:762-770` appends a `clipToken` to `ChildBreakTokens` in the combined-clip case, and with unwrap this would mutate the spanner's actual break-token rather than the wrapper). The wrapper turns out to also act as a defensive-copy boundary.

**Current understanding.** Louis14's positional `ChildBreakTokens[0..2]` encoding plus the wrapper around `pendingPartialSpannerToken` is more entangled with the surrounding code than expected. Several invariants depend on the wrapper being a separate token that can be mutated/extended (clipToken append, ConsumedBlockSize semantics distinction). A proper port to Blink's flat walker model requires touching:

1. The 3-slot positional read site at `multicol_layout.go:428-447`.
2. The `pendingPartialSpannerToken` build site at `multicol_layout.go:721-728`.
3. The combined-clip mutation at `multicol_layout.go:762-770`.
4. The `nextSpannerClipToken` chain at `multicol_layout.go:823-832`.
5. The `buildOuterBreakResult` outgoing-token construction at `multicol_layout.go:480-512`.
6. The `BlockBreakToken` shape itself (whether to keep positional or move to a flat-with-Node-dispatch list).

This is a Phase 18-equivalent refactor. **Recommend deferring 16.e until either** (a) we have a longer session block to do it carefully end-to-end, or (b) Phase 17 / 19 / other targets give easier wins first and we come back to 16.e armed with more spanner-test understanding. Until 16.e lands, `ClipBlockAxisOnly` stays in tree as a load-bearing workaround.

### Phase 16.e re-sequenced — bundled with Phase 18 (2026-04-27)

**Decision (Opus review of CONTINUE-16e.md).** The CONTINUE-16e.md plan to do the spanner walker port immediately on mainline was reviewed and re-sequenced. Rationale:

1. **The minimal-port reversion above** showed the 6 sites are interlocked in non-obvious ways (the wrapper acts as a defensive-copy boundary; `spannerConsumed` semantics distinguish from spanner-content cursor in places not visible from grep). A 6-site coordinated rewrite carries real risk of finding 7th and 8th invariants the same way.
2. **The CONTINUE's "acceptable temporary regressions"** policy committed broken intermediate states to `fix/flexbox-fast` (steps 3-4 of its outline expected commit-3 to break spanner-frag and commit-4 to restore). Multi-commit refactors with broken intermediate states belong in a worktree.
3. **Phase 18 needs the same break-token-shape change.** Phase 18's `MulticolBreakTokenData` carrier (this section's brief above) requires either a polymorphic `data_` field or a typed nullable pointer on `BlockBreakToken`. Doing 16.e first WITHOUT the carrier means re-touching the same 6 sites again to thread it through the new walker. Bundling 16.e + 18 is one carrier-and-walker port instead of two passes through the same code.
4. **Phase 17 is well-scoped, low-risk, and ~5 tests** — a clean intermediate win on mainline that doesn't touch the spanner code. See findings.md § Phase 17 brief: single function rewrite at `multicol_layout.go:1382-1455` with full Blink algorithm already documented.
5. **Marginal multicol-gate gain from 16.e+16.c.2 alone is ~+4 tests** (192 → 196). Phase 17 alone is +5; bundled 16.e+18 is ~+15-20 (Phase 18's nested-multicol cluster is the bigger gate-mover). Sequencing Phase 17 first delivers a faster intermediate win without blocking the bundled work.

**Re-sequenced order.**

1. **Phase 17** (active, mainline) — Forced-break balance. ~5 tests. Continuation: `CONTINUE-17.md`.
2. **Phase 16.e + 18 bundled** (worktree) — port `MulticolPartWalker` + add `MulticolBreakTokenData` carrier as one foundational change. ~15-20 tests. Bundled brief below.
3. **Phase 16.c.2 retry #3** (mainline, mechanical) — delete `ClipBlockAxisOnly` end-to-end. ~3-4 tests.
4. **Option 1** (worktree) — finish FinishFragmentation port (drop leaf-only gate; delete or shrink parent-side overflow path).

**Bundled 16.e + 18 brief (sketch — flesh out before starting).**

The walker port and the carrier port collapse into one design exercise. The break-token shape becomes:

```go
type BlockBreakToken struct {
    // ...existing fields...
    ChildBreakTokens []*BlockBreakToken  // FLAT, document-order; dispatch by Node
    MulticolData     *MulticolBreakTokenData  // typed nullable pointer (Phase 18)
}

type MulticolBreakTokenData struct {
    ConsumedRowBlockSize layoutunit.LayoutUnit
}
```

Walker iterates `ChildBreakTokens` in document order; dispatch on `child.Node`:
- `child.Node == mla.node` → column-rows segment (resume `LayoutLine`).
- `child.Node.IsColumnSpanAll()` → spanner (resume `layoutSpanner` with `child` as input break-token, including any nested `ChildBreakTokens` for spanner-content overflow).
- `child.Node.IsOutOfFlowPositioned()` → OOF candidate (existing OOF aggregation).

The combined-clip case (current `multicol_layout.go:762-770`) sets `ConsumedBlockSize` directly on the spanner's own outgoing token instead of appending a wrapped `clipToken`. The next-spanner-clip-chain case (current `:823-832`) becomes its own flat entry in document order, no nesting. The outer's `buildOuterBreakResult` emits `ChildBreakTokens` flat with `MulticolData` attached when the row-overflow condition fires (Blink cla.cc:1374).

**Recommended commit decomposition (in worktree branch, e.g. `phase-16e-18-walker`):**

1. `Phase 16.e+18 step 1: introduce MulticolBreakTokenData carrier + walker scaffold` — schema change + walker type, no behavior change. Build clean. Mainline-mergeable.
2. `Phase 16.e+18 step 2: switch read site to walker dispatch` — remove positional read at `multicol_layout.go:428-447`; walker drives resume. Expect spanner-fragmentation regressions.
3. `Phase 16.e+18 step 3: switch write site to flat ChildBreakTokens` — rewrite `buildOuterBreakResult` (`:480-512`) to emit flat list; drop `pendingPartialSpannerToken` wrap (`:721-728`); collapse combined-clip mutation (`:762-770`) and next-spanner-clip-chain (`:823-832`). Step 2's regressions should restore.
4. `Phase 16.e+18 step 4: wire MulticolBreakTokenData write site` — Phase 18's nested-multicol row-carry (Blink cla.cc:1374). Read site at `multicol_layout.go:289-292` already plumbed.
5. `Phase 16.e+18 step 5: drop IsInsideColumnSpanner gate` — remove gate in `block_layout.go:1410-1415` + propagation at `:611-614` + `ConstraintSpace.IsInsideColumnSpanner` field/setter. Verify spanner-frag-006 and the `multicol-nested-011..032` cluster.
6. `Phase 16.e+18 step 6: gate sweep` — full multicol + spanner-frag + invariants. Worktree branch should pass before merging back to mainline.

**Hard exit conditions during the bundled work.** If step 5 regresses spanner-fragmentation-006 even with the walker in place, the walker-restructures-outer-tokens-but-not-inner-spanner-tokens hypothesis is wrong and the spanner-resume mechanism needs additional changes. STOP, diagnose, do not chase with predicates. If `multicol-nested-010` (Phase 16.c.1 regrowth) regresses at any step, the row-carry write or the walker's column-rows dispatch is wrong.

### Phase 16.d.1 retrospective (2026-04-27, commits `a6446061` + `c40b4b56`, +25 multicol / +4 spanner-fragmentation)

**Shipped.** `LayoutResult.DidBreakSelf bool` carrier and FinishFragmentation-equivalent clamp in `block_layout.go` after the min/max constraints (between line 1372 and the `builder.SetSize` call). When the gate fires, the fragment is sized to `space_left = FragmentainerBlockSize - FragmentainerOffset - geom.BlockBorderPadding()`, `didBreakSelf=true`, and after `result := builder.Build()` a continuation `BlockBreakToken{Node, ConsumedBlockSize=prev+finalBlockSize, SequenceNumber=prev+1}` is attached.

**Gate refinement (vs the brief's bare-minimum gate).** Five gates were essential to avoid regressions:

1. `!IsBlockSizeOverride` — the multicol's column-fragmentainer's direct anon child has its size authoritatively fixed; clamping it again would double-clamp.
2. `hasExplicitBlock` — auto-height blocks have their content drive the size; clamping makes no sense.
3. `FragmentainerBlockSize > 0` — louis14 sometimes propagates `HasBlockFragmentation=true` with `FragmentainerBlockSize=0` (e.g., `column-fill:auto + auto column-height + Indefinite colBlockSize` in `multicol_layout.go:1038`). With `space_left=0`, every block was clamped to 0, making green content invisible (driver: `nested-floated-multicol-with-monolithic-child` at 1.9% diff before this gate).
4. `!IsInitialColumnBalancingPass` — the measure pass would corrupt the balance estimate.
5. **Leaf only (no DOM children, not column-span:all)** — the dominant gate. Without this, `column-height-006` (`column-wrap:wrap` + 4 spanners + leaf siblings inside a `<div>` parent) hit an infinite row-wrap loop because non-leaf parents have their own parent-side fragmentation logic at `block_layout.go:1001-1196` (the children-loop overflow path). Interleaving self-fragmentation with that logic produced break-token chains that never advanced past the spanner siblings; the row-wrap loop kept re-laying out the same row. Spanners similarly have their own resume mechanism (`pendingPartialSpannerToken` / `spannerConsumed` in `multicol_layout.go:721-728`) — the brief's `MonolithicOverflow` was a different abstraction than what the existing spanner code expects.

**13/13 driver tests recovered after the IsInsideColumnSpanner gate (commit `c40b4b56`).** First commit `a6446061` recovered 12/13; the spanner-fragmentation-006 holdout (0.1% / 250px) was caused by the spanner's 360h leaf descendant self-fragmenting and confusing the spanner-resume mechanism. Blink reference (`column_layout_algorithm.cc::LayoutSpanner` via WebFetch): "the spanner ITSELF is monolithic for placement (breaks BEFORE as a unit, never mid-content)" — even though Blink does set `HasBlockFragmentation` on the spanner's constraint space, the existing `pendingContentOverflow` + `spannerContentBreakToken` machinery in `multicol_layout.go:663-676 / 724` expects a parent-side overflow break-token chain (`{Node:next-sibling, IsBreakBefore:true}`), NOT the leaf-self-fragmentation chain (`{Node:leaf, ConsumedBlockSize:N}`). The fix added `ConstraintSpace.IsInsideColumnSpanner` set in `layoutSpanner` / `layoutSpannerInFrag` and propagated through child constraint spaces in `block_layout.go:585-614`. The 16.d.1 clamp now skips spanner descendants entirely; they keep using the existing monolithic-leaf path at `block_layout.go:1069-1082`.

Final 13/13 PASS at 0 diff: `column-height-001/010/017/026/027`, `multicol-nested-030/031`, `spanner-fragmentation-001/004/006`, `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`.

**16.b regression cluster — partially recovered.** With +24 multicol and +3 spanner-fragmentation, several of the 16.b regression cluster tests likely recovered (gate sweep moved 167 → 191, well past the 13 driver-test ceiling). A spot-check would map which specific tests recovered; the cluster as documented in `findings.md` § 16.c.2 attempt may be partially closed.

**16.c.2 retry path.** With per-fragment clamping in place, the per-column `ClipBlockAxisOnly` is no longer load-bearing for the 12 driver tests. 16.c.2 retry (delete the clip + paint-side branch) should now be net-positive. Sequence: verify 16.d.2/16.d.3 are not strictly required for any clip-removal-broken test (multicol-nested-030/031 already PASS via 16.d.1 alone), then attempt 16.c.2 again.

---

### Phase 17 brief — Forced-break balance (DONE 2026-04-27, +4 multicol: 192→196)

**Failing tests.** `multicol-fill-balance-040` family (`-038, -039, -040, -041`) plus subset of `-029..-036`. Pattern: N forced `break-before:column` children inside `columns: K`. Expected: column-count expands to N+1 when N ≥ K, with each forced break getting its own column at content-determined height.

**Blink algorithm location.** `third_party/blink/renderer/core/layout/column_layout_algorithm.cc`. Legacy `column_balancer.cc`/`InitialColumnHeightFinder`/`MinimalSpaceShortageFinder` are **deleted** in LayoutNG; their roles are folded inline:

| Legacy class | LayoutNG equivalent | File:line |
|---|---|---|
| `InitialColumnHeightFinder` | `ResolveColumnAutoBlockSizeInternal` + file-local `struct ContentRun` + `class ContentRuns` | `column_layout_algorithm.cc:1734-1934` |
| `MinimalSpaceShortageFinder` | inline in per-column loop via `UpdateMinimalSpaceShortage(...)` | `column_layout_algorithm.cc:1044-1045` |
| `ColumnBalancer` outer driver | `Layout() → LayoutChildren() → LayoutLine()` outer stretch loop | `column_layout_algorithm.cc:967-1252` |

Key entry points:
- `Layout()` — line 266. Computes `used_column_count_` at line 271.
- `ResolveColumnAutoBlockSize(...)` — line 1722. Resets `spanner_path_ = nullptr`, delegates.
- `ResolveColumnAutoBlockSizeInternal(...)` — line 1734. **The actual initial-balance estimator.**
- `ConstrainColumnBlockSize(...)` — line 1938. Clamps by max-block-size, outer fragmentation context.

**Inside `ResolveColumnAutoBlockSizeInternal` (lines 1734-1934).**

```cpp
struct ContentRun {                     // line 1760
  LayoutUnit content_block_size;
  int implicit_breaks_assumed_count = 0;
  LayoutUnit ColumnBlockSize() const {
    return ceil(content_block_size / (implicit_breaks_assumed_count + 1));
  }
};

class ContentRuns {                     // line 1782
  Vector<ContentRun, 1> runs_;
  void AddRun(LayoutUnit) { ... }
  LayoutUnit TallestColumnBlockSize() const { ... }
  LayoutUnit TallestContentBlockSize() const { ... }
  void DistributeImplicitBreaks(int used_column_count); // line 1800
};
```

`DistributeImplicitBreaks` iteratively bumps `implicit_breaks_assumed_count` on the tallest run until `runs_.size() + Σ implicit_breaks_assumed_count >= used_column_count_`.

**The measure-pass loop (lines 1825-1903).**

```cpp
ContentRuns content_runs;
const BlockBreakToken* break_token = child_break_token;
int forced_break_count = 0;

do {
  // Lay out one "tall strip" pass with NO soft breaks allowed.
  ...
  if (forced_break_count < used_column_count_ || consider_all_columns) {
    LayoutUnit column_block_size = BlockSizeForFragmentation(...);
    ...
    content_runs.AddRun(column_block_size);
  }
  ...
  if (result->HasForcedBreak()) forced_break_count++;
  break_token = fragment.GetBreakToken();
} while (break_token);

if (balance_columns) {
  content_runs.DistributeImplicitBreaks(used_column_count_);
}
return ConstrainColumnBlockSize(
    content_runs.TallestColumnBlockSize(), line_offset, available_outer_space);
```

There is **no separate "forced break count field"** — the measure pass re-invokes Layout() on the trailing break token after each forced break. `content_runs.runs_.size()` equals `forced_break_count + 1` (or capped by `used_column_count_`).

**Forced-break propagation (already complete in louis14).**
- `BreakBeforeChildIfNeeded` (Blink: `fragmentation_utils.cc`; ours: `pkg/layout/fragmentation_utils.go`) returns `BreakStatus::kBrokeBefore` with `is_forced=true`.
- `LayoutResult::has_forced_break_` / `LayoutResult::HasForcedBreak()` — Blink: `layout_result.h`; ours: `pkg/layout/layout_result.go:106` (`HasForcedBreak bool`).
- `BlockBreakToken::IsForcedBreak()` — already present in our `pkg/layout/break_token.go`.
- Our `pkg/layout/multicol_layout.go:1042, 1112-1114` already counts `forcedBreakCount` correctly in the **outer** stretch loop.
- Our exit at `pkg/layout/multicol_layout.go:1180-1183` (`if numCols <= forcedBreakCount+1: break`) mirrors Blink `cla.cc:1211` correctly.

**Expansion math.** Blink does **NOT** expand `used_column_count_` past K. Instead:

- If `N + 1 < K`: `DistributeImplicitBreaks` adds `(K − N − 1)` implicit breaks onto the tallest run(s); height = `tallest_run.content_block_size / (implicit_breaks_assumed + 1)`. Result: K columns of roughly equal height.
- If `N + 1 == K`: each forced break gets its own column; no implicit breaks added; height = max content_block_size of the runs.
- If `N + 1 > K`: `DistributeImplicitBreaks` is a no-op. Per-column loop produces N+1 columns; acceptance test `actual_column_count <= used_column_count_` (cla.cc:1204) **fails**, then the safety valve at cla.cc:1211 fires:
  ```cpp
  if (used_column_count_ <= forced_break_count + 1) {
    if (!is_constrained_by_outer_fragmentation_context_)
      break;          // Accept the over-count. Row uses N+1 columns.
    new_column_block_size = LayoutUnit::Max();
  }
  ```
  Result: N+1 columns (overflows inline-axis), each at content height.

So effective column count is **`max(N+1, K)`**, achieved by *failing acceptance* and exiting the stretch loop, **not** by recomputing `used_column_count_`.

**Louis14 gap (root cause of `multicol-fill-balance-040` family).**

Our `resolveColumnAutoBlockSize` at `pkg/layout/multicol_layout.go:1382-1455` does:
```go
totalHeight := result.IntrinsicBlockSize
estimate := math.Ceil(totalHeight / float64(numCols))
```
i.e. "total content height ÷ K", with **a single** `BlockLayoutAlgorithm.Layout()` call via `IsInitialColumnBalancingPass=true`. There is no measure-pass do-while loop, no `ContentRun`/`ContentRuns` equivalent, **no forced-break counting at all** during measurement.

For `multicol-fill-balance-040` (3 children with `break-before:column`, `column-count: 2`):
- Total content height ≈ 3·h. Our estimate = `ceil(3h/2) = 1.5h`.
- Outer stretch loop lays out 2 columns at 1.5h, but each forced break consumes a whole column, so column 1 gets 1 child (h tall), column 2 gets 1 child + a forced break that demands a column 3.
- Acceptance fails (`actual_column_count > used_column_count_`); the `numCols <= forcedBreakCount+1` exit fires (`2 <= 3`), so we break — but with the wrong height (`1.5h` vs `h`) and only 2 committed columns.

Expected: 3 columns of ≈40px. Actual: 2 columns of ≈60px with overflow. Visible as red-stripe-at-bottom.

**Implementation plan.** Replace the body of `resolveColumnAutoBlockSize` (`pkg/layout/multicol_layout.go:1382-1455`) with a Blink-parity loop:

```go
type contentRun struct {
    contentBlockSize       float64
    implicitBreaksAssumed  int
}
func (r contentRun) columnBlockSize() float64 {
    return math.Ceil(r.contentBlockSize / float64(r.implicitBreaksAssumed+1))
}

// In resolveColumnAutoBlockSize:
runs := []contentRun{}
breakToken := childBreakToken
forcedBreaks := 0
for {
    space := buildMeasureSpace(...) // IsInitialColumnBalancingPass=true,
                                    // AvailableSize.Block = LayoutUnit::Max,
                                    // pass `breakToken` as input.
    result := layoutElement(ctx, multicolChild, space)
    if forcedBreaks < numCols || considerAllColumns {
        runs = append(runs, contentRun{
            contentBlockSize: result.BlockSizeForFragmentation, // or IntrinsicBlockSize
        })
    }
    if result.HasForcedBreak { forcedBreaks++ }
    if result.BreakToken == nil { break }
    breakToken = result.BreakToken
}
if balanceColumns {
    distributeImplicitBreaks(runs, numCols)
}
tallest := 0.0
for _, r := range runs { tallest = math.Max(tallest, r.columnBlockSize()) }
return constrainColumnBlockSize(tallest, lineOffset, availableOuter)

func distributeImplicitBreaks(runs []contentRun, target int) {
    total := len(runs)
    for total < target {
        // find run with tallest current columnBlockSize
        idx := 0
        for i := 1; i < len(runs); i++ {
            if runs[i].columnBlockSize() > runs[idx].columnBlockSize() {
                idx = i
            }
        }
        runs[idx].implicitBreaksAssumed++
        total++
    }
}
```

**No outside changes needed.** The outer stretch loop at `multicol_layout.go:1031-1206` already counts forced breaks and has the `numCols <= forcedBreakCount+1` exit (mirroring Blink cla.cc:1211). Once the initial estimate is right, the existing code accepts the N+1 column case correctly.

**Test order.** `multicol-fill-balance-040` (canonical 3-break, 2-column) → `-039`/`-038`/`-041` (variants) → re-run cluster sweep.

**Tractability.** Medium. ~120 lines in one function. Mirror Blink's loop literally; the semantics of `BlockSizeForFragmentation` vs `IntrinsicBlockSize` need verification (Blink uses the former because it accounts for trailing margin).

---

### Phase 18 brief — Nested multicol break-token forwarding (target T3, ~15 tests)

**Failing tests.** `multicol-nested-011..032` cluster + `multicol-fill-balance-003, -026`. Symptom: inner multicol content spills sideways into more inner sub-columns instead of breaking and resuming in the next outer column.

**Blink data structure.** `third_party/blink/renderer/core/layout/multicol_break_token_data.h`:

```cpp
struct MulticolBreakTokenData final : BreakTokenAlgorithmData {
  explicit MulticolBreakTokenData(LayoutUnit consumed_row_block_size)
      : BreakTokenAlgorithmData(kMulticolData),
        consumed_row_block_size(consumed_row_block_size) {}

  // In nested block fragmentation, when a column row (specified by the
  // `column-height` property) is too tall to fit in one outer fragmentainer,
  // the remainder needs to be handled in subsequent outer fragmentainers.
  LayoutUnit consumed_row_block_size;
};
```

**Polymorphic carrier.** `BreakTokenAlgorithmData` (`break_token_algorithm_data.h`) is a 3-bit-tagged GC'd polymorphic root with `DataType` enum: `kFieldsetData, kFlexData, kGridData, kTableData, kTableRowData, kMulticolData`. `IsMulticolType()` discriminates for `DynamicTo<MulticolBreakTokenData>`.

**Storage on `BlockBreakToken`.** `Member<BreakTokenAlgorithmData> data_;` accessor `BlockBreakToken::TokenData()`. Builder side: `BoxFragmentBuilder::SetBreakTokenData(...)`. Constructor moves `data_ = builder->break_token_data_; builder->break_token_data_ = nullptr;`.

**Write site (cla.cc:~1374, inside `LayoutLine`).**

```cpp
if (ShouldWrapColumns() && HasRowHeight() && is_first_row &&
    GetConstraintSpace().HasKnownFragmentainerBlockSize()) {
  LayoutUnit overflow = RemainingRowHeightAtOffset(line_offset) -
                        (FragmentainerSpaceLeftForChildren() - line_offset);
  if (overflow > LayoutUnit()) {
    // There wasn't even enough room for one row in the outer fragmentainer.
    // Resume the row in the next fragmentainer.
    container_builder_.SetBreakTokenData(
        MakeGarbageCollected<MulticolBreakTokenData>(RowHeight() - overflow));
  }
}
```

Conditions for write:
- `ShouldWrapColumns()` — `column-wrap: wrap` (CSS Multicol L2).
- `HasRowHeight()` — non-auto `column-height` (a fixed row stride exists).
- `is_first_row` — only the row that started against the outer column edge.
- `HasKnownFragmentainerBlockSize()` — nested inside an outer fragmentation context with known fragmentainer height.
- `overflow > 0` — the row's required height exceeds remaining outer space.

The value stored is `RowHeight() - overflow` = the amount of the row that *did* paint in this outer fragmentainer.

**Read site (cla.cc:~2122, `OffsetInCurrentRow`).**

```cpp
LayoutUnit ColumnLayoutAlgorithm::OffsetInCurrentRow(
    LayoutUnit line_offset) const {
  LayoutUnit row_stride = RowHeight() + row_gap_size_;
  if (row_stride == LayoutUnit()) return LayoutUnit();
  if (GetBreakToken()) {
    if (const auto* data = DynamicTo<MulticolBreakTokenData>(
            GetBreakToken()->TokenData())) {
      // Add row progress from previous outer fragmentainers.
      line_offset += data->consumed_row_block_size;
    }
  }
  return CurrentContentBlockOffset(line_offset) % row_stride;
}
```

**Round-trip flow.**
1. Outer multicol calls inner multicol `Layout()` in outer-column N.
2. Inner multicol detects `is_first_row && row > remaining outer space`.
3. Inner multicol writes `MulticolBreakTokenData(painted_amount)` via `container_builder_.SetBreakTokenData(...)`. Outgoing `BlockBreakToken` carries it in `data_`.
4. Outer multicol receives the inner's break token, attaches it to the per-column break-token chain.
5. Outer moves to outer-column N+1, calls inner `Layout()` again with that token as input.
6. Inner's `OffsetInCurrentRow(0)` adds `consumed_row_block_size` before the modulo → mid-row resume works correctly.

**Outer-side propagation.** Per-column loop at cla.cc pulls the next column break token from the child's physical fragment:
```cpp
next_column_token = To<BlockBreakToken>(
    result->GetPhysicalFragment().GetBreakToken());
while (next_column_token && ShouldWrapColumns() && !result->GetColumnSpannerPath());
```
`MulticolPartWalker::AddNextColumnBreakToken` carries the token forward. `data_` rides along — no special outer-side handling needed.

**Fragmentation entry/exit conditions in Blink.** When `is_constrained_by_outer_fragmentation_context_ = HasKnownFragmentainerBlockSize()`:
1. `ConstrainColumnBlockSize` clamps inner column-height: `size = std::min(size, available_outer_space.ClampNegativeToZero())`. Prevents spill-sideways for `column-fill:auto`.
2. `ColumnsOverflowInInlineDirection` returning `false` causes inner multicol to *break* (emit continuation BreakToken) rather than allocate more sub-columns sideways.
3. `MulticolBreakTokenData` row-carry only seeded for `column-wrap: wrap` + non-auto `column-height` (CSS Multicol L2).
4. `block-size-for-fragmentation` (Phase 14b) is the *parent-side* hook: when inner multicol cannot fit at all, parent's `BreakBeforeChildIfNeeded` consults `LayoutResult::BlockSizeForFragmentation()` to push the entire multicol to next outer fragmentainer.
5. `column-fill: balance` is forced when nested inside an outer fragmentation context with unknown fragmentainer block-size (cla.cc:1025).

**Louis14 current state (the read side is plumbed, the write side is missing).**

- `pkg/layout/break_token.go:1-73` — `BlockBreakToken` is a flat record. **No `data_`/`TokenData`/`BreakTokenAlgorithmData` polymorphic carrier exists.**
- `pkg/layout/multicol_layout.go:43-46` — `consumedRowBlockSize float64` is a plain instance field on `MulticolLayoutAlgorithm`, not a break-token-carried value.
- `pkg/layout/multicol_layout.go:292` — `mla.consumedRowBlockSize = 0` hard-coded with comment "wired from the break token when 12f.6 lands."
- `pkg/layout/multicol_layout.go:187-197` (`offsetInCurrentRow`) — **already** adds `mla.consumedRowBlockSize` to modulo math. Read side is complete; only transport is missing.
- `pkg/layout/multicol_layout.go:414-442` — break-token parser uses positional `ChildBreakTokens[0..2]` slots (`nextColToken / partialSpannerToken / colRowsResumeToken`). 3-slot scheme has no place for `MulticolBreakTokenData`.
- `pkg/layout/multicol_layout.go:504-512` (`buildOuterBreakResult`) — emits outer break-token but does NOT compute or attach `consumed_row_block_size`. The row-overflow case (Blink cla.cc:1374) is not implemented at all.
- `pkg/layout/multicol_layout.go:368-400` (Phase 14b) — `BlockSizeForFragmentation` defer-entire-multicol path is implemented for `column-fill: auto + explicit height + outerAvailable < explicitBlockSize`. For `column-fill: balance` and "row partially fits, row remainder must resume" the code falls through to mid-row spilling.
- `pkg/layout/multicol_layout.go:542-556` — `needsRowAdvance` block calls `buildOuterBreakResult(nil, nil)` when next row won't fit, but never seeds `consumed_row_block_size` for the row that was *partially* placed.

**Implementation plan.**

**Step 1** — Add the polymorphic carrier on `BlockBreakToken` (`pkg/layout/break_token.go`):

```go
// MulticolBreakTokenData carries algorithm-specific resume state for nested
// multicols. Mirrors Blink's BlockBreakToken::data_ + MulticolBreakTokenData
// (multicol_break_token_data.h). Non-nil only on tokens emitted by the
// MulticolLayoutAlgorithm.
type MulticolBreakTokenData struct {
    ConsumedRowBlockSize float64
}

type BlockBreakToken struct {
    // ...existing fields...
    MulticolData *MulticolBreakTokenData
}
```

(Keep it as a typed nullable pointer rather than a `BreakTokenAlgorithmData` interface for now; we have only one algorithm carrying break-token data. Generalize when grid/flex/table need it.)

**Step 2** — Write site in walker loop at `multicol_layout.go:542` (`needsRowAdvance` outer-fits check) AND a new condition mirroring Blink cla.cc:1374:

```go
if hasOuterFrag && hasRowHeight && row > outerAvailable - blockCursor {
    paintedAmount := rowHeight - overflow
    return mla.buildOuterBreakResultWithRowCarry(paintedAmount)
}
```

The helper sets `result.BreakToken.MulticolData = &MulticolBreakTokenData{ConsumedRowBlockSize: paintedAmount}`.

**Step 3** — Read site at `multicol_layout.go:289-292`. Replace `mla.consumedRowBlockSize = 0` with:

```go
if mla.space.BreakToken != nil && mla.space.BreakToken.MulticolData != nil {
    mla.consumedRowBlockSize = mla.space.BreakToken.MulticolData.ConsumedRowBlockSize
}
```

The existing `offsetInCurrentRow` (line 195) consumes the field — no change needed.

**Step 4** — Verify outer multicol's per-column break-token plumbing preserves `MulticolData` when threading inner's `BlockBreakToken` through `colBreakToken` at lines 1046/1143. The chain should pass through unchanged because `MulticolData` is a field on the existing struct.

**Step 5** — Generalize Phase 14b's defer condition. The current `columnFill == "auto"` check at `multicol_layout.go:381` should also handle `"balance"` once the row-carry write is in place. Balanced inner multicols inside a too-small outer column should write the row-carry token rather than balance into a too-tall single row. `BlockSizeForFragmentation` defer remains the "doesn't fit at all" fast path; row-carry is the "partially fit, resume next outer column" path.

**Test order.** `multicol-nested-011` (simplest single-overflow case) → confirm round-trip resume → `multicol-nested-012..032` sweep → `multicol-fill-balance-003, -026`.

**Tractability.** Hard. Largest of the three Phase 16+ targets. Touches the break-token type itself + 5 sites in `multicol_layout.go` + the Phase 14b condition. Design risk: the polymorphic carrier shape may need to be generalized later; choosing `MulticolData *MulticolBreakTokenData` field is the minimal-blast-radius option.

---

### Phase 19 brief — span-all-children-height 002-013 cluster (target T4, 12 tests)

**Status.** Phase 15 (`containerPercentResolutionBlockSize`) closed test 001 only. Tests 002-013 fail with diffs ranging 0.3% to 26.8%. The cluster is **heterogeneous** — 7 distinct sub-clusters with different root causes. Phase 19 is best treated as a series of small targeted fixes, not a single phase.

**Sub-cluster decomposition (verified by diagnostic audit 2026-04-26).**

| Sub-cluster | Tests | Root cause hypothesis | Fix site | Tractability |
|---|---|---|---|---|
| **A** | 002, 003 | Height distribution for sections before/after spanner is wrong; `remainingContentBlockSize` not updated correctly post-spanner | `multicol_layout.go` Layout() loop ~1050-1100 (post-spanner remaining-height update) | Medium |
| **B** | 004a, 004b, 005, 006, 008 | Fixed-height descendant block split by spanners doesn't distribute its height proportionally across sections (this is **NOT** covered by Phase 15) | New `distributeHeightAcrossSpannerSections()` in `multicol_layout.go:~1100-1200` | Hard |
| **C** | 007 | Sub-pixel rounding in nested multicol spanner geometry (0.4% diff) | `layoutSpanner()` containerPercentResolutionBlockSize propagation when nested | Easy |
| **D** | 009, 010 | Negative margin-top on spanner uses column height for resolution, not container height | `layoutSpanner()` margin resolution (or `ResolveBlockSize` for spanner margin context) | Medium |
| **E** | 011 | Overflow column boundary rounding (0.3% diff) | gap geometry / paint boundary calc when spanners present | Easy |
| **F** | 012 | Abspos child uses fragmentainer size for containing block instead of multicol container size | `createConstraintSpaceForColumn` abspos handling, or `layoutElement` abspos branch | Medium |
| **G** | 013 | Multicol-in-fixed-wrapper doesn't use its own explicit height for `containerPercentResolutionBlockSize` when constrained by outer | `Layout()` initialization of `containerPercentResolutionBlockSize` | Easy |

**Recommended Phase 19 attack order.**
1. Sub-cluster B first (5 tests, largest diffs, novel logic for fixed-height-block-split-by-spanner). This is the dominant gain. The fix is NOT a refinement of Phase 15 — it's new logic.
2. Sub-cluster A (2 tests, refines Phase 15's remaining-height tracking).
3. Sub-cluster D (2 tests, margin context for spanner).
4. Sub-clusters C, E, G (3 tests, near-pass rounding fixes).
5. Sub-cluster F (1 test, abspos containing-block).

**Per-test detail (audit 2026-04-26).**

- **002 (1.3%).** `column-count: 2`, `height: 200px`. block1=100%=200px (2 cols × 100px), spanner=25%=50px, block2=100%=200px (overflows). Expected: 4 columns of 50px for block2 (2 in container + 2 overflow). Diff: block2 left column only ~15px tall instead of 50px. Root cause: `remainingContentBlockSize` after spanner placement gives wrong row height to block2.
- **003 (1.9%).** Same as 002 but block1=300% (overflows first), spanner=25%, block2=100%. Diff: spanner positioned too low. Root cause: spanner placement after block1 overflow doesn't account for container's height budget; uses content extent.
- **004a (26.8%).** Pink fixed-height container (450px) split by 2 spanners; each block inside is 200px fixed. NO percentage children. Diff: blocks stacked vertically in narrow columns instead of distributed. Root cause: anonymous multicol child block doesn't split its declared height across spanner-bounded sections. Phase 15 doesn't apply.
- **004b (15.5%).** Same as 004a but container 350px. Same root cause; smaller diff because overflow is less.
- **005 (8.3%).** `column-fill: auto`, single column, 250px container split by 2 spanners. Same Cluster B issue.
- **006 (15.0%).** Cluster B + 20px borders. Border-skipping logic for split-by-spanner is correct; height distribution is wrong.
- **007 (0.4%).** Nested multicol (outer 110px, inner 270px). Tiny diff in spanner region. Sub-pixel rounding in `layoutSpannerInFrag()`.
- **008 (13.3%).** Auto-height container split by spanners. Cluster B with auto height instead of fixed.
- **009 (2.1%).** 10-column multicol, 1000px child, spanner with `margin-top: -100px`. Negative margin should let spanner cover overflow. Diff: spanner doesn't extend up. Root cause: margin resolution uses column height, not `containerPercentResolutionBlockSize`.
- **010 (2.1%).** Spanner with negative margin inside `<span>` (inline-level wrapper). Same as 009 but inline parent context.
- **011 (0.3%).** 4-column multicol, overflow columns + spanner. Tiny black box at corner. Sub-pixel paint boundary.
- **012 (1.9%).** Abspos child + spanner. Diff: abspos sized wrong. Root cause: abspos uses fragmentainer size as containing block instead of multicol container size when `IsBlockSizeOverride=true`.
- **013 (0.6%).** Multicol inside 100px wrapper. Tiny spanner-position diff. `containerPercentResolutionBlockSize` should use multicol's own height, not be constrained by outer.

**Tractability summary.** B is the dominant gain (5 tests, hard). A/D are medium. C/E/G are easy near-pass fixes worth bundling together once we touch the geometry. F is an isolated abspos issue.

---

## Phase 16.e + 18 BUNDLED BRIEF (prep complete 2026-04-28) — SUPERSEDED 2026-04-28 by v2 below

**⚠️ THIS BRIEF (v1) IS SUPERSEDED.** Empirical refutation: v1's Cmt 3 produced 11/13 (not the predicted 13/13); the Path A spike showed clip removal alone is not mechanical (9/13) and walker WRITE-flat × no-clip introduces 4 additional regressions on `column-height-026/027` + `multicol-nested-030/031` (5/13). v1's 6-commit decomposition, "regressions restore" claim, and "mechanical 16.c.2 retry #3" framing are all empirically wrong.

v2 brief (next section below) is the authoritative plan as of 2026-04-28. v1 is preserved here for archaeology. Do not implement against v1.

---

## Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)

### Why v2 supersedes v1

v1 assumed `ClipBlockAxisOnly` could be removed mechanically AFTER the walker port (Phase 16.c.2 retry #3 queued at the end). The Path A spike (2026-04-28) refuted this on three counts:

1. **Clip removal is not mechanical.** Spike A (Cmt 2 + clip OFF): 9/13 — drops `spanner-fragmentation-004` and `nested-floated-multicol-with-monolithic-child` vs Cmt 2 baseline. 16.d.1's per-fragment clamp closes the upstream gap for 9 of 13 drivers but not all 13.
2. **Walker flat WRITE × no-clip has additional regressions.** Spike B (Cmt 3 + clip OFF): 5/13 — drops `column-height-026/027` + `multicol-nested-030/031` on top of Spike A. These tests pass under both Cmt 2 baseline AND Spike A AND Cmt 3 + clip ON; the 4-test failure is specific to walker-flat × no-clip. Mechanism unknown.
3. **The walker model and the clip path are conceptually incompatible.** OLD positional encoding's slot[0] driver provided BLA's child-cursor advance as a side effect of the unconditional layoutLine call. The walker model elides layoutLine on direct spanner dispatch — losing cursor advance. With clip retained, this gap is invisible (clip masks the post-spanner content discovery failure); with clip removed, the gap becomes visible.

v1's prescription (walker + clip removal in 6 commits, with the clip removal trailing as a "mechanical" cleanup) cannot land. v2 reorders: clip path is removed FIRST via the upstream Blink mechanism (Phase 16.d.2/3 `TallestUnbreakableBlockSize` carrier), and the walker port runs only after the clip is gone, ensuring louis14 has no clip-dependent code paths the walker can't encode.

This is bigger than v1 (8 commits vs 6) but is the only conceptually clean port. v1's incremental "small bundled change" framing was mismatched to the actual complexity.

### Strategic framing

**Goal.** End state: louis14 multicol has Blink-parity walker dispatch, no `ClipBlockAxisOnly` workaround, and `MulticolBreakTokenData.ConsumedRowBlockSize` carrier (Phase 18). All 13 driver invariants pass at 0 diff. Multicol gate target ≥ 211/455 (+15 from Phase 18 cluster), spanner-fragmentation cluster ≥ 12/13.

**Method.** Sequence ports so each commit verifies cleanly. Land carrier work BEFORE the walker port so clip-dependent paths disappear by the time walker is mandatory. Diagnose unknowns BEFORE committing (Step 0 below) so we don't re-run into a hard-exit blind.

**Empirical baseline (from 2026-04-28 spike, anchored to worktree `a8ea3adb`).**

| Configuration | PASS | Notes |
|---|---|---|
| Mainline (`fix/flexbox-fast`) | gate as-is | CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol 196/455 · spanner-frag 11/13 |
| Worktree Cmt 1 (`43ec8c66`, schema only) | 13/13 drivers | scaffold, behavior unchanged |
| Worktree Cmt 2 (`a8ea3adb`, walker READ + positional WRITE) | 11/13 | -001 0.8%, -006 1.4% — `MoveToSpanner` clobbers parent-token slots |
| Spike A (Cmt 2 + clip OFF) | 9/13 | + -004 1.0%, nested-floated 0.2% NEW fail |
| Spike B (Cmt 3 + clip OFF) | 5/13 | Spike A + column-height-026/027 + multicol-nested-030/031 NEW fail |
| Cmt 3 attempt + clip ON | 11/13 | same regressions as Cmt 2 (walker flat WRITE × clip-on does not regress 026/027/030/031, but does not fix -001/-006) |

### Authoritative Blink references

The v1 brief's references for the walker port and Phase 18 carrier are still valid; do not re-fetch:
- `multicol_break_token_data.h` (verbatim in v1).
- `break_token_algorithm_data.h` (verbatim in v1).
- `column_layout_algorithm.cc:41-223` (`MulticolPartWalker` inline).
- `column_layout_algorithm.cc:605-714` (LayoutChildren loop).
- `column_layout_algorithm.cc:733-738` (cleanup loop).
- `column_layout_algorithm.cc:822-833` (carrier write site).
- `column_layout_algorithm.cc:2122-2139` (carrier read site).

For 16.d.2/3 (`TallestUnbreakableBlockSize`), references already captured in findings.md § "Phase 16.d Blink research (2026-04-27)" lines 627-721:
- `box_fragment_painter.cc:1080-1114` (no painter-side clip — refutes paint-time hypothesis).
- `block_break_token.h:96-106` (`MonolithicOverflow` is print-only — NOT the multicol mechanism).
- `fragmentation_utils.cc:1105-1113` (`BreakBeforeChildIfNeeded` setter — `PropagateTallestUnbreakableBlockSize` for `ShouldAvoidBreakInside` children during initial balancing pass).
- `fragmentation_utils.cc:510-514` (`SetupFragmentation` border/padding contribution).
- `box_fragment_builder.cc:566-569` (child-result propagation).
- `column_layout_algorithm.cc:1879-1948` (consumer — `tallest_unbreakable_block_size_` accumulation + outer-frag forwarding).

### Implementation sequencing

**Branch:** worktree `phase-16e-18-walker-carrier`. Cmts 1+2 already landed (`43ec8c66`, `a8ea3adb`). v2 builds 6 more commits on top.

#### Step 0 — DIAGNOSTIC (no code commit, mainline-friendly)

Before any code change in v2, trace the break-token chain on `column-height-026` under three configurations and document the divergence:

(a) Cmt 2 baseline (clip ON, walker READ only) — passing.
(b) Spike A reproduce (Cmt 2 + clip OFF) — passing. Clip removal alone doesn't break this test.
(c) Spike B reproduce (Cmt 3 stash applied + clip OFF) — failing at 1.0%. Walker WRITE flat × no clip breaks this test.

Capture per outer-column resume: incoming break token shape (children list with Node identities, ConsumedBlockSize, HasSeenAllChildren, IsBreakBefore), 16.d.1 clamp inputs (constraint space FragmentainerBlockSize, FragmentainerOffset, IsInsideColumnSpanner, BreakToken.ConsumedBlockSize), and outgoing fragment block-size.

**Output of Step 0:** A concrete hypothesis about which 16.d.1 clamp condition diverges between (b) and (c). Likely candidates:
- Walker WRITE flat zeroes some field that 16.d.1 reads (e.g., HasSeenAllChildren or SequenceNumber, both of which the v1 brief's `{Node:s, ChildBreakTokens:..., ConsumedBlockSize:0}` proposal would have stripped).
- Walker WRITE flat changes the child-token order in a way that 16.d.1's clamp consumes the wrong child's resume info.
- The walker's flushWalker cleanup pushes a column-content driver token whose ConsumedBlockSize accumulates wrong on resume.

**Hard exit:** if Step 0 doesn't yield a concrete hypothesis (i.e., the diff between (b) and (c) is not localized to a few break-token fields), STOP. v2 doesn't proceed without this. Re-evaluate whether to do Option B (walker-with-clip-shim) or pause the bundled phase entirely.

Step 0 cost: ~1-2 hours instrumenting break-token logging in the multicol algorithm. No commits. Output: a paragraph in findings.md describing the divergence.

#### Commit B1 — `TallestUnbreakableBlockSize` field on `LayoutResult`

Add `TallestUnbreakableBlockSize float64` field on `LayoutResult` (mirrors Blink's `LayoutResult::tallest_unbreakable_block_size_`). Add `PropagateTallestUnbreakableBlockSize(LayoutUnit)` method on `BoxFragmentBuilder` (mirrors Blink's `BoxFragmentBuilder::PropagateTallestUnbreakableBlockSize`). Set the field in `Build()`. NOT yet wired into `BreakBeforeChildIfNeeded` or column algorithm.

Verification: build clean. 13 drivers PASS. Multicol gate unchanged.

#### Commit B2 — Wire `TallestUnbreakableBlockSize` propagation

Three sites (mirror Blink fragmentation_utils.cc):

(a) `pkg/layout/fragmentation_utils.go` `BreakBeforeChildIfNeeded`: when `space.IsInitialColumnBalancingPass && ShouldAvoidBreakInside(space, layoutResult)`, call `builder.PropagateTallestUnbreakableBlockSize(CalculateUnbreakableBlockSize(space, layoutResult, fragmentainer_offset))`. Add `CalculateUnbreakableBlockSize` helper (mirrors Blink fragmentation_utils.cc's helper of the same name).

(b) `pkg/layout/fragmentation_utils.go` `SetupFragmentation` (or equivalent): when `space.IsInitialColumnBalancingPass`, propagate border + padding block-start and block-end as floors.

(c) `pkg/layout/box_fragment_builder.go` (or block_layout's child-loop): when laying out a child during the initial balancing pass, propagate the child's `TallestUnbreakableBlockSize` upward via `PropagateTallestUnbreakableBlockSize(child_layout_result.TallestUnbreakableBlockSize)`.

Then in `pkg/layout/multicol_layout.go` `resolveColumnAutoBlockSize` (around the existing TODO at `:1601`):
- Replace `// TODO(Phase 16.d.2/3): tallestUnbreakable = ...` with `tallestUnbreakable = max(tallestUnbreakable, result.TallestUnbreakableBlockSize)`.

Verification: build clean. 13 drivers PASS. Multicol gate ≥ 196 (must not drop). Spanner-frag 11/13 (must not drop). Tests likely to be affected: column-height-026/027, multicol-nested-030/031, multicol-fill-balance cluster — verify they don't regress.

**Hard exit B2:** any of 13 drivers regresses → carrier wiring is wrong; STOP. The path forward depends on which test regressed and how — record + diagnose.

#### Commit B3 — Mechanical clip removal (Phase 16.c.2 retry #3)

Delete:
- `pkg/layout/multicol_layout.go:1261-1275` (clip setter block).
- `pkg/render/paint_layer.go:274-296` (paint-time block-axis clip branch). Verify the paint code compiles without it.
- `pkg/layout/types.go:77-80` (`Box.ClipBlockAxisOnly`).
- `pkg/layout/layout_result.go:216-226` (`PhysicalFragment.ClipBlockAxisOnly`).
- `pkg/layout/engine.go:332` (propagation).

Verification: build clean. Drivers expected to pass per spike data + B2's TallestUnbreakable contribution:
- column-height-001/010/017/026/027 + multicol-nested-030/031: pass via 16.d.1 + 16.d.2/3 (already passed Spike A; B2 strengthens it).
- multicol-rule-nested-balancing-004 + nested-past-fragmentation-line: pass per Spike A.
- nested-floated-multicol-with-monolithic-child: spike A regressed at 0.2% — B2's carrier should close this if the float's monolithic-content height propagates.
- spanner-fragmentation-004: spike A regressed at 1.0% — B2's carrier might or might not close this depending on whether the spanner's content height propagates as `TallestUnbreakable`.
- spanner-fragmentation-001 + -006: pre-existing or partially clip-dependent failures; expected to track Cmt 2 baseline (-001 0.8%, -006 1.4% or thereabouts).

Target: ≥ 11/13 (matching Cmt 2 baseline). 13/13 if B2 + clip removal closes all clip-dependent paths.

**Hard exit B3:** if drivers drop below 11/13, B2's carrier missed something. STOP. Diagnose which test broke and which carrier hop is missing. Likely candidates: spanner is itself a `ShouldAvoidBreakInside` candidate and needs explicit `TallestUnbreakable` propagation; or the float in `nested-floated-multicol-with-monolithic-child` needs special handling.

#### Commit B4 — re-verify walker READ (Cmt 2 already landed)

No code change; verification commit (or skip and embed verification in B3). With clip gone, the regressions Cmt 2 introduced (-001, -006 at 0.8%/1.4%) should be unaffected because they don't involve the clip — but verify drivers stay at the post-B3 baseline.

#### Commit B5 — walker WRITE flat (was Cmt 3 attempt)

Apply the Cmt 3 stash (`git stash@{0}` in worktree) with adjustments per Step 0 diagnostic. The stash contains the `MulticolBreakTokenBuilder`-based outBuilder, the spanner content-overflow flat emission, the combined-clip flat emission, the spanner-branch [0]/[1] index drops, and the `flushWalker` cleanup. Adjust whatever Step 0 identified.

Stash pop won't be clean — the clip-only mid-spanner block is deleted by B3. Manual reconciliation: drop the `walker.Next()` + clip-token push from the stash's clip-only path (the entire `if hasOuterFrag && blockCursor+spanHeight > outerAvailable` block in the spanner branch is gone post-B3). The `pendingContentOverflow` combined-clip handling also simplifies because no second clip can occur. Remaining stash content (buildOuterBreakResult rewrite, content-overflow flat, AddBreakBeforeChild, flushWalker, post-loop pendingContentOverflow handler) applies cleanly.

Verification: 13 drivers PASS at 0 diff.

**Hard exit B5:** any driver regresses below post-B3 baseline → walker WRITE flat has a bug Step 0 didn't catch. STOP and re-trace.

#### Commit B6 — Phase 18 carrier WRITE site

Wire `MulticolBreakTokenData.ConsumedRowBlockSize` population at the row-advance failure branch in `multicol_layout.go`. Mirror Blink cla.cc:822-833 verbatim:

```
if shouldWrapColumns && hasRowHeight && isFirstRow && hasOuterFrag {
    overflow := rowHeight - (outerAvailable - blockCursor)
    if overflow > 0 {
        outgoingBreakToken.MulticolData = &MulticolBreakTokenData{
            ConsumedRowBlockSize: rowHeight - overflow,
        }
    }
}
```

The READ site is already plumbed at multicol_layout.go:294-297 (Cmt 1).

Verification: 13 drivers PASS. multicol-nested-011 (single-overflow case) closes. multicol-nested-012..032 + multicol-fill-balance-003/-026 sweep. Multicol gate target: 196 → 211+ (+15).

**Hard exit B6:** multicol-nested-010 regresses (Phase 16.c.1 column regrowth driver) → row-carry write site fires in wrong condition. Diagnose by tracing `consumed_row_block_size` against expected geometry.

#### Commit B7 — drop `IsInsideColumnSpanner` clamp gate (was v1 Commit 5)

Same as v1 Commit 5: remove `!bla.space.IsInsideColumnSpanner` from the 16.d.1 clamp gate, plus constraint-space propagation, plus setters in layoutSpanner / layoutSpannerInFrag.

Verification: 13 drivers PASS at 0 diff. spanner-fragmentation-006 must hold (was the load-bearing driver for the gate per Phase 16.d.1 retro).

**Hard exit B7:** spanner-fragmentation-006 regresses → 16.d.1 gate is still load-bearing; revert B7 and document.

#### Commit B8 — full gate sweep + worktree merge

No code change; verification commit. Run full multicol/spanner-fragmentation/css2/flex/position/wm sweep. Required gate:
- CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (invariants).
- multicol ≥ 211/455 (+15).
- spanner-frag ≥ 12/13 (+1 from -006 cleanly handled, or 11/13 minimum if -006 still residual).

If green, merge worktree branch back to `fix/flexbox-fast` via fast-forward or single squash commit.

### Test invariants (13, must hold at 0 diff at every commit B2 onward)

`column-height-001/010/017/026/027`, `multicol-nested-030/031`, `spanner-fragmentation-001/004/006`, `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`.

```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-multicol/(column-height-001|column-height-010|column-height-017|column-height-026|column-height-027|multicol-nested-030|multicol-nested-031|spanner-fragmentation-001|spanner-fragmentation-004|spanner-fragmentation-006|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)"
```

Plus a single sweep at B1 + B8 for the four other categories:
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/(css2|css-flexbox|css-position|css-writing-modes)"
```

### Hard exit conditions (consolidated)

1. Step 0 doesn't yield a concrete hypothesis → STOP, regroup. Don't run B1.
2. B2 regresses any of 13 drivers → carrier wiring wrong; STOP.
3. B3 drops drivers below 11/13 → B2's carrier missed something; STOP and diagnose which carrier hop is needed.
4. B5 regresses below post-B3 baseline → walker WRITE flat has a bug Step 0 didn't catch.
5. B6 regresses multicol-nested-010 → row-carry fires in wrong condition.
6. B7 regresses spanner-fragmentation-006 → 16.d.1 gate is still load-bearing; revert B7.
7. Multicol gate drops below 196 at any commit other than the deliberately-regressing Cmt 2 (already landed) → STOP.

### Risks + open questions

1. **`spanner-fragmentation-001` is INVARIANT under all 4 spike configurations at 0.8% diff.** This is NOT a clip / walker / carrier issue — it's a separate bug. Track as a separate phase, not a v2 hard-exit. v2 is a success even if -001 still fails at 0.8% post-B8.

2. **`spanner-fragmentation-006` oscillates 1.0%-1.5%** across configurations. May or may not be addressed by B2 + B3 + B5. If it doesn't pass post-B5, accept and continue; v2's gate target of "spanner-frag ≥ 12/13" tolerates this if needed.

3. **Step 0's Spike B mystery.** The 4-test cluster (column-height-026/027 + multicol-nested-030/031) failing under walker-flat × no-clip is the v2 critical path. Without diagnosis, B2 + B3 might fix them by accident (since B2 strengthens 16.d.1's clamp), or might not. If they pass post-B3, Step 0's diagnostic was less critical than feared and v2 proceeds smoothly. If they don't, B5 is likely where they manifest, and Step 0's diagnostic was load-bearing.

4. **Walker spannerPath.Box vs spanner-leaf in `AddBreakBeforeChild`.** The OLD positional code used `spannerPath.Box` (flow-thread top-level) for `beforeSpannerToken`'s break-before child Node. v1 Cmt 3's `outBuilder.AddBreakBeforeChild(spanner, ...)` uses the leaf. For nested spanner paths (where `spannerPath.Box != spanner`), the leaf-vs-path-top distinction matters. Verify spanner-fragmentation cluster doesn't need path-top break-before in flat encoding. If it does, `MulticolBreakTokenBuilder.AddBreakBeforeChild` needs to accept a path argument or extract path-top.

5. **Outer-multicol per-column threading of `MulticolData`.** When inner multicol emits `MulticolData` on its outgoing token, outer multicol's per-column `colBreakToken` plumbing (`multicol_layout.go:1069-1156`) must preserve the field. Since `MulticolData` is a struct field on `BlockBreakToken`, this should pass through unchanged — verify with a trace at B6.

6. **B5 stash pop conflicts.** Worktree `git stash@{0}` is the Cmt 3 attempt against post-Cmt-2 state. With B3 deleting the clip-only mid-spanner code path, stash pop will conflict on `multicol_layout.go` lines 786-825 (the spanFrag-doesn't-fit clip block). Resolve by accepting B3's deletion and dropping the stash's `walker.Next()` + clip-token push from that block. The `pendingContentOverflow` combined-clip case also disappears (no second clip can happen with no clip path).

### What v2 explicitly does NOT address

- `spanner-fragmentation-001`'s 0.8% pre-existing failure (separate phase).
- Phase 16.c.2 retry #3 as a SEPARATE commit — it's now B3 inline.
- Option 1 — Finish FinishFragmentation port (drop leaf-only gate in 16.d.1 + delete parent-side overflow path). Larger separate worktree phase.
- Phase 19 — span-all-children-height 002-013 (T4, 12 tests, MIXED). Independent.
- Generalizing `MulticolBreakTokenData *X` to polymorphic interface. Single-algorithm; generalize when grid/flex/table need it.
- Fragmented OOF-in-CB. Blink's runtime-flag-gated walker entry handling; louis14 ignores OOF walker entries for now.

### v1 → v2 mapping

| v1 commit | v2 commit | Status |
|---|---|---|
| Cmt 1 (schema + walker scaffold) | (already landed `43ec8c66`) | Reused |
| Cmt 2 (walker READ) | (already landed `a8ea3adb`) | Reused |
| Cmt 3 (walker WRITE flat) | B5 | Defers to B5 with stash + adjustments |
| Cmt 4 (Phase 18 carrier WRITE) | B6 | Renumbered |
| Cmt 5 (drop IsInsideColumnSpanner gate) | B7 | Renumbered |
| Cmt 6 (gate sweep + merge) | B8 | Renumbered |
| (NOT in v1) | Step 0 (diagnostic) | NEW — mandatory before B1 |
| (NOT in v1) | B1 (TallestUnbreakable field) | NEW — Phase 16.d.2/3 carrier port |
| (NOT in v1) | B2 (TallestUnbreakable propagation) | NEW — Phase 16.d.2/3 wiring |
| Phase 16.c.2 retry #3 (queued AFTER bundle) | B3 | Inlined into bundle, no longer trailing

**This is the authoritative brief.** Supersedes the earlier "Phase 16.e re-sequenced — bundled with Phase 18" sketch (lines ~846-895) which was annotated as "sketch — flesh out before starting." That sketch is preserved above for historical context but the implementation plan, file:line citations, and verification gates below are the binding ones.

### Strategic framing

We are doing TWO Blink-parity ports as one bundled change because they share a break-token-shape mutation:

- **Phase 16.e**: port Blink's `MulticolPartWalker` model. Replaces louis14's positional `ChildBreakTokens[0..2]` slot encoding (`nextColToken / partialSpannerToken / colRowsResumeToken`) with a flat document-order list dispatched by `Node` identity. Unlocks spanner-content fragmentation correctness so the workaround clip (`ClipBlockAxisOnly`) can be dropped (Phase 16.c.2 retry #3 becomes mechanical).
- **Phase 18**: add Blink's `MulticolBreakTokenData` carrier on `BlockBreakToken`. Stores `consumed_row_block_size` so a row that overflows an outer fragmentainer can resume mid-stride in the next outer fragmentainer. Read site already plumbed (`multicol_layout.go:204-217` `offsetInCurrentRow` reads `mla.consumedRowBlockSize`); only the carrier-on-token + the write site are missing.

Bundled because the carrier needs to be a field on `BlockBreakToken`, and the walker port rewrites every site that constructs/reads `BlockBreakToken` for multicol. Doing them sequentially means re-touching 6 entangled sites twice. Bundling is one carrier-and-walker port.

### Authoritative Blink references (fetched 2026-04-28)

`third_party/blink/renderer/core/layout/multicol_break_token_data.h` (verbatim, current Blink main):

```cpp
struct MulticolBreakTokenData final : BreakTokenAlgorithmData {
  explicit MulticolBreakTokenData(LayoutUnit consumed_row_block_size)
      : BreakTokenAlgorithmData(kMulticolData),
        consumed_row_block_size(consumed_row_block_size) {}
  LayoutUnit consumed_row_block_size;
};
template <>
struct DowncastTraits<MulticolBreakTokenData> {
  static bool AllowFrom(const BreakTokenAlgorithmData& token_data) {
    return token_data.IsMulticolType();
  }
};
```

`third_party/blink/renderer/core/layout/break_token_algorithm_data.h` (verbatim):

```cpp
struct BreakTokenAlgorithmData : public GarbageCollected<BreakTokenAlgorithmData> {
  enum DataType {
    kFieldsetData, kFlexData, kGridData, kTableData, kTableRowData, kMulticolData,
  };
  DataType Type() const { return static_cast<DataType>(type); }
  explicit BreakTokenAlgorithmData(DataType type) : type(type) {}
  bool IsMulticolType() const { return Type() == kMulticolData; }
  unsigned type : 3;
};
```

`MulticolPartWalker` is now defined inline at the top of `column_layout_algorithm.cc` (lines 41-223 of current Blink main; the cited `multicol_part_walker.cc/h` files no longer exist as separate files):

```cpp
class MulticolPartWalker {
 public:
  struct Entry {
    const BlockBreakToken* break_token = nullptr;  // null at start
    BlockNode descendant_node = nullptr;            // spanner OR fragmented OOF, null for column content
  };
  MulticolPartWalker(BlockNode multicol_container, const BlockBreakToken* break_token);
  Entry Current() const;
  bool IsFinished() const;
  void Next();
  void MoveToSpanner(BlockNode spanner, const BlockBreakToken* next_column_token);
  void AddNextColumnBreakToken(const BlockBreakToken& next_column_token);
  void UpdateNextColumnBreakToken(const FragmentBuilder::ChildrenVector& children);
 private:
  void MoveToNext();
  void UpdateCurrent();
  Entry current_;
  BlockNode descendant_node_ = nullptr;
  BlockNode multicol_container_;
  const BlockBreakToken* parent_break_token_;
  const BlockBreakToken* next_column_token_ = nullptr;
  wtf_size_t child_token_idx_;
  bool is_finished_ = false;
};
```

Iteration semantics (from `UpdateCurrent` at cla.cc:157-193 + `MoveToNext` at cla.cc:195-223):

1. Walk `parent_break_token_->ChildBreakTokens()` by `child_token_idx_`. Each entry's `InputNode()` discriminates:
   - `InputNode() == multicol_container_` → column-content resume (Entry has `break_token` set, `descendant_node` null).
   - `InputNode().IsColumnSpanAll()` → spanner resume (Entry has both set; `descendant_node` is the spanner).
   - `InputNode().IsOutOfFlowPositioned()` → fragmented OOF (gated on `RuntimeEnabledFeatures::FragmentedOofInCbEnabled()`; we ignore for now).
2. After incoming child tokens exhausted: optional `descendant_node_` (a spanner moved-to via `MoveToSpanner` after a fresh discovery via `result->GetColumnSpannerPath()`).
3. After that: optional `next_column_token_` (the trailing column-content resume token from the most recent LayoutLine result).
4. `is_finished_ = true` when all three are exhausted.

The driver loop in `LayoutChildren` (cla.cc:605-714):

```cpp
MulticolPartWalker walker(Node(), GetBreakToken());
while (!walker.IsFinished()) {
  auto entry = walker.Current();
  if (!entry.descendant_node) {
    // Column content (or initial state). LayoutLine through LayoutFragmentationContext.
    const LayoutResult* result = LayoutFragmentationContext(entry.break_token, &margin_strut);
    walker.Next();
    const auto* next_column_token = To<BlockBreakToken>(result->GetPhysicalFragment().GetBreakToken());
    if (const auto* path = result->GetColumnSpannerPath()) {
      walker.MoveToSpanner(GetSpannerFromPath(path), next_column_token);
      continue;
    }
    if (next_column_token) walker.AddNextColumnBreakToken(*next_column_token);
    break;
  }
  if (entry.descendant_node.IsColumnSpanAll()) {
    BreakStatus s = LayoutSpanner(entry.descendant_node, entry.break_token, &margin_strut);
    walker.Next();
    if (s == BreakStatus::kBrokeBefore || container_builder_.HasInflowChildBreakInside()) break;
  }
}
// Cleanup: any unfinished walker entries get pushed forward as outgoing break tokens.
for (; !walker.IsFinished(); walker.Next()) {
  auto entry = walker.Current();
  if (entry.break_token) container_builder_.AddBreakToken(entry.break_token);
  else if (entry.descendant_node) container_builder_.AddBreakBeforeChild(entry.descendant_node, ...);
}
```

The carrier write site (cla.cc:822-833, inside `LayoutFragmentationContext`'s do-while):

```cpp
if (ShouldWrapColumns() && HasRowHeight() && is_first_row &&
    GetConstraintSpace().HasKnownFragmentainerBlockSize()) {
  LayoutUnit overflow = RemainingRowHeightAtOffset(line_offset) -
                        (FragmentainerSpaceLeftForChildren() - line_offset);
  if (overflow > LayoutUnit()) {
    container_builder_.SetBreakTokenData(
        MakeGarbageCollected<MulticolBreakTokenData>(RowHeight() - overflow));
  }
}
```

The carrier read site (cla.cc:2122-2139, `OffsetInCurrentRow`):

```cpp
LayoutUnit ColumnLayoutAlgorithm::OffsetInCurrentRow(LayoutUnit line_offset) const {
  LayoutUnit row_stride = RowHeight() + row_gap_size_;
  if (row_stride == LayoutUnit()) return LayoutUnit();
  if (GetBreakToken()) {
    if (const auto* data = DynamicTo<MulticolBreakTokenData>(GetBreakToken()->TokenData())) {
      line_offset += data->consumed_row_block_size;
    }
  }
  return CurrentContentBlockOffset(line_offset) % row_stride;
}
```

### Louis14 entangled sites — current line numbers (post-Phase 17, file 2109 lines)

| # | Site | File:line | What it does today | What changes |
|---|---|---|---|---|
| 1 | **3-slot positional read parser** | `pkg/layout/multicol_layout.go:419-447` | Extracts `nextColToken / partialSpannerToken / colRowsResumeToken` from `ChildBreakTokens[0/1/2]`; further extracts `spannerContentBreakToken` and `nextSpannerClipToken` from slot[1].ChildBreakTokens[0/1] | Replace with `Walker` constructor that records the parent break token + initial `child_token_idx_=0`. The `hasSpannerResume` / `spannerConsumed` / `spannerContentBreakToken` / `nextSpannerClipToken` locals are derived from walker entries on demand. |
| 2 | **Pure-nested-resume promotion** | `pkg/layout/multicol_layout.go:449-456` | Promotes `colRowsResumeToken` to `nextColToken` when no spanner state | Becomes redundant: in the flat list, the FIRST entry is whatever needs to resume next, regardless of whether a spanner intervened. |
| 3 | **Spanner content-overflow build site** | `pkg/layout/multicol_layout.go:721-728` | Wraps `fullResult.BreakToken` as `{Node:spanner, ChildBreakTokens:[fullResult.BreakToken]}` | Emit a single `BlockBreakToken{Node: spanner, ChildBreakTokens: fullResult.BreakToken.ChildBreakTokens, ConsumedBlockSize: 0}` (no wrapping). The spanner-content break info is the spanner's *own* token's children, not a wrapper. |
| 4 | **Combined-clip mutation** | `pkg/layout/multicol_layout.go:765-770` | `pendingPartialSpannerToken.ChildBreakTokens = append(..., clipToken)` — appends a clip-resume sibling to the content-overflow token | The minimal-port reversion (findings.md § "Attempted 2026-04-27") proved this site is the defensive-copy boundary that the wrapper provides. Replacement: emit the clipToken as a SEPARATE walker entry (separate flat ChildBreakTokens entry), not nested. |
| 5 | **Mid-spanner partial-token return** | `pkg/layout/multicol_layout.go:775-779` | `partialSpannerToken := &BlockBreakToken{Node: spanner, ConsumedBlockSize: available}` for clip-only return | Same shape but pushed into the walker's outgoing `child_break_tokens` flat list as the spanner-row entry. |
| 6 | **Next-spanner-clip-chain consumption** | `pkg/layout/multicol_layout.go:822-832` | After OC2 places content-overflow spanner, reads `nextSpannerClipToken` (slot [1][1]) and sets `hasSpannerResume=true` to engage clip-resume on the *next* spanner | In the flat model, `nextSpannerClipToken` is just the next walker entry whose `Node.IsColumnSpanAll()`. Walker.Next() advances naturally. |
| 7 | **`buildOuterBreakResult` outgoing-token construction** | `pkg/layout/multicol_layout.go:480-525` | Emits `[nextColToken, partialSpannerToken, pendingColRowsBreakToken]` 3-slot vector with trailing-nil trim | Emit flat document-order list. Use a `Walker.PushOutgoing(token)` accumulator + `Walker.PushSpannerBreakBefore(node)` to mirror Blink's `AddBreakToken` / `AddBreakBeforeChild`. |
| 8 | **`BlockBreakToken` shape** | `pkg/layout/break_token.go:8-60` | Flat record; `ChildBreakTokens []*BlockBreakToken` is positional for multicol callers, document-order for everyone else | Add `MulticolData *MulticolBreakTokenData`. Document `ChildBreakTokens` as flat document-order for ALL callers; multicol callers stop using slot indices. |
| 9 | **Carrier read site (Phase 18)** | `pkg/layout/multicol_layout.go:294-297` | Hardcoded `mla.consumedRowBlockSize = 0` with TODO comment | Read from `mla.space.BreakToken.MulticolData.ConsumedRowBlockSize` when non-nil. Existing `offsetInCurrentRow` (`:191-204`) already consumes the field. |
| 10 | **Carrier write site (Phase 18, NEW)** | NEW callsite in `MulticolLayoutAlgorithm.Layout` after a layoutLine call that returned a `remainingToken` | Currently absent | Mirror Blink cla.cc:822-833: when `shouldWrapColumns && hasRowHeight && isFirstRow && hasOuterFrag && (rowHeight - (outerAvailable - blockCursor)) > 0`, attach `MulticolData{ConsumedRowBlockSize: paintedAmount}` to the outgoing break token via the build helper. `paintedAmount = rowHeight - overflow = outerAvailable - blockCursor`. |
| 11 | **`IsInsideColumnSpanner` clamp gate** | `pkg/layout/block_layout.go:1426-1448` (Phase 16.d.1 clamp); `block_layout.go:606-614` (constraint-space propagation); `pkg/layout/constraint_space.go:171-191, 494-505` (field+setters); `pkg/layout/multicol_layout.go:1426, 1459` (setters in `layoutSpanner`/`layoutSpannerInFrag`) | Disables 16.d.1 self-fragmentation clamp on spanner descendants because the spanner-resume mechanism doesn't handle leaf-self-fragmentation chains | Step 5 of the bundled work removes this gate (and the supporting field). With the walker model, the spanner's own break-token's `ChildBreakTokens` carry self-fragmented leaf descendants naturally; the gate becomes redundant. |
| 12 | **`ClipBlockAxisOnly` setter + paint branch** | `pkg/layout/multicol_layout.go:1281-1290`; `pkg/render/paint_layer.go:274-296`; `pkg/layout/engine.go:332`; `PhysicalFragment.ClipBlockAxisOnly`; `Box.ClipBlockAxisOnly` | Per-column block-axis clip workaround for spanner-content overflow + nested-monolithic patterns | NOT removed in this phase. Phase 16.c.2 retry #3 is queued AFTER the bundled work lands and verifies the walker handles all clipping cases. Mechanical removal commit. |

### Target shape

`pkg/layout/break_token.go` becomes:

```go
// MulticolBreakTokenData mirrors Blink's MulticolBreakTokenData
// (multicol_break_token_data.h). Carries algorithm-specific resume state for
// MulticolLayoutAlgorithm; nil for tokens emitted by other algorithms.
type MulticolBreakTokenData struct {
    // ConsumedRowBlockSize is the portion of the current column-row block
    // size that was painted in earlier outer fragmentainers. Mirrors Blink's
    // consumed_row_block_size. Used by MulticolLayoutAlgorithm.offsetInCurrentRow
    // to add row progress before the modulo-against-row_stride.
    ConsumedRowBlockSize float64
}

type BlockBreakToken struct {
    Node *LayoutInputNode
    ConsumedBlockSize layoutunit.LayoutUnit
    SequenceNumber int
    // ChildBreakTokens is flat document-order. Each entry's Node identifies its
    // role to multicol callers via Walker dispatch:
    //   Node == multicol container's content node → column-content resume.
    //   Node.IsColumnSpanAll() (style.GetColumnSpan() == "all") → spanner.
    //   Position-fixed/absolute → fragmented OOF (not yet handled).
    // Non-multicol callers continue treating ChildBreakTokens as
    // direct-child resume tokens by node identity.
    ChildBreakTokens []*BlockBreakToken
    IsBreakBefore bool
    IsForcedBreak bool
    IsCausedByColumnSpanner bool
    HasSeenAllChildren bool
    MonolithicOverflow float64
    InlineItemStartIndex int
    InlineTextOffset int
    HasUnpositionedListMarker bool
    // MulticolData carries MulticolLayoutAlgorithm-specific resume state.
    // Nil for tokens emitted by other algorithms. Mirrors Blink's
    // BlockBreakToken::data_ + MulticolBreakTokenData (multicol_break_token_data.h).
    MulticolData *MulticolBreakTokenData
}
```

Walker (placed in new file `pkg/layout/multicol_part_walker.go`, mirroring Blink's source location even though Blink now defines it inline — louis14 keeps it as a separate file because Go file boundaries are per-package and the multicol algorithm file is already 2109 lines):

```go
// MulticolPartWalker mirrors Blink's MulticolPartWalker
// (column_layout_algorithm.cc:41-223). Iterates the parts of a multicol
// container that need separate layout: regular column content, column
// spanners, and (eventually) fragmented OOF positioned descendants whose
// containing block is the multicol container.
type MulticolPartWalker struct {
    multicolContainer *LayoutInputNode  // the anonymous content node
    parentBreakToken  *BlockBreakToken  // incoming break token (may be nil)
    nextColumnToken   *BlockBreakToken  // trailing column-content (set by AddNextColumnBreakToken)
    descendantNode    *LayoutInputNode  // discovered spanner (set by MoveToSpanner)
    childTokenIdx     int               // index into parentBreakToken.ChildBreakTokens
    current           MulticolPartWalkerEntry
    isFinished        bool
}

type MulticolPartWalkerEntry struct {
    BreakToken     *BlockBreakToken  // resume token; nil at very start
    DescendantNode *LayoutInputNode  // spanner OR fragmented OOF; nil for column content
}

func NewMulticolPartWalker(container *LayoutInputNode, parentToken *BlockBreakToken) *MulticolPartWalker { ... }
func (w *MulticolPartWalker) Current() MulticolPartWalkerEntry { ... }
func (w *MulticolPartWalker) IsFinished() bool { ... }
func (w *MulticolPartWalker) Next() { ... }
func (w *MulticolPartWalker) MoveToSpanner(spanner *LayoutInputNode, nextColumnToken *BlockBreakToken) { ... }
func (w *MulticolPartWalker) AddNextColumnBreakToken(token *BlockBreakToken) { ... }
// updateCurrent + moveToNext — internal, mirror Blink's UpdateCurrent/MoveToNext.
```

Outgoing-token accumulator (replaces `buildOuterBreakResult`'s positional emission):

```go
// MulticolBreakTokenBuilder accumulates outgoing child break tokens in
// document order. Mirrors Blink's container_builder_.AddBreakToken /
// AddBreakBeforeChild calls.
type MulticolBreakTokenBuilder struct {
    children []*BlockBreakToken
}

func (b *MulticolBreakTokenBuilder) AddBreakToken(t *BlockBreakToken) { ... }
func (b *MulticolBreakTokenBuilder) AddBreakBeforeChild(node *LayoutInputNode, isForced bool) { ... }
func (b *MulticolBreakTokenBuilder) Children() []*BlockBreakToken { ... }
```

### Implementation plan — worktree commit decomposition

**Branch:** `phase-16e-18-walker-carrier` (in worktree under `~/louis14-worktrees/phase-16e-18`).

**Commit 1 — schema + walker scaffold (build clean, behavior unchanged).**

Changes:
- `pkg/layout/break_token.go`: add `MulticolData *MulticolBreakTokenData` field + `MulticolBreakTokenData` struct. Document `ChildBreakTokens` as flat document-order.
- `pkg/layout/multicol_part_walker.go` (new): walker type + `MulticolBreakTokenBuilder` type. NOT yet wired into `multicol_layout.go`.
- `pkg/layout/multicol_layout.go:294-297`: read `mla.space.BreakToken.MulticolData.ConsumedRowBlockSize` when non-nil. Field is always nil at this point so behavior is unchanged.

Verification:
- Build: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...`
- 13 driver tests must PASS at 0 diff.
- Multicol gate sweep should be unchanged at 196/455.

**Commit 2 — switch READ site to walker dispatch (expect spanner-fragmentation regressions).**

Changes:
- `pkg/layout/multicol_layout.go:419-456`: replace 3-slot positional parser + pure-nested-resume promotion with `walker := NewMulticolPartWalker(contentNode, mla.space.BreakToken)`.
- The main loop body (currently lines 535-864) consumes `walker.Current()` and calls `walker.Next()` instead of consuming positional locals.
- `nextColToken / hasSpannerResume / spannerConsumed / spannerContentBreakToken / nextSpannerClipToken / colRowsResumeToken` locals are derived from walker entries on demand, not persisted across iterations.

Expected regressions: spanner-fragmentation cluster, multicol-nested-* cluster (because the WRITE site still emits positional 3-slot tokens — read-side reinterprets them as a flat list and gets the wrong dispatch).

This step's `git commit` body should explicitly note "EXPECTED to regress; restored in Commit 3."

**Commit 3 — switch WRITE site to flat ChildBreakTokens (regressions restore).**

Changes:
- `pkg/layout/multicol_layout.go:480-525` (`buildOuterBreakResult`): rewrite to emit `MulticolBreakTokenBuilder.Children()` flat document-order.
- `pkg/layout/multicol_layout.go:721-728` (spanner content-overflow): emit `walker.PushOutgoing({Node: spanner, ChildBreakTokens: fullResult.BreakToken.ChildBreakTokens, ConsumedBlockSize: 0})` — no wrapper.
- `pkg/layout/multicol_layout.go:765-770` (combined-clip): emit clip and content-overflow as separate walker entries; do NOT mutate the content-overflow token.
- `pkg/layout/multicol_layout.go:775-779` (clip-only): push as a flat entry.
- `pkg/layout/multicol_layout.go:822-832` (next-spanner-clip-chain): walker handles automatically.

Verification:
- 13 driver tests must PASS at 0 diff.
- spanner-fragmentation cluster must hold at 11/13 minimum (target: improve to 12/13 if walker fixes -006 properly).
- Multicol gate sweep must hold at 196/455 minimum.

**Commit 4 — wire MulticolBreakTokenData WRITE site (Phase 18).**

Changes:
- `pkg/layout/multicol_layout.go`: in the row-advance failure branch (after a `layoutLine` returns a `remainingToken` but the next row won't fit in the outer fragmentainer), before returning the outer break result, attach `MulticolData{ConsumedRowBlockSize: paintedAmount}`. Mirror Blink cla.cc:822-833 logic literally.
- `pkg/layout/multicol_layout.go` Phase 14b defer: revisit the `columnFill == "auto"` gate at `:386-405` to also handle `column-fill: balance` cases that should now go through row-carry instead of the entire-defer path.

Verification:
- 13 driver tests must PASS at 0 diff.
- multicol-nested-011 must close (single-overflow case — primary target).
- multicol-nested-012..032 sweep + multicol-fill-balance-003/-026.
- Multicol gate target after this commit: 196 → 211+ (+15 from Phase 18 cluster).

**Commit 5 — drop `IsInsideColumnSpanner` clamp gate.**

Changes:
- `pkg/layout/block_layout.go:1426-1448`: remove `!bla.space.IsInsideColumnSpanner` from the Phase 16.d.1 clamp gate. Spanner descendants self-fragment normally now.
- `pkg/layout/block_layout.go:606-614`: remove constraint-space propagation.
- `pkg/layout/multicol_layout.go:1426, 1459`: remove `SetIsInsideColumnSpanner(true)` calls.
- `pkg/layout/constraint_space.go:171-191, 494-505`: remove the field + setter.

Verification:
- 13 driver tests must PASS at 0 diff (especially spanner-fragmentation-006 — without the gate, the leaf 360h descendant self-fragments via 16.d.1 + the walker correctly forwards the leaf break token in the flat list).
- spanner-fragmentation cluster: target 12/13 (+1 from -006 cleanly handled, or stay at 11/13 if -006 still needs the gate; that's the hard exit signal).
- Multicol gate must be net-positive vs Commit 4.

**Commit 6 — full gate sweep + worktree merge.**

Changes:
- None — verification commit.

Verification:
- Full multicol/spanner-fragmentation/css2/flex/position/wm gate sweep.
- Required gate: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol ≥ 211/455 · spanner-frag ≥ 11/13.
- If green: merge worktree branch back to `fix/flexbox-fast` via fast-forward or single squash commit (preserve the 6 commit history if possible — they document the staged port for future archaeology).
- If red: STOP and diagnose; do not merge.

### Test invariants (13 must hold at 0 diff at every commit)

`column-height-001/010/017/026/027`, `multicol-nested-030/031`, `spanner-fragmentation-001/004/006`, `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`.

Run after each commit:

```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-multicol/(column-height-001|column-height-010|column-height-017|column-height-026|column-height-027|multicol-nested-030|multicol-nested-031|spanner-fragmentation-001|spanner-fragmentation-004|spanner-fragmentation-006|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)"
```

Plus the four other category invariants — these are usually unaffected by multicol changes, so a single sweep at Commit 1 + Commit 6 is enough:

```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/(css2|css-flexbox|css-position|css-writing-modes)"
```

### Hard exit conditions (STOP, DO NOT chase with predicates)

1. Commit 3 doesn't restore the spanner-fragmentation regressions from Commit 2 → walker write-site mapping is wrong; re-read Blink cla.cc:605-714 + 1397-1522 and diagnose. Don't pile predicates on top.
2. Commit 5 regresses spanner-fragmentation-006 → walker doesn't forward leaf-self-fragmentation chains correctly through the spanner's own break token. STOP. The original 16.d.1 gate is still load-bearing; revert Commit 5 and document the residual.
3. Commit 4 regresses `multicol-nested-010` (Phase 16.c.1 column regrowth driver) → row-carry write site is firing in the wrong condition or the walker's column-rows dispatch is wrong. Diagnose by tracing `consumed_row_block_size` against expected geometry.
4. Multicol gate drops below 196 at any commit other than Commit 2 → unexpected; STOP and diagnose before continuing.

### Risks + open questions

1. **Walker behavior on `nextColumnToken_` after spanner discovery.** In Blink, `MoveToSpanner` resets `parent_break_token_=nullptr` and stores `next_column_token_` separately. This means the walker sees: `[spanner_node, nextColumnToken]` — spanner first, then column-content resume. Louis14 must mirror this exactly; getting the order wrong loses the post-spanner column-content resume.
2. **`UpdateNextColumnBreakToken` (cla.cc:143-155).** Updates the `next_column_token_` after column content has been laid out so the resume point reflects the latest fragment's break token. Louis14 doesn't have an equivalent because we currently re-derive the resume from `remainingToken` — porting the walker should preserve this semantics through the existing `nextColToken` re-assignments at `multicol_layout.go:580, 828, 859`.
3. **`HasInflowChildBreakInside` predicate.** Blink uses it to short-circuit the loop after a spanner caused a break. Louis14 doesn't expose this directly; our equivalent is `mla.space.BreakToken == nil && hasOuterFrag && remainingToken != nil`. Verify the equivalent fires in the same conditions during Commit 3.
4. **Phase 14b defer interaction.** The current `columnFill == "auto" && outerAvailable < explicitBlockSize` defer at `multicol_layout.go:386-405` returns early before the walker even constructs. It should keep working unchanged because it's a fast path BEFORE the walker engages. Confirm no regression on multicol-nested-009 (which depends on this).
5. **Pointer identity for spanner break tokens.** Blink uses `child_break_token != next_column_token_` (cla.cc:153) to detect that the column content has progressed. Go pointer comparison works the same way; ensure walker's `next_column_token_` is set to the actual outgoing token from `result->GetPhysicalFragment().GetBreakToken()`, not a copy.
6. **`MulticolData` propagation through outer-multicol per-column threading.** When the inner multicol emits `MulticolData{ConsumedRowBlockSize}` on its outgoing break token, the outer multicol's per-column break-token plumbing (`colBreakToken` at `multicol_layout.go:1069-1156`) must preserve `MulticolData` when threading the inner's `BlockBreakToken` forward. Since `MulticolData` is a field on the existing struct, the chain passes through unchanged — but verify with a unit test or trace before relying on it.

### What we are explicitly NOT doing in this bundle

- Phase 16.c.2 retry #3 (mechanical `ClipBlockAxisOnly` removal). Queued for AFTER bundled work lands. It's a separate mainline commit because the bundled work itself doesn't remove the clip — only verifies the walker handles all clipping cases so the clip becomes redundant.
- Option 1 (Finish FinishFragmentation port: drop the leaf-only gate in 16.d.1; delete or shrink the parent-side overflow path in `block_layout.go:1001-1196`). Larger separate worktree phase.
- Phase 19 (span-all-children-height 002-013, 7 sub-clusters). Independent; queue for after.
- Generalizing `MulticolBreakTokenData *X` to a polymorphic `BreakTokenAlgorithmData` interface. We have ONE algorithm carrying break-token data; generalize when grid/flex/table need it.
- Fragmented OOF-in-CB (Blink's `RuntimeEnabledFeatures::FragmentedOofInCbEnabled()`). The walker's OOF entry handling is gated on this flag in Blink (still being rolled out). Louis14 ignores OOF walker entries for now — the OOF layout part runs independently after column layout.

---

## Error Log

*(Add entries as failures are diagnosed — format: date, symptom, root cause, fix or status)*

**2026-04-28 — Phase 16.e+18 Commit 3 hit hard-exit #1.**

- **Symptom:** Faithful WRITE-side flattening (all 6 brief sites + `flushWalker` cleanup mirroring Blink cla.cc:733-738) produced the same 11/13 driver result as Commit 2. `spanner-fragmentation-001` unchanged at 0.8% diff; `-006` marginally improved 1.4%→1.0% but still failing.
- **Root cause:** louis14's `ClipBlockAxisOnly` workaround (Phase 12h F2 partial) creates a "clip-only mid-spanner" code path at `multicol_layout.go:786-790` (Commit 2 numbers). When a spanner clips at the outer fragmentainer boundary, the previous outer column's loop exits BEFORE enumerating post-spanner content (e.g., spanner_3 / trailing block in `-001`'s 3-spanner+block test). The OLD (pre-Commit-2) 3-slot encoding solved this via slot[0] = `beforeSpannerToken = {Node: contentNode, ChildBreakTokens: [{Node: spanner, IsBreakBefore: true}]}` — a column-content driver that on resume drove BLA via layoutLine to re-discover post-spanner content via spannerPath returns. The walker model elides this driver by design.
- **Status:** Reverted. Worktree restored to `a8ea3adb`. Commit 3 attempt diff preserved in worktree `git stash@{0}`. Brief corrected (see notice at top of "Phase 16.e + 18 BUNDLED BRIEF" section). DO NOT retry Commit 3 without re-confirming sequencing with operator.

**2026-04-28 — v2 B2.5 + B2.6 + B5 LANDED on worktree (`da5730b8`, `3b3b4208`, `33afa6fa`); operator-mandated PAUSE for review at 10/13.**

After hard-exit B3, operator approved options 1+2+3 in sequence. All three landed; driver count stayed at 10/13 throughout but `-004` improved meaningfully under B5 (2.1% → 1.0%).

- **B2.5 (`da5730b8`):** `IsMonolithic` flag on `PhysicalFragment` + extended `ShouldAvoidBreakInside` + extended `CalculateUnbreakableBlockSize`. Sources: `style.HasSizeContainment()` (CSS Containment 2 §2.6) and spanners with `IntrinsicBlockSize > fragment.BlockSize()`. Pre-step trace ruled out a B2 wiring bug on `-004` — its 2.1% diff under B0+clip-OFF (no B1/B2) confirmed B2 isn't actively wrong; the regression is the cache fix exposing -004's true non-clip layout. Detection verified to fire (via temp trace prints; subsequently removed). 10/13 unchanged: residuals are upstream of the carrier (measure-pass spanner handling + balanceColumns scope).
- **B2.6 (`3b3b4208`):** SetupFragmentation border/padding contribution at BLA Layout entry. Mirrors Blink fragmentation_utils.cc:510-514. 10/13 unchanged — none of the 3 residuals have meaningful borders. Hook wired for completeness.
- **B5 (`33afa6fa`):** walker WRITE flat — applied v1 Cmt 3 stash. Auto-merged cleanly with B3 + B2.5 + B2.6 (git's three-way merge dropped the stash's clip-only-mid-spanner edits since B3 deleted that block). Walker WRITE replaces 3-slot positional encoding with flat document-order via `MulticolBreakTokenBuilder`. **`-004` improved 2.1% → 1.0%** under B5 — the walker port is mechanically beneficial. `-006` slightly worsened 0.2% → 0.3% (sub-pixel). Stash is consumed.

**Diagnosed residual gaps (full text in B2.5 commit message):**

(a) `-004` / `-006`: `resolveColumnAutoBlockSize` measure pass exits at `spannerPath` BEFORE laying out the spanner. Spanner's `IsMonolithic` doesn't propagate via the measure-pass child loop. Blink's measure pass DOES lay out spanners. Closing requires extending the louis14 measure pass — a structural change.

(b) `nested-floated`: float's `column-fill:auto` + non-fragmented float context bypasses `IsInitialColumnBalancingPass` entirely. Carrier never fires for the float's `contain:size` child. Closing requires widening `balanceColumns` scope or a non-balancing-pass carrier path.

**Status:** PAUSED for operator review. Two upstream paths (X/Y in `CONTINUE-19.md`) or accepting 10/13 + proceeding to B6 (Phase 18 carrier) are the candidate next moves.

---

**2026-04-28 — v2 B1 + B2 + B3 LANDED on worktree (`8e2aa078`, `f513f338`, `f97e4ac0`); HARD EXIT B3 at 10/13.**

- **B1 (`8e2aa078`):** TallestUnbreakableBlockSize field on LayoutResult + PropagateTallestUnbreakableBlockSize method on BoxFragmentBuilder + Build()-site population. No callers; behavior unchanged. 13/13 drivers PASS.
- **B2 (`f513f338`):** wired carrier propagation at three sites:
  - `fragmentation_utils.go`: ShouldAvoidBreakInside + CalculateUnbreakableBlockSize helpers; BreakBeforeChildIfNeeded propagates the child's contribution during initial column-balancing pass when child has break-inside:avoid (mirrors Blink fragmentation_utils.cc:1105-1113).
  - `block_layout.go`: BLA child loop propagates child's accumulated tallest unbreakable up to the current builder during initial column-balancing pass (mirrors box_fragment_builder.cc:566-569).
  - `multicol_layout.go:resolveColumnAutoBlockSize`: max'd across measure-pass iterations; floored against the resolved column block-size at the final clamp (mirrors cla.cc:1879-1948).
  - **Skipped:** SetupFragmentation border/padding contribution (fragmentation_utils.cc:510-514) and outer-multicol forwarding. Brief said "add if B3 exposes a regression."
  - **Skipped:** monolithic detection in ShouldAvoidBreakInside. louis14 lacks IsMonolithic flag on PhysicalFragment; ShouldAvoidBreakInside currently only checks style break-inside. v2 brief flagged this as a gap.
  - 13/13 drivers PASS at 0 diff.
- **B3 (`f97e4ac0`):** mechanical ClipBlockAxisOnly removal — setter, paint branch, struct fields (Box.ClipBlockAxisOnly, PhysicalFragment.ClipBlockAxisOnly), engine.go propagation. Mirrors Blink's no-per-column-paint-clip.

**B3 driver result: 10/13.** Below the v2 brief's 11/13 hard-exit threshold. Three residuals all involve monolithic-shape content the carrier doesn't yet detect:

| Test | Status | Diff | Pattern |
|---|---|---|---|
| `nested-floated-multicol-with-monolithic-child` | FAIL | 0.2% | float with `contain:size` 100h box containing 90h green; contain:size is per-spec monolithic but louis14 doesn't tag it |
| `spanner-fragmentation-004` | FAIL | 2.1% | 50h spanner declared height with 200h of children; children's 150h overflow exposed without clip; spanners are implicitly monolithic for column-balance but louis14 doesn't propagate this |
| `spanner-fragmentation-006` | FAIL | 0.2% | similar to -004; closer to passing but residual remains |

`-004` actually regressed vs Spike A's 1.0% (now 2.1%). B2's carrier wiring may be misfiring on this test specifically; needs trace if option (1) below is taken.

**Why the hard exit:** v2 brief explicitly anticipated this signal — `ShouldAvoidBreakInside` currently checks only the style-level `break-inside:avoid` property, but Blink's version also checks `result.GetPhysicalFragment().IsMonolithic()`. The 3 residuals all need the monolithic clause. v2 brief: "If B3 exposes a test where monolithic detection is required, extend PhysicalFragment + ShouldAvoidBreakInside then."

**Next-step options (operator decision required):**

1. **Add monolithic detection (B2.5).** Extend PhysicalFragment with `IsMonolithic bool` flag (or use existing IsInsideColumnSpanner-like heuristics + replaced-element detection). Update `ShouldAvoidBreakInside` to also return true for monolithic fragments. Spanners should propagate their content height as TallestUnbreakable when they're "implicit monolithic" (declared height < content height). Estimated 30-50 lines.

2. **Add SetupFragmentation border/padding contribution.** v2 brief skipped this site — unlikely to close these tests but quick to verify (~10 lines).

3. **Accept 10/13 as floor; proceed to B5.** Document the 3 residuals as deferred; B5 walker WRITE flat under 10/13 baseline; potentially close residuals at B7 (drop IsInsideColumnSpanner gate — might let spanner children self-fragment via 16.d.1's clamp).

4. **Revert B3.** Keeps clip in tree as the workaround; pause the bundled phase. Means the walker port (B5) can't proceed cleanly because clip-only-mid-spanner code path still exists.

Worktree state: `f97e4ac0` (B3). 4-category invariants intact (1499/6720, 16 known-fails). DO NOT proceed past hard exit B3 without operator decision.

---

**2026-04-28 — v2 Step 0 diagnostic + B0 cache fix (LANDED on worktree `fdb9343a`).**

- **Diagnostic question:** Why does walker WRITE-flat × no-clip break column-height-026/027 + multicol-nested-030/031 (Spike B's 4 walker-flat-specific regressions)?
- **Method:** Instrumented MulticolLayoutAlgorithm.Layout to log incoming/outgoing break-token shape. Ran column-height-026 under Cmt 2 baseline (passes), Spike A (passes), Spike B (fails 1.0%). Compared traces.
- **Smoking gun:** Walker dispatches by `child.Node == multicolContainer` pointer equality. `MulticolLayoutAlgorithm.Layout()` allocates a fresh `contentNode := &LayoutInputNode{...}` on every call. Outer column 1's inner-multicol Layout emits a break token whose `outBT[0].Node` points to the col-1 contentNode. Outer column 2's inner-multicol Layout builds the walker with a fresh col-2 contentNode (different pointer). The walker's identity check fails, falls through to spanner branch, mis-dispatches every column-content resume as a spanner.
- **Fix:** Cache contentNode on `mla.node` (analogous to `groupedChildrenCache`). 1 struct field + 5 lines of cache logic.
- **Result on the 13 drivers (verified all matrix corners):**

  | Config | Cache OFF | Cache ON |
  |---|---|---|
  | Cmt 2 + clip ON (baseline) | 11/13 | **13/13** |
  | Cmt 2 + clip OFF (Spike A) | 9/13 | 10/13 |
  | Cmt 3 + clip ON | 11/13 | **13/13** |
  | Cmt 3 + clip OFF (Spike B) | 5/13 | 10/13 |

  Cache fix alone closes -001 (0.8% → 0) and -006 (1.4% → 0) at clip-on baseline. Walker dispatch is now correct for both positional and flat WRITE encodings. The 3 residuals at clip-off (-004, -006, nested-floated) correlate with clip handling, not WRITE model — they're the genuine 16.d.2/3 carrier work.
- **Why v1 didn't catch this earlier:** Cmt 2's positional WRITE puts the col-rows resume at slot[2] with slot[0]=nil. Walker iter 1 sees child[0]=nil → empty entry → column-content with nextColToken=nil → `layoutLine` runs **fresh**. The 400h block re-lays-out from zero in outer col 2, producing visually identical content to col 1 — coincidentally matching the reference. Cmt 2 was always semantically broken (post-Cmt-1); just visually masked by the fresh-layout coincidence. Cmt 3's flat WRITE puts the col-rows resume at slot[0], surfacing the pointer mismatch on the first walker iteration. The diagnostic shows v1's "Cmt 3 fixes Cmt 2's regressions" premise was wrong — the regressions trace to a pre-Cmt-1 latent bug that Cmt 2 introduced, not to walker WRITE shape.
- **column-height-008 follow-up:** Hangs at clean baseline `a8ea3adb` (10m timeout, default Go test) regardless of cache fix. Pre-existing issue; not addressed here. Document for separate triage.
- **Status:** B0 landed on worktree `fdb9343a`. Worktree at 13/13 drivers, multicol gate ≥ Cmt 2 baseline (verification deferred until non-hanging tests can be measured cleanly), 4-category invariants intact.
- **Implication for v2 sequencing:** B0 is complete. The original v2 B1+B2 (TallestUnbreakable carrier) targets the 3 clip-off residuals (-004, -006, nested-floated). B3 (clip removal) is now genuinely mechanical given B0+B1+B2. v2 sequence holds.

---

**2026-04-28 — Path A spike (operator-requested empirical test).**

- **Question:** Is 16.c.2 retry #3 (`ClipBlockAxisOnly` removal) actually mechanical post-16.d.1, as the brief claims?
- **Method:** In worktree at `a8ea3adb` (Commit 2 baseline), comment out the `ClipBlockAxisOnly` setter at `multicol_layout.go:1273`. Build, run 13 drivers. Then apply Commit 3 stash on top, re-run.
- **Result:**
  - **Spike A (Commit 2 + clip OFF): 9/13.** 16.d.1 closed the gap for `column-height-001/010/017/026/027`, `multicol-nested-030/031`, `multicol-rule-nested-balancing-004`, `nested-past-fragmentation-line` (9 tests clip-independent). NEW failures: `spanner-fragmentation-004` (1.0%), `nested-floated-multicol-with-monolithic-child` (0.2%); `-006` slightly worsens 1.4%→1.5%. `-001` unchanged at 0.8%.
  - **Spike B (Commit 3 + clip OFF): 5/13.** 4 ADDITIONAL regressions vs Spike A: `column-height-026/027` and `multicol-nested-030/031`. These tests pass under both Commit 2 baseline AND Spike A but break under the walker WRITE-flat encoding even without the clip — independent of clip handling.
- **Diagnosis:**
  1. `progress.md` line 19 "ClipBlockAxisOnly is no longer load-bearing for ANY of the 13 driver tests" is half-true. True for 9 of 13; false for 4 (`-001`, `-004`, `-006`, `nested-floated`). The original test for that claim was done with the clip still in tree.
  2. **Path A is NOT mechanical.** Removing the clip alone regresses 2 tests vs Commit 2 baseline.
  3. **The walker WRITE-flat has an independent regression vector** unrelated to the clip. Spike B identified the cluster (`column-height-026/027` + `multicol-nested-030/031`) but not the mechanism. These tests rely on 16.d.1's per-fragment clamp, which apparently inspects break-token shape in a way the flat encoding diverges from positional. Mechanism trace required before any retry.
- **Status:** Worktree restored to `a8ea3adb`. Spike documented; bundled brief needs redesign (see `CONTINUE-18.md` § "Revised recommendation"). The 6-commit decomposition in findings.md § "Phase 16.e + 18 BUNDLED BRIEF" — particularly Commit 3's "regressions restore" claim and Commit 6's gate target — is empirically refuted.
