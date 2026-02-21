# Plan: DrawString Kerning Consistency

## The Problem

Our engine has 344 subtly different pixels in the `color-applies-to-001/002` tests
(0.07%, within the 0.1% threshold). The cause is a mismatch in how freetype applies
kerning depending on whether text is rendered as one `DrawString` call or two.

### Why it happens

The test renders two table cells on the same visual line:
```
Cell 1: DrawString("Filler", x=0, y)
Cell 2: DrawString(" Text", x=34, y)
```

The reference renders a plain div:
```
DrawString("Filler Text", x=0, y)
```

Both show the same visual text at the same position. But freetype's internal glyph
positioning uses **kerning pairs** — adjustments between adjacent character pairs
like "r " that make the spacing look more natural. This means:

```
MeasureString("Filler")   = 34px   (correct, used for cell 2's X position)
MeasureString(" ")        = 4px    (isolated space advance)
MeasureString("Filler ")  = 39px   (includes "r "+" " kerning pair = 5px, not 4px)
```

So "T" in the reference lands at **x=39** (inside "Filler Text" with kerning context),
while "T" in the test lands at **x=38** (fresh DrawString starting at 34, space=4px).
That 1px difference in "T"'s fractional sub-pixel position causes slightly different
anti-aliasing coverage on the "T" glyph — 344 pixels worth.

### Real browsers don't have this problem

Real browsers (Blink/WebKit) use text-shaping libraries (HarfBuzz, CoreText) that
shape an entire **text run** at the line level. Adjacent inline boxes ("Filler" and
" Text" in adjacent cells) are shaped as one run before drawing, so the kerning
between "r" and " " is applied correctly. The result is glyph positions that are
independent of box boundaries.

---

## Options (Simplest to Most Correct)

### Option 1: Accept the 0.07% diff (do nothing)

The tests already pass. The difference is invisible to the human eye. Move on.

**Pro:** Zero work.
**Con:** Non-zero diffs remain. MEMORY.md goal is "most tests at 0px diff."

---

### Option 2: Per-character rendering within each box

Instead of `DrawString(text, x, y)` for a whole word, measure cumulative glyph
positions from the full word string and draw each glyph individually at an integer X:

```go
// For each box in drawText:
runes := []rune(textContent)
for i, ch := range runes {
    prefix := string(runes[:i+1])
    glyphX := math.Round(textX + MeasureString(string(runes[:i])))
    DrawString(string(ch), glyphX, textY)
}
```

Each glyph starts at an integer X derived from the full word's kerning context.
Cross-box kerning ("r " → " T") is still not captured, but each box is internally
consistent. A box containing "Filler Text" renders identically to two boxes
containing "Filler" and " Text" when both start at integer X.

**BUT**: as shown above, "T"'s position still differs by 1px (38 vs 39) because
the "r" in "Filler" doesn't kern with the " " in the adjacent box. This option
does NOT fix the color-applies-to diff.

**Cost:** N cumulative MeasureString calls per word (O(N²) per line character count).
**Pro:** Makes within-box rendering fully consistent, may help other tests.
**Con:** Does not fix color-applies-to because cross-box kerning is still absent.

---

### Option 3: Disable kerning

Configure freetype to use zero kerning. Then MeasureString("Filler ") =
MeasureString("Filler") + MeasureString(" ") = 34 + 4 = 38, and "T" lands at
x=38 in both test and reference.

**Problem:** The gg library does not expose a "disable kerning" option. This would
require forking gg or using freetype's lower-level API directly. Also, disabling
kerning makes all text look slightly worse — letters like "VA", "AW", "Te" would
have visibly wrong spacing.

**Cost:** Fork gg or drop to raw freetype API. Significant dependency change.
**Con:** Degrades text quality for all rendered pages.

---

### Option 4: Line-level text run rendering

This is how real browsers work. The fix:

1. **During the render phase**, group adjacent text boxes on the same line that
   share the same font (same size, bold, italic, family) into a **text run**.
   "Adjacent" means box2.X ≈ box1.X + box1.Width (within 1px).

2. **Concatenate** the text of all boxes in the run: "Filler" + " Text" = "Filler Text".

3. **Measure cumulative glyph positions** within the full run string:
   ```
   pos[i] = math.Round(textRunStartX + MeasureString(fullRun[:i]))
   ```
   This gives "T" its correct position of `round(0 + MeasureString("Filler ")) = 39`.

4. **Draw each glyph** individually: `DrawString(glyph[i], pos[i], y)`.
   Each is at an integer X, starting fresh — consistent anti-aliasing.

5. **Apply color changes** within the run by calling `SetRGBA` between glyphs
   when transitioning from one box's color to another.

**Result:** "Filler Text" as one run → "T" at x=39 in BOTH test and reference.
Exact pixel match. This is the principled fix.

**Cost:**
- Need a `groupTextRuns()` pass in the render phase (new function ~80 lines)
- `drawText()` needs to be replaced with `drawTextRun()` for grouped boxes
- Color changes within a run need per-glyph `SetRGBA` calls
- O(N) MeasureString calls per run (N = character count)
- Need to handle text-decoration, letter-spacing, text-shadow per run (complex)
- Need to handle right-to-left text correctly (future concern)

**Estimated scope:** ~150 lines of new render code, ~50 lines of modifications.

---

## Recommendation

**Option 4 is the right fix** but carries significant complexity. The risk is
introducing regressions in text-decoration, letter-spacing, and shadow rendering
which currently work at the box level.

**Practical path:**

1. **Short term:** Leave color-applies-to at 0.07% (it passes). Record this as a
   known architectural limitation.

2. **If pursuing Option 4:** Implement it behind a flag first, run full test suite,
   confirm color-applies-to → 0px, then enable by default.

The correct long-term architecture for a production browser engine is line-level
text run shaping (Option 4). But for the current test suite where 0.07% already
passes, the cost/benefit is low.

---

## Files That Would Change (Option 4)

| File | Change |
|---|---|
| `pkg/render/render.go` | Add `groupTextRuns()`, replace `drawText()` with `drawTextRun()` for grouped boxes |
| `pkg/layout/types.go` | Optional: add `RunID` field to Box for grouping hints |

No layout engine changes needed — the grouping is purely a render-phase concern.

## Verification

```bash
# Before: color-applies-to shows 344px
go test ./pkg/visualtest/... -run "color-applies-to" -v

# After Option 4: should show 0px
go test ./pkg/visualtest/... -run "color-applies-to" -v

# Full suite: confirm no regressions
go clean -testcache && go test ./pkg/visualtest/... 2>&1 | tail -3
```
