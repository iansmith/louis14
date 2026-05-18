# LOU-128 Phase 8 — Delivery & Cross-Plan Handoff

**Date:** 2026-05-16
**Branch:** `feat/LOU-128-svg-foundation`
**HEAD before Phase 8:** `61d4f810` (Phase 7 merge)

This is the delivery artifact for the final phase of LOU-128 (SVG render-subsystem
foundation). No new functional Go code landed in Phase 8 — only a sanctioned
regression sweep, oksvg-path sanity check, and surgical updates to the four
downstream plan docs.

---

## 1. Gate regression sweep

All runs prefixed `rm -rf fonts && ln -sfn /Users/iansmith/louis14/fonts fonts`
and used `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/`.

### 1a. 13 SVG-relevant gate reftests — all PASS

| # | Test | Result |
|---|------|--------|
| 1 | `filter-effects/svg-feflood-001.html` | PASS @ 0 |
| 2 | `filter-effects/filter-subregion-01.html` | PASS @ 0 |
| 3 | `css-masking/mask-image-1d.html` | PASS @ 0 |
| 4 | `css-masking/mask-opacity-1d.html` | PASS @ max diff 1 (within authored fuzz `0-5 / 0-1000`) |
| 5 | `css-position/position-relative-007.html` | PASS @ 0 |
| 6 | `css-position/position-relative-table-tr-left.html` | PASS @ 0 |
| 7 | `css-flexbox/flexbox_block.html` | PASS @ 0 |
| 8 | `css-flexbox/flexbox_flex-formatting-interop.html` | PASS @ 0 |
| 9 | `css-animations/svg-transform-animation.html` | PASS @ 0 |
| 10 | `pseudo-elements/after-content-001.html` (CSS2) | PASS @ 0 |
| 11 | `pseudo-elements/before-block-001.html` (CSS2) | PASS @ 0 |
| 12 | `pseudo-elements/before-after-combined-001.html` (CSS2) | PASS @ 0 |
| 13 | `stacking-context/opacity-affects-block-in-inline.html` (CSS2) | PASS @ 0 |

**Result: 13/13 PASS.** No gate failures.

### 1b. Broader sample (within CLAUDE.md §4 discipline — 2–4 per category)

| Category | Test | Result | Notes |
|----------|------|--------|-------|
| filter-effects (bucket I) | `svg-feoffset-001.html` | FAIL 3.5% | Pre-existing bucket-J / FE-primitive residue, not LOU-128 |
| filter-effects (bucket I) | `svg-multiple-filter-functions.html` | FAIL 2.1% | Pre-existing, bucket-J residue |
| filter-effects (bucket I) | `svg-feimage-001.html` | FAIL 18.8% | Pre-existing, FEImage not implemented |
| filter-effects (bucket I) | `svg-empty-container-with-filter-content-added.html` | FAIL 2.1% | Pre-existing — JS `waitForAtLeastOneFrame` undefined (harness limitation) |
| filter-effects (bucket I) | `svg-empty-element-with-filter-001.html` | SKIP | No usable reference files |
| filter-effects (bucket I) | `svg-empty-hidden-foreignobject-with-filter-001.html` | FAIL 2.1% | Pre-existing — `<foreignObject>` deferred |
| css-masking | `clip-path-circle-001.html` | PASS @ 0 | **win** (Phase-1 SC trigger pre-LOU-128) |
| css-masking | `clip-path-ellipse-001.html` | PASS @ 0 | **win** |
| css-masking | `clip-path-polygon-001.html` | PASS @ 0 | **win** |
| css-masking | `clip-path-inset-round-percent.html` | PASS @ 0 | unchanged |
| css-masking | `mask-image-2.html` | PASS @ 0 | **win** |
| css-masking | `mask-image-5.html` | PASS @ 0 | unchanged |
| css-position | `clear-001.xht` | FAIL 0.0% (96 pixels) | Pre-existing tiny diff, not LOU-128 |
| css-position | `clear-002.xht` | PASS @ 0 | |
| css-flexbox | `align-baseline.html` | PASS @ 0 | |
| css-flexbox | `align-content_center.html` | PASS @ 0 | |
| CSS2 | `floats/float-no-content-beside-001.html` | PASS @ 0 | |
| CSS2 | `normal-flow/block-vertical-stack-001.html` | PASS @ 0 | |

