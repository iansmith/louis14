# Findings & Decisions — CSS2 reftests remaining 3

## Requirements
- Fix 3 CSS2 reftest failures to 0% diff:
  1. `pkg/visualtest/testdata/wpt-css2/floats/float-no-content-beside-001.html` — currently 17244 px diff (3.6%)
  2. `pkg/visualtest/testdata/wpt-css2/linebox/inline-box-002.xht` — 17244 px (3.6%)
  3. `pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht` — 111939 px (23.3%)
- Current CSS2 status: 96/99 passing
- Follow CLAUDE.md: study Blink before coding; 0% diff; no easy-win point fixes

## Research Findings

### Test #1: float-no-content-beside-001
Test HTML (3 variations):
```html
<p style="width:10em; border:solid aqua">
  <span style="float:left; width:5em; height:5em; border:solid blue"></span>
  Supercalifragilisticexpialidocious
</p>
```
- Variations: (a) no space before word, (b) space before word, (c) explicit `<br>` after span
- REF: in all 3, the long word is pushed BELOW the float
- TEST: (a) and (b) render the word beside the float (force-fit overflow); (c) passes

Current code at `pkg/layout/inline_layout.go:525-532`:
```go
if (floatStart > 0 || floatEnd > 0) && line.Width > lineAvailableInline && exclusionSpace != nil {
    clearedBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, bfcBlockOrigin+blockOffset, wdm)
    blockOffset = clearedBfc - bfcBlockOrigin
    lineInlineOffset = 0
    lineAvailableInline = contentInlineSize
}
```

**ACTUAL ROOT CAUSE (confirmed by code trace):** the guard `(floatStart > 0 || floatEnd > 0)` is FALSE on the first line because the float is still in `pendingFloats` — floats in our engine are only placed when their `InlineItemFloat` is encountered in the item loop at line ~556-562, AFTER the push-down check runs. So on the first iteration, `floatStart/floatEnd = 0` (exclusion space is empty) → push-down skipped → text force-fits at original block offset.

Variation 3 (`<br>` between float and text) passes because `<br>` forces NextLine to return a line with just the float. On that line the push-down isn't needed. On the NEXT line the float has been placed (now in exclusion space), so floatStart > 0, and the existing push-down fires correctly.

### LineBreaker state for rewinding
- `currentItemIndex int` — advances through `itemsData.Items`
- `currentTextOffset int` — byte offset into flat `TextContent` buffer; tracks progress within a text item. Reset each NextLine? NO — only reset by handleText etc. Must be manually reset on rewind to `Items[newIdx].StartOffset`.
- `done bool` — set true only when items exhausted; safe to leave false for rewind.
- `position float64` — reset to 0 at top of NextLine, so no rewind needed.

### InlineItem tokenization confirmed
`<span style="float:left"></span>` → single `InlineItemFloat` item (inline_item.go:166-171). No surrounding OpenTag/CloseTag for span-turned-float.

### `ExclusionSpace.ClearanceOffset(ClearBoth, currentBlockOffset, wdm)`
Returns `max(BlockEnd of all exclusions past currentBlockOffset, currentBlockOffset)`. After placing a float, this gives float's bottom — exactly what we want to push the text line to.

### Test #2: inline-box-002
Test structure:
```html
<div id=div1 style="height:2in; width:2in; position:relative; bg:yellow">
  <div id=div2 style="display:inline; position:relative; top:2in; bg:blue">
    <!-- text -->
    <div id=div3 style="width:2in; bg:orange; display:block"></div>
    <!-- text -->
  </div>
</div>
```
REF renders: yellow square → blue-text / orange block / blue-text clustered 2in below yellow.
TEST (post-Phase-1, 0.9% diff) renders: yellow square → orange block right below yellow (correct!) → blue stripes ~198px LOWER than they should be.

**ROOT CAUSE (confirmed by tree trace):** DOUBLE position:relative offset on inline continuations.

The layout tree builder does two things for a positioned inline with a block child:
1. `expandInlineWithBlockChildren` (layout_tree_builder.go:735-748): wraps the block child in an anon block with inherited position:relative top/left/right/bottom. → anon_wrapper_block_for_#div3 has relpos. Correct.
2. `maybeWrapAnonymousBlocks` (layout_tree_builder.go:402-418): when wrapping inline runs (continuations) in anon blocks, ALSO propagates position:relative to the wrapping anon block. → anon_wrapper_for_continuation_1 and _2 each get relpos.

