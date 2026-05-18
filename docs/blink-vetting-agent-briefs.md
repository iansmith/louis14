# Blink-vetting agent briefs (4 plans)

These are self-contained per-plan briefs for dispatching four parallel worktree agents
to vet Blink citations in the four louis14 plan docs that haven't been started yet:
`plan-css-lists.md`, `plan-css-ruby.md`, `plan-css-text-decor.md`, `plan-css-will-change.md`.

## Orchestrator pre-flight

Before dispatching, resolve **Blink HEAD SHA** once (so all four agents pin to the same
commit):

```
curl -s https://chromium.googlesource.com/chromium/src/+/refs/heads/main?format=JSON \
  | tail -n +2 | python3 -c 'import json,sys;print(json.load(sys.stdin)["commit"])'
```

Substitute that SHA for `<BLINK_SHA>` in each brief below. Dispatch all four in a single
message with four parallel `Agent` tool calls, all with `isolation: "worktree"`.

The agents will each start from this branch's HEAD. Confirm `git status` is clean and
this branch is pushed before dispatching (CLAUDE.md operational rule).

---

## Common rules (embedded in each brief — DO NOT trim when dispatching)

```
═══════════════════════════════════════════════════════════════
HARD RULES — read first
═══════════════════════════════════════════════════════════════

You are operating in an isolated git worktree on a temporary branch. The orchestrator
will merge your branch back when you're done.

1. **CWD anchoring.** First commands: `pwd`, `git status`, `git rev-parse HEAD`,
   `git rev-parse --abbrev-ref HEAD`. Capture pwd as $WT. Use $WT as the root for
   ALL absolute paths. NEVER reference `/Users/iansmith/louis14/...` paths in
   writes — that's the orchestrator's tree and writes there leak out of your
   isolation (`feedback_worktree_cwd_hazard`).

2. **Worktree commit scope — CRITICAL.** You are on a temporary branch named
   `worktree-agent-<hash>`. Commit ONLY to this branch. NEVER commit to:
   - `feat/LOU-129-filter-effects-bucket-j` (the branch the orchestrator is on)
   - `master`, `main`
   - any `fix/*` branch
   The orchestrator merges your branch back. (`feedback_commit_before_agents`,
   project CLAUDE.md operational rules.)

3. **Push with explicit branch name.** `git push origin <your-branch-name>` —
   never bare `git push`. Project repo config requires the branch name.

4. **Fonts symlink.** Worktrees ship only Ahem.ttf. Run once at start from $WT:
   `rm -rf fonts && ln -sfn /Users/iansmith/louis14/fonts fonts`
   Not strictly needed for this task (no test runs), but cheap insurance against
   accidental test execution and matches the standard worktree setup
   (`feedback_worktree_fonts`).

5. **No `open` commands.** Never use `open` to display files — it disrupts the
   user's screen (`feedback_no_open_commands`).

6. **No code changes.** This task edits markdown plan docs only. Don't touch any
   `.go` file, build config, or test file. Don't run `go build`, `go test`, or
   `gofmt`. Don't reformat the plan doc beyond the citation edits.

7. **Predictive humility.** A citation is verified ONLY if you fetched the cited
   Blink file at the pinned SHA, located the symbol, AND read the surrounding ±30
   lines to confirm the plan's behavior description matches the actual code.
   Don't rubber-stamp based on "the symbol exists somewhere in the file"
   (`feedback_predictive_humility`).

8. **No plan restructuring.** Don't reorder phases, rewrite goals, or change the
   plan's approach. Only:
   - Fix Blink citations that are wrong (file path, line number, type name).
   - Rewrite behavior descriptions that don't match current Blink, preserving the
     plan's intent and noting the delta in the vetting log.
   - Add citations where the plan describes Blink behavior without one.
   - Add the vetting-log section at the top.

9. **Commit + push at each milestone**, not just at the end
   (`feedback_agent_checkpoints`).

10. **Study Blink BEFORE concluding.** This is literally what the task is —
    don't shortcut by inferring from type names alone.
```

---

## Brief 1 — `plan-css-lists.md`

```
You are an isolated worktree agent. Your task: vet the Blink references in
`docs/plan-css-lists.md` against Chromium `main` @ <BLINK_SHA>.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

File: $WT/docs/plan-css-lists.md (~571 lines)

The plan covers porting Blink's counter / list-marker model to louis14. Citation
density (audited 2026-05-17): 27 Blink mentions, 11 source-file refs (with line
numbers), 10 explicit "**Blink reference.**" blocks.

Key types you must verify exist at <BLINK_SHA> with the cited fields/behavior:
- `CountersAttachmentContext` — counter scope tree; the foundation for Phase 1
- `ListMarker` — Blink's list-marker abstraction (the "B1" replacement)
- `LayoutOutsideListMarker` / `LayoutInsideListMarker` — the layout-tree boxes
- `LayoutTreeBuilder` — how Blink constructs the marker box at tree-build time
- counter-style → glyph resolution path (cited in the marker text source work)

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

**M0 — Setup + extract citation checklist**
- `pwd`, `git status`, `git rev-parse HEAD`, `git rev-parse --abbrev-ref HEAD`.
- Symlink fonts (see Hard Rule 4).
- Read the full plan into memory. Build a numbered checklist of EVERY Blink
  citation: file path, line range, type/function name, behavior claim.
- Expected count: ~15-25 distinct citations (some cited multiple times).
- Commit + push the checklist as a temp file (e.g., `$WT/.vetting-checklist.md`)
  so it's auditable. Title the commit `chore(vetting): css-lists citation
  checklist`.

**M1 — Fetch + verify each citation**
- For each checklist entry, WebFetch the file at the pinned SHA via:
  `https://chromium.googlesource.com/chromium/src/+/<BLINK_SHA>/<path>`
  (raw view; falls back to `?format=TEXT` if HTML).
- Mark each as: ✓ unchanged (exact file:line still matches), ↻ updated
  (file/line/name moved — record old and new), ✗ broken (cited content gone
  or wrong description — escalate).
- Read ±30 lines around each cited symbol; confirm the plan's behavior claim
  matches the actual code. If the claim drifts, draft a corrected description.
- Commit + push the raw verification log (your working notes, before edits) as
  `$WT/.vetting-rawlog.md`. Title: `chore(vetting): css-lists raw verification
  log @ <BLINK_SHA>`.

**M2 — Edit the plan in place**
- Apply citation fixes directly in `docs/plan-css-lists.md`:
  - Updated file paths / line numbers replace the stale ones.
  - Renamed types replace the stale names.
  - Rewritten behavior descriptions preserve intent + flag the change.
- Add a `## Blink vetting log` section IMMEDIATELY AFTER the existing `## Goal`
  section (before any other H2), in this exact format:

  ```
  ## Blink vetting log

  **Vetted against Chromium `main` @ <BLINK_SHA>** on <date>.

  ### Citations verified
  - `<path>:<line>` (`<symbol>`) — ✓ unchanged
  - ...

  ### Citations updated
  - `<old path:line>` (`<symbol>`) → `<new path:line>` (<reason>)
  - ...

  ### Citations broken / missing in current Blink
  - `<old path:line>` (`<symbol>`) — <what was lost> — <recommended plan action>
  - (or: none)

  ### Citations added
  - <plan section> now also cites `<new ref>` — <reason>
  - (or: none)
  ```

- Delete the temp `.vetting-checklist.md` and `.vetting-rawlog.md` files
  (they served their audit purpose; the in-plan vetting log is the canonical
  record).
- Commit + push. Title: `docs(plan-css-lists): vet Blink refs @ <BLINK_SHA>`.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

When you finish, report:
- Your worktree path and branch name.
- Pinned Blink SHA.
- Counts: total citations checked / verified / updated / broken / added.
- **Most important:** any plan section whose Blink claim CANNOT be supported at
  this SHA — i.e. cases where the plan's approach depends on a Blink type or
  algorithm that doesn't exist (or has been replaced by a different pattern).
  These are architectural risks; flag them loudly under "DESIGN ESCALATION."
- Commit SHAs at each milestone.

State actual deltas, not predictions.
```

---

## Brief 2 — `plan-css-ruby.md`

```
You are an isolated worktree agent. Your task: vet the Blink references in
`docs/plan-css-ruby.md` against Chromium `main` @ <BLINK_SHA>.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

File: $WT/docs/plan-css-ruby.md (~733 lines — the longest of the four)

The plan covers correct ruby layout: UA styles, display model, box-fixup,
inline ruby-column data model + base/annotation stacking, line-box expansion.
Citation density (audited 2026-05-17): 37 Blink mentions, 48 source-file refs
(the densest of the four plans), 14 explicit "**Blink reference.**" blocks.

This is the highest-volume verification job among the four — budget accordingly.

Key types you must verify exist at <BLINK_SHA> with the cited fields/behavior:
- `LayoutRubyColumn` — the new (post-LayoutRubyRun) ruby box (Blink refactored
  this circa 2023; verify it's still `LayoutRubyColumn` and not since renamed)
- `LayoutRubyAsBlock` — block-level ruby container
- `LayoutRubyRun` — the older API (verify whether plan correctly identifies it
  as legacy or current)
- `LayoutBlockFlow` integration points for ruby annotation line-box expansion
- `LayoutTreeBuilder` ruby-specific paths
- `ruby-position: over/under` resolution in the style adjuster
- UA stylesheet rules for `ruby`, `rt`, `rb`, `rbc`, `rtc`, `rp`

**Cross-check:** css-text-decor plan also cites `LayoutRubyColumn` (Bucket C
gates on ruby layout). If you find that Blink has renamed/refactored this, flag
it — the css-text-decor vetting agent should know.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

(Identical structure to Brief 1's M0/M1/M2. See that brief for the format of
the vetting-log section to insert.)

For this plan specifically:
- Expected citation count: ~50-70 distinct citations. Budget more verification
  time than the other three.
- Pay extra attention to recent Blink ruby refactors (search the git history
  of the cited files for "ruby" keyword if a citation's line number is way
  off — Blink overhauled ruby fairly recently and some legacy refs may be in
  the plan).

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

(Same structure as Brief 1.)

Additionally: flag any LayoutRubyColumn ↔ LayoutRubyRun naming discrepancies
explicitly, as the css-text-decor agent depends on this.
```

---

## Brief 3 — `plan-css-text-decor.md`

```
You are an isolated worktree agent. Your task: vet the Blink references in
`docs/plan-css-text-decor.md` against Chromium `main` @ <BLINK_SHA>.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

File: $WT/docs/plan-css-text-decor.md (~382 lines)

The plan covers text-decoration (underline/overline/line-through), emphasis
marks, and the integration with ruby layout. Citation density (audited
2026-05-17): 16 Blink mentions, 27 source-file refs (dense per-line), 7
explicit "**Blink reference.**" blocks. The plan has the most line-number-
specific citations of the four (e.g. `text_decoration_info.cc:417-432`,
`applied_text_decoration.h:52`, `text_painter.cc:539-567`).

Key types you must verify exist at <BLINK_SHA> with the cited fields/behavior:
- `AppliedTextDecoration` (`core/style/applied_text_decoration.h`, cited at
  line 52 for the typedef, lines 20-56 for the class fields)
- `AppliedTextDecorationVector` = `GCedHeapVector<AppliedTextDecoration, 1>`
  (cited at applied_text_decoration.h:52)
- `TextDecorationInfo` (`core/paint/text_decoration_info.{h,cc}`)
  - `ComputeThickness()` cited at text_decoration_info.cc:417-432
  - `ComputeUnderlineOffset()` cited at text_decoration_info.cc:354-370
  - `ComputeOverlineLineData()` cited at text_decoration_info.cc:373-390
- `TextPainter::SetEmphasisMark()` cited at text_painter.cc:539-567
- `InlinePaintContext::DecoratingBoxList` etc. (`core/paint/inline_paint_context.h`)
- `LayoutRubyColumn` (Bucket C — cross-check against css-ruby plan vetting)

Specific line numbers are dense — verify each carefully.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

(Same structure as Brief 1's M0/M1/M2.)

For this plan specifically:
- Pay extra attention to the line-range citations — they're often -432
  exact-end-line specific. Tolerance ±50 lines; record exact current ranges.
- Bucket C is gated on the css-ruby plan being implementable — if the
  css-ruby agent flags a LayoutRubyColumn refactor, surface it here too.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

(Same structure as Brief 1.)

Additionally: report whether the plan's line-range citations are mostly
within ±10 lines, ±50 lines, or further off. This calibrates how stale the
plan is against current Blink (a few days vs. several months drift).
```

---

## Brief 4 — `plan-css-will-change.md`

```
You are an isolated worktree agent. Your task: vet the Blink references in
`docs/plan-css-will-change.md` against Chromium `main` @ <BLINK_SHA>.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

File: $WT/docs/plan-css-will-change.md (~444 lines)

The plan covers building a canonical `will-change` value model and routing CB
/ stacking-context predicates through it. Citation density (audited
2026-05-17): 15 Blink mentions, 10 source-file refs, 0 explicit "**Blink
reference.**" header blocks — citations are inline-narrative rather than
sectioned. This means citations are easier to MISS during extraction; be
thorough.

Line ranges in this plan use the tilde-approximate form (e.g.,
`layout_object.cc ~lines 1520-1580`, `style_adjuster.cc ~lines 1240-1430`).
Treat these as ±100 lines tolerance, but record exact current line numbers
in the vetting log.

Key types you must verify exist at <BLINK_SHA> with the cited fields/behavior:
- `StyleWillChangeData` (`third_party/blink/renderer/core/style/style_will_change_data.h`)
  - Fields: `values` (Vector<AtomicString>), `resolved_longhand_ids` (CSSBitset),
    `has_transform_property`, `has_any_transform_property`,
    `has_scroll_position_value`
  - Verify the comment "The bitset only contains resolved longhand CSSPropertyID(s).
    No aliases." still appears (or has equivalent semantics)
- `ComputedStyle::HasWillChangeProperty(CSSPropertyID)` accessor
- `LayoutObject::ComputeIsFixedContainer` (~lines 1520-1580)
- `LayoutObject::ComputeIsAbsoluteContainer`
- `ComputedStyle::HasTransformRelatedProperty()` (`computed_style.h` ~2740)
- `StyleAdjuster::AdjustComputedStyle` (~lines 1240-1430): `ForcesStackingContext`,
  `AllowsZIndex`
- `PaintLayerStackingNode` z-sort behavior

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

(Same structure as Brief 1's M0/M1/M2.)

For this plan specifically:
- Citations are inline (no Blink reference blocks). Extract by regex on file
  names (.cc, .h) AND on type names (PascalCase identifiers that look Blink-
  like). Build an audit pass to confirm you didn't miss any.
- The ~lines form is approximate by design — your job is to replace it with
  exact current line numbers in the vetting log.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

(Same structure as Brief 1.)

Additionally: report how many inline citations you extracted vs. how many
were captured by the initial regex pass. This calibrates the extraction
methodology for future inline-citation plans.
```

---

## Orchestrator post-flight (after all 4 agents complete)

1. Read each agent's final report. Pull out any **DESIGN ESCALATION** flags
   first — those are architectural risks that need user attention before any
   of these plans get picked up by an implementing agent.
2. For each agent's branch, verify:
   - `git log` shows ONLY commits on `worktree-agent-*` (no leakage to
     `feat/LOU-129-filter-effects-bucket-j` or main).
   - `git diff` against the base shows ONLY changes under `docs/plan-css-*.md`.
3. Merge each agent's branch into `feat/LOU-129-filter-effects-bucket-j` with
   a `--no-ff` merge commit summarizing the vetting outcome.
4. Push.
5. Update MEMORY.md with: "Blink-citation discipline — plans MUST cite by file
   path + line range + Blink commit SHA going forward" (if the vetting surfaces
   patterns worth canonicalizing).

If any DESIGN ESCALATION flag fires, **stop and surface it to the user before
merging** — the plan may need a re-design pass before any implementation work
is dispatched.
