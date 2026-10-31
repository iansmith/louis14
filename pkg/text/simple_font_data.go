package text

// Glyph is a glyph id within one font; 0 means the font has no glyph for the
// character (it maps to .notdef). Mirrors Blink's Glyph (platform/fonts/
// glyph.h:37 @ 5992657f556bcd9d207a4aca187488462f027cab).
type Glyph uint16

// The probe characters of the per-metric primary fonts. Mirror Blink's
// uchar::kSpace / kDigitZero / kCjkWater (platform/wtf/text/
// character_names.h:48/:56/:194 @ 5992657f556bcd9d207a4aca187488462f027cab).
const (
	kSpace     rune = 0x0020
	kDigitZero rune = 0x0030
	kCjkWater  rune = 0x6C34
)

// glyphForCharacter returns the glyph the font at fontPath maps c to.
func glyphForCharacter(fontPath string, fontSize float64, c rune) Glyph {
	return 0
}
