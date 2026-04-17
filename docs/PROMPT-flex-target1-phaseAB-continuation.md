# Continuation Prompt — Flex Baseline Target 1, Phases A/B

**Branch**: `fix/flexbox-fast`
**Predecessor plan**: `docs/PLAN-flex-target1-baseline-synthesis.md`
**Prior commits** (already landed on branch):
- `53986ff3` Phase C — exclude orthogonal flex items from baseline accumulation
- `a8c1eebf` Phase D — fieldset baseline export + inline-block anonymous wrapping

## Project rules (from CLAUDE.md — non-negotiable)

1. **Foundational correctness over quick wins.** No point-fixes, no filtering by error %. Solve the category completely. A 122-pixel diff is a failure the same way 25862 is.
2. **Study Blink BEFORE writing any code.** Mirror Blink's types, algorithm structure, and constraint-passing. Quote the Blink source in every commit message.
3. **All tests must pass at 0% diff.** The only acceptable exception is a test that also fails in Chrome per wpt.fyi (`status:"F"`); record wpt.fyi evidence in that case.
4. **Test execution discipline.** Iterate on 1–4 tests per phase. Full-suite runs only at commit checkpoints. Do NOT `go test ./...`.
5. **Operational rules.** Never `open` files from agents. Commit+push before launching worktree agents. Agents commit at each milestone, not only at end.

## What's still failing (Target 1)

| Test | Current diff | Expected close by |
|---|---|---|
| flexbox-align-self-baseline-horiz-006.xhtml | 7646 | Phase B (+wrap-reverse flip) |
| flexbox-align-self-baseline-horiz-008.xhtml | 25862 | Phase B |
| flexbox-align-self-horiz-002.xhtml | 235 | Phase A |
| flexbox-baseline-align-self-baseline-horiz-001.html | 557 | Phase A |
| flexbox-baseline-multi-item-horiz-001a.html | 122 | Phase B |
| flexbox-baseline-multi-item-horiz-001b.html | 122 | Phase B |
| flexbox-baseline-multi-line-vert-001.html | 1649 | Phase A |
| flexbox-baseline-multi-line-vert-002.html | 737 | Phase A |

(horiz-006 may retain a small residual — pink item margin-top on wrap-reverse line — confirm vs Chrome via wpt.fyi if it doesn't close fully.)

## Prior attempt postmortem (DO NOT REPEAT)

A previous session tried Phase A's accumulator refactor in one shot. It **regressed `flexbox-baseline-multi-line-horiz-004`** (wrap-reverse, `align-content: center`, baseline items on DOM-first = visual-last line) because the refactor iterated lines in physical (post-reversal) order for the accumulator **and** picked the wrong "first" line for source-first-line-wins semantics.

Root cause, in one sentence: Blink's `BaselineAccumulator::AccumulateLine` runs during `GiveItemsFinalPositionAndSize`, which runs **after** `ApplyReversals` — so "first physical line" there is the post-reversal index 0. In louis14 today, `lines` is still in **source order** at baseline-export time; iteration must either explicitly flip under `wrap-reverse` or the equivalent semantics must be encoded in the per-line `crossAxisOffset`. The prior attempt flipped iteration without also making sure `major_baseline / cross_axis_offset` math matched Blink's invariants on the post-reversal frame.

**Regression guard** for Phases A and B (run after each phase):
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table|flexbox-baseline-multi-line-horiz-004|flexbox-baseline-multi-line-horiz-001|flexbox-baseline-multi-line-horiz-003|fieldset-baseline-alignment)' \
  -count=1
