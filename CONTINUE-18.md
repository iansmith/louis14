# CONTINUE: Phase 16.e + 18 bundled v2 — Step 0 diagnostic next

## Authoritative brief

**v2 brief (Option A — clip-removal-first):** `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 (Option A — clip-removal-first, redesigned 2026-04-28)". Read the entire v2 section before any code change. v1 brief is preserved in findings.md but marked SUPERSEDED — do not implement against v1.

## State (2026-04-28)

**Worktree:** `/Users/iansmith/louis14-phase-16e-18`, branch `phase-16e-18-walker-carrier`. Clean.
- HEAD: `a8ea3adb` (Cmt 2 — walker READ + positional WRITE).
- Stash `stash@{0}`: Cmt 3 attempt (walker WRITE flat). Will be reused at B5 with reconciliation.
- Driver baseline: 11/13 (-001 0.8%, -006 1.4%).

**Mainline:** `fix/flexbox-fast` @ `443ec747`.
- Tracking commits: `66e819e3` (Cmts 1+2 done) · `d798c1a4` (Cmt 3 hard-exit) · `443ec747` (Path A spike).
- Gate (unchanged): CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol 196/455 · spanner-frag 11/13.

## v2 sequence

| # | Scope | Verification target |
|---|---|---|
| **Step 0** | DIAGNOSTIC: trace `column-height-026` break-token chain at Cmt 2 vs Spike B | concrete hypothesis about walker-flat × no-clip divergence |
| B1 | `TallestUnbreakableBlockSize` field on `LayoutResult` + `PropagateTallestUnbreakableBlockSize` on builder | 13/13 drivers, gate unchanged |
| B2 | Wire `TallestUnbreakable` propagation: `BreakBeforeChildIfNeeded` + `SetupFragmentation` + child-result propagation; populate `tallestUnbreakable` at multicol_layout.go:1601 | 13/13 drivers, gate ≥ 196 |
| B3 | Mechanical `ClipBlockAxisOnly` removal (setter + paint branch + struct fields + propagation) | ≥ 11/13 drivers (target 13/13 if B2 closes carrier gaps) |
| B4 | Re-verify walker READ (Cmt 2 already landed) | post-B3 baseline holds |
| B5 | Walker WRITE flat (Cmt 3 stash + Step 0 adjustments + B3 reconciliation) | 13/13 at 0 diff |
| B6 | Phase 18 `ConsumedRowBlockSize` carrier WRITE site | 13/13 + multicol gate 196 → 211+ |
| B7 | Drop `IsInsideColumnSpanner` clamp gate | 13/13, spanner-frag-006 holds |
| B8 | Full gate sweep + merge worktree → `fix/flexbox-fast` | gate target met |

## Next concrete action: Step 0

Step 0 is a DIAGNOSTIC, not a code commit. It runs in the worktree without committing. The output is a paragraph in findings.md (or a new note file) describing the break-token-shape divergence between Spike A (clip OFF, passing column-height-026) and Spike B (clip OFF + walker WRITE flat, failing column-height-026).

### Concrete steps

1. **Reproduce Spike B locally.** Worktree at `a8ea3adb`. Apply `git stash@{0}` (Cmt 3 attempt). Comment out `ClipBlockAxisOnly` setter at `multicol_layout.go:~1313` (line shifts post-stash). Run `column-height-026`; confirm 1.0% diff fail.

2. **Add break-token tracing.** In `multicol_layout.go`, instrument:
   - At `Layout` entry: log `mla.space.BreakToken` shape (children, ConsumedBlockSize, HasSeenAllChildren, IsInsideColumnSpanner-ness).
   - At each `outBuilder.AddBreakToken` / `AddBreakBeforeChild`: log what's being pushed.
   - At `buildOuterBreakResult`: log final outgoing break token shape.
   - At BLA's per-fragment clamp (`block_layout.go:1426-1448`): log the clamp inputs (constraint space FragmentainerBlockSize, FragmentainerOffset, BreakToken.ConsumedBlockSize) + decision.

3. **Run `column-height-026` under three configs and capture trace logs:**
   - (a) Cmt 2 baseline (clip ON, walker READ, positional WRITE) — passing.
   - (b) Spike A (Cmt 2 + clip OFF) — passing.
   - (c) Spike B (Cmt 3 + clip OFF) — failing 1.0%.

4. **Diff the traces.** Identify where (b) and (c) diverge. Likely loci:
   - `BlockBreakToken` field stripped by walker WRITE flat that 16.d.1 reads (HasSeenAllChildren, SequenceNumber, IsCausedByColumnSpanner).
   - Child-token order divergence (positional vs flat affects which child 16.d.1 inspects).
   - `flushWalker` pushes a column-content driver token whose ConsumedBlockSize accumulates wrong on resume.

5. **Document the hypothesis** — a paragraph in `findings.md` § "Step 0 diagnostic — column-height-026 trace" (new section). Include trace excerpts, the divergence locus, and how B5 should adjust the stash to fix it.

6. **Revert all instrumentation** — leave the worktree at `a8ea3adb` clean. Stash any partial diagnostics in `git stash` if needed.

### Step 0 hard exit

If after ~2 hours the divergence is not localized to a few break-token fields or a clear order issue (i.e., (b) and (c) differ in many places throughout the layout pass), STOP. v2 doesn't proceed without Step 0's hypothesis. Re-engage operator to decide whether to do Option B (walker-with-clip-shim) or pause the bundled phase.

### Why Step 0 first

v1 didn't ground its premise empirically — it assumed the WRITE flatten alone restores 13/13. Spike B refuted that. v2 mandates the diagnostic so B5's walker WRITE flat is targeted, not a re-attempt of v1 Cmt 3 with fingers crossed.

## Tracking files

- `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF v2 ..." — authoritative.
- `findings.md` § "Phase 16.e + 18 BUNDLED BRIEF (prep complete 2026-04-28) — SUPERSEDED" — v1 preserved for archaeology.
- `findings.md` § "Phase 16.d Blink research (2026-04-27)" — still current; cited by v2 B1/B2.
- `findings.md` § Error Log entries 2026-04-28 — three entries: Cmt 3 hard-exit, Path A spike, v2 redesign.
- `progress.md` § "Active phase" — points to v2.
- `task_plan.md` § "Current focus" — points to v2.

## Operational reminders

- Worktree work commits to `phase-16e-18-walker-carrier`, NOT `fix/flexbox-fast`.
- Mainline updates are docs-only.
- Always commit + push before launching any sub-agents (worktrees start from HEAD).
- CLAUDE.md rules: study Blink first, all tests at 0 diff, no chasing easy wins.
- Each B-commit verifies independently; if a B-commit's hard exit fires, STOP and engage operator. Do not pile predicates.
