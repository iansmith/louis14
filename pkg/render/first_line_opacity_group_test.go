package render

import "testing"

// LOU-305: fractional opacity on ::first-line is GROUP opacity over the whole
// first line, exactly like element opacity (CSS Color 3 §3.2). ::first-line has
// no DOM node — Blink wraps first-line content in an anonymous LayoutInline
// (LayoutBlockFlow::NeedsAnonymousInlineWrapper, computed_style.cc:369 @
// 4883d11f), so first-line opacity rides on that wrapper and groups through the
// ordinary element-opacity path. louis14 merges first-line style onto each
// fragment instead of wrapping, so without a synthetic per-line group every
// first-line fragment (the ::first-line background band AND each first-line text
// run) carries the alpha independently and composites it more than once — the
// double-blend the ticket reports (overlap renders rgb(64,64,255) instead of the
// correct single-group rgb(128,128,255)).
//
// Contract: exactly ONE opacity group layer for the first line, carrying the
// ::first-line alpha, with the band and first-line text as Opacity-1 members.
func TestPaintTree_FirstLineOpacity_SingleGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<p style="font: 30px monospace; width: 200px; margin: 0">AAAA BBBB CCCC DDDD EEEE</p>
<style>p::first-line { opacity: 0.5; background: blue; color: blue }</style>`)

	groups := countOpacityGroups(root)
	if groups != 1 {
		t.Errorf("expected exactly 1 opacity group layer for the ::first-line group, got %d "+
			"(each first-line fragment applies the 0.5 alpha independently → double-blend)", groups)
	}

	var groupOpacity float64
	forEachLayer(root, nil, func(l *PaintLayer, _ []*PaintLayer) {
		if l.Opacity < 1.0 {
			groupOpacity = l.Opacity
		}
	})
	if groups == 1 && groupOpacity != 0.5 {
		t.Errorf("first-line group opacity = %v, want 0.5", groupOpacity)
	}
}
