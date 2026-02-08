package layout

import (
	"fmt"
	"strconv"
	"strings"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

func NewTextFragment(text string, style *css.Style, x, y, width, height float64, node *html.Node) *Fragment {
	return &Fragment{
		Type: FragmentText,
		Text: text,
		Node: node, // CRITICAL: Must set Node for rendering
		Style: style,
		Position: Position{X: x, Y: y},
		Size:     Size{Width: width, Height: height},
	}
}

// NewBoxFragment creates a fragment from a box (for existing layout code).
func NewBoxFragment(box *Box, fragType FragmentType) *Fragment {
	return &Fragment{
		Type:     fragType,
		Node:     box.Node,
		Style:    box.Style,
		Position: Position{X: box.X, Y: box.Y},
		Size:     Size{Width: box.Width, Height: box.Height},
		Box:      box,
	}
}

// MinMaxSizes represents the intrinsic sizing information for an element.
// These are the "content-based" sizes (CSS Sizing Level 3):
// - MinContentSize: minimum size without overflow (narrowest the content can be)
// - MaxContentSize: preferred size without wrapping (widest the content wants to be)
//
// For text: min = longest word, max = full text width
// For inline boxes with inline children: min = max child min, max = sum of child max
// For inline boxes with block children: min = max child min, max = max child max

// ComputeMinMaxSizes calculates the intrinsic sizing information for a node
// WITHOUT laying it out (pure function, no side effects).
//
// This is CRITICAL for the new architecture:
// - OLD way: Call layoutNode() to get dimensions → causes side effects (pollutes le.floats)
// - NEW way: Call ComputeMinMaxSizes() → pure query, no state changes
//
// This function is used during Phase 1 (CollectInlineItems) to get float dimensions
// without actually laying them out.

// computeTextMinMax calculates min/max sizes for text content.
// Min size: width of longest word (won't wrap within words)
// Max size: width of full text (preferred width without wrapping)

// computeInlineMinMax calculates min/max sizes for inline elements.
// Inline elements with inline children: sum child widths (horizontal flow)
// Inline elements with block children: max child widths (stacking)

// computeBlockMinMax calculates min/max sizes for block elements.
// For blocks: min/max based on children (blocks stack vertically)

// computeInlineBlockMinMax calculates min/max sizes for inline-block elements.
// Inline-blocks are sized like blocks but participate in inline layout.

// computeStylesForTree computes styles for a node and all its descendants.
// This is a helper to avoid recomputing styles multiple times.

// BreakLines performs line breaking on a list of inline items.
// This is Phase 2 of the multi-pass inline layout pipeline.
//
// PURE FUNCTION - no side effects! Only decides what items go on which lines.
// The actual fragment construction happens in Phase 3.
//
// Algorithm:
// 1. Iterate through items sequentially
// 2. For each item, check if it fits on current line
// 3. Account for floats via constraint.ExclusionSpace
// 4. If doesn't fit, start new line
// 5. Return list of LineInfo (one per line)
func (le *LayoutEngine) BreakLines(
	items []*InlineItem,
	constraint *ConstraintSpace,
	startY float64,
) []*LineInfo {
	if len(items) == 0 {
		return []*LineInfo{}
	}

	lines := []*LineInfo{}
	currentY := startY
	currentLine := &LineInfo{
		Y:          currentY,
		Items:      []*InlineItem{},
		Constraint: constraint,
		Height:     0,
	}
	currentX := 0.0 // X position on current line

	for i := 0; i < len(items); i++ {
		item := items[i]

		// Get available width at current Y position
		// This accounts for floats via the exclusion space
		availableWidth := constraint.AvailableInlineSize(currentY, item.Height)

		// Check if we need to start at a different X due to floats
		leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)

		// If this is a new line, start at the left offset
		if currentX == 0 {
			currentX = leftOffset
		}

		// Calculate how much space we've used on this line
		usedWidth := currentX - leftOffset

		switch item.Type {
		case InlineItemText:
			// Text item - may need to wrap
			textWidth := item.Width

			if usedWidth+textWidth <= availableWidth {
				// Fits on current line
				currentLine.Items = append(currentLine.Items, item)
				currentX += textWidth

				// Update line height
				if item.Height > currentLine.Height {
					currentLine.Height = item.Height
				}
			} else if textWidth <= availableWidth {
				// Doesn't fit, but would fit on new line
				// Finish current line
				if len(currentLine.Items) > 0 {
					lines = append(lines, currentLine)
					currentY += currentLine.Height
				}

				// Start new line
				leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)
				currentLine = &LineInfo{
					Y:          currentY,
					Items:      []*InlineItem{item},
					Constraint: constraint,
					Height:     item.Height,
				}
				currentX = leftOffset + textWidth
			} else {
				// Text is wider than available width - need to break the text
				// For now, force it onto current line (TODO: implement text breaking)
				currentLine.Items = append(currentLine.Items, item)
				currentX += textWidth

				if item.Height > currentLine.Height {
					currentLine.Height = item.Height
				}
			}

		case InlineItemFloat:
			// Float item - doesn't take up inline space, but affects subsequent content
			// For Phase 2, we just note that it's on this line
			// Phase 3 will position it and update the constraint
			currentLine.Items = append(currentLine.Items, item)

			// Update line height
			if item.Height > currentLine.Height {
				currentLine.Height = item.Height
			}

		case InlineItemAtomic:
			// Atomic item (inline-block, replaced element) - cannot break
			atomicWidth := item.Width

			if usedWidth+atomicWidth <= availableWidth {
				// Fits on current line
				currentLine.Items = append(currentLine.Items, item)
				currentX += atomicWidth

				// Update line height
				if item.Height > currentLine.Height {
					currentLine.Height = item.Height
				}
			} else {
				// Doesn't fit - start new line
				if len(currentLine.Items) > 0 {
					lines = append(lines, currentLine)
					currentY += currentLine.Height
				}

				// Start new line with this item
				leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)
				currentLine = &LineInfo{
					Y:          currentY,
					Items:      []*InlineItem{item},
					Constraint: constraint,
					Height:     item.Height,
				}
				currentX = leftOffset + atomicWidth
			}

		case InlineItemOpenTag, InlineItemCloseTag:
			// Tag markers - add to current line but don't affect layout
			currentLine.Items = append(currentLine.Items, item)

		case InlineItemControl:
			// Control item (br, etc.) - forces line break
			currentLine.Items = append(currentLine.Items, item)

			// Finish current line
			if len(currentLine.Items) > 0 {
				lines = append(lines, currentLine)
				currentY += currentLine.Height
			}

			// Start new line
			currentLine = &LineInfo{
				Y:          currentY,
				Items:      []*InlineItem{},
				Constraint: constraint,
				Height:     0,
			}
			currentX = 0

		case InlineItemBlockChild:
			// Block child - MUST be on its own line
			// Finish current line if it has any content
			if len(currentLine.Items) > 0 {
				lines = append(lines, currentLine)
				currentY += currentLine.Height
			}

			// Create a line containing ONLY the block child
			// Height will be determined during recursive layout in Phase 3
			currentLine = &LineInfo{
				Y:          currentY,
				Items:      []*InlineItem{item},
				Constraint: constraint,
				Height:     0, // Will be set after recursive layout
			}
			lines = append(lines, currentLine)

			// Y advance will happen in Phase 3 after we know the block's height
			// For now, just reset for next line
			currentLine = &LineInfo{
				Y:          currentY, // Will be updated in Phase 3
				Items:      []*InlineItem{},
				Constraint: constraint,
				Height:     0,
			}
			currentX = 0

		default:
			// Unknown item type - treat as atomic
			currentLine.Items = append(currentLine.Items, item)
			currentX += item.Width

			if item.Height > currentLine.Height {
				currentLine.Height = item.Height
			}
		}
	}

	// Add final line if it has items
	if len(currentLine.Items) > 0 {
		lines = append(lines, currentLine)
	}

	return lines
}

