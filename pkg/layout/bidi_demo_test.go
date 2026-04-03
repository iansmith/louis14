package layout

import (
	"fmt"
	"testing"

	"golang.org/x/text/unicode/bidi"
)

func applyBidiVisualOrderForTest(text string, defaultDir bidi.Direction) string {
	var p bidi.Paragraph
	p.SetString(text, bidi.DefaultDirection(defaultDir))
	o, err := p.Order()
	if err != nil {
		return text
	}

	if o.Direction() == bidi.LeftToRight {
		return text
	}

	// RTL paragraph: reorder runs in reverse and mirror RTL runs
	result := make([]rune, 0, len([]rune(text)))
	for i := o.NumRuns() - 1; i >= 0; i-- {
		run := o.Run(i)
		runes := []rune(run.String())
		if run.Direction() == bidi.RightToLeft {
			// Reverse and mirror brackets
			for j, k := 0, len(runes)-1; j <= k; j, k = j+1, k-1 {
				rj, rk := runes[j], runes[k]
				if m := bidiMirror(rj); m != rj {
					rj = m
				}
				if m := bidiMirror(rk); m != rk {
					rk = m
				}
				runes[j], runes[k] = rk, rj
			}
		}
		result = append(result, runes...)
	}
	return string(result)
}

func TestBidiDemo(t *testing.T) {
	text := "> ab > \u05d2\u05d3 > ef >"
	fmt.Printf("Input: %q\n", text)

	var p bidi.Paragraph
	p.SetString(text, bidi.DefaultDirection(bidi.RightToLeft))

	o, err := p.Order()
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	fmt.Printf("Direction: %v\n", o.Direction())
	fmt.Printf("NumRuns: %d\n", o.NumRuns())
	for i := 0; i < o.NumRuns(); i++ {
		r := o.Run(i)
		start, end := r.Pos()
		fmt.Printf("Run %d: dir=%v, text=%q, pos=%d-%d\n", i, r.Direction(), r.String(), start, end)
	}

	visual := applyBidiVisualOrderForTest(text, bidi.RightToLeft)
	fmt.Printf("Visual order: %q\n", visual)
}