Then at layout time:
- `block_layout.go:923-933` stamps each relpos anon block with `RelativeOffset.Y = +192`.
- `inline_layout.go:1137-1143` also stamps the text fragment of the inline continuation inside with `RelativeOffset.Y = +192` (because the inline continuation's own style has relpos top:2in).
- `engine.go:456-458` applies both in sequence during paint → total +384px (≈ observed 390).

The orange block (#div3) works correctly because its anon-block wrapper (case 1 above) carries the offset and #div3 itself has no relpos, so only a single +192 applies.

**FIX:** remove the position:relative propagation from `maybeWrapAnonymousBlocks` (lines 407-418). The inline continuation's own fragments already carry the offset; propagating to the wrapping anon block double-offsets. The block-child wrapper (case 1) stays because that wrapper is the ONLY site that carries the offset for #div3.

CSS 2.1 §9.4.3: relative positioning "does not affect the flow" — so the wrapping anon block should stay at its normal-flow position; subsequent siblings should use the pre-shift position. The inline's own offset shifts only the inline's own paint position, not the anon wrapper's flow position.

### Test #3: empty-inline-002
```html
<div id=div2 style="width:500px; border:25px solid green">
  <span style="bg:green; border:25px green; margin:100px; padding:100px"></span>
</div>
<!-- red z-index:-1 div behind -->
```
- Empty span's border-box = 250×250 (100+25 padding+border on each side)
- REF: solid green plus-shape covering red
- TEST: thin green horizontal stripe only

Bug hypothesis: inline fragment for empty span has near-zero block extent for painting. Painter uses line-box height instead of inline's own border-box.

## Blink References

### For Test #1 (float push-down)
- `InlineLayoutAlgorithm::HandleFloat` — commits floats to exclusion space BEFORE line fit check
- `LineBreaker::HandleFloat` — adds floats as items but doesn't let them consume line inline space
- CSS 2.1 §9.5.2: "If a shortened line box is too small to contain any content, then the line box is shifted downward (and its width recomputed) until either some content fits or there are no more floats present."

### For Test #2 (block-in-inline + position:relative)
- `LayoutBlockFlow::AnonymousBlockBeforeBlockInInline` — wraps block child of inline in anonymous block
- `NGInlineNode::CollectInlinesInternal` — split inline handling
- `NGBoxFragmentPainter::PaintBlockChildren` — applies parent's accumulated offsets including position:relative deltas
- CSS 2.1 §9.2.1.1: split inline elements retain position:relative offset on both fragments

### For Test #3 (empty inline painting)
- `InlineBoxFragmentPainter::PaintBoxDecorationBackground`
- `NGInlineBoxFragmentPainter::ComputeInkOverflow` — includes padding+border regardless of content extent
- CSS 2.1 §10.8: inline box vertical extent for LINE-HEIGHT calc is font metrics; for BOX DECORATIONS extent includes padding+border

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Fix order #1 → #2 → #3 | #1 is narrowest (one file, scaffolding exists). #2 builds inline-fragment knowledge useful for #3. |
| Each phase = separate commit | Bisectable; matches project operational discipline |
| No shared refactor | All three are in inline/rendering layer but address distinct bugs — avoid over-abstracting |

### Phase 5: cascade.go presentational attribute "px" double-suffix bug

**Bug:** `applyPresentationalAttributes` in `pkg/css/cascade.go` did `val+"px"` at every site without first stripping an existing "px" suffix. So `width="100px"` in HTML became CSS `width: 100pxpx` — an invalid value.

**Affected attributes / 6 sites:**
| HTML attribute | Element(s) | CSS property |
|----------------|------------|--------------|
| `width` (non-%) | table, td, th, col, colgroup, img, input, object, embed, hr | `width` |
| `height` (non-%) | table, td, th, tr, img, input, object, embed | `height` |
| `border` | `<table>` | `border-width` |
| `border` | `<img>` | `border-width` |
| `cellspacing` | `<table>` | `border-spacing` |
| `cellpadding` (ancestor table) | `<td>`, `<th>` | `padding` |

**Fix:** added `pxValue(s string) string` helper that does `strings.TrimSuffix(s, "px") + "px"`, then replaced all 6 `val+"px"` patterns with `pxValue(val)`.

**Verification:** 150 abs-pos VLR tests (`css-writing-modes/abs-pos-*`) — all PASS at 0 diff.

## Issues Encountered
| Issue | Resolution |
|-------|------------|
|       |            |

## Resources
- Test dir: `/Users/iansmith/louis14/pkg/visualtest/testdata/wpt-css2/`
- Current push-down code: `pkg/layout/inline_layout.go:525-532`
- Float exclusion API: `pkg/layout/*exclusion*` (TBD exact file)
- Test runner: `pkg/visualtest/reftest_runner_test.go`
- Recent PNG diffs (verified during prior investigation):
  - `output/reftests/float-no-content-beside-001_*.png`
  - `output/reftests/inline-box-002_*.png`
  - `output/reftests/empty-inline-002_*.png`

## Visual/Browser Findings
<!-- Captured from prior-session PNG diff inspection; re-verify before implementation -->
- **float-no-content-beside-001**: TEST shows variations 1 and 2 with text wrapping beside the float (incorrect); variation 3 with explicit `<br>` renders correctly. REF has all three variations identical — word below float.
- **inline-box-002**: TEST's orange block appears flush beneath yellow with no 2in shift; blue text appears far below. REF has blue/orange/blue grouped 2in below yellow (all three moved together by position:relative).
- **empty-inline-002**: TEST shows thin green horizontal stripe with red leaking above and below. REF shows solid green plus-shape that fully occludes the z-index:-1 red div.

---
*Update after every 2 view/browser/search operations*
