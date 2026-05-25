package css

import "testing"

func TestParseInlineStyle_SingleProperty(t *testing.T) {
	style := ParseInlineStyle("color: red", nil)
	value, ok := style.Get("color")
	if !ok || value != "red" {
		t.Error("expected color='red'")
	}
}

func TestParseInlineStyle_MultipleProperties(t *testing.T) {
	style := ParseInlineStyle("color: red; width: 100px", nil)
	color, _ := style.Get("color")
	width, _ := style.Get("width")
	if color != "red" || width != "100px" {
		t.Error("expected both properties to parse")
	}
}

func TestParseInlineStyle_DataURLContainingSemicolon(t *testing.T) {
	// Mirrors the blur-text WPT reftest: a `filter: url("data:...")` with
	// a `;` inside the data URL's MIME prefix must survive declaration
	// splitting. Before splitInlineStyleDeclarations this was broken by
	// a naive strings.Split(";") that mangled the data URL into two
	// half-declarations.
	style := ParseInlineStyle(`filter: url("data:image/svg+xml;utf8,<svg><filter id='b'/></svg>#b")`, nil)
	filters := style.GetFilter()
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter function, got %d (%+v)", len(filters), filters)
	}
	if filters[0].Name != "url" {
		t.Errorf("filter name = %q, want url", filters[0].Name)
	}
	wantURL := `data:image/svg+xml;utf8,<svg><filter id='b'/></svg>#b`
	// data: URLs are absolute and bypass parse-time base-relative
	// resolution, so both URL forms equal the raw value.
	if filters[0].URL.Absolute != wantURL {
		t.Errorf("filter URL.Absolute:\n  got  = %q\n  want = %q", filters[0].URL.Absolute, wantURL)
	}
	if filters[0].URL.Relative != wantURL {
		t.Errorf("filter URL.Relative:\n  got  = %q\n  want = %q", filters[0].URL.Relative, wantURL)
	}
}

func TestParseInlineStyle_SemicolonInsideQuotes(t *testing.T) {
	// A quoted string containing `;` must not split the surrounding
	// declaration (e.g. `content: "a;b"` is one decl).
	style := ParseInlineStyle(`color: red; content: "a;b"; width: 10px`, nil)
	c, _ := style.Get("color")
	w, _ := style.Get("width")
	if c != "red" {
		t.Errorf("color = %q, want red", c)
	}
	if w != "10px" {
		t.Errorf("width = %q, want 10px", w)
	}
}

func TestGetLength_PixelValue(t *testing.T) {
	style := ParseInlineStyle("width: 100px", nil)
	width, ok := style.GetLength("width")
	if !ok || width != 100.0 {
		t.Errorf("expected width=100.0, got %f", width)
	}
}

func TestParseColor_BasicColors(t *testing.T) {
	tests := map[string]Color{
		"red":   {255, 0, 0, 1.0},
		"blue":  {0, 0, 255, 1.0},
		"green": {0, 128, 0, 1.0},
	}
	for name, expected := range tests {
		color, ok := ParseColor(name)
		if !ok || color != expected {
			t.Errorf("color %s: expected %+v, got %+v", name, expected, color)
		}
	}
}

// Phase 2 tests: Box model properties

func TestParseInlineStyle_MarginShorthand(t *testing.T) {
	style := ParseInlineStyle("margin: 10px", nil)
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Right != 10 || margin.Bottom != 10 || margin.Left != 10 {
		t.Errorf("expected all margins to be 10, got %+v", margin)
	}
}

func TestParseInlineStyle_MarginTwoValues(t *testing.T) {
	style := ParseInlineStyle("margin: 10px 20px", nil)
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Bottom != 10 {
		t.Errorf("expected top/bottom margins to be 10, got %+v", margin)
	}
	if margin.Right != 20 || margin.Left != 20 {
		t.Errorf("expected left/right margins to be 20, got %+v", margin)
	}
}

func TestParseInlineStyle_MarginFourValues(t *testing.T) {
	style := ParseInlineStyle("margin: 10px 20px 30px 40px", nil)
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Right != 20 || margin.Bottom != 30 || margin.Left != 40 {
		t.Errorf("expected margins 10,20,30,40, got %+v", margin)
	}
}

func TestParseInlineStyle_PaddingShorthand(t *testing.T) {
	style := ParseInlineStyle("padding: 15px", nil)
	padding := style.GetPadding()

	if padding.Top != 15 || padding.Right != 15 || padding.Bottom != 15 || padding.Left != 15 {
		t.Errorf("expected all padding to be 15, got %+v", padding)
	}
}

