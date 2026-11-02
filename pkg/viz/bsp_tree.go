package viz

// BspNode is a node of a BspTree: its splitting polygon, the polygons
// coplanar with it on either side, and its subtrees. Mirrors viz::BspNode
// (components/viz/service/display/bsp_tree.h:20-31 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BspNode struct {
	NodeData       *DrawPolygon
	CoplanarsFront []*DrawPolygon
	CoplanarsBack  []*DrawPolygon
	BackChild      *BspNode
	FrontChild     *BspNode
}

// BspCompareResult mirrors viz::BspCompareResult (bsp_compare_result.h).
type BspCompareResult int

const (
	BspFront BspCompareResult = iota
	BspBack
	BspSplit
	BspCoplanarFront
	BspCoplanarBack
	BspCoplanar
)

// BspTree sorts a 3D rendering context's polygons for back-to-front
// drawing, splitting the ones that cross another's plane. Mirrors
// viz::BspTree (bsp_tree.{h,cc} @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BspTree struct {
	root *BspNode
}

// NewBspTree builds the tree from polygons, taking the first as the root's
// splitting plane (BspTree::BspTree, bsp_tree.cc:22-28). Like viz, it expects
// every polygon to have vertices and a normal; DirectRenderer drops the
// others before building (direct_renderer.cc:829-836).
func NewBspTree(polygons []*DrawPolygon) *BspTree {
	return &BspTree{root: newSubtree(polygons)}
}

// newSubtree makes a node split by the first polygon and builds it from the
// rest; nil for no polygons.
func newSubtree(polygons []*DrawPolygon) *BspNode {
	if len(polygons) == 0 {
		return nil
	}
	node := &BspNode{NodeData: polygons[0]}
	buildTree(node, polygons[1:])
	return node
}

// Root returns the tree's root, nil for an empty tree.
func (t *BspTree) Root() *BspNode { return t.root }

// buildTree classifies polygons against node's plane into its coplanar
// lists and front and back subtrees, each subtree split by its first
// polygon (BspTree::BuildTree, bsp_tree.cc:38-82).
func buildTree(node *BspNode, polygons []*DrawPolygon) {
	var frontList, backList []*DrawPolygon
	for _, polygon := range polygons {
		front, back, isCoplanar := node.NodeData.SplitPolygon(polygon)
		if isCoplanar {
			if front != nil {
				node.CoplanarsFront = append(node.CoplanarsFront, front)
			}
			if back != nil {
				node.CoplanarsBack = append(node.CoplanarsBack, back)
			}
			continue
		}
		if front != nil {
			frontList = append(frontList, front)
		}
		if back != nil {
			backList = append(backList, back)
		}
	}
	node.BackChild = newSubtree(backList)
	node.FrontChild = newSubtree(frontList)
}

// TraverseWithActionHandler calls action on every polygon back to front
// (BspTree::TraverseWithActionHandler).
func (t *BspTree) TraverseWithActionHandler(action func(*DrawPolygon)) {
	walkInOrderRecursion(action, t.root)
}

// walkInOrderRecursion walks the side of node's plane away from the viewer
// first. Coplanar polygons paint in document order whichever way they
// face, as css-transforms orders them by paint order alone
// (BspTree::WalkInOrderRecursion, bsp_tree.h:93-109).
func walkInOrderRecursion(action func(*DrawPolygon), node *BspNode) {
	if node == nil {
		return
	}
	first, second := node.FrontChild, node.BackChild
	if getCameraPositionRelative(node.NodeData) == BspFront {
		first, second = node.BackChild, node.FrontChild
	}
	walkInOrderRecursion(action, first)
	for _, p := range node.CoplanarsBack {
		action(p)
	}
	action(node.NodeData)
	for _, p := range node.CoplanarsFront {
		action(p)
	}
	walkInOrderRecursion(action, second)
}

// getCameraPositionRelative reports whether the viewer, at the origin
// looking down -z, is in front of or behind node's plane
// (BspTree::GetCameraPositionRelative, bsp_tree.cc:85-90).
func getCameraPositionRelative(node *DrawPolygon) BspCompareResult {
	if node.normal.Z > 0 {
		return BspFront
	}
	return BspBack
}