**Reading.** Of the broader sample's failures: none are LOU-128 regressions. All are
pre-existing — either bucket-J primitive correctness (which the
`docs/plan-filter-effects.md` Phase 8 owns), `<foreignObject>` (deferred from
LOU-128 by design), pre-existing layout micro-diffs (`clear-001.xht` at 0.0% / 96
pixels), or harness limitations (`waitForAtLeastOneFrame` JS missing).

All 6 css-masking gate samples pass at 0% / fuzz tolerance — confirming the
broader css-masking unblock.

### 1c. oksvg-path sanity check

| Test | Result | Notes |
|------|--------|-------|
| `css-overflow/overflow-img-svg.html` (`<img src=*.svg>`) | PASS @ 0 | Confirms oksvg path in `pkg/images/loader.go` is intact |
| `css-backgrounds/background-size-043.html` (`background-image: url(*.svg)`) | FAIL 16.7% | Pre-existing failure (confirmed in `docs/reftest-survey-2026-05-14-raw.txt`) — fails the **same way** as before LOU-128; oksvg entry not hijacked |
| `css-backgrounds/background-size-044.html` (`background-image: url(*.svg)`) | FAIL 16.7% | Same pre-existing failure |

The `<img src=*.svg>` PASS @ 0 directly demonstrates the oksvg path in
`pkg/images/loader.go::DecodeImageBytes` → `isSVGData` → `rasterizeSVG` (lines
150-280) is intact and has not been hijacked by LOU-128's inline-SVG work. The
two `background-image: url(*.svg)` failures are pre-existing background-positioning
bugs unrelated to SVG loading.

---

## 2. Doc updates

### `docs/plan-css-masking.md` — trimmed SVG-prerequisite hedges

1. Added "Status update (2026-05-16 — post LOU-128)" section after the bucket breakdown,
   listing both SVG-shaped mask gates as PASS.
2. Replaced the original "louis14 does not paint SVG child shapes at all" hedge in the
   Bucket B / `mask-image-1d` root-cause analysis with a reference to the landed APIs
   (`pkg/layout/svg/`, `pkg/render/svg_shape_painter.go`,
   `applyPresentationalAttributes`, `SVGResourceRegistry.LookupMasker`).
3. Inserted a "What LOU-128 (SVG render-subsystem foundation) replaces" section before
   the Blink references — enumerates the landed file paths, the dispatch seam
   (`block_layout.go:2504-2513`), and the **`SVGResourceRegistry`** as the single
   `url(#id)` resolver. Notes the color-convention compatibility shim and the
   minimal `mask` shorthand parser as known limitations.
4. Replaced the target-files table with a status column showing which rows are
   landed vs residual.
5. Collapsed Phase 3 to "SVG half done by LOU-128; residual = CSS multi-layer parse",
   and Phase 4 to "REPLACED by LOU-128; `mask-image-1d` PASS @ 0".
6. Resolved Key Questions 1–3 (DOM-root threading, inline-`<svg>` origin alignment,
   Phase-1 self-painting interactions) with references to the LOU-128 resolution.
7. Updated Phase → test coverage map with current status column.

### `docs/plan-filter-effects.md` — trimmed SVG-prerequisite hedges

1. Added "Status update (2026-05-16 — post LOU-128)" subsection under Current Phase
   noting that Phase 7 is REPLACED and Phase 6's element-model layer is REPLACED.