```

Treat these seven as a **signal**, not an absolute gate:
- `flexbox-align-self-baseline-horiz-001a/b` — motivating three, baseline sets cross-size
- `flexbox-align-self-horiz-001-table` — row/table baseline interaction
- `flexbox-baseline-multi-line-horiz-004` — the regressor from last attempt (wrap-reverse align-content)
- `flexbox-baseline-multi-line-horiz-001/003` — sanity, wrap-reverse behaviours
- `fieldset-baseline-alignment` — just closed in Phase D

**When it's OK for one of these to regress** (per CLAUDE.md rule #1 — foundational correctness beats preserving the local state of the suite):

Temporary regression in the guard set is acceptable **only if all three hold**:
1. The new code is a faithful port of the cited Blink algorithm — you can point at the exact Blink lines and show the port matches.
2. The regressed test was passing by coincidence of the legacy heuristic (e.g., the old `firstBLLine`/`lastBLLine` special case) rather than on actual correctness. Check this by diffing test vs ref pixel output: a test passing with the right pixels for the wrong reason is a latent bug.
3. You have a concrete hypothesis for *what Blink-correct mechanism closes the regressed test* (e.g., "Phase B's flip will resolve this once it lands", "this needs follow-up Phase C+ to handle `is_reverse_direction_`"). Write that hypothesis into the commit message so a future session can verify.

**When it's NOT OK**: a regression you can't explain, a regression masked by a second change, or a regression in the motivating-three (`horiz-001a/b`, `horiz-001-table`) — those validate the core "baseline participates in cross-sizing" contract and should not break under any correct port.

When in doubt: commit the WIP, push, and write a follow-up note explaining what you believe will re-close the regressed test. Do not paper over it with a targeted branch in the code.

**Target verification command** (run each phase to see progress):
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-baseline|flexbox-align-self-baseline|flexbox-align-self-horiz-002|fieldset-baseline)' \
  -count=1
```

## Blink investigation (already done — quote this in commit messages)

All line numbers below are in the cached sources at `/tmp/blink_*.{cc,h}`. Re-fetch from Chromium if the cache is gone — the files are small.

### 1. `BaselineAccumulator` class — `flex_layout_algorithm.cc:80-153`

```cpp
class BaselineAccumulator {
  STACK_ALLOCATED();
 public:
  explicit BaselineAccumulator(const ComputedStyle& style)
      : font_baseline_(style.GetFontBaseline()) {}

  void AccumulateItem(const LogicalBoxFragment& fragment,
                      const LayoutUnit block_offset,
                      bool is_first_line, bool is_last_line) {
    if (is_first_line) {
      if (!first_fallback_baseline_) {
        first_fallback_baseline_ =
            block_offset + fragment.FirstBaselineOrSynthesize(font_baseline_);
      }
    }
    if (is_last_line) {
      last_fallback_baseline_ =
          block_offset + fragment.LastBaselineOrSynthesize(font_baseline_);
    }
  }

  void AccumulateLine(const FlexLine& line,
                      bool is_first_line, bool is_last_line) {
    if (is_first_line) {
      if (line.major_baseline != LayoutUnit::Min())
        first_major_baseline_ = line.cross_axis_offset + line.major_baseline;
      if (line.minor_baseline != LayoutUnit::Min())
        first_minor_baseline_ =
            line.cross_axis_offset + line.line_cross_size - line.minor_baseline;
    }
    if (is_last_line) {
      if (line.major_baseline != LayoutUnit::Min())
        last_major_baseline_ = line.cross_axis_offset + line.major_baseline;
      if (line.minor_baseline != LayoutUnit::Min())
        last_minor_baseline_ =
            line.cross_axis_offset + line.line_cross_size - line.minor_baseline;
    }
  }

  std::optional<LayoutUnit> FirstBaseline() const {
    if (first_major_baseline_) return *first_major_baseline_;
    if (first_minor_baseline_) return *first_minor_baseline_;
    return first_fallback_baseline_;
  }
  std::optional<LayoutUnit> LastBaseline() const {
    if (last_minor_baseline_) return *last_minor_baseline_;
    if (last_major_baseline_) return *last_major_baseline_;
    return last_fallback_baseline_;
  }
};
```

**Priorities** — note the asymmetry:
- First: **major → minor → fallback**
- Last:  **minor → major → fallback**

### 2. Per-line major/minor ascent tracking — `flex_layout_algorithm.cc:1460-1559`

Inside `PlaceFlexItems` (source order, pre-reversal), while sizing each line:
```cpp
LayoutUnit max_major_ascent = LayoutUnit::Min();
LayoutUnit max_minor_ascent = LayoutUnit::Min();
LayoutUnit max_major_descent = LayoutUnit::Min();
LayoutUnit max_minor_descent = LayoutUnit::Min();
// ...
if (has_baseline_alignment) {
  const LayoutUnit ascent =
      layout_result
          ? BaselineAscent(flex_item, PhysicalBoxFragment)
          : SynthesizedBaselineAscent(flex_item, cross_axis_size);
  const LayoutUnit descent = cross_axis_margin_size - ascent;
  if (flex_item.baseline_group == BaselineGroup::kMajor) {
    max_major_ascent = std::max(max_major_ascent, ascent);
    max_major_descent = std::max(max_major_descent, descent);
    cross_axis_margin_size = max_major_ascent + max_major_descent;
  } else {
    max_minor_ascent = std::max(max_minor_ascent, ascent);
    max_minor_descent = std::max(max_minor_descent, descent);
    cross_axis_margin_size = max_minor_ascent + max_minor_descent;
  }
}
// stored on FlexLine:
flex_lines->emplace_back(..., line_cross_size, max_major_ascent, max_minor_ascent, ...);
```

