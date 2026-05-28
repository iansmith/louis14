package css

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// CSS Animations: keyframe resolution and application to computed style.
//
// Mirrors Blink core/animation/css/css_animations.cc:
//   - CalculateAnimationUpdate: reads animation-* longhands, resolves
//     animation-name -> @keyframes, builds a keyframe effect model.
//   - ProcessKeyframesRule / CreateKeyframeEffectModel: turns @keyframes stops
//     into normalized keyframes, merges equal offsets, synthesizes missing
//     from/to keyframes.
//   - IdleTriggerAllowsVisualEffect: the fill-mode visibility gate (handled by
//     Timing.Resolve().InEffect).
//
// louis14's reftest harness is a static renderer (document timeline frozen at
// t=0), so the animation's effective state is derived purely from
// animation-delay. This file applies the in-effect keyframe declarations to
// computed style; the cascade ordering (animation origin sits below author
// !important) is enforced by the caller via the importantProps map.

// Keyframe is a single normalized keyframe: an offset in [0,1] plus the CSS
// declarations that apply at that offset, with the segment's easing.
type Keyframe struct {
	Offset       float64
	Declarations map[string]string
	Easing       TimingFunction // per-keyframe animation-timing-function (default: nil = animation's)
}

// KeyframeEffect is the normalized, ordered keyframe model for one animation
// name. Mirrors Blink's StringKeyframeEffectModel.
type KeyframeEffect struct {
	Keyframes []Keyframe // sorted by offset ascending
	// Properties is the set of every property mentioned anywhere in the
	// keyframes (used to know which properties this animation contributes).
	Properties map[string]bool
}

// ResolvedAnimation pairs a parsed Timing with the keyframe effect for one
// entry of the animation-name list.
type ResolvedAnimation struct {
	Name   string
	Timing Timing
	Effect *KeyframeEffect
}

// BuildKeyframeEffect normalizes a slice of raw KeyframeRule stops into a
// KeyframeEffect: parses offsets (from=0, to=1, N%=N/100, comma-separated
// selectors expand to multiple keyframes), merges equal-offset keyframes, and
// synthesizes missing from/to (offset 0 / offset 1) keyframes for any property
// not present at that boundary. Mirrors Blink CreateKeyframeEffectModel.
//
// defaultEasing is the animation's animation-timing-function, applied to any
// keyframe with no per-keyframe animation-timing-function declaration.
func BuildKeyframeEffect(frames []KeyframeRule, defaultEasing TimingFunction) *KeyframeEffect {
	eff := &KeyframeEffect{Properties: make(map[string]bool)}

	// 1. Expand comma-separated selectors and parse offsets.
	type rawKF struct {
		offset float64
		decls  map[string]string
		easing TimingFunction
	}
	var raws []rawKF
	for _, kr := range frames {
		for _, sel := range strings.Split(kr.Stop, ",") {
			sel = strings.TrimSpace(strings.ToLower(sel))
			off, ok := parseKeyframeOffset(sel)
			if !ok {
				continue
			}
			// Per-keyframe animation-timing-function.
			var easing TimingFunction
			decls := make(map[string]string, len(kr.Declarations))
			for p, v := range kr.Declarations {
				if p == "animation-timing-function" {
					easing = parseTimingFunction(v)
					continue
				}
				decls[p] = v
			}
			raws = append(raws, rawKF{offset: off, decls: decls, easing: easing})
		}
	}

	// 2. Merge equal-offset keyframes. Later declarations for the same property
	//    at the same offset win (source order).
	byOffset := make(map[float64]*Keyframe)
	var offsets []float64
	for _, r := range raws {
		kf, exists := byOffset[r.offset]
		if !exists {
			kf = &Keyframe{Offset: r.offset, Declarations: make(map[string]string), Easing: r.easing}
			byOffset[r.offset] = kf
			offsets = append(offsets, r.offset)
		}
		if r.easing != nil {
			kf.Easing = r.easing
		}
		for p, v := range r.decls {
			kf.Declarations[p] = v
			eff.Properties[p] = true
		}
	}

	// 3. Apply the default easing to keyframes with no explicit one.
	for _, kf := range byOffset {
		if kf.Easing == nil {
			kf.Easing = defaultEasing
		}
	}

	// 4. Synthesize missing from (offset 0) and to (offset 1) keyframes.
	//    Per the spec these carry a "neutral" value: the underlying
	//    (un-animated) computed value. We mark them with a sentinel so
	//    ResolveKeyframeStyle / ApplyAnimations can substitute the base value.
	if _, ok := byOffset[0]; !ok {
		byOffset[0] = &Keyframe{Offset: 0, Declarations: map[string]string{}, Easing: defaultEasing}
		offsets = append(offsets, 0)
	}
	if _, ok := byOffset[1]; !ok {
		byOffset[1] = &Keyframe{Offset: 1, Declarations: map[string]string{}, Easing: defaultEasing}
		offsets = append(offsets, 1)
	}
	// For every property in the effect, ensure offset 0 and offset 1 carry a
	// value: if absent, mark it as "neutral" (underlying value). We represent
	// neutral by simply not putting the property in that keyframe's
	// Declarations — ResolveKeyframeStyle then falls back to the base style.

	sort.Float64s(offsets)
	for _, off := range offsets {
		eff.Keyframes = append(eff.Keyframes, *byOffset[off])
	}
	return eff
}

