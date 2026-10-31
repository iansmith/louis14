package html

import "testing"

// TestAnimationClockIgnoresEarlierTime pins the monotonic guard in Blink
// AnimationClock::UpdateTime (core/animation/animation_clock.cc:45-53 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): a time earlier than the current
// one leaves the clock unchanged.
func TestAnimationClockIgnoresEarlierTime(t *testing.T) {
	var c AnimationClock
	if got := c.CurrentTime(); got != 0 {
		t.Fatalf("zero-value clock reads %v, want 0", got)
	}
	c.UpdateTime(2 * ApproximateFrameTime)
	c.UpdateTime(ApproximateFrameTime)
	if got, want := c.CurrentTime(), 2*ApproximateFrameTime; got != want {
		t.Errorf("after UpdateTime(2f) then UpdateTime(f): got %v, want %v", got, want)
	}
}
