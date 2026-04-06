# Medium-Impact CSS Feature Implementation Plan

Six features that are parsed but produce no visual output, ordered by
ease of implementation and web prevalence. These build on the high-impact
features already shipped (border-radius, box-shadow, transforms, etc.).

---

## 1. Text Transform (uppercase, lowercase, capitalize)

**Web prevalence:** 68% of pages
**Difficulty:** Easy
**Why it matters:** Navigation bars, buttons, and headings frequently use
`text-transform: uppercase`. Without this, styled text appears in its
original case, making many UIs look wrong.

### What exists
- CSS parsing: `GetTextTransform()` returns the value string
- Not called anywhere in layout or paint

### Implementation

**Step 1 — Apply transform during text shaping** (`pkg/render/render.go`)

The simplest correct approach: transform the text string before drawing.
In `drawText()`, after getting `box.Text` and before any drawing:

```go
text := box.Text
switch layer.TextTransform {
case "uppercase":
    text = strings.ToUpper(text)
case "lowercase":
    text = strings.ToLower(text)
case "capitalize":
    text = capitalizeWords(text)
}
```

For `capitalize`, title-case the first letter of each word:
```go
func capitalizeWords(s string) string {
    inWord := false
    var b strings.Builder
    for _, r := range s {
        if unicode.IsSpace(r) || unicode.IsPunct(r) {
            inWord = false
            b.WriteRune(r)
        } else if !inWord {
            inWord = true
            b.WriteRune(unicode.ToUpper(r))
        } else {
            b.WriteRune(r)
        }
    }
    return b.String()
}
```

**Step 2 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
TextTransform string // none, uppercase, lowercase, capitalize
```

Populate from `s.GetTextTransform()`.

**Step 3 — Also transform for text measurement**

The transform should also apply in layout (line breaking uses the
transformed text width). Check if `GetTextTransform()` is applied in
`pkg/layout/line_breaker.go` during text measurement. If not, add it
there too so line widths match the rendered text.

### Edge cases
- Unicode: `strings.ToUpper` handles Unicode correctly in Go
- `capitalize` should only uppercase the first letter of each "word"
  (separated by whitespace per CSS spec)

### Estimated scope
~30 lines across 2 files.

---

## 2. Outline Rendering

**Web prevalence:** 72% of pages (focus outlines, accessibility)
**Difficulty:** Easy
**Why it matters:** Outlines are critical for accessibility — focus rings
on buttons, inputs, and links. Without outline rendering, `:focus` styles
are invisible, and many designs that use `outline` for decoration are broken.

### What exists
- CSS parsing: `GetOutlineStyle()`, `GetOutlineWidth()`, `GetOutlineColor()`,
  `GetOutlineOffset()` — all implemented
- Not used anywhere in paint code

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
OutlineStyle  css.BorderStyle // none, solid, dashed, dotted, double
OutlineWidth  float64
OutlineColor  css.Color
OutlineOffset float64         // gap between border edge and outline
```

Populate from the outline getters.

**Step 2 — Draw outline after borders** (`pkg/render/render.go`)

In `paintLayerContent()`, add `r.drawOutline(layer)` after `r.drawBorders(layer)`.

Outlines differ from borders: they don't affect layout, they can overlap
other content, and they honor `outline-offset`. Draw at:
  `border-box + outline-offset + outline-width/2`

```go
func (r *Renderer) drawOutline(layer *PaintLayer) {
    if layer.OutlineStyle == css.BorderStyleNone || layer.OutlineWidth <= 0 {
        return
    }
    box := layer.Box
    x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)
    off := layer.OutlineOffset + layer.OutlineWidth/2
    ox := x - off
    oy := y - off
    ow := w + 2*off
    oh := h + 2*off

    r.setColor(layer.OutlineColor)
    r.dc.SetLineWidth(layer.OutlineWidth)

    if hasBorderRadius(layer) {
        expandedRadii := [4]float64{...} // expand by offset
        r.buildRoundedRectPath(ox, oy, ow, oh, expandedRadii)
    } else {
        r.dc.DrawRectangle(ox, oy, ow, oh)
    }
    r.dc.Stroke()
}
```

