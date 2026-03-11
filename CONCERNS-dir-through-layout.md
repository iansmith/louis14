# Concerns Log: Dir Through layoutNode (Phase 1a-1c)

## Purpose
Track regression concerns as we thread Dir through layoutNode call sites.
Each entry notes the call site, what Dir it currently passes (HTB), what it
should pass, and potential issues.

---

## Call Site Categories

### Category A: Block layout child calls (layout_block.go)
These lay out children of a block container. The parent's Dir should propagate.

### Category B: Inline layout calls (layout_inline_multipass.go, layout_inline_singlepass.go)
Block children, floats, and atomic inlines within inline formatting contexts.
These run in the parent's inline formatting context, which currently assumes HTB.

### Category C: Flex layout calls (layout_flex.go)
Flex item layout. Flex has its own axis system; writing-mode interacts with it.

### Category D: Grid layout calls (grid.go)
Grid cell layout. Grid has its own track system.

### Category E: Table layout calls (layout_table.go)
Table cell and caption layout.

### Category F: Multicol layout calls (layout_multicol.go)
Column content layout.

### Category G: Test calls (inline_layout_test.go)
Unit tests — always HTB, no concern.

---

## Concerns

### 1. Flex layout + Dir interaction (layout_flex.go, ~10 call sites)
**Concern**: Flex layout has its own main/cross axis system that already partially
handles writing-mode via `isVerticalWM`. Passing a vertical Dir into flex item
layout could interact with the existing vertical writing-mode handling in
`createFlexItemsProper` and `transformToVerticalRL` in complex ways. The flex
code already does its own writing-mode detection.
**Decision**: Skip flex call sites for now — leave as layoutNodeHTB. The flex
code manages its own writing-mode handling. Converting flex is a separate task.

### 2. Grid layout + Dir interaction (grid.go, 2 call sites)
**Concern**: Similar to flex — grid has its own track sizing that handles
writing-mode separately.
**Decision**: Skip grid call sites for now.

### 3. Table layout + Dir interaction (layout_table.go, 3 call sites)
**Concern**: Table layout has complex cell sizing. Writing-mode in tables is
partially handled by the vertical transform at the end of layoutNode.
**Decision**: Skip table call sites for now.

### 4. Multicol + Dir interaction (layout_multicol.go, 2 call sites)
**Concern**: Multicol already has its own column arrangement logic.
**Decision**: Skip multicol call sites for now.

### 5. Inline layout block children (layout_inline_multipass.go:1915)
**Concern**: Block children within inline formatting contexts are laid out by
layoutNodeHTB. These blocks should inherit the container's Dir. However, the
inline layout system itself doesn't yet use Dir for line breaking/stacking.
Passing Dir here would affect the block child's internal layout (correct) but
the block child's position is still computed in HTB coordinates by the inline
layout engine.
**Decision**: Convert this — the block child's internal layout benefits from
Dir awareness. Its position within the inline context is handled separately.

### 6. Inline layout floats (layout_inline_multipass.go:2193)
**Concern**: Floats within inline content. Float positioning uses physical
coordinates. Passing Dir here would affect the float's internal layout but
not its float positioning (which is in the parent's coordinate system).
**Decision**: Convert this — float's internal layout benefits from Dir.

### 7. Inline layout atomic inlines (layout_inline_multipass.go:2279)
**Concern**: Inline-block, inline-table, etc. These establish their own
formatting context. Their internal layout should use their own Dir.
**Decision**: Convert this.

### 8. Block layout main child loop (layout_block.go:1601, 1800)
**Concern**: This is the main block child layout loop. Passing childDir here
is the core of Phase 1. The child's available width and positioning are
currently computed in physical HTB coordinates. Passing Dir changes what
"available width" means for the child.
**Risk**: High — this is the most impactful change. Children in vertical mode
will get their available inline-size from the parent's physical height instead
of width. But the parent's box coordinates (x, y) are still physical.
**Decision**: This is the core change. Proceed carefully.

### 9. Block layout re-layout (layout_block.go:2421)
**Concern**: Re-layout of children after height determination. Should use
the same Dir as the initial layout.
**Decision**: Convert this.

### 10. The `transformToVerticalRL` interaction
**Concern**: After layoutNode returns, layout_block.go applies
transformToVerticalRL for vertical writing-mode elements. If we pass Dir
through layoutNode, the child layout is already Dir-aware, but the
transform still runs and may double-transform or produce incorrect results.
**Risk**: The transform groups children by Y position and rearranges them
into columns. If children are already laid out in vertical coordinates
(block = X, inline = Y), the transform would misinterpret them.
**Decision**: For Phase 1a-1c, we must NOT disable the transform yet. Instead,
only pass Dir for the `isSameAxisVertical` case where both parent and child
are vertical — the child's available inline-size needs to come from the parent's
height. The transform handles the rest. OR: gate Dir propagation behind
`if !dir.IsVertical()` to avoid interaction with the transform pipeline.

UPDATED: After analysis, the safest approach is to propagate childDir in the
main block child loop but ONLY when the child is NOT going to be transformed
by transformToVerticalRL. Elements that trigger the transform should still
get HTB layout (the transform handles the conversion). Elements that DON'T
trigger the transform (because they share the parent's vertical writing-mode)
should get the parent's Dir.

### 11. RESOLVED: Dir causes logical-physical mismatch in sizing (Phase 1a)
**Issue discovered**: When dir=VLR is passed to a child, all 100+ uses of `dir.`
in layoutNode change behavior. `dir.InlineSizeProp()` returns "height" instead
of "width", `dir.SetInlineSize(box, v)` sets `box.Height` instead of `box.Width`.
This caused 10 float regressions in VLR/VRL tests because float children had
their width/height swapped, and the parent's float positioning code expected
physical dimensions.
**Root cause**: The layout code mixes logical Dir accessors with physical
coordinate assumptions. You can't just pass a vertical Dir without converting
ALL code that touches box dimensions to use Dir accessors consistently.
**Resolution**: Override `dir = NewDir(HorizontalTB)` at the top of layoutNode
so the element's own sizing stays physical. Only `childDir` (for recursive
calls) carries the actual writing-mode. Future Phase 1b will remove this
override for specific code paths as they're converted to be Dir-aware.
