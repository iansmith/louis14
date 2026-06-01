package layout

import (
	"louis14/pkg/css"
	"strings"
)

// resolveReplacedIntrinsicInlineKeyword resolves min-width or max-width when
// it's an intrinsic-sizing keyword (min-content, max-content, fit-content) on
// a replaced element. For a replaced element with an aspect ratio the
// min/max-content intrinsic inline-size is the *transferred size* — the
// definite block-size run through the aspect ratio (CSS Sizing 4 §5.1) — not
// the resolved inline-size. When both axes are explicit the resolved
// inline-size is the explicit width, so returning it would make the keyword a
// no-op; the transferred size (e.g. an explicit height carried through the
// ratio) is the value the keyword must resolve to. The caller passes
// transferredInline = blockSize × logicalRatio when a ratio exists, falling
// back to the resolved inline-size when there is no aspect ratio (no
// transferred size is defined).
//
// Mirrors Blink's ResolveInlineLengthInternal kMinContent/kMaxContent →
// MinMaxSizesFunc(kContent) → ComputeReplacedSizeInternal lambda →
// InlineSizeFromAspectRatio (length_utils.cc:64-69, :1313-1316, :1083-1093
// @ Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). min-content and
// max-content are equal for a replaced box.
//
// kind is "min" or "max" — determines which property to read.
// Returns the resolved length and true if the value was a keyword; false
// otherwise (caller falls back to ResolveMin/MaxInlineSize).
func resolveReplacedIntrinsicInlineKeyword(
	style *css.Style, wdm WritingDirectionMode, kind string, transferredInline float64,
) (float64, bool) {
	if style == nil {
		return 0, false
	}
	prop := kind + "-width"
	if wdm.IsVertical() {
		prop = kind + "-height"
	}
	v, ok := style.Get(prop)
	if !ok {
		return 0, false
	}
	switch strings.TrimSpace(v) {
	case "min-content", "max-content", "fit-content",
		"-webkit-min-content", "-webkit-max-content", "-webkit-fill-available",
		"-moz-min-content", "-moz-max-content", "-moz-fit-content":
		return transferredInline, true
	}
	return 0, false
}

// replacedBlockConstraintTransfersToInline reports whether the element's
// min-block-size or max-block-size constraints can transfer through its
// aspect ratio to affect the inline-size, per CSS Sizing 4 §3.4. When true,
// callers should honor ComputeReplacedSize's inline value even when it
// exceeds the shrink-to-fit / stretch-fit intrinsic width — the block-axis
// constraint is mandatory and dominates the otherwise-preferred intrinsic
// inline contribution. Mirrors the constraint-resolution flow in Blink's
// LayoutReplaced::ComputeIntrinsicSizingInfoForReplacedContent.
func replacedBlockConstraintTransfersToInline(
	ctx *LayoutContext, node *LayoutInputNode, style *css.Style,
	wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry,
) bool {
	if style == nil || node == nil {
		return false
	}
	info := GetIntrinsicSizingInfo(ctx, node)
	hasRatio := info.HasAspectRatio
	if !hasRatio {
		if ar := style.GetAspectRatio(); ar.IsSet && ar.Width > 0 && ar.Height > 0 {
			hasRatio = true
		}
	}
	if !hasRatio {
		return false
	}
	if ResolveMinBlockSize(style, wdm, space, geom).Float64() > 0 {
		return true
	}
	if _, ok := ResolveMaxBlockSize(style, wdm, space, geom); ok {
		return true
	}
	return false
}

