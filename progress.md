# Progress Log — wm category (css-writing-modes)

## Rules pointer
Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and in auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not duplicate them here.

## Session: 2026-04-19

### Phase 0: Baseline & categorization — **DONE**
- **Started / finished:** 2026-04-19
- Actions taken:
  - Counted wm test files on disk: 867 (drivers + refs)
  - Ran the single sanctioned full-category baseline: `TestWPTCSS3Reftests/css-writing-modes` (~57s)
    - Discovered `TestWPTReftests` scans wpt-css2 only → switched to `TestWPTCSS3Reftests`
  - Saved raw output to `output/wm-baseline/raw.log`
  - Wrote `failing.txt` (113 names) and `failing_with_diff.tsv` (sorted desc by % diff)
    - First parser attempt misassociated diffs; fixed by anchoring REFTEST FAIL to the most recent `=== RUN`
  - Bucketed 113 failures by filename prefix → 30+ buckets, collapsed into 5 super-clusters
  - Rebalanced phase plan: bidi (49) + orthog/sizing (35) are the real work; abs-pos only has 3 failures
- Files created:
  - `output/wm-baseline/raw.log`, `failing.txt`, `failing_with_diff.tsv`
  - Updated `findings.md` with bucket table, super-clusters, top-10 outliers, per-phase Blink entry points
  - Updated `task_plan.md` from 9 phases → 6 phases
- Key findings:
  - 674/787 pass before any work (85.6%) — much healthier than the 700+ feared
  - Bidi × writing-modes is 43% of failures; single root cause hypothesis worth investigating first in Phase 1
  - Highest-diff outlier is `scrollbar-vertical-rl.html` at 12.7% (singleton; Phase 5)
  - Most bidi/block-bidi failures are ≤ 0.2% diff → likely tight axis/paragraph-direction bugs, not large layout misses
- Next:
  - Phase 1: enumerate bidi bucket, study Blink's `InlineNode::SegmentBidiRuns` + `BidiParagraph`, look for shared root cause

### Phase 1: Bidi × writing-modes (49 tests) — IN PROGRESS
- **Started:** 2026-04-19
- Root cause 1: cross-span kerning merged across bidi levels (38/49 fixed)
  - `applyCrossSpanKerning` (pkg/layout/line_breaker.go:1427) re-shaped runs of adjacent text items as ONE concatenated string to capture cross-span HarfBuzz kern pairs. After `SplitItemsAtLevelBoundaries` splits items at bidi-level boundaries AND `ReorderLineVisual` reorders them, the run iterated visual-order items whose logical text concatenates into a scrambled mixed-direction string (e.g. "> aא >  >"). Shaping that under a single LTR direction gave cluster→x positions that bear no relation to the per-item byte ranges, so per-item widths got swapped between split runs.
  - Fix: add a `BidiLevel != BidiLevel` short-circuit at the top of `canMergeShapingContext`.
- Root cause 2: block-level plaintext/override bidi control injection (4/49 fixed)
  - `injectBlockBidiControls` (inline_item.go) had a "plaintext" case wrapping block content in FSI/PDI, causing `determineFSIDirection` to return 0 (skip isolate content) and mis-resolve paragraph direction.
  - Anonymous blocks created around inline content next to sibling blocks did not inherit parent's `unicode-bidi`, so mixed content lost its plaintext paragraph direction.
  - Fix: remove plaintext case from `injectBlockBidiControls`; propagate `unicode-bidi: plaintext/bidi-override/isolate-override` onto anonymous block styles in `layout_tree_builder.go`.
- Root cause 3: same-level RTL cross-span kerning (4/49 fixed — this session)
  - Even at a single bidi level, concatenating multiple RTL text items and passing to `ShapeAdvances` (LTR-only) produced scrambled/negative widths. HarfBuzz with RTL direction emits clusters in descending byte order, which `cum[]`'s ascending-cluster assumption cannot interpret.
  - Fix: `canMergeShapingContext` now also returns false when `BidiLevel % 2 == 1`. RTL items measure standalone; acceptable for Hebrew (no contextual shaping to lose).
