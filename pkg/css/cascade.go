package css

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"louis14/pkg/html"
	urlpkg "louis14/pkg/url"
)

// Phase 3: CSS Cascade - computing final styles for a node

// Phase 17: applyUserAgentStyles applies default browser styles based on element type
func applyUserAgentStyles(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}

	// CSS 2.1 §15.3: initial value of font-family is UA-dependent.
	// Browsers default to serif; set on the root element so all descendants
	// inherit it. Author/inline styles override via normal cascade rules.
	if node.TagName == "html" {
		if _, ok := style.Get("font-family"); !ok {
			style.Set("font-family", "serif")
		}
	}

	// Default styles for <a> (anchor/link) elements
	if node.TagName == "a" {
		style.Set("color", "#0645ad") // Standard link blue
		// Expressed as the text-decoration-line longhand (not the legacy
		// shorthand) so the AppliedTextDecoration model can append this
		// element's own contribution without colliding with other longhands.
		// Mirrors Blink UA stylesheet at
		// third_party/blink/renderer/core/html/resources/html.css (SHA pinned).
		style.Set("text-decoration-line", "underline")
	}

	// UA default decorations for <u>, <ins>: underline. <s>, <strike>, <del>:
	// line-through. CSS Text Decor 3 §2.1 list of UA-default decorated elements.
	switch node.TagName {
	case "u", "ins":
		style.Set("text-decoration-line", "underline")
	case "s", "strike", "del":
		style.Set("text-decoration-line", "line-through")
	}

	// Default margin for <body> element (CSS 2.1 §8.3.1).
	// Physical: 8px on all sides, same in every writing mode.
	if node.TagName == "body" {
		style.Set("margin-top", "8px")
		style.Set("margin-right", "8px")
		style.Set("margin-bottom", "8px")
		style.Set("margin-left", "8px")
	}

	// Default margin for block elements: use logical markers so that
	// resolveLogicalBoxProperties maps to the correct physical side in
	// vertical/sideways writing modes (HTML spec §15.3.3).
	if node.TagName == "p" || node.TagName == "dl" ||
		node.TagName == "hr" {
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
	}

	// Headings: logical block margins per HTML spec.
	switch node.TagName {
	case "h1":
		style.Set("margin-top", "0.67em")
		style.Set("margin-bottom", "0.67em")
		style.Set("_margin-block-start", "0.67em")
		style.Set("_margin-block-end", "0.67em")
	case "h2":
		style.Set("margin-top", "0.83em")
		style.Set("margin-bottom", "0.83em")
		style.Set("_margin-block-start", "0.83em")
		style.Set("_margin-block-end", "0.83em")
	case "h3":
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
	case "h4":
		style.Set("margin-top", "1.33em")
		style.Set("margin-bottom", "1.33em")
		style.Set("_margin-block-start", "1.33em")
		style.Set("_margin-block-end", "1.33em")
	case "h5":
		style.Set("margin-top", "1.67em")
		style.Set("margin-bottom", "1.67em")
		style.Set("_margin-block-start", "1.67em")
		style.Set("_margin-block-end", "1.67em")
	case "h6":
		style.Set("margin-top", "2.33em")
		style.Set("margin-bottom", "2.33em")
		style.Set("_margin-block-start", "2.33em")
		style.Set("_margin-block-end", "2.33em")
	}

	// Lists and blockquotes: logical block margins.
	if node.TagName == "ul" || node.TagName == "ol" || node.TagName == "menu" ||
		node.TagName == "dir" || node.TagName == "blockquote" || node.TagName == "figure" {
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
	}

	// Non-rendered elements should be hidden by default
	// Author CSS can override this (e.g., Acid2 sets display:block on head)
	switch node.TagName {
	case "head", "style", "script", "meta", "title", "link", "base":
		style.Set("display", "none")
	}

	// Dialog elements are hidden by default unless they have the "open" attribute
	if node.TagName == "dialog" {
		if _, hasOpen := node.GetAttribute("open"); !hasOpen {
			style.Set("display", "none")
		}
	}

	// HTML5 semantic elements default to display: block
	switch node.TagName {
	case "main", "nav", "header", "footer", "section", "article", "aside",
		"figure", "figcaption", "details", "summary", "hgroup":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "block")
		}
	}

	// Default font-style for emphasis elements
	switch node.TagName {
	case "em", "i", "cite", "dfn", "var":
		style.Set("font-style", "italic")
	}

	// Default font-weight for strong elements
	switch node.TagName {
	case "strong", "b":
		style.Set("font-weight", "bold")
	}

	// Default monospace font-family for code elements
	switch node.TagName {
	case "code", "pre", "kbd", "samp", "tt":
		style.Set("font-family", "monospace")
	}

	// Default white-space: pre for preformatted elements
	switch node.TagName {
	case "pre", "xmp", "listing":
		style.Set("white-space", "pre")
	}

	// Default inline display for inline HTML elements.
	//
	// `first-letter` / `first-line` are not real HTML elements but WPT
	// reference pages routinely use them as inline stand-ins for the
	// `::first-letter` / `::first-line` pseudo-elements (e.g. css-pseudo
	// `first-letter-skip-empty-span-ref.html`). Per HTML living spec
	// §15.3.1 unknown elements default to `display: inline`; louis14's
	// `GetDisplay()` default is `block`, so without an explicit entry
	// these refs render the synthetic letter on its own line and fail to
	// match the (correctly inline) test side.
	switch node.TagName {
	case "span", "em", "strong", "b", "i", "u", "s", "a", "abbr", "cite",
		"code", "dfn", "kbd", "mark", "q", "samp", "small", "sub", "sup",
		"var", "time", "label", "br", "wbr", "img", "object",
		"first-letter", "first-line":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline")
		}
	}

	// Replaced inline elements: behave as inline-block for layout purposes
	// (they are replaced elements that honor width/height attributes)
	switch node.TagName {
	case "canvas", "video", "embed", "iframe":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
	}

	// CSS Overflow Module Level 4 §3.7 / Blink html.css:
	//   embed, iframe, img, video {
	//     overflow: clip;
	//     overflow-clip-margin: content-box;
	//   }
	// "Host languages should define UA style sheet rules that apply a
	// default value of clip to such elements and set their
	// overflow-clip-margin to content-box."
	// Vetted against Chromium `main` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
	// (third_party/blink/renderer/core/html/resources/html.css).
	switch node.TagName {
	case "embed", "iframe", "img", "video":
		if _, ok := style.Get("overflow"); !ok {
			style.Set("overflow", "clip")
		}
		if _, ok := style.Get("overflow-clip-margin"); !ok {
			style.Set("overflow-clip-margin", "content-box")
		}
	}

	// Ruby UA stylesheet. Mirrors Blink html.css:1701-1720 exactly
	// (vetted at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). In
	// modern Blink only `ruby` and `rt` have ruby-specific UA display
	// values; `rb`, `rbc`, and `rtc` receive NO display override and
	// remain plain inline boxes (Blink's EDisplay has no kRubyBase /
	// kRubyBaseContainer / kRubyTextContainer). `rp` is hidden by the
	// separate flat element-only rule at html.css:972-975
	// (base, basefont, datalist, head, link, meta, noembed, noframes,
	// param, rp, script, style, template, title { display: none }),
	// not by a parent-scoped `ruby > rp` selector — so rp is hidden
	// regardless of parent.
	//
	// The rt UA rule has two layers in Blink: the unconditional `rt {
	// line-height: normal; text-emphasis: none }` applies always, while
	// `ruby > rt { display: ruby-text; font-size: 50%; text-align: start }`
	// only applies when rt's parent is a ruby box.
	switch node.TagName {
	case "ruby":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "ruby")
		}
		// html.css:1701-1704 — `ruby, rt { text-indent: 0 }`.
		if _, ok := style.Get("text-indent"); !ok {
			style.Set("text-indent", "0")
		}
	case "rt":
		// Unconditional rt rules.
		if _, ok := style.Get("line-height"); !ok {
			style.Set("line-height", "normal")
		}
		if _, ok := style.Get("text-indent"); !ok {
			style.Set("text-indent", "0")
		}
		// `ruby > rt` rules — only when parent is a ruby element.
		if node.Parent != nil && node.Parent.TagName == "ruby" {
			if _, ok := style.Get("display"); !ok {
				style.Set("display", "ruby-text")
			}
			if _, ok := style.Get("font-size"); !ok {
				// Blink uses 50%, not 0.5em — ruby-rt-fontsize-001
				// expects exactly half the parent's font-size after
				// inheritance, which 50% guarantees and 0.5em does
				// not (`em` resolves against the inherited size
				// before this UA rule, but `%` does the same and is
				// the spec-exact value Blink ships).
				style.Set("font-size", "50%")
			}
			if _, ok := style.Get("text-align"); !ok {
				style.Set("text-align", "start")
			}
		}
	case "rp":
		// Hidden unconditionally per Blink html.css:972-975 (the flat
		// element-only display:none rule). Not parent-scoped.
		style.Set("display", "none")
	}

	// Default styles for form elements — rendered as simple boxes.
	// Note: must use individual properties (not shorthands like "border" or "padding")
	// because style.Set() does not expand shorthands.
	switch node.TagName {
	case "input":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		inputType, _ := node.GetAttribute("type")
		if inputType == "" {
			inputType = "text"
		}
		switch inputType {
		case "checkbox", "radio":
			if _, ok := style.Get("width"); !ok {
				style.Set("width", "13px")
			}
			if _, ok := style.Get("height"); !ok {
				style.Set("height", "13px")
			}
			setFormBorder(style, "1px", "solid", "#767676")
			if _, ok := style.Get("background-color"); !ok {
				style.Set("background-color", "white")
			}
		default:
			// text, password, email, number, search, etc.
			// UA uses border-box sizing (matches real browser UA stylesheets)
			style.Set("box-sizing", "border-box")
			if _, ok := style.Get("width"); !ok {
				style.Set("width", "173px")
			}
			if _, ok := style.Get("height"); !ok {
				style.Set("height", "19px")
			}
			setFormPadding(style, "1px", "2px", "1px", "2px")
			setFormBorder(style, "2px", "solid", "#767676")
			if _, ok := style.Get("background-color"); !ok {
				style.Set("background-color", "white")
			}
			if _, ok := style.Get("font-size"); !ok {
				style.Set("font-size", "13.3333px")
			}
			style.Set("overflow", "hidden")
		}
	case "textarea":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		// Blink/Firefox/Safari: textarea defaults to content-box (border-box is
		// gated behind the experimental AppearanceBase feature). Default width
		// 173px and height 54px are border-box-ish approximations of 20 cols × 2 rows
		// but we express them as content-box to match the standard box model.
		if _, ok := style.Get("width"); !ok {
			style.Set("width", "167px")
		}
		if _, ok := style.Get("height"); !ok {
			style.Set("height", "48px")
		}
		setFormPadding(style, "2px", "2px", "2px", "2px")
		setFormBorder(style, "1px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "white")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		style.Set("font-family", "monospace")
		style.Set("overflow", "hidden")
	case "select":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		// UA uses border-box sizing (matches real browser UA stylesheets)
		style.Set("box-sizing", "border-box")
		if _, ok := style.Get("width"); !ok {
			style.Set("width", "173px")
		}
		if _, ok := style.Get("height"); !ok {
			style.Set("height", "19px")
		}
		setFormPadding(style, "1px", "2px", "1px", "2px")
		setFormBorder(style, "1px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "white")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		style.Set("overflow", "hidden")
	case "button":
		// Blink's <button> is an inline flex container with cross-axis centering
		// (align-items:center) and start-aligned main-axis content
		// (justify-content defaults to flex-start). Horizontal centering of text
		// content is handled separately by text-align:center below.
		// Mirrors third_party/blink/renderer/core/html/forms/html_button_element.cc
		// and the UA defaults in third_party/blink/renderer/core/html/resources/html.css.
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-flex")
		}
		if _, ok := style.Get("align-items"); !ok {
			style.Set("align-items", "center")
		}
		style.Set("box-sizing", "border-box")
		setFormPadding(style, "1px", "6px", "1px", "6px")
		setFormBorder(style, "2px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "#efefef")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		if _, ok := style.Get("text-align"); !ok {
			style.Set("text-align", "center")
		}
	}

	// Phase 23: Default styles for table elements
	switch node.TagName {
	case "table":
		style.Set("display", "table")
		style.Set("border-collapse", "separate")
		style.Set("border-spacing", "2px")
		// CSS Tables: table 'width' effectively specifies border-box width.
		// Matches browser behavior where table width includes padding/border.
		style.Set("box-sizing", "border-box")
	case "thead":
		style.Set("display", "table-header-group")
	case "tbody":
		style.Set("display", "table-row-group")
	case "tfoot":
		style.Set("display", "table-footer-group")
	case "tr":
		style.Set("display", "table-row")
	case "td":
		style.Set("display", "table-cell")
		style.Set("padding", "1px")
		// HTML 15.3.4: `<td>` has no UA text-align rule — text-align inherits
		// from the enclosing table / row context. Mirrors Blink's html.css
		// which omits any td-specific text-align declaration.
		style.Set("vertical-align", "middle")
	case "th":
		style.Set("display", "table-cell")
		style.Set("padding", "1px")
		style.Set("font-weight", "bold")
		style.Set("vertical-align", "middle")
		// HTML 15.3.4 + Blink html.css `th { text-align: -internal-center; }`:
		// the UA-default centers the header cell ONLY when the inherited
		// text-align is the initial value (`start`). If the table or an
		// ancestor explicitly sets text-align: end/right/etc., the th's
		// text-align inherits that value rather than being forced to center.
		// The `-internal-center` sentinel marks this UA-defaulted state; it
		// is resolved post-cascade in ApplyInheritedProperties below.
		// Mirrors Blink's HTMLBodyElement/ComputedStyle handling of
		// `-internal-center` at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		style.Set("text-align", "-internal-center")
	case "caption":
		style.Set("display", "table-caption")
		style.Set("text-align", "center")

	// Phase 23: Default styles for list elements
	// Per HTML spec default stylesheet, list-style-type is set on ul/ol (not li),
	// so author "list-style: none" on ul/ol overrides it and li inherits "none".
	case "ul":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		expandShorthand(style, "padding-inline-start", "40px")
		style.Set("list-style-type", "disc")
	case "ol":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		expandShorthand(style, "padding-inline-start", "40px")
		style.Set("list-style-type", "decimal")
	case "li":
		style.Set("display", "list-item")
	}
}

