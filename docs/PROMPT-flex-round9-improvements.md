# Flex Round 9: Top 6 Improvements

Current state: 553 pass / 76 fail (87.9% pass rate) across 629 tests.
Previous rounds fixed float/BFC interaction, font metrics, line-height:normal, and formatting-interop.

**These six targets ALL touch `pkg/layout/flex_layout.go` and CANNOT be parallelized.** They must be worked sequentially — complete each target, commit, verify no regressions, then proceed to the next.

---

## Target 1: Baseline Alignment Rewrite (~11 tests)

### Problem
Baseline alignment of flex items is fundamentally incomplete. The current code uses a single `maxAscent`/`maxDescent` pair per line, but the CSS Align 3 spec requires **two baseline sharing groups** (major and minor) per flex line. Additionally, several baseline calculation steps are missing: wrap-reverse baseline flipping, per-group margin handling, baseline clamping removal, and the zero-baseline guard.

### Affected Tests (~11 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| flexbox-align-self-baseline-horiz-001a | 26173 | Mixed block items, different font sizes |
| flexbox-align-self-baseline-horiz-001b | 26173 | Same test, `last baseline` variant |
| flexbox-align-self-baseline-horiz-003 | 3938 | Cross-axis margins with wrap-reverse |
| flexbox-align-self-baseline-horiz-006 | 8436 | Baseline with replaced elements |
| flexbox-align-self-baseline-horiz-008 | 28520 | Baseline with nested flex |
| flexbox-baseline-align-self-baseline-horiz-001 | 402 | Container's own baseline |
| flexbox-baseline-multi-item-horiz-001a | 355 | Multi-item baseline in row |
| flexbox-baseline-multi-item-horiz-001b | 355 | Multi-item baseline, last baseline |
| flexbox-baseline-multi-line-horiz-002 | 616 | Multi-line baseline distribution |
| flexbox-baseline-multi-line-vert-001 | 2077 | Multi-line baseline in column |
| flexbox-baseline-multi-line-vert-002 | 1847 | Multi-line baseline in column, last |

### Root Cause (from code analysis)
In `flex_layout.go`:
- **Line 180-186**: `flexItem` struct has `baseline`, `hasBaseline`, `lastBaseline` but no baseline group assignment (major vs minor).
- **Line 630-680**: Line sizing uses a single `maxAscent`/`maxDescent` pair. Should be four accumulators: `majorAscent`, `majorDescent`, `minorAscent`, `minorDescent`.
- **Line 640-643**: Baseline is clamped to `item.crossSize` — Blink does NOT clamp; baselines outside the border box are valid.
- **Line 637**: Guard `if bl > 0` incorrectly excludes items with zero baseline (baseline at top of border box).
- **Line 1044-1181**: Cross-axis offset for baseline items uses `sharedBaseline - bl` uniformly. Should use major baseline for major-group items, minor baseline for minor-group items (offset from cross-end).
- **Line 1323-1400**: Container baseline uses raw item baseline. Should use per-line major/minor baselines.

### What Blink Does
**Key files**: `flex_layout_algorithm.cc`, `flex_line.h`, `baseline_utils.h`

Blink's algorithm:
1. **Per-item setup**: Computes `baseline_writing_mode` via `DetermineBaselineWritingMode(container_wd, child_wm)` and `baseline_group` (kMajor or kMinor) via `DetermineBaselineGroup(container_wd, baseline_wm, is_parallel, is_last_baseline, is_wrap_reverse)`.
2. **Baseline ascent** (`BaselineAscent`, ~line 370): Reads `FirstBaselineOrSynthesize()` or `LastBaselineOrSynthesize()`. Flips (`blockSize - baseline`) when `is_wrap_reverse != is_last_baseline`. Adds `CrossStart` margin for major items, `CrossEnd` margin for minor items.
3. **Line sizing**: Tracks four running maxima per line: `max_major_ascent`, `max_major_descent`, `max_minor_ascent`, `max_minor_descent`. Line stores `major_baseline = max_major_ascent`, `minor_baseline = max_minor_ascent`.
4. **Item placement**: Major items offset from cross-start by `major_baseline - item_ascent`. Minor items offset from cross-end by `minor_baseline - item_ascent` (i.e., `space - delta`).
5. **Container baseline**: First line's `major_baseline` (preferred) for first baseline; last line's `minor_baseline` for last baseline.

