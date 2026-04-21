package text

// IsVerticalScriptCharacter reports whether r belongs to a script natively
// written vertically. Per UTR#50, these scripts' glyphs are designed for
// vertical flow — their horizontal advance equals the cell used in vertical
// upright mode. Mirrors the Mongolian subset of Blink's
// Character::IsUprightInMixedVertical (property VO = U/Tu for these ranges).
func IsVerticalScriptCharacter(r rune) bool {
	switch {
	case r >= 0x1800 && r <= 0x18AF: // Mongolian
		return true
	case r >= 0xA840 && r <= 0xA87F: // Phags-Pa
		return true
	case r >= 0x11660 && r <= 0x1166C: // Mongolian Supplement
		return true
	}
	return false
}

// ShouldRotateSideways returns true if the character should be rotated
// 90° CW in vertical text with text-orientation: mixed.
// Based on Unicode UTR#50 Vertical_Orientation property:
//
//	R (Rotated) → rotate sideways → return true
//	U (Upright) → keep upright → return false
//	Tu (Transformed upright) → keep upright → return false
func ShouldRotateSideways(r rune) bool {
	// Common "upright" ranges (CJK, Kana, CJK symbols)
	if r >= 0x2E80 && r <= 0x2EFF {
		return false // CJK Radicals Supplement
	}
	if r >= 0x2F00 && r <= 0x2FDF {
		return false // Kangxi Radicals
	}
	if r >= 0x3000 && r <= 0x303F {
		return false // CJK Symbols and Punctuation
	}
	if r >= 0x3040 && r <= 0x309F {
		return false // Hiragana
	}
	if r >= 0x30A0 && r <= 0x30FF {
		return false // Katakana
	}
	if r >= 0x3100 && r <= 0x312F {
		return false // Bopomofo
	}
	if r >= 0x3130 && r <= 0x318F {
		return false // Hangul Compatibility Jamo
	}
	if r >= 0x3190 && r <= 0x319F {
		return false // Kanbun
	}
	if r >= 0x31A0 && r <= 0x31BF {
		return false // Bopomofo Extended
	}
	if r >= 0x31C0 && r <= 0x31EF {
		return false // CJK Strokes
	}
	if r >= 0x31F0 && r <= 0x31FF {
		return false // Katakana Phonetic Extensions
	}
	if r >= 0x3200 && r <= 0x32FF {
		return false // Enclosed CJK Letters
	}
	if r >= 0x3300 && r <= 0x33FF {
		return false // CJK Compatibility
	}
	if r >= 0x3400 && r <= 0x4DBF {
		return false // CJK Unified Ideographs Extension A
	}
	if r >= 0x4E00 && r <= 0x9FFF {
		return false // CJK Unified Ideographs
	}
	if r >= 0xA000 && r <= 0xA48F {
		return false // Yi Syllables
	}
	if r >= 0xA490 && r <= 0xA4CF {
		return false // Yi Radicals
	}
	if r >= 0xAC00 && r <= 0xD7AF {
		return false // Hangul Syllables
	}
	if r >= 0xF900 && r <= 0xFAFF {
		return false // CJK Compatibility Ideographs
	}
	if r >= 0xFE10 && r <= 0xFE1F {
		return false // Vertical Forms
	}
	if r >= 0xFE30 && r <= 0xFE4F {
		return false // CJK Compatibility Forms
	}
	if r >= 0xFF00 && r <= 0xFFEF {
		return false // Halfwidth and Fullwidth Forms
	}
	// Supplementary CJK ranges
	if r >= 0x20000 && r <= 0x2A6DF {
		return false // CJK Unified Ideographs Extension B
	}
	if r >= 0x2A700 && r <= 0x2B73F {
		return false // CJK Unified Ideographs Extension C
	}
	if r >= 0x2B740 && r <= 0x2B81F {
		return false // CJK Unified Ideographs Extension D
	}

	// Default: rotate (Latin, Cyrillic, Greek, digits, most punctuation)
	return true
}
