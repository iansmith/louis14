# Task Plan: Pass the entire css-writing-modes category (wm)

## Goal
All 787 wm tests under `pkg/visualtest/testdata/wpt-css3/css-writing-modes/` pass at 0% diff via `TestWPTCSS3Reftests/css-writing-modes`. Baseline 674/787 passing → close the remaining 113 failures without regressing the 99/99 CSS2 suite.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Authoritative sources — re-read both at the start of any session before planning or coding. These are the non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline (only failing tests + regression-adjacent), operational rules (no `open`, commit before worktree agents, worktree commit scope).
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index pointing to the same rules as individual feedback memories (`feedback_foundational_correctness.md`, `feedback_blink_reference.md`, `feedback_all_tests_must_pass.md`, `feedback_study_blink_before_coding.md`, `feedback_no_open_commands.md`, `feedback_commit_before_agents.md`, `feedback_agent_checkpoints.md`).

If you find yourself about to type a rule verbatim into this file or into code comments, stop and link instead.

## Current Phase
Phase 5 (singletons). **Reality-check 2026-04-20:** multi-category baseline shows wm at 749 PASS / 32 FAIL (not the 771/16 previously estimated — drift unexplained). Phase 1/B2/iframe capability gaps all closed since the snapshot was taken; divergence likely comes from merge fallout that also produced the CSS2 crash (see Phase 7).

- `bidi-dynamic-iframe-001` — **FIXED** 2026-04-20 via merge `[pending SHA]` (Text.appendData + Text.data, `<iframe srcdoc>`, iframe.contentDocument). Regression spot-check icb-001..006 clean.
- 5a done (3 tests). 5b root cause identified, fix still pending.
- **URGENT:** CSS2 suite now crashes (nil pointer) at `generated-content/before-after-display-types-001.xht`. Panics through `block_layout.go:1330 → 422 → engine.go:160`. Introduced by one of the I1/I2/I3/I4 merges — the "CSS2 99/99 unaffected" claim was per-fix, never re-checked post-integration. Must be fixed before any further feature work (blocks Phase 6 delivery).

## Baseline snapshot (from Phase 0)
- 113 failures / 787 tests (85.6% passing before any wm-specific work)
- Largest cluster: **bidi × writing-modes (49 tests, 43%)** — not abs-pos as originally hypothesized
- Second cluster: **orthogonal / sizing (35 tests, 31%)**
- Abs-pos has only 3 failures total → absorbed into the singletons phase
- Full breakdown in `findings.md`

## Phases (rebalanced to match real distribution)

### Phase 0: Baseline & categorization — **DONE**
- [x] Full wm run via `TestWPTCSS3Reftests/css-writing-modes` (single sanctioned full-category run per CLAUDE.md §4)
- [x] Save raw pass/fail → `output/wm-baseline/raw.log`, `failing.txt`, `failing_with_diff.tsv`
- [x] Bucket by filename prefix, sort by % diff
- [x] Rebalance phase plan per actual distribution (this file)

### Phase 1: Bidi × writing-modes (49 tests, biggest cluster) — **DONE (47/49)**
Covers all `bidi-*` (39) + bidi-themed `block-{plaintext,embed,override,override-isolate}-*` (10).
- [x] Study bidi root causes; identify 3 independent bug classes
- [x] Fix 1: cross-span kerning merged items across bidi levels → scrambled LTR-shaped widths for Hebrew (commit `bbec3193`)
- [x] Fix 2: block-level `unicode-bidi: plaintext` injected FSI/PDI, mis-resolving P2/P3 paragraph direction; anonymous blocks didn't inherit `unicode-bidi` (commit `9a2f675e`)
- [x] Fix 3: same-bidi-level RTL runs also merged in cross-span kerning; ShapeAdvances is LTR-only, RTL clusters are descending → negative widths (commit `233a65de`)
- [x] CSS2 regression check: 99/99 unaffected at each fix
- **Remaining 2 (non-bidi root causes — parked until Phase 5):**
  - `bidi-dynamic-iframe-001` — iframe rendering capability gap, not a bidi text bug
  - `block-plaintext-006` — `white-space: pre` preservation bug inside plaintext block; parked in Phase 5 (singletons)
