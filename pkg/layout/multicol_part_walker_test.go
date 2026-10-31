package layout

import "testing"

// walkerSpannerFixture builds content > [s1, " ", s2, block, s3] with parent
// links, where s1, s2 and s3 are column spanners.
func walkerSpannerFixture() (content, s1, s2, block, s3 *LayoutInputNode) {
	spanner := func() *LayoutInputNode {
		return &LayoutInputNode{style: makeStyle("display", "block", "column-span", "all")}
	}
	s1, s2, s3 = spanner(), spanner(), spanner()
	block = &LayoutInputNode{style: makeStyle("display", "block")}
	text := &LayoutInputNode{DOMNode: makeTextNode(" ")}
	content = &LayoutInputNode{children: []*LayoutInputNode{s1, text, s2, block, s3}, isAnonymous: true}
	linkLayoutParents(content)
	return content, s1, s2, block, s3
}

// TestMulticolPartWalker_MoveToSpannerWalksSiblingSpanners: after
// MoveToSpanner the walker visits the spanner, then each directly following
// sibling spanner (text skipped), then the stored column token, and stops
// at the first non-spanner sibling. Mirrors MulticolPartWalker::MoveToNext
// (column_layout_algorithm.cc:194-222 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestMulticolPartWalker_MoveToSpannerWalksSiblingSpanners(t *testing.T) {
	content, s1, s2, _, _ := walkerSpannerFixture()
	token := &BlockBreakToken{Node: content}

	w := NewMulticolPartWalker(content, nil)
	w.MoveToSpanner(s1, token)
	for i, want := range []MulticolPartWalkerEntry{
		{DescendantNode: s1},
		{DescendantNode: s2},
		{BreakToken: token},
	} {
		if w.IsFinished() {
			t.Fatalf("entry %d: walker finished early", i)
		}
		if got := w.Current(); got != want {
			t.Fatalf("entry %d: Current() = %+v, want %+v", i, got, want)
		}
		w.Next()
	}
	if !w.IsFinished() {
		t.Fatalf("walker not finished after the column token; Current() = %+v", w.Current())
	}
}

// TestMulticolPartWalker_LastSpannerWithoutColumnTokenFinishes: a spanner
// with no following sibling spanner and no column token to resume ends the
// walk.
func TestMulticolPartWalker_LastSpannerWithoutColumnTokenFinishes(t *testing.T) {
	content, _, _, _, s3 := walkerSpannerFixture()

	w := NewMulticolPartWalker(content, nil)
	w.MoveToSpanner(s3, nil)
	if got := w.Current(); got.DescendantNode != s3 || got.BreakToken != nil {
		t.Fatalf("Current() = %+v, want spanner s3", got)
	}
	w.Next()
	if !w.IsFinished() {
		t.Fatalf("walker not finished; Current() = %+v", w.Current())
	}
}
