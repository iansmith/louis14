# Writing Modes Round 5: Top 6 Improvements

Current state: 673 pass / 114 fail (85.5% pass rate) across 787 tests in `TestWPTCSS3Reftests/css-writing-modes`.

**Note:** The block-flow-direction collapse-through OOF propagation bug has already been fixed and committed (`6950d4f1`). This resolved 12 of the 14 block-flow-direction/line-box-direction tests.

These six targets are **independent** (touch different primary subsystems) and can be worked on in parallel by separate worktree agents.

---

## Project Rules (ALL agents MUST follow these)

1. **Foundational correctness over quick wins.** Every fix must work for ALL cases. Don't chase "nearly passing" tests. If a fix doesn't generalize, it's the wrong fix.
2. **Study Blink BEFORE writing any code.** The louis14 codebase is modeled on Blink's LayoutNG. Mirror their type names, algorithm structure, and constraint-passing patterns. Each target below includes Blink references — read them first.
3. **All tests must pass at 0% diff.** A 0.5% diff is a failure just like 28%. Never dismiss failures as "font rendering" or "anti-aliasing."
4. **Test execution discipline.** Run ONLY the specific tests listed in the Verification section — do NOT run the full writing-modes suite or broader test suites.
5. **Commit and report at each milestone.** Each target below has numbered milestones — commit after each one with a clear message describing what changed and why. Do not batch all work into a single final commit.
6. **Use `GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go`** as the Go command for all build and test operations.
7. **Never use `open`** to display image files — it disrupts the user's screen.
8. **Worktree branch discipline.** When running in a worktree, commit ONLY to your worktree branch. Never commit directly to `fix/*` or `master` branches from a worktree.

---

## Target 1: Float clear/side physical-vs-logical confusion (~25 tests)

### Problem
`float: left/right` and `clear: left/right` are **physical** CSS values per spec. But the louis14 exclusion space stores float sides using a half-logical scheme — it converts `float: left` to `FloatLeft` (inline-start) in RTL, but doesn't account for vertical writing modes at all. Then `ClearanceOffset()` matches the physical `clear: left` against this half-logical stored side, producing wrong results in vertical modes. Additionally, the orthogonal-child float-avoidance check compares sizes in mismatched coordinate frames.

### Affected Tests (~25 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| ortho-htb-alongside-vrl-floats | 002, 006, 010, 014 | 18–28% |
| contiguous-floated-table-vlr | 003, 005, 007, 009 | 1–2.1% |
| contiguous-floated-table-vrl | 002, 004, 006, 008 | 1–2.1% |
| clearance-calculations-vrl | 002, 004, 006, 008 | 1–3.3% |
| float-vlr | 007, 011, 013 | 0.5–1.6% |
| float-vrl | 010, 012 | 0.6% |
| float-lft-orthog-* | 4 tests | 0.4% |

### Root Cause (from code analysis)

There are three related bugs:

**Bug 1 — Physical→logical conversion only handles RTL, not vertical WMs.**

`pkg/layout/block_layout.go`, lines 939–950 in `layoutFloat()`:
```go
// Current code — ONLY swaps for RTL, ignores vertical modes entirely
logicalSide := floatSide
if parentWDM.Dir == DirectionRTL {
    if floatSide == css.FloatLeft {
        logicalSide = css.FloatRight
    } else {
        logicalSide = css.FloatLeft
    }
}
```
In `vertical-rl` LTR, `float:left` stays `FloatLeft` (meaning inline-start = physical top), but physically "left" in VRL is block-end. The exclusion is stored with the wrong side.

**Bug 2 — `ClearanceOffset()` compares physical clear against half-logical float side.**

`pkg/layout/exclusion_space.go`, lines 106–113:
```go
case css.ClearLeft:
    shouldClear = e.Side == css.FloatLeft
```
`css.ClearLeft` is the physical CSS value `clear:left`. `e.Side` is the half-logical side stored by Bug 1. In vertical modes, these represent different physical edges, so the wrong exclusions are matched.

**Bug 3 — Orthogonal child float-avoidance uses wrong coordinate frame.**

`pkg/layout/block_layout.go`, lines 246–279: When a BFC child has a different writing mode from the parent (orthogonal), the code compares the child's needed inline-size against the parent's available inline-size minus float offsets. But in an orthogonal relationship, the child's inline axis is the parent's block axis. The comparison is in the wrong coordinate frame, causing 18–28% diffs in `ortho-htb-alongside-vrl-floats`.

### What Blink Does (STUDY THIS FIRST)

In Blink's LayoutNG, the **entire float/exclusion system is physical**:

