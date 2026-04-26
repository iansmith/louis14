Read CLAUDE.md, MEMORY.md, then task_plan.md §"Phase 14b" + findings.md §"Phase 14b research"
before writing any code.

State at HEAD (commit `87d06be5`, "Phase 14a: fix IFC fragmentation guard + empty-child overflow trigger").
Branch: fix/flexbox-fast. Phase 14a CLOSED; Phase 14b is the active work.
Gate baseline: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781
             · multicol 186/455 · spanner-frag 12/13

---

Phase 14b — Nested multicol leaf-frag.

Driver test (run first to confirm current state):

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-010' -v

Expected: currently failing (~3500px diff, ~0.7%).

---

MANDATORY FIRST STEP — Blink research before any code.

The bug: in `multicol-nested-010.html`, a leaf block that exceeds one inner
sub-column height is distributed across BOTH inner sub-col 1 AND sub-col 2
within the same outer column pass. Blink places the overflow portion only in
sub-col 1's continuation inside the same outer column.

Fetch and read these Blink sources before touching any louis14 code:

1. https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/column_layout_algorithm.cc
   Focus on the column loop termination logic (~lines 1080–1130): when does
   ColumnLayoutAlgorithm stop creating new inner columns for a given outer
   column pass? What signals "this outer column is full, stop here"?

2. https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/block_layout_algorithm.cc
   Focus on leaf-block overflow handling (~lines 2440–2500): how does the
   leaf fragmentation path interact with the outer fragmentainer block-size
   (not just the inner column block-size)?

Record key findings in findings.md §"Phase 14b research addendum" before
writing any code.

---

Then investigate these louis14 sites:

  pkg/layout/multicol_layout.go   — the `layoutLine` / outer column loop
  pkg/layout/block_layout.go      — the leaf-fragmentation path (lines ~947–985)

Specific questions to answer from the code + Blink research:
  a. Where does the outer `FragmentainerBlockSize` limit propagate into the
     inner multicol column layout? Does it reach the leaf-block split at all?
  b. In block_layout.go's leaf-split path, is `ConsumedBlockSize` accumulated
     across fragmentainers correctly when there is an outer fragmentainer limit?
  c. Does the outer column loop in multicol_layout.go check whether the inner
     multicol has consumed the outer column's remaining space?

---

Only after Blink research and louis14 investigation are complete:
  - Identify the SINGLE divergence point (one function, one condition).
  - Write the fix. Do not change more than necessary.
  - Run the driver test. Target: 0 pixel diff.
  - Run gate sweep (all six invariants).
  - Commit with message "Phase 14b: ..." if gate passes.
  - Update progress.md and task_plan.md.

If gate regresses: STOP, do not commit, report the regression.
Do NOT modify anything outside the identified divergence site.
