# Task Plan: Pass the entire css-position category

## Goal
All 104 tests under `pkg/visualtest/testdata/wpt-css3/css-position/` pass at 0% diff via `TestWPTCSS3Reftests/css-position`. Baseline (2026-04-21): **50 passing, 54 failing, 5 no-run**. Current (2026-04-21 post OOF re-entrance): **62 passing, 42 failing**. Remaining: close 42 without regressing:

- css-writing-modes (currently 781/781 PASS — Phase 5f complete)
- CSS2 (99/99 PASS)
- css-flexbox (626/629 PASS — verified post OOF re-entrance)

## Rules & Discipline
Authoritative sources (re-read at session start):

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index.

If you find yourself about to type a project rule into this file, stop and link instead.

## Archived wm work
The css-writing-modes category is complete. Its planning/findings/progress have been moved into `docs/`:
- `docs/plan-wm.md` — full 787-test plan (Phases 0–7, foundational groupings A/B/C, all done)
- `docs/findings-wm.md` — bucket analysis, Blink entry points, post-mortems
- `docs/progress-wm.md` — session-by-session log through Group C fix

Do not duplicate wm notes here.

## Baseline snapshot (2026-04-21)
Log: `output/baselines/css-position-2026-04-21.log`
- 104 tests exercised: **50 PASS · 54 FAIL · 5 NORUN** at baseline.
- Latest (post OOF re-entrance, commit `ed16475f`): **62 PASS · 42 FAIL** in this category.
- Failing test list + diffs: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`).

Highest-diff outliers (top 5 by pixel count, current state):
| % | px | test | group |
|---|---|------|-------|
| 10.4% | 50000 | `containing-block-change-scrollframe.html` | G-SCROLL (was G-CB-CHANGE) |
|  4.2% | 20000 | `containing-block-change-button.html` | G-SINGLETONS (was G-CB-CHANGE) |
|  4.2% | 20000 | `hypothetical-dynamic-change-003.html` | G-HYPO |
|  3.4% | 16308 | `sticky-top-001.html` | G-STICKY |
|  1.0% |  4672 | `position-fixed-scroll-nested-fixed.html` | G-FIXED residual (paint-clip / scrollTop) |

5 NORUN — **triaged 2026-04-21** (full table in `findings.md`):
- 4 are runner **SKIPs** ("no usable reference files found") — infrastructure gaps, not layout bugs:
  - `hypothetical-box-scroll-parent` (ref file missing from snapshot)
  - `hypothetical-box-scroll-viewport` (missing `window.scrollTo` + ref)
  - `position-absolute-multicol-001` (absolute-path ref unresolved by runner)
  - `replaced-object-backdrop` (`<object popover>` JS unsupported + absolute-path ref)
- 1 is a real **FAIL** miscounted as NORUN because the parser-error log format doesn't match our regex: `position-change.html` — HTML parser bails with `tokenizer error: expected '>' but reached EOF`. Counted as a real failure (moves to G-SINGLETONS).

**Target revision.** True runnable set = 100 tests; true failure count = 55 (54 original + position-change). The 4 SKIPs need harness / JS-engine work and are **out of scope for this plan**. Deliverable: **100/100 runnable at 0 diff** (104 total if the SKIPs are later un-skipped by separate infra work).

## Groups (root-cause-oriented, not %-diff-oriented)
Full detail in `findings.md`. 54 failing tests cluster into **11 groups** by likely shared root cause. Fix one representative test per group; if the root cause is correct, siblings fall out for free.

| # | Group | Count | % range | Estimated shape of root cause |
|---|---|---|---|---|
| 1 | ~~**G-TABLE-REL**~~ — position:relative on table-internal elements (thead/tbody/tfoot/tr/td) — **DONE** | 11 (primary) | 0.4–1.7% | Closed by commits `d174049b`, `ac2dc780`, `b6ec7d3f`. 8 `-absolute-child` variants moved to G-ABS-IN-INLINE / G-ABS-IN-TABLE. |
| 2 | **G-ABS-CENTER** — abspos centering with `margin: auto` + both-axis insets (`css-align-3` abspos sizing) | 5 | 0.3–2.1% | Abspos available-space = 2 × distance(center→closest edge); auto-margin distribution |
| 3 | **G-CB-CHANGE** — dynamic change of containing-block establishment (JS toggle of overflow/button/height) | 3 | 0.5–10.4% | Abspos children need re-resolve to new CB after JS mutation |
| 4 | **G-DYN-STATIC** — dynamic static-position re-layout (JS-triggered property flips affect float/inline/table-cell static pos) | 6 | 0.3–2.1% | Static position rectangle recomputation on relayout |
| 5 | **G-HYPO** — hypothetical position dynamic change + scroll (fixed/abs ancestor moves) | 3+2 NORUN | 2.1–4.2% | `HypotheticalBoxPosition` not recomputed when ancestor offset changes |
| 6 | **G-ROOT-FLEX-GRID** — `<html>` as position:fixed/absolute root with `display: flex|grid` | 4 | 0.8% | Root-element OOF sizing — insets must resolve against ICB even when `display` is flex/grid |
| 7 | **G-FIXED** — nested OOF re-entrance + scroll-clip escape for fixed | 2 (1 closed) | 0.5–4.2% | OOF resolver wasn't re-entrant. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` 2026-04-21. Residual on `position-fixed-scroll-nested-fixed` is paint-clip / scrollTop, not layout. |
| 8 | **G-ABS-IN-INLINE** — abspos whose containing block is an inline (CSS2 §10.1.4) | 2 | 2.3–2.9% | Inline-CB bounding box computation for abspos children |
| 9 | **G-STICKY** — `position: sticky` at scroll=0 must stay in normal flow | 1 | 3.4% | We treat sticky as relative (applies `top:10px` unconditionally); needs scroll-aware algorithm |
| 10 | **G-REPLACED** — abspos replaced elements with no intrinsic size / `max-content` sizing | 1 | 2.1% | CSS 2.2 §10.3.7 / §10.6.5 abs-replaced-width/height |
| 11 | **G-SINGLETONS** — `clear-001` (96px), `position-absolute-dynamic-list-marker` (18px), `stack-floats-001`, `position-absolute-iframe-print-001/002`, `position-relative-011/012/013` (%-top on table rows), plus 3 NORUN (`position-change`, `replaced-object-backdrop`, `position-absolute-multicol-001`) | 11 | 0.0–1.7% | Heterogeneous; attack last |

