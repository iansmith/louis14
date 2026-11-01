package layout

// LOU-542: the document element is laid out as an ordinary in-flow block
// child of the LayoutView, whose fixed size is the initial containing block
// (LayoutView::LayoutRoot, layout_view.cc:848-909 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): its margins offset and inset
// it, an auto block-size is its content's, and nothing stretches it to the
// ICB block-size.
//
// Expected geometry was measured in Chromium 152.0.7977.167
// (getBoundingClientRect of the same markup in an 800x600 borderless
// iframe) on 2026-11-01.

import (
	"testing"

	"louis14/pkg/html"
)

func layoutRootTestHTML(t *testing.T, src string) *Box {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	return NewLayoutEngine(800, 600).Layout(doc)[0]
}

// findBoxByID returns the first box under b whose DOM node has id.
func findBoxByID(b *Box, id string) *Box {
	if b.Node != nil {
		if v, ok := b.Node.GetAttribute("id"); ok && v == id {
			return b
		}
	}
	for _, c := range b.Children {
		if f := findBoxByID(c, id); f != nil {
			return f
		}
	}
	return nil
}

func TestLayoutView_RootIsInFlowBlockChildOfICB(t *testing.T) {
	type rect struct{ x, y, w, h float64 }
	cases := []struct {
		name       string
		src        string
		root, body rect
	}{
		{
			name: "explicit height and margins",
			src:  `<!doctype html><style>html{height:300px;margin:50px}</style><body></body>`,
			root: rect{50, 50, 700, 300},
			body: rect{58, 58, 684, 0},
		},
		{
			name: "auto height is content height",
			src:  `<!doctype html><style>html{margin:10px 20px}body{margin:8px}</style><div style="height:10px"></div>`,
			root: rect{20, 10, 760, 26},
			body: rect{28, 18, 744, 10},
		},
		{
			name: "auto inline margins center",
			src:  `<!doctype html><style>html{margin:0 auto;width:400px}</style><div style="height:10px"></div>`,
			root: rect{200, 0, 400, 26},
			body: rect{208, 8, 384, 10},
		},
		{
			name: "vertical-rl sits at block-start after its margin",
			src:  `<!doctype html><style>html{writing-mode:vertical-rl;margin:5px 30px 0 0}body{margin:0}</style><div style="width:10px"></div>`,
			root: rect{760, 5, 10, 595},
			body: rect{760, 5, 10, 595},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, tc.src)
			if got := (rect{root.X, root.Y, root.Width, root.Height}); got != tc.root {
				t.Errorf("root border box = %+v, want %+v", got, tc.root)
			}
			if len(root.Children) == 0 {
				t.Fatalf("root has no children")
			}
			body := root.Children[0]
			if got := (rect{body.X, body.Y, body.Width, body.Height}); got != tc.body {
				t.Errorf("body border box = %+v, want %+v", got, tc.body)
			}
		})
	}
}

// An absolutely positioned box with no positioned ancestor is placed in the
// initial containing block, not in the root's (margin-offset) box; its static
// position follows the root's margin.
func TestLayoutView_AbsposInICBWithRootMargins(t *testing.T) {
	root := layoutRootTestHTML(t, `<!doctype html><style>html{margin:50px}</style>`+
		`<div id=x style="position:absolute;top:0;left:0;width:10px;height:10px"></div>`+
		`<div id=y style="position:absolute;width:10px;height:10px"></div>`)
	for _, tc := range []struct {
		id   string
		x, y float64
	}{{"x", 0, 0}, {"y", 58, 58}} {
		b := findBoxByID(root, tc.id)
		if b == nil {
			t.Fatalf("no box for #%s", tc.id)
		}
		if b.X != tc.x || b.Y != tc.y {
			t.Errorf("#%s at (%v, %v), want (%v, %v)", tc.id, b.X, b.Y, tc.x, tc.y)
		}
	}
}
