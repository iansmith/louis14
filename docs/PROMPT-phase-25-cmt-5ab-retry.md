# P25 Cmt-5a + Cmt-5b — RETRY prompt (2026-05-01)

You are picking up a **failed previous attempt** at Cmt-5a + Cmt-5b. Read this whole file before you touch a line of code. The previous attempt was force-reverted; the throwaway branch (`phase-25-cmt-5ab`) and worktree (`/Users/iansmith/louis14-p25-cmt5ab` from that run) are gone. This is a clean retry on a fresh branch.

This is **not** "implement this scoped patch". It is "fix the underlying break-token chaining bug in nested multicol that the `contain:size` IsMonolithic deviation was masking, then remove the deviation". You are working on real load-bearing code; you must trace the chain, find where it breaks, and fix it. Speculative one-line patches are not acceptable.

---

## Setup

- **Worktree:** `/Users/iansmith/louis14-p25-cmt5ab` (already created, fonts already populated). DO NOT touch `/Users/iansmith/louis14-phase-25` (the parked Cmt-5d worktree) — that worktree stays at `e8f25761` so we can A/B against it.
- **Branch:** `phase-25-cmt-5ab-retry`, currently at `e8f25761` (Cmt-5d). Cmt-5c (drain inner trailing cols) and Cmt-5d (column-gap percent) are already on this branch. You are adding Cmt-5b, then Cmt-5a, both on top.
- **Read first, in this order:**
  1. `findings.md` § Phase 25 — full context. Especially the "Cmt-5a (`contain:size` IsMonolithic deviation) — ATTEMPTED + REVERTED" and "Cmt-5b (BlockBreakToken child-chaining) — DEFERRED" entries. Those describe what went wrong and what 5b actually means.
  2. `docs/PROMPT-phase-25-cmt-5.md` § "Cmt-5a" and § "Cmt-5b" — the original (still-correct) framing of the two fixes. The original Cmt-5b text is intentionally light on detail because the right fix is determined by tracing the chain in code; do not treat that prompt's brief paragraph as "the spec".
  3. This file (which is more concrete than the original prompt because we now have a failed attempt to learn from).

---

## Why the previous attempt was wrong (do not repeat these mistakes)

**Mistake 1 — Cmt-5a alone, without a real Cmt-5b first.** The previous Sonnet agent landed Cmt-5a (Blink-correct scoping: auto-height + floats stay monolithic; `contain:size` + explicit-height + non-float fragments). The patch itself is correct per Blink. But it regressed the multicol gate from **212/455 to 177/455** (−35 tests). Why: the `contain:size` IsMonolithic deviation was MASKING break-token chaining bugs in nested multicol. With contain:size monolithic, the per-column BLA's overflow handler emits `HasSeenAllChildren=true` (via `block_layout.go:1150` "Child completed" else branch) instead of chaining the contain:size box's own break token. This silent-completion path was hiding a real chain bug; it stopped firing when 5a removed IsMonolithic, and the chain failed visibly across 35 tests. **You must fix the chaining bug FIRST (Cmt-5b), then remove the deviation (Cmt-5a). Both must land in a single push that keeps the gate ≥ 212/455.**

**Mistake 2 — Cmt-5b implemented as `flushWalker()` cleanup.** The previous agent interpreted "Cmt-5b" as "do something to flushWalker / spanner walker entry cleanup". That is a *different mechanism*: `flushWalker` (`multicol_layout.go:548–558`) drains spanner / parent-token entries from the `MulticolPartWalker` into `outBuilder` at outer-fragmentainer break points. It mirrors `column_layout_algorithm.cc:733-738` cleanup. **It has nothing to do with in-flow descendant resume.** If your fix touches `flushWalker` or `MulticolPartWalker`, you are almost certainly off-track. Stop and re-read this section.