### 3. `BaselineAscent` (with wrap-reverse flip) — `flex_layout_algorithm.cc:370-393`

```cpp
LayoutUnit FlexLayoutAlgorithm::BaselineAscent(
    const FlexItem& item, const PhysicalBoxFragment& fragment) const {
  LogicalBoxFragment baseline_fragment(item.baseline_writing_direction, fragment);
  const bool is_last_baseline = item.alignment == ItemPosition::kLastBaseline;
  const auto font_baseline = Style().GetFontBaseline();
  LayoutUnit baseline =
      is_last_baseline
          ? baseline_fragment.LastBaselineOrSynthesize(font_baseline)
          : baseline_fragment.FirstBaselineOrSynthesize(font_baseline);
  if (is_wrap_reverse_ != is_last_baseline) {
    baseline = baseline_fragment.BlockSize() - baseline;   // <-- the flip
  }
  const PhysicalToFlex margins(...);
  return item.baseline_group == BaselineGroup::kMajor
             ? margins.CrossStart() + baseline
             : margins.CrossEnd() + baseline;
}
```

`SynthesizedBaselineAscent` (`:395-415`) is structurally identical, just starts from `LogicalBoxFragment::SynthesizedBaseline(...)` instead of reading a real baseline.

### 4. `ApplyReversals` — `flex_layout_algorithm.cc:1589-1599`

```cpp
void FlexLayoutAlgorithm::ApplyReversals(FlexLineVector* flex_lines) {
  if (is_wrap_reverse_) flex_lines->Reverse();
  if (is_reverse_direction_) {
    for (auto& flex_line : *flex_lines) flex_line.item_indices.Reverse();
  }
}
```

Called **once**, between `PlaceFlexItems` and `GiveItemsFinalPositionAndSize` (at `:1276`). After this call, `flex_lines[0]` is the **physical** first line (top under row, left under column).

### 5. Accumulator feed site — `GiveItemsFinalPositionAndSize`, `:1780-1786`

```cpp
FlexLine& flex_line = (*flex_lines)[flex_line_idx];
flex_line.cross_axis_offset = line_cross_axis_offset;       // assigned here
bool is_first_line = flex_line_idx == 0;
bool is_last_line  = flex_line_idx == flex_lines->size() - 1;
if (!InvolvedInBlockFragmentation(container_builder_) && !is_column_) {
  baseline_accumulator.AccumulateLine(flex_line, is_first_line, is_last_line);
}
```

Note: **only called for row containers** (`!is_column_`). Columns use the per-item fallback path instead.

Fallback feed (per item) — `:1980-1981` and `:2603-2604`:
```cpp
baseline_accumulator.AccumulateItem(fragment, offset.block_offset,
                                    is_first_line, is_last_line);
```

### 6. `BaselineGroup` and `baseline_writing_direction` — `baseline_utils.h`

- `DetermineBaselineWritingMode` (`:15-44`) picks the writing-mode to read the baseline from a fragment, based on container WD, child WM, and parallel-context boolean.
- `DetermineBaselineGroup` (`:51-89`) decides major/minor. In **parallel** contexts (our flex case): `baseline_writing_mode == container_writing_mode ? start_group : end_group`, with `start/end_group` swapped by `is_last_baseline` and `is_flipped`.

In `FlexItem` (`flex_item.h:56-58, 126-127`):
```cpp
baseline_writing_direction({baseline_writing_mode, TextDirection::kLtr}),
baseline_group(baseline_group),
```
These are computed once at item construction; propagated through layout.

## Louis14 surface area (current state on HEAD a8c1eebf)

