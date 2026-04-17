# Flexbox Round 11: Top 6 Improvements

Current state: **589 pass / 40 fail** across 6,720 css-flexbox tests. CSS2 reftests stand at 93/99; css-tables at 52/6,720. Commit `9f2ce1f8` ("Size rowspan cells to visible spanned rows + border-box clip") is HEAD.

The following six targets group the 32 failing flex tests by their most likely root cause. Unlike earlier rounds these targets **may share files** — the user allowed overlapping work because some of these categories intersect in the flex core (`flex_layout.go`, `replaced_layout.go`). Run them sequentially if you need to, or in a single agent if you prefer.

---

## Project rules (non-negotiable — repeat to any delegated agent)

These five rules come from `/Users/iansmith/louis14/CLAUDE.md`. Every step of every target must follow them.

1. **Foundational correctness over quick wins.** NEVER look for low-hanging fruit, near-passing tests, or easy wins. Every fix must work for ALL cases. Don't filter tests by error percentage or chase "nearly passing" tests. A point fix now will not help and will likely make things worse later. Pick a target and solve it completely — don't skip around looking for something "more tractable."
2. **Study Blink BEFORE writing any code.** When starting work on a new area, the FIRST step is to look at what Blink/Chromium does. Study their abstractions, algorithms, and types. Only then write code. Mirror their type names, algorithm structure, and constraint-passing patterns. The louis14 codebase is modeled on Blink's LayoutNG — keep it aligned.
3. **All tests must pass at 0% diff.** A 0.5% diff is a failure just like 28%. Never dismiss failures as "font rendering" or "anti-aliasing" — the WPT tests have built-in fuzzy tolerances provided by the test authors specifically to account for text rendering differences. If a test is failing, the diff exceeds what the test author considered acceptable for rendering variation, which means it's a real bug.
   - **Exception**: if the test also fails in Chrome (confirm via `wpt.fyi` `status:"F"`), we accept the failure because we are matching Blink. Record the wpt.fyi evidence in the commit message.
4. **Test execution discipline.** Do not run the full test suite or even the full section test suite during feature work. Run only the specific tests associated with the feature being worked on — typically 1 to 4 tests. Broader test runs are expensive and should only happen at commit checkpoints.
5. **Operational rules.**
   - Never use `open` to display files from agents — it disrupts the user's screen.
   - Always commit and push before launching worktree agents — worktrees start from HEAD, not the working directory.
   - Instruct agents to commit and report at each milestone, not just at the end.
   - When running in a worktree (any directory that is NOT `~/louis14`), commit ONLY to the worktree branch. Never commit directly to `fix/*` or `master` from a worktree.

If you delegate any subtask to an agent, paste this block into the agent's prompt verbatim.

---

## Target 1: Flex baseline synthesis for multi-line, orthogonal, and fieldset items (~9 tests, ~40k px)

### Problem
louis14's flex baseline resolution disagrees with Blink whenever the baseline source is not a simple, same-writing-mode, single-line item. The failing tests all hit a flavor of this: orthogonal items, wrapped lines, second-line items, fieldset containers.

### Affected tests

| Test | Diff |
|---|---|
| flexbox-align-self-baseline-horiz-006.xhtml | 8120 |
| flexbox-align-self-baseline-horiz-008.xhtml | 25862 |
| flexbox-align-self-horiz-002.xhtml | 235 |
| flexbox-baseline-align-self-baseline-horiz-001.html | 557 |
| flexbox-baseline-multi-item-horiz-001a.html | 122 |
| flexbox-baseline-multi-item-horiz-001b.html | 122 |
| flexbox-baseline-multi-line-horiz-002.html | 574 |
| flexbox-baseline-multi-line-vert-001.html | 1649 |
| flexbox-baseline-multi-line-vert-002.html | 1436 |
| fieldset-baseline-alignment.html | 251 |

