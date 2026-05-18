# Task Plan: Pass the entire css-cascade WPT reftest category

## Goal
All `css-cascade` tests under `pkg/visualtest/testdata/wpt-css3/css-cascade/` pass at 0% pixel
diff via `TestWPTCSS3Reftests/css-cascade`. Baseline **19 passing / 33 failing / 1 skipped (53
run)** → close the 33 failures without regressing adjacent categories (css-color, css-fonts,
css-values, CSS2). This is core cascade machinery — every fix here is exercised by every other
category, so foundational correctness is doubly important.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff
   required, test-execution discipline (only the specific failing tests for the phase being worked,
   1–4 tests), operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory
   index pointing at the same rules.

If you are about to type a rule verbatim into this file or into code comments, stop and link instead.

## Baseline snapshot (2026-05-14)
From `grep "FAIL: TestWPTCSS3Reftests/css-cascade/" docs/reftest-survey-2026-05-14-raw.txt` (33 fails):

| Test | Bucket | Symptom in `output/reftests/*_test.png` |
|------|--------|------------------------------------------|
| all-prop-001.html | A: `all` shorthand | digits show **red** + a tall red border bar — `all: initial` ignored, `border/color/font` from prior rule survive |
| all-prop-002.html | A: `all` shorthand | `all: inherit` ignored → `.inline` divs stay `display:block`, red shows |
| all-prop-initial-visited.html | A + D | `all: initial` ignored on `<a>` |
| initial-background-color.html | B: CSS-wide keyword resolution | `background-color: initial` leaves the `background:red` from the previous declaration → red bar |
| initial-color-background-001.html | B | `color:initial` / `background-color:initial` leave red |
| unset-val-001.html | B | `color:unset` / `background-color:unset` leave red |
| unset-val-002.html | B | `display:unset` leaves `inline-block` instead of resolving to `inline` |
| important-prop.html | C: `!important` vs animations ordering | square is black w/ red border + red "FAIL" — `!important` background loses, animation `!important` wrongly applied |
| revert-layer-001.html … 005,007 | E: `revert-layer` keyword | red square — `revert-layer` parsed as an unknown color / no-op |
| revert-layer-009.html | E + style attr | `revert-layer` in inline `style=` ignored |
| revert-layer-010,011.html | E + animations | `revert-layer` as a keyframe value ignored |
| revert-layer-013,014.html | E + F | `revert-layer` + shadow DOM |
| layer-slotted-rule.html | F: shadow DOM + layers | `::slotted` rule + `@layer` inside shadow root |
| import-conditional-001.html | G: conditional `@import` | `@import ... (media)` / fallback list — all three sheets imported unconditionally, red wins |
| import-conditional-002.html | G | `@import ... supports(...)` — condition ignored, red wins |
| import-removal.html | G + DOM scripting | `@import` removed by JS `deleteRule` — not modelled |
| scope-implicit-001-print.html, -002, -005, -006 (.html) / -004 (.xhtml) | H: `@scope` | `@scope` block not parsed → `.a` unstyled |
| scope-part.html, scope-pseudo-element.html, scope-shadow-sharing.html, scope-ua-shadow-host.html, scope-visited.html | H + F | `@scope` + shadow DOM / `::part` / `:visited` |

### Bucket breakdown (fail counts)
- **Bucket A — the `all` shorthand (3):** `all-prop-001`, `all-prop-002`, `all-prop-initial-visited`.
- **Bucket B — CSS-wide keyword resolution `initial`/`unset` (4):** `initial-background-color`,
  `initial-color-background-001`, `unset-val-001`, `unset-val-002`. (`inherit` partly works today
  but only as a post-pass; it is folded into the same fix.)
- **Bucket C — `!important` ordering vs animations (1):** `important-prop`.
- **Bucket E — `revert-layer` keyword (10):** `revert-layer-001,002,003,004,005,007,009,010,011`
  plus `013,014` which additionally need Bucket F. (`revert` shares the machinery.)
- **Bucket F — shadow DOM / `::slotted` + layers (overlaps E and H):** `layer-slotted-rule`,
  `revert-layer-004,013,014`, and the shadow-DOM scope tests.
- **Bucket G — conditional `@import` (3):** `import-conditional-001`, `import-conditional-002`,
  `import-removal` (last also needs DOM scripting).
- **Bucket H — `@scope` (10):** `scope-implicit-001/002/004/005/006-print`, `scope-part`,
  `scope-pseudo-element`, `scope-shadow-sharing`, `scope-ua-shadow-host`, `scope-visited`.

The phases below are ordered foundational-first: A–C–E all depend on a **correct cascade-priority
model that carries per-declaration order and importance** and on **per-rule declaration *lists*
(not maps)**. That model is Phase 0/1. The `all` shorthand (Phase 2) and conditional `@import`
(Phase 4) sit on top. Shadow DOM (Phase 5) and `@scope` (Phase 6) are the two genuinely large
features and come last.

## Root-cause analysis (done — read before coding)

### The two structural defects underneath Buckets A, B, C, E

**Defect 1 — `Rule.Declarations` is a `map[string]string` (`pkg/css/stylesheet.go:62-69`).**
A map cannot hold two declarations for the same property in one rule, and it has no order.
Every `revert-layer` test relies on a rule like:

