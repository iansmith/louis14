# Continuation prompt — sideways remaining failures (session 2)

Execute the plan in `docs/sideways-remaining-failures-plan.md` to fix the remaining
sideways writing-mode WPT test failures.

## Branch / commit

Branch: `rewrite/louis13-louis14`  
Last commit: `f0a7c8f0` (or whatever HEAD is — check with `git log --oneline -3`)

## Current baseline

Run to verify:
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run "TestWPT/css-writing-modes" -v 2>&1 \
  | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
```

Expected: 37 pass, 12 fail (the 12 listed in the plan).

---

## What was already done

`pkg/layout/engine.go` — Phase 5 was fixed to use `rootWDM` as both parent and child
WDM when building the root constraint space. Previously used `icbWDM = HTB` as the
parent, which set `IsOrthogonalWritingModeRoot=true` and triggered shrink-to-fit sizing
on the root element. The fix:

```go
var rootInlineSize, rootBlockSize float64
if rootWDM.IsHorizontal() {
    rootInlineSize = le.viewport.width
    rootBlockSize = le.viewport.height
} else {
    rootInlineSize = le.viewport.height
    rootBlockSize = le.viewport.width
}
var rootMinBlock float64
if rootWDM.IsVertical() {
    rootMinBlock = le.viewport.width
} else {
    rootMinBlock = le.viewport.height
}
rootSpace := NewConstraintSpaceBuilder(rootWDM, rootWDM, true).
    SetForcedMinBlockSize(rootMinBlock).
    SetAvailableSize(LogicalSize{InlineSize: rootInlineSize, BlockSize: rootBlockSize}).
    SetPercentageResolutionSize(LogicalSize{InlineSize: rootInlineSize, BlockSize: rootBlockSize}).
    Build()
```

This fix is **already committed** (see engine.go in HEAD).

---

## Group 1 — Two bugs to fix

### srl-065: `drawImage` ignores padding (16px offset)

**Root cause confirmed:** `pkg/render/render.go` function `drawImage` (around line 388)
draws the image at `box.X + box.Border.Left` but ignores `box.Padding.Left`.
The reference for srl-065 uses `<img style="float:right; padding-left:16px">` — the
swatch-blue.png content is 16px inside the img box's left edge, but `drawImage` draws
it flush against the border.

**Fix:** In `drawImage`:
```go
// Current (wrong):
dstW := int(math.Round(box.Width - box.Border.Left - box.Border.Right))
dstH := int(math.Round(box.Height - box.Border.Top - box.Border.Bottom))
drawX := int(math.Round(box.X + box.Border.Left))
drawY := int(math.Round(box.Y + box.Border.Top))

// Should be (content area):
dstW := int(math.Round(box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right))
dstH := int(math.Round(box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom))
drawX := int(math.Round(box.X + box.Border.Left + box.Padding.Left))
drawY := int(math.Round(box.Y + box.Border.Top + box.Padding.Top))
```

After this fix, **run the full test suite** (not just writing-modes) to confirm no regressions — this touches a hot path.

### slr-066: abs-pos containing block propagation

**Root cause confirmed:** The test renders correctly (blue 100×100 at bottom-left ✓).
The **reference** file `block-flow-direction-066-ref.xht` renders blank.

The reference uses: `<p style="position:absolute; bottom:8px; ...">`. The `<body>` has
no in-flow children, so its `finalBlockSize = 0`. `OutOfFlowLayoutPart` uses
`cbBlock = 0`, so `blockOffset = 0 - 8 - 0 - 100 = -108` (off-screen).

CSS rule: if no positioned ancestor exists, the containing block is the **initial
containing block** (the viewport). Our engine uses the nearest block ancestor's content
size instead.

**Fix location:** `pkg/layout/block_layout.go` — `OutOfFlowLayoutPart` is created with:
```go
containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
```
When the element is `position: fixed` or the body has no in-flow children, `finalBlockSize`
can be 0 even though the containing block should be the ICB (viewport height).

The fix requires passing the viewport size as a fallback for the containing block when
there is no positioned ancestor. Look at how `OutOfFlowLayoutPart` is constructed in
`block_layout.go` and propagate `ctx.ViewportWidth` / `ctx.ViewportHeight` as a minimum.

The most correct fix: after normal flow layout, if `finalBlockSize < viewportHeight`
(for HTB or equivalent), set the out-of-flow containing block to the **ICB size**
(not the content size) when laying out children whose ancestors are not positioned.

This is a moderate fix. Read `pkg/layout/block_layout.go` carefully before changing —
specifically the `OutOfFlowLayoutPart` construction and `LayoutCandidates` call near
the end of `BlockLayoutAlgorithm.Layout()`.

---

## Groups 2–5 — Untouched

See `docs/sideways-remaining-failures-plan.md` for full details. Quick summary:

- **Group 2** (4 tests, slr-043/048/062/063): block siblings in sideways-lr placed top-to-bottom instead of left-to-right. Fix in `pkg/layout/block_layout.go`.
- **Group 3** (2 tests, slr-009/srl-008): inline-block alphabetic baseline wrong in sideways modes. Fix in `pkg/layout/inline_layout.go`.
- **Group 4** (1 test, line-box-direction-slr-048): unknown — read the test file first.
- **Group 5** (3 tests, row-progression slr/srl, sideways-lr-main-axis): flex/table main-axis direction wrong. Fix in `pkg/layout/flex_layout.go` (and possibly table).

## Workflow reminder

1. Fix one group at a time.
2. After each fix, run the specific group's tests and then the full writing-modes suite.
3. Commit each group separately.
4. Never modify `pkg/render/render.go` for writing-mode layout bugs (rendering is correct — except for the `drawImage` padding bug above, which IS a rendering fix).
