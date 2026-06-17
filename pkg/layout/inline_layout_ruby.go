package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

// surfaceRubyColumnOOFCandidates surfaces out-of-flow descendants buried in a
// ruby column's base and annotation sub-lines as OOF candidates on the outer
// line's builder. An OOF child of a ruby element (e.g. an abs-pos <span>
// inside an <rb>) is collected into the column's sub-LineInfo by
// CreateSubLineInfo, so it never appears in the outer line.Results and would
// otherwise be dropped. The inline containing block comes from the
// already-built positionedInlineMap (keyed by the shared *InlineItem
// pointers), which maps the OOF to its nearest positioned inline ancestor
// (the rel <rb>/<rbc>/<ruby>). Recurses into nested ruby columns.
//
// Mirrors Blink's InlineLayoutAlgorithm propagating positioned descendants
// found inside ruby columns into the algorithm's OOF candidate list (ruby
// vetted @ Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). The surfaced
// candidate is later resolved by the normal ComputeInlineContainerGeometry
// path; correct geometry for <rb>/<rbc> additionally needs their sub-line box
// fragments (LOU-311).
func surfaceRubyColumnOOFCandidates(
	column *InlineItemResultRubyColumn,
	inlineOrigin, blockOffset float64,
	positionedInlineMap map[*InlineItem]*html.Node,
	builder *BoxFragmentBuilder,
) {
	if column == nil {
		return
	}
	surfaceSubLineOOFCandidates(column.BaseLine, inlineOrigin, blockOffset, positionedInlineMap, builder)
	for _, anno := range column.AnnotationLines {
		surfaceSubLineOOFCandidates(anno, inlineOrigin, blockOffset, positionedInlineMap, builder)
	}
}

func surfaceSubLineOOFCandidates(
	line *LineInfo,
	inlineOrigin, blockOffset float64,
	positionedInlineMap map[*InlineItem]*html.Node,
	builder *BoxFragmentBuilder,
) {
	if line == nil {
		return
	}
	for i := range line.Results {
		r := &line.Results[i]
		if r.Item == nil {
			continue
		}
		if r.RubyColumn != nil {
			surfaceRubyColumnOOFCandidates(r.RubyColumn, inlineOrigin, blockOffset, positionedInlineMap, builder)
			continue
		}
		if r.Item.Type != InlineItemOutOfFlow || r.Item.LayoutNode == nil {
			continue
		}
		// All sub-line OOFs are stamped at the column's inline start
		// (inlineOrigin); we do not advance per item within the sub-line. The
		// static position only matters for OOFs auto-positioned on the inline
		// axis (no left/right) — the abs-in-ruby cases resolve via insets.
		builder.AddOutOfFlowCandidate(inlineOOFCandidate(
			r.Item.LayoutNode, inlineOrigin, blockOffset,
			r.Item.Style != nil && r.Item.Style.GetPosition() == css.PositionFixed,
			positionedInlineMap[r.Item],
		))
	}
}

