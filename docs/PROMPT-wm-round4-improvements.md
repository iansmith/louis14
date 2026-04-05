# CSS Writing Modes Round 4: Top 3 Improvements

Current state: 658 pass / 129 fail (83.6% pass rate) across 787 tests.
Test command: `cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1`

These three targets are **independent** (touch different source files) and can be worked on in parallel by separate worktree agents.

---

## Target 1: Table Border-Collapse Conflict Resolution (~12 tests)

### Problem

When `border-collapse: collapse` is set on a table in vertical writing modes, adjacent cell borders must be resolved according to CSS 2.1 §17.6.2.1. The current table layout code detects `border-collapse` (line 78) and zeroes out spacing (lines 82-85), but performs **no border conflict resolution** — each cell's borders are rendered independently, resulting in doubled/overlapping borders instead of a single resolved border.

### Affected Tests (~12 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| border-conflict-element-vlr-003/005/007/009/011/013 | 6 | Border collapse conflict in vertical-lr |
| border-conflict-element-vrl-002/004/006/008/010/012 | 6 | Border collapse conflict in vertical-rl |

### Root Cause (from code analysis)

In `pkg/layout/table_layout.go`:

**Line 78:** Border-collapse is detected:
```go
borderCollapse := tla.style.GetBorderCollapse() == css.BorderCollapseCollapse
```

**Lines 82-85:** Spacing is correctly zeroed:
```go
inlineSpacing, blockSpacing := 0.0, 0.0
if !borderCollapse {
    inlineSpacing, blockSpacing = tla.logicalBorderSpacing()
}
```

**Lines 250-258:** Row/cell borders are set WITHOUT conflict resolution:
```go
if row.node != nil && row.style != nil {
    physBorder := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Border, wdm)
    physPadding := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Padding, wdm)
    rowBuilder.SetBoxData(&PhysicalBoxData{
        Border:  physBorder,
        Padding: physPadding,
    })
}
```

**What's missing:** The CSS 2.1 §17.6.2.1 border conflict resolution algorithm, which determines which border "wins" when two adjacent cells have different border styles. The algorithm must work in logical coordinates since the tests use vertical writing modes.

### What Blink Does

Blink implements border conflict resolution in `table_borders.cc`:

1. For each edge between two adjacent cells, compare their border styles
2. **Conflict resolution rules (§17.6.2.1):**
   - `hidden` always wins (hides the border)
   - Wider borders win over narrower
   - For equal widths: double > solid > dashed > dotted > ridge > outset > groove > inset
   - For equal width+style: cell > row > row-group > column > column-group > table
   - For same element type: the border from the cell that is inline-start or block-start wins
3. The "start wins" tiebreaker uses **logical** coordinates — in vertical-lr LTR, inline-start is physical top and block-start is physical left

### Fix Location

**File: `pkg/layout/table_layout.go`**

**Step 1: Collect cell borders during the row loop (~lines 130-270)**

During cell layout, store each cell's border values (all 4 logical sides) along with its grid position (row index, column index, colspan, rowspan).

**Step 2: Implement conflict resolution**

After all cells are laid out, resolve borders for each shared edge:

```go
// For each horizontal (inline) edge between row i and row i+1:
//   Compare row[i]'s block-end border with row[i+1]'s block-start border
//   Use CSS 2.1 §17.6.2.1 rules to pick winner
// For each vertical (block) edge between col j and col j+1:
//   Compare col[j]'s inline-end border with col[j+1]'s inline-start border
//   Use CSS 2.1 §17.6.2.1 rules to pick winner
```

**Step 3: Apply resolved borders to cell fragments**

Replace each cell's border with the resolved values. In collapsed mode, the border is shared — each cell gets half the winning border's width (CSS 2.1 §17.6.2: "half the border is drawn on each side of the cell gap").

**Step 4: Adjust cell positioning for collapsed borders**

In collapsed mode, cells overlap by half the border width. Adjust inline offsets and row heights accordingly.

### Milestones