## Attack order (foundational impact ÷ effort)

Research insights from Blink study (2026-04-21) reshape the ordering — **G-DYN-STATIC is a prerequisite for both G-ABS-CENTER and G-HYPO**, and the IMCB machinery is shared between G-ABS-CENTER and G-HYPO.

1. ~~**G-TABLE-REL (11 primary tests).**~~ **Done 2026-04-21** — commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Relative offset moved into shared `BoxFragmentBuilder.AddChild`; positioned thead/tbody/tfoot emit section fragments; inline-block §10.8.1 last-baseline fallback corrected.
2. **G-CB-CHANGE (3 tests) — invalidation-only.** Add the style-change path: `StyleDifference::NeedsPositionedLayout` + `RemovePositionedObjects(stay_within)` so abspos children re-register with their new CB. No sizing changes.
3. **G-DYN-STATIC (6 tests) — foundational.** Rebuild static position every layout pass via an `OutOfFlowPositionedDescendants` list on `LayoutResult`. Drop any existing static-position caching. Required before IMCB can be exercised with dynamic inputs.
4. **G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests).** Both depend on `ComputeUnclampedIMCBInOneAxis` / `ResizeIMCBInOneAxis` in a new `pkg/layout/absolute_utils.go`. The hypothetical-box tests *are* the both-insets-auto branch — they may pass for free once IMCB lands. Verify after the IMCB commit and split if needed.
5. **G-ROOT-FLEX-GRID + G-FIXED (5 tests).** Blink research **deferred** to phase start — study `layout_view.cc` root-element specials + nested-fixed scroll offset at that point.
6. **G-ABS-IN-INLINE (2 tests).** New `pkg/layout/inline_containing_block.go` mirroring `InlineContainingBlockUtils::ComputeInlineContainerGeometry` — union rects of first + last line-boxes.
7. **G-STICKY (1 test).** Minimum viable: at layout, sticky boxes get zero offset; `sticky-top-001` naturally passes. Full `StickyPositionScrollingConstraints` can wait until a scroll-based sticky test appears.
8. **G-REPLACED (1 test).** Blink research **deferred** to phase start — CSS 2.2 §10.3.7 / §10.6.5 abs-replaced sizing.
9. **G-SINGLETONS (11 tests, includes `position-change`).** Sweep last; some (e.g. `position-relative-011/012/013`) are expected to close when G-TABLE-REL lands.