// emitRubyColumnFragments paints a single ruby column's base + annotation
// sub-line glyphs into lineBuilder, positioned at the given inline /
// block offsets within the outer line.
//
//   - baseBlockBaseline is the outer line's main baseline (= the line's
//     maxAscent before the ruby annotation-growth, i.e. the line's
//     font-derived ascent).
//   - annotationBlockTop is the block-axis position of the top of the
//     over-side annotation strip (= 0 in line-local coords for the
//     default `ruby-position: over` case where the line grew its
//     ascent to make room above).
//
// `textContent` is the outer InlineItemsData.TextContent — the
// sub-LineBreaker (CreateSubLineInfo) shares the same backing buffer,
// so item TextStart/TextEnd offsets index directly into it.
//
// `rubyAlign` is the computed ruby-align value from the enclosing <ruby>
// element, used to distribute the shorter sub-line within the column slot.
// Mirrors Blink's ApplyRubyAlign at
// core/layout/inline/ruby_utils.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Phase 2 supports the single-level case (`ruby-position: over` only).
// Mirrors Blink's inline_layout_algorithm.cc:396-418 column placement
// flow at @ 4883d11fef — combined with UpdateRubyColumnInlinePositions
// (called by createLineBoxEx) and RubyBlockPositionCalculator
// (already integrated into the line-metrics adjustment).
func emitRubyColumnFragments(
	column *InlineItemResultRubyColumn,
	columnInlineOffset float64,
	baseBlockBaseline float64,
	annotationBlockTop float64,
	lineHeight float64,
	maxAscent float64,
	textContent string,
	rubyAlign css.RubyAlign,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
	sidewaysVLR bool,
	lineBuilder *BoxFragmentBuilder,
) {
	// Base sub-line: apply ruby-align distribution when the base is the
	// narrower side of the column (annotation is wider).
	//
	// CSS Ruby 1 §7: ruby-align distributes the shorter of the two sub-lines
	// within the column's InlineSize. Mirrors Blink's ApplyRubyAlign path
	// in ruby_utils.cc @ 4883d11fef.
	if column.BaseLine != nil {
		baseOffset, baseExpansion := rubyAlignSubLineOffset(
			column.BaseLine.Width, column.InlineSize,
			countSubLineSpaces(column.BaseLine, textContent),
			rubyAlign,
		)
		emitSubLineTextFragments(
			column.BaseLine, columnInlineOffset+baseOffset, baseBlockBaseline,
			baseBlockBaseline-maxAscent, lineHeight,
			textContent, baseExpansion, wdm, fonts, centralBaseline, sidewaysVLR, lineBuilder,
		)
	}

	// Annotation sub-lines: apply ruby-align distribution when the annotation
	// is the narrower side (base is wider), otherwise fall back to center (the
	// historical Phase 2 default).
	//
	// Per CSS Ruby 1 §7, the annotation gets the same alignment treatment as
	// the base — both the shorter side and the longer side participate.
	// Phase 2 default: when annotation is narrower, apply ruby-align;
	// when annotation is wider or equal, it already fills the column so no
	// offset is added. This mirrors Blink's behavior for the common case.
	for _, anno := range column.AnnotationLines {
		if anno == nil {
			continue
		}
		// Annotation distribution: apply ruby-align when annotation is narrower;
		// use a fixed center fallback when annotation == column (i.e. it IS the
		// wider side), as Blink does for the non-text annotation case.
		var annoInlineOffset float64
		if anno.Width < column.InlineSize {
			offset, _ := rubyAlignSubLineOffset(
				anno.Width, column.InlineSize, 0, rubyAlign,
			)
			annoInlineOffset = columnInlineOffset + offset
		} else {
			annoInlineOffset = columnInlineOffset
		}
		// Mirror Blink's annotation positioning: layout works in sub-pixel
		// font typo metrics, paint snaps to integer ascent (`int_ascent_`
		// via `lroundf` at `text_fragment_painter.cc:517-522 @
		// 574216cbb0c2b86a39c1d41ad85b2891a050b44c`). emitSubLineTextFragments
		// subtracts the per-item rounded ascent from blockBaseline to get
		// blockPos; pre-correcting annoBaseline with `math.Round(annoEmAscent)`
		// makes that subtraction cancel cleanly so the painted baseline
		// lands at `lineBoxTop + annotationBlockTop + ascent_unrounded` —
		// the same place the renderer paints any other text fragment, and
		// the place emphasis-mark Y is computed relative to.
		annoEmAscent, _ := annotationEmHeightFromSubLine(anno, wdm, fonts, centralBaseline)
		annoBaseline := annotationBlockTop + math.Round(annoEmAscent)
		// Annotation inline boxes keep the em-box (baseline-anchored) model:
		// pass a zero band so emitSubLineTextFragments takes the em-box branch.
		emitSubLineTextFragments(
			anno, annoInlineOffset, annoBaseline,
			0, 0,
			textContent, 0, wdm, fonts, centralBaseline, sidewaysVLR, lineBuilder,
		)
	}
}

