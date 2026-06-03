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
		mkItem(InlineItemOpenRubyColumn),      // 0
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

// === Phase 0 (LOU-156, item 3) — HasAnnotation invariant locks ===
//
// CodeRabbit on PR #14 suggested tightening HasAnnotation to
// `AnnotationStart+1 < ColumnEnd` so a "zero-content annotation"
// (placeholder immediately before the column close) would not count.
//
// Blink alignment check (SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
// ruby_utils.cc:151 (inside ParseRubyInInlineItems, lines 147-176)
// initialises RubyItemIndexes with `annotation_start = kNotFound`, then
// assigns it at line 169 only when an annotation kOpenTag is encountered
// (`item.Type() == InlineItem::kOpenTag && item.GetLayoutObject()->IsInlineRubyText()`
// at lines 164-165). "Has annotation" in Blink is `annotation_start !=
// kNotFound` — i.e. it tests whether the annotation EXISTS at all, NOT
// whether the annotation has CONTENT. Adopting CodeRabbit's tightening would silently
// diverge from Blink semantics: a future emitter shape with an
// adjacent-placeholder degenerate case would be classified differently
// in louis14 than in Blink.
//
// louis14 uses a slightly different sentinel encoding (AnnotationStart
// collapses to ColumnEnd when no annotation), but the semantic is the
// same as Blink: `AnnotationStart < ColumnEnd` is exactly "annotation
// exists" — equivalent to Blink's `annotation_start != kNotFound`.
//
// The tests below codify both invariants so a future PR cannot silently
// adopt the misaligned tightening.

func TestHasAnnotation_NoAnnotation_AnnotationStartCollapsesToColumnEnd(t *testing.T) {
	// Invariant: for any column without an annotation, AnnotationStart
	// MUST equal ColumnEnd. This is louis14's sentinel encoding —
	// equivalent to Blink's annotation_start == kNotFound.
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1
		mkItem(InlineItemText),                // 2
		mkItem(InlineItemCloseRubyColumn),     // 3
	}
	got := ParseRubyInInlineItems(items, 0)
	if got.AnnotationStart != got.ColumnEnd {
		t.Errorf("no-annotation column: AnnotationStart=%d should equal ColumnEnd=%d (sentinel encoding)",
			got.AnnotationStart, got.ColumnEnd)
	}
	if got.HasAnnotation() {
		t.Error("no-annotation column: HasAnnotation() must return false")
	}
}

func TestHasAnnotation_AdjacentPlaceholderBeforeClose_HasAnnotationTrue(t *testing.T) {
	// Degenerate but well-formed shape: [Open, BasePH, AnnoPH, Close].
	// The annotation placeholder is emitted (so the annotation EXISTS
	// per Blink semantics) but no items follow it before the column close.
	//
	// Blink-aligned answer: HasAnnotation() == true (annotation exists,
	// even if its content range is empty). CodeRabbit's suggested
	// `AnnotationStart+1 < ColumnEnd` would return false here — that
	// would diverge from Blink. This test pins the Blink-aligned answer
	// so any future regression in the direction of CodeRabbit's
	// suggestion fails loudly.
	items := []*InlineItem{
		mkItem(InlineItemOpenRubyColumn),      // 0
		mkItem(InlineItemRubyLinePlaceholder), // 1 base PH
		mkItem(InlineItemRubyLinePlaceholder), // 2 annotation PH (degenerate: adjacent to Close)
		mkItem(InlineItemCloseRubyColumn),     // 3
	}
	got := ParseRubyInInlineItems(items, 0)
	if got.AnnotationStart != 2 {
		t.Errorf("adjacent-placeholder: AnnotationStart=%d, want 2", got.AnnotationStart)
	}
	if got.ColumnEnd != 3 {
		t.Errorf("adjacent-placeholder: ColumnEnd=%d, want 3", got.ColumnEnd)
	}
	if !got.HasAnnotation() {
		t.Error("adjacent-placeholder: HasAnnotation() must return true — annotation EXISTS (placeholder emitted), even though its content range is empty. CodeRabbit's `AnnotationStart+1 < ColumnEnd` tightening would silently return false here, diverging from Blink's `annotation_start != kNotFound` semantics.")
	}
}