// buildLayerOrder builds a combined layer order from all stylesheets.
// Earlier layers in the order have lower cascade priority (they lose to later layers).
// The order comes from @layer declaration statements and @layer block rules.
func buildLayerOrder(stylesheets []*Stylesheet) []string {
	seen := make(map[string]bool)
	var order []string
	for _, ss := range stylesheets {
		for _, name := range ss.LayerOrder {
			if !seen[name] {
				seen[name] = true
				order = append(order, name)
			}
		}
	}
	return order
}

// layerPriority returns the cascade priority of a rule based on its layer name.
// Rules in earlier layers get lower priority values (they lose to later layers).
// Unlayered rules (layerName == "") get priority = len(layerOrder) (highest priority).
// Anonymous @layer blocks get a synthetic non-empty name in parseLayerRule —
// they are NEVER treated as unlayered (CSS Cascade 5 §6.2).
func layerPriority(layerName string, layerOrder []string) int {
	if layerName == "" {
		// Unlayered rules win over any layered rule
		return len(layerOrder)
	}
	for i, name := range layerOrder {
		if name == layerName {
			return i
		}
	}
	// Unknown layer name: treat as last declared layer (just before unlayered)
	return len(layerOrder) - 1
}

// importantLayerPriority returns the cascade priority of an !important rule
// per CSS Cascade 5 §6.4.1 step 4: the layer order is REVERSED for the
// important origin. Earliest-declared layer takes the HIGHEST important
// precedence; unlayered/important sits at the BOTTOM of the important
// bucket.
//
// We compose this from layerPriority by mapping:
//   - unlayered (layerPriority == len(layerOrder)) → 0 (lowest important).
//   - layer at index i (layerPriority == i)        → len(layerOrder) - i
//     (so earliest declared layer i=0 becomes the highest, and the latest
//     declared layer i=N-1 becomes 1, just above unlayered).
//
// Mirrors Blink's CascadePriority::GetImportance ordering at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func importantLayerPriority(layerName string, layerOrder []string) int {
	if layerName == "" {
		return 0
	}
	normalPri := layerPriority(layerName, layerOrder)
	return len(layerOrder) - normalPri
}

// ComputeStyle computes the final style for a node by applying the cascade.
// ctx is the owning document's ParserContext — used to resolve url() tokens
// inside the node's inline `style=""` attribute at parse time. Pass nil for
// "no base URL" (test fixtures that don't exercise URL composition).
func ComputeStyle(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64, ctx *ParserContext) *Style {
	finalStyle := NewStyle()

	// Phase 17: Apply user agent (default browser) styles first
	applyUserAgentStyles(node, finalStyle)

	// Map HTML presentational attributes to CSS (lower priority than stylesheets)
	applyPresentationalAttributes(node, finalStyle)

	// CSS Cascade 3 §8 (`all` shorthand) + CSS Cascade 4 §6.1.2 (`revert`):
	// `revert` rolls the cascade back to the previous origin's cascaded value.
	// Capture the UA + presentational-attribute baseline now so post-cascade
	// resolveAllRevertValues can replay it for any longhand whose author-origin
	// declaration ended up as the `revert` sentinel.
	revertSnap := snapshotForAllRevert(finalStyle)

	// Collect all matching rules from all stylesheets
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		matches := FindMatchingRules(node, stylesheet, viewportWidth, viewportHeight)
		allRules = append(allRules, matches...)
	}

	// Sort rules by cascade order: layer priority (lowest first), then specificity
	layerOrder := buildLayerOrder(stylesheets)
	sort.SliceStable(allRules, func(i, j int) bool {
		pi := layerPriority(allRules[i].LayerName, layerOrder)
		pj := layerPriority(allRules[j].LayerName, layerOrder)
		if pi != pj {
			return pi < pj
		}
		return allRules[i].Selector.Specificity < allRules[j].Selector.Specificity
	})

	// Track which properties have been set with !important
	importantProps := make(map[string]bool)

	// Apply rules in order (lower specificity first, higher specificity overwrites).
	// Within a rule, the `all` shorthand must be applied BEFORE every other
	// declaration so subsequent longhands in the same rule (e.g. `all: unset;
	// color: green;`) override the reset. Per CSS Cascade 3 §8, this matches
	// the typical author idiom and Blink's StyleBuilder ordering where `all`
	// is processed before sibling longhand declarations in the same block.
	//
	// CSS Selectors 4 §17.2: declarations from a rule whose selector contains
	// `:visited` are filtered to the visitedAllowedProperties subset before
	// being applied. The non-color longhands a `:visited` rule names (or that
	// `all` would expand to) silently drop, preserving user-browsing-history
	// privacy. Mirrors Blink's StyleResolverState IsVisitedDependentProperty
	// gating at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	// CSS Cascade 5 §6.1.3 (`revert-layer`): each cascade layer needs its
	// own start-of-layer snapshot, AND any `revert-layer` declaration left
	// behind by a layer's rules must be resolved against THAT layer's
	// snapshot before crossing into the next layer. Otherwise the next
	// layer's snapshot captures the literal `revert-layer` sentinel rather
	// than the cascaded value the spec wants. The fixup is invariant for
	// the chained pattern in revert-layer-007 where consecutive layers
	// each contain `revert-layer`.
	//
	// The per-layer snapshots are ALSO recorded in `layerSnapshots` keyed
	// by the rule's LayerName so the !important pass below — which iterates
	// in REVERSED layer order — can resolve revert-layer against the same
	// "start of layer L in declaration order" baseline. CSS Cascade 5's
	// revert-layer semantics ignore the importance-pass reversal: an
	// !important `revert-layer` in layer L still rolls back to the
	// declaration-order predecessor's value, not to the next-lower
	// important-pass layer (revert-layer-005).
	layerSnap := snapshotForAllRevert(finalStyle)
	layerSnapshots := make(map[string]allShorthandRevertSnapshot)
	currentLayerPriority := -1
	for _, rule := range allRules {
		rulePriority := layerPriority(rule.LayerName, layerOrder)
		if rulePriority != currentLayerPriority {
			// Crossing a layer boundary: resolve revert-layer against the
			// snapshot of the layer we're LEAVING, then take a fresh
			// snapshot for the layer we're ENTERING and stash it under
			// the entering layer's name.
			if currentLayerPriority != -1 {
				resolveRevertLayerOnly(finalStyle, revertSnap, layerSnap)
			}
			layerSnap = snapshotForAllRevert(finalStyle)
			layerSnapshots[rule.LayerName] = layerSnap
			currentLayerPriority = rulePriority
		}
		visitedOnly := selectorMatchesVisited(rule.Selector)
		if allVal, hasAll := rule.Declarations["all"]; hasAll {
			if !importantProps["all"] {
				applyDeclarationWithVisitedFilter(finalStyle, "all", allVal, visitedOnly)
			}
		}
		for property, value := range rule.Declarations {
			if property == "all" {
				continue
			}
			// Skip if already set by an important rule
			if importantProps[property] {
				continue
			}
			applyDeclarationWithVisitedFilter(finalStyle, property, value, visitedOnly)
		}
	}
	// Final layer's revert-layer entries — resolve against the snapshot of
	// the last layer iterated. resolveCSSWideKeywords below still handles
	// any leftover entries (e.g. revert-layer in inline styles or where no
	// layer was active) using the same snapshot.
	if currentLayerPriority != -1 {
		resolveRevertLayerOnly(finalStyle, revertSnap, layerSnap)
	}

	// Apply !important declarations (second pass). CSS Cascade 5 §6.4.1
	// step 4: the layer order is REVERSED for the important origin —
	// declarations in the earliest-declared layer have the HIGHEST
	// important precedence, and unlayered/important sits at the BOTTOM of
	// the important bucket. Within a layer the normal specificity order
	// still applies.
	//
	// `revert-layer` inside !important still references the
	// declaration-order predecessor layer's value (NOT the next-lower
	// layer in the importance-reversed order). We re-use the per-layer
	// snapshots `layerSnapshots` captured during the normal pass above —
	// each entry is the cascade state at the start of that layer's
	// declaration-order processing, which is exactly what revert-layer
	// (in either pass) should restore. Mirrors Blink's
	// CSSRevertLayerValue::Resolve at SHA 4883d11fef which looks up the
	// declaration-layer snapshot regardless of which pass is active.
	importantRules := make([]Rule, 0, len(allRules))
	for _, rule := range allRules {
		if len(rule.Important) > 0 {
			importantRules = append(importantRules, rule)
		}
	}
	sort.SliceStable(importantRules, func(i, j int) bool {
		pi := importantLayerPriority(importantRules[i].LayerName, layerOrder)
		pj := importantLayerPriority(importantRules[j].LayerName, layerOrder)
		if pi != pj {
			return pi < pj
		}
		return importantRules[i].Selector.Specificity < importantRules[j].Selector.Specificity
	})
	currentImportantLayerPri := -1
	var currentImportantLayerSnap allShorthandRevertSnapshot
	flushImportantLayer := func() {
		if currentImportantLayerSnap != nil {
			resolveRevertLayerOnly(finalStyle, revertSnap, currentImportantLayerSnap)
		}
	}
	for _, rule := range importantRules {
		rulePri := importantLayerPriority(rule.LayerName, layerOrder)
		if rulePri != currentImportantLayerPri {
			flushImportantLayer()
			// Look up the start-of-layer snapshot captured during the
			// normal pass. Fall back to the post-normal-pass state for
			// the unlayered/important bucket (no entry was recorded for
			// "" if there were no unlayered normal rules).
			snap, ok := layerSnapshots[rule.LayerName]
			if !ok {
				snap = snapshotForAllRevert(finalStyle)
			}
			currentImportantLayerSnap = snap
			currentImportantLayerPri = rulePri
		}
		visitedOnly := selectorMatchesVisited(rule.Selector)
		if rule.Important["all"] {
			if allVal, hasAll := rule.Declarations["all"]; hasAll {
				applyDeclarationWithVisitedFilter(finalStyle, "all", allVal, visitedOnly)
				importantProps["all"] = true
			}
		}
		for property, value := range rule.Declarations {
			if property == "all" {
				continue
			}
			if rule.Important[property] {
				applyDeclarationWithVisitedFilter(finalStyle, property, value, visitedOnly)
				importantProps[property] = true
			}
		}
	}
	flushImportantLayer()

	// Inline styles have highest specificity (specificity = 1000).
	// CSS Cascade 5 §6.4.1 step 3 places the style-attribute origin between
	// the unlayered author bucket and the !important author bucket. For
	// `revert-layer` purposes (CSS Cascade 5 §6.1.3) it is its own
	// cascade-layer-like origin, so we snapshot the cascade state
	// IMMEDIATELY before applying inline declarations — revert-layer in
	// inline styles then rolls back to that pre-inline state, restoring
	// the unlayered/layered author value the stylesheets produced
	// (revert-layer-009).
	// Note: inline !important would override stylesheet !important, but we don't track that yet
	if styleAttr, ok := node.GetAttribute("style"); ok {
		inlineStyle := ParseInlineStyle(styleAttr, ctx)
		preInlineSnap := snapshotForAllRevert(finalStyle)
		for property, value := range inlineStyle.Properties {
			if !importantProps[property] {
				finalStyle.Set(property, value)
			}
		}
		resolveRevertLayerOnly(finalStyle, revertSnap, preInlineSnap)
	}

	// CSS Cascade 4 §6.1.2 / §6.1.3 / Cascade 5 §6.1.3: resolve the CSS-wide
	// keywords that may reach the cascaded value as literal strings —
	// `initial`, `unset`, `revert`, and `revert-layer`. These appear either
	// as longhand-level declarations (e.g. `color: unset`) or as the sentinel
	// placed by the `all` shorthand. `inherit` is resolved later in
	// resolveInheritValues once the parent style is available.
	//
	// `revert` replays the UA + presentational-attribute snapshot taken
	// before author rules ran. `revert-layer` replays the most-recent
	// prior-layer snapshot taken during the layer iteration above. `unset`
	// behaves as `inherit` for inheritable properties (handled in
	// resolveInheritValues) or as `initial` otherwise. `initial` deletes
	// the property so the getter fallback returns the initial value, with
	// the exception of properties in nonDefaultInitialValues whose getter
	// default differs from spec.
	resolveCSSWideKeywords(finalStyle, revertSnap, layerSnap)

	// CSS Animations §4: apply in-effect @keyframes values. The animation
	// cascade origin sits below author !important (CSS Cascade §6.3), so
	// ApplyAnimations skips any property in importantProps. animation-name and
	// the other animation-* longhands have already been resolved (via rule
	// declarations / inline style) into finalStyle by this point.
	ApplyAnimations(finalStyle, collectKeyframesMaps(stylesheets), importantProps)

	// Store viewport dimensions for viewport unit resolution (vw, vh, vmin, vmax)
	finalStyle.ViewportWidth = viewportWidth
	finalStyle.ViewportHeight = viewportHeight

	// CSS Values 5 §11.5: resolve typed attr() references in property values.
	// attr() in non-content properties (e.g. background-color, width) must be
	// substituted at cascade time because Style.Get() does not have element access.
	// This mirrors Blink's StyleCascade::ResolveAttr() in style_cascade.cc at
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which reads the element's
	// attribute and substitutes the typed value into the property's token list.
	resolveAttrReferences(node, finalStyle)

	return finalStyle
}

