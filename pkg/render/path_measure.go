package render

import "math"

// pathMeasure is a pathBuilder that measures what is built on it, for
// Blink's Path::length and Path::IsClosed (platform/geometry/path.cc:
// 233-242, :309-311 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the
// summed length of every contour (SkPathMeasure, closed contours including
// their closing segment) and whether the last contour is closed. Cubics are
// measured as fine polylines; Skia's SkContourMeasure subdivides them by
// tolerance, which differs below the int truncation its callers apply.
type pathMeasure struct {
	length         float64
	closed         bool
	x, y           float64 // current point
	startX, startY float64 // current contour's start
}

func (m *pathMeasure) MoveTo(x, y float64) {
	m.x, m.y, m.startX, m.startY = x, y, x, y
	m.closed = false
}

func (m *pathMeasure) LineTo(x, y float64) {
	m.length += math.Hypot(x-m.x, y-m.y)
	m.x, m.y = x, y
}

func (m *pathMeasure) CubicTo(x1, y1, x2, y2, x3, y3 float64) {
	const steps = 64
	x0, y0 := m.x, m.y
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		mt := 1 - t
		a, b, c, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
		m.LineTo(a*x0+b*x1+c*x2+d*x3, a*y0+b*y1+c*y2+d*y3)
	}
}

func (m *pathMeasure) ClosePath() {
	m.LineTo(m.startX, m.startY)
	m.closed = true
}
