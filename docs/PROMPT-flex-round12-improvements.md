# Flexbox Round 12: Top 6 Improvements

Current state: **611 pass / 19 fail** across 630 css-flexbox tests (branch
`fix/flexbox-fast`, HEAD `946b8861`). The prior round closed out 12 baseline
tests; these are the remaining 19, ordered by pixel diff:

| # | Test | Diff px | % | Category |
|---|---|---|---|---|
| 1 | content-height-with-scrollbars | 69,200 | 14.4% | scrollbars |
| 2 | fixed-table-layout-with-percentage-width-in-flex-item | 57,899 | 12.1% | % in flex |
| 3 | cross-axis-scrollbar | 38,500 | 8.0% | scrollbars |
| 4 | dynamic-isize-change-004 | 15,000 | 3.1% | % in flex |
| 5 | auto-margins-001 | 7,066 | 1.5% | auto margins |
| 6 | flexbox-basic-textarea-vert-001 | 6,275 | 1.3% | form controls |
| 7 | auto-margins-003 | 3,172 | 0.7% | auto margins |
| 8 | flexbox-basic-textarea-horiz-001 | 3,005 | 0.6% | form controls |
| 9 | css-box-justify-content | 2,596 | 0.5% | justify-content |
| 10 | flexbox-safe-overflow-position-004 | 2,000 | 0.4% | safe alignment |
| 11 | flexbox-safe-overflow-position-003 | 1,800 | 0.4% | safe alignment |
| 12 | baseline-synthesis-vert-lr-line-under | 1,600 | 0.3% | baseline |
| 13 | flexbox-flex-wrap-vert-002 | 1,140 | 0.2% | min-height honoring |
| 14 | flexbox-basic-canvas-horiz-001v | 751 | 0.2% | form controls |
| 15 | flexbox-root-node-001b | 594 | 0.1% | HTML-root flex |
| 16 | align-content-007 | 200 | 0.0% | align-content fallback |
| 17 | align-self-016 | 200 | 0.0% | fit-content wrap |
| 18 | flexbox-order-only-flexitems | 97 | 0.0% | order scope |
| 19 | flex-direction-modify | 85 | 0.0% | baseline |

The user has asked for **6 targets, not 3**, because the remaining work is
mostly sequential — most of it touches `flex_layout.go` or adjacent files and
won't parallelize cleanly across agents. Work the targets in priority order
below; only delegate to a worktree agent for Target 3 (form controls) or
Target 6 (root-node/order-scope), which are meaningfully independent.

---

## Project rules (non-negotiable — repeat to any delegated agent)

These five rules come from `/Users/iansmith/louis14/CLAUDE.md`. Every step of
every target must follow them.

1. **Foundational correctness over quick wins.** NEVER look for low-hanging
   fruit, near-passing tests, or easy wins. Every fix must work for ALL cases.
   Don't filter tests by error percentage or chase "nearly passing" tests.
2. **Study Blink BEFORE writing any code.** When starting work on a new area,
   the FIRST step is to look at what Blink/Chromium does. Mirror their type
   names, algorithm structure, and constraint-passing patterns.
3. **All tests must pass at 0% diff.** A 0.5% diff is a failure just like
   28%. Exception: if Chrome fails the test on wpt.fyi (`status:"F"`), we
   accept the failure to match Blink — cite the wpt.fyi link in the commit.
4. **Test execution discipline.** Run only the 1–4 tests under active work.
   Broader runs are expensive and should only happen at commit checkpoints.
5. **Operational rules.**
   - Never use `open` to display files from agents.
   - Always commit and push before launching worktree agents.
   - Agents commit and report at each milestone, not just at the end.
   - In a worktree, commit ONLY to the worktree branch.

---

## Target 1 (FOUNDATIONAL): Percentage sizing inside flex items (~2 tests, ~72,899 px)

### Problem
A flex item's post-flexing main size is supposed to be treated as **definite**
for its descendants (CSS Flexbox §9.8). louis14 does not consistently propagate
that definiteness, so `%` padding / `%` width / `table-layout:fixed` inside a
flex item resolves against the wrong basis (or becomes indefinite and falls
back to intrinsic sizing).

