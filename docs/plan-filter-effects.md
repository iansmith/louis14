# Task Plan: Pass the entire filter-effects category

## Goal
All filter-effects reftests under `pkg/visualtest/testdata/wpt-css3/filter-effects/` pass at 0% diff via `TestWPTCSS3Reftests/filter-effects/`. Baseline **92 passing / 178 failing / 6 skipped (270 run)** → close the 178 failures without regressing adjacent categories (css-backgrounds box-shadow, compositing, css-masking, css-will-change).

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before any planning or coding session — non-negotiable:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline (only the 1–4 tests under work), operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index pointing to the same rules.

If you are about to type a rule verbatim here or in a code comment, stop and link instead.

## Current Phase
Phase 0 (baseline & categorization) — **COMPLETE 2026-05-14**. Plan written; no code yet.

### Status update (2026-05-16 — post LOU-128)
The SVG render-subsystem foundation (`docs/plan-svg-foundation.md` Phases 0–7) has
landed on `feat/LOU-128-svg-foundation`. This **replaces** Phase 7 (bucket I) and the
element-model layer of Phase 6 (bucket H). See "What LOU-128 (SVG foundation) replaces"
below for the exact API surface and the carry-over gaps bucket J must address.

`pkg/graphics/filters/` (the FilterEffect graph from Phases 1–3 of this plan) is also
landed, plus three additions from LOU-128: `svg_filter_builder.go`, `fe_blend.go`, and
`fe_subregion_clip.go`. LOU-128 gate samples passing: `svg-feflood-001.html`,
`filter-subregion-01.html`.

## Baseline snapshot (Phase 0)

178 failures. The root cause was structural — louis14 had **no FilterEffect graph and
no SVG render subsystem** at the time of writing. Both are now landed (the FilterEffect
graph from Phases 1–3 here; the SVG render subsystem from LOU-128). What follows
preserves the original baseline analysis so the bucket math is auditable; see the
"Status update" above and the "What LOU-128 replaces" section below for current state.

The existing code in `pkg/render/render.go` (`paintLayerWithFilter`, `applyBackdropFilter`, `applyGrayscale`/`applyContrast`/…) is a flat list of per-pixel byte loops that:

- operate on **premultiplied** `image.RGBA` bytes **without un-premultiplying** — `applyContrast`, `applyGrayscale`, `applySaturate`, `applySepia`, `applyInvert`, `applyHueRotate`, `applyBrightness` all read `pix[i]` directly. This is why `filter: contrast(100%)` on an opaque green box renders **solid black** (`filter-contrast-001_test.png`): the identity transform is only the identity in **non-premultiplied** space.
- never convert to **linearRGB** — the working space CSS filters and SVG filters require by default.
- have wrong **filter-region / buffer-extent** math (border-box only; blur padding ad-hoc; drop-shadow clipped to the element box so the shadow vanishes — `filters-drop-shadow-001_test.png` shows no shadow at all).
- `applyBackdropFilter` captures the wrong rectangle at the wrong time and produces nothing for elements with no own background (`backdrop-filter-basic_test.png` — the filterbox is invisible).
- ~~have **no SVG support whatsoever**~~ — **resolved by LOU-128**: inline `<svg>` now builds an
  `SVGRoot` + `SVGNode` tree via `pkg/layout/svg/`, `<filter>` is hosted by
  `pkg/layout/svg/svg_resource_filter.go`, `pkg/graphics/filters/svg_filter_builder.go` builds the
  FilterEffect graph, and `filter: url(#id)` on HTML now flows through `BuildReferenceFilter` in
  `pkg/render/filter_effect_builder.go`. Remaining `svg-*`/`fe*` failures are bucket-J primitive
  correctness, not infrastructure.

### Bucket breakdown (178 fails)

| # | Bucket | Fails | In scope | Root cause |
|---|--------|-------|----------|------------|
| A | CSS filter functions on the element (`filter: contrast/grayscale/invert/hue-rotate/saturate/sepia/brightness/opacity/blur`) | ~26 | Yes | No linearRGB working space; premultiplied-byte math; identity transforms not identity |
| B | `drop-shadow()` CSS function | ~9 | Yes | Shadow clipped to element box; offset/blur/extent math; `currentcolor` |
| C | `backdrop-filter` (all variants: basic, clip, border-radius, isolation, transform, plus-filter/mask/opacity, root, svg) | ~31 | Yes | No backdrop-root concept; wrong capture rect/timing; no clip to border-box+radius |
| D | `css-backdrop-filters-animation-*` (paused-animation reftests) | ~8 | Yes | Falls out of C once backdrop-filter is correct + animation sampling already works |
| E | `css-filters-animation-*` (paused-animation reftests) | ~11 | Yes | Falls out of A once CSS filter functions are correct |
| F | Filter as containing block / `filtered-inline-*` / `filter-cb-abspos-*` | ~3 | Yes | `filter != none` must establish a containing block for abs/fixed descendants; inline-filter must apply to floats |
| G | Filter region / units / clipping interaction (`filter-region-*`, `filter-units`, `fixed-pos-filter-clip`, `clip-under-filter`, `visibility-hidden-element-with-filter`) | ~10 | Yes | Filter region resolution; filter on `visibility:hidden`/empty/clipped elements |
| H | SVG `<filter>` reference pipeline — `url(#id)` to an SVG `<filter>` applied to an **HTML** element (`fecolormatrix-type`, `fecomposite-non-zero-inoffset`, `feflood-with-filter-reference`, `empty-element-with-filter*`, `filter-region-units`, `svg-multiple-filter-functions` partially, `effect-reference-*`, `blur-text`, `svg-external-filter-resource`, `filter-external-*`) | ~25 | Yes (large sub-project) | No SVG `<filter>` element model; no FilterEffect graph; no `fe*` primitives |
| I | SVG content rendering + filters on SVG elements (`svg-*`: `svg-mutation-*`, `svg-filter-vs-clip-path`, `svg-filter-vs-mask`, `svg-feimage-*`, `svg-visibility-hidden-*`, `svg-image-root-filter`, `svg-relative-urls`, `svg-shorthand-*`, `morphology-mirrored`) | ~32 | Yes (large sub-project) | No SVG layout/paint tree at all |
| J | SVG filter primitive correctness — `tainting-fe*` (31), `fe*` standalone, `feComposite-intersection-feTile` | ~33 | Yes (depends on H) | Each `fe*` primitive's algorithm + the tainting/cross-origin rules |