### Fix Location
`pkg/layout/flex_layout.go`:
1. Add `baselineGroup` field to `flexItem` struct (line ~180). Define constants `baselineGroupMajor`, `baselineGroupMinor`.
2. Add `determineBaselineGroup()` function mirroring Blink's logic.
3. Replace single `maxAscent`/`maxDescent` with four per-group accumulators in line sizing (line ~630).
4. Remove `bl > 0` guard (line 637) and `bl = min(bl, crossSize)` clamp (line 642-643).
5. In `BaselineAscent()` equivalent: add cross-end margin for minor items, flip for wrap-reverse.
6. Update cross-axis offset computation (line ~1044-1181): use `majorBaseline` for major items, `minorBaseline` for minor items.
7. Update container baseline (line ~1323-1400): use per-line major/minor baselines.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-align-self-baseline|TestWPTCSS3Reftests/css-flexbox/flexbox-baseline" -v
# Regression check:
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox" -v 2>&1 | grep -c "REFTEST PASS"
# Must be >= 553
```

---

## Target 2: Vertical Writing Mode Flex Items (~13 tests)

### Problem
Flex items with vertical writing modes (vertical-lr, vertical-rl, sideways-lr, sideways-rl) in horizontal flex containers, and items in vertical flex containers, have sizing and positioning errors. The axis mapping logic (`computeMainIsItemInline`) is correct, but several downstream consumers don't properly handle the orthogonal case — particularly for replaced elements (canvas, img, textarea, iframe, video, fieldset) whose intrinsic sizes need writing-mode-aware resolution.

### Affected Tests (~13 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| flexbox-basic-block-horiz-001v | 16175 | Block items with vertical-lr/rl writing modes |
| flexbox-basic-block-vert-001v | 975 | Block items in column flex with WM |
| flexbox-basic-canvas-horiz-001v | 751 | Canvas in horiz flex with WM items |
| flexbox-basic-canvas-vert-001 | 84 | Canvas in column flex |
| flexbox-basic-canvas-vert-001v | 84 | Canvas in column flex with WM items |
| flexbox-basic-fieldset-vert-001 | 84 | Fieldset in column flex |
| flexbox-basic-iframe-vert-001 | 84 | Iframe in column flex |
| flexbox-basic-img-vert-001 | 84 | Image in column flex |
| flexbox-basic-textarea-horiz-001 | 3005 | Textarea in horiz flex |
| flexbox-basic-textarea-vert-001 | 6295 | Textarea in column flex |
| flexbox-basic-video-vert-001 | 84 | Video in column flex |
| flexbox-flex-wrap-vert-002 | 1140 | Wrap in column flex with WM |
| baseline-synthesis-vert-lr-line-under | 1600 | Baseline synthesis in vertical-lr |

### Root Cause (from code analysis)
The 84-pixel failures (canvas, fieldset, iframe, img, video in column flex) share a common pattern: these are replaced/embedded elements in a column flex container where the intrinsic size resolution is incorrect. The `max diff: 200` on these tests suggests a positioning error (items shifted by a fixed amount).

In `flex_layout.go`:
- **Line 2050-2160**: `itemMaxContentMainSize()` and `itemContentMaxMainSize()` handle replaced elements but may not correctly resolve intrinsic dimensions when the item has a vertical writing mode.
- **Line 1621-1631**: Orthogonal flex-basis resolution dispatches correctly but the constraint space may not carry proper orthogonal fallback sizes.
- The `*-001v` tests specifically put `writing-mode: vertical-lr` or `vertical-rl` on flex items inside a horizontal flex container. This makes the item orthogonal: its inline axis is vertical while the flex main axis is horizontal. Sizing should use the item's block-axis (horizontal) for the flex main size.

### What Blink Does
**Key file**: `flex_layout_algorithm.cc`

Blink precomputes `is_horizontal_flow_` at the container level:
```cpp
is_horizontal_flow_ = Style().IsHorizontalWritingMode() ? !is_column_ : is_column_;
```
Per item, `is_main_axis_inline_axis` determines axis alignment. For orthogonal items:
1. Flex basis uses `ResolveMainBlockLength()` instead of `ResolveMainInlineLength()`.
2. Content-based sizing runs a full layout pass (not just `ComputeMinMaxSizes()`).
3. The `ConstraintSpaceBuilder` automatically swaps axes when child is non-parallel.

### Fix Location
`pkg/layout/flex_layout.go`:
1. Verify constraint space construction for orthogonal items (around line 1621-1631) passes correct orthogonal fallback sizes.
2. Fix replaced element sizing for column flex (line 2050-2160): ensure intrinsic sizes are queried in the correct axis.
3. Check that `computeFlexBaseSize` for orthogonal items with content-based sizing runs layout rather than just using min-max.
4. For the `*-001v` tests, ensure that when the item has `writing-mode: vertical-lr` and the container is `flex-direction: row`, the item's block-axis (horizontal width) is used as the flex main size.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-basic-(block|canvas|fieldset|iframe|img|textarea|video)-(horiz|vert)" -v
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-flex-wrap-vert-002" -v
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/baseline-synthesis-vert" -v
# Regression check: total PASS count must be >= 553
```

