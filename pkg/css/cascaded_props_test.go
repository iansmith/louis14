package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestComputeStyleRecordsCascadedProps: ComputeStyle must record which
// longhands the element's OWN cascade set (Style.CascadedProps) so that
// ::first-line application can distinguish a declared value from an
// inherited one even when the two are textually identical
// (WPT first-line-change-inline-color.html: span declares color:green while
// also inheriting green — LOU-179's value-comparison heuristic misses it).
// Mirrors the declaration re-application of Blink's
// kPseudoIdFirstLineInherited resolution (LayoutObject::
// FirstLineStyleWithoutFallback, layout_object.cc:4386-4404 @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5).
//
// RED before fix: CascadedProps is never populated.
func TestComputeStyleRecordsCascadedProps(t *testing.T) {
	sheet, err := ParseStylesheet(`.green { color: green; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	sheets := []*Stylesheet{sheet}

	declared := &html.Node{Type: html.ElementNode, TagName: "span",
		Attributes: map[string]string{"class": "green"}}
	if s := ComputeStyle(declared, sheets, 800, 600, nil); !s.CascadedProps["color"] {
		t.Errorf("span.green: CascadedProps[color] = false, want true (rule declared it); got %v", s.CascadedProps)
	}

	plain := &html.Node{Type: html.ElementNode, TagName: "span"}
	if s := ComputeStyle(plain, sheets, 800, 600, nil); s.CascadedProps["color"] {
		t.Errorf("plain span: CascadedProps[color] = true, want false (nothing declared color)")
	}

	inline := &html.Node{Type: html.ElementNode, TagName: "span",
		Attributes: map[string]string{"style": "color: green"}}
	if s := ComputeStyle(inline, sheets, 800, 600, nil); !s.CascadedProps["color"] {
		t.Errorf("inline style: CascadedProps[color] = false, want true")
	}

	// A literal `inherit` declaration re-inherits: it must NOT count as a
	// declaration blocking ::first-line inheritance.
	inheritSheet, err := ParseStylesheet(`.re { color: inherit; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	reinherit := &html.Node{Type: html.ElementNode, TagName: "span",
		Attributes: map[string]string{"class": "re"}}
	if s := ComputeStyle(reinherit, []*Stylesheet{inheritSheet}, 800, 600, nil); s.CascadedProps["color"] {
		t.Errorf("color:inherit: CascadedProps[color] = true, want false (inherit re-inherits)")
	}
}
