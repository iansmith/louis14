package render

import (
	"sort"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"mazarin/textshape"
)

// PaintLayer represents a node in the pre-paint tree.
// Built from the layout Box tree before painting, PaintLayers pre-sort
// children by CSS 2.1 Appendix E stacking order and pre-compute
// all paint-relevant properties so draw methods never access Style.
//
// Boxes without Style (line boxes, text runs) do NOT get PaintLayers.
// They remain reachable via their parent PaintLayer's Box.Children
// and are painted inline during the parent's paint.
//
// Mirrors Blink's PaintLayer / PrePaintTreeWalk.
type PaintLayer struct {
	Box      *layout.Box
	Position css.PositionType // Always set (PositionStatic for unstyled)
	ZIndex   int

	// Stacking children (only populated for stacking context roots):
	NegativeZ []*PaintLayer // z < 0, sorted ascending
	AutoZero  []*PaintLayer // z-index:auto positioned + z-index:0 SCs, tree order
	PositiveZ []*PaintLayer // z > 0, sorted ascending

	// Non-positioned children in DOM order (Appendix E steps 3-5):
	FlowChildren []*PaintLayer

	// Float children (Appendix E step 4: floats paint after non-float blocks).
	// Floats are separated from FlowChildren so they paint above block backgrounds.
	FloatChildren []*PaintLayer

	// Overflow clip (pre-computed from Style):
	HasClip  bool
	ClipX    bool       // true if X axis is clipped (overflow-x != visible)
	ClipY    bool       // true if Y axis is clipped (overflow-y != visible)
	ClipRect [4]float64 // x, y, w, h of padding box

	// CSS clip: rect() (purely physical, per CSS Writing Modes §7.6):
	HasCSSClip  bool
	CSSClipRect [4]float64 // x, y, w, h of clip region

	// Pre-computed paint properties — no Style access needed during paint.

	// Compositing:
	Visible bool    // false = visibility:hidden, skip subtree
	Opacity float64 // 0.0..1.0; 1.0 = fully opaque

	// Image (for <img> replaced elements):
	ImageSrc        string         // src attribute value; empty if not an img element
	ObjectFit       css.ObjectFit  // fill, contain, cover, none, scale-down
	ObjectPosition  [2]float64     // x%, y% in range [0,1]; default (0.5, 0.5)
	ImageRendering  string         // auto, pixelated, crisp-edges, -webkit-optimize-contrast

	// Background:
	BackgroundColor  css.Color              // A==0 means no background
	BackgroundClip   css.BackgroundClipType // clip for background-color (always set)
	BackgroundLayers *css.FillLayer         // linked list of layers, head = topmost CSS layer

	// Borders: indices 0=Top, 1=Right, 2=Bottom, 3=Left
	BorderColors [4]css.Color
	BorderStyles [4]css.BorderStyle
	BorderRadius css.EllipticalRadii // TopLeft, TopRight, BottomRight, BottomLeft (elliptical)

	// Border image (9-slice): replaces regular border drawing when source is set.
	BorderImageSource string             // URL or gradient; empty = none
	BorderImageSlice  css.BorderImageSlice // 4 slice values + fill flag
	BorderImageWidth  [4]float64         // top, right, bottom, left (px)
	BorderImageRepeat [2]string          // [horizontal, vertical]: stretch/repeat/round/space

	// Box shadows (outset and inset):
	BoxShadows []css.BoxShadow

	// Outline (doesn't affect layout, drawn outside border-box):
	OutlineStyle  string  // none, solid, dashed, dotted, double
	OutlineWidth  float64
	OutlineColor  css.Color
	OutlineOffset float64

	// Text:
	TextColor     css.Color
	FontSize      float64
	FontBold      bool
	FontItalic    bool
	FontMono      bool
	FontAhem       bool
	FontFamily     string
	LetterSpacing  float64
	WordSpacing      float64
	TabSize          float64 // tab-size value (character count or px)
	TabSizeIsLength  bool    // true = px length, false = character count
	IsVerticalText bool
	IsSidewaysLR   bool
	IsSidewaysRL   bool

	// Text decoration (underline, overline, line-through):
	TextDecoration          css.TextDecoration
	TextDecorationColor     css.Color  // defaults to TextColor (currentColor)
	TextDecorationThickness float64    // defaults to ~1px
	TextDecorationStyle     string     // solid, double, dotted, dashed, wavy
	TextUnderlineOffset     float64    // additional offset for underline (px); 0 = auto/default

	// Text shadows:
	TextShadows []css.TextShadow

	// CSS font-variant-caps (small-caps, all-small-caps, etc.):
	FontVariantCaps string

	// CSS font-feature-settings: OpenType feature tags parsed into tag/value pairs.
	// Populated from CSS like "kern" 1, "liga" 0. Empty when "normal".
	FontFeatures []textshape.FontFeature

	// Text emphasis marks (small marks above/below each character):
	TextEmphasisMark     string    // resolved mark character ("●", "•", etc.); "" = none
	TextEmphasisColor    css.Color // defaults to currentColor
	TextEmphasisOver     bool      // true = over (above), false = under (below)

	// CSS text-transform (uppercase, lowercase, capitalize):
	TextTransform css.TextTransform

	// CSS text-overflow (clip or ellipsis):
	TextOverflow css.TextOverflowType

	// List markers:
	IsListItem              bool
	ListStyleType           css.ListStyleType
	ListStyleImage          string // URL from list-style-image (empty or "none" means no image)
	ListStylePositionInside bool   // true = inside, false = outside (default)
	ListItemIndex           int    // 1-based ordinal for ordered lists
	MarkerContent   string    // custom content from ::marker { content: "..." }
	MarkerColor     css.Color // color override from ::marker rules
	HasMarkerColor  bool      // true if ::marker specifies a color
	MarkerFontSize  float64   // font-size override from ::marker rules
	HasMarkerFont   bool      // true if ::marker specifies font-size

	// CSS Transforms:
	Transforms      []css.Transform
	TransformOrigin [2]float64 // resolved to px: (origin-x, origin-y)
	HasTransform    bool

	// CSS Filters:
	Filters   []css.FilterFunction
	HasFilter bool

	// CSS Backdrop Filters:
	BackdropFilters   []css.FilterFunction
	HasBackdropFilter bool

	// CSS clip-path:
	ClipPath *css.ClipPath // nil = no clip-path

	// CSS mask-image:
	MaskImage    string // "none", url(...), or gradient value
	HasMaskImage bool

	// CSS mix-blend-mode:
	BlendMode css.MixBlendMode

	// PaintsCanvasBackground is true for the root element (or body when
	// background propagates). Per CSS 2.1 §14.2, the root element's background
	// paints the entire canvas, not just its own box.
	PaintsCanvasBackground bool

	// CanvasBackgroundRootBox is set on a promoted body layer to store the
	// root element's box. Per CSS Backgrounds Level 3 §2.11.2, when the
	// body's background is promoted to the canvas, the positioning area is
	// the root element's padding box, not the body's.
	CanvasBackgroundRootBox *layout.Box

	// empty-cells: hide — skip background/border painting for empty table cells
	// (CSS 2.1 §17.6.1.1, only in separate border model).
	EmptyCellHide bool

	// Column rules (for multicol containers):
	IsMulticol      bool
	ColumnCount     int     // number of columns actually rendered
	ColumnWidth     float64 // used column width
	ColumnGap       float64 // gap between columns
	ColumnRuleWidth float64 // rule width in px
	ColumnRuleStyle string  // none, solid, dashed, dotted, etc.
	ColumnRuleColor css.Color
}

