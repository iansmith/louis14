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
// paint_property_tree_builder.cc:1310 (in NeedsTransform, lines 1299-1319)
// short-circuits on `if (!object.IsBox()) return false;` BEFORE consulting
// any transform properties. Non-atomic inline LayoutObjects (the
// LayoutInline class) are not boxes, so transforms never reach
// paint-property-tree creation and never establish a stacking context for
// them.
//
// The cited reference in the source comment of paint_layer.go
// (computed_style.cc:1319 HasPropertyThatCreatesStackingContext) is
// MISLEADING at the pinned SHA — that function returns true for
// kTransform/kTranslate/kRotate/kScale unconditionally. The actual gate
// is at paint-tree-build time via IsBox(). The comment will be corrected
// as part of the fix.

// makeBox builds a Box with the given style and node, defaulting Position
// to css.PositionStatic so tests that combine z-index with non-positioned
// styles don't spuriously fall into the explicit-z-index stacking-context
// branch at types.go:126 (which tests `b.Position != css.PositionStatic`
// — the zero value "" is not equal to "static"). Mirrors the makeNode /
// makeStyle helper vocabulary in this package's test suite.
func makeBox(s *css.Style, node *html.Node) *Box {
	return &Box{Style: s, Node: node, Position: css.PositionStatic}
}

// === Item 1 RED tests — non-transformable + transform => no SC ===

