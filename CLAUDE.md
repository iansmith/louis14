# Louis14

Browser engine modeled on Blink's LayoutNG. Written in Go.

Depends on **mazzy** (`mazarin/textshape`) via a `go.mod` replace pointing at `~/mazzy/mazarin/textshape`. Concurrent development happens in `~/mazzy`; see the sibling-project block below before editing anything in that tree.

---

# Universal Project Rules

These rules apply across all of Ian's projects unless this CLAUDE.md explicitly overrides them.

## 1. Pre-commit

- **ALWAYS run `/simplify` on uncommitted changes before every commit.** No exceptions on size — a one-line change can introduce a duplicate constant, touch the wrong file, or violate a project rule, all of which `/simplify` catches cheaply. Apply real findings inline before committing.
- Run the project's build + targeted tests (the package or area you touched) before commit. Run the full suite only when touching shared/cross-cutting code.
- Commit, then push — only after the above are clean. **If the project has multiple remotes, push to all of them.**

## 2. Tests

- **Tests-first for new behavior AND for fixes.** For new behavior, write the test describing the desired contract; confirm it's red **for the right reason** before implementing. For bug fixes, write a test that reproduces the bug — it must be red before the fix and green after. Trivial tweaks, copy changes, and pure refactors are exempt.
- **A failing test is signal, not chore.** Investigate the root cause before changing anything. Never delete a test, narrow an assertion, call `Skip()`, or cite an unverified "flake" to silence it. "Known flake" is a label, not an explanation.

