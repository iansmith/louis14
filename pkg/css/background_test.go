package css

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

func TestParseURLValue(t *testing.T) {
	tests := []struct {
		input   string
		wantURL string
		wantOK  bool
	}{
		{"url(image.png)", "image.png", true},
		{"url('image.png')", "image.png", true},
		{`url("image.png")`, "image.png", true},
		{"url( image.png )", "image.png", true},
		{"url(data:image/png;base64,iVBOR)", "data:image/png;base64,iVBOR", true},
		{"url()", "", false},
		{"none", "", false},
		{"", "", false},
		{"url(  'spaced.png'  )", "spaced.png", true},
	}

	for _, tt := range tests {
		url, ok := ParseURLValue(tt.input)
		if ok != tt.wantOK || url != tt.wantURL {
			t.Errorf("ParseURLValue(%q) = (%q, %v), want (%q, %v)", tt.input, url, ok, tt.wantURL, tt.wantOK)
		}
	}
}

func TestGetBackgroundImage(t *testing.T) {
	s := NewStyle()
	s.Set("background-image", "url(test.png)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("GetBackgroundImage() = (%q, %v), want (\"test.png\", true)", url, ok)
	}
}

func TestGetBackgroundImage_DataURI(t *testing.T) {
	s := NewStyle()
	s.Set("background-image", "url(data:image/png;base64,abc123)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "data:image/png;base64,abc123" {
		t.Errorf("GetBackgroundImage() = (%q, %v)", url, ok)
	}
}

func TestGetBackgroundImage_NotSet(t *testing.T) {
	s := NewStyle()
	_, ok := s.GetBackgroundImage()
	if ok {
		t.Error("expected false for unset background-image")
	}
}

func TestExpandBackgroundShorthand_URL(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(test.png)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("background shorthand url: got (%q, %v)", url, ok)
	}
}

func TestExpandBackgroundShorthand_URLAndColor(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "red url(bg.png) no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "bg.png" {
		t.Errorf("background-image: got (%q, %v)", url, ok)
	}

	if color, ok := s.Get("background-color"); !ok || color != "red" {
		t.Errorf("background-color: got (%q, %v)", color, ok)
	}

	if repeat, ok := s.Get("background-repeat"); !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestExpandBackgroundShorthand_DataURI(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(data:image/png;base64,iVBORw0KGgo=) no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("background data URI: got (%q, %v)", url, ok)
	}

	if repeat, ok := s.Get("background-repeat"); !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestExpandBackgroundShorthand_ColorOnly(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "yellow")

	if color, ok := s.Get("background-color"); !ok || color != "yellow" {
		t.Errorf("background-color: got (%q, %v)", color, ok)
	}

	_, ok := s.GetBackgroundImage()
	if ok {
		t.Error("expected no background-image for color-only background")
	}
}

func TestGetBackgroundRepeat(t *testing.T) {
	tests := []struct {
		value string
		want  BackgroundRepeat
	}{
		{"no-repeat", BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatNoRepeat}},
		{"repeat-x", BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatNoRepeat}},
		{"repeat-y", BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatRepeat}},
		{"repeat", BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}},
		{"space", BackgroundRepeat{X: BackgroundRepeatSpace, Y: BackgroundRepeatSpace}},
		{"round", BackgroundRepeat{X: BackgroundRepeatRound, Y: BackgroundRepeatRound}},
		{"space round", BackgroundRepeat{X: BackgroundRepeatSpace, Y: BackgroundRepeatRound}},
		{"no-repeat space", BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatSpace}},
	}

	for _, tt := range tests {
		s := NewStyle()
		s.Set("background-repeat", tt.value)
		if got := s.GetBackgroundRepeat(); got != tt.want {
			t.Errorf("GetBackgroundRepeat() for %q = %+v, want %+v", tt.value, got, tt.want)
		}
	}
}

func TestGetBackgroundRepeat_Default(t *testing.T) {
	s := NewStyle()
	want := BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
	if got := s.GetBackgroundRepeat(); got != want {
		t.Errorf("default GetBackgroundRepeat() = %+v, want %+v", got, want)
	}
}

// positionLayer maps the element's background-position onto a FillLayer.
func positionLayer(s *Style) *FillLayer {
	fl := &FillLayer{}
	s.MapFillPosition(fl)
	return fl
}

func TestMapFillPosition(t *testing.T) {
	s := NewStyle()
	s.Set("background-position", "-46px 0")
	fl := positionLayer(s)
	if fl.PositionX != (Length{Fixed: -46}) || fl.PositionY != (Length{}) {
		t.Errorf("position = %+v %+v, want -46px 0", fl.PositionX, fl.PositionY)
	}
}

