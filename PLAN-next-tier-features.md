# Next-Tier CSS Feature Implementation Plan

Six features that are parsed but produce no visual output. Selected for
high web prevalence and feasibility, avoiding features already handled
in layout (white-space, text-align, text-indent are already implemented).

---

## 1. Word Spacing

**Web prevalence:** 35% of pages (inherited, so applied broadly)
**Difficulty:** Easy
**Why it matters:** `word-spacing` adjusts the gap between words. It's
inherited by default and affects readability. Currently parsed in CSS
but never applied during text measurement or rendering.

### What exists
- CSS parsing: `GetWordSpacing()` returns `float64` (pixels, default 0)
- `LetterSpacing` is already implemented in PaintLayer and drawText()
  with per-character advance — word-spacing follows the same pattern
- Not used in layout or render

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
WordSpacing float64
```

Populate from `s.GetWordSpacing()`.

**Step 2 — Apply in drawText()** (`pkg/render/render.go`)

In the letter-spacing path (and potentially the normal path), when
iterating characters, add extra space after space characters:

For the normal (no letter-spacing) path, if word-spacing != 0, switch
to a character-by-character draw that adds WordSpacing after spaces:

```go
if layer.WordSpacing != 0 {
    x := box.X
    baselineY := box.Y + ascent
    for i, ch := range text {
        r.dc.DrawText(string(ch), fontID, x, baselineY)
        charW := r.dc.MeasureText(string(ch), fontID)
        x += charW
        if ch == ' ' {
            x += layer.WordSpacing
        }
    }
} else if layer.LetterSpacing != 0 {
    // existing letter-spacing code
}
```

For the combined letter-spacing + word-spacing case, add both.

**Step 3 — Apply in layout** (`pkg/layout/line_breaker.go`)

Word spacing must also be applied during text measurement for correct
line breaking. In the line breaker, when measuring word widths, add
`WordSpacing` to each space character's advance. Check how letter-spacing
is handled in the line breaker and mirror that approach.

### Estimated scope
~40 lines across 3 files.

---

## 2. Text Overflow: Ellipsis

**Web prevalence:** 52% of pages
**Difficulty:** Medium
**Why it matters:** `text-overflow: ellipsis` truncates overflowing text
with "..." instead of just clipping. Without it, card titles, nav items,
and truncated labels just abruptly cut off, which looks broken. This is
one of the most visually jarring missing features on real websites.

### What exists
- CSS parsing: `GetTextOverflow()` returns `TextOverflowType` (clip/ellipsis)
- Overflow clipping already works (overflow:hidden clips content)
- Not used in layout or render

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
TextOverflow string // "clip" or "ellipsis"
```

Populate from `s.GetTextOverflow()`.

**Step 2 — Detect overflow + draw ellipsis** (`pkg/render/render.go`)

In `drawText()`, when the text box has overflow:hidden and text-overflow
is "ellipsis", check if text exceeds the available width and truncate:

```go
if layer.TextOverflow == "ellipsis" && layer.HasClip {
    availW := layer.ClipRect[2] // clip width
    textW := r.dc.MeasureText(text, fontID)
    if textW > availW - (box.X - layer.ClipRect[0]) {
        ellipsis := "…"
        ellipsisW := r.dc.MeasureText(ellipsis, fontID)
        // Binary search or linear scan for truncation point
        truncW := availW - (box.X - layer.ClipRect[0]) - ellipsisW
        truncated := ""
        w := 0.0
        for _, ch := range text {
            cw := r.dc.MeasureText(string(ch), fontID)
            if w + cw > truncW { break }
            truncated += string(ch)
            w += cw
        }
        text = truncated + ellipsis
    }
}
```

This should be applied early in drawText(), after text-transform but
before drawing.

**Step 3 — Handle in PaintLayer population**

Need to also propagate the parent's clip rect info to text nodes.
Text runs inherit from their parent's overflow clip. Check how HasClip
is propagated from parent layers to text content.

### Edge cases
- Only applies when overflow:hidden + white-space:nowrap (typically)
- Multi-line ellipsis (line-clamp) — defer
- RTL text — defer

### Estimated scope
~50 lines across 2 files.

---

## 3. Background-Clip (padding-box, content-box)