// ApplyStylesToDocument applies stylesheets to all nodes in the document
// resolveAttrReferences resolves CSS Values 5 §11.5 typed attr() references
// in all property values of style for the given node. Typed attr() appears in
// non-content properties such as background-color, width, color, etc. and must
// be resolved at cascade time because Style.Get() has no element access.
//
// Mirrors Blink's StyleCascade::ResolveAttr() in style_cascade.cc which reads
// the element attribute, parses it as the declared type, and substitutes the
// result (or fallback) into the property's token list at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// For each property whose stored value contains "attr(", this function calls
// resolveAttrInValue() to substitute the attribute value and rewrites the
// property in-place. Properties without attr() are not touched.
//
// Scope: typed attr() only. The legacy content: attr(name) path is handled
// separately in ParseContentValues / layout_tree_builder.go.
func resolveAttrReferences(node *html.Node, style *Style) {
	if node == nil || node.Type != html.ElementNode {
		return
	}
	for prop, val := range style.Properties {
		if strings.Contains(val, "attr(") {
			resolved := resolveAttrInValue(val, node)
			if resolved != val {
				style.Properties[prop] = resolved
			}
		}
	}
}

// resolveAttrInValue resolves all attr() function calls in a CSS property value
// string, using element attributes for substitution. Handles:
//
//   - attr(<name> type(<color>))              — typed color
//   - attr(<name> type(<length>))             — typed length
//   - attr(<name> type(<number>))             — typed number
//   - attr(<name> type(<integer>))            — typed integer
//   - attr(<name> type(<percentage>))         — typed percentage
//   - attr(<name> type(<length-percentage>))  — typed length or percentage
//   - attr(<name>)                            — no type → string (skip for non-content)
//   - attr(<name> type(<type>), <fallback>)   — with fallback
//
// Resolution rules (CSS Values 5 §11.5):
//  1. Read the attribute from the element.
//  2. If absent → use fallback. If fallback absent → property becomes invalid (leave as-is).
//  3. If present → try to parse as the specified type. Success → substitute.
//     Failure → use fallback. If fallback absent → property becomes invalid (leave as-is).
//
// Returns the value with attr() calls substituted. If any attr() cannot be
// resolved (absent attribute + no fallback), the original token is left in place
// so the property becomes invalid at computed-value time (IACVT-like behavior).
func resolveAttrInValue(value string, node *html.Node) string {
	for iter := 0; iter < 20; iter++ {
		// Find the next "attr(" occurrence.
		idx := indexAttrFunction(value)
		if idx < 0 {
			break
		}
		// Find the matching closing paren.
		openParen := idx + 4
		closeIdx := matchClosingParen(value, openParen)
		if closeIdx < 0 {
			break // malformed
		}
		inner := strings.TrimSpace(value[openParen+1 : closeIdx])

		// Parse the attr() arguments: <name> [type(<type>)]? [, <fallback>]?
		attrName, attrType, fallbackRaw := parseAttrArgs(inner)

		// Read the attribute value from the element.
		attrVal, attrPresent := "", false
		if node.Attributes != nil {
			attrVal, attrPresent = node.Attributes[attrName]
		}

		// Resolve to a CSS token.
		var substituted string
		var ok bool
		if attrPresent {
			substituted, ok = resolveAttrTyped(attrVal, attrType)
		}
		if !ok {
			// Attribute absent or failed type parse → try fallback.
			if fallbackRaw == "" {
				// No fallback: leave the attr() in place so the property is
				// treated as invalid (the caller can see attr() remained).
				// Advance past this token to avoid infinite loop.
				value = value[:idx] + value[closeIdx+1:]
				continue
			}
			substituted = strings.TrimSpace(fallbackRaw)
		}

		// Reconstruct the value with the substitution.
		var sb strings.Builder
		sb.WriteString(value[:idx])
		sb.WriteString(substituted)
		sb.WriteString(value[closeIdx+1:])
		value = sb.String()
	}
	return value
}

// indexAttrFunction returns the byte index of the first "attr(" (case-insensitive)
// in s that is NOT immediately preceded by an ident-continue byte (word boundary),
// or -1 if not found.
func indexAttrFunction(s string) int {
	lower := strings.ToLower(s)
	start := 0
	for {
		i := strings.Index(lower[start:], "attr(")
		if i < 0 {
			return -1
		}
		absIdx := start + i
		// Word boundary check: byte before 'a' must not be ident-continue.
		if absIdx > 0 {
			prev := s[absIdx-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') ||
				(prev >= '0' && prev <= '9') || prev == '_' || prev == '-' || prev >= 0x80 {
				start = absIdx + 5
				continue
			}
		}
		return absIdx
	}
}

// parseAttrArgs parses the inner content of attr(…): returns the attribute
// name, the type string (e.g. "<color>", "<length>", "string"), and the raw
// fallback text (everything after the first top-level comma, or "" if absent).
//
// Handles:
//
//	"data-foo"                          → name="data-foo", type="string", fallback=""
//	"data-foo type(<color>)"            → name="data-foo", type="<color>", fallback=""
//	"data-foo type(<length>), 100px"    → name="data-foo", type="<length>", fallback="100px"
//	"data-foo type(<color>), green"     → name="data-foo", type="<color>", fallback="green"
func parseAttrArgs(inner string) (name, attrType, fallback string) {
	// Split at the first top-level comma.
	commaIdx := findCommaOutsideParens(inner)
	namePart := inner
	if commaIdx >= 0 {
		namePart = strings.TrimSpace(inner[:commaIdx])
		fallback = strings.TrimSpace(inner[commaIdx+1:])
	}
	// namePart is "<attrname>" or "<attrname> type(<type>)".
	// Split on whitespace.
	spaceIdx := strings.IndexByte(namePart, ' ')
	if spaceIdx < 0 {
		return strings.TrimSpace(namePart), "string", fallback
	}
	name = strings.TrimSpace(namePart[:spaceIdx])
	rest := strings.TrimSpace(namePart[spaceIdx+1:])
	// rest should be "type(<type>)" — extract the inner type token.
	lrest := strings.ToLower(rest)
	if strings.HasPrefix(lrest, "type(") && strings.HasSuffix(rest, ")") {
		attrType = strings.TrimSpace(rest[5 : len(rest)-1])
	} else {
		// Unknown syntax — treat as string type.
		attrType = "string"
	}
	return name, attrType, fallback
}

// resolveAttrTyped parses attrVal as the CSS type named by attrType and returns
// the resolved CSS token string. Returns ("", false) if the value cannot be
// parsed as the requested type.
//
// Supported types (CSS Values 5 §11.5):
//
//	"<color>"             — parses as a CSS color; returns normalized color if valid.
//	"<length>"            — parses as a CSS length with unit; must not be a bare number.
//	"<percentage>"        — parses as a CSS percentage (number%).
//	"<number>"            — parses as a CSS number (integer or decimal).
//	"<integer>"           — parses as a CSS integer (whole number only).
//	"<length-percentage>" — parses as a CSS length or percentage.
//	"string"              — always valid; returns attrVal as-is.
//
// All other type tokens return ("", false) (unsupported type → fallback).
func resolveAttrTyped(attrVal, attrType string) (string, bool) {
	v := strings.TrimSpace(attrVal)
	switch strings.ToLower(attrType) {
	case "string", "":
		// String type: attr value is used as-is.
		return v, true
	case "<color>":
		// Must parse as a CSS color.
		if _, ok := ParseColor(v); ok {
			return v, true
		}
		return "", false
	case "<length>":
		// Must parse as a CSS length with a unit (bare number is NOT a valid length).
		// CSS Values 4 §6.1.2: a <length> requires a unit unless the value is 0.
		return validateCSSLength(v)
	case "<percentage>":
		return validateCSSPercentage(v)
	case "<length-percentage>":
		if resolved, ok := validateCSSLength(v); ok {
			return resolved, true
		}
		return validateCSSPercentage(v)
	case "<number>":
		return validateCSSNumber(v)
	case "<integer>":
		return validateCSSInteger(v)
	default:
		// Unsupported type — return failure so fallback is used.
		return "", false
	}
}

// validateCSSLength checks whether v is a valid CSS <length>. Returns the value
// as-is if valid, else ("", false). A bare integer/float without a unit is only
// valid as a length if the value is exactly 0 (CSS Values 4 §6.2).
func validateCSSLength(v string) (string, bool) {
	if v == "0" {
		return "0px", true
	}
	// Must end in a recognized length unit.
	units := []string{"px", "em", "rem", "ex", "ch", "vw", "vh", "vmin", "vmax",
		"cm", "mm", "in", "pt", "pc", "Q", "lh", "rlh", "svh", "svw", "dvh", "dvw", "lvh", "lvw"}
	lower := strings.ToLower(v)
	for _, u := range units {
		if strings.HasSuffix(lower, u) {
			numStr := v[:len(v)-len(u)]
			if _, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
				return v, true
			}
		}
	}
	return "", false
}

// validateCSSPercentage checks whether v is a valid CSS <percentage>.
func validateCSSPercentage(v string) (string, bool) {
	if !strings.HasSuffix(v, "%") {
		return "", false
	}
	numStr := strings.TrimSpace(v[:len(v)-1])
	if _, err := strconv.ParseFloat(numStr, 64); err == nil {
		return v, true
	}
	return "", false
}

// validateCSSNumber checks whether v is a valid CSS <number> (integer or decimal).
func validateCSSNumber(v string) (string, bool) {
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v, true
	}
	return "", false
}

// validateCSSInteger checks whether v is a valid CSS <integer>.
func validateCSSInteger(v string) (string, bool) {
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return v, true
	}
	return "", false
}

// Phase 22: Added viewport dimensions for media query evaluation
func ApplyStylesToDocument(doc *html.Document, viewportWidth, viewportHeight float64) map[*html.Node]*Style {
	styles := make(map[*html.Node]*Style)

	// Parse all stylesheets
	stylesheets := ParseDocumentStylesheets(doc)

	// Build the document's ParserContext once — used to resolve inline
	// `style=""` url() tokens at the same parse-time chokepoint that
	// ParseStylesheet uses for embedded sheets.
	ctx := NewParserContextFromDocument(doc)

	// Recursively apply styles to all nodes
	applyStylesToNode(doc.Root, stylesheets, styles, viewportWidth, viewportHeight, ctx)

	// Propagate the owning document's BaseDir onto every Style — see
	// the Style.BaseDir doc for the parse-time URL resolution it feeds.
	if doc.BaseDir != "" {
		for _, style := range styles {
			style.BaseDir = doc.BaseDir
		}
	}

	return styles
}