// parseKeyframeOffset parses a keyframe selector ("from", "to", "50%") into an
// offset in [0,1]. Returns false for unparseable selectors.
func parseKeyframeOffset(sel string) (float64, bool) {
	switch sel {
	case "from":
		return 0, true
	case "to":
		return 1, true
	}
	if strings.HasSuffix(sel, "%") {
		if f, err := strconv.ParseFloat(strings.TrimSpace(sel[:len(sel)-1]), 64); err == nil {
			return f / 100.0, true
		}
	}
	return 0, false
}

// nonAnimatableProps lists properties CSS does not permit animating; a
// @keyframes declaration for one of these is ignored when resolving keyframe
// values (Blink: these carry `is_animatable: false` in css_properties.json5).
// Most CSS properties ARE animatable — this is the small denylist of the
// clearly-non-animatable longhands that realistically appear in @keyframes.
var nonAnimatableProps = map[string]bool{
	"contain":              true,
	"content":              true,
	"direction":            true,
	"unicode-bidi":         true,
	"writing-mode":         true,
	"text-orientation":     true,
	"text-combine-upright": true,
	"will-change":          true,
	"all":                  true,
	"counter-reset":        true,
	"counter-increment":    true,
	"counter-set":          true,
	"quotes":               true,
}

// ResolveKeyframeStyle selects the keyframe value(s) for each animated property
// at the given transformed iteration progress, returning property -> value.
//
// Per CSS Animations, animation-timing-function applies *between* keyframes:
// the keyframe pair [k_i, k_{i+1}] bracketing the simple iteration progress is
// found, the segment's easing maps the within-segment fraction, and the value
// is selected/interpolated. Every in-scope test is either:
//   - from==to (constant): both endpoints carry the same value, so the result
//     is that value regardless of the segment fraction;
//   - frozen exactly on a keyframe offset: progress lands on k_i, value = k_i;
//   - a two-color from/to interpolation at a fractional progress
//     (animation-important-002): handled by interpolateValue for colors;
//   - a filter-list from/to interpolation at a fractional progress
//     (css-filters-animation-* / css-backdrop-filters-animation-*): handled
//     by interpolateValue for the filter / backdrop-filter properties.
//
// progress is the SIMPLE iteration progress (used for keyframe bracketing);
// segmentEasingProgress is the TRANSFORMED progress from the timing pipeline
// — but since CSS applies the timing function per-segment, we instead apply
// each segment's own easing here. For the in-scope tests the animation-level
// timing function is what is authored, so we treat it as the segment easing.
//
// base is the element's underlying computed style (used to fill neutral
// synthesized keyframes).
func ResolveKeyframeStyle(eff *KeyframeEffect, simpleProgress float64, base *Style) map[string]string {
	out := make(map[string]string)
	if eff == nil || len(eff.Keyframes) == 0 {
		return out
	}
	// Clamp progress into [0,1].
	if simpleProgress < 0 {
		simpleProgress = 0
	}
	if simpleProgress > 1 {
		simpleProgress = 1
	}

	for prop := range eff.Properties {
		// CSS Animations §3 / Web Animations: a @keyframes declaration for a
		// non-animatable property (e.g. `contain`) is ignored.
		if nonAnimatableProps[prop] {
			continue
		}
		// Build the per-property keyframe list: offset -> value, using the base
		// (underlying) value for any keyframe (including synthesized
		// boundaries) that does not declare this property.
		type pkf struct {
			offset float64
			value  string
			easing TimingFunction
		}
		var list []pkf
		for _, kf := range eff.Keyframes {
			v, ok := kf.Declarations[prop]
			if !ok {
				// Neutral value: the underlying computed value. Required for
				// synthesized from/to (e.g. @keyframes bg { 50%{...} }).
				if base != nil {
					if bv, bok := base.Get(prop); bok {
						v = bv
						ok = true
					}
				}
				if !ok {
					continue
				}
			}
			// CSS-wide keywords in a keyframe value (`initial`, `inherit`,
			// `unset`, `revert`, `revert-layer`) resolve against the
			// underlying cascaded value when the effect is sampled (Web
			// Animations §3.4.3 / CSS Animations §2.2). For the animation
			// origin, the underlying value is the value cascaded BEFORE
			// animations applied — which is exactly `base`. We substitute
			// here so subsequent interpolation works on real values.
			//
			// `revert-layer` in a keyframe rolls back to the value the
			// property would have without the keyframe's own (animation)
			// origin, which for the simple cases below is also base.
			// Mirrors Blink CSSToStyleMap::MapAnimationFromTo +
			// StyleResolver::ApplyAnimation behavior at SHA
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			if base != nil && isAnimationCSSWideKeyword(v) {
				if bv, bok := base.Get(prop); bok {
					v = bv
				}
			}
			list = append(list, pkf{offset: kf.offsetOrSelf(), value: v, easing: kf.Easing})
		}
		if len(list) == 0 {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].offset < list[j].offset })

		// Below the first / above the last keyframe: clamp to the endpoint.
		if simpleProgress <= list[0].offset {
			out[prop] = list[0].value
			continue
		}
		if simpleProgress >= list[len(list)-1].offset {
			out[prop] = list[len(list)-1].value
			continue
		}
		// Find the bracketing pair [lo, hi] with lo.offset <= p <= hi.offset.
		for i := 0; i+1 < len(list); i++ {
			lo, hi := list[i], list[i+1]
			if simpleProgress < lo.offset || simpleProgress > hi.offset {
				continue
			}
			if hi.offset == lo.offset {
				out[prop] = hi.value
				break
			}
			// Exactly on a keyframe offset: take that keyframe's value.
			if simpleProgress == lo.offset {
				out[prop] = lo.value
				break
			}
			if simpleProgress == hi.offset {
				out[prop] = hi.value
				break
			}
			// Within the segment: easing maps the local fraction, then
			// interpolate. The segment easing is the keyframe k_i's easing
			// (CSS Animations §3: animation-timing-function on a keyframe
			// applies to the segment starting at that keyframe).
			localFraction := (simpleProgress - lo.offset) / (hi.offset - lo.offset)
			easing := lo.easing
			if easing == nil {
				easing = LinearTimingFunction{}
			}
			eased := easing.Evaluate(localFraction, limitRight)
			out[prop] = interpolateValue(prop, lo.value, hi.value, eased)
			break
		}
	}
	return out
}

