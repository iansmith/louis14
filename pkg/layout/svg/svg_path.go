package svg

import (
	"math"
	"strconv"

	"louis14/pkg/geometry"
)

// PathSegmentKind enumerates the resolved path-segment types we emit
// after parsing SVG path data and converting all shorthand commands
// (H/V/T/S/A) into their canonical Line/Cubic/Quad equivalents.
// Mirrors Blink's path-data normalization in core/svg/svg_path_*.cc:
// after parsing through SVGPathStringSource, downstream consumers see
// only absolute MoveTo/LineTo/CurveToCubic/CurveToQuadratic/Close.
type PathSegmentKind int

const (
	// SegMove is "M x y" — start a new sub-path at (x, y). Per SVG 1.1
	// §8.3.2, the current point becomes (x, y).
	SegMove PathSegmentKind = iota
	// SegLine is "L x y" — draw a line from the current point to (x, y).
	// Includes the normalized H/V/Z-implied first-MoveTo conversions.
	SegLine
	// SegQuad is "Q x1 y1 x y" — quadratic Bezier from current to (x, y)
	// with control point (x1, y1). Used for the explicit Q form and the
	// T-shorthand reflection.
	SegQuad
	// SegCubic is "C x1 y1 x2 y2 x y" — cubic Bezier. Used for the
	// explicit C, the S-shorthand reflection, and the arc-to-cubics
	// approximation produced for A commands.
	SegCubic
	// SegClose is "Z" — close the current sub-path back to its starting
	// MoveTo point. Per SVG 1.1 §8.3.3, after Z the current point is the
	// initial point of the most recent MoveTo.
	SegClose
)

// PathSegment is a single resolved segment in a Path. All coordinates
// are in absolute user-space (already normalized from relative
// commands). The fields used per kind:
//
//	SegMove, SegLine — End
//	SegQuad — C1, End
//	SegCubic — C1, C2, End
//	SegClose — none (End repeats current sub-path's start, set by parser
//	            for downstream painters but unused at fill/stroke time)
type PathSegment struct {
	Kind PathSegmentKind
	C1   geometry.PointF
	C2   geometry.PointF
	End  geometry.PointF
}

// Path is a resolved SVG path: a flat list of normalized segments
// (M/L/Q/C/Z) with all coordinates in absolute user-space. Mirrors
// the role Blink's gfx::Path / SVGPathByteStream plays after
// SVGPathStringSource normalization — i.e. the geometry consumers
// downstream (fill/stroke painter, hit-test, bounding-box) see only
// these five primitives. The original H/V/T/S/A commands have all
// been resolved here.
type Path struct {
	Segments []PathSegment

	// startX, startY is the (x, y) of the most recent SegMove —
	// used to set the current point after a SegClose. Tracked at
	// build time, not stored on each segment, since SVG Z does not
	// carry its own coordinates.
	startX, startY float64
	curX, curY     float64

	// lastControlEnd is the last cubic control point (C2) or
	// quadratic control point (C1) that the S/T shorthand reflects
	// across the current point. Tracked separately by kind so the
	// SVG 1.1 §8.3.6 reflection rule fires only when the preceding
	// command was the matching type.
	lastCubicC2 geometry.PointF
	lastQuadC1  geometry.PointF
	hadCubic    bool
	hadQuad     bool
}

// Empty reports whether the path has no segments.
func (p *Path) Empty() bool { return len(p.Segments) == 0 }

// MoveTo appends an absolute MoveTo. Mirrors Blink's
// SVGPathParser handling of M/m: starts a new sub-path at (x, y),
// sets the current point, and clears the trailing control-point
// reflection state.
func (p *Path) MoveTo(x, y float64) {
	p.Segments = append(p.Segments, PathSegment{
		Kind: SegMove,
		End:  geometry.PointF{X: x, Y: y},
	})
	p.startX, p.startY = x, y
	p.curX, p.curY = x, y
	p.hadCubic, p.hadQuad = false, false
}

// LineTo appends an absolute LineTo.
func (p *Path) LineTo(x, y float64) {
	p.Segments = append(p.Segments, PathSegment{
		Kind: SegLine,
		End:  geometry.PointF{X: x, Y: y},
	})
	p.curX, p.curY = x, y
	p.hadCubic, p.hadQuad = false, false
}