**Web prevalence:** 42% of pages
**Difficulty:** Easy
**Why it matters:** `background-clip: padding-box` prevents backgrounds
from showing under borders (important for semi-transparent borders).
`background-clip: content-box` restricts backgrounds to the content area
only. The default `border-box` is current behavior.

### What exists
- CSS parsing: `GetBackgroundClip()` returns `BackgroundClipType`
  (border-box/padding-box/content-box)
- Background drawing in `drawBackground()` uses the full border-box
- Not used in render

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
BackgroundClip css.BackgroundClipType
```

Populate from `s.GetBackgroundClip()`.

**Step 2 — Clip background to appropriate box** (`pkg/render/render.go`)

In `drawBackground()`, compute the clip area based on background-clip
instead of always using the border-box:

```go
func (r *Renderer) backgroundClipBox(layer *PaintLayer) (float64, float64, float64, float64) {
    box := layer.Box
    switch layer.BackgroundClip {
    case css.BackgroundClipPaddingBox:
        return pixelSnap(
            box.X+box.Border.Left, box.Y+box.Border.Top,
            box.Width-box.Border.Left-box.Border.Right,
            box.Height-box.Border.Top-box.Border.Bottom)
    case css.BackgroundClipContentBox:
        return pixelSnap(
            box.X+box.Border.Left+box.Padding.Left,
            box.Y+box.Border.Top+box.Padding.Top,
            box.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right,
            box.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom)
    default: // border-box
        return pixelSnap(box.X, box.Y, box.Width, box.Height)
    }
}
```

Use this in `drawBackground()` instead of `pixelSnap(box.X, box.Y, box.Width, box.Height)`.

### Estimated scope
~30 lines across 2 files.

---

## 4. Clip-Path (circle, ellipse, polygon)

**Web prevalence:** 28% of pages
**Difficulty:** Medium
**Why it matters:** `clip-path` creates non-rectangular visible areas —
circular avatars, diagonal sections, custom shapes. Very common in
modern web design for image masking and decorative layouts.

### What exists
- CSS parsing: `GetClipPath()` returns `*ClipPath` with Type, Radius,
  Rx/Ry, Cx/Cy, Points for circle/ellipse/polygon
- DrawContext has `DrawCircle()`, `MoveTo/LineTo/ClosePath`, `Clip()`,
  `Push()/Pop()`
- Not used in render

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
ClipPath *css.ClipPath // nil = no clip-path
```

Populate from `s.GetClipPath()`.

**Step 2 — Apply clip-path in paintLayerContent()** (`pkg/render/render.go`)

At the very start of `paintLayerContent()`, before any drawing, apply
the clip-path:

```go
clipPathActive := false
if layer.ClipPath != nil {
    r.dc.Push()
    clipPathActive = true
    r.applyClipPath(layer)
}
// ... existing paint code ...
if clipPathActive {
    r.dc.Pop()
}
```

**Step 3 — Implement applyClipPath()**

```go
func (r *Renderer) applyClipPath(layer *PaintLayer) {
    box := layer.Box
    cp := layer.ClipPath
    cx := box.X + box.Width/2  // default center
    cy := box.Y + box.Height/2
    
    switch cp.Type {
    case css.ClipPathCircle:
        radius := cp.Radius
        if radius < 0 {
            // closest-side default
            radius = math.Min(box.Width, box.Height) / 2
        }
        r.dc.DrawCircle(cx, cy, radius)
    case css.ClipPathEllipse:
        rx, ry := cp.Rx, cp.Ry
        if rx < 0 { rx = box.Width / 2 }
        if ry < 0 { ry = box.Height / 2 }
        // Approximate ellipse with cubic beziers
        r.drawEllipsePath(cx, cy, rx, ry)
    case css.ClipPathPolygon:
        pts := cp.Points
        for i := 0; i < len(pts)-1; i += 2 {
            px := box.X + pts[i]*box.Width
            py := box.Y + pts[i+1]*box.Height
            if i == 0 {
                r.dc.MoveTo(px, py)
            } else {
                r.dc.LineTo(px, py)
            }
        }
        r.dc.ClosePath()
    }
    r.dc.Clip()
}
```

### Estimated scope
~60 lines across 2 files.

---

## 5. Mix-Blend-Mode

**Web prevalence:** 22% of pages (growing)
**Difficulty:** Medium
**Why it matters:** `mix-blend-mode` controls how an element composites
with its backdrop — multiply, screen, overlay, darken, lighten,
difference. Used for image overlays, text effects, and decorative layers.