1. **Milestone 1:** Read 2-3 test HTML files and their references to understand expected visual output. Commit a no-op scaffold with the data structures needed. Run tests to confirm no regressions.
2. **Milestone 2:** Implement the basic conflict resolution algorithm (wider wins, style precedence). Apply resolved borders to cell fragments. Run border-conflict tests to check progress.
3. **Milestone 3:** Handle the "start wins" tiebreaker in logical coordinates (the part that's writing-mode-dependent). Run full suite for regressions.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/border-conflict" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Target 2: Float Positioning and Clearance in Vertical Writing Modes (~13 tests)

### Problem

Float positioning and clearance calculations have multiple issues in vertical writing modes. The core exclusion space works in logical coordinates (correct), but there are issues with: (1) how `clear: left/right` maps to logical sides in vertical modes, (2) margin collapsing behavior around clearance in vertical modes, and (3) orthogonal float sizing when the float's writing mode differs from the parent's.

### Affected Tests (~13 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| float-vlr-007/011/013 | 3 | Basic floats in vertical-lr |
| float-vrl-010/012 | 2 | Basic floats in vertical-rl |
| float-lft-orthog-htb-in-vlr/vrl-002 | 2 | Orthogonal horizontal float in vertical parent |
| float-lft-orthog-vlr/vrl-in-htb-002 | 2 | Orthogonal vertical float in horizontal parent |
| clearance-calculations-vrl-002/004/006/008 | 4 | Clearance in vertical-rl with margin collapse |

### Root Cause (from code analysis)

**Issue 1: `clear: left/right` side mapping** (`pkg/layout/block_layout.go:169-180`)

The `GetClear()` method returns physical `ClearLeft`/`ClearRight`. The `ClearanceOffset()` method in `exclusion_space.go:98-121` matches `ClearLeft` against `FloatLeft` (which the exclusion space treats as inline-start). In horizontal-tb, `clear: left` correctly clears inline-start floats. But in vertical modes:

- `clear: left` should clear floats on the physical-left side
- In vertical-rl, physical-left is **block-end** (not inline-start)
- The current code would clear inline-start (physical-top) floats instead

The mapping from physical clear side → logical float side must account for writing mode.

**Issue 2: Orthogonal float sizing** (`pkg/layout/block_layout.go:680-694`)

When a float has a different writing mode from its parent (e.g., horizontal-tb float in vertical-lr parent), the constraint space builder handles the coordinate transform. But the float's margin-box dimensions at lines 700-702 are computed in the parent's logical coordinates:

```go
floatInlineSize := childMargins.InlineSum() + childLogical.InlineSize()
floatBlockSize := childMargins.BlockSum() + childLogical.BlockSize()
```

If the float is orthogonal, `childLogical` is already in the parent's coordinates (via `NewLogicalFragment(parentWDM, ...)`), which should be correct. However, the **available size** passed to the float's constraint space (line 685-686) may be incorrect — an orthogonal float needs its available inline-size to come from the parent's block-size, not inline-size.

**Issue 3: Clearance margin collapsing** (`pkg/layout/block_layout.go:169-180`)

The clearance test `clearance-calculations-vrl-002.xht` has `writing-mode: vertical-rl` with:
- Preceding sibling: `margin-left: 1em` (= block-end margin in vrl)
- Float: `float: left` (= inline-start in LTR)
- Clearing element: `clear: left; margin-right: 4em` (margin-right = block-start in vrl)

The clearing element's margin-right (block-start) should collapse with the preceding sibling's margin-left (block-end via margin collapse). After clearance, the margin strut is reset (line 177), but the spec says clearance is computed so that the element's border edge is placed at the float's block-end.

### What Blink Does

Blink resolves `clear: left/right` by mapping physical sides to logical in `ClearTypeForElement()`:
- `clear: left` → check if left is inline-start or inline-end based on writing mode + direction
- In horizontal-tb LTR: `clear: left` → clear inline-start floats ✓
- In vertical-rl LTR: `clear: left` → clear block-end floats (left is block-end in vrl)
- This is more nuanced than just mapping to `FloatLeft`

For orthogonal floats, Blink computes the float's available inline size from the parent's remaining block-size when the float is orthogonal.

### Fix Location

**File 1: `pkg/layout/block_layout.go`** (~lines 169-180, 663-738)

Create a helper that maps physical `ClearLeft`/`ClearRight` to the correct logical float side based on writing mode:

```go
// mapPhysicalClearToLogicalFloat maps CSS physical clear values to the
// logical float side they should clear, accounting for writing mode.
func mapPhysicalClearToLogicalFloat(clearType css.ClearType, wdm WritingDirectionMode) css.ClearType {
    if clearType == css.ClearBoth || clearType == css.ClearNone {
        return clearType
    }
    // In horizontal-tb: left=inline-start, right=inline-end (no change needed)
    // In vertical-rl: left=block-end, right=block-start → neither maps to inline floats directly
    // In vertical-lr: left=block-start, right=block-end → neither maps to inline floats directly
    // CSS 2.1 says clear:left clears left-floated boxes. float:left = inline-start.
    // So clear:left should match float:left (inline-start) regardless of writing mode.
    // ... BUT the clearance-calculations-vrl tests suggest otherwise.
    // Study the actual test expectations carefully before implementing.
}
```

**IMPORTANT:** Read ALL 4 clearance test files and their references before implementing. The correct mapping is not obvious from the spec alone — verify against test expectations.

**File 2: `pkg/layout/exclusion_space.go`** (~lines 95-121)

May need to be updated if the clearance side mapping changes.

### Milestones

1. **Milestone 1:** Read ALL 13 failing test HTML files and their reference files. Document what each test expects. Commit a summary of findings as a code comment. Run baseline tests.
2. **Milestone 2:** Fix the simplest float tests first (float-vlr-007, float-vrl-010). These test basic float positioning in vertical modes. Commit and run regression check.
3. **Milestone 3:** Fix the clearance tests (clearance-calculations-vrl-*). These require understanding margin collapse + clearance interaction in vertical modes. Commit and run regression check.
4. **Milestone 4:** Fix orthogonal float tests (float-lft-orthog-*). These require correct available-size computation for orthogonal floats. Commit and run regression check.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-vlr" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-vrl" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/float-lft-orthog" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/clearance-calculations" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Target 3: Background Position and Size in Vertical Writing Modes (~7 tests)

### Problem

Background image positioning and sizing does not account for writing mode at all. Per CSS Writing Modes §7.6, background properties use **purely physical** mappings — `background-position: left top` always means physical left/top regardless of writing mode. However, the document root element in vertical-rl mode starts content at the right edge (block-start = right in vrl), which affects how the background painting area is computed. The current code computes padding-box coordinates without considering that the root element's content area position depends on writing mode.

### Affected Tests (~7 failures)

| Test Pattern | Count | What It Tests |
|---|---|---|
| background-position-vrl-018/020/022 | 3 | Background position in vertical-rl root |
| background-size-document-root-vrl-002/004/006/008 | 4 | Background size on vertical-rl root |

### Root Cause (from code analysis)

**File: `pkg/render/render.go`** (lines 322-430)

The `drawBackgroundImage()` function computes the background origin from the physical padding box:

```go
// Lines 344-347:
paddingX := math.Round(box.X + box.Border.Left)
paddingY := math.Round(box.Y + box.Border.Top)
paddingW := math.Round(box.X+box.Width-box.Border.Right) - paddingX
paddingH := math.Round(box.Y+box.Height-box.Border.Bottom) - paddingY
```

This is correct for any writing mode — padding box is physical. But the issue is likely upstream: the **box dimensions themselves** may not be correct for the root element in vertical-rl mode. When the html root has `writing-mode: vertical-rl` and `width: auto`, the root element's width might not be computed correctly, causing the background to be positioned incorrectly.

**File: `pkg/render/paint_layer.go`** (lines 21-74)

The `PaintLayer` struct stores background properties but has no writing-mode context. It does have `IsVerticalText` (line 71) which could be extended for background calculations. However, since backgrounds are purely physical (§7.6), the real fix may not need writing-mode in the paint layer at all — it may be a layout issue with the root element's box dimensions.

**Possible root cause in layout:** The root element with `writing-mode: vertical-rl` and `width: auto` should have its width determined by the viewport height (since inline-size = height in vertical-rl, and width = block-size). If the layout engine doesn't compute this correctly, the Box.Width and Box.X values will be wrong, causing background misplacement.

### What Blink Does

Blink's `PaintBackground()` in `box_painter_base.cc`:
1. Gets the painting area (padding-box by default for `background-origin: padding-box`)
2. Computes the positioning area based on physical coordinates
3. For the root element: uses the initial containing block (ICB) as the painting area, which accounts for viewport dimensions correctly regardless of writing mode

The key difference: Blink handles the **root element as a special case** where the background painting area is the viewport/ICB, not the element's own padding box.

### Fix Location

**File: `pkg/render/render.go`** (lines 322-430)

**Step 1:** Check whether the issue is in `drawBackgroundImage()` or in the layout-computed box dimensions.

**Step 2:** If the box dimensions are wrong (likely for root element), the fix may be in how the root element's block-size is resolved in vertical-rl mode. Check `pkg/layout/engine.go` or wherever the root element's initial constraint space is set up.

**Step 3:** If the background positioning itself needs fixing, modify `drawBackgroundImage()` to handle the root element case — use viewport dimensions for the background painting area when the element is the root.

**File: `pkg/render/paint_layer.go`** (lines 89-217, `newPaintLayer()`)

May need to propagate a flag indicating whether this is the root element, so `drawBackgroundImage()` can use viewport dimensions.

### Milestones

1. **Milestone 1:** Read ALL 7 test HTML files and their reference images/HTML. Generate actual vs expected renders for 1-2 tests to visually identify the difference. Commit findings as comments.
2. **Milestone 2:** Diagnose whether the root cause is in layout (wrong box dimensions for root in vrl) or in rendering (wrong background computation). Add debug logging if needed. Commit diagnosis.
3. **Milestone 3:** Implement the fix. If it's a layout issue, fix root element sizing. If it's a render issue, fix `drawBackgroundImage()`. Commit and run regression check.

### Verification

```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-position-vrl" -count=1
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes/background-size-document-root-vrl" -count=1

# Full regression check
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/css-writing-modes" -count=1 2>&1 | grep -E "^    --- (PASS|FAIL)" | sort | uniq -c | sort -rn
```

---

## Independence Check

| | table_layout.go | block_layout.go | exclusion_space.go | render.go | paint_layer.go |
|---|---|---|---|---|---|
| Target 1 (Border-collapse) | **Yes** | - | - | - | - |
| Target 2 (Float/clearance) | - | **Yes** | **Yes** | - | - |
| Target 3 (Background) | - | - | - | **Yes** | **Yes** |

All three targets touch different source files and can be developed independently.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area. Key Blink files:
  - Target 1: `table_borders.cc` — border conflict resolution algorithm
  - Target 2: `block_layout_algorithm.cc` — float placement, clearance; `exclusion_space.cc`
  - Target 3: `box_painter_base.cc` — background painting; `view_fragmentation_context.cc` for root sizing
- **Commit and report at each milestone** (don't batch everything to the end). Each milestone above should result in at least one commit. Report the test count after each milestone commit so progress is visible.
- **Run the full writing-modes test suite** after each change to check for regressions. The baseline is 658 pass / 129 fail. Any regression below 658 passes must be investigated and resolved before proceeding.
- **Do NOT modify files outside your target's scope** — the three agents run in parallel.
- **Read test HTML files** to understand what each test expects before implementing fixes.
- When in doubt about logical/physical coordinate mapping, refer to `pkg/layout/writing_mode_converter.go` which has correct implementations for all writing modes.
