package layout

import "louis14/pkg/css"

// ComputeReplacedSize implements CSS 2.1 §10.3.2 + §10.6.2 constraint
// resolution for replaced elements. Returns content-box inline-size and
// block-size in logical coordinates.
func ComputeReplacedSize(ctx *LayoutContext, node *LayoutInputNode, style *css.Style, space ConstraintSpace) (float64, float64) {
	wdm := space.WritingDirection
	geom := ComputeFragmentGeometry(style, wdm)
	info := GetIntrinsicSizingInfo(ctx, node)

	// Convert physical intrinsic dimensions to logical.
	var intrinsicInline, intrinsicBlock float64
	if wdm.IsVertical() {
		intrinsicInline = info.IntrinsicHeight
		intrinsicBlock = info.IntrinsicWidth
	} else {
		intrinsicInline = info.IntrinsicWidth
		intrinsicBlock = info.IntrinsicHeight
	}

	// Logical aspect ratio: inline/block.
	var logicalRatio float64
	if info.HasAspectRatio && intrinsicBlock > 0 {
		logicalRatio = intrinsicInline / intrinsicBlock
	}

	// Resolve explicit CSS sizes.
	explicitInline, hasExplicitInline := ResolveInlineSize(style, wdm, space, geom)
	explicitBlock, hasExplicitBlock := ResolveBlockSize(style, wdm, space, geom)

	// Handle fixed inline-size from parent (e.g. flex).
	if space.IsFixedInlineSize {
		explicitInline = space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if explicitInline < 0 {
			explicitInline = 0
		}
		hasExplicitInline = true
	}
	// Handle fixed block-size from parent (e.g. flex).
	if space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
		explicitBlock = space.AvailableSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlock < 0 {
			explicitBlock = 0
		}
		hasExplicitBlock = true
	}

	// CSS 2.1 §10.3.2 + §10.6.2 constraint resolution.
	var inlineSize, blockSize float64

	switch {
	case hasExplicitInline && hasExplicitBlock:
		inlineSize = explicitInline
		blockSize = explicitBlock

	case hasExplicitInline && !hasExplicitBlock:
		inlineSize = explicitInline
		if logicalRatio > 0 {
			blockSize = inlineSize / logicalRatio
		} else if intrinsicBlock > 0 {
			blockSize = intrinsicBlock
		} else {
			blockSize = 150 // CSS default
		}

	case !hasExplicitInline && hasExplicitBlock:
		blockSize = explicitBlock
		if logicalRatio > 0 {
			inlineSize = blockSize * logicalRatio
		} else if intrinsicInline > 0 {
			inlineSize = intrinsicInline
		} else {
			inlineSize = 300 // CSS default
		}

	default: // neither explicit
		if intrinsicInline > 0 {
			inlineSize = intrinsicInline
		} else if logicalRatio > 0 && intrinsicBlock > 0 {
			inlineSize = intrinsicBlock * logicalRatio
		} else {
			inlineSize = 300
		}
		if intrinsicBlock > 0 {
			blockSize = intrinsicBlock
		} else if logicalRatio > 0 && inlineSize > 0 {
			blockSize = inlineSize / logicalRatio
		} else {
			blockSize = 150
		}
	}

	// Apply min/max constraints (CSS 2.1 §10.4) with aspect ratio preservation.
	minInline := ResolveMinInlineSize(style, wdm, space, geom)
	maxInline, hasMaxInline := ResolveMaxInlineSize(style, wdm, space, geom)
	minBlock := ResolveMinBlockSize(style, wdm, space, geom)
	maxBlock, hasMaxBlock := ResolveMaxBlockSize(style, wdm, space, geom)

	// Clamp inline, re-derive block if needed.
	if inlineSize < minInline {
		inlineSize = minInline
		if logicalRatio > 0 {
			blockSize = inlineSize / logicalRatio
		}
	}
	if hasMaxInline && inlineSize > maxInline {
		inlineSize = maxInline
		if logicalRatio > 0 {
			blockSize = inlineSize / logicalRatio
		}
	}

	// Clamp block, re-derive inline if needed.
	if blockSize < minBlock {
		blockSize = minBlock
		if logicalRatio > 0 {
			inlineSize = blockSize * logicalRatio
		}
	}
	if hasMaxBlock && blockSize > maxBlock {
		blockSize = maxBlock
		if logicalRatio > 0 {
			inlineSize = blockSize * logicalRatio
		}
	}

	// Final inline clamp (after block re-derivation).
	if inlineSize < minInline {
		inlineSize = minInline
	}
	if hasMaxInline && inlineSize > maxInline {
		inlineSize = maxInline
	}

	return inlineSize, blockSize
}