(Test scope before commit is covered by §1. Project-specific guidance on test runtime and scoping lives in each project's CLAUDE.md.)

## 3. Git

- **NEVER squash-merge or rebase-merge.** Use `gh pr merge --merge` (real merge commit). Squash and rebase lose fixup context and break `git bisect`.
- Always include the explicit branch name in `git push origin <branch>`.
- Never `git push --force`, `git reset --hard`, `git commit --no-verify`, `git push --no-verify`, or `gh pr merge --admin` unless the user explicitly asks. When a hook or check fails, fix the underlying issue, don't bypass.
- Create new commits rather than amending. The single exception: amending one fresh commit on a solo branch before anyone has pulled it.

## 4. Refactoring scope

- **Dedupe is in scope.** If you find 2+ near-identical code paths while working on a change, extract the helper and migrate the duplicates in the same PR.
- **Structural changes are out of scope without discussion.** Renaming exported symbols, altering public signatures, moving files, or reshaping module boundaries must be raised separately.
- When extending an existing system, study its types and patterns first. Mirror existing vocabulary; don't invent parallel terms for the same concept.
- Foundational correctness over quick wins. "Nearly passing" is failing. When working through a category of failures, **don't declare done by cherry-picking the easy cases** — solve the problem completely.

## 5. Source of truth

- **One definition per value.** No duplicate constants, aliases, or parallel names. If something needs renaming, update every reference — never add an alias.
- Never edit generated files by hand. Edit the source and regenerate.

## 6. Agents and worktrees

### Coordinator rules — how to behave when running agents

- Commit and push before launching worktree agents — worktrees start from HEAD, not the working directory.
- **Aim for fine-grained milestones** — frequent enough that progress is visible (rough target: a check-in every few minutes of work), but not so frequent that the output becomes noise. Every 10 seconds is too often; every 20 minutes is too long.
- **Aim for parallelism that won't cause merge-back conflicts on the base branch.** If the work can't be cleanly parallelized, consider whether sequential agent offload is actually worth the overhead — small tasks belong on your own plate; genuinely large offloads (long builds, multi-file refactors you'd otherwise wait on) can still be a win even when sequential.
- **Never use `open` to display files unless the user explicitly asks.** Disruptive even from the main session.

### Agent instructions — what to include in every agent prompt

- **Run on a separate branch in a separate directory.** Before working, prepare the directory if the project requires it — e.g., symlink large, rarely-changing directories that aren't under git control from the worktree to their original location, so the agent has its dependencies without duplicating them.
- **Commit only to your worktree's branch.** Never touch `main`/`master` or other shared branches from a worktree.
- **Commit and report at every milestone, not just at the end.**
- **Never use `open` to display files** (disrupts the user's screen).
- **Restate the relevant project rules verbatim in the prompt.** Agents start with no prior context and won't follow rules they don't see.

## 7. Environment

- Never modify PATH manually. If the project has special path or environment requirements, ask the user the first time, then save them to memory for that project so subsequent sessions pick them up automatically.

## 8. Documentation directory layout (universal)

- `docs/` is **gitignored** — used for personal notes, scratch work, drafts. Not committed.
- `design/` is **tracked**, but you do **not** add files to it without explicit user confirmation. Design docs are deliberate artifacts.
- Files specific to a particular ticket (continuation prompts, mid-flight notes, ticket-local plans) go into the **ticket's local storage directory** (`~/.claude/ticket-active/<TICKET>/`), not into `docs/` or `design/`.

## 9. CodeRabbit (universal)

- Every project should have at least one remote that can be used with CodeRabbit. `/simplify`'s pre-commit role is to preempt CodeRabbit findings, not to substitute for the actual review.
- When the project has multiple remotes, **prefer the GitHub remote** for CodeRabbit. **CodeRabbit does not work on Bitbucket**; if Bitbucket is the only remote, factor that into the review plan separately.

## 10. Adding a new rule — where it lives

- **Project-specific operational tip or bug record** → `feedback_*.md` in this project's memory dir; index it in `MEMORY.md`. Default home for new learnings.
- **Project-specific rule every session must follow** → the project-specific section of this `CLAUDE.md`. Delete the memory file if it would duplicate.
- **Universal rule applying to every project of Ian's** → propose adding to the universal §1-§10 block of all four projects' `CLAUDE.md` files identically. Don't drift one project's universal block.

Promotion is one-way: memory → project-specific → universal. Rules go up when they prove durable.

---

# Louis14-Specific Declarations

## Go toolchain

Always invoke Go as:

```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go <command>
```

`go.mod`'s `go` directive trails the toolchain that transitive deps (notably `mazarin/textshape`) require, so plain `go` or the system Go fails with version-parse errors. Do not bump `go.mod`'s `go` directive to work around this — the committed version is intentional.

## Test execution discipline (project-specific override of universal §1's "full suite" allowance)

- During feature work, run only the specific tests associated with the feature — typically **1 to 4 tests**. The full suite, and even section-level suites (full css-flexbox, full css-writing-modes), is expensive and runs only on explicit request. This narrows universal §1's "full suite when touching shared/cross-cutting code" allowance.
- **ALL tests must pass at 0% diff.** A 0.5% diff is a failure just like 28%.
- **Anti-pattern:** dismissing failures as "font rendering" or "anti-aliasing." WPT tests have built-in fuzzy tolerances chosen by the test authors specifically to account for rendering variation. If a diff exceeds them, it's a real bug — find the systemic cause.

## Blink as canonical reference

The louis14 codebase is modeled on Blink's LayoutNG. When starting work on a new area — coding **or** planning — the first step is to look at what Blink/Chromium does: types, algorithms, abstractions. Mirror their type names, algorithm structure, and constraint-passing patterns; don't invent parallel vocabulary for a concept Blink already names. A plan written without a Blink survey is not an acceptable plan for this codebase.

**The Blink survey is per work item, not a one-time chapter at the top of the plan.** Every numbered work item — in `/ticket-plan` or any other plan doc — must independently cite the Blink type, algorithm, or file it mirrors, or explicitly note `no Blink analog: <reason>` if the item is genuinely louis14-specific. An upfront-only survey lets piecemeal items drift from Blink even when the plan's introduction looks aligned. For `/ticket-plan` specifically: Step 1c (the Explore investigation) gathers these citations, and Step 2b's work items surface them in the "Detailed steps" block — SHA-pinned per the citation discipline below.

When porting a Blink primitive, place it in the louis14 package that mirrors Blink's *source location*, not wherever a phase brief literally says. E.g. `platform/geometry/layout_unit.h` → `pkg/geometry/layoutunit`; `core/layout/geometry/physical_rect.h` → `pkg/geometry`. Blink's factoring is load-bearing.

### Blink citation discipline

When a louis14 plan doc cites Blink code (paths, line numbers, type names, behavior claims), the citation **MUST** include a pinned Chromium commit SHA against which it was verified — not just `path:line`.

**Why:** Line numbers drift fast. The 2026-05-18 vetting pass of four plan docs (css-lists, css-ruby, css-text-decor, css-will-change) found that:

- `.cc` file line numbers had drifted 250–1260 lines from the cited values.
- One plan cited the wrong file entirely (`style_adjuster.cc` for SC attribution; actual is `computed_style.cc:2927` → `:1319 HasPropertyThatCreatesStackingContext`).
- One type (`LayoutRubyColumn`) was removed outright; modern Blink ruby is inline-based (`InlineItemResultRubyColumn` / `LogicalRubyColumn`).

Line numbers alone become noise within ~6 months. SHA-pinned citations make staleness auditable and re-verification mechanical.

**How to apply:**

- When writing or editing a plan doc, every Blink reference block declares the Blink commit SHA it was verified against (e.g. a `## Blink vetting log` section near the top: "Vetted against Chromium `main` @ <SHA> on <date>").
- When citing a symbol, include both `file:line` AND the SHA in the reference.
- When picking up a plan that's older than ~3 months, vet citations against current Blink HEAD before relying on them.

## Sibling project: mazzy

- Louis14 depends on mazzy via `replace mazarin/textshape => ../mazzy/mazarin/textshape` in `go.mod`. Concurrent development happens in `~/mazzy`.
- Treat `~/mazzy` files as **read-only by default**. Read freely; never `Edit`/`Write` without naming the file and intent in chat and getting explicit per-file go-ahead first. Silent edits cause merge conflicts.
- Worktrees need a `../mazzy` symlink — see Worktree setup below.

## Worktree setup

Worktree agents need two symlinks before any non-targeted test sweep, or they hit catastrophic false regressions:

```
ln -s /Users/iansmith/louis14/fonts/* $WORKTREE/fonts/
ln -sfn /Users/iansmith/mazzy "$WORKTREE/../mazzy"
```

- **`fonts/`** — only `Ahem.ttf` is committed; the 180-file Liberation/Atkinson set is gitignored, so a fresh worktree has almost no fonts and ~50% of writing-modes / 25 multicol / 6 flexbox tests fail with mis-rendered text. The Ahem collision on symlinking is harmless.
- **`../mazzy`** — `go.mod` has `replace mazarin/textshape => ../mazzy/mazarin/textshape`. From `~/louis14/.claude/worktrees/agent-*/`, `../mazzy` resolves to `~/louis14/.claude/worktrees/mazzy`, which doesn't exist without the symlink; `go test` fails to build with module errors.

Agents working in a worktree must also:

- Anchor every absolute file-tool path under the worktree root, NOT `/Users/iansmith/louis14/...`. Edit/Write take absolute paths and have no default base; unqualified `/Users/iansmith/louis14/...` writes to the orchestrator's tree, silently no-op'ing the worktree.
- Receive the worktree root explicitly in their brief.
- Run `pwd` + `git rev-parse --show-toplevel` as a sanity check at start; if either reports `/Users/iansmith/louis14`, stop.
