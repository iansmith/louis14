package js

import (
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
// started, and the posted task that will dispatch its pending load or error
// event (Blink's pending_load_event_ / pending_error_event_).
type imageLoader struct {
	started      bool
	pendingEvent *postedTask
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
				node.Attributes[attr] = strconv.FormatUint(uint64(uint32(call.Argument(i).ToInteger())), 10)
			}
		}
		return ctx.elementProxy(node).ToObject(e.vm)
	})
}

// updateImageFromElement starts an <img>'s image request: after its src
// content attribute changed, or for a parsed image before the window load
// event (startParsedImageLoads). It replaces the previous request's
// undispatched event and posts the new request's. Mirrors ImageLoader::
// UpdateFromElement → DoUpdateFromElement (image_loader.cc:427-645): no
// src attribute means no request and no event; an empty or unloadable
// source fires error; an image that loads fires load. The src is completed
// against the document's base first (ImageLoader::ImageSourceToKURL,
// image_loader.cc:746 @ d4ecdfed88f962439247c2ad36b8fe47805b1520). With no
// fetcher only data: URLs load; any other source stays loading, with no
// event yet. Whether it loaded is decided when the event is dispatched,
// and only if a handler is listening.
func (e *Engine) updateImageFromElement(ctx *domContext, node *html.Node) {
	loader := e.imageLoaders[node]
	if loader == nil {
		loader = &imageLoader{}
		e.imageLoaders[node] = loader
	}
	loader.started = true
	if loader.pendingEvent != nil {
		loader.pendingEvent.canceled = true
		loader.pendingEvent = nil
	}
	src, ok := node.GetAttribute("src")
	if !ok {
		return
	}
	if src != "" {
		src = urlpkg.CompleteURL(src, e.doc.BaseDir)
		if !images.ImageLoadIsAttempted(src, e.imageFetcher) {
			return
		}
	}
	loader.pendingEvent = e.postTask(func() {
		loader.pendingEvent = nil
		e.dispatchImageLoaderEvent(ctx, node, src)
	})
}

// dispatchImageLoaderEvent fires the load or error event of node's request
// for src at the image, whose handler runs with the image as `this`:
// ImageLoader::DispatchPendingLoadEvent / DispatchPendingErrorEvent
// (image_loader.cc:1010-1030).
func (e *Engine) dispatchImageLoaderEvent(ctx *domContext, node *html.Node, src string) {
	handlers := e.eventHandlers[node]
	if handlers["load"] == nil && handlers["error"] == nil {
		return
	}
	eventType := "error"
	if src != "" && images.ImageIsPotentiallyAvailable(src, e.imageFetcher) {
		eventType = "load"
	}
	fn := handlers[eventType]
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
// <img> in the document whose request script has not already replaced: a
// parsed image's load completes after the parser-blocking scripts and
// delays the window load event (Document load-event delay).
func (e *Engine) startParsedImageLoads(ctx *domContext) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
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