// rubyAlignSubLineOffset computes the inline start offset and per-space
// justification expansion for a sub-line of width `lineWidth` within a
// column slot of size `slotWidth`, according to the given ruby-align value.
//
// Returns (startOffset, spaceExpansion) where:
//   - startOffset is added to columnInlineOffset before the first fragment.
//   - spaceExpansion is the per-space-character expansion for space-between/
//     space-around (0 if no expansion is needed).
//
// CSS Ruby 1 §7 — https://drafts.csswg.org/css-ruby-1/#ruby-align-property.
// Mirrors Blink's ruby-align justification in ApplyRubyAlign at
// core/layout/inline/ruby_utils.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func rubyAlignSubLineOffset(lineWidth, slotWidth float64, spaceCount int, align css.RubyAlign) (startOffset, spaceExpansion float64) {
	slack := slotWidth - lineWidth
	if slack <= 0 {
		return 0, 0
	}
	switch align {
	case css.RubyAlignStart:
		return 0, 0
	case css.RubyAlignCenter:
		return slack / 2, 0
	case css.RubyAlignSpaceBetween:
		if spaceCount > 0 {
			// Distribute all slack across space characters (text-align-last:
			// justify semantics); no edge padding.
			return 0, slack / float64(spaceCount)
		}
		// No spaces — fall back to centering (same as Blink for space-between
		// when there are no expansion points).
		return slack / 2, 0
	default: // space-around
		if spaceCount > 0 {
			// Space-around: half-space at each edge + full-space between.
			// n gaps (spaces) + 2 half-edges = n+1 units total.
			// Each space gets 1 unit, each edge gets 0.5 unit.
			unit := slack / float64(spaceCount+1)
			return unit / 2, unit
		}
		// No spaces — center the content.
		return slack / 2, 0
	}
}

// countSubLineSpaces counts ASCII space characters in text items of a
// sub-LineInfo. Used to compute ruby-align space-between/space-around
// expansion points. Mirrors the opportunity-counting in
// countJustifyOpportunities (inline_layout.go) but for sub-lines.
func countSubLineSpaces(line *LineInfo, textContent string) int {
	if line == nil {
		return 0
	}
	n := 0
	for _, r := range line.Results {
		if r.Item == nil || r.Item.Type != InlineItemText {
			continue
		}
		if r.TextEnd <= r.TextStart || r.TextEnd > len(textContent) {
			continue
		}
		n += strings.Count(textContent[r.TextStart:r.TextEnd], " ")
	}
	return n
}

// openSubLineBox tracks an inline box (<rb>/<rbc>/<span>…) opened inside a
// ruby sub-line so its matching close tag can emit a box fragment spanning the
// content between them.
type openSubLineBox struct {
	node        *html.Node
	style       *css.Style
	startInline float64
}

// subLineEmitter carries the shared context for rendering one ruby sub-line's
// fragments. Held as a value struct (copying only the fields the per-item
// helpers need) so those helpers stay small without threading a dozen
// parameters — and without a closure capturing the enclosing scope.
type subLineEmitter struct {
	textContent      string
	blockBaseline    float64
	boxBandTop       float64
	boxBandHeight    float64
	justifyExpansion float64
	wdm              WritingDirectionMode
	fonts            text.FontConfig
	centralBaseline  bool
	sidewaysVLR      bool
	lineBuilder      *BoxFragmentBuilder
}

