# WPT Test Roadmap — Feature Gap Analysis

Date: 2026-02-19. Baseline: CSS2 99/99, CSS3 213/217.

Cross-referenced implemented features (from codebase audit) against HTTP Archive /
Web Almanac / Project Wallace data on real-world CSS usage. Features are ranked by
web usage frequency × current gap severity.

## Batch 1 Results (12 tests added)

| # | Test | Status | Error | Root Cause |
|---|------|--------|-------|------------|
| 1 | percentage-padding-001 | FAIL | 4.2% | `%` padding not implemented (returns 0) |
| 2 | calc-percent-width-001 | FAIL | 4.2% | `%` inside calc() resolves to 0 |
| 3 | nested-flex-001 | FAIL | 70.8% | Inner flex containers ignore outer flex cross-size constraint |
| 4 | flex-auto-margin-001 | PASS | 0% | Auto margins in flex working correctly |
| 5 | border-radius-clip-001 | PASS | 0% | Rounded clipping with overflow:hidden working |
| 6 | box-sizing-border-box-percent-001 | PASS | 0% | border-box + % width working |
| 7 | max-height-overflow-001 | PASS | 0% | max-height + overflow:hidden working |
| 8 | position-fixed-001 | PASS | 0% | Fixed positioning correct at scroll=0 |
| 9 | margin-auto-center-001 | PASS | 0% | margin:auto centering working |
| 10 | letter-spacing-001 | PASS | 0% | Letter spacing in layout + rendering working |
| 11 | text-overflow-ellipsis-001 | PASS | 0% | Ellipsis truncation working |
| 12 | line-height-inherit-001 | FAIL | 3.3% | Unitless line-height not being inherited from parent |

**Score: 8/12 pass. 4 failures expose real bugs to fix.**

---

## Batch 1: Implement Now (12 tests)

These target the highest-impact gaps — features used on 40–90%+ of real websites that
are either not implemented, partially broken, or lacking test coverage.

### 1. Percentage Padding (NOT IMPLEMENTED)
**Web usage:** Ubiquitous. The `padding-top: 56.25%` pattern for 16:9 aspect ratios is
on millions of pages (video embeds, responsive images).
**Gap:** `GetPadding()` calls `ParseLengthFull` which has no `%` case → returns 0.
CSS spec: all padding percentages (including top/bottom) resolve against containing
block WIDTH.
**Test:** `css-percentage-padding/percentage-padding-001.html`
**Fix location:** `pkg/css/style.go` GetPadding + `pkg/layout/layout_block.go`

### 2. calc() with Percentages (BROKEN)
**Web usage:** `calc(100% - 20px)` is the #1 most common calc() pattern. ~35% of pages.
**Gap:** `parseCalcAtom` passes viewport=0 to ParseLengthFull, so `%` inside calc
resolves to 0. Need to thread containing-block width into calc resolution.
**Test:** `css-calc-percent/calc-percent-width-001.html`
**Fix location:** `pkg/css/style.go` evalCalcExpr / parseCalcAtom

### 3. Nested Flexbox (IMPLEMENTED — needs test coverage)
**Web usage:** ~74% of pages use flexbox. Nested flex (flex-in-flex) is the dominant
modern layout pattern (navbars, card grids, form layouts).
**Gap:** Works by recursive dispatch but zero test coverage. Important to verify.
**Test:** `css-flexbox-nested/nested-flex-001.html`

### 4. Flex Auto Margins (IMPLEMENTED — needs test coverage)
**Web usage:** `margin-left: auto` in flex is the standard "push to end" pattern.
Used on most flex-based navbars and toolbars.
**Gap:** Implemented in layout_flex.go but not directly tested.
**Test:** `css-flexbox-auto-margin/flex-auto-margin-001.html`

### 5. border-radius + overflow:hidden Clipping (IMPLEMENTED — needs test)
**Web usage:** Card UI pattern. ~70%+ of modern sites use rounded containers that
clip children. `border-radius` is in the top 20 CSS properties.
**Gap:** Implemented in render.go but no WPT test verifies rounded clipping of children.
**Test:** `css-border-radius-clip/border-radius-clip-001.html`

### 6. box-sizing: border-box with Percentage Width (IMPLEMENTED — needs test)
**Web usage:** 92% of pages use `box-sizing: border-box`. Combined with `width: 50%`
for grid-like layouts, this is the foundation of most CSS frameworks.
**Gap:** No test combines border-box + percentage width + padding.
**Test:** `css-box-sizing/box-sizing-border-box-percent-001.html`

### 7. max-height with Overflow (IMPLEMENTED — needs test)
**Web usage:** Common for dropdown menus, expandable sections, scrollable regions.
**Gap:** max-height is implemented but not tested in combination with overflow.
**Test:** `css-max-height/max-height-overflow-001.html`

### 8. position:fixed (IMPLEMENTED — needs test)
**Web usage:** 82% of pages. Fixed headers, modals, floating action buttons.
At scroll=0, identical to absolute relative to viewport.
**Gap:** Implemented correctly but zero test coverage.
**Test:** `css-position-fixed/position-fixed-001.html`

### 9. Margin Auto Centering (IMPLEMENTED — needs test)
**Web usage:** `margin: 0 auto` for horizontal centering is one of the oldest and
most universal CSS patterns. On virtually every site.
**Gap:** Likely works but has no dedicated test.
**Test:** `css-margin-auto/margin-auto-center-001.html`

