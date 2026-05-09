# LOU-89: Float margin-top not honored inside multicol

## FIRST STEP — Read CLAUDE.md
Read `/Users/iansmith/louis14/CLAUDE.md` now. Do not proceed until you have read and understood it.

## Worktree Environment Setup
Do these BEFORE running any tests:

1. **Fonts symlink** — the worktree only has Ahem.ttf.
   ```
   ln -sfn /Users/iansmith/louis14/fonts fonts
   ```

2. **go.mod mazarin replace path** — the worktree is nested under `.claude/worktrees/`, so the relative path
   `../mazzy/mazarin/textshape` in go.mod resolves wrong. Fix it:
   ```
   sed -i '' 's|replace mazarin/textshape => ../mazzy/mazarin/textshape|replace mazarin/textshape => /Users/iansmith/mazzy/mazarin/textshape|' go.mod
   ```
   Then verify: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go mod tidy`

## Go Toolchain
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go
```

## Test Command
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-multicol/nested-floated-multicol-with-monolithic-child'
```

## Build Command
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./pkg/layout/
```

## The Bug
**Test:** `nested-floated-multicol-with-monolithic-child`
**Source:** Diagnosed but unfixed from Phase 16.e+18 v2

**Bug:** Float's `margin-top:10` is not being honored — float starts at y=0 instead of y=10 inside a multicol container. This is a float-margin-collapse bug specific to floats inside multicol (not a `balanceColumns` issue, not a `TallestUnbreakable` issue).

## Session-Specific Rules
- NEVER make changes outside your git worktree
- Focus on foundational correctness — fix root causes, not surface symptoms
- A correct foundational fix that causes small regressions in other tests is acceptable
- Study Blink's implementation BEFORE writing code
- Mirror Blink's types, algorithms, and constraint-passing patterns
- Commit at each milestone and report what you found/changed
- Run ONLY nested-floated-multicol-with-monolithic-child during development

## Key Source Files
- `pkg/layout/out_of_flow_layout.go` — float positioning and margin handling
- `pkg/layout/multicol_layout.go` — multicol container (float constraint within columns)
- `pkg/layout/fragment_builder.go` — fragment building
- `pkg/visualtest/` — test runner and test data

## Reference
- Blink source: `third_party/blink/renderer/core/layout/ng/` (LayoutNG)
- Search: https://source.chromium.org/chromium/chromium/src
