# CONTINUE: Phase 16.e + 18 bundled — Commit 3 (WRITE-site flat tokens)

## Where we are

**Worktree:** `/Users/iansmith/louis14-phase-16e-18`, branch `phase-16e-18-walker-carrier`. Clean (after Commit 2 lands).

**Mainline (`fix/flexbox-fast` @ `36b3101c`)** — gate: CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 · multicol **196/455** · spanner-frag 11/13.

**Commit 1 DONE (`43ec8c66`):** schema + walker scaffold. `pkg/layout/multicol_part_walker.go` added; `MulticolBreakTokenData` field on `BlockBreakToken`; READ at `multicol_layout.go:294-297` plumbed but field always nil → behavior unchanged. 13 drivers pass.

**Commit 2 DONE (this push):** READ site switched to walker dispatch. The 3-slot positional parser + pure-nested-resume promotion at `multicol_layout.go:415-432` is replaced with `walker := NewMulticolPartWalker(contentNode, mla.space.BreakToken)`. The main loop at `multicol_layout.go:512-849` is rewritten as Blink-style two-branch dispatch (mirrors cla.cc:605-714):

- **Column-content branch** (`entry.DescendantNode == nil`): row-advance guard → layoutLine → walker.Next → if no spanner: row-wrap continue via `walker.AddNextColumnBreakToken` / nested-multicol-break / done; if spanner discovered: `walker.MoveToSpanner(spanner, remainingToken)` and continue.
- **Spanner branch** (`entry.DescendantNode != nil`): derive `hasSpannerResume / spannerConsumed / spannerContentBreakToken / nextSpannerClipToken` from `entry.BreakToken` (still nested, since WRITE side is unchanged) → break-before / outer-frag clip / fresh layout vs content-overflow resume vs clip resume / pre-commit row snap / margins / GapGeometry / forced break-after via `walker.Current()` peek for post-spanner content.

**Driver test result:** 11/13 pass. **Regressions match the brief's expectation:** `spanner-fragmentation-001` (0.8% diff) and `spanner-fragmentation-006` (1.4% diff). Both regressions trace to the same root: with the WRITE side still emitting 3-slot positional tokens, when the resumed parent break token has `[col_token, partial_spanner, col_rows]`, the walker iterates: entry 0 = col_token (col content) → layoutLine re-detects spanner via path → walker.MoveToSpanner clobbers the walker, dropping the partial_spanner (with its resume info) and col_rows entries that were queued. The spanner is then laid out FRESH instead of resumed. Commit 3's WRITE-side flattening removes the redundant col_token, and the spanner's resume info appears as the FIRST walker entry directly.

## Authoritative brief

`findings.md` § "Phase 16.e + 18 BUNDLED BRIEF (prep complete 2026-04-28)" — line 1282.

## Next: Commit 3 — switch WRITE site to flat ChildBreakTokens (regressions restore)

**Scope:** rewrite all WRITE sites in `pkg/layout/multicol_layout.go` to emit flat document-order child tokens via the `MulticolBreakTokenBuilder` accumulator (already defined in `multicol_part_walker.go:193-235`).

Concrete sites (post-Commit-2 line numbers — re-grep before editing):

1. **`buildOuterBreakResult` closure (~`multicol_layout.go:466-501`)** — currently emits `[prevNextColToken, partialSpannerToken, pendingColRowsBreakToken]` 3-slot vector with trailing-nil trim. Rewrite to take a `MulticolBreakTokenBuilder` accumulated during the loop (or compose flat from the existing pendings in document order). The `prevNextColToken` arg should disappear — the spanner's resume info IS the partialSpannerToken directly (no need for a re-detect col_token at slot[0]).

