// Package csstest embeds the WPT CSSTest font set used by the
// `css/css-fonts/font-family-name-*` reftests in
// pkg/visualtest/testdata/wpt-css3/css-fonts/.
//
// The fonts are derived from SIL's Gentium Basic and distributed by the
// W3C Web Platform Tests project at https://github.com/web-platform-tests/wpt
// under the SIL Open Font License 1.1 (see LICENSE). Vendored here so the
// WPT reftests don't depend on fonts being installed system-wide.
//
// Registration happens at first use via [Register]; the registration is
// idempotent. Callers don't need to invoke Register explicitly — it is
// triggered from the package init() once the shared GlyphProvider has been
// lazily constructed.
//
// The CSSTest set follows a Blink-equivalent contract: each font's name
// table declares one or more family names (English plus, for the
// `familyname` faces, the Japanese localized name "ＣＳＳテスト　フォント名").
// CSS `font-family` lookups match by family-name string equality.
package csstest

import (
	_ "embed"
	"sync"

	"mazarin/textshape"
)

//go:embed csstest-ascii.ttf
var fontASCII []byte

//go:embed csstest-fallback.ttf
var fontFallback []byte

//go:embed csstest-familyname.ttf
var fontFamilyName []byte

//go:embed csstest-familyname-bold.ttf
var fontFamilyNameBold []byte

//go:embed csstest-familyname-funkyA.ttf
var fontFamilyNameFunkyA []byte

//go:embed csstest-familyname-funkyB.ttf
var fontFamilyNameFunkyB []byte

//go:embed csstest-familyname-funkyC.ttf
var fontFamilyNameFunkyC []byte

//go:embed csstest-verify.ttf
var fontVerify []byte

//go:embed csstest-weights-400.ttf
var fontWeights400 []byte

// FontEntry pairs a CSS-visible family name with the embedded font bytes and
// the OS/2 variant (Regular/Bold/Italic/BoldItalic) the face represents.
//
// Multiple aliases for the same byte payload (e.g. English + Japanese
// localized names for the FamilyName face) appear as separate entries —
// CSS Fonts 3 §3.1 says localized family names match like English ones.
//
// Critically, the Family field must be the *family* name from the font's
// name table (nameID 1 / 16 "Preferred Family"), NOT the full name (nameID 4
// "<Family> <Subfamily>"). Registering the fullname as if it were a family
// would let `font-family: "CSSTest FamilyName Bold"` resolve to the Bold
// face — exactly what tests 013/015 assert must NOT happen.
type FontEntry struct {
	Family  string
	Variant int32
	Data    []byte
}

// Entries is the complete list of CSSTest family aliases registered with
// the shared GlyphProvider. The list mirrors the name-table family entries
// (nameID 1 / 16) the upstream fonts declare. Variant is taken from the
// face's OS/2 weight/style so a single (family, variant) lookup picks the
// right payload across the Regular + Bold faces.
var Entries = []FontEntry{
	{Family: "CSSTest ASCII", Variant: textshape.VariantRegular, Data: fontASCII},
	{Family: "CSSTest Fallback", Variant: textshape.VariantRegular, Data: fontFallback},
	{Family: "CSSTest FamilyName", Variant: textshape.VariantRegular, Data: fontFamilyName},
	{Family: "ＣＳＳテスト　フォント名", Variant: textshape.VariantRegular, Data: fontFamilyName},
	{Family: "CSSTest FamilyName", Variant: textshape.VariantBold, Data: fontFamilyNameBold},
	{Family: "ＣＳＳテスト　フォント名", Variant: textshape.VariantBold, Data: fontFamilyNameBold},
	// The "funky" faces declare their family-name as a string that resembles
	// a `font` shorthand prefix (e.g. "small-caps 1in CSSTest FamilyName
	// Funky"). The point of tests 020/021 is that CSS Syntax 3 family-name
	// matching is string-equality on the whole `font-family` value — the
	// shorthand parser must not strip these leading tokens.
	{Family: "small-caps 1in CSSTest FamilyName Funky", Variant: textshape.VariantRegular, Data: fontFamilyNameFunkyA},
	{Family: "x-large CSSTest FamilyName Funky", Variant: textshape.VariantRegular, Data: fontFamilyNameFunkyB},
	{Family: "12px CSSTest FamilyName Funky", Variant: textshape.VariantRegular, Data: fontFamilyNameFunkyC},
	{Family: "CSSTest Verify", Variant: textshape.VariantRegular, Data: fontVerify},
	{Family: "CSSTest Weights 400", Variant: textshape.VariantRegular, Data: fontWeights400},
}

// Names returns the set of unique CSS family names registered by [Register].
// Callers in pkg/text use it to gate the synthetic-path mapping that routes
// CSS-side family strings into the shared GlyphProvider.
func Names() []string {
	seen := make(map[string]struct{}, len(Entries))
	out := make([]string, 0, len(Entries))
	for _, e := range Entries {
		if _, ok := seen[e.Family]; ok {
			continue
		}
		seen[e.Family] = struct{}{}
		out = append(out, e.Family)
	}
	return out
}

var registerOnce sync.Once

// Register registers every CSSTest font with the supplied provider. Idempotent
// across concurrent callers. Errors from RegisterBuffer are swallowed — the
// only documented failure mode is an empty family or full font table, neither
// of which applies to the vetted embedded payloads.
func Register(p textshape.GlyphProvider) {
	if p == nil {
		return
	}
	registerOnce.Do(func() {
		for _, e := range Entries {
			_ = p.RegisterBuffer(e.Family, e.Variant, e.Data)
		}
	})
}
