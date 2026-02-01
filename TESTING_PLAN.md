# Louis14 Testing Plan

## Testing Philosophy

Each subsystem should be testable in isolation AND as part of the integrated system. We use:
1. **Unit tests** - Test individual functions/components
2. **Integration tests** - Test components working together
3. **Visual regression tests** - Compare rendered output to expected images
4. **Manual tests** - Human review of visual output

---

## Phase 1 Testing Plan

### 1. HTML Tokenizer Tests

**Location**: `pkg/html/tokenizer_test.go`

**Test Cases**:

```go
func TestTokenizer_SimpleTag(t *testing.T) {
    // Test: <div>
    tokenizer := NewTokenizer("<div>")
    token, err := tokenizer.NextToken()
    
    assert.NoError(t, err)
    assert.Equal(t, TokenStartTag, token.Type)
    assert.Equal(t, "div", token.TagName)
}

func TestTokenizer_TagWithAttributes(t *testing.T) {
    // Test: <div style="color: red" id="main">
    tokenizer := NewTokenizer(`<div style="color: red" id="main">`)
    token, err := tokenizer.NextToken()
    
    assert.NoError(t, err)
    assert.Equal(t, TokenStartTag, token.Type)
    assert.Equal(t, "div", token.TagName)
    assert.Equal(t, "color: red", token.Attributes["style"])
    assert.Equal(t, "main", token.Attributes["id"])
}

func TestTokenizer_EndTag(t *testing.T) {
    // Test: </div>
    tokenizer := NewTokenizer("</div>")
    token, err := tokenizer.NextToken()
    
    assert.NoError(t, err)
    assert.Equal(t, TokenEndTag, token.Type)
    assert.Equal(t, "div", token.TagName)
}

func TestTokenizer_TextContent(t *testing.T) {
    // Test: Hello World
    tokenizer := NewTokenizer("Hello World")
    token, err := tokenizer.NextToken()
    
    assert.NoError(t, err)
    assert.Equal(t, TokenText, token.Type)
    assert.Equal(t, "Hello World", token.Text)
}

func TestTokenizer_CompleteSequence(t *testing.T) {
    // Test: <div>Hello</div>
    tokenizer := NewTokenizer("<div>Hello</div>")
    
    // First token: <div>
    token1, _ := tokenizer.NextToken()
    assert.Equal(t, TokenStartTag, token1.Type)
    
    // Second token: Hello
    token2, _ := tokenizer.NextToken()
    assert.Equal(t, TokenText, token2.Type)
    assert.Equal(t, "Hello", token2.Text)
    
    // Third token: </div>
    token3, _ := tokenizer.NextToken()
    assert.Equal(t, TokenEndTag, token3.Type)
    
    // Fourth token: EOF
    token4, _ := tokenizer.NextToken()
    assert.Equal(t, TokenEOF, token4.Type)
}

func TestTokenizer_QuotedAttributes(t *testing.T) {
    // Test both single and double quotes
    cases := []struct{
        html string
        expectedValue string
    }{
        {`<div style="red">`, "red"},
        {`<div style='red'>`, "red"},
        {`<div style="background: red; color: blue">`, "background: red; color: blue"},
    }
    
    for _, tc := range cases {
        tokenizer := NewTokenizer(tc.html)
        token, _ := tokenizer.NextToken()
        assert.Equal(t, tc.expectedValue, token.Attributes["style"])
    }
}

func TestTokenizer_SelfClosingTag(t *testing.T) {
    // Test: <br />
    tokenizer := NewTokenizer("<br />")
    token, err := tokenizer.NextToken()
    
    assert.NoError(t, err)
    assert.Equal(t, TokenStartTag, token.Type)
    assert.Equal(t, "br", token.TagName)
}

func TestTokenizer_ErrorCases(t *testing.T) {
    cases := []struct{
        name string
        html string
    }{
        {"unclosed tag", "<div"},
        {"unclosed attribute", `<div style="red`},
        {"empty tag", "<>"},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            tokenizer := NewTokenizer(tc.html)
            _, err := tokenizer.NextToken()
            assert.Error(t, err)
        })
    }
}
```

---

### 2. HTML Parser Tests

**Location**: `pkg/html/parser_test.go`

**Test Cases**:

```go
func TestParser_SingleElement(t *testing.T) {
    doc, err := Parse("<div></div>")
    
    assert.NoError(t, err)
    assert.Equal(t, 1, len(doc.Root.Children))
    assert.Equal(t, "div", doc.Root.Children[0].TagName)
}

