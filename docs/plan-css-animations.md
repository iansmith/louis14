# Task Plan: Pass the css-animations reftest category

## Goal
Maximize passing tests in `pkg/visualtest/testdata/wpt-css3/css-animations/` under
`TestWPTCSS3Reftests/css-animations`. Baseline: **6 passing / 18 failing / 5 skipped**
(24 run; 5 skipped require `flags=dom`). Target after this plan: **15 passing / 9
out-of-scope** — i.e. fix all 9 statically-reproducible failures, formally park the 9
that genuinely need an animation timeline / Web Animations JS API.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink
   first, 0% diff required, test-execution discipline (only failing tests +
   regression-adjacent), operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** —
   auto-memory index pointing at the same feedback rules.

If you are about to type a rule verbatim into this file or into code, stop and link.

---

## Critical framing — why this category is mostly *static*

louis14's reftest harness (`pkg/visualtest/reftest_runner_test.go`) is a static
renderer. It skips only `flags=dom` tests (line 237); it does **not** skip
`class="reftest-wait"` or `<script>`-bearing tests — those are rendered at their
*static initial state*. So the triage question per test is:

> *Does the reference describe the element's static initial computed style, or does it
> describe a mid-timeline / script-driven state?*

The decisive root-cause finding from the source survey:

> **`@keyframes` rules are parsed and stored but never applied.**
> `pkg/css/stylesheet.go:132` defines `Stylesheet.Keyframes`,
> `:412-419` populates it, `:2071 parseKeyframesRule` parses stops — but
> `grep -rn "Keyframes" pkg/` shows **zero consumers**. `pkg/css/style.go:2103`
> stores the `animation` shorthand as an opaque string and never decomposes it.
> `pkg/css/cascade.go` `ComputeStyle` never references keyframes.

This single gap explains the majority of the in-scope failures: CSS Animations spec
§4 says that when an animation is in effect (which includes the static
`animation-fill-mode: both/backwards/forwards` boundary case, and any animation
"frozen" at a fixed progress via huge duration + negative delay), the keyframe
values **must be applied to computed style** ahead of the cascade's normal author
declarations. louis14 currently applies *none* of them, so every such element renders
with its un-animated base style.

---

## Triage — all 18 failures

### IN SCOPE — statically reproducible bugs (9)

| Test | Static-repro reason | Bucket |
|------|--------------------|--------|
| `animate-font-size-with-margin-override.html` | `animation: anim both`, `@keyframes anim { from,to { font-size:30px } }`. `both` fill-mode → keyframe value applies at the static boundary. Ref hard-codes `font-size:30px`. louis14 renders default h2 size. | B1 keyframe application |
| `animation-name-in-shadow-part.html` | `@keyframes` + `animation` declared on `#shadow::part(target)` in the **outer** scope; single rule. Ref = static green box. No JS. | B2 keyframe tree-scope + B5 shadow |
| `animation-name-in-shadow-part-inner-match.html` | Two `@keyframes animation` (outer + shadow); the matching one is the *shadow* rule because `animation` is set inside the shadow `<style>`. Ref = static green. No JS. | B2 + B5 |
| `animation-name-in-shadow-part-outer-match.html` | Two `@keyframes animation`; `animation` set on `::part` in outer scope → outer keyframes win. Ref = static green. No JS. | B2 + B5 |
| `animation-name-in-nested-shadow.html` | 3 nested scopes, 9 elements; each `animation-name` resolves against the `@keyframes` in **the scope where the `animation-name` declaration lives**. `fill-mode: both` → all static. Ref = static color grid. No JS. | B2 + B5 |
| `inheritance-pseudo-element.html` | `@keyframes` on `::after` pseudo-elements; `from,to` set `font-size` to `5px`/`1em`/`100%`/`inherit`. `fill-mode` defaults via `animation` shorthand? No — uses `animation: kf 1s infinite` (no fill-mode) **but** `from`/`to` are identical so the value is constant across the whole active range, and the test expects the *running* value. All four resolve to the same `1em` box. Ref = static. | B1 + B3 keyframe relative-value resolution |
| `animation-important-002.html` | `@keyframes` animates `color`/`background-color`; frozen at 50% via `duration:1000s; delay:-500s; steps(2,end)`. Ref hard-codes the frozen colors. `a:visited { color:white !important }` must still override the animation. Tests animation-vs-`!important` cascade order. | B1 + B4 cascade order |
| `animation-delay-011.html` | `@keyframes bg { 50%{background:green} }` on `div:after`, frozen via `bg 100s step-end infinite` + inherited `animation-delay:-50s` → 50% progress → green. Ref (`animation-common-ref.html`) = static green box. Tests `animation-delay: inherit` + mismatched-length list + `step-end` + missing `from`/`to` keyframe synthesis. | B1 + B3 + B6 timing-at-static-progress |
| `svg-transform-animation.html` | `@keyframes transform { from,to { transform: translate(100px,100px) } }` on an SVG `<rect>`; `from`==`to` so constant. `animation: transform 2s infinite` (no fill-mode) but the value is constant across the active range. Ref = static div at (100,100). louis14 renders nothing for the green rect. | B1 + B7 SVG transform |