// ReplacedLayoutAlgorithm lays out a replaced element (img, canvas, etc.).
// Mirrors Blink's LayoutReplaced.
type ReplacedLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
}

// NewReplacedLayoutAlgorithm creates a replaced element layout algorithm.
func NewReplacedLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *ReplacedLayoutAlgorithm {
	return &ReplacedLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// Layout performs replaced element layout and returns the result.
func (rla *ReplacedLayoutAlgorithm) Layout() *LayoutResult {
	wdm := rla.space.WritingDirection
	geom := ComputeFragmentGeometry(rla.style, wdm)

	var contentInline, contentBlock float64
	if rla.style != nil && rla.style.HasSizeContainment() {
		// CSS Containment: size containment — replaced element intrinsic size is 0.
		// Only use explicit inline/block sizes if set.
		if explInline, ok := ResolveInlineSize(rla.style, wdm, rla.space, geom); ok {
			contentInline = explInline
		}
		if explBlock, ok := ResolveBlockSize(rla.style, wdm, rla.space, geom); ok {
			contentBlock = explBlock
		}
	} else {
		contentInline, contentBlock = ComputeReplacedSize(rla.ctx, rla.node, rla.style, rla.space)
	}

	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(rla.node)
	builder.SetSize(LogicalSize{
		InlineSize: contentInline + geom.InlineBorderPadding(),
		BlockSize:  contentBlock + geom.BlockBorderPadding(),
	})
	builder.SetIntrinsicBlockSize(contentBlock)

	// Set box data for the renderer (margins, borders, padding).
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(rla.style, wdm, rla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})

	// For iframe/object elements with a document source, lay out the nested
	// document and embed its content as children.
	if rla.ctx.DocumentFetcher != nil && rla.node.DOMNode != nil {
		if nestedFrag := rla.layoutNestedDocument(contentInline, contentBlock); nestedFrag != nil {
			builder.AddChild(nestedFrag, LogicalOffset{})
		}
	}

	// Propagate exclusion space unchanged.
	if rla.space.ExclusionSpace != nil {
		builder.SetExclusionSpace(rla.space.ExclusionSpace)
	}

	return builder.Build()
}

// layoutNestedDocument fetches and lays out the embedded document for
// iframe/object elements. Returns the nested root fragment, or nil.
func (rla *ReplacedLayoutAlgorithm) layoutNestedDocument(contentInline, contentBlock float64) *PhysicalFragment {
	dom := rla.node.DOMNode
	tag := dom.TagName

	// Get the document URI from the appropriate attribute.
	var uri string
	switch tag {
	case "iframe":
		uri, _ = dom.GetAttribute("src")
	case "object":
		if dataType, _ := dom.GetAttribute("type"); dataType == "text/html" || dataType == "" {
			uri, _ = dom.GetAttribute("data")
		}
	default:
		return nil
	}
	if uri == "" {
		return nil
	}

	htmlContent, err := rla.ctx.DocumentFetcher(uri)
	if err != nil {
		return nil
	}

	// Compute physical viewport for the nested document.
	wdm := rla.space.WritingDirection
	physSize := ToPhysicalSize(LogicalSize{InlineSize: contentInline, BlockSize: contentBlock}, wdm.WM)

	result := layoutNestedDocument(rla.ctx, htmlContent, physSize.Width, physSize.Height)
	if result == nil || result.Fragment == nil {
		return nil
	}
	return result.Fragment
}
