# Plan B5: sideways-lr flex column main axis (sideways-lr-main-axis)

## Root Cause

`buildItemConstraintSpace` in `pkg/layout/flex_layout.go` (column branch, lines 1600-1626)
sets `IsFixedBlockSize=true` with the flex-resolved main size, but does NOT set an
override flag. `CalculateInitialFragmentGeometry` (`pkg/layout/fragment_geometry.go`
line 523) then re-derives the block size from CSS via `ResolveBlockSize`, which for
sideways-lr reads CSS `width`. For 20×20 items this coincidentally matches; for the
larger items in this test it diverges and items overlap / fall outside the container.

Main repo has the fix: `SetIsBlockSizeOverride(true)` is called at flex_layout.go:3418
(row branch) and 3470 (column branch). The worktree is missing both the
`IsBlockSizeOverride` field on `ConstraintSpace` and the call sites.

`computeMainIsItemInline` itself is correct: sideways-lr container + sideways-lr item
+ `flex-direction: column` → `mainIsItemInline=false` (main maps to item's block).

## Changes

### `pkg/layout/constraint_space.go`

1. Add field `IsBlockSizeOverride bool` to `ConstraintSpace` (after `IsFixedBlockSizeIndefinite`).
2. Add builder method `SetIsBlockSizeOverride(v bool) *ConstraintSpaceBuilder`.

### `pkg/layout/fragment_geometry.go` (around line 523)

Change:
```go
} else if space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
```
to gate on `!space.IsBlockSizeOverride`, and add a new branch:
```go
} else if space.IsBlockSizeOverride && space.IsFixedBlockSize {
    borderBoxBlock = space.AvailableSize.BlockSize
}
```

### `pkg/layout/flex_layout.go::buildItemConstraintSpace`

- Row flex branch (after `b.SetIsFixedInlineSize(true)` ~line 1596): add
  `b.SetIsBlockSizeOverride(true)` (mirrors main repo line 3418).
- Column flex branch (after `b.SetIsFixedBlockSize(true)` ~line 1624): add
  `b.SetIsBlockSizeOverride(true)` (mirrors main repo line 3470).

### Doc-only

Update `computeMainIsItemInline` comment table (lines 52-56) to include the sideways-lr
case row.

## No-change verification

- `measureFlexMinMax` column branch — formula is architecturally sound for parallel
  parent/child; no change.
- `resolveItemMargins` — `ResolveMargins(style, childWDM, contentInlineSize)` already
  resolves percentages against the containing block's inline-size per CSS §8.

## Tests Fixed

- `sideways-lr-main-axis.html` (primary).
- `flexbox-writing-mode-slr.html`, `flexbox-writing-mode-slr-row-mix.html` (related).

## Regression Risk

- The override semantics are exactly what main repo ships, so behavior aligns with the
  shipped code path. Low risk.
- Row-flex orthogonal items: must re-run row-flex tests with vertical WM to confirm.
- `flex-basis` vs `height` divergence: previously CSS won by accident; now flex value
  wins per §9.5. Spec-correct but may surface pre-existing test issues — verify by
  running flex tests.

## Critical Files

- `pkg/layout/constraint_space.go`
- `pkg/layout/fragment_geometry.go`
- `pkg/layout/flex_layout.go`
- `pkg/layout/min_max_sizing.go` (verify only)