### IN SCOPE but lower-confidence — "frozen animation" via huge-duration math (verify in Phase 0)

These are written so the animation is *deliberately frozen* at a fixed progress, so
the rendered result is deterministic and static — **but** confirming louis14 produces
the exact frozen value requires implementing keyframe interpolation at a computed
progress, not just at a `from`/`to` boundary. Treat as in-scope, gated behind B3/B6:

| Test | Frozen mechanism | Bucket |
|------|-----------------|--------|
| `animation-offscreen-to-onscreen.html` | `from`==`to` `translate(100px,0)`; constant. Element also has base `transform: translate(-2000px,0)` which the animation must *override*. Ref = box at translate(100px). louis14 renders blank (box stays at -2000px). | B1 + B8 |
| `jump-start-animation-before-phase.html` | `steps(1,jump-start) backwards`, in the *before* phase (delay 5000s) → `backwards` fill → first keyframe value `translateX(100px)`. Ref hard-codes `translateX(100px)`. louis14 renders at 0. | B1 + B3 + B6 |
| `nested-scale-animations.html` | `@keyframes scale {0%{scale(1)} 1%{scale(10)} 100%{scale(10)}}` `forwards`; frozen at 100% → `scale(10)`. Two nested → `scale(100)`. Needs `forwards` fill + non-boundary keyframe selection. Ref masks AA band. | B1 + B3 + B6 |
| `translation-animation-subpixel-offset.html` | `from`==`to` `translateY(10px)`; constant. Tests subpixel-offset stacking with an animated transform. Ref = green at (1,11). | B1 + B8 |
| `flip-running-animation-via-variable.html` | `@keyframes spin` uses `var(--scale)`; frozen at 50% via huge duration + neg delay + `cubic-bezier(0,1,1,0)`. **Final** state is post-JS class toggle (`--scale:-1`). The *toggle* is JS. **OUT OF SCOPE** unless the static `--scale:1` initial state also matches — it does not (ref needs `scaleX(-1)`). Re-confirm in Phase 0; expected park. | (likely OOS) |

> **Phase 0 will re-bucket** `flip-running-animation-via-variable` — its ref depends on
> a `classList.add` that runs in JS, so it is almost certainly out of scope; listed
> here only because it shares the keyframe+var machinery. The other four are genuinely
> static (the `<script>` only calls `takeScreenshot()` after rAF; no DOM mutation).

### OUT OF SCOPE — harness limitation (genuinely need an animation timeline / WAAPI JS) (5 + 1)

| Test | Why untestable in a static renderer |
|------|-------------------------------------|
| `animation-opacity-pause-and-set-time.html` | Uses `element.animate([...], 1000)` (Web Animations API), then `.pause()` and `.currentTime = 500`. Ref = `opacity:0.4` (the t=500ms interpolated value). Requires a live `Animation` object + timeline. No `@keyframes`, no CSS animation — purely WAAPI. |
| `animation-transform-pause-and-set-time.html` | Same: `element.animate(...)`, `.pause()`, `.currentTime=500`. Ref = `translate(500px)`. WAAPI-only. |
| `flip-running-animation-via-variable.html` | Ref state only reached after `classList.add('tweaked')` runs in `window.onload`. Static initial state (`--scale:1`) does not match the ref. |
| (the 5 `flags=dom` skips — already correctly skipped, not counted in the 18) | `cancel-animation-shadow-slot-invalidation`, `display-none-*` etc. |

