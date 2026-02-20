# Plan: 10 High-Impact CSS Features for Real Web Page Rendering

**Goal**: Implement 10 CSS features that will dramatically improve rendering of real
websites from the internet. Features are chosen by cross-referencing:
- Web Almanac 2022–2024 usage data (HTTP Archive)
- State of CSS 2024 survey
- Direct inspection of what's missing in Louis14's codebase

**Current baseline**: CSS2 99/99, CSS3 275/275 at 0.1% threshold.

**Execution model**: Each feature is a self-contained task for a parallel agent.
Features are independent and do not depend on each other.

---

## Feature 1: `@layer` — CSS Cascade Layers

### Why critical
Bootstrap 5.3+, Angular Material, Open Props, and PicoCSS all use `@layer`.
Without it, every `@layer` block's contents are silently dropped (the current parser
sees `@layer` as an unknown at-rule and skips it). This breaks 15–25% of modern pages.
Specificity ordering is also completely wrong when layers are present.

### Current state
In `pkg/css/stylesheet.go:ParseStylesheet()` lines 174–191, `@layer` hits the
`// Unknown at-rules are silently skipped` path. ALL rules inside `@layer` blocks
are discarded.

### CSS Specification Summary
```css
/* Layer declaration order */
@layer reset, base, components, utilities;

/* Layer block (rules inside belong to that layer) */
@layer base {
  h1 { font-size: 2em; }
}
@layer utilities {
  .mt-4 { margin-top: 1rem; }
}

/* Anonymous layer */
@layer {
  p { color: green; }
}

/* Inline layer rules take priority over block rules of same layer */
@layer utilities { .mt-4 { margin-top: 2rem; } }  /* overrides earlier */
```

**Cascade order**: unlayered rules > last-declared layer > earlier layers
(i.e., later `@layer` declarations WIN over earlier ones, which is REVERSED from normal
cascade order for unlayered rules).

### Implementation

#### Step 1: Add layer tracking to `Stylesheet` struct
In `pkg/css/stylesheet.go`:
```go
type Stylesheet struct {
    Rules      []Rule
    FontFaces  []FontFaceRule
    LayerOrder []string  // NEW: ordered list of layer names as declared
}
```

#### Step 2: Add `LayerName` field to `Rule` struct
In `pkg/css/stylesheet.go` (find the `Rule` struct):
```go
type Rule struct {
    Selector     Selector
    Declarations map[string]string
    Important    map[string]bool
    MediaQuery   *MediaQuery
    ContainerQuery *ContainerQuery
    LayerName    string   // NEW: "" = unlayered, name = belongs to this layer
    LayerOrder   int      // NEW: layer declaration index (for sorting)
}
```

#### Step 3: Parse `@layer` rules in `ParseStylesheet()`
In the `for _, ruleStr := range rules` loop, add BEFORE the `continue`:
```go
} else if strings.HasPrefix(trimmed, "@layer") {
    layerRules := parseLayerRule(ruleStr, stylesheet)
    stylesheet.Rules = append(stylesheet.Rules, layerRules...)
}
```

#### Step 4: Implement `parseLayerRule()`
```go
func parseLayerRule(ruleStr string, stylesheet *Stylesheet) []Rule {
    // Strip "@layer" prefix
    rest := strings.TrimSpace(ruleStr[len("@layer"):])

    // Case 1: Layer declaration statement (no braces): @layer reset, base, utilities;
    if !strings.Contains(rest, "{") {
        names := strings.Split(strings.TrimSuffix(rest, ";"), ",")
        for _, name := range names {
            name = strings.TrimSpace(name)
            if name != "" && !containsLayer(stylesheet.LayerOrder, name) {
                stylesheet.LayerOrder = append(stylesheet.LayerOrder, name)
            }
        }
        return nil
    }

    // Case 2: Layer block: @layer name { rules... }
    bracePos := strings.Index(rest, "{")
    layerName := strings.TrimSpace(rest[:bracePos])
    if layerName == "" {
        layerName = fmt.Sprintf("__anonymous_%d__", len(stylesheet.LayerOrder))
    }

    // Register layer name (preserving declaration order)
    if !containsLayer(stylesheet.LayerOrder, layerName) {
        stylesheet.LayerOrder = append(stylesheet.LayerOrder, layerName)
    }
    layerIdx := indexOfLayer(stylesheet.LayerOrder, layerName)

    // Extract inner CSS
    innerStart := bracePos + 1
    innerEnd := strings.LastIndex(rest, "}")
    if innerEnd <= innerStart { return nil }
    innerCSS := rest[innerStart:innerEnd]

    // Parse inner rules
    var result []Rule
    for _, innerRuleStr := range splitRules(innerCSS) {
        // Handle nested @layer, @media, etc. inside layer blocks
        inner := strings.TrimSpace(innerRuleStr)
        if strings.HasPrefix(inner, "@media") {
            mediaRules := parseMediaRule(innerRuleStr)
            for i := range mediaRules {
                mediaRules[i].LayerName = layerName
                mediaRules[i].LayerOrder = layerIdx
            }
            result = append(result, mediaRules...)
        } else if strings.HasPrefix(inner, "@layer") {
            // Nested @layer — recurse
            nested := parseLayerRule(innerRuleStr, stylesheet)
            result = append(result, nested...)
        } else {
            rules, err := parseRules(innerRuleStr)
            if err != nil { continue }
            for i := range rules {
                rules[i].LayerName = layerName
                rules[i].LayerOrder = layerIdx
            }
            result = append(result, rules...)
        }
    }
    return result
}
```

#### Step 5: Update cascade in `pkg/css/cascade.go`
In `FindMatchingRules()` (in `pkg/css/matcher.go`) and the cascade sort in
`cascade.go`, layers must be sorted BEFORE specificity:

```
Sort order (ascending priority = last wins):
  1. Unlayered rules (HIGHEST priority — override all layers)
     Wait: actually per spec, unlayered = implicit outermost layer = HIGHEST

  Actually: spec says:
  - Last-declared @layer wins over earlier-declared @layer
  - Unlayered rules are treated as an implicit "outermost" layer that wins over ALL layers

  Sort order (lowest to highest priority):
    1. Earlier layers (layerOrder = 0, 1, 2...)
    2. Later layers (higher layerOrder)
    3. Unlayered rules (LayerName == "")
```