Each group runs through the same discipline loop (CLAUDE.md §1–§4):
1. **Study Blink.** Read the relevant Blink file (entry points in `findings.md`). No code before this step.
2. **Pick a representative failing test.** Read test + ref HTML; identify the DOM/CSS invariant being exercised.
3. **Instrument.** Use `/tmp/scanimg.go`-style pixel tools when visual delta is ambiguous; stderr-log layout metrics when geometric.
4. **Write the fix.** Narrow as possible. Mirror Blink's type names and algorithm order.
5. **Verify target + regression-adjacent.** Run only the failing group + 1–2 adjacent groups (CLAUDE.md §4).
6. **Regression sweep before commit.** wm full + CSS2 full + flex spot-check.

## Phases

### Phase 0: Baseline — **DONE 2026-04-21**
- [x] Fresh baseline run: `output/baselines/css-position-2026-04-21.log`
- [x] Parse into failing list: `/tmp/css-position-fails.tsv`
- [x] Group by root cause: 11 groups, see `findings.md`
- [x] Triage the 5 NORUN entries → 4 SKIP (infra), 1 real FAIL (`position-change.html`). Details in `findings.md` "NORUN triage".
- [x] Blink research for 7 groups (G-TABLE-REL, G-CB-CHANGE, G-DYN-STATIC, G-ABS-CENTER, G-HYPO, G-STICKY, G-ABS-IN-INLINE) — see `findings.md` per-group "Blink entry points" sections.

### Phase 1: G-TABLE-REL (11 primary tests) — **DONE 2026-04-21**
- [x] Blink research: relative offset is applied in `BoxFragmentBuilder::AddChild` via `ComputeRelativeOffsetForBoxFragment`. Fragment-builder-level, not algorithm-level.
- [x] Decision (2026-04-21, user-directed): do what Blink does — push the check into our shared `BoxFragmentBuilder.AddChild`. Parent's content-box size is the CB for percentage resolution.
- [x] Scope narrowed after isolated test run (2026-04-21): `td-top` / `td-left` diffs were a *baseline* bug, not a RelativeOffset bug. The green cell box was already shifted correctly; residual was a ~4px text-offset below the table.
- [x] Part A — RelativeOffset at shared `AddChild`. Committed `d174049b`. Removed duplicate tail blocks in block/flex/grid/inline layout algorithms.
- [x] Part B — emit section fragments (thead/tbody/tfoot) in `table_layout.go` so positioned sections have a fragment to attach to. Committed `ac2dc780`.
- [x] §10.8.1 fix — inline-block last-baseline. Committed `b6ec7d3f`. Two edits: stop synthesizing table LastBaseline at content-box block-end when no cell has a text baseline, and stop propagating `childResult.Baseline` as `LastBaseline` in block_layout. Per Blink's `LayoutBox::LastBaselineForInlineBlock`, a block has a last-baseline only if a line-box descendant provides one; otherwise the inline-block's §10.8.1 bottom-margin-edge fallback fires at atomic-inline placement.
- [x] Regression sweeps held: wm 781/781, CSS2 99/99.
- [x] Result: all 11 primary `position-relative-table-*` tests pass at 0 px diff. 8 `-absolute-child` variants remain out of scope (tracked under G-ABS-IN-INLINE / G-ABS-IN-TABLE).