// QuadTo appends an absolute quadratic Bezier curve.
func (p *Path) QuadTo(x1, y1, x, y float64) {
	p.Segments = append(p.Segments, PathSegment{
		Kind: SegQuad,
		C1:   geometry.PointF{X: x1, Y: y1},
		End:  geometry.PointF{X: x, Y: y},
	})
	p.lastQuadC1 = geometry.PointF{X: x1, Y: y1}
	p.hadQuad = true
	p.hadCubic = false
	p.curX, p.curY = x, y
}

// CubicTo appends an absolute cubic Bezier curve.
func (p *Path) CubicTo(x1, y1, x2, y2, x, y float64) {
	p.Segments = append(p.Segments, PathSegment{
		Kind: SegCubic,
		C1:   geometry.PointF{X: x1, Y: y1},
		C2:   geometry.PointF{X: x2, Y: y2},
		End:  geometry.PointF{X: x, Y: y},
	})
	p.lastCubicC2 = geometry.PointF{X: x2, Y: y2}
	p.hadCubic = true
	p.hadQuad = false
	p.curX, p.curY = x, y
}

// Close appends a SegClose. Per SVG 1.1 §8.3.3, after Z the current
// point becomes the initial point of the most recent MoveTo.
func (p *Path) Close() {
	p.Segments = append(p.Segments, PathSegment{Kind: SegClose})
	p.curX, p.curY = p.startX, p.startY
	p.hadCubic, p.hadQuad = false, false
}

// BoundingBox returns the axis-aligned bounding box of the path's
// control points and endpoints. This is the geometric (fill)
// bounding box minus stroke contribution — mirrors Blink's
// gfx::Path::ComputeBoundingBox / SVGShape::fill_bounding_box_.
//
// The bounding box of Bezier curves is approximated by including
// the control points; this is a conservative box (larger than tight
// for cubics whose control polygon overshoots the curve), which is
// the same trade-off Blink makes for the cheap path. Tight curve
// bounds would require subdivision; not required for Phase 2.
func (p *Path) BoundingBox() geometry.RectF {
	if len(p.Segments) == 0 {
		return geometry.RectF{}
	}
	var minX, minY, maxX, maxY float64
	first := true
	include := func(pt geometry.PointF) {
		if first {
			minX, maxX = pt.X, pt.X
			minY, maxY = pt.Y, pt.Y
			first = false
			return
		}
		if pt.X < minX {
			minX = pt.X
		}
		if pt.X > maxX {
			maxX = pt.X
		}
		if pt.Y < minY {
			minY = pt.Y
		}
		if pt.Y > maxY {
			maxY = pt.Y
		}
	}
	for _, s := range p.Segments {
		switch s.Kind {
		case SegMove, SegLine:
			include(s.End)
		case SegQuad:
			include(s.C1)
			include(s.End)
		case SegCubic:
			include(s.C1)
			include(s.C2)
			include(s.End)
		case SegClose:
			// No new point.
		}
	}
	if first {
		return geometry.RectF{}
	}
	return geometry.NewRectF(minX, minY, maxX-minX, maxY-minY)
}