- Bidi bucket status: 49 fail → 2 fail. CSS2 suite: unaffected.
- Remaining 2:
  - `bidi-dynamic-iframe-001` (iframe-specific, not a bidi bug)
  - `block-plaintext-006` (`white-space: pre` in plaintext block, 0.8% diff — separate whitespace issue, not bidi)
- Files touched: pkg/layout/line_breaker.go, pkg/layout/inline_item.go, pkg/layout/layout_tree_builder.go

### Phase 2: Orthogonal / sizing — **DONE**
- `available-size-022` and `available-size-023` (top-diff outliers at 4.6% and 3.0%): fixed with commit `d660e64f` — orthogonal available-size for scroller ancestors was not being propagated correctly.
- `float-lft-orthog-*` (4 tests): fixed with commit `994a6018` — `bfcInlineOrigin` was not being added to `floatStart` before BFC-absolute comparison in line layout; float was displaced by the wrong amount in non-BFC-establishing containers.
- Additional float work: commits `78857abd` (defer second float when committed text fills shortened line), `b6c787b3` (suppress background-image on text run boxes).
- Table column sizing: commit `91431291` (VLR/VRL table column sizing) and `390f332c` (col/colgroup border styles in border-collapse resolution).
- CSS2 suite: unaffected.
- `orthogonal-root-resize-icb-007.html` (1.1%) — still failing, moved to Phase 5.

### Phase 5a: Logical props / logical-physical mapping — **DONE**
- **Tests fixed:** `logical-props-003.html`, `logical-props-004.html`, `logical-physical-mapping-001.html` (all 0.1% diff)
- **Root cause:** Logical border properties (`border-block-start`, `border-inline-end`, etc.) were suffering cascade contamination — a physical border property set earlier in the cascade was leaking into the computed logical property value, causing incorrect border sizes in vertical writing modes.
- **Fix (commit `e639eca6`):** Fixed cascade contamination in logical border property resolution.
- CSS2 suite: unaffected.

### Phase 5b: Abs-pos VLR — **DONE** (verified 2026-04-20)
- **Tests fixed:** `abs-pos-vlr-border-001.html`, `abs-pos-vlr-padding-001.html`, `abs-pos-border-offset-003.html` — all at 0% diff.
- **Fix landed:** commit `d9d313c3` ("Fix double-px suffix in presentational attribute pixel values") — added `pxValue(s)` helper (`pkg/css/cascade.go:1323-1327`) that trims `"px"` before appending, so `width="100px"` cascades to CSS `width: 100px` instead of `100pxpx`. `applyPresentationalAttributes` now uses `pxValue` for width and height.
- **Verification (2026-04-20):** `GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/Cellar/go/1.26.2/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/(abs-pos-vlr-border-001|abs-pos-vlr-padding-001|abs-pos-border-offset-003)' -v` → 3/3 PASS at 0 pixel diff.
- **Lesson:** progress docs can drift from reality. Before declaring a fix "pending", re-run the targeted test — the fix may already exist from an adjacent commit. `d9d313c3` is adjacent to the logical-props fix `e639eca6` from 5a; the same session that fixed the cascade contamination also cleaned up the presentational-attribute px handling.

### Phase 5b ORIGINAL notes (pre-fix, retained for history)
- **Investigation status:** Root cause fully identified for `abs-pos-vlr-border-001.html` block 4 (broken `<img>`).
- **Root cause:**
  1. HTML `<img width="100px" height="40px">` — in `applyPresentationalAttributes` (`cascade.go:1348`), `val+"px"` with `val="100px"` produces `"100pxpx"` (double-px, invalid CSS).
  2. CSS length parser can't parse `"100pxpx"` → width remains unset.
  3. `getImgAttrFallbackInfo` (`intrinsic_sizing.go:202`) calls `strconv.ParseFloat("100px", 64)` → fails → returns empty `IntrinsicSizingInfo{}` (no intrinsic dimensions, no ratio).
  4. `ComputeReplacedIntrinsicInlineSize`: `hasExplicitBlock=true` (author CSS `height:40px`), no ratio, no intrinsic inline → `inlineSize = 300` (CSS 2.1 §10.3.2 default).
  5. Fragment width = 304px. Static position in VLR-RTL: `{BlockOffset=117, BlockEdge=End}`.
  6. `blockOffset = 117 - 304 = -187` → physical x = -183 (far off-screen left).
  7. Canvas (`<canvas width="100" height="40">`) works: `getCanvasIntrinsicInfo` defaults to 300×150 with `ratio=2.0` → `inlineSize = 40×2.0 = 80` → fragment width = 84px → `blockOffset = 33` → x = 37 (correct).