// ParseDocumentStylesheets parses all stylesheets from a document and
// returns the cached parses. Per-document caching (StyleSheetContents)
// lets the 4-6 callers within one layout pass share a single parse;
// see StyleSheetContents for the Blink-aligned wrapper.
func ParseDocumentStylesheets(doc *html.Document) []*Stylesheet {
	sheets := getOrBuildStyleSheetContents(doc)
	out := make([]*Stylesheet, 0, len(sheets))
	for _, sheet := range sheets {
		parsed, err := sheet.ParsedStylesheet()
		if err == nil {
			out = append(out, parsed)
		}
	}
	return out
}

// getOrBuildStyleSheetContents returns the cached []*StyleSheetContents for
// doc, rebuilding it if Stylesheets has been mutated since the last call
// (length or any source text/href differs, or doc.BaseDir changed).
//
// Each entry's ParserContext carries:
//   - BaseDir, derived per-source:
//   - <style> block (Href empty): BaseDir = doc.BaseDir.
//   - <link href="X">: BaseDir = url.URLDir(url.CompleteURL(X, doc.BaseDir))
//     so url() refs inside the external sheet resolve against the sheet's
//     own location, not the owning document's.
//   - Fetcher = doc.CSSResourceFetcher, so @import resolution
//     (LOU-138 phase 6) loads imported sheets through the same fetcher
//     the HTML parser used for <link rel=stylesheet> fetches.
//
// The wrappers parse lazily on first ParsedStylesheet() call, so this
// function is cheap on the hot path — only the source-slice equality check
// runs per call.
func getOrBuildStyleSheetContents(doc *html.Document) []*StyleSheetContents {
	if cached, ok := doc.ParsedStylesheetsCache.([]*StyleSheetContents); ok && styleSheetCacheMatches(cached, doc.Stylesheets, doc.BaseDir) {
		return cached
	}
	sheets := make([]*StyleSheetContents, len(doc.Stylesheets))
	for i, source := range doc.Stylesheets {
		ctx := NewParserContext(sheetBaseDir(source, doc.BaseDir))
		ctx.Fetcher = doc.CSSResourceFetcher
		sheets[i] = NewStyleSheetContents(source.Text, ctx)
		sheets[i].Href = source.Href
	}
	doc.ParsedStylesheetsCache = sheets
	return sheets
}

// sheetBaseDir computes the per-sheet ParserContext BaseDir for a single
// source. Mirrors Blink's `CSSStyleSheet::BaseURL`
// (third_party/blink/renderer/core/css/css_style_sheet.cc:519 @
// chromium-main d4ecdfed88f962439247c2ad36b8fe47805b1520): external
// sheets resolve url() refs against their own response URL, not the
// owning document's.
func sheetBaseDir(source html.StylesheetSource, docBaseDir string) string {
	if source.Href == "" {
		return docBaseDir
	}
	return urlpkg.URLDir(urlpkg.CompleteURL(source.Href, docBaseDir))
}

// styleSheetCacheMatches reports whether each cached StyleSheetContents
// still wraps the same source (Text + Href) at the same index, and the
// document's BaseDir hasn't shifted. Per-field equality (not pointer
// identity) so that an HTML re-parse that produces the same sources
// doesn't force a re-parse of the CSS.
func styleSheetCacheMatches(cached []*StyleSheetContents, sources []html.StylesheetSource, docBaseDir string) bool {
	return slices.EqualFunc(cached, sources, func(s *StyleSheetContents, src html.StylesheetSource) bool {
		if s.Text != src.Text || s.Href != src.Href {
			return false
		}
		return s.Context.baseDir() == sheetBaseDir(src, docBaseDir)
	})
}

// resolvePseudoInheritKeyword resolves any property whose value is the literal
// `inherit` keyword against the originating element's computed style (the
// pseudo-element's "parent"). Properties the parent does not have are dropped
// so they fall back to their initial value.
func resolvePseudoInheritKeyword(finalStyle *Style, parentStyles []*Style) {
	if len(parentStyles) == 0 || parentStyles[0] == nil {
		return
	}
	parent := parentStyles[0]
	for property, value := range finalStyle.Properties {
		if strings.TrimSpace(value) != "inherit" {
			continue
		}
		if parentVal, ok := parent.Get(property); ok {
			finalStyle.Set(property, parentVal)
		} else {
			delete(finalStyle.Properties, property)
		}
	}
}

// Phase 11: ComputePseudoElementStyle computes the style for a pseudo-element
// Phase 22: Added viewport dimensions for media query evaluation
func ComputePseudoElementStyle(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64, parentStyles ...*Style) *Style {
	finalStyle := NewStyle()

	// Inherit inheritable properties from parent element
	if len(parentStyles) > 0 && parentStyles[0] != nil {
		parent := parentStyles[0]
		inheritableProps := []string{"font-size", "font-family", "font-weight", "font-style",
			"color", "line-height", "text-align", "white-space", "visibility",
			"letter-spacing", "word-spacing", "text-indent", "text-transform",
			// CSS Lists §2: the list-style properties are inherited. A
			// ::before / ::after pseudo-element with display:list-item is
			// itself a list item and must pick up the originating subtree's
			// list-style-type / -position / -image for its own ::marker box.
			"list-style-type", "list-style-position", "list-style-image",
			// CSS Writing Modes §2-3: writing-mode, direction and
			// text-orientation are inherited. A pseudo-element (notably a
			// ::marker laid out as a real box) must inherit them so its
			// inline/block axes and glyph orientation match the originating
			// subtree (marker-foundation Phase 5: writing-mode-correct markers).
			"writing-mode", "direction", "text-orientation"}
		for _, prop := range inheritableProps {
			if val, ok := parent.Get(prop); ok {
				finalStyle.Set(prop, val)
			}
		}
		// CSS Custom Properties for Cascading Variables §3: Custom properties
		// inherit by default. Pseudo-elements inherit custom properties from
		// their originating element so that var() references in the pseudo-
		// element's declarations resolve correctly.
		for prop, val := range parent.Properties {
			if strings.HasPrefix(prop, "--") {
				if _, ok := finalStyle.Get(prop); !ok {
					finalStyle.Set(prop, val)
				}
			}
		}
	}

	// Collect all matching rules for this pseudo-element
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			// Phase 22: Check media query (Kleene 3-valued — apply only when
			// the result is definitely true; Unknown does not apply). Combines
			// the rule's @media wrapper with any @import-level media-query-list
			// (CSS Cascade 4 §3.1).
			if !RuleMediaApplies(&rule, viewportWidth, viewportHeight) {
				continue
			}

			// Check if this rule's selector matches the node AND has the right pseudo-element
			rulePseudo := rule.Selector.PseudoElement

			// Handle "descendant:" prefix - these pseudo-elements apply to descendants only
			if strings.HasPrefix(rulePseudo, "descendant:") {
				actualPseudo := strings.TrimPrefix(rulePseudo, "descendant:")
				if actualPseudo == pseudoElement {
					// For descendant pseudo-elements, check if the node is a descendant of a matching element
					// (not the matching element itself)
					ancestor := node.Parent
					for ancestor != nil {
						if MatchesSelector(ancestor, rule.Selector) {
							allRules = append(allRules, rule)
							break
						}
						ancestor = ancestor.Parent
					}
				}
			} else if rulePseudo == pseudoElement {
				// Direct pseudo-element match
				if MatchesSelector(node, rule.Selector) {
					allRules = append(allRules, rule)
				}
			}
		}
	}

	// Sort rules by cascade order: layer priority (lowest first), then specificity
	layerOrder2 := buildLayerOrder(stylesheets)
	sort.SliceStable(allRules, func(i, j int) bool {
		pi := layerPriority(allRules[i].LayerName, layerOrder2)
		pj := layerPriority(allRules[j].LayerName, layerOrder2)
		if pi != pj {
			return pi < pj
		}
		return allRules[i].Selector.Specificity < allRules[j].Selector.Specificity
	})

	// Track which properties have been set with !important
	importantProps := make(map[string]bool)

	// CSS Pseudo-4 §4.4: a ::marker rule only accepts the marker-allowed
	// property subset. Filter author declarations as they are applied (the
	// inherited values copied in above are not author declarations and are
	// left alone — the restriction is on what a ::marker rule can specify).
	isMarker := pseudoElement == "marker"

	// Capture the pre-author baseline for `revert` resolution (mirrors
	// ComputeStyle). For pseudo-elements there's no UA + presentational pass
	// — the inheritance-only initialisation above is the closest analog —
	// so this snapshot is the inherited-only baseline. `revert-layer` uses
	// per-layer snapshots captured during iteration; layerSnapshots is
	// populated during the normal pass and reused by the !important pass
	// (CSS Cascade 5 §6.1.3 — see the matching block in ComputeStyle for
	// the full rationale).
	pseudoOriginSnap := snapshotForAllRevert(finalStyle)
	pseudoLayerSnap := snapshotForAllRevert(finalStyle)
	pseudoLayerSnapshots := make(map[string]allShorthandRevertSnapshot)
	currentLayerPriority := -1

	// Apply rules in order (normal declarations first). Within a rule, `all`
	// runs before sibling longhand declarations so author idiom
	// `all: unset; color: green;` lets color win — see ComputeStyle for the
	// same ordering rationale. The visited-link filter applies here too,
	// though :visited pseudo-elements are unusual (typically pseudo-elements
	// don't combine with :visited in practice).
	for _, rule := range allRules {
		rulePriority := layerPriority(rule.LayerName, layerOrder2)
		if rulePriority != currentLayerPriority {
			if currentLayerPriority != -1 {
				resolveRevertLayerOnly(finalStyle, pseudoOriginSnap, pseudoLayerSnap)
			}
			pseudoLayerSnap = snapshotForAllRevert(finalStyle)
			pseudoLayerSnapshots[rule.LayerName] = pseudoLayerSnap
			currentLayerPriority = rulePriority
		}
		visitedOnly := selectorMatchesVisited(rule.Selector)
		if allVal, hasAll := rule.Declarations["all"]; hasAll {
			if !importantProps["all"] && !(isMarker && !markerAllowedProperty("all")) {
				applyDeclarationWithVisitedFilter(finalStyle, "all", allVal, visitedOnly)
			}
		}
		for property, value := range rule.Declarations {
			if property == "all" {
				continue
			}
			// Skip if already set by an important rule
			if importantProps[property] {
				continue
			}
			if isMarker && !markerAllowedProperty(property) {
				continue
			}
			applyDeclarationWithVisitedFilter(finalStyle, property, value, visitedOnly)
		}
	}
	if currentLayerPriority != -1 {
		resolveRevertLayerOnly(finalStyle, pseudoOriginSnap, pseudoLayerSnap)
	}

	// Apply !important declarations in REVERSED layer order per CSS Cascade
	// 5 §6.4.1 step 4. Reuse the normal-pass per-layer snapshots for any
	// revert-layer encountered (declaration-order semantics — see the
	// matching block in ComputeStyle).
	pseudoImportantRules := make([]Rule, 0, len(allRules))
	for _, rule := range allRules {
		if len(rule.Important) > 0 {
			pseudoImportantRules = append(pseudoImportantRules, rule)
		}
	}
	sort.SliceStable(pseudoImportantRules, func(i, j int) bool {
		pi := importantLayerPriority(pseudoImportantRules[i].LayerName, layerOrder2)
		pj := importantLayerPriority(pseudoImportantRules[j].LayerName, layerOrder2)
		if pi != pj {
			return pi < pj
		}
		return pseudoImportantRules[i].Selector.Specificity < pseudoImportantRules[j].Selector.Specificity
	})
	pseudoCurrentImportantPri := -1
	var pseudoCurrentImportantSnap allShorthandRevertSnapshot
	pseudoFlushImportant := func() {
		if pseudoCurrentImportantSnap != nil {
			resolveRevertLayerOnly(finalStyle, pseudoOriginSnap, pseudoCurrentImportantSnap)
		}
	}
	for _, rule := range pseudoImportantRules {
		rulePri := importantLayerPriority(rule.LayerName, layerOrder2)
		if rulePri != pseudoCurrentImportantPri {
			pseudoFlushImportant()
			snap, ok := pseudoLayerSnapshots[rule.LayerName]
			if !ok {
				snap = snapshotForAllRevert(finalStyle)
			}
			pseudoCurrentImportantSnap = snap
			pseudoCurrentImportantPri = rulePri
		}
		visitedOnly := selectorMatchesVisited(rule.Selector)
		if rule.Important["all"] {
			if allVal, hasAll := rule.Declarations["all"]; hasAll {
				if !(isMarker && !markerAllowedProperty("all")) {
					applyDeclarationWithVisitedFilter(finalStyle, "all", allVal, visitedOnly)
					importantProps["all"] = true
				}
			}
		}
		for property, value := range rule.Declarations {
			if property == "all" {
				continue
			}
			if rule.Important[property] {
				if isMarker && !markerAllowedProperty(property) {
					continue
				}
				applyDeclarationWithVisitedFilter(finalStyle, property, value, visitedOnly)
				importantProps[property] = true
			}
		}
	}
	pseudoFlushImportant()

	// CSS Pseudo-4 §4.4: the ::marker pseudo-element only accepts the
	// marker-allowed property subset, and the UA stylesheet sets specific
	// defaults. Apply the filter to the cascaded author declarations, then
	// layer the UA defaults underneath any author value. Done before the
	// generic display:inline default so the ::marker UA display still wins
	// only if the author did not set it.
	if pseudoElement == "marker" {
		applyMarkerCascade(finalStyle)
	}

	// Resolve longhand-level CSS-wide keywords for the pseudo-element style.
	// Same rationale as ComputeStyle: must happen before resolvePseudoInheritKeyword
	// (which is `inherit`-specific) so that `unset` first rewrites itself to
	// `inherit` for inheritable properties before that pass runs.
	resolveCSSWideKeywords(finalStyle, pseudoOriginSnap, pseudoLayerSnap)

	// CSS Cascade §7.3: the `inherit` keyword resolves to the parent's computed
	// value. For pseudo-elements the "parent" is the originating element. This
	// must run before ApplyAnimations so that e.g. `animation-delay: inherit`
	// is a real time value when the keyframe pass reads it.
	resolvePseudoInheritKeyword(finalStyle, parentStyles)

	// CSS Animations §4: apply in-effect @keyframes values to the
	// pseudo-element's computed style (same cascade position as for elements).
	ApplyAnimations(finalStyle, collectKeyframesMaps(stylesheets), importantProps)

	// An animated declaration may itself be the `inherit` keyword (e.g.
	// @keyframes kf { from,to { font-size: inherit } }) — resolve again so the
	// animation's `inherit` is computed against the originating element.
	resolvePseudoInheritKeyword(finalStyle, parentStyles)

	// CSS 2.1 §15.7: resolve a font-relative font-size (em / %) on the
	// pseudo-element against the originating element's computed font-size. For
	// regular elements ApplyInheritedProperties does this; pseudo-elements
	// take their inherited font-size from the originating element. This must
	// run after ApplyAnimations so an animated `font-size: 1em` is resolved
	// too (e.g. inheritance-pseudo-element.html).
	if len(parentStyles) > 0 && parentStyles[0] != nil {
		if fsVal, has := finalStyle.Get("font-size"); has {
			trimmed := strings.TrimSpace(fsVal)
			parentFS := parentStyles[0].GetFontSize()
			if strings.HasSuffix(trimmed, "%") {
				if pct, ok := ParsePercentage(trimmed); ok {
					finalStyle.Set("font-size", fmt.Sprintf("%.6gpx", pct/100.0*parentFS))
				}
			} else if strings.HasSuffix(trimmed, "em") && !strings.HasSuffix(trimmed, "rem") {
				if resolved, ok := ParseLengthWithFontSize(fsVal, parentFS); ok {
					finalStyle.Set("font-size", fmt.Sprintf("%.6gpx", resolved))
				}
			}
		}
	}

	// CSS 2.1 §12.1: Pseudo-elements default to display:inline unless
	// explicitly set by a rule. GetDisplay() defaults to DisplayBlock for
	// regular elements, but pseudo-elements are inline by default.
	if _, ok := finalStyle.Get("display"); !ok {
		finalStyle.Set("display", "inline")
	}

	// Store viewport dimensions for viewport unit resolution
	finalStyle.ViewportWidth = viewportWidth
	finalStyle.ViewportHeight = viewportHeight

	return finalStyle
}

