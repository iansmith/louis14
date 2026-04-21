# Findings & Decisions — wm category (css-writing-modes)

## Rules pointer
Do not restate project rules here. They live in:
- `/Users/iansmith/louis14/CLAUDE.md`
- `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` (auto-memory index)

Findings should assume those rules are already loaded in context.

## Requirements
- 787 wm tests actually exercised by `TestWPTCSS3Reftests/css-writing-modes` (of 867 files on disk — the rest are refs / not test drivers).
- Goal: all 787 pass at 0% diff. Baseline 674/787 (85.6%) → 113 failures to close.
- Do not regress the 99/99 CSS2 suite.

## Phase 0 Baseline (complete — 2026-04-19)
Raw log: `output/wm-baseline/raw.log` (~57s runtime)
Failing list: `output/wm-baseline/failing.txt` (113 entries)
With diffs: `output/wm-baseline/failing_with_diff.tsv` (sorted by % diff, desc)

**Runner note:** wm suite is under `TestWPTCSS3Reftests` (not `TestWPTReftests`, which walks wpt-css2 only). Future bucket runs use:
`go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/<prefix>' -v`

### Failure distribution by filename-prefix bucket
| Count | Bucket | Phase |
|------:|--------|-------|
| 22 | `available-size-*` | 2 (orthog/sizing) |
|  8 | `sizing-orthog-htb-in-vlr/vrl` | 2 |
|  8 | `bidi-plaintext-*` | 1 (bidi) |
|  6 | `bidi-isolate-*` | 1 |
|  6 | `bidi-isolate-override-*` | 1 |
|  5 | `block-plaintext-*` | 1 |
|  5 | `bidi-embed-*` | 1 |
|  5 | `bidi-normal-*` | 1 |
|  4 | `border-spacing-*` | 4 (tables) |
|  4 | `float-lft-orthog-*` | 2 |
|  4 | `bidi-override-*` | 1 |
|  4 | `bidi-unset-*` | 1 |
|  3 | `float-vlr-*` | 3 (floats) |
|  3 | `float-vrl-*` | 3 |
|  3 | `block-embed-*` | 1 |
|  3 | misc (singletons) | 5 |
|  2 | `inline-block-alignment-*` | 5 |
|  2 | `img-intrinsic-size-contribution-*` | 5 |
|  2 | `mongolian-*` | 5 |
|  2 | `border-conflict-element-*` | 4 |
|  2 | `abs-pos-*` (other) | 5 |
|  2 | `logical-props-*` | 5 |
|  1 | `scrollbar-vertical-rl` | 5 |
|  1 | `block-flow-direction-vrl-026` | 5 |
|  1 | `orthogonal-*` | 2 |
|  1 | `abs-pos-border-offset-003` | 5 |
|  1 | `bidi-dynamic-iframe` | 1 |
|  1 | `block-override-*` | 1 |
|  1 | `block-override-isolate-*` | 1 |
|  1 | `baseline-*` | 5 |

### Super-clusters (drives the rebalanced phase plan)
| Count | Cluster |
|------:|---------|
| 49 | **Bidi × writing-modes** — all `bidi-*` (39) + `block-{plaintext,embed,override,override-isolate}-*` (10) |
| 35 | **Orthogonal / sizing** — `available-size` (22) + `sizing-orthog` (8) + `float-lft-orthog` (4) + `orthogonal` (1) |
|  6 | **Floats in vertical modes** — `float-vrl` (3) + `float-vlr` (3) |
|  6 | **Tables in vertical modes** — `border-spacing` (4) + `border-conflict` (2) |
| 17 | **Singletons & small groups** — abs-pos(3), inline-block-align(2), img-intrinsic(2), mongolian(2), logical-props(2), misc(3), baseline(1), scrollbar(1), block-flow-direction(1) |
| **113** | **Total** |

### High-diff outliers (top 10 by % diff — study these first within each bucket)
| % | px | test |
|---|---|------|
| 12.7% | 61004 | `scrollbar-vertical-rl.html` |
|  8.4% | 40320 | `inline-block-alignment-007.xht` |
|  4.6% | 22105 | `available-size-022.html` |
|  4.4% | 20996 | `img-intrinsic-size-contribution-001.html` |
|  3.0% | 14319 | `available-size-023.html` |
|  2.6% | 12452 | `block-flow-direction-vrl-026.xht` |
|  2.5% | 12023 | `available-size-020.html` |
|  2.5% | 12023 | `available-size-021.html` |
|  2.1% | 10000 | `border-spacing-vlr-005.xht` |
|  2.1% | 10000 | `border-spacing-vrl-004.xht` |

