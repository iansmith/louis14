# l14 — A Standards-Compatible Web Browser Engine

l14 (pronounced "el-fourteen") is a web browser rendering engine written in Go, built from scratch with the goal of full standards compatibility. The project is an experiment in how far a browser engine can be taken with modern AI-assisted development: every line of code in this repository was written by [Claude Code](https://claude.ai/code), Anthropic's AI coding assistant, with human direction but no hand-written code.

The engine implements the full CSS visual formatting model — block layout, inline layout, flexbox, grid, multi-column, and table layout — along with an HTML parser, a CSS cascade, a rendering pipeline, and a basic JavaScript execution environment (powered by [goja](https://github.com/dop251/goja)). It renders to PNG using Go's image libraries and a fork of [gg](https://github.com/fogleman/gg) for 2D graphics.

## Standards Compliance: Where We Stand

Compatibility is measured using the [Web Platform Tests](https://web-platform-tests.org/) (WPT) suite, the industry-standard test harness used by all major browser vendors to track their own spec compliance. WPT provides tens of thousands of reftests and script tests covering every corner of the web platform. We run the CSS3 WPT reftests as our primary benchmark.

As of early March 2026, l14 passes approximately 2,780 of 6,720 CSS3 WPT reftests (roughly 41%). The CSS2 suite passes entirely at 99/99. Within CSS3, results vary significantly by feature area. CSS Grid is currently at 100% (72/72 passing), CSS Box and CSS Environment Variables are also at 100%, and CSS Masking, CSS Text, and CSS Logical Properties are all above 95%. CSS Flexbox sits at about 74% (464/630). These are areas where the layout model is largely correct and remaining failures tend to be edge cases.

The more difficult categories are CSS Writing Modes (17%, 135/790) and CSS Multi-Column Layout (18%). These features involve fundamental changes to the flow direction or fragmentation of content, and getting them right requires rethinking parts of the layout engine that were built assuming horizontal left-to-right flow. Progress in these areas tends to be incremental — each bug fix is meaningful, but the surface area is large.

## Current Stumbling Blocks

The biggest remaining challenges are writing modes and multi-column layout. CSS Writing Modes (`writing-mode: vertical-rl`, `vertical-lr`, etc.) require the entire layout pipeline — inline measurement, block stacking, flexbox cross-axis, and grid track sizing — to understand that the main axis may be vertical. While block-level and flex containers now handle vertical writing modes, the interactions between writing modes and more complex layout contexts (nested flex/grid, tables, multi-column) remain partially broken.

Multi-column layout is similarly deep. The CSS multi-column spec requires fragmenting content across columns, which touches the same fragmentation model that would eventually be needed for paged media and CSS regions. The current implementation handles basic cases but struggles with spanning elements, nested multi-column containers, and correct balancing.

CSS Flexbox, while mostly working, still has failures in areas like baseline alignment, complex percentage-sized children, and interactions with overflow. These are mostly known spec corner cases rather than fundamental misunderstandings of the model.

JavaScript support is intentionally minimal — enough to handle the WPT tests that mutate the DOM on load (`img.src` reassignment, `replaceChild`, `splitText`, `getBoundingClientRect` as a layout-flush idiom). Full JS execution is not a near-term goal.

## Architecture

The engine is organized as a set of loosely coupled packages:

- `std/net` — HTTP/HTTPS fetching and URL resolution, with no internal dependencies
- `pkg/html` — HTML tokenizer and parser, with optional CSS fetcher for external stylesheets
- `pkg/css` — CSS parser, cascade, and computed style resolution
- `pkg/layout` — The CSS layout engine: block, inline, flex, grid, table, multi-column
- `pkg/render` — The rendering pipeline: paint order, stacking contexts, clipping, compositing
- `pkg/images` — Image loading (PNG, JPEG, GIF, WebP, SVG) with network fetcher support
- `pkg/resource` — Fetcher/Renderer interfaces that wire the pipeline together
- `pkg/js` — JavaScript execution via goja, with a DOM implementation

## CLI Tools

Two command-line tools are available for rendering:

```bash
# Render a local HTML file to PNG and open it
l14open <input.html> <output.png> [width] [height]

# Fetch a URL and render to PNG
l14show [-w 800] [-h 600] [-o output.png] <url>
```

## Running the Tests

The WPT CSS3 reftests are the primary test suite. To run a category:

```bash
go test ./pkg/visualtest/... -run TestWPTCSS3Reftests/css-flexbox -v -timeout 120s
```

All comparisons use pixel-exact diffing with a threshold of under 0.1% different pixels (fewer than 480 pixels in an 800×600 viewport). Fuzzy matching is explicitly prohibited — it conceals real rendering bugs and masks regressions.

## The AI Development Experiment

This project is a live experiment in AI-assisted systems programming. The entire codebase — the HTML parser, the CSS cascade, the layout engine with all its layout modes, the renderer, the JS DOM — was produced through conversations with Claude Code, guided by test failures and human judgment about what to fix next. No code was written by hand.

The development process works by running WPT reftests, identifying failures, reasoning about the CSS specification, and asking Claude Code to implement the fix. Bugs range from trivial (a missing `case` in a switch statement) to subtle (double-counting border/padding in grid auto row sizing because the box model convention changed mid-development). The AI handles both kinds.

The current pace is several dozen WPT tests gained per development session, with the rate slowing as the low-hanging fruit is exhausted and the remaining failures require deeper architectural work.
