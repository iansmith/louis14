package layout

// PositionedFloat is the outcome of PositionFloat: the float's layout result
// and border-box BFC offset, or a break token when it breaks before (is
// pushed to the next fragmentainer), plus what it reports to column
// balancing. Mirrors Blink's PositionedFloat (positioned_float.h:15-48 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); Blink's BfcOffset is kept as
// two float64 fields, as louis14's exclusion space works in float64 BFC
// coordinates.
type PositionedFloat struct {
	LayoutResult *LayoutResult
	// BreakBeforeToken is set when the float breaks before: it is then
	// not placed in this fragmentainer, and LayoutResult is not added.
	BreakBeforeToken *BlockBreakToken
	BfcLineOffset    float64
	BfcBlockOffset   float64

	TallestUnbreakableBlockSize float64
	MinimumSpaceShortage        float64
}
