---
name: Flex Baseline Target 1 — Remaining 3 Failing Tests
status: OPEN
target: All 3 tests pass at 0% diff
order: Decreasing diff count
parent: docs/PROMPT-flex-target1-remaining-5.md
---

# Continuation Prompt — 3 Remaining Flex-Baseline Failures

Close out the last 3 failing flex-baseline tests in the Target 1 work.
Issues 1 and 2 from the prior prompt
(`docs/PROMPT-flex-target1-remaining-5.md`) are landed on
`fix/flexbox-fast` (commits `3340937a` and `2b1f60bf`). Continue in
decreasing-diff order. Do NOT skip to easier ones partway through —
each category needs a complete foundational fix.

---

## PROJECT RULES (CLAUDE.md — NON-NEGOTIABLE)

### 1. Foundational correctness over quick wins
NEVER look for low-hanging fruit, near-passing tests, or easy wins.
Every fix must work for ALL cases. Don't filter tests by error
percentage or chase "nearly passing" tests. If a fix doesn't generalize
to all cases, it's the wrong fix. A point fix now will not help and
will likely make things worse later. Pick a category and solve it
completely — don't skip around looking for something "more tractable."

### 2. Study Blink BEFORE writing any code
When starting work on a new area, the FIRST step is to look at what
Blink/Chromium does. Study their abstractions, algorithms, and types.
Only then write code. Mirror their type names, algorithm structure,
and constraint-passing patterns. The louis14 codebase is modeled on
Blink's LayoutNG — keep it aligned.

### 3. All tests must pass
Do not treat small pixel diffs as acceptable. ALL tests must pass at
0% diff. A 0.5% diff is a failure just like 28%. Never dismiss
failures as "font rendering" or "anti-aliasing" — the WPT tests have
built-in fuzzy tolerances provided by the test authors specifically
to account for text rendering differences. If a test is failing, the
diff exceeds what the test author considered acceptable for rendering
variation, which means it's a real bug. Identify the systemic issue
and fix it with correct foundational code.

### 4. Test execution discipline
Do not run the full test suite or even the full section test suite
during feature work. Run only the specific tests associated with the
feature being worked on — typically 1 to 4 tests. Broader test runs
are expensive and should only happen when explicitly requested.

### 5. Operational rules
- Never use `open` to display files from agents — disrupts the user's
  screen.
- Always commit and push before launching worktree agents — worktrees
  start from HEAD, not working directory.
- Instruct agents to commit+report at each milestone, not just at the
  end.
- When running in a worktree (any directory that is NOT ~/louis14),
  commit ONLY to your worktree branch.

### 6. WPT-defect handling
For tests marked "possible WPT defect" — follow Blink's lead. If
Blink/Chromium passes the test (verify on wpt.fyi; check Edge results
when Chrome's reftest summary is missing — Edge is Chromium and is
typically the only complete data source), there's a real rendering
issue to chase. If Blink fails it too, document the wpt.fyi link and
move on. Do not patch around WPT defects.

---

## What landed in the prior session (context)

### Issue 1 — `flexbox-align-self-baseline-horiz-008` (25862 px → 0)
Implemented BaselineAccumulator with `BaselineAscent` flip and
`baseline_group` swap under wrap-reverse for sideways/vertical
writing modes. Commit `3340937a`.

### Issue 2 — `flexbox-align-self-baseline-horiz-006` (7646 px → 0)
Two foundational corrections, commit `2b1f60bf`:

1. **Orthogonal flex item baseline synthesis at block-end.** Previously
   `flex_layout.go` returned `(0, false)` for orthogonal items per a
   misreading of CSS Flexbox §8.3. Blink actually includes them with
   `FirstBaselineOrSynthesize` returning `cross_size` (block-end edge
   of the item in the container's cross-axis frame). Updated
   `resolvedFirstBaseline` / `resolvedLastBaseline` to return
   `(crossSize, true)` for orthogonal items.

2. **Same orthogonal synthesis in inline-block atomic baselines.**
   Updated two sites in `inline_layout.go` (`createLineBoxEx` and
   `computeLineMetricsEx`) so that orthogonal inline-blocks contribute
   `blockSize` as their atomic baseline rather than the layout
   result's content baseline (which lives in the child's writing-mode
   frame, not the container's).

3. **`<br>` joins text runs in flex child enumeration.** Updated
   `buildFlexChildren` so that `<br>` (Blink's `LayoutBR` is a
   `LayoutText` subclass) is grouped with adjacent text into a single
   anonymous block-level flex item. Other inline-level elements
   (`<i>`, `<span>`) remain blockified into their own flex items per
   CSS Display 3 §2.4. This is what makes the reference's
   `<div style="display:inline-flex">two<br/>lines</div>` render as
   two stacked lines (one flex item with inline content "two\nlines")
   instead of three separate flex items.

---

## Shared context & tooling

**Primary implementation file:** `pkg/layout/flex_layout.go`
(after Issues 1+2):
- Lines ~287–346: `resolvedFirstBaseline` / `resolvedLastBaseline` —
  orthogonal items now synthesize at `crossSize`.
- Lines ~1577: `crossBPStart := geom.Border.BlockStart +
  geom.Padding.BlockStart` — **STILL SUSPECT** for column containers:
  `Border.BlockStart` is the container's block-axis start, which for
  `flex-direction: column` is the **main axis**, not the cross axis.
  Investigate per Issue 3.
- Lines ~1594–1637: `baselineAxisParallel`, `itemBlockOffset`,
  `fallbackFirstBaseline`, `fallbackLastBaseline`.
- Lines ~1688–1727: accumulator loop feeding per-item fallback.

**Blink reference (cached):**
- `/tmp/blink_flex_layout_algorithm.cc` (3207 lines)
- `/tmp/blink_baseline_utils.h`
- `/tmp/blink_flex_item.h`

**Test runner:**
```
GOROOT=$HOME/sdk/go1.25.5 GOTOOLCHAIN=local \
  $HOME/sdk/go1.25.5/bin/go test ./pkg/visualtest \
  -run 'TestWPTCSS3Reftests/css-flexbox/<name>' -count=1
```

**Regression guard (run after every change before moving on):**
```
GOROOT=$HOME/sdk/go1.25.5 GOTOOLCHAIN=local \
  $HOME/sdk/go1.25.5/bin/go test ./pkg/visualtest -count=1 -run \
  'TestWPTCSS3Reftests/css-flexbox/(flexbox-baseline-motivating-three|flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-baseline-horiz-001-table|flexbox-baseline-multi-line-horiz-001|flexbox-baseline-multi-line-horiz-003|flexbox-baseline-multi-line-horiz-004|flexbox-fieldset-baseline-alignment|flexbox-baseline-multi-item-horiz-001a|flexbox-baseline-multi-item-horiz-001b|flexbox-align-self-baseline-horiz-008|flexbox-align-self-baseline-horiz-006)'
```

All 12 of these currently pass on `fix/flexbox-fast` at HEAD
(`2b1f60bf`). If any regresses, STOP and revert.

**wpt.fyi caveat:** Chrome's reftest summary on wpt.fyi is frequently
empty (`{}`); Edge (also Chromium) is the reliable data source. Use
the legacy_status `s` field — `"P"` = pass, `"F"` = fail, `""` =
not reported. Direct API form:
```
curl -s "https://wpt.fyi/api/search?label=master&label=experimental&q=<test-name>"
```

---

## Issue 3 — `flexbox-baseline-multi-line-vert-001` — 1649 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-baseline-multi-line-vert-001.html`

**Structure:**
- Three `inline-flex` containers, each `flex-direction: column;
  flex-wrap: wrap; width:40; height:40`.
- 4 items, 20px height each, wrap into 2 columns of 2 items.
- nth-child(3,4) have `visibility: hidden`.
- Items have mix of `medFont`/`bigFont`/`smallFont` classes.
- Ref uses `inline-block` layout with inner inline-blocks + `<br>` +
  `float:left`.

**Prior-session pixel analysis (DO NOT RE-DO):**
- Each test container exports a **different** baseline (measured at
  y=6, y=9, y=3 in the three containers respectively) matching the
  **first item's font ascent** (medFont=6, bigFont=9, smallFont=3).
- Ref has ALL three containers' baselines at y=17 (the natural
  line-box baseline of the surrounding inline context).