- `ExclusionArea` (in `exclusion_area.h`) stores `const EFloat type` using physical `EFloat::kLeft` / `EFloat::kRight` values. No logical conversion.
- `ExclusionSpace` (in `exclusion_space.cc`) maintains `left_clear_offset_` and `right_clear_offset_` — physical left and physical right. All comparisons are physical.
- `ClearanceOffset(EClear)` directly maps `kLeft` → `left_clear_offset_`, `kRight` → `right_clear_offset_`. No writing-mode conversion.
- The only logical↔physical conversion for floats happens in `ComputedStyle::Floating(TextDirection)` which resolves `float: inline-start/inline-end` to physical `kLeft`/`kRight`. Physical `float: left/right` pass through unchanged.
- Exclusion positioning (where floats sit in the inline axis) is where logical coordinates are used — the exclusion's `InlineOffset` is logical, but `Side` is physical.

Key source files:
- `third_party/blink/renderer/core/layout/exclusions/exclusion_area.h`
- `third_party/blink/renderer/core/layout/exclusions/exclusion_space.h` and `.cc`
- `third_party/blink/renderer/core/layout/unpositioned_float.h` — `IsLineLeft(TextDirection)` / `IsLineRight(TextDirection)`
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc` — `HandleFloat()`

### Recommended Approach

The key insight is: **do not convert float sides to logical at all**. Keep `FloatLeft`/`FloatRight` as physical sides throughout the exclusion space. The only place that needs to know about logical vs physical is the inline positioning code that decides whether a `FloatLeft` sits at the inline-start or inline-end edge.

### Implementation Steps and Milestones

**Milestone 1: Make exclusion sides consistently physical** (commit after this)
1. In `layoutFloat()` (`block_layout.go:939–950`), **remove the RTL direction swap entirely**. Store `floatSide` directly as the exclusion's `Side` — it's already physical (`css.FloatLeft` = physical left, `css.FloatRight` = physical right).
2. In `FindAvailableInlineSize()` (`exclusion_space.go:57–93`), the code currently assumes `FloatLeft` = inline-start and `FloatRight` = inline-end. This needs to change: for horizontal-tb LTR, `FloatLeft` = inline-start (physical left = start). For horizontal-tb RTL, `FloatLeft` = inline-end. For vertical-rl LTR, `FloatLeft` = block-end. You need to add a `WritingDirectionMode` parameter to `FindAvailableInlineSize` (and to `ExclusionSpace` or pass it per-call) so it can correctly map physical float sides to inline-start/inline-end offsets based on the current writing mode.
3. `ClearanceOffset()` already compares `ClearLeft` against `FloatLeft` — since both are now physical, this is correct. No change needed here.
4. Run `clearance-calculations-vrl` and `float-vlr/vrl` tests. Commit.

**Milestone 2: Fix inline positioning for physical float sides** (commit after this)
1. In `layoutFloat()` (lines 952–960), the code uses `logicalSide == css.FloatLeft` to decide inline position. This must now convert the physical side to a logical side for positioning. Add a helper: `isInlineStartFloat(physicalSide css.FloatType, wdm WritingDirectionMode) bool`. In horizontal-tb LTR, `FloatLeft` = inline-start. In horizontal-tb RTL, `FloatLeft` = inline-end. In vertical-rl LTR, `FloatLeft` = neither inline-start nor inline-end (it's on the block axis) — but `float:left` in vertical modes means the float is placed at the inline-start edge (physical top for VRL LTR). Study how Blink's `UnpositionedFloat::IsLineLeft(TextDirection)` works.
2. Run `contiguous-floated-table-vlr/vrl` and `float-lft-orthog` tests. Commit.

**Milestone 3: Fix orthogonal child float-avoidance** (commit after this)
1. In `block_layout.go` (lines 246–279), when checking if an orthogonal BFC child fits beside floats, the child's `neededInline` is in the child's inline axis. But `floatStartOff`/`floatEndOff` are in the parent's inline axis. For orthogonal children, the child's inline axis maps to the parent's block axis (and vice versa). The float offsets need to be converted or the comparison needs to account for the axis swap.
2. Run `ortho-htb-alongside-vrl-floats` tests. Commit.

**Milestone 4: Run all 25 affected tests together** (commit if any final adjustments needed)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(ortho-htb-alongside-vrl|clearance-calculations-vrl|contiguous-floated-table|float-v|float-lft-orthog)" -count=1
```

### Verification (per milestone)
```bash
# Milestone 1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/clearance-calculations-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-v" -count=1
# Milestone 2
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous-floated-table" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-lft-orthog" -count=1
# Milestone 3
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ortho-htb-alongside-vrl" -count=1
```

---

## Target 2: ch-unit ignores text-orientation in vertical writing modes (8 tests)

### Problem
The `chScale()` function returns `1.0` for ALL vertical writing modes, ignoring the `text-orientation` property. Per CSS Values §6.1, `ch` is the advance measure of "0" in the **inline axis**. The inline advance depends on whether the glyph is upright or sideways.

### Affected Tests (8 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| ch-units-vrl (upright) | 001, 002, 003, 004 | 2.1–4.7% |
| ch-units-vrl (sideways) | 005, 006, 007, 008 | 1.0–3.1% |

### Root Cause (from code analysis)

