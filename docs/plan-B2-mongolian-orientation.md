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

## Blink References (canonical implementation this plan should mirror)

Per CLAUDE.md §2, study Blink's approach before writing code. These are the
Blink/Chromium files our implementation should be shaped after. Verify file
paths + function names against the current tree before citing them in code
comments — Blink renames are common. The *shapes* (enums, iterator segmentation,
ICU classification driver) are what to mirror.

### UTR#50 classification (what "vertical-script" means)

- `third_party/blink/renderer/platform/text/character.h` / `character.cc` —
  `Character::IsUprightInMixedVertical(UChar32)` is the canonical test for
  "this rune stays upright under `text-orientation: mixed`". Backed by a
  generated table of Unicode `Vertical_Orientation` property values (classes
  `U`, `R`, `Tu`, `Tr` from UTR#50). Our `IsVerticalScriptCharacter` is a
  narrowed subset (explicit Mongolian/Phags-Pa ranges); Blink's table is the
  full UTR#50.
- `third_party/blink/renderer/platform/text/character_property_data.h` (or the
  generated `character_property_data_generated.cc`) — the Unicode property
  lookup table. ICU's `U_VERTICAL_ORIENTATION` is also a viable reference.

### Run segmentation by orientation

- `third_party/blink/renderer/platform/fonts/orientation_iterator.h` /
  `orientation_iterator.cc` — `OrientationIterator::Consume(&limit, &render_orientation)`
  walks a UTF-16 string and returns the next sub-run with a uniform
  `RenderOrientation` value. The enum has (approximately):
  `kKeep` (upright), `kRotateSideways` (90° CW for horizontal glyphs in vertical
  flow), and a `kRotateSidewaysRight` variant for sideways-rl style rotations.
  Blink does NOT decide per-fragment — it segments runs at orientation
  boundaries and hands each sub-run to the shaper. This is the exact pattern
  B2.2 must replicate in `pkg/layout/engine.go`.

### Inline segmentation driver

- `third_party/blink/renderer/core/layout/inline/inline_items_builder.cc` —
  `InlineItemsBuilder::AppendText` is where text items are created. When
  `text-orientation` is `mixed`, it runs `OrientationIterator` over the text
  and emits a separate `InlineItem` per orientation-uniform sub-run. Our
  louis14 equivalent is `pkg/layout/inline_item.go::collectTextNode` +
  `pkg/layout/engine.go` fragment splitting.

### Shaping + upright advance

- `third_party/blink/renderer/platform/fonts/shaping/harfbuzz_shaper.cc` —
  selects HarfBuzz vertical features (`vert`, `vkrn`, `vpal`, `vhal`) and uses
  HarfBuzz vertical metrics for upright glyphs; uses horizontal advance +
  90° rotation for `kRotateSideways`. `Font::GetCharacterAndGlyphMetrics`
  returns the upright em-square advance for CJK/vertical-script glyphs, which
  is what B2.3 (`isVerticalMeasurement`) must use.
- HarfBuzz direction: upright runs use `HB_DIRECTION_TTB`; sideways runs use
  `HB_DIRECTION_LTR` then rotate in paint.

### Baseline selection (relevant to `text-orientation-script-002a`)

- `third_party/blink/renderer/core/style/computed_style.h` —
  `ComputedStyle::GetFontBaseline()` returns `kAlphabeticBaseline` for
  horizontal-tb and for `sideways*`/`vertical* + text-orientation:sideways`;
  returns `kCentralBaseline` for `vertical-*` with `mixed` or `upright`
  orientation. Our `UsesCentralBaselineWithStyle` matches this table.
- `third_party/blink/renderer/core/layout/inline/logical_box_fragment.cc` —
  `BaselineMetrics(FontBaseline)` derives `alignment_ascent`/`alignment_descent`
  from font metrics; for central baseline, ascent == descent == 0.5em.

### Sideways rotation geometry

- `third_party/blink/renderer/core/layout/geometry/writing_mode_converter.cc`
  and `writing_mode_utils.h` — `IsFlippedLinesWritingMode(WritingMode)` is true
  only for `sideways-lr`. This is exactly the predicate our
  `IsSidewaysLRMode(wdm, style)` mirrors (extended to cover
  `vertical-lr + text-orientation:sideways`, which Blink treats equivalently
  during layout).

### Related enums / types to mirror

- `blink::TextOrientation` — `kMixed`, `kUpright`, `kSideways`.
- `blink::FontOrientation` — `kHorizontal`, `kVerticalMixed`, `kVerticalUpright`,
  `kVerticalRotated`, `kVerticalSideways`.
- `blink::RenderOrientation` (from `OrientationIterator`) — `kKeep`,
  `kRotateSideways`, `kRotateSidewaysRight`.

## Implementation Notes for Fresh Agent (2026-04-20)

- B1.2 and B1.3 have already landed on `fix/flexbox-fast` (salvage commit
  `8700eb9c`). Rebase onto current HEAD; do not duplicate the ascent/descent
  swap or `fragmentToBox` broadening.
- `IsSidewaysLRMode(wdm WritingDirectionMode, style *css.Style)` helper now
  exists in `pkg/layout/writing_mode.go` — reuse it.
- `text-orientation` is already inherited (B1.1 via I1 merge `2ef71c5f`).
  Don't re-add to `inheritableProperties`.
- Dispatch prompt MUST require milestone commits (one per B2.x step) and
  restrict file scope to: `pkg/text/orientation.go`,
  `pkg/layout/engine.go`, `pkg/layout/line_breaker.go`,
  `pkg/layout/writing_mode.go`. Any proposed change outside that set must be
  reported back before writing.
- Regression-first testing: after B2.2, run existing `text-orientation-mixed-*`
  tests before the B2 target tests to catch segmentation bugs that would
  reorder mixed-script lines.
