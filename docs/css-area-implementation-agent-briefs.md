# CSS-area implementation agent briefs

Canonical brief artifact for the four CSS-area implementation streams
(`plan-css-lists.md`, `plan-css-ruby.md`, `plan-css-text-decor.md`,
`plan-css-will-change.md`). Mirrors the structure of
`docs/blink-vetting-agent-briefs.md` (the prior round's methodology doc).

## Launch shape (decided)

File-footprint analysis (see git log on this doc for the matrix) ruled out
running all four plans in parallel — `pkg/css/style.go`, `inline_layout.go`,
`layout_tree_builder.go`, and the ruby-overlap between css-ruby and
css-text-decor produce too much merge contention. The agreed shape:

- **Round 1 (parallel pair)**: css-will-change + css-lists.
  Lowest footprint overlap (only `pkg/css/style.go`, additive).
- **Round 2 (solo)**: css-ruby. Biggest single stream; runs alone to avoid
  line-layout merge collisions.
- **Round 3 (solo, after Round 2 lands Phases 1-2)**: css-text-decor.
  Depends on css-ruby's column model.

This doc starts with Round 1. Round 2/3 briefs will be appended as they
launch.

## Orchestrator pre-flight (Round 1)

1. Confirm `git status` clean, `master` is at the head with the vetted plans
   (commit `6c854e73` or later).
2. Confirm `origin/master` is pushed and the working tree is on master
   (worktrees branch from HEAD).
3. Dispatch both Round 1 agents in a single message with two parallel
   `Agent` tool calls, `isolation: "worktree"`, `run_in_background: true`.
4. When an agent returns, verify its branch in the orchestrator's tree:
   - All commits on `worktree-agent-<hash>` only.
   - Diff scope matches the plan's "louis14 target files" + new files.
   - No edits to `pkg/css/style.go` accessors that pre-existed (Hard Rule 6).
5. Merge `--no-ff origin/<agent-branch>` to master with a summary commit.
   Push `origin master`.

If an agent reports a **DESIGN ESCALATION**, do not merge until the user has
reviewed the specific code-surface mismatch.

---

## Common rules (embedded in each brief — DO NOT TRIM)

```
═══════════════════════════════════════════════════════════════
HARD RULES — read first
═══════════════════════════════════════════════════════════════

You are operating in an isolated git worktree on a temporary branch named
`worktree-agent-<hash>`. The orchestrator will merge your branch back when
you're done. You start from `master` HEAD.

1. **CWD anchoring.** First commands: `pwd`, `git status`, `git rev-parse HEAD`,
   `git rev-parse --abbrev-ref HEAD`. Capture pwd as $WT. Use $WT as the root
   for ALL absolute paths. NEVER reference `/Users/iansmith/louis14/...`
   paths in writes — that's the orchestrator's tree and writes there leak
   out of your isolation (`feedback_worktree_cwd_hazard`).

2. **Worktree commit scope — CRITICAL.** You are on
   `worktree-agent-<hash>`. Commit ONLY to this branch. NEVER commit to:
   - `master`, `main`
   - any `feat/*` or `fix/*` branch
   The orchestrator merges your branch back. (`feedback_commit_before_agents`,
   project CLAUDE.md operational rules.)

3. **Push with explicit branch name.** `git push origin <your-branch-name>` —
   never bare `git push`. Project repo config requires the branch name.

4. **Fonts symlink (REQUIRED).** Worktrees ship only Ahem.ttf. Tests need
   the full font set. Run once at start from $WT:
   `rm -rf fonts && ln -sfn /Users/iansmith/louis14/fonts fonts`
   Without this, broad sweeps report false catastrophic regressions
   (`feedback_worktree_fonts`).

5. **No `open` commands.** Never use `open` to display files — it disrupts
   the user's screen (`feedback_no_open_commands`).

6. **`pkg/css/style.go` discipline — CRITICAL FOR PARALLEL SAFETY.**
   Round 1 has two agents both touching `pkg/css/style.go`. Rules:
   - You MAY add new getter/setter functions, new struct fields, new
     property constants, and new helper types **for the properties your
     plan declares**.
   - You MUST NOT rename, reorder, reformat, or refactor existing
     accessors, even if they look wrong.
   - You MUST NOT touch other plans' property surface area (css-lists
     touches `counter-*`/`list-*`; css-will-change touches `will-change`).
   - If an existing accessor needs a behavior change to make your phase
     work, surface that in your report as a "PARALLEL CONTENTION" note —
     do not edit it. The orchestrator decides whether to sequentialize.

7. **Test execution discipline (CLAUDE.md §4).** Run ONLY the 1-4 tests
   your phase targets, never the full section. Test runs are expensive.
   Each phase has a named gate test set in the plan. Use:
   `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/<section>/<name>' -v`
   Multiple names can be ORed with `|` inside the `-run` regex.

8. **gofmt before AND after Go edits** (`feedback_gofmt_after_edits`):
   - Before editing: `gofmt -l <file>` to confirm canonical tabbing matches
     what the Edit tool needs.
   - After editing: `gofmt -w <file>` and verify it stays clean.

9. **Foundational correctness (CLAUDE.md §1).** No point fixes. Each phase
   must fix ALL cases in its bucket. NEVER filter to "easy wins" or
   "nearly passing" tests. A 0.5% diff is a failure just like 28%. If a fix
   doesn't generalize, it's the wrong fix.

10. **0% diff required (CLAUDE.md §3).** WPT tests have built-in fuzzy
    tolerances set by the test authors. If the test is failing, the diff
    exceeds what they considered acceptable for rendering variation — it's
    a real bug. Never dismiss as "anti-aliasing".

11. **Study Blink first (CLAUDE.md §2).** The plan cites specific Blink
    files/lines at SHA `4883d11fef4a8713e32cd582ecef6dc5457c8c3f`. Read
    those citations via WebFetch
    (`https://chromium.googlesource.com/chromium/src/+/4883d11fef4a8713e32cd582ecef6dc5457c8c3f/<path>`)
    BEFORE implementing. They are the source of truth for types,
    algorithms, and constants.

12. **Commit + push at each phase boundary**, not just at end
    (`feedback_agent_checkpoints`). Every phase gate-passing commit is a
    durable artifact the orchestrator can inspect.

13. **DESIGN ESCALATION trigger.** If implementation reveals that the
    plan's approach doesn't fit louis14's actual code surface (e.g. the
    plan assumes a type/method that doesn't exist or has a different
    contract than described), commit your work-in-progress with a
    commit message starting with `WIP DESIGN ESCALATION:` and push.
    Report immediately with the specific surface mismatch. Do NOT
    improvise a different design — the orchestrator + user decide.

14. **No CSS area outside your plan.** Even if you see a bug in adjacent
    code that's obviously related, DO NOT fix it. Stay strictly inside
    your plan's files. Surface in the report as a "SCOPE NOTE."
```

---

## Brief 1A — `plan-css-will-change.md` Phase 1

```
You are an isolated worktree agent. Your task: implement Phase 1 of
`docs/plan-css-will-change.md`: the canonical `will-change` value model.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

Plan: $WT/docs/plan-css-will-change.md
Phase: Phase 1 ONLY ("The canonical `will-change` value model —
FOUNDATIONAL, blocks all others").
Baseline: 17 passing / 30 failing (47 run).

The plan was Blink-vetted on 2026-05-18 against Chromium main @
4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Read the "Blink vetting log"
section at the top of the plan — citations are SHA-pinned and accurate.

Phase 1 is FOUNDATIONAL: it builds the value model that Phases 2-5 route
through. It does NOT yet change visual output, so the "tests fixed" count
for Phase 1 alone is 0 — the gate is "no regression + the model compiles
and is ready for Phase 2".

Key new surface in `pkg/css/style.go` (additive only — see Hard Rule 6):
- `WillChange` type modeling the parsed value: `values: []AtomicString`,
  `resolved_longhand_ids` (a CSSPropertyID-like bitset), and the cached
  predicates `has_transform_property`, `has_any_transform_property`,
  `has_scroll_position_value`. Mirror `StyleWillChangeData` (Blink
  `third_party/blink/renderer/core/style/style_will_change_data.h` —
  read at the pinned SHA before coding).
- `Style.GetWillChangeData() *WillChange` accessor.
- `HasWillChange*` predicates aligned with current Blink's surface
  (per the vetting log): `HasWillChangeTransformProperty`,
  `HasWillChangeAnyTransformProperty`, `HasWillChangeScrollPosition`.
  Do NOT invent `*Hint` variants (the plan's vetting confirmed those
  don't exist in current Blink).
- Reuse the existing shorthand→longhand expander in `pkg/css/longhand.go`
  for the `will-change` longhand resolution — do NOT hand-roll a second
  table.

Crucially: also enumerate the canonical "creates stacking context" set
(per the plan's vetting log addition citing
`computed_style.cc:1319 HasPropertyThatCreatesStackingContext` at the
pinned SHA). Verbatim set at that SHA:
`kOpacity, kTransform, kTransformStyle, kPerspective, kTranslate, kRotate,
 kScale, kOffsetPath, kOffsetPosition, kMaskImage, kWebkitMaskBoxImageSource,
 kClipPath, kWebkitBoxReflect, kFilter, kBackdropFilter, kPosition,
 kMixBlendMode, kIsolation, kContain, kViewTransitionName, kZIndex
 (only if allows_z_index)`.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

**M0 — Setup**
- `pwd`, `git status`, `git rev-parse HEAD`, `git rev-parse --abbrev-ref HEAD`.
- Symlink fonts (Hard Rule 4).
- Read the entire plan doc (`docs/plan-css-will-change.md`).
- WebFetch each Blink citation from the plan's vetting log at the pinned SHA.
  Focus: `style_will_change_data.h`, `computed_style.cc:1319`,
  `computed_style.cc:2927`, `computed_style.h:1278-1292`.
- Commit + push your reading checklist as `$WT/.phase1-checklist.md`.
  Title: `chore(will-change): Phase 1 reading checklist`.

**M1 — Implement the WillChange value model**
- Add the new types and accessors to `pkg/css/style.go` (additive).
- Parser: extend the property parser in `pkg/css/style.go` (or
  `pkg/css/longhand.go`) to recognise the `will-change` syntax
  (comma-separated `<ident>` list; aliases resolve to longhand sets).
- Cache the predicate flags at parse time (cheap to query later).
- `gofmt -w` then `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...`
  must succeed.
- Run 1-2 representative tests from the regression set to confirm no
  existing behavior breaks (suggested: any `css-position` or
  `css-transforms` test that currently passes — they read transform CB
  logic, which Phase 1 must not perturb).
- Commit + push. Title: `feat(will-change): canonical value model (Phase 1)`.

**M2 — Phase 1 gate**
- Phase 1 has no visual gate of its own (it's pure scaffolding for
  Phase 2-5). The gate is:
  - `go build ./...` clean.
  - `gofmt -l pkg/css/...` empty.
  - 2-4 representative regression tests still pass (pick from
    currently-passing css-will-change, css-position, css-transforms).
  - The new value model is callable from where Phase 2 will plug it in
    (a smoke unit test or a written-out call site is fine).
- Delete `.phase1-checklist.md`.
- Commit + push if anything changed.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

- Worktree path and branch name.
- Pinned Blink SHA you referenced.
- Files added / modified, with line counts.
- Tests run (names) and their pass/fail status.
- **Any DESIGN ESCALATION** (Hard Rule 13).
- **Any PARALLEL CONTENTION note** (Hard Rule 6 edge case).
- **Any SCOPE NOTE** (Hard Rule 14 — adjacent bug spotted but not fixed).
- Phase 1 commit SHA.

State actual deltas, not predictions.
```

---

## Brief 1B — `plan-css-lists.md` Phase 1

```
You are an isolated worktree agent. Your task: implement Phase 1 of
`docs/plan-css-lists.md`: the counter scope tree (Bucket B1).

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

Plan: $WT/docs/plan-css-lists.md
Phase: Phase 1 ONLY ("Counter scope tree (B1)").
Baseline: 44 passing / 100 failing / 11 skipped (155 run).
Phase 1 target bucket: B1, ≈19 tests:
  counter-001..004, counters-001..006, counters-scope-001..004,
  counter-slot-order, counter-slot-order-scoping,
  counter-list-item-slot-order, counter-invalid.

The plan was Blink-vetted on 2026-05-18 against Chromium main @
4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Read the "Blink vetting log"
section at the top of the plan — citations are SHA-pinned and accurate.

Phase 1 is FOUNDATIONAL: it replaces the flat
`LayoutTreeBuilder.counters map[string][]int` with a Blink-faithful
`CountersAttachmentContext` (mirrors
`third_party/blink/renderer/core/css/counters_attachment_context.{h,cc}`).
B2/B3/B4 all depend on this single mechanism — get this right and the
next ~70 tests become tractable.

Files to create (mirror Blink file placement, per memory
`feedback_blink_file_placement`):
- NEW `pkg/css/counters_attachment_context.go` — mirror Blink
  `core/css/counters_attachment_context.{h,cc}`. Types:
  `CounterEntry{Origin *html.Node; Value int}`,
  `counterStack []*CounterEntry`,
  `CounterInheritanceTable = map[string]*counterStack` (pointer-to-stack;
  see the plan's vetting log — Blink uses pointer-to-stack semantics),
  `CountersAttachmentContext`.
- Methods (mirror Blink): `EnterObject`, `LeaveObject`,
  `ProcessCounter`, `CreateCounter`, `UpdateCounterValue`,
  `RemoveStaleCounters`, `RemoveCounterIfAncestorExists`,
  `GetCounterValues`.
- The `Type` enum is a BITMASK
  (`kIncrementType=1<<0, kResetType=1<<1, kSetType=1<<2`) — see the
  plan's vetting log for the resolution precedence (reset > set >
  increment per element, NOT a sequential pass).

Files to edit:
- `pkg/layout/layout_tree_builder.go`: delete
  `counters map[string][]int` (`:22`), `processCounterReset` (`:1200`),
  `processCounterIncrement` (`:1228`), `getCounterValue` (`:1260`).
  Thread a `*css.CountersAttachmentContext` through `buildNode`.
  Call `EnterObject` at the top of `buildNode` (after style resolution,
  before children) and `LeaveObject` after children. Pseudo-elements
  (`createPseudoElement`, `createMarkerPseudoElement`) call
  `EnterObject`/`LeaveObject` on their synthetic nodes in pseudo order
  (marker, before, children, after).
- `pkg/css/style.go` (additive only — see Hard Rule 6):
  `ParseContentValues` (`:5410`) add `case "counters"` for the
  `counters()` function (captures name, separator, optional style);
  extend `ContentValue` with `Separator` and `Style` fields.
- `pkg/css/style.go` `counter-reset` parser: store a parsed struct
  with the optional `reversed(name)` wrapper preserved for Phase 4 (do
  NOT implement reversed semantics now).

DO NOT touch (those are later phases):
- `counter-set` handling — Phase 2.
- `list-item` counter — Phase 3.
- `reversed()` semantics — Phase 4.
- `CounterStyle` / `@counter-style` — Phase 5.
- `::marker` box layout — Phase 6.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

**M0 — Setup**
- `pwd`, `git status`, `git rev-parse HEAD`, `git rev-parse --abbrev-ref HEAD`.
- Symlink fonts (Hard Rule 4).
- Read the entire plan doc (`docs/plan-css-lists.md`).
- WebFetch each Phase 1 Blink citation at the pinned SHA:
  - `core/css/counters_attachment_context.h` (full file)
  - `core/css/counters_attachment_context.cc` (functions named in
    the plan's vetting log — `ProcessCounter`, `CreateCounter`,
    `UpdateCounterValue`, `RemoveStaleCounters`,
    `RemoveCounterIfAncestorExists`, `GetCounterValues`, `EnterObject`,
    `LeaveObject`)
- Commit + push reading checklist as `$WT/.phase1-checklist.md`.
  Title: `chore(css-lists): Phase 1 reading checklist`.

**M1 — Implement the counter scope tree**
- Create `pkg/css/counters_attachment_context.go` with the types and
  methods above. Match Blink's pointer-to-stack semantics and bitmask
  Type enum exactly.
- Rewire `pkg/layout/layout_tree_builder.go` to use the new context.
- Add `counters()` function parsing in `pkg/css/style.go`
  (`ParseContentValues`) and resolve it via `ctx.GetCounterValues(name)`
  with the separator.
- Parse `counter-reset` to preserve the optional `reversed(name)` form
  for Phase 4 (parse only — no semantic effect this phase).
- `gofmt -w pkg/css/... pkg/layout/...`.
- `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...` clean.
- Commit + push. Title: `feat(css-lists): counter scope tree (Phase 1)`.

**M2 — Phase 1 gate**
- Run the B1 bucket (≈19 tests). All must pass at 0% diff.
  Test command:
  ```
  GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -v \
    -run 'TestWPTCSS3Reftests/css-lists/(counter-001|counter-002|counter-003|counter-004|counters-001|counters-002|counters-003|counters-004|counters-005|counters-006|counters-scope-001|counters-scope-002|counters-scope-003|counters-scope-004|counter-slot-order|counter-slot-order-scoping|counter-list-item-slot-order|counter-invalid)'
  ```
- Also run the 44 baseline-passing list/counter tests (sample at
  least 4) to confirm no regression — pick names from
  `docs/reftest-survey-2026-05-14-raw.txt` filtering for
  `PASS: TestWPTCSS3Reftests/css-lists/`.
- If any B1 test diff is > 0%, that's a Phase 1 failure. Diagnose,
  fix, re-run — do not move on until B1 is 0%.
- Delete `.phase1-checklist.md`.
- Commit + push. Title: `feat(css-lists): Phase 1 gate — B1 at 0% diff`.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

- Worktree path and branch name.
- Pinned Blink SHA you referenced.
- Files added / modified, with line counts.
- B1 test results (name + pass/fail + diff%).
- Baseline-passing regression sample (count run / count passed).
- **Any DESIGN ESCALATION** (Hard Rule 13).
- **Any PARALLEL CONTENTION note** (Hard Rule 6).
- **Any SCOPE NOTE** (Hard Rule 14).
- Phase 1 commit SHA(s).

State actual deltas, not predictions.
```

---

## Orchestrator post-flight (Round 1)

After both agents return:

1. Read each report. Surface any DESIGN ESCALATION first.
2. Verify each agent's branch in the orchestrator's tree:
   - `git fetch origin <agent-branch>`
   - `git log --oneline <base>..origin/<agent-branch>` — commits only on
     `worktree-agent-*`.
   - `git diff --stat <base>..origin/<agent-branch>` — files match plan
     "louis14 target files" + new files.
   - Spot-check `pkg/css/style.go` diff for additive-only edits
     (Hard Rule 6).
3. Merge cleanly-vetted agents with `--no-ff` and a summary commit.
   Push `origin master`.
4. If both merge cleanly, propose Round 2 (css-ruby solo) to user.
5. If either flags DESIGN ESCALATION, stop and surface — let the user
   decide whether the plan needs a re-design pass.

---

## Round 1 retrospective (post-merge, 2026-05-18)

Both Round 1 streams landed cleanly. Observations worth carrying forward:

- **The `pkg/css/style.go` additive-only rule worked.** Both agents touched
  the file; git auto-merged with no conflicts at the orchestrator-side
  `--no-ff` step. Keep this rule in every future brief.
- **css-lists Phase 1 surfaced 4 plan-bucketing errors in its B1 bucket**:
  counter-004 / counters-004 need georgian counter style (Phase 5 work);
  3 slot-order tests need Shadow DOM `<slot>` flattening (out of plan
  scope entirely). The plan listed them under B1 but B1's work doesn't fix
  them. Implementing agents should be empowered to surface this kind of
  discrepancy as a SCOPE NOTE rather than try to expand scope.
- **8 B4 accidental-pass collapses are now visible on master**
  (foo-counter-reversed-007a/b/009a/b, li-value-reversed-007a/b/009a/b).
  Foundationally correct — old code was broken-byte-identical on both
  sides; new code is correct on one side. Phase 4 will resolve.

---

## Brief 2 — `plan-css-ruby.md` Phase 1

Round 2: solo agent. Runs alone because of `inline_layout.go` /
`layout_tree_builder.go` contention with later text-decor work.

```
You are an isolated worktree agent. Your task: implement Phase 1 of
`docs/plan-css-ruby.md`: correct ruby UA styles, display model, and
box-fixup.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

Plan: $WT/docs/plan-css-ruby.md
Phase: Phase 1 ONLY ("Correct ruby UA styles, display model, and
box-fixup — FOUNDATIONAL").
Baseline: 24 passing / 51 failing (75 run) per the plan.

The plan was Blink-vetted on 2026-05-18 against Chromium main @
4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Read the "Blink vetting log"
section at the top of the plan — citations are SHA-pinned and accurate.

Phase 1 is FOUNDATIONAL: no layout yet, just the box tree and display
values must match Blink. It does NOT fix any css-ruby tests on its own,
but unblocks all 51 failures by removing the modeling errors. The gate
is "no regression in css-ruby below 24 passing, CSS2 99/99 unchanged,
display computed values for ruby/rt/rb/rbc/rtc/rp match Blink."

Key surface (see plan §Phase 1 for citations + line numbers):
- `pkg/css/style.go:4453-4532` — keep `DisplayRuby` and
  `DisplayRubyText`. **ADD `DisplayBlockRuby`** for the two-keyword
  `display: block ruby` form. **DELETE `DisplayRubyBase`** and any
  `ruby-base`/`ruby-base-container`/`ruby-text-container` parsing —
  these are NOT real display values in modern Blink. Update
  `ParseDisplay` for the two-keyword form per CSS Display L3.
- `pkg/css/cascade.go:162-183` — rewrite the ruby UA block to mirror
  Blink `html.css:1701-1720` exactly:
  - `ruby` → `display:ruby`
  - `rt` → `display:ruby-text` ONLY when parent is a ruby box
    + `font-size:50%` (Blink uses 50%, not 0.5em — `ruby-rt-fontsize-001`
    expects exactly half) + `text-align:start`
  - `rp` → `display:none` UNCONDITIONALLY (Blink does this via a flat
    element-only rule at `html.css:972-975`, not parent-scoped)
  - `rb`, `rbc`, `rtc` → NO UA display override
- `pkg/css/style.go` display-classification helpers (`IsInlineLevelDisplay`,
  `isBlockContainer`, `isBlockLevel`): treat `DisplayRuby`/`DisplayRubyText`
  as inline-level; `DisplayBlockRuby` as block-level.
- `pkg/layout/layout_tree_builder.go` — NEW `normalizeRubySubtrees(node)`
  invoked from `BuildLayoutTree`/`buildNode` analogous to
  `normalizeTableSubtrees` (`:44-63`). Two responsibilities:
  1. For `display: block ruby`: generate the two-box `LayoutRubyAsBlock`
     structure (block-flow principal box + anonymous inline `display:ruby`
     child holding the original children). Mirror `wrapAnonymousTableBoxes`
     with a new `css.NewAnonymousInlineRubyStyle`.
  2. Inlinification (§2.2): when a node's layout parent is `display:ruby`
     or `display:ruby-text`, set each in-flow child's used display to its
     `EquivalentInlineDisplay` (block→inline-block, table→inline-table,
     etc) and force `float:none`. Recurse.
- `pkg/layout/inline_layout.go:61-74` — `isInlineLevelDisplay` already
  lists the ruby displays; drop `DisplayRubyBase`, keep the others.

DO NOT touch (those are later phases):
- Ruby-column inline-item model (Phase 2).
- Intra-base white space (Phase 4).
- ruby-align / ruby-overhang (Phase 5).
- Autohiding (Phase 6).
- Anything in `pkg/layout/inline_item.go`, `pkg/layout/line_breaker.go`,
  `pkg/layout/fragment_builder.go` beyond what the items above strictly
  require. Phase 2 owns those.

`pkg/css/style.go` parallel-safety: same rule as Round 1 — additive
where possible. The DELETION of `DisplayRubyBase` is the exception (it
was a modeling error). Be careful that no other CSS-area work in flight
uses it. The Round 1 agents (css-will-change, css-lists) did not.
css-text-decor's Phase 5 will land later and explicitly does NOT depend
on `DisplayRubyBase` per its vetting log.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

**M0 — Setup + Blink reads**
- `pwd`, `git status`, `git rev-parse HEAD`, `git rev-parse --abbrev-ref HEAD`.
- Symlink fonts (Hard Rule 4).
- Read the entire plan doc (`docs/plan-css-ruby.md`).
- WebFetch each Phase 1 Blink citation at the pinned SHA:
  - `core/html/resources/html.css` lines 972-975 and 1701-1720
  - `core/css/resolver/style_adjuster.cc` `EquivalentBlockDisplay` /
    `EquivalentInlineDisplay`
  - `core/layout/layout_object.cc` ~lines 430-435 (object creation gate
    for ruby; verify exact line at the SHA)
  - `core/layout/layout_ruby_as_block.{cc,h}` (the block-ruby wrapper)
- Commit + push checklist as `$WT/.phase1-checklist.md`.
  Title: `chore(css-ruby): Phase 1 reading checklist`.

**M1 — Implement the box model changes**
- `pkg/css/style.go`: add `DisplayBlockRuby`, delete `DisplayRubyBase`,
  add two-keyword `display: block ruby` / `inline ruby` parse, update
  classification helpers.
- `pkg/css/cascade.go`: rewrite ruby UA block to mirror Blink exactly.
- `pkg/layout/layout_tree_builder.go`: add `normalizeRubySubtrees` +
  `inlinifyRubyChildren`. Invoke from `BuildLayoutTree`/`buildNode`.
- `pkg/layout/inline_layout.go`: drop `DisplayRubyBase` from
  `isInlineLevelDisplay`.
- Add `css.NewAnonymousInlineRubyStyle(parent)` helper.
- `gofmt -w pkg/css/... pkg/layout/...`.
- `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...` clean.
- Commit + push. Title: `feat(css-ruby): UA styles + display model
  + box-fixup (Phase 1)`.

**M2 — Phase 1 gate**
- Run 2-3 representative tests from currently-passing css-ruby (24
  baseline) to confirm no regression. Pick names by filtering
  `docs/reftest-survey-2026-05-14-raw.txt` for `PASS:
  TestWPTCSS3Reftests/css-ruby/`. Use 2-3 names, not the whole set.
- Run 1-2 CSS2 tests (any currently-passing) for regression sample.
- Hand-verify in code that `ruby/rt/rb/rbc/rtc/rp` produce the correct
  display values at parse + cascade (write a small unit test if
  practical; otherwise reason from the cascade output for a 3-line
  HTML fixture in your report).
- Phase 1 does NOT fix any css-ruby tests on its own. Do NOT run the
  whole css-ruby section.
- Delete `.phase1-checklist.md`.
- Commit + push. Title: `feat(css-ruby): Phase 1 gate — display
  model verified, no regressions`.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

- Worktree path and branch name.
- Pinned Blink SHA you referenced.
- Files added / modified, with line counts.
- Tests run (names) + pass/fail status. Note: Phase 1 does NOT add new
  passes; the gate is "no regression."
- Computed-display values for ruby/rt/rb/rbc/rtc/rp on a sample DOM
  (either via a small unit test or reasoned-out from cascade.go).
- **Any DESIGN ESCALATION** (Hard Rule 13).
- **Any PARALLEL CONTENTION note** (Hard Rule 6 — particularly note if
  the `DisplayRubyBase` deletion surfaces a caller in unexpected code).
- **Any SCOPE NOTE** (Hard Rule 14).
- Phase 1 commit SHA.

State actual deltas, not predictions.
```

---

---

## Round 2 retrospective (post-merge, 2026-05-18)

css-ruby Phase 1 landed cleanly with three Phase-1 commits + unit tests.
Observations:

- The deletion of `DisplayRubyBase` (plan-authorized — it's not in modern
  Blink) required two 1-line edits in `louis13/pkg/layout/*.go` to keep
  the build clean. The agent flagged this as a SCOPE NOTE and kept the
  edits minimal + behavior-preserving. Acceptable; the alternative (leave
  the build broken) was worse than touching adjacent files.
- The agent correctly mirrored Blink's `font-size: 50%` (not `0.5em`) per
  the vetting log — this is the kind of detail that gets lost without
  SHA-pinned citations.
- `normalizeRubySubtrees` slotted into `BuildLayoutTree` analogously to
  `normalizeTableSubtrees`. The "study Blink and mirror its file
  placement" principle keeps producing tractable diffs.

Dependency note: css-text-decor Phase 5 (ruby integration for emphasis)
will eventually need css-ruby's Phase 2 (inline ruby-column model). But
**css-text-decor Phase 1 (the AppliedTextDecoration model) does NOT depend
on ruby** — it's the cascade fix + value model only. Round 3 can run in
parallel with the LOU-128+LOU-129 landing merge.

---

## Brief 3 — `plan-css-text-decor.md` Phase 1

Round 3. Runs in parallel with the LOU-128+LOU-129 landing merge agent
(different files). Subsequent text-decor phases (2-4) are solo; Phase 5+
gates on css-ruby Phase 2.

```
You are an isolated worktree agent. Your task: implement Phase 1 of
`docs/plan-css-text-decor.md`: replace inherited text-decoration with
the AppliedTextDecoration accumulating model.

[INSERT THE FULL "HARD RULES" BLOCK FROM THIS DOC HERE]

═══════════════════════════════════════════════════════════════
PLAN-SPECIFIC SCOPE
═══════════════════════════════════════════════════════════════

Plan: $WT/docs/plan-css-text-decor.md
Phase: Phase 1 ONLY ("Decoration model: stop inheriting, introduce
AppliedTextDecoration").
Baseline: 96 passing / 154 failing / 0 skipped (250 run) per the plan.

The plan was Blink-vetted on 2026-05-18 against Chromium main @
4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Read the "Blink vetting log"
section at the top of the plan — citations are SHA-pinned and accurate.
NOTE: the vetting log calibrates that `.cc` file line numbers in this
plan drift ~250-530 lines from cited values; header line numbers are
within ±5 lines. Phase 1 cites mostly headers (applied_text_decoration.h)
so the citations should be close.

Phase 1 is FOUNDATIONAL: it replaces the single inherited TextDecoration
enum with an accumulating `[]AppliedTextDecoration` model. It targets
~5 tests directly (text-decoration-line, text-decoration-line-011/012/013,
text-decoration-color) but the bigger payoff is unblocking Phases 2-4
(geometry, L4 knobs, decorating-box propagation) and Phase 6 (emphasis).

Key new surface in `pkg/css/style.go` (additive — see Hard Rule 6):
- Type `TextDecorationLine` (bitfield: `Underline | Overline | LineThrough`)
- Type `TextDecorationThickness { Kind: Auto|FromFont|Length; Value Length }`
- Type `AppliedTextDecoration { Lines TextDecorationLine; Style string;
  Color Color; HasColor bool; Thickness TextDecorationThickness;
  UnderlineOffset Length }` — mirrors Blink
  `core/style/applied_text_decoration.h` (lines 18-48 at the pinned SHA;
  fields lines_/style_/color_/thickness_/underline_offset_ per vetting
  log).
- Accessor `Style.GetAppliedTextDecorations() []AppliedTextDecoration`.

Cascade fix in `pkg/css/cascade.go`:
- DELETE `"text-decoration"` from `inheritableProperties` (cascade.go:843
  per the plan — verify the current line number; it may have shifted).
  This is the central Bucket A bug.
- During cascade, compute each element's OWN contributed
  AppliedTextDecoration from its `text-decoration-line/style/color/
  thickness` longhands + `text-underline-offset`. Append it to the
  parent's resolved vector to form this element's vector. The Phase 4
  work wires the boundary logic into layout; for Phase 1, just store
  the per-element contribution.
- Expand the `text-decoration` shorthand into the four longhands.
- Keep `<u>`/`<ins>`/`<a>` UA defaults at `cascade.go:30` (per the plan)
  but EXPRESSED AS `text-decoration-line` — the underline becomes a
  longhand contribution, not the legacy enum value.

DO NOT touch (later phases):
- TextDecorationInfo geometry — Phase 2.
- L4 knobs (text-underline-position, from-font, text-decoration-inset) —
  Phase 3.
- Decorating-box propagation through inline tree — Phase 4.
- Ruby layout — Phase 5 (and gates on css-ruby Phase 2).
- Emphasis painting — Phase 6.
- skip-ink / skip-spaces — Phase 7.

`pkg/css/cascade.go` parallel-safety: this file has been touched by the
css-ruby Phase 1 agent (already merged) to rewrite the ruby UA block. Do
NOT touch the ruby UA block. The text-decoration changes are independent
(cascade list at `inheritableProperties`, plus the shorthand expander
for `text-decoration`). Stay strictly in your area.

`pkg/css/style.go` parallel-safety: same additive-only rule as Round 1.
You're running concurrently with a landing-merge agent that will be
pulling in LOU-129's changes to style.go (additions of
`splitTopLevelCommas` etc). Both yours and theirs are additive at the
file tail. If your branch and theirs need to merge later, git will
auto-merge cleanly as it did in Round 1.

═══════════════════════════════════════════════════════════════
MILESTONES
═══════════════════════════════════════════════════════════════

**M0 — Setup + Blink reads**
- `pwd`, `git status`, `git rev-parse HEAD`, `git rev-parse --abbrev-ref HEAD`.
- Symlink fonts.
- Read the entire plan doc (`docs/plan-css-text-decor.md`).
- WebFetch each Phase 1 Blink citation at the pinned SHA. Focus:
  - `core/style/applied_text_decoration.h` (lines 18-50)
  - `core/style/computed_style.cc` (style-builder behavior that appends
    AppliedTextDecoration rather than inheriting — search for the
    StyleAdjuster / RecalcStyle path)
- Commit + push checklist as `$WT/.phase1-checklist.md`.
  Title: `chore(css-text-decor): Phase 1 reading checklist`.

**M1 — Implement the model + cascade fix**
- Add the new types and accessors to `pkg/css/style.go` (additive).
- Add the `text-decoration` shorthand expander.
- Delete `"text-decoration"` from `inheritableProperties` in
  `pkg/css/cascade.go`.
- Wire the per-element AppliedTextDecoration computation + accumulation.
- Re-express `<u>`/`<ins>`/`<a>` UA defaults as text-decoration-line
  contributions.
- `gofmt -w pkg/css/...`.
- `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...` clean.
- Commit + push. Title: `feat(css-text-decor): AppliedTextDecoration
  model + stop inheriting (Phase 1)`.

**M2 — Phase 1 gate**
- Run the 5 plan-cited Phase-1 tests at 0% diff:
  - text-decoration-line.html
  - text-decoration-line-011.xht
  - text-decoration-line-012.xht
  - text-decoration-line-013.xht
  - text-decoration-color.html
- Run a regression sample (2-3 currently-passing css-text-decor + 2
  css-text + 2 CSS2 tests). NO regressions.
- If any of the 5 Phase-1 tests don't reach 0%, diagnose and fix —
  don't move on with diffs > 0.
- Delete `.phase1-checklist.md`.
- Commit + push. Title: `feat(css-text-decor): Phase 1 gate — 5 tests
  at 0% diff`.

═══════════════════════════════════════════════════════════════
REPORT
═══════════════════════════════════════════════════════════════

- Worktree path and branch name.
- Pinned Blink SHA.
- Files added / modified, with line counts.
- 5 Phase-1 test results (name + pass/fail + max diff).
- Regression sample results.
- Any DESIGN ESCALATION / PARALLEL CONTENTION / SCOPE NOTE.
- Phase 1 commit SHA.

State actual deltas, not predictions.
```
