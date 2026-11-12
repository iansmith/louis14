package js

import (
	"fmt"
	"time"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"

	"github.com/dop251/goja"
)

// Engine executes JavaScript against an HTML document's DOM.
type Engine struct {
	vm *goja.Runtime
	// imageFetcher loads the images script asks for (img.src); nil serves
	// only data: URLs. See SetImageFetcher.
	imageFetcher images.ImageFetcher
	// imageLoaders holds each <img>'s ImageLoader state
	// (html_image_element.go).
	imageLoaders map[*html.Node]*imageLoader
	// tasks queues posted tasks (postTask), run after the current one.
	tasks []*postedTask

	// Layout snapshot from the most recent layout pass, used to back
	// getComputedStyle. Set via SetLayoutSnapshot before Execute.
	layoutStyles map[*html.Node]*css.Style
	layoutBoxes  map[*html.Node]*layout.Box

	// updateLayout lays the document out again and returns the node→box
	// index, for the bindings that read or write layout state mid-script
	// (Element.scrollLeft/scrollTop). Mirrors Document::
	// UpdateStyleAndLayoutForNode, which Element::scrollLeft/setScrollLeft
	// call first (dom/element.cc:2724-2760, 2811-2866 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Set via SetLayoutUpdater
	// before Execute; nil leaves those bindings without layout (the
	// requested offset is stored and read back unclamped).
	updateLayout func() map[*html.Node]*layout.Box

	// doc is the document most recently passed to Execute, retained so
	// CurrentSelection() can read doc.Selection after scripts finish
	// running (e.g. from a test, without the caller needing to thread the
	// *html.Document back through separately).
	doc *html.Document

	// OnFragmentDirective is invoked synchronously by window.location's
	// hash/href setters (pkg/js/dom_location.go, LOU-349) whenever the
	// newly-assigned fragment contains a `:~:` Text Fragment directive
	// marker. The callback receives everything after the marker (e.g.
	// "text=match%20me" for "#:~:text=match%20me"). nil (the zero value) is
	// a valid "no listener configured" state — callers that don't need
	// ::target-text matching (e.g. existing ::selection-only tests) simply
	// never set this and the hash/href setters become plain field writes.
	// Set directly by the caller before Execute, mirroring
	// SetLayoutSnapshot's "configure before Execute" shape.
	OnFragmentDirective func(directive string)
}