- **Fix needed:** In `applyPresentationalAttributes` (`cascade.go` ~line 1348): strip any existing `"px"` suffix before appending `"px"`, so `width="100px"` → CSS `width: 100px` (not `"100pxpx"`). Same fix needed for height. Likely also need to check if this alone fully resolves the position, or if static-position conversion also needs a fix.
- **No fix committed yet.**
- Files involved: `pkg/css/cascade.go:1341-1369`, `pkg/layout/intrinsic_sizing.go:162-228`

## Session: 2026-04-20 (continuation)

### iframe capability gap closed
- Dispatched sonnet-4.6 worktree agent `a8f2863d` for `bidi-dynamic-iframe-001` with JS-infra inventory (see `docs/plan-wm-final-8-FINDINGS.md` → "JS Test Infrastructure Inventory") as bootstrap.
- Agent landed 3 milestone commits: `c4906855` (Text.appendData + Text.data), `c04f8aa5` (iframe srcdoc), `83906cd3` (iframe.contentDocument via nested document proxy sharing outer `domContext.cache` for cross-document adoption).
- Merged to `fix/flexbox-fast` as merge commit (no-ff). Build clean, vet clean, target test PASS at 0% diff. Regression spot-check `orthogonal-root-resize-icb-001..006` all PASS.
- **Surprise:** the iframe path runs through `BlockLayoutAlgorithm.tryLayoutNestedDocument` (block_layout.go:1274), not `ReplacedLayoutAlgorithm` as the dispatch prompt assumed. Both sites got updated for correctness.

### Four-category baseline (2026-04-20)
Sanctioned multi-category run. Raw logs in `output/baselines/`.
| Category | PASS | FAIL | Panic | Notes |
|---|---|---|---|---|
| css2 (TestWPTReftests) | 37 | 0 | **1** | Crash at `generated-content/before-after-display-types-001.xht`. Regression from I1–I4 merges — plan's "99/99 unaffected" was per-fix, never post-integration. |
| css-flexbox | 621 | 8 | 0 | Near-green (98.7%). |
| css-writing-modes | 749 | 32 | 0 | Plan claimed 771/16 — actual is **749/32**. 16 extra failures unaccounted for; likely merge fallout. |
| css-position | 50 | 54 | 0 | 45% passing — biggest headroom of the four. |

### icb-007 diagnosis (addendum)
Captured in `docs/plan-wm-final-8-FINDINGS.md`. Inline-block orthogonal root inside a non-positioned ancestor does not walk to the ICB for available-inline-size; abspos chain does. Points at `position`-shaped holes in containing-block helpers — same shape of bug as I3's B3 `IsOrthogonalTo`-vs-full-conversion issue.

## Test Results
| Test / bucket | Input | Expected | Actual | Status |
|---------------|-------|----------|--------|--------|
| CSS2 full (kickoff baseline) | all 99 | 0 diff | 99/99 pass | PASS |
| wm full (Phase 0 baseline) | 787 | n/a | 674/787 pass, 113 fail | BASELINE |
| wm (post-Phase1+2+5a, plan estimate) | estimated | 0 diff | ~771/787 pass, 16 fail | OPTIMISTIC |
| wm (2026-04-20 measured baseline) | 781 run | 0 diff | 749 pass, 32 fail | ACTUAL |
| css2 (2026-04-20 measured baseline) | 37+ | 0 diff | panic at display-types-001 | REGRESSION |
| css-flexbox (2026-04-20 baseline) | 629 | 0 diff | 621 pass, 8 fail | 98.7% |
| css-position (2026-04-20 baseline) | 104 run | 0 diff | 50 pass, 54 fail | 45% |
| bidi-dynamic-iframe-001 (post-merge) | 1 | 0 diff | PASS | FIXED |

