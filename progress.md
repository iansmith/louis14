# Progress Log — css-multicol (active)

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not duplicate here.

## Archived wm work
All writing-modes progress archived to `docs/progress-wm.md`. Do not copy wm content here.

---

## Current gate (2026-04-27 — post-Phase-16.d.1 commit `c40b4b56`)

| Category | Count | Notes |
|---|---|---|
| CSS2 | **99/99** | invariant |
| css-flexbox | **626/629** | 3 pre-existing residuals |
| css-position | **92/105** | 13 pre-existing residuals; no active work |
| css-writing-modes | **781/781** | complete |
| css-multicol | **192/455** | **+25 from Phase 16.d.1** (per-fragment block-size clamp + DidBreakSelf carrier + IsInsideColumnSpanner gate) — 167 → 192. Per-fragment clamp lets monolithic content fragment naturally at column boundaries; spanner descendants are gated out so the existing pendingContentOverflow mechanism keeps working. The per-column ClipBlockAxisOnly is no longer load-bearing for any of the 13 driver tests but stays in tree until Phase 16.c.2 retry. |
| spanner-fragmentation | **11/13** | **+4 from Phase 16.d.1** (7 → 11): spanner-fragmentation-001/004/006 now pass. -006 specifically required the IsInsideColumnSpanner gate (Blink: spanners are monolithic for placement). |

## Active phase

**Phase 16.e + 18 BUNDLED — MulticolPartWalker port + MulticolBreakTokenData carrier.** WORKTREE WORK. Continuation: `CONTINUE-18.md`. Multi-commit refactor (6 commits) on `phase-16e-18-walker-carrier` branch. Bundled brief written 2026-04-28: `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF (prep complete 2026-04-28)" supersedes the earlier sketch. Authoritative Blink references captured (`multicol_break_token_data.h` verbatim; `MulticolPartWalker` is now inline at top of `column_layout_algorithm.cc:41-223`; carrier write/read sites at cla.cc:822-833 / 2122-2139). 12 entangled louis14 sites mapped with current line numbers. Gate target after bundle: multicol 196 → 211+/455 (+15 from Phase 18 nested cluster), spanner-frag 11 → 12/13. Phase 16.c.2 retry #3 (mechanical clip removal) queued AFTER this lands.

**Commits 1+2 DONE on worktree (2026-04-28):**

- **Commit 1 (`43ec8c66`)** — schema + walker scaffold. `pkg/layout/multicol_part_walker.go` added (`MulticolPartWalker`, `MulticolPartWalkerEntry`, `MulticolBreakTokenBuilder`); `MulticolData *MulticolBreakTokenData` field on `BlockBreakToken`; READ at `multicol_layout.go:294-297` plumbed (still nil → behavior unchanged). 13/13 drivers PASS.
- **Commit 2 (`a8ea3adb`)** — READ site switched to walker dispatch. The 3-slot positional parser + pure-nested-resume promotion at `multicol_layout.go:415-432` is replaced with `walker := NewMulticolPartWalker(...)`. Main loop at `multicol_layout.go:512-849` rewritten as Blink-style two-branch dispatch (cla.cc:605-714). **11/13 drivers PASS** — `spanner-fragmentation-001` (0.8% diff) and `-006` (1.4% diff) regress per brief expectation.

**Commit 3 ATTEMPTED + HIT HARD-EXIT #1 (2026-04-28):**

WRITE-site flattening (all 6 brief sites + a self-derived `flushWalker` mirroring Blink cla.cc:733-738 cleanup loop) executed faithfully. Build clean. Driver result: **same 11/13** — `-001` unchanged at 0.8% diff, `-006` improved 1.4%→1.0% but still failing. Both fail invariantly under WRITE-side flattening alone. Per hard-exit #1 ("walker WRITE-site mapping wrong; re-read Blink, do NOT pile predicates"), the attempt was reverted; worktree is back at `a8ea3adb` (Commit 2). The Commit 3 diff is preserved in `git stash@{0}` on the worktree for archaeology.

**Root cause of the brief's incompleteness** (full analysis in `CONTINUE-18.md`): louis14's `ClipBlockAxisOnly` workaround creates a "clip-only mid-spanner" code path that the walker model cannot encode in pure flat form. When a spanner clips at the outer boundary, the previous outer column's loop exits BEFORE enumerating post-spanner content. The OLD 3-slot encoding's slot[0] = `beforeSpannerToken` drove BLA to re-discover post-spanner content via `layoutLine`. The walker elides this driver by design.

**Path A spike (2026-04-28, operator-requested) refuted v1's "mechanical 16.c.2 retry #3" framing:**

| Configuration | PASS | Notes |
|---|---|---|
| Cmt 2 baseline (clip ON, walker READ only) | **11/13** | -001 0.8%, -006 1.4% fail |
| Spike A (Cmt 2 + clip OFF) | **9/13** | + -004 1.0%, nested-floated 0.2% NEW fail |
| Spike B (Cmt 3 + clip OFF) | **5/13** | + column-height-026/027 + multicol-nested-030/031 NEW fail (walker-flat × no-clip regressions) |

**v2 brief written 2026-04-28 (Option A — clip-removal-first).** Authoritative: `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)". v1 brief preserved but marked SUPERSEDED. Sequence: Step 0 diagnostic → B0 cache fix → B1+B2 carrier port → B3 clip removal → B4 re-verify → B5 walker WRITE flat → B6 Phase 18 carrier WRITE → B7 drop 16.d.1 gate → B8 sweep + merge.

