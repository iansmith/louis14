package layout

import (
	"strings"

	"louis14/pkg/text"
)

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
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
	sidewaysVLR bool,
	lineBuilder *BoxFragmentBuilder,
) {
	// Base sub-line: paint its text fragments at the column's inline
	// position, with each fragment baseline-aligned to baseBlockBaseline.
	if column.BaseLine != nil {
		emitSubLineTextFragments(
			column.BaseLine, columnInlineOffset, baseBlockBaseline,
			textContent, wdm, fonts, centralBaseline, sidewaysVLR, lineBuilder,
		)
	}

	// Annotation sub-lines: center each annotation horizontally within
	// the column's `InlineSize` slot (so a narrow annotation appears
	// centered over a wider base) and stack them above the base by
	// their own ascent so the annotation's baseline sits at
	// (annotationBlockTop + annotation.ascent). Phase 2 places all
	// annotations on the over side; multi-level / under support is
	// Phase 11.
	for _, anno := range column.AnnotationLines {
		if anno == nil {
			continue
		}
		annoOffset := columnInlineOffset
		if anno.Width < column.InlineSize {
			annoOffset += (column.InlineSize - anno.Width) / 2
		}
		annoAscent, _ := computeLineMetricsEx(anno, wdm, fonts, centralBaseline, nil)
		annoBaseline := annotationBlockTop + annoAscent
		emitSubLineTextFragments(
			anno, annoOffset, annoBaseline,
			textContent, wdm, fonts, centralBaseline, sidewaysVLR, lineBuilder,
		)
	}
}

// emitSubLineTextFragments walks a sub-LineInfo's Results and emits
// a PhysicalFragment for each text item at the appropriate offset
// inside lineBuilder. inlineOrigin is where the sub-line's first text
// item starts (in line-local coords); blockBaseline is the block-axis
// position of the alphabetic baseline (so each fragment is placed at
// blockBaseline - itemAscent). textContent is the shared outer
// InlineItemsData.TextContent.
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
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
			}
			blockPos := blockBaseline - ascent

			parentNode := r.Item.Node
			if parentNode != nil && parentNode.Parent != nil {
				parentNode = parentNode.Parent
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
			// Atomic inlines, floats, OOFs inside ruby columns are
			// Phase 13 territory; skip for Phase 2 with zero advance.
			inlinePos += r.InlineSize
		}
	}
}
