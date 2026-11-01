package layout

import "testing"

// LOU-483: a balanced two-column multicol holding a padding-top:10px block
// whose only child is a 95px break-inside:avoid block. Geometry is Chrome 155
// headless (LOU-483 scratchpad probe).
//
// With an auto block-size the initial balancing pass floors the column at the
// avoid child's 95px (BreakBeforeChildIfNeeded → CalculateUnbreakableBlockSize,
// fragmentation_utils.cc:1110-1118, :979-992 @ a9f50e522efa); the padding's
// 10px is propagated separately (SetupFragmentBuilderForFragmentation,
// :515-519). The child does not fit at offset 10 in a 95px column, and the
// only breakpoints are inside it or before it (last resort), so the column
// stretches to 105px and holds it unbroken.
//
// With block-size 100px the column cannot stretch, so Chrome breaks inside
// the avoid child, exactly as with column-fill:auto.
func TestBalancedAvoidChildAfterPadding_LOU483(t *testing.T) {
	tree := func() marginSpec {
		return ms("c", props("padding-top", "10px"),
			ms("av", props("height", "95px", "break-inside", "avoid")))
	}
	t.Run("auto height: one unbroken 105px column", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t,
			props("width", "100px", "column-count", "2", "column-gap", "0"), tree())
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"c":  {{0, 0, 50, 105}},
			"av": {{0, 10, 50, 95}},
		})
		checkMulticolHeight(t, frag, 105)
	})
	t.Run("100px height: the avoid child breaks", func(t *testing.T) {
		frag, ids := layoutTwoColumnSpec(t, tree(), true)
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"c":  {{0, 0, 50, 100}, {50, 0, 50, 5}},
			"av": {{0, 10, 50, 90}, {50, 0, 50, 5}},
		})
		checkMulticolHeight(t, frag, 100)
	})
}
