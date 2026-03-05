package text

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/fogleman/gg"
)

// measureCache caches text measurement results to avoid repeated font loading.
// Key: "text\x00fontSize\x00fontPath", Value: [2]float64{width, height}
var measureCache sync.Map

func measureCacheKey(text string, fontSize float64, fontPath string) string {
	return fmt.Sprintf("%s\x00%.4f\x00%s", text, fontSize, fontPath)
}

// FontConfig holds paths to font files used for text measurement and rendering.
type FontConfig struct {
	Regular     string
	Bold        string
	Italic      string
	BoldItalic  string
	Monospace   string
	MonoBold    string
	Ahem        string         // Special test font where all glyphs are 1em x 1em squares
	Registry    *FontRegistry  // Optional web font registry for @font-face fonts
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

// FontPathForFamily returns the font path for a CSS font-family string.
// It parses comma-separated font-family lists and tries each family in order,
// checking the web font registry and built-in aliases before falling back.
func (fc FontConfig) FontPathForFamily(family string, bold, italic, mono, ahem bool) string {
	families := parseFontFamilyList(family)

	for _, fam := range families {
		// Check web font registry
		if fc.Registry != nil {
			if path := fc.Registry.Lookup(fam, bold, italic); path != "" {
				return path
			}
		}
		// Check bundled font aliases
		if path := fc.resolveBuiltinFamily(fam, bold, italic); path != "" {
			return path
		}
	}
	// Final fallback to default bundled font
	return fc.FontPath(bold, italic, mono, ahem)
}

// parseFontFamilyList splits a CSS font-family value into individual family names.
// e.g. `"Helvetica Neue", Arial, sans-serif` → ["helvetica neue", "arial", "sans-serif"]
func parseFontFamilyList(raw string) []string {
	var families []string
	for _, part := range strings.Split(raw, ",") {
		fam := strings.TrimSpace(part)
		// Strip surrounding quotes
		if len(fam) >= 2 && ((fam[0] == '"' && fam[len(fam)-1] == '"') || (fam[0] == '\'' && fam[len(fam)-1] == '\'')) {
			fam = fam[1 : len(fam)-1]
		}
		fam = strings.TrimSpace(fam)
		if fam != "" {
			families = append(families, fam)
		}
	}
	return families
}

// resolveBuiltinFamily maps well-known CSS font family names to bundled Liberation fonts.
func (fc FontConfig) resolveBuiltinFamily(family string, bold, italic bool) string {
	dir := defaultFontsDir()
	lower := strings.ToLower(family)

	switch lower {
	case "helvetica", "helvetica neue", "arial",
		"liberation sans", "nimbus sans", "sans-serif":
		return liberationSansPath(dir, bold, italic)
	case "times", "times new roman", "liberation serif", "serif":
		return liberationSerifPath(dir, bold, italic)
	case "courier", "courier new", "liberation mono", "monospace":
		return liberationMonoPath(dir, bold, italic)
	}
	return ""
}

func liberationSansPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "LiberationSans-BoldItalic.ttf")
	case bold:
		return filepath.Join(dir, "LiberationSans-Bold.ttf")
	case italic:
		return filepath.Join(dir, "LiberationSans-Italic.ttf")
	default:
		return filepath.Join(dir, "LiberationSans-Regular.ttf")
	}
}

func liberationSerifPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "LiberationSerif-BoldItalic.ttf")
	case bold:
		return filepath.Join(dir, "LiberationSerif-Bold.ttf")
	case italic:
		return filepath.Join(dir, "LiberationSerif-Italic.ttf")
	default:
		return filepath.Join(dir, "LiberationSerif-Regular.ttf")
	}
}

func liberationMonoPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "LiberationMono-BoldItalic.ttf")
	case bold:
		return filepath.Join(dir, "LiberationMono-Bold.ttf")
	case italic:
		return filepath.Join(dir, "LiberationMono-Italic.ttf")
	default:
		return filepath.Join(dir, "LiberationMono-Regular.ttf")
	}
}

// DefaultFontPath is the path to the default font.
// Deprecated: use DefaultFontConfig() instead.
var DefaultFontPath = DefaultFontConfig().Regular

// BoldFontPath is the path to the bold font.
// Deprecated: use DefaultFontConfig() instead.
var BoldFontPath = DefaultFontConfig().Bold

// MeasureText measures the width and height of text with the given font size.
// Results are cached by (text, fontSize, fontPath) to avoid repeated font loading.
func MeasureText(text string, fontSize float64, fontPath string) (width, height float64) {
	key := measureCacheKey(text, fontSize, fontPath)
	if v, ok := measureCache.Load(key); ok {
		dims := v.([2]float64)
		return dims[0], dims[1]
	}

	// Use a temporary context for measurement
	dc := gg.NewContext(1000, 1000)

	// Load the font
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		// If font loading fails, return rough estimate
		w := float64(len(text)) * fontSize * 0.6
		h := fontSize * 1.2
		measureCache.Store(key, [2]float64{w, h})
		return w, h
	}

	// Measure the text and snap to integer pixels.
	// Real browsers quantize character advance widths to integer pixels via
	// font hinting, ensuring text positions stay on the integer grid.
	// Without this, separate DrawString calls for adjacent text (e.g. table
	// cells vs continuous strings) produce different sub-pixel anti-aliasing.
	w, h := dc.MeasureString(text)
	w = math.Round(w)
	if w < 1 && len(text) > 0 {
		w = 1
	}
	h = math.Round(h)
	if h < 1 {
		h = 1
	}
	measureCache.Store(key, [2]float64{w, h})
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

// ascentCache caches font ascent results to avoid repeated font loading.
// Key: "fontSize\x00fontPath", Value: float64 ascent
var ascentCache sync.Map

// FontAscent returns the font ascent in pixels (distance from baseline to top of glyph)
// for the given font size and style parameters. This is used for baseline alignment.
func FontAscent(fontSize float64, bold, italic, mono, ahem bool) float64 {
	fontConfig := DefaultFontConfig()
	fontPath := fontConfig.FontPath(bold, italic, mono, ahem)
	key := fmt.Sprintf("%.4f\x00%s", fontSize, fontPath)
	if v, ok := ascentCache.Load(key); ok {
		return v.(float64)
	}
	dc := gg.NewContext(1, 1)
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		// Fall back to 0.8 * fontSize (reasonable approximation)
		ascent := fontSize * 0.8
		ascentCache.Store(key, ascent)
		return ascent
	}
	ascent := dc.FontAscent()
	ascentCache.Store(key, ascent)
	return ascent
}

// BreakTextAtCharacterBoundary splits text at a character boundary so the prefix fits within maxWidth.
// Returns (prefix, remainder). If no character fits, prefix is empty and remainder is the full text.
func BreakTextAtCharacterBoundary(textStr string, fontSize float64, bold, italic, mono, ahem bool, maxWidth float64) (string, string) {
	fontConfig := DefaultFontConfig()
	fontPath := fontConfig.FontPath(bold, italic, mono, ahem)

	dc := gg.NewContext(1000, 1000)
	if err := dc.LoadFontFace(fontPath, fontSize); err != nil {
		return "", textStr
	}

	runes := []rune(textStr)
	bestLen := 0
	for i := 1; i <= len(runes); i++ {
		prefix := string(runes[:i])
		w, _ := dc.MeasureString(prefix)
		if w > maxWidth {
			break
		}
		bestLen = i
	}

	if bestLen == 0 {
		return "", textStr
	}
	return string(runes[:bestLen]), string(runes[bestLen:])
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