// BuildPaintTree constructs a PaintLayer tree from a layout Box tree.
// The root box is always treated as a stacking context root
// (CSS 2.1 Appendix E: root element establishes the initial stacking context).
func BuildPaintTree(root *layout.Box) *PaintLayer {
	if root == nil {
		return nil
	}
	rootLayer := newPaintLayer(root)
	buildPaintSubtree(root, rootLayer, rootLayer)
	rootLayer.sortZLists()
	return rootLayer
}

func newPaintLayer(box *layout.Box) *PaintLayer {
	layer := &PaintLayer{
		Box:      box,
		Position: css.PositionStatic,
		Visible:  true,
		Opacity:  1.0,
	}
	s := box.Style
	if s == nil {
		// Unstyled root (rare): no paint properties to compute.
		return layer
	}

	layer.Position = box.Position
	if layer.Position == "" {
		layer.Position = css.PositionStatic
	}
	layer.ZIndex = box.ZIndex

	// Overflow clip (or paint containment clip at padding box).
	// Per CSS Overflow Level 3, overflow-x and overflow-y are independent:
	// a box may clip in one axis but not the other.
	overflowX := s.GetOverflowX()
	overflowY := s.GetOverflowY()
	hasPaintContain := s.HasPaintContainment()
	clipX := overflowX == css.OverflowHidden || overflowX == css.OverflowScroll || overflowX == css.OverflowAuto || hasPaintContain
	clipY := overflowY == css.OverflowHidden || overflowY == css.OverflowScroll || overflowY == css.OverflowAuto || hasPaintContain

	// CSS Tables 3 §5.4.1: rowspan cells whose span overlaps a
	// visibility:collapse row must clip content to their border-box,
	// regardless of the computed `overflow` property (the default on
	// <td> is `visible`). Layout sets Box.ClipContentToBorderBox on
	// such cells; here we promote it into a two-axis clip with the
	// border-box as the clip rectangle (not the padding box, per the
	// spec wording "clip the content to the table-cell's border-box").
	forceBorderBoxClip := box.ClipContentToBorderBox
	if forceBorderBoxClip {
		clipX = true
		clipY = true
	}

	if clipX || clipY {
		layer.HasClip = true
		layer.ClipX = clipX
		layer.ClipY = clipY

		var padX, padY, clipW, clipH float64
		if forceBorderBoxClip {
			// Border-box clip: include border region.
			padX = box.X
			padY = box.Y
			clipW = box.Width
			clipH = box.Height
		} else {
			// Overflow clip defaults to the padding box.
			padX = box.X + box.Border.Left
			padY = box.Y + box.Border.Top
			clipW = box.Width - box.Border.Left - box.Border.Right
			clipH = box.Height - box.Border.Top - box.Border.Bottom
		}
		if clipW < 0 {
			clipW = 0
		}
		if clipH < 0 {
			clipH = 0
		}

		// For an unclipped axis, extend the clip rect to a very large range
		// so that content is not clipped along that axis.
		const largeExtent = 1e7
		if !clipX {
			padX = -largeExtent / 2
			clipW = largeExtent
		}
		if !clipY {
			padY = -largeExtent / 2
			clipH = largeExtent
		}

		layer.ClipRect = [4]float64{padX, padY, clipW, clipH}
	}

	// CSS clip: rect() — applies to absolutely positioned elements (CSS 2.1 §11.1.2).
	// Values are physical offsets from the element's border-box top-left corner.
	if cr := s.GetClipRect(); cr != nil {
		layer.HasCSSClip = true
		clipRight := cr.Right
		if clipRight < 0 {
			clipRight = box.Width // "auto" sentinel
		}
		clipBottom := cr.Bottom
		if clipBottom < 0 {
			clipBottom = box.Height // "auto" sentinel
		}
		layer.CSSClipRect = [4]float64{
			box.X + cr.Left,
			box.Y + cr.Top,
			clipRight - cr.Left,
			clipBottom - cr.Top,
		}
	}

	// Compositing.
	if vis, ok := s.Get("visibility"); ok && (vis == "hidden" || vis == "collapse") {
		layer.Visible = false
	}
	layer.Opacity = s.GetOpacity()

	// Replaced element (img): capture src for paint-time image loading.
	if box.Node != nil && box.Node.TagName == "img" {
		if src, ok := box.Node.GetAttribute("src"); ok {
			layer.ImageSrc = src
		}
		layer.ObjectFit = s.GetObjectFit()
		opX, opY := s.GetObjectPosition()
		layer.ObjectPosition = [2]float64{opX, opY}
	}
	// image-rendering applies to all elements (images and background images).
	if ir, ok := s.Get("image-rendering"); ok {
		layer.ImageRendering = ir
	}

	// Background color.
	// Text runs (box.Text != "") inherit the parent element's style which may
	// include background-color, but CSS backgrounds are painted on element boxes,
	// not on individual text runs within them. Skip background for text runs
	// to avoid painting the inherited background outside the element's area.
	if box.Text == "" {
		if bg, ok := s.Get("background-color"); ok {
			if c, ok := css.ParseColor(bg); ok {
				layer.BackgroundColor = c
			}
		}
	}

	// Background clip for background-color (uses bottom layer's clip per CSS spec).
	layer.BackgroundClip = s.GetBackgroundColorClip()

	// Background layers (multi-layer support via FillLayer linked list).
	layer.BackgroundLayers = s.GetBackgroundLayers()
	// Fallback for single-layer backgrounds not caught by GetBackgroundLayers:
	// e.g., when background-image is a single gradient not in url() form.
	if layer.BackgroundLayers == nil {
		if val, ok := s.Get("background-image"); ok && val != "none" && val != "" {
			if isGradientValue(val) {
				layer.BackgroundLayers = &css.FillLayer{
					Gradient:      val,
					ImageSet:      true,
					Repeat:        s.GetBackgroundRepeat(),
					RepeatSet:     true,
					Position:      s.GetBackgroundPosition(),
					PositionSet:   true,
					Size:          s.GetBackgroundSize(),
					SizeSet:       true,
					Clip:          s.GetBackgroundClip(),
					ClipSet:       true,
					Origin:        s.GetBackgroundOrigin(),
					OriginSet:     true,
					Attachment:    s.GetBackgroundAttachment(),
					AttachmentSet: true,
				}
			}
		}
	}

	// Border colors: currentColor fallback.
	currentColor := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if cv, ok := s.Get("color"); ok {
		if c, ok := css.ParseColor(cv); ok {
			currentColor = c
		}
	}
	sides := [4]string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}
	for i, prop := range sides {
		if val, ok := s.Get(prop); ok {
			if c, ok := css.ParseColor(val); ok {
				layer.BorderColors[i] = c
				continue
			}
		}
		layer.BorderColors[i] = currentColor
	}

	// Border styles.
	bs := s.GetBorderStyle()
	layer.BorderStyles = [4]css.BorderStyle{bs.Top, bs.Right, bs.Bottom, bs.Left}

	// Border radius — resolve percentages against box dimensions and constrain.
	layer.BorderRadius = s.GetBorderRadiiResolved(box.Width, box.Height).
		ConstrainRadii(box.Width, box.Height)

	// Border image (9-slice).
	if biSrc := s.GetBorderImageSource(); biSrc != "" && biSrc != "none" {
		layer.BorderImageSource = biSrc
		layer.BorderImageSlice = s.GetBorderImageSlice()
		bwArr := [4]float64{box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left}
		layer.BorderImageWidth = s.GetBorderImageWidth(bwArr)
		layer.BorderImageRepeat = s.GetBorderImageRepeat()
	}

	// Box shadows. Resolve currentcolor to the element's text color.
	layer.BoxShadows = s.GetBoxShadow()
	for i := range layer.BoxShadows {
		if layer.BoxShadows[i].UseCurrentColor {
			layer.BoxShadows[i].Color = currentColor
		}
	}

	// Outline.
	layer.OutlineStyle = s.GetOutlineStyle()
	if layer.OutlineStyle != "none" {
		layer.OutlineWidth = s.GetOutlineWidth()
		r, g, b, a := s.GetOutlineColor()
		layer.OutlineColor = css.Color{R: r, G: g, B: b, A: a}
		layer.OutlineOffset = s.GetOutlineOffset()
	}

	// Text.
	layer.TextColor = currentColor // default: currentColor
	layer.FontSize = s.GetFontSize()
	if layer.FontSize <= 0 {
		layer.FontSize = 16
	}
	layer.FontBold = s.GetFontWeight() == css.FontWeightBold
	layer.FontItalic = s.GetFontStyle() == css.FontStyleItalic
	layer.FontMono = s.IsMonospaceFamily()
	layer.FontAhem = s.IsAhemFamily()
	if family, ok := s.Get("font-family"); ok {
		layer.FontFamily = family
	}
	layer.LetterSpacing = s.GetLetterSpacing()
	layer.WordSpacing = s.GetWordSpacing()
	layer.TabSize, layer.TabSizeIsLength = s.GetTabSize()
	layer.IsVerticalText = box.IsVerticalText
	layer.IsSidewaysLR = box.IsSidewaysLR
	layer.IsSidewaysRL = box.IsSidewaysRL

	// Text decoration.
	layer.TextDecoration = s.GetTextDecoration()
	if decColor, ok := s.GetTextDecorationColor(); ok {
		layer.TextDecorationColor = decColor
	} else {
		layer.TextDecorationColor = currentColor
	}
	layer.TextDecorationThickness = s.GetTextDecorationThickness()
	layer.TextDecorationStyle = s.GetTextDecorationStyle()
	layer.TextUnderlineOffset = s.GetTextUnderlineOffset()
	layer.TextShadows = s.GetTextShadow()
	layer.FontVariantCaps = s.GetFontVariantCaps()

	// CSS font-feature-settings.
	if ffs, ok := s.Get("font-feature-settings"); ok && ffs != "normal" && ffs != "" {
		layer.FontFeatures = parseFontFeatureSettings(ffs)
	}

	// Text emphasis marks.
	if mark := s.GetTextEmphasisMark(); mark != "" {
		layer.TextEmphasisMark = mark
		layer.TextEmphasisColor = s.GetTextEmphasisColor()
		pos := s.GetTextEmphasisPosition()
		layer.TextEmphasisOver = !strings.Contains(pos, "under")
	}

	layer.TextTransform = s.GetTextTransform()
	layer.TextOverflow = s.GetTextOverflow()

	// List markers.
	if s.GetDisplay() == css.DisplayListItem {
		layer.IsListItem = true
		layer.ListStyleType = s.GetListStyleType()
		layer.ListStylePositionInside = s.GetListStylePosition() == "inside"
		layer.ListItemIndex = computeListItemIndex(box)
		if imgVal, ok := s.Get("list-style-image"); ok && imgVal != "none" {
			if u, valid := css.ParseURLValue(imgVal); valid {
				layer.ListStyleImage = u
			}
		}

		// ::marker pseudo-element: apply styling overrides.
		if ms := box.MarkerStyle; ms != nil {
			// Check for color override — only if different from the element's text color.
			if colorStr, ok := ms.Get("color"); ok {
				if c, valid := css.ParseColor(colorStr); valid {
					if c != layer.TextColor {
						layer.MarkerColor = c
						layer.HasMarkerColor = true
					}
				}
			}
			// Check for font-size override.
			if _, ok := ms.Get("font-size"); ok {
				mfs := ms.GetFontSize()
				if mfs > 0 && mfs != layer.FontSize {
					layer.MarkerFontSize = mfs
					layer.HasMarkerFont = true
				}
			}
		}
	}

	// CSS Transforms (individual properties + shorthand).
	// Per CSS Transforms Level 2, the effective transform is:
	//   translate * rotate * scale * transform
	// i.e., individual properties are applied first, then the shorthand.

	// Collect individual transform properties.
	var individualTransforms []css.Transform
	if tx, ty, ok := s.GetIndividualTranslate(); ok {
		// Resolve percentage sentinels (negative values) against element dimensions.
		if tx < 0 {
			tx = (-tx / 100) * box.Width
		}
		if ty < 0 {
			ty = (-ty / 100) * box.Height
		}
		individualTransforms = append(individualTransforms, css.Transform{Type: "translate", Values: []float64{tx, ty}})
	}
	if deg, ok := s.GetIndividualRotate(); ok {
		individualTransforms = append(individualTransforms, css.Transform{Type: "rotate", Values: []float64{deg}})
	}
	if sx, sy, ok := s.GetIndividualScale(); ok {
		individualTransforms = append(individualTransforms, css.Transform{Type: "scale", Values: []float64{sx, sy}})
	}

	// Collect shorthand transforms.
	transforms := s.GetTransforms()

	if len(individualTransforms) > 0 || len(transforms) > 0 {
		layer.HasTransform = true
		origin := s.GetTransformOrigin()
		// Resolve percentage origin to px relative to element's border box.
		layer.TransformOrigin = [2]float64{
			origin.X * box.Width,
			origin.Y * box.Height,
		}
		// Resolve percentage translate values in shorthand transforms.
		// parseTransformValue() uses negative values as a sentinel for percentages.
		resolved := make([]css.Transform, len(transforms))
		for i, t := range transforms {
			resolved[i] = css.Transform{Type: t.Type, Values: make([]float64, len(t.Values))}
			copy(resolved[i].Values, t.Values)
			if t.Type == "translate" {
				// Values[0] is X (percentage relative to width)
				// Values[1] is Y (percentage relative to height)
				if len(resolved[i].Values) > 0 && resolved[i].Values[0] < 0 {
					resolved[i].Values[0] = (-resolved[i].Values[0] / 100) * box.Width
				}
				if len(resolved[i].Values) > 1 && resolved[i].Values[1] < 0 {
					resolved[i].Values[1] = (-resolved[i].Values[1] / 100) * box.Height
				}
			}
		}
		// Compose: individual properties first, then shorthand.
		layer.Transforms = append(individualTransforms, resolved...)
	}

	// CSS Filters.
	filters := s.GetFilter()
	if len(filters) > 0 {
		layer.Filters = filters
		layer.HasFilter = true
	}

	// CSS Backdrop Filters.
	bdFilters := s.GetBackdropFilter()
	if len(bdFilters) > 0 {
		layer.BackdropFilters = bdFilters
		layer.HasBackdropFilter = true
	}

	// CSS clip-path.
	layer.ClipPath = s.GetClipPath()

	// CSS mask-image.
	if mi := s.GetMaskImage(); mi != "" && mi != "none" {
		layer.MaskImage = mi
		layer.HasMaskImage = true
	}

	// CSS mix-blend-mode.
	layer.BlendMode = s.GetMixBlendMode()

	// Column rules for multicol containers.
	if s.GetColumnCount() > 0 || s.GetColumnWidth() > 0 {
		layer.IsMulticol = true
		layer.ColumnRuleWidth = s.GetColumnRuleWidth()
		layer.ColumnRuleStyle = s.GetColumnRuleStyle()
		layer.ColumnRuleColor = s.GetColumnRuleColor()
		layer.ColumnGap = s.GetColumnGapMulticol()

		// Compute used column count and width from the content width.
		contentW := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right
		if contentW < 0 {
			contentW = 0
		}
		colCount, colWidth := layout.ResolveColumnCountForPaint(contentW, s.GetColumnCount(), s.GetColumnWidth(), layer.ColumnGap)
		layer.ColumnCount = colCount
		layer.ColumnWidth = colWidth
	}

	// empty-cells: hide — suppress background/border for empty table cells
	// in the separate border model (CSS 2.1 §17.6.1.1).
	if s.GetDisplay() == css.DisplayTableCell &&
		s.GetEmptyCells() == "hide" &&
		s.GetBorderCollapse() == css.BorderCollapseSeparate &&
		len(box.Children) == 0 &&
		box.Node != nil && isCellNodeEmpty(box.Node) {
		layer.EmptyCellHide = true
	}

	return layer
}