// LayoutInlineContent orchestrates the three-phase inline layout with retry support.
// This is the CLEAN implementation following Blink LayoutNG principles.
//
// Three phases:
// 1. CollectInlineItems - flatten DOM to sequential items (PURE - no side effects)
// 2. BreakLines - decide line breaks (PURE - no side effects)
// 3. ConstructFragments - create positioned fragments (HAS side effects)
//
// Retry logic:
// - After Phase 3, check if floats were added that affect line breaking
// - If yes, retry with updated constraint space
// - Max 3 iterations to prevent infinite loops
//
// NOTE: Phase 3 (ConstructFragments) is not yet implemented, so this is a
// simplified version that demonstrates the retry pattern.
func (le *LayoutEngine) LayoutInlineContent(
	children []*html.Node,
	constraint *ConstraintSpace,
	startY float64,
) []*Fragment {
	const maxRetries = 3

	// Three-phase pipeline with retry support
	// This is the COMPLETE implementation following Blink LayoutNG principles!

	// CRITICAL: Always start from the ORIGINAL constraint
	// Don't carry over float exclusions from previous retries
	// Phase 3 will rebuild exclusions from scratch each time
	originalConstraint := constraint
	var finalFragments []*Fragment

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 1: Collect inline items (PURE - no side effects!)
		// Use original constraint - don't accumulate floats across retries
		items := le.collectInlineItemsClean(children, originalConstraint)

		// DEBUG: Show collected items
		if attempt == 0 {
			fmt.Printf("\n=== PHASE 1: Collected %d items ===\n", len(items))
			for i, item := range items {
				typeName := ""
				switch item.Type {
				case InlineItemText:
					typeName = "Text"
				case InlineItemOpenTag:
					typeName = "OpenTag"
				case InlineItemCloseTag:
					typeName = "CloseTag"
				case InlineItemFloat:
					typeName = "Float"
				case InlineItemAtomic:
					typeName = "Atomic"
				case InlineItemBlockChild:
					typeName = "BlockChild"
				default:
					typeName = fmt.Sprintf("Type%d", item.Type)
				}
				fmt.Printf("  [%d] %s: %s, Size: %.1fx%.1f\n",
					i, typeName, getNodeName(item.Node), item.Width, item.Height)
				if item.Type == InlineItemText {
					fmt.Printf("       Text: %q\n", truncateString(item.Text, 30))
				}
			}
			fmt.Printf("=== END PHASE 1 ===\n\n")
		}

		// Phase 2: Break lines (PURE - no side effects!)
		// Use original constraint - floats will be added in Phase 3
		lines := le.BreakLines(items, originalConstraint, startY)

		// Phase 3: Construct fragments (HAS side effects - creates fragments)
		// Start from original constraint and build up float exclusions
		fragments, finalConstraint := le.ConstructFragments(lines, originalConstraint)

		// Check if we need to retry
		if !constraintsChanged(originalConstraint, finalConstraint, lines) {
			// Success! Constraints didn't change, we're done
			finalFragments = fragments
			break
		}

		// Constraints changed - this means Phase 3 added floats
		// For now, we don't actually retry - we just return what we have
		// The retry logic isn't needed for simple float cases
		// TODO: Implement proper retry when float changes affect line breaking
		finalFragments = fragments
		break
	}

	return finalFragments
}

// collectInlineItemsClean is a clean version of CollectInlineItems that works
// with the new architecture (ConstraintSpace instead of InlineLayoutState).
//
// This is a simplified placeholder until we fully refactor CollectInlineItems.
// For now, it creates a minimal InlineLayoutState and delegates to the existing
// CollectInlineItems function.
func (le *LayoutEngine) collectInlineItemsClean(
	children []*html.Node,
	constraint *ConstraintSpace,
) []*InlineItem {
	// Create a temporary state for collecting items
	// This is a bridge between old and new architectures
	state := &InlineLayoutState{
		Items:          []*InlineItem{},
		AvailableWidth: constraint.AvailableSize.Width,
		ContainerStyle: css.NewStyle(),
	}

	// Compute styles for all children
	computedStyles := make(map[*html.Node]*css.Style)
	for _, child := range children {
		computedStyles[child] = css.ComputeStyle(
			child,
			le.stylesheets,
			le.viewport.width,
			le.viewport.height,
		)
	}

	// Collect items using existing function
	for _, child := range children {
		le.CollectInlineItems(child, state, computedStyles)
	}

	return state.Items
}

// constructLine creates positioned fragments for a single line.
// This is the core of Phase 3 - it creates fragments with CORRECT positions
// from the start (no repositioning needed).
//
// For each item on the line:
// - Text: Create text fragment at current X
// - Float: Position float, create fragment, update constraint
// - Atomic: Create atomic fragment at current X
// - Tags: Skip (markers only)
//
// Returns:
// - fragments: List of positioned fragments for this line
// - newConstraint: Updated constraint with floats added
func (le *LayoutEngine) constructLine(
	line *LineInfo,
	constraint *ConstraintSpace,
) ([]*Fragment, *ConstraintSpace) {
	fragments := []*Fragment{}
	currentConstraint := constraint

	// CRITICAL: Process floats FIRST before inline content
	// Floats affect the positioning of subsequent inline content, even if they
	// appear later in document order. This is per CSS spec: floats are removed
	// from flow and positioned first.
	//
	// Pass 1: Position all floats and update constraint
	for _, item := range line.Items {
		if item.Type == InlineItemFloat {
			floatFrag, newConstraint := le.positionFloat(
				item,
				line.Y,
				currentConstraint,
			)
			fragments = append(fragments, floatFrag)
			currentConstraint = newConstraint
		}
	}

	// Calculate starting X position accounting for floats (now updated)
	leftOffset, _ := currentConstraint.ExclusionSpace.AvailableInlineSize(line.Y, line.Height)
	currentX := leftOffset

	// Pass 2: Process inline content with floats already positioned
	for _, item := range line.Items {
		switch item.Type {
		case InlineItemText:
			// Create text fragment with correct position
			frag := NewTextFragment(
				item.Text,
				item.Style,
				currentX,
				line.Y,
				item.Width,
				item.Height,
				item.Node, // Pass the text node for rendering
			)
			fragments = append(fragments, frag)
			currentX += item.Width

		case InlineItemFloat:
			// Skip - floats are already handled in Pass 1
			continue

		case InlineItemAtomic:
			// Create atomic fragment (inline-block, replaced element)
			frag := &Fragment{
				Type:     FragmentAtomic,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: item.Width, Height: item.Height},
			}
			fragments = append(fragments, frag)
			currentX += item.Width

		case InlineItemOpenTag:
			// Opening tag marker - create inline fragment marker
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)
			// Tags don't advance X

		case InlineItemCloseTag:
			// Closing tag marker
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)
			// Tags don't advance X

		case InlineItemControl:
			// Control item (br, etc.) - just marker
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)

		case InlineItemBlockChild:
			// Block child - create marker fragment
			// Actual layout will happen in LayoutInlineContentToBoxes which has
			// access to all the context needed for layoutNode()
			frag := &Fragment{
				Type:     FragmentBlockChild,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: leftOffset, Y: line.Y}, // Start at left edge
				Size:     Size{Width: 0, Height: 0},          // Will be set after layout
			}
			fragments = append(fragments, frag)
		}
	}

	return fragments, currentConstraint
}

// positionFloat positions a float and creates its fragment.
// This also updates the constraint space with the new float exclusion.
//
// Key principle: Fragment is created with CORRECT position from the start.
// No repositioning or deltas needed.

// ConstructFragments creates positioned fragments from line breaking results.
// This is Phase 3 of the multi-pass inline layout pipeline.
//
// For each line:
// 1. Call constructLine to create fragments
// 2. Propagate constraint updates (floats) to next line
// 3. Accumulate all fragments
//
// Returns:
// - fragments: All positioned fragments (flattened from all lines)
// - finalConstraint: Constraint space after all floats added
//
// This function HAS side effects (creates fragments), but the constraint
// space propagation is clean (immutable updates via WithExclusion).
func (le *LayoutEngine) ConstructFragments(
	lines []*LineInfo,
	constraint *ConstraintSpace,
) ([]*Fragment, *ConstraintSpace) {
	allFragments := []*Fragment{}
	currentConstraint := constraint

	for _, line := range lines {
		// Construct fragments for this line using current constraint
		lineFragments, newConstraint := le.constructLine(line, currentConstraint)

		// Add fragments to result
		allFragments = append(allFragments, lineFragments...)

		// Propagate constraint to next line
		// This ensures floats added on this line affect subsequent lines
		currentConstraint = newConstraint
	}

	return allFragments, currentConstraint
}

// constraintsChanged checks if the constraint space changed during fragment construction.
// This is used to determine if we need to retry line breaking.
//
// Returns true if:
// - Floats were added (exclusion space changed)
// - Any other constraints changed (future extensions)
//
// This is the key to the retry logic: if Phase 3 added floats that affect
// line breaking, we need to re-run Phase 2 with the updated constraints.

// fragmentsToBoxes converts Fragment tree back to Box tree for existing rendering pipeline.
// This is a TEMPORARY BRIDGE until we migrate the entire pipeline to use fragments.
//
// For now, this allows us to use the new multi-pass architecture while keeping
// the existing rendering code working.
func fragmentsToBoxes(fragments []*Fragment) []*Box {
	boxes := []*Box{}

	for _, frag := range fragments {
		// Skip tag markers (they don't produce visual output)
		if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
			continue
		}

		// Create box from fragment
		box := &Box{
			Node:   frag.Node,
			Style:  frag.Style,
			X:      frag.Position.X,
			Y:      frag.Position.Y,
			Width:  frag.Size.Width,
			Height: frag.Size.Height,
		}

		// Convert fragment type to box positioning info
		switch frag.Type {
		case FragmentFloat:
			// Mark as positioned (floats are out of flow)
			box.Position = css.PositionAbsolute // Treated like absolute for rendering
		}

		boxes = append(boxes, box)
	}

	return boxes
}

