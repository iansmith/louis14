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
		emitSubLineTextFragments(
			anno, annoInlineOffset, annoBaseline,
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

// emitSubLineTextFragments walks a sub-LineInfo's Results and emits
// a PhysicalFragment for each text item at the appropriate offset
// inside lineBuilder. inlineOrigin is where the sub-line's first text
// item starts (in line-local coords); blockBaseline is the block-axis
// position of the alphabetic baseline (so each fragment is placed at
// blockBaseline - itemAscent). textContent is the shared outer
// InlineItemsData.TextContent.
//
// justifyExpansion is the per-space-character inline expansion applied
// for ruby-align: space-between and space-around distribution. Zero
// means no expansion (start, center, or the wider side of the column).
//
// This is a slimmed-down version of the createLineBoxEx text path:
// no vertical-align stack, no first-letter / first-line styling, no
// decorating-box metadata propagation. Span backgrounds inside a
// ruby base (OpenTag/CloseTag fragment generation) are not emitted
// here either — TODO Phase 5: ruby-base inline backgrounds.
func emitSubLineTextFragments(
	subLine *LineInfo,
	inlineOrigin float64,
	blockBaseline float64,
	textContent string,
	justifyExpansion float64,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
	sidewaysVLR bool,
	lineBuilder *BoxFragmentBuilder,
) {
	inlinePos := inlineOrigin
	for _, r := range subLine.Results {
		switch r.Item.Type {
		case InlineItemText:
			// CSS Writing Modes 3 §9.1.1: text-combine-upright: all in a
			// vertical writing mode renders the combined text as a 1em upright
			// unit (tcy run). In our sub-line rendering pipeline this item
			// occupies inline space (already counted in rubySize) but does not
			// emit a fragment directly — it behaves like an orthogonal atomic
			// inline whose LayoutResult is nil, which emitSubLineTextFragments
			// treats as the default (advance-only) case. Full tcy rendering
			// (horizontal glyph inside the vertical column) is a later phase.
			if wdm.IsVertical() && r.Item.Style != nil && r.Item.Style.GetTextCombineUpright() {
				inlinePos += r.InlineSize
				continue
			}
			content := textContent[r.TextStart:r.TextEnd]
			if len(content) == 0 {
				inlinePos += r.InlineSize
				continue
			}
			// CSS Text 3 §5.2: strip soft hyphens (U+00AD) — invisible
			// when not used as a break point.
			content = strings.ReplaceAll(content, "­", "")
			if r.HasHyphen {
				content += "-"
			}

			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent float64
			if centralBaseline {
				ascent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				// CSS Fonts 4 §6.1: same as createLineBoxEx text path — use native
				// ascent for per-item placement so the glyph lands at blockBaseline -
				// nativeAscent, not blockBaseline - overriddenAscent. The override
				// affects line-box sizing (strut), not glyph positioning within the box.
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, nil)
			}
			blockPos := blockBaseline - ascent

			parentNode := r.Item.Node
			if parentNode != nil && parentNode.Parent != nil {
				parentNode = parentNode.Parent
			}

			// ruby-align: space-between/space-around — when justifyExpansion > 0,
			// split text at space boundaries and emit one fragment per piece, with
			// each space expanded by justifyExpansion. Mirrors the justify-expansion
			// path in createLineBoxEx (inline_layout.go) applied to ruby sub-lines.
			if justifyExpansion > 0 && strings.Contains(content, " ") {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				letterSpacing := r.Item.Style.GetLetterSpacing()
				wordSpacing := r.Item.Style.GetWordSpacing()
				spaceWidth := measureTextContent(" ", fontSize, fontPath, letterSpacing, 0, false)
				pieces := strings.Split(content, " ")
				for i, piece := range pieces {
					if i > 0 {
						spFrag := &PhysicalFragment{
							Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
								InlineSize: spaceWidth + justifyExpansion + wordSpacing,
								BlockSize:  fontSize,
							}, wdm.WM)),
							Type:             FragmentText,
							TextContent:      " ",
							BidiLevel:        r.Item.BidiLevel,
							Node:             parentNode,
							Style:            r.Item.Style,
							WritingDirection: wdm,
						}
						lineBuilder.AddChild(spFrag, LogicalOffset{
							InlineOffset: inlinePos,
							BlockOffset:  blockPos,
						})
						inlinePos += spaceWidth + justifyExpansion + wordSpacing
					}
					if len(piece) == 0 {
						continue
					}
					pieceWidth := measureTextContent(piece, fontSize, resolveFontPath(r.Item.Style, fonts), letterSpacing, 0, false)
					pieceFrag := &PhysicalFragment{
						Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
							InlineSize: pieceWidth,
							BlockSize:  fontSize,
						}, wdm.WM)),
						Type:             FragmentText,
						TextContent:      piece,
						BidiLevel:        r.Item.BidiLevel,
						Node:             parentNode,
						Style:            r.Item.Style,
						WritingDirection: wdm,
					}
					lineBuilder.AddChild(pieceFrag, LogicalOffset{
						InlineOffset: inlinePos,
						BlockOffset:  blockPos,
					})
					inlinePos += pieceWidth
				}
				continue
			}

			textFrag := &PhysicalFragment{
				Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
					InlineSize: r.InlineSize,
					BlockSize:  fontSize,
				}, wdm.WM)),
				Type:             FragmentText,
				TextContent:      content,
				BidiLevel:        r.Item.BidiLevel,
				Node:             parentNode,
				Style:            r.Item.Style,
				WritingDirection: wdm,
			}
			// TODO Phase 5: position:relative offset propagation for
			// sub-line text fragments (rare in ruby content).
			lineBuilder.AddChild(textFrag, LogicalOffset{
				InlineOffset: inlinePos,
				BlockOffset:  blockPos,
			})
			inlinePos += r.InlineSize
		case InlineItemOpenTag, InlineItemCloseTag,
			InlineItemRubyLinePlaceholder, InlineItemCloseRubyColumn:
			// Zero-width markers — no glyph contribution, no advance.
			// TODO Phase 5: emit span backgrounds for OpenTag/CloseTag
			// inside ruby bases (e.g. `<rb style="background: red">`).
		default:
			// Fallback for item types this sub-line emitter doesn't
			// yet handle. The dominant case is a nested
			// `InlineItemOpenRubyColumn` (a `<ruby>` inside another
			// ruby's base or annotation sub-line) — full nested-ruby
			// rendering is Phase 11 of `docs/plan-css-ruby.md`
			// (multi-level RubyLevel/RubyLine stacking; Blink
			// `ruby_utils.{h,cc}` + `ParseRubyInInlineItems`
			// recursion `:170-173` @ 4883d11fef). Atomic inlines,
			// floats, and OOFs inside ruby columns land in Phase 13.
			//
			// Until those phases land, the result-level metrics are
			// already counted into the column's InlineSize by
			// LineBreaker.handleRuby, so we MUST advance inlinePos by
			// r.InlineSize to keep subsequent fragments at their
			// correct positions on the sub-line. We just don't emit a
			// fragment for the inner content — so the inner glyphs
			// are silently dropped (the visual symptom Phase 11
			// fixes; was LOU-156 item 2, split out to its own child
			// ticket).
			inlinePos += r.InlineSize
		}
	}
}