Most other failures are ≤ 0.5% — likely subtle axis-mapping bugs, not large layout misses. The scrollbar-vertical-rl outlier is a singleton; handle it in Phase 5.

## Phase 1 Technical Findings — bidi × writing-modes (COMPLETE)

### Result
49 bidi-bucket failures → 2 remaining. 2 remaining are out-of-bucket (iframe, `white-space: pre`). CSS2 unaffected throughout.

### Root Cause 1: Cross-span kerning merged items across bidi-level boundaries (closes 38/49)
**Location:** `pkg/layout/line_breaker.go` — `applyCrossSpanKerning` / `canMergeShapingContext`

Cross-span kerning concatenates adjacent text items sharing the same font properties and re-shapes the combined string once, then slices per-item widths from the cumulative advance array. After `SplitItemsAtLevelBoundaries` + `ReorderLineVisual`, items in visual order have bidi levels that don't correspond to source byte order. Concatenating them in visual order produced a scrambled mixed-direction string (e.g., `"> aא >  >"`) shaped as LTR — cluster→x positions were meaningless relative to per-item byte ranges.

**Fix (commit `bbec3193`):** `canMergeShapingContext` returns false when `a.BidiLevel != b.BidiLevel`.

### Root Cause 2: Block-level `unicode-bidi: plaintext` injected FSI/PDI control chars (closes 4/49)
**Location:** `pkg/layout/inline_item.go` — `injectBlockBidiControls`; `pkg/layout/layout_tree_builder.go` — `maybeWrapAnonymousBlocks`

Sub-bug A: `injectBlockBidiControls` had a `case "plaintext"` that wrapped the block's inline content in FSI (U+2068) + PDI (U+2069). `determineFSIDirection` skips content inside isolate pairs when scanning for the first strong character (UAX #9 P2), so the paragraph direction resolved to 0 (LTR default) instead of the correct RTL (strong Hebrew as first char). The plaintext block IS the paragraph; it should not be wrapped in an isolate — it should run P2/P3 resolution directly on its content.

Sub-bug B: When a block with `unicode-bidi: plaintext` contained inline content followed by a block-level child, CSS 2.1 §9.2.1.1 wraps the initial inline content in an anonymous block. That anonymous block was created without inheriting `unicode-bidi`, so it lost the plaintext semantics.

**Fix (commit `9a2f675e`):** Removed `"plaintext"` case from `injectBlockBidiControls` (block is its own paragraph — no FSI/PDI wrapper). Propagated `unicode-bidi: plaintext/bidi-override/isolate-override` to the anonymous block style in `maybeWrapAnonymousBlocks`.

### Root Cause 3: Same-level RTL cross-span kerning shaped as LTR (closes 4/49)
**Location:** `pkg/layout/line_breaker.go` — `canMergeShapingContext`; `pkg/text/measure.go` — `ShapeAdvances`

Even when all adjacent items share the same bidi level (odd = RTL), they were merged for cross-span kerning. `ShapeAdvances` calls HarfBuzz with no direction field, defaulting to LTR. HarfBuzz with direction=RTL returns glyphs in descending cluster order; the `cum[]` LTR-ascending assumption cannot interpret these — yielding scrambled / negative per-item widths (e.g., `"א > ב"` got width -41.78px).

`ShapingParams.Direction` supports `textshape.RTL` but `ShapeAdvances` never sets it. The `cum[]` slicing logic (`cum[end] - cum[start]`) also assumes ascending clusters, which is inverted for RTL.

**Fix (commit `233a65de`):** `canMergeShapingContext` returns false when `a.BidiLevel % 2 != 0`. RTL items measure standalone. Cross-span kerning between RTL spans is skipped — acceptable for Hebrew (no contextual shaping across span boundaries) and avoids scrambled widths.

