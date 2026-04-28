---
layout: default
title: Louis14
author: iansmith
---

# Louis14

A Go browser engine focused on CSS layout and paint. Louis14 takes
parsed HTML and CSS, runs the layout, and rasterises the result to a
PNG via a directly-written paint pipeline. There is no JavaScript
engine, no network stack, no DOM API surface beyond what layout
needs — it is the layout/render half of the
[mazarin](https://iansmith.github.io/mazarin/) project (a pure-Go
operating system), built so that the OS can render documents and UI
without depending on a C-language browser engine.

The architecture mirrors Blink's
[LayoutNG](https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/README.md)
— same fragment-tree shape, same break-token model, same
constraint-passing patterns — ported into idiomatic Go. When in doubt
about how something should work, the rule is: read Blink first.

## Goal

Pixel-perfect conformance with the W3C
[Web Platform Tests](https://web-platform-tests.org/) (WPT) suite.
"Pixel-perfect" means literally zero pixels of difference against the
WPT reference renderings — no fuzzy tolerances, no anti-aliasing
allowances. WPT tests already encode the tolerances their authors
considered acceptable; if a test diffs at all, it is a real bug to fix
at the source.

## Current WPT scores

Conformance state on the per-suite reference-test count, post Phase
20 (multicol overflow clip Blink-aligned port):

| Suite                                                                               | Passing | Notes                                  |
|-------------------------------------------------------------------------------------|---------|----------------------------------------|
| [CSS 2.1 reftests](https://web-platform-tests.org/running-tests/index.html)         | 99/99   | invariant                              |
| [css-flexbox](https://drafts.csswg.org/css-flexbox-1/)                              | 626/629 | three pre-existing residuals           |
| [css-position](https://drafts.csswg.org/css-position-3/)                            | 92/105  | thirteen pre-existing residuals        |
| [css-writing-modes](https://drafts.csswg.org/css-writing-modes-4/)                  | 781/781 | complete                               |
| [css-multicol](https://drafts.csswg.org/css-multicol-1/)                            | 211/455 | active feature track                   |

The 13-test multicol "driver invariant" subset (the named tests that
gate any multicol change) is at 13/13 at zero diff.

## Source

Repository: <https://github.com/iansmith/louis14>

The codebase is organised under `pkg/`:

- `pkg/layout/` — the layout engine (block, inline, flex, multicol,
  table, fragmentation).
- `pkg/render/` — paint-time pipeline (paint layers, painter, text
  shaping consumers).
- `pkg/css/` — CSS parsing, cascade, computed values.
- `pkg/html/` — HTML parser.
- `pkg/text/` — font configuration and text measurement.
- `pkg/visualtest/` — WPT reference-test harness (PNG-diff against
  embedded test fixtures).

## Project principles

The day-to-day rules live in the repository's
[`CLAUDE.md`](https://github.com/iansmith/louis14/blob/master/CLAUDE.md):

1. Foundational correctness over quick wins — every fix has to work
   for all cases, not the easy ones.
2. Study Blink before writing code in a new area.
3. All tests must pass at zero pixel difference.
4. Test execution discipline — only run the tests the current change
   targets, not the full suite, except at gate boundaries.