// Helper functions for debug logging



// fragmentToBoxSingle converts a single fragment to a box.
// Helper for LayoutInlineContentToBoxes when processing fragments individually.
func fragmentToBoxSingle(frag *Fragment) *Box {
	// Skip tag markers (they don't produce visual output)
	if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
		return nil
	}

	// Create box from fragment
	box := &Box{
		Node:   frag.Node,
		Style:  frag.Style,
		X:      frag.Position.X,
		Y:      frag.Position.Y,
		Width:  frag.Size.Width,
		Height: frag.Size.Height,
	}

	// Convert fragment type to box positioning info
	switch frag.Type {
	case FragmentFloat:
		// Mark as positioned (floats are out of flow)
		box.Position = css.PositionAbsolute // Treated like absolute for rendering
	}

	return box
}

// LayoutInlineContentToBoxes is a convenience wrapper that runs the new multi-pass
// pipeline and converts the result to boxes for the existing rendering pipeline.
//
// This allows gradual migration: call this instead of the old inline layout,
// and the rest of the pipeline keeps working.
var multipassCallID int

func (le *LayoutEngine) LayoutInlineContentToBoxes(
	children []*html.Node,
	containerBox *Box,
	availableWidth float64,
	startY float64,
	computedStyles map[*html.Node]*css.Style,
) *InlineLayoutResult {
	multipassCallID++
	callID := multipassCallID
	// DEBUG: Log multi-pass invocation
	fmt.Printf("\n=== MULTI-PASS [%d]: LayoutInlineContentToBoxes ===\n", callID)
	fmt.Printf("Container: %s, StartY: %.1f, AvailableWidth: %.1f\n",
		getNodeName(containerBox.Node), startY, availableWidth)
	fmt.Printf("Children count: %d\n", len(children))

	// Create constraint space
	constraint := NewConstraintSpace(availableWidth, 0)

	// Run new multi-pass pipeline
	fragments := le.LayoutInlineContent(children, constraint, startY)

	fmt.Printf("Fragments created: %d\n", len(fragments))

	// Process fragments, handling block children with recursive layout
	boxes := []*Box{}
	currentY := startY
	currentLineY := startY     // Track which line we're on
	currentLineMaxHeight := 0.0 // Track maximum height on current line
	currentX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left // Track rightmost X position

	// Track inline element spans for creating wrapper boxes
	type inlineSpan struct {
		node     *html.Node
		style    *css.Style
		startX   float64
		startY   float64
		startIdx int // Fragment index where span started
	}
	inlineStack := []*inlineSpan{}

	// Track which nodes we've seen to distinguish OpenTag from CloseTag
	// First FragmentInline for a node = OpenTag, second = CloseTag
	seenNodes := make(map[*html.Node]bool)

	for i, frag := range fragments {
		if frag.Type == FragmentBlockChild {
			// Block child - first finalize the current line before laying out the block
			// Advance currentY past any content on the current line
			if currentLineMaxHeight > 0 {
				fmt.Printf("  Finalizing current line before block: currentY %.1f, height %.1f\n",
					currentY, currentLineMaxHeight)
				currentY = currentY + currentLineMaxHeight
				currentLineMaxHeight = 0
			}

			// Block child - call layoutNode recursively
			childNode := frag.Node
			childStyle := computedStyles[childNode]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}

			fmt.Printf("\n[Call %d][Fragment %d] BlockChild: %s\n", callID, i, getNodeName(childNode))
			fmt.Printf("  Fragment Y: %.1f, CurrentY (after line finalize): %.1f\n", frag.Position.Y, currentY)

			// Calculate X position (block children start at left edge)
			childX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
			fmt.Printf("  Calculated childX: %.1f (container.X=%.1f + border=%.1f + padding=%.1f)\n",
				childX, containerBox.X, containerBox.Border.Left, containerBox.Padding.Left)

			// Recursively layout the block child
			fmt.Printf("  Calling layoutNode(x=%.1f, y=%.1f, availWidth=%.1f)...\n",
				childX, currentY, availableWidth)
			childBox := le.layoutNode(
				childNode,
				childX,
				currentY,
				availableWidth,
				computedStyles,
				containerBox,
			)

			fmt.Printf("  Result: Box at (%.1f, %.1f) size %.1fx%.1f\n",
				childBox.X, childBox.Y, childBox.Width, childBox.Height)
			fmt.Printf("  Margins: T=%.1f R=%.1f B=%.1f L=%.1f\n",
				childBox.Margin.Top, childBox.Margin.Right, childBox.Margin.Bottom, childBox.Margin.Left)

			boxes = append(boxes, childBox)

			// Update Y for next content (advance past this block)
			childBox.Parent = containerBox
			totalHeight := childBox.Margin.Top + childBox.Border.Top + childBox.Padding.Top +
				childBox.Height + childBox.Padding.Bottom + childBox.Border.Bottom + childBox.Margin.Bottom
			fmt.Printf("  [Fragment %d] TotalHeight: %.1f, Advancing currentY: %.1f → %.1f\n",
				i, totalHeight, currentY, currentY+totalHeight)
			// CRITICAL: Only advance Y for elements in normal flow
			// Absolutely positioned and fixed positioned elements are removed from flow
			floatType := css.FloatNone
			if childBox.Style != nil {
				floatType = childBox.Style.GetFloat()
			}

			if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && floatType == css.FloatNone {
				// Child is in normal flow - advance Y
				currentY += totalHeight
				currentLineY = currentY // Update line Y to match
				currentLineMaxHeight = 0 // Reset for next line

				// Reset currentX - block child takes full width, next content starts at left
				currentX = containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
				fmt.Printf("  Reset currentX to left edge: %.1f, currentLineY updated to %.1f\n", currentX, currentLineY)
			} else {
				// Child is out of flow - don't advance Y
				fmt.Printf("  [Fragment %d] Out-of-flow element (Position=%v, Float=%v), NOT advancing currentY (stays at %.1f)\n",
					i, childBox.Position, floatType, currentY)
			}
		} else if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
			// Inline element marker (OpenTag or CloseTag)
			// Distinguish by checking if we've seen this node before
			isOpenTag := !seenNodes[frag.Node]

			if isOpenTag {
				// OpenTag - push to stack and record fragment index
				// CRITICAL: Use frag.Position.X not currentX - fragments are pre-positioned
				// accounting for floats by line breaking phase
				fmt.Printf("\n[Fragment %d] OpenTag: %s\n", i, getNodeName(frag.Node))
				fmt.Printf("  Position: (%.1f, %.1f), CurrentX: %.1f\n",
					frag.Position.X, frag.Position.Y, currentX)

				span := &inlineSpan{
					node:     frag.Node,
					style:    frag.Style,
					startX:   frag.Position.X, // Use fragment position, not currentX
					startY:   currentY,
					startIdx: i,
				}
				inlineStack = append(inlineStack, span)
				seenNodes[frag.Node] = true
			} else {
				// CloseTag - pop from stack and create wrapper box
				fmt.Printf("\n[Call %d][Fragment %d] CloseTag: %s (currentY=%.1f)\n", callID, i, getNodeName(frag.Node), currentY)
				fmt.Printf("  Position: (%.1f, %.1f), CurrentX: %.1f\n",
					frag.Position.X, frag.Position.Y, currentX)

				if len(inlineStack) > 0 {
					// Find matching span on stack (should be top for well-formed HTML)
					var span *inlineSpan
					spanIdx := -1
					for idx := len(inlineStack) - 1; idx >= 0; idx-- {
						if inlineStack[idx].node == frag.Node {
							span = inlineStack[idx]
							spanIdx = idx
							break
						}
					}

					if span != nil {
						// Check if this inline element was split by a block child
						hasBlockChild := false
						blockChildIdx := -1
						for j := span.startIdx; j < i; j++ {
							if fragments[j].Type == FragmentBlockChild {
								hasBlockChild = true
								blockChildIdx = j
								break
							}
						}

						if hasBlockChild {
							// Block-in-inline: Create fragment boxes (CSS 2.1 §9.2.1.1)
							fmt.Printf("  Block-in-inline detected! Creating fragment boxes for <%s> (container=<%s>)\n",
								getNodeName(span.node), getNodeName(containerBox.Node))

							// Check if there's content before the block
							hasContentBefore := false
							contentBeforeMaxX := span.startX
							for j := span.startIdx + 1; j < blockChildIdx; j++ {
								if fragments[j].Type == FragmentText || fragments[j].Type == FragmentAtomic {
									hasContentBefore = true
									fragEndX := fragments[j].Position.X + fragments[j].Size.Width
									if fragEndX > contentBeforeMaxX {
										contentBeforeMaxX = fragEndX
									}
								}
							}

							// Fragment 1: Content before block (if any)
							fmt.Printf("    hasContentBefore=%v, span.startX=%.1f, contentBeforeMaxX=%.1f\n",
								hasContentBefore, span.startX, contentBeforeMaxX)
							if hasContentBefore {
								// Compute border, padding, margin from style
								border := span.style.GetBorderWidth()
								padding := span.style.GetPadding()
								margin := span.style.GetMargin()
								lineHeight := span.style.GetLineHeight()

								fragment1 := &Box{
									Node:            span.node,
									Style:           span.style,
									X:               span.startX,
									Y:               span.startY,
									Width:           contentBeforeMaxX - span.startX,
									Height:          lineHeight, // Use line-height, not text height
									Border:          border,
									Padding:         padding,
									Margin:          margin,
									Parent:          containerBox,
									IsFirstFragment: true,  // First fragment has left border
									IsLastFragment:  false, // Not last
								}
								boxes = append(boxes, fragment1)
								fmt.Printf("    Fragment 1 (first): X=%.1f Y=%.1f W=%.1f H=%.1f (line-height=%.1f) Border=%.1f/%.1f/%.1f/%.1f\n",
									fragment1.X, fragment1.Y, fragment1.Width, fragment1.Height, lineHeight,
									border.Top, border.Right, border.Bottom, border.Left)
							}

							// Fragment 2: Content after block (if any)
							endX := frag.Position.X
							fmt.Printf("    Checking Fragment 2: endX=%.1f, span.startX=%.1f, currentY=%.1f, currentLineY=%.1f, frag.Position.Y=%.1f\n",
								endX, span.startX, currentY, currentLineY, frag.Position.Y)
							if endX > span.startX {
								// Use currentY which is correctly updated after block child layout
								afterBlockY := currentY
								fmt.Printf("    Creating Fragment 2 at Y=%.1f (using currentY)\n", afterBlockY)

								// Compute border, padding, margin from style
								border := span.style.GetBorderWidth()
								padding := span.style.GetPadding()
								margin := span.style.GetMargin()
								lineHeight := span.style.GetLineHeight()

								fragment2 := &Box{
									Node:            span.node,
									Style:           span.style,
									X:               containerBox.X + containerBox.Border.Left + containerBox.Padding.Left,
									Y:               afterBlockY,
									Width:           endX - (containerBox.X + containerBox.Border.Left + containerBox.Padding.Left),
									Height:          lineHeight, // Use line-height, not text height
									Border:          border,
									Padding:         padding,
									Margin:          margin,
									Parent:          containerBox,
									IsFirstFragment: false, // Not first
									IsLastFragment:  true,  // Last fragment has right border
								}
								boxes = append(boxes, fragment2)
								fmt.Printf("    Fragment 2 (last): X=%.1f Y=%.1f W=%.1f H=%.1f (line-height=%.1f) Border=%.1f/%.1f/%.1f/%.1f\n",
									fragment2.X, fragment2.Y, fragment2.Width, fragment2.Height, lineHeight,
									border.Top, border.Right, border.Bottom, border.Left)
							}
						} else {
							// Normal inline box (not split)
							endX := frag.Position.X
							wrapperWidth := endX - span.startX

							// Compute border, padding, margin from style
							border := span.style.GetBorderWidth()
							padding := span.style.GetPadding()
							margin := span.style.GetMargin()

							// Inline elements ignore vertical margins (CSS 2.1 §8.3)
							margin.Top = 0
							margin.Bottom = 0

							// CRITICAL FIX: Empty inline elements (no content between OpenTag and CloseTag)
							// must still have dimensions from border and padding (CSS 2.1 §10.3.1)
							// Example: <span style="border:25px; padding:100px"></span>
							// Should render as 250px wide (25+100+0+100+25) even with no content

							// Check if inline is truly empty (no text/atomic content between OpenTag and CloseTag)
							isEmpty := true
							for j := span.startIdx + 1; j < i; j++ {
								if fragments[j].Type == FragmentText || fragments[j].Type == FragmentAtomic {
									isEmpty = false
									break
								}
							}

							if isEmpty {
								// Empty inline: width = full horizontal border + padding (no content)
								wrapperWidth = border.Left + padding.Left + padding.Right + border.Right
								fmt.Printf("  Empty inline element - using border+padding: width %.1f\n", wrapperWidth)
							}

							// Calculate height from line-height or font-size
							// Empty inline elements establish line box height per CSS 2.1 §10.8.1
							wrapperHeight := currentLineMaxHeight
							if wrapperHeight == 0 {
								// Use font-size as minimum height for empty inline elements
								fontSize := span.style.GetFontSize()
								if lineHeightValue, ok := span.style.Get("line-height"); ok && lineHeightValue != "normal" && lineHeightValue != "" {
									// Handle relative units (em, %) relative to font-size
									if strings.HasSuffix(lineHeightValue, "em") {
										// Parse the number before "em"
										numStr := strings.TrimSuffix(lineHeightValue, "em")
										if multiplier, err := strconv.ParseFloat(numStr, 64); err == nil {
											wrapperHeight = fontSize * multiplier
										} else {
											wrapperHeight = fontSize // Fallback
										}
									} else if strings.HasSuffix(lineHeightValue, "%") {
										// Parse percentage
										numStr := strings.TrimSuffix(lineHeightValue, "%")
										if pct, err := strconv.ParseFloat(numStr, 64); err == nil {
											wrapperHeight = fontSize * (pct / 100.0)
										} else {
											wrapperHeight = fontSize // Fallback
										}
									} else if parsedValue, parseOk := css.ParseLength(lineHeightValue); parseOk {
										// Absolute units (px, pt, etc.)
										wrapperHeight = parsedValue
									} else {
										wrapperHeight = fontSize // Fallback to font-size
									}
								} else {
									wrapperHeight = fontSize // Default: font-size
								}
							}

							// Box height is the line box height (CSS 2.1 §10.8.1)
							// Borders/padding "bleed" outside this and are drawn separately by the render phase
							// wrapperHeight already equals currentLineMaxHeight (line box height)
							fmt.Printf("  Wrapper box height: line-height %.1f (borders/padding rendered separately)\n",
								wrapperHeight)

							fmt.Printf("  Creating wrapper box: X %.1f → %.1f (width %.1f, height %.1f)\n",
								span.startX, endX, wrapperWidth, wrapperHeight)

						// Convert from content-relative to absolute coordinates
						// Fragment positions are relative to container's content area
						// (after border+padding), so add container's offset
						baseX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
						// baseY :=  // Y coordinates are already absolute, not needed containerBox.Y + containerBox.Border.Top + containerBox.Padding.Top

							wrapperBox := &Box{
								Node:    span.node,
								Style:   span.style,
								X:       baseX + span.startX + margin.Left,  // Apply left margin
								Y:       span.startY + margin.Top,   // Apply top margin
								Width:   wrapperWidth,
								Height:  wrapperHeight,
								Border:  border,
								Padding: padding,
								Margin:  margin,
								Parent:  containerBox,
							}
							boxes = append(boxes, wrapperBox)
						}

						// Remove span from stack
						inlineStack = append(inlineStack[:spanIdx], inlineStack[spanIdx+1:]...)
						fmt.Printf("  Wrapper box(es) created, stack size: %d\n", len(inlineStack))
					} else {
						fmt.Printf("  ⚠️  WARNING: CloseTag without matching OpenTag!\n")
					}
				}
			}
		} else if frag.Type == FragmentFloat {
			// Float - recursively layout its contents like a block child
			floatNode := frag.Node
			floatStyle := computedStyles[floatNode]
			if floatStyle == nil {
				floatStyle = css.NewStyle()
			}

			fmt.Printf("\n[Fragment %d] Float: %s\n", i, getNodeName(floatNode))
			fmt.Printf("  Fragment Position: (%.1f, %.1f), Size: %.1fx%.1f\n",
				frag.Position.X, frag.Position.Y, frag.Size.Width, frag.Size.Height)

			// Recursively layout the float's content
			// Floats are positioned absolutely, use fragment position
			fmt.Printf("  Calling layoutNode for float content...\n")
			floatBox := le.layoutNode(
				floatNode,
				frag.Position.X,
				currentY,
				frag.Size.Width, // Use float's explicit width
				computedStyles,
				containerBox,
			)

			fmt.Printf("  Float box at (%.1f, %.1f) size %.1fx%.1f\n",
				floatBox.X, floatBox.Y, floatBox.Width, floatBox.Height)

			// Mark as floated for rendering
			floatBox.Position = css.PositionAbsolute
			floatBox.Parent = containerBox
			boxes = append(boxes, floatBox)

			// Floats don't affect currentY or currentX for inline flow
			// (they're out of flow)
		} else {
			// Regular fragment - convert to box
			box := fragmentToBoxSingle(frag)
			if box != nil {
				fmt.Printf("\n[Fragment %d] %v: %s\n", i, frag.Type, getNodeName(frag.Node))
				fmt.Printf("  Fragment Position: (%.1f, %.1f), Size: %.1fx%.1f\n",
					frag.Position.X, frag.Position.Y, frag.Size.Width, frag.Size.Height)

				// Check if we've moved to a new line (Y changed)
				if frag.Position.Y != currentLineY {
					// Advance currentY past the previous line
					if currentLineMaxHeight > 0 {
						fmt.Printf("  Line break detected: Y %.1f → %.1f (height %.1f)\n",
							currentLineY, frag.Position.Y, currentLineMaxHeight)
						currentY = currentLineY + currentLineMaxHeight
					}
					currentLineY = frag.Position.Y
					currentLineMaxHeight = 0
				}

				// CRITICAL FIX: Use currentY instead of frag.Position.Y
				// After block children, frag.Position.Y is wrong because BreakLines
				// doesn't know block heights. We track actual Y in currentY.
				if box.Y != currentY {
					fmt.Printf("  ⚠️  Correcting Y: %.1f → %.1f (currentY)\n", box.Y, currentY)
					box.Y = currentY
				}

				// Track maximum height on this line
				if box.Height > currentLineMaxHeight {
					currentLineMaxHeight = box.Height
				}

				if frag.Type == FragmentText {
					fmt.Printf("  Text: %q\n", truncateString(frag.Text, 30))
				}
				fmt.Printf("  Final Box Position: (%.1f, %.1f), currentLineMaxH: %.1f\n",
					box.X, box.Y, currentLineMaxHeight)

				// Update currentX to track rightmost position
				boxRight := box.X + box.Width
				if boxRight > currentX {
					currentX = boxRight
					fmt.Printf("  Updated currentX: %.1f\n", currentX)
				}

				box.Parent = containerBox
				boxes = append(boxes, box)
			}
		}
	}

	fmt.Printf("\nTotal boxes created: %d\n", len(boxes))

	// Apply text-align to inline children
	if containerBox.Style != nil {
		display := containerBox.Style.GetDisplay()
		if display != css.DisplayInline && display != css.DisplayInlineBlock {
			if textAlign, ok := containerBox.Style.Get("text-align"); ok && textAlign != "left" && textAlign != "" {
				contentWidth := containerBox.Width // containerBox.Width is already the content width
				fmt.Printf("DEBUG: Applying text-align=%s to %d boxes\n", textAlign, len(boxes))
				le.applyTextAlignToBoxes(boxes, containerBox, textAlign, contentWidth)
			}
		}
	}

	fmt.Printf("=== END MULTI-PASS ===\n\n")

	// Create inline context for auto-height calculation
	// Track the final line Y and line height so parent can calculate its height
	finalInlineCtx := &InlineContext{
		LineX:      0,                    // Not needed for height calculation
		LineY:      currentY,             // Final Y position after all content
		LineHeight: currentLineMaxHeight, // Height of the last line
		LineBoxes:  boxes,                // All created boxes
	}

	fmt.Printf("DEBUG: Returning InlineLayoutResult: currentY=%.1f, currentLineMaxHeight=%.1f, boxes=%d\n",
		currentY, currentLineMaxHeight, len(boxes))

	return &InlineLayoutResult{
		ChildBoxes:     boxes,
		FinalInlineCtx: finalInlineCtx,
		UsedMultiPass:  true,
	}
}
func (le *LayoutEngine) layoutInlineContentWIP(
	node *html.Node,
	box *Box,
	availableWidth float64,
	startY float64,
	border, padding css.BoxEdge,
	computedStyles map[*html.Node]*css.Style,
) []*Box {
	// Initialize state
	state := &InlineLayoutState{
		Items:          []*InlineItem{},
		Lines:          []*LineBreakResult{},
		ContainerBox:   box,
		ContainerStyle: box.Style,
		AvailableWidth: availableWidth,
		StartY:         startY,
		Border:         border,
		Padding:        padding,
		FloatList:      []FloatInfo{},
		FloatBaseIndex: le.floatBase,
	}

	// Phase 1: Collect inline items
	for _, child := range node.Children {
		le.CollectInlineItems(child, state, computedStyles)
	}

	// DEBUG: Show collected items
	fmt.Printf("\n=== PHASE 1: Collected %d items ===\n", len(state.Items))
	for i, item := range state.Items {
		typeName := ""
		switch item.Type {
		case InlineItemText:
			typeName = "Text"
		case InlineItemOpenTag:
			typeName = "OpenTag"
		case InlineItemCloseTag:
			typeName = "CloseTag"
		case InlineItemFloat:
			typeName = "Float"
		case InlineItemAtomic:
			typeName = "Atomic"
		case InlineItemBlockChild:
			typeName = "BlockChild"
		default:
			typeName = fmt.Sprintf("Type%d", item.Type)
		}
		fmt.Printf("  [%d] %s: %s, Size: %.1fx%.1f\n",
			i, typeName, getNodeName(item.Node), item.Width, item.Height)
		if item.Type == InlineItemText {
			fmt.Printf("       Text: %q\n", truncateString(item.Text, 30))
		}
	}
	fmt.Printf("=== END PHASE 1 ===\n\n")

	// Phase 2 & 3: Line breaking with retry when floats change available width
	// This implements the Gecko-style retry mechanism (RedoMoreFloats)
	const maxRetries = 3 // Prevent infinite loops
	var boxes []*Box

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 2: Break into lines with current float state
		success := le.breakLinesWIP(state)
		if !success {
			return []*Box{} // Line breaking failed
		}

		// Phase 3: Construct line boxes and layout floats
		// Returns boxes and whether retry is needed
		boxes, retryNeeded := le.constructLineBoxesWithRetry(state, box, computedStyles)

		if !retryNeeded {
			// Success - no floats changed available width
			return boxes
		}

		// Retry needed - a float changed available width
		// Phase 2 will be re-run with updated float list on next iteration
	}

	// Max retries exceeded - return what we have
	return boxes
}

