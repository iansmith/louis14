package layout

// list_marker.go mirrors Blink's core/layout/list/list_marker.{h,cc}.
//
// ListMarker is the non-layout-object helper bag of per-marker logic. In Blink
// it is a member (list_marker_) of both LayoutInsideListMarker and
// LayoutOutsideListMarker; it is NOT itself a LayoutObject. Here it is a
// stateless value type carrying only pure-logic methods: list-style
// categorization, marker-text generation, symbol width, inline margins, and
// the relative symbol rect.
//
// Marker text comes from a pluggable MarkerTextSource so the marker box model
// is decoupled from the counter tree / counter style. The Phase-1 concrete
// implementation wraps the existing getListItemCounterValue / formatListMarker
// paint-time helpers (see layout_tree_builder.go); docs/plan-css-lists.md
// B1-B5 replaces that implementation with the CountersAttachmentContext +
// CounterStyle path without touching this file.

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// File-scope constants, mirroring list_marker.cc:28-35.
const (
	// kCMarkerPaddingPx — padding between an image/symbol marker and content.
	markerPaddingPx = 7
	// kCUAMarkerMarginEm — UA marker margin for symbol markers (inside).
	uaMarkerMarginEm = 1.0
	// kClosureMarkerMarginEm — margin for disclosure markers (inside).
	closureMarkerMarginEm = 0.4
	// disclosureSymbolSizeFactor — DisclosureSymbolSize multiplies the
	// specified font size by this factor (list_marker.cc:32-34).
	disclosureSymbolSizeFactor = 0.66
)

// disclosureSymbolSize mirrors list_marker.cc:32-34
// DisclosureSymbolSize(const ComputedStyle&). EffectiveZoom is always 1.0 in
// louis14, so it is omitted.
func disclosureSymbolSize(style *css.Style) layoutunit.LayoutUnit {
	return layoutunit.FromFloat64Round(style.GetFontSize() * disclosureSymbolSizeFactor)
}

// ListStyleCategory mirrors Blink's ListMarker::ListStyleCategory
// (list_marker.h). It classifies a list-style-type for marker-text dispatch.
type ListStyleCategory int

const (
	// CategoryNone — no list-style (Blink kNone). Also used for image markers,
	// which are not a counter style.
	CategoryNone ListStyleCategory = iota
	// CategorySymbol — a predefined symbol marker: disc/circle/square and the
	// disclosure-* markers (Blink kSymbol).
	CategorySymbol
	// CategoryLanguage — an algorithmic / numeric counter style: decimal,
	// alpha, roman, greek, etc. (Blink kLanguage).
	CategoryLanguage
	// CategoryStaticString — a <string> list-style-type value (Blink
	// kStaticString).
	CategoryStaticString
)

// MarkerTextType mirrors Blink's ListMarker::MarkerTextType (private enum in
// list_marker.h). It is the text-resolution state / kind of the marker child.
type MarkerTextType int

const (
	// MarkerNotText — the marker has no text child (image marker, or a
	// content:none / non-normal content marker).
	MarkerNotText MarkerTextType = iota
	// MarkerUnresolved — the marker child exists but its text is not yet
	// resolved.
	MarkerUnresolved
	// MarkerOrdinalValue — text generated from the list-item ordinal value
	// through the counter style (kLanguage category).
	MarkerOrdinalValue
	// MarkerStatic — text is a static <string> list-style-type value.
	MarkerStatic
	// MarkerSymbolValue — text generated from a predefined symbol counter
	// style (kSymbol category).
	MarkerSymbolValue
)

// MarkerTextSource decouples the marker box model from the counter subsystem.
// The Phase-1 implementation (markerTextSourceLegacy in layout_tree_builder.go)
// wraps getListItemCounterValue and the render-time counter-style formatter;
// docs/plan-css-lists.md B1-B5 replaces it with the CountersAttachmentContext +
// CounterStyle path. No code outside that adapter may reach the legacy
// formatter directly.
type MarkerTextSource interface {
	// OrdinalValue returns the list-item counter value for kLanguage / kSymbol
	// category markers. Blink: counter(list-item) via the attachment context
	// (ListMarker::ListItemValue / ListItemOrdinal).
	OrdinalValue(item *LayoutInputNode) int
	// CounterStyleText formats an ordinal through the resolved counter style.
	// With withPrefixSuffix it includes the prefix/suffix (Blink:
	// CounterStyle::GenerateRepresentationWithPrefixAndSuffix); without, it is
	// the bare representation (CounterStyle::GenerateRepresentation).
	CounterStyleText(style *css.Style, value int, withPrefixSuffix bool) string
}

