package text

import (
	"bytes"
	"encoding/binary"
	"os"
	"sync"

	"github.com/go-text/typesetting/font/opentype"
)

// typeface is one font's table data, parsed once: its vertical metrics,
// unitsPerEm and face ascender/descender. It stands in for the SkTypeface that
// Blink's OpenTypeVerticalData reads tables from (CopyOpenTypeTable,
// platform/fonts/opentype/open_type_vertical_data.cc:144-152 @
// 5992657f556bcd9d207a4aca187488462f027cab) and that HarfBuzzFace reads the
// head table from. Like OpenTypeVerticalData, which copies what it reads out
// of the typeface, it keeps only the parsed values, never the font bytes. No
// Blink analog for the record itself: mazzy's GlyphProvider keeps font bytes
// private, so louis14 parses them itself.
type typeface struct {
	once     sync.Once
	path     string // builtin font file, read on first use; "" when registered
	vertical *openTypeVerticalData
	upem     int
	// ascender and descender are FreeType's FT_FaceRec ascender/descender
	// in font units (sfntFaceAscenderDescender).
	ascender, descender int
	// loaded reports whether FreeType would load the face: its sfnt
	// tables parse and head.unitsPerEm is within [16, 16384], the range
	// sfnt_load_face accepts (src/sfnt/sfobjs.c:977-985, FreeType @
	// a5877f48fff8dd6cf8d53c505ce055499376aee2, Chromium's DEPS pin at
	// 5992657f556bcd9d207a4aca187488462f027cab). When false the tables
	// above are unusable and VerticalData falls back to FontMetrics.
	loaded bool
}

// headUnitsPerEmOffset is the offset of the head table's uint16 unitsPerEm.
const headUnitsPerEmOffset = 18

// copyOpenTypeTable returns the table's bytes, nil when the font has no such
// table or does not parse — as Blink's CopyOpenTypeTable resizes its buffer
// to 0 (open_type_vertical_data.cc:144-152 @
// 5992657f556bcd9d207a4aca187488462f027cab). Table extraction is go-text's
// opentype.Loader.
func copyOpenTypeTable(ld *opentype.Loader, tag uint32) []byte {
	if ld == nil {
		return nil
	}
	b, err := ld.RawTable(opentype.Tag(tag))
	if err != nil {
		return nil
	}
	return b
}

// newRegisteredTypeface parses a registered font buffer at once, so the
// record holds no reference to the buffer.
func newRegisteredTypeface(data []byte) *typeface {
	t := &typeface{}
	t.once.Do(func() { t.parse(data) })
	return t
}

// load parses a builtin font file's tables on first use.
func (t *typeface) load() {
	t.once.Do(func() {
		data, _ := os.ReadFile(t.path)
		t.parse(data)
	})
}

// parse reads the vertical metrics, unitsPerEm and ascender/descender from
// font file bytes. Web font containers are decoded first and a collection
// reads its first face, so the tables are those of the typeface Skia
// instantiates: Blink decodes WOFF/WOFF2 before making the SkTypeface
// (DecodeFontWithIft, platform/fonts/web_font_decoder.cc:189 @
// 5992657f556bcd9d207a4aca187488462f027cab) and SkFontMgr opens a
// collection at ttc index 0 unless told otherwise.
func (t *typeface) parse(data []byte) {
	if sfnt, err := decodeWebFont(data); err == nil {
		data = sfnt
	}
	var ld *opentype.Loader
	if lds, err := opentype.NewLoaders(bytes.NewReader(data)); err == nil && len(lds) > 0 {
		ld = lds[0]
	}
	get := func(tag uint32) []byte { return copyOpenTypeTable(ld, tag) }
	head := get(tagHead)
	t.vertical = newOpenTypeVerticalData(get)
	t.upem = unitsPerEmFromHead(head)
	t.loaded = isValidUnitsPerEm(rawUnitsPerEm(head))
	t.ascender, t.descender = sfntFaceAscenderDescender(get(tagHhea), get(tagOS2))
}

