# Task Plan: Shared list-marker / `::marker` box-model foundation

## Goal
Convert louis14's list marker from a paint-time hack into a **real laid-out
`::marker` pseudo-element box** implementing Blink's `ListMarker` +
`UnpositionedListMarker` protocol. This is the **shared foundation** that the
three category plans — `docs/plan-css-lists.md`, `docs/plan-css-pseudo.md`,
`docs/plan-css-ruby.md` — each independently require. Per the "Option C"
decision it is pulled out, implemented, and landed **first**; the three
downstream plans then build on the landed API instead of each re-doing marker
infrastructure (and colliding on the same files).

This plan does **not** rebuild the counter tree (`docs/plan-css-lists.md` B1–B4
owns `CountersAttachmentContext`) and does **not** rebuild `CounterStyle`
(`docs/plan-css-lists.md` B5). It defines the marker **box model** — the box,
its placement, its baseline alignment, its style cascade, and the
unpositioned-marker carry protocol — against a *pluggable* marker-text source so
the counter/counter-style work can land independently and feed it.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before any planning or coding session — non-negotiable:
1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study
   Blink first, 0% diff required, test-execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`**
   — auto-memory index pointing at the same rules.

If you are about to type a rule verbatim, stop and link instead.

---

## Why this plan exists — the collision

Three already-written category plans each independently rework the marker
subsystem and would collide on `pkg/layout/layout_tree_builder.go`,
`pkg/layout/unpositioned_list_marker.go`, `pkg/render/render.go`, and
`pkg/render/paint_layer.go`:

- **`docs/plan-css-lists.md`** — bucket **B6** ("`::marker` box layout +
  styling cascade", ~10 fails) plus B7 (outside string markers), B8 (markers in
  vertical writing modes), B9 (inline list-item markers), B10
  (`list-style-image` markers). Its Phase 6 is titled "Layout-tree `::marker`
  box" and explicitly says "Make *every* marker (inside *and* outside) a real
  layout box".
- **`docs/plan-css-pseudo.md`** — bucket **B1** ("`::marker` content / styling
  / layout", **49 failures** — the single largest bucket in that category).
  Its Phase 2 is titled "`::marker` as a laid-out pseudo-element box" and says
  "Replace the paint-time marker hack with a real `::marker` pseudo-element box
  that is styled by the `::marker` cascade and laid out through the
  inline/fragment pipeline". Its Phase 3 (`::first-letter`) further *depends* on
  the marker being a real skippable box.
- **`docs/plan-css-ruby.md`** — its Phase 2 builds the inline-item model
  (`InlineItemOpenRubyColumn` etc.) and its B1 bucket includes
  `pseudo-first-letter` / `pseudo-first-line` ruby interaction; ruby base/`<rt>`
  layout shares `inline_layout.go` `createLineBoxEx` and the inline-item
  collection path with inside-marker layout. css-ruby does not *own* marker
  work, but it consumes the same `pkg/layout/inline_item.go` collection and
  `pkg/layout/inline_layout.go` line-box code that the inside marker flows
  through; landing the marker as a real inline item *first* removes a moving
  target from under the ruby inline-item work.

The union of what the three need from the marker subsystem **is** this
foundation's scope:

| Capability required | css-lists | css-pseudo | css-ruby |
|---|---|---|---|
| Marker is a real laid-out box (inside *and* outside) | B6 | B1 Phase 2 | — (consumes shared inline path) |
| `::marker` style cascade + marker-allowed-property filter | B6 | B1 Phase 2 | — |
| Marker text from a pluggable source (counter / string / symbol / `content`) | B5/B6 | B1 Phase 2 | — |
| Outside marker placed against content's first baseline | B6 (`list-marker-alignment`) | B1 Phase 2 | — |
| `UnpositionedListMarker` carry/claim protocol filled in | B6 | B1 Phase 2 | — |
| Marker box is skippable by `::first-letter` walk / inline collection | — | B2/B3 (`first-letter-skip-marker` etc.) | B1 (`pseudo-first-letter`) |
| Marker offsets in logical coords (writing-mode-correct) | B8 | B1 Phase 2 | — (ruby is also logical) |
| `inline list-item` marker attachment | B9 | — | — |
| `list-style-image` marker box | B10 | — | — |

This plan delivers all rows **except** the rows that are pure counter-tree /
counter-style data supply, which it consumes through a clean interface
(`MarkerTextSource`, see Phase 1). css-lists B9/B10 and the image/inline-level
*extensions* are folded in as Phases 6–7 here because they are marker-box-model
changes, not counter changes — landing them in the foundation keeps the marker
box model whole (CLAUDE.md §1: works for ALL cases).

---

## Current louis14 state (what exists, what's stubbed, what's a hack)

Established by reading the source (read-only):

### `pkg/layout/unpositioned_list_marker.go` — all-stub
`UnpositionedListMarker` exists with the right *shape* (`MarkerNode`,
`IsValid`, `ContentAlignmentBaseline`, `AddToBox`, `AddToBoxWithoutLineBoxes`,
`Layout`, `InlineOffset`) but every method is a no-op or trivial:
- `AddToBox` / `AddToBoxWithoutLineBoxes` / `Layout` are explicit no-ops with
  comments "marker placement is handled at paint time in render.go".
- `ContentAlignmentBaseline` only reads `childResult.HasBaseline`.
- `InlineOffset` returns a bare `-markerInlineSize` (no `InlineMarginsForOutside`
  logic, no symbol/disclosure/image cases).
- `LayoutInputNode.ListMarkerBlockNodeIfListItem()`
  (`pkg/layout/layout_input_node.go:175`) **always returns nil** — the comment
  states this "makes the UnpositionedListMarker protocol a no-op". So the
  protocol never engages.

### `pkg/render/render.go` — paint-time hack
- `drawListMarker` (`render.go:3450`) — paints the marker ad-hoc with magic
  numbers: `markerSize := fontSize * 0.35`, vertical offset `fontSize * 0.55`,
  `contentLeft - box.Padding.Left/2`. Branches to `drawListMarkerInside` /
  `drawListMarkerOutside`.
- `drawListMarkerOutside` (`render.go:3502`) — for `disc`/`circle`/`square`
  draws a primitive; for everything else calls `formatListMarker` and
  `DrawText` at hand-computed coordinates. Baseline is `box.Y + box.Border.Top +
  ascent` — i.e. *the list item's own border-box top + font ascent*, not the
  content's first line-box baseline. This is exactly why `list-marker-alignment`
  fails.
- `drawListMarkerInside` (`render.go:3552`) — a no-op; inside markers were
  *already* moved to a synthetic inline node (see below).
- `formatListMarker` (`render.go:3273`) / `applyCounterStyle` (`render.go:3308`)
  / `fallbackCounter` (`render.go:3423`) — paint-time counter-style switch.
  These are owned by `docs/plan-css-lists.md` B5 (`CounterStyle`); this plan
  treats them as an existing text source to be wrapped, then deleted by B5.

### `pkg/layout/layout_tree_builder.go` — inside-only synthetic node
- `createMarkerPseudoElement` (`layout_tree_builder.go:706`) — **only** creates
  a marker `LayoutInputNode` when `style.GetListStylePosition() == "inside"`
  (explicit guard at `:715`). For `outside` it returns nil and the paint-time
  path runs. So today there are *two* marker code paths: a real-ish inline node
  for `inside`, a paint hack for `outside`.
- It builds a synthetic `html.Node{TagName:"::marker"}` with a text-node child
  carrying `resolveListStyleType` / `resolveContentText` output, and stores a
  `markerStyle` (with UA `unicode-bidi:isolate` and `display:inline` forced).
- `getListItemCounterValue` (`layout_tree_builder.go:678`) — DOM-sibling
  counting hack, reads `<ol start>` but nothing else. Owned for replacement by
  `docs/plan-css-lists.md` B3; this plan consumes it behind the
  `MarkerTextSource` interface.
- `buildNode` (`layout_tree_builder.go:99` and `:137`) — computes
  `lin.MarkerStyle` / `lin.MarkerContent` for `inside` and prepends the marker
  node before `::before`.

### `pkg/render/paint_layer.go` — shortcut fields
`PaintLayer` carries `IsListItem`, `ListStyleType`, `ListStyleImage`,
`ListStylePositionInside`, `ListItemIndex`, `MarkerContent`, `MarkerColor`,
`HasMarkerColor`, `MarkerFontSize`, `HasMarkerFont` (`paint_layer.go:157-166`).
`computeListItemIndex` (`paint_layer.go:713`) is a second DOM-sibling-counting
hack. All of this exists *only* to feed `drawListMarker`.

### Net assessment
The inside marker is "half real" (a synthetic inline node), the outside marker
is fully paint-time, the two disagree on text source and on baseline, and
`UnpositionedListMarker` is a shaped-but-dead stub. The foundation must unify
both onto **one** real-box path and make `UnpositionedListMarker` actually
carry/claim.

---

## Blink reference model (study BEFORE writing code)

All paths under `third_party/blink/renderer/`. Verified against the Chromium
`main` mirror on 2026-05-14.

### `core/layout/list/list_marker.{h,cc}` — `ListMarker`
The *non-layout-object* helper bag of static + per-marker logic. It is **not** a
`LayoutObject`; it is a member (`list_marker_`) of both
`LayoutInsideListMarker` and `LayoutOutsideListMarker`.

- **`enum class ListStyleCategory { kNone, kSymbol, kLanguage, kStaticString }`**
  (`list_marker.h`). `GetListStyleCategory(Document&, const ComputedStyle&)`
  (`list_marker.cc:301-313`): no `list-style` → `kNone`;
  `list_style->IsString()` → `kStaticString`; else if
  `GetCounterStyle().IsPredefinedSymbolMarker()` → `kSymbol`; else → `kLanguage`.
- **`enum MarkerTextType { kNotText, kUnresolved, kOrdinalValue, kStatic,
  kSymbolValue }`** (private, `list_marker.h`) — the text-resolution state of
  the marker's child.
- **`String MarkerTextWithSuffix(const LayoutObject&) const`** (`:182`) and
  **`MarkerTextWithoutSuffix`** (`:188`) — call the private
  `MarkerText(format)` (`:155`). `MarkerText` dispatches on
  `ListStyleCategory`: `kNone` → empty/`kNotText`; `kStaticString` → appends
  `style.ListStyleStringValue()` (no suffix); `kSymbol` → `CounterStyle`'s
  `GenerateRepresentationWithPrefixAndSuffix(0)` / `GenerateRepresentation(0)`;
  `kLanguage` → list-item ordinal value through the same `CounterStyle` methods.
- **`void UpdateMarkerContentIfNeeded(LayoutObject&)`** (`:211-239`) — builds
  the marker's *child*: a `LayoutListMarkerImage` for an image marker, a
  `LayoutTextFragment` (anonymous-styled, initially empty) for text. Sets
  `marker_text_type_` to `kNotText` or `kUnresolved`. When `::marker { content }`
  is author-specified, the child is generated-content, not counter text.
- **`static LayoutUnit WidthOfSymbol(const ComputedStyle&, const AtomicString&)`**
  (`:240-258`): disclosure → `style.SpecifiedFontSize() * style.EffectiveZoom()
  * 0.66` (`DisclosureSymbolSize`, `:38`); other symbols →
  `(font_data->GetFontMetrics().Ascent() * 2 / 3 + 1) / 2 + 2` (`:257`).
- **`static std::pair<LayoutUnit,LayoutUnit> InlineMarginsForInside(...)`**
  (`:260-281`): image → `{0, kCMarkerPaddingPx}` (7px); disclosure → `{0,
  kClosureMarkerMarginEm * SpecifiedSize}` (0.4em); other symbol → `{-1,
  kCUAMarkerMarginEm * ComputedSize}` (1em).
- **`static std::pair<LayoutUnit,LayoutUnit> InlineMarginsForOutside(...)`**
  (`:283-305`): image → `margin_start = -marker_inline_size -
  kCMarkerPaddingPx`, `margin_end = kCMarkerPaddingPx`; disclosure →
  `margin_start = -DisclosureSymbolSize - kCMarkerPaddingPx - 1`; other symbol →
  `margin_start = -(font_metrics.Ascent() * 2 / 3) - kCMarkerPaddingPx - 1`;
  invariant `-margin_start - margin_end == marker_inline_size` (`:304`).
- **`static PhysicalRect RelativeSymbolMarkerRect(...)`** (`:315-343`):
  disclosure → `LogicalRect(0, ascent - marker_size, marker_size, marker_size)`;
  other → `LogicalRect(1, 3 * (ascent - ascent * 2 / 3) / 2, (ascent * 2/3 + 1)/2,
  (ascent * 2/3 + 1)/2)`; converted to physical via `WritingModeConverter`.
- **`static LayoutObject* MarkerFromListItem(const LayoutObject*)`** and
  **`LayoutObject* ListItem(const LayoutObject&) const`** — the marker↔list-item
  linkage.

### `core/layout/list/unpositioned_list_marker.{h,cc}` — the carry/claim protocol
A value type holding `Member<LayoutOutsideListMarker> marker_layout_object_`
(`unpositioned_list_marker.h`). `explicit operator bool()` = non-null.

- **`Layout(const ConstraintSpace& parent_space, const ComputedStyle&
  parent_style, FontBaseline)`** (`unpositioned_list_marker.cc:46-54`) — lays
  the marker node out via `marker_node.LayoutAtomicInline(...)`, requesting the
  marker's *first-line baseline* rather than the atomic-inline baseline.
- **`ContentAlignmentBaseline(const ConstraintSpace&, FontBaseline, const
  PhysicalFragment& content)`** (`:56-72`) — for a line-box child returns the
  ascent from `line_box.Metrics()`, `nullopt` if the line box is empty; for
  non-line-box content recurses to the first child's `FirstBaseline()`. "the
  list marker should be aligned to the first line box of next child".
- **`AddToBox(space, baseline_type, content, BoxStrut&, marker_layout_result,
  content_baseline, block_offset*, BoxFragmentBuilder*)`** (`:74-112`) —
  converts the marker fragment to logical coords; computes
  `content_baseline - marker_metrics.ascent`; if positive moves the marker
  down, if negative pushes the *content* down; adjusts for intruded floats via
  `ComputeIntrudedFloatOffset()`; adds the marker fragment to the builder.
- **`AddToBoxWithoutLineBoxes(space, baseline_type, marker_layout_result,
  BoxFragmentBuilder*, intrinsic_block_size*)`** (`:114-141`) — when the list
  item has no line boxes, the marker is top-aligned at block offset 0 and
  contributes to block size.
- **`InlineOffset(LayoutUnit marker_inline_size)`** (`:34-43`) — looks up the
  list item, calls `ListMarker::InlineMarginsForOutside(...)`, returns the
  *start* (first) margin.
- **The carry pattern**: an outside marker whose baseline isn't yet known is
  stored as an `UnpositionedListMarker` *on the `LayoutResult`/builder* and
  propagates up — analogous to out-of-flow propagation — until the nearest
  `LayoutListItem` (or, per `docs/findings-multicol-archive.md`, an intervening
  `ColumnLayoutAlgorithm`) *claims* it and calls `AddToBox`. Four callsites
  exist in `ColumnLayoutAlgorithm`; the list-item block algorithm is the
  primary claimant.

### `core/layout/layout_object.cc` — marker box creation
`LayoutObject::CreateObject` (`:395-403`): when `element->GetPseudoId() ==
kPseudoIdMarker`, it consults `parent->GetComputedStyle()->MarkerShouldBeInside(
*parent, style.GetDisplayStyle())` (which reads `list-style-position`) and
returns either `MakeGarbageCollected<LayoutInsideListMarker>(element)` or
`LayoutOutsideListMarker(element)`. **The marker is a real pseudo-element box in
both cases** — `inside` and `outside` differ only in the box *class*, not in
whether a box exists.

### `core/layout/list/layout_{inside,outside}_list_marker.{h,cc}`
- **`LayoutOutsideListMarker : LayoutBlockFlow`** (`layout_outside_list_marker.h`)
  — a block-flow box that lives as a *sibling* of the list item's content in
  the box tree (not truly out-of-flow — that would force anonymous-block
  generation, per the `core/layout/list/README.md`). It gets positioned when
  the parent `LayoutListItem` is laid out, via the `UnpositionedListMarker`
  propagation. Has `NeedsOccupyWholeLine()`, `IsMonolithic()`,
  `WillCollectInlines()`, and a `ListMarker& Marker()` accessor.
- **`LayoutInsideListMarker : LayoutInline`** (`layout_inside_list_marker.h`) —
  an *inline-level* box; it flows as the first in-flow inline item of the list
  item's content. Also carries a `ListMarker& Marker()`. `AddChild` enforces
  that a `content: normal` marker has at most one child.
- **`LayoutListItem : LayoutBlockFlow`** (`layout_list_item.h`) — holds a
  `ListItemOrdinal ordinal_`; `Marker()` returns
  `list_item->PseudoElementLayoutObject(kPseudoIdMarker)`;
  `UpdateMarkerTextIfNeeded()`, `UpdateCounterStyle()`, `OrdinalValueChanged()`,
  `Value()`. The marker is reached *through the pseudo-element system*, not as a
  plain DOM child.

### `core/layout/list/README.md`
Confirms the architecture: outside markers are box-tree siblings that avoid
anonymous-block generation; markers flow through `InlineLayoutAlgorithm` or
`BlockLayoutAlgorithm` depending on context; the *unpositioned* marker is set on
`LayoutResult` and propagates to the nearest `LayoutListItem`, the deferred
positioning being the central design choice.

### `core/html/list_item_ordinal.{h,cc}` — `ListItemOrdinal`
The ordinal cache on the list item element. `UseExplicitValue()` /
`SetExplicitValue()` model `<li value>` (an explicit value); `<ol start>` /
`<ol reversed>` feed `CalcValue`. **Note for scope**: the *correct* ordinal
source is `docs/plan-css-lists.md` B3 (`list-item` as a real counter in
`CountersAttachmentContext`). This foundation does not port `ListItemOrdinal`;
it consumes the ordinal through `MarkerTextSource` (Phase 1) so that B3 can
swap the implementation underneath without touching the marker box model.

### `::marker` UA style + property filter (CSS Pseudo-4 §4.4)
Blink's UA `::marker` style sets `unicode-bidi: isolate`, `text-transform:
none`, `white-space: pre`, `font-variant-numeric: tabular-nums` and the
`::marker` rule only accepts the marker-allowed property subset: `color`,
`direction`, `font-*`, `content`, `text-combine-upright`, `unicode-bidi`,
`white-space`, `letter-spacing`, `word-spacing`, `line-height`, `text-shadow`,
`text-transform`, `animation-*`, `transition-*`. `list-style-position` is read
off the **originating list-item**, never off the `::marker`.

---

## Architecture the foundation lands

A single real-box marker path replacing both the `inside` synthetic node and
the `outside` paint hack:

```
LayoutListItem (display:list-item LayoutInputNode)
  ├─ ::marker box  ← always created by createMarkerPseudoElement
  │     • LayoutInsideListMarker-equivalent  → inline-level, first in-flow item
  │     • LayoutOutsideListMarker-equivalent → block-flow sibling, carried as
  │                                            UnpositionedListMarker
  ├─ ::before
  ├─ content
  └─ ::after
