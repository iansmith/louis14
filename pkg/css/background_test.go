package css

import (
	"testing"
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

func TestGetBackgroundPosition(t *testing.T) {
	s := NewStyle()
	s.Set("background-position", "-46px 0")
	pos := s.GetBackgroundPosition()
	if pos.X != -46 || pos.Y != 0 {
		t.Errorf("GetBackgroundPosition() = (%v, %v), want (-46, 0)", pos.X, pos.Y)
	}
}

func TestGetBackgroundPosition_Default(t *testing.T) {
	s := NewStyle()
	pos := s.GetBackgroundPosition()
	if pos.X != 0 || pos.Y != 0 {
		t.Errorf("default GetBackgroundPosition() = (%v, %v), want (0, 0)", pos.X, pos.Y)
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
		// clamp() should also resolve at the used-value level.
		// clamp(0%, 100%, 100%) = max(0%, min(100%,100%)) = 100% -> used -50.
		{"clamp negative base", "clamp(0%, 100%, 100%) clamp(0%, 0%, 100%)", -50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStyle()
			s.Set("background-position", tt.value)
			pos := s.GetBackgroundPosition()
			gotX := pos.ResolveX(area, image)
			gotY := pos.ResolveY(area, image)
			if gotX != tt.wantX || gotY != tt.wantY {
				t.Errorf("ResolveX/Y(%q) = (%v, %v), want (%v, %v)",
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
	pos := s.GetBackgroundPosition()
	// X = min(used(0%), used(100%)) = min(0, 100) = 0   (left)
	// Y = max(used(0%), used(100%)) = max(0, 100) = 100 (bottom)
	if gotX := pos.ResolveX(area, image); gotX != 0 {
		t.Errorf("ResolveX positive base = %v, want 0", gotX)
	}
	if gotY := pos.ResolveY(area, image); gotY != 100 {
		t.Errorf("ResolveY positive base = %v, want 100", gotY)
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
