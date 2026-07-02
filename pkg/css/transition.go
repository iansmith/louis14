package css

import (
	"strings"

	"louis14/pkg/html"
)

// CSS Transitions Level 1 — first-frame semantics for louis14's static
// renderer.
//
// louis14 paints exactly one frame after script execution: the second layout
// pass in the render pipelines (pkg/resource/renderer.go Render,
// pkg/visualtest/helpers.go) re-cascades the mutated DOM from scratch. A
// transition's only observable contribution in that model is its value at
// local time 0:
//
//   - during transition-delay the before-change value shows;
//   - steps(N, start)-family easings have already jumped (transformed
//     progress at t=0 is the steps-start offset, e.g. 1/N);
//   - continuous easings (linear/ease/cubic-bezier) evaluate to 0 at t=0 —
//     the before-change value shows.
//
// Mirrors Blink CSSAnimations::CalculateTransitionUpdate /
// CalculateTransitionUpdateForStandardProperty /
// CalculateTransitionUpdateForPropertyHandle
// (core/animation/css/css_animations.cc:2806-2954 @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5): a transition starts when the
// before-change and after-change computed values differ for a property named
// by transition-property, the combined duration is nonzero, and the value
// pair interpolates (standard behavior — discrete pairs do not transition
// without `transition-behavior: allow-discrete`, which louis14 does not yet
// model); the running effect samples its timing function at the current
// time. The timing inputs come from the AFTER-change style, like Blink's
// `transition_data` read from the new style.
//
// Divergence note: Blink resolves a name in transition-property to physical
// longhands (shorthandForProperty + CSSProperty::ToPhysical in
// CalculateTransitionUpdateForStandardProperty). louis14 matches the name
// against the computed Properties map directly, so only PHYSICAL longhand
// names (and `all`) take effect — shorthand and logical (`margin-inline-*`)
// names are not expanded/mapped. The engine calls this pass after
// ResolveLogicalPropertiesInTree, so both style maps are physical-keyed and
// `all` covers logical declarations via their resolved physical keys.
// Sufficient for the WPT css-pseudo transition tests.

// resolvedTransition is one entry of the after-change style's transition
// list: a property name (longhand or "all") plus its timing.
type resolvedTransition struct {
	property string
	timing   Timing
}

// resolveTransitions reads the transition-* longhands (falling back to the
// raw `transition` shorthand, which the cascade stores unexpanded) and
// builds the per-property transition list. List-valued longhands repeat to
// the length of transition-property, mirroring Blink CSSTimingData::
// GetRepeated (css_animations.cc:2860-2862 @ 906a32d8).
func resolveTransitions(style *Style) []resolvedTransition {
	props, durations, timingFns, delays := transitionLonghands(style)
	if len(props) == 0 {
		return nil
	}
	repeated := func(list []string, i int, def string) string {
		if len(list) == 0 {
			return def
		}
		return strings.TrimSpace(list[i%len(list)])
	}
	var out []resolvedTransition
	for i, p := range props {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == "none" {
			continue
		}
		duration := parseTimeSeconds(repeated(durations, i, "0s"), 0)
		delay := parseTimeSeconds(repeated(delays, i, "0s"), 0)
		if duration <= 0 && delay <= 0 {
			continue
		}
		out = append(out, resolvedTransition{
			property: p,
			timing: Timing{
				StartDelay:        delay,
				IterationDuration: duration,
				IterationCount:    1,
				TimingFunction:    parseTimingFunction(repeated(timingFns, i, "ease")),
			},
		})
	}
	return out
}

// transitionLonghands returns the four transition-* longhand lists. When the
// style carries only the unexpanded `transition` shorthand, it is parsed here
// with the CSS Transitions §2.5 <single-transition> grammar (`[ none |
// <single-transition-property> ] || <time> || <easing-function> || <time>` —
// first <time> is the duration, second the delay), the same token
// classification expandAnimationShorthand uses for the animation shorthand.
func transitionLonghands(style *Style) (props, durations, timingFns, delays []string) {
	if v, ok := style.Get("transition-property"); ok {
		props = splitTopLevelCommas(v)
		durations = splitTopLevelCommas(getOr(style, "transition-duration", "0s"))
		timingFns = splitTopLevelCommas(getOr(style, "transition-timing-function", "ease"))
		delays = splitTopLevelCommas(getOr(style, "transition-delay", "0s"))
		return props, durations, timingFns, delays
	}
	shorthand, ok := style.Get("transition")
	if !ok || strings.TrimSpace(shorthand) == "" {
		return nil, nil, nil, nil
	}
	for _, item := range splitTopLevelCommas(shorthand) {
		prop := "all" // <single-transition-property> defaults to all
		duration := "0s"
		timingFn := "ease"
		delay := "0s"
		sawDuration := false
		sawTiming := false
		for _, tok := range splitTopLevelFields(strings.TrimSpace(item)) {
			switch {
			case isTimeToken(tok):
				if !sawDuration {
					duration = tok
					sawDuration = true
				} else {
					delay = tok
				}
			case isAnimationTimingFunctionToken(tok):
				if !sawTiming {
					timingFn = tok
					sawTiming = true
				}
			default:
				prop = tok
			}
		}
		props = append(props, prop)
		durations = append(durations, duration)
		timingFns = append(timingFns, timingFn)
		delays = append(delays, delay)
	}
	return props, durations, timingFns, delays
}