```css
#target { background-color: red; background-color: revert-layer; }
```

Both declarations are author, same selector, same layer — the *last* one wins, and only because
it is last. With a `map`, the second `Set` clobbers the first and the position is lost; there is
no "previous declaration of this property" for `revert-layer` to fall back to. `important-prop`'s
keyframe `border-color: green; border-color: red !important;` has the same shape. **`Declarations`
must become an ordered slice of `(property, value, important)` triples** — mirroring Blink's
`CSSPropertyValueSet`, which is an ordered list of `CSSPropertyValue` (property id + value +
`is_important_` + `is_set_from_shorthand_`). See `core/css/css_property_value_set.h`.

**Defect 2 — there is no `CascadePriority`.** `ComputeStyle` (`pkg/css/cascade.go:388-468`) sorts
matched `Rule`s by `(layerPriority, specificity)` only (`cascade.go:409-416`), then iterates
declarations in arbitrary map order, then does a second loop for `!important`
(`cascade.go:433-443`). It tracks `importantProps map[string]bool` to stop normal declarations
overwriting important ones. This is *almost* the cascade but it is missing:
  - **origin** (UA / user / author / animation / inline) as a first-class ordering key — currently
    UA styles are pre-applied (`applyUserAgentStyles`), presentational attributes pre-applied,
    inline applied last by a separate code path, animations applied by yet another (`ApplyAnimations`).
    There is no single comparable key, so `revert` (which must walk *down origins*) and
    `revert-layer` (which must walk *down layers within an origin*) have nothing to walk.
  - **declaration order within a rule** (lost to the map — Defect 1).
  - **rule order / source order** as a tiebreaker independent of specificity.

Blink packs all of this into one comparable `CascadePriority`
(`core/css/resolver/cascade_priority.h`): two integers, `high_bits_` = `[tree-order | origin+
importance]`, `low_bits_` = `[applied | declaration_index | rule_index | layer_order |
is_inline_style | …]`. `EncodeOriginImportance(origin, important)` returns `origin` normally and
`origin ^ 0xF` when important — the XOR is what makes `!important` *reverse* the origin order
(author-important beats user-important beats UA-important). louis14 needs the same single key.

louis14 will not need the full bit-packing — a small comparable Go struct
`CascadePriority{ Origin, Important, LayerOrder, TreeOrder, IsInline, RuleIndex, DeclIndex }`
with a `Less` method is enough, and is the exact analogue.

### Bucket A: the `all` shorthand falls through to the `default` case
`expandShorthand` (`pkg/css/style.go:1690-2185`) has **no `case "all"`**. `all: initial` therefore
hits the `default: style.Set("all", value)` branch (`style.go:2181-2183`) — it stores a bogus
property named `all` and resets nothing. The diff for `all-prop-001` shows exactly this: the
`border:solid red`, `color:red`, `font:…`, `width:0.5em`, `margin:10em` from the earlier
`.test, bdo` rule all survive.

Blink: `all` is the widest shorthand. `CSSProperty::IsAffectedByAll()` marks every longhand that
`all` resets (all of them **except** `direction`, `unicode-bidi`, and custom properties — see
`css_properties.json5` `affected_by_all` and `all-prop-001`'s own comment). The `all` shorthand's
`CSSShorthand` expansion (`css_shorthand.cc` / generated `ParseShorthand`) only accepts a
**CSS-wide keyword** (`initial`/`inherit`/`unset`/`revert`/`revert-layer`) and applies that keyword
to every affected longhand. So `all: initial` ≡ "`<longhand>: initial`" for every non-excluded
longhand.

### Bucket B: CSS-wide keywords are not resolved as cascaded values
`initial` and `unset` are never resolved. `inherit` is *partly* handled but wrongly — as a
**post-cascade text substitution**: `resolveInheritValues` (`cascade.go:818-836`) and
`resolvePseudoInheritKeyword` (`cascade.go:501-516`) scan the *final* style for the literal string
`"inherit"` and copy the parent's value. There is no analogue for `initial` (should drop the
property so the property's initial value applies) or `unset` (= `inherit` for inherited
properties, `initial` for non-inherited — `unset-val-001/002` test exactly this distinction).
`initial-background-color` shows the consequence: `background:red; background-color:initial;` —
the `initial` is stored as the literal value `"initial"`, the renderer does not understand it as a
background colour, and (because of Defect 1) the earlier `background:red` longhands from the same
shorthand are not even cleared.

