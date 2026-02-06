# Plan: Inline Element Box Creation in Multi-Pass Architecture

## Goal
Fix multi-pass to create proper boxes for inline elements (`<span>`, `<em>`, `<strong>`, etc.) so backgrounds and borders paint correctly across their content extent.

**Target**: Reduce 7.1% failure to 5.4% or better (eliminate 1.7pp regression)

---

## Current State

### What Works ✅
- Text nodes create boxes correctly
- Block children laid out recursively
- Float positioning working
- Y position correction after blocks
- Line-aware Y tracking

### What's Broken ❌
- Inline elements treated as zero-size markers
- Backgrounds on `<span>` don't paint
- Only text content gets boxes, not wrapper elements

### Example Failure
```html
<span style="background: orange">Inline box</span>
```

**Expected**: Orange background across ~400px (full line width)
**Actual**: Orange background only ~70px (text width)

**Impact**: 7.1% failure vs 5.4% baseline (31% worse)

---

## Architecture Analysis

### Current Flow (Phase 3: constructLine)

```
<span id="span1">Inline box</span>
↓ CollectInlineItems (Phase 1)
↓
[OpenTag(span1), Text("Inline box"), CloseTag(span1)]
↓ constructLine (Phase 3)
↓
[
  Fragment{Type: FragmentInline, Width: 0, Height: 0},  ← OpenTag marker
  Fragment{Type: FragmentText, Width: 70, Height: 12},  ← Text content
  Fragment{Type: FragmentInline, Width: 0, Height: 0}   ← CloseTag marker
]
↓ fragmentToBoxSingle
↓
[
  Box{Width: 70, Height: 12, Node: textNode}  ← Only text box created!
]
```

### Desired Flow

```
<span id="span1">Inline box</span>
↓ CollectInlineItems (Phase 1) - UNCHANGED
↓
[OpenTag(span1), Text("Inline box"), CloseTag(span1)]
↓ constructLine (Phase 3) - UNCHANGED
↓
[
  Fragment{Type: FragmentInline, Width: 0, Height: 0, Node: span1},
  Fragment{Type: FragmentText, Width: 70, Height: 12},
  Fragment{Type: FragmentInline, Width: 0, Height: 0, Node: span1}
]
↓ LayoutInlineContentToBoxes - NEW LOGIC HERE
↓
[
  Box{Width: 70, Height: 12, Node: textNode},     ← Text box
  Box{Width: 400, Height: 12, Node: span1}        ← NEW: Wrapper box for span
]
```

**Key Insight**: Don't change Phase 1-3. Fix in LayoutInlineContentToBoxes by post-processing fragments to create wrapper boxes.

---

## Implementation Plan

### Phase A: Single-Line Inline Elements (Simple Case)

**Goal**: Handle inline elements that fit on one line

**Steps**:

1. **Track OpenTag/CloseTag pairs** (in LayoutInlineContentToBoxes)
   ```go
   type inlineSpan struct {
       node     *html.Node
       style    *css.Style
       startX   float64
       startY   float64
       startIdx int  // Fragment index of OpenTag
   }

   inlineStack := []*inlineSpan{}
   ```

2. **Handle OpenTag fragments**
   ```go
   if frag.Type == FragmentInline && isOpenTag(frag.Node) {
       // Push onto stack
       span := &inlineSpan{
           node:     frag.Node,
           style:    frag.Style,
           startX:   currentX,  // Track where span starts
           startY:   currentY,
           startIdx: i,
       }
       inlineStack = append(inlineStack, span)
       continue  // Don't create box for marker
   }
   ```

3. **Handle CloseTag fragments**
   ```go
   if frag.Type == FragmentInline && isCloseTag(frag.Node) {
       // Pop from stack
       if len(inlineStack) > 0 {
           span := inlineStack[len(inlineStack)-1]
           inlineStack = inlineStack[:len(inlineStack)-1]

           // Create wrapper box spanning from start to current position
           wrapperBox := &Box{
               Node:   span.node,
               Style:  span.style,
               X:      span.startX,
               Y:      span.startY,
               Width:  currentX - span.startX,  // Span width
               Height: currentLineMaxHeight,    // Line height
               Parent: containerBox,
           }
           boxes = append(boxes, wrapperBox)
       }
       continue  // Don't create box for marker
   }
   ```

