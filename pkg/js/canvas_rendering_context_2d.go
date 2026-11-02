package js

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics"
	"louis14/pkg/html"
	"louis14/pkg/images"
	urlpkg "louis14/pkg/url"

	"github.com/dop251/goja"
)

// This file ports the slice of Blink's canvas 2D API the louis14 corpus
// draws with (@ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// HTMLCanvasElement's width / height / getContext
// (core/html/canvas/html_canvas_element.cc), and CanvasRenderingContext2D's
// fillStyle / fillRect / drawImage (modules/canvas/canvas2d/
// canvas_2d_recorder_context.cc). The context draws into the canvas's
// bitmap (html.Node.CanvasBitmap) through pkg/graphics; HTMLCanvasPainter
// paints that bitmap (pkg/render).

// canvasRenderingContext2D is a <canvas>'s 2D context: the canvas and the
// drawing state (CanvasRenderingContext2DState) the corpus uses.
type canvasRenderingContext2D struct {
	canvas    *html.Node
	object    *goja.Object
	fillStyle css.Color
}

// reset restores the initial drawing state (CanvasRenderingContext2D::
// Reset → the default CanvasRenderingContext2DState): fillStyle black.
func (c *canvasRenderingContext2D) reset() {
	c.fillStyle = css.Color{A: 1}
}

// canvasGet answers the HTMLCanvasElement IDL attributes and operations of
// a <canvas>; ok is false for any other key.
func (e *elementAccessor) canvasGet(key string) (goja.Value, bool) {
	vm := e.ctx.vm
	switch key {
	case "width":
		w, _ := e.node.CanvasSize()
		return vm.ToValue(w), true
	case "height":
		_, h := e.node.CanvasSize()
		return vm.ToValue(h), true
	case "getContext":
		// HTMLCanvasElement::GetCanvasRenderingContext: "2d" creates the
		// canvas's one 2D context on first use and returns it after; other
		// context types are not supported (null).
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if call.Argument(0).String() != "2d" {
				return goja.Null()
			}
			return e.ctx.canvasContext2D(e.node).object
		}), true
	}
	return nil, false
}

// canvasSet sets a <canvas>'s width / height: setWidth / setHeight store an
// IDL unsigned long, the default above 0x7fffffff
// (SetUnsignedIntegralAttribute, html_canvas_element.cc:422-435); ok is
// false for any other key.
func (e *elementAccessor) canvasSet(key string, val goja.Value) bool {
	var def int
	switch key {
	case "width":
		def = html.DefaultCanvasWidth
	case "height":
		def = html.DefaultCanvasHeight
	default:
		return false
	}
	v := int64(toUint32(val))
	if v > 0x7fffffff {
		v = int64(def)
	}
	e.setAttribute(key, strconv.FormatInt(v, 10))
	return true
}

// canvasAttributeChanged resets a <canvas> whose width or height was
// assigned: the bitmap is cleared to the new size and the 2D context's
// state reset (HTMLCanvasElement::ParseAttribute → OnWidthOrHeightAssigned,
// html_canvas_element.cc:354-366, :805-830).
func (e *elementAccessor) canvasAttributeChanged(name string) {
	if name != "width" && name != "height" {
		return
	}
	e.node.CanvasBitmap = nil
	if c := e.ctx.canvasContexts[e.node]; c != nil {
		c.reset()
	}
}

// canvasContext2D returns canvas's 2D context, creating it on first use.
func (ctx *domContext) canvasContext2D(canvas *html.Node) *canvasRenderingContext2D {
	if c := ctx.canvasContexts[canvas]; c != nil {
		return c
	}
	if ctx.canvasContexts == nil {
		ctx.canvasContexts = make(map[*html.Node]*canvasRenderingContext2D)
	}
	c := &canvasRenderingContext2D{canvas: canvas}
	c.reset()
	ctx.canvasContexts[canvas] = c
	vm := ctx.vm
	obj := vm.NewObject()
	c.object = obj
	_ = obj.Set("canvas", ctx.elementProxy(canvas))
	_ = obj.DefineAccessorProperty("fillStyle",
		vm.ToValue(func(goja.FunctionCall) goja.Value { return vm.ToValue(serializeCanvasColor(c.fillStyle)) }),
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			// setFillStyle: a string that parses as a CSS <color> replaces
			// the style; anything else is ignored (canvas_2d_recorder_context.
			// cc:1037).
			if col, ok := ctx.parseCanvasColor(canvas, call.Argument(0).String()); ok {
				c.fillStyle = col
			}
			return goja.Undefined()
		}),
		goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("fillRect", func(call goja.FunctionCall) goja.Value {
		c.fillRect(call.Argument(0).ToFloat(), call.Argument(1).ToFloat(), call.Argument(2).ToFloat(), call.Argument(3).ToFloat())
		return goja.Undefined()
	})
	_ = obj.Set("drawImage", func(call goja.FunctionCall) goja.Value {
		c.drawImage(ctx, call)
		return goja.Undefined()
	})
	return c
}

