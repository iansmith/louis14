# Plan: 10 High-Impact CSS Features (Round 2)

**Goal**: Implement 10 CSS features that fix significant rendering gaps for real websites.
These were chosen by cross-referencing:
- HTTP Archive / Web Almanac 2024–2025 usage data
- State of CSS 2025 survey
- projectwallace.com 2026 CSS analysis (100k+ sites)
- Direct inspection of what's missing in Louis14's codebase

**Current baseline**: CSS2 99/99, CSS3 298/298 at 0.1% threshold.

**WPT test policy**: Each feature must have WPT-style visual reftests that PASS after
implementation. All tests use `MaxDifferentPercent=0.1`, `FuzzyRadius=0`. Tests are in
`pkg/visualtest/testdata/wpt-css3/`. The WPT reftest format: a test HTML with
`<link rel="match" href="ref.html">` and a reference HTML that achieves the same
visual output without using the new feature.

**Execution model**: Each feature runs in its own git worktree as a parallel agent.
Run `./run-swarm.sh` to launch all 10 agents simultaneously.

---

## Feature 1: CSS Native Nesting

### Why critical
CSS native nesting (`&` reference and implicit nesting) is now universally supported in
browsers and emitted by every modern build tool (Vite, Webpack, Rollup). Angular 17+,
React frameworks, Tailwind v4, and Bootstrap 5.3 all emit nested CSS. Without it, ALL
nested rules inside a selector block are silently ignored.

Projectwallace 2026: CSS nesting is in the top 10 most-deployed new CSS features.

### Current state
`ParseStylesheet()` in `pkg/css/stylesheet.go` calls `splitRules(css)` which splits on
balanced braces, then `parseRules(ruleStr)`. But `parseRules()` calls `parseRule()` which
treats everything inside `{}` as declarations. Nested rules (inner `{` blocks) inside a
selector block are completely lost.

Example: `.card { background: white; .title { color: blue; } }` — `.title` rule is dropped.

### CSS Specification Summary
```css
/* 1. & reference — explicit parent selector substitution */
.parent {
  color: red;
  &:hover { color: blue; }       /* = .parent:hover */
  & > p { margin: 0; }           /* = .parent > p */
  .child & { font-size: 0.9em; } /* = .child .parent */
}

/* 2. Implicit nesting — no &, selector is automatically relative to parent */
.card {
  background: white;
  .title { font-size: 1.5em; }   /* = .card .title (& prepended automatically) */
  p { margin: 0; }               /* = .card p (implicit &) */
}

/* 3. @media nested inside a rule */
.container {
  width: 100%;
  @media (min-width: 600px) { width: 50%; }  /* Applies media condition to parent selector */
}

/* 4. Multi-level nesting */
.a { .b { .c { color: red; } } } /* = .a .b .c { color: red } */
```

### Implementation

**Location**: `pkg/css/stylesheet.go`

#### Step 1: Pre-process CSS to expand nesting before `splitRules()`

In `ParseStylesheet()`, before calling `splitRules(css)`, call:
```go
css = expandNesting(css, "")
```

#### Step 2: Implement `expandNesting()`

```go
// expandNesting recursively expands CSS native nesting into flat rules.
// parentSelector is the selector of the enclosing rule ("" for top-level).
func expandNesting(css string, parentSelector string) string {
    var result strings.Builder
    parts := splitRules(css)  // splitRules already handles brace depth

    for _, part := range parts {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }

        // Does this part contain a nested block? (has '{' and '}')
        bracePos := strings.Index(part, "{")
        if bracePos < 0 {
            // Flat declaration or at-rule statement — pass through
            if parentSelector != "" {
                // This is a flat declaration inside a rule — handled by caller
            }
            continue
        }

        sel := strings.TrimSpace(part[:bracePos])
        // Find matching closing brace
        closePos := strings.LastIndex(part, "}")
        if closePos < 0 {
            continue
        }
        body := part[bracePos+1 : closePos]

        // Handle at-rules inside a selector block (e.g., nested @media)
        if strings.HasPrefix(sel, "@media") || strings.HasPrefix(sel, "@supports") ||
           strings.HasPrefix(sel, "@container") {
            if parentSelector != "" {
                // Nested @media: move the parent selector inside the @media block
                // @media (min-width: 600px) { parentSel { body... } }
                inner := expandNesting(body, parentSelector)
                result.WriteString(sel + " { " + inner + " }\n")
            } else {
                // Top-level @media — pass through, let ParseStylesheet handle it
                result.WriteString(part + "\n")
            }
            continue
        }

        // Regular selector block
        if parentSelector != "" {
            // This is a NESTED rule inside a parent rule
            resolvedSel := resolveNestedSelector(parentSelector, sel)
            // Separate flat declarations from nested rules in body
            flatDecls, nestedParts := separateDeclsAndNested(body)
            // Emit flat rule with just this selector's declarations
            if strings.TrimSpace(flatDecls) != "" {
                result.WriteString(resolvedSel + " { " + flatDecls + " }\n")
            }
            // Recursively expand nested rules
            result.WriteString(expandNesting(nestedParts, resolvedSel))
        } else {
            // Top-level rule — separate flat declarations from nested rules
            flatDecls, nestedParts := separateDeclsAndNested(body)
            // Emit the top-level rule
            if strings.TrimSpace(flatDecls) != "" {
                result.WriteString(sel + " { " + flatDecls + " }\n")
            }
            // Expand any nested rules inside, passing sel as parent
            result.WriteString(expandNesting(nestedParts, sel))
        }
    }
    return result.String()
}

// resolveNestedSelector resolves a nested selector relative to the parent.
// If the nested selector contains '&', substitute it with the parent.
// Otherwise, implicitly prepend the parent with a descendant combinator.
func resolveNestedSelector(parent, nested string) string {
    if strings.Contains(nested, "&") {
        return strings.ReplaceAll(nested, "&", parent)
    }
    // Implicit: ".child" inside ".parent" becomes ".parent .child"
    return parent + " " + nested
}

// separateDeclsAndNested splits a CSS rule body into:
// - flatDecls: property declarations (no nested blocks)
// - nestedParts: string containing all nested rule blocks
func separateDeclsAndNested(body string) (flatDecls, nestedParts string) {
    var decls strings.Builder
    var nested strings.Builder

    // Split body into tokens: declarations end with ';', nested rules have '{ ... }'
    // We scan character by character, tracking brace depth
    depth := 0
    start := 0
    inString := false
    var stringChar byte

    for i := 0; i < len(body); i++ {
        ch := body[i]
        if inString {
            if ch == stringChar && (i == 0 || body[i-1] != '\\') {
                inString = false
            }
            continue
        }
        if ch == '"' || ch == '\'' {
            inString = true
            stringChar = ch
            continue
        }
        switch ch {
        case '{':
            if depth == 0 {
                // Everything from start to here is the nested rule's selector
                nested.WriteString(strings.TrimSpace(body[start:i+1]))
            }
            depth++
        case '}':
            depth--
            if depth == 0 {
                nested.WriteString(body[i:i+1])
                nested.WriteByte('\n')
                start = i + 1
            }
        case ';':
            if depth == 0 {
                // This is a flat declaration
                decls.WriteString(body[start : i+1])
                start = i + 1
            }
        }
    }
    // Remaining content (unterminated declarations)
    if remaining := strings.TrimSpace(body[start:]); remaining != "" && depth == 0 {
        decls.WriteString(remaining)
    }

    return decls.String(), nested.String()
}
```

