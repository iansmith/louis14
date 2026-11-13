package layout

// BaselineGroup labels the two baseline-sharing groups of a row or column
// (https://www.w3.org/TR/css-align-3/#baseline-sharing-group): the major
// group aligns to the alignment context's start, the minor group to its
// end. Mirrors Blink's BaselineGroup (core/layout/baseline_utils.h:13 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BaselineGroup uint8

const (
	BaselineGroupMajor BaselineGroup = iota
	BaselineGroupMinor
)

// DetermineBaselineWritingMode is the writing mode a child's baseline is
// read in: in a parallel alignment context the child's own mode, or the
// container's when the child is orthogonal; otherwise the child's mode when
// it is orthogonal, else horizontal-tb or vertical-lr/-rl, whichever is
// orthogonal (css-align-3 §9.1). Mirrors Blink's
// DetermineBaselineWritingMode (baseline_utils.h:16-44 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func DetermineBaselineWritingMode(container WritingDirectionMode, child WritingMode, isParallelContext bool) WritingMode {
	orthogonal := container.WM
	if !isParallelContext {
		switch {
		case child != WritingModeHorizontalTB:
			orthogonal = WritingModeHorizontalTB
		case container.IsLTR():
			orthogonal = WritingModeVerticalLR
		default:
			orthogonal = WritingModeVerticalRL
		}
	}
	isParallel := !container.IsOrthogonalTo(WritingDirectionMode{WM: child})
	if isParallelContext == isParallel {
		return child
	}
	return orthogonal
}

// DetermineBaselineGroup is the baseline-sharing group a child joins: the
// major group aligns with the container's start, swapped for a last
// baseline and again when the context is flipped (wrap-reverse). Mirrors
// Blink's DetermineBaselineGroup (baseline_utils.h:50-89 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func DetermineBaselineGroup(container WritingDirectionMode, baselineWritingMode WritingMode, isParallelContext, isLastBaseline, isFlipped bool) BaselineGroup {
	startGroup, endGroup := BaselineGroupMajor, BaselineGroupMinor
	if isLastBaseline {
		startGroup, endGroup = endGroup, startGroup
	}
	if isFlipped {
		startGroup, endGroup = endGroup, startGroup
	}
	if isParallelContext {
		if baselineWritingMode == container.WM {
			return startGroup
		}
		return endGroup
	}
	// For each writing mode the major group aligns with the container's
	// direction, so the item's inline-start offset matches its baseline
	// offset.
	isLTR := container.IsLTR()
	switch baselineWritingMode {
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		isLTR = !isLTR
	}
	if isLTR {
		return startGroup
	}
	return endGroup
}
