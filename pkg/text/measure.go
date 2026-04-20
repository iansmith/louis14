package text

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"mazarin/textshape"
)

// measureCache caches text advance widths keyed on "text\x00fontSize\x00fontPath".
var measureCache sync.Map

func measureCacheKey(text string, fontSize float64, fontPath string) string {
	return fmt.Sprintf("%s\x00%.4f\x00%s", text, fontSize, fontPath)
}

// sharedLayout is the package-level TextLayout used for all measurement.
// Set via SetTextLayout (called by the renderer after creating its DrawContext)
// or lazily initialized on first use. Safe for concurrent use.
var (
	sharedLayout   textshape.TextLayout
	sharedLayoutMu sync.Mutex
)

func getLayout() textshape.TextLayout {
	sharedLayoutMu.Lock()
	defer sharedLayoutMu.Unlock()
	if sharedLayout == nil {
		sharedLayout = textshape.NewTextLayout(defaultFontsDir())
	}
	return sharedLayout
}

// SetTextLayout sets the shared TextLayout used for all text measurement.
// Must be called by the renderer after creating its DrawContext so that
// layout measurement and paint rendering share the same engine, font cache,
// and shape cache. Resets all derived caches.
func SetTextLayout(tl textshape.TextLayout) {
	sharedLayoutMu.Lock()
	sharedLayout = tl
	sharedLayoutMu.Unlock()
	fontIDCacheMu.Lock()
	fontIDCache = make(map[fontIDKey]textshape.FontMetrics)
	fontIDCacheMu.Unlock()
	measureCache = sync.Map{}
}

// fontIDCache caches (fontPath, sizeInt32) → FontMetrics to avoid re-opening fonts.
var (
	fontIDCache   = make(map[fontIDKey]textshape.FontMetrics)
	fontIDCacheMu sync.Mutex
)

type fontIDKey struct {
	path string
	size int32
}

// fontPathToFamilyVariant extracts a logical family name and variant from a
// font file path. For example:
//
//	"/.../AtkinsonHyperlegible-Bold.ttf" → ("AtkinsonHyperlegible", VariantBold)
//	"/.../Ahem.ttf" → ("Ahem", VariantRegular)
func fontPathToFamilyVariant(fontPath string) (string, int32) {
	base := filepath.Base(fontPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	variant := int32(textshape.VariantRegular)
	family := name

	if idx := strings.LastIndex(name, "-"); idx > 0 {
		suffix := strings.ToLower(name[idx+1:])
		family = name[:idx]
		switch suffix {
		case "bold":
			variant = textshape.VariantBold
		case "italic":
			variant = textshape.VariantItalic
		case "bolditalic":
			variant = textshape.VariantBoldItalic
		case "light":
			variant = textshape.VariantLight
		case "condensed":
			variant = textshape.VariantCondensed
		case "regular":
			variant = textshape.VariantRegular
		default:
			// Unknown suffix — treat entire name as family.
			family = name
		}
	}
	// For absolute paths (e.g., @font-face web fonts cached in temp dirs with
	// hash-based filenames), return the full path as the family so that
	// resolveFamily() can use it directly via its filepath.IsAbs() check.
	if filepath.IsAbs(fontPath) {
		family = fontPath
	}

	return family, variant
}

// openFont returns the FontMetrics for the given path+size, opening it if needed.
// Returns zero FontMetrics with FontID=-1 on error.
func openFont(fontPath string, fontSize float64) textshape.FontMetrics {
	size := int32(math.Round(fontSize))
	key := fontIDKey{path: fontPath, size: size}
	fontIDCacheMu.Lock()
	defer fontIDCacheMu.Unlock()
	if m, ok := fontIDCache[key]; ok {
		return m
	}
	family, variant := fontPathToFamilyVariant(fontPath)
	metrics, err := getLayout().OpenFont(textshape.OpenFontRequest{
		Family:  family,
		Variant: variant,
		Size:    size,
	})
	if err != nil {
		return textshape.FontMetrics{FontID: -1}
	}
	fontIDCache[key] = metrics
	return metrics
}

// FontConfig holds paths to font files used for text measurement and rendering.
type FontConfig struct {
	Regular    string
	Bold       string
	Italic     string
	BoldItalic string
	Monospace  string
	MonoBold   string
	Ahem       string        // Special test font where all glyphs are 1em x 1em squares
	Registry   *FontRegistry // Optional web font registry for @font-face fonts
}

// DefaultFontsDir returns the fonts directory relative to this source file.
func DefaultFontsDir() string {
	return defaultFontsDir()
}

func defaultFontsDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "fonts")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
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
func (fc FontConfig) FontPathForFamily(family string, bold, italic, mono, ahem bool) string {
	families := parseFontFamilyList(family)
	for _, fam := range families {
		if fc.Registry != nil {
			if path := fc.Registry.Lookup(fam, bold, italic); path != "" {
				return path
			}
		}
		if path := fc.resolveBuiltinFamily(fam, bold, italic); path != "" {
			return path
		}
	}
	return fc.FontPath(bold, italic, mono, ahem)
}