**File:** `pkg/css/style.go`, lines 227–242 — `chScale()`:
```go
func (s *Style) chScale() float64 {
    wm, _ := s.Get("writing-mode")
    if wm == "vertical-rl" || wm == "vertical-lr" ||
        wm == "sideways-rl" || wm == "sideways-lr" {
        return 1.0  // BUG: always returns 1.0 for ALL vertical modes
    }
    if s.ChWidth > 0 {
        fs := s.GetFontSize()
        if fs > 0 {
            return s.ChWidth / fs
        }
    }
    return 0.5
}
```

The rules per CSS Values §6.1 and CSS Writing Modes §7.5:
- `vertical-rl`/`vertical-lr` with `text-orientation: upright` → glyphs are upright, ch = vertical advance ≈ 1em. **Return 1.0** (currently correct).
- `vertical-rl`/`vertical-lr` with `text-orientation: sideways` → glyphs are rotated, ch = horizontal advance of "0" ≈ 0.5em. **Should return ChWidth/fontSize** (currently wrong — returns 1.0).
- `vertical-rl`/`vertical-lr` with `text-orientation: mixed` (default) → Latin glyphs are rotated (ch = horizontal advance). **Should return ChWidth/fontSize for Latin "0"** (currently wrong — returns 1.0).
- `sideways-rl`/`sideways-lr` → glyphs are ALWAYS rotated (text-orientation has no effect), ch = horizontal advance. **Should return ChWidth/fontSize** (currently wrong — returns 1.0).
- `horizontal-tb` → ch = horizontal advance. Returns ChWidth/fontSize (currently correct).

The test HTML files use `font-size: 20px` with various fonts and `5ch` dimensions. Tests 001–004 use `text-orientation: upright`, tests 005–008 use `text-orientation: sideways`. The upright tests (001–004) also fail but for a different reason — likely the table element's ch calculation isn't picking up the correct writing-mode inheritance.

### What Blink Does (STUDY THIS FIRST)

In Blink, `ComputedStyle::GetFontSizeStyle()` and the font metrics code check both `writing-mode` and `text-orientation` when computing the `ch` unit. Specifically:
- `FontMetrics::ZeroWidth()` returns the horizontal advance of "0".
- For upright vertical text, `ch` = `font-size` (1em), not `ZeroWidth()`.
- For sideways vertical text and sideways-* modes, `ch` = `ZeroWidth()`.
- The distinction is in `ComputedStyle::IsHorizontalTypographicMode()` — when false (upright vertical), font metrics use the vertical advance; when true (sideways or horizontal), font metrics use horizontal advance.

Key source: `third_party/blink/renderer/core/style/computed_style.h` — `IsHorizontalTypographicMode()`.

### Implementation Steps and Milestones

**Milestone 1: Fix `chScale()` to respect text-orientation** (commit after this)
1. Replace the current `chScale()` function in `pkg/css/style.go` (lines 227–242) with:

```go
func (s *Style) chScale() float64 {
    wm, _ := s.Get("writing-mode")

    // Helper for horizontal-advance-based ch (glyphs are sideways/rotated)
    horizontalCh := func() float64 {
        if s.ChWidth > 0 {
            if fs := s.GetFontSize(); fs > 0 {
                return s.ChWidth / fs
            }
        }
        return 0.5 // fallback: "0" is roughly half an em in most fonts
    }

    switch wm {
    case "vertical-rl", "vertical-lr":
        to, _ := s.Get("text-orientation")
        // "mixed" is the initial value — for Latin "0", mixed = sideways
        if to == "upright" {
            return 1.0 // vertical advance ≈ 1em
        }
        // "sideways" or "mixed" (default): glyphs rotated, use horizontal advance
        return horizontalCh()

    case "sideways-rl", "sideways-lr":
        // sideways-* always rotates all glyphs; text-orientation is irrelevant
        return horizontalCh()

    default:
        // horizontal-tb
        return horizontalCh()
    }
}
```

2. Run `ch-units-vrl` tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ch-units-vrl" -count=1
```

**Milestone 2: Investigate remaining failures if any** (commit if changes needed)

If tests 001–004 (upright) still fail after Milestone 1, the issue is that the `ch` unit in the table context isn't using the correct font metrics. Check:
- Does `computeChWidths()` in `pkg/layout/engine.go` (lines 448–476) correctly set `ChWidth` for the font used in the table cells?
- Does the table cell's style inherit `writing-mode` and `text-orientation` from the table?
- Read the test HTML to see which font is used and verify `ChWidth` matches.

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ch-units-vrl" -count=1
```

---

## Target 3: Background origin uses element box instead of ICB for canvas background (7 tests)

### Problem
When the root element's background is painted on the canvas (`PaintsCanvasBackground = true`), the `background-position` and `background-size` percentage resolution area incorrectly uses the root element's own box dimensions instead of the Initial Containing Block (viewport). In vertical writing modes, the root element's box may differ from the viewport, causing the background image to be mispositioned or mis-sized.

