package css

import (
	"math"
	"strconv"
	"strings"
)

// Static-snapshot animation timing model.
//
// Mirrors Blink's core/animation/timing.h (the Timing struct) and the phase /
// progress arithmetic from core/animation/timing_calculations.cc and
// core/animation/animation_effect.cc, plus the steps() evaluator from
// ui/gfx/animation/keyframe/timing_function.cc.
//
// louis14's reftest harness is a static renderer: the document timeline is
// frozen at t=0. So the animation's local time is derived purely from
// animation-delay (a negative delay pushes the animation into the active or
// after phase — this is exactly how the in-scope "frozen" tests are authored).

// AnimationPhase mirrors Blink Timing::Phase.
type AnimationPhase int

const (
	PhaseBefore AnimationPhase = iota
	PhaseActive
	PhaseAfter
	PhaseNone
)

// FillMode mirrors the CSS animation-fill-mode values.
type FillMode int

const (
	FillNone FillMode = iota
	FillForwards
	FillBackwards
	FillBoth
)

// PlaybackDirection mirrors the CSS animation-direction values.
type PlaybackDirection int

const (
	DirNormal PlaybackDirection = iota
	DirReverse
	DirAlternate
	DirAlternateReverse
)

// StepPosition mirrors gfx StepsTimingFunction::StepPosition.
type StepPosition int

const (
	StepStart StepPosition = iota
	StepEnd
	StepJumpStart
	StepJumpEnd
	StepJumpNone
	StepJumpBoth
)

// limitDirection mirrors Blink TimingFunction::LimitDirection.
type limitDirection int

const (
	limitRight limitDirection = iota
	limitLeft
)

// TimingFunction is an easing function: maps a fraction in [0,1] to an output.
// Mirrors Blink's TimingFunction hierarchy; for the in-scope css-animations
// tests only linear and steps() are needed, plus cubic-bezier for completeness.
type TimingFunction interface {
	Evaluate(fraction float64, ld limitDirection) float64
}

// LinearTimingFunction is the identity easing.
type LinearTimingFunction struct{}

func (LinearTimingFunction) Evaluate(f float64, _ limitDirection) float64 { return f }

// StepsTimingFunction mirrors gfx StepsTimingFunction.
type StepsTimingFunction struct {
	Steps    int
	Position StepPosition
}

// getStepsStartOffset mirrors gfx StepsTimingFunction::GetStepsStartOffset.
func (s StepsTimingFunction) getStepsStartOffset() float64 {
	switch s.Position {
	case StepStart, StepJumpStart, StepJumpBoth:
		return 1.0
	default: // StepEnd, StepJumpEnd, StepJumpNone
		return 0.0
	}
}

// numberOfJumps mirrors gfx StepsTimingFunction::NumberOfJumps.
func (s StepsTimingFunction) numberOfJumps() int {
	switch s.Position {
	case StepJumpBoth:
		return s.Steps + 1
	case StepJumpNone:
		if s.Steps > 1 {
			return s.Steps - 1
		}
		return 1
	default: // StepStart, StepEnd, StepJumpStart, StepJumpEnd
		return s.Steps
	}
}

// Evaluate mirrors gfx StepsTimingFunction::GetValue.
func (s StepsTimingFunction) Evaluate(t float64, ld limitDirection) float64 {
	jumps := s.numberOfJumps()
	if jumps <= 0 {
		jumps = 1
	}
	steps := float64(s.Steps)
	currentStep := math.Floor(steps*t + s.getStepsStartOffset())
	// Left-limit discontinuity: if evaluating the left limit and t lands exactly
	// on a step boundary, step back by one.
	if ld == limitLeft && steps*t == math.Floor(steps*t) {
		currentStep--
	}
	// Clamp to the valid range.
	if t >= 0 && currentStep < 0 {
		currentStep = 0
	}
	if t <= 1 && currentStep > float64(jumps) {
		currentStep = float64(jumps)
	}
	return currentStep / float64(jumps)
}

// CubicBezierTimingFunction mirrors gfx CubicBezierTimingFunction. Only used by
// out-of-scope tests; included for completeness so the parser never falls back
// silently to linear for a cubic-bezier() value.
type CubicBezierTimingFunction struct {
	X1, Y1, X2, Y2 float64
}