2. Edited baseline snapshot's "no SVG support whatsoever" line to a strike-through
   with the landed-API replacement reference (LOU-128's pkg paths).
3. Inserted a "What LOU-128 replaces" section listing the 3 new files added to
   `pkg/graphics/filters/` (`svg_filter_builder.go`, `fe_blend.go`,
   `fe_subregion_clip.go`) and a "Carry-over gaps" subsection documenting:
   - `FillPaint`/`StrokePaint`/`BackgroundImage` alias-to-`Source*` (bucket J).
   - Filter-element resolution wired via `BuildReferenceFilter`.
   - Color-convention shim in `svg_mask_painter.go`.
   - Content-based clipper rect-fallback.
   - `SVGResourceReference` not yet plumbed at call sites.
4. Collapsed Phase 6 to "Element-model layer REPLACED; FilterRegion + external-URL
   fetch residue remains".
5. Collapsed Phase 7 to "REPLACED by LOU-128; residual = bucket-I per-test
   correctness/interaction-order".
6. Resolved the `<foreignObject>` key question.

### `docs/plan-css-animations.md` — light touch

1. Updated Phase 3 status: SVG transform paint **unblocked by LOU-128**;
   `svg-transform-animation.html` PASS @ 0.
2. Resolved key question 3 (layout box for SVG children).

### `docs/HANDOFF-reftest-section.md` — comprehensive update

1. Header: marked LOU-128 LANDED at `feat/LOU-128-svg-foundation` (commit range
   `752e726a..61d4f810` + this Phase 8 delivery).
2. Plans-written list: marked `plan-svg-foundation.md` as LANDED with cross-ref to
   "What LOU-128 landed" below.
3. Dependency-sequencing graph: replaced the "SVG foundation NOT STARTED" block
   with the landed state and the three downstream impacts.
4. Recommended-next-move: revised to point at `plan-filter-effects.md` Phase 8 +
   Phase 6 residue, and `plan-css-ruby.md`.
5. New "What LOU-128 landed — cumulative learnings for downstream foundations"
   section enumerating the full landed file inventory across `pkg/geometry/`,
   `pkg/layout/svg/`, `pkg/graphics/filters/`, `pkg/css/`, `pkg/render/`.
6. New "Known limitations (LOU-128 carry-over)" subsection with the 5 carry-over
   gaps.
7. New "Pre-existing failures confirmed unaffected" subsection listing
   `TestBlockLayout_FloatLeft`, `TestErrorRecovery_UnclosedBlocks`,
   `TestParseInlineStyle_BorderShorthand`, `clear-001.xht`,
   `background-size-043/044.html`, and the bucket-I filter primitive gaps.
8. Open loose ends: added 4 LOU-128 follow-up items (plumb
   `SVGResourceReference`, replace alias-to-`Source*`, add real `mask` shorthand
   parser, fix mazarin Bevel-fallback / dashoffset).

---

## 3. Inconsistencies noticed between brief and landed code

The Phase 8 brief lists a few line numbers and file references that have shifted
slightly during Phases 1–7 (orchestrator may wish to correct the source-of-truth):

- **`paintSelfForeground`** brief says `render.go:1228`; HEAD `61d4f810` has it at
  `render.go:1407`.
- **`paintLayerWithMask`** brief says `render.go:520-720`; HEAD has `paintLayerWithMask`
  at `render.go:621` (function start) with the SVG fast-path / mask compositing
  body running through ~720. The brief's range is close but the function start
  is later than 520.
- **`LayoutInputNode.SVGRoot`** brief says `pkg/layout/layout_input_node.go:133`;
  HEAD has the `SVGRoot any` declaration at line 133 with the doc-comment block
  starting at 123 — both ways of citing it are fine.
- **`pkg/layout/svg/svg_length_context.go`** the brief's bullet listed it
  correctly under `pkg/layout/svg/`; verified present.
- **`applyPresentationalAttributes`** brief says `pkg/css/cascade.go:1508`; HEAD
  has it at exactly 1508 — accurate.
- **Dispatch seam** brief says `pkg/layout/block_layout.go:2504-2513`; HEAD has the
  comment block at 2504–2513 with `NewSVGRootAlgorithm(...).Layout()` at 2513 —
  accurate.

None of these shifts affect the public API surface; only line numbers for the
in-doc references. The downstream plan-doc edits cite ranges (e.g. "approx line
1407") and module-relative paths so they remain accurate as future commits shift
line numbers.

---

## 4. Pre-existing failures confirmed (not LOU-128's concern)

Per the brief's expectations:

- `TestBlockLayout_FloatLeft` — Phase 2-era, unrelated.
- `TestErrorRecovery_UnclosedBlocks`, `TestParseInlineStyle_BorderShorthand` —
  unrelated.
- `css-position/clear-001.xht` — 0.0% / 96 pixels pre-existing.
- `css-backgrounds/background-size-043/044.html` — pre-existing
  background-positioning bug; confirmed in `docs/reftest-survey-2026-05-14-raw.txt`.
  oksvg path entry confirmed intact via `css-overflow/overflow-img-svg.html` PASS @ 0.
- `filter-effects/svg-feimage-001`, `svg-feoffset-001`,
  `svg-multiple-filter-functions`, `svg-empty-*` — bucket-J primitive correctness
  residue, owned by `docs/plan-filter-effects.md` Phase 8.

---

## 5. Summary

- All 13 gate reftests PASS (CSS3 + CSS2). Broader 18-test sample shows no LOU-128
  regressions — all failures map to pre-existing buckets.
- oksvg path verified intact via `<img src=*.svg>` PASS @ 0.
- 4 downstream plan docs updated: `plan-css-masking.md`, `plan-filter-effects.md`,
  `plan-css-animations.md`, `HANDOFF-reftest-section.md`.
- Phase 8 delivery commit pushes this artifact to the worktree branch for
  orchestrator merge.