// ComputeReplacedIntrinsicInlineSize returns the intrinsic inline-size for a
// replaced element without applying min/max block-size constraints that would
// transfer through the aspect ratio. This is used for intrinsic sizing (min/max
// content width) where block-axis constraints like min-height should NOT inflate
// the inline-size per CSS Sizing 3 §5.1.
func ComputeReplacedIntrinsicInlineSize(ctx *LayoutContext, node *LayoutInputNode, style *css.Style, space ConstraintSpace) float64 {
	wdm := space.WritingDirection
	geom := ComputeFragmentGeometry(style, wdm)
	info := GetIntrinsicSizingInfo(ctx, node)

	var intrinsicInline, intrinsicBlock float64
	if wdm.IsVertical() {
		intrinsicInline = info.IntrinsicHeight
		intrinsicBlock = info.IntrinsicWidth
	} else {
		intrinsicInline = info.IntrinsicWidth
		intrinsicBlock = info.IntrinsicHeight
	}

	var logicalRatio float64
	if info.HasAspectRatio {
		if info.AspectRatio > 0 {
			if wdm.IsVertical() {
				logicalRatio = 1.0 / info.AspectRatio
			} else {
				logicalRatio = info.AspectRatio
			}
		} else if intrinsicBlock > 0 {
			logicalRatio = intrinsicInline / intrinsicBlock
		}
	}

	explicitInlineLU, hasExplicitInline := ResolveInlineSize(style, wdm, space, geom)
	explicitBlockLU, hasExplicitBlock := ResolveBlockSize(style, wdm, space, geom)
	explicitInline := explicitInlineLU.Float64()
	explicitBlock := explicitBlockLU.Float64()

	if space.IsFixedInlineSize {
		explicitInline = space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if explicitInline < 0 {
			explicitInline = 0
		}
		hasExplicitInline = true
	}

	var inlineSize float64
	switch {
	case hasExplicitInline:
		inlineSize = explicitInline
	case hasExplicitBlock:
		if logicalRatio > 0 {
			inlineSize = explicitBlock * logicalRatio
		} else if intrinsicInline > 0 {
			inlineSize = intrinsicInline
		} else {
			inlineSize = 300
		}
	default:
		if intrinsicInline > 0 {
			inlineSize = intrinsicInline
		} else if logicalRatio > 0 && intrinsicBlock > 0 {
			inlineSize = intrinsicBlock * logicalRatio
		} else {
			inlineSize = 300
		}
	}

	// Apply only inline-axis min/max constraints (not block-axis).
	minInline := ResolveMinInlineSize(style, wdm, space, geom).Float64()
	if inlineSize < minInline {
		inlineSize = minInline
	}
	maxInlineLU, hasMaxInline := ResolveMaxInlineSize(style, wdm, space, geom)
	maxInline := maxInlineLU.Float64()
	if hasMaxInline && inlineSize > maxInline {
		inlineSize = maxInline
	}

	return inlineSize
}

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
	if info.HasAspectRatio {
		if info.AspectRatio > 0 {
			// Use pre-computed physical ratio, convert to logical.
			if wdm.IsVertical() {
				logicalRatio = 1.0 / info.AspectRatio
			} else {
				logicalRatio = info.AspectRatio
			}
		} else if intrinsicBlock > 0 {
			logicalRatio = intrinsicInline / intrinsicBlock
		}
	}

	// Resolve explicit CSS sizes.
	explicitInlineLU, hasExplicitInline := ResolveInlineSize(style, wdm, space, geom)
	explicitBlockLU, hasExplicitBlock := ResolveBlockSize(style, wdm, space, geom)
	explicitInline := explicitInlineLU.Float64()
	explicitBlock := explicitBlockLU.Float64()

	// Handle fixed inline-size from parent (e.g. flex).
	if space.IsFixedInlineSize {
		explicitInline = space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if explicitInline < 0 {
			explicitInline = 0
		}
		hasExplicitInline = true
	}
	// Handle fixed block-size from parent (e.g. flex).
	if space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
		explicitBlock = space.AvailableSize.BlockSize.Float64() - geom.BlockBorderPadding()
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
		} else if logicalRatio > 0 {
			// CSS 2.1 §10.3.2 + CSS Sizing 4 §6.3: auto/auto replaced element
			// with an intrinsic ratio but no intrinsic dimensions uses the
			// stretch-fit available inline-size (the block-level non-replaced
			// constraint equation) when the containing block's width doesn't
			// depend on the element's width. block-size then transfers from
			// inline-size via the ratio. Falls back to 300 when available
			// inline-size is indefinite (e.g., max-content measurement).
			avail := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
			if avail > 0 && avail < 1e8 {
				inlineSize = avail
			} else {
				inlineSize = 300
			}
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
	minInline := ResolveMinInlineSize(style, wdm, space, geom).Float64()
	maxInlineLU, hasMaxInline := ResolveMaxInlineSize(style, wdm, space, geom)
	maxInline := maxInlineLU.Float64()
	minBlock := ResolveMinBlockSize(style, wdm, space, geom).Float64()
	maxBlockLU, hasMaxBlock := ResolveMaxBlockSize(style, wdm, space, geom)
	maxBlock := maxBlockLU.Float64()

	// CSS Sizing 4 §5.1: intrinsic-keyword min/max constraints (min-content,
	// max-content, fit-content) on the inline axis of a replaced element
	// resolve to the *transferred size* — the definite block-size run through
	// the aspect ratio. When both axes are explicit the resolved inlineSize is
	// the explicit width, so using it would make the keyword a no-op; the
	// transferred size (blockSize × logicalRatio) is the correct keyword value.
	// With no aspect ratio there is no transferred size, so fall back to the
	// resolved inlineSize. Without this, ResolveMinInlineSize returns 0 and the
	// min/max constraint is effectively dropped.
	transferredInline := inlineSize
	if logicalRatio > 0 {
		transferredInline = blockSize * logicalRatio
	}
	if minVal, ok := resolveReplacedIntrinsicInlineKeyword(style, wdm, "min", transferredInline); ok {
		if minVal > minInline {
			minInline = minVal
		}
	}
	if maxVal, ok := resolveReplacedIntrinsicInlineKeyword(style, wdm, "max", transferredInline); ok {
		if !hasMaxInline || maxVal < maxInline {
			maxInline = maxVal
			hasMaxInline = true
		}
	}

	// Clamp inline, re-derive block if needed.
	// CSS 2.1 §10.4: apply max first, then min — so min-width wins when min > max.
	if hasMaxInline && inlineSize > maxInline {
		inlineSize = maxInline
		if logicalRatio > 0 {
			blockSize = inlineSize / logicalRatio
		}
	}
	if inlineSize < minInline {
		inlineSize = minInline
		if logicalRatio > 0 {
			blockSize = inlineSize / logicalRatio
		}
	}

	// Clamp block, re-derive inline if needed.
	// Same CSS 2.1 §10.4 ordering: max first, then min.
	if hasMaxBlock && blockSize > maxBlock {
		blockSize = maxBlock
		if logicalRatio > 0 {
			inlineSize = blockSize * logicalRatio
		}
	}
	if blockSize < minBlock {
		blockSize = minBlock
		if logicalRatio > 0 {
			inlineSize = blockSize * logicalRatio
		}
	}

	// Final inline clamp (after block re-derivation).
	if hasMaxInline && inlineSize > maxInline {
		inlineSize = maxInline
	}
	if inlineSize < minInline {
		inlineSize = minInline
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
	if rla.style != nil && rla.style.ShouldApplySizeContainment() {
		// CSS Containment: size containment — replaced element intrinsic dimensions
		// are suppressed (treated as 0), but the element's intrinsic aspect ratio
		// is preserved per CSS Containment §3.1.2.
		//
		// Sizing rules:
		// - Explicit CSS width/height are used if set.
		// - If only one explicit dimension is set, the intrinsic aspect ratio
		//   (from the replaced element's native dimensions) is used to derive
		//   the other dimension.
		// - If neither is set, the element is 0×0 (no intrinsic dimensions under
		//   size containment).
		hasInline, hasBlock := false, false
		if explInline, ok := ResolveInlineSize(rla.style, wdm, rla.space, geom); ok {
			contentInline = explInline.Float64()
			hasInline = true
		}
		if explBlock, ok := ResolveBlockSize(rla.style, wdm, rla.space, geom); ok {
			contentBlock = explBlock.Float64()
			hasBlock = true
		}
		// If only one dimension is set, use the element's intrinsic aspect ratio
		// (preserved under contain: size) to derive the missing dimension.
		if hasInline != hasBlock {
			info := GetIntrinsicSizingInfo(rla.ctx, rla.node)
			if info.HasAspectRatio && info.AspectRatio > 0 {
				// Convert physical aspect ratio (width/height) to logical (inline/block).
				logicalRatio := info.AspectRatio
				if wdm.IsVertical() {
					logicalRatio = 1.0 / info.AspectRatio
				}
				if logicalRatio > 0 {
					if hasInline && !hasBlock {
						contentBlock = contentInline / logicalRatio
					} else if hasBlock && !hasInline {
						contentInline = contentBlock * logicalRatio
					}
				}
			}
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
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(rla.style, wdm, rla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	// For iframe/object elements with a document source, lay out the nested
	// document and embed its content as children.
	if rla.node.DOMNode != nil {
		if nested := rla.layoutNestedDocument(contentInline, contentBlock); nested != nil {
			// For vertical-rl/sideways-rl nested roots, the root is physically
			// anchored to the right edge of the iframe viewport. Apply the X
			// offset as an inline offset (in the parent's HTB coordinate system,
			// inline = horizontal = physical X).
			offset := LogicalOffset{InlineOffset: nested.rootOffsetX}
			builder.AddChild(nested.fragment, offset)
		}
	}

	// Propagate exclusion space unchanged.
	if rla.space.ExclusionSpace != nil {
		builder.SetExclusionSpace(rla.space.ExclusionSpace)
	}

	return builder.Build()
}

// nestedDocFragment wraps the result of laying out an embedded document.
type nestedDocFragment struct {
	fragment    *PhysicalFragment
	rootOffsetX float64 // physical X offset of root within the iframe viewport
}

// layoutNestedDocument fetches and lays out the embedded document for
// iframe/object elements. Returns the nested root fragment and its X offset, or nil.
func (rla *ReplacedLayoutAlgorithm) layoutNestedDocument(contentInline, contentBlock float64) *nestedDocFragment {
	dom := rla.node.DOMNode
	tag := dom.TagName

	// Get the document URI from the appropriate attribute.
	var uri string
	var htmlContent string

	switch tag {
	case "iframe":
		// srcdoc takes priority over src per HTML spec.
		if srcdoc, ok := dom.GetAttribute("srcdoc"); ok && srcdoc != "" {
			// Wrap the srcdoc text in a minimal document so the HTML parser has
			// a proper <html><body> context, then lay it out without fetching.
			htmlContent = srcdoc
			uri = ""
		} else {
			uri, _ = dom.GetAttribute("src")
		}
	case "object":
		if dataType, _ := dom.GetAttribute("type"); dataType == "text/html" || dataType == "" {
			uri, _ = dom.GetAttribute("data")
		}
	default:
		return nil
	}

	// If we have inline srcdoc content, skip the fetcher entirely.
	if htmlContent == "" {
		if uri == "" || rla.ctx.DocumentFetcher == nil {
			return nil
		}
		var err error
		htmlContent, err = rla.ctx.DocumentFetcher(uri)
		if err != nil {
			return nil
		}
	}

	// Compute physical viewport for the nested document.
	wdm := rla.space.WritingDirection
	physSize := ToPhysicalSize(LogicalSize{InlineSize: contentInline, BlockSize: contentBlock}, wdm.WM)

	res := layoutNestedDocument(rla.ctx, htmlContent, physSize.Width, physSize.Height, uri)
	if res == nil {
		return nil
	}
	// Retain the parsed nested document on the iframe/object DOM node so that
	// JS can access iframe.contentDocument after layout completes.
	if res.Doc != nil {
		dom.NestedDocument = res.Doc
	}
	return &nestedDocFragment{fragment: res.Result.Fragment, rootOffsetX: res.RootOffsetX}
}
