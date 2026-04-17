# Flex Round 10: Complete the Remaining 70 Failures

Current state: **559 pass / 70 fail** (88.9% pass rate) across 629 tests.

Previous rounds fixed: float/BFC interaction, font metrics, line-height:normal, formatting-interop, safe alignment, orthogonal writing-mode content sizing, min-height:min-content equivalence, ComputeMinMaxSizes clamping, and intrinsic sizing for form controls.

**All changes touch `pkg/layout/flex_layout.go` or closely related files. Work sequentially — complete each target, commit, verify no regressions (must stay >= 559 passes), then proceed.**

---

## Category A: Baseline Alignment (~14 tests, HIGH IMPACT)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| flexbox-align-self-baseline-horiz-001a | 26173 | Mixed block items, different font sizes |
| flexbox-align-self-baseline-horiz-001b | 26173 | Same test, `last baseline` variant |
| flexbox-align-self-baseline-horiz-003 | 3938 | Cross-axis margins with wrap-reverse |
| flexbox-align-self-baseline-horiz-006 | 8436 | Baseline with replaced elements |
| flexbox-align-self-baseline-horiz-008 | 28520 | Baseline with nested flex |
| flexbox-baseline-align-self-baseline-horiz-001 | 402 | Container's own baseline |
| flexbox-baseline-multi-item-horiz-001a | 355 | Multi-item baseline in row |
| flexbox-baseline-multi-item-horiz-001b | 355 | Multi-item baseline, last baseline |
| flexbox-baseline-multi-line-horiz-002 | 616 | Multi-line baseline distribution |
| flexbox-baseline-multi-line-vert-001 | 2077 | Multi-line baseline in column |
| flexbox-baseline-multi-line-vert-002 | 1847 | Multi-line baseline in column, last |
| baseline-synthesis-vert-lr-line-under | 1600 | Baseline synthesis in vertical-lr |
| fieldset-baseline-alignment | 251 | Baseline alignment with fieldset |
| flexbox-align-self-horiz-001-table | 32574 | align-self with table flex items |

### Root Cause
The current baseline alignment uses a single `maxAscent`/`maxDescent` pair per flex line. CSS Align 3 requires **two baseline sharing groups** (major and minor) per flex line. Missing: wrap-reverse baseline flipping, per-group margin handling, baseline clamping, zero-baseline guard, last-baseline support.

### What Blink Does
Study `flex_layout_algorithm.cc` — Blink's `CalculateBaselines()` and baseline sharing group logic. Key concepts:
- Major group: items whose `align-self: baseline` baseline axis matches the flex line's dominant baseline
- Minor group: items with `align-self: last baseline`
- Each group independently determines its shared baseline offset
- Items in neither group use fallback alignment (stretch/flex-start)

### Fix Location
`pkg/layout/flex_layout.go` — search for `maxAscent`, `maxDescent`, baseline-related code in the cross-axis alignment section.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline|flexbox-baseline|baseline-synthesis|fieldset-baseline|flexbox-align-self-horiz-001-table)" -v
```

---

## Category B: Aspect Ratio & Intrinsic Sizing (~9 tests, HIGH IMPACT)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| aspect-ratio-intrinsic-size-001 | 9000 | Canvas with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-002 | 9000 | Canvas with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-007 | 262328 | Img aspect-ratio sizing breakdown |
| aspect-ratio-intrinsic-size-008 | 1600 | Div with aspect-ratio in inline-flex |
| aspect-ratio-intrinsic-size-009 | 1600 | Div with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-010 | 1600 | Div with aspect-ratio + min-width |
| flex-aspect-ratio-img-column-010 | 16000 | Image aspect-ratio in column flex |
| flex-aspect-ratio-img-column-018 | 5000 | Image aspect-ratio column with constraints |
| flex-aspect-ratio-img-row-015 | 20000 | Image aspect-ratio in row flex |

### Root Cause
`useAspectRatioStretch` in `flex_layout.go` (around line 3978) only handles `<img>` elements, not arbitrary elements with CSS `aspect-ratio` property. However, generalizing to all elements previously caused a regression with `aspect-ratio-transferred-max-size` (items with `flex: 1` had main-size overridden by aspect-ratio-derived value). The fix needs to be more nuanced: only apply aspect-ratio stretch when the item does NOT have a flex-grow/shrink that already determined its main size.

Tests 008/009/010 fail due to IFC (Inline Formatting Context) vertical positioning of inline-flex elements — these are display:inline-flex containers being mispositioned on the line. Tests 007/018/015 involve SVG/image sizing with complex constraints.

### Prior Investigation Results
- Generalizing `useAspectRatioStretch` to non-replaced elements with CSS `aspect-ratio` was attempted and reverted because `aspect-ratio-transferred-max-size` regressed (flex:1 items got 50px instead of 100px flex-resolved size).
- The transferred size suggestion combination logic (min vs max for replaced vs non-replaced) in `flexItemAutoMinSize` around line 3824 was already fixed.

### Fix Location
`pkg/layout/flex_layout.go`:
- `useAspectRatioStretch` (~line 3978): needs conditional generalization
- Inline-flex vertical positioning: likely in IFC layout (`inline_layout.go`), not flex code

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/(aspect-ratio-intrinsic|flex-aspect-ratio)" -v
```

