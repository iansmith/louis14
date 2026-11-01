package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutBoxesByID lays out src and returns its boxes keyed by id attribute.
func layoutBoxesByID(t *testing.T, src string) map[string]*Box {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	byID := map[string]*Box{}
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil {
			if id, ok := b.Node.GetAttribute("id"); ok && byID[id] == nil {
				byID[id] = b
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	return byID
}

// TestRubyInlinifiesRecursivelyThroughInlines pins Blink's recursive
// inlinification: an inline box inlinified by a `display: ruby` (or
// `ruby-text`) layout parent inlinifies its own in-flow children in turn
// (ComputedStyle::InlinifiesChildren, style/computed_style.h:1805-1817, and
// StyleAdjuster::AdjustStyleForDisplay, css/resolver/style_adjuster.cc:789-806
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), but an inline-block does not.
// A block inside display:ruby is therefore an atomic inline-block, never a
// block-in-inline (WPT css-ruby/ruby-inlinize-recursive-simple).
func TestRubyInlinifiesRecursivelyThroughInlines(t *testing.T) {
	boxes := layoutBoxesByID(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div><ruby><b><span id=inb style="display:block">a</span></b></ruby></div>`+
		`<div><span style="display:ruby-text"><b><span id=inrt style="display:block">a</span></b></span></div>`+
		`<div><ruby><span style="display:inline-block"><span id=inib style="display:block">a</span></span></ruby></div>`)
	for _, tc := range []struct {
		id   string
		want css.DisplayType
	}{
		{"inb", css.DisplayInlineBlock},
		{"inrt", css.DisplayInlineBlock},
		{"inib", css.DisplayBlock},
	} {
		b := boxes[tc.id]
		if b == nil {
			t.Errorf("#%s: no box", tc.id)
			continue
		}
		if got := b.Style.GetDisplay(); got != tc.want {
			t.Errorf("#%s display = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// TestRubyBaseAtomicInlineIsPlaced pins that an atomic inline inside a ruby
// base is placed in the line box like any other atomic inline: Blink lays out
// a ruby column's base items through HandleItemResults → PlaceAtomicInline
// (LogicalLineBuilder::PlaceRubyColumn, inline/logical_line_builder.cc:550-635
// and :466-525 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestRubyBaseAtomicInlineIsPlaced(t *testing.T) {
	boxes := layoutBoxesByID(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="font: 20px/1 Ahem"><ruby>x<span id=ib style="display:inline-block;margin-left:5px">y</span></ruby></div>`)
	ib := boxes["ib"]
	if ib == nil {
		t.Fatal("inline-block inside ruby base produced no box")
	}
	// 'x' is 20px wide in Ahem, then a 5px inline-start margin.
	if ib.X != 25 || ib.Y != 0 || ib.Width != 20 || ib.Height != 20 {
		t.Errorf("inline-block at (%v,%v %vx%v), want (25,0 20x20)", ib.X, ib.Y, ib.Width, ib.Height)
	}
}