### Likely root causes
- `resolvedFirstBaseline()` / `resolvedLastBaseline()` in `pkg/layout/flex_layout.go` select the wrong synthesis source when items are orthogonal or span multiple lines.
- CSS Flexbox §8.5: "Each flex line has a baseline... the flex container baseline is the baseline of the *first flex line*." If the first line has no baseline-aligned item, the spec defines a precise fallback (first item's synthesized baseline).
- Orthogonal items: Blink excludes them from baseline calculation entirely (`flex_layout_algorithm.cc` — see `FlexLine::ComputeCrossAxisMetrics`). Louis14 may be including them.
- Fieldset baseline: the legend element perturbs where the fieldset's baseline comes from — Blink's `layout_fieldset.cc` handles this specifically.

### Blink references to study first
- `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`, `ComputeBaselineAlignmentInfo` and `AlignFlexLines`
- `third_party/blink/renderer/core/layout/flex/flex_line.cc`, baseline accumulation
- `third_party/blink/renderer/core/layout/layout_fieldset.cc` for fieldset baseline

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-baseline|flexbox-align-self-baseline|flexbox-align-self-horiz-002|fieldset-baseline)' \
  -count=1
```

---

## Target 2: Aspect-ratio transfer through min-size:auto for flex items (~5 tests, ~43k px)

### Problem
The automatic minimum size for flex items (CSS Flexbox §4.5) with replaced content must transfer cross-axis size constraints through the aspect-ratio. louis14 does not fully implement that transfer — images end up at their intrinsic size instead of filling definite main-axis extent.

### Affected tests

| Test | Diff |
|---|---|
| flex-aspect-ratio-img-column-010.html | 4000 |
| flex-aspect-ratio-img-column-012.html | 9900 |
| flex-aspect-ratio-img-column-018.html | 5000 |
| flex-aspect-ratio-img-row-015.html | 20000 |
| flex-minimum-width-flex-items-009.html | 4000 |

### Likely root causes
- `pkg/layout/flex_layout.go` `flexItemMinMain` / `flexItemContentSuggestion` path: does not fold the transferred size from the cross-axis (min-height or height) through the item's aspect-ratio.
- CSS Sizing 4 §6.3 ("Transferred Minimums/Maximums via Aspect Ratio") gives the exact rule: `transferred-min = cross-min × (main-intrinsic / cross-intrinsic)`.
- Blink implements this in `flex_layout_algorithm.cc` `ComputeMinAndMaxContentContributionForItem` calling `CalculateInitialFragmentGeometryForItem` which consults `BlockNode::TransferredMinMaxSizes`.

### Blink references
- `third_party/blink/renderer/core/layout/min_max_sizes.cc` — the transferred-min/max math
- `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`, `ConstructAndAppendFlexItem`
- `third_party/blink/renderer/core/layout/block_node.cc` `TransferredMinMaxSizes`

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flex-aspect-ratio-img|flex-minimum-width-flex-items-009)' \
  -count=1
```

---

## Target 3: min-height/min-width auto content-size suggestion for flex items (~4 tests, ~37k px)

### Problem
The content-size component of min-size:auto (§4.5) — the size necessary so that the content does not overflow when the item is not flexed — disagrees with Blink for items that contain definite cross-axis-dimensioned replaced content or scrolling descendants.

### Affected tests

| Test | Diff |
|---|---|
| flex-minimum-height-flex-items-019.html | 7000 |
| flex-minimum-height-flex-items-030.html | 10000 |
| flexbox-min-width-auto-005.html | 7600 |
| flexbox-min-width-auto-006.html | 12000 |

### Likely root causes
- §4.5 specifies content-size suggestion = min(specified-size-suggestion, content-size) and the content-size is the smallest size the item can be "without any of its children overflowing".
- louis14's implementation may be using min-content size (tightest line-wrap) rather than the §4.5 "content size" which allows child overflow along main-axis when the cross-axis is constrained.
- The §4.5 "specified size suggestion" also has a definite-cross-size carve-out when the item has an aspect-ratio; overlaps with Target 2 but distinct.