func parseFontFamilyList(raw string) []string {
	var families []string
	for _, part := range strings.Split(raw, ",") {
		fam := strings.TrimSpace(part)
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

func (fc FontConfig) resolveBuiltinFamily(family string, bold, italic bool) string {
	dir := defaultFontsDir()
	switch strings.ToLower(family) {
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

var DefaultFontPath = DefaultFontConfig().Regular
var BoldFontPath = DefaultFontConfig().Bold

// measureWidth returns the advance width of text for a given font path+size,
// using the shared TextLayout (HarfBuzz shaping) with a sync.Map cache.
func measureWidth(text string, fontSize float64, fontPath string) float64 {
	key := measureCacheKey(text, fontSize, fontPath)
	if v, ok := measureCache.Load(key); ok {
		return v.(float64)
	}
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		w := math.Round(float64(len(text)) * fontSize * 0.6)
		measureCache.Store(key, w)
		return w
	}
	adv, err := getLayout().MeasureText(textshape.ShapingParams{
		Text:   text,
		FontID: m.FontID,
	})
	var w float64
	if err != nil {
		w = math.Floor(float64(len(text)) * fontSize * 0.6)
	} else {
		// Return the exact sub-pixel advance (no rounding).
		// Layout accumulates these as float64, so adjacent text boxes are
		// positioned at exact fractional-pixel boundaries. DrawText then
		// decomposes each box's X into floor(X) + frac(X)*64 as StartPenX,
		// producing glyph positions identical to a single combined run.
		w = float64(adv) / 64.0
		if w < 1 && len(text) > 0 {
			w = 1
		}
	}
	measureCache.Store(key, w)
	return w
}

// MeasureText measures the width and height of text with the given font.
func MeasureText(text string, fontSize float64, fontPath string) (width, height float64) {
	w := measureWidth(text, fontSize, fontPath)
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		return w, math.Round(float64(m.Height) / 64.0)
	}
	return w, math.Round(fontSize * 1.2)
}

// MeasureTextDefault measures text using the default font.
func MeasureTextDefault(text string, fontSize float64) (width, height float64) {
	return MeasureText(text, fontSize, DefaultFontPath)
}

// MeasureTextWithWeight measures text using the specified font weight.
func MeasureTextWithWeight(text string, fontSize float64, bold bool) (width, height float64) {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}
	return MeasureText(text, fontSize, fontPath)
}

// MeasureTextWithStyle measures text with the full style combination.
func MeasureTextWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool) (width, height float64) {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	return MeasureText(text, fontSize, fontPath)
}

