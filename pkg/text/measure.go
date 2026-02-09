package text

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/fogleman/gg"
)

// FontConfig holds paths to font files used for text measurement and rendering.
type FontConfig struct {
	Regular     string
	Bold        string
	Italic      string
	BoldItalic  string
	Monospace   string
	MonoBold    string
	Ahem        string // Special test font where all glyphs are 1em x 1em squares
}

// defaultFontsDir returns the fonts directory relative to this source file.
func defaultFontsDir() string {
	// Try relative to executable first
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "fonts")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	// Fall back to compile-time source location
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "fonts")
}

// DefaultFontConfig returns a FontConfig using the bundled Atkinson Hyperlegible fonts.
func DefaultFontConfig() FontConfig {
	dir := defaultFontsDir()
	return FontConfig{
		Regular:    filepath.Join(dir, "AtkinsonHyperlegible-Regular.ttf"),
		Bold:       filepath.Join(dir, "AtkinsonHyperlegible-Bold.ttf"),
		Italic:     filepath.Join(dir, "AtkinsonHyperlegible-Italic.ttf"),
		BoldItalic: filepath.Join(dir, "AtkinsonHyperlegible-BoldItalic.ttf"),
		Monospace:  filepath.Join(dir, "AtkinsonHyperlegibleMono-Regular.otf"),
		MonoBold:   filepath.Join(dir, "AtkinsonHyperlegibleMono-Bold.otf"),
		Ahem:       filepath.Join(dir, "Ahem.ttf"),
	}
}

// FontPath returns the font path for the given style combination.
func (fc FontConfig) FontPath(bold, italic, mono, ahem bool) string {
	// Ahem font takes precedence over all other fonts
	if ahem && fc.Ahem != "" {
		return fc.Ahem
	}
	if mono {
		if bold && fc.MonoBold != "" {
			return fc.MonoBold
		}
		if fc.Monospace != "" {
			return fc.Monospace
		}
		// fall through to proportional if no mono font configured
	}
	if bold && italic && fc.BoldItalic != "" {
		return fc.BoldItalic
	}
	if bold {
		return fc.Bold
	}
	if italic && fc.Italic != "" {
		return fc.Italic
	}
	return fc.Regular
}

// DefaultFontPath is the path to the default font.
// Deprecated: use DefaultFontConfig() instead.
var DefaultFontPath = DefaultFontConfig().Regular

// BoldFontPath is the path to the bold font.
// Deprecated: use DefaultFontConfig() instead.
var BoldFontPath = DefaultFontConfig().Bold

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

// MeasureTextWithStyle measures text using the specified font style (bold, italic, mono, ahem).
// This is the comprehensive text measurement function that respects all font-family properties.
func MeasureTextWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool) (width, height float64) {
	fontConfig := DefaultFontConfig()
	fontPath := fontConfig.FontPath(bold, italic, mono, ahem)
	return MeasureText(text, fontSize, fontPath)
}

// Phase 6 Enhancement: BreakTextIntoLines breaks text into lines that fit within maxWidth
func BreakTextIntoLines(text string, fontSize float64, bold bool, maxWidth float64) []string {
	return BreakTextIntoLinesWithWrap(text, fontSize, bold, maxWidth, maxWidth)
}

// BreakTextIntoLinesWithWrap breaks text into lines where the first line fits
// within firstLineMax and subsequent lines fit within remainingMax.
// This handles the case where text starts partway through a line (e.g., after
// an inline element) but subsequent lines use the full container width.
func BreakTextIntoLinesWithWrap(text string, fontSize float64, bold bool, firstLineMax, remainingMax float64) []string {
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

	// Check if text fits on first line
	textWidth, _ := dc.MeasureString(text)
	if textWidth <= firstLineMax {
		return []string{text}
	}

	// Preserve leading whitespace — important for inline flow where
	// a text node like " more text" follows an inline element.
	leadingSpace := ""
	if len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n') {
		leadingSpace = " "
	}

	// Split into words
	words := splitIntoWords(text)
	if len(words) == 0 {
		return []string{text}
	}

	// Build lines
	lines := make([]string, 0)
	currentLine := ""
	lineNum := 0

	for i, word := range words {
		// Prepend leading space to first word if original text had leading whitespace
		if i == 0 && leadingSpace != "" {
			word = leadingSpace + word
		}

		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		maxWidth := remainingMax
		if lineNum == 0 {
			maxWidth = firstLineMax
		}

		lineWidth, _ := dc.MeasureString(testLine)
		if lineWidth <= maxWidth {
			currentLine = testLine
		} else {
			// Word doesn't fit, start new line
			if currentLine != "" {
				lines = append(lines, currentLine)
				lineNum++
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

// GetFirstWord returns the first word of the text (skipping leading whitespace)
func GetFirstWord(text string) string {
	words := splitIntoWords(text)
	if len(words) > 0 {
		return words[0]
	}
	return ""
}

// BreakTextIntoLinesWithStyle breaks text into lines using the specified font style.
// This is the comprehensive line-breaking function that respects all font-family properties.
func BreakTextIntoLinesWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool, firstLineMax, remainingMax float64) []string {
	fontConfig := DefaultFontConfig()
	fontPath := fontConfig.FontPath(bold, italic, mono, ahem)

	// Use a temporary context for measurement
	dc := gg.NewContext(1000, 1000)
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		// If font loading fails, return text as single line
		return []string{text}
	}

	// Check if text fits on first line
	textWidth, _ := dc.MeasureString(text)
	if textWidth <= firstLineMax {
		return []string{text}
	}

	// Preserve leading whitespace
	leadingSpace := ""
	if len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n') {
		leadingSpace = " "
	}

	// Split into words
	words := splitIntoWords(text)
	if len(words) == 0 {
		return []string{text}
	}

	// Build lines
	lines := make([]string, 0)
	currentLine := ""
	lineNum := 0

	for i, word := range words {
		// Prepend leading space to first word if original text had leading whitespace
		if i == 0 && leadingSpace != "" {
			word = leadingSpace + word
		}

		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		maxWidth := remainingMax
		if lineNum == 0 {
			maxWidth = firstLineMax
		}

		lineWidth, _ := dc.MeasureString(testLine)
		if lineWidth <= maxWidth {
			currentLine = testLine
		} else {
			// Word doesn't fit, start new line
			if currentLine != "" {
				lines = append(lines, currentLine)
				lineNum++
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
