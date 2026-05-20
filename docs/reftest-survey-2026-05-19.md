# WPT Reftest Survey — Passing Tests by Region

**Date:** 2026-05-19
**Branch:** `master` (HEAD `75cfdcfb`)
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

Per-region tallies are aggregated by the first path component under each testdata
root. **SKIP** = `flags="dom"` (needs JS/DOM scripting) or no usable reference file —
not applicable to a static renderer, not failures.

## Changes since the a2de9e3f baseline

This snapshot is at `75cfdcfb` — after landing LOU-130 (external-URL filter fetch /
SVGDocumentFetcher), LOU-133 (WPT helpers + createElementNS), LOU-134 (filter dispatch
on hidden/empty-bbox), LOU-135 (chained `url()` filters), LOU-136 (inline-style
declaration splitting + attribute entity decoding), LOU-137/137 v2 (parse-time URL
resolution via `Style.BaseDir`), LOU-138 phases 1–8 (URL/baseDir/ParserContext
refactor), and the css-ruby / css-lists / css-text-decor Phase 1 batches.

**CSS3: 3952 → 4052 passing, net +100.** CSS2 unchanged at 99/99. The CSS3 corpus
itself grew by 3 reftests (6719 → 6722).

### Gains
| Region | Before | After | Δ |
|--------|-------:|------:|--:|
| filter-effects | 130 | 184 | **+54** |
| css-lists | 44 | 77 | **+33** |
| css-pseudo | 64 | 74 | **+10** |
| css-color | 130 | 137 | +7 |
| css-contain | 168 | 174 | +6 |
| css-masking | 4 | 8 | +4 |
| css-counter-styles | 2 | 5 | +3 |
| css-transforms | 194 | 196 | +2 |
| css-backgrounds | 167 | 168 | +1 |
| css-overflow | 68 | 69 | +1 |
| css-display | 27 | 28 | +1 |
| css-will-change | 17 | 18 | +1 |
| css-animations | 16 | 17 | +1 |
| compositing | 11 | 12 | +1 |

### ⚠️ Regressions — to investigate
| Region | Before | After | Δ |
|--------|-------:|------:|--:|
| css-text-decor | 96 | 80 | **−16** |
| css-multicol | 240 | 236 | **−4** |
| css-conditional | 49 | 47 | **−2** |
| css-ruby | 24 | 23 | **−1** |
| css-cascade | 18 | 17 | **−1** |
| css-sizing | 96 | 95 | **−1** |

The big one is css-text-decor: Phase 1 (`AppliedTextDecoration` model, commit
`e85efa44` → merge `c7d252d4`) gated on 4 of 5 named tests at 0% diff but the
broader region net-regressed by 16. Adjacent text-decoration tests are now failing
that previously passed. The Phase 1 gate did not catch this — needs root-causing
before further phases land.

The other regressions are smaller but unexplained. css-multicol −4 may stem from
the LOU-138 baseDir refactor touching style-block parsing; css-ruby −1 is the
inverse of Phase 1's intent (UA styles + display model landed in `3bed3458` /
merge `fa21520e`). Each warrants a quick `git bisect` against the prior raw.

## Headline

| Suite | Pass | Fail | Skip | Run (P+F) | Pass rate |
|-------|-----:|-----:|-----:|----------:|----------:|
| **CSS2** | 99 | 0 | 0 | 99 | **100.0%** |
| **CSS3** | 4,052 | 2,530 | 140 | 6,582 | **61.6%** |
| **Total** | 4,151 | 2,530 | 140 | 6,681 | **62.1%** |

CSS2 remains complete. CSS3 is the remaining surface — 2,530 failing reftests
across 37 regions with any failures.

## Regions we may need to *consider*

**No region is at zero passes.** The lowest pass rates (genuine engine work, except
as noted):

| Region | Pass | Fail | Skip | Pass rate | Note |
|--------|-----:|-----:|-----:|----------:|------|
| css3/css-ruby | 23 | 76 | 0 | 23.2% | ruby layout largely unbuilt — Phase 1 landed, Phases 2+ remain (`plan-css-ruby.md`) |
| css3/css-text-decor | 80 | 170 | 0 | 32.0% | regressed −16 — Phase 1 introduced fallout (see above) |
| css3/css-cascade | 17 | 35 | 1 | 32.7% | plan exists; regressed −1 |
| css3/css-display | 28 | 52 | 1 | 35.0% | no plan yet |
| css3/css-pseudo | 74 | 136 | 8 | 35.2% | plan exists; +10 since baseline |
| css3/css-will-change | 18 | 29 | 0 | 38.3% | plan exists, only partially implemented |