> Note: `animation-opacity/transform-pause-and-set-time` *could* in principle be made
> testable if louis14's JS engine grew a real Web Animations API with a controllable
> timeline — that is a separate, much larger capability and explicitly **not** part of
> this plan. They are parked. If a future WAAPI effort lands, revisit.

### Net
- **9 firmly in-scope** (the first table) + **4 in-scope pending B3/B6** = **13 fixable**.
- **3 out-of-scope** in the failing-18 (`flip-running-animation-via-variable` + the two
  `pause-and-set-time`).
- Realistic target: **13 fixed → 19 passing / 5 skipped / 0 in-scope failing**, with the
  3 WAAPI/JS-toggle tests formally parked.

---

## Feature buckets (in-scope work)

- **B1 — Keyframe application to computed style.** The foundational fix: decompose the
  `animation` shorthand into longhands, resolve `animation-name` → `@keyframes`, and
  apply the in-effect keyframe declarations to computed style before/around the cascade.
- **B2 — Tree-scoped `@keyframes` lookup.** `animation-name` is a *tree-scoped reference*:
  the `@keyframes` is resolved in the tree scope of the **rule that set
  `animation-name`**, not the element's scope. Needed for the shadow tests.
- **B3 — Keyframe value resolution at a static progress.** Selecting which keyframe(s)
  apply for `from`/`to`-only, `0%/1%/100%`, missing-`from`/`to` synthesis, and computing
  the frozen progress from duration/delay/timing-function/direction/fill-mode.
- **B4 — Animation vs. cascade origin / `!important`.** Animated values sit in their own
  cascade origin; author `!important` still wins (CSS Cascade §6.3).
- **B5 — Shadow DOM + `::part`.** Prerequisite for B2's shadow tests: louis14 currently
  has **no shadow DOM** (`grep -rn "ShadowRoot" pkg/` → nothing). Needs
  `<template shadowrootmode>` parsing, a shadow tree model, and `::part()` matching.
- **B6 — Animation timing model (static snapshot).** A minimal `Timing` struct + phase
  calc so B3 can compute the frozen progress; mirrors Blink `Timing` /
  `AnimationEffect`.
- **B7 — SVG transform.** `svg-transform-animation` also needs the animated `transform`
  to actually paint on an SVG child element.
- **B8 — Animated transform overrides base transform.** When a keyframe sets `transform`,
  it must replace (not stack with) the element's base `transform` declaration.

---

## Blink grounding (study before coding)

Blink source (GitHub mirror
`https://github.com/chromium/chromium/blob/main/third_party/blink/renderer/...`):

### Keyframe resolution & application
- **`core/animation/css/css_animations.cc`** —
  - `CSSAnimations::CalculateAnimationUpdate` — per-element, reads `animation-*`
    longhands from `ComputedStyleBuilder`, for each animation name calls
    `resolver->FindKeyframesRule(...)` then builds a `CssKeyframeEffectModel`.
  - `ProcessKeyframesRule` (~L750-830) — turns a `StyleRuleKeyframes` into
    `StringKeyframe`s: parses each stop's offset (`from`=0, `to`=1, `N%`=N/100),
    per-keyframe `animation-timing-function`, and the property/value pairs.
  - `CreateKeyframeEffectModel` (~L837-1030) — processes keyframes in reverse (spec
    step 6), merges equal-offset keyframes, tracks which properties appear at offsets
    0 / 1 / intermediate, and **synthesizes missing `from`/`to` keyframes** for any
    property not present at 0 or 1 (this is what `animation-delay-011` exercises).
  - `IdleTriggerAllowsVisualEffect` (~L740-760) — the fill-mode gate: `BOTH` →
    visible in before/after/idle; `BACKWARDS` → before phase only; `FORWARDS` →
    after phase only; `NONE` → not visible when idle.
  - `CSSAnimations::CalculateAnimationActiveInterpolations` — produces the
    `ActiveInterpolationsMap` that the cascade consumes.