In `cascade.go`, find the rule sorting logic (look for `sort.SliceStable`) and add
layer ordering BEFORE specificity:

```go
// First sort by layer order (earlier layer = lower priority)
// LayerName == "" means unlayered (highest priority, beats all layers)
sort.SliceStable(matchedRules, func(i, j int) bool {
    ri, rj := matchedRules[i], matchedRules[j]
    // Unlayered rules beat layered rules
    iLayered := ri.LayerName != ""
    jLayered := rj.LayerName != ""
    if iLayered != jLayered {
        return iLayered // layered < unlayered (layered sorts earlier = lower priority)
    }
    // Both layered: later declaration order wins
    if iLayered && ri.LayerOrder != rj.LayerOrder {
        return ri.LayerOrder < rj.LayerOrder // smaller index = earlier = lower priority
    }
    // Same layer (or both unlayered): use specificity
    return specificityLess(ri.Selector.Specificity, rj.Selector.Specificity)
})
```

### WPT Test to Create
File: `pkg/visualtest/testdata/wpt-css3/css-cascade-layers/layer-basic-001.html`
```html
<!DOCTYPE html>
<title>@layer: later layer wins over earlier layer</title>
<link rel="match" href="layer-basic-001-ref.html">
<style>
@layer base, override;
@layer base { div { background: red; width: 100px; height: 100px; } }
@layer override { div { background: lime; } }
</style>
<div></div>
```
Reference: `layer-basic-001-ref.html` — a 100×100 lime square.

Add 3–4 more tests: unlayered-beats-layer, layer-declaration-order, layer-in-media.

---

## Feature 2: CSS Native Nesting

### Why critical
PostCSS-processed nesting has existed for years; native CSS nesting (shipping in all
browsers since 2024) is now being shipped by build tools. Angular, SvelteKit, and many
design systems now emit nested CSS. Without it, nested rules are silently ignored.

### Current state
`ParseStylesheet()` sees `div { p { color: red; } }` as a `div` rule with garbled
declarations containing `p { color: red; }`. The nested rule is lost.

### CSS Specification Summary
```css
/* Nested rules — selector is relative to parent */
.card {
  background: white;

  .title { font-size: 1.5em; }   /* = .card .title */

  &:hover { opacity: 0.9; }      /* = .card:hover — & is the parent selector */

  & > p { margin: 0; }           /* = .card > p */

  @media (max-width: 600px) {    /* @media nested inside a rule */
    font-size: 0.875em;
  }
}
```

### Implementation

#### Step 1: In `parseRule()`, detect nested rules in declarations
After extracting `declStr` (the content between `{` and `}`), scan for nested braces:

```go
func parseRule(ruleStr string) (Rule, error) {
    // ... existing selector extraction ...

    declStr := ruleStr[declStart:declEnd]

    // Check if there are nested rules (inner { } blocks)
    if strings.Contains(declStr, "{") {
        return parseRuleWithNesting(selectorStr, selector, declStr)
    }

    // ... existing declaration parsing ...
}
```

#### Step 2: Implement `parseRuleWithNesting()`
This function separates flat declarations from nested rules:
```go
func parseRuleWithNesting(parentSelectorStr string, parentSelector Selector, declStr string) (Rule, error) {
    // Split declStr into: (a) flat declarations (no braces), (b) nested rule blocks
    // Use splitRules() on the inner content, which handles brace-depth correctly.

    innerRules := splitRules(declStr)
    flatDecls := ""
    var nestedRules []Rule

    for _, inner := range innerRules {
        inner = strings.TrimSpace(inner)
        if strings.Contains(inner, "{") {
            // This is a nested rule (has a block)
            if strings.HasPrefix(inner, "@media") {
                // Nested @media: extract media query, apply parent selector to inner rules
                mediaRules := parseMediaRule(inner)
                for _, mr := range mediaRules {
                    // Prepend parent selector to each inner rule
                    resolved := resolveNestedSelector(parentSelectorStr, mr.Selector.Raw)
                    mr.Selector = parseSelector(resolved)
                    nestedRules = append(nestedRules, mr)
                }
            } else {
                // Regular nested rule: "& .child { ... }" or ".child { ... }"
                bracePos := strings.Index(inner, "{")
                nestedSelStr := strings.TrimSpace(inner[:bracePos])
                resolved := resolveNestedSelector(parentSelectorStr, nestedSelStr)
                // Parse as a regular rule with resolved selector
                nestedRule, err := parseRule(resolved + " " + inner[bracePos:])
                if err == nil {
                    nestedRules = append(nestedRules, nestedRule)
                }
            }
        } else {
            // Flat declaration (no braces)
            flatDecls += inner + ";"
        }
    }

    // Build the parent rule from flat declarations
    declResult := parseDeclarations(flatDecls)
    parentRule := Rule{
        Selector:     parentSelector,
        Declarations: declResult.Declarations,
        Important:    declResult.Important,
        NestedRules:  nestedRules, // NEW field
    }
    return parentRule, nil
}

// resolveNestedSelector resolves a nested selector relative to parent.
// "&" is replaced by parent. If no "&", a descendant combinator is implied.
func resolveNestedSelector(parent, nested string) string {
    if strings.Contains(nested, "&") {
        return strings.ReplaceAll(nested, "&", parent)
    }
    // Implicit: ".child" inside ".parent" = ".parent .child"
    return parent + " " + nested
}
```

#### Step 3: Flatten nested rules when adding to stylesheet
In `ParseStylesheet()`, after parsing a rule, recursively add its nested rules:
```go
rules, err := parseRules(ruleStr)
for _, rule := range rules {
    stylesheet.Rules = append(stylesheet.Rules, rule)
    // Recursively add nested rules (they already have resolved selectors)
    stylesheet.Rules = append(stylesheet.Rules, rule.NestedRules...)
}
```

The `NestedRules` field on `Rule` is temporary — rules are flattened on parse.
Actually: it's cleaner to return all rules flat from `parseRules()` directly.

#### Revised approach (simpler):
In `ParseStylesheet()`, after calling `splitRules(css)`, first run a
**nesting expansion** pass that converts nested CSS to flat CSS:

```go
// Pre-process: expand native CSS nesting to flat rules
css = expandNesting(css, "")
rules := splitRules(css)
```

