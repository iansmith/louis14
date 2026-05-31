package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

func styleWith(props map[string]string) *css.Style {
	s := css.NewStyle()
	for k, v := range props {
		s.Properties[k] = v
	}
	return s
}

// TestFirstLineDirectlySpecified is the LOU-179 regression guard. CSS Pseudo-4
// §3.2.1: a ::first-line property must NOT clobber a descendant element's own
// directly-specified value, but MUST override a value the descendant merely
// inherits. firstLineDirectlySpecified reports the properties the element set
// directly (computed value differs from the value inherited from its nearest
// ancestor element) AND that ::first-line would otherwise override — those are
// the properties mergeFirstLineStyleExcept must skip.
func TestFirstLineDirectlySpecified(t *testing.T) {
	firstLine := styleWith(map[string]string{"color": "red"})

	t.Run("directly-specified color is skipped", func(t *testing.T) {
		// display-contents-first-line-002: <span> sets color:green; its DOM
		// parent (the #container div) has no color (black default). green !=
		// "" -> directly specified -> ::first-line red must NOT apply.
		elem := styleWith(map[string]string{"color": "green"})
		parent := styleWith(map[string]string{}) // container, color unset
		skip := firstLineDirectlySpecified(elem, parent, firstLine)
		if !skip["color"] {
			t.Fatalf("color set directly on the element must be skipped; got skip=%v", skip)
		}
	})

	t.Run("inherited color is not skipped", func(t *testing.T) {
		// first-line-with-out-of-flow-and-nested-span: the <span> does not set
		// color; it inherits blue from an ancestor. elem.color == parent.color
		// -> inherited -> ::first-line value MAY apply (not skipped).
		elem := styleWith(map[string]string{"color": "blue"})
		parent := styleWith(map[string]string{"color": "blue"})
		skip := firstLineDirectlySpecified(elem, parent, firstLine)
		if skip["color"] {
			t.Fatalf("inherited color must not be skipped; got skip=%v", skip)
		}
	})

	t.Run("property ::first-line does not touch is never skipped", func(t *testing.T) {
		// Even when the element sets a property directly, it is irrelevant to
		// the skip set unless ::first-line would override it.
		elem := styleWith(map[string]string{"color": "green", "font-weight": "700"})
		parent := styleWith(map[string]string{})
		skip := firstLineDirectlySpecified(elem, parent, firstLine)
		if skip["font-weight"] {
			t.Fatalf("font-weight is not in the ::first-line style; must not be skipped; got skip=%v", skip)
		}
	})
}

// TestMergeFirstLineStyleExceptSkipsSpecified verifies the skip set is honored:
// a skipped property keeps the base value while a non-skipped ::first-line
// property is still applied. Uses a no-op FontConfig path by keeping the base
// font-size at the default and only exercising the color property, which does
// not depend on font metrics for the assertions below.
func TestMergeFirstLineStyleExceptSkipsSpecified(t *testing.T) {
	base := styleWith(map[string]string{"color": "green"})
	firstLine := styleWith(map[string]string{"color": "red"})

	// With color in the skip set, the merge must leave color at the base value.
	skip := map[string]bool{"color": true}
	merged := mergeFirstLineStyleExcept(base, firstLine, skip, text.FontConfig{})
	if merged == nil {
		t.Fatal("merge returned nil")
	}
	if got := merged.Properties["color"]; got != "green" {
		t.Fatalf("skipped color must stay green; got %q", got)
	}

	// Without the skip set, the ::first-line red wins.
	merged2 := mergeFirstLineStyleExcept(base, firstLine, nil, text.FontConfig{})
	if got := merged2.Properties["color"]; got != "red" {
		t.Fatalf("non-skipped color must take ::first-line red; got %q", got)
	}
}
