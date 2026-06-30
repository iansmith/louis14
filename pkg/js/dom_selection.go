package js

import (
	"unicode/utf16"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// dom_selection.go implements the minimal DOM Selection/Range surface
// (document.createRange, window.getSelection, Range#selectNodeContents,
// Range#setStart/setEnd, Selection#addRange/removeAllRanges,
// document.activeElement) needed to drive ::selection highlight painting
// (LOU-344). No single Blink analog — Range/Selection are auto-generated
// from .idl in Blink; this file mirrors louis14's OWN existing goja-closure
// registration pattern (see dom_classlist.go, dom_mutation.go) rather than
// any one Blink source file. The underlying state model (Range as a pair of
// boundary points) mirrors Blink's core/dom/range.h; the persisted
// current-selection mirrors FrameSelection
// (third_party/blink/renderer/core/editing/frame_selection.h @ blob
// 5011f9bab79cae12d0d7757103eac204e4aaf1b2), simplified to a single Range
// (see html.Document.Selection's doc comment in pkg/html/dom.go).

// rangeAccessor implements goja.DynamicObject for a DOM Range. It wraps a
// *html.Range directly so Selection#addRange can read back the same
// instance script code mutated via setStart/setEnd/selectNodeContents.
type rangeAccessor struct {
	ctx *domContext
	r   *html.Range
}

// newRangeProxy creates a Range JS proxy and registers it in
// ctx.ranges so addRange can later resolve the proxy back to its
// *html.Range. Shared by document.createRange and the `new Range()`
// constructor.
func newRangeProxy(ctx *domContext) *goja.Object {
	r := &html.Range{}
	proxy := ctx.vm.NewDynamicObject(&rangeAccessor{ctx: ctx, r: r})
	ctx.ranges = append(ctx.ranges, rangeEntry{proxy: proxy, r: r})
	return proxy
}

// resolveRange looks up the *html.Range backing a Range JS proxy.
func (ctx *domContext) resolveRange(val goja.Value) *html.Range {
	if val == nil || goja.IsNull(val) || goja.IsUndefined(val) {
		return nil
	}
	for _, entry := range ctx.ranges {
		if entry.proxy.SameAs(val) {
			return entry.r
		}
	}
	return nil
}

func (ra *rangeAccessor) Get(key string) goja.Value {
	vm := ra.ctx.vm
	switch key {
	case "startContainer":
		if ra.r.StartContainer == nil {
			return goja.Null()
		}
		return ra.ctx.elementProxy(ra.r.StartContainer)
	case "startOffset":
		return vm.ToValue(ra.r.StartOffset)
	case "endContainer":
		if ra.r.EndContainer == nil {
			return goja.Null()
		}
		return ra.ctx.elementProxy(ra.r.EndContainer)
	case "endOffset":
		return vm.ToValue(ra.r.EndOffset)
	case "collapsed":
		return vm.ToValue(ra.r.StartContainer == ra.r.EndContainer && ra.r.StartOffset == ra.r.EndOffset)

	case "selectNodeContents":
		// Range#selectNodeContents(node): set both boundary points to span
		// all of node's children. Mirrors Blink's
		// Range::selectNodeContents (core/dom/range.cc): start = (node, 0),
		// end = (node, node.childNodes.length).
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("Failed to execute 'selectNodeContents' on 'Range': 1 argument required"))
			}
			node := ra.ctx.unwrapNode(call.Arguments[0])
			if node == nil {
				panic(vm.NewTypeError("Failed to execute 'selectNodeContents' on 'Range': parameter 1 is not a Node"))
			}
			ra.r.StartContainer = node
			ra.r.StartOffset = 0
			ra.r.EndContainer = node
			if node.Type == html.TextNode {
				// A text/CDATA node has no children — DOM spec offsets into
				// it are UTF-16 code-unit length, not child count (Range#
				// selectNodeContents step 4: "If refNode is a CharacterData
				// node, set end to the length of refNode's data").
				endOffset := 0
				for _, r := range node.Text {
					endOffset += utf16.RuneLen(r)
				}
				ra.r.EndOffset = endOffset
			} else {
				ra.r.EndOffset = len(node.Children)
			}
			return goja.Undefined()
		})
	case "setStart":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				panic(vm.NewTypeError("Failed to execute 'setStart' on 'Range': 2 arguments required"))
			}
			node := ra.ctx.unwrapNode(call.Arguments[0])
			if node == nil {
				panic(vm.NewTypeError("Failed to execute 'setStart' on 'Range': parameter 1 is not a Node"))
			}
			ra.r.StartContainer = node
			ra.r.StartOffset = int(call.Arguments[1].ToInteger())
			return goja.Undefined()
		})
	case "setEnd":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				panic(vm.NewTypeError("Failed to execute 'setEnd' on 'Range': 2 arguments required"))
			}
			node := ra.ctx.unwrapNode(call.Arguments[0])
			if node == nil {
				panic(vm.NewTypeError("Failed to execute 'setEnd' on 'Range': parameter 1 is not a Node"))
			}
			ra.r.EndContainer = node
			ra.r.EndOffset = int(call.Arguments[1].ToInteger())
			return goja.Undefined()
		})
	}
	return goja.Undefined()
}

