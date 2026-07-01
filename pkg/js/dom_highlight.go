package js

import (
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// dom_highlight.go implements the minimal CSS Custom Highlight API surface
// (`CSS.highlights`, `new Highlight(range, ...)`) needed to drive
// ::highlight(name) painting (LOU-354). No single Blink analog for the
// JS-binding shape — like Range/Selection (see dom_selection.go's file doc
// comment), Highlight/HighlightRegistry are auto-generated IDL interfaces
// (third_party/blink/renderer/core/highlight/highlight.idl,
// highlight_registry.idl) with no one .cc file to mirror the goja-closure
// registration itself; this follows louis14's OWN existing pattern instead.
// The underlying state model mirrors:
//   - Highlight — third_party/blink/renderer/core/highlight/highlight.h @
//     blob f8a065b0a122c72251cb3a9c056b024583941369: a set-like wrapper
//     around a range collection with a `priority` property (default 0).
//     Simplified to a plain []*html.Range (no HeapLinkedHashSet dedup
//     machinery — no LOU-354 target test adds a duplicate Range).
//   - HighlightRegistry — third_party/blink/renderer/core/highlight/
//     highlight_registry.h @ blob a859ec40274d3281ad59e450a2fcb776d0b54e3e:
//     an ordered name -> Highlight map. Simplified to
//     html.Document.CustomHighlights/CustomHighlightOrder (see that field's
//     doc comment) — only `setForBinding` is implemented (the sole method
//     every LOU-354 target test calls; no `.delete()`/`.clear()`/`.has()`/
//     iteration, confirmed via grep).
//
// Scope cut: every LOU-354 target test calls `CSS.highlights.set(name, new
// Highlight(range))` with exactly ONE range argument and never reads/writes
// `.priority` or calls any other HighlightRegistry/Highlight method
// (confirmed via grep) — so priority is stored but never consulted by the
// paint side, and the registry exposes only `.set`.
//
// document.execCommand("selectAll") is also a LOU-354 target test
// dependency (selection-over-highlight-001.html) but lives in
// dom_selection.go instead of here — it mutates ctx.doc.Selection, the
// field that file already owns, not any highlight-registry state.

// registerHighlightAPI wires window.CSS (with its .highlights registry)
// and the Highlight constructor (`new Highlight(range, ...)`) onto the
// runtime. Called from registerDocument alongside the other document.*
// bindings.
func registerHighlightAPI(ctx *domContext) {
	vm := ctx.vm

	// `new Highlight(range, ...)` — Highlight is a constructible IDL
	// interface (https://drafts.csswg.org/css-highlight-api-1/#dom-highlight-highlight)
	// accepting a variadic list of AbstractRange arguments. `.priority` is
	// a plain data property (goja's default get/set) rather than an
	// accessor-property pair — nothing needs a live getter here, unlike
	// document.activeElement (dom_selection.go), which must reflect
	// FocusedElement changes made after construction.
	vm.Set("Highlight", func(call goja.ConstructorCall) *goja.Object {
		var ranges []*html.Range
		for _, arg := range call.Arguments {
			if r := ctx.resolveRange(arg); r != nil {
				ranges = append(ranges, r)
			}
		}
		obj := call.This
		obj.Set("priority", 0)
		ctx.highlights = append(ctx.highlights, highlightEntry{obj: obj, ranges: ranges})
		return obj
	})

	// CSS.highlights — a Map-like object exposing only `.set(name,
	// highlight)`, the sole HighlightRegistry method any LOU-354 target
	// test calls (see file doc comment).
	highlightsObj := vm.NewObject()
	highlightsObj.Set("set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewTypeError("Failed to execute 'set' on 'HighlightRegistry': 2 arguments required"))
		}
		name := call.Arguments[0].String()
		ranges := ctx.resolveHighlight(call.Arguments[1])

		if ctx.doc.CustomHighlights == nil {
			ctx.doc.CustomHighlights = make(map[string][]*html.Range)
		}
		if _, exists := ctx.doc.CustomHighlights[name]; !exists {
			ctx.doc.CustomHighlightOrder = append(ctx.doc.CustomHighlightOrder, name)
		}
		ctx.doc.CustomHighlights[name] = ranges
		return highlightsObj
	})

	cssObj := vm.NewObject()
	cssObj.Set("highlights", highlightsObj)
	vm.Set("CSS", cssObj)
}

// resolveHighlight looks up the []*html.Range backing a Highlight JS object,
// the same SameAs-identity lookup resolveRange (dom_selection.go) uses for
// Range proxies.
func (ctx *domContext) resolveHighlight(val goja.Value) []*html.Range {
	if val == nil || goja.IsNull(val) || goja.IsUndefined(val) {
		return nil
	}
	for _, entry := range ctx.highlights {
		if entry.obj.SameAs(val) {
			return entry.ranges
		}
	}
	return nil
}
