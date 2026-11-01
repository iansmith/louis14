package css

// cssProperty is one property of the engine's property table, mirroring one
// entry of Blink's core/css/css_properties.json5 (Chromium @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): its name, its `inherited` flag
// and, for a shorthand, its `longhands` list. Blink generates
// CSSProperty::IsInherited, shorthandForProperty and the `all` longhand
// iteration from that one file; louis14 derives inheritableProperties,
// shorthandLonghands and cssLonghandProperties from cssProperties the same
// way, so there is one definition of each.
type cssProperty struct {
	name      string
	inherited bool
	// longhands is non-nil for a shorthand: the longhands a CSS-wide keyword
	// on the shorthand sets (setLonghandsToCSSWideKeyword).
	longhands []string
}

// cssProperties lists every property louis14 knows. The trailing comment on
// each entry is its css_properties.json5 line @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Divergences are marked inline:
//   - "Blink shorthand; louis14 stores one value": louis14 keeps the
//     shorthand's whole value under its own name rather than expanding it; it
//     is inherited exactly when every Blink longhand is.
//   - "alias_for": inherited as its alias target.
//   - "no Blink entry": a spec property Blink does not ship yet.
//
// When adding support for a CSS property, add its entry here: the `all`
// shorthand, inheritance (ApplyInheritedProperties, pseudo-element styles,
// `unset`) and CSS-wide keywords on shorthands all read this table.
var cssProperties = []cssProperty{
	// Longhands (and louis14 single-value properties).
	{"display", false, nil},                      // :3439
	{"position", false, nil},                     // :5232
	{"float", false, nil},                        // :3646
	{"clear", false, nil},                        // :3044
	{"visibility", true, nil},                    // :6815
	{"top", false, nil},                          // :6532
	{"right", false, nil},                        // :5384
	{"bottom", false, nil},                       // :2878
	{"left", false, nil},                         // :4158
	{"width", false, nil},                        // :8095
	{"min-width", false, nil},                    // :4567
	{"max-width", false, nil},                    // :4519
	{"height", false, nil},                       // :3925
	{"min-height", false, nil},                   // :4543
	{"max-height", false, nil},                   // :4495
	{"margin-top", false, nil},                   // :4377
	{"margin-right", false, nil},                 // :4350
	{"margin-bottom", false, nil},                // :4297
	{"margin-left", false, nil},                  // :4323
	{"padding-top", false, nil},                  // :5096
	{"padding-right", false, nil},                // :5073
	{"padding-bottom", false, nil},               // :5027
	{"padding-left", false, nil},                 // :5050
	{"box-sizing", false, nil},                   // :2929
	{"overflow", false, nil},                     // :9817 Blink shorthand; louis14 stores one value
	{"overflow-x", false, nil},                   // :4913
	{"overflow-y", false, nil},                   // :4938
	{"aspect-ratio", false, nil},                 // :2100
	{"border-top-width", false, nil},             // :2834
	{"border-right-width", false, nil},           // :2707
	{"border-bottom-width", false, nil},          // :2464
	{"border-left-width", false, nil},            // :2631
	{"border-top-style", false, nil},             // :2811
	{"border-right-style", false, nil},           // :2685
	{"border-bottom-style", false, nil},          // :2442
	{"border-left-style", false, nil},            // :2609
	{"border-top-color", false, nil},             // :2734
	{"border-right-color", false, nil},           // :2658
	{"border-bottom-color", false, nil},          // :2365
	{"border-left-color", false, nil},            // :2582
	{"border-top-left-radius", false, nil},       // :2761
	{"border-top-right-radius", false, nil},      // :2786
	{"border-bottom-left-radius", false, nil},    // :2392
	{"border-bottom-right-radius", false, nil},   // :2417
	{"border-image-source", false, nil},          // :2549
	{"border-image-slice", false, nil},           // :2533
	{"border-image-width", false, nil},           // :2564
	{"border-image-outset", false, nil},          // :2502
	{"border-image-repeat", false, nil},          // :2518
	{"border-collapse", true, nil},               // :2491
	{"border-spacing", true, nil},                // :9273 Blink shorthand; louis14 stores one value
	{"outline-width", false, nil},                // :4839
	{"outline-style", false, nil},                // :4817
	{"outline-color", false, nil},                // :4781
	{"outline-offset", false, nil},               // :4801
	{"background-color", false, nil},             // :2200
	{"background-image", false, nil},             // :2231
	{"background-repeat", false, nil},            // :2306
	{"background-position", false, nil},          // :9076 Blink shorthand; louis14 stores one value
	{"background-size", false, nil},              // :2320
	{"background-attachment", false, nil},        // :2142
	{"background-clip", false, nil},              // :2181
	{"background-origin", false, nil},            // :2252
	{"color", true, nil},                         // :1357
	{"color-scheme", true, nil},                  // :3137
	{"opacity", false, nil},                      // :4721
	{"font-family", true, nil},                   // :1411
	{"font-size", true, nil},                     // :1491
	{"font-weight", true, nil},                   // :1689
	{"font-style", true, nil},                    // :1561
	{"font-variant", true, nil},                  // :9658 Blink shorthand; louis14 stores one value
	{"font-feature-settings", true, nil},         // :1766
	{"font-optical-sizing", true, nil},           // :1451
	{"font-size-adjust", true, nil},              // :1514
	{"font-synthesis-weight", true, nil},         // :1712
	{"font-synthesis-style", true, nil},          // :1730
	{"font-synthesis-small-caps", true, nil},     // :1748
	{"font-synthesis-position", true, nil},       // no Blink entry: CSS Fonts 4 §6.6 Inherited: yes
	{"font-variant-caps", true, nil},             // :1608
	{"font-variant-ligatures", true, nil},        // :1585
	{"font-variant-numeric", true, nil},          // :1648
	{"font-variant-east-asian", true, nil},       // :1628
	{"font-variant-alternates", true, nil},       // :1669
	{"line-height", true, nil},                   // :4225
	{"text-align", true, nil},                    // :6118
	{"text-align-last", true, nil},               // :6142
	{"text-indent", true, nil},                   // :6340
	{"text-transform", true, nil},                // :6453
	{"text-decoration-line", false, nil},         // :6251
	{"text-decoration-style", false, nil},        // :6306
	{"text-decoration-color", false, nil},        // :6227
	{"text-decoration-thickness", false, nil},    // :6321
	{"text-decoration-inset", false, nil},        // no Blink entry: CSS Text Decor 4 Inherited: no
	{"text-decoration-skip-spaces", true, nil},   // :6285
	{"text-underline-offset", true, nil},         // :6477
	{"text-underline-position", true, nil},       // :6497
	{"text-overflow", false, nil},                // :6371
	{"text-shadow", true, nil},                   // :6385
	{"text-wrap", true, nil},                     // :8045 Blink shorthand; louis14 stores one value
	{"text-emphasis-color", true, nil},           // :7830
	{"text-emphasis-style", true, nil},           // :7865
	{"text-emphasis-position", true, nil},        // :7851
	{"letter-spacing", true, nil},                // :4181
	{"word-spacing", true, nil},                  // :8150
	{"white-space", true, nil},                   // :8021 Blink shorthand; louis14 stores one value
	{"word-break", true, nil},                    // :8136
	{"overflow-wrap", true, nil},                 // :4870
	{"word-wrap", true, nil},                     // :10924 alias_for overflow-wrap
	{"hyphens", true, nil},                       // :3996
	{"tab-size", true, nil},                      // :6101
	{"vertical-align", false, nil},               // :6713
	{"writing-mode", true, nil},                  // :1944
	{"text-orientation", true, nil},              // :1918
	{"list-style-type", true, nil},               // :4279
	{"list-style-position", true, nil},           // :4268
	{"list-style-image", true, nil},              // :4249
	{"caption-side", true, nil},                  // :2994
	{"empty-cells", true, nil},                   // :3484
	{"table-layout", false, nil},                 // :6090
	{"flex-direction", false, nil},               // :3584
	{"flex-wrap", false, nil},                    // :3619
	{"flex-grow", false, nil},                    // :3595
	{"flex-shrink", false, nil},                  // :3607
	{"flex-basis", false, nil},                   // :3569
	{"align-items", false, nil},                  // :2031
	{"align-content", false, nil},                // :2020
	{"align-self", false, nil},                   // :2054
	{"justify-content", false, nil},              // :4120
	{"justify-items", false, nil},                // :4131
	{"justify-self", false, nil},                 // :4142
	{"order", false, nil},                        // :4741
	{"row-gap", false, nil},                      // :7054
	{"column-gap", false, nil},                   // :7037
	{"grid-template-columns", false, nil},        // :3868
	{"grid-template-rows", false, nil},           // :3904
	{"grid-template-areas", false, nil},          // :3854
	{"grid-auto-columns", false, nil},            // :3727
	{"grid-auto-rows", false, nil},               // :3756
	{"grid-auto-flow", false, nil},               // :3742
	{"grid-column-start", false, nil},            // :3785
	{"grid-column-end", false, nil},              // :3771
	{"grid-row-start", false, nil},               // :3840
	{"grid-row-end", false, nil},                 // :3826
	{"column-count", false, nil},                 // :7022
	{"column-width", false, nil},                 // :7368
	{"column-fill", false, nil},                  // :3153
	{"column-rule-width", false, nil},            // :7324
	{"column-rule-style", false, nil},            // :7263
	{"column-rule-color", false, nil},            // :7228
	{"column-span", false, nil},                  // :7357
	{"transform", false, nil},                    // :6578
	{"transform-origin", false, nil},             // :6606
	{"translate", false, nil},                    // :6635
	{"rotate", false, nil},                       // :6653
	{"scale", false, nil},                        // :6669
	{"transform-style", false, nil},              // :6624
	{"backface-visibility", false, nil},          // :2132
	{"perspective", false, nil},                  // :5188
	{"perspective-origin", false, nil},           // :5201
	{"transition", false, nil},                   // :10030 Blink shorthand; louis14 stores one value
	{"animation-name", false, nil},               // :1034
	{"animation-duration", false, nil},           // :980
	{"animation-timing-function", false, nil},    // :1116
	{"animation-delay", false, nil},              // :946
	{"animation-iteration-count", false, nil},    // :1015
	{"animation-direction", false, nil},          // :962
	{"animation-fill-mode", false, nil},          // :998
	{"animation-play-state", false, nil},         // :1051
	{"box-shadow", false, nil},                   // :2910
	{"filter", false, nil},                       // :3551
	{"backdrop-filter", false, nil},              // :2114
	{"mix-blend-mode", false, nil},               // :4591
	{"isolation", false, nil},                    // :4109
	{"clip", false, nil},                         // :3055
	{"clip-path", false, nil},                    // :3071
	{"mask-image", false, nil},                   // :7643
	{"fill", true, nil},                          // :3496
	{"fill-rule", true, nil},                     // :3536
	{"stroke", true, nil},                        // :5956
	{"stroke-dasharray", true, nil},              // :5980
	{"stroke-dashoffset", true, nil},             // :5999
	{"stroke-linecap", true, nil},                // :6015
	{"stroke-linejoin", true, nil},               // :6029
	{"stroke-miterlimit", true, nil},             // :6043
	{"stroke-width", true, nil},                  // :6072
	{"flood-color", false, nil},                  // :3662
	{"flood-opacity", false, nil},                // :3683
	{"stop-color", false, nil},                   // :5920
	{"stop-opacity", false, nil},                 // :5941
	{"lighting-color", false, nil},               // :4205
	{"content", false, nil},                      // :3239
	{"counter-increment", false, nil},            // :3336
	{"counter-reset", false, nil},                // :3349
	{"quotes", true, nil},                        // :5324
	{"cursor", true, nil},                        // :3375
	{"z-index", false, nil},                      // :8174
	{"will-change", false, nil},                  // :8120
	{"contain", false, nil},                      // :3164
	{"break-before", false, nil},                 // :2958
	{"break-after", false, nil},                  // :2942
	{"break-inside", false, nil},                 // :2974
	{"orphans", true, nil},                       // :4766
	{"widows", true, nil},                        // :8081
	{"image-rendering", true, nil},               // :4025
	{"object-fit", false, nil},                   // :4608
	{"object-position", false, nil},              // :4619
	{"scrollbar-color", true, nil},               // :5485
	{"scrollbar-gutter", false, nil},             // :5501
	{"scrollbar-width", false, nil},              // :5518
	{"box-decoration-break", false, nil},         // :2901
	{"initial-letter", false, nil},               // :4070
	{"-webkit-box-align", false, nil},            // :6941
	{"-webkit-box-direction", false, nil},        // :6959
	{"-webkit-box-orient", false, nil},           // :6990
	{"-webkit-box-decoration-break", false, nil}, // :6952
	{"-webkit-line-clamp", false, nil},           // :7507
	{"-webkit-mask-image", false, nil},           // :7655 alias_for mask-image
	{"font-kerning", true, nil},                  // :1431
	{"direction", true, nil},                     // :1393
	{"line-break", true, nil},                    // :7433
	{"text-decoration-skip-ink", true, nil},      // :6268
	{"accent-color", true, nil},                  // :2002
	{"color-interpolation-filters", true, nil},   // :3115
	// Shorthands.
	{"margin", false, []string{"margin-top", "margin-right", "margin-bottom", "margin-left"}},      // :9770
	{"padding", false, []string{"padding-top", "padding-right", "padding-bottom", "padding-left"}}, // :9830
	{"border", false, []string{"border-top-color", "border-top-style", "border-top-width", "border-right-color", "border-right-style", "border-right-width", "border-bottom-color", "border-bottom-style", "border-bottom-width", "border-left-color", "border-left-style", "border-left-width", "border-image-source", "border-image-slice", "border-image-width", "border-image-outset", "border-image-repeat"}}, // :9084
	{"border-top", false, []string{"border-top-width", "border-top-style", "border-top-color"}},                                                                                                                // :9290
	{"border-right", false, []string{"border-right-width", "border-right-style", "border-right-color"}},                                                                                                        // :9261
	{"border-bottom", false, []string{"border-bottom-width", "border-bottom-style", "border-bottom-color"}},                                                                                                    // :9154
	{"border-left", false, []string{"border-left-width", "border-left-style", "border-left-color"}},                                                                                                            // :9239
	{"border-width", false, []string{"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"}},                                                                                    // :9300
	{"border-style", false, []string{"border-top-style", "border-right-style", "border-bottom-style", "border-left-style"}},                                                                                    // :9280
	{"border-color", false, []string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}},                                                                                    // :9166
	{"border-radius", false, []string{"border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius"}},                                                         // :9251
	{"border-image", false, []string{"border-image-source", "border-image-slice", "border-image-width", "border-image-outset", "border-image-repeat"}},                                                         // :9176
	{"border-inline", false, []string{"border-inline-start-color", "border-inline-start-style", "border-inline-start-width", "border-inline-end-color", "border-inline-end-style", "border-inline-end-width"}}, // :9186
	{"border-block", false, []string{"border-block-start-color", "border-block-start-style", "border-block-start-width", "border-block-end-color", "border-block-end-style", "border-block-end-width"}},        // :9101
	{"border-inline-start", false, []string{"border-inline-start-width", "border-inline-start-style", "border-inline-start-color"}},                                                                            // :9214
	{"border-inline-end", false, []string{"border-inline-end-width", "border-inline-end-style", "border-inline-end-color"}},                                                                                    // :9201
	{"border-block-start", false, []string{"border-block-start-width", "border-block-start-style", "border-block-start-color"}},                                                                                // :9129
	{"border-block-end", false, []string{"border-block-end-width", "border-block-end-style", "border-block-end-color"}},                                                                                        // :9116
	{"outline", false, []string{"outline-color", "outline-style", "outline-width"}},                                                                                                                            // :9809
	{"column-rule", false, []string{"column-rule-width", "column-rule-style", "column-rule-color"}},                                                                                                            // :10079
	{"background", false, []string{"background-color", "background-image", "background-repeat", "background-position", "background-size", "background-origin", "background-clip", "background-attachment"}},    // :9059; louis14 stores background-position as one longhand
	{"font", false, []string{"font-style", "font-variant-ligatures", "font-variant-caps", "font-variant-numeric", "font-variant-east-asian", "font-variant-alternates", "font-variant-position", "font-variant-emoji", "font-weight", "font-stretch", "font-size", "line-height", "font-family", "font-optical-sizing", "font-size-adjust", "font-kerning", "font-feature-settings", "font-variation-settings", "font-language-override"}}, // :9642
	{"font-synthesis", false, []string{"font-synthesis-weight", "font-synthesis-style", "font-synthesis-small-caps", "font-synthesis-position"}}, // :9671; + font-synthesis-position (CSS Fonts 4 §6.5; no Blink longhand)
	{"flex", false, []string{"flex-grow", "flex-shrink", "flex-basis"}},                                                                          // :9628
	{"flex-flow", false, []string{"flex-direction", "flex-wrap"}},                                                                                // :9637
	{"list-style", false, []string{"list-style-position", "list-style-image", "list-style-type"}},                                                // :9765
	{"gap", false, []string{"row-gap", "column-gap"}},                                                                                            // :9705
	{"place-items", false, []string{"align-items", "justify-items"}},                                                                             // :9694
	{"place-content", false, []string{"align-content", "justify-content"}},                                                                       // :9689
	{"place-self", false, []string{"align-self", "justify-self"}},                                                                                // :9699
	{"columns", false, []string{"column-width", "column-count", "column-height", "column-wrap"}},                                                 // :10094
	{"margin-inline", false, []string{"margin-inline-start", "margin-inline-end"}},                                                               // :9788
	{"margin-block", false, []string{"margin-block-start", "margin-block-end"}},                                                                  // :9780
	{"padding-inline", false, []string{"padding-inline-start", "padding-inline-end"}},                                                            // :9846
	{"padding-block", false, []string{"padding-block-start", "padding-block-end"}},                                                               // :9840
	{"inset", false, []string{"top", "right", "bottom", "left"}},                                                                                 // :9744
	{"inset-inline", false, []string{"inset-inline-start", "inset-inline-end"}},                                                                  // :9758
	{"inset-block", false, []string{"inset-block-start", "inset-block-end"}},                                                                     // :9751
	{"grid-template", false, []string{"grid-template-rows", "grid-template-columns", "grid-template-areas"}},                                     // :9735
	{"animation", false, []string{"animation-duration", "animation-timing-function", "animation-delay", "animation-iteration-count", "animation-direction", "animation-fill-mode", "animation-play-state", "animation-name", "animation-timeline", "animation-range-start", "animation-range-end"}}, // :9036
	{"text-emphasis", false, []string{"text-emphasis-style", "text-emphasis-color"}},                                                            // :10137
	{"text-decoration", false, []string{"text-decoration-line", "text-decoration-thickness", "text-decoration-style", "text-decoration-color"}}, // :10023
}

// inheritableProperties is the set of inherited properties: Blink's
// CSSProperty::IsInherited (`inherited: true` in css_properties.json5).
var inheritableProperties = func() map[string]bool {
	m := make(map[string]bool)
	for _, p := range cssProperties {
		if p.inherited {
			m[p.name] = true
		}
	}
	return m
}()

// shorthandLonghands maps each shorthand to its longhands: Blink's
// shorthandForProperty (css_properties.json5 `longhands`).
var shorthandLonghands = func() map[string][]string {
	m := make(map[string][]string)
	for _, p := range cssProperties {
		if p.longhands != nil {
			m[p.name] = p.longhands
		}
	}
	return m
}()

// cssLonghandProperties is every non-shorthand property, the set the `all`
// shorthand expands to (Blink's all.cc iterates the longhands of
// CSSPropertyIDIterator).
var cssLonghandProperties = func() []string {
	var out []string
	for _, p := range cssProperties {
		if p.longhands == nil {
			out = append(out, p.name)
		}
	}
	return out
}()