### Blink references
- `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc`, `ResolveAutoMinSize`
- `third_party/blink/renderer/core/layout/flex/flex_item.cc`, `MinContentContributionForContentBasedMinimum`

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flex-minimum-height-flex-items-019|flex-minimum-height-flex-items-030|flexbox-min-width-auto)' \
  -count=1
```

---

## Target 4: Replaced form elements (canvas, textarea) in flex contexts (~3 tests, ~10k px)

### Problem
Canvas and textarea flex items do not resolve to the sizes Chrome gives them. Default intrinsic sizes for these form elements must be exact (UA stylesheet + replaced-content defaults in HTML §10.12 and CSS Images §3.2).

### Affected tests

| Test | Diff |
|---|---|
| flexbox-basic-canvas-horiz-001v.xhtml | 751 |
| flexbox-basic-textarea-horiz-001.xhtml | 3005 |
| flexbox-basic-textarea-vert-001.xhtml | 6275 |

### Likely root causes
- `pkg/layout/replaced_layout.go`: intrinsic width/height defaults for canvas (default 300×150) and textarea (computed from cols/rows + font metrics).
- The flex algorithm's treatment of min-width:0 on these replaced items may be bypassed by a replaced-default that louis14 applies too early.
- For textareas specifically: UA-stylesheet line-height metrics must match Chrome's `cols×avg-glyph-width` intrinsic-width convention.

### Blink references
- `third_party/blink/renderer/core/html/html_canvas_element.cc` — default intrinsic size (300×150)
- `third_party/blink/renderer/core/html/forms/html_text_area_element.cc` — intrinsic size from cols/rows
- `third_party/blink/renderer/core/layout/layout_replaced.cc` — `ComputeIntrinsicSizing`

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/flexbox-basic-(canvas|textarea)' \
  -count=1
```

---

## Target 5: flex-direction dynamics + DOM mutation + order (~4 tests, ~62k px)

### Problem
These tests change flex state after initial layout (flex-direction swap via JS, element insertion, isize change, order-only reordering). Our largest diffs are here: 46k and 15k on two tests alone.

### Affected tests

| Test | Diff |
|---|---|
| flex-direction-modify.html | 46492 |
| flex-direction-with-element-insert.html | 520 |
| dynamic-isize-change-004.html | 15000 |
| flexbox-order-only-flexitems.html | 97 |

### Likely root causes
- The reftest harness drives JS between test render and comparison; if louis14 caches layout results across DOM/style mutations, they leak stale geometry.
- `flex-direction` swapping from row to column reorients main/cross axes — any pre-computed item data keyed by the wrong axis must be invalidated.
- `order` property flex-item reordering: Blink sorts items by order within a pass; louis14 may sort wrong or skip the sort on re-layout.
- Inspect whether louis14 reuses any layout cache; compare to Blink's `NGFragmentPlacement` invalidation in `local_frame_view.cc`.

### Blink references
- `third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc` — main/cross-axis plumbing
- `third_party/blink/renderer/core/layout/layout_box.cc` — cache invalidation hooks
- `third_party/blink/renderer/core/dom/node.cc` — style recalc / mutation → layout invalidation

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flex-direction|dynamic-isize|flexbox-order)' \
  -count=1