func (c CubicBezierTimingFunction) Evaluate(t float64, _ limitDirection) float64 {
	// Solve for the parameter s where bezierX(s) == t, then return bezierY(s).
	// Newton-Raphson with a bisection fallback (mirrors gfx CubicBezier::Solve).
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	bezier := func(p1, p2, s float64) float64 {
		mt := 1 - s
		return 3*mt*mt*s*p1 + 3*mt*s*s*p2 + s*s*s
	}
	s := t
	for i := 0; i < 8; i++ {
		x := bezier(c.X1, c.X2, s) - t
		if math.Abs(x) < 1e-6 {
			return bezier(c.Y1, c.Y2, s)
		}
		// Derivative of bezierX.
		mt := 1 - s
		d := 3*mt*mt*c.X1 + 6*mt*s*(c.X2-c.X1) + 3*s*s*(1-c.X2)
		if math.Abs(d) < 1e-6 {
			break
		}
		s -= x / d
	}
	lo, hi := 0.0, 1.0
	s = t
	for i := 0; i < 20; i++ {
		x := bezier(c.X1, c.X2, s)
		if math.Abs(x-t) < 1e-6 {
			break
		}
		if x < t {
			lo = s
		} else {
			hi = s
		}
		s = (lo + hi) / 2
	}
	return bezier(c.Y1, c.Y2, s)
}

// Timing mirrors Blink core/animation/timing.h Timing. Times are in seconds.
type Timing struct {
	StartDelay        float64 // animation-delay (seconds)
	IterationDuration float64 // animation-duration (seconds)
	IterationCount    float64 // animation-iteration-count (math.Inf for "infinite")
	Direction         PlaybackDirection
	Fill              FillMode
	TimingFunction    TimingFunction // animation-timing-function (default: linear)
}

// activeDuration returns the active duration of the animation: the iteration
// duration multiplied by the iteration count (mirrors Blink's
// CalculateActiveDuration). Infinite count -> infinite duration.
func (t Timing) activeDuration() float64 {
	if math.IsInf(t.IterationCount, 1) || t.IterationCount < 0 {
		return math.Inf(1)
	}
	return t.IterationDuration * t.IterationCount
}

// ComputePhase mirrors Blink TimingCalculations::CalculatePhase. localTime is
// the animation's local time (in a static render: -StartDelay measured from the
// document timeline origin t=0).
func (t Timing) ComputePhase(localTime float64) AnimationPhase {
	activeDur := t.activeDuration()
	beforeActiveBoundary := math.Max(t.StartDelay, 0)
	activeAfterBoundary := math.Max(t.StartDelay+activeDur, 0)
	if localTime < beforeActiveBoundary {
		return PhaseBefore
	}
	if localTime >= activeAfterBoundary {
		return PhaseAfter
	}
	return PhaseActive
}