### Affected tests
| Test | Diff | What it tests |
|---|---|---|
| fixed-table-layout-with-percentage-width-in-flex-item | 57,899 | `width:100%` on `display:table; table-layout:fixed` inside flex items of 1..5 siblings must resolve against the flex item's post-flexing main size |
| dynamic-isize-change-004 | 15,000 | `flex-basis:50%` + `padding-right:calc(100px - 50%)` must produce border-box 100px (not just the initial flex-basis) |

### Root cause (from code analysis)
Run:
```bash
grep -n "IsDefinite\|DefiniteMain\|postFlexMain" pkg/layout/flex_layout.go | head -30
```
Investigate:
- Whether the `ConstraintSpace` passed when re-laying-out a flex item for its
  final main size marks the main size as **definite** and the
  `PercentageResolutionSize.InlineSize` as the flex item's resolved main size
  (not the flex container's). Blink sets
  `ConstraintSpaceBuilder::SetIsFixedInlineSize(true)` and
  `SetPercentageResolutionInlineSize(resolved_main_size)` in the final item
  layout pass (`flex_layout_algorithm.cc :: LayoutAndPlaceItem`).
- Whether tables inside flex items correctly use that definite flex-item
  inline-size as their `100%` basis. Look at `pkg/layout/table_layout.go` for
  how `table-layout: fixed` resolves `width: 100%`.

### What Blink does
- `NGFlexLayoutAlgorithm::LayoutAndPlaceItem` (blink/renderer/core/layout/flex/) constructs the final child constraint space with:
  - `SetAvailableSize({main_size, cross_size})`
  - `SetIsFixedInlineSize(true)` — marks main-axis as fixed
  - `SetPercentageResolutionInlineSize(main_size)` — percentage descendants resolve against resolved main
- `NGBlockLayoutAlgorithm::ComputeInlineSizeForFragment` for table items: a table with `width:100%` and a definite containing block resolves to that 100%.

### Fix location
- `pkg/layout/flex_layout.go`: find the per-item final layout call site (search
  for `layoutFlexItem` / `LayoutFlexItem` / `ConstraintSpaceBuilder` inside the
  main flex loop). Ensure:
  - `SetIsFixed*Size(true)` is set for the resolved main axis.
  - `SetPercentageResolutionInlineSize` is set to the resolved main size (for
    a row flex) or `SetPercentageResolutionBlockSize` (for column).
- `pkg/layout/min_max_sizing.go`: the intrinsic contribution path may also
  need to mark the flex-item's main as fixed when the flex line is
  fully resolved.

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(fixed-table-layout-with-percentage-width-in-flex-item|dynamic-isize-change-004)' \
  -count=1 -v
```
Expect both at 0 diff. Then run the 12-test baseline regression guard from
`PROMPT-flex-target1-remaining-3.md` to confirm no regressions.

---

## Target 2: Scrollbar gutter reservation in flex containers (~2 tests, ~107,700 px)

### Problem
When a flex container has `overflow: scroll`, the classic scrollbar occupies
layout space. The flex container's **content box** excludes the scrollbar
gutter, so:
1. Percentage main/cross sizes on flex items must resolve against the content
   box (excluding scrollbar).
2. The scrollbar must be placed on the correct axis based on `flex-direction`
   and `writing-mode`.

louis14's scrollbar sizing code probably works for block containers but the
flex path doesn't subtract scrollbar space before resolving `%` children — the
flex item ends up larger than the content box and overflow appears.

### Affected tests
| Test | Diff | What it tests |
|---|---|---|
| content-height-with-scrollbars | 69,200 | flex item with `height:100%` in a `height:100px; overflow:scroll` column flex — must not scroll |
| cross-axis-scrollbar | 38,500 | Scrollbar placement under `row` vs `column` direction across LTR / vertical-rl / vertical-lr writing modes |

### Root cause (from code analysis)
Run:
```bash
grep -rn "Scrollbar\|scrollbar" pkg/layout/flex_layout.go pkg/layout/constraint_space.go pkg/render/ | head -20
```
Investigate:
- Whether the container's available content size passed to each flex item's
  constraint space has scrollbar width subtracted.
- Whether the scrollbar gutter is applied to the correct physical edge for
  each `flex-direction` × `writing-mode` combo.

### What Blink does
- `LayoutBox::ScrollbarGutter` and `NGConstraintSpaceBuilder` subtract
  scrollbar size when building the child constraint space. The scrollbar is
  placed on the inline-end (for horizontal-tb LTR) or block-end depending on
  axis.
- `NGFlexLayoutAlgorithm::Layout` subtracts scrollbar from
  `border_scrollbar_padding_` before distributing flex space.
- For cross-axis-scrollbar: the scrollbar should appear on the overflow axis,
  which is determined by `flex-direction` (main) and `writing-mode`.

### Fix location
- `pkg/layout/flex_layout.go`: where the container main/cross content-box is
  computed for item sizing, subtract `ScrollbarWidth` from the overflowing axis.
- `pkg/render/scrollbar.go` (or similar): ensure scrollbar placement follows
  flex main/cross, not just block-direction.

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(content-height-with-scrollbars|cross-axis-scrollbar)' \
  -count=1 -v
```
Also spot-check `flexbox-basic-vert-*` tests if scrollbar sizing changes.

---

## Target 3 (DELEGATABLE): Form controls as flex items (~3 tests, ~10,031 px)

### Problem
`<textarea>` and `<canvas>` as flex items have specific intrinsic-size rules
(CSS Sizing 3 "replaced element default sizes"). They should produce a UA-
default size when no width/height is given, and that default size must feed
the flex intrinsic algorithm correctly — particularly in vertical flex
containers and for vertical-WM children.

### Affected tests
| Test | Diff | What it tests |
|---|---|---|
| flexbox-basic-textarea-vert-001 | 6,275 | `<textarea/>` as flex items in `flex-direction:column`, `justify-content:space-between` |
| flexbox-basic-textarea-horiz-001 | 3,005 | `<textarea/>` as flex items in row flex |
| flexbox-basic-canvas-horiz-001v | 751 | `<canvas>` with `writing-mode:vertical-*` as flex item in row flex |

### Root cause (from code analysis)
Run:
```bash
grep -rn "textarea\|Textarea\|HTMLTextarea" pkg/layout/ pkg/render/
grep -rn "canvas\|Canvas\|HTMLCanvas" pkg/layout/ pkg/render/ | head -20
```
Investigate:
- Do textarea/canvas report UA-default intrinsic sizes? (Textarea: ~20
  cols × 2 rows in `ch`/`em`. Canvas: 300 × 150 px.)
- Does the flex intrinsic path call the right code path for replaced/
  form-control elements?

### What Blink does
- `HTMLTextAreaElement::DefaultToolTip` / `CreateLayoutObject` — textarea
  default size: 20 cols × 2 rows, converted to px via font metrics.
- `HTMLCanvasElement` — intrinsic size 300×150 (from `canvas-size` spec).
- `NGBlockLayoutAlgorithm` treats these as replaced-like: their intrinsic
  contribution is the default size, honored through the flex intrinsic pass.

### Fix location
- `pkg/layout/form_controls.go` (if exists) or `pkg/layout/replaced.go`:
  confirm textarea/canvas report default sizes.
- `pkg/layout/flex_layout.go` / `pkg/layout/min_max_sizing.go`: ensure the
  replaced-element default size feeds `measureFlexMinMax`'s intrinsic calc.

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-basic-textarea-horiz-001|flexbox-basic-textarea-vert-001|flexbox-basic-canvas-horiz-001v)' \
  -count=1 -v
