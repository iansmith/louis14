package css

import (
	"math"
	"strconv"
	"strings"
)

// CSS animation sampling for the static reftest renderer.
//
// The renderer is static: it produces a single frame. WPT animation reftests
// drive an animation to a fixed point with `animation-play-state: paused` plus
// a negative `animation-delay`, so the "current" frame is deterministic — the
// reference file is just the element with the equivalent static style. To
// match, ComputeStyle samples such paused animations: it resolves the active
// time, finds the surrounding keyframes, and interpolates the animated
// properties (currently the subset the filter-effects suite needs: `filter`).
//
// This mirrors Blink's animation pipeline conceptually (KeyframeEffect ->
// effective time -> interpolated computed value) but only for the static,
// paused case the reftests require.

// animationSpec holds the resolved animation properties for one node.
type animationSpec struct {
	name       string
	durationMS float64
	delayMS    float64
	iterations float64 // math.Inf for "infinite"
	timing     string  // "linear", "ease", "ease-in", "ease-out", "ease-in-out"
	direction  string  // "normal", "reverse", "alternate", "alternate-reverse"
	fillMode   string  // "none", "forwards", "backwards", "both"
	paused     bool
}

// resolveAnimationSpec extracts the animation properties from a computed
// style, honouring both the `animation` shorthand and the longhands. Returns
// nil when the element has no named animation.
func resolveAnimationSpec(s *Style) *animationSpec {
	spec := &animationSpec{
		durationMS: 0,
		iterations: 1,
		timing:     "ease",
		direction:  "normal",
		fillMode:   "none",
	}

	// Parse the shorthand first (lowest priority); longhands override.
	if sh, ok := s.Get("animation"); ok && sh != "" && sh != "none" {
		parseAnimationShorthand(sh, spec)
	}
	if v, ok := s.Get("animation-name"); ok && v != "" {
		if v == "none" {
			return nil
		}
		spec.name = strings.TrimSpace(v)
	}
	if v, ok := s.Get("animation-duration"); ok && v != "" {
		spec.durationMS = parseTimeMS(v)
	}
	if v, ok := s.Get("animation-delay"); ok && v != "" {
		spec.delayMS = parseTimeMS(v)
	}
	if v, ok := s.Get("animation-timing-function"); ok && v != "" {
		spec.timing = strings.TrimSpace(strings.ToLower(v))
	}
	if v, ok := s.Get("animation-direction"); ok && v != "" {
		spec.direction = strings.TrimSpace(strings.ToLower(v))
	}
	if v, ok := s.Get("animation-fill-mode"); ok && v != "" {
		spec.fillMode = strings.TrimSpace(strings.ToLower(v))
	}
	if v, ok := s.Get("animation-iteration-count"); ok && v != "" {
		spec.iterations = parseIterationCount(v)
	}
	if v, ok := s.Get("animation-play-state"); ok {
		spec.paused = strings.TrimSpace(strings.ToLower(v)) == "paused"
	}

	if spec.name == "" || spec.name == "none" {
		return nil
	}
	return spec
}

// parseAnimationShorthand parses the `animation` shorthand into spec. The
// shorthand is order-tolerant except: the first <time> is duration, the second
// is delay; an unrecognised token is taken as the name.
func parseAnimationShorthand(sh string, spec *animationSpec) {
	toks := strings.Fields(sh)
	timesSeen := 0
	for _, t := range toks {
		lt := strings.ToLower(t)
		switch {
		case isTimeToken(lt):
			if timesSeen == 0 {
				spec.durationMS = parseTimeMS(t)
			} else {
				spec.delayMS = parseTimeMS(t)
			}
			timesSeen++
		case lt == "linear" || lt == "ease" || lt == "ease-in" || lt == "ease-out" || lt == "ease-in-out" || strings.HasPrefix(lt, "cubic-bezier") || strings.HasPrefix(lt, "steps"):
			spec.timing = lt
		case lt == "normal" || lt == "reverse" || lt == "alternate" || lt == "alternate-reverse":
			spec.direction = lt
		case lt == "none" || lt == "forwards" || lt == "backwards" || lt == "both":
			spec.fillMode = lt
		case lt == "infinite":
			spec.iterations = posInf()
		case lt == "running" || lt == "paused":
			spec.paused = lt == "paused"
		case isNumberToken(lt):
			if n, err := strconv.ParseFloat(lt, 64); err == nil {
				spec.iterations = n
			}
		default:
			if spec.name == "" {
				spec.name = t
			}
		}
	}
}