**RTL shaping left for later:** If an Arabic test ever requires cross-span contextual forms, `ShapeAdvances` will need an `rtl bool` parameter, and `cum[]` will need to be built from per-glyph cluster sums (summing advances for glyphs in each byte range) rather than a prefix array.

### Phase 1 remaining (non-bidi root causes — parked in Phase 5)
- `bidi-dynamic-iframe-001`: iframe rendering capability gap (empty box). Not a bidi bug.
- `block-plaintext-006`: `white-space: pre` preservation failure inside `unicode-bidi: plaintext` block. Not a bidi ordering bug.

### Blink References — Phase 1
- `third_party/blink/renderer/core/layout/inline/inline_node.cc`
  - `InlineNode::SegmentBidiRuns`, `InlineNode::SegmentScriptRuns`
- `third_party/blink/renderer/platform/text/bidi_paragraph.h` / `.cc` — paragraph-level bidi level resolution (plaintext / auto direction uses this)
- `platform/text/bidi_resolver.h` — run resolution
- Unicode Bidirectional Algorithm (UAX #9) — especially paragraph direction rules for `unicode-bidi: plaintext`
- Writing-mode ↔ direction coupling: `ComputedStyle::GetWritingDirection`, `WritingDirectionMode`

### Phase 2 — orthogonal / sizing (35 tests)
- `core/layout/block_node.cc`
  - `BlockNode::ComputeMinMaxSizes` (orthogonal branches)
  - `BlockNode::Layout` (orthogonal root handling)
- `core/layout/constraint_space_builder.{h,cc}` — `SetIsOrthogonalWritingModeRoot`, available-size propagation
- `core/layout/length_utils.{h,cc}` — `ResolveMainInlineLength`, `ResolveMainBlockLength`
- `available-size` spec: CSS Writing Modes L3 §7.3 (available size in orthogonal flow)

### Phase 3 — floats in vertical modes (6 tests)
- `core/layout/inline/line_breaker.cc` — float positioning in line layout (vertical axis mapping)
- `core/layout/exclusions/exclusion_space.{h,cc}` — physical vs logical exclusion geometry
- `core/layout/block_layout_algorithm.cc` — `PositionFloat`, `PerformFloatLayout`

### Phase 4 — tables in vertical modes (6 tests)
- `core/layout/table/table_layout_algorithm.cc` — column-width propagation in vertical modes
- `core/layout/table/table_borders.cc` — border conflict resolution with logical vs physical edges
- Spec: CSS Writing Modes L3 §7.1 (table row/column axes)

### Phase 5 — singletons / misc (17 tests)
- Scrollbar layout: `core/layout/scrollable_overflow_calculator.cc`
- Inline-block alignment: `core/layout/inline/inline_box_state.cc` — baseline derivation for inline-block
- `img-intrinsic-size-contribution`: `core/layout/layout_replaced.cc` — intrinsic ratio in orthogonal writing mode
- Mongolian (`vertical-lr` with CJK): text orientation tables in `platform/fonts/orientation_iterator.cc`
- `logical-props-*`: `core/css/properties/longhands/` logical-property ↔ physical mapping

## Phase 2 Technical Findings — orthogonal / sizing (COMPLETE)

### Float displacement in non-BFC-establishing containers (commit `994a6018`)
**Tests:** `float-lft-orthog-{001,002,003,004}.html`
**Location:** `pkg/layout/inline_layout.go` (line layout float positioning)

`bfcInlineOrigin` (the inline offset from the current block's content edge to the BFC's content edge) was not being added to `floatStart` before comparing against the BFC-absolute float exclusion zones. The float would be positioned as if the block had no inline offset from the BFC, causing it to land at the wrong position.

**Fix:** Add `bfcInlineOrigin` to `floatStart` before the BFC-absolute comparison.

### Orthogonal available-size for scroller ancestors (commit `d660e64f`)
**Tests:** `available-size-022.html` (4.6%), `available-size-023.html` (3.0%)
**Location:** `pkg/layout/constraint_space.go` (available-size propagation)

Orthogonal writing-mode children needed available-size from their scroll container ancestors propagated correctly. The fix ensured scroller ancestor sizes were included in the orthogonal available-size computation.

## Phase 5a Technical Findings — logical-props (COMPLETE)

### Logical border property cascade contamination (commit `e639eca6`)
**Tests:** `logical-props-003.html`, `logical-props-004.html`, `logical-physical-mapping-001.html` (all 0.1% diff)
**Location:** `pkg/css/cascade.go` (logical border property cascade resolution)

Logical border properties (`border-block-start-width`, `border-inline-end-width`, etc.) were being contaminated by physical border property values set earlier in the cascade. When a physical border was set and then overridden by a logical border, the physical value was leaking into the computed logical value, producing incorrect border dimensions in vertical writing modes.

**Fix:** Fixed cascade resolution for logical border shorthand properties to not carry over physical border values.

## Phase 5b Technical Findings — abs-pos VLR (DONE — 2026-04-20)

**Tests fixed:** all three at 0% diff — `abs-pos-vlr-border-001.html`, `abs-pos-vlr-padding-001.html`, `abs-pos-border-offset-003.html`.

**Fix landed in commit `d9d313c3`** ("Fix double-px suffix in presentational attribute pixel values"): introduced a `pxValue(s)` helper in `pkg/css/cascade.go:1323-1327` that strips an existing `"px"` suffix before appending `"px"`. `applyPresentationalAttributes` now calls `style.Set("width", pxValue(val))` and `pxValue` for height. `width="100px"` now produces CSS `width: 100px` (not `100pxpx`), so the length parser accepts it, the img gets its intrinsic width from the explicit CSS, and the VLR-RTL static-position math lands the fragment correctly.

**Re-verified 2026-04-20:** all three 5b tests pass at 0% diff.

Below is the original root-cause analysis retained for historical context.

### Root cause: `applyPresentationalAttributes` double-px bug
**Tests:** `abs-pos-vlr-border-001.html` (0.1% diff — block 4 only, the `<img>` case)
**Location:** `pkg/css/cascade.go:1341-1369` (`applyPresentationalAttributes`)

HTML presentational attributes like `width="100px"` are mapped to CSS properties by appending "px":
```go
style.Set("width", val+"px")  // "100px" + "px" = "100pxpx" (INVALID CSS)
```
The CSS length parser cannot parse "100pxpx", so the width remains unset.

**Downstream effects:**
1. `getImgAttrFallbackInfo` (`intrinsic_sizing.go:202`) uses `strconv.ParseFloat(val, 64)` which fails for "100px" → returns empty `IntrinsicSizingInfo{}` (no intrinsic dimensions, no ratio).
2. `ComputeReplacedIntrinsicInlineSize` (`replaced_layout.go`): `hasExplicitBlock=true` (author CSS sets `height:40px`), no ratio, no intrinsic inline → `inlineSize = 300` (CSS 2.1 §10.3.2 replaced-element default).
3. Fragment width = 304px (300 content + 4 border).
4. Static position in VLR-RTL: `{BlockOffset=117, BlockEdge=End}`.
5. `blockOffset = 117 - 304 = -187` → physical x = -183 (far off-screen left).

**Contrast with canvas (passes):** `<canvas width="100" height="40">` is NOT in the list that sets CSS width (excluded by spec — canvas width/height set intrinsic space, not CSS width). `getCanvasIntrinsicInfo` defaults to 300×150 with `ratio=2.0` regardless of attribute parse success → `inlineSize = 40×2.0 = 80` → fragment width = 84px → `blockOffset = 117-84 = 33` → x = 37 (correct).

**Fix needed:**
```go
// cascade.go ~line 1346-1349 — strip existing "px" before appending:
numStr := strings.TrimSuffix(strings.TrimSpace(val), "px")
style.Set("width", numStr+"px")
```
Same fix for height (~line 1364-1367). This makes `width="100px"` → CSS `width: 100px` (valid).

**Open question:** After this fix, the img will have intrinsic width=100px from CSS (explicit inline). The static position conversion from HTB-RTL→VLR-RTL gives `{BlockOffset=117, BlockEdge=End, InlineOffset=52, InlineEdge=End}`. With correct inline size (104px with border), `blockOffset = 117-104 = 13` → physical x = 17. Reference expects x ≈ 5. The remaining delta (12px) needs further investigation once the fix is applied.

**Files involved:**
- `pkg/css/cascade.go:1341-1369` — `applyPresentationalAttributes`
- `pkg/layout/intrinsic_sizing.go:162-228` — `getImgAttrFallbackInfo`
- `pkg/layout/replaced_layout.go:1-81` — `ComputeReplacedIntrinsicInlineSize`
- `pkg/layout/out_of_flow_layout.go:45-293` — `LayoutCandidates` (BlockEdge=End path: line 258-267)
- `pkg/layout/static_position.go` — `ConvertToPhysical` / `ConvertToLogical`

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Phase 0 single full wm run (done) | Baseline is a one-time cost; subsequent runs are per-bucket + 20 regression-adjacent (CLAUDE.md §4) |
| Rebalanced phase order: bidi → orthog/sizing → floats → tables → singletons | Follows actual failure distribution — bidi is 43% of failures, dwarfing abs-pos (which has only 3 failures) |
| CSS2 regression check at each phase boundary | wm fixes touch inline layout + length resolution, used by CSS2 too |
| Use `TestWPTCSS3Reftests` not `TestWPTReftests` | `TestWPTReftests` scans wpt-css2 only; wm lives under wpt-css3 |

## Issues Encountered
| Issue | Resolution |
|-------|------------|
| Initial `TestWPTReftests/css-writing-modes` returned 0/99 | Wrong test function; switched to `TestWPTCSS3Reftests` |
| First pixel-diff parser showed 0.0% / ? everywhere | Anchor was wrong; fixed by tracking `=== RUN` → next `REFTEST FAIL` |

## Phase 5 Remaining — Foundational Grouping (2026-04-20)

After 5a, 5b, 5d (Mongolian B2), and the singleton sweep, four wm failures remain. They cluster into **three foundational issues**, not four — 004 and 006 likely share one root cause.

### Group A — Orthogonal-root ancestor walk (`orthogonal-root-resize-icb-007`, 1.1%)

**Test structure:** `body > div(10×10) > div(plain) > div(inline-block, WM: vertical-rl, width: 100px) > float+float (each 100×50)`.

**Failure:** after iframe resize to 100×100, inline-block orthogonal root still gets only 10px inline-size (grandparent's definite block-size), so the two floats stack instead of fitting side-by-side — 5400px of residual red.

**Blink reference (confirmed via WebFetch 2026-04-20):** in LayoutNG, orthogonal-root inline-size resolution has **no `position` gate**. The walk looks for the nearest ancestor with definite inline-size; if none exists, it falls back to the ICB. `display: inline-block`, abspos, and inline orthogonal roots all use the same algorithm. `LayoutBoxModelObject::ContainingBlockLogicalWidthForOrthogonalChild` (legacy) and LayoutNG's `ComputeOrthogonalChildrenInlineSize` both walk unconditionally.

**Our code (`pkg/layout/block_layout.go:1487` `computeOrthogonalAvailableBlock`):** climbs via `childAvailableBlock` (parent's already-resolved block-size), caps at ICB, falls back to `OrthogonalFallbackBlockSize`. The algorithm exists but the sibling tests `icb-001..006` all gate their orthogonal root through an abspos chain — abspos gets ICB via a different path in our code. Inline-block likely routes through `inline_layout.go` atomic-inline layout (`contentInlineSize` indefinite for block axis, but the orthogonal child's block-size comes from the grandparent's 10px, not the ICB).

**Fix shape:** when building the orthogonal child's constraint space from inline-layout (atomic inline path at `inline_layout.go:186-202`), the block-size passed must match the abspos path — walk ancestors unconditionally to nearest definite block-size or ICB, ignoring `position`.

**Broader impact:** per the findings already recorded — two independent data points (icb-007 + I3's B3 `IsOrthogonalTo` narrowing) suggest "position-gated" holes in our containing-block helpers. Unblocking icb-007 is likely also a wedge into css-position (54 failures currently).

### Group B — `unicode-bidi: plaintext` paragraph resolution (`block-plaintext-004`, 0.9% & `block-plaintext-006`, 1.0`) — **DONE 2026-04-21**

Both tests exercise UAX#9 P2/P3 where each paragraph is separated by a hard break and gets its own base direction from its first strong character. 004 uses `<div class="test" unicode-bidi:plaintext>` with `<br>` separators; 006 uses `<pre unicode-bidi:plaintext>` with literal `\n` separators inside `white-space: pre`.

**Expected output (both):** 3 content lines with per-line directions derived from first strong char — LTR, RTL, LTR. 006 additionally expects a blank leading/trailing line from preserved newlines.

**Prior work landed:**
- `5502e36a` B4.2: emit `InlineItemControl` per `\n` in the `!collapseSpaces+preserveNewlines` branch of `collectTextNode`.
- `8413ef9f`: blank-line strut via control-item style in `computeLineMetricsEx`; NBSP content-height via CSS-aware trimming; RTL alignment uses real container width.
- `21c779ea`: preserve leading/trailing whitespace for `white-space: pre/pre-wrap`.
- `parser.go`: `commentSeenInPre` prevents stripping leading `\n` from `<pre>` when a comment precedes text.
- `bidi.go::ResolveBidiLevelsPlaintext` (line 163): per-paragraph P2/P3 — splits at `xbidi.B` class (which `\n` has), runs `determineFSIDirection` on each paragraph independently.

**Actual root causes (the paragraph-level hypothesis was wrong — existing paragraph resolution was correct).** The remaining ~1% delta came from two independent foundational bugs, both exercised by this test's `<pre>` element which has `font-size: 150%`:

1. **Font-size percentage inheritance (`pkg/css/cascade.go:709-729`)**. `ApplyInheritedProperties` only resolved `em` font-size values against the parent's computed font-size. Percentage values were left as-is (e.g. `"150%"`). Downstream `GetFontSize()` ran the string through `ParseLengthWithFontSize` which doesn't understand `%`, and fell back to the 16px default — so the `<pre>` rendered at 16px instead of 24px. Fix: parse `%` the same way we parse `em`, using parent's font-size. Top-down cascade guarantees the parent is already resolved to an absolute px value.

2. **`InlineItemControl` strut metrics diverged from `InlineItemText` (`pkg/layout/inline_layout.go:1577-1614`).** For blank lines (two consecutive `\n` in `<pre>` produce a Control-only line), `computeLineMetricsEx` sized the strut wrong when `line-height: normal`:
   - Used `GetLineHeight()`'s 1.2×fontSize fallback instead of `text.FontHeightFromFont` (typographic ascent+descent). Text lines on the same element used the typographic path, so blank lines had a different height than text lines.
   - Used `fontSize - ascent` for descent instead of `text.FontDescentFromFont` — wrong for fonts where the typographic descent is not `fontSize - ascent` (e.g. Ahem at 24px: descent 0.2×em, but `fontSize - ascent` gave a different value when the ascent metric was derived from the font file).
   - Gated half-leading on `halfLeading > 0` — dropped it for `line-height: normal` where `lineHeight == ascent+descent` exactly.
   Fix: mirror the `InlineItemText` branch exactly — use `FontHeightFromFont` when `IsLineHeightNormal()`, `FontDescentFromFont` for descent, and apply half-leading unconditionally.

**Why the previous hypothesis was wrong.** We assumed since targeted bidi fixes had already landed, the remaining delta must be bidi-direction flow. Actually, visual inspection of the test/ref PNGs (via `/tmp/scanimg.go` pixel scanner) showed the boxes were the right shape and horizontally correct — only the vertical line spacing was off. That pointed at line metrics, not bidi.

**Result:** both 004 and 006 PASS at 0 pixel diff. The two fixes are foundational (CSS 2.1 §15.7 font-size inheritance; CSS 2.1 §10.8 line-box strut) — they should not be interpreted as plaintext-specific. Any other test exercising `%` font-size or blank-line-in-`white-space:pre` benefits.

**Regression check to run before closing:** targeted sweep of tests that use either mechanism — `font-size: NN%` in any wm test, and `<pre>` with preserved empty lines in any suite.

### Group C — VLR + `text-orientation: sideways` baseline (`inline-block-alignment-007`, 8.4%)

**Test:** `writing-mode: vertical-lr; text-orientation: sideways; font: Ahem 60/120/30`. An inline-block contains a block descendant with a larger font; the reference expects the inline-block's last-line-box baseline to align with outer "É" baselines — a straight left edge on the composite polygon.

**Three coupled bugs per `docs/plan-B1-inline-block-baseline.md`:**

1. **`text-orientation` inheritance** — `pkg/css/cascade.go` `inheritableProperties` was missing `text-orientation`. **FIXED** in I1 (commit `2ef71c5f`, B1.1). Children now inherit `sideways` from `#lr-sideways`, so `UsesCentralBaselineWithStyle` correctly returns `false` (alphabetic, not central).
2. **VLR+sideways baseline swap** — `computeLineMetricsEx` in `inline_layout.go` uses typographic ascent as `alignment_ascent`. Correct for `sideways-lr/rl` writing-mode keywords, but wrong for `vertical-lr/rl + text-orientation: sideways`: after 90° CW glyph rotation, the alphabetic baseline lands at `descent` from block-start, so `alignment_ascent = typographic_descent` and `alignment_descent = typographic_ascent`. **Attempt reverted** (`df19b64a`, 2026-04-20): I2 salvage's bulk swap was net -25 on wm. The plan's post-mortem is explicit: *"B1.2's 'swap ascent/descent for all SLR strut/text' is wrong; Blink's `LogicalBoxFragment::BaselineMetrics` likely swaps only for inline-block baseline export."*
3. **`IsSidewaysLR` flag not set for VLR+sideways** — `engine.go::fragmentToBox` conditions only on `WM == WritingModeSidewaysLR`. For `vertical-lr + text-orientation: sideways` the WM stays `WritingModeVerticalLR`, so the renderer takes the upright-stacked path. Ahem hides the glyph rotation issue visually; baseline math still needs the flag to route correctly.

**Blink reference:** `LogicalBoxFragment::BaselineMetrics` performs the swap **only when exporting a baseline for parent alignment**, not when reading strut/text metrics for line-box sizing. That is, the baseline seen by an inline-block's parent should be `physical_descent_from_block_start` in VLR+sideways, but within the inline-block's own line layout, typographic ascent/descent stay unswapped. This is the distinction the prior salvage missed.

**Fix shape:** narrow the swap to the inline-block-baseline-export path — i.e., in the place where an atomic inline's `LastBaseline` / `FirstBaseline` is computed FROM a finished child fragment for use in the parent's line-box. Don't touch strut/text metrics of the line layout inside that child.

**Regression risk:** every prior attempt broadened scope too much. Any new attempt must (a) verify the 11 text-orientation tests and 24+ `sideways-lr-*` tests currently passing remain unchanged, and (b) locate the export site precisely — probably `LogicalFragment.FirstBaseline/LastBaseline` consumers in atomic-inline placement, not the global strut math.

### Attack order (foundational correctness)

Ranked by "foundational impact per unit of effort", not by % diff:

1. ~~**Group B (block-plaintext-004 + 006)**~~ — **DONE 2026-04-21**. Two foundational fixes: font-size % inheritance in cascade, and `InlineItemControl` strut alignment with `InlineItemText` strut for `line-height: normal`. Root cause was not paragraph-level sourcing.
2. **Group A (icb-007)** — 1 test but unblocks the "position-gated ancestor walk" class of bugs; likely also unblocks unknown css-position failures. Moderate complexity.
3. **Group C (inline-block-alignment-007)** — hardest. Needs precise Blink baseline-metrics study before touching code; prior broad attempts regressed 25 tests each time. Save for last, dispatch as its own focused task with narrow scope guard.

## Multi-category baseline — 2026-04-20

Sanctioned cross-category baseline run after iframe merge. Raw logs in `output/baselines/`.

| Category | PASS | FAIL | Panic | Pass rate | Log |
|---|---|---|---|---|---|
| CSS2 (`TestWPTReftests`) | 37 | 0 | **1 — aborts run** | — | `output/baselines/css2.log` |
| css-flexbox | 621 | 8 | 0 | 98.7% | `output/baselines/flex.log` |
| css-writing-modes | 749 | 32 | 0 | 94.6% | `output/baselines/wm.log` |
| css-position | 50 | 54 | 0 | 45.5% | `output/baselines/css-position.log` |

### CSS2 crash (regression, blocks delivery)
Nil-pointer dereference at `generated-content/before-after-display-types-001.xht`. Stack:
```
pkg/layout/block_layout.go:1330 (layoutElement)
pkg/layout/block_layout.go:422  (BlockLayoutAlgorithm.Layout)
pkg/layout/engine.go:160        (LayoutEngine.Layout)
```
No code has landed between `8700eb9c` (I2 salvage) and the baseline run except findings-doc edits — so the regression was introduced by one of the four merges themselves: `2ef71c5f` (I1) → `489020db` (I3) → `6814437e` (I4) → `8700eb9c` (I2 salvage). The plan-doc claim "CSS2 regression check: 99/99 unaffected" was done per-individual-fix, never post-integration.

### WM pass-count discrepancy
Plan's running tally said 771/16. Measured baseline is **749/32** — 22 more failures than expected. Either 5a's 3 fixes silently regressed, or a subsequent merge (e.g. I4's B6 float/table work) hit wm tests that were previously passing.

### Next-category ranking (derived from the table)
1. **CSS2 panic** — urgent; unblocks delivery and the "don't regress CSS2" invariant.
2. **css-writing-modes** — stay on current plan; ~22 unaccounted failures to reconcile.
3. **css-position** — biggest headroom (54 fails at 45% pass rate). Second-best ROI.
4. **css-flexbox** — 8 singletons, likely cheap to mop up.
Tables deliberately de-prioritized — not in the top-4 failure-density pile and the table layout algorithm carries high implementation cost.

## Phase 7 — Integration Regression Audit (2026-04-20, URGENT)

Blocks Phase 5b/6/7. Two independent regressions from the integration merges on `fix/flexbox-fast`. Structured as 7a/7b/7c.

### Merge timeline on `fix/flexbox-fast`
1. `2ef71c5f` — I1: cascade + parser (B1.1, B4.1, B4.2)
2. `489020db` — I3: constraint-space + OOF static position (B5, B3)
3. `6814437e` — I4: JS engine rAF + element onload, float max-content, table-row wrapping (B6)
4. `8700eb9c` — I2 salvage: B1.2 baseline swap, B1.3 sideways broadening for VLR

### 7a — CSS2 nil-pointer panic
- Test: `generated-content/before-after-display-types-001.xht`
- Stack: `block_layout.go:1330 → :422 → engine.go:160`
- **Most likely culprit:** I4 (touched `block_layout.go` for table-row wrapping + float max-content; generated content with `display: table-row` fits the shape). Second guess: I3 (OOF `PropagateOOFCandidates` touched same file).
- **Diagnostic first step:** read the test, identify what type the nil pointer is (`LayoutBox` / `Fragment` / `ConstraintSpace`). The type narrows the culprit immediately.
- **Bisect cmd:** `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests/generated-content/before-after-display-types-001' -v` at each of the 4 SHAs.

### 7b — WM 22-test drift
- Diff `output/baselines/wm.log` failures (32) vs Phase 0 `output/wm-baseline/failing.txt` (originally 16+8=24 expected).
- Bucket by test-name prefix; attribute per bucket:
  - float/table buckets → I4
  - abs-pos/OOF buckets → I3
  - bidi/plaintext buckets → I1
  - sideways/VLR buckets → I2 salvage
- Verify Phase 5a's 3 logical-props tests (commit `e639eca6`) still pass — they may be in the 22.

### 7c — Verification gate
- CSS2: 99/99 pass.
- css-writing-modes: ≥771/781 pass.
- css-flexbox: ≥621/629 unchanged.
- css-position: ≥50/104 unchanged or improved.

### Sequencing
Do not open 7b until 7a's crash is fixed — the panic may originate in a shared code path (block_layout.go is mutated by both I3 and I4) that is also causing some of the wm drift.

## Resources
- Test dir: `/Users/iansmith/louis14/pkg/visualtest/testdata/wpt-css3/css-writing-modes/`
- Test runner: `pkg/visualtest/reftest_runner_test.go` (function `TestWPTCSS3Reftests`)
- Layout entry points: `pkg/layout/block_layout.go`, `pkg/layout/inline_layout.go`, `pkg/layout/writing_direction_mode.go`
- Baseline artifacts: `output/wm-baseline/{raw.log,failing.txt,failing_with_diff.tsv}`, `output/baselines/{css2,wm,flex,css-position}.log`
- WM-final-8 detail docs (per-phase plans, Blink refs, JS-infra inventory, icb-007 diagnosis): `docs/plan-wm-final-8-{TASK,FINDINGS,PROGRESS}.md`

---
*Update after every 2 view/browser/search operations*
