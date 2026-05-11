# LOU-60: Bidi embedding level depth + pipeline reorder (~50 tests)

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
```bash
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go
```

## Test Commands
Bidi tests are spread across `css-writing-modes` and `css-pseudo`:
```bash
# Run all bidi tests at once:
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' -timeout 60s

# Also check css-pseudo bidi tests:
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-pseudo/marker-unicode-bidi' -timeout 60s
```

## Build Command
```bash
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./pkg/layout/
```

## The Bug
**Current state:** 32/82 bidi tests passing, ~50 failing.
**Root cause identified:** `golang.org/x/text/unicode/bidi` only exposes `Run.Direction()` (LTR/RTL), NOT actual embedding levels. `ResolveBidiLevels` collapses everything to level 0 or 1.

**Three approaches:** 
- (A) ICU via cgo for `ubidi_getLevelAt()` — exact Blink parity, requires cgo
- (B) Pure Go `computeEmbeddingLevels()` — already written, correct, but downstream cascading from control char stripping broke it
- (C) Minimal UAX#9 level resolver in Go

**Pipeline reorder needed** regardless of level source. Current order is broken: item splitting happens BEFORE stripping, creating offset inconsistencies. Correct order:
1. ResolveBidiLevels (per-rune levels, no splitting)
2. StripBidiControlsFromItems (strip controls, remap offsets)
3. SplitItemsAtLevelBoundaries (split using stripped text offsets)

**Watch out:** Previous attempt caused 100+ regressions in writing-modes suite due to byte-offset inconsistencies in line breaker.

## Session-Specific Rules
- NEVER make changes outside your git worktree
- Focus on foundational correctness — fix root causes, not surface symptoms
- A correct foundational fix that causes small regressions in other tests is acceptable
- Study Blink's implementation BEFORE writing code
- Mirror Blink's types, algorithms, and constraint-passing patterns
- Commit at each milestone and report what you found/changed
- Run ONLY bidi tests during development

## Key Source Files
- `pkg/layout/inline_item.go` — bidi item processing, level resolution
- `pkg/layout/line_box.go` — line box creation with bidi direction
- Search for `ResolveBidiLevels`, `StripBidiControls`, `SplitItemsAtLevelBoundaries`
- `pkg/layout/` — grep for "bidi" to find all relevant files

## Reference
- Blink: `third_party/blink/renderer/core/layout/ng/inline/ng_inline_items_builder.cc` — items build with bidi
- Blink: `third_party/blink/renderer/platform/text/bidi_resolver.h` — UAX#9 resolution
- Search: https://source.chromium.org/chromium/chromium/src
