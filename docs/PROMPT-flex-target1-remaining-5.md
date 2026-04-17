---
name: Flex Baseline Target 1 — Remaining 5 Failing Tests
status: OPEN
target: All 5 tests pass at 0% diff
order: Decreasing diff count
---

# Continuation Prompt — 5 Remaining Flex-Baseline Failures

Close out the last 5 failing flex-baseline tests in the Target 1 work. Work them
in decreasing-diff order so the biggest failures get real attention first, but
do NOT skip to "easier" ones partway through — each category needs a complete
foundational fix.

---

## PROJECT RULES (CLAUDE.md — NON-NEGOTIABLE)

### 1. Foundational correctness over quick wins
NEVER look for low-hanging fruit, near-passing tests, or easy wins. Every fix
must work for ALL cases. Don't filter tests by error percentage or chase
"nearly passing" tests. If a fix doesn't generalize to all cases, it's the
wrong fix. A point fix now will not help and will likely make things worse
later. Pick a category and solve it completely — don't skip around looking for
something "more tractable."

### 2. Study Blink BEFORE writing any code
When starting work on a new area, the FIRST step is to look at what
Blink/Chromium does. Study their abstractions, algorithms, and types. Only
then write code. Mirror their type names, algorithm structure, and
constraint-passing patterns. The louis14 codebase is modeled on Blink's
LayoutNG — keep it aligned.

### 3. All tests must pass
Do not treat small pixel diffs as acceptable. ALL tests must pass at 0% diff.
A 0.5% diff is a failure just like 28%. Never dismiss failures as "font
rendering" or "anti-aliasing" — the WPT tests have built-in fuzzy tolerances
provided by the test authors specifically to account for text rendering
differences. If a test is failing, the diff exceeds what the test author
considered acceptable for rendering variation, which means it's a real bug.
Identify the systemic issue and fix it with correct foundational code.

### 4. Test execution discipline
Do not run the full test suite or even the full section test suite during
feature work. Run only the specific tests associated with the feature being
worked on — typically 1 to 4 tests. Broader test runs are expensive and
should only happen when explicitly requested.

### 5. Operational rules
- Never use `open` to display files from agents — disrupts the user's screen.
- Always commit and push before launching worktree agents — worktrees start
  from HEAD, not working directory.
- Instruct agents to commit+report at each milestone, not just at the end.
- When running in a worktree (any directory that is NOT ~/louis14), commit
  ONLY to your worktree branch.

---

## Shared context & tooling

**Primary implementation file:** `pkg/layout/flex_layout.go`
- Lines 290–343: `resolvedFirstBaseline` / `resolvedLastBaseline` — orthogonal
  items already excluded (return `(0, false)` when verticality differs).
- Lines 808–810: `baselineParallel` / `canSynthesizeRow` gate per-item cross
  sizing.
- Line 1577: `crossBPStart := geom.Border.BlockStart + geom.Padding.BlockStart`
  — **SUSPECT** for column containers: `Border.BlockStart` is the container's
  block-axis start, which for `flex-direction: column` is the **main axis**,
  not the cross axis. Blink's offset is the item's `offset.block_offset` in
  container coordinates, which is legitimate; but the *accumulator* stores
  baselines as distances from the container's **block-start** edge (so they
  reverse under wrap-reverse via `BlockSize() - baseline`). Investigate
  whether column containers should use `Border.BlockStart +
  Padding.BlockStart` directly (already block-axis) and whether
  `itemBlockOffset` (`mainOffset` for column) is the right second operand.
- Lines 1594–1637: `baselineAxisParallel`, `itemBlockOffset`,
  `fallbackFirstBaseline`, `fallbackLastBaseline`.
- Lines 1688–1727: accumulator loop feeding per-item fallback.

**Blink reference (cached):**
- `/tmp/blink_flex_layout_algorithm.cc` (3207 lines)
- `/tmp/blink_baseline_utils.h`
- `/tmp/blink_flex_item.h`

**Test runner:**
```
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestVisualReftests/<name>' -count=1
```

**Regression guard (run after every change before moving on):**
```
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -count=1 -run \
'TestVisualReftests/(flexbox-baseline-motivating-three|flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-baseline-horiz-001-table|flexbox-baseline-multi-line-horiz-001|flexbox-baseline-multi-line-horiz-003|flexbox-baseline-multi-line-horiz-004|flexbox-fieldset-baseline-alignment|flexbox-baseline-multi-item-horiz-001a|flexbox-baseline-multi-item-horiz-001b)'
```

