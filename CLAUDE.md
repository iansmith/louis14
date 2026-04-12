# Louis14 Browser Engine — Project Principles

These principles are non-negotiable. Follow them exactly.

## 1. Foundational correctness over quick wins
NEVER look for low-hanging fruit, near-passing tests, or easy wins. Every fix must
work for ALL cases. Don't filter tests by error percentage or chase "nearly passing"
tests. If a fix doesn't generalize to all cases, it's the wrong fix. A point fix
now will not help and will likely make things worse later. Pick a category and solve
it completely — don't skip around looking for something "more tractable."

## 2. Study Blink BEFORE writing any code
When starting work on a new area, the FIRST step is to look at what Blink/Chromium
does. Study their abstractions, algorithms, and types. Only then write code. Mirror
their type names, algorithm structure, and constraint-passing patterns. The louis14
codebase is modeled on Blink's LayoutNG — keep it aligned.

## 3. All tests must pass
Do not treat small pixel diffs as acceptable. ALL tests must pass at 0% diff. A
0.5% diff is a failure just like 28%. Never dismiss failures as "font rendering"
or "anti-aliasing" — the WPT tests have built-in fuzzy tolerances provided by the
test authors specifically to account for text rendering differences. If a test is
failing, the diff exceeds what the test author considered acceptable for rendering
variation, which means it's a real bug. Identify the systemic issue and fix it with
correct foundational code.

## 4. Test execution discipline
Do not run the full test suite or even the full section test suite during feature
work. Run only the specific tests associated with the feature being worked on —
typically 1 to 4 tests. Broader test runs are expensive and should only happen when
explicitly requested.

## 5. Operational rules
- Never use `open` to display files from agents — it disrupts the user's screen.
- Always commit and push before launching worktree agents — worktrees start from
  HEAD, not the working directory.
- Instruct agents to commit and report at each milestone, not just at the end.
- When running in a worktree (any directory that is NOT ~/louis14), commit ONLY to
  your worktree branch. Never commit directly to fix/* or master branches from a
  worktree.