### Affected Tests (7 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| background-position-vrl | 018, 020, 022 | 12.5% |
| background-size-document-root-vrl | 002, 004, 006, 008 | 14.4% |

### Root Cause (from code analysis)

**File:** `pkg/render/render.go`, lines 1453–1481 — `drawBackgroundImageLayer()`:

```go
isFixed := bg.Attachment == css.BackgroundAttachmentFixed
var originX, originY, originW, originH float64
if isFixed {
    // Uses viewport bounds — CORRECT for fixed attachment
    bounds := r.target.Bounds()
    originX = float64(bounds.Min.X)
    originY = float64(bounds.Min.Y)
    originW = float64(bounds.Dx())
    originH = float64(bounds.Dy())
} else {
    // Uses element's own box — WRONG for canvas background
    switch bg.Origin {
    case css.BackgroundOriginBorderBox:
        originX = math.Round(box.X)
        originY = math.Round(box.Y)
        originW = math.Round(box.X+box.Width) - originX
        originH = math.Round(box.Y+box.Height) - originY
    // ... etc
    }
}
```

The clip area (lines 1550–1555) correctly uses the full canvas when `PaintsCanvasBackground` is true:
```go
if layer.PaintsCanvasBackground {
    bounds := r.target.Bounds()
    boxX0, boxY0 = bounds.Min.X, bounds.Min.Y
    boxX1, boxY1 = bounds.Max.X, bounds.Max.Y
}
```

But the **positioning area** (origin) does not get the same treatment. Per CSS Backgrounds §3.10 and CSS 2.1 §14.2: when the root element's background is propagated to the canvas, the background positioning area is the ICB (viewport), not the element's own box.

### What Blink Does (STUDY THIS FIRST)

In Blink's `BackgroundImageGeometry::Calculate()` (`background_image_geometry.cc`), when computing the positioning area for the root element's canvas background, the code explicitly uses the ICB size:
- `BoxPainter::PaintFillLayer()` passes the viewport rect as the positioning area when painting the canvas background.
- The `painting_area` is set to the viewport bounds, not the root element's layout box.

Key source: `third_party/blink/renderer/core/paint/background_image_geometry.cc` — `Calculate()` method.

### Implementation Steps and Milestones

**Milestone 1: Use ICB for canvas background positioning area** (commit after this)

In `pkg/render/render.go`, modify the origin computation (lines 1453–1481). Change:
```go
if isFixed {
```
To:
```go
if isFixed || layer.PaintsCanvasBackground {
```

This makes the canvas background use viewport bounds for both the positioning area AND the clip area (the clip is already handled at line 1550). The full change:

```go
if isFixed || layer.PaintsCanvasBackground {
    bounds := r.target.Bounds()
    originX = float64(bounds.Min.X)
    originY = float64(bounds.Min.Y)
    originW = float64(bounds.Dx())
    originH = float64(bounds.Dy())
} else {
    // existing box-based origin logic (unchanged)
}
```

Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-size-document-root-vrl" -count=1
```

**Milestone 2: Regression check** (commit if adjustments needed)

The change `isFixed || layer.PaintsCanvasBackground` could affect horizontal-tb canvas backgrounds too. Run a quick sanity check on a few css2 background tests to make sure nothing regresses:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-backgrounds/background-size" -count=1 2>&1 | grep -c "REFTEST FAIL"
```
Compare before/after failure count. If regressions appear, the fix needs to be more targeted — only use viewport bounds when `PaintsCanvasBackground && !isFixed` and the root element has a vertical writing mode. But likely the fix generalizes correctly since horizontal-tb root elements have their box matching the viewport anyway.

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-size-document-root-vrl" -count=1
```

---

## Target 4: Absolute positioning uses content-box instead of padding-box for CB size (~7 tests)

### Problem
The containing block (CB) size passed to `OutOfFlowLayoutPart` is the **content-box** size, but CSS 2.1 §10.3.7 specifies that the constraint equation for absolutely positioned elements uses the **padding-box** of the containing block. Additionally, `PropagateOOFCandidates` computes child border/padding geometry using the parent's `WritingDirectionMode` instead of the child's own WDM, causing wrong static position offsets when writing modes differ across the hierarchy.

### Affected Tests (~7 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| abs-pos-border-offset | 001, 002, 003 | 2.4–5.4% |
| abs-pos-vlr-border | 001 | 8.1% |
| abs-pos-vlr-padding | 001 | 8.1% |
| abs-pos-replaced-vrl | 001 | 0.9% |
| abs-pos-with-replaced-child | 1 | 2.1% |

### Root Cause (from code analysis)

**Bug A — Content-box instead of padding-box for CB size.**

`pkg/layout/block_layout.go`, line 692:
```go
oofPart := &OutOfFlowLayoutPart{
    ctx:                 bla.ctx,
    containingBlockWDM:  wdm,
    containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
    geom:                geom,
}
```
`contentInlineSize` is the content-box inline size (after subtracting padding). `finalBlockSize` is the content-box block size. The constraint equation (CSS 2.1 §10.3.7):
```
left + margin-left + border-left + padding-left + width + padding-right + border-right + margin-right + right = CB padding-box width
```
The "CB padding-box width" = `contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd`. Currently the code passes `contentInlineSize` (too small by the padding amount).

**Bug B — Wrong WDM in `PropagateOOFCandidates` child geometry.**

`pkg/layout/block_layout.go`, line 837:
```go
childBP := ComputeFragmentGeometry(childStyle, parentWDM)
```
`ComputeFragmentGeometry` interprets the child's physical border/padding values through `parentWDM`'s logical mapping. When the child is orthogonal to the parent (e.g., child is HTB, parent is VRL), this maps the child's physical borders to the wrong logical axes. The result: `blockAdj` and `inlineAdj` (lines 838–839) have wrong values, and the static position for OOF descendants is displaced.

### What Blink Does (STUDY THIS FIRST)

In Blink's `OutOfFlowLayoutPart::GetContainingBlockInfo()` (`out_of_flow_layout_part.cc`):
```cpp
const BoxStrut border_scrollbar = container_builder_->Borders() + container_builder_->Scrollbar();
LogicalSize container_size = ShrinkLogicalSize(container_builder_->Size(), border_scrollbar);
```
The container size is the **border-box shrunk by only borders and scrollbars** — padding is NOT subtracted. This gives the **padding-box** size. The `container_offset` starts at `(border.inline_start, border.block_start)`.

For OOF propagation across writing modes, Blink always computes child geometry in the child's own WDM, then uses `WritingModeConverter` to transform coordinates between parent and child logical spaces.

Key source: `third_party/blink/renderer/core/layout/out_of_flow_layout_part.cc` — `GetContainingBlockInfo()`.

### Implementation Steps and Milestones

**Milestone 1: Fix CB size to use padding-box** (commit after this)

1. In `pkg/layout/block_layout.go`, line 692, change the CB size to include padding:
```go
containingBlockSize: LogicalSize{
    InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
    BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
},
```

2. In `pkg/layout/out_of_flow_layout.go`, the constraint equation solver (starting at line 48 in `LayoutCandidates`) uses `cbInline` and `cbBlock` which are now the padding-box size. The inset offsets (`insets.InlineStart`, etc.) are measured from the padding-box edge, which is correct per spec. The final `inlineOffset`/`blockOffset` values will now be relative to the padding-box origin.

3. The final fragment position added via `builder.AddChild` must be relative to the **content-box** origin (since that's where normal-flow children are placed). The padding-box origin is `(geom.Padding.InlineStart, geom.Padding.BlockStart)` before the content-box origin. So you need to **subtract** the padding from the final offsets:
```go
// After computing inlineOffset and blockOffset:
inlineOffset -= p.geom.Padding.InlineStart
blockOffset -= p.geom.Padding.BlockStart
```
Wait — actually check what the existing code does. Read how `builder.AddChild` positions the fragment and where the origin is. If `AddChild` positions relative to the parent's border-box, then the padding-box offsets might need different adjustment. **Trace the coordinate chain carefully before writing code.**

4. Run the abs-pos tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos-border-offset" -count=1
```

**Milestone 2: Fix PropagateOOFCandidates WDM** (commit after this)

1. In `pkg/layout/block_layout.go`, line 837, change `parentWDM` to `childWDM`:
```go
// Before (WRONG): interprets child's borders through parent's writing mode
childBP := ComputeFragmentGeometry(childStyle, parentWDM)

// After (CORRECT): interprets child's borders through child's own writing mode
childWDM := NewWritingDirectionMode(childStyle)  // already computed at line 844
childBP := ComputeFragmentGeometry(childStyle, childWDM)
```

Note: `childWDM` is already computed at line 844. Move the `childWDM` computation to before line 837, or extract it. Then compute `blockAdj` and `inlineAdj` using the child's geometry in the **parent's** logical frame. This requires converting the child's border/padding from child-logical to parent-logical. The conversion already exists in the `needsConversion` path (lines 850–868). Consider whether `blockAdj`/`inlineAdj` should use the child's geometry converted to the parent's frame rather than computed directly in the parent's frame.

2. Run the full set of abs-pos tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos" -count=1
```

**Milestone 3: Run all affected tests** (commit if adjustments needed)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos" -count=1
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos-border-offset" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos-vlr" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos-replaced" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/abs-pos-with-replaced" -count=1
```

---

## Target 5: Overconstrained relative positioning in vertical writing modes (~9 tests)

### Problem
Two related issues: (1) `computeRelativeOffset()` has errors in the physical-to-logical-to-physical roundtrip for some WM+direction combinations, and (2) the overconstrained inline-margin recalculation (CSS 2.1 §10.3.3) is missing for vertical writing modes.