---

## Target 3: Aspect Ratio Flex Item Sizing (~9 tests)

### Problem
Flex items with CSS `aspect-ratio` or intrinsic aspect ratios (images, canvas) are not correctly sized when stretch alignment establishes a definite cross-size. The current code only handles `<img>` elements for aspect-ratio stretch, not all elements with the CSS `aspect-ratio` property. The transferred size suggestion in auto-min-size also has incorrect combination logic.

### Affected Tests (~9 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| aspect-ratio-intrinsic-size-001 | 9000 | Canvas with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-002 | 9000 | Canvas with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-007 | 262328 | Img aspect-ratio sizing breakdown |
| aspect-ratio-intrinsic-size-008 | 1600 | Div with aspect-ratio in inline-flex |
| aspect-ratio-intrinsic-size-009 | 1600 | Div with aspect-ratio + stretch |
| aspect-ratio-intrinsic-size-010 | 1600 | Div with aspect-ratio + min-width |
| flex-aspect-ratio-img-column-010 | 16000 | Image aspect-ratio in column flex |
| flex-aspect-ratio-img-column-018 | 5000 | Image aspect-ratio column with constraints |
| flex-aspect-ratio-img-row-015 | 10000 | Image aspect-ratio in row flex |

### Root Cause (from code analysis)
In `flex_layout.go`:
- **Line 3934-4010**: `useAspectRatioStretch` only checks for `img` tag (`item.node.TagName == "img"`), not CSS `aspect-ratio` on arbitrary elements (div, canvas, etc.).
- **Line 3593-3701**: Transferred suggestion computation is only for replaced elements with intrinsic aspect ratio. Missing handling for CSS `aspect-ratio` on non-replaced elements.
- **Line 3787-3800**: Auto-min combination uses `max(content, transferred)` for non-replaced and `min(content, transferred)` for replaced. Blink folds transferred sizes into the content suggestion and uses a single `min(specified, content)` combination.
- **Line 577-619**: Cross-size derivation via aspect ratio only applies to specific replaced element types.

### What Blink Does
**Key file**: `flex_layout_algorithm.cc`, `length_utils.cc`

Blink's approach:
1. Communicates stretch intent through constraint space via `AutoSizeBehavior::kStretchExplicit`.
2. In generic sizing functions (`ComputeInlineSizeForFragment`), stretch wins over aspect-ratio for the auto dimension, but aspect-ratio still affects the automatic minimum size.
3. Transferred min/max sizes are suppressed during flex basis computation, then re-applied contextually.
4. For non-replaced elements with `aspect-ratio`, the content suggestion is `max(content_via_aspect_ratio, intrinsic_min_content)`.
5. All checks use `!style.AspectRatio().IsAuto()` — no tag-name checks.