### What exists
- CSS parsing: `GetMixBlendMode()` returns `MixBlendMode` (normal,
  multiply, screen, overlay, darken, lighten, difference)
- DrawContext has `PushGroup()`/`PopGroupWithAlpha()` for offscreen
  compositing and `NewChildContext()` for offscreen buffers
- Filter implementation already does offscreen render + pixel manipulation
- Not used in render

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
BlendMode css.MixBlendMode // normal, multiply, screen, etc.
```

Populate from `s.GetMixBlendMode()`.

**Step 2 — Apply in paintLayer()** (`pkg/render/render.go`)

When blend mode is not "normal", render the element to an offscreen
buffer, then composite using the blend function:

```go
if layer.BlendMode != css.MixBlendModeNormal && layer.BlendMode != "" {
    r.paintLayerWithBlend(layer)
    return
}
```

**Step 3 — Implement blend compositing**

```go
func (r *Renderer) paintLayerWithBlend(layer *PaintLayer) {
    box := layer.Box
    bx := int(math.Floor(box.X))
    by := int(math.Floor(box.Y))
    bw := int(math.Ceil(box.Width)) + 1
    bh := int(math.Ceil(box.Height)) + 1
    if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
        r.paintLayerContent(layer)
        return
    }
    
    // Render element to offscreen buffer.
    buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
    origDC := r.dc
    origTarget := r.target
    childDC := origDC.NewChildContext(buf)
    r.dc = childDC
    r.target = buf
    r.dc.Translate(float64(-bx), float64(-by))
    r.paintLayerContent(layer)
    r.dc = origDC
    r.target = origTarget
    
    // Composite with blend mode.
    blendComposite(r.target, buf, bx, by, layer.BlendMode)
}
```

**Step 4 — Blend functions** (pixel-level)

```go
func blendComposite(dst *image.RGBA, src *image.RGBA, ox, oy int, mode css.MixBlendMode) {
    bounds := src.Bounds()
    for sy := 0; sy < bounds.Dy(); sy++ {
        dy := sy + oy
        if dy < 0 || dy >= dst.Bounds().Dy() { continue }
        for sx := 0; sx < bounds.Dx(); sx++ {
            dx := sx + ox
            if dx < 0 || dx >= dst.Bounds().Dx() { continue }
            
            srcOff := sy*src.Stride + sx*4
            dstOff := dy*dst.Stride + dx*4
            sa := float64(src.Pix[srcOff+3]) / 255
            if sa == 0 { continue }
            
            sr := float64(src.Pix[srcOff]) / 255
            sg := float64(src.Pix[srcOff+1]) / 255
            sb := float64(src.Pix[srcOff+2]) / 255
            dr := float64(dst.Pix[dstOff]) / 255
            dg := float64(dst.Pix[dstOff+1]) / 255
            db := float64(dst.Pix[dstOff+2]) / 255
            
            var rr, rg, rb float64
            switch mode {
            case css.MixBlendModeMultiply:
                rr, rg, rb = sr*dr, sg*dg, sb*db
            case css.MixBlendModeScreen:
                rr = sr + dr - sr*dr
                rg = sg + dg - sg*dg
                rb = sb + db - sb*db
            case css.MixBlendModeOverlay:
                rr = overlayBlend(dr, sr)
                rg = overlayBlend(dg, sg)
                rb = overlayBlend(db, sb)
            case css.MixBlendModeDarken:
                rr, rg, rb = math.Min(sr, dr), math.Min(sg, dg), math.Min(sb, db)
            case css.MixBlendModeLighten:
                rr, rg, rb = math.Max(sr, dr), math.Max(sg, dg), math.Max(sb, db)
            case css.MixBlendModeDifference:
                rr = math.Abs(sr - dr)
                rg = math.Abs(sg - dg)
                rb = math.Abs(sb - db)
            default:
                rr, rg, rb = sr, sg, sb
            }
            
            // Alpha compositing: blend result over destination
            dst.Pix[dstOff]   = uint8((rr*sa + dr*(1-sa)) * 255)
            dst.Pix[dstOff+1] = uint8((rg*sa + dg*(1-sa)) * 255)
            dst.Pix[dstOff+2] = uint8((rb*sa + db*(1-sa)) * 255)
            da := float64(dst.Pix[dstOff+3]) / 255
            dst.Pix[dstOff+3] = uint8((sa + da*(1-sa)) * 255)
        }
    }
}