```

- One new `pkg/layout/list_marker.go` mirroring Blink `core/layout/list/
  list_marker.{h,cc}`: `ListMarker` helper type — `ListStyleCategory`,
  `MarkerTextType`, marker-text generation, `WidthOfSymbol`,
  `InlineMarginsForInside/Outside`, `RelativeSymbolMarkerRect`.
- `pkg/layout/unpositioned_list_marker.go` filled in: real `AddToBox`,
  `AddToBoxWithoutLineBoxes`, `Layout`, `ContentAlignmentBaseline`,
  `InlineOffset` (using `ListMarker::InlineMarginsForOutside`).
- `createMarkerPseudoElement` produces a marker box for **both** positions.
- `ListMarkerBlockNodeIfListItem()` returns the real marker node.
- The block layout algorithm for `display:list-item` claims the carried marker
  and calls `AddToBox` against the content's first baseline.
- `drawListMarker` and the `PaintLayer` marker shortcut fields are deleted;
  markers paint as ordinary boxes/fragments.
- Marker text comes from a `MarkerTextSource` interface so the
  counter-tree / counter-style work (`docs/plan-css-lists.md` B1–B5) plugs in
  without touching the box model.

---

## Phases

Foundational-first. Each phase: goal, Blink reference, louis14 target files,
new types, approach, gate metric. Per CLAUDE.md §4 run only the 1–4 tests under
the phase plus a tiny regression sample; never the full suite during the work.

### Phase 0: Baseline & categorization — **DONE (this document)**
- Read the marker sections of `docs/plan-css-lists.md` (B6–B10),
  `docs/plan-css-pseudo.md` (B1 Phase 2, and B2/B3 marker-skip dependencies),
  `docs/plan-css-ruby.md` (Phase 2 inline-item model + `pseudo-first-*`).
- Read current louis14 marker code: `pkg/layout/unpositioned_list_marker.go`,
  `pkg/render/render.go` (`drawListMarker*`, `formatListMarker`,
  `applyCounterStyle`), `pkg/layout/layout_tree_builder.go`
  (`createMarkerPseudoElement`, `resolveListStyleType`,
  `getListItemCounterValue`, `buildNode` marker block),
  `pkg/layout/layout_input_node.go` (`IsListItem`,
  `ListMarkerBlockNodeIfListItem`, `ListMarkerOccupiesWholeLine`),
  `pkg/render/paint_layer.go` (marker fields, `computeListItemIndex`).
- Studied Blink `list_marker.{h,cc}`, `unpositioned_list_marker.{h,cc}`,
  `layout_object.cc` marker creation, `layout_{inside,outside}_list_marker.h`,
  `layout_list_item.h`, `core/layout/list/README.md`, `list_item_ordinal.h`.

---

### Phase 1 — `ListMarker` helper + `MarkerTextSource` seam (FOUNDATIONAL)
**Goal.** Port Blink's `ListMarker` non-layout-object helper as a pure-logic
type, and define the `MarkerTextSource` interface that decouples the marker
*box model* from the counter tree / counter style. After this phase the marker
text path is a clean function call; no box layout yet.

**Blink reference.**
- `core/layout/list/list_marker.h` — `enum class ListStyleCategory { kNone,
  kSymbol, kLanguage, kStaticString }`; `enum MarkerTextType { kNotText,
  kUnresolved, kOrdinalValue, kStatic, kSymbolValue }`.
- `core/layout/list/list_marker.cc` — `GetListStyleCategory` (`:301-313`),
  `MarkerText` / `MarkerTextWithSuffix` / `MarkerTextWithoutSuffix`
  (`:155-200`), `WidthOfSymbol` (`:240-258`), `InlineMarginsForInside`
  (`:260-281`), `InlineMarginsForOutside` (`:283-305`),
  `RelativeSymbolMarkerRect` (`:315-343`).
- `core/html/list_item_ordinal.h` — the ordinal-value concept that
  `MarkerTextSource` abstracts.

**louis14 target files.**
- New: `pkg/layout/list_marker.go` — mirrors Blink `core/layout/list/
  list_marker.{h,cc}` file placement (memory `feedback_blink_file_placement`).
- `pkg/layout/layout_tree_builder.go` — `getListItemCounterValue` and
  `resolveListStyleType` become the *initial concrete implementation* of
  `MarkerTextSource`; they are not deleted here (that is
  `docs/plan-css-lists.md` B3/B5), they are moved behind the interface.
- `pkg/render/render.go` — `formatListMarker` / `applyCounterStyle` /
  `fallbackCounter` stay for now but are *only* reachable through the
  `MarkerTextSource` adapter; no caller outside the adapter.

**New types.**
```go
// pkg/layout/list_marker.go
type ListStyleCategory int // CategoryNone, CategorySymbol, CategoryLanguage, CategoryStaticString
type MarkerTextType int    // MarkerNotText, MarkerUnresolved, MarkerOrdinalValue, MarkerStatic, MarkerSymbolValue