```go
func expandNesting(css string, parentSelector string) string {
    var result strings.Builder
    parts := splitRules(css)
    for _, part := range parts {
        part = strings.TrimSpace(part)
        if !strings.Contains(part, "{") {
            // Flat declaration or at-rule statement
            if parentSelector != "" {
                // Declaration inside a rule — not a nested rule, handled by caller
            }
            continue
        }
        bracePos := strings.Index(part, "{")
        sel := strings.TrimSpace(part[:bracePos])
        body := part[bracePos+1 : strings.LastIndex(part, "}")]

        if parentSelector != "" && !strings.HasPrefix(sel, "@") {
            // Nested rule — resolve selector
            sel = resolveNestedSelector(parentSelector, sel)
        }

        // Separate flat declarations from nested rules in body
        flatDecls, nestedParts := separateDeclsAndRules(body)

        // Emit flat rule with just the flat declarations
        if strings.TrimSpace(flatDecls) != "" {
            result.WriteString(sel + " { " + flatDecls + " }\n")
        }

        // Recursively expand nested rules
        result.WriteString(expandNesting(nestedParts, sel))
    }
    return result.String()
}
```

### WPT Test to Create
File: `pkg/visualtest/testdata/wpt-css3/css-nesting/nesting-basic-001.html`
```html
<!DOCTYPE html>
<title>CSS Nesting: basic nested rule</title>
<link rel="match" href="nesting-basic-001-ref.html">
<style>
.container {
  width: 200px;
  height: 50px;
  background: red;

  .inner { background: lime; width: 100px; height: 50px; }
}
</style>
<div class="container"><div class="inner"></div></div>
```
Reference: shows a 100px lime box inside a red 200px container.

Add tests for: `&` selector, nested `@media`, multiple levels of nesting.

---

## Feature 3: `color-mix()` + `oklch()` / `lch()` / `hwb()` Color Functions

### Why critical
**TailwindCSS v4** uses `oklch()` for ALL its colors. Any site using Tailwind v4
renders with completely wrong colors without this. `color-mix()` is used by Open Props,
shadcn/ui, and many modern design systems to derive tints and shades.

### Current state
In `pkg/css/style.go`, the `parseColor()` or `isValidColorValue()` function handles
hex, rgb, rgba, hsl, hsla, and named colors. No support for `oklch`, `lch`, `hwb`,
or `color-mix`.

### CSS Specification Summary
```css
/* oklch(lightness chroma hue / alpha) */
color: oklch(0.5 0.2 270);        /* purple */
color: oklch(67% 0.1986 141.76);  /* same as lime */

/* lch(lightness chroma hue / alpha) - similar but different gamut */
color: lch(50 100 270);

/* hwb(hue whiteness blackness / alpha) */
color: hwb(270 20% 0%);

/* color-mix(in colorspace, color1 percentage, color2 percentage) */
color: color-mix(in oklch, blue, white);        /* 50/50 mix */
color: color-mix(in srgb, red 30%, blue 70%);
color: color-mix(in oklch, #3b82f6, white 20%);  /* lighten blue by 20% */
```

### Implementation

#### Step 1: Add OKLCH → sRGB conversion
In `pkg/css/style.go` (or a new `pkg/css/colorspace.go`):
```go
// oklchToRGB converts OKLCH color to sRGB [0,1] values.
// L=lightness [0,1], C=chroma [0,0.4], H=hue [0,360deg]
func oklchToRGB(L, C, H float64) (r, g, b float64) {
    // Step 1: OKLCH → OKLab
    hRad := H * math.Pi / 180
    a := C * math.Cos(hRad)
    bLab := C * math.Sin(hRad)

    // Step 2: OKLab → linear sRGB via OKLab matrices
    // OKLab → LMS (cube root space)
    l_ := L + 0.3963377774*a + 0.2158037573*bLab
    m_ := L - 0.1055613458*a - 0.0638541728*bLab
    s_ := L - 0.0894841775*a - 1.2914855480*bLab

    // Cube to get LMS
    l := l_ * l_ * l_
    m := m_ * m_ * m_
    s := s_ * s_ * s_

    // Step 3: LMS → linear sRGB
    r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
    g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
    b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

    // Step 4: Gamma correction (linear → sRGB)
    r = linearToSRGB(r)
    g = linearToSRGB(g)
    b = linearToSRGB(b)

    // Clamp to [0,1]
    return clamp01(r), clamp01(g), clamp01(b)
}

func linearToSRGB(c float64) float64 {
    if c <= 0.0031308 { return 12.92 * c }
    return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// lchToRGB converts LCH (CIELCh) to sRGB.
func lchToRGB(L, C, H float64) (r, g, b float64) {
    // LCH → Lab
    hRad := H * math.Pi / 180
    a := C * math.Cos(hRad)
    bLab := C * math.Sin(hRad)

    // Lab → XYZ (D65)
    fy := (L + 16) / 116
    fx := a/500 + fy
    fz := fy - bLab/200
    x := labF(fx) * 0.95047
    y := labF(fy) * 1.00000
    z := labF(fz) * 1.08883

    // XYZ → linear sRGB
    rl :=  3.2406*x - 1.5372*y - 0.4986*z
    gl := -0.9689*x + 1.8758*y + 0.0415*z
    bl :=  0.0557*x - 0.2040*y + 1.0570*z

    return clamp01(linearToSRGB(rl)), clamp01(linearToSRGB(gl)), clamp01(linearToSRGB(bl))
}

func labF(t float64) float64 {
    if t > 0.206897 { return t * t * t }
    return (t - 16.0/116) / 7.787
}

// hwbToRGB converts HWB color to sRGB.
func hwbToRGB(H, W, B float64) (r, g, b float64) {
    W /= 100; B /= 100
    if W+B >= 1 { gray := W/(W+B); return gray, gray, gray }
    r, g, b = hslToRGB(H, 1, 0.5) // full saturation hue
    r = r*(1-W-B) + W
    g = g*(1-W-B) + W
    b = b*(1-W-B) + W
    return
}
```

