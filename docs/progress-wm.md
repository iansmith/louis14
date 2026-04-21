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
| Where am I? | Phase 5f COMPLETE 2026-04-21. All 781 wm tests PASS at 0 diff. CSS2 99/99 preserved. css-flexbox+css-position improved 671→676. Phase 6 delivery next. |
| Where am I going? | Phase 6 delivery (write-up) → pivot to css-position (53-test remaining headroom) or css-flexbox (singleton mop-up). |
| What's the goal? | All 781 wm tests at 0 diff; CSS2 stays at 99/99. **Achieved.** |
| What have I learned? | Group C's fix required TWO paired changes — the baseline swap (narrow predicate only) AND CSS Syntax §9 EOF recovery for the REF's `]]>`-terminated `.ignore` rule. Either alone leaves 8.4% diff. Prior I2 salvage failed because it broadened the swap to all sideways cases (regressed 25 tests) AND skipped the §9 fix. Always read the REF file, not just the test file. |
| What have I done? | Phases 1 + 2 + 5a + 5b + 5c + 5d + 5e + 5f (all groups A/B/C) complete; iframe capability gap closed; Phase 7 integration audit complete. Zero wm failures remain. |

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
1. Group B — two tests, one root cause, low regression risk. **DONE 2026-04-21.**
2. Group A — one test, but unlocks containing-block / ancestor-walk class of bugs.
3. Group C — hardest, needs focused Blink study; save for last.

## Session log entry — 2026-04-21 (Group B — DONE)

**Tests fixed:** `block-plaintext-004.html` (was 0.9%) and `block-plaintext-006.html` (was 1.0%) — both now PASS at 0 pixel diff.

**Hypothesis revision.** The plan's single-root-cause hypothesis ("per-line paragraph level sourced wrong") was **wrong**. Visual scanning of the output PNGs via `/tmp/scanimg.go` showed the boxes were shaped/ordered correctly; only vertical line spacing was off. That pointed at line-box metrics, not paragraph-direction flow.

**Actual root causes — both foundational (not plaintext-specific):**

1. **`pkg/css/cascade.go:709-729`** — `ApplyInheritedProperties` only handled `em` font-size values against parent's computed font-size; missing `%`. The test's `<pre>` has `font-size: 150%`, but `GetFontSize()` fell back to 16px because `ParseLengthWithFontSize` doesn't understand `%`. Fix: resolve `%` the same way as `em`, using parent's already-cascaded absolute value (CSS 2.1 §15.7). Added `ParsePercentage` path + `!HasSuffix "rem"` guard alongside the existing em path.

2. **`pkg/layout/inline_layout.go:1577-1614`** — `computeLineMetricsEx`'s `InlineItemControl` case (which sizes blank-line struts for `\n\n` in `white-space: pre`) diverged from the `InlineItemText` case when `line-height: normal`: used `GetLineHeight()`'s 1.2×fontSize fallback, used `fontSize - ascent` for descent, and gated half-leading on `>0`. Fix: mirror `InlineItemText` exactly — `FontHeightFromFont` when `IsLineHeightNormal()`, `FontDescentFromFont` for descent, unconditional half-leading application (CSS 2.1 §10.8).

**Key insight.** Fix #1 alone caused a **regression** (0.9% → 1.7%). It correctly bumped `<pre>` font-size from 16→24px, but that exposed the dormant strut divergence — blank lines had the wrong height at the new font-size. Fix #2 brought both tests to 0 diff. Interpretation: the tests were previously "close" only because both bugs were canceling partially. Either fix alone is a regression; both together are correct.

**Debug process:**
- `/tmp/scanimg.go` — Go pixel scanner that located orange border rows and dark-glyph rows in both test and ref PNGs. Quantified "box is 40px too tall" objectively when visual diff was ambiguous.
- Stderr instrumentation in `layoutInlineChildren` logging `{tag, font-size, line-height, font-path, fontHeight, lineHeight, ascent}` per line. Showed 5 lines × 19.2px each = 96px content for the `<pre>` — correct structure but at 16px font. After fix #1: 5 lines × wrong-per-line = incorrect. After fix #2: 5 lines × correct = 0 diff.

**Verification commands:**
```
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/Cellar/go/1.26.2/bin/go test \
  ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/(block-plaintext-004|block-plaintext-006)' -v
→ 2/2 PASS at max diff 0.
```

**Cleanup before commit:**
- Removed stderr instrumentation (`fmt`, `os` imports + `fmt.Fprintf(os.Stderr, ...)` block after `createLineBoxEx`).
- Reverted `go.mod` 1.25.5 → 1.25.5 (test runner bumps to 1.26.2 automatically).

**Files modified:** `pkg/css/cascade.go`, `pkg/layout/inline_layout.go` (2 files, ~23 insertions / 10 deletions).

**Regression sweep owed:** targeted run of other wm tests that exercise `%` font-size or blank lines in `<pre>`/`white-space: pre`. Both fixes are foundational so broader-than-plaintext benefit is expected, but the regression risk is non-zero where adjacent tests depended on the old behavior.

