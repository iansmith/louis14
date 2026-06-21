package layout

import "testing"

// TestResolveBidiLevels_RTLMultiParagraph is the Phase-0 RED test for LOU-313.
//
// In a dir=rtl block split by <br> into multiple bidi paragraphs, every
// paragraph must use the explicit RTL base direction. Per UAX#9 the text is
// split into paragraphs at class-B separators (P1), and when a higher-level
// protocol (CSS `direction`/`dir`) sets the base level, every paragraph uses
// that base — P2/P3 auto-detection is NOT re-run per paragraph.
//
// A leading neutral (the U+FFFC object-replacement char that represents an
// inline-block atomic inline) on a non-first line must therefore resolve to
// the RTL base level (1), exactly like the first line. The bug: because
// ResolveBidiLevels resolves the whole TextContent as one neutral-resolution
// context (no P1 split), line 2's leading neutral inherits L context from the
// previous line's trailing text across the <br>'s \n, resolving to level 2
// (LTR embedding inside RTL). That even-level run then double-reverses during
// L2 reordering and lands the inline-block on the wrong (left) side.
//
// Repro: <p dir="rtl"><b>I</b>foo<br><b>I</b>bar</p>  (b = inline-block)
func TestResolveBidiLevels_RTLMultiParagraph(t *testing.T) {
	const orc = "￼" // object-replacement char = atomic inline (inline-block)
	// TextContent the collect phase produces (inline_item.go:271,315):
	//   b#1 | foo | <br>=\n | b#2 | bar     (U+FFFC is 3 bytes, ASCII is 1)
	text := orc + "foo" + "\n" + orc + "bar"
	items := []*InlineItem{
		{Type: InlineItemAtomicInline, StartOffset: 0, EndOffset: 3},  // b#1
		{Type: InlineItemText, StartOffset: 3, EndOffset: 6},          // foo
		{Type: InlineItemControl, StartOffset: 6, EndOffset: 7},       // <br> \n
		{Type: InlineItemAtomicInline, StartOffset: 7, EndOffset: 10}, // b#2
		{Type: InlineItemText, StartOffset: 10, EndOffset: 13},        // bar
	}
	data := &InlineItemsData{TextContent: text, Items: items}

	ResolveBidiLevels(data, DirectionRTL)

	b1 := data.Items[0].BidiLevel // line-1 inline-block
	b2 := data.Items[3].BidiLevel // line-2 inline-block

	if b1 != 1 {
		t.Fatalf("line-1 inline-block BidiLevel = %d, want 1 (RTL base)", b1)
	}
	if b2 != 1 {
		t.Errorf("LOU-313: line-2 inline-block BidiLevel = %d, want 1 (RTL base); "+
			"level %d is LTR-embedding and reorders the line LTR-like", b2, b2)
	}
	if b1 != b2 {
		t.Errorf("LOU-313: structurally identical lines resolve their inline-blocks "+
			"to different bidi levels (line1=%d, line2=%d); they must match", b1, b2)
	}
}
