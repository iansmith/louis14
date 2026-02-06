# Multi-Pass Inline Layout Redesign

**Date**: 2026-02-05
**Status**: Design phase
**Based on**: Blink LayoutNG architecture principles

---

## Key Learnings from Blink LayoutNG

### Core Principle: Immutability
- **Old way**: Mutable Box tree, modify positions in place
- **New way**: Immutable Fragment tree, calculate correct positions once
- **Benefit**: No position deltas, no recursive repositioning, no stale state

### Core Principle: Constraint Space
- **Old way**: Pass raw available width, access global float list
- **New way**: Package all constraints (width, floats, etc.) into one object
- **Benefit**: Clear API, easy to create modified copies for retry

### Core Principle: Exclusion Space
- **Old way**: Global `le.floats []FloatInfo`, mutated during layout
- **New way**: Immutable exclusion space passed via constraint space
- **Benefit**: No global state, retry iterations get clean copies

### Core Principle: Separation of Sizing vs Layout
- **Old way**: layoutNode() both queries dimensions AND lays out with side effects
- **New way**: Separate functions for "get min/max size" vs "create fragments"
- **Benefit**: Dimension queries don't pollute state

**Sources**:
- [LayoutNG Architecture](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/third_party/blink/renderer/core/layout/layout_ng.md)
- [RenderingNG deep-dive: LayoutNG](https://developer.chrome.com/docs/chromium/layoutng)
- [NGConstraintSpace source](https://chromium.googlesource.com/chromium/src/+/a5795e0a0e58d1bba6c9890f2467bbe2055791a8/third_party/blink/renderer/core/layout/ng/ng_constraint_space.h)

---

## Our Design

### Phase 1: Data Structures

#### ExclusionSpace (Float Tracking)
```go
// Immutable representation of floats affecting inline layout
type ExclusionSpace struct {
	exclusions []Exclusion  // List of float rectangles
}

type Exclusion struct {
	Rect  Rect        // Y, Height, Left or Right edge
	Side  css.Float   // FloatLeft or FloatRight
}

// Pure query methods (no mutation)
func (es *ExclusionSpace) AvailableInlineSize(y, height float64) (leftOffset, rightOffset float64)
func (es *ExclusionSpace) IsEmpty() bool

// Returns NEW ExclusionSpace with added exclusion (immutable)
func (es *ExclusionSpace) Add(exclusion Exclusion) *ExclusionSpace
```

**Key**: Immutable! `Add()` returns a NEW ExclusionSpace, doesn't modify original.

#### ConstraintSpace
```go
// All constraints for laying out a subtree
type ConstraintSpace struct {
	AvailableSize   Size
	ExclusionSpace  *ExclusionSpace
	TextAlign       css.TextAlign
	// ... other constraints as needed
}

// Helper to create modified constraint space
func (cs *ConstraintSpace) WithExclusion(exclusion Exclusion) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize:  cs.AvailableSize,
		ExclusionSpace: cs.ExclusionSpace.Add(exclusion),
		TextAlign:      cs.TextAlign,
	}
}
```

**Key**: Passed by value or pointer, never mutated. Create modified copies as needed.

#### Fragment (Immutable Output)
```go
// Immutable layout output
type Fragment struct {
	Node      *html.Node
	Style     *css.Style
	Position  Position    // Correct position from the start
	Size      Size
	Children  []*Fragment // Owns children
	Type      FragmentType
}

type FragmentType int
const (
	FragmentText FragmentType = iota
	FragmentInline
	FragmentBlock
	FragmentFloat
)
```

**Key**: Created with correct position, never repositioned. Immutable after creation.

---

### Phase 2: Separation of Concerns

#### Dimension Queries (No Side Effects!)
```go
// Query min/max content sizes WITHOUT laying out
type MinMaxSizes struct {
	MinContentSize float64
	MaxContentSize float64
}

func (le *LayoutEngine) ComputeMinMaxSizes(
	node *html.Node,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) MinMaxSizes {
	// PURE FUNCTION - no side effects!
	// Doesn't modify le.floats
	// Doesn't create fragments
	// Just calculates what sizes WOULD be
}
```

#### Layout (Creates Fragments)
```go
// Actually lay out and create fragment tree
func (le *LayoutEngine) Layout(
	node *html.Node,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) *Fragment {
	// THIS function can have side effects
	// Creates fragments
	// But uses constraint space, not global state
}
```

**Key**: CollectInlineItems can call ComputeMinMaxSizes to get dimensions without side effects!

---

### Phase 3: Three-Phase Inline Layout

#### Phase 1: CollectInlineItems (Pure)
```go
type InlineItem struct {
	Type        InlineItemType
	Node        *html.Node
	Style       *css.Style
	Text        string
	MinMaxSizes MinMaxSizes  // Computed dimensions (for floats/atomic)
}

func (le *LayoutEngine) CollectInlineItems(
	children []*html.Node,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) []*InlineItem {
	items := []*InlineItem{}

	for _, child := range children {
		if child.Type == html.TextNode {
			// Measure text
			width, height := measureText(child.Text, style)
			items = append(items, &InlineItem{
				Type: InlineItemText,
				Node: child,
				MinMaxSizes: MinMaxSizes{width, width},
			})
		} else if isFloat(child) {
			// Query dimensions WITHOUT side effects!
			sizes := le.ComputeMinMaxSizes(child, constraint, computedStyles)
			items = append(items, &InlineItem{
				Type:        InlineItemFloat,
				Node:        child,
				MinMaxSizes: sizes,
			})
		}
		// ... handle other types
	}

	return items  // Pure output, no state mutation
}
```

**Key**: Calls `ComputeMinMaxSizes`, NOT `Layout`! No side effects!

#### Phase 2: BreakLines (Pure-ish)
```go
type LineInfo struct {
	Y             float64
	Items         []*InlineItem
	Constraint    *ConstraintSpace  // Constraint for THIS line
}

func (le *LayoutEngine) BreakLines(
	items []*InlineItem,
	constraint *ConstraintSpace,
) []*LineInfo {
	lines := []*LineInfo{}
	currentY := 0.0

	for _, item := range items {
		// Get available width at current Y from exclusion space
		leftOff, rightOff := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.MinMaxSizes.MaxContentSize)
		availWidth := constraint.AvailableSize.Width - leftOff - rightOff

		// Check if item fits
		if item.MinMaxSizes.MinContentSize > availWidth {
			// Doesn't fit, new line
			currentY += lineHeight
			lines = append(lines, newLine)
			newLine = &LineInfo{Y: currentY}
		}

		newLine.Items = append(newLine.Items, item)
	}

	return lines  // Pure output
}
```

**Key**: Uses constraint.ExclusionSpace to query, doesn't modify it!

#### Phase 3: ConstructFragments (Has Side Effects)
```go
func (le *LayoutEngine) ConstructFragments(
	lines []*LineInfo,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) []*Fragment {
	fragments := []*Fragment{}
	currentConstraint := constraint

	for _, line := range lines {
		lineFragments, newConstraint := le.constructLine(line, currentConstraint, computedStyles)
		fragments = append(fragments, lineFragments...)
		currentConstraint = newConstraint  // Propagate exclusions to next line
	}

	return fragments
}

func (le *LayoutEngine) constructLine(
	line *LineInfo,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) ([]*Fragment, *ConstraintSpace) {
	fragments := []*Fragment{}
	currentConstraint := constraint

	// Calculate starting X from exclusion space
	leftOff, _ := constraint.ExclusionSpace.AvailableInlineSize(line.Y, 0)
	x := leftOff

	for _, item := range line.Items {
		switch item.Type {
		case InlineItemText:
			frag := &Fragment{
				Type:     FragmentText,
				Node:     item.Node,
				Position: Position{X: x, Y: line.Y},
				Size:     Size{Width: item.MinMaxSizes.MaxContentSize},
			}
			fragments = append(fragments, frag)
			x += item.MinMaxSizes.MaxContentSize

		case InlineItemFloat:
			// Layout the float to get actual fragment
			floatFrag := le.Layout(item.Node, currentConstraint, computedStyles)

			// Calculate float position from current constraint
			floatX := le.calculateFloatPosition(floatFrag, line.Y, currentConstraint)
			floatFrag.Position = Position{X: floatX, Y: line.Y}

			fragments = append(fragments, floatFrag)

			// Create NEW constraint with this float added
			exclusion := Exclusion{
				Rect: Rect{
					Y:      line.Y,
					Height: floatFrag.Size.Height,
					X:      floatX,
					Width:  floatFrag.Size.Width,
				},
				Side: item.Style.GetFloat(),
			}
			currentConstraint = currentConstraint.WithExclusion(exclusion)
		}
	}

	return fragments, currentConstraint
}
```

**Key**: Creates NEW constraint spaces with `.WithExclusion()`, doesn't mutate global state!

---

### Phase 4: Retry Logic

```go
func (le *LayoutEngine) LayoutInlineContent(
	children []*html.Node,
	constraint *ConstraintSpace,
	computedStyles map[*html.Node]*css.Style,
) []*Fragment {
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 1: Collect (pure, no side effects)
		items := le.CollectInlineItems(children, constraint, computedStyles)

		// Phase 2: Break lines (pure-ish)
		lines := le.BreakLines(items, constraint)

		// Phase 3: Construct fragments
		fragments, finalConstraint := le.ConstructFragments(lines, constraint, computedStyles)

		// Check if any float changed available width
		if !constraintsChanged(constraint, finalConstraint, lines) {
			return fragments  // Success!
		}

		// Retry with updated constraint that includes floats
		constraint = finalConstraint
	}

	// Max retries - return last attempt
	// (This shouldn't happen in practice)
	return le.ConstructFragments(le.BreakLines(items, constraint), constraint, computedStyles)
}
```

**Key**: Each retry uses updated constraint, no global state to reset!

---

## Migration Path

### Step 1: Implement Core Data Structures
- [ ] ExclusionSpace type
- [ ] ConstraintSpace type
- [ ] Fragment type
- [ ] Unit tests for each

### Step 2: Implement Dimension Queries
- [ ] ComputeMinMaxSizes for text
- [ ] ComputeMinMaxSizes for inline boxes
- [ ] ComputeMinMaxSizes for floats
- [ ] ComputeMinMaxSizes for inline-blocks
- [ ] Unit tests

### Step 3: Refactor Existing Code
- [ ] Extract CollectInlineItems to use ComputeMinMaxSizes
- [ ] Extract BreakLines to use ConstraintSpace
- [ ] Keep existing Layout as fallback

### Step 4: Implement Fragment Construction
- [ ] constructLine with ConstraintSpace
- [ ] Float positioning with ExclusionSpace
- [ ] Convert fragments back to Box tree (for now)

### Step 5: Integration
- [ ] Wire up LayoutInlineContent
- [ ] Test with WPT tests
- [ ] Fix issues
- [ ] Gradually replace single-pass

### Step 6: Eventually
- [ ] Replace Box tree with Fragment tree everywhere
- [ ] Remove old layout code
- [ ] Full LayoutNG-style architecture

---

## Expected Benefits

### Correctness
✅ No negative X coordinates (correct calculation from start)
✅ No float accumulation bugs (immutable exclusion space)
✅ No retry side effects (clean constraint copies)

### Maintainability
✅ Each phase testable in isolation
✅ Clear data flow (input → algorithm → output)
✅ No hidden global state

### Performance
✅ Can cache fragments (immutable)
✅ Can reuse dimension calculations
✅ Clearer optimization opportunities

---

## Next Steps

1. **Implement ExclusionSpace** - Start with the foundational data structure
2. **Add unit tests** - Test exclusion space queries thoroughly
3. **Implement ConstraintSpace** - Package constraints cleanly
4. **Implement ComputeMinMaxSizes** - Pure dimension queries
5. **Refactor CollectInlineItems** - Use new dimension queries
6. **Test incrementally** - Each step maintains baseline

**Ready to start? Begin with ExclusionSpace implementation!** 🚀