// ListMarker is the non-layout-object marker helper, mirroring Blink's
// ListMarker. It is stateless; all per-marker state lives on the marker
// LayoutInputNode.
type ListMarker struct{}

// isPredefinedSymbolListStyleType reports whether a list-style-type is one of
// the predefined symbol markers. Mirrors the kSymbol arm of
// GetListStyleCategory: a CounterStyle whose IsPredefinedSymbolMarker() is
// true — disc/circle/square and the disclosure-* markers.
func isPredefinedSymbolListStyleType(lst css.ListStyleType) bool {
	switch lst {
	case css.ListStyleTypeDisc, css.ListStyleTypeCircle, css.ListStyleTypeSquare,
		css.ListStyleTypeDisclosureOpen, css.ListStyleTypeDisclosureClosed:
		return true
	}
	return false
}

// isStringListStyleType reports whether a list-style-type value is a <string>
// (Blink ListStyleTypeData::IsString). In louis14 GetListStyleType strips the
// quotes from a quoted value and returns it verbatim, so a value that is
// neither a known keyword nor an algorithmic counter style name is treated as
// a static string.
func isStringListStyleType(lst css.ListStyleType) bool {
	if lst == "" {
		return false
	}
	if isPredefinedSymbolListStyleType(lst) {
		return false
	}
	switch lst {
	case css.ListStyleTypeDecimal, css.ListStyleTypeNone,
		css.ListStyleTypeDecimalLeadingZero,
		css.ListStyleTypeLowerAlpha, css.ListStyleTypeUpperAlpha,
		css.ListStyleTypeLowerLatin, css.ListStyleTypeUpperLatin,
		css.ListStyleTypeLowerRoman, css.ListStyleTypeUpperRoman,
		css.ListStyleTypeLowerGreek:
		return false
	}
	return true
}

// GetListStyleCategory mirrors list_marker.cc:267-277
// ListMarker::GetListStyleCategory. The originating list-item style is passed
// (list-style-type is read off the list item, never off the ::marker).
//
//	no list-style                  -> CategoryNone
//	<string> value                 -> CategoryStaticString
//	predefined symbol marker       -> CategorySymbol
//	otherwise (algorithmic style)  -> CategoryLanguage
func GetListStyleCategory(itemStyle *css.Style) ListStyleCategory {
	if itemStyle == nil {
		return CategoryNone
	}
	lst := itemStyle.GetListStyleType()
	if lst == css.ListStyleTypeNone {
		return CategoryNone
	}
	if isStringListStyleType(lst) {
		return CategoryStaticString
	}
	if isPredefinedSymbolListStyleType(lst) {
		return CategorySymbol
	}
	return CategoryLanguage
}

// markerText mirrors the private list_marker.cc:164-203 ListMarker::MarkerText.
// It dispatches on the list-style category of the originating item and, for
// the Symbol / Language arms, calls through MarkerTextSource. withPrefixSuffix
// selects kWithPrefixSuffix vs kWithoutPrefixSuffix.
//
// The image-marker case (style.GeneratesMarkerImage()) is handled by the
// caller before reaching here — for an image marker Blink returns kNotText
// (and, with prefix/suffix, appends a single space). Image markers in louis14
// are built as a separate marker-image box (Phase 7), so markerText is only
// called for text markers.
func (ListMarker) markerText(item *LayoutInputNode, src MarkerTextSource, withPrefixSuffix bool) (string, MarkerTextType) {
	if item == nil {
		return "", MarkerNotText
	}
	itemStyle := item.Style()
	switch GetListStyleCategory(itemStyle) {
	case CategoryNone:
		return "", MarkerNotText
	case CategoryStaticString:
		// Blink: text->Append(style.ListStyleStringValue()) — no suffix.
		return string(itemStyle.GetListStyleType()), MarkerStatic
	case CategorySymbol:
		// Blink generates the representation of ordinal 0 through the counter
		// style for a predefined symbol marker.
		if src == nil {
			return "", MarkerSymbolValue
		}
		return src.CounterStyleText(itemStyle, 0, withPrefixSuffix), MarkerSymbolValue
	case CategoryLanguage:
		if src == nil {
			return "", MarkerOrdinalValue
		}
		value := src.OrdinalValue(item)
		return src.CounterStyleText(itemStyle, value, withPrefixSuffix), MarkerOrdinalValue
	}
	return "", MarkerNotText
}

