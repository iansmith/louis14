# Progress Log — css-multicol (active)

## Rules pointer

Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not restate them here.

## Tracking files

- `findings.md` — active phase briefs (Phase 21–24) + open residuals + key data-structure pointers + recent error log.
- `docs/findings-multicol-archive.md` — historical detail for Phase 12–20 (sub-phase landings, retired briefs, Blink citations, pre-Phase-21 error-log entries, full ColumnLayoutAlgorithm pseudocode).
- `task_plan.md` — Phase 21–24 plan + test command templates.
- `CONTINUE.md` — concise next-session pointer.

## Current gate (2026-04-29 — post Phase 20)

| Category | Count | Notes |
|---|---|---|
| CSS2 | **99/99** | invariant |
| css-flexbox | **626/629** | three pre-existing residuals |
| css-position | **92/105** | thirteen pre-existing residuals; no active work |
| css-writing-modes | **781/781** | closed |
| css-multicol | **211/455** | Phase 20 closed structural overflow-clip rework. |
| 13 driver invariants | **13/13** | column-height-001/010/017/026/027, multicol-nested-030/031, spanner-fragmentation-001/004/006, multicol-rule-nested-balancing-004, nested-floated-multicol-with-monolithic-child, nested-past-fragmentation-line |
| spanner-fragmentation cluster | **12/13** | -008 fails 0.2%; pre-existing |

## Phase status

### Active queue (Phase 21–25)

- **Phase 21** — Conditional `IsMulticolContainer` clip. Hard-blocked by Phase 25 (was Phase 22; revised after Phase 22 close-out — see below). Target gate: 211 → 214+. Brief: `findings.md` § Phase 21.
- **Phase 22** — Nested-multicol resume `ConsumedBlockSize` chain. **LANDING Cmt-1 only, 2026-04-30**. Cmt-1 (`2a822b9d`) wires `ConsumedBlockSize` on inner multicol's outgoing BreakToken — Blink-aligned. Multicol gate **211 → 212** (+1 reclaim of `broken-column-rule-1.html`). 4-cat invariants intact. Cmt-A (`3c17b1da`, on worktree only) was rejected after full-sweep verification: introduces a 0.1% regression on `multicol-nested-033`. The cluster-closure target was NOT met — actual fix needs fragmentation-aware OOF positioning (see Phase 25). Detail: `docs/PLAN-phase-22.md` §14.9.
- **Phase 23** — Finish FinishFragmentation port. Independent. Worktree. Target: stable or +N. Brief: `findings.md` § Phase 23.
- **Phase 24** — span-all-children-height cluster (002–013). Worktree. Target: 211 → 220+. Brief: `findings.md` § Phase 24, sub-cluster detail in archive.
- **Phase 25** — Fragmentation-aware OOF positioning (Blink-aligned port). NEW, added 2026-04-29 after Phase 22 close-out. Multi-thousand-line port of Blink's `MulticolWithPendingOOFs` / `LayoutOOFsInMulticol` pipeline. Closes `multicol-nested-011, -032`, `fill-balance-026` (OOF portion), and unblocks Phase 21. Brief: `findings.md` § Phase 25; full design: `docs/PLAN-phase-22.md` §14.5–§14.10.

### Completed phases (one-line summary)

Full per-phase commit refs and detail in `docs/findings-multicol-archive.md`.

