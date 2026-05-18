# Task Plan: Pass the css-lists WPT reftest category

## Goal
Take `TestWPTCSS3Reftests/css-lists` from **44 passing / 100 failing / 11 skipped** (155 run)
toward 100%, by replacing louis14's ad-hoc counter/marker machinery with a
Blink-grounded counter scope tree, a real `@counter-style`/`CounterStyle` text
generator, and a layout-tree list-marker box. No point fixes — every phase
fixes a whole CSS Lists subsystem.

## Blink vetting log

**Vetted against Chromium `main` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f** on 2026-05-18.

### Citations verified
- `core/css/counters_attachment_context.h` (`CountersAttachmentContext`, `CounterEntry`, `CounterStack`, `CounterInheritanceTable`, `enum class Type`) — ✓ unchanged
- `core/css/counters_attachment_context.cc` (`ProcessCounter`, `CreateCounter`, `UpdateCounterValue`, `RemoveStaleCounters`, `RemoveCounterIfAncestorExists`, `EnterObject`, `LeaveObject`, `GetCounterValues`, `MaybeCreateListItemCounter`, `ElementGeneratesListItemCounter`, `CalculateInitialValueForReversed`) — ✓ unchanged
- `core/css/counter_style.h/.cc` (`CounterStyle`, `CounterStyleSystem`, `GenerateRepresentation`, `GenerateRepresentationWithPrefixAndSuffix`, `RangeContains`, `ResolveExtends`, `ResolveFallback`, `IsPredefinedSymbolMarker`) — ✓ unchanged; default `suffix_ = ". "`, default `negative_prefix_ = "-"`.
- `core/layout/list/list_marker.h/.cc` (`ListMarker`, `MarkerTextWithSuffix` / `MarkerTextWithoutSuffix` driven by `kWithPrefixSuffix` / `kWithoutPrefixSuffix`, `GetListStyleCategory` → `{kNone, kSymbol, kLanguage, kStaticString}`, `UpdateMarkerContentIfNeeded`, `WidthOfSymbol`, `InlineMarginsForOutside`, `InlineMarginsForInside`, `RelativeSymbolMarkerRect`, `GetCounterStyle`) — ✓ unchanged. `LayoutOutsideListMarker` / `LayoutInsideListMarker` both still present.
- `core/layout/list/unpositioned_list_marker.h/.cc` (`UnpositionedListMarker`, `ContentAlignmentBaseline`, `AddToBox`, `AddToBoxWithoutLineBoxes`, `Layout`, `InlineOffset`) — ✓ unchanged.
- `core/html/html_olist_element.h` (`HTMLOListElement::InitialCounter()` returns `int64_t`; `IsReversed()`) — ✓ unchanged.
- `core/html/list_item_ordinal.h` (`ListItemOrdinal::Get`, `ExplicitValue`, `IsInReversedOrderedList`) — ✓ unchanged.

### Citations updated
- `list_marker.cc` (`OrdinalValue` / `ListItemForMarker`) → at this SHA the equivalent code path is `ListMarker::MarkerText` consuming `CountersAttachmentContext::GetCounterValues("list-item", only_last=true)`, with `ListMarker::ListItem(marker)` for the marker→list-item parent lookup. `OrdinalValueChanged` exists as a notification hook but not as a value getter. Plan Phase 3 reworded to reference the actual symbols.
- `counters_attachment_context.cc` `ProcessCounter` precedence — plan Phase 2 wording ("set runs after reset and increment per spec ordering — encode the order in `EnterObject`") replaced. Actual Blink model: type stored as **bitmask** (`Type { kIncrementType = 1<<0, kResetType = 1<<1, kSetType = 1<<2 }`); `DetermineCounterTypeAndValue` combines bits; `ProcessCounter` early-exits to `CreateCounter` when the reset bit is set; otherwise `CalculateCounterValue` uses `IsSetOrReset(type) → return counter_value`, else `current + counter_value`. Net per-element precedence: reset > set > increment.

### Citations broken / missing in current Blink
- none.

