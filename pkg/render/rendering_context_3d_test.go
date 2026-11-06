package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// Participants of a 3D rendering context are painted with their own
// projection to the context base, unflattened through their preserve-3d
// ancestors (TransformPaintPropertyNode flattens_inherited_transform /
// rendering_context_id, FragmentPaintPropertyTreeBuilder::UpdateTransform,
// core/paint/paint_property_tree_builder.cc:1458-1481, :1634-1651; the
// projection is GeometryMapper::SourceToDestinationProjection @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// TestRenderingContext3D_ParticipantThroughInlineBlock: a ::before
// inline-block with rotateX(90deg) inside a rotateX(90deg) preserve-3d div
// composes to rotateX(180deg) — visible, mirrored. WPT
// css-transforms/preserve3d-pseudo-element.
func TestRenderingContext3D_ParticipantThroughInlineBlock(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
body { margin: 0 }
div { width: 200px; height: 200px; transform: rotateX(90deg); transform-style: preserve-3d; }
div::before { display: inline-block; width: 200px; height: 200px; transform: rotateX(90deg); content: ''; background-color: green; }
</style></head><body><div></div></body></html>`
	img := render.RenderPage(t, src, 300, 300)
	if c := img.RGBAAt(100, 100); !(c.G > 100 && c.R < 60 && c.B < 60) {
		t.Errorf("pixel (100,100) = %v, want green: the ::before composes with its preserve-3d parent", c)
	}
}

// TestRenderingContext3D_PerspectiveFunctionOnContextRoot: a preserve-3d
// root with transform: perspective(0px) (used 1px) projects its
// translateZ(0.5px) participant at 2×, the root's perspective composing with
// the participant's z before projection. WPT
// css-transforms/perspective-zero-2-ref.
func TestRenderingContext3D_PerspectiveFunctionOnContextRoot(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
body { margin: 0 }
.parent { transform: perspective(0px); transform-style: preserve-3d; transform-origin: top left; }
.parent > div { width: 100px; height: 100px; position: absolute; }
.child-3d { background: green; transform: translateZ(0.5px); }
</style></head><body><div class="parent"><div class="child-3d"></div></div></body></html>`
	img := render.RenderPage(t, src, 300, 300)
	if c := img.RGBAAt(150, 150); !(c.G > 100 && c.R < 60 && c.B < 60) {
		t.Errorf("pixel (150,150) = %v, want green: translateZ(0.5px) under perspective(1px) scales 2×", c)
	}
}
