package js

import (
	"math"
	"strconv"

	"louis14/pkg/html"
	"louis14/pkg/images"
	urlpkg "louis14/pkg/url"

	"github.com/dop251/goja"
)

// This file ports the script-visible slice of Blink's HTMLImageElement and
// ImageLoader (core/html/html_image_element.cc, core/loader/image_loader.cc
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the `Image` named
// constructor, and the load / error events an <img>'s image request
// dispatches.
//
// louis14 loads synchronously, so an image request completes when it is
// observed; what stays asynchronous, as in Blink, is the event:
// DispatchPendingLoadEvent / DispatchPendingErrorEvent are posted tasks
// (image_loader.cc:375-380, :859-864), so they run after the script that
// set src (Engine.postTask).

// imageLoader is an <img>'s ImageLoader state: whether its request has
// started, and the posted tasks that will dispatch its pending load and
// error events (Blink's pending_load_event_ / pending_error_event_).
type imageLoader struct {
	started           bool
	pendingLoadEvent  *postedTask
	pendingErrorEvent *postedTask
}

// registerImageConstructor defines the `Image` named constructor:
// new Image([width[, height]]) creates an <img> not created by the parser
// whose width / height content attributes are set from the arguments
// (HTMLImageElement::CreateForJSConstructor, html_image_element.cc:149-165).
func (e *Engine) registerImageConstructor(ctx *domContext) {
	e.vm.Set("Image", func(call goja.ConstructorCall) *goja.Object {
		node := newElementNode("img")
		// setWidth / setHeight take an IDL unsigned long.
		for i, attr := range []string{"width", "height"} {
			if i < len(call.Arguments) {
				node.Attributes[attr] = strconv.FormatUint(uint64(toUint32(call.Argument(i))), 10)
			}
		}
		return ctx.elementProxy(node).ToObject(e.vm)
	})
}

// updateImageFromElement starts an <img>'s image request: after its src
// content attribute changed, or for a parsed image before the window load
// event (startParsedImageLoads). Mirrors ImageLoader::UpdateFromElement →
// DoUpdateFromElement (image_loader.cc:427-645):
//   - no src attribute (a null source): no request and no error of its
//     own; the pending load event is canceled, an error event already
//     queued by the previous request still fires (:585-628);
//   - an empty src: QueuePendingErrorEvent (:586-590);
//   - otherwise a request: both pending events are canceled, and the
//     loaded image posts load, a failed one error (ImageNotifyFinished,
//     :844-864). With no fetcher only data: URLs load; any other source
//     stays loading, with no event yet.
//
// The src is completed against the document's base first
// (ImageLoader::ImageSourceToKURL, image_loader.cc:746 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (e *Engine) updateImageFromElement(ctx *domContext, node *html.Node) {
	loader := e.imageLoaders[node]
	if loader == nil {
		loader = &imageLoader{}
		e.imageLoaders[node] = loader
	}
	loader.started = true
	src, hasSrc := node.GetAttribute("src")
	if hasSrc && src == "" {
		e.queuePendingImageEvent(ctx, node, &loader.pendingErrorEvent, "error")
	}
	cancelPostedTask(&loader.pendingLoadEvent)
	if !hasSrc || src == "" {
		return
	}
	cancelPostedTask(&loader.pendingErrorEvent)
	src = urlpkg.CompleteURL(src, e.doc.BaseDir)
	switch {
	case !images.ImageLoadIsAttempted(src, e.imageFetcher):
	case images.ImageIsPotentiallyAvailable(src, e.imageFetcher):
		e.queuePendingImageEvent(ctx, node, &loader.pendingLoadEvent, "load")
	default:
		e.queuePendingImageEvent(ctx, node, &loader.pendingErrorEvent, "error")
	}
}

// queuePendingImageEvent posts the dispatch of eventType at node into slot,
// replacing (canceling) what the slot held, as assigning a TaskHandle does
// (QueuePendingErrorEvent, image_loader.cc:363-381).
func (e *Engine) queuePendingImageEvent(ctx *domContext, node *html.Node, slot **postedTask, eventType string) {
	cancelPostedTask(slot)
	*slot = e.postTask(func() {
		*slot = nil
		e.dispatchImageLoaderEvent(ctx, node, eventType)
	})
}

// cancelPostedTask cancels the task in slot, if any.
func cancelPostedTask(slot **postedTask) {
	if *slot != nil {
		(*slot).canceled = true
		*slot = nil
	}
}

// dispatchImageLoaderEvent fires eventType at the image, whose handler
// runs with the image as `this`: ImageLoader::DispatchPendingLoadEvent /
// DispatchPendingErrorEvent (image_loader.cc:1010-1030).
func (e *Engine) dispatchImageLoaderEvent(ctx *domContext, node *html.Node, eventType string) {
	fn := e.eventHandlers[node][eventType]
	if fn == nil {
		return
	}
	target := ctx.elementProxy(node)
	event := e.vm.NewObject()
	_ = event.Set("type", eventType)
	_ = event.Set("target", target)
	_ = event.Set("currentTarget", target)
	// An exception in one handler is reported and does not stop the
	// event loop.
	_, _ = fn(target, event)
}

// startParsedImageLoads starts, in tree order, the request of each parsed
// connected <img> in the document whose request script has not already
// replaced: a
// parsed image's load completes after the parser-blocking scripts and
// delays the window load event (Document load-event delay).
func (e *Engine) startParsedImageLoads(ctx *domContext) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		// <template> content is an unconnected fragment
		// (HTMLTemplateElement::content): its images are inert.
		if n.Type == html.ElementNode && n.TagName == "template" {
			return
		}
		if n.Type == html.ElementNode && n.TagName == "img" {
			if l := e.imageLoaders[n]; l == nil || !l.started {
				e.updateImageFromElement(ctx, n)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(e.doc.Root)
}

// toUint32 is WebIDL's ToUint32 conversion for an `unsigned long`
// argument: NaN and ±Infinity become 0, otherwise the value is truncated
// and taken modulo 2^32 (Web IDL §3.2.4.6, ConvertToUint32 in Blink's
// bindings).
func toUint32(v goja.Value) uint32 {
	f := v.ToFloat()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	m := math.Mod(math.Trunc(f), 1<<32)
	if m < 0 {
		m += 1 << 32
	}
	return uint32(m)
}
