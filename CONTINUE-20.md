# CONTINUE-20: Multicol overflow clip — Blink-aligned port

**STATUS: HISTORICAL — Phase 20 landed 2026-04-28 (worktree
`phase-20-overflow-clip`, merged via `--no-ff`). Multicol gate
205 → 211/455 (+6, hits the brief's minimum target). 13 driver
invariants 13/13 at 0 diff; all 9 prior-clip-wins held; six
new reclaims. See `progress.md` § "Phase 20 LANDED" for the
landed summary, `findings.md` § "Phase 20 LANDED" for the
implementation notes including diverges from the brief, and the
P20.1–P20.7 commit messages for per-step verification.**

Self-contained continuation prompt for the Phase 20 port. Drop into a fresh
session and proceed.

## Mission

Replace the ad-hoc `ClipContentToBorderBox`-on-multicol mechanism (commit
`3389efe7`) with a Blink-aligned paint-time clipping port. End the cycle of
gate-tweaking the broad clip — the gate variations attempted 2026-04-28 were
all non-monotonic (`hasExplicitBlock`, `hasExplicitBlock || IsFixedBlockSize`).
The root issue is structural: louis14 lacks `BoxType` infrastructure, and the
multicol-overflow workaround masks layout/paint bugs that need their own
fixes.

**Gate target:** multicol 205/455 → 211+/455 (+6 to +9). 13 driver invariants
13/13 must hold. 4-cat invariants 1499/6720 must hold.

## Where we are at session start

Mainline `fix/flexbox-fast` @ commit `95433ce6` (or later if more docs commits
have landed). Tree clean. No in-flight worktree for this work.

Current gate snapshot:
- CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (1499/6720, 16 known-fails)
- multicol 205/455 (with broad clip workaround in place)
- 13 driver invariants 13/13 at 0 diff
- spanner-fragmentation cluster 12/13 (-008 0.2% pre-existing)

## Authoritative brief

Read `findings.md` § "Phase 20 BRIEF — Multicol overflow clip: proper Blink
port (BoxType + container OverflowClip)" before starting. It contains:

- Verbatim Blink references (file:line).
- The counter-intuitive headline: Blink does **NOT** clip per column. It
  clips at the **multicol container**, via paint-property-tree OverflowClip.
- Per-column `kColumnBox` infrastructure exists in Blink for paint-cache
  identity (`ScopedDisplayItemFragment`) and column-rule extent computation,
  not for clipping.
- 6-commit decomposition (P20.1 through P20.6).
- Test expectations + reclaim list (6 regressions to close).

## Project rules (must follow — see `CLAUDE.md`)

1. **Foundational correctness over quick wins.** Pick a sub-step and finish
   it; don't filter tests by near-passing percentage.
2. **Study Blink BEFORE writing any code** for each step. The brief has the
   refs; verify them in fresh source if anything has shifted.
3. **All tests must pass at 0% diff.** A 0.5% diff is a failure. Don't
   dismiss small diffs as anti-aliasing.
4. **Test execution discipline.** Run only the specific tests the current
   step targets — typically the 13 drivers + the named reclaim/wins lists.
   No full-suite runs unless explicitly requested.
5. **Worktree for multi-commit refactors.** Phase 20 is ~6 commits; use a
   worktree branch (`phase-20-overflow-clip`). Commit on every milestone.
   Mainline merges only after the worktree's full gate sweep verifies.
6. **Discuss regressions, no hard exits.** If a step regresses something
   unexpected, pause and report — don't try to power through.

## Pre-flight checklist

Before P20.1:

- [ ] Confirm baseline: 13 drivers 13/13, multicol 205/455, 4-cat 1499/6720.
- [ ] Spawn worktree from mainline HEAD (`git worktree add
  .claude/worktrees/phase-20 -b phase-20-overflow-clip`).
- [ ] Push the worktree branch so a future session can resume from a fresh
  clone (`cd .claude/worktrees/phase-20 && git push -u origin
  phase-20-overflow-clip`).
- [ ] Create a TaskList in the new session with the 6-commit decomposition.

## 6-commit decomposition (from brief)

### P20.1 — `BoxType` enum on `PhysicalFragment`

Add `BoxType` field to `PhysicalFragment` (`pkg/layout/layout_result.go` or
`pkg/layout/types.go`, wherever the canonical fragment lives). Enum values:
at least `BoxTypeNormal` (default, value 0) and `BoxTypeColumn`. Mirrors
Blink `physical_fragment.h` BoxType. Add helper methods `IsColumnBox() bool`
and `IsFragmentainerBox() bool`.

**Verification:** build clean. 13 drivers PASS (no behaviour change). Commit
the worktree branch.

### P20.2 — Set `BoxTypeColumn` on column fragments in MLA

In `multicol_layout.go::layoutLine` (around line 1148-1180 — the per-column
inner loop where `NewBlockLayoutAlgorithm` produces each column fragment),
after the per-column `result := NewBlockLayoutAlgorithm(...).Layout()`, set
`result.Fragment.BoxType = BoxTypeColumn` before adding to the multicol's
children. Mirror Blink `column_layout_algorithm.cc:1620`.

**Verification:** build clean. 13 drivers PASS. Multicol gate unchanged (no
painter changes yet). Commit.

### P20.3 — Painter recognises `BoxTypeColumn`

In `pkg/render/paint_layer.go` (or wherever the renderer walks fragment
children), when descending into a `BoxTypeColumn` child, establish a
display-item-fragment identity scope for paint caching. **Do NOT emit a
clip rect here** — Blink doesn't, and the louis14 comment at
`paint_layer.go:274-281` confirms ClipBlockAxisOnly removal was correct.

For louis14 specifically, "display-item-fragment scope" might just mean a
unique fragment identifier so that paint caching across multiple fragments
of the same node works. If louis14 doesn't yet have a paint cache, this
step may be a no-op pass; document it and move on.

**Verification:** build clean. 13 drivers PASS. Multicol gate unchanged.
Commit.

### P20.4 — Column-rule painter uses `kColumnBox` extents

Currently `render.go::drawColumnRules` (~line 2919) uses `contentH =
box.Height - border - padding` — the multicol's full content area. This
causes column rules to extend past the columns' actual heights when the
multicol's box is taller than its columns (e.g. flex-stretched
`flexbox_columns-flexitems-2`).