| Concern | File:line | Notes |
|---|---|---|
| `flexLine` struct | `pkg/layout/flex_layout.go:346-349` | Only has `items`, `crossSize`. Missing `majorBaseline`, `minorBaseline`, `crossAxisOffset`. |
| Per-item ascent tracking (line cross-sizing) | `pkg/layout/flex_layout.go:687-744` | Existing loop tracks `maxAscent/maxDescent` and `maxLastAscent/maxLastDescent` locally — does NOT store major/minor on the line. Phase A folds these into line fields. |
| `resolvedFirstBaseline`/`resolvedLastBaseline` | `pkg/layout/flex_layout.go:304-343` | Already excludes orthogonal items (Phase C). No wrap-reverse flip yet — Phase B. |
| Container baseline export (legacy `firstBLLine`/`lastBLLine` logic) | `pkg/layout/flex_layout.go:1439-1586` | **Delete and replace** with a baselineAccumulator instance. |
| Wrap-reverse line reversal | `pkg/layout/flex_layout.go:3469-3473` (per plan) | `computeAlignContent` flips per-line cross offsets. That is the assignment Blink does via `ApplyReversals` + `line.cross_axis_offset = ...`. |
| `baselineParallel` expression | `pkg/layout/flex_layout.go:692-693` | `(isRow && same verticality) \|\| (!isRow && orthogonal verticality)` — Blink's equivalent is encoded via `baseline_writing_direction` + `is_parallel_context`. The current expression matches Blink's effect for row+column. |

## Phase plan (two commits)

### Phase A — `baselineAccumulator` struct + per-line major/minor fields

**Goal**: Mirror Blink's data model, wire it in, keep wrap-reverse semantics correct. No wrap-reverse *flip* yet — that's Phase B.

**Data model changes**:
```go
// Add to flexLine (pkg/layout/flex_layout.go:346):
type flexLine struct {
    items           []*flexItem
    crossSize       float64
    majorBaseline   float64 // max ascent of align-self:baseline items; math.Inf(-1) if none
    minorBaseline   float64 // max ascent of align-self:last-baseline items; math.Inf(-1) if none
    crossAxisOffset float64 // physical cross-axis offset; set during final placement
}

// New file pkg/layout/flex_baseline_accumulator.go (or inline):
type baselineAccumulator struct {
    firstMajor, firstMinor, firstFallback optFloat
    lastMajor,  lastMinor,  lastFallback  optFloat
}
type optFloat struct { val float64; set bool }

func (a *baselineAccumulator) accumulateLine(line *flexLine, lineCrossSize float64, isFirst, isLast bool) { /* mirror :104-125 */ }
func (a *baselineAccumulator) accumulateItem(blockOffset, firstBL, lastBL float64, isFirst, isLast bool) { /* mirror :87-101 */ }
func (a *baselineAccumulator) firstBaseline() (float64, bool) { /* major → minor → fallback */ }
func (a *baselineAccumulator) lastBaseline()  (float64, bool) { /* minor → major → fallback */ }
```

**Where each field gets set**:
- `majorBaseline / minorBaseline`: inside the cross-sizing loop at `:687-744`. Compute `ascent := item.crossMarginStart() + resolvedFirstBaseline/resolvedLastBaseline`; take max by `align-self:baseline` vs `last baseline`; assign to `line.majorBaseline`/`line.minorBaseline` at the end of the line's loop. Use `math.Inf(-1)` sentinel for "no participant".
- `crossAxisOffset`: in `computeAlignContent`, where `lineOffsets[i]` is already computed. Under wrap-reverse the per-line offset is flipped there — store the **post-flip physical offset** on `line.crossAxisOffset` so Blink's `offset + line.majorBaseline` invariant holds verbatim.

**Iteration at export site** (replacement for `:1439-1586`):
```go
order := make([]int, len(lines))
for i := range order { order[i] = i }
if reverseCross { // wrap-reverse
    slices.Reverse(order)
}
accum := &baselineAccumulator{}
if isRow {
    for idx, lineIdx := range order {
        line := lines[lineIdx]
        isFirst := idx == 0
        isLast  := idx == len(order)-1
        accum.accumulateLine(line, line.crossSize, isFirst, isLast)
        // Fallback per-item: first item of first line, last item of last line.
        if isFirst && len(line.items) > 0 {
            item := line.items[0]
            // compute block-offset + item first-baseline-or-synthesize
            accum.accumulateItem(crossBPStart+itemBlockOffset(item), item.baseline, item.lastBaseline, true, false)
        }
        if isLast && len(line.items) > 0 {
            item := line.items[len(line.items)-1]
            accum.accumulateItem(crossBPStart+itemBlockOffset(item), item.baseline, item.lastBaseline, false, true)
        }
    }
} else {
    // Column: only fallback path (Blink skips AccumulateLine for columns at :1784).
    // Use first item of the first line and last item of the last line.
}
if bl, ok := accum.firstBaseline(); ok { builder.SetBaseline(bl) }
if bl, ok := accum.lastBaseline();  ok { builder.SetLastBaseline(bl) }
```