// parseCanvasColor parses a fillStyle color string: a CSS <color>, with
// currentcolor resolving to the canvas element's computed color, or opaque
// black when it has none (ParseCanvasColorString /
// ParseColorOrCurrentColor, modules/canvas/canvas2d @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (ctx *domContext) parseCanvasColor(canvas *html.Node, s string) (css.Color, bool) {
	if strings.EqualFold(strings.TrimSpace(s), "currentcolor") {
		if style := ctx.layoutStyles[canvas]; style != nil {
			return style.GetColor(), true
		}
		return css.Color{A: 1}, true
	}
	return css.ParseColor(s)
}

// bitmap returns the canvas's bitmap, allocating it (transparent black) on
// first draw; nil for an empty canvas.
func (c *canvasRenderingContext2D) bitmap() *image.NRGBA {
	if c.canvas.CanvasBitmap == nil {
		w, h := c.canvas.CanvasSize()
		if w == 0 || h == 0 {
			return nil
		}
		c.canvas.CanvasBitmap = image.NewNRGBA(image.Rect(0, 0, w, h))
	}
	return c.canvas.CanvasBitmap
}

// fillRect mirrors Canvas2DRecorderContext::fillRect
// (canvas_2d_recorder_context.cc:1707-1760): a rect with a non-finite
// value draws nothing (ValidateRectForCanvas), a negative size extends the
// other way (AdjustRectForCanvas), and the rect fills with fillStyle,
// source-over.
func (c *canvasRenderingContext2D) fillRect(x, y, w, h float64) {
	r, ok := normalizedCanvasRect(x, y, w, h)
	if !ok {
		return
	}
	if bm := c.bitmap(); bm != nil {
		graphics.FillRect(bm, r, color.NRGBA{R: c.fillStyle.R, G: c.fillStyle.G, B: c.fillStyle.B, A: uint8(math.Round(c.fillStyle.A * 255))})
	}
}

// drawImage mirrors Canvas2DRecorderContext::drawImage
// (canvas_2d_recorder_context.cc:2012-2300): drawImage(image, dx, dy) draws
// the whole image at its natural size, (image, dx, dy, dw, dh) scales it to
// the destination, and the nine-argument form draws a source rect. The
// source rect is clipped to the image and the destination rect clipped in
// proportion (ClipRectsToImageRect); images are smoothed (the default
// imageSmoothingEnabled). An image that has not loaded draws nothing.
func (c *canvasRenderingContext2D) drawImage(ctx *domContext, call goja.FunctionCall) {
	// WebIDL overload resolution: drawImage takes 3, 5 or 9 arguments.
	switch n := len(call.Arguments); n {
	case 3, 5, 9:
	default:
		panic(ctx.vm.NewTypeError(fmt.Sprintf("Failed to execute 'drawImage' on 'CanvasRenderingContext2D': Valid arities are: [3, 5, 9], but %d arguments provided.", n)))
	}
	src := canvasImageSource(ctx, ctx.unwrapNode(call.Argument(0)))
	if src == nil {
		return
	}
	iw, ih := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
	arg := func(i int) float64 { return call.Argument(i).ToFloat() }
	var sx, sy, sw, sh, dx, dy, dw, dh float64
	switch len(call.Arguments) {
	case 3:
		sx, sy, sw, sh, dx, dy, dw, dh = 0, 0, iw, ih, arg(1), arg(2), iw, ih
	case 5:
		sx, sy, sw, sh, dx, dy, dw, dh = 0, 0, iw, ih, arg(1), arg(2), arg(3), arg(4)
	default:
		sx, sy, sw, sh, dx, dy, dw, dh = arg(1), arg(2), arg(3), arg(4), arg(5), arg(6), arg(7), arg(8)
	}
	srcRect, ok1 := normalizedCanvasRect(sx, sy, sw, sh)
	dstRect, ok2 := normalizedCanvasRect(dx, dy, dw, dh)
	if !ok1 || !ok2 {
		return
	}
	srcRect, dstRect = clipRectsToImageRect(srcRect, dstRect, iw, ih)
	bm := c.bitmap()
	if bm == nil {
		return
	}
	// A canvas drawn onto itself draws a snapshot of its bitmap
	// (HTMLCanvasElement::GetSourceImageForCanvas snapshots the resource),
	// not the pixels being written.
	if src == image.Image(bm) {
		src = &image.NRGBA{Pix: append([]uint8(nil), bm.Pix...), Stride: bm.Stride, Rect: bm.Rect}
	}
	graphics.DrawImageRect(bm, src, srcRect, dstRect, true)
}

