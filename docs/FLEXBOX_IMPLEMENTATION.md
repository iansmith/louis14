# Flexbox Implementation Plan

This document describes how CSS Flexbox will be implemented in louis14, building on the new architectural foundations.

## Architecture Overview

The new layout architecture provides these foundations for flexbox:

### 1. IntrinsicSizes

```go
type IntrinsicSizes struct {
    MinContent float64  // Width when all soft wrap opportunities are taken
    MaxContent float64  // Width when no soft wrapping occurs
    Preferred  float64  // Preferred width (for flex-basis: auto)
}
```

Flexbox needs intrinsic sizes for:
- `flex-basis: auto` - uses the item's `max-content` size
- `flex-basis: content` - uses the item's content size
- Resolving `min-width: auto` on flex items

### 2. Axis Abstraction

```go
type Axis int
const (
    AxisHorizontal Axis = iota
    AxisVertical
)
```

Flexbox operates on main and cross axes. For `flex-direction: row`, main=horizontal, cross=vertical. For `flex-direction: column`, it's reversed.

### 3. Alignment Types

```go
type Alignment int
const (
    AlignStart Alignment = iota
    AlignEnd
    AlignCenter
    AlignStretch
    AlignBaseline
    AlignSpaceBetween
    AlignSpaceAround
    AlignSpaceEvenly
)
```

These map to CSS alignment properties:
- `justify-content` - main axis alignment of items
- `align-items` - cross axis alignment of items
- `align-content` - cross axis alignment of lines (for wrapped flex)
- `align-self` - per-item cross axis alignment

### 4. LayoutMode Interface

```go
type LayoutMode interface {
    ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style,
                          computedStyles map[*html.Node]*css.Style) IntrinsicSizes
    LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node,
                   availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box
}
```

`FlexLayoutMode` will implement this interface with the full flexbox algorithm.

## Flexbox Algorithm Overview

The CSS Flexbox algorithm (CSS Flexible Box Layout Module Level 1 §9) has these main steps:

### Step 1: Generate Flex Items

1. Each in-flow child becomes a flex item
2. Anonymous flex items wrap consecutive text nodes
3. `display: none` children are skipped
4. Absolutely positioned children are positioned separately

### Step 2: Determine Available Space

```go
type FlexContext struct {
    MainSize        float64  // Available size on main axis
    CrossSize       float64  // Available size on cross axis (may be indefinite)
    IsRow           bool     // true for row/row-reverse
    IsReverse       bool     // true for row-reverse/column-reverse
    IsWrap          bool     // true for wrap/wrap-reverse
    IsWrapReverse   bool     // true for wrap-reverse
}
```

### Step 3: Determine Flex Base Size and Hypothetical Main Size

For each flex item:

```go
type FlexItem struct {
    Box            *Box
    FlexGrow       float64   // from flex-grow property
    FlexShrink     float64   // from flex-shrink property
    FlexBasis      float64   // resolved flex-basis
    HypotheticalMain float64 // clamped by min/max
    MainSize       float64   // final main size after flex
    CrossSize      float64   // cross size
    Order          int       // from order property
}
```

Flex basis resolution:
- `flex-basis: auto` → use `width`/`height` if set, else content size
- `flex-basis: content` → use content size
- `flex-basis: <length>` → use specified length

### Step 4: Collect Flex Items into Lines

```go
type FlexLine struct {
    Items     []*FlexItem
    MainSize  float64      // sum of hypothetical main sizes
    CrossSize float64      // max cross size in line
}
```

For `flex-wrap: nowrap`, all items go in one line.
For `flex-wrap: wrap`, start new line when items overflow.

### Step 5: Resolve Flexible Lengths

This is the core flex algorithm:

```go
func resolveFlexibleLengths(line *FlexLine, availableMain float64) {
    // Calculate free space
    freeSpace := availableMain - line.MainSize

    if freeSpace > 0 {
        // Grow items using flex-grow
        totalGrow := sumFlexGrow(line.Items)
        if totalGrow > 0 {
            for _, item := range line.Items {
                item.MainSize = item.HypotheticalMain +
                    (freeSpace * item.FlexGrow / totalGrow)
            }
        }
    } else if freeSpace < 0 {
        // Shrink items using flex-shrink
        // (weighted by flex-shrink * flex-basis)
        totalShrink := sumScaledFlexShrink(line.Items)
        if totalShrink > 0 {
            for _, item := range line.Items {
                shrinkRatio := (item.FlexShrink * item.FlexBasis) / totalShrink
                item.MainSize = item.HypotheticalMain + (freeSpace * shrinkRatio)
            }
        }
    }

    // Clamp to min/max constraints
    for _, item := range line.Items {
        item.MainSize = clamp(item.MainSize, item.MinMain, item.MaxMain)
    }
}
```

### Step 6: Determine Cross Sizes

1. If `align-items: stretch` and cross size is auto, stretch to fill line
2. Otherwise, use intrinsic cross size
3. Apply min/max constraints

### Step 7: Main Axis Alignment (justify-content)