// Phase 1: CollectInlineItems flattens the DOM tree into a sequential list of inline items.
// This converts the hierarchical structure into a flat array that's easier to process for line breaking.
//
// Example:
//
//	<p>Hello <em>world</em>!</p>
//
// Becomes:
//
//	[Text("Hello "), OpenTag(<em>), Text("world"), CloseTag(</em>), Text("!")]
func (le *LayoutEngine) CollectInlineItems(node *html.Node, state *InlineLayoutState, computedStyles map[*html.Node]*css.Style) {
	if node == nil {
		return
	}

	// Handle text nodes
	if node.Type == html.TextNode {
		if node.Text == "" {
			return
		}

		// Get parent style for text measurements
		parentStyle := state.ContainerStyle
		if node.Parent != nil {
			if style := computedStyles[node.Parent]; style != nil {
				parentStyle = style
			}
		}

		// Check for ::first-letter pseudo-element styling
		// This applies to the first letter of the first text in a block container
		shouldApplyFirstLetter := false
		if node.Parent != nil && len(state.Items) == 0 {
			// This is the first text in the inline batch
			// Check if there are any :first-letter rules for the parent
			for _, stylesheet := range le.stylesheets {
				for _, rule := range stylesheet.Rules {
					if rule.Selector.PseudoElement == "first-letter" {
						if css.MatchesSelector(node.Parent, rule.Selector) {
							shouldApplyFirstLetter = true
							break
						}
					}
				}
				if shouldApplyFirstLetter {
					break
				}
			}
		}

		if shouldApplyFirstLetter {
			// Get the computed first-letter style
			firstLetterStyle := css.ComputePseudoElementStyle(node.Parent, "first-letter", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			firstLetter, remaining := extractFirstLetter(node.Text)

			if firstLetter != "" {
				// Create item for the first letter with special styling
				flFontSize := firstLetterStyle.GetFontSize()
				flBold := firstLetterStyle.GetFontWeight() == css.FontWeightBold
				flWidth, flHeight := text.MeasureTextWithWeight(firstLetter, flFontSize, flBold)

				firstLetterItem := &InlineItem{
					Type:        InlineItemText,
					Node:        node,
					Text:        firstLetter,
					StartOffset: 0,
					EndOffset:   len(firstLetter),
					Style:       firstLetterStyle,
					Width:       flWidth,
					Height:      flHeight,
				}
				state.Items = append(state.Items, firstLetterItem)

				// If there's remaining text, create an item for it
				if remaining != "" {
					fontSize := parentStyle.GetFontSize()
					bold := parentStyle.GetFontWeight() == css.FontWeightBold
					width, height := text.MeasureTextWithWeight(remaining, fontSize, bold)

					remainingItem := &InlineItem{
						Type:        InlineItemText,
						Node:        node,
						Text:        remaining,
						StartOffset: len(firstLetter),
						EndOffset:   len(node.Text),
						Style:       parentStyle,
						Width:       width,
						Height:      height,
					}
					state.Items = append(state.Items, remainingItem)
				}
				return
			}
		}

		// Normal text without first-letter styling
		fontSize := parentStyle.GetFontSize()
		bold := parentStyle.GetFontWeight() == css.FontWeightBold
		width, height := text.MeasureTextWithWeight(node.Text, fontSize, bold)

		item := &InlineItem{
			Type:        InlineItemText,
			Node:        node,
			Text:        node.Text,
			StartOffset: 0,
			EndOffset:   len(node.Text),
			Style:       parentStyle,
			Width:       width,
			Height:      height,
		}
		state.Items = append(state.Items, item)
		return
	}

	// Handle element nodes
	if node.Type == html.ElementNode {
		style := computedStyles[node]
		if style == nil {
			style = css.NewStyle()
		}

		display := style.GetDisplay()

		// Skip display:none elements
		if display == css.DisplayNone {
			return
		}

		// Handle different display types
		switch display {
		case css.DisplayBlock, css.DisplayTable, css.DisplayListItem:
			// Block elements in inline contexts are handled as BlockChild items
			// They force line breaks before and after, and require recursive layout
			fmt.Printf("  [CollectItems] Found block child: %s (display=%v)\n",
				getNodeName(node), display)
			item := &InlineItem{
				Type:   InlineItemBlockChild,
				Node:   node,
				Style:  style,
				Width:  0, // Will be determined during recursive layout
				Height: 0, // Will be determined during recursive layout
			}
			state.Items = append(state.Items, item)
			return

		case css.DisplayInline:
			// Check for floats
			if style.GetFloat() != css.FloatNone {
				// Floated inline elements become atomic items
				// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
				// This is PURE - no side effects, no float pollution

				// Create a constraint space for sizing the float
				constraint := NewConstraintSpace(state.AvailableWidth, 0)

				// Compute dimensions WITHOUT laying out (no side effects!)
				sizes := le.ComputeMinMaxSizes(node, constraint, style)

				// For floats, use max content size (preferred width)
				// Height will be computed during actual layout in Phase 3
				width := sizes.MaxContentSize

				// Estimate height based on font size (will be accurate in Phase 3)
				// TODO: Make ComputeMinMaxSizes return height as well
				height := style.GetFontSize() * 1.2 // Rough estimate

				item := &InlineItem{
					Type:   InlineItemFloat,
					Node:   node,
					Style:  style,
					Width:  width,
					Height: height,
				}
				state.Items = append(state.Items, item)
				// Don't process children - they're part of the float box
				return
			}
		// Check if this inline element contains ONLY block-level children
		// Per CSS 2.1 §9.2.1.1: When an inline box contains a block box, the inline
		// is broken around the block. If the resulting anonymous inline boxes are empty
		// (no text, no inline content), they shouldn't create visible space.
		hasOnlyBlockChildren := true
		hasAnyChildren := false
		for _, child := range node.Children {
			hasAnyChildren = true
			// Text nodes with non-whitespace content count as inline
			if child.Type == html.TextNode && strings.TrimSpace(child.Text) != "" {
				hasOnlyBlockChildren = false
				break
			}
			// Element nodes need style check
			if child.Type == html.ElementNode {
				childStyle := computedStyles[child]
				if childStyle != nil {
					childDisplay := childStyle.GetDisplay()
					// Block-level displays don't break the pattern
					if childDisplay != css.DisplayBlock && childDisplay != css.DisplayTable && childDisplay != css.DisplayListItem {
						hasOnlyBlockChildren = false
						break
					}
				}
			}
		}

		// If inline contains only block children, skip OpenTag/CloseTag to avoid empty inline boxes
		if hasAnyChildren && hasOnlyBlockChildren {
			// Just process children directly without creating inline box fragments
			for _, child := range node.Children {
				le.CollectInlineItems(child, state, computedStyles)
			}
			return
		}


			// Regular inline element - add open tag
			openItem := &InlineItem{
				Type:  InlineItemOpenTag,
				Node:  node,
				Style: style,
			}
			state.Items = append(state.Items, openItem)

			// Process children recursively
			for _, child := range node.Children {
				le.CollectInlineItems(child, state, computedStyles)
			}

			// Add close tag
			closeItem := &InlineItem{
				Type:  InlineItemCloseTag,
				Node:  node,
				Style: style,
			}
			state.Items = append(state.Items, closeItem)

		case css.DisplayInlineBlock:
			// Atomic inline element
			// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
			// This is PURE - no side effects

			// Create a constraint space for sizing the inline-block
			constraint := NewConstraintSpace(state.AvailableWidth, 0)

			// Compute dimensions WITHOUT laying out (no side effects!)
			sizes := le.ComputeMinMaxSizes(node, constraint, style)

			// For inline-blocks, use max content size (preferred width)
			width := sizes.MaxContentSize

			// Estimate height (will be accurate in Phase 3)
			height := style.GetFontSize() * 1.2 // Rough estimate

			item := &InlineItem{
				Type:   InlineItemAtomic,
				Node:   node,
				Style:  style,
				Width:  width,
				Height: height,
			}
			state.Items = append(state.Items, item)
			// Don't process children - they're part of the atomic box

		default:
			// Other display types - treat as atomic for now
			// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
			// This is PURE - no side effects

			// Create a constraint space for sizing
			constraint := NewConstraintSpace(state.AvailableWidth, 0)

			// Compute dimensions WITHOUT laying out (no side effects!)
			sizes := le.ComputeMinMaxSizes(node, constraint, style)

			// Use max content size (preferred width)
			width := sizes.MaxContentSize

			// Estimate height (will be accurate in Phase 3)
			height := style.GetFontSize() * 1.2 // Rough estimate

			item := &InlineItem{
				Type:   InlineItemAtomic,
				Node:   node,
				Style:  style,
				Width:  width,
				Height: height,
			}
			state.Items = append(state.Items, item)
		}
	}
}

// Phase 2: BreakLines determines what items go on each line, accounting for floats.
// This is where retry happens - if floats change available width, we re-break affected lines.
//
// Returns true if line breaking succeeded, false if retry is needed.
// NOTE: This is the OLD WIP implementation. New code should use BreakLines() instead.
func (le *LayoutEngine) breakLinesWIP(state *InlineLayoutState) bool {
	if len(state.Items) == 0 {
		return true // Nothing to break
	}

	state.Lines = nil // Clear any previous line breaking results
	currentY := state.StartY
	itemIndex := 0

	for itemIndex < len(state.Items) {
		// Start a new line
		line := &LineBreakResult{
			Y:          currentY,
			Items:      []*InlineItem{},
			StartIndex: itemIndex,
			TextBreaks: make(map[*InlineItem]struct {
				StartOffset int
				EndOffset   int
			}),
		}

		// Calculate available width for this line (accounting for floats)
		leftOffset, rightOffset := le.getFloatOffsets(currentY)
		line.AvailableWidth = state.AvailableWidth - leftOffset - rightOffset

		// Accumulate items on this line
		lineX := 0.0
		lineHeight := 0.0

		for itemIndex < len(state.Items) {
			item := state.Items[itemIndex]

			// Calculate item width
			itemWidth := 0.0
			itemHeight := 0.0

			switch item.Type {
			case InlineItemText:
				// For text, we might need to break it
				itemWidth = item.Width
				itemHeight = item.Height

				// Check if text fits on current line
				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Text doesn't fit - need to break
					// For now, simple algorithm: break entire text to next line
					// TODO: Implement proper word breaking within text
					goto finishLine
				}

			case InlineItemOpenTag:
				// Opening tag contributes to line height even if element is empty
				// This is per CSS 2.1: empty inline elements still influence line height
				itemWidth = 0

				// CSS 2.1 §10.8.1: For inline boxes, line box height is determined by 'line-height'
				// Padding and borders render visually but DON'T affect line box height calculation
				lineHeightValue := item.Style.GetLineHeight()
				itemHeight = lineHeightValue

			case InlineItemCloseTag:
				// Closing tag doesn't add height (already accounted for in opening tag)
				itemWidth = 0
				itemHeight = 0

			case InlineItemAtomic, InlineItemFloat:
				// Atomic items have their own width/height
				itemWidth = item.Width
				itemHeight = item.Height

				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Atomic item doesn't fit
					goto finishLine
				}

			case InlineItemBlockChild:
				// Block children force line breaks before and after
				// If we have items on current line, finish it first
				if len(line.Items) > 0 {
					goto finishLine
				}
				// Add block child as sole item on its own line
				line.Items = append(line.Items, item)
				itemIndex++
				goto finishLine

			case InlineItemControl:
				// Control items (like <br>) force a line break
				itemIndex++
				goto finishLine
			}

			// Add item to line
			line.Items = append(line.Items, item)
			lineX += itemWidth
			if itemHeight > lineHeight {
				lineHeight = itemHeight
			}

			itemIndex++
		}

	finishLine:
		// Finalize this line
		line.EndIndex = itemIndex
		line.LineHeight = lineHeight
		if line.LineHeight == 0 {
			// Use container's line-height as minimum
			line.LineHeight = state.ContainerStyle.GetLineHeight()
		}

		state.Lines = append(state.Lines, line)

		// Move to next line
		currentY += line.LineHeight

		// If we didn't make progress, force at least one item
		if itemIndex == line.StartIndex && itemIndex < len(state.Items) {
			// Force include at least one item to avoid infinite loop
			item := state.Items[itemIndex]
			line.Items = append(line.Items, item)
			line.EndIndex = itemIndex + 1
			itemIndex++
		}
	}

	return true // Line breaking succeeded
}