Buckets A, B, D, E, F, G (≈67 tests) are achievable by fixing the CSS-side painter and adding a proper FilterEffect graph for CSS filter functions. Buckets C (≈31) needs the backdrop-root machinery. Buckets H, I, J (≈90 tests) are a self-contained **SVG subsystem** sub-project — large but in scope and static.

### In-scope vs out-of-scope

- **In scope (≈176):** Every CSS filter function, `backdrop-filter`, the FilterEffect graph, linearRGB working space, the SVG `<filter>` element model and the `fe*` primitive pipeline, filter regions/units, and filter-as-containing-block. All are statically renderable.
- **Genuinely out of scope / defer with reason (≈2):**
  - `azimuth-and-elevation.html`, `limiting-cone-angle.html`, `lighting-region.html` — `feDiffuseLighting`/`feSpecularLighting` 3-D lighting model. The *tainting* variants (`tainting-fediffuselighting-*`, `tainting-fespecularlighting-*`) only need the primitive to **exist and not taint** (they match a solid-color ref), so they ARE in scope under bucket J with a minimal lighting implementation; the dedicated lighting-correctness reftests above are deferred until the rest of the category is green. Mark these explicitly; do not chase them early.
  - `filter-turbulence-invalid-001`, `effect-reference-displacement-negative-scale-001`, `kernel-unit-length-*` — `feTurbulence`/`feConvolveMatrix`/`feDisplacementMap` edge cases; in scope under J but lowest priority.

## Blink references this plan is grounded in

