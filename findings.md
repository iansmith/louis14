# Findings & Decisions — css-position category

## Rules pointer
Do not restate project rules here. They live in:
- `/Users/iansmith/louis14/CLAUDE.md`
- `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`

Findings should assume those rules are already loaded in context.

## Archived wm work
All writing-modes category findings — 787 tests, bidi root-causes, orthogonal sizing, Phase 5f Groups A/B/C — have been moved to `docs/findings-wm.md`. Do not duplicate here.

## Requirements
- 104 css-position tests actually exercised by `TestWPTCSS3Reftests/css-position`.
- Goal: all 104 pass at 0 diff. Baseline 50/104 (48%) → close 54 failures + 5 NORUN.
- Do not regress: css-writing-modes (781/781), CSS2 (99/99), css-flexbox (~621/629).

## Phase 0 Baseline (complete — 2026-04-21)
Raw log: `output/baselines/css-position-2026-04-21.log`
Parsed list: `/tmp/css-position-fails.tsv` (regenerate via `/tmp/parse_css_position.sh`)

### Overall
- 104 tests run · **50 PASS · 54 FAIL · 5 NORUN**
- Highest diff: `containing-block-change-scrollframe.html` (10.4% / 50000 px).
- Lowest diff (still failing): `position-absolute-dynamic-list-marker.html` (0.0% / 18 px) — likely a 1-pixel geometric slip, not visible fuzz.

### NORUN triage
These 5 tests produce `=== RUN` but no `REFTEST PASS/FAIL` line. Possible causes: harness timeout, crash in render, JS-only test that doesn't reach the screenshot step, or reftest-wait never resolving.

| Test | Likely cause (pre-investigation) |
|------|---|
| `hypothetical-box-scroll-parent.html` | Uses `scrollLeft = 1000` + text mutation; harness may not flush scroll in sync |
| `hypothetical-box-scroll-viewport.html` | Similar: scroll-then-mutate dependency |
| `position-absolute-multicol-001.html` | Multicol abspos; possible unsupported feature dropping test |
| `position-change.html` | `reftest-wait` + double rAF + `takeScreenshot`; may hang if rAF is not driving |
| `replaced-object-backdrop.html` | Likely `<object>` element rendering — may be unsupported |

Treat all 5 as failing for planning purposes. Each gets a 10-minute triage pass in its owning phase (most fall under Phase 4 G-HYPO or Phase 9 G-SINGLETONS).

## Group breakdown (54 fails + 5 NORUN = 59)

Grouped by hypothesised shared root cause, not by diff %. Largest-cluster-first.

### G-TABLE-REL — 16 tests — **Phase 1 target**
```
position-relative-table-thead-top.html       1.2%
position-relative-table-thead-left.html      1.2%
position-relative-table-tfoot-top.html       1.2%
position-relative-table-tfoot-left.html      1.2%
position-relative-table-tbody-top.html       1.2%
position-relative-table-tbody-left.html      1.2%
position-relative-table-tr-top.html          1.7%
position-relative-table-tr-left.html         1.7%
position-relative-table-tfoot-top-absolute-child.html  1.7%
position-relative-table-tr-top-absolute-child.html     1.0%
position-relative-table-tr-left-absolute-child.html    1.0%
position-relative-table-thead-top-absolute-child.html  1.0%
position-relative-table-thead-left-absolute-child.html 1.0%
position-relative-table-tfoot-left-absolute-child.html 1.0%
position-relative-table-tbody-top-absolute-child.html  1.0%
position-relative-table-tbody-left-absolute-child.html 1.0%
position-relative-table-td-top.html          0.6%
position-relative-table-td-left.html         0.6%
```
**Hypothesis.** `pkg/layout/table_layout.go` has no `PositionRelative`/`PositionSticky` branch. `block_layout.go:928-939`, `flex_layout.go:1821-1832`, `grid_layout.go:395-403`, and `inline_layout.go:1122/1286/1401` all set `Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)` when the style's position is relative/sticky. The table algorithm emits fragments but never calls this.

**Blink entry points (to read in Phase 1):**
- `third_party/blink/renderer/core/layout/layout_table_section.cc` — section (thead/tbody/tfoot) layout + paint.
- `third_party/blink/renderer/core/layout/layout_table_row.cc` — row layout.
- `third_party/blink/renderer/core/layout/layout_object.cc` — `ComputeRelativeOffset`.
- `third_party/blink/renderer/core/layout/ng/table/ng_table_layout_algorithm.cc` — NG table algorithm.

**Expected fix shape.** In `table_layout.go`, after emitting each row/section/cell fragment, check the child's style. If `Position == Relative || Sticky`, compute `offset` from inset properties (`top/right/bottom/left`) against the containing-block dimensions and set `fragment.RelativeOffset`. Mirror `computeRelativeOffset` from `block_layout.go:964`.

