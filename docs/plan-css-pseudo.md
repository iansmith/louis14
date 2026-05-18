# Task Plan: css-pseudo WPT reftest category

## Goal
Take `TestWPTCSS3Reftests/css-pseudo` from **59 passing / 151 failing / 8 skipped** toward
**100% of the in-scope failures** at 0% pixel diff, grounded in Blink's pseudo-element and
highlight-pseudo architecture. In-scope work targets the **static** pseudo-element families
(`::before`/`::after`, `::first-line`, `::first-letter`, `::marker`) plus the one foundational
parser/cascade bug that currently *leaks* highlight-pseudo declarations onto originating
elements.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable:
1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first,
   0% diff required, test-execution discipline (only the 1–4 tests under work), operational
   rules (no `open`, commit before worktree agents, worktree commit scope, push with branch).
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory
   index (foundational correctness, Blink reference, all-tests-must-pass, study-Blink-first,
   Blink file placement, gofmt before/after edits, worktree fonts symlink).

If you are about to type a rule verbatim here or in code comments, stop and link instead.

---

## Triage: in-scope vs out-of-scope

The runner (`pkg/visualtest/reftest_runner_test.go`) skips only tests with `<meta name="flags"
content="...dom...">`. **None of the 151 css-pseudo failures carry `flags=dom`**, so every one
of them is *run* and currently *fails*. Triage therefore cannot rely on the flag — it must
look at whether the **`-ref` file** depicts a result that is only reachable with a runtime
selection / fragment-directive (JS), or whether the ref is plain static content that a correct
static renderer can match.

### Decision rule
- **OUT OF SCOPE** — the `-ref` is a *static depiction of the highlighted result*. Example:
  `selection-background-color-001.html` matches `reference/ref-filled-green-100px-square.xht`
  (a literal green square); `target-text-001.html` matches `target-text-lime-green-ref.html`
  (lime/green squares); `active-selection-014-ref.html` hard-codes
  `background-color: Highlight; text-decoration: HighlightText underline`. Passing these
  requires actually *painting a highlight overlay*, which requires a runtime
  `window.getSelection()` range or a `#:~:text=` fragment directive processed by JS. A static
  renderer cannot produce the selection, so it cannot match the ref. **Untestable until the
  engine runs selection/fragment-directive JS.**
- **IN SCOPE** — either (a) the family is fully static (`::before`/`::after`, `::first-line`,
  `::first-letter`, `::marker`), or (b) it is a highlight-pseudo test whose `-ref` *also*
  renders as plain unselected text in a static renderer (e.g. `active-selection-051-ref.html`
  is just `div { font-size: 300% }` over `Selected Text`). For (b) the *only* reason the test
  currently fails is the parser bug below — fix it and test == ref == plain text.

### Counts

| | Count |
|---|---|
| **Total css-pseudo failures** | **151** |
| **In scope (statically fixable)** | **103** |
| **Out of scope (needs runtime selection / fragment-directive JS)** | **48** |

### In-scope bucket breakdown (103)

| Bucket | Fails | Root cause summary |
|---|---:|---|
| **B1 — `::marker` content / styling / layout** | 49 | Marker is a paint-time hack (`render.go drawListMarker`), not a laid-out pseudo-element box. `::marker { content }` with counters, `text-transform`, `letter-spacing`, `font-variant-numeric`, `text-shadow`, `word-spacing`, `list-style-position`, bidi, line metrics all ignored or wrong. |
| **B2 — `::first-letter`** | 18 | `splitFirstLetter` takes only the first rune of the first *direct* text child: no leading-punctuation inclusion, no digraph/grapheme handling, no descent into inline children, no skipping of empty spans / `::marker` / `::before` generated content, no float exclusion. |
| **B3 — `::first-line`** | 18 | `applyFirstLineStyles` blindly overwrites the allowed properties on *every* item on the line, including child spans that set the property explicitly — wrong cascade. Also `background-color` not painted at all, first-line box metrics (line-height/font-size) wrong, first-line scoping confused by out-of-flow children and nested blocks. |
| **B4 — `::selection` / highlight-pseudo parser leak** | 14 | `::selection`, `::target-text`, `::highlight()`, `::spelling-error`, `::grammar-error` are *not parsed* as pseudo-elements; their declaration blocks leak onto the originating element. The 4 `active-selection-051..054` + 10 others whose refs are plain text fail *only* because of this leak. Fixing the parser to recognise + drop these makes test == plain ref. |
| **B5 — `::before`/`::after` generated content** | 4 | `::before` with `display:flex` is not run through flex layout; `::before`/`::after` content feeding `::first-letter`; pseudo-element box order with newer pseudos; `var()` in pseudo `content`. |

Sum: 49 + 18 + 18 + 14 + 4 = **103**.

