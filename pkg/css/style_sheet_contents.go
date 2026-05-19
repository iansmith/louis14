package css

import "sync"

// StyleSheetContents mirrors Blink's
// `core/css/style_sheet_contents.h::StyleSheetContents` @
// chromium-main d4ecdfed88f962439247c2ad36b8fe47805b1520. It pairs a piece
// of CSS source text with the ParserContext used to parse it, and caches
// the parsed Stylesheet so that multiple consumers within a single document
// lifetime (cascade, layout tree builder for pseudo-elements, @font-face
// registration, @counter-style collection, …) share one parse.
//
// Blink's version stores the parsed Rule vectors directly and exposes a
// `ParseString()` mutator; louis14's louis14 keeps the parse lazy by
// retaining the source text and invoking ParseStylesheet on first access.
// Phase 5 of LOU-138 swaps `Document.Stylesheets []string` for
// `[]*StyleSheetContents` and gives each entry its own `Context` (e.g. an
// external `<link href="sub/main.css">` sheet's BaseDir = the sheet's URL
// directory, not the document's).
//
// Blink's copy-on-write sharing flags (`IsUsedFromTextCache`,
// `referenced_from_resource_`) are intentionally omitted: no test today
// exercises sheet sharing across documents, and the LOU-139 resource-cache
// ticket is the right home for that primitive.
type StyleSheetContents struct {
	Text    string
	Context *ParserContext

	once     sync.Once
	parsed   *Stylesheet
	parseErr error
}

// NewStyleSheetContents wraps a piece of CSS source text together with the
// ParserContext that should be used to parse it. The actual parse is
// deferred to the first ParsedStylesheet call.
func NewStyleSheetContents(text string, ctx *ParserContext) *StyleSheetContents {
	return &StyleSheetContents{Text: text, Context: ctx}
}

// ParsedStylesheet returns the parsed Stylesheet, parsing on first call and
// returning the same cached result thereafter. Safe for concurrent callers.
func (s *StyleSheetContents) ParsedStylesheet() (*Stylesheet, error) {
	s.once.Do(func() {
		s.parsed, s.parseErr = ParseStylesheet(s.Text, s.Context)
	})
	return s.parsed, s.parseErr
}
