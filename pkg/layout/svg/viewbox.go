package svg

import (
	"strconv"
	"strings"

	"louis14/pkg/geometry"
)

// PreserveAspectRatioAlign enumerates the nine align values of SVG's
// preserveAspectRatio attribute (xMinYMin … xMaxYMax + none). Mirrors
// Blink's SVGPreserveAspectRatio::Align enum.
type PreserveAspectRatioAlign int

const (
	AlignNone PreserveAspectRatioAlign = iota
	AlignXMinYMin
	AlignXMidYMin
	AlignXMaxYMin
	AlignXMinYMid
	AlignXMidYMid // default
	AlignXMaxYMid
	AlignXMinYMax
	AlignXMidYMax
	AlignXMaxYMax
)

// PreserveAspectRatioMeetOrSlice enumerates the meetOrSlice values.
// Mirrors Blink's SVGPreserveAspectRatio::MeetOrSlice enum.
type PreserveAspectRatioMeetOrSlice int

const (
	MeetOrSliceMeet PreserveAspectRatioMeetOrSlice = iota // default
	MeetOrSliceSlice
)

// PreserveAspectRatio holds a parsed preserveAspectRatio attribute.
type PreserveAspectRatio struct {
	Align       PreserveAspectRatioAlign
	MeetOrSlice PreserveAspectRatioMeetOrSlice
}

// DefaultPreserveAspectRatio returns the SVG default: xMidYMid meet.
func DefaultPreserveAspectRatio() PreserveAspectRatio {
	return PreserveAspectRatio{Align: AlignXMidYMid, MeetOrSlice: MeetOrSliceMeet}
}

// ViewBox is a parsed `viewBox` attribute. The Valid flag distinguishes a
// genuine viewBox="0 0 100 100" from an unset/malformed attribute (which
// the spec treats as "no viewBox" → user units == CSS px, identity
// viewport mapping).
type ViewBox struct {
	X, Y, Width, Height float64
	Valid               bool
}

// ParseViewBox parses an SVG viewBox attribute value of the form
//
//	"min-x min-y width height"
//
// returning (x, y, width, height, ok). Per SVG 2 §8.2.2:
//   - Whitespace and optional comma separators are accepted.
//   - All four values are parsed as numbers (user units).
//   - A negative width or height makes the viewBox invalid (ok=false).
//   - A zero width or height makes the viewBox invalid (ok=false) — the
//     element renders nothing.
//
// Returns ok=false for any parse failure, missing components, or non-
// positive width/height. Callers must keep the existing
// `getInlineSVGIntrinsicInfo` behavior in sync — this is the shared parser.
func ParseViewBox(s string) (x, y, w, h float64, ok bool) {
	parts := splitViewBox(s)
	if len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	var err error
	if x, err = strconv.ParseFloat(parts[0], 64); err != nil {
		return 0, 0, 0, 0, false
	}
	if y, err = strconv.ParseFloat(parts[1], 64); err != nil {
		return 0, 0, 0, 0, false
	}
	if w, err = strconv.ParseFloat(parts[2], 64); err != nil {
		return 0, 0, 0, 0, false
	}
	if h, err = strconv.ParseFloat(parts[3], 64); err != nil {
		return 0, 0, 0, 0, false
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	return x, y, w, h, true
}

// splitViewBox tokenizes a viewBox value into four string components.
// SVG 2 §8.2.2 allows commas and whitespace as separators between numbers.
func splitViewBox(s string) []string {
	// Replace commas with spaces, then split on whitespace.
	mapped := strings.Map(func(r rune) rune {
		if r == ',' {
			return ' '
		}
		return r
	}, s)
	return strings.Fields(mapped)
}

// ParsePreserveAspectRatio parses an SVG preserveAspectRatio attribute
// value: an optional "defer" keyword (used only on <image>, ignored here)
// followed by an align token and an optional meetOrSlice token.
//
// Returns the default (xMidYMid meet) for any unrecognized input.
// Mirrors SVGPreserveAspectRatio::Parse in Blink (svg_preserve_aspect_ratio.cc).
func ParsePreserveAspectRatio(s string) PreserveAspectRatio {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return DefaultPreserveAspectRatio()
	}
	// Skip an optional leading "defer" keyword. It is only meaningful on
	// <image>, ignored on <svg>/<symbol>/<marker>/<pattern>/<view>.
	if fields[0] == "defer" {
		fields = fields[1:]
		if len(fields) == 0 {
			return DefaultPreserveAspectRatio()
		}
	}

	var align PreserveAspectRatioAlign
	switch fields[0] {
	case "none":
		align = AlignNone
	case "xMinYMin":
		align = AlignXMinYMin
	case "xMidYMin":
		align = AlignXMidYMin
	case "xMaxYMin":
		align = AlignXMaxYMin
	case "xMinYMid":
		align = AlignXMinYMid
	case "xMidYMid":
		align = AlignXMidYMid
	case "xMaxYMid":
		align = AlignXMaxYMid
	case "xMinYMax":
		align = AlignXMinYMax
	case "xMidYMax":
		align = AlignXMidYMax
	case "xMaxYMax":
		align = AlignXMaxYMax
	default:
		return DefaultPreserveAspectRatio()
	}

	meetOrSlice := MeetOrSliceMeet
	if len(fields) >= 2 {
		switch fields[1] {
		case "meet":
			meetOrSlice = MeetOrSliceMeet
		case "slice":
			meetOrSlice = MeetOrSliceSlice
		default:
			// Unrecognized meetOrSlice — fall back to default.
		}
	}
	return PreserveAspectRatio{Align: align, MeetOrSlice: meetOrSlice}
}