### Affected Tests (~9 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| overconstrained-rel-pos-ltr-left-right-vrl | 004 | 4.0% |
| overconstrained-rel-pos-ltr-top-bottom-vrl | 002 | 1.3% |
| overconstrained-rel-pos-rtl-left-right-vlr | 009 | 1.3% |
| overconstrained-rel-pos-rtl-left-right-vrl | 008 | 1.3% |
| overconstrained-rel-pos-rtl-top-bottom-vlr | 007 | 1.3% |
| overconstrained-rel-pos-rtl-top-bottom-vrl | 006 | 1.3% |
| normal-flow-overconstrained-vlr | 003 | 0.1% |
| normal-flow-overconstrained-vrl | 002, 004 | 0.3–1.3% |

### Root Cause (from code analysis)

**Bug A — Relative offset sign errors in some WM+direction combinations.**

`pkg/layout/block_layout.go`, lines 745–800 — `computeRelativeOffset()`:

The function converts physical insets to logical via `PhysicalInsetsToLogical`, applies "inline-start wins" and "block-start wins" in logical space, then converts back to physical dx/dy. The CSS 2.1 §9.4.3 rule:
- **In LTR:** `left` wins over `right` (both axes, regardless of writing mode).
- **In RTL:** `right` wins over `left`.
- These are **physical** rules based on `direction`, mapped to logical as "inline-start always wins."

The current logical-to-physical conversion (lines 767–799) has distinct branches for each writing mode. The 4.0% diff on `overconstrained-rel-pos-ltr-left-right-vrl-004` suggests a sign error in the VRL+LTR `left`/`right` branch. In VRL LTR:
- `left` = physical left = block-end (VRL block-start is right)
- `right` = physical right = block-start
- `left` wins in LTR → `left` offset is applied → `dx = left` (shift right by `left` amount)

But `PhysicalInsetsToLogical` for VRL maps: `left` → `BlockEnd`, `right` → `BlockStart`. Then "block-start wins": `blockDelta = right`. This is wrong — CSS says "left wins in LTR" but the logical mapping made `right` = block-start, and block-start wins. The physical rule "left wins" and the logical rule "start wins" conflict because in VRL, physical left is block-END.

**Bug B — Missing overconstrained inline-margin recalculation.**

`pkg/layout/block_layout.go`, lines 360–378: The inline-axis margin handling only covers `auto` margins for centering. CSS 2.1 §10.3.3 says: when a non-replaced block's width + horizontal margin + border + padding > containing block width, and no margins are auto, the inline-end margin must be recalculated to balance the equation. In vertical modes, "width" is `height`, and "horizontal margin" is `margin-top`/`margin-bottom`. This recalculation is entirely absent.

### What Blink Does (STUDY THIS FIRST)

Blink resolves relative offsets in `ComputeRelativeOffset()` in `layout_box_utils.cc`. **Critically, Blink works entirely in physical coordinates** — no logical roundtrip:

```cpp
// Simplified from layout_box_utils.cc
if (IsLtr(direction)) {
    // left wins
    if (has_left)
        dx = left;
    else if (has_right)
        dx = -right;
} else {
    // right wins
    if (has_right)
        dx = -right;
    else if (has_left)
        dx = left;
}
// top always wins over bottom
if (has_top)
    dy = top;
else if (has_bottom)
    dy = -bottom;
```

The physical `left`/`right`/`top`/`bottom` apply directly as physical `dx`/`dy`. Writing mode is irrelevant — relative positioning is purely physical. The `direction` property determines which horizontal offset wins (left vs right). Top always wins over bottom.

For overconstrained inline margins, Blink's `ResolveInlineMargins()` in `block_layout_algorithm.cc` explicitly adjusts the inline-end margin:
```cpp
LayoutUnit inline_end = child_available_size - margins.inline_start -
                        margins.inline_end - inline_size;
if (inline_end != 0)
    margins.inline_end += inline_end;
```

Key sources:
- `third_party/blink/renderer/core/layout/layout_box_utils.cc` — `ComputeRelativeOffset()`
- `third_party/blink/renderer/core/layout/block_layout_algorithm.cc` — `ResolveInlineMargins()`

### Implementation Steps and Milestones

**Milestone 1: Rewrite `computeRelativeOffset()` to be purely physical** (commit after this)

Following Blink's approach, replace the current logical-roundtrip implementation with a direct physical computation:

```go
func computeRelativeOffset(offset css.PositionOffset, wdm WritingDirectionMode) PhysicalOffset {
    var dx, dy float64

    // CSS 2.1 §9.4.3: direction determines which horizontal offset wins.
    // "left" and "right" are always physical horizontal offsets.
    if wdm.Dir == DirectionLTR {
        // LTR: left wins
        if offset.HasLeft {
            dx = offset.Left
        } else if offset.HasRight {
            dx = -offset.Right
        }
    } else {
        // RTL: right wins
        if offset.HasRight {
            dx = -offset.Right
        } else if offset.HasLeft {
            dx = offset.Left
        }
    }

    // top always wins over bottom (physical vertical, independent of WM)
    if offset.HasTop {
        dy = offset.Top
    } else if offset.HasBottom {
        dy = -offset.Bottom
    }

    return PhysicalOffset{X: dx, Y: dy}
}
```

