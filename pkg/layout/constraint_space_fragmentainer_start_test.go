package layout

import "testing"

// TestSetupSpaceBuilderForFragmentation_IsAtFragmentainerStart (LOU-389):
// a child fragmentation space is at the fragmentainer start exactly when its
// fragmentainer offset is <= 0. Mirrors SetupSpaceBuilderForFragmentation
// (fragmentation_utils.cc:343-344 @ a9f50e522efa).
func TestSetupSpaceBuilderForFragmentation_IsAtFragmentainerStart(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	bla := &BlockLayoutAlgorithm{space: ConstraintSpace{
		HasBlockFragmentation:  true,
		FragmentainerBlockSize: 100,
		BlockFragmentationType: FragmentColumn,
	}}
	for _, tc := range []struct {
		offset float64
		want   bool
	}{
		{offset: 0, want: true},
		{offset: -5, want: true},
		{offset: 10, want: false},
	} {
		b := NewConstraintSpaceBuilder(wdm, wdm, false)
		bla.setupSpaceBuilderForFragmentation(b, tc.offset)
		if got := b.Build().IsAtFragmentainerStart; got != tc.want {
			t.Errorf("offset %v: IsAtFragmentainerStart = %v, want %v", tc.offset, got, tc.want)
		}
	}
}

// TestFloatSpace_IsAtFragmentainerStartUsesMarginEdge (LOU-389): a float's
// margins are unbreakable, so a float whose block-start margin edge is at
// the fragmentainer start is at the start even though its border edge (the
// space's fragmentainer offset) is not. Mirrors floats_utils.cc:89-96 @
// a9f50e522efa.
func TestFloatSpace_IsAtFragmentainerStartUsesMarginEdge(t *testing.T) {
	if !isFloatAtFragmentainerStart(10, 10) {
		t.Errorf("float with 10px margin at border-edge offset 10: want at fragmentainer start")
	}
	if isFloatAtFragmentainerStart(20, 10) {
		t.Errorf("float with 10px margin at border-edge offset 20: want not at fragmentainer start")
	}
}