// New creates a new JS engine with a fresh goja runtime.
func New() *Engine {
	vm := goja.New()
	e := &Engine{
		vm:            vm,
		imageLoaders:  make(map[*html.Node]*imageLoader),
	}

	// Register console API
	c := &consoleAPI{}
	c.register(vm)

	// In browsers, `window` is the global object. Set it so scripts using
	// `window.onload`, `window.document`, etc. work as expected.
	vm.Set("window", vm.GlobalObject())

	// requestAnimationFrame: louis14 has no frame loop, so each call runs
	// its callback immediately as its own animation frame. Tests use nested
	// rAF calls to wait for "next frame"; those run synchronously in the
	// same task. Each frame first advances the document's AnimationClock by
	// one html.ApproximateFrameTime (Blink kApproximateFrameTime,
	// core/animation/animation_clock.cc:40), then passes the frame time in
	// ms as the callback's DOMHighResTimeStamp, as
	// ScriptedAnimationController::ExecuteFrameCallbacks passes
	// current_frame_time_ms_ (core/dom/scripted_animation_controller.cc:
	// 176-181), both @ d4ecdfed88f962439247c2ad36b8fe47805b1520. Before
	// Execute has a document, the timestamp is 0 and no clock advances.
	var rafID int64
	vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
		rafID++
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			var frameTime float64
			if e.doc != nil {
				clock := &e.doc.AnimationClock
				clock.UpdateTime(clock.CurrentTime() + html.ApproximateFrameTime)
				frameTime = frameTimeMs(clock.CurrentTime())
			}
			fn(goja.Undefined(), vm.ToValue(frameTime)) //nolint:errcheck
		}
		return vm.ToValue(rafID)
	})
	// cancelAnimationFrame: no-op in test env (rAF has already fired synchronously).
	vm.Set("cancelAnimationFrame", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	// setTimeout(fn, ms, args…) and setInterval(fn, ms, args…): in a static
	// reftest engine we have no real event loop, so we execute the callback
	// synchronously on the same task. WPT reftests use setTimeout to defer
	// past the first restyle (the "wait one tick after onload" idiom in the
	// :placeholder-shown / :required / :read-write type-change tests).
	// Executing it inline produces the same final DOM state the post-tick
	// callback would have reached, and the visualtest harness captures the
	// final state regardless of when the script settles. Mirrors the same
	// synchronous-execution shape we use for requestAnimationFrame above.
	var timerID int64
	timerFn := func(call goja.FunctionCall) goja.Value {
		timerID++
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			args := make([]goja.Value, 0)
			// Skip arg 0 (the callback) and arg 1 (the delay); pass through
			// the rest verbatim per WHATWG HTML "setTimeout(handler, timeout,
			// ...arguments)".
			for i := 2; i < len(call.Arguments); i++ {
				args = append(args, call.Arguments[i])
			}
			fn(goja.Undefined(), args...) //nolint:errcheck
		}
		return vm.ToValue(timerID)
	}
	vm.Set("setTimeout", timerFn)
	vm.Set("setInterval", timerFn)
	// clearTimeout / clearInterval: no-op — by the time JS could cancel, the
	// timer has already fired synchronously.
	vm.Set("clearTimeout", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})
	vm.Set("clearInterval", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	// WPT reftest helpers. Tests load these via external
	// <script src="/common/rendering-utils.js">,
	// <script src="/common/reftest-wait.js"> and
	// <script src="/web-animations/testcommon.js">. A fetched upstream
	// script's declarations replace these; the prelude covers documents
	// parsed without a script fetcher and scripts that are not fetched.
	// The prelude is compiled once at init (goja *Program is cross-runtime
	// safe) and re-run into each fresh runtime — a sweep builds many Engines.
	if _, err := vm.RunProgram(wptReftestPrelude); err != nil {
		panic(fmt.Errorf("wpt reftest prelude: %w", err))
	}

	return e
}

// wptReftestPrelude is the compiled subset of WPT testharness globals that
// reftests use directly. Faithful to upstream WPT (see
// https://github.com/web-platform-tests/wpt/blob/master/common/rendering-utils.js
// and /common/reftest-wait.js):
//   - waitForAtLeastOneFrame returns a Promise that resolves after two
//     requestAnimationFrame ticks. Our rAF runs synchronously, so the
//     resolve fires in the same task and goja drains the .then job on
//     RunProgram return.
//   - takeScreenshot removes the root's reftest-wait class, as upstream
//     does; louis14 captures after JS finishes regardless.
//   - waitForCompositorReady (upstream in /web-animations/testcommon.js)
//     resolves once the compositor has committed a frame; louis14 has no
//     compositor, so an immediately-resolving promise is the faithful
//     equivalent — the caller's await continuation still runs before
//     Execute returns because goja drains the job queue.
var wptReftestPrelude = goja.MustCompile("wptReftestPrelude", `
function waitForAtLeastOneFrame() {
  return new Promise(function(resolve) {
    requestAnimationFrame(function() {
      requestAnimationFrame(resolve);
    });
  });
}
function takeScreenshot() {
  document.documentElement.classList.remove("reftest-wait");
}
function waitForCompositorReady() {
  return Promise.resolve();
}
`, false)

// frameTimeMs converts an AnimationClock time to the milliseconds that
// DOMHighResTimeStamp and CSSNumberish timeline times use.
func frameTimeMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// SetImageFetcher sets the fetcher the document's images load through when
// script sets an <img>'s src: the same document-relative fetcher layout and
// paint use (the Document's ResourceFetcher, in Blink).
func (e *Engine) SetImageFetcher(fetcher images.ImageFetcher) {
	e.imageFetcher = fetcher
}

// postedTask is a task posted to the engine's queue; a canceled one is
// skipped (Blink's TaskHandle from PostCancellableTask).
type postedTask struct {
	run      func()
	canceled bool
}

// postTask queues fn to run after the current task, as Blink posts to the
// document's task runner (Document::GetTaskRunner(TaskType::
// kDOMManipulation) + PostCancellableTask, core/loader/image_loader.cc:375,
// :859 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (e *Engine) postTask(fn func()) *postedTask {
	t := &postedTask{run: fn}
	e.tasks = append(e.tasks, t)
	return t
}