- **`core/css/resolver/scoped_style_resolver.cc`** — `KeyframesRuleMap` (animation
  name → `StyleRuleKeyframes`), `AddKeyframeStyle` (~L161-168) with
  `KeyframeStyleShouldOverride` (~L170-179: non-prefixed beats prefixed, then cascade
  layer order), `KeyframeStylesForAnimation` (~L151-160) the local lookup, and
  `ScopedStyleResolver::Parent()` (~L85-93) which walks **outward** to the parent tree
  scope's resolver.
- **`core/css/resolver/style_resolver.cc`** — `StyleResolver::FindKeyframesRule`:
  takes `element`, `animating_element`, and the `name_tree_scope` (the tree scope of
  the rule that declared `animation-name`). It searches **the `name_tree_scope`'s
  `ScopedStyleResolver` first, then walks `Parent()` outward** — i.e. the keyframes
  are resolved relative to where the *name* was written, not where the element lives.
  This is precisely what the four shadow-part / nested-shadow tests verify. Returns
  `FindKeyframesRuleResult { rule, tree_scope }`.

### Cascade ordering (B4)
- **`core/css/resolver/cascade_priority.h` / `style_cascade.cc`** — animations are
  origin `kAnimation`, slotted between author-normal and author-`!important` (CSS
  Cascade §6.3 / §6.4.4: "Animations" origin is below "Author Important"). So
  `a:visited { color:white !important }` overrides the animated `color`.

### Timing model (B6)
- **`core/animation/timing.h`** — `Timing` struct: `start_delay`, `end_delay`,
  `iteration_duration`, `iteration_count`, `direction`, `fill_mode`,
  `timing_function`.
- **`core/animation/animation_effect.cc`** — `AnimationEffect::CalculateTimings` /
  `CalculatePhase` → `{kPhaseBefore,kPhaseActive,kPhaseAfter,kPhaseNone}`;
  `CalculateActiveTime` applies fill-mode; `CalculateOverallProgress` /
  `CalculateSimpleIterationProgress` → progress in [0,1]; then the
  `timing_function` (steps()/cubic-bezier) maps it to "transformed progress".
- **`core/animation/timing_calculations.cc`** — the actual phase/progress arithmetic.

### Easing (B3/B6)
- **`platform/animation/timing_function.cc`** — `StepsTimingFunction`
  (`jump-start`/`jump-end`/`jump-both`/`jump-none`/`start`/`end`) and
  `CubicBezierTimingFunction`. For the in-scope tests we only need `steps()` and the
  identity-ish frozen cases; full cubic-bezier evaluation is needed for
  `flip-running` which is OOS anyway.

### Shadow DOM (B5)
- **`core/dom/shadow_root.h`**, **`core/dom/element.cc`** `attachShadow` /
  declarative `shadowrootmode`, **`core/html/html_template_element.cc`** for
  `<template shadowrootmode>` parsing.
- **`core/css/check_pseudo_has_argument_context.cc`** is *not* it — `::part` matching
  lives in **`core/css/selector_checker.cc`** `CheckPseudoElement` /
  `MatchesPartPseudoElement`, keyed off the element's `part` attribute and the
  `PartNames` set; a `::part()` rule from the host's scope matches a shadow element
  whose `part` attribute lists that name.

---

## louis14 target map

