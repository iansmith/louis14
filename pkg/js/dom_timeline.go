package js

import (
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// registerDocumentTimeline defines document.timeline, the document's default
// DocumentTimeline, with a read-only currentTime that reads the document's
// AnimationClock in milliseconds. Mirrors Blink AnimationTimeline::currentTime
// (core/animation/animation_timeline.cc:54) over
// DocumentTimeline::CurrentPhaseAndTime, which reads
// document->GetAnimationClock().CurrentTime()
// (core/animation/document_timeline.cc:170, :52), all @
// d4ecdfed88f962439247c2ad36b8fe47805b1520. The clock's zero is the
// timeline's zero, so no origin time is subtracted.
func registerDocumentTimeline(vm *goja.Runtime, docObj *goja.Object, doc *html.Document) {
	timeline := vm.NewObject()
	currentTimeGetter := vm.ToValue(func(goja.FunctionCall) goja.Value {
		return vm.ToValue(frameTimeMs(doc.AnimationClock.CurrentTime()))
	})
	_ = timeline.DefineAccessorProperty("currentTime", currentTimeGetter, nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
	_ = docObj.Set("timeline", timeline)
}