### Citations added
- Phase 1 §types now notes `CounterInheritanceTable` is `HashMap<AtomicString, CounterStack*>` (pointer-to-stack) and that `enum class Type` is a **bitmask** (`1<<0`, `1<<1`, `1<<2`).
- Phase 5 §predefined-symbols cites the cached flag form `IsPredefinedSymbolMarker() const { return is_predefined_symbol_marker_; }` (set via `SetIsPredefinedSymbolMarker()`), rather than name-time matching against `disc`/`circle`/`square`/`disclosure-open`/`disclosure-closed`.
- Phase 5 §`CounterStyleSystem` notes the enum also contains `kSimpChineseInformal`, `kSimpChineseFormal`, `kTradChineseInformal`, `kTradChineseFormal`, `kKoreanHangulFormal`, `kKoreanHanjaInformal`, `kKoreanHanjaFormal`, `kLowerArmenian`, `kUpperArmenian`, `kEthiopicNumeric` between the listed common systems and `kUnresolvedExtends` — louis14 will need stubs for at least the enum members even if the algorithms are scoped to a later phase.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before any planning or coding session:
1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study
   Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`**
   — auto-memory index pointing at the same rules.

If you are about to type a rule verbatim, stop and link instead.

---

## Baseline analysis (2026-05-14)

Failing list: `grep "FAIL: TestWPTCSS3Reftests/css-lists/" docs/reftest-survey-2026-05-14-raw.txt`
(100 entries). Pre-rendered diffs under `output/reftests/<name>_{diff,ref,test}.png`.

### Root cause: there is no counter tree

louis14's counter mechanism is `LayoutTreeBuilder.counters map[string][]int`
(`pkg/layout/layout_tree_builder.go:22`). It is mutated inline during the
`buildNode` DOM recursion:

- `processCounterReset` (`layout_tree_builder.go:1200`) just **appends** a value
  to the named stack — it never pops, never tracks the originating element, and
  never removes stale sibling-subtree counters. So a `counter-reset` on
  `span:first-child` leaks into every following sibling (`counters-001`,
  `counters-scope-001`).
- `processCounterIncrement` (`:1228`) mutates `stack[len-1]` with no notion of
  which scope owns it.
- `getCounterValue` (`:1260`) returns the top of the flat stack — wrong whenever
  scopes nest or siblings reset.
- `getListItemCounterValue` (`:678`) is an **independent** DOM-sibling-counting
  hack that does not feed `b.counters`, so `counter(list-item)` in `content:`
  always reads `0` (see `counter-list-item_test.png`: every generated `:before`
  shows `0`).
- `counters()` is never resolved: `ParseContentValues` (`pkg/css/style.go:5410`)
  lists `"counters"` as a recognised function name but the `switch funcName`
  block at `:5436` has **no `case "counters"`** — the value is silently
  dropped. `counters-001_test.png` and `counter-set-001_test.png` render blank.
- `counter-set` is not parsed or processed at all.
- `display:contents` subtrees are skipped for counter processing, so
  `counter-increment`/`-reset`/`-set` on their children is lost
  (`counter-increment-inside-display-contents_test.png` shows `6`, ref `7`).
- `reversed()` counters and `<ol reversed>` / `<li value>` / `<ol start>`
  feeding the `list-item` counter are not implemented as counters at all.

This is one subsystem and must be fixed as one: **port Blink's
`CountersAttachmentContext`** (`third_party/blink/renderer/core/css/
counters_attachment_context.{h,cc}`, added 2024). It owns a
`CounterInheritanceTable` (name → stack of `CounterEntry{layout_object, value}`)
and is threaded through a single pre-order traversal with `EnterObject` /
`LeaveObject`. The `list-item` counter is *the same machinery* — Blink's
`MaybeCreateListItemCounter` / `ElementGeneratesListItemCounter` create an
implicit `list-item` counter for `ol/ul/li/menu/dir`, seeded from
`HTMLOListElement::InitialCounter()` (`start`/`reversed`) and
`ListItemOrdinal` (`li[value]`). Marker text then just reads
`counter(list-item)` from the table.

### Root cause: there is no layout-tree marker box

`UnpositionedListMarker` (`pkg/layout/unpositioned_list_marker.go`) is an
all-stub no-op; the comment admits "the current list-marker path is paint-time
only." Markers are painted ad-hoc in `Renderer.drawListMarker`
(`pkg/render/render.go:3450`) with magic numbers (`markerSize := fontSize*0.35`,
vertical offset `fontSize*0.55`). For `list-style-position:inside` a *synthetic
inline `::marker` node* is injected in `createMarkerPseudoElement`
(`layout_tree_builder.go:706`) but for `outside` it is paint-time only. This
fails baseline alignment, vertical writing modes (`list-style-type-decimal-
vertical-rl_test.png` — markers scattered), outside string markers
(`list-style-type-string-002` — overlapping), and inline-block/inline list
items. Blink lays *every* marker out as a real box: `LayoutOutsideListMarker` /
`LayoutInsideListMarker` driven by `ListMarker`
(`core/layout/list/list_marker.{h,cc}`), positioned via
`UnpositionedListMarker` against the content's first baseline.

### Root cause: `@counter-style` is a shallow string transform

`pkg/css/counter_format.go` has only `ToRoman/ToAlpha/ToGreek` with decimal
fallback for out-of-range. `applyCounterStyle` in `render.go:3308` is a
paint-time switch. There is no `CounterStyle` object, no `range`, no `pad`, no
`negative`, no `extends`, no predefined-styles table, and the predefined
`list-style-type` algorithms (`decimal-leading-zero`, the alphabetic/numeric
systems) are not spec-correct. Blink: `core/css/counter_style.{h,cc}`
(`CounterStyle`, `CounterStyleSystem` enum, `GenerateRepresentation`,
`GenerateRepresentationWithPrefixAndSuffix`, `RangeContains`, `ResolveExtends`,
`ResolveFallback`) plus the UA `@counter-style` definitions.

---

## Bucket breakdown (100 failures)

| # | Bucket | Fails | Core defect |
|---|--------|-------|-------------|
| **B1** | Counter scope/nesting tree (`counter-001..003`, `counters-001..003`, `counters-005..006`, `counters-scope-001..004`, `counter-invalid`, `counter-important`-adjacent) | ~14 | Flat `map[string][]int`; no scope push/pop, no stale-sibling removal, no `counters()` join. Phase 1 (2026-05-18) closed 10/10 core scope-tree tests at 0% diff. |
| **B1-deferred** | Tests originally bucketed under B1 that B1's work does NOT fix (verified by Phase 1 closure): `counter-004`, `counters-004` need georgian counter style → see B5; `counter-slot-order`, `counter-slot-order-scoping`, `counter-list-item-slot-order` need Shadow DOM `<slot>` flattening → out of plan scope (separate ticket: "Shadow DOM slot rendering for static reftests"). Counter VALUES (1,2,3) in slot tests are correct; only DOM-flatten order is wrong. | 5 | Mis-bucketed; see B5 + new ticket |
| **B2** | `counter-set` + overflow/underflow + display-none/contents (`counter-set-001/002`, `counter-reset-increment-overflow-underflow`, `counter-reset-increment-set-display-{none,contents}`, `counter-{increment,reset}-inside-display-contents`) | ~7 | `counter-set` unimplemented; `display:contents` subtree skipped; no i32 saturation |
| **B3** | `list-item` counter as a real counter — `<ol start>`, `<li value>`, implicit/explicit (`counter-list-item`, `counter-list-item-2/3`, `counter-list-item-slot-order`, `li-list-item-counter-003`, `implicit-and-explicit-list-item-counters`, `list-item-definition`, `li-insert-child`, `details-open`, `li-value-counter-reset-002`) | ~10 | `list-item` not in counter table; `counter(list-item)` reads 0 |
| **B4** | `reversed()` counters + `<ol reversed>` (`counter-reset-reversed-*` ×15, `foo-counter-reversed-006/008*` ×7, `li-value-reversed-*` ×12) | ~34 | `reversed()` syntax + `CalculateInitialValueForReversed` not implemented |
| **B5** | `@counter-style` / `CounterStyle` + predefined `list-style-type` algorithms (`list-style-type-decimal-line-height`, decimal/alphabetic correctness across markers, `calc-in-counter`-adjacent, `counter-004`, `counters-004` — both need `georgian`, reclassified from B1 post Phase 1) | ~6 + cross-cutting | No `CounterStyle` object, no range/pad/negative/extends |
| **B6** | `::marker` box layout + styling cascade (`marker-content-001`, `marker-counter`, `marker-quotes`, `marker-dynamic-content-change`, `marker-webkit-text-fill-color`, `nested-marker`, `nested-marker-styling`, `nested-marker-dynamic`, `list-marker-alignment`, `list-marker-symbol-bidi`) | ~10 | Outside marker is paint-time only; no real marker box; `counter-*`/`quotes` on `::marker` ignored; pseudo-element list-items don't generate markers |
| **B7** | `list-style-type: <string>` outside markers (`list-style-type-string-001b..007`) | ~8 | Outside string marker box not laid out; overlaps content |
| **B8** | Marker layout in vertical writing modes (`list-style-type-decimal-vertical-{lr,rl}`) | 2 | Paint-time marker placement ignores writing mode |
| **B9** | `inline list-item` / inline-block list (`inline-list`, `inline-list-marker`, `inline-block-list`, `inline-block-list-marker`, `inline-list-with-table-child`) | ~5 | `display:inline list-item` not recognised; marker not attached to inline-level box |
| **B10** | `list-style-image` (`list-style-image-gradients`, `list-style-image-gradients-dynamic`, `list-type-none-style-image`) | ~3 | Image marker not a real box; gradient images unsupported as markers |

Buckets are ordered by dependency: **B1→B2→B3→B4** are the counter tree (must be
one mechanism); **B5** is the text generator; **B6→B10** are marker box layout
which consumes B1–B5. `details-open` and `*-dynamic*` tests carry `flags=dom` or
script; verify against `hasWPTFlag` in `reftest_runner_test.go:67` — those that
are `flags=dom` are auto-skipped and should not be counted.

---

## Phases

### Phase 0: Baseline & categorization — **DONE (this document)**
Failing list captured; diffs read for `counters-001`, `counter-set-001`,
`counter-list-item`, `counter-increment-inside-display-contents`,
`nested-marker`, `list-style-type-string-002`, `list-style-type-decimal-
vertical-rl`, `list-marker-alignment`. Buckets above.

---

### Phase 1 — Counter scope tree (B1)
**Goal.** Replace the flat `map[string][]int` with a Blink-faithful counter
inheritance table threaded through one pre-order traversal. Fix scoping,
nesting, sibling isolation, and `counters()` resolution.

**Blink reference.**
- `core/css/counters_attachment_context.h` — `CounterEntry{Member<const
  LayoutObject> layout_object; int value;}`; `CounterStack` =
  `HeapVector<Member<CounterEntry>>`; `CounterInheritanceTable` =
  `HashMap<AtomicString, CounterStack*>` (pointer-to-stack);
  `enum class Type { kIncrementType = 1<<0, kResetType = 1<<1, kSetType = 1<<2 }`
  — used as a **bitmask** so a single element can carry multiple directive kinds for
  the same counter name (see Phase 2 for the resolution model); methods
  `EnterObject`, `LeaveObject`, `GetCounterValues`, `ProcessCounter`,
  `CreateCounter`, `UpdateCounterValue`, `RemoveStaleCounters`,
  `RemoveCounterIfAncestorExists`, `ShallowClone`/`DeepClone`.
- `counters_attachment_context.cc`:
  - `ProcessCounter` → `RemoveStaleCounters` first, then `CreateCounter`
    (reset) or `UpdateCounterValue` (increment/set).
  - `CreateCounter` (reset) **pushes a new `CounterEntry` unconditionally**;
    also removes the innermost counter if its originating element *is*
    `layout_object` or a *previous sibling* of it (so two resets on the same
    element, or a reset following a sibling reset, do not stack).
  - `UpdateCounterValue` (increment/set) mutates the **topmost** entry; if the
    stack is empty or ends in a containment boundary, it `CreateCounter`s with
    the implicit instantiation value (0).
  - `RemoveStaleCounters` pops every entry whose originating element's
    `LayoutParentElement` is **not** an ancestor of the element being entered
    (`IsAncestorOf`) — this is what isolates sibling subtrees.
  - `RemoveCounterIfAncestorExists` runs in `LeaveObject` for reset counters:
    if the previous stack entry is an ancestor of the one being left, drop the
    left one (ancestor counters always win for inheritance).
  - `GetCounterValues` walks the stack **in reverse**: `counter()` returns the
    innermost value; `counters()` returns *all* values from outermost in-scope
    entry down, joined by the separator. Stops at containment-boundary
    `nullptr` entries.

**louis14 target files.**
- New: `pkg/css/counters_attachment_context.go` — mirror Blink's file
  placement (`core/css`). Types: `CounterEntry`, `counterStack`,
  `CounterInheritanceTable`, `CountersAttachmentContext`.
- `pkg/layout/layout_tree_builder.go` — delete `counters map[string][]int`
  (`:22`), `processCounterReset` (`:1200`), `processCounterIncrement`
  (`:1228`), `getCounterValue` (`:1260`), `getListItemCounterValue` (`:678`).
  Thread a `*css.CountersAttachmentContext` through `buildNode`; call
  `EnterObject` at the top of `buildNode` (after style resolution, before
  children) and `LeaveObject` after children. Pseudo-elements
  (`createPseudoElement`, `createMarkerPseudoElement`) call `EnterObject` /
  `LeaveObject` on their synthetic nodes in pseudo order
  (marker, before, children, after).
- `pkg/css/style.go` — `ParseContentValues` (`:5410`): add `case "counters"`
  that captures `name`, `separator`, optional `style`; extend `ContentValue`
  with `Separator` and `Style` fields (currently only `Type`/`Value`).
  `resolveContentText` (`layout_tree_builder.go:650`) and `createPseudoElement`
  (`:610`) call `ctx.GetCounterValues(name)` and join with the separator.
- `pkg/css/style.go` — parse `counter-reset` value list with the optional
  `reversed(name)` wrapper preserved for Phase 4 (store a parsed struct, not a
  raw string).

**New types.**
```go
// pkg/css/counters_attachment_context.go
type CounterEntry struct { Origin *html.Node; Value int }
type CountersAttachmentContext struct {
    table map[string][]*CounterEntry // name → stack, innermost last
    rootIsDocumentElement bool
}
type counterDirective struct { Name string; Value int; Kind counterKind } // reset/increment/set
```

**Approach.** Single ownership: the builder holds one
`CountersAttachmentContext`; `EnterObject(node, style)` parses the node's
`counter-reset`/`-increment`/`-set` into `counterDirective`s, runs
`RemoveStaleCounters(node)`, then `ProcessCounter` for each. `GetCounterValues`
is the sole reader for both `counter()`/`counters()` content and marker text.
Originating-element ancestry uses `html.Node.Parent` chains.

**Tests fixed.** B1: `counter-001..004`, `counters-001..006`,
`counters-scope-001..004`, `counter-slot-order`, `counter-slot-order-scoping`,
`counter-list-item-slot-order`, `counter-invalid` (≈19).

**Gate.** B1 bucket 0% diff. CSS2 99/99 unchanged. No regression in any
currently-passing css-lists test (re-run only the ~19 B1 tests + the 44
baseline-passing list/counter tests).

---

### Phase 2 — `counter-set`, display:contents/none, overflow (B2)
**Goal.** Add `counter-set`; make counters flow through `display:contents`;
skip `display:none` subtrees correctly; saturate at int32 bounds.

**Blink reference.**
- `counters_attachment_context.cc` `ProcessCounter` resolves the type **bitmask**
  built by `DetermineCounterTypeAndValue` (it ORs `kIncrementType`/`kResetType`/
  `kSetType` together when a single element declares multiple kinds for the same
  counter name). If `IsReset(counter_type)` is true it early-exits to
  `CreateCounter` (so reset wins); otherwise `UpdateCounterValue` runs
  `CalculateCounterValue(type, value, current)`, which is `counter_value` for
  `IsSetOrReset(type)` and `current + counter_value` for plain increment. Net
  per-element precedence is therefore **reset > set > increment** for the same
  counter, rather than a sequential pass-ordered model. Instantiation value for a
  bare `counter-set: n` is 0 (the `current` argument passed to
  `CalculateCounterValue` when the stack is empty).
- Counters are attached during *layout-tree* building, and `display:contents`
  elements **do** participate (they have no box but their `EnterObject`/
  `LeaveObject` still run — Blink walks the flat tree). `display:none` elements
  are not in the layout tree at all, so their counter directives never run.
- Counter values are plain `int`; CSS Lists §overflow says counters saturate at
  the `int32` range — Blink uses `base::ClampedNumeric`/`base::CheckAdd` (the
  `CalculateCounterValue` add path is `base::CheckAdd(current, value).ValueOrDefault(current)`).

**louis14 target files.**
- `pkg/css/counters_attachment_context.go` — `ProcessCounter` `kSetType` branch;
  saturating add/assign helpers (clamp to `math.MinInt32`/`MaxInt32`).
- `pkg/layout/layout_tree_builder.go` — `buildNode`: when
  `style.GetDisplay() == css.DisplayContents`, **still** call `EnterObject`/
  `LeaveObject` and recurse into children (do not early-return); confirm
  `display:none` children are filtered *before* `EnterObject` so their
  directives are skipped.
- `pkg/css/style.go` — parse the `counter-set` property. Precedence vs
  `counter-reset`/`-increment` on the same element: store all three kinds for a
  given counter name as a bitmask on the parsed `counterDirective`; resolve in
  `ProcessCounter` exactly like Blink (reset bit ⇒ `CreateCounter`; else
  set/reset assigns via `CalculateCounterValue` returning the directive value,
  increment adds with int32 saturation). Do **not** thread three sequential
  passes through `EnterObject`.

**Tests fixed.** `counter-set-001`, `counter-set-002`,
`counter-reset-increment-overflow-underflow`,
`counter-reset-increment-set-display-contents`,
`counter-reset-increment-set-display-none`,
`counter-reset-inside-display-contents`,
`counter-increment-inside-display-contents` (7).

**Gate.** B2 0% diff; B1 still 0%. CSS2 99/99.

---

### Phase 3 — `list-item` as a real counter (B3)
**Goal.** Make `list-item` an ordinary counter in the same table, auto-created
for list elements, seeded from `<ol start>`, `<li value>`, and reachable by
`counter(list-item)` / `counters(list-item)`.

**Blink reference.**
- `counters_attachment_context.cc` — `ElementGeneratesListItemCounter(Element)`
  returns true for `ol/ul/li/menu/dir` (and `display:list-item` elements);
  `MaybeCreateListItemCounter(Element)` is called from `EnterObject`. It seeds
  the implicit `list-item` counter using `ListItemOrdinal` (which reads
  `li[value]` — an explicit value acts like `counter-set: list-item N` on that
  item) and `HTMLOListElement::InitialCounter()` (reads `start`/`reversed`).
- An `<li>` with no explicit `value` does an implicit `counter-increment:
  list-item 1`. An explicit `value` is an implicit `counter-set: list-item N`.
- `list_marker.cc` `ListMarker::MarkerText` calls
  `CountersAttachmentContext::GetCounterValues("list-item", /*only_last=*/true)`
  on the layout object for the list item (looked up via
  `ListMarker::ListItem(marker)`), then formats the resulting `int` with the
  item's `CounterStyle::GenerateRepresentationWithPrefixAndSuffix`. (Note:
  `OrdinalValueChanged` exists as a notification hook but is not a value
  getter — the actual read goes through the attachment context as above.)

**louis14 target files.**
- `pkg/css/counters_attachment_context.go` — add
  `ElementGeneratesListItemCounter`, `MaybeCreateListItemCounter`; the
  `EnterObject` path consults `<ol start>`, `<ol reversed>` (placeholder for
  Phase 4), `<li value>`.
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` (`:706`)
  and `resolveListStyleType` (`:816`): replace `getListItemCounterValue` calls
  with `ctx.GetCounterValues("list-item")`. Delete `getListItemCounterValue`
  (already removed in Phase 1) — ensure no callers remain.
