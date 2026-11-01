package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
)

// noTableCollapsedEdge marks an edge index past the grid (Blink's UINT_MAX).
const noTableCollapsedEdge = -1

// TableCollapsedEdge is one collapsed border edge being painted, with its
// width and style cached. Mirrors the TableCollapsedEdge helper class of
// core/paint/table_painters.cc:35-238 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
type TableCollapsedEdge struct {
	borders     *layout.TableBorders
	edgeIndex   int
	borderWidth float64
	borderStyle css.BorderStyle
}

func newTableCollapsedEdge(borders *layout.TableBorders, edgeIndex int) TableCollapsedEdge {
	e := TableCollapsedEdge{borders: borders, edgeIndex: edgeIndex}
	if edgeIndex < 0 || edgeIndex >= borders.EdgeCount() {
		e.edgeIndex = noTableCollapsedEdge
	}
	e.initCachedProps()
	return e
}

// offset returns the edge offset positions away (TableCollapsedEdge's
// (source, offset) constructor, :45-57).
func (e TableCollapsedEdge) offset(offset int) TableCollapsedEdge {
	if e.edgeIndex == noTableCollapsedEdge {
		return newTableCollapsedEdge(e.borders, noTableCollapsedEdge)
	}
	return newTableCollapsedEdge(e.borders, e.edgeIndex+offset)
}

func (e *TableCollapsedEdge) initCachedProps() {
	if e.edgeIndex == noTableCollapsedEdge {
		e.borderWidth, e.borderStyle = 0, css.BorderStyleNone
		return
	}
	e.borderWidth = e.borders.BorderWidth(e.edgeIndex)
	e.borderStyle = e.borders.BorderStyle(e.edgeIndex)
}

// Exists mirrors TableCollapsedEdge::Exists (:69).
func (e TableCollapsedEdge) Exists() bool { return e.edgeIndex != noTableCollapsedEdge }

// CanPaint mirrors TableCollapsedEdge::CanPaint (:71-80).
func (e TableCollapsedEdge) CanPaint() bool {
	if !e.Exists() {
		return false
	}
	if e.borderStyle == css.BorderStyleNone || e.borderStyle == css.BorderStyleHidden {
		return false
	}
	return e.borderWidth != 0
}

// compareBoxOrder mirrors TableCollapsedEdge::CompareBoxOrder (:88-96).
func (e TableCollapsedEdge) compareBoxOrder(other int) int {
	a, b := e.borders.BoxOrder(e.edgeIndex), e.borders.BoxOrder(other)
	if a < b {
		return 1
	}
	if a > b {
		return -1
	}
	return 0
}

// IsInlineAxis mirrors TableCollapsedEdge::IsInlineAxis (:98-102): odd edges
// run along the inline axis.
func (e TableCollapsedEdge) IsInlineAxis() bool {
	return e.edgeIndex%e.borders.EdgesPerRow()%2 != 0
}

// TableColumn mirrors TableCollapsedEdge::TableColumn (:104-107).
func (e TableCollapsedEdge) TableColumn() int { return e.edgeIndex % e.borders.EdgesPerRow() / 2 }

// TableRow mirrors TableCollapsedEdge::TableRow (:109-112).
func (e TableCollapsedEdge) TableRow() int { return e.edgeIndex / e.borders.EdgesPerRow() }

// compareTableCollapsedEdgesForPaint decides which edge paints a joint: 1
// if lhs wins, -1 if rhs wins, 0 on a tie. Mirrors
// TableCollapsedEdge::CompareForPaint (:116-148).
func compareTableCollapsedEdgesForPaint(lhs, rhs TableCollapsedEdge) int {
	if lhs.edgeIndex == rhs.edgeIndex {
		return 0
	}
	lhsPaints, rhsPaints := lhs.CanPaint(), rhs.CanPaint()
	if lhsPaints && rhsPaints {
		if lhs.borderWidth > rhs.borderWidth {
			return 1
		}
		if lhs.borderWidth < rhs.borderWidth {
			return -1
		}
		// Paint precedence differs from the winning-edge rules: hidden
		// does not win.
		if lhs.borderStyle == rhs.borderStyle {
			return lhs.compareBoxOrder(rhs.edgeIndex)
		}
		if rhs.borderStyle == css.BorderStyleHidden {
			return 1
		}
		if lhs.borderStyle == css.BorderStyleHidden {
			return -1
		}
		if layout.EBorderStyleOrder(lhs.borderStyle) > layout.EBorderStyleOrder(rhs.borderStyle) {
			return 1
		}
		return -1
	}
	if !lhsPaints && !rhsPaints {
		return 0
	}
	if !lhsPaints {
		return -1
	}
	return 1
}

// Neighbor edges around the edge's start and end intersections (:150-206).
func (e TableCollapsedEdge) edgeBeforeStartIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(-2)
	}
	return e.offset(-1)
}