// The unitsPerEm range both FreeType (sfnt_load_face, which refuses a face
// outside it) and HarfBuzz (OT::head::get_upem, which reports 1000 instead)
// accept.
const (
	minUnitsPerEm = 16
	maxUnitsPerEm = 16384
)

// rawUnitsPerEm returns head.unitsPerEm, 0 when the head table is missing
// or short.
func rawUnitsPerEm(head []byte) int {
	if len(head) < headUnitsPerEmOffset+2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(head[headUnitsPerEmOffset:]))
}

func isValidUnitsPerEm(upem int) bool { return upem >= minUnitsPerEm && upem <= maxUnitsPerEm }

// unitsPerEmFromHead returns head.unitsPerEm, or 1000 when the head table is
// missing or the value is outside [16, 16384]. Mirrors HarfBuzz's
// OT::head::get_upem (src/hb-ot-head-table.hh, HarfBuzz main; Chromium pins
// HarfBuzz as a DEPS submodule, outside the Chromium SHA), which
// hb_face_get_upem returns to HarfBuzzFace::UnitsPerEmFromHeadTable
// (platform/fonts/shaping/harfbuzz_face.cc:457-460 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func unitsPerEmFromHead(head []byte) int {
	if upem := rawUnitsPerEm(head); isValidUnitsPerEm(upem) {
		return upem
	}
	return 1000
}

// typefaceKey is the (family, variant) a font buffer is registered under, the
// GlyphProvider's own registration key.
type typefaceKey struct {
	family  string
	variant int32
}

var (
	typefacesMu sync.Mutex
	// registeredTypefaces mirrors the GlyphProvider's registered buffers:
	// last registration wins, as in DirectGlyphProvider.RegisterBuffer.
	registeredTypefaces = map[typefaceKey]*typeface{}
	// fileTypefaces caches builtin font files by path. It outlives
	// typefacesByPath's clears, so a file is never parsed twice.
	fileTypefaces = map[string]*typeface{}
	// typefacesByPath caches typefaceForPath's answer per font path, so the
	// hot path takes no lock; written under typefacesMu, and cleared by
	// every registration.
	typefacesByPath sync.Map
)

// registerTypeface records the typeface of a font buffer registered under
// (family, variant), parsed now. A new record replaces the old one
// wholesale, so a re-registered family never serves the previous face's
// tables, and the path lookup cache is dropped with it. Called next to every
// GlyphProvider RegisterBuffer (registerFontBuffer, and registerVendoredFonts
// for csstest.Register).
func registerTypeface(family string, variant int32, data []byte) {
	t := newRegisteredTypeface(data)
	typefacesMu.Lock()
	defer typefacesMu.Unlock()
	registeredTypefaces[typefaceKey{family, variant}] = t
	typefacesByPath.Clear()
}

// registerFontBuffer registers a font buffer with r (a GlyphProvider or
// TextLayout) and records its bytes for typefaceForPath.
func registerFontBuffer(r interface {
	RegisterBuffer(family string, variant int32, data []byte) error
}, family string, variant int32, data []byte) error {
	if err := r.RegisterBuffer(family, variant, data); err != nil {
		return err
	}
	registerTypeface(family, variant, data)
	return nil
}

// typefaceForPath returns the loaded typeface openFont resolves fontPath to:
// a registered buffer first, then the font file at fontPath — the provider's
// own registered-before-filesystem order. A path that is neither yields a
// typeface that did not load (VerticalData then uses FontMetrics).
func typefaceForPath(fontPath string) *typeface {
	if t, ok := typefacesByPath.Load(fontPath); ok {
		t := t.(*typeface)
		t.load()
		return t
	}
	family, variant := FontPathToFamilyVariant(fontPath)
	typefacesMu.Lock()
	t, ok := registeredTypefaces[typefaceKey{family, variant}]
	if !ok {
		if t, ok = fileTypefaces[fontPath]; !ok {
			t = &typeface{path: fontPath}
			fileTypefaces[fontPath] = t
		}
	}
	typefacesByPath.Store(fontPath, t)
	typefacesMu.Unlock()
	t.load()
	return t
}