func TestParser_MultipleElements(t *testing.T) {
    doc, err := Parse("<div></div><p></p><span></span>")
    
    assert.NoError(t, err)
    assert.Equal(t, 3, len(doc.Root.Children))
    assert.Equal(t, "div", doc.Root.Children[0].TagName)
    assert.Equal(t, "p", doc.Root.Children[1].TagName)
    assert.Equal(t, "span", doc.Root.Children[2].TagName)
}

func TestParser_ElementsWithAttributes(t *testing.T) {
    doc, err := Parse(`<div style="color: red" id="main"></div>`)
    
    assert.NoError(t, err)
    node := doc.Root.Children[0]
    
    style, ok := node.GetAttribute("style")
    assert.True(t, ok)
    assert.Equal(t, "color: red", style)
    
    id, ok := node.GetAttribute("id")
    assert.True(t, ok)
    assert.Equal(t, "main", id)
}

func TestParser_TextContent(t *testing.T) {
    doc, err := Parse("<div>Hello World</div>")
    
    assert.NoError(t, err)
    // Phase 1: text is a separate node
    assert.Equal(t, 2, len(doc.Root.Children))
    assert.Equal(t, ElementNode, doc.Root.Children[0].Type)
    assert.Equal(t, TextNode, doc.Root.Children[1].Type)
    assert.Equal(t, "Hello World", doc.Root.Children[1].Text)
}
```

---

### 3. CSS Parser Tests

**Location**: `pkg/css/style_test.go`

**Test Cases**:

```go
func TestParseInlineStyle_SingleProperty(t *testing.T) {
    style := ParseInlineStyle("color: red")
    
    value, ok := style.Get("color")
    assert.True(t, ok)
    assert.Equal(t, "red", value)
}

func TestParseInlineStyle_MultipleProperties(t *testing.T) {
    style := ParseInlineStyle("color: red; background-color: blue; width: 100px")
    
    color, _ := style.Get("color")
    assert.Equal(t, "red", color)
    
    bg, _ := style.Get("background-color")
    assert.Equal(t, "blue", bg)
    
    width, _ := style.Get("width")
    assert.Equal(t, "100px", width)
}

func TestParseInlineStyle_Whitespace(t *testing.T) {
    style := ParseInlineStyle("  color:  red  ;  width:  100px  ")
    
    color, _ := style.Get("color")
    assert.Equal(t, "red", color)
    
    width, _ := style.Get("width")
    assert.Equal(t, "100px", width)
}

func TestGetLength_PixelValues(t *testing.T) {
    style := ParseInlineStyle("width: 100px; height: 50px")
    
    width, ok := style.GetLength("width")
    assert.True(t, ok)
    assert.Equal(t, 100.0, width)
    
    height, ok := style.GetLength("height")
    assert.True(t, ok)
    assert.Equal(t, 50.0, height)
}

func TestGetLength_MissingProperty(t *testing.T) {
    style := ParseInlineStyle("color: red")
    
    _, ok := style.GetLength("width")
    assert.False(t, ok)
}

func TestParseColor_NamedColors(t *testing.T) {
    cases := []struct{
        name string
        expected Color
    }{
        {"red", Color{255, 0, 0}},
        {"blue", Color{0, 0, 255}},
        {"green", Color{0, 128, 0}},
        {"white", Color{255, 255, 255}},
        {"black", Color{0, 0, 0}},
    }
    
    for _, tc := range cases {
        color, ok := ParseColor(tc.name)
        assert.True(t, ok)
        assert.Equal(t, tc.expected, color)
    }
}

func TestParseColor_CaseInsensitive(t *testing.T) {
    cases := []string{"RED", "Red", "red", "rEd"}
    
    for _, colorStr := range cases {
        color, ok := ParseColor(colorStr)
        assert.True(t, ok)
        assert.Equal(t, Color{255, 0, 0}, color)
    }
}