// isCellNodeEmpty returns true if a table cell's DOM node has no visible content.
// Per CSS 2.1 §17.6.1.1: whitespace-only text is not "visible content",
// but &nbsp; (U+00A0) IS content, and any child element means not empty.
func isCellNodeEmpty(node *html.Node) bool {
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) != "" {
				return false
			}
		} else if child.Type == html.ElementNode {
			return false
		}
	}
	return true
}

// computeListItemIndex returns the 1-based ordinal for a list item,
// respecting <ol start="N"> and counting preceding list-item siblings.
// Only siblings with display:list-item are counted (not all element siblings),
// which matches how the CSS list-item counter works.
func computeListItemIndex(box *layout.Box) int {
	if box.LayoutNode == nil || box.LayoutNode.DOMNode == nil {
		return 1
	}
	node := box.LayoutNode.DOMNode
	if node.Parent == nil {
		return 1
	}
	// Check for <ol start="N">
	start := 1
	if val, ok := node.Parent.GetAttribute("start"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			start = n
		}
	}
	// Count preceding list-item siblings using the box tree, which gives
	// access to computed styles. This correctly skips non-list-item elements
	// (e.g., a <p> before a <div display:list-item>).
	if box.Parent != nil {
		idx := start
		for _, sibling := range box.Parent.Children {
			if sibling == box {
				break
			}
			if sibling.Style != nil && sibling.Style.GetDisplay() == css.DisplayListItem {
				idx++
			}
		}
		return idx
	}
	return start
}

