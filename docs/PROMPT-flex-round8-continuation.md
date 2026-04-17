Continue flex round 8 improvements (Targets 2 & 4). The following fixes have been completed and committed on fix/flexbox-fast:

1. Font path resolution for @font-face web fonts (pkg/text/measure.go) — fixed flexbox_direction-row-reverse
2. Collapsed item strut algorithm per CSS Flexbox §12 (pkg/layout/flex_layout.go) — fixed flexbox-collapsed-item-horiz-002/003
3. Float clear handling in shrink-to-fit width (pkg/layout/min_max_sizing.go) — improved RTL-001/002
4. Flex baseline alignment for column flex containers (pkg/layout/flex_layout.go) — baseline falls back to flex-start per §8.3; normalized baseline keyword handling (first-baseline/last-baseline)

5 groups of test failures remain. All have been investigated; root causes are identified and Blink's approach has been studied for each.

GROUP 1 — flexbox-align-self-vert-001 (64px diff, 0.0%)
  Root cause: Pre-existing text rendering diff in the pink "stretch" area (y=230-233). Anti-aliasing difference between flex and block reference rendering of text at 10px sans-serif. Test has NO fuzzy tolerance. NOT caused by any flex code.
  Fix area: Text measurement / font rasterization (pkg/text/measure.go or mazarin/textshape/).
  Blink approach: Blink uses LayoutUnit (fixed-point 1/64th pixel precision) throughout layout, then pixel-snaps text draw positions at paint time via SnapToDevicePixels(). This ensures text glyphs always land on exact pixel boundaries regardless of how they were positioned during layout. Louis14 uses raw float64 coordinates with no pixel snapping, so text can be drawn at sub-pixel positions causing anti-aliasing differences between equivalent layouts (flex vs block) that position text at slightly different fractional offsets.
  Fix: Add pixel-snapping (round to nearest integer) for text draw positions in the paint/rasterization layer. This should eliminate the 64px anti-aliasing diff since both flex and block will snap to the same pixel grid.

GROUP 2 — flexbox-align-self-vert-003 (2.3%) and vert-004 (6.5%)
  Root cause: Reference files use float/inline-block layout in 4px-wide containers. Our block/float/inline-block rendering in extremely narrow containers produces different vertical spacing than browsers. Flex layout verified correct via debug logging — all alignment values match expected positions.
  Fix area: Block layout / inline layout in narrow containers (pkg/layout/block_layout.go, inline_layout.go).
  Blink approach: Blink's text-align:center implementation returns space/2 even when the available space is NEGATIVE (i.e., when text overflows the container). Louis14's computeTextAlignOffset at inline_layout.go:1224 clamps to 0 when slack <= 0 (lines 1226-1228). In narrow containers where content overflows, this clamping difference changes how text is horizontally positioned, which in turn affects line breaking and vertical layout. The WPT reference tests use a "centerParent" trick (100px wide with -48px left margin) that avoids the narrow-container centering issue, but the flex items themselves are 4px wide with 10px font text.
  Fix: Remove the clamp-to-zero behavior in computeTextAlignOffset for text-align:center. When space is negative, return space/2 (a negative value) to match Blink. This allows text to be centered even when it overflows, producing the same visual positioning as browsers. Verify that this doesn't break other tests.

GROUP 3 — flexbox-align-self-vert-rtl-001 (0.1%, 560px), rtl-002 (0.4%, 1772px), rtl-005 (0.4%)
  Root cause: In column flex with direction:rtl, baseline falls back to flex-start (correct per CSS Flexbox §8.3, confirmed against Blink source). Each baseline item is individually right-aligned. But the WPT reference groups baseline items in a float:right parent where both items share the same physical-left edge. This creates a ~32px shift for the smaller "base" item (18px vs 50px group width). The remaining diff after the clear fix is this positioning mismatch. The test has fuzzy tolerance of 101 pixels but our diff is ~560px.
  Blink approach: float:left is ALWAYS physical left in Blink (and per CSS spec), never logical. This is confirmed in Blink's StyleAdjuster which explicitly does NOT flip float values for RTL. Our block_layout.go swap logic (float:left in RTL → logicalSide=FloatRight → WritingModeConverter gives physX=0=physical left) produces the correct physical result. The remaining diff comes from how shrink-to-fit width is computed for the float:right baselineParent container — after the clear fix, same-side floats with clear:left correctly take max instead of sum, but the baselineParent's width computation may still differ from browsers in how it handles the grouping of baseline items.
  Fix: This requires deeper investigation into shrink-to-fit width computation for float containers that contain floats with clear. The reference baselineParent is 50px wide (width of widest float child), but our computation may produce a different width due to remaining edge cases in measureBlockMinMax's float accumulation logic. Compare pixel-by-pixel with browser rendering to identify the exact sizing difference.

GROUP 4 — flexbox-align-self-vert-rtl-003 (2.4%) and rtl-004 (6.9%)
  Root cause: Same as GROUP 2 (narrow 4px containers) combined with RTL direction. The reference rendering in narrow containers differs from browsers.
  Fix area: Same as GROUP 2 — block/float/inline layout in narrow containers. The text-align:center clamping fix should resolve these as well.

GROUP 5 — flexbox_flex-formatting-interop (1.6%)
  Root cause: Flex container with negative margin doesn't properly interact with adjacent float. Should be re-tested after merging the Target 6 BFC float avoidance fix (already implemented in a separate worktree on fix/flexbox-fast as commit c205be67).
  Fix area: Re-test first; may already be fixed.

Recommended order:
1. Re-test GROUP 5 (formatting-interop) — may already be fixed by BFC merge
2. Fix GROUP 2 text-align:center clamping (inline_layout.go) — fixes GROUP 4 too
3. Fix GROUP 1 pixel snapping for text draw positions — small, isolated
4. Investigate GROUP 3 (RTL shrink-to-fit width) — deepest issue, compare with browser pixel output
