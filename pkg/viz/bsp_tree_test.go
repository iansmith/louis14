package viz

import (
	"testing"
)

// Ported from components/viz/service/display/bsp_tree_unittest.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

func runBspTest(t *testing.T, polygons []*DrawPolygon, want []int) {
	t.Helper()
	tree := NewBspTree(polygons)
	var got []int
	tree.TraverseWithActionHandler(func(p *DrawPolygon) { got = append(got, p.OrderIndex()) })
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if !verifySidedness(tree.Root()) {
		t.Error("tree sidedness is inconsistent")
	}
}

func sideCompare(a, b *DrawPolygon) BspCompareResult {
	pos, neg := false, false
	for _, pt := range a.Points() {
		d := b.SignedPointDistance(pt)
		neg = neg || d < -splitThreshold
		pos = pos || d > splitThreshold
	}
	switch {
	case pos && neg:
		return BspSplit
	case neg:
		return BspBack
	case pos:
		return BspFront
	case a.OrderIndex() < b.OrderIndex():
		return BspCoplanarBack
	}
	return BspCoplanarFront
}

func verifySidedness(node *BspNode) bool {
	if node.BackChild != nil {
		if sideCompare(node.BackChild.NodeData, node.NodeData) != BspBack || !verifySidedness(node.BackChild) {
			return false
		}
	}
	if node.FrontChild != nil {
		if sideCompare(node.FrontChild.NodeData, node.NodeData) != BspFront || !verifySidedness(node.FrontChild) {
			return false
		}
	}
	for _, p := range node.CoplanarsFront {
		if sideCompare(p, node.NodeData) != BspCoplanarFront {
			return false
		}
	}
	for _, p := range node.CoplanarsBack {
		if sideCompare(p, node.NodeData) != BspCoplanarBack {
			return false
		}
	}
	return true
}

var (
	quadA = []p3{{X: -5, Y: -5, Z: 0}, {X: -5, Y: 5, Z: 0}, {X: 5, Y: 5, Z: 0}, {X: 5, Y: -5, Z: 0}}
	quadB = []p3{{X: 0, Y: -5, Z: -5}, {X: 0, Y: 5, Z: -5}, {X: 0, Y: 5, Z: 5}, {X: 0, Y: -5, Z: 5}}
)

func TestBspTree_NoSplit(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0),
		NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: -5}, {X: 0, Y: 0, Z: -5}, {X: 10, Y: 0, Z: -5}, {X: 10, Y: 10, Z: -5}}, v3{Z: 1}, 1),
		NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 5}, {X: 0, Y: 0, Z: 5}, {X: 10, Y: 0, Z: 5}, {X: 10, Y: 10, Z: 5}}, v3{Z: 1}, 2),
	}, []int{1, 0, 2})
}

func TestBspTree_BasicSplit(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, quadA, v3{Z: 1}, 0),
		NewDrawPolygon(nil, quadB, v3{X: -1}, 1),
	}, []int{1, 0, 1})
}

func TestBspTree_QuadOffset(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, quadA, v3{Z: 1}, 0),
		NewDrawPolygon(nil, []p3{{X: 0, Y: 5, Z: -15}, {X: 0, Y: -5, Z: -15}, {X: 0, Y: -5, Z: -10}, {X: 0, Y: 5, Z: -10}}, v3{X: -1}, 1),
	}, []int{1, 0})
}

func TestBspTree_QuadOffsetSplit(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, []p3{{X: 0, Y: -5, Z: -15}, {X: 0, Y: 5, Z: -15}, {X: 0, Y: 5, Z: -10}, {X: 0, Y: -5, Z: -10}}, v3{X: -1}, 1),
		NewDrawPolygon(nil, quadA, v3{Z: 1}, 0),
	}, []int{0, 1, 0})
}

func TestBspTree_ThreeWaySplit(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, quadA, v3{Z: 1}, 0),
		NewDrawPolygon(nil, quadB, v3{X: -1}, 1),
		NewDrawPolygon(nil, []p3{{X: -5, Y: 0, Z: -5}, {X: -5, Y: 0, Z: 5}, {X: 5, Y: 0, Z: 5}, {X: 5, Y: 0, Z: -5}}, v3{Y: 1}, 2),
	}, []int{2, 1, 2, 0, 2, 1, 2})
}

func reversed(points []p3) []p3 {
	out := make([]p3, len(points))
	for i, p := range points {
		out[len(points)-1-i] = p
	}
	return out
}

// Coplanar geometry comes back in document order whatever order it is
// inserted in and whichever way it faces.
func TestBspTree_Coplanar(t *testing.T) {
	va := quadA
	vb := []p3{{X: -4, Y: -4, Z: 0}, {X: -4, Y: 4, Z: 0}, {X: 4, Y: 4, Z: 0}, {X: 4, Y: -4, Z: 0}}
	vc := []p3{{X: -3, Y: -3, Z: 0}, {X: -3, Y: 3, Z: 0}, {X: 3, Y: 3, Z: 0}, {X: 3, Y: -3, Z: 0}}
	fwd := func(v []p3, id int) *DrawPolygon { return NewDrawPolygon(nil, v, v3{Z: 1}, id) }
	rev := func(v []p3, id int) *DrawPolygon { return NewDrawPolygon(nil, reversed(v), v3{Z: -1}, id) }
	for _, tc := range []struct {
		name     string
		polygons []*DrawPolygon
	}{
		{"in order", []*DrawPolygon{fwd(va, 0), fwd(vb, 1), fwd(vc, 2)}},
		{"shuffled", []*DrawPolygon{fwd(vc, 2), fwd(va, 0), fwd(vb, 1)}},
		{"reversed in order", []*DrawPolygon{rev(va, 0), rev(vb, 1), rev(vc, 2)}},
		{"reversed shuffled", []*DrawPolygon{rev(vc, 2), rev(va, 0), rev(vb, 1)}},
		{"middle reversed", []*DrawPolygon{fwd(va, 0), rev(vb, 1), fwd(vc, 2)}},
		{"middle reversed shuffled", []*DrawPolygon{fwd(va, 0), rev(vb, 1), fwd(vc, 2)}},
	} {
		t.Run(tc.name, func(t *testing.T) { runBspTest(t, tc.polygons, []int{0, 1, 2}) })
	}
}

// Coplanar geometry shares one node, and a crossing polygon splits into two
// pieces on either side of it.
func TestBspTree_CoplanarSplit(t *testing.T) {
	runBspTest(t, []*DrawPolygon{
		NewDrawPolygon(nil, quadA, v3{Z: 1}, 0),
		NewDrawPolygon(nil, []p3{{X: -4, Y: -4, Z: 0}, {X: -4, Y: 4, Z: 0}, {X: 4, Y: 4, Z: 0}, {X: 4, Y: -4, Z: 0}}, v3{Z: 1}, 1),
		NewDrawPolygon(nil, []p3{{X: -3, Y: -3, Z: 0}, {X: -3, Y: 3, Z: 0}, {X: 3, Y: 3, Z: 0}, {X: 3, Y: -3, Z: 0}}, v3{Z: 1}, 2),
		NewDrawPolygon(nil, []p3{{X: 0, Y: -15, Z: -15}, {X: 0, Y: 15, Z: -15}, {X: 0, Y: 15, Z: 15}, {X: 0, Y: -15, Z: 15}}, v3{X: -1}, 3),
	}, []int{3, 0, 1, 2, 3})
}
