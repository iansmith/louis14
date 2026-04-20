# Progress Log — CSS2 reftests remaining 3

## Session: 2026-04-19

### Phase 0: Planning setup
- **Status:** complete
- **Started:** 2026-04-19
- Actions taken:
  - Confirmed 3 failures via `TestWPTReftests/floats` and `TestWPTReftests/linebox` runs
  - Added planning files to `.gitignore`
  - Drafted 3-phase plan (one phase per failure) + Phase 4 delivery
  - Seeded task_plan.md with Blink references and key questions
  - Seeded findings.md with test content and bug hypotheses from prior-session PNG inspection
- Files created/modified:
  - `.gitignore` (added planning-file entries)
  - `task_plan.md` (created)
  - `findings.md` (created)
  - `progress.md` (created)

### Phase 1: float-no-content-beside-001
- **Status:** complete
- **Started:** 2026-04-19
- **Completed:** 2026-04-19
- Actions taken:
  - Traced the push-down guard at inline_layout.go:527 — found its `(floatStart > 0 || floatEnd > 0)` clause is false on first encounter of a float because the float is still in `pendingFloats` (not yet in exclusion space)
  - Audited LineBreaker state (currentItemIndex, currentTextOffset, position, done) to confirm a safe rewind point
  - Verified `placeFloat` closure semantics and `ExclusionSpace.ClearanceOffset(ClearBoth)` return value
  - Implemented pre-push-down float commit + LineBreaker rewind after NextLine returns with line containing a pending-float followed by overflowing non-float content
- Files created/modified:
  - `pkg/layout/inline_layout.go` (inserted shortened-line-box push-down between line ~490 and line ~491)
  - `findings.md` (root cause + LineBreaker state notes)
  - `task_plan.md` (Phase 1 marked complete)
  - `progress.md` (this entry)

