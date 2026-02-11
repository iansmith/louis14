# Plan: Pass justify-content WPT Reftests

## Test Inventory (21 actionable tests added, 1 already passing)

### Tier 1: Horizontal (row) — 7 tests
All use `flex-direction: row` (default), XHTML format with `<?xml?>` PI.

| Test | What It Tests | Special Features |
|------|--------------|-----------------|
| horiz-001a | All 8 values: default, flex-start, flex-end, center, space-between, space-around, space-evenly, left, right | Core test — items with `flex: 0 Npx` |
| horiz-001b | Same as 001a + `min-width` on items | min-width interaction |
| horiz-002 | Same values + margin/border/padding on flex items | Box model interaction |
| horiz-003 | Items that overflow the container | Overflow alignment behavior |
| horiz-004 | Overflow + margin/border/padding | Combined edge case |
| horiz-005 | Auto-sized container (no explicit width) | Shrink-to-fit interaction |
| horiz-006 | `flex-direction: row-reverse` with all values | Reversed main axis; **ref uses `direction:rtl`** |

### Tier 2: Vertical (column) — 7 tests
All use `flex-direction: column`, XHTML format. Flex containers are `float: left` side-by-side.

| Test | What It Tests | Special Features |
|------|--------------|-----------------|
| vert-001a | All 8 values in column direction | Same structure as horiz-001a |
| vert-001b | Same + `min-height` | min-height interaction |
| vert-002 | + margin/border/padding | Box model interaction |
| vert-003 | Items overflow container height | Overflow behavior |
| vert-004 | Overflow + margin/border/padding | Combined edge case |
| vert-005 | Auto-sized container (no explicit height) | Shrink-to-fit |
| vert-006 | `flex-direction: column-reverse` | Reversed cross axis |

### Tier 3: Simple HTML tests — 7 tests

| Test | Status | Notes |
|------|--------|-------|
| css-box-justify-content | **ALREADY PASSING** | flex-end + text items + `&nbsp;` |
| justify-content-001 through 005 | **BLOCKED** | Use `linear-gradient` backgrounds (not supported) |
| space-between-003.tentative | Actionable | column-reverse + row-reverse + overflow:hidden |

### Not Downloaded (need writing-mode)
wmvert-001/002/003, sideways-001 — all require `writing-mode: vertical-rl/sideways-rl`

## Current Implementation Status

### Already Working
- **justify-content values** in `layout_flex.go` (lines 307-330):
  - `flex-start`, `flex-end`, `center` ✓
  - `space-between`, `space-around`, `space-evenly` ✓
- **Missing `left`/`right`** — not in `GetJustifyContent()` switch (style.go:1555). For LTR, `left` = `flex-start`, `right` = `flex-end`
- **CSS named colors** all present: lightgreen, pink, orange, lightblue, yellow, purple ✓
- **No `calc()` support** — reference files use `calc(40px / 6)` for space-around margins

## Blockers (ordered by priority)

### Blocker 1: XHTML Parsing — 14 tests blocked
Parser crashes on `<?xml version="1.0" encoding="UTF-8"?>` processing instruction.

**Fix:** Strip `<?xml ...?>` PI before parsing. Add to `RenderHTMLToFileWithBase` or the parser's preprocessing. Simple regex: `strings.TrimPrefix` or `regexp.ReplaceAll`.

**XHTML self-closing tags** (`<div class="a"/>`): In XHTML, this is an empty element. In HTML5, `<div/>` is treated as `<div>` (NOT void/self-closing). Our parser likely treats `<div/>` as `<div>` per HTML5 rules, meaning subsequent siblings would become children. **Must verify** — if broken, convert `<div .../>` → `<div ...></div>` during preprocessing.

### Blocker 2: `justify-content: left/right` — affects all horiz/vert 001a tests
Not handled in `GetJustifyContent()`. Simple fix:
```go
case "left":
    return JustifyContentFlexStart  // LTR
case "right":
    return JustifyContentFlexEnd    // LTR
```

### Blocker 3: `calc()` — affects reference rendering accuracy
Reference files use `calc(40px / 6)`, `calc(140px / 3)`. Without this, reference rendering will have wrong margins for space-around/space-evenly cases.

**Fix:** Add basic `calc()` support to `ParseLength` / `GetLength`. Only need: `calc(Npx / M)` and `calc(Npx * M)` patterns.

