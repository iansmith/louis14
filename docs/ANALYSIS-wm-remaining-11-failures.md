# Analysis of 11 Remaining Writing-Modes Test Failures

Starting state after round 6 fixes: 450 pass / 337 fail (same baseline before and after round 6 changes — the "700/87" number from the original prompt was from a different test configuration).

## Group 1: `box-offsets-rel-pos-vlr-005` + `vrl-004` (2 tests, 0.5%)

**What's happening:** The TL/TR corner labels match perfectly, but the BL/BR labels and the bottom edge of the parent box are shifted down ~4px.

**Root cause: Image descender gap.** The parent `div` has `height: 100px` (inline-size in VLR) and contains an `<img>`. The image is inline content sitting on a text baseline. CSS 2.1 §10.8.1: replaced elements align their bottom edge at the baseline, leaving a descender gap below. Our engine appears to let this descender gap expand the parent's content area rather than clipping to the explicit 100px.

**What Blink does:** Blink's block layout honors the explicit `height` (inline-size in VLR) strictly. The `intrinsicBlockSize` computed from content (which would include the descender gap) is **overridden** by `explicitBlockSize` when CSS height is definite. Content overflows but doesn't grow the box. The parent is exactly 300px border-box, placing BL/BR at the correct positions.

**Fix direction:** The issue is likely in how `ResolveBlockSize` vs content-derived sizing interacts. In our engine, `finalBlockSize = explicitBlockSize` when explicit — but in VLR, `height: 100px` maps to inline-size, not block-size. We need to check whether the inline-size determination is letting content overflow leak into the parent's reported inline-size. The fix is narrow: ensure that when a block container has an explicit inline-size (via `height` in VLR), the inline image descender gap doesn't inflate it.

---

## Group 2: `normal-flow-overconstrained-vrl-002` (1 test, 1.3%)

**What's happening:** The green 80x80 div is positioned in the center-right area, but a red background image at the left edge is visible. In the reference, the green box covers the red area.

**Root cause: The `<p>` element with a missing support image takes up different block space.** The body has `background-position: -152px 8px` (physical), making red visible from x=0 to x=168. The green div needs to be positioned there to cover it. But the `<p>` before the div occupies block space (physical width in VRL), pushing the div leftward. The `<p>` contains `<img src="support/pass-cdts-abs-pos-non-replaced.png" width="246" height="36">` — if our engine doesn't honor the HTML `width`/`height` attributes when the image file fails to load, the `<p>` takes the wrong block-size, displacing the div.

**What Blink does:** When an image can't be loaded, Blink still uses the HTML `width`/`height` attributes to determine the replaced element's intrinsic dimensions. The `<img>` is 246x36 regardless of whether the file loads. This guarantees the `<p>` column takes consistent block space.

**Fix direction:** Check whether `getImgIntrinsicInfo` falls through to `IntrinsicSizingInfo{}` (all zeros) when the image file is missing — it currently does, ignoring the HTML `width`/`height` attributes. The fix: when `ImageFetcher` fails, fall back to the element's HTML `width`/`height` attributes before returning the empty defaults. This would also fix Group 1, since `support/100x100-lime.png` and `support/pass-cdts-box-offsets-rel-pos.png` might also be affected.

---

## Group 3: `float-lft-orthog-*-002` (4 tests, 0.4%)

**What's happening:** The test renders "**V** R L  p a r e n t" vertically, but the reference shows "R L  p a r e n t" — one extra character ("V") is visible. The difference is exactly one character.

**Root cause: Orthogonal float inline-size off by a sub-pixel amount.** The float (HTB, `float:left`) has one line of text at `font-size: 32px; line-height: 1.25` = 40px physical height. In VRL, this 40px is the float's inline-size. The parent text also uses 40px line-height per character. The float should consume exactly one character's worth of inline space. If the float's computed height is 39px instead of 40px (or the text's line position has a 1px offset), the "V" character falls just outside the float zone and becomes visible.

**What Blink does:** Blink's `ExclusionSpace` uses precise sub-pixel float dimensions. The float's height comes from `line-height` which is exact (32 * 1.25 = 40.0). Blink also computes text offsets with sub-pixel accuracy. The "V" character falls exactly within the float's inline extent and is pushed to the next column (behind the float).

**Fix direction:** This is a text measurement precision issue. The float's physical height (40px from line-height) should exactly equal one character's line-height position. Check whether our inline layout computes line-height differently for the float's content vs the parent's content — any sub-pixel rounding discrepancy would cause this. Also check whether the float's exclusion zone is slightly shorter than the float's actual rendered height.

---

## Group 4: `ortho-htb-alongside-vrl-floats-*` (4 tests, 15-34%)

**What's happening:** The overall layout structure is wrong — floats, clears, and the orthogonal BFC child are all interacting in VRL with complex positioning.

**Root cause: Multiple interacting issues.** (1) Missing support image `ortho-htb-alongside-vrl-floats-002-exp-res.png` causes the `<p>` to take wrong block-size (same root cause as Group 2). (2) Orthogonal BFC avoidance is not fully implemented — when an orthogonal child (HTB) is a BFC in a VRL parent with floats, Blink uses `AllLayoutOpportunities` to find rectangular regions where the child fits, handling the axis swap correctly. Our engine skips the BFC push for orthogonal children entirely rather than doing the axis-swapped avoidance.

**What Blink does:** In `LayoutNewFormattingContext`, Blink iterates through `AllLayoutOpportunities` from the `ExclusionSpace`. Each opportunity is a rectangle in the parent's coordinate system. For orthogonal children, the child's inline-size (perpendicular to parent's inline axis) is matched against the opportunity's block dimension. The first opportunity where both the child's inline-size and block-size fit is selected. This is significantly more complex than what we currently implement.

**Fix direction:** This requires implementing Blink's `LayoutOpportunity` concept — finding rectangular regions in the exclusion space where the child can fit, accounting for orthogonal axis mapping. It's a substantial feature, not a point fix. The missing image issue (same as Group 2) should be fixed first, as it would reduce the diff significantly.

---

## Cross-cutting root cause: Missing image fallback dimensions

Groups 1, 2, and 4 all share a common root cause: **when an image file can't be loaded, our engine ignores the HTML `width`/`height` attributes** and returns zero intrinsic dimensions. Blink always respects these attributes as fallback intrinsic dimensions. Fixing `getImgIntrinsicInfo` to fall back to HTML attributes would likely fix or significantly reduce 7 of the 11 failures.

The relevant code is in `pkg/layout/intrinsic_sizing.go`, function `getImgIntrinsicInfo`. When `images.GetImageDimensionsWithFetcher` fails, it returns `IntrinsicSizingInfo{}` without checking the DOM element's `width`/`height` attributes.
