package layout

import "louis14/pkg/css"

// contain_utils.go — helpers shared by block, flex, and grid layout for
// CSS Containment §4.2 (size containment).
//
// Mirrors Blink's length_utils.cc::CalculateIntrinsicBlockSizeIgnoringChildren
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.

// sizeContainedIntrinsicBlockSize returns 0 when CSS size containment is active
// and no explicit block-size is present (auto block-size must collapse to 0 so
// the container is sized as if it had no in-flow children).  Otherwise it
// returns intrinsicBlockSize unchanged.
//
// Call site: replace the inline guard
//
//	if style != nil && style.ShouldApplySizeContainment() && !hasExplicitBlock {
//	    intrinsicBlockSize = 0
//	}
//
// with intrinsicBlockSize = sizeContainedIntrinsicBlockSize(style, hasExplicitBlock, intrinsicBlockSize).
func sizeContainedIntrinsicBlockSize(style *css.Style, hasExplicitBlock bool, intrinsicBlockSize float64) float64 {
	if style != nil && style.ShouldApplySizeContainment() && !hasExplicitBlock {
		return 0
	}
	return intrinsicBlockSize
}
