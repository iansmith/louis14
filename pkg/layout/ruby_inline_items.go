package layout

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// rubyCollectState tracks the inline-items-builder state for a ruby
// subtree being walked by collectInlinesRecursive. It is nil for any
// recursion outside a `<ruby>` element. When non-nil, the recursion
// is inside a ruby and ruby column open/close items are being emitted
// at the appropriate boundaries.
//
// Mirrors Blink's per-element state in
// `core/layout/inline/inline_items_builder.cc` (`EnterInline` /
// `ExitInline` body, `ruby_text_nesting_level_` field).
// Vetted against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type rubyCollectState struct {
	// rubyStyle is the style of the enclosing `<ruby>` element. Used
	// to pick the isolate-control direction (LRI vs RLI vs FSI) on
	// each new column open.
	rubyStyle *css.Style
	// rubyNode is the DOM node of the enclosing `<ruby>` element.
	rubyNode *html.Node
	// currentColumnCheckpoint is the rollback point for the column
	// currently being collected — used to strip almost-empty trailing
	// columns at `</ruby>` (mirrors
	// `inline_items_builder.cc:1617-1628`).
	currentColumnCheckpoint rubyColumnCheckpoint
	// textNestingLevel is the depth of `<rt>` descendants currently
	// being walked. Bumped on `<rt>` enter, decremented on `<rt>`
	// exit. When > 0, forced breaks (`<br>` items and `\n` control
	// items emitted from preserved text) are rewritten to spaces
	// (mirrors `kDisableForcedBreakInRubyColumn`,
	// `inline_items_builder.cc:74,801,1068`).
	textNestingLevel int
}

// rubyColumnCheckpoint records the snapshot of (data.Items length,
// text buffer length) at the moment a column is opened. Used to
// rollback the column if it turns out to be almost-empty at `</ruby>`
// time.
//
// isPrimaryBase distinguishes the primary base column (opened at `<ruby>`,
// closed at `</ruby>`) from reopened trailing columns (opened after each
// `</rt>` when more content follows). Only reopened trailing columns should
// be stripped when almost-empty; the primary base column is preserved to
// maintain inline-size for empty rubies (CSS Inline 3 invisible line boxes).
// Mirrors Blink's distinction at `inline_items_builder.cc:1617-1628`.
type rubyColumnCheckpoint struct {
	itemsLen      int
	textLen       int
	isPrimaryBase bool
}

// openRubyColumn emits the isolate bidi control + InlineItemOpenRubyColumn
// + an InlineItemRubyLinePlaceholder marking the start of the base
// sub-line, and returns the checkpoint for a future strip-or-close.
//
// Mirrors Blink's `kOpenRubyColumn` emission in
// `core/layout/inline/inline_items_builder.cc:1550-1595` — the column
// carries an isolate (LRI for LTR ruby, RLI for RTL, FSI if direction
// is unknown), and the immediately-following placeholder anchors the
// base sub-LineInfo construction in line_breaker handleRuby.
func openRubyColumn(
	data *InlineItemsData,
	text *strings.Builder,
	rubyStyle *css.Style,
	rubyNode *html.Node,
	isPrimaryBase bool,
) rubyColumnCheckpoint {
	cp := rubyColumnCheckpoint{itemsLen: len(data.Items), textLen: text.Len(), isPrimaryBase: isPrimaryBase}
	// Per-column bidi isolate: LRI / RLI / FSI per the ruby's resolved
	// direction (mirrors `inline_items_builder.cc:1556-1559,1583-1586`).
	switch {
	case rubyStyle != nil && rubyStyle.GetDirection() == css.DirectionRTL:
		text.WriteRune('⁧') // RLI
	case rubyStyle != nil && rubyStyle.GetDirection() == css.DirectionLTR:
		text.WriteRune('⁦') // LRI
	default:
		text.WriteRune('⁨') // FSI
	}
	offset := text.Len()
	data.Items = append(data.Items, &InlineItem{
		Type:        InlineItemOpenRubyColumn,
		StartOffset: offset,
		EndOffset:   offset,
		Node:        rubyNode,
		Style:       rubyStyle,
	})
	// Base sub-line anchor — exactly one per column.
	data.Items = append(data.Items, &InlineItem{
		Type:        InlineItemRubyLinePlaceholder,
		StartOffset: offset,
		EndOffset:   offset,
		Node:        rubyNode,
		Style:       rubyStyle,
	})
	return cp
}

