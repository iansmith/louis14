# Writing Modes Round 5: Top 6 Improvements

Current state: 673 pass / 114 fail (85.5% pass rate) across 787 tests.

**Note:** The block-flow-direction collapse-through OOF propagation bug has already been fixed in the working tree (block_layout.go lines 344–368). This fix resolves 12 of the 14 block-flow-direction/line-box-direction tests. Commit this fix before launching agents.

These six targets are **independent** (touch different primary subsystems) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Float clear/side physical-vs-logical confusion (~25 tests)

### Problem
`float: left/right` and `clear: left/right` are physical CSS values, but the exclusion space stores float sides using a half-logical scheme. `ClearanceOffset()` matches physical `clear:left` against what it assumes is logical `FloatLeft`, creating mismatches in vertical writing modes. Additionally, the physical→logical conversion for float sides only accounts for `direction: rtl`, not for vertical writing modes where the physical-to-logical axis mapping is entirely different.

### Affected Tests (~25 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| ortho-htb-alongside-vrl-floats | 4 | 18–28% |
| contiguous-floated-table-vlr/vrl | 8 | 1–2.1% |
| clearance-calculations-vrl | 4 | 1–3.3% |
| float-vlr/vrl basic | 5 | 0.5–1.6% |
| float-lft-orthog (orthogonal floats) | 4 | 0.4% |

### Root Cause (from code analysis)

**File:** `pkg/layout/block_layout.go`, lines 939–950 — `layoutFloat()`:
```go
logicalSide := floatSide
if parentWDM.Dir == DirectionRTL {
    if floatSide == css.FloatLeft {
        logicalSide = css.FloatRight
    } else {
        logicalSide = css.FloatLeft
    }
}
```
This only swaps for RTL direction, not for vertical writing modes. In `vertical-rl` with LTR, `float:left` is stored as `FloatLeft` (inline-start), but physically "left" in VRL is block-end.

**File:** `pkg/layout/exclusion_space.go`, lines 106–113 — `ClearanceOffset()`:
```go
case css.ClearLeft:
    shouldClear = e.Side == css.FloatLeft
```
This compares physical `clear:left` against the stored (half-logical) `FloatLeft` side. In vertical modes, `clear:left` should clear floats on the physical left (which is block-end in VRL), but `FloatLeft` is inline-start (physical top in VRL). Wrong exclusions are matched.

**File:** `pkg/layout/block_layout.go`, lines 246–279 — orthogonal child float-avoidance:
The `childAvailableInline` check for BFC children uses the parent's inline axis, but for orthogonal children the "inline" size is in a different coordinate frame. This causes 18–28% diffs in `ortho-htb-alongside-vrl-floats` tests.

### What Blink Does
In Blink's LayoutNG, `float: left/right` values are mapped to `EFloat::kLeft/kRight` which are **physical**. The exclusion space operates in physical coordinates. `clear: left` physically clears physical-left floats. There is no physical-to-logical conversion for float sides — the entire float/exclusion system is physical. The logical coordinate conversion happens when positioning the non-float content around exclusions, not when storing or matching float sides. See `exclusion_space.cc` and `unpositioned_float.cc` in `third_party/blink/renderer/core/layout/`.

