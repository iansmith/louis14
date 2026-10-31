package main

import (
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// divBoxes lays out doc and returns its <div> boxes in tree order. The box
// tree is rooted at <html> with a <body> child, as in Chrome, so tests can't
// index the top-level boxes directly.
func divBoxes(doc *html.Document, width, height float64) ([]*layout.Box, []*layout.Box) {
	boxes := layout.NewLayoutEngine(width, height).Layout(doc)
	var divs []*layout.Box
	var walk func(*layout.Box)
	walk = func(b *layout.Box) {
		if b.Node != nil && b.Node.TagName == "div" {
			divs = append(divs, b)
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
	return boxes, divs
}

// bodyMargin is the UA stylesheet's 8px body margin, which offsets every
// in-flow <div> in these documents.
const bodyMargin = 8.0

func TestIntegration_SimpleHTMLToBoxes(t *testing.T) {
	htmlContent := `<div style="background-color: red; width: 100px; height: 100px;"></div>`

	// Parse HTML
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Layout
	_, divs := divBoxes(doc, 800, 600)

	// Verify layout
	if len(divs) != 1 {
		t.Fatalf("expected 1 div box, got %d", len(divs))
	}
	if divs[0].X != bodyMargin || divs[0].Y != bodyMargin {
		t.Errorf("expected position=(%v,%v), got (%v,%v)", bodyMargin, bodyMargin, divs[0].X, divs[0].Y)
	}
	if divs[0].Width != 100.0 {
		t.Errorf("expected width=100, got %f", divs[0].Width)
	}
	if divs[0].Height != 100.0 {
		t.Errorf("expected height=100, got %f", divs[0].Height)
	}

	// Verify style
	bgColor, ok := divs[0].Style.Get("background-color")
	if !ok {
		t.Error("expected background-color to exist")
	}
	if bgColor != "red" {
		t.Errorf("expected background-color='red', got '%s'", bgColor)
	}
}

func TestIntegration_MultipleElements(t *testing.T) {
	htmlContent := `
		<div style="background-color: red; width: 200px; height: 100px;"></div>
		<div style="background-color: blue; width: 300px; height: 50px;"></div>
	`

	// Parse
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Layout
	_, divs := divBoxes(doc, 800, 600)

	// Verify
	if len(divs) != 2 {
		t.Fatalf("expected 2 div boxes, got %d", len(divs))
	}

	// First box
	if divs[0].Width != 200.0 {
		t.Errorf("box 0: expected width=200, got %f", divs[0].Width)
	}
	if divs[0].Y != bodyMargin {
		t.Errorf("box 0: expected Y=%v, got %f", bodyMargin, divs[0].Y)
	}

	// Second box
	if divs[1].Width != 300.0 {
		t.Errorf("box 1: expected width=300, got %f", divs[1].Width)
	}
	if divs[1].Y != bodyMargin+100 {
		t.Errorf("box 1: expected Y=%v, got %f", bodyMargin+100, divs[1].Y)
	}
}

func TestIntegration_EndToEndRender(t *testing.T) {
	htmlContent := `
		<div style="background-color: red; width: 200px; height: 100px;"></div>
		<div style="background-color: blue; width: 300px; height: 50px;"></div>
	`

	// Parse
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Layout
	engine := layout.NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	// Render
	renderer := render.NewRenderer(800, 600)
	renderer.Render(boxes)

	// Save to temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.png")
	err = renderer.SavePNG(tmpFile)
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Verify file exists and has content
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("file stat error: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-empty PNG file")
	}

	// Basic validation: PNG files should start with PNG magic bytes
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if len(content) < 8 {
		t.Fatal("file too small to be a valid PNG")
	}

	// Check PNG signature
	pngSignature := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	for i := 0; i < 8; i++ {
		if content[i] != pngSignature[i] {
			t.Errorf("byte %d: expected %d, got %d (not a valid PNG)", i, pngSignature[i], content[i])
		}
	}
}

func TestIntegration_AllNamedColors(t *testing.T) {
	colors := []string{
		"red", "green", "blue", "yellow", "cyan", "magenta",
		"white", "black", "gray", "orange", "purple", "pink",
	}

	for _, color := range colors {
		t.Run(color, func(t *testing.T) {
			htmlContent := `<div style="background-color: ` + color + `; width: 100px; height: 50px;"></div>`

			doc, err := html.Parse(htmlContent)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			_, divs := divBoxes(doc, 800, 600)

			if len(divs) != 1 {
				t.Fatalf("expected 1 div box, got %d", len(divs))
			}

			bgColor, ok := divs[0].Style.Get("background-color")
			if !ok {
				t.Error("expected background-color to exist")
			}
			if bgColor != color {
				t.Errorf("expected background-color='%s', got '%s'", color, bgColor)
			}
		})
	}
}

func TestIntegration_EmptyHTML(t *testing.T) {
	htmlContent := ""

	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	boxes, divs := divBoxes(doc, 800, 600)

	if len(divs) != 0 {
		t.Errorf("expected 0 div boxes for empty HTML, got %d", len(divs))
	}

	// Should still be able to render without crashing
	renderer := render.NewRenderer(800, 600)
	renderer.Render(boxes)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "empty.png")
	err = renderer.SavePNG(tmpFile)
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Should produce a valid PNG (white background)
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("file stat error: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-empty PNG file even for empty HTML")
	}
}

