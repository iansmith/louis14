package text

import (
	"encoding/binary"
	"testing"
)

// TestOpenTypeVerticalData_ValidateTableTest (LOU-424): port of Blink's
// OpenTypeVerticalDataTest.ValidateTableTest (platform/fonts/opentype/
// open_type_vertical_data_test.cc:36-48 @
// 5992657f556bcd9d207a4aca187488462f027cab). Blink's TestTable is a Fixed
// plus an Int16, 6 bytes: a buffer of exactly that size validates, one byte
// short does not, one byte long does. The count rows exercise the count
// argument of open_type::ValidateTable (open_type_types.h:69-74 @ the same
// SHA), which LoadMetrics passes for hmtx and vmtx.
func TestOpenTypeVerticalData_ValidateTableTest(t *testing.T) {
	const sizeofTestTable = 6
	for _, tc := range []struct {
		len, count int
		want       bool
	}{
		{sizeofTestTable, 1, true},
		{sizeofTestTable - 1, 1, false},
		{sizeofTestTable + 1, 1, true},
		{2 * sizeofTestTable, 2, true},
		{2*sizeofTestTable - 1, 2, false},
	} {
		if got := validateTable(make([]byte, tc.len), sizeofTestTable, tc.count); got != tc.want {
			t.Errorf("validateTable(len %d, size %d, count %d) = %v, want %v", tc.len, sizeofTestTable, tc.count, got, tc.want)
		}
	}
}

// metricsHeaderTable returns a 36-byte hhea or vhea table whose last field
// (numberOfHMetrics / numOfLongVerMetrics, offset 34) is n. The layout is
// Blink's HheaTable / VheaTable (open_type_vertical_data.cc:55-89 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func metricsHeaderTable(n uint16) []byte {
	b := make([]byte, 36)
	binary.BigEndian.PutUint16(b[34:], n)
	return b
}

// longMetricsTable returns an hmtx or vmtx table of (advance, side bearing)
// entries, Blink's HmtxTable / VmtxTable::Entry (open_type_vertical_data.cc:
// 91-113 @ 5992657f556bcd9d207a4aca187488462f027cab), side bearings 0.
func longMetricsTable(advances ...uint16) []byte {
	b := make([]byte, 4*len(advances))
	for i, a := range advances {
		binary.BigEndian.PutUint16(b[4*i:], a)
	}
	return b
}

// tableGetter is a CopyOpenTypeTable over an in-memory table map; a missing
// tag returns nil, as CopyOpenTypeTable resizes the buffer to 0.
func tableGetter(tables map[uint32][]byte) func(uint32) []byte {
	return func(tag uint32) []byte { return tables[tag] }
}

// TestOpenTypeVerticalData_AdvanceHeight (LOU-424): with vertical metrics,
// AdvanceHeight is the glyph's vmtx advance (an unsigned 16-bit value) times
// size_per_unit_, and a glyph at or past numOfLongVerMetrics reads the last
// long metric. Mirrors OpenTypeVerticalData::AdvanceHeight
// (platform/fonts/opentype/open_type_vertical_data.cc:289-300 @
// 5992657f556bcd9d207a4aca187488462f027cab). Expected values are hand
// arithmetic at 80px over 1024 units per em.
func TestOpenTypeVerticalData_AdvanceHeight(t *testing.T) {
	v := newOpenTypeVerticalData(tableGetter(map[uint32][]byte{
		tagHhea: metricsHeaderTable(2),
		tagHmtx: longMetricsTable(500, 600),
		tagVhea: metricsHeaderTable(4),
		tagVmtx: longMetricsTable(1024, 0, 256, 40000),
	}))
	v.SetScaleAndFallbackMetrics(80.0/1024, 99)
	if !v.IsOpenType() || !v.HasVerticalMetrics() {
		t.Fatalf("IsOpenType, HasVerticalMetrics = %v, %v; want true, true", v.IsOpenType(), v.HasVerticalMetrics())
	}
	for _, tc := range []struct {
		g    Glyph
		want float64
	}{
		{0, 80},
		{1, 0}, // a real 0 advance, not the height fallback
		{2, 20},
		{3, 3125}, // 40000 read unsigned
		{9, 3125}, // past numOfLongVerMetrics: the last long metric
	} {
		if got := v.AdvanceHeight(tc.g); got != tc.want {
			t.Errorf("AdvanceHeight(%d) = %v, want %v", tc.g, got, tc.want)
		}
	}
}