2. **Spanner content-overflow build site (~`multicol_layout.go:687-693`)** — currently `pendingPartialSpannerToken = &BlockBreakToken{Node: spanner, ChildBreakTokens: [fullResult.BreakToken]}`. Change to emit `&BlockBreakToken{Node: spanner, ChildBreakTokens: fullResult.BreakToken.ChildBreakTokens, ConsumedBlockSize: 0}` (the wrapper is needed for Node identity in the walker, but its `ChildBreakTokens` should now be the spanner's OWN content-resume children, not a wrapper layer).

3. **Combined-clip mutation site (~`multicol_layout.go:730-737`)** — `pendingPartialSpannerToken.ChildBreakTokens = append(..., clipToken)`. In flat encoding, the clipToken is a SEPARATE walker entry (a second flat entry with Node=spanner, ConsumedBlockSize=available). Push it as a flat entry on the builder instead.

4. **Mid-spanner partial-token return (~`multicol_layout.go:740-744`)** — `partialSpannerToken := &BlockBreakToken{Node: spanner, ConsumedBlockSize: available}`. Same shape — push as flat entry via builder instead of arg-positional through buildOuterBreakResult.

5. **Spanner branch READ at `multicol_layout.go:608-624` (the `entry.BreakToken.ChildBreakTokens[0/1]` peek)** — once WRITE side flattens, `nextSpannerClipToken` becomes a separate walker entry whose `Node.Style().GetColumnSpan()=="all"`. Drop the `[1]` index read; rely on the walker's natural advance to surface the clip-chain spanner as the next iteration. The `[0]` index for `spannerContentBreakToken` may also drop if the partial token's ChildBreakTokens are now the spanner's OWN content children — the walker entry's BreakToken IS the spanner's break token directly.

6. **Pure-nested-resume case** — old code promoted slot[2] → slot[0] when slot[0]/slot[1] were nil. Removed entirely; in flat encoding the colRows entry IS the first walker entry. The walker's empty-entry behavior (when parent break token has explicit nil padding, e.g. `[nil, nil, colRows]`) regresses in Commit 2 because the walker generates wasted iterations on those nil slots; Commit 3 stops emitting nil padding (just the col_rows entry directly).

After Commit 3, the 13 driver tests must restore to 13/13 at 0 diff. **Hard exit #1:** if they don't, walker WRITE-site mapping is wrong — re-read Blink cla.cc:605-714 + 1397-1522, do NOT pile predicates.

## Driver invariants (must hold at 0 diff at Commit 3 onward)

`column-height-001/010/017/026/027`, `multicol-nested-030/031`, `spanner-fragmentation-001/004/006`, `multicol-rule-nested-balancing-004`, `nested-floated-multicol-with-monolithic-child`, `nested-past-fragmentation-line`.

```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run "TestWPTCSS3Reftests/css-multicol/(column-height-001|column-height-010|column-height-017|column-height-026|column-height-027|multicol-nested-030|multicol-nested-031|spanner-fragmentation-001|spanner-fragmentation-004|spanner-fragmentation-006|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)"
```

At Commit 2: 11/13 (with spanner-fragmentation-001 and -006 regressing) is the recorded baseline. Commit 3 must restore to 13/13.

## Remaining commits (4–6)

4. Wire `MulticolData` WRITE site (Phase 18 row-carry, mirror cla.cc:822-833). Target: multicol-nested-011 closes; sweep 012–032 + multicol-fill-balance-003/-026.
5. Drop `IsInsideColumnSpanner` clamp gate (`block_layout.go:1426-1448, 606-614`; `constraint_space.go:171-191, 494-505`; `multicol_layout.go:1426, 1459` per pre-Commit-2 numbers — re-grep). Hard exit if spanner-frag-006 regresses.
6. Full gate sweep; merge worktree → `fix/flexbox-fast` if green (≥211/455 multicol, ≥11/13 spanner-frag, all other invariants held).

## Hard exit conditions (do NOT chase with predicates)

1. Commit 3 doesn't restore Commit 2's regressions → walker write-site mapping wrong; re-read Blink cla.cc:605-714 + 1397-1522.
2. Commit 5 regresses spanner-frag-006 → revert Commit 5; the 16.d.1 gate is still load-bearing.
3. Commit 4 regresses multicol-nested-010 → row-carry write fires in wrong condition.
4. Multicol gate drops below 196 at any commit other than Commit 2 → STOP.

## Operational reminders

- Work in the worktree only. Commit to `phase-16e-18-walker-carrier`, NOT `fix/flexbox-fast`.
- Mainline-merge only at Commit 6, only if green.
- CLAUDE.md rules: study Blink first, all tests at 0 diff, no chasing easy wins.
