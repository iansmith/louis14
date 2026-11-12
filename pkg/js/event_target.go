package js

import (
	"github.com/dop251/goja"

	"louis14/pkg/html"
)

// registeredEventListener is one addEventListener registration on a node:
// its callback, and the capture and once options. Mirrors Blink's
// RegisteredEventListener (core/dom/events/registered_event_listener.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); passive and signal are not
// ported. raw is kept so removeEventListener can match by JS identity.
// attribute marks the entry of an event handler IDL attribute (onload),
// which Blink also keeps in the listener list.
type registeredEventListener struct {
	raw       goja.Value
	fn        goja.Callable
	capture   bool
	once      bool
	removed   bool
	attribute bool
}

// Event phases (Event.eventPhase), as in core/dom/events/event.h.
const (
	eventPhaseCapturing = 1
	eventPhaseAtTarget  = 2
	eventPhaseBubbling  = 3
)

// eventListenerOptions reads addEventListener / removeEventListener's
// third argument: an object is an options dictionary giving `capture` and
// `once` (a Boolean wrapper object too, as WebIDL converts any object in
// the union to the dictionary); anything else converts to the boolean
// `capture` (EventTarget::addEventListener overloads,
// core/dom/events/event_target.cc:528-583).
func eventListenerOptions(v goja.Value) (capture, once bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return false, false
	}
	if obj, ok := v.(*goja.Object); ok {
		get := func(name string) bool {
			m := obj.Get(name)
			return m != nil && m.ToBoolean()
		}
		return get("capture"), get("once")
	}
	return v.ToBoolean(), false
}

// addEventListener registers call's (type, callback, options) on target,
// ignoring a registration with the same callback and capture flag. Mirrors
// EventTarget::AddEventListenerInternal (event_target.cc:583-) over
// EventListenerMap::Add.
func (ctx *domContext) addEventListener(target *html.Node, call goja.FunctionCall) {
	if len(call.Arguments) < 2 {
		return
	}
	raw := call.Arguments[1]
	fn, ok := goja.AssertFunction(raw)
	if !ok {
		return
	}
	typ := call.Arguments[0].String()
	capture, once := eventListenerOptions(call.Argument(2))
	for _, l := range ctx.eventListeners[target][typ] {
		if !l.attribute && l.capture == capture && l.raw.StrictEquals(raw) {
			return
		}
	}
	ctx.appendListener(target, typ, &registeredEventListener{raw: raw, fn: fn, capture: capture, once: once})
}

// appendListener adds l to target's typ listeners, recording target's
// first registration so element load events fire in that order.
func (ctx *domContext) appendListener(target *html.Node, typ string, l *registeredEventListener) {
	if ctx.eventListeners == nil {
		ctx.eventListeners = make(map[*html.Node]map[string][]*registeredEventListener)
	}
	if ctx.eventListeners[target] == nil {
		ctx.eventListeners[target] = make(map[string][]*registeredEventListener)
		ctx.listenerTargets = append(ctx.listenerTargets, target)
	}
	ctx.eventListeners[target][typ] = append(ctx.eventListeners[target][typ], l)
}

// setAttributeEventListener sets target's event handler IDL attribute for
// typ (element.onload = value). A function replaces the handler's entry in
// place, or is appended as a new entry; anything else removes it. Mirrors
// EventTarget::SetAttributeEventListener (core/dom/events/
// event_target.cc:813-827 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (ctx *domContext) setAttributeEventListener(target *html.Node, typ string, value goja.Value) {
	fn, ok := goja.AssertFunction(value)
	for _, l := range ctx.eventListeners[target][typ] {
		if l.attribute {
			if ok {
				l.raw, l.fn = value, fn
			} else {
				ctx.removeRegisteredListener(target, typ, func(r *registeredEventListener) bool { return r == l })
			}
			return
		}
	}
	if ok {
		ctx.appendListener(target, typ, &registeredEventListener{raw: value, fn: fn, attribute: true})
	}
}

// hasEventListeners reports whether target has a typ listener or handler.
func (ctx *domContext) hasEventListeners(target *html.Node, typ string) bool {
	return len(ctx.eventListeners[target][typ]) > 0
}

// removeEventListener removes the registration of call's (type, callback)
// with the same capture flag. Mirrors EventTarget::removeEventListener
// (event_target.cc:728-770).
func (ctx *domContext) removeEventListener(target *html.Node, call goja.FunctionCall) {
	if len(call.Arguments) < 2 {
		return
	}
	typ := call.Arguments[0].String()
	raw := call.Arguments[1]
	capture, _ := eventListenerOptions(call.Argument(2))
	ctx.removeRegisteredListener(target, typ, func(l *registeredEventListener) bool {
		return !l.attribute && l.capture == capture && l.raw.StrictEquals(raw)
	})
}