func (ra *rangeAccessor) Set(key string, val goja.Value) bool { return false }

func (ra *rangeAccessor) Has(key string) bool {
	switch key {
	case "startContainer", "startOffset", "endContainer", "endOffset",
		"collapsed", "selectNodeContents", "setStart", "setEnd":
		return true
	}
	return false
}

func (ra *rangeAccessor) Delete(key string) bool { return false }

func (ra *rangeAccessor) Keys() []string {
	return []string{"startContainer", "startOffset", "endContainer", "endOffset",
		"collapsed", "selectNodeContents", "setStart", "setEnd"}
}

// selectionAccessor implements goja.DynamicObject for window.getSelection().
// One conceptual Selection per document; addRange/removeAllRanges mutate
// ctx.doc.Selection directly (see html.Document.Selection's doc comment for
// why the state lives on *html.Document rather than here).
type selectionAccessor struct {
	ctx *domContext
}

func (sa *selectionAccessor) Get(key string) goja.Value {
	vm := sa.ctx.vm
	switch key {
	case "rangeCount":
		if sa.ctx.doc.Selection == nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(1)
	case "addRange":
		// Selection#addRange(range): louis14 supports a single active Range
		// (see html.Document.Selection's doc comment), so addRange replaces
		// rather than appends — sufficient for every LOU-344 target test,
		// none of which add more than one range.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("Failed to execute 'addRange' on 'Selection': 1 argument required"))
			}
			if r := sa.ctx.resolveRange(call.Arguments[0]); r != nil {
				sa.ctx.doc.Selection = r
			}
			return goja.Undefined()
		})
	case "removeAllRanges":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			sa.ctx.doc.Selection = nil
			return goja.Undefined()
		})
	case "getRangeAt":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			// rangeCount is always 0 or 1 (single-Range model, see this
			// type's doc comment), so only index 0 is ever valid.
			if len(call.Arguments) == 0 ||
				int(call.Arguments[0].ToInteger()) != 0 ||
				sa.ctx.doc.Selection == nil {
				panic(vm.NewTypeError("Failed to execute 'getRangeAt' on 'Selection': index is not in range"))
			}
			return vm.NewDynamicObject(&rangeAccessor{ctx: sa.ctx, r: sa.ctx.doc.Selection})
		})
	}
	return goja.Undefined()
}

func (sa *selectionAccessor) Set(key string, val goja.Value) bool { return false }

func (sa *selectionAccessor) Has(key string) bool {
	switch key {
	case "rangeCount", "addRange", "removeAllRanges", "getRangeAt":
		return true
	}
	return false
}

func (sa *selectionAccessor) Delete(key string) bool { return false }

func (sa *selectionAccessor) Keys() []string {
	return []string{"rangeCount", "addRange", "removeAllRanges", "getRangeAt"}
}

// registerSelectionAPI wires document.createRange, the Range constructor
// (`new Range()`, used by selection-text-decoration-currentcolor.html),
// window.getSelection/getSelection, and the document.activeElement getter
// onto the runtime. Called from registerDocument alongside the other
// document.* bindings. bodyNode is the document's <body> element (or nil),
// reusing findBodyNode's lookup (engine.go) rather than re-walking the tree
// a second time the way registerDocumentProperties does internally.
func registerSelectionAPI(ctx *domContext, docObj *goja.Object, bodyNode *html.Node) {
	vm := ctx.vm

	docObj.Set("createRange", func(call goja.FunctionCall) goja.Value {
		return newRangeProxy(ctx)
	})

	// `new Range()` — Range is a constructible IDL interface
	// (https://dom.spec.whatwg.org/#dom-range-range). goja invokes a plain
	// Go func via `new` as a ConstructorCall when set as a constructor; the
	// new object is the same proxy createRange builds, so reuse newRangeProxy.
	vm.Set("Range", func(call goja.ConstructorCall) *goja.Object {
		return newRangeProxy(ctx)
	})

	getSelectionFn := func(call goja.FunctionCall) goja.Value {
		return vm.NewDynamicObject(&selectionAccessor{ctx: ctx})
	}
	vm.Set("getSelection", getSelectionFn)
	docObj.Set("getSelection", getSelectionFn)

	// document.activeElement (HTML5 §6.5.1): defaults to <body> when no
	// element is focused, otherwise the most recently .focus()ed element.
	// Implemented as a live getter (DefineAccessorProperty, the same
	// mechanism document.dir uses in dom_traversal.go) rather than a
	// snapshot value, since scripts focus/blur elements after document.*
	// properties are first set up.
	activeElementGetter := vm.ToValue(func(call goja.FunctionCall) goja.Value {
		if ctx.doc.FocusedElement != nil {
			return ctx.elementProxy(ctx.doc.FocusedElement)
		}
		if bodyNode != nil {
			return ctx.elementProxy(bodyNode)
		}
		return goja.Null()
	})
	_ = docObj.DefineAccessorProperty("activeElement", activeElementGetter, nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
}