func TestMapFillPosition_Default(t *testing.T) {
	fl := positionLayer(NewStyle())
	if fl.PositionX != (Length{}) || fl.PositionY != (Length{}) ||
		fl.BackgroundXOrigin != BackgroundEdgeOriginLeft || fl.BackgroundYOrigin != BackgroundEdgeOriginTop {
		t.Errorf("default position = %+v", *fl)
	}
}

// A negative percentage is a percentage, not a pixel length (LOU-598):
// -7% of a 10px available extent is -0.7px, truncated to LayoutUnit.
func TestMapFillPosition_NegativePercentage(t *testing.T) {
	s := NewStyle()
	s.Set("background-position", "50% -7%")
	fl := positionLayer(s)
	if fl.PositionY != (Length{Percent: -7}) {
		t.Fatalf("PositionY = %+v, want -7%%", fl.PositionY)
	}
	if got := fillPositionY(fl, 20, 10); got != -0.6875 {
		t.Errorf("resolved y = %v, want -0.6875", got)
	}
}

// Edge + offset pairs keep the offset and record the origin edge
// (CSSToStyleMap::MapFillPositionX/Y, css_to_style_map.cc:200-238 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestMapFillPosition_EdgeOffsetOrigins(t *testing.T) {
	s := NewStyle()
	s.Set("background-position", "right 25px bottom 10%")
	fl := positionLayer(s)
	if fl.PositionX != (Length{Fixed: 25}) || fl.BackgroundXOrigin != BackgroundEdgeOriginRight ||
		fl.PositionY != (Length{Percent: 10}) || fl.BackgroundYOrigin != BackgroundEdgeOriginBottom {
		t.Errorf("position = %+v", *fl)
	}
}

// TestBackgroundPositionMinMaxNegativeBase reproduces LOU-184: min()/max()/
// clamp() in background-position resolved against a NEGATIVE base (positioning
// area smaller than the image). WPT
// css-backgrounds/background-position-negative-percentage-comparison.html sets
// `background-position: min(0%, 100%) max(0%, 100%)` on a 50x50 box with a
// 100x100 image and expects it to render as `right top`. Because the base is
// (area - image) = 50-100 = -50, min()/max() must compare at the used-value
// (offset) level, not the percentage level — which inverts the naive ordering:
//
//	X = min(used(0%), used(100%)) = min(0, -50) = -50  (the 100% / `right` branch)
//	Y = max(used(0%), used(100%)) = max(0, -50) =  0   (the 0%  / `top`   branch)
//
// The math function is a Length::kCalculated evaluated with the base
// (CalculationValue::Evaluate), so the comparison happens on used values.
func TestBackgroundPositionMinMaxNegativeBase(t *testing.T) {
	const area, image = 50.0, 100.0 // base = area - image = -50
	tests := []struct {
		name  string
		value string
		wantX float64
		wantY float64
	}{
		{"min/max negative base", "min(0%, 100%) max(0%, 100%)", -50, 0},
		// Sanity: against this same negative base, `right top` is the reference.
		{"right top reference", "right top", -50, 0},
		// clamp() resolves at the used-value level too. At base -50 the used
		// values invert, so clamp(MIN, VAL, MAX) is compared as
		// max(used(MIN), min(used(VAL), used(MAX))):
		//   X: clamp(100%, 50%, 0%) -> max(-50, min(-25, 0)) = max(-50,-25) = -25
		//   Y: clamp(0%, 50%, 100%) -> max(0, min(-25, -50)) = max(0,-50)   = 0
		{"clamp negative base", "clamp(100%, 50%, 0%) clamp(0%, 50%, 100%)", -25, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStyle()
			s.Set("background-position", tt.value)
			fl := positionLayer(s)
			gotX := fillPositionX(fl, area, image)
			gotY := fillPositionY(fl, area, image)
			if gotX != tt.wantX || gotY != tt.wantY {
				t.Errorf("position(%q) = (%v, %v), want (%v, %v)",
					tt.value, gotX, gotY, tt.wantX, tt.wantY)
			}
		})
	}
}

// TestBackgroundPositionMinMaxPositiveBase guards the ordinary (positive-base)
// case: when the positioning area is larger than the image, min()/max() at the
// used-value level reduces to the naive percentage ordering.
func TestBackgroundPositionMinMaxPositiveBase(t *testing.T) {
	const area, image = 200.0, 100.0 // base = +100
	s := NewStyle()
	s.Set("background-position", "min(0%, 100%) max(0%, 100%)")
	fl := positionLayer(s)
	// X = min(used(0%), used(100%)) = min(0, 100) = 0   (left)
	// Y = max(used(0%), used(100%)) = max(0, 100) = 100 (bottom)
	if gotX := fillPositionX(fl, area, image); gotX != 0 {
		t.Errorf("x positive base = %v, want 0", gotX)
	}
	if gotY := fillPositionY(fl, area, image); gotY != 100 {
		t.Errorf("y positive base = %v, want 100", gotY)
	}
}