// computeActiveTime mirrors Blink TimingCalculations::CalculateActiveTime.
// Returns (activeTime, resolved): resolved is false when the fill mode means the
// animation is not in effect for this phase.
func (t Timing) computeActiveTime(localTime float64, phase AnimationPhase) (float64, bool) {
	activeDur := t.activeDuration()
	switch phase {
	case PhaseBefore:
		if t.Fill == FillBackwards || t.Fill == FillBoth {
			return math.Max(localTime-t.StartDelay, 0), true
		}
		return 0, false
	case PhaseActive:
		return localTime - t.StartDelay, true
	case PhaseAfter:
		if t.Fill == FillForwards || t.Fill == FillBoth {
			at := localTime - t.StartDelay
			if at < 0 {
				at = 0
			}
			if !math.IsInf(activeDur, 1) && at > activeDur {
				at = activeDur
			}
			return at, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// computeOverallProgress mirrors TimingCalculations::CalculateOverallProgress.
func (t Timing) computeOverallProgress(activeTime float64, phase AnimationPhase) float64 {
	if t.IterationDuration == 0 {
		if phase == PhaseAfter {
			return t.IterationCount
		}
		return 0
	}
	return activeTime / t.IterationDuration
}

// computeSimpleIterationProgress mirrors
// TimingCalculations::CalculateSimpleIterationProgress.
func (t Timing) computeSimpleIterationProgress(overallProgress, activeTime float64, phase AnimationPhase) float64 {
	var progress float64
	if math.IsInf(overallProgress, 1) {
		progress = 0
	} else {
		progress = math.Mod(overallProgress, 1.0)
		if progress < 0 {
			progress += 1.0
		}
	}
	// Edge case: at the very end of the active range, progress 0 becomes 1 so the
	// final keyframe (not the first) is selected.
	if progress == 0 && (phase == PhaseActive || phase == PhaseAfter) &&
		activeTime == t.activeDuration() && t.IterationCount != 0 {
		progress = 1.0
	}
	return progress
}

// computeCurrentIteration mirrors TimingCalculations::CalculateCurrentIteration.
func (t Timing) computeCurrentIteration(overallProgress, simpleProgress float64, phase AnimationPhase) float64 {
	if phase == PhaseAfter && math.IsInf(t.IterationCount, 1) {
		return math.Inf(1)
	}
	if simpleProgress == 1.0 {
		return math.Max(0, math.Floor(overallProgress)-1)
	}
	return math.Floor(overallProgress)
}

// isCurrentDirectionForwards mirrors TimingCalculations::IsCurrentDirectionForwards.
func isCurrentDirectionForwards(currentIteration float64, dir PlaybackDirection) bool {
	if dir == DirNormal {
		return true
	}
	if dir == DirReverse {
		return false
	}
	// Alternate: even iterations forwards, odd iterations reversed (and inverted
	// again for alternate-reverse).
	iterationIsOdd := false
	if !math.IsInf(currentIteration, 1) {
		iterationIsOdd = math.Mod(currentIteration, 2) != 0
	}
	if dir == DirAlternate {
		return !iterationIsOdd
	}
	// DirAlternateReverse
	return iterationIsOdd
}

// computeDirectedProgress mirrors TimingCalculations::CalculateDirectedProgress.
func computeDirectedProgress(simpleProgress, currentIteration float64, dir PlaybackDirection) float64 {
	if isCurrentDirectionForwards(currentIteration, dir) {
		return simpleProgress
	}
	return 1 - simpleProgress
}

// computeTransformedProgress mirrors
// TimingCalculations::CalculateTransformedProgress: applies the timing function
// with the correct limit direction.
func computeTransformedProgress(phase AnimationPhase, directedProgress float64, forwards bool, fn TimingFunction) float64 {
	before := phase == PhaseBefore
	if !forwards {
		before = phase == PhaseAfter
	}
	ld := limitRight
	if before {
		ld = limitLeft
	}
	if phase == PhaseAfter {
		const eps = 1e-6
		if forwards && math.Abs(directedProgress-1) < eps {
			directedProgress = 1
		} else if !forwards && math.Abs(directedProgress) < eps {
			directedProgress = 0
		}
	}
	if fn == nil {
		fn = LinearTimingFunction{}
	}
	return fn.Evaluate(directedProgress, ld)
}

// ResolvedTiming is the result of evaluating a Timing at a given local time: it
// carries everything ResolveKeyframeStyle needs to pick keyframes.
type ResolvedTiming struct {
	Phase            AnimationPhase
	InEffect         bool    // fill-mode gate: is the animation visible at this phase?
	Progress         float64 // transformed iteration progress in [0,1]
	CurrentIteration float64
	Forwards         bool // current direction is forwards
}

// Resolve evaluates the full static-snapshot timing pipeline at localTime
// (in a static render, localTime = -StartDelay relative to document time 0,
// i.e. we pass localTime = 0 - StartDelay... no: localTime is the document
// timeline time, which is 0). The phase/activeTime math already subtracts
// StartDelay, so callers pass the document timeline time (0).
func (t Timing) Resolve(localTime float64) ResolvedTiming {
	phase := t.ComputePhase(localTime)
	activeTime, resolved := t.computeActiveTime(localTime, phase)
	if !resolved {
		return ResolvedTiming{Phase: phase, InEffect: false}
	}
	overall := t.computeOverallProgress(activeTime, phase)
	simple := t.computeSimpleIterationProgress(overall, activeTime, phase)
	currentIter := t.computeCurrentIteration(overall, simple, phase)
	forwards := isCurrentDirectionForwards(currentIter, t.Direction)
	directed := computeDirectedProgress(simple, currentIter, t.Direction)
	transformed := computeTransformedProgress(phase, directed, forwards, t.TimingFunction)
	return ResolvedTiming{
		Phase:            phase,
		InEffect:         true,
		Progress:         transformed,
		CurrentIteration: currentIter,
		Forwards:         forwards,
	}
}

// --- parsing helpers: animation-* longhand values -> Timing fields ---

// parseAnimationDuration parses a single animation-duration value (seconds).
func parseAnimationDuration(v string) float64 {
	return parseTimeSeconds(v, 0)
}

// parseAnimationDelay parses a single animation-delay value (seconds).
func parseAnimationDelay(v string) float64 {
	return parseTimeSeconds(v, 0)
}

// parseTimeSeconds parses a CSS <time> value (e.g. "1s", "200ms") into seconds.
func parseTimeSeconds(v string, def float64) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	if strings.HasSuffix(v, "ms") {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v[:len(v)-2]), 64); err == nil {
			return f / 1000.0
		}
		return def
	}
	if strings.HasSuffix(v, "s") {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v[:len(v)-1]), 64); err == nil {
			return f
		}
		return def
	}
	// Unitless: per CSS spec invalid, but tolerate as seconds.
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return def
}

