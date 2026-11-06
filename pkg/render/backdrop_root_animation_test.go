package render_test

import (
	"image/color"
	"testing"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, animatedBackdropRootPage(tc.keyframes, tc.animation), 120, 120)
			wantPixel(t, img, 50, 50, tc.want, "the box's backdrop, inverted")
		})
	}
}