- **Status: DONE**

### Phase 2: Orthogonal / sizing — **DONE (already passing + float fix)**
- `available-size-*` (22) and `sizing-orthog-*` (8) were already passing at baseline — no work needed.
- `float-lft-orthog-*` (4): Fixed 2026-04-19 — `bfcInlineOrigin` not added to `floatStart` before BFC-absolute comparison in line layout (commit `994a6018`). All 4 now 0% diff.
- `orthogonal-root-resize-icb-007.html`: still failing (1.1%, 5400px) — moved to Phase 5.
- **Status: DONE**

### Phase 3: Floats in vertical modes — **N/A (no failures)**
No `float-vrl-*` or `float-vlr-*` failures in final baseline.

### Phase 4: Tables in vertical modes — **N/A (no failures)**
No table-related failures in final baseline.

### Phase 5: Singletons & small groups — **IN PROGRESS**
Started with 19 failures. **2026-04-20 re-scan: only 4 wm failures remain** —
`block-plaintext-004` (0.9%), `block-plaintext-006` (1.0%), `inline-block-alignment-007` (8.4%), `orthogonal-root-resize-icb-007` (1.1%).
All other Phase 5 targets now PASS at 0% diff (5a, 5b, 5d, and singletons: img-intrinsic-001/002, block-flow-direction-vrl-026, sideways-lr-main-axis, outline-inline-block-vrl-006, baseline-with-orthogonal-flow-001).

| Test | Diff | Group | Status |
|------|------|-------|--------|
| scrollbar-vertical-rl.html | 12.7% | scrollbar | **PARKED** (capability gap) |
| inline-block-alignment-007.xht | 8.4% | inline-block vertical alignment | open |
| img-intrinsic-size-contribution-001.html | 4.4% | img intrinsic size | open |
| block-flow-direction-vrl-026.xht | 2.6% | block-flow direction | open |
| mongolian-orientation-001.html | 1.3% | Mongolian orientation | open |
| orthogonal-root-resize-icb-007.html | 1.1% | orthogonal root resize | open |
| mongolian-orientation-002.html | 0.9% | Mongolian orientation | open |
| abs-pos-border-offset-003.html | 0.9% | abs-pos in VRL | open |
| block-plaintext-006.html | 0.8% | white-space:pre in plaintext block | **PARKED** |
| sideways-lr-main-axis.html | 0.6% | sideways-lr | open |
| img-intrinsic-size-contribution-002.html | 0.3% | img intrinsic size | open |
| bidi-dynamic-iframe-001.html | 0.3% | iframe | **FIXED** 2026-04-20 (Text.appendData + srcdoc + contentDocument) |
| logical-props-003.html | 0.1% | logical props col | **FIXED** (commit e639eca6) |
| logical-props-004.html | 0.1% | logical props col | **FIXED** (commit e639eca6) |
| logical-physical-mapping-001.html | 0.1% | logical/physical mapping | **FIXED** (commit e639eca6) |
| abs-pos-vlr-border-001.html | 0.1% | abs-pos in VLR | **IN PROGRESS** (root cause identified) |
| abs-pos-vlr-padding-001.html | 0.1% | abs-pos in VLR | open |
| outline-inline-block-vrl-006.html | 0.1% | outline in VRL | open |
| baseline-with-orthogonal-flow-001.html | 0.1% | orthogonal baseline | open |

