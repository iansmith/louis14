# Louis14

Browser engine modeled on Blink's LayoutNG. Written in Go.

Depends on **mazzy** (`mazarin/textshape`) via a `go.mod` replace pointing at `~/mazzy/mazarin/textshape`. Concurrent development happens in `~/mazzy`; see the sibling-project block below before editing anything in that tree.

---

## Universal Project Rules

These live in `CLAUDE-universal.md` alongside this file — one mirrored copy per project,
byte-identical everywhere. **Edit them in the slopstop repo (the reference copy) and
propagate; never edit the copy in this repo.** Project-specific rules and deliberate
overrides go below, in this file, where they take precedence.

@CLAUDE-universal.md

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

`~/louis14` owns `master` directly — a single working tree (HEAD on `master`). Ship tickets **serially** from here via the `/slopstop-*` pipeline; branch with `git switch -c <type>/LOU-N origin/master`. (The former `~/louis14-campaign` orchestration tree + per-ticket lane worktrees were removed 2026-06-21 — see the `swarm-approach-abandoned` memory; don't resurrect that approach without first solving its integration problems.)

If you DO spin up a worktree (e.g. an `isolation: "worktree"` agent, or `/code-review ultra`), it needs two symlinks before any non-targeted test sweep, or it hits catastrophic false regressions:

```
ln -s /Users/iansmith/louis14/fonts/* $WORKTREE/fonts/
ln -sfn /Users/iansmith/mazzy "$WORKTREE/../mazzy"
```

- **`fonts/`** — only `Ahem.ttf` is committed; the 180-file Liberation/Atkinson set is gitignored, so a fresh worktree has almost no fonts and ~50% of writing-modes / 25 multicol / 6 flexbox tests fail with mis-rendered text. The Ahem collision on symlinking is harmless.
- **`../mazzy`** — `go.mod` has `replace mazarin/textshape => ../mazzy/mazarin/textshape`. From `~/louis14/.claude/worktrees/agent-*/`, `../mazzy` resolves to `~/louis14/.claude/worktrees/mazzy`, which doesn't exist without the symlink; `go test` fails to build with module errors.

- A worktree agent must write **only** under its own worktree root, never the main `~/louis14` tree — every absolute path in Edit / Write / `>`-redirect / `sed -i` / `mv` / `cp` / `gofmt -w` / `go build -o` must start with the worktree path. An Edit at the bare main-tree path is a silent no-op for the agent's branch (zero commits) and contaminates the main tree's `git status`. Pass the worktree root explicitly in the launch brief and have the agent verify `pwd` before writing.
