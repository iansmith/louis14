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

// toBoundedRuneStyle maps a 1-based ordinal to a per-style symbol table,
// returning the symbol at position n or a decimal fallback when n is outside
// [1, len(symbols)]. This is the bounded form shared by the symbolic styles
// below; full additive / place-value composition for larger ordinals is tracked
// in LOU-314.
func toBoundedRuneStyle(n int, symbols []rune) string {
	if n <= 0 || n > len(symbols) {
		return fmt.Sprintf("%d", n)
	}
	return string(symbols[n-1])
}

// ToHebrew converts a positive integer to Hebrew letter notation (א=1, ב=2, ...).
// Bounded form: only [1, 22]; n >= 23 falls back to decimal, which is NOT
// spec-correct — real `hebrew` is an additive system to 999. Full algorithm
// tracked in LOU-314.
func ToHebrew(n int) string {
	hebrew := []rune{'א', 'ב', 'ג', 'ד', 'ה', 'ו', 'ז', 'ח', 'ט', 'י', 'כ', 'ל', 'מ', 'נ', 'ס', 'ע', 'פ', 'צ', 'ק', 'ר', 'ש', 'ת'}
	return toBoundedRuneStyle(n, hebrew)
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

// ToKoreanHangulFormal converts a positive integer to Korean Hangul formal digit letters.
// Bounded form: only [1, 9]; n >= 10 falls back to decimal, which is NOT
// spec-correct — real `korean-hangul-formal` composes place values (십/백/천/만).
// Full algorithm tracked in LOU-314.
func ToKoreanHangulFormal(n int) string {
	korean := []rune{'일', '이', '삼', '사', '오', '육', '칠', '팔', '구'}
	return toBoundedRuneStyle(n, korean)
}