func TestParseColor_Invalid(t *testing.T) {
    _, ok := ParseColor("notacolor")
    assert.False(t, ok)
}
```

---

### 4. Layout Engine Tests

**Location**: `pkg/layout/layout_test.go`

**Test Cases**:

```go
func TestLayoutEngine_SingleBox(t *testing.T) {
    html := `<div style="width: 200px; height: 100px;"></div>`
    doc, _ := html.Parse(html)
    
    engine := NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    assert.Equal(t, 1, len(boxes))
    assert.Equal(t, 0.0, boxes[0].X)
    assert.Equal(t, 0.0, boxes[0].Y)
    assert.Equal(t, 200.0, boxes[0].Width)
    assert.Equal(t, 100.0, boxes[0].Height)
}

func TestLayoutEngine_MultipleBoxes(t *testing.T) {
    html := `
        <div style="width: 200px; height: 100px;"></div>
        <div style="width: 300px; height: 50px;"></div>
    `
    doc, _ := html.Parse(html)
    
    engine := NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    assert.Equal(t, 2, len(boxes))
    
    // First box
    assert.Equal(t, 0.0, boxes[0].Y)
    assert.Equal(t, 100.0, boxes[0].Height)
    
    // Second box (should be below first)
    assert.Equal(t, 100.0, boxes[1].Y)
    assert.Equal(t, 50.0, boxes[1].Height)
}

func TestLayoutEngine_DefaultWidth(t *testing.T) {
    html := `<div style="height: 100px;"></div>`
    doc, _ := html.Parse(html)
    
    engine := NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    // Should default to viewport width
    assert.Equal(t, 800.0, boxes[0].Width)
}

func TestLayoutEngine_DefaultHeight(t *testing.T) {
    html := `<div style="width: 200px;"></div>`
    doc, _ := html.Parse(html)
    
    engine := NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    // Should default to 50px
    assert.Equal(t, 50.0, boxes[0].Height)
}
```

---

### 5. Integration Tests

**Location**: `integration_test.go`

**Test Cases**:

```go
func TestIntegration_SimpleRendering(t *testing.T) {
    html := `<div style="background-color: red; width: 100px; height: 100px;"></div>`
    
    // Parse
    doc, err := html.Parse(html)
    assert.NoError(t, err)
    
    // Layout
    engine := layout.NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    // Verify layout
    assert.Equal(t, 1, len(boxes))
    assert.Equal(t, 100.0, boxes[0].Width)
    assert.Equal(t, 100.0, boxes[0].Height)
    
    // Verify style
    bgColor, ok := boxes[0].Style.Get("background-color")
    assert.True(t, ok)
    assert.Equal(t, "red", bgColor)
}

func TestIntegration_EndToEnd(t *testing.T) {
    // This test actually renders and saves a PNG
    html := `
        <div style="background-color: red; width: 200px; height: 100px;"></div>
        <div style="background-color: blue; width: 300px; height: 50px;"></div>
    `
    
    // Parse
    doc, err := html.Parse(html)
    assert.NoError(t, err)
    
    // Layout
    engine := layout.NewLayoutEngine(800, 600)
    boxes := engine.Layout(doc)
    
    // Render
    renderer := render.NewRenderer(800, 600)
    renderer.Render(boxes)
    
    // Save to temp file
    tmpfile := filepath.Join(t.TempDir(), "test.png")
    err = renderer.SavePNG(tmpfile)
    assert.NoError(t, err)
    
    // Verify file exists and has content
    info, err := os.Stat(tmpfile)
    assert.NoError(t, err)
    assert.Greater(t, info.Size(), int64(0))
}
```

---

### 6. Visual Regression Tests

**Location**: `testdata/phase1/` + `visual_test.go`

**Approach**:
1. Create reference images for known-good outputs
2. Render test cases
3. Compare pixel-by-pixel (or use perceptual diff)

**Test Structure**:

```go
func TestVisualRegression_SimpleBoxes(t *testing.T) {
    cases := []struct{
        name string
        htmlFile string
        refImage string
    }{
        {"simple boxes", "testdata/phase1/simple.html", "testdata/phase1/simple_ref.png"},
        {"different colors", "testdata/phase1/colors.html", "testdata/phase1/colors_ref.png"},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            // Render test case
            htmlContent, _ := os.ReadFile(tc.htmlFile)
            doc, _ := html.Parse(string(htmlContent))
            engine := layout.NewLayoutEngine(800, 600)
            boxes := engine.Layout(doc)
            renderer := render.NewRenderer(800, 600)
            renderer.Render(boxes)
            
            // Save to temp file
            tmpfile := filepath.Join(t.TempDir(), "output.png")
            renderer.SavePNG(tmpfile)
            
            // Compare with reference
            match := compareImages(tmpfile, tc.refImage)
            assert.True(t, match, "Output doesn't match reference image")
        })
    }
}