func overlayBlend(backdrop, source float64) float64 {
    if backdrop < 0.5 {
        return 2 * backdrop * source
    }
    return 1 - 2*(1-backdrop)*(1-source)
}
```

### Estimated scope
~120 lines across 2 files.

---

## 6. Text Decoration Style (wavy, dotted, dashed, double)

**Web prevalence:** 31% of pages use text-decoration; style variants on ~15%
**Difficulty:** Easy-Medium
**Why it matters:** `text-decoration-style: wavy` is the standard way to
indicate spelling errors (red wavy underline). `dotted` and `dashed`
are used for abbreviation underlines and other semantic decoration.
Currently all text decorations render as solid lines.

### What exists
- CSS parsing: `GetTextDecorationStyle()` returns string (solid/double/
  dotted/dashed/wavy)
- Text decoration already renders solid lines via `drawTextDecoration()`
- Border style helpers already exist: `drawDashedLine()`, `drawDottedLine()`,
  `drawDoubleLine()`
- Not used in render (style is parsed but ignored)

### Implementation

**Step 1 — Add to PaintLayer** (`pkg/render/paint_layer.go`)

```go
TextDecorationStyle string // solid, double, dotted, dashed, wavy
```

Populate from `s.GetTextDecorationStyle()`.

**Step 2 — Modify drawTextDecoration()** (`pkg/render/render.go`)

Currently `drawTextDecoration()` draws a single solid line. Extend it:

```go
switch layer.TextDecorationStyle {
case "dashed":
    r.drawDashedLine(box.X, lineY, box.X+textWidth, lineY, thickness)
case "dotted":
    r.drawDottedLine(box.X, lineY, box.X+textWidth, lineY, thickness)
case "double":
    r.drawDoubleLine(box.X, lineY, box.X+textWidth, lineY, thickness)
case "wavy":
    r.drawWavyLine(box.X, lineY, textWidth, thickness)
default: // "solid"
    r.dc.MoveTo(box.X, lineY)
    r.dc.LineTo(box.X+textWidth, lineY)
    r.dc.Stroke()
}
```

**Step 3 — Implement wavy line**

```go
func (r *Renderer) drawWavyLine(x, y, width, thickness float64) {
    amplitude := thickness * 1.5
    wavelength := thickness * 4
    r.dc.SetLineWidth(thickness)
    r.dc.MoveTo(x, y)
    for cx := x; cx < x+width; cx += wavelength/2 {
        // Alternating up and down arcs
        midX := cx + wavelength/4
        endX := math.Min(cx+wavelength/2, x+width)
        if int((cx-x)/(wavelength/2))%2 == 0 {
            r.dc.QuadraticTo(midX, y-amplitude, endX, y)
        } else {
            r.dc.QuadraticTo(midX, y+amplitude, endX, y)
        }
    }
    r.dc.Stroke()
}
```

### Estimated scope
~50 lines across 2 files.

---

## Summary

| # | Feature | Web % | Difficulty | Lines (est.) | Depends On |
|---|---------|-------|-----------|-------------|------------|
| 1 | Word spacing | 35% | Easy | ~40 | Nothing |
| 2 | Text overflow: ellipsis | 52% | Medium | ~50 | Nothing |
| 3 | Background-clip | 42% | Easy | ~30 | Nothing |
| 4 | Clip-path | 28% | Medium | ~60 | Nothing |
| 5 | Mix-blend-mode | 22% | Medium | ~120 | Filter infra (done) |
| 6 | Text decoration style | 31% | Easy-Medium | ~50 | Text decoration (done) |

**Total estimated new code: ~350 lines**

Items 1, 3, and 6 are easy and independent — good parallel candidates.
Items 2 and 4 are medium and independent.
Item 5 reuses the offscreen rendering pattern from filters.

### Suggested execution order

**Batch 1 (parallel, easy):**
- Agent A: Word spacing (#1)
- Agent B: Background-clip (#3)
- Agent C: Text decoration style (#6)

**Batch 2 (parallel, medium):**
- Agent A: Text overflow: ellipsis (#2)
- Agent B: Clip-path (#4)
- Agent C: Mix-blend-mode (#5)
