---
layout: default
title: Louis14
author: iansmith
---

# Louis14

A Go browser engine focused on CSS layout and paint. The architecture
mirrors Blink's LayoutNG — same fragment tree shape, same break-token
model, same constraint-passing patterns — ported into idiomatic Go.

Louis14 is the layout/render half of the [mazarin](https://iansmith.github.io/mazarin/)
project. It does not implement networking or scripting; it consumes
parsed HTML + CSS and produces rasterized PNG output via a directly
written paint pipeline.

## Status

Active development. The current focus is conformance with the W3C CSS
WPT reference-test suite. Recent work has concentrated on
`css-multicol` (multi-column fragmentation), with prior phases for
`css-writing-modes`, `css-flexbox`, and `css-position`.

Current gate (post Phase 20, multicol overflow clip Blink-aligned port):

| Suite             | Passing |
|-------------------|---------|
| CSS 2.1 reftests  | 99/99   |
| css-flexbox       | 626/629 |
| css-position      | 92/105  |
| css-writing-modes | 781/781 |
| css-multicol      | 211/455 |

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

The principles that govern day-to-day work are documented in the
repository's [`CLAUDE.md`](https://github.com/iansmith/louis14/blob/master/CLAUDE.md):
foundational correctness over quick wins, study Blink before writing
code in a new area, all tests at 0% diff (pixel-perfect against the
WPT references), and tight test-execution discipline.
