package main

import (
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func TestIntegration_SimpleHTMLToBoxes(t *testing.T) {
	htmlContent := `<div style="background-color: red; width: 100px; height: 100px;"></div>`

	// Parse HTML
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Verify parsing
	if len(doc.Root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(doc.Root.Children))
	}

	// Layout
	engine := layout.NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	// Verify layout
	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}
	if boxes[0].Width != 100.0 {
		t.Errorf("expected width=100, got %f", boxes[0].Width)
	}
	if boxes[0].Height != 100.0 {
		t.Errorf("expected height=100, got %f", boxes[0].Height)
	}

	// Verify style
	bgColor, ok := boxes[0].Style.Get("background-color")
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
	engine := layout.NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	// Verify
	if len(boxes) != 2 {
		t.Fatalf("expected 2 boxes, got %d", len(boxes))
	}

	// First box
	if boxes[0].Width != 200.0 {
		t.Errorf("box 0: expected width=200, got %f", boxes[0].Width)
	}
	if boxes[0].Y != 0.0 {
		t.Errorf("box 0: expected Y=0, got %f", boxes[0].Y)
	}

	// Second box
	if boxes[1].Width != 300.0 {
		t.Errorf("box 1: expected width=300, got %f", boxes[1].Width)
	}
	if boxes[1].Y != 100.0 {
		t.Errorf("box 1: expected Y=100, got %f", boxes[1].Y)
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

			engine := layout.NewLayoutEngine(800, 600)
			boxes := engine.Layout(doc)

			if len(boxes) != 1 {
				t.Fatalf("expected 1 box, got %d", len(boxes))
			}

			bgColor, ok := boxes[0].Style.Get("background-color")
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

	engine := layout.NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 0 {
		t.Errorf("expected 0 boxes for empty HTML, got %d", len(boxes))
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

	engine := layout.NewLayoutEngine(1024, 768)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	// Should use viewport width and default height
	if boxes[0].Width != 1024.0 {
		t.Errorf("expected width=1024 (viewport), got %f", boxes[0].Width)
	}
	if boxes[0].Height != 0.0 {
		t.Errorf("expected height=0 (auto, no children), got %f", boxes[0].Height)
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

	engine := layout.NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 50 {
		t.Fatalf("expected 50 boxes, got %d", len(boxes))
	}

	// Verify they stack correctly
	for i, box := range boxes {
		expectedY := float64(i * 20)
		if box.Y != expectedY {
			t.Errorf("box %d: expected Y=%f, got %f", i, expectedY, box.Y)
		}
	}

	// Should render without crashing
	renderer := render.NewRenderer(800, 600)
	renderer.Render(boxes)
}

func TestIntegration_ParseError(t *testing.T) {
	// Intentionally malformed HTML
	htmlContent := `<div style="unclosed`

	_, err := html.Parse(htmlContent)
	if err == nil {
		t.Error("expected parse error for malformed HTML")
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

			engine := layout.NewLayoutEngine(800, 600)
			boxes := engine.Layout(doc)

			if len(boxes) != 1 {
				t.Fatalf("expected 1 box, got %d", len(boxes))
			}

			// Just verify it doesn't crash and produces reasonable output
			renderer := render.NewRenderer(800, 600)
			renderer.Render(boxes)
		})
	}
}