4. **Helper to distinguish OpenTag vs CloseTag**
   ```go
   func isOpenTag(node *html.Node) bool {
       // Check if this is an opening tag by looking at fragment position
       // OpenTag appears BEFORE node's children in fragment stream
   }

   func isCloseTag(node *html.Node) bool {
       // CloseTag appears AFTER node's children
   }
   ```

   **Challenge**: Fragments don't distinguish OpenTag vs CloseTag!
   Both create `FragmentInline` with same node.

   **Solution**: Track fragment sequence:
   - First FragmentInline for a node = OpenTag
   - Second FragmentInline for same node = CloseTag
   - Use map: `seenNodes := make(map[*html.Node]bool)`

5. **Update currentX tracking**
   ```go
   // After processing non-marker fragments
   if frag.Type == FragmentText || frag.Type == FragmentAtomic {
       currentX = box.X + box.Width  // Track rightmost position
   }
   ```

**Testing**:
```html
<span style="background: orange">Text</span>
```
Should create wrapper box with orange background spanning text width.

---

### Phase B: Multi-Line Inline Elements (Complex Case)

**Challenge**: Inline elements can span multiple lines

```html
<span style="background: orange">This is a very long text that wraps
to multiple lines and the orange background should appear on all lines</span>
```

**Expected**: Orange background on EACH line the span appears on

**Current Issue**: Line breaking happens in Phase 2, but we don't know which fragments belong to which lines in Phase 3.

**Solutions**:

#### Option B1: Track Line Membership (Recommended)
1. **Add line reference to fragments**
   ```go
   type Fragment struct {
       ...
       LineY float64  // Which line this fragment is on
   }
   ```

2. **Set LineY during constructLine**
   ```go
   frag := NewTextFragment(..., line.Y)  // Pass line Y
   ```

3. **Create separate wrapper box per line**
   ```go
   // When processing CloseTag:
   // Group fragments by LineY between OpenTag and CloseTag
   lineFragments := groupByLine(startIdx, i, fragments)

   for lineY, lineFrags := range lineFragments {
       minX := min(lineFrags, frag => frag.Position.X)
       maxX := max(lineFrags, frag => frag.Position.X + frag.Width)

       wrapperBox := &Box{
           X:      minX,
           Y:      lineY,
           Width:  maxX - minX,
           Height: lineHeight,
           ...
       }
       boxes = append(boxes, wrapperBox)
   }
   ```

#### Option B2: Defer to Rendering (Simpler, Less Accurate)
- Create single wrapper box with width = total content
- Let renderer handle line wrapping and background painting
- **Caveat**: Renderer may not handle this correctly

**Recommendation**: Option B1 for correctness

---

### Phase C: Nested Inline Elements

**Challenge**: Inline elements can be nested

```html
<span style="background: orange">
    Outer <em style="background: yellow">inner</em> outer
</span>
```

**Expected**:
- Yellow background on "inner"
- Orange background on "Outer", "inner", "outer"
- Overlapping backgrounds (yellow on top of orange for "inner")

**Current**: Stack-based tracking already handles this!
- OpenTag(span) → push span
- OpenTag(em) → push em
- CloseTag(em) → pop em, create box
- CloseTag(span) → pop span, create box

**Additional Consideration**: Z-order
- Outer inline elements should be painted BEHIND inner elements
- Ensure wrapper boxes are added in correct order
- **Solution**: Add outer boxes to beginning of list, not end
  ```go
  boxes = append([]*Box{wrapperBox}, boxes...)  // Prepend
  ```

---

### Phase D: Edge Cases

#### D1: Empty Inline Elements
```html
<span style="background: orange"></span>
```
- OpenTag and CloseTag at same position
- Width = 0
- Still create box (zero-width background, technically valid)

#### D2: Inline Elements with Only Whitespace
```html
<span style="background: orange">   </span>
```
- Text collapsed to zero width
- Create zero-width wrapper box

#### D3: Inline Elements Broken by Floats
```html
<span>Text <span style="float:left">Float</span> More text</span>
```
- Float is removed from inline flow
- Outer span should NOT include float in width calculation
- **Solution**: Skip FragmentFloat when calculating span width

#### D4: Absolutely Positioned Inline Elements
```html
<span>Text <span style="position: absolute">Abs</span> More</span>
```
- Similar to floats - abs positioned content removed from flow
- Outer span continues as if abs element doesn't exist

---

## Testing Strategy

### Test 1: Simple Inline Background
```html
<span style="background: orange">Text</span>
```
**Expected**: Orange box 70px wide (text width)
**Measure**: Visual inspection + box tree check

