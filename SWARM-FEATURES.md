# Feature Swarm Guide

This document describes how to run a parallel swarm of Claude agents to implement multiple
CSS features simultaneously in Louis14's browser engine.

---

## How the Swarm Works

Each feature is implemented by a separate agent running in its own **git worktree**
(`.worktrees/feature-N`). Worktrees share the git history but have independent working
trees, so agents can modify `style.go`, `stylesheet.go`, etc. without conflicting with
each other. After all agents finish, the worktrees are merged back into the main branch.

---

## How to Run Agents

### Preferred Method: Task Tool (from within a Claude session)

**Use the Task tool directly** — do NOT use `run-swarm.sh`. The shell-based swarm has a
reliability problem: Claude CLI subprocesses can pick up stale session history or
`MEMORY.md` context from previous runs, causing them to describe past failures instead of
doing actual work.

The Task tool's `general-purpose` agents start fresh with only the prompt you provide.
They don't read `~/.claude/projects/` session history or inherit `MEMORY.md` context.

**Workflow**:
1. Create worktrees (see Setup below)
2. Launch all agents in a single message with one `Task` tool call per feature
3. Each agent prompt must include: feature name, worktree path (absolute), implementation
   details, test instructions, Go binary path
4. Monitor via task notifications; collect results as agents complete
5. Commit each worktree and merge sequentially

**Example prompt structure for an agent**:
```
You are implementing Feature N: <name> in the Louis14 browser engine.

Working directory: /Users/iansmith/louis14/.worktrees/feature-N
All file operations MUST use absolute paths starting with that prefix.

Go binary: /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go

[Detailed implementation instructions]

WPT tests to create: [exact HTML content]

After implementing:
1. Build: go build ./...
2. Run new tests: go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/<dir>' -count=1 -v
3. Full regression: go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests|TestWPTReftests' -count=1 2>&1 | tail -5
4. Do NOT commit. Report what you changed and test results.
```

### run-swarm.sh (Reference Only)

`run-swarm.sh` exists as documentation but is unreliable when called from inside a Claude
session. Use it only to understand the swarm structure or when launching from a terminal
**outside** any Claude session. Key flags:
- Binary: `~/.local/bin/claude`
- Flags: `--allowedTools "Bash,Read,Write,Edit,Glob,Grep,TodoWrite,Task,WebFetch"` `-c WORKTREE` `-p PROMPT`
- `unset CLAUDECODE` before launching (prevents "nested session" error)
- Logs: `/tmp/swarm-logs/feature-N.log`

---

## Setup: Creating Worktrees

```bash
cd /Users/iansmith/louis14
for i in $(seq 1 10); do
  BRANCH="feature-$i-impl"
  WORKTREE=".worktrees/feature-$i"
  git worktree add "$WORKTREE" -b "$BRANCH"
  # Copy fonts — NOT tracked in git, required for pixel-accurate tests
  cp fonts/AtkinsonHyperlegible*.ttf "$WORKTREE/fonts/" 2>/dev/null || true
  cp fonts/AtkinsonHyperlegibleMono*.otf "$WORKTREE/fonts/" 2>/dev/null || true
  cp fonts/Liberation*.ttf "$WORKTREE/fonts/" 2>/dev/null || true
done
```

**Font copying is critical**: Ahem.ttf is tracked in git but AtkinsonHyperlegible and
Liberation fonts are not. Without them, the renderer falls back to a system font with
different metrics, causing pixel tests to fail.

---

## After Agents Finish: Commit and Merge

```bash
# Commit each worktree
for i in $(seq 1 10); do
  WT="/Users/iansmith/louis14/.worktrees/feature-$i"
  git -C "$WT" add -A
  git -C "$WT" commit -m "Feature $i: <description>"
done

# Merge into main branch (sequentially to catch conflicts)
cd /Users/iansmith/louis14
for i in $(seq 1 10); do
  git merge "feature-$i-impl" --no-edit || echo "CONFLICT on feature $i — resolve manually"
done

# Verify
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go build ./...
/opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests|TestWPTReftests' -count=1 -v 2>&1 | grep Summary
```

---

## Common Pitfalls

### 1. Stale Claude session files
`~/.claude/projects/-Users-iansmith-louis14--worktrees-feature-N/` stores conversation
history from previous swarm runs. If a worktree path is reused, a shell-mode `claude`
subprocess will load this history and behave as if continuing the old conversation.

**Fix**: Use the Task tool instead of shell subprocesses. Or delete stale session dirs:
```bash
rm -rf ~/.claude/projects/-Users-iansmith-louis14--worktrees-feature-*/
```

### 2. CLAUDECODE env var (run-swarm.sh only)
When run-swarm.sh is invoked from inside a Claude Code session, `CLAUDECODE` is set.
Child processes inherit it and immediately exit with "cannot be launched inside another
Claude Code session". The script includes `unset CLAUDECODE` which fixes this for direct
shell execution, but when launched via the Bash tool the env is inherited differently.

