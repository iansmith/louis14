# Fix Top 3 Failing Tests

## Current Baseline: 37/51 tests passing

Work these three tests in order of error severity. After each fix, run a full regression (`go test ./pkg/visualtest -run "TestWPTReftests" -v`) and confirm no regressions before moving to the next.

---

## 1. `border-padding-bleed-001.xht` — 13.0% error

**What the test does:** A `<div>` with `font: 40px/1 Ahem` (line-height equals content height) contains two lines. Line 1 is red text "shuldboverlaPPed". Line 2 is a `<span>` with green background, 15px green border-top, and 25px green padding-top. Per CSS 2.1 §10.8.1, inline borders/padding don't affect line box height but ARE rendered — so the span's border+padding should "bleed" upward over line 1, painting everything green. The reference is a solid 80px×640px green rectangle.

**Likely root cause:** The renderer either (a) clips inline border/padding to the line box, (b) doesn't render inline border-top/padding-top at all, or (c) renders them but at the wrong position. The Ahem font is critical — each glyph is exactly 1em × 1em, making the test pixel-perfect.

**Key files:**
- `pkg/render/render.go` — where inline box backgrounds/borders are painted
- `pkg/layout/layout_inline_multipass.go` — where inline span boxes get their dimensions
- Test: `pkg/visualtest/testdata/wpt-css2/linebox/border-padding-bleed-001.xht`

**CSS spec:** CSS 2.1 §10.8.1 — "Margins, borders, and padding of inline non-replaced elements do not enter into the line box calculation... they are still rendered around inline boxes."

---

## 2. `anonymous-boxes-inheritance-001.xht` — 12.3% error

**What the test does:** An outer `<div>` with `color: blue; font: 100px/1 Ahem` contains bare text "X" followed by an inner `<div>` with `color: orange` containing "X". The bare "X" gets wrapped in an anonymous block box which should inherit `font: 100px/1 Ahem` from the outer div — producing a 100×100 blue square. The inner div produces a 100×100 orange square. The reference is two stacked 100×100 squares (blue then orange).

**Likely root cause:** The anonymous block box created around the bare "X" text (when the outer div mixes inline text with a block child) may not be inheriting the font properties correctly — likely rendering at the default 16px font instead of the inherited 100px Ahem font, producing a tiny blue rectangle instead of a 100×100 square.

**Key files:**
- `pkg/layout/layout_block.go` — block-in-inline splitting, anonymous box creation
- `pkg/layout/layout_inline_multipass.go` — `InlineItemBlockChild` handling
- `pkg/css/cascade.go` — `ApplyInheritedProperties`, anonymous box style inheritance
- Test: `pkg/visualtest/testdata/wpt-css2/box-display/anonymous-boxes-inheritance-001.xht`

**CSS spec:** CSS 2.1 §9.2.1.1 — "Anonymous boxes inherit property values from their non-anonymous parent box."

---

## 3. `margin-001.xht` — 11.2% error

**What the test does:** Tests `margin: 2.54cm` (centimeter units). A `#reference` div has `position: absolute; border: 10px solid red; height: 308px; width: 500px`. A `#div1` (also absolute, same position) contains `#div2` with `margin: 2.54cm; border: 10px solid green; height: 1in; width: 3in`. The green box with its margin should exactly overlap the red reference box (2.54cm ≈ 96px, so total = 96px margin + 10px border + 96px/288px content + 10px border + 96px margin = 308px/500px). Test passes if no red is visible.

**Likely root cause:** The CSS unit `cm` (centimeters) is probably not parsed correctly. CSS defines `1in = 2.54cm = 96px`, so `2.54cm = 96px`. If `ParseLength` doesn't handle `cm` units, the margin would be 0 or wrong, causing the green box to be smaller than the red reference.

**Key files:**
- `pkg/css/style.go` — `ParseLength` function, unit conversion
- Test: `pkg/visualtest/testdata/wpt-css2/margin-padding-clear/margin-001.xht`

**Verification commands:**
```bash
# Test individual fixes
go test ./pkg/visualtest -run "TestWPTReftests/linebox/border-padding-bleed-001" -v
go test ./pkg/visualtest -run "TestWPTReftests/box-display/anonymous-boxes-inheritance-001" -v
go test ./pkg/visualtest -run "TestWPTReftests/margin-padding-clear/margin-001" -v

# Full regression
go clean -testcache && go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"

# Unit tests
go test ./pkg/layout/... -v
```
