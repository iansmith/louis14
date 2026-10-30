package layout

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

// TestFinishFragmentation (LOU-395): table tests for the branches of Blink's
// FinishFragmentation (fragmentation_utils.cc:547-822 @ a9f50e522efa). Sizes
// are border-box; trailing border/padding is 10 unless stated.
func TestFinishFragmentation(t *testing.T) {
	resumed := &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(40)}
	pastEnd := &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(40), IsAtBlockEnd: true}
	base := FinishFragmentationInput{
		HasBlockFragmentation:   true,
		IsColumnFragmentation:   true,
		TrailingBorderPadding:   10,
		BlockStartBorderPadding: 10,
	}
	for _, tc := range []struct {
		name string
		in   func(FinishFragmentationInput) FinishFragmentationInput
		want FinishFragmentationResult
	}{
		{"fits: at block end", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 100, 60, 60
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 60, IntrinsicBlockSize: 60, ConsumedBlockSize: 60, IsAtBlockEnd: true, IncludeBlockEnd: true}},
		{"taller than space: breaks itself before the trailing edge", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 50, 120, 30
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 50, IntrinsicBlockSize: 50, ConsumedBlockSize: 50, DidBreakSelf: true, SpaceShortage: 70}},
		{"unbreakable content taller than space: allowed to overflow", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 50, 120, 90
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 80, IntrinsicBlockSize: 80, ConsumedBlockSize: 80, DidBreakSelf: true, SpaceShortage: 40}},
		{"resumed slice fragment: no block-start edge", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 100, 100, 60, resumed
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 60, IntrinsicBlockSize: 60, ConsumedBlockSize: 100, IsAtBlockEnd: true, IncludeBlockEnd: true}},
		{"past end: zero size, at block end, no edges", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 100, 100, 0, pastEnd
			in.ShouldBreakInside = true
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 0, IntrinsicBlockSize: 0, ConsumedBlockSize: 140, IsAtBlockEnd: true}},
		{"column spanner inside: breaks itself, no trailing edge", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 100, 100, 40
			in.FoundColumnSpanner = true
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 30, IntrinsicBlockSize: 40, ConsumedBlockSize: 30, DidBreakSelf: true}},
		{"child broke inside, box fits but not known to fit: not at block end", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 100, 80, 80
			in.ShouldBreakInside, in.HasInflowChildBreakInside = true, true
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 80, IntrinsicBlockSize: 80, ConsumedBlockSize: 80}},
		{"child broke inside, box known to fit: at block end, consumes the fragmentainer", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 100, 80, 80
			in.ShouldBreakInside, in.HasInflowChildBreakInside, in.IsKnownToFitInFragmentainer = true, true, true
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 80, IntrinsicBlockSize: 80, ConsumedBlockSize: 100, IsAtBlockEnd: true, IncludeBlockEnd: true}},
		{"unknown space (balancing pass): at block end unless broken by a child", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = Indefinite, 300, 300
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 300, IntrinsicBlockSize: 300, ConsumedBlockSize: 300, IsAtBlockEnd: true, IncludeBlockEnd: true}},
		{"clone: resumed fragment keeps both edges while breaking itself", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 50, 140, 30, resumed
			in.ShouldCloneBoxEndDecorations, in.PreventBreakBeforeBlockEndDecorations = true, true
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 50, IntrinsicBlockSize: 50, ConsumedBlockSize: 90, DidBreakSelf: true, IncludeBlockEnd: true, SpaceShortage: 50}},
		{"first fragment, space smaller than the block-start edge: last-resort appeal", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize = 5, 120, 10
			return in
		}, FinishFragmentationResult{FragmentBlockSize: 5, IntrinsicBlockSize: 5, ConsumedBlockSize: 5, DidBreakSelf: true, SpaceShortage: 115, ClampAppealToLastResort: true}},
	} {
		if got := FinishFragmentation(tc.in(base)); got != tc.want {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}
