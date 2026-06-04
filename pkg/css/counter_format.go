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

// ToHebrew converts a positive integer to Hebrew letter notation (א=1, ב=2, ...).
// This is the bounded form; returns decimal as fallback for values outside [1, 22].
// Full Hebrew additive numeral composition is not implemented.
func ToHebrew(n int) string {
	hebrew := []rune{'א', 'ב', 'ג', 'ד', 'ה', 'ו', 'ז', 'ח', 'ט', 'י', 'כ', 'ל', 'מ', 'נ', 'ס', 'ע', 'פ', 'צ', 'ק', 'ר', 'ש', 'ת'}
	if n <= 0 || n > len(hebrew) {
		return fmt.Sprintf("%d", n)
	}
	return string(hebrew[n-1])
}

// ToCJKDecimal converts a positive integer to CJK ideographic digits (〇=0, 一=1, 二=2, ..., 九=9).
// This implements a basic numeric system; returns decimal as fallback for values >= 10.
func ToCJKDecimal(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}
	if n >= 10 {
		// For simplicity, values >= 10 fall back to decimal.
		// Full positional CJK composition would require more complex rules.
		return fmt.Sprintf("%d", n)
	}
	digits := []rune{'〇', '一', '二', '三', '四', '五', '六', '七', '八', '九'}
	return string(digits[n])
}

// ToKoreanHangulFormal converts a positive integer to Korean Hangul formal digit letters.
// This is the bounded form; returns decimal as fallback for values outside [1, 9].
func ToKoreanHangulFormal(n int) string {
	korean := []rune{'일', '이', '삼', '사', '오', '육', '칠', '팔', '구'}
	if n <= 0 || n > len(korean) {
		return fmt.Sprintf("%d", n)
	}
	return string(korean[n-1])
}