- `pkg/render/paint_layer.go` — `computeListItemIndex` (`:713`) becomes a thin
  read of the counter value stored on the box (set during layout-tree build);
  remove the DOM-sibling-counting body.
- `pkg/layout/types.go` / `layout_input_node.go` — carry the resolved
  `list-item` counter value on the box so paint and the marker box agree.

**Tests fixed.** `counter-list-item`, `counter-list-item-2`,
`counter-list-item-3`, `li-list-item-counter-003`,
`implicit-and-explicit-list-item-counters`, `list-item-definition`,
`li-insert-child`, `li-value-counter-reset-002`, plus contributes to B6/B9
marker numbering (10+).

**Gate.** B3 0% diff; B1+B2 still 0%. CSS2 99/99.

---

### Phase 4 — `reversed()` counters and `<ol reversed>` (B4)
**Goal.** Implement the `reversed(name)` `counter-reset` syntax and the
reversed `list-item` initial-value algorithm.

**Blink reference.**
- `counters_attachment_context.cc` `CalculateInitialValueForReversed(...)` —
  implements <https://drafts.csswg.org/css-lists/#instantiating-counters>:
  for a reversed counter, traverse the DOM **forward** within the parent scope,
  accumulating the negation of every `counter-increment` and every
  `counter-set` of that counter among in-scope siblings; **break at a sibling
  `counter-reset`** of the same counter so later siblings aren't counted. The
  result is the reset's initial value.