#### Step 2: Parse `oklch()`, `lch()`, `hwb()` in `parseColorToRGBA()`
Find the existing color parsing function (searches for `rgb\(`, `hsl\(`, etc.) and add:
```go
case strings.HasPrefix(lower, "oklch("):
    inner := lower[6:strings.LastIndex(lower, ")")]
    parts := parseColorArgs(inner) // handles "/" for alpha
    if len(parts) >= 3 {
        L := parseColorComponent(parts[0], 1.0)  // 0-1 or 0%-100%
        C := parseColorComponent(parts[1], 0.4)
        H := parseHue(parts[2])
        alpha := 1.0
        if len(parts) >= 4 { alpha = parseColorComponent(parts[3], 1.0) }
        r, g, b := oklchToRGB(L, C, H)
        return color.RGBA{uint8(r*255), uint8(g*255), uint8(b*255), uint8(alpha*255)}
    }
```

#### Step 3: Parse `color-mix()`
```go
case strings.HasPrefix(lower, "color-mix("):
    return parseColorMix(lower)
```

```go
func parseColorMix(val string) color.RGBA {
    // color-mix(in colorspace, color1 [pct%], color2 [pct%])
    inner := val[len("color-mix("):strings.LastIndex(val, ")")]
    // Split on commas (but not commas inside color functions)
    args := splitColorMixArgs(inner) // depth-aware split
    if len(args) < 3 { return color.RGBA{} }

    // args[0] = "in oklch" or "in srgb"
    // args[1] = "red 30%" or just "red"
    // args[2] = "blue 70%" or just "blue"

    color1, pct1 := parseColorWithPercent(strings.TrimSpace(args[1]))
    color2, pct2 := parseColorWithPercent(strings.TrimSpace(args[2]))

    // Default percentages: 50/50
    if pct1 < 0 && pct2 < 0 { pct1, pct2 = 0.5, 0.5 }
    if pct1 < 0 { pct1 = 1 - pct2 }
    if pct2 < 0 { pct2 = 1 - pct1 }

    // Normalize percentages
    total := pct1 + pct2
    if total > 0 { pct1 /= total; pct2 /= total }

    // Mix in sRGB (simplest; spec allows per-colorspace mixing but sRGB is fine for most uses)
    r := uint8(float64(color1.R)*pct1 + float64(color2.R)*pct2)
    g := uint8(float64(color1.G)*pct1 + float64(color2.G)*pct2)
    b := uint8(float64(color1.B)*pct1 + float64(color2.B)*pct2)
    a := uint8(float64(color1.A)*pct1 + float64(color2.A)*pct2)
    return color.RGBA{r, g, b, a}
}
```

### WPT Tests to Create
3 tests: `oklch-basic-001.html` (known oklch → expected hex),
`color-mix-srgb-001.html` (50/50 red+blue → purple),
`hwb-basic-001.html` (hwb(0,0%,0%) → pure red).

Use solid colored divs with known expected output as reference.

---

## Feature 4: `repeat()` in CSS Grid Tracks (including `auto-fill` / `auto-fit`)

### Why critical
`grid-template-columns: repeat(auto-fill, minmax(200px, 1fr))` is the most common
responsive grid pattern in the wild. Currently, `repeat()` is NOT parsed at all in
`parseGridTracks()`. Any grid using `repeat()` silently has NO track definitions.

### Current state
`parseGridTracks()` in `pkg/css/style.go` handles: auto, min-content, max-content,
minmax(), fr, %, px. No `repeat()` handling.

### CSS Specification Summary
```css
/* Fixed count repeat */
grid-template-columns: repeat(3, 1fr);          /* → 1fr 1fr 1fr */
grid-template-columns: repeat(2, 100px 1fr);    /* → 100px 1fr 100px 1fr */

/* auto-fill: fill as many tracks as fit, keeping empty tracks */
grid-template-columns: repeat(auto-fill, 100px);
grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));

/* auto-fit: same as auto-fill but collapses empty tracks to 0 */
grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
```

### Implementation

#### Step 1: Expand `repeat(N, tracks)` in `parseGridTracks()`
In the `splitGridTrackValues()` loop (which is paren-depth-aware), detect `repeat(...)`:

```go
// In parseGridTracks(), BEFORE the current loop over parts, expand repeat():
func expandRepeatTracks(parts []string) []string {
    var result []string
    for _, part := range parts {
        if !strings.HasPrefix(part, "repeat(") {
            result = append(result, part)
            continue
        }
        // Parse: repeat(count, track-list)
        inner := part[7:len(part)-1] // strip "repeat(" and ")"
        commaIdx := strings.Index(inner, ",")
        if commaIdx < 0 { result = append(result, part); continue }
        countStr := strings.TrimSpace(inner[:commaIdx])
        trackList := strings.TrimSpace(inner[commaIdx+1:])

        if countStr == "auto-fill" || countStr == "auto-fit" {
            // Return a special sentinel track for auto-fill/auto-fit
            // We'll handle this in grid.go layout
            result = append(result, "auto-fill:"+trackList)
            if countStr == "auto-fit" {
                result[len(result)-1] = "auto-fit:" + trackList
            }
            continue
        }

        count, err := strconv.Atoi(countStr)
        if err != nil || count <= 0 { result = append(result, part); continue }

        // Expand: repeat(3, 1fr 100px) → 1fr 100px 1fr 100px 1fr 100px
        innerTracks := splitGridTrackValues(trackList)
        for i := 0; i < count; i++ {
            result = append(result, innerTracks...)
        }
    }
    return result
}
```

Add a call to `expandRepeatTracks(parts)` at the start of `parseGridTracks()`.

#### Step 2: Add `AutoFill` and `AutoFit` fields to `GridTrack`
In `pkg/css/style.go`, add to `GridTrack`:
```go
type GridTrack struct {
    // ... existing fields ...
    AutoFill  bool        // repeat(auto-fill, ...)
    AutoFit   bool        // repeat(auto-fit, ...)
    AutoTemplate []GridTrack // the track template for auto-fill/fit
}
```

Parse `"auto-fill:trackList"` sentinel:
```go
} else if strings.HasPrefix(part, "auto-fill:") || strings.HasPrefix(part, "auto-fit:") {
    isAutoFit := strings.HasPrefix(part, "auto-fit:")
    templateStr := part[strings.Index(part, ":")+1:]
    templateTracks := splitGridTrackValues(templateStr)
    // Parse template tracks recursively
    var template []GridTrack
    for _, t := range templateTracks {
        sub := parseGridTracks(t) // single track parse
        template = append(template, sub...)
    }
    tracks = append(tracks, GridTrack{AutoFill: !isAutoFit, AutoFit: isAutoFit, AutoTemplate: template})
}
```