// ListMarker is the non-layout-object helper, mirroring Blink ListMarker.
type ListMarker struct{}
func GetListStyleCategory(style *css.Style) ListStyleCategory
func (ListMarker) MarkerTextWithSuffix(item *LayoutInputNode, src MarkerTextSource) (string, MarkerTextType)
func (ListMarker) MarkerTextWithoutSuffix(item *LayoutInputNode, src MarkerTextSource) (string, MarkerTextType)
func WidthOfSymbol(style *css.Style, listStyleType css.ListStyleType) layoutunit.LayoutUnit
func InlineMarginsForInside(markerStyle, itemStyle *css.Style, cat ListStyleCategory) (start, end layoutunit.LayoutUnit)
func InlineMarginsForOutside(markerStyle, itemStyle *css.Style, cat ListStyleCategory, markerInlineSize layoutunit.LayoutUnit) (start, end layoutunit.LayoutUnit)
func RelativeSymbolMarkerRect(style *css.Style, ascent layoutunit.LayoutUnit) geometry.LogicalRect

// MarkerTextSource decouples the marker box model from the counter subsystem.
// Phase-1 impl wraps getListItemCounterValue/formatListMarker; replaced by the
// CountersAttachmentContext + CounterStyle path in docs/plan-css-lists.md.
type MarkerTextSource interface {
    // OrdinalValue returns the list-item counter value for kLanguage/kSymbol
    // category markers (Blink: counter(list-item) via the attachment context).
    OrdinalValue(item *LayoutInputNode) int
    // CounterStyleText formats an ordinal through the resolved counter style,
    // with or without prefix/suffix (Blink: CounterStyle::GenerateRepresentation*).
    CounterStyleText(style *css.Style, value int, withPrefixSuffix bool) string
}
```

**Approach.** `GetListStyleCategory` mirrors `list_marker.cc:301-313` exactly:
no list-style → `CategoryNone`; `<string>` value → `CategoryStaticString`;
predefined symbol marker (`disc`/`circle`/`square`/`disclosure-*`) →
`CategorySymbol`; everything else → `CategoryLanguage`. `MarkerText*` dispatches
on category and calls `MarkerTextSource` for the `Language`/`Symbol` paths.
`WidthOfSymbol` / `InlineMargins*` / `RelativeSymbolMarkerRect` are ported
formula-for-formula with the exact constants (`kCMarkerPaddingPx` = 7,
`kClosureMarkerMarginEm` = 0.4, `kCUAMarkerMarginEm` = 1.0, disclosure factor
0.66, bullet `(ascent*2/3 + 1)/2 + 2`). All geometry in
`pkg/geometry/layoutunit` per the Phase-13 precision discipline noted in
`docs/findings-multicol-archive.md`.

**Tests fixed.** None directly — pure refactor, no behaviour change. Verified by
non-regression.

**Gate.** `pkg/layout` and `pkg/render` build clean; `go vet` clean. Spot-check
4 currently-passing list reftests (2 `css2/lists`, 2 `css-lists`) — 0% diff
unchanged. CSS2 sample (4 tests) unchanged.

---

### Phase 2 — `::marker` style cascade + always-create the marker box
**Goal.** `createMarkerPseudoElement` produces a real marker `LayoutInputNode`
for **both** `inside` and `outside`, carrying a correctly cascaded `::marker`
style (UA defaults + author `::marker` rules + marker-allowed-property filter).
No placement change yet — `outside` still paints via the old path *temporarily*
guarded — this phase isolates the style/box-creation change from the placement
change.

**Blink reference.**
- `core/layout/layout_object.cc:395-403` — `kPseudoIdMarker` always creates a
  box; `MarkerShouldBeInside` (reads `list-style-position` off the *parent*)
  only chooses the class.
- CSS Pseudo-4 §4.4 — marker-allowed property subset; UA `::marker` sets
  `unicode-bidi: isolate`, `text-transform: none`, `white-space: pre`,
  `font-variant-numeric: tabular-nums`.
- `core/layout/list/list_marker.cc:211-239` `UpdateMarkerContentIfNeeded` — when
  `::marker { content }` is author-specified the child is generated content,
  not counter text; otherwise the child is the resolved marker text.

**louis14 target files.**
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` (`:706`):
  remove the `GetListStylePosition() != "inside"` early-return (`:715`); always
  build the marker node. Add `resolveMarkerStyle(node, itemStyle)` that:
  (1) starts from UA `::marker` defaults; (2) cascades author `::marker` rules
  via `css.ComputePseudoElementStyle`; (3) applies the marker-allowed-property
  filter (drop any non-allowed declaration); (4) inherits remaining inherited
  properties from the originating list-item. The existing `buildNode` block at
  `:99-128` (which computes `lin.MarkerStyle`/`lin.MarkerContent` for `inside`
  only) is folded into `resolveMarkerStyle`.
