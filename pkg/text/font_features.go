package text

import (
	"strings"

	"louis14/pkg/css"

	"mazarin/textshape"
)

// FeatureSettings returns the font-feature-settings property's parsed list:
// Blink's FontDescription::FeatureSettings(). A malformed value is ignored
// (nil), as the cascade would have dropped it.
func FeatureSettings(s *css.Style) []textshape.FontFeature {
	if s == nil {
		return nil
	}
	ffs, ok := s.Get("font-feature-settings")
	if !ok || ffs == "" {
		return nil
	}
	settings, _ := ParseFontFeatureSettings(ffs)
	return settings
}

// GetFontFeatures returns the face-independent OpenType feature list text
// in style s shapes with — Blink's Font::GetFontFeatures()
// (platform/fonts/font.cc:212-214 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which
// FontFeatureRange::FromFontDescription builds from the FontDescription
// (platform/fonts/shaping/font_features.cc:33-235 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), preceded by the
// font-variant-caps features HarfBuzzShaper's
// CapsFeatureSettingsScopedOverlay prepends at shaping time
// (platform/fonts/shaping/harfbuzz_shaper.cc:823-857 @
// 5992657f556bcd9d207a4aca187488462f027cab). Layout (the line breaker's
// measurement and its shaping-merge predicate) and paint both shape with
// this one list, so a run is measured with the features it is drawn with.
//
// Order: font-variant-caps, font-kerning, font-variant-ligatures,
// -east-asian, -numeric, then font-feature-settings. HarfBuzz lets a later
// setting of a tag win, so the low-level property overrides the
// font-variant-* shorthands (CSS Fonts 4 §7.2). The face-dependent features
// — font-variant-alternates and the @font-face descriptor, the face's
// ResolvedFeatures — are not part of it: paint inserts them before this
// list per face (render.Renderer.shapeFeatures).
//
// Returns nil when every input is at its initial value (HarfBuzz defaults).
func GetFontFeatures(s *css.Style) []textshape.FontFeature {
	if s == nil {
		return nil
	}
	var features []textshape.FontFeature

	// CSS Fonts 4 §6.7 font-variant-caps: emit the corresponding OpenType
	// feature tags so a font carrying native small-caps / petite-caps /
	// unicase / titling-caps glyphs activates them. Emission is unconditional
	// on font-synthesis-small-caps — §6.6 makes font-synthesis-small-caps
	// gate only the *synthesized* fallback (paint's drawTextSmallCaps), not
	// the font's native feature. Blink's CapsFeatureSettingsScopedOverlay is
	// constructed from FontDescription::VariantCaps() alone; synthesis is
	// consulted only at the synth-font selection site (OpenTypeCapsSupport::
	// SyntheticSmallCapsAllowed), never at feature emission.
	switch s.GetFontVariantCaps() {
	case "small-caps":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'s', 'm', 'c', 'p'}, Value: 1},
		)
	case "all-small-caps":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'s', 'm', 'c', 'p'}, Value: 1},
			textshape.FontFeature{Tag: [4]byte{'c', '2', 's', 'c'}, Value: 1},
		)
	case "petite-caps":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'p', 'c', 'a', 'p'}, Value: 1},
		)
	case "all-petite-caps":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'p', 'c', 'a', 'p'}, Value: 1},
			textshape.FontFeature{Tag: [4]byte{'c', '2', 'p', 'c'}, Value: 1},
		)
	case "unicase":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'u', 'n', 'i', 'c'}, Value: 1},
		)
	case "titling-caps":
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'t', 'i', 't', 'l'}, Value: 1},
		)
	}

	// CSS Fonts 4 §6.2 font-kerning: only `none` is observable on top of
	// HarfBuzz defaults — `auto` and `normal` both leave the kern feature on.
	// FromFontDescription's kNoneKerning (font_features.cc:40-50) pushes
	// kern=0 for horizontal text or vkrn=0 for vertical-upright text; louis14
	// pushes both (the inapplicable one is a no-op), as it has no
	// orientation in hand here.
	if s.GetFontKerning() == css.FontKerningNone {
		features = append(features,
			textshape.FontFeature{Tag: [4]byte{'k', 'e', 'r', 'n'}, Value: 0},
			textshape.FontFeature{Tag: [4]byte{'v', 'k', 'r', 'n'}, Value: 0},
		)
	}

	if ligs := s.GetFontVariantLigatures(); ligs != "" && ligs != "normal" {
		features = append(features, fontVariantLigatureFeatures(ligs)...)
	}
	if ea := s.GetFontVariantEastAsian(); ea != "" && ea != "normal" {
		features = append(features, fontVariantEastAsianFeatures(ea)...)
	}
	if num := s.GetFontVariantNumeric(); num != "" && num != "normal" {
		features = append(features, fontVariantNumericFeatures(num)...)
	}
	return append(features, FeatureSettings(s)...)
}

