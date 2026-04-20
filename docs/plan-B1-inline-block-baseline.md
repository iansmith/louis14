# Plan B1: Inline-block baseline in VLR + text-orientation:sideways (inline-block-alignment-007)

## Three Coupled Bugs

### Bug 1 — `text-orientation` not inherited

`pkg/css/cascade.go` `inheritableProperties` map (~lines 680-690) is missing
`"text-orientation": true`. CSS Writing Modes L3 §6.1 makes it inherited. Today
descendants of `#lr-sideways` see no `text-orientation`, so
`UsesCentralBaselineWithStyle` returns `true` (central baseline) for all children
in VLR mode — wrong for sideways orientation.

### Bug 2 — Wrong ascent/descent for VLR/VRL + sideways alphabetic baseline

`pkg/layout/inline_layout.go::computeLineMetricsEx` (~line 885) treats typographic
ascent (e.g., 0.8em for Ahem) as alignment_ascent. Correct for `sideways-lr`/`sideways-rl`
keywords, wrong for `vertical-lr/rl + text-orientation: sideways`. After the 90° CW
rotation, the alphabetic baseline lands at `descent` from block-start. So:
- `alignment_ascent = typographic_descent` (0.2em)
- `alignment_descent = typographic_ascent` (0.8em)

Bug 1 currently masks Bug 2 (we never enter the alphabetic path); fixing Bug 1 alone
would still produce wrong baselines without this swap.

### Bug 3 — `IsSidewaysLR` not set for VLR+sideways

`pkg/layout/engine.go::fragmentToBox` (~lines 296-298) only sets `IsSidewaysLR` when
`WM == WritingModeSidewaysLR`. For `vertical-lr + text-orientation: sideways` the WM
stays `WritingModeVerticalLR`, so the renderer takes the upright-stacked path. Ahem
masks the visual; real fonts render wrong glyphs.

## Changes

### `pkg/css/cascade.go`

Add `"text-orientation": true` to `inheritableProperties`.

### `pkg/layout/inline_layout.go`

At line ~453 derive:
```go
sidewaysVLR := !centralBaseline &&
    (wdm.WM == WritingModeVerticalLR || wdm.WM == WritingModeVerticalRL)
```
Thread into `createLineBoxEx` → `computeLineMetricsEx`.

In `computeLineMetricsEx`, for `InlineItemText` and `InlineItemOpenTag` branches when
`sidewaysVLR`:
```go
typographicAscent := text.FontAscentFromFont(fontSize, fontPath)
ascent  = fontSize - typographicAscent  // alignment_ascent  = descent
descent = typographicAscent              // alignment_descent = ascent
```

`InlineItemAtomicInline` needs no change — `LayoutResult.LastBaseline` is already
correct once children layout with the swap. `lastBaselineOffset` formula at line ~462
also unchanged.

### `pkg/layout/engine.go`

In `fragmentToBox`, broaden:
```go
box.IsSidewaysLR = WM == WritingModeSidewaysLR ||
    (WM == WritingModeVerticalLR && isSidewaysOrientation(frag.Style))
box.IsSidewaysRL = WM == WritingModeSidewaysRL ||
    (WM == WritingModeVerticalRL && isSidewaysOrientation(frag.Style))
```

## Verification (Ahem 60/120/30)

| Item | fontSize | alignment_ascent |
|------|---------|------------------|
| outer "É" | 60 | 12 |
| `#inline-block` | — | LastBaseline=204 |
| small "É" | 30 | 6 |

`maxAscent = 204`; all items align to baseline at 204px from div start. Matches reference.

## Tests Fixed

- `inline-block-alignment-007.xht` (primary, currently 8.4% diff).

## Regression Risk

- Step 1 changes global cascade. Audit any test with `text-orientation` set on a
  parent — children now inherit (spec-correct, but may shift current pixel diffs).
- Step 2 only fires for VLR/VRL + sideways alphabetic mode (narrow guard via
  `sidewaysVLR` flag). `sideways-lr/rl` keywords untouched.
- Step 3 only changes painter behavior in VLR/VRL+sideways. Ahem squares hide most
  visual change; real-font tests benefit.

## Critical Files

- `pkg/css/cascade.go`
- `pkg/layout/inline_layout.go`
- `pkg/layout/engine.go`
- `pkg/layout/writing_mode.go` (no change but sanity check `UsesCentralBaselineWithStyle`)