For dashed/dotted outlines, reuse the border style helpers with midline
coordinates.

### Estimated scope
~50 lines across 2 files.

---

## 3. Object-Fit and Object-Position for Images

**Web prevalence:** 48% of pages (growing rapidly)
**Difficulty:** Easy-Medium
**Why it matters:** `object-fit: cover` and `object-fit: contain` are
the standard way to display images that don't match their container's
aspect ratio. Without this, images are stretched/squished to fill the
box, which looks wrong on virtually every modern image gallery, avatar,
or hero image.

### What exists
- CSS parsing: `GetObjectFit()` and `GetObjectPosition()` return values
- `drawImage()` in render.go loads and draws images but does not apply
  object-fit — images are scaled to fill the content box

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
ObjectFit      string  // fill (default), contain, cover, none, scale-down
ObjectPosition [2]float64 // percentage (0.5, 0.5 = center center)
```

**Step 2 — Apply in drawImage()** (`pkg/render/render.go`)

After loading the image and determining the content box, compute the
draw rect based on object-fit:

```go
// Content box
cx, cy, cw, ch := contentBox(box)
imgW, imgH := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())

switch layer.ObjectFit {
case "contain":
    // Scale to fit within content box, preserving aspect ratio
    scale := min(cw/imgW, ch/imgH)
    dw, dh := imgW*scale, imgH*scale
    dx := cx + (cw-dw)*layer.ObjectPosition[0]
    dy := cy + (ch-dh)*layer.ObjectPosition[1]
    // Draw at (dx, dy, dw, dh)

case "cover":
    // Scale to cover content box, preserving aspect ratio
    scale := max(cw/imgW, ch/imgH)
    dw, dh := imgW*scale, imgH*scale
    dx := cx + (cw-dw)*layer.ObjectPosition[0]
    dy := cy + (ch-dh)*layer.ObjectPosition[1]
    // Clip to content box, draw at (dx, dy, dw, dh)

case "none":
    // Draw at intrinsic size, positioned by object-position
    dx := cx + (cw-imgW)*layer.ObjectPosition[0]
    dy := cy + (ch-imgH)*layer.ObjectPosition[1]
    // Clip to content box, draw at (dx, dy)

case "scale-down":
    // Like contain, but never scales up
    scale := min(1, min(cw/imgW, ch/imgH))
    // ...same as contain with clamped scale

default: // "fill"
    // Current behavior: stretch to fill
}
```

For cover/none, clip to the content box before drawing to prevent
overflow.

### Estimated scope
~60 lines across 2 files.

---

## 4. Text Shadow

**Web prevalence:** 43% of pages
**Difficulty:** Medium
**Why it matters:** Text shadows are common on headings, hero text, and
buttons for readability over images. Similar architecture to box-shadow
but applied per text run.

### What exists
- CSS parsing: `GetTextShadow()` returns `[]TextShadow` with OffsetX,
  OffsetY, Blur, Color — fully implemented
- No rendering code exists

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
TextShadows []css.TextShadow
```

**Step 2 — Draw shadows before text** (`pkg/render/render.go`)

In `drawText()`, before drawing the actual text, draw shadow copies.
For each shadow (in reverse order):

```go
for i := len(layer.TextShadows) - 1; i >= 0; i-- {
    shadow := layer.TextShadows[i]
    r.setColor(shadow.Color)
    if shadow.Blur > 0 {
        // Render text to offscreen buffer, blur, composite
        r.drawBlurredTextShadow(layer, box, fontID, shadow)
    } else {
        // Draw text offset by shadow position
        r.dc.DrawText(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY)
    }
}
// Then draw the actual text
r.setColor(layer.TextColor)
r.dc.DrawText(text, fontID, box.X, box.Y+ascent)
```

For blurred text shadows, reuse the offscreen buffer + boxBlur approach
from box-shadow, but render text into the buffer instead of a shape.

### Estimated scope
~70 lines across 2 files (leveraging existing boxBlur).

---

## 5. List Markers (disc, circle, square, decimal)

