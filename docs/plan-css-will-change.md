# Task Plan: Pass the entire css-will-change WPT reftest category

## Goal
All `css-will-change` tests under `pkg/visualtest/testdata/wpt-css3/css-will-change/` pass at 0%
pixel diff via `TestWPTCSS3Reftests/css-will-change`. Baseline **17 passing / 30 failing (47 run)**
→ close the 30 failures without regressing adjacent categories (css-position, css-flexbox,
css-transforms, css-masking, css-contain, css-writing-modes, CSS2).

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff
   required, test-execution discipline (only the failing tests + regression-adjacent), operational
   rules (no `open`, commit before worktree agents, worktree commit scope, `git push` needs an
   explicit branch).
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory
   index pointing at the same rules, plus `feedback_gofmt_after_edits.md` (run `go fmt` before and
   after Go edits) and `reference_go_toolchain.md` (`GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go`).

If you are about to type a rule verbatim into this file or into code comments, stop and link instead.

## Framing — what this category actually tests
`will-change` paints **nothing** itself. Per [CSS Will Change Level 1
§3](https://drafts.csswg.org/css-will-change-1/#will-change), naming a property in `will-change`
must reproduce that property's *side effects* — specifically:

> "If any non-initial value of a property would create a stacking context on the element,
> specifying that property in `will-change` must create a stacking context on the element."
> "If any non-initial value of a property would cause the element to generate a containing block
> for fixed-position / absolute-position elements, specifying that property in `will-change` must
> cause the element to generate that containing block."

So every failure is louis14 not honoring one of two side effects: (1) **containing-block
generation** for abs/fixed-pos descendants, and (2) **stacking-context establishment**. The
fix is *not* to special-case `will-change` everywhere it is read today — it is to build one
canonical will-change value model and route the existing CB / stacking-context predicates
through it, mirroring how Blink's `StyleWillChangeData` feeds `ComputedStyle::HasWillChangeProperty`
which feeds `LayoutObject::ComputeIsFixedContainer` / `ComputeIsAbsoluteContainer` and the
stacking-context determination in `StyleAdjuster`.

## Baseline snapshot (2026-05-14)
From `grep "FAIL: TestWPTCSS3Reftests/css-will-change/" docs/reftest-survey-2026-05-14-raw.txt`
(30 fails), cross-referenced with the test HTML and the pre-rendered `output/reftests/*_test.png`.

### Bucket A — `will-change` → containing block for **absolute-pos** descendants (3 tests)
| Test | will-change value | Symptom in `*_test.png` |
|------|-------------------|--------------------------|
| will-change-abspos-cb-001.html | `position` | abspos child painted at viewport (0,0); should be inside the 100×100 container at margin-top 100. louis14 doesn't treat `will-change: position` as an abspos CB. |
| will-change-abspos-cb-003.html | `backdrop-filter` | red `.container` shows; abspos green child escaped to ICB instead of covering the container. |
| will-change-abspos-cb-dynamic-001.html | `position` set via JS `style.willChange` after load | same as -001 but applied dynamically; tests that a dynamic will-change change re-resolves the CB. |

### Bucket B — `will-change` → containing block for **fixed-pos** descendants (11 tests)
| Test | will-change value | Notes |
|------|-------------------|-------|
| will-change-fixedpos-cb-002.html | `filter` (on an **inline** `<span>`) | fixed child painted at (0,0) + "FAIL" text visible; `will-change: filter` on an inline must make it a fixed-pos CB. |
| will-change-fixedpos-cb-003.html | `filter` on **root** `<html>` | fixed child must NOT be re-contained — root `will-change: filter` does not create a fixed CB; child must still position against viewport and scroll-fix. |
| will-change-fixedpos-cb-004.html | `backdrop-filter` (block container) | fixed child must be contained by the container. |
| will-change-fixedpos-cb-005.html | `backdrop-filter` (on an **inline**) | inline fixed-pos CB. |
| will-change-fixedpos-cb-006.html | `backdrop-filter` on **root** | root exception — must NOT create fixed CB. |
| will-change-fixpos-cb-contain-1.html | `contain` | `will-change: contain` → fixed CB (and abspos CB). |
| will-change-fixpos-cb-offset-path-1.html | `offset-path` | `will-change: offset-path` → fixed CB. |
| will-change-fixpos-cb-position-1.html | `position` | `will-change: position` → abspos CB but **NOT** fixed CB; fixed child stays at viewport. |
| will-change-fixpos-cb-transform-style-1.html | `transform-style` | `will-change: transform-style` → fixed CB. |
| will-change-fixpos-cb-translate-1.html | `translate` | `will-change: translate` → fixed CB (individual transform property). |
| will-change-fixpos-cb-webkit-perspective-1.html | `-webkit-perspective` | alias of `perspective` → fixed CB; tests will-change ident alias handling. |

(Already passing in this bucket: `will-change-fixpos-cb-transform-1`, `-filter-1`, `-perspective-1`
— confirms `transform`/`filter`/`perspective` are partially wired at
`block_layout.go:1762-1767`; the new keywords are not.)

### Bucket C — `will-change` → **stacking context** (15 tests)
All follow one template: `#wc { will-change: <prop>; background: red }` wrapping
`#child { position: absolute; z-index: -1; background: green }`. If `#wc` is a stacking context,
the negative-z child paints *behind* `#wc`'s background → green covers red ⇒ pass. If not, the
child escapes to the root stacking context and paints behind the root ⇒ red `#wc` visible ⇒ fail.
| Test | will-change value | In `CreatesStackingContext` today? |
|------|-------------------|-------------------------------------|
| will-change-stacking-context-clip-path-1.html | `clip-path` | `clip-path` listed → but test still fails ⇒ predicate not consulted, or child not z-sorted vs a non-positioned non-z SC |
| will-change-stacking-context-isolation-1.html | `isolation` | listed; fails ⇒ same |
| will-change-stacking-context-mask-1.html | `mask` (shorthand) | `mask` listed; fails — `mask` shorthand vs `mask-image` longhand mismatch |
| will-change-stacking-context-mask-image-1.html | `mask-image` | NOT listed (`types.go:185` lists `mask`, not `mask-image`) |
| will-change-stacking-context-mix-blend-mode-1.html | `mix-blend-mode` | listed; fails |
| will-change-stacking-context-offset-path-1.html | `offset-path` | listed; fails |
| will-change-stacking-context-opacity-1.html | `opacity` | listed; fails |
| will-change-stacking-context-opacity-2.html | `opacity` on an **inline** `<span>` | listed, but the SC check is on a `Box` whose layout/paint-layer treats inlines differently |
| will-change-stacking-context-position-1.html | `position` | NOT listed — `will-change: position` must create a SC |
| will-change-stacking-context-transform-style-1.html | `transform-style` | NOT listed |
| will-change-stacking-context-translate-1.html | `translate` | NOT listed (only `transform` is) |
| will-change-stacking-context-view-transition-name-1.html | `view-transition-name` | NOT listed |
| will-change-stacking-context-z-index-1.html | `z-index` (+ `position: relative`) | NOT listed |
| will-change-stacking-context-z-index-2.html | `z-index` on a **flex item** | NOT listed |
| will-change-stacking-context-z-index-3.html | `z-index` on a **grid item** | NOT listed |

### Bucket D — harness / content-update singleton (1 test)
| Test | Notes |
|------|-------|
| will-change-transform-add-content.html | `*_test.png` is **blank**. Uses `class="reftest-wait"` + `/common/reftest-wait.js` + `/common/rendering-utils.js`, sets `target.textContent` in a `waitForAtLeastOneFrame().then()` then calls `takeScreenshot()`. The reftest runner is screenshotting before the deferred JS runs / before `reftest-wait` is cleared. Independent of will-change side effects — a harness timing bug. |

### Bucket fail counts
- **A** (abspos CB): 3
- **B** (fixed-pos CB): 11
- **C** (stacking context): 15
- **D** (harness/reftest-wait): 1
- **Total: 30**

## Root-cause analysis (done — read before coding)

### The shared root cause: there is no canonical will-change value model
`pkg/css/style.go:8469 GetWillChange()` returns a raw `[]string` of comma-split tokens. Every
consumer re-implements its own substring match against that slice:
- `pkg/layout/types.go:183-190` `CreatesStackingContext()` — hardcoded keyword `switch` (missing
  `position`, `z-index`, `translate`/`scale`/`rotate`, `transform-style`, `view-transition-name`,
  `mask-image`; has `mask` but not the shorthand→longhand mapping).
- `pkg/layout/block_layout.go:1762-1767` `isTransformCB` — only `transform`/`perspective`/`filter`.
- `pkg/layout/block_layout.go:2588` `isSelfValidColumnSpanner` — only the literal string
  `will-change: transform` (won't even match `will-change: transform, opacity`).
- `pkg/css/cascade.go:942` — `will-change` is in a property list for some unrelated cascade pass.

This duplication is exactly the anti-pattern Blink avoids: Blink resolves `will-change` **once**,
at style-resolution time, into a `StyleWillChangeData` (see below), and every side-effect site
queries `ComputedStyle::HasWillChangeProperty(CSSPropertyID)`. louis14 must do the same — one
resolved model, queried by all predicates.

### Why Bucket A/B fail — `will-change` is absent from the OOF containing-block decision
`block_layout.go:1748-1870` decides which element resolves an out-of-flow descendant:
- `isPositioned` (`:1748`) = `position != static` → resolves **abspos** here, propagates **fixed** up.
- `isContainmentCB` (`:1751`) = layout/paint containment → resolves **both** abspos and fixed.
- `isTransformCB` (`:1755-1768`) = `transform` / `filter` / `will-change:{transform,perspective,filter}`
  → resolves **both**.

There is **no branch** that says "this element is an abspos CB but not a fixed CB" driven by
will-change, and the `isTransformCB` keyword set is incomplete:
- `will-change: position` (abspos-cb-001, fixpos-cb-position-1, abspos-cb-dynamic-001) matches
  *nothing* → the container is treated as a plain in-flow block, the abspos child propagates past
  it to the ICB. Per spec `will-change: position` makes the element a CB for **absolute** descendants
  only (because a non-static `position` value contains abspos), **not** for fixed.
- `will-change: contain` / `offset-path` / `translate` / `transform-style` / `-webkit-perspective`
  (the fixpos-cb-* tests) each should make the element a CB for **both** abspos and fixed (every one
  of those, in its non-initial form, contains fixed-pos descendants), but none are in the
  `isTransformCB` keyword set.
- `will-change: backdrop-filter` (abspos-cb-003, fixedpos-cb-004/005) — `backdrop-filter` contains
  both; not in the set.
- The **root-element exception** (fixedpos-cb-003/006): when the will-change element is the root
  (`<html>`), it must *not* create a fixed-pos CB — Blink: `LayoutObject::ComputeIsFixedContainer`
  early-returns for the `LayoutView`. louis14's `isRoot` branch (`block_layout.go:1772`) already
  resolves everything against the viewport, so once we *don't* incorrectly add a CB branch for the
  root, these pass. The fix must gate the new will-change CB branches on `!isRoot`.
- **Inline** will-change CB (fixedpos-cb-002/005): `inline_containing_block.go:311
  inlineEstablishesContainingBlock` only checks `position != static` and `GetFilter()`. It must
  also honor `will-change` of a fixed-CB-inducing property, and `BuildPositionedInlineMap`
  (`:256-304`) must set `containsFixed` accordingly so the fixed-pos child is stamped with the
  inline container instead of propagating to the ICB.

### Why Bucket C fails — incomplete keyword set + the inline/flex/grid SC paths
`CreatesStackingContext()` (`types.go:114-197`) is missing `position`, `z-index`, the individual
transform longhands by *name* (it checks resolved `translate`/`rotate`/`scale` *values* at
`:142-150` but not `will-change` naming them), `transform-style`, `view-transition-name`, and
`mask-image` (only `mask`). For `clip-path`/`isolation`/`mix-blend-mode`/`opacity`/`offset-path`
the keyword *is* listed yet the tests still fail — meaning the predicate result is not reaching
the paint-layer z-sort for these shapes. Two sub-causes:
1. **Shorthand vs longhand**: `mask` is a shorthand; the test `will-change: mask` must resolve to
   the `mask-image` (etc.) longhand. Blink stores only **resolved longhand** IDs in
   `StyleWillChangeData::resolved_longhand_ids` ("The bitset only contains resolved longhand
   CSSPropertyID(s). No aliases."). louis14 must expand `will-change` shorthands to longhands and
   normalize aliases (`-webkit-perspective` → `perspective`).
2. **Inline / flex-item / grid-item SC** (stacking-context-opacity-2, -z-index-2, -z-index-3):
   `paint_layer.go:958-1000` routes a *non-positioned* `Box` through `CreatesStackingContext()` and,
   if true, into `parentLayer.FlowChildren` + a new subtree — but the negative-z abspos child of an
   inline/flex/grid `#wc` only paints behind `#wc` if `#wc`'s layer is a real self-painting
   stacking-context root. Confirm `CreatesStackingContext()` is consulted for inline boxes and
   flex/grid items the same way `types.go:124-129/193-195` already special-cases flex items with
   explicit z-index.

### Why Bucket D fails — `reftest-wait` not honored
`will-change-transform-add-content.html` is blank because `pkg/visualtest/reftest_runner_test.go`
screenshots before `class="reftest-wait"` is removed / before the test's deferred-frame JS runs.
This is **not** a will-change bug. Confirm whether the runner already supports `reftest-wait`
(some WPT runners poll for the class to be removed). If unsupported, this test is out of scope for
a will-change fix and should be tracked separately; do not let it block A/B/C. (If
`reftest-wait.js` / `rendering-utils.js` exist under the wpt support tree and the runner has a JS
event loop, the fix is to make the runner wait for `reftest-wait` removal — a one-line gating
change in the runner. Scope decision belongs in Phase 0.)

## Blink reference model (study before coding)

### `StyleWillChangeData` — the resolved will-change value model
`third_party/blink/renderer/core/style/style_will_change_data.h`. Fields:
- `const Vector<AtomicString> values;` — the raw idents (for serialization).
- `const CSSBitset resolved_longhand_ids;` — **resolved longhand** CSSPropertyIDs only, no aliases
  or shorthands. This is the bitset every side-effect predicate queries.
- `const bool has_scroll_position_value;` — `will-change: scroll-position`.
- `const bool has_transform_property;` — any of `transform`/`perspective`/the transform-creating
  set, computed during `ApplyValue`.
- `const bool has_any_transform_property;` — `transform`/`translate`/`scale`/`rotate`/`perspective`/
  `offset-path`/… (the broader transform-related set).

`ComputedStyle` accessors (`core/style/computed_style.h`, ~lines 2550-2595):
- `bool HasWillChangeProperty(CSSPropertyID id) const` → `WillChange() &&
  WillChange()->resolved_longhand_ids.Has(id)`.
- `bool HasWillChangeTransformHint() const` → `WillChange()->has_transform_property`.
- `bool HasWillChangeHintForAnyTransformProperty()` (a.k.a. `HasWillChangeAnyTransformProperty`) →
  `WillChange()->has_any_transform_property`.
- `HasWillChangeOpacityHint` / `HasWillChangeFilterHint` / `HasWillChangeBackdropFilterHint` /
  `HasWillChangeScrollPositionHint` — analogous, used for compositing hints (NOT needed for
  reftests, but the same model serves them).

### Containing-block side effect — `LayoutObject::ComputeIsFixedContainer` / `ComputeIsAbsoluteContainer`
`third_party/blink/renderer/core/layout/layout_object.cc`, ~lines 1520-1580.
`ComputeIsFixedContainer(const ComputedStyle* style)` returns true when:
- `style->HasTransformRelatedProperty()` — and `HasTransformRelatedProperty()` itself
  (`computed_style.h` ~2740) = `HasWillChangeTransformProperty() || has_transform_value ||
  has_perspective_value`, i.e. it folds the will-change transform hint in directly; **or**
- `style->GetPosition() != EPosition::kStatic || style->HasWillChangeProperty(CSSPropertyID::kPosition)`
  — note for **fixed** this contributes only the non-static-position part that contains fixed; the
  `kPosition` will-change hint participates in the *absolute* container check; **or**
- `style->TransformStyle3D() == ETransformStyle3D::kPreserve3d` (and the will-change hint for it); **or**
- `ShouldApplyPaintContainment(style) || ShouldApplyLayoutContainment(style)` — and these consult
  `style->HasWillChangeProperty(CSSPropertyID::kContain)`; **or**
- `style->HasFilterInducingProperty()` / `HasBackdropFilter()` — folding the will-change filter /
  backdrop-filter hints; **or**
- `style->HasNonInitialOffsetPath()` (and its will-change hint).
- **Root exception**: the method early-returns for the `LayoutView` / document element — the root
  never becomes a fixed/abspos *container* in the sense that re-parents descendants away from the
  viewport.

`ComputeIsAbsoluteContainer(bool is_fixed_container, const ComputedStyle* style)` =
`is_fixed_container || style->GetPosition() != EPosition::kStatic ||
style->HasWillChangeProperty(CSSPropertyID::kPosition) || containment || transform checks`.
The key asymmetry: **`will-change: position` ⇒ absolute container only** (it does not flow into
`is_fixed_container`), while every transform/contain/filter/offset-path hint ⇒ **both**.

`CanContainAbsolutePositionObjects()` / `CanContainFixedPositionObjects()`
(`computed_style.cc`) delegate to `ComputeIsAbsoluteContainer` / `ComputeIsFixedContainer`.

### Stacking-context side effect — `StyleAdjuster` + `ComputedStyle::IsStackingContext...`
`third_party/blink/renderer/core/css/resolver/style_adjuster.cc` `AdjustComputedStyle`
(~lines 1240-1430) sets `ForcesStackingContext` for the document element / top layer / view
transitions, and `AllowsZIndex`. The general "would this property create a stacking context"
determination consults the same `HasWillChangeProperty` accessor: per
[CSS Will Change §3](https://drafts.csswg.org/css-will-change-1/#will-change), the will-change
stacking set is exactly the set of properties that themselves create a stacking context:
`opacity`, `transform` (+ `translate`/`scale`/`rotate`/`perspective`), `filter`, `backdrop-filter`,
`clip-path`, `mask`/`mask-image`/`mask-border`, `mix-blend-mode != normal`, `isolation: isolate`,
`contain: {layout,paint,strict,content}`, `offset-path`, `transform-style: preserve-3d`,
`view-transition-name`, **`z-index` on a positioned-or-flex/grid element**, and **`position`**
(because a non-static position + z-index creates one; `will-change: position` alone is treated by
Blink as forcing the stacking context). `PaintLayerStackingNode` then z-sorts children of any
`IsStackingContext()` layer.

## louis14 target files
- `pkg/css/style.go` — the new will-change value model: `WillChange` type + `GetWillChangeData()`,
  shorthand→longhand expansion, alias normalization, and the `HasWillChange*` predicate set.
- `pkg/css/longhand.go` / wherever `mask`/`transition`/etc. shorthands are expanded — reuse the
  existing shorthand→longhand table for `will-change` longhand resolution (do NOT hand-roll a
  second table).
- `pkg/layout/types.go` — `Box.CreatesStackingContext()` (`:114`) routes its will-change branch
  through the new model.
- `pkg/layout/block_layout.go` — the OOF CB decision (`:1748-1870`): add will-change-driven
  abspos-CB and fixed-CB branches, gated on `!isRoot`; fold `isTransformCB` into the new model;
  `isSelfValidColumnSpanner` (`:2588`) uses the model.
- `pkg/layout/inline_containing_block.go` — `inlineEstablishesContainingBlock` (`:311`) and
  `BuildPositionedInlineMap` (`:256`, the `containsFixed` flag) honor will-change.
- `pkg/render/paint_layer.go` — confirm inline / flex-item / grid-item boxes with a will-change SC
  are routed through `CreatesStackingContext()` like the existing flex-item/z-index special-case.
- `pkg/visualtest/reftest_runner_test.go` — Bucket D only: `reftest-wait` gating (scope TBD Phase 0).

## Phases (foundational-first)

### Phase 0: Baseline, bucketing, and Bucket D scope decision — **DO FIRST**
- [ ] Re-confirm the 30-fail list and the A/B/C/D bucketing above against current `HEAD`.
- [ ] Read `pkg/visualtest/reftest_runner_test.go`: does it support `class="reftest-wait"` /
  `takeScreenshot()` / a JS event loop? Decide whether `will-change-transform-add-content`
  (Bucket D) is in scope. If the runner has no deferred-JS support, document it as out-of-scope
  for this category and target **29/30** as the will-change-side-effects gate, with D tracked
  separately. **Do not** let D's harness work block A/B/C.
- **Gate:** bucketing confirmed; D scope decided and written down.

### Phase 1: The canonical `will-change` value model — **FOUNDATIONAL, blocks all others**
**Goal:** one resolved will-change model in `pkg/css`, queried by every side-effect predicate;
delete the per-call-site keyword matching.
- **Blink reference:** `style_will_change_data.h` (`StyleWillChangeData`: `resolved_longhand_ids`
  CSSBitset, `has_transform_property`, `has_any_transform_property`, `has_scroll_position_value`);
  `computed_style.h` ~2550-2595 (`HasWillChangeProperty`, `HasWillChangeTransformHint`,
  `HasWillChangeHintForAnyTransformProperty`).
- **louis14 target:** `pkg/css/style.go` (new), reusing the shorthand→longhand expander.
- **New types / API:**
  - `type WillChange struct` with: `Longhands map[string]bool` (resolved longhand property names —
    louis14 keys CSS by string, mirroring `resolved_longhand_ids`), `HasTransformHint bool`,
    `HasAnyTransformHint bool`, `HasScrollPositionHint bool`.
  - `func (s *Style) GetWillChangeData() *WillChange` — parses `will-change`, normalizes aliases
    (`-webkit-perspective`→`perspective`, `-webkit-transform`→`transform`, etc.), expands shorthands
    (`mask`→`mask-image`/`mask-mode`/…; `transition`, etc.) to longhands via the existing expander,
    drops `auto`, returns `nil` for `auto`/unset.
  - Predicate accessors on `*WillChange` (or `*Style`): `HasWillChangeProperty(name string) bool`,
    `WillChangeCreatesStackingContext() bool`, `WillChangeIsAbsPosCB() bool`,
    `WillChangeIsFixedPosCB() bool`. Each encodes the spec property set:
    - **Stacking-context set:** `opacity, transform, translate, scale, rotate, perspective, filter,
      backdrop-filter, clip-path, mask, mask-image, mask-border, mix-blend-mode, isolation, contain,
      offset-path, transform-style, view-transition-name, z-index, position`.
    - **Fixed-pos CB set:** `transform, translate, scale, rotate, perspective, filter,
      backdrop-filter, contain, offset-path, transform-style`. (`will-change: position` is
      **excluded** here.)
    - **Abspos CB set:** the fixed-pos CB set **plus `position`**.
- **Approach:** keep `GetWillChange() []string` for now (cascade.go still uses it) but make it a
  thin wrapper or leave untouched; introduce `GetWillChangeData()` as the single source of truth
  for side effects. Memoize on the `Style` if `Style` caches computed sub-values elsewhere.
- **Tests fixed:** none directly — this is the substrate. **Gate:** `go build ./pkg/css/...`
  clean; `go fmt` clean; a focused unit test (if the package has `_test.go` coverage for style
  getters) covering `mask`→`mask-image`, `-webkit-perspective`→`perspective`, `transform,opacity`
  multi-token, `auto`.

### Phase 2: Stacking-context side effect (Bucket C, 15 tests)
**Goal:** `will-change` of any stacking-context-inducing property makes the box a stacking context,
for block, inline, flex-item, and grid-item boxes.
- **Blink reference:** `style_adjuster.cc` `AdjustComputedStyle` (~1240-1430) + the spec
  stacking set in [CSS Will Change §3]; `ComputedStyle::HasWillChangeProperty`.
- **louis14 target:** `pkg/layout/types.go` `Box.CreatesStackingContext()` (`:114-197`);
  `pkg/render/paint_layer.go` (`:958-1000`).
- **Approach:**
  1. Replace the hardcoded `switch` at `types.go:183-190` with
     `if wc := b.Style.GetWillChangeData(); wc != nil && wc.WillChangeCreatesStackingContext() { return true }`.
     This adds `position`, `z-index`, `translate`/`scale`/`rotate`, `transform-style`,
     `view-transition-name`, `mask-image` and fixes the `mask` shorthand mismatch.
  2. In `paint_layer.go`, confirm a *non-positioned* inline / flex-item / grid-item `#wc` whose
     only SC trigger is will-change is routed through the `!isPositioned &&
     child.CreatesStackingContext()` branch (`:958-971`) and becomes a new subtree root, so its
     negative-z abspos child is z-sorted inside it. If inline boxes bypass `CreatesStackingContext`,
     extend the routing to call it for inlines (mirror the flex-item/z-index special-case at
     `:943-956`). For flex/grid items, `types.go:124-129` already special-cases explicit z-index;
     ensure `will-change: z-index` on a flex/grid item likewise returns true (it will, via the
     stacking set, but verify the flex/grid item path in `paint_layer.go` reaches it).
- **Tests fixed:** all 15 Bucket C tests.
- **Gate:** render `will-change-stacking-context-z-index-1`, `-mask-1`, `-opacity-2` (one
  block + one shorthand + one inline) at 0% diff; spot-check no css-flexbox / css-masking
  regression on 2-3 adjacent tests.

### Phase 3: Containing-block side effect for block-level will-change (Buckets A + B, block cases)
**Goal:** block-level `will-change` of a CB-inducing property generates the correct containing
block — abspos-only for `position`, abspos+fixed for the transform/contain/filter/offset-path set —
with the root-element exception.
- **Blink reference:** `layout_object.cc` `ComputeIsFixedContainer` / `ComputeIsAbsoluteContainer`
  (~1520-1580) and the `is_fixed_container` asymmetry; root early-return for `LayoutView`.
- **louis14 target:** `pkg/layout/block_layout.go` OOF CB decision (`:1748-1870`);
  `isSelfValidColumnSpanner` (`:2588`).
- **Approach:**
  1. Compute, alongside `isPositioned`/`isContainmentCB`/`isTransformCB`, two new flags from the
     model: `wcAbsCB := wc != nil && wc.WillChangeIsAbsPosCB()` and
     `wcFixedCB := wc != nil && wc.WillChangeIsFixedPosCB()`, **gated on `!isRoot`** (the root
     exception — fixedpos-cb-003/006).
  2. Fold the existing `isTransformCB` keyword loop (`:1762-1767`) into `wcFixedCB` (delete the
     ad-hoc loop; `transform`/`perspective`/`filter` are already in the model's fixed set).
  3. Branch structure (replacing `:1793-1870`):
     - `isContainmentCB || isTransformCB || wcFixedCB` → CB for **both** abspos and fixed
       (existing padding-box-sized `OutOfFlowLayoutPart` with `resolvesFixed: true`).
     - else if `isPositioned || wcAbsCB` → CB for **abspos**, propagate **fixed** upward
       (existing `:1812-1866` path; `wcAbsCB` makes a `will-change: position` element take the
       `isPositioned` path without actually being positioned).
     - else → propagate all upward (unchanged).
  4. `isSelfValidColumnSpanner` (`:2588`): replace the literal `v == "transform"` string match
     with `wc != nil && wc.WillChangeIsFixedPosCB()` (it currently won't even match
     `will-change: transform, opacity`).
- **Tests fixed:** A: `will-change-abspos-cb-001`, `-003`; B: `will-change-fixedpos-cb-003`,
  `-004`, `-006`, `will-change-fixpos-cb-contain-1`, `-offset-path-1`, `-position-1`,
  `-transform-style-1`, `-translate-1`, `-webkit-perspective-1`. (`abspos-cb-dynamic-001` deferred
  to Phase 5; inline cases `fixedpos-cb-002`/`-005` to Phase 4.)
- **Gate:** render `will-change-abspos-cb-001`, `will-change-fixpos-cb-position-1` (asymmetry
  check), `will-change-fixedpos-cb-006` (root exception) at 0% diff; spot-check 2-3 css-position /
  css-contain adjacent tests for no regression.

### Phase 4: Containing-block side effect for inline-level will-change (Bucket B, inline cases)
**Goal:** `will-change` on an *inline* element generates a containing block for fixed-pos
descendants the same way `filter` on an inline already does.
- **Blink reference:** same `ComputeIsFixedContainer` — Blink does not distinguish inline vs block
  for the CB decision; it is a `LayoutObject`-level property. louis14 models inline CBs separately
  via the positioned-inline stack.
- **louis14 target:** `pkg/layout/inline_containing_block.go` — `inlineEstablishesContainingBlock`
  (`:311-319`) and `BuildPositionedInlineMap` (`:256-304`).
- **Approach:**
  1. `inlineEstablishesContainingBlock`: add
     `if wc := style.GetWillChangeData(); wc != nil && wc.WillChangeIsAbsPosCB() { return true }`.
  2. `BuildPositionedInlineMap`: when pushing an `inlineCB` stack entry (`:274-279`), set
     `containsFixed: len(item.Style.GetFilter()) > 0 || (wc != nil && wc.WillChangeIsFixedPosCB())`
     so a `will-change: filter` / `backdrop-filter` inline stamps its fixed-pos child with the
     inline container (routing it to inline-CB sizing instead of propagating to the ICB).
- **Tests fixed:** `will-change-fixedpos-cb-002`, `will-change-fixedpos-cb-005`.
- **Gate:** render `will-change-fixedpos-cb-002`, `-005` at 0% diff; spot-check an existing
  filter-on-inline css-position test for no regression.

### Phase 5: Dynamic will-change re-resolution (Bucket A, dynamic case)
**Goal:** changing `will-change` via script (`element.style.willChange = "position"` after load)
re-runs style → layout so the CB is re-resolved.
- **Blink reference:** `LayoutObject::StyleDidChange` invalidates layout when CB-affecting
  properties change; `will-change` is in that set.
- **louis14 target:** the reftest runner's script-execution + relayout path (find via
  `reftest_runner_test.go` and the existing dynamic-restyle tests, e.g. how
  `css-position`/`css-contain` dynamic tests already relayout). If louis14 already relayouts after
  `style.*` mutations (other `*-dynamic-*` reftests pass), this phase is **a verification only** —
  Phases 1+3 make the static path correct, and the dynamic path inherits the fix automatically.
- **Approach:** confirm `will-change-abspos-cb-dynamic-001` passes once Phase 3 lands. If it does
  not, trace whether `will-change` mutations trigger a relayout (vs. only a repaint); if they only
  repaint, add `will-change` to the layout-invalidating property set.
- **Tests fixed:** `will-change-abspos-cb-dynamic-001`.
- **Gate:** render `will-change-abspos-cb-dynamic-001` at 0% diff.

### Phase 6 (conditional): Bucket D — `reftest-wait` harness support
**Only if Phase 0 scoped D into this category.**
- **Goal:** `will-change-transform-add-content` renders the post-`textContent` frame.
- **louis14 target:** `pkg/visualtest/reftest_runner_test.go`.
- **Approach:** make the runner detect `class="reftest-wait"` on `<html>` and defer the screenshot
  until the class is removed (or until `takeScreenshot()` is invoked), running the page's deferred
  JS / `waitForAtLeastOneFrame` first. Mirror however the runner already handles `onload` scripts.
- **Tests fixed:** `will-change-transform-add-content`.
- **Gate:** render `will-change-transform-add-content` at 0% diff; no regression in other
  `reftest-wait` tests across categories.

### Phase 7: Full-category verification & delivery
- [ ] Sanctioned single full-category run: `TestWPTCSS3Reftests/css-will-change` → expect 47/47
      (or 46/47 if D was scoped out in Phase 0).
- [ ] Regression spot-check (sanctioned, targeted): a handful of css-position, css-flexbox,
      css-transforms, css-masking, css-contain tests that exercise CB / stacking-context code.
- [ ] Commit per-phase (checkpoint commits), final summary commit.
- **Gate:** 47/47 (or 46/47) css-will-change at 0% diff; zero regressions in spot-checked
  adjacent categories.

## Phase count
**8 phases** (Phase 0 baseline → Phase 7 delivery), Phase 6 conditional on the Phase 0 scope
decision for Bucket D.

## Risks & notes
- **Shorthand expansion correctness.** `will-change: mask` must expand to the same longhand set the
  cascade uses; reuse the existing expander — a second hand-rolled table will drift.
- **The `position` asymmetry is the subtle part.** `will-change: position` ⇒ abspos CB **and**
  stacking context, but **not** fixed CB. `fixpos-cb-position-1` is the canary: it has both an
  abspos and a fixed child and only the abspos one must be re-contained.
- **Root exception.** Phases 3's new CB branches must be gated on `!isRoot` or fixedpos-cb-003/006
  will regress (they currently pass *because* nothing fires; an ungated will-change CB branch would
  break them).
- **`isTransformCB` is being deleted, not extended.** Folding it into the model avoids a fourth
  divergent keyword list; verify the three currently-passing `will-change-fixpos-cb-{transform,
  filter,perspective}-1` tests still pass after the fold.
- **Bucket C "listed but failing" tests** (`clip-path`, `isolation`, `mix-blend-mode`, `opacity`,
  `offset-path`) prove the bug is partly in paint-layer routing, not only the keyword set — do not
  assume Phase 1+2's keyword completion alone fixes them; the `paint_layer.go` inline/flex/grid
  routing check in Phase 2 step 2 is mandatory.