### Phase 2: G-CB-CHANGE (3 tests) — **plan invalidated 2026-04-21; group dissolved**
- [x] Blink research: `StyleDifference::NeedsPositionedLayout` + `LayoutBlock::RemovePositionedObjects(stay_within)`.
- [x] Audit (2026-04-21): our `pkg/visualtest/helpers.go:85-102` already runs `engine2 := layout.NewLayoutEngine(...)` from-scratch on the post-JS DOM. There is no caching to invalidate. JS mutations land correctly (`fixed.style.height = "300px"` → inline-style attr `"height: 300px"`, pass-2 sees it). The Blink invalidation pattern doesn't apply.
- [x] Per-test triage (see `findings.md` "G-CB-CHANGE — Phase 2 audit invalidated"):
  - `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5%) → **G-FIXED** (positioned-fragment box-tree gap; pos:fixed/absolute boxes absent from box tree).
  - `containing-block-change-button` (4.2%) → **G-SINGLETONS** (`<button>` vertical-centering rendering bug).
  - `containing-block-change-scrollframe` (10.4%) → new **G-SCROLL** sub-group (needs `Element.scrollTop` setter + `overflow:hidden` scroll paint).
- [x] Phase 2 closed as a no-op — no code changes needed for "invalidation". Move on to next phase per revised attack order.

### Phase 3: G-DYN-STATIC (6 tests) — **per-FC computation fixes; original rebuild-list hypothesis invalidated 2026-04-21**
- [x] Blink research: static position NOT cached; rebuilt each pass via `LayoutResult::OutOfFlowPositionedDescendants` list.
- [x] Audit (2026-04-21): we already rebuild every pass via fresh `engine2`. Original "add OutOfFlowPositionedDescendants list" hypothesis is a no-op. Real root causes are per-FC COMPUTATION bugs in static-position capture sites. See `findings.md` "G-DYN-STATIC — Phase 3 hypothesis invalidated".
- [x] **(a) `inline_layout.go:682-694`** — split by child's `display`. Block-level abspos → `(0, lineBlockEnd)` when in-flow content precedes on the line, `(0, blockOffset)` otherwise; inline-level abspos → `(inlinePos, blockOffset)`. Helper `isInlineLevelDisplay` mirrors Blink's `ComputedStyle::IsOriginalDisplayInlineType`. `hasInflowOnLine` flag mirrors `line_box_.LineBoxBlockEnd()` at time-of-encounter so the first-child-block-level case (no prior in-flow) stays at `blockOffset`. Closes `inline` (2.1% → 0%). wm 781/781 ✓, CSS2 99/99 ✓.
- [ ] **(b) `block_layout.go:217-237`** — for inline-level abspos children, peek at exclusion space at `blockCursor` and use the float-aware inline-start as `InlineOffset`. Fixes `floats-001` and likely `floats-002/003`.
- [ ] **(c) `table_layout.go`** (abspos-in-table-cell capture site) — apply vertical-align to static-position block-offset. Fixes `table-cell` test.
- [ ] **(d) RTL-direction awareness** on capture (inline-edge annotation + flip). Fixes `floats-004`.
- [ ] Per-site commits with wm 781/781 + CSS2 99/99 regression gate after each.
- [ ] Representative drivers: `inline` (2.1%) for (a); `floats-001` (0.7%) for (b); `table-cell` (2.1%) for (c); `floats-004` (0.7%) for (d).

### Phase 4: G-ABS-CENTER + G-HYPO combined (5 + 3 = 8 tests)
- [x] Blink research: `absolute_utils.cc` IMCB machinery. G-HYPO is the both-insets-auto branch.
- [ ] New `pkg/layout/absolute_utils.go` with `InsetModifiedContainingBlock`, `ComputeUnclampedIMCBInOneAxis`, `ResizeIMCBInOneAxis`, `ComputeOofInlineDimensions`, `ComputeOofBlockDimensions`.
- [ ] Route existing OOF sizing through the new module.
- [ ] Representatives: `position-absolute-center-001` + `hypothetical-dynamic-change-001`.
- [ ] If hypothetical tests pass without additional work, mark G-HYPO closed.
- [ ] Regression + commit.

### Phase 5: G-ROOT-FLEX-GRID + G-FIXED (5 tests, 1 closed)
- [x] **G-FIXED Part A — OOF resolver re-entrance.** `OutOfFlowLayoutPart.LayoutCandidates` was dropping `childResult.PropagatedOOFCandidates`. Mirrored Blink's `OutOfFlowLayoutPart::LayoutOOFNodes` worklist pattern. Returns unresolved fixed candidates to caller; new `resolvesFixed` flag selects ICB / transform-or-containment-CB sites that absorb fixed. Updated all 7 call sites. Closes `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (0.5% → 0%); reduces `position-fixed-scroll-nested-fixed` (4.2% → 1.0%). Residual diff is paint-time scroll/clipping (fixed escaping `overflow:auto`), not layout — defer.
- [ ] **Blink research (deferred from Phase 0):** `layout_view.cc` root-element OOF sizing for the 4 G-ROOT-FLEX-GRID tests (still 0.8% each).
- [ ] G-FIXED residual: scroll-clip escape for fixed inside `overflow:auto` scrollable, plus `Element.scrollTop` JS setter (overlaps G-SCROLL).
- [ ] Implement + verify.
- [ ] Regression + commit.

