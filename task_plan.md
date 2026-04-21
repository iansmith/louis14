# Task Plan: Pass the entire css-position category

## Goal
All 104 tests under `pkg/visualtest/testdata/wpt-css3/css-position/` pass at 0% diff via `TestWPTCSS3Reftests/css-position`. Baseline (2026-04-21): **50 passing, 54 failing, 5 no-run** → close 59 tests without regressing:

- css-writing-modes (currently 781/781 PASS — Phase 5f complete)
- CSS2 (99/99 PASS)
- css-flexbox (~621/629 PASS at last measure; verify before major changes)

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
- 104 tests exercised: **50 PASS · 54 FAIL · 5 NORUN**
- Pass rate 48% — lowest of the four categories in the 2026-04-20 multi-baseline.
- Failing test list + diffs: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`).

Highest-diff outliers (top 5 by pixel count):
| % | px | test | group |
|---|---|------|-------|
| 10.4% | 50000 | `containing-block-change-scrollframe.html` | G-CB-CHANGE |
|  4.2% | 20000 | `containing-block-change-button.html` | G-CB-CHANGE |
|  4.2% | 20000 | `position-fixed-scroll-nested-fixed.html` | G-FIXED |
|  4.2% | 20000 | `hypothetical-dynamic-change-003.html` | G-HYPO |
|  3.4% | 16308 | `sticky-top-001.html` | G-STICKY |

5 NORUN (tests that execute but emit no PASS/FAIL line — harness quirk or timeout):
`hypothetical-box-scroll-parent`, `hypothetical-box-scroll-viewport`, `position-absolute-multicol-001`, `position-change`, `replaced-object-backdrop`. Triage each in Phase 0.

## Groups (root-cause-oriented, not %-diff-oriented)
Full detail in `findings.md`. 54 failing tests cluster into **11 groups** by likely shared root cause. Fix one representative test per group; if the root cause is correct, siblings fall out for free.

| # | Group | Count | % range | Estimated shape of root cause |
|---|---|---|---|---|
| 1 | **G-TABLE-REL** — position:relative on table-internal elements (thead/tbody/tfoot/tr/td) | 16 | 0.4–1.7% | `table_layout.go` never applies `RelativeOffset` — all 16 likely one commit |
| 2 | **G-ABS-CENTER** — abspos centering with `margin: auto` + both-axis insets (`css-align-3` abspos sizing) | 5 | 0.3–2.1% | Abspos available-space = 2 × distance(center→closest edge); auto-margin distribution |
| 3 | **G-CB-CHANGE** — dynamic change of containing-block establishment (JS toggle of overflow/button/height) | 3 | 0.5–10.4% | Abspos children need re-resolve to new CB after JS mutation |
| 4 | **G-DYN-STATIC** — dynamic static-position re-layout (JS-triggered property flips affect float/inline/table-cell static pos) | 6 | 0.3–2.1% | Static position rectangle recomputation on relayout |
| 5 | **G-HYPO** — hypothetical position dynamic change + scroll (fixed/abs ancestor moves) | 3+2 NORUN | 2.1–4.2% | `HypotheticalBoxPosition` not recomputed when ancestor offset changes |
| 6 | **G-ROOT-FLEX-GRID** — `<html>` as position:fixed/absolute root with `display: flex|grid` | 4 | 0.8% | Root-element OOF sizing — insets must resolve against ICB even when `display` is flex/grid |
| 7 | **G-FIXED** — nested `position: fixed` inside a scrolling fixed container | 1 | 4.2% | Scroll offset propagation through fixed-nested-fixed |
| 8 | **G-ABS-IN-INLINE** — abspos whose containing block is an inline (CSS2 §10.1.4) | 2 | 2.3–2.9% | Inline-CB bounding box computation for abspos children |
| 9 | **G-STICKY** — `position: sticky` at scroll=0 must stay in normal flow | 1 | 3.4% | We treat sticky as relative (applies `top:10px` unconditionally); needs scroll-aware algorithm |
| 10 | **G-REPLACED** — abspos replaced elements with no intrinsic size / `max-content` sizing | 1 | 2.1% | CSS 2.2 §10.3.7 / §10.6.5 abs-replaced-width/height |
| 11 | **G-SINGLETONS** — `clear-001` (96px), `position-absolute-dynamic-list-marker` (18px), `stack-floats-001`, `position-absolute-iframe-print-001/002`, `position-relative-011/012/013` (%-top on table rows), plus 3 NORUN (`position-change`, `replaced-object-backdrop`, `position-absolute-multicol-001`) | 11 | 0.0–1.7% | Heterogeneous; attack last |

## Attack order (foundational impact ÷ effort)

1. **G-TABLE-REL (16 tests, ~one fix).** `table_layout.go` has no `RelativeOffset` path. This is the biggest single-root-cause cluster and the cleanest unlock. **Start here.**
2. **G-DYN-STATIC + G-CB-CHANGE (9 tests).** Both classes hinge on recomputing abspos children when JS mutates a property. Likely share a common missing-invalidation path. Tackle together after #1.
3. **G-ABS-CENTER (5 tests).** CSS-align-3 abspos sizing — well-specified, bounded scope, no JS-driven state.
4. **G-HYPO (5 tests).** Hypothetical-box recomputation when ancestor moves. Includes the two NORUN `hypothetical-box-scroll-*` tests — triage whether those are harness issues or real.
5. **G-ROOT-FLEX-GRID + G-FIXED (5 tests).** Root-element OOF + nested fixed. May share scrollable-fixed CB handling.
6. **G-ABS-IN-INLINE (2 tests).** §10.1.4 inline CB for abspos.
7. **G-STICKY (1 test).** Minimum viable sticky: at scroll=0, no offset.
8. **G-REPLACED (1 test).** Abs-replaced sizing for `max-content`.
9. **G-SINGLETONS (11 tests).** Sweep last — `clear-001` and `dynamic-list-marker` may already be within fuzz tolerances and need only careful inspection.

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
- [x] Triage the 5 NORUN entries (classify as real failures vs harness artifacts) → see `findings.md` "NORUN triage"

### Phase 1: G-TABLE-REL (16 tests) — **NEXT**
- [ ] Study Blink `layout_table.cc`, `layout_table_row.cc`, `layout_table_cell.cc` for `ComputeRelativeOffset` application.
- [ ] Identify where `table_layout.go` terminates children (fragment emission) and add a `RelativeOffset` step mirroring `block_layout.go:928-939`.
- [ ] Run one test per sub-shape (`thead-top`, `tbody-left`, `tr-top`, `td-top`, `*-absolute-child`, `position-relative-001/002`).
- [ ] Regression: full wm + CSS2 + flex spot-check.
- [ ] Commit: "Phase 1: relative positioning on table-internal elements".

### Phase 2: G-DYN-STATIC + G-CB-CHANGE (9 tests)
- [ ] Study Blink `out_of_flow_layout_part.cc` + `LayoutInvalidation` for how abspos children re-resolve when constraints/CB mutate.
- [ ] Representative: `containing-block-change-scrollframe` (10.4%) — triggers abspos re-layout after JS adds overflow.
- [ ] Regression + commit.

### Phase 3: G-ABS-CENTER (5 tests)
- [ ] Study Blink `ng_absolute_utils.cc` `ComputeOutOfFlowInsetSize` + auto-margin distribution.
- [ ] Representative: `position-absolute-center-001`.
- [ ] Regression + commit.

### Phase 4: G-HYPO (3 fails + 2 NORUN)
- [ ] Study Blink `HypotheticalBoxPosition` (needs exact file name from Blink search).
- [ ] Representative: `hypothetical-dynamic-change-001`.
- [ ] Decide whether `hypothetical-box-scroll-*` NORUN are harness or real failures.

### Phase 5: G-ROOT-FLEX-GRID + G-FIXED (5 tests)
### Phase 6: G-ABS-IN-INLINE (2 tests)
### Phase 7: G-STICKY (1 test)
### Phase 8: G-REPLACED (1 test)
### Phase 9: G-SINGLETONS (11 tests)
### Phase 10: Delivery
- [ ] Confirm 104/104 at 0 diff.
- [ ] Confirm wm 781/781, CSS2 99/99, flex unchanged.
- [ ] Final session log summary.

## Milestones (commit + report after each)
- **M1:** G-TABLE-REL closed → expect +16 tests (50 → 66 pass).
- **M2:** G-DYN-STATIC + G-CB-CHANGE closed → expect +9 tests (66 → 75).
- **M3:** G-ABS-CENTER closed → +5 (75 → 80).
- **M4:** G-HYPO closed → +3–5 (80 → 85).
- **M5:** G-ROOT-FLEX-GRID + G-FIXED closed → +5 (85 → 90).
- **M6:** G-ABS-IN-INLINE closed → +2 (90 → 92).
- **M7:** G-STICKY + G-REPLACED closed → +2 (92 → 94).
- **M8:** G-SINGLETONS swept → +10 (94 → 104 = all pass).

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
1. Does `table_layout.go` emit intermediate fragments that would receive `RelativeOffset`, or does it fold rows/cells into the table's single fragment? (If the latter, the fix touches fragment construction, not the terminal commit.) Answer in Phase 1.
2. Are the 6 `dynamic-static-position-*` failures all driven by the same missing invalidation point, or do the `inline`/`table-cell`/`floats` branches each need separate plumbing? Answer in Phase 2 representative study.
3. Is our `position: sticky` currently defined (fallback to relative is the visible behavior); does closing G-STICKY require implementing scroll-aware sticky or just gating the offset on scroll delta? Likely the latter for `sticky-top-001` specifically.

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Attack G-TABLE-REL first | 16 tests, one likely root cause, cleanest unlock. Foundational correctness §1. |
| NORUN tests logged as failures until proven harness | Cannot silently drop them — §3 ("all tests must pass") includes these. |
| Do not run the full css-position category more than once per milestone | CLAUDE.md §4 — broad runs only at baselines and milestone verifications. |
| css-writing-modes stays at 781/781 as an invariant | Phase 5f is complete; any regression in wm reverts the commit. |

## Notes
- `output/baselines/` holds raw logs; parse scripts live in `/tmp/` and are regenerated per session.
- Current branch: `fix/flexbox-fast`. Master is the delivery target.
