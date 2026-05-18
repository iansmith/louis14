# Task Plan: Shared SVG render-subsystem foundation

## Goal

Build an **SVG render subsystem** in louis14 — an SVG layout tree, SVG paint,
the SVG coordinate/viewport model, and the SVG *resource* elements (`<mask>`,
`<clipPath>`, `<filter>` host, paint servers) — as a **shared foundation** that
the three paused category plans (`docs/plan-css-masking.md`,
`docs/plan-filter-effects.md`, css-animations) each independently require.

Per the "Option C" decision (the same one used for
`docs/plan-marker-foundation.md`): the SVG subsystem is pulled out into one
foundation plan, implemented and **landed first**; the three dependent plans
then build on the landed API instead of each re-doing SVG infrastructure and
colliding on `pkg/layout/`, `pkg/render/`, and `pkg/css/style.go`.

This plan delivers the SVG render tree and the resource model. It does **not**
re-do the FilterEffect graph — `pkg/graphics/filters/` (the `FilterEffect`
graph: `FEColorMatrix`, `FEGaussianBlur`, etc.) is owned by
`docs/plan-filter-effects.md` Phases 1–3 and is treated here as a *consumer
target*: this foundation builds the SVG `<filter>` **element model** and wires
it into that graph, but the graph itself is out of scope.

## Rules & Discipline (DO NOT DUPLICATE HERE)

Re-read both before any planning or coding session — non-negotiable:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study
   Blink first, 0% diff required, test-execution discipline (only the 1–4 tests
   under the phase + a tiny regression sample), operational rules (no `open`,
   commit before worktree agents, worktree commit scope, `git push` with branch
   name).
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`**
   — auto-memory index pointing at the same rules, plus
   `feedback_blink_file_placement` (port a Blink primitive into the package
   mirroring its Blink source location), `feedback_worktree_fonts`,
   `feedback_gofmt_after_edits`.

If you are about to type a rule verbatim into this file or a code comment, stop
and link instead.

---

## Why this plan exists — the collision

Three already-written plans each hit the same wall: louis14 has **no SVG render
subsystem**. An inline `<svg>` is handled only as an intrinsic-sized *replaced
element* and its children (`<rect>`, `<circle>`, `<path>`, `<g>`, `<mask>`,
`<filter>`, `<linearGradient>`, …) are never laid out or painted.

- **`docs/plan-css-masking.md`** — its Phase 3 needs SVG `<mask>` element
  resolution via `url(#id)` and rasterizing the mask's child shapes at
  luminance; its Phase 4 needs basic SVG shape painting (`<rect>` + `fill`) and
  the SVG `mask=` presentation attribute. Tests: `mask-image-1d`,
  `mask-opacity-1d` (and the SVG-shape half of the category).
- **`docs/plan-filter-effects.md`** — its buckets **H** (~25, SVG `<filter>`
  reference pipeline applied to HTML elements), **I** (~32, SVG content
  rendering + filters on SVG elements), **J** (~33, SVG filter primitive
  correctness) and Phases 6–7 all require an SVG `<filter>`/`fe*` element model,
  an SVG render tree, and SVG resource reference resolution. Its Phase 7 is
  literally titled "SVG layout & paint tree".
- **css-animations** — `svg-transform-animation` needs inline-SVG layout +
  paint + `<g transform>` so the animated transform has something to apply to.

Implemented independently in three worktrees these would collide badly on
`pkg/layout/intrinsic_sizing.go`, `pkg/layout/layout_tree_builder.go`,
`pkg/layout/inline_item.go`, `pkg/render/render.go`, `pkg/render/paint_layer.go`
and `pkg/css/style.go`. The union of what they need **is** this foundation's
scope.

### Union of requirements (the foundation's scope)

| Capability | css-masking | filter-effects | css-animations |
|---|---|---|---|
| `<svg>` root that lays out + paints its children (not just a replaced element) | Phase 4 | bucket I, Phase 7 | yes |
| SVG coordinate/viewport model: `viewBox`, `preserveAspectRatio`, user units, length resolution | Phase 4 | bucket I | yes |
| Basic SVG shapes: `<rect>`/`<circle>`/`<ellipse>`/`<line>`/`<polyline>`/`<polygon>`/`<path>` layout + fill/stroke paint | Phase 4 (`<rect>`+`fill`) | bucket I/J | — |
| `<g>` container + `transform` attribute / CSS `transform` | — | bucket I | yes (the animated `<g>`) |
| Nested `<svg>` viewport container | — | bucket I | — |
| Paint servers: `<linearGradient>`, `<radialGradient>`, `<pattern>` for `fill`/`stroke` `url(#id)` | — | bucket I | — |
| `<clipPath>` resource element + `clip-path: url(#id)` on SVG content | — | bucket I (`svg-filter-vs-clip-path`) | — |
| `<mask>` resource element + `mask`/`mask-image: url(#id)` resolution, rasterized at luminance | Phase 3 | bucket I (`svg-filter-vs-mask`) | — |
| `<filter>` **element host** + `fe*` children, wired into `pkg/graphics/filters/` graph | — | buckets H/I/J, Phases 6–7 | — |
| `url(#id)` reference resolution: shared resolver from a CSS/presentation-attr reference to an SVG resource DOM element | Phase 3 | buckets H/I | — |
| SVG presentation attributes → style (`fill`, `stroke`, `x`, `y`, `width`, `height`, `transform`, `color-interpolation-filters`, …) | Phase 4 | buckets H/I | yes |

### What `pkg/graphics/filters/` already provides (do NOT duplicate)

`docs/plan-filter-effects.md` Phases 1–3 create `pkg/graphics/filters/`
mirroring Blink `platform/graphics/filters/`:
- `colorspace.go` — sRGB↔linearRGB transfer, premultiply/unpremultiply,
  `InterpolationSpace`.
- `filter.go` / `filter_effect.go` — the `Filter` graph owner and the
  `FilterEffect` interface (`ApplyEffect`, `MapRect`, `InputEffects`).
- `fe_color_matrix.go`, `fe_component_transfer.go`, `fe_gaussian_blur.go`,
  `fe_drop_shadow.go`, `fe_offset.go`, `fe_flood.go`, `fe_merge.go`,
  `source_graphic.go` (and, in later filter-effects phases, the rest of the
  `fe_*` set).
- `pkg/render/filter_effect_builder.go` — `FilterEffectBuilder` turning
  `[]css.FilterFunction` into a `filters.Filter` graph.

**This foundation does not touch the FilterEffect graph.** Its Phase 7 builds
the SVG `<filter>`/`fe*` *element model* (`pkg/graphics/filters/svg_filter_builder.go`,
the `SVGFilterBuilder` Blink calls out) and the `url(#id)` host wiring — it
*feeds* the existing graph. See "Land order" in Downstream impact: the
filter-effects FilterEffect-graph phases (1–3) and this foundation are
independent and can land in either order; this foundation's Phase 7 *consumes*
the graph and so should land after filter-effects Phase 3 (or stub the graph
edge and let filter-effects fill it).

---

## Current louis14 state (what exists — read-only survey)

Established by reading the source:

### `<svg>` is a replaced element only — children are dead weight

- `pkg/layout/intrinsic_sizing.go:27` — `GetIntrinsicSizingInfo` dispatches
  `case "svg"` to `getInlineSVGIntrinsicInfo` (`intrinsic_sizing.go:88-160`),
  which parses `width`/`height`/`viewBox` *attributes* for intrinsic size and
  aspect ratio. This is correct CSS-replaced-element sizing and the foundation
  **keeps it** — it is the input to the SVG-root box's content size.
- `pkg/layout/inline_item.go:611-613` — `isReplacedElement` returns true for
  `"svg"`, so an inline `<svg>` becomes an `InlineItemAtomicInline`
  (`inline_item.go:23`). `pkg/layout/block_layout.go:140` routes replaced
  elements to `ComputeReplacedSize`; `pkg/layout/replaced_layout.go:234`
  `ReplacedLayoutAlgorithm` lays the `<svg>` box out **with no knowledge of its
  children**.
- `pkg/layout/layout_tree_builder.go:148-165` — `buildNode` recurses into *all*
  DOM children unconditionally, so `<rect>`/`<g>`/`<mask>`/`<filter>` etc. *do*
  become `LayoutInputNode`s in the tree — but they carry no SVG semantics and
  `ReplacedLayoutAlgorithm` never lays them out, so they are inert.
- `pkg/render/render.go:1361` `paintSelfForeground` → `render.go:1369`
  `r.drawImage(layer)` (`render.go:2034`) only paints when
  `layer.ImageSrc != ""`. `pkg/render/paint_layer.go:367-369` sets `ImageSrc`
  **only for `box.Node.TagName == "img"`**. So an inline `<svg>` element paints
  *nothing at all* — confirming the dependent plans' "renders blank" symptom.

### `oksvg` / `rasterx` — used only for *image* SVGs, not inline SVG

- `pkg/images/loader.go:19-20` imports `github.com/srwiley/oksvg` and
  `github.com/srwiley/rasterx`. `loader.go:153-154` `rasterizeSVG` (`:244-`)
  rasterizes an SVG **file/data-URL** referenced by `<img src>` or
  `background-image` via `oksvg.ReadIconStream` + `rasterx`. `loader.go:354`
  `SVGSizingInfo` parses root-element sizing metadata for `<img>` intrinsic
  sizing.
- oksvg is a **whole-document rasterizer**: it parses an SVG byte stream and
  draws it to an `image.RGBA`. It has **no per-element layout tree, no DOM, no
  resource-by-id model exposed to the caller, no paint-server/`<filter>` hooks,
  and no integration with louis14's CSS cascade, transforms, or compositing**.

### Decision: Blink-faithful layout/paint tree, NOT oksvg for inline SVG

**Inline `<svg>` rendering must be a Blink-faithful SVG layout + paint tree,
not delegated to oksvg.** Reasoning:

1. **The dependents need per-element integration oksvg cannot give.**
   css-masking needs to rasterize *one specific `<mask>` element's children* at
   luminance into a buffer aligned to a CSS element's border box. filter-effects
   needs `filter: url(#f)` on an *HTML* element to resolve an SVG `<filter>` DOM
   node and feed `pkg/graphics/filters/`. css-animations needs to mutate a
   `<g transform>` and re-paint. All three require addressing SVG elements
   individually, through louis14's DOM and CSS cascade — oksvg's opaque
   "parse-bytes-draw-bitmap" surface exposes none of this.
2. **CLAUDE.md §1 (foundational correctness) and §2 (mirror Blink).** Blink
   models SVG as a real `LayoutObject` subtree (`LayoutSVGRoot`,
   `LayoutSVGContainer`, `LayoutSVGShape`, …) that participates in the same
   layout/paint/transform/compositing machinery as HTML. A bolt-on rasterizer
   is the "quick win" CLAUDE.md §1 forbids — it would not generalize to masks,
   filters, clip-paths, gradients, animation, or `<foreignObject>`.
3. **0% pixel diff (CLAUDE.md §3).** oksvg's rasterizer is not pixel-aligned
   with louis14's `pkg/render` `DrawContext` (anti-aliasing, gradient ramps,
   stroke geometry, transform rounding all differ). To pass WPT SVG reftests at
   0% the SVG content must paint through the *same* `DrawContext` primitives as
   HTML content.