```

**Delegatability:** this target is isolated in form-control / replaced-element
code — a worktree agent could take it in parallel with Targets 1/2/4/5.

---

## Target 4: Auto margins in flex — calc() and column flow (~2 tests, ~10,238 px)

### Status update (2026-04-18)
`auto-margins-001`'s computed-style assertions flipped from FAIL to PASS
after commit `ed0aada8` ("Propagate flex auto-margin resolution to fragment
margins"); pixel diff dropped from 1491 → 1056 (remaining diff is
pre-existing vertical-rl text rendering, not a flex bug).


### Problem
CSS Flexbox §9.6 ("Align with auto margins"): an `auto` margin on a flex item
absorbs free space. louis14's auto-margin path (`getItemAutoMargins` in
`flex_layout.go`) likely does not handle:
- `auto-margins-001`: a flex container sized with `calc(100% - 4em)` whose
  children also have `margin:auto`. Nested concentric `display:flex` circles.
- `auto-margins-003`: column flex with `margin: 0 auto` (auto on cross axis
  only) — should center items horizontally. Includes a vertical-lr variant.

### Affected tests
| Test | Diff | What it tests |
|---|---|---|
| auto-margins-001 | 7,066 | auto margins inside nested `calc(100% - 4em)` flex containers with `border-radius:50%` (concentric circles) |
| auto-margins-003 | 3,172 | column flex: `margin: 0 auto` centers on cross (horizontal) axis; plus vertical-lr writing mode variant |

### Root cause (from code analysis)
Run:
```bash
grep -n "getItemAutoMargins\|auto margin" pkg/layout/flex_layout.go | head -20
```
Investigate:
- When `margin: 0 auto` resolves in writing-mode vertical-lr: the
  horizontal-axis margins become **block-axis** margins (top/bottom in logical
  terms). Our code may apply them on the wrong axis.
- Whether the auto-margin absorption step runs AFTER percentage resolution
  against the (possibly calc'd) container size.
- Whether `calc(100% - 4em)` propagates correctly from a parent flex item to a
  nested flex container — auto-margins-001 nests 3 flex containers.

### What Blink does
- `NGFlexLayoutAlgorithm::AlignFlexItem` in
  `flex_layout_algorithm.cc` — after main-axis justification, iterate items
  with auto margins on the cross axis and absorb free space per item.
- CSS Box 4 §8: logical vs physical margin mapping must respect writing-mode.

### Fix location
- `pkg/layout/flex_layout.go`: auto-margin cross-axis absorption; verify
  logical→physical mapping for vertical-* writing modes.

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(auto-margins-001|auto-margins-003)' \
  -count=1 -v
```