| Bucket | Target files | New types / functions |
|--------|-------------|----------------------|
| B1 | `pkg/css/animation.go` (**new**, mirrors `core/animation/css/`), `pkg/css/cascade.go` `ComputeStyle` + `ComputePseudoElementStyle`, `pkg/css/style.go:2103` `expandShorthand` `case "animation"` | `KeyframeEffect`, `ResolvedAnimation`; `ApplyAnimations(node, style, stylesheets, treeScope)`; decompose `animation` shorthand into the 8 longhands |
| B2 | `pkg/css/animation.go`, `pkg/css/stylesheet.go` (`Keyframes` already exists but is per-stylesheet) | `FindKeyframesRule(name, declTreeScope) (*[]KeyframeRule, found)`; track, per `Rule`, the tree scope it came from |
| B3 | `pkg/css/animation.go` | `ResolveKeyframeStyle(effect, progress) map[string]string` — merge `from`/`to`, synthesize missing endpoints, pick keyframe by progress |
| B4 | `pkg/css/cascade.go` `ComputeStyle` / `ComputePseudoElementStyle` | apply animated values in a pass that sits *after* author-normal but *before* author-`!important` (reuse `importantProps` map) |
| B5 | `pkg/html/` (parser + node model), `pkg/css/matcher.go` (`MatchesSelector`), `pkg/css/cascade.go` (`ParseDocumentStylesheets`, scope-aware rule collection) | `html.ShadowRoot`, `Node.ShadowRoot`, declarative `<template shadowrootmode>` handling; `::part()` selector parsing in `pkg/css/stylesheet.go` selector parser + matching in `matcher.go` |
| B6 | `pkg/css/animation.go` (or `pkg/css/timing.go` mirroring `core/animation/timing.*`) | `Timing` struct; `ComputePhase`, `ComputeProgress`, `applyFillMode`; `StepsTimingFunction` eval |
| B7 | `pkg/layout/` SVG path (`inline_item.go`, `intrinsic_sizing.go` already touch SVG), `pkg/render/render.go` / `paint_layer.go` | ensure SVG child elements honor computed `transform` |
| B8 | `pkg/render/paint_layer.go` / `render.go` transform application | animated `transform` from B1 replaces base `transform` in the computed style before paint reads it |

---

## Phases — foundational-first

### Phase 0: Baseline & re-triage — gate before any code
- [ ] Run **only** the 18 failing css-animations tests + `keyframes-parse-001`,
      `revert-rule-keyframes`, `keyframes-unrelated-custom-property` (currently-passing
      keyframe tests — regression anchors). One sanctioned narrow run.
- [ ] Confirm the "frozen animation" four (`animation-offscreen-to-onscreen`,
      `jump-start-animation-before-phase`, `nested-scale-animations`,
      `translation-animation-subpixel-offset`) have **no DOM-mutating script** — verified
      by reading: their `<script>` only awaits `ready` / rAF then `takeScreenshot()`.
      Keep them in-scope.
- [ ] Confirm `flip-running-animation-via-variable` ref needs `classList.add` → park OOS.
- [ ] Read `pkg/css/cascade.go`, `pkg/css/stylesheet.go:2071`, `pkg/css/style.go:2103`,
      `pkg/css/matcher.go` end-to-end. Confirm: nothing consumes `Stylesheet.Keyframes`.
- **Gate:** triage table above confirmed; no code yet.

### Phase 1: B6 — minimal static timing model
Mirror Blink `core/animation/timing.*` + `animation_effect.cc` phase/progress math,
**static snapshot only** (no real clock — the "current time" is derived from
`animation-delay` since the document timeline is 0 in a static render).
- [ ] `Timing` struct: delay, duration, iteration-count, direction, fill-mode,
      timing-function. Parse from the 8 `animation-*` longhands.
- [ ] `ComputePhase(timing, localTime=0)` → before/active/after/idle. With document
      time 0, `localTime = -animation-delay`; negative delay pushes into the active
      (or after) phase, which is exactly how `animation-important-002`,
      `animation-delay-011`, `flip-running` "freeze" the animation.
- [ ] `ComputeProgress` → simple iteration progress in [0,1]; apply
      `StepsTimingFunction` (`jump-start`/`step-end`/`steps(2,end)` needed by the
      in-scope tests).
- [ ] `applyFillMode` gate (Blink `IdleTriggerAllowsVisualEffect`): `both` → always
      visible; `backwards` → before phase; `forwards` → after phase.
- **Blink ref:** `core/animation/timing.h`, `core/animation/animation_effect.cc`
  (`CalculatePhase`, `CalculateActiveTime`, `CalculateSimpleIterationProgress`),
  `core/animation/timing_calculations.cc`, `platform/animation/timing_function.cc`
  (`StepsTimingFunction::Evaluate`).
- **louis14:** `pkg/css/timing.go` (new), parsing helpers near `pkg/css/style.go:2109`.
- **Tests fixed:** none yet (infrastructure).
- **Gate:** unit tests for `ComputePhase`/`ComputeProgress` on the exact
  delay/duration/timing combos of `animation-important-002` (50%, `steps(2,end)`),
  `animation-delay-011` (50%, `step-end`), `jump-start-animation-before-phase`
  (before phase, `backwards`). No regression on the 3 anchor tests.