**Fix**: Use Task tool agents. If using run-swarm.sh, run it from a separate terminal.

### 3. MEMORY.md context confusion
MEMORY.md is loaded into every agent's system prompt (truncated at 200 lines). If it
contains notes about a previous failed swarm run, agents may describe that failure instead
of implementing new code. Task tool agents start fresh and don't read MEMORY.md.

**Fix**: Keep MEMORY.md focused on stable architectural facts. Move session-specific notes
to timestamped entries at the bottom (which get truncated anyway).

### 4. Missing fonts in worktrees
Fonts not tracked by git won't appear in new worktrees. Always copy AtkinsonHyperlegible
and Liberation fonts immediately after `git worktree add`.

### 5. Merge conflicts in shared files
Many features modify `pkg/css/style.go` and `pkg/css/stylesheet.go`. Conflicts are
usually mechanical (two new `case` clauses or two new `if strings.HasPrefix` blocks).
Resolve by including BOTH sets of changes side by side.

**Conflict-prone files**: `style.go`, `stylesheet.go`, `matcher.go`, `render.go`
**Usually clean**: new test files, individual layout files (`grid.go`, `flex.go`), new Go files

---

## How to Find Features to Implement

### Process

1. **Survey the codebase** to understand what's already implemented:
   ```bash
   grep -c "^func (s \*Style) Get" pkg/css/style.go   # count getters
   grep -n "HasSuffix.*\"vh\"" pkg/css/style.go        # find specific unit
   grep -rn "pointer.*fine\|hover.*hover" pkg/css/     # check media features
   ```
   Or use a Task/Explore agent: "Survey what CSS features are implemented in Louis14."

2. **Research what real websites use** via web sources:
   - [HTTP Archive Web Almanac](https://almanac.httparchive.org/en/2024/css) — usage stats by property
   - [State of CSS survey](https://stateofcss.com/) — developer usage rankings
   - [projectwallace.com CSS stats](https://www.projectwallace.com/css-stats) — live analysis of real sites
   - Search: "CSS features 2025 usage statistics" or "most used CSS properties 2026"

3. **Cross-reference with WPT tests**:
   The Web Platform Tests (WPT) suite at https://github.com/web-platform-tests/wpt
   has tests for every CSS feature. Check `css/css-<feature>/` directories.

   Browse: `https://wpt.live/css/<feature>/` to see live tests.

4. **Prioritize** by: (usage on real sites) × (implementation difficulty)
   - High-value, low-difficulty: unit aliases (dvh, ch), env() function, vendor prefixes
   - High-value, medium-difficulty: CSS nesting, @media features, color() function
   - High-value, high-difficulty: container queries, subgrid, animations

### How to Get WPT Tests

Option A — Use the Task tool's Explore/WebFetch agents to fetch test content:
```
"Fetch the WPT tests for CSS nesting from web-platform-tests/wpt on GitHub.
Get the content of nesting-basic.html and nesting-basic-ref.html."
```

Option B — Browse https://wpt.live/css/css-nesting/ to view existing WPT tests.

Option C — Write custom tests following the WPT visual reftest format:
```html
<!-- test.html -->
<!DOCTYPE html>
<html>
<head>
  <link rel="match" href="test-ref.html" />
  <style>/* CSS using the new feature */</style>
</head>
<body><!-- markup --></body>
</html>

<!-- test-ref.html: same visual output WITHOUT the new feature -->
```

Test location: `pkg/visualtest/testdata/wpt-css3/<feature-dir>/`

**Test policy**: `MaxDifferentPercent=0.1`, `FuzzyRadius=0` always. Tests must pass with
fewer than 480 different pixels at 800×600. Never use fuzzy matching.

---

## Testing Policy (always enforced)

- `MaxDifferentPercent=0.1` (≤480 pixels at 800×600)
- `FuzzyRadius=0` — never use fuzzy matching (conceals real bugs)
- Acceptable: `Tolerance` (per-channel color tolerance, e.g. `Tolerance=2`)
- CLI: `go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests|TestWPTReftests' -count=1 -v 2>&1 | grep Summary`
- Debug: `go run ./cmd/l14open input.html output.png 800 600`

---

## Round History

| Round | Features | Tests | Result |
|-------|----------|-------|--------|
| Round 1 (2026-02-20f) | @layer, logical props, oklch/hwb/color-mix, repeat(), @keyframes, font-variant, flow-root, text-wrap, display:flow-root | +23 | CSS3 298/298 |
| Round 2 (2026-02-21) | CSS nesting, dvh/cqw units, ch/ex/lh units, env(), @media features, image-set(), color(), subgrid, ::marker, vendor prefixes | +29 | CSS3 327/327 |
