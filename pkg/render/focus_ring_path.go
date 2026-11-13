package render

import (
	"image"
	"math"
	"slices"
	"sort"

	"louis14/pkg/css"
)

// rightAnglePath is Blink's ComputeRightAnglePath (core/paint/
// outline_painter.cc:87-101 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// each rect is outset by outline-offset (clamped per axis by
// AdjustedOutlineOffset, :77-82) and the union's boundary is returned as
// closed right-angle contours (SkRegion::getBoundaryPath). A nil result is
// Blink's empty path.
func rightAnglePath(rects []image.Rectangle, offset int) [][]image.Point {
	outset := make([]image.Rectangle, 0, len(rects))
	for _, r := range rects {
		ox := max(offset, -r.Dx()/2)
		oy := max(offset, -r.Dy()/2)
		outset = append(outset, image.Rect(r.Min.X-ox, r.Min.Y-oy, r.Max.X+ox, r.Max.Y+oy))
	}
	return regionBoundary(regionRects(outset))
}

// regionRects decomposes the union of rects into SkRegion's canonical runs:
// horizontal bands, each a sorted list of disjoint x-intervals with touching
// intervals merged, and vertically adjacent bands with identical intervals
// coalesced. Returned in SkRegion::Iterator order (band by band, left to
// right). no Blink analog: Skia's SkRegion union (src/core/SkRegion.cpp @
// skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e, Chromium a9f50e52's
// skia_revision).
func regionRects(rects []image.Rectangle) []image.Rectangle {
	var ys []int
	for _, r := range rects {
		if !r.Empty() {
			ys = append(ys, r.Min.Y, r.Max.Y)
		}
	}
	slices.Sort(ys)
	ys = slices.Compact(ys)

	type band struct {
		top, bottom int
		xs          [][2]int
	}
	var bands []band
	for i := 0; i+1 < len(ys); i++ {
		top, bottom := ys[i], ys[i+1]
		var spans [][2]int
		for _, r := range rects {
			if !r.Empty() && r.Min.Y <= top && r.Max.Y >= bottom {
				spans = append(spans, [2]int{r.Min.X, r.Max.X})
			}
		}
		if len(spans) == 0 {
			continue
		}
		sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
		merged := spans[:1]
		for _, s := range spans[1:] {
			if last := &merged[len(merged)-1]; s[0] <= last[1] {
				last[1] = max(last[1], s[1])
			} else {
				merged = append(merged, s)
			}
		}
		if n := len(bands); n > 0 && bands[n-1].bottom == top && slices.Equal(bands[n-1].xs, merged) {
			bands[n-1].bottom = bottom
			continue
		}
		bands = append(bands, band{top, bottom, merged})
	}

	var out []image.Rectangle
	for _, b := range bands {
		for _, s := range b.xs {
			out = append(out, image.Rect(s[0], b.top, s[1], b.bottom))
		}
	}
	return out
}

// regionEdge is SkRegion_path.cpp's Edge: a vertical region edge at x from
// y0 to y1 (a left edge runs bottom to top, a right edge top to bottom),
// linked to the edge its y1 end continues into.
type regionEdge struct {
	x, y0, y1 int
	y0Linked  bool
	y1Linked  bool
	next      int
	used      bool
}

// regionBoundary is SkRegion::addBoundaryPath (src/core/SkRegion_path.cpp @
// skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e): two vertical edges per
// region rect, sorted by x then top, each linked to the first later edge
// sharing its endpoint (find_link), then walked into clockwise contours of
// alternating vertical and horizontal lines, merging collinear verticals
// (extract_path). A single rect is its own contour. no Blink analog: Skia.
func regionBoundary(rects []image.Rectangle) [][]image.Point {
	switch len(rects) {
	case 0:
		return nil
	case 1:
		r := rects[0]
		return [][]image.Point{{r.Min, {r.Max.X, r.Min.Y}, r.Max, {r.Min.X, r.Max.Y}}}
	}
	edges := make([]regionEdge, 0, 2*len(rects))
	for _, r := range rects {
		edges = append(edges,
			regionEdge{x: r.Min.X, y0: r.Max.Y, y1: r.Min.Y, next: -1},
			regionEdge{x: r.Max.X, y0: r.Min.Y, y1: r.Max.Y, next: -1})
	}
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].x != edges[b].x {
			return edges[a].x < edges[b].x
		}
		return min(edges[a].y0, edges[a].y1) < min(edges[b].y0, edges[b].y1)
	})
	// find_link.
	for i := range edges {
		base := &edges[i]
		if !base.y0Linked {
			for j := i + 1; j < len(edges); j++ {
				if e := &edges[j]; !e.y1Linked && e.y1 == base.y0 {
					e.next = i
					e.y1Linked = true
					break
				}
			}
		}
		if !base.y1Linked {
			for j := i + 1; j < len(edges); j++ {
				if e := &edges[j]; !e.y0Linked && e.y0 == base.y1 {
					base.next = j
					e.y0Linked = true
					break
				}
			}
		}
		base.y0Linked, base.y1Linked = true, true
	}
	// extract_path.
	var contours [][]image.Point
	for start := range edges {
		if edges[start].used || edges[start].next < 0 {
			continue
		}
		prev := start
		edges[prev].used = true
		pts := []image.Point{{edges[prev].x, edges[prev].y0}}
		for e := edges[prev].next; ; e = edges[e].next {
			if edges[prev].x != edges[e].x || edges[prev].y1 != edges[e].y0 {
				pts = append(pts, image.Point{edges[prev].x, edges[prev].y1}, image.Point{edges[e].x, edges[e].y0})
			}
			prev = e
			edges[e].used = true
			if e == start {
				break
			}
		}
		// extract_path's final vertical line closes onto the start point,
		// which pts already holds.
		if last := pts[len(pts)-1]; last == pts[0] {
			pts = pts[:len(pts)-1]
		}
		contours = append(contours, pts)
	}
	return contours
}

