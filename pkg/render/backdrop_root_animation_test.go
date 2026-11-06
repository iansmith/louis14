package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// LOU-742 (filter-effects worklist B7): an element with a current or
// in-effect animation of opacity, filter or backdrop-filter is a backdrop
// root, whatever value the animation gives. Blink flags the style
// (CSSAnimations::UpdateAnimationFlags: IsCurrent() || IsInEffect(),
// css_animations.cc; timing.cc:258-269 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), the flag is a compositing reason
// (compositing_reason_finder.cc:576-588 @ a9f50e52), and the effect node it
// makes is a render surface, which bounds a descendant's backdrop.

// animatedBackdropRootPage puts a backdrop-filter:invert(1) box inside a
// container animated with animation, over a black page. Inside a backdrop
// root the box's backdrop is the container's transparent content and stays
// black; otherwise it is the black page, inverted to white.
func animatedBackdropRootPage(keyframes, animation string) string {
	return `<!DOCTYPE html><style>body{margin:0;background:black}
@keyframes k {` + keyframes + `}
.c{width:100px;height:100px;animation:` + animation + `}
.f{width:100%;height:100%;backdrop-filter:invert(1)}</style>
<div class="c"><div class="f"></div></div>`
}

func TestBackdropRoot_Animation(t *testing.T) {
	for _, tc := range []struct {
		name, keyframes, animation string
		want                       color.RGBA
	}{
		// backdrop-filter-backdrop-root-animation-in-effect: finished at
		// opacity 1, held by fill-mode forwards (in effect).
		{"finished opacity, forwards", "0%{opacity:0} 100%{opacity:1}", "k 1s -2s forwards", black},
		// Active (in play), at a value that is no backdrop root on its own.
		{"active filter at none", "from{filter:none} to{filter:none}", "k 10s", black},
		{"active backdrop-filter at none", "from{backdrop-filter:none} to{backdrop-filter:none}", "k 10s", black},
		// Before its delay: current, though not in effect.
		{"delayed opacity", "0%{opacity:1} 100%{opacity:1}", "k 1s 10s", black},
		// Finished without a fill: neither current nor in effect.
		{"finished opacity, no fill", "0%{opacity:0} 100%{opacity:1}", "k 1s -2s", white},
		// A transform animation makes a stacking context, not an effect node
		// (kDirectReasonsForEffectProperty / kDirectReasonsForFilterProperty,
		// compositing_reasons.h:164-175 @ a9f50e52): no backdrop root.
		{"active transform at none", "from{transform:none} to{transform:none}", "k 10s", white},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, animatedBackdropRootPage(tc.keyframes, tc.animation), 120, 120)
			wantPixel(t, img, 50, 50, tc.want, "the box's backdrop, inverted")
		})
	}
}

// ShouldCompositeForCurrentAnimations makes a stacking context
// (CalculateIsStackingContextWithoutContainment, computed_style.cc:2941-2945
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), transform-related animations
// included, and keyframe properties are matched after alias resolution
// (css_properties.json5 alias_for @ a9f50e52): the z-index:-1 child paints
// above the animated element's green background.
func TestStackingContext_CurrentAnimation(t *testing.T) {
	for _, tc := range []struct{ name, keyframes string }{
		{"transform", "from{transform:none} to{transform:none}"},
		{"translate", "from{translate:none} to{translate:none}"},
		{"rotate", "from{rotate:none} to{rotate:none}"},
		{"scale", "from{scale:none} to{scale:none}"},
		{"-webkit-transform", "from{-webkit-transform:none} to{-webkit-transform:none}"},
		{"-webkit-filter", "from{-webkit-filter:none} to{-webkit-filter:none}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}
@keyframes k {`+tc.keyframes+`}
div{width:100px;height:100px}</style>
<div style="background:green;animation:k 10s"><div style="position:relative;z-index:-1;background:red"></div></div>`, 120, 120)
			wantPixel(t, img, 50, 50, canvasRed, "the z-index:-1 child inside the animated stacking context")
		})
	}
}

// A relatively positioned span's background fragment is not an element: its
// style is the span's with position static, so it paints in flow order under
// the span's text, and it carries none of the span's current-animation
// compositing reasons either, or it would be a stacking context of its own.
// The pixel symptom (the background over the text) is not reachable while
// the fragment paints inside the span's own paint group, so this checks the
// fragment itself.
func TestStackingContext_AnimatedSpanBackgroundFragmentIsNoStackingContext(t *testing.T) {
	doc, err := html.Parse(`<!DOCTYPE html><style>@keyframes k {0%{opacity:1} 100%{opacity:1}}</style>
<span style="position:relative;background:grey;animation:k 1s -2s forwards">WWW</span>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var bg *layout.Box
	for _, root := range layout.NewLayoutEngine(200, 100).Layout(doc) {
		if bg = findSpanBackgroundFragment(root); bg != nil {
			break
		}
	}
	if bg == nil {
		t.Fatal("no background fragment for the span")
	}
	if bg.CreatesStackingContext() {
		t.Error("the span's background fragment creates a stacking context")
	}
}

// findSpanBackgroundFragment returns the first background fragment of a
// relatively positioned span: a <span> box with no text whose style has
// position static.
func findSpanBackgroundFragment(b *layout.Box) *layout.Box {
	if b.Node != nil && b.Node.TagName == "span" && b.Text == "" && b.Style != nil && b.Style.GetPosition() == css.PositionStatic {
		return b
	}
	for _, c := range b.Children {
		if f := findSpanBackgroundFragment(c); f != nil {
			return f
		}
	}
	return nil
}
