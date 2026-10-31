package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// TestMergeFirstLineStyle_RebuildsFont (LOU-428 review): base.Clone() carries
// the base font's used size and metrics, which GetUsedFontSize and the
// ex / cap / lh resolvers read in preference to Properties, so when
// ::first-line overrides font-size the merge must re-build the font and lh
// size. Blink's ::first-line ComputedStyle is resolved through the same
// StyleResolverState::UpdateFont / UpdateLineHeight step
// (core/css/resolver/style_resolver_state.cc:553/565 @
// 5992657f556bcd9d207a4aca187488462f027cab). Ahem's x-height is 0.8em.
func TestMergeFirstLineStyle_RebuildsFont(t *testing.T) {
	fc := text.DefaultFontConfig()
	base := css.NewStyle()
	base.Set("font-family", "Ahem")
	base.Set("font-size", "20px")
	base.Set("line-height", "1")
	base.UpdateFont(text.NewCSSFontSelector(fc))
	base.UpdateLineHeight()
	firstLine := css.NewStyle()
	firstLine.Set("font-size", "40px")

	merged := mergeFirstLineStyle(base, firstLine, fc)

	if got := merged.GetUsedFontSize(); got != 40 {
		t.Errorf("used font-size = %v, want 40 (the ::first-line size)", got)
	}
	if want := 0.8 * 40; merged.XHeight != want {
		t.Errorf("x-height = %v, want %v (Ahem 0.8em at 40px)", merged.XHeight, want)
	}
	if merged.LhSize != 40 {
		t.Errorf("lh size = %v, want 40 (line-height 1 at 40px)", merged.LhSize)
	}
}
