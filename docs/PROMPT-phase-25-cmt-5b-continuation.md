# P25 Cmt-5b — CONTINUATION prompt (2026-05-01)

You are picking up a **partially-investigated, partially-failed** Cmt-5b. The prior Sonnet sub-agent's Cmt-5b was REVERTED (commit `dcd6f40d` reverts `57bb8967` on `phase-25-cmt-5ab-retry`). Cmt-5a (`1ca0904d`) stays. The actual chain bug is still open. Read this whole file, plus the originals it references, before touching code.

---

## Background — what already happened

1. **Cmt-5a (`1ca0904d`) landed cleanly.** Float-only narrowing of `contain:size` IsMonolithic in `pkg/layout/block_layout.go:94-97`. Gate 212→214/455. Stays.

2. **Sonnet sub-agent's Cmt-5b (`57bb8967`) was wrong, was reverted (`dcd6f40d`).** The agent set `HasSeenAllChildren=true` when `outBuilder.IsEmpty()` inside `buildOuterBreakResult` (`pkg/layout/multicol_layout.go:533`). Two fatal problems were uncovered post-landing:

   - **Skipped the prompt's mandatory Cmt-A verification.** The original prompt (`docs/PROMPT-phase-25-cmt-5ab-retry.md`) required: *"Include before/after for `multicol-nested-011.html` UNDER Cmt-A — this is your proof that 5b actually fixes the chain."* The agent's commit message literally said "No Cmt-A applied". Direct verification: under Cmt-A applied, `-011` still failed at 5000 px / 1.0% — **the exact pre-fix symptom from the prompt**.

   - **Bisect proved Cmt-5b actively regresses `-032` under Cmt-A.** With Cmt-5a alone + Cmt-A → `-032` PASSES. With Cmt-5a + 5b + Cmt-A → `-032` fails at 5000 px / 1.0%. So when Cmt-5e (Cmt-A permanent) eventually lands, the agent's commit would have turned `-032` from pass→fail. The "+2 gate" headline masked a regression that's load-bearing for the next phase.

3. **The agent's diagnosis was related but NOT the chain bug.** The agent identified a real "post-loop guard with empty outBuilder re-runs content" issue — which IS a real bug — but it's a separate symptom from the descendant break-token chaining the prompt described. Both bugs may be relevant; only the chain bug closes `-011` under Cmt-A.

---

## What the trace actually shows (2026-05-01 investigation)

I instrumented the four prompt-recommended chain surfaces and ran `multicol-nested-011.html` under Cmt-A (Phase 14b narrowed to `FragmentainerOffset > 0` at `multicol_layout.go:424`). Trace evidence is preserved in two places:

- `docs/cmt5b-trace-stash.patch` — the trace+CmtA diff (apply with `git apply` from worktree root if you want to re-run it).
- This document, summarized below.

### Test markup (`multicol-nested-011.html`)

```html
<div style="width:100px; height:100px; background:red;">                    <!-- wrapper -->
<div style="columns:2; column-fill:auto; column-gap:0; height:50px;">       <!-- outer-mc -->
  <div style="columns:2; column-fill:auto; column-gap:0; height:100px;">    <!-- inner-mc -->
    <div style="contain:size; width:400%; height:100px; background:green;"></div>
  </div>
</div>
</div>
```

### Diagnostic state under Cmt-A (with Cmt-5a only, no Cmt-5b)

```
[T4.mla.entry] outer-mc fresh, fragOff=0, hasFrag=false
[T4.mla.entry] inner-mc fresh, fragOff=0, hasFrag=true                     (in outer col-1)
[T1.chain.bla] inner per-col BLA chains contain-size: BT{consumed=3200/64=50px, children=0}
[T3.buildOuterBreak] inner-mc emits outgoing: BT{consumed=3200, children=0}    ← BUG: empty children
[T1.chain.bla] outer per-col BLA chains inner-mc-tok: BT{consumed=3200, children=0}
[T4.mla.entry] inner-mc resumes with that empty token (in outer col-2)
[T1.chain.bla] inner per-col BLA chains contain-size AGAIN: BT{consumed=3200, children=0}
                                                          ← contain-size re-runs from scratch
```

### Visual outcome under Cmt-A