// removeRegisteredListener removes target's typ listeners that match,
// marking them removed so a dispatch in progress skips them.
func (ctx *domContext) removeRegisteredListener(target *html.Node, typ string, match func(*registeredEventListener) bool) {
	listeners := ctx.eventListeners[target][typ]
	kept := listeners[:0]
	for _, l := range listeners {
		if match(l) {
			l.removed = true
		} else {
			kept = append(kept, l)
		}
	}
	if len(listeners) > 0 {
		ctx.eventListeners[target][typ] = kept
	}
}

// eventTargetValue is the JS object for node: the document object for the
// Document node, else node's proxy.
func (ctx *domContext) eventTargetValue(node *html.Node) goja.Value {
	if node == ctx.doc.Root {
		return ctx.vm.Get("document")
	}
	return ctx.elementProxy(node)
}

// domEvent is the state of an event being dispatched.
type domEvent struct {
	obj                 *goja.Object
	stopped, stoppedNow bool
}

// newDOMEvent creates the JS Event object for a dispatch of typ at target.
func (ctx *domContext) newDOMEvent(typ string, bubbles bool, target goja.Value) *domEvent {
	ev := &domEvent{obj: ctx.vm.NewObject()}
	_ = ev.obj.Set("type", typ)
	_ = ev.obj.Set("bubbles", bubbles)
	_ = ev.obj.Set("target", target)
	_ = ev.obj.Set("defaultPrevented", false)
	_ = ev.obj.Set("stopPropagation", func(goja.FunctionCall) goja.Value {
		ev.stopped = true
		return goja.Undefined()
	})
	_ = ev.obj.Set("stopImmediatePropagation", func(goja.FunctionCall) goja.Value {
		ev.stopped, ev.stoppedNow = true, true
		return goja.Undefined()
	})
	_ = ev.obj.Set("preventDefault", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	return ev
}

// dispatchEvent dispatches a typ event at target along its ancestor path
// to the document: capture listeners from the top down to the target, the
// target's listeners, then, if the event bubbles, bubble listeners back up;
// stopPropagation ends the dispatch after the current node. Mirrors
// EventDispatcher::Dispatch / DispatchEventAtCapturing /
// DispatchEventAtBubbling (core/dom/events/event_dispatcher.cc:181-370 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Event handler attributes run
// as entries of their target's list. The window step of the path is not
// part of it: louis14's window shares the document's listeners.
func (ctx *domContext) dispatchEvent(target *html.Node, typ string, bubbles bool) {
	var path []*html.Node
	for n := target; n != nil; n = n.Parent {
		path = append(path, n)
	}
	ev := ctx.newDOMEvent(typ, bubbles, ctx.eventTargetValue(target))
	// After dispatch the event has no current target and phase NONE
	// (EventDispatcher::DispatchEventPostProcess).
	defer func() {
		_ = ev.obj.Set("currentTarget", goja.Null())
		_ = ev.obj.Set("eventPhase", 0)
	}()
	for i := len(path) - 1; i >= 0; i-- {
		phase := eventPhaseCapturing
		if path[i] == target {
			phase = eventPhaseAtTarget
		}
		ctx.fireEventListeners(path[i], typ, ev, phase, true)
		if ev.stopped {
			return
		}
	}
	for i, node := range path {
		phase := eventPhaseBubbling
		if i == 0 {
			phase = eventPhaseAtTarget
		} else if !bubbles {
			return
		}
		ctx.fireEventListeners(node, typ, ev, phase, false)
		if ev.stopped {
			return
		}
	}
}

// fireEventListeners calls node's typ listeners whose capture flag is
// capture, in registration order, with node as `this` and currentTarget. A
// listener removed while the event is dispatched does not run; a once
// listener is removed before it runs; an exception in one listener is
// reported and does not stop the others. Mirrors
// EventTarget::FireEventListeners (event_target.cc:1002-1080).
func (ctx *domContext) fireEventListeners(node *html.Node, typ string, ev *domEvent, phase int, capture bool) {
	listeners := ctx.eventListeners[node][typ]
	if len(listeners) == 0 {
		return
	}
	current := ctx.eventTargetValue(node)
	_ = ev.obj.Set("currentTarget", current)
	_ = ev.obj.Set("eventPhase", phase)
	for _, l := range append([]*registeredEventListener(nil), listeners...) {
		if l.removed || l.capture != capture {
			continue
		}
		if ev.stoppedNow {
			break
		}
		if l.once {
			ctx.removeRegisteredListener(node, typ, func(r *registeredEventListener) bool { return r == l })
		}
		_, _ = l.fn(current, ev.obj)
	}
}

// fireDocumentLoadListeners fires the window load event at the listeners
// registered on the document and window, which share one table in
// louis14: capture listeners, then the others, as at a target.
func (ctx *domContext) fireDocumentLoadListeners() {
	ev := ctx.newDOMEvent("load", false, ctx.eventTargetValue(ctx.doc.Root))
	ctx.fireEventListeners(ctx.doc.Root, "load", ev, eventPhaseAtTarget, true)
	ctx.fireEventListeners(ctx.doc.Root, "load", ev, eventPhaseAtTarget, false)
}
