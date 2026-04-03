package layout

// LogicalSize represents dimensions in the element's writing mode.
// InlineSize is the extent in the inline direction (width in HTB, height in vertical).
// BlockSize is the extent in the block direction (height in HTB, width in vertical).
type LogicalSize struct {
	InlineSize float64
	BlockSize  float64
}

// LogicalOffset represents a position in the element's writing mode.
// InlineOffset is the distance from inline-start.
// BlockOffset is the distance from block-start.
type LogicalOffset struct {
	InlineOffset float64
	BlockOffset  float64
}

// LogicalEdges represents four logical edges.
// Used for margins, borders, and padding in logical coordinates.
type LogicalEdges struct {
	InlineStart float64
	InlineEnd   float64
	BlockStart  float64
	BlockEnd    float64
}

// InlineSum returns the total inline-direction extent (start + end).
func (e LogicalEdges) InlineSum() float64 {
	return e.InlineStart + e.InlineEnd
}

// BlockSum returns the total block-direction extent (start + end).
func (e LogicalEdges) BlockSum() float64 {
	return e.BlockStart + e.BlockEnd
}