**Invariant to preserve** (this is where Phase A failed last time):
- Under **wrap-reverse**, `line.crossAxisOffset` MUST already be the physical post-flip offset when the accumulator reads it. The accumulator does `crossAxisOffset + majorBaseline` — there is no second flip in the accumulator.
- The `order` iteration gives us physical-order `isFirst/isLast`; the `crossAxisOffset` stored on each line gives us physical position. These two together exactly replicate Blink's post-`ApplyReversals` state.

**Motivation for the regressor `multi-line-horiz-004`**:
It uses `flex-wrap: wrap-reverse` with `align-content: center`, small items with `align-self: baseline`. Baseline items are on the **source-first** line which wraps-reverse to physical last. Blink's answer: the physical-first line has no baseline-aligned items → `first_major/minor_baseline_` unset → falls back to `first_fallback_baseline_` = first item of physical-first line's block-offset + `FirstBaselineOrSynthesize`. Louis14's legacy code special-cases "source-first line wins if it has baseline-aligned item" at `:1449-1467` — which produces the correct answer FOR THAT CASE, but only because the source-first line also happens to be visually physical-last. Getting this right in the accumulator requires that the **synthesized fallback** come from the correct item at the correct block offset; the Blink approach (fallback = first item of first **physical** line) is the correct one.

**Expected improvements**:
- `flexbox-align-self-horiz-002` (235 → ~0): multi-column-like structure where current logic grabs wrong line.
- `flexbox-baseline-align-self-baseline-horiz-001` (557 → ~0): similar structural miss.
- `flexbox-baseline-multi-line-vert-001/002` (1649, 737 → likely close, maybe residual): vertical-container multi-line interacts with `baselineParallel` correctly once lines all feed the accumulator.

**Expected still-open after Phase A**: horiz-006, horiz-008, multi-item-horiz-001a/b (wrap-reverse flip issues).

**Commit message must cite**: `flex_layout_algorithm.cc:80-153` for the accumulator and `:1460-1559` for line field population.

### Phase B — Wrap-reverse baseline flip in per-item ascent

**Change** (inside `resolvedFirstBaseline`/`resolvedLastBaseline` OR at their call sites):

```go
// Mirror BaselineAscent at flex_layout_algorithm.cc:382-384:
//   if (is_wrap_reverse_ != is_last_baseline) baseline = BlockSize() - baseline;
if reverseCross != isLastBaseline {
    baseline = item.crossSize - baseline  // crossSize is border-box; matches LogicalFragment.BlockSize()
}
```

Apply at the **point the ascent is recorded into the line's major/minor accumulator** (i.e., inside the `:687-744` loop, after `resolvedFirstBaseline` returns). Do NOT apply at container export time — the flip is about the item's local baseline, not the container's.

The `baseline_group` (major vs minor) is implicit in louis14 because we track `maxAscent/maxLastAscent` separately; Blink's `baseline_group` is computed pre-flight by `DetermineBaselineGroup` but for the row/column cases the "major" group is baseline-aligned and "minor" is last-baseline-aligned, which matches louis14's split exactly.

**Expected close**: horiz-006, horiz-008, multi-item-horiz-001a/b. If horiz-006 residual persists (pink-margin issue from prior postmortem), compare to Chrome via wpt.fyi — the test may tolerate that diff via the fuzz tag.

**Commit message must cite**: `flex_layout_algorithm.cc:382-384` (`BaselineAscent` flip).

## Checkpoint protocol (same as master plan)

For each phase:
1. Re-read the Blink sources cited above if they're not fresh in your context. The cached copies are at `/tmp/blink_flex_layout_algorithm.cc` (3207 lines), `/tmp/blink_baseline_utils.h`, `/tmp/blink_flex_item.h`, `/tmp/blink_flex_line.h`. Re-fetch from chromium.googlesource.com if /tmp was wiped.
2. Implement the phase. Keep each commit focused on one structural concept.
3. Run the regression guard (7 tests) — ALL must pass.
4. Run the target verification command — enumerate the pass/fail delta.
5. Commit with a Blink source quote, then push.
6. Move to the next phase.

