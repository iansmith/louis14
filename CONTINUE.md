# CONTINUE: css-multicol next-step

Active operational continuation. Concise pointer for the next session.

## Where we are

Mainline `master` post-Phase-20. Multicol gate **211/455** (+6 from Phase 20). 13 driver invariants 13/13 at 0 diff. Spanner-fragmentation cluster 12/13 (-008 pre-existing). 4-cat invariants: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781.

## Active queue (Phase 21–24)

The plan post-cleanup is four sized phases. Full briefs in `findings.md` § Phase 2X.

- **Phase 21 — Conditional `IsMulticolContainer` clip** (closes the 3 Phase 20 stuck tests). Hard-blocked by Phase 22 + spanner-overflow placement, otherwise it regresses the 9 prior-clip-wins. Target +3.
- **Phase 22 — Nested-multicol resume `ConsumedBlockSize` chain** (recommended next). Closes `multicol-nested-011..032` + `multicol-fill-balance-003/-026`. First-pass attempt 2026-04-28 was non-monotonic and reverted; trace-the-three-hop recipe in the brief. Target +9 to +15. Worktree.
- **Phase 23 — Finish FinishFragmentation port.** Drop the leaf-only gate in 16.d.1 + retire the parent-side overflow path in `block_layout.go:1001-1196`. Independent of 21/22. Worktree.
- **Phase 24 — span-all-children-height cluster (002–013).** Twelve tests across seven sub-clusters (Phase 19 brief in archive). Worktree.

## Tracking files

- `findings.md` — Phase 21–24 briefs + open residuals + key data structures + recent error log.
- `progress.md` — current gate + completed phase summary + active queue.
- `task_plan.md` — Phase 21–24 plan + test command templates.
- `docs/findings-multicol-archive.md` — historical detail (Phase 12–20 sub-phase work, retired briefs, Blink citations, ColumnLayoutAlgorithm pseudocode, pre-Phase-21 error log).
- `CONTINUE-20.md` — HISTORICAL.

## Rules pointer

`CLAUDE.md` (project) + `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` (session memory). Key rules:

- Study Blink first; all tests at 0 diff; no chasing easy wins.
- High-risk multi-commit refactors → worktree.
- Mainline merges only after gate verification.
