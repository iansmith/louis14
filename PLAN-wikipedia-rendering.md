# Plan: Wikipedia Main Page Rendering Improvements

## Goal
Make the Wikipedia Main Page (https://en.wikipedia.org/wiki/Main_Page) render
correctly in louis14. The Safari reference rendering shows a two-column layout
with colored section headers, proper typography, and a sidebar.

## Current State
- **CSS2: 99/99, CSS3: 265/265** — all reftests pass
- When rendered with `l14show`, the page loads and external CSS is fetched, but
  the layout is wrong (no two-column split, dark/missing backgrounds)
- Default viewport is 800px, but Wikipedia sets `<meta name="viewport" content="width=1120">`

## Architecture Overview (for agents unfamiliar with the codebase)

The rendering pipeline is:
1. **HTML parsing** (`pkg/html/parser.go`) → DOM tree + collected stylesheets
2. **Style cascade** (`pkg/css/cascade.go`) → computed styles per node
3. **Layout** (`pkg/layout/`) → box tree with positions/sizes
4. **Rendering** (`pkg/render/render.go`) → PNG output

Key files:
- `pkg/css/style.go` — CSS property parsing, `expandShorthand()`, property getters
- `pkg/css/stylesheet.go` — CSS rule parsing, `@media` evaluation
- `pkg/css/cascade.go` — style computation, `ApplyStylesToDocument()`
- `pkg/css/matcher.go` — CSS selector matching
- `pkg/layout/layout_block.go` — block layout
- `pkg/layout/layout_flex.go` — flexbox layout
- `pkg/layout/grid.go` — CSS Grid layout
- `pkg/render/render.go` — box rendering to image

---

## Task 1: Viewport Meta Tag Support

**Priority**: CRITICAL (unlocks Task 5)
**Difficulty**: Easy
**Files**: `pkg/html/parser.go`, `pkg/resource/renderer.go`

### Problem
Wikipedia sets `<meta name="viewport" content="width=1120">`. We ignore this and
use the default 800px viewport. At 800px, the critical `@media (min-width: 1120px)`
rules don't fire, so the CSS Grid outer layout never activates.

### What to implement
1. In `pkg/html/parser.go`, during parsing, when encountering
   `<meta name="viewport" content="...">`, extract and store the `width` value
   on the `Document` struct. The Document struct is in `pkg/html/node.go`.
   Add a field like `ViewportWidth int` to the Document struct.

2. Parse the content attribute: it's a comma-separated list of key=value pairs.
   Example: `width=1120` or `width=device-width, initial-scale=1`. For now,
   only extract the numeric `width` value. Ignore `device-width` and other
   non-numeric values.

3. In `pkg/resource/renderer.go`, in `RenderAutoHeight()` (line ~54), after
   parsing the HTML, check if `doc.ViewportWidth > 0` and if so, use that as
   the rendering width instead of the caller-provided width.

### Verification
Run `l14show` on Wikipedia — the content height should change because the wider
viewport enables different layout rules.

---

## Task 2: `grid-template` Shorthand Parsing

**Priority**: CRITICAL (needed for Wikipedia's outer page layout)
**Difficulty**: Medium
**Files**: `pkg/css/style.go`

### Problem
The Vector 2022 skin CSS uses the `grid-template` shorthand:
```css
.mw-page-container-inner {
  grid-template: min-content 1fr min-content / 12.25rem minmax(0,1fr);
  grid-template-areas: 'siteNotice siteNotice' 'columnStart pageContent' 'footer footer';
}
```

The `grid-template` shorthand is NOT expanded in `expandShorthand()` (around line
963). It falls through to the default case which stores the raw string as-is.
Then `GetGridTemplateColumns()` and `GetGridTemplateRows()` fail to parse it.

### What to implement
Add `case "grid-template":` to the `expandShorthand()` function in `pkg/css/style.go`.

The `grid-template` shorthand has this format:
```
grid-template: <row-track-list> / <column-track-list>
```

Split on ` / ` (space-slash-space) to separate rows from columns:
```go
case "grid-template":
    // grid-template: <row-tracks> / <col-tracks>
    // Also handles: grid-template: none
    if value == "none" {
        style.Set("grid-template-rows", "none")
        style.Set("grid-template-columns", "none")
        style.Set("grid-template-areas", "none")
    } else if slashIdx := strings.Index(value, " / "); slashIdx >= 0 {
        rowPart := strings.TrimSpace(value[:slashIdx])
        colPart := strings.TrimSpace(value[slashIdx+3:])
        style.Set("grid-template-rows", rowPart)
        style.Set("grid-template-columns", colPart)
    } else {
        // Single value — treat as rows
        style.Set("grid-template-rows", value)
    }
```

**Important**: The `grid-template` shorthand can also include area names inline
like `grid-template: "header header" auto "main sidebar" 1fr / 1fr 1fr`, but
Wikipedia doesn't use that form — it sets `grid-template-areas` separately.
So the simple slash-split is sufficient for now.

### Verification
After this change, when Wikipedia CSS is parsed, `GetGridTemplateColumns()` should
return `[minmax(0,1fr)]` tracks (after Task 3 is done) and `GetGridTemplateRows()`
should return `[min-content, 1fr, min-content]`.

---

## Task 3: `minmax()` in Grid Track Parsing

**Priority**: CRITICAL (needed for Wikipedia grid columns)
**Difficulty**: Medium
**Files**: `pkg/css/style.go`, `pkg/layout/grid.go`

### Problem
Wikipedia's grid uses `minmax(0, 1fr)` as a column track:
```css
grid-template-columns: 12.25rem minmax(0, 1fr);
```

The `parseGridTracks()` function in `pkg/css/style.go` (line 3388) only handles
`auto`, `fr`, and fixed lengths. It does NOT handle `minmax()`. The track
`minmax(0, 1fr)` is silently skipped, so the grid gets only 1 column track
instead of 2.

### What to implement

#### Step 1: Extend `GridTrack` struct
In `pkg/css/style.go` around line 3331, add `MinMax` fields:
```go
type GridTrack struct {
    Size   float64
    Auto   bool
    Fr     float64
    IsMinMax bool      // true if this is a minmax() track
    MinSize  float64   // minimum size (px) — 0 for min-content
    MaxFr    float64   // maximum as fr value (0 if not fr)
    MaxSize  float64   // maximum as fixed size (px)
    MaxAuto  bool      // true if max is "auto"
    MinContent bool    // true if value is "min-content"
    MaxContent bool    // true if value is "max-content"
}
```

#### Step 2: Parse `minmax()` in `parseGridTracks()`
The current `parseGridTracks()` does `strings.Fields(val)` which splits on
whitespace. This BREAKS `minmax(0, 1fr)` because the space after the comma
splits it into two tokens. You need to split respecting parentheses.

Replace the simple `strings.Fields` split with a paren-aware tokenizer:
```go
func splitGridTrackValues(val string) []string {
    var parts []string
    depth := 0
    start := 0
    for i := 0; i < len(val); i++ {
        switch val[i] {
        case '(':
            depth++
        case ')':
            depth--
        case ' ', '\t':
            if depth == 0 {
                part := strings.TrimSpace(val[start:i])
                if part != "" {
                    parts = append(parts, part)
                }
                start = i + 1
            }
        }
    }
    if part := strings.TrimSpace(val[start:]); part != "" {
        parts = append(parts, part)
    }
    return parts
}
```

Then in `parseGridTracks`, for each part, check if it starts with `minmax(`:
```go
if strings.HasPrefix(part, "minmax(") {
    // Extract contents: minmax(min, max)
    inner := part[7 : len(part)-1] // strip "minmax(" and ")"
    args := strings.SplitN(inner, ",", 2)
    min := strings.TrimSpace(args[0])
    max := strings.TrimSpace(args[1])

    track := GridTrack{IsMinMax: true}
    // Parse min
    if min == "0" || min == "0px" {
        track.MinSize = 0
    } else if min == "auto" {
        track.Auto = true
    } else if size, ok := ParseLength(min); ok {
        track.MinSize = size
    }
    // Parse max
    if strings.HasSuffix(max, "fr") {
        frStr := strings.TrimSuffix(max, "fr")
        if fr, err := strconv.ParseFloat(frStr, 64); err == nil {
            track.MaxFr = fr
        }
    } else if max == "auto" {
        track.MaxAuto = true
    } else if size, ok := ParseLength(max); ok {
        track.MaxSize = size
    }
    tracks = append(tracks, track)
}
```

Also handle `min-content` and `max-content` keywords as standalone track values:
```go
} else if part == "min-content" {
    tracks = append(tracks, GridTrack{MinContent: true})
} else if part == "max-content" {
    tracks = append(tracks, GridTrack{MaxContent: true})
}
```

#### Step 3: Handle `minmax()` in `resolveTrackSizes()` (grid.go line 378)
In `pkg/layout/grid.go`, the `resolveTrackSizes()` function needs to handle
`IsMinMax` tracks. A `minmax(0, 1fr)` track should:
- Have a minimum of 0px
- Distribute remaining space using fr units (like a regular fr track)

In the first pass (line 391-401), treat `minmax` tracks similarly:
```go
if t.IsMinMax {
    if t.MaxFr > 0 {
        // minmax(X, Nfr) — acts like fr track with a minimum
        sizes[i] = t.MinSize  // start at minimum
        usedSpace += sizes[i]
        totalFr += t.MaxFr
    } else {
        // minmax(X, Ypx) — use max as fixed size, clamp to min
        size := t.MaxSize
        if size < t.MinSize { size = t.MinSize }
        sizes[i] = size
        usedSpace += sizes[i]
    }
}
```

Also handle `MinContent`/`MaxContent` tracks: treat `min-content` like `auto`
(use the auto content size), treat `max-content` similarly.

### Verification
Parse `grid-template-columns: 12.25rem minmax(0,1fr)` → should produce 2 tracks:
track 0 = 196px fixed, track 1 = minmax(0, 1fr) which fills remaining space.

---

## Task 4: `calc()` in Media Query Conditions

**Priority**: HIGH
**Difficulty**: Easy
**Files**: `pkg/css/stylesheet.go`

### Problem
The Vector 2022 CSS uses `calc()` inside media queries:
```css
@media all and (max-width: calc(640px - 1px)) { ... }
```

The `parseMediaLength()` function in `pkg/css/stylesheet.go` (line 1204) only
handles plain `px` values and plain numbers. It does NOT handle `calc()`.
When it encounters `calc(640px - 1px)`, parsing fails, `unit` is empty,
and the media condition is skipped (assumes match via the `unit != "px"` check
at line 1186 which returns `true`).

Actually, looking more carefully at the code: if `unit != "px"`, it returns
`true` (assume match). So `calc()` in media queries would ALWAYS match, which
for `max-width: calc(640px - 1px)` at viewport 1120px means the mobile styles
would incorrectly apply. This is a real bug.

### What to implement
In `parseMediaLength()` (line 1205), add a check for `calc(`:
```go
func parseMediaLength(val string) (float64, string) {
    val = strings.TrimSpace(val)

    // Handle calc() expressions
    if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
        inner := val[5 : len(val)-1]
        // Use the existing calc evaluator
        result, ok := EvalCalcWithPercent(inner, 0, 0, 16) // no % context in media queries
        if ok {
            return result, "px"
        }
    }

    // ... existing px parsing code ...
```

Note: `EvalCalcWithPercent` is already implemented in `pkg/css/style.go`
(around line 362). It handles `px`, `em`, `rem` units and arithmetic operators.
Check if it's exported (capitalized). If not, you'll need to call it differently
or make it accessible from the stylesheet package. Since both files are in
`pkg/css`, it should be directly callable.

### Verification
`@media all and (max-width: calc(640px - 1px))` should evaluate to
`max-width: 639px`. At viewport 1120px, this should NOT match (1120 > 639).

---

## Task 5: `rem` Units in Media Queries

**Priority**: HIGH (complements Task 4)
**Difficulty**: Easy
**Files**: `pkg/css/stylesheet.go`

### Problem
Wikipedia's grid breakpoint is `@media screen and (min-width: 1120px)`. This
uses `px` so it works. But the Vector CSS also uses em-based conditions in other
places. More importantly, the `parseMediaLength()` function (line 1204) only
handles `px` — any `rem` or `em` value silently returns true.

### What to implement
Extend `parseMediaLength()` to handle `rem` and `em`:
```go
if strings.HasSuffix(val, "rem") {
    numStr := strings.TrimSuffix(val, "rem")
    if num, err := strconv.ParseFloat(numStr, 64); err == nil {
        return num * 16.0, "px"  // 1rem = 16px
    }
}
if strings.HasSuffix(val, "em") {
    numStr := strings.TrimSuffix(val, "em")
    if num, err := strconv.ParseFloat(numStr, 64); err == nil {
        return num * 16.0, "px"  // Assume 1em = 16px for media queries
    }
}
```

Add this BEFORE the existing `px` check. Note: for media queries, `em` and `rem`
both resolve relative to the initial font size (16px) per the CSS spec, NOT
relative to the element's font-size.

### Verification
A media query `@media (min-width: 18.75em)` should evaluate to `min-width: 300px`.

---

## Task 6: `max-width` with `margin: 0 auto` Centering in Block Layout

**Priority**: HIGH
**Difficulty**: Medium
**Files**: `pkg/layout/layout_block.go`

### Problem
Wikipedia's main container has:
```css
.mw-page-container {
    max-width: 99.75rem;   /* = 1596px */
    margin: 0 auto;
    box-sizing: border-box;
}
```

This means the container should be centered and capped at 1596px width. The
block layout code (`pkg/layout/layout_block.go` line 316) applies `max-width`
to `contentWidth`, but the `margin: 0 auto` centering logic (around line 290)
runs BEFORE the max-width constraint. So auto margins may not center correctly
when max-width is the constraining factor.

### What to check and fix
Read `pkg/layout/layout_block.go` from line 280-330 to understand the order of:
1. Width computation (explicit width or available width)
2. `box-sizing: border-box` adjustment
3. `max-width` application
4. `margin: auto` centering

The correct CSS order is:
1. Compute width (or use available width)
2. Apply box-sizing
3. Clamp by min-width / max-width
4. Then compute auto margins from remaining space

If `max-width` reduces the width, the auto margins should use the NEW (smaller)
width for centering. Check that the centering logic uses the post-max-width
width, not the pre-max-width width.

### Verification
A div with `width: 100%; max-width: 500px; margin: 0 auto` inside an 800px
container should be 500px wide and centered (150px from each edge).

---

## Task 7: `:root` Selector Matching

**Priority**: HIGH
**Difficulty**: Easy
**Files**: `pkg/css/matcher.go`

### Problem
Wikipedia's external CSS defines custom properties on `:root`:
```css
:root { --background-color-base: #fff; --color-base: #202122; ... }
```

The `:root` pseudo-class should match the `<html>` element (the document root).
Check if `pkg/css/matcher.go` handles `:root` in its pseudo-class matching.

### What to check
Search for "root" in `pkg/css/matcher.go`. If `:root` is not handled, add it:

In the pseudo-class matching section (look for the switch/if chain that handles
`:first-child`, `:last-child`, `:hover`, etc.), add:
```go
case "root":
    // :root matches the html element (document root)
    return node.Parent != nil && node.Parent.TagName == "document"
```

Or alternatively, check if the node's parent is the Document node.

### Why this matters
If `:root` doesn't match, custom properties like `--background-color-base` are
never set. Then `var(--background-color-base, #fff)` correctly falls back to
`#fff`. So this might NOT be the critical issue. However, if any `var()` usage
does NOT include a fallback (just `var(--name)`), it would resolve to empty
string, which could cause black/missing backgrounds.

Check the Wikipedia CSS for var() without fallbacks:
- The Vector skin CSS ALWAYS includes fallbacks: `var(--color-base, #202122)`
- So `:root` support helps correctness but may not visually change the rendering

### Verification
After implementing, custom properties set on `:root` should be available to all
descendant elements via `var()` resolution.

---

## Task 8: Multi-value `class` Attribute Selector Matching

**Priority**: MEDIUM
**Difficulty**: Easy (might already work)
**Files**: `pkg/css/matcher.go`

### Problem
Wikipedia's `<html>` element has a massive class attribute with ~30 classes:
```html
<html class="client-nojs vector-feature-language-in-header-enabled
  vector-feature-page-tools-pinned-disabled vector-feature-toc-pinned-clientpref-1
  vector-feature-main-menu-pinned-disabled vector-feature-limited-width-clientpref-1
  ...">
```

CSS selectors chain multiple classes:
```css
.vector-feature-main-menu-pinned-disabled.vector-toc-not-available .mw-page-container-inner
```

The existing matcher (line 87-106) checks that ALL required classes are present
in the node's class attribute. This SHOULD work correctly — it splits on spaces
and checks each required class. But verify with a test that handles 30+ classes
and 3+ chained class selectors.

### Potential issue
The selector parser needs to correctly extract compound class selectors like
`.class1.class2.class3` into a `SelectorPart` with 3 entries in `Classes`.
Check `pkg/css/stylesheet.go` selector parsing — the function that builds
`SelectorPart` from a raw selector string.

### What to check
Search for how selectors are parsed in `pkg/css/stylesheet.go`. Look for the
function that handles `.class1.class2.class3` parsing. Verify it creates a
`SelectorPart` with `Classes: ["class1", "class2", "class3"]`.

---

## Task 9: `grid-area` as Named Area Reference

**Priority**: MEDIUM
**Difficulty**: Easy (might already work)
**Files**: `pkg/css/style.go`, `pkg/layout/grid.go`

### Problem
Wikipedia uses:
```css
.mw-content-container { grid-area: pageContent; }
.vector-column-start  { grid-area: columnStart; }
```

With `grid-template-areas`:
```css
grid-template-areas: 'siteNotice siteNotice' 'columnStart pageContent' 'footer footer';
```

### Current state
`GetGridArea()` in `style.go` (line 3520) returns the string value. The grid
layout code (grid.go line 118-135) checks if this string matches a template
area name in `templateAreas` map. This looks correct.

### What to verify
1. That `GetGridTemplateAreas()` (style.go line 3456) correctly parses the
   areas into `GridAreaInfo` structs with correct row/col spans
2. That the grid item placement using `gridAreaName` (grid.go line 123-128)
   correctly maps to the right cells
3. That the `grid-template-areas` property is correctly set after the
   `grid-template` shorthand expansion (Task 2)

If `grid-template-areas` is set as a SEPARATE property (not part of the
`grid-template` shorthand), this should already work. Wikipedia sets it
separately, so this is likely fine. Just verify.

---

## Task 10: `border-collapse` for Non-Table `display:block` Elements

**Priority**: LOW
**Difficulty**: Easy
**Files**: `pkg/css/cascade.go` (UA defaults)

### Problem
Wikipedia's `mp-box` class uses:
```css
.mp-box { border: 1px solid #aaa; padding: 0 0.5em 0.5em; margin-top: 4px; }
```

These are standard box properties that should already work. However, Wikipedia
also has table elements within the page that use `border-collapse: collapse`.
This is already supported. No implementation needed — this task is a verification.

### What to verify
Render a simple page with the Wikipedia `mp-box` styling and confirm borders
and padding render correctly.

---

## Task 11: Multi-Value `background` Shorthand Robustness

**Priority**: LOW
**Difficulty**: Medium
**Files**: `pkg/css/style.go`

### Problem
Some Vector CSS rules use complex background shorthands:
```css
background: linear-gradient(to right, transparent 0, var(--bg-color,#f8f9fa) 3em)
```

After var() resolution, this becomes:
```css
background: linear-gradient(to right, transparent 0, #f8f9fa 3em)
```

The background shorthand parsing in `expandBackgroundProperty()` needs to
handle gradient functions correctly, not confusing them with color values.

### What to check
Read `expandBackgroundProperty()` in `style.go` and verify:
1. It detects `linear-gradient(` and doesn't try to parse it as a color
2. It correctly sets `background-image` to the gradient value
3. It doesn't choke on the `var()` inside the gradient (var should resolve first)

This might already work. Test with a simple HTML page that uses the pattern above.

---

## Implementation Order

**Phase 1 (Critical — enables basic layout):**
1. Task 1: Viewport meta tag (easy, unlocks correct media query evaluation)
2. Task 2: `grid-template` shorthand (medium, needed for outer layout)
3. Task 3: `minmax()` in grid tracks (medium, needed for flexible columns)

**Phase 2 (High — fixes visual issues):**
4. Task 4: `calc()` in media queries (easy)
5. Task 5: `rem/em` in media queries (easy)
6. Task 6: `max-width` + auto margin centering (medium)
7. Task 7: `:root` selector (easy)

**Phase 3 (Verification — may already work):**
8. Task 8: Multi-class selectors (verify)
9. Task 9: grid-area references (verify)
10. Task 10: Box styling (verify)
11. Task 11: Background shorthand (verify)

---

## How to Test

After implementing, run:
```bash
go run ./cmd/l14show -w 1120 -o output.png "https://en.wikipedia.org/wiki/Main_Page"
```

Key things to check in the output:
1. Two-column layout (Featured Article left, In the News right)
2. Colored section headers (green-ish left, blue-ish right)
3. Content centered on page (not full-width)
4. White background (not black/dark)
5. Proper text color (#202122 dark gray, not black)

Also verify no reftests regressed:
```bash
go clean -testcache
go test ./pkg/visualtest/ -run "TestWPTReftests$" -v 2>&1 | grep Summary
go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests" -v 2>&1 | grep Summary
```

Expected: CSS2 99/99, CSS3 265/265.

---

## Agent Instructions

Each task can be worked on independently. Tasks 1-3 should be done first as
they are prerequisites for the page to render with correct structure. Tasks
4-7 can be parallelized. Tasks 8-11 are verification tasks.

**CRITICAL RULES:**
- Never use fuzzy matching (FuzzyRadius > 0) in any visual tests
- Go binary is at `/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go`
- Build with `go build ./...` before testing
- Run `go clean -testcache` before test runs to avoid stale results
- Do NOT modify existing passing tests — only add new functionality