### Out-of-scope list (48 — needs runtime selection / fragment-directive JS)
`active-selection-014, -016, -025, -027, -031, -063`,
`selection-background-color-001`, `selection-background-painting-order`,
`selection-intercharacter-011, -012`, `selection-link-001, -002`,
`selection-originating-decoration-color`, `selection-originating-underline-order`,
`selection-over-highlight-001`, `selection-overlay-and-grammar-001`,
`selection-overlay-and-spelling-001`, `selection-text-decoration-currentcolor`,
`spelling-error-001`, `grammar-error-001`, `grammar-spelling-errors-001, -002`,
`highlight-painting-001, -002, -003, -004`,
`highlight-painting-currentcolor-001, -001a, -002, -002a, -002b, -003, -003a, -003b, -004,
-004a, -004b, -005`,
`highlight-painting-shadows-horizontal, -shadows-vertical`,
`highlight-painting-soft-hyphens-001`, `highlight-styling-004, -005`,
`highlight-z-index-001, -002`, `highlight-custom-properties-dynamic-001`,
`target-text-001..010` (the 9 failing), `target-text-dynamic-001, -002, -004`,
`target-text-shadow-horizontal, -vertical`, `target-text-text-decoration-currentcolor`,
`svg-text-selection-002, -fill-only, -shadow, -stroke-only, -transparent-background`.

Each requires the highlight to actually be *painted*, which in turn requires either a live
`window.getSelection()` range or a `#:~:text=` fragment directive — both supplied only by the
inline `<script>` in the test (the WPT `support/selections.js` shipped in our testdata is an
empty stub). **Phase 5 builds the highlight cascade + overlay painter so that the day the
engine gains a selection model, these flip to passing with no further pseudo-element work** —
but they are not counted toward the in-scope goal.

---

## Blink references (grounding)

Pseudo-element style resolution and the highlight cascade live in:
- `core/css/resolver/style_resolver.cc` — `StyleResolver::ResolveStyle`,
  `PseudoElementStyleForElement`, `StyleForPseudoElement`; first-line uses the
  `kPseudoIdFirstLine` / `kPseudoIdFirstLineInherited` pseudo-IDs and a *separate inherited
  style layer*, so a descendant that sets `color` explicitly keeps its own value.
- `core/css/style-calculation.md` — Blink doc: "::first-line is treated like a child element,
  not part of the parent; rules cascade to it."
- `core/dom/first_letter_pseudo_element.cc` — `FirstLetterPseudoElement::FirstLetterTextLayoutObject`
  (walks inline descendants via `SlowFirstChild()`/`NextInPreOrder()` with a `stay_inside`
  boundary; skips marker contents, floats/out-of-flow, rejects buttons / atomic inlines /
  nested-`::first-letter`), `IsPunctuationForFirstLetter` (Unicode categories Ps/Pe/Pi/Pf/Po),
  `IsSpaceForFirstLetter`, `LengthOfGraphemeCluster` for digraphs/combining marks; the
  `Punctuation` state machine (NotSeen/Seen/Disallow).
- `core/layout/layout_text_fragment.h` — `LayoutTextFragment` splits a text node into a
  first-letter fragment + a remaining-text fragment (`SetIsRemainingTextLayoutObject`,
  `Start`, `FragmentLength`, `CompleteText`).
- `core/layout/list/list_marker.h` + `list_marker.cc` — `ListMarker::MarkerText`,
  `MarkerTextWithSuffix` / `MarkerTextWithoutSuffix`, `MarkerTextType`
  (`kNotText`/`kUnresolved`/`kOrdinalValue`/`kStatic`/`kSymbolValue`),
  `UpdateMarkerContentIfNeeded`, `GetContentChild`, `GetListStyleCategory`,
  `InlineMarginsForInside` / `InlineMarginsForOutside`; the marker is a real laid-out box
  (`LayoutInsideListMarker` / `LayoutOutsideListMarker`) styled by the `::marker` cascade.
- `core/highlight/highlight_style_utils.h` — `HighlightStyleUtils::HighlightPaintingStyle`,
  `HighlightPseudoStyle`, `ResolveColor` / `MaybeResolveColor`, `HighlightBackgroundColor`,
  `ResolveColorsFromPreviousLayer`, `SelectionTextDecoration`; the
  `HighlightColorProperty` set (`kCurrentColor`/`kFillColor`/`kEmphasisColor`/
  `kSelectionDecorationColor`/`kTextDecorationColor`/`kBackgroundColor`) and the
  layer-by-layer `currentColor` resolution.