// markerAllowedProperty reports whether a property may be specified on a
// ::marker pseudo-element. CSS Pseudo-4 §4.4: the ::marker rule only accepts
// color, direction, the font-* longhands/shorthand, content,
// text-combine-upright, unicode-bidi, white-space, the *-spacing properties,
// line-height, text-shadow, text-transform, and the animation-*/transition-*
// properties. Custom properties (--*) and the internal viewport tracking
// keys are always allowed (custom props inherit; var() must resolve).
func markerAllowedProperty(name string) bool {
	if strings.HasPrefix(name, "--") {
		return true
	}
	if strings.HasPrefix(name, "font-") ||
		strings.HasPrefix(name, "animation-") ||
		strings.HasPrefix(name, "transition-") {
		return true
	}
	switch name {
	case "color", "direction", "font", "content",
		"text-combine-upright", "unicode-bidi", "white-space",
		"letter-spacing", "word-spacing", "line-height",
		"text-shadow", "text-transform", "animation", "transition":
		// CSS Pseudo-4 §4.4: `display` is NOT marker-allowed — an author
		// `::marker { display: ... }` rule must be rejected. The engine's own
		// ::marker box display (inside = inline default, outside = inline-block)
		// is stamped outside this author-property filter, in the layout tree
		// builder (createMarkerPseudoElement).
		return true
	}
	return false
}

// applyMarkerCascade layers the UA ::marker defaults underneath any surviving
// author value of an already-cascaded ::marker style. Mirrors Blink's UA
// ::marker rule: unicode-bidi: isolate; text-transform: none; white-space:
// pre; font-variant-numeric: tabular-nums. The marker-allowed property filter
// is applied during rule application in ComputePseudoElementStyle, not here.
func applyMarkerCascade(style *Style) {
	uaDefaults := [...][2]string{
		{"unicode-bidi", "isolate"},
		{"text-transform", "none"},
		{"white-space", "pre"},
		{"font-variant-numeric", "tabular-nums"},
	}
	for _, kv := range uaDefaults {
		if _, ok := style.Get(kv[0]); !ok {
			style.Set(kv[0], kv[1])
		}
	}
}

// HasFirstLetterRules returns true if any stylesheet rules with ::first-letter
// pseudo-element match the given node.
func HasFirstLetterRules(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.Selector.PseudoElement != "first-letter" {
				continue
			}
			if !RuleMediaApplies(&rule, viewportWidth, viewportHeight) {
				continue
			}
			if MatchesSelector(node, rule.Selector) {
				return true
			}
		}
	}
	return false
}

// HasFirstLineRules returns true if any stylesheet rules with ::first-line
// pseudo-element match the given node, indicating first-line styling is needed.
func HasFirstLineRules(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.Selector.PseudoElement != "first-line" {
				continue
			}
			if !RuleMediaApplies(&rule, viewportWidth, viewportHeight) {
				continue
			}
			if MatchesSelector(node, rule.Selector) {
				return true
			}
		}
	}
	return false
}

// HasPseudoElementRules returns true if any stylesheet rules with the given
// pseudo-element match the given node.
func HasPseudoElementRules(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			pseudo := rule.Selector.PseudoElement
			if pseudo != pseudoElement {
				// Also check for "descendant:" prefixed pseudo-elements.
				if !strings.HasPrefix(pseudo, "descendant:") || strings.TrimPrefix(pseudo, "descendant:") != pseudoElement {
					continue
				}
			}
			if !RuleMediaApplies(&rule, viewportWidth, viewportHeight) {
				continue
			}
			if pseudo == pseudoElement {
				if MatchesSelector(node, rule.Selector) {
					return true
				}
			} else {
				// Descendant pseudo-element: check if node is a descendant.
				ancestor := node.Parent
				for ancestor != nil {
					if MatchesSelector(ancestor, rule.Selector) {
						return true
					}
					ancestor = ancestor.Parent
				}
			}
		}
	}
	return false
}

// resolveInheritValues resolves any "inherit" keyword values by copying from the parent's computed style.
func resolveInheritValues(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	for property, value := range style.Properties {
		if value != "inherit" {
			continue
		}
		// Look up parent's computed style
		if node.Parent != nil {
			if parentStyle, ok := styles[node.Parent]; ok {
				if parentVal, ok := parentStyle.Get(property); ok {
					style.Set(property, parentVal)
					continue
				}
			}
		}
		// No parent or parent doesn't have the property: remove the inherit value
		// so the property falls back to its default
		delete(style.Properties, property)
	}
}

// inheritableProperties lists CSS properties that inherit from parent to child by default
var inheritableProperties = map[string]bool{
	"color": true, "font-family": true, "font-size": true,
	"font-style": true, "font-weight": true, "font-variant": true,
	"font-feature-settings": true, "font-kerning": true,
	// CSS Fonts 4 §6.4: font-variant-ligatures and font-variant-numeric
	// inherit by default. Mirrors Blink's FontDescription propagation via
	// inherited ComputedStyle (font_description.h::VariantLigatures /
	// VariantNumeric fields, Chromium SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	"font-variant-ligatures": true,
	"font-variant-numeric":   true,
	// CSS Fonts 4 §6.6: each font-synthesis-* longhand is inherited.
	// https://drafts.csswg.org/css-fonts-4/#font-synthesis-weight
	"font-synthesis-weight": true, "font-synthesis-style": true,
	"font-synthesis-small-caps": true, "font-synthesis-position": true,
	// NOTE: text-decoration is intentionally NOT inherited (CSS Text Decor 3
	// §2). Instead, ResolveAppliedTextDecorations accumulates an
	// []AppliedTextDecoration vector from parent → child. See
	// applied_text_decoration.h:18-50 (Blink SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	"line-height": true, "text-align": true,
	"text-transform": true, "text-indent": true, "white-space": true,
	"visibility": true, "list-style-type": true, "list-style-position": true, "list-style-image": true,
	"direction": true, "letter-spacing": true, "word-spacing": true,
	"cursor": true, "writing-mode": true, "text-orientation": true,
	"empty-cells": true,
	// CSS 2.1 §17.6: border-collapse is inherited (table → cell). Paint
	// uses the cell's own GetBorderCollapse() to decide empty-cells
	// suppression and other collapsed-only behavior, so it must see the
	// value cascaded from the parent <table>.
	"border-collapse": true,
	// CSS Text 3 inherited properties:
	"word-break": true, "overflow-wrap": true, "hyphens": true,
	"line-break": true, "tab-size": true,
	// CSS Text Decor 4: skip-spaces is inherited (unlike the other
	// text-decoration-* longhands which accumulate via
	// AppliedTextDecorations). See
	// https://drafts.csswg.org/css-text-decor-4/#text-decoration-skip-spaces-property
	"text-decoration-skip-spaces": true,
	// CSS Text Decor 4 §1.1.5: text-decoration-skip-ink is inherited.
	// https://drafts.csswg.org/css-text-decor-4/#text-decoration-skip-ink-property
	"text-decoration-skip-ink": true,
	// CSS Generated Content L3 §3.2 `quotes` is inherited; the
	// open-quote/close-quote pair lookup in pseudo-element content
	// reads `quotes` off the originating element, which must see the
	// ancestor declaration (the css-contain quote-scoping tests set
	// `quotes` on the container and observe open-quote behavior on
	// descendant pseudo-elements).
	"quotes": true,
}

