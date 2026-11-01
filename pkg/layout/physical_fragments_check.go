package layout

import (
	"fmt"
	"os"
)

// Temporary WI-0 check (LOU-526 M2): with LOUIS14_CHECK_PHYSICAL_FRAGMENTS
// set, the fragment-tree walks that find a box's fragments report on stderr
// where the box's PhysicalFragments differ from what they found.
var checkPhysicalFragments = os.Getenv("LOUIS14_CHECK_PHYSICAL_FRAGMENTS") != ""

func checkPhysicalFragmentsMatch(where string, node *LayoutInputNode, walked []*PhysicalFragment) {
	if !checkPhysicalFragments {
		return
	}
	stored := node.PhysicalFragments()
	same := len(stored) == len(walked)
	for i := 0; same && i < len(stored); i++ {
		same = stored[i] == walked[i]
	}
	if !same {
		fmt.Fprintf(os.Stderr, "PHYSFRAG-MISMATCH %s node=%p tag=%s walked=%d stored=%d\n", where, node, node.TagName(), len(walked), len(stored))
	}
}

func checkUnfinishedMatch(where string, cb *LayoutInputNode, walked bool) {
	if !checkPhysicalFragments {
		return
	}
	results := cb.GetLayoutResults()
	stored := len(results) > 0 && results[len(results)-1].BreakToken != nil
	if stored != walked {
		fmt.Fprintf(os.Stderr, "PHYSFRAG-MISMATCH unfinished-%s node=%p tag=%s walked=%v stored=%v\n", where, cb, cb.TagName(), walked, stored)
	}
}