**Mistake 3 — One-line speculative patches.** The previous attempts (the agent's flushWalker change; an even earlier "post-loop guard" type tweak) were narrow patches written without tracing the actual data flow on the failing test. Do not write a patch until you have an end-to-end trace (debug prints work fine) showing exactly which `BlockBreakToken` is missing which `ChildBreakTokens` entry on which fragmentainer pass.

---

## What Cmt-5b actually is (in plain terms)

In a nested multicol where an in-flow descendant (e.g., a fragmentable `contain:size` box) breaks at the outer fragmentainer boundary, the descendant's continuation `BlockBreakToken` (carrying its `ConsumedBlockSize`) must thread end-to-end up the chain so the resumed inner multicol picks it back up in the next outer fragmentainer.

The conceptual chain for `multicol-nested-011.html` (the canonical failing test):

```
wrapper div (red, 100×100)
└── outer-mc  (columns:2, column-fill:auto, column-gap:0, height:50)
    └── inner-mc  (columns:2, column-fill:auto, column-gap:0, height:100)
        └── contain-size box  (width:400%, height:100, green, contain:size)
```

Outer-mc has no outer fragmentation context. Inner-mc has hasOuterFrag=true with outerAvailable=50, explicitBlockSize=100. After Cmt-5e re-applies Cmt-A (or for testing here, with Phase 14b narrowed), inner-mc must fragment across two outer columns.

**The chain that must work on the BREAK side:**

```
contain-size BLA emits result.BreakToken{ConsumedBlockSize:50}            (per inner col-1's frag boundary)
  → consumed by inner per-column BLA (block_layout.go:1086-1088)
    → outToken.ChildBreakTokens = [contain-size-token]
    → returned to inner-mla.layoutLine as colBreakToken / finalColBreakToken
      → returned to inner-mla main loop as remainingToken
        → outBuilder.AddBreakToken(remainingToken)               (multicol_layout.go:660)
          → buildOuterBreakResult: result.BreakToken.ChildBreakTokens = outBuilder.Children()
                                                                  (multicol_layout.go:519)
            → consumed by outer per-column BLA (block_layout.go:1086-1088)
              → outToken.ChildBreakTokens = [inner-mc-token]
              → returned as colBreakToken to outer-mla.layoutLine
                → outer-mla buildOuterBreakResult emits outer's outgoing token
```

**The chain that must work on the RESUME side (outer col-2 / next outer fragmentainer):**

```
outer's resumed walker reads outer-mc's incoming BreakToken
  → walker entry's BreakToken == per-column-BLA-token (containing inner-mc-token nested)
    → layoutLine called with that as nextColToken
      → per-column BLA receives it as space.BreakToken (incomingBreakToken)
        → resumeChildBreakToken = incomingBreakToken.ChildBreakTokens[0] = inner-mc-token
          → finds matching child (inner-mc) in children loop
          → csBuilder.SetBreakToken(inner-mc-token)             (block_layout.go:650)
            → inner-mla receives space.BreakToken = inner-mc-token
              → inner-mla walker: NewMulticolPartWalker(contentNode, mla.space.BreakToken)
                → walker entry's BreakToken == inner-per-column-BLA-token
                  → layoutLine for resumed inner col
                    → per-column BLA's space.BreakToken set with ChildBreakTokens=[contain-size-token-resume]
                      → resumeChildBreakToken = contain-size-token-resume
                        → contain-size BLA receives space.BreakToken with ConsumedBlockSize=50
                          → places remaining 50px of green
```

If any link in either chain drops or mis-encodes the descendant's resume info, you get the `-011` symptom: top half green in outer col-1, bottom half empty (wrapper red shows through) in outer col-2.

**Your job in Cmt-5b is to find the broken link and fix it.** The chain spans three files: `multicol_layout.go`, `block_layout.go`, `break_token.go`. The most-likely failure surfaces are (in priority order):

1. **`multicol_layout.go::buildOuterBreakResult` (line 495-536)** — emits the inner-mc's outgoing `BlockBreakToken`. Look at what's actually in `outBuilder.Children()` when called from line 660-661 (the "Phase 12c: nested multicol hit the outer fragmentainer boundary" branch). That entry needs to be a per-column BLA token whose `ChildBreakTokens` carry the in-flow descendant resume — verify it does.
2. **`multicol_layout.go::layoutLine` per-column loop (lines 1190-1290)** — `colBreakToken = result.BreakToken` (line 1280) captures the per-column BLA's outgoing token. Trace whether that token's `ChildBreakTokens` actually contains the contain-size-token under Cmt-5a-applied conditions. If the per-column BLA dropped it, the bug is upstream in `block_layout.go`.
3. **`block_layout.go` per-child overflow path (lines 1067-1161)** — when `childHasBreak` (the descendant's BLA returned a non-nil break token), line 1086-1088 appends `childResult.BreakToken` to `outToken.ChildBreakTokens`. Verify this fires on the contain-size case under Cmt-5a (with IsMonolithic removed). The leaf-vs-non-leaf branching on line 1088 (`len(child.Children()) == 0`) and the various `IsBlockSizeOverride` branches may interfere — especially the "Child completed" else branch at line 1150 if `childHasBreak` is somehow false.
4. **The READ side** — `MulticolPartWalker` on the resumed pass. If the walker's first `entry.BreakToken` for a resumed inner-mc doesn't carry the right `ChildBreakTokens`, the per-column BLA won't see the descendant resume token even if the WRITE side wrote it correctly. Less likely to be the culprit (the walker's READ path was thoroughly debugged in Phase 16.e), but worth a sanity-check trace.

**Reference Blink code for shape:**
- `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:605-714` — `LayoutChildren` loop. Pay attention to `AddBreakToken(...)` calls and to how the inner column's break token's `child_break_tokens` is assembled.
- `third_party/blink/renderer/core/layout/column_layout_algorithm.cc:733-738` — post-loop cleanup (this is what `flushWalker` mirrors; it is NOT what Cmt-5b is about).
- `third_party/blink/renderer/core/layout/fragmentation_utils.cc::FinishFragmentation` — the canonical break-token assembly for a block layout result. louis14's `block_layout.go::Layout` overflow paths (lines 730-785, 870-944, 1067-1161, 1267-1308, 1315-1361) are partial ports of this.
- `third_party/blink/renderer/core/layout/block_break_token.h:120-160` — the `child_break_tokens_` field semantics. louis14's equivalent is at `pkg/layout/break_token.go:38-46` (`ChildBreakTokens`).

Fetch the Blink source if you don't have it locally; do not rely on what you remember.

---

## What Cmt-5a is (after Cmt-5b is in place)

`pkg/layout/block_layout.go:85-87`:

```go
if bla.style != nil && bla.style.HasSizeContainment() {
    builder.SetIsMonolithic(true)
}
```

This deviation does NOT match Blink. Per `third_party/blink/renderer/core/layout/fragmentation_utils.cc::SetupFragmentBuilderForFragmentation`, `IsMonolithic` is set based on `IsBlockFragmentationForcedOff` (overflow:scroll/auto/clip on the box, replaced content, etc.), NOT on `contain:size`. CSS Containment 2 §2.6 says contain:size eliminates intrinsic-size contribution from descendants, but it does NOT make the box monolithic for fragmentation purposes — a contain:size box with explicit height fragments normally.

The previous agent's 5a patch already had the right scoping (auto-height stays monolithic because intrinsic-block-size is needed for layout shape; `contain:size` + explicit-block-size + non-float becomes fragmentable). You may use a similar shape, but verify against Blink: the truly Blink-correct behavior is to NOT special-case contain:size at all and let normal fragmentation rules apply. Auto-height contain:size resolves to 0 intrinsic block-size (per CSS Containment 2), which already makes it trivially "fragmentable but with no content" — so the IsMonolithic flag may be entirely unneeded.

If you find the unconditional removal regresses tests that 5b doesn't cover, that's a signal that ANOTHER code path is depending on the deviation. Trace that path; fix it; do not preserve the deviation as a workaround.

---

## How to verify the chain is broken (before fixing)

1. Apply Cmt-A locally on the worktree (TEMPORARY — revert before commit). Cmt-A narrows Phase 14b to `FragmentainerOffset > 0`. Without this narrow, inner-mc on `-011` is deferred via Phase 14b and never fragments, so you can't observe the chain bug. The narrow is at `multicol_layout.go:413` — change `outerAvailable < explicitBlockSize` to `outerAvailable < explicitBlockSize && mla.space.FragmentainerOffset > 0`.

2. Run `multicol-nested-011.html`:
   ```bash
   GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
     -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
     -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-011\.html$' -v
   ```
   Confirm 5000 px diff (1.0%). If you see 0/0 already, the chain is working under Cmt-A alone — re-check that Cmt-A actually applied.

3. Add temporary debug logging at the four likely-failure surfaces:
   - `block_layout.go:1086` (after the `outToken.ChildBreakTokens = append(...)` line): print `child.DebugName()`, `childResult.BreakToken.ConsumedBlockSize`, and `len(childResult.BreakToken.ChildBreakTokens)`.
   - `multicol_layout.go:660` (the `outBuilder.AddBreakToken(remainingToken)` for the nested-multicol-overflow branch): print `mla.node.DebugName()`, `remainingToken.ConsumedBlockSize`, `len(remainingToken.ChildBreakTokens)`.
   - `multicol_layout.go:519` (in `buildOuterBreakResult`, after `result.BreakToken = ...`): print `mla.node.DebugName()`, count of `outBuilder.Children()`, and for each child print `Node.DebugName()` + `ConsumedBlockSize` + `len(ChildBreakTokens)`.
   - `multicol_layout.go::Layout` entry (line ~290 area): when `mla.space.BreakToken != nil`, print the incoming token's full structure (Node, ConsumedBlockSize, ChildBreakTokens recursively).

4. Re-run `-011` (with Cmt-A still applied locally) AND temporarily toggle `IsMonolithic` off via Cmt-5a. The trace should show:
   - Outer col-1: contain-size BLA emits a break token with `ConsumedBlockSize=50` (or similar). Per-column BLA chains it. Inner-mc's `buildOuterBreakResult` emits a token whose `outBuilder.Children()[0].ChildBreakTokens[0]` is the contain-size-token-resume.
   - Outer col-2: outer-mla's incoming BreakToken (printed at Layout entry) has the same nested structure. If it doesn't, the WRITE side dropped it (between outer col-1 emit and outer col-2 read). If it does, the bug is on the resume-side READ — somewhere between walker construction and contain-size BLA's `incomingBreakToken`.

5. The trace will tell you exactly which file/line dropped the token. **Fix at the dropping site, not somewhere upstream or downstream.**

6. Remove debug logging before committing.

---

## Worktree commit order

You may not need to land commits in the order below — what matters is that the **final pushed state** has both 5b and 5a, and the validation gate is green. But sequencing them as separate commits (with each commit's message containing before/after gate numbers) makes review and bisection trivial if something later regresses.

1. **Cmt-5b** — break-token chaining fix. Commit message must:
   - Cite the Blink reference for the chained behavior.
   - State the exact code site(s) changed and what data was being dropped before.
   - Include before/after for `multicol-nested-011.html` UNDER Cmt-A (apply Cmt-A locally, run, revert Cmt-A, then commit). This is your proof that 5b actually fixes the chain.
   - Confirm gate-neutrality WITHOUT Cmt-A: drivers 13/13, prior-clip-wins 9/9, multicol gate ≥ 212/455. (If you broke something incidentally, fix or revert before committing.)

2. **Cmt-5a** — `contain:size` IsMonolithic deviation removal. Commit message must:
   - Cite `fragmentation_utils.cc::SetupFragmentBuilderForFragmentation` as the Blink reference.
   - Report the exact change (line range deleted/modified).
   - Include the multicol gate count: must be **≥ 212/455** post-5b+5a. If the previous attempt's 35-test regression still occurs, 5b didn't cover all chain sites — go back to step 1 and trace the specific failing tests.
   - Drivers 13/13, prior-clip-wins 9/9. No invariant regressions.

3. **(Optional, if both above land cleanly) Cmt-5e** — narrow Phase 14b to `FragmentainerOffset > 0`. This is the closure flip that makes `-011`/`-032`/`-033` actually go to 0/0 in the test gate. Land as its own commit with the targeted-tests gate showing closure. If 5b+5a are not solid first, 5e WILL regress the prior-clip-wins.

After each successful commit, push to `origin/phase-25-cmt-5ab-retry`.

---

## Validation harness

Run these against the worktree (not the main dir):

```bash
# Targeted closure (must close after Cmt-5e)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/multicol-nested-(010|011|032|033)\.html$'

# 13 driver invariants (must hold 13/13 throughout)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031|032|033)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins (must hold 9/9 throughout)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Full multicol gate — REQUIRED before declaring 5b+5a done.
# Must be >= 212/455. The previous 5a attempt regressed to 177/455; that is
# the regression you are preventing.
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/' 2>&1 | tail -5

# 4-cat invariants (must hold post-Cmt-5e)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS21Reftests/' 2>&1 | tail -3
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test -count=1 \
  -C /Users/iansmith/louis14-p25-cmt5ab ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-(flexbox|position|writing-modes)/' 2>&1 | tail -5
```

Per CLAUDE.md §4, run targeted tests during feature work; the full multicol sweep is the **acceptance gate**, not a debugging tool. If the targeted gate is green but the full sweep drops below 212, identify which specific tests regressed and trace each one — do not write speculative patches "to handle the new failures".

---

## Hard exit conditions

Stop and report rather than continuing if any of these occur:

1. **You can't reproduce the 5000 px on `-011` under Cmt-A.** Means Cmt-A isn't applied correctly, or something else changed under your feet. Re-set up before doing anything else.
2. **Your trace shows the chain is intact end-to-end** but `-011` still fails. Means the bug is somewhere else (e.g., in the per-column BLA's space construction, or in the fragmentainer-offset accumulation across outer cols). Document what you found and ask before guessing.
3. **Cmt-5b lands clean (gate-neutral) but Cmt-5a still regresses ≥ 5 tests.** Means there are MORE chain sites needing fixing — the contain:size resume probably uses additional code paths that 5b didn't cover (e.g., line-overflow vs block-overflow, replaced-element-with-contain:size, contain:size-inside-flex). Identify the specific failing tests, group them by failure shape, and ask before extending 5b.
4. **Drivers regress.** Drivers are infrastructure; their regression means a structural assumption is broken. Stop and report.
5. **Prior-clip-wins regress.** They depend on the unconditional clip masking layout bugs; in this phase they should NOT change because Phase 21 hasn't gated the clip yet. If they regress, your changes broke layout shape (not just clipping). Stop.
6. **You find yourself touching `flushWalker`, `MulticolPartWalker`, `MulticolBreakTokenBuilder`, `IsBlockFragmentationContextRoot`, or `oof_fragmentation_drain.go`.** None of those are Cmt-5b. Stop and re-read "Why the previous attempt was wrong" above.

---

## After you finish

1. Push `phase-25-cmt-5ab-retry` to origin.
2. Update `findings.md` § Phase 25 Cmt-5 entry on the umbrella branch (`multicol-phase-21-24` in `/Users/iansmith/louis14`):
   - Mark Cmt-5a/5b as DONE with their commit shas.
   - Mark Cmt-5e as DONE if you landed it; otherwise leave DEFERRED and explain why.
   - Update the multicol gate count.
   - Move the Phase 25 PARK note to the past tense.
3. Report back with:
   - The bisected root cause (which file:line dropped which break-token field).
   - The fix shape (what you changed, in 2-3 sentences per commit).
   - The gate numbers (drivers, prior-clip-wins, full multicol, 4-cat invariants).
   - Anything that surprised you — code paths the previous fix attempt should have known about but didn't.

---

## Quick references

| Item | Path |
|---|---|
| `contain:size` IsMonolithic site (Cmt-5a target) | `pkg/layout/block_layout.go:85-87` |
| Per-column BLA child overflow handler | `pkg/layout/block_layout.go:1057-1361` (the big `if bla.space.HasBlockFragmentation` block) |
| Per-column BLA chain site (where in-flow child break propagates) | `pkg/layout/block_layout.go:1086-1088` |
| `MulticolLayoutAlgorithm.layoutLine` per-column loop | `pkg/layout/multicol_layout.go:1175-1290` |
| `MulticolLayoutAlgorithm` outer walker loop | `pkg/layout/multicol_layout.go:560-940` |
| Nested-multicol outer-fragmentation break branch | `pkg/layout/multicol_layout.go:658-661` |
| `buildOuterBreakResult` | `pkg/layout/multicol_layout.go:495-536` |
| `flushWalker` (NOT a Cmt-5b target) | `pkg/layout/multicol_layout.go:548-558` |
| `BlockBreakToken` definition | `pkg/layout/break_token.go:27-91` |
| Phase 14b defer (Cmt-5e target) | `pkg/layout/multicol_layout.go:413-432` |
| Cmt-A original (rejected on Phase 22) | commit `3c17b1da` on `multicol-phase-22` |
| Cmt-4 (post-loop break guard) | commit `e84d3ece` on `phase-25-oof-fragmentation` |
| Cmt-5c (drain inner trailing cols) | commit `7dc344b4` on `phase-25-oof-fragmentation` |
| Cmt-5d (column-gap percent) | commit `e8f25761` on `phase-25-oof-fragmentation` |
| Test source — `multicol-nested-011.html` | `pkg/visualtest/testdata/wpt-css3/css-multicol/multicol-nested-011.html` |
| Test source — `multicol-nested-032.html` | `pkg/visualtest/testdata/wpt-css3/css-multicol/multicol-nested-032.html` |
| Diff PNG output | `output/reftests/multicol-nested-011_diff.png` etc. (generated on test run) |

Project rules (CLAUDE.md §1–§5) override anything ambiguous in this prompt.