// Phase 3: ConstructLineBoxes creates actual positioned Box fragments from line breaking results.
// This is the final phase that produces the output fragment tree.
func (le *LayoutEngine) ConstructLineBoxes(state *InlineLayoutState, parent *Box) []*Box {
	boxes := []*Box{}

	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffset, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffset

		// Track open inline elements (for nested inline styling)
		type inlineContext struct {
			node               *html.Node
			style              *css.Style
			box                *Box
			fragmentStartX     float64  // Where current fragment starts
			fragmentStartY     float64
			fragmentMaxX       float64 // Bounding box of current fragment
			fragmentMaxY       float64
			completedFragments []*Box // Completed fragments (before blocks)
		}
		openInlines := []inlineContext{}

		// Reorder items: floats first, then everything else (CSS-correct)
		reorderedItems := make([]*InlineItem, 0, len(line.Items))
		nonFloats := make([]*InlineItem, 0, len(line.Items))

		for _, item := range line.Items {
			if item.Type == InlineItemFloat {
				reorderedItems = append(reorderedItems, item)
			} else {
				nonFloats = append(nonFloats, item)
			}
		}
		reorderedItems = append(reorderedItems, nonFloats...)

		// Process each item on this line
		for _, item := range reorderedItems {
			switch item.Type {
			case InlineItemText:
				// Create a text box
				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				fmt.Printf("DEBUG MP CONSTRUCT: Added text box with content=%q\n", item.Text)
				currentX += item.Width

				// Update fragment bounds for all open inline elements
				for i := range openInlines {
					if currentX > openInlines[i].fragmentMaxX {
						openInlines[i].fragmentMaxX = currentX
					}
					if line.Y+line.LineHeight > openInlines[i].fragmentMaxY {
						openInlines[i].fragmentMaxY = line.Y + line.LineHeight
					}
				}

			case InlineItemOpenTag:
				// Start tracking this inline element
				// Create a box for it (will be sized after seeing all children)
				padding := item.Style.GetPadding()
				border := item.Style.GetBorderWidth()
				margin := item.Style.GetMargin()

				// CSS 2.1 §10.8.1: Inline element vertical margins/padding don't affect line box height
				// but padding/borders DO render visually extending beyond the line box

				// Inline elements ignore vertical margins (CSS 2.1 §10.6.1)
				margin.Top = 0
				margin.Bottom = 0

				// Box height is the line box height (CSS 2.1 §10.8.1)
				// Borders/padding "bleed" outside this and are drawn separately by the render phase
				inlineBoxHeight := line.LineHeight

			// Apply left margin BEFORE positioning the box
			currentX += margin.Left

				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0, // Will be computed from children
					Height:   inlineBoxHeight,
					Margin:   margin, // Inline elements have horizontal margins
					Padding:  padding,
					Border:   border,
					Position: css.PositionStatic,
					Parent:   parent,
				}
				// Initialize fragment tracking
				fragStartX := currentX + border.Left + padding.Left
				openInlines = append(openInlines, inlineContext{
					node:           item.Node,
					style:          item.Style,
					box:            inlineBox,
					fragmentStartX: fragStartX,
					fragmentStartY: line.Y,
					fragmentMaxX:   fragStartX,
					fragmentMaxY:   line.Y + inlineBoxHeight,
				})

				// Advance currentX by left border + padding (margin already applied above)
				// This ensures empty inline elements have proper width
				currentX += border.Left + padding.Left

			case InlineItemCloseTag:
				// Close the most recent inline element
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]

					// Add right padding + border (NOT margin) before computing width
					currentX += ctx.box.Padding.Right + ctx.box.Border.Right

					// Compute width from current X - start X
					ctx.box.Width = currentX - ctx.box.X
					boxes = append(boxes, ctx.box)

				// Now add right margin for positioning next element
				currentX += ctx.box.Margin.Right
				}

			case InlineItemAtomic:
				// Atomic inline element - it has its own dimensions
				atomicBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, atomicBox)
				currentX += item.Width

			case InlineItemFloat:
				// Floats are positioned separately by float logic
				// We don't position them here
				// TODO: Integrate with existing float positioning
			}
		}
	}

	return boxes
}

