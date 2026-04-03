# Continuation prompt — sideways remaining failures (session 4)

Continue fixing the remaining sideways writing-mode WPT test failures.

## Branch / commit

Branch: `rewrite/louis13-louis14`  
Check HEAD with `git log --oneline -5`

## What was done this session

### Commits made this session:

1. **cd0e892b** — Fix drawImage padding and abs-pos ICB containing block
   - `pkg/render/render.go`: `drawImage` now uses content area (adds padding to position/size)
   - `pkg/layout/block_layout.go`: abs-pos children of non-positioned containers use ICB
   - `pkg/css/cascade.go`: body margin uses individual props (still 0)

2. **f195041a** — Fix table section ordering and flex min/max sizing
   - `pkg/layout/table_layout.go`: thead → tbody → tfoot ordering (was source order)
   - `pkg/layout/min_max_sizing.go`: `measureFlexMinMax()` for flex containers:
     - row flex: min/max inline = SUM of items (not max)
     - column flex: min/max inline = MAX of items' inline sizes
     Fixes orthogonal writing mode root flex containers (shrink-to-fit was using max
     instead of sum, causing row flex containers to be 20px instead of 60px)

## Current baseline

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Expected results:
- **40 pass** (added slr-029, srl-028 vs session 3 baseline of 38)
- **9 fail** (down from 11)

Still failing (9):
- `block-flow-direction-slr-043.xht` ← body margin blocked
- `block-flow-direction-slr-048.xht` ← float in sideways-lr (unknown)
- `block-flow-direction-slr-062.xht` ← body margin blocked
- `block-flow-direction-slr-063.xht` ← body margin blocked
- `block-flow-direction-slr-066.xht` ← body margin blocked
- `inline-block-alignment-slr-009.xht` ← alphabetic baseline
- `inline-block-alignment-srl-008.xht` ← alphabetic baseline
- `line-box-direction-slr-048.xht` ← unknown, needs investigation
- `sideways-lr-main-axis.html` ← 1680 pixels still differ (0.4%)

## sideways-lr-main-axis: remaining 1680 pixels

The test improved from 2.6% to 0.4% (12420 → 1680 pixels). The diff shows red at
approximately X=20..40, Y=240..320 (the column-flex containers 5-8). This suggests
item 2 (limegreen) in each column flex container is wrong.

The test has 8 flex containers with `writing-mode: sideways-lr`:
- Containers 1-4: `flex-direction: row` (main = inline = physical Y, bottom-to-top)
- Containers 5-8: `flex-direction: column` (main = block = physical X, left-to-right)

The reference `sideways-lr-main-axis-ref.html` uses normal HTB layout with `display:
inline-block` items for column-equivalent, and block items for row-equivalent.

Investigation needed: why does the column flex limegreen item (2nd item, X=20..40)
appear differently in the test vs reference? Render the test image and compare with
reference. It might be related to the `direction: rtl` containers (6 and 8) or a
cross-axis alignment issue.

## Recommended fix order

1. **sideways-lr-main-axis** (0.4% remaining): Investigate the 1680-pixel diff more
   carefully. Examine `output/reftests/sideways-lr-main-axis_*.png`. Check if the
   issue is with `direction: rtl` container (6 or 8) specifically.

2. **Group 3** (inline-block-alignment-slr-009, srl-008): alphabetic baseline wrong
   in sideways modes. Fix in `pkg/layout/inline_layout.go` — dominant baseline for
   sideways-lr and sideways-rl should be alphabetic.
   Reference: `inline-block-alignment-slr-009-ref.xht` and `inline-block-alignment-006-ref.xht`
   Test: uses `writing-mode: sideways-lr/sideways-rl` with inline-block children.

3. **Group 4** (line-box-direction-slr-048): float:right in sideways-lr. Test file
   says "ordering direction of line boxes" with `float: right` on `.floated-right` divs.
   May be related to float direction in sideways-lr.

4. **slr-048** (float:right in sideways-lr): separately from slr-048 line-box test.
   Check if it's body-margin-dependent.

5. **Body margin project** (slr-043, 062, 063, 066): blocked on body margin=8px.
   Large scope — save for a dedicated session.

## Running all sideways tests

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Full regression check:
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -count=1 2>&1 \
  | grep "^--- FAIL" | wc -l
```
Expected: ≤ 2 failures (the baseline before this work began).