```

> ⚠️ flex-direction-modify's 46k diff is huge — this likely indicates a single structural bug (e.g., never reading current computed style). Focus investigation there first; the other three may fall out for free.

---

## Target 6: Safe-overflow alignment + percentage-in-flex-item + misc (~6 tests, ~72k px)

### Problem
A grab-bag whose common thread is alignment/sizing at the *flex-item level* rather than the container's flex algorithm.

### Affected tests

| Test | Diff |
|---|---|
| flexbox-safe-overflow-position-003.html | 1800 |
| flexbox-safe-overflow-position-004.html | 2000 |
| fixed-table-layout-with-percentage-width-in-flex-item.html | 57899 |
| css-box-justify-content.html | 2596 |
| flexbox-flex-wrap-vert-002.html | 1140 |
| flexbox-root-node-001b.html | 594 |

### Likely root causes
- **Safe overflow (2 tests)**: the `safe` alignment keyword (CSS Alignment 3 §4.3) instructs the renderer to fall back to start alignment rather than clip when content overflows. Grep `"safe"` in `pkg/layout/flex_layout.go` — if absent, this is unimplemented.
- **fixed-table-layout-with-percentage-width-in-flex-item (58k diff)**: a table-layout:fixed child with percentage width inside a flex item — percentage resolution must use the flex item's final main-size, not its initial constraint. Very similar to a bug we fixed in the rowspan path; check if `ResolvePercent` inside table layout gets the post-flex width or the hypothetical one.
- **css-box-justify-content**: basic `justify-content` edge case; likely a distribution math bug when flex grow factors sum to a specific value.
- **flexbox-flex-wrap-vert-002**: wrapping in column direction; cross-axis alignment with multiple lines (writing-mode ≠ axis).
- **flexbox-root-node-001b**: flex container as the document root (`html { display: flex; }`). ICB and initial size handling.

### Blink references
- `third_party/blink/renderer/core/layout/geometry/box_strut.cc` and `style/style_self_alignment_data.h` — overflow safety-keyword plumbing
- `third_party/blink/renderer/core/layout/table/table_layout_algorithm.cc` — percentage resolution inside sized containers
- `third_party/blink/renderer/core/layout/layout_view.cc` — root-element display:flex handling

### Verification
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-safe-overflow-position|fixed-table-layout-with-percentage-width-in-flex-item|css-box-justify-content|flexbox-flex-wrap-vert-002|flexbox-root-node-001b)' \
  -count=1
```

---

## Global regression checks (run after each target)

```bash
# Motivating flex three must stay PASS:
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -count=1

# CSS2 (must stay at 93/99 or better):
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -count=1 -timeout 600s 2>&1 | grep Summary

# Full flex suite (must rise above 589):
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox' -count=1 -timeout 900s 2>&1 | grep Summary
```

---

## Independence Matrix (informational only)

The user explicitly waived the disjoint-files constraint for this round. Targets 1, 2, 3, and 5 will all touch `pkg/layout/flex_layout.go`; Targets 2 and 4 will touch `pkg/layout/replaced_layout.go`. Plan sequential work, or run a single agent across the stack.

| | flex_layout.go | replaced_layout.go | layout_fieldset.go | table_layout.go | css/ua_stylesheet |
|---|---|---|---|---|---|
| Target 1 | ✓ | – | ✓ | – | – |
| Target 2 | ✓ | ✓ | – | – | – |
| Target 3 | ✓ | – | – | – | – |
| Target 4 | – | ✓ | – | – | ✓ |
| Target 5 | ✓ | – | – | – | – |
| Target 6 | ✓ | – | – | ✓ | – |

## IMPORTANT: Agent guidelines

Copy the "Project rules" block at the top of this document into every agent prompt — these rules are load-bearing. Summary reminders:

- **Study Blink before writing code.** Every target lists Blink source files — read them first, quote what they actually do in your commit message, and only then patch.
- **Commit at each milestone**, not only at the end. If a single test flips or a foundational refactor lands, that's a checkpoint. Push checkpoints to the remote branch so the user can inspect progress.
- **Never regress** the motivating flex three or the CSS2 93/99 count. Run the specific regression checks below after each commit, not after the whole target.
- **Blink-parity is success.** If a test fails in Chrome (confirm via `wpt.fyi` `status:"F"`), close with a write-up and evidence rather than diverging from Blink's algorithm.
- **Foundational correctness over test counts.** A smaller diff with a correct algorithm beats a smaller count with a hack. Never chase "nearly passing" tests with point fixes.
- **Test execution discipline.** While iterating, run only the 1–4 tests for the current target; defer full-suite runs to the commit checkpoint.
- **If working in a worktree**, commit only to the worktree branch. Never commit to `fix/*` or `master` from a worktree. Always commit + push the parent branch before launching worktree agents — they start from HEAD.
- **Never use `open`** to display files from agents; it disrupts the user.