Attack order: group by shared root cause, fix smallest/most isolated cluster first, work up to larger diffs.
- [x] Sub-phase 5a: logical-props (3 tests, 0.1% each) — logical border cascade contamination (commit `e639eca6`)
- [x] Sub-phase 5b: abs-pos VLR (3 tests) — already fixed by `d9d313c3` (`pxValue` strips existing `"px"` before appending). Verified 2026-04-20: all three tests at 0% diff. Progress doc was stale.
- [x] Sub-phase 5c: img-intrinsic (2 tests) — passing at 0% diff (incidental fix from cascade/intrinsic-sizing cleanup; commit trail via `7f138825`, `8dd8021f`).
- [x] Sub-phase 5d: mongolian-orientation (2 tests) — fixed via B2 style-level sideways resolution (commit `1dcffb34`).
- [x] Sub-phase 5e singleton sweep: `block-flow-direction-vrl-026`, `sideways-lr-main-axis`, `outline-inline-block-vrl-006`, `baseline-with-orthogonal-flow-001` all pass at 0% diff.
- [ ] **Sub-phase 5f: foundational-grouping finish (4 remaining tests, 3 root causes)** — see `findings.md` "Phase 5 Remaining — Foundational Grouping". Attack order is foundational-impact-first, not %-diff-first:
  1. **Group B — plaintext paragraph-level line flow (2 tests)**: `block-plaintext-004` (0.9%), `block-plaintext-006` (1.0%). Single root cause hypothesis: per-line paragraph level sourced wrong. Lowest regression risk, highest ratio (2 tests / 1 fix).
  2. **Group A — orthogonal-root ancestor walk (1 test)**: `orthogonal-root-resize-icb-007` (1.1%). Blink walks unconditionally past non-positioned ancestors to ICB; our inline-block path stops at grandparent. Likely unblocks css-position failures that share the same position-gate.
  3. **Group C — VLR + text-orientation:sideways baseline (1 test)**: `inline-block-alignment-007` (8.4%). Hardest; prior bulk-swap attempt was net -25 on wm. Save for last; dispatch as its own narrow-scope task targeting only the inline-block-baseline-export site.
- **Status: active — starting 5f, Group B first**

### Phase 6: Delivery
- [ ] Confirm all 787 wm tests pass at 0 diff
- [ ] Re-run CSS2 suite (99/99) to confirm no regression
- [ ] Final commit summary / report
- **Status:** pending (blocked on Phase 7)

### Phase 7: Integration regression audit (2026-04-20) — **COMPLETE**

Outcome: CSS2 99/99 restored (`2bc9076c`), wm 775/787 = +26 net (+4 above estimate) via revert `df19b64a` of I2 salvage. No flex/position regressions. Phase 7 B2 (Mongolian) dispatch now unblocked.

Original plan retained below for record.

---
Post-I1/I2/I3/I4 merge regressions surfaced by the multi-category baseline. Must be resolved before Phase 5b/6 or any new-category work. Two independent regressions (CSS2 crash, wm drift) tracked as 7a and 7b, with 7c as the combined verification gate.

Merge order on `fix/flexbox-fast` (chronological, all 4 land between Phase 0 baseline and 2026-04-20 baseline):
1. `2ef71c5f` — I1: cascade + parser (B1.1, B4.1, B4.2)
2. `489020db` — I3: constraint-space + OOF static position (B5, B3)
3. `6814437e` — I4: JS engine rAF + element onload, float max-content, table-row wrapping (B6)
4. `8700eb9c` — I2 salvage: B1.2 baseline swap, B1.3 sideways broadening for VLR

