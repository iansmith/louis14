# LOU-138 — Migrate remaining url()-bearing CSS properties to URLData, then drop `<style>`-block pre-baking

**Linear:** [LOU-138](https://linear.app/mazarin/issue/LOU-138/migrate-remaining-url-bearing-css-properties-to-urldata-then-drop)
**Predecessor:** LOU-137 v2 (commit `3a492366`)

## Blink vetting log

Vetted against Chromium `main` @ `d4ecdfed88f962439247c2ad36b8fe47805b1520` on 2026-05-19.
All Blink references below cite `file:line` against that SHA. Re-vet before resuming
if more than ~3 months have elapsed (per `feedback_blink_citation_discipline`).

---

## Goal

Replace the post-LOU-137 mix of "filter resolves at parse time, everything else
relies on renderer-side fetcher join" with the **Blink invariant**: every CSS
`url()` token is resolved at parse time, in one place, via a base URL carried on a
parser context. Then strip `pkg/layout/layout_algorithm.go::ResolveRelativeURLsInHTML`
— the HTML preprocessor that pre-bakes `<style>`-block url()s — which becomes a
no-op the moment parse-time resolution is universal.

We will **mirror Blink's chokepoint architecture** rather than the ticket's
"Option 1" (eagerly rewrite url() text inside Properties strings, no type churn).
The Blink invariant is the foundationally-correct end state; Option 1 would be a
half-step that we'd undo when getComputedStyle/CSSOM arrives. Per CLAUDE.md
principle 1, no half-steps.

---

## Blink reference architecture (post-survey)

Pinned SHA: `d4ecdfed88f962439247c2ad36b8fe47805b1520`. The five chokepoint sites:

### 1. `CSSParserContext` — carries the base URL

`third_party/blink/renderer/core/css/parser/css_parser_context.h:153`

```cpp
KURL base_url_;
const KURL& BaseURL() const { return base_url_; }  // .h:82
```

Constructed in three families (`.cc`):
- **From a Document** (`.cc:85`): defaults to `document.BaseURL()`. Used for inline
  `style=""` attrs and `<style>` blocks.
- **With explicit override** (`.h:56-63`): used when the stylesheet's URL differs
  from the document's (external `<link rel=stylesheet>`).
- **From parent + override** (`.h:46-51`): used for `@import`, swapping in the
  imported sheet's response URL as the new base.

Immutable for the duration of a parse; threaded into every `ConsumeXxx` call.

### 2. `CompleteNonEmptyURL` — the parse-time resolver

`third_party/blink/renderer/core/css/parser/css_parser_context.cc:202,212`

```cpp
KURL CSSParserContext::CompleteURL(const String& url) const {
  if (url.IsNull()) return KURL();
  if (!Charset().IsValid()) return KURL(BaseURL(), url);
  return KURL(BaseURL(), url, Charset());
}
KURL CSSParserContext::CompleteNonEmptyURL(const String& url) const {
  if (url.empty() && !url.IsNull()) return KURL(g_empty_string);
  return CompleteURL(url);
}
```

This is the only place a relative URL string becomes absolute during CSS parse.

### 3. `CollectUrlData` — the single chokepoint that builds CSSUrlData

`third_party/blink/renderer/core/css/properties/css_parsing_utils.cc:1777`

```cpp
const CSSUrlData* CollectUrlData(const StringView& url,
                                 const CSSUrlRequestModifiers& modifiers,
                                 const CSSParserContext& context) {
  AtomicString url_string = url.ToAtomicString();
  return MakeGarbageCollected<CSSUrlData>(
      url_string, context.CompleteNonEmptyURL(url_string),
      context.GetReferrer(), context.IsOriginClean(),
      context.IsAdRelated(), modifiers);
}
```

Every `url()` token in every property funnels through this helper. Per-property
parser variants (`ConsumeUrl` `.cc:1972`, `ConsumeImage` `.cc:3885`,
`CreateCSSImageValueWithReferrer` `.cc:3774`) all call `CollectUrlData`.

### 4. `CSSUrlData` — storage primitive

`third_party/blink/renderer/core/css/css_url_data.h:108-131`

```cpp
const AtomicString& UnresolvedUrl() const { return relative_url_; }
const AtomicString& ResolvedUrl() const { return absolute_url_; }
private:
  AtomicString relative_url_;
  mutable AtomicString absolute_url_;
```

Both forms always present. `mutable` only to support the spec-edge
`ReResolveUrl()` (`.cc:152`) — normal flow does not re-resolve.

### 5. Typed CSSValue wrappers around CSSUrlData

- **`CSSURIValue`** (`core/css/css_uri_value.h:23`) — for **non-image** refs:
  `filter url(#…)`, mask reference, `shape-outside`, `offset-path`, SVG paint server,
  `@font-face src`. Constructor (`.cc:15`) takes `(const CSSUrlData&)`.
- **`CSSImageValue`** (`core/css/css_image_value.h:37`) — for **image-bearing**
  properties: `background-image`, `mask-image` (image form), `list-style-image`,
  `border-image-source`, `cursor` (each fallback). Constructor (`.cc:46`) takes
  `(const CSSUrlData&, StyleImage* = nullptr)`.

The two wrappers are intentionally separate: `CSSImageValue` holds a lazy
`StyleImage` (`cached_image_`) for the fetch lifecycle; `CSSURIValue` holds an SVG
resource ref. We will mirror that separation in louis14.

### Property → consumer map (for migration order in Phase 7)

| Property                | Blink parser site (`css_parsing_utils.cc`)                  | Blink wrapper   |
|-------------------------|-------------------------------------------------------------|-----------------|
| `background-image`      | `ConsumeImage` :3885 via `ConsumeImageOrNone` :3443         | `CSSImageValue` |
| `mask-image` (image)    | `ConsumeImageOrNone` :~5344                                  | `CSSImageValue` |
| `mask-image` (ref)      | `ConsumeUrl` :1972                                           | `CSSURIValue`   |
| `border-image-source`   | `ConsumeImageOrNone` :~5608                                  | `CSSImageValue` |
| `list-style-image`      | `ConsumeImageOrNone`                                         | `CSSImageValue` |
| `cursor` (each fallback)| `ConsumeImage` per list element                              | `CSSImageValue` |
| `filter url(...)`       | `ConsumeUrl` :4019                                           | `CSSURIValue`   |
| `shape-outside`         | `ConsumeUrl` :~8987                                          | `CSSURIValue`   |
| `@font-face src`        | `at_rule_descriptor_parser.cc::ConsumeFontFaceSrcURI` :~137  | `CSSURIValue`   |

---

## Current louis14 state (post-LOU-137 v2)

**Existing primitives (already Blink-aligned by name):**
- `URLData` at [pkg/css/style.go:45](pkg/css/style.go:45) — mirrors `CSSUrlData`, fields `Relative` and `Absolute`.
- `ResolveURL` at [pkg/css/style.go:21](pkg/css/style.go:21) — primitive resolver; will be wrapped by `CSSParserContext.CompleteURL`.
- `isAbsoluteCSSURL` at [pkg/css/style.go:30](pkg/css/style.go:30) — short-circuit for already-absolute forms.
- `Style.BaseDir` and `Document.BaseDir` — propagated from doc to every Style via [pkg/css/cascade.go:529-535](pkg/css/cascade.go:529).

**Filter url() (already migrated):**
- `FilterFunction.URL` is `URLData`, populated by `parseFilterList(val, baseDir)` at [pkg/css/style.go:8592](pkg/css/style.go:8592).
- Getter `Style.GetFilter()` at [pkg/css/style.go:8582](pkg/css/style.go:8582) reads `s.BaseDir` and runs `ResolveURL` per token.
- Consumer `pkg/render/filter_effect_builder.go:60,69` reads `.Absolute`.

**The latent double-resolution path (Problem 1):**
- `ResolveRelativeURLsInHTML` at [pkg/layout/layout_algorithm.go:93-106](pkg/layout/layout_algorithm.go:93) still rewrites `<style>{ filter: url(foo.svg) }` → `<style>{ filter: url(support/foo.svg) }` against the iframe's BaseDir before `html.Parse`.
- Called from [pkg/layout/engine.go:201-225](pkg/layout/engine.go:201) (`layoutNestedDocument`).
- When cascade later runs filter through `parseFilterList("support/foo.svg", "support")`, `ResolveURL` does `path.Join("support", "support/foo.svg")` → `"support/support/foo.svg"`. **Double-resolved.**
- No test currently exercises `<style>{filter: url(...)}` inside an iframe, so the bug is latent.

**Other url()-bearing properties (still raw strings, Problem 2):**
- `FillLayer.Image` (background-image) at [pkg/css/style.go:7374-7378](pkg/css/style.go:7374); consumer [pkg/render/paint_layer.go:618](pkg/render/paint_layer.go:618) via `GetBackgroundImage()` at style.go:7237.
- `Style.GetMaskImage()` at [pkg/css/style.go:8975-8987](pkg/css/style.go:8975); consumer paint_layer.go:618.
- `Style.GetBorderImageSource()` at [pkg/css/style.go:9021-9026](pkg/css/style.go:9021).
- `list-style-image` Phase-5 parser at [pkg/css/style.go:1960](pkg/css/style.go:1960).
- `FontFaceRule.Src` at [pkg/css/stylesheet.go:96-102](pkg/css/stylesheet.go:96).
- `cursor`, `shape-outside` — `url()` form not yet implemented; out of scope.

**Parser entry points that will need a ParserContext:**
- `ParseStylesheet(css string)` at [pkg/css/stylesheet.go:360](pkg/css/stylesheet.go:360). Call sites:
  - [pkg/css/cascade.go:545](pkg/css/cascade.go:545) (`<style>` element body).
  - [pkg/resource/renderer.go:222,356](pkg/resource/renderer.go:222) (external `<link>` stylesheet).
  - [pkg/visualtest/helpers.go:118,155](pkg/visualtest/helpers.go:118) (test fixtures).
- `ParseInlineStyle(styleAttr string)` at [pkg/css/style.go:1801](pkg/css/style.go:1801). Call site:
  - [pkg/css/cascade.go:496](pkg/css/cascade.go:496) (inline `style=""` attribute).

---

## Plan

Eight phases. Each ships independently. Tests gate each phase.

Phases 5–6 fold in two latent base-URL bugs that surfaced during the open-questions
investigation (external `<link>` stylesheets and `@import`). Both are trivial to
fix once Phase 1's `ParserContext` exists — "construct a different ParserContext
at the right boundary" rather than architectural overhauls.

Phase 4 folds in a third issue (absolute-URL composition) that affects every
BaseDir-derivation site in the codebase: `path.Join`/`path.Dir` mangle
scheme-prefixed URLs (`https://...`, `file://...`), so any document loaded from
a real URL — or any `<link href="https://...">`, `<iframe src="https://...">`
— produces broken BaseDir math. Doing this BEFORE Phases 5–6 means those phases
inherit a correct primitive instead of papering over the bug.

### Phase 1 — Introduce `ParserContext` (foundational type)

**Goal:** add the Blink chokepoint carrier without behavior change.

**File placement:** mirror Blink's `core/css/parser/css_parser_context.h` →
`pkg/css/parser_context.go` (per `feedback_blink_file_placement`).

**New type:**

```go
// Mirrors Blink's CSSParserContext (core/css/parser/css_parser_context.h:153
// @ d4ecdfed8). Carries the base URL during a parse; immutable for the parse's
// lifetime.
type ParserContext struct {
    BaseDir string // mirrors CSSParserContext::base_url_
    // Future fields (intentionally deferred): Charset, Referrer, IsOriginClean,
    // IsAdRelated, Mode. Add only when a test demands them.
}

func NewParserContext(baseDir string) *ParserContext {
    return &ParserContext{BaseDir: baseDir}
}

// NewParserContextFromDocument mirrors the Blink ctor
// `CSSParserContext(const Document&)` at css_parser_context.cc:85.
func NewParserContextFromDocument(doc *html.Document) *ParserContext {
    return &ParserContext{BaseDir: doc.BaseDir}
}

// CompleteURL mirrors CSSParserContext::CompleteURL at css_parser_context.cc:202.
func (c *ParserContext) CompleteURL(raw string) string {
    return ResolveURL(raw, c.BaseDir)
}

// CollectUrlData mirrors core/css/properties/css_parsing_utils.cc::CollectUrlData
// at line 1777. This is the single chokepoint every url() token must funnel
// through during a parse.
func (c *ParserContext) CollectUrlData(raw string) URLData {
    return URLData{Relative: raw, Absolute: c.CompleteURL(raw)}
}
```

**Tests:** unit test for `CompleteURL` covering absolute URL passthrough, empty
BaseDir passthrough, `data:` URL passthrough, and `path.Join` for relative. No
WPT involvement; this phase is type infrastructure.

**Gate:** `go build ./...` clean. No render-test changes (none expected).

**Commit:** "feat(css): introduce ParserContext + CollectUrlData chokepoint (LOU-138 phase 1)"

---

### Phase 2 — Thread `ParserContext` through `ParseStylesheet` and `ParseInlineStyle`

**Goal:** every `url()` token consumed inside a CSS declaration value is rewritten
to absolute form via `ParserContext.CompleteURL` before being stored in
`Style.Properties` / `Stylesheet.Rules`. After this phase, the Properties map holds
absolute URLs uniformly, and Style.BaseDir-based resolution at getter time becomes
redundant for the rewritten-properties path.

**Signature changes:**

```go
// pkg/css/stylesheet.go:360
func ParseStylesheet(css string, ctx *ParserContext) (*Stylesheet, error) { ... }

// pkg/css/style.go:1801
func ParseInlineStyle(styleAttr string, ctx *ParserContext) *Style { ... }
```

**Caller updates** (Phase 2 uses `doc.BaseDir` for every sheet — per-sheet base
is fixed in Phase 5):
- [pkg/css/cascade.go:545](pkg/css/cascade.go:545) — pass `NewParserContextFromDocument(doc)`.
- [pkg/css/cascade.go:496](pkg/css/cascade.go:496) — pass same (inline `style=""`).
- [pkg/resource/renderer.go:222,356](pkg/resource/renderer.go:222) — pass same
  (`@font-face` and `@counter-style` collection iterators). Phase 5 changes these
  to use per-sheet `ParserContext`.
- [pkg/visualtest/helpers.go:118,155](pkg/visualtest/helpers.go:118) — test fixtures;
  pass `NewParserContext("")` or document BaseDir as appropriate.

**Resolution mechanic (the actual work):**
Inside `ParseStylesheet`'s declaration parser and `ParseInlineStyle`, when a
property value contains one or more `url(...)` tokens, rewrite each token's inner
string to its absolute form via `ctx.CompleteURL(inner)`. Reuse the existing
regex/scanner pattern from `ResolveRelativeURLsInCSS` at
[pkg/layout/layout_algorithm.go:66](pkg/layout/layout_algorithm.go:66) — that
function is essentially the same rewrite, just done at the wrong layer (HTML
preprocessor). Move its logic into a private helper in `pkg/css/parser_context.go`
called from the declaration parser. The HTML-layer copy will be deleted in Phase 3.

**Tests added in this phase:**
- Unit test: a stylesheet `{ filter: url(foo.svg) }` parsed with `ctx.BaseDir="sub"`
  yields a declaration value of `url(sub/foo.svg)`.
- Unit test: `style=""` attribute with `filter: url(foo.svg)` parsed via same
  context yields `url(sub/foo.svg)`.
- Unit test: `data:` URLs and absolute URLs pass through unchanged.
- Unit test: multiple url()s in one declaration all rewritten (e.g.
  `mask-image: url(a.png), url(b.png)`).

**Filter getter idempotency:**
After Phase 2, `Style.Properties["filter"]` already holds absolute URLs. The
existing `Style.GetFilter() → parseFilterList(val, s.BaseDir)` path would
double-resolve. Two options:
- (a) Change `parseFilterList` to ignore `s.BaseDir` (pass empty). The getter
  becomes a no-op on base resolution.
- (b) Make `ResolveURL` idempotent on its own output. Today it's not (`path.Join`
  applied twice doubles up). Adding an idempotency check ("if the value already
  starts with `BaseDir/`...") is fragile and matches no Blink primitive.

**Pick (a).** Mirrors Blink: by the time a CSSValue reaches a consumer, its URLs
are already absolute. Update `GetFilter()` to call `parseFilterList(val, "")` and
add a doc comment pointing at this plan.

**Gate:** all existing LOU-13x gate tests still pass at 0% diff (no behavior
change for the inline-style filter path that LOU-137 v2 covered). The two
existing tests that exercise filter url() resolution remain green:
- `svg-relative-urls-001.html` (inline style across adoptNode).
- `svg-relative-urls-002.html` (external stylesheet + relative filter URL).

**Commit:** "feat(css): thread ParserContext through ParseStylesheet + ParseInlineStyle (LOU-138 phase 2)"

---

### Phase 3 — New gate test for `<style>{filter: url(...)}` in iframe; drop `ResolveRelativeURLsInHTML`

**Goal:** prove Phase 2 fixed the latent double-resolution bug, then delete the
HTML preprocessor that was masking it.

**Step 3a — Find or write the failing test.**

The Blink web-tests survey did not turn up an iframe-scoped `<style>`-block filter
url() test. We need one. Two options:

- **Option A (preferred): find existing WPT test that hits this shape.** Search
  `pkg/visualtest/testdata/wpt-css3/filter-effects/` and `wpt-css/` for any test
  with a `<style>` block inside an iframe-loaded HTML and a filter url() inside it.
  If one exists already and is currently passing only because of the HTML
  preprocessor pre-bake, that's the gate.

- **Option B: author a minimal WPT-style test under
  `pkg/visualtest/testdata/local/lou-138-style-block-filter-iframe.html`.** Two
  files: outer HTML with `<iframe src="support/inner.html">`, and `support/inner.html`
  containing `<style>.x { filter: url(hueRotate.svg#MyFilter); }</style>
  <div class="x">…</div>` plus a `support/hueRotate.svg`. The reference is a
  static-color div. **Before** Phase 2, this test would render incorrectly because
  the HTML preprocessor rewrote the url to `support/hueRotate.svg`, then the
  filter getter re-prepended `support/`, yielding `support/support/hueRotate.svg`
  and a missing filter (broken render). **After** Phase 2, it renders correctly.

Run the new test against current HEAD (before Phase 2 lands) to confirm it fails.
If it doesn't fail at HEAD, **the gate is wrong** — the bug story is incomplete
and we need to understand why before proceeding (per `feedback_predictive_humility`:
identical diff-shapes do not prove shared cause).

**Step 3b — Delete the pre-baking.**

Remove the `<style>`-block branch from `ResolveRelativeURLsInHTML` at
[pkg/layout/layout_algorithm.go:93-106](pkg/layout/layout_algorithm.go:93). The
function body becomes `return htmlContent`. Then remove the call from
[pkg/layout/engine.go:212](pkg/layout/engine.go:212) and delete the function
entirely. Also delete `ResolveRelativeURLsInCSS` at line 66 unless something else
still calls it (grep first).

**Step 3c — Verify.**

- The new Phase-3a test PASSES.
- All five LOU-13x gate tests (filter set) still PASS at 0% diff.
- A targeted sweep of iframe-using tests in the test suite does not regress.
  Specifically: any test under `wpt-css3/filter-effects/` and `wpt-html/iframes/`
  that exercises an iframe should be re-run.

**If iframe-scoped `background-image`/`mask-image`/etc. tests regress here**,
that's the expected Problem-2 fallout. Don't fix in this phase. Capture each
regression, hand them to Phase 6 as the failing-test driver for each property.

**Commit:** "fix(css+layout): drop <style>-block URL pre-baking (LOU-138 phase 3)"

---

### Phase 4 — URL-aware composition primitives (replace `path.Join`/`path.Dir` for URL math)

**Goal:** make the URL-composition primitives correctly handle absolute URLs
(scheme-prefixed: `https://`, `http://`, `file://`, plus scheme-relative `//foo`
and root-relative `/foo`). Today everything is `path.Join`/`path.Dir` which
mangles absolute URLs by collapsing `//` to `/`. This affects all subsequent
phases that derive a BaseDir from a URL or compose a URL against a BaseDir.

**Blink reference (pinned SHA `d4ecdfed8`):**
- `third_party/blink/renderer/platform/weborigin/kurl.{h,cc}` — Blink's URL
  type. The constructor `KURL(const KURL& base, const String& relative)`
  (`kurl.cc::Init`) performs RFC 3986 URL resolution: absolute refs returned
  as-is, scheme-relative inherit scheme, root-relative inherit
  scheme+authority, path-relative resolved against base directory.
- `core/css/parser/css_parser_context.cc:202 CompleteURL` invokes
  `KURL(BaseURL(), url)` — the same RFC 3986 resolver. The louis14 mirror
  must produce the same outputs.

**Current breakage** (all uses of `path.Join`/`path.Dir` for URL math):
- [pkg/css/style.go:25 ResolveURL](pkg/css/style.go:25) — `path.Join(baseDir, raw)`.
  Today short-circuits on absolute `raw` via `isAbsoluteCSSURL`
  ([style.go:30](pkg/css/style.go:30)), but breaks when `baseDir` is absolute
  and `raw` is relative: `path.Join("https://example.com/styles", "foo.svg")`
  yields `"https:/example.com/styles/foo.svg"` (single slash — mangled).
- [pkg/layout/engine.go:208](pkg/layout/engine.go:208) —
  `nestedBaseDir := path.Join(ctx.BaseDir, path.Dir(nestedDocURI))`. Both
  ops mangle absolute URLs.
- [pkg/layout/layout_algorithm.go:210,218,228,236](pkg/layout/layout_algorithm.go:210)
  — fetcher composition uses `path.Join`/`path.Dir` for nested-doc URL math.
- **Phase 5's `<link>` href composition** would use the same broken pattern if
  Phase 4 didn't land first. Same for any future site needing
  "what's the directory part of this URL."

**New primitives** (extend `pkg/css/parser_context.go` from Phase 1):

```go
import "net/url"

// CompleteURL resolves a relative URL reference against a base URL. Mirrors
// Blink's KURL(BaseURL(), url) RFC 3986 resolution (kurl.cc::Init @
// d4ecdfed8). Handles: absolute refs (https://..., file://...), scheme-
// relative (//foo), root-relative (/foo), path-relative (./foo, foo, ../foo)
// against either absolute or relative base URLs.
//
// Replaces path.Join-based URL math (which mangles scheme-prefixed URLs by
// collapsing // to /).
func CompleteURL(raw, base string) string {
    if raw == "" { return raw }
    // Absolute ref always wins; matches isAbsoluteCSSURL short-circuit.
    if isAbsoluteURL(raw) { return raw }
    if base == "" || base == "." { return raw }
    baseURL, errB := url.Parse(base)
    refURL, errR := url.Parse(raw)
    if errB != nil || errR != nil {
        // Non-parseable input: fall back to path.Join for the relative-path
        // case. This preserves today's behavior for filesystem-loaded WPT
        // tests (BaseDir = "support", raw = "foo.svg").
        return path.Join(base, raw)
    }
    return baseURL.ResolveReference(refURL).String()
}

// URLDir returns the directory portion of a URL — everything up to and
// including the last '/'. URL-aware: absolute URLs survive scheme intact
// (path.Dir collapses https:// to https:/, mangling them).
//
//   URLDir("support/main.css")                = "support"
//   URLDir("https://cdn.example.com/x/y.css") = "https://cdn.example.com/x/"
//   URLDir("/styles/main.css")                = "/styles/"
//   URLDir("")                                = ""
func URLDir(uri string) string {
    if uri == "" { return "" }
    if !isAbsoluteURL(uri) {
        // Relative path: preserve today's path.Dir semantics (returns "" or
        // "support" with no trailing slash). Subsequent CompleteURL handles
        // the missing slash correctly via ResolveReference path math.
        return path.Dir(uri)
    }
    u, err := url.Parse(uri)
    if err != nil { return path.Dir(uri) }
    if i := strings.LastIndex(u.Path, "/"); i >= 0 {
        u.Path = u.Path[:i+1]
    }
    u.RawQuery, u.Fragment = "", ""
    return u.String()
}

// isAbsoluteURL is the louis14-internal version of isAbsoluteCSSURL (which is
// CSS-specific). Lives in parser_context.go since URLDir/CompleteURL need it.
func isAbsoluteURL(s string) bool {
    return strings.HasPrefix(s, "/") ||
        strings.HasPrefix(s, "data:") ||
        strings.HasPrefix(s, "http://") ||
        strings.HasPrefix(s, "https://") ||
        strings.HasPrefix(s, "file://") ||
        strings.HasPrefix(s, "//")
}
```

**Sites to migrate to the new primitives:**

1. `pkg/css/style.go:25 ResolveURL` — replace body with `CompleteURL(raw, baseDir)`.
   Keep the function name as a thin wrapper for source-stability; eventually
   call sites migrate to `ctx.CompleteURL` directly via Phase 7's typed wrappers.
2. `pkg/layout/engine.go:208 layoutNestedDocument` — replace with
   `nestedBaseDir := css.URLDir(css.CompleteURL(nestedDocURI, ctx.BaseDir))`.
   The composed form computes the iframe document's absolute URL, then takes
   its directory for the iframe's BaseDir.
3. `pkg/layout/layout_algorithm.go:210,218,228,236` — same pattern: nested-doc
   fetcher uses `css.URLDir(css.CompleteURL(...))` instead of
   `path.Join(base, ...)`.
4. **Phase 5's `<link>` href composition** uses `css.URLDir(css.CompleteURL(href, doc.BaseDir))`
   from the start (replacing what would have been `path.Dir(path.Join(...))`).

**Tests added in this phase** (unit tests for the primitives + integration tests
for the migrated sites):

Unit tests for `CompleteURL` and `URLDir` covering:
- Relative path + empty base: passthrough.
- Relative path + relative base (`"support"`, `"foo.svg"` → `"support/foo.svg"`):
  preserves today's behavior for WPT filesystem fixtures.
- Relative path + absolute base
  (`"https://example.com/styles/"`, `"foo.svg"` → `"https://example.com/styles/foo.svg"`):
  the case that broke under `path.Join`.
- Absolute ref + any base: passthrough.
- Scheme-relative ref + absolute base: scheme inheritance.
- Root-relative ref + absolute base: scheme+authority inheritance.
- `../foo` against absolute base: parent-directory navigation preserves authority.
- `URLDir("https://example.com/styles/main.css")` → `"https://example.com/styles/"`.
- `URLDir("https://example.com/main.css")` → `"https://example.com/"`.
- `URLDir("support/main.css")` → `"support"` (relative — match today's `path.Dir`).
- `URLDir("")` → `""`.

Integration test: a test where the document is loaded with an absolute base URL
(simulated via the fetcher) — e.g. a `<link href="https://example.com/styles/main.css">`
that contains `filter: url(hueRotate.svg)`. After Phase 4, the resolved url is
`https://example.com/styles/hueRotate.svg`. Today (and post-Phase 5 without
Phase 4) it would be `https:/example.com/styles/hueRotate.svg` — mangled.

If the test fetcher infrastructure can't yet simulate absolute URLs, the unit
tests are the minimum gate; flag the integration test as a follow-up in
findings.md.

**Gate:**
- The unit tests above PASS.
- All LOU-13x gate tests + Phase-3 gate test still PASS at 0% diff (filesystem
  fixtures use relative URLs; behavior is identical for that path).
- No regression in `pkg/layout/svg/` tests (the SVG document fetcher at
  [svg_document_fetcher.go:152](pkg/layout/svg/svg_document_fetcher.go:152)
  uses `filepath.Dir` which is filesystem-specific and intentionally out of
  scope — that's a filesystem-Origin computation, not URL composition).

**Commit:** "feat(css): URL-aware composition primitives — CompleteURL + URLDir (LOU-138 phase 4)"

---

### Phase 5 — External stylesheet per-sheet base URL

**Goal:** make `<link rel=stylesheet href="...">` carry its own response URL as
the base for `url()` resolution, rather than implicitly using `doc.BaseDir`.

**Blink reference (pinned SHA `d4ecdfed8`):**
- `core/css/style_sheet_contents.h` — Blink's per-sheet typed container holds a
  `Member<CSSParserContext> parser_context_;` set at parse time.
- `core/css/css_style_sheet.cc:106-126` — three CSSParserContext construction
  sites; external sheet path uses **response URL** as base.
- `core/css/style_rule_import.cc:77-82` (constructor for imported sheets — also
  uses response URL; reused as the prototype for our Phase 5).
- `core/loader/resource/css_style_sheet_resource.h` — the resource handle that
  carries the response URL into parse.

**Current louis14 state to change:**
- [pkg/html/dom.go:31](pkg/html/dom.go:31) `Document.Stylesheets []string` —
  flat raw-text slice; source URL discarded.
- [pkg/html/parser.go:50,157](pkg/html/parser.go:50) — `<style>` blocks and
  `<link>` sheets both append raw text via the same `Stylesheets = append(...)`
  path. The `<link>` branch knows the `href` but doesn't retain it.
- [pkg/css/cascade.go:544](pkg/css/cascade.go:544) and
  [pkg/resource/renderer.go:221,355](pkg/resource/renderer.go:221) — consumers
  iterate `doc.Stylesheets []string` and call `ParseStylesheet(text, ctx)` (post-
  Phase 2) with `ctx` built from `doc.BaseDir`. All sheets share one context.

**New type** — mirrors Blink's `StyleSheetContents`:

```go
// pkg/css/style_sheet_contents.go — mirrors core/css/style_sheet_contents.h @ d4ecdfed8
// Note: Blink's StyleSheetContents holds already-parsed rules. louis14's
// version holds the source text + parser context; cascade still re-parses on
// demand. Eager-parse-at-html-time is a future ticket (would also drop the
// re-parse hot path in cascade + resource/renderer).
type StyleSheetContents struct {
    Text    string         // mirrors StyleSheetContents::source_text_
    Context *ParserContext // mirrors StyleSheetContents::parser_context_
}
```

**Per-sheet base URL derivation:**

Each storage site computes the right BaseDir at write time:

- **`<style>` block** ([parser.go:50](pkg/html/parser.go:50)): inline in the
  document — BaseDir = `doc.BaseDir`. The block has no separate URL identity.
  Blink does the same (`css_style_sheet.cc:106-112`).
- **`<link rel=stylesheet href="...">`** ([parser.go:157](pkg/html/parser.go:157)):
  the sheet's URL is `css.CompleteURL(href, doc.BaseDir)` (URL-aware after Phase 4);
  BaseDir = `css.URLDir(<that resolved URL>)`. Code change:

  ```go
  // pkg/html/parser.go:152-162 — add href -> per-sheet baseDir derivation
  if href, ok := token.Attributes["href"]; ok {
      if cssText := p.loadLinkStylesheet(href); cssText != "" {
          sheetURL := css.CompleteURL(href, p.doc.BaseDir)
          baseDir := css.URLDir(sheetURL)
          p.doc.Stylesheets = append(p.doc.Stylesheets,
              &css.StyleSheetContents{
                  Text:    cssText,
                  Context: css.NewParserContext(baseDir, p.cssFetcher),
              })
      }
  }
  ```

  This handles both relative href (`"support/main.css"` against doc-relative
  base) and absolute href (`"https://cdn.example.com/styles/main.css"` against
  any base) correctly, because Phase 4's primitives are URL-aware.
- **`data:text/css,...`** ([parser.go:480](pkg/html/parser.go:480)): no URL
  identity; BaseDir = `doc.BaseDir`.

**Consumer updates:**

- [pkg/css/cascade.go:544](pkg/css/cascade.go:544) — iterate `doc.Stylesheets`
  (`[]*StyleSheetContents`), call `ParseStylesheet(entry.Text, entry.Context)`.
- [pkg/resource/renderer.go:221,355](pkg/resource/renderer.go:221) — same.
- The `ParserContext` is built **once per entry** at HTML-parse time (not on
  every cascade run). Cache the context on the entry.

**`pkg/css` ↔ `pkg/html` layering:** `StyleSheetContents` lives in `pkg/css`
(its home in Blink). `pkg/html` must import `pkg/css` to construct entries. If
this introduces a circular dep, the alternative is `pkg/html.StyleSheetSource`
(local mirror) with a converter at the css-package boundary — but
`grep -r "pkg/css" pkg/html/` should be checked first; if `pkg/html` already
depends on `pkg/css` (it doesn't today, looks like), this is the chance to
introduce the edge cleanly.

**Tests added in this phase:**

Failing-test gate: a test exercising an external `<link>` sheet in a
sub-directory, with a `url()` value that requires per-sheet base resolution.
The simplest property to drive this is **filter** (already URLData-wrapped post-
LOU-137 v2; post-Phase 2 it resolves at parse via the context). Author under
`pkg/visualtest/testdata/local/lou-138-link-stylesheet-base.html`:

- `support/styles.css` containing `.x { filter: url(hueRotate.svg#MyFilter); }`
- `support/hueRotate.svg` defining the filter
- Outer HTML: `<link rel=stylesheet href="support/styles.css"><div class=x>…</div>`

Before Phase 5 + post-Phase 2, the link sheet uses `doc.BaseDir=""` → filter url
resolves to `"hueRotate.svg"` → file not found at outer base → broken render.
After Phase 5, the link sheet uses `BaseDir="support"` → resolves to
`"support/hueRotate.svg"` → correct.

**Confirm the test fails at HEAD before Phase 5 lands.** If it doesn't fail,
re-check the test setup before proceeding (per `feedback_predictive_humility`).

**Gate:**
- The new Phase-5 test PASSES at 0% diff.
- All LOU-13x gate tests + Phase-3 + Phase-4 gate tests still PASS at 0% diff.

**Commit:** "feat(css+html): per-sheet ParserContext via StyleSheetContents (LOU-138 phase 5)"

---

### Phase 6 — `@import` per-sheet base URL

**Goal:** stop the HTML-parse-time inlining of `@import` (which discards the
imported sheet's URL) and replace it with CSS-parse-time recursive resolution
that builds a fresh `ParserContext` per imported sheet — mirroring Blink's
`StyleRuleImport::NotifyFinished` pattern.

**Blink reference (pinned SHA `d4ecdfed8`):**

```cpp
// core/css/style_rule_import.cc:77-82
CSSParserContext* context = MakeGarbageCollected<CSSParserContext>(
    parent_context, cached_style_sheet->GetResponse().ResponseUrl(),
    cached_style_sheet->GetResponse().IsCorsSameOrigin(),
    Referrer(cached_style_sheet->GetResponse().ResponseUrl(),
             cached_style_sheet->GetReferrerPolicy()),
    cached_style_sheet->Encoding(), document);
```

The key invariant: the imported sheet's CSSParserContext inherits from the
parent's (mode, document, etc.), but `base_url_` is the **response URL** of the
imported sheet, not the parent's base URL.

**Current louis14 state to change:**

- [pkg/html/parser.go:494-540](pkg/html/parser.go:494) `resolveImports` — fetches
  the imported CSS and **textually prepends** it to the importing sheet, losing
  the imported sheet's URL identity. Called from `<style>`-block path
  ([parser.go:50](pkg/html/parser.go:50)) and `<link>`-sheet path
  ([parser.go:485](pkg/html/parser.go:485)).
- [pkg/html/parser.go:544 `parseImportURL`](pkg/html/parser.go:544) — URL
  extraction helper. Move to `pkg/css` (mirrors Blink's parser-side @import URL
  extraction).
- [pkg/css/stylesheet.go:427](pkg/css/stylesheet.go:427) — current at-rule
  scanner comment: "Unknown at-rules (@three-dee, @import, etc.) are silently
  skipped." This branch becomes the @import handler.

**Implementation:**

1. **Add `Fetcher` to `ParserContext`:**

   ```go
   // pkg/css/parser_context.go (Phase 1 file, extended here in Phase 6)
   type ParserContext struct {
       BaseDir string
       Fetcher func(uri string) (string, error) // mirrors CSSResourceClient
                                                 // wiring on Blink's
                                                 // StyleRuleImport
   }
   ```

   Blink reaches the fetcher via `Document*` on CSSParserContext; louis14 holds
   it directly. Document the deviation in the type doc comment.

2. **Stop inlining at HTML-parse time.** Either delete `resolveImports`
   ([parser.go:494](pkg/html/parser.go:494)) or shrink it to a no-op that just
   returns the raw text unchanged. `parseImportURL` moves to
   `pkg/css/parser_context.go` (or a new `pkg/css/at_import.go`) as a private
   helper.

3. **Handle `@import` in `ParseStylesheet`:** at the at-rule scanner branch
   ([stylesheet.go:427](pkg/css/stylesheet.go:427)), when the at-rule is
   `@import`:

   - Extract the URL via the relocated `parseImportURL`.
   - Compute the imported sheet's URL:
     `importedURL = ctx.CompleteURL(rawURL)` (uses the importing context's
     BaseDir — matches Blink's "URL resolution is relative to the importing
     sheet").
   - Fetch via `ctx.Fetcher(importedURL)`. If nil/error, skip (matches today's
     behavior — silent failure).
   - Build a new ParserContext for the imported sheet:
     `importedCtx := &ParserContext{BaseDir: URLDir(importedURL), Fetcher: ctx.Fetcher}`.
     This is the Blink invariant: `base_url_ = response URL`. Uses Phase 4's
     URL-aware `URLDir` so absolute imported-URL paths survive intact.
   - Recursively call `ParseStylesheet(importedCSS, importedCtx)`. (Recursion
     terminates on @import cycles only via the resource cache; that cache is
     out of scope for LOU-138 — tracked separately at
     [LOU-139](https://linear.app/mazarin/issue/LOU-139/css-resource-cache-for-import-dedupe-cycle-detection).
     Without it, a self-importing sheet loops forever; no test exercises that
     today.)
   - Inline the imported sheet's Rules into the current stylesheet's Rules at
     the @import's position. (CSS spec: @import rules apply BEFORE the importing
     sheet's other rules, so the imported rules go at the head of the rule list
     in source order; the existing scanner already enforces @import-before-
     other-rules.)
   - Also fold imported FontFaces and CounterStyles into the current sheet
     (consumers iterate them flat today).

4. **Wire the fetcher into ParserContext at construction:**

   - `pkg/html/parser.go:50,157` — when building `StyleSheetContents.Context`
     in Phase 5, set `Fetcher: p.cssFetcher` as well.
   - `pkg/css/cascade.go:496` (inline `style=""` attrs) — Fetcher: nil
     (no `@import` allowed in style attributes; matches Blink).

5. **Delete dead code:**

   - `resolveImports` and `parseImportURL` (relocated to css).
   - `loadLinkStylesheet`'s `p.resolveImports(css)` call sites
     ([parser.go:480,485](pkg/html/parser.go:480)) become bare `css` returns —
     no pre-processing.

**Tests added in this phase:**

Failing-test gate: a test exercising `@import` from a sub-directory'd sheet
where the imported sheet's `url()` value requires the imported sheet's own base
(not the parent sheet's, not the document's).

Author under `pkg/visualtest/testdata/local/lou-138-at-import-base.html`:

- `support/outer.css` containing `@import "imports/inner.css";`
- `support/imports/inner.css` containing `.x { filter: url(hueRotate.svg#MyFilter); }`
- `support/imports/hueRotate.svg` defining the filter
- Outer HTML: `<link rel=stylesheet href="support/outer.css"><div class=x>…</div>`

Today (pre-Phase 6): outer.css gets textually inlined into the same buffer at
HTML-parse time; the merged text is stored with whatever the outer storage path
records (post-Phase 5: BaseDir=`support`). Inner.css's `url(hueRotate.svg)`
then resolves to `support/hueRotate.svg` — WRONG (file is at
`support/imports/hueRotate.svg`).

After Phase 6: the `@import` is handled at CSS-parse time with a context whose
BaseDir = `support/imports` (the imported sheet's URL dir). The filter url
resolves to `support/imports/hueRotate.svg` — correct.

**Confirm failure at HEAD before Phase 6 lands.**

**Gate:**
- The new Phase-6 test PASSES at 0% diff.
- The Phase-4 + Phase-5 tests still PASS at 0% diff.
- All LOU-13x gate tests + Phase-3 gate test still PASS at 0% diff.
- A test for the basic @import case (no sub-directory nesting) — find or write
  one — still PASSES, proving we didn't regress @import functionality itself
  while moving its resolution from HTML-parse to CSS-parse time.

**Commit:** "feat(css): @import resolution via per-import ParserContext (LOU-138 phase 6)"

---

### Phase 7 — Property-by-property migration to CSSImageValue / CSSURIValue wrappers

**Goal:** for each url()-bearing property still stored as a raw string, follow the
Blink shape — parser uses `ctx.CollectUrlData`, storage is a typed wrapper,
consumer reads the absolute URL directly. **One property per commit, each driven
by a failing test that regressed in Phase 3 (or that we author if Phase 3 didn't
surface one).**

**Two new wrapper types** (file placement: mirror Blink's `core/css/`):

```go
// pkg/css/css_image_value.go — mirrors core/css/css_image_value.h:37 @ d4ecdfed8
type CSSImageValue struct {
    Data URLData
    // cached_image_ is intentionally omitted until louis14 has a StyleImage
    // fetch lifecycle to back it.
}

// pkg/css/css_uri_value.go — mirrors core/css/css_uri_value.h:23 @ d4ecdfed8
type CSSURIValue struct {
    Data URLData
}
```

Keep the wrappers minimal at first; do not pre-add fields for features (referrer,
origin-clean, resource cache) that no current consumer needs. Add when a test demands.

**Per-property migration template:**

For each property below, the commit does exactly four things, in this order:

1. **Run the failing test first.** Confirm it fails at HEAD (post-Phase 3).
2. **Parser change** at the property's parser site: replace raw-string capture
   with `ctx.CollectUrlData(inner)` wrapped in the appropriate typed value.
3. **Storage change**: change the Style/FillLayer/Stylesheet field type from
   `string` to `*CSSImageValue` (or `*CSSURIValue`). Update the getter.
4. **Consumer change** in `pkg/render/`: read `.Data.Absolute` instead of the raw
   string. **Delete** any `filepath.Join(basePath, uri)` fallback at the consumer
   site (the URL is now guaranteed absolute by the parser).
5. **Re-run the failing test. It MUST go green.** Re-run the LOU-13x gate set —
   no regressions allowed.

**Order (smallest-blast-radius first):**

| # | Property              | Storage site                                  | Consumer site                                    | Wrapper          |
|---|-----------------------|-----------------------------------------------|--------------------------------------------------|------------------|
| 1 | `background-image`    | [pkg/css/style.go:7374](pkg/css/style.go:7374) `FillLayer.Image` | [pkg/render/paint_layer.go:618](pkg/render/paint_layer.go:618) | `CSSImageValue`  |
| 2 | `mask-image`          | [pkg/css/style.go:8975](pkg/css/style.go:8975) `GetMaskImage`    | paint_layer.go                                   | `CSSImageValue`* |
| 3 | `border-image-source` | [pkg/css/style.go:9021](pkg/css/style.go:9021) `GetBorderImageSource` | paint_layer.go                              | `CSSImageValue`  |
| 4 | `list-style-image`    | [pkg/css/style.go:1960](pkg/css/style.go:1960) (Phase-5 parser)  | list rendering                                   | `CSSImageValue`  |
| 5 | `@font-face src`      | [pkg/css/stylesheet.go:96](pkg/css/stylesheet.go:96) `FontFaceRule.Src` | font resource loader                      | `CSSURIValue`    |

*`mask-image` accepts both `url(image)` and `url(#svgref)`. Blink dispatches on
token shape: image form → `CSSImageValue`, fragment ref → `CSSURIValue`. Mirror
this — handle both in the parser, branch on result type.

**`cursor` and `shape-outside`:** the url() forms are not implemented today.
Out of scope for this ticket; they get migrated when first needed.

**One commit per row** to keep blame and revert clean. Worktree agents acceptable
for migrations 3-5 once 1-2 land on master (per CLAUDE.md operational rules:
commit+push before launching worktrees).

---

### Phase 8 — Final cleanup

- Verify `ResolveRelativeURLsInHTML` and `ResolveRelativeURLsInCSS` are both
  deleted (grep for callers; none expected).
- Verify `resolveImports` and `parseImportURL` are both deleted from
  `pkg/html/parser.go` (relocated to `pkg/css` in Phase 5).
- Update [pkg/css/style.go:57](pkg/css/style.go:57) `Style.BaseDir` doc comment:
  it's still used for inline `style=""` parsing context construction in cascade,
  but no longer for getter-time url() resolution (consumers see absolute URLs).
- Remove `s.BaseDir` argument from `parseFilterList` if Phase 2 left it as a
  no-op param (Go compiler will flag unused-arg if we tag it).
- Run `simplify` skill on the full diff before the final commit.
- Update LOU-138 description with the actual commits.

---

## Gate (acceptance criteria for closing LOU-138)

- All five LOU-13x gate tests still PASS at 0% diff.
- The Phase-3 gate test (`<style>{filter: url(...)}` inside iframe) PASSES at 0% diff.
- The Phase-4 unit tests (CompleteURL + URLDir) PASS, covering absolute, scheme-
  relative, root-relative, and path-relative references against both absolute and
  relative bases.
- The Phase-5 gate test (external `<link>` sheet in sub-directory with relative url())
  PASSES at 0% diff.
- The Phase-6 gate test (`@import` from sub-directory'd sheet with relative url())
  PASSES at 0% diff.
- For each property migrated in Phase 7, a corresponding test PASSES at 0% diff
  (this is what proves the per-property regression from Phase 3 is fixed).
- `ResolveRelativeURLsInHTML` is gone from the codebase (`grep -r` returns nothing).
- `resolveImports` and `parseImportURL` are gone from `pkg/html/parser.go` (relocated
  to `pkg/css` in Phase 6).
- `path.Join` / `path.Dir` are gone from URL-composition sites in `pkg/css`,
  `pkg/layout/engine.go`, `pkg/layout/layout_algorithm.go`, and `pkg/html/parser.go`
  (filesystem-specific uses like `pkg/layout/svg/svg_document_fetcher.go`'s
  `filepath.Dir` are intentionally retained).
- `layoutNestedDocument` does NOT call any URL pre-baking helper before `html.Parse`.
- The Blink chokepoint invariant holds: every `url()` token consumed during
  CSS parse goes through `ParserContext.CollectUrlData`. Spot-check by grepping
  for `URLData{` literal construction outside `parser_context.go` — there should
  be none after Phase 7.
- The Blink per-sheet base-URL invariant holds: every entry in `doc.Stylesheets`
  has its own `ParserContext`; `<link>` sheets use `URLDir(CompleteURL(href, doc.BaseDir))`
  and `@import` sheets use the imported sheet's URL dir via the same URL-aware
  primitive — neither falling back to `doc.BaseDir`.

---

## Open questions — resolved 2026-05-19

### Q1. External stylesheet base URL — **folded into Phase 4**

louis14 fetches external `<link rel=stylesheet>` content at
[pkg/html/parser.go:484 `loadLinkStylesheet`](pkg/html/parser.go:484) and
appends raw text to `doc.Stylesheets []string` with no per-sheet source URL
retained. Cascade falls back to `doc.BaseDir`. WRONG for sub-directory'd
external sheets.

Originally deferred as a separate sub-ticket; on user instruction (we have the
context now and the fix is trivial after Phase 1's ParserContext primitive),
folded in as **Phase 4**. See Phase 4 section above for full plan, Blink
references, and test gate.

### Q2. `@import` handling — **folded into Phase 5**

`@import` is implemented at the wrong layer:
[pkg/html/parser.go:494 `resolveImports`](pkg/html/parser.go:494) inlines
imported CSS at HTML-parse time, discarding the imported sheet's URL. Imported
content's `url()`s resolve against the wrong base.

Originally deferred; folded into LOU-138 as **Phase 5** on the same rationale
as Q1. See Phase 5 section above for full plan, Blink references
(`style_rule_import.cc:77-82` at pinned SHA), and test gate.

### Q3. `URLData.Relative` after parse-time rewriting — **keep both fields, accept asymmetry**

Today, no consumer reads `URLData.Relative` (grep confirms only
`.Absolute` is read, at [pkg/render/filter_effect_builder.go:60,69](pkg/render/filter_effect_builder.go:60)).
`.Relative` is populated for future-proofing (getComputedStyle / CSSOM
serialization).

**Decision:** Keep `URLData` shape as `{Relative, Absolute}` — mirrors Blink's
`CSSUrlData{relative_url_, absolute_url_}`. Accept that two paths produce
different fidelity:

- **String-rewrite path (Phase 2):** Properties map holds the absolute string;
  the original Relative is lost at the map level. For filter post-Phase 2,
  `GetFilter()` calls `parseFilterList(val, "")` which produces
  `URLData{Relative: absoluteStr, Absolute: absoluteStr}` — both fields equal.
  Acceptable: no consumer reads Relative.
- **Typed-wrapper path (Phase 4):** Each property's parser site calls
  `ctx.CollectUrlData(rawToken)` which captures the pre-rewrite raw token as
  Relative and the resolved form as Absolute. Wrappers retain both faithfully.

When a future consumer (e.g. getComputedStyle implementation) needs accurate
Relative on a string-rewrite-path property, that property gets migrated to the
typed-wrapper path. The migration recipe is the Phase-6 template.

---

## Risks

- **Renderer fetcher implicit-base behavior.** The LOU-138 description says today's
  rendering works because `ResolveRelativeURLsInHTML` + the renderer fetcher's
  `filepath.Join(basePath, uri)` combine. The louis14 survey found **no central
  renderer fetcher with that join** — only test fixtures use `filepath.Join`. If
  the description's claim is true at some other layer (image fetcher in
  `pkg/images/`?), Phase 3's regression catch may be incomplete. Investigate
  before Phase 3 lands: grep `pkg/images/` and `pkg/resource/` for `filepath.Join`
  patterns that take a per-document basePath. Document findings in
  `~/.claude/ticket-active/LOU-138/findings.md`.

- **Cascade re-parse on adoptNode cross-document moves.** LOU-137 v2's commit
  message notes "moved DOM nodes don't take their `<style>` with them." After
  Phase 2, this still holds — inline `style=""` is re-resolved against the new
  document's BaseDir when cascade re-runs. Verify the LOU-137 cross-doc-move
  test (`svg-relative-urls-001`) still passes after each phase; if it breaks,
  the BaseDir re-stamp path in `cascade.go:529-535` needs adjustment.

- **`isAbsoluteCSSURL` vs. path-based URLs.** The function only recognizes
  scheme-prefixed URLs (`http://`, `data:`, etc.) and root-relative (`/foo`).
  A path-based URL like `support/foo.svg` is "relative" to `isAbsoluteCSSURL`
  but is already-resolved with respect to a `support`-rooted BaseDir.
  `ResolveURL("support/foo.svg", "support")` therefore double-prepends — this
  is exactly the Problem-1 bug. Phase 2's parse-time resolution + Phase 2(a)'s
  "filter getter passes empty BaseDir" both depend on the absolute URL never
  being re-fed to `ResolveURL` with a non-empty BaseDir. If any code path
  violates this, the bug returns. **Add a regression test** in `pkg/css` that
  asserts `ResolveURL(ResolveURL(x, b), b) == ResolveURL(x, b)` only when the
  input is scheme-prefixed; document the path-based fragility.

---

## Out of scope

- Migrating to a fully-typed CSSValue tree (replacing `map[string]string`
  declaration storage). That's the "Option 2" the ticket mentions; we get the
  Blink chokepoint invariant without it. File a separate ticket if a test ever
  demands it (e.g., getComputedStyle / CSSOM serialization).
- Eager parse at HTML-parse time. Phase 5's `StyleSheetContents` holds source
  text + context; Blink's holds parsed rules. Migrating to eager parse drops
  the re-parse hot path in cascade and resource/renderer but is a separate
  optimization — file when the re-parse cost shows up in profiling.
- Implementing `cursor: url(...)` and `shape-outside: url(...)`. Not present
  today; migrate when implemented.
- Implementing `StyleImage` / `StyleFetchedImage` (Blink's fetch-lifecycle
  abstraction). louis14 doesn't have a fetch lifecycle to attach it to.
  Wrappers in Phase 7 omit the `cached_image_` field deliberately.
- `ReResolveUrl` (Blink's "dangling markup" edge case at `css_url_data.cc:152`).
  Not needed for a correct parse-time pipeline; skip.
- **CSS resource cache for `@import` (dedupe + cycle detection).** Filed as
  [LOU-139](https://linear.app/mazarin/issue/LOU-139/css-resource-cache-for-import-dedupe-cycle-detection).
  Without it, the recursive `@import` resolution in Phase 6 loops forever on
  self-importing sheets. No test exercises that today; the cache work belongs
  with the broader resource-loading model and is its own scope.