Note: Check that `css.PositionOffset` has `HasLeft`/`Left`/`HasRight`/`Right`/`HasTop`/`Top`/`HasBottom`/`Bottom` fields. If the current struct uses a different layout, adapt accordingly. The key insight is: **no logical conversion needed at all**.

Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/overconstrained-rel-pos" -count=1
```

**Milestone 2: Implement overconstrained inline-margin recalculation** (commit after this)

In `pkg/layout/block_layout.go`, near the inline positioning code (around line 378), after computing `childInlineOffset` and when neither inline margin is auto, add the overconstrained check:

```go
// CSS 2.1 §10.3.3: overconstrained non-replaced blocks.
// If both inline margins are specified (not auto) and the total exceeds
// the container, adjust the inline-end margin.
if !autoInlineStart && !autoInlineEnd {
    totalUsed := resolvedInlineSize + childGeom.InlineBorderPadding() +
        childMargins.InlineStart + childMargins.InlineEnd
    overflow := totalUsed - contentInlineSize
    if overflow != 0 {
        childMargins.InlineEnd -= overflow
        // Recompute childInlineOffset since margins changed
        childInlineOffset = childMargins.InlineStart + floatStartOff
    }
}
```

Note: You need to find where `resolvedInlineSize` and `childGeom` are available in this scope. Trace the code to find the right variables. The key is: `inline-end margin = container inline size - inline-start margin - border - padding - content inline size`.

Run tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/normal-flow-overconstrained" -count=1
```

**Milestone 3: Run all affected tests** (commit if adjustments needed)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(overconstrained-rel-pos|normal-flow-overconstrained)" -count=1
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/overconstrained-rel-pos" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/normal-flow-overconstrained" -count=1
```

---

## Target 6: requestAnimationFrame and onload attribute support in JS engine (8 tests)

### Problem
The JS engine does not implement `requestAnimationFrame` or fire element-level `onload` attribute handlers. Many WPT tests use JavaScript to mutate the DOM (resize iframes, change styles, remove `reftest-wait` class) before the reftest screenshot is taken. Without these APIs, the mutations never happen and the tests render the initial (wrong) state.

### Affected Tests (8 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| orthogonal-root-resize-icb | 001, 002, 003, 004, 005, 006, 007 | 1.1–2.1% |
| orthogonal-child-with-border | 001 | 1.0% |

### Root Cause (from code analysis)

**The test patterns that fail:**

Test `orthogonal-root-resize-icb-001.html` (representative of 001–007):
```html
<iframe id="iframe" src="data:text/html,..."></iframe>
<script>
  iframe.onload = function() {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        iframe.style.height = "100px";
        document.documentElement.classList.remove("reftest-wait");
      });
    });
  }
</script>
```

Test `orthogonal-child-with-border.html`:
```html
<script>
  function run() {
    inner.style.width = "50px";
    document.documentElement.classList.remove("reftest-wait");
  }
</script>
<body onload="run()">
```

**Three missing JS engine features:**

1. **`requestAnimationFrame` not defined.** (`pkg/js/engine.go`) — The function doesn't exist in the goja runtime. Nested `requestAnimationFrame(() => requestAnimationFrame(() => {...}))` calls silently fail because `requestAnimationFrame` is `undefined`. In our single-threaded test environment, rAF callbacks should execute immediately (synchronously).

2. **Element-level `onload` not supported.** (`pkg/js/dom.go`) — When JS sets `iframe.onload = function() {...}`, the function is stored as a property on the DOM proxy object but never called. The iframe's content loads during layout (in `layoutNestedDocument()` / `tryLayoutNestedDocument()` in `block_layout.go:980`), but there's no callback mechanism to fire `onload` after loading completes.

3. **`<body onload="...">` inline attribute not handled.** (`pkg/js/engine.go`) — The `Execute()` function only checks `window.onload`. The `<body onload="run()">` HTML attribute is a separate mechanism: the parser stores it as an attribute on the body node, but nobody reads it and executes it as JS.

### What Blink Does

In Blink, `requestAnimationFrame` schedules a callback via `ScriptedAnimationController` before the next compositing frame. For reftests, this means "run after current script, before screenshot." Element `onload` events fire through the standard DOM event dispatch pipeline. `<body onload="...">` is handled by the HTML parser registering it as an event listener.

For our purposes (single-threaded test rendering), all three can be simplified to synchronous execution.

### Implementation Steps and Milestones

**Milestone 1: Implement `requestAnimationFrame` as synchronous** (commit after this)

In `pkg/js/engine.go`, add rAF registration in `New()`:

```go
func New() *Engine {
    vm := goja.New()
    e := &Engine{vm: vm}

    c := &consoleAPI{}
    c.register(vm)

    vm.Set("window", vm.GlobalObject())

    // requestAnimationFrame: in our single-threaded test environment,
    // execute callbacks immediately. Tests use nested rAF to wait for
    // "next frame" which in our case is just "later in the same tick."
    var rafID int64
    vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
        rafID++
        if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
            // Pass a fake timestamp (DOMHighResTimeStamp)
            fn(goja.Undefined(), vm.ToValue(0))
        }
        return vm.ToValue(rafID)
    })
    // cancelAnimationFrame: no-op since callbacks execute immediately
    vm.Set("cancelAnimationFrame", func(call goja.FunctionCall) goja.Value {
        return goja.Undefined()
    })

    return e
}
```

Run a quick compile check:
```bash
cd /Users/iansmith/louis14 && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go build ./pkg/js/...
```

**Milestone 2: Handle `<body onload="...">` attribute** (commit after this)

In `pkg/js/engine.go`, at the end of `Execute()`, after firing `window.onload`, add:

```go
// Fire <body onload="..."> if present.
// CSS 2.1 and HTML spec: the body element's onload attribute is an
// alias for window.onload, but many tests use it directly.
if doc.Body != nil {
    if onloadAttr, ok := doc.Body.GetAttribute("onload"); ok {
        if _, err := e.vm.RunString(onloadAttr); err != nil {
            return fmt.Errorf("body onload: %w", err)
        }
    }
}
```

Note: Check how `doc.Body` is accessed — it might be `doc.Body()` (method) or a field. Look at `pkg/html/document.go` for the `Document` struct. The body node needs a `GetAttribute(name string) (string, bool)` method — check if `html.Node` already has this (it likely does based on the DOM proxy code in `dom.go`).

Run the orthogonal-child-with-border test:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-child-with-border" -count=1
```

