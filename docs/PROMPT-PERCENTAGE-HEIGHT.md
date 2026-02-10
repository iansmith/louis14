# Implement Percentage Height Resolution

## Context

The test suite stands at 35/51. The highest-error failing test is `height-percentage-003a.xht` at **90.2% pixel error** — essentially a total failure. A second test `height-percentage-004.xht` at **5.0% error** shares the same root cause.

### The Bug

**Percentage heights are not resolved.** The layout engine handles percentage widths correctly but completely ignores percentage heights.

In `pkg/layout/layout_block.go` lines 185-189, the height calculation only checks `style.GetLength("height")` (absolute lengths like `px`, `em`). There is no `style.GetPercentage("height")` check. Percentage values like `height: 100%` return `(0, false)` from `GetLength` and fall through to `contentHeight = 0` (auto).

Compare with the working width path at lines 136-143:
```go
} else if pct, ok := style.GetPercentage("width"); ok {
    cbWidth := availableWidth
    contentWidth = cbWidth * pct / 100
    hasExplicitWidth = true
}
```

### What the Test Expects

`height-percentage-003a.xht`:
```css
html  { background-color: red;   height: 100%; }
body, p { height: 100%; margin: 0px; }
p     { background-color: green; }
```

Chain: `html` gets 100% of viewport → `body` gets 100% of html → `p` gets 100% of body → entire page is green. Currently: `html` height = 0 (auto), red background fills 90% of viewport.

`height-percentage-004.xht`:
```css
#container { height: 100%; background: red }
#container div { position: absolute; height: inherit }
```

### CSS Spec (CSS 2.1 §10.5)

- Percentage heights resolve against the **containing block's height**
- For the root element (`html`), the containing block is the **initial containing block** (viewport)
- If the containing block's height is NOT explicitly set (i.e., depends on content), percentage heights are treated as `auto`
- Special case: `html` and `body` elements — `height: 100%` resolves against viewport

### Infrastructure Already Available

- `style.GetPercentage("height")` — exists and works (see `pkg/css/style.go:49`)
- `le.viewport.height` — viewport height available on layout engine
- The width percentage code at lines 136-143 is a working template

---

## Implementation Plan

### Step 1: Add percentage height resolution in `layout_block.go`

After line 186 (`} else if h, ok := style.GetLength("height"); ok {`), add a percentage height check:

```go
} else if hPct, ok := style.GetPercentage("height"); ok {
    // CSS 2.1 §10.5: Percentage heights resolve against containing block height
    // For the root element, the containing block is the initial containing block (viewport)
    cbHeight := 0.0
    if node.TagName == "html" || (node.Parent != nil && node.Parent.TagName == "") {
        // Root element: resolve against viewport
        cbHeight = le.viewport.height
    } else if parent != nil && parent.Height > 0 {
        // Non-root: resolve against parent's content height (if explicitly set)
        cbHeight = parent.Height - parent.Padding.Top - parent.Padding.Bottom - parent.Border.Top - parent.Border.Bottom
    }
    if cbHeight > 0 {
        contentHeight = cbHeight * hPct / 100
    }
    // else: containing block height depends on content → treat as auto (contentHeight = 0)
```

**Subtlety**: CSS spec says if the containing block's height is NOT explicitly set, percentage heights are treated as `auto`. You need to track whether a parent has an explicitly-set height vs auto height. The `parent.Height > 0` check is a rough proxy — a more correct approach might need a flag like `hasExplicitHeight`.

### Step 2: Handle `body` element chain

The `body` element's containing block is `html`. When `html` has `height: 100%` (resolved to viewport height), `body`'s `height: 100%` should resolve against that. This should work naturally if the layout processes html before body (which it does — parent before children), as long as `parent.Height` is correctly set by the time body is laid out.

### Step 3: Also add percentage resolution for `min-height` and `max-height`

Lines 203-213 currently only use `GetLength` for min/max height. Add `GetPercentage` checks there too.

### Step 4: Consider absolute positioning

`height-percentage-004.xht` uses `position: absolute` with `height: inherit`. The `inherit` value should pick up the parent's `height: 100%` string, then resolve it. Check that the CSS inheritance code handles this.

---

## Verification

```bash
# Primary test (should drop from 90.2% to ~0%)
go test ./pkg/visualtest -run "TestWPTReftests/visudet/height-percentage-003a" -v

# Secondary test
go test ./pkg/visualtest -run "TestWPTReftests/visudet/height-percentage-004" -v

# Regression check
go test ./pkg/visualtest -run "TestWPTReftests" -v 2>&1 | grep "Summary:"

# Unit tests
go test ./pkg/layout/... -v
```

**Success criteria**: height-percentage-003a passes (< 1% error), no regressions.

## Current Failing Tests (for reference)

| Test | Error | Likely Cause |
|------|-------|-------------|
| height-percentage-003a.xht | 90.2% | **Percentage height (this task)** |
| border-padding-bleed-001.xht | 13.0% | Inline border/padding painting across line boxes |
| anonymous-box-generation-001.xht | 12.3% | Anonymous box generation |
| float-no-content-beside-001.html | 11.2% | Float clearing/content beside floats |
| inline-box-002.xht | 7.8% | Inline box model |
| absolute-non-replaced-height-002.xht | 7.7% | Absolute positioning height calc |
| absolute-non-replaced-height-006.xht | 7.0% | Absolute positioning height calc |
| height-percentage-004.xht | 5.0% | **Percentage height (this task)** |
| margin-001.xht | 1.5% | Margin collapsing |
| inline-box-001.xht | 1.2% | Inline box model |
| anonymous-boxes-inheritance-001.xht | 1.2% | Anonymous box inheritance |
| height-applies-to-010a.xht | 0.7% | Height on list-item/li elements |
| inline-block-baseline-001.xht | 0.6% | Inline-block baseline alignment |
| inline-formatting-context-002.xht | 0.5% | Inline formatting context |
| absolute-non-replaced-height-003.xht | 0.4% | Absolute positioning height calc |
| block-in-inline-003.xht | 0.4% | Block-in-inline splitting |