### Phase 2: B1 + B3 — keyframe application (document scope only)
The foundational fix. Document-scope `@keyframes` only; shadow deferred to Phase 4.
- [ ] In `expandShorthand` `case "animation"` (`pkg/css/style.go:2103`): decompose the
      shorthand into the 8 longhands (still also keep the raw string if needed
      elsewhere). Handle reset-to-initial of omitted longhands per CSS Animations §4.
- [ ] New `pkg/css/animation.go`:
  - `ResolvedAnimation` — name, `Timing`, resolved `[]KeyframeRule`.
  - `BuildKeyframeEffect(frames []KeyframeRule)` — normalize stop offsets
    (`from`→0, `to`→1, `N%`→N/100), merge equal-offset stops, **synthesize missing
    `from`/`to`** for any property not present at 0/1 (Blink `CreateKeyframeEffectModel`
    step ~L900+). `animation-delay-011`'s `@keyframes bg { 50%{...} }` has no
    `from`/`to` → the synthesized endpoints carry the *base* (un-animated) value.
  - `ResolveKeyframeStyle(effect, progress)` → `map[string]string`: select the
    keyframe whose offset matches the transformed progress; for `from`==`to` (constant)
    cases this is trivially the single value; for the frozen non-boundary cases
    (`nested-scale 100%`, `delay-011 50%`) pick the bracketing keyframe per the
    timing-function's step output (no numeric interpolation of colors/transforms is
    required by *any* in-scope test — every in-scope `@keyframes` either has identical
    `from`/`to` or is frozen exactly *on* a keyframe offset; interpolation is only
    needed by the OOS WAAPI tests). **Document this explicitly** — interpolation is a
    deliberate non-goal here.
- [ ] `ApplyAnimations(node, style, animTreeScopeStylesheets)` called from
      `ComputeStyle` (`pkg/css/cascade.go:390`) and `ComputePseudoElementStyle`
      (`:492`): after author-normal declarations, for each `animation-name` look up
      `@keyframes`, build the effect, compute phase/progress (Phase 1), and if the
      fill-mode gate passes, `style.Set(prop, value)` for every keyframe-declared
      property — but **skip** properties already in `importantProps` (B4).
- [ ] B8: when the keyframe sets `transform`, it overwrites the base `transform`
      longhand outright (it does in the loop above naturally — just make sure base
      `transform` is a longhand, not buried in a shorthand).
- **Blink ref:** `core/animation/css/css_animations.cc`
  `CalculateAnimationUpdate` / `ProcessKeyframesRule` / `CreateKeyframeEffectModel`.
- **louis14:** `pkg/css/animation.go` (new), `pkg/css/style.go:2103`,
  `pkg/css/cascade.go:390` + `:492`.
- **Tests fixed:** `animate-font-size-with-margin-override`, `inheritance-pseudo-element`,
  `svg-transform-animation` (pending B7), `animation-offscreen-to-onscreen`,
  `jump-start-animation-before-phase`, `nested-scale-animations`,
  `translation-animation-subpixel-offset`, `animation-delay-011`.
- **Gate:** those 8 at 0% diff (svg one may need Phase 3). 3 anchor tests still pass.
  CSS2 regression spot-check unaffected (run css-animations bucket only).

### Phase 3: B7 — SVG animated transform paint
**Status (2026-05-16):** **Unblocked by LOU-128.** `svg-transform-animation.html` is
included in the LOU-128 gate sample and passes at 0% diff. The SVG render tree
(`pkg/layout/svg/`) lays out `<rect>` and applies `<g transform>` / CSS-transform on
SVG shapes; the paint walk in `pkg/render/svg_*_painter.go` applies the transform.

The keyframe engine (Phase 2 of this plan) populates `transform` on the SVG `<rect>`'s
computed style via `pkg/css/animation.go`; LOU-128's transform helper
(`pkg/layout/svg/transform_helper.go::ParseCSSTransformForSVG`) consumes it. No
remaining work specific to this phase.

- **Blink ref:** SVG transform is folded into the same `ComputedStyle.transform`;
  `core/layout/svg/` consumes it.
- **Tests fixed:** `svg-transform-animation` (PASS @ 0).
- **Gate:** keep `svg-transform-animation` in the regression sample.