### Blocker 4: `column-reverse` — may not be implemented
Used by vert-006 and space-between-003. Check `GetFlexDirection()` for column-reverse support.

### Blocker 5: `min-width`/`min-height` on flex items — horiz-001b, vert-001b
CSS Flexbox spec says flex items have `min-width: auto` / `min-height: auto` by default, which prevents shrinking below content size. The 001b tests explicitly set min-width/min-height to override flex-basis.

## Implementation Plan

### Phase 1: Unblock XHTML (highest impact — unblocks 14 tests)
1. Add XHTML preprocessing: strip `<?xml ...?>` PI
2. Handle self-closing `<div/>` → convert to `<div></div>` if needed
3. Verify all 14 XHTML tests render without crashing
4. Can be done in the test runner (`reftest_runner_test.go`) or in `pkg/html/parser.go`

### Phase 2: Add missing justify-content values
1. Add `left` → `flex-start` and `right` → `flex-end` to `GetJustifyContent()` in `style.go`
2. Verify `column-reverse` exists in `GetFlexDirection()`

### Phase 3: Add basic calc() support
1. Detect `calc(...)` in length parsing
2. Support patterns: `calc(Npx / M)`, `calc(Npx * M)`, `calc(Npx + Mpx)`, `calc(Npx - Mpx)`
3. Only needed for reference files, but important for accuracy

### Phase 4: Run tests, fix flex layout bugs
1. Run all 14 XHTML tests, compare output to reference
2. Fix any justify-content positioning bugs
3. Fix flex item margin/border/padding handling if needed
4. Fix overflow alignment behavior if needed

### Phase 5: Edge cases
1. space-between-003: column-reverse + row-reverse + overflow:hidden
2. min-width/min-height interaction with flex-basis

## Expected Outcomes

| Category | Tests | Expected Pass | Notes |
|----------|-------|---------------|-------|
| Tier 1 horiz (basic) | 001a, 002, 003 | 3/3 | Core tests, should work after Phase 1-3 |
| Tier 1 horiz (features) | 001b, 004, 005 | 2-3/3 | min-width, auto-size may need work |
| Tier 1 horiz (reverse) | 006 | 0-1/1 | Ref uses `direction:rtl`, may not match |
| Tier 2 vert (basic) | 001a, 002, 003 | 2-3/3 | Column layout + justify |
| Tier 2 vert (features) | 001b, 004, 005 | 1-3/3 | Similar to horiz |
| Tier 2 vert (reverse) | 006 | 0-1/1 | Needs column-reverse |
| Tier 3 | space-between-003 | 0-1/1 | Complex edge case |
| Tier 3 | 001-005 | 0/5 | Blocked on linear-gradient |
| Tier 3 | css-box-justify-content | 1/1 | Already passing |
| **Total** | **21 actionable** | **~10-15** | |

## Prompt for Implementation Session

> Continue working on the CSS flexbox implementation. We've added 21 new WPT justify-content reftests to `pkg/visualtest/testdata/wpt-css3/css-flexbox/`. The current test run shows 37/59 CSS3 tests passing.
>
> Priority tasks:
> 1. Fix XHTML parsing — strip `<?xml?>` processing instructions before parsing. The 14 `.xhtml` test files all crash with "expected tag name at position 1". Also verify that XHTML self-closing `<div/>` syntax works correctly (should create empty elements, not nest siblings inside).
> 2. Add `justify-content: left` and `right` to `GetJustifyContent()` in `pkg/css/style.go` — map to flex-start/flex-end respectively.
> 3. Add basic `calc()` support to CSS length parsing — reference files use `calc(40px / 6)` patterns.
> 4. Run the tests and fix any flex layout bugs that emerge.
>
> Key files: `pkg/css/style.go`, `pkg/html/parser.go` (or tokenizer), `pkg/layout/layout_flex.go`, `pkg/visualtest/reftest_runner_test.go`
>
> Tests blocked on unimplemented features (skip these):
> - justify-content-001 through 005: need `linear-gradient` (not supported)
> - horiz-006 ref: uses `direction:rtl` (may not match)
> - Writing-mode tests: not downloaded, need `writing-mode: vertical-rl`
>
> Current baseline: 37/59 CSS3 (36 old passing + css-box-justify-content), 51/51 CSS 2.1.
