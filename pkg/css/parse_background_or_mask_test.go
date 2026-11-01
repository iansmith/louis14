package css

import (
	"math"
	"testing"
)

// Tests for the port of Blink's ParseBackgroundOrMask / ConsumeBackgroundSize /
// ConsumePosition (core/css/properties/css_parsing_utils.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each case uses the exact
// declaration from a css-backgrounds WPT test (LOU-532).

// bgLayers returns the FillLayer list for an inline style declaration block.
func bgLayers(t *testing.T, decl string) (*Style, []*FillLayer) {
	t.Helper()
	s := ParseInlineStyle(decl, nil)
	var out []*FillLayer
	for cur := s.GetBackgroundLayers(); cur != nil; cur = cur.Next {
		out = append(out, cur)
	}
	return s, out
}

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// background-334.html: `<position> / <bg-size>` in the shorthand.
func TestParseBackgroundOrMask_SlashSize_Background334(t *testing.T) {
	s, layers := bgLayers(t, "background: #CCC url(support/60x60-green.png) top left / 100% auto no-repeat")
	if len(layers) != 1 {
		t.Fatalf("got %d layers, want 1", len(layers))
	}
	l := layers[0]
	if !l.SizeSet || l.Size != (BackgroundSize{Width: -100}) {
		t.Errorf("size = %+v (set=%v), want 100%% auto", l.Size, l.SizeSet)
	}
	if l.Repeat != (BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatNoRepeat}) {
		t.Errorf("repeat = %+v, want no-repeat", l.Repeat)
	}
	if x, y := l.Position.ResolveX(150, 60), l.Position.ResolveY(150, 60); x != 0 || y != 0 {
		t.Errorf("position = (%v,%v), want (0,0)", x, y)
	}
	if c, _ := s.Get("background-color"); c != "#CCC" {
		t.Errorf("background-color = %q, want #CCC", c)
	}
}

// background-none-none-and-color.html: the shorthand resets every longhand it
// does not mention to its initial value (ParseBackgroundOrMask pushes
// CSSInitialValue for each unparsed longhand).
func TestParseBackgroundOrMask_ResetsColor_NoneNone(t *testing.T) {
	s := ParseInlineStyle("background-color: blue; background: none, none", nil)
	if c, _ := s.Get("background-color"); c != "transparent" {
		t.Errorf("background-color after `background: none, none` = %q, want transparent", c)
	}
	s = ParseInlineStyle("background: none, none; background-color: blue", nil)
	if c, _ := s.Get("background-color"); c != "blue" {
		t.Errorf("background-color = %q, want blue", c)
	}
}

func TestParseBackgroundOrMask_ResetsUnmentionedLonghands(t *testing.T) {
	s, layers := bgLayers(t, "background-size: 10px; background-repeat: no-repeat; background-attachment: local; background-origin: content-box; background-position: 5px 5px; background: url(a-lou532.png)")
	if len(layers) != 1 {
		t.Fatalf("got %d layers, want 1", len(layers))
	}
	l := layers[0]
	if l.Size != (BackgroundSize{}) || l.Repeat != (BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}) ||
		l.Attachment != BackgroundAttachmentScroll || l.Origin != BackgroundOriginPaddingBox || l.Clip != BackgroundClipBorderBox ||
		l.Position.ResolveX(100, 10) != 0 || l.Position.ResolveY(100, 10) != 0 {
		t.Errorf("unmentioned longhands not reset to initial: %+v", *l)
	}
	if c, _ := s.Get("background-color"); c != "transparent" {
		t.Errorf("background-color = %q, want transparent", c)
	}
}