### Phase 4: B5 — Shadow DOM + `::part`, then B2 — tree-scoped keyframes
Largest new capability. Required by 4 shadow tests; do it last so the keyframe
machinery is already proven on document scope.
- [ ] **B5a — declarative shadow DOM.** Parse `<template shadowrootmode="open">` into
      an `html.ShadowRoot` attached to its host (`pkg/html/`). Model the shadow tree:
      host element has a `ShadowRoot` whose children are the template content; the
      shadow tree has its own `<style>` scope.
- [ ] **B5b — scoped style resolution.** `ParseDocumentStylesheets` /
      `applyStylesToNode` (`pkg/css/cascade.go`) must associate each `Stylesheet` (and
      thus each `Rule` and each `@keyframes`) with the tree scope it was parsed in.
      Add a `TreeScope` handle to `Rule` and to `Stylesheet.Keyframes` entries. Style
      resolution for a shadow element uses: the element's own scope's stylesheets +
      (for `::part`) the host scope's `::part()` rules.
- [ ] **B5c — `::part()` matching.** Parse `::part(name)` in the selector parser
      (`pkg/css/stylesheet.go`) and match in `pkg/css/matcher.go`: a `::part(target)`
      rule authored in scope S matches an element E iff E is in a shadow tree whose
      host is in S (or a descendant exposing the part) **and** E's `part` attribute
      lists `target`. Mirror Blink `selector_checker.cc` `MatchesPartPseudoElement`.
- [ ] **B2 — tree-scoped `@keyframes` lookup.** Implement `FindKeyframesRule(name,
      declTreeScope)`: search `declTreeScope`'s keyframes first, then walk outward to
      parent scopes (Blink `ScopedStyleResolver::Parent()` + `FindKeyframesRule`).
      Critically: the `declTreeScope` is the scope of the **rule that set
      `animation-name`**, not the element's scope. So:
  - `animation-name-in-shadow-part` / `-outer-match`: `animation` set by a
    `::part` rule in the *outer* document scope → outer `@keyframes` win → green.
  - `animation-name-in-shadow-part-inner-match`: `animation` set by a `div{}` rule
    *inside the shadow* → shadow `@keyframes` win → green.
  - `animation-name-in-nested-shadow`: each `#x_anim_y { animation-name: y }` rule
    lives in scope X, so `@keyframes y` is resolved starting at scope X then outward.
  - Plumb `declTreeScope` through `ApplyAnimations` — the cascade already iterates
    rules in `ComputeStyle`; track which rule contributed `animation-name` and pass
    its scope.
- **Blink ref:** `core/dom/shadow_root.h`, `core/html/html_template_element.cc`
  (declarative shadow), `core/css/selector_checker.cc` (`::part` matching),
  `core/css/resolver/scoped_style_resolver.cc` (`KeyframesRuleMap`, `Parent()`),
  `core/css/resolver/style_resolver.cc` (`FindKeyframesRule`).
- **louis14:** `pkg/html/` (shadow model + template parsing), `pkg/css/stylesheet.go`
  (`::part` selector parse, `TreeScope` on `Rule`/keyframes),
  `pkg/css/matcher.go` (`::part` match), `pkg/css/cascade.go` (scoped resolution),
  `pkg/css/animation.go` (`FindKeyframesRule`).
- **Tests fixed:** `animation-name-in-shadow-part`,
  `animation-name-in-shadow-part-inner-match`,
  `animation-name-in-shadow-part-outer-match`, `animation-name-in-nested-shadow`.
- **Gate:** those 4 at 0% diff.

### Phase 5: B4 — animation vs. `!important` cascade origin
Verify (the `importantProps` skip in Phase 2 should mostly cover it) and finish.
- [ ] Confirm `animation-important-002`: the animated `color` (frozen 50% via
      `steps(2,end)` → endpoint `to` value `rgb(200,0,0)` for unvisited;
      `rgb(0,200,0)` bg) is applied, **but** `a:visited { color:white !important }`
      still wins for the visited link. Ensure the animation pass runs *after*
      author-normal and *before* the author-`!important` second pass in `ComputeStyle`
      (`pkg/css/cascade.go:432`).
  - Note: louis14 must also resolve `:visited` correctly here — the test's ref proves
    the visited link is rgb(0,150,0) bg (the `steps(2,end)` midpoint of the bg
    animation) with white text. Confirm `:visited` matching + the bg animation both
    land; if `:visited` is itself broken, that is a pre-existing matcher bug to fix
    foundationally, not a point patch.
