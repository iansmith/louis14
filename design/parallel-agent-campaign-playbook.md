# Parallel agent campaign playbook

Captured 2026-05-25 after a sustained run that took louis14's WPT CSS3 pass count from ~4134 to ~4457 in one orchestration session (~+323 tests across ~32 real-merge commits, ~7 hours of wall time). This document captures **what made it work**, **what broke**, and **the exact information every plan needs** for the next time we want to run this pattern.

## Outcome of the run

| Metric | Start (mid-campaign 18:20) | End | Δ |
|---|---:|---:|---:|
| WPT CSS3 tests passing | 4134 / 6722 | 4457 / 6722 | **+323** |
| WPT CSS3 tests failing | 2447 | 2124 | **−323** |
| Real-merge commits (this session) | — | 32 | — |
| Investigation-only no-commits | — | 2 (W4.11, W13.38) | — |
| Regressions accepted | — | ~1 (li-value-reversed-009a, pre-existing exposed) | — |

The pattern is reproducible. The rest of this document is the recipe.

---

## 1. Topology

```
                            ┌──────────────────────┐
                            │  Orchestrator        │
                            │  (this Claude)       │
                            └─┬───────┬──────────┬─┘
                              │       │          │
                ┌─────────────┘       │          └──────────────┐
                ▼                     ▼                         ▼
        ┌──────────────┐      ┌──────────────┐         ┌──────────────┐
        │ Survey agent │      │ Impl agent   │   ...   │ Impl agent   │
        │ (Explore,    │      │ (general,    │         │ (general,    │
        │  no commit)  │      │  worktree)   │         │  worktree)   │
        └──────┬───────┘      └──────┬───────┘         └──────┬───────┘
               │                     │                        │
               │                     ▼                        ▼
               │             ┌──────────────────┐    ┌──────────────────┐
               │             │ worktree branch  │    │ worktree branch  │
               │             │ agent/NN-...     │    │ agent/NN-...     │
               │             └────────┬─────────┘    └────────┬─────────┘
               │                      │                       │
               │                      └───────┬───────────────┘
               │                              │
               ▼                              ▼
        ┌──────────────────────────────────────────┐
        │ Orchestrator merges back into master     │
        │ (real --no-ff merge, hand-resolve        │
        │  conflicts inline, push every merge)     │
        └──────────────────────────────────────────┘
```

Key invariants:
- **Master is the only branch ever pushed.** Agents commit to their own `agent/NN-...` branches and push them to origin so the orchestrator can fetch and merge.
- **Orchestrator never edits in agent worktrees, and agents never edit master.**
- **Every merge is `--no-ff` with a hand-written message.** No squash, no rebase. This preserves the foundation: when something regresses later, `git bisect` lands inside the single agent's commit, not on a mashed-together meta-commit.

---

## 2. Per-agent durable state (survives compaction)

Everything an agent and orchestrator might need outlives the conversation:

```
/tmp/campaign/
├── ledger.md                    # orchestrator's running log: SHAs, merges, in-flight roster
├── all-failures.txt             # initial baseline of failing tests per region (pipe-separated)
├── raw-baseline.txt             # initial full survey output
└── agents/
    ├── 01-css-cascade-all/
    │   ├── plan.md              # orchestrator-written plan (authoritative)
    │   └── progress.md          # agent appends milestone notes
    ├── 27-css-multicol-column-rule/
    │   ├── plan.md
    │   └── progress.md
    └── ...
```

**Why this matters.** A conversation that's been `/compact`'d still has these files. When the orchestrator wakes up post-compaction, it can re-read the ledger and know:
- which master SHA is current
- which agents are in flight (by ID + branch + plan path)
- what's been merged and what hasn't
- which categories are exhausted

If a single agent's completion notification gets lost (rate-limit truncation, network hiccup), the orchestrator can still pick up its work by reading the agent's `progress.md` and inspecting the agent's branch on origin.

The numbered prefix (`27-css-multicol-...`) gives stable ordering and lets the directory listing serve as a chronological index.

---

