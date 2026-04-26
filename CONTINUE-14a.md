Read CLAUDE.md, MEMORY.md, then task_plan.md §"Phase 14a" + findings.md §"Phase 14a research"
before writing any code.

State at HEAD (commit da58d975, "Phase 13h: verification + cleanup + retrospective").
Branch: fix/flexbox-fast. Phase 13 CLOSED; Phase 14 is the active work.

Phase 14a — IFC fragmentation guard + empty-child overflow trigger. Two code changes, one commit.

Gate baseline: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781
             · multicol 179/455 · spanner-frag 12/13

---

PART 1 — inline_layout.go:963

File: pkg/layout/inline_layout.go
Line: 963 (the `if blockOffset+lineHeight > fragEnd && blockOffset > 0 {` check)

Change the guard from:
    blockOffset > 0
to:
    bla.space.FragmentainerOffset+blockOffset > 0

This is the ONLY change in inline_layout.go. Do not touch anything else.

Rationale (from findings.md §"Phase 14a research"):
  Blink's refuse_break_before = (space_left >= fragmentainer_block_size), where
  space_left = fragSize - fragOffset. Refuses only when fragmentainer_block_offset ≤ 0
  (fragmentainer is completely empty). Louis14's blockOffset > 0 asked the wrong question
  (has THIS IFC placed any lines?) instead of "is the fragmentainer empty?"

---

PART 2 — block_layout.go

File: pkg/layout/block_layout.go
After the existing overflow check at line 895 (the `if fragSize != Indefinite && (blockCursor > fragEnd || ...` block), add a NEW independent condition that detects the "IFC produced empty fragment + break token" case.

The new block fires when:
  fragSize != Indefinite        (we're in a fragmented context)
  && childHasBreak              (child emitted a break token)
  && childBlockSize == 0        (child made zero forward progress)
  && !bla.space.IsInitialColumnBalancingPass  (not the unconstrained measure pass)

When it fires, build an outer break token (same structure as the existing outToken at line 902)
and append the child's break token to outToken.ChildBreakTokens.

Make sure the new block uses a separate `if` from the existing overflow check, and that it
does NOT fire if the existing check already fired (use an `else if` or a `handled` flag).

---

Driver tests to run before committing (CLAUDE.md §4 — max 4 tests):

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol/multicol-margin-001' -v

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol/multicol-inherit-001' -v

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol/multicol-margin-child-001' -v

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-margin-001' -v

All four should PASS at 0 pixel diff.

---

Gate sweep (run after all four drivers pass):

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css2' -v 2>&1 | tail -5

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-flexbox' -v 2>&1 | tail -5

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-position' -v 2>&1 | tail -5

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-writing-modes' -v 2>&1 | tail -5

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/css-multicol' -v 2>&1 | tail -5

  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/... \
    -run 'TestWPTCSS3Reftests/spanner-fragmentation' -v 2>&1 | tail -5

Expected: CSS2 ≥99/99, flex ≥626/629, position ≥92/105, wm ≥781/781,
          multicol ≥179/455 (should go UP by 4), spanner-frag ≥12/13.

---

After gate sweep passes, commit to fix/flexbox-fast:

  git commit -m "Phase 14a: fix IFC fragmentation guard + empty-child overflow trigger

  Part 1 (inline_layout.go:963): change blockOffset > 0 guard to
  bla.space.FragmentainerOffset+blockOffset > 0, mirroring Blink's
  refuse_break_before = (space_left >= fragmentainer_block_size) which
  refuses to break only when the fragmentainer is completely empty.

  Part 2 (block_layout.go): detect IFC-produced empty fragment + break
  token as an overflow condition, emitting an outer break token carrying
  the child's break token when childHasBreak && childBlockSize == 0.

  Closes 4 F4 regressions: multicol-inherit-001, multicol-margin-001,
  multicol-margin-child-001, multicol-nested-margin-001.

  Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"

Then update progress.md (append §"Phase 14a" with gate numbers + commit hash) and
task_plan.md (mark 14a DONE in the sub-phase table, update top summary).

---

If any gate invariant regresses: STOP, do not commit, report the regression here.
If the driver tests don't reach 0 pixel diff: investigate before committing.
Do NOT modify anything outside pkg/layout/inline_layout.go and pkg/layout/block_layout.go.