// background-size-with-negative-value.html: ConsumeBackgroundSize uses
// ValueRange::kNonNegative, so a negative size invalidates the declaration.
func TestConsumeBackgroundSize_NegativeInvalid(t *testing.T) {
	_, layers := bgLayers(t, "background-image: url(support/60x60-green.png); background-repeat: no-repeat; background-size: -1px -1px")
	if len(layers) != 1 {
		t.Fatalf("got %d layers, want 1", len(layers))
	}
	if layers[0].Size != (BackgroundSize{}) {
		t.Errorf("size = %+v, want auto (declaration invalid)", layers[0].Size)
	}
	ss, err := ParseStylesheet("div { background-size: 10px; background-size: -1px -1px; }", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ss.Rules[0].Declarations["background-size"]; got != "10px" {
		t.Errorf("background-size = %q, want the earlier valid 10px", got)
	}
}

func TestParseBackgroundOrMask_InvalidDeclarationsKeepEarlier(t *testing.T) {
	for _, bad := range []string{
		"url(b.png) 0 0 / -1px auto",    // negative <bg-size>
		"url(b.png) / 10px",             // <bg-size> without <bg-position>
		"red url(b.png), url(c.png)",    // color in a non-final layer
		"url(b.png) bogus",              // unknown token
		"url(b.png) url(c.png)",         // two images in one layer
		"url(b.png) no-repeat repeat-x", // repeat-x after a repeat-style
	} {
		ss, err := ParseStylesheet("div { background: url(a.png) green; background: "+bad+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		d := ss.Rules[0].Declarations
		if d["background-image"] != "url(a.png)" || d["background-color"] != "green" {
			t.Errorf("`background: %s` should be invalid; got image=%q color=%q", bad, d["background-image"], d["background-color"])
		}
	}
}

// background-clip defaults to the layer's background-origin when only one
// <visual-box> is given.
func TestParseBackgroundOrMask_OriginAndClip(t *testing.T) {
	_, layers := bgLayers(t, "background: url(o1-lou532.png) content-box")
	if l := layers[0]; l.Origin != BackgroundOriginContentBox || l.Clip != BackgroundClipContentBox {
		t.Errorf("one box: origin=%v clip=%v, want content-box both", l.Origin, l.Clip)
	}
	_, layers = bgLayers(t, "background: url(o2-lou532.png) padding-box content-box")
	if l := layers[0]; l.Origin != BackgroundOriginPaddingBox || l.Clip != BackgroundClipContentBox {
		t.Errorf("two boxes: origin=%v clip=%v, want padding-box / content-box", l.Origin, l.Clip)
	}
}

// local-attachment-*-scroll.html: per-layer attachment, slash sizes, and
// positions in a three-layer shorthand.
func TestParseBackgroundOrMask_LocalAttachmentMultiLayer(t *testing.T) {
	_, layers := bgLayers(t, `background:
    local linear-gradient(to right, yellow 50%, blue 50%) 0 0 / 100% 100% no-repeat,
    radial-gradient(farthest-side at 0 50%, rgba(0,0,0,0.2), transparent) 0 0 / 20px 100% no-repeat,
    radial-gradient(farthest-side at 100% 50%, rgba(0,0,0,0.2), transparent) 100% 0 / 20px 100% no-repeat`)
	if len(layers) != 3 {
		t.Fatalf("got %d layers, want 3", len(layers))
	}
	want := []struct {
		attach BackgroundAttachmentType
		size   BackgroundSize
		x, y   float64 // resolved in a 100x100 area for the layer's tile
	}{
		{BackgroundAttachmentLocal, BackgroundSize{Width: -100, Height: -100}, 0, 0},
		{BackgroundAttachmentScroll, BackgroundSize{Width: 20, Height: -100}, 0, 0},
		{BackgroundAttachmentScroll, BackgroundSize{Width: 20, Height: -100}, 80, 0},
	}
	for i, w := range want {
		l := layers[i]
		if l.Gradient == "" {
			t.Errorf("layer %d: no gradient", i)
		}
		if l.Attachment != w.attach {
			t.Errorf("layer %d: attachment = %v, want %v", i, l.Attachment, w.attach)
		}
		if l.Size != w.size {
			t.Errorf("layer %d: size = %+v, want %+v", i, l.Size, w.size)
		}
		if l.Repeat.X != BackgroundRepeatNoRepeat || l.Repeat.Y != BackgroundRepeatNoRepeat {
			t.Errorf("layer %d: repeat = %+v, want no-repeat", i, l.Repeat)
		}
		if x, y := l.Position.ResolveX(100, 20), l.Position.ResolveY(100, 100); !approxEq(x, w.x) || !approxEq(y, w.y) {
			t.Errorf("layer %d: position = (%v,%v), want (%v,%v)", i, x, y, w.x, w.y)
		}
	}
}

// local-attachment-content-box-scroll.html declares the attachment as a
// longhand next to a color-only shorthand.
func TestParseBackgroundOrMask_ColorOnlyThenAttachmentLonghand(t *testing.T) {
	s := ParseInlineStyle("background: blue; background-attachment: local; background-clip: content-box", nil)
	if c, _ := s.Get("background-color"); c != "blue" {
		t.Errorf("background-color = %q, want blue", c)
	}
	if a := s.GetBackgroundAttachment(); a != BackgroundAttachmentLocal {
		t.Errorf("attachment = %v, want local", a)
	}
}

// background-position-three-four-values.html: ConsumePosition's 3/4-value
// edge-offset forms (PositionFromThreeOrFourValues) and the two-value y/x
// keyword order (PositionFromTwoValues). Container 200x200, image 50x50.
func TestConsumePosition_ThreeFourValuesAndKeywordOrder(t *testing.T) {
	cases := []struct {
		pos  string
		x, y float64
	}{
		{"left 50px center", 50, 75},
		{"right 25px top 75%", 125, 112.5},
		{"left 25px bottom 75%", 25, 37.5},
		{"right 25px bottom 25%", 125, 112.5},
		{"top right", 150, 0},
		{"bottom left", 0, 150},
		{"center right", 150, 75},
		{"bottom 10px right", 150, 140},
	}
	for _, c := range cases {
		for _, decl := range []string{
			"background-image: url(p-lou532.png); background-position: " + c.pos,
			"background: url(p-lou532.png) " + c.pos + " no-repeat",
		} {
			_, layers := bgLayers(t, decl)
			if len(layers) != 1 {
				t.Errorf("%q: got %d layers", decl, len(layers))
				continue
			}
			p := layers[0].Position
			if x, y := p.ResolveX(200, 50), p.ResolveY(200, 50); !approxEq(x, c.x) || !approxEq(y, c.y) {
				t.Errorf("%q: position = (%v,%v), want (%v,%v)", decl, x, y, c.x, c.y)
			}
		}
	}
}

// order-of-images.htm: a background-* longhand list shorter than the
// background-image list is repeated to cover every layer (CSS Backgrounds 3
// §2.2 "excess/missing values"), as Blink's FillLayer::FillUnsetProperties
// does (core/style/fill_layer.cc:209 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each unset layer copies the
// value from the set prefix, cycling. Layers whose property no layer sets
// keep the initial value. LOU-560 (worklist C7).
func TestGetBackgroundLayers_CyclesShortLonghandLists(t *testing.T) {
	_, layers := bgLayers(t, "background-image: url(a.png), url(b.png), url(c.png); "+
		"background-repeat: no-repeat; "+
		"background-size: 10px 10px, 20px 20px; "+
		"background-clip: padding-box, content-box; "+
		"background-origin: content-box; "+
		"background-attachment: fixed")
	if len(layers) != 3 {
		t.Fatalf("got %d layers, want 3", len(layers))
	}
	noRepeat := BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatNoRepeat}
	wantSize := []float64{10, 20, 10}
	wantClip := []BackgroundClipType{BackgroundClipPaddingBox, BackgroundClipContentBox, BackgroundClipPaddingBox}
	for i, l := range layers {
		if l.Repeat != noRepeat {
			t.Errorf("layer %d repeat = %+v, want no-repeat", i, l.Repeat)
		}
		if l.Size.Width != wantSize[i] || l.Size.Height != wantSize[i] {
			t.Errorf("layer %d size = %+v, want %vpx", i, l.Size, wantSize[i])
		}
		if l.Clip != wantClip[i] {
			t.Errorf("layer %d clip = %v, want %v", i, l.Clip, wantClip[i])
		}
		if l.Origin != BackgroundOriginContentBox {
			t.Errorf("layer %d origin = %v, want content-box", i, l.Origin)
		}
		if l.Attachment != BackgroundAttachmentFixed {
			t.Errorf("layer %d attachment = %v, want fixed", i, l.Attachment)
		}
		// background-position unset on every layer: initial 0% 0%.
		if l.Position != (BackgroundPosition{}) {
			t.Errorf("layer %d position = %+v, want initial", i, l.Position)
		}
	}
}