---

## Category C: Alignment & Margins (~8 tests, MEDIUM IMPACT)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| align-content-004 | 11100 | align-content space-between wrapping |
| align-content-007 | 50200 | align-content stretch fallback |
| align-self-016 | 19600 | align-self shrink-to-fit multi-line |
| auto-margins-001 | 10618 | auto margins centering |
| auto-margins-003 | 3158 | auto margins column flex |
| flexbox-align-self-horiz-002 | 235 | align-self with margin/padding/border |
| css-box-justify-content | 3159 | justify-content flex-end row |
| flexbox-safe-overflow-position-006 | 1000 | safe alignment webkit-box compat |

### Root Cause
Multiple alignment issues:
- `align-content` with wrapping: the cross-size distribution across flex lines may be wrong
- Auto margins: the free-space distribution to auto margins may have edge cases
- `align-self-016`: involves shrink-to-fit sizing on a multi-line flex container, likely a container-level sizing issue

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/(align-content-004|align-content-007|align-self-016|auto-margins-00[13]|flexbox-align-self-horiz-002|css-box-justify|flexbox-safe-overflow-position-006)" -v
```

---

## Category D: Flex-Basis Content & Definite Sizes (~8 tests, MEDIUM IMPACT)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| flexbox-flex-basis-content-002a | 7828 | flex-basis:content column sizing |
| flexbox-flex-basis-content-002b | 7828 | Same, reverse |
| flexbox-flex-basis-content-003a | 4779 | flex-basis:content max-content |
| flexbox-flex-basis-content-003b | 4779 | Same, reverse |
| flexbox-definite-sizes-003 | 5474 | nested flex max-height definite sizing |
| flexbox-definite-sizes-004 | 5474 | nested flex max-height definite sizing |
| flexbox-min-width-auto-005 | 4400 | aspect ratio min-width for images |
| flexbox-min-height-auto-001 | 72 | min-height auto (nearly passing) |

### Root Cause
- flex-basis:content tests: `itemContentMaxMainSize` may not correctly suppress the item's own CSS main-size when computing content-based sizing
- Definite sizes: nested flex containers where a max-height creates a "definite" cross-size that should propagate to children
- min-height-auto-001: only 72 pixels off, likely a sub-pixel rounding issue

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/(flexbox-flex-basis-content|flexbox-definite-sizes|flexbox-min-(width|height)-auto)" -v
```

---

## Category E: Column Flex & Wrapping (~5 tests, MEDIUM IMPACT)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| flex-minimum-height-flex-items-030 | 10000 | Items not wrapping in nested column flex |
| flex-minimum-height-flex-items-019 | 7000 | min-height with wrapped items (needs JS?) |
| flexbox-flex-wrap-vert-002 | 1140 | min-height honored in vertical multi-line |
| flexbox-mbp-horiz-004 | 8780 | percent margin/padding on flex items |
| flex-container-max-content-001 | 352 | flex container max-content sizing |
| flex-container-min-content-001 | 324 | flex container min-content sizing |

### Root Cause
- Test 030: items aren't wrapping side-by-side; rendering as single column ~25px wide instead of two 50px columns. Pre-existing column flex-wrap cross-size issue.
- Test 019: likely requires JavaScript for relayout testing.
- flex-wrap-vert-002: column wrapping with min-height constraint.
- Container intrinsic sizing (352/324 tests): how flex containers report their own min/max content sizes.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/(flex-minimum-height-flex-items-(019|030)|flexbox-flex-wrap-vert-002|flexbox-mbp-horiz-004|flex-container-(min|max)-content)" -v
```

---

## Category F: Writing Mode & Orthogonal Items (~7 tests, LOW-MEDIUM IMPACT)

### Failing Tests  
| Test | Pixels | Notes |
|------|--------|-------|
| flexbox-basic-canvas-horiz-001v | 751 | Canvas borders with vertical WM items |
| flexbox-basic-canvas-vert-001 | 84 | Text label "a b" position in case B |
| flexbox-basic-canvas-vert-001v | 84 | Same with WM on items |
| flexbox-basic-fieldset-vert-001 | 84 | Text label position |
| flexbox-basic-iframe-vert-001 | 84 | Text label position |
| flexbox-basic-img-vert-001 | 84 | Text label position |
| flexbox-basic-video-vert-001 | 84 | Text label position |

### Root Cause
- The 84-pixel failures: all share identical issue — the text "a b" in case B of the `-vert-001` tests renders with the "b" character slightly mispositioned. This is a text rendering/anonymous flex item positioning issue, not a flex algorithm bug.
- canvas-horiz-001v (751px): canvas borders render differently when `writing-mode: vertical-lr` is applied. Likely a border drawing phase issue with writing-mode, not flex sizing.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-(canvas|fieldset|iframe|img|video)-(horiz|vert)" -v
```