## 3. Orchestrator's job in one paragraph

Walk the failure list. Pick a cluster. Survey it (Explore agent, read-only, returns drop-in plan sections). Write the plan. Launch a worktree impl agent with the plan and the full ruleset. While agents work, build the next 1–2 plans. When an agent completes, judge: merge worthy? Real foundation, no regressions, scope respected. If yes, `git merge --no-ff origin/<branch>`, resolve any conflict inline, push master. Update ledger. Repeat. Cap at ~10–12 concurrent agents. Stagger launches.

The orchestrator does **NOT** review the agent's transcript — the agent writes a DONE block at the end (see §6), and that block plus a one-glance `git diff master..origin/<branch>` is the gate.

---

## 4. What the orchestrator needs **before** writing a plan

Reading order, fastest path to a confident plan:

1. **The failing test cluster.** `grep "^<region>" /tmp/campaign/all-failures.txt | grep <pattern>`. Want 5–25 tests in a tight cluster (same prefix, similar shape). Below 5 → not worth the orchestration overhead. Above 25 → probably crosses several features, scope it down or split into multiple agents.

2. **One sample test's content.** `head -50 <test.html>`. Confirms what CSS feature the cluster actually exercises (sometimes the filename lies).

3. **Current louis14 state for that feature.** Two greps:
   - `grep -n "<PropertyName>\|<EnumName>" pkg/css/style.go pkg/css/stylesheet.go` — is the property even parsed?
   - `grep -rn "<feature>" pkg/render/ pkg/layout/` — is it consumed downstream?
   This tells you whether the fix is parser, accessor, cascade, layout, or paint — and roughly how foundational it has to be.

4. **The Blink reference at the pinned SHA.** louis14 mirrors Blink LayoutNG. **Every plan cites Blink at SHA `4883d11fef4a8713e32cd582ecef6dc5457c8c3f` with `file:line` (or symbol).** Without this the plan isn't acceptable — agents will improvise their own architecture, and the patterns won't compose.

5. **Sibling work that might collide.** Check the in-flight roster. If another agent is in cascade.go, don't launch a second cascade.go agent — they'll conflict on merge. Pick a different region.

6. **Whether fixtures might be missing.** For every category that involves images:
   ```sh
   grep -rohE 'url\((support|reference)/[^)]+\)' *.html *.xht | sed -E 's/url\((.*)\)/\1/' | sort -u
   ```
   compared against the actual contents of `support/`. Often a whole cluster fails not because of a code bug but because the WPT fixture was never vendored. This was W5.14 (SVG) and W13.34 (PNGs); pattern is high-leverage.

---

## 5. Plan template — every section is required

Save to `/tmp/campaign/agents/<NN>-<topic>/plan.md`. Filling the template is the orchestrator's main creative work; everything else is mechanics.