- `pkg/layout/layout_tree_builder.go` — marker content: if `::marker` set
  `content` explicitly use it (generated content, Blink
  `UpdateMarkerContentIfNeeded` content branch); else use
  `ListMarker.MarkerTextWithSuffix` (Phase 1). Tag the marker node with its
  `ListStyleCategory` and a `markerIsOutside bool` (read off the originating
  item's `list-style-position`).
- `pkg/layout/layout_input_node.go` — extend `LayoutInputNode`: add
  `MarkerCategory ListStyleCategory`, `MarkerIsOutside bool`,
  `MarkerTextTyp MarkerTextType`. Keep `MarkerStyle` / `MarkerContent`.
  `ListMarkerBlockNodeIfListItem()` (`:175`) now returns the marker child node
  instead of nil (the protocol begins to engage in Phase 4 — until then
  callers may receive it but `UnpositionedListMarker` stays a no-op behind a
  feature guard so this phase is style-only).
- `pkg/css/cascade.go` — `ComputePseudoElementStyle` for `"marker"` must apply
  the marker-allowed-property filter and the UA `::marker` defaults
  (`unicode-bidi: isolate`, `text-transform: none`, `white-space: pre`).
  (`docs/plan-css-pseudo.md` Phase 1 also touches `ComputePseudoElementStyle`
  for the broader pseudo-ID set — coordinate: this plan adds only the
  `::marker` UA-default + filter logic; the pseudo-ID enum widening is left to
  css-pseudo Phase 1, which lands *after* this foundation.)

**New types.** `LayoutInputNode.MarkerCategory`, `.MarkerIsOutside`,
`.MarkerTextTyp`; `resolveMarkerStyle` helper; a `markerAllowedProperty(name
string) bool` predicate (CSS Pseudo-4 §4.4 list).

**Approach.** Single ownership of marker style construction in
`resolveMarkerStyle`. The marker is *always* created as a child node so the
inside/outside distinction is purely a placement decision (Phase 3/4), exactly
like Blink's `LayoutInsideListMarker` vs `LayoutOutsideListMarker` being two
classes of the *same* pseudo box. `list-style-position` is read **only** off
the originating item.

**Tests fixed.** Lands the style cascade for the css-pseudo B1 marker-styling
tests (`marker-color`, `marker-text-transform-*`, `marker-letter-spacing`,
`marker-word-spacing`, `marker-font-variant-numeric-*`,
`marker-webkit-text-fill-color`) at the *style* level; full pass requires
Phase 3 layout. Counts toward css-pseudo B1 and css-lists B6.

**Gate.** Marker `::marker` computed style for a hand-checked DOM matches Blink
(allowed-property filter applied, UA defaults present). The 4 Phase-1
regression reftests still 0%. No new failures in the css-pseudo / css-lists
baseline-passing set. CSS2 sample unchanged.

---

### Phase 3 — Inside marker as a real inline-level box
**Goal.** The `inside` marker flows through inline layout as the first in-flow
inline-level item of the list item's content, with correct
`InlineMarginsForInside` margins and baseline — replacing the current
synthetic-node approximation with the `ListMarker`-driven box.

**Blink reference.**
- `core/layout/list/layout_inside_list_marker.h` — `LayoutInsideListMarker :
  LayoutInline`, inline-level; `ListMarker& Marker()`; `AddChild` enforces a
  single child for `content: normal`.
- `core/layout/list/list_marker.cc:260-281` `InlineMarginsForInside` — symbol
  `{-1, 1em}`, disclosure `{0, 0.4em}`, image `{0, 7px}`.
- `core/layout/list/README.md` — inside marker processed by
  `InlineLayoutAlgorithm` as an ordinary inline item.

**louis14 target files.**
- `pkg/layout/inline_item.go` — the inline-item collection
  (`collectInlinesRecursive`) already accepts the synthetic `::marker` inline
  node as a child; switch it to consume the Phase-2 marker box. Apply
  `InlineMarginsForInside` as the marker item's inline margins.
- `pkg/layout/inline_layout.go` — `createLineBoxEx` places the marker item
  first on the first line; its baseline participates in line metrics like any
  inline box. The Phase-1a comment at `inline_layout.go:139-142` (already says
  the inside marker is "a proper layout node child") is now *actually* true via
  the real box.
- `pkg/layout/layout_input_node.go` — `ListMarkerOccupiesWholeLine()` (`:183`)
  stays correct for `inside`.
- `pkg/render/render.go` — `drawListMarkerInside` (`:3552`) is already a no-op;
  delete it and its call site in `drawListMarker` (`:3495`).

**New types.** None — reuses the inline-item path.

**Approach.** The inside marker is just an inline-level box. Because Phase 2
made it a real cascaded box, every text property
(`text-transform`/`letter-spacing`/`word-spacing`/`font-variant-numeric`/
`text-shadow`/`line-height`/`unicode-bidi`) "just works" through normal inline
layout — this is the css-pseudo B1 thesis. `InlineMarginsForInside` is applied
as the box's margins so the marker-to-content gap is spec-correct, not the old
`markerSize*0.5` magic number.

**Tests fixed.** css-pseudo B1 inside-marker tests:
`marker-list-style-position` (the inside cases), `marker-text-transform-*`,
`marker-letter-spacing`, `marker-word-spacing`, `marker-line-height`,
`marker-unicode-bidi-default`, `marker-text-shadow` and siblings — the subset
whose `-ref` uses `list-style-position: inside`. Also the css-lists
`list-style-type-string-001b..007` *inside* variants if any.

**Gate.** `marker-list-style-position`, `marker-text-transform-uppercase`,
`marker-letter-spacing` at 0% diff (the inside-position cases). The Phase-1/2
regression set still 0%. css-ruby baseline unchanged (shared `inline_item.go` /
`inline_layout.go` — spot-check 3 ruby tests). CSS2 sample unchanged.

---

### Phase 4 — Outside marker box + `UnpositionedListMarker` carry/claim protocol (FOUNDATIONAL CORE)
**Goal.** The `outside` marker is a real block-flow-equivalent sibling box,
laid out and carried as an `UnpositionedListMarker`, then *claimed* by the list
item's block layout algorithm and positioned against the content's **first
content baseline** — replacing the paint-time `drawListMarkerOutside` hack and
its wrong "border-box top + ascent" baseline.

**Blink reference.**
- `core/layout/list/unpositioned_list_marker.cc` — `Layout` (`:46-54`),
  `ContentAlignmentBaseline` (`:56-72`), `AddToBox` (`:74-112`),
  `AddToBoxWithoutLineBoxes` (`:114-141`), `InlineOffset` (`:34-43`).
- `core/layout/list/layout_outside_list_marker.h` —
  `LayoutOutsideListMarker : LayoutBlockFlow`, box-tree sibling, positioned by
  the parent list item; `NeedsOccupyWholeLine()`.
- `core/layout/list/list_marker.cc:283-305` `InlineMarginsForOutside` — the
  negative-start-margin formulas, invariant `-margin_start - margin_end ==
  marker_inline_size`.
- `core/layout/list/README.md` — the unpositioned marker is set on
  `LayoutResult` and propagates to the nearest `LayoutListItem`; deferred
  positioning is the central design choice.
- `docs/findings-multicol-archive.md` "UnpositionedListMarker protocol" — the
  four `ColumnLayoutAlgorithm` callsites confirm the carry/claim shape louis14
  already half-models.

**louis14 target files.**
- `pkg/layout/unpositioned_list_marker.go` — fill in the stubs:
  - `Layout(ctx, space)` — lay the marker node out (atomic-inline style layout),
    request its first-line baseline.
  - `ContentAlignmentBaseline(child, childResult)` — for a line-box child return
    the line-box ascent; for a block child recurse to first baseline; `false`
    when none (mirrors `:56-72`).
  - `AddToBox(child, childResult, contentBaseline, blockOffset, builder)` —
    compute `contentBaseline - markerAscent`; if positive offset the marker
    down, if negative push content down; apply `InlineOffset` for the inline
    position; add the marker fragment as a child of the list-item box fragment
    (mirrors `:74-112`).
  - `AddToBoxWithoutLineBoxes` — top-align the marker at block offset 0, add its
    block-size contribution (mirrors `:114-141`).
  - `InlineOffset(markerInlineSize)` — replace the bare `-markerInlineSize` with
    `ListMarker.InlineMarginsForOutside(...).start` (Phase 1).
- `pkg/layout/layout_input_node.go` — `ListMarkerBlockNodeIfListItem()` returns
  the outside marker node (already wired in Phase 2 to return non-nil); add a
  guard so `inside` markers are *not* returned here (they go through the inline
  path, Phase 3).
- `pkg/layout/block_layout.go` — the `display:list-item` block layout path:
  (1) at the start, build the `UnpositionedListMarker` from
  `ListMarkerBlockNodeIfListItem()`; (2) after laying out the first in-flow
  child, call `ContentAlignmentBaseline` and, if a baseline is found,
  `AddToBox`; (3) if the list item produced no line boxes, call
  `AddToBoxWithoutLineBoxes`; (4) propagate an unclaimed marker on the
  `LayoutResult` for an ancestor to claim (the multicol callsites in
  `docs/findings-multicol-archive.md` already expect this — verify they still
  compile and engage).
- `pkg/render/render.go` — delete `drawListMarkerOutside` (`:3502`) and the
  `drawListMarker` dispatcher (`:3450`); delete the `r.drawListMarker(layer)`
  call at `render.go:1357`. Outside markers now paint as ordinary box
  fragments.
- `pkg/render/paint_layer.go` — remove the `IsListItem` marker-paint setup
  block (`:535-568`) and the `MarkerContent`/`MarkerColor`/`MarkerFontSize`/
  `HasMarkerColor`/`HasMarkerFont`/`ListItemIndex`/`ListStylePositionInside`/
  `ListStyleImage`/`ListStyleType` shortcut fields (`:157-166`); remove
  `computeListItemIndex` (`:713`). Markers are real boxes now.

**New types.** None new — fills in the existing `UnpositionedListMarker` shape;
may add an `UnpositionedListMarker` field to `LayoutResult` /
`BoxFragmentBuilder` if not already present (check — the multicol archive
implies it exists at least conceptually).

**Approach.** This is the architectural pivot. The outside marker stops being a
paint afterthought and becomes a box whose position is *computed*: laid out for
size, carried unpositioned, then placed by the claimant against a *real*
baseline. `InlineOffset` + `InlineMarginsForOutside` give the correct negative
inline offset into the marker margin. The carry/claim shape is exactly what
louis14 already stubbed and what multicol already references — this phase makes
the stub real.

**Tests fixed.** css-lists B6 `list-marker-alignment` (the headline baseline
test), `marker-content-001`, `marker-counter`, `nested-marker`,
`nested-marker-styling`, `list-marker-symbol-bidi`; css-pseudo B1 outside-marker
tests (`marker-list-style-position` outside cases, `marker-text-align-*`,
`marker-and-other-pseudo-elements`). The single largest correctness gain.

**Gate.** `list-marker-alignment`, `marker-content-001`, `nested-marker` at 0%
diff. All Phase-1/2/3 gains still 0%. css-ruby baseline unchanged (3-test
sample). multicol sample unchanged (the `UnpositionedListMarker` callsites — run
2 multicol list-item tests). CSS2 sample unchanged.

---

### Phase 5 — Marker geometry in vertical writing modes
**Goal.** The Phase-4 marker box positions correctly under
`writing-mode: vertical-rl` / `vertical-lr` / `sideways-*`, because all marker
offsets are computed in **logical** coordinates and converted once at the end.

**Blink reference.**
- `core/layout/list/list_marker.cc:315-343` `RelativeSymbolMarkerRect` — builds
  a `LogicalRect` and converts via `WritingModeConverter` to physical.
- `core/layout/list/unpositioned_list_marker.cc` — `InlineOffset` /
  `ContentAlignmentBaseline` / `AddToBox` all operate on the *logical* inline
  and block axes; writing-mode correctness is by construction.

**louis14 target files.**
- `pkg/layout/list_marker.go` — `RelativeSymbolMarkerRect` returns a
  `geometry.LogicalRect`; conversion to physical happens at the paint boundary
  via `pkg/layout/writing_mode_converter.go`, not inside the formula.
- `pkg/layout/unpositioned_list_marker.go` — confirm `InlineOffset` and the
  `AddToBox` offset math are in logical coords (inline axis = the list item's
  logical inline axis); the physical conversion is the converter's job.
- `pkg/layout/block_layout.go` — the claimant applies `InlineOffset` on the
  *logical* inline axis of the list item.

**New types.** None.

**Approach.** Nothing writing-mode-specific is hand-coded; the marker is
writing-mode-correct because every offset is logical and the existing
`WritingModeConverter` (Phase-13 precision work,
`docs/findings-multicol-archive.md`) does the physical mapping. The per-`::marker`
`font-size` override is already correct since the marker is a real box with its
own style (Phase 2).

**Tests fixed.** css-lists B8: `list-style-type-decimal-vertical-lr`,
`list-style-type-decimal-vertical-rl`.

**Gate.** Both B8 tests at 0% diff. All prior gains 0%. css-writing-modes
sample unchanged (run 3 wm list-related tests; do **not** run the full wm
suite — CLAUDE.md §4). CSS2 sample unchanged.

---

### Phase 6 — Inline-level list-item markers (`display: inline list-item`)
**Goal.** A marker box attaches correctly to an *inline-level* list item, so
`display: inline list-item` and lists inside inline-block render their markers.

**Blink reference.**
- CSS Display L3 — `display: inline list-item` is an inline-level box with the
  `list-item` inner display type.
- `core/layout/list/layout_inline_list_item.{h,cc}` — `LayoutInlineListItem`;
  the marker (inside or outside) attaches the same way, `InlineMarginsForInside/
  Outside` apply unchanged.

**louis14 target files.**
- `pkg/css/style.go` — `parseDisplay` / `GetDisplay`: parse the two-value
  `inline list-item` form; surface it as an `IsListItem()` flag orthogonal to
  inline/block level (so the marker generation in `createMarkerPseudoElement`
  triggers on the flag, not on `DisplayListItem` alone).
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` triggers on
  the `IsListItem()` flag; an inline list item gets the same marker box.
- `pkg/layout/inline_layout.go` — place the marker for an inline-level list
  item: an `inside` marker is the first inline item of the inline list item's
  content; an `outside` marker is carried/claimed by the inline list item's box.

**New types.** A `css.Style.IsListItemDisplay() bool` (or equivalent) covering
both `list-item` and `inline list-item`.

**Approach.** The marker box model from Phases 2–4 is level-agnostic; this phase
only widens the *trigger* for marker generation and confirms the claimant works
when the list item is inline-level. No new marker geometry.

**Tests fixed.** css-lists B9: `inline-list`, `inline-list-marker`,
`inline-block-list`, `inline-block-list-marker`,
`inline-list-with-table-child`.

**Gate.** All 5 B9 tests at 0% diff. All prior gains 0%. CSS2 sample unchanged.

---

### Phase 7 — `list-style-image` marker box
**Goal.** Image markers (raster *and* gradient images) are a real marker box —
a replaced/inline-block-sized box whose content is the `list-style-image`, with
`InlineMarginsFor{Inside,Outside}` image-case margins, falling back to
`list-style-type` on load failure.

**Blink reference.**
- `core/layout/list/list_marker.cc:211-239` `UpdateMarkerContentIfNeeded` — for
  `style->GeneratesMarkerImage()` it creates a `LayoutListMarkerImage` child.
- `core/layout/list/layout_list_marker_image.{h,cc}` — the image marker box.
- `core/layout/list/list_marker.cc` — `InlineMarginsForOutside` image case
  `{-marker_inline_size - 7, 7}`, `InlineMarginsForInside` image `{0, 7}`.
- `list-style-image: none` falls back to `list-style-type`.

**louis14 target files.**
- New: `pkg/layout/layout_list_marker_image.go` — mirrors Blink
  `core/layout/list/layout_list_marker_image.{h,cc}`; an image marker box sized
  from the resolved `list-style-image` (raster via `pkg/images`, gradient via
  `pkg/css/gradient.go`).
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement`: when
  `list-style-image` resolves to an image, build the image marker box; on load
  failure fall through to the `list-style-type` text/symbol path.
- `pkg/layout/list_marker.go` — `GetListStyleCategory` already returns
  `CategoryNone` for an image (the image is not a counter style); the image
  margin cases go in `InlineMarginsFor{Inside,Outside}`.
- `pkg/render/render.go` — the `ListStyleImage` branch inside the (now deleted)
  `drawListMarker` is gone; image markers paint as ordinary replaced boxes.

**New types.** A marker-image box type in `pkg/layout/layout_list_marker_image.go`.

**Approach.** The image marker is just a replaced-content marker box; it reuses
the Phase-4 carry/claim for `outside` and the Phase-3 inline path for `inside`.
Gradient images route through the existing `pkg/css/gradient.go`.

**Tests fixed.** css-lists B10: `list-style-image-gradients`,
`list-style-image-gradients-dynamic` (verify it is not `flags=dom`;
auto-skipped if so), `list-type-none-style-image`.

**Gate.** B10 tests at 0% diff. All prior gains 0%. CSS2 sample unchanged.

---

### Phase 8 — Delivery & cross-plan handoff
**Goal.** Confirm the foundation is whole and hand off to the three downstream
plans.

- Re-run the marker-relevant subset across the three categories (sanctioned
  end-of-foundation run, scoped to marker tests only — *not* the full
  categories): css-lists B6–B10 marker tests, css-pseudo B1 marker tests, the
  css-ruby `pseudo-first-*` tests. Expect every marker-box test that does *not*
  depend on the counter tree / counter style to pass at 0% diff.
- Re-run CSS2 (expect 99/99) and a wm/multicol/ruby spot-check (markers touch
  line layout, the block algorithm, and the multicol carry callsites).
- Confirm the `MarkerTextSource` seam is clean: `getListItemCounterValue` /
  `formatListMarker` / `applyCounterStyle` are reachable *only* through the
  Phase-1 adapter, so `docs/plan-css-lists.md` B1–B5 can replace them without
  touching the box model.
- Update the three downstream plans per the **Downstream impact** section
  below (trim the buckets/phases this foundation replaced).
- Final report: which marker tests now pass, the `MarkerTextSource` interface
  the downstream plans consume, and the trimmed scope of each downstream plan.

**Gate.** All marker-box tests that are counter-independent pass at 0% diff.
CSS2 99/99. No regression in the wm / multicol / ruby spot-check samples.

---

## Downstream impact — what each plan can drop or simplify

Once this foundation lands, the three category plans are trimmed as follows.
Each entry says precisely which of *their* buckets/phases this foundation
**replaces** (delete entirely) or **simplifies** (reduce to a thin
counter/data change on top of the landed box model).

### `docs/plan-css-lists.md`
- **Phase 6 (B6 — "Layout-tree `::marker` box", ~10 fails) — REPLACED.** This
  foundation *is* css-lists Phase 6: the real marker box (inside + outside), the
  `::marker` style cascade with `counter-*`/`quotes`, baseline alignment, and
  pseudo-element list-item markers all land here. css-lists Phase 6 collapses to
  "verify B6 tests pass on the landed foundation; wire `counter-*`/`quotes`
  declared *on* `::marker` into the counter tree" — a counter-tree task, not a
  box task.
- **Phase 7 (B7 — outside `<string>` markers, 8 fails) — REPLACED.** The
  `CategoryStaticString` path and the outside-string margins are part of this
  foundation's `ListMarker` (Phase 1) + outside box (Phase 4). css-lists Phase 7
  is deleted.
- **Phase 8 (B8 — markers in vertical writing modes, 2 fails) — REPLACED** by
  this foundation's Phase 5.
- **Phase 9 (B9 — `inline list-item` markers, 5 fails) — REPLACED** by this
  foundation's Phase 6.
- **Phase 10 (B10 — `list-style-image` markers, 3 fails) — REPLACED** by this
  foundation's Phase 7.
- **Phases 1–5 (B1–B5, the counter tree + `CounterStyle`) — UNCHANGED but
  SIMPLIFIED at the seam.** They keep their full scope (`CountersAttachmentContext`,
  `CounterStyle`) but now have a defined consumer: they implement
  `MarkerTextSource` (replacing the Phase-1 adapter). css-lists Phase 3's
  "replace `getListItemCounterValue`" and Phase 5's "delete `formatListMarker`/
  `applyCounterStyle`" become "swap the `MarkerTextSource` implementation" —
  a localized change behind the interface, no marker-box edits.
- **Net:** css-lists drops Phases 7–10 outright, collapses Phase 6 to a
  counter-wiring verification, and keeps Phases 1–5 with a clean handoff seam.
  ~28 of its 100 failures (B6–B10) are addressed by the foundation.

### `docs/plan-css-pseudo.md`
- **Phase 2 (B1 — "`::marker` as a laid-out pseudo-element box", 49 fails — the
  category's largest bucket) — REPLACED.** This foundation delivers exactly
  what css-pseudo Phase 2 specifies: the paint-time `drawListMarker` hack is
  deleted, the marker is a real cascaded pseudo box flowing through inline
  layout (so `text-transform`/`letter-spacing`/`word-spacing`/
  `font-variant-numeric`/`text-shadow`/`line-height`/`unicode-bidi`/
  `list-style-position` all "just work"), `UnpositionedListMarker` is filled
  in, and the `PaintLayer` marker shortcut fields are removed. css-pseudo
  Phase 2 collapses to "verify the 49 B1 tests pass on the landed foundation"
  plus any `::marker`-content edge cases tied to css-pseudo Phase 1's pseudo-ID
  parser widening.
- **Phase 3 (B2 — `::first-letter`) — SIMPLIFIED.** css-pseudo Phase 3 lists
  `first-letter-skip-marker`, `first-letter-exclude-inline-marker`,
  `first-letter-exclude-inline-child-marker`,
  `first-letter-exclude-block-child-marker` — all require the
  `::first-letter` walk to *skip the marker box*. With the marker now a real,
  identifiable `LayoutInputNode` (tagged `MarkerCategory` / via
  `ListMarkerBlockNodeIfListItem`), the skip is a clean `if isMarkerNode`
  check instead of reasoning about a paint-time hack. css-pseudo Phase 3 keeps
  its scope but the marker-skip sub-task becomes trivial.
- **Phase 4 (B3 — `::first-line`) — SIMPLIFIED.** `first-line-and-marker` needs
  the marker excluded from / correctly interacting with the first-line box;
  a real marker box makes this a normal inline-item question.
- **Phase 1 (pseudo-element parser + cascade backbone) — COORDINATE.** css-pseudo
  Phase 1 widens `ComputePseudoElementStyle` to the full pseudo-ID set. This
  foundation's Phase 2 adds *only* the `::marker` UA-default + marker-allowed-
  property-filter logic to that function. Land order: foundation first, then
  css-pseudo Phase 1 widens around it — no conflict, but css-pseudo Phase 1
  should note the `::marker` branch is already populated.
- **Net:** css-pseudo drops its entire Phase 2 (49 failures — its largest
  bucket) to a verification pass, and the marker-skip sub-tasks in Phases 3–4
  become trivial. ~49+ of its 103 in-scope failures are addressed or unblocked.

### `docs/plan-css-ruby.md`
- **No bucket REPLACED** — css-ruby does not own marker work. But two
  SIMPLIFICATIONS:
- **Phase 2 (ruby-column inline-item model) — SIMPLIFIED / DE-RISKED.** css-ruby
  Phase 2 adds `InlineItemOpenRubyColumn` etc. to `pkg/layout/inline_item.go`
  and reworks `pkg/layout/inline_layout.go` `createLineBoxEx`. The inside
  marker *also* flows through `inline_item.go` collection and
  `inline_layout.go` line-box layout. Landing the marker as a real, settled
  inline item *before* css-ruby Phase 2 means the ruby inline-item work builds
  on a stable inline path instead of a half-migrated one — removing a moving
  target and a merge-collision surface on those two files.
- **B1 bucket (`pseudo-first-letter`, `pseudo-first-line`) — SIMPLIFIED.** These
  ruby tests exercise pseudo-element interaction with ruby; the marker being a
  real, skippable box (same property css-pseudo Phase 3 relies on) means ruby's
  pseudo interaction is reasoning about real boxes, not a paint hack.
- **Net:** css-ruby keeps all 15 phases but Phase 2 lands on a stable
  `inline_item.go` / `inline_layout.go` baseline, and the foundation should be
  landed *before* css-ruby Phase 2 begins to avoid concurrent edits to those
  two shared files.

### Recommended land order
1. **`docs/plan-marker-foundation.md`** (this plan) — lands first, on `master`
   via a `fix/*` branch.
2. **`docs/plan-css-lists.md` Phases 1–5** — the counter tree + `CounterStyle`,
   implementing `MarkerTextSource`. Can proceed in parallel with css-pseudo once
   the foundation is landed.
3. **`docs/plan-css-pseudo.md`** — Phase 1 (parser) then the trimmed Phase 2
   (verification) and Phases 3–6.
4. **`docs/plan-css-ruby.md`** — after the foundation, so ruby Phase 2 builds on
   the settled inline path.

---

## Key Blink files this plan is grounded in
- `core/layout/list/list_marker.{h,cc}` — `ListMarker`, `ListStyleCategory`
  (`kNone/kSymbol/kLanguage/kStaticString`), `MarkerTextType`,
  `MarkerText`/`MarkerTextWithSuffix`/`MarkerTextWithoutSuffix`,
  `GetListStyleCategory` (`:301-313`), `WidthOfSymbol` (`:240-258`),
  `InlineMarginsForInside` (`:260-281`), `InlineMarginsForOutside` (`:283-305`),
  `UpdateMarkerContentIfNeeded` (`:211-239`), `RelativeSymbolMarkerRect`
  (`:315-343`).
- `core/layout/list/unpositioned_list_marker.{h,cc}` —
  `UnpositionedListMarker`, `Layout` (`:46-54`), `ContentAlignmentBaseline`
  (`:56-72`), `AddToBox` (`:74-112`), `AddToBoxWithoutLineBoxes` (`:114-141`),
  `InlineOffset` (`:34-43`), the carry/claim propagation.
- `core/layout/layout_object.cc:395-403` — `kPseudoIdMarker` always creates a
  box; `MarkerShouldBeInside` chooses the class.
- `core/layout/list/layout_inside_list_marker.{h,cc}` —
  `LayoutInsideListMarker : LayoutInline` (inline-level marker box).
- `core/layout/list/layout_outside_list_marker.{h,cc}` —
  `LayoutOutsideListMarker : LayoutBlockFlow` (box-tree-sibling marker box).
- `core/layout/list/layout_list_item.{h,cc}` — `LayoutListItem`, `Marker()` via
  `kPseudoIdMarker`, `UpdateMarkerTextIfNeeded`, `OrdinalValueChanged`.
- `core/layout/list/layout_inline_list_item.{h,cc}` — `LayoutInlineListItem`
  (inline-level list item).
- `core/layout/list/layout_list_marker_image.{h,cc}` — the image marker box.
- `core/layout/list/README.md` — the inside/outside architecture and deferred
  marker positioning.
- `core/html/list_item_ordinal.{h,cc}` — `ListItemOrdinal` (the ordinal source
  abstracted behind `MarkerTextSource`; its real port is css-lists B3).
- CSS Pseudo-4 §4.4 — the `::marker` UA defaults and marker-allowed property
  subset.

## Notes
- Test command template:
  `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/<category>/<name>' -v`
- Pre-rendered diff artifacts: `output/reftests/<name>_{diff,ref,test}.png`.
- All geometry in `pkg/geometry/layoutunit` per the Phase-13 precision
  discipline (`docs/findings-multicol-archive.md`).
- Worktree agents: symlink `fonts/` from the main dir before any broad run
  (memory `feedback_worktree_fonts`); `go fmt` before and after Go edits
  (memory `feedback_gofmt_after_edits`); commit + report at each phase
  milestone (memory `feedback_agent_checkpoints`).
- Per memory `feedback_blink_file_placement`: `list_marker.go` and
  `layout_list_marker_image.go` go in `pkg/layout/` mirroring
  `core/layout/list/`; `unpositioned_list_marker.go` already lives there.
