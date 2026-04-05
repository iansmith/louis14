# CSS Writing Modes: Foundational Investigation & Categorized Gap Analysis

**Date**: 2026-04-04
**Current baseline**: 400/788 passing (50.8%)
**Total failures**: 388

---

## Principles

1. **Study Blink first** — understand their types, algorithms, abstractions before writing any code
2. **Foundational correctness** — fix root causes for ALL cases, not individual near-passing tests
3. **All tests must pass** — 0.5% diff is a failure just like 28%
4. **No easy-win hunting** — don't filter by error percentage

---

## What Blink Does (Architecture Summary)

Blink's LayoutNG has a clean, powerful design for writing modes:

- **Layout works entirely in logical coordinates** — `ConstraintSpace` carries `WritingDirectionMode` + logical available sizes -> layout algorithms position children with `LogicalOffset` -> `FragmentBuilder` accumulates in logical space
- **Conversion happens exactly once** — at `PhysicalBoxFragment::Create()`, a `WritingModeConverter` converts every child's `LogicalOffset` -> `PhysicalOffset`. The converter needs the parent's WDM, the parent's physical size (outer), and each child's physical size (inner)
- **`WritingDirectionMode`** is the key carrier type — stored in `ConstraintSpace`, `FragmentBuilder`, and `ComputedStyle`
- **Special handling**: `BfcOffset` is direction-agnostic (not the same as logical), `LogicalStaticPosition` for OOF tracks which edge the position refers to, inline layout uses a hybrid physical/logical model

Louis14 already mirrors this architecture well — `WritingDirectionMode`, `WritingModeConverter`, `LogicalSize/Offset/Edges`, `ConstraintSpace` with axis swapping are all in place and tested. The gaps are in specific subsystems that don't yet fully leverage this infrastructure.

---

## Categorized Failure Analysis (388 failures, grouped by root cause)

### Category A: Abspos in Vertical ICB (0% pass rate, ~39 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| abs-pos-non-replaced-icb-vlr | 0 | 16 | 0% |
| abs-pos-non-replaced-icb-vrl | 0 | 16 | 0% |
| orthogonal-root-resize-icb | 0 | 7 | 0% |

**Root cause**: When the ICB (root element) itself is vertical, `out_of_flow_layout.go` resolves insets against the wrong physical dimensions. Blink's `NGOutOfFlowLayoutPart` specifically tracks `container_builder_writing_mode_` vs `candidate_writing_mode_` — the ICB's writing mode is separate from the containing block's writing mode. Our OOF layout doesn't distinguish these.

**Blink reference**: `NGOutOfFlowLayoutPart::LayoutCandidate()` when `container_is_icb` is true.

**Infrastructure investment**: Fix OOF layout to properly handle the ICB as a vertical containing block. This is a **wire-up** fix — the converter machinery exists, but the ICB case doesn't thread the writing mode correctly.

---

### Category B: Abspos Constraint Equation in Vertical Modes (69% pass rate, ~68 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| abs-pos-non-replaced-vlr | 78 | 34 | 69.6% |
| abs-pos-non-replaced-vrl | 78 | 34 | 69.6% |

**Root cause**: The 69% pass rate means the basic solver works but edge cases fail — auto margins on abspos in vertical modes, overconstrained cases where direction matters, and static position when the abspos ancestor is vertical. The code in `out_of_flow_layout.go` does `PhysicalInsetsToLogical` correctly in isolation, so the bug is likely in which physical dimensions are passed as the CB size, or in how the overconstrained direction adjustment (line 203-206) works in vertical modes.

**Blink reference**: `NGOutOfFlowLayoutPart::ComputeOOFInlineDimensions()` overconstrained handling.

**Infrastructure investment**: Audit the constraint equation solver for all 10 writing-direction combinations. This is the single highest-impact family (68 failures).

---

### Category C: Bidi Embedding/Override/Isolation (~48 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| bidi-embed | 2 | 9 | 18% |
| bidi-override | 3 | 9 | 25% |
| bidi-plaintext | 3 | 8 | 27% |
| bidi-isolate | 4 | 7 | 36% |
| bidi-isolate-override | 5 | 7 | 42% |
| bidi-normal | 7 | 4 | 64% |
| bidi-unset | 6 | 4 | 60% |

**Root cause**: The graduated pass rates (18% to 64%) reveal the UAX#9 implementation handles basic reordering but not the full embedding/override/isolation level stack. `ResolveBidiLevels()` in `bidi.go` uses `golang.org/x/text/unicode/bidi` which gives per-run directions, but the code only distinguishes level 0 (LTR) vs level 1 (RTL) — it doesn't handle deeper nesting levels from `unicode-bidi: embed/override/isolate`. Each element needs matching UAX#9 control characters (LRE/RLE/PDF, LRO/RLO/PDF, LRI/RLI/FSI/PDI) injected into the inline item stream *before* the bidi algorithm runs.