// emitSubLineTextFragments walks a sub-LineInfo's Results and emits a
// PhysicalFragment for each text run and inline box. inlineOrigin is where the
// sub-line's first item starts (in line-local coords); blockBaseline is the
// block-axis position of the alphabetic baseline. textContent is the shared
// outer InlineItemsData.TextContent.
//
// justifyExpansion is the per-space-character inline expansion applied for
// ruby-align: space-between / space-around. boxBandTop/boxBandHeight describe
// the line band an inline box fills (zero band → em-box model).
//
// This is a slimmed-down version of the createLineBoxEx text path: no
// vertical-align stack, no first-letter / first-line styling, no decorating-box
// metadata. Inline box fragments carry geometry only — backgrounds/borders are
// not painted here yet (the remaining half of the former Phase 5 TODO).
func emitSubLineTextFragments(
	subLine *LineInfo,
	inlineOrigin float64,
	blockBaseline float64,
	boxBandTop float64,
	boxBandHeight float64,
	textContent string,
	justifyExpansion float64,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
	sidewaysVLR bool,
	lineBuilder *BoxFragmentBuilder,
) {
	e := subLineEmitter{
		textContent:      textContent,
		blockBaseline:    blockBaseline,
		boxBandTop:       boxBandTop,
		boxBandHeight:    boxBandHeight,
		justifyExpansion: justifyExpansion,
		wdm:              wdm,
		fonts:            fonts,
		centralBaseline:  centralBaseline,
		sidewaysVLR:      sidewaysVLR,
		lineBuilder:      lineBuilder,
	}
	inlinePos := inlineOrigin
	var boxStack []openSubLineBox
	for _, r := range subLine.Results {
		switch r.Item.Type {
		case InlineItemText:
			inlinePos = e.emitTextItem(r, inlinePos)
		case InlineItemOpenTag:
			// Record the inline start; the close tag spans to the cursor.
			boxStack = append(boxStack, openSubLineBox{
				node: r.Item.Node, style: r.Item.Style, startInline: inlinePos,
			})
		case InlineItemCloseTag:
			boxStack = e.emitInlineBox(boxStack, inlinePos)
		case InlineItemRubyLinePlaceholder, InlineItemCloseRubyColumn:
			// Zero-width markers — no glyph contribution, no advance.
		default:
			// Nested ruby columns / atomic inlines / floats / OOFs inside ruby
			// columns aren't rendered here yet (plan-css-ruby Phase 11/13,
			// Blink ruby_utils.{h,cc} @ 4883d11fef), but their inline size is
			// already in the column width, so advance the cursor to keep later
			// fragments correctly placed. Inner glyphs are silently dropped
			// until those phases (was LOU-156 item 2).
			inlinePos += r.InlineSize
		}
	}
}

// emitTextItem emits the fragment(s) for one InlineItemText result and returns
// the advanced inline cursor.
func (e subLineEmitter) emitTextItem(r InlineItemResult, inlinePos float64) float64 {
	// CSS Writing Modes 3 §9.1.1: text-combine-upright: all in a vertical mode
	// occupies inline space (already in the column width) but emits no fragment
	// here — full tcy rendering is a later phase.
	if e.wdm.IsVertical() && r.Item.Style != nil && r.Item.Style.GetTextCombineUpright() {
		return inlinePos + r.InlineSize
	}
	content := e.textContent[r.TextStart:r.TextEnd]
	if len(content) == 0 {
		return inlinePos + r.InlineSize
	}
	// CSS Text 3 §5.2: strip soft hyphens (U+00AD) — invisible off-break.
	content = strings.ReplaceAll(content, "­", "")
	if r.HasHyphen {
		content += "-"
	}

	fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
	var ascent float64
	if e.centralBaseline {
		ascent = fontSize / 2
	} else {
		// CSS Fonts 4 §6.1: native ascent for per-item placement (matches the
		// createLineBoxEx text path); the strut override affects line sizing,
		// not glyph position within the box.
		ascent = alignmentAscentFromFont(e.sidewaysVLR, fontSize, resolveFontPath(r.Item.Style, e.fonts), nil)
	}
	blockPos := e.blockBaseline - ascent

	parentNode := r.Item.Node
	if parentNode != nil && parentNode.Parent != nil {
		parentNode = parentNode.Parent
	}

	// ruby-align space-between/space-around: split at spaces, expand each.
	if e.justifyExpansion > 0 && strings.Contains(content, " ") {
		return e.emitJustifiedText(r, content, inlinePos, blockPos, fontSize, parentNode)
	}

	e.lineBuilder.AddChild(&PhysicalFragment{
		Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
			InlineSize: r.InlineSize,
			BlockSize:  fontSize,
		}, e.wdm.WM)),
		Type:             FragmentText,
		TextContent:      content,
		BidiLevel:        r.Item.BidiLevel,
		Node:             parentNode,
		Style:            r.Item.Style,
		WritingDirection: e.wdm,
	}, LogicalOffset{InlineOffset: inlinePos, BlockOffset: blockPos})
	// TODO Phase 5: position:relative offset propagation for sub-line text
	// fragments (rare in ruby content).
	return inlinePos + r.InlineSize
}

