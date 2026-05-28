package css

import (
	"testing"

	"louis14/pkg/html"
)

// divVW returns the width that the div in `htmlSrc` resolves to after
// the cascade is applied at viewport (vw, vh). The div's CSS must set
// `width: 100vw` so the resolved value reflects how vw is computed.
func divVW(t *testing.T, htmlSrc string, vw, vh float64) float64 {
	t.Helper()
	doc, _ := html.Parse(htmlSrc)
	styles := ApplyStylesToDocument(doc, vw, vh)
	for node, style := range styles {
		if node.Type == html.ElementNode && node.TagName == "div" {
			ctx := calcContext{
				fontSize:       style.GetFontSize(),
				percentBase:    vw,
				viewportWidth:  style.ViewportWidth,
				viewportHeight: style.ViewportHeight,
				chScale:        style.chScale(),
			}
			val, ok := style.Get("width")
			if !ok {
				continue
			}
			if length, ok := parseLengthFullWithCh(val, ctx.fontSize, ctx.viewportWidth, ctx.viewportHeight, ctx.chScale, style.XHeight, style.CapHeight, style.LhSize); ok {
				return length
			}
		}
	}
	t.Fatalf("no <div> found or width unset")
	return 0
}

// TestEffectiveViewportForUnits_NoScrollbar verifies that vw resolves
// against the raw viewport when the root <html> has no unconditional
// scrollbars (overflow: visible / hidden, no oversized dimensions).
func TestEffectiveViewportForUnits_NoScrollbar(t *testing.T) {
	got := divVW(t, `
		<style>div { width: 100vw; }</style>
		<div></div>
	`, 800, 600)
	if got != 800 {
		t.Errorf("100vw on viewport 800 with no scrollbar: got %v, want 800", got)
	}
}

// TestEffectiveViewportForUnits_OverflowScroll verifies that vw is
// reduced by the classic scrollbar width (17 px) when the root <html>
// has overflow:scroll. Mirrors Blink's small-viewport vw resolution at
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestEffectiveViewportForUnits_OverflowScroll(t *testing.T) {
	got := divVW(t, `
		<html style="overflow: scroll;">
		<style>div { width: 100vw; }</style>
		<body><div></div></body>
		</html>
	`, 800, 600)
	if got != 783 {
		t.Errorf("100vw with overflow:scroll on root: got %v, want 783 (= 800 - 17)", got)
	}
}

// TestEffectiveViewportForUnits_OverflowAutoOversized verifies that vw is
// reduced when the root <html> has overflow:auto AND a declared width
// larger than the viewport (e.g. width: 200%), causing unconditional
// scrollbar reservation.
func TestEffectiveViewportForUnits_OverflowAutoOversized(t *testing.T) {
	got := divVW(t, `
		<html style="overflow: auto; width: 200%;">
		<style>div { width: 100vw; }</style>
		<body><div></div></body>
		</html>
	`, 800, 600)
	if got != 783 {
		t.Errorf("100vw with overflow:auto + width:200%% on root: got %v, want 783 (= 800 - 17)", got)
	}
}

// TestEffectiveViewportForUnits_OverflowAutoNotOversized verifies that vw
// is NOT reduced when the root has overflow:auto but no overflow trigger
// (auto reserves only when content actually overflows).
func TestEffectiveViewportForUnits_OverflowAutoNotOversized(t *testing.T) {
	got := divVW(t, `
		<html style="overflow: auto;">
		<style>div { width: 100vw; }</style>
		<body><div></div></body>
		</html>
	`, 800, 600)
	if got != 800 {
		t.Errorf("100vw with overflow:auto (not overflowing) on root: got %v, want 800", got)
	}
}

// TestEffectiveViewportForUnits_ScrollbarWidthNone verifies that vw is
// NOT reduced when the root has scrollbar-width:none, even with
// overflow:scroll — scrollbar reservation is 0 px so no reduction.
func TestEffectiveViewportForUnits_ScrollbarWidthNone(t *testing.T) {
	got := divVW(t, `
		<html style="overflow: scroll; scrollbar-width: none;">
		<style>div { width: 100vw; }</style>
		<body><div></div></body>
		</html>
	`, 800, 600)
	if got != 800 {
		t.Errorf("100vw with scrollbar-width:none: got %v, want 800", got)
	}
}

// TestEffectiveViewportForUnits_ScrollbarWidthThin verifies that vw is
// reduced by 10 px when scrollbar-width:thin is set with overflow:scroll.
func TestEffectiveViewportForUnits_ScrollbarWidthThin(t *testing.T) {
	got := divVW(t, `
		<html style="overflow: scroll; scrollbar-width: thin;">
		<style>div { width: 100vw; }</style>
		<body><div></div></body>
		</html>
	`, 800, 600)
	if got != 790 {
		t.Errorf("100vw with scrollbar-width:thin: got %v, want 790 (= 800 - 10)", got)
	}
}