---

## Category G: Textarea Form Control Rendering (~2 tests, LOW IMPACT for flex)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| flexbox-basic-textarea-horiz-001 | 3005 | Textarea sizing in row flex |
| flexbox-basic-textarea-vert-001 | 6295 | Textarea sizing in column flex |

### Root Cause
Textareas render narrower than expected. The flex algorithm computes correct widths, but the textarea form control rendering adds an implicit scrollbar or extra internal element that reduces the visible content area. The reference uses static layout where this doesn't manifest. This is a textarea rendering issue, not flex.

---

## Category H: Miscellaneous (~17 tests)

### Failing Tests
| Test | Pixels | Notes |
|------|--------|-------|
| content-height-with-scrollbars | 49600 | Scrollbar height calculation |
| cross-axis-scrollbar | 38502 | Scrollbar in cross axis |
| dynamic-isize-change-004 | 15000 | JS: dynamic inline-size change |
| flex-direction-modify | 52348 | JS: dynamic flex-direction change |
| flex-direction-with-element-insert | 520 | JS: element insertion |
| flex-inline | 1224 | inline-flex display sizing |
| flexbox_align-items-stretch-3 | 7000 | stretch with orthogonal nested |
| flexbox-order-only-flexitems | 97 | order property on non-flex items |
| flexbox-paint-ordering-001 | 3676 | Paint/z-order of flex items |
| flexbox-paint-ordering-002 | 12097 | Paint order with z-index |
| flexbox-root-node-001b | 594 | Flex on root `<html>` element |
| flexbox-whitespace-handling-001a | 4800 | Whitespace anonymous flex items |
| flexbox-whitespace-handling-001b | 4800 | Same, with `pre` white-space |
| justify-content_space-between-003 | 10000 | justify-content fallback reverse |
| fit-content-item-001 | 34200 | fit-content shrink-wrap sizing |
| fixed-table-layout-with-percentage-width | 57899 | table-layout:fixed in flex item |
| flexbox-align-self-horiz-001-table | 32574 | table elements as flex items |

### Notes
- **JS tests** (dynamic-isize-change-004, flex-direction-modify, flex-direction-with-element-insert): require JavaScript execution for relayout. May not be fixable without a JS engine.
- **Scrollbar tests**: require scrollbar rendering implementation.
- **Paint ordering tests**: require z-index / paint order implementation for flex items.
- **Table-in-flex tests**: require table layout integration with flex constraints.
- **Whitespace handling**: anonymous flex item creation from whitespace text nodes.

---

## Recommended Execution Order

1. **Category D** (flex-basis:content, definite sizes, min-auto) — 8 tests, moderate complexity, good ROI. Start with `flexbox-min-height-auto-001` (72px diff, nearly passing).

2. **Category C** (alignment & margins) — 8 tests. Study Blink's auto-margin and align-content logic. `flexbox-align-self-horiz-002` (235px) may be quick.

3. **Category A** (baseline alignment) — 14 tests, highest impact but most complex. Requires baseline sharing groups rewrite. Study Blink's `CalculateBaselines()` thoroughly before coding.

4. **Category B** (aspect ratio) — 9 tests. The `useAspectRatioStretch` generalization needs careful conditional logic. Tests 008/009/010 may be IFC issues.

5. **Category E** (column flex & wrapping) — 5-6 tests. Column wrapping cross-size calculation is a deep issue.

6. **Categories F, G, H** — mostly non-flex-algorithm issues (text rendering, scrollbar rendering, JS, paint ordering, tables). Fix opportunistically.

### Test Execution
```bash
# Run specific category tests
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/<test-name>" -v

# Full regression check (must stay >= 559 passes)
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/" -v 2>&1 | grep -c "REFTEST PASS"
```

### Key Files
- `pkg/layout/flex_layout.go` (~4079 lines) — main flex algorithm
- `pkg/layout/min_max_sizing.go` — intrinsic sizing functions
- `pkg/layout/intrinsic_sizing.go` — replaced element intrinsic dimensions
- `pkg/layout/replaced_layout.go` — replaced element sizing (ComputeReplacedSize)
- `pkg/layout/fragment_geometry.go` — aspect-ratio CSS property handling
- `pkg/layout/inline_layout.go` — IFC layout (affects inline-flex positioning)
