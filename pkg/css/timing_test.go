package css

import (
	"math"
	"testing"
)

// Phase 1 gate: verify ComputePhase / progress on the exact delay/duration/
// timing-function combos exercised by the in-scope css-animations tests.

func TestTimingAnimationImportant002(t *testing.T) {
	// animation-important-002: duration 1000s, delay -500s, steps(2, end).
	// localTime = document time 0. Phase active, progress 0.5, transformed 0.5.
	tm := Timing{
		StartDelay:        -500,
		IterationDuration: 1000,
		IterationCount:    math.Inf(1),
		Direction:         DirNormal,
		Fill:              FillNone,
		TimingFunction:    StepsTimingFunction{Steps: 2, Position: StepEnd},
	}
	r := tm.Resolve(0)
	if r.Phase != PhaseActive {
		t.Fatalf("phase = %v, want PhaseActive", r.Phase)
	}
	if !r.InEffect {
		t.Fatalf("InEffect = false, want true")
	}
	if math.Abs(r.Progress-0.5) > 1e-9 {
		t.Fatalf("progress = %v, want 0.5", r.Progress)
	}
}

func TestTimingAnimationDelay011(t *testing.T) {
	// animation-delay-011: bg 100s step-end infinite, animation-delay -50s.
	// localTime 0. Phase active, simple progress 0.5. step-end is steps(1,
	// jump-end): at exactly 0.5 with right limit -> floor(1*0.5)=0 -> 0.
	tm := Timing{
		StartDelay:        -50,
		IterationDuration: 100,
		IterationCount:    math.Inf(1),
		Direction:         DirNormal,
		Fill:              FillNone,
		TimingFunction:    StepsTimingFunction{Steps: 1, Position: StepJumpEnd},
	}
	r := tm.Resolve(0)
	if r.Phase != PhaseActive {
		t.Fatalf("phase = %v, want PhaseActive", r.Phase)
	}
	if r.Progress != 0 {
		t.Fatalf("transformed progress = %v, want 0", r.Progress)
	}
	// But the *simple* iteration progress is 0.5 — the 50% keyframe is selected
	// because keyframe selection brackets the simple progress, while the timing
	// function maps the within-segment fraction. This is verified at the
	// BuildKeyframeEffect / ResolveKeyframeStyle layer in Phase 2.
}

func TestTimingJumpStartBeforePhase(t *testing.T) {
	// jump-start-animation-before-phase: slide 10000s 5000s steps(1, jump-start)
	// backwards. localTime 0 < delay 5000 -> before phase. backwards fill ->
	// in effect. Transformed progress: before phase -> left limit. steps(1,
	// jump-start): startOffset 1, floor(1*0+1)=1, left limit at boundary ->
	// decrement to 0 -> 0/1 = 0.  -> "from" keyframe (translateX(100px)).
	tm := Timing{
		StartDelay:        5000,
		IterationDuration: 10000,
		IterationCount:    1,
		Direction:         DirNormal,
		Fill:              FillBackwards,
		TimingFunction:    StepsTimingFunction{Steps: 1, Position: StepJumpStart},
	}
	r := tm.Resolve(0)
	if r.Phase != PhaseBefore {
		t.Fatalf("phase = %v, want PhaseBefore", r.Phase)
	}
	if !r.InEffect {
		t.Fatalf("InEffect = false, want true (backwards fill)")
	}
	if r.Progress != 0 {
		t.Fatalf("transformed progress = %v, want 0", r.Progress)
	}
}

func TestTimingFillModeGate(t *testing.T) {
	// FillNone in the before phase -> not in effect.
	tm := Timing{
		StartDelay:        5000,
		IterationDuration: 10000,
		IterationCount:    1,
		Direction:         DirNormal,
		Fill:              FillNone,
		TimingFunction:    LinearTimingFunction{},
	}
	if r := tm.Resolve(0); r.InEffect {
		t.Fatalf("FillNone before phase: InEffect = true, want false")
	}
	// FillBoth in the before phase -> in effect.
	tm.Fill = FillBoth
	if r := tm.Resolve(0); !r.InEffect {
		t.Fatalf("FillBoth before phase: InEffect = false, want true")
	}
}

func TestTimingActivePhaseInEffect(t *testing.T) {
	// from==to constant animation, no fill mode, active phase -> in effect at
	// progress 0 (offscreen-to-onscreen, translation-subpixel, svg-transform).
	tm := Timing{
		StartDelay:        0,
		IterationDuration: 10,
		IterationCount:    math.Inf(1),
		Direction:         DirNormal,
		Fill:              FillNone,
		TimingFunction:    LinearTimingFunction{},
	}
	r := tm.Resolve(0)
	if r.Phase != PhaseActive || !r.InEffect {
		t.Fatalf("active no-fill: phase=%v inEffect=%v, want active/true", r.Phase, r.InEffect)
	}
	if r.Progress != 0 {
		t.Fatalf("progress = %v, want 0", r.Progress)
	}
}

func TestStepsTimingFunctionEvaluate(t *testing.T) {
	cases := []struct {
		name string
		fn   StepsTimingFunction
		in   float64
		ld   limitDirection
		want float64
	}{
		{"steps(2,end) @0.5 right", StepsTimingFunction{2, StepEnd}, 0.5, limitRight, 0.5},
		{"steps(1,jump-end) @0.5 right", StepsTimingFunction{1, StepJumpEnd}, 0.5, limitRight, 0},
		{"steps(1,jump-start) @0 left", StepsTimingFunction{1, StepJumpStart}, 0, limitLeft, 0},
		{"steps(1,jump-start) @0 right", StepsTimingFunction{1, StepJumpStart}, 0, limitRight, 1},
		{"steps(2,end) @1 right", StepsTimingFunction{2, StepEnd}, 1, limitRight, 1},
	}
	for _, c := range cases {
		got := c.fn.Evaluate(c.in, c.ld)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
