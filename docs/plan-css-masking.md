# Task Plan: Pass the entire css-masking WPT reftest category

## Goal
All `css-masking` tests under `pkg/visualtest/testdata/wpt-css3/css-masking/` pass at 0% pixel
diff via `TestWPTCSS3Reftests/css-masking`. Baseline **2 passing / 6 failing (8 run)** → close the
6 failures without regressing adjacent categories (css-position, css-flexbox, css-writing-modes,
CSS2).

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff
   required, test-execution discipline (only the 6 failing tests + regression-adjacent), operational
   rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory
   index pointing at the same rules.

If you are about to type a rule verbatim into this file or into code comments, stop and link instead.

## Baseline snapshot (2026-05-14)
From `grep "FAIL: TestWPTCSS3Reftests/css-masking/" docs/reftest-survey-2026-05-14-raw.txt`:

| Test | Bucket | Symptom in `output/reftests/*_test.png` |
|------|--------|------------------------------------------|
| clip-path-circle-001.html  | A: clip-path basic shapes | renders the **unclipped** 200×200 green square — no clip applied |
| clip-path-ellipse-001.html | A: clip-path basic shapes | renders the unclipped div **with its red 50px border** — no clip applied |
| clip-path-polygon-001.html | A: clip-path basic shapes | renders the unclipped div with its red border — no clip applied |
| mask-image-1d.html   | B: mask | SVG `<rect>` renders **nothing** (blank). Ref = purple 100×50 div |
| mask-image-2.html    | B: mask | two divs render as **solid purple** — gradient mask ignored entirely |
| mask-opacity-1d.html | B: mask | renders **full** blue square at opacity 0.5 — SVG `url(#myMask)` mask ignored |

### Bucket breakdown
- **Bucket A — CSS `clip-path` basic shapes (3 tests):** `circle()`, `ellipse(...)`, `polygon(...)`.
- **Bucket B — CSS/SVG mask (3 tests):** gradient `mask-image`, SVG `<mask>` element resolution via
  `url(#id)`, and an SVG `mask=` presentation attribute on an SVG shape.

### Status update (2026-05-16 — post LOU-128)
The SVG render subsystem is now landed (`docs/plan-svg-foundation.md` Phases 0–7 on
`feat/LOU-128-svg-foundation`). Both SVG-shaped mask gate tests pass:

| Test | Status |
|------|--------|
| `mask-image-1d.html`   | PASS @ max diff 0 |
| `mask-opacity-1d.html` | PASS @ max diff 1 (within authored fuzz `0-5 / 0-1000`) |

Bucket A `clip-path-{circle,ellipse,polygon}-001` and Bucket B `mask-image-2` also
pass on `feat/LOU-128-svg-foundation`. The remaining residue of this plan is its
CSS-side work; Phases 3–4 collapse to verification per the foundation handoff (see
"What LOU-128 replaces" below).

## Root-cause analysis (done — read before coding)

### Bucket A: `applyClipPath` exists but is never reached for in-flow elements
The clip-path pipeline is **already built end-to-end** and is correct:
- Parsing: `pkg/css/style.go:7612 GetClipPath` → `parseClipPathCircle/Ellipse/Polygon`
  (`style.go:7709-7788`), value/percentage resolution `ClipPath.ResolveClipPath`
  (`style.go:7850-7905`). Spot-checked against the three tests — `circle()` resolves to
  `closest-side` radius 100 at center (100,100) for the 200×200 box; `ellipse(75px 50px at 125px
  100px)` and the polygon coordinates resolve correctly.
- Paint: `pkg/render/render.go:1542 applyClipPath` builds a `DrawCircle` / cubic-Bezier ellipse /
  `MoveTo`+`LineTo` polygon path and calls `r.dc.Clip()`. It is invoked at `render.go:1256-1259`
  inside `paintLayerContent`, *before* `paintSelfDecorations` — so it would clip background + border,
  which is correct per CSS Masking L1 (clip-path clips the entire box).
- The `PaintLayer.ClipPath` field is populated at `pkg/render/paint_layer.go:639`.

