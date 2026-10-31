package text

import "encoding/binary"

// openTypeVerticalData holds a typeface's vertical metrics: the vmtx advance
// heights, and the scale and fallback that turn them into pixels at one font
// size. Mirrors Blink's OpenTypeVerticalData (platform/fonts/opentype/
// open_type_vertical_data.h:42-75 @ 5992657f556bcd9d207a4aca187488462f027cab),
// keeping its member and method names: advanceWidths / advanceHeights are
// advance_widths_ / advance_heights_ (:66-67), sizePerUnit / heightFallback
// are size_per_unit_ / height_fallback_ (:72, :74).
//
// Only the advance path is ported. Not ported, because no louis14 code reads
// them: vertical glyph origins — VORG (vert_origin_y_, default_vert_origin_y_),
// top_side_bearings_, vertical_glyph_map_ and GetVerticalTranslationsForGlyphs
// (:57-59) — and ascent_fallback_ (:73), which only
// GetVerticalTranslationsForGlyphs reads (open_type_vertical_data.cc:355 @ the
// same SHA). louis14 paints upright glyphs at y + ascent within their cell.
// Pixel values are float64, louis14's metric type, where Blink uses float.
type openTypeVerticalData struct {
	advanceWidths  []uint16
	advanceHeights []uint16
	sizePerUnit    float64
	heightFallback int
}

// The layout of the tables loadMetrics reads (tags tagHhea, tagHmtx, tagVhea,
// tagVmtx): an hhea / vhea header is a Fixed plus 16 Int16/UInt16 fields,
// sizeof HheaTable / VheaTable, whose last field is the metrics count,
// number_of_h_metrics / num_of_long_ver_metrics; an hmtx / vmtx long metric is
// a UInt16 advance then an Int16 side bearing, HmtxTable / VmtxTable::Entry.
// Mirrors open_type_vertical_data.cc:55-113 @
// 5992657f556bcd9d207a4aca187488462f027cab.
const (
	sizeofMetricsHeaderTable = 36
	metricsHeaderCountOffset = 34
	sizeofLongMetricsEntry   = 4
)

// newOpenTypeVerticalData loads the vertical metrics of the typeface whose
// tables copyOpenTypeTable returns (nil for a missing table). Mirrors the
// OpenTypeVerticalData constructor, which calls LoadMetrics on the typeface
// (open_type_vertical_data.cc:140-142 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func newOpenTypeVerticalData(copyOpenTypeTable func(tag uint32) []byte) *openTypeVerticalData {
	v := &openTypeVerticalData{}
	v.loadMetrics(copyOpenTypeTable)
	return v
}

// validateTable reports whether buffer holds count records of size bytes.
// Mirrors open_type::ValidateTable (platform/fonts/opentype/
// open_type_types.h:69-74 @ 5992657f556bcd9d207a4aca187488462f027cab).
func validateTable(buffer []byte, size, count int) bool {
	return len(buffer) >= size*count
}

// loadMetrics reads hhea and hmtx (without them the font is not OpenType),
// then vhea and vmtx (without them it has no vertical metrics). Every invalid
// or missing table returns early with what loaded so far. Mirrors
// OpenTypeVerticalData::LoadMetrics' advance path (open_type_vertical_data.cc:
// 154-240 @ 5992657f556bcd9d207a4aca187488462f027cab): hhea/hmtx :158-184,
// vhea :186-197, vmtx :220-240.
func (v *openTypeVerticalData) loadMetrics(copyOpenTypeTable func(tag uint32) []byte) {
	advances := func(headerTag, metricsTag uint32) []uint16 {
		header := copyOpenTypeTable(headerTag)
		if !validateTable(header, sizeofMetricsHeaderTable, 1) {
			return nil
		}
		count := int(binary.BigEndian.Uint16(header[metricsHeaderCountOffset:]))
		if count == 0 {
			return nil
		}
		metrics := copyOpenTypeTable(metricsTag)
		if !validateTable(metrics, sizeofLongMetricsEntry, count) {
			return nil
		}
		out := make([]uint16, count)
		for i := range out {
			out[i] = binary.BigEndian.Uint16(metrics[i*sizeofLongMetricsEntry:])
		}
		return out
	}
	if v.advanceWidths = advances(tagHhea, tagHmtx); v.advanceWidths == nil {
		return
	}
	v.advanceHeights = advances(tagVhea, tagVmtx)
}

// SetScaleAndFallbackMetrics sets the font units to pixels scale and the
// advance used when the font has no vertical metrics. Mirrors
// OpenTypeVerticalData::SetScaleAndFallbackMetrics (open_type_vertical_data.cc:
// 281-287 @ 5992657f556bcd9d207a4aca187488462f027cab), without its ascent
// parameter (see the type's doc comment).
func (v *openTypeVerticalData) SetScaleAndFallbackMetrics(sizePerUnit float64, height int) {
	v.sizePerUnit = sizePerUnit
	v.heightFallback = height
}

// IsOpenType reports whether hhea and hmtx loaded. Mirrors
// OpenTypeVerticalData::IsOpenType (open_type_vertical_data.h:53 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func (v openTypeVerticalData) IsOpenType() bool { return len(v.advanceWidths) > 0 }

// HasVerticalMetrics reports whether vhea and vmtx loaded. Mirrors
// OpenTypeVerticalData::HasVerticalMetrics (open_type_vertical_data.h:54 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func (v openTypeVerticalData) HasVerticalMetrics() bool { return len(v.advanceHeights) > 0 }

// AdvanceHeight is g's vertical advance in pixels: its vmtx advance times the
// scale, where a glyph at or past the last long metric reads that metric; with
// no vertical metrics, the height fallback. Mirrors
// OpenTypeVerticalData::AdvanceHeight (open_type_vertical_data.cc:289-300 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func (v openTypeVerticalData) AdvanceHeight(g Glyph) float64 {
	if n := len(v.advanceHeights); n > 0 {
		return float64(v.advanceHeights[min(int(g), n-1)]) * v.sizePerUnit
	}
	// No vertical info in the font file; use height as advance.
	return float64(v.heightFallback)
}
