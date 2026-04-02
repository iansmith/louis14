# Plan: Fix Remaining Sideways Writing-Mode Test Failures

## Current state

After fixing sideways text rendering (off-screen buffer + pixel rotation, commit f0a7c8f0),
37 of 49 sideways WPT tests pass. Twelve remain failing, grouped below by root cause.
Fix them in order — each group is independent.

---

## Group 1 — Initial block position for sideways root element (2 tests, Simple)

**Tests:** `block-flow-direction-slr-066.xht`, `block-flow-direction-srl-065.xht`  
**Diff:** ~0.3–2.1%, small localized block

**What's wrong:**  
When `writing-mode: sideways-lr` is set on the root element, the first block child
should originate at the physical **bottom-left** corner of the ICB (because sideways-lr's
block axis runs right→left physically, so block-start is at the physical bottom).
Currently it renders at the top-left, as if it were HTB.

**Fix:** In the block layout code that computes the initial block-start position,
check `wdm.WM == WritingModeSidewaysLR` and offset the starting block position by the
viewport height. Likely 1–2 lines in `pkg/layout/block_layout.go` or
`pkg/layout/engine.go` where the root block constraint space is built.

**Verification:**
```
go test ./pkg/visualtest/ -run "TestWPT/css-writing-modes/block-flow-direction-s.l-06[56]"
```

---

## Group 2 — Block siblings flowing horizontally in sideways-lr (4 tests, Moderate)

**Tests:** `block-flow-direction-slr-043.xht`, `slr-048.xht`, `slr-062.xht`, `slr-063.xht`  
**Diff:** 8–16%

**What's wrong:**  
In `sideways-lr`, block-level siblings should be laid out left-to-right (the block axis
is physical horizontal). The current engine places them top-to-bottom, same as
`vertical-lr`. The reference files show a horizontal "P A S S" letter pattern confirming
sequential left-to-right column placement.

`slr-048` adds `float:right` inside a sideways-lr container, which should establish a
BFC with horizontal block flow — same root cause, float position is also wrong.

**Fix:** In `pkg/layout/block_layout.go`, wherever block children are stacked
(the block-offset accumulator), ensure the converter produces correct physical offsets
for sideways-lr. The `WritingModeConverter.ToPhysicalOffset` already handles
`WritingModeSidewaysLR` — the issue may be that the available-size or block-start
offset passed into child layout is not correct. Compare a passing `slr-05x` test
against a failing `slr-043` to find the divergence.

**Verification:**
```
go test ./pkg/visualtest/ -run "TestWPT/css-writing-modes/block-flow-direction-slr-(043|048|062|063)"
```

---

## Group 3 — Inline-block alphabetic baseline in sideways modes (2 tests, Moderate)

**Tests:** `inline-block-alignment-slr-009.xht`, `inline-block-alignment-srl-008.xht`  
**Diff:** ~5–6%

**What's wrong:**  
CSS Writing Modes §4.3: when `writing-mode` is `sideways-lr` or `sideways-rl`, the
**alphabetic** baseline is used as the dominant baseline for inline alignment.
The inline layout engine currently aligns inline-block elements using the standard
HTB baseline logic, producing wrong vertical alignment for mixed font-size inline
blocks.

**Fix:** In `pkg/layout/inline_layout.go`, find where the dominant baseline is
selected for line-box vertical alignment. When `wdm.WM` is `WritingModeSidewaysLR`
or `WritingModeSidewaysRL`, force use of the alphabetic baseline rather than the
computed baseline of the line. May interact with the `InlineFragment` baseline field.

**Verification:**
```
go test ./pkg/visualtest/ -run "TestWPT/css-writing-modes/inline-block-alignment-s[lr]"
```

---

## Group 4 — line-box-direction-slr-048 (1 test, Unknown)

**Test:** `line-box-direction-slr-048.xht`

This test was failing but was not analyzed in detail. Read the test file and its
reference to determine root cause before fixing. It may share the root cause of
Group 2 or Group 3.

---

## Group 5 — Flex/table main-axis direction in sideways modes (3 tests, Complex)

**Tests:** `row-progression-slr-029.xht`, `row-progression-srl-028.xht`,
`sideways-lr-main-axis.html`  
**Diff:** 1–4%

### Flexbox (`sideways-lr-main-axis.html`)

**What's wrong:**  
`flex-direction: row` with `writing-mode: sideways-lr` should produce a horizontal
main axis (because sideways-lr's inline axis is vertical but its *physical* block axis
is horizontal). Currently the flex engine likely maps main-axis based on
`IsVertical()` alone, producing wrong item ordering.

In `pkg/layout/flex_layout.go`, the logic `!containerWDM.IsVertical() == isRow`
treats sideways-lr as a fully vertical mode. For sideways modes, the mapping of
logical row/column to physical axes is different from vertical-rl/vertical-lr.
Check `IsFlippedBlocks()` and handle `WritingModeSidewaysLR` / `WritingModeSidewaysRL`
explicitly.

### Tables (`row-progression-slr-029`, `row-progression-srl-028`)

**What's wrong:**  
Table rows in sideways-lr/srl should progress along the physical horizontal axis.
The table layout engine currently stacks rows vertically (same as vertical-rl/lr).
In sideways-lr, each row is a physical column progressing left→right; in sideways-rl,
right→left.

**Fix sequence:**
1. Fix flex main-axis mapping in `pkg/layout/flex_layout.go` for sideways modes.
2. Fix table row progression in `pkg/layout/table_layout.go` (if it exists) or
   wherever table children are stacked.
3. Both changes must account for `WritingModeSidewaysLR` and `WritingModeSidewaysRL`
   as distinct from `WritingModeVerticalLR` / `WritingModeVerticalRL`.

**Verification:**
```
go test ./pkg/visualtest/ -run "TestWPT/css-writing-modes/(row-progression-s|sideways-lr-main)"
```

---

## Fix order

1. **Group 1** — 2 lines, 2 tests, easiest win
2. **Group 4** — read the test first; if simple, fix next
3. **Group 3** — inline baseline, isolated to `inline_layout.go`
4. **Group 2** — block sibling layout in sideways-lr
5. **Group 5** — flex + table, most complex, fix flex first then table

---

## Running all sideways tests at once

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Expected: 0 failures when all groups are fixed.