// transitionValuesInterpolable reports whether the from/to pair interpolates
// for prop — the standard-behavior gate: a discrete pair does not start a
// transition (Blink CanCalculateTransitionUpdateForProperty +
// InterpolationTypesMap; css_animations.cc:2523 @ 906a32d8). The branches
// mirror interpolateValue's non-discrete paths.
func transitionValuesInterpolable(prop, from, to string, base *Style) bool {
	switch prop {
	case "filter", "backdrop-filter":
		_, ok := interpolateFilterValue(from, to, 0.5)
		return ok
	case "transform":
		_, ok := interpolateTransformList(from, to, 0.5)
		return ok
	}
	if isColorProperty(prop) {
		_, ok1 := ParseColor(from)
		_, ok2 := ParseColor(to)
		if ok1 && ok2 {
			return true
		}
	}
	if _, ok := interpolateLengthPair(from, to, 0.5, base); ok {
		return true
	}
	if _, ok := interpolateBareNumber(from, to, 0.5); ok {
		return true
	}
	return false
}

// ApplyTransitionsFirstFrame overrides the after-change computed styles with
// each started transition's value at local time 0 (see the file doc comment
// for the semantics and Blink citations). root is the DOM (sub)tree whose
// styles were just re-cascaded; styles is the after-change map, prev the
// before-change map from the previous layout pass. Nodes without a
// before-change entry (created by script) start no transitions.
//
// When a transitioned property is inheritable, the override is propagated to
// descendants that inherited it (no own cascaded declaration and the same
// after-change value) — the post-cascade analog of the inheritance that
// would have carried the transitioned value down had it been present during
// the cascade, stopping at any descendant that declares the property itself.
func ApplyTransitionsFirstFrame(root *html.Node, styles, prev map[*html.Node]*Style) {
	if root == nil || len(prev) == 0 {
		return
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		newStyle := styles[n]
		oldStyle := prev[n]
		if newStyle != nil && oldStyle != nil {
			applyNodeTransitions(n, newStyle, oldStyle, styles)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// applyNodeTransitions starts and samples the transitions for one element.
func applyNodeTransitions(n *html.Node, newStyle, oldStyle *Style, styles map[*html.Node]*Style) {
	transitions := resolveTransitions(newStyle)
	if len(transitions) == 0 {
		return
	}
	for _, tr := range transitions {
		if tr.property == "all" {
			// Diff the union of both sides' longhands (Blink
			// CalculateTransitionUpdateForAll / PropertiesForTransitionAll,
			// css_animations.cc:2894 @ 906a32d8). Custom properties are
			// skipped — louis14 does not model registered custom-property
			// interpolation.
			seen := map[string]bool{}
			for prop := range newStyle.Properties {
				seen[prop] = true
			}
			for prop := range oldStyle.Properties {
				seen[prop] = true
			}
			for prop := range seen {
				if strings.HasPrefix(prop, "--") || strings.HasPrefix(prop, "transition") {
					continue
				}
				transitionProperty(n, prop, tr.timing, newStyle, oldStyle, styles)
			}
			continue
		}
		transitionProperty(n, tr.property, tr.timing, newStyle, oldStyle, styles)
	}
}

// transitionProperty samples one property's transition at local time 0 and
// applies the override (plus inherited propagation) when it changes the
// after-change value.
func transitionProperty(n *html.Node, prop string, timing Timing, newStyle, oldStyle *Style, styles map[*html.Node]*Style) {
	oldVal := strings.TrimSpace(oldStyle.Properties[prop])
	newVal := strings.TrimSpace(newStyle.Properties[prop])
	if oldVal == "" || newVal == "" || oldVal == newVal {
		return
	}
	if !transitionValuesInterpolable(prop, oldVal, newVal, newStyle) {
		return
	}
	var val string
	if rt := timing.Resolve(0); rt.InEffect {
		val = interpolateValue(prop, oldVal, newVal, rt.Progress, newStyle)
	} else {
		// Delay (before) phase: CSS Transitions §3 — the element renders
		// the before-change style until the delay elapses.
		val = oldVal
	}
	if val == newVal {
		return
	}
	newStyle.Properties[prop] = val
	if inheritableProperties[prop] {
		for _, c := range n.Children {
			propagateInheritedTransitionValue(c, prop, newVal, val, styles)
		}
	}
}

// propagateInheritedTransitionValue rewrites an inheritable property's
// inherited after-change value with the transitioned value on descendants
// that (a) have no own cascaded declaration for it (Style.CascadedProps) and
// (b) still carry the inherited after-change value. Declaring descendants
// terminate the walk for their subtree, exactly like real inheritance.
func propagateInheritedTransitionValue(n *html.Node, prop, newVal, val string, styles map[*html.Node]*Style) {
	if n == nil {
		return
	}
	if s := styles[n]; s != nil {
		// nil CascadedProps means the declaration record was not captured
		// (style built outside ComputeStyle, e.g. the synthetic root style)
		// — treated here as "declares nothing", so it neither keeps the
		// after-change value nor truncates the walk below it.
		if s.CascadedProps != nil && s.CascadedProps[prop] {
			return
		}
		if strings.TrimSpace(s.Properties[prop]) != newVal {
			return
		}
		s.Properties[prop] = val
	}
	for _, c := range n.Children {
		propagateInheritedTransitionValue(c, prop, newVal, val, styles)
	}
}
