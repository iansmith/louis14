package css

import "testing"

// LOU-291: the longhand border-*-width properties must resolve the
// thin/medium/thick keywords to their CSS line-width pixel values at
// parse/cascade time, exactly as the shorthand expansion paths already do.
//
// Mirrors Blink's css_longhand::Border{Top,Right,Bottom,Left}Width::
// ParseSingleValue -> ConsumeBorderWidth (which calls ConsumeLineWidth and
// maps thin=1px, medium=3px, thick=5px) in
// third_party/blink/renderer/core/css/properties/longhands/longhands_custom.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Before the fix, a bare `border-bottom-width: medium` declaration falls
// through expandShorthand's default arm (style.go:2966-2968) and is stored
// verbatim as the string "medium". getLengthOrZero -> GetLength cannot parse
// "medium" and returns 0, so the border renders 0px wide.

// borderWidthKeywordPx is the canonical CSS line-width mapping. It mirrors the
// values already encoded in borderWidthKeyword (style.go) so the test asserts
// against the single source of truth rather than a magic literal.
func borderWidthKeywordPx(t *testing.T, keyword string) string {
	t.Helper()
	px, ok := borderWidthKeyword(keyword)
	if !ok {
		t.Fatalf("borderWidthKeyword(%q) returned ok=false; keyword is not a recognized line-width", keyword)
	}
	return px
}

// TestBorderWidthLonghandKeyword_StoredAsPixels verifies that each of the four
// border-*-width longhands resolves thin/medium/thick to pixels before storage,
// for every side/keyword combination (the 12 WPT targets).
func TestBorderWidthLonghandKeyword_StoredAsPixels(t *testing.T) {
	sides := []string{"top", "right", "bottom", "left"}
	keywords := []string{"thin", "medium", "thick"}

	for _, side := range sides {
		for _, kw := range keywords {
			prop := "border-" + side + "-width"
			want := borderWidthKeywordPx(t, kw)

			style := ParseInlineStyle(prop+": "+kw, nil)
			got, ok := style.Get(prop)
			if !ok {
				t.Errorf("%s: %q not stored", prop, kw)
				continue
			}
			if got != want {
				t.Errorf("%s: %q resolved to %q, want %q", prop, kw, got, want)
			}
		}
	}
}

// TestBorderWidthLonghandKeyword_ComputedLength verifies the end-to-end layout
// path: with a visible border-style, GetBorderWidth must report the resolved
// pixel width for a keyword-valued longhand (not 0).
func TestBorderWidthLonghandKeyword_ComputedLength(t *testing.T) {
	cases := []struct {
		side    string
		keyword string
		wantPx  float64
	}{
		{"top", "thin", 1},
		{"right", "medium", 3},
		{"bottom", "thick", 5},
		{"left", "medium", 3},
	}

	for _, c := range cases {
		prop := "border-" + c.side + "-width"
		style := ParseInlineStyle("border-style: solid; "+prop+": "+c.keyword, nil)
		edge := style.GetBorderWidth()

		var got float64
		switch c.side {
		case "top":
			got = edge.Top
		case "right":
			got = edge.Right
		case "bottom":
			got = edge.Bottom
		case "left":
			got = edge.Left
		}
		if got != c.wantPx {
			t.Errorf("%s: %q computed to %vpx, want %vpx", prop, c.keyword, got, c.wantPx)
		}
	}
}