### Fix Location
1. **`pkg/layout/exclusion_space.go`**: Store float exclusions with **physical** sides (no conversion). `ClearanceOffset()` already compares `ClearLeft` against `FloatLeft` — keep this but ensure both represent physical sides.
2. **`pkg/layout/block_layout.go:layoutFloat()`** (lines 939–950): Remove the RTL swap. Store the float's physical side directly in the exclusion. The inline positioning (lines 952–960) should convert physical side to logical for placement math.
3. **`pkg/layout/block_layout.go`** (lines 246–279): When computing whether an orthogonal BFC child fits beside floats, convert the child's needed inline-size to the parent's coordinate frame before comparing against float exclusion offsets.

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ortho-htb-alongside-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/clearance-calculations-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/contiguous-floated-table" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-v" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-lft-orthog" -count=1
```

---

## Target 2: ch-unit ignores text-orientation in vertical writing modes (8 tests)

### Problem
The `chScale()` function returns `1.0` for all vertical writing modes regardless of `text-orientation`. Per CSS Values §6.1, `ch` is the advance measure of "0" in the **inline axis**. With `text-orientation: sideways`, glyphs are rotated 90°, so the inline advance of "0" is the horizontal advance width (~0.5em), not 1em.

### Affected Tests (8 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| ch-units-vrl (upright) | 001–004 | 2.1–4.7% |
| ch-units-vrl (sideways) | 005–008 | 1.0–3.1% |

### Root Cause (from code analysis)

**File:** `pkg/css/style.go`, lines 227–242 — `chScale()`:
```go
func (s *Style) chScale() float64 {
    wm, _ := s.Get("writing-mode")
    if wm == "vertical-rl" || wm == "vertical-lr" ||
        wm == "sideways-rl" || wm == "sideways-lr" {
        // Vertical modes: ch = vertical advance height of "0" ≈ 1em
        return 1.0
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

The function hard-codes `1.0` for ALL vertical modes. For `text-orientation: sideways`, the "0" glyph is displayed sideways, so `ch` = horizontal advance width (same as in horizontal mode, ~0.5em). Only `text-orientation: upright` (and `mixed` for Latin) should use `1.0`.

Additionally, `sideways-rl` and `sideways-lr` writing modes always display glyphs sideways (the `text-orientation` property has no effect), so they should always use the horizontal advance width.

### What Blink Does
Blink's `ComputedStyle::GetFontSizeStyle()` checks both `writing-mode` and `text-orientation` to determine the `ch` unit. In `vertical-rl`/`vertical-lr` with `text-orientation: upright`, ch = 1em (vertical advance). In `vertical-rl`/`vertical-lr` with `text-orientation: sideways` or in `sideways-rl`/`sideways-lr`, ch = horizontal advance of "0". See `computed_style.cc` and `font_metrics.h`.

### Fix Location
**`pkg/css/style.go:chScale()`** (lines 227–242):
```go
func (s *Style) chScale() float64 {
    wm, _ := s.Get("writing-mode")
    if wm == "vertical-rl" || wm == "vertical-lr" {
        to, _ := s.Get("text-orientation")
        if to == "sideways" {
            // Sideways: glyphs rotated, ch = horizontal advance
            if s.ChWidth > 0 {
                if fs := s.GetFontSize(); fs > 0 {
                    return s.ChWidth / fs
                }
            }
            return 0.5
        }
        // upright or mixed: ch = vertical advance ≈ 1em
        return 1.0
    }
    if wm == "sideways-rl" || wm == "sideways-lr" {
        // sideways-* always rotates glyphs: ch = horizontal advance
        if s.ChWidth > 0 {
            if fs := s.GetFontSize(); fs > 0 {
                return s.ChWidth / fs
            }
        }
        return 0.5
    }
    // Horizontal mode
    if s.ChWidth > 0 {
        if fs := s.GetFontSize(); fs > 0 {
            return s.ChWidth / fs
        }
    }
    return 0.5
}
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/ch-units-vrl" -count=1
```

---

## Target 3: Background origin uses element box instead of ICB for canvas background (7 tests)

### Problem
When the root element's background is painted on the canvas (`PaintsCanvasBackground = true`), `background-position` and `background-size` percentages resolve against the element's own box instead of the Initial Containing Block (viewport). In vertical writing modes, the root element's box may be offset or sized differently from the viewport, causing background images to be mispositioned.

### Affected Tests (7 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| background-position-vrl | 018, 020, 022 | 12.5% |
| background-size-document-root-vrl | 002, 004, 006, 008 | 14.4% |

### Root Cause (from code analysis)

**File:** `pkg/render/render.go`, lines 1453–1481 — `drawBackgroundImageLayer()`:
```go
if isFixed {
    bounds := r.target.Bounds()
    originX, originY = float64(bounds.Min.X), float64(bounds.Min.Y)
    originW, originH = float64(bounds.Dx()), float64(bounds.Dy())
} else {
    switch bg.Origin {
    // ... uses box.X, box.Y, box.Width, box.Height
    }
}
```

When `PaintsCanvasBackground` is true and attachment is NOT fixed, the origin still uses the element's box coordinates. The clip area (lines 1550–1555) correctly uses the full canvas, but the positioning area does not.

Per CSS Backgrounds §3.10: when the root element's background is propagated to the canvas, the background positioning area is the ICB. Per CSS Writing Modes §7.6: `background-position` and `background-size` are physical properties unaffected by writing mode.

### What Blink Does
In Blink's `BoxPainter::PaintFillLayer()` and `BackgroundImageGeometry::Calculate()`, when painting the root element's background on the canvas, the positioning area is set to the ICB dimensions, not the root element's box. See `box_painter.cc` and `background_image_geometry.cc`.

### Fix Location
**`pkg/render/render.go`** (lines 1453–1481): Add a check for `layer.PaintsCanvasBackground` alongside the `isFixed` check:
```go
if isFixed || layer.PaintsCanvasBackground {
    bounds := r.target.Bounds()
    originX = float64(bounds.Min.X)
    originY = float64(bounds.Min.Y)
    originW = float64(bounds.Dx())
    originH = float64(bounds.Dy())
} else {
    // existing box-based logic
}
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-size-document-root-vrl" -count=1
```

---

## Target 4: Absolute positioning uses content-box instead of padding-box for CB size (~7 tests)

### Problem
The containing block size passed to `OutOfFlowLayoutPart` is the content-box size, but CSS 2.1 §10.3.7 specifies that the constraint equation for absolutely positioned elements uses the **padding-box** of the containing block. Additionally, `PropagateOOFCandidates` computes child border/padding using the parent's WDM instead of the child's WDM, causing wrong static position offsets when writing modes differ.

### Affected Tests (~7 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| abs-pos-border-offset | 001, 002, 003 | 2.4–5.4% |
| abs-pos-vlr-border/padding | 001, 001 | 8.1% |
| abs-pos-replaced-vrl | 001 | 0.9% |
| abs-pos-with-replaced-child | 1 | 2.1% |

### Root Cause (from code analysis)

**Bug A — File:** `pkg/layout/block_layout.go`, line 692:
```go
containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
```
This passes content-box dimensions. The constraint equation (CSS 2.1 §10.3.7) states: `left + margin-left + border-left + padding-left + width + padding-right + border-right + margin-right + right = containing block width`. The "containing block width" is the **padding-box width** per spec.

**Bug B — File:** `pkg/layout/block_layout.go`, line 837:
```go
childBP := ComputeFragmentGeometry(childStyle, parentWDM)
```
When the child's writing mode is orthogonal to the parent, this interprets the child's physical borders using the parent's WDM mapping. `blockAdj` and `inlineAdj` (lines 838–839) then add the wrong border/padding amounts, displacing the static position.

### What Blink Does
In Blink's `OutOfFlowLayoutPart`, the containing block size is explicitly set to the padding-box size. See `out_of_flow_layout_part.cc` — the constraint space's available size for OOF children includes padding. For OOF propagation, child geometry is always computed in the child's own WDM.

### Fix Location
1. **`pkg/layout/block_layout.go:692`**: Change to padding-box:
```go
containingBlockSize: LogicalSize{
    InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
    BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
},
```
2. **`pkg/layout/out_of_flow_layout.go`**: The inset offsets already position from the padding-box edge, so the final physical offset must be adjusted to account for the CB's border (subtract border-start from the logical offset before converting to physical). Add border offset: after computing `inlineOffset` and `blockOffset`, the final physical position should be relative to the CB's border-box origin, meaning `inlineOffset` already includes the padding implicitly. Review the final `builder.AddChild` call to ensure the offset is from the CB's content-box origin (where children are normally placed) — if so, subtract `geom.Padding.InlineStart` and `geom.Padding.BlockStart` from the computed offsets.
3. **`pkg/layout/block_layout.go:837`**: Change `parentWDM` to `childWDM`:
```go
childBP := ComputeFragmentGeometry(childStyle, childWDM)
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
The `computeRelativeOffset()` function maps physical insets to logical and applies "start wins over end", but the physical-to-logical-to-physical roundtrip has sign errors in some WM+direction combinations. Additionally, normal-flow overconstrained inline margins (CSS 2.1 §10.3.3) are not implemented for vertical writing modes.

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

**Bug A — File:** `pkg/layout/block_layout.go`, lines 745–800 — `computeRelativeOffset()`:
The function correctly maps physical insets to logical, applies "start wins," and converts back. However, for overconstrained cases (both `left` and `right` specified with `position: relative` in VRL+RTL or VLR+RTL), the choice of which inset "wins" depends on the direction:
- CSS 2.1 §9.4.3: In LTR, `left` wins over `right`. In RTL, `right` wins over `left`.
- In logical terms: inline-start always wins.
The current code does this correctly in the logical domain (line 752: `HasInlineStart` wins). But the physical-to-logical mapping via `PhysicalInsetsToLogical` must correctly map `left`/`right` to inline-start/inline-end for each WM+direction. If this mapping is wrong for any combination, the wrong inset wins.

**Bug B — File:** `pkg/layout/block_layout.go`, lines 360–378:
The inline-axis margin handling only covers `auto` margins for centering. There is no implementation of CSS 2.1 §10.3.3's overconstrained rule: when a non-replaced block's inline-axis margins, border, padding, and size sum to more or less than the container's inline size, the inline-end margin must be adjusted. In vertical modes, the inline axis is physical height, so `margin-top`/`margin-bottom` become the inline margins. Without this recalculation, elements are mispositioned.

### What Blink Does
Blink resolves relative offsets in `ComputeRelativeOffset()` in `layout_box_utils.cc`. The function operates entirely in physical coordinates — it doesn't convert to logical and back. It directly applies "left wins in LTR, right wins in RTL" without axis conversion. For overconstrained normal flow margins, `ResolveInlineMargins()` in `block_layout_algorithm.cc` explicitly adjusts the inline-end margin when the equation doesn't balance.

### Fix Location
1. **`pkg/layout/block_layout.go:computeRelativeOffset()`**: Verify `PhysicalInsetsToLogical` for every WM+direction combo. Alternatively, follow Blink and compute entirely in physical coordinates without the logical roundtrip: in LTR `left` wins, in RTL `right` wins, regardless of writing mode.
2. **`pkg/layout/block_layout.go`** (near line 378): After computing `childInlineOffset`, add overconstrained margin recalculation:
```go
// CSS 2.1 §10.3.3: If the inline dimension is overconstrained,
// recalculate the inline-end margin.
if !autoInlineStart && !autoInlineEnd {
    usedInline := resolvedInlineSize + childGeom.InlineBorderPadding() +
        childMargins.InlineStart + childMargins.InlineEnd
    if usedInline != contentInlineSize {
        // Override inline-end margin
        childMargins.InlineEnd = contentInlineSize - resolvedInlineSize -
            childGeom.InlineBorderPadding() - childMargins.InlineStart
    }
}
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/overconstrained-rel-pos" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/normal-flow-overconstrained" -count=1
```

---

## Target 6: requestAnimationFrame and onload attribute support in JS engine (8 tests)

### Problem
The JS engine does not implement `requestAnimationFrame` or fire element-level `onload` attribute handlers (e.g., `<body onload="run()">`). Many writing modes tests rely on JavaScript to mutate the DOM before the reftest screenshot is taken, and without these APIs the mutations never happen.

### Affected Tests (8 failures)
| Category | Tests | Diff Range |
|----------|-------|------------|
| orthogonal-root-resize-icb | 001–007 | 1.1–2.1% |
| orthogonal-child-with-border | 001 | 1.0% |

### Root Cause (from code analysis)

**File:** `pkg/js/engine.go`, lines 37–60 — `Execute()`:
```go
func (e *Engine) Execute(doc *html.Document) error {
    registerDocument(e.vm, doc)
    for i, script := range doc.Scripts {
        _, err := e.vm.RunString(script)
        // ...
    }
    // Fire onload if any script registered it
    if onloadVal := e.vm.Get("onload"); onloadVal != nil {
        // ...
    }
    return nil
}
```

Problems:
1. `requestAnimationFrame` is not defined in the JS runtime. The tests use `requestAnimationFrame(() => { requestAnimationFrame(() => { /* mutate DOM */ }) })`. Since it's undefined, the nested callbacks never fire.
2. Element-level `onload` (e.g., `iframe.onload = function() {...}`) is not supported. Only `window.onload` is checked.
3. `<body onload="run()">` inline event handler attributes are not collected from the HTML parser.

### What Blink Does
In Blink, `requestAnimationFrame` schedules a callback before the next paint. For reftest purposes, this effectively means "run after layout but before screenshot." Element `onload` events fire via the standard DOM event dispatch.

### Fix Location
1. **`pkg/js/engine.go`**: Add `requestAnimationFrame` as a synchronous immediate-call:
```go
// In New() or Execute(), register:
vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
    if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
        fn(goja.Undefined())  // Execute immediately (single-threaded)
    }
    return vm.ToValue(0) // Return fake timer ID
})
```

2. **`pkg/js/dom.go`**: Support setting `onload` on DOM elements. When `iframe.onload = fn` is set, store the callback. After the iframe's content is loaded and laid out, fire the callback.

3. **`pkg/html/parser.go`**: Collect inline event handler attributes (`onload`, `onclick`, etc.) from HTML elements. After all scripts run, check `<body>` for an `onload` attribute and execute it:
```go
// After script execution, check body onload
if body := doc.Body(); body != nil {
    if onload, ok := body.GetAttribute("onload"); ok {
        vm.RunString(onload)
    }
}
```

### Verification
```bash
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-root-resize-icb" -count=1
cd pkg/visualtest && GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test -v -run "TestWPTCSS3Reftests/css-writing-modes/orthogonal-child-with-border" -count=1
```

---

## Independence Check

| | exclusion_space.go | block_layout.go | css/style.go | render/render.go | out_of_flow_layout.go | js/engine.go |
|---|---|---|---|---|---|---|
| Target 1 (float/clear) | **Primary** | layoutFloat (939–960) | - | - | - | - |
| Target 2 (ch-units) | - | - | **Primary** (227–242) | - | - | - |
| Target 3 (background) | - | - | - | **Primary** (1453–1555) | - | - |
| Target 4 (abs-pos CB) | - | OOF init (692), PropagateOOF (837) | - | - | **Primary** (48–240) | - |
| Target 5 (rel-pos/overconstrained) | - | computeRelativeOffset (745–800), margin (360–378) | - | - | - | - |
| Target 6 (JS rAF/onload) | - | - | - | - | - | **Primary** |

**Overlap note:** Targets 1, 4, and 5 all touch `block_layout.go` but at well-separated locations (939–960, 692+837, 745–800+360–378). These functions are independent and merge conflicts are unlikely. If concerned, merge Target 5 last. Targets 2, 3, and 6 are fully independent.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Mirror their type names, algorithm structure, and constraint-passing patterns.
- **Commit and report at each milestone** (don't batch everything to the end).
- **Run ONLY the specific tests listed** in each target's Verification section — do NOT run the full writing-modes suite.
- **Use `GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go`** as the Go command.
- **Never use `open`** to display files — it disrupts the user's screen.
- When running in a worktree, commit ONLY to your worktree branch. Never commit directly to fix/* or master branches from a worktree.
- A 0.5% diff is a failure just like 28%. ALL tests must pass at 0% diff.
