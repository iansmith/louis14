# Louis14 Rendering Engine - Architecture

## Overview
Louis14 is a web rendering engine built in Go, targeting ACID2 compliance through incremental phased development. Built on top of `fogleman/gg` for 2D graphics primitives.

## Design Philosophy
- **Incremental**: Each phase adds one major capability
- **Testable**: Visual output after each phase
- **Clean separation**: Components communicate through well-defined interfaces
- **No premature optimization**: Correctness first, performance later

## Core Components

### 1. HTML Parser (`pkg/html`)
- **Tokenizer**: Breaks HTML into tokens (tags, text, attributes)
- **Tree Builder**: Constructs DOM tree from tokens
- **DOM**: In-memory representation of document structure

### 2. CSS Engine (`pkg/css`)
- **Tokenizer**: Breaks CSS into tokens
- **Parser**: Builds stylesheet rules
- **Selector Matcher**: Determines which rules apply to which elements
- **Cascade/Specificity**: Resolves conflicting rules
- **Computed Styles**: Final style values for each element

### 3. Layout Engine (`pkg/layout`)
- **Box Tree Builder**: Creates layout boxes from DOM + styles
- **Box Model**: Width, height, padding, border, margin calculations
- **Positioning**: Static, relative, absolute, fixed
- **Floats**: Float positioning and clearing
- **Tables**: Table layout algorithm
- **Line Breaking**: Text wrapping and line boxes

### 4. Rendering Engine (`pkg/render`)
- **Paint**: Converts layout boxes to drawing commands
- **Stacking Context**: Z-order rendering
- **Text Rendering**: Font handling and text drawing
- **Background/Border**: Colors, images, border styles

### 5. Resource Loading (`pkg/resources`)
- **Image Loader**: PNG, JPEG, GIF support
- **Font Loader**: TrueType/OpenType fonts
- **Cache**: Resource caching

## Development Phases

### Phase 1: Basic HTML + Simple Rendering ✓ (Session 1)
**Goal**: Render "Hello World" with colored rectangles

**Capabilities**:
- Parse simple HTML (no nesting yet, just sequential elements)
- Tokenize basic tags: `<div>`, `<p>`, `<span>`
- Inline `style` attribute parsing (background-color, width, height only)
- Render colored rectangles with text
- Basic box model (width, height, background)

**Test Case**: 
```html
<div style="background-color: red; width: 100px; height: 100px;">Red Box</div>
<div style="background-color: blue; width: 150px; height: 50px;">Blue Box</div>
```

**Output**: PNG image with two colored boxes stacked vertically

**Components to build**:
- HTML tokenizer
- Minimal DOM (flat list of elements)
- Minimal CSS parser (inline styles only)
- Basic layout (sequential block boxes)
- Basic renderer (rectangles + text)

---

### Phase 2: Nested Elements + Box Model (Session 2)
**Goal**: Proper document tree and complete box model

**Capabilities**:
- Nested HTML elements (proper tree structure)
- Full box model: margin, border, padding
- Block vs inline display
- Basic CSS properties: color, font-size, border, margin, padding

**Test Case**: Nested divs with margins/padding/borders

---

### Phase 3: CSS Stylesheets + Cascade (Session 3)
**Goal**: External stylesheets and CSS cascade

**Capabilities**:
- `<style>` tag parsing
- CSS selectors (element, class, id)
- Specificity and cascade resolution
- Inheritance

**Test Case**: HTML with `<style>` block using multiple selectors

---

### Phase 4: Positioning (Session 4)
**Goal**: Static, relative, absolute positioning

**Capabilities**:
- `position` property
- `top`, `left`, `right`, `bottom`
- Containing blocks
- Basic z-index

**Test Case**: Absolutely positioned elements overlapping

---

### Phase 5: Floats (Session 5)
**Goal**: Float layout

**Capabilities**:
- `float: left/right`
- `clear` property
- Float positioning algorithm
- Text wrapping around floats

**Test Case**: Floated images with text wrap

---

### Phase 6: Text Layout (Session 6)
**Goal**: Proper text rendering and line breaking

**Capabilities**:
- Font loading and metrics
- Line breaking algorithm
- Baseline alignment
- Text decoration (underline, etc.)

---

### Phase 7: Tables (Session 7+)
**Goal**: Table layout

**Capabilities**:
- Table structure parsing
- Table layout algorithm
- Cell sizing and spanning

---

### Phase 8+: ACID2 Specific Features
- Advanced selectors
- Content generation (::before, ::after)
- Additional positioning edge cases
- Any remaining ACID2 requirements

## Project Structure

```
~/louis14/
├── ARCHITECTURE.md          # This file
├── README.md                # Project overview and build instructions
├── go.mod                   # Go module definition
├── cmd/
│   └── louis14/
│       └── main.go          # CLI entry point
├── pkg/
│   ├── html/
│   │   ├── tokenizer.go     # HTML tokenization
│   │   ├── parser.go        # DOM tree construction
│   │   └── dom.go           # DOM node structures
│   ├── css/
│   │   ├── tokenizer.go     # CSS tokenization
│   │   ├── parser.go        # Stylesheet parsing
│   │   ├── selector.go      # Selector matching
│   │   └── style.go         # Computed style structures
│   ├── layout/
│   │   ├── box.go           # Layout box structures
│   │   ├── builder.go       # Box tree construction
│   │   └── compute.go       # Layout computation
│   └── render/
│       ├── painter.go       # Drawing operations
│       └── text.go          # Text rendering
├── testdata/
│   ├── phase1/              # Phase 1 test HTML files
│   ├── phase2/
│   └── ...
└── output/                  # Generated PNG outputs
```

## Data Flow

```
HTML String
    ↓
[HTML Tokenizer] → Tokens
    ↓
[HTML Parser] → DOM Tree
    ↓
[CSS Parser] → Styled DOM (DOM + computed styles)
    ↓
[Layout Engine] → Layout Tree (boxes with positions/sizes)
    ↓
[Rendering Engine] → gg.Context drawing commands
    ↓
PNG Image
```

## Key Design Decisions

1. **Simplified Initial Parsing**: Phase 1 uses simplified tokenizer that doesn't handle all edge cases. We'll enhance it in later phases.

2. **No JavaScript**: This is a rendering engine only. No JS execution.

3. **gg Integration**: We use `fogleman/gg` for all low-level drawing. No direct image manipulation.

4. **Incremental DOM**: Start with flat list (Phase 1), evolve to tree (Phase 2).

5. **Testing Strategy**: Each phase has visual test cases. We compare output images to expected results.

## Dependencies

- `github.com/fogleman/gg` - 2D graphics
- `golang.org/x/image/font` - Font handling (Phase 6+)
- Standard library for everything else

## Session Budget Tracking

- **Session 1** (Phase 1): ~15-30 messages
  - Architecture + structure: ~10 messages
  - HTML tokenizer implementation: ~10-20 messages
  
Each subsequent session: ~20-30 messages per phase