### Phase 6: G-ABS-IN-INLINE (2 tests)
- [x] Blink research: `inline_containing_block_utils.cc` — union of first + last line-box fragment rects.
- [ ] New `pkg/layout/inline_containing_block.go` with `computeInlineContainerGeometry`.
- [ ] Wire into OOF pass for abspos children whose CB resolves to an inline.
- [ ] Regression + commit.

### Phase 7: G-STICKY (1 test) — minimum viable
- [x] Blink research: `sticky_position_scrolling_constraints.h` + `ComputeStickyPositionConstraints`; scroll-time offset, not layout-time.
- [ ] Short-circuit: for `position: sticky` at layout, emit zero `RelativeOffset` when the natural flow satisfies the threshold (at scroll=0 this is always true for sticky-top-001).
- [ ] Note: full `StickyPositionScrollingConstraints` deferred until scroll-based sticky tests appear.
- [ ] Regression + commit.

### Phase 8: G-REPLACED (1 test)
- [ ] **Blink research (deferred from Phase 0):** CSS 2.2 §10.3.7 / §10.6.5 abs-replaced width/height with `max-content`.
- [ ] Implement + verify.
- [ ] Regression + commit.

### Phase 9: G-SINGLETONS (11 tests, includes `position-change`)
- [ ] Per-test triage; many may already be closed by earlier phases.
- [ ] `position-change.html` — fix HTML parser to not bail on `expected '>' but reached EOF`.

### Phase 10: Delivery
- [ ] Confirm 100/100 runnable at 0 diff (104 total if SKIPs are later un-skipped by separate infra work).
- [ ] Confirm wm 781/781, CSS2 99/99, flex unchanged.
- [ ] Final session log summary.

## Milestones (commit + report after each)
Counts are against **runnable tests (100)**; 4 SKIPs excluded.

- **M1:** G-TABLE-REL closed → +11 primary (50 → 61). **Achieved 2026-04-21** via commits `d174049b`, `ac2dc780`, `b6ec7d3f`. Verified at re-baseline post OOF re-entrance: also closed `position-relative-012` (was conjectured). 8 `-absolute-child` variants still failing at 1.0% — distinct root cause, deferred to G-ABS-IN-INLINE / G-ABS-IN-TABLE.
- **M2:** ~~G-CB-CHANGE~~ — group dissolved 2026-04-21. Tests reassigned to G-FIXED / G-SINGLETONS / G-SCROLL.
- **M3:** G-DYN-STATIC closed → +6 (→ ~68).
- **M4:** G-ABS-CENTER + G-HYPO combined (IMCB) → +8 (→ ~76).
- **M5a:** G-FIXED Part A — OOF resolver re-entrance. **Achieved 2026-04-21** via commit `ed16475f`. Closed `absolute-pos-box-inside-fixed-pos-box-with-changing-height` (62 PASS total). Reduced `position-fixed-scroll-nested-fixed` 4.2% → 1.0% (residual paint-clip).
- **M5b:** G-ROOT-FLEX-GRID closed → +4 (→ ~80). G-FIXED Part B (paint-clip / scrollTop) overlaps G-SCROLL.
- **M6:** G-ABS-IN-INLINE closed → +2 (→ ~82).
- **M7:** G-STICKY + G-REPLACED closed → +2 (→ ~84).
- **M8:** G-SINGLETONS (including `position-change`) + G-SCROLL swept → 100/100 runnable.