func isTimeToken(t string) bool {
	return strings.HasSuffix(t, "ms") || strings.HasSuffix(t, "s")
}

func isNumberToken(t string) bool {
	_, err := strconv.ParseFloat(t, 64)
	return err == nil
}

// parseTimeMS parses a CSS <time> value to milliseconds.
func parseTimeMS(v string) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	if strings.HasSuffix(v, "ms") {
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "ms"), 64)
		return f
	}
	if strings.HasSuffix(v, "s") {
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "s"), 64)
		return f * 1000
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func parseIterationCount(v string) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "infinite" {
		return posInf()
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 1
	}
	return f
}

// applyAnimationSampling samples any paused animation on the node's style at
// its deterministic current time and overwrites the animated properties with
// the interpolated values. Called by ComputeStyle after the cascade. Only the
// `filter` property is interpolated (the subset the filter-effects reftests
// need); other properties are left at their cascaded value.
func applyAnimationSampling(s *Style, stylesheets []*Stylesheet) {
	spec := resolveAnimationSpec(s)
	if spec == nil || spec.durationMS <= 0 {
		return
	}
	// The static renderer only produces a deterministic frame for a paused
	// animation. A running animation has no well-defined frame; leave it.
	if !spec.paused {
		return
	}

	var frames []KeyframeRule
	for _, sheet := range stylesheets {
		if sheet.Keyframes == nil {
			continue
		}
		if f, ok := sheet.Keyframes[spec.name]; ok {
			frames = f
		}
	}
	if len(frames) == 0 {
		return
	}

	// Active time = -delay (the paused playhead). Negative delay fast-forwards
	// into the animation; the playhead sits at max(0, -delay) within the
	// timeline. The reftests use animation-delay: -Ns with duration 2N so the
	// playhead is at the timeline midpoint.
	playhead := -spec.delayMS
	if playhead < 0 {
		playhead = 0
	}

	// Resolve iteration & local progress.
	iteration := 0.0
	localTime := playhead
	if spec.durationMS > 0 {
		iteration = playhead / spec.durationMS
		localTime = playhead - float64(int(iteration))*spec.durationMS
	}
	// Clamp to the final frame once iterations are exhausted.
	if !isInf(spec.iterations) && iteration >= spec.iterations {
		localTime = spec.durationMS
	}
	progress := localTime / spec.durationMS
	if progress > 1 {
		progress = 1
	}

	// Direction handling.
	switch spec.direction {
	case "reverse":
		progress = 1 - progress
	case "alternate":
		if int(iteration)%2 == 1 {
			progress = 1 - progress
		}
	case "alternate-reverse":
		if int(iteration)%2 == 0 {
			progress = 1 - progress
		}
	}

	// Easing applies between the surrounding keyframes; the reftests use
	// linear. Both filter and backdrop-filter accept the same value syntax
	// and interpolate identically.
	applyAnimatedProperty(s, "filter", frames, progress, spec.timing)
	applyAnimatedProperty(s, "backdrop-filter", frames, progress, spec.timing)
}