## Error Log
| Timestamp | Error | Attempt | Resolution |
|-----------|-------|---------|------------|
| 2026-04-19 | `TestWPTReftests/css-writing-modes` returned 0/99 | Ran wrong test function | Switched to `TestWPTCSS3Reftests` |
| 2026-04-19 | Pixel-diff parser wrote 0.0%/? for all entries | Broken anchoring across `=== RUN` lines | Reset on each RUN; attribute next REFTEST FAIL to current test |

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Phase 7 (new) — integration regression audit, now expanded into 7a (CSS2 panic) / 7b (wm drift) / 7c (verification). Diagnosis not yet started. Phase 5b root cause identified, blocked on 7. iframe capability gap closed. |
| Where am I going? | 7a CSS2 panic → 7b wm 22-test drift → 7c full-suite verification → 5b (abs-pos VLR) → 5c/5d/5e → 6 (delivery). After wm is green: css-position (45% passing — biggest headroom). |
| What's the goal? | All 787 wm tests at 0 diff; CSS2 back to 99/99. |
| What have I learned? | wm actual baseline is 749/32, not the estimated 771/16. css-position sits at 50/104 — highest-ROI next category. The iframe agent fix validated that the JS-infra inventory approach unblocks capability-gap dispatches cleanly. Phase 7 is now structured with merge-bisect methodology + hypothesis ranking per bucket in findings.md. |
| What have I done? | Phases 1+2+5a complete; iframe capability gap closed via merge; multi-category baseline run; icb-007 diagnosed but not fixed; CSS2 regression discovered; Phase 7 structured into 7a/7b/7c across root + tracked plan docs. |

## Session log entry — 2026-04-20 (Phase 7 plan expansion)
- Expanded Phase 7 in `task_plan.md`, `findings.md`, `progress.md` (root) and `docs/plan-wm-final-8-{TASK,FINDINGS,PROGRESS}.md` (tracked).
- Added: merge timeline (4 SHAs), 7a/7b/7c subtask structure, hypothesis ranking per bucket, sequencing rule (7a before 7b), bisect command template.
- Next: commit tracked-file updates, then begin 7a diagnosis (read the failing test, identify nil-deref site, bisect).

---
*Update after completing each phase or encountering errors*

## Session log entry — 2026-04-20 (Phase 7 execution — COMPLETE)

**9a — CSS2 panic fix (`2bc9076c`)**
- Panic was at `fragment_builder.go:124` (not `block_layout.go:1330` as plan estimated — that was a middle frame in the stack).
- Culprit: `92728908` (in I4 merge) added anonymous-row wrapping in `collectRowsAndCaptions` but left `tableRow.node` nil.
- Fix: construct real `*LayoutInputNode` with `NewAnonymousTableRowStyle` for both anonymous-row branches.
- CSS2 restored to 99/99.

**9b — WM drift fix (revert `df19b64a`)**
- 25 new failures, 24 of which were pure `writing-mode: sideways-lr` tests, 2 were `vertical-lr`+`text-orientation:sideways`.
- Bucket attribution matched hypothesis #3 (I2 salvage) exactly.
- I2 salvage's B1.2 swap was net 0 fixes + 25 regressions (its target `inline-block-alignment-007` still failed). Reverted entirely.
- WM 749 → 775 (+26). 775 is 4 above plan's estimate of 771.

**9c verification (all green)**
| Category | Pass | Target | Result |
|---|---|---|---|
| CSS2 | 99/99 | 99/99 | ✓ |
| wm | 775/787 | ≥771 | ✓ +4 |
| css-flexbox | 621/629 | 621/629 | ✓ |
| css-position | 50/104 | ≥50 | ✓ |

**Remaining wm failures (6, all pre-existing Phase 0 targets):** inline-block-alignment-007 (B1), block-plaintext-004 (not yet bucketed), block-plaintext-006 (B4 parked), mongolian-orientation-001/002 (B2), orthogonal-root-resize-icb-007 (singleton, diagnosed).

**Phase 7 B2 dispatch note:** B1.2's "swap ascent/descent for all SLR strut/text" is wrong; Blink's `LogicalBoxFragment::BaselineMetrics` likely swaps only for inline-block baseline export. Fresh B2 agent must model this precisely.

