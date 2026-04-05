# Continuation Prompt: Orthogonal Sizing in Vertical Writing Modes

## Task

Fix the 20 failing `sizing-orthog-htb-in-*` WPT tests. These test how an HTB (horizontal-tb) block with `width: auto` is sized inside a vertical (VLR or VRL) containing block. The test structure places `<html>` in `writing-mode: vertical-lr` (or vrl), and nests a `div` with `writing-mode: horizontal-tb` inside.

**Current: 18/38 pass (VLR: 10/20 pass, VRL: 8/18 pass). Target: all 38.**

## What's Already Implemented (Don't Rewrite)

The orthogonal sizing infrastructure is already in place and Blink-aligned:

1. **ConstraintSpaceBuilder** (`constraint_space.go:89-121`): Auto-detects orthogonal children via `IsOrthogonalTo()`, swaps inline↔block in `SetAvailableSize()`, applies `OrthogonalFallbackInlineSize` when parent block-size is Indefinite.

2. **orthogonalFallbackSize** (`block_layout.go:731-738`): Returns `ctx.ViewportWidth` for HTB children, `ctx.ViewportHeight` for vertical children. Used as ICB cross-size fallback per CSS Writing Modes §10.3.2.

3. **Shrink-to-fit for orthogonal roots** (`fragment_geometry.go:363-372`): `CalculateInitialFragmentGeometry` triggers shrink-to-fit when `IsOrthogonalWritingModeRoot=true`, using `ComputeMinMaxSizes` → `ShrinkToFit(available)`.

4. **Orthogonal min/max with cycle detection** (`min_max_sizing.go:230-342`): `measureBlockMinMax` detects orthogonal children → calls `measureOrthogonalChild` which does actual layout with cycle detection cache. Mirrors Blink's `NGOrthogonalWritingModeRootInlineSize()`.

5. **Writing-mode change creates new BFC** (`block_layout.go:190`): `isChildNewFC := createsFormattingContext(childStyle) || wdm.WM != childWDM.WM` per CSS Writing Modes §4.3.

## Failing vs Passing Test Pattern — The Key Clue

The tests are organized in sub-batches of 24 (mirrored VLR/VRL):

| Tests | Parent width | Sibling text | Passes? |
|-------|-------------|-------------|---------|
| 001-003 | auto (indefinite) | "Sentence before/after" paragraphs | **FAIL** |
| 004-006 | auto (indefinite) | No siblings | PASS |
| 007-009 | 400px (definite) | "Sentence before/after" paragraphs | **FAIL** |
| 010-012 | 400px (definite) | No siblings | PASS |
| 013-015 | auto, no body margins | "Sentence before/after" | **FAIL** |
| 016-018 | auto, no body margins | No siblings | PASS |
| 019-021 | 400px, no body margins | "Sentence before/after" | **FAIL** |
| 022-024 | 400px, no body margins | No siblings | PASS |

**The failing tests ALL have parallel sibling `<p>` elements alongside the orthogonal block.** When the orthogonal block is the sole child, it passes. This strongly suggests the issue is in how the parent's block layout handles multiple children (parallel + orthogonal) when computing sizes.

Within each failing group of 3:
- `*-001`/`*-007`/`*-013`/`*-019`: Long wrapping text (max-content > constraint) → width = constraint
- `*-003`/`*-009`/`*-015`/`*-021`: Short text (max-content < constraint) → width = max-content
- `*-002`/`*-008`/`*-014`/`*-020`: Very long word (min-content > constraint) → width = min-content (PASS if no siblings → but these DO fail with siblings)

Wait — 002/008/014/020 FAIL too. So all tests with siblings fail regardless of text length.

## Pixel Diffs for Failing Tests (VLR)

| Test | Diff | Description |
|------|------|-------------|
| 001 | 3.1% | Long text, auto parent |
| 003 | 0.6% | Short text, auto parent |
| 007 | 3.2% | Long text, 400px parent |
| 008 | 1.5% | Long word, 400px parent |
| 009 | 0.6% | Short text, 400px parent |
| 013 | 3.4% | Long text, auto, no margins |
| 015 | 0.6% | Short text, auto, no margins |
| 019 | 3.2% | Long text, 400px, no margins |
| 020 | 1.5% | Long word, 400px, no margins |
| 021 | 0.6% | Short text, 400px, no margins |

## Visual Analysis (test 001)

**Test output**: "Sentence before." renders vertically in column 1. The orthogonal blue-bordered div renders horizontally starting from column 2 — but text extends far to the RIGHT, overflowing the viewport. "Sentence after." is NOT visible (pushed off-screen or missing).

