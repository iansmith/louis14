# Task Plan — css-multicol (active)

## Rules pointer

Project rules live in `/Users/iansmith/louis14/CLAUDE.md` and auto-memory at `/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`. Do not restate them here.

## Tracking files

- `findings.md` — active phase briefs (Phase 21–24) + open residuals + key data-structure pointers + recent error log.
- `docs/findings-multicol-archive.md` — historical detail for Phase 12–20 (sub-phase landings, retired briefs, Blink citations, pre-Phase-21 error-log entries, full ColumnLayoutAlgorithm pseudocode).
- `progress.md` — current gate + per-phase landing notes.
- `CONTINUE.md` — concise next-session pointer.

## Current focus (2026-04-29)

Mainline `master` post-Phase-20. Multicol gate **211/455**. 13 driver invariants 13/13. 4-cat invariants: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781.

The next four phases are sized to one focused effort each. Phases 21–22 are linked: Phase 22 must land before Phase 21 can complete without regressing the 9 prior-clip-wins.

## Phases 21–24

### Phase 21 — Conditional `IsMulticolContainer` clip

Gate Phase 20's overflow clip on user-set `overflow != visible`, mirroring Blink `LayoutBox::UpdateFromStyle`. Closes three Phase 20 stuck tests (`multicol-gap-large-001`, `increase-prev-sibling-height`, `multicol-nested-029`). Hard-blocked by Phase 22 + spanner-overflow placement (the 9 prior-clip-wins rely on the unconditional clip masking layout bugs that Phase 22 fixes).

**Target multicol gate: 211 → 214+ (+3).** One-commit mainline change to `pkg/render/paint_layer.go`. Full brief: `findings.md` § Phase 21.

### Phase 22 — Nested-multicol resume `ConsumedBlockSize` chain

Fix the inner multicol's outgoing `BlockBreakToken.ConsumedBlockSize` so resume across outer-column boundaries works. Closes the `multicol-nested-011..032` cluster + `multicol-fill-balance-003/-026` (~12–15 tests). B6 was a no-op for these because they don't use `column-wrap`; the actual fix is the standard `ConsumedBlockSize` chain. First-pass attempt 2026-04-28 (Phase-14b gate-tweak) was non-monotonic and reverted; recipe is in the brief.

**Target multicol gate: 211 → 220+ (+9 to +15).** Worktree, 3–6 commits. Full brief: `findings.md` § Phase 22.

### Phase 23 — Finish FinishFragmentation port

Drop the `len(children) == 0` leaf-only gate in 16.d.1 + delete or shrink the parent-side overflow path in `block_layout.go:1001-1196`. Replaces the parent-side handling with Blink's `FinishFragmentation` flow. Independent of Phase 21/22; primarily a structural cleanup with incidental gate movement.

**Target multicol gate: stable or +N.** Worktree, 4–8 commits. Full brief: `findings.md` § Phase 23.

### Phase 24 — span-all-children-height cluster (002–013)

Twelve tests across seven sub-clusters that exercise percentage-height descendants of multicol containers. Phase 15 closed `-001`; Phase 24 picks up the remaining 12. Each sub-cluster has its own failure shape; the original Phase 19 brief (in the archive) categorises them.

**Target multicol gate: 211 → 220+ (depends on Phase 22 overlap).** Worktree, 8–12 commits. Full brief: `findings.md` § Phase 24 (sub-cluster detail in `docs/findings-multicol-archive.md` § Phase 19 brief).

## Per-phase discipline (CLAUDE.md recap)

1. Read Blink source BEFORE writing code in a new area.
2. Run only 1–4 driver tests during feature work; gate sweep (all 6 invariants) before each commit.
3. Sub-pixel diffs (even 0.1%) are real bugs — fix at source.
4. If a gate sweep regresses: STOP, ROLLBACK, re-read Blink before re-attempting.

## Test command templates

```bash
# Single multicol test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/<name>' -v

# Full build check
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...

# Gate sweep — run each separately
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests'                                # CSS2: expect 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox'                # expect 626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position'               # expect 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'          # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol'               # 211/455 (post Phase 20)

# 13 driver invariants
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'
```