#### Step 3: Implement `auto-fill`/`auto-fit` in `grid.go`
In `pkg/layout/grid.go`, in `resolveTrackSizes()` or equivalent:

When a track with `AutoFill=true` is found:
1. Determine the track template's minimum size (sum of minimums in template)
2. Calculate: `count = floor(containerWidth / templateMinSize)`
3. Replace the single AutoFill track with `count` copies of the template tracks
4. For `auto-fit`: after placement, collapse empty tracks to 0 width

```go
func expandAutoFillTracks(tracks []css.GridTrack, containerWidth float64) []css.GridTrack {
    var result []css.GridTrack
    for _, track := range tracks {
        if !track.AutoFill && !track.AutoFit {
            result = append(result, track)
            continue
        }
        // Compute minimum template width
        templateMin := 0.0
        for _, t := range track.AutoTemplate {
            if t.IsMinMax { templateMin += t.MinSize
            } else if t.Size > 0 { templateMin += t.Size
            } else { templateMin += 0 } // auto = 0 minimum
        }
        if templateMin <= 0 { templateMin = 1 } // avoid infinite loop

        count := int(containerWidth / templateMin)
        if count < 1 { count = 1 }

        for i := 0; i < count; i++ {
            result = append(result, track.AutoTemplate...)
        }
    }
    return result
}
```

Call `expandAutoFillTracks(colTracks, containerWidth)` in `layoutGridContainer()`
before the track sizing phase.

### WPT Tests to Create
`css-grid/repeat-fixed-001.html`: `repeat(3, 100px)` → 3 equal columns
`css-grid/repeat-auto-fill-001.html`: `repeat(auto-fill, 100px)` in 400px → 4 columns
`css-grid/repeat-minmax-001.html`: `repeat(auto-fill, minmax(100px, 1fr))`

---

## Feature 5: `@keyframes` + `animation` + `transition` (static initial-state rendering)

### Why critical
85% of pages use `transition`, 77% use `animation`. Even for a static PNG renderer:
- The `transition` property must parse without breaking cascade
- `animation` + `@keyframes` with `animation-fill-mode: forwards` or
  `animation-delay: -1s` show their "final" or "paused" state visually
- More importantly: sites that hide/show elements via `opacity: 0` + transitions
  need to render correctly in their un-animated initial state

### Current state
Both `transition` and `animation` are completely absent. `@keyframes` is silently
skipped (unknown at-rule). Properties are NOT parsed; they pass through the cascade
as-is, causing no errors but also no effects.

### Implementation (minimal — just enough to not break pages)

#### Step 1: Parse `@keyframes` and store (ignore for static render)
In `ParseStylesheet()`, add:
```go
} else if strings.HasPrefix(trimmed, "@keyframes") || strings.HasPrefix(trimmed, "@-webkit-keyframes") {
    // Parse and store keyframes for potential future use; currently ignored in static rendering
    name, frames := parseKeyframesRule(ruleStr)
    if name != "" { stylesheet.Keyframes[name] = frames }
}
```

Add to `Stylesheet` struct:
```go
Keyframes map[string][]KeyframeRule  // animation name → keyframe stops
```

```go
type KeyframeRule struct {
    Stop         string              // "from", "to", "50%"
    Declarations map[string]string
}
```

`parseKeyframesRule()` extracts the name and stores stops (but we don't USE them
in static rendering — just avoid errors):
```go
func parseKeyframesRule(ruleStr string) (string, []KeyframeRule) {
    // @keyframes name { from { ... } to { ... } }
    rest := ruleStr[strings.Index(ruleStr, " ")+1:]
    bracePos := strings.Index(rest, "{")
    if bracePos < 0 { return "", nil }
    name := strings.TrimSpace(rest[:bracePos])
    inner := rest[bracePos+1:strings.LastIndex(rest, "}")]

    var frames []KeyframeRule
    for _, part := range splitRules(inner) {
        part = strings.TrimSpace(part)
        if !strings.Contains(part, "{") { continue }
        bp := strings.Index(part, "{")
        stop := strings.TrimSpace(part[:bp])
        declStr := part[bp+1:strings.LastIndex(part, "}")]
        frames = append(frames, KeyframeRule{
            Stop: stop,
            Declarations: parseDeclarations(declStr).Declarations,
        })
    }
    return name, frames
}
```

#### Step 2: Add `transition` and `animation` to style property allowlist
In `pkg/css/style.go` `expandShorthand()` (or `parseDeclarations()`), add:
```go
case "transition":
    // Store transition value but don't expand — static renderer ignores it
    style.Set("transition", value)
case "animation":
    // Store animation value; static renderer shows initial (t=0) state
    style.Set("animation", value)
case "transition-property", "transition-duration", "transition-timing-function",
     "transition-delay", "animation-name", "animation-duration", "animation-timing-function",
     "animation-delay", "animation-iteration-count", "animation-direction",
     "animation-fill-mode", "animation-play-state":
    style.Set(property, value)
```

Currently these properties are likely just silently stored anyway via the generic
`style.Set(property, value)` fallback in `expandShorthand()`. Verify that they
pass through without triggering errors.

#### Step 3: Implement `animation-fill-mode: forwards` (OPTIONAL but high-impact)
Sites that use `@keyframes` with `animation-fill-mode: forwards` and no delay
show the final keyframe state. For static rendering, apply the `to {}` keyframe's
declarations to the element:

```go
func applyAnimationFillMode(node *html.Node, style *css.Style, stylesheet *css.Stylesheet) {
    animName, _ := style.Get("animation-name")
    fillMode, _ := style.Get("animation-fill-mode")
    delay, _ := style.Get("animation-delay")

    if fillMode != "forwards" && fillMode != "both" { return }
    if delay != "" && delay != "0s" && delay != "0" { return }

    frames := stylesheet.Keyframes[animName]
    for _, frame := range frames {
        if frame.Stop == "to" || frame.Stop == "100%" {
            for k, v := range frame.Declarations {
                style.Set(k, v)
            }
            break
        }
    }
}
```

