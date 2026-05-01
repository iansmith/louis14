# Phase 22 implementation — continuation prompt for Sonnet

You are picking up Phase 22 of the louis14 multicol track. The full implementation plan has already been written: read it first.

```
/Users/iansmith/louis14/docs/PLAN-phase-22.md
```

That document is your spec. Do not redesign it. Your job is to execute it.

## Required reading order (do this before touching any code)

1. `/Users/iansmith/louis14/CLAUDE.md` — non-negotiable project rules.
2. `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` — session memory.
3. `/Users/iansmith/louis14/docs/PLAN-phase-22.md` — the Phase 22 plan (440 lines). Read all of it.
4. The current state of `pkg/layout/multicol_layout.go:280-540` (the change region) and `pkg/layout/block_layout.go:1750-1775` (the canonical pattern you are mirroring).

If anything in the plan disagrees with what you find in the code now, STOP and report — don't paper over the divergence.

## What you are doing

The plan's §3.1 ("Cmt-1") is the primary change: a six-line edit at `pkg/layout/multicol_layout.go:502-506` that adds `ConsumedBlockSize` and `SequenceNumber` to the outgoing `BlockBreakToken` constructed in `buildOuterBreakResult`. This mirrors `block_layout.go:1761-1766` and Blink's `FinishFragmentation` primary clause.

Cmt-2 is an audit (no edit). Cmt-3/4/5 are conditional contingencies — only invoke them if Cmt-1 leaves residuals, and only after reading their conditions in §3 and §9 of the plan.

## Workflow (per CLAUDE.md §5)

This is a worktree task. Mainline is `master`; current branch is `multicol-phase-21-24`. The worktree starts from HEAD; commit-and-push **before** creating any agents.

```bash
git worktree add ../louis14-phase-22 -b multicol-phase-22
cd ../louis14-phase-22
```

All work happens in `../louis14-phase-22`. Commit ONLY to the `multicol-phase-22` branch from inside the worktree. Never commit to `master` or `multicol-phase-21-24` from a worktree. Commit + report at each milestone (per CLAUDE.md §5), not just at the end.

## Step-by-step (follow exactly)

### Step 0 — pre-flight gate (capture baseline first, before editing)