**The bug:** `paintLayerContent` is only called for **self-painting stacking-context layers** (via
`paintLayer`, `render.go:455`). A static `<div>` whose only fancy property is `clip-path` is **not**
a stacking context in louis14 — `layout.Box.CreatesStackingContext()`
(`pkg/layout/types.go:114-187`) checks opacity/transform/filter/blend/contain/isolation/will-change
but **never `clip-path`**. So the test divs land in `parentLayer.FlowChildren`
(`paint_layer.go:978`) and are painted by `paintDescendantsPhase` →
`paintDescendantPhase` (`render.go:1415`), which paints `paintSelfDecorations` + `paintSelfForeground`
and recurses — but **never touches `layer.ClipPath`**. The clip-path code path is dead for these
elements.

Per CSS Masking Level 1 §3.1 and the Blink longhand metadata, a computed `clip-path` other than
`none` **creates a stacking context** (and in Blink, a `PaintLayer`). louis14 must mirror this.

### Bucket B: mask requires (1) the same SC fix, (2) SVG `<mask>` resolution, (3) basic SVG shape paint
louis14 already has a real CSS-mask compositor: `pkg/render/paintLayerWithMask`
(`render.go:515-653`). It renders the subtree to an offscreen buffer, builds a mask buffer, and does
`buf.Pix[*] *= maskFactor` per pixel — this is exactly Blink's `SkBlendMode::kDstIn` (destination
alpha scaled by source). It already distinguishes **luminance** mode (gradients, sRGB-coefficient
luminance × alpha) from **alpha** mode (`url()` images). But three things are missing:

1. **`mask-image-2` (gradient mask, static divs):** same SC bug as Bucket A. `HasMaskImage` is set
   on the `PaintLayer` (`paint_layer.go:642-645`), but `paintLayer`'s `if layer.HasMaskImage` branch
   (`render.go:468`) only runs for self-painting layers. Static divs with only `mask-image` are not
   stacking contexts → `paintLayerWithMask` never runs → the divs paint as solid purple.
   Per CSS Masking L1 §6.1 a non-`none` `mask`/`mask-image` creates a stacking context.

2. **`mask-opacity-1d` (`mask-image: url(#myMask)` + `opacity:0.5`):** `opacity<1` *does* trigger a
   stacking context, so `paintLayerWithMask` **is** reached. But the mask value is
   `url(#myMask), url(#myMask)` — a **comma-separated two-layer list** referencing an **inline SVG
   `<mask>` element**. `paintLayerWithMask` (`render.go:575-608`) treats `url(...)` only as a raster
   image: `images.LoadImageWithFetcher("#myMask")` fails → `maskImg == nil` → the "no valid mask →
   composite as-is" fallback (`render.go:611-615`) → the element renders fully (at 0.5 opacity),
   which is the observed bug. Needs: (a) parse a multi-layer mask list, (b) resolve `url(#id)` to an
   SVG `<mask>` DOM element and rasterize its child shapes into the mask buffer (SVG masks are
   **luminance** by default), (c) intersect multiple mask layers.

3. **`mask-image-1d` (`<svg><rect ... mask="url(#foo)"/></svg>`):** the test renders blank because
   **louis14 does not paint SVG child shapes at all** (historical note — **resolved by LOU-128**:
   `pkg/layout/svg/` builds an `SVGRoot` + shape tree, and `pkg/render/svg_shape_painter.go` paints
   `<rect>`/`<circle>`/`<ellipse>`/`<path>` with `fill`/`stroke`; the SVG `mask=` presentation
   attribute flows through `pkg/css/cascade.go` `applyPresentationalAttributes`, and unresolvable
   `url(#foo)` falls back to "no mask, render normally" via the
   `SVGResourceRegistry.LookupMasker → (_, false)` path). The work below is no longer required;
   `mask-image-1d` now passes at 0% diff on LOU-128.

## What LOU-128 (SVG render-subsystem foundation) replaces

