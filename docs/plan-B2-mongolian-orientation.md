# Plan B2: Mongolian / vertical-script text-orientation (text-orientation-script-001a)

## Root Cause

`pkg/layout/engine.go` lines 411-419 makes a single monolithic orientation decision
per fragment based on the first character. UTR#50 requires per-character
classification: scripts native to vertical writing (Mongolian, Phags-Pa) keep their
upright glyphs even in `text-orientation: mixed`, while non-vertical scripts rotate.

`pkg/text/orientation.go::ShouldRotateSideways` exists but is dead code — never
called from the layout pipeline.

`pkg/layout/writing_mode.go::UsesCentralBaselineWithStyle` (lines 113-123): for
test 002 (baseline alignment of vertical scripts) the central-baseline detection
is style-based. Mongolian content with default `text-orientation: mixed` should
use central baseline (per CSS Writing Modes §4.3) — verify the lookup matches
the spec table.

`pkg/layout/line_breaker.go::isVerticalMeasurement` (lines 123-133): measurement
must use the upright em-square advance for vertical-script characters, not the
rotated horizontal advance.

## Changes

### `pkg/text/orientation.go`

Add:
```go
// IsVerticalScriptCharacter reports whether r belongs to a script natively
// written vertically (UTR#50: keep upright in mixed orientation).
func IsVerticalScriptCharacter(r rune) bool {
    switch {
    case r >= 0x1800 && r <= 0x18AF:   // Mongolian
    case r >= 0xA840 && r <= 0xA87F:   // Phags-Pa
    case r >= 0x11660 && r <= 0x1166C: // Mongolian Supplement
        return true
    }
    return false
}
```

Wire `ShouldRotateSideways` (or a new per-rune classifier) into the layout
pipeline.

### `pkg/layout/engine.go` (lines 408-419)

Replace the monolithic per-fragment decision with per-character (or per-cluster)
orientation. When a fragment contains a mix of vertical-script and other
characters, split into sub-fragments OR annotate each cluster with its
orientation flag so painting can rotate selectively.

### `pkg/layout/writing_mode.go::UsesCentralBaselineWithStyle` (lines 113-123)

Verify central-baseline selection for `mixed` + vertical-script content matches
the CSS table. Likely correct already — confirm with test 002.

### `pkg/layout/line_breaker.go::isVerticalMeasurement` (lines 123-133)

Switch to upright em-square advance when the cluster is a vertical-script
character. Today it uses the horizontal advance regardless.

## Tests Fixed

- `text-orientation-script-001a` (Mongolian).
- `text-orientation-script-002a` (baseline alignment for vertical-script content).

## Regression Risk

- Fragment splitting can change line-breaking opportunities for mixed-script text.
  Audit any test mixing vertical-script and Latin in the same `<span>`.
- Central-baseline detection change (if needed) affects all VRL/VLR vertical-script
  tests — most should already be on the central path, so risk is low but verify.

## Critical Files

- `pkg/text/orientation.go`
- `pkg/layout/engine.go`
- `pkg/layout/writing_mode.go`
- `pkg/layout/line_breaker.go`