func TestIntegration_DefaultDimensions(t *testing.T) {
	htmlContent := `<div></div>`

	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	_, divs := divBoxes(doc, 1024, 768)

	if len(divs) != 1 {
		t.Fatalf("expected 1 div box, got %d", len(divs))
	}

	// Should fill the body's content box and have auto height
	if want := 1024 - 2*bodyMargin; divs[0].Width != want {
		t.Errorf("expected width=%v (viewport minus body margins), got %f", want, divs[0].Width)
	}
	if divs[0].Height != 0.0 {
		t.Errorf("expected height=0 (auto, no children), got %f", divs[0].Height)
	}
}

func TestIntegration_ManyBoxes(t *testing.T) {
	// Stress test: 50 boxes
	htmlContent := ""
	for i := 0; i < 50; i++ {
		htmlContent += `<div style="background-color: red; width: 100px; height: 20px;"></div>`
	}

	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	boxes, divs := divBoxes(doc, 800, 600)

	if len(divs) != 50 {
		t.Fatalf("expected 50 div boxes, got %d", len(divs))
	}

	// Verify they stack correctly
	for i, box := range divs {
		expectedY := bodyMargin + float64(i*20)
		if box.Y != expectedY {
			t.Errorf("box %d: expected Y=%f, got %f", i, expectedY, box.Y)
		}
	}

	// Should render without crashing
	renderer := render.NewRenderer(800, 600)
	renderer.Render(boxes)
}

func TestIntegration_MalformedHTMLRecovers(t *testing.T) {
	// HTML parsing never fails (HTML §13.2.2): a tag still open at EOF inside
	// an attribute value is dropped, exactly as Chrome does.
	htmlContent := `<div style="unclosed`

	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("expected malformed HTML to parse, got %v", err)
	}
	if _, divs := divBoxes(doc, 800, 600); len(divs) != 0 {
		t.Errorf("expected the unterminated tag to be dropped, got %d div boxes", len(divs))
	}
}

func TestIntegration_VariousSizes(t *testing.T) {
	tests := []struct {
		name   string
		width  string
		height string
	}{
		{"small", "10px", "10px"},
		{"medium", "100px", "100px"},
		{"large", "500px", "400px"},
		{"wide", "800px", "50px"},
		{"tall", "50px", "600px"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			htmlContent := `<div style="width: ` + tt.width + `; height: ` + tt.height + `;"></div>`

			doc, err := html.Parse(htmlContent)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			boxes, divs := divBoxes(doc, 800, 600)

			if len(divs) != 1 {
				t.Fatalf("expected 1 div box, got %d", len(divs))
			}

			// Just verify it doesn't crash and produces reasonable output
			renderer := render.NewRenderer(800, 600)
			renderer.Render(boxes)
		})
	}
}
