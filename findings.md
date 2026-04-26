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

### Phase 16 brief — Spanner BFC filtering (target T1, ~6 tests)

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