**v2 B0 → B5 + Path X LANDED 2026-04-28; PAUSED at 10/13 drivers, 199/455 multicol gate.**

Path X (`2d6822b3`): nested-balancing TallestUnbreakable propagation — mirrors Blink cla.cc:1706-1712. When MLA itself is in initial-balancing-pass (nested column balancing), forwards the accumulated `tallestUnbreakable` to its outer container via `result.TallestUnbreakableBlockSize`. **+2 multicol gate (197→199):** `multicol-span-all-list-item-001/002`. 13 drivers unchanged.

Initial Path X attempt also measured the spanner during the measure pass (would have addressed -004/-006). REVERTED — caused regressions on `spanner-fragmentation-000/002/010` (extra layout call had side effects on spanner resume state). The spanner-content-overflow residuals need a different mechanism at the spanner-placement layer, not the measure-pass layer.

Path Y as originally framed (widen balanceColumns) was the wrong diagnosis. Visual inspection shows nested-floated's 0.2% is a float `margin-top:10` not-honored bug inside multicol — separate float-margin issue, not balanceColumns scope.



Worktree commits in order:
- `fdb9343a` B0 contentNode pointer cache (11→13/13).
- `8e2aa078` B1 TallestUnbreakable scaffold (13/13).
- `f513f338` B2 wire carrier (13/13).
- `f97e4ac0` B3 mechanical clip removal (10/13 — hard exit).
- `da5730b8` B2.5 monolithic detection (10/13 — infrastructure correct, upstream gaps).
- `3b3b4208` B2.6 SetupFragmentation border/padding (10/13 — none of residuals have borders).
- `33afa6fa` B5 walker WRITE flat (10/13 — `-004` improved 2.1%→1.0%; walker port mechanically beneficial).

**Three residuals at PAUSE:** `nested-floated-multicol-with-monolithic-child` (0.2%), `spanner-fragmentation-004` (1.0%), `spanner-fragmentation-006` (0.3%). Diagnosed as upstream-architectural gaps:
- `-004` / `-006`: louis14's measure pass exits at `spannerPath` without laying out the spanner; spanner's `IsMonolithic` doesn't propagate via the measure-pass child loop. Blink's measure pass lays out spanners.
- `nested-floated`: float's `column-fill:auto` + non-fragmented context bypasses `IsInitialColumnBalancingPass` entirely.