// BuildViewBoxToViewportTransform returns the AffineTransform that maps
// a point in viewBox coordinates onto the viewport rectangle (in CSS px
// inside the <svg> content box), honoring preserveAspectRatio.
//
// Mirrors Blink's SVGPreserveAspectRatio::ComputeTransform / Blink's
// SVGSVGElement::ViewBoxToViewTransform (core/svg/svg_svg_element.cc):
//
//  1. If viewBox is not valid or viewport is empty, return identity (the
//     <svg> still uses user-units == CSS px in that case).
//  2. Compute the per-axis scale: viewport.width / viewBox.width and
//     viewport.height / viewBox.height.
//  3. preserveAspectRatio="none" → scale anisotropically and translate to
//     align the viewBox origin with the viewport origin.
//  4. align != none → uniform scale = meet ? min(sx,sy) : max(sx,sy).
//     Then translate so the viewBox is aligned per the align value
//     (xMin / xMid / xMax × yMin / yMid / yMax).
//
// xMidYMid meet is the SVG default.
//
// Note: the viewport's Origin is the viewport's top-left in <svg>
// content-box coordinates. For an inline <svg>, that is typically (0, 0).
// The transform maps viewBox space → that same coordinate system.
func BuildViewBoxToViewportTransform(viewBox ViewBox, viewport geometry.RectF, par PreserveAspectRatio) geometry.AffineTransform {
	if !viewBox.Valid || viewBox.Width <= 0 || viewBox.Height <= 0 ||
		viewport.Width() <= 0 || viewport.Height() <= 0 {
		return geometry.Identity()
	}

	scaleX := viewport.Width() / viewBox.Width
	scaleY := viewport.Height() / viewBox.Height

	if par.Align == AlignNone {
		// Anisotropic: scaleX along X, scaleY along Y. The viewBox's origin
		// (viewBox.X, viewBox.Y) maps to the viewport's origin.
		// (viewport.Origin.X - scaleX * viewBox.X, viewport.Origin.Y - scaleY * viewBox.Y)
		// gives the translation that puts viewBox.X at viewport.Origin.X
		// after scaling.
		return geometry.NewAffineTransform(
			scaleX, 0,
			0, scaleY,
			viewport.X()-scaleX*viewBox.X,
			viewport.Y()-scaleY*viewBox.Y,
		)
	}

	// Uniform scale (meet → min, slice → max).
	var scale float64
	if par.MeetOrSlice == MeetOrSliceSlice {
		scale = scaleX
		if scaleY > scale {
			scale = scaleY
		}
	} else {
		scale = scaleX
		if scaleY < scale {
			scale = scaleY
		}
	}

	// Sizes of the scaled viewBox in viewport coordinates.
	scaledW := viewBox.Width * scale
	scaledH := viewBox.Height * scale

	// X alignment.
	var tx float64
	switch par.Align {
	case AlignXMinYMin, AlignXMinYMid, AlignXMinYMax:
		tx = viewport.X() - scale*viewBox.X
	case AlignXMaxYMin, AlignXMaxYMid, AlignXMaxYMax:
		tx = viewport.X() + (viewport.Width() - scaledW) - scale*viewBox.X
	default: // xMid*
		tx = viewport.X() + (viewport.Width()-scaledW)/2 - scale*viewBox.X
	}

	// Y alignment.
	var ty float64
	switch par.Align {
	case AlignXMinYMin, AlignXMidYMin, AlignXMaxYMin:
		ty = viewport.Y() - scale*viewBox.Y
	case AlignXMinYMax, AlignXMidYMax, AlignXMaxYMax:
		ty = viewport.Y() + (viewport.Height() - scaledH) - scale*viewBox.Y
	default: // *YMid
		ty = viewport.Y() + (viewport.Height()-scaledH)/2 - scale*viewBox.Y
	}

	return geometry.NewAffineTransform(scale, 0, 0, scale, tx, ty)
}
