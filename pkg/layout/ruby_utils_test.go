package layout

import "testing"

// mkItem is a tiny helper for building InlineItem slices in test
// fixtures. Tests don't care about offsets/styles; only Type matters
// for the parser.
func mkItem(t InlineItemType) *InlineItem {
	return &InlineItem{Type: t}
}

func TestParseRubyInInlineItems_BaseOnly(t *testing.T) {
	// Item stream for `<ruby>X</ruby>` (no <rt>):
	//   [Open, BasePlaceholder, Text("X"), Close]
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),    // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1
		mkItem(InlineItemText),                // 2
		mkItem(InlineItemCloseRubyColumn),     // 3
	}
	got := ParseRubyInInlineItems(items, 0)
	want := RubyItemIndexes{
		ColumnStart: 0, BaseEnd: 3, AnnotationStart: 3, ColumnEnd: 3,
	}
	if got != want {
		t.Errorf("BaseOnly: got %+v, want %+v", got, want)
	}
	if got.HasAnnotation() {
		t.Error("BaseOnly: HasAnnotation() should be false")
	}
}

func TestParseRubyInInlineItems_BaseAndAnnotation(t *testing.T) {
	// Item stream for `<ruby>X<rt>x</rt></ruby>`:
	//   [Open, BasePH, Text("X"), AnnoPH, RtOpen, Text("x"), RtClose, Close]
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1 (base)
		mkItem(InlineItemText),                // 2
		mkItem(InlineItemRubyLinePlaceholder), // 3 (annotation)
		mkItem(InlineItemOpenTag),             // 4 <rt>
		mkItem(InlineItemText),                // 5
		mkItem(InlineItemCloseTag),            // 6 </rt>
		mkItem(InlineItemCloseRubyColumn),     // 7
	}
	got := ParseRubyInInlineItems(items, 0)
	want := RubyItemIndexes{
		ColumnStart: 0, BaseEnd: 3, AnnotationStart: 3, ColumnEnd: 7,
	}
	if got != want {
		t.Errorf("BaseAndAnnotation: got %+v, want %+v", got, want)
	}
	if !got.HasAnnotation() {
		t.Error("BaseAndAnnotation: HasAnnotation() should be true")
	}
}

func TestParseRubyInInlineItems_NestedColumn(t *testing.T) {
	// Item stream with a nested column (Phase 2 doesn't emit these
	// but the parser must skip them correctly for Phase 3+ to layer on
	// without retouching ruby_utils.go).
	//
	// Outer: [Open0, BasePH, OpenInner, BasePH, Text, CloseInner,
	//         AnnoPH, OpenTag, Text, CloseTag, Close0]
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0 outer open
		mkItem(InlineItemRubyLinePlaceholder), // 1 outer base
		mkItem(InlineItemOpenRubyColumn),      // 2 inner open
		mkItem(InlineItemRubyLinePlaceholder), // 3 inner base
		mkItem(InlineItemText),                // 4
		mkItem(InlineItemCloseRubyColumn),     // 5 inner close
		mkItem(InlineItemRubyLinePlaceholder), // 6 outer annotation
		mkItem(InlineItemOpenTag),             // 7
		mkItem(InlineItemText),                // 8
		mkItem(InlineItemCloseTag),            // 9
		mkItem(InlineItemCloseRubyColumn),     // 10 outer close
	}
	got := ParseRubyInInlineItems(items, 0)
	want := RubyItemIndexes{
		ColumnStart: 0, BaseEnd: 6, AnnotationStart: 6, ColumnEnd: 10,
	}
	if got != want {
		t.Errorf("NestedColumn outer: got %+v, want %+v", got, want)
	}

	// Inner column parses standalone.
	gotInner := ParseRubyInInlineItems(items, 2)
	wantInner := RubyItemIndexes{
		ColumnStart: 2, BaseEnd: 5, AnnotationStart: 5, ColumnEnd: 5,
	}
	if gotInner != wantInner {
		t.Errorf("NestedColumn inner: got %+v, want %+v", gotInner, wantInner)
	}
}

func TestParseRubyInInlineItems_EmptyColumn(t *testing.T) {
	// `<ruby></ruby>` (no children at all) — would be stripped by
	// closeOrStripRubyColumn before reaching the parser, but if a
	// caller does invoke us on it, return a sane range.
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1
		mkItem(InlineItemCloseRubyColumn),     // 2
	}
	got := ParseRubyInInlineItems(items, 0)
	want := RubyItemIndexes{
		ColumnStart: 0, BaseEnd: 2, AnnotationStart: 2, ColumnEnd: 2,
	}
	if got != want {
		t.Errorf("EmptyColumn: got %+v, want %+v", got, want)
	}
}

func TestParseRubyInInlineItems_NonOpenStart(t *testing.T) {
	// Defensive: if start doesn't point at an OpenRubyColumn, return
	// a zero range so the caller can skip without further access.
	items := []*InlineItem{mkItem(InlineItemText)}
	got := ParseRubyInInlineItems(items, 0)
	want := RubyItemIndexes{
		ColumnStart: 0, BaseEnd: 0, AnnotationStart: 0, ColumnEnd: 0,
	}
	if got != want {
		t.Errorf("NonOpenStart: got %+v, want %+v", got, want)
	}
}

func TestParseRubyInInlineItems_UnclosedColumn(t *testing.T) {
	// Malformed (no Close item). ColumnEnd should pin to len(items)
	// so callers can advance past without an infinite loop.
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1
		mkItem(InlineItemText),                // 2
	}
	got := ParseRubyInInlineItems(items, 0)
	if got.ColumnEnd != len(items) {
		t.Errorf("UnclosedColumn: ColumnEnd=%d, want %d (len)", got.ColumnEnd, len(items))
	}
}