### Test 2: Full-Width Inline (box-generation-001)
```html
<div>
    <span style="background: orange">Inline box</span>
    <span style="float:left; background: yellow">Float</span>
</div>
```
**Expected**: Orange extends from after yellow float to right edge
**Measure**: Pixel diff should reduce from 7.1% toward 5.4%

### Test 3: Multi-Line Inline
```html
<div style="width: 100px">
    <span style="background: orange">This wraps to multiple lines</span>
</div>
```
**Expected**: Orange background on each line
**Measure**: Visual inspection

### Test 4: Nested Inline
```html
<span style="background: orange">
    Outer <em style="background: yellow">inner</em> outer
</span>
```
**Expected**: Yellow over orange for "inner", only orange elsewhere
**Measure**: Z-order check + visual

### Test 5: Inline with Float
```html
<span style="background: orange">
    Text <span style="float:left">Float</span> More
</span>
```
**Expected**: Orange background doesn't include float width
**Measure**: Wrapper box width calculation

---

## Implementation Order

1. **✓ DONE**: Identify problem and create plan
2. **Phase A**: Single-line inline boxes
   - Implement OpenTag/CloseTag tracking
   - Add isOpenTag/isCloseTag logic using seen map
   - Create wrapper boxes for simple case
   - Test with test-span-width.html
3. **Test simple case**: Verify basic functionality
4. **Phase B**: Multi-line support (if needed)
   - Add LineY to fragments
   - Group fragments by line
   - Create per-line wrapper boxes
5. **Test multi-line**: Verify wrapping works
6. **Phase C**: Nested inline (should already work)
   - Verify z-order (prepend vs append)
7. **Test nested**: Verify overlapping backgrounds
8. **Phase D**: Edge cases
   - Test empty spans, whitespace, floats, abs positioned
9. **Final test**: Run box-generation-001.xht
   - Measure new baseline
   - Target: ≤ 5.4% (at or below single-pass)

---

## Success Criteria

**Minimum Success** (MVP):
- [ ] Single-line inline elements create wrapper boxes
- [ ] Backgrounds paint correctly for simple cases
- [ ] box-generation-001.xht improves from 7.1% to ≤ 6.0%

**Full Success**:
- [ ] Multi-line inline elements handled correctly
- [ ] Nested inline elements work (z-order correct)
- [ ] box-generation-001.xht matches or beats 5.4% baseline
- [ ] No regressions on other tests

**Stretch Goals**:
- [ ] Improve beyond 5.4% baseline (beat single-pass!)
- [ ] All inline element edge cases handled
- [ ] Multi-pass becomes default for inline contexts

---

## Risk Assessment

**Low Risk**:
- Single-line implementation - contained change in LayoutInlineContentToBoxes
- Can disable multi-pass if broken (no regression to main branch)

**Medium Risk**:
- Multi-line handling - requires fragment line tracking
- May need to modify constructLine (Phase 3)

**High Risk**:
- Z-order for nested elements - rendering code may not handle correctly
- Performance impact of creating many wrapper boxes

**Mitigation**:
- Implement incrementally, test at each phase
- Keep multi-pass disabled by default
- Can revert to current state if needed (git branch)

---

## Open Questions

1. **Fragment reordering**: Do we need to ensure wrapper boxes are painted before content?
   - **Answer TBD**: Test and observe rendering order

2. **Line height calculation**: How to get accurate line height for wrapper boxes?
   - **Current**: Use currentLineMaxHeight
   - **Better**: Track line height in LineInfo from Phase 2?

3. **Rendering support**: Does the renderer correctly paint backgrounds for inline boxes?
   - **Answer TBD**: May need renderer changes

4. **Performance**: Creating many wrapper boxes - is this acceptable?
   - **Answer TBD**: Profile and measure

---

## Next Actions

1. Implement isOpenTag/isCloseTag using seenNodes map
2. Add OpenTag tracking (push to inlineStack)
3. Add CloseTag handling (pop and create wrapper box)
4. Test with test-span-width.html
5. Enable multi-pass for box-generation-001.xht
6. Measure improvement
7. Iterate based on results

---

## References

- **CSS Spec**: https://www.w3.org/TR/CSS2/visuren.html#inline-boxes
  - Section 9.2.2: Inline-level elements and inline boxes
  - Section 9.4.2: Inline formatting contexts
- **Blink LayoutNG**: https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/ng/
  - NGInlineItem for item representation
  - NGInlineBoxState for tracking inline element spans
  - NGFragmentItem for final positioned fragments
- **Test File**: pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht
- **Current Code**: pkg/layout/layout.go lines 1220-1310 (LayoutInlineContentToBoxes)