- `core/style/filter_operation.{h,cc}` — `FilterOperation` hierarchy and `OperationType` enum: `kReference, kGrayscale, kSepia, kSaturate, kHueRotate, kLuminanceToAlpha, kInvert, kOpacity, kBrightness, kContrast, kBlur, kDropShadow, kBoxReflect`. Subclasses: `BasicColorMatrixFilterOperation` (`double amount_`), `BasicComponentTransferFilterOperation` (`double amount_`, `AffectsOpacity()` true only for `kOpacity`), `BlurFilterOperation` (`LengthPoint std_deviation_`), `DropShadowFilterOperation` (`ShadowData shadow_`), `ReferenceFilterOperation` (`AtomicString url_`, `Member<SVGResource> resource_`, `Member<Filter> filter_`).
- `core/paint/filter_effect_builder.{h,cc}` — `FilterEffectBuilder`. Ctor: `(const gfx::RectF& reference_box, std::optional<gfx::SizeF> viewport, float zoom, Color current_color, mojom::ColorScheme, const cc::PaintFlags* fill, const cc::PaintFlags* stroke)`. Key methods: `BuildFilterEffect(const FilterOperations&, bool input_tainted)`, `BuildReferenceFilter(const ReferenceFilterOperation&, FilterEffect* previous, SVGFilterGraphNodeMap*)`, `BuildFilterOperations(...)`, `SetShorthandScale(float)`. `BuildFilterEffect` switches on `FilterOperation::GetType()`: grayscale/sepia → `FEColorMatrix` `MATRIX`; saturate → `FEColorMatrix` `SATURATE`; hue-rotate → `FEColorMatrix` `HUEROTATE`; invert/opacity/brightness/contrast → `FEComponentTransfer`; blur → `FEGaussianBlur`; drop-shadow → `FEDropShadow`. **CSS shorthand filters run in `kInterpolationSpaceSRGB` and do not clip to a primitive subregion.**
- `platform/graphics/filters/filter_effect.{h,cc}` — `FilterEffect` base class. `input_effects_` (upstream deps), `filter_primitive_subregion_`, `clips_to_bounds_`, `operating_interpolation_space_`, `image_filters_` cache. Methods: `MapRect()` (forward bounds mapping), `CreateImageFilter()`, `AbsoluteBounds()`, `AffectsTransparentPixels()`, `InputEffect()`, `MapInputs()`/`MapEffect()`, `AdaptColorToOperatingInterpolationSpace()`.
- `platform/graphics/filters/filter.{h,cc}` — `Filter` owns the graph, the `reference_box_`, `filter_region_`, `Scale()`, the `SourceGraphic`/`SourceAlpha` builtins, and `MapAbsolutePointToLocalPoint`.
- `platform/graphics/filters/fe_color_matrix.cc` — exact `SATURATE` coefficients for value `s`: row0 `[0.213+0.787s, 0.715-0.715s, 0.072-0.072s, 0, 0]`, row1 `[0.213-0.213s, 0.715+0.285s, 0.072-0.072s, 0, 0]`, row2 `[0.213-0.213s, 0.715-0.715s, 0.072+0.928s, 0, 0]`, rows 3-4 identity. `HUEROTATE`: `cos`/`sin` of the angle composed with the same luminance basis (the spec matrix already in `applyHueRotate`). Uses **premultiplied** pixel input and applies via `ColorFilterPaintFilter` in the operating interpolation space.
- `platform/graphics/filters/{fe_gaussian_blur,fe_drop_shadow,fe_component_transfer,fe_composite,fe_merge,fe_offset,fe_flood,fe_blend,fe_morphology,fe_tile,fe_displacement_map,fe_image,fe_turbulence,fe_convolve_matrix,fe_diffuse_lighting,fe_specular_lighting}.cc` — one class per primitive; each implements `CreateImageFilter()` and `MapRect()`.
- `core/svg/graphics/filters/svg_filter_builder.{h,cc}` — `SVGFilterBuilder::BuildGraph()` iterates `<filter>` children implementing `IsFilterEffect()`, calls each element's `Build()`, registers in the node map, applies standard attributes (subregion, units, viewport). `GetEffectById()` resolution order: builtins (`SourceGraphic`, `SourceAlpha`) → named `result` effects → last effect → fallback `SourceGraphic`. `color-interpolation-filters` read per-element from style/presentation attribute → `SetOperatingInterpolationSpace(linearRGB|sRGB)`.
- `core/svg/svg_filter_element.h`, `core/svg/svg_fe_*_element.{h,cc}` — the SVG `<filter>` and `fe*` element classes; `filterUnits`/`primitiveUnits` (`objectBoundingBox` default for filter region, `userSpaceOnUse` for primitives), `x/y/width/height` defaults `-10% -10% 120% 120%`.
- `core/paint/filter_painter.cc` + `core/paint/object_paint_properties` (`FilterPaintPropertyNode`) — filters are a paint-property effect node; `FilterPainter` records the source into a `PaintRecord`, wraps it in the filter's `cc::PaintFilter`, and replays. The filter establishes a layer.
- `core/paint/object_paint_properties` — `filter != none` adds a transform/effect node and makes the element a containing block for fixed/abs descendants (bucket F).
- Spec: **Filter Effects 1 §13** ([w3.org/TR/filter-effects-1/#supported-filter-functions](https://www.w3.org/TR/filter-effects-1/#supported-filter-functions)) — CSS shorthand → primitive equivalences; **Filter Effects 2** ([drafts.fxtf.org/filter-effects-2](https://drafts.fxtf.org/filter-effects-2/)) — backdrop-root definition and the backdrop-filter rendering algorithm; **§"tainted filter primitives"** — the tainting rules for bucket J.
  - grayscale(a): `FEColorMatrix MATRIX`, each row blends the luminance vector `(0.2126, 0.7152, 0.0722)` toward identity by `(1-a)`.
  - sepia(a): `FEColorMatrix MATRIX`, row blends toward identity by `(1-a)` of the fixed sepia basis (`0.393 0.769 0.189` / `0.349 0.686 0.168` / `0.272 0.534 0.131`).
  - saturate(s): `FEColorMatrix SATURATE` (coefficients above).
  - hue-rotate(θ): `FEColorMatrix HUEROTATE`.
  - invert(a): `FEComponentTransfer` `type="table" tableValues="a 1-a"` on R,G,B.
  - opacity(a): `FEComponentTransfer` `type="table" tableValues="0 a"` on **A** only.
  - brightness(a): `FEComponentTransfer` `type="linear" slope="a" intercept="0"` on R,G,B.
  - contrast(a): `FEComponentTransfer` `type="linear" slope="a" intercept="-(0.5*a)+0.5"` on R,G,B.
  - blur(r): `FEGaussianBlur` with `stdDeviation = r` (note: **σ = r**, not `r/2` — the existing `sigma := f.Value / 2` in `render.go:664,717` is a bug).
  - drop-shadow(dx dy r color): `FEDropShadow` = `FEGaussianBlur(σ=r)` of `SourceAlpha`, `FEOffset(dx,dy)`, `FEFlood(color)` composited `in`, then `FEMerge` of that under `SourceGraphic`.

## louis14 target files

- `pkg/css/style.go` — `FilterFunction` struct (`style.go:7907`), `GetFilter` (`:7920`), `GetBackdropFilter` (`:8281`). Parsing exists but is lossy (blur σ, hue-rotate units, drop-shadow color/`currentcolor`). Extend, don't rewrite.
- `pkg/render/render.go` — `paintLayerWithFilter` (`:657`), `applyBackdropFilter` (`:754`), and the `applyGrayscale`/`applyBrightness`/`applyContrast`/`applyFilterOpacity`/`applySaturate`/`applySepia`/`applyInvert`/`applyHueRotate`/`applyDropShadow` family (`:976`–`:1170`), `boxBlur` (`:4587`). Dispatch at `render.go:474`.
- `pkg/render/paint_layer.go` — `PaintLayer.Filters/HasFilter/BackdropFilters/HasBackdropFilter` (`:173`–`:179`), populated at `:624`–`:635`.
- **New package `pkg/graphics/filters/`** (mirrors Blink `platform/graphics/filters/`) — the FilterEffect graph: `filter.go`, `filter_effect.go`, `fe_color_matrix.go`, `fe_component_transfer.go`, `fe_gaussian_blur.go`, `fe_drop_shadow.go`, `fe_offset.go`, `fe_flood.go`, `fe_merge.go`, `fe_composite.go`, `fe_blend.go`, `fe_morphology.go`, `fe_tile.go`, `fe_displacement_map.go`, `fe_image.go`, `fe_turbulence.go`, `fe_convolve_matrix.go`, `fe_lighting.go`, `source_graphic.go`.
- **New file `pkg/render/filter_effect_builder.go`** (mirrors Blink `core/paint/filter_effect_builder.cc`) — `FilterEffectBuilder` turning `[]css.FilterFunction` and `url()` references into a `filters.FilterEffect` graph.
- `pkg/layout/` — SVG layout: `pkg/layout/svg_layout.go` (new) for the SVG render/layout tree; `layout_tree_builder.go` (`:211` tag switch) and `intrinsic_sizing.go:27` need an SVG branch. `pkg/css/style.go` needs SVG presentation-attribute and `color-interpolation-filters` plumbing.
- `pkg/layout/types.go` / `paint_layer.go` — `LayoutInputNode` / `PaintLayer` need an `IsContainingBlockForFixed/Abs` flag set when `filter != none` (bucket F), and SVG node fields.

---

## What LOU-128 (SVG render-subsystem foundation) replaces

LOU-128 landed the inline-SVG render tree, paint walk, resource registry, and the
SVG `<filter>` element host. From this plan's perspective:

- **Phase 7 (bucket I, ~32 tests) — REPLACED.** LOU-128 *is* this plan's SVG render
  tree: `pkg/layout/svg/{svg_root,svg_node,svg_container,svg_shape,svg_path,
  svg_length_context,viewbox,transform_helper}.go` plus the paint-side
  `pkg/render/svg_{root,container,shape,object,paint_*}_painter.go`. Paint servers
  (gradients + pattern) and clip-path/mask resources are in
  `pkg/layout/svg/svg_resource_*.go` + their painters. Bucket I's remaining work
  is per-test fuzz / interaction order (`svg-filter-vs-clip-path`,
  `svg-filter-vs-mask`); the structural infrastructure is in place.
- **Phase 6 (bucket H, ~25 tests) — REPLACED at the element-model layer.**
  `pkg/layout/svg/svg_resource_filter.go` is the `<filter>` element host;
  `pkg/graphics/filters/svg_filter_builder.go` has `SVGFilterBuilder.BuildGraph(...)`
  + `getEffectByID` + `ResolveInterpolationSpace`; `pkg/render/filter_effect_builder.go`
  has `BuildReferenceFilter` for `filter: url(#id)` on HTML elements. What remains
  here: `FilterRegion` resolution + external/`data:` URL fetching (bucket H's
  region-and-URL math, not the element model).
- **`pkg/graphics/filters/` additions from LOU-128 (3 new files):**
  - `svg_filter_builder.go` — `SVGFilterBuilder.BuildGraph(filterElem, source) *Filter`,
    Blink-grounded `getEffectByID` (builtins → named `result` → last → fallback
    `SourceGraphic`), `ResolveInterpolationSpace` per-`fe*` element.
  - `fe_blend.go` — `FEBlend` primitive: normal / multiply / screen / darken / lighten
    modes. The other Blink modes are still bucket J residue.
  - `fe_subregion_clip.go` — generic per-primitive subregion-clip wrapper used by the
    builder when a primitive declares `x`/`y`/`width`/`height`.

### Carry-over gaps from LOU-128 — work for bucket J (Phase 8) and bucket H

- **`FillPaint` / `StrokePaint` / `BackgroundImage` SVG filter builtins** are aliased
  to `SourceGraphic` / `SourceAlpha` in LOU-128's filter builder. Proper
  implementations belong here in bucket J. Look for the alias in
  `pkg/graphics/filters/svg_filter_builder.go` `getEffectByID`.
- **Filter-element resolution is wired** (LOU-128 Phase 7's `BuildReferenceFilter`).
  Bucket J's primitive correctness work plugs into the existing graph — do not
  re-wire dispatch; add/replace `fe*` primitive implementations in
  `pkg/graphics/filters/`.
- **Color-convention compatibility shim** in `pkg/render/svg_mask_painter.go::compositeBufferWithOpacityOnto`
  is a project-wide straight-alpha convention; not a filter concern but worth
  knowing if/when filter+mask composite paths cross. Project-wide cleanup is a
  separate ticket.
- **Content-based clipper fallback** in `pkg/render/svg_clip_painter.go` uses a
  bounding-rect approximation because mazarin currently lacks a generic alpha-mask
  clip. Affects only `clip-path` referencing non-`<clipPath>` SVG content; not on
  bucket H/I/J's critical path.
- **`SVGResourceReference` type** exists in `pkg/layout/svg/svg_resource.go` but is
  not plumbed at call sites yet. Sites use direct `LookupClipper`/`LookupMasker`/
  `LookupFilter`/`LookupPaintServer`. Plumbing it is a clean follow-up; not a
  blocker for bucket J or the rest of bucket H.

## Phases (foundational-first)

### Phase 0: Baseline & categorization — **DONE**
- [x] Full FAIL list from `docs/reftest-survey-2026-05-14-raw.txt`.
- [x] Read ~30 failing tests' HTML + `-ref` across all buckets; read `output/reftests/*_diff/_test/_ref.png` for buckets A, B, C, H, J.
- [x] Bucket by root cause; identify the structural gap (no FilterEffect graph, no SVG subsystem).
- [x] This file.

### Phase 1: linearRGB working space + premultiplied-alpha helpers — **foundational, fixes nothing alone**
**Goal.** A correct color pipeline that every later phase builds on. No test passes from this phase in isolation; it is the prerequisite for Phases 2–8.

**Blink reference.** `FilterEffect::operating_interpolation_space_` and `AdaptColorToOperatingInterpolationSpace`; `platform/graphics/color.h` sRGB↔linearRGB transfer functions; `SkColorFilter` semantics — color filters run on **premultiplied** pixels but each effect first un-premultiplies, transforms in its working space, re-premultiplies.

**louis14 target.** New `pkg/graphics/filters/colorspace.go`.

**New types / functions.**
- `type InterpolationSpace int` — `InterpolationSpaceSRGB`, `InterpolationSpaceLinearRGB`.
- `srgbToLinear(c float64) float64`, `linearToSRGB(c float64) float64` — the exact piecewise transfer (`0.04045` / `2.4` gamma), with 256-entry lookup tables for speed.
- `unpremultiply(px [4]uint8) (r,g,b,a float64)` / `premultiply(r,g,b,a float64) [4]uint8` — operating on `[0,1]` floats.
- `convertImageSpace(img *image.RGBA, from, to InterpolationSpace)` — bulk per-pixel un-premult → transfer → re-premult; no-op when `from==to`.

**Approach.** Pure helpers, fully unit-testable. Establish the invariant: **every filter primitive un-premultiplies, works in its declared space, re-premultiplies.** This single discipline is what makes `contrast(100%)` an identity (currently it is not — `filter-contrast-001` is black).

**Gate metric.** New `pkg/graphics/filters/colorspace_test.go` round-trips sRGB↔linear and premult↔unpremult to <1 ULP; identity-matrix `FEColorMatrix` (Phase 2) leaves an opaque pixel unchanged. No reftest delta yet.

### Phase 2: FilterEffect graph + CSS filter functions (bucket A, E) — **~37 tests**
**Goal.** `filter: grayscale/sepia/saturate/hue-rotate/invert/opacity/brightness/contrast/blur(...)` on an HTML element renders pixel-correct, including all "100% / 0 = identity" tests and the paused-animation reftests.

**Blink reference.** `FilterEffectBuilder::BuildFilterEffect` switch; `FEColorMatrix` (`MATRIX`/`SATURATE`/`HUEROTATE`), `FEComponentTransfer` (`TABLE`/`LINEAR`), `FEGaussianBlur`. CSS shorthand filters run in **sRGB** and do **not** clip to a primitive subregion (`filter_effect_builder.cc`). Spec §13 equivalences (listed above).

**louis14 target.** New `pkg/graphics/filters/{filter.go,filter_effect.go,fe_color_matrix.go,fe_component_transfer.go,fe_gaussian_blur.go,source_graphic.go}`; new `pkg/render/filter_effect_builder.go`; rewrite `paintLayerWithFilter` in `render.go:657`; delete the byte-loop `apply*` functions (`render.go:976`–`1107`).

**New types.**
- `filters.FilterEffect` interface — `ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA`, `MapRect(image.Rectangle) image.Rectangle`, `InputEffects() []FilterEffect`, `OperatingSpace() InterpolationSpace`.
- `filters.Filter` — owns the graph root, the `referenceBox image.Rectangle`, `filterRegion`, the `SourceGraphic`/`SourceAlpha` source effects.
- `filters.FEColorMatrix{Type MatrixType; Values [20]float64}` — `MatrixTypeMatrix/Saturate/HueRotate/LuminanceToAlpha`.
- `filters.FEComponentTransfer{Funcs [4]TransferFunc}` — `TransferFunc{Type: Identity/Table/Discrete/Linear/Gamma; TableValues []float64; Slope,Intercept,Amplitude,Exponent,Offset float64}`.
- `filters.FEGaussianBlur{StdDevX, StdDevY float64}` — three-pass box blur approximating a true Gaussian (Blink's `MakeBoxBlur` does the same; box radius `d` from σ per the SVG spec `d = floor(σ*3*sqrt(2π)/4 + 0.5)`).
- `render.FilterEffectBuilder` — `BuildFilterEffect(ops []css.FilterFunction, referenceBox image.Rectangle, currentColor css.Color) *filters.Filter`.

**Approach.**
1. `FilterEffectBuilder.BuildFilterEffect` walks `[]css.FilterFunction` (chained — each effect's input is the previous), emitting the Blink-mapped effect per function. grayscale/sepia build the §13 blend-toward-identity matrix; saturate/hue-rotate use `SATURATE`/`HUEROTATE`; invert/opacity/brightness/contrast build the `FEComponentTransfer` table/linear params; blur builds `FEGaussianBlur` with **σ = radius** (fix the `/2` bug).
2. `FEColorMatrix.ApplyEffect` and `FEComponentTransfer.ApplyEffect` run in **sRGB** for CSS shorthand: un-premult → matrix/transfer → re-premult (Phase 1 helpers). The matrix is applied to non-premultiplied RGBA; `opacity()` touches only A.
3. Rewrite `paintLayerWithFilter`: render the subtree to an offscreen buffer sized to the **filter's mapped region** (`Filter.MapRect` over the reference box — for blur this is the box inflated by ~`3σ`, computed by the graph, not the ad-hoc `blurExtend`), run the graph, composite the graph output back at the correct origin.
4. Fix `GetFilter` parsing: `hue-rotate` angle units already handled; ensure unitless `grayscale(0)` vs `grayscale(0%)` both parse; `opacity`/`brightness`/`contrast`/`saturate` accept number-or-percentage.

**Tests fixed.** Bucket A (~26): `filter-contrast-001/002/003`, `filter-grayscale-002/003/004/005`, `filter-invert-001/002-test`, `filter-hue_rotate-001/002-test`, `filter-saturate-001-test`, `filters-grayscale-001-test`, `filters-opacity-001-test`, `filters-sepia-001-test`, `filters-test-brightness-001/002/003`, `filtered-inline-applies-to-float` (the blur on the inline). Bucket E (~11): `css-filters-animation-{blur,brightness,contrast,grayscale,hue-rotate,invert,opacity,saturate,sepia,combined-001}`, plus `dynamic-filter-changes-001`, `remove-filter-repaint`, `background-image-blur-repaint`, `blur-text` partially (`blur-text` needs Phase 6's data-URL SVG filter).

**Gate metric.** Bucket A all 0% diff; ≥9/11 bucket E. No regression in css-backgrounds / compositing.

### Phase 3: drop-shadow + filter region/extent correctness (bucket B, G) — **~19 tests**
**Goal.** `drop-shadow()` casts a correct, un-clipped shadow; filters on `visibility:hidden`/empty/clipped/out-of-view elements behave correctly; the filter region resolves correctly.

**Blink reference.** `FEDropShadow` = `FEGaussianBlur(SourceAlpha, σ)` → `FEOffset(dx,dy)` → flood-`in` with `FEFlood(color)` → `FEMerge{shadow, SourceGraphic}`. `FEOffset`, `FEFlood`, `FEMerge`. `FilterEffect::MapRect`/`AbsoluteBounds` — the offscreen buffer must cover the **union of all primitive output rects**, not the element box; `clips_to_bounds_=false` for shorthand filters. `Filter::filter_region_` and the `-10%/120%` default region for `url()` filters (Phase 6).

**louis14 target.** New `pkg/graphics/filters/{fe_drop_shadow.go,fe_offset.go,fe_flood.go,fe_merge.go}`; the buffer-extent rewrite in `paintLayerWithFilter`; `paint_layer.go` extent fields.

**New types.**
- `filters.FEDropShadow{DX, DY, StdDev float64; ShadowColor css.Color}` — built as the composite graph above, not a single pass.
- `filters.FEOffset{DX, DY float64}`, `filters.FEFlood{Color css.Color}`, `filters.FEMerge{}` (multi-input).
- `filters.SourceAlpha` source effect (zeroes RGB, keeps A).

**Approach.**
1. Implement `FEDropShadow` as a real sub-graph so the shadow is computed from `SourceAlpha`, blurred, offset, flooded, and merged **under** `SourceGraphic`. Default shadow color = `currentColor` when omitted (not black — fix `applyDropShadow`'s `render.go:1117` black default; wire `currentColor` from the element's `color`).
2. `MapRect` on each effect so `paintLayerWithFilter`'s buffer = the element box unioned with the offset+blurred shadow rect. `drop-shadow-clipped-001` (shadow at `-105px`), `filters-drop-shadow-003` (element at `-1000px`, shadow `+1000px`) prove the buffer must not be the element box.
3. `filters-drop-shadow-002`: a `drop-shadow` container with a `border-radius:overflow:hidden` child — the clip applies to the child *before* the filter sees it; ensure `paintLayerContent` runs the child's overflow clip inside the offscreen buffer.
4. Bucket G: an element with `filter` that is `visibility:hidden` still must not paint its own content, but a `url()` filter (`feFlood`) still produces output — handled in Phase 6; for CSS-function filters on hidden/empty elements the buffer is empty → output empty. `clip-under-filter-*`, `fixed-pos-filter-clip-002`: the filter is applied, *then* the result is clipped by ancestors — composite order. `filter-region-*` for CSS functions: region = mapped bounds.

**Tests fixed.** Bucket B (~9): `filters-drop-shadow-001/002/003`, `drop-shadow-clipped-001`, `drop-shadow-currentcolor-dynamic-001/002/003`, `drop-shadow-with-3d-transform`, `css-filters-animation-drop-shadow`. Bucket G CSS-side: `clip-under-filter-003`, `fixed-pos-filter-clip-002`, `visibility-hidden-element-with-filter-001` (CSS-func part).

**Gate metric.** Bucket B all 0% diff. No regression.

### Phase 4: filter establishes a containing block (bucket F) — **~3 tests + correctness for many**
**Goal.** `filter != none` (including `blur(0px)`, `brightness(100%)`) makes the element a containing block for `position:absolute`/`fixed` descendants, except on the document root; a filtered **inline** is a containing block and applies to floats.

**Blink reference.** `core/style/computed_style.cc` `HasFilterInducingProperty` → `LayoutObject::CanContainAbsolutePositionObjects/CanContainFixedPositionObjects`; `core/layout/layout_object.cc` containing-block walk. Spec: Filter Effects 1 §"The filter property" — "A value other than `none` … results in the creation of a containing block …".

**louis14 target.** `pkg/layout/out_of_flow_layout.go`, `pkg/layout/layout_input_node.go` (the abs/fixed containing-block resolution — `filter` is already in the cascade list at `pkg/css/cascade.go:805`), `pkg/layout/layout_tree_builder.go`.

**New types.** `LayoutInputNode.EstablishesContainingBlockForAbsPos/ForFixed bool` — set true when `GetFilter() != nil` and the node is not the root element.

**Approach.** In the OOF containing-block search, treat a filtered element as the containing block (already partially wired via `cascade.go:805` preserving `filter` onto inline-split anonymous boxes — verify inline filters split correctly and the float-application path covers `filtered-inline-applies-to-float`/`filtered-inline-is-container`). The document-root exception: a filter on `<html>` does **not** create a CB (`root-element-with-opacity-filter-001` — note that test is mainly about *painting* the root with `opacity(0.501)`; ensure root-element filter compositing works).

**Tests fixed.** `filter-cb-abspos-inline-001`, `filtered-inline-is-container`, `filtered-inline-applies-to-float` (layout half — paint half from Phase 2), `root-element-with-opacity-filter-001`.

**Gate metric.** Bucket F 0% diff; no regression in css-position / css-will-change abs/fixed-CB tests.

### Phase 5: backdrop-filter + backdrop-root (bucket C, D) — **~39 tests**
**Goal.** `backdrop-filter` renders per the Filter Effects 2 algorithm: capture the backdrop image, filter it, clip to the element's border-box (incl. `border-radius`), composite under the element's own content.

**Blink reference.** Filter Effects 2 — **backdrop root** is formed by: the document root; `filter != none`; `opacity < 1`; `mask`/`mask-image`/`mask-border`/`clip-path != none`; `backdrop-filter != none`; `will-change` of any of those. **Not** formed by `z-index`, fixed/sticky position, or `transform`. The algorithm: copy the *backdrop root image* into buffer `T'`; apply the filter to all of `T'`; apply inverse of any transforms between the element and the backdrop root; clip `T'` to the element's border box including `border-radius`; composite. `isolation: isolate` does **not** create a backdrop root (`backdrop-filter-isolation-isolate` matches the non-isolation ref). Blink: `core/paint/object_paint_properties` `BackdropFilter` effect node, `PaintLayer::SetNeedsCompositingInputsUpdate`, `cc` `BackdropFilterMask`.

**louis14 target.** Rewrite `applyBackdropFilter` (`render.go:754`); the dispatch point (`render.go:1263` inside `paintLayerContent`, before `paintSelfDecorations`); new `PaintLayer.IsBackdropRoot bool` in `paint_layer.go`; `paint_layer.go:631`–`635` already populates `BackdropFilters`.

**New types.** `PaintLayer.IsBackdropRoot bool` (computed from the conditions above). Reuse the Phase 2/3 `filters.Filter` graph — `backdrop-filter` accepts the same filter-function list (`GetBackdropFilter` at `style.go:8281`).

**Approach.**
1. Compute backdrop-root membership when building the layer tree. Track, during paint, the *current backdrop root's accumulated canvas region* — the backdrop image is "everything painted into the backdrop root so far, below this element."
2. Rewrite `applyBackdropFilter`: (a) snapshot the backdrop-root buffer region under the element's border-box; (b) run the **full filter graph** (reuse `FilterEffectBuilder`) over that snapshot — fixes the per-pixel byte loops at `render.go:790`–`819` which share bucket A's premultiplied-alpha bug; (c) clip the filtered result to the element's border box + `border-radius` (`backdrop-filter-clip-rounded-clip`, `backdrop-filter-border-radius-change`, `backdrop-filter-clip-radius-zoom`); (d) draw it, then paint the element's own decorations/content on top. Current code captures from `r.target` at the wrong time and skips the border-radius clip.
3. Handle `backdrop-filter` + `opacity`/`mask`/`mix-blend-mode`/`filter` on the same element (`backdrop-filter-plus-*`), `backdrop-filter` under transforms (`backdrop-filter-transform`, `backdrop-filter-scale-transform` — inverse-transform `T'`), fixed/abs positioning, and the root element (`backdrop-filter-root-element`).
4. `backdrop-filter-reference-filter` / `backdrop-filter-svg*` depend on Phase 6 (`url()` filters) — defer those few to after Phase 6.

**Tests fixed.** Bucket C (~31, minus the ~3 `url()`/svg ones): `backdrop-filter-basic*`, `backdrop-filters-{grayscale,brightness,contrast,invert,opacity,saturate,sepia,hue-rotate}*`, `backdrop-filter-{clip-*,border-radius-change,box-shadow,containing-block,edge-behavior,fixed-clip,isolation-*,opacity-rounded-clip,plus-filter,plus-will-change-opacity,transform,inline-positioning,image-size-filter-size-mismatch}`, `backdrop-filter-backdrop-root-{opacity,mask,mix-blend-mode,clip-path,clip-path-2}`, `css-backdrop-filter-transform-clip`, `repaint-added-backdrop-filter`. Bucket D (~8): `css-backdrop-filters-animation-{brightness,contrast,grayscale,invert,opacity,saturate,sepia,combined}`.

**Gate metric.** ≥36/39 buckets C+D at 0% diff (remaining 3 are `url()`-dependent → Phase 6+); no regression in compositing / css-will-change.

### Phase 6: SVG `<filter>` element model + reference filters on HTML (bucket H) — **~25 tests, large sub-project**
**Status (2026-05-18):** **Element-model layer REPLACED by LOU-128; FilterRegion + external-URL fetch CLOSED by LOU-130 (master `03f014a5`).** Residual per-test failures carved into separate tickets (see below).

**Goal (residue).** Make `filter: url(#id)` on HTML elements produce correct output
for the full bucket-H test set: resolve the filter region (`filterUnits`,
`primitiveUnits`, `-10%/120%` defaults), fetch external/data-URL SVG, and handle
the `empty-element` / `visibility-hidden` interaction with `feFlood`-style filters
that produce output even when the source graphic is empty.

**What LOU-128 already provides (do not duplicate):**
- `pkg/graphics/filters/svg_filter_builder.go` — `SVGFilterBuilder.BuildGraph`,
  `getEffectByID` (builtins → named → last → fallback `SourceGraphic`),
  `ResolveInterpolationSpace` per-`fe*` element.
- `pkg/render/filter_effect_builder.go` — `BuildReferenceFilter` for `filter: url(#id)`.
- `pkg/layout/svg/svg_resource_filter.go` — the `<filter>` element model.
- `pkg/css/style.go` SVG plumbing: `color-interpolation-filters`, `flood-color`,
  `flood-opacity`, `SVGPaint`, `ReferenceFilterOperation`, `ClipPathReference`,
  `ParseURLReference`.
- `pkg/css/cascade.go::applyPresentationalAttributes` — SVG presentation attrs.

**Residual work for this phase — landed under LOU-130:**

- `filters.FilterRegion` resolution — **VERIFIED CORRECT** by LOU-130 Phase 1 falsification tests (master `aaeb5ade`). `pkg/layout/svg/svg_resource_filter.go::ResourceBoundingBox` already implements the `-10%/120%` defaults end-to-end in both `objectBoundingBox` and `userSpaceOnUse` modes; explicit attrs correctly override. 3 unit tests committed as regression coverage.
- External / `data:` URL SVG fetching — **LANDED** by LOU-130 Phase 2 (master `03f014a5`). New `std/net` helpers + `pkg/layout/svg/svg_document_fetcher.go::SVGDocumentFetcher` mirror Blink's `ExternalSVGResourceDocumentContent::Load`. Closes `filter-external-002-test` (8.3%→0) and `svg-external-filter-resource` (2.1%→0).

**Citations re-pinned 2026-05-18 to chromium-main SHA `bf955d02bf0b0c67868b2e62359c0af199af9acc`:**
- `core/svg/svg_filter_element.cc:47-75` — ctor defaults (`kPercentMinus10`/`kPercent120`).
- `core/svg/svg_filter_element.h:44-55` — element accessors.
- `core/svg/svg_length.h:57,61` — `kPercentMinus10`/`kPercent120` constants.
- `core/layout/svg/layout_svg_resource_filter.cc:44-48` — `ResourceBoundingBox` + `ResolveRectangle`.
- `core/svg/svg_resource.cc:227-283` — `ExternalSVGResourceDocumentContent::Load` (line 235: `ScriptForbiddenScope` (N/A); 256-257: cross-origin CORS attr; 272: `SVGResourceDocumentContent::Fetch`).

**Remaining Phase-6-area failures (carved into separate tickets):**

- ~~`svg-empty-container-with-filter-content-added` (2.1%): JS shim missing `waitForAtLeastOneFrame`~~ — **CLOSED** by [LOU-133](https://linear.app/mazarin/issue/LOU-133/) (master `a6d3805e`). Added `waitForAtLeastOneFrame` + `takeScreenshot` WPT prelude + `document.createElementNS` in `pkg/js/`. Test PASS @ 0% diff.
- `svg-empty-hidden-foreignobject-with-filter-001` (2.1%): no `<foreignObject>` support + filter painters bail on empty userBBox — [LOU-134](https://linear.app/mazarin/issue/LOU-134/).
- `svg-multiple-filter-functions` (2.1%): mixed `url(...)+CSS-shorthand` filter list — `filter_effect_builder.go` explicitly skips `url()` in mixed lists (deferred FilterChain merge) — [LOU-135](https://linear.app/mazarin/issue/LOU-135/).
- `blur-text` (13.2%): inline elements with `filter:` don't promote to paint layer; `BuildReferenceFilter` never invoked — [LOU-136](https://linear.app/mazarin/issue/LOU-136/).
- ~~`svg-relative-urls-001` (2.1% post-Phase-2; was 4.3%): JS shim missing `iframe.contentDocument` + cross-document `appendChild`~~ — **CLOSED** by [LOU-137](https://linear.app/mazarin/issue/LOU-137/). Real blocker was URL pre-baking inside iframe content surviving the JS cross-doc move; fix mirrors Blink `Document::adoptNode` by tagging iframe descendants with `IframeBase` and reversing the URL pre-bake on cross-doc `appendChild`. Test PASS @ 0% diff; no regression on `svg-relative-urls-002` or `bidi-dynamic-iframe-001`.
- `FillPaint`/`StrokePaint` filter builtins still aliased to `SourceGraphic`/`SourceAlpha` from LOU-128 — not surfaced by any current bucket-H/J failure; leave the alias until a test exercises it.

**Gate metric.** ≥22/25 bucket H at 0% diff; FilterEffect graph proven on real `fe*` chains.

### Phase 7: SVG layout & paint tree + filters on SVG elements (bucket I) — **REPLACED by LOU-128**
**Status (2026-05-16):** **REPLACED in full by LOU-128 Phases 1–7.**

This phase as originally written *is* what LOU-128 built. See
`docs/plan-svg-foundation.md` for the landed scope; this phase no longer carries
work for this plan. The remaining bucket-I residue is per-test
correctness/interaction-order:

- `svg-filter-vs-clip-path`, `svg-filter-vs-mask` — exercise the paint-walk
  ordering between filter, clip, and mask. LOU-128's SVG paint walk in
  `pkg/render/svg_*_painter.go` lays out the ordering; any test-driven correction
  belongs here.
- `svg-mutation-*`, `svg-visibility-hidden-*`, `svg-relative-urls-001`,
  `svg-shorthand-{drop-shadow,hue-rotate}-001`, `svg-multiple-filter-functions`,
  `svg-empty-*`, `svg-sourcegraphic-*`, `svg-external-filter-resource`,
  `svg-filter-{filter,primitive}-units-user-space`, `morphology-mirrored`,
  `svg-feimage-{001..005}` — bucket-J primitive correctness and/or filter-region
  resolution (Phase 6 residue above).

Bucket I tests confirmed PASS post-LOU-128 (sample): `svg-feflood-001.html` @ 0,
`filter-subregion-01.html` @ 0, `svg-transform-animation.html` @ 0.

**Gate metric (revised).** ≥28/32 bucket I at 0% diff, achieved by completing
Phase 6 residue + Phase 8 primitive correctness; no SVG-infrastructure work
remains here.

### Phase 8: SVG filter primitive correctness — feComposite/feMerge/feMorphology/feTile/feDisplacementMap/feBlend/feImage/feTurbulence/feConvolveMatrix/lighting + tainting (bucket J) — **~33 tests**
**Goal.** Every `fe*` primitive is algorithmically correct, and the tainting/cross-origin rules are enforced so `feDisplacementMap` (and friends) reject tainted inputs.

**Blink reference.** `platform/graphics/filters/{fe_composite,fe_merge,fe_morphology,fe_tile,fe_displacement_map,fe_blend,fe_image,fe_turbulence,fe_convolve_matrix,fe_diffuse_lighting,fe_specular_lighting}.cc`; Filter Effects 1 §"tainted filter primitives" and §"feDisplacementMap restrictions" — a primitive is *tainted* if it consumes a cross-origin image or (for `feImage`/`feTile` etc.) tainted input; `feDisplacementMap` with a tainted `in2` produces transparent black. The `tainting-fe*` reftests verify each primitive **does not taint** when fed a same-origin gradient/flood — i.e. the primitive must exist, be correct, and propagate the (un)tainted flag.

**louis14 target.** Complete the `pkg/graphics/filters/fe_*.go` set; a `tainted bool` field on `filters.FilterEffect` results; `filters.FEDisplacementMap` honors it.

**New types.** `FEComposite` (already from Phase 6), `FEMerge` (already), `FEMorphology{Operator: Erode/Dilate; RadiusX, RadiusY float64}`, `FETile`, `FEDisplacementMap{ScaleX, ScaleY float64; XChannel, YChannel ChannelSelector}`, `FEBlend{Mode}`, `FEImage`, `FETurbulence{Type, BaseFreqX, BaseFreqY, NumOctaves, Seed, Stitch}`, `FEConvolveMatrix`, `FEDiffuseLighting`/`FESpecularLighting` (minimal — enough for the `tainting-*` solid-color refs). Each carries/propagates `tainted`.

**Approach.** Implement each primitive's algorithm against its Blink `.cc`. Thread a `tainted` flag through the graph (set by cross-origin `feImage`; consumed by `feDisplacementMap`). The 31 `tainting-fe*` tests mostly need: primitive exists + correct + does not taint same-origin input → matches a `green-blue-stripe`/solid ref. `feComposite-intersection-feTile-input*` exercise `feTile` + `feComposite operator="in"`. The lighting-correctness reftests (`azimuth-and-elevation`, `limiting-cone-angle`, `lighting-region`, `kernel-unit-length-*`) and `feTurbulence`/`feConvolveMatrix`/`feDisplacementMap` edge cases are the **lowest priority** — finish them last; if any prove impractical for the static renderer, mark them deferred with a one-line reason in this file's residuals section, but expect them to pass.

**Tests fixed.** Bucket J (~33): all `tainting-fe*` (31), `feComposite-intersection-feTile-input{,-svg}`.

**Gate metric.** ≥31/33 bucket J at 0% diff. Full filter-effects category ≥176/178 at 0% diff.

### Phase 9: Delivery & regression audit
- [ ] Single sanctioned full-category run of `TestWPTCSS3Reftests/filter-effects/`; record pass/fail.
- [ ] Regression spot-check: css-backgrounds (box-shadow), compositing, css-masking, css-will-change, css-position abs/fixed-CB.
- [ ] Update this file's status; archive detailed findings to `docs/findings-filter-effects-archive.md` if it grows large.
- [ ] Any genuinely-deferred tests listed with reason in the residuals section below.

## Residuals / deferred (update as work proceeds)
- `azimuth-and-elevation`, `limiting-cone-angle`, `lighting-region`, `kernel-unit-length-001/002` — `feDiffuseLighting`/`feSpecularLighting` 3-D lighting correctness; attempted last in Phase 8. Not in the failing-178 set if currently skipped/passing — re-verify at Phase 8.
- Revisit after Phase 8 whether `feTurbulence` reproducibility (`filter-turbulence-invalid-001`, `effect-reference-displacement-negative-scale-001`) matches Blink's PRNG exactly; if not, document the gap here.

## Key Questions
- Does louis14's `dc.NewChildContext`/`PushGroup` already give correct premultiplied compositing for the offscreen filter buffer? (Phase 1 must verify before building on it.)
- How are paused CSS animations sampled in the reftest harness — is the filter value already correct at `animation-play-state: paused` so buckets D/E need no animation work? (Spot-check during Phase 2.)
- ~~For `<foreignObject>` (bucket I) — can the existing HTML layout be re-entered cleanly inside an SVG subtree?~~ Deferred by LOU-128 (out-of-scope; foundation provides the `SVGContainer` it would attach to). Decide here if/when a `<foreignObject>` reftest is on the path.

## Decisions Made
- New code lives in `pkg/graphics/filters/` mirroring Blink `platform/graphics/filters/`, per the file-placement rule. `FilterEffectBuilder` lives in `pkg/render/` mirroring Blink `core/paint/`.
- The existing flat `apply*` byte-loop functions in `render.go` are **deleted**, not patched — they cannot be made correct without the FilterEffect graph and linearRGB pipeline.
- Phases are strictly foundational-first: color pipeline → FilterEffect graph + CSS functions → drop-shadow/region → containing block → backdrop-filter → SVG `<filter>` references → SVG render tree → SVG primitive correctness.
