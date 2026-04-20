# Plan B3: Abs-pos static position across writing modes (abs-pos-border-offset-003)

## Two Coupled Bugs

### Bug 1 — `LayoutCandidates` ignores `StaticPositionEdge` (primary)

`pkg/layout/out_of_flow_layout.go` lines 213-215 (inline) and 242-244 (block):
```go
} else {
    inlineOffset = staticInline + childMargins.InlineStart
}
...
} else {
    blockOffset = staticBlock + childMargins.BlockStart
}
```
ignores `candidate.StaticPosition.InlineEdge` and `BlockEdge`. Cross-WM conversion
(`ConvertToLogical`) emits `BlockEdge=End` to mean "offset measures CB-start to item's
END". Treating End as Start places the item on the wrong side.

Correct (block axis, mirror for inline):
```go
switch candidate.StaticPosition.BlockEdge {
case StaticEdgeStart:
    blockOffset = staticBlock + childMargins.BlockStart
case StaticEdgeEnd:
    blockOffset = cbBlock - staticBlock - childLogical.BlockSize() - childMargins.BlockEnd
case StaticEdgeCenter:
    inset := (cbBlock - childLogical.BlockSize()) / 2
    blockOffset = staticBlock - inset + childMargins.BlockStart
}
```

Trace for Container 2 (parent=HTB inside VRL): converted candidate
`{Inline:30, Block:55, InlineEdge:Start, BlockEdge:End}`, cbBlock=80, item block-size=30.
`blockOffset = 80 - 55 - 30 = -5` → physical left = `80 - (-5) - 30 = 55`. Matches
reference `left:55px`.

### Bug 2 — `PropagateOOFCandidates` misses parallel-but-different WMs

`pkg/layout/block_layout.go` line 785:
```go
needsConversion := parentWDM.IsOrthogonalTo(childWDM)
```
VRL↔VLR are both vertical (not orthogonal) but block direction reverses
(left→right vs right→left). Conversion is needed but skipped.

Fix:
```go
needsConversion := childWDM.WM != parentWDM.WM || childWDM.Dir != parentWDM.Dir
```

`childContentPhys` computation (lines 790-808) must extend to handle the parallel
case (no axis swap, just `fragment.Size - physical_borders`).

## Coupling

Containers 1/5/6 (VLR parent in VRL) currently pass — Bug 2 missing the conversion
is coincidentally compensated by Bug 1's wrong `BlockEdge=Start` interpretation.
Fixing only one bug regresses the others. Both must land together.

## Tests Fixed

- `abs-pos-border-offset-003.html` containers 2/3/4 (currently failing).
- Containers 1/5/6 must remain passing — verify after fix.
- `abs-pos-border-offset-002.html` (sideways-rl/lr, parallel-to-VRL) potentially improved.

## Regression Risk

- `abs-pos-border-offset-001` — same-WM child/CB, `BlockEdge=Start` path unchanged.
- `abs-pos-non-replaced-vrl/vlr` — same-WM all-auto, unchanged.
- Flex abs-pos (`flex_layout.go:1024-1025`) emits `Edge=Start` only — unchanged.

## Open Risk

Trace verification couldn't reconcile Containers 1/5/6 fully. Real risk that the
`childContentPhys` computation needs adjustment for the parallel VRL↔VLR case to
keep them passing. Run all 6 containers after the change before declaring done.

## Critical Files

- `pkg/layout/out_of_flow_layout.go`
- `pkg/layout/block_layout.go`
- `pkg/layout/static_position.go`
- `pkg/layout/writing_mode_converter.go`