**oksvg/rasterx stay exactly where they are** — `pkg/images/loader.go` for
`<img>`/`background-image` SVG *images*. That path is correct and untouched.
The foundation builds a parallel, Blink-faithful path for *inline* SVG content.
(A later, separate decision could switch the `<img src=*.svg>` path onto the
new tree too, for consistency; that is explicitly out of scope here — see
Deferrals.)

### Embedding seam

The HTML↔SVG embedding point already half-exists: an inline `<svg>` is an
atomic-inline replaced box (`isReplacedElement`), sized by
`getInlineSVGIntrinsicInfo` + `ComputeReplacedSize`. The foundation's Phase 1
replaces `ReplacedLayoutAlgorithm` *for `<svg>`* with an `SVGRootAlgorithm` that
keeps that exact outer sizing (so the `<svg>` box's position/size in the HTML
flow is unchanged) but lays out an SVG subtree inside it. This mirrors Blink
exactly: `LayoutSVGRoot extends LayoutReplaced` — the root *is* a replaced
element to its HTML parent, and an SVG container to its children.

---

## Blink reference model (study BEFORE writing code)

All paths under `third_party/blink/renderer/`. Verified against the Chromium
`main` mirror, 2026-05-14.

### Layout — `core/layout/svg/`

- **`layout_svg_root.{h,cc}` — `LayoutSVGRoot : LayoutReplaced`.** The
  HTML↔SVG embedding point. Members: `container_size_` (physical), `content_`
  (an `SVGContentContainer` holding the child SVG layout objects),
  `local_to_border_box_transform_`. Methods: `LayoutRoot(const PhysicalRect&
  content_rect)` (the SVG layout pass), `UnscaledNaturalSizingInfo()` (intrinsic
  sizing), `ViewBoxRect()`, `ViewportSize()`, `BuildLocalToBorderBoxTransform()`
  / `LocalToBorderBoxTransform()` — the `AffineTransform` mapping "local SVG
  viewport coordinates to local CSS box coordinates" (this is where `viewBox` +
  `preserveAspectRatio` are baked in). `IsEmbeddedThroughSVGImage()` /
  `IsEmbeddedThroughFrameContainingSVGDocument()` distinguish embedding contexts
  — for louis14's inline `<svg>` neither is true (the "inline document" case).