**Commit:** `c0536939` "Phase 5f Group B: fix block-plaintext-004/006 at 0 pixel diff".

### Regression sweep (2026-04-21, post-commit)

Grep of wm test data shows `%` font-size is used by ~96 test drivers across `bidi-*` and `block-*` buckets (refs included, 180 files total). `<pre>` is used by `block-plaintext-006` and `block-flow-direction-vrl-026`. Ran the full `(bidi|block)-` pattern plus CSS2.

| Scope | Result |
|---|---|
| `TestWPTCSS3Reftests/css-writing-modes/(bidi\|block)-` | 163 matched, 162 PASS, 1 FAIL |
| CSS2 full (`TestWPTReftests`) | 99/99 PASS |

The one FAIL is `inline-block-alignment-007.xht` — pre-existing Group C at 8.4%, unchanged. Zero new regressions.

All `bidi-*` buckets (embed/isolate/isolate-override/normal/override/plaintext/table/unset: 79 tests) stayed green — the `%` font-size fix doesn't regress any bidi test that inherits `%` font-size from the reference scaffolding. Both `block-plaintext-004` and `block-plaintext-006` re-verified at 0 diff in the broad run. `block-flow-direction-vrl-026` (the other `<pre>` case) also PASS.

**Next:** Group A (icb-007 orthogonal-root ancestor walk).

## Phase 5f Group A — `orthogonal-root-resize-icb-007` at 0 diff (2026-04-21)

**Root cause.** The block-child path at `block_layout.go:368-370` correctly passed the §10.3.2 orthogonal-available-block (walked to ICB via `computeOrthogonalAvailableBlock`) to orthogonal children, but the **atomic-inline path** (`line_breaker.handleAtomicInline` at `line_breaker.go:1205-1234`) did not. It used the raw `lb.space.AvailableSize.BlockSize` that had flowed down from a grandparent's content-box (10px in icb-007), so the orthogonal inline-block root's axis-swapped inline-size was 10 instead of ICB = 100. A second amplifier: `layoutInlineChildren`'s hand-constructed `lineSpace` dropped `OrthogonalFallbackInlineSize`/`OrthogonalFallbackBlockSize` from `bla.space`, so even the existing Indefinite→ICB fallback couldn't fire.

**Fix (3 files, 46 insertions / 4 deletions):**

1. **`pkg/layout/constraint_space.go`** — new field `ConstraintSpace.OrthogonalAvailableBlock` carrying the pre-resolved available block-size that orthogonal atomic-inline descendants of the current IFC should see.
2. **`pkg/layout/inline_layout.go`** — `layoutInlineChildren` now computes `hasExplicitBlock`/`explicitBlockSize` from `geomForPct`, calls `computeOrthogonalAvailableBlock` (same helper as block-child path), and populates the new field plus `OrthogonalFallbackInlineSize` + `OrthogonalFallbackBlockSize` (via `computeOrthogonalFallbackBlockForChildren`) on `lineSpace`.
3. **`pkg/layout/line_breaker.go`** — `handleAtomicInline`'s normal layout branch now checks `lb.space.WritingDirection.IsOrthogonalTo(childWDM)` and prefers `lb.space.OrthogonalAvailableBlock` as the parent-side BlockSize. The axis-swap in `SetAvailableSize` then gives the child the correct ICB-capped inline-size.

**Blink verification.** Confirmed via `block_node.cc` + `space_utils.cc` fetches that Blink applies the same ancestor-walk-to-ICB algorithm to atomic-inline orthogonal roots as to block-level ones — no position:static gate, no inline-vs-block gate. Our fix mirrors that symmetry.