## Test command templates
```
# Single test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position/<name>' -v

# Whole category (sanctioned baseline; don't rerun casually)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position' -v 2>&1 | tee output/baselines/css-position-<date>.log

# Regression sweeps
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'     # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests'                           # CSS2: expect 99/99
```

## Key Questions
1. ~~Does `table_layout.go` emit intermediate fragments?~~ **Answered (2026-04-21):** yes, it emits per-row and per-cell fragments at `table_layout.go:685` and `:735`. The fix targets those add-sites (or better, the shared `AddChild` below them, matching Blink's `BoxFragmentBuilder::AddChild`).
2. Are the 6 `dynamic-static-position-*` failures all driven by static-position caching, or do the `inline`/`table-cell`/`floats` branches each need separate plumbing? Blink evidence says "all driven by the same cached-vs-rebuilt static position." Confirm with Phase 3 representative.
3. **G-STICKY scope:** minimum viable is zero offset at scroll=0 — sufficient for `sticky-top-001`. Full `StickyPositionScrollingConstraints` is deferred until scroll-based tests appear.
4. **New:** is the hypothetical-box algorithm already folded into the IMCB both-auto-insets branch, or does it need separate plumbing? Blink evidence says it is the same branch. If Phase 4 makes the hypothetical tests pass for free, G-HYPO collapses into G-ABS-CENTER.

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Attack G-TABLE-REL first | 16 tests, one likely root cause, cleanest unlock. Foundational correctness §1. |
| Prefer pushing relative-offset check into shared `AddChild` | Mirrors Blink's `BoxFragmentBuilder::AddChild`; prevents the same class of bug recurring in future layouts. |
| NORUN — 4 SKIPs out of scope, 1 real FAIL folded into G-SINGLETONS | Triage 2026-04-21: SKIPs are harness/JS gaps; target is 100/100 runnable. |
| Bundle G-ABS-CENTER + G-HYPO into one phase | Both use the same Blink IMCB machinery; the hypothetical-box algorithm IS the both-insets-auto branch. |
| G-DYN-STATIC precedes G-ABS-CENTER/G-HYPO | The IMCB reads `LogicalStaticPosition`; without rebuild-per-pass, dynamic inputs won't flow through. |
| G-CB-CHANGE is invalidation-only → group dissolved | Audit 2026-04-21: harness already does fresh re-layout post-JS, so Blink's invalidation pattern is a no-op for us. Tests reassigned by actual root cause. |
| G-FIXED OOF re-entrance via worklist loop, not single-pass | Mirrors Blink's `OutOfFlowLayoutPart::LayoutOOFNodes`. New `resolvesFixed` flag distinguishes ICB / containment / transform CB sites (absorb fixed) from ordinary positioned sites (return unresolved fixed to caller). |
| Split G-FIXED into Part A (re-entrance, layout) and Part B (paint-clip / scrollTop) | Re-entrance closed 1 of 2 cleanly; the residual is squarely in paint/scroll, not OOF layout. Don't conflate — Part B will be picked up alongside G-SCROLL. |
| G-STICKY minimum viable acceptable | Full constraint machinery is overkill for the one failing test; flag as tech debt for when scroll-based sticky tests appear. |
| Do not run the full css-position category more than once per milestone | CLAUDE.md §4 — broad runs only at baselines and milestone verifications. |
| css-writing-modes stays at 781/781 as an invariant | Phase 5f is complete; any regression in wm reverts the commit. |

## Notes
- `output/baselines/` holds raw logs; parse scripts live in `/tmp/` and are regenerated per session.
- Current branch: `fix/flexbox-fast`. Master is the delivery target.
