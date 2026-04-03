# Continuation prompt — sideways remaining failures

Execute the plan in `docs/sideways-remaining-failures-plan.md` to fix the remaining
sideways writing-mode WPT test failures.

## Context

Branch: `rewrite/louis13-louis14`  
Last commit: `f0a7c8f0` — "Fix sideways text rendering and fragment builder coordinate bug"

Sideways text *rendering* is correct (off-screen buffer + pixel rotation in
`pkg/render/render.go`). The remaining failures are **layout** bugs.

## Before you touch code

1. Run the full sideways test suite to get the current baseline:
   ```bash
   GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
     -run "TestWPT/css-writing-modes" -v 2>&1 \
     | grep -E "(PASS|FAIL).*(slr|srl|sideways)" | sort
   ```
   Expected: 37 pass, 12 fail (see plan for the 12 test names).

2. For each group in the plan, read the failing test file AND a passing sibling
   (e.g., `block-flow-direction-slr-047.xht` passes while `slr-043` fails) before
   editing any code.

3. Study Blink's handling of sideways writing modes for any area you're working on.
   Sideways modes have `IsFlippedBlocks()` returning the opposite of vertical-rl/lr,
   which is the key logical difference.

## Key files

- `pkg/layout/block_layout.go` — Groups 1 and 2
- `pkg/layout/inline_layout.go` — Group 3
- `pkg/layout/flex_layout.go` — Group 5 (flex)
- `pkg/layout/writing_mode_converter.go` — reference for axis mapping
- `pkg/css/style.go` — `WritingModeSidewaysLR`, `WritingModeSidewaysRL` constants

## Workflow per group

1. Read the test and reference files to understand expected pixel output.
2. Identify what the layout engine is producing vs. what's expected.
3. Make the minimal fix.
4. Run the specific test(s) for that group to confirm improvement.
5. Run the full writing-modes suite to confirm no regressions.
6. Commit.

Never modify `pkg/render/render.go` — rendering is correct.