// applyAnimatedProperty finds the keyframe interval surrounding progress for
// the given property and writes the interpolated value into the style.
func applyAnimatedProperty(s *Style, prop string, frames []KeyframeRule, progress float64, timing string) {
	type stop struct {
		offset float64
		value  string
		ok     bool
	}
	var stops []stop
	for _, kf := range frames {
		off, valid := keyframeOffset(kf.Stop)
		if !valid {
			continue
		}
		v, has := kf.Declarations[prop]
		st := stop{offset: off}
		if has {
			st.value = strings.TrimSpace(v)
			st.ok = true
		}
		// Also honour the `animation` / `filter` written via shorthand inside
		// a keyframe — keyframes only carry longhands here, so prop lookup is
		// enough.
		stops = append(stops, st)
	}
	if len(stops) == 0 {
		return
	}
	// Sort by offset.
	for i := 1; i < len(stops); i++ {
		for j := i; j > 0 && stops[j-1].offset > stops[j].offset; j-- {
			stops[j-1], stops[j] = stops[j], stops[j-1]
		}
	}
	// Find surrounding stops that declare the property.
	var lower, upper *stop
	for i := range stops {
		if !stops[i].ok {
			continue
		}
		if stops[i].offset <= progress {
			lower = &stops[i]
		}
		if stops[i].offset >= progress && upper == nil {
			upper = &stops[i]
		}
	}
	if lower == nil && upper == nil {
		return
	}
	if lower == nil {
		s.Set(prop, upper.value)
		return
	}
	if upper == nil {
		s.Set(prop, lower.value)
		return
	}
	if lower == upper || upper.offset == lower.offset {
		s.Set(prop, upper.value)
		return
	}
	// Local fraction between the two stops, eased.
	frac := (progress - lower.offset) / (upper.offset - lower.offset)
	frac = applyEasing(frac, timing)
	if prop == "filter" || prop == "backdrop-filter" {
		if interp, ok := interpolateFilterValue(lower.value, upper.value, frac); ok {
			s.Set(prop, interp)
			return
		}
	}
	// Fallback: discrete — snap at the 50% mark.
	if frac < 0.5 {
		s.Set(prop, lower.value)
	} else {
		s.Set(prop, upper.value)
	}
}

// keyframeOffset converts a keyframe selector ("from", "to", "50%") to a
// [0,1] offset.
func keyframeOffset(stop string) (float64, bool) {
	stop = strings.TrimSpace(strings.ToLower(stop))
	switch stop {
	case "from":
		return 0, true
	case "to":
		return 1, true
	}
	if strings.HasSuffix(stop, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(stop, "%"), 64)
		if err == nil {
			return f / 100.0, true
		}
	}
	return 0, false
}

// applyEasing applies a CSS timing function to a [0,1] fraction. Only the
// keyword functions the reftests use are implemented precisely; others fall
// back to linear.
func applyEasing(t float64, timing string) float64 {
	switch strings.ToLower(strings.TrimSpace(timing)) {
	case "linear", "":
		return t
	case "ease":
		return cubicBezier(0.25, 0.1, 0.25, 1.0, t)
	case "ease-in":
		return cubicBezier(0.42, 0, 1.0, 1.0, t)
	case "ease-out":
		return cubicBezier(0, 0, 0.58, 1.0, t)
	case "ease-in-out":
		return cubicBezier(0.42, 0, 0.58, 1.0, t)
	default:
		return t
	}
}

// cubicBezier evaluates a cubic-bezier easing curve at x = t (the X axis is
// the input fraction), returning the Y value. Uses Newton iteration to invert
// X(s) = t, then evaluates Y(s).
func cubicBezier(x1, y1, x2, y2, t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	bezX := func(s float64) float64 {
		u := 1 - s
		return 3*u*u*s*x1 + 3*u*s*s*x2 + s*s*s
	}
	bezY := func(s float64) float64 {
		u := 1 - s
		return 3*u*u*s*y1 + 3*u*s*s*y2 + s*s*s
	}
	// Invert X to find s.
	s := t
	for i := 0; i < 20; i++ {
		x := bezX(s) - t
		if x > -1e-6 && x < 1e-6 {
			break
		}
		// Derivative dX/ds.
		u := 1 - s
		d := 3*u*u*x1 + 6*u*s*(x2-x1) + 3*s*s*(1-x2)
		if d == 0 {
			break
		}
		s -= x / d
		if s < 0 {
			s = 0
		}
		if s > 1 {
			s = 1
		}
	}
	return bezY(s)
}

// posInf / isInf wrap math helpers for the iteration-count "infinite" case.
func posInf() float64      { return math.Inf(1) }
func isInf(v float64) bool { return math.IsInf(v, 0) }