All 10 of these currently pass. If any regresses, STOP and revert.

---

## Issue 1 — `flexbox-align-self-baseline-horiz-008` — 25862 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-align-self-baseline-horiz-008.xhtml`

**Structure:**
- Two flex containers, each `display:flex; writing-mode: sideways-rl;
  width:80px; font:14px sans-serif; border:1px dashed blue;`
- Second container has `.reverse { flex-flow: row wrap-reverse; }`
- Items:
  - lime `base` (align-self: baseline)
  - yellow `lastbase` (align-self: last baseline)
  - orange `.offset lastbase` with two text lines ("two\nlines and offset")
  - pink `.offset base`
- `.offset { margin-right:10px; margin-left:3px; }`

**What it tests:** wrap-reverse + sideways-rl writing mode + mixed
baseline/last-baseline alignment. Biggest failing test by far (25862 px) —
almost certainly a systemic issue with one of: sideways writing-mode baseline
synthesis, wrap-reverse line ordering under sideways-rl, or margin-box
baseline computation when margins are on the inline axis of a vertical WM.

**Blink research — required reading before any code change:**

1. **BaselineAscent wrap-reverse flip** (`flex_layout_algorithm.cc:370-393`):
   ```cpp
   LayoutUnit BaselineAscent(const FlexItem& item, LayoutUnit ascent) const {
     return (is_wrap_reverse_ != item.is_last_baseline) ? item.cross_axis_size
                                                        - ascent : ascent;
   }
   ```
   The flip applies per-item, keyed on the XOR of `is_wrap_reverse_` and
   whether the item aligns with last-baseline.

2. **Sideways writing modes** — `sideways-rl` is a *vertical* writing mode
   where block progression is leftward. `LogicalBoxFragment` applied with
   the container's `writing_direction` (`flex_layout_algorithm.cc:1873`)
   returns baselines along the container's block axis. For a `sideways-rl`
   container, block axis = horizontal (x-axis). Items' baselines are
   synthesized/reported relative to that axis.

3. **BaselineAccumulator::AccumulateLine** (`flex_layout_algorithm.cc:103-153`)
   — row-only, gated at `:1784` by `if (!is_column_)`. For row containers,
   tracks both `max_major_ascent` and `max_minor_ascent` per line.

4. **BaselineAccumulator::AccumulateItem** (`:87-101`) — fallback that fires
   when no aligned item exists on the first/last line. For last_fallback,
   the assignment is unconditional (last-item-iterated wins). For
   first_fallback, gated on `!first_fallback_baseline_` (first wins).

5. **ResolvedAlignSelf wrap-reverse flip** (`:260-332`) — `flex-start`
   becomes `flex-end` under wrap-reverse (and vice versa). `baseline`
   and `last baseline` are NOT swapped by wrap-reverse at the resolution
   layer; the visual flip happens inside BaselineAscent.

**Louis14 current state to investigate:**
- `flex_layout.go:808-810` — `baselineParallel` uses `item.wdm.IsVertical()
  == wdm.IsVertical()`. For sideways-rl container (`IsVertical() == true`)
  and default children (also vertical? verify): this returns true.
- `flex_layout.go:1577` — `crossBPStart` uses `Border.BlockStart`, correct
  for row under sideways-rl.
- `flex_layout.go:1646-1677` — `firstNonCollapsed` / `lastNonCollapsed` with
  `reverseMain` flip. Under wrap-reverse the fix is at the **line order**
  level (`order` slice reversed at `:1684-1688`), not at the per-line item
  level. Verify this matches Blink's order of iteration.
- Per-item baseline under wrap-reverse: does louis14 apply the
  `BlockSize() - baseline` flip? Grep for `wrapReverse` and check
  `BaselineAscent` equivalent logic.

**Likely hypothesis:** Either (a) louis14 doesn't flip individual item
baselines under wrap-reverse (missing BaselineAscent equivalent), (b)
sideways-rl writing direction isn't plumbed through correctly for baseline
synthesis, or (c) margin-box baseline for `.offset` pink/orange uses the
wrong margin axis (inline margins should NOT affect block-axis baseline).

**Verify against Chrome via wpt.fyi** — this test may be a WPT design quirk;
confirm Chrome passes before assuming bug.

---

## Issue 2 — `flexbox-align-self-baseline-horiz-006` — 7646 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-align-self-baseline-horiz-006.xhtml`