Next options: 7 B2 Mongolian dispatch, 5b abs-pos VLR, or pivot to css-position (54-test headroom).

## Session log entry — 2026-04-20 (B2 Mongolian + Phase 5 re-scan)

### B2 Mongolian orientation (DONE — commits `1dcffb34`, `44f2cd10`)
- Approach: style-level convergence in `collectTextNode`. For text runs consisting entirely of vertical-script characters (Mongolian, Phags-Pa, Mongolian Supplement), rewrite `text-orientation: mixed|upright` to `sideways` on a cloned parent style. Downstream measurement, baseline selection, and painting then unify on the existing sideways path (horizontal advance via `MeasureText`).
- Supporting changes: new `IsVerticalScriptCharacter(r rune)` helper in `pkg/text/orientation.go`; `isAllVerticalScript(content)` guard in inline_item.go; `lineIsSidewaysResolved(results)` post-check in `inline_layout.go` so `UsesCentralBaselineWithStyle` flips to alphabetic baseline when every text item on the line resolved to sideways.
- Verification: mongolian-orientation-001 and 002 PASS at 0 pixel diff. Regression checks: 11/11 text-orientation PASS, 6/7 inline-block-alignment (007 still fails at 8.4%, pre-existing), css-flexbox 621/629 unchanged.
- B2.2 (per-character orientation segmentation à la Blink `OrientationIterator`) and B2.3 (per-rune em-square vs horizontal advance in `MeasureTextVerticalFromFont`) assessed after B2 landed. Both declined — no failing test exercises them (pure-Mongolian runs already route through `sideways`; mixed Latin+vertical-script is untested in WPT wm); per-rune render changes were attempted once and reverted for baseline drift. Deferral recorded in `docs/plan-wm-final-8-PROGRESS.md` (commit `9997fc25`).

### Phase 5 re-scan (sweep of remaining candidates)
- Verified via direct `-run` targeting of every open Phase 5 test: only 4 wm failures remain — `inline-block-alignment-007.xht` (8.4%), `orthogonal-root-resize-icb-007.html` (1.1%), `block-plaintext-006.html` (1.0%), `block-plaintext-004.html` (0.9%).
- Phase 5b tests verified passing (commit `d9d313c3` already landed the `pxValue` fix).
- Other previously-failing singletons verified passing: `img-intrinsic-size-contribution-001/002`, `block-flow-direction-vrl-026`, `sideways-lr-main-axis`, `outline-inline-block-vrl-006`, `baseline-with-orthogonal-flow-001`.
- Plan update committed as `66490949`.

### Foundational grouping of remaining 4 (see `findings.md` "Phase 5 Remaining — Foundational Grouping")
- **Group A — orthogonal-root ancestor walk:** icb-007 (1.1%). Blink walks unconditionally (WebFetch confirmed: no position gate); our inline-block orthogonal-root path likely gets grandparent's 10px instead of ICB. Unblocks `position`-gated holes that also likely affect css-position.
- **Group B — `unicode-bidi: plaintext` paragraph-level flow to line layout:** block-plaintext-004 (0.9%) + block-plaintext-006 (1.0%). Both tests have their targeted fixes landed (B4.2, blank-line strut, NBSP trimming, leading-`\n`-in-`<pre>` comment guard, per-paragraph P2/P3 in `ResolveBidiLevelsPlaintext`). Remaining delta is line-level: paragraph level likely sourced from the first item or block-global value, not per-line from each line's items. Single fix expected to close both tests.
- **Group C — VLR + `text-orientation: sideways` baseline:** inline-block-alignment-007 (8.4%). Prior I2 salvage bulk swap was wrong (net -25 wm); per the post-mortem, Blink's `LogicalBoxFragment::BaselineMetrics` swaps ascent/descent only for inline-block baseline export, not for strut/text metrics inside the child. Any future attempt must narrow scope to the export site.

### Attack order (foundational correctness, not % diff)
1. Group B — two tests, one root cause, low regression risk.
2. Group A — one test, but unlocks containing-block / ancestor-walk class of bugs.
3. Group C — hardest, needs focused Blink study; save for last.