- **Blink ref:** `core/css/resolver/cascade_priority.h` — origin order
  author-normal < animation < author-important.
- **louis14:** `pkg/css/cascade.go` `ComputeStyle` ordering.
- **Tests fixed:** `animation-important-002`.
- **Gate:** `animation-important-002` at 0% diff.

### Phase 6: Regression sweep & delivery
- [ ] Re-run the full css-animations bucket (sanctioned end-of-category run):
      expect 19 passing / 5 skipped / 3 OOS-failing.
- [ ] Spot-check adjacent categories that touch the cascade / shadow / transform code:
      a small sample of css-writing-modes (transform paint) + CSS2 (cascade) — *not*
      full suites. No regressions.
- [ ] Update this file's status; record the 3 parked tests and the reason.
- **Gate:** +13 net css-animations, 0 regressions.

---

## Out of scope (harness limitation) — formal park list

| Test | Reason | Would need |
|------|--------|-----------|
| `animation-opacity-pause-and-set-time.html` | WAAPI `element.animate()` + `pause()` + `currentTime` setter; ref is the t=500ms interpolated state. | A live Web Animations API with a controllable timeline + keyframe interpolation. |
| `animation-transform-pause-and-set-time.html` | Same WAAPI pattern; ref = `translate(500px)`. | Same. |
| `flip-running-animation-via-variable.html` | Ref state only exists after `classList.add('tweaked')` runs in `window.onload`; static initial state does not match. | DOM-mutating script execution + re-style + cubic-bezier eval. |

These are **not** louis14 layout/paint/parse bugs — they require an animation timeline
and/or DOM scripting the static reftest harness intentionally does not provide
(`reftest_runner_test.go` only runs static renders; it does not advance a clock or run
mutating scripts). Revisit only if a Web Animations API capability is added to
`pkg/js/`.

---

## Key questions (resolve during execution)
1. Does any in-scope `@keyframes` actually require *numeric interpolation* between
   distinct keyframe values? Survey says **no** — every in-scope case is `from`==`to`
   or frozen exactly on a keyframe offset. Confirm in Phase 2; if one slips through,
   add interpolation only for the property type that needs it (and reconsider whether
   it is truly static).
2. Is louis14's `:visited` matching correct independent of animations
   (`animation-important-002`)? Check in Phase 5; fix foundationally if not.
3. ~~Does louis14 already build a layout box for SVG child elements at all~~
   **Resolved by LOU-128.** SVG `<rect>`/`<circle>`/`<ellipse>`/`<path>` are laid out
   by `pkg/layout/svg/svg_shape.go` and painted by `pkg/render/svg_shape_painter.go`.
4. Where is the cleanest seam to thread `declTreeScope` from the cascade rule loop
   into `ApplyAnimations`? Phase 4 design decision.

## Decisions made
| Decision | Rationale |
|----------|-----------|
| Document-scope keyframes (Phase 2) before shadow DOM (Phase 4) | Proves the keyframe-application machinery on 8 tests before taking on the large shadow-DOM capability; foundational-first. |
| No numeric keyframe interpolation in this plan | No in-scope test needs it; all are `from`==`to` or frozen on an offset. Interpolation is a non-goal — adding it speculatively risks the "easy win" anti-pattern. |
| 3 tests parked OOS, not patched | They need a real animation timeline / DOM mutation; a static renderer cannot reproduce their refs. Per CLAUDE.md §1, no point fixes to fake them. |
| Timing model is a *static snapshot* | Document timeline is 0 in a static render; "current time" = `-animation-delay`. This is exactly how every in-scope "frozen" test is authored — it is not a hack, it is the spec evaluated at t=0. |

## Notes
- `pkg/css/animation.go` placement mirrors Blink's `core/animation/css/css_animations.*`
  living alongside the CSS resolver — keep it in `pkg/css/` next to `cascade.go`.
- The 5 `flags=dom` skips are correctly skipped by the harness and are not part of the
  18; do not touch them.