// ApplyInheritedProperties copies inheritable properties from parent if not set on child.
// Also resolves font-size em values using parent's computed font-size.
// ApplyInheritedProperties applies inherited CSS properties from parent to child
func ApplyInheritedProperties(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	if node.Parent == nil {
		return
	}
	parentStyle, ok := styles[node.Parent]
	if !ok {
		return
	}

	// Resolve font-size em, lh, and percentage values using parent's metrics.
	// Parent has already been processed (top-down cascade), so its font-size
	// is resolved to an absolute px value. This propagates through to
	// children so 1em and 100% both resolve correctly per CSS 2.1 §15.7.
	// For lh: CSS Values 4 §6.1 says "when used in font-size, lh refers to
	// the computed metrics of the parent element". Mirrors Blink's cascade
	// behavior in css_to_length_conversion_data.cc at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if fsVal, hasFontSize := style.Get("font-size"); hasFontSize {
		trimmed := strings.TrimSpace(fsVal)
		parentFS := 16.0
		if parentStyle != nil {
			parentFS = parentStyle.GetFontSize()
		}
		if strings.HasSuffix(trimmed, "%") {
			if pct, ok := ParsePercentage(trimmed); ok {
				style.Set("font-size", fmt.Sprintf("%.6gpx", pct/100.0*parentFS))
			}
		} else if strings.HasSuffix(trimmed, "em") && !strings.HasSuffix(trimmed, "rem") {
			if resolved, ok := ParseLengthWithFontSize(fsVal, parentFS); ok {
				style.Set("font-size", fmt.Sprintf("%.6gpx", resolved))
			}
		} else if strings.HasSuffix(trimmed, "lh") {
			// lh in font-size resolves against the parent's computed line-height.
			if parentStyle != nil {
				if num, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "lh"), 64); err == nil {
					parentLh := parentStyle.GetLineHeight()
					style.Set("font-size", fmt.Sprintf("%.6gpx", num*parentLh))
				}
			}
		}
	}

	// CSS Custom Properties (--*) inherit by default (CSS Custom Properties §2.2).
	// Custom-property inheritance must run BEFORE inheritableProperties because
	// any inheritable property whose declared value contains a var() reference
	// needs the child's custom-property table populated to resolve correctly.
	// Otherwise a span with `color: var(--a)` whose --a was unset and is being
	// reinherited from the body would see an empty --a at color-resolution
	// time, trigger IACVT, and fall back to the *parent's* `color` (which may
	// not be the intended value). Order swap mirrors Blink's StyleResolver
	// processing custom properties first in
	// CSSPropertyValueSet::FilterPropertiesAndCustomProperties at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	for prop, val := range parentStyle.Properties {
		if strings.HasPrefix(prop, "--") {
			if _, hasOwn := style.Properties[prop]; !hasOwn {
				style.Properties[prop] = val
			}
		}
	}

	for prop := range inheritableProperties {
		if _, hasOwn := style.Get(prop); !hasOwn {
			if parentVal, ok := parentStyle.Get(prop); ok {
				style.Set(prop, parentVal)
				// Track that writing-mode was inherited, not explicitly set.
				// This allows resolveLogicalSizeProperties to skip the
				// vertical-mode resolution for inherited values, since the
				// layout engine's transformToVerticalRL handles positioning
				// as a post-pass and expects children to use horizontal dimensions.
				if prop == "writing-mode" {
					style.Set("_writing-mode-inherited", "true")
				}
			}
		}
	}

	// HTML 15.3.4: resolve the `-internal-center` sentinel applied by the UA
	// stylesheet to `<th>`. If the inherited (= parent's computed) text-align
	// is `start` (the initial value), the th's text-align becomes `center`.
	// Otherwise it inherits the parent's explicit value. This makes
	// `text-align: end` on a containing table propagate to th cells rather
	// than being overridden by the UA default. Mirrors Blink's html.css rule
	// `th { text-align: -internal-center; }` and its post-cascade resolution
	// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if val, ok := style.Get("text-align"); ok && val == "-internal-center" {
		parentTA, _ := parentStyle.Get("text-align")
		// Treat unset / explicit `start` / `-internal-center` (from a
		// nested th inside a th — pathological but defensible) all as
		// "parent at initial value".
		switch parentTA {
		case "", "start", "-internal-center":
			style.Set("text-align", "center")
		default:
			style.Set("text-align", parentTA)
		}
	}
}

// ApplyInheritedFrom copies inheritable CSS properties from a parent style to
// a child style when the child hasn't explicitly set them. This is used during
// intrinsic sizing when css.ComputeStyle is called standalone (without the full
// document style tree) and inherited properties like font-family are missing.
func ApplyInheritedFrom(child, parent *Style) {
	if child == nil || parent == nil {
		return
	}
	for prop := range inheritableProperties {
		if _, hasOwn := child.Get(prop); !hasOwn {
			if parentVal, ok := parent.Get(prop); ok {
				child.Set(prop, parentVal)
			}
		}
	}
}

// NewBlockifiedStyle creates a minimal anonymous block wrapper style for
// block-in-inline splitting. When all inline continuations are suppressed
// (whitespace-only), the extracted block children are wrapped in this style
// so that non-inheritable stacking context properties (opacity, transform,
// filter) from the inline are preserved. Backgrounds, borders, padding, and
// margins are NOT copied — they belong only to the inline's own box edges.
//
// position:relative/sticky + top/right/bottom/left are also preserved so the
// blocks inside a positioned inline shift with the inline (CSS 2.1 §9.4.3).
func NewBlockifiedStyle(inline *Style) *Style {
	s := NewAnonymousBlockStyle(inline)
	// Preserve stacking-context-creating non-inheritable properties.
	for _, prop := range []string{"opacity", "transform", "filter", "isolation", "will-change", "position", "top", "right", "bottom", "left"} {
		if val, ok := inline.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousBlockStyle creates a style for an anonymous block box
// (CSS 2.1 §9.2.1.1). Anonymous boxes inherit all inheritable properties
// from the parent and have display:block with zero margin/border/padding.
func NewAnonymousBlockStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.XHeight = parent.XHeight
	s.CapHeight = parent.CapHeight
	s.LhSize = parent.LhSize
	s.Set("display", "block")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	s.inheritAppliedTextDecorationsFrom(parent)
	return s
}

// inheritAppliedTextDecorationsFrom copies the parent's accumulated
// text-decoration vector onto an anonymous box's style. text-decoration is
// not a CSS-inheritable property — it propagates via the cascade's
// decorating-box rule (CSS Text Decor 3 §2.1), which
// ResolveAppliedTextDecorations applies only to DOM-tree nodes during the
// cascade phase. Anonymous boxes (block-in-inline split, blockification,
// table wrappers, ruby wrappers) are created post-cascade by the layout-tree
// builder, so they never go through that resolver. Without this explicit
// copy, descendant text inside an anonymous box would see an empty vector
// and miss its splitting box's decoration — the LOU-151 bug observed on
// `text-decoration-subelements-005`.
//
// Mirrors Blink's `StyleResolver::CreateAnonymousStyleWithDisplay` which
// carries the `AppliedTextDecoration` vector through to anonymous wrappers
// (Blink ref @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Companion to
// the cascade-time method ResolveAppliedTextDecorations in style.go.
func (s *Style) inheritAppliedTextDecorationsFrom(parent *Style) {
	if len(parent.AppliedTextDecorations) > 0 {
		s.AppliedTextDecorations = append([]AppliedTextDecoration(nil), parent.AppliedTextDecorations...)
	}
}

// NewAnonymousTableCellStyle creates a style for an anonymous table-cell box
// that inherits from the parent row style. Per CSS Tables §2.1, when a
// non-table-cell child appears inside a table-row, it must be wrapped in
// an anonymous table-cell box.
func NewAnonymousTableCellStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.XHeight = parent.XHeight
	s.CapHeight = parent.CapHeight
	s.LhSize = parent.LhSize
	s.Set("display", "table-cell")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	s.inheritAppliedTextDecorationsFrom(parent)
	return s
}

// NewAnonymousTableRowStyle creates a style for an anonymous table-row box.
// Per CSS 2.1 §17.2.1, non-row children of a table-row-group (and bare
// non-row siblings of rows in a table) are wrapped in anonymous table-row
// boxes. Mirrors Blink's LayoutTableRow::CreateAnonymousWithParent.
func NewAnonymousTableRowStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.XHeight = parent.XHeight
	s.CapHeight = parent.CapHeight
	s.LhSize = parent.LhSize
	s.Set("display", "table-row")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	s.inheritAppliedTextDecorationsFrom(parent)
	return s
}

// NewAnonymousInlineRubyStyle creates a style for the single anonymous
// inline `display: ruby` box that wraps the children of a block-level
// `display: block ruby` element (CSS Display L3 + Blink's
// LayoutRubyAsBlock two-box model). The principal box is the
// block-flow generated from the block-ruby element itself; this style
// is for the inline-ruby child that actually carries the ruby column
// items.
//
// Per Blink layout_ruby_as_block.cc, the inline ruby child inherits
// all inheritable properties from the principal box; non-inheritable
// box properties (margin/border/padding) live on the principal block
// box, not on the inline ruby. Mirrors NewAnonymousBlockStyle but with
// display:ruby.
func NewAnonymousInlineRubyStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.XHeight = parent.XHeight
	s.CapHeight = parent.CapHeight
	s.LhSize = parent.LhSize
	s.Set("display", "ruby")
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	s.inheritAppliedTextDecorationsFrom(parent)
	return s
}

// NewAnonymousTableRowGroupStyle creates a style for an anonymous
// table-row-group box. Per CSS 2.1 §17.2.1, a run of non-proper-table
// children of a table/inline-table is wrapped in an anonymous
// table-row-group. Mirrors Blink's LayoutTableSection::CreateAnonymousWithParent.
func NewAnonymousTableRowGroupStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.XHeight = parent.XHeight
	s.CapHeight = parent.CapHeight
	s.LhSize = parent.LhSize
	s.Set("display", "table-row-group")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	s.inheritAppliedTextDecorationsFrom(parent)
	return s
}

// resolveOrthogonalDisplay implements CSS Writing Modes §2.1:
// If an inline-level box has a different writing-mode axis than its containing
// block, its display value computes to inline-block. This causes inline spans
// with orthogonal writing-mode to honor width/height properties.
func resolveOrthogonalDisplay(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	if node == nil || node.Parent == nil {
		return
	}
	// Only apply to inline-level elements
	display, hasDisplay := style.Get("display")
	if !hasDisplay || display != "inline" {
		return
	}
	// Get this element's writing-mode axis
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr"
	// Get parent's writing-mode axis
	parentStyle, ok := styles[node.Parent]
	if !ok || parentStyle == nil {
		// No parent style: assume horizontal-tb (the default)
		// If this element is vertical, it's orthogonal → promote to inline-block
		if isVertical {
			style.Set("display", "inline-block")
		}
		return
	}
	parentWM, _ := parentStyle.Get("writing-mode")
	parentIsVertical := parentWM == "vertical-rl" || parentWM == "vertical-lr" || parentWM == "sideways-rl" || parentWM == "sideways-lr"
	// If the axes differ, promote inline to inline-block
	if isVertical != parentIsVertical {
		style.Set("display", "inline-block")
	}
}

// resolveLogicalSizeProperties remaps logical size properties (inline-size,
// block-size, and their min/max variants) to the correct physical properties
// based on the element's computed writing-mode.
//
// During shorthand expansion, inline-size is mapped to width (the default for
// horizontal-tb). For vertical writing modes (vertical-rl, vertical-lr,
// sideways-rl, sideways-lr), the inline axis is vertical, so inline-size must
// map to height instead. This function fixes the mapping after the writing-mode
// has been fully resolved (including inheritance).
func resolveLogicalSizeProperties(style *Style) {
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr"
	if !isVertical {
		return // horizontal-tb: default mapping is correct
	}
	// Remap for both explicitly-set and inherited writing-mode — logical
	// axes are always computed from the element's used writing-mode.

	// For vertical writing modes:
	//   inline-size -> height (not width)
	//   block-size  -> width  (not height)
	// The expandShorthand stored the logical values under _inline-size / _block-size
	// markers and mapped them to width/height assuming horizontal-tb. We now need
	// to swap them.

	inlineVal, hasInline := style.Get("_inline-size")
	blockVal, hasBlock := style.Get("_block-size")

	if hasInline && hasBlock {
		// Both set: swap width <-> height
		style.Set("height", inlineVal)
		style.Set("width", blockVal)
	} else if hasInline {
		// Only inline-size was set: move from width to height.
		// Clear width if it was set only by inline-size (check if there's
		// an independent width declaration by comparing values).
		style.Set("height", inlineVal)
		if curW, ok := style.Get("width"); ok && curW == inlineVal {
			delete(style.Properties, "width")
		}
	} else if hasBlock {
		// Only block-size was set: move from height to width.
		style.Set("width", blockVal)
		if curH, ok := style.Get("height"); ok && curH == blockVal {
			delete(style.Properties, "height")
		}
	}

	// Same for min-inline-size / min-block-size
	minInlineVal, hasMinInline := style.Get("_min-inline-size")
	minBlockVal, hasMinBlock := style.Get("_min-block-size")

	if hasMinInline && hasMinBlock {
		style.Set("min-height", minInlineVal)
		style.Set("min-width", minBlockVal)
	} else if hasMinInline {
		style.Set("min-height", minInlineVal)
		if curMW, ok := style.Get("min-width"); ok && curMW == minInlineVal {
			delete(style.Properties, "min-width")
		}
	} else if hasMinBlock {
		style.Set("min-width", minBlockVal)
		if curMH, ok := style.Get("min-height"); ok && curMH == minBlockVal {
			delete(style.Properties, "min-height")
		}
	}

	// Same for max-inline-size / max-block-size
	maxInlineVal, hasMaxInline := style.Get("_max-inline-size")
	maxBlockVal, hasMaxBlock := style.Get("_max-block-size")

	if hasMaxInline && hasMaxBlock {
		style.Set("max-height", maxInlineVal)
		style.Set("max-width", maxBlockVal)
	} else if hasMaxInline {
		style.Set("max-height", maxInlineVal)
		if curMW, ok := style.Get("max-width"); ok && curMW == maxInlineVal {
			delete(style.Properties, "max-width")
		}
	} else if hasMaxBlock {
		style.Set("max-width", maxBlockVal)
		if curMH, ok := style.Get("max-height"); ok && curMH == maxBlockVal {
			delete(style.Properties, "max-height")
		}
	}
}

