# Coordinator Continuation Prompt

Paste this into a new Claude Code session to resume coordination.

---

I am coordinating 11 parallel agents working on WM and flexbox test improvements for the louis14 browser engine. The full plan with detailed per-agent instructions is in `docs/PARALLEL-AGENT-PLAN.md` — read it now.

**Baseline**: WM 327/790 (41.4%), Flexbox 380/630 (60.3%)
**Branch**: `rewrite/louis13-louis14`
**Goal**: Merge each agent's worktree branch, in order, verifying compilation and no regressions after each merge.

## Agent Summary

| # | Task | Key files | Est. gain |
|---|------|-----------|-----------|
| 1 | Flex justify-content overflow fallback + self-start/self-end + normal/initial | flex_layout.go | +20-25 flex |
| 2 | Flex baseline alignment (last-baseline, synthesis, line cross-size, LastBaseline field) | flex_layout.go, layout_result.go, fragment_builder.go, block_layout.go | +15-20 flex |
| 3 | Flex §4.5 automatic minimum sizing (specified/transferred suggestions, overflow check) | flex_layout.go | +15-20 flex |
| 4 | Flex aspect-ratio (stretch recomputation, intrinsic AR, replaced cross-size line calc) | flex_layout.go | +20-30 flex |
| 5 | WM relative positioning % fix + CSS clip:rect() implementation | block_layout.go, render/paint_layer.go | +25-30 WM |
| 6 | WM percentage margins (parent WDM) + percentage padding support | block_layout.go, fragment_geometry.go, css/style.go | +16 WM |
| 7 | WM float/clear physical→logical mapping | block_layout.go, exclusion_space.go | +20-34 WM |
| 8 | WM orthogonal flow sizing (min-block, ICB clamp, orthogonal min/max) | block_layout.go, min_max_sizing.go | +20-40 WM |
| 9 | Abs-pos paint order (text fragments inherit position — 1-line fix) + ICB RTL | engine.go | +100-120 WM |
| 10 | Table layout (cell size lookup, border-collapse, caption, border-spacing) | table_layout.go | +15-25 WM |
| 11 | Bidi UAX#9 L4 mirroring + unicode-bidi control char injection | engine.go, inline_item.go | +40-57 WM |

## Merge Order (this order matters)

Phase 1 — WM: 9, 11, 5, 7, 6, 8, 10
Phase 2 — Flex: 1, 3, 4, 2

## Per-merge procedure

```bash
# Find agent branches
git branch --list 'agent-*'
ls .claude/worktrees/

# Merge one branch
git merge --no-ff <agent-branch>
go build ./...
go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5
go test -v -run TestWPTCSS3Reftests/css-flexbox ./pkg/visualtest/ -timeout 600s 2>&1 | tail -5
```

**Accept** if it compiles and pass counts don't drop. **Reject** (revert the merge) if regressions appear — investigate before retrying.

## If agents are still running

Check `.claude/worktrees/` for active worktrees. Merge completed ones first. You can inspect an agent's progress by reading files in its worktree directory.

## If all agents are done

Merge all in order above, running tests after each. After all merges, run the full suites and report final WM and flexbox pass/fail counts vs the baseline.
