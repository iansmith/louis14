# Gap 3: Sideways Text Rendering Plan

## What is broken

`writing-mode: sideways-rl` and `writing-mode: sideways-lr` exist to allow
horizontal-origin text to be placed in a vertical container. The glyphs are
not upright (like CJK ideographs in `vertical-rl`); instead the **entire line
of text is physically rotated 90°**. You can read the characters normally by
tilting your head.

Current code in `pkg/render/render.go:520-555` treats sideways text the same
as upright vertical text: it stacks individual characters vertically.  That
produces unreadable output and fails every WPT pixel-comparison test.

### Correct visual output (from WPT reference files)

```
sideways-rl  – text rotated 90° clockwise:   top of fragment = start of string
sideways-lr  – text rotated 90° CCW:         top of fragment = end of string
                                              (reads bottom-to-top)
```

Both modes place a **horizontal line of text** inside the physical box for
that text fragment.  The box is already sized correctly by the layout engine
(see "Layout is already correct" below).

---

## Why layout is already correct

CSS logical coordinates make the layout math mode-agnostic.  For any vertical
writing mode (including sideways):

```
InlineSize  = text advance width  (e.g. 60 px for "ABC" at 20 px/glyph)
BlockSize   = line height         (e.g. 20 px for a 20 px font)
```

`ToPhysicalSize()` swaps axes for vertical modes:

```
PhysicalWidth  = BlockSize   (= line height ≈ 20 px)
PhysicalHeight = InlineSize  (= advance ≈ 60 px)
```

So `box.Width` is the line height and `box.Height` is the text advance.  This
is **exactly** the physical bounding box that a rotated horizontal text string
occupies.  No layout changes are required for Gap 3.

---

## Rendering strategy: context rotation

Instead of drawing characters one-by-one at stacked Y positions, we:

1. **Save** the graphics state (`dc.Push()`).
2. **Rotate** the coordinate frame 90° around the box centre.
3. **Draw** the entire string as normal horizontal text in the rotated frame.
4. **Restore** the graphics state (`dc.Pop()`).

The key arithmetic (box centre = `(cx, cy)`):

```
cx = box.X + box.Width/2
cy = box.Y + box.Height/2
```

In the rotated frame the "horizontal box" has:

```
hW = box.Height   (physical height becomes horizontal width)
hH = box.Width    (physical width  becomes horizontal height)

hboxX = cx - hW/2 = box.X + (box.Width - box.Height) / 2
hboxY = cy - hH/2 = box.Y + (box.Height - box.Width) / 2
```

The baseline position in the horizontal box is `hboxY + ascent`.

### sideways-rl (rotate +π/2, i.e. 90° clockwise on screen)

```
RotateAbout(+π/2, cx, cy)
DrawText(text, fontID, hboxX, hboxY + ascent)
```

After +π/2 rotation: rightward in the rotated frame → downward in physical
space.  So the string advances top-to-bottom, which is the correct inline
direction for `sideways-rl`.

### sideways-lr (rotate −π/2, i.e. 90° counter-clockwise on screen)

```
RotateAbout(-π/2, cx, cy)
DrawText(text, fontID, hboxX, hboxY + ascent)
```

After −π/2 rotation: rightward in the rotated frame → upward in physical
space.  So the string advances bottom-to-top, which is the correct inline
direction for `sideways-lr`.

---

## File changes

### `pkg/render/render.go` – lines 520-555 (the only real change)

Replace the character-stacking block with the rotation approach:

```go
if layer.IsSidewaysRL || layer.IsSidewaysLR {
    cx := box.X + box.Width/2
    cy := box.Y + box.Height/2

    // In the rotated horizontal frame the box dimensions are swapped:
    //   horizontal width  = box.Height (text advance direction)
    //   horizontal height = box.Width  (line height)
    hboxX := cx - box.Height/2
    hboxY := cy - box.Width/2

    r.dc.Push()
    if layer.IsSidewaysRL {
        r.dc.RotateAbout(math.Pi/2, cx, cy)
    } else {
        r.dc.RotateAbout(-math.Pi/2, cx, cy)
    }
    r.dc.DrawText(box.Text, fontID, hboxX, hboxY+ascent)
    r.dc.Pop()
    return
}
```

Add `"math"` to the import block if it is not already present.

### No other files need changing

- `pkg/layout/` — no changes (box sizes are correct).
- `pkg/layout/engine.go` — no changes (no rune reversal needed; text is
  already in visual order because the text advances in the rotated direction).
- `pkg/render/paint_layer.go` — no changes (`IsSidewaysRL`/`IsSidewaysLR`
  already set).
- `pkg/text/` — no changes.
- CSS parsing — no changes (writing-mode values already parsed).

---

## DrawContext API used

From `mazarin/textshape` `DrawContext` interface
(`/Users/iansmith/mazzy/mazarin/textshape/draw_context.go`):

```go
Push()                                   // save graphics state
Pop()                                    // restore graphics state
RotateAbout(angle, x, y float64)         // rotate around (x,y) in radians
DrawText(text string, fontID int32, x, y float64)  // draw at baseline (x,y)
GetFontMetrics(fontID int32) FontMetrics // ascent via metrics.Ascent/64.0
```

All four methods are present on the existing `r.dc` field – no interface
changes required.

---

## Letter-spacing

The current stacking code threads `layer.LetterSpacing` through the per-glyph
loop.  `DrawText` draws a full run so letter-spacing is applied by the shaper
internally for horizontal text.  Verify: does the existing normal-text path
pass `LetterSpacing` to the shaper, or is it applied manually?

Check `render.go:580-600` (the normal horizontal path).  If letter-spacing is
already handled by the shaper for the normal case, no extra work is needed.
If it is applied manually in the horizontal path too, you may need to set a
letter-spacing property on the draw context before the rotated draw call.
Defer this to the implementation; the WPT sideways tests don't use
letter-spacing.

---

## Test impact

49 failing tests across five directories:

| Category | Count | Example file |
|---|---|---|
| `block-flow-direction-slr` | ~10 | `block-flow-direction-045.xht` |
| `block-flow-direction-srl` | ~10 | `block-flow-direction-046.xht` |
| `line-box-direction-slr/srl` | ~8 | `line-box-direction-slr-002.xht` |
| `row-progression-slr/srl` | ~8 | `row-progression-slr-001.xht` |
| `inline-block-alignment-slr/srl` | ~8 | `inline-block-alignment-slr-001.xht` |
| `sideways-lr-main-axis` | 1 | `sideways-lr-main-axis.html` |

All 49 share the same root cause (character stacking vs. context rotation).
Fixing `render.go:520-555` should resolve all of them in a single commit.

---

## Verification steps

```bash
# Run the WPT writing-modes visual tests with the new rendering:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run TestWPT/css-writing-modes \
  -v 2>&1 | grep -E "PASS|FAIL|sideways"
```

Before the fix, expect ~49 failures in `sideways` tests.
After the fix, expect 0 failures in those tests (and no regressions in the
upright vertical tests that share the `IsVerticalText` path).

---

## Implementation order

1. Edit `pkg/render/render.go` lines 520-555 as above.
2. Add `"math"` import if needed.
3. Run the full writing-modes test suite, check counts.
4. If letter-spacing is needed, add it; otherwise commit.

This is a **single-file, ~10 line change** that should take one short session.
