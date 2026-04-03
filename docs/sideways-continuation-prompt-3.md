# Continuation prompt — sideways remaining failures (session 3)

Continue fixing the remaining sideways writing-mode WPT test failures.

## Branch / commit

Branch: `rewrite/louis13-louis14`  
Check HEAD with `git log --oneline -3`

## Changes made this session (NOT YET COMMITTED)

Three files were modified. Verify with `git diff --stat`:

### 1. `pkg/render/render.go` — `drawImage` padding fix

`drawImage` now draws images in the content area (border+padding), not just
border area. This was needed for tests where `<img>` had explicit padding.

### 2. `pkg/layout/block_layout.go` — abs-pos ICB containing block fix

When a block element is NOT a positioned container (`position: static`),
abs-pos children should use the ICB as their containing block. The fix
computes an "effective ICB block size" by subtracting the element's own
block-start margin+border+padding from the viewport height:

```go
ownBlockStart := ownMargins.BlockStart + geom.Border.BlockStart + geom.Padding.BlockStart
icbEffective := icbBlockSize - ownBlockStart
if icbEffective > oofBlockSize {
    oofBlockSize = icbEffective
}
```

### 3. `pkg/css/cascade.go` — body margin uses explicit side properties

Changed `style.Set("margin", "0")` to individual side properties (still 0).
The shorthand `Set("margin", "8px")` doesn't expand via `style.Set()`, so
individual side properties must be used. Body margin is STILL 0.

## Current test baseline

Run to verify:
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Expected: 38 pass (srl-065 now passes), 11 fail (down from 12).

Passing that previously failed: `block-flow-direction-srl-065.xht`

Still failing (11):
- `block-flow-direction-slr-043.xht`
- `block-flow-direction-slr-048.xht`
- `block-flow-direction-slr-062.xht`
- `block-flow-direction-slr-063.xht`
- `block-flow-direction-slr-066.xht`
- `inline-block-alignment-slr-009.xht`
- `inline-block-alignment-srl-008.xht`
- `line-box-direction-slr-048.xht`
- `row-progression-slr-029.xht`
- `row-progression-srl-028.xht`
- `sideways-lr-main-axis.html`

## Commit the session's safe changes first

Before starting work, commit what was done:
```bash
git add pkg/render/render.go pkg/layout/block_layout.go pkg/css/cascade.go
git commit -m "Fix drawImage padding and abs-pos ICB containing block for sideways modes

- drawImage: draw in content area (border+padding), not just border area
- block_layout: abs-pos children of non-positioned containers use ICB as
  containing block, with element's own offset subtracted for correct
  ICB-relative positioning
- cascade: use individual margin-* props for body default margin (still 0)
"
```

---

## CRITICAL: Body margin issue (understand before coding)

slr-043, slr-062, slr-066 all need body `margin: 8px` (the real UA default)
to pass. Enabling it naively breaks 400+ tests. The underlying problem:

- These WPT tests are designed for browsers with `body { margin: 8px }`
- Our engine uses `body { margin: 0 }` (legacy decision)
- 400+ tests currently pass coincidentally with `margin: 0` + incorrect OOF
- Enabling `margin: 8px` exposes the incorrect OOF positioning in those tests

**DO NOT enable body margin=8px** until the abs-pos OOF positioning is fully
correct for all ICB cases. The `icbEffective` approach in block_layout.go is
a step toward this, but it's not enough alone.

slr-066 status: reference now renders visibly (not blank), but is 8px off
from the test because `bottom: 8px` in the reference + no body margin in test
= 8px mismatch. This test can only pass when body margin is correctly enabled.

---

## Remaining failures: what's needed

### slr-043, slr-062, slr-063 (Group 2 partial)

These use `font: 20px/1 Ahem; height: 9em` on body. In sideways-lr, body
height = inline size (physical height = 180px). They also use the same
reference `block-flow-direction-043-ref.xht` which has:

```css
div {
    bottom: 8px;
    padding: 1em;
    position: absolute;
    width: 19em;  /* content width */
}
```

The reference uses abs-pos `bottom:8px`. Same problem as slr-066: needs
body margin=8px to match the test's sideways-lr layout.

Root cause: all four (043, 062, 063, 066) are blocked on the body margin
fix. **Don't attempt these without the body margin fix**.

### slr-048 (Group 2 float)

Float:right inside sideways-lr. Check the test file and reference. The
float positioning in sideways-lr may have a separate root cause from the
body margin issue.

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes/block-flow-direction-slr-048" -v
```

### Group 3 (inline-block alignment, 2 tests)

slr-009 and srl-008: alphabetic baseline wrong in sideways modes.
Per original plan `docs/sideways-remaining-failures-plan.md` Group 3.
Fix in `pkg/layout/inline_layout.go` — dominant baseline for sideways modes.

### Group 4 (line-box-direction-slr-048, 1 test)

Unknown root cause. Read the test file first.

### Group 5 (row-progression, sideways-lr-main-axis, 3 tests)

Flex/table main-axis direction wrong for sideways modes.
Per original plan Group 5. Fix in `pkg/layout/flex_layout.go`.

---

## Recommended fix order

1. **First**: Understand if slr-048 (float) is independent of body margin.
   If yes, fix it. If it also needs body margin, skip.

2. **Group 3**: inline-block baseline (slr-009, srl-008) — isolated to
   `inline_layout.go`, likely independent of body margin.

3. **Group 4**: line-box-direction-slr-048 — read test to determine cause.

4. **Group 5**: flex/table main-axis — likely independent of body margin.

5. **Body margin project** (larger scope, do last or separate session):
   a. Enable body margin=8px
   b. Fix ALL tests that break (abs-pos-non-replaced-icb-* etc.)
   c. This unlocks slr-043, 062, 063, 066

---

## Running all sideways tests

```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Full regression check (run after each group):
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -count=1 2>&1 \
  | grep "^--- FAIL" | wc -l
```
Expected: ≤ 2 failures (the baseline before this session).