Blink resolves CSS-wide keywords *inside* the cascade, per longhand, in `StyleCascade::Apply`
(`style_cascade.cc`): `initial` → `StyleBuilder::ApplyInitial`; `inherit` → `ApplyInherit`
(copies the parent's *computed* value); `unset` → `inherit` if `CSSProperty::IsInherited()` else
`initial`; `revert`/`revert-layer` → `ResolveRevert`/`ResolveRevertLayer` (Phase 3). The key
point: the keyword is resolved against the property's *inheritance class* and the *parent computed
value*, not against a string in the final map.

### Bucket C: `!important` and animation origin are not on the same scale
`important-prop` needs three facts simultaneously: (1) a normal author rule is overridden by an
in-effect animation; (2) an `!important` author rule overrides the animation; (3) an `!important`
declaration *inside* `@keyframes` is ignored. louis14 applies animations in `ApplyAnimations`
(called from `cascade.go:461`) *after* the `!important` pass and skips properties in
`importantProps` — so (2) works by luck, but (1) does **not**: a *normal* author `color:red` was
applied in the first pass and `ApplyAnimations` has no way to know the animation (`color:green`)
should beat it, because animation is not represented as a higher-origin cascade entry. And (3)
(`border-color: red !important` inside a keyframe being ignored) is not modelled at all.

Blink: animations are a *cascade origin* sitting between author-normal and author-important
(CSS Cascade §6.3). `StyleCascade::AddInterpolations` adds them to the same `CascadeMap` with
`CascadeOrigin::kAnimation`; `!important` inside `@keyframes` is dropped at parse time
(`StyleRuleKeyframe` declarations are forced non-important). Once animations are real cascade
entries with an origin, (1)(2)(3) all fall out of the single `CascadePriority` comparison.

### Bucket E: `revert-layer` has no machinery and no fallback target
`revert-layer` (and `revert`) appear nowhere in `pkg/css/*.go` except that `style.go:2422` treats
`inherit/initial/unset` as resettable in the border-radius corner expander. So `background-color:
revert-layer` is stored as the literal `"revert-layer"`, the renderer cannot parse it as a colour,
and the box renders red (the previous declaration) or transparent. Even once the keyword is
*recognised*, it needs a *target to revert to*:
  - `revert` rolls the property back to the value it would have had **with all declarations from
    this origin (and higher) removed** — i.e. the winning declaration from a *lower origin*.
  - `revert-layer` rolls back to the winning declaration from a **lower cascade layer in the same
    origin** (and, if none, behaves like `revert`).
This is only expressible if every candidate declaration is retained with its `CascadePriority`
(Phase 1) so the resolver can re-run the cascade with a filter. Blink: `ResolveRevert` /
`ResolveRevertLayer` in `style_cascade.cc` take a `CascadeOrigin&` out-param and look up the
next declaration below the revert boundary in the `CascadeMap`; `TargetOriginForRevert` defines
the origin-rollback ladder (author→user→UA→none).

`revert-layer-009` puts `revert-layer` in an **inline `style=`** (inline style is unlayered author
→ reverts to the layered/unlayered author rule below it); `revert-layer-010/011` put it in
**`@keyframes`** (animation origin → reverts to author origin). Both are just different start
points on the same ladder, so they need the origin model from Phase 1, not new code.

### Bucket G: `@import` is resolved unconditionally and conditions are discarded
`@import` is handled in the **HTML parser**, not the CSS parser: `Parser.resolveImports`
(`pkg/html/parser.go:494-540`) splits leading `@import` lines, fetches each via `p.cssFetcher`,
and *prepends* the fetched text. `parseImportURL` (`parser.go:542-597`) explicitly throws away the
media query / `supports()` condition ("For now, we import unconditionally" — `parser.go:554-555`).
So `import-conditional-001` imports `test-red`, `test-green`, **and** the second `test-red`
unconditionally; with no condition gating, the last `@import` (red) wins.
`import-conditional-002` is the same with `supports(display:block)` / `supports(foo:bar)`.
`import-removal` additionally mutates the sheet by JS (`sheet.deleteRule(0)`), which the static
runner does not execute.

Blink: `StyleRuleImport` carries a `MediaQuerySet` and a `supports` condition; the imported sheet
is only applied when both evaluate true (`StyleRuleImport::SetCSSStyleSheet` /
`CSSStyleSheet`/`StyleEngine` gating). louis14 already has `EvaluateMediaQuery`
(`stylesheet.go`) and a `@supports` parser (`parseSupportsRule`, referenced at
`stylesheet.go:835`) — the fix is to (a) keep `@import` as a real CSS construct, (b) parse and
attach the trailing `<media-query-list>` and optional `supports(<condition>)`, (c) evaluate them
before contributing the imported rules. `import-removal` additionally needs the minimal DOM
scripting the runner already gates on (`flags=dom`); see Phase 4 notes.

### Bucket H: `@scope` is not parsed at all
`splitRules` / the `@`-rule dispatch in `ParseStylesheet` (`stylesheet.go:383-427`) handles
`@media`, `@layer`, `@keyframes`, `@font-face`, `@counter-style`, `@container` and **silently
skips everything else** including `@scope` (`stylesheet.go:427`). The matcher knows `:scope`
(`matcher.go:307`) but nothing produces scoped rules. Every `scope-*` test therefore renders with
the `@scope` block's contents ignored. Half the failing scope tests are also shadow-DOM tests
(Phase 5). The print-only ones (`scope-implicit-001/002/004/005/006-print`) and
`scope-ua-shadow-host` need only `@scope` itself.

Blink: `StyleRuleScope` wraps a `StyleScope` (a scope-start `<selector-list>` + optional
scope-end `to (<selector-list>)`); scoped rules carry the `StyleScope` and the matcher
(`ElementRuleCollector` / `CheckPseudoHasArgumentTraversalIterator` and `StyleScope`-aware
matching in `selector_checker.cc`) limits a rule to elements that are descendants-or-self of a
scope root and not past a scope-end. `:scope` matches the scope root; an implicit `@scope { }`
(no prelude) is scoped to the stylesheet owner's parent element. Proximity is a new cascade
tiebreaker (closer scope root wins) — but for these reftests, getting *matching* right (which
elements are in scope) is what produces pixels; proximity only matters where two `@scope` blocks
overlap (`scope-proximity` is already passing/out of scope here).

## Blink references (study before writing code)

### Cascade priority & the cascade engine
- **`core/css/resolver/cascade_priority.h`** — `CascadePriority`. `EncodeOriginImportance(origin,
  important)` = `origin` or `origin ^ 0xF`. `CascadeOrigin` enum
  (`core/css/cascade_origin.h`): `kNone`, `kUserAgent`, `kUser`, `kAuthorPresentationalHint`,
  `kAuthor`, `kAnimation`, `kTransition`. Comparison is lexicographic over the packed integers and
  yields: origin/importance ▸ tree-order ▸ inline-style ▸ layer-order ▸ rule-index ▸
  declaration-index.
- **`core/css/resolver/style_cascade.{h,cc}`** — `StyleCascade`, `CascadeResolver`,
  `CascadeMap`. `MutableMatchResult()` / `AddMatchedProperties()` collect rule declarations;
  `AddInterpolations()` adds animations at `kAnimation` origin; `Apply()` →
  `ApplyCascadeAffecting` (direction/writing-mode first), `ApplyHighPriority` (font-size etc.),
  then the rest. `LookupAndApplyDeclaration` → `Resolve` → `StyleBuilder::ApplyProperty`.
- **`core/css/resolver/cascade_resolver.h`** — tracks the currently-applying property to detect
  cycles and to give `revert`/`revert-layer` their "current origin/layer" boundary.

### CSS-wide keywords, `revert`, `revert-layer`
- **`core/css/css_unset_value.h`, `css_initial_value.h`, `css_inherited_value.h`,
  `css_revert_value.h`, `css_revert_layer_value.h`** — the five CSS-wide keyword value types.
- **`core/css/resolver/style_cascade.cc`** — `ResolveRevert(origin, …)` and
  `ResolveRevertLayer(origin, layer, …)`; `TargetOriginForRevert(origin)` defines the rollback
  ladder (UA→none, user→UA, author/animation→user). `revert-layer` searches the `CascadeMap` for
  the highest-priority declaration **below the current layer within the same origin**, falling
  back to `revert` semantics when there is none.
- **`core/css/resolver/style_builder.cc` / generated `style_builder_functions`** —
  `ApplyInitial`/`ApplyInherit`/`ApplyValue` per property; `unset` dispatches to one of the first
  two via `CSSProperty::IsInherited()`.

### The `all` shorthand
- **`core/css/properties/shorthands/`** + generated `ParseShorthand` for `all` — accepts only a
  CSS-wide keyword; `core/css/css_properties.json5` `affected_by_all` flag and
  `CSSProperty::IsAffectedByAll()` enumerate the longhands `all` touches (everything except
  `direction`, `unicode-bidi`, custom properties; and `all` itself is not a real longhand).

### Cascade layers
- **`core/css/cascade_layer_map.{h,cc}`** — `CascadeLayerMap` builds one global layer order from
  every sheet in a tree scope; `GetLayerOrder(layer)` → `uint16_t`; `kImplicitOuterLayerOrder =
  numeric_limits<uint16_t>::max()` is the **unlayered** position (unlayered author rules beat all
  layered author rules — louis14's `layerPriority` (`cascade.go:374-386`) already does this with
  `len(layerOrder)`, which is correct and should be kept).
- **`core/css/cascade_layer.h`** — `CascadeLayer` is a tree of named/anonymous sub-layers;
  ordering is a pre-order walk so `@layer a { @layer b }` sorts `a` then `a.b` then siblings.

### Conditional `@import`
- **`core/css/style_rule_import.{h,cc}`** — `StyleRuleImport` holds the href, a `MediaQuerySet`,
  and a `supports` string; the imported sheet contributes rules only when both pass.
- **`core/css/parser/css_parser_impl.cc`** — `ConsumeImportRule` parses
  `@import <url> [layer]? [supports(<condition>)]? <media-query-list>? ;`.

### `@scope`
- **`core/css/style_rule.h`** — `StyleRuleScope`; **`core/css/style_scope.{h,cc}`** —
  `StyleScope` (scope-start selector list, optional scope-end `to(...)`, implicit flag).
- **`core/css/selector_checker.cc`** — `StyleScope`-aware matching: an element is *in scope* iff it
  is a descendant-or-self of a scope root and no ancestor-or-self up to it matches the scope-end.
  `:scope` matches the scope root. Proximity (`CascadePriority` `low_bits` again) breaks ties.
- **`core/css/parser/css_parser_impl.cc`** — `ConsumeScopeRule`.

### Declarative shadow DOM (Phase 5, the large one)
- **`core/dom/element.cc` `attachShadow` / `AttachDeclarativeShadowRoot`**, HTML parser
  `<template shadowrootmode>` handling, and `core/css/scoped_style_resolver.{h,cc}` — each shadow
  tree scope has its own `ScopedStyleResolver`; `::slotted()` rules live in the *slot's* scope and
  apply to *slotted* (light-DOM) children; `:host` matches the shadow host.

## Phased plan (foundational-first)

### Phase 0 — Ordered declaration lists (unblocks A, B, C, E)
**Goal.** Replace `Rule.Declarations map[string]string` + `Rule.Important map[string]bool` with an
ordered slice that can hold duplicate properties and per-declaration importance.
**Blink ref.** `core/css/css_property_value_set.h` — `CSSPropertyValue { property_id, value,
is_important_, is_set_from_shorthand_ }`, stored as an ordered list.
**louis14 targets.** `pkg/css/stylesheet.go` (`Rule` struct ~line 62, `DeclarationResult`
~line 1866, `parseDeclarations` ~line 1866–1960, `parseRules`, `parseLayerRule`,
`parseMediaRule`, `parseSupportsRule`, `parseContainerQuery`); `pkg/css/cascade.go`
(`ComputeStyle`, `ComputePseudoElementStyle`, `applyContainerQueryRules`); `pkg/css/animation.go`
(keyframe declarations); plus every `_test.go` that constructs a `Rule` literal.
**New types.** `type Declaration struct { Property, Value string; Important bool }`;
`Rule.Declarations []Declaration` (keep `Rule.Important` removed). Provide a small accessor
`(*Rule).LonghandDeclarations()` that runs `expandShorthand` per declaration *preserving order*
(today `expandShorthand` writes into a `Style`; Phase 0 adds an ordered-output variant or has the
cascade call it per declaration).
**Approach.** Mechanical but wide. `parseDeclarations` already walks declarations in source order
(`stylesheet.go:1866+`) — emit a slice instead of a map. `expandShorthand` stays as-is for the
*Style*-mutating call sites; the cascade switches to expanding declaration-by-declaration in order
so a later `background-color` in the same rule overrides an earlier `background` longhand.
**Tests fixed.** None yet on its own.
**Gate.** `pkg/css` unit tests green; `css-cascade` pass count unchanged (no regression);
spot-render `all-prop-001` only to confirm no crash.

### Phase 1 — `CascadePriority` + single-pass origin-aware cascade (unblocks B, C, E; fixes nothing visible yet but is the spine)
**Goal.** One comparable priority key per candidate declaration; one cascade pass that subsumes
UA / presentational-hint / author / animation / inline; retain *all* candidate declarations keyed
by priority so `revert`/`revert-layer` (Phase 3) can re-resolve.
**Blink ref.** `core/css/resolver/cascade_priority.h` (`CascadePriority`,
`EncodeOriginImportance`, `CascadeOrigin`); `core/css/resolver/style_cascade.cc` (`CascadeMap`,
`Apply`).
**louis14 targets.** `pkg/css/cascade.go` — rewrite `ComputeStyle` (`388-468`) and
`ComputePseudoElementStyle` (`520-693`); keep `buildLayerOrder`/`layerPriority` (`353-386`).
New file `pkg/css/cascade_priority.go`.
**New types.**
```
type CascadeOrigin int   // OriginUA, OriginUser, OriginPresentationalHint, OriginAuthor, OriginAnimation
type CascadePriority struct {
    Origin     CascadeOrigin
    Important  bool
    LayerOrder int   // from layerPriority(); unlayered = len(layerOrder)
    TreeOrder  int   // shadow tree depth — 0 for the document, Phase 5 sets >0
    IsInline   bool
    RuleIndex  int   // source order of the rule across all sheets
    DeclIndex  int   // position of the declaration within its rule
}
func (a CascadePriority) Less(b CascadePriority) bool   // mirrors Blink lexicographic order
type CascadeEntry struct { Priority CascadePriority; Property, Value string }
```
`Less` orders by: `encodeOriginImportance(Origin, Important)` ▸ `TreeOrder` ▸ `IsInline` ▸
`LayerOrder` ▸ `RuleIndex` ▸ `DeclIndex`, where `encodeOriginImportance` flips the origin rank for
`Important` (Blink's `^0xF`) so author-important > animation > author-normal > pres-hint > UA.
**Approach.** Build a `[]CascadeEntry` from: UA styles (synthesise as `OriginUA` entries instead
of pre-`Set`ing), presentational attributes (`OriginPresentationalHint`), every matched author
rule's longhand-expanded declarations (`OriginAuthor`, with `LayerOrder`/`RuleIndex`/`DeclIndex`),
inline style (`OriginAuthor`, `IsInline=true`), and in-effect animations (`OriginAnimation` — see
Phase 2 of *css-animations*, but the origin slot is reserved here). For each property, the winning
entry = max by `Less`. Store the full per-property sorted entry list on the `Style` (or a
side table) so Phase 3 can ask "what wins below priority P?". Custom properties and `font-size`
still need the existing "high-priority first" ordering — apply `font-size`/`writing-mode`/
`direction` before other properties (Blink `ApplyHighPriority`/`ApplyCascadeAffecting`).
**Tests fixed.** Expect `important-prop` to go green once animations are a real `OriginAnimation`
entry **and** `!important` keyframe declarations are dropped (do that drop in `animation.go`
parsing here — Blink forces keyframe declarations non-important).
**Gate.** `important-prop` passes; `css-cascade` pass count ≥ 20; no regression in css-color /
CSS2 (spot-render 2 prior-passing tests if the agent is unsure).

### Phase 2 — `all` shorthand + CSS-wide keyword resolution (`initial`/`inherit`/`unset`) (Buckets A, B)
**Goal.** `all: <css-wide-keyword>` expands to that keyword on every affected longhand; the cascade
resolves `initial`/`inherit`/`unset` per longhand against the property's inheritance class.
**Blink ref.** `CSSProperty::IsAffectedByAll()` + `affected_by_all` in `css_properties.json5`;
`StyleBuilder::ApplyInitial`/`ApplyInherit`; `unset` → inherited? inherit : initial
(`css_unset_value` handling in `style_cascade.cc`).
**louis14 targets.** `pkg/css/style.go` — add `case "all":` to `expandShorthand` (~line 1709);
new helpers near `inheritableProperties` (`cascade.go:839-852`). `pkg/css/cascade.go` — keyword
resolution moves *into* the cascade pass (replace `resolveInheritValues` `818-836` and
`resolvePseudoInheritKeyword` `501-516`); add `initialValue(property)` and a CSS-wide-keyword
resolver.
**New types / data.** `var affectedByAll map[string]bool` — every longhand louis14 knows, minus
`direction` and `unicode-bidi` (and custom properties handled separately). `var initialValues
map[string]string` — initial value per longhand (e.g. `background-color` → `transparent`,
`display` → `inline`, `color` → `canvastext`/`black`, border widths → `medium`, etc.); for
properties whose initial value is "absent" the resolver simply *omits* the property so existing
`Get`-with-default logic applies.
**Approach.**
  - `expandShorthand` `case "all"`: if `value` is one of `initial|inherit|unset|revert|
    revert-layer`, iterate `affectedByAll` and `style.Set(longhand, value)` — i.e. emit one
    declaration per affected longhand carrying the keyword. (In the Phase-1 cascade this happens
    during longhand expansion of the declaration list, preserving order.)
  - During the cascade pass, when a winning entry's value is a CSS-wide keyword:
    `initial` → set the property's initial value (or omit); `inherit` → copy the parent's
    *computed* value (parent style is already resolved — top-down traversal); `unset` →
    `inheritableProperties[prop] ? inherit : initial`. This replaces the brittle post-pass string
    scan and makes `inherit` correct for pseudo-elements too (the "parent" is the originating
    element — keep that special case).
  - `all: initial` must **not** touch `direction`/`unicode-bidi` — `all-prop-001` asserts the
    digits stay in RTL order; excluding them from `affectedByAll` is the whole fix.
  - `all: inherit` includes `display` — `all-prop-002` needs `.inline { all: inherit }` to inherit
    `display:inline` from the parent `<span>`. Since `display` *is* in `affectedByAll` and the
    keyword resolver handles `inherit` generically, this falls out for free.
**Tests fixed.** `all-prop-001`, `all-prop-002`, `all-prop-initial-visited`,
`initial-background-color`, `initial-color-background-001`, `unset-val-001`, `unset-val-002`.
**Gate.** All 7 pass; `css-cascade` ≥ 27; no regression in css-fonts/css-color (the `initialValues`
table is the risk surface — render 1–2 prior-passing color tests if unsure).

### Phase 3 — `revert` and `revert-layer` (Bucket E, non-shadow)
**Goal.** `revert` rolls a property back to the winning declaration from a *lower origin*;
`revert-layer` rolls back to the winning declaration from a *lower cascade layer in the same
origin*, falling back to `revert` when there is none.
**Blink ref.** `core/css/resolver/style_cascade.cc` — `ResolveRevert`, `ResolveRevertLayer`,
`TargetOriginForRevert`; `css_revert_value.h`, `css_revert_layer_value.h`.
**louis14 targets.** `pkg/css/cascade.go` (the Phase-1 cascade resolver), `pkg/css/style.go`
(recognise the two keywords so they are never mistaken for colours/lengths).
**Approach.** Phase 1 already retains, per property, the full list of `CascadeEntry` sorted by
`CascadePriority`. To resolve a winning entry whose value is `revert` or `revert-layer`:
  - `revert`: find the highest-priority entry for that property whose `Origin` is strictly below
    the current entry's `Origin` (ladder: Animation/Author → PresentationalHint → UA → none). If
    found, recurse-resolve *that* entry's value (it may itself be a CSS-wide keyword); if none, the
    property gets its initial value (Phase 2 machinery).
  - `revert-layer`: find the highest-priority entry for that property with the **same `Origin`**
    and **same `Important`** but a strictly lower `LayerOrder` (treating unlayered = `len(order)`,
    so unlayered → next-lower layer; layer N → layer N-1; layer 0 → no lower layer). If found,
    recurse-resolve it; if none, fall through to `revert` semantics.
  - Importance interacts: `revert-layer !important` (`revert-layer-005`) is itself an important
    entry, so it reverts within the *important* layered declarations — the `same Important` filter
    handles this; the `!important` declaration in the *third* `@layer` of `revert-layer-005` must
    therefore be the fallback target, not the normal `green`.
  - Inline-style start point (`revert-layer-009`): inline style is `OriginAuthor, IsInline=true,
    LayerOrder=unlayered`; `revert-layer` from there finds the unlayered author *rule* below it
    (the `#target` rule sets `green`).
  - Animation start point (`revert-layer-010/011`): a keyframe value of `revert-layer` is an
    `OriginAnimation` entry; reverting "within the same origin, lower layer" finds nothing →
    falls to `revert` → next origin down is `OriginAuthor` → the author `width:150px` / `--x:150px`
    wins. `revert-layer-011` additionally exercises a custom property (`--x`) and `@property`
    registration — the custom-property revert path must use the same resolver; `@property`
    `initial-value` is the `revert` "no lower origin" fallback.
**Tests fixed.** `revert-layer-001`, `-002`, `-003` (`all: revert-layer` — combines Phase 2's
`all` expansion with this resolver), `-005`, `-007`, `-009`, `-010`, `-011`.
(`revert-layer-004`, `-013`, `-014` need Phase 5.)
**Gate.** Those 8 pass; `css-cascade` ≥ 35; no regression.

### Phase 4 — Conditional `@import` (Bucket G)
**Goal.** `@import <url> [layer]? [supports(<cond>)]? <media-query-list>? ;` contributes its rules
only when the `supports()` condition and the media-query list both evaluate true.
**Blink ref.** `core/css/style_rule_import.{h,cc}`, `CSSParserImpl::ConsumeImportRule`.
**louis14 targets.** Primary: keep `@import` resolution but make it *conditional*. Two viable
placements — pick the one that keeps `@import` a CSS construct:
  - **Preferred:** parse `@import` in `pkg/css/stylesheet.go` (`ParseStylesheet`'s `@`-rule
    dispatch, ~line 383–427) into a `StyleRuleImport`-like record `{ URL, Layer, Supports string,
    Media *MediaQuery }`, resolve the URL via the existing fetcher, parse the imported sheet, and
    *only if* `EvaluateSupports(Supports)` && `EvaluateMediaQuery(Media, …)` splice its rules in
    at the `@import` position (assigning `Layer` if present). This needs the fetcher handed to the
    CSS parser; if that is impractical, keep fetching in `pkg/html/parser.go:resolveImports` but
    **stop discarding the condition** — have `parseImportURL` return the trailing condition text
    and skip the fetch when the condition is statically false, or tag the prepended block.
  - `parseImportURL` (`pkg/html/parser.go:542-597`) must stop dropping everything after the URL
    (`parser.go:554-555`): parse the optional `supports(...)` and the media-query list.
**Approach.** `support/test-green.css` and `support/test-red.css` are *empty* in the repo — the
real WPT fixtures set `div{background:green}` / `div{background:red}`; confirm the runner's
`cssFetcher` resolves `support/*.css` (it must, or the test cannot pass). The cascade work is
nil — once only `test-green.css` is contributed, the existing cascade picks green over the
inline `div{background:red}` by source order. `import-conditional-001`'s comma fallback
(`@import "x" (cond), nonsense;`) means: a media-query *list* where any entry matching counts;
`nonsense` is an invalid query and is ignored. `import-conditional-002`'s `supports(foo: bar)` is
a valid-syntax but unsupported declaration → false.
`import-removal` additionally requires executing the `<script>` (`insertRule`/`deleteRule`) — the
runner skips `flags=dom`, but this test has *no* `flags=dom` attribute, so either (a) implement the
minimal `CSSStyleSheet.insertRule`/`deleteRule` the test uses, or (b) if that is out of scope for
css-cascade, document it as the one test deferred to a DOM-scripting phase. Recommend (a): it is
~30 lines and the same minimal scripting `revert-layer` shadow tests will also want.
**Tests fixed.** `import-conditional-001`, `import-conditional-002`, and `import-removal` if (a) is
done.
**Gate.** Those tests pass; `css-cascade` ≥ 37; no regression in any category that uses `@import`.

### Phase 5 — Declarative shadow DOM, `::slotted`, `:host`, `TreeOrder` (Bucket F)
**Goal.** Parse `<template shadowrootmode=open>` (and the `<script>`-driven `attachShadow` the
remaining tests use) into real shadow trees; resolve `:host`, `::slotted(...)`, per-tree-scope
stylesheets, and the `TreeOrder` field of `CascadePriority`.
**Blink ref.** HTML parser declarative-shadow-root handling; `core/dom/element.cc`
`AttachDeclarativeShadowRoot`; `core/css/scoped_style_resolver.{h,cc}`; `selector_checker.cc`
`::slotted` / `:host` matching. Cascade: shadow-tree rules sit at a *later tree order* than the
document for the same origin (`CascadePriority` `tree_order`).
**louis14 targets.** `pkg/html/parser.go` (declarative shadow root construction; minimal
`attachShadow` + `innerHTML` scripting for the `<script>`-based tests), `pkg/css/matcher.go`
(`:host`, `::slotted`), `pkg/css/cascade.go` (per-tree-scope stylesheet collection; set
`TreeOrder`), layout/render tree builders (flatten the shadow tree for layout — slotted light-DOM
children render in the slot's place).
**Approach.** This is the single largest feature and is genuinely new subsystem work; scope it as
its own sub-plan. The css-cascade payoff is: `layer-slotted-rule` (a `@layer`'d `::slotted` rule
must beat an unlayered `::slotted` rule — needs Phase 1 layer order *inside* a shadow scope),
`revert-layer-004` (`revert-layer` from the document into a `:host` rule — needs `revert` to climb
*tree scopes*, an extension of the Phase-3 ladder), `revert-layer-013/014` (`revert-layer` across
layers *inside* a shadow scope), and the shadow-DOM half of Bucket H. Do not start Phase 5 until
1–4 are green.
**Tests fixed.** `layer-slotted-rule`, `revert-layer-004`, `revert-layer-013`, `revert-layer-014`.
**Gate.** Those 4 pass; `css-cascade` ≥ 41; no regression.

### Phase 6 — `@scope` (Bucket H)
**Goal.** Parse `@scope (<start>) [to (<end>)]? { … }` (and implicit `@scope { … }`); limit scoped
rules to in-scope elements; `:scope` matches the scope root; proximity as a tiebreaker.
**Blink ref.** `core/css/style_rule.h` `StyleRuleScope`; `core/css/style_scope.{h,cc}`
`StyleScope`; `core/css/selector_checker.cc` scope-aware matching; `CSSParserImpl::ConsumeScopeRule`.
**louis14 targets.** `pkg/css/stylesheet.go` (`@scope` in the `@`-rule dispatch ~line 383–427;
new `parseScopeRule`), `pkg/css/matcher.go` (in-scope test; `:scope` already at `matcher.go:307`
needs a real scope root rather than the document root), `pkg/css/cascade.go` (carry the scope on
the `Rule`, filter at match time, add proximity to `CascadePriority` low-order tiebreak).
**New types.** `type StyleScope struct { Start, End []Selector; Implicit bool }`;
`Rule.Scope *StyleScope`.
**Approach.** A rule with a `Scope` matches element `E` iff: there exists a *scope root* `R`
(an ancestor-or-self of `E` matching `Scope.Start`, or — for implicit scope — the stylesheet
owner's parent) such that `E` is a descendant-or-self of `R`, the rule's own selector matches `E`
with `:scope` bound to `R`, and no ancestor-or-self of `E` up to `R` matches `Scope.End`. The
print-only tests (`scope-implicit-001/002/004/005/006-print`) and `scope-ua-shadow-host` exercise
only the implicit form and simple start selectors. `scope-pseudo-element` needs `:scope::before/
::after/::marker` (the pseudo-element pipeline must accept a scoped originating rule).
`scope-part`, `scope-shadow-sharing`, `scope-visited` also need Phase 5 (shadow DOM); sequence
them after Phase 5 lands.
**Tests fixed.** `scope-implicit-001-print`, `scope-implicit-002-print`,
`scope-implicit-004-print.xhtml`, `scope-implicit-005-print`, `scope-implicit-006-print`,
`scope-ua-shadow-host`, `scope-pseudo-element`, and (with Phase 5) `scope-part`,
`scope-shadow-sharing`, `scope-visited`.
**Gate.** All 10 scope tests pass; `css-cascade` = 52/52 (the 1 skipped stays skipped or is
un-skipped if it no longer needs unavailable infra); no regression anywhere.

## Sequencing summary
| Phase | Theme | Tests fixed | Running total |
|-------|-------|-------------|---------------|
| 0 | Ordered declaration lists | 0 | 19 |
| 1 | `CascadePriority` + origin-aware single-pass cascade | `important-prop` | 20 |
| 2 | `all` shorthand + `initial`/`inherit`/`unset` | 7 (all-prop ×3, initial ×2, unset ×2) | 27 |
| 3 | `revert` / `revert-layer` (non-shadow) | 8 (revert-layer 001,002,003,005,007,009,010,011) | 35 |
| 4 | Conditional `@import` | 3 (import-conditional ×2, import-removal) | 38 |
| 5 | Declarative shadow DOM / `::slotted` / `:host` | 4 (layer-slotted-rule, revert-layer 004,013,014) | 42 |
| 6 | `@scope` | 10 (scope ×10) | 52 |

Phases 0–1 fix one visible test but are the load-bearing rewrite — do not skip ahead. Phases 2–4
are the bulk of the cheap wins and depend only on 0–1. Phases 5–6 are large standalone features;
each deserves its own detailed sub-plan before coding starts.

## Risk notes
- **Phase 0 is wide.** `Rule.Declarations` is referenced across `cascade.go`, `stylesheet.go`,
  `animation.go`, `matcher.go` and many `_test.go` files. Land it as one mechanical commit, keep
  `expandShorthand` behaviour identical for the `Style`-mutating callers, and run `pkg/css` unit
  tests before touching the cascade.
- **The `initialValues` table (Phase 2) is the highest regression risk** in the category —
  a wrong initial value silently changes every element that doesn't set the property. Cross-check
  each entry against `css_properties.json5` `initial` / Blink's `ComputedStyleInitialValues`, and
  prefer *omission* (let existing `Get`-default logic apply) over guessing.
- **Animation origin (Phase 1)** touches `pkg/css/animation.go` and the *css-animations* category —
  coordinate so the `OriginAnimation` slot and the keyframe-`!important`-drop are consistent with
  any parallel css-animations work.
- **Phases 5 and 6 overlap** on the shadow-DOM scope tests; do Phase 5 first so Phase 6 can reuse
  the tree-scope plumbing for scoped stylesheets.
