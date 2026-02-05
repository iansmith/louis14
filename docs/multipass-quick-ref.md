# Multi-Pass Inline Layout - Quick Reference

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ layoutNode (main layout function)                      │
│                                                         │
│  Loop through children:                                │
│  ┌────────────────────────────────────────────┐       │
│  │ If inline/text children found:              │       │
│  │  1. Collect consecutive inline batch        │       │
│  │  2. Call LayoutInlineBatch()                │       │
│  │     ↓                                        │       │
│  │     ┌─────────────────────────────────┐    │       │
│  │     │ Phase 1: CollectInlineItems     │    │       │
│  │     │  - Flatten DOM to item list     │    │       │
│  │     └─────────────────────────────────┘    │       │
│  │     ┌─────────────────────────────────┐    │       │
│  │     │ Phase 2: BreakLines             │    │       │
│  │     │  - Decide line breaks           │    │       │
│  │     │  - Account for float offsets    │    │       │
│  │     └─────────────────────────────────┘    │       │
│  │     ┌─────────────────────────────────┐    │       │
│  │     │ Phase 3: ConstructLineBoxes     │    │       │
│  │     │  - Create positioned boxes      │    │       │
│  │     │  - Layout floats                │    │       │
│  │     │  - Update currentX for floats   │    │       │
│  │     └─────────────────────────────────┘    │       │
│  │     │                                       │       │
│  │     └→ Returns boxes + retryNeeded         │       │
│  │                                             │       │
│  │  3. If retryNeeded, retry with floats      │       │
│  │  4. Add boxes to parent                    │       │
│  │  5. Update inline context                  │       │
│  └────────────────────────────────────────────┘       │
│                                                         │
│  ┌────────────────────────────────────────────┐       │
│  │ If block element found:                     │       │
│  │  - Process normally (unchanged)             │       │
│  └────────────────────────────────────────────┘       │
└─────────────────────────────────────────────────────────┘
```

## Key Methods

### LayoutInlineBatch
```go
func (le *LayoutEngine) LayoutInlineBatch(
    children []*html.Node,      // Batch of inline children
    box *Box,                   // Container box
    availableWidth float64,     // Available horizontal space
    startY float64,            // Starting Y position
    border, padding css.BoxEdge,
    computedStyles map[*html.Node]*css.Style,
) []*Box
```

**Purpose**: Process a batch of consecutive inline/text children in one pass
**Returns**: Positioned boxes ready to add to parent.Children

### CollectInlineItems
```go
func (le *LayoutEngine) CollectInlineItems(
    node *html.Node,
    state *InlineLayoutState,
    computedStyles map[*html.Node]*css.Style,
)
```

**Purpose**: Flatten DOM tree into sequential InlineItem list
**Side effects**: Appends to state.Items

**Item types**:
- `InlineItemText`: Text content
- `InlineItemOpenTag`: Start of inline element
- `InlineItemCloseTag`: End of inline element
- `InlineItemAtomic`: Inline-block or replaced element
- `InlineItemFloat`: Floated element
- `InlineItemControl`: Line break control (e.g., `<br>`)

### BreakLines
```go
func (le *LayoutEngine) BreakLines(state *InlineLayoutState) bool
```

**Purpose**: Decide which items go on which lines
**Returns**: true if successful, false if failed
**Side effects**: Populates state.Lines with LineBreakResult

### ConstructLineBoxes / constructLineBoxesWithRetry
```go
func (le *LayoutEngine) ConstructLineBoxes(
    state *InlineLayoutState,
    parent *Box,
) []*Box

func (le *LayoutEngine) constructLineBoxesWithRetry(
    state *InlineLayoutState,
    parent *Box,
    computedStyles map[*html.Node]*css.Style,
) ([]*Box, bool) // Returns (boxes, retryNeeded)
```

**Purpose**: Create actual positioned Box fragments from line breaking results
**Returns**: Array of positioned boxes (and retry flag for WithRetry version)

**Key behavior**:
- Handles floats by calling layoutNode and adding to le.floats
- Updates currentX after left floats
- Detects when floats change available width (triggers retry)

## Data Structures

### InlineLayoutState
```go
type InlineLayoutState struct {
    Items          []*InlineItem       // Phase 1 output
    Lines          []*LineBreakResult  // Phase 2 output
    ContainerBox   *Box
    ContainerStyle *css.Style
    AvailableWidth float64
    StartY         float64
    Border         css.BoxEdge
    Padding        css.BoxEdge
    FloatList      []FloatInfo
    FloatBaseIndex int  // Where this batch started adding floats
}
```

### InlineItem
```go
type InlineItem struct {
    Type        InlineItemType
    Node        *html.Node
    Text        string        // For text items
    StartOffset int           // For text breaking
    EndOffset   int
    Style       *css.Style
    Width       float64
    Height      float64
}
```

### LineBreakResult
```go
type LineBreakResult struct {
    Items          []*InlineItem
    StartIndex     int
    EndIndex       int
    Y              float64
    AvailableWidth float64
    LineHeight     float64
    TextBreaks     map[*InlineItem]struct{...}
}
```

## Retry Mechanism

**Why needed**: Floats that appear later in document order affect earlier inline content

**How it works**:
1. First pass: Layout without knowing about floats
2. Float gets added to le.floats during ConstructLineBoxes
3. Detect available width changed → set retryNeeded=true
4. Retry: BreakLines now sees the float and accounts for it
5. ConstructLineBoxes creates boxes with correct positions

**Critical fix**: Don't reset le.floats between retries (keep accumulated floats)

## Common Patterns

### Checking if element should be in inline batch
```go
isInlineChild := false
if node.Type == html.TextNode {
    isInlineChild = true
} else if node.Type == html.ElementNode {
    style := computedStyles[node]
    display := style.GetDisplay()
    float := style.GetFloat()

    isInlineChild = (display == css.DisplayInline ||
                    display == css.DisplayInlineBlock ||
                    float != css.FloatNone)
}
```

### Collecting a batch
```go
batchStart := i
batchEnd := i + 1

for batchEnd < len(children) {
    if isInline(children[batchEnd]) {
        batchEnd++
    } else {
        break
    }
}

batch := children[batchStart:batchEnd]
```

### Processing batch and updating context
```go
batchBoxes := le.LayoutInlineBatch(batch, box, ...)

for _, batchBox := range batchBoxes {
    box.Children = append(box.Children, batchBox)
}

if len(batchBoxes) > 0 {
    lastBox := batchBoxes[len(batchBoxes)-1]
    inlineCtx.LineY = lastBox.Y
    inlineCtx.LineX = lastBox.X + le.getTotalWidth(lastBox)
    inlineCtx.LineHeight = le.getTotalHeight(lastBox)
}
```

## Testing Commands

```bash
# Format code
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/gofmt -w pkg/layout/layout.go

# Compile
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go build ./pkg/layout

# Run isolated test
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go run /tmp/test-batch-integration.go

# Run visual tests
make test-visual FILTER=box-generation-001
make test-visual FILTER=before-after-floated-001
```

## Key Learnings

1. **Floats must persist between retries** - don't reset le.floats
2. **Update currentX after adding left float** - subsequent content must clear it
3. **Retry detects width change** - not position change
4. **2 iterations typical** - initial + 1 retry for most cases
5. **Format first, compile second** - gofmt catches many issues

## Files Modified

- `pkg/layout/layout.go` - All multi-pass logic
- `docs/multipass-integration-plan.md` - Integration plan
- `.claude/projects/.../memory/MEMORY.md` - Learnings documentation