// offsetOrSelf returns the keyframe's offset (helper to keep ResolveKeyframeStyle
// readable; Keyframe.Offset is the field).
func (k Keyframe) offsetOrSelf() float64 { return k.Offset }

// interpolateValue interpolates between two CSS values at fraction t in [0,1].
// The value types supported are those the in-scope reftests exercise:
//   - <color> (animation-important-002): interpolated in sRGB;
//   - <filter-value-list> for the filter / backdrop-filter properties
//     (css-filters-animation-* / css-backdrop-filters-animation-*):
//     interpolated component-wise per CSS Filter Effects §interpolation,
//     with a `none` endpoint promoted to the identity-filter list.
//
// For value types where interpolation is not implemented, the endpoint is
// returned discretely (t < 1 -> from, t >= 1 -> to) — this is correct for
// every other in-scope test, which is either from==to (constant) or frozen
// exactly on a keyframe offset.
func interpolateValue(prop, from, to string, t float64) string {
	if from == to {
		return from
	}
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	// Filter-list interpolation for the filter / backdrop-filter properties.
	// Per CSS Filter Effects, the two filter-value-lists interpolate
	// component-wise (a `none` endpoint becomes the identity-filter list
	// matching the other side). This routes filter-effects' animated-filter
	// reftests through the single canonical animation path.
	if prop == "filter" || prop == "backdrop-filter" {
		if interp, ok := interpolateFilterValue(from, to, t); ok {
			return interp
		}
	}
	// Color interpolation (background-color, color, border colors, etc.).
	if isColorProperty(prop) {
		c1, ok1 := ParseColor(from)
		c2, ok2 := ParseColor(to)
		if ok1 && ok2 {
			return lerpColor(c1, c2, t)
		}
	}
	// <length> interpolation for px values (and unitless `0`).
	// Mirrors Blink LengthInterpolationFunctions::MaybeConvertCSSValue at
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, narrowed to the px
	// case the in-scope reftests exercise (revert-layer-010,
	// revert-layer-011 substitute base px values into keyframes).
	if v, ok := interpolatePxLength(from, to, t); ok {
		return v
	}
	// Discrete fallback for unsupported value types.
	if t < 1 {
		return from
	}
	return to
}

