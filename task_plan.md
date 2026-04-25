# Task Plan: css-position (Phases 0–11) → css-multicol (Phase 12) → LayoutUnit precision (Phase 13)

## Current focus (2026-04-25)
**Phases 13a + 13b + 13c + 13d + 13e (CLOSED) + 13e′ (CLOSED) + 13f (CLOSED) + 13g.1 (helper additive) landed 2026-04-25.** Phase 13e closed at 13e.6 (entry-side migration via `ResolvePercent`); **Phase 13e′ closed at 13e′.3** (exit-side return-type promotion: 13e′.1 `ResolveInlineSize`+`ResolveBlockSize`, 13e′.2 `ResolveMinInlineSize`+`ResolveMaxInlineSize`, 13e′.3 `ResolveMinBlockSize`+`ResolveMaxBlockSize`); **Phase 13f closed at 13f.3** (13f.1 snap helpers + 13f.2 MeasureText* return-type promotion + 13f.3 ShapeAdvances*/Mixed return-type promotion to ShapeCumulative; 13f.4 closed as "skip"). **Phase 13g.1 (this commit) adds the additive `SnapSizeToPixel` + `SnapSizeToPixelAllowingZero` helpers to `pkg/geometry/layoutunit`** mirroring `third_party/blink/renderer/platform/geometry/layout_unit.h` verbatim, including the >4-raw thin-line clause that closes "0.5 px border vanishes" bugs; no callers yet, gate-sweep held by construction (matches 13a / 13e.1 / 13f.1 additive-helper precedent). **Rounding-mode discovery (during 13e′.1):** first attempt used `FromFloat64Trunc` to mirror Blink's `LayoutUnit(float)` ctor; regressed css-multicol 179 → 172 because louis14's float64 `Length.value` storage (vs Blink's float32) makes `8.2 * 50.0` land at IEEE 754 `409.99999...`, which Trunc snapped DOWN to 409.984 and shifted column-rule positions by 1 px. Fix: switch to `FromFloat64Round` — round-half-away-from-zero absorbs the IEEE 754 noise while preserving bit-exact round-trip for 1/64-clean inputs. The Round-mode pattern carried cleanly through 13e′.2 + 13e′.3 with no further rollbacks. All six gate invariants held at the 13e′.3 checkpoint (CSS2 99/99, flex 626/629, position **91/104**, wm 781/781, multicol 179/455, spanner-frag 12/13). **Queued: 13g.2 (migrate inner border-edge corners — the clear-001 hypothesis test, see `findings.md` "Phase 13g research" for the render.go:2672-2675 ad-hoc-Round site analysis), 13g.3 (route `pixelSnap` helper through `SnapSizeToPixel`), 13g.4+ (background/clip clusters), 13h (verification + cleanup, retro doc).** See `findings.md` "Phase 13e′ research" for the Blink-parity reference and the rounding-mode-discovery rollback notes, "Phase 13g research" for the SnapSizeToPixel port + ad-hoc Round inventory, and `progress.md` "Phase 13e′.{1,2,3}" + "Phase 13g.1" for landing details.

**Phase 12 (css-multicol)** remains the active layout-feature track at 179/455. Most active follow-up targets done: F1 (wm 781/781), F5 (multicol +3 list-item tests), HTML tokenizer EOF-recovery (css-position 91/104). F2/F3/F4 remain PARTIAL with documented residuals. **Phase 13 (LayoutUnit precision discipline)** is in active execution: 13a + 13b + 13c + 13d + 13e (all six sub-steps 13e.1–13e.6) **+ 13e′ (CLOSED: 13e′.1 + 13e′.2 + 13e′.3) + 13f (CLOSED: 13f.1 + 13f.2 + 13f.3, 13f.4 skipped) + 13g.1 (additive helper)** all done; 13g.2/13g.3/13g.4+ + 13h queued.

css-position is at **91/104** (gate-swept 2026-04-25 at 13e.1/13e.2 HEAD; the earlier-claimed 92 was a tracking miscount). The 13 remaining failures are pre-existing residuals across G-SINGLETONS / G-SCROLL / G-SEMI-REPLACED / G-ABS-IN-TABLE classes — see "css-position Goal" below.

**Phase 12a is COMPLETE (commit `2a0d0a07`, 2026-04-22).** Fragmentation infrastructure landed: Blink-parity `LayoutLine` outer stretch loop, `BlockBreakToken` threading, shortage reporting, `ResolveColumnAutoBlockSize` for column-fill:balance, inline fragmentation at column boundaries, multicol dispatch enabled in `layoutElement`. Driver test `multicol-fill-balance-001.xht` PASS at 0 diff.

**Phase 12b is COMPLETE (commit `931f48c5`, 2026-04-23).** All 13 spanner-fragmentation-* tests PASS at 0 pixel diff. Gate: wm 781/781, CSS2 99/99, css-flexbox 626/629.

**Phase 12c Blink-parity infra LANDED (commits `cccbd05e` + `b0825367`, 2026-04-23).** Three of four canonical cla.cc sites closed: nested-initial-balancing override (guard fix), outward shortage propagation (`PropagateSpaceShortage` on `BoxFragmentBuilder`), resume-break emission for nested outer-boundary hit. `MulticolBreakTokenData` row-carry deferred to 12f. Driver `multicol-nested-010.html` 6000 → 3500 px (1.2% → 0.7%); css-multicol 108 → **130 PASS** (+22 across nested, span-all, fill-auto/balance, columns, width). Driver residual is paint/leaf-fragmentation, not a 12c scope miss — tracked as follow-up.

**Phase 12d COMPLETE (2026-04-24).** Forced-break + break-inside:avoid-column dispatch. Net +2 multicol PASS (121 → 123). See progress.md.

**Phase 12e PARTIAL (2026-04-24).** Driver `multicol-fill-auto-block-children-003` (max-height-imposes-on-columns) PASS at 0 diff. Cluster residuals (missing-text rendering, inline-overflow clip, etc.) tracked as follow-ups. Net +1 multicol PASS (123 → 124).

**Phase 12f PARTIAL (2026-04-24).** Driver `column-height-001.html` PASS at 0 diff. Blink-parity port of CSS Multi-column L2 §4.2 `column-height` + `column-wrap` — 6 of 5 cla.cc consumption sites landed (row-height clamp, LayoutLine block-size override, row-wrap loop, intrinsic top-off, break-token slot-layout fix, block_layout leaf cumulative-consumed fix). Net +6 multicol PASS (124 → 130). 24 cluster residuals (0.1–4.2% diffs) tracked as follow-ups: row-gap plumbing, MulticolBreakTokenData row-carry (12f.6 deferred), forced-break + wrap interactions, overflow-past-declared-columns for `column-wrap:nowrap`.

**Phase 12g PARTIAL (2026-04-24).** Three `balance-break-avoidance-*` drivers PASS at 0 diff. Blink-parity port of break-appeal propagation from the block_layout fragmentainer-split overflow path + MinSpaceShortage computation for the BreakBefore soft-break path. Full `EarlyBreak` + `RelayoutAndBreakEarlier` retry NOT ported — stretch-retry alone handles current drivers (Blink cla.cc:1053 ↔ 1210+ flow). Net +3 multicol PASS (130 → 133).

**Phase 12h KICKOFF SURVEY (2026-04-24).** Scope revised from the §7/§8/§9b name-parity port. Survey (see `findings.md` "Phase 12h kickoff survey (2026-04-24)") shows that `multicol-list-item-001/002` already PASS, most `multicol-rule-*` failures are Ahem font-loader or sub-pixel AA, and the named Blink abstractions close ~0 tests on their own. Revised attack order: (1) Ahem font loader **[DONE 2026-04-24, +2]**, (2) high-diff rule-paint fixes (`-large-001`, `-stacking-001`, `-nested-balancing-003`), (3) `multicol-list-item-003` trailing-text, (4) tiny-diff `-solid/ridge/groove/…-000` cluster sweep; `GapGeometry` / `PropagateBaselineFromChild` / `UnpositionedListMarker` parity ports deferred until a test demands them.

---

## ACTIVE FOLLOW-UP BATCH (2026-04-24 — post-12h step 4) — TEMPORARY

Five highest-value targets from the deferred list, picked after Phase 12h step 4 landed (css-multicol 154/458). This is a **scratch planning block**: as each target lands, move its completed summary into its real phase section below and strike the entry here. When all five resolve or get reclassified, delete this block.

**Status as of 2026-04-25:** F1 DONE, F5 DONE, F2/F3/F4 PARTIAL (residuals captured in their respective rows). Plus three follow-on landings outside the original five: HTML-tokenizer EOF-recovery (closed `position-change.html`, +1 css-position); **Phase 13 LayoutUnit precision discipline** with 13a/13b/13c/13d/13e all landed in 16 commits today (foundational types + composites + all `PhysicalFragment` coordinate fields + all `ConstraintSpace`/`BlockBreakToken`/`ExclusionSpace` precision fields are LayoutUnit-backed; all percentage-resolution call sites flow through the canonical `ResolvePercent` helper; 13e′ + 13f–13h queued); F1d cleanup pass landed via earlier mazzy registration work. Block stays open until F2/F3/F4 reach DONE or get reclassified into their own phases.

Companion scratch blocks live at the top of `findings.md` (research notes per target) and `progress.md` (landing/partial summaries per target).

| # | Target | Category | Ambition | Status | Driver |
|---|---|---|---|---|---|
| F1 | wm `bidi-embed-006` + `bidi-override-006` (0.3% / 1598 px each) | css-writing-modes @font-face layout-time provider registration + Blink-parity bidi-level shape segmentation | +2 wm → 781/781 | **DONE 2026-04-25.** Three layered fixes: (1) F1d wiring — `RegisterBuffer(family, variant, []byte)` added to the `mazarin/textshape.GlyphProvider` / `TextLayout` / `DrawContext` interfaces, implemented on both `DirectGlyphProvider` (filesystem-bypass via `registered map`) and `FontSvcGlyphProvider` (in-process, no fontsvc IPC); `mazzy/textshape/harfbuzz.go` `maxFonts` 32→256 to absorb the host visualtest's accumulated unique-font count; louis14 `pkg/text/measure.go` adds `sharedProvider` + `CurrentProvider()` so layout and renderer share one provider (`pkg/render/render.go newProvider()` returns it); `pkg/text/fontcache.go` rewrite drops temp-file caching and `sanitizeFamily`. (2) Bidi-level merge predicate — `pkg/layout/line_breaker.go canMergeShapingContext` now requires equal `BidiLevel` (not just parity), and new `isBidiBoundary` breaks the merge run at `unicode-bidi != normal` span tags. Mirrors Blink's per-item-one-level invariant + `kBidiControl` separator. (3) Cross-span kerning gate — `applyCrossSpanKerning` runs only when every text item in the merge run is at level 0; non-level-0 cases use per-item shape consistently between measure and paint, since paint-side shape sharing (`ShapeResult::CopyRange`, deferred F1c) is not yet implemented. **+2 primary** (`bidi-embed-006`, `bidi-override-006`); **+5 spillover** (`bidi-embed-005`, `bidi-override-005`, `bidi-isolate-006`, `bidi-override-012`, `bidi-plaintext-002`, `bidi-plaintext-006` — all silently wrong pre-fix, masked by byte-count fallback giving identical bogus widths to .test and .ref). **Cleanups deferred to follow-up:** `openFont(path, size)` → `openFont(family, variant, size)`, removal of `FontPathToFamilyVariant` and `DirectGlyphProvider.resolveFamily` path fallback. **F1c (paint-side shape sharing)** also deferred — would let cross-span kerning fire for non-level-0 items. See progress.md / findings.md §F1. | Done. |
| F2 | Phase 12c nested-multicol leaf paint-slicing | css-multicol paint+layout | +~7 multicol (`-nested-007..014`) | **PARTIAL 2026-04-24.** Block-axis-only-clip fix landed (`ClipBlockAxisOnly` field on `PhysicalFragment` + `Box`, threaded through `engine.go`). Multicol column fragmentainers now allow inline overflow while clipping block overflow. css-multicol +1; `multicol-nested-010` 4500→3500 px. **Second root cause open:** our inner-multicol fragments the leaf across both inner sub-cols; Blink places it only in sub-col 1's continuation. Deferred — shares code path with F4. See findings §F2. | `multicol-nested-010.html` |
| F3 | Phase 12f `column-height`/`column-wrap` cluster residuals | css-multicol layout | +up to 24 multicol (partial closure) | **PARTIAL 2026-04-24, 5 increments landed (F3a-F3e, cumulative +14 css-multicol: 154→168; 13/32 in the column-height/wrap cluster now PASS).** Landed: row-gap plumbing (F3a), `columns:N/H` + `column-height:0` + zero-frag last-resort + abspos-in-multicol OOF aggregation (F3b), spanner-first-row advance (F3c), Blink-parity spanner pre-snap + row-wrap first-iter guard (F3d), non-auto-column-height-triggers-multicol (F3e). **19 residuals remain, each a distinct spec edge case.** Four Blink-research agents landed findings §F3 addendums for remaining clusters: spanner-row-stride (ported F3d), multi-spanner sequencing (no gap between adjacent, pre-snap per spanner), `column-wrap:nowrap` overflow (needs **paint-layer change** to paint past declared width), auto-height trailing row (agent sim matched our 120-px output — needs **real-Blink build trace** to find Blink's actual suppression path). | Done: most-tractable residuals landed. Remaining biggest: `-013` (6500), `column-wrap-no-constraints-002` (6000), `-006` (5250), nowrap-cluster (`-005/-011/-030` at 5000 each). |
| F4 | Phase 12h.2 inline-in-balanced-multicol | css-multicol layout | +2 multicol (`-large-001`, `-stacking-001`) + capability that also closes F2 phase 2 | **PARTIAL 2026-04-24 (commit `617332ae`).** Two-line fix: both `block_layout.go` and `inline_layout.go` gated inline break-token resume on `InlineItemStartIndex > 0`; Blink's `InlineBreakToken.start_` carries BOTH item-index and text-offset. Changed to `(idx > 0 \|\| textOff > 0)`. **`multicol-rule-large-001` PASS at 0 diff** (was 13.1 %). `-stacking-001` 19840→32 px (sub-pixel near-pass). +11 spillover passes. **Regressions: 4 margin-family tests** (tied to pre-existing block-pushed-past-fragmentainer bug that was previously masked; needs break-before-child-when-overflowing fix in outer block_layout — deferred). **F2 phase 2 check:** `multicol-nested-010` unchanged — nested leaf-fragmentation genuinely separate. css-multicol **+8 net** (168→176; +22 cumulative from pre-F3 baseline 154). See findings/progress §F4. | Done: `multicol-rule-stacking-001.xht` researched (now 32 px near-pass). |
| F5 | Phase 12h.3 `multicol-list-item-003` trailing inline-after-spanner | css-multicol layout (post-spanner continuation row stretch) | +3 multicol (`-list-item-003/004/005`) | **DONE 2026-04-24.** Diagnosis: research had pointed at `InlineBreakToken` forwarding, but our forwarding was already correct (anon-block-with-text resumed via `spannerBreakToken.ChildBreakTokens[0]={Node: anon-block, IsBreakBefore: true}`). The actual bug was in the **post-spanner row's column-block-size estimate**: `resolveColumnAutoBlockSize` returned `ceil(line_height/numCols) = 6` for a single 16-px line, then the per-column inner loop placed the line monolithically (line_breaker `blockOffset > 0` guard let the first line through), but `block_layout.go` overflow path then emitted `outToken.HasSeenAllChildren=true` with `MinSpaceShortage=10`. Pre-fix the multicol acceptance fired (no `hasViolatingBreak`, `colBreakToken==nil` after col 1 consumed `HasSeenAllChildren`), `ClipBlockAxisOnly` clipped at 6 → text cut off. Fix: in `MulticolLayoutAlgorithm.layoutLine` per-column loop, when **`lineOffset > 0`** (continuation row, post-spanner) AND the column's result has `MinSpaceShortage > 0` AND its `BreakToken.HasSeenAllChildren==true` AND `len(ChildBreakTokens)==0`, set `hasViolatingBreak=true` so the stretch loop fires (`colBlockSize` grows from 6 to 16). The `lineOffset > 0` guard avoids triggering for first-row "all siblings stacked overflow" (e.g. `multicol-rule-nested-balancing-001/002` where outer-block + inner-article overflow col 0 by design — both test/ref currently render the same clipped shape and stretching there would diverge them). **+3 net** css-multicol (176→179: `-list-item-003`, `-004`, `-005`). All invariants unchanged. See findings/progress §F5. | Done. |

**Ordering rationale.** F1 first because a broken gate invariant compromises every subsequent landing's regression signal. F2 because it's a tight cluster behind a single paint-level concept (inner-column slicing of a leaf fragment) — highest tests/effort ratio. F3 because even partial closure of the 24-cluster is the largest remaining named bucket. F4/F5 are about unblocking downstream capability more than the immediate test-count win.

**Revised ordering 2026-04-24 (post-F3 +14).** F3 delivered its most-tractable gains in 5 increments (F3a-F3e). Remaining F3 residuals now require work outside pure layout logic: `-005`-class (nowrap column overflow) needs paint-layer changes to let overflow columns paint past the border-box; `-024`-class (auto-height trailing row) needs a live-Blink build trace because agent simulation can't distinguish which suppression path Blink actually takes. Both are documented with full Blink line refs in findings §F3 for when we pick them up. **Next target: F4** — (a) clean Blink research already captured, (b) mechanical fix (break-token forwarding) with clear algorithm, (c) shares code path with F2 phase 2 so F4 done well also closes `multicol-nested-010` cluster (~3-7 additional tests). Higher expected impact/effort than further F3 grind.

**F5 landed 2026-04-24, cumulative +25 css-multicol from pre-F3 baseline 154 → 179.** F5 closed via continuation-row terminal-shortage detection (above). Surprise: research's expected fix path (`InlineBreakToken` forwarding) was already correct in our code post-F4; the actual bug was a balance-estimate underrun that our existing per-column shortage propagation should have stretched, but the column's `HasSeenAllChildren=true` break token caused acceptance to fire before the stretch loop did. Treating this specific shape as a violating break (only in continuation rows, to avoid first-row regressions like `nested-balancing-001/002` that pass by both sides rendering the same clip shape) is the minimal Blink-parity fix.

**F1 landed 2026-04-25.** wm 779 → 781/781 (full pass). Three-layer fix detailed in the F1 row above. Surfaced 5 spillover wins on bidi tests that were silently wrong via byte-count fallback. Remaining open follow-ups from this work: F1d cleanup pass (mechanical refactor of `openFont` signature + `FontPathToFamilyVariant` removal), and F1c paint-side shape sharing (`ShapeResult::CopyRange`) which would let cross-span kerning fire for non-level-0 items.

**Next candidates (no current target picked):** F2 phase 2 (`multicol-nested-010` and cluster) is back on the table now that the inline-after-spanner path is settled — same `lineOffset > 0` heuristic used in F5 may need refinement when leaf-fragmentation in nested multicol is properly implemented (the F2 ClipBlockAxisOnly workaround would then go away and the F5 stretch trigger could simplify). Or pick remaining F3 residuals individually. Or F1d cleanup + F1c.

**Discipline per target** (CLAUDE.md recap):
1. Read the target driver + its reference HTML before writing code.
2. Study the Blink reference for the algorithm, not just an adjacent passing test.
3. Do not settle for small diffs — 0.1 % is a failure just like 28 %.
4. Run only the target + ≤2 adjacent tests during feature work; full category only at completion.
5. Gate sweep (all 5 invariants) before each commit.

**Invariants to hold on each landing:** CSS2 99/99, css-flexbox 626/629, **css-position 91/104** (gate-swept 2026-04-25 at 13e.1/13e.2 HEAD; the earlier-claimed 92 was a tracking miscount around the HTML tokenizer EOF-recovery fix landing), spanner-fragmentation 12/13, **css-writing-modes 781/781** (raised from 779/781 by F1 closing 2026-04-25), css-multicol ≥179/455.

---

## Phase 13: LayoutUnit precision discipline (13a + 13b + 13c + 13d LANDED, 2026-04-25)

**Goal.** Port Blink's `LayoutUnit` precision discipline to louis14. Today every geometry value in `pkg/layout/` is `float64`; ~580 `float64` references across 43 files. Blink uses `int32` fixed-point with 6 fractional bits (1/64 px = `Epsilon`), saturating arithmetic, and explicit rounding-mode entry/exit at every float boundary (text shaping, transforms, length resolution).

**Why now.** Three drivers:

1. **`clear-001.xht` is the labeled residual** (96 px / 0.0%, marked "deferred pending Blink LayoutUnit trace"). Diagnosis re-examined 2026-04-25: the diff is a 1-px y-offset at the blue/orange boundary (our blue=96 tall, ref expects 97 tall, both totals match at 192). `1in = 96 CSS px` is integer-clean; a faithful LayoutUnit port would still produce 96, not 97. So **clear-001 may NOT close from this work alone** — its real cause is likely sub-pixel placement at the float-bottom snap or a paint-time anti-alias detail. Phase 13 still lays the groundwork; the actual fix may need Phase 13's `SnapSizeToPixel` paint-time analog plus a separate float-clear bottom-edge investigation.
2. **Bit-exact reproducibility is foundational** (CLAUDE.md rule 1). Float arithmetic associativity failures cause sibling drift that hasn't surfaced as a category but will once we tighten other categories — pagination, scroll anchoring, paint invalidation hashing.
3. **Sub-pixel snapping at paint** (Blink's `SnapSizeToPixel`) is the right architectural shape for "0.5px border at 0.5px origin draws as 1px" cases that are currently ad-hoc in our painter.

**Approach.** Mirror Blink's two-layer split: `pkg/geometry/layoutunit` holds the scalar `LayoutUnit{raw int32}` (analogue of `platform/geometry/layout_unit.h`); `pkg/geometry` holds the composites — `LogicalOffset/Size/Rect`, `PhysicalOffset/Size/Rect`, `WritingMode/Direction/WritingDirectionMode`, `WritingModeConverter` (analogue of `core/layout/geometry/`). Both packages landed (13a + 13b). Migrate consumers in dependency order — fragments next (13c), constraint-space + length resolution after (13d/e). The text-shaping boundary stays in `float64` internally (HarfBuzz output) and crosses into `LayoutUnit` via `FromFloatCeil` / `FromFloatRound` at well-defined call sites (13f).

See `findings.md` "Phase 13: LayoutUnit research" for the detailed Blink-parity reference (`platform/geometry/layout_unit.h`, `core/layout/geometry/`, `ShapeResult::SnappedWidth`, `PhysicalRect::EnclosingRect`, `SnapSizeToPixel`, the 6-fractional-bit rationale, the saturating-arithmetic guards, and the migration pitfalls Blink hit during NG).

### Phase 13 sub-phases

| # | Phase | Files touched | Test target | Risk |
|---|---|---|---|---|
| 13a | **Foundational types** — new `pkg/geometry/layoutunit` package: `LayoutUnit{raw int32}`, `New(int)`, `FromFloat64Round/Ceil/Floor`, `Float64()`, `Round/Floor/Ceil int32`, `Add/Sub/Mul/Div` (saturating via `math.MaxInt32`/`MinInt32` clamp), `MulDiv` (int64 widening for percentages), `Fraction()`, `Abs()`, `IsIndefinite()` (sentinel `kIndefinite = -64` raw, matching Blink's `kIndefiniteSize == -1` px). **DONE 2026-04-25.** Package + 16 unit tests (constructors, rounding modes, saturating arithmetic, MulDiv int64-widening, Round/Floor/Ceil for both signs, Fraction, Abs incl. MinInt32 saturation, IsIndefinite). All tests pass; gate sweep all 6 invariants held by construction (no existing code touched). | New package only; no existing callers. | Pure new code; comprehensive unit tests for arithmetic correctness, saturation, rounding modes. | Low. |
| 13b | **Geometry types** — `LogicalOffset{Inline, Block LayoutUnit}`, `LogicalSize{Inline, Block}`, `LogicalRect`, `PhysicalOffset{Left, Top}`, `PhysicalSize{Width, Height}`, `PhysicalRect`. Each has `New*` constructors, `From*F64Round/Ceil/Floor` for entry from float, `*F64()` accessors for exit, plus the writing-mode permutation methods (Logical↔Physical conversion via `WritingDirectionMode`). **DONE 2026-04-25.** New `pkg/geometry` package on top of `pkg/geometry/layoutunit`: composite types in `logical.go` / `physical.go`, `WritingMode`/`Direction`/`WritingDirectionMode` enums in `writing_mode.go` (numeric values mirror `pkg/layout` so the eventual migration is mechanical), `WritingModeConverter` + size/offset/rect conversions in `converter.go` (Blink-parity port of `writing_mode_converter.cc` `SlowToPhysical/SlowToLogical` for all 5 writing modes × 2 directions). 11 unit tests (incl. 10-row WM × Dir matrix verifying hand-traced expected physical offsets, full round-trip property, sub-pixel precision survival, size swap, rect conversion). All gate invariants held by construction (no callers). | New package; no migration yet. | Unit tests for the writing-mode conversions (matches `writing_mode_converter.cc` `SlowToPhysical`). | Low. |
| 13c | **Fragment offsets/sizes** — `pkg/layout/PhysicalFragment.Size`, `RelativeOffset`, `Children[].Offset` migrate from `float64` pairs to `geometry.PhysicalSize` / `geometry.PhysicalOffset`. The `Box` tree (`fragmentToBox`) keeps `float64` at the layout↔render boundary; conversion is `frag.Size.WidthF64()`. **DONE 2026-04-25.** Landed in three checkpoint commits: 13c.1 RelativeOffset (`6e689d8e`), 13c.2 Children[].Offset (`4dc4ac0b`), 13c.3 Size (`912c03fa`). Every PhysicalFragment coordinate field is now LayoutUnit-backed. Two transitional bridge helpers in `pkg/layout/physical.go` (`geomSizeToOld`/`oldSizeToGeom`) carry the type swap at sites where the new fragment fields meet the still-float64 `pkg/layout` converter API; those shrink as 13d/e migrate the converter itself. | `pkg/layout/layout_result.go`, `fragment_builder.go`, `engine.go`, `block_layout.go`, `table_layout.go`, `multicol_layout.go`, `grid_layout.go`, `out_of_flow_layout.go`, `flex_layout.go`, `inline_layout.go`, `inline_containing_block.go`, `positioned_root.go`, `physical.go` (+ four test files). | All six gate invariants held at every checkpoint commit. No regressions. PhysicalFragment.Offset (mentioned in original plan) was a misread — fragment has no top-level Offset field; per-child offsets live in `Children[].Offset`. | Medium-high (delivered without behavior changes; sub-step staging absorbed the risk). |
| 13d | **ConstraintSpace + LayoutResult** — replace `float64` available-size / percentage-resolution-size / line-offset / clearance-offset fields with `LayoutUnit`. **DONE 2026-04-25.** Five checkpoint commits: 13d.1 `ConsumedBlockSize` (`7d64570a`), 13d.2 `ClearanceOffset` method (`c6211fb8`), 13d.3 `Bfc{Block,Inline}Offset/BfcContainerInlineSize` (`d1687adc`), 13d.4a `AvailableSize` (`3e7d598c`), 13d.4b `PercentageResolutionSize` (`7db1f2fd`). Setters keep their float64 signatures and convert internally; readers add `.Float64()` at access sites; new bridge helpers `oldLogicalToGeom`/`geomLogicalToOld` in `physical.go` mirror the 13c `oldSizeToGeom`/`geomSizeToOld` pattern. All six invariants held at every commit. | `pkg/layout/constraint_space.go`, `break_token.go`, `exclusion_space.go` + readers across block/flex/grid/inline/line_breaker/multicol/table/min_max/fragment_geometry/replaced/absolute_utils/positioned_root/fragment_builder/physical/inline_layout. | The `layout_result.go` residual `float64` fields (`IntrinsicBlockSize`, `Baseline`, `LastBaseline`, `MinSpaceShortage`) were not part of 13d's plan-text scope (they are baseline-relative, not constraint-space precision-edge values) and stay `float64` for now. | Medium (delivered cleanly via sub-step staging; bridge-helper pattern absorbed the blast radius). |
| 13e | **Length / percentage resolution** — single canonical helper for percentage-of-basis resolution. `pkg/css/length` keeps `Length.value float32` (matches Blink); the new chokepoint is `pkg/geometry/layoutunit.ResolvePercent(basis LayoutUnit, percent float64) LayoutUnit` with Blink-parity truncation (mirrors `length_functions.cc:MinimumValueForLengthInternal` kPercent which uses the implicit `LayoutUnit(float)` ctor — NOT `FromFloatRound`, NOT `MulDiv`; verified against `refs/heads/main` 2026-04-25). Eliminates the "two siblings of same percentage compute different LayoutUnits" class of bug. **13e.1 + 13e.2 + 13e.3 + 13e.4 + 13e.5 + 13e.6 DONE 2026-04-25 — Phase 13e CLOSED.** Six sub-steps: 13e.1 helper + tests (additive, no callers); **13e.2 inline_layout text-indent (1 site) — DONE**; **13e.3 flex row-gap + column-gap (2 sites) — DONE**; **13e.4 flex-basis (2 sites: inline-axis + block-axis-with-definite-main) — DONE**; **13e.5 fragment_geometry Min/Max{Inline,Block}Size (4 sites) — DONE**; **13e.6 fragment_geometry ResolveInlineSize/ResolveBlockSize (2 sites — highest-fan-out, called from ~every layout algorithm) — DONE**. Return-type promotion of `ResolveInlineSize`/etc. from `float64` to `LayoutUnit` is deferred to 13e′ (~50-site ripple). `calc(...%...)` paths via `css.EvalCalcWithPercent` are already centralized; no re-routing in 13e. | `pkg/geometry/layoutunit/layoutunit.go` (+test); `pkg/layout/{inline_layout,flex_layout,fragment_geometry}.go` for 13e.2–13e.6. | Closes percentage-of-percentage drift; gate-sweep all six invariants per sub-step. | Medium. |
| 13e′ | **Resolve\*Size return-type promotion — DONE 2026-04-25 (CLOSED).** Promoted the six size-resolvers in `pkg/layout/fragment_geometry.go` from `float64` (or `(float64, bool)`) to `layoutunit.LayoutUnit` (or `(layoutunit.LayoutUnit, bool)`), mirroring Blink's `length_utils.{h,cc}` where every `Resolve*Length`/`Resolve*Length{Min,Max}` returns `LayoutUnit` directly. **Landed sub-steps:** 13e′.1 `ResolveInlineSize`+`ResolveBlockSize` (`ae7d60ed`); 13e′.2 `ResolveMinInlineSize`+`ResolveMaxInlineSize` (`b5d1e6c1`); 13e′.3 `ResolveMinBlockSize`+`ResolveMaxBlockSize` (this commit). ~90 consumer sites total bridged with `.Float64()` across `pkg/layout/{flex,block,grid,multicol,replaced,min_max_sizing,table}_layout.go` and `fragment_geometry.go` self-references. **Rounding-mode discovery during 13e′.1**: first attempt used `FromFloat64Trunc` to mirror Blink's `LayoutUnit(float)` ctor; regressed css-multicol 179 → 172 because louis14 stores `Length.value` as `float64` (Blink stores `float32`), and `8.2 * 50.0` in IEEE 754 doubles yields `409.99999...` which Trunc snapped DOWN to `409.984` (off by 1/64 raw, shifting the column-rule by 1 px). Fix: switch to `FromFloat64Round` at the boundary — round-half-away-from-zero absorbs the IEEE 754 noise while preserving bit-exact round-trip for 1/64-clean inputs. The Round-mode pattern carried cleanly through 13e′.2 + 13e′.3 with no further rollbacks. The float boundary at length-resolution EXIT is now closed (13e closed the ENTRY at `ResolvePercent`). | `pkg/layout/fragment_geometry.go` (6 producers); ~90 consumer sites in `pkg/layout/{flex_layout,replaced_layout,min_max_sizing,block_layout,grid_layout,multicol_layout}.go`. | Six invariants per sub-step (CSS2 99/99 · flex 626/629 · position 91/104 · wm 781/781 · multicol 179/455 · spanner-frag 12/13) — held at every checkpoint. | Medium → CLOSED. |
| 13f | **Text-shaping boundary — DONE 2026-04-25 (CLOSED)**. `pkg/text` exposes `MeasureText`/`MeasureTextVerticalFromFont` returning `layoutunit.LayoutUnit` (via `FromFloat64Ceil` analog of `ShapeResult::SnappedWidth`); `ShapeAdvances`/`ShapeAdvancesMixed` return `text.ShapeCumulative` carrying paired floor/ceil-snapped LayoutUnit cumulative-position slices (Blink's `SnappedStart/EndPositionForOffset`); consumers read `cum.SnappedWidth(s, e)` (Blink's `CachedWidth(start, end)` analog). HarfBuzz output stays `float64` *inside* the `mazarin/textshape` package. **Landed sub-steps:** 13f.1 snap helpers (`c5c9b67c`); 13f.2 MeasureText* return-type promotion (`76ef4cb4`); 13f.3 ShapeAdvances*/Mixed return-type promotion (this commit); 13f.4 closed as "skip" — HarfBuzz int32 26.6 → /64.0 → LayoutUnit raw is already lossless, no shaper-internal `TextRunLayoutUnit` coordination needed. | `pkg/text/{shape_snap.go,measure.go}`, `pkg/layout/{line_breaker.go,engine.go}`. | wm 781/781 held at every checkpoint. | Medium → CLOSED. |
| 13g | **Paint-time pixel snap** — port `SnapSizeToPixel(size, location)` (and `SnapSizeToPixelAllowingZero` companion) to `pkg/geometry/layoutunit` (mirrors Blink's `platform/geometry/layout_unit.h` placement, NOT `pkg/render` per original brief — Blink keeps the primitive in geometry; painters call via rect-wrappers like `PhysicalRect::PixelSnappedWidth/Height`). Replaces ad-hoc `math.Round` in border/background-edge painting. The "preserve thin lines" rule (Blink's `>4 raw → ±1 px` clause for lines below 1/16 px that would round to 0) closes the class of "0.5px border vanishes" bugs. **13g.1 DONE 2026-04-25** — additive helper in `pkg/geometry/layoutunit/layoutunit.go` (mirror of Blink verbatim incl. >4-raw clause + AllowingZero companion); 24 unit tests covering integer/half-px origins, thin-line clause sign symmetry, sub-threshold non-firing, edge-difference adjacency property; no callers; gate-sweep held by construction. Sub-step staging: **13g.2** migrate inner border-edge corners at `pkg/render/render.go:2672-2675` (and the parallel sites at :2779, :2940, :4209) — the highest-impact site, where ad-hoc `math.Round` bypasses even louis14's existing `pixelSnap` helper; **13g.3** route `pixelSnap(x,y,w,h)` (render.go:1525) through `SnapSizeToPixel` so all 13 callers inherit the thin-line rule; **13g.4+** background/clip clusters one at a time. Image / shadow / text clusters out of scope per `findings.md` "Phase 13g research" inventory. | `pkg/geometry/layoutunit/{layoutunit,layoutunit_test}.go` for 13g.1; `pkg/render/render.go` for 13g.2+. | Verify clear-001 + any `border-*-width: 0.5px` test. **This is where clear-001 most likely closes** — sub-pixel snap differences, not LayoutUnit arithmetic, are the suspected cause. | Medium. |
| 13h | **Verification + cleanup** — re-run full gate sweep, re-examine clear-001, file follow-ups for any newly-surfaced bugs, document the float64 → LayoutUnit migration retrospective. | Tracking files. | Closure. | Low. |

### Acceptance criteria

- All invariants held: CSS2 99/99, css-flexbox 626/629, css-position **≥91/104** (target: 92/104 if clear-001 closes from 13g), spanner-fragmentation 12/13, css-writing-modes 781/781, css-multicol ≥179/455.
- Zero `float64` fields in any geometry struct under `pkg/layout/`. Verified by static scan (a small custom analyzer or `go vet`-style rule).
- All float→LayoutUnit conversions go through a `From*` constructor with explicit rounding mode. Greppable invariant: no `LayoutUnit{int32(x*64)}`-style raw coercion outside the package.
- New package builds clean and unit tests cover saturating arithmetic, rounding modes, percentage round-trip, writing-mode permutations.

### Discipline (per-phase)

1. Read the relevant Blink site before each sub-phase (cited in findings.md §Phase 13).
2. Land each sub-phase in its own commit gated on full WPT invariant sweep. No "land 13a-c then test" big-bang merges.
3. Sub-pixel diff regressions (≤200 px cumulative) are real bugs per CLAUDE.md rule 3 — fix at source, do not accept.
4. If a sub-phase regresses an invariant, roll back and re-design before continuing — do NOT bandaid forward.

### Out of scope for Phase 13

- `pkg/css` length parsing stays `float32` for the `Length.value` field — Blink does the same. The migration changes `Resolve()`'s return type, not internal storage.
- Transforms (`pkg/render` matrix operations) stay `float64`. Conversion at the boundary uses an `EnclosingRect`-style "floor offset / ceil far-edges" rule. A full transform-precision audit is its own future phase.
- Mazzy-side `mazarin/textshape` changes (e.g. an `InlineLayoutUnit` 16-bit-fractional shaper-internal accumulator) are deferred. Phase 13 does the louis14 side; if shaper precision shows up as a residual we revisit then.

---

## css-position Goal (prior category, 91/104 — effectively complete)
All 104 tests under `pkg/visualtest/testdata/wpt-css3/css-position/` exercised via `TestWPTCSS3Reftests/css-position`. Baseline (2026-04-21): **50 passing, 54 failing, 5 no-run**. Current (2026-04-25, gate-swept at 13e.1/13e.2 HEAD): **91 passing, 13 failing**. The 13 failures are all pre-existing residuals — none caused by Phase 12 or Phase 13. (`position-change.html` was a deferred residual until the 2026-04-25 HTML tokenizer EOF-in-tag fix — see progress.md. Earlier text claimed `92/104 (+1 from HTML fix)` but that math didn't match the residuals table; ground-truth at 13e.1 HEAD — commit `ce5dc7f2`, additive only — is 91/104 with these 13 entries.)

| Test | Pixels | Group | Notes |
|------|--------|-------|-------|
| `clear-001.xht` | 96 (0.0%) | G-SINGLETONS | deferred pending Blink LayoutUnit trace |
| `containing-block-change-scrollframe.html` | 50000 (10.4%) | G-SCROLL | scroll-based CB change |
| `position-absolute-semi-replaced-stretch-button.html` | 15885 (3.3%) | G-SEMI-REPLACED | abspos stretch on button |
| `position-absolute-semi-replaced-stretch-input.html` | 25509 (5.3%) | G-SEMI-REPLACED | abspos stretch on input |
| `position-absolute-semi-replaced-stretch-other.html` | 4217 (0.9%) | G-SEMI-REPLACED | abspos stretch on other |
| `position-relative-table-tbody-left/top-absolute-child` (×2) | 5000 (1.0%) each | G-ABS-IN-TABLE | abspos child of position:relative tbody |
| `position-relative-table-tfoot-left/top-absolute-child` (×2) | 5000 each | G-ABS-IN-TABLE | abspos child of tfoot |
| `position-relative-table-thead-left/top-absolute-child` (×2) | 5000 each | G-ABS-IN-TABLE | abspos child of thead |
| `position-relative-table-tr-left/top-absolute-child` (×2) | 5000 each | G-ABS-IN-TABLE | abspos child of tr |

**Note:** The tracking file previously claimed "95 passing" and later "92 passing" — both were incorrect. Ground-truth at 13e.1/13e.2 HEAD (gate-swept 2026-04-25) is **91/104** with the 13 residuals listed in the table above. The 8 G-ABS-IN-TABLE and 3 G-SEMI-REPLACED tests were never fixed; they were inadvertently omitted from earlier residuals lists. The 2026-04-25 HTML tokenizer EOF-recovery fix did close `position-change.html`; the "+1 to 92" arithmetic in earlier landing notes off by one — gate sweep gives 91 unambiguously.

Invariants (must stay green on every landing). Last verified 2026-04-25 across the eight Phase 13c/13d sub-step commits, all green:
- css-writing-modes **781/781**. Restored to full pass at F1 close (2026-04-25, commit `41b674ef` plus mazzy `d6b27049`/`cde2c29`); the earlier 12d-era 410/781 dip from `pkg/resource/renderer.go` modifications has cleared.
- CSS2 **99/99**. Restored from 12d-era 96/99.
- css-flexbox **≥626/629** (3 pre-existing residuals tracked in Phase 11).
- css-transforms 172/381 watch (not an invariant; post Phase 9 stack-floats refactor, +10 vs baseline).
- css-position **≥91/104** (gate-swept 2026-04-25 at 13e.1/13e.2 HEAD; ground-truth = 13 pre-existing residuals listed in the table above; the earlier-claimed "92/104 after HTML tokenizer fix" did not match the residuals enumeration and is corrected here). 13 residuals remain pre-existing, out-of-scope for Phase 12 / Phase 13.
- spanner-fragmentation **12/13** watch (005 pre-existing residual since 12b).
- css-multicol (active target) **179/455** — gains +25 cumulative across F2/F3/F4/F5 from the 12-entry baseline (154 post-12g).

## Rules & Discipline
Authoritative sources (re-read at session start):

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index.

If you find yourself about to type a project rule into this file, stop and link instead.

## Archived wm work
The css-writing-modes category is complete. Its planning/findings/progress have been moved into `docs/`:
- `docs/plan-wm.md` — full 787-test plan (Phases 0–7, foundational groupings A/B/C, all done)
- `docs/findings-wm.md` — bucket analysis, Blink entry points, post-mortems
- `docs/progress-wm.md` — session-by-session log through Group C fix

Do not duplicate wm notes here.

## Baseline snapshot (2026-04-21)
Log: `output/baselines/css-position-2026-04-21.log`
- 104 tests exercised: **50 PASS · 54 FAIL · 5 NORUN** at baseline.
- Latest (post Phase 8 closed, commit `0e1fde9f`): **85 PASS · 19 FAIL** in this category.
- Failing test list + diffs: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`).

Highest-diff outliers (snapshot at baseline; `hypothetical-dynamic-change-003.html`, `sticky-top-001.html` closed in Phases 4/7 respectively):
| % | px | test | group |
|---|---|------|-------|
| 10.4% | 50000 | `containing-block-change-scrollframe.html` | G-SCROLL (was G-CB-CHANGE) |
|  4.2% | 20000 | `containing-block-change-button.html` | G-SINGLETONS (was G-CB-CHANGE) |
|  4.2% | 20000 | ~~`hypothetical-dynamic-change-003.html`~~ | ~~G-HYPO~~ — **DONE** |
|  3.4% | 16308 | ~~`sticky-top-001.html`~~ | ~~G-STICKY~~ — **DONE** |
|  1.0% |  4672 | `position-fixed-scroll-nested-fixed.html` | G-FIXED residual (paint-clip / scrollTop) |

5 NORUN — **triaged 2026-04-21** (full table in `findings.md`):
- 4 are runner **SKIPs** ("no usable reference files found") — infrastructure gaps, not layout bugs:
  - `hypothetical-box-scroll-parent` (ref file missing from snapshot)
  - `hypothetical-box-scroll-viewport` (missing `window.scrollTo` + ref)
  - `position-absolute-multicol-001` (absolute-path ref unresolved by runner)
  - `replaced-object-backdrop` (`<object popover>` JS unsupported + absolute-path ref)
- 1 is a real **FAIL** miscounted as NORUN because the parser-error log format doesn't match our regex: `position-change.html` — HTML parser bails with `tokenizer error: expected '>' but reached EOF`. Counted as a real failure (moves to G-SINGLETONS).

**Target revision.** True runnable set = 100 tests; true failure count = 55 (54 original + position-change). The 4 SKIPs need harness / JS-engine work and are **out of scope for this plan**. Deliverable: **100/100 runnable at 0 diff** (104 total if the SKIPs are later un-skipped by separate infra work).

## Groups (root-cause-oriented, not %-diff-oriented)
Full detail in `findings.md`. 54 failing tests cluster into **11 groups** by likely shared root cause. Fix one representative test per group; if the root cause is correct, siblings fall out for free.

| # | Group | Count | % range | Estimated shape of root cause |
|---|---|---|---|---|
| 1 | ~~**G-TABLE-REL**~~ — position:relative on table-internal elements (thead/tbody/tfoot/tr/td) — **DONE** | 11 (primary) | 0.4–1.7% | Closed by commits `d174049b`, `ac2dc780`, `b6ec7d3f`. 8 `-absolute-child` variants moved to G-ABS-IN-INLINE / G-ABS-IN-TABLE. |
| 2 | **G-ABS-CENTER** — abspos centering with `margin: auto` + both-axis insets (`css-align-3` abspos sizing) | 5 | 0.3–2.1% | Abspos available-space = 2 × distance(center→closest edge); auto-margin distribution |
| 3 | **G-CB-CHANGE** — dynamic change of containing-block establishment (JS toggle of overflow/button/height) | 3 | 0.5–10.4% | Abspos children need re-resolve to new CB after JS mutation |
| 4 | **G-DYN-STATIC** — dynamic static-position re-layout (JS-triggered property flips affect float/inline/table-cell static pos) | 6 | 0.3–2.1% | Static position rectangle recomputation on relayout |
| 5 | **G-HYPO** — hypothetical position dynamic change + scroll (fixed/abs ancestor moves) | 3+2 NORUN | 2.1–4.2% | `HypotheticalBoxPosition` not recomputed when ancestor offset changes |
| 6 | **G-ROOT-FLEX-GRID** — `<html>` as position:fixed/absolute root with `display: flex|grid` | 4 | 0.8% | Root-element OOF sizing — insets must resolve against ICB even when `display` is flex/grid |
| 7 | **G-FIXED** — nested OOF re-entrance + scroll-clip escape for fixed | 2 (1 closed) | 0.5–4.2% | OOF resolver wasn't re-entrant. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` 2026-04-21. Residual on `position-fixed-scroll-nested-fixed` is paint-clip / scrollTop, not layout. |
| 8 | **G-ABS-IN-INLINE** — abspos whose containing block is an inline (CSS2 §10.1.4) | 2 | 2.3–2.9% | Inline-CB bounding box computation for abspos children |
| 9 | ~~**G-STICKY**~~ — `position: sticky` at scroll=0 must stay in normal flow — **DONE** | 1 | 3.4% → 0% | Closed by commit `05aff97e` — sticky emits zero layout-time offset (Blink-faithful); scroll-time `StickyPositionScrollingConstraints` deferred. |
| 10 | ~~**G-REPLACED**~~ — abspos replaced elements with no intrinsic size / `max-content` sizing — **DONE** | 1 | 2.1% → 0% | Closed by commit `0e1fde9f` — stretch-fit gate now excludes replaced elements; `ComputeReplacedSize` + auto-margin path (CSS 2.2 §10.3.7 / §10.6.5). |
| 11 | **G-SINGLETONS** — `clear-001` (96px), `position-absolute-dynamic-list-marker` (18px), `stack-floats-001`, `position-absolute-iframe-print-001/002`, `position-relative-011/012/013` (%-top on table rows), plus 3 NORUN (`position-change`, `replaced-object-backdrop`, `position-absolute-multicol-001`) | 11 | 0.0–1.7% | Heterogeneous; attack last |

## Attack order (foundational impact ÷ effort)

Research insights from Blink study (2026-04-21) reshape the ordering — **G-DYN-STATIC is a prerequisite for both G-ABS-CENTER and G-HYPO**, and the IMCB machinery is shared between G-ABS-CENTER and G-HYPO.

1. ~~**G-TABLE-REL (11 primary tests).**~~ **Done 2026-04-21** — commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Relative offset moved into shared `BoxFragmentBuilder.AddChild`; positioned thead/tbody/tfoot emit section fragments; inline-block §10.8.1 last-baseline fallback corrected.
2. ~~**G-CB-CHANGE (3 tests).**~~ **Dissolved 2026-04-21** (audit no-op) — our harness already does fresh relayout post-JS. Tests reassigned to G-FIXED / G-SINGLETONS / G-SCROLL.
3. ~~**G-DYN-STATIC (6 tests).**~~ **Done 2026-04-21** — commits `233d408f` (a), `d250c5cf` (b+d), `5399d328` (c) (orphan-cell vertical-align at `block_layout.go` + transform percent-sentinel fix at `pkg/css/style.go`). Original "rebuild via `OutOfFlowPositionedDescendants` list" hypothesis was invalidated — our harness already relays out fresh; the real bugs were per-FC static-position computation at each capture site.
4. ~~**G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests).**~~ **Done 2026-04-21** — commits `a3c8db38` (Commit 1: absolute_utils.go), `d9f6628b` (Commit 2: wire resolver), Commit 3 (residual 3). IMCB machinery ported at Blink type/function parity.
5. ~~**G-ROOT-FLEX-GRID + G-FIXED (5 tests).**~~ **Done 2026-04-21 (partial)** — G-FIXED Part A (OOF re-entrance) via commit `ed16475f`; G-ROOT-FLEX-GRID via commit `7e686a28` (`pkg/layout/positioned_root.go`). Residual: G-FIXED Part B (paint-clip / scrollTop) overlaps G-SCROLL, deferred.
6. ~~**G-ABS-IN-INLINE (2 tests).**~~ **Done 2026-04-21** — commit `01f468d9`: new `pkg/layout/inline_containing_block.go` mirrors `InlineContainingBlockUtils::ComputeInlineContainerGeometry`.
7. ~~**G-STICKY (1 test).**~~ **Done 2026-04-21** — commit `05aff97e`: sticky emits zero layout-time offset, matching Blink. Full `StickyPositionScrollingConstraints` deferred until scroll-based sticky tests appear.
8. ~~**G-REPLACED (1 test).**~~ **Done 2026-04-21** — commit `0e1fde9f`: stretch-fit gate in `out_of_flow_layout.go` now excludes replaced elements so abs-replaced stays on `ComputeReplacedSize` + auto-margin path (CSS 2.2 §10.3.7 / §10.6.5).
9. **G-SINGLETONS (11 tests, includes `position-change`).** Sweep last; some (e.g. `position-relative-011/012/013`) are expected to close when G-TABLE-REL lands. **Next up.**

Each group runs through the same discipline loop (CLAUDE.md §1–§4):
1. **Study Blink.** Read the relevant Blink file (entry points in `findings.md`). No code before this step.
2. **Pick a representative failing test.** Read test + ref HTML; identify the DOM/CSS invariant being exercised.
3. **Instrument.** Use `/tmp/scanimg.go`-style pixel tools when visual delta is ambiguous; stderr-log layout metrics when geometric.
4. **Write the fix.** Narrow as possible. Mirror Blink's type names and algorithm order.
5. **Verify target + regression-adjacent.** Run only the failing group + 1–2 adjacent groups (CLAUDE.md §4).
6. **Regression sweep before commit.** wm full + CSS2 full + flex spot-check.

## Phases

### Phase 0: Baseline — **DONE 2026-04-21**
- [x] Fresh baseline run: `output/baselines/css-position-2026-04-21.log`
- [x] Parse into failing list: `/tmp/css-position-fails.tsv`
- [x] Group by root cause: 11 groups, see `findings.md`
- [x] Triage the 5 NORUN entries → 4 SKIP (infra), 1 real FAIL (`position-change.html`). Details in `findings.md` "NORUN triage".
- [x] Blink research for 7 groups (G-TABLE-REL, G-CB-CHANGE, G-DYN-STATIC, G-ABS-CENTER, G-HYPO, G-STICKY, G-ABS-IN-INLINE) — see `findings.md` per-group "Blink entry points" sections.

### Phase 1: G-TABLE-REL (11 primary tests) — **DONE 2026-04-21**
- [x] Blink research: relative offset is applied in `BoxFragmentBuilder::AddChild` via `ComputeRelativeOffsetForBoxFragment`. Fragment-builder-level, not algorithm-level.
- [x] Decision (2026-04-21, user-directed): do what Blink does — push the check into our shared `BoxFragmentBuilder.AddChild`. Parent's content-box size is the CB for percentage resolution.
- [x] Scope narrowed after isolated test run (2026-04-21): `td-top` / `td-left` diffs were a *baseline* bug, not a RelativeOffset bug. The green cell box was already shifted correctly; residual was a ~4px text-offset below the table.
- [x] Part A — RelativeOffset at shared `AddChild`. Committed `d174049b`. Removed duplicate tail blocks in block/flex/grid/inline layout algorithms.
- [x] Part B — emit section fragments (thead/tbody/tfoot) in `table_layout.go` so positioned sections have a fragment to attach to. Committed `ac2dc780`.
- [x] §10.8.1 fix — inline-block last-baseline. Committed `b6ec7d3f`. Two edits: stop synthesizing table LastBaseline at content-box block-end when no cell has a text baseline, and stop propagating `childResult.Baseline` as `LastBaseline` in block_layout. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, a block has a last-baseline only if a line-box descendant provides one; otherwise the inline-block's §10.8.1 bottom-margin-edge fallback fires at atomic-inline placement.
- [x] Regression sweeps held: wm 781/781, CSS2 99/99.
- [x] Result: all 11 primary `position-relative-table-*` tests pass at 0 px diff. 8 `-absolute-child` variants remain out of scope (tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE).

### Phase 2: G-CB-CHANGE (3 tests) — **plan invalidated 2026-04-21; group dissolved**
- [x] Blink research: `StyleDifference::NeedsPositionedLayout` + `LayoutBlock::RemovePositionedObjects(stay_within)`.
- [x] Audit (2026-04-21): our `pkg/visualtest/helpers.go:85-102` already runs `engine2 := layout.NewLayoutEngine(...)` from-scratch on the post-JS DOM. There is no caching to invalidate. JS mutations land correctly (`fixed.style.height = "300px"` → inline-style attr `"height: 300px"`, pass-2 sees it). The Blink invalidation pattern doesn't apply.
- [x] Per-test triage (see `findings.md` "G-CB-CHANGE — Phase 2 audit invalidated"):
  - `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5%) → **G-FIXED** (positioned-fragment box-tree gap; pos:fixed/absolute boxes absent from box tree).
  - `containing-block-change-button` (4.2%) → **G-SINGLETONS** (`<button>` vertical-centering rendering bug).
  - `containing-block-change-scrollframe` (10.4%) → new **G-SCROLL** sub-group (needs `Element.scrollTop` setter + `overflow:hidden` scroll paint).
- [x] Phase 2 closed as a no-op — no code changes needed for "invalidation". Move on to next phase per revised attack order.

### Phase 3: G-DYN-STATIC (6 tests) — **DONE 2026-04-21**
- [x] Blink research: static position NOT cached; rebuilt each pass via `LayoutResult::OutOfFlowPositionedDescendants` list.
- [x] Audit (2026-04-21): we already rebuild every pass via fresh `engine2`. Original "add OutOfFlowPositionedDescendants list" hypothesis is a no-op. Real root causes are per-FC COMPUTATION bugs in static-position capture sites. See `findings.md` "G-DYN-STATIC — Phase 3 hypothesis invalidated".
- [x] **(a) `inline_layout.go:682-694`** — split by child's `display`. Block-level abspos → `(0, lineBlockEnd)` when in-flow content precedes on the line, `(0, blockOffset)` otherwise; inline-level abspos → `(inlinePos, blockOffset)`. Helper `isInlineLevelDisplay` mirrors Blink's `ComputedStyle::IsOriginalDisplayInlineType`. `hasInflowOnLine` flag mirrors `line_box_.LineBoxBlockEnd()` at time-of-encounter so the first-child-block-level case (no prior in-flow) stays at `blockOffset`. Closes `inline` (2.1% → 0%). wm 781/781 ✓, CSS2 99/99 ✓. Commit `233d408f`.
- [x] **(b) `block_layout.go:217-237`** — for inline-level abspos children, query `exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)` and use the returned inline-start offset directly as `InlineOffset` (no bfcInlineOrigin subtraction — floats are stored with LOCAL inline offsets, matching how inline_layout's line-start recomputation uses them). Closes `floats-001` (0.7% → 0%), `floats-002` (0.3% → 0%), `floats-003` (0.3% → 0%), AND `floats-004` RTL (0.7% → 0%). Turns out (d) is covered by the shared exclusion-space path (`PhysicalFloatToExclusionSide` already flips for RTL; the query is direction-agnostic). wm 781/781 ✓, CSS2 99/99 ✓. Commit `d250c5cf`.
- [x] **(c) orphan table-cell (NOT the originally-planned site).** Investigation: target test uses `display:table-cell` with **no table ancestor**. `normalizeTableSubtrees` in `layout_tree_builder.go` doesn't wrap it (reverse §17.2.1 is unimplemented), so layout dispatches to `block_layout.go`, not `table_layout.go`. Two fixes: (i) orphan-cell vertical-align in `block_layout.go` (applied to in-flow children + OOF candidates, guarded by `space.TableSectionData == nil`); (ii) transform parser percent-sentinel fix in `pkg/css/style.go` + `pkg/render/paint_layer.go` (added `IsPercent []bool` on `Transform`; widened `GetIndividualTranslate` signature to return explicit percent flags; updated 3 `louis13/` callers). Closes `table-cell` (2.1% → 0%) and +9 css-transforms for free. **Uncommitted** pending user review.
- [x] **(d) RTL-direction awareness** — **NO-OP, closed by (b)**. `floats-004` passes because `exclusionSpace.FindAvailableInlineSize` operates on `PhysicalFloatToExclusionSide`-normalised sides (ExclusionInlineStart = visual-start regardless of direction). No separate edge-annotation flip needed on capture.
- [ ] **Tech debt**: proper-table path vertical-align on propagated OOF candidates. Structural design (OOF-candidate `vaBlockShift` during row sweep in `table_layout.go`) drafted but dropped from this phase because the `contentBlockSize` pre-stretch change regressed 3 wm orthogonal-writing-mode tests. Revisit when a test requires vertical-align centering of abspos inside a real `<table><td>`.
- [x] Per-site commits with wm 781/781 + CSS2 99/99 regression gate after each.
- [x] Representative drivers: `inline` (2.1%) for (a); `floats-001` (0.7%) for (b); `table-cell` (2.1%) for (c); `floats-004` (0.7%) for (d).

### Phase 4: G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests) — reframed 2026-04-21 as Blink-parity-first
**Goal.** Port Blink's OOF sizing layer (`absolute_utils.cc`) to louis14 at function/type/algorithm parity. Closing the 8 target tests is a *verification* that the port is correct, not the target. Reframing driven by CLAUDE.md §2 — "study Blink, mirror names and structure" — and the concern that point-fixing the 8 tests would let our OOF sizing code diverge further from Blink before we port it cleanly.

**Blink-parity items our code is missing today** (audit 2026-04-21):
1. `InsetBias` enum (kStart/kEnd/kEqual).
2. `LogicalAlignment` struct (carries `align-self`/`justify-self` to the resolver). `align-self`/`justify-self` are NOT currently consulted for abspos.
3. `InsetModifiedContainingBlock` type (CB size minus insets; used for percentage resolution of the abspos child — today we pass the raw CB size to `SetPercentageResolutionSize`).
4. `LogicalOofDimensions` output struct (inset + size + margins, capturing the resolved rect).
5. Center-clipping collapse in `ComputeUnclampedIMCBInOneAxis` (the `2 × min(static, cb − static)` rule in the both-insets-auto + kEqual branch). Never exercised today because no candidate site sets `StaticEdgeCenter`.

**Existing Blink-parity items** (reuse):
- `LogicalStaticPosition` with `InlineEdge/BlockEdge` (`StaticEdgeStart/Center/End`) — already 1:1 with Blink. `pkg/layout/static_position.go:25-29`.
- `LogicalInsets` with `HasInlineStart/...` flags — close to Blink's `LogicalOofInsets`; we will reuse.
- OOF resolver worklist pattern — mirrored in Phase 5 Part A (`out_of_flow_layout.go:58-77`).
- Both container + child WDM already threaded (`out_of_flow_layout.go:37-39, 89, 103`).

**Explicit scope boundaries — NOT ported in Phase 4** (named now so later Blink-parity work doesn't find hidden gaps):
- Anchor positioning (`LogicalAnchorCenterPosition`, `anchor-center` alignment). No WPT tests in css-position exercise it. Leave `TODO(anchor-positioning)` breadcrumbs where Blink's signatures take anchor params.
- Table-specific IMCB clamp in `ComputeInsetModifiedContainingBlock`'s table-overflow branch. Skip until a css-tables test requires it.
- Fragmentation column/page context for OOF. Out of scope.

#### Commit 1 — Pure module (types + algorithmic functions, no wiring) — **DONE 2026-04-21, commit `a3c8db38`**
- [x] Created `pkg/layout/absolute_utils.go` (612 lines):
  - Types: `InsetBias` (enum), `ItemPosition` (enum), `AlignmentData`, `LogicalAlignment`, `LogicalOofInsets` (+ `LogicalInsets.AsOofInsets()`), `InsetModifiedContainingBlock`, `LogicalOofDimensions`.
  - Pure functions (Blink naming): `GetAlignmentInsetBias`, `axesOppose`, `ComputeUnclampedIMCBInOneAxis` (including center-clipping collapse `2 × min(static, cb − static)`), `ResizeIMCBInOneAxis`, `ComputeUnclampedIMCB`, `BiasFromStaticEdge`, `ComputeMargins`, `ComputeInsets`.
  - `ComputeOofInlineDimensions` / `ComputeOofBlockDimensions` deferred to Commit 2 (need layout-engine context).
  - No integration; no existing caller changes.
- [x] `absolute_utils_test.go`: 16 unit tests — three IMCB branches, ResizeIMCB, ComputeMargins, GetAlignmentInsetBias, ComputeInsets end-overflow fallback. All pass.
- [x] **Gate:** compiles; 16/16 new unit tests pass; no wm/CSS2/flex impact (dead code pending Commit 2).

#### Commit 2 — Wire resolver + alignment (Blink-parity behavior change) — **landed 2026-04-21**
- [x] `out_of_flow_layout.go` `layoutCandidatesOnce`: rewritten to use `ComputeUnclampedIMCB` + `ComputeMargins` + `ComputeInsets`. Static position shifted into CB-padding-box on input (`+ containingBlockPadding.Start`) and back to CB-content-box on output (`- containingBlockPadding.Start`).
- [x] Pre-layout fixed-size on both axes when both insets specified + child size is auto: `IMCB.size - non-auto-margins - child-BP`.
- [x] Indefinite-cbBlock fallback (preserves prior per-case formulas for the block axis when IMCB math isn't meaningful).
- [x] `OutOfFlowCandidate.Alignment LogicalAlignment` field added; zero value (ItemPositionNormal) yields BiasStart → compatibility with existing callers that don't populate it yet.
- [x] Flex OOF static-edge derived from parent's `justify-content` (main) + `align-items` (cross) via new helpers `flexOOFStaticMain` / `flexOOFStaticCross`. Mapped back to inline/block based on row-vs-column. (`flex_layout.go`.)
- [x] `absolute_utils.go` both-auto `BiasEqual` branch arms `defaultInsetBias = BiasStart` so the default-overflow fallback snaps centered abspos to the start edge when overflowing (mirrors Blink).
- [x] `ComputeUnclampedIMCB` propagates static-center overflow flag (both insets auto + `StaticEdgeCenter`) into `InsetModifiedContainingBlock.InlineHasDefaultAlignmentOverflow` / `BlockHasDefaultAlignmentOverflow`.
- [x] Propagated OOF candidates from a laid-out OOF ancestor: coordinate-translate their `StaticPosition.Offset` from the ancestor's content-box into the CB's content-box (add `finalInlineOffset + parentBP.InlineStart`, symmetric for block). Cross-WM physical-round-trip when `childWDM != wdm`. Mirrors `block_layout.go` `PropagateOOFCandidates`.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (+0 vs post-Phase-3 baseline). css-position **68 → 74** (+6). Closes `position-absolute-center-001/003/004/006` (G-ABS-CENTER) + `hypothetical-dynamic-change-001/002` (G-HYPO).
- [ ] Residual (3 of 8 targets) pushed to Commit 3: `position-absolute-center-002` (vertical-rl + column flex + align-items:center), `position-absolute-center-007` (`display:table` with auto margins + both insets + `margin-top:-50px`), `hypothetical-dynamic-change-003` (position:relative ancestor's left-offset must propagate into fixed descendant's static position).

#### Commit 3 — Residual 3 tests — **landed 2026-04-21**
- [x] `hypothetical-dynamic-change-003`: `block_layout.go` PropagateOOFCandidates now adds the positioned ancestor's `RelativeOffset` to each propagated candidate's `StaticPosition.Offset` so the fixed descendant's static position reflects the ancestor's `left`. Mirrors Blink's `OutOfFlowLayoutPart::PropagateOOFPositionedInfo` carrying `RelativeOffset`.
- [x] `position-absolute-center-002`: removed legacy `_writing-mode-inherited` early-return in `pkg/css/cascade.go` and `pkg/css/style.go` `resolveLogicalSizeProperties`. The skip was a louis13 artifact tied to a `transformToVerticalRL` post-pass that doesn't exist in louis14; it caused inline descendants of a `vertical-rl` container to keep `inline-size` mapped to physical `width` instead of `height`. +1 target test, +19 other CSS3 tests, zero regressions.
- [x] `position-absolute-center-007`: `out_of_flow_layout.go` now gates the IMCB stretched-fit path on `isNonStretchableDisplay(childStyle)` — tables (`display:table` / `display:inline-table`) keep intrinsic sizing and let auto margins absorb leftover space per CSS 2 §17.5 + Align 3 §8.2. Mirrors Blink's `node.IsTable()` gate in `absolute_utils.cc` `ComputeOof{Block,Inline}Dimensions`.
- [x] Paint-ordering regression (flex): `paint_layer.go` `sortZLists` preserves order-modified paint for flex items. DOMIndex-sort of AutoZero (added for hypothetical-003) broke flex paint ordering because flex items can hoist to an enclosing stacking context. Fix: skip the DOMIndex sort whenever any AutoZero entry `IsFlexItem()`.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓, css-position 74 → 77 (+3). All 3 residual tests at 0 diff.

**Representatives:** `position-absolute-center-001.html` (0.4%, drives Commits 2-3), `position-absolute-center-007.html` (2.1%, most likely to exercise center-clipping), `hypothetical-dynamic-change-001.html` (G-HYPO verification).

### Phase 5: G-ROOT-FLEX-GRID + G-FIXED (5 tests, 1 closed)
- [x] **G-FIXED Part A — OOF resolver re-entrance.** `OutOfFlowLayoutPart.LayoutCandidates` was dropping `childResult.PropagatedOOFCandidates`. Mirrored Blink's `OutOfFlowLayoutPart::LayoutOOFNodes` worklist pattern. Returns unresolved fixed candidates to caller; new `resolvesFixed` flag selects ICB / transform-or-containment-CB sites that absorb fixed. Updated all 7 call sites. Closes `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5% → 0%); reduces `position-fixed-scroll-nested-fixed` (4.2% → 1.0%). Residual diff is paint-time scroll/clipping (fixed escaping `overflow:auto`), not layout — defer.
- [x] **G-ROOT-FLEX-GRID (4 tests).** Blink research (2026-04-21): `layout_view.cc` has no special ICB-level IMCB short-circuit; `LayoutView::LayoutRoot` builds a viewport-sized fixed constraint space, then the root `<html>` is discovered as OOF in the LayoutView's in-flow pass and routed through `OutOfFlowLayoutPart::LayoutOOFNodes` → `absolute_utils.cc`'s `ComputeOof{Inline,Block}Dimensions`. With both insets specified + `align-self: normal`, the auto length resolves to `Length::Stretch()` against `imcb.InlineSize()` — box fills IMCB instead of shrinkwrapping.
- [x] Implementation (commit `7e686a28`): new `pkg/layout/positioned_root.go` with `buildRootConstraintSpace` + `resolvePositionedRootOffset`. When the root is `position:absolute/fixed`, pre-layout the root against an IMCB-derived constraint space (fixed inline/block size when both insets are specified + size is auto), then post-layout compute the final physical offset via `ComputeMargins` + `ComputeInsets` + WritingModeConverter. `engine.go` Layout() and `layoutNestedDocument()` both route through the helpers; non-positioned roots keep the existing viewport-stretched path verbatim.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (unchanged). All 4 G-ROOT-FLEX-GRID tests at 0 diff: `position-absolute-root-element-flex`, `position-absolute-root-element-grid`, `position-fixed-root-element-flex`, `position-fixed-root-element-grid`.
- [x] Regression + commit (`7e686a28` code, `6c53f52d` planning).
- [ ] G-FIXED residual: scroll-clip escape for fixed inside `overflow:auto` scrollable, plus `Element.scrollTop` JS setter (overlaps G-SCROLL).

### Phase 6: G-ABS-IN-INLINE (2 tests) — **DONE 2026-04-21**
- [x] Blink research: `inline_containing_block_utils.cc` — union of first + last line-box fragment rects.
- [x] New `pkg/layout/inline_containing_block.go`: `ComputeInlineContainerGeometry` + `BuildPositionedInlineMap` + `InlineCBLogical`. Walks line-box fragments (transparently descends anonymous block continuations from block-in-inline splits), unions first-line and last-line fragment rects for the target inline's DOM node, converts to logical via the block's writing-mode converter.
- [x] Wire into OOF pass. `inline_layout.go` tags `InlineItemOutOfFlow.InlineContainer` via `BuildPositionedInlineMap` (position:fixed excluded — CB is viewport, not inline). `block_layout.go` resolves geometry before OOF layout; when the inline produced no line-box fragments (CSS 2.1 §9.4.2 line-box suppression for OOF-only lines) the candidate is routed as a regular non-inline-CB candidate. `out_of_flow_layout.go` tracks `cbOriginInBuilder` and subtracts it from static-position offsets so IMCB math runs in CB coords, then adds it back at `AddChild` time. `layout_tree_builder.go` emits an empty leading continuation for positioned inlines with block-in-inline splits when trailing inline content exists, so the start line-box union rect covers the span's start.
- [x] Regression + commit: M6 landed 2026-04-21 via commit `01f468d9`.

**Verification:** css-position **81 → 83** (+2 of 2 targets closed). `position-absolute-in-inline-003.html` and `position-absolute-in-inline-004.html` both pass at 0 diff.

**Gates:** wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓ (pre-existing residuals unchanged), absolute-tables 14/14 ✓, position-relative-003/004/005 ✓ (no regression), position-relative-002/011/013 baseline unchanged.

### Phase 7: G-STICKY (1 test) — **DONE 2026-04-21 via commit `05aff97e`**
- [x] Blink research: `sticky_position_scrolling_constraints.h` + `ComputeStickyPositionConstraints`; scroll-time offset, not layout-time.
- [x] Layout-time zero: dropped `PositionSticky` from the 7 RelativeOffset-computation sites (`fragment_builder.go` AddChild; `block_layout.go` / `flex_layout.go` / `grid_layout.go` own-result tail blocks; `inline_layout.go` span-background / text / atomic-inline sites). Structural sticky gates (positioned-inline splits in `layout_tree_builder.go`, section fragments in `table_layout.go`, positioned-inline CB stack in `inline_containing_block.go`) preserved so scroll-time wiring has a place to attach.
- [x] `StickyPositionScrollingConstraints` (min/max inset, CB range, sticky box range) deferred until scroll-based sticky tests appear or the engine gains scroll-time fragment offsets.
- [x] Regression + commit (`05aff97e`). sticky-top-001 3.4% → 0%; sticky-basic-001 unchanged (already 0% via top:0). css-position **83 → 84**. Gates: wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓.

### Phase 8: G-REPLACED (1 test) — **DONE 2026-04-21 via commit `0e1fde9f`**
- [x] **Blink research:** CSS 2.2 §10.3.7 / §10.6.5 abs-replaced width/height. Blink's `absolute_utils.cc` `ComputeOof{Inline,Block}Dimensions` dispatches replaced elements to `ComputeReplacedSize` and skips the stretch-fit path that applies to block-level non-replaced non-table children.
- [x] **Audit:** `isAutoSizeInDirection` reports `height:max-content` as auto (no length, no percentage), so with both block-axis insets specified the replaced element was being stretched to IMCB (200px) instead of resolving to intrinsic/ratio-derived 100px.
- [x] **Fix:** in `out_of_flow_layout.go` `layoutCandidatesOnce`, extend the `stretchable` gate to exclude replaced elements (`isReplacedElement(child.DOMNode)`). Then `ComputeReplacedSize` returns 100×100 from `width:100px` + 1:1 viewBox ratio, and `ComputeMargins` auto-distributes the block-axis leftover 100px (margin-top/bottom = 50/50). 7-line change, mirrors Blink's abs-replaced dispatch.
- [x] Regression + commit. wm 781/781 ✓, CSS2 99/99 ✓, flex 626/629 ✓. css-position 84 → 85.

### Phase 9: G-SINGLETONS (11 tests, includes `position-change`)
- [x] Per-test triage; many may already be closed by earlier phases.
- [x] **2026-04-21 (commit `a7e79598`):** closed `position-relative-001/002` (non-table %-top/left) + `position-relative-011/012/013` (%-top on table tbody/tr/td under position:relative). Three fixes:
  - `NewBlockifiedStyle` preserves `position` + `top/right/bottom/left` when block-in-inline split collapses to a single anonymous wrapper.
  - Anonymous auto-height block wrappers propagate parent's `PercentageResolutionSize.BlockSize` instead of resetting to 0.
  - Table cell constraint space carries row's SPECIFIED block-size as its percentage resolution block size; table row's `RelativeOffset` is pre-computed against row group's SPECIFIED block-size (mirrors Blink's chromium bug 1227884 fix).
  - Gates hold: wm 781/781, CSS2 99/99, flex 626/629. css-position 85 → 90.
- [x] **2026-04-21 (commit `1bdcfc85`):** closed `position-absolute-dynamic-list-marker.html`. Root cause in `pkg/css/stylesheet.go`: a bare `::marker` selector produced an empty selector parts list, and `MatchesSelector` rejected every node, so the UA rule `::marker { color: white }` never applied. Fix: per CSS Selectors Level 3 §6.6, a bare pseudo-element is shorthand for `*::pseudo` — default the implicit compound selector to `*` when only a pseudo-element is present. css-position 90 → 91.
- [x] **2026-04-21 (commit `a22cfe10`):** closed `containing-block-change-button.html`. Two fixes bundled:
  - `pkg/css/cascade.go` — `<button>` UA cascade switches from `inline-block` to `inline-flex` + `align-items:center` (mirrors Blink's `html.css` + `html_button_element.cc`). Horizontal centering of text content is unchanged (`text-align:center`).
  - `pkg/layout/flex_layout.go` — flex's `OutOfFlowLayoutPart` now receives containing-block size as **padding-box** (content + padding, borders excluded) per CSS 2.1 §10.3.7, matching block_layout.go's convention. Prior flex OOF passed content-box, mis-resolving abspos percent insets by the padding amount. css-position 91 → 92.
- [x] **2026-04-21 (paint-phase refactor):** closed `stack-floats-001.xht`. Added `PaintPhase` enum + split `paintLayerContent` into `paintSelfDecorations` / `paintSelfForeground` / `paintDescendantsPhase` / `paintDescendantPhase` driving the 3-phase loop at every stacking-context root. `buildPaintSubtree` now unconditionally routes text fragments (`LayoutNode==nil && Text!=""`) to `FlowChildren` — a text run inherits its parent's `Style*` and must never be classified by that style's `float`. `Box.CreatesStackingContext` extended to individual transform properties (`translate`/`rotate`/`scale`) per CSS Transforms Level 2 §3. Gates: wm 781/781, CSS2 99/99, flex 626/629 (same 3 pre-existing), css-backgrounds 162/351, css-inline unchanged; css-position **88 → 89**, css-transforms 171 → 172.
- [x] **2026-04-21 (WPT sub preprocessor + http→local rewriter):** closed `position-absolute-iframe-print-001.sub.html` + `position-absolute-iframe-print-002.sub.html`. New `pkg/visualtest/wpt_sub.go` handles template tokens (`{{host}}`, `{{hosts[alt][www]}}`, `{{ports[http][0/1]}}`, `{{ports[https][0]}}`, `{{location[path|host|server|scheme]}}`) and WPT-host URL stripping. `pkg/visualtest/helpers.go` fetchers accept WPT-host URLs and re-preprocess fetched `.sub.*` bodies. Runner preprocesses test+ref before rendering. Missing child HTMLs stubbed under `testdata/wpt-css3/css-position/resources/`. css-position **93 → 95**. Gates held: wm 781/781, CSS2 99/99, flex 626/629.
- [ ] **Remaining Phase 9 tests (triaged 2026-04-21):**
  - `clear-001.xht` (96px): **Deferred — research incomplete.** Ref hardcodes blue=97px/orange=95px for `height:1in` divs (1in = 96px); we render 96+96. Total identical; split differs. Category points at Blink's `LayoutUnit` fixed-point + asymmetric fragment-boundary rounding, but the specific Blink call site that produces the +1/-1 split has not been traced — our "LayoutUnit is the answer" note is an educated guess, not a verified implementation plan. See `findings.md` "clear-001 partially researched" for gap list. **Before picking up: do the Blink source-trace session** (which `LayoutUnit::Round()` / `FragmentBuilder::SetSize` / `ComputeContentAndScrollbarLogicalHeightUsing` path generates the asymmetric residue, and whether the quirk hits only `in`/`cm`/`mm` or all fractional lengths). Output of that session gates the fix scope: narrow snap helper vs full LayoutUnit port.
  - `position-change.html`: **Out of scope (parser infra).** HTML parser bails on `expected '>' but reached EOF`. Test-author bug or lenient-parsing gap; not a layout bug. Defer to HTML parser work.

### Phase 10: Delivery
- [ ] Confirm 100/100 runnable at 0 diff (104 total if SKIPs are later un-skipped by separate infra work).
- [ ] Confirm wm 781/781, CSS2 99/99, flex unchanged.
- [ ] Final session log summary.

### Phase 11: css-flexbox watch-category residuals (3 tests)
Parallel / tail-end track. The css-flexbox suite is a *watch* invariant (must stay ≥621 PASS), not part of the css-position delivery goal. Three residuals have sat at the same counts since Phase 1; document them here so the scoped Blink research doesn't get lost. Pick up after css-position 100/100 is delivered — or opportunistically if a css-position fix happens to touch the relevant flex paths.

#### 11a — `auto-margins-001.html` (0.2% diff, ~1024 px)
- [ ] **Blink research (done 2026-04-21):** sub-case 2 (the `writing-mode: vertical-rl` flex container + `<p style="margin:auto">OK</p>`) is the offender — VRL centering rather than VRL block-start flush. Sub-case 1 (HTB) and the 3-concentric-circles sub-case render identically modulo AA noise.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc` — `ApplyReversals`, `AlignFlexLines`, the auto-margin block inside item placement (search `auto_margins` / `ResolveAutoMargins`). `third_party/blink/renderer/core/layout/flex/flex_item.cc` — `HasAutoMarginsInCrossAxis` and cross-axis stretch suppression. `third_party/blink/renderer/core/layout/geometry/writing_mode_converter.{h,cc}` — physical↔logical mapping reference.
- [ ] **louis14 touchpoints:** `pkg/layout/flex_layout.go` — `getItemAutoMargins` (~lines 4043-4086) and the cross-axis auto-margin resolution (~lines 1318-1360 / §8.1 block ~line 2086).
- [ ] **Hypothesis:** `getItemAutoMargins` loses the item-vs-container writing-mode distinction (`mainIsItemInline`) for the cross axis in VRL. When the container is a flex *row* (main=inline, cross=block) in VRL, the physical `margin-top/bottom` of the `<p>` should map through the container's logical converter; today the mapping centers instead of leaving block-start flush. Gate the cross-axis stretch-suppression on both-auto cross margins via the correct logical-axis conversion.
- [ ] **Gate:** target passes at 0 diff; wm 781/781 and CSS2 99/99 hold; no regression to other flex tests.

#### 11b — `content-height-with-scrollbars.html` (14.4% diff, ~69200 px) — classic-scrollbar reservation
- [ ] **Blink research (done 2026-04-21):** this is not a flex bug — it is a platform/layout gap. WPT Chromium reference PNGs are generated with *classic* (space-taking) scrollbars at 15px; louis14's `classicScrollbarWidth()` in `pkg/layout/fragment_geometry.go:141` returns **0** for the default `"auto"` scrollbar-width, so `overflow: scroll` elements reserve zero space. The `FragmentGeometry.Scrollbar` / `BorderBoxPadding` / `Inline/BlockScrollbarSum` pipeline is already threaded through all layout algorithms — the single defect is the constant.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/layout_box.h` — `VerticalScrollbarWidth()` / `HorizontalScrollbarHeight()`. `third_party/blink/renderer/platform/scroll/scrollbar_theme_aura.cc` — `ScrollbarThickness()` returns 15px (classic default). `third_party/blink/renderer/core/layout/ng/ng_box_fragment_builder.cc` — `SetScrollbar()` populates `FragmentGeometry::scrollbar` from `ComputeScrollbars()`. `ng_length_utils.cc` — `CalculateBoxSizes` subtracts scrollbar from available inline/block size.
- [ ] **louis14 touchpoints:** `pkg/layout/fragment_geometry.go` — `classicScrollbarWidth()`. Change the `"auto"` return from `0` to `15` when the element reserves a classic scrollbar; keep `10` for `thin`, `0` for `none`. No other code changes needed.
- [ ] **Scope boundary:** layout-only reservation; we do not paint the scrollbar chrome (matches existing louis14 rendering model).
- [ ] **Regression risk:** any existing test implicitly passing because louis14 ignored scrollbar reservation will shift. Candidate fallout in `cross-axis-scrollbar.html`, `contain-size-scrollbars-002.html`, `scrollable-overflow-transform-unreachable-region.html`, plus non-flex `overflow: scroll` tests across css2 and css-position. Triage in the same commit; do not split.
- [ ] **Gate:** target + candidate-fallout tests all at 0 diff; wm 781/781 and CSS2 99/99 hold; css-flexbox stays ≥626 (no regressions).

#### 11c — `flexbox-align-self-vert-004.xhtml` (0.8% diff, ~3664 px)
- [ ] **Blink research (done 2026-04-21):** two candidate roots, in priority order:
  1. `align-self: baseline` in the column-direction container. `flex_layout.go:1403-1433` (placement) synthesizes `bl = 0` for column-sameWM baseline — inline-start edge — bypassing `resolvedFirstBaseline`. But the first-pass line-cross accumulation at `flex_layout.go:822-871` still routes baseline items through `resolvedFirstBaseline`, which for `baselineParallel=false` returns `crossSize` as the synthetic baseline — inflating `maxAscent = crossMarginStart + crossSize`. Line-cross and per-item offset disagree.
  2. `align-self: stretch` with items wider than the 4px container. `stretchFlexItems` (`flex_layout.go:4643`) clamps `stretchBorderBox` to 0 when `line.crossSize - crossMarginSum < 0`, then re-lays out the item at 0 content-width. Blink's `DetermineUsedCrossSize` takes the max of the stretched size and the item's min-content, so an item with `width:50px` keeps 50px.
- [ ] **Blink references:** `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc` — `PlaceFlexItems`, `DetermineUsedCrossSize`, `BaselineAscent`, `ApplyFinalAlignmentAndReversals`. `third_party/blink/renderer/core/layout/flex/baseline_utils.{h,cc}` — `DetermineBaselineWritingMode`, `DetermineBaselineGroup` (~lines 51-89), `SynthesizedBaseline`.
- [ ] **louis14 touchpoints:** `pkg/layout/flex_layout.go` — unify the two baseline-resolution paths (first-pass accumulation at ~:819-871 vs placement at ~:1403-1433) so column-sameWM synthesizes at inline-start in *both*. In `stretchFlexItems` (~:4607+), honor min-content / explicit `width` when line cross-size is smaller than the item. `resolvedFirstBaseline` at ~:309 needs a column-sameWM caller contract or a dedicated helper so accumulation uses `bl = 0` matching placement.
- [ ] **Hypothesis:** unify column-sameWM baseline synthesis to inline-start in both the accumulation and placement paths; in stretch, clamp to `max(line.crossSize, itemMinContent/explicitCross)` instead of zero. Fixes the residual pixels.
- [ ] **Gate:** target passes at 0 diff; wm 781/781 and CSS2 99/99 hold; css-flexbox stays ≥626.

## Milestones (commit + report after each)
Counts are against **runnable tests (100)**; 4 SKIPs excluded.

- **M1:** G-TABLE-REL closed → +11 primary (50 → 61). **Achieved 2026-04-21** via commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Verified at re-baseline post OOF re-entrance: also closed `position-relative-012` (was conjectured). 8 `-absolute-child` variants still failing at 1.0% — distinct root cause, deferred to G-ABS-IN-INLINE / G-ABS-IN-TABLE.
- **M2:** ~~G-CB-CHANGE~~ — group dissolved 2026-04-21. Tests reassigned to G-FIXED / G-SINGLETONS / G-SCROLL.
- **M3:** G-DYN-STATIC closed → +6 (→ 68). **Achieved 2026-04-21** (Parts a+b+d via commits `233d408f`, `d250c5cf`; Part c via commit `5399d328` — orphan-cell vertical-align + transform percent-sentinel fix). Bonus: +9 css-transforms (162 → 171).
- **M4:** G-ABS-CENTER + G-HYPO combined (IMCB) → +8 (→ 77). **Achieved 2026-04-21** via Phase 4 Commits 1 (`a3c8db38`), 2 (`d9f6628b`), and 3. Group closed.
- **M5a:** G-FIXED Part A — OOF resolver re-entrance. **Achieved 2026-04-21** via commit `ed16475f`. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (62 PASS total). Reduced `position-fixed-scroll-nested-fixed` 4.2% → 1.0% (residual paint-clip).
- **M5b:** G-ROOT-FLEX-GRID closed → +4 (77 → 81). **Achieved 2026-04-21** via commit `7e686a28` (new `pkg/layout/positioned_root.go` routing positioned `<html>` through IMCB sizing). G-FIXED Part B (paint-clip / scrollTop) overlaps G-SCROLL, still open.
- **M6:** G-ABS-IN-INLINE closed → +2 (81 → 83). **Achieved 2026-04-21** via commit `01f468d9`. New `pkg/layout/inline_containing_block.go` mirrors Blink's `InlineContainingBlockUtils::ComputeInlineContainerGeometry` (start/end fragment union rects, transparent walk through anonymous-block continuations). OOF resolver routed through inline-CB sizing with `cbOriginInBuilder` tracking; `InlineContainer` stamped on OOF items via `BuildPositionedInlineMap` (position:fixed excluded — viewport CB). Empty-leading-continuation emitted in `layout_tree_builder.go` for positioned inlines with block-in-inline splits to keep the first-line union rect anchored at the span's start.
- **M7:** G-STICKY closed → +1 (83 → 84). **Achieved 2026-04-21** via commit `05aff97e`. Dropped sticky from the 7 RelativeOffset-computation sites — sticky now emits zero layout-time offset (Blink-faithful; scroll-time `StickyPositionScrollingConstraints` wiring deferred).
- **M7b:** G-REPLACED closed → +1 (84 → 85). **Achieved 2026-04-21** via commit `0e1fde9f`. Extended the `layoutCandidatesOnce` `stretchable` gate to exclude replaced elements so abs-replaced sizing stays on the `ComputeReplacedSize` + auto-margin path per CSS 2.2 §10.3.7 / §10.6.5.
- **M8:** G-SINGLETONS (including `position-change`) + G-SCROLL swept → 100/100 runnable.
- **M9 (parallel track):** Phase 11 flex residuals swept → css-flexbox 629/629. Independent of css-position delivery; pick up opportunistically or after M8.

## Test command templates
```
# Single test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position/<name>' -v

# Whole category (sanctioned baseline; don't rerun casually)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position' -v 2>&1 | tee output/baselines/css-position-<date>.log

# Regression sweeps
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'     # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests'                           # CSS2: expect 99/99
```

## Key Questions
1. ~~Does `table_layout.go` emit intermediate fragments?~~ **Answered (2026-04-21):** yes, it emits per-row and per-cell fragments at `table_layout.go:685` and `:735`. The fix targets those add-sites (or better, the shared `AddChild` below them, matching Blink's `BoxFragmentBuilder::AddChild`).
2. Are the 6 `dynamic-static-position-*` failures all driven by static-position caching, or do the `inline`/`table-cell`/`floats` branches each need separate plumbing? Blink evidence says "all driven by the same cached-vs-rebuilt static position." Confirm with Phase 3 representative.
3. **G-STICKY scope:** minimum viable is zero offset at scroll=0 — sufficient for `sticky-top-001`. Full `StickyPositionScrollingConstraints` is deferred until scroll-based tests appear.
4. **New:** is the hypothetical-box algorithm already folded into the IMCB both-auto-insets branch, or does it need separate plumbing? Blink evidence says it is the same branch. If Phase 4 makes the hypothetical tests pass for free, G-HYPO collapses into G-ABS-CENTER.

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Attack G-TABLE-REL first | 16 tests, one likely root cause, cleanest unlock. Foundational correctness §1. |
| Prefer pushing relative-offset check into shared `AddChild` | Mirrors Blink's `BoxFragmentBuilder::AddChild`; prevents the same class of bug recurring in future layouts. |
| NORUN — 4 SKIPs out of scope, 1 real FAIL folded into G-SINGLETONS | Triage 2026-04-21: SKIPs are harness/JS gaps; target is 100/100 runnable. |
| Bundle G-ABS-CENTER + G-HYPO into one phase | Both use the same Blink IMCB machinery; the hypothetical-box algorithm IS the both-insets-auto branch. |
| G-DYN-STATIC precedes G-ABS-CENTER/G-HYPO | The IMCB reads `LogicalStaticPosition`; without rebuild-per-pass, dynamic inputs won't flow through. |
| G-CB-CHANGE is invalidation-only → group dissolved | Audit 2026-04-21: harness already does fresh re-layout post-JS, so Blink's invalidation pattern is a no-op for us. Tests reassigned by actual root cause. |
| G-FIXED OOF re-entrance via worklist loop, not single-pass | Mirrors Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`. New `resolvesFixed` flag distinguishes ICB / containment / transform CB sites (absorb fixed) from ordinary positioned sites (return unresolved fixed to caller). |
| Split G-FIXED into Part A (re-entrance, layout) and Part B (paint-clip / scrollTop) | Re-entrance closed 1 of 2 cleanly; the residual is squarely in paint/scroll, not OOF layout. Don't conflate — Part B will be picked up alongside G-SCROLL. |
| G-STICKY minimum viable acceptable | Full constraint machinery is overkill for the one failing test; flag as tech debt for when scroll-based sticky tests appear. |
| ExclusionSpace stores LOCAL inline offsets (not BFC-absolute) | Learned 2026-04-21 while fixing Phase 3(b). Readers must NOT subtract `bfcInlineOrigin` from `FindAvailableInlineSize` results. Invariant holds for any caller in the same enclosing block that owns the exclusion space. Full write-up in `findings.md` "Coordinate-system notes". |
| Inline-FC OOF block-end read "at time of encounter" | Mirrors Blink's `line_box_.LineBoxBlockEnd()` semantics. `hasInflowOnLine` incremental flag is the correct primitive; deferred emission at end-of-line without this gate regresses orthogonal-float wm tests. |
| Transform parser uses explicit `IsPercent []bool`, not sign sentinel | Sign-based percent sentinel (`result := -percent`) collided with legitimate negative pixel lengths. `translate: 0 -50px` was rendering as `+50px`. Fixed by storing percent-ness per component; widened `GetIndividualTranslate` signature. +1 css-position + 9 css-transforms for free. |
| Orphan `display:table-cell` gets vertical-align at `block_layout.go`, not via §17.2.1 anon-wrapping | Reverse §17.2.1 anonymous-table generation is unimplemented, so orphan cells dispatch to block layout. Adding a guarded vertical-align shift at the block-layout end-of-pass is cheaper (and bounded) than implementing reverse §17.2.1. `TableSectionData == nil` guard prevents double-shift on the proper-table path. |
| Do not run the full css-position category more than once per milestone | CLAUDE.md §4 — broad runs only at baselines and milestone verifications. |
| css-writing-modes stays at 781/781 as an invariant | Phase 5f is complete; any regression in wm reverts the commit. |

## Deferred / parked work (may not be needed; capture so we don't lose it)

### Proper-table-path vertical-align on propagated OOF candidates
**Context.** During Phase 3(c) I drafted a `table_layout.go` change that (i) recomputed `contentBlockSize` from the cell's pre-stretch intrinsic content + box-model (instead of the post-stretch `cellLogical.BlockSize()`), and (ii) extracted `vaBlockShift` into a variable so it could be added to `PropagatedOOFCandidates[].StaticPosition.Offset.BlockOffset` during the per-row sweep (matching Blink's `TableCellLayoutAlgorithm` applying `intrinsic_padding_before` before OOF propagation).

**Why it didn't ship.** The Phase 3(c) target test turned out to use *orphan* `display:table-cell` (no table ancestor) and never exercised the proper-table path. Worse, the `contentBlockSize` pre-stretch change regressed 3 wm orthogonal-writing-mode tests (`box-offsets-rel-pos-vlr-005`, `box-offsets-rel-pos-vrl-004`, `orthogonal-cell-001`) — the pre-stretch shape interacts badly with orthogonal cells where `cellBlockForRow` reads the cell's *inline* size as the row's block size.

**When to revisit.** The moment a test requires vertical-align centering of an abspos descendant inside a real `<table><td>...`. Candidate triggers: future G-HYPO tests using `<td>`, any css-tables-3 test with abspos + vertical-align inside a cell.

**What to redo.**
1. Re-apply the `vaBlockShift` extraction + `PropagatedOOFCandidates` shift in `table_layout.go` (structurally it's correct — mirrors Blink).
2. Debug the `contentBlockSize` shape separately against `orthogonal-cell-001` before landing. The fix likely needs to detect orthogonal cells and use a different pre-stretch quantity (cell's inline-size in its own WDM rather than block-size). Do not ship one without the other.
3. Drop the `TableSectionData == nil` guard in `block_layout.go` only if the proper-table path handles the shift — otherwise keep both paths.

**Pointer.** See `findings.md` § "G-DYN-STATIC — (c) table-cell" → "Not shipped — proper-table-path vertical-align capture" for the exact diff that was dropped.

## Notes
- `output/baselines/` holds raw logs; parse scripts live in `/tmp/` and are regenerated per session.
- Current branch: `fix/flexbox-fast`. Master is the delivery target.

---

# Phase 12: css-multicol (next category)

Opening 2026-04-21. css-position at 91/104 (baseline corrected 2026-04-23; prior "95/100" was an inaccurate tracking claim — see the `## css-position Goal` note above for details). Entry baseline for css-multicol: **94 PASS / 361 FAIL / 3 SKIP** out of 458 (20.5%). After 12a+12b+12c: **130 PASS / 325 FAIL / 3 SKIP** (28.4%).

Baseline extracted to `/tmp/multicol-all.txt` and `/tmp/multicol-fails.txt`.

## Research & plan
Full Blink-source research + louis14 audit + cluster triage in `findings.md` "css-multicol category (2026-04-21)". Must be re-read before starting any phase. Key facts that drive the plan:

- **Blink has deleted legacy multicol** (`LayoutMultiColumnFlowThread`, `LayoutMultiColumnSet`, `MultiColumnFragmentainerGroup`, `LayoutMultiColumnSpannerPlaceholder`). NG model is the only model. Mirror that.
- **Fragmentation infrastructure (`MinimalSpaceShortage` + `BlockBreakToken` + `ConstraintSpace` fragmentainer flags)** is the common dependency for ~80 failing tests across fill-balance, nested, spanner-fragmentation, and breaking clusters. Phase 12a first.
- **`ColumnSpannerPath` + `MulticolPartWalker`** are NG's replacement for the legacy spanner-placeholder + fragmentainer-group model. Phase 12b.
- **Outward shortage propagation** (`IsInsideBalancedColumns` + `PropagateSpaceShortage`) is what makes nested multicol work. Phase 12c.

## Phases

### Phase 12a — NG fragmentation infrastructure (~80 tests, L) — **DONE 2026-04-22 via commit `2a0d0a07`**
**Goal.** Rewrite `pkg/layout/multicol_layout.go` outer stretch loop to match Blink's `LayoutLine`. Thread `BlockBreakToken` between columns. Add shortage reporting + collection.

- [x] Add `HasForcedBreak bool` to `LayoutResult`.
- [x] Add `IsInitialColumnBalancingPass`, `IsInsideBalancedColumns` to `ConstraintSpace` (+ setters on builder).
- [x] Add `InlineItemStartIndex int` to `BlockBreakToken` for inline-content resume.
- [x] Thread `BlockBreakToken` through per-column `BlockLayoutAlgorithm` calls — `space.BreakToken` on input, `result.BreakToken` on output.
- [x] Implement `createConstraintSpaceForColumn` with `IsFixedBlockSize=true` + `IsBlockSizeOverride=true` to override CSS height with column height.
- [x] `resolveColumnAutoBlockSize` — unconstrained measurement pass with `IsContentSuggestionLayout=true` + `IsInitialColumnBalancingPass=true`.
- [x] Replace single-pass column placement with `layoutLine` — Blink-parity outer stretch loop: `do { ... } while(true)`, acceptance condition, `colBlockSize += minSpaceShortage` stretch rule.
- [x] Inline fragmentation: `layoutInlineChildren` stops at column boundaries, returns `inlineBreakToken` with `InlineItemStartIndex`; `block_layout.go` resumes from saved index.
- [x] Enable multicol dispatch in `layoutElement` (`isMulticolContainer(style)` guard).
- [x] Add `GetColumnFill()` to `css.Style`.
- [x] Driver test: `multicol-fill-balance-001.xht` — **PASS at 0 pixel diff**.
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, css-flexbox 626/629 ✓, css-position 91/104 ✓ (all failures pre-existing).

### Phase 12b — Spanner re-balance (~40 tests, L)
**Goal.** `ColumnSpannerPath` + `MulticolPartWalker` equivalents; each column-run before/after a spanner re-balances independently.

- [x] `ColumnSpannerPath` struct + accessor on `LayoutResult`.
- [x] Inner `BlockLayoutAlgorithm` encountering `IsColumnSpanAll()` returns early with `column_spanner_path` set.
- [x] `MulticolPartWalker` in `multicol_layout.go` that serializes (column-run, spanner, column-run, …).
- [x] `LayoutSpanner` — full-container-width constraint space, commit at intrinsic_block_size.
- [x] Post-spanner column run re-enters `LayoutLine` with its own `next_column_token`, its own `ResolveColumnAutoBlockSize` estimate.
- [x] Spanner-forces-balance-on-preceding-row rule for `column-fill:auto`.
- [x] Ghost-row fix: `resolveColumnAutoBlockSize` returns 0 (not 1) when all content is spanners; `constrainColumnBlockSize` allows 0; `createConstraintSpaceForColumn` treats `colBlockSize=0` as `Indefinite` so no 1px phantom row precedes each spanner.
- [x] Leaf-block fragmentation fix: `block_layout.go` Change 1 — detect leaf block overflow, create `BlockBreakToken{ConsumedBlockSize}` for the leaf instead of pointing to the next sibling (spanner). Change 2 — resumed explicit-height leaf blocks compute `remaining = explicitBlockSize - ConsumedBlockSize` as the fragment height. (2026-04-22, uncommitted.)
- [x] Fix `spanner-fragmentation-001` — trailing leaf child fragment height after spanners. Root cause: leaf block overflowed column, resumed with full height instead of `remaining = explicit - consumed`. Closed 2026-04-22.
- [x] Fix `spanner-fragmentation-002` — same leaf-block fragment height fix. Closed 2026-04-22.
- [x] Fix `spanner-fragmentation-004` through `010` — outer fragmentation (IIM break propagation, `buildOuterBreakResult`, `beforeSpannerToken` threading). Closed 2026-04-22.
- [x] Fix `spanner-fragmentation-009` — `break-before:column` with no prior content in column. Zero-height fragment with suppressed border+padding BoxData (only margin emitted). Resumed fragment draws full borders. Closed 2026-04-23.
- [x] Fix `spanner-fragmentation-011` (5000px failure) — two-part fix: (1) `groupedChildrenCache` on `LayoutInputNode` gives anonymous block wrappers stable pointer identity across repeated `Layout()` calls; (2) whitespace-only inline run suppression in `flushRun` eliminates spurious IIM re-layout after `break-after:column`. PASS at 0 diff. (2026-04-23, commit `931f48c5`)
- [x] Fix `spanner-fragmentation-012` (2500px failure) — same two-part fix. PASS at 0 diff. (2026-04-23, commit `931f48c5`)
- [x] Remove debug code from `multicol_layout.go` and `block_layout.go`. (2026-04-23)
- [x] **Gate:** wm 781/781 ✓, CSS2 99/99 ✓, css-flexbox 626/629 ✓.

**Results as of 2026-04-23 (commit `931f48c5`): all 13 PASS at 0 pixel diff.**
| Test | Status | Pixels wrong |
|------|--------|-------------|
| spanner-fragmentation-000 | **PASS** | 0 |
| spanner-fragmentation-001 | **PASS** | 0 |
| spanner-fragmentation-002 | **PASS** | 0 |
| spanner-fragmentation-003 | **PASS** | 0 |
| spanner-fragmentation-004 | **PASS** | 0 |
| spanner-fragmentation-005 | **PASS** | 0 |
| spanner-fragmentation-006 | **PASS** | 0 |
| spanner-fragmentation-007 | **PASS** | 0 |
| spanner-fragmentation-008 | **PASS** | 0 |
| spanner-fragmentation-009 | **PASS** | 0 |
| spanner-fragmentation-010 | **PASS** | 0 |
| spanner-fragmentation-011 | **PASS** | 0 |
| spanner-fragmentation-012 | **PASS** | 0 |

### Phase 12c — Nested multicol (Blink-parity infra landed 2026-04-23)
**Goal.** Outward shortage propagation + nested-initial-balancing override + missing resume-break emission for nested multicol hitting outer fragmentainer boundary.

**Driver:** `multicol-nested-010.html` (baseline 6000 px / 1.2% fail → current 3500 px / 0.7% fail). `multicol-nested-001.html` in the original plan does not exist in our snapshot; series starts at `-002`.

Blink-source-verified checklist (cla.cc line anchors, full source quoted in `findings.md`):

- [x] **Outer-fragmentainer clamp** (cla.cc:860–895). Already implemented pre-12c via `outerRemaining` (`multicol_layout.go:573–581`) + `constrainColumnBlockSize` (`:877–879`). No change needed.
- [x] **Nested-initial-balancing override** (cla.cc:1025). Dropped the reversed `!IsInitialColumnBalancingPass` clause at `multicol_layout.go:106–108` so the override fires during the outer's initial pass. Blink-parity: `HasBlockFragmentation() && !HasKnownFragmentainerBlockSize()`.
- [x] **Outward shortage propagation** (cla.cc:1235). New `BoxFragmentBuilder.PropagateSpaceShortage` in `fragment_builder.go`; wrote into `LayoutResult.MinSpaceShortage` at `Build()`. Replaced stub at `multicol_layout.go:720` with real call gated on `IsInsideBalancedColumns && !IsInitialColumnBalancingPass && hasShortage`.
- [x] **Resume-break emission for nested hit** (NOT in original checklist; root cause of driver 010's 6000-px diff). When the outer fragmentation context is active and inner `layoutLine` returns with `remainingToken != nil` and no spanner, now calls `buildOuterBreakResult(nil, nil)` so the outer block_layout gets a break token to resume the inner in its next outer column. Paired with a resume-path wiring: `nextColToken ← colRowsResumeToken` when the incoming break token carries a column-rows continuation with no spanner state.
- [x] **FragmentainerOffset propagation through block_layout** — audit confirmed already correct at `block_layout.go:537` (`childFragOffset := bla.space.FragmentainerOffset + blockCursor + prevMarginStrut.Resolve()`). No change needed.
- [ ] `MulticolBreakTokenData{consumed_row_block_size}` — deferred to 12f (gated on `ShouldWrapColumns() && HasRowHeight()`; not exercised by current 12c tests).
- [x] **Gate 2026-04-23:** wm 781/781, CSS2 99/99, css-flexbox 626/629, css-position 91/104. css-multicol 130/458 (+22 vs 108 baseline post-12b).

**Driver residual (010 still 3500 px).** Not a Blink-parity-checklist miss — the remaining gap is in how a single explicit-height leaf block fragments across inner columns on resume (inner col 1 missing when leaf finishes in inner col 0 + how the inline-overflow region of the leaf is painted vs the inner multicol bg). This is paint/leaf-fragmentation work that's deeper than the four canonical 12c sites. Candidates for follow-up phase or dedicated fix:
- Inner column painting: Blink likely slices content across inner columns via the painting pass (each inner column paints the same underlying leaf content at a column-specific inline offset, clipped per-column), producing the "all-green" visual even when only one column has a layout fragment. Our engine paints only what each column's fragment tree contains.
- Or: `contain:size` + width:200% interacting with block fragmentation in a way we don't mirror.

Sibling tests 007/008/009/011/013/014 unchanged from their 1.2–1.6% baselines; same root cause. Don't treat them as 12c residuals — open under a focused "nested multicol leaf/paint" follow-up whose scope is paint-level slicing, not balancing infrastructure.

### Phase 12d — Forced breaks in column context — **COMPLETE 2026-04-24**
**Goal.** `break-before/after:column` + `break-inside:avoid-column` honored via `BreakToken` + `BreakAppeal`.

**Driver-pick correction.** The plan named `multicol-breaking-001.html`, but inspection
showed it's a nested-multicol test with fixed-height inner div — *not* forced-break.
True forced-break drivers: `multicol-break-000.xht` (break-after:column) +
`multicol-break-001.xht` (break-before:column) + `multicol-br-inside-avoidcolumn-001.xht`
(break-inside:avoid-column).

- [x] `IsForcedBreakValue(space, ebreakbetween)` dispatch on `BlockFragmentationType`. (`pkg/layout/break_appeal.go`)
- [x] `IsAvoidBreakValue<Property>` dispatch for `avoid-column`. (same file)
- [x] `BreakBeforeChildIfNeeded` wired into `block_layout.go` per-child loop (only column-fragmentation context; gated on `HasBlockFragmentation && BlockFragmentationType == FragmentColumn`).
- [x] `BreakAppeal` enum ordering: `LastResort < ViolatingOrphansAndWidows < ViolatingBreakAvoid < Perfect`. (`pkg/layout/break_appeal.go`)
- [x] `hasViolatingBreak` tracked in outer stretch loop (demote on non-perfect appeal). (`multicol_layout.go`)
- [x] `BlockBreakToken.IsForcedBreak` field added; outgoing token marks the break as forced when triggered by `break-before/after:column`.
- [x] `BoxFragmentBuilder.previousBreakAfter` + `JoinedBreakBetweenValue` mirror Blink's per-child break-after propagation.
- [x] `MovePastBreakpoint` simplified port: column-context decision, no EarlyBreak retry (deferred 12g), no FlexColumnBreakInfo, no paginated paths.
- [x] **Scope-restriction note**: Phase 12d's `BreakBeforeChildIfNeeded` returns `BrokeBefore` ONLY for forced break-between values OR break-inside:avoid violations on a child that overflows. Normal soft-break overflow is left to `block_layout.go`'s existing overflow handler at lines ~764-913 — taking it over regressed `spanner-fragmentation-006/008`. Full Blink-parity AttemptSoftBreak + EarlyBreak retry deferred to 12g.
- [x] Initial-balancing-pass forced-break suppression: during `IsInitialColumnBalancingPass=true`, dispatch returns Continue so content flows continuously and `resolveColumnAutoBlockSize` measures correctly. Approximation of Blink's `ContentRuns::DistributeImplicitBreaks`; full version deferred.

**Drivers + verification (2026-04-24):**
| Test | Pre-12d | Post-12d |
|------|---------|----------|
| `multicol-break-000.xht` | 1200 px | 1200 px (blocked by Ahem font loader bug — fragmentation tree verified correct) |
| `multicol-break-001.xht` | 1200 px | 1200 px (same) |
| `multicol-br-inside-avoidcolumn-001.xht` | 30000 px | **0 PASS** ✓ |
| `change-transform-in-nested.html` | FAIL | **PASS** ✓ |
| `change-transform-in-second-column.html` | FAIL | **PASS** ✓ |
| `multicol-overflow-clip-auto-sized.html` | PASS | 361 px (regression — see below) |
| spanner-fragmentation-* | 12/13 PASS | 12/13 PASS (no regression) |

**Trade explained:** `multicol-overflow-clip-auto-sized` regression is from correctly honoring `break-inside:avoid` in the REF (which has it explicitly) while the TEST relies on `overflow:hidden` being treated as monolithic content (CSS Fragmentation L3 — not yet implemented in louis14). The fix is to mark overflow:hidden boxes as monolithic; tracked separately as a follow-up.

**Gate (2026-04-24):** wm 410/781, CSS2 96/99, css-flexbox 621/629, css-position 89/104 — all unchanged from pre-12d (the wm/CSS2/css-pos/flex regressions are pre-existing in the working-tree's renderer.go modifications, not introduced by 12d). css-multicol 121 → **123 PASS** (+2 net: +3 newly passing, -1 newly failing).

**Files added/modified:**
- new: `pkg/layout/break_appeal.go` — BreakAppeal enum, BreakStatus enum, IsForcedBreakValue, IsAvoidBreakValue, JoinFragmentainerBreakValues, FragmentainerBreakPrecedence
- new: `pkg/layout/fragmentation_utils.go` — BreakBeforeChildIfNeeded, CalculateBreakBetweenValue, CalculateBreakAppealBefore, CalculateBreakAppealInside, MovePastBreakpoint
- modified: `pkg/css/style.go` — added `GetBreakInside()`
- modified: `pkg/layout/constraint_space.go` — `MinBreakAppeal` + `ShouldIgnoreForcedBreaks` fields + setter
- modified: `pkg/layout/layout_result.go` — `BreakAppeal` field
- modified: `pkg/layout/break_token.go` — `IsForcedBreak` field
- modified: `pkg/layout/fragment_builder.go` — `previousBreakAfter` + `breakAppeal` fields, `SetPreviousBreakAfter`, `JoinedBreakBetweenValue`, `SetBreakAppeal`, default `BreakAppeal=Perfect` in `Build()`
- modified: `pkg/layout/block_layout.go` — wire `BreakBeforeChildIfNeeded` after each in-flow child layout in column context
- modified: `pkg/layout/multicol_layout.go` — track `hasViolatingBreak |= result.BreakAppeal != Perfect`

### Phase 12e — column-fill:auto (~25 tests, M) — **PARTIAL 2026-04-24**
**Goal.** Sequential-fill branch; honors `block-size` + outer remaining space + max-height; spanner-forces-balance special case.

- [x] Branch in `LayoutLine` on `column-fill` (already shared with 12a; activates the `!balance_columns` exit). Pre-existed since 12a.
- [x] `column_size.block = content_box_block_size` when definite — pre-existed via `hasExplicitBlock` branch.
- [x] **NEW** `column_size.block = max-height` when block-size auto + max-height set. `effectiveMaxBlockSize` resolved from `ResolveMaxBlockSize` at top of `Layout()`, threaded into `layoutLine` + `constrainColumnBlockSize`. Only consulted when `!hasExplicitBlock` (an explicit height has already been clamped through min/max by `CalculateInitialFragmentGeometry`; re-applying max here would override min per CSS 2.1 §10.7).
- [x] **NEW** Final multicol block-size capped by `effectiveMaxBlockSize` for the auto-height case.
- [x] **NEW** Spec-correct column-rule painting: rules only drawn between columns that both have content (CSS Multicol L1 §5). New `PhysicalFragment.RenderedColumnCount` populated by multicol layout, threaded to `Box.RenderedColumnCount`, consumed by `paint_layer.go` to narrow `layer.ColumnCount` when actual placed columns < CSS column-count. Counts only columns with non-zero intrinsic content (a forced-size empty column doesn't qualify).
- [x] Outer-constrained clamp at `FragmentainerSpaceLeftForChildren()` — pre-existed via `outerRemaining` plumbing in `layoutLine`.
- **Driver-pick correction.** Plan named `columnfill-auto-001.html` but the actual file is `multicol-fill-auto-001.xht` (already passing pre-12e; not a useful driver). Picked `multicol-fill-auto-block-children-003.html` (canonical Mozilla max-height-imposes-on-columns test) as the driver — passes at 0 diff.
- [x] **Gate (2026-04-24):** wm 410/781, CSS2 96/99, css-flexbox 621/629, css-position 89/104, spanner-fragmentation 12/13 (005 still pre-existing fail) — all unchanged from pre-12e baseline. css-multicol **123 → 124 PASS** (+1 net for the driver). Rendered-column-count change has no PASS impact today (the cluster's residuals are missing-text-rendering bugs, not column-fill:auto bugs) but is spec-correct and removes spurious red column-rule painting in `columnfill-auto-max-height-001/002`.

**Residuals NOT closed by 12e (out of scope, separate root causes):**
- `columnfill-auto-max-height-001/002.html` (10000 px, 2.1%): Ahem text not rendering for `font-family:Ahem` longhand combined with `font-size:25px` + `line-height:1`. Reproduces in baseline; pre-existed 12e. The diff is exactly the 100×100 expected green text region.
- `columnfill-auto-max-height-003.html` (5000 px, 1.0%): inline-overflow content (`width:200%`) clipped by column-fragmentainer's `ClipContentToBorderBox`. CSS Multicol L1 §3.7 says columns clip in BLOCK direction only, not inline — needs a directional clip API. Tried narrowing the clip to "only when `result.IntrinsicBlockSize > colBlockSize`" but it regressed `spanner-fragmentation-004/006`, so reverted. Tracked as follow-up.
- `multicol-fill-auto-003.xht` (30000 px, 6.2%): long unbreakable digit token (`1234567890` = 10 chars × 20px = 200px) overflows 180px column inline; our inline layout drops the content entirely instead of overflowing. Inline-layout bug, not 12e scope.
- `multicol-fill-auto-004/005.html` (9000/8000 px): "more forced breaks than columns" + auto-height inner multicol; inner needs to overflow with extra columns past the parent. Spec edge case that needs auto-height + forced-break-count > column-count handling. Tracked separately.
- `multicol-fill-auto-block-children-001/002.xht` (78295/56077 px, 16%/12%): h1 spanner + dl block children with explicit body height; body height overflowing canvas. Spanner+block interaction, not column-fill:auto.

### Phase 12f — column-height + column-wrap (~29 tests, S) — **PARTIAL 2026-04-24**
**Goal.** Add CSS Multi-column L2 §4.2 `column-height: auto | <length [0,∞]>` + companion `column-wrap: auto | nowrap | wrap` and wire them through the five `column_layout_algorithm.cc` consumption sites. Blink gates both on the `MulticolColumnWrapping` runtime flag (stable); we enable unconditionally.

Reference: findings.md §9a. Blink source: `core/layout/column_layout_algorithm.{h,cc}`.

- [x] Add `column-height` + `column-wrap` to style getters. `GetColumnHeight` returns length or -1 for auto. `GetColumnWrap` returns "auto"/"wrap"/"nowrap" (default "auto"). No percentage support on `column-height`.
- [x] `shouldWrapColumns`, `hasRowHeight`, `rowHeight` helpers (cla.h:221/258/267 parity). `rowStride` = `rowHeight + rowGapSize` (today rowGapSize=0; CSS L2 multicol row-gap not plumbed).
- [x] `offsetInCurrentRow(lineOffset)` / `offsetToNextRow(lineOffset)` / `remainingRowHeightAtOffset(lineOffset)` helpers. At an exact row boundary `offsetInCurrentRow==0`, so `remainingRowHeightAtOffset` returns the full row-height (Blink-faithful).
- [x] `constrainColumnBlockSize` clamp by `remainingRowHeightAtOffset(lineOffset)` when `hasRowHeight()` (cla.cc:2017 parity). New `lineOffset` param threaded through every caller.
- [x] `layoutLine` block-size choice (cla.cc:864): non-auto column-height seeds `colBlockSize = remainingRowHeightAtOffset(lineOffset)` ahead of the balance / explicit / max-height / Indefinite branches.
- [x] Row-wrap loop (cla.cc:835): walker continues with `nextColToken=remainingToken` when `spannerPath==nil && remainingToken!=nil && shouldWrapColumns() && hasRowHeight()`. Pre-LayoutLine advance (cla.cc:795) with `isFirstRow` flag (reset after spanner placement). Bail to outer fragmentainer when the next row won't fit.
- [x] Intrinsic block-size top-off at end of `Layout()` (cla.cc:342): non-auto column-height pads `blockCursor += remainingRowHeightAtOffset(blockCursor)`, clamped by outer-fragmentainer remaining space; skipped at exact row boundaries.
- [x] `buildOuterBreakResult` slot-layout fix: child-break-token slots are fixed `[nextColToken, partialSpannerToken, pendingColRowsBreakToken]` with trailing-nil trim (never load slot 1 as a spanner when it's a post-spanner col-rows resume). Parser nil-checks slot 1 before treating it as a partial-spanner token.
- [x] `pkg/layout/block_layout.go` leaf fragmentation (foundational prerequisite): the outgoing child break token under `IsBlockSizeOverride` now carries CUMULATIVE `ConsumedBlockSize` across fragmentainers. Previously each fragmentainer emitted its own local share (always `fragEnd - actualChildBlockOff`) so a wrap resume always saw the same "remaining" and looped forever. This was masked on fixed-height non-wrap layouts (the child token was never re-resumed past the row) but surfaces immediately under `column-wrap:wrap`.
- [ ] `MulticolBreakTokenData{consumed_row_block_size}` row-carry on outgoing break tokens; `offsetInCurrentRow` reads it on resume (cla.cc:2087–2093 parity). **Deferred** — driver and 5 sibling passers don't need it; `nextColToken=nil, consumedRowBlockSize=0` is the current safe default. Needed by nested multicol whose column row splits across an outer fragmentainer boundary.
- [ ] Row-gap between column rows (CSS Multicol L2 row-gap): today `rowGapSize = 0` hardcoded. `column-height-008.html` and others with explicit `gap:<row> <column>` miss the between-row padding.
- [x] Driver test: `column-height-001.html` — **PASS at 0 pixel diff**.
- [x] Cluster status: 6/31 PASS (`column-height-001/010/014/015/016/026`); 24 FAIL at 0.1%–4.2% diff. Residuals are row-gap plumbing, MulticolBreakTokenData row-carry, forced-break + wrap interactions, and `column-wrap:nowrap` overflow-past-declared-columns for `column-height-009`.
- [x] **Gate (2026-04-24):** wm 410/781, CSS2 96/99, css-flexbox 621/629, css-position 89/104, spanner-fragmentation 12/13 — all unchanged from pre-12f baseline. css-multicol **124 → 130 PASS** (+6 net).

### Phase 12g — Break-avoidance stretch retry + orphans/widows (PARTIAL 2026-04-24)
**Goal.** Port Blink's break-appeal propagation so that `break-inside:avoid` / `break-before:avoid` / `break-after:avoid` violations in column context trigger the multicol stretch loop to grow `colBlockSize` until the violation resolves.

**Scoping correction from original plan.** The plan called for full `UpdateEarlyBreakBetweenLines` + `EarlyBreak` storage + `RelayoutAndBreakEarlier` retry. Research into Blink source (see findings.md "Phase 12g findings") revealed that for the visible failing tests, EarlyBreak storage is NOT the driver — it's the `has_violating_break |= result.GetBreakAppeal() != kBreakAppealPerfect` threading (cla.cc:1053) feeding the stretch loop (cla.cc:1210+). EarlyBreak only matters when a PRIOR acceptable break point needs to be snapped to (widows/orphans mid-paragraph). The one widow/orphan driver in our test set (`balance-orphans-widows-000.html`) passes via stretch-retry alone.

- [x] **Thread `has_violating_break` via `result.GetBreakAppeal() != Perfect`** — landed in Phase 12d at `multicol_layout.go:933`. 12g extends the PRODUCERS of non-Perfect appeals:
- [x] **`block_layout.go` fragmentainer-split overflow path writes `result.BreakAppeal`.** Worst of {child's existing appeal, break-inside:avoid violation when splitting the current child, break-before:avoid violation when a leaf child starts exactly at the column boundary (childConsumed==0), break-between:avoid violation when deferring the next sibling past the column boundary}.
- [x] **`BreakBeforeChildIfNeeded → BrokeBefore` computes MinSpaceShortage.** When a soft break-before fires because the child didn't fit and has `break-inside:avoid`, shortage = `childBlock − spaceLeft` so the multicol stretch loop has a signal to grow colBlockSize to fit the child.
- [x] Driver tests: `balance-break-avoidance-000/001/002.html` — all 3 PASS at 0 diff. `balance-orphans-widows-000.html` — already passed pre-12g; verified no regression.
- [x] **Gate (2026-04-24):** css-multicol 130 → **133 PASS** (+3 net); css-position 89/104, css-flexbox 621/629, CSS2 96/99, spanner-fragmentation 12/13 all unchanged.
- [ ] `UpdateEarlyBreakBetweenLines` — **deferred** until a widow/orphan test demands it.
- [ ] `EarlyBreak` struct + `RelayoutAndBreakEarlier<MulticolLayoutAlgorithm>` path — **deferred** until a test demands it.
- [ ] Full Blink-parity `MovePastBreakpoint` refactoring (currently split between `BreakBeforeChildIfNeeded` in fragmentation_utils.go and the overflow path in block_layout.go) — **deferred** cleanup.

### Phase 12h — Rule paint + baseline + list markers (~15 tests, S–M) — **SCOPE REVISED 2026-04-24 (kickoff survey)**

**Survey finding (2026-04-24).** See `findings.md` "Phase 12h kickoff survey (2026-04-24)" for full data. The originally-planned §7/§8/§9b Blink-parity abstractions close ~0 visible tests on their own: `multicol-list-item-001/002` already PASS; most `multicol-rule-*` failures are sub-pixel AA or the Ahem font-loader bug; the §9b "first-target test" is already green; and the current `drawColumnRules` painter + Phase 12e `RenderedColumnCount` plumbing is more capable than the pre-12a §7 audit assumed.

**Revised scope (priority order; stop at each step if gate invariants move unexpectedly):**

1. [x] **Ahem font loader.** **DONE 2026-04-24** — `pkg/text/fontcache.go` now writes @font-face cache files as `<family>-<variant>.ttf` so `FontPathToFamilyVariant` can reverse-derive the logical family and `DirectGlyphProvider.resolveFamily` routes to the fonts.csv entry. Net +2 multicol PASS (`columnfill-auto-max-height-001/002` at 0 diff). `multicol-break-000/001` and `multicol-rule-001` now render Ahem glyphs but still fail on separate non-Ahem residuals (break-after:column positioning; column-rule edge paint) that were masked before. Bespoke @font-face families not in fonts.csv remain unresolvable — **picked up as F1d (2026-04-24)** after the `bidi-embed/override-006` tests were re-diagnosed and shown to fail because `ezra_silregular` hits exactly this gap. **F1d redesigned 2026-04-25** as `GlyphProvider.RegisterBuffer` on the interface — once it lands the entire path-as-identifier scaffolding (`sanitizeFamily`/`<family>-<variant>.ttf`/`FontPathToFamilyVariant`) becomes deletable, and this step's basename trick is supplanted by direct buffer registration. Details in `progress.md` "Phase 12h step 1" and `findings.md` §F1.
2. [~] **Root-cause high-diff rule-paint failures.** `multicol-rule-large-001` (7.8%), `multicol-rule-stacking-001` (3.7%), `multicol-rule-nested-balancing-003` (7.6%). **RECLASSIFIED 2026-04-24 — layout-blocked, not paint.** Debug instrumentation of `drawColumnRules` showed: `stacking-001` sets `Box.RenderedColumnCount=2` on a `column-count:4` container (layout only places content in 2 cols); `large-001` same — only col 0 gets the inline Ahem text (diff rose 7.8 → 13.1 % after step 1 exposed the lime glyphs); `nested-balancing-003` painter gets correct `contentH` (outer 250, inner fragments 200) — the 7.6 % is driven by how we render the *reference* HTML's `column-fill:auto`+`height:200` inner articles (our layout sizes them 250 and 400). Each needs a dedicated driver under a different phase (inline-in-balanced-multicol for the first two, nested `column-fill:auto` height resolution for the third). Deferred; see `findings.md` "Phase 12h step 2 reclassified".
3. [ ] **`multicol-list-item-003` dropped trailing text.** Inline text after a `column-span:all` spanner disappears in our render. Marker position is already correct; this is `block_layout` / IIM work (post-spanner inline flow), not marker-protocol work. Likely closes just -003 but touches Phase 12b territory.
4. [x] **Tiny-diff cluster sweep.** **DONE 2026-04-24** — root cause was `pkg/css/style.go` `GetColumnRuleWidth` hard-coding em base to 16 px via `ParseLength` (every other length getter on the Style struct uses `parseLengthFullWithCh` with `s.GetFontSize()`). One-line fix routes rule-width through the same helper. Net **css-multicol 135 → 154 PASS (+19)** — the 9 named `-{solid,ridge,groove,outset,inset,dashed,dotted,double,color}-000` tests plus ~10 additional -rule-* and adjacent tests whose font-size-scaled rule widths previously clipped. Gate invariants all held. Details in `progress.md` / `findings.md` "Phase 12h step 4".
5. [ ] **Deferred (keep for future phase, not abandoned):**
  - `GapGeometry{kMultiColumn}` + `GapDecorationsPainter` structural port (cla.cc:424–481) — revisit when a test demands cross_gaps / columns_per_row / spanner-adjacency flagging (`UpdateCrossGapSegmentStates`).
  - `PropagateBaselineFromChild` on column + spanner commits (cla.cc:1336, 1496) — revisit when a multicol-inside-flex/grid test needs the outer's baseline to track the inner.
  - `UnpositionedListMarker` four-callsite protocol (cla.cc:250, 1302, 1498, 383) — revisit when a test exercises marker protocol beyond what our current path already handles (001/002 PASS; 003's bug is elsewhere).

- [x] Driver tests (updated): task #7 Ahem loader (step 1); `multicol-rule-solid-000.xht` for step 4 (PASS at 0 diff post-fix). Step 2 drivers confirmed layout-blocked (see above); step 3 driver `multicol-list-item-003.html` pending.
- [x] **Gate (post-step-4, 2026-04-24):** CSS2 99/99, css-flexbox 626/629, css-position 91/105, spanner-fragmentation 12/13 all held. css-multicol **135 → 154 PASS (+19, exceeding the 145-150 ambition)**. css-writing-modes 779/781 — 2 pre-existing `bidi-embed-006/override-006` fails confirmed via `git stash` to predate this fix; tracking files had incorrectly asserted 781/781 from phase-5f. Filed as a separate pre-existing regression.

## Discipline (CLAUDE.md recap)
1. Re-read `findings.md` "css-multicol" Blink research section at the start of each phase.
2. Pick a single driver test. Do not start coding before reading that test + its reference HTML.
3. Mirror Blink type names, function names, algorithm order. No louis14-original abstractions in this category — we have reference code.
4. Run only the phase's target test + ≤2 adjacent tests (CLAUDE.md §4). Full category only at phase completion.
5. Regression sweep (all 5 gate invariants) before each commit.
6. Commit at phase boundaries with a milestone note.

## Deferred / out-of-scope for Phase 12
- Orthogonal-WDM multicol (no visible cluster in the FAIL list).
- Print / paged-media × multicol interaction (separate category).
- `::column` pseudo-element tree (CSS Overflow L4 — no WPT coverage).
- Exotic `column-rule-style` variants that need Skia primitives we don't expose (groove/ridge).

## Milestones
- **M12a:** fragmentation infrastructure re-architecture landed. **Achieved 2026-04-22** via commit `2a0d0a07`. Blink-parity `LayoutLine` outer stretch loop + `BlockBreakToken` threading + `ResolveColumnAutoBlockSize` + inline fragmentation at column boundaries + multicol dispatch enabled. Driver `multicol-fill-balance-001.xht` PASS at 0 diff. Gates held: wm 781/781, CSS2 99/99, flex 626/629, css-position 91/104 (all pre-existing).
- **M12b:** spanner re-balance. **Achieved 2026-04-23** via commit `931f48c5`. All 13 spanner-fragmentation-* tests PASS at 0 diff. css-multicol 95 → 108.
- **M12c:** nested multicol Blink-parity infra (3 of 4 cla.cc sites + resume-break emission). **Achieved 2026-04-23** via commits `cccbd05e` + `b0825367`. css-multicol 108 → 130 (+22). Driver 010 6000 → 3500 px; residual is paint/leaf-fragmentation (not 12c scope).
- **M12d:** forced breaks + break-inside:avoid-column. **Achieved 2026-04-24** — Blink-parity `BreakBeforeChildIfNeeded` + `BreakAppeal` machinery. Net +2 multicol PASS (121→123 in re-baselined run). Drivers `multicol-break-000/001` blocked by Ahem font loader bug (fragmentation tree verified correct), `multicol-br-inside-avoidcolumn-001` PASS at 0 diff. Spanner-fragmentation invariant held (12/13 PASS, no regression). Note: the +2 net in this run is small because the re-baselined snapshot reads 121 (not the previously-claimed 130) — see progress.md for the reconciliation.
- **M12e:** column-fill:auto; **PARTIAL 2026-04-24** — driver `multicol-fill-auto-block-children-003` (max-height-imposes-on-columns) PASS at 0 diff. Net +1 multicol PASS (123 → 124). Cluster residuals are missing-text-rendering, inline-overflow-clip, "more forced breaks than columns" (auto-height), and spanner+block-children — all separate root causes documented in the Phase 12e section.
- **M12f:** column-height + column-wrap (Blink-parity port of CSS Multicol L2 §4.2). **PARTIAL 2026-04-24** — driver `column-height-001.html` PASS at 0 diff. Net +6 multicol PASS (124 → 130); cluster 6/31. Leaf cumulative-consumed fix + break-token slot-layout fix unblocked row-wrap; 12f.6 `MulticolBreakTokenData` row-carry and row-gap between column rows deferred. Details in Phase 12f section.
- **M12g:** break-avoidance stretch retry (scoped port of Blink's has_violating_break propagation). **PARTIAL 2026-04-24** — 3 `balance-break-avoidance-*` drivers PASS at 0 diff. Net +3 multicol PASS (130 → 133). Full `EarlyBreak` + `RelayoutAndBreakEarlier` retry deferred (not needed by visible failing tests — stretch-retry alone handles them). Details in Phase 12g section + findings.md.
- **M12h:** rule paint + baseline + list markers — **scope revised 2026-04-24 (kickoff survey)**. Original §7/§8/§9b abstractions close ~0 tests on their own; `multicol-list-item-001/002` already PASS; most `multicol-rule-*` failures are Ahem-font-loader or sub-pixel AA. Revised attack: (1) Ahem loader, (2) high-diff rule-paint bugs, (3) `multicol-list-item-003` trailing-text, (4) tiny-diff cluster sweep; `GapGeometry` / `PropagateBaselineFromChild` / `UnpositionedListMarker` parity ports deferred until a test demands them. Ambition: css-multicol 133 → 145–150 + cross-category Ahem wins. **PARTIAL 2026-04-24** — steps 1 & 4 landed (133 → 154, +21 total — step 1 +2, step 4 +19); step 2 reclassified as layout-blocked and deferred (not paint); step 3 (list-item-003 trailing text) pending.
- Targets are conservative; overlapping cluster closures (e.g., `multicol-count-*`, `multicol-columns-*`, `multicol-gap-*`, `multicol-width-*`) likely push the final number higher without explicit phase work.