// TestOpenTypeVerticalData_LoadMetricsGates (LOU-424): every early return of
// OpenTypeVerticalData::LoadMetrics (platform/fonts/opentype/
// open_type_vertical_data.cc:154-240 @
// 5992657f556bcd9d207a4aca187488462f027cab) leaves advance_heights_ empty, so
// HasVerticalMetrics is false and AdvanceHeight returns height_fallback_
// unscaled (:298-299). IsOpenType is true once hhea and hmtx loaded (:158-184).
func TestOpenTypeVerticalData_LoadMetricsGates(t *testing.T) {
	full := func() map[uint32][]byte {
		return map[uint32][]byte{
			tagHhea: metricsHeaderTable(2),
			tagHmtx: longMetricsTable(500, 600),
			tagVhea: metricsHeaderTable(2),
			tagVmtx: longMetricsTable(1000, 1000),
		}
	}
	for _, tc := range []struct {
		name     string
		edit     func(map[uint32][]byte)
		openType bool
	}{
		{"no hhea", func(m map[uint32][]byte) { delete(m, tagHhea) }, false},
		{"numberOfHMetrics 0", func(m map[uint32][]byte) { m[tagHhea] = metricsHeaderTable(0) }, false},
		{"hmtx shorter than numberOfHMetrics", func(m map[uint32][]byte) { m[tagHmtx] = longMetricsTable(500) }, false},
		{"no vhea", func(m map[uint32][]byte) { delete(m, tagVhea) }, true},
		{"short vhea", func(m map[uint32][]byte) { m[tagVhea] = m[tagVhea][:35] }, true},
		{"numOfLongVerMetrics 0", func(m map[uint32][]byte) { m[tagVhea] = metricsHeaderTable(0) }, true},
		{"vmtx shorter than numOfLongVerMetrics", func(m map[uint32][]byte) { m[tagVmtx] = longMetricsTable(1000) }, true},
		{"vhea without vmtx", func(m map[uint32][]byte) { delete(m, tagVmtx) }, true},
	} {
		tables := full()
		tc.edit(tables)
		v := newOpenTypeVerticalData(tableGetter(tables))
		v.SetScaleAndFallbackMetrics(1, 99)
		if v.HasVerticalMetrics() {
			t.Errorf("%s: HasVerticalMetrics = true, want false", tc.name)
		}
		if got := v.AdvanceHeight(0); got != 99 {
			t.Errorf("%s: AdvanceHeight(0) = %v, want the height fallback 99", tc.name, got)
		}
		if v.IsOpenType() != tc.openType {
			t.Errorf("%s: IsOpenType = %v, want %v", tc.name, v.IsOpenType(), tc.openType)
		}
	}
}

// TestOpenTypeVerticalData_HasVerticalMetricsFonts (LOU-424): the vertical
// data a font's HarfBuzzFace builds from the font file. ChTestShortZero.woff
// (WPT css-values resource) has vhea/vmtx: upem 1024, its "0" glyph's vmtx
// advance 256, so 20px at 80px. The builtin Liberation Serif (upem 2048),
// Ahem.ttf (upem 1000) and the other css-values fonts have no vhea/vmtx.
// Table facts from an independent sfnt table read of each file, not from
// louis14 output.
func TestOpenTypeVerticalData_HasVerticalMetricsFonts(t *testing.T) {
	fr := NewFontRegistry()
	shortZero := registerTestFace(t, fr, "LOU424-HVM-ShortZero", "ChTestShortZero.woff", fetchCSSValuesResource)
	exTest := registerTestFace(t, fr, "LOU424-HVM-ExTest", "ExTest.woff", fetchCSSValuesResource)
	icFull := registerTestFace(t, fr, "LOU424-HVM-IcFull", "IcTestFullWidth.woff2", fetchCSSValuesResource)
	zeroW := registerTestFace(t, fr, "LOU424-HVM-ZeroW", "ChTestZeroWidthZero.woff", fetchCSSValuesResource)
	serif := DefaultFontConfig().resolveBuiltinFamily("serif", false, false)

	t.Run("ChTestShortZero", func(t *testing.T) {
		face := fontPlatformData{fontPath: shortZero, size: 80}.GetHarfBuzzFace()
		if got := face.UnitsPerEmFromHeadTable(); got != 1024 {
			t.Errorf("UnitsPerEmFromHeadTable = %d, want 1024", got)
		}
		vd := face.VerticalData()
		if !vd.HasVerticalMetrics() {
			t.Fatal("HasVerticalMetrics = false, want true")
		}
		zero := glyphForCharacter(shortZero, 80, kDigitZero)
		if zero == 0 {
			t.Fatal("fixture: ChTestShortZero has no '0' glyph")
		}
		if got := vd.AdvanceHeight(zero); got != 20 {
			t.Errorf("AdvanceHeight('0') at 80px = %v, want 20 (256/1024 em)", got)
		}
	})

	for _, tc := range []struct {
		name, path string
		upem       int
	}{
		{"LiberationSerif", serif, 2048},
		{"Ahem.ttf", "../../fonts/Ahem.ttf", 1000},
		{"ExTest", exTest, 0},
		{"IcTestFullWidth", icFull, 0},
		{"ChTestZeroWidthZero", zeroW, 0},
	} {
		face := fontPlatformData{fontPath: tc.path, size: 20}.GetHarfBuzzFace()
		if face.VerticalData().HasVerticalMetrics() {
			t.Errorf("%s: HasVerticalMetrics = true, want false", tc.name)
		}
		if tc.upem != 0 {
			if got := face.UnitsPerEmFromHeadTable(); got != tc.upem {
				t.Errorf("%s: UnitsPerEmFromHeadTable = %d, want %d", tc.name, got, tc.upem)
			}
		}
	}
}