// domOrderedChildren returns the children of box in DOM tree order.
//
// In-flow children (block, inline, anonymous) are already laid out in DOM
// order and appear in box.Children in that order. Out-of-flow (abs-pos/fixed)
// children are appended at the end of box.Children by OutOfFlowLayoutPart,
// regardless of their DOM position. This function re-inserts out-of-flow
// children at their correct DOM position while keeping in-flow children
// (including anonymous boxes) in their original order.
func domOrderedChildren(box *layout.Box) []*layout.Box {
	// Flex containers: children are already in order-modified document order
	// from flex layout. CSS Flexbox §4.3 says flex items paint in this order,
	// NOT in DOM order. Skip the DOM reordering.
	if box.Style != nil {
		d := box.Style.GetDisplay()
		if d == css.DisplayFlex || d == css.DisplayInlineFlex {
			return box.Children
		}
	}

	lin := box.LayoutNode
	if lin == nil {
		return box.Children
	}

	// Identify out-of-flow (abs-pos/fixed) children. These are appended at
	// the end of box.Children by OutOfFlowLayoutPart out of DOM order.
	oofSet := make(map[*layout.Box]bool)
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			oofSet[child] = true
		}
	}

	if len(oofSet) == 0 {
		// No out-of-flow children — in-flow order is already correct.
		return box.Children
	}

	// Build LIN → Box map for out-of-flow children only.
	byLIN := make(map[*layout.LayoutInputNode]*layout.Box, len(oofSet))
	for _, child := range box.Children {
		if oofSet[child] && child.LayoutNode != nil {
			byLIN[child.LayoutNode] = child
		}
	}

	// Collect in-flow children in their original (already DOM-correct) order.
	inFlow := make([]*layout.Box, 0, len(box.Children)-len(oofSet))
	for _, child := range box.Children {
		if !oofSet[child] {
			inFlow = append(inFlow, child)
		}
	}

	// Walk lin.Children() (DOM order) to interleave out-of-flow boxes at their
	// correct positions. Each non-OOF LIN child corresponds to one in-flow box
	// (block, anonymous block, or continuation). OOF LIN children are inserted
	// at their DOM position without consuming an in-flow slot.
	result := make([]*layout.Box, 0, len(box.Children))
	inserted := make(map[*layout.Box]bool)
	inFlowIdx := 0
	for _, linChild := range lin.Children() {
		if cb, ok := byLIN[linChild]; ok {
			// Out-of-flow child: insert at this DOM position.
			result = append(result, cb)
			inserted[cb] = true
		} else if linChild.IsText() {
			// Text node: block layout never emits a box child for text nodes
			// (they are handled by inline layout inside anonymous blocks).
			// Skip without consuming an inFlow slot.
			continue
		} else {
			// In-flow (or anonymous) child: emit next in-flow box in order.
			if inFlowIdx < len(inFlow) {
				result = append(result, inFlow[inFlowIdx])
				inFlowIdx++
			}
		}
	}
	// Emit any remaining in-flow boxes (e.g. line boxes with no LIN).
	for ; inFlowIdx < len(inFlow); inFlowIdx++ {
		result = append(result, inFlow[inFlowIdx])
	}
	// Insert OOF children that propagated up from descendants (not direct
	// DOM children of this box) at their correct DOM position.
	// These weren't found during the DOM walk above because their
	// LayoutInputNode lives deeper in the tree. We use DOMIndex to find
	// the ancestor that IS a direct child of this box, then insert the
	// OOF child just before that ancestor so it's processed in correct
	// DOM tree order relative to positioned descendants encountered
	// during subtree recursion (CSS 2.1 Appendix E).
	for _, child := range box.Children {
		if oofSet[child] && !inserted[child] {
			pos := len(result)
			// Find the DOMIndex of the direct child (of this box's LIN)
			// that is an ancestor of this OOF child. The OOF child should
			// be painted within that ancestor's subtree.
			if child.LayoutNode != nil && lin != nil {
				ancestorIdx := findAncestorDOMIndex(child.LayoutNode, lin)
				if ancestorIdx >= 0 {
					// Insert just before the ancestor in the result, so the
					// OOF child is processed before the ancestor's subtree.
					for k := 0; k < len(result); k++ {
						if result[k].DOMIndex == ancestorIdx {
							pos = k
							break
						}
					}
				}
			}
			result = append(result, nil)
			copy(result[pos+1:], result[pos:])
			result[pos] = child
		}
	}
	return result
}