**7a — CSS2 nil-pointer panic (blocks any CSS2 run)**
- [ ] Read `tests/wpt-css2/generated-content/before-after-display-types-001.xht` (and its `-ref.xht`) to understand the DOM shape that reaches the deref.
- [ ] Identify the exact nil-deref site at `pkg/layout/block_layout.go:1330` (one step up from the stack's top frame); capture which pointer is nil and which call site passes it.
- [ ] Git bisect across the 4 merges above by running just this single test at each SHA; identify the offending merge. Use `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests/generated-content/before-after-display-types-001' -v`.
- [ ] Root-cause within the offending merge (look for new fields/types touched in that merge that the CSS2 path doesn't set). Favor upgrading callers rather than adding nil-guards in hot paths.
- [ ] Land fix on `fix/flexbox-fast` with a commit message referencing this plan and the bisect SHA.
- **Status:** not started.

**7b — WM pass-count drift (22 tests)**
- [ ] Diff `output/baselines/wm.log` failures (2026-04-20, 32 fails) against Phase 0's `output/wm-baseline/failing.txt` (original 16 fails + 8 targeted = 24 expected max post-5a). Compute the exact set of *new* failures post-integration.
- [ ] Bucket new failures by test-name prefix (bidi / orthogonal / abs-pos / sideways / text-orientation / ...).
- [ ] For each bucket, run one representative test at each of the 4 merge SHAs above to attribute regression to specific merge(s). Some buckets may blame I4's table-row wrapping or float max-content tweaks, others I3's OOF changes.
- [ ] Fix per bucket (may require separate commits per bucket, each with its own attribution).
- **Status:** not started. Must also verify the 3 logical-props tests that Phase 5a was supposed to fix are still passing (commit `e639eca6`); they may be part of the 22.

**7c — Verification gate (blocks Phase 5b/6/next-category)**
- [ ] Re-run full CSS2: expect 99/99 pass (restore pre-regression count).
- [ ] Re-run full css-writing-modes: expect ≥771/781 pass (Phase 0 baseline minus the 8 remaining targeted fails; actually more since 5a + Phase 8 should have added 4).
- [ ] Spot-check css-flexbox (expect 621/629 unchanged) and css-position (expect 50/104 unchanged or improved).
- [ ] Log final counts in `progress.md` and `docs/plan-wm-final-8-PROGRESS.md`.
- **Status:** blocked on 7a + 7b.

**Rule:** do not open 7b until 7a's crash is fixed — the panic aborts CSS2 mid-run, and if it originates in a shared code path it may also be causing some of the wm drift.

### Next-category candidates (out-of-phase context)
From the 2026-04-20 four-category baseline:
| Category | PASS / total-run | Headroom |
|---|---|---|
| css2 | 37 (crash) | crash blocks run — urgent |
| css-flexbox | 621 / 629 | 8 fails → near-green |
| css-writing-modes | 749 / 781 | 32 fails (this plan) |
| css-position | 50 / 104 | 54 fails → highest ROI after wm |

Raw logs: `output/baselines/{css2,wm,flex,css-position}.log`.

## Key Questions
1. Is the bidi-heavy cluster driven by one root cause (paragraph-direction under vertical writing mode) or by N independent bugs? Answer in Phase 1 research step.
2. Does fixing orthogonal available-size resolution also close the `float-lft-orthog-*` failures (shared root cause)?
3. Are there cross-phase bugs — e.g. a single axis-mapping bug surfacing in bidi, tables, and floats? Watch for this during Phase 1 research.

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Rebalanced from 9 phases to 6 | Real failure distribution is bidi-heavy and sizing-heavy; abs-pos (originally Phase 2) has only 3 failures — absorbed into singletons |
| Phase 0 mandatory before any code | Avoided fix-then-discover-bigger-bug churn; paid off — hypothesis that abs-pos was biggest was wrong |
| Full wm run ONCE in Phase 0 only | CLAUDE.md §4 allows baselines; afterwards only failing bucket + regression-adjacent |
| Do NOT copy project rules into this plan | They live in CLAUDE.md + memory; linked at top — changes there propagate automatically |
| Use `TestWPTCSS3Reftests` | `TestWPTReftests` scans wpt-css2 only; wm is under wpt-css3 |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| `TestWPTReftests/css-writing-modes` returned 0/99 | Wrong test function | Switched to `TestWPTCSS3Reftests` |
| Pixel-diff parser showed 0.0%/? | Bad anchoring across `=== RUN` lines | Track current test from RUN, attribute next REFTEST FAIL to it |

## Notes
- Test command template: `GOTOOLCHAIN=go1.25.5 /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/<subpath>' -v`
- Baseline artifacts live under `output/wm-baseline/`
- CSS2 state at kickoff: 99/99 passing (do not regress)
