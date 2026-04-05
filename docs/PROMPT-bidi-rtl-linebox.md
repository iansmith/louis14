# Bidi RTL Line Box Regression Fix — Continuation Prompt

Read `.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` first for project principles.

## Current baseline: 536/788 WM passing, 46/82 bidi passing

## The problem: 8 bidi regressions in horizontal RTL contexts

These 8 tests fail because the test div renders text in logical order instead of bidi-reordered order. Example: `bidi-normal-003` shows `> א > a >` in box 1 instead of `< a < א <`.

### Failing tests
```
bidi-isolate-003.html      bidi-normal-003.html
bidi-isolate-010.html      bidi-normal-004.html
bidi-isolate-override-010.html  bidi-plaintext-010.html
bidi-unset-003.html        bidi-unset-004.html
```

All share this pattern:
- Test div has `dir="rtl"` (or inherits RTL) with `unicode-bidi: normal/unset`
- Text has mixed LTR/RTL characters (e.g. `> א > a >`)
- No bidi control characters in the TEST div text
- Reference div uses literal LRO/PDF characters (bidi controls present)

### What happens now

The full bidi pipeline runs for ALL text:
1. `ResolveBidiLevels` — pure-Go UAX#9 resolver, computes correct per-rune levels
2. `StripBidiControls` — removes formatting chars
3. `SplitItemsAtLevelBoundaries` — splits text items at level changes

For the test div text `> א > a >` at RTL base level 1:
- Levels: [1,1,1,1,1,1,2,1,1] (neutrals→R at level 1, `a`→L at level 2)
- Split into: `"> א > "` (level 1), `"a"` (level 2), `" >"` (level 1)
- L2 reordering reverses level≥1 items: `[" >", "a", "> א > "]`
- Each level-1 fragment gets `reverseAndMirrorRunes` → `>` becomes `<`

For the reference div text `LRO< a < א <PDF`:
- LRO forces all to type L at level 2
- After stripping LRO/PDF: `< a < א <` all at level 2
- No split needed (uniform level), no reversal (even level)
- Renders correctly as `< a < א <`

### The root cause

After L2 reordering, `createLineBox` places items sequentially. For **horizontal** writing modes, the line box direction is forced to LTR (line 313 of `inline_layout.go`) because L2 already arranged items in visual left-to-right order. This works correctly.

For **vertical** writing modes, the line box keeps its original direction. This was necessary because forcing LTR for vertical RTL would place items top-to-bottom, but RTL inline-start is at the bottom. The RTL line box correctly places items from the bottom.

**However**, the RTL line box also FLIPS inline positions: `physicalY = inlineSize - logicalInlinePos - childSize`. This flip interacts with L2 in a way that effectively double-reverses the items, producing logical order instead of visual order.

The specific interaction:
1. L2 reverses RTL items to visual order (first visual item first in the array)
2. Items are placed at increasing inline positions: 0, 80, 160, ...
3. The RTL-to-physical conversion flips these positions
4. Result: first visual item ends up at the BOTTOM (inline-start for RTL) — correct position
5. BUT the flip also reverses the order of items relative to each other

### What Blink does (study this)

In Blink's `InlineLayoutAlgorithm::CreateLine()`:

1. **Line box direction**: Blink uses `LineInfo::BaseDirection()` for the line, which may differ from the paragraph direction. Look at `NGInlineLayoutAlgorithm::CreateLine` in:
   - `third_party/blink/renderer/core/layout/ng/inline/ng_inline_layout_algorithm.cc`

2. **BidiReorder**: Blink calls `NGLineBreaker::BidiReorder()` which calls `ReorderLine()`. The reordered items are in VISUAL order. Then `PlaceItems()` positions them.

3. **Key question**: Does Blink force LTR for the line box in vertical modes? Or does it handle the coordinate conversion differently?
   - Look at `NGLogicalLineItems::CreateLine()` and how it builds the `NGPhysicalLineBoxFragment`
   - Check `NGWritingModeConverter` usage during line box fragment creation

4. **Physical offset conversion**: In Blink, `NGLogicalOffset` → `NGPhysicalOffset` conversion uses the writing mode. Look at:
   - `NGWritingModeConverter::ToPhysical(LogicalOffset, PhysicalSize)` in `writing_mode_utils.h`
   - How this differs when the line box has LTR vs RTL direction

5. **Text alignment in RTL**: Look at how `NGLineInfo::ComputeWidth()` and `text-align: start` interact with RTL in vertical modes. The `alignOffset` in our `computeTextAlignOffset` uses the original `wdm` (RTL), not `lineWDM`.

### Specific things to investigate

1. **Does Blink force the line box to LTR for ALL writing modes?** If yes, then the issue is in our `computeTextAlignOffset` or L2 algorithm needing adjustment for vertical modes.

2. **Does Blink reverse L2 results for vertical RTL?** Maybe L2 produces different ordering for vertical vs horizontal.

3. **Is the physical offset conversion different?** Maybe Blink's line box fragment creation handles RTL differently for vertical modes by NOT flipping inline positions for L2-reordered content.

4. **Try this experiment**: For vertical RTL, reverse the L2 result BEFORE placing items. This would undo the RTL flip, giving the correct final order. Add this after `ReorderLineVisual(line.Results)`:
   ```go
   if wdm.IsVertical() && wdm.IsRTL() {
       // Reverse L2 result for vertical RTL — the RTL line box flip
       // will re-reverse it to the correct visual order.
       for i, j := 0, len(line.Results)-1; i < j; i, j = i+1, j-1 {
           line.Results[i], line.Results[j] = line.Results[j], line.Results[i]
       }
   }
   ```
   This is a hack but would confirm the theory.

5. **The cleaner fix**: Study how `computeTextAlignOffset` should work with L2 reordering. Maybe for RTL after L2, the alignment should be 0 (not slack), because L2 already accounts for the RTL direction.

### Key files

- `pkg/layout/inline_layout.go:247-253` — L2 reordering + createLineBox call
- `pkg/layout/inline_layout.go:313-324` — lineWDM computation (LTR force for horizontal only)
- `pkg/layout/inline_layout.go:311` — computeTextAlignOffset uses wdm (not lineWDM)
- `pkg/layout/inline_layout.go:432` — inlinePos starts at alignOffset
- `pkg/layout/bidi.go:599-639` — ReorderLineVisual (L2 algorithm)
- `pkg/layout/engine.go:286-288` — reverseAndMirrorRunes for odd-level fragments

### Test commands

```bash
# The 8 regressions
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi-normal-003' ./pkg/visualtest/ -timeout 30s

# Full bidi suite (current: 46/82)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 120s

# Full WM suite (current: 536/788) — MUST NOT REGRESS
go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS: TestWPT'

# Quick regression check
go test ./pkg/layout/ ./pkg/css/ ./pkg/render/ -timeout 60s
```

### Debugging approach

Add `layout.SetDebugLayout(true)` (defined in engine.go but currently removed — re-add as needed) to trace fragment positions. The key thing to look for: do the text fragment Y coordinates place items in visual order (after L2) or logical order?

With the lineWDM forced to LTR for ALL modes, the abs-pos tests pass but these 8 bidi tests show un-reordered text. With lineWDM keeping RTL for vertical modes, the abs-pos tests pass and these 8 show the same un-reordered text. The issue is specifically in how L2 visual ordering maps to physical coordinates in vertical RTL.
