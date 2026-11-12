package render

// sidewaysEmphasisGeometry is what places a sideways text fragment's
// emphasis mark buffer: the fragment's physical left edge and width, the
// base font's integer ascent and descent, and the mark font's ascent,
// descent and buffer height.
type sidewaysEmphasisGeometry struct {
	boxLeft, boxWidth       float64
	ascent, descent         float64
	markAscent, markDescent float64
	annotH                  int
}

// screenX returns the physical X of the mark buffer's left column.
func (g sidewaysEmphasisGeometry) screenX(over, clockwise bool) float64 {
	return g.boxLeft + g.markAscent + g.markDescent + float64(g.annotH)
}