**Verification commands:**
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes/orthogonal-root-resize-icb-007' -v
→ PASS (480000 pixels, max diff: 0)
```

**Regression sweep (post-fix):**

| Scope | Result |
|---|---|
| `TestWPTCSS3Reftests/css-writing-modes/*` (full) | 780/781 PASS — only Group C `inline-block-alignment-007` still FAIL (unchanged 8.4%) |
| `TestWPTCSS3Reftests/css-writing-modes/(inline-block-\|orthogonal-\|available-)` | 42/43 PASS — same pre-existing Group C |
| `TestWPTReftests` (CSS2) | 99/99 PASS |
| `TestWPTCSS3Reftests/(css-position\|css-display\|css-flexbox)/` | 703/813 PASS — **identical before and after** (baselined via stash) |

Zero regressions across writing-modes, CSS2, and broader layout suites. The remaining writing-modes FAIL is Group C (deliberately deferred — prior broad attempts regressed 25 tests, requires precise Blink baseline-metrics study).

**Files modified:** `pkg/layout/constraint_space.go`, `pkg/layout/inline_layout.go`, `pkg/layout/line_breaker.go`.

**Next:** Group C (`inline-block-alignment-007`) or exit Phase 5f.

## Phase 5f Group C — `inline-block-alignment-007` at 0 diff (2026-04-21)

**Test:** `writing-mode: vertical-lr; text-orientation: sideways; font: Ahem 60/120/30`. Three swatches of increasing font-size inside an inline-block; reference expects a straight LEFT edge where each swatch's alphabetic baseline aligns. Baseline 8.4% diff (40320 px).

**Root cause: two paired bugs. Either alone is insufficient.**

1. **VLR+sideways alphabetic baseline not swapped.** In `writing-mode: vertical-lr` with `text-orientation: sideways`, glyphs rotate 90° CW. Block-start is on the LEFT; after CW rotation, the alphabetic baseline lands at `typographic_descent` from block-start, not `typographic_ascent`. Our `computeLineMetricsEx` and `createLineBoxEx` always used `FontAscentFromFont` as `alignment_ascent` and `FontDescentFromFont` as `alignment_descent`. Correct for horizontal + VLR-upright + `sideways-lr` (CCW) + `sideways-rl`/VRL-sideways (block-start on RIGHT cancels), but wrong for VLR+sideways.

2. **CSS parser dropped unclosed blocks instead of applying CSS Syntax L3 §9 EOF recovery.** The reference file `inline-block-alignment-007-ref.xht` wraps its stylesheet in an XHTML `<style><![CDATA[ … ]]></style>` block. Its last rule is:
   ```css
   .ignore { float: left; width: 120px; height: 120px; margin: 60px 24px 30px 60px;
   ```
   — terminated only by `]]>`, no `}`. Our `splitRules` discarded any rule whose brace-depth > 0 at EOF. CSS Syntax Level 3 §9 mandates that tokenizers treat any open block as closed at EOF. Blink applies §9. Without recovery, the `.ignore` float was absent from the REF, and the swatches sat at x=8 instead of being displaced to x=212 by the float.

**Why the I2 salvage (`df19b64a`, reverted) failed.** It broadened the swap to all sideways writing modes (`sideways-lr`, `sideways-rl`, VRL+sideways). Those cases don't need the swap — only VLR+sideways puts block-start on the LEFT in a way that CW rotation inverts. Broadening regressed 25 tests. It also did not include the §9 recovery fix, so the target test still failed.

**Fix (2 files, ~65 lines):**

1. **`pkg/layout/inline_layout.go`** — three helpers + one narrow predicate:
   ```go
   func needsSidewaysVLRBaselineSwap(wdm WritingDirectionMode, centralBaseline bool) bool {
       return wdm.WM == WritingModeVerticalLR && !centralBaseline
   }
   func alignmentAscentFromFont(swap bool, fontSize float64, fontPath string) float64 {
       if swap { return text.FontDescentFromFont(fontSize, fontPath) }
       return text.FontAscentFromFont(fontSize, fontPath)
   }
   func alignmentDescentFromFont(swap bool, fontSize float64, fontPath string) float64 {
       if swap { return text.FontAscentFromFont(fontSize, fontPath) }
       return text.FontDescentFromFont(fontSize, fontPath)
   }
   ```
   Applied at 6 sites — `inlineBoxAsDesc` closure, text-positioning `ascent` computation in `createLineBoxEx`, and the strut / `InlineItemOpenTag` / `InlineItemText` / `InlineItemControl` arms of `computeLineMetricsEx`. Every line-metric site that formerly called `text.FontAscentFromFont` / `text.FontDescentFromFont` now routes through the helpers. `!centralBaseline` excludes VLR+upright (which uses central baseline anyway).

2. **`pkg/css/stylesheet.go`** — `splitRules` emits the remainder with synthesized closing `}`s when `depth > 0 && start < len(css) && strings.TrimSpace(css[start:]) != ""` at EOF. Replaces the previous silent-discard fallback. Documented inline with the §9 spec reference.

**Blink verification.** Blink's `LogicalBoxFragment::BaselineMetrics` + its CSS tokenizer (§9 compliant) both apply the same behavior. The narrow predicate matches Blink's "alphabetic, not central" gate on the baseline swap.

**Verification commands:**
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes/inline-block-alignment-007' -v
→ PASS (480000 pixels, max diff: 0)
```

**Regression sweep (post-fix):**

| Scope | Result |
|---|---|
| `TestWPTCSS3Reftests/css-writing-modes/*` (full) | **781/781 PASS** (was 780/781; Phase 5f complete) |
| 33 hand-picked regression candidates (sideways-lr, VLR line-box-height, text-orientation, inline-block-alignment-slr-009) | 33/33 PASS |
| `TestWPTReftests` (CSS2) | 99/99 PASS |
| `TestWPTCSS3Reftests/(css-flexbox\|css-position)/` | 676 PASS / 57 FAIL (was 671/62 — **improved** by 5 tests from §9 recovery) |

Zero regressions. §9 EOF recovery incidentally fixed 5 css-flexbox/css-position tests with similarly `]]>`-terminated rules.

**Files modified:** `pkg/layout/inline_layout.go`, `pkg/css/stylesheet.go`.

**Phase 5f complete.** All 781 wm tests pass at 0 pixel diff. CSS2 99/99 preserved. Pivoting next to css-position (highest remaining headroom) or Phase 6 delivery write-up.