```markdown
# Agent W<phase>.<num> — <category> <feature> (<spec-section-ref>)

## Target tests (≈N)

**Cluster A — M tests:**
- pkg/visualtest/testdata/wpt-css3/<category>/<test-1>.html
- <test-2>.html
- ...

**Cluster B — K tests:**
- <test-1>.html
- ...

(Tell the agent to confirm the exact list at start, e.g. `grep "^<category>" /tmp/campaign/all-failures.txt | grep <pattern>`. The baseline drifts over the campaign.)

## Spec rule

<one-paragraph summary of the CSS spec rule, with section reference>
<short table of cases or grammar where useful>

## Blink reference @ `4883d11fef4a8713e32cd582ecef6dc5457c8c3f`

- `third_party/blink/renderer/<path>.cc` — `<Symbol>`. <One line on what it does.>
- `third_party/blink/renderer/<path>.h` — enum/struct definition.
- URL: https://chromium.googlesource.com/chromium/src/+/4883d11fef4a8713e32cd582ecef6dc5457c8c3f/...

<One paragraph naming the Blink contract: the algorithm/invariant the agent should mirror.>

## Current louis14 state

`pkg/<...>:LINE` — what exists, what's missing. Specific file + line. Be precise: "X is parsed but never consulted at Y" beats "X needs implementing".

## Implementation plan

### Milestone 1 — Reproduce
Run all target tests; confirm baseline failures. Capture diff for at least one to understand failure mode.

### Milestone 2 — <first concrete step>
<5–15 lines of pseudo-code or specific instructions>

### Milestone 3 — <next concrete step>
...

### Milestone N — Verify
- All target tests pass at 0% diff.
- Run full <category> region for regressions.
- Run <adjacent-category> smoke (because of overlap X).

### Milestone N+1 — Commit + push.

## Scope guards
- DO NOT touch <unrelated subsystem> — separate work.
- DO NOT duplicate <existing helper> — reuse it.
- If <surprise condition>, STOP and produce no-commit investigation report.

## Rules: see /tmp/campaign/agents/01-css-cascade-all/plan.md "Project rules" section. Apply VERBATIM.

**CAMPAIGN RULE (HARD): NO `git stash` from this worktree.**

Report to `/tmp/campaign/agents/<NN>-<topic>/progress.md`.

## DONE entry format
```
## DONE at HH:MM:SS
- Branch: <name>
- Final commit SHA: <sha>
- <target metric 1>: X/N at 0%
- <target metric 2>: X/N at 0%
- <category> region delta: <before/after>
- <adjacent> smoke: <before/after>
- Regressions: <list or "none">
```
```

### Plan-writing failure modes

**Vague targets.** "Fix multicol column-rule" instead of "5 specific tests, here are the paths". Agents need exact filenames to grep, run, and report against.

**Missing Blink reference.** Without one, the agent reinvents an algorithm. Even if it passes the targeted tests, the result usually doesn't compose with the rest of the LayoutNG pattern and bites later.

**Generic milestones.** "Implement the feature" is not a milestone. "Add `case css.VerticalAlignMiddle` to the switch at `pkg/layout/inline_layout.go:1734`" is.

**No scope guards.** Agents will follow the failure trail wherever it goes. Tell them what they're **not** allowed to touch (related subsystems, deeper layout machinery, anything that needs a dedicated ticket).

**No DONE format.** Without a structured report, the orchestrator has to read prose and guess. The DONE block is what gates the merge decision.

---

## 6. Launch prompt template

Every agent prompt MUST restate (verbatim) the things below. The agent starts with zero prior context.

```
You are agent W<phase>.<num> in a parallel campaign. Read your plan at
`/tmp/campaign/agents/<NN>-<topic>/plan.md` and execute it FULLY.

CRITICAL CONTEXT — YOU ARE IN A WORKTREE:
- Your worktree root is the directory you start in. Run `pwd` and
  `git rev-parse --show-toplevel` FIRST to confirm. If either reports
  `/Users/iansmith/louis14`, STOP — you are in the wrong place.
- All Edit/Write paths MUST be absolute paths under your worktree root.
- The worktree symlinks `fonts/` and `../mazzy` should already exist;
  verify with `ls fonts/` and `ls ../mazzy` early.

PROJECT RULES:
- Go toolchain: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go <command>`.
  Plain `go` fails.
- ONE definition per value. No duplicate constants or parallel names.
- Tests must pass at **0% diff**. A 0.5% diff is failure.
- Blink as canonical reference @ `4883d11fef`. Mirror Blink's type names
  and algorithm structure — don't invent parallel vocabulary.
- mazzy (`~/mazzy` or `../mazzy`) is READ-ONLY by default. Never Edit/Write
  files there without explicit per-file go-ahead.
- **NEVER `git stash` from this worktree.** The worktree-shared .git store
  leaks stashes between concurrent agents. Use `git checkout -- <file>`
  or throwaway branches instead.
- Never `git push --force`, `git reset --hard`, `git commit --no-verify`,
  or `git push --no-verify`.
- **WE DO NOT WANT QUICK WINS OR LOWEST-HANGING FRUIT.** Correctness and
  foundational fixes only. If your fix is a hack that papers over a real
  bug, abandon it.