func (e TableCollapsedEdge) edgeAfterStartIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(0)
	}
	return e.offset(1)
}

func (e TableCollapsedEdge) edgeOverStartIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(-(e.borders.EdgesPerRow() + 1))
	}
	return e.offset(-e.borders.EdgesPerRow())
}

func (e TableCollapsedEdge) edgeUnderStartIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(-1)
	}
	return e.offset(0)
}

func (e TableCollapsedEdge) edgeBeforeEndIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(0)
	}
	return e.offset(e.borders.EdgesPerRow() - 1)
}

func (e TableCollapsedEdge) edgeAfterEndIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(2)
	}
	return e.offset(e.borders.EdgesPerRow() + 1)
}

func (e TableCollapsedEdge) edgeOverEndIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(-(e.borders.EdgesPerRow() - 1))
	}
	return e.offset(0)
}

func (e TableCollapsedEdge) edgeUnderEndIntersection() TableCollapsedEdge {
	if e.IsInlineAxis() {
		return e.offset(1)
	}
	return e.offset(e.borders.EdgesPerRow())
}

// winner returns the edge a CompareForPaint result picks between a and b.
func winner(compare int, a, b TableCollapsedEdge) TableCollapsedEdge {
	if compare == 1 {
		return a
	}
	return b
}

// computeEdgeJoints sizes the start and end joints of edge and reports
// whether edge wins each (so it grows into the joint rather than shrinking
// out of it). Mirrors ComputeEdgeJoints (table_painters.cc:243-332 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) without fragmentation: louis14
// paints a table in one fragment, so both fragmentation-boundary flags are
// false.
func computeEdgeJoints(edge TableCollapsedEdge) (startJoint, endJoint layout.LogicalSize, startWins, endWins bool) {
	before := edge.edgeBeforeStartIntersection()
	after := edge.edgeAfterStartIntersection()
	over := edge.edgeOverStartIntersection()
	under := edge.edgeUnderStartIntersection()

	inlineCompare := compareTableCollapsedEdgesForPaint(before, after)
	startJoint.BlockSize = winner(inlineCompare, before, after).borderWidth
	blockCompare := compareTableCollapsedEdgesForPaint(over, under)
	startJoint.InlineSize = winner(blockCompare, over, under).borderWidth
	inlineVsBlock := compareTableCollapsedEdgesForPaint(winner(inlineCompare, before, after), winner(blockCompare, over, under))
	if edge.IsInlineAxis() {
		startWins = inlineVsBlock != -1 && inlineCompare != 1
	} else {
		startWins = inlineVsBlock != 1 && blockCompare != 1
	}

	before = edge.edgeBeforeEndIntersection()
	after = edge.edgeAfterEndIntersection()
	over = edge.edgeOverEndIntersection()
	under = edge.edgeUnderEndIntersection()

	inlineCompare = compareTableCollapsedEdgesForPaint(before, after)
	endJoint.BlockSize = winner(inlineCompare, before, after).borderWidth
	blockCompare = compareTableCollapsedEdgesForPaint(over, under)
	endJoint.InlineSize = winner(blockCompare, over, under).borderWidth
	inlineVsBlock = compareTableCollapsedEdgesForPaint(winner(inlineCompare, before, after), winner(blockCompare, over, under))
	if edge.IsInlineAxis() {
		endWins = inlineVsBlock != -1 && inlineCompare != -1
	} else {
		endWins = inlineVsBlock != 1 && blockCompare != -1
	}
	return
}