### WPT Tests to Create
`css-animations/keyframes-parse-001.html`: @keyframes defined but animation-fill-mode: backwards → element shows initial state (just verifies no crash).
`css-transitions/transition-parse-001.html`: element with transition property → renders correctly in initial state.

---

## Feature 6: `font-variant-numeric` and `font-variant-caps`

### Why critical
Used heavily in finance/data tables (tabular numbers), scientific documents
(ordinals, fractions), and any site using design systems with polished typography.
`font-variant: small-caps` appears on ~15% of pages with rich typography.

### Current state
`font-variant` is mentioned in `expandFontShorthand()` as "skip optional font-variant"
but has no getter. The actual font variant properties are not applied in rendering.

### CSS Specification Summary
```css
font-variant-caps: small-caps;           /* Uppercase letters in smaller caps */
font-variant-numeric: tabular-nums;      /* Fixed-width numbers (tables) */
font-variant-numeric: oldstyle-nums;     /* Old-style numerals */
font-variant-numeric: ordinal;           /* Ordinal indicators (1st, 2nd) */
font-variant: small-caps;               /* Shorthand: small-caps */
```

### Implementation

#### Step 1: Add getter functions in `pkg/css/style.go`
```go
func (s *Style) GetFontVariantCaps() string {
    if v, ok := s.Get("font-variant-caps"); ok { return v }
    if v, ok := s.Get("font-variant"); ok {
        if v == "small-caps" { return "small-caps" }
    }
    return "normal"
}

func (s *Style) GetFontVariantNumeric() string {
    if v, ok := s.Get("font-variant-numeric"); ok { return v }
    return "normal"
}
```

Also expand the `font` shorthand parser to properly handle `font-variant`:
```go
// In expandFontShorthand(): when a token is "small-caps", set font-variant-caps
if token == "small-caps" {
    style.Set("font-variant-caps", "small-caps")
    continue
}
```

#### Step 2: Apply `small-caps` in text rendering
In `pkg/render/render.go` `drawText()`, after determining the font:
```go
if style.GetFontVariantCaps() == "small-caps" {
    // For small-caps: render lowercase as uppercase at ~0.8x font-size
    text = applySmallCaps(text, style.GetFontSize())
}
```

```go
// applySmallCaps converts lowercase letters to uppercase and tracks size changes.
// Real small-caps uses a different font; we approximate by scaling down.
func applySmallCaps(text string, fontSize float64) string {
    return strings.ToUpper(text) // simplest: just uppercase
}
```

For a more complete rendering: split text into runs of lowercase/uppercase, render
lowercase runs at 0.8× font size in uppercase. This is complex; the approximation
of just uppercasing is acceptable for most visual tests.

#### Step 3: Apply `tabular-nums` in text measurement
In `pkg/text/measure.go`, if `font-variant-numeric: tabular-nums` is set,
measure each digit as having the width of the widest digit (usually '0'):
```go
func MeasureTextWithStyle(text string, fontSize float64, ..., fontVariantNumeric string) (float64, float64) {
    if fontVariantNumeric == "tabular-nums" {
        // Replace all digits with '0' for width measurement (fixed-width digits)
        normalized := digitRegexp.ReplaceAllString(text, "0")
        // then measure normalized
    }
    // ... existing measurement ...
}
```

### WPT Tests to Create
`css-font-variant/small-caps-001.html`: span with `font-variant: small-caps`, compare
with explicit `text-transform: uppercase + font-size:0.8em` reference.
`css-font-variant/tabular-nums-001.html`: digits with tabular-nums should be same width.

---

## Feature 7: Complete CSS Logical Properties

### Why critical
React, Vue, and Angular component libraries now default to logical properties for
internationalisation-ready layouts. `margin-inline: auto` for centering is increasingly
common. `padding-block` and `inset-inline` are in every modern UI kit.

### Current state
`pkg/css/style.go` `expandShorthand()` already has SOME logical property handling
(lines 1097–1175), mapping `margin-inline-start` → `margin-left`, etc.
However the WPT css-logical tests may reveal gaps. The shorthand forms
(`margin-inline`, `padding-block`, `inset`) may be incomplete.

### CSS Specification Summary
```css
/* Physical → Logical equivalents (horizontal writing mode, LTR) */
margin-inline-start   = margin-left
margin-inline-end     = margin-right
margin-block-start    = margin-top
margin-block-end      = margin-bottom
margin-inline         = margin-left + margin-right (shorthand)
margin-block          = margin-top + margin-bottom (shorthand)

padding-inline-start  = padding-left
padding-inline-end    = padding-right
padding-block-start   = padding-top
padding-block-end     = padding-bottom

inset                 = top + right + bottom + left (shorthand like margin)
inset-inline-start    = left
inset-inline-end      = right
inset-block-start     = top
inset-block-end       = bottom

border-inline-start-width = border-left-width
/* ... etc. */

min-inline-size       = min-width
max-inline-size       = max-width
min-block-size        = min-height
max-block-size        = max-height
```

### Implementation

#### Step 1: Audit current implementation
Run WPT css-logical tests to find which ones fail:
```bash
go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-logical" -v
```

#### Step 2: Add missing shorthand expansions in `expandShorthand()`
The key missing ones are typically:
```go
case "margin-inline":
    // 1 value: all; 2 values: block-start block-end
    parts := strings.Fields(value)
    if len(parts) == 1 {
        style.Set("margin-left", parts[0])
        style.Set("margin-right", parts[0])
    } else if len(parts) == 2 {
        style.Set("margin-left", parts[0])
        style.Set("margin-right", parts[1])
    }
case "margin-block":
    parts := strings.Fields(value)
    if len(parts) == 1 {
        style.Set("margin-top", parts[0])
        style.Set("margin-bottom", parts[0])
    } else if len(parts) == 2 {
        style.Set("margin-top", parts[0])
        style.Set("margin-bottom", parts[1])
    }
case "padding-inline":
    // same pattern as margin-inline
case "padding-block":
    // same pattern as margin-block
case "inset":
    // Like margin: 4-value TRBL shorthand
    expandTRBLShorthand(style, "top", "right", "bottom", "left", value)
case "inset-inline":
    // Like margin-inline
case "inset-block":
    // Like margin-block
case "min-inline-size":
    style.Set("min-width", value)
case "max-inline-size":
    style.Set("max-width", value)
case "min-block-size":
    style.Set("min-height", value)
case "max-block-size":
    style.Set("max-height", value)
```

