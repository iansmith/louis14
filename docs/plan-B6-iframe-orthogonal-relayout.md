# Plan B6: Orthogonal root iframe resize (orthogonal-root-resize-icb-*)

## Root Cause

Two missing JS engine features prevent the reftest mutation from occurring:

1. **`requestAnimationFrame` is not registered** on the goja runtime (`pkg/js/engine.go` `New()`).
2. **Element-level `onload` callbacks are never fired.** `iframe.onload = fn` is stored on the goja object but never dispatched. Only `window.onload` fires today.

Layout pipeline is already correct: once `iframe.style.height = "100px"` lands, `layoutNestedDocument` with `vpHeight=100` produces the expected 100×100 green square.

## Changes

### `pkg/js/engine.go`

1. Add `onloadCallbacks map[*html.Node]goja.Callable` to `Engine` struct; init in `New()`.
2. Register `requestAnimationFrame` and `cancelAnimationFrame` on vm. rAF fires its callback synchronously (single-threaded test env):
   ```go
   vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
       rafID++
       if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
           fn(goja.Undefined(), vm.ToValue(0))
       }
       return vm.ToValue(rafID)
   })
   ```
3. Add `RegisterOnloadCallback(node, fn)` method.
4. In `Execute()`, after `window.onload`, iterate `onloadCallbacks` and call each.
5. Also fire `<body onload="...">` attribute if present.

### `pkg/js/dom.go`

1. Add `engine *Engine` field to `domContext`; populate from `Execute()` after `registerDocument`.
2. In `elementAccessor.Set`, case `"onload"`: register via `ctx.engine.RegisterOnloadCallback(node, fn)`.
3. Update `Has()` and `Keys()` to surface `"onload"`.

### Layout

No changes. `layoutNestedDocument` + `computeOrthogonalAvailableBlock` already correct — `ctx.ViewportHeight` propagates the iframe's 100px to orthogonal children.

## Tests Fixed

- All 7 `orthogonal-root-resize-icb-*.html` (identical JS pattern).
- Unblocks `bidi-dynamic-iframe-001.html` partially (needs additional iframe.contentDocument APIs).
- Benefits several `css-overflow` and `css-sizing` tests using rAF.

## Regression Risk

Purely additive:
- rAF was previously undefined (ReferenceError); making it synchronous cannot regress tests that didn't use it.
- Element `onload` was stored but never fired; firing it cannot break non-reftest-wait tests.
- Zero layout algorithm changes.