**Open question:** table sections and rows may or may not generate their own fragments in our current architecture. If they are folded into the table's single fragment, the `RelativeOffset` has to be applied during fragment tree construction rather than at algorithm exit. Answer before writing code.

### G-ABS-CENTER — 5 tests
```
position-absolute-center-001.html   0.4%
position-absolute-center-002.html   0.8%
position-absolute-center-003.html   0.3%
position-absolute-center-004.html   0.3%
position-absolute-center-007.html   2.1%
```
**What the tests exercise.** `position: absolute` with either `margin: auto` + both insets, or `justify-content: center` on a flex container, combined with CSS Align 3 abspos sizing (available space = 2 × distance from center of static-position rectangle to closest CB edge).

**Blink entry points:**
- `third_party/blink/renderer/core/layout/ng/ng_absolute_utils.cc` — `ComputeOutOfFlowInsetSize`, `ComputeOutOfFlowBlockSize`.
- `third_party/blink/renderer/core/layout/ng/ng_out_of_flow_layout_part.cc` — dispatch.

**Spec:** <https://drafts.csswg.org/css-align-3/#abspos-sizing>. Available-space algorithm is non-trivial — center alignment needs the center of the static-position rect to be known before sizing.

### G-CB-CHANGE — 3 tests
```
containing-block-change-scrollframe.html               10.4%
containing-block-change-button.html                    4.2%
absolute-pos-box-inside-fixed-pos-box-with-changing-height.html  0.5%
```
**What they exercise.** JS mutates a property that establishes a new containing block — `overflow: hidden` on a div, or insertion of a button — after the page has laid out. Abspos children must re-resolve to the new CB.

**Blink entry points:**
- `third_party/blink/renderer/core/style/computed_style.cc` — `NeedsContainingBlockInvalidation`.
- `third_party/blink/renderer/core/layout/layout_object.cc` — `ContainingBlock()`, `ContainingBlockForAbsolutePosition()`.
- `third_party/blink/renderer/core/layout/layout_object.cc` — `StyleDidChange` containing-block-change path.

**Related:** overlaps with G-DYN-STATIC when the CB change happens through a `display` or `position` flip.

### G-DYN-STATIC — 6 tests
```
position-absolute-dynamic-static-position-floats-001.html   0.7%
position-absolute-dynamic-static-position-floats-002.html   0.3%
position-absolute-dynamic-static-position-floats-003.html   0.3%
position-absolute-dynamic-static-position-floats-004.html   0.7%
position-absolute-dynamic-static-position-inline.html       2.1%
position-absolute-dynamic-static-position-table-cell.html   2.1%
```
**What they exercise.** JS flips a property (float insertion, `display: inline → block`, table-cell vertical-align interaction) that changes the abspos child's static position. Triggers re-layout; the new static position must be picked up.

**Blink entry point:** `ng_out_of_flow_layout_part.cc::LayoutCandidate` + static-position caching in layout-input.

### G-HYPO — 3 FAIL + 2 NORUN
```
hypothetical-dynamic-change-001.html   2.1%  (fixed-pos ancestor moves)
hypothetical-dynamic-change-002.html   2.1%
hypothetical-dynamic-change-003.html   4.2%
hypothetical-box-scroll-parent.html    NORUN
hypothetical-box-scroll-viewport.html  NORUN
```
**What they exercise.** CSS Position 3 hypothetical-box algorithm: `position: absolute` with auto-left/auto-right uses the parent's in-flow position. When the ancestor itself moves (via JS), the child's hypothetical position must re-derive.

**Blink entry points:**
- `third_party/blink/renderer/core/layout/ng/ng_block_layout_algorithm.cc` — `PrepareLayout` for OOF.
- Hypothetical-position reference: <https://drafts.csswg.org/css-position/#size-and-position-details>.

### G-ROOT-FLEX-GRID — 4 tests
```
position-fixed-root-element-flex.html    0.8%
position-fixed-root-element-grid.html    0.8%
position-absolute-root-element-flex.html 0.8%
position-absolute-root-element-grid.html 0.8%
```
**What they exercise.** `<html>` element with `position: fixed|absolute` and `display: flex|grid`. Insets define the box size relative to ICB; not a shrink-to-fit.

**Blink entry point:** `layout_view.cc` + flex/grid root-element special-cases.

### G-FIXED — 1 test
```
position-fixed-scroll-nested-fixed.html   4.2%
```
Nested `position: fixed` inside a scrolling fixed container; inner fixed should escape the scroller and paint above.

### G-ABS-IN-INLINE — 2 tests
```
position-absolute-in-inline-003.html   2.9%
position-absolute-in-inline-004.html   2.3%
```
**What they exercise.** Inline as containing block for abspos descendants. Spec: <https://www.w3.org/TR/css-position-3/#def-cb> + CSS 2.1 §10.1.4 ("if the element is inline-level, the containing block depends on the `direction` property of the container").

**Blink entry point:** `inline_box.cc` + `InlineNode::ComputeContainingBlockForOutOfFlow`.

