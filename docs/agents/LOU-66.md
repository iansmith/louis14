# LOU-66: Orthogonal sizing — fix 16-20 failing tests

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
Orthogonal sizing tests are in `css-writing-modes`. Start with the VLR group:
```
# VLR orthogonal sizing (001, 003, 009, 013, 015, 021):
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -count=1 ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes/abs-pos-non-replaced-vlr' -timeout 60s
```

## Build Command
```
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./pkg/layout/
```

## The Bug
**16-20 failing orthogonal sizing tests** with identified root causes:

**VLR group (6 tests):** 001 3.8%, 003 0.3%, 009 0.3%, 013 3.4%, 015 0.3%, 021 0.3%.
Root cause: margin collapsing for `<p>` in VLR — reference expects 36px contribution (collapsed start margin + 20px line + 16px end), engine produces 52px (20px line + 32px margins). 0.3% tests likely from `ch` unit resolution or table padding/box-sizing.

**VRL group (10 tests):** Sibling tests with ~32px offset from margin collapsing. References use `left: calc(100% - ...)` positioning and reversed cell order.

**3 remaining VRL tests** (008 1.8%, 013 3.4%, 020 1.6%): ~16-20px horizontal shift. VLR siblings pass pixel-perfect. Hypothesis: parent-child margin collapsing in VRL where block-start = RIGHT.

**Attack order:**
1. Fix 0.3% tests first (ch unit, column width rounding)
2. Fix margin collapsing for `<p>` in VLR
3. Fix VRL after VLR correct

## Session-Specific Rules
- NEVER make changes outside your git worktree
- Focus on foundational correctness — fix root causes, not surface symptoms
- A correct foundational fix that causes small regressions in other tests is acceptable
- Study Blink's implementation BEFORE writing code
- Mirror Blink's types, algorithms, and constraint-passing patterns
- Commit at each milestone and report what you found/changed
- Run ONLY orthogonal sizing tests during development

## Key Source Files
- `pkg/layout/block_layout.go:95-101` — margin collapsing
- `pkg/layout/writing_mode_converter.go:146-157` — physical offset conversion
- `pkg/layout/fragment_builder.go:140-168` — strut handling
- `pkg/layout/out_of_flow_layout.go` — static position for VRL+RTL

## Reference
- Blink source: `third_party/blink/renderer/core/layout/ng/` (LayoutNG)
- Search: https://source.chromium.org/chromium/chromium/src