Run all three commands from PLAN-phase-22.md §8.1:
- 13 driver invariants (must show 13/13).
- 9 Phase-21 prior-clip-wins (must show 9/9).
- Phase 22 cluster of 22 tests (expected: 7 pass, 15 fail per the plan's §4 table).

Save the output. If the driver count or prior-clip-wins count is different from §8.1's expectation, STOP and report — your baseline differs from the plan's baseline, and that needs to be reconciled before changing code.

### Step 1 — apply Cmt-1

Edit `pkg/layout/multicol_layout.go:502-506` exactly per PLAN-phase-22.md §3.1. The replacement is roughly nine lines (six declarations + the existing struct fields). Do not change anything else.

Build:
```bash
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...
```

Build must be clean. If it isn't, fix the build error before continuing.

### Step 2 — Cmt-1 targeted re-run

Re-run the three commands from §8.1 against the worktree.

Expected outcomes (from §8.3):
- 13 drivers: 13/13 at 0 diff. **Regression → STOP, revert, report.**
- 9 prior-clip-wins: 9/9 at 0 diff. **Regression → STOP.**
- Phase 22 cluster: +12 to +14 close (`-011, -013, -014, -016, -017, -018, -019, -020, -022, -023, -024, -027, -032, fill-balance-026`). `-029` may or may not close — don't claim it.

Commit Cmt-1 at this milestone. Commit message body should describe: which tests closed, which held, gate movement on the targeted set. Report back to me with that data before continuing.

### Step 3 — Cmt-2 audit (no edit)

Verify every container-level `BlockBreakToken` emission in `multicol_layout.go` routes through `buildOuterBreakResult`. PLAN-phase-22.md §3.3 lists the four sites to check (Phase 18 row-phase, spanner content-overflow, spanner clip-overflow, forced-break-after-spanner). If any path constructs an outgoing `&BlockBreakToken{...}` without going through the closure, **stop and report** — the plan's §3.1 then needs to extend to that site too.

### Step 4 — Cmt-3/4/5 (conditional)

Only if Cmt-1 leaves Phase-22-cluster residuals. Decide between:
- §3.4 — narrow the Phase 14b defer gate (do NOT re-apply the 2026-04-28 attempt verbatim — see §3.4).
- §3.5 — port the `max(final_block_size, space_left)` clause from Blink's `FinishFragmentation` resume-variant.

Make the decision per §3 of the plan. Report which path you chose and why before applying.

### Step 5 — full multicol gate sweep

PLAN-phase-22.md §8.4. Examine every unexpected delta individually. Goal floor: 220.

### Step 6 — 4-cat invariant sweep

PLAN-phase-22.md §8.5. Any change in any of CSS2 99/99, flex 626/629, position 92/105, wm 781/781 → STOP, report.

### Step 7 — merge to master

Per PLAN-phase-22.md §10. Use the commit-message draft as the starting point, but rewrite the test list to reflect the actual outcomes you observed. Do NOT push to master without my explicit go-ahead.

## Hard exits (PLAN-phase-22.md §9)

These are "stop and discuss with the user" conditions, not "work around" conditions:
- Any of the 13 drivers fails.
- Any of the 9 prior-clip-wins regresses.
- Any 4-cat invariant moves.
- Spanner-fragmentation cluster drops below 12/13.
- Cluster gain after Cmt-1 < +9 (the brief target was +9 to +15; falling short means more than the WRITE site is broken — diagnose with traces, don't speculate).

When a hard exit fires: do NOT bypass with a point fix or filter. Per CLAUDE.md §1, that's the wrong move. Revert to a clean checkpoint, capture the symptom precisely, and report.

## Test commands cheat-sheet

All commands run from the worktree root (`../louis14-phase-22`). Always use the project's pinned toolchain.

```bash
# 13 drivers
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 prior-clip-wins
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Phase 22 cluster (22 tests; 7 pass / 15 fail at baseline)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-nested-(011|012|013|014|015|016|017|018|019|020|021|022|023|024|025|026|027|028|029|030|031|032)|multicol-fill-balance-(003|026))\.(html|xht)$'

# Full multicol sweep (211 baseline, target ≥220 post-fix)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol' 2>&1 | tail -50

# 4-cat invariants
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests'                        # 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-flexbox'        # 626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-position'       # 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes'  # 781/781
```

Per CLAUDE.md §4: do not run the full suite during feature work; run only the test sets the plan calls for at each step.

## Reporting format

After each milestone (Cmt-1, Cmt-2 audit, Cmt-3 if any, full multicol sweep, 4-cat sweep), report back with:

1. The test outcome counts (drivers · prior-clip-wins · cluster · multicol gate · 4-cat).
2. Specific tests that moved (closed / regressed) with diff sizes.
3. The commit SHA on the `multicol-phase-22` branch.
4. Any deviation from the plan and why.

Don't bundle multiple milestones into one report — that loses the per-step verifiability the plan was designed for.

## Anti-patterns to avoid (CLAUDE.md highlights)

- No "near-passing" filtering. If a fix doesn't generalize to all cases in the cluster, it's the wrong fix.
- No dismissing failures as anti-aliasing or font rendering. WPT tests have built-in fuzzy tolerances.
- No `--no-verify` to bypass hooks.
- No `git reset --hard` to "clean up" without confirming with me first.
- No `open` commands to display files.
- Do not skip the audit step (Cmt-2) — it's there because the plan's primary edit is at one site, but related sites must agree.

## When you are done

Stop after Step 6 (4-cat sweep clean). Do NOT merge to master without my explicit go-ahead. Phase 21 is hard-blocked by Phase 22 — the Phase 21 plan reruns the 9 prior-clip-wins assuming Phase 22 made them pass without the clip; that re-verification is part of Phase 21, not Phase 22.

Begin with Step 0.
