package text

// RED stub (LOU-424): compile scaffolding for the OpenTypeVerticalData port;
// the implementation commit fills it in.

type openTypeVerticalData struct {
	advanceWidths  []uint16
	advanceHeights []uint16
	sizePerUnit    float64
	heightFallback int
}

func newOpenTypeVerticalData(copyOpenTypeTable func(tag string) []byte) *openTypeVerticalData {
	return &openTypeVerticalData{}
}

func validateTable(buffer []byte, size, count int) bool { return false }

func (v *openTypeVerticalData) SetScaleAndFallbackMetrics(sizePerUnit float64, height int) {}

func (v openTypeVerticalData) IsOpenType() bool { return false }

func (v openTypeVerticalData) HasVerticalMetrics() bool { return false }

func (v openTypeVerticalData) AdvanceHeight(g Glyph) float64 { return 0 }
