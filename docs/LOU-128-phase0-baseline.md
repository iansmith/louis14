# LOU-128 — Phase 0 baseline & embedding-seam confirmation

Foundation plan: `docs/plan-svg-foundation.md` §"Phase 0".
Branch: `worktree-agent-a506e0a094bc4a77d` (from `feat/LOU-128-svg-foundation`).

## Goal

No code changes. Confirm at runtime that inline `<svg>` children never paint,
and confirm by reading source that the HTML↔SVG embedding seam is where the
findings doc and plan say it is.

## Three representative tests rendered

Each rendered once (CLAUDE.md §4 "1–4 tests"):

| Test | Status | What it tells us |
|---|---|---|
| `filter-effects/svg-feflood-001.html` | PASS @ 0 diff | Test has `<svg><rect fill="red" filter="url(#filter)"/></svg>`; reference has `<svg><rect fill="black"/></svg>`. Both contain inline-SVG content; both render the SVG content area entirely blank. Pixel-identical → 0 diff. Direct evidence that inline `<svg>` children never paint (in either the test or reference). |
| `css-masking/mask-image-1d.html` | FAIL: 5000/480000 pixels differ (1.0%, max diff 255) | Test has `<svg width=100 height=100><rect width=100 height=50 fill="purple" mask="url(#foo)"/></svg>`; reference is `<div style="width:100px;height:50px;background:purple">`. Test renders blank SVG (5000 pixels = 100×50 missing purple rect); reference renders the purple block via HTML. Diff is exactly the missing rect. `mask-image-1d_test.png` inspected: visibly all-white (no purple square present). |
| `filter-effects/filter-subregion-01.html` | PASS @ 0 diff | Both test and reference contain inline `<svg>` with `<g transform="translate(...)">` plus `<rect>`/`<line>`/`<circle>` children and a `<filter>` resource. Both render the SVG content area blank → 0 diff. Direct evidence that `<g transform>` children, like shape children, never paint (no transform application happens because no shape paint happens). |

## Embedding seam — verified by source reading (no debug prints needed)

Per `findings.md` and confirmed by direct file reads at this commit:

1. **Outer sizing (UNCHANGED in Phase 1):**
   `pkg/layout/intrinsic_sizing.go:27` — `GetIntrinsicSizingInfo` dispatches
   `case "svg"` → `getInlineSVGIntrinsicInfo` (`:88-160`), which parses
   `width`/`height`/`viewBox` attributes and yields intrinsic size + aspect
   ratio. Correct CSS-replaced-element sizing.

2. **Replaced layout used inline by `BlockLayoutAlgorithm`:**
   `pkg/layout/block_layout.go:160` — `isReplacedElement(node.DOMNode)` is
   detected; `:166` and `:180` call `ComputeReplacedSize`. The `<svg>` box is
   sized but its children are not laid out as SVG.

   Note: the standalone `ReplacedLayoutAlgorithm` type defined at
   `pkg/layout/replaced_layout.go:234` is currently **dead code** — `grep`
   finds no callers anywhere in `pkg/` or `cmd/`. The plan's wording
   ("find where `NewReplacedLayoutAlgorithm` is constructed for `<svg>`") is
   slightly stale; in reality replaced-element layout is inlined in
   `BlockLayoutAlgorithm.Layout()` plus the atomic-inline machinery. The actual
   Phase-1 dispatch seam is therefore `layoutElement` at
   `pkg/layout/block_layout.go:2498-2535`, where a tagname check can route
   `<svg>` to `SVGRootAlgorithm` before the display-based switch.

3. **Children become inert `LayoutInputNode`s:**
   `pkg/layout/layout_tree_builder.go:148-165` — `buildNode` recurses into all
   DOM children unconditionally. So `<rect>`/`<g>`/`<mask>`/`<filter>` etc.
   *do* exist as `LayoutInputNode`s under the `<svg>` node — they carry no
   SVG semantics and no layout algorithm consumes them.

4. **`<svg>` produces no paint:**
   `pkg/render/paint_layer.go:367-369` sets `ImageSrc` only for
   `box.Node.TagName == "img"`. `pkg/render/render.go:1361` →
   `paintSelfForeground` → `drawImage(layer)` only paints when
   `layer.ImageSrc != ""`. So an inline `<svg>` element paints **nothing at
   all** — exactly the blank content area visible in `mask-image-1d_test.png`.

## Phase 0 gate

- Scope confirmed: no SVG render subsystem exists for inline SVG. ✓
- Embedding seam matches plan + findings doc (with the staleness correction
  on `ReplacedLayoutAlgorithm` documented above). ✓
- No code changed. ✓

Proceed to Phase 1.
