---
name: next-test-batch
description: Analyze test failures in a given test section, identify the top 3 foundational fixes based on Blink's implementation, and generate a continuation prompt for worktree agents to implement them in parallel.
allowed-tools: Bash Read Write Grep Glob Agent WebSearch WebFetch
argument-hint: [section-name e.g. "wm", "css2", "flex", "grid", "tables"]
---

# Next Test Batch: Analyze Failures and Generate Continuation Prompt

The user wants to analyze the failing tests in a specific section and produce a continuation prompt (a markdown file in docs/) that a fresh session can use to launch 3 parallel worktree agents to fix the highest-impact foundational issues.

## Section Name Mapping

The argument `$ARGUMENTS` maps to test suites as follows. If the argument is empty or unclear, ask the user which section to analyze.

| Shorthand | Test Function | Test Dir | Description |
|-----------|--------------|----------|-------------|
| `wm` | `TestWPTCSS3Reftests/css-writing-modes` | `testdata/wpt-css3/css-writing-modes` | CSS Writing Modes |
| `css2` | `TestListReftestResults` + `TestWPTReftests` | `testdata/wpt-css2` | CSS 2.1 |
| `flex` | `TestWPTCSS3Reftests/css-flexbox` | `testdata/wpt-css3/css-flexbox` | Flexbox |
| `grid` | `TestWPTCSS3Reftests/css-grid` | `testdata/wpt-css3/css-grid` | Grid |
| `tables` | `TestWPTCSS3Reftests/css-tables` | `testdata/wpt-css3/css-tables` | Tables |
| `text` | `TestWPTCSS3Reftests/css-text` | `testdata/wpt-css3/css-text` | CSS Text |
| `position` | `TestWPTCSS3Reftests/css-position` | `testdata/wpt-css3/css-position` | CSS Position |
| Other | `TestWPTCSS3Reftests/<arg>` | `testdata/wpt-css3/<arg>` | Direct CSS3 subsection |

## Step-by-Step Procedure

### Step 1: Ask for the section (if not provided)

If `$ARGUMENTS` is empty, ask the user:
> Which test section should I analyze? (e.g., wm, css2, flex, grid, tables)

### Step 2: Run the tests and collect failures

Run the appropriate test command and capture ALL failing test names with their pixel diff counts. Working directory is `pkg/visualtest`.

For CSS3 sections:
```bash
cd pkg/visualtest && go test -v -run "TestWPTCSS3Reftests/<section>" -count=1 2>&1
```

For CSS2:
```bash
cd pkg/visualtest && go test -v -run "TestListReftestResults" -count=1 2>&1
cd pkg/visualtest && go test -v -run "TestWPTReftests" -count=1 2>&1
```

Parse the output to extract:
- Each failing test name
- Pixel diff count and percentage (from "REFTEST FAIL: X/Y pixels differ (Z%)")
- Total pass/fail counts

### Step 3: Categorize failures by root cause

Group the failing tests by examining their test HTML files to understand what CSS feature each tests. Look for patterns:
- Tests with similar names (e.g., `float-vlr-*`, `percent-margin-*`) likely share a root cause
- Read a sample of failing test HTML files to understand what CSS property/behavior they exercise
- Group tests that test the same underlying layout algorithm or CSS feature

For each group, identify:
- The CSS specification section being tested
- The approximate number of tests in the group
- Total pixel diff across the group
- Which source files in `pkg/layout/`, `pkg/render/`, or `pkg/css/` are likely involved

### Step 4: Research Blink's implementation for each group

For each of the top candidate groups, use WebSearch and WebFetch to research how Blink/Chromium implements the relevant feature:
- Search chromium.googlesource.com for the relevant class/algorithm
- Identify the key data types, algorithms, and control flow
- Note how Blink's approach differs from the current louis14 implementation
- Focus on the algorithmic structure, not line-by-line porting

Key Blink source locations:
- Layout: `third_party/blink/renderer/core/layout/`
- Block layout: `block_layout_algorithm.cc`
- Inline layout: `inline_layout_algorithm.cc`
- Flex: `flex_layout_algorithm.cc`
- Grid: `grid_layout_algorithm.cc`
- Float: `exclusion_space.cc`, `unpositioned_float.cc`
- Writing modes: `logical_fragment.cc`, `physical_fragment.cc`
- Constraint space: `constraint_space.cc`, `constraint_space_builder.cc`

### Step 5: Select 3 targets

Choose 3 targets following these rules (in priority order):

1. **Target 1 is always the most foundational fix** - the one that addresses the deepest architectural issue or fixes the most systemic root cause, even if it doesn't flip the most tests.

2. **Targets 2 and 3 must not overlap with Target 1 or each other in source files.** This is a HARD CONSTRAINT because they will be implemented by parallel worktree agents. Check the independence matrix:
   - List which `.go` files each target would modify
   - If two targets touch the same file, demote the less foundational one and pick the next candidate
   - Acceptable overlap: test files, since agents run in worktrees

3. **Among non-overlapping candidates, prefer more foundational fixes** over higher test counts. A fix that corrects an algorithm is better than one that handles an edge case, even if the edge case affects more tests.

### Step 6: Generate the continuation prompt

Write the continuation prompt to `docs/PROMPT-<section>-round<N>-improvements.md` where `<N>` is the next round number (check existing files).

The prompt MUST follow this exact structure:

```markdown
# <Section> Round <N>: Top 3 Improvements

Current state: X pass / Y fail (Z% pass rate) across W tests.
[Additional baseline metrics as relevant]

These three targets are **independent** (touch different subsystems) and can be worked on in parallel by separate worktree agents.

---

## Target 1: <Name> (~N tests)

### Problem
[Clear description of what's wrong]

### Affected Tests (~N failures)
[Table of test categories and counts]

### Root Cause (from code analysis)
[Specific file, function, line number, and code snippet showing the bug]

### What Blink Does
[How Blink implements this correctly, with reference to specific Blink source files]

### Fix Location
[Specific files, functions, and line numbers to change, with before/after code snippets]

### Verification
[Exact test commands to run, including regression checks]

---

## Target 2: ...
[Same structure]

---

## Target 3: ...
[Same structure]

---

## Independence Check

| | file1.go | file2.go | ... |
|---|---|---|---|
| Target 1 | Yes | - | ... |
| Target 2 | - | Yes | ... |
| Target 3 | - | - | ... |

All three targets touch different subsystems and can be developed independently.

## IMPORTANT: Agent Guidelines

- **Study Blink's approach** before writing code in any new area.
- **Commit and report at each milestone** (don't batch everything to the end).
- [Regression constraints specific to this section]
- [Target-specific cautions]
```

### Step 7: Output the path

Print the full path to the generated file:
```
Continuation prompt written to: /Users/iansmith/louis14/docs/PROMPT-<section>-round<N>-improvements.md
```

## Key Principles (from project memory)

- **Foundational correctness over test counts**: Every change must make the codebase structurally more correct, even if it causes regressions.
- **Study Blink first**: Always research Blink's actual implementation before proposing fixes. Port their algorithms, not reinvent.
- **Stay faithful to Blink's algorithms**: Don't drift back to simpler patterns.
- **Tests are never wrong**: If a test fails, the bug is in our code.
- **Non-determinism is our bug**: "Flaky" tests indicate bugs in our rendering engine.
- **Agents commit at milestones**: Instruct agents to commit after each meaningful fix.
- **No overlapping files**: The 3 targets MUST be implementable by parallel worktree agents.
