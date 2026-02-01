package text

import (
	"github.com/fogleman/gg"
)

// DefaultFontPath is the path to the default font
const DefaultFontPath = "/Users/iansmith/louis14/fonts/AtkinsonHyperlegible-Regular.ttf"

// BoldFontPath is the path to the bold font
const BoldFontPath = "/Users/iansmith/louis14/fonts/AtkinsonHyperlegible-Bold.ttf"

// MeasureText measures the width and height of text with the given font size
func MeasureText(text string, fontSize float64, fontPath string) (width, height float64) {
	// Use a temporary context for measurement
	dc := gg.NewContext(1000, 1000)

	// Load the font
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		// If font loading fails, return rough estimate
		return float64(len(text)) * fontSize * 0.6, fontSize * 1.2
	}

	// Measure the text
	w, h := dc.MeasureString(text)

	// Add some padding to height for proper baseline alignment
	return w, h
}

// MeasureTextDefault measures text using the default font
func MeasureTextDefault(text string, fontSize float64) (width, height float64) {
	return MeasureText(text, fontSize, DefaultFontPath)
}

// MeasureTextWithWeight measures text using the specified font weight
func MeasureTextWithWeight(text string, fontSize float64, bold bool) (width, height float64) {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}
	return MeasureText(text, fontSize, fontPath)
}

// Phase 6 Enhancement: BreakTextIntoLines breaks text into lines that fit within maxWidth
func BreakTextIntoLines(text string, fontSize float64, bold bool, maxWidth float64) []string {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}

	// Use a temporary context for measurement
	dc := gg.NewContext(1000, 1000)
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		// If font loading fails, return text as single line
		return []string{text}
	}

	// Check if text fits on one line
	textWidth, _ := dc.MeasureString(text)
	if textWidth <= maxWidth {
		return []string{text}
	}

	// Split into words
	words := splitIntoWords(text)
	if len(words) == 0 {
		return []string{text}
	}

	// Build lines
	lines := make([]string, 0)
	currentLine := ""

	for _, word := range words {
		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		lineWidth, _ := dc.MeasureString(testLine)
		if lineWidth <= maxWidth {
			currentLine = testLine
		} else {
			// Word doesn't fit, start new line
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		}
	}

	// Add last line
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	if len(lines) == 0 {
		return []string{text}
	}

	return lines
}

// splitIntoWords splits text into words preserving spaces
func splitIntoWords(text string) []string {
	words := make([]string, 0)
	currentWord := ""

	for _, ch := range text {
		if ch == ' ' || ch == '\t' || ch == '\n' {
			if currentWord != "" {
				words = append(words, currentWord)
				currentWord = ""
			}
		} else {
			currentWord += string(ch)
		}
	}

	if currentWord != "" {
		words = append(words, currentWord)
	}

	return words
}