```go
func justifyContent(line *FlexLine, availableMain float64, justify Alignment) {
    usedMain := sumMainSizes(line.Items)
    freeSpace := availableMain - usedMain

    switch justify {
    case AlignStart:
        // Items at start, no spacing
        pos := 0.0
        for _, item := range line.Items {
            item.MainPos = pos
            pos += item.MainSize
        }

    case AlignEnd:
        // Items at end
        pos := freeSpace
        for _, item := range line.Items {
            item.MainPos = pos
            pos += item.MainSize
        }

    case AlignCenter:
        // Items centered
        pos := freeSpace / 2
        for _, item := range line.Items {
            item.MainPos = pos
            pos += item.MainSize
        }

    case AlignSpaceBetween:
        // First at start, last at end, even spacing
        if len(line.Items) == 1 {
            line.Items[0].MainPos = 0
        } else {
            gap := freeSpace / float64(len(line.Items)-1)
            pos := 0.0
            for _, item := range line.Items {
                item.MainPos = pos
                pos += item.MainSize + gap
            }
        }

    case AlignSpaceAround:
        // Equal space around each item
        gap := freeSpace / float64(len(line.Items))
        pos := gap / 2
        for _, item := range line.Items {
            item.MainPos = pos
            pos += item.MainSize + gap
        }

    case AlignSpaceEvenly:
        // Equal space between items and edges
        gap := freeSpace / float64(len(line.Items)+1)
        pos := gap
        for _, item := range line.Items {
            item.MainPos = pos
            pos += item.MainSize + gap
        }
    }
}
```

### Step 8: Cross Axis Alignment (align-items, align-self)

```go
func alignItems(line *FlexLine, alignItems Alignment) {
    for _, item := range line.Items {
        align := alignItems
        if item.AlignSelf != AlignAuto {
            align = item.AlignSelf
        }

        switch align {
        case AlignStart:
            item.CrossPos = 0
        case AlignEnd:
            item.CrossPos = line.CrossSize - item.CrossSize
        case AlignCenter:
            item.CrossPos = (line.CrossSize - item.CrossSize) / 2
        case AlignStretch:
            item.CrossPos = 0
            item.CrossSize = line.CrossSize
        case AlignBaseline:
            // Align baselines (complex, requires baseline calculation)
        }
    }
}
```

### Step 9: Align Flex Lines (align-content)

For multi-line flex containers:

```go
func alignContent(lines []*FlexLine, availableCross float64, alignContent Alignment) {
    usedCross := sumCrossSizes(lines)
    freeSpace := availableCross - usedCross

    // Similar logic to justify-content but for lines
}
```

## Implementation Plan

### Phase 1: Basic Flex Container

1. Detect `display: flex` / `display: inline-flex`
2. Create flex items from children
3. Implement single-line, no-wrap layout
4. Support `flex-direction: row` only

### Phase 2: Flex Properties

1. Implement `flex-grow`, `flex-shrink`, `flex-basis`
2. Implement the flex length resolution algorithm
3. Support `flex` shorthand parsing

### Phase 3: Alignment

1. Implement `justify-content`
2. Implement `align-items` and `align-self`
3. Add baseline alignment support

### Phase 4: Wrapping

1. Implement `flex-wrap`
2. Implement multi-line layout
3. Implement `align-content`

### Phase 5: Advanced Features

1. Support `flex-direction: column`, `row-reverse`, `column-reverse`
2. Implement `order` property
3. Support `gap` property
4. Handle min/max sizing constraints properly

## CSS Properties to Support

### Container Properties

| Property | Values | Default |
|----------|--------|---------|
| `display` | `flex`, `inline-flex` | - |
| `flex-direction` | `row`, `row-reverse`, `column`, `column-reverse` | `row` |
| `flex-wrap` | `nowrap`, `wrap`, `wrap-reverse` | `nowrap` |
| `justify-content` | `flex-start`, `flex-end`, `center`, `space-between`, `space-around`, `space-evenly` | `flex-start` |
| `align-items` | `stretch`, `flex-start`, `flex-end`, `center`, `baseline` | `stretch` |
| `align-content` | `stretch`, `flex-start`, `flex-end`, `center`, `space-between`, `space-around` | `stretch` |
| `gap` | `<length>` | `0` |

### Item Properties

| Property | Values | Default |
|----------|--------|---------|
| `flex-grow` | `<number>` | `0` |
| `flex-shrink` | `<number>` | `1` |
| `flex-basis` | `auto`, `content`, `<length>` | `auto` |
| `flex` | shorthand for grow/shrink/basis | `0 1 auto` |
| `align-self` | `auto`, `stretch`, `flex-start`, `flex-end`, `center`, `baseline` | `auto` |
| `order` | `<integer>` | `0` |

## Testing Strategy

1. Use WPT flexbox tests from `css-flexbox-1/`
2. Start with simple single-line tests
3. Progress to wrapping and alignment tests
4. Verify with real-world flex layouts

## References

- [CSS Flexible Box Layout Module Level 1](https://www.w3.org/TR/css-flexbox-1/)
- [MDN Flexbox Guide](https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_Flexible_Box_Layout)
- [CSS Flexbox Algorithm (§9)](https://www.w3.org/TR/css-flexbox-1/#layout-algorithm)