- TEST: top half of wrapper (50×100) GREEN, bottom half (50×100) RED. 5000 px diff.
- REF (`ref-filled-green-100px-square.xht`): 100×100 fully GREEN.
- See `output/reftests/multicol-nested-011_{test,ref,diff}.png` after running the test.

### Bisect summary (under Cmt-A applied locally)

| State                          | -011         | -032         | -033 |
|--------------------------------|--------------|--------------|------|
| Cmt-5a only                    | FAIL 5000 px | **PASS 0/0** | PASS |
| Cmt-5a + agent's 5b            | FAIL 5000 px | FAIL 5000 px | PASS |
| (deviation restored, no 5a)    | FAIL 5000 px | (untested)   | (?)  |
| (deviation restored, no Cmt-A) | FAIL 10000 px (full wrapper) | FAIL 10000 px | (?) |

Key implications:
- Agent's 5b doesn't fix `-011`.
- Agent's 5b regresses `-032`.
- Even with the `contain:size` deviation re-enabled, `-011` STILL fails 5000 px under Cmt-A. The deviation alone is not the (whole) fix either.

---

## What the chain bug actually is (revised understanding)

The prompt described a chain in which contain-size's `BlockBreakToken` threads end-to-end up the inner-mc → outer-mc stack:

```
contain-size BLA emits BreakToken{ConsumedBlockSize:50}        (per inner col-1's frag boundary)
  → consumed by inner per-column BLA (block_layout.go:1101)
    → outToken.ChildBreakTokens = [contain-size-token]
    → returned to inner-mla.layoutLine as colBreakToken / finalColBreakToken
      → returned to inner-mla main loop as remainingToken
        → outBuilder.AddBreakToken(remainingToken)            (multicol_layout.go:669)
```

What the trace shows happens in louis14:

1. Inner col-1 of inner-mc (50 tall, capped by outer): per-column BLA correctly chains contain-size's BreakToken{consumed=50}. ✓
2. Inner col-2 of inner-mc (50 tall): per-column BLA receives col-1's outToken with the chained contain-size resume. It lays out contain-size from consumed=50, places remaining 50, **completes cleanly with BreakToken=nil**.
3. `layoutLine` sees `colBreakToken=nil` → breaks out of the inner-col loop. `finalColBreakToken=nil`. Returns `remainingToken=nil`.
4. Inner-mc's main loop: `remainingToken==nil` → does NOT enter the line-669 `outBuilder.AddBreakToken` branch. Walker exhausts.
5. Cmt-4 post-loop guard at `multicol_layout.go:1096` fires (`blockCursor=50 >= outerAvailable=50` AND `blockCursor=50 < remainingContentBlockSize=100`). Calls `buildOuterBreakResult` with **empty outBuilder**. Emits `BT{consumed=50, ChildBreakTokens=[], HasSeenAllChildren=false}`.
6. In outer col-2, inner-mc resumes. Walker sees the empty token without `HasSeenAllChildren` → starts iterating in-flow children from scratch. Re-runs contain-size, re-paints top 50 instead of bottom 50.

**So per louis14's actual layout flow, contain-size COMPLETES inside outer col-1 (across 2 inner cols), and there is no mid-inflight chain to forward.** The prompt's chain description doesn't fully match what louis14 actually does — louis14's inner col-2 absorbs contain-size's resume cleanly, leaving inner-mc with no in-flow continuation needed.

**The visual symptom** (top-half-only green) suggests the EXPECTED Blink behavior here is different from louis14's behavior. Possibilities:

- **(A)** Blink actually treats contain-size as monolithic in this context (contradicting the prompt's claim that contain:size + explicit-height fragments normally), so contain-size paints 200×100 at inner-col-1 origin, overflowing visibly into wrapper's bottom half.
- **(B)** Blink's inner column geometry is different — e.g., inner col-2 has 0 block-space because col-1 wasn't done, so contain-size doesn't get absorbed in inner col-2; it forces a chain forward to outer col-2.
- **(C)** Some other mechanism (overflow propagation, BlockSizeForFragmentation) makes contain-size's fragments paint at the box's full declared height even when the fragment slice is smaller.

**Path (B) matches the prompt's described chain.** If Blink does NOT lay out inner col-2 when col-1 still has unfinished content (i.e., column-fill:auto means "fill col-1 completely BEFORE col-2"), then in outer col-1 only inner col-1 gets used, contain-size breaks at the col-1 boundary, and `remainingToken` is non-nil → propagates correctly. In outer col-2, inner col-1 fragment-2 paints contain-size's bottom 50.

If (B) is correct, the actual chain bug is in louis14's `MulticolLayoutAlgorithm.layoutLine` inner-column loop: it iterates ALL `numCols` regardless, but should stop early when col-1 still has more content to chain at the OUTER boundary.

**Verify (B) before patching.** Look at Blink's `column_layout_algorithm.cc` per-column loop — specifically how it handles a col-1 break that exceeds the outer fragmentainer. Does Blink let col-2 absorb the resume, or does it stop and propagate to outer?

Reference Blink files (fetch if not local):
- `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:605-714` — `LayoutChildren` loop.
- `third_party/blink/renderer/core/layout/fragmentation_utils.cc::FinishFragmentation` — break-token assembly.
- `third_party/blink/renderer/core/layout/block_break_token.h:120-160` — `child_break_tokens_` semantics.

---

## Working theory of the right fix

Based on the trace + bisect, my best current hypothesis is:

**In a NESTED multicol where the inner-mc itself fragments at the outer fragmentainer boundary, `layoutLine` should STOP iterating inner cols once an outer-bound chain is established.** Specifically: if col-1's per-column BLA returned a non-nil break token AND `hasOuterFrag` AND the outer fragmentainer is exhausted, the inner-mc should propagate that break upward as `remainingToken` rather than letting col-2 absorb the resume.

**This may NOT be what the original prompt described as Cmt-5b.** The original prompt assumed the chain is dropping somewhere. The trace shows the chain is being CORRECTLY ABSORBED at the wrong layer (inner col-2 instead of propagating up). Either could be considered "the chain bug", but the fix shape is different.

**Hypothesis to validate:** Add a guard in `multicol_layout.go::layoutLine` that, when `hasOuterFrag` AND the inner-mc is itself at an outer fragmentainer boundary, stops the inner-col loop after col-1 if col-1 returned a break token. Propagate that break token up through `finalColBreakToken` / `remainingToken`. The MLA main loop's existing line-669 `outBuilder.AddBreakToken(remainingToken)` then chains correctly.

**Risks of this hypothesis:**
- May regress tests where inner col-2 is supposed to absorb intra-row continuations (e.g., when the inner-mc has more content than fits in col-1 alone but does NOT fragment outward).
- Need to distinguish "outer-bound" vs "inner-only" fragmentation. The condition is probably `hasOuterFrag && outer-fragmentainer-is-exhausted-after-col-1`.

**Counter-hypothesis worth keeping in mind:** maybe the rendering bug is in PAINT (overflow propagation), not LAYOUT. If contain-size's painted fragments are supposed to extend past the fragment box's block boundary in louis14 but don't, that's a paint-side fix, not a layout-side fix. Check `paint/` and `BoxFragmentPainter` for fragment-overflow handling before locking in a layout-side patch.

---

## Setup

- **Worktree:** `/Users/iansmith/louis14-p25-cmt5ab` (still live; fonts populated). Do NOT touch `/Users/iansmith/louis14-phase-25` (parked at `e8f25761` for A/B).
- **Branch:** `phase-25-cmt-5ab-retry`, currently at `dcd6f40d` (Revert Cmt-5b). Cmt-5a is in. Cmt-5b is OUT.
- **Read order:**
  1. This file in full.
  2. `docs/PROMPT-phase-25-cmt-5ab-retry.md` — the original prompt. Still mostly correct, but read it knowing the agent already failed once and the chain story may be subtler than that prompt described.
  3. `findings.md` § "Phase 25 — Fragmentation-aware OOF positioning" — current status.
  4. `docs/cmt5b-trace-stash.patch` — the trace instrumentation I used. You can reapply it to re-run the trace.

---

## Workflow recommendation

1. **Re-establish the repro.** Apply Cmt-A (`pkg/layout/multicol_layout.go:424`, add `&& mla.space.FragmentainerOffset > 0`) on the worktree. Run `multicol-nested-011`. Confirm 5000 px / 1.0%.

2. **Reapply the trace.** `git apply docs/cmt5b-trace-stash.patch` from the worktree root, OR just reproduce the four `fmt.Printf` sites described above. Confirm the trace matches the diagnostic state in this file.

3. **Validate hypothesis (B) before patching.** Look at Blink's `column_layout_algorithm.cc:605-714`. Specifically: when a per-column BLA returns a break token AND the outer fragmentainer is exhausted, does Blink stop the inner-col loop, or let inner col-2 absorb the resume?

4. **Patch at the layer the trace identifies.** Don't patch `buildOuterBreakResult` again — that's downstream of the actual bug. The bug is upstream, in `layoutLine`'s inner-col loop OR in the per-column BLA's overflow handling, depending on what (B) reveals.

5. **Verify both directions:**
   - Under Cmt-A: `-011` MUST close from 5000→0; `-032` MUST stay 0/0; `-033` and `-010` MUST stay 0/0.
   - Without Cmt-A: gate ≥ 214/455 (don't lose Cmt-5a's gain); drivers 14/15 (`-032` pre-existing); prior-clip-wins 9/9.

6. **Strip trace, revert Cmt-A, commit, push.** Commit message MUST cite Blink reference, identify the dropping site, include before/after under Cmt-A, AND state the bisect against the agent's reverted approach to prevent the same mistake.

---

## Hard exit conditions (revised)

- **Trace doesn't match what's documented above.** Something changed under your feet — re-bisect before continuing.
- **Hypothesis (B) doesn't hold against Blink.** The fix shape is wrong; re-read Blink's column loop carefully and revise.
- **`-032` regresses.** Same trap as the agent fell into. Stop and re-bisect.
- **Drivers regress.** Drivers are infrastructure; their regression means a structural assumption broke. Stop.
- **Prior-clip-wins regress.** Layout shape changed beyond clip masking — investigate before any further commits.
- **You find yourself touching `flushWalker`, `MulticolPartWalker`, `MulticolBreakTokenBuilder`, `IsBlockFragmentationContextRoot`, or `oof_fragmentation_drain.go`.** Same as the original prompt — those are NOT Cmt-5b.

---

## After you finish

1. Push `phase-25-cmt-5ab-retry` to origin.
2. Update `findings.md` § Phase 25 entry on the umbrella branch (`multicol-phase-21-24` in `/Users/iansmith/louis14`):
   - Mark Cmt-5b DONE with the new commit sha.
   - Update the multicol gate count.
3. Report back:
   - Bisected root cause (file:line of the actual dropping/absorbing site).
   - Bisect results under Cmt-A: `-011`/`-032`/`-033`/`-010` before/after.
   - Gate numbers without Cmt-A.
   - What surprised you — anything in the actual flow that contradicts the prompt's described chain (the prompt may be slightly inaccurate about WHERE the chain breaks).

---

## Quick references

| Item | Path |
|---|---|
| Reverted Cmt-5b (DO NOT REVIVE THIS APPROACH) | commit `57bb8967` (reverted by `dcd6f40d`) |
| Cmt-5a (KEEP) | commit `1ca0904d`, `block_layout.go:94-97` |
| Cmt-A site (Phase 14b narrow) | `multicol_layout.go:424` |
| Cmt-4 post-loop guard | `multicol_layout.go:1094-1097` |
| Inner-col loop in layoutLine | `multicol_layout.go:1276-1394` (esp. lines 1366, 1379, 1387) |
| Per-column BLA chain site | `block_layout.go:1099-1101` |
| Nested-overflow propagation | `multicol_layout.go:668-673` |
| `buildOuterBreakResult` | `multicol_layout.go:496-547` |
| Test source — `multicol-nested-011.html` | `pkg/visualtest/testdata/wpt-css3/css-multicol/multicol-nested-011.html` |
| Diff outputs | `output/reftests/multicol-nested-011_{test,ref,diff}.png` |
| Trace stash | `docs/cmt5b-trace-stash.patch` |
| Original prompt | `docs/PROMPT-phase-25-cmt-5ab-retry.md` |

Project rules (CLAUDE.md §1–§5) override anything ambiguous in this prompt.