### Phase 2: inline-box-002
- **Status:** complete
- **Started:** 2026-04-19
- **Completed:** 2026-04-19
- Actions taken:
  - Traced fragment tree at paint time: anon-block wrapper (from maybeWrapAnonymousBlocks) AND inline text fragment (from inline_layout.go:1137-1143) both carried same RelativeOffset.Y = +192 from position:relative top:2in. engine.go:456-458 applied them in sequence → 2× offset (~384px observed).
  - Distinguished from the block-in-inline wrapper (expandInlineWithBlockChildren at layout_tree_builder.go:735-748) which is the SOLE site that shifts the block child (#div3) — kept unchanged.
  - Removed relpos propagation from maybeWrapAnonymousBlocks. Per CSS 2.1 §9.4.3 the flow position stays at normal-flow; the inline's own fragments handle the paint shift.
  - Verified inline-box-002 → 0 px diff.
  - Ran ~20 adjacent regression tests (floats, linebox, normal-flow, positioning, text, margin-padding-clear, stacking-context, colors) — all PASS.
- Files created/modified:
  - `pkg/layout/layout_tree_builder.go` (maybeWrapAnonymousBlocks lines 402-430)
  - `findings.md` (root cause + §9.4.3 rationale)
  - `task_plan.md` (Phase 2 marked complete, Current Phase → 3)
  - `progress.md` (this entry)

### Phase 3: empty-inline-002
- **Status:** complete
- **Started:** 2026-04-19
- **Completed:** 2026-04-19
- Actions taken:
  - Instrumented createLineBoxEx and layoutInlineChildren with debug prints to discover the code path. Found: div2 collects 3 inline items (whitespace text + span OpenTag + span CloseTag). NextLine returns ok with 2 results. `createLineBoxEx` is NEVER called for div2, so no span background fragment is produced.
  - Root cause: `lineHasOnlyOutOfFlow` at inline_layout.go:1670 treats only Text/AtomicInline/Control as in-flow — OpenTag/CloseTag are not considered. So an empty span with visible background/border fails the check and its line gets suppressed. The span bg fragment (sized correctly at 250×350 via spanBlockSize = blockOverhang+lineHeight+padEnd+borderEnd) never reaches the fragment tree.
  - Added `InlineItemOpenTag/InlineItemCloseTag` case in `lineHasOnlyOutOfFlow`: if the item's Style has any visible box decoration (background, border, padding, or margin) the line is preserved.
  - Added helper `hasVisibleInlineBoxDecoration` (wraps `hasVisibleInlinePaint` + checks padding/margin).
  - Verified empty-inline-002 → 0 px diff; full CSS2 suite 99/99 PASS; no regressions.
- Files created/modified:
  - `pkg/layout/inline_layout.go` (lineHasOnlyOutOfFlow OpenTag/CloseTag case + hasVisibleInlineBoxDecoration helper)
  - `task_plan.md` (Phase 3 marked complete, Current Phase → 4 delivery)
  - `progress.md` (this entry)

### Phase 4: Delivery
- **Status:** complete
- **Completed:** 2026-04-19
- Actions taken:
  - Confirmed 99/99 CSS2 reftests passing at 0 diff
  - All three phase commits landed on fix/flexbox-fast branch

### Phase 5: cascade.go pxValue fix
- **Status:** complete
- **Started:** 2026-04-19
- **Completed:** 2026-04-19
- Actions taken:
  - Identified bug: `applyPresentationalAttributes` in `pkg/css/cascade.go` concatenated "px" onto attribute values that already ended in "px" (e.g. `width="100px"` → CSS property `"100pxpx"`)
  - Added `pxValue(s string) string` helper (calls `strings.TrimSuffix(s, "px") + "px"`)
  - Applied at 6 sites: `width` attribute, `height` attribute, `border` on `<table>`, `border` on `<img>`, `cellspacing`→`border-spacing`, `cellpadding`→`padding`
  - Ran all 150 abs-pos VLR tests (`TestWPTCSS3Reftests/css-writing-modes/abs-pos`) — all pass (0 diff)
  - Only pre-existing failure is `abs-pos-border-offset-003.html` (unrelated, 0.9%, existed before this fix)
- Files created/modified:
  - `pkg/css/cascade.go` (added `pxValue` helper + 6 call sites)
  - Planning files updated

### Phase 6: Writing Modes remaining 13 failures
- **Status:** not started
- **Baseline:** 773/787 (98.2%) as of 2026-04-19
- 14 test failures identified; 1 excluded as untestable (`bidi-dynamic-iframe-001.html` — needs JS iframe)
- Font `sileot-webfont.woff` (Ezra SIL) downloaded to `testdata/wpt-css3/fonts/` and `testdata/wpt-css3/css-writing-modes/fonts/` — `block-plaintext-006.html` now loads the font but still fails at 1.0% (real rendering bug)
- Remaining 13 failures listed in task plan Phase 6, ordered largest-diff-first
- No code changes made yet

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
| float-no-content-beside-001 | pre-fix baseline | 0 diff | 17244 px (3.6%) | FAIL (pre-fix) |
| inline-box-002 | pre-fix baseline | 0 diff | 17244 px (3.6%) | FAIL (pre-fix) |
| empty-inline-002 | pre-fix baseline | 0 diff | 111939 px (23.3%) | FAIL (pre-fix) |
| float-no-content-beside-001 | post-Phase-1 | 0 diff | 0 diff | PASS |
| inline-box-002 | post-Phase-1 (no direct change) | 0 diff | 4556 px (0.9%) | FAIL — but improved from 17244 |
| inline-box-002 | post-Phase-2 | 0 diff | 0 diff | PASS |
| ~20 adjacent tests (floats/linebox/normal-flow/positioning/text/stacking-context/colors) | post-Phase-2 regression check | 0 diff | 0 diff | PASS |
| empty-inline-002 | post-Phase-3 | 0 diff | 0 diff | PASS |
| full CSS2 suite (99 tests) | post-Phase-3 | all pass | 99/99 pass | PASS |
| abs-pos VLR (150 tests) | post-Phase-5 pxValue fix | 0 diff | 0 diff | PASS |
| scrollbar-vertical-rl.html | Phase 6 baseline | 0 diff | 12.7% | FAIL |
| inline-block-alignment-007.xht | Phase 6 baseline | 0 diff | 8.4% | FAIL |
| img-intrinsic-size-contribution-001.html | Phase 6 baseline | 0 diff | 4.4% | FAIL |
| block-flow-direction-vrl-026.xht | Phase 6 baseline | 0 diff | 2.6% | FAIL |
| mongolian-orientation-001.html | Phase 6 baseline | 0 diff | 1.3% | FAIL |
| orthogonal-root-resize-icb-007.html | Phase 6 baseline | 0 diff | 1.1% | FAIL |
| block-plaintext-006.html | Phase 6 baseline (font now present) | 0 diff | 1.0% | FAIL |
| mongolian-orientation-002.html | Phase 6 baseline | 0 diff | 0.9% | FAIL |
| abs-pos-border-offset-003.html | Phase 6 baseline | 0 diff | 0.9% | FAIL |
| sideways-lr-main-axis.html | Phase 6 baseline | 0 diff | 0.6% | FAIL |
| img-intrinsic-size-contribution-002.html | Phase 6 baseline | 0 diff | 0.3% | FAIL |
| outline-inline-block-vrl-006.html | Phase 6 baseline | 0 diff | 0.1% | FAIL |
| baseline-with-orthogonal-flow-001.html | Phase 6 baseline | 0 diff | 0.1% | FAIL |
| empty-inline-002 | post-Phase-1 (no direct change) | 0 diff | 111939 px (23.3%) | FAIL |
| float-nowrap-3/4, float-root | Phase 1 regression check | 0 diff | 0 diff | PASS |
| border-padding-bleed-001, inline-box-001, inline-formatting-context-001/002 | Phase 1 regression check | 0 diff | 0 diff | PASS |
| normal-flow/*, positioning/* (11 tests) | Phase 1 regression check | 0 diff | 0 diff | PASS |
| float-nowrap-3 | regression check | 0 diff | 0 diff | PASS (baseline) |
| float-nowrap-4 | regression check | 0 diff | 0 diff | PASS (baseline) |
| float-root | regression check | 0 diff | 0 diff | PASS (baseline) |

## Error Log
| Timestamp | Error | Attempt | Resolution |
|-----------|-------|---------|------------|
|           |       | 1       |            |

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Phase 1 — about to study Blink's float/line-break ordering |
| Where am I going? | Phases 1→2→3→4 (delivery) |
| What's the goal? | 99/99 CSS2 reftests passing at 0% diff |
| What have I learned? | See findings.md — bug hypotheses for all 3 failures |
| What have I done? | Planning setup complete; no code changes yet |

---
*Update after completing each phase or encountering errors*
