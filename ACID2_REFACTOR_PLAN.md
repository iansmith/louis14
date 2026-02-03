# Acid2 Refactoring Plan

## Current Issues

### 1. Flat-List Rendering Breaks Stacking Contexts
**Problem**: The renderer collects all boxes into a flat list and sorts by z-index globally. This violates CSS stacking context semantics where:
- Children render within their parent's stacking context
- A parent with `position: relative` and `z-index: 2` should render its entire subtree at z-level 2, not have its children interleaved with other z-level elements

**Evidence**: When we tried to fix the scalp (fixed, z-index:0) rendering under the parser div (static, z-index:0) by making positioned elements render last, it caused parents to render over their own children (the `eyes` element with red background covered its own child elements).

### 2. Margin Collapsing Not Fully Implemented
**Problem**: Vertical margins between siblings should collapse (the larger wins). Our layout compresses the face vertically because margins aren't collapsing/stacking properly.

**Evidence**:
- Forehead has 48px bottom margin
- Eyes starts immediately after forehead (no margin gap)
- Smile is at viewport y=60 but should be at y=120 in reference

### 3. Paint Order Within Stacking Contexts
CSS 2.1 Appendix E specifies this order within each stacking context:
1. Background and borders of the stacking context element
2. Child stacking contexts with negative z-index
3. In-flow, non-inline-level, non-positioned descendants
4. Non-positioned floats
5. In-flow, inline-level, non-positioned descendants
6. Child stacking contexts with z-index: auto/0 and positioned descendants
7. Child stacking contexts with positive z-index

Our flat-list approach doesn't respect this hierarchy.

---

## Proposed Solution: Stacking Context Tree

### Phase 1: Data Structure Changes

#### Add StackingContext struct
```go
// pkg/layout/stacking.go
type StackingContext struct {
    Box       *Box                // The box that creates this context
    ZIndex    int                 // Z-index value
    Parent    *StackingContext    // Parent context (nil for root)
    Children  []*StackingContext  // Child stacking contexts

    // Content within this context, organized by paint order
    NegativeZChildren []*StackingContext  // z-index < 0
    BlockBoxes        []*Box              // Non-positioned blocks
    FloatBoxes        []*Box              // Floated elements
    InlineBoxes       []*Box              // Inline content
    PositionedAuto    []*Box              // Positioned with z-index: auto
    PositiveZChildren []*StackingContext  // z-index > 0
}
```

#### Modify Box struct
```go
type Box struct {
    // ... existing fields ...
    StackingContext *StackingContext  // The context this box belongs to
    CreatesContext  bool              // Does this box create a new stacking context?
}
```

### Phase 2: Layout Changes

#### Build stacking context tree during layout
- When a box creates a stacking context (positioned + z-index, or opacity < 1, etc.), create a new StackingContext
- Assign each box to its nearest ancestor's stacking context
- Sort children within each context by paint order

#### Stacking context creation rules (CSS 2.1):
- Root element
- Positioned elements with z-index != auto
- Elements with opacity < 1
- Elements with transform, filter, etc.

### Phase 3: Renderer Changes

#### Hierarchical rendering
```go
func (r *Renderer) RenderStackingContext(ctx *StackingContext) {
    // 1. Draw background/borders of context's box
    r.drawBoxBackgroundBorders(ctx.Box)

    // 2. Negative z-index children (recursively)
    for _, child := range ctx.NegativeZChildren {
        r.RenderStackingContext(child)
    }

    // 3. In-flow blocks
    for _, box := range ctx.BlockBoxes {
        r.drawBox(box)
    }

    // 4. Floats
    for _, box := range ctx.FloatBoxes {
        r.drawBox(box)
    }

    // 5. Inline content
    for _, box := range ctx.InlineBoxes {
        r.drawBox(box)
    }

    // 6. Positioned with z-index: auto and z-index: 0 children
    for _, box := range ctx.PositionedAuto {
        r.drawBox(box)
    }
    for _, child := range ctx.Children {
        if child.ZIndex == 0 {
            r.RenderStackingContext(child)
        }
    }

    // 7. Positive z-index children
    for _, child := range ctx.PositiveZChildren {
        r.RenderStackingContext(child)
    }
}
```

---

## Phase 4: Margin Collapsing Review

### Current behavior to fix:
1. **Adjacent sibling margins** - vertical margins between siblings should collapse (larger wins)
2. **Parent-child margins** - first/last child margins can collapse with parent margins (if no padding/border separates them)
3. **Empty boxes** - margins through empty boxes collapse
4. **Negative margins** - negative and positive margins are summed separately, then the absolute values determine final margin

### Implementation approach:
- Add a margin collapsing pass after initial layout
- Track "uncollapsed" vs "collapsed" margin values
- Apply collapsing rules to adjacent vertical margins

---

## Migration Strategy

### Step 1: Add StackingContext without breaking existing code
- Create the stacking context tree during layout
- Keep existing flat-list rendering as fallback
- Add flag to switch between old and new rendering

### Step 2: Implement new renderer
- Create RenderStackingContext method
- Test against visual regression suite
- Fix any differences

### Step 3: Fix margin collapsing
- May require adjustments to how Layout() calculates Y positions
- Focus on the specific cases Acid2 needs

### Step 4: Remove old code path
- Once new renderer passes all tests, remove flat-list code

---

## Expected Impact

### Things that might break temporarily:
- Some visual regression tests (due to render order changes)
- Complex layouts with z-index interactions

### Benefits:
- Correct CSS stacking context semantics
- Proper paint order within contexts
- Foundation for more complex CSS features (transforms, filters, clip-path)
- Better Acid2 compliance

---

## Estimated Effort

- Phase 1 (Data structures): Small
- Phase 2 (Layout changes): Medium - need to integrate with existing layout logic
- Phase 3 (Renderer): Medium - need to handle all the edge cases
- Phase 4 (Margin collapsing): Large - this is the most complex CSS behavior

Recommend starting with Phases 1-3 first, then tackling margin collapsing separately.
