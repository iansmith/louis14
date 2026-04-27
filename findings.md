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

### Phase 17 brief — Forced-break balance (target T2, ~5 tests)

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

## Error Log

*(Add entries as failures are diagnosed — format: date, symptom, root cause, fix or status)*