// MarkerTextWithSuffix mirrors list_marker.cc:205-210
// ListMarker::MarkerTextWithSuffix — the marker text including counter-style
// prefix/suffix (the trailing ". " of "1." etc.).
func (m ListMarker) MarkerTextWithSuffix(item *LayoutInputNode, src MarkerTextSource) (string, MarkerTextType) {
	return m.markerText(item, src, true)
}

// MarkerTextWithoutSuffix mirrors list_marker.cc:212-217
// ListMarker::MarkerTextWithoutSuffix — the bare marker text, no prefix/suffix.
func (m ListMarker) MarkerTextWithoutSuffix(item *LayoutInputNode, src MarkerTextSource) (string, MarkerTextType) {
	return m.markerText(item, src, false)
}

// WidthOfSymbol mirrors list_marker.cc:268-284 ListMarker::WidthOfSymbol.
//
// Blink reads font_data->GetFontMetrics().Ascent() off the marker's computed
// style; louis14's css.Style cannot resolve font metrics in the layout
// package, so the integer font ascent (in px) is passed in by the caller
// (the marker box layout, which has the font system). disclosure markers do
// not use the ascent; other symbols use (ascent*2/3 + 1)/2 + 2.
func WidthOfSymbol(style *css.Style, listStyleType css.ListStyleType, ascent int) layoutunit.LayoutUnit {
	if style == nil || style.GetFontSize() == 0 {
		return layoutunit.LayoutUnit{}
	}
	if listStyleType == css.ListStyleTypeDisclosureOpen ||
		listStyleType == css.ListStyleTypeDisclosureClosed {
		return disclosureSymbolSize(style)
	}
	// Integer arithmetic, matching Blink exactly: (ascent*2/3 + 1)/2 + 2.
	return layoutunit.New((ascent*2/3+1)/2 + 2)
}

// InlineMarginsForInside mirrors list_marker.cc:286-310
// ListMarker::InlineMarginsForInside. Returns (start, end) margins for an
// inside marker. cat is the category of the originating list item; markerStyle
// supplies the font size for the disclosure / symbol em-relative margins.
//
//	image marker        -> {0, 7px}
//	disclosure marker   -> {0, 0.4em (specified font size)}
//	other symbol        -> {-1, 1em (computed font size)}
//	otherwise           -> {0, 0}
func InlineMarginsForInside(markerStyle, itemStyle *css.Style, cat ListStyleCategory) (start, end layoutunit.LayoutUnit) {
	if itemStyle != nil && generatesMarkerImage(itemStyle) {
		return layoutunit.LayoutUnit{}, layoutunit.New(markerPaddingPx)
	}
	if cat == CategorySymbol {
		lst := css.ListStyleTypeDisc
		if itemStyle != nil {
			lst = itemStyle.GetListStyleType()
		}
		fontSize := 0.0
		if markerStyle != nil {
			fontSize = markerStyle.GetFontSize()
		}
		if lst == css.ListStyleTypeDisclosureOpen || lst == css.ListStyleTypeDisclosureClosed {
			return layoutunit.LayoutUnit{},
				layoutunit.FromFloat64Round(closureMarkerMarginEm * fontSize)
		}
		// Blink: {LayoutUnit(-1), LayoutUnit(kCUAMarkerMarginEm * ComputedSize)}.
		return layoutunit.New(-1),
			layoutunit.FromFloat64Round(uaMarkerMarginEm * fontSize)
	}
	return layoutunit.LayoutUnit{}, layoutunit.LayoutUnit{}
}

