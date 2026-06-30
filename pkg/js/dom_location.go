package js

import (
	"strings"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// dom_location.go implements the minimal window.location surface (LOU-349)
// needed to drive ::target-text URL Text Fragment matching: a `.hash`
// getter/setter and a `.href` getter/setter that supports fragment-only
// assignment. No single Blink analog — like Range/Selection (see
// dom_selection.go's file doc comment), Location is an auto-generated IDL
// interface (third_party/blink/renderer/core/frame/location.idl) with no
// one .cc file to mirror; this follows louis14's OWN existing goja-closure
// registration pattern instead.
//
// Scope cut: only fragment-only assignment
// (`location.hash = "#:~:text=..."` / `location.href = "#:~:text=..."`)
// needs to work — confirmed via grep that none of the 14 LOU-349 target
// tests assign a full absolute URL. General URL parsing/resolution
// (origin, pathname, search, etc.) is therefore out of scope; href getter
// returns whatever was last set (defaulting to "" — about:blank-shaped
// documents have no real URL in this static-render engine), and href
// setter only special-cases a leading '#' (fragment-only navigation per
// WHATWG HTML's "navigate to a fragment" algorithm
// https://html.spec.whatwg.org/#scroll-to-the-fragment); any other value is
// stored verbatim into href without further parsing.
//
// Setting EITHER .hash or .href synchronously invokes
// Engine.OnFragmentDirective (when the new fragment contains a `:~:`
// marker) — louis14's one-shot rendering model already runs scripts,
// rAF callbacks, and the TestRendered dispatch synchronously (see
// engine.go's Execute), so a direct synchronous call is correct and
// sufficient; no navigation/event-loop machinery is built beyond that.

// locationAccessor implements goja.DynamicObject for window.location /
// document.location (the same object is exposed under both names, mirroring
// Blink's Document.location returning the owning Window's Location).
type locationAccessor struct {
	ctx *domContext
}

func (la *locationAccessor) Get(key string) goja.Value {
	vm := la.ctx.vm
	switch key {
	case "hash":
		return vm.ToValue(la.ctx.locationHash())
	case "href":
		return vm.ToValue(la.ctx.locationHref())
	}
	return goja.Undefined()
}

func (la *locationAccessor) Set(key string, val goja.Value) bool {
	switch key {
	case "hash":
		la.ctx.setLocationHash(val.String())
		return true
	case "href":
		la.ctx.setLocationHref(val.String())
		return true
	}
	return false
}

func (la *locationAccessor) Has(key string) bool {
	switch key {
	case "hash", "href":
		return true
	}
	return false
}

func (la *locationAccessor) Delete(key string) bool { return false }

func (la *locationAccessor) Keys() []string {
	return []string{"hash", "href"}
}

// locationHash returns the current fragment including its leading '#', or
// "" if no fragment has been set — mirrors WHATWG HTML's `hash` getter
// (https://html.spec.whatwg.org/#dom-location-hash): "" when the URL's
// fragment is null or empty, "#" + fragment otherwise.
func (ctx *domContext) locationHash() string {
	if ctx.locationFragment == "" {
		return ""
	}
	return "#" + ctx.locationFragment
}

// locationHref returns the last value assigned via .href, or a synthesized
// "#fragment" if only .hash was ever set — there is no real document URL to
// report in this static-render engine (see this file's doc comment).
func (ctx *domContext) locationHref() string {
	if ctx.locationHrefValue != "" {
		return ctx.locationHrefValue
	}
	return ctx.locationHash()
}

// setLocationHash implements the `hash` setter
// (https://html.spec.whatwg.org/#dom-location-hash): strips a leading '#'
// if present, stores the fragment, then triggers fragment-directive
// matching. URL-decoding is deliberately NOT done here — the 14 LOU-349
// target tests already pass a pre-encoded string (e.g. "%20" for a space);
// decoding happens in the directive parser (Checkpoint 2), not at
// assignment time, matching how a real URL's fragment is stored
// percent-encoded until something downstream interprets it.
func (ctx *domContext) setLocationHash(newHash string) {
	ctx.locationFragment = strings.TrimPrefix(newHash, "#")
	ctx.locationHrefValue = ctx.locationHash()
	ctx.notifyFragmentChanged()
}

// setLocationHref implements the `href` setter for the fragment-only case
// this engine supports (see file doc comment): a value starting with '#' is
// treated as fragment-only navigation, delegating to setLocationHash so
// both setters share one fragment-directive trigger path. Any other value
// is stored verbatim (no parsing) and does not by itself carry a `:~:`
// directive, so it does not trigger matching.
func (ctx *domContext) setLocationHref(newHref string) {
	if strings.HasPrefix(newHref, "#") {
		ctx.setLocationHash(newHref)
		return
	}
	ctx.locationHrefValue = newHref
	ctx.locationFragment = ""
}

// notifyFragmentChanged parses and resolves a `:~:` Text Fragment directive
// (LOU-349) out of the current fragment, when one is present, updating
// ctx.doc.TargetTextRanges with the resulting matches (see
// html.FindTextFragmentMatches). A fragment with no `:~:` marker (a plain
// element-id anchor, or no fragment at all) does not trigger matching —
// mirrors WHATWG's Text Fragments integration, which only invokes the
// fragment-directive processing steps when a directive delimiter is
// present
// (https://wicg.github.io/scroll-to-text-fragment/#parsing-the-fragment-directive).
// Also invokes ctx.engine.OnFragmentDirective (when set) so callers/tests
// can observe the raw directive string independent of the match resolution
// itself.
func (ctx *domContext) notifyFragmentChanged() {
	const marker = ":~:"
	idx := strings.Index(ctx.locationFragment, marker)
	if idx < 0 {
		return
	}
	directive := ctx.locationFragment[idx+len(marker):]
	if ctx.engine != nil && ctx.engine.OnFragmentDirective != nil {
		ctx.engine.OnFragmentDirective(directive)
	}
	if ctx.doc == nil || ctx.doc.Root == nil {
		return
	}
	selectors := html.ParseTextFragmentDirectives(directive)
	ctx.doc.TargetTextRanges = html.FindTextFragmentMatches(ctx.doc.Root, selectors)
}

// registerLocationAPI wires window.location and document.location (the
// same object, per Document.location's IDL — see this file's doc comment)
// onto the runtime. Called from registerDocument alongside the other
// document.* bindings.
func registerLocationAPI(ctx *domContext, docObj *goja.Object) {
	vm := ctx.vm
	locProxy := vm.NewDynamicObject(&locationAccessor{ctx: ctx})
	vm.Set("location", locProxy)
	docObj.Set("location", locProxy)
}