LOU-128 landed the inline-SVG render tree, paint walk, resource registry, and
`<filter>` element host. The css-masking plan's SVG-half work is covered:

- **SVG `<mask>` element + resolution:** built by LOU-128 Phase 5 in
  `pkg/layout/svg/svg_resource_masker.go` (luminance + alpha rasterization).
  `url(#id)` resolution flows through `pkg/layout/svg/svg_resource_registry.go`
  (`SVGResourceRegistry.LookupMasker`), with cycle detection in
  `svg_resources_cycle_solver.go`. The mask painter is
  `pkg/render/svg_mask_painter.go`.
- **Basic SVG shape painting:** `pkg/layout/svg/svg_shape.go` +
  `pkg/render/svg_shape_painter.go` (rect, circle, ellipse, path with fill/stroke).
- **SVG `mask=` / `clip-path=` presentation attributes:**
  `pkg/css/cascade.go:1508 applyPresentationalAttributes` is extended to map SVG
  presentation attributes onto computed style on every element.
- **`url(#id)` resolver — the single resolver:** `SVGResourceRegistry` per
  `Renderer`. Phase 6 of LOU-128. Use `LookupClipper(id)`/`LookupMasker(id)` /
  the typed wrappers; `ResourceReference` plumbing is reserved
  (`svg_resource.go::SVGResourceReference`) as a follow-up but not yet at call sites.