// constructLineBoxesWithRetry is like ConstructLineBoxes but also detects when floats
// change available width and signals that retry is needed.
// Returns (boxes, retryNeeded)
func (le *LayoutEngine) constructLineBoxesWithRetry(
	state *InlineLayoutState,
	parent *Box,
	computedStyles map[*html.Node]*css.Style,
) ([]*Box, bool) {
	boxes := []*Box{}
	retryNeeded := false

	fmt.Printf("DEBUG MP CONSTRUCT: %d lines to construct\n", len(state.Lines))
	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffsetBefore, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffsetBefore

		// Track open inline elements
		type inlineContext struct {
			node               *html.Node
			style              *css.Style
			box                *Box
			fragmentStartX     float64  // Where current fragment starts
			fragmentStartY     float64
			fragmentMaxX       float64 // Bounding box of current fragment
			fragmentMaxY       float64
			completedFragments []*Box // Completed fragments (before blocks)
		}
		openInlines := []inlineContext{}

		// Reorder items: floats first, then everything else (CSS-correct)
		reorderedItems := make([]*InlineItem, 0, len(line.Items))
		nonFloats := make([]*InlineItem, 0, len(line.Items))

		for _, item := range line.Items {
			if item.Type == InlineItemFloat {
				reorderedItems = append(reorderedItems, item)
			} else {
				nonFloats = append(nonFloats, item)
			}
		}
		reorderedItems = append(reorderedItems, nonFloats...)

		// Process each item on this line
		// Track if we've seen content (non-float) on this line yet
		hasSeenContentOnLine := false
		for _, item := range reorderedItems {
			switch item.Type {
			case InlineItemText:
				// CSS whitespace collapsing: trim leading whitespace at start of line
				// (after line breaks, leading spaces should be trimmed)
				trimmedText := item.Text
				if !hasSeenContentOnLine && item.Node != nil {
					trimmedText = strings.TrimLeft(item.Text, " \t")
					// Update the node's text for rendering
					if trimmedText != item.Text {
						item.Node.Text = trimmedText
						// Recalculate width for trimmed text
						if item.Style != nil {
							fontSize := item.Style.GetFontSize()
							fontWeight := item.Style.GetFontWeight()
							bold := fontWeight == css.FontWeightBold
							trimmedWidth, _ := text.MeasureTextWithWeight(trimmedText, fontSize, bold)
							item.Width = trimmedWidth
						}
					}
				}
				hasSeenContentOnLine = true

				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				fmt.Printf("DEBUG MP CONSTRUCT: Added text box with content=%q\n", item.Text)
				currentX += item.Width

				// Update fragment bounds for all open inline elements
				for i := range openInlines {
					if currentX > openInlines[i].fragmentMaxX {
						openInlines[i].fragmentMaxX = currentX
					}
					if line.Y+line.LineHeight > openInlines[i].fragmentMaxY {
						openInlines[i].fragmentMaxY = line.Y + line.LineHeight
					}
				}

			case InlineItemOpenTag:
				padding := item.Style.GetPadding()
				border := item.Style.GetBorderWidth()
				margin := item.Style.GetMargin()

				// CSS 2.1 §10.8.1: Inline element vertical margins/padding don't affect line box height
				// but padding/borders DO render visually extending beyond the line box

				// Inline elements ignore vertical margins (CSS 2.1 §10.6.1)
				margin.Top = 0
				margin.Bottom = 0

				// Box height is the line box height (CSS 2.1 §10.8.1)
				// Borders/padding "bleed" outside this and are drawn separately by the render phase
				inlineBoxHeight := line.LineHeight

			// Apply left margin BEFORE positioning the box
			currentX += margin.Left

				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0,
					Height:   inlineBoxHeight,
					Margin:   margin, // Inline elements have horizontal margins
					Padding:  padding,
					Border:   border,
					Position: css.PositionStatic,
					Parent:   parent,
				}
				// Initialize fragment tracking
				fragStartX := currentX + border.Left + padding.Left
				openInlines = append(openInlines, inlineContext{
					node:           item.Node,
					style:          item.Style,
					box:            inlineBox,
					fragmentStartX: fragStartX,
					fragmentStartY: line.Y,
					fragmentMaxX:   fragStartX,
					fragmentMaxY:   line.Y + inlineBoxHeight,
				})

				// Advance currentX by left border + padding (margin already applied above)
				// This ensures empty inline elements have proper width
				currentX += border.Left + padding.Left

			case InlineItemCloseTag:
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]

					// Add right padding + border (NOT margin) before computing width
					currentX += ctx.box.Padding.Right + ctx.box.Border.Right

					ctx.box.Width = currentX - ctx.box.X
					boxes = append(boxes, ctx.box)

				// Now add right margin for positioning next element
				currentX += ctx.box.Margin.Right
				}

			case InlineItemAtomic:
				// Atomic inline (inline-block) - recursively layout its content
				// Use the pre-computed width as the available width for its children
				atomicBox := le.layoutNode(
					item.Node,
					currentX,
					line.Y,
					item.Width, // Use computed width as constraint
					computedStyles,
					parent,
				)
				if atomicBox != nil {
					// Apply vertical alignment to inline-block
					// For baseline alignment, the inline-block's baseline (last line box's baseline)
					// should align with the parent line's baseline
					le.applyVerticalAlign(atomicBox, line.Y, line.LineHeight)

					boxes = append(boxes, atomicBox)
					// Use actual width (might include margins/padding/borders)
					actualWidth := le.getTotalWidth(atomicBox)
					currentX += actualWidth
				}

			case InlineItemBlockChild:
				// Block-in-inline: Block children split inline elements into fragments (CSS 2.1 §9.2.1.1)
				fmt.Printf("DEBUG MP: Block child <%s> splits %d open inline elements\n", item.Node.TagName, len(openInlines))

				// STEP 1: Complete current fragments for ALL open inline elements
				for i := range openInlines {
					ctx := &openInlines[i]

					// Create fragment box for content before the block
					if ctx.fragmentMaxX > ctx.fragmentStartX {
						fragmentBox := &Box{
							Node:            ctx.node,
							Style:           ctx.style,
							X:               ctx.fragmentStartX - ctx.box.Border.Left - ctx.box.Padding.Left,
							Y:               ctx.fragmentStartY,
							Width:           ctx.fragmentMaxX - ctx.fragmentStartX + ctx.box.Border.Left + ctx.box.Border.Right + ctx.box.Padding.Left + ctx.box.Padding.Right,
							Height:          ctx.fragmentMaxY - ctx.fragmentStartY,
							Margin:          css.BoxEdge{}, // Fragments don't have margins
							Padding:         ctx.box.Padding,
							Border:          ctx.box.Border,
							Position:        css.PositionStatic,
							Parent:          parent,
							IsFirstFragment: len(ctx.completedFragments) == 0, // First fragment if no previous fragments
							IsLastFragment:  false,                            // Not last - more content after block
						}
						ctx.completedFragments = append(ctx.completedFragments, fragmentBox)
						fmt.Printf("  Completed fragment for <%s>: X=%.1f Y=%.1f W=%.1f H=%.1f (first=%v)\n",
							ctx.node.TagName, fragmentBox.X, fragmentBox.Y, fragmentBox.Width, fragmentBox.Height, fragmentBox.IsFirstFragment)
					}
				}

				// STEP 2: Layout the block child
				fmt.Printf("DEBUG MP: Laying out block child <%s> at Y=%.1f\n", item.Node.TagName, line.Y)
				blockBox := le.layoutNode(
					item.Node,
					state.ContainerBox.X+state.Border.Left+state.Padding.Left,
					line.Y,
					state.AvailableWidth,
					computedStyles,
					parent,
				)
				if blockBox != nil {
					boxes = append(boxes, blockBox)
				}

				// STEP 3: Restart fragments for open inline elements (content after block)
				// Note: New fragments will start on the next line, which will be processed in next iteration
				for i := range openInlines {
					ctx := &openInlines[i]
					// Fragment bounds will be set when we process the next line's content
					ctx.fragmentStartX = 0
					ctx.fragmentStartY = 0
					ctx.fragmentMaxX = 0
					ctx.fragmentMaxY = 0
				}

			case InlineItemFloat:
				// Check if this float has already been laid out (to avoid duplicate layouts on retry)
				var existingFloatBox *Box
				for i := state.FloatBaseIndex; i < len(le.floats); i++ {
					if le.floats[i].Box != nil && le.floats[i].Box.Node == item.Node {
						existingFloatBox = le.floats[i].Box
						fmt.Printf("DEBUG MP: Float <%s> already laid out at X=%.1f Y=%.1f, reusing\n",
							item.Node.TagName, existingFloatBox.X, existingFloatBox.Y)
						break
					}
				}

				// If float already exists, skip re-layout and continue
				if existingFloatBox != nil {
					boxes = append(boxes, existingFloatBox)
					continue
				}

				// Layout the float to get its dimensions
				fmt.Printf("DEBUG MP: Laying out float <%s> at Y=%.1f\n", item.Node.TagName, line.Y)

				// Track float count before layoutNode (layoutNode may add float as side effect)
				floatCountBefore := len(le.floats)

				floatBox := le.layoutNode(
					item.Node,
					state.ContainerBox.X+state.Border.Left+state.Padding.Left,
					line.Y,
					state.AvailableWidth,
					computedStyles,
					parent,
				)

				if floatBox != nil {
					fmt.Printf("DEBUG MP: Float box initially at X=%.1f Y=%.1f W=%.1f H=%.1f\n",
						floatBox.X, floatBox.Y, floatBox.Width, floatBox.Height)

					// Remove any floats added during layoutNode (float seeing itself bug)
					if len(le.floats) > floatCountBefore {
						fmt.Printf("DEBUG MP: Removing %d floats added during layoutNode\n", len(le.floats)-floatCountBefore)
						le.floats = le.floats[:floatCountBefore]
					}

					// Get float type and reposition the box correctly
					floatType := item.Style.GetFloat()
					floatWidth := le.getTotalWidth(floatBox)
					floatY := line.Y
					// IMPORTANT: Get fresh float offsets BEFORE positioning this float
					// Don't use leftOffsetBefore which was captured at start of line
					leftOffset, rightOffset := le.getFloatOffsets(floatY)

					// Calculate correct position based on float type
					var newX float64
					if floatType == css.FloatLeft {
						// For left floats, position must clear both other floats (leftOffset) AND inline content (currentX)
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						floatClearX := baseX + leftOffset + floatBox.Margin.Left
						inlineEndX := currentX + floatBox.Margin.Left
						fmt.Printf("DEBUG MP: Left float positioning - baseX=%.1f leftOffset=%.1f floatClearX=%.1f currentX=%.1f inlineEndX=%.1f\n",
							baseX, leftOffset, floatClearX, currentX, inlineEndX)
						if inlineEndX > floatClearX {
							newX = inlineEndX
							fmt.Printf("DEBUG MP: Using inlineEndX=%.1f\n", newX)
						} else {
							newX = floatClearX
							fmt.Printf("DEBUG MP: Using floatClearX=%.1f\n", newX)
						}
					} else {
						// Right float
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						newX = baseX + state.AvailableWidth - rightOffset - floatWidth + floatBox.Margin.Left
						fmt.Printf("DEBUG MP: Right float calc - baseX=%.1f avail=%.1f rightOff=%.1f floatW=%.1f -> X=%.1f\n",
							baseX, state.AvailableWidth, rightOffset, floatWidth, newX)
					}
					newY := floatY + floatBox.Margin.Top

					// Calculate position delta to reposition the float and its children
					deltaX := newX - floatBox.X
					deltaY := newY - floatBox.Y

					// Reposition child boxes
					for _, child := range floatBox.Children {
						child.X += deltaX
						child.Y += deltaY
					}

					floatBox.X = newX
					floatBox.Y = newY

					fmt.Printf("DEBUG MP: Float repositioned to X=%.1f Y=%.1f (delta X=%.1f Y=%.1f)\n",
						floatBox.X, floatBox.Y, deltaX, deltaY)

					boxes = append(boxes, floatBox)

					// Add float to engine's float list
					le.addFloat(floatBox, floatType, floatY)
					fmt.Printf("DEBUG MP: Added %v float to list, currentX before=%.1f\n", floatType, currentX)

					// Update currentX to account for the float we just added
					// (subsequent inline content must clear the float)
					if floatType == css.FloatLeft {
						leftOffsetNew, _ := le.getFloatOffsets(line.Y)
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						newCurX := baseX + leftOffsetNew
						fmt.Printf("DEBUG MP: Left float - leftOffset=%.1f, baseX=%.1f, newCurX=%.1f\n",
							leftOffsetNew, baseX, newCurX)
						if newCurX > currentX {
							currentX = newCurX
						}
					}
					fmt.Printf("DEBUG MP: currentX after=%.1f\n", currentX)

					// Check if this float changes available width for this line
					leftOffsetAfter, _ := le.getFloatOffsets(line.Y)
					if leftOffsetAfter != leftOffsetBefore {
						// Float changed available width - retry needed
						fmt.Printf("DEBUG MP: Float changed available width %.1f -> %.1f, retry needed\n",
							leftOffsetBefore, leftOffsetAfter)
						retryNeeded = true
					}
				} else {
					fmt.Printf("DEBUG MP: Float box was nil!\n")
				}
			}
		}
	}

	return boxes, retryNeeded
}