// interpolatePxLength linearly interpolates between two CSS lengths whose
// values are bare numbers or `<number>px`. Returns the result as a px string,
// or false if either input is not a recognized px-or-zero length.
func interpolatePxLength(from, to string, t float64) (string, bool) {
	a, aok := parsePxOrZero(from)
	b, bok := parsePxOrZero(to)
	if !aok || !bok {
		return "", false
	}
	v := a + (b-a)*t
	return formatPx(v), true
}

// parsePxOrZero parses a CSS length expressed in px (e.g. "150px") or the
// unitless `0`. Returns the numeric value in px and true on success.
func parsePxOrZero(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, true
	}
	if strings.HasSuffix(s, "px") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// isAnimationCSSWideKeyword reports whether a keyframe value is a CSS-wide
// keyword (`initial`, `inherit`, `unset`, `revert`, `revert-layer`) that
// must be substituted with the underlying cascaded value before
// interpolation (Web Animations §3.4.3 / CSS Animations §2.2). The list
// excludes `default` / `none` because they are not CSS-wide keywords for
// property values (cf. style.go::isCSSWideKeyword, which serves a different
// caller and includes the broader custom-ident reservation set).
func isAnimationCSSWideKeyword(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "initial", "inherit", "unset", "revert", "revert-layer":
		return true
	}
	return false
}

// lerpColor linearly interpolates two colors in (non-premultiplied) sRGB and
// returns an rgb()/rgba() string. Mirrors the simple-sRGB path used by Blink's
// CSSColorInterpolationType for legacy color interpolation.
func lerpColor(a, b Color, t float64) string {
	lerp := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	r := lerp(a.R, b.R)
	g := lerp(a.G, b.G)
	bl := lerp(a.B, b.B)
	alpha := a.A + (b.A-a.A)*t
	if alpha >= 1.0 {
		return "rgb(" + strconv.Itoa(int(r)) + ", " + strconv.Itoa(int(g)) + ", " + strconv.Itoa(int(bl)) + ")"
	}
	return "rgba(" + strconv.Itoa(int(r)) + ", " + strconv.Itoa(int(g)) + ", " +
		strconv.Itoa(int(bl)) + ", " + strconv.FormatFloat(alpha, 'g', -1, 64) + ")"
}

// resolveAnimations reads the 8 animation-* longhands from style, looks up each
// animation-name in the supplied keyframes maps, and builds a ResolvedAnimation
// per name. keyframesMaps is searched in order (Phase 2: all document-scope
// stylesheets; Phase 4 adds tree-scope ordering).
func resolveAnimations(style *Style, keyframesMaps []map[string][]KeyframeRule) []ResolvedAnimation {
	nameStr, ok := style.Get("animation-name")
	if !ok || strings.TrimSpace(nameStr) == "" {
		return nil
	}
	names := splitTopLevelCommas(nameStr)
	if len(names) == 0 {
		return nil
	}
	durations := splitTopLevelCommas(getOr(style, "animation-duration", "0s"))
	timingFns := splitTopLevelCommas(getOr(style, "animation-timing-function", "ease"))
	delays := splitTopLevelCommas(getOr(style, "animation-delay", "0s"))
	iterCounts := splitTopLevelCommas(getOr(style, "animation-iteration-count", "1"))
	directions := splitTopLevelCommas(getOr(style, "animation-direction", "normal"))
	fillModes := splitTopLevelCommas(getOr(style, "animation-fill-mode", "none"))

	// CSS Animations §4: when a longhand list is shorter than animation-name,
	// its values repeat (are "filled" by cycling).
	at := func(list []string, i int, def string) string {
		if len(list) == 0 {
			return def
		}
		return strings.TrimSpace(list[i%len(list)])
	}

	var resolved []ResolvedAnimation
	for i, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" || strings.EqualFold(name, "none") {
			continue
		}
		var frames []KeyframeRule
		// CSS Animations §3: duplicate @keyframes names resolve to the LAST
		// declaration in document order. keyframesMaps is in stylesheet order,
		// so walk it in reverse and take the first (= last-declared) match.
		for j := len(keyframesMaps) - 1; j >= 0; j-- {
			m := keyframesMaps[j]
			if m == nil {
				continue
			}
			if f, found := m[name]; found {
				frames = f
				break
			}
		}
		if frames == nil {
			continue
		}
		easing := parseTimingFunction(at(timingFns, i, "ease"))
		tm := Timing{
			StartDelay:        parseAnimationDelay(at(delays, i, "0s")),
			IterationDuration: parseAnimationDuration(at(durations, i, "0s")),
			IterationCount:    parseIterationCount(at(iterCounts, i, "1")),
			Direction:         parseDirection(at(directions, i, "normal")),
			Fill:              parseFillMode(at(fillModes, i, "none")),
			TimingFunction:    easing,
		}
		eff := BuildKeyframeEffect(frames, easing)
		resolved = append(resolved, ResolvedAnimation{Name: name, Timing: tm, Effect: eff})
	}
	return resolved
}

