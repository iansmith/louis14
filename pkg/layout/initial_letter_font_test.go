package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// TestApplyInitialLetter_RebuildsFont (LOU-428): the cascade now builds a
// ::first-letter style's font at its declared font-size, so when
// applyInitialLetter replaces that size with the initial-letter size it must
// re-build the font, or the stale used size and metrics win over the new
// font-size. Blink gives the initial-letter text a new Font at the adjusted
// size with the same FontSelector (ComputeInitialLetterFont /
// StyleForInitialLetterText, core/css/resolver/style_resolver.cc:3949/3996 @
// 5992657f556bcd9d207a4aca187488462f027cab). WPT: css-inline/initial-letter/
// initial-letter-drop-initial.html.
func TestApplyInitialLetter_RebuildsFont(t *testing.T) {
	fc := text.DefaultFontConfig()
	newStyle := func(decls map[string]string) *css.Style {
		s := css.NewStyle()
		for k, v := range decls {
			s.Set(k, v)
		}
		s.UpdateFont(text.NewCSSFontSelector(fc))
		s.UpdateLineHeight()
		return s
	}
	para := newStyle(map[string]string{"font-family": "Ahem", "font-size": "20px", "line-height": "24px"})
	fl := newStyle(map[string]string{"font-family": "Ahem", "font-size": "100px", "line-height": "50px", "initial-letter": "3 drop"})

	(&LayoutTreeBuilder{fontConfig: fc}).applyInitialLetter(fl, para)

	size := fl.GetFontSize()
	if size == 100 {
		t.Fatalf("font-size still 100px; applyInitialLetter did not size the letter")
	}
	if got := fl.GetUsedFontSize(); got != size {
		t.Errorf("used font-size = %v, want the initial-letter size %v", got, size)
	}
	if want := 0.8 * size; fl.CapHeight != want { // Ahem cap-height is 0.8em
		t.Errorf("cap-height = %v, want %v (Ahem 0.8em at %v)", fl.CapHeight, want, size)
	}
	if fl.LhSize != size {
		t.Errorf("lh size = %v, want %v (line-height 1em of the letter font)", fl.LhSize, size)
	}
}
