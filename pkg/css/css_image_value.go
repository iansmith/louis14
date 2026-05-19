package css

// CSSImageValue mirrors Blink's `CSSImageValue`
// (third_party/blink/renderer/core/css/css_image_value.h:37 @ chromium-main
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Carries the parsed `url(...)`
// reference for image-bearing CSS properties (background-image, mask-image,
// border-image-source, list-style-image, cursor) so consumers fetch by
// `Data.Absolute` while `Data.Relative` survives for getComputedStyle /
// CSSOM serialization.
//
// Blink's version also carries a `cached_image_` field for the fetch
// lifecycle. louis14 omits that until the StyleImage / StyleFetchedImage
// abstractions land — when a consumer needs an in-place async fetch
// (rather than the current synchronous filesystem fetcher), this struct
// gains that field.
type CSSImageValue struct {
	Data URLData
}