#### Step 3: Hook into ParseStylesheet
At the top of `ParseStylesheet()`, right after the call to `stripCSSComments(css)`:
```go
// Expand CSS native nesting into flat rules before parsing
css = expandNesting(css, "")
```

### WPT Tests to Create

Source: https://github.com/web-platform-tests/wpt/tree/master/css/css-nesting

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-nesting/nesting-basic.html`
```html
<!DOCTYPE html>
<title>CSS Nesting: basic nested rule</title>
<link rel="match" href="nesting-basic-ref.html">
<style>
  body { margin: 0; }
  .parent {
    background: red;
    width: 200px;
    height: 100px;
    .child {
      background: lime;
      width: 100px;
      height: 100px;
    }
  }
</style>
<div class="parent"><div class="child"></div></div>
```
Reference (`nesting-basic-ref.html`): red 200×100 container with lime 100×100 child
inside (no nesting, plain selectors `.parent` and `.parent .child`).

**Test 2**: `nesting-ampersand-001.html` — `&:hover` and `&.active` don't match in static renderer but parse correctly.
```html
<!DOCTYPE html>
<title>CSS Nesting: & selector</title>
<link rel="match" href="nesting-ampersand-001-ref.html">
<style>
  body { margin: 0; }
  .box {
    background: red;
    width: 100px;
    height: 100px;
    &.active { background: lime; }
    &:hover { background: blue; }
  }
</style>
<div class="box active"></div>
```
Reference: lime 100×100 box (`.box.active` matches after nesting expansion).

**Test 3**: `nesting-media-inside-rule.html` — `@media` nested inside a selector.
```html
<!DOCTYPE html>
<title>CSS Nesting: @media nested inside rule</title>
<link rel="match" href="nesting-media-inside-rule-ref.html">
<style>
  body { margin: 0; }
  .box {
    background: red;
    width: 100px;
    height: 100px;
    @media (min-width: 100px) {
      background: lime;
    }
  }
</style>
<div class="box"></div>
```
Reference: lime 100×100 box (the media query matches our 800px viewport).

---

## Feature 2: Dynamic Viewport Units (dvh, svh, lvh, dvw, svw, lvw, etc.)

### Why critical
Dynamic viewport units are used on nearly every modern mobile-first site for hero sections,
sticky headers, and full-screen overlays. `height: 100dvh` prevents the content from being
obscured by mobile browser UI. Without them, the value is unrecognized → height = 0.

State of CSS 2025: `dvh` has >80% developer awareness and broad adoption.

### Current state
`ParseLengthFull()` in `pkg/css/style.go` handles: `px`, `em`, `rem`, `vw`, `vh`,
`vmin`, `vmax`, `cm`, `mm`, `in`, `pt`. The `dvh`, `svh`, `lvh`, `dvw`, `svw`,
`lvw`, `dvmin`, `dvmax`, `svmin`, `svmax`, `lvmin`, `lvmax` units are not handled.
They return `(0, false)` from ParseLength, causing elements to have 0 size.

### Implementation

**Location**: `pkg/css/style.go`, `ParseLengthFull()` function.

In a static renderer, we don't have a dynamic viewport (browser chrome never changes size).
So all dynamic units (dvh, svh, lvh) are equivalent to their static counterparts (vh, vh, vh):
- `dvh` = `svh` = `lvh` = `vh` (100% of viewport height)
- `dvw` = `svw` = `lvw` = `vw` (100% of viewport width)
- `dvmin` = `svmin` = `lvmin` = `vmin`
- `dvmax` = `svmax` = `lvmax` = `vmax`
- `cqw` = `cqh` (container query units — approximate as `vw`/`vh` if no container)
- `vi` = `vw` (inline axis in horizontal writing mode)
- `vb` = `vh` (block axis in horizontal writing mode)

Add these BEFORE the existing `vw`/`vh` checks (since `dvh` ends with `vh`, ordering matters):

```go
// Dynamic/static/large viewport units — treat all as equivalent to vh/vw
// in static renderer (no browser chrome changes).
for _, suffix := range []string{"dvh", "svh", "lvh"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil && vh > 0 {
            return num * vh / 100, true
        }
        return 0, false
    }
}
for _, suffix := range []string{"dvw", "svw", "lvw"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil && vw > 0 {
            return num * vw / 100, true
        }
        return 0, false
    }
}
for _, suffix := range []string{"dvmin", "svmin", "lvmin"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil {
            minDim := math.Min(vw, vh)
            return num * minDim / 100, true
        }
        return 0, false
    }
}
for _, suffix := range []string{"dvmax", "svmax", "lvmax"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil {
            maxDim := math.Max(vw, vh)
            return num * maxDim / 100, true
        }
        return 0, false
    }
}
// Container query units — approximate as viewport units
for _, suffix := range []string{"cqw", "cqi"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil && vw > 0 {
            return num * vw / 100, true
        }
        return 0, false
    }
}
for _, suffix := range []string{"cqh", "cqb"} {
    if strings.HasSuffix(val, suffix) {
        numStr := strings.TrimSuffix(val, suffix)
        if num, err := strconv.ParseFloat(numStr, 64); err == nil && vh > 0 {
            return num * vh / 100, true
        }
        return 0, false
    }
}
```

Also add `vb` (= `vh`) and `vi` (= `vw`) for the logical axis units.

### WPT Tests to Create

Source: https://github.com/web-platform-tests/wpt/tree/master/css/css-values-4

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-dynamic-viewport/dvh-basic-001.html`
```html
<!DOCTYPE html>
<title>dvh unit: 100dvh equals viewport height</title>
<link rel="match" href="dvh-basic-001-ref.html">
<style>
  body { margin: 0; }
  .box { background: lime; width: 100px; height: 100dvh; }
</style>
<div class="box"></div>
```
Reference: lime box 100×600px (= 100vh at 800×600 viewport).

**Test 2**: `dvw-basic-001.html` — `width: 50dvw` = 400px at 800px viewport.

**Test 3**: `svh-basic-001.html` — `100svh` renders as `100vh`.

---

## Feature 3: `ch` and `ex` Length Units

### Why critical
`ch` (width of the '0' character at current font size) is widely used for:
- Form field widths: `input { width: 20ch; }` — exactly wide enough for 20 characters
- Code blocks, monospace text sizing
- Modern UI kits use `ch` for baseline grid systems

`ex` (x-height of the current font) is used for:
- Decorative borders/rule heights matching text proportions
- Icon sizing relative to text

projectwallace 2026: `ch` unit appears in ~15% of CSS files analyzed.

### Current state
`ParseLengthFull()` does not handle `ch` or `ex`. They return `(0, false)`.
This breaks form layouts that use `width: 20ch`, which becomes `width: 0`.

### Implementation

**Location**: `pkg/css/style.go`, `ParseLengthFull()`.