**Milestone 3: Handle element-level onload for iframes** (commit after this)

This is the trickiest part. The `orthogonal-root-resize-icb` tests set `iframe.onload = function() {...}` via JS, then expect the callback to fire after the iframe loads. The challenge: iframes load during layout (in `tryLayoutNestedDocument`), which happens AFTER `Execute()` returns.

Approach:
1. In `pkg/js/dom.go`, when JS sets a property like `element.onload = fn`, store the callback in a map on the Engine: `engine.elementCallbacks[nodeID]["onload"] = fn`.
2. In `pkg/layout/block_layout.go:tryLayoutNestedDocument()` (around line 980–1010), after successfully loading the nested document, check if the iframe node has a registered `onload` callback. If so, fire it.
3. This requires the layout engine to have access to the JS engine — add an optional `JSEngine` field to `LayoutContext` that can fire pending callbacks.
4. After firing `onload`, `requestAnimationFrame` callbacks within it will execute immediately (from Milestone 1). Then `iframe.style.height = "100px"` will modify the DOM, and `classList.remove("reftest-wait")` will update the class.
5. **Important:** After firing onload and the DOM mutation, the layout needs to happen AGAIN with the updated styles. The test runner's `reftest-wait` mechanism should handle this — if `reftest-wait` is present on `<html>`, the engine should re-render after it's removed. Check how the test runner handles `reftest-wait` in `pkg/visualtest/reftest_runner_test.go`.

Alternative simpler approach: Instead of wiring callbacks through layout, run `Execute()` **twice** — once to register callbacks and run scripts, then trigger a synthetic onload for all iframes found in the DOM, then re-execute to process the mutations. Check if this is feasible given the test runner architecture.

Run the orthogonal-root-resize-icb tests:
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-root-resize-icb" -count=1
```

**Milestone 4: Run all affected tests** (final commit)
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/(orthogonal-root-resize-icb|orthogonal-child-with-border)" -count=1
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-root-resize-icb" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-child-with-border" -count=1
```

---

## Independence Check

| | exclusion_space.go | block_layout.go | css/style.go | render/render.go | out_of_flow_layout.go | js/engine.go + dom.go |
|---|---|---|---|---|---|---|
| Target 1 (float/clear) | **Primary** | layoutFloat (939–960), BFC avoidance (246–279) | - | - | - | - |
| Target 2 (ch-units) | - | - | **Primary** (227–242) | - | - | - |
| Target 3 (background) | - | - | - | **Primary** (1453–1555) | - | - |
| Target 4 (abs-pos CB) | - | OOF init (692), PropagateOOF (835–837) | - | - | **Primary** (48–240) | - |
| Target 5 (rel-pos) | - | computeRelativeOffset (745–800), margin (360–378) | - | - | - | - |
| Target 6 (JS rAF) | - | - | - | - | - | **Primary** |

**Overlap in block_layout.go:** Targets 1, 4, and 5 all touch `block_layout.go` but at well-separated line ranges:
- Target 1: lines 246–279 and 939–960 (float avoidance + layoutFloat)
- Target 4: lines 692 and 835–837 (OOF init + PropagateOOF)
- Target 5: lines 360–378 and 745–800 (margins + relative offset)

These are all different functions with no shared state. Merge conflicts are unlikely. If concerned, merge Target 5 last since its line ranges (360–378) are closest to Target 4's (692).

Targets 2, 3, and 6 are **fully independent** — no shared files at all.