- **Dispatch seam:** inline `<svg>` is routed through
  `SVGRootAlgorithm` at `pkg/layout/block_layout.go:2504-2513`; the `<svg>` keeps
  its replaced-element role (mirrors Blink's `LayoutSVGRoot : LayoutReplaced`).
  The new `LayoutInputNode.SVGRoot any` field (`pkg/layout/layout_input_node.go:123`)
  carries the SVG subtree.

**Net effect on this plan:**
- Phase 3 ("SVG mask resolution + multi-layer list") — the SVG half is **done**.
  Any remaining work is the CSS `mask-image` comma-list parsing (`GetMaskLayers`)
  if/when a multi-layer test surfaces. The `mask` CSS shorthand parser is minimal;
  `GetMaskImage` falls back to the `mask` shorthand string — this is a known LOU-128
  carry-over gap and the natural place to address it is here.
- Phase 4 ("Basic SVG shape painting + SVG `mask=` attribute") — **done**.
  `mask-image-1d` now passes at 0% diff.

**Known limitation that any future css-masking work touching translucent SVG
content must respect:** `pkg/render/svg_mask_painter.go::compositeBufferWithOpacityOnto`
contains a color-convention compatibility shim that reproduces louis14's
project-wide straight-alpha `color.RGBA` convention. The result is correct for
`mask-opacity-1d` (passes at max diff 1, within authored fuzz). A project-wide
color-convention cleanup is a future ticket. Do not "fix" the shim in isolation;
fix it as part of the broader convention work.

## Blink references (study before writing code)

### clip-path → Path
- **`core/paint/clip_path_clipper.cc`** — `ClipPathClipper`. `LocalReferenceBox()` picks the
  `GeometryBox` (default **border-box** for CSS boxes; `BorderBoxRect()` for non-SVG).
  `GetPathWithObjectZoom()` calls `ShapeClipPathOperation::GetPath(reference_box, zoom, scale)`.
  `PathBasedClipInternal()` returns the `Path`; the painter applies it via
  `context.ClipPath(path->GetSkPath(), kAntiAliased)`.
- **`core/style/basic_shapes.cc`** — `BasicShapeCircle::GetPath`, `BasicShapeEllipse::GetPath`,
  `BasicShapePolygon::GetPath`, all `Path GetPath(const gfx::RectF& bounding_box, float zoom, float
  path_scale)`. Circle/ellipse delegate to `GetPathFromCenter()`; center via
  `PointForCenterCoordinate()` (`FloatValueForLength` against box width/height). Radius resolution:
  `kValue` → length against the box diagonal; `kClosestSide` → min distance center→edge;
  `kFarthestSide` → max distance center→corner. Polygon: scale coordinate pairs to points, build the
  path (optional vertex rounding via `GetRoundedPolygonRadius()` — not needed for the 3 tests).
- **`core/style/clip_path_operation.h`** — `ShapeClipPathOperation`, `GeometryBoxClipPathOperation`,
  `ReferenceClipPathOperation` (the SVG `url()` variant), `GeometryBox` enum.

louis14's `parseClipPath*` + `ResolveClipPath` + `applyClipPath` already mirror this structure
(closest-side, center default to box centre, percentage-of-diagonal/√2 for circle). **No new
clip-path geometry code is required** — only the stacking-context wiring and (Phase 2) using the
**border-box** as the reference box, which `applyClipPath` already does (`box.X/Y` + `box.Width/Height`
is the border box per `pkg/layout/types.go:13-22`).

### CSS / SVG mask painting
- **`core/paint/box_fragment_painter.cc`** — `PaintMask()` runs in `PaintPhase::kMask`. The mask
  layers are painted into a compositing layer (`context.BeginLayer()` / `EndLayer()`); the mask
  layer is composited against the element with **`SkBlendMode::kDstIn`** (destination alpha × source
  alpha). louis14's `paintLayerWithMask` already implements the kDstIn math directly on the pixel
  buffer.
- **`core/paint/svg_mask_painter.cc`** — `SVGMaskPainter::Paint`. `ResolveElementReference()` finds
  the `LayoutSVGResourceMasker` for a `url(#id)`. `PaintSVGMaskLayer()` builds a `PaintRecord` via
  `masker->CreatePaintRecord()`, applies `MaskToContentTransform()`, clips to
  `masker->ResourceBoundingBox()`. **Luminance vs alpha:** `EFillMaskMode::kLuminance` wraps the
  content in `ScopedMaskLuminanceLayer` (`cc::ColorFilter::MakeLuma()`); SVG `<mask>` defaults to
  **luminance** (`mask-type: luminance`).
- **`core/style/fill_layer.h` / `BoxModelObjectPainter::PaintMaskImages`** — iterates the mask layer
  list (`mask-image` is a comma list); each layer can be a gradient, an image, or an SVG mask
  reference. `match-source` (the default `mask-mode`) → SVG-`<mask>`/`<image>` references use
  luminance/their own type, raster images use alpha, gradients use luminance.

## Target files in louis14

| Concern | File | Status |
|---------|------|--------|
| clip-path / mask create a stacking context | `pkg/layout/types.go` — `Box.CreatesStackingContext()` | landed |
| clip-path geometry | `pkg/css/style.go`, `pkg/render/render.go::applyClipPath` | landed |
| SVG `<mask>` element resolution + rasterization | `pkg/layout/svg/svg_resource_masker.go` + `pkg/render/svg_mask_painter.go` | landed (LOU-128 Phase 5–6) |
| basic SVG shape painting (`<rect>`, fill, stroke, path) | `pkg/layout/svg/svg_shape.go` + `pkg/render/svg_shape_painter.go` | landed (LOU-128 Phase 2) |
| SVG `mask=` presentation attribute → style | `pkg/css/cascade.go::applyPresentationalAttributes` | landed (LOU-128) |
| Multi-layer `mask-image` parsing (remaining residue) | `pkg/css/style.go` — `GetMaskImage` (+ new `GetMaskLayers`) | not yet — natural follow-up here |
| `mask` CSS shorthand parser (minimal in LOU-128 — `GetMaskImage` falls back to the `mask` shorthand) | `pkg/css/style.go` | LOU-128 carry-over; finish here when needed |

Mirror Blink's source layout: clip-path-as-clip belongs with the painter (`pkg/render/`, like
`clip_path_clipper.cc` lives in `core/paint/`); shape→path conversion belongs with style
(`pkg/css/style.go`, like `basic_shapes.cc` in `core/style/`) — both already placed correctly.
New SVG mask/shape painting code lives in `pkg/render/svg_mask_painter.go` /
`pkg/render/svg_shape_painter.go` mirroring `core/paint/svg_mask_painter.cc` etc. SVG resource
elements live in `pkg/layout/svg/svg_resource_*.go` mirroring `core/layout/svg/`.