- **css-position phases 0–11** — DONE 2026-04-21 at 92/105 (13 pre-existing residuals).
- **Phase 12 (a–h.6)** — Multicol fragmentation infra; ColumnSpannerPath; nested multicol; forced breaks; column-height/wrap; balance-break-avoidance; F1–F5 residual fixes. Multicol 94 → 188.
- **Phase 13 (a–h)** — LayoutUnit precision discipline. Closed 2026-04-26.
- **Phase 14a/b/c** — IFC fragmentation guard (+4); nested leaf-frag deferral (+2); clear-001 permanently deferred. Multicol 179 → 188.
- **Phase 15** — `containerPercentResolutionBlockSize` for multicol children. multicol-span-all-children-height-001 closed.
- **Phase 16** — Spanner BFC filtering + 16.d.1 leaf clamp + 16.d.2/3 TallestUnbreakable carrier. Multicol 188 → 192.
- **Phase 17** — Forced-break balance via Blink ContentRuns measure-pass. Multicol 192 → 196.
- **Phase 16.e+18 v2 BUNDLE** — Walker port + ClipBlockAxisOnly removal + IsMonolithic flag + contentNode pointer cache. Merge `00c0d197`. Multicol 196 → 199. Then `3389efe7` broad ClipContentToBorderBox: 199 → 205. 13 drivers 11/13 → 13/13.
- **B6 (Phase 18 ConsumedRowBlockSize WRITE-site)** — Landed `b251c8db`, gate-neutral; brief target was wrong, see archive.
- **Phase 20** — Multicol overflow clip Blink-aligned port. Worktree commits P20.1–P20.7. Multicol 205 → **211** (+6, hits brief target). 13 drivers 13/13. Six reclaims (multicol-fill-balance-032/034/035/036, multicol-span-all-margin-nested-001, inline-block-and-column-span-all). All 9 prior-clip-wins held. Brief diverged in three places (Blink's clip is conditional on user-set overflow; rect is padding-box; reclaim diff numbers were fonts-artifact stale). Three stuck tests remain (`multicol-gap-large-001`, `increase-prev-sibling-height`, `multicol-nested-029`) — all need Phase 21 + Phase 22.

## Open residuals

See `findings.md` § "Open residuals". Highlights:

- `clear-001.xht` — permanently deferred (CoreText metrics).
- `column-wrap:nowrap` overflow columns — needs paint-layer ink-overflow change.
- G-ABS-IN-TABLE / G-SEMI-REPLACED / G-SCROLL — abspos residuals; no active work.
- spanner-fragmentation-005 + -008 — pre-existing 0.2% diffs.
- css-flexbox 11a/b/c — three pre-existing failures at 626/629.

## Error log

(Format: date · symptom · root cause · fix or status. Recent entries; Phase 12–20 entries archived.)

**2026-04-29 · Phase 22 hard exit · Cmt-1 zero cluster gain · two distinct bug classes** — Cmt-1 (`2a822b9d`) wired `ConsumedBlockSize` on the inner multicol's outgoing BreakToken (Blink-aligned, gate-neutral) but closed zero of the predicted ~14 cluster tests. Two root causes uncovered: Class A (`-011`, `-032`) — Phase 14b defer fired at `FragmentainerOffset=0`, deferring fresh inner multicols indefinitely; Cmt-A (`3c17b1da`) narrowed the gate, also gate-neutral. Class B (12 auto-height tests) — the `ConsumedBlockSize` READ site is gated on `hasExplicitBlock` so auto-height inner multicols never consult it; root cause unknown. See `docs/PLAN-phase-22.md` §13.

**2026-04-29 · Phase 22 Cmt-B post-loop break guard · regressed `-032` · OOF-on-fragmentation gap** — After Cmt-A landed gate-neutral (no closures), traced `-011`/`-032` to a missing post-walker-loop check: the inner multicol exits "complete" even when the outer fragmentainer is exhausted with explicit content remaining, so no BreakToken is emitted and the outer reruns the inner from scratch in col-2. Cmt-B candidate added that guard with `mla.remainingContentBlockSize` (correct for fresh + resumed). `-011` improved 1.6% → 1.0%; `-032` regressed 3.1% → 4.2%. Cause of regression: `-032` has an abspos child relative to a `position:relative` inner block, and the break path bypasses the complete-path `OutOfFlowLayoutPart.LayoutCandidates(...)` call (`multicol_layout.go:999-1027`), bubbling raw OOF candidates upward to the wrong containing block. Cmt-B reverted. Closing `-011`/`-032` requires the full fragmentation-aware OOF port that Blink uses (`MulticolWithPendingOOFs` / `LayoutOOFsInMulticol`); promoted to **Phase 25**. See `docs/PLAN-phase-22.md` §14 for the Blink reference, the field/method inventory, and the Phase 22 close-out plan.

**2026-04-30 · Phase 22 close-out · Cmt-A rejected after full-sweep verification** — The §8.4 multicol-gate sweep + §8.5 4-cat invariant sweep (skipped during Cmt-A's targeted verification) ran 2026-04-30. After diagnosing and clearing a missing-fonts artifact (worktree `fonts/` had only `Ahem.ttf` — 180+ Liberation/Atkinson fonts were absent, masquerading as 391 writing-modes / 25 multicol regressions), the true picture: Cmt-1 + Cmt-A combined gives multicol 212 (+1 reclaim `broken-column-rule-1`, +1 reclaim `multicol-breaking-nobackground-003`) but introduces a 0.1% / 400 px regression on `multicol-nested-033`. Bisect: Cmt-1 alone passes `-033` at 0% diff and gives multicol 212 with one reclaim (`broken-column-rule-1`). Cmt-A is the source of the `-033` regression. Per CLAUDE.md §3, regression blocks merge regardless of offsetting closures. Cmt-A rejected; Cmt-1 alone is landing. Cmt-A's Phase 14b narrowing was always speculative (no specific Blink-line citation backed the `FragmentainerOffset > 0` predicate); Phase 25 will re-derive the right defer shape. Untracked `fonts/Ahem.ttf` from the repo as a hygiene fix to surface this trap earlier next time.