Fix: mirror Blink's `BoxFragmentPainter::PaintColumnRules` (~line 1876).
Iterate the multicol's child fragments. Use only `IsColumnBox()` children.
Compute rule positions from adjacent column-fragment offsets/sizes; rule
block-extent = column block-extent.

For the GapGeometry path (which already has `CrossGaps`), use the column
fragments' Y range to bound the rule's block extent. Plumb a per-column
`BlockOffset` + `BlockSize` if not already exposed via GapGeometry.

**Verification:** build clean. 13 drivers PASS. Run
`flexbox_columns-flexitems-2` — should close at 0 diff. Multicol gate may
move +1 (no expected loss). Run a quick column-rule cluster sweep to ensure
no column-rule tests regress. Commit.

### P20.5 — Multicol container OverflowClip; remove broad ClipContentToBorderBox

This is the structural commit. Two parts:

**Part 5a — Layout side:** In `multicol_layout.go`, **delete** the
`ClipContentToBorderBox = true` assignment at the bottom of `Layout()`
(around `:1061-1063` per the current commit). Multicol fragments should no
longer carry this flag.

**Part 5b — Paint side:** Mark the multicol's box as needing OverflowClip.
Two options for louis14:

- **Option A (preferred):** Add a flag `IsMulticolContainer bool` (or
  detect dynamically via `box.Style.GetColumnCount() > 1 ||
  box.Style.GetColumnWidth().Defined()`). In `paint_layer.go`, when
  building a layer for a multicol box, set up an overflow clip with
  rect = multicol's *content box* (not border box; mirror Blink's default
  for non-scroll-container OverflowClip). Apply this clip before walking
  children.
- **Option B (interim):** Repurpose `ClipContentToBorderBox` as
  "ClipContentToContentBox"; rename and change the rect computation in
  `paint_layer.go:289-300` from border-box to content-box. Set the flag
  unconditionally for all multicol fragments. Slightly less clean but
  fewer code surfaces touched. Either way, the rect must be content-box,
  not border-box.

**Open verify:** before committing, fetch `LayoutBox::OverflowClipRect` in
Blink to confirm content-box vs padding-box. Best guess from the research
is content-box for non-scroll-container, padding-box for scroll. Multicol
is non-scroll → content-box.

**Verification:** build clean. 13 drivers PASS. Run the 6-test reclaim list:
- `inline-block-and-column-span-all` (1.5%)
- `multicol-fill-balance-032` (1.4%)
- `multicol-gap-large-001` (0.3%)
- `multicol-span-all-margin-nested-001` (0.2%)
- `increase-prev-sibling-height` (~0%)
- `multicol-nested-029` (~0%)

And the 9 prior-clip-wins must NOT regress:
- `multicol-breaking-002`, `multicol-breaking-nobackground-002`,
  `multicol-fill-balance-nested-000`, `multicol-list-item-001`,
  `multicol-nested-015/021/026/028`, `nested-after-float-clearance`.

Multicol gate target: 205 → 211+ (+6 from reclaiming). If only some of the
6 reclaim, that's the next sub-investigation — likely TallestUnbreakable
gaps closed in P20.6.

Commit when stable.

### P20.6 — TallestUnbreakable for atomic inlines

