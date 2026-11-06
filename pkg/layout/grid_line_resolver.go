package layout

import "louis14/pkg/css"

// gridLineResolver resolves grid items' lines from their four placement
// longhands against the explicit grid: its track counts, its named lines,
// and the implicit <area>-start / <area>-end lines its named areas create.
// Mirrors Blink's GridLineResolver (core/layout/grid/grid_line_resolver.cc
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Lines are 0-based. Not
// ported: a name with more than one line (louis14 keeps the first; an nth
// line past it counts on into the implicit grid), named spans (as an
// unnamed span of the same count), and implicit tracks before the explicit
// grid (lines before line 0 clamp to it).
type gridLineResolver struct {
	explicitCols, explicitRows int
	colLineNames, rowLineNames map[string]int // 1-based lines
	namedAreas                 map[string]css.GridAreaInfo
}

// gridSpan is one axis of a resolved placement: 0-based lines [start, end)
// when definite, else only the span size auto-placement fills.
type gridSpan struct {
	start, end int
	definite   bool
	size       int
}

func (r gridLineResolver) explicitCount(columns bool) int {
	if columns {
		return r.explicitCols
	}
	return r.explicitRows
}

// namedLine is the first line named name in the axis, from the explicit
// named lines or an area's implicit -start / -end line.
func (r gridLineResolver) namedLine(name string, columns bool) (int, bool) {
	names := r.rowLineNames
	if columns {
		names = r.colLineNames
	}
	if l, ok := names[name]; ok {
		return l - 1, true
	}
	for area, info := range r.namedAreas {
		start, end := info.RowStart, info.RowEnd
		if columns {
			start, end = info.ColStart, info.ColEnd
		}
		switch name {
		case area + "-start":
			return start - 1, true
		case area + "-end":
			return end - 1, true
		}
	}
	return 0, false
}

// resolvePosition is a definite line's position (ResolveGridPosition,
// :760-825).
func (r gridLineResolver) resolvePosition(p css.GridLine, columns, startSide bool) int {
	last := r.explicitCount(columns)
	switch {
	case p.IsCustomIdent():
		// <ident>-start / <ident>-end first, then the line named ident;
		// failing both, every implicit line has the name (:780-818).
		suffix := "-end"
		if startSide {
			suffix = "-start"
		}
		if l, ok := r.namedLine(p.Name+suffix, columns); ok {
			return l
		}
		if l, ok := r.namedLine(p.Name, columns); ok {
			return l
		}
		return last + 1
	case p.Name != "":
		// The nth line named Name (ResolveNamedGridLinePosition, :734-758).
		l, found := r.namedLine(p.Name, columns)
		switch {
		case p.Integer > 0 && found:
			if p.Integer == 1 {
				return l
			}
			return last + p.Integer - 1
		case p.Integer > 0:
			return last + p.Integer
		case found && p.Integer == -1:
			return l
		}
		return 0
	case p.Integer > 0:
		return p.Integer - 1
	}
	// A negative integer counts back from the explicit grid's end (:776-779).
	return last - (-p.Integer - 1)
}

// resolveAgainstOpposite resolves an auto or span position against the
// definite line on the other side (ResolveGridPositionAgainstOppositePosition,
// :691-715).
func resolveAgainstOpposite(opposite int, p css.GridLine, startSide bool) gridSpan {
	n := 1
	if p.Span {
		n = p.Integer
	}
	if startSide {
		return gridSpan{start: opposite - n, end: opposite, definite: true}
	}
	return gridSpan{start: opposite, end: opposite + n, definite: true}
}

// resolve is the item's placement in one axis
// (ResolveGridPositionsFromStyle, :827-877, with
// InitialAndFinalPositionsFromStyle's error handling, :417-440).
func (r gridLineResolver) resolve(style *css.Style, columns bool) gridSpan {
	startProp, endProp := "grid-row-start", "grid-row-end"
	if columns {
		startProp, endProp = "grid-column-start", "grid-column-end"
	}
	initial, final := style.GetGridLine(startProp), style.GetGridLine(endProp)
	if initial.Span && final.Span {
		final = css.GridLine{Auto: true}
	}
	if initial.Auto && final.Span && final.Name != "" {
		final = css.GridLine{Span: true, Integer: 1}
	}
	if final.Auto && initial.Span && initial.Name != "" {
		initial = css.GridLine{Span: true, Integer: 1}
	}
	initialOpposite := initial.Auto || initial.Span
	finalOpposite := final.Auto || final.Span

	var span gridSpan
	switch {
	case initialOpposite && finalOpposite:
		size := 1
		if initial.Span {
			size = initial.Integer
		} else if final.Span {
			size = final.Integer
		}
		return gridSpan{size: size}
	case initialOpposite:
		span = resolveAgainstOpposite(r.resolvePosition(final, columns, false), initial, true)
	case finalOpposite:
		span = resolveAgainstOpposite(r.resolvePosition(initial, columns, true), final, false)
	default:
		start, end := r.resolvePosition(initial, columns, true), r.resolvePosition(final, columns, false)
		if end < start {
			start, end = end, start
		} else if end == start {
			end = start + 1
		}
		span = gridSpan{start: start, end: end, definite: true}
	}
	// No implicit tracks before the explicit grid in louis14.
	if span.start < 0 {
		span.end, span.start = max(span.end-span.start, 1), 0
	}
	span.size = span.end - span.start
	return span
}
