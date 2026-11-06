package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/render"
)

func isGreen(c color.RGBA) bool { return c.G > 100 && c.R < 60 && c.B < 60 }

// TestDepthSort3D_CloserPlanePaintsOnTop: the participants of a 3D
// rendering context are drawn back to front by depth, not in paint order:
// viz batches the quads of a sorting context into DrawPolygons and draws
// them through a BspTree (DirectRenderer::FlushPolygons,
// components/viz/service/display/direct_renderer.cc:591-603, :806-847 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The first child sits 10px
// nearer the viewer, so it covers the second where they overlap.
func TestDepthSort3D_CloserPlanePaintsOnTop(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform-style:preserve-3d;transform:translateZ(0);width:100px;height:100px;position:relative">` +
		`<div style="position:absolute;width:100px;height:100px;background:lime;transform:translateZ(10px)"></div>` +
		`<div style="position:absolute;width:100px;height:100px;background:red;transform:translateZ(0)"></div>` +
		`</div></body></html>`
	img := render.RenderPage(t, src, 200, 200)
	if c := img.RGBAAt(50, 50); !isGreen(c) {
		t.Errorf("pixel (50,50) = %v, want the nearer lime plane", c)
	}
}

// TestDepthSort3D_IntersectingPlanesSplit: two planes that cross are split
// along their intersection, each half drawn in depth order
// (DrawPolygon::SplitPolygon, BspTree). One plane leans back on the left,
// the other on the right, so each is in front on one side.
func TestDepthSort3D_IntersectingPlanesSplit(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform-style:preserve-3d;transform:translateZ(0);width:200px;height:100px;position:relative">` +
		`<div style="position:absolute;width:200px;height:100px;background:lime;transform:rotateY(30deg)"></div>` +
		`<div style="position:absolute;width:200px;height:100px;background:blue;transform:rotateY(-30deg)"></div>` +
		`</div></body></html>`
	img := render.RenderPage(t, src, 300, 200)
	// rotateY(30deg) brings the left edge toward the viewer (+z), so the
	// lime plane is in front on the left and the blue one on the right.
	left, right := img.RGBAAt(40, 50), img.RGBAAt(160, 50)
	if !isGreen(left) {
		t.Errorf("left pixel = %v, want lime in front", left)
	}
	if right.B < 150 || right.G > 100 {
		t.Errorf("right pixel = %v, want blue in front", right)
	}
}

// TestDepthSort3D_PlaneBehindCameraIsDropped: a participant entirely behind
// the camera maps to fewer than three clipped vertices; viz adds only
// polygons with a normal (direct_renderer.cc:829-836), and it must not
// reach the BspTree, whose split math needs a vertex.
func TestDepthSort3D_PlaneBehindCameraIsDropped(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><div style="perspective:100px">` +
		`<div style="transform-style:preserve-3d;transform:translateZ(0);width:100px;height:100px;position:relative">` +
		`<div style="position:absolute;width:100px;height:100px;background:red;transform:translateZ(500px)"></div>` +
		`<div style="position:absolute;width:100px;height:100px;background:lime"></div>` +
		`</div></div></body></html>`
	img := render.RenderPage(t, src, 200, 200)
	if c := img.RGBAAt(50, 50); !isGreen(c) {
		t.Errorf("pixel (50,50) = %v, want lime with the behind-camera plane dropped", c)
	}
}

// TestDepthSort3D_RootBehindCameraIsDropped: when the root's own quads lie
// behind the camera they clip to no vertices, and as the first polygons
// they would become the BspTree's splitting plane, whose split math reads
// the plane's first vertex. The guard drops them; the participant in front
// still draws.
func TestDepthSort3D_RootBehindCameraIsDropped(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><div style="perspective:100px">` +
		`<div style="transform-style:preserve-3d;transform:translateZ(500px);width:100px;height:100px;position:relative;background:red">` +
		`<div style="position:absolute;width:100px;height:100px;background:lime;transform:translateZ(-450px)"></div>` +
		`</div></div></body></html>`
	img := render.RenderPage(t, src, 200, 200)
	if c := img.RGBAAt(50, 50); !isGreen(c) {
		t.Errorf("pixel (50,50) = %v, want the lime participant in front of the camera", c)
	}
}