func TestExpandBackgroundShorthand_WithPosition(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(sprite.png) -46px 0 no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "sprite.png" {
		t.Errorf("background-image: got (%q, %v)", url, ok)
	}

	pos, ok := s.Get("background-position")
	if !ok || pos != "-46px 0" {
		t.Errorf("background-position: got (%q, %v)", pos, ok)
	}

	repeat, ok := s.Get("background-repeat")
	if !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestParseInlineStyle_BackgroundImage(t *testing.T) {
	s := ParseInlineStyle("background-image: url(test.png)", nil)
	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("ParseInlineStyle background-image: got (%q, %v)", url, ok)
	}
}

// TestParseLinearGradient_RejectsInvalidAngleUnitSpellings locks in that the
// gradient parser refuses non-canonical angle unit spellings (CSS Values 3
// §6.2: only deg / grad / rad / turn are valid). Regression guard for
// angle-units-001.html.
func TestParseLinearGradient_RejectsInvalidAngleUnitSpellings(t *testing.T) {
	invalid := []string{
		"linear-gradient(90degree, red, red)",
		"linear-gradient(100gradian, red, red)",
		"linear-gradient(1.57radian, red, red)",
		"linear-gradient(0.25turns, red, red)",
	}
	for _, val := range invalid {
		if _, ok := ParseLinearGradient(val); ok {
			t.Errorf("ParseLinearGradient(%q) = ok; want rejected", val)
		}
	}
}

// TestParseLinearGradient_AcceptsCanonicalAngleUnits locks in that the four
// canonical angle unit spellings parse as gradient directions, case-
// insensitively (CSS Values 3 §3.5).
func TestParseLinearGradient_AcceptsCanonicalAngleUnits(t *testing.T) {
	valid := []string{
		"linear-gradient(90deg, red, red)",
		"linear-gradient(90DeG, red, red)",
		"linear-gradient(100grad, red, red)",
		"linear-gradient(1.57rad, red, red)",
		"linear-gradient(0.25turn, red, red)",
		"linear-gradient(0.25TURN, red, red)",
	}
	for _, val := range valid {
		if _, ok := ParseLinearGradient(val); !ok {
			t.Errorf("ParseLinearGradient(%q) = !ok; want accepted", val)
		}
	}
}

// TestParseStylesheet_InvalidGradientDoesNotOverwriteValid is the cascade-
// gate regression guard for angle-units-001.html. An invalid
// background-image declaration must NOT overwrite an earlier valid one —
// the parser must drop the declaration before it enters the cascade.
func TestParseStylesheet_InvalidGradientDoesNotOverwriteValid(t *testing.T) {
	css := `div {
		background-image: linear-gradient(green, green);
		background-image: linear-gradient(90degree, red, red);
		background-image: linear-gradient(100gradian, red, red);
		background-image: linear-gradient(1.57radian, red, red);
		background-image: linear-gradient(0.25turns, red, red);
	}`
	ss, err := ParseStylesheet(css, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet error: %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ss.Rules))
	}
	got, ok := ss.Rules[0].Declarations["background-image"]
	if !ok {
		t.Fatalf("background-image not in declarations")
	}
	if got != "linear-gradient(green, green)" {
		t.Errorf("background-image: got %q, want %q", got, "linear-gradient(green, green)")
	}
}

// fillPositionX / fillPositionY resolve a layer's position for a tile of
// size tile in an area of size area, as BackgroundImageGeometry's
// ResolveXPosition / ResolveYPosition do (core/paint/
// background_image_geometry.cc:63-87 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with no offset.
func fillPositionX(fl *FillLayer, area, tile float64) float64 {
	return resolveFillPositionAxis(fl.PositionX, fl.BackgroundXOrigin == BackgroundEdgeOriginRight, area-tile)
}

func fillPositionY(fl *FillLayer, area, tile float64) float64 {
	return resolveFillPositionAxis(fl.PositionY, fl.BackgroundYOrigin == BackgroundEdgeOriginBottom, area-tile)
}

func resolveFillPositionAxis(l Length, farEdge bool, available float64) float64 {
	avail := layoutunit.FromFloat64Round(available)
	v := l.MinimumValueForLength(avail)
	if farEdge {
		v = avail.Sub(v)
	}
	return v.Float64()
}