// findAncestorDOMIndex walks up from lin's DOM node to find the
// LayoutInputNode that is a direct child of parentLIN, returning its DOMIndex.
// Returns -1 if not found.
func findAncestorDOMIndex(lin, parentLIN *layout.LayoutInputNode) int {
	if lin.DOMNode == nil || parentLIN.DOMNode == nil {
		return -1
	}
	parentDOM := parentLIN.DOMNode
	// Walk up the DOM tree from lin's node to find the child of parentDOM.
	node := lin.DOMNode
	for node != nil && node.Parent != parentDOM {
		node = node.Parent
	}
	if node == nil {
		return -1
	}
	// node is a direct child of parentDOM. Find its LIN.
	for _, ch := range parentLIN.Children() {
		if ch.DOMNode == node {
			return ch.DOMIndex
		}
	}
	return -1
}

// isFlexContainer returns true if box is a flex or inline-flex container.
func isFlexContainer(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	d := box.Style.GetDisplay()
	return d == css.DisplayFlex || d == css.DisplayInlineFlex
}

// paintOrderChildren returns the children of box in the order they should
// be painted. For flex containers, box.Children is already in order-modified
// document order (sorted by the CSS order property during flex layout).
// For other boxes, DOM tree order is used.
func paintOrderChildren(box *layout.Box) []*layout.Box {
	if isFlexContainer(box) {
		// Flex layout produces children in order-modified document order
		// (sorted by CSS order property, ties broken by DOM order).
		// Use this order directly — do NOT re-sort to DOM order.
		return box.Children
	}
	return domOrderedChildren(box)
}