// emitJustifiedText emits one text run split at spaces, expanding each space by
// justifyExpansion (ruby-align space-between/space-around). Mirrors the
// justify-expansion path in createLineBoxEx applied to ruby sub-lines.
func (e subLineEmitter) emitJustifiedText(r InlineItemResult, content string, inlinePos, blockPos, fontSize float64, parentNode *html.Node) float64 {
	fontPath := resolveFontPath(r.Item.Style, e.fonts)
	letterSpacing := r.Item.Style.GetLetterSpacing()
	wordSpacing := r.Item.Style.GetWordSpacing()
	spaceWidth := measureTextContent(" ", fontSize, fontPath, letterSpacing, 0, false)
	for i, piece := range strings.Split(content, " ") {
		if i > 0 {
			e.lineBuilder.AddChild(&PhysicalFragment{
				Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
					InlineSize: spaceWidth + e.justifyExpansion + wordSpacing,
					BlockSize:  fontSize,
				}, e.wdm.WM)),
				Type:             FragmentText,
				TextContent:      " ",
				BidiLevel:        r.Item.BidiLevel,
				Node:             parentNode,
				Style:            r.Item.Style,
				WritingDirection: e.wdm,
			}, LogicalOffset{InlineOffset: inlinePos, BlockOffset: blockPos})
			inlinePos += spaceWidth + e.justifyExpansion + wordSpacing
		}
		if len(piece) == 0 {
			continue
		}
		pieceWidth := measureTextContent(piece, fontSize, fontPath, letterSpacing, 0, false)
		e.lineBuilder.AddChild(&PhysicalFragment{
			Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
				InlineSize: pieceWidth,
				BlockSize:  fontSize,
			}, e.wdm.WM)),
			Type:             FragmentText,
			TextContent:      piece,
			BidiLevel:        r.Item.BidiLevel,
			Node:             parentNode,
			Style:            r.Item.Style,
			WritingDirection: e.wdm,
		}, LogicalOffset{InlineOffset: inlinePos, BlockOffset: blockPos})
		inlinePos += pieceWidth
	}
	return inlinePos
}

// emitInlineBox pops the innermost open box and, if it paints or establishes a
// containing block, emits its box fragment — the rect
// ComputeInlineContainerGeometry resolves an OOF descendant against
// (LOU-311/LOU-312). Returns the updated stack. BoxData is omitted: these
// sub-line boxes carry geometry only, no painted background/border yet.
func (e subLineEmitter) emitInlineBox(boxStack []openSubLineBox, inlinePos float64) []openSubLineBox {
	if len(boxStack) == 0 {
		return boxStack
	}
	ob := boxStack[len(boxStack)-1]
	boxStack = boxStack[:len(boxStack)-1]
	if ob.node == nil || ob.style == nil {
		return boxStack
	}
	// Gate like the normal inline path (createLineBoxEx): only inlines that
	// paint or establish a containing block need a fragment.
	if !hasVisibleInlinePaint(ob.style) && !inlineEstablishesContainingBlock(ob.style) {
		return boxStack
	}
	// Vertical model from createLineBoxEx (spanContentBlock): fill the line band
	// when the font is shorter than the line, else an em box on the baseline.
	boxFontSize, boxAscent := inlineAlignmentAscent(ob.style, e.fonts, e.wdm, e.centralBaseline)
	boxTop, boxBlockSize := e.blockBaseline-boxAscent, boxFontSize
	if e.boxBandHeight > 0 && boxFontSize < e.boxBandHeight {
		boxTop, boxBlockSize = e.boxBandTop, e.boxBandHeight
	}
	e.lineBuilder.AddChild(&PhysicalFragment{
		Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
			InlineSize: inlinePos - ob.startInline,
			BlockSize:  boxBlockSize,
		}, e.wdm.WM)),
		Type:             FragmentBox,
		Node:             ob.node,
		Style:            ob.style,
		WritingDirection: e.wdm,
	}, LogicalOffset{InlineOffset: ob.startInline, BlockOffset: boxTop})
	return boxStack
}