- A plain `<li>` under `<ol reversed>` increments `list-item` by **−1** (not
  +1); `<ol reversed>` resets `list-item` with `reversed(list-item)` and its
  initial value comes from `CalculateInitialValueForReversed` (count of items),
  offset by `start` if present.
- `CSSListCounterAccountingEnabled` gates the spec-correct path — louis14
  should implement the modern path unconditionally.

**louis14 target files.**
- `pkg/css/style.go` — `counter-reset` parser: recognise `reversed(<name>)` and
  store `Reversed bool` on the parsed `counterDirective`.
- `pkg/css/counters_attachment_context.go` — `CalculateInitialValueForReversed`;
  `CreateCounter` for a reversed entry computes its seed via that function;
  reversed `list-item` items increment by −1.
- `pkg/layout/layout_tree_builder.go` — `MaybeCreateListItemCounter` honours
  `<ol reversed>`; the implicit per-`<li>` increment sign follows the reversed
  flag of the in-scope `list-item` counter.

**Tests fixed.** `counter-reset-reversed-*` (15), `foo-counter-reversed-006/008*`
(7), `li-value-reversed-*` (12) — ≈34, the single largest bucket.

**Gate.** B4 0% diff; B1–B3 still 0%. CSS2 99/99. (This phase alone is ~⅓ of the
category — split into sub-batches: 4a `counter-reset-reversed-*`,
4b `li-value-reversed-*`, 4c `foo-counter-reversed-*`, gating each.)