// resolveLogicalBoxProperties resolves logical margin/padding/border properties
// (e.g. border-inline-start, margin-block-end) to physical properties based on
// the element's computed writing-mode. Must be called after inheritance.
//
// Mapping for horizontal-tb (default):
//
//	inline-start=left, inline-end=right, block-start=top, block-end=bottom
//
// For vertical-rl / vertical-lr:
//
//	inline-start=top, inline-end=bottom, block-start=right(rl)/left(lr), block-end=left(rl)/right(lr)
func resolveLogicalBoxProperties(style *Style) {
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr"
	// CSS Logical Properties Level 1: logical properties resolve based on
	// the element's computed writing-mode, regardless of whether writing-mode
	// was inherited or explicitly set. Previously we forced HTB mapping for
	// inherited writing-mode, but that incorrectly maps UA logical properties
	// (e.g. padding-inline-start on <ul>) to the wrong physical side in
	// vertical writing modes.

	dir, _ := style.Get("direction")
	isRTL := dir == "rtl"

	// Check if any logical markers exist; if not, nothing to resolve
	hasMarkers := false
	for key := range style.Properties {
		if len(key) > 0 && key[0] == '_' {
			hasMarkers = true
			break
		}
	}
	if !hasMarkers {
		return
	}

	// Determine the correct physical mapping for each logical direction.
	// CSS Logical Properties: https://www.w3.org/TR/css-logical-1/#box
	//
	// horizontal-tb + LTR: inline-start=left, inline-end=right, block-start=top, block-end=bottom
	// horizontal-tb + RTL: inline-start=right, inline-end=left, block-start=top, block-end=bottom
	// vertical-rl + LTR: inline-start=top, inline-end=bottom, block-start=right, block-end=left
	// vertical-rl + RTL: inline-start=bottom, inline-end=top, block-start=right, block-end=left
	// vertical-lr + LTR: inline-start=top, inline-end=bottom, block-start=left, block-end=right
	// vertical-lr + RTL: inline-start=bottom, inline-end=top, block-start=left, block-end=right
	// sideways-lr + LTR: inline-start=bottom, inline-end=top, block-start=left, block-end=right
	// sideways-lr + RTL: inline-start=top, inline-end=bottom, block-start=left, block-end=right

	var inlineStartSide, inlineEndSide, blockStartSide, blockEndSide string

	switch {
	case wm == "sideways-lr" && !isRTL:
		inlineStartSide = "bottom"
		inlineEndSide = "top"
		blockStartSide = "left"
		blockEndSide = "right"
	case wm == "sideways-lr" && isRTL:
		inlineStartSide = "top"
		inlineEndSide = "bottom"
		blockStartSide = "left"
		blockEndSide = "right"
	case isVertical && !isRTL:
		inlineStartSide = "top"
		inlineEndSide = "bottom"
		if wm == "vertical-rl" || wm == "sideways-rl" {
			blockStartSide = "right"
			blockEndSide = "left"
		} else {
			blockStartSide = "left"
			blockEndSide = "right"
		}
	case isVertical && isRTL:
		inlineStartSide = "bottom"
		inlineEndSide = "top"
		if wm == "vertical-rl" || wm == "sideways-rl" {
			blockStartSide = "right"
			blockEndSide = "left"
		} else {
			blockStartSide = "left"
			blockEndSide = "right"
		}
	case !isVertical && isRTL:
		// horizontal-tb + RTL
		inlineStartSide = "right"
		inlineEndSide = "left"
		blockStartSide = "top"
		blockEndSide = "bottom"
	default:
		// horizontal-tb + LTR (default)
		inlineStartSide = "left"
		inlineEndSide = "right"
		blockStartSide = "top"
		blockEndSide = "bottom"
	}

	// The expandShorthand always maps to horizontal-tb LTR physical properties:
	//   inline-start -> left, inline-end -> right
	//   block-start -> top, block-end -> bottom
	type logicalMapping struct {
		markerPrefix string // e.g., "_margin-inline-start"
		wrongSide    string // what expandShorthand used (e.g., "left")
		correctSide  string // what it should be (e.g., "top")
	}

	mappings := []logicalMapping{
		{"_margin-inline-start", "left", inlineStartSide},
		{"_margin-inline-end", "right", inlineEndSide},
		{"_margin-block-start", "top", blockStartSide},
		{"_margin-block-end", "bottom", blockEndSide},
		{"_padding-inline-start", "left", inlineStartSide},
		{"_padding-inline-end", "right", inlineEndSide},
		{"_padding-block-start", "top", blockStartSide},
		{"_padding-block-end", "bottom", blockEndSide},
	}

	for _, m := range mappings {
		if val, ok := style.Get(m.markerPrefix); ok {
			propType := strings.TrimPrefix(m.markerPrefix, "_")
			propType = propType[:strings.Index(propType, "-")]
			wrongProp := propType + "-" + m.wrongSide
			correctProp := propType + "-" + m.correctSide

			// Only set the resolved physical property if it wasn't explicitly
			// set by a higher-cascade-priority source (e.g., author stylesheet
			// setting padding-top should not be overridden by UA's logical
			// padding-inline-start resolving to the same physical side).
			if _, alreadySet := style.Get(correctProp); !alreadySet {
				style.Set(correctProp, val)
			}
			if m.wrongSide != m.correctSide {
				if curVal, curOk := style.Get(wrongProp); curOk && curVal == val {
					delete(style.Properties, wrongProp)
				}
			}
			delete(style.Properties, m.markerPrefix)
		}
	}

	// Border properties: same pattern but with -width/-style/-color suffixes
	borderMappings := []logicalMapping{
		{"_border-inline-start", "left", inlineStartSide},
		{"_border-inline-end", "right", inlineEndSide},
		{"_border-block-start", "top", blockStartSide},
		{"_border-block-end", "bottom", blockEndSide},
	}

	for _, m := range borderMappings {
		for _, suffix := range []string{"-width", "-style", "-color"} {
			marker := m.markerPrefix + suffix
			if val, ok := style.Get(marker); ok {
				correctProp := "border-" + m.correctSide + suffix

				if _, alreadySet := style.Get(correctProp); !alreadySet {
					style.Set(correctProp, val)
				}
				delete(style.Properties, marker)
			}
		}
	}
}

// applyStylesToNode recursively applies styles to a node and its children
func applyStylesToNode(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style, viewportWidth, viewportHeight float64, ctx *ParserContext) {
	if node.Type == html.ElementNode && node.TagName != "document" {
		style := ComputeStyle(node, stylesheets, viewportWidth, viewportHeight, ctx)
		resolveInheritValues(node, style, styles)
		ApplyInheritedProperties(node, style, styles)
		// CSS Writing Modes §2.1: If an inline box has a different writing-mode
		// than its containing block, its display computes to inline-block.
		resolveOrthogonalDisplay(node, style, styles)
		// Resolve logical size properties (inline-size, block-size) based on the
		// element's computed writing-mode. Must happen after inheritance so that
		// inherited writing-mode is available.
		resolveLogicalSizeProperties(style)
		// Resolve logical box properties (margin/padding/border-inline/block)
		resolveLogicalBoxProperties(style)
		// Apply container query rules after base style (needs ancestor styles resolved)
		applyContainerQueryRules(node, stylesheets, styles, style)
		// CSS Text Decor 3 §2: accumulate AppliedTextDecorations from parent.
		// Must run AFTER longhand resolution so this element's contribution sees
		// final text-decoration-line/style/color/thickness values. Mirrors
		// Blink's StyleBuilder append-to-inherited-vector at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f (applied_text_decoration.h:18-50).
		var parentStyle *Style
		if node.Parent != nil {
			parentStyle = styles[node.Parent]
		}
		style.ResolveAppliedTextDecorations(parentStyle, node)
		styles[node] = style
	}

	// When the HTML parser doesn't create implicit <html>/<body> elements (common for
	// fragment-style HTML and WPT test files), elements appear as direct children of
	// doc.Root. In this case, CSS inheritance from :root rules cannot propagate because
	// doc.Root has no style entry. Fix: synthesize a root style from the :root-matching
	// element (the first element child of doc.Root) and store it as styles[doc.Root].
	// This allows ApplyInheritedProperties to correctly propagate font-size, font-family,
	// etc. to all children of doc.Root, so em units resolve against the correct font-size.
	if node.TagName == "document" && styles[node] == nil {
		syntheticRootStyle := NewStyle()
		syntheticRootStyle.ViewportWidth = viewportWidth
		syntheticRootStyle.ViewportHeight = viewportHeight
		for _, child := range node.Children {
			if child.Type == html.ElementNode {
				// First element child matches :root — compute its style to capture
				// :root rules, then extract inheritable properties for doc.Root.
				rootChildStyle := ComputeStyle(child, stylesheets, viewportWidth, viewportHeight, ctx)
				for prop := range inheritableProperties {
					// CSS Writing Modes §7.1: writing-mode's initial value is horizontal-tb.
					// Don't propagate from :root to synthetic doc root — otherwise every
					// descendant inherits it and the parentIsVertical guard blocks transforms.
					if prop == "writing-mode" {
						continue
					}
					if val, ok := rootChildStyle.Get(prop); ok {
						syntheticRootStyle.Set(prop, val)
					}
				}
				// Also propagate CSS custom properties (--*) which inherit by default
				for prop, val := range rootChildStyle.Properties {
					if strings.HasPrefix(prop, "--") {
						syntheticRootStyle.Set(prop, val)
					}
				}
				break
			}
		}
		if len(syntheticRootStyle.Properties) > 0 {
			styles[node] = syntheticRootStyle
		}
	}

	// Always traverse children (parent is already computed, so top-down order is maintained)
	for _, child := range node.Children {
		applyStylesToNode(child, stylesheets, styles, viewportWidth, viewportHeight, ctx)
	}
}

// applyContainerQueryRules evaluates @container rules for a node by checking
// ancestor containers' computed sizes. Called after base style computation.
func applyContainerQueryRules(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style, style *Style) {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.ContainerQuery == nil {
				continue
			}
			// Skip pseudo-element rules
			if rule.Selector.PseudoElement != "" {
				continue
			}
			// Check if selector matches this node
			if !MatchesSelector(node, rule.Selector) {
				continue
			}
			// Evaluate the container query against ancestors
			if !evaluateContainerQuery(node, rule.ContainerQuery, styles) {
				continue
			}
			// Apply declarations
			for property, value := range rule.Declarations {
				expandShorthand(style, property, value)
			}
		}
	}
}

// evaluateContainerQuery walks up the DOM to find a matching container and evaluates size conditions.
func evaluateContainerQuery(node *html.Node, cq *ContainerQuery, styles map[*html.Node]*Style) bool {
	// Walk up ancestors to find a container
	ancestor := node.Parent
	for ancestor != nil {
		ancestorStyle, ok := styles[ancestor]
		if !ok {
			ancestor = ancestor.Parent
			continue
		}

		containerType, _ := ancestorStyle.Get("container-type")
		if containerType != "inline-size" && containerType != "size" {
			ancestor = ancestor.Parent
			continue
		}

		// Check container name if specified
		if cq.Name != "" {
			containerName, _ := ancestorStyle.Get("container-name")
			if containerName != cq.Name {
				ancestor = ancestor.Parent
				continue
			}
		}

		// Found a matching container — evaluate conditions against its width
		widthStr, _ := ancestorStyle.Get("width")
		containerWidth, _ := ParseLength(widthStr)

		for _, cond := range cq.Conditions {
			condValue, _ := ParseLength(cond.Value)
			switch cond.Feature {
			case "min-width":
				if containerWidth < condValue {
					return false
				}
			case "max-width":
				if containerWidth > condValue {
					return false
				}
			case "width":
				if containerWidth != condValue {
					return false
				}
			}
		}
		return true
	}
	return false
}

