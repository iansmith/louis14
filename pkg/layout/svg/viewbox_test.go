package svg

import (
	"math"
	"testing"

	"louis14/pkg/geometry"
)

const eps = 1e-9

func TestParseViewBox(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want [4]float64
		ok   bool
	}{
		{"basic spaces", "0 0 100 100", [4]float64{0, 0, 100, 100}, true},
		{"commas", "0, 0, 200, 150", [4]float64{0, 0, 200, 150}, true},
		{"negatives_origin", "-10 -20 100 100", [4]float64{-10, -20, 100, 100}, true},
		{"decimals", "0 0 12.5 7.5", [4]float64{0, 0, 12.5, 7.5}, true},
		{"too_few", "0 0 100", [4]float64{}, false},
		{"too_many", "0 0 100 100 10", [4]float64{}, false},
		{"zero_width", "0 0 0 100", [4]float64{}, false},
		{"zero_height", "0 0 100 0", [4]float64{}, false},
		{"negative_width", "0 0 -100 100", [4]float64{}, false},
		{"garbage", "abc", [4]float64{}, false},
		{"empty", "", [4]float64{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, w, h, ok := ParseViewBox(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			got := [4]float64{x, y, w, h}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParsePreserveAspectRatio_Defaults(t *testing.T) {
	// Empty / unknown → default xMidYMid meet.
	for _, in := range []string{"", "bogus", "xMidYMid"} {
		got := ParsePreserveAspectRatio(in)
		if in == "" || in == "bogus" {
			if got.Align != AlignXMidYMid || got.MeetOrSlice != MeetOrSliceMeet {
				t.Errorf("ParsePreserveAspectRatio(%q) = %+v, want default", in, got)
			}
		}
	}
}

func TestParsePreserveAspectRatio_Slice(t *testing.T) {
	got := ParsePreserveAspectRatio("xMinYMax slice")
	if got.Align != AlignXMinYMax {
		t.Errorf("align = %v, want xMinYMax", got.Align)
	}
	if got.MeetOrSlice != MeetOrSliceSlice {
		t.Errorf("meetOrSlice = %v, want slice", got.MeetOrSlice)
	}
}

func TestParsePreserveAspectRatio_None(t *testing.T) {
	got := ParsePreserveAspectRatio("none")
	if got.Align != AlignNone {
		t.Errorf("align = %v, want none", got.Align)
	}
}

func TestParsePreserveAspectRatio_DeferPrefix(t *testing.T) {
	got := ParsePreserveAspectRatio("defer xMaxYMid slice")
	if got.Align != AlignXMaxYMid || got.MeetOrSlice != MeetOrSliceSlice {
		t.Errorf("got %+v, want xMaxYMid slice (defer ignored)", got)
	}
}

// TestBuildViewBoxToViewportTransform_NoViewBox: invalid viewBox → identity.
func TestBuildViewBoxToViewportTransform_NoViewBox(t *testing.T) {
	tr := BuildViewBoxToViewportTransform(
		ViewBox{},
		geometry.NewRectF(0, 0, 100, 50),
		DefaultPreserveAspectRatio(),
	)
	if !tr.IsIdentity() {
		t.Errorf("no viewBox → expected identity, got %+v", tr)
	}
}

// TestBuildViewBoxToViewportTransform_None: anisotropic stretch.
func TestBuildViewBoxToViewportTransform_None(t *testing.T) {
	tr := BuildViewBoxToViewportTransform(
		ViewBox{X: 0, Y: 0, Width: 10, Height: 10, Valid: true},
		geometry.NewRectF(0, 0, 100, 200),
		PreserveAspectRatio{Align: AlignNone},
	)
	// scaleX=10, scaleY=20. point (5,5) → (50, 100).
	p := tr.MapPoint(geometry.PointF{X: 5, Y: 5})
	if math.Abs(p.X-50) > eps || math.Abs(p.Y-100) > eps {
		t.Errorf("none.MapPoint(5,5) = %+v, want (50,100)", p)
	}
}

// TestBuildViewBoxToViewportTransform_MidMeet: default xMidYMid meet.
// viewBox 0 0 100 100 → viewport 200×100 with meet:
//   - scale = min(200/100, 100/100) = 1
//   - scaled size 100×100, viewport 200×100 → tx = (200-100)/2 = 50, ty = 0
func TestBuildViewBoxToViewportTransform_MidMeet(t *testing.T) {
	tr := BuildViewBoxToViewportTransform(
		ViewBox{X: 0, Y: 0, Width: 100, Height: 100, Valid: true},
		geometry.NewRectF(0, 0, 200, 100),
		DefaultPreserveAspectRatio(),
	)
	// (0,0) viewBox → (50, 0) viewport.
	p := tr.MapPoint(geometry.PointF{X: 0, Y: 0})
	if math.Abs(p.X-50) > eps || math.Abs(p.Y-0) > eps {
		t.Errorf("midMeet.MapPoint(0,0) = %+v, want (50,0)", p)
	}
	// (100,100) viewBox → (150, 100) viewport.
	q := tr.MapPoint(geometry.PointF{X: 100, Y: 100})
	if math.Abs(q.X-150) > eps || math.Abs(q.Y-100) > eps {
		t.Errorf("midMeet.MapPoint(100,100) = %+v, want (150,100)", q)
	}
}

// TestBuildViewBoxToViewportTransform_MaxSlice: slice + xMaxYMax.
// viewBox 0 0 100 100 → viewport 200×100 with slice:
//   - scale = max(2, 1) = 2
//   - scaled size 200×200, viewport 200×100 → tx = 200 - 200 = 0
//     ty = 100 - 200 = -100 (for YMax: top-aligned at bottom)
func TestBuildViewBoxToViewportTransform_MaxSlice(t *testing.T) {
	tr := BuildViewBoxToViewportTransform(
		ViewBox{X: 0, Y: 0, Width: 100, Height: 100, Valid: true},
		geometry.NewRectF(0, 0, 200, 100),
		PreserveAspectRatio{Align: AlignXMaxYMax, MeetOrSlice: MeetOrSliceSlice},
	)
	// scale = 2, so viewBox (50, 100) → viewport (100, 100).
	// origin offset: tx = (200 - 200) = 0; ty = (100 - 200) = -100.
	// viewBox (0,0) → (0, -100). viewBox (100,100) → (200, 100).
	p := tr.MapPoint(geometry.PointF{X: 0, Y: 0})
	if math.Abs(p.X-0) > eps || math.Abs(p.Y-(-100)) > eps {
		t.Errorf("maxSlice.MapPoint(0,0) = %+v, want (0,-100)", p)
	}
	q := tr.MapPoint(geometry.PointF{X: 100, Y: 100})
	if math.Abs(q.X-200) > eps || math.Abs(q.Y-100) > eps {
		t.Errorf("maxSlice.MapPoint(100,100) = %+v, want (200,100)", q)
	}
}

func TestSVGLengthContext_Px(t *testing.T) {
	c := NewSVGLengthContext(geometry.SizeF{Width: 200, Height: 100})
	if got := c.Resolve("42", LengthModeWidth); got != 42 {
		t.Errorf("Resolve('42') = %v, want 42", got)
	}
	if got := c.Resolve("42px", LengthModeHeight); got != 42 {
		t.Errorf("Resolve('42px') = %v, want 42", got)
	}
	if got := c.Resolve("-7.5", LengthModeOther); got != -7.5 {
		t.Errorf("Resolve('-7.5') = %v, want -7.5", got)
	}
}

func TestSVGLengthContext_Percent(t *testing.T) {
	c := NewSVGLengthContext(geometry.SizeF{Width: 200, Height: 100})
	if got := c.Resolve("50%", LengthModeWidth); math.Abs(got-100) > eps {
		t.Errorf("50%% width = %v, want 100", got)
	}
	if got := c.Resolve("50%", LengthModeHeight); math.Abs(got-50) > eps {
		t.Errorf("50%% height = %v, want 50", got)
	}
	// Other: sqrt((200² + 100²)/2) = sqrt(50000/2 + 5000) =
	// sqrt(25000) ≈ 158.11... actually (200² + 100²)/2 = 50000/2 + 10000/2 = 25000+5000 = wait:
	// 200² = 40000; 100² = 10000; sum = 50000; /2 = 25000; sqrt = ~158.1138.
	want := 0.5 * math.Sqrt((200.0*200.0+100.0*100.0)/2.0)
	if got := c.Resolve("50%", LengthModeOther); math.Abs(got-want) > 1e-6 {
		t.Errorf("50%% other = %v, want %v", got, want)
	}
}

func TestSVGLengthContext_Empty(t *testing.T) {
	c := NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 50})
	if got := c.Resolve("", LengthModeWidth); got != 0 {
		t.Errorf("Resolve('') = %v, want 0", got)
	}
	if got := c.Resolve("notanumber", LengthModeWidth); got != 0 {
		t.Errorf("Resolve('notanumber') = %v, want 0", got)
	}
}

// TestBuildSVGRoot_NoViewBox: an <svg> without viewBox should produce an
// identity local-to-border-box transform.
func TestBuildSVGRoot_NoViewBox(t *testing.T) {
	root := BuildSVGRoot(&fakeAdapter{tag: "svg"}, geometry.SizeF{Width: 200, Height: 100}, nil)
	if !root.LocalToBorderBoxTransform.IsIdentity() {
		t.Errorf("no-viewBox root transform = %+v, want identity", root.LocalToBorderBoxTransform)
	}
	if root.ViewBox.Valid {
		t.Errorf("ViewBox.Valid = true, want false (none parsed)")
	}
}

// TestBuildSVGRoot_WithViewBox: viewBox attribute is parsed and feeds the
// local-to-border-box transform.
func TestBuildSVGRoot_WithViewBox(t *testing.T) {
	root := BuildSVGRoot(
		&fakeAdapter{tag: "svg", attrs: map[string]string{"viewbox": "0 0 100 100"}},
		geometry.SizeF{Width: 200, Height: 200},
		nil,
	)
	if !root.ViewBox.Valid {
		t.Fatal("ViewBox.Valid = false, want true")
	}
	// scale = 2; viewBox (0,0) → viewport (0,0).
	p := root.LocalToBorderBoxTransform.MapPoint(geometry.PointF{X: 50, Y: 50})
	if math.Abs(p.X-100) > eps || math.Abs(p.Y-100) > eps {
		t.Errorf("LocalToBorderBoxTransform(50,50) = %+v, want (100,100)", p)
	}
}

// TestBuildSVGRoot_TreeStructure: Phase 2 routes the seven shape tags
// to SVGShape and keeps SVGContainer as the recursive fallback for
// other tags (e.g. <g>, until Phase 3).
func TestBuildSVGRoot_TreeStructure(t *testing.T) {
	rect := &fakeAdapter{tag: "rect"}
	g := &fakeAdapter{tag: "g", children: []*fakeAdapter{rect}}
	svgElt := &fakeAdapter{tag: "svg", children: []*fakeAdapter{g}}
	root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 100, Height: 100}, nil)
	if len(root.Children) != 1 {
		t.Fatalf("root.Children len = %d, want 1", len(root.Children))
	}
	gNode, ok := root.Children[0].(*SVGContainer)
	if !ok {
		t.Fatalf("root.Children[0] type = %T, want *SVGContainer", root.Children[0])
	}
	if gNode.TagName != "g" {
		t.Errorf("g container TagName = %q, want %q", gNode.TagName, "g")
	}
	if len(gNode.Children) != 1 {
		t.Fatalf("g.Children len = %d, want 1", len(gNode.Children))
	}
	// Phase 2: <rect> is an SVGShape now, not a stub container.
	rNode, ok := gNode.Children[0].(*SVGShape)
	if !ok {
		t.Fatalf("rect node type = %T, want *SVGShape (Phase 2 shape)", gNode.Children[0])
	}
	if rNode.TagName != "rect" {
		t.Errorf("rect shape TagName = %q, want %q", rNode.TagName, "rect")
	}
	// UpdateSVGLayout walks the subtree without panicking.
	res := root.UpdateSVGLayout(SVGLayoutInfo{ForceLayout: true})
	if res.BoundsChanged || res.HasViewportDependence {
		t.Errorf("Phase 2 layout reported non-empty result: %+v", res)
	}
	// Paint on the root is a no-op; the renderer drives shape paint via
	// pkg/render/svg_root_painter.go. Make sure root.Paint doesn't crash.
	root.Paint(&SVGPaintContext{})
}

// fakeAdapter is a minimal ElementAdapter for unit tests that doesn't
// pull in the full html.Node machinery.
type fakeAdapter struct {
	tag      string
	attrs    map[string]string
	children []*fakeAdapter
	text     string
}

func (f *fakeAdapter) TagName() string { return f.tag }
func (f *fakeAdapter) Attribute(name string) (string, bool) {
	if f.attrs == nil {
		return "", false
	}
	v, ok := f.attrs[name]
	return v, ok
}
func (f *fakeAdapter) SVGChildren() []ElementAdapter {
	out := make([]ElementAdapter, 0, len(f.children))
	for _, c := range f.children {
		out = append(out, c)
	}
	return out
}
func (f *fakeAdapter) TextContent() string { return f.text }