**Blink reference**: `BidiParagraph` and `NGInlineNode::CollectInlines()` for how bidi control characters are injected around elements with `unicode-bidi` properties.

**Infrastructure investment**: This is a **missing subsystem** — the inline item collection (`CollectInlines`) needs to inject UAX#9 control characters based on `unicode-bidi` and `direction` CSS properties. Then the bidi resolver needs to preserve actual nesting levels (not just 0/1).

---

### Category D: Orthogonal Sizing (~20 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| sizing-orthog-htb-in-vlr | 10 | 10 | 50% |
| sizing-orthog-htb-in-vrl | 8 | 10 | 44% |

**Root cause**: When a horizontal child is inside a vertical parent (or vice versa), the child's auto inline-size should fall back to the ICB's cross-axis dimension per CSS Writing Modes SS10.3.2. The constraint space builder handles this for non-root containers, but the root setup in `engine.go` doesn't call `SetOrthogonalFallbackInlineSize()`. Additionally, min/max sizing for orthogonal children requires performing an actual layout to get the child's block-size, with cycle detection (Blink's `NGOrthogonalWritingModeRootInlineSize()`).

**Infrastructure investment**: Two parts — (1) a near-trivial fix to add orthogonal fallback at the root, and (2) a medium-effort layout cache for orthogonal min/max sizing with cycle breaking.

---

### Category E: Clip Rect in Vertical Modes (0% pass rate, ~16 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| clip-rect-vlr | 0 | 8 | 0% |
| clip-rect-vrl | 0 | 8 | 0% |

**Root cause**: Per the existing docs, the clip-rect tests render clipped content correctly but position surrounding text wrong because the root `<html>` has `writing-mode: vertical-*`. This ties directly to **Category A** (ICB vertical mode propagation). Fixing the root vertical mode threading should unlock these.

---

### Category F: Float/Clear in Vertical Modes (~22 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| float-vlr | 3 | 4 | 43% |
| float-vrl | 4 | 2 | 67% |
| float-clear-vlr | 1 | 3 | 25% |
| float-clear-vrl | 1 | 3 | 25% |
| contiguous-floated-table-vlr | 0 | 4 | 0% |
| contiguous-floated-table-vrl | 0 | 4 | 0% |
| ortho-htb-alongside-vrl-floats | 0 | 4 | 0% |
| float-lft/rgt-orthog-* | 0 | 8 | 0% |

**Root cause**: CSS `float: left/right` are **physical** values. In vertical modes, "left" and "right" map to block-start/block-end, not inline-start/inline-end. The float placement and exclusion space logic needs a physical-to-logical float mapping. Additionally, orthogonal floats (horizontal float inside vertical container or vice versa) need special sizing.

**Blink reference**: `ResolveFloating()` in `style_utils.cc` maps physical float values to logical `EFloat`. `NGExclusionSpace` works entirely in logical coordinates.

**Infrastructure investment**: Create `MapPhysicalFloatToLogical(side, wdm)` and apply it at float placement and clear resolution.

---

### Category G: Table Features in Vertical Modes (~20 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| border-conflict-element-vlr | 0 | 6 | 0% |
| border-conflict-element-vrl | 0 | 6 | 0% |
| border-spacing-vlr | 0 | 2 | 0% |
| border-spacing-vrl | 0 | 2 | 0% |
| caption-side-vlr | 0 | 2 | 0% |
| caption-side-vrl | 0 | 2 | 0% |

**Root cause**: Three distinct table features: border-collapse (CSS 2.1 SS17.6.2.1 conflict resolution — not implemented at all), border-spacing physical-to-logical mapping, and caption support. All are 0% pass rate = missing features.

**Infrastructure investment**: Border-collapse is a significant algorithm (conflict resolution rules). Border-spacing and caption-side are smaller but still require writing-mode-aware positioning.

---

### Category H: Vertical Margin/Padding/Percent Resolution (~22 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| margin-vlr | 0 | 1 | 0% |
| margin-vrl | 0 | 1 | 0% |
| margin-collapse-vlr | 4 | 6 | 40% |
| margin-collapse-vrl | 7 | 3 | 70% |
| percent-margin-vlr | 0 | 3 | 0% |
| percent-margin-vrl | 0 | 3 | 0% |
| percent-padding-vlr | 1 | 2 | 33% |
| percent-padding-vrl | 1 | 2 | 33% |

**Root cause**: The 0% on `margin-vlr/vrl` and `percent-margin-*` means margin resolution in vertical modes has a fundamental physical-to-logical mapping issue. Margin collapsing works partially (40-70%), suggesting the collapsing logic itself is fine but the initial margin values are wrong. Percentage margins/padding all resolve against the containing block's inline-size (per spec), but the code may be resolving against the wrong physical dimension in vertical modes.

**Infrastructure investment**: Audit how physical margin/padding CSS values get converted to logical values in vertical formatting contexts.

---

### Category I: Block Flow Direction (~14 tests)

| Family | Pass | Fail | Rate |
|--------|------|------|------|
| block-flow-direction-slr | 6 | 6 | 50% |
| block-flow-direction-vlr | 9 | 2 | 82% |
| block-flow-direction-vrl | 11 | 2 | 85% |
| block-flow-direction-srl | 11 | 1 | 92% |

**Root cause**: sideways-lr at 50% is the outlier. VLR/VRL are mostly working (82-85%). The sideways-lr failures likely come from its unique inline direction (bottom-to-top) not being fully handled in block child positioning.

---

### Category J: Miscellaneous Vertical Features (~30+ tests)

| Family | Fail | Root Cause |
|--------|------|-----------|
| ch-units-vrl | 8 | `ch` unit should use height of "0" in vertical modes, not width |
| inline-block-alignment | 6 | Baseline alignment for inline-blocks in vertical |
| available-size | 13 | Available size computation in vertical — overlaps with orthogonal sizing |
| overconstrained-rel-pos-* | 8 | Relative positioning overconstrained resolution in vertical + RTL |
| box-offsets-rel-pos-vlr/vrl | 4 | Relative positioning box offsets in vertical |
| background-*-vrl | 7 | Background painting in vertical modes |
| Various singletons | ~10 | Scattered one-off issues |

---

## Recommended Investment Priority (Foundational Order)

Based on the principle of fixing root causes that unlock **whole families**, and studying Blink's architecture:

### Tier 1: Highest Leverage Infrastructure (~107+ tests)

1. **Abspos constraint equation audit for all 10 WDMs** (Cat B: 68 tests) — The abspos solver is the largest single failure family. A systematic audit of the constraint equation in `out_of_flow_layout.go` across all writing-direction combinations would be the highest-leverage fix. Blink's `ComputeOOFInlineDimensions()` / `ComputeOOFBlockDimensions()` is the reference.

2. **ICB vertical root threading** (Cat A + E: ~39 tests, unlocks 16 more from clip-rect) — Wire-up fix, not algorithmic. The infrastructure exists; the root just doesn't thread it through. This is a prerequisite for many other vertical-mode families.

### Tier 2: Missing Subsystem (~48 tests)

3. **Bidi embedding/override/isolation levels** (Cat C: 48 tests) — A missing subsystem. Requires injecting UAX#9 control characters into the inline item stream based on `unicode-bidi` CSS property, then preserving actual nesting levels (not just 0/1). Blink's `CollectInlines()` + `BidiParagraph` is the reference.

### Tier 3: Physical-to-Logical Mapping Fixes (~42 tests)

4. **Float physical-to-logical mapping** (Cat F: ~22 tests) — `MapPhysicalFloatToLogical()` needed for correct float placement in all writing modes.

5. **Margin/padding resolution in vertical modes** (Cat H: ~22 tests) — Audit physical-to-logical conversion of margin/padding values and percentage resolution base.

### Tier 4: Feature Gaps (~34 tests)

6. **Orthogonal sizing with layout cache** (Cat D: ~20 tests) — Requires a per-layout-pass cache for laying out orthogonal children during min/max computation with cycle detection.

7. **Table border-collapse + vertical features** (Cat G: ~20 tests) — Entirely missing table features. Border-collapse is the most complex.

### Tier 5: Targeted Fixes (~25+ tests)

8. `ch` units in vertical (8 tests), inline-block baseline alignment (6), overconstrained rel-pos (8), background painting (7), etc.

---

## Key Takeaway

The infrastructure (WritingModeConverter, LogicalSize/Offset, ConstraintSpace axis-swapping) is **solid and Blink-aligned**. The failures are not in the abstraction layer — they're in subsystems that don't yet fully use it:

- **OOF layout** doesn't handle vertical ICB or all constraint equation edge cases
- **Bidi** only does level 0/1 instead of the full UAX#9 embedding stack
- **Floats** use physical values without logical mapping
- **Margins/padding** have percentage resolution gaps in vertical modes
- **Tables** are missing border-collapse entirely

The right approach is to take these one subsystem at a time, study the corresponding Blink code, and make each subsystem properly writing-mode-aware using the existing converter infrastructure.

---

## Test Commands

```bash
# Full WM suite (current: 400/788)
go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS: TestWPT'

# Category A: ICB abspos
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-icb' ./pkg/visualtest/ -timeout 120s

# Category B: Abspos vertical
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-v(lr|rl)' ./pkg/visualtest/ -timeout 300s

# Category C: Bidi
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 120s

# Category D: Orthogonal sizing
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s

# Category F: Floats
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/float' ./pkg/visualtest/ -timeout 120s

# Category G: Table vertical
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/border-conflict' ./pkg/visualtest/ -timeout 120s

# Category H: Margins/padding
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/margin' ./pkg/visualtest/ -timeout 120s
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/percent' ./pkg/visualtest/ -timeout 120s
```