## Phases (foundational-first)

### Phase 0: Baseline & confirm root causes — **prerequisite**
- [ ] Render the 6 failing tests once each (sanctioned: ≤3 at a time, two passes) and confirm the
      `_test.png` symptoms match the table above. Artifacts already on disk under
      `output/reftests/`.
- [ ] Confirm via a debug print (or test) that for `clip-path-circle-001` the div's `PaintLayer`
      lands in `FlowChildren` and `CreatesStackingContext()` returns false — proving the dead-code
      path.
- **Gate:** root-cause table confirmed; no code changed yet.

### Phase 1: clip-path & mask establish a stacking context (fixes Bucket A entirely + mask-image-2)
**Goal:** a non-positioned element whose only relevant property is `clip-path` (or `mask`/
`mask-image`) becomes a self-painting layer so `paintLayerContent` / `paintLayerWithMask` runs.

- **Blink ref:** CSS Masking L1 §3.1 (clip-path) and §6.1 (mask) — both create a stacking context;
  Blink's `PaintLayerPainter` / `LayoutObject::HasNonInitialMaskImage` etc. gate on this. Algorithm
  structure mirrors the existing opacity/filter/transform branches in
  `CreatesStackingContext()`.
- **louis14 target:** `pkg/layout/types.go` — `Box.CreatesStackingContext()` (`types.go:114-187`).
  Add, alongside the existing filter/blend checks:
  - `if b.Style.GetClipPath() != nil { return true }` (clip-path other than `none`).
  - `if mi := b.Style.GetMaskImage(); mi != "" && mi != "none" { return true }` — also covers the
    SVG `mask=` presentation attribute once Phase 3 maps it into style.
- **New types:** none.
- **Approach:** one-line-each additions mirroring the existing branches. No new paint code — the
  existing `applyClipPath` (`render.go:1542`) and `paintLayerWithMask` (`render.go:515`) become
  reachable. Verify the clip-path reference box: `applyClipPath` uses `box.X/Y` +
  `box.Width/Height` = border box, which is the CSS Masking L1 default reference box for
  `<basic-shape>` — correct, no change.
- **Tests fixed:** `clip-path-circle-001`, `clip-path-ellipse-001`, `clip-path-polygon-001`,
  `mask-image-2`. (4 of 6.)
- **Watch for regressions:** making clip-path/mask elements self-painting changes their paint
  slot from "DOM-order FlowChild phase walk" to "atomic `paintLayer`". This is exactly how
  opacity/filter elements already behave, and `buildPaintSubtree` (`paint_layer.go:958-981`)
  already routes non-positioned stacking contexts into `FlowChildren` for correct DOM-order
  placement — so siblings/z-order are unaffected. Still: re-run css-position + css-flexbox +
  CSS2 spot-checks for any element that combined clip-path/mask with floats or overflow.
- **Gate:** the 3 clip-path tests and `mask-image-2` pass at 0% diff; css-position, css-flexbox,
  css-writing-modes, CSS2 unchanged. **Commit.**

### Phase 2: clip-path correctness audit (no test target — hardening)
**Goal:** ensure the clip-path primitive is foundationally correct for all basic shapes, not just
the three tested argument forms — per CLAUDE.md §1.

- **Blink ref:** `basic_shapes.cc` radius resolution (`kClosestSide` / `kFarthestSide` / `kValue`),
  `PointForCenterCoordinate`; `clip_path_clipper.cc` `LocalReferenceBox` (border-box default).
- **louis14 target:** `pkg/css/style.go` `ResolveClipPath` (`style.go:7850-7905`),
  `pkg/render/render.go` `applyClipPath` (`render.go:1542`).