---

## Target 5: `safe` alignment keyword in reverse flex (~2 tests, ~3,800 px)

### Problem
CSS Align 3 §5.3: `safe flex-start` means "use flex-start alignment, but if it
causes overflow past the end edge, align to the start edge instead". louis14's
implementation of `safe flex-start` for `row-reverse` and `column-reverse`
aligns the item at the wrong edge — item ends up off-screen to the
start instead of overflowing past the end edge.

### Affected tests
| Test | Diff | What it tests |
|---|---|---|
| flexbox-safe-overflow-position-003 | 1,800 | `flex-flow:row-reverse`, `align-content:safe flex-start`, `justify-content:safe flex-start`: 100px item in 90px container overflows past BOTTOM/RIGHT |
| flexbox-safe-overflow-position-004 | 2,000 | Same but `flex-flow:column-reverse` |

### Root cause (from code analysis)
Run:
```bash
grep -n "safe\|Safe" pkg/layout/flex_layout.go pkg/css/*.go | head -20
```
Investigate:
- Does the parser recognize `safe flex-start` as two-keyword
  `<overflow-position> <self-position>`?
- In the alignment step, if `safe` is set and the item overflows, louis14
  probably falls back to `start`. But "start" in the writing-mode-relative
  sense, not the reverse-flow sense — for `row-reverse`, `flex-start` is the
  right edge, but `start` (per CSS Align) is the left edge of the writing
  mode. These differ.
- The fallback in safe-overflow should preserve the reverse flow (item still
  overflows at right/bottom) but place the item at the CONTAINER start edge
  (left/top), not flip the reverse.

