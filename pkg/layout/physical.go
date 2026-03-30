package layout

// PhysicalSize represents a width and height in physical screen coordinates.
type PhysicalSize struct {
	Width  float64
	Height float64
}

// PhysicalOffset represents an (x, y) position in physical screen coordinates,
// measured from the top-left corner.
type PhysicalOffset struct {
	X float64
	Y float64
}

// PhysicalRect represents a rectangle in physical screen coordinates.
type PhysicalRect struct {
	Offset PhysicalOffset
	Size   PhysicalSize
}

// PhysicalEdges represents four physical edges (top, right, bottom, left).
// Used for margins, borders, and padding in physical coordinates.
type PhysicalEdges struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}