// MeasureTextVertical returns the inline advance of text in a vertical writing
// mode. For upright text (the default for Ahem and CJK), each glyph advances
// by fontSize in the inline (vertical) direction. For sideways text, the
// inline advance equals the horizontal advance (rotated glyphs keep their
// horizontal width as the inline advance).
//
// CSS Writing Modes §5.1: text-orientation determines whether glyphs are
// upright or sideways. For now, this treats all characters as upright,
// which is correct for Ahem (1em × 1em squares) and CJK text.
func MeasureTextVertical(text string, fontSize float64, bold, italic, mono, ahem bool) (inlineAdvance, blockAdvance float64) {
	runeCount := utf8.RuneCountInString(text)
	// Upright: each glyph advances by fontSize in the inline direction.
	inlineAdvance = float64(runeCount) * fontSize
	// Block advance = font height (line thickness in the block direction).
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		blockAdvance = math.Round(float64(m.Height) / 64.0)
	} else {
		blockAdvance = math.Round(fontSize * 1.2)
	}
	return
}

// FontAscent returns the font ascent in pixels for the given style.
func FontAscent(fontSize float64, bold, italic, mono, ahem bool) float64 {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	return FontAscentFromFont(fontSize, fontPath)
}

// FontAscentFromFont returns the font ascent in pixels for the given font path.
func FontAscentFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return fontSize * 0.8
	}
	return float64(m.Ascent) / 64.0
}

// MeasureTextVerticalFromFont returns the inline advance of text in a vertical
// writing mode using the given font path. Each upright glyph advances by fontSize.
func MeasureTextVerticalFromFont(text string, fontSize float64, fontPath string) (inlineAdvance, blockAdvance float64) {
	runeCount := utf8.RuneCountInString(text)
	inlineAdvance = float64(runeCount) * fontSize
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		blockAdvance = math.Round(float64(m.Height) / 64.0)
	} else {
		blockAdvance = math.Round(fontSize * 1.2)
	}
	return
}

// BreakTextAtCharacterBoundary splits text so the prefix fits within maxWidth.
func BreakTextAtCharacterBoundary(textStr string, fontSize float64, bold, italic, mono, ahem bool, maxWidth float64) (string, string) {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	runes := []rune(textStr)
	bestLen := 0
	for i := 1; i <= len(runes); i++ {
		prefix := string(runes[:i])
		w := measureWidth(prefix, fontSize, fontPath)
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

// BreakTextIntoLines breaks text into lines that fit within maxWidth.
func BreakTextIntoLines(text string, fontSize float64, bold bool, maxWidth float64) []string {
	return BreakTextIntoLinesWithWrap(text, fontSize, bold, maxWidth, maxWidth)
}

// BreakTextIntoLinesWithWrap breaks text into lines with separate first/remaining widths.
func BreakTextIntoLinesWithWrap(text string, fontSize float64, bold bool, firstLineMax, remainingMax float64) []string {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}
	return breakLines(text, fontSize, fontPath, firstLineMax, remainingMax)
}

// BreakTextIntoLinesWithStyle breaks text with the full style combination.
func BreakTextIntoLinesWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool, firstLineMax, remainingMax float64) []string {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	return breakLines(text, fontSize, fontPath, firstLineMax, remainingMax)
}

func breakLines(text string, fontSize float64, fontPath string, firstLineMax, remainingMax float64) []string {
	if measureWidth(text, fontSize, fontPath) <= firstLineMax {
		return []string{text}
	}
	leadingSpace := ""
	if len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n') {
		leadingSpace = " "
	}
	words := splitIntoWords(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	currentLine := ""
	lineNum := 0
	for i, word := range words {
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
		if measureWidth(testLine, fontSize, fontPath) <= maxWidth {
			currentLine = testLine
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
				lineNum++
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

func splitIntoWords(text string) []string {
	var words []string
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

// GetFirstWord returns the first word of the text.
func GetFirstWord(text string) string {
	words := splitIntoWords(text)
	if len(words) > 0 {
		return words[0]
	}
	return ""
}
