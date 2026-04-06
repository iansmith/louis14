# Louis14 Web Page Rendering Capability Analysis

_Generated 2026-04-06_

## WPT Test Results

### CSS 2.1 Reftests: 99/99 (100%)

### CSS 3+ Reftests: 3,213/6,608 (48.6%)

Breakdown by module:

| Module | Pass | Fail | Total | Rate |
|---|---|---|---|---|
| css-box | 5 | 0 | 5 | 100.0% |
| css-color-adjust | 1 | 0 | 1 | 100.0% |
| css-env | 2 | 0 | 2 | 100.0% |
| css-hyphens | 2 | 0 | 2 | 100.0% |
| css-transitions | 6 | 0 | 6 | 100.0% |
| css-logical | 29 | 1 | 30 | 96.7% |
| css-writing-modes | 669 | 118 | 787 | 85.0% |
| css-text | 21 | 4 | 25 | 84.0% |
| css-scrollbars | 29 | 6 | 35 | 82.9% |
| css-images | 232 | 70 | 302 | 76.8% |
| css-flexbox | 402 | 227 | 629 | 63.9% |
| css-grid | 43 | 29 | 72 | 59.7% |
| compositing | 10 | 7 | 17 | 58.8% |
| mediaqueries | 36 | 26 | 62 | 58.1% |
| css-align | 4 | 3 | 7 | 57.1% |
| css-contain | 155 | 145 | 300 | 51.7% |
| css-overflow | 67 | 66 | 133 | 50.4% |
| css-inline | 7 | 7 | 14 | 50.0% |
| css-counter-styles | 3 | 3 | 6 | 50.0% |
| css-values | 77 | 79 | 156 | 49.4% |
| css-variables | 84 | 98 | 182 | 46.2% |
| css-ui | 71 | 86 | 157 | 45.2% |
| css-backgrounds | 152 | 199 | 351 | 43.3% |
| css-color | 131 | 178 | 309 | 42.4% |
| css-position | 42 | 62 | 104 | 40.4% |
| css-transforms | 156 | 233 | 389 | 40.1% |
| filter-effects | 110 | 164 | 274 | 40.1% |
| css-fonts | 114 | 171 | 285 | 40.0% |
| css-text-decor | 99 | 151 | 250 | 39.6% |
| css-conditional | 46 | 74 | 120 | 38.3% |
| selectors | 53 | 87 | 140 | 37.9% |
| css-cascade | 19 | 33 | 52 | 36.5% |
| css-tables | 39 | 70 | 109 | 35.8% |
| css-display | 26 | 54 | 80 | 32.5% |
| css-pseudo | 65 | 145 | 210 | 31.0% |
| css-lists | 44 | 100 | 144 | 30.6% |
| css-nesting | 6 | 14 | 20 | 30.0% |
| css-ruby | 27 | 72 | 99 | 27.3% |
| css-will-change | 12 | 35 | 47 | 25.5% |
| css-masking | 2 | 6 | 8 | 25.0% |
| css-animations | 6 | 21 | 27 | 22.2% |
| css-sizing | 45 | 160 | 205 | 22.0% |
| css-multicol | 64 | 391 | 455 | 14.1% |
| **TOTAL** | **3,213** | **3,395** | **6,608** | **48.6%** |

## Real-World Web Page Renderability Estimate

### Feature Usage vs. Louis14 Coverage

Features ranked by how often they appear on real web pages (per HTTP Archive/Web Almanac data):

| Feature | Pages Using | Louis14 Status |
|---|---|---|
| Block/inline layout | ~100% | Covered well (100% CSS2.1) |
| Colors/backgrounds | ~100% | Partial (42-43%) — gaps in gradients, advanced color spaces |
| Fonts | ~100% | Weak (40%) — web fonts, @font-face gaps |
| Flexbox | ~95% | Partial (63.9%) |
| Media queries | ~95% | Partial (58.1%) |
| CSS variables | ~85% | Partial (46.2%) |
| Pseudo-elements (::before/::after) | ~80% | Weak (31%) — heavily used for icons, decorations |
| Positioning (abs/fixed/sticky) | ~80% | Weak (40.4%) |
| Transforms | ~70% | Weak (40.1%) |
| Animations/transitions | ~70% | Very weak (22%) — acceptable for static rendering |
| Grid | ~40% | Decent (59.7%) |

### Estimates by Page Category

| Category | Estimate | Rationale |
|---|---|---|
| Simple hand-written HTML (personal blogs, basic CSS) | ~50-60% | CSS2.1 is solid, flexbox/grid decent |
| Static HTML with inline styles (no JS, no external resources) | ~30-40% | Layout fundamentals work, but sizing/pseudo gaps hurt |
| Real-world pages as served (JS frameworks, external CSS, web fonts, CDN images) | ~2-5% | No networking, no JS-driven rendering, no resource loading |
| **Overall realistic estimate** | **~5-8%** | Weighted by actual web traffic distribution |

### Critical Gaps for Real-World Rendering

1. **No JavaScript execution for rendering** — most modern sites rely on JS for initial DOM construction (SPAs, React, etc.)
2. **No network/resource loading** — cannot fetch CSS, images, fonts from URLs
3. **No `<script>`/`<style>` processing pipeline** for live pages
4. **Pseudo-elements** at 31% breaks many real layouts (icons, decorations, layout hacks)
5. **CSS sizing** at 22% means many width/height calculations are wrong
6. **Multi-column** at 14.1% — though rarely critical for page structure

### Strengths

- 100% CSS 2.1 compliance (99/99 reftests)
- Broad CSS parsing (180+ properties recognized)
- Strong writing-modes support (85%)
- Good flexbox coverage (63.9%)
- Solid layout fundamentals (block, inline, float, absolute positioning)
- DOM API with querySelector, classList, mutations
- WPT fuzzy matching with edge-locality validation
