# Plan: CSS `direction` and Bidirectional Text in the Inline Pipeline

## Background and Scope

The louis14 inline pipeline is already natively dir-aware for **writing-mode** (vertical
vs horizontal). Layout works in logical coordinates throughout with a single
logical-to-physical conversion in `FragmentBuilder.Build()`. There is no post-processing
transform code.

What is **missing** is support for CSS `direction: rtl` and bidirectional text (the Unicode
Bidirectional Algorithm, UAX#9). This plan addresses that gap.

### What This Plan Covers

1. `text-align: start/end` resolving correctly for RTL
2. Paragraph-level base direction from CSS `direction`
3. Unicode Bidirectional Algorithm (UAX#9) for mixed-direction text
4. Visual reordering of inline items within a line
5. `unicode-bidi` property support (embed, isolate, bidi-override)
6. RTL-aware text rendering

### What This Plan Does NOT Cover

- Writing-mode vertical/horizontal switching (already works)
- Logical-to-physical coordinate conversion (already works)
- Sideways text rendering (separate gap, see wm-strategy.md)

---

## Architecture: How Blink Does It

Blink's LayoutNG processes bidirectional text in three phases. Our implementation should
mirror this architecture because it is proven correct and our existing code already uses
Blink's data structures.

### Blink Phase 1: BidiParagraph (pre-layout)

After `CollectInlines()` produces `InlineItemsData`, Blink runs a `BidiParagraph` pass
that:
1. Determines the paragraph's base embedding level from CSS `direction` (P2/P3 rules)
2. Walks the text content applying UAX#9 rules to resolve each character's embedding level
3. Stores the resolved `BidiLevel` on each `InlineItem`

This happens once per block container, not per line.

### Blink Phase 2: Line Breaking (logical order)

The `LineBreaker` processes items in **logical (source) order**. This is identical to what
we already do. Line breaking does not need to know about bidi — it operates on logical
runs and produces `InlineItemResult` entries with measured inline sizes.

### Blink Phase 3: Visual Reordering (per line, in PlaceItems)

After the `LineBreaker` produces a line's `InlineItemResult` list, Blink reorders the
results into **visual order** (left-to-right on screen) using `ubidi_reorderVisual()`.
Then it positions items sequentially from line-left to line-right.

Key detail: inline positions are computed AFTER reordering, not before. The line breaker
measures sizes but does not assign positions.

This is already how our code works — the line breaker fills `line.Results` with sizes,
and `createLineBox()` assigns positions by walking `line.Results`. So we just need to
insert a reordering step between `NextLine()` and `createLineBox()`.

---

## Implementation Plan

### Phase 1: text-align direction awareness (standalone, no bidi dependency)

**Files:** `pkg/layout/inline_layout.go`

This is the simplest fix and has immediate impact on direction tests.

**Step 1a:** Add a `wdm WritingDirectionMode` parameter to `computeTextAlignOffset()`:

```go
func computeTextAlignOffset(line *LineInfo, availableInline float64, wdm WritingDirectionMode) float64 {
```

**Step 1b:** Update the switch cases for "start" and "end":

```go
case "start":
    if wdm.IsRTL() {
        return slack  // RTL start = physical right
    }
    return 0
case "end":
    if wdm.IsRTL() {
        return 0      // RTL end = physical left
    }
    return slack
```

Note: "left" and "right" remain physical regardless of direction (CSS Text §7.1).
"center" is direction-independent.

**Step 1c:** Update the call site in `createLineBox()` (line 293) to pass `wdm`.

**Step 1d:** Update the default case. Currently `"left", "start", ""` are grouped.
After this change, "start" gets its own case. The default should be `"left", ""` which
always returns 0.

**Testing:** Run writing-modes tests with `direction:rtl` + `text-align:start` patterns.
Many tests that previously mis-aligned should now pass.

---

### Phase 2: Paragraph base direction

**Files:** `pkg/layout/inline_layout.go`, `pkg/layout/line_breaker.go`

**Step 2a:** In `layoutInlineChildren()`, determine the paragraph's base direction from
the container's CSS `direction` property. This is already available via `wdm.Dir`.

Currently `LineInfo.BaseDirection` exists (line_breaker.go:67) but is never set. Set it
from `wdm.Dir` when constructing the line space or in `finishLine()`:

```go
line.BaseDirection = wdm.Dir
```

**Step 2b:** Thread this through to `computeTextAlignOffset()` as well, since the default
text-align for a paragraph should match its base direction.

**Testing:** Pure RTL paragraphs (no mixed-direction text) should now align correctly.

---

### Phase 3: UAX#9 Bidi Level Resolution

**Files:** New file `pkg/layout/bidi.go`, modify `pkg/layout/inline_item.go`

This phase adds the Unicode Bidirectional Algorithm. We already have `golang.org/x/text`
(v0.22.0) as a dependency, which includes `golang.org/x/text/unicode/bidi`.

**Step 3a:** Create `pkg/layout/bidi.go` with a function:

```go
// ResolveBidiLevels computes the resolved bidi embedding level for each
// InlineItem in the items data, based on the paragraph's base direction
// and the Unicode Bidirectional Algorithm (UAX#9).
//
// This mirrors Blink's BidiParagraph pass.
func ResolveBidiLevels(itemsData *InlineItemsData, baseDir Direction)
```

**Step 3b:** The function should:
1. Extract the full `TextContent` string from `InlineItemsData`
2. Create a `bidi.Paragraph` from `golang.org/x/text/unicode/bidi`
3. Set the paragraph direction based on `baseDir`:
   - `DirectionLTR` → `bidi.LeftToRight`
   - `DirectionRTL` → `bidi.RightToLeft`
4. Call `paragraph.Order()` to get resolved embedding levels
5. Walk the items and set each item's `BidiLevel` from the resolved levels
   at the item's text offset

**Step 3c:** For non-text items (OpenTag, CloseTag, AtomicInline, Control):
- OpenTag/CloseTag: inherit the bidi level of the adjacent text
- AtomicInline: treated as a strong character in the direction of the element's
  CSS `direction` property
- Control (forced break): level 0 (paragraph separator)

**Step 3d:** Handle `unicode-bidi` property values. These create explicit embedding
contexts in the UAX#9 algorithm:
- `embed` → insert LRE/RLE at OpenTag, PDF at CloseTag (before feeding to bidi)
- `bidi-override` → insert LRO/RRO at OpenTag, PDF at CloseTag
- `isolate` → insert LRI/RLI at OpenTag, PDI at CloseTag
- `isolate-override` → insert FSI+LRO/RRO at OpenTag, PDF+PDI at CloseTag
- `plaintext` → insert FSI at OpenTag, PDI at CloseTag
- `normal` → no embedding characters inserted

The approach: before calling the bidi library, pre-process the text by inserting
Unicode bidi control characters at the positions of OpenTag/CloseTag items that
have non-normal `unicode-bidi`. The resulting text with embedded controls is fed
to the bidi algorithm.

**Step 3e:** Call `ResolveBidiLevels()` from `layoutInlineChildren()`, after
`CollectInlines()` but before creating the `LineBreaker`:

```go
itemsData := CollectInlines(bla.node)
ResolveBidiLevels(itemsData, wdm.Dir)
// ... create LineBreaker
```

**Architectural note:** The `golang.org/x/text/unicode/bidi` package provides:
- `bidi.Paragraph` — UAX#9 paragraph processing
- `bidi.Ordering` — result with visual runs and their levels
- `bidi.Direction` — per-run direction

Study `golang.org/x/text/unicode/bidi` carefully before starting. The package
handles the full UAX#9 algorithm including bracket pairing (BD14-BD16, N0 rules).
If the API is awkward or limited, you can also implement the level resolution
directly — it is well-specified in UAX#9.

**Testing:** After this phase, `BidiLevel` is set correctly on all items. No visible
change yet — reordering comes in Phase 4.

---

### Phase 4: Visual Reordering Per Line

**Files:** `pkg/layout/inline_layout.go`, new helper in `pkg/layout/bidi.go`

This is the core integration point. After `LineBreaker.NextLine()` produces a line,
reorder the `line.Results` slice from logical order to visual order before passing
to `createLineBox()`.

**Step 4a:** Add a reordering function:

```go
// ReorderLineVisual reorders a line's InlineItemResults from logical order
// to visual order based on resolved bidi levels, following UAX#9 L2.
//
// Mirrors Blink's InlineLayoutAlgorithm::BidiReorder().
func ReorderLineVisual(results []InlineItemResult) []InlineItemResult
```

**Step 4b:** The UAX#9 L2 reordering algorithm:
1. Find the highest embedding level in the line
2. Find the lowest odd level in the line
3. For each level from highest down to lowest odd:
   - Reverse all contiguous runs at that level or higher

This can be implemented directly (it's ~20 lines of code) or via the
`golang.org/x/text/unicode/bidi` package's ordering.

**Step 4c:** Handle inline box boundaries correctly during reordering.

CRITICAL: When reordering, OpenTag and CloseTag items must stay correctly paired.
Blink's approach:
- OpenTag/CloseTag items inherit the bidi level of their content
- After reordering, OpenTag items that are now in reversed runs need their
  inline-start/inline-end to swap (or equivalently, the open/close logic in
  createLineBox must account for reversed runs)

The simplest approach (matching Blink): reorder only at the granularity of text
runs and atomic inlines. Keep OpenTag/CloseTag items attached to their content.
Concretely, group items into "reorderable units" where each unit is either:
- A text item (possibly with surrounding OpenTag/CloseTag at the same bidi level)
- An atomic inline
- An OpenTag or CloseTag that spans a bidi level boundary

Then reorder these units rather than individual items.

**Step 4d:** Insert the reorder call in `layoutInlineChildren()`, between
`NextLine()` and `createLineBox()`:

```go
if !lb.NextLine(&line) {
    break
}
// Reorder for bidi BEFORE positioning
ReorderLineVisual(line.Results)

lineFragment, lineHeight, lineAscent := createLineBox(...)
```

**Step 4e:** After reordering, `createLineBox()` positions items left-to-right
(increasing inline offset). Since items are now in visual order, this produces
correct physical positioning. The fragment builder then converts logical offsets
to physical coordinates, which for LTR-in-HTB is identity.

For RTL base direction with all-RTL content: after reordering, the items are
reversed, so placing them left-to-right in visual order is correct.

**Testing:** Mixed-direction text (e.g., "Hello עברית World") should now render
with correct visual order. Run the bidi-embed, bidi-isolate test suites.

---

### Phase 5: RTL-Aware Text Rendering

**Files:** `pkg/render/render.go`

**Step 5a:** By the time text reaches the renderer, it should already be in visual
order (from Phase 4 reordering). Each text fragment's content is a single-direction
run. For LTR runs, the current character-by-character rendering is correct.

For RTL runs: the text content is in logical order (Unicode codepoint order) but
needs to be rendered right-to-left. The simplest approach:
- Check if the text fragment has an odd bidi level (RTL)
- If RTL: reverse the rune order before drawing

Alternatively, mark each `PhysicalFragment` with its bidi level so the renderer
can detect RTL runs.

**Step 5b:** Add a `BidiLevel` field to `PhysicalFragment` (or to a sub-struct).
Set it in `createLineBox()` when creating text fragments.

**Step 5c:** In `drawText()`, check the bidi level:

```go
if fragment.BidiLevel % 2 == 1 {
    // RTL run: reverse rune order for visual rendering
    runes := []rune(box.Text)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    text = string(runes)
}
```

**Note:** This is a simplified approach. Full RTL rendering requires:
- Arabic/Hebrew ligature shaping (contextual forms)
- Mirroring of bracket characters (UAX#9 L4)

Character shaping is out of scope for this plan. Simple rune reversal handles
the common case (isolated RTL characters, numbers in RTL context).

Bracket mirroring (e.g., "(" → ")" in RTL context) is specified in UAX#9 L4
and can be added as a follow-up using `golang.org/x/text/unicode/bidi`'s
mirroring support.

**Testing:** RTL text should render in correct visual order. Hebrew/Arabic test
cases should show correct character sequence.

---

## File-by-File Change Summary

| File | Changes |
|------|---------|
| `pkg/layout/inline_layout.go` | Pass direction to text-align; insert bidi reorder call between NextLine and createLineBox; set BidiLevel on text fragments |
| `pkg/layout/line_breaker.go` | Set `BaseDirection` on `LineInfo` |
| `pkg/layout/bidi.go` | NEW — `ResolveBidiLevels()`, `ReorderLineVisual()`, unicode-bidi handling |
| `pkg/layout/inline_item.go` | No changes needed (BidiLevel field already exists) |
| `pkg/render/render.go` | RTL text reversal in `drawText()` |
| `pkg/layout/fragment_builder.go` | No changes needed |
| `pkg/layout/writing_mode_converter.go` | No changes needed |

---

## Dependencies

- `golang.org/x/text/unicode/bidi` — already in go.mod (v0.22.0, indirect).
  Will need to become a direct dependency.
- No other new dependencies required.

---

## Phase Ordering and Testing Strategy

Phases 1 and 2 are standalone and can be done first. They fix pure-RTL text
alignment without any bidi complexity.

Phases 3 and 4 are the core bidi implementation and must be done together
(3 produces the levels, 4 uses them for reordering).

Phase 5 can be done after 3+4, or deferred if rendering quality is acceptable
without it (the positioning will be correct, only individual character order
within RTL runs would be wrong).

**Test suites to validate against:**
- `css-writing-modes/bidi-embed-*` (84 tests)
- `css-writing-modes/block-flow-direction-*` (direction variants)
- Any test with `direction: rtl` in its HTML
- `css-writing-modes/text-align-*` tests

---

## Key Principles

1. **Follow Blink's architecture.** The data structures are already Blink-aligned.
   Insert bidi at exactly the same points Blink does: after CollectInlines (resolve
   levels), after NextLine (reorder results).

2. **Line breaking stays in logical order.** Do not change the line breaker to work
   in visual order. Blink breaks lines in logical order, then reorders. This is
   simpler and matches UAX#9.

3. **Reorder results, not items.** The `InlineItemsData.Items` list stays in logical
   order always. Only `line.Results` gets reordered, per line.

4. **Use `golang.org/x/text/unicode/bidi` if it works.** Study the API first. If
   it doesn't provide what we need (e.g., per-character level resolution), implement
   UAX#9 L2 directly — it's well-specified and not large.

5. **Don't change the fragment builder.** The logical-to-physical conversion already
   handles direction correctly. The fix is upstream: get items in the right order
   before positioning.