- **`layout_svg_container.{h,cc}` — `LayoutSVGContainer : LayoutSVGModelObject`.**
  Base for `<g>` and nested `<svg>`. Holds an `SVGContentContainer`; provides
  `ObjectBoundingBox()` / `StrokeBoundingBox()` / `DecoratedBoundingBox()`
  (delegated to the content container — i.e. the union of children's boxes),
  `UpdateSVGLayout(const SVGLayoutInfo&)`, `UpdateAfterSVGLayout()`,
  `UpdateLocalTransform(const gfx::RectF& reference_box)`,
  `transform_uses_reference_box_`.
- **`layout_svg_transformable_container.{h,cc}` — `LayoutSVGTransformableContainer
  : LayoutSVGContainer`.** The concrete `<g>` (and `<use>`, `<switch>`) box.
  Owns a `local_transform_` `AffineTransform` computed from the element's
  `transform` attribute / CSS `transform` via `TransformHelper`.
- **`layout_svg_viewport_container.{h,cc}` — `LayoutSVGViewportContainer :
  LayoutSVGContainer`.** A *nested* `<svg>` — establishes a new viewport, holds
  `viewport_` (a `gfx::RectF`) and `local_to_parent_transform_` (its own
  `viewBox`/`preserveAspectRatio` mapping), clips to the viewport when
  `overflow: hidden`.
- **`layout_svg_shape.{h,cc}` — `LayoutSVGShape : LayoutSVGModelObject`.** Base
  for every shape. Members: `std::unique_ptr<Path> path_`, `fill_bounding_box_`
  (→ `ObjectBoundingBox()`), `decorated_bounding_box_` (fill ∪ stroke ∪
  markers). A `GeometryType` enum: `kPath, kLine, kRectangle,
  kRoundedRectangle, kEllipse, kCircle`. Pure virtual
  `UpdateShapeFromElement()` implemented by each subclass to (re)build `path_`
  and `fill_bounding_box_` from the element's geometry attributes. The layout
  pass calls `UpdateSVGLayout(const SVGLayoutInfo&)` then `UpdateAfterSVGLayout()`.
- **`layout_svg_rect.{h,cc}` (`LayoutSVGRect`), `layout_svg_ellipse.{h,cc}`
  (`LayoutSVGEllipse` — covers `<circle>` and `<ellipse>`),
  `layout_svg_path.{h,cc}` (`LayoutSVGPath` — `<path>`, `<line>`, `<polyline>`,
  `<polygon>`).** Each implements `UpdateShapeFromElement()`. `LayoutSVGRect`
  fast-paths a non-rounded rect (no `Path` object needed); the others build a
  `Path`.
- **`svg_layout_info.h` — `SVGLayoutInfo` / `SVGLayoutResult`.** `SVGLayoutInfo`
  carries `force_layout`, `scale_factor_changed`, `viewport_changed` *down* the
  tree; `SVGLayoutResult` carries `bounds_changed`, `has_viewport_dependence`
  *up*. The SVG layout pass is **not** CSS box flow — it is a recursive
  geometry/transform/bounding-box update.
- **`transform_helper.{h,cc}` — `TransformHelper`.** `ComputeTransform(const
  ComputedStyle&, const gfx::RectF& reference_box, ApplyTransformOrigin)` →
  `AffineTransform` from the element's CSS/`transform`-attribute properties;
  `ComputeReferenceBox()` (honors `transform-box`); `ComputeTransformOrigin()`.
- **`core/layout/svg/README.md`** — confirms: SVG layout objects form a real
  `LayoutObject` subtree under `LayoutSVGRoot`; layout is transform- and
  bounding-box-driven, not flow-driven; resources (`<mask>`/`<clipPath>`/
  `<filter>`/paint servers) are `LayoutSVGResourceContainer`s that are *not*
  painted in tree order but referenced by `url(#id)`.

### Geometry / coordinates — `core/svg/`

- **`svg_length_context.{h,cc}` — `SVGLengthContext`.** Resolves SVG `<length>`
  values: `ConvertValueToUserUnits` / `ConvertValueFromUserUnits`,
  `ResolveValue` (CSS math functions), takes an `SVGLengthMode` (width / height
  / other) so a `%` resolves against the viewport's width, height, or the
  normalized diagonal. `ViewportSize()` supplies the percentage basis.
  `SVGLengthConversionData : CSSToLengthConversionData`.
- **`viewBox` / `preserveAspectRatio`** — `SVGSVGElement::ViewBoxToViewTransform`
  (in `svg_svg_element.cc`) builds the `viewBox`→viewport `AffineTransform`;
  `SVGPreserveAspectRatio::ComputeTransform` handles `xMidYMid meet/slice` etc.
  `SVGViewSpec`/`SVGFitToViewBox` hold the parsed values.
- **Units: `objectBoundingBox` vs `userSpaceOnUse`.** Resources resolve their
  geometry against either the referencing element's *object bounding box*
  (a 0..1 normalized box, then scaled/translated to the bbox) or the *current
  user space*. This enum recurs on `<mask>` (`maskUnits`/`maskContentUnits`),
  `<clipPath>` (`clipPathUnits`), `<filter>` (`filterUnits`/`primitiveUnits`),
  gradients/patterns (`gradientUnits`/`patternUnits`).

### Paint — `core/paint/svg_*`

- **`svg_root_painter.{h,cc}` — `SVGRootPainter`.** `PaintReplaced(const
  PaintInfo&, const PhysicalOffset&)` — paints the SVG root: applies
  `LocalToBorderBoxTransform`, sets up the viewport clip, then paints `content_`.
- **`svg_container_painter.{h,cc}` — `SVGContainerPainter::Paint(const
  PaintInfo&)`.** Step sequence (from `svg_container_painter.cc`): (1) early-out
  on `HasEmptyViewBox()`; (2) `ScopedSVGPaintState::ComputePaintBehavior()`;
  (3) cull-rect intersect test; (4) `ScopedSVGTransformState` establishes the
  child coordinate space (applies the container's `local_transform_`); (5) for
  a viewport container with `overflow: hidden`, push the overflow clip; (6)
  ensure a filter chunk if a reference filter exists; (7) **iterate children
  in tree order**, recursing `child->Paint()` (`SVGForeignObjectPainter` for
  `<foreignObject>`); (8) paint outline.
- **`svg_shape_painter.{h,cc}` — `SVGShapePainter::Paint(const PaintInfo&)`.**
  Private `PaintShape` → `FillShape(GraphicsContext&, const cc::PaintFlags&,
  WindRule)` then `StrokeShape(GraphicsContext&, const cc::PaintFlags&)` then
  `PaintMarkers(const PaintInfo&)`. **Paint order: fill, then stroke, then
  markers** (SVG 1.1 §11.4). Holds `const LayoutSVGShape& layout_svg_shape_`.
- **`svg_object_painter.{h,cc}` — `SVGObjectPainter`.** Resolves the *paint*
  for fill/stroke: a flat color, `currentColor`, or a paint-server `url(#id)`.
  `PreparePaint(...)` populates a `cc::PaintFlags` — either a solid color or,
  for a `url(#id)`, delegates to the paint server's `ApplyShader`.
- **`svg_mask_painter.{h,cc}` — `SVGMaskPainter`.** `ResolveElementReference()`
  finds the `LayoutSVGResourceMasker` for a `StyleMaskSourceImage`
  (`url(#id)`); `masker->CreatePaintRecord(paint_flags)` records the mask's
  children; `MaskToContentTransform()` (objectBoundingBox → translate to bbox
  origin + scale by bbox size; else zoom); clip to
  `masker->ResourceBoundingBox()`; **luminance** (`cc::ColorFilter::MakeLuma()`)
  vs **alpha** per `EMaskType`. SVG `<mask>` defaults to `mask-type: luminance`.

### Resources — `core/layout/svg/` + `core/svg/`

- **`layout_svg_resource_container.{h,cc}` — `LayoutSVGResourceContainer :
  LayoutSVGHiddenContainer`.** Base for every resource. `enum
  LayoutSVGResourceType { kMaskerResourceType, kMarkerResourceType,
  kPatternResourceType, kLinearGradientResourceType,
  kRadialGradientResourceType, kFilterResourceType, kClipperResourceType }`.
  `ResourceType()`, `IsSVGPaintServer()` (gradient/pattern),
  `RemoveAllClientsFromCache()`, `RemoveClientFromCache(SVGResourceClient&)`,
  `MarkAllClientsForInvalidation(InvalidationModeMask)`. **Resource containers
  are `LayoutSVGHiddenContainer` — they are NOT painted in tree order.** They
  are reached only via `url(#id)`.
- **`LayoutSVGResourceMasker`** (`<mask>`) — `CreatePaintRecord()`,
  `ResourceBoundingBox(reference_box)`, `MaskUnits()`/`MaskContentUnits()`.
- **`LayoutSVGResourceClipper`** (`<clipPath>`) — `CreateClipPath()` (a `Path`
  for shape-based clips) or `CreatePaintRecord()` (for content-based clips),
  `ClipPathUnits()`, `AsPath()`.
- **`LayoutSVGResourceFilter`** (`<filter>`) — holds `filterUnits` /
  `primitiveUnits`, the filter region defaults `x/y/width/height` =
  `-10% -10% 120% 120%`; `ResourceBoundingBox`.
- **`LayoutSVGResourcePaintServer`** (abstract) — pure virtual
  `ApplyShader(const SVGResourceClient&, const gfx::RectF& reference_box, const
  AffineTransform* additional_transform, const AutoDarkMode&, cc::PaintFlags&,
  PaintFlags paint_flags)`. Subclasses `LayoutSVGResourceLinearGradient` /
  `LayoutSVGResourceRadialGradient` (both extend a `LayoutSVGResourceGradient`
  base; honor `gradientUnits`, `gradientTransform`, `spreadMethod`, `<stop>`
  children) and `LayoutSVGResourcePattern` (`patternUnits`/`patternContentUnits`,
  tiles a paint record).
- **`core/svg/svg_resource.{h,cc}` — `SVGResource` / `LocalSVGResource` /
  `ExternalSVGResource*`.** The `url(#id)` resolution mechanism.
  `LocalSVGResource` resolves a same-document `url(#id)` within a `TreeScope`
  via an `IdTargetObserver` (`TargetChanged()`); `ExternalSVGResourceDocumentContent`
  resolves into a loaded SVG document via `ResolveTarget()`. Clients register
  with `AddClient(SVGResourceClient&)` / `AddObserver(...)`. Notification flow:
  `<event> → SVG*Element → SVGResource → SVGResourceClient(0..N)`.
  `FindCycle(SVGResourceClient&)` does cycle detection
  (`kNeedCheck/kPerformingCheck/kHasCycle/kNoCycle`).
- **`core/svg/svg_resources.{h,cc}` — `SVGResources` / `SVGElementResourceClient`.**
  An element's resolved resource set (its `mask` / `clip-path` / `filter` /
  fill+stroke paint servers). `SVGElementResourceClient` is the
  `SVGResourceClient` for an SVG element.
- **`core/style/clip_path_operation.h` — `ReferenceClipPathOperation`** — the
  `clip-path: url(#id)` variant (vs `ShapeClipPathOperation` for `circle()`
  etc., which css-masking already handles).
- **`core/svg/graphics/filters/svg_filter_builder.{h,cc}` — `SVGFilterBuilder`.**
  `BuildGraph(Filter*, SVGFilterElement&, viewport rect, viewport override)` —
  iterates the `<filter>` element's children that implement `IsFilterEffect()`,
  calls each `fe*` element's `Build()`, registers in `SVGFilterGraphNodeMap*
  node_map_`. `GetEffectById(id)` resolution order: `builtin_effects_`
  (`SourceGraphic`, `SourceAlpha`, populated by `AddBuiltinEffects()`) →
  `named_effects_` (named `result=` outputs, registered via `Add()`) →
  `last_effect_` fallback. `ResolveInterpolationSpace(EColorInterpolation)` maps
  `color-interpolation-filters` (default **linearRGB** for SVG filters) to an
  `InterpolationSpace`.

### DOM — `core/svg/` element classes

`SVGElement` (base) → `SVGGraphicsElement` (has `transform`) →
`SVGGeometryElement` (`SVGRectElement`, `SVGCircleElement`,
`SVGEllipseElement`, `SVGLineElement`, `SVGPolylineElement`,
`SVGPolygonElement`, `SVGPathElement`) and `SVGSVGElement`, `SVGGElement`.
Resource elements: `SVGMaskElement`, `SVGClipPathElement`, `SVGFilterElement`,
`SVGLinearGradientElement`/`SVGRadialGradientElement`/`SVGPatternElement`,
`SVGStopElement`, the `SVGFE*Element` family. **Presentation attributes** (an
SVG element's `fill`, `stroke`, `x`, `width`, `transform`, …) map into the CSS
cascade as the lowest-priority author-level declarations — exactly louis14's
existing `applyPresentationalAttributes` mechanism
(`pkg/css/cascade.go:1357-1407`), which the foundation extends.

---

## Architecture the foundation lands

```
HTML layout tree
  └─ <svg> box  ← isReplacedElement, sized by getInlineSVGIntrinsicInfo
                  + ComputeReplacedSize  (UNCHANGED outer sizing)
       │  laid out by NEW SVGRootAlgorithm (replaces ReplacedLayoutAlgorithm for <svg>)
       ▼
     SVG layout subtree  (pkg/layout/svg/, mirrors core/layout/svg/)
       SVGRoot           — viewBox/preserveAspectRatio → localToBorderBoxTransform
        ├─ SVGShape      — <rect>/<circle>/<ellipse>/<line>/<polyline>/<polygon>/<path>
        ├─ SVGContainer  — <g> (transformable), nested <svg> (viewport container)
        │    └─ … recursion …
        └─ resource elements (NOT in paint order; resolved by url(#id)):
             SVGResourceMasker     <mask>
             SVGResourceClipper    <clipPath>
             SVGResourceFilter     <filter> + fe* children
             SVGResourcePaintServer <linearGradient>/<radialGradient>/<pattern>

     paint:  SVGRootPainter → SVGContainerPainter → SVGShapePainter
             (pkg/render/svg_*.go, mirrors core/paint/svg_*)
             fill→stroke order; SVGObjectPainter resolves paint (color / url(#id))

     references: pkg/layout/svg/svg_resource.go — SVGResourceRegistry
                 resolves url(#id) → SVG resource node, shared by
                 css-masking, filter-effects, SVG content itself
```

Key principle (CLAUDE.md §2): louis14 packages mirror Blink source locations.
`core/layout/svg/` → **`pkg/layout/svg/`** (new sub-package). `core/paint/svg_*`
→ **`pkg/render/svg_*.go`** (paint lives in `pkg/render`, mirroring
`core/paint`). `core/svg/svg_length_context` → **`pkg/layout/svg/svg_length_context.go`**.
The SVG `<filter>` element model → **`pkg/graphics/filters/svg_filter_builder.go`**
(mirroring `core/svg/graphics/filters/`), feeding the existing FilterEffect
graph.

---

## Phases

Foundational-first. Each phase: goal, Blink reference (file / class / algorithm
/ types), louis14 target files, new types, approach, gate metric. Per CLAUDE.md
§4 run only the 1–4 tests under the phase plus a tiny regression sample; never
the full suite during the work.

### Phase 0: Baseline & embedding-seam confirmation — **prerequisite**

- Read the SVG sections of `docs/plan-css-masking.md` (Phases 3–4),
  `docs/plan-filter-effects.md` (buckets H/I/J, Phases 6–7), and confirm the
  `svg-transform-animation` requirement from css-animations.
- Confirm the current state: render 3 representative tests once each (sanctioned
  ≤3) — one inline-SVG shape test (`filter-effects/svg-*` simplest), one
  `css-masking/mask-image-1d`, one with `<g transform>` — and confirm `_test.png`
  is blank, proving `<svg>` children never paint.
- Confirm the embedding seam: a debug print that an inline `<svg>` reaches
  `ReplacedLayoutAlgorithm` (`replaced_layout.go:254`) and its children are
  present as inert `LayoutInputNode`s under it.
- **Gate:** scope confirmed; root-cause (no SVG subsystem) verified; no code
  changed.

### Phase 1: SVG root embedding + coordinate/viewport model — **FOUNDATIONAL, fixes nothing alone**

**Goal.** An inline `<svg>` is laid out by a new `SVGRootAlgorithm` that keeps
its existing replaced-element *outer* sizing but establishes the SVG coordinate
system (`viewBox` → viewport `AffineTransform`, `preserveAspectRatio`, user
units) and an empty SVG layout subtree under it. No SVG content paints yet; this
is the chassis every later phase bolts onto.

**Blink reference.**
- `core/layout/svg/layout_svg_root.{h,cc}` — `LayoutSVGRoot : LayoutReplaced`;
  `container_size_`, `local_to_border_box_transform_`,
  `BuildLocalToBorderBoxTransform()`, `LayoutRoot(content_rect)`,
  `ViewBoxRect()`, `ViewportSize()`.
- `core/svg/svg_length_context.{h,cc}` — `SVGLengthContext`, `SVGLengthMode`,
  `ConvertValueToUserUnits`, `ViewportSize()`.
- `core/svg/svg_svg_element.cc` — `ViewBoxToViewTransform`;
  `SVGPreserveAspectRatio::ComputeTransform` (`xMidYMid meet` default).
- `core/layout/svg/svg_layout_info.h` — `SVGLayoutInfo` / `SVGLayoutResult`.

**louis14 target files.**
- New package **`pkg/layout/svg/`** (mirrors `core/layout/svg/`).
- New `pkg/layout/svg/svg_root.go` — `SVGRoot` layout node + `SVGRootAlgorithm`.
- New `pkg/layout/svg/svg_length_context.go` — mirrors `svg_length_context.{h,cc}`.
- New `pkg/layout/svg/viewbox.go` — `viewBox`/`preserveAspectRatio` parsing +
  the viewBox→viewport `AffineTransform`.
- New `pkg/layout/svg/svg_layout_info.go` — `SVGLayoutInfo` / `SVGLayoutResult`.
- `pkg/layout/layout_algorithm.go` / wherever `ReplacedLayoutAlgorithm` is
  dispatched — route `<svg>` to `SVGRootAlgorithm` instead of
  `ReplacedLayoutAlgorithm`.
- `pkg/layout/intrinsic_sizing.go` — **unchanged**; `getInlineSVGIntrinsicInfo`
  (`:88-160`) remains the outer-size source.
- `pkg/geometry/` — add an `AffineTransform` type (2×3 affine matrix:
  `a,b,c,d,e,f`) if one does not already exist; SVG needs full affine
  composition that the CSS-transform paint code in `render.go:1174` builds
  ad-hoc.

**New types.**
- `svg.SVGRoot` — the root SVG layout node; holds `containerSize geometry.PhysicalSize`,
  `viewBox` (parsed), `preserveAspectRatio`, `localToBorderBoxTransform geometry.AffineTransform`,
  `children []SVGNode`.
- `svg.SVGNode` interface — `ObjectBoundingBox() geometry.RectF`,
  `LocalTransform() geometry.AffineTransform`, `UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult`,
  `Paint(*SVGPaintContext)`. Every SVG layout node implements it.
- `svg.SVGLengthContext` — `Resolve(value string, mode SVGLengthMode) float64`,
  `ViewportSize() geometry.SizeF`; `SVGLengthMode` ∈ `{Width, Height, Other}`.
- `svg.SVGLayoutInfo{ForceLayout, ScaleChanged, ViewportChanged bool}` /
  `svg.SVGLayoutResult{BoundsChanged, HasViewportDependence bool}`.
- `geometry.AffineTransform` (if not present).

**Approach.**
1. `SVGRootAlgorithm.Layout()` computes the `<svg>` box content size exactly as
   today (`ComputeReplacedSize` via `getInlineSVGIntrinsicInfo`) — the HTML-flow
   position/size of the `<svg>` box is **byte-identical** to current behavior.
2. It then builds the `SVGRoot` node: parse `viewBox` + `preserveAspectRatio`
   (the existing `getInlineSVGIntrinsicInfo` viewBox parse moves into
   `viewbox.go` and is shared), and compute `localToBorderBoxTransform` =
   the `viewBox`→content-box mapping (`xMidYMid meet` default per
   `SVGPreserveAspectRatio::ComputeTransform`). When there is no `viewBox`,
   the transform is identity (user units == CSS px).
3. Build the SVG layout subtree from the `<svg>`'s `LayoutInputNode` children:
   a `tag → SVGNode` dispatch (`svg.BuildSVGTree`). In this phase the dispatch
   recognizes only `<svg>` (root) and produces empty `SVGContainer` stubs for
   everything else — *no shape geometry, no paint yet*. The point is the chassis
   and the coordinate model.
4. Run the SVG layout pass: a recursive `UpdateSVGLayout(SVGLayoutInfo)` walk
   (mirrors Blink's `LayoutRoot` → container `UpdateSVGLayout`), bottom-up
   accumulating `ObjectBoundingBox`. This is **not** CSS box flow — no margins,
   no line boxes; pure geometry/transform/bbox.

**Tests fixed.** None — pure chassis, no behavior change (SVG still paints
blank, exactly as before).

**Gate.** `pkg/layout` + `pkg/layout/svg` build clean; `go vet` clean. The 3
Phase-0 tests still render blank (unchanged). A 4-test css-position /
css-flexbox sample unchanged — confirms the `<svg>`-box outer sizing is
untouched. CSS2 sample (4 tests) unchanged.

### Phase 2: Basic SVG shapes — layout + fill/stroke paint

**Goal.** `<rect>`, `<circle>`, `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`,
`<path>` directly inside an `<svg>` lay out (build a `Path`, compute the
object bounding box) and paint their `fill` and `stroke` through louis14's
`DrawContext`. This is the first phase that puts SVG pixels on screen.

**Blink reference.**
- `core/layout/svg/layout_svg_shape.{h,cc}` — `LayoutSVGShape`, `path_`,
  `fill_bounding_box_`, `decorated_bounding_box_`, `GeometryType`,
  `UpdateShapeFromElement()`.
- `core/layout/svg/layout_svg_rect.{h,cc}`, `layout_svg_ellipse.{h,cc}`,
  `layout_svg_path.{h,cc}` — per-shape `UpdateShapeFromElement()`.
- `core/paint/svg_shape_painter.{h,cc}` — `Paint` → `PaintShape` → `FillShape`
  → `StrokeShape` → `PaintMarkers`; **fill before stroke** (SVG 1.1 §11.4).
- `core/paint/svg_object_painter.{h,cc}` — `SVGObjectPainter::PreparePaint` —
  resolve fill/stroke to a solid color, `currentColor`, or (Phase 4) a paint
  server.
- `core/svg/svg_length_context.cc` — geometry attribute resolution
  (`x`/`y`/`width`/`height`/`cx`/`cy`/`r`/`rx`/`ry`/`points`/`d`).

**louis14 target files.**
- New `pkg/layout/svg/svg_shape.go` — `SVGShape` node + the per-shape geometry
  builders (`buildRectPath`, `buildEllipsePath`, `buildPolyPath`, `buildPath`
  for the `d` mini-parser). One file mirrors the small `layout_svg_*` cluster.
- New `pkg/render/svg_shape_painter.go` — mirrors `core/paint/svg_shape_painter.cc`.
- New `pkg/render/svg_object_painter.go` — mirrors `core/paint/svg_object_painter.cc`;
  resolves `fill`/`stroke`/`fill-opacity`/`stroke-opacity`/`stroke-width`/
  `stroke-linecap`/`stroke-linejoin`/`fill-rule` from style.
- `pkg/css/cascade.go` — extend `applyPresentationalAttributes`
  (`cascade.go:1357`) to map SVG presentation attributes (`fill`, `stroke`,
  `fill-opacity`, `stroke-*`, `fill-rule`, `opacity`, geometry attrs are read
  directly by the shape builders, not the cascade) into `css.Style`.
- `pkg/css/style.go` — add `GetFill()`, `GetStroke()`, `GetStrokeWidth()`,
  `GetFillRule()`, `GetFillOpacity()`, `GetStrokeOpacity()`,
  `GetStrokeLinecap()`, `GetStrokeLinejoin()` accessors with SVG defaults
  (`fill: black`, `stroke: none`, `fill-rule: nonzero`, `stroke-width: 1`).
- `pkg/render/render.go` — `SVGRootPainter` entry (new `pkg/render/svg_root_painter.go`,
  Phase 3 also touches it) is reached from `paintSelfForeground`
  (`render.go:1361`) when the layer's node is `<svg>` — replacing the
  `drawImage`-only path.

**New types.**
- `svg.SVGShape` — `geometryType`, `path geometry.Path`, `fillBoundingBox geometry.RectF`.
- `svg.SVGGeometryType` ∈ `{Path, Line, Rect, RoundedRect, Ellipse, Circle}`.
- `render.SVGShapePainter`, `render.SVGObjectPainter`.
- `render.SVGPaintContext` — carries the accumulated `AffineTransform`, the
  `DrawContext`, the `SVGResourceRegistry` (Phase 6), the current viewport for
  `%` resolution.

**Approach.**
1. `BuildSVGTree` dispatch (Phase 1) now recognizes the seven shape tags and
   builds `SVGShape` nodes. `UpdateShapeFromElement` per shape: `<rect>` →
   rounded-or-plain rectangle path (resolve `x/y/width/height/rx/ry` via
   `SVGLengthContext`); `<circle>`/`<ellipse>` → ellipse path; `<line>` → a
   2-point path; `<polyline>`/`<polygon>` → poly path (polygon closes);
   `<path>` → parse the `d` attribute (a faithful SVG path-data mini-parser:
   `M m L l H h V v C c S s Q q T t A a Z z`). The resolved geometry goes into
   `path` and `fillBoundingBox` mirrors `fill_bounding_box_`.
2. `SVGShapePainter.Paint`: apply the shape's `LocalTransform` (identity for a
   bare shape — `<g>` transforms come in Phase 3), then **fill then stroke**:
   `FillShape` builds a fill path on the `DrawContext` honoring `fill-rule`
   (`nonzero`/`evenodd`); `StrokeShape` strokes honoring `stroke-width`,
   `stroke-linecap`, `stroke-linejoin`, `stroke-dasharray`. Paint resolution
   (`SVGObjectPainter`): `fill`/`stroke` of `none` → skip; a color → that color;
   `currentColor` → the element's `color`; `url(#id)` → deferred to Phase 4 (a
   no-op + fallback color for now).
3. `SVGRootPainter` applies `localToBorderBoxTransform` then recurses into the
   root's children in tree order.
4. **css-masking dependency satisfied here:** `mask-image-1d` needs exactly
   "paint a `<rect>` with `fill`" — once Phase 2 lands, that test's `<rect>`
   paints; the SVG `mask=` attribute resolving to a non-existent `#foo`
   (unresolvable → not masked → renders normally) is the existing
   `maskImg == nil` fallback, already correct (see css-masking Phase 4). So
   `mask-image-1d` is **fixed by this phase** modulo css-masking's
   stacking-context wiring.

**Tests fixed.** `css-masking/mask-image-1d` (with css-masking Phase 1+4's SC
wiring). The simplest `filter-effects` bucket-I `svg-*` shape tests that need
only fill/stroke (e.g. `svg-empty-*`, plain-shape `svg-sourcegraphic-*` refs).

**Gate.** A hand-checked inline-`<svg>`-with-`<rect>` paints a pixel-correct
filled rect at the right offset; `mask-image-1d` passes at 0% diff once
css-masking's SC fix is also present (coordinate with that plan — verify with a
local `<rect>`-only fixture if css-masking has not landed). No regression in the
css-position / CSS2 samples.

### Phase 3: `<g>` container + transforms + nested `<svg>` viewport

**Goal.** `<g>` groups its children and applies its `transform` attribute / CSS
`transform`; a nested `<svg>` establishes a new viewport with its own
`viewBox`. Transforms compose down the tree.

**Blink reference.**
- `core/layout/svg/layout_svg_container.{h,cc}` — `LayoutSVGContainer`,
  `UpdateLocalTransform(reference_box)`, `transform_uses_reference_box_`,
  `ObjectBoundingBox()` (union of children).
- `core/layout/svg/layout_svg_transformable_container.{h,cc}` —
  `LayoutSVGTransformableContainer : LayoutSVGContainer`, `local_transform_`.
- `core/layout/svg/layout_svg_viewport_container.{h,cc}` —
  `LayoutSVGViewportContainer`, `viewport_`, `local_to_parent_transform_`,
  overflow clip.
- `core/layout/svg/transform_helper.{h,cc}` — `TransformHelper::ComputeTransform`,
  `ComputeReferenceBox`, `ComputeTransformOrigin`.
- `core/paint/svg_container_painter.cc` — `ScopedSVGTransformState` applies the
  container transform before recursing into children.

**louis14 target files.**
- New `pkg/layout/svg/svg_container.go` — `SVGContainer` (`<g>`) +
  `SVGViewportContainer` (nested `<svg>`).
- New `pkg/layout/svg/transform_helper.go` — mirrors `transform_helper.{h,cc}`;
  parses the SVG `transform` attribute (`translate`/`scale`/`rotate`/`skewX`/
  `skewY`/`matrix`) and CSS `transform`, composes into a `geometry.AffineTransform`.
- New `pkg/render/svg_container_painter.go` — mirrors `svg_container_painter.cc`.
- `pkg/css/cascade.go` — SVG `transform` *attribute* into the cascade /
  presentation hints (Blink: the `transform` attribute is a presentation
  attribute that feeds the `transform` CSS property).

**New types.**
- `svg.SVGContainer` — `localTransform geometry.AffineTransform`, `children []SVGNode`.
- `svg.SVGViewportContainer` — embeds `SVGContainer`, adds `viewport geometry.RectF`,
  `localToParentTransform`.
- `render.SVGContainerPainter`.
- `svg.TransformHelper` — `ComputeTransform(style, refBox) geometry.AffineTransform`.

**Approach.**
1. `BuildSVGTree` dispatch recognizes `<g>` → `SVGContainer` and a nested
   `<svg>` → `SVGViewportContainer`. `UpdateSVGLayout` for a container computes
   its `localTransform` (from `transform`) and recurses; `ObjectBoundingBox` is
   the transform-mapped union of children's boxes.
2. `SVGContainerPainter.Paint` mirrors the `svg_container_painter.cc` step
   sequence: compute paint behavior, push the container's `localTransform` onto
   the paint context's transform stack (`ScopedSVGTransformState`), push the
   viewport overflow clip for a viewport container with `overflow: hidden`,
   recurse into children in tree order, pop.
3. The transform stack in `SVGPaintContext` accumulates: a shape deep in a
   `<g><g>` paints with the composed transform. Transforms are applied at the
   `DrawContext` level (the same primitive `render.go:1174 applyTransforms`
   uses), so SVG transforms pixel-align with CSS transforms.
4. **css-animations dependency satisfied here:** `svg-transform-animation`
   animates a `<g transform>` — once `<g>` is a real transformable container
   and the animation sampler sets the computed `transform`, the animated value
   flows through `TransformHelper`. (The animation sampling itself is
   css-animations' job; this phase provides the `<g>` it animates.)

**Tests fixed.** `css-animations/svg-transform-animation` (with css-animations'
sampler). `filter-effects` bucket-I tests using `<g>`/nested `<svg>`/transforms
without resources.

**Gate.** A hand-checked `<g transform="translate(...)">` over a `<rect>`
paints the rect at the translated offset, 0% diff; a nested `<svg viewBox>`
maps coordinates correctly. Prior gates still 0%.

### Phase 4: Paint servers — gradients + patterns

**Goal.** `fill="url(#grad)"` / `stroke="url(#grad)"` resolve a `<linearGradient>`
/ `<radialGradient>` / `<pattern>` resource element and paint the shape with it,
honoring `gradientUnits` / `patternUnits` (`objectBoundingBox` vs
`userSpaceOnUse`) and `gradientTransform`.

**Blink reference.**
- `core/layout/svg/layout_svg_resource_paint_server.{h,cc}` —
  `LayoutSVGResourcePaintServer`, pure virtual `ApplyShader(client, reference_box,
  additional_transform, auto_dark_mode, cc::PaintFlags&, paint_flags)`.
- `core/layout/svg/layout_svg_resource_gradient.{h,cc}` /
  `layout_svg_resource_linear_gradient.{h,cc}` /
  `layout_svg_resource_radial_gradient.{h,cc}` — `gradientUnits`,
  `gradientTransform`, `spreadMethod`, `<stop>` offset/color/opacity.
- `core/layout/svg/layout_svg_resource_pattern.{h,cc}` — `patternUnits`,
  `patternContentUnits`, tiles a paint record.
- `core/paint/svg_object_painter.cc` — `PreparePaint` delegates a `url(#id)`
  fill/stroke to the paint server's `ApplyShader`.

**louis14 target files.**
- New `pkg/layout/svg/svg_resource_paint_server.go` — `SVGResourcePaintServer`
  interface + `SVGLinearGradient`, `SVGRadialGradient`, `SVGPattern`.
- `pkg/render/svg_object_painter.go` — extend `PreparePaint` to call the paint
  server when `fill`/`stroke` is `url(#id)`.
- `pkg/render/gradient.go` — reuse louis14's existing gradient ramp/rasterizer
  (`pkg/render/gradient.go` already paints CSS gradients); the SVG paint server
  maps its stops onto that machinery so SVG gradients pixel-align with CSS
  gradients.

**New types.**
- `svg.SVGResourcePaintServer` interface — `ApplyPaint(refBox geometry.RectF,
  additional geometry.AffineTransform) render.PaintShader`.
- `svg.SVGLinearGradient` / `svg.SVGRadialGradient` / `svg.SVGPattern`.
- `svg.SVGGradientStop{Offset float64; Color css.Color; Opacity float64}`.
- `svg.SVGUnitType` ∈ `{ObjectBoundingBox, UserSpaceOnUse}` — shared by all
  resource elements (gradients, `<mask>`, `<clipPath>`, `<filter>`).

**Approach.**
1. Resource elements (`<linearGradient>` etc.) are built by `BuildSVGTree` into
   `SVGResourcePaintServer` nodes but are **not** added to the paint-order child
   list (Blink: `LayoutSVGHiddenContainer` — never painted in tree order). They
   are registered in the `SVGResourceRegistry` (Phase 6 formalizes the registry;
   for now a simple `map[string]SVGNode` keyed by `id`).
2. `SVGObjectPainter.PreparePaint`: a `url(#id)` fill/stroke → look up the paint
   server, call `ApplyPaint(referenceBox, gradientTransform)`. `objectBoundingBox`
   units → the gradient coordinate space is the shape's `fillBoundingBox`
   (0..1 normalized, then scaled/translated); `userSpaceOnUse` → current user
   space. The resulting shader feeds `DrawContext`.
3. `<stop>` children supply the ramp; map onto `pkg/render/gradient.go`'s
   existing stop model.

**Tests fixed.** `filter-effects` bucket-I tests that use gradient/pattern
fills on SVG content.

**Gate.** A hand-checked `<rect fill="url(#linearGradient)">` paints a
pixel-correct ramp identical to the equivalent CSS `linear-gradient`. Prior
gates 0%.

### Phase 5: `<clipPath>` + `<mask>` resource elements

**Goal.** `<clipPath>` and `<mask>` resource elements resolve and apply:
`clip-path: url(#id)` clips SVG (and CSS) content to the clipper's geometry;
`mask`/`mask-image: url(#id)` rasterizes the `<mask>`'s children at luminance
and composites with `kDstIn`.

**Blink reference.**
- `core/layout/svg/layout_svg_resource_clipper.{h,cc}` —
  `LayoutSVGResourceClipper`, `CreateClipPath()` / `AsPath()` (shape-based
  clip), `CreatePaintRecord()` (content-based clip), `ClipPathUnits()`.
- `core/layout/svg/layout_svg_resource_masker.{h,cc}` —
  `LayoutSVGResourceMasker`, `CreatePaintRecord()`, `ResourceBoundingBox()`,
  `MaskUnits()`/`MaskContentUnits()`.
- `core/paint/svg_mask_painter.{h,cc}` — `SVGMaskPainter`,
  `ResolveElementReference()`, `MaskToContentTransform()`, clip to
  `ResourceBoundingBox()`, **luminance** (`cc::ColorFilter::MakeLuma()`) vs
  **alpha** per `EMaskType` (SVG `<mask>` defaults to `mask-type: luminance`).
- `core/paint/clip_path_clipper.cc` — `ReferenceClipPathOperation` resolution
  (the `url(#id)` clip-path variant).
- `core/style/clip_path_operation.h` — `ReferenceClipPathOperation`.

**louis14 target files.**
- New `pkg/layout/svg/svg_resource_clipper.go` — `SVGResourceClipper`.
- New `pkg/layout/svg/svg_resource_masker.go` — `SVGResourceMasker`.
- New `pkg/render/svg_mask_painter.go` — mirrors `svg_mask_painter.cc`:
  rasterize a `<mask>`'s children into a luminance buffer.
- New `pkg/render/svg_clip_painter.go` — mirrors `clip_path_clipper.cc` for the
  reference case.
- `pkg/render/render.go` — `paintLayerWithMask` (`render.go:515`) gains an SVG
  branch: `url(#id)` whose target is an SVG `<mask>` → call the new
  `svg_mask_painter`. **This is css-masking Phase 3's "SVG `<mask>` resolution"
  — that work moves here.**
- `pkg/css/style.go` — `clip-path: url(#id)` parsing (the
  `ReferenceClipPathOperation` variant) alongside the existing
  `circle()/ellipse()/polygon()` parsing (`style.go:7612`).

**New types.**
- `svg.SVGResourceClipper` — `ClipPathUnits SVGUnitType`, `AsPath() (geometry.Path, bool)`,
  `CreatePaintRecord()`.
- `svg.SVGResourceMasker` — `MaskUnits`, `MaskContentUnits SVGUnitType`,
  `MaskType` (luminance/alpha), `ResourceBoundingBox(refBox) geometry.RectF`,
  `Rasterize(refBox) *image.RGBA`.
- `render.SVGMaskPainter`, `render.SVGClipPainter`.

**Approach.**
1. `<clipPath>` / `<mask>` build into `SVGResourceClipper` / `SVGResourceMasker`
   nodes, registered in the `SVGResourceRegistry`, **not** in paint order.
2. `SVGMaskPainter`: resolve `url(#id)` → `SVGResourceMasker`; render its child
   shapes (reusing `SVGShapePainter`) into an offscreen buffer at
   `MaskToContentTransform` (objectBoundingBox → translate+scale to the
   reference box); take **luminance** of the buffer (reuse the luminance branch
   already in `paintLayerWithMask`, `render.go:624-637`); composite `kDstIn`.
   This is the exact capability css-masking Phase 3 specifies — built once here.
3. `SVGClipPainter`: a shape-based `<clipPath>` → `AsPath()` returns a
   `geometry.Path` fed to `DrawContext.Clip()`; a content-based clipper →
   rasterize-as-mask fallback. `clip-path: url(#id)` on a CSS box reuses the
   stacking-context trigger css-masking Phase 1 adds.
4. `svg-filter-vs-clip-path` / `svg-filter-vs-mask` (filter-effects bucket I)
   exercise the clip↔mask↔filter ordering — Phase 7 wires the filter side; the
   ordering rule (clip, then mask, then filter applied to the result, per
   SVG 1.1 §3.4) is implemented in the SVG paint walk here for clip+mask, with
   the filter slot left for Phase 7.

**Tests fixed.** `css-masking/mask-opacity-1d` (SVG `<mask>` resolution — moved
from css-masking Phase 3). `filter-effects` bucket-I clip/mask tests'
non-filter half.

**Gate.** `mask-opacity-1d` passes at 0% diff (with css-masking's
opacity-triggered SC, which already exists). A hand-checked
`<rect clip-path="url(#c)">` clips correctly. Prior gates 0%.

### Phase 6: `url(#id)` reference resolution — the shared resolver

**Goal.** A single, Blink-faithful `url(#id)` resolution mechanism — the
`SVGResourceRegistry` — that any consumer (SVG content, css-masking,
filter-effects) uses to go from a CSS/presentation-attribute reference to an SVG
resource DOM node, with cycle detection.

**Blink reference.**
- `core/svg/svg_resource.{h,cc}` — `SVGResource`, `LocalSVGResource`
  (`url(#id)` within a `TreeScope`, `IdTargetObserver`, `TargetChanged()`),
  `ExternalSVGResource*` (`ResolveTarget()`), `AddClient`/`RemoveClient`,
  `FindCycle()` (`kNeedCheck/kPerformingCheck/kHasCycle/kNoCycle`),
  `NotifyContentChanged()`.
- `core/svg/svg_resources.{h,cc}` — `SVGResources`, `SVGElementResourceClient`.
- `core/style/clip_path_operation.h` — `ReferenceClipPathOperation`.

**louis14 target files.**
- New `pkg/layout/svg/svg_resource.go` — `SVGResourceRegistry`,
  `SVGResourceReference`, cycle detection.
- Refactor the Phase 4/5 ad-hoc `map[string]SVGNode` into the registry.
- `pkg/render/render.go` / `pkg/render/paint_layer.go` — the renderer threads
  the document root (or the registry) so `paintLayerWithMask` and the SVG
  painters can resolve `#id` (css-masking Phase 3 "Key Question 1" — answered
  here: a single registry built once at layout time and carried on the
  `Renderer`).
- `pkg/css/style.go` — a shared `ParseURLReference(value string) (id string, ok bool)`
  helper so `mask`, `clip-path: url()`, `filter: url()`, `fill: url()` all parse
  the reference uniformly.

**New types.**
- `svg.SVGResourceRegistry` — `Resolve(id string) (SVGNode, bool)`,
  `ResolveTyped(id string, want SVGResourceType) (SVGNode, bool)`, built once by
  walking the document for elements with `id` that are SVG resource elements.
- `svg.SVGResourceReference` — a parsed `url(#id)` reference + the resolved
  target; carries the cycle-check state.
- `svg.SVGResourceType` ∈ `{Masker, Clipper, Filter, LinearGradient,
  RadialGradient, Pattern, Marker}` — mirrors Blink's `LayoutSVGResourceType`.

**Approach.**
1. Build the registry once during layout-tree construction: walk all DOM nodes,
   index every SVG resource element by `id`. The registry is carried on the
   `Renderer` and on the `SVGPaintContext`.
2. Every consumer routes through `Resolve`/`ResolveTyped`: SVG fill/stroke paint
   servers (Phase 4), `<mask>`/`<clipPath>` (Phase 5), `<filter>` (Phase 7),
   and css-masking's CSS `mask`/`clip-path`. `ResolveTyped` returning `(_, false)`
   for a missing or wrong-typed id is the "unresolvable reference → not
   applied" behavior (SVG 1.1 §14.4 / css-masking's existing `maskImg == nil`
   fallback).
3. `FindCycle` — a `<mask>` referencing itself, a gradient `href` chain, etc.
   `kPerformingCheck` during the walk → `kHasCycle` → the reference is dropped.

**Tests fixed.** None new directly — this phase *unifies* the resolution that
Phases 4–5 used ad-hoc and that Phase 7 needs; it is the seam css-masking and
filter-effects consume.

**Gate.** Phases 4–5 tests still 0% after the registry refactor. A cyclic
`<mask>` reference does not infinite-loop. `go vet` clean.

### Phase 7: `<filter>` element host — wire into `pkg/graphics/filters/`

**Goal.** The SVG `<filter>` element + its `fe*` children are modeled as
resource elements and built into a `pkg/graphics/filters/` `Filter` graph via a
`SVGFilterBuilder`. `filter: url(#id)` on an HTML element, and `filter` on SVG
content, both resolve through the Phase 6 registry and feed the existing
FilterEffect graph.

**Blink reference.**
- `core/svg/graphics/filters/svg_filter_builder.{h,cc}` — `SVGFilterBuilder`,
  `BuildGraph(Filter*, SVGFilterElement&, viewport, override)`, `GetEffectById`
  resolution order (`builtin_effects_` → `named_effects_` → `last_effect_`),
  `AddBuiltinEffects()`, `SVGFilterGraphNodeMap`,
  `ResolveInterpolationSpace(EColorInterpolation)`.
- `core/svg/svg_filter_element.h`, `core/svg/svg_fe_*_element.{h,cc}` — the
  `<filter>` and `fe*` element classes; `filterUnits` (`objectBoundingBox`
  default), `primitiveUnits` (`userSpaceOnUse` default); filter region defaults
  `x/y/width/height` = `-10% -10% 120% 120%`.
- `core/layout/svg/layout_svg_resource_filter.{h,cc}` —
  `LayoutSVGResourceFilter`, `ResourceBoundingBox`.
- `core/paint/filter_painter.cc` — how the resolved filter graph is applied as
  a paint effect.

**louis14 target files.**
- New `pkg/layout/svg/svg_resource_filter.go` — `SVGResourceFilter` (the
  `<filter>` element model: `filterUnits`, `primitiveUnits`, region defaults,
  the `fe*` child list).
- New **`pkg/graphics/filters/svg_filter_builder.go`** — `SVGFilterBuilder`,
  mirroring `core/svg/graphics/filters/svg_filter_builder.{h,cc}`. **This is the
  ONLY file this foundation adds to `pkg/graphics/filters/`** — it consumes the
  FilterEffect graph (`filters.Filter`, `filters.FEColorMatrix`, …) that
  `docs/plan-filter-effects.md` Phases 1–3 own; it does not reimplement it.
- `pkg/render/filter_effect_builder.go` (created by filter-effects) — gains a
  `BuildReferenceFilter` path that calls `SVGFilterBuilder` when a
  `filter: url(#id)` resolves to an `SVGResourceFilter`. (If filter-effects has
  not landed Phase 6 yet, this is the integration point both plans meet at.)
- `pkg/css/style.go` — `filter: url(#id)` parsing (the
  `ReferenceFilterOperation` variant); `color-interpolation-filters`,
  `flood-color`, `flood-opacity` accessors.

**New types.**
- `svg.SVGResourceFilter` — `filterUnits`, `primitiveUnits SVGUnitType`,
  `region` (with the `-10%/120%` defaults), `feChildren []*html.Node` (the
  `fe*` element nodes, kept in DOM form — the FilterEffect graph is built from
  them on demand).
- `filters.SVGFilterBuilder` — `BuildGraph(filterNode *html.Node, sourceGraphic
  filters.FilterEffect, referenceBox geometry.RectF) *filters.Filter`;
  `getEffectByID(id string) filters.FilterEffect` with the builtins →
  named-results → last-effect order.

**Approach.**
1. `<filter>` builds into an `SVGResourceFilter` resource node (registered,
   not painted in tree order). Its `fe*` children are kept as DOM nodes —
   the FilterEffect graph is built lazily by `SVGFilterBuilder.BuildGraph` when
   the filter is actually referenced.
2. `SVGFilterBuilder.BuildGraph` iterates the `fe*` children, building each into
   the corresponding `filters.FE*` node (from `pkg/graphics/filters/`), wiring
   `in`/`in2`/`result` via `getEffectByID` (builtins `SourceGraphic`/
   `SourceAlpha` → named `result=` outputs → `last_effect_` fallback). Resolves
   the filter region from `filterUnits` + the reference box, with the
   `-10%/120%` defaults. Sets each effect's interpolation space from
   `color-interpolation-filters` (default linearRGB).
3. **Consumers:** `filter: url(#id)` on an HTML element →
   `render.FilterEffectBuilder` (filter-effects' code) resolves the id via the
   Phase 6 registry, calls `SVGFilterBuilder.BuildGraph`, applies the graph —
   this is filter-effects bucket H. `filter` on SVG content → the SVG paint walk
   does the same — filter-effects bucket I. The `fe*` primitive *correctness*
   (bucket J) is entirely `pkg/graphics/filters/`'s concern (filter-effects
   Phase 8) — out of scope here.
4. The clip→mask→filter ordering slot left open in Phase 5 is filled: the SVG
   paint walk applies clip, then mask, then filter to the masked+clipped result
   (SVG 1.1 §3.4).

**Tests fixed.** Unblocks `filter-effects` buckets H and I (the SVG-element
side) — those tests pass once filter-effects' own FilterEffect-graph phases are
also landed. No `filter-effects` test passes from *this* phase alone, because
the primitive correctness lives in `pkg/graphics/filters/`.

**Gate.** A hand-checked `filter: url(#f)` with a single `feFlood` resolves,
builds a one-node graph, and applies it (verified against a trivial
`pkg/graphics/filters/` `FEFlood` if filter-effects Phase 3 has landed; else
the gate is "the graph is built with the correct node count and wiring",
verified by a `pkg/graphics/filters` unit test). Prior gates 0%.

### Phase 8: Delivery & cross-plan handoff

**Goal.** Confirm the SVG foundation is whole and hand off to the three
dependent plans.

- Re-run the SVG-relevant subset across the categories (sanctioned
  end-of-foundation run, scoped to SVG tests only — *not* the full categories):
  `css-masking/mask-image-1d`, `css-masking/mask-opacity-1d`,
  `css-animations/svg-transform-animation`, a sample of `filter-effects`
  bucket-I `svg-*` shape/`<g>`/gradient tests. Expect every test whose only
  dependency is the SVG render tree (not the FilterEffect-graph correctness) to
  pass at 0% diff.
- Regression spot-check (adjacent only, CLAUDE.md §4): css-position,
  css-flexbox, CSS2 — the `<svg>`-box embedding change is the highest-risk item;
  confirm `<svg>`-as-`<img>` (`pkg/images/loader.go` oksvg path) is untouched.
- Confirm the seams are clean: `SVGResourceRegistry` is the single `url(#id)`
  resolver; `SVGFilterBuilder` is the single SVG-`<filter>`→graph entry; the
  `pkg/graphics/filters/` FilterEffect graph is untouched by this foundation
  except for the one `svg_filter_builder.go` consumer file.
- Update the three dependent plans per the **Downstream impact** section below.
- Final report: which SVG tests now pass, the API surface (`pkg/layout/svg/`,
  the `SVGResourceRegistry`, `SVGFilterBuilder`) the dependent plans consume,
  and the trimmed scope of each.

**Gate.** All SVG-render-tree-only tests pass at 0% diff. CSS2 unchanged. No
regression in the css-position / css-flexbox samples. `<img src=*.svg>` /
`background-image: url(*.svg)` (oksvg path) unchanged.

---

## Out-of-scope deferrals (explicit, with reasons)

Decided by what the three dependent plans actually need:

- **SVG text (`<text>`, `<tspan>`, `<textPath>`).** None of css-masking's 6
  failures, css-animations' `svg-transform-animation`, or the *foundation-level*
  filter-effects work needs SVG text. filter-effects bucket I lists `blur-text`
  but that is "a CSS/data-URL filter on *HTML* text" (filter-effects Phase 6),
  not SVG `<text>`. Deferred — a `<text>` element is a no-op SVG node for now.
  Add as a follow-up plan if a later category needs it.
- **SMIL animation (`<animate>`, `<animateTransform>`, `<set>`,
  `<animateMotion>`).** css-animations' one SVG test
  (`svg-transform-animation`) is a **CSS** animation/transition on an SVG
  element, sampled by the existing css-animations machinery — it needs the SVG
  `<g transform>` *target* (Phase 3), not SMIL. SMIL is a separate timed-content
  subsystem; deferred.
- **`<use>`, `<symbol>`, `<switch>`, `<image>`, `<foreignObject>`,
  `<marker>`.** Not required by any of the three dependent plans' foundation
  needs. `<marker>` painting is touched by filter-effects bucket I only
  indirectly; if needed it slots in next to the resource model — deferred with
  a clean extension point (`SVGResourceType` already reserves `Marker`).
  `<foreignObject>` (re-entering HTML layout inside SVG) is filter-effects
  bucket I's hardest item and is explicitly *its* Phase 7 scope, not the
  foundation's — deferred here, the foundation provides the `SVGContainer` it
  would attach to.
- **`feDiffuseLighting`/`feSpecularLighting` 3-D lighting, `feTurbulence` PRNG,
  `feConvolveMatrix`, `feDisplacementMap` edge cases.** These are
  `pkg/graphics/filters/` primitive-correctness concerns —
  `docs/plan-filter-effects.md` Phase 8 (bucket J) owns them. This foundation
  builds the `<filter>` element *host* and the `SVGFilterBuilder`; the
  primitive algorithms are out of scope.
- **Switching `<img src=*.svg>` / `background-image: url(*.svg)` onto the new
  SVG tree.** The oksvg/rasterx path in `pkg/images/loader.go` is correct and
  passing for *image* SVGs; rewriting it onto the Blink-faithful tree is a
  consistency improvement, not a dependency of any paused plan. Deferred — keep
  oksvg for image SVGs, the new tree for inline SVG. (Revisit only if an
  image-SVG test starts failing in a way that needs per-element control.)
- **SVG hit testing, `pointer-events`, SVG DOM scripting APIs
  (`getBBox`, `SVGLength` IDL).** No reftest in the three dependent plans needs
  them (reftests are static paint comparisons). Deferred.
- **`color-interpolation` (non-filters) and full SVG color management.** The
  foundation handles `color-interpolation-filters` (Phase 7, needed by the
  filter graph) but not the broader SVG color pipeline. Deferred.

---

## Downstream impact — what each dependent plan can drop or simplify

Once this foundation lands, the three plans are trimmed as follows. Each entry
says precisely which of *their* buckets/phases this foundation **replaces**
(delete) or **unblocks/simplifies** (reduce to a thin change on top of the
landed SVG subsystem).

### `docs/plan-css-masking.md`

- **Phase 3 ("SVG mask resolution + multi-layer mask list") — SVG half
  REPLACED.** This foundation's Phase 5 builds the SVG `<mask>` resource element,
  `SVGMaskPainter`, luminance rasterization, and the `url(#id)` resolution
  (Phase 6). css-masking Phase 3 collapses to its **CSS** half: the multi-layer
  `mask-image` comma-list parsing (`GetMaskLayers`) and intersecting multiple
  mask layers — pure CSS-side work. The "look up the `<mask>` DOM element and
  rasterize its children at luminance" sub-task is done by the foundation; the
  "Key Question 1" (threading the document root for `url(#id)` resolution) is
  answered by the foundation's `SVGResourceRegistry`.
- **Phase 4 ("Basic SVG shape painting + SVG `mask=` attribute") — REPLACED.**
  This foundation's Phase 2 paints SVG `<rect>` (and every other basic shape)
  with `fill`/`stroke`; the SVG `mask=` presentation attribute maps through the
  same `applyPresentationalAttributes` path the foundation extends. css-masking
  Phase 4 collapses to "verify `mask-image-1d` passes on the landed
  foundation" — its `<rect>` paints, the unresolvable `mask="url(#foo)"` →
  not-masked behavior is the foundation's `ResolveTyped → (_, false)` plus the
  existing `maskImg == nil` fallback.
- **Phases 1–2 (stacking-context trigger + clip-path geometry audit) —
  UNCHANGED.** Pure CSS-side work (`CreatesStackingContext`, `basic_shapes.cc`
  mirroring); no SVG dependency. *Exception:* css-masking Phase 1 also makes a
  CSS box with `clip-path: url(#id)` a stacking context — that `url(#id)` clip
  now resolves through the foundation's Phase 6 registry + Phase 5
  `SVGClipPainter`, a small simplification.
- **Net:** css-masking drops Phase 4 entirely, halves Phase 3 (CSS multi-layer
  only), keeps Phases 1–2, 5. The SVG-shaped 3 of its 6 failures
  (`mask-image-1d`, `mask-opacity-1d`, and the SVG-shape parts) are addressed
  by the foundation.

### `docs/plan-filter-effects.md`

- **Phase 7 ("SVG layout & paint tree + filters on SVG elements", bucket I,
  ~32 tests) — REPLACED.** This foundation *is* filter-effects Phase 7's SVG
  render tree: `pkg/layout/svg/` (SVGRoot/container/shape/transform), the SVG
  paint walk, paint servers, `<clipPath>`/`<mask>`. filter-effects Phase 7
  collapses to "apply the FilterEffect graph to SVG content" — i.e. wiring
  `filter` on an SVG element through the foundation's Phase 7 `SVGFilterBuilder`
  + filter-effects' own graph. `svg-filter-vs-clip-path` / `svg-filter-vs-mask`
  ordering is implemented in the foundation's SVG paint walk (Phase 5 + 7).
- **Phase 6 ("SVG `<filter>` element model + reference filters on HTML",
  bucket H, ~25 tests) — REPLACED at the element-model layer.** This
  foundation's Phase 7 builds the `SVGResourceFilter` element model and the
  `SVGFilterBuilder` (the `pkg/graphics/filters/svg_filter_builder.go` Blink
  calls out), plus the `url(#id)` resolution. filter-effects Phase 6 collapses
  to "`filter: url(#id)` on an HTML element calls the foundation's
  `SVGFilterBuilder` and applies the resulting graph" — the `FilterRegion`
  resolution and external/`data:` URL fetching stay with filter-effects.
- **Phases 1–3 (`colorspace.go`, the FilterEffect graph, CSS filter functions,
  drop-shadow) — UNCHANGED, and are this foundation's *upstream*.** The
  foundation's Phase 7 *consumes* `pkg/graphics/filters/`. **Land order:**
  filter-effects Phases 1–3 (the FilterEffect graph) and this foundation's
  Phases 1–6 (the SVG render tree) are independent — either order. The
  foundation's Phase 7 needs filter-effects Phase 3 landed (or stubs the graph
  edge). Recommended: land filter-effects 1–3 and the foundation 1–6 in
  parallel, then the foundation's Phase 7, then filter-effects 6–8.
- **Phase 8 (bucket J, `fe*` primitive correctness) — UNCHANGED.** Entirely
  `pkg/graphics/filters/` — the foundation builds the `<filter>` *host*, not the
  primitive algorithms.
- **Net:** filter-effects drops its entire Phase 7, replaces Phase 6's
  element-model layer (keeping region/external-URL handling), and keeps
  Phases 1–5, 8. ~57 of its 178 failures (buckets H+I) are unblocked by the
  foundation; bucket J (~33) still needs filter-effects' own Phase 8.

### css-animations

- **`svg-transform-animation` — UNBLOCKED.** The foundation's Phases 1–3 give
  the inline-`<svg>` layout + paint + the `<g transform>` transformable
  container the animation targets. css-animations keeps its own animation
  sampling (computing the `transform` value at the paused/sampled time); it just
  now has a real SVG `<g>` to apply it to. No css-animations phase is replaced —
  one previously-impossible test becomes a normal CSS-transform-on-an-SVG-element
  test.
- **Net:** css-animations gains 1 test; no scope change beyond "remove the
  `svg-transform-animation` blocker note".

### Recommended land order

1. **`docs/plan-svg-foundation.md`** (this plan) Phases 1–6 — the SVG render
   tree, paint, coordinate model, paint servers, `<clipPath>`/`<mask>`,
   `url(#id)` resolver. Lands on `master` via a `fix/*` branch. Can run in
   parallel with filter-effects Phases 1–3 (no shared files until Phase 7).
2. **`docs/plan-filter-effects.md` Phases 1–3** — the `pkg/graphics/filters/`
   FilterEffect graph + CSS filter functions. Independent of the foundation's
   Phases 1–6.
3. **`docs/plan-svg-foundation.md` Phase 7** — wire the SVG `<filter>` element
   model into the (now-landed) FilterEffect graph. Lands after both (1) and (2).
4. **`docs/plan-css-masking.md`** — its trimmed Phases 1–3 (CSS half), 5; Phase 4
   becomes a verification pass. Can start after the foundation's Phase 5.
5. **`docs/plan-filter-effects.md` Phases 4–6, 8** — backdrop-filter, the
   trimmed Phase 6, bucket-J primitive correctness.
6. **css-animations** — re-enable `svg-transform-animation` once the
   foundation's Phase 3 is landed.

---

## Key Blink files this plan is grounded in

- `core/layout/svg/README.md` — the SVG layout architecture overview.
- `core/layout/svg/layout_svg_root.{h,cc}` — `LayoutSVGRoot : LayoutReplaced`,
  the HTML↔SVG embedding point, `BuildLocalToBorderBoxTransform`, `LayoutRoot`.
- `core/layout/svg/layout_svg_container.{h,cc}` — `LayoutSVGContainer`,
  `UpdateSVGLayout`, `UpdateLocalTransform`, `ObjectBoundingBox`.
- `core/layout/svg/layout_svg_transformable_container.{h,cc}` —
  `LayoutSVGTransformableContainer` (`<g>`), `local_transform_`.
- `core/layout/svg/layout_svg_viewport_container.{h,cc}` —
  `LayoutSVGViewportContainer` (nested `<svg>`), `viewport_`.
- `core/layout/svg/layout_svg_shape.{h,cc}` + `layout_svg_rect.{h,cc}` /
  `layout_svg_ellipse.{h,cc}` / `layout_svg_path.{h,cc}` — `LayoutSVGShape`,
  `path_`, `fill_bounding_box_`, `GeometryType`, `UpdateShapeFromElement`.
- `core/layout/svg/svg_layout_info.h` — `SVGLayoutInfo` / `SVGLayoutResult`.
- `core/layout/svg/transform_helper.{h,cc}` — `TransformHelper::ComputeTransform`,
  `ComputeReferenceBox`, `ComputeTransformOrigin`.
- `core/layout/svg/layout_svg_resource_container.{h,cc}` —
  `LayoutSVGResourceContainer : LayoutSVGHiddenContainer`, `LayoutSVGResourceType`
  enum, the invalidation model.
- `core/layout/svg/layout_svg_resource_masker.{h,cc}` /
  `layout_svg_resource_clipper.{h,cc}` / `layout_svg_resource_filter.{h,cc}` /
  `layout_svg_resource_paint_server.{h,cc}` (+ gradient/pattern subclasses) —
  the four resource families.
- `core/svg/svg_length_context.{h,cc}` — `SVGLengthContext`, `SVGLengthMode`,
  `ConvertValueToUserUnits`, `ViewportSize`.
- `core/svg/svg_svg_element.cc` — `ViewBoxToViewTransform`;
  `SVGPreserveAspectRatio::ComputeTransform`.
- `core/svg/svg_resource.{h,cc}` — `SVGResource` / `LocalSVGResource` /
  `ExternalSVGResource*`, `IdTargetObserver`, `FindCycle`, the
  `<event> → SVG*Element → SVGResource → SVGResourceClient` notification flow.
- `core/svg/svg_resources.{h,cc}` — `SVGResources`, `SVGElementResourceClient`.
- `core/svg/graphics/filters/svg_filter_builder.{h,cc}` — `SVGFilterBuilder`,
  `BuildGraph`, `GetEffectById` (builtins → named → last), `SVGFilterGraphNodeMap`,
  `ResolveInterpolationSpace`.
- `core/svg/svg_filter_element.h`, `core/svg/svg_fe_*_element.{h,cc}` — the
  `<filter>`/`fe*` element classes, `filterUnits`/`primitiveUnits`, the
  `-10%/120%` region defaults.
- `core/paint/svg_root_painter.{h,cc}` — `SVGRootPainter::PaintReplaced`.
- `core/paint/svg_container_painter.{h,cc}` — `SVGContainerPainter::Paint`, the
  `ScopedSVGTransformState` step sequence.
- `core/paint/svg_shape_painter.{h,cc}` — `SVGShapePainter`, fill→stroke→markers.
- `core/paint/svg_object_painter.{h,cc}` — `SVGObjectPainter::PreparePaint`,
  paint-server resolution.
- `core/paint/svg_mask_painter.{h,cc}` — `SVGMaskPainter::ResolveElementReference`,
  `MaskToContentTransform`, luminance vs alpha.
- `core/paint/clip_path_clipper.cc` — `ReferenceClipPathOperation` resolution.
- `core/style/clip_path_operation.h` — `ReferenceClipPathOperation`.

## Notes

- Test command template:
  `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/<category>/<name>' -v`
- Pre-rendered diff artifacts: `output/reftests/<name>_{diff,ref,test}.png`.
- All geometry in `pkg/geometry` / `pkg/geometry/layoutunit` per the Phase-13
  precision discipline (`docs/findings-multicol-archive.md`). SVG user-space
  coordinates are floating-point by spec; resolve to `LayoutUnit` only at the
  paint boundary, mirroring Blink's `gfx::RectF` (SVG) vs `LayoutUnit` (CSS box)
  split.
- Per memory `feedback_blink_file_placement`: SVG layout → `pkg/layout/svg/`
  (mirrors `core/layout/svg/`); SVG paint → `pkg/render/svg_*.go` (mirrors
  `core/paint/svg_*`); the SVG `<filter>` element builder →
  `pkg/graphics/filters/svg_filter_builder.go` (mirrors
  `core/svg/graphics/filters/`).
- oksvg/rasterx (`pkg/images/loader.go`) stay for `<img>`/`background-image` SVG
  *images* — untouched by this foundation.
- Worktree agents: symlink `fonts/` from the main dir before any broad run
  (memory `feedback_worktree_fonts`); `go fmt` before and after Go edits
  (memory `feedback_gofmt_after_edits`); commit + report at each phase milestone
  (memory `feedback_agent_checkpoints`); `git push` includes the branch name
  (CLAUDE.md §5).
