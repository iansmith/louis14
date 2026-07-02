package js

import (
	"strings"
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
	// Register for live-range boundary adjustment on node removal
	// (Document.LiveRanges — the Blink Document::AttachRange analog).
	ctx.doc.LiveRanges = append(ctx.doc.LiveRanges, r)
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
	case "surroundContents":
		// Range#surroundContents(newParent) — WHATWG DOM §4.10; mirrors
		// Blink Range::surroundContents (core/dom/range.cc @ Chromium
		// 906a32d8634edf17db094507908f439bd9b52de5): extract the range's
		// contents, empty newParent, insert newParent at the range start,
		// re-append the extracted contents into it, then select newParent.
		//
		// louis14 supports the whole-child-containment shape (both boundary
		// points in the SAME element container) — the form the DOM spec's
		// step-1 partial-containment check leaves for element content and
		// the one first-letter-of-html-root-refcrash.html exercises.
		// Partial text-node boundaries are not modeled (no extractContents
		// text splitting yet); those return undefined un-mutated.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("Failed to execute 'surroundContents' on 'Range': 1 argument required"))
			}
			newParent := ra.ctx.unwrapNode(call.Arguments[0])
			if newParent == nil {
				panic(vm.NewTypeError("Failed to execute 'surroundContents' on 'Range': parameter 1 is not a Node"))
			}
			r := ra.r
			container := r.StartContainer
			if container == nil || container != r.EndContainer || container.Type == html.TextNode {
				return goja.Undefined() // unsupported boundary shape
			}
			// Out-of-range boundary offsets are rejected un-mutated (the
			// DOM spec throws IndexSizeError; this accessor's convention
			// for unsupported/invalid input is a no-op) — clamping would
			// silently operate on a DIFFERENT range and mutate the tree.
			s, e := r.StartOffset, r.EndOffset
			if s < 0 || e < 0 || s > len(container.Children) ||
				e > len(container.Children) || s > e {
				return goja.Undefined()
			}

			// Step "extract": detach the contained children (newParent may
			// itself live inside this subtree — the refcrash flow wraps the
			// head into a style element the head contains).
			extracted := append([]*html.Node(nil), container.Children[s:e]...)
			container.Children = append(container.Children[:s:s], container.Children[e:]...)
			for _, c := range extracted {
				c.Parent = nil
			}

			// Step "empty newParent" (DOM "replace all": removed children
			// become parentless), then detach it from wherever it lives.
			for _, c := range newParent.Children {
				c.Parent = nil
			}
			newParent.Children = nil
			if newParent.Parent != nil {
				newParent.Parent.RemoveChild(newParent)
			}

			// Step "insert newParent" at the (collapsed) range start.
			if s > len(container.Children) {
				s = len(container.Children)
			}
			rest := append([]*html.Node{newParent}, container.Children[s:]...)
			container.Children = append(container.Children[:s:s], rest...)
			newParent.Parent = container

			// Step "append the fragment into newParent".
			for _, c := range extracted {
				if c == newParent {
					continue
				}
				c.Parent = newParent
				newParent.Children = append(newParent.Children, c)
			}

			// Step "select newParent".
			r.StartContainer = container
			r.StartOffset = s
			r.EndContainer = container
			r.EndOffset = s + 1
			return goja.Undefined()
		})
	}
	return goja.Undefined()
}

func (ra *rangeAccessor) Set(key string, val goja.Value) bool { return false }

func (ra *rangeAccessor) Has(key string) bool {
	switch key {
	case "startContainer", "startOffset", "endContainer", "endOffset",
		"collapsed", "selectNodeContents", "setStart", "setEnd",
		"surroundContents":
		return true
	}
	return false
}

func (ra *rangeAccessor) Delete(key string) bool { return false }

func (ra *rangeAccessor) Keys() []string {
	return []string{"startContainer", "startOffset", "endContainer", "endOffset",
		"collapsed", "selectNodeContents", "setStart", "setEnd", "surroundContents"}
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
	case "selectAllChildren":
		// Selection#selectAllChildren(node) — Selection API §5.5; mirrors
		// Blink DOMSelection::selectAllChildren
		// (core/editing/dom_selection.cc @ Chromium
		// 906a32d8634edf17db094507908f439bd9b52de5): the selection becomes
		// a range from (node, 0) to (node, number of children). Replaces
		// the single active Range like addRange does.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("Failed to execute 'selectAllChildren' on 'Selection': 1 argument required"))
			}
			node := sa.ctx.unwrapNode(call.Arguments[0])
			if node == nil {
				panic(vm.NewTypeError("Failed to execute 'selectAllChildren' on 'Selection': parameter 1 is not a Node"))
			}
			sa.ctx.doc.Selection = &html.Range{
				StartContainer: node,
				StartOffset:    0,
				EndContainer:   node,
				EndOffset:      len(node.Children),
			}
			return goja.Undefined()
		})
	}
	return goja.Undefined()
}

func (sa *selectionAccessor) Set(key string, val goja.Value) bool { return false }

func (sa *selectionAccessor) Has(key string) bool {
	switch key {
	case "rangeCount", "addRange", "removeAllRanges", "getRangeAt",
		"selectAllChildren":
		return true
	}
	return false
}

func (sa *selectionAccessor) Delete(key string) bool { return false }

func (sa *selectionAccessor) Keys() []string {
	return []string{"rangeCount", "addRange", "removeAllRanges", "getRangeAt", "selectAllChildren"}
}

// registerSelectionAPI wires document.createRange, the Range constructor
// (`new Range()`, used by selection-text-decoration-currentcolor.html),
// window.getSelection/getSelection, the document.activeElement getter, and
// document.execCommand("selectAll") (LOU-354) onto the runtime. Called from
// registerDocument alongside the other document.* bindings. bodyNode is the
// document's <body> element (or nil), reusing findBodyNode's lookup
// (engine.go) rather than re-walking the tree a second time the way
// registerDocumentProperties does internally.
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

	// document.execCommand("selectAll") (selection-over-highlight-001.html,
	// highlight-painting-005-crash.html — LOU-354): mirrors Blink's
	// SelectAll editing command (core/editing/commands/
	// select_all_command.cc), simplified to louis14's single-Range
	// Selection model (see html.Document.Selection's doc comment) — selects
	// the whole <body>'s contents, equivalent to
	// selectNodeContents(document.body). Other command names are silently
	// no-ops (no LOU-354 target test uses any other execCommand). Lives
	// here rather than in dom_highlight.go despite being a LOU-354 target
	// test dependency: it mutates ctx.doc.Selection, the same field every
	// other binding in this file owns, not any highlight-registry state.
	docObj.Set("execCommand", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 || !strings.EqualFold(call.Arguments[0].String(), "selectall") {
			return vm.ToValue(false)
		}
		if bodyNode == nil {
			return vm.ToValue(false)
		}
		ctx.doc.Selection = &html.Range{
			StartContainer: bodyNode,
			StartOffset:    0,
			EndContainer:   bodyNode,
			EndOffset:      len(bodyNode.Children),
		}
		return vm.ToValue(true)
	})
}