Do NOT run the full flex suite or full visualtest suite during phase iteration. Save that for a final post-Phase-B checkpoint and only if the regression guard is green.

## Files to study before writing any code

Read these first in this order:
1. `/tmp/blink_flex_layout_algorithm.cc:80-153` — BaselineAccumulator (whole class)
2. `/tmp/blink_flex_layout_algorithm.cc:370-415` — BaselineAscent / SynthesizedBaselineAscent
3. `/tmp/blink_flex_layout_algorithm.cc:1460-1559` — per-line ascent tracking inside PlaceFlexItems
4. `/tmp/blink_flex_layout_algorithm.cc:1589-1599` — ApplyReversals
5. `/tmp/blink_flex_layout_algorithm.cc:1770-1810` — the loop that calls AccumulateLine
6. `/tmp/blink_flex_item.h:56-58, 126-127` — baseline_writing_direction, baseline_group
7. `/tmp/blink_baseline_utils.h` — DetermineBaselineGroup / DetermineBaselineWritingMode (whole file; it's 94 lines)
8. `pkg/layout/flex_layout.go:280-350` — flexItem / flexLine structs + resolvedFirstBaseline/resolvedLastBaseline
9. `pkg/layout/flex_layout.go:670-745` — the cross-sizing loop where ascent is tracked today
10. `pkg/layout/flex_layout.go:1439-1586` — the legacy baseline export block to be replaced
11. `pkg/layout/flex_layout.go:3469-3473` — per-line cross-offset flip under wrap-reverse (stores into `lineOffsets`)

## Open risks

- **`crossAxisOffset` lifecycle**. Today nothing on `flexLine` knows its own cross offset after `computeAlignContent` runs. You may need to either (a) populate `line.crossAxisOffset` inside `computeAlignContent` at the same point `lineOffsets[i]` is set, or (b) pass `lineOffsets` alongside `lines` to the accumulator. Option (a) is cleaner and matches Blink.
- **Column fallback path**. Blink only calls `AccumulateLine` for rows (`!is_column_`). Columns rely exclusively on the per-item fallback. Louis14's column tests in the target set (multi-line-vert-001/002) are **row-wise baselines but column containers** — they need the column fallback to read `item.baseline` off the first item in the first physical line. Confirm this detail works before claiming Phase A closes them.
- **`baselineParallel` vs Blink's `baseline_writing_direction`**. Louis14 recomputes parallel-ness inline at each call site. That's fine for the row/column cases currently covered. If you find a case where Blink's `DetermineBaselineWritingMode` flips a baseline-reading WM differently than louis14's boolean, that's a latent Phase-C-adjacent issue — note it and defer; do not fold it into A or B.
- **Alphabetic synthesis box**. Blink uses `SynthesizedBaseline(font_baseline, IsFlippedLines, block_size)` = block-end edge for alphabetic in horizontal WM. Louis14 uses `crossSize` (border-box block-size). These match for alphabetic+horizontal — the motivating case. For vertical WM the match is less clean but the plan's `canSynthesizeRow := isRow && !wdm.IsVertical()` already disables synthesis for vertical containers. Keep that gate in Phase B's flip logic.

## File-touch map

| Phase | File |
|---|---|
| A | `pkg/layout/flex_layout.go` (flexLine struct, :670-745, :1439-1586, :3469-3473) |
| A | Optional new file: `pkg/layout/flex_baseline_accumulator.go` |
| B | `pkg/layout/flex_layout.go` (:687-744 per-item ascent loop, or :304-343 resolver) |

No changes expected to: `block_layout.go`, `layout_tree_builder.go`, `replaced_layout.go`, `writing_mode.go`, test infrastructure.

## Starting actions when you pick this up

1. `git status && git log --oneline -5` to confirm branch is `fix/flexbox-fast` at `a8c1eebf`.
2. Read the 11 sources listed in "Files to study before writing any code" above.
3. Run the regression guard + target verification commands to capture the current baseline (should match the diff numbers in the "still failing" table above).
4. Start Phase A. Do not touch the wrap-reverse flip yet.