#### Step 3: Add border logical property handling
Logical border properties need to map to physical:
```go
case "border-inline":
    expandShorthand(style, "border-left", value)
    expandShorthand(style, "border-right", value)
case "border-block":
    expandShorthand(style, "border-top", value)
    expandShorthand(style, "border-bottom", value)
case "border-inline-start":
    expandShorthand(style, "border-left", value)
case "border-inline-end":
    expandShorthand(style, "border-right", value)
case "border-block-start":
    expandShorthand(style, "border-top", value)
case "border-block-end":
    expandShorthand(style, "border-bottom", value)
```

### WPT Tests to Create
Add 4 tests covering:
1. `margin-inline: auto` centering
2. `padding-block: 20px`
3. `inset: 10px` (= top:10 right:10 bottom:10 left:10)
4. `max-inline-size: 200px` width constraint

---

## Feature 8: `display: flow-root` and `contain` property basics

### Why critical
`display: flow-root` creates a new block formatting context — it's the modern,
semantics-free replacement for the `overflow: hidden` BFC hack that appears on
essentially every page layout. Without it, floats escape their containers and
margins collapse incorrectly.

`contain: layout` and `contain: paint` are used by performance-conscious sites
and component frameworks. Without parsing, they break layout by having unknown
display types.

### Current state
`display: flow-root` is not handled in `GetDisplay()`. It would fall through to
some default. `contain` is not parsed.

### CSS Specification Summary
```css
/* display: flow-root creates new BFC */
.clearfix { display: flow-root; }   /* = modern clearfix */
.container { display: flow-root; }  /* Contains floats */

/* contain: restricts layout/paint to element subtree */
.widget { contain: layout; }        /* Layout changes don't affect outside */
.panel  { contain: paint; }         /* Paints clipped to border-box */
.card   { contain: content; }       /* = layout + paint */
contain: strict;                    /* = size layout paint */
```

### Implementation

#### Step 1: Add `DisplayFlowRoot` to `GetDisplay()`
In `pkg/css/style.go`:
```go
const (
    // ... existing ...
    DisplayFlowRoot DisplayType = "flow-root"
)

func (s *Style) GetDisplay() DisplayType {
    if v, ok := s.Get("display"); ok {
        switch v {
        // ... existing cases ...
        case "flow-root": return DisplayFlowRoot
        }
    }
    return DisplayBlock // default
}
```

#### Step 2: Treat `DisplayFlowRoot` as `DisplayBlock` in layout
In `pkg/layout/layout_block.go`, everywhere `DisplayBlock` is checked, also check
`DisplayFlowRoot`:
```go
// Helper: isBlock returns true for any block-generating display type
func isBlockDisplay(d css.DisplayType) bool {
    return d == css.DisplayBlock || d == css.DisplayFlowRoot ||
           d == css.DisplayListItem
}
```

For BFC creation: `display: flow-root` MUST create a new BFC (like `overflow:hidden`
without the clipping). In float layout, when checking if a container creates a BFC
(for float containment), also check for `DisplayFlowRoot`:
```go
// In floats.go or layout_block.go — BFC check:
func createsBlockFormattingContext(box *Box) bool {
    if box.Style == nil { return false }
    d := box.Style.GetDisplay()
    if d == css.DisplayFlowRoot { return true }
    if box.Style.GetOverflow() != css.OverflowVisible { return true }
    // ... other BFC triggers ...
}
```

#### Step 3: Parse `contain` property
In `style.go` add:
```go
func (s *Style) GetContain() string {
    if v, ok := s.Get("contain"); ok { return v }
    return "none"
}
```

In layout, when `contain: layout` or `contain: strict` is set, treat as a new
BFC (same as `display: flow-root` for layout purposes):
```go
func createsBlockFormattingContext(box *Box) bool {
    // ... existing checks ...
    contain := box.Style.GetContain()
    if strings.Contains(contain, "layout") || contain == "strict" || contain == "content" {
        return true
    }
    return false
}
```

### WPT Tests to Create
`css-display-flow-root/flow-root-contains-float.html`: float inside flow-root should
be contained; outer container should have correct height.
`css-contain/contain-layout-001.html`: contain: layout creates BFC.

---

## Feature 9: `text-wrap: balance` and `text-wrap: pretty`

### Why critical
`text-wrap: balance` for headings is used by 5%+ of pages with modern CSS (all
major news sites, documentation sites, blogs). Without it, headings render with
jagged line breaks that look unprofessional. `text-wrap: pretty` is the `normal`
with better last-line orphan prevention.

### Current state
`text-wrap` is not parsed. Headings using it render with default wrapping.

### CSS Specification Summary
```css
h1 { text-wrap: balance; }   /* Lines are roughly equal length */
p  { text-wrap: pretty; }    /* Avoid orphans on last line (similar to normal, slightly different) */
   { text-wrap: nowrap; }    /* Same as white-space: nowrap */
   { text-wrap: normal; }    /* Default */
```

**`text-wrap: balance` algorithm**:
1. Find the total text width if put on one long line
2. Target width = total_width / ceil(total_lines)
3. Break lines at or before that target width

### Implementation

#### Step 1: Add getter in `style.go`
```go
func (s *Style) GetTextWrap() string {
    if v, ok := s.Get("text-wrap"); ok { return v }
    return "normal"
}
```

#### Step 2: Apply `text-wrap: balance` in `BreakLines()`
In `pkg/layout/layout_inline_multipass.go`, in the `BreakLines()` function:

```go
if constraint.TextWrap == "balance" {
    return breakLinesBalanced(items, constraint)
}
```

```go
func breakLinesBalanced(items []*InlineItem, constraint ConstraintSpace) []LineInfo {
    // Step 1: Measure total width of all text
    totalWidth := 0.0
    for _, item := range items {
        if item.Type == InlineItemText { totalWidth += item.Width }
    }

    // Step 2: Estimate number of lines with normal breaking
    normalLines := breakLinesNormal(items, constraint) // existing function
    nLines := float64(len(normalLines))
    if nLines <= 1 { return normalLines }

    // Step 3: Target line width = totalWidth / nLines
    targetWidth := totalWidth / nLines

    // Step 4: Break with reduced available width = targetWidth
    balancedConstraint := constraint
    balancedConstraint.AvailableWidth = math.Min(constraint.AvailableWidth, targetWidth)
    // Account for float exclusions etc. by using max(targetWidth, minRequiredWidth)

    return breakLinesNormal(items, balancedConstraint)
}
```