---

### Phase 5 — `CounterStyle` and predefined `list-style-type` (B5)
**Goal.** Replace `counter_format.go` + `applyCounterStyle` with a real
`CounterStyle` object: spec-correct numbering systems, `range`, `pad`,
`negative`, `prefix`/`suffix`, `fallback`, `extends`, and a predefined-styles
table.

**Blink reference.**
- `core/css/counter_style.h` — `class CORE_EXPORT CounterStyle final : public
  GarbageCollected<CounterStyle>`. `enum class CounterStyleSystem { kCyclic,
  kFixed, kSymbolic, kAlphabetic, kNumeric, kAdditive, kHebrew,
  kSimpChineseInformal, kSimpChineseFormal, kTradChineseInformal,
  kTradChineseFormal, kKoreanHangulFormal, kKoreanHanjaInformal,
  kKoreanHanjaFormal, kLowerArmenian, kUpperArmenian, kEthiopicNumeric,
  kUnresolvedExtends }` — louis14 must declare the full set even if some
  algorithms are stubbed early (decimal/alphabetic/roman/hebrew/greek cover most
  css-lists tests; CJK/Armenian/Ethiopic can be `kUnresolvedExtends`-style stubs
  until cross-cutting tests need them). Fields `symbols_`, `additive_weights_`,
  `prefix_`/`suffix_` (default `". "`), `negative_prefix_` (default `"-"`)/
  `negative_suffix_`, `pad_symbol_`/`pad_length_`, `first_symbol_value_`
  (`wtf_size_t`, default 1), `range_` (`Vector<std::pair<int,int>>`),
  `fallback_style_`/`extended_style_` (`Member<CounterStyle>`). The
  `disc`/`circle`/`square`/`disclosure-open`/`disclosure-closed` predefined
  styles are identified by a **cached flag**, not a name lookup:
  `bool IsPredefinedSymbolMarker() const { return is_predefined_symbol_marker_; }`
  set at registry-build time via `SetIsPredefinedSymbolMarker()`.
