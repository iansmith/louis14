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

### Active queue (Phase 21–24)

- **Phase 21** — Conditional `IsMulticolContainer` clip. Hard-blocked by Phase 22 + spanner-overflow placement. Target gate: 211 → 214+. Brief: `findings.md` § Phase 21.
- **Phase 22** — Nested-multicol resume `ConsumedBlockSize` chain. Target gate: 211 → 220+ (+9 to +15). Worktree. Brief: `findings.md` § Phase 22.
- **Phase 23** — Finish FinishFragmentation port. Independent. Worktree. Target: stable or +N. Brief: `findings.md` § Phase 23.
- **Phase 24** — span-all-children-height cluster (002–013). Worktree. Target: 211 → 220+. Brief: `findings.md` § Phase 24, sub-cluster detail in archive.

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

No new entries since Phase 20 LANDED. Full Phase 20 LANDED notes in `docs/findings-multicol-archive.md` § "Phase 20 LANDED".