### G-STICKY — 1 test
```
sticky-top-001.html   3.4%
```
**What it exercises.** `position: sticky; top: 10px` in the middle of content at scroll=0 should stay in normal flow (offset 0), NOT offset by 10px.

**Current behavior.** Our code treats sticky identically to relative (`block_layout.go:929`, etc.), applying `computeRelativeOffset` unconditionally. At scroll=0, the top inset is applied, giving wrong result.

**Blink entry point:** `third_party/blink/renderer/core/layout/sticky_position_constraint.h` + `scroll_anchor.cc`.

**Minimum viable fix:** for `position: sticky`, emit zero `RelativeOffset` when the scroll container's scroll offset is 0 (or more generally, when the sticky's flow position satisfies `flow_top >= scroll_offset + inset_top`).

### G-REPLACED — 1 test
```
position-absolute-replaced-no-intrinsic-size.tentative.html   2.1%
```
`<img>` with `position: absolute; top:0; bottom:0; height: max-content; width: 100px; margin: auto` on an SVG with `viewBox='0 0 50 50'`. CSS 2.2 §10.3.7 / §10.6.5.

### G-SINGLETONS — 11 tests (includes 3 NORUN)
```
position-relative-001.html                          1.0%  non-table % top/left
position-relative-002.html                          1.0%
position-relative-011.html                          0.4%  tbody %-top shouldn't resolve
position-relative-012.html                          0.4%  tbody position:relative + top:100%
position-relative-013.html                          0.4%  td position:relative + top:100%
stack-floats-001.xht                                1.7%  CSS 2.1 §9.9 stacking order
position-absolute-iframe-print-001.sub.html         0.3%  abspos iframe in pagination
position-absolute-iframe-print-002.sub.html         0.3%
clear-001.xht                                       0.0%  96 px; CSS 2.1 §9.5 clear
position-absolute-dynamic-list-marker.html          0.0%  18 px; ::marker + abspos
position-change.html                                NORUN
replaced-object-backdrop.html                       NORUN
position-absolute-multicol-001.html                 NORUN
```
Mixed shapes; likely several independent root causes. Sweep last.

**Note:** `position-relative-011/012/013` are table-related (`%-top` on `<tr>`/`<tbody>`/`<td>` under position:relative) — they may share a root cause with G-TABLE-REL. If so, closing Phase 1 may also close them. Verify in Phase 1's regression sweep.

## Super-cluster counts
| Cluster | Count | Cumulative if closed |
|---|---|---|
| G-TABLE-REL | 16 | 66 |
| G-DYN-STATIC + G-CB-CHANGE | 9 | 75 |
| G-ABS-CENTER | 5 | 80 |
| G-HYPO | 5 | 85 |
| G-ROOT-FLEX-GRID + G-FIXED | 5 | 90 |
| G-ABS-IN-INLINE | 2 | 92 |
| G-STICKY | 1 | 93 |
| G-REPLACED | 1 | 94 |
| G-SINGLETONS | 11 | 105 (capped at 104 in practice) |
| **Total** | **54 + 5 NORUN** | **104 if all close** |

## Blink study checklist (before Phase 1 code)
- [ ] Read `ng_table_layout_algorithm.cc` for fragment emission order.
- [ ] Read `ComputeRelativeOffset` (likely `layout_object.cc` or `ng_relative_utils.cc`).
- [ ] Find where Blink applies `RelativeOffset` to table sections/rows/cells — `PaintLayer`? Fragment construction?
- [ ] Confirm whether Blink applies relative offsets to `<caption>` (none of our failing tests use caption, so this is a bounds check only).

## Test Results
| Scope | Test count | Baseline | Target |
|---|---|---|---|
| css-position (TestWPTCSS3Reftests) | 104 | 50 PASS / 54 FAIL / 5 NORUN | 104 PASS |
| css-writing-modes (invariant) | 781 | 781 PASS | 781 PASS |
| CSS2 (invariant) | 99 | 99 PASS | 99 PASS |
| css-flexbox (watch) | ~629 | 621 PASS (as of 2026-04-20 baseline) | ≥621 |

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Start with G-TABLE-REL | Highest single-root-cause yield (16 tests); `table_layout.go` clearly missing the branch. |
| Treat NORUN as failing | CLAUDE.md §3 — all tests must pass; cannot silently drop. |
| Do not run css-position category in full except at milestone verifications | CLAUDE.md §4 — only failing-test + adjacent runs during feature work. |
| Preserve wm invariants as hard gate | Phase 5f complete; any wm regression reverts the offending commit. |

## Issues Encountered (for this category)
*(populated as work progresses)*

## Notes
- Attack order is **not** by % diff. Shared-root-cause grouping is prioritised (CLAUDE.md §1).
- Every group's first step is Blink study (CLAUDE.md §2). Do not start coding a group without that.
- Each phase commits at completion of the group plus passing regression sweeps. Intermediate milestones can commit WIP if blocked, but must note the blocker.