// fontVariantLigatureFeatures maps a CSS font-variant-ligatures value (one
// or more space-separated keywords from §6.4) to the OpenType feature tag
// list per CSS Fonts 4 §6.4 (Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f,
// FontDescription::SetVariantLigatures in
// third_party/blink/renderer/platform/fonts/font_description.cc).
//
// Callers handle `normal` (empty/default) themselves; this function assumes
// non-default input. `none` emits the full five-tag off list per spec.
func fontVariantLigatureFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" {
		return []textshape.FontFeature{
			{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 0},
			{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 0},
			{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 0},
			{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 0},
			{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 0},
		}
	}
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "common-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 1},
				textshape.FontFeature{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-common-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 0},
				textshape.FontFeature{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 0},
			)
		case "discretionary-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-discretionary-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 0},
			)
		case "historical-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-historical-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 0},
			)
		case "contextual":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 1},
			)
		case "no-contextual":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 0},
			)
		}
	}
	return features
}

// fontVariantNumericFeatures maps a CSS font-variant-numeric value to the
// OpenType feature tag list per CSS Fonts 4 §6.4. Mirrors Blink's
// FontDescription::SetVariantNumeric in font_description.cc at Chromium
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Callers handle `normal`/empty themselves.
func fontVariantNumericFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "lining-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'n', 'u', 'm'}, Value: 1},
			)
		case "oldstyle-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'o', 'n', 'u', 'm'}, Value: 1},
			)
		case "proportional-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'p', 'n', 'u', 'm'}, Value: 1},
			)
		case "tabular-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'t', 'n', 'u', 'm'}, Value: 1},
			)
		case "diagonal-fractions":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'f', 'r', 'a', 'c'}, Value: 1},
			)
		case "stacked-fractions":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'a', 'f', 'r', 'c'}, Value: 1},
			)
		case "ordinal":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'o', 'r', 'd', 'n'}, Value: 1},
			)
		case "slashed-zero":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'z', 'e', 'r', 'o'}, Value: 1},
			)
		}
	}
	return features
}

// fontVariantEastAsianFeatures maps a CSS font-variant-east-asian value
// (CSS Fonts 4 §6.5) to the OpenType feature tag list. Mirrors Blink's
// FontDescription::SetVariantEastAsian in
// third_party/blink/renderer/platform/fonts/font_description.cc at Chromium
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Callers handle `normal`/empty themselves.
func fontVariantEastAsianFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "jis78":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '7', '8'}, Value: 1},
			)
		case "jis83":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '8', '3'}, Value: 1},
			)
		case "jis90":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '9', '0'}, Value: 1},
			)
		case "jis04":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '0', '4'}, Value: 1},
			)
		case "simplified":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'s', 'm', 'p', 'l'}, Value: 1},
			)
		case "traditional":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'t', 'r', 'a', 'd'}, Value: 1},
			)
		case "full-width":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'f', 'w', 'i', 'd'}, Value: 1},
			)
		case "proportional-width":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'p', 'w', 'i', 'd'}, Value: 1},
			)
		case "ruby":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'r', 'u', 'b', 'y'}, Value: 1},
			)
		}
	}
	return features
}