### Fix Location
`pkg/layout/flex_layout.go`:
1. **Line 3934-4010**: Generalize `useAspectRatioStretch` to check CSS `aspect-ratio` property, not just `img` tag name.
2. **Line 3593-3775**: Unify transferred suggestion into content suggestion. For replaced elements, use `ComputeReplacedSize`. For non-replaced with aspect-ratio, compute `max(content_from_ratio, intrinsic_min_content)`.
3. **Line 3787-3800**: Simplify to `min(specified, content)` since transferred is now inside content.
4. Add utility to check `HasAspectRatio()` that covers both intrinsic (replaced) and CSS `aspect-ratio` property.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/aspect-ratio|TestWPTCSS3Reftests/css-flexbox/flex-aspect-ratio" -v
# Regression check: total PASS count must be >= 553
```

---

## Target 4: Min-Size Auto Algorithm Fixes (~6 tests)

### Problem
The automatic minimum size computation (CSS Flexbox §4.5) has several divergences from Blink's implementation: the three-suggestion combination logic is incorrect, box-sizing adjustments are inconsistent, and the min-cross-size fallback in the transferred suggestion doesn't match the spec.

### Affected Tests (~6 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| flex-minimum-height-flex-items-019 | 7000 | Nested flex with percentage heights |
| flex-minimum-height-flex-items-023 | 5000 | Min-height auto with definite cross |
| flex-minimum-height-flex-items-030 | 10000 | Min-height auto with aspect ratio |
| flexbox-min-height-auto-001 | 72 | Basic min-height:auto resolution |
| flexbox-min-height-auto-002c | 2560 | Min-height auto capped by max-height |
| flexbox-min-width-auto-005 | 4400 | Min-width auto with overflow |

### Root Cause (from code analysis)
In `flex_layout.go`, function `flexItemAutoMinSize` (line 3426-3805):
- **Line 3787-3800**: Uses `max(content, transferred)` for non-replaced, `min(content, transferred)` for replaced. Blink uses a unified `min(specified, content)` where transferred is folded into content.
- **Line 3626-3640**: Falls back to `min-cross-size` when no explicit cross-size for transferred suggestion. Blink only uses definite cross-size or stretched cross-size — no min fallback.
- **Box-sizing inconsistency**: Content suggestion sometimes computed as content-box, sometimes as border-box. Blink computes everything in border-box, then subtracts border+padding at the end.

### What Blink Does
**Key file**: `flex_layout_algorithm.cc`

Blink computes all three suggestions as **border-box** values:
1. **Specified**: Resolves explicit CSS main-size if definite.
2. **Content**: For inline main-axis, uses `MinMaxSizes.min_size` (min-content). For block main-axis, runs intrinsic layout. For non-replaced with aspect-ratio, returns `max(content_via_ratio, intrinsic_min_content)`.
3. **Combination**: `auto_min = min(specified, content)`. No separate transferred variable — transferred sizes are folded into content computation.
4. **Final**: Subtracts `border_padding` if `box-sizing: content-box`.

### Fix Location
`pkg/layout/flex_layout.go`, function `flexItemAutoMinSize` (line 3426-3805):
1. Remove separate transferred suggestion tracking (line 3596-3775). Fold aspect-ratio-based sizing into the content suggestion.
2. Change combination (line 3787-3800) to `min(specified, content)`.
3. Remove min-cross-size fallback (line 3626-3640).
4. Ensure consistent border-box computation, then adjust for box-sizing at the end.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flex-minimum-height|TestWPTCSS3Reftests/css-flexbox/flexbox-min-(height|width)-auto" -v
# Regression check: total PASS count must be >= 553
```

---

## Target 5: Safe Alignment for align-self/align-items (~2 tests)

### Problem
The `safe` keyword is correctly handled for `justify-content` and `align-content`, but is **stripped and ignored** for `align-self` and `align-items`. When `align-self: safe center` is used and an item overflows the flex line's cross-size, it should fall back to `flex-start` alignment instead of centering (which would push content off-screen).

### Affected Tests (~2 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| flexbox-safe-overflow-position-001 | 836 | safe align-items/align-self with overflow |
| flexbox-safe-overflow-position-006 | 1000 | safe alignment edge cases |

### Root Cause (from code analysis)
In `flex_layout.go`:
- **Line 3111-3128**: `stripOverflowKeyword()` removes "safe " / "unsafe " prefix but returns only the stripped keyword. The `safe` flag is discarded.
- **Line 3185-3191**: `getAlignSelf()` calls `stripOverflowKeyword()` — safe/unsafe info is lost.
- **Line 1113-1187**: Cross-axis alignment switch (`switch selfAlign`) applies the keyword but has no safe-overflow check. When `safe center` is used and the item's cross-size exceeds the line's cross-size, the item is still centered, causing negative offsets.