- **Mismatch:** louis14's column-flex containers export the *first
  item's* synthesized first-baseline rather than something aligned
  with the inline context's baseline.

**Blink research — required reading before any code change:**

1. **Column-flex baseline export** (`flex_layout_algorithm.cc:1780-1790`):
   ```cpp
   if (!is_column_) baseline_accumulator.AccumulateLine(...);
   ```
   Columns skip AccumulateLine entirely; they only get AccumulateItem
   calls. That means column baseline export is: first-item's first
   baseline (with synthesis fallback) at its `block_offset`.

2. **AccumulateItem for column** (`:87-101`):
   ```cpp
   void AccumulateItem(const LogicalBoxFragment& fragment,
                       const LogicalOffset& offset,
                       bool is_first_line, bool is_last_line) {
     if (is_first_line) {
       if (!first_fallback_baseline_)
         first_fallback_baseline_ =
             offset.block_offset + fragment.FirstBaselineOrSynthesize(...);
     }
     if (is_last_line) {
       last_fallback_baseline_ =
           offset.block_offset + fragment.LastBaselineOrSynthesize(...);
     }
   }
   ```
   `offset.block_offset` is the **item's offset along the container's
   block axis**. For `flex-direction: column`, the block axis IS the
   main axis — so `offset.block_offset` = item's main-axis offset.

