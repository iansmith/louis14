package css

// CSSURIValue mirrors Blink's `CSSURIValue`
// (third_party/blink/renderer/core/css/css_uri_value.h:23 @ chromium-main
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Carries a parsed `url(...)`
// reference for non-image URI properties — fragment refs like
// `mask: url(#svg-id)`, filter refs, font-face srcs, etc. Mirrors
// CSSImageValue's shape but is a distinct type so consumers can dispatch
// on intent (image-bearing vs reference).
type CSSURIValue struct {
	Data URLData
}
