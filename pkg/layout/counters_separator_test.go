package layout

import (
	"strings"
	"testing"
)

// counters() joins every level, outermost first, with the separator, even
// when the separator is the empty string: only counter() takes a single
// value (LayoutCounter::GenerateCounterText joins whenever the separator is
// non-null, core/layout/layout_counter.cc:82-95 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Mirrors WPT
// css-contain/contain-style-counters-005.
func TestCountersEmptySeparatorJoinsAllLevels(t *testing.T) {
	// body resets c and the inner .reset (a descendant) nests a second c.
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><style>
		body, .reset { counter-reset: c; }
		.increment { counter-increment: c; }
		.increment::before { content: counters(c, ""); }
		.dot::before { content: counters(c, "."); }
		.one::before { content: counter(c); }
	</style><body>
		<div class="increment"></div>
		<div class="reset"><div class="increment"></div><div class="increment dot"></div><div class="increment one"></div></div>
	</body>`, nil)
	want := []string{"1", "11", "1.2", "3"}
	if got := pseudoTexts(fl.root, "::before"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("::before texts %v, want %v", got, want)
	}
}