WORKFLOW:
1. `pwd` + `git rev-parse --show-toplevel` (verify worktree).
   `ls fonts/ && ls ../mazzy` (verify symlinks).
2. Verify build: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...`
3. Read plan; execute milestones in order.
4. Commit to a fresh agent branch `agent/<NN>-<topic>`; push to origin.
5. Append milestone notes to `/tmp/campaign/agents/<NN>-<topic>/progress.md`.
6. End with a DONE block in the format specified by the plan.

ENVIRONMENT:
- Tests: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest \
   -run "TestVisualReftest/<test-name>" -timeout 60s`
- For multi-test runs use `-run "TestVisualReftest/(name1|name2|...)"`.

REPORT BACK with the DONE block content as your final message.
```

Notes:
- **Always invoke the agent with `isolation: "worktree"`** in the Agent tool. This is what creates the per-agent branch + working tree, isolated from master and from other agents.
- **For diagnosis-heavy targets** (e.g. multicol column-rule, contain-paint-clip), add a Milestone 1 protocol: "Report findings in progress.md BEFORE any code changes. If diagnosis reveals the issue is bigger than the plan's scope, STOP and produce a no-commit investigation report." Roughly 1 in 6 agents will correctly choose no-commit; that's healthy scope discipline.

---

## 7. Concurrency, pacing, and rate-limit reality

| Knob | Value used | Why |
|---|---|---|
| Max in-flight agents | **10–12** | Above 12 server-side rate limits kicked in. Below 8 left throughput on the floor. |
| Launch stagger | **30–90 s between launches** | A burst of 6 simultaneous launches reliably hit "Server is temporarily limiting requests". Single launches per orchestrator turn solved it. |
| Plan queue depth | **1–3 plans ready** | Lets the orchestrator fire as soon as a slot opens, without context-switching to research mid-completion. |
| Survey vs impl ratio | **~1 survey per 4–6 impl** | Surveys feed plans. Too few surveys → orchestrator runs out of plan ideas. Too many → wasted slots on read-only work. |

### Two-track orchestrator loop

While impl agents run, the orchestrator should be **building the next plan**, not idle-waiting. Pattern:

1. Agent X completes → notification arrives.
2. Orchestrator merges X (5–10 turns).
3. Orchestrator picks the next cluster, drafts a plan (8–15 turns).
4. Orchestrator launches the next impl agent.
5. Orchestrator looks at the slot count; if room, drafts another plan; otherwise idles waiting.

Plans **must be pre-drafted while other agents are running**. Drafting on demand at slot-open time wastes the slot.

---

## 8. Failure modes seen and how to prevent them

### The git-stash leak (W2.5, W2.6, W2.8, W1.3 all hit it)
Worktrees share `.git/`. `git stash` is global across all worktrees. One agent stashing its WIP and another agent doing `git stash pop` will clobber the second agent with the first's content. **One agent literally lost all its work** before we caught it.

**Fix**: hard ban `git stash` in every prompt. Use `git checkout -- <file>` or throwaway branches.

### Rate-limit cascade
Six agents launched in a 10-second window all hit "Server is temporarily limiting requests (not your usage limit)". They retry, fail again, eventually give up. **The agent isn't suspended — it's burnt.**

**Fix**: stagger launches across 30–90 s. Launch one per orchestrator turn at most.

### Stale baseline files
`/tmp/campaign/all-failures.txt` was captured pre-campaign. After 20 merges it lies about which tests still fail. Agents read it at start and waste time on already-passing tests.

**Fix**: tell every plan to **re-derive the failing list at agent start** with a fresh grep over the actual test directory or a quick run. Treat the baseline as an orientation aid, not an authority.

### False-positive passes from missing fixtures (W13.34)
A WPT test references `url(support/foo.png)`. The PNG isn't vendored. Both the test page and the reference page render as empty boxes. The reftest "passes" at 0% diff — but **the real bug is invisible**. After vendoring the PNG, the test now renders the actual image and fails — exposing 15 real rendering bugs (background-repeat: round/space, background-position with negative percentage) that had been hiding for months.

**Fix**: routinely audit `url(support/...)` references against `support/` directory contents. Vendor missing fixtures from upstream WPT at a pinned SHA. The "regressions" caused by exposed real bugs are wins — they're signal that lets the next agent fix them (W13.37 took the W13.34 reveal and turned it into +47 in css-backgrounds).

### Conflict hotspots
These three files account for ~80% of merge conflicts: `pkg/css/style.go`, `pkg/css/stylesheet.go`, `pkg/css/cascade.go`. Patterns:
- `inheritableProperties` map — many agents add to it
- `isSupportedCSSProperty` / `supportedCSSProperties` map — many agents add to it
- `expandShorthand`, parser cases — many agents extend
- Named color tables, system color tables — touched together

**Fix**: when launching, avoid two simultaneous agents that both need to extend the same map. Sequence them: launch A, wait for merge, then launch B.

Conflicts that DO happen are **orchestrator-resolved inline**, not pushed back to the agent. The agent's context is gone; the orchestrator has fresh master and the agent's branch and can merge both halves in 2–5 minutes.

### Over-aggressive scope
Several agents fixed 1 target test plus 5 "obviously related" things and ended up with a 200-line commit. These usually merged fine, but the foundation drifted and follow-up agents had a harder time.

**Fix**: tighten scope guards. Specifically list out-of-scope features by name. "DO NOT touch X, DO NOT touch Y." Reject "while I was here I also…" diffs.

### Investigation-only is a success
W4.11 (::marker content) and W13.38 (contain-paint-clip with border-radius) both returned no-commit investigation reports correctly diagnosing that the fix was bigger than their scope. **These are wins.** They prevented bad commits and produced clean diagnostic memos for follow-up tickets.

Build the no-commit protocol into diagnosis-heavy plans explicitly.

---

## 9. What makes a target a good campaign target

| Property | Why |
|---|---|
| **5–25 tests in tight cluster** | Below 5, orchestration overhead exceeds gain. Above 25, scope creeps. |
| **Foundational** (parser, accessor, cascade, paint-gate) | These are short diffs with broad effect — single-map additions unlocking 20+ tests. |
| **Spec-driven** (clear CSS spec section + Blink mirror) | Agent can act decisively. Without a spec to mirror, decisions become guesses. |
| **1–3 file touch** | More than 3 means structural risk; reconsider. |
| **Independent of in-flight work** | No file-collision with currently-merging agents. |
| **Test fixture audit clean** | If `url(...)` refs are missing, vendor first, fix code second. |

Anti-targets:
- Deep layout machinery rewrites (multicol fragmentation, ruby box construction, ::first-line re-shaping)
- Feature work needing JS APIs (DOM mutation observers, getSelection, animations runtime)
- Anything needing CSSTest fonts not yet vendored
- "Refactor X for clarity" — campaign is about correctness, not cleanup

---

## 10. The DONE block: orchestrator's only merge gate

The agent's final message is a DONE block. The orchestrator reads only this block (plus optionally `git diff`) to decide merge / hold / revert.

```
## DONE at HH:MM:SS
- Branch: <name or "no-commit">
- Final commit SHA: <40-char or N/A>
- <specific target metric>: X/N at 0%
- <region> delta: <before/after pass counts>
- <adjacent region> smoke: <before/after>
- Regressions: <bulleted list or the literal word "none">
```

What the orchestrator looks for:
- **0% diff on the target tests** is non-negotiable. "Close to 0%" is failing.
- **"Regressions: none"** must be literal. If anything's listed, evaluate case by case — exposed-real-bugs (W13.34) are good signal; actual regressions need triage.
- **Region delta** must be net-positive in the target region and net-non-negative elsewhere.

Merge decision is a 30-second read. If the DONE block doesn't make this fast, the plan template was too loose.

---

## 11. Campaign rhythm and capacity planning

Approximate per-agent timings observed:

| Phase | Duration |
|---|---|
| Survey agent (Explore, no commit) | 2–4 minutes |
| Impl agent: simple parser/accessor fix | 5–15 minutes |
| Impl agent: cascade/render integration | 15–40 minutes |
| Impl agent: diagnosis-heavy or layout | 30–90 minutes |
| Orchestrator: write a plan | 8–20 turns ≈ 10–25 minutes |
| Orchestrator: merge (clean) | 3–6 turns ≈ 4–8 minutes |
| Orchestrator: merge (conflict) | 8–15 turns ≈ 10–20 minutes |

For a 7-hour session aiming at +200–300 tests:
- Roughly 30–40 impl agent launches
- Roughly 5–8 survey launches
- 20–35 successful merges
- 2–4 no-commit investigations
- 1–3 worktree-leak or conflict recoveries

If you're not building plans **while** agents work, you'll cap at 12–15 merges in 7 hours. With pipelining, 25–35 is achievable.

---

## 12. Worktree environment requirements

louis14-specific (see CLAUDE.md), but generally the worktree must be a fully-functioning checkout — symlinks for:
- `fonts/` (huge font collection, gitignored)
- `../mazzy` (sibling project go.mod replace target)

Agents must `ls fonts/` and `ls ../mazzy` early to fail fast if the worktree is malformed. A worktree without fonts will silently fail 50% of writing-modes tests, giving a false impression of regression.

For other projects: enumerate the gitignored-but-required paths and bake them into the launch script.

---

## 13. Things this playbook does NOT cover

- **Real CI integration.** This campaign was orchestrator-driven, not PR-driven. CodeRabbit was deferred for the smaller foundational changes. For changes worth a PR (anything > ~150 LOC or cross-cutting), the same plan-and-agent flow can produce a branch + PR with CodeRabbit feedback, but that path was used only sparingly here.
- **Cross-project agents.** Every agent in this campaign was scoped to louis14. If a fix actually requires mazzy changes too, the agent should stop and the orchestrator should split the work.
- **Long-running diagnosis tickets.** When an agent says "this is bigger than scope", the no-commit report is the deliverable. Turning that into a real ticket (via `/ticket-plan`) is a separate step.

---

## 14. The minimum viable campaign

If you want to reproduce this with one specific category instead of a full sweep:

1. `mkdir -p /tmp/campaign/agents`
2. Capture a baseline: full WPT run, save the failure list (`grep "^<category>"`).
3. Launch 1 survey Explore agent on the category; have it return drop-in plan sections for 2–3 clusters.
4. Write 2–3 plan files using the template in §5.
5. Launch 2 impl agents with the prompt template in §6, isolation: worktree.
6. While they work, write 1–2 more plans.
7. On completion, merge each via `git merge --no-ff origin/<branch>` and push.
8. Repeat for 2–3 cycles.

Even with that small loop, you should see 30–60 tests merged in ~2 hours if the category has a good cluster structure.

---

## Appendix A — file paths reference

Documents the orchestrator depends on:
- `/Users/iansmith/louis14/CLAUDE.md` — universal + louis14-specific rules
- `/Users/iansmith/louis14/design/parallel-agent-campaign-playbook.md` — this file
- `/tmp/campaign/ledger.md` — live orchestrator log
- `/tmp/campaign/agents/<NN>-<topic>/{plan,progress}.md` — per-agent
- `/tmp/campaign/all-failures.txt` — pre-campaign baseline
- `/tmp/reftest-survey-<date>-<phase>.txt` — periodic full-survey snapshots

Worktrees live at `/Users/iansmith/louis14/.claude/worktrees/agent-<task-id>/`.

## Appendix B — common merge-conflict resolutions

| Conflict | Resolution pattern |
|---|---|
| Two agents added to `inheritableProperties` | Keep both blocks, both additions valid |
| `isSupportedCSSProperty` modernized (W8.19) + old-style addition | Discard old map, add new entry to `supportedCSSProperties` |
| `expandShorthand` two new case branches | Keep both cases, order doesn't matter |
| Named color / system color map additions | Keep both; verify no duplicate keys |
| `stylesheet.go` parser case additions | Keep both in declared order |

When in doubt, build (`GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...`) and run the targeted tests for both agents before pushing.