- `counter_style.cc` — `GenerateRepresentation(int)` →
  `RangeContains` check (else `GenerateFallbackRepresentation` with cycle
  detection via an `is_in_fallback_` re-entrancy flag) →
  `GenerateInitialRepresentation` (per-system algorithm) → pad → negative sign.
  `GenerateRepresentationWithPrefixAndSuffix` wraps with prefix/suffix.
  `ResolveExtends`/`ResolveFallback` link names.
- UA `@counter-style` table: `decimal`, `decimal-leading-zero`,
  `lower/upper-roman`, `lower/upper-alpha`/`-latin`, `lower-greek`,
  `disc`/`circle`/`square`/`disclosure-open`/`disclosure-closed` (the last five
  carry the `is_predefined_symbol_marker_` flag).

**louis14 target files.**
- New: `pkg/css/counter_style.go` — `CounterStyle` struct, `CounterStyleSystem`
  enum, `GenerateRepresentation`, `GenerateRepresentationWithPrefixAndSuffix`,
  `RangeContains`, fallback/extends resolution. Mirrors Blink `core/css`.
- New: `pkg/css/counter_style_predefined.go` — the UA predefined table.
- `pkg/css/stylesheet.go` — `parseCounterStyleRule` (`:2127`) already produces a
  `CounterStyleRule`; extend it to parse `range`, `pad`, `negative`, and feed a
  `CounterStyle` registry. Resolve `system: extends` against the registry.
- `pkg/css/counter_format.go` — keep `ToRoman`/`ToAlpha`/`ToGreek` only as
  internal helpers used by `CounterStyle` system algorithms, or fold into
  `counter_style.go` and delete the file.
- `pkg/render/render.go` — delete `formatListMarker` (`:3273`),
  `applyCounterStyle` (`:3308`), `fallbackCounter` (`:3423`); marker text now
  comes from `CounterStyle.GenerateRepresentationWithPrefixAndSuffix`.

**New types.** `CounterStyle`, `CounterStyleSystem`, `counterRange{lower,upper}`,
`CounterStyleRegistry`.

**Tests fixed.** `list-style-type-decimal-line-height`, `calc-in-counter`-
adjacent, and correctness fixes that unblock B6/B7/B8 marker text. Several
currently-passing tests depend on this not regressing.

**Gate.** B5 tests 0% diff; B1–B4 still 0%. CSS2 99/99. Spot-check the 44
baseline-passing list tests for marker-text regressions.

