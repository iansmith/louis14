# Progress Log — css-position category

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and in auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not duplicate them here.

## Archived wm work
Phase 5f of the css-writing-modes effort is complete (commit `9913a9e4`, 2026-04-21). All 781 wm tests now PASS at 0 pixel diff. The full session history — phases 0 through 7 plus foundational Groups A/B/C — has been archived to:

- `docs/plan-wm.md`
- `docs/findings-wm.md`
- `docs/progress-wm.md`

Do not copy old wm content back into this file. If a wm regression is discovered during css-position work, link to the relevant archived section rather than duplicating.

## Current Phase
**Phase 0 complete** (baseline + groupings). **Phase 1 (G-TABLE-REL) next.**

## Test Results
| Date | Scope | Pass | Fail | NORUN | Notes |
|------|-------|------|------|-------|-------|
| 2026-04-21 | css-position (TestWPTCSS3Reftests) — baseline | 50 | 54 | 5 | Fresh run post-Phase 5f. Log: `output/baselines/css-position-2026-04-21.log`. |
| 2026-04-21 | css-writing-modes (invariant) | 781 | 0 | 0 | Phase 5f complete. |

## Invariants (must stay green)
| Category | Count | Last verified |
|---|---|---|
| css-writing-modes | 781/781 | 2026-04-21 (post-9913a9e4) |
| CSS2 (TestWPTReftests) | 99/99 | 2026-04-21 (pre-Phase-1 baseline implicit) |
| css-flexbox | ≥621/629 | 2026-04-20 (stale — re-verify after Phase 1) |

## Session: 2026-04-21

### Phase 0: Baseline & grouping — **DONE**
- Fresh css-position baseline: 50 PASS / 54 FAIL / 5 NORUN (no change from stale 2026-04-20 baseline — css-position was not improved by any of the §9 recovery / wm fixes, despite flex+position combined showing +5 earlier).
- Grouped 54 failures + 5 NORUN into 11 clusters by shared root-cause hypothesis (see `findings.md`).
- Largest cluster: **G-TABLE-REL (16 tests)** — `table_layout.go` has no relative-position branch; `block/flex/grid/inline_layout.go` all do. This is the cleanest single-root-cause unlock.
- Tracking docs restructured: wm content archived to `docs/plan-wm.md` / `docs/findings-wm.md` / `docs/progress-wm.md`; top-level `task_plan.md` / `findings.md` / `progress.md` now focus exclusively on css-position.

### Phase 1 preparation
Before writing any code: study Blink's table relative-offset application path. Checklist in `findings.md` "Blink study checklist". Do not start coding until that checklist is complete.

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | css-position category, Phase 0 done. Phase 1 (G-TABLE-REL, 16 tests) next. |
| Where am I going? | 104/104 css-position at 0 diff, then review delivery. |
| What's the goal? | All 104 css-position tests at 0 diff; wm 781/781 and CSS2 99/99 must hold. |
| What have I learned? | `table_layout.go` never applies `RelativeOffset`, while block/flex/grid/inline all do — this is why 16 `position-relative-table-*` tests fail with ~1% diff each. |
| What have I done? | Phase 5f (wm) complete. Fresh css-position baseline captured. Failures grouped into 11 clusters. Attack order set. Tracking docs restructured. |

## Error Log
*(populated as work progresses)*

---
*Update after each phase, after each milestone, or when a regression is discovered.*
