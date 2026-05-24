package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// Phase 0 (LOU-156, item 1) — stacking-context creation should be gated on
// the CSS Transforms Level 1 §3 "transformable element" rule. Non-replaced
// inline-level boxes (display: inline | ruby | ruby-text) MUST NOT create
// a stacking context purely because of a transform / translate / rotate /
// scale property, because the transform itself paints as a no-op on those
// elements (see pkg/render/paint_layer.go::isTransformableBox). Without the
// gate, the element is spuriously hoisted into a z-list and reorders paint
// even though its transform is ignored.
//
// Blink alignment: at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f,
// paint_property_tree_builder.cc::NeedsTransform short-circuits on
// `if (!object.IsBox()) return false;` BEFORE consulting any transform
// properties. Non-atomic inline LayoutObjects (the LayoutInline class) are
// not boxes, so transforms never reach paint-property-tree creation and
// never establish a stacking context for them.
//
// The cited reference in the source comment of paint_layer.go
// (computed_style.cc:1319 HasPropertyThatCreatesStackingContext) is
// MISLEADING at the pinned SHA — that function returns true for
// kTransform/kTranslate/kRotate/kScale unconditionally. The actual gate
// is at paint-tree-build time via IsBox(). The comment will be corrected
// as part of the fix.

// styleWith builds a Style with the given properties already set.
// Pairs come as (key, value, key, value, ...).
func styleWith(props ...string) *css.Style {
	s := css.NewStyle()
	for i := 0; i+1 < len(props); i += 2 {
		s.Properties[props[i]] = props[i+1]
	}
	return s
}

// boxWith builds a Box with the given style and optional node.
func boxWith(s *css.Style, node *html.Node) *Box {
	return &Box{Style: s, Node: node}
}

// === Item 1 RED tests — non-transformable + transform => no SC ===

func TestCreatesStackingContext_InlineWithTransform_NoSC(t *testing.T) {
	// display: inline + transform: translate(10px,0) — transform is a no-op
	// per CSS Transforms L1 §3 (non-replaced inline is not a transformable
	// element). MUST NOT create a stacking context.
	b := boxWith(styleWith("display", "inline", "transform", "translate(10px, 0)"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with transform must NOT create a stacking context (transform is a no-op on non-replaced inlines)")
	}
}

func TestCreatesStackingContext_RubyWithTransform_NoSC(t *testing.T) {
	// display: ruby + transform — same gate (ruby is a non-transformable
	// element per CSS Transforms L1 §3 enumeration).
	b := boxWith(styleWith("display", "ruby", "transform", "translate(10px, 0)"), makeNode("ruby"))
	if b.CreatesStackingContext() {
		t.Error("display:ruby with transform must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_RubyTextWithTransform_NoSC(t *testing.T) {
	// display: ruby-text + transform — same gate.
	b := boxWith(styleWith("display", "ruby-text", "transform", "translate(10px, 0)"), makeNode("rt"))
	if b.CreatesStackingContext() {
		t.Error("display:ruby-text with transform must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithTranslate_NoSC(t *testing.T) {
	// Individual transform property `translate` should be gated identically
	// to the shorthand `transform`.
	b := boxWith(styleWith("display", "inline", "translate", "10px 0"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with translate must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithRotate_NoSC(t *testing.T) {
	b := boxWith(styleWith("display", "inline", "rotate", "45deg"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with rotate must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithScale_NoSC(t *testing.T) {
	b := boxWith(styleWith("display", "inline", "scale", "1.5"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with scale must NOT create a stacking context")
	}
}

// === Item 1 regression-guard tests — transformable + transform => SC ===

func TestCreatesStackingContext_ReplacedInlineWithTransform_SC(t *testing.T) {
	// Replaced inline elements (img, video, ...) are atomic inlines per
	// CSS Display 3 §2.2 — they DO accept transforms and MUST create a
	// stacking context, even though their computed display is `inline`.
	b := boxWith(styleWith("display", "inline", "transform", "translate(10px, 0)"), makeNode("img"))
	if !b.CreatesStackingContext() {
		t.Error("replaced inline (img) with transform MUST create a stacking context (atomic inline)")
	}
}

func TestCreatesStackingContext_BlockWithTransform_SC(t *testing.T) {
	// display: block is always transformable — must continue to create
	// a stacking context. Regression guard against a too-aggressive gate.
	b := boxWith(styleWith("display", "block", "transform", "translate(10px, 0)"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with transform MUST create a stacking context")
	}
}

func TestCreatesStackingContext_InlineBlockWithTransform_SC(t *testing.T) {
	// display: inline-block is atomic — transformable.
	b := boxWith(styleWith("display", "inline-block", "transform", "translate(10px, 0)"), makeNode("span"))
	if !b.CreatesStackingContext() {
		t.Error("display:inline-block with transform MUST create a stacking context")
	}
}

func TestCreatesStackingContext_BlockWithTranslate_SC(t *testing.T) {
	// Individual translate on a block must still create a stacking
	// context — guards against the gate misreading the individual
	// property branches.
	b := boxWith(styleWith("display", "block", "translate", "10px 0"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with translate MUST create a stacking context")
	}
}
