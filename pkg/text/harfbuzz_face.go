package text

// harfBuzzFace is the per-font face that owns the font's vertical data.
// Mirrors the part of Blink's HarfBuzzFace (platform/fonts/shaping/
// harfbuzz_face.h @ 5992657f556bcd9d207a4aca187488462f027cab) that
// SimpleFontData reads: VerticalData() (:92) and UnitsPerEmFromHeadTable()
// (:84). The table data is per typeface (typefaceForPath) and the
// size-dependent scale and fallback are applied per call, as Blink shares one
// HarfBuzzFontData per typeface (HarfBuzzFontCache keyed by the font's unique
// id, harfbuzz_face.cc:74-77) whose OpenTypeVerticalData is re-scaled on each
// VerticalData() (harfbuzz_font_data.h:92-104).
type harfBuzzFace struct {
	platformData fontPlatformData
}

// GetHarfBuzzFace returns the face of the font p instantiates. Mirrors
// FontPlatformData::GetHarfBuzzFace (platform/fonts/font_platform_data.cc:185-191 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func (p fontPlatformData) GetHarfBuzzFace() harfBuzzFace {
	return harfBuzzFace{platformData: p}
}

// UnitsPerEmFromHeadTable is the font's unitsPerEm. Mirrors
// HarfBuzzFace::UnitsPerEmFromHeadTable (platform/fonts/shaping/
// harfbuzz_face.cc:457-460 @ 5992657f556bcd9d207a4aca187488462f027cab), which
// returns hb_face_get_upem (see unitsPerEmFromHead).
func (h harfBuzzFace) UnitsPerEmFromHeadTable() int {
	return typefaceForPath(h.platformData.fontPath).upem
}

// VerticalData returns the font's vertical metrics scaled to its size, with
// the fallback height for a font with no vmtx. Mirrors HarfBuzzFace::
// VerticalData (platform/fonts/shaping/harfbuzz_face.cc:690-697), which runs
// HarfBuzzFontData::UpdateFallbackMetricsAndScale(kPrepareForVerticalLayout)
// and returns HarfBuzzFontData::VerticalData() (platform/fonts/shaping/
// harfbuzz_font_data.h:47-83, :92-104), all @
// 5992657f556bcd9d207a4aca187488462f027cab:
//
//   - size_per_unit_ = platform_data.size() / UnitsPerEmFromHeadTable()
//     (:71-77), at the font's exact size;
//   - height_fallback_ = lroundf(ascent) + lroundf(descent) (:69), from
//     FontMetrics::AscentDescentWithHacks called with no ascent/descent
//     override (:64-65) — ascentDescentWithHacks, the FreeType face
//     ascender/descender Skia reports. Not louis14's line metrics
//     (FontAscentFromFont/FontDescentFromFont), whose source differs
//     (Liberation Serif 20px: 18 there, 22 here and in Chrome).
//
// A typeface that did not load (typeface.loaded: its tables do not parse,
// or FreeType would refuse it) has no usable tables, but Blink always has a
// font's FontMetrics there, so the fallback is louis14's FontMetrics height,
// lround(ascent) + lround(descent) of the font as louis14 lays it out, with
// no vertical metrics.
//
// Blink re-scales one shared OpenTypeVerticalData in place; louis14 returns a
// scaled copy whose advance slices are shared read-only, so concurrent
// callers at different sizes cannot race.
func (h harfBuzzFace) VerticalData() openTypeVerticalData {
	p := h.platformData
	t := typefaceForPath(p.fontPath)
	if !t.loaded {
		var v openTypeVerticalData
		v.SetScaleAndFallbackMetrics(0, int(FontAscentFromFont(p.size, p.fontPath, nil)+FontDescentFromFont(p.size, p.fontPath, nil)))
		return v
	}
	v := *t.vertical
	ascent, descent := ascentDescentWithHacks(t, p.size)
	v.SetScaleAndFallbackMetrics(p.size/float64(t.upem), int(ascent+descent))
	return v
}