- `core/paint/highlight_painter.h` + `highlight_painter.cc` — `HighlightPainter`,
  `HighlightLayer` / `HighlightPart` / `HighlightOverlay`, `PaintCase`,
  `PaintOriginatingShadow` → `PaintHighlightOverlays` → text → decorations order;
  `SelectionPaintState::PaintSelectionBackground` / `PaintSelectedText`.

---

## Phases

Foundational-first: **Phase 1** rebuilds the shared pseudo-element style-resolution +
box-generation backbone (parser + cascade) that every later phase rides on. Phases 2–4 are the
three static families. Phase 5 builds the highlight cascade + overlay painter (so that
out-of-scope tests are *unblocked* the moment a selection model exists, and the in-scope
parser-leak tests pass). Phase 6 is delivery.

Do **not** run the whole category. Per CLAUDE.md §4, run only the 1–4 tests under active work;
the single category baseline below was already taken.

---

### Phase 0: Baseline & categorization — **DONE**
- [x] Full css-pseudo run captured: 59 / 151 / 8 (`docs/reftest-survey-2026-05-14-raw.txt`).
- [x] Read ~30 failing tests + their `-ref` files across all families; read pre-rendered
  `output/reftests/*_diff|_ref|_test.png` sets for each bucket.
- [x] Triaged in-scope (103) vs out-of-scope (48); bucketed B1–B5 (this file).

---

### Phase 1: Pseudo-element parser + cascade backbone (foundational)
**Goal.** Every CSS pseudo-element token is parsed into `Selector.PseudoElement` (never left
in the selector string to leak onto the originating element), and the cascade resolves a
correct pseudo-element style — including the *inherited-layer* model that `::first-line` and
the highlight pseudos need. This phase fixes **B4's 14 in-scope leak failures outright** and is
a prerequisite for Phases 3, 4 and 5.

**Blink reference.**
- `core/css/parser/css_selector_parser.cc` — `CSSSelectorParser::ConsumePseudo` recognises the
  full pseudo-element set and the functional `::highlight(<name>)`.
- `core/css/resolver/style_resolver.cc` — `PseudoElementStyleForElement`,
  `StyleForPseudoElement`; pseudo styles inherit from the originating element's style.
- `core/style/computed_style.h` — `kPseudoIdFirstLine`, `kPseudoIdFirstLineInherited`,
  `kPseudoIdSelection`, `kPseudoIdTargetText`, `kPseudoIdSpellingError`,
  `kPseudoIdGrammarError`, `kPseudoIdHighlight`, `kPseudoIdMarker`,
  `kPseudoIdBefore`/`kPseudoIdAfter`/`kPseudoIdFirstLetter`.

**louis14 target files.**
- `pkg/css/stylesheet.go` — the pseudo-element extraction block (lines ~1192–1280) currently
  hand-codes only `before`/`after`/`first-letter`/`first-line`/`marker`. Replace the
  `strings.Contains` ladder with a single tokeniser pass that recognises the *complete* set:
  `before`, `after`, `first-letter`, `first-line`, `marker`, `selection`, `target-text`,
  `spelling-error`, `grammar-error`, `highlight(<ident>)`, `placeholder`,
  `file-selector-button`, `backdrop`, and unknown `::x` → store as a distinct
  `PseudoElement` value so the rule still never matches a real element. Capture the
  `highlight()` argument into a new `Selector.HighlightName string` field. Keep the existing
  `descendant:` convention and the `"" → "*"` universal rewrite.
- `pkg/css/stylesheet.go` `parseSelectorPart` — already breaks on `::`; no change needed once
  extraction is complete, but add a guard: if a part still contains `::` after extraction it
  is an *unrecognised* pseudo-element and the whole rule must be marked non-matching (set a
  sentinel `PseudoElement` such as `"unknown"`).
- `pkg/css/matcher.go` `FindMatchingRules` (line ~685) already skips `PseudoElement != ""`;
  verify the sentinel and `highlight()` rules are likewise skipped for normal matching.