// ParsePathData parses an SVG `d` attribute into a normalized Path.
// Mirrors Blink's core/svg/svg_path_string_source.cc + svg_path_parser.cc:
// a single forward pass with a tiny tokenizer, normalizing every command
// (M m L l H h V v C c S s Q q T t A a Z z) to absolute Move/Line/Quad/
// Cubic/Close segments. The arc command A/a is converted to a sequence of
// cubic Bezier approximations using the endpoint-to-center conversion
// from SVG 1.1 Appendix F.6.
//
// Unparseable input stops the parse at the first failure and returns
// whatever was parsed so far (matching the spec's "stop at first
// non-conforming character" rule — SVG 1.1 §8.3 / SVG 2 §9.3).
func ParsePathData(d string) Path {
	p := Path{}
	s := newPathScanner(d)

	// SVG 1.1 §8.3.2: the first command must be M or m. If the first
	// command is lowercase m, it is treated as absolute M.
	command := byte(0)
	for s.skipWSAndCommas(); !s.atEnd(); s.skipWSAndCommas() {
		if c, ok := s.peekCommand(); ok {
			command = c
			s.advance()
		} else if command == 0 {
			// No leading command — bail.
			return p
		}

		switch command {
		case 'M', 'm':
			x, ok1 := s.readNumber()
			y, ok2 := s.readNumber()
			if !ok1 || !ok2 {
				return p
			}
			if command == 'm' && !p.Empty() {
				x += p.curX
				y += p.curY
			}
			p.MoveTo(x, y)
			// SVG 1.1 §8.3.2: subsequent coordinate pairs after an M/m
			// are treated as implicit L/l.
			if command == 'M' {
				command = 'L'
			} else {
				command = 'l'
			}

		case 'L', 'l':
			x, ok1 := s.readNumber()
			y, ok2 := s.readNumber()
			if !ok1 || !ok2 {
				return p
			}
			if command == 'l' {
				x += p.curX
				y += p.curY
			}
			p.LineTo(x, y)

		case 'H', 'h':
			x, ok := s.readNumber()
			if !ok {
				return p
			}
			if command == 'h' {
				x += p.curX
			}
			p.LineTo(x, p.curY)

		case 'V', 'v':
			y, ok := s.readNumber()
			if !ok {
				return p
			}
			if command == 'v' {
				y += p.curY
			}
			p.LineTo(p.curX, y)

		case 'C', 'c':
			x1, ok1 := s.readNumber()
			y1, ok2 := s.readNumber()
			x2, ok3 := s.readNumber()
			y2, ok4 := s.readNumber()
			x, ok5 := s.readNumber()
			y, ok6 := s.readNumber()
			if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
				return p
			}
			if command == 'c' {
				cx, cy := p.curX, p.curY
				x1 += cx
				y1 += cy
				x2 += cx
				y2 += cy
				x += cx
				y += cy
			}
			p.CubicTo(x1, y1, x2, y2, x, y)

		case 'S', 's':
			// Smooth cubic: x1 is the reflection of the previous C2
			// across the current point (if the previous command was
			// C/c/S/s — else x1 = current point).
			x1, y1 := p.curX, p.curY
			if p.hadCubic {
				x1 = 2*p.curX - p.lastCubicC2.X
				y1 = 2*p.curY - p.lastCubicC2.Y
			}
			x2, ok1 := s.readNumber()
			y2, ok2 := s.readNumber()
			x, ok3 := s.readNumber()
			y, ok4 := s.readNumber()
			if !(ok1 && ok2 && ok3 && ok4) {
				return p
			}
			if command == 's' {
				cx, cy := p.curX, p.curY
				x2 += cx
				y2 += cy
				x += cx
				y += cy
			}
			p.CubicTo(x1, y1, x2, y2, x, y)

		case 'Q', 'q':
			x1, ok1 := s.readNumber()
			y1, ok2 := s.readNumber()
			x, ok3 := s.readNumber()
			y, ok4 := s.readNumber()
			if !(ok1 && ok2 && ok3 && ok4) {
				return p
			}
			if command == 'q' {
				cx, cy := p.curX, p.curY
				x1 += cx
				y1 += cy
				x += cx
				y += cy
			}
			p.QuadTo(x1, y1, x, y)

		case 'T', 't':
			// Smooth quadratic: x1 is the reflection of the previous
			// Q control point across the current point.
			x1, y1 := p.curX, p.curY
			if p.hadQuad {
				x1 = 2*p.curX - p.lastQuadC1.X
				y1 = 2*p.curY - p.lastQuadC1.Y
			}
			x, ok1 := s.readNumber()
			y, ok2 := s.readNumber()
			if !(ok1 && ok2) {
				return p
			}
			if command == 't' {
				x += p.curX
				y += p.curY
			}
			p.QuadTo(x1, y1, x, y)

		case 'A', 'a':
			rx, ok1 := s.readNumber()
			ry, ok2 := s.readNumber()
			xAxisRotDeg, ok3 := s.readNumber()
			largeArcF, ok4 := s.readFlag()
			sweepF, ok5 := s.readFlag()
			x, ok6 := s.readNumber()
			y, ok7 := s.readNumber()
			if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) {
				return p
			}
			if command == 'a' {
				x += p.curX
				y += p.curY
			}
			x0, y0 := p.curX, p.curY
			appendArcAsCubics(&p, x0, y0, x, y, rx, ry, xAxisRotDeg, largeArcF, sweepF)

		case 'Z', 'z':
			p.Close()
		}
	}
	return p
}

