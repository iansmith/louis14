package text

import (
	"bytes"
	"encoding/binary"
	"os"
	"sync"

	"github.com/go-text/typesetting/font/opentype"
)

// typeface is one font file's table data, read once: its vertical metrics and
// unitsPerEm. It stands in for the SkTypeface that Blink's
// OpenTypeVerticalData reads tables from (CopyOpenTypeTable,
// platform/fonts/opentype/open_type_vertical_data.cc:144-152 @
// 5992657f556bcd9d207a4aca187488462f027cab) and that HarfBuzzFace reads the
// head table from. No Blink analog for the record itself: mazzy's
// GlyphProvider keeps font bytes private, so louis14 records them itself.
type typeface struct {
	once     sync.Once
	data     []byte // sfnt bytes; nil after load, or read from path at load
	path     string // builtin font file, read at load when data is nil
	vertical *openTypeVerticalData
	upem     int
	// ascender and descender are FreeType's FT_FaceRec ascender/descender
	// in font units (sfntFaceAscenderDescender).
	ascender, descender int
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

// load fills the vertical metrics, unitsPerEm and ascender/descender once,
// then drops the bytes.
func (t *typeface) load() {
	t.once.Do(func() {
		data := t.data
		if data == nil && t.path != "" {
			data, _ = os.ReadFile(t.path)
		}
		ld, err := opentype.NewLoader(bytes.NewReader(data))
		if err != nil {
			ld = nil
		}
		get := func(tag uint32) []byte { return copyOpenTypeTable(ld, tag) }
		t.vertical = newOpenTypeVerticalData(get)
		t.upem = unitsPerEmFromHead(get(tagHead))
		t.ascender, t.descender = sfntFaceAscenderDescender(get(tagHhea), get(tagOS2))
		t.data = nil
	})
}

// unitsPerEmFromHead returns head.unitsPerEm, or 1000 when the head table is
// missing or the value is outside [16, 16384]. Mirrors HarfBuzz's
// OT::head::get_upem (src/hb-ot-head-table.hh, HarfBuzz main; Chromium pins
// HarfBuzz as a DEPS submodule, outside the Chromium SHA), which
// hb_face_get_upem returns to HarfBuzzFace::UnitsPerEmFromHeadTable
// (platform/fonts/shaping/harfbuzz_face.cc:457-460 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func unitsPerEmFromHead(head []byte) int {
	upem := 0
	if len(head) >= headUnitsPerEmOffset+2 {
		upem = int(binary.BigEndian.Uint16(head[headUnitsPerEmOffset:]))
	}
	if upem < 16 || upem > 16384 {
		return 1000
	}
	return upem
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
	// fileTypefaces caches builtin font files by path.
	fileTypefaces = map[string]*typeface{}
)

// registerTypeface records the sfnt bytes a font buffer was registered with
// under (family, variant). A new record replaces the old one wholesale, so a
// re-registered family never serves the previous face's tables. Called next
// to every GlyphProvider RegisterBuffer (registerFontBuffer, and
// registerVendoredFonts for csstest.Register).
func registerTypeface(family string, variant int32, data []byte) {
	typefacesMu.Lock()
	defer typefacesMu.Unlock()
	registeredTypefaces[typefaceKey{family, variant}] = &typeface{data: data}
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
// own registered-before-filesystem order. A path that is neither yields an
// empty typeface: no vertical metrics, so AdvanceHeight's height fallback.
func typefaceForPath(fontPath string) *typeface {
	family, variant := FontPathToFamilyVariant(fontPath)
	typefacesMu.Lock()
	t, ok := registeredTypefaces[typefaceKey{family, variant}]
	if !ok {
		if t, ok = fileTypefaces[fontPath]; !ok {
			t = &typeface{path: fontPath}
			fileTypefaces[fontPath] = t
		}
	}
	typefacesMu.Unlock()
	t.load()
	return t
}
