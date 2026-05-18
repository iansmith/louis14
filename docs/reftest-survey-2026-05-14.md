# WPT Reftest Survey — Passing Tests by Region

**Date:** 2026-05-14 (updated — overwrites the earlier 64e204b3 baseline snapshot)
**Branch:** `fix/LOU-114` (HEAD `a2de9e3f`)
**Purpose:** Section-by-section progress snapshot for the reftest suite. Pass/fail/skip
per region, so we can pick a section and solve it completely (CLAUDE.md principle #1).

## How this was generated

```
cd pkg/visualtest
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -run 'TestWPTReftests|TestWPTCSS3Reftests' \
  -v -timeout 90m 2>&1 \
  | grep -E '(^\s*--- (PASS|FAIL|SKIP): |Summary: |Found [0-9]+ WPT)' \
  > /tmp/reftest-survey-raw.txt
```

"Region" = the first path component under each testdata root. **SKIP** = `flags="dom"`
(needs JS/DOM scripting) or no usable reference file — not applicable to a static
renderer, not failures.

## Changes since the 64e204b3 baseline

This snapshot is at `a2de9e3f` — after landing marker foundation Phases 1–6,
css-masking, css-animations, filter-effects, and the CodeRabbit PR#5 review fixes.

**CSS3: 3897 → 3952 passing, net +55.** CSS2 unchanged at 99/99.

### Gains
| Region | Before | After | Δ |
|--------|-------:|------:|--:|
| filter-effects | 92 | 130 | **+38** |
| css-animations | 6 | 16 | **+10** |
| css-pseudo | 59 | 64 | **+5** |
| css-masking | 2 | 4 | **+2** |
| css-multicol | 238 | 240 | **+2** |
| css-color | 129 | 130 | +1 |

### ⚠️ Regressions — to investigate
| Region | Before | After | Δ |
|--------|-------:|------:|--:|
| css-cascade | 19 | 18 | **−1** |
| css-contain | 169 | 168 | **−1** |
| css-counter-styles | 3 | 2 | **−1** |

These appeared while landing the work above. Likely cause: the landed changes touch
shared code paths these regions exercise — `pkg/css/cascade.go` (marker `::marker`
cascade + the animation hook + the CodeRabbit `text-orientation` / `markerAllowedProperty`
edits), `CreatesStackingContext` in `pkg/layout/types.go` (css-masking added `clip-path`/
`mask`), and the marker text source (counter-style feeds `@counter-style` tests). Not yet
root-caused — flagged here so it isn't lost.

### Notes
- `css-masking`'s 3 `clip-path` basic-shape tests still need the `mazarin/textshape`
  `draw_impl.go` clip-as-mask change ported into the `~/mazzy` repo to pass at runtime;
  the +2 here is the mask-mode work.
- `css-will-change` / `css-text-decor` are unchanged — plans written, not yet implemented.

## Headline

| Suite | Pass | Fail | Skip | Run (P+F) | Pass rate |
|-------|-----:|-----:|-----:|----------:|----------:|
| **CSS2** | 99 | 0 | 0 | 99 | **100.0%** |
| **CSS3** | 3,952 | 2,627 | 140 | 6,579 | **60.1%** |
| **Total** | 4,051 | 2,627 | 140 | 6,678 | **60.7%** |

CSS2 is complete. CSS3 is the remaining surface — 2,627 failing reftests across 41 regions.

## Regions we may need to *consider*

**No region is at zero passes** — every region that ran has at least one pass. The
lowest pass rates (genuine engine work, except as noted):

| Region | Pass | Fail | Skip | Pass rate | Note |
|--------|-----:|-----:|-----:|----------:|------|
| css3/css-ruby | 24 | 75 | 0 | 24.2% | ruby layout largely unbuilt — plan exists (`plan-css-ruby.md`) |
| css3/css-pseudo | 64 | 146 | 8 | 30.5% | plan exists; marker foundation already lifted it +5 |
| css3/css-lists | 44 | 100 | 11 | 30.6% | plan exists; counter tree (B3/B4) is the critical path |
| css3/css-counter-styles | 2 | 4 | 0 | 33.3% | tiny region; regressed −1 — see above |
| css3/css-display | 27 | 53 | 1 | 33.8% | no plan yet |
| css3/css-cascade | 18 | 34 | 1 | 34.6% | plan exists; regressed −1 — see above |

`css-animations` has moved out of this group — 25.0% → **66.7%** after the keyframe engine landed.

## CSS3 — all regions, worst pass rate first

| Region | Pass | Fail | Skip | Run | Pass rate |
|--------|-----:|-----:|-----:|----:|----------:|
| css3/css-ruby | 24 | 75 | 0 | 99 | 24.2% |
| css3/css-pseudo | 64 | 146 | 8 | 218 | 30.5% |
| css3/css-lists | 44 | 100 | 11 | 155 | 30.6% |
| css3/css-counter-styles | 2 | 4 | 0 | 6 | 33.3% |
| css3/css-display | 27 | 53 | 1 | 81 | 33.8% |
| css3/css-cascade | 18 | 34 | 1 | 53 | 34.6% |
| css3/css-will-change | 17 | 30 | 0 | 47 | 36.2% |
| css3/css-text-decor | 96 | 154 | 0 | 250 | 38.4% |
| css3/css-fonts | 111 | 174 | 2 | 287 | 38.9% |
| css3/css-nesting | 8 | 12 | 0 | 20 | 40.0% |
| css3/css-conditional | 49 | 71 | 0 | 120 | 40.8% |
| css3/selectors | 55 | 79 | 18 | 152 | 41.0% |
| css3/css-color | 130 | 179 | 1 | 310 | 42.1% |
| css3/css-variables | 85 | 97 | 3 | 185 | 46.7% |
| css3/css-sizing | 96 | 109 | 3 | 208 | 46.8% |
| css3/css-backgrounds | 167 | 183 | 5 | 355 | 47.7% |
| css3/css-values | 75 | 81 | 0 | 156 | 48.1% |
| css3/filter-effects | 130 | 140 | 6 | 276 | 48.1% |
| css3/css-ui | 77 | 79 | 16 | 172 | 49.4% |
| css3/css-inline | 7 | 7 | 0 | 14 | 50.0% |
| css3/css-masking | 4 | 4 | 0 | 8 | 50.0% |
| css3/css-transforms | 194 | 187 | 12 | 393 | 50.9% |
| css3/css-overflow | 68 | 65 | 6 | 139 | 51.1% |
| css3/css-multicol | 240 | 215 | 3 | 458 | 52.7% |
| css3/css-contain | 168 | 132 | 18 | 318 | 56.0% |
| css3/css-align | 4 | 3 | 0 | 7 | 57.1% |
| css3/mediaqueries | 36 | 25 | 2 | 63 | 59.0% |
| css3/css-grid | 43 | 29 | 0 | 72 | 59.7% |
| css3/css-tables | 67 | 42 | 3 | 112 | 61.5% |
| css3/compositing | 11 | 6 | 0 | 17 | 64.7% |
| css3/css-animations | 16 | 8 | 5 | 29 | 66.7% |
| css3/css-images | 235 | 68 | 2 | 305 | 77.6% |
| css3/css-scrollbars | 29 | 6 | 0 | 35 | 82.9% |
| css3/css-text | 21 | 4 | 0 | 25 | 84.0% |
| css3/css-position | 92 | 13 | 4 | 109 | 87.6% |
| css3/css-logical | 29 | 1 | 0 | 30 | 96.7% |
| css3/css-flexbox | 617 | 12 | 1 | 630 | 98.1% |
| css3/css-writing-modes | 780 | 1 | 9 | 790 | 99.9% |
| css3/css-box | 5 | 0 | 0 | 5 | 100.0% |
| css3/css-color-adjust | 1 | 0 | 0 | 1 | 100.0% |
| css3/css-env | 2 | 0 | 0 | 2 | 100.0% |
| css3/css-hyphens | 2 | 0 | 0 | 2 | 100.0% |
| css3/css-transitions | 6 | 0 | 0 | 6 | 100.0% |

## Biggest absolute opportunities (most failures)

1. css3/css-multicol — 215 fails
2. css3/css-transforms — 187 fails
3. css3/css-backgrounds — 183 fails
4. css3/css-color — 179 fails
5. css3/css-fonts — 174 fails
6. css3/css-text-decor — 154 fails  *(plan written)*
7. css3/css-pseudo — 146 fails  *(plan written)*
8. css3/filter-effects — 140 fails  *(SVG-gated remainder)*
9. css3/css-contain — 132 fails
10. css3/css-sizing — 109 fails

## CSS2 — all 23 regions at 100%

backgrounds (5), borders (3), box-display (8), cascade (1), colors (6),
dimension-constraints (3), display (3), floats (4), generated-content (6),
linebox (6), margin-collapse (3), margin-padding-clear (2), normal-flow (5),
opacity (3), overflow (10), positioning (6), pseudo-elements (3), sizing (3),
stacking-context (1), table (1), text (6), visudet (5), zindex (6) — **99/99 pass.**

## Coverage caveat

The runner executes only WPT tests with a `<link rel="match">` reference.
`rel="mismatch"`, `testharness.js`, and helper files are not counted — this measures
the *reftest* slice only.