// runPostedTasks runs the queued tasks in order, including any they post.
func (e *Engine) runPostedTasks() {
	for len(e.tasks) > 0 {
		t := e.tasks[0]
		e.tasks = e.tasks[1:]
		if !t.canceled {
			t.run()
		}
	}
	e.tasks = nil
}

// CurrentSelection returns the document's current DOM Selection/Range
// (set via document.createRange()/selectNodeContents()/getSelection().
// addRange() during script execution), or nil if nothing is selected or
// Execute hasn't run yet. Thin accessor over html.Document.Selection — see
// that field's doc comment for why the state itself lives on *html.Document
// rather than on Engine (pkg/render needs to read it at paint time without
// importing pkg/js).
func (e *Engine) CurrentSelection() *html.Range {
	if e.doc == nil {
		return nil
	}
	return e.doc.Selection
}

// SetLayoutSnapshot stores the computed-style map and node→box index from the
// most recent layout pass so that getComputedStyle can return real values.
// Callers should invoke this after layout completes but before Execute.
func (e *Engine) SetLayoutSnapshot(styles map[*html.Node]*css.Style, boxes map[*html.Node]*layout.Box) {
	e.layoutStyles = styles
	e.layoutBoxes = boxes
}

// SetLayoutUpdater installs the function that re-runs layout on the
// current DOM and returns its node→box index (see Engine.updateLayout).
func (e *Engine) SetLayoutUpdater(update func() map[*html.Node]*layout.Box) {
	e.updateLayout = update
}

// HasScriptWork reports whether Execute has anything to run for doc: its
// scripts, or a <body onload="..."> handler, which fires on the window's
// load event even in a document with no <script> (HTML §8.1.8.2 body
// element event handlers).
func HasScriptWork(doc *html.Document) bool {
	return len(doc.Scripts) > 0 || bodyOnloadHandler(doc) != ""
}

// bodyOnloadHandler returns the source of the <body onload="..."> event
// handler content attribute, or "" when there is none.
func bodyOnloadHandler(doc *html.Document) string {
	if body := findBodyNode(doc.Root); body != nil {
		onload, _ := body.GetAttribute("onload")
		return onload
	}
	return ""
}

