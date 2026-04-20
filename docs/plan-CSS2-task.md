# Task Plan: CSS2 fixes + Writing Modes remaining 13 failures

## Goal
Get the css-writing-modes WPT suite to 787/787 passing (currently 773/787, 98.2%). Thirteen tests remain at > 0% diff. CSS2 suite is already 99/99.

## Current Phase
Phase 6 — Writing Modes remaining 13 failures (none done)

## Phases

### Phase 1: Fix float-no-content-beside-001 (CSS 2.1 §9.5.2 shortened line push-down)
- [x] Re-read inline_layout.go:525-532 push-down branch
- [x] Root cause found: guard `(floatStart > 0 || floatEnd > 0)` is false on the first line because float hasn't been placed yet (pendingFloats[item] still set)
- [x] Write findings to findings.md
- [x] Implement: post-NextLine check for pending floats on line; if non-float content after the float overflows the shortened line, place float now, clear past it, rewind LineBreaker cursor past the float, `continue`
- [x] Verify float-no-content-beside-001 → 0 px diff (was 17244 px / 3.6%)
- [x] Verify 3 passing float tests + 16 adjacent linebox/normal-flow/positioning tests don't regress
- [x] BONUS: inline-box-002 diff reduced from 17244 → 4556 px (Phase 2 should be easier)
- **Status:** complete

### Phase 2: Fix inline-box-002 (position:relative on inline with block-in-inline child)
- [x] Trace relative-offset propagation through block-in-inline split in inline_layout.go + block_layout.go + render.go
- [x] Root cause found: DOUBLE offset — both maybeWrapAnonymousBlocks anon-wrapper AND inline text fragment carry the same relpos RelativeOffset, applied in sequence at paint (~2× top:2in ≈ 384px observed)
- [x] Implement: remove relpos propagation from maybeWrapAnonymousBlocks (layout_tree_builder.go:402-430). The inline's own fragments carry the offset; expandInlineWithBlockChildren wrapper still carries offset for the block child (that's the sole site).
- [x] Verify inline-box-002 → 0 px diff (was 17244 px / 3.6%)
- [x] Verify no regressions in ~20 adjacent tests (floats, linebox, normal-flow, positioning, text, margin-padding-clear, stacking-context, colors)
- **Status:** complete

### Phase 3: Fix empty-inline-002 (empty inline painting with padding/border/margin)
- [x] Traced the flow: line is created, NextLine returns ok but createLineBoxEx is never called — confirmed via debug print that `lineHasOnlyOutOfFlow` returns true and the line is suppressed.
- [x] Root cause: `lineHasOnlyOutOfFlow` only treated Text/AtomicInline/Control as "in-flow content". OpenTag/CloseTag with visible paint (background/border/padding/margin) still require the line box so the inline's box decorations render.
- [x] Fix: in `lineHasOnlyOutOfFlow` added an OpenTag/CloseTag case that consults a new `hasVisibleInlineBoxDecoration` (background, border, padding, or margin).
- [x] Verify empty-inline-002 → 0 px diff.
- [x] Verify full CSS2 suite: 99/99 pass.
- **Status:** complete

### Phase 4: Delivery
- [x] Confirm all 99 CSS2 reftests pass at 0 diff
- [x] Commit each phase's fix separately with clear message
- [x] Report final status to user
- **Status:** complete

### Phase 5: cascade.go `pxValue` fix (presentational attributes)
- [x] Identify bug: `applyPresentationalAttributes` appended "px" to attribute values that already contained "px" (e.g. `width="100px"` → CSS `"100pxpx"`)
- [x] Add `pxValue(s string) string` helper (strips existing "px" suffix before appending)
- [x] Apply at all 6 sites: `width`, `height`, table `border-width`, img `border-width`, `border-spacing`, `cellpadding`
- [x] Verify build compiles clean
- [x] Run all 150 abs-pos VLR tests (`TestWPTCSS3Reftests/css-writing-modes/abs-pos`) — all pass
- **Status:** complete

### Phase 6: Writing Modes — fix remaining 13 failures (current: 773/787)

Each sub-task below is one failing test. All require study-Blink-first per CLAUDE.md §2.
Test command: `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/<name>' -v`

#### Group A: Largest diffs first (most impactful)
- [ ] `scrollbar-vertical-rl.html` (12.7%) — scrollbar geometry in vertical-rl
- [ ] `inline-block-alignment-007.xht` (8.4%) — inline-block baseline alignment in vertical
- [ ] `img-intrinsic-size-contribution-001.html` (4.4%) — image intrinsic size contribution in orthogonal flow
- [ ] `block-flow-direction-vrl-026.xht` (2.6%) — block flow direction VRL edge case (Cat I from gap analysis)
- [ ] `mongolian-orientation-001.html` (1.3%) — Mongolian script vertical orientation
- [ ] `orthogonal-root-resize-icb-007.html` (1.1%) — last surviving ICB abspos case (Cat A from gap analysis)
- [ ] `block-plaintext-006.html` (1.0%) — unicode-bidi:plaintext with Ezra SIL font (font now present)
- [ ] `mongolian-orientation-002.html` (0.9%) — Mongolian script vertical orientation
- [ ] `abs-pos-border-offset-003.html` (0.9%) — abspos border offset in vertical
- [ ] `sideways-lr-main-axis.html` (0.6%) — sideways-lr flex main axis (Cat I from gap analysis)
- [ ] `img-intrinsic-size-contribution-002.html` (0.3%) — image intrinsic size contribution
- [ ] `outline-inline-block-vrl-006.html` (0.1%) — outline on inline-block in VRL
- [ ] `baseline-with-orthogonal-flow-001.html` (0.1%) — baseline sync with orthogonal child

**Status:** not started
**Excluded (untestable):** `bidi-dynamic-iframe-001.html` — requires JS iframe scripting not supported by the test runner.

## Key Questions
1. Why does the current push-down at inline_layout.go:527 fail — is it ordering (float placed after push-down shifts it) or does `line.Width > lineAvailableInline` not fire for indivisible single-word lines?
2. For block-in-inline: does our engine generate an anonymous block wrapper at all, or does the block child get positioned in the parent block's coordinate space directly?
3. For empty-inline painting: is the bug in layout (fragment size = 0) or render (painter uses line-box extent instead of inline border-box)?

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Attack failures in order #1 → #2 → #3 | Narrowest scope first; #1 has 90% scaffolding in place; #3 highest-diff but benefits from inline-fragment knowledge from #2 |
| Each failure gets its own commit | Makes regressions bisectable per CLAUDE.md §5 discipline |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
|       | 1       |            |

## Notes
- CLAUDE.md §1: foundational correctness — no point fixes
- CLAUDE.md §2: study Blink BEFORE writing code
- CLAUDE.md §3: 0% diff required, not "close enough"
- CLAUDE.md §4: run ONLY the failing tests (plus regression-adjacent), not the whole suite
- CSS2 test command: `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests/<subpath>' -v`
- WM test command: `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/<name>' -v`
- Full WM suite: `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes' -v`
