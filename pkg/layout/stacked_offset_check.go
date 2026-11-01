package layout

import (
	"fmt"
	"math"
	"os"
)

// Temporary WI-3 check (LOU-526 M3): with LOUIS14_CHECK_STACKED_OFFSETS set,
// GetContainingBlockInfo reports on stderr where a descendant's stacked
// containing-block offset differs from the one the fragment-tree walk finds.
var checkStackedOffsets = os.Getenv("LOUIS14_CHECK_STACKED_OFFSETS") != ""

func checkStackedOffset(d LogicalOofNodeForFragmentation, walked LogicalOffset) {
	if !checkStackedOffsets {
		return
	}
	got := d.ContainingBlock.Offset
	same := math.Abs(got.InlineOffset-walked.InlineOffset) < 0.01 && math.Abs(got.BlockOffset-walked.BlockOffset) < 0.01
	fmt.Fprintf(os.Stderr, "STACKED-CHECKED\n")
	if !same {
		fmt.Fprintf(os.Stderr, "STACKED-MISMATCH cb=%s oof=%s offset=(%v,%v) walked=(%v,%v)\n",
			d.ContainingBlock.Fragment.LayoutNode.TagName(), d.Candidate.Node.TagName(),
			got.InlineOffset, got.BlockOffset, walked.InlineOffset, walked.BlockOffset)
	}
}