`css-lists` has moved out of this group — 30.6% → **53.5%** after the counter
scope tree (LOU-128 Phase 1 / B1) landed. `css-counter-styles` is now 83.3% (was
33.3%) but only on 6 tests.

## CSS3 — all regions, worst pass rate first

| Region | Pass | Fail | Skip | Run | Pass rate |
|--------|-----:|-----:|-----:|----:|----------:|
| css3/css-ruby | 23 | 76 | 0 | 99 | 23.2% |
| css3/css-text-decor | 80 | 170 | 0 | 250 | 32.0% |
| css3/css-cascade | 17 | 35 | 1 | 52 | 32.7% |
| css3/css-display | 28 | 52 | 1 | 80 | 35.0% |
| css3/css-pseudo | 74 | 136 | 8 | 210 | 35.2% |
| css3/css-will-change | 18 | 29 | 0 | 47 | 38.3% |
| css3/css-fonts | 111 | 174 | 2 | 285 | 38.9% |
| css3/css-conditional | 47 | 73 | 0 | 120 | 39.2% |
| css3/css-nesting | 8 | 12 | 0 | 20 | 40.0% |
| css3/selectors | 55 | 79 | 18 | 134 | 41.0% |
| css3/css-color | 137 | 172 | 1 | 309 | 44.3% |
| css3/css-sizing | 95 | 110 | 3 | 205 | 46.3% |
| css3/css-variables | 85 | 97 | 3 | 182 | 46.7% |
| css3/css-backgrounds | 168 | 182 | 5 | 350 | 48.0% |
| css3/css-values | 75 | 81 | 0 | 156 | 48.1% |
| css3/css-ui | 77 | 79 | 16 | 156 | 49.4% |
| css3/css-inline | 7 | 7 | 0 | 14 | 50.0% |
| css3/css-transforms | 196 | 185 | 12 | 381 | 51.4% |
| css3/css-multicol | 236 | 219 | 3 | 455 | 51.9% |
| css3/css-overflow | 69 | 64 | 6 | 133 | 51.9% |
| css3/css-lists | 77 | 67 | 11 | 144 | 53.5% |
| css3/css-align | 4 | 3 | 0 | 7 | 57.1% |
| css3/css-contain | 174 | 126 | 18 | 300 | 58.0% |
| css3/mediaqueries | 36 | 25 | 2 | 61 | 59.0% |
| css3/css-grid | 43 | 29 | 0 | 72 | 59.7% |
| css3/css-tables | 67 | 42 | 3 | 109 | 61.5% |
| css3/filter-effects | 184 | 88 | 6 | 272 | 67.6% |
| css3/compositing | 12 | 5 | 0 | 17 | 70.6% |
| css3/css-animations | 17 | 7 | 5 | 24 | 70.8% |
| css3/css-images | 235 | 68 | 2 | 303 | 77.6% |
| css3/css-scrollbars | 29 | 6 | 0 | 35 | 82.9% |
| css3/css-counter-styles | 5 | 1 | 0 | 6 | 83.3% |
| css3/css-text | 21 | 4 | 0 | 25 | 84.0% |
| css3/css-position | 92 | 13 | 4 | 105 | 87.6% |
| css3/css-logical | 29 | 1 | 0 | 30 | 96.7% |
| css3/css-flexbox | 617 | 12 | 1 | 629 | 98.1% |
| css3/css-writing-modes | 780 | 1 | 9 | 781 | 99.9% |
| css3/css-box | 5 | 0 | 0 | 5 | 100.0% |
| css3/css-color-adjust | 1 | 0 | 0 | 1 | 100.0% |
| css3/css-env | 2 | 0 | 0 | 2 | 100.0% |
| css3/css-hyphens | 2 | 0 | 0 | 2 | 100.0% |
| css3/css-masking | 8 | 0 | 0 | 8 | 100.0% |
| css3/css-transitions | 6 | 0 | 0 | 6 | 100.0% |

`css-masking` is now at 100% (was 50%) — all 8 mask-mode tests pass.

## Biggest absolute opportunities (most failures)

1. css3/css-multicol — 219 fails
2. css3/css-transforms — 185 fails
3. css3/css-backgrounds — 182 fails
4. css3/css-fonts — 174 fails
5. css3/css-color — 172 fails
6. css3/css-text-decor — 170 fails  *(plan written; Phase 1 regressed; needs root-cause)*
7. css3/css-pseudo — 136 fails  *(plan written; gained +10)*
8. css3/css-contain — 126 fails
9. css3/css-sizing — 110 fails
10. css3/css-variables — 97 fails

`filter-effects` has dropped out of the top 10 (140 → 88 fails) after the LOU-130
/ LOU-134 / LOU-135 work landed.

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