- `pkg/css/cascade.go` `ComputePseudoElementStyle` (line ~492) — extend the
  `pseudoElement` switch so callers can request any of the new pseudo IDs; ensure pseudo
  styles inherit from the passed `parentStyles` (already done for custom properties — widen to
  all inherited properties, mirroring Blink's "pseudo inherits from originating element").

**New types.**
- `Selector.HighlightName string` (named `::highlight()` argument).
- `css.PseudoID` enum (or string constants) covering the full set — replaces ad-hoc string
  literals scattered across `cascade.go` / `layout_tree_builder.go`.

**Approach.** One tokeniser function `extractPseudoElement(selectorStr) (rest string,
pseudo string, highlightName string, forDescendants bool)`; call it once; everything
downstream keys off the returned `pseudo`. Unknown `::tokens` resolve to a sentinel so the
rule is parsed (no crash) but never applied to an element — eliminating the leak.

**Tests fixed (in-scope).** `active-selection-051, -052, -053, -054` (refs are plain text),
plus the subset of selection/highlight tests whose refs render as plain unselected text in a
static renderer. Net B4 in-scope: **14**.

**Gate.** `active-selection-051..054` pass at 0% diff; no regression in the 59 currently-passing
css-pseudo tests; CSS2 99/99 unaffected. Verify via the 4 named tests only.

---

### Phase 2: `::marker` as a laid-out pseudo-element box (B1 — 49)
**Goal.** Replace the paint-time marker hack with a real `::marker` pseudo-element box that is
styled by the `::marker` cascade and laid out through the inline/fragment pipeline, so
`content` (incl. `counter()`/`counters()`), `text-transform`, `letter-spacing`,
`word-spacing`, `font-variant-numeric`, `text-shadow`, `line-height`, `tab-size`,
`unicode-bidi`, `list-style-position`, `text-align`, `hyphens`, `word-break`, `overflow-wrap`,
`text-combine-upright`, `text-emphasis`, `text-decoration-skip-ink` all apply.

**Blink reference.**
- `core/layout/list/list_marker.cc` — `ListMarker::MarkerText` (resolves counter / string /
  symbol text via `MarkerTextType`), `UpdateMarkerContentIfNeeded` (when `::marker { content }`
  is present the marker's child is a generated content box, *not* counter text),
  `GetContentChild`, `ListItemOrdinal` for the ordinal value.
- `core/layout/list/layout_inside_list_marker.cc` / `layout_outside_list_marker.cc` — the
  marker is a real box; `list-style-position` on the *list item* (not the `::marker`) selects
  inside vs outside; `InlineMarginsForOutside` / `InlineMarginsForInside`.
- `core/css/resolver/style_resolver.cc` — `::marker` UA style sets `unicode-bidi: isolate`,
  `text-transform: none`, `white-space: pre`; `::marker` only accepts the marker-allowed
  property subset (CSS Pseudo-4 §4.4) — `color`, `direction`, `font-*`, `content`,
  `text-combine-upright`, `unicode-bidi`, `white-space`, `letter-spacing`, `word-spacing`,
  `line-height`, `text-shadow`, `text-transform`, `animation-*`, `transition-*`.

**louis14 target files.**
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` (line ~706) currently only
  produces a node for `list-style-position: inside`; widen it so a marker `LayoutInputNode` is
  *always* created for `display:list-item` (both inside and outside), carrying the resolved
  `::marker` style and content. `resolveContentText` must support `counter()` / `counters()`
  with an explicit `list-style-type` argument.
- `pkg/layout/unpositioned_list_marker.go` — turn the stubbed `AddToBox` /
  `AddToBoxWithoutLineBoxes` / `Layout` into the real layout-time placement, mirroring Blink's
  `UnpositionedListMarker`. This is the architectural pivot: markers stop being a render-time
  afterthought.
- `pkg/layout/inline_layout.go` — accept the marker box as the first in-flow inline item for
  `list-style-position: inside`; for `outside`, place it in the marker margin via the
  unpositioned-marker protocol.
- `pkg/layout/block_layout.go` — wire `ListMarkerBlockNodeIfListItem` so the marker node
  reaches the layout algorithm (today `IsValid()` never returns true).
- `pkg/render/render.go` — delete the bespoke `drawListMarker` / `formatListMarker` /
  `drawListMarkerInside` / `drawListMarkerOutside` text path; the marker now paints as an
  ordinary fragment. Keep `formatListMarker`'s counter-style numbering logic but move it into
  the marker-content resolution at tree-build time.
- `pkg/render/paint_layer.go` — drop the `MarkerContent` / `MarkerColor` / `MarkerFontSize` /
  `HasMarkerColor` / `HasMarkerFont` shortcut fields once the marker is a real layer.

**New types.**
- `MarkerTextType` enum (`NotText`/`Unresolved`/`OrdinalValue`/`Static`/`SymbolValue`),
  mirroring Blink.
- A `MarkerContent` resolution result type carrying resolved text + whether it came from
  `content` vs `list-style-type`.

**Approach.** Build the marker as a synthetic inline-level pseudo box exactly like `::before`
(`createPseudoElement` is the template). UA `::marker` defaults applied first
(`unicode-bidi: isolate`, `white-space: pre`, `text-transform: none`), then author `::marker`
rules cascaded on top, then the marker-allowed-property filter. `list-style-position` is read
off the *originating list-item's* style, never the `::marker`'s. The marker box then flows
through normal inline layout, so every text property "just works" for free.

**Tests fixed.** All 49 B1 tests: `marker-content-001..024`, `marker-text-transform-*`,
`marker-letter-spacing`, `marker-word-spacing`, `marker-line-height`, `marker-line-break`,
`marker-list-style-position`, `marker-text-align-002/003`, `marker-font-variant-numeric-*`,
`marker-hyphens`, `marker-overflow-wrap`, `marker-tab-size`, `marker-text-combine-upright`,
`marker-text-decoration-skip-ink`, `marker-text-emphasis`, `marker-text-shadow`,
`marker-unicode-bidi-default`, `marker-word-break`, `marker-and-other-pseudo-elements`,
`marker-animate-002`, `marker-intrinsic-contribution-002`.

**Gate.** `marker-content-001`, `marker-content-002`, `marker-text-transform-uppercase`,
`marker-list-style-position` pass at 0% diff. Spot-check 2 existing list reftests outside
css-pseudo (e.g. a `css2/lists` or `css-lists` test) for no regression.

---

### Phase 3: `::first-letter` text identification (B2 — 18)
**Goal.** Port Blink's `FirstLetterPseudoElement::FirstLetterTextLayoutObject` so the
first-letter span covers the correct grapheme(s): leading punctuation + first typographic
letter (incl. trailing same-node punctuation), digraphs/combining marks as one unit, descending
into inline children, skipping empty inline elements / `::marker` / floats / out-of-flow, and
covering text generated by `::before`.

**Blink reference.**
- `core/dom/first_letter_pseudo_element.cc` — `FirstLetterTextLayoutObject` (the
  `SlowFirstChild()`/`NextInPreOrder()` walk with the `stay_inside` boundary; skip marker
  contents but don't escape the list item; skip floats / out-of-flow; reject buttons /
  atomic inlines / nested-`::first-letter`), `IsPunctuationForFirstLetter` (Unicode general
  categories Ps, Pe, Pi, Pf, Po), `IsSpaceForFirstLetter`, the `Punctuation` state machine,
  `LengthOfGraphemeCluster`.
- `core/layout/layout_text_fragment.h` — first-letter fragment + remaining-text fragment split.

**louis14 target files.**
- `pkg/layout/layout_tree_builder.go` — replace `splitFirstLetter` (line ~1125) and
  `applyFirstLetterSplit` (line ~1096). The new `firstLetterTextNode` walk must:
  recurse into inline children; skip `LayoutInputNode`s that are empty / whitespace-only /
  the `::marker` box / floated / abs-pos; accept the `::before` generated text as the source;
  return the *node + byte range* of the first-letter grapheme cluster(s).
- `pkg/layout/layout_tree_builder.go` — `applyFirstLetterSplit` must run *after* `::before`
  generation (it already runs after `createPseudoElement`, but verify the `::before` node is
  visible to the walk) and after the `::marker` box exists (Phase 2) so it can be skipped.
- `pkg/text/` — add a `GraphemeClusterLength(s string, offset int) int` helper (Unicode
  extended grapheme cluster boundaries) and an `IsFirstLetterPunctuation(r rune) bool` helper
  (categories Ps/Pe/Pi/Pf/Po). Mirror Blink file placement: punctuation/grapheme helpers go in
  `pkg/text/` (text segmentation), not `pkg/layout/`.

**New types.**
- `firstLetterRange` value type: `{ node *LayoutInputNode; start, end int }` describing the
  span to wrap.
- `punctuationState` enum (`NotSeen`/`Seen`/`Disallow`) used by the walk.

**Approach.** Two-pass like Blink: (1) find the first eligible text-bearing
`LayoutInputNode` by pre-order walk with skip rules; (2) within it, consume leading spaces,
then a run of leading punctuation, then exactly one grapheme cluster, then any immediately
following same-node punctuation — that byte range becomes the `::first-letter` span. Splitting
across multiple nodes (`first-letter-with-span`: the `"` is in a child `<span>`) means the
wrap may need to promote a *prefix of a descendant* — model it as wrapping the descendant's
leading range, matching the ref where the whole span is colored.

**Tests fixed.** `first-letter-004`, `-005`, `first-letter-digraph`,
`first-letter-punctuation-and-space`, `first-letter-punctuation-dynamic`,
`first-letter-with-span`, `first-letter-with-quote`, `first-letter-with-preceding-new-line`,
`first-letter-with-before-after`, `first-letter-skip-empty-span`,
`first-letter-skip-empty-span-nested`, `first-letter-skip-marker`,
`first-letter-exclude-inline-marker`, `first-letter-exclude-inline-child-marker`,
`first-letter-exclude-block-child-marker`, `first-letter-list-item-dynamic-001`,
`first-letter-opacity-001`, `first-letter-opacity-float-001`,
`first-letter-of-html-root-refcrash`.

**Gate.** `first-letter-with-span`, `first-letter-punctuation-and-space`,
`first-letter-skip-empty-span`, `first-letter-with-before-after` pass at 0% diff; existing
passing `first-letter-001/002/003` unaffected.

---

### Phase 4: `::first-line` inherited-style cascade (B3 — 18)
**Goal.** `::first-line` styles apply to the first formatted line via a *separate inherited
style layer* — a descendant inline that sets an allowed property *explicitly* keeps its own
value; only properties that would otherwise be inherited pick up the first-line value. Also
paint `::first-line` `background-color`, and get first-line box metrics (`line-height`,
`font-size`) right, and scope first-line correctly across out-of-flow / nested-block children.

**Blink reference.**
- `core/css/resolver/style_resolver.cc` + `core/style/computed_style.h` — the
  `kPseudoIdFirstLine` vs `kPseudoIdFirstLineInherited` split: Blink computes a first-line
  style *and* a first-line-inherited style; layout applies the inherited one down the inline
  tree, but an element's own matched declarations still win (standard cascade specificity).
- `core/layout/layout_block_flow.cc` — `LayoutBlockFlow::FirstLineStyle` /
  `UpdateFirstLineStyle`; the first-line box is generated only over the content of the first
  formatted line.
- `core/css/style-calculation.md` — "::first-line is treated like a child element."

**louis14 target files.**
- `pkg/layout/inline_layout.go` — rewrite `applyFirstLineStyles` (line ~1114). Instead of
  unconditionally overwriting `overrides` onto every item's cloned style, for each item:
  apply a first-line override for property `P` *only if* the item's own originating element
  did not set `P` explicitly in its cascade. This needs the item's style to carry an
  "explicitly set" set (or the builder to pass the first-line style as an *inherited parent*
  the item's cascade runs against). Prefer the Blink model: build a `FirstLineInheritedStyle`
  at tree-build time and have inline items on line 0 resolve against it as their inherited
  parent.
- `pkg/layout/layout_tree_builder.go` — `computeFirstLineStyle` (line ~1069) currently stores
  one `FirstLineStyle`. Add `FirstLineInheritedStyle` (the inherited-only subset) and store
  both on the `LayoutInputNode`.
- `pkg/layout/layout_input_node.go` — add `FirstLineInheritedStyle *css.Style` next to the
  existing `FirstLineStyle` (line ~78).
- `pkg/render/render.go` / `pkg/render/paint_layer.go` — paint the first-line
  `background-color` over the first line box's inline extent (today it is dropped — see
  `first-line-background-001` rendering blank).
- `pkg/layout/inline_layout.go` — first-line box metrics: when the first line carries a
  `::first-line` `font-size` / `line-height`, the line box height must use those (see
  `first-line-line-height-001/002`); first-line scoping must skip out-of-flow children when
  deciding what "the first line" contains (`first-line-with-out-of-flow*`).

**New types.**
- `LayoutInputNode.FirstLineInheritedStyle *css.Style`.
- A small `firstLineCascadeContext` helper that, given an inline item and the originating
  element's explicitly-set property set, decides per-property whether the first-line value
  applies.

**Approach.** Mirror Blink's two-style model. At tree-build time compute both the full
`::first-line` style and the inherited-only projection. During inline layout of line 0, each
item's effective style = its own cascaded style, but with *inheritable, non-explicitly-set*
properties sourced from `FirstLineInheritedStyle` instead of the normal parent. `background`
and other non-inherited first-line properties apply to the first-line box itself, not to
descendants. Out-of-flow and nested-block children are excluded from "the first line" exactly
as in normal inline-formatting-context line assignment.

**Tests fixed.** `first-line-background-001`, `first-line-color-002`,
`first-line-change-inline-color`, `first-line-change-inline-color-nested`,
`first-line-line-height-001`, `-002`, `first-line-opacity-001`,
`first-line-on-ancestor-block`, `first-line-nested-gcs`,
`first-line-inherited-with-transition`, `first-line-and-marker`,
`first-line-with-out-of-flow`, `first-line-with-out-of-flow-and-nested-div`,
`first-line-with-out-of-flow-and-nested-span` (14 named) plus the remaining B3 first-line
failures surfaced by the same root cause.

**Gate.** `first-line-background-001`, `first-line-change-inline-color`,
`first-line-line-height-001`, `first-line-with-out-of-flow` pass at 0% diff; existing passing
`first-line-color-001` and `first-line-line-height-003` unaffected.

---

### Phase 5: Highlight cascade + overlay painter (B4 painter half + unblocks the 48)
**Goal.** Build the highlight-pseudo *cascade* and *overlay painting* infrastructure —
`::selection`, `::target-text`, `::spelling-error`, `::grammar-error`, `::highlight()` — so
that (a) the in-scope parser-leak tests stay green with a *correct* (empty, in a static
renderer) highlight, and (b) the 48 out-of-scope tests become *one selection-model change
away* from passing. This phase does **not** ship a selection model; it ships everything else.

**Blink reference.**
- `core/highlight/highlight_style_utils.cc` — `HighlightStyleUtils::HighlightPaintingStyle`
  (combine originating + pseudo style), `HighlightPseudoStyle`, `ResolveColor` /
  `MaybeResolveColor`, `HighlightBackgroundColor`, `ResolveColorsFromPreviousLayer` (the
  layer-by-layer `currentColor` chain), `SelectionTextDecoration`; the
  `HighlightColorProperty` set.
- `core/highlight/highlight_registry.cc` / `highlight.cc` — the named-highlight registry that
  `::highlight(<name>)` resolves against.
- `core/paint/highlight_painter.cc` — `HighlightPainter` painting order
  (`PaintOriginatingShadow` → `PaintHighlightOverlays` → text → decorations),
  `HighlightLayer` / `HighlightPart` / `HighlightOverlay`, `PaintCase`;
  `SelectionPaintState::PaintSelectionBackground` / `PaintSelectedText`.
- CSS Pseudo-4 §7 (highlight cascade): a highlight pseudo's used value for a property is the
  cascade of *all* highlight rules of that type matching any ancestor, with `currentColor`
  resolving against the *previous highlight layer*, not the originating element.

**louis14 target files.**
- `pkg/css/cascade.go` — add highlight-pseudo style resolution mirroring
  `HighlightStyleUtils::HighlightPseudoStyle`: collect `::selection` / `::target-text` /
  `::spelling-error` / `::grammar-error` / `::highlight(name)` rules matching the element *and
  its ancestors* and cascade them (highlight inheritance is by *originating tree*, not pseudo
  tree).
- `pkg/render/` — new file `pkg/render/highlight_painter.go` mirroring Blink's
  `core/paint/highlight_painter.*`: the `HighlightLayer` / `HighlightPart` / `HighlightOverlay`
  types, the overlay decomposition of a text fragment into per-highlight segments, and the
  paint order (originating shadow, then highlight backgrounds bottom-up, then text in the
  topmost active layer's color, then decorations). Mirror Blink file placement: highlight
  painting goes in `pkg/render/` (the louis14 analogue of `core/paint/`).
- `pkg/render/highlight_painter.go` — `currentColor` in a highlight layer resolves against the
  layer below (`ResolveColorsFromPreviousLayer`), not the element color.
- `pkg/css/` — a `HighlightRegistry` keyed by name for `::highlight()` (populated from
  `CSS.highlights` once a JS path exists; empty today, which is correct for the static
  renderer).
- Selection/target ranges: introduce a `HighlightRangeSet` input on the render context,
  *empty by default*. With it empty, the overlay painter paints nothing — so a static render
  of an `active-selection-051`-style test stays plain text and matches its plain-text ref.

**New types.**
- `render.HighlightLayer`, `render.HighlightPart`, `render.HighlightOverlay`.
- `css.HighlightPseudoStyle` resolution result; `css.HighlightRegistry`.
- `render.HighlightRangeSet` (selection range, target-text range, spelling/grammar ranges) —
  empty in the static renderer.

**Approach.** Implement the full cascade + overlay decomposition + painter, driven by a
`HighlightRangeSet` that is *empty* in the current static renderer. This keeps in-scope
parser-leak tests green and makes the out-of-scope set a pure data-supply problem (populate
`HighlightRangeSet` from a future selection model / `#:~:text=` fragment-directive parser).

**Tests fixed (in-scope).** Holds Phase 1's B4 gains green and converts any remaining
selection/highlight test whose ref is plain text. **Does not** count the 48 out-of-scope tests.

**Gate.** No regression in Phase 1's `active-selection-051..054`; the 59 original passing
tests plus all Phase 2–4 gains stay green.

---

### Phase 6: Generated-content gaps (B5 — 4) + delivery
**Goal.** Close the 4 `::before`/`::after` generated-content failures and confirm the whole
in-scope set.

**Blink reference.**
- `core/layout/layout_object.cc` — `LayoutObject::CreateObject` blockifies / runs a
  `::before` with `display:flex` through flex layout exactly like a real element.
- `core/css/properties/longhands/content.cc` + `core/css/css_variable_data.cc` — `var()` in
  `content` is substituted during cascade, before the content list is built.
- CSS Pseudo-4 §3 — generated-box order: `::marker`, `::before`, children, `::after`.

**louis14 target files.**
- `pkg/layout/layout_tree_builder.go` — `createPseudoElement` (line ~509): after computing the
  pseudo style, route the pseudo `LayoutInputNode` through the *same* display-driven layout
  dispatch as a real element so `display:flex` on `::before` builds a flex container
  (`before-as-flex-container`).
- `pkg/css/cascade.go` / `pkg/css/stylesheet.go` — ensure `var()` substitution runs on the
  `content` property of pseudo-element rules before `GetContentValues` parses it
  (`before-after-dynamic-custom-property-001` — verify it is not `flags=dom`; if it requires
  rAF it is out of scope and drops from B5).
- `pkg/layout/layout_tree_builder.go` — pseudo-element box order vs newer pseudos
  (`relative-box-order-of-pseudo-elements`): keep `::marker`, `::before`, children, `::after`
  ordering; unknown pseudos (`::scroll-button`, `::column`) are not generated, which is the
  correct static behavior.

**Delivery checklist.**
- [ ] Re-run the css-pseudo category once (sanctioned end-of-category run): expect
  **≥ 162 passing** (59 baseline + 103 in-scope), 0 regressions.
- [ ] Re-run CSS2 (expect 99/99) and spot-check `css-lists` / list reftests for the Phase 2
  marker rework.
- [ ] Confirm the 48 out-of-scope tests are unchanged (still failing — documented, not
  regressions) and that Phase 5's infrastructure is in place to flip them once a selection
  model lands.
- [ ] Final report: in-scope pass count, out-of-scope list, follow-up ticket for the
  selection model + `#:~:text=` fragment-directive parser.

---

## Out of scope (needs runtime selection / fragment-directive JS)
The 48 tests listed under **Out-of-scope list** above. Every one has a `-ref` that is a static
depiction of a *painted highlight*; matching it requires an actual selection range
(`window.getSelection()`) or a processed `#:~:text=` fragment directive — both produced only
by the test's inline `<script>`, which the static renderer does not execute. Phase 5 builds
the cascade + overlay painter so these become a pure data-supply problem; the data supply
(a selection model + fragment-directive parser) is a **separate follow-up ticket** and is not
part of this plan's pass-count goal.

---

## Key Questions
1. Phase 2: does routing the marker through real inline layout regress any currently-passing
   list reftest outside css-pseudo? Spot-check before/after.
2. Phase 3: how should a first-letter that spans into a descendant element (the `"` in a
   child `<span>`) be wrapped — promote a descendant prefix, or wrap the descendant whole?
   Blink wraps the minimal range; the `first-letter-with-span` ref colors the whole span
   because the span *is* the minimal range. Confirm with the 3 first-letter-with-span tests.
3. Phase 4: is the cleanest louis14 expression of `kPseudoIdFirstLineInherited` a second
   stored style + per-property "explicitly set" check, or re-running each line-0 item's
   cascade with the first-line style as inherited parent? Decide in the Phase 4 research step.
4. `before-after-dynamic-custom-property-001` — re-confirm it is not effectively
   `flags=dom` (it uses inline script); if the test genuinely needs rAF it moves to
   out-of-scope and B5 becomes 3.

## Decisions Made
| Decision | Rationale |
|---|---|
| Triage on the `-ref` file, not `flags` | No css-pseudo failure carries `flags=dom`; the real in/out boundary is whether the ref depicts a *painted highlight* (needs runtime) or plain content (static-fixable). |
| Phase 1 (parser + cascade) is foundational and first | Unrecognised `::selection`/`::target-text`/etc. *leak* declarations onto originating elements — a correctness bug that also blocks Phases 3–5; fixing it is a prerequisite, not a quick win. |
| `::marker` rebuilt as a real laid-out box, not patched | The paint-time `drawListMarker` hack cannot express `content`/`text-transform`/`letter-spacing`/bidi/line-metrics for ALL cases; only a real pseudo box flowing through inline layout generalises (CLAUDE.md §1). |
| Phase 5 builds highlight infra even though 48 tests stay out-of-scope | Foundational: the cascade + overlay painter is shared by the in-scope parser-leak tests and unblocks the out-of-scope set; building it now avoids a second pass later. |
| Mirror Blink file placement | grapheme/punctuation helpers → `pkg/text/`; highlight painter → `pkg/render/`; marker layout → `pkg/layout/` — per memory `feedback_blink_file_placement`. |
| Do NOT copy project rules into this plan | They live in CLAUDE.md + memory; linked at top. |

## Notes
- Test command template:
  `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-pseudo/<name>' -v`
- Pre-rendered diff artifacts: `output/reftests/<name>_{diff,ref,test}.png`.
- Raw baseline: `docs/reftest-survey-2026-05-14-raw.txt`
  (`grep "FAIL: TestWPTCSS3Reftests/css-pseudo/"`).
- Worktree agents: symlink `fonts/` from the main dir before any broad sweep
  (memory `feedback_worktree_fonts`); run `go fmt` before and after Go edits
  (memory `feedback_gofmt_after_edits`).
</content>
</invoke>