Add after the `em` unit handling:
```go
// ch unit: width of '0' character at current font size.
// Exact measurement requires font access; approximate as 0.5em (typical for proportional fonts).
// Note: monospace fonts have ch ≈ 1em, but 0.5em is the right average for body text.
if strings.HasSuffix(val, "ch") {
    numStr := strings.TrimSuffix(val, "ch")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * fontSize * 0.5, true
    }
    return 0, false
}

// ex unit: x-height of the current font, approximately 0.5em.
if strings.HasSuffix(val, "ex") {
    numStr := strings.TrimSuffix(val, "ex")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * fontSize * 0.5, true
    }
    return 0, false
}

// lh unit: line-height (approximate as 1.2em since we don't have lh here)
if strings.HasSuffix(val, "lh") && !strings.HasSuffix(val, "rlh") {
    numStr := strings.TrimSuffix(val, "lh")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * fontSize * 1.2, true
    }
    return 0, false
}

// rlh unit: root line-height (approximate as 1.2 * 16px = 19.2px)
if strings.HasSuffix(val, "rlh") {
    numStr := strings.TrimSuffix(val, "rlh")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * 19.2, true // 16px default font size * 1.2 line-height
    }
    return 0, false
}

// ic unit: advance measure of the full-width CJK character '水'.
// Approximate as 1em for simplicity.
if strings.HasSuffix(val, "ic") {
    numStr := strings.TrimSuffix(val, "ic")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * fontSize, true
    }
    return 0, false
}

// cap unit: cap-height (uppercase letter height), approximately 0.7em.
if strings.HasSuffix(val, "cap") {
    numStr := strings.TrimSuffix(val, "cap")
    if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
        return num * fontSize * 0.7, true
    }
    return 0, false
}
```

