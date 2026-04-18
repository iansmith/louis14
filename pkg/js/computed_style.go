package js

import (
	"strconv"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"

	"github.com/dop251/goja"
)

// computedStyleAccessor backs the object returned by window.getComputedStyle.
// It implements goja.DynamicObject so JS can read both camelCase and kebab-case
// property names, and also respond to methods like getPropertyValue.
//
// Values come from two sources:
//   - For layout-dependent properties (width, height, margin-*, padding-*,
//     border-*-width), the used value is read from the node's principal Box
//     and formatted as "Npx".
//   - For all other properties, the resolved string is read from the cascade
//     style map (css.Style.Properties), or "" if absent.
type computedStyleAccessor struct {
	vm     *goja.Runtime
	node   *html.Node
	styles map[*html.Node]*css.Style
	boxes  map[*html.Node]*layout.Box
}

// kebab-case CSS properties whose getComputedStyle value must be the used
// value in px from the fragment tree, not the cascade specified/computed
// string. This mirrors Blink's split between CSSOM "resolved value" categories
// (used vs computed) — see https://drafts.csswg.org/cssom/#resolved-values.
var layoutUsedValueProps = map[string]struct{}{
	"width":               {},
	"height":              {},
	"margin-top":          {},
	"margin-right":        {},
	"margin-bottom":       {},
	"margin-left":         {},
	"padding-top":         {},
	"padding-right":       {},
	"padding-bottom":      {},
	"padding-left":        {},
	"border-top-width":    {},
	"border-right-width":  {},
	"border-bottom-width": {},
	"border-left-width":   {},
}

func (c *computedStyleAccessor) Get(key string) goja.Value {
	switch key {
	case "getPropertyValue":
		return c.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return c.vm.ToValue("")
			}
			return c.vm.ToValue(c.resolve(call.Arguments[0].String()))
		})
	case "setProperty":
		// Real browsers throw NoModificationAllowedError here; returning
		// undefined is tolerant enough for WPT "readback then write" idioms.
		return c.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return goja.Undefined()
		})
	case "getPropertyPriority":
		return c.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return c.vm.ToValue("")
		})
	case "length":
		if style := c.style(); style != nil {
			return c.vm.ToValue(len(style.Properties))
		}
		return c.vm.ToValue(0)
	case "cssText":
		return c.vm.ToValue("")
	}

	return c.vm.ToValue(c.resolve(camelToKebab(key)))
}

func (c *computedStyleAccessor) Set(key string, val goja.Value) bool {
	// getComputedStyle's CSSStyleDeclaration is read-only in real browsers;
	// silently drop writes rather than throwing so passthru tests work.
	return true
}

func (c *computedStyleAccessor) Has(key string) bool {
	return true
}

func (c *computedStyleAccessor) Delete(key string) bool {
	return true
}

func (c *computedStyleAccessor) Keys() []string {
	style := c.style()
	if style == nil {
		return nil
	}
	keys := make([]string, 0, len(style.Properties))
	for k := range style.Properties {
		keys = append(keys, k)
	}
	return keys
}

func (c *computedStyleAccessor) style() *css.Style {
	if c.node == nil || c.styles == nil {
		return nil
	}
	return c.styles[c.node]
}

func (c *computedStyleAccessor) box() *layout.Box {
	if c.node == nil || c.boxes == nil {
		return nil
	}
	return c.boxes[c.node]
}

// resolve returns the CSSOM resolved value for a kebab-case property name.
// Layout-dependent properties come from the Box fragment (used values);
// everything else comes from the cascade Properties map.
func (c *computedStyleAccessor) resolve(prop string) string {
	if _, isLayoutDep := layoutUsedValueProps[prop]; isLayoutDep {
		if box := c.box(); box != nil {
			return usedValueFromBox(box, prop)
		}
	}
	if style := c.style(); style != nil {
		if v, ok := style.Properties[prop]; ok {
			return v
		}
	}
	return ""
}

// usedValueFromBox returns the used value for a layout-dependent CSS property,
// formatted as "Npx" per CSSOM resolved-value serialization rules.
func usedValueFromBox(box *layout.Box, prop string) string {
	var v float64
	switch prop {
	case "width":
		v = box.Width
	case "height":
		v = box.Height
	case "margin-top":
		v = box.Margin.Top
	case "margin-right":
		v = box.Margin.Right
	case "margin-bottom":
		v = box.Margin.Bottom
	case "margin-left":
		v = box.Margin.Left
	case "padding-top":
		v = box.Padding.Top
	case "padding-right":
		v = box.Padding.Right
	case "padding-bottom":
		v = box.Padding.Bottom
	case "padding-left":
		v = box.Padding.Left
	case "border-top-width":
		v = box.Border.Top
	case "border-right-width":
		v = box.Border.Right
	case "border-bottom-width":
		v = box.Border.Bottom
	case "border-left-width":
		v = box.Border.Left
	}
	return formatPx(v)
}

// formatPx formats a pixel length the way browsers do in getComputedStyle:
// integer values get no decimal, fractional values use the shortest
// round-trippable representation.
func formatPx(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10) + "px"
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + "px"
}