func TestParseInlineStyle_BorderShorthand(t *testing.T) {
	style := ParseInlineStyle("border: 2px solid black", nil)

	borderWidth := style.GetBorderWidth()
	if borderWidth.Top != 2 || borderWidth.Right != 2 {
		t.Errorf("expected border width to be 2, got %+v", borderWidth)
	}

	borderStyle, ok := style.Get("border-style")
	if !ok || borderStyle != "solid" {
		t.Errorf("expected border-style 'solid', got '%s'", borderStyle)
	}

	borderColor, ok := style.Get("border-color")
	if !ok || borderColor != "black" {
		t.Errorf("expected border-color 'black', got '%s'", borderColor)
	}
}

func TestParseInlineStyle_IndividualMargins(t *testing.T) {
	style := ParseInlineStyle("margin-top: 5px; margin-left: 10px", nil)
	margin := style.GetMargin()

	if margin.Top != 5 {
		t.Errorf("expected margin-top 5, got %f", margin.Top)
	}
	if margin.Left != 10 {
		t.Errorf("expected margin-left 10, got %f", margin.Left)
	}
	if margin.Right != 0 || margin.Bottom != 0 {
		t.Errorf("expected other margins to be 0, got %+v", margin)
	}
}

func TestParseInlineStyle_CombinedBoxModel(t *testing.T) {
	style := ParseInlineStyle("margin: 10px; padding: 20px; border: 1px solid red", nil)

	margin := style.GetMargin()
	if margin.Top != 10 {
		t.Errorf("expected margin 10, got %+v", margin)
	}

	padding := style.GetPadding()
	if padding.Top != 20 {
		t.Errorf("expected padding 20, got %+v", padding)
	}

	borderWidth := style.GetBorderWidth()
	if borderWidth.Top != 1 {
		t.Errorf("expected border width 1, got %+v", borderWidth)
	}
}

// ----- will-change canonical value model tests --------------------------

func TestGetWillChangeData_AutoReturnsNil(t *testing.T) {
	s := NewStyle()
	if wc := s.GetWillChangeData(); wc != nil {
		t.Errorf("unset will-change should return nil, got %+v", wc)
	}
	s.Set("will-change", "auto")
	if wc := s.GetWillChangeData(); wc != nil {
		t.Errorf("will-change:auto should return nil, got %+v", wc)
	}
	s.Set("will-change", "")
	if wc := s.GetWillChangeData(); wc != nil {
		t.Errorf("empty will-change should return nil, got %+v", wc)
	}
}

func TestGetWillChangeData_TransformSetsNarrowAndBroadHints(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "transform")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:transform should return non-nil WillChange")
	}
	if !wc.HasTransformProperty {
		t.Error("transform should set HasTransformProperty (narrow)")
	}
	if !wc.HasAnyTransformProperty {
		t.Error("transform should set HasAnyTransformProperty (broad)")
	}
	if !s.HasWillChangeProperty("transform") {
		t.Error("HasWillChangeProperty(transform) should be true")
	}
}

func TestGetWillChangeData_TranslateBroadOnly(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "translate")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:translate should return non-nil WillChange")
	}
	if wc.HasTransformProperty {
		t.Error("translate must NOT set narrow HasTransformProperty")
	}
	if !wc.HasAnyTransformProperty {
		t.Error("translate must set broad HasAnyTransformProperty")
	}
}

func TestGetWillChangeData_MaskShorthandExpandsToMaskImage(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "mask")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:mask should return non-nil WillChange")
	}
	if wc.Longhands["mask"] {
		t.Error("mask is a shorthand and must not appear in resolved longhand set")
	}
	if !wc.Longhands["mask-image"] {
		t.Error("mask must expand to mask-image longhand")
	}
	if !wc.Longhands["-webkit-mask-box-image-source"] {
		t.Error("mask must also expand to -webkit-mask-box-image-source per Blink's SC set")
	}
}

func TestGetWillChangeData_WebkitPerspectiveAlias(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "-webkit-perspective")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:-webkit-perspective should return non-nil WillChange")
	}
	if wc.Longhands["-webkit-perspective"] {
		t.Error("-webkit-perspective alias must be normalized away from resolved longhands")
	}
	if !wc.Longhands["perspective"] {
		t.Error("-webkit-perspective must normalize to perspective longhand")
	}
	if !wc.HasAnyTransformProperty {
		t.Error("perspective is in the broad transform set; HasAnyTransformProperty should be true")
	}
}

func TestGetWillChangeData_MultiToken(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "transform, opacity")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:transform,opacity should return non-nil WillChange")
	}
	if !wc.Longhands["transform"] || !wc.Longhands["opacity"] {
		t.Errorf("expected both transform and opacity longhands, got %v", wc.Longhands)
	}
	if !wc.HasTransformProperty || !wc.HasAnyTransformProperty {
		t.Error("transform present in multi-token list must set both transform hints")
	}
	if len(wc.Values) != 2 || wc.Values[0] != "transform" || wc.Values[1] != "opacity" {
		t.Errorf("Values should preserve source order, got %v", wc.Values)
	}
}

