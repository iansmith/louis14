package css

import "testing"

// Phase-0 RED tests for LOU-314: the full additive (hebrew) and place-value
// (korean-hangul-formal) CSS Counter Styles algorithms. Expected values are
// the Blink-faithful outputs verified against Chromium core/css/counter_style.cc
// (HebrewAlgorithm / HebrewAlgorithmUnder1000 / CJKIdeoGraphicAlgorithm) @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// hebrew emits BARE letters for 1–999 (no gershayim); geresh (U+05F3) appears
// only between thousands and remainder for n≥1000. korean-hangul-formal is the
// FORMAL variant, which keeps the leading 일 (one) before 십/백/천 (informal
// drops it).

func TestToHebrew_AdditiveAlgorithm(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		// 1–10: alphabetical position == numeral value (already correct today).
		{1, "א"}, {9, "ט"}, {10, "י"},
		// 11–22: composed; bounded form returns the WRONG single letter today.
		{11, "יא"}, {15, "טו"}, {16, "טז"}, {17, "יז"}, {18, "יח"},
		{19, "יט"}, {20, "כ"}, {22, "כב"}, {23, "כג"},
		// hundreds + composed tens/ones; decimal fallback today.
		{100, "ק"}, {200, "ר"}, {400, "ת"}, {500, "תק"}, {900, "תתק"},
		{999, "תתקצט"},
		// thousands: geresh (U+05F3) between thousands and remainder.
		{1000, "א׳"}, {1500, "א׳תק"},
	}
	for _, c := range cases {
		if got := ToHebrew(c.n); got != c.want {
			t.Errorf("ToHebrew(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestToKoreanHangulFormal_PlaceValue(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		// 1–9: single digits (already correct today).
		{1, "일"}, {9, "구"},
		// place-value composition; decimal fallback today. Formal keeps 일.
		{10, "일십"}, {11, "일십일"}, {20, "이십"}, {23, "이십삼"},
		{100, "일백"}, {200, "이백"}, {1000, "일천"},
		{1234, "일천이백삼십사"},
		// 만 (10^4) group separator.
		{10000, "일만"}, {12345, "일만이천삼백사십오"},
	}
	for _, c := range cases {
		if got := ToKoreanHangulFormal(c.n); got != c.want {
			t.Errorf("ToKoreanHangulFormal(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