**Structure:**
- Two flex containers with `align-items: baseline` and `align-items: last baseline`.
- First item `.ortho` is `writing-mode: vertical-rl; width:17px; height:40px`.
- Pink item in test has `.offset` class (`margin-top:10; margin-bottom:3`).

**Prior-session pixel analysis (DO NOT RE-DO):**
- Pink's `margin-top: 10px` legitimately shifts pink's margin-box baseline
  down by 10px per CSS Box Alignment §9.4.
- **Ref renders pink WITHOUT the `.offset` class** — the test and ref
  disagree on whether pink gets `.offset`. This is a test-authoring
  inconsistency.

**Blink research — required:**

1. **Margin-box baseline** (`flex_item.h:~200` — `MarginBoxAscent`):
   Blink's `MarginBoxAscent` = `margin_top + baseline` for a border-box
   baseline. When `align-self: baseline` aligns two items, their
   margin-box baselines line up, which means positive `margin-top` on one
   item pushes its *content* up relative to the other.

2. **Last-baseline margin offset** — for `last baseline`, it's
   `margin_bottom + (cross_size - last_baseline)` (distance from
   margin-box block-end).

3. **`.ortho` item** (vertical-rl, width 17): synthesized baseline
   (`FirstBaselineOrSynthesize` returns `cross_size` when no natural
   baseline available for orthogonal). Blink's synthesis places baseline
   at the block-end edge — for a vertical-rl item in a horizontal-tb row,
   the synthesized baseline is at the item's bottom.

**Action:** Before making any louis14 change, fetch the WPT test history
for this test at wpt.fyi and confirm whether Chrome passes against the
same ref file. If Chrome fails (likely, given the test/ref mismatch),
treat as a WPT quirk and SKIP — document as known WPT design issue.
If Chrome passes, there's a rendering difference to chase.

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
- Each test container exports a **different** baseline (measured at y=6,
  y=9, y=3 in the three containers respectively) matching the **first
  item's font ascent** (medFont=6, bigFont=9, smallFont=3).
- Ref has ALL three containers' baselines at y=17 (the natural line-box
  baseline of the surrounding inline context).
- **Mismatch:** louis14's column-flex containers export the *first item's*
  synthesized first-baseline rather than something aligned with the
  inline context's baseline.

**Blink research — required:**

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
   block axis**. For `flex-direction: column`, the block axis IS the main
   axis — so `offset.block_offset` = item's main-axis offset.

