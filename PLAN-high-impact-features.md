# High-Impact CSS Feature Implementation Plan

Six features that are parsed but produce no visual output, ordered by
ease of implementation and web prevalence.

Graphics context reference: `textshape.DrawContext` already supports
Stroke, SetLineWidth, QuadraticTo, CubicTo, DrawRoundedRectangle,
DrawCircle, Translate, Scale, Rotate, RotateAbout, PushGroup/PopGroupWithAlpha,
SetDash (stored but not applied in stroke expansion), and full path clipping.

---

## 1. Text Decoration (underline, overline, line-through)

**Web prevalence:** 83% of pages
**Difficulty:** Easy
**Why it matters:** Every `<a>` tag should have an underline. Without this,
links are visually indistinguishable from surrounding text.

### What exists
- CSS parsing: `GetTextDecoration()`, `GetTextDecorationColor()`,
  `GetTextDecorationStyle()`, `GetTextDecorationThickness()` — all implemented
  in `pkg/css/style.go`
- None of these getters are called anywhere in layout or paint

### Implementation

**Step 1 — Add fields to PaintLayer** (`pkg/render/paint_layer.go`)

```go
TextDecoration          css.TextDecoration   // none, underline, overline, line-through
TextDecorationColor     css.Color            // defaults to text color
TextDecorationThickness float64              // defaults to ~1px
```

Populate in `newPaintLayer()` from `style.GetTextDecoration()` etc.

**Step 2 — Draw decoration lines** (`pkg/render/render.go`, in `drawText()`)

After drawing each text run, check `layer.TextDecoration`:

- **underline**: draw a horizontal line at `baseline + descent * 0.15`
  (or use font's underline position if available). Width = text width.
  Thickness from `TextDecorationThickness` or default ~1px.
- **overline**: draw at `baseline - ascent`
- **line-through**: draw at `baseline - ascent * 0.35` (middle of x-height)

Use `SetLineWidth(thickness)` + `MoveTo/LineTo` + `Stroke()` for crisp lines.
Use `TextDecorationColor` if set, otherwise fall back to `TextColor`.

**Step 3 — Handle inheritance**

`text-decoration` is NOT inherited in CSS, but the spec says decorations
propagate to descendants visually. The simplest correct approach: treat it
as inherited for paint purposes (it already appears in the inheritable list
in cascade.go). This matches how browsers actually render it for most cases.

### Edge cases to defer
- `text-decoration-style: wavy/dotted/dashed` — start with solid only
- Skip decorations on `display: inline-block` children (per spec)
- Multiple decorations (`underline line-through`) — support later

### Estimated scope
~50 lines of code across 2 files.

---

## 2. Margin Auto Centering for Block Elements

**Web prevalence:** 91% of pages use margin; auto centering is extremely common
**Difficulty:** Easy
**Why it matters:** `margin: 0 auto` is the most basic centering technique.
Without it, centered page layouts (max-width containers, centered cards) are
all left-aligned.

### What exists
- Margin resolution in `pkg/layout/fragment_geometry.go:ResolveMargins()`
  returns 0 for auto margins
- Auto margin centering works for absolutely positioned elements
  (`pkg/layout/out_of_flow_layout.go:192-208`)
- Block layout just uses resolved margins directly without checking for auto

### Implementation

**Step 1 — Detect auto margins** (`pkg/css/style.go`)

Add a method (or use the existing property check):
```go
func (s *Style) HasAutoMargin(prop string) bool {
    val := s.Get(prop)
    return val == "auto"
}
```

**Step 2 — Apply centering in block layout** (`pkg/layout/block_layout.go`)

In the block layout function, after resolving the child's inline size,
check if both inline-start and inline-end margins are auto:

```go
// After resolving child width
autoStart := childStyle.HasAutoMargin("margin-left")  // or logical equivalent
autoEnd := childStyle.HasAutoMargin("margin-right")    // or logical equivalent

if childHasDefiniteInlineSize && autoStart && autoEnd {
    remaining := availableInlineSize - childInlineSize - borders - padding
    if remaining > 0 {
        halfMargin := remaining / 2
        childInlineOffset = halfMargin
    }
} else if autoStart && !autoEnd {
    remaining := availableInlineSize - childInlineSize - childMargins.InlineEnd
    childInlineOffset = max(0, remaining)
} else if !autoStart && autoEnd {
    // normal: auto end margin absorbs remaining space (no change needed)
}
```

This mirrors the logic already in `out_of_flow_layout.go`.

**Step 3 — Handle edge cases**
- If element has no definite width, auto margins resolve to 0 (current behavior is fine)
- Floated or absolutely positioned elements: auto margins resolve to 0 (already handled)
- Negative remaining space: start margin wins (per CSS 2.1 §10.3.3)

### Estimated scope
~30 lines of code across 2 files.

---

## 3. Border Radius

**Web prevalence:** 86% of pages
**Difficulty:** Medium
**Why it matters:** Rounded corners are ubiquitous in modern design. Every
button, card, avatar, input field, and modal uses border-radius. Without it,
everything looks like 1998.

### What exists
- CSS parsing: `GetBorderRadiusCorners()` and `GetBorderRadiusCornersElliptical()`
  fully implemented in `pkg/css/style.go`
- `DrawContext` has `DrawRoundedRectangle(x, y, w, h, r)` built in
- `DrawContext` has `QuadraticTo()` and `CubicTo()` for custom curves
- PaintLayer has no border-radius fields

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
BorderRadius [4]float64  // TopLeft, TopRight, BottomRight, BottomLeft (px)
// Start with circular radii; elliptical can come later
```

Populate from `style.GetBorderRadiusCorners()`. Apply the CSS corner-overlap
reduction algorithm (scale all radii proportionally if sum exceeds box dimension).

**Step 2 — Rounded background clipping** (`pkg/render/render.go`, `drawBackground()`)

Before drawing the background color/image/gradient, build a rounded-rect
clip path:

```go
func (r *Renderer) buildRoundedRectPath(x, y, w, h float64, radii [4]float64) {
    tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
    r.dc.MoveTo(x+tl, y)
    r.dc.LineTo(x+w-tr, y)
    r.dc.QuadraticTo(x+w, y, x+w, y+tr)         // top-right corner
    r.dc.LineTo(x+w, y+h-br)
    r.dc.QuadraticTo(x+w, y+h, x+w-br, y+h)     // bottom-right corner
    r.dc.LineTo(x+bl, y+h)
    r.dc.QuadraticTo(x, y+h, x, y+h-bl)          // bottom-left corner
    r.dc.LineTo(x, y+tl)
    r.dc.QuadraticTo(x, y, x+tl, y)              // top-left corner
    r.dc.ClosePath()
}
```

Use this path for:
1. Background fill (clip, then fill, then reset clip)
2. Border drawing (stroke along the same path with appropriate widths)

**Step 3 — Rounded borders** (`pkg/render/render.go`, `drawBorders()`)

Replace the current trapezoid-based border drawing with path-based rounded
borders when any radius > 0. For uniform borders (same width+color all sides),
this is straightforward: stroke the rounded rect path. For non-uniform borders,
draw each side as a separate curved segment.

Simple approach for uniform borders:
```go
r.buildRoundedRectPath(x, y, w, h, radii)
r.dc.SetLineWidth(borderWidth)
r.dc.Stroke()
```

**Step 4 — Overflow clipping with border-radius**

When `overflow: hidden` + `border-radius`, the clip rect should be rounded.
Use the same `buildRoundedRectPath` + `Clip()` instead of `DrawRectangle` + `Clip()`.

### Corner overlap reduction algorithm (CSS Backgrounds §5.5)
```
f = min(width/(r_tl+r_tr), width/(r_bl+r_br), height/(r_tl+r_bl), height/(r_tr+r_br))
if f < 1: scale all radii by f
```

### Edge cases to defer
- Elliptical radii (different horizontal/vertical) — start with circular
- Per-corner different border widths with radius — complex geometry
- Border-radius on tables

### Estimated scope
~120 lines of code across 2 files. The DrawContext already has the hard
primitives (QuadraticTo, clip paths).

---

## 4. Box Shadow

**Web prevalence:** 82% of pages
**Difficulty:** Medium
**Why it matters:** Shadows give depth to cards, buttons, dropdowns, modals,
and tooltips. Most material/modern designs rely heavily on box-shadow.

### What exists
- CSS parsing: `GetBoxShadow()` returns `[]BoxShadow` with OffsetX, OffsetY,
  Blur, Spread, Color, Inset — fully implemented
- DrawContext has PushGroup/PopGroupWithAlpha for compositing
- DrawContext has DrawRoundedRectangle for shadow shapes
- No shadow rendering code exists

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
BoxShadows []css.BoxShadow
```

Populate from `style.GetBoxShadow()`.

**Step 2 — Draw shadows before background** (`pkg/render/render.go`)

In `paintLayerContent()`, add shadow drawing before `drawBackground()`.
Shadows paint behind the element (outset) or inside the element (inset).

**Step 3 — Shadow rendering without blur** (MVP)

Start without blur (many real shadows use 0 blur or are subtle enough):

```go
func (r *Renderer) drawBoxShadows(layer *PaintLayer) {
    box := layer.Box
    for _, shadow := range layer.BoxShadows {
        if shadow.Inset { continue } // defer inset shadows
        
        sx := box.X + shadow.OffsetX - shadow.Spread
        sy := box.Y + shadow.OffsetY - shadow.Spread
        sw := box.Width + 2*shadow.Spread
        sh := box.Height + 2*shadow.Spread
        
        r.setColor(shadow.Color)
        // Use border-radius of element if available
        if hasRadius(layer) {
            r.buildRoundedRectPath(sx, sy, sw, sh, layer.BorderRadius)
            r.dc.Fill()
        } else {
            r.dc.DrawRectangle(sx, sy, sw, sh)
            r.dc.Fill()
        }
    }
}
```

**Step 4 — Gaussian blur** (enhancement)

For blur > 0, render shadow to an offscreen buffer, apply a box blur
(3-pass box blur approximates Gaussian), composite back:

```go
// Create offscreen buffer slightly larger than shadow bounds
buf := image.NewRGBA(image.Rect(0, 0, int(sw+6*blur), int(sh+6*blur)))
childCtx := r.dc.NewChildContext(buf)
// Draw shadow shape into buffer
// Apply 3-pass box blur (horizontal then vertical, 3 times)
// Composite buffer back to main canvas
```

A separable box blur is ~30 lines. Three passes approximate Gaussian well.

**Step 5 — Clip shadow behind element**

Outset shadows should not be visible where the element's background is drawn.
Clip out the element's border-box from the shadow before compositing.

### Edge cases to defer
- Inset shadows — need to clip to inside of border-box
- Multiple shadows — already supported by loop, just need correct paint order
  (last declared shadow paints first, behind others)

### Estimated scope
~100 lines for no-blur MVP, ~180 lines with blur support.

---

## 5. CSS Transforms

**Web prevalence:** 84% of pages
**Difficulty:** Medium-Hard
**Why it matters:** Transforms are used for centering (`translate(-50%, -50%)`),
hover effects, layout adjustments, and visual polish. Many modern sites break
visually without transform support.

### What exists
- CSS parsing: `GetTransforms()` returns `[]Transform` with Type and Values
  for translate, rotate, scale, skew, matrix — fully implemented
- `GetTransformOrigin()` returns origin point
- DrawContext has full 2D affine transforms: `Translate()`, `Scale()`,
  `Rotate()`, `RotateAbout()`, `TransformPoint()`
- DrawContext has `Push()/Pop()` for state save/restore

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
Transforms      []css.Transform
TransformOrigin [2]float64  // resolved to px (origin-x, origin-y)
HasTransform    bool
```

Populate from `style.GetTransforms()` and `style.GetTransformOrigin()`.

**Step 2 — Apply transforms during paint** (`pkg/render/render.go`)

In `paintLayer()`, wrap the entire layer painting in a transform:

```go
if layer.HasTransform {
    r.dc.Push()
    ox := layer.Box.X + layer.TransformOrigin[0]
    oy := layer.Box.Y + layer.TransformOrigin[1]
    r.dc.Translate(ox, oy)
    for _, t := range layer.Transforms {
        switch t.Type {
        case "translate":
            r.dc.Translate(t.Values[0], t.Values[1])
        case "translateX":
            r.dc.Translate(t.Values[0], 0)
        case "translateY":
            r.dc.Translate(0, t.Values[0])
        case "rotate":
            r.dc.Rotate(t.Values[0]) // already in radians
        case "scale":
            r.dc.Scale(t.Values[0], t.Values[1])
        case "scaleX":
            r.dc.Scale(t.Values[0], 1)
        case "scaleY":
            r.dc.Scale(1, t.Values[0])
        case "skew":
            // Implement via matrix
        case "matrix":
            // a,b,c,d,e,f → apply as affine
        }
    }
    r.dc.Translate(-ox, -oy)
    // ... paint layer content ...
    r.dc.Pop()
}
```

**Step 3 — Stacking context**

Elements with transforms must create stacking contexts. Update
`CreatesStackingContext()` in `pkg/layout/types.go` to check for transforms.

**Step 4 — Containing block for fixed elements**

Elements with transforms become containing blocks for fixed-position
descendants. This affects `out_of_flow_layout.go`.

**Step 5 — Percentage translate values**

`translate(50%, -50%)` is relative to the element's own size, not the
containing block. Resolve these percentages against `box.Width`/`box.Height`
when populating PaintLayer.

### Edge cases to defer
- `transform-style: preserve-3d` / perspective — 3D transforms
- Transform affecting hit testing (not relevant for static renderer)
- `will-change: transform` optimization

### Estimated scope
~80 lines for core transform application, ~40 lines for stacking context
and containing block fixes. Total ~120 lines.

---

## 6. Border Styles (dashed, dotted, double)

**Web prevalence:** 90% of pages use borders; dashed/dotted appear on ~20-30%
**Difficulty:** Easy-Medium
**Why it matters:** Dashed borders are common for drag targets, placeholder
areas, and separators. Dotted borders appear on focus outlines and form fields.
Currently these all render as solid.

### What exists
- CSS parsing: `BorderStyle` enum with None, Solid, Dashed, Dotted, Double
- PaintLayer already stores `BorderStyles [4]css.BorderStyle`
- Current `drawBorders()` only checks `!= None` and draws solid trapezoids
- DrawContext has `SetLineWidth()` and `Stroke()` but `SetDash()` stores
  dash patterns without applying them in the stroke expansion

### Implementation

**Step 1 — Dashed borders** (`pkg/render/render.go`)

For dashed borders, instead of filling a trapezoid, draw dashes along the
border edge. The CSS spec says dash length = 3× border-width, gap = same.

```go
case css.BorderStyleDashed:
    dashLen := borderWidth * 3
    gapLen := borderWidth * 3
    // Walk along the edge, drawing filled rectangles for each dash
    along := 0.0
    for along < edgeLength {
        segLen := min(dashLen, edgeLength-along)
        // Draw a short filled rectangle at current position
        along += segLen + gapLen
    }
```

Alternative: implement `SetDash()` in the DrawContext's stroke expansion
(in `mazarin/textshape/draw_impl.go`), then use `Stroke()` with dash pattern.
This is more correct but requires modifying the textshape library.

**Step 2 — Dotted borders**

For dotted borders, draw circles (or filled squares for thin borders) at
regular intervals along the edge. Dot diameter = border-width, gap = border-width.

```go
case css.BorderStyleDotted:
    dotSize := borderWidth
    gap := borderWidth
    along := dotSize / 2
    for along < edgeLength {
        cx := startX + along * dx  // dx/dy = unit direction along edge
        cy := startY + along * dy
        r.dc.DrawCircle(cx, cy, dotSize/2)
        r.dc.Fill()
        along += dotSize + gap
    }
```

**Step 3 — Double borders**

For double borders, draw two parallel lines with a gap between them.
Each line width = border-width / 3, gap = border-width / 3.

```go
case css.BorderStyleDouble:
    lineW := borderWidth / 3
    // Draw outer border (inset by 0)
    drawSolidBorder(outerEdge, lineW)
    // Draw inner border (inset by 2*lineW)
    drawSolidBorder(innerEdge, lineW)
```

**Step 4 — Refactor drawBorders()**

The current `drawBorders()` draws four trapezoids. Refactor to:
1. Check style per side
2. For solid: current trapezoid code (unchanged)
3. For dashed/dotted/double: call new style-specific drawing functions
4. Each function receives: start point, end point, width, color

### Estimated scope
~100 lines for dashed + dotted, ~40 lines for double. Total ~140 lines.

---

## Summary

| # | Feature | Web % | Difficulty | Lines (est.) | Depends On |
|---|---------|-------|-----------|-------------|------------|
| 1 | Text decoration | 83% | Easy | ~50 | Nothing |
| 2 | Margin auto centering | 91% | Easy | ~30 | Nothing |
| 3 | Border radius | 86% | Medium | ~120 | Nothing |
| 4 | Box shadow | 82% | Medium | ~100-180 | Border radius (for rounded shadows) |
| 5 | CSS transforms | 84% | Medium-Hard | ~120 | Nothing |
| 6 | Border styles | 90% | Easy-Medium | ~140 | Nothing |

**Total estimated new code: ~560-640 lines**

Items 1, 2, and 6 are independent and can be done in parallel by separate agents.
Item 4 benefits from item 3 being done first (rounded shadow shapes).
Item 5 is independent but more complex.

### Suggested execution order

**Batch 1 (parallel, easy wins):**
- Agent A: Text decoration (#1)
- Agent B: Margin auto centering (#2)
- Agent C: Border styles (#6)

**Batch 2 (parallel, medium):**
- Agent A: Border radius (#3)
- Agent B: CSS transforms (#5)

**Batch 3 (depends on #3):**
- Agent A: Box shadow (#4)

Each batch should be committed and tested before starting the next.