// canvasImageSource returns the pixels of a CanvasImageSource: an <img>'s
// loaded image (its ImageLoader's request has loaded), oriented as drawn (HTMLImageElement::GetSourceImageForCanvas
// respects the image's EXIF orientation), or another <canvas>'s bitmap.
// Nil when there is nothing to draw: not an image source, not loaded, or an
// empty canvas.
func canvasImageSource(ctx *domContext, node *html.Node) image.Image {
	if node == nil || node.Type != html.ElementNode {
		return nil
	}
	switch node.TagName {
	case "canvas":
		if node.CanvasBitmap == nil {
			return nil
		}
		return node.CanvasBitmap
	case "img":
		// Only an image whose request has loaded is drawn
		// (HTMLImageElement::GetSourceImageForCanvas).
		src := node.ImageSourceURL()
		if src == "" || ctx.engine == nil {
			return nil
		}
		if l := ctx.engine.imageLoaders[node]; l == nil || !l.imageLoaded {
			return nil
		}
		src = urlpkg.CompleteURL(src, ctx.doc.BaseDir)
		if !images.ImageLoadIsAttempted(src, ctx.engine.imageFetcher) {
			return nil
		}
		img, err := images.OrientedImageWithFetcher(src, ctx.engine.imageFetcher)
		if err != nil || img.Bounds().Empty() {
			return nil
		}
		return img
	}
	return nil
}

// normalizedCanvasRect is a canvas rect with non-finite values rejected
// (ValidateRectForCanvas) and a negative width / height made positive by
// moving the origin (AdjustRectForCanvas / NormalizeRect).
func normalizedCanvasRect(x, y, w, h float64) (geometry.RectF, bool) {
	for _, v := range [4]float64{x, y, w, h} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return geometry.RectF{}, false
		}
	}
	if w < 0 {
		x, w = x+w, -w
	}
	if h < 0 {
		y, h = y+h, -h
	}
	return geometry.NewRectF(x, y, w, h), true
}

// clipRectsToImageRect clips srcRect to the image's bounds and shrinks
// dstRect by the same proportions (ClipRectsToImageRect,
// canvas_2d_recorder_context.cc).
func clipRectsToImageRect(srcRect, dstRect geometry.RectF, iw, ih float64) (geometry.RectF, geometry.RectF) {
	if srcRect.Width() == 0 || srcRect.Height() == 0 {
		return srcRect, geometry.RectF{}
	}
	sx, sy := dstRect.Width()/srcRect.Width(), dstRect.Height()/srcRect.Height()
	x0, y0 := max(srcRect.X(), 0), max(srcRect.Y(), 0)
	x1, y1 := min(srcRect.X()+srcRect.Width(), iw), min(srcRect.Y()+srcRect.Height(), ih)
	if x1 <= x0 || y1 <= y0 {
		return geometry.RectF{}, geometry.RectF{}
	}
	clipped := geometry.NewRectF(x0, y0, x1-x0, y1-y0)
	dst := geometry.NewRectF(
		dstRect.X()+(x0-srcRect.X())*sx, dstRect.Y()+(y0-srcRect.Y())*sy,
		(x1-x0)*sx, (y1-y0)*sy)
	return clipped, dst
}

// serializeCanvasColor serializes a fillStyle color as the canvas API does
// (HTML "serialization of a color"): #rrggbb when opaque, else
// rgba(r, g, b, a).
func serializeCanvasColor(c css.Color) string {
	if c.A >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.R, c.G, c.B, strconv.FormatFloat(c.A, 'f', -1, 64))
}