// Execute runs all scripts from the document against the DOM.
// Scripts are executed in order. Any JS errors are returned but
// callers may choose to log and continue rather than fail.
// After all scripts run, any onload handler registered on the global
// scope (window.onload = ... or just onload = ...) is called.
// Then <body onload="..."> attribute is fired, and finally any
// element-level onload callbacks (e.g. iframe.onload) are fired.
func (e *Engine) Execute(doc *html.Document) error {
	e.doc = doc
	// Register document global pointing at this document's DOM
	ctx := registerDocument(e.vm, doc)
	// Give the DOM context a reference to the engine so element proxies
	// can set event handlers and start image requests.
	ctx.engine = e
	ctx.layoutStyles = e.layoutStyles
	ctx.layoutBoxes = e.layoutBoxes
	// Script mutations of <style>/<link> elements (insert, remove, text
	// change) must reach the derived doc.Stylesheets before anything reads
	// the cascade: before each layout flush a script forces, and once more
	// when Execute returns (on every path, including a script error) for
	// the caller's post-script layout. Mirrors Document::
	// UpdateStyleAndLayoutTree → StyleEngine::UpdateActiveStyleSheets
	// (core/css/style_engine.cc:674 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	defer doc.UpdateActiveStyleSheets()
	if update := e.updateLayout; update != nil {
		ctx.updateLayout = func() map[*html.Node]*layout.Box {
			doc.UpdateActiveStyleSheets()
			return update()
		}
	}

	e.registerImageConstructor(ctx)
	registerWindowViewportSize(e.vm, ctx)

	// Each step below is a task; tasks it posts (image load/error events)
	// run after it.
	runTask := func(step func() error) error {
		err := step()
		e.runPostedTasks()
		return err
	}

	// Execute each script in document order.
	for i, script := range doc.Scripts {
		if err := runTask(func() error {
			_, err := e.vm.RunString(script)
			return err
		}); err != nil {
			return fmt.Errorf("script %d: %w", i, err)
		}
	}
	// The document's images finish loading before the window load event.
	_ = runTask(func() error {
		e.startParsedImageLoads(ctx)
		return nil
	})

	// The window load event: window.onload, then the <body onload="...">
	// attribute (per HTML, an event handler for the Window's load event;
	// many WPT tests use this form directly), then load listeners.
	if err := runTask(func() error {
		if onloadVal := e.vm.Get("onload"); onloadVal != nil &&
			!goja.IsUndefined(onloadVal) && !goja.IsNull(onloadVal) {
			if fn, ok := goja.AssertFunction(onloadVal); ok {
				if _, err := fn(goja.Undefined()); err != nil {
					return fmt.Errorf("onload: %w", err)
				}
			}
		}
		if onloadAttr := bodyOnloadHandler(doc); onloadAttr != "" {
			if _, err := e.vm.RunString(onloadAttr); err != nil {
				return fmt.Errorf("body onload: %w", err)
			}
		}
		ctx.fireDocumentLoadListeners()
		return nil
	}); err != nil {
		return err
	}

	// WPT reftest-wait protocol (/common/reftest-wait.js): after the page
	// has rendered, the harness dispatches "TestRendered" on the document;
	// tests mutate styles in the handler and then clear the reftest-wait
	// class. Louis14 renders once after Execute returns, so dispatching
	// synchronously here is the faithful equivalent — the mutations land
	// before the only screenshot. It follows the window load event and the
	// image events the scripts' loads posted (browser event order).
	_ = runTask(func() error {
		fireTestRendered(ctx)
		return nil
	})

	// Dispatch the other elements' load events (an iframe's: it has
	// already loaded, the first layout pass fetched the content) to their
	// onload handlers and load listeners, in the order the elements first
	// got one (HTMLFrameOwnerElement::DispatchLoad → DispatchScopedEvent,
	// core/html/html_frame_owner_element.cc:606-608 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An <img>'s load comes
	// from its ImageLoader instead.
	_ = runTask(func() error {
		for _, node := range append([]*html.Node(nil), ctx.listenerTargets...) {
			if node != doc.Root && node.TagName != "img" && ctx.hasEventListeners(node, "load") {
				ctx.dispatchEvent(node, "load", false)
			}
		}
		return nil
	})

	return nil
}

// fireTestRendered dispatches the reftest harness's "TestRendered" event:
// a bubbling event at the root element, so root-element listeners run and
// then the document's (wptrunner's reftest-wait support dispatches
// `new Event("TestRendered", {bubbles: true})` at
// document.documentElement). A failing listener does not stop the others.
func fireTestRendered(ctx *domContext) {
	target := documentElement(ctx.doc)
	if target == nil {
		target = ctx.doc.Root
	}
	ctx.dispatchEvent(target, "TestRendered", true)
}

// findBodyNode walks the document tree to find the <body> element.
func findBodyNode(root *html.Node) *html.Node {
	if root == nil {
		return nil
	}
	if root.Type == html.ElementNode && root.TagName == "body" {
		return root
	}
	for _, child := range root.Children {
		if found := findBodyNode(child); found != nil {
			return found
		}
	}
	return nil
}

// registerWindowViewportSize defines window.innerWidth and innerHeight: the
// viewport's size, scrollbars included, as integers, after bringing layout
// up to date (LocalDOMWindow::innerWidth / innerHeight → GetViewportSize →
// LocalFrameView::Size, core/frame/local_dom_window.cc:1723-1771 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14's LayoutView box,
// keyed by the Document node, is the viewport. Without a layout updater
// (no frame view) both are 0, as with no frame.
func registerWindowViewportSize(vm *goja.Runtime, ctx *domContext) {
	viewportSize := func() (int, int) {
		if ctx.updateLayout == nil {
			return 0, 0
		}
		view := ctx.updateLayout()[ctx.doc.Root]
		if view == nil {
			return 0, 0
		}
		return int(view.Width), int(view.Height)
	}
	window := vm.GlobalObject()
	_ = window.DefineAccessorProperty("innerWidth", vm.ToValue(func(goja.FunctionCall) goja.Value {
		w, _ := viewportSize()
		return vm.ToValue(w)
	}), nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
	_ = window.DefineAccessorProperty("innerHeight", vm.ToValue(func(goja.FunctionCall) goja.Value {
		_, h := viewportSize()
		return vm.ToValue(h)
	}), nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
}