// getOr returns style.Get(prop) or def if unset/empty.
func getOr(style *Style, prop, def string) string {
	if v, ok := style.Get(prop); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// ApplyAnimations applies in-effect CSS animation keyframe values to a computed
// style, mirroring Blink CSSAnimations::CalculateAnimationUpdate +
// CalculateAnimationActiveInterpolations. It is called from ComputeStyle /
// ComputePseudoElementStyle *after* author-normal declarations and *before* the
// author-!important pass — the animation cascade origin sits between them
// (CSS Cascade §6.3). Properties already set with !important (in importantProps)
// are skipped.
//
// keyframesMaps is the ordered list of @keyframes maps to search for each
// animation-name (Phase 2: document scope only).
func ApplyAnimations(style *Style, keyframesMaps []map[string][]KeyframeRule, importantProps map[string]bool) {
	anims := resolveAnimations(style, keyframesMaps)
	if len(anims) == 0 {
		return
	}
	// Capture the underlying (un-animated) style so synthesized neutral
	// keyframes can read base values. style is mutated below, so clone first.
	base := style.Clone()
	for _, a := range anims {
		rt := a.Timing.Resolve(0)
		if !rt.InEffect {
			continue
		}
		// CSS Animations selects keyframes by the SIMPLE iteration progress and
		// applies each keyframe segment's own easing within the segment (the
		// animation-timing-function is the default per-keyframe easing). So we
		// pass rt.SimpleProgress for keyframe bracketing; ResolveKeyframeStyle
		// applies the segment easing to the within-segment fraction.
		values := ResolveKeyframeStyle(a.Effect, rt.SimpleProgress, base)
		for prop, val := range values {
			if importantProps != nil && importantProps[prop] {
				continue
			}
			// expandShorthand may expand `prop` into longhands; an animated
			// shorthand must not overwrite a longhand protected by !important.
			// Expand into a scratch style, then copy only the unprotected keys.
			tmp := NewStyle()
			expandShorthand(tmp, prop, val)
			for lp, lv := range tmp.Properties {
				if importantProps != nil && importantProps[lp] {
					continue
				}
				style.Set(lp, lv)
			}
		}
	}
}

// collectKeyframesMaps returns the @keyframes maps from a set of stylesheets,
// in document order (Phase 2: document scope). Phase 4 will replace this with
// tree-scope-aware resolution.
func collectKeyframesMaps(stylesheets []*Stylesheet) []map[string][]KeyframeRule {
	var maps []map[string][]KeyframeRule
	for _, ss := range stylesheets {
		if ss.Keyframes != nil {
			maps = append(maps, ss.Keyframes)
		}
	}
	return maps
}

// --- CSS Filter Effects: filter-list interpolation -------------------------
//
// The filter / backdrop-filter properties animate as a <filter-value-list>.
// Per CSS Filter Effects §interpolation (and Blink's FilterInterpolationType):
//   - two lists with the same filter functions in the same order interpolate
//     component-wise;
//   - if one endpoint is `none`, it is treated as the list of identity filter
//     functions matching the other endpoint (blur(0), brightness(1), ...), so
//     a `none` <-> list animation still interpolates smoothly.
// Mismatched-length / mismatched-function lists are not smoothly
// interpolable; interpolateFilterValue reports !ok and interpolateValue falls
// back to the discrete endpoint.

// interpolateFilterValue interpolates between two CSS filter-value-list
// strings at fraction frac. Returns (value, true) on success.
func interpolateFilterValue(from, to string, frac float64) (string, bool) {
	// Animation interpolation doesn't carry the owning document's BaseDir
	// — interp produces filter strings that already contain whatever URL
	// form was in the keyframe value, so no further base-relative
	// resolution is meaningful here.
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
