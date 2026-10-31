package html

import "time"

// Animation timing state owned by the document and its elements: the
// document's AnimationClock (read by document.timeline and by CSS animation
// sampling) and each element's running CSS animations.

// ApproximateFrameTime is the time one animation frame advances the
// document's AnimationClock. Mirrors Blink kApproximateFrameTime =
// base::Seconds(1 / 60.0) (core/animation/animation_clock.cc:40 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
const ApproximateFrameTime = time.Second / 60

// AnimationClock is the document's animation time source. Mirrors Blink
// AnimationClock (core/animation/animation_clock.h:46 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), with time measured from the
// timeline's zero (document load) instead of as base::TimeTicks. The zero
// value reads time 0.
//
// louis14 has no frame loop: the synchronous requestAnimationFrame shim in
// pkg/js advances the clock by ApproximateFrameTime per frame callback.
type AnimationClock struct {
	time time.Duration
}

// UpdateTime moves the clock to t. A t earlier than the current time is
// ignored, so the clock is monotonic. Mirrors Blink AnimationClock::UpdateTime
// (core/animation/animation_clock.cc:45-53 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (c *AnimationClock) UpdateTime(t time.Duration) {
	if t < c.time {
		return
	}
	c.time = t
}

// CurrentTime returns the clock's time since the timeline's zero. Mirrors
// Blink AnimationClock::CurrentTime (core/animation/animation_clock.h:60 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (c *AnimationClock) CurrentTime() time.Duration {
	return c.time
}

// ElementAnimations is an element's animation state. Mirrors Blink
// ElementAnimations, whose CssAnimations() holds the element's CSS
// animations (core/animation/element_animations.h:95 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
type ElementAnimations struct {
	// CSSAnimations is the element's running CSS animations, in
	// animation-name order. Mirrors Blink CSSAnimations::running_animations_
	// (core/animation/css/css_animations.h:278 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520).
	CSSAnimations []RunningAnimation
}

// RunningAnimation is one CSS animation that style resolution has started on
// an element. Mirrors Blink CSSAnimations::RunningAnimation
// (core/animation/css/css_animations.h:164 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
type RunningAnimation struct {
	// Name is the animation-name.
	Name string
	// NameIndex is n where this is the nth occurrence of Name in the
	// animation-name list, as Blink computes name_index
	// (core/animation/css/css_animations.cc:1877-1883 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520). Name and NameIndex together
	// identify the animation across style recalcs.
	NameIndex int
	// StartTimeResolved is true when the animation is playing with a start
	// time of the timeline's zero, so its local time is the clock's current
	// time. A pipeline's style recalc before any script runs happens at clock
	// 0, so "the previous style recalc" and "timeline zero" coincide.
	// False means the animation is held at local time 0, like a Blink
	// animation whose play is still pending with hold time 0 or that is
	// paused.
	StartTimeResolved bool
}