#### Step 3: Pass `TextWrap` through `ConstraintSpace`
Add to `pkg/layout/types.go` `ConstraintSpace` struct:
```go
TextWrap string // "normal", "balance", "pretty", "nowrap"
```

In `LayoutInlineContentToBoxes()`, read the style:
```go
constraint.TextWrap = containerBox.Style.GetTextWrap()
// text-wrap: nowrap = white-space: nowrap
if constraint.TextWrap == "nowrap" { constraint.NoWrap = true }
```

### WPT Tests to Create
`css-text-wrap/balance-basic-001.html`: heading with 3 words that fits on 1 line vs
2 balanced lines; reference uses manually balanced line breaks via `<br>`.

---

## Feature 10: `gap` in Flexbox (verification + fix if broken)

### Why critical
`gap` (formerly `grid-gap`) is now used in BOTH flex AND grid. It's the standard
spacing mechanism in Tailwind, Bootstrap 5, and every modern component framework.
Already partially implemented in flex (lines 140–157 in layout_flex.go) but needs
verification against WPT tests.

### Current state
`layout_flex.go` lines 140–157 read `row-gap` and `column-gap`. But:
- The `gap` shorthand (`gap: 16px`) may not be expanded to `row-gap`/`column-gap`
- Percentage gap values may not resolve correctly
- Gap in flex needs to be excluded from the flex item width calculation

### Implementation

#### Step 1: Verify `gap` shorthand expansion in `expandShorthand()`
```go
case "gap":
    parts := strings.Fields(value)
    if len(parts) == 1 {
        style.Set("row-gap", parts[0])
        style.Set("column-gap", parts[0])
    } else if len(parts) == 2 {
        style.Set("row-gap", parts[0])
        style.Set("column-gap", parts[1])
    }
case "row-gap", "column-gap":
    style.Set(property, value)
// Also handle legacy grid-gap:
case "grid-gap":
    parts := strings.Fields(value)
    if len(parts) == 1 {
        style.Set("row-gap", parts[0])
        style.Set("column-gap", parts[0])
    } else if len(parts) == 2 {
        style.Set("row-gap", parts[0])
        style.Set("column-gap", parts[1])
    }
case "grid-row-gap":
    style.Set("row-gap", value)
case "grid-column-gap":
    style.Set("column-gap", value)
```

#### Step 2: Verify gap is applied between ALL flex items (not just between wrapping lines)
In `layout_flex.go`, the gap should be applied as spacing between EACH flex item in
the main axis. Check the current implementation handles:
- Row flex (horizontal): `column-gap` between items
- Column flex (vertical): `row-gap` between items
- Multi-line flex (wrapped): `row-gap` between lines + `column-gap` within line

#### Step 3: Ensure gap doesn't cause overflow
Gap should be excluded from the total main-axis content width when checking for wrap.

### WPT Tests to Create
`css-flex-gap/flex-row-gap-001.html`: 3 items in row with gap:20px = two 20px gaps
`css-flex-gap/flex-column-gap-001.html`: flex-direction:column with gap:20px
`css-flex-gap/flex-gap-shorthand-001.html`: `gap: 10px 20px` → row-gap and column-gap

---

## Implementation Priority Order for Swarm Execution

All 10 features are independent and can run in parallel. Suggested assignment:

| Feature | Complexity | Expected Impact | Files to Modify |
|---------|-----------|-----------------|-----------------|
| 1. @layer | High | Very High | stylesheet.go, cascade.go, matcher.go |
| 2. CSS Nesting | Medium | Very High | stylesheet.go |
| 3. color-mix/oklch | Medium | High (Tailwind v4) | style.go (+ new colorspace.go) |
| 4. Grid repeat() | Medium | Very High | style.go, grid.go |
| 5. @keyframes/animation | Low | Medium | stylesheet.go, style.go |
| 6. font-variant | Low | Medium | style.go, render.go |
| 7. Logical properties | Low | High | style.go |
| 8. display: flow-root | Low | High | style.go, layout_block.go, floats.go |
| 9. text-wrap: balance | Medium | Medium | style.go, layout_inline_multipass.go |
| 10. gap in flex | Low | Very High | style.go, layout_flex.go |

---

## Agent Instructions

Each agent implementing one feature should:

1. **Read** the relevant source files first to understand current structure
2. **Add WPT test files** (HTML test + HTML reference) in the appropriate
   `pkg/visualtest/testdata/wpt-css3/css-FEATURENAME/` directory
3. **Implement** the feature following the approach described above
4. **Run tests** using:
   ```bash
   /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ \
     -run "TestWPTCSS3Reftests/css-FEATURENAME" -count=1 -v
   ```
5. **Run full regression** to verify nothing broke:
   ```bash
   /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ \
     -run "TestWPTCSS3Reftests|TestWPTReftests" -count=1 2>&1 | grep "Summary"
   ```
6. **Target**: MaxDifferentPercent=0.1 (< 480 diff pixels at 800×600). No FuzzyRadius.

### Key Architecture Notes for Agents

- **`pkg/css/style.go`**: Property getters, shorthand expansion (`expandShorthand()`),
  color parsing. Add new CSS property getters here.
- **`pkg/css/stylesheet.go`**: CSS parser. `ParseStylesheet()` is the entry point.
  `parseRule()` handles individual rules. Add new at-rule handlers here.
- **`pkg/css/cascade.go`**: Cascade resolution, specificity sorting. Add layer-aware
  sorting here.
- **`pkg/layout/grid.go`**: Grid track sizing and item placement. Add repeat/auto-fill here.
- **`pkg/layout/layout_flex.go`**: Flex layout algorithm. Add gap fixes here.
- **`pkg/layout/layout_block.go`**: Block layout, BFC detection. Add flow-root here.
- **`pkg/layout/layout_inline_multipass.go`**: Inline layout pipeline (3 phases).
  Add text-wrap: balance here.
- **`pkg/render/render.go`**: Rendering (paint). Add font-variant visual effects here.
- **Go binary**: `/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go`
- **Render test tool**: `cmd/l14open <input.html> <output.png> [width] [height]`