3. **BUT:** `LogicalBoxFragment fragment(container_writing_direction, ...)`
   at `:1873` means `FirstBaselineOrSynthesize` is computed in the
   **container's** writing mode. For a horizontal-tb column container
   with horizontal-tb children, `fragment.FirstBaselineOrSynthesize`
   returns baseline along the container's block axis. The item's block
   axis matches the container's block axis, so that's the item's natural
   first baseline (font ascent from item's top).

4. **Result in Blink:** column-flex container exports
   `item.mainOffset + item.baseline` as its first baseline. For a column
   with main-axis = vertical, item 0 at mainOffset=0 with font ascent 6:
   exports baseline = 6. Which matches what louis14 does.

**So what's the discrepancy?** The REF is using `inline-block` layout, not
`inline-flex`. Inline-blocks have DIFFERENT baseline rules:
- Per CSS 2.1 §10.8, an inline-block's baseline is the baseline of its
  **last in-flow line box**, OR (if no in-flow line box) its **margin box
  bottom edge**.
- For inline-flex, CSS Flexbox §4.2 says: baseline comes from the flex
  container's baseline export rules (AccumulateItem fallback on first
  item's baseline).

**The ref file lies** about being an equivalent rendering — it uses
inline-blocks that get different baseline treatment. **Verify this against
wpt.fyi — does Chrome pass this test?** If yes, Chrome must have some
column-flex baseline handling that matches inline-block's "last line box"
rule. Look for that.

**Possible Blink quirk:** For column flex with `flex-wrap: wrap` producing
multiple columns, maybe Blink exports the **last flex line's** first
baseline? Investigate `AccumulateItem` call order under column-wrap — if
items are iterated in visual column order and the LAST item's fallback
overwrites, the exported baseline could be different.

**Investigation steps:**
1. Read `/tmp/blink_flex_layout_algorithm.cc` lines 1960-2020 for the
   AccumulateItem call site with ALL column-wrap specifics.
2. Check if Blink distinguishes the "first column" first-baseline for
   baseline export in multi-column flex.
3. Check wpt.fyi for flexbox-baseline-multi-line-vert-001 Chrome status.

**Note on `flex_layout.go:1577`:** `crossBPStart = Border.BlockStart +
Padding.BlockStart`. For column containers this IS the block-axis start,
which is correct for storing baselines "distance from container
block-start." Blink uses the same: `offset.block_offset` is
container-block-axis-relative. NOT a bug by itself. The bug is likely in
**which item** is picked, not in the offset.

---

## Issue 4 — `flexbox-baseline-multi-line-vert-002` — 737 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-baseline-multi-line-vert-002.html`

Same family as vert-001 — column flex multi-line baseline. Fix vert-001
first; vert-002 likely has the same root cause with a smaller visual
manifestation. Verify after the vert-001 fix lands.

If vert-002 still fails after vert-001 passes, enumerate the remaining
differences and apply the same Blink-first research discipline.

---

## Issue 5 — `flexbox-align-self-horiz-002` — 235 px diff

**Test file:** `pkg/visualtest/testdata/wpt-css3/css-flexbox/flexbox-align-self-horiz-002.xhtml`

**Prior-session analysis (DO NOT RE-DO):**
- Three inline-flex containers with various `align-self` values.
- Third container has `.self-end.wmvertrev` with `writing-mode:
  vertical-lr; direction: rtl`.
- Test container measured **width 345px**, ref measured **width 257px**
  — 88px overshoot.
- Items themselves position identically; only the container's right
  border differs.
- **NOT a baseline issue** — this is a container-sizing bug for
  inline-flex containers holding vertical-WM items.

**Blink research — required:**

1. **Inline-flex container intrinsic sizing** — for inline-level flex,
   the container's inline size is typically `max-content` unless
   constrained. See `flex_layout_algorithm.cc::ComputeMinMaxSizes` or
   `ng_flex_layout_algorithm.cc` (name may vary).

2. **Vertical-WM item inline size from parent's perspective:** a
   `vertical-lr` child inside a horizontal-tb parent contributes its
   **height** (child's inline-axis) to the parent's inline-size
   calculation. The child's `width` (child's block-axis) contributes to
   the parent's block-size. Verify louis14's intrinsic-size plumbing
   uses the right axis swap.

3. **`direction: rtl` on vertical-lr** — affects which end is the
   inline-start for text, but shouldn't affect the container's
   computed inline-size (which is based on content min/max).

**Hypothesis:** louis14's intrinsic-size computation for inline-flex
with vertical-WM children double-counts something (88px is suspiciously
close to the item's content height plus some padding/margin).

**Investigation steps:**
1. Find the inline-flex intrinsic size code path in louis14
   (likely `flex_layout.go` near `ComputeInlineSizes` or similar;
   grep for `intrinsic` and `min_max` in flex files).
2. Add instrumentation to print the per-item contribution to container
   inline-size for this specific test case.
3. Compare against Blink's contribution formula for an orthogonal
   flex item.

**This is small but SYSTEMIC** — it's a container-sizing rule, not a
one-off adjustment. A fix here may surface other test improvements or
regressions. Run the regression guard carefully.

---

## Workflow

For each issue (in this order: horiz-008 → horiz-006 → vert-001 → vert-002
→ horiz-002):

1. **Study** the relevant Blink code paths. Quote specific file:line
   refs in your commits/PR.
2. **Verify** against wpt.fyi whether Chrome passes the test. If Chrome
   fails too, document the finding and move to the next issue.
3. **Hypothesize** the systemic cause — not a point patch.
4. **Implement** the fix in `pkg/layout/flex_layout.go` (or the
   appropriate intrinsic-sizing file for horiz-002).
5. **Run the single test** to verify 0% diff.
6. **Run the regression guard** (10-test command above). If any
   regresses, revert immediately.
7. **Commit** with a message referencing the specific Blink file:line
   you mirrored.
8. **Report** back with the diff before/after and the regression status
   before moving to the next issue.

---

## Success criteria

All 5 tests pass at **0% pixel diff**:
- flexbox-align-self-baseline-horiz-008
- flexbox-align-self-baseline-horiz-006
- flexbox-baseline-multi-line-vert-001
- flexbox-baseline-multi-line-vert-002
- flexbox-align-self-horiz-002

AND the 10-test regression guard continues to pass.

If a test turns out to be a genuine WPT design defect (test/ref
mismatch that Chrome also fails), document the Chrome status with a
wpt.fyi link and move on. Do not patch around WPT defects.
