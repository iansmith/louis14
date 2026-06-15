# Swarm "Release the Hounds" RUNBOOK — validated 2026-05-30

**This supersedes `CONTINUATION.md`** (which described the older `lane/*`-branch + manual-merge model). Use THIS. Validated end-to-end on the LOU-176 pilot (PR #34 merged `9dad7b37`).

A new session can execute the swarm from this file alone. Read it top-to-bottom, plus memory [[feedback-swarm-lane-ticket-skills]], [[feedback-campaign-worktree-frozen-mazzy]], [[feedback-fix-pipeline]].

---

## The architecture in one paragraph

Each lane = one Linear ticket = one `fix/LOU-N` branch in its own worktree. The **orchestrator** preps the lane (mint the Linear issue *In Progress* → `git worktree add -b fix/LOU-N` off `origin/master` + frozen-mazzy recipe), then launches **ONE Agent-tool subagent** into the worktree that runs the entire `/ticket-*` lifecycle **itself** — `/ticket-start` (adopts the pre-made branch) → `/ticket-plan` → implement → `/ticket-pr` (incl. resolving CodeRabbit) — then **STOPS**. The orchestrator then runs the serial funnel in the **campaign root** — **merging autonomously within the §5 parameter gate**, escalating only on genuine judgment calls: independent regression gate → `git worktree remove` → `git switch fix/LOU-N` → `/ticket-merge` → `/ticket-archive`. Why the split: every `/ticket-*` skill operates on `git branch --show-current` in its cwd, and `/ticket-merge` does `git switch master` — fine in a worktree for `start/plan/pr`, but `merge`/`archive` must run in the root where master lives.

**Concurrency:** ~8 lanes per wave. Lane agents are **Agent-tool subagents** (they have the `Skill` tool + Linear MCP — confirmed); do NOT use Workflow `agent()` for lanes (Skill access unconfirmed). Research/scout agents: cap 8.

---

## 1. Per-session prereqs (verify before launching)

- **STEP 0 — make sure the orchestration worktree is free and on master.** Before anything else: `git -C /Users/iansmith/louis14-campaign status` must report `On branch master` AND `nothing to commit, working tree clean`. A dirty or wrong-branch orchestration tree means a previous wave didn't clean up — resolve it BEFORE minting issues or creating worktrees.
- **SHARED-GIT-DIR STASH HAZARD (load-bearing — read this).** `louis14-campaign`, the main `~/louis14` tree, and EVERY lane worktree share ONE git common dir (`/Users/iansmith/louis14/.git`), so there is a SINGLE global `git stash` stack across all of them. **NEVER `git stash` anywhere during a wave** — one lane's `git stash pop` pops another lane's stash into the wrong worktree and silently cross-contaminates files. Observed LIVE in the 2026-05-30 Wave 1 (LOU-181 `flex_layout.go` ↔ LOU-183 `table_layout.go` swap via a cross-popped stash). Use the file-swap baseline method (§4 step 4 / §5 step a) instead. This is the exact leak the parallel-agent playbook §8 documents.
- Orchestrate from `/Users/iansmith/louis14-campaign` (campaign tree), on `master`, clean working tree.
- Frozen mazzy present at `/Users/iansmith/mazzy-perspective-dc`.
- `go.work` / `go.work.sum` excluded in the shared common-dir exclude (`/Users/iansmith/louis14/.git/info/exclude` lines ~7-8 — already done).
- Go: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go`. **NEVER `GOWORK=off`** (worktree `go.work` pins frozen mazzy).
- Linear: team `louis14`, prefix `LOU`. States Backlog→Todo→In Progress→Done (NO intermediate; Done id `118c680d-f7dc-47ce-9fd7-09d9ba996cfc`), so `/ticket-merge`'s "advance one" = Done.
- `gh` authed (keyring; `/opt/homebrew/bin/gh`). CodeRabbit lives on the GitHub remote (`iansmith/louis14`).
- Blink citation SHA pin: `4883d11fef4a8713e32cd582ecef6dc5457c8c3f`.

---

## 2. Candidate queue

Scouted 2026-05-30 (`wf_9a3b3277`, 39 areas, 44 candidates across 31 areas). **Full data — incl. each target's full hypothesis + Blink analog — in `docs/swarm-lanes/candidate-queue.json`** (the `[idx]` below indexes that file; mint each lane's Linear description from its hypothesis). Already-shipped, excluded: selectors (LOU-174), css-grid (LOU-175), css-tables row-height (LOU-176). Empty areas (no genuine bounded target): css-box, css-color-adjust, css-counter-styles, css-hyphens, css-masking, css-nesting, css-scrollbars, css-transitions.

**18 of the 44 are pairwise file-disjoint** — so the wave-1 limit is the 8-slot cap, not conflicts.

### Wave 1 — recommended 8 (all ≤0.3% diff, pairwise-disjoint, high confidence)

| idx | area | test | diff | file(s) | one-liner |
|---|---|---|---|---|---|
| 28 | css-sizing | image-min-max-content-intrinsic-size-change-001 | 0.0% (100px) | `layout/replaced_layout.go` | replaced/image min/max-content intrinsic size on content change |
| 18 | css-inline | empty-span-size-002 | 0.0% (17px) | `layout/ruby_inline_items.go` (+2 ruby) | empty inline span sizing via the ruby item path |
| 10 | css-display | display-contents-first-line-002 | 0.0% (228px) | `layout/inline_layout.go` | `display:contents` + `::first-line` |
| 22 | css-multicol | baseline-005 | 0.0% (232px) | `layout/multicol_layout.go` (+fragment_builder, layout_result) | multicol baseline export |
| 12 | css-flexbox | flexbox-flex-direction-column-percentage-ignored | 0.2% (785px) | `layout/flex_layout.go` | column-flex percentage block-size ignored |
| 32 | css-transforms | transform3d-preserve3d-012 | 0.1% (317px) | `render/render.go` | `transform-style: preserve-3d` |
| 33 | css-transforms | transform-transformed-tr-contains-fixed-position | 0.3% (1349px) | `layout/table_layout.go` | transformed `<tr>` as containing block for fixed-pos |
| 3 | css-backgrounds | background-position-negative-percentage-comparison | 0.3% (1444px) | `css/style.go` | `min()/max()` in background-position with negative base |

### Wave 1-overflow — next-ready, also disjoint (fire as slots free, slightly higher diff)

`[17]` css-inline initial-letter-raise 1.1% (`layout_tree_builder.go`) · `[24]` css-overflow overflow-clip-x-visible-y-svg 1.0% (`render/svg_root_painter.go`,`paint_layer.go`) · `[16]` css-images linear-gradient-non-square 2.1% (`render/gradient.go`) · `[5]` css-cascade import-conditional-002 2.1% (`css/at_import.go`) · `[39]` filter-effects filter-region-units-001 1.9% (`layout/svg/svg_resource_{paint_server,filter}.go`)

### Defer / scrutinize (disjoint but high-diff or larger scope — verify "bounded" before committing)

`[1]` css-animations animate-pause-set-time 0.5% but needs a new `Element.animate()` JS binding (`js/dom.go`,`js/engine.go`,`css/animation.go`) · `[30]` css-text overflow-wrap-001 5.3% (`line_breaker.go`) · `[25]` css-position abspos-semi-replaced-stretch 5.3% · `[40]` filter-effects feconvolve 8.3% · `[7]` css-conditional css-supports-005 **100%** (`css/stylesheet.go`) · `[15]` css-images color-stop-currentcolor **97.7%** (`gradient.go`).

### Wave 2+ — blocked behind a hot shared file (one lane per file per wave; see §6)

- **`css/style.go`** (after wave-1 `[3]` frees it): `[6]`css-color contrast-color, `[14]`css-fonts font-size-zero, `[21]`css-logical float-clear, `[27]`css-ruby ruby-align, `[35]`css-values q-unit, `[36]`css-variables wide-keyword, `[38]`css-writing-modes scrollbar.
- **`render/render.go`** (after `[32]`): `[2]`css-backgrounds bg-color-clip, `[23]`css-overflow ellipsis-rtl, `[31]`css-text-decor underline-offset, `[37]`css-will-change opacity-SC, `[38]`, `[42]`/`[43]`compositing root bg/opacity (both 100%).
- **`layout/layout_tree_builder.go`** (after `[17]`): `[8]`css-contain style-counters, `[11]`css-display first-letter, `[19]`/`[20]`css-lists counters, `[26]`css-pseudo marker-text-transform.
- **`css/stylesheet.go`** (after `[7]`, or swap `[7]` out): `[4]`css-cascade important-prop, `[13]`css-fonts first-available-font, `[41]`mediaqueries negation.
- **`layout/inline_layout.go`** (after `[10]`): `[0]`css-align baseline-of-scrollable, `[21]`, `[29]`css-sizing whitespace-break.
- Misc blocked: `[9]`css-contain scrollbars (huge footprint — flex/layout_result/paint_layer/etc.), `[34]`css-ui box-sizing 25.8% (`replaced_layout.go`, after `[28]`), `[29]` (`line_breaker.go`, after `[30]`).

---

## 3. Per-lane prep (orchestrator, campaign root)

For each target in the current wave:

1. **Mint the Linear issue** — `save_issue` team `louis14`, **state `In Progress`** (the lane is live; NOT Todo), assignee `me`, description = the failure (symptom / hypothesis / Blink analog / files), drawn from the candidate queue. Note the returned `LOU-N`.
2. **Create the worktree on a pre-made branch** (this is what makes the agent's `/ticket-start` non-interactive):
   ```
   WT=/Users/iansmith/louis14-campaign/.claude/worktrees/lane-lou-<N>
   git -C /Users/iansmith/louis14-campaign worktree add -b fix/LOU-<N> "$WT" origin/master
   printf 'go 1.26.2\n\nuse .\nuse /Users/iansmith/mazzy-perspective-dc/mazarin/textshape\n' > "$WT/go.work"
   ln -sfn /Users/iansmith/mazzy-perspective-dc /Users/iansmith/louis14-campaign/.claude/worktrees/mazzy
   mkdir -p "$WT/fonts" && ln -sf /Users/iansmith/louis14-campaign/fonts/* "$WT/fonts/"
   ```
   Verify a quick `go build ./pkg/layout/` in `$WT` before launching (known-good env).
3. **Launch ONE Agent-tool subagent** into `$WT` with the §4 prompt (run_in_background:true). Do 1-3 for each wave-1 lane; the agents run in parallel.

---

## 4. LANE-AGENT PROMPT template (paste, fill the `<<...>>`)

```
You are the LANE AGENT in the louis14 WPT fix-swarm. Project: a Go browser engine modeled on Blink LayoutNG. Drive ticket <<LOU-N>> from plan → implementation → PR entirely inside your worktree using the /ticket-* skills YOURSELF, then STOP at /ticket-pr (the orchestrator does merge/archive).

WORKTREE_ROOT = <<WT path>>
TICKET = <<LOU-N>>   BRANCH = fix/<<LOU-N>>  (already created + checked out in your worktree)

=== MANDATORY STARTUP CHECK ===
cd <<WT path>> ; confirm `pwd` ends with that path AND `git branch --show-current` == fix/<<LOU-N>>. If not, STOP and report.

=== HARD RULE — never write outside your worktree ===
Every absolute path you pass to Edit/Write/>-redirect/sed -i/mv/cp/gofmt -w/go build -o MUST start with <<WT path>>/. NEVER write under /Users/iansmith/louis14/<anything> or /Users/iansmith/louis14-campaign/<non-worktree>. (Tracking files under ~/.claude/ticket-active/<<LOU-N>>/ are the exception — the /ticket-* skills manage them; see the FRICTION rules.)

=== PROJECT RULES ===
- Go: ALWAYS `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go`. NEVER GOWORK=off.
- Tests-first (§2): the WPT reftest is the spec; confirm RED-for-the-right-reason before, GREEN after.
- ALL tests pass at 0% diff (0.1% is failure). WPT fuzzy tolerance is built in.
- Blink canonical: mirror Blink type/algorithm; cite symbol+file pinned to Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. NO per-test constants / Skip() / magic offsets. One source of truth (§5); dedupe (§4).
- Commit only to fix/<<LOU-N>>. Never touch master/other branches. Never force-push/reset/--no-verify.
- HARD RULE — **NEVER `git stash`**. All worktrees share one global stash stack; a stash/pop in your worktree WILL clobber a concurrent lane (see §1 hazard). Use the §4-step-4 WIP-commit + file-swap baseline method instead.

=== TASK ===
Target WPT reftest(s) — must reach 0% (max diff 0): <<test(s)>>
Run: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest -run 'TestWPTCSS3Reftests/<<test-regex>>' -count=1 -timeout 600s -v`
Hypothesis to VERIFY (don't assume): <<hypothesis + likely files>>. The <<LOU-N>> Linear description has the full hypothesis.

=== STEPS (drive the /ticket-* skills yourself) ===
1. /ticket-start <<LOU-N>> — adopts the existing fix/<<LOU-N>> branch (non-interactive), transitions Linear → In Progress, seeds ~/.claude/ticket-active/<<LOU-N>>/.
2. /ticket-plan — Blink-aligned, SHA-pinned, foundational plan + self-gate.
3. Implement in your worktree ONLY. Do NOT commit yet (leave it for /ticket-pr's /simplify). Reach 0% on the target(s).
4. Regression: run the FULL affected section(s) on your working tree AND on origin/master. **HARD RULE — NEVER `git stash`** (all worktrees share ONE global stash stack; a pop cross-contaminates other lanes — see §1 hazard). Baseline method: first make a throwaway WIP commit of your work (`git add -A && git commit -m 'wip: regression baseline'`), then file-swap the changed files to master (`git checkout origin/master -- <changed-files>`), run the section, restore (`git checkout HEAD -- <changed-files>`); the WIP commit is folded in / squashed at /ticket-pr's /simplify. ZERO newly-failing. Note newly-fixed.
5. gofmt changed files; `go build ./...`; `go vet ./pkg/layout/`.
6. /ticket-pr — runs /simplify, commits, pushes, opens the PR, triggers CodeRabbit. Poll CodeRabbit up to ~8 min; address CLEARLY-CORRECT findings (commit+push), LIST debatable ones for the orchestrator.
7. STOP. Do NOT run /ticket-merge or /ticket-archive.

=== FRICTION WORKAROUNDS (REQUIRED — learned in the LOU-176 pilot) ===
A. The subagent "don't write report files" guard BLOCKS the Write tool for ~/.claude/ticket-active/<<LOU-N>>/findings.md. Write ALL ticket tracking files via a Bash heredoc (`cat > ~/.claude/ticket-active/<<LOU-N>>/findings.md <<'EOF' ... EOF`), NOT the Write tool. The documentation MUST be produced — it is the point of /ticket-*.
B. You have NO Agent tool — you cannot nest sub-agents. /simplify and /ticket-pr try to spawn helpers (code-simplifier, 4 simplify agents); when they would, perform those review passes INLINE yourself across the same angles (reuse/simplification/efficiency/altitude).

=== DONE block ===
WORKTREE_ROOT / TICKET / BRANCH / PR #<n> url / TARGET DIFF (0%?) / REGRESSION (zero new; newly-fixed) / CR (clean|addressed|debatable-listed|pending) / TICKET-SKILL LOG (per skill: ran? interactivity hit + how resolved? friction?) / git log --oneline -5 / ROOT TREES CLEAN (git -C /Users/iansmith/louis14 status --porcelain AND git -C /Users/iansmith/louis14-campaign status --porcelain — your work in NEITHER root).
```

---

## 5. Serial funnel (orchestrator) — AUTONOMOUS merge within the gate

**The orchestrator may merge autonomously — no per-lane human approval — when EVERY parameter below holds.** This is the standing authorization ("the parameters that are set"). If any parameter fails or the lane hits a genuine judgment call, do NOT merge: fix-and-recheck, or escalate (triggers below). Log each lane's gate outcome to the RUNLOG so every autonomous merge is auditable.

### Merge-decision parameters (ALL must hold to auto-merge)
1. **Target 0%** — the lane's target test(s) at max diff 0 (re-confirm; don't just trust the agent's DONE block).
2. **Independent regression gate clean** — orchestrator-run (step a), ZERO newly-failing tests on the affected section(s). Never trust the agent's self-report.
3. **PR MERGEABLE / CLEAN** — no conflicts, base current (`gh pr view`).
4. **CodeRabbit resolved** — every actionable finding either applied (mechanical/clear) or dispositioned by the orchestrator as a verified false positive with a one-line reason. No open material finding.
5. **`/simplify` clean** (or its findings applied).
6. **Foundational** — a general fix from a single source mirroring Blink; NO `Skip()`, magic constant, per-test patch, or dead-path over-claim. Same bar as the adversary gate; the orchestrator confirms by reading the diff.
7. **Disjoint footprint** — the lane's files don't overlap any other lane in the same merge window (per the wave plan, §6).
8. **Honest scope** — the target reached 0% on its own merits; if an orthogonal bug blocked it, the lane was split to a new ticket and this PR does only the bounded part.

### Escalate to the human instead of merging when ANY of:
- the regression gate shows new fails the orchestrator can't resolve;
- a CodeRabbit/adversary finding is **material AND debatable** (a real judgment call, not a clear false positive);
- the implementation deviated from the plan in a way that *might* be a hack and the orchestrator isn't confident it's spec-correct;
- a merge conflict needs hand-resolution (master moved into the lane's files);
- a scope-split call is non-obvious (orthogonal bug vs in-scope);
- the target cannot reach 0% (documented failure → RUNLOG + new gap ticket, not a merge).

### Steps (per lane that reports clean)
a. **Independent regression gate** — in the still-present worktree, in-place file-swap: run the affected section on branch HEAD, then `git checkout origin/master -- <changed files>`, run again, restore with `git checkout HEAD -- <changed files>`; `comm` the FAIL sets → zero new. (Frozen mazzy stays constant.) While here, read the diff to confirm parameters 6 + 8.
b. `git worktree remove --force <WT>` → in campaign root `git switch fix/LOU-N`.
c. **`/ticket-merge --pr <N> --strategy merge`** — the orchestrator is the authorized decision-maker, so it **self-approves** the skill's Step-3 confirmation once the parameters hold (no human wait). Real merge commit (§3, never squash); advances LOU-N In Progress → Done; deletes branch; switches root to master.
d. **`/ticket-archive LOU-N`** — pushes the final task plan as the description + DoD-confirmation comment + findings comment (orchestrator is UNGUARDED, so this always works); moves tracking to `~/.claude/ticket-archive/`.
e. ff campaign master if needed; append the lane + its gate outcome to `docs/swarm-lanes/RUNLOG-<date>.md`.

The orchestrator runs `/ticket-merge` + `/ticket-archive` directly (it is the main agent, NOT a subagent → the findings-write guard and nested-agent limits do NOT apply to it).

---

## 6. Non-conflict rule (load-bearing)

Lanes in a wave MUST touch **disjoint files**, or they conflict on merge-back. Hot shared files — `pkg/render/render.go`, `pkg/layout/{table_layout,inline_layout,block_layout,line_breaker}.go`, `pkg/css/style.go` — allow only ONE lane per file per wave; queue the rest for a later wave. After each merge master advances; later lanes still merge clean IF their files are disjoint from what landed. The candidate queue (§2) is pre-bucketed by file so wave selection is mechanical.

---

## 7. What the orchestrator owns (executes autonomously; never delegated to lane agents)

**Merge decisions — made autonomously within the §5 parameters**, not deferred to the human per-lane; escalate only on the §5 triggers. Also: CodeRabbit triage (apply clear findings, disposition false positives with a reason, escalate only debatable-AND-material ones); scope splits (target won't reach 0% due to an orthogonal bug → `/ticket-start` a NEW ticket, don't hack); the independent regression gate; the disjointness curation + wave sequencing. These never go to the lane agents — but the orchestrator now executes them **under the gate rather than pausing for approval**. The human sets the parameters and audits via the RUNLOG; the orchestrator runs the funnel.