**Web prevalence:** 78% of pages use lists
**Difficulty:** Medium
**Why it matters:** `<ul>` and `<ol>` are among the most common HTML
elements. Without list markers, bulleted and numbered lists are
indistinguishable from regular paragraphs. Navigation menus, feature
lists, TOCs, and documentation all rely on visible markers.

### What exists
- CSS parsing: `GetListStyleType()`, `GetListStylePosition()` implemented
- Layout: `display: list-item` is parsed but markers are not generated
- The default UA stylesheet sets `padding-left: 40px` for `<ul>`/`<ol>`,
  so the indent exists but the marker area is empty

### Implementation

**Step 1 — Track list-item display** (`pkg/render/paint_layer.go`)

```go
ListStyleType     string // disc, circle, square, decimal, none
ListStylePosition string // outside (default), inside
ListItemIndex     int    // ordinal position (1-based for <ol>)
IsListItem        bool
```

Populate from style and the element's DOM position among siblings.

**Step 2 — Draw markers during paint** (`pkg/render/render.go`)

In `paintLayerContent()`, after drawing background/borders and before
text, draw the list marker if `IsListItem`:

```go
if layer.IsListItem && layer.ListStyleType != "none" {
    r.drawListMarker(layer)
}
```

For unordered lists:
- **disc**: filled circle at marker position
- **circle**: stroked circle
- **square**: filled square

Marker position for `outside`: to the left of the padding box, centered
vertically on the first line. X = box.X + border.Left - markerOffset.
Y = box.Y + border.Top + firstLineBaseline - markerSize/2.

For ordered lists:
- **decimal**: draw the number string + "." using the same font
- Position similarly to unordered markers

```go
func (r *Renderer) drawListMarker(layer *PaintLayer) {
    box := layer.Box
    // Marker size ≈ 0.4em
    markerSize := layer.FontSize * 0.4
    // Position: left of content, vertically at first line midpoint
    mx := box.X + box.Border.Left - markerSize*2
    my := box.Y + box.Border.Top + layer.FontSize*0.5

    r.setColor(layer.TextColor)
    switch layer.ListStyleType {
    case "disc":
        r.dc.DrawCircle(mx, my, markerSize/2)
        r.dc.Fill()
    case "circle":
        r.dc.DrawCircle(mx, my, markerSize/2)
        r.dc.SetLineWidth(1)
        r.dc.Stroke()
    case "square":
        r.dc.DrawRectangle(mx-markerSize/2, my-markerSize/2, markerSize, markerSize)
        r.dc.Fill()
    case "decimal":
        numStr := fmt.Sprintf("%d.", layer.ListItemIndex)
        // Draw right-aligned at marker position
        fontPath := r.fonts.FontPathForFamily(layer.FontFamily, layer.FontBold, layer.FontItalic, layer.FontMono, layer.FontAhem)
        fid := r.openFont(fontPath, layer.FontSize)
        if fid >= 0 {
            tw := r.dc.MeasureText(numStr, fid)
            r.dc.DrawText(numStr, fid, mx-tw, box.Y+box.Border.Top+float64(r.dc.GetFontMetrics(fid).Ascent)/64.0)
        }
    }
}
```

**Step 3 — Determine list item index**

In `newPaintLayer()`, if the element is `display: list-item`, count its
position among sibling list items. This requires walking the DOM parent's
children. If `box.Node` is available:

```go
if box.Node != nil && box.Node.Parent != nil {
    idx := 1
    // Check for <ol start="N"> attribute
    if startAttr, ok := box.Node.Parent.GetAttribute("start"); ok {
        if n, err := strconv.Atoi(startAttr); err == nil { idx = n }
    }
    for _, sibling := range box.Node.Parent.Children {
        if sibling == box.Node { break }
        if sibling.TagName != "" && !sibling.IsText() { idx++ }
    }
    layer.ListItemIndex = idx
}
```

### Estimated scope
~80 lines across 2 files.

---

## 6. CSS Filter Effects (opacity, blur, grayscale, brightness)