// buildPaintSubtree walks the Box tree, creating PaintLayers and assigning
// them to the correct parent/stacking-context lists.
//
// parentLayer: the PaintLayer that owns FlowChildren at this level.
// currentSC:   the nearest ancestor stacking context's PaintLayer.
func buildPaintSubtree(box *layout.Box, parentLayer, currentSC *PaintLayer) {
	for _, child := range paintOrderChildren(box) {
		if child.Style == nil {
			// Unstyled box (line box, text run) — no PaintLayer.
			// Recurse to find any styled descendants.
			buildPaintSubtree(child, parentLayer, currentSC)
			continue
		}

		childLayer := newPaintLayer(child)
		isPositioned := child.Position != css.PositionStatic && child.Position != ""

		// CSS Flexbox §4.3: Flex items with explicit z-index create stacking
		// contexts even if position is static. They participate in the nearest
		// ancestor stacking context's z-lists, just like positioned elements
		// with explicit z-index.
		if !isPositioned && child.IsFlexItem() && child.Style.HasExplicitZIndex() {
			z := child.ZIndex
			switch {
			case z < 0:
				currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
			case z > 0:
				currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
			default:
				currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			}
			// New stacking context — descendants collected by childLayer.
			buildPaintSubtree(child, childLayer, childLayer)
			continue
		}

		if !isPositioned {
			// CSS Flexbox §4.3: flex items with explicit z-index create stacking
			// contexts even when position:static. They participate in z-index sorting.
			if child.CreatesStackingContext() {
				z := child.ZIndex
				switch {
				case z < 0:
					currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
				case z > 0:
					currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
				default:
					currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
				}
				buildPaintSubtree(child, childLayer, childLayer)
			} else if isFloat(child) {
				// CSS 2.1 Appendix E step 4: floats paint after non-float block
				// backgrounds (step 3) so they appear above block backgrounds.
				parentLayer.FloatChildren = append(parentLayer.FloatChildren, childLayer)
				buildPaintSubtree(child, childLayer, currentSC)
			} else {
				parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
				buildPaintSubtree(child, childLayer, currentSC)
			}
			continue
		}

		// Positioned child forming a stacking context. Per CSS 2.1 Appendix E,
		// z-index ordering takes precedence over overflow containment — the
		// child is z-sorted in the nearest ancestor stacking context even
		// when clipped by a parent's overflow. Clipping is applied at paint
		// time; it does not change z-order.
		if child.CreatesStackingContext() {
			z := child.ZIndex
			switch {
			case z < 0:
				currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
			case z > 0:
				currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
			default:
				currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			}
			// New stacking context — descendants collected by childLayer.
			buildPaintSubtree(child, childLayer, childLayer)
			continue
		}

		// Positioned child without stacking context. Check overflow clip:
		// contained children stay in DOM-order painting (FlowChildren).
		if isContainedByOverflow(child, box) {
			parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
			buildPaintSubtree(child, childLayer, currentSC)
			continue
		}

		// Positioned, z-index:auto, not contained — participates at Appendix E step 6.
		currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
		if hasOverflowClipping(child) {
			// Overflow containment boundary — positioned descendants
			// stay within this subtree.
			buildPaintSubtree(child, childLayer, childLayer)
		} else {
			buildPaintSubtree(child, childLayer, currentSC)
		}
	}
}