// cornerPathEffect traces a closed right-angle contour with every corner
// replaced by a quadratic curve through it, cut back by radius along each
// line (or half the line when shorter than twice radius): Skia's
// SkCornerPathEffect, which DrawPlatformFocusRing applies for a uniform
// focus ring radius (platform/graphics/platform_focus_ring.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; src/effects/
// SkCornerPathEffect.cpp @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
func cornerPathEffect(dc quadPathBuilder, contour []image.Point, radius float64) {
	n := len(contour)
	pt := func(i int) (float64, float64) {
		p := contour[i%n]
		return float64(p.X), float64(p.Y)
	}
	var firstX, firstY float64
	for i := 0; i < n; i++ {
		ax, ay := pt(i)
		bx, by := pt(i + 1)
		sx, sy, drawSegment := cornerStep(ax, ay, bx, by, radius)
		if i == 0 {
			dc.MoveTo(ax+sx, ay+sy)
			firstX, firstY = sx, sy
		} else {
			dc.QuadraticTo(ax, ay, ax+sx, ay+sy)
		}
		if drawSegment {
			dc.LineTo(bx-sx, by-sy)
		}
	}
	cx, cy := pt(0)
	dc.QuadraticTo(cx, cy, cx+firstX, cy+firstY)
	dc.ClosePath()
}

// quadPathBuilder is a pathBuilder that can also add quadratic curves.
type quadPathBuilder interface {
	pathBuilder
	QuadraticTo(x1, y1, x2, y2 float64)
}

// cornerStep is SkCornerPathEffect's ComputeStep: the vector from a toward
// b of length radius, or half of a→b when the line is no longer than twice
// radius (then no straight segment is drawn).
func cornerStep(ax, ay, bx, by, radius float64) (sx, sy float64, drawSegment bool) {
	dx, dy := bx-ax, by-ay
	dist := math.Hypot(dx, dy)
	if dist <= radius*2 {
		return dx / 2, dy / 2, false
	}
	return dx * radius / dist, dy * radius / dist, true
}

// addCornerRadiiToPath is Blink's AddCornerRadiiToPath for convex radii
// equal to concave radii (outline_painter.cc:203-322 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each line is shortened by the
// radii of the corners at its ends (AdjustLineBetweenCorners, scaled down
// and floored when they overlap), and each corner becomes a 90° arc
// (conicTo with weight 1/√2, drawn here as a cubic quarter ellipse with
// roundedCornerKappa).
func addCornerRadiiToPath(dc pathBuilder, contour []image.Point, radii css.EllipticalRadii) {
	n := len(contour)
	at := func(i int) image.Point { return contour[(i%n+n)%n] }
	type line struct{ sx, sy, ex, ey float64 }
	lines := make([]line, n)
	for i := 0; i < n; i++ {
		prev, start, end, next := at(i-1), at(i), at(i+1), at(i+2)
		c1 := radiiCorner(radii, prev, start, end)
		c2 := radiiCorner(radii, start, end, next)
		l := line{float64(start.X), float64(start.Y), float64(end.X), float64(end.Y)}
		if start.X == end.X {
			l.sy, l.ey = shortenBetweenCorners(l.sy, l.ey, c1.Ry, c2.Ry)
		} else {
			l.sx, l.ex = shortenBetweenCorners(l.sx, l.ex, c1.Rx, c2.Rx)
		}
		lines[i] = l
	}
	dc.MoveTo(lines[n-1].ex, lines[n-1].ey)
	for i := 0; i < n; i++ {
		corner := at(i)
		cx, cy := float64(corner.X), float64(corner.Y)
		px, py := lines[(i+n-1)%n].ex, lines[(i+n-1)%n].ey
		qx, qy := lines[i].sx, lines[i].sy
		k := roundedCornerKappa
		dc.CubicTo(px+(cx-px)*k, py+(cy-py)*k, qx+(cx-qx)*k, qy+(cy-qy)*k, qx, qy)
		dc.LineTo(lines[i].ex, lines[i].ey)
	}
	dc.ClosePath()
}

// shortenBetweenCorners is one axis of Blink's AdjustLineBetweenCorners
// (outline_painter.cc:240-288 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the line from start to end loses r1 at its start and r2 at its end,
// both scaled down and floored when together they exceed its length.
func shortenBetweenCorners(start, end, r1, r2 float64) (float64, float64) {
	if length := math.Abs(end - start); r1+r2 > length {
		scale := length / (r1 + r2)
		r1, r2 = math.Floor(r1*scale), math.Floor(r2*scale)
	}
	if start < end {
		return start + r1, end - r2
	}
	return start - r1, end + r2
}

// radiiCorner is Blink's GetRadiiCorner with convex radii equal to concave
// radii: the radius of the corner p2 between p1→p2 and p2→p3, picked by
// which way the contour turns there; zero where p2 is no corner.
func radiiCorner(radii css.EllipticalRadii, p1, p2, p3 image.Point) css.EllipticalRadius {
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	if p1.X == p2.X {
		if p1.Y == p2.Y || p2.X == p3.X {
			return css.EllipticalRadius{}
		}
		if p1.Y < p2.Y {
			if p2.X < p3.X {
				return bl
			}
			return br
		}
		if p2.X < p3.X {
			return tl
		}
		return tr
	}
	if p2.X != p3.X || p2.Y == p3.Y {
		return css.EllipticalRadius{}
	}
	if p1.X < p2.X {
		if p2.Y < p3.Y {
			return tr
		}
		return br
	}
	if p2.Y < p3.Y {
		return tl
	}
	return bl
}