### What Blink does
- `ComputeContentDistributionStartPoint` /
  `ResolvedSelfAlignment` in `third_party/blink/renderer/core/style/` and
  related flex alignment code: `safe` falls back to `start` (the physical/
  writing-mode start, which for `-reverse` directions is on the OPPOSITE side
  of flex-start).
- The item overflows past `flex-end` which is bottom-or-right for reverse.

### Fix location
- `pkg/layout/flex_layout.go`: alignment / justify-content application for
  overflow-fallback. Look for `flex-start` placement logic and wire up the
  `safe` keyword override.

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-safe-overflow-position-00[34]' \
  -count=1 -v
```

---

## Target 6: Remaining layout correctness bugs (~8 tests, ~6,672 px)

### Problem
Eight small-diff tests appear independent:
- Baseline synthesis for `vertical-lr`/`sideways` items: `block-start` edge is
  the correct baseline (not `block-end`).
- `order` should only affect flex items, not block children.
- Flex on the root `<html>` element with an implicit `<body>`.
- Vertical flex-wrap honoring `min-height`.
- `align-content:stretch` fallback.
- Shrink-to-fit item in multi-line column.
- `justify-content:flex-end` with interleaved whitespace text nodes.
- Flex-direction after JS mutation (reftest is static, so initial state only).

These are likely small point fixes but should each be verified as a real bug,
not a WPT defect. Some may share root cause — verify before splitting.

### Affected tests
| Test | Diff | Note |
|---|---|---|
| baseline-synthesis-vert-lr-line-under | 1,600 | vertical-lr + sideways: block-start edge is line-under baseline |
| flexbox-flex-wrap-vert-002 | 1,140 | column-wrap flex: min-height must bound block-size |
| flexbox-root-node-001b | 594 | `<html style="display:flex">` with implicit body |
| css-box-justify-content | 2,596 | `justify-content:flex-end` with `&nbsp;` text nodes between items |
| align-content-007 | 200 | `align-content:stretch` falls back to `flex-start`, not `safe flex-start` |
| align-self-016 | 200 | `align-self:start` + fit-content in column-wrap |
| flexbox-order-only-flexitems | 97 | `order` must not reorder non-flex-item children |
| flex-direction-modify | 85 | static reftest (JS ignored) — initial flex-direction:row must match ref |

### Approach
For each test:
1. Render headless, diff vs ref, identify the single pixel cluster that
   differs.
2. Read the HTML + ref + CSS spec link.
3. Confirm against wpt.fyi that Chrome/Edge passes (if not — document as WPT
   defect).
4. Fix or group by root cause.

Likely groupings:
- `baseline-synthesis-vert-lr-line-under` continues the Target-1 baseline
  work from the prior round (Round 11's `flexbox-baseline-multi-line-vert-*`
  landed on `da0713bc`/`946b8861`). Sideways `vertical-lr` items need a
  **line-under (block-start)** baseline, not a synthesized content-box.
- `flexbox-order-only-flexitems` and `flex-direction-modify` both test that
  non-flex properties don't leak into block layout — likely the same CSS
  parsing/cascade bug.
- `align-content-007` is a one-line spec change: `stretch` falls back to
  `flex-start` (unsafe), not `safe flex-start`.

### Fix location
Distributed across `pkg/layout/flex_layout.go`,
`pkg/layout/inline_layout.go` (whitespace text nodes as anonymous flex
items), `pkg/css/` (parse `align-content:stretch` fallback), and possibly
`pkg/dom/` (root-element flex).

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(baseline-synthesis-vert-lr-line-under|flexbox-flex-wrap-vert-002|flexbox-root-node-001b|css-box-justify-content|align-content-007|align-self-016|flexbox-order-only-flexitems|flex-direction-modify)' \
  -count=1 -v
```

**Delegatability:** the narrower subparts (root-node, order-scope,
flex-direction-modify) could be worktree-agented independently if the user
wants parallelism; the rest should stay sequential.

---

## Suggested execution order

Work the targets in decreasing foundational weight:

1. **Target 1** (% sizing) — biggest systemic fix; propagation of definite
   flex-item sizes affects many downstream sites.