// closeOrStripRubyColumn closes the currently-open ruby column, OR
// strips it entirely if it contains no real base/annotation content
// (the "almost-empty trailing column" case at `</ruby>` after a
// fresh-column reopen following the final `</rt>`, mirrors
// `inline_items_builder.cc:1617-1628`).
//
// Only reopened trailing columns (isPrimaryBase=false) are subject to
// stripping; the primary base column is preserved to maintain inline-size
// for empty rubies (CSS Inline 3 invisible line boxes). Mirrors Blink's
// distinction at `inline_items_builder.cc:1617-1628`.
//
// Returns true if the column was stripped (no Close item emitted).
//
// Note: strip rolls back the text buffer by allocating a copy of the
// full buffer-so-far. strings.Builder has no Truncate; an alloc-free
// truncate would require switching collectInlinesRecursive to
// bytes.Buffer or a raw []byte slice. The strip path only fires on
// the trailing reopened column after a final `</rt>`, which is rare
// in practice (most ruby trees end with the last `</rt>` outside a
// fresh-column reopen). Revisit if profiling on ruby-heavy pages
// surfaces it as hot.
func closeOrStripRubyColumn(
	data *InlineItemsData,
	text *strings.Builder,
	cp rubyColumnCheckpoint,
	rubyStyle *css.Style,
	rubyNode *html.Node,
) bool {
	// Strip almost-empty columns. For a ruby with `<rt>` content, only the
	// reopened column after the final `</rt>` should be stripped. For a ruby
	// without `<rt>` (base-only), an empty base is kept to preserve a minimal
	// inline extent (invisible line box, CSS Inline 3).
	// The isPrimaryBase flag distinguishes: primary (isPrimaryBase=true, opened
	// at `<ruby>`) vs reopened (isPrimaryBase=false, opened after `</rt>`).
	// Per Blink's distinction (inline_items_builder.cc:1617-1628), only reopened
	// trailing columns are stripped.
	if !cp.isPrimaryBase && isAlmostEmptyRubyColumn(data, cp.itemsLen) {
		data.Items = data.Items[:cp.itemsLen]
		buf := text.String()
		text.Reset()
		text.WriteString(buf[:cp.textLen])
		return true
	}
	offset := text.Len()
	data.Items = append(data.Items, &InlineItem{
		Type:        InlineItemCloseRubyColumn,
		StartOffset: offset,
		EndOffset:   offset,
		Node:        rubyNode,
		Style:       rubyStyle,
	})
	// PDI to pop the column's isolate (mirrors the matching pop at
	// `inline_items_builder.cc:1628,1682-1690`).
	text.WriteRune('⁩') // PDI
	return false
}

// isAlmostEmptyRubyColumn reports whether the items appended since
// the column opened consist only of placeholders / open/close tags
// (no real text or atomic content) — i.e. the column would contribute
// nothing to layout and is the "trailing reopened column" that Blink
// strips at `</ruby>`. The items in [openItemsLen, len(data.Items))
// were appended after the OpenRubyColumn (which lives at
// data.Items[openItemsLen]).
func isAlmostEmptyRubyColumn(data *InlineItemsData, openItemsLen int) bool {
	for i := openItemsLen + 1; i < len(data.Items); i++ {
		switch data.Items[i].Type {
		case InlineItemRubyLinePlaceholder,
			InlineItemOpenTag,
			InlineItemCloseTag:
			// transparent for the "almost-empty" check
		default:
			return false
		}
	}
	return true
}

// emitRubyAnnotationPlaceholder appends an InlineItemRubyLinePlaceholder
// marking the start of an annotation sub-line. Emitted when entering a
// `<rt>` inside a ruby column (mirrors `inline_items_builder.cc:1550-1595`,
// the `IsInlineRubyText()` branch).
func emitRubyAnnotationPlaceholder(
	data *InlineItemsData,
	text *strings.Builder,
	rtStyle *css.Style,
	rtNode *html.Node,
) {
	offset := text.Len()
	data.Items = append(data.Items, &InlineItem{
		Type:        InlineItemRubyLinePlaceholder,
		StartOffset: offset,
		EndOffset:   offset,
		Node:        rtNode,
		Style:       rtStyle,
	})
}

// rubyForcedBreakSuppressed reports whether forced-break items
// (`<br>`, preserved `\n`) being emitted now should be rewritten to a
// regular space. True when the current collection point is inside an
// `<rt>` (or any descendant), mirroring Blink's
// `kDisableForcedBreakInRubyColumn` gate at
// `inline_items_builder.cc:74,801,1068`.
func rubyForcedBreakSuppressed(state *rubyCollectState) bool {
	return state != nil && state.textNestingLevel > 0
}
