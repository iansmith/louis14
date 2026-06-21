package css

import (
	"fmt"
	"strings"
)

// ToRoman converts a positive integer to Roman numeral notation.
// Returns the decimal representation as a fallback for values outside [1, 3999].
func ToRoman(n int) string {
	if n <= 0 || n > 3999 {
		return fmt.Sprintf("%d", n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// ToAlpha converts a positive integer to alphabetic notation (a=1, b=2, ..., z=26, aa=27, ...).
// Returns lowercase letters. Use strings.ToUpper on the result for uppercase.
func ToAlpha(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}
	var b strings.Builder
	for n > 0 {
		n-- // make 0-indexed
		b.WriteByte(byte('a' + n%26))
		n /= 26
	}
	// Reverse
	s := []byte(b.String())
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return string(s)
}

// ToGreek converts a positive integer to lower Greek letter notation (α=1, β=2, ...).
// Returns the decimal representation as a fallback for values outside [1, 24].
func ToGreek(n int) string {
	greek := []rune{'α', 'β', 'γ', 'δ', 'ε', 'ζ', 'η', 'θ', 'ι', 'κ', 'λ', 'μ', 'ν', 'ξ', 'ο', 'π', 'ρ', 'σ', 'τ', 'υ', 'φ', 'χ', 'ψ', 'ω'}
	if n <= 0 || n > len(greek) {
		return fmt.Sprintf("%d", n)
	}
	return string(greek[n-1])
}

// Hebrew numeral letters by place value (built once), mirroring Blink's
// HebrewAlgorithm (core/css/counter_style.cc).
var (
	hebrewHundreds = []rune{'ק', 'ר', 'ש'}                               // 100, 200, 300
	hebrewTens     = []rune{'י', 'כ', 'ל', 'מ', 'נ', 'ס', 'ע', 'פ', 'צ'} // 10..90
	hebrewOnes     = []rune{'א', 'ב', 'ג', 'ד', 'ה', 'ו', 'ז', 'ח', 'ט'} // 1..9
	hebrew15or16   = []rune{'ו', 'ז'}                                    // ones of 15 / 16
)

// hebrewUnder1000 renders 1..999 as bare Hebrew letters (no gershayim),
// mirroring Blink's HebrewAlgorithmUnder1000: a run of ת (400) for the
// four-hundreds, then ק/ר/ש for the remaining hundreds, then tens+ones — with
// 15 and 16 special-cased to טו / טז to avoid spelling a divine name. n must be
// in [0, 999]; 0 yields the empty string.
func hebrewUnder1000(n int) string {
	var b strings.Builder
	for i := 0; i < n/400; i++ {
		b.WriteRune('ת') // 400
	}
	n %= 400
	if h := n / 100; h > 0 {
		b.WriteRune(hebrewHundreds[h-1])
	}
	n %= 100
	if n == 15 || n == 16 {
		b.WriteRune('ט') // 9
		b.WriteRune(hebrew15or16[n-15])
		return b.String()
	}
	if t := n / 10; t > 0 {
		b.WriteRune(hebrewTens[t-1])
	}
	if o := n % 10; o > 0 {
		b.WriteRune(hebrewOnes[o-1])
	}
	return b.String()
}

// ToHebrew renders a positive integer as a Hebrew numeral using the additive
// system, mirroring Blink's HebrewAlgorithm. Bare letters for 1..999; for
// n >= 1000 a geresh (U+05F3) separates the thousands letters from the
// remainder (e.g. 1000 → "א׳", 1500 → "א׳תק"). Falls back to decimal outside
// the predefined `hebrew` style's range (1..10999), matching Blink.
func ToHebrew(n int) string {
	if n <= 0 || n > 10999 {
		return fmt.Sprintf("%d", n)
	}
	if n <= 999 {
		return hebrewUnder1000(n)
	}
	return hebrewUnder1000(n/1000) + "׳" + hebrewUnder1000(n%1000)
}

// ToCJKDecimal converts a positive integer to CJK decimal notation by substituting
// each decimal digit with its CJK ideographic equivalent (〇=0, 一=1, ..., 九=9).
// E.g., 10 → "一〇", 42 → "四二". Returns decimal as fallback for n <= 0.
func ToCJKDecimal(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}
	digits := []rune{'〇', '一', '二', '三', '四', '五', '六', '七', '八', '九'}
	decimal := fmt.Sprintf("%d", n)
	var b strings.Builder
	for _, ch := range decimal {
		b.WriteRune(digits[ch-'0'])
	}
	return b.String()
}

// Korean Hangul formal numerals (built once), mirroring Blink's
// CJKIdeoGraphicAlgorithm. Index 0 of the place / group-marker arrays is the
// ones position, which carries no marker.
var (
	koreanDigits     = []rune{'영', '일', '이', '삼', '사', '오', '육', '칠', '팔', '구'}
	koreanPlaces     = [4]rune{0, '십', '백', '천'} // position within a 4-digit group
	koreanPlacePow   = [4]int{1, 10, 100, 1000}
	koreanGroupMarks = [4]rune{0, '만', '억', '조'} // group number: 10^0/10^4/10^8/10^12
	koreanGroupDiv   = [4]int{1, 1_0000, 1_0000_0000, 1_0000_0000_0000}
)

// koreanFourDigitGroup renders 1..9999 in the formal style: each non-zero place
// emits its digit + place marker (the ones place has no marker), zero places
// skipped. Formal keeps the leading 일 (informal drops it).
func koreanFourDigitGroup(g int) string {
	var b strings.Builder
	for pos := 3; pos >= 0; pos-- {
		d := (g / koreanPlacePow[pos]) % 10
		if d == 0 {
			continue
		}
		b.WriteRune(koreanDigits[d])
		if koreanPlaces[pos] != 0 {
			b.WriteRune(koreanPlaces[pos])
		}
	}
	return b.String()
}

// ToKoreanHangulFormal renders a positive integer in the formal Sino-Korean
// place-value system, mirroring Blink's CJKIdeoGraphicAlgorithm (formal branch).
// The number is split into 4-digit groups joined by 만 (10^4) / 억 (10^8) /
// 조 (10^12); so 10 → 일십, 100 → 일백, 1234 → 일천이백삼십사. Falls back to decimal
// for n <= 0 and beyond the 조 range.
func ToKoreanHangulFormal(n int) string {
	const maxN = 9999_9999_9999_9999 // 16 digits: up to the 조 (10^12) group
	if n <= 0 || n > maxN {
		return fmt.Sprintf("%d", n)
	}
	var b strings.Builder
	for gi := 3; gi >= 0; gi-- {
		gv := (n / koreanGroupDiv[gi]) % 1_0000
		if gv == 0 {
			continue
		}
		b.WriteString(koreanFourDigitGroup(gv))
		if koreanGroupMarks[gi] != 0 {
			b.WriteRune(koreanGroupMarks[gi])
		}
	}
	return b.String()
}