// appendArcAsCubics implements the SVG endpoint→center arc conversion
// (SVG 1.1 Appendix F.6) and emits a sequence of cubic Bezier
// approximations covering the arc. Each cubic spans ≤ π/2 radians, so
// the conventional cubic-to-circular-arc constant
// k = (4/3) · tan(Δθ/4) is accurate to ~0.001 pixel per unit radius —
// well below the 0% diff target for the tests this plan covers.
//
// Mirrors Blink's BuildArcFromEndpoints / SVGPathParser::ParseArcToSegment
// (core/svg/svg_path_parser.cc, blink::ui_blink/cpp/cpp_path_*).
func appendArcAsCubics(p *Path, x1, y1, x2, y2, rx, ry, xAxisRotDeg float64, largeArcFlag, sweepFlag bool) {
	// F.6.6: If the endpoints (x1, y1) and (x2, y2) are identical, then
	// this is equivalent to omitting the elliptical arc segment entirely.
	if x1 == x2 && y1 == y2 {
		return
	}
	// F.6.6: If rx = 0 or ry = 0 then this arc is treated as a
	// straight line segment (a "lineto") joining the endpoints.
	rx = math.Abs(rx)
	ry = math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.LineTo(x2, y2)
		return
	}

	phi := xAxisRotDeg * math.Pi / 180.0
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)

	// F.6.5 Step 1: compute (x1', y1').
	dx := (x1 - x2) / 2.0
	dy := (y1 - y2) / 2.0
	x1p := cosPhi*dx + sinPhi*dy
	y1p := -sinPhi*dx + cosPhi*dy

	// F.6.6: ensure radii are large enough to span (x1, y1)→(x2, y2).
	rxSq := rx * rx
	rySq := ry * ry
	x1pSq := x1p * x1p
	y1pSq := y1p * y1p
	radiiCheck := x1pSq/rxSq + y1pSq/rySq
	if radiiCheck > 1 {
		s := math.Sqrt(radiiCheck)
		rx *= s
		ry *= s
		rxSq = rx * rx
		rySq = ry * ry
	}

	// F.6.5 Step 2: compute (cx', cy').
	denom := rxSq*y1pSq + rySq*x1pSq
	var factor float64
	if denom > 0 {
		factor = (rxSq*rySq - denom) / denom
	}
	if factor < 0 {
		factor = 0
	}
	sq := math.Sqrt(factor)
	if largeArcFlag == sweepFlag {
		sq = -sq
	}
	cxp := sq * rx * y1p / ry
	cyp := -sq * ry * x1p / rx

	// F.6.5 Step 3: compute (cx, cy).
	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2.0
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2.0

	// F.6.5 Step 4: compute theta1 and deltaTheta.
	angle := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		lenU := math.Hypot(ux, uy)
		lenV := math.Hypot(vx, vy)
		if lenU == 0 || lenV == 0 {
			return 0
		}
		c := dot / (lenU * lenV)
		if c < -1 {
			c = -1
		} else if c > 1 {
			c = 1
		}
		a := math.Acos(c)
		if ux*vy-uy*vx < 0 {
			a = -a
		}
		return a
	}
	theta1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	deltaTheta := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweepFlag && deltaTheta > 0 {
		deltaTheta -= 2 * math.Pi
	} else if sweepFlag && deltaTheta < 0 {
		deltaTheta += 2 * math.Pi
	}

	// Split deltaTheta into segments of at most π/2 and approximate each
	// with a cubic Bezier using the k = (4/3)·tan(Δθ/4) constant.
	const maxSegAngle = math.Pi / 2.0
	n := int(math.Ceil(math.Abs(deltaTheta) / maxSegAngle))
	if n == 0 {
		n = 1
	}
	segAngle := deltaTheta / float64(n)
	t := (4.0 / 3.0) * math.Tan(segAngle/4.0)

	// curPoint tracks position along the arc parameterized by theta;
	// initial position is the user-space x1, y1.
	curX, curY := x1, y1
	for i := 0; i < n; i++ {
		thetaA := theta1 + float64(i)*segAngle
		thetaB := thetaA + segAngle
		sinA, cosA := math.Sin(thetaA), math.Cos(thetaA)
		sinB, cosB := math.Sin(thetaB), math.Cos(thetaB)

		// Endpoint at thetaB in user space:
		ex := cosPhi*rx*cosB - sinPhi*ry*sinB + cx
		ey := sinPhi*rx*cosB + cosPhi*ry*sinB + cy

		// Control point 1 (in user space): curPoint + t · derivative at thetaA.
		// d/dθ (cosPhi·rx·cosθ − sinPhi·ry·sinθ) = −cosPhi·rx·sinθ − sinPhi·ry·cosθ
		// d/dθ (sinPhi·rx·cosθ + cosPhi·ry·sinθ) = −sinPhi·rx·sinθ + cosPhi·ry·cosθ
		c1x := curX + t*(-cosPhi*rx*sinA-sinPhi*ry*cosA)
		c1y := curY + t*(-sinPhi*rx*sinA+cosPhi*ry*cosA)

		// Control point 2: endpoint − t · derivative at thetaB.
		c2x := ex - t*(-cosPhi*rx*sinB-sinPhi*ry*cosB)
		c2y := ey - t*(-sinPhi*rx*sinB+cosPhi*ry*cosB)

		p.CubicTo(c1x, c1y, c2x, c2y, ex, ey)
		curX, curY = ex, ey
	}
}