### What Blink Does
When `OverflowAlignment::kSafe` and free space is negative (item overflows line):
```cpp
if (free_space <= 0 && data.Overflow() == OverflowAlignment::kSafe) {
    return LayoutUnit(); // fall back to start alignment
}
```

### Fix Location
`pkg/layout/flex_layout.go`:
1. **Line 3111-3128**: Return both the stripped keyword AND whether `safe` was specified: `func stripOverflowKeyword(v string) (string, bool)`.
2. **Line 3185-3191**: Propagate the `safe` flag from `getAlignSelf()`.
3. **Line 1113-1187**: In the cross-axis alignment switch, when `safe` is true and the computed offset would be negative (item overflows), fall back to offset = 0 (start alignment). Apply to `center`, `flex-end`, `end`, `self-end` cases.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-safe-overflow" -v
# Regression check: total PASS count must be >= 553
```

---

## Target 6: Whitespace Handling with white-space:pre (~2 tests)

### Problem
Whitespace-only text runs between flex items should NOT become anonymous flex items, even when `white-space: pre` is set on the container. The test uses `justify-content: space-around` to detect whether spurious anonymous flex items are consuming packing space.

### Affected Tests (~2 failures)
| Test | Pixels | Issue |
|------|--------|-------|
| flexbox-whitespace-handling-001a | 4800 | Whitespace with pre, space-around |
| flexbox-whitespace-handling-001b | 4800 | Same, with space-between |

### Root Cause (from code analysis)
In `flex_layout.go`, function `buildFlexChildList` (line 1465-1524):
- **Line 1481-1483**: Uses `strings.TrimSpace(n.TextContent()) != ""` to detect non-whitespace content. This check trims ASCII whitespace only.
- The issue may be that `white-space: pre` causes the whitespace to be treated as significant during text run accumulation, but the spec says whitespace-only runs must be excluded from flex item creation regardless of `white-space` property.
- Need to verify that the filtering happens BEFORE the `white-space` property affects text handling.

### What Blink Does
Blink handles this at layout tree construction: `LayoutTreeBuilderForText` uses `IsAllCollapsibleWhitespace()` which checks the characters against the `white-space` property. However, for flex containers, the spec explicitly says whitespace-only runs don't generate flex items regardless of `white-space` — Blink's `FlexChildIterator` skips empty anonymous blocks that resulted from whitespace-only runs.

### Fix Location
`pkg/layout/flex_layout.go`, function `buildFlexChildList` (line 1465-1524):
1. Read the test HTML to understand the exact DOM structure producing the failure.
2. Verify that whitespace nodes between `<div>` elements are being filtered.
3. If the issue is that indentation/newlines in the XHTML source create text nodes between elements, ensure these are caught by the `TrimSpace` check.
4. Debug by logging what `buildFlexChildList` produces for the test case to identify spurious items.

### Verification
```bash
GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-flexbox/flexbox-whitespace-handling" -v
# Regression check: total PASS count must be >= 553
```

---

## Execution Order

Since all targets touch `flex_layout.go`, they must be done **sequentially**:

1. **Target 5** (safe alignment) — smallest, most self-contained change. ~20 lines of code.
2. **Target 6** (whitespace handling) — small, diagnostic-first. May be a quick fix.
3. **Target 1** (baseline alignment) — the most foundational rewrite. Do this before targets that depend on correct cross-axis computation.
4. **Target 4** (min-size auto) — clean up the suggestion algorithm to match Blink's unified approach.
5. **Target 3** (aspect ratio) — depends on min-size auto being correct. Generalizes `img`-only code.
6. **Target 2** (vertical writing modes) — most complex, touches many code paths. Benefits from all prior fixes.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. The Blink research above should be sufficient, but if uncertain about edge cases, look at `flex_layout_algorithm.cc` in Chromium source.
- **Commit and report at each milestone** (after each target, not at the end).
- **Regression constraint**: After each target, verify the PASS count is >= 553. Any regression means the fix is wrong — debug before proceeding.
- **All tests must pass at 0% diff** — don't dismiss small pixel differences.
- **Run only the specific tests** for each target during development. Only run the full flex suite for the regression check.
- **Do not modify files outside `pkg/layout/flex_layout.go` and `pkg/css/style.go`** unless absolutely necessary. If other files need changes, explain why in the commit message.