// parseIterationCount parses an animation-iteration-count value.
func parseIterationCount(v string) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "infinite" {
		return math.Inf(1)
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
		return f
	}
	return 1
}

// parseDirection parses an animation-direction value.
func parseDirection(v string) PlaybackDirection {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "reverse":
		return DirReverse
	case "alternate":
		return DirAlternate
	case "alternate-reverse":
		return DirAlternateReverse
	default:
		return DirNormal
	}
}

// parseFillMode parses an animation-fill-mode value.
func parseFillMode(v string) FillMode {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "forwards":
		return FillForwards
	case "backwards":
		return FillBackwards
	case "both":
		return FillBoth
	default:
		return FillNone
	}
}

// parseTimingFunction parses an animation-timing-function (or per-keyframe
// animation-timing-function) value into a TimingFunction. Mirrors the keyword
// set in platform/animation/timing_function.cc.
func parseTimingFunction(v string) TimingFunction {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", "linear":
		return LinearTimingFunction{}
	case "ease":
		return CubicBezierTimingFunction{0.25, 0.1, 0.25, 1.0}
	case "ease-in":
		return CubicBezierTimingFunction{0.42, 0.0, 1.0, 1.0}
	case "ease-out":
		return CubicBezierTimingFunction{0.0, 0.0, 0.58, 1.0}
	case "ease-in-out":
		return CubicBezierTimingFunction{0.42, 0.0, 0.58, 1.0}
	case "step-start":
		return StepsTimingFunction{Steps: 1, Position: StepJumpStart}
	case "step-end":
		return StepsTimingFunction{Steps: 1, Position: StepJumpEnd}
	}
	if strings.HasPrefix(v, "steps(") && strings.HasSuffix(v, ")") {
		inner := v[len("steps(") : len(v)-1]
		parts := strings.Split(inner, ",")
		steps := 1
		if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && n >= 1 {
			steps = n
		}
		pos := StepEnd
		if len(parts) > 1 {
			switch strings.TrimSpace(parts[1]) {
			case "start":
				pos = StepStart
			case "end":
				pos = StepEnd
			case "jump-start":
				pos = StepJumpStart
			case "jump-end":
				pos = StepJumpEnd
			case "jump-none":
				pos = StepJumpNone
			case "jump-both":
				pos = StepJumpBoth
			}
		}
		return StepsTimingFunction{Steps: steps, Position: pos}
	}
	if strings.HasPrefix(v, "cubic-bezier(") && strings.HasSuffix(v, ")") {
		inner := v[len("cubic-bezier(") : len(v)-1]
		parts := strings.Split(inner, ",")
		if len(parts) == 4 {
			nums := make([]float64, 4)
			ok := true
			for i, p := range parts {
				f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
				if err != nil {
					ok = false
					break
				}
				nums[i] = f
			}
			if ok {
				return CubicBezierTimingFunction{nums[0], nums[1], nums[2], nums[3]}
			}
		}
	}
	return LinearTimingFunction{}
}