// isFloat returns true if the box has float:left or float:right.
func isFloat(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	f := box.Style.GetFloat()
	return f == css.FloatLeft || f == css.FloatRight
}

// parseFontFeatureSettings parses a CSS font-feature-settings value like
// `"kern" 1, "liga" 0` into a slice of FontFeature.
// CSS syntax: each entry is a 4-character tag in quotes, optionally followed by
// an integer value (default 1) or "on"/"off".
func parseFontFeatureSettings(value string) []textshape.FontFeature {
	var features []textshape.FontFeature
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "normal" {
			continue
		}
		// Split into tag and optional value.
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		// Extract tag: must be a quoted 4-character string.
		tag := fields[0]
		tag = strings.Trim(tag, `"'`)
		if len(tag) != 4 {
			continue
		}
		// Parse value: default is 1 (on).
		val := uint32(1)
		if len(fields) > 1 {
			switch strings.ToLower(fields[1]) {
			case "on":
				val = 1
			case "off":
				val = 0
			default:
				// Parse integer.
				n := 0
				for _, c := range fields[1] {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					} else {
						break
					}
				}
				val = uint32(n)
			}
		}
		features = append(features, textshape.FontFeature{
			Tag:   [4]byte{tag[0], tag[1], tag[2], tag[3]},
			Value: val,
		})
	}
	return features
}

// sortZLists sorts NegativeZ and PositiveZ by z-index (ascending),
// then recurses into all child layers.
func (layer *PaintLayer) sortZLists() {
	sort.SliceStable(layer.NegativeZ, func(i, j int) bool {
		return layer.NegativeZ[i].ZIndex < layer.NegativeZ[j].ZIndex
	})
	sort.SliceStable(layer.PositiveZ, func(i, j int) bool {
		return layer.PositiveZ[i].ZIndex < layer.PositiveZ[j].ZIndex
	})
	for _, child := range layer.NegativeZ {
		child.sortZLists()
	}
	for _, child := range layer.AutoZero {
		child.sortZLists()
	}
	for _, child := range layer.PositiveZ {
		child.sortZLists()
	}
	for _, child := range layer.FlowChildren {
		child.sortZLists()
	}
	for _, child := range layer.FloatChildren {
		child.sortZLists()
	}
}