**Web prevalence:** 38% of pages
**Difficulty:** Medium-Hard
**Why it matters:** `filter: blur()` is used for frosted glass effects
and loading states. `filter: grayscale()` is used for hover effects and
disabled states. `filter: brightness()` and `filter: contrast()` adjust
image appearance. Without filters, these visual states are missing.

### What exists
- CSS parsing: `GetFilter()` returns the raw filter string
- DrawContext has `NewChildContext()` for offscreen rendering and
  `PushGroup()`/`PopGroupWithAlpha()` for compositing
- `boxBlur()` already implemented for box-shadow (reusable)

### Implementation

**Step 1 — Parse filter functions** (`pkg/css/style.go` or `pkg/render/`)

Parse the filter string into a list of operations:
```go
type FilterOp struct {
    Type  string  // blur, brightness, contrast, grayscale, opacity, saturate, sepia
    Value float64 // parameter value
}
```

Parse `filter: blur(5px) grayscale(100%)` into `[]FilterOp`.

**Step 2 — Apply filters during paint** (`pkg/render/render.go`)

In `paintLayer()`, when filters are present, render the entire subtree
to an offscreen buffer, apply filters, then composite:

```go
if layer.HasFilter {
    // Determine buffer bounds (element border-box + filter extension like blur)
    buf := image.NewRGBA(...)
    childDC := r.dc.NewChildContext(buf)
    // Paint layer content into childDC
    // Apply filter operations to buf
    // Composite buf back to main canvas
}
```

**Step 3 — Implement individual filters**

- **blur(Npx)**: reuse `boxBlur()` (already exists)
- **grayscale(N%)**: iterate pixels, convert to luminance, blend
- **brightness(N)**: multiply RGB channels by N
- **contrast(N)**: adjust channels: `((c/255 - 0.5) * N + 0.5) * 255`
- **opacity(N)**: multiply alpha by N (simpler than element opacity)
- **saturate(N)**: adjust saturation via HSL conversion
- **sepia(N)**: apply sepia matrix

Each filter is ~5-10 lines of pixel manipulation:
```go
func applyGrayscale(img *image.RGBA, amount float64) {
    for i := 0; i < len(img.Pix); i += 4 {
        r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
        if a == 0 { continue }
        lum := uint8(0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b))
        img.Pix[i]   = uint8(float64(r)*(1-amount) + float64(lum)*amount)
        img.Pix[i+1] = uint8(float64(g)*(1-amount) + float64(lum)*amount)
        img.Pix[i+2] = uint8(float64(b)*(1-amount) + float64(lum)*amount)
    }
}
```

### Edge cases to defer
- `backdrop-filter` (requires reading from background behind element)
- SVG filter references (`filter: url(#id)`)
- `drop-shadow()` filter (different from box-shadow)

### Estimated scope
~150 lines: 40 for parsing, 30 for offscreen pipeline, 80 for individual
filter implementations.

---

## Summary

| # | Feature | Web % | Difficulty | Lines (est.) | Depends On |
|---|---------|-------|-----------|-------------|------------|
| 1 | Text transform | 68% | Easy | ~30 | Nothing |
| 2 | Outline | 72% | Easy | ~50 | Border radius (done) |
| 3 | Object-fit/position | 48% | Easy-Medium | ~60 | Nothing |
| 4 | Text shadow | 43% | Medium | ~70 | boxBlur (done) |
| 5 | List markers | 78% | Medium | ~80 | Nothing |
| 6 | CSS filters | 38% | Medium-Hard | ~150 | boxBlur (done) |

**Total estimated new code: ~440 lines**

Items 1, 2, 3, and 5 are independent and easy — good parallel candidates.
Item 4 leverages existing blur infrastructure.
Item 6 is the most complex but reuses offscreen + blur patterns from box-shadow.

### Suggested execution order

**Batch 1 (parallel, easy):**
- Agent A: Text transform (#1)
- Agent B: Outline (#2)
- Agent C: Object-fit (#3)
- Agent D: List markers (#5)

**Batch 2 (parallel, medium):**
- Agent A: Text shadow (#4)
- Agent B: CSS filters (#6)