Two candidate paths to close residuals (Path X: extend measure pass to layout spanners, ~30-50 lines; Path Y: widen `balanceColumns` for float multicols, ~10-20 lines). Both upstream-architectural, beyond v2 brief scope. Alternative: accept 10/13 + proceed to B6 (Phase 18 carrier WRITE site, targets +15 multicol gate).

**Operator decision required.** Full diagnosis + paths in `CONTINUE-19.md` § "PAUSE for review (current state)" and findings.md error log entry "v2 B2.5 + B2.6 + B5 LANDED ...".

DO NOT proceed past B5 to B6/B7 without re-engaging.

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

## Phase 16 — split into 16.a (DONE), 16.b (DONE), 16.c (QUEUED)

### Phase 16.a — DONE (commit `d42e3cf2`, +2)

Blink-parity `IsValidColumnSpannerInTree` predicate chain. New helpers `isSelfValidColumnSpanner` (candidate-side display/float/oof check) + `shouldPreventColumnSpannerDescendants` (ancestor-side BFC/table-internal/transform check) in `block_layout.go:2185-2248`. `ConstraintSpace.ColumnSpannerDescendantsBlocked` flag propagates to child spaces (mirrors Blink's containing-block walk). Spanner detection gate at `block_layout.go:379-384`. Tests `multicol-span-all-002, -004` PASS. Gate: 190 → 192/455.

### Phase 16.b — DONE (commit `a375cb45`, +3 targets / −25 net)

BSFF row-advance + spanner polish. `block_layout.go`: populated `BlockSizeForFragmentation` at 4 BLA return sites; added nested ColumnSpannerPath propagation so a grandchild spanner detected by an inner BLA bubbles up. `multicol_layout.go`: `spannerLeafNode` traversal, spanner WDM (RTL parity), spanner margin-block-start/end, `maxColHeight` uses BSFF, row-advance cap conditioned on `!hasSpannerDetector`, narrowed `ClipBlockAxisOnly`, cross-gap loop only adds gaps between adjacent populated columns. Tests `multicol-span-all-006, -007, -008` PASS at 0 diff. `column-height-001` continues PASS.

**Trade-off:** kept `ClipBlockAxisOnly` (just narrowed). Phase 16.c's Blink research showed Blink has no per-column paint clip at all (`box_fragment_painter.cc:1080-1114`); any predicate diverges from Blink. The narrowed predicate hit ~25 regressions across `column-height-003/004`, `multicol-list-item-003/004/005`, `multicol-fill-balance-005/018/024`, `spanner-fragmentation-{000,002,008,010,012}`, `multicol-nested-{015,026,028}`, `change-fragmentainer-size-{001,002,003}`. Multicol gate 192 → 167; spanner-fragmentation 12/13 → 7/13.

### Phase 16.c.1 — DONE (commit `2aa01920`, gate-neutral)

Ported Blink's column-regrowth pattern (`column_layout_algorithm.cc:1099-1124`) into `layoutLine`. New `minimumColumnBlockSize` parameter threads through `constrainColumnBlockSize` as a floor (applied after upper clamps so it overrides outer-fragmentainer space when content needs the room — Blink-parity via `available_outer_space = std::max(minimum_column_block_size, FragmentainerSpaceLeftForChildren() - line_offset)`). After the inner column loop, when nested in a column fragmentainer and any column shows true monolithic overflow (`BreakToken == nil && BSFF > fragH`), tail-recurse with the floor raised. Verified `multicol-nested-010` passes; gate identical to baseline.

The `BreakToken == nil` gate excludes content that fragmented at column boundary (e.g. `multicol-nested-030/031` with `break-inside:avoid` violated): there's no monolithic overflow, BSFF just measures trailing content, and forcing regrowth would collapse 4×50h column-fragments into one 400h oversized column.

### Phase 16.c.2 — ATTEMPTED, ROLLED BACK (2026-04-27)

Removed `ClipBlockAxisOnly` setter (`multicol_layout.go`) + paint-side branch (`paint_layer.go`). Result: net **−8 multicol** (167 → 159), spanner-frag unchanged. Per the brief's "STOP, ROLLBACK, do NOT chase the regression with a new predicate" guidance, reverted both files.

**Why it failed:** the Phase 16.b regression cluster (column-height-003/004, multicol-list-item-003/004/005, multicol-fill-balance-005/018/024, spanner-fragmentation-{000,002,008,010,012}, multicol-nested-{015,026,028}, change-fragmentainer-size-{001,002,003}) was unaffected by clip removal — its root cause is deeper than `ClipBlockAxisOnly`. Meanwhile clip removal newly broke 13 tests (column-height-001/010/017/026/027 — column-wrap:wrap monolithic content; multicol-nested-030/031 — break-inside:avoid; spanner-fragmentation-001/004/006; nested-floated-multicol-with-monolithic-child; nested-past-fragmentation-line; multicol-rule-nested-balancing-004) and recovered only 5 (increase-prev-sibling-height, inline-block-and-column-span-all, multicol-fill-balance-032, multicol-nested-029, multicol-zero-height-002).

**Blink-divergence signal:** in Blink, the newly-broken cluster passes without a per-column clip because Blink's layout properly fragments monolithic content at column boundaries. Louis14's `column-wrap:wrap`/`break-inside:avoid` paths place the full block in a single column and rely on the clip to hide overflow. Until that fragmentation gap is closed, the clip stays as a workaround. See `findings.md` § "Phase 16.c.2 attempt — what we learned" for the full diff and pointer to Phase 16.d brief.

### Phase 16.d research (DONE 2026-04-27, docs-only)

Research-only commit reads five Blink files (`box_fragment_painter.cc`, `block_break_token.h`, `box_fragment_builder.cc/h`, `fragmentation_utils.cc`, `column_layout_algorithm.cc`) to resolve Hypothesis A (painter clip) vs B (multi-fragment slicing). **B is correct.** The brief's proposed mechanism (`MonolithicOverflow` on `BlockBreakToken`) is wrong: that carrier is print-only in Blink (gated by `IsPaginated()`), not the multicol mechanism. The actual mechanism is **regular CSS block fragmentation via `DidBreakSelf` + `BlockBreakToken.ConsumedBlockSize`**, plus `TallestUnbreakableBlockSize` for `break-inside:avoid` content. Revised three-sub-fix plan in `findings.md` § "Phase 16.d Blink research". Gate unchanged.

### Phase 16.c.2 retry attempt #2 — REVERTED (2026-04-27, after 16.d.1)

Second 16.c.2 attempt. With Phase 16.d.1's per-fragment clamping in place, removing `ClipBlockAxisOnly` (setter + paint branch) gives multicol 192 → 195 (+3) with the IsInsideColumnSpanner gate kept, or 192 → 196 (+4) with that gate also removed. Both variants regress 2-3 driver tests by 0.2-1.0% (`spanner-fragmentation-006`, `nested-floated-multicol-with-monolithic-child`, optionally `spanner-fragmentation-004`). Per CLAUDE.md "ALL tests must pass", reverted. Path forward: Phase 16.e (Blink-parity spanner-content fragmentation) before retrying 16.c.2 a third time. See `findings.md` § "Phase 16.c.2 retry attempt #2 — REVERTED" for full diagnosis and the two routes to Phase 16.e.

### Phase 16.d.1 — DONE (commits `a6446061` + `c40b4b56`, +25 multicol / +4 spanner-fragmentation)

Per-fragment block-size clamp + DidBreakSelf carrier in BlockLayoutAlgorithm. Mirrors Blink's FinishFragmentation `else if (space_left != kIndefiniteSize && desired_block_size > space_left && space.HasBlockFragmentation())` branch (fragmentation_utils.cc:542-657). When a true leaf block's desired border-box exceeds the fragmentainer's remaining space inside an active block-fragmentation context, the fragment is sized to space_left, `LayoutResult.DidBreakSelf` is set, and a continuation `BlockBreakToken` with updated `ConsumedBlockSize` is emitted — even if no inner child broke. The next fragmentainer resumes the block via the new break-token.

Gated to true leaf blocks: `!IsBlockSizeOverride && !IsInsideColumnSpanner && hasExplicitBlock && HasBlockFragmentation && FragmentainerBlockSize > 0 && !IsInitialColumnBalancingPass && len(children) == 0 && !column-span:all`. Non-leaves keep parent-driven fragmentation (interleaving caused break-token misalignment + infinite row-wrap loops on column-wrap:wrap + spanner siblings — e.g., `column-height-006`). Spanners themselves and their descendants keep their MulticolLayoutAlgorithm resume mechanism (`pendingContentOverflow` + `spannerContentBreakToken`); the `IsInsideColumnSpanner` flag is set in `layoutSpanner` / `layoutSpannerInFrag` and propagated through child constraint spaces in `block_layout.go`. Driver: spanner-fragmentation-006 — without this gate the spanner's 360h leaf self-fragmented and the spanner's content incorrectly fragmented across all 4 outer columns instead of overflowing once monolithically (Blink reference: `column_layout_algorithm.cc::LayoutSpanner` — spanners are monolithic for placement).

Driver-test recoveries (13/13 PASS at 0 diff without relying on the clip): column-height-001/010/017/026/027, multicol-nested-030/031, spanner-fragmentation-001/004/006, multicol-rule-nested-balancing-004, nested-floated-multicol-with-monolithic-child, nested-past-fragmentation-line. Other gates unchanged: CSS2 99/99, flex 626/629, position 92/105, wm 781/781.

### Phase 17 — DONE (multicol 192 → 196, +4)

Blink-parity ContentRuns measure-pass for forced-break column balancing. Rewrote `resolveColumnAutoBlockSize` in `multicol_layout.go` with a do-while loop that records one `contentRun` per forced-break segment, then calls `distributeImplicitBreaks` to compute the balanced column height. Four supporting changes: (1) `fragmentation_utils.go`: moved `IsInitialColumnBalancingPass` early-return to AFTER forced-break check, so forced breaks fire during the measure pass; (2) `block_layout.go` Change 2: enabled `HasForcedBreak` propagation during measure pass; (3) Change 3: CSS Fragmentation §3.4.2 fragment extension — extends non-last fragments to fill the column, gated on `IsInsideBalancedColumns && hasAutoColumnHeight()` to prevent extension in explicit `column-height` contexts; (4) Change 4: propagates `HasForcedBreak` through the overflow path. Critical fix: `colBSize = max(fragBSize, BlockSizeForFragmentation, IntrinsicBlockSize)` in the measure loop ensures IsBlockSizeOverride-forced fragments report the correct content height. Tests gained: `multicol-fill-balance-040/041`, `multicol-nested-column-rule-003`, `spanner-in-child-after-parallel-flow-004`. Gate: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol **196/455** · spanner-frag 11/13.

## Phase 16.c-19 plan (research complete, briefs in findings.md)

Detailed Blink-parity analysis lives in `findings.md`. Each brief includes Blink source citations (file:line), our current code state, implementation plan, test driver order, and tractability rating.

| Phase | Target | Tests | Tract. | Brief location |
|---|---|---|---|---|
| 16.c | Column regrowth + remove ClipBlockAxisOnly | ~25 (recovery) | Med-Hi | `findings.md` § Phase 16.c brief |
| 17 | Forced-break balance (Blink ContentRuns/DistributeImplicitBreaks) | ~5 | Med | `findings.md` § Phase 17 brief |
| 18 | Nested multicol MulticolBreakTokenData row-carry | ~15 | Hard | `findings.md` § Phase 18 brief |
| 19 | span-all-children-height 002-013 (7 sub-clusters) | 12 | Mixed | `findings.md` § Phase 19 brief |

Total addressable after 16.c (recovery): ~32 tests across Phases 17-19, on top of the ~25 recovered by 16.c itself.

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