3. **BUT:** `LogicalBoxFragment fragment(container_writing_direction,
   ...)` at `:1873` means `FirstBaselineOrSynthesize` is computed in
   the **container's** writing mode. For a horizontal-tb column
   container with horizontal-tb children, `fragment.FirstBaselineOrSynthesize`
   returns baseline along the container's block axis. The item's block
   axis matches the container's block axis, so that's the item's
   natural first baseline (font ascent from item's top).

4. **Result in Blink:** column-flex container exports
   `item.mainOffset + item.baseline` as its first baseline. For a
   column with main-axis = vertical, item 0 at mainOffset=0 with font
   ascent 6: exports baseline = 6. Which matches what louis14 does.

**So what's the discrepancy?** The REF is using `inline-block`
layout, not `inline-flex`. Inline-blocks have DIFFERENT baseline
rules:
- Per CSS 2.1 §10.8, an inline-block's baseline is the baseline of
  its **last in-flow line box**, OR (if no in-flow line box) its
  **margin box bottom edge**.
- For inline-flex, CSS Flexbox §4.2 says: baseline comes from the
  flex container's baseline export rules (AccumulateItem fallback on
  first item's baseline).

**The ref file lies** about being an equivalent rendering — it uses
inline-blocks that get different baseline treatment. **Verify against
wpt.fyi via Edge — does Chromium pass this test?** If yes, Chromium
must have some column-flex baseline handling that matches
inline-block's "last line box" rule. Look for that.

**Possible Blink quirk:** For column flex with `flex-wrap: wrap`
producing multiple columns, maybe Blink exports the **last flex
line's** first baseline? Investigate `AccumulateItem` call order
under column-wrap — if items are iterated in visual column order and
the LAST item's fallback overwrites, the exported baseline could be
different.

**Investigation steps:**
1. Read `/tmp/blink_flex_layout_algorithm.cc` lines 1960–2020 for
   the AccumulateItem call site with ALL column-wrap specifics.
2. Check if Blink distinguishes the "first column" first-baseline
   for baseline export in multi-column flex.
3. Verify Edge status on wpt.fyi for
   `flexbox-baseline-multi-line-vert-001`.

**Note on `flex_layout.go:1577`:** `crossBPStart = Border.BlockStart
+ Padding.BlockStart`. For column containers this IS the block-axis
start, which is correct for storing baselines "distance from
container block-start." Blink uses the same: `offset.block_offset`
is container-block-axis-relative. NOT a bug by itself. The bug is
likely in **which item** is picked, not in the offset.

---

## Issue 4 — `flexbox-baseline-multi-line-vert-002` — 737 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-baseline-multi-line-vert-002.html`

Same family as vert-001 — column flex multi-line baseline. Fix
vert-001 first; vert-002 likely has the same root cause with a
smaller visual manifestation. Verify after the vert-001 fix lands.

If vert-002 still fails after vert-001 passes, enumerate the
remaining differences and apply the same Blink-first research
discipline.

---

## Issue 5 — `flexbox-align-self-horiz-002` — 235 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-align-self-horiz-002.xhtml`

**Prior-session analysis (DO NOT RE-DO):**
- Three inline-flex containers with various `align-self` values.
- Third container has `.self-end.wmvertrev` with `writing-mode:
  vertical-lr; direction: rtl`.
- Test container measured **width 345px**, ref measured **width
  257px** — 88px overshoot.
- Items themselves position identically; only the container's right
  border differs.
- **NOT a baseline issue** — this is a container-sizing bug for
  inline-flex containers holding vertical-WM items.

**Blink research — required:**

1. **Inline-flex container intrinsic sizing** — for inline-level
   flex, the container's inline size is typically `max-content`
   unless constrained. See `flex_layout_algorithm.cc::ComputeMinMaxSizes`
   or `ng_flex_layout_algorithm.cc` (name may vary).

2. **Vertical-WM item inline size from parent's perspective:** a
   `vertical-lr` child inside a horizontal-tb parent contributes its
   **height** (child's inline-axis) to the parent's inline-size
   calculation. The child's `width` (child's block-axis) contributes
   to the parent's block-size. Verify louis14's intrinsic-size
   plumbing uses the right axis swap.

3. **`direction: rtl` on vertical-lr** — affects which end is the
   inline-start for text, but shouldn't affect the container's
   computed inline-size (which is based on content min/max).

**Hypothesis:** louis14's intrinsic-size computation for inline-flex
with vertical-WM children double-counts something (88px is
suspiciously close to the item's content height plus some
padding/margin).

**Investigation steps:**
1. Find the inline-flex intrinsic size code path in louis14
   (likely `flex_layout.go` near `ComputeInlineSizes` or similar;
   grep for `intrinsic` and `min_max` in flex files).
2. Add instrumentation to print the per-item contribution to
   container inline-size for this specific test case.
3. Compare against Blink's contribution formula for an orthogonal
   flex item.

**This is small but SYSTEMIC** — it's a container-sizing rule, not
a one-off adjustment. A fix here may surface other test improvements
or regressions. Run the regression guard carefully.

---

## Workflow

For each issue (in this order: vert-001 → vert-002 → horiz-002):

1. **Study** the relevant Blink code paths. Quote specific
   file:line refs in your commits/PR.
2. **Verify** against wpt.fyi (use Edge data when Chrome is empty)
   whether Chromium passes the test. If Chromium fails too,
   document the finding and move to the next issue.
3. **Hypothesize** the systemic cause — not a point patch.
4. **Implement** the fix in `pkg/layout/flex_layout.go` (or the
   appropriate intrinsic-sizing file for horiz-002).
5. **Run the single test** to verify 0% diff.
6. **Run the regression guard** (12-test command above). If any
   regresses, revert immediately.
7. **Commit** with a message referencing the specific Blink
   file:line you mirrored.
8. **Report** back with the diff before/after and the regression
   status before moving to the next issue.

---

## Success criteria

All 3 tests pass at **0% pixel diff**:
- flexbox-baseline-multi-line-vert-001
- flexbox-baseline-multi-line-vert-002
- flexbox-align-self-horiz-002

AND the 12-test regression guard continues to pass.

If a test turns out to be a genuine WPT design defect (test/ref
mismatch that Chromium also fails, verified on wpt.fyi), document
the Edge status with a wpt.fyi link and move on. Do not patch
around WPT defects.