### 10. letter-spacing (IMPLEMENTED — needs test)
**Web usage:** Common in headings, navigation, branding. ~30% of sites.
**Gap:** Implemented in layout + rendering but no WPT test.
**Test:** `css-letter-spacing/letter-spacing-001.html`

### 11. white-space:nowrap + text-overflow:ellipsis (IMPLEMENTED — needs test)
**Web usage:** The truncated text pattern. Used on every site with dynamic content
in fixed-width containers (nav items, card titles, table cells).
**Gap:** Both features implemented separately but no test combines them.
**Test:** `css-text-overflow-ellipsis/text-overflow-ellipsis-001.html`

### 12. line-height Inheritance Bug (PARTIALLY BROKEN)
**Web usage:** `line-height` is on every page with text. The unitless-vs-percentage
inheritance distinction matters when parent and child have different font sizes.
**Gap:** Percentage/em line-height re-resolves against child font-size instead of
being inherited as a computed px value. Unitless line-height works correctly.
**Test:** `css-line-height-inherit/line-height-inherit-001.html`

---

## Batch 2: Next Round (10 tests)

### 13. vh/vw Units in Layout
**Web usage:** 72–76% of sites. `height: 100vh` for full-viewport sections.
**Test:** Hero section with `min-height: 50vh` and `width: 80vw`.

### 14. rem Units with Custom Root Font Size
**Web usage:** 80% of sites use rem. Currently hardcoded to 16px root.
**Test:** `:root { font-size: 20px }` then child with `padding: 2rem` (→ 40px).

### 15. CSS Grid: auto-flow and Implicit Tracks
**Web usage:** Grid is on 12% of pages and growing fast. Auto-placement is the
most common grid pattern.
**Test:** `grid-template-columns: repeat(3, 1fr)` with 5 items (tests auto-row).

### 16. CSS Grid: fr Units with Fixed Columns
**Web usage:** `1fr` is used on 60% of sites (Project Wallace). Sidebar + main content.
**Test:** `grid-template-columns: 200px 1fr` with colored regions.

### 17. Flex Order Property
**Web usage:** Used for responsive reordering (mobile-first patterns).
**Test:** Three items with `order: 3, 1, 2` should render in 2, 3, 1 visual order.

### 18. Multiple Box Shadows
**Web usage:** Elevation / Material Design shadows use 2–3 layers.
**Test:** Element with `box-shadow: 0 1px 3px rgba(0,0,0,.12), 0 1px 2px rgba(0,0,0,.24)`.

### 19. CSS Variables (var()) Inheritance
**Web usage:** 35–43% of pages. Variables defined on `:root`, used deep in tree.
**Test:** `--color: green` on ancestor, `color: var(--color)` on deep descendant.

### 20. background-size: cover/contain
**Web usage:** Nearly universal for hero images and responsive backgrounds.
**Test:** Element with background-image + `background-size: cover` vs equivalent.

### 21. Flex Wrap with Align-Content
**Web usage:** Multi-line flex with wrap is the standard responsive grid pattern.
**Test:** Flex container with `flex-wrap: wrap; align-content: center` and overflowing items.

### 22. Pseudo-element Counters (::before with counter())
**Web usage:** Ordered lists with custom numbering, step indicators.
**Test:** Multiple divs with `counter-increment` and `::before { content: counter(n) }`.

---

## Batch 3: Completeness (10+ tests)

### 23. display: inline-flex Sizing
### 24. overflow-wrap: break-word
### 25. Percentage Heights with Explicit Parent Height
### 26. z-index Stacking with Nested Contexts
### 27. Table with border-collapse: collapse
### 28. Multi-column with Column Break
### 29. Flexbox min-width: auto (Implicit Minimum)
### 30. CSS :not() with Complex Selectors
### 31. background-position with Keywords (center, bottom right)
### 32. Opacity on Stacking Context Children

---

## Feature Priority Matrix

| Feature | Web Usage | Implemented? | Test Coverage | Priority |
|---------|-----------|-------------|---------------|----------|
| % padding | ~70% pages | NO | None | CRITICAL |
| calc(% - px) | ~35% pages | BROKEN | None | CRITICAL |
| line-height % inherit | ~95% pages | BUG | None | HIGH |
| nested flex | ~60% pages | Yes | None | HIGH |
| flex auto margin | ~50% pages | Yes | None | HIGH |
| border-radius clip | ~70% pages | Yes | None | HIGH |
| box-sizing + % | 92% pages | Yes | None | MEDIUM |
| position: fixed | 82% pages | Yes | None | MEDIUM |
| max-height + overflow | ~50% pages | Yes | None | MEDIUM |
| margin auto center | ~80% pages | Yes | None | MEDIUM |
| letter-spacing | ~30% pages | Yes | None | MEDIUM |
| text-overflow:ellipsis | ~40% pages | Yes | None | MEDIUM |
| vh/vw units | 72–76% pages | Yes | None | MEDIUM |
| rem + custom root | 80% pages | Partial | None | MEDIUM |
| grid fr units | 60% sites | Yes | None | MEDIUM |
| var() inheritance | 35–43% pages | Yes | Partial | LOW |

---

## Data Sources

- HTTP Archive Web Almanac 2021/2022 — CSS chapter
- Project Wallace "The CSS Selection" 2026 — 100K site analysis
- State of CSS 2024 — Developer survey
- Chrome Platform Status — CSS property use counters