- **Approach (read + verify, fix only if wrong):**
  - `circle()` default radius = `closest-side`; verify `ResolveClipPath` computes
    `min(Cx, w-Cx, Cy, h-Cy)` (it does — `style.go:7876-7882`). Confirm `farthest-side` is also
    handled or explicitly out of scope (the tests use default + value only; `parseClipPathCircle`
    currently drops `closest-side`/`farthest-side` keywords — note as a known gap, not a blocker).
  - `ellipse()` default radii = `closest-side` per axis; current code defaults to `w/2,h/2`
    (`style.go:7886-7892`) which equals `closest-side` only when the centre is centred — fine for
    `clip-path-ellipse-001` (explicit radii) but flag for completeness.
  - Polygon: `applyClipPath` requires ≥4 floats (≥2 points); `clip-path-polygon-001` has 4 points.
    Confirm winding/`ClosePath` produces a filled quad.
  - Reference box: confirm `box.X/Y/Width/Height` is the **border box** (it is) — `clip-path-
    ellipse-001` and `-polygon-001` rely on coordinates measured from the border-box origin,
    including the 50px red border.
- **Tests fixed:** none new (Phase 1 already fixes them) — this phase prevents a point-fix and
  documents known basic-shape gaps (`inset()`, `path()`, `farthest-side`) as out-of-category.
- **Gate:** the 3 clip-path tests still pass; a short note in the progress doc lists the verified
  vs. deferred basic-shape forms. **Commit if any fix was needed.**

### Phase 3: SVG mask resolution + multi-layer mask list (fixes mask-opacity-1d)
**Status (2026-05-16):** SVG-half **done by LOU-128 Phase 5–6**. `mask-opacity-1d` passes at
max diff 1 (within authored fuzz `0-5 / 0-1000`). What remains is the CSS-side multi-layer parse.

- LOU-128 wired: `pkg/layout/svg/svg_resource_masker.go`,
  `pkg/render/svg_mask_painter.go`, `pkg/layout/svg/svg_resource_registry.go`
  (the single `url(#id)` resolver per Renderer; cycles handled by
  `svg_resources_cycle_solver.go`).
- Residual CSS-side work for this plan:
  - `pkg/css/style.go` — add `GetMaskLayers() []string` that splits `GetMaskImage()` on top-level
    commas (respecting parens). Note: `mask` CSS shorthand parser is minimal — `GetMaskImage`
    currently falls back to the `mask` shorthand string. That is the natural place to start.
  - `pkg/render/paint_layer.go` — if a multi-layer test surfaces, store `[]string`
    mask layers on the `PaintLayer` alongside the single `MaskImage`.
  - `pkg/render/render.go` `paintLayerWithMask` — generalize to iterate layers when a multi-layer
    test demands it. LOU-128 left the single-layer path correct; multi-layer is straight extension.
- **Known limitation to preserve:** `svg_mask_painter.go::compositeBufferWithOpacityOnto` carries
  a straight-alpha color-convention shim. Do not remove in isolation; project-wide cleanup is a
  separate ticket.
- **Tests fixed by LOU-128:** `mask-opacity-1d` (max diff 1, within fuzz).
- **Gate:** verify `mask-opacity-1d` still passes; if a multi-layer reftest surfaces, add the parse.

### Phase 4: Basic SVG shape painting + SVG `mask=` attribute (fixes mask-image-1d)
**Status (2026-05-16):** **REPLACED by LOU-128.** `mask-image-1d` passes at 0% diff.

LOU-128 Phase 2 + Phase 5 + Phase 6 jointly delivered:
- SVG shape painting (`pkg/layout/svg/svg_shape.go`,
  `pkg/render/svg_shape_painter.go`) for `<rect>`/`<circle>`/`<ellipse>`/`<path>` with
  `fill`/`stroke`.
- SVG presentation-attribute mapping via
  `pkg/css/cascade.go::applyPresentationalAttributes` (unconditional on every element).
- Unresolvable `mask="url(#foo)"` falls back to "no mask, render normally" through
  `SVGResourceRegistry.LookupMasker → (_, false)`.

No remaining work in this phase. Keep the phase header as a record that the test landed.

### Phase 5: Delivery & regression audit
- [ ] Confirm all 8 `css-masking` tests pass at 0% diff via
      `TestWPTCSS3Reftests/css-masking`.
