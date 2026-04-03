# Writing Modes: Foundational Work Remaining

Status as of 2026-04-02. Ordered by architectural depth, not test count.

## Architecture Summary

The louis14 inline pipeline is **natively dir-aware for writing-mode** (vertical vs
horizontal). Layout works in logical coordinates (InlineSize/BlockSize, InlineOffset/
BlockOffset) throughout, with a single conversion point in `FragmentBuilder.Build()`.
There is no post-processing transform code. This is a clean break from louis13.

The core abstractions — `WritingDirectionMode`, `WritingModeConverter`, `LogicalSize`,
`LogicalOffset`, `LogicalEdges`, `ConstraintSpace` with axis-swapping — are mature and
mirror Blink's LayoutNG architecture.

---

## Gap 1: Root-Level Writing Mode Propagation

**What:** When `<html>` has `writing-mode: vertical-*`, the ICB's writing mode is not
fully propagated to absolute positioning resolution. There is also a force-HTB override
path in `layout_block.go` that can prevent vertical abs-pos children from ever receiving
their correct writing mode.

**Where:**
- `engine.go` — root constraint space construction
- `out_of_flow_layout.go` — abs-pos resolution using CB's writing mode

**Impact:** ~64 abs-pos tests fail when the containing block is the root element with a
vertical writing mode. The layout engine resolves insets against an HTB coordinate system
instead of the element's actual writing mode.

**Nature:** Missing wire. The infrastructure exists; the root just doesn't thread it through.

---

## Gap 2: Two-Pass Absolute Positioning Sizing

**What:** CSS §10.3.7 / §10.6.4 require that when both insets are set and size is auto,
the element's size is determined by the constraint equation (CB size - insets - margins)
*before* layout. Currently, `out_of_flow_layout.go` lays out first with the full CB size
available, then uses the unconstrained result size.

**Where:** `out_of_flow_layout.go:68` — lays out with full CB, then uses unconstrained
result at line 89.

**Impact:** ~54 abs-pos tests. Auto-sized elements get shrink-to-fit instead of
inset-constrained sizing.

**Nature:** Spec-order issue. The forced size from insets needs to be computed before
`layoutElement()`, then passed as a fixed size in the constraint space.

---

## Gap 3: Sideways Text Rendering (Coordinate Frame Rotation)

**What:** Sideways-rl and sideways-lr should rotate the drawing context 90 degrees and
draw text horizontally. The current renderer stacks characters vertically, the same as
VRL/VLR upright rendering. These are fundamentally different rendering strategies.

**Where:** `render.go:520-556` — character stacking code path.

**Impact:** ~42 writing-modes tests with sideways variants show 15-29% pixel diffs.

**Nature:** Rendering strategy change. Need to check whether the drawing context supports
rotation (save/rotate/draw/restore pattern).

---

## Gap 4: CSS `direction` (RTL/LTR) and Bidirectional Text

**What:** The inline pipeline handles writing-mode (vertical vs horizontal) natively, but
CSS `direction: rtl` and bidirectional text (UAX#9) are not implemented.

**Specific gaps:**
1. `text-align: start/end` ignores direction — "start" always means left
2. `BidiLevel` field on `InlineItem` exists but is never computed
3. No Unicode Bidirectional Algorithm — mixed-direction text is not reordered
4. `unicode-bidi` property values are parsed but not acted on
5. Text renderer draws in logical order, not visual order

**Where:**
- `inline_layout.go:578-600` — `computeTextAlignOffset()` has no direction parameter
- `inline_item.go:54-55` — `BidiLevel` is always 0
- `line_breaker.go` — items processed in source order only

**Impact:** ~84 bidi tests, ~108 direction tests, plus cross-cutting failures in other
categories where direction:rtl is tested.

**Nature:** Missing subsystem. Blink implements this via `BidiParagraph` (UAX#9 resolution)
and visual reordering in `InlineLayoutAlgorithm::PlaceItems()`. See
`docs/dir-aware-inline-pipeline.md` for the detailed plan.

---

## Gap 5: Orthogonal Fallback at Root

**What:** When the root element is vertical and a child is horizontal (or vice versa), the
child's auto inline-size should fall back to the ICB's cross-axis dimension per CSS Writing
Modes §10.3.2. The root constraint space doesn't call `SetOrthogonalFallbackInlineSize()`.

**Where:** `engine.go` — root constraint space builder.

**Impact:** 38 sizing-orthog tests have 0% pass rate, all with small diffs (0.3-3.4%).

**Nature:** Near-trivial fix. Non-root constraint spaces already set this correctly
(`block_layout.go:169`, `flex_layout.go:1359`, etc.). The root just needs the same call.