func compareImages(img1, img2 string) bool {
    // Load both images
    file1, _ := os.Open(img1)
    defer file1.Close()
    file2, _ := os.Open(img2)
    defer file2.Close()
    
    decoded1, _ := png.Decode(file1)
    decoded2, _ := png.Decode(file2)
    
    // Compare dimensions
    bounds1 := decoded1.Bounds()
    bounds2 := decoded2.Bounds()
    if bounds1 != bounds2 {
        return false
    }
    
    // Compare pixels (with small tolerance for rendering differences)
    tolerance := 2 // Allow 2-point color difference
    for y := bounds1.Min.Y; y < bounds1.Max.Y; y++ {
        for x := bounds1.Min.X; x < bounds1.Max.X; x++ {
            r1, g1, b1, a1 := decoded1.At(x, y).RGBA()
            r2, g2, b2, a2 := decoded2.At(x, y).RGBA()
            
            if abs(int(r1)-int(r2)) > tolerance ||
               abs(int(g1)-int(g2)) > tolerance ||
               abs(int(b1)-int(b2)) > tolerance ||
               abs(int(a1)-int(a2)) > tolerance {
                return false
            }
        }
    }
    
    return true
}
```

---

### 7. Manual Test Cases

**Location**: `testdata/phase1/manual_tests/`

**Test Files to Create**:

1. **simple.html** - Basic colored boxes (already created)
2. **all_colors.html** - Test all named colors
3. **sizes.html** - Various widths and heights
4. **edge_cases.html** - Tiny boxes, huge boxes, zero-sized boxes
5. **stress_test.html** - Many boxes (100+)

**Manual Testing Checklist**:
- [ ] Colors render correctly
- [ ] Box sizes match specified dimensions
- [ ] Boxes stack vertically without gaps/overlaps
- [ ] White background renders
- [ ] Large numbers of boxes render without crashing
- [ ] Edge cases (0px, 9999px) handle gracefully

---

## Running Tests

### Unit Tests
```bash
cd ~/louis14
go test ./pkg/html/... -v
go test ./pkg/css/... -v
go test ./pkg/layout/... -v
```

### Integration Tests
```bash
go test ./... -v
```

### Visual Tests
```bash
# Generate test outputs
for file in testdata/phase1/*.html; do
    go run cmd/louis14/main.go "$file" "output/$(basename $file .html).png"
done

# Review outputs manually
open output/*.png
```

### Benchmark Tests
```bash
go test -bench=. ./...
```

---

## Test Coverage Goals

- **Phase 1**: 70%+ code coverage for core tokenizer/parser
- **Phase 2+**: 80%+ code coverage
- **Critical paths**: 100% coverage (tokenizer, layout calculations)

---

## Continuous Testing Strategy

After each session:
1. Run all unit tests
2. Generate visual outputs for all test cases
3. Manual review of new test cases
4. Update reference images if behavior intentionally changed
5. Add regression test for any bugs found

---

## Known Limitations to Test

Phase 1 limitations that should be documented in tests:
- ✗ No nested elements (flattened)
- ✗ No margin/border/padding
- ✗ No actual text content rendering (just tag names)
- ✗ No error recovery
- ✗ No whitespace handling
- ✗ Font must exist at hardcoded path

These become test cases for Phase 2!