// setFormBorder sets individual border properties for form element UA styles.
// style.Set() doesn't expand shorthands, so we must set each property individually.
func setFormBorder(style *Style, width, borderStyle, color string) {
	for _, side := range []string{"top", "right", "bottom", "left"} {
		if _, ok := style.Get("border-" + side + "-width"); !ok {
			style.Set("border-"+side+"-width", width)
		}
		if _, ok := style.Get("border-" + side + "-style"); !ok {
			style.Set("border-"+side+"-style", borderStyle)
		}
		if _, ok := style.Get("border-" + side + "-color"); !ok {
			style.Set("border-"+side+"-color", color)
		}
	}
}

// setFormPadding sets individual padding properties for form element UA styles.
func setFormPadding(style *Style, top, right, bottom, left string) {
	if _, ok := style.Get("padding-top"); !ok {
		style.Set("padding-top", top)
	}
	if _, ok := style.Get("padding-right"); !ok {
		style.Set("padding-right", right)
	}
	if _, ok := style.Get("padding-bottom"); !ok {
		style.Set("padding-bottom", bottom)
	}
	if _, ok := style.Get("padding-left"); !ok {
		style.Set("padding-left", left)
	}
}

// pxValue appends "px" to s, stripping any existing "px" suffix first so that
// attribute values like "100px" don't become "100pxpx".
func pxValue(s string) string {
	return strings.TrimSuffix(s, "px") + "px"
}

// applyPresentationalAttributes maps HTML presentational attributes to CSS properties.
// These have lower priority than author CSS — they're applied before stylesheet rules
// and inline styles, so CSS can override them.
func applyPresentationalAttributes(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}

	// bgcolor → background-color (on table, tr, td, th, body)
	if val, ok := node.GetAttribute("bgcolor"); ok {
		style.Set("background-color", val)
	}

	// width attribute → CSS width (on table, td, th, img, etc.)
	// Per HTML spec (2022+), canvas and video width/height attributes set
	// intrinsic dimensions and aspect-ratio, NOT CSS width/height. This lets
	// CSS height override the attribute height and have the width transfer
	// through the aspect ratio correctly.
	if val, ok := node.GetAttribute("width"); ok {
		switch node.TagName {
		case "table", "td", "th", "col", "colgroup", "img", "input", "object", "embed", "hr":
			if strings.HasSuffix(val, "%") {
				style.Set("width", val)
			} else {
				// Numeric value = pixels
				style.Set("width", pxValue(val))
			}
		}
	}

	// height attribute → CSS height.
	// Per HTML spec (2024), canvas and video height attributes do NOT map to CSS
	// height directly — they define the element's intrinsic coordinate space height
	// and establish a CSS aspect-ratio when paired with the width attribute.
	// Excluding canvas/video from the height→CSS height mapping ensures that
	// CSS Containment (contain:size) can correctly use the intrinsic aspect ratio
	// to determine block-size from an author-set inline-size.
	if val, ok := node.GetAttribute("height"); ok {
		switch node.TagName {
		case "table", "td", "th", "tr", "img", "input", "object", "embed":
			if strings.HasSuffix(val, "%") {
				style.Set("height", val)
			} else {
				style.Set("height", pxValue(val))
			}
		}
	}

	// canvas and video: width+height attributes → CSS aspect-ratio (per HTML spec 2024).
	// When both numeric attributes are present, they establish the element's
	// CSS aspect-ratio as a presentational hint. The width attribute continues
	// to map to CSS width (above) for backward-compatibility and flex sizing.
	switch node.TagName {
	case "canvas", "video":
		wVal, wOk := node.GetAttribute("width")
		hVal, hOk := node.GetAttribute("height")
		if wOk && hOk && !strings.HasSuffix(wVal, "%") && !strings.HasSuffix(hVal, "%") {
			// Both numeric attributes present → set aspect-ratio presentational hint.
			style.Set("aspect-ratio", wVal+"/"+hVal)
		}
	}

	// align → text-align (on td, th, tr, div, p, etc.)
	if val, ok := node.GetAttribute("align"); ok {
		switch val {
		case "left", "center", "right", "justify":
			switch node.TagName {
			case "td", "th", "tr", "div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				style.Set("text-align", val)
			}
		}
		// table/img align="center" → auto margins
		if val == "center" && (node.TagName == "table" || node.TagName == "img") {
			style.Set("margin-left", "auto")
			style.Set("margin-right", "auto")
		}
	}

	// valign → vertical-align (on td, th, tr)
	if val, ok := node.GetAttribute("valign"); ok {
		switch node.TagName {
		case "td", "th", "tr":
			style.Set("vertical-align", val)
		}
	}

	// border attribute on table → border-width on cells
	if val, ok := node.GetAttribute("border"); ok {
		if node.TagName == "table" {
			style.Set("border-width", pxValue(val))
			style.Set("border-style", "outset")
		}
	}

	// cellpadding on table → stored for table layout to apply to cells
	// cellspacing on table → border-spacing
	if val, ok := node.GetAttribute("cellspacing"); ok {
		if node.TagName == "table" {
			style.Set("border-spacing", pxValue(val))
		}
	}

	// <center> element → text-align: center + auto margins for block children
	if node.TagName == "center" {
		style.Set("text-align", "center")
	}

	// cellpadding on ancestor <table> → padding on td/th
	if node.TagName == "td" || node.TagName == "th" {
		// Walk up to find containing table
		for p := node.Parent; p != nil; p = p.Parent {
			if p.TagName == "table" {
				if cp, ok := p.GetAttribute("cellpadding"); ok {
					style.Set("padding", pxValue(cp))
				}
				break
			}
		}
	}

	// colspan attribute — stored for table layout (not a CSS property,
	// but we note it here so table layout can read it from the style)
	if val, ok := node.GetAttribute("colspan"); ok {
		if node.TagName == "td" || node.TagName == "th" {
			style.Set("-x-colspan", val)
		}
	}

	// color attribute → color (on font, hr, etc.)
	if val, ok := node.GetAttribute("color"); ok {
		style.Set("color", val)
	}

	// <font size="N"> → font-size
	if node.TagName == "font" {
		if val, ok := node.GetAttribute("size"); ok {
			if fontSize := fontSizeFromHTMLSize(val); fontSize != "" {
				style.Set("font-size", fontSize)
			}
		}
		if val, ok := node.GetAttribute("face"); ok {
			style.Set("font-family", val)
		}
	}

	// HTML5 list counter presentational hints (HTML §6.4.6, §6.4.7):
	//
	// <ol reversed>   → counter-reset: reversed(list-item)
	//   CSS counter-reset for list-item uses the CSS Lists auto-initial algorithm
	//   (CountUsage / computeReversedCounterInitial) to count items in scope,
	//   matching Blink's list rendering. The `start` attribute is IGNORED for
	//   the CSS counter when `reversed` is present — the CSS spec mandates
	//   the auto-initial computation, not the HTML `start` value.
	//
	// <ol start="N">  → counter-reset: list-item N   (no reversed attribute)
	//   The `start` attribute sets the list-item counter to an explicit N.
	//   Overrides any UA stylesheet value; CSS can further override it.
	//
	// <li value="N">  → counter-set: list-item N
	//   The `value` attribute jumps the counter to N at this item.
	//
	// These presentational hints run before author CSS, so author `counter-reset`
	// rules override them. Only applied to the specific elements; not inherited.
	switch node.TagName {
	case "ol":
		_, hasReversed := node.GetAttribute("reversed")
		startVal, hasStart := node.GetAttribute("start")
		if hasReversed {
			// reversed(list-item) without explicit integer: let the CSS auto-initial
			// algorithm compute the initial value from the scope.
			style.Set("counter-reset", "reversed(list-item)")
		} else if hasStart {
			if _, err := strconv.Atoi(startVal); err == nil {
				style.Set("counter-reset", "list-item "+startVal)
			}
		}
	case "li":
		if val, ok := node.GetAttribute("value"); ok {
			if _, err := strconv.Atoi(val); err == nil {
				style.Set("counter-set", "list-item "+val)
			}
		}
	}

	// <img> border attribute
	if node.TagName == "img" {
		if val, ok := node.GetAttribute("border"); ok {
			style.Set("border-width", pxValue(val))
			style.Set("border-style", "solid")
		}
	}

	// dir attribute → direction + unicode-bidi (HTML presentational hint).
	// Per HTML spec §14.3.5, the dir attribute maps to:
	//   direction: ltr/rtl
	//   unicode-bidi: isolate (or isolate-override for <bdo>)
	// As a presentational hint, author CSS can override these values.
	//
	// dir="auto" is resolved per HTML §3.2.6 "auto directionality" by
	// scanning descendant text for the first character with a strong
	// Unicode bidi class (L → ltr; R or AL → rtl). The shared
	// elementDirectionality helper (matcher.go) implements that walk and
	// also handles ancestor fallback when no strong character is found,
	// matching Blink's HTMLElement::ResolveAutoDirectionality
	// (third_party/blink/renderer/core/html/html_element.cc @
	// 4883d11fef4a).
	if dirAttr, ok := node.GetAttribute("dir"); ok {
		dirAttr = strings.ToLower(strings.TrimSpace(dirAttr))
		switch dirAttr {
		case "rtl":
			style.Set("direction", "rtl")
		case "ltr":
			style.Set("direction", "ltr")
		case "auto":
			if elementDirectionality(node) == DirectionRTL {
				style.Set("direction", "rtl")
			} else {
				style.Set("direction", "ltr")
			}
		}
		if dirAttr == "rtl" || dirAttr == "ltr" || dirAttr == "auto" {
			if node.TagName == "bdo" {
				style.Set("unicode-bidi", "isolate-override")
			} else {
				style.Set("unicode-bidi", "isolate")
			}
		}
	}

	// SVG presentation attributes per SVG 1.1 §6.4 / SVG 2 §6.4. For
	// every SVG element these attribute spellings are equivalent to
	// the matching CSS property — they participate in the cascade as
	// non-specific author-level declarations (specificity 0), which
	// is exactly the slot applyPresentationalAttributes fills: it
	// runs before any stylesheet rule, so a later CSS rule with any
	// non-zero specificity wins.
	//
	// Mirrors Blink's per-element CollectPresentationAttribute
	// implementations in core/svg/svg_*_element.cc — every shape /
	// container / text element opts every paint and stroke property
	// in. Reading them here for all element types is a small
	// over-trigger (non-SVG elements never reach an SVG painter), but
	// it keeps the wiring in one place and matches the way Blink
	// treats these attributes as plain CSS property maps once
	// CollectPresentationAttribute has fired.
	applySVGPresentationalAttributes(node, style)
}

// applySVGPresentationalAttributes maps SVG presentation attributes
// to their CSS-property equivalents. Per SVG 1.1 §6.4, these have
// specificity 0 — so they apply only when no stylesheet rule sets
// the same property. The function is called from
// applyPresentationalAttributes before stylesheet matching, so the
// cascade's normal "later origin wins" rule handles the precedence
// correctly.
func applySVGPresentationalAttributes(node *html.Node, style *Style) {
	// Map of attribute name → CSS property name. SVG attribute names
	// are camelCase / lowercase per the HTML tokenizer; we match
	// against the lowercased forms only since HTML tokenizes them
	// that way. Mirrors the property list from
	// core/svg/svg_animated_paint_properties.cc + the per-element
	// CollectPresentationAttribute calls.
	svgPresentationAttrs := []string{
		"fill",
		"fill-opacity",
		"fill-rule",
		"stroke",
		"stroke-opacity",
		"stroke-width",
		"stroke-linecap",
		"stroke-linejoin",
		"stroke-miterlimit",
		"stroke-dasharray",
		"stroke-dashoffset",
		"opacity",
		"color",
		"clip-rule",
		"clip-path",
		"mask",
		"mask-type",
		"stop-color",
		"stop-opacity",
		// Phase 7: filter on SVG shapes / containers + flood-color /
		// flood-opacity / color-interpolation-filters on the filter
		// primitive `<feFlood>` (and equivalents). These are SVG
		// presentation attributes per SVG Filter Effects 1 §6.4.
		"filter",
		"flood-color",
		"flood-opacity",
		"color-interpolation-filters",
	}
	for _, attr := range svgPresentationAttrs {
		if val, ok := node.GetAttribute(attr); ok {
			// Only set if no value yet — preserves the user-agent
			// stylesheet default in cases where both UA and the
			// attribute would otherwise tie. Author CSS later in
			// the cascade overrides this freely.
			if _, exists := style.Get(attr); !exists {
				style.Set(attr, val)
			}
		}
	}
}

// fontSizeFromHTMLSize converts HTML <font size="N"> to CSS font-size.
func fontSizeFromHTMLSize(size string) string {
	switch size {
	case "1":
		return "x-small"
	case "2":
		return "small"
	case "3":
		return "medium"
	case "4":
		return "large"
	case "5":
		return "x-large"
	case "6":
		return "xx-large"
	case "7":
		return "xxx-large"
	}
	return ""
}