- [ ] Regression spot-check (sanctioned, adjacent only): css-position, css-flexbox,
      css-writing-modes, CSS2 — Phase 1's stacking-context change is the highest-risk item.
- [ ] Final commit summary + progress note.
- **Gate:** css-masking 8/8; no regression in adjacent categories.

## Phase → test coverage map
| Test | Fixed by | Status |
|------|----------|--------|
| clip-path-circle-001  | Phase 1 (SC trigger) | PASS @ 0 |
| clip-path-ellipse-001 | Phase 1 (SC trigger) | PASS @ 0 |
| clip-path-polygon-001 | Phase 1 (SC trigger) | PASS @ 0 |
| mask-image-2          | Phase 1 (SC trigger) | PASS @ 0 |
| mask-opacity-1d       | LOU-128 Phase 5–6 (SVG `<mask>` resolution via `SVGResourceRegistry` + luminance rasterization) | PASS @ max diff 1 (within fuzz) |
| mask-image-1d         | LOU-128 Phase 2 + Phase 6 (SVG `<rect>` paint + SVG `mask=` attr unresolvable fallback) | PASS @ 0 |

## Key Questions
1. ~~Does threading the document root into `Renderer` (for `url(#id)` resolution) collide with the
   existing `NewRendererForDrawContext` / child-context renderers?~~ **Resolved by LOU-128.** The
   `Renderer` carries `svgResources *SVGResourceRegistry`; `collectSVGResources` +
   `SolveResourceCycles` run per-frame, so `url(#id)` resolution is independent of which
   `DrawContext` is active.
2. ~~Is the inline `<svg>`'s box laid out at the right physical origin for `mask-image-1d` to
   align with its plain-`<div>` reference?~~ **Resolved by LOU-128.** `mask-image-1d` passes at 0
   diff; inline-`<svg>` flows correctly through `block_layout.go:2504-2513`.
3. ~~Does Phase 1 making mask elements self-painting interact badly with the existing
   `paintLayerWithMask` offscreen-buffer path?~~ Phase 1 already landed; no regressions observed
   in the LOU-128 gate sample (`mask-image-2`, `mask-image-5`, `clip-path-*` all PASS @ 0).

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Phase 1 fixes 4 of 6 tests with a ~2-line change | The clip-path & CSS-mask compositors already exist and are correct; the only bug is the missing stacking-context trigger. Foundational, not a point fix — it is the spec-mandated behaviour. |
| clip-path geometry gets an audit phase, not a rewrite | `parseClipPath*` + `ResolveClipPath` + `applyClipPath` already mirror Blink's `basic_shapes.cc`. CLAUDE.md §1: verify it generalizes; only fix if wrong. |
| SVG `<mask>` resolution is its own phase | It is genuinely new capability (DOM `url(#id)` lookup + rasterizing SVG children at luminance) — distinct from the CSS-mask compositor, which already works. |
| Basic SVG shape painting is scoped to `<rect>`+`fill` | `mask-image-1d` only needs `<rect>`. Structure for extension but do not gold-plate (CLAUDE.md). |
| Unresolvable mask = no mask (render normally) | SVG 1.1 §14.4 / CSS Masking L1 §6.1; louis14's existing `maskImg == nil` fallback is already correct. |
| New SVG paint code goes in `pkg/render/` | Mirrors Blink's `core/paint/svg_mask_painter.cc` / `svg_*` placement (MEMORY: mirror Blink file placement). |

## Notes
- Test command template:
  `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-masking/<name>' -v`
- Pre-rendered diff artifacts: `output/reftests/<name>_{diff,ref,test}.png`.
- The 8 tests in this category: 6 failing (above) + 2 already passing
  (`clip-path-inset-round-percent`, `mask-image-5`).
- Worktree note (CLAUDE.md §5): if Phase 3/4 work is delegated to worktree agents, symlink `fonts/`
  from the main dir first, and commit+push `master`/`fix/*` before launching them.
</content>
</invoke>