// interpolateFilterValue interpolates between two CSS filter-value-list
// strings at fraction frac. Per CSS Filter Effects animation rules, when one
// side is `none` it is treated as the list of identity filter functions
// matching the other side (blur(0), brightness(1), etc.). Two lists with the
// same functions in the same order interpolate component-wise. Returns
// (value, true) on success.
func interpolateFilterValue(from, to string, frac float64) (string, bool) {
	a := parseFilterList(normalizeFilterNone(from))
	b := parseFilterList(normalizeFilterNone(to))
	fromNone := strings.TrimSpace(strings.ToLower(from)) == "none"
	toNone := strings.TrimSpace(strings.ToLower(to)) == "none"

	// If one side is `none`, synthesize an identity list matching the other.
	if fromNone && !toNone {
		a = identityFilterList(b)
	}
	if toNone && !fromNone {
		b = identityFilterList(a)
	}
	if len(a) != len(b) || len(a) == 0 {
		return "", false
	}
	var out []string
	for i := range a {
		if a[i].Name != b[i].Name {
			return "", false
		}
		s, ok := interpolateFilterFunction(a[i], b[i], frac)
		if !ok {
			return "", false
		}
		out = append(out, s)
	}
	return strings.Join(out, " "), true
}

// normalizeFilterNone keeps `none` as-is; parseFilterList returns an empty
// list for it, which the caller replaces with an identity list.
func normalizeFilterNone(v string) string {
	if strings.TrimSpace(strings.ToLower(v)) == "none" {
		return ""
	}
	return v
}

// identityFilterList returns, for each function in ref, the identity
// FilterFunction of the same kind (the value `none` interpolates against).
func identityFilterList(ref []FilterFunction) []FilterFunction {
	out := make([]FilterFunction, len(ref))
	for i, f := range ref {
		id := FilterFunction{Name: f.Name}
		switch f.Name {
		case "blur":
			id.Value = 0
		case "hue-rotate":
			id.Value = 0
		case "drop-shadow":
			id.ShadowOffsetX = 0
			id.ShadowOffsetY = 0
			id.ShadowBlur = 0
			id.ShadowColor = Color{0, 0, 0, 0}
			id.ShadowUseCurrentColor = f.ShadowUseCurrentColor
		default:
			// The "no-op" value for each function: grayscale/sepia/invert
			// have identity 0; brightness/contrast/saturate/opacity have
			// identity 1. `none` interpolates against these.
			switch f.Name {
			case "brightness", "contrast", "saturate", "opacity":
				id.Value = 1
			default:
				id.Value = 0
			}
		}
		out[i] = id
	}
	return out
}

// interpolateFilterFunction interpolates one filter function between a and b.
func interpolateFilterFunction(a, b FilterFunction, t float64) (string, bool) {
	lerp := func(x, y float64) float64 { return x + (y-x)*t }
	switch a.Name {
	case "blur":
		return "blur(" + formatPx(lerp(a.Value, b.Value)) + ")", true
	case "hue-rotate":
		return "hue-rotate(" + formatNum(lerp(a.Value, b.Value)) + "deg)", true
	case "grayscale", "sepia", "invert", "opacity", "brightness", "contrast", "saturate":
		return a.Name + "(" + formatNum(lerp(a.Value, b.Value)) + ")", true
	case "drop-shadow":
		dx := lerp(a.ShadowOffsetX, b.ShadowOffsetX)
		dy := lerp(a.ShadowOffsetY, b.ShadowOffsetY)
		bl := lerp(a.ShadowBlur, b.ShadowBlur)
		cr := uint8(lerp(float64(a.ShadowColor.R), float64(b.ShadowColor.R)))
		cg := uint8(lerp(float64(a.ShadowColor.G), float64(b.ShadowColor.G)))
		cb := uint8(lerp(float64(a.ShadowColor.B), float64(b.ShadowColor.B)))
		ca := lerp(a.ShadowColor.A, b.ShadowColor.A)
		return "drop-shadow(" + formatPx(dx) + " " + formatPx(dy) + " " + formatPx(bl) +
			" rgba(" + formatNum(float64(cr)) + "," + formatNum(float64(cg)) + "," +
			formatNum(float64(cb)) + "," + formatNum(ca) + "))", true
	}
	return "", false
}

func formatPx(v float64) string  { return formatNum(v) + "px" }
func formatNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