**IMPORTANT**: The `ch` check must come BEFORE the `cm` check (since `ch` ends with `h`
which doesn't conflict, but `cm` comes before `ch` in the file currently). Also ensure
`ch` is checked before `c` (but there's no `c` unit). The `ex` check must come BEFORE
the `em` check (since `ex` doesn't end with `em`, no conflict — actually `ex` ends with
`x`, not `em`, so no ordering issue).

Actually double-check: `strings.HasSuffix("1ex", "em")` = false. OK, no conflict.
But `strings.HasSuffix("1ch", "h")` would match `vh` prematurely. Check ordering
carefully:
- If `vh` check comes before `ch` check, `"50ch"` would NOT match `vh` (correct, since
  `50ch` ends with `ch`, not `vh`). No conflict.

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-ch-unit/ch-width-001.html`
```html
<!DOCTYPE html>
<title>ch unit: 10ch width matches 10 zeros</title>
<link rel="match" href="ch-width-001-ref.html">
<style>
  body { margin: 0; font-family: monospace; font-size: 16px; }
  .box { width: 10ch; height: 20px; background: lime; overflow: hidden; }
</style>
<div class="box"></div>
```
Reference: A lime box whose width equals 10 × the width of '0' in the same monospace font.
Use explicit pixel width: `width: 80px` (16px × 10ch × 0.5 = 80px at monospace ~0.5em per ch).

Actually for monospace: measure `0000000000` (10 zeros) at 16px monospace. In Liberation Mono,
16px × 10 = about 96px. Use `width: 96px` in reference. Agent should measure this
empirically using the font metrics.

**Test 2**: `ex-height-001.html` — `1ex` height matches approximately half the em size.

---

## Feature 4: `env()` CSS Function

### Why critical
`env(safe-area-inset-top)` and friends are used by all iOS/PWA-optimized sites to avoid
the iPhone notch and home indicator bar. The patterns are:
```css
padding-top: env(safe-area-inset-top);
margin-bottom: env(safe-area-inset-bottom, 0px);
height: calc(100vh - env(safe-area-inset-top) - env(safe-area-inset-bottom));
```
Without `env()`, the `calc()` call returns 0 (env() returns empty string), and
`padding-top: env(safe-area-inset-top)` sets padding to 0 (acceptable but currently errors).

projectwallace 2026: env() appears in ~12% of stylesheets.

### Current state
`ParseLengthFull()` does not handle `env(...)`. The value is not recognized as a length
and returns `(0, false)`. In `calc()`, any `env()` reference causes the whole calc to fail.

### Implementation

**Location**: `pkg/css/style.go`

#### Step 1: Replace `env(...)` with its default value before length parsing

Add a helper function:
```go
// resolveEnvValue replaces env(variable-name) and env(variable-name, fallback)
// with either the known value or the fallback.
// In our static renderer, all standard env() values return 0px.
func resolveEnvValue(val string) string {
    val = strings.TrimSpace(val)
    lower := strings.ToLower(val)

    // Known env() variables and their static values
    // All safe-area-insets = 0 (no notch in static renderer)
    knownEnvVars := map[string]string{
        "safe-area-inset-top":    "0px",
        "safe-area-inset-right":  "0px",
        "safe-area-inset-bottom": "0px",
        "safe-area-inset-left":   "0px",
        "titlebar-area-x":        "0px",
        "titlebar-area-y":        "0px",
        "titlebar-area-width":    "100%",
        "titlebar-area-height":   "0px",
        "keyboard-inset-top":     "0px",
        "keyboard-inset-right":   "0px",
        "keyboard-inset-bottom":  "0px",
        "keyboard-inset-left":    "0px",
        "keyboard-inset-height":  "0px",
        "keyboard-inset-width":   "0px",
    }

    // Find all env(...) occurrences and replace them
    for strings.Contains(lower, "env(") {
        start := strings.Index(lower, "env(")
        if start < 0 { break }

        // Find the matching closing paren (handle nested parens)
        depth := 0
        end := start + 4 // after "env("
        for end < len(val) {
            switch val[end] {
            case '(':
                depth++
            case ')':
                if depth == 0 {
                    goto foundEnv
                }
                depth--
            }
            end++
        }
        break // malformed
    foundEnv:
        inner := strings.TrimSpace(val[start+4 : end])
        lower = lower[:start] + lower[end+1:]  // remove from lower for loop detection

        // Split on comma to get variable name and optional fallback
        commaIdx := strings.Index(inner, ",")
        varName := strings.ToLower(strings.TrimSpace(inner))
        fallback := "0px"
        if commaIdx >= 0 {
            varName = strings.ToLower(strings.TrimSpace(inner[:commaIdx]))
            fallback = strings.TrimSpace(inner[commaIdx+1:])
        }

        resolved := fallback // default to fallback
        if known, ok := knownEnvVars[varName]; ok {
            resolved = known
        }

        val = val[:start] + resolved + val[end+1:]
        lower = strings.ToLower(val)
    }
    return val
}
```

#### Step 2: Call `resolveEnvValue()` early in `ParseLengthFull()`
At the top of `ParseLengthFull()`, before any other processing:
```go
func ParseLengthFull(val string, fontSize, vw, vh float64) (float64, bool) {
    val = strings.TrimSpace(val)
    // Resolve env() variables before any other parsing
    if strings.Contains(val, "env(") {
        val = resolveEnvValue(val)
        val = strings.TrimSpace(val)
    }
    // ... rest of function ...
}
```

#### Step 3: Also handle `env()` in color parsing
In `parseColorToRGBA()`, handle the case where a color property contains `env(...)`.
For colors, `env()` typically isn't used, but for safety, normalize it there too.

#### Step 4: Handle `env()` in `EvalCalcWithPercent()`
The `calc()` evaluator in `style.go` needs to handle `env(...)` tokens. Before tokenizing
the calc expression, run `resolveEnvValue()` on the expression.

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-env/env-basic-001.html`
```html
<!DOCTYPE html>
<title>env() function: safe-area-inset returns 0</title>
<link rel="match" href="env-basic-001-ref.html">
<style>
  body { margin: 0; }
  .box {
    background: lime;
    width: 100px;
    height: 100px;
    padding-top: env(safe-area-inset-top, 0px);
    margin-top: env(safe-area-inset-top);
  }
</style>
<div class="box"></div>
```
Reference: lime 100×100 box at top-left (env() returns 0, so no extra padding/margin).

**Test 2**: `env-calc-001.html` — `height: calc(100px - env(safe-area-inset-top))` = 100px.

---

## Feature 5: `@media` Interaction and Additional Feature Queries

### Why critical
Modern CSS extensively uses interaction media features. Without them:
- `@media (hover: hover)` → **returns false** (never matches) — many sites use this to
  conditionally show hover effects
- `@media (pointer: fine)` → **returns false** — sites use this for fine-grained controls
- `@media print` → **returns false** (correct! But `@media screen` also needs to work)
- `@media (orientation: landscape)` → **returns false** — affects column layouts

projectwallace 2026: `@media (hover: hover)` = 41% adoption, `@media print` = 62%.

### Current state
`evaluateMediaCondition()` in `stylesheet.go` only handles `prefers-color-scheme`,
`prefers-reduced-motion`, `min-width`, `max-width`, `min-height`, `max-height`.
ALL other media features `return false` (never match). This is wrong for many common cases.

The media type check also has a bug: `@media print { ... }` doesn't match (correct), but
`@media screen` is handled by the `mq.MediaType == "screen"` check which returns true.

### Implementation

**Location**: `pkg/css/stylesheet.go`, `evaluateMediaCondition()` and `EvaluateMediaQuery()`.

#### Step 1: Fix `EvaluateMediaQuery()` for media types
```go
func EvaluateMediaQuery(mq *MediaQuery, viewportWidth, viewportHeight float64) bool {
    if mq == nil { return true }

    // Check media type
    switch mq.MediaType {
    case "all", "screen", "":
        // matches — we render for screen
    case "print":
        return false  // Print media: never match in screen renderer
    default:
        return false  // Unknown media types: don't match
    }

    // Check conditions
    for _, cond := range mq.Conditions {
        if !evaluateMediaCondition(cond, viewportWidth, viewportHeight) {
            return false
        }
    }
    return true
}
```

#### Step 2: Expand `evaluateMediaCondition()` with all missing features
```go
func evaluateMediaCondition(cond MediaCondition, viewportWidth, viewportHeight float64) bool {
    feature := strings.TrimSpace(strings.ToLower(cond.Feature))
    value := strings.TrimSpace(strings.ToLower(cond.Value))

    switch feature {
    case "prefers-color-scheme":
        return value == "light"  // static renderer = light mode
    case "prefers-reduced-motion":
        return value == "reduce" // static = no motion
    case "prefers-reduced-transparency":
        return value == "no-preference" || value == "reduce"
    case "prefers-contrast":
        return value == "no-preference" || value == ""
    case "forced-colors":
        return value == "none"  // no forced colors in static renderer
    case "inverted-colors":
        return value == "none"

    // Interaction media features — desktop defaults
    case "pointer":
        return value == "fine" || value == ""  // desktop = fine pointer (mouse)
    case "any-pointer":
        return value == "fine" || value == "none" || value == ""
    case "hover":
        return value == "hover" || value == "" // desktop = can hover
    case "any-hover":
        return value == "hover" || value == "none" || value == ""

    // Orientation — based on viewport aspect ratio
    case "orientation":
        if viewportWidth >= viewportHeight {
            return value == "landscape"
        }
        return value == "portrait"

    // Display mode
    case "display-mode":
        return value == "browser" || value == ""  // rendering as a browser

    // Scripting — we don't run JS but say "enabled" for compatibility
    case "scripting":
        return value == "enabled" || value == "initial-only" || value == ""

    // Color media features
    case "color":
        return true  // we support color
    case "color-index":
        return value == "" || value == "0"
    case "monochrome":
        return value == "0" || value == ""  // not monochrome

    // Resolution
    case "resolution":
        return true  // always match (we don't check actual dpi)
    case "min-resolution", "max-resolution":
        return true  // simplified: always match

    // Device dimensions (legacy — treat as viewport)
    case "device-width", "device-height":
        return true
    case "min-device-width", "min-device-height",
         "max-device-width", "max-device-height":
        return true

    // Numeric dimension features — existing cases
    default:
        // Fall through to numeric parsing
    }

    // Parse numeric value
    numVal, unit := parseMediaLength(cond.Value)
    if unit != "px" {
        return true  // Unknown unit = assume match
    }

    switch feature {
    case "min-width":  return viewportWidth >= numVal
    case "max-width":  return viewportWidth <= numVal
    case "min-height": return viewportHeight >= numVal
    case "max-height": return viewportHeight <= numVal
    case "width":      return viewportWidth == numVal
    case "height":     return viewportHeight == numVal
    default:
        return false  // Unknown feature = don't match
    }
}
```

#### Step 3: Ensure `not` modifier works in media queries
If the media query parser stores negation (e.g., `@media not (hover: none)`), handle the
`not` modifier in `EvaluateMediaQuery()`.

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-media-features/media-hover-001.html`
```html
<!DOCTYPE html>
<title>@media (hover: hover) matches in desktop renderer</title>
<link rel="match" href="media-hover-001-ref.html">
<style>
  body { margin: 0; }
  .box { background: red; width: 100px; height: 100px; }
  @media (hover: hover) { .box { background: lime; } }
</style>
<div class="box"></div>
```
Reference: lime 100×100 box (hover: hover matches for desktop rendering).

**Test 2**: `media-pointer-001.html` — `@media (pointer: fine)` matches.

**Test 3**: `media-orientation-001.html` — `@media (orientation: landscape)` matches at 800×600.

**Test 4**: `media-print-001.html` — `@media print` does NOT match; content uses screen rules.

---

## Feature 6: `image-set()` Function for Background Images

### Why critical
`image-set()` provides resolution-aware background images. Used for high-DPI displays:
```css
background-image: image-set(url("icon.png") 1x, url("icon@2x.png") 2x);
background-image: image-set(url("hero.avif") type("image/avif"), url("hero.jpg") type("image/jpeg"));
```
Without `image-set()`, background images on sites using this pattern are completely missing.

projectwallace 2026: image-set() used in ~8% of stylesheets.

### Current state
`GetBackgroundImage()` in `pkg/css/style.go` calls `extractFirstURL()` and then
`ParseLinearGradient()`/`ParseRadialGradient()`/`ParseConicGradient()`. It does not
handle `image-set(...)`. The background image evaluates to empty/nil.

### Implementation

**Location**: `pkg/css/style.go`, in `GetBackgroundImage()` or the background layer parser.

#### Step 1: Detect `image-set()` in the background value

In `GetBackgroundImage()` (or wherever individual background-image layers are parsed),
after trying to extract `url(...)`, also handle `image-set(...)`:

```go
// extractImageSetFirstURL extracts the first URL from an image-set() expression.
// Selects the 1x image (lowest resolution) or first type-compatible image.
// image-set( url("a.png") 1x, url("b.png") 2x ) → "a.png"
// image-set( url("a.avif") type("image/avif"), url("b.jpg") type("image/jpeg") ) → "a.avif"
func extractImageSetFirstURL(val string) string {
    lower := strings.ToLower(strings.TrimSpace(val))

    // Find image-set( or -webkit-image-set(
    imageSetStart := strings.Index(lower, "image-set(")
    if imageSetStart < 0 {
        imageSetStart = strings.Index(lower, "-webkit-image-set(")
        if imageSetStart < 0 { return "" }
        imageSetStart += len("-webkit-")
    }
    imageSetStart += len("image-set(")

    // Find the matching close paren
    depth := 1
    end := imageSetStart
    for end < len(val) && depth > 0 {
        switch val[end] {
        case '(': depth++
        case ')': depth--
        }
        if depth > 0 { end++ }
    }

    inner := val[imageSetStart:end]

    // Split into comma-separated entries (respecting nested parens)
    entries := splitCSSFunctionArgs(inner)

    // Extract the first URL entry
    for _, entry := range entries {
        entry = strings.TrimSpace(entry)
        // Each entry is: url("path") [resolution | type("mime")]
        // Extract the URL part
        url := extractFirstURL(entry)
        if url != "" { return url }
    }
    return ""
}
```

#### Step 2: Use `extractImageSetFirstURL()` when `image-set(` is present

In the background-image value parsing, before or alongside `extractFirstURL()`:
```go
// In the background image layer parsing:
imageVal := layerValue
if strings.Contains(strings.ToLower(imageVal), "image-set(") ||
   strings.Contains(strings.ToLower(imageVal), "-webkit-image-set(") {
    url := extractImageSetFirstURL(imageVal)
    if url != "" {
        // Treat as a simple url() reference
        imageVal = "url(" + url + ")"
    }
}
url := extractFirstURL(imageVal)
```

Also update `expandBackgroundProperty()` to detect `image-set(` as a background-image indicator:
```go
// In expandBackgroundProperty, detect image-set as background-image:
if strings.Contains(lowerVal, "image-set(") || strings.Contains(lowerVal, "-webkit-image-set(") {
    // Background is an image-set — extract first URL
    url := extractImageSetFirstURL(value)
    if url != "" {
        style.Set("background-image", "url("+url+")")
    }
}
```

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-image-set/image-set-basic-001.html`
Use a data URI PNG as the 1x image (avoids network dependency):
```html
<!DOCTYPE html>
<title>image-set(): renders first URL</title>
<link rel="match" href="image-set-basic-001-ref.html">
<style>
  body { margin: 0; }
  .box {
    width: 100px;
    height: 100px;
    /* A 1x1 lime pixel as data URI */
    background-image: image-set(
      url("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==") 1x,
      url("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI6QAAAABJRU5EKJggg==") 2x
    );
    background-size: cover;
    background-color: lime;
  }
</style>
<div class="box"></div>
```
Reference: lime 100×100 box (since the 1x1 lime data URI covers the box background).

Actually, use simpler: create a small colored div reference.

**Test 2**: `image-set-type-001.html` — `image-set(url(...) type("image/png"))` selects
the first image regardless of type.

---

## Feature 7: `color()` Function with Named Color Spaces

### Why critical
`color(srgb r g b)` and `color(display-p3 r g b)` are used by modern design systems
for wide-gamut colors. Apple's design systems, CSS Color Level 4 implementations in
Tailwind v4.1, and many production sites use `color(display-p3 0.5 0.2 0.8)`.

State of CSS 2025: `color()` function has growing adoption as wide-gamut displays proliferate.

### Current state
`parseColorToRGBA()` in `pkg/css/style.go` handles: hex, rgb/rgba, hsl/hsla, named colors,
oklch, lch, hwb, color-mix. It does NOT handle the `color()` function with named color
spaces. These values return `color.RGBA{}` (transparent black).

### CSS Specification Summary
```css
/* color(colorspace r g b / alpha) */
color: color(srgb 1 0 0);           /* pure red, same as rgb(255 0 0) */
color: color(srgb 0.5 0.5 0.5);    /* gray */
color: color(display-p3 0.5 0.2 0.8 / 0.9);  /* purple in P3 gamut */
color: color(a98-rgb 0.5 0.5 0.5); /* Adobe 98 color space */
color: color(prophoto-rgb 0.5 0.5 0.5);
color: color(rec2020 0.5 0.5 0.5);
color: color(xyz-d65 0.2 0.2 0.2); /* CIE XYZ with D65 white point */
color: color(xyz-d50 0.2 0.2 0.2); /* CIE XYZ with D50 white point */
```

### Implementation

**Location**: `pkg/css/style.go` or `pkg/css/colorspace.go`, in `parseColorToRGBA()`.

#### Step 1: Detect `color(...)` function

In the switch/if chain of `parseColorToRGBA()`:
```go
if strings.HasPrefix(lower, "color(") && strings.HasSuffix(lower, ")") {
    return parseColorFunction(lower[6 : len(lower)-1])
}
```

#### Step 2: Implement `parseColorFunction()`

```go
// parseColorFunction parses a CSS color() function body.
// The input is the content between "color(" and ")".
// Returns sRGB RGBA with values clamped to [0,1].
func parseColorFunction(inner string) color.RGBA {
    inner = strings.TrimSpace(inner)

    // Split into: colorspace r g b [/ alpha]
    // Handle "/" for alpha separator
    parts := strings.Fields(inner)
    if len(parts) < 4 { return color.RGBA{} }

    colorSpace := strings.ToLower(parts[0])

    // Parse r, g, b values (0-1 range or 0%-100%)
    parseComp := func(s string) float64 {
        s = strings.TrimSuffix(s, "/") // remove trailing slash if present
        if strings.HasSuffix(s, "%") {
            v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
            if err != nil { return 0 }
            return v / 100.0
        }
        v, err := strconv.ParseFloat(s, 64)
        if err != nil { return 0 }
        return v
    }

    // Handle "/ alpha" notation — parts may be: ["srgb", "0.5", "0.2", "0.8"]
    // or with alpha: ["srgb", "0.5", "0.2", "0.8", "/", "0.5"] or ["srgb", "0.5", "0.2", "0.8/0.5"]
    // Rejoin and re-split on "/" to separate alpha
    remainder := strings.Join(parts[1:], " ")
    slashIdx := strings.Index(remainder, "/")
    alpha := 1.0
    rgbStr := remainder
    if slashIdx >= 0 {
        alphaStr := strings.TrimSpace(remainder[slashIdx+1:])
        alpha = parseColorFloat01(alphaStr, 1.0)
        rgbStr = remainder[:slashIdx]
    }
    rgbParts := strings.Fields(rgbStr)
    if len(rgbParts) < 3 { return color.RGBA{} }

    r := parseComp(rgbParts[0])
    g := parseComp(rgbParts[1])
    b := parseComp(rgbParts[2])

    // Convert to sRGB based on color space
    switch colorSpace {
    case "srgb", "srgb-linear":
        // sRGB: values are already in sRGB [0,1]
        if colorSpace == "srgb-linear" {
            // Linear sRGB: apply gamma
            r = linearToSRGB(r)
            g = linearToSRGB(g)
            b = linearToSRGB(b)
        }
    case "display-p3":
        // Display P3 → sRGB conversion
        // P3 uses D65 white point, same as sRGB but different primaries
        r, g, b = p3ToSRGB(r, g, b)
    case "a98-rgb":
        // Adobe RGB (1998) → sRGB
        r, g, b = a98RGBToSRGB(r, g, b)
    case "prophoto-rgb":
        // ProPhoto RGB → sRGB (via XYZ D50)
        r, g, b = prophotoToSRGB(r, g, b)
    case "rec2020":
        // Rec. 2020 → sRGB
        r, g, b = rec2020ToSRGB(r, g, b)
    case "xyz", "xyz-d65":
        // CIE XYZ D65 → sRGB
        r, g, b = xyzD65ToSRGB(r, g, b)
    case "xyz-d50":
        // CIE XYZ D50 → sRGB (via chromatic adaptation)
        r, g, b = xyzD50ToSRGB(r, g, b)
    default:
        // Unknown color space — treat as sRGB
    }

    // Clamp and convert to uint8
    return color.RGBA{
        R: uint8(colorClamp01(r) * 255),
        G: uint8(colorClamp01(g) * 255),
        B: uint8(colorClamp01(b) * 255),
        A: uint8(colorClamp01(alpha) * 255),
    }
}
```

#### Step 3: Add display-p3 → sRGB conversion

P3 to sRGB requires going through linear light:
```go
// p3ToSRGB converts Display P3 [0,1] to sRGB [0,1].
func p3ToSRGB(r, g, b float64) (float64, float64, float64) {
    // Step 1: P3 gamma decode to linear light
    linearP3 := func(c float64) float64 {
        if c < 0 { return -linearP3(-c) }
        if c <= 0.04045 { return c / 12.92 }
        return math.Pow((c+0.055)/1.055, 2.4)
    }
    rl := linearP3(r)
    gl := linearP3(g)
    bl := linearP3(b)

    // Step 2: P3 linear → XYZ D65 (Display P3 primaries)
    x := 0.4866*rl + 0.2657*gl + 0.1982*bl
    y := 0.2290*rl + 0.6917*gl + 0.0793*bl
    z := 0.0000*rl + 0.0451*gl + 1.0439*bl

    // Step 3: XYZ D65 → linear sRGB
    rls :=  3.2404542*x - 1.5371385*y - 0.4985314*z
    gls := -0.9692660*x + 1.8760108*y + 0.0415560*z
    bls :=  0.0556434*x - 0.2040259*y + 1.0572252*z

    // Step 4: Linear sRGB → gamma sRGB
    return linearToSRGB(rls), linearToSRGB(gls), linearToSRGB(bls)
}
```

Add similar functions for `a98RGBToSRGB`, `rec2020ToSRGB`, `xyzD65ToSRGB`, `xyzD50ToSRGB`
in `pkg/css/colorspace.go`.

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-color-function/color-srgb-001.html`
```html
<!DOCTYPE html>
<title>color() function: sRGB color space</title>
<link rel="match" href="color-srgb-001-ref.html">
<style>
  body { margin: 0; }
  .r { background: color(srgb 1 0 0); width: 100px; height: 50px; }
  .g { background: color(srgb 0 1 0); width: 100px; height: 50px; }
  .b { background: color(srgb 0 0 1); width: 100px; height: 50px; }
</style>
<div class="r"></div><div class="g"></div><div class="b"></div>
```
Reference: `color(srgb 1 0 0)` = red, `color(srgb 0 1 0)` = lime, `color(srgb 0 0 1)` = blue.
Use explicit `rgb()` colors in the reference.

**Test 2**: `color-p3-001.html` — `color(display-p3 1 0 0)` (pure P3 red → slightly
different from sRGB red, but renders visually close).

---

## Feature 8: CSS Grid Subgrid

### Why critical
Subgrid allows a nested grid to participate in the parent grid's track definitions,
enabling alignment across nested components. Used heavily in:
- Card layouts where all cards need aligned content areas
- Form layouts where labels and inputs align across rows
- Component libraries (Adobe Spectrum, Shoelace, Panda CSS)

State of CSS 2025: Subgrid has >85% browser support and growing adoption.
Can't be easily polyfilled — without it, nested grids have misaligned columns.

### Current state
`parseGridTracks()` handles: px, %, fr, auto, min-content, max-content, minmax(), repeat().
The keyword `subgrid` is not handled. When `grid-template-columns: subgrid` is set on a
grid item that is itself a grid container, it receives no track definitions (empty slice).

### CSS Specification Summary
```css
.parent-grid {
  display: grid;
  grid-template-columns: 1fr 2fr 1fr;
  grid-template-rows: auto auto;
}

.child-item {
  display: grid;
  grid-column: 1 / 4;            /* spans all 3 parent columns */
  grid-template-columns: subgrid; /* inherits parent's 3-column track sizes */
}

/* Subgrid with named lines */
.parent-grid {
  grid-template-columns: [start] 1fr [middle] 2fr [end];
}
.child-item {
  grid-template-columns: subgrid;  /* inherits [start], [middle], [end] lines */
}
```

### Implementation

**Location**: `pkg/css/style.go` (GridTrack struct) and `pkg/layout/grid.go`.

#### Step 1: Add `IsSubgrid` field to `GridTrack`
```go
type GridTrack struct {
    // ... existing fields ...
    IsSubgrid bool  // true if this represents a "subgrid" keyword
}
```

In `parseGridTracks()`, when the token is `"subgrid"`:
```go
case "subgrid":
    tracks = append(tracks, GridTrack{IsSubgrid: true})
```

#### Step 2: Add `Subgrid` flags to grid style getters
```go
func (s *Style) GridTemplateColumnsIsSubgrid() bool {
    v, _ := s.Get("grid-template-columns")
    return strings.TrimSpace(strings.ToLower(v)) == "subgrid"
}

func (s *Style) GridTemplateRowsIsSubgrid() bool {
    v, _ := s.Get("grid-template-rows")
    return strings.TrimSpace(strings.ToLower(v)) == "subgrid"
}
```

#### Step 3: Pass parent tracks to subgrid child in `layoutGridContainer()`

In `pkg/layout/grid.go`, in `layoutGridContainer()`:

1. When laying out a grid item that is itself a grid container AND
   `GridTemplateColumnsIsSubgrid()` is true:
2. Instead of resolving its own column tracks, use the PARENT grid's resolved column
   tracks that the item spans.

```go
// In layoutGridContainer(), when processing a grid item:
if childStyle.GetDisplay() == css.DisplayGrid || childStyle.GetDisplay() == css.DisplayInlineGrid {
    if childStyle.GridTemplateColumnsIsSubgrid() {
        // Child is a subgrid — pass parent column tracks for spanned columns
        colSpan := item.ColEnd - item.ColStart  // number of parent columns spanned
        subgridColTracks := parentResolvedColTracks[item.ColStart-1 : item.ColEnd-1]
        // Override child's column tracks with parent's spanned tracks
        layoutSubgrid(item, subgridColTracks, childRowTracks, ...)
    }
}
```

The `layoutSubgrid()` function is the same as `layoutGridContainer()` but uses pre-resolved
tracks from the parent instead of computing its own.

#### Implementation Complexity Note
Full subgrid is complex because the parent grid and child subgrid are tightly coupled in
their sizing algorithm. A simplified approach for initial implementation:

**Simplified approach**: When `grid-template-columns: subgrid` is detected, use the
available width of the grid item divided equally by the number of spanned columns as track sizes.
This won't be pixel-perfect for all cases but handles the common case of equal columns.

```go
if childStyle.GridTemplateColumnsIsSubgrid() {
    // Simplified: divide available width equally among spanned columns
    colCount := item.ColEnd - item.ColStart
    if colCount <= 0 { colCount = 1 }
    trackWidth := item.Width / float64(colCount)
    subgridTracks := make([]float64, colCount)
    for i := range subgridTracks {
        subgridTracks[i] = trackWidth
    }
    // Use subgridTracks as column track widths when placing child items
}
```

### WPT Tests to Create

Source: https://github.com/web-platform-tests/wpt/tree/master/css/css-grid/subgrid

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-grid-subgrid/subgrid-basic-001.html`
```html
<!DOCTYPE html>
<title>CSS Grid Subgrid: basic column subgrid</title>
<link rel="match" href="subgrid-basic-001-ref.html">
<style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 100px 100px 100px;
    width: 300px;
  }
  .item {
    grid-column: 1 / 4;
    display: grid;
    grid-template-columns: subgrid;
  }
  .cell { height: 50px; background: lime; }
  .cell:nth-child(2) { background: red; }
  .cell:nth-child(3) { background: blue; }
</style>
<div class="grid">
  <div class="item">
    <div class="cell"></div>
    <div class="cell"></div>
    <div class="cell"></div>
  </div>
</div>
```
Reference: Three 100px-wide cells (lime, red, blue) in a row.

---

## Feature 9: `::marker` Pseudo-Element

### Why critical
`::marker` allows styling list item bullets and numbers. Used in:
- Documentation sites (custom bullet colors matching the design system)
- Navigation menus (custom markers)
- FAQ accordions with stylized markers
- Tailwind's `list-*` utilities use `::marker`

Without `::marker`, `:marker` CSS rules are silently dropped and lists use
browser-default black bullets.

### Current state
`matcher.go` handles `::before`, `::after`, `::first-line`, `::first-letter` as
pseudo-elements. `::marker` is not handled. In `layout_block.go` / `render.go`,
list item markers are rendered using `GetListStyleType()` but don't apply `::marker` styles.

### Implementation

**Location**: `pkg/css/matcher.go`, `pkg/layout/layout_block.go`, `pkg/render/render.go`.

#### Step 1: Add `::marker` pseudo-element to the cascade

In `matcher.go`, in the pseudo-element matching section:
```go
case "marker":
    // ::marker matches the list item marker box
    // Treat as a special pseudo-element on display:list-item elements
    return node.Type == html.ElementNode && isListItem(node)
```

Where `isListItem()` returns true for `<li>`, `<summary>`, and elements with
`display: list-item`.

In `cascade.go`, when computing styles for a list item, also apply `::marker` rules
to the marker style:
```go
// Compute ::marker pseudo-element styles for list items
if style.GetDisplay() == css.DisplayListItem {
    markerStyle := computePseudoElementStyle(node, "marker", stylesheets)
    if markerStyle != nil {
        // Store marker style so renderer can use it
        style.SetMarkerStyle(markerStyle)
    }
}
```

#### Step 2: Apply marker styles in rendering

In `pkg/render/render.go` or `pkg/layout/layout_block.go`, when drawing the list marker
(the bullet disc/circle or number), use the `::marker` style if available:

```go
// Get marker color — use ::marker { color } if set, else inherit from element
markerColor := box.Style.GetColor() // default: inherit
if markerStyle := box.Style.GetMarkerStyle(); markerStyle != nil {
    if c, ok := markerStyle.GetColorOK(); ok {
        markerColor = c
    }
    // Also check font-size, content, etc.
}
```

#### Step 3: Add `GetMarkerStyle()` to Style

In `pkg/css/style.go`, add a field/method for storing and retrieving the `::marker`
computed style:
```go
// MarkerStyle stores the computed ::marker pseudo-element style, if any.
// Set during cascade computation for list items.
func (s *Style) GetMarkerStyle() *Style {
    if s.markerStyle != nil {
        return s.markerStyle
    }
    return nil
}

func (s *Style) SetMarkerStyle(ms *Style) {
    s.markerStyle = ms
}
```

Add `markerStyle *Style` field to the `Style` struct.

#### Simpler alternative approach
Instead of a new field, store the `::marker` style properties directly in the list item's
style using a special prefix:
```go
// In cascade.go when applying ::marker rules to a list item:
for prop, val := range markerDeclarations {
    style.Set("--marker-"+prop, val)
}

// In render.go when drawing marker:
markerColorStr, _ := box.Style.Get("--marker-color")
```

This avoids adding a new field to `Style`.

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-marker/marker-color-001.html`
```html
<!DOCTYPE html>
<title>::marker pseudo-element: color</title>
<link rel="match" href="marker-color-001-ref.html">
<style>
  body { margin: 0; font-size: 16px; font-family: serif; }
  ul { margin: 0; padding-left: 20px; }
  li::marker { color: lime; }
  li { color: black; }
</style>
<ul><li>Item 1</li></ul>
```
Reference: List item with lime-colored bullet and black text.

**Test 2**: `marker-content-001.html` — `::marker { content: "→ " }` overrides the bullet.

---

## Feature 10: Vendor-Prefixed Width Values and `:any-link` / Missing Pseudo-Classes

### Why critical
Two categories of common real-world CSS failures:

**A) Vendor-prefixed width keywords**:
- `width: -webkit-fill-available` — fills parent width (like `width: 100%` but in flex contexts)
- `width: -moz-available` — Firefox version
- `width: -webkit-fit-content` — shrinks to content (like `width: fit-content`)
- `height: -webkit-fill-available` — fills parent height
These appear in CSS reset libraries, component libraries, and vendor-specific optimizations.

**B) Missing pseudo-classes** that cause syntax errors or silent failures:
- `:any-link` — matches all links (`:link` + `:visited`), very common in reset CSS
- `:local-link` — matches local anchors
- `:target` — matches element with matching URL fragment
- `:placeholder-shown` — input showing placeholder text
- `:autofill` / `:-webkit-autofill` — autofilled form fields
- `:paused` / `:playing` — animation state
- `:muted` / `:volume-locked` — media element state

### Current state
`ParseLengthFull()` returns `(0, false)` for `-webkit-fill-available` and similar values.
In `pkg/css/matcher.go`, unknown pseudo-classes return `false` (never match, which is
correct behavior for dynamic pseudo-classes, but they must parse without breaking rules).

### Implementation

#### Part A: Vendor-prefixed width/height values

**Location**: `pkg/css/style.go`, in `GetWidth()` and `GetHeight()` getters, or in
the length parsing pipeline.

Add normalization of vendor-prefixed keywords before length parsing:

```go
// normalizeVendorPrefixedValue resolves common vendor-prefixed length/width values.
func normalizeVendorPrefixedValue(val string) string {
    lower := strings.ToLower(strings.TrimSpace(val))
    switch lower {
    case "-webkit-fill-available", "-moz-available",
         "-webkit-stretch", "stretch":
        return "100%"  // fill-available ≈ 100% in most contexts
    case "-webkit-fit-content", "-moz-fit-content",
         "fit-content":
        return "fit-content"  // already handled, just normalize prefix
    case "-webkit-max-content", "-moz-max-content":
        return "max-content"
    case "-webkit-min-content", "-moz-min-content":
        return "min-content"
    }
    return val
}
```

Call this in `GetWidth()`, `GetHeight()`, `GetMinWidth()`, `GetMaxWidth()`,
`GetFlexBasis()`:
```go
func (s *Style) GetWidth() (string, bool) {
    v, ok := s.Get("width")
    if ok {
        v = normalizeVendorPrefixedValue(v)
    }
    return v, ok
}
```

Or, add it to `expandShorthand()` when setting width/height:
```go
case "width", "height", "min-width", "max-width", "min-height", "max-height",
     "flex-basis":
    style.Set(property, normalizeVendorPrefixedValue(value))
```

#### Part B: Missing pseudo-classes

**Location**: `pkg/css/matcher.go`, `matchPseudoClass()`.

Add the missing pseudo-classes with their correct static-renderer behavior:

```go
// Pseudo-classes that are always false in static renderer (no state/interaction):
case pc == "focus-visible", pc == "focus-within":
    return false  // no focus in static rendering
case pc == "target":
    return false  // no URL fragment matching in static rendering
case pc == "placeholder-shown":
    return false  // inputs don't show placeholders in static render
case pc == "autofill", pc == strings.HasPrefix(pc, "-webkit-autofill"):
    return false
case pc == "paused", pc == "playing":
    return false  // no animation state
case pc == "muted", pc == "volume-locked":
    return false  // no media element state

// :any-link matches all <a>, <area>, <link> elements with href attribute
case pc == "any-link", pc == "-webkit-any-link":
    if node.Type != html.ElementNode { return false }
    tag := strings.ToLower(node.Data)
    if tag != "a" && tag != "area" && tag != "link" { return false }
    for _, attr := range node.Attr {
        if attr.Key == "href" { return true }
    }
    return false

// :local-link — matches anchors linking to the current page
case pc == "local-link":
    return false  // can't determine current URL in static render

// :scope — matches the element serving as the reference scope
case pc == "scope":
    return node == node.Parent  // approximate: root element
```

### WPT Tests to Create

**Test 1**: `pkg/visualtest/testdata/wpt-css3/css-vendor-values/fill-available-001.html`
```html
<!DOCTYPE html>
<title>-webkit-fill-available: fills parent width</title>
<link rel="match" href="fill-available-001-ref.html">
<style>
  body { margin: 0; }
  .container { width: 200px; height: 100px; background: red; }
  .child { width: -webkit-fill-available; height: 50px; background: lime; }
</style>
<div class="container"><div class="child"></div></div>
```
Reference: red 200×100 container with lime 200×50 top half.

**Test 2**: `any-link-001.html` — `:any-link { color: blue }` applies to links.

---

## Implementation Priority Order for Swarm

All 10 features are independent and run in parallel:

| # | Feature | Complexity | Files Modified |
|---|---------|-----------|---------------|
| 1 | CSS Native Nesting | High | `stylesheet.go` |
| 2 | Dynamic viewport units | Low | `style.go` |
| 3 | ch/ex/lh/rlh units | Low | `style.go` |
| 4 | env() function | Low-Med | `style.go` |
| 5 | @media feature queries | Low | `stylesheet.go` |
| 6 | image-set() function | Medium | `style.go` |
| 7 | color() function | Medium | `style.go`, `colorspace.go` |
| 8 | CSS Grid Subgrid | High | `style.go`, `grid.go` |
| 9 | ::marker pseudo-element | Medium | `matcher.go`, `cascade.go`, `render.go` |
| 10 | Vendor prefix values + :any-link | Low | `style.go`, `matcher.go` |

---

## Agent Instructions

Each agent implementing one feature must:

1. **Read** the relevant source files to understand current structure
2. **Read** this plan file completely — implement ONLY your assigned feature number
3. **Create WPT test files** in `pkg/visualtest/testdata/wpt-css3/css-FEATURE/`:
   - Test HTML: `<link rel="match" href="name-ref.html">` pointing to reference
   - Reference HTML: same visual using explicit CSS (no new feature)
   - Create at least 2 tests per feature
4. **Implement** the feature following the approach described above
5. **Run your new tests**:
   ```bash
   /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ \
     -run "TestWPTCSS3Reftests/css-FEATURE" -count=1 -v 2>&1 | tail -20
   ```
6. **Run full regression** to verify nothing broke:
   ```bash
   /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ \
     -run "TestWPTCSS3Reftests|TestWPTReftests" -count=1 2>&1 | tail -5
   ```
7. **Fix any regressions** before reporting done. Target: 0 new failures.
8. **Leave changes uncommitted** — the merge script handles committing.

### Key Rules
- **MaxDifferentPercent = 0.1** (480px max at 800×600). Never higher.
- **FuzzyRadius = 0** always. Never use fuzzy matching.
- **Do not implement other features** — only your assigned number.
- **Go binary**: `/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go`
- **Render tool**: `go run ./cmd/l14open input.html output.png 800 600`

### Key Architecture Notes

- **`pkg/css/style.go`**: CSS property getters, shorthand expansion (`expandShorthand()`),
  color parsing (`parseColorToRGBA()`), length parsing (`ParseLengthFull()`).
- **`pkg/css/stylesheet.go`**: CSS parser. `ParseStylesheet()` entry point. `splitRules()`,
  `parseRule()`, `parseRules()`. `evaluateMediaCondition()` for @media.
- **`pkg/css/cascade.go`**: Cascade resolution, pseudo-element style computation.
- **`pkg/css/matcher.go`**: Selector matching, pseudo-class/element handling.
- **`pkg/css/colorspace.go`**: Color space conversion functions (oklch, lch, hwb, color-mix).
  Add new color() function converters here.
- **`pkg/layout/grid.go`**: Grid track sizing and item placement. Subgrid goes here.
- **`pkg/render/render.go`**: Rendering/painting. Marker rendering is here or in layout_block.go.
- **`pkg/layout/layout_block.go`**: Block layout, list item rendering.
