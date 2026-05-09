# LOU-54: Flex T2 — Scrollbar gutter reservation (2 tests, ~108k px)

## FIRST STEP — Read CLAUDE.md
Read `/Users/iansmith/louis14/CLAUDE.md` now. Do not proceed until you have read and understood it.

## Worktree Environment Setup
Do these BEFORE running any tests:

1. **Fonts symlink** — the worktree only has Ahem.ttf.
   ```
   ln -sfn /Users/iansmith/louis14/fonts fonts
   ```

2. **go.mod mazarin replace path** — fix relative path for nested worktree:
   ```
   sed -i '' 's|replace mazarin/textshape => ../mazzy/mazarin/textshape|replace mazarin/textshape => /Users/iansmith/mazzy/mazarin/textshape|' go.mod
   ```
   Then verify: `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go mod tidy`

## Go Toolchain
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go
```

## Test Commands
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-flexbox/content-height-with-scrollbars' -timeout 30s

GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-flexbox/cross-axis-scrollbar' -timeout 30s
```

## Build Command
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./pkg/layout/
```

## The Bug
**Tests:** `content-height-with-scrollbars` (69,200 px, 14.4%) + `cross-axis-scrollbar` (38,500 px).
**Priority:** High (Flex T2)

**Bug:** Flex container with `overflow:scroll` does not exclude scrollbar from content box. The scrollbar width must be subtracted from the overflowing axis, and scrollbar placement must follow the flex main/cross direction.

**Fix in:** `flex_layout.go` — subtract `ScrollbarWidth` from overflowing axis; scrollbar placement follows flex main/cross direction.

## Session-Specific Rules
- NEVER make changes outside your git worktree
- Focus on foundational correctness — fix root causes, not surface symptoms
- A correct foundational fix that causes small regressions in other tests is acceptable
- Study Blink's implementation BEFORE writing code
- Mirror Blink's types, algorithms, and constraint-passing patterns
- Commit at each milestone and report what you found/changed
- Run ONLY these two flexbox tests during development

## Key Source Files
- `pkg/layout/flex_layout.go` — flex container layout, scrollbar subtraction
- `pkg/layout/constraint_space.go` — constraint space with scrollbar awareness
- `pkg/layout/fragment_geometry.go` — Scrollbar geometry
- `pkg/visualtest/` — test runner and test data

## Reference
- Blink source: `third_party/blink/renderer/core/layout/ng/flex/ng_flex_layout_algorithm.cc`
- Search: https://source.chromium.org/chromium/chromium/src