---

### Phase 6 — Layout-tree `::marker` box (B6)
**Goal.** Make *every* marker (inside *and* outside) a real layout box, with the
`::marker` style cascade (including `counter-*` and `quotes` on `::marker`),
correct baseline alignment, and pseudo-element list-items generating their own
markers.

**Blink reference.**
- `core/layout/list/list_marker.{h,cc}` — `ListMarker`; `MarkerText`
  (`kWithPrefixSuffix` vs `kWithoutPrefixSuffix`); `GetListStyleCategory` →
  `{kNone, kStaticString, kSymbol, kLanguage}` (string ⇒ static; predefined
  symbol marker ⇒ symbol; other counter style ⇒ language);
  `UpdateMarkerContentIfNeeded` builds the marker's child text/image;
  `WidthOfSymbol`: bullets `(ascent*2/3 + 1)/2 + 2`, disclosure
  `font_size*zoom*0.66`; `InlineMarginsForOutside` symbol case
  `{-offset-7-1, offset+7+1-marker_inline_size}`, `InlineMarginsForInside`
  symbol `{-1, 1em}` / disclosure `{0, 0.4em}`, image `{0,7}`.
- `core/layout/list/unpositioned_list_marker.{h,cc}` — `UnpositionedListMarker`
  (already stubbed in louis14): `ContentAlignmentBaseline`, `AddToBox`,
  `AddToBoxWithoutLineBoxes`, `Layout`, `InlineOffset`. The outside marker is
  laid out as an `UnpositionedListMarker` and placed against the list item's
  **first content baseline** (`list-marker-alignment`).
- `LayoutOutsideListMarker` / `LayoutInsideListMarker` are the two box types.
- `core/css/...` — `::marker` is a real pseudo-element whose style cascades;
  `counter-increment`/`-set`/`-reset` and `quotes` declared on `::marker` apply
  and are read by `MarkerText`. Markers generated by `::before`/`::after`
  `display:list-item` are *not* addressable by a bare `::marker` selector
  (`nested-marker`).

**louis14 target files.**
- `pkg/layout/unpositioned_list_marker.go` — fill in the stubbed `AddToBox`,
  `AddToBoxWithoutLineBoxes`, `Layout`, `ContentAlignmentBaseline` with the real
  algorithm; add a `ListMarker`-equivalent helper for `MarkerText` /
  `GetListStyleCategory` / `WidthOfSymbol` / inline-margins.
- New: `pkg/layout/list_marker.go` — mirror Blink `core/layout/list`. The
  `ListMarker` type: category classification, marker-text generation via
  `css.CounterStyle`, symbol width, inside/outside margins.
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` (`:706`):
  generate a marker box for **both** inside and outside (not just inside);
  apply the `::marker` style cascade; run `EnterObject`/`LeaveObject` for
  `counter-*` declared on `::marker`; thread `quotes` from `::marker` style.
  Also generate markers for `::before`/`::after` whose `display:list-item`.
- `pkg/layout/block_layout.go` / `inline_layout.go` — wire the
  `UnpositionedListMarker` protocol so the outside marker is positioned against
  the first line-box baseline (Blink `AddToBox`).
- `pkg/render/render.go` — delete `drawListMarker`/`drawListMarkerOutside`/
  `drawListMarkerInside` (`:3450`–`:3552`); markers now paint as ordinary boxes.
- `pkg/render/paint_layer.go` — remove the `IsListItem` marker-paint branch
  (`:534`–`:568`); keep only what's needed for box painting.

**Tests fixed.** `marker-content-001`, `marker-counter`, `marker-quotes`,
`marker-webkit-text-fill-color`, `nested-marker`, `nested-marker-styling`,
`nested-marker-dynamic`, `marker-dynamic-content-change`,
`list-marker-alignment`, `list-marker-symbol-bidi` (≈10). Also stabilises B3
marker numbering.

**Gate.** B6 0% diff; B1–B5 still 0%. CSS2 99/99. wm suite spot-check
(marker box touches line layout) — re-run a small wm sample, do **not** run the
full wm suite.

---

### Phase 7 — `list-style-type: <string>` outside markers (B7)
**Goal.** Outside string markers laid out as a real box (Blink `kStaticString`
category), not paint-time.

**Blink reference.** `list_marker.cc` `GetListStyleCategory` → `kStaticString`
when `list_style->IsString()`; `MarkerText` returns the string verbatim (no
suffix); the marker box gets `InlineMarginsForOutside` non-normal case
`{-marker_inline_size, 0}`. The css-lists ref pattern (see
`list-style-type-string-002-ref.html`) is a `direction:rtl; white-space:pre`
inline-block — louis14's box must match that geometry.

**louis14 target files.**
- `pkg/layout/list_marker.go` — `kStaticString` path in `MarkerText` /
  `GetListStyleCategory`; correct outside margins for string markers.
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement` already has
  a string branch (`:760`); route it through the Phase 6 marker box for the
  `outside` case instead of the inside-only synthetic node.

**Tests fixed.** `list-style-type-string-001b`, `-002`, `-003`, `-004`,
`-005a`, `-005b`, `-006`, `-007` (8).

**Gate.** B7 0% diff; B1–B6 still 0%. CSS2 99/99.

---