// pathScanner is a tiny SVG path-data tokenizer. Mirrors Blink's
// SVGPathStringSource: a forward cursor over the raw string, with
// helpers for whitespace/comma skipping, command-letter peeking,
// number reading (matching the SVG number grammar: optional sign,
// integer/fractional parts, optional exponent), and arc-flag reading
// (single 0/1 digit, optional surrounding whitespace).
type pathScanner struct {
	s string
	i int
}

func newPathScanner(s string) *pathScanner { return &pathScanner{s: s} }

func (sc *pathScanner) atEnd() bool { return sc.i >= len(sc.s) }

func (sc *pathScanner) advance() { sc.i++ }

func (sc *pathScanner) skipWSAndCommas() {
	for sc.i < len(sc.s) {
		c := sc.s[sc.i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == ',' {
			sc.i++
		} else {
			return
		}
	}
}

func (sc *pathScanner) peekCommand() (byte, bool) {
	if sc.i >= len(sc.s) {
		return 0, false
	}
	c := sc.s[sc.i]
	switch c {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v',
		'C', 'c', 'S', 's', 'Q', 'q', 'T', 't',
		'A', 'a', 'Z', 'z':
		return c, true
	}
	return 0, false
}

// readNumber reads an SVG number per SVG 1.1 §8.3.9:
//
//	number ::= ("+" | "-")? (digits ("." digits)? | "." digits) exponent?
//	exponent ::= ("e" | "E") ("+" | "-")? digits
//
// Whitespace and commas around the number are consumed before parsing.
func (sc *pathScanner) readNumber() (float64, bool) {
	sc.skipWSAndCommas()
	start := sc.i
	if sc.i < len(sc.s) && (sc.s[sc.i] == '+' || sc.s[sc.i] == '-') {
		sc.i++
	}
	hasDigits := false
	for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
		sc.i++
		hasDigits = true
	}
	if sc.i < len(sc.s) && sc.s[sc.i] == '.' {
		sc.i++
		for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
			sc.i++
			hasDigits = true
		}
	}
	if !hasDigits {
		sc.i = start
		return 0, false
	}
	if sc.i < len(sc.s) && (sc.s[sc.i] == 'e' || sc.s[sc.i] == 'E') {
		sc.i++
		if sc.i < len(sc.s) && (sc.s[sc.i] == '+' || sc.s[sc.i] == '-') {
			sc.i++
		}
		expHasDigits := false
		for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
			sc.i++
			expHasDigits = true
		}
		if !expHasDigits {
			sc.i = start
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(sc.s[start:sc.i], 64)
	if err != nil {
		sc.i = start
		return 0, false
	}
	return v, true
}

// readFlag reads an SVG arc flag per SVG 1.1 §8.3.8: a single digit
// 0 or 1. Whitespace and commas are consumed before.
func (sc *pathScanner) readFlag() (bool, bool) {
	sc.skipWSAndCommas()
	if sc.i >= len(sc.s) {
		return false, false
	}
	switch sc.s[sc.i] {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}

// parsePointList parses an SVG `points` attribute value
// (`<polyline>` / `<polygon>`) into a list of (x, y) pairs. Per SVG 1.1
// §9.7.1 the format is a list of numbers separated by whitespace or
// commas, taken as coordinate pairs. A trailing unpaired number is
// dropped (matching Blink's SVGParserUtilities behavior).
func parsePointList(s string) []geometry.PointF {
	sc := newPathScanner(s)
	var pts []geometry.PointF
	for {
		x, ok1 := sc.readNumber()
		if !ok1 {
			break
		}
		y, ok2 := sc.readNumber()
		if !ok2 {
			break
		}
		pts = append(pts, geometry.PointF{X: x, Y: y})
	}
	return pts
}