**Reference**: "Sentence before." and "Sentence after." as two vertical columns close together. The orthogonal block between them is narrow (text wraps within the constraint width).

The test output shows the orthogonal block's width is MUCH too large — as if it's using max-content or an unconstrained width rather than shrink-to-fit at the ICB/parent constraint.

## Where to Investigate

### Hypothesis 1: measureBlockMinMax returns wrong contribution for mixed parallel+orthogonal children

`measureBlockMinMax()` (`min_max_sizing.go:230-285`) iterates all children and takes the MAX of their inline contributions. For orthogonal children, it calls `measureOrthogonalChild()` which returns the child's block-size as the contribution. But this block-size depends on how the child was laid out — which depends on the available inline-size, which comes from the fallback.

When the container has sibling `<p>` elements, the container's own min/max is being computed (for the container's PARENT to determine the container's size). The parallel `<p>` children contribute small inline sizes (their text width). The orthogonal child contributes its block-size (physical height after wrapping). The container's min/max = max of all children.

**Possible bug**: The orthogonal child's contribution might be computed with wrong available sizes, or the parallel children's margins might be misresolved when the parent is vertical.

### Hypothesis 2: The orthogonal child's available inline-size is wrong when siblings exist

The constraint space for the ortho child is built during the container's block layout. The `childInlineForSpace` accounts for float exclusions and margins. Maybe the float or margin computation is wrong in vertical modes, narrowing or widening the available space incorrectly.

### Hypothesis 3: The container's block-size is computed wrong

The container with `width: auto` (block-size auto in VLR) shrinks to its content. Its content includes the orthogonal block's block-size (physical height). If this height is computed wrong, the container overflows.

### Hypothesis 4: The auto-height-clear-floats fix (hasOwnFloats) affects this

The recent fix restricts auto-height-clear to elements with own floats. Check if this inadvertently affects orthogonal containers with auto block-size.

## Key Files to Study

| File | Lines | What |
|------|-------|------|
| `block_layout.go` | 180-206 | Constraint space building for children (isChildNewFC, orthogonal handling) |
| `block_layout.go` | 731-738 | orthogonalFallbackSize() |
| `constraint_space.go` | 89-121 | ConstraintSpaceBuilder, axis swapping |
| `fragment_geometry.go` | 342-400 | CalculateInitialFragmentGeometry, shrink-to-fit path |
| `min_max_sizing.go` | 230-342 | measureBlockMinMax, measureOrthogonalChild |
| `engine.go` | 95-144 | Root constraint space (VLR root) |

## Blink's Approach

Blink's key function is `NGOrthogonalWritingModeRootInlineSize()` which:
1. Walks up the containing block chain looking for a definite block-size ancestor
2. If found, uses that as the available inline-size for the orthogonal child
3. If not found, uses the ICB cross-axis size
4. Uses a layout cache to avoid redundant layouts during min/max sizing

Our `measureOrthogonalChild()` already mirrors this. The cache + cycle detection is in `LayoutContext.OrthogonalLayoutCache`.

## Test Commands

```bash
# All orthogonal sizing tests
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog' ./pkg/visualtest/ -timeout 120s

# Specific failing test
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/sizing-orthog-htb-in-vlr-001' ./pkg/visualtest/ -timeout 30s

# Full WM suite (current: 585/787)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes' ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS:'

# CSS2 regression check (current: 95/99)
go test -v -run 'TestWPTReftests$' ./pkg/visualtest/ -timeout 300s 2>&1 | grep 'Summary:'
```

## How to Debug

Add targeted `fmt.Printf` in `block_layout.go` inside the BFC child loop, guarded by `if wdm.IsVertical()`, to trace:
- Each child's type (parallel vs orthogonal)
- `childInlineForSpace`, `blockForChild` values
- The constraint space's AvailableSize after building
- The child's resulting fragment size

Compare output between test 001 (FAIL, has sibling text) and test 004 (PASS, no sibling text) to find where they diverge.

## Recent Changes to Be Aware Of

Three fixes were just committed (commit 730ded3c):
1. **line_breaker.go**: `finishLine()` trailing-whitespace trimming skips float/OOF items
2. **block_layout.go**: Clearance prevents parent-child margin collapsing (`hasClearance` flag)
3. **block_layout.go**: Auto-height-clear-floats restricted to elements with `hasOwnFloats`. Writing-mode change added as BFC trigger.

These may interact with orthogonal sizing — especially fix #3 which changes when `isChildNewFC` is true for orthogonal children.