### Phase 8 — Markers in vertical writing modes (B8)
**Goal.** The Phase 6 marker box positions correctly under `writing-mode:
vertical-rl/-lr` and respects per-`::marker` `font-size`.

**Blink reference.** `list_marker.cc` `RelativeSymbolMarkerRect` uses
`WritingModeConverter` to map logical marker geometry to physical space;
`UnpositionedListMarker::InlineOffset` works in logical coords so it is
writing-mode-agnostic by construction. The per-`::marker` `font-size` override
is just the marker box's own style — already correct once the marker is a real
box (Phase 6).

**louis14 target files.**
- `pkg/layout/unpositioned_list_marker.go` / `list_marker.go` — ensure marker
  offsets are computed in logical coordinates and converted via
  `pkg/layout/writing_mode_converter.go` at the end, not in physical space.
- `pkg/layout/block_layout.go` — confirm the marker's `InlineOffset` is applied
  on the logical inline axis of the list item.

**Tests fixed.** `list-style-type-decimal-vertical-lr`,
`list-style-type-decimal-vertical-rl` (2).

**Gate.** B8 0% diff; B1–B7 still 0%. CSS2 99/99 · wm sample unchanged.

---

### Phase 9 — `inline list-item` and inline-block lists (B9)
**Goal.** Recognise `display: inline list-item`; attach the marker box to an
inline-level list item; lists inside inline-block.

**Blink reference.** CSS Display: `display: inline list-item` is an
inline-level box with the `list-item` inner; Blink's `ListMarker` attaches to
the inline list item the same way; `InlineMarginsForInside/Outside` apply.

**louis14 target files.**
- `pkg/css/style.go` — `parseDisplay` / `GetDisplay` (`:4507`): parse the
  two-value `inline list-item` form; add an `inline-list-item` display value or
  an `IsListItem()` flag orthogonal to inline/block level.
- `pkg/layout/layout_tree_builder.go` — generate the marker box for
  inline-level list items too.
- `pkg/layout/inline_layout.go` — place the marker for an inline list item.

**Tests fixed.** `inline-list`, `inline-list-marker`, `inline-block-list`,
`inline-block-list-marker`, `inline-list-with-table-child` (5).

**Gate.** B9 0% diff; B1–B8 still 0%. CSS2 99/99.

---

### Phase 10 — `list-style-image` (B10)
**Goal.** Image markers (including gradient images) as a real marker box.

**Blink reference.** `list_marker.cc` `UpdateMarkerContentIfNeeded` creates a
`LayoutListMarkerImage` for `style->GeneratesMarkerImage()`;
`InlineMarginsForOutside` image case `{-marker_inline_size - 7, 7}`,
`InlineMarginsForInside` `{0, 7}`. `list-style-image: none` falls back to
`list-style-type`.

**louis14 target files.**
- `pkg/layout/list_marker.go` — image-marker box: a replaced/inline-block-sized
  box whose content is the `list-style-image` (raster or `pkg/css/gradient.go`
  gradient).
- `pkg/layout/layout_tree_builder.go` — `createMarkerPseudoElement`: when
  `list-style-image` resolves, build an image marker box; on load failure fall
  back to `list-style-type`.
- `pkg/render/render.go` — remove the `ListStyleImage` branch in the deleted
  `drawListMarker`; image markers paint as ordinary replaced boxes.

**Tests fixed.** `list-style-image-gradients`,
`list-style-image-gradients-dynamic`, `list-type-none-style-image` (3).

**Gate.** B10 0% diff; B1–B9 still 0%. CSS2 99/99.

---

## Final gate
- All non-`flags=dom` css-lists tests 0% diff (target ~144/144 of the runnable
  set; `flags=dom`/script tests stay skipped via `hasWPTFlag`).
- CSS2 99/99 unchanged.
- wm / multicol / flex / position spot-checks unchanged (marker box and counter
  context touch line layout and the layout-tree builder; do **not** run full
  suites — per CLAUDE.md §4, sample only).

## Key Blink files this plan is grounded in
- `core/css/counters_attachment_context.{h,cc}` — `CountersAttachmentContext`,
  `CounterEntry`, `CounterInheritanceTable`, `ProcessCounter`,
  `RemoveStaleCounters`, `GetCounterValues`, `MaybeCreateListItemCounter`,
  `ElementGeneratesListItemCounter`, `CalculateInitialValueForReversed`.
- `core/css/counter_style.{h,cc}` — `CounterStyle`, `CounterStyleSystem`,
  `GenerateRepresentation`, `GenerateRepresentationWithPrefixAndSuffix`,
  `RangeContains`, `ResolveExtends`/`ResolveFallback`.
- `core/layout/list/list_marker.{h,cc}` — `ListMarker`, `MarkerText`,
  `GetListStyleCategory` (`kNone/kStaticString/kSymbol/kLanguage`),
  `WidthOfSymbol`, `InlineMarginsForInside/Outside`,
  `UpdateMarkerContentIfNeeded`, `OrdinalValue`.
- `core/layout/list/unpositioned_list_marker.{h,cc}` —
  `UnpositionedListMarker`, `ContentAlignmentBaseline`, `AddToBox`, `Layout`,
  `InlineOffset`.
- `core/html/HTMLOListElement` (`InitialCounter`) and `ListItemOrdinal`
  (`li[value]`) — `<ol start/reversed>` and `<li value>` counter seeding.