// InlineMarginsForOutside mirrors list_marker.cc:312-346
// ListMarker::InlineMarginsForOutside. Returns (start, end) margins for an
// outside marker; markerInlineSize is the laid-out inline size of the marker
// box. Blink's invariant: -start - end == markerInlineSize.
//
// Blink reads font_metrics.Ascent() off the marker style for the non-
// disclosure symbol case; that integer ascent (px) is passed in as ascent.
func InlineMarginsForOutside(markerStyle, itemStyle *css.Style, cat ListStyleCategory, markerInlineSize layoutunit.LayoutUnit, ascent int) (start, end layoutunit.LayoutUnit) {
	// Blink checks ContentBehavesAsNormal() FIRST (list_marker.cc:399-402 @
	// 4883d11f): an author ::marker content value hangs by the marker's own
	// inline-size regardless of the item's list-style category — including
	// list-style-type:none and the symbol types.
	if markerStyle != nil {
		if cv, ok := markerStyle.GetContentValues(); ok && len(cv) > 0 {
			return markerInlineSize.MulInt(-1), layoutunit.LayoutUnit{}
		}
	}
	if itemStyle != nil && generatesMarkerImage(itemStyle) {
		start = markerInlineSize.MulInt(-1).Sub(layoutunit.New(markerPaddingPx))
		end = layoutunit.New(markerPaddingPx)
		return start, end
	}
	switch cat {
	case CategoryNone:
		// start == end == 0.
		return layoutunit.LayoutUnit{}, layoutunit.LayoutUnit{}
	case CategorySymbol:
		lst := css.ListStyleTypeDisc
		if itemStyle != nil {
			lst = itemStyle.GetListStyleType()
		}
		var offset layoutunit.LayoutUnit
		if lst == css.ListStyleTypeDisclosureOpen || lst == css.ListStyleTypeDisclosureClosed {
			offset = disclosureSymbolSize(markerStyle)
		} else {
			offset = layoutunit.New(ascent * 2 / 3)
		}
		// margin_start = -offset - 7 - 1
		start = offset.MulInt(-1).Sub(layoutunit.New(markerPaddingPx)).Sub(layoutunit.New(1))
		// margin_end = offset + 7 + 1 - marker_inline_size
		end = offset.Add(layoutunit.New(markerPaddingPx)).Add(layoutunit.New(1)).Sub(markerInlineSize)
		return start, end
	default:
		// kLanguage / kStaticString: margin_start = -marker_inline_size.
		return markerInlineSize.MulInt(-1), layoutunit.LayoutUnit{}
	}
}

// RelativeSymbolMarkerRect mirrors list_marker.cc:348-379
// ListMarker::RelativeSymbolMarkerRect — the symbol's drawing rect relative to
// the marker box, in LOGICAL coordinates. The caller converts to physical via
// the writing-mode converter at the paint boundary (Phase 5). ascent is the
// integer font ascent in px; width is the marker box inline size.
func RelativeSymbolMarkerRect(style *css.Style, listStyleType css.ListStyleType, ascent int, width layoutunit.LayoutUnit) geometry.LogicalRect {
	if listStyleType == css.ListStyleTypeDisclosureOpen ||
		listStyleType == css.ListStyleTypeDisclosureClosed {
		markerSize := disclosureSymbolSize(style)
		return geometry.LogicalRect{
			Offset: geometry.LogicalOffset{
				InlineOffset: layoutunit.LayoutUnit{},
				BlockOffset:  layoutunit.New(ascent).Sub(markerSize),
			},
			Size: geometry.LogicalSize{
				InlineSize: markerSize,
				BlockSize:  markerSize,
			},
		}
	}
	// Bullet: width (ascent*2/3 + 1)/2, block offset 3*(ascent - ascent*2/3)/2.
	bulletWidth := layoutunit.New((ascent*2/3 + 1) / 2)
	return geometry.LogicalRect{
		Offset: geometry.LogicalOffset{
			InlineOffset: layoutunit.New(1),
			BlockOffset:  layoutunit.New(3 * (ascent - ascent*2/3) / 2),
		},
		Size: geometry.LogicalSize{
			InlineSize: bulletWidth,
			BlockSize:  bulletWidth,
		},
	}
}

// generatesMarkerImage reports whether the list item's style generates an
// image marker (a non-"none" list-style-image). Mirrors Blink's
// ComputedStyle::GeneratesMarkerImage().
func generatesMarkerImage(itemStyle *css.Style) bool {
	if itemStyle == nil {
		return false
	}
	val, ok := itemStyle.Get("list-style-image")
	if !ok {
		return false
	}
	val = strings.TrimSpace(val)
	return val != "" && val != "none"
}