// PaintCollapsedBorders paints a border-collapse:collapse table's borders
// edge by edge along the grid, each a straight BoxBorderPainter::DrawBoxSide
// segment (so collapsed borders never take the rounded-border path), sized
// into the joints its neighbors lose. Mirrors TablePainter::
// PaintCollapsedBorders (table_painters.cc:518-729 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), walking each section's rows.
// louis14 tables do not fragment, so the fragmentation adjustments drop out.
func (r *Renderer) PaintCollapsedBorders(layer *PaintLayer) {
	box := layer.Box
	if box == nil || box.TableCollapsedBorders == nil || box.TableCollapsedBordersGeometry == nil {
		return
	}
	borders := box.TableCollapsedBorders
	geom := box.TableCollapsedBordersGeometry
	wdm := geom.WritingDirection
	edgesPerRow := borders.EdgesPerRow()
	if edgesPerRow == 0 {
		return
	}
	converter := layout.NewConverter(wdm, layout.PhysicalSize{Width: box.Width, Height: box.Height})

	// Sections paint one after another; the inline edges along a section
	// boundary belong to the section above (previous_painted_row_index).
	previousPaintedRow := -1
	for _, section := range geom.Sections {
		rowOffsets := section.RowOffsets
		for edge := newTableCollapsedEdge(borders, section.StartRowIndex*edgesPerRow); edge.Exists(); edge = edge.offset(1) {
			tableRow := edge.TableRow()
			tableColumn := edge.TableColumn()
			fragmentTableRow := tableRow - section.StartRowIndex
			if fragmentTableRow >= len(rowOffsets) {
				previousPaintedRow = tableRow - 1
				break
			}
			if !edge.CanPaint() {
				continue
			}
			rowStart := rowOffsets[fragmentTableRow]
			columnStart := geom.Columns[tableColumn]

			if edge.IsInlineAxis() {
				if tableColumn+1 >= len(geom.Columns) || previousPaintedRow == tableRow {
					continue
				}
			} else if fragmentTableRow+1 >= len(rowOffsets) {
				continue
			}

			var inlineStart, blockStart, inlineSize, blockSize float64
			startJoint, endJoint, startWins, endWins := computeEdgeJoints(edge)
			if edge.IsInlineAxis() {
				inlineStart = columnStart
				inlineSize = geom.Columns[tableColumn+1] - columnStart
				blockStart = rowStart - edge.borderWidth/2
				blockSize = edge.borderWidth
				if startWins {
					inlineStart -= startJoint.InlineSize / 2
					inlineSize += startJoint.InlineSize / 2
				} else {
					inlineStart += startJoint.InlineSize / 2
					inlineSize -= startJoint.InlineSize / 2
				}
				if endWins {
					inlineSize += endJoint.InlineSize / 2
				} else {
					inlineSize -= endJoint.InlineSize / 2
				}
			} else {
				blockStart = rowStart
				blockSize = rowOffsets[fragmentTableRow+1] - rowStart
				inlineStart = columnStart - edge.borderWidth/2
				inlineSize = edge.borderWidth
				if startWins {
					blockStart -= startJoint.BlockSize / 2
					blockSize += startJoint.BlockSize / 2
				} else {
					blockStart += startJoint.BlockSize / 2
					blockSize -= startJoint.BlockSize / 2
				}
				if endWins {
					blockSize += endJoint.BlockSize / 2
				} else {
					blockSize -= endJoint.BlockSize / 2
				}
			}

			physSize := layout.ToPhysicalSize(layout.LogicalSize{InlineSize: inlineSize, BlockSize: blockSize}, wdm.WM)
			off := converter.ToPhysicalOffset(layout.LogicalOffset{InlineOffset: inlineStart, BlockOffset: blockStart}, physSize)
			snapped := geometry.ToPixelSnappedRect(toLayoutRect(box.X+off.X, box.Y+off.Y, physSize.Width, physSize.Height))

			side := BoxSideLeft
			if edge.IsInlineAxis() == !wdm.IsVertical() {
				side = BoxSideTop
			}
			r.DrawBoxSide(intRect{snapped.Min.X, snapped.Min.Y, snapped.Dx(), snapped.Dy()}, side,
				borders.BorderColor(edge.edgeIndex), edge.borderStyle)
		}
	}
}

// tableBoxHasLayer reports whether a table part paints in a layer of its
// own, standing in for LayoutBoxModelObject::HasLayer: a positioned box or
// one that creates a stacking context.
func tableBoxHasLayer(box *layout.Box) bool {
	if box == nil || box.Style == nil {
		return false
	}
	return (box.Position != css.PositionStatic && box.Position != "") || box.CreatesStackingContext()
}

// clipTableCellBackground clips a collapsed-border cell's box decorations to
// inside its borders when the cell, its row or its section paints in a
// layer: such a layer paints after the table's collapsed borders and would
// otherwise cover them. Returns whether it pushed a clip the caller must pop.
// Mirrors TableCellBackgroundClipper (table_painters.cc:382-413 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) clipTableCellBackground(layer *PaintLayer, rect layout.PhysicalRect) bool {
	cell := layer.Box
	if !layer.HasCollapsedBorders || cell == nil || cell.Style == nil || cell.Style.GetDisplay() != css.DisplayTableCell {
		return false
	}
	row := cell.Parent
	needsClip := tableBoxHasLayer(cell) || tableBoxHasLayer(row)
	if row != nil && row.Parent != nil && row.Parent.Style != nil {
		switch row.Parent.Style.GetDisplay() {
		case css.DisplayTableRowGroup, css.DisplayTableHeaderGroup, css.DisplayTableFooterGroup:
			needsClip = needsClip || tableBoxHasLayer(row.Parent)
		}
	}
	if !needsClip {
		return false
	}
	clip := geometry.ToPixelSnappedRect(toLayoutRect(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height).
		Contract(toLayoutStrut(cell.Border)))
	r.dc.Push()
	r.dc.DrawRectangle(float64(clip.Min.X), float64(clip.Min.Y), float64(clip.Dx()), float64(clip.Dy()))
	r.dc.Clip()
	return true
}