func TestGetWillChangeData_ScrollPosition(t *testing.T) {
	s := NewStyle()
	s.Set("will-change", "scroll-position")
	wc := s.GetWillChangeData()
	if wc == nil {
		t.Fatal("will-change:scroll-position should return non-nil WillChange")
	}
	if !wc.HasScrollPosition {
		t.Error("scroll-position must set HasScrollPosition flag")
	}
	if wc.Longhands["scroll-position"] {
		t.Error("scroll-position is not a longhand and must not appear in Longhands set")
	}
	if !s.HasWillChangeScrollPosition() {
		t.Error("HasWillChangeScrollPosition() accessor should return true")
	}
}

func TestWillChangeCreatesStackingContext_BlinkCanonicalSet(t *testing.T) {
	// Verbatim set from computed_style.cc:1319 @ SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	cases := []struct {
		ident         string
		allowsZIndex  bool
		expectCreates bool
	}{
		{"opacity", false, true},
		{"transform", false, true},
		{"transform-style", false, true},
		{"perspective", false, true},
		{"translate", false, true},
		{"rotate", false, true},
		{"scale", false, true},
		{"offset-path", false, true},
		{"offset-position", false, true},
		{"mask-image", false, true},
		{"-webkit-mask-box-image-source", false, true},
		{"clip-path", false, true},
		{"-webkit-box-reflect", false, true},
		{"filter", false, true},
		{"backdrop-filter", false, true},
		{"position", false, true},
		{"mix-blend-mode", false, true},
		{"isolation", false, true},
		{"contain", false, true},
		{"view-transition-name", false, true},
		// z-index gated on AllowsZIndex flag.
		{"z-index", false, false},
		{"z-index", true, true},
		// Negative case: an ident not in the SC set.
		{"color", false, false},
		// `auto` returns nil → no SC.
		{"auto", true, false},
	}
	for _, c := range cases {
		s := NewStyle()
		s.Set("will-change", c.ident)
		got := s.WillChangeCreatesStackingContext(c.allowsZIndex)
		if got != c.expectCreates {
			t.Errorf("WillChangeCreatesStackingContext(%q, allowsZIndex=%v) = %v, want %v",
				c.ident, c.allowsZIndex, got, c.expectCreates)
		}
	}
}

func TestWillChangeCreatesStackingContext_MaskShorthandRoutes(t *testing.T) {
	// `mask` is a shorthand; Blink's SC set contains `mask-image` and
	// `-webkit-mask-box-image-source` but never `mask` itself. We must
	// nevertheless return true for `will-change: mask` via shorthand
	// expansion (this is the documented fix for
	// will-change-stacking-context-mask-1).
	s := NewStyle()
	s.Set("will-change", "mask")
	if !s.WillChangeCreatesStackingContext(false) {
		t.Error("will-change:mask should create a stacking context via shorthand expansion to mask-image")
	}
}

func TestGetTextEmphasisLineLogicalOver_BlinkCollapseTable(t *testing.T) {
	// Mirrors Blink's ComputedStyle::GetTextEmphasisLineLogicalSide() at
	// third_party/blink/renderer/core/style/computed_style.cc @
	// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	cases := []struct {
		pos          string
		isHorizontal bool
		isSidewaysLR bool
		wantOver     bool
	}{
		// Horizontal-tb: over/under collapse directly.
		{"over right", true, false, true},
		{"over left", true, false, true},
		{"under right", true, false, false},
		{"under left", true, false, false},
		// Vertical-rl / vertical-lr / sideways-rl: right → kOver.
		{"over right", false, false, true},
		{"over left", false, false, false},
		{"under right", false, false, true},
		{"under left", false, false, false},
		// Sideways-lr: left → kOver (the flip).
		{"over right", false, true, false},
		{"over left", false, true, true},
		{"under right", false, true, false},
		{"under left", false, true, true},
		// Default (empty value → "over right").
		{"", true, false, true},
		{"", false, false, true},
		{"", false, true, false},
		// `auto` value defaults to kOver for horizontal-tb and the
		// vertical-non-sideways-lr family, kUnder for sideways-lr.
		{"auto", true, false, true},
		{"auto", false, false, true},
		{"auto", false, true, false},
		// Single-axis values: missing axis falls back to the writing-mode's
		// default. Horizontal: missing over/under → kOver. Vertical: missing
		// right/left → kOver.
		{"right", true, false, true},
		{"left", true, false, true},
		{"over", false, false, true},
		{"under", false, false, true},
	}
	for _, c := range cases {
		s := NewStyle()
		if c.pos != "" {
			s.Set("text-emphasis-position", c.pos)
		}
		got := s.GetTextEmphasisLineLogicalOver(c.isHorizontal, c.isSidewaysLR)
		if got != c.wantOver {
			t.Errorf("GetTextEmphasisLineLogicalOver(pos=%q, horizontal=%v, sidewaysLR=%v) = %v, want %v",
				c.pos, c.isHorizontal, c.isSidewaysLR, got, c.wantOver)
		}
	}
}
