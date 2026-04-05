# Writing Modes Continuation Prompt

Read `.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` first for project principles.
Read `docs/WRITING-MODES-GAP-ANALYSIS.md` for the full categorized failure analysis.

## Current baseline: 405/788 WM passing (was 400)

## What was done (2026-04-04)

**Abspos constraint equation audit (Tier 1 Item 1) — COMPLETE.**

Fixed: overconstrained RTL inline-axis in `out_of_flow_layout.go:201-206`. The old code had separate LTR/RTL branches; per CSS 2.1 §10.3.7 both cases ignore inline-end, so both use `insets.InlineStart + childMargins.InlineStart`. This fixed 5 tests.

Key finding: the remaining 64 "Category B" tests (abspos with `writing-mode` on `<html>`) all have the **green square at the correct pixel position** — verified by rendering both test and reference and comparing green pixel coordinates. The failures come solely from the **indicator image** in the `<p>` tag being at a different Y offset in VRL inline layout (Y≈12 in ours vs Y≈56 in reference). This is a VRL inline image alignment issue, not abspos.

## Three remaining items to work, in order

### 1. ICB vertical root threading (Cat A + E: 39+16 tests, 0% pass rate)

Test families: `abs-pos-non-replaced-icb-vlr` (16), `abs-pos-non-replaced-icb-vrl` (16), `orthogonal-root-resize-icb` (7), plus `clip-rect-vlr/vrl` (16) which are blocked on this.

Root cause per gap analysis: when the ICB itself is vertical, `out_of_flow_layout.go` resolves insets against the wrong physical dimensions. Blink's `NGOutOfFlowLayoutPart` tracks `container_builder_writing_mode_` vs `candidate_writing_mode_` separately. Our OOF part doesn't distinguish these for ICB cases.

Study Blink's `NGOutOfFlowLayoutPart::LayoutCandidate()` when `container_is_icb` is true.

Test commands:
```bash
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-icb' ./pkg/visualtest/ -timeout 120s
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/clip-rect-v' ./pkg/visualtest/ -timeout 120s
```

### 2. VRL inline image alignment (~64 Cat B tests blocked on this)

The 64 "Category B" tests with `writing-mode` on `<html>` fail only because the indicator `<p><img>` is at the wrong Y position in VRL/VLR inline layout. The abspos content (green square) is pixel-perfect. The Y offset difference (44px in one sample) suggests incorrect image baseline/alignment handling in vertical inline formatting contexts.

Fixing this would recover all 64 tests without any abspos code changes.

### 3. Bidi embedding/override/isolation (Cat C: 48 tests, Tier 2)

Missing subsystem: inline item collection needs UAX#9 control character injection based on `unicode-bidi` CSS property. Currently only distinguishes bidi level 0 vs 1. Study Blink's `CollectInlines()` + `BidiParagraph`.

```bash
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 120s
```

## Key files

- `pkg/layout/out_of_flow_layout.go` — OOF constraint solver (audited, correct now)
- `pkg/layout/engine.go:98-141` — root constraint space setup (handles vertical root WDM)
- `pkg/layout/block_layout.go` — block child positioning
- `pkg/layout/writing_mode_converter.go` — logical↔physical conversions
- `pkg/layout/constraint_space.go` — axis swapping for orthogonal children
- `pkg/layout/inline_layout.go` / `line_breaker.go` — inline formatting context
