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
	// hasColumn is true once this state has opened a real ruby column
	// (the IsInlineRuby case calls openRubyColumn). It is false for the
	// suppress-only sentinel created for a standalone ruby-family box,
	// which tracks forced-break suppression but has no column. The
	// annotation-column machinery (`<rt>` placeholder / column close+reopen)
	// must run only when hasColumn is true — driving it from a sentinel
	// would close a column that was never opened (zero checkpoint), which
	// strips unrelated items.
	hasColumn bool
	// textNestingLevel is the forced-break suppression depth — Blink's
	// `ruby_text_nesting_level_`. Bumped on entering a `<ruby>` column
	// (`inline_items_builder.cc:1587-1588`) and on entering an `<rt>`
	// annotation (`:1551-1552`), decremented on the matching exit; a
	// standalone ruby-family base seeds it at 1. When > 0, forced breaks
	// (`<br>` items and `\n` control items from preserved text) are
	// suppressed (mirrors `kDisableForcedBreakInRubyColumn`,
	// `inline_items_builder.cc:74,801,1068`): `<br>` is dropped, `\n`
	// becomes a space.
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
	// Primary base columns (opened at `<ruby>`) are never stripped; only
	// reopened trailing columns (opened after `</rt>`) are subject to the
	// almost-empty check. See doc comment above.
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

// rubyForcedBreakSuppressed reports whether forced breaks (`<br>`,
// preserved `\n`) being emitted now fall inside a ruby column and must be
// suppressed. True when textNestingLevel > 0 — which holds inside the ruby
// base (entered via `<ruby>`, mirrors `inline_items_builder.cc:1587-1588`),
// inside an `<rt>` annotation (`:1551-1552`), and inside a standalone
// ruby-family box that forms a base (louis14's box-fixup analog, see
// collectInlinesRecursive). Mirrors Blink's `kDisableForcedBreakInRubyColumn`
// gate at `inline_items_builder.cc:74,801,1068` @ 4883d11fef.
func rubyForcedBreakSuppressed(state *rubyCollectState) bool {
	return state != nil && state.textNestingLevel > 0
}

// isRubyFamilyTag reports whether tag is a ruby-internal element that
// suppresses forced breaks within its base content. `<rp>` is excluded —
// it is `display: none` per the UA stylesheet (cascade.go) and never
// contributes layout.
func isRubyFamilyTag(tag string) bool {
	switch tag {
	case "ruby", "rb", "rbc", "rt", "rtc":
		return true
	}
	return false
}

// rubyStandaloneFormsBase reports whether a standalone ruby-family element
// (one not wrapped in an enclosing `<ruby>`) holds enough content to form a
// real ruby base. Forced breaks are suppressed only inside such a base; a
// ruby box whose content is whitespace-only does NOT form a base, so a
// forced break inside it is preserved. Mirrors the CSS Ruby 1 box-fixup
// rule exercised by WPT ruby-line-break-suppression-004 ("whitespaces
// wrapped but not contained in ruby boxes").
//
// `<br>` descendants do not count as base content (a box whose only content
// is a forced break is not a base); replaced elements and non-whitespace
// text do.
func rubyStandaloneFormsBase(node *html.Node) bool {
	for _, c := range node.Children {
		switch c.Type {
		case html.TextNode:
			// c.Text is the canonical content field (collectTextNode keys off
			// it); whitespace-collapsing never removes content runes, so a
			// non-whitespace Text means real base content.
			if strings.TrimSpace(c.Text) != "" {
				return true
			}
		case html.ElementNode:
			if c.TagName == "br" {
				continue
			}
			if IsReplacedElement(c) {
				return true
			}
			if rubyStandaloneFormsBase(c) {
				return true
			}
		}
	}
	return false
}