func TestCreatesStackingContext_InlineWithTransform_NoSC(t *testing.T) {
	// display: inline + transform: translate(10px,0) — transform is a no-op
	// per CSS Transforms L1 §3 (non-replaced inline is not a transformable
	// element). MUST NOT create a stacking context.
	b := makeBox(makeStyle("display", "inline", "transform", "translate(10px, 0)"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with transform must NOT create a stacking context (transform is a no-op on non-replaced inlines)")
	}
}

func TestCreatesStackingContext_RubyWithTransform_NoSC(t *testing.T) {
	// display: ruby + transform — same gate (ruby is a non-transformable
	// element per CSS Transforms L1 §3 enumeration).
	b := makeBox(makeStyle("display", "ruby", "transform", "translate(10px, 0)"), makeNode("ruby"))
	if b.CreatesStackingContext() {
		t.Error("display:ruby with transform must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_RubyTextWithTransform_NoSC(t *testing.T) {
	// display: ruby-text + transform — same gate.
	b := makeBox(makeStyle("display", "ruby-text", "transform", "translate(10px, 0)"), makeNode("rt"))
	if b.CreatesStackingContext() {
		t.Error("display:ruby-text with transform must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithTranslate_NoSC(t *testing.T) {
	// Individual transform property `translate` should be gated identically
	// to the shorthand `transform`.
	b := makeBox(makeStyle("display", "inline", "translate", "10px 0"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with translate must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithRotate_NoSC(t *testing.T) {
	b := makeBox(makeStyle("display", "inline", "rotate", "45deg"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with rotate must NOT create a stacking context")
	}
}

func TestCreatesStackingContext_InlineWithScale_NoSC(t *testing.T) {
	b := makeBox(makeStyle("display", "inline", "scale", "1.5"), makeNode("span"))
	if b.CreatesStackingContext() {
		t.Error("display:inline with scale must NOT create a stacking context")
	}
}

// === Item 1 regression-guard tests — transformable + transform => SC ===

func TestCreatesStackingContext_ReplacedInlineWithTransform_SC(t *testing.T) {
	// Replaced inline elements (img, video, ...) are atomic inlines per
	// CSS Display 3 §2.2 — they DO accept transforms and MUST create a
	// stacking context, even though their computed display is `inline`.
	b := makeBox(makeStyle("display", "inline", "transform", "translate(10px, 0)"), makeNode("img"))
	if !b.CreatesStackingContext() {
		t.Error("replaced inline (img) with transform MUST create a stacking context (atomic inline)")
	}
}

func TestCreatesStackingContext_BlockWithTransform_SC(t *testing.T) {
	// display: block is always transformable — must continue to create
	// a stacking context. Regression guard against a too-aggressive gate.
	b := makeBox(makeStyle("display", "block", "transform", "translate(10px, 0)"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with transform MUST create a stacking context")
	}
}

func TestCreatesStackingContext_InlineBlockWithTransform_SC(t *testing.T) {
	// display: inline-block is atomic — transformable.
	b := makeBox(makeStyle("display", "inline-block", "transform", "translate(10px, 0)"), makeNode("span"))
	if !b.CreatesStackingContext() {
		t.Error("display:inline-block with transform MUST create a stacking context")
	}
}

func TestCreatesStackingContext_BlockWithTranslate_SC(t *testing.T) {
	// Individual translate on a block must still create a stacking
	// context — guards against the gate misreading the individual
	// property branches.
	b := makeBox(makeStyle("display", "block", "translate", "10px 0"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with translate MUST create a stacking context")
	}
}

func TestCreatesStackingContext_BlockWithRotate_SC(t *testing.T) {
	// Individual rotate on a block must still create a stacking context.
	// Pairs with _InlineWithRotate_NoSC: the IsTransformableBox gate
	// covers all four transform-related branches (transform, translate,
	// rotate, scale) — over-gating on any single branch would silently
	// regress production rendering of `<div style='rotate:...'>`.
	b := makeBox(makeStyle("display", "block", "rotate", "45deg"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with rotate MUST create a stacking context")
	}
}

func TestCreatesStackingContext_BlockWithScale_SC(t *testing.T) {
	// Individual scale on a block must still create a stacking context.
	// Pairs with _InlineWithScale_NoSC. Same rationale as _BlockWithRotate_SC.
	b := makeBox(makeStyle("display", "block", "scale", "1.5"), makeNode("div"))
	if !b.CreatesStackingContext() {
		t.Error("display:block with scale MUST create a stacking context")
	}
}

// === Item 1 matrix fill — display × individual-property cells not covered above ===
//
// CreatesStackingContext (types.go:146-157) consults 4 independent
// transform-related branches: GetTransforms (shorthand) and
// GetIndividual{Translate,Rotate,Scale}. The eventual fix wraps all four
// in one IsTransformableBox gate, so a copy-paste bug in any single
// branch could silently regress one display × property combination.
//
// The explicit tests above cover inline×{transform,translate,rotate,scale}
// (NoSC) and block×{transform,translate,rotate,scale} (SC) — the two
// per-property columns. They also cover ruby/ruby-text/inline-block/
// replaced-inline × {transform} (the per-display row). This table fills
// the remaining 12 cells across those six displays, so the production
// fix's per-branch correctness is regression-guarded across the matrix.
// (Atomic-inline variants beyond inline-block — inline-flex, inline-grid,
// inline-list-item — and `display: block ruby` are NOT covered; they
// fall through isTransformableBox's switch default to `return true`,
// same as inline-block.)
//
// CSS Transforms L1 §3 enumerates the non-transformable element set as
// "non-replaced inline boxes, table-column boxes, table-column-group
// boxes." Ruby and ruby-text aren't named separately by the spec — they
// fall under "non-replaced inline boxes" because their generated boxes
// are inline-level (Blink models them via LayoutInline). Louis14
// recognizes display: inline | ruby | ruby-text directly; display:
// table-column / table-column-group are not yet recognized by
// GetDisplay() (see the gap note at paint_layer.go:715-719) and so
// don't appear here.
//
// At SHA 4883d11fef Blink's `paint_property_tree_builder.cc:1310`
// (NeedsTransform, :1299-:1319) implements the same gate via
// `if (!object.IsBox()) return false;` — non-atomic inline
// LayoutObjects (LayoutInline) are not boxes, so transforms never reach
// paint-property-tree creation for them. Replaced inlines (LayoutReplaced
// — img/video/...) and atomic inlines (inline-block/inline-flex/...)
// ARE boxes and ARE transformable.

func TestCreatesStackingContext_TransformMatrix(t *testing.T) {
	cases := []struct {
		name    string
		display string
		prop    string
		value   string
		tag     string
		wantSC  bool
	}{
		// Non-transformable display × individual property → no SC.
		{"ruby_translate", "ruby", "translate", "10px 0", "ruby", false},
		{"ruby_rotate", "ruby", "rotate", "45deg", "ruby", false},
		{"ruby_scale", "ruby", "scale", "1.5", "ruby", false},
		{"rubytext_translate", "ruby-text", "translate", "10px 0", "rt", false},
		{"rubytext_rotate", "ruby-text", "rotate", "45deg", "rt", false},
		{"rubytext_scale", "ruby-text", "scale", "1.5", "rt", false},
		// Transformable display × individual property → SC.
		{"inlineblock_translate", "inline-block", "translate", "10px 0", "span", true},
		{"inlineblock_rotate", "inline-block", "rotate", "45deg", "span", true},
		{"inlineblock_scale", "inline-block", "scale", "1.5", "span", true},
		// Replaced inline (img) keeps computed display:inline but is an
		// atomic inline per CSS Display 3 §2.2 — transformable.
		{"img_translate", "inline", "translate", "10px 0", "img", true},
		{"img_rotate", "inline", "rotate", "45deg", "img", true},
		{"img_scale", "inline", "scale", "1.5", "img", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := makeBox(makeStyle("display", c.display, c.prop, c.value), makeNode(c.tag))
			if got := b.CreatesStackingContext(); got != c.wantSC {
				t.Errorf("display=%s %s=%s tag=%s: CreatesStackingContext()=%v, want %v",
					c.display, c.prop, c.value, c.tag, got, c.wantSC)
			}
		})
	}
}