2. **Target 2** (scrollbars) — second-biggest diff; subtract scrollbar before
   distributing flex space.
3. **Target 4** (auto margins) — §9.6 algorithm needs to handle `calc()` and
   cross-axis column flow.
4. **Target 5** (safe keyword) — narrow but touches the same alignment code
   as Target 4; handle after to avoid conflicts.
5. **Target 3** (form controls) — parallelizable worktree candidate while
   any of 4/5 is landing.
6. **Target 6** (remaining 8) — grab-bag after the foundational work lands.
   Some may auto-fix from Target 1's definite-sizing propagation.

After each target, run the 12-test baseline regression guard:
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-baseline-motivating-three|flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-baseline-horiz-001-table|flexbox-baseline-multi-line-horiz-001|flexbox-baseline-multi-line-horiz-003|flexbox-baseline-multi-line-horiz-004|flexbox-fieldset-baseline-alignment|flexbox-baseline-multi-item-horiz-001a|flexbox-baseline-multi-item-horiz-001b|flexbox-align-self-baseline-horiz-008|flexbox-align-self-baseline-horiz-006|flexbox-baseline-multi-line-vert-001|flexbox-baseline-multi-line-vert-002|flexbox-align-self-horiz-002)' \
  -count=1
```
All 12 must pass at 0 diff after every target.

---

## Independence check

| | flex_layout.go | min_max_sizing.go | scrollbar | form_controls.go | css/parsing | dom/root |
|---|---|---|---|---|---|---|
| 1. % sizing | ✔ | ✔ | - | - | - | - |
| 2. scrollbars | ✔ (small) | - | ✔ | - | - | - |
| 3. form controls | ✔ (small) | ✔ (small) | - | ✔ | - | - |
| 4. auto margins | ✔ | - | - | - | - | - |
| 5. safe keyword | ✔ | - | - | - | ✔ (small) | - |
| 6. grab bag | ✔ | - | - | - | ✔ | ✔ |

Targets 1, 2, 4, 5, 6 all touch `flex_layout.go` to varying degrees — they
are NOT safe to run as parallel worktree agents. Target 3 (form controls) is
the cleanest isolate and is the best candidate for parallel delegation.

## Success criteria

All 19 currently-failing flex tests pass at 0% pixel diff, AND the 12-test
baseline regression guard continues to pass. Total: **630/630 css-flexbox
tests passing**.

If any test turns out to be a genuine WPT defect (test/ref mismatch that
Chromium also fails, verified on wpt.fyi), document the Edge status with a
wpt.fyi link and skip. Do not patch around WPT defects.

---

## Remaining Target 6 failures (deferred — text-rendering, not flex layout)

These three failures are NOT flex layout bugs. They all stem from text
measurement/shaping — specifically how text is measured when it crosses
an element boundary (anonymous flex item split, span boundary, etc.).
Fixing them requires work in `pkg/text/` and `pkg/layout/inline_layout.go`,
not in `flex_layout.go`. Leaving them for a dedicated text-rendering round.

- **css-box-justify-content** (2596 px) — `&nbsp;` anonymous flex item
  width vs inline-block space width. In the test, `&nbsp;` between flex
  item divs becomes an anon flex item at ~8 px wide; in the ref (inline-
  block layout), the whitespace between inline-blocks collapses to ~4 px.
  Fix requires making anon flex item text measurement agree with inline
  text measurement for the same glyph run.
- **flexbox-order-only-flexitems** (97 px) — text shaping across span
  boundaries. The test has three sibling `<span>` elements with the same
  style; the ref expects the shaping to be continuous (as if all three
  spans were one text run). Fix requires text-shaper to merge adjacent
  runs with equivalent style.
- **flex-direction-modify** (85 px) — same root cause as above. The test
  has `<h1>flex-direction:<span id="current_direction">row</h1>` (text
  split across an inline element); the ref is a single continuous text
  node. Shaping mismatch at the `:` / `r` boundary (kerning, maybe
  ligature).