`inline-block-and-column-span-all` is the test that motivated the whole
investigation. After P20.5 it may still fail at 1.5% if louis14's
TallestUnbreakable carrier doesn't account for atomic-inline content
(`display:inline-block` is implicitly monolithic in Blink — can't break
across columns).

Look at `pkg/layout/fragmentation_utils.go::CalculateUnbreakableBlockSize`
(or wherever TallestUnbreakable is computed during the initial balancing
pass). Verify it includes atomic-inline children (inline-blocks, replaced
inline elements, etc.). If not, extend it to mirror Blink's
`fragmentation_utils.cc:1105-1113` (the `PropagateTallestUnbreakableBlockSize`
site for `ShouldAvoidBreakInside` children).

In Blink: an atomic inline contributes its own height as a candidate for
TallestUnbreakable because the inline can't break across fragmentainer
boundaries. The multicol's column auto-block-size must be at least this
tall.

**Verification:** `inline-block-and-column-span-all` PASS at 0 diff. Other
reclaims hold. Multicol gate +1 (or more if additional auto-balanced
multicols benefit). Commit.

### Final sweep (no commit until passing)

After P20.6, run the full multicol gate + 4-cat invariants + spanner-
fragmentation cluster + 13 drivers. Required gate:

- CSS2 99/99 · flex 626/629 · pos 92/105 · wm 781/781 (1499/6720, 16 known)
- multicol ≥ 211/455 (+6 minimum)
- 13 driver invariants 13/13
- spanner-fragmentation cluster ≥ 12/13 (no regression)

If green, merge worktree to mainline via `--no-ff` from
`phase-20-overflow-clip`. Update `progress.md`, `task_plan.md`,
`CONTINUE.md` with the new gate. Move `CONTINUE-20.md` to the historical
list (mark HISTORICAL at top).

## Authoritative Blink references (research 2026-04-28)

Verbatim refs preserved in `findings.md` § "Phase 20 BRIEF". Key ones:

- `physical_fragment.h` — BoxType enum, IsFragmentainerBox, IsColumnBox.
- `column_layout_algorithm.cc:1620` (and balance path ~2045) — SetBoxType
  call site on column fragments.
- `box_fragment_painter.cc::PaintBlockChild` (~line 1480) — fragmentainer
  branch sets up display-item-fragment scope, no clip.
- `box_fragment_painter.cc::PaintColumnRules` (~line 1876) — rule loop
  uses adjacent column fragment offsets/sizes.
- `paint_property_tree_builder.cc::UpdateOverflowClip` — multicol container
  OverflowClip property node setup. **Open question:** content vs padding
  rect — verify against `LayoutBox::OverflowClipRect`.

If any reference has shifted in current Blink head, re-fetch via
`source.chromium.org` or `chromium.googlesource.com` before relying on the
line numbers.

## Hard exits + discussion points

These are NOT hard-exit-and-stop conditions per operator preference; they
are **pause-and-discuss** triggers:

1. Driver invariant regresses at any step → pause and discuss. The driver
   set is the integrity check.
2. P20.5 OverflowClip rect choice (content vs padding) regresses any of
   the 9 prior-clip-wins → discuss; might need overflow-clip-margin
   equivalent or a different rect.
3. P20.6 doesn't close `inline-block-and-column-span-all` → a deeper
   layout bug exists (e.g. multicol's resolveColumnAutoBlockSize doesn't
   consult the TallestUnbreakable contribution from atomic inlines on the
   measure pass); investigate before committing.
4. Multicol gate moves backward at any commit → pause and discuss.

## Files this work will touch (rough estimate)

- `pkg/layout/types.go` or `layout_result.go` — `BoxType` enum + field on
  `PhysicalFragment` (P20.1).
- `pkg/layout/multicol_layout.go` — set BoxType on column fragments
  (P20.2); remove `ClipContentToBorderBox` assignment (P20.5).
- `pkg/render/paint_layer.go` — recognise `BoxTypeColumn` (P20.3); replace
  border-box clip with content-box clip on multicol containers (P20.5).
- `pkg/render/render.go` — `drawColumnRules` rewrite using kColumnBox
  child extents (P20.4).
- `pkg/layout/fragmentation_utils.go` — extend `CalculateUnbreakableBlockSize`
  for atomic inlines (P20.6).

## Rules pointer

`CLAUDE.md` (project) + `~/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`
(session memory). Key rules above.

## Why this is the right port

Two seasons of multicol-overflow workarounds (Phase 12h F2 partial
ClipBlockAxisOnly → v2 B3 removal → 3389efe7 broad clip → multiple
narrow-gate attempts) all bottomed out because the underlying machinery
(`BoxType` infra + paint-property-tree OverflowClip) was missing. Phase 20
ports the right machinery from Blink in 6 small commits. After P20.6
lands, no further "clip workaround" should be needed for multicol; future
multicol overflow issues become layout bugs (which is the right level to
fix them at).

End of continuation prompt.
