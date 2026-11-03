package layout

// BackgroundBleedAvoidance is how a box with rounded borders keeps its
// background from showing through the anti-aliased border edge. Mirrors
// Blink's BackgroundBleedAvoidance
// (core/layout/background_bleed_avoidance.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BackgroundBleedAvoidance int

const (
	BackgroundBleedNone BackgroundBleedAvoidance = iota
	BackgroundBleedShrinkBackground
	BackgroundBleedClipOnly
	BackgroundBleedClipLayer
)
