# Task Plan: Final 8 CSS Writing Modes Test Failures

## Goal

Fix the last 8 failing `css-writing-modes` WPT tests to 0% pixel diff, using Blink-aligned algorithms across 6 root-cause areas (B1-B6), with 1 test deferred pending new JS APIs.

## Current Phase

Phase 2

## Phases

### Phase 1: Research & Plan Synthesis

- [x] Triage 8 failures into 6 root-cause areas
- [x] Dispatch 6 parallel sonnet-4.6 Blink-research agents (B1-B6)
- [x] Receive and save per-area plans to `docs/plan-B[1-6]-*.md`
- [x] Synthesize unified master plan (content now in this file)
- **Status:** complete

### Phase 2: Dispatch I1 — Cascade & Parser Fixes

Low-risk pure-cascade changes. Tests: `block-plaintext-006`, partial foundation for `inline-block-alignment-007`.

- [ ] B1.1 Add `text-orientation` to `inheritableProperties` in `pkg/css/cascade.go`
- [ ] B4.1 Comment-aware leading `\n` strip in `pkg/html/parser.go`
- [ ] B4.2 Emit `InlineItemControl` per `\n` in `collectTextNode` `!collapseSpaces` branch (`pkg/layout/inline_item.go`)
- [ ] Verify `block-plaintext-006` at 0% diff
- [ ] Regression spot-check: `bidi-plaintext-*`, `block-plaintext-001..005`
- **Status:** pending

### Phase 3: Dispatch I2 — Baseline + Orientation Refactor

Medium risk. Tests: `inline-block-alignment-007`, `text-orientation-script-001a/002a`.

- [ ] B1.2 Swap ascent/descent in `computeLineMetricsEx` for VLR/VRL+sideways (`pkg/layout/inline_layout.go`)
- [ ] B1.3 Broaden `IsSidewaysLR/RL` setter in `fragmentToBox` (`pkg/layout/engine.go`)
- [ ] B2.1 Add `IsVerticalScriptCharacter` to `pkg/text/orientation.go`; wire `ShouldRotateSideways` into layout pipeline
- [ ] B2.2 Per-character orientation split in `engine.go` lines 408-419
- [ ] B2.3 `isVerticalMeasurement` upright-advance for vertical-script chars (`pkg/layout/line_breaker.go`)
- [ ] Verify `inline-block-alignment-007`, `text-orientation-script-001a/002a` at 0% diff
- [ ] Regression spot-check: `text-orientation-mixed-*`, `inline-block-alignment-001..006`
- **Status:** pending

### Phase 4: Dispatch I3 — Constraint Space + OOF Static Position

Medium risk; B3 bugs are coupled and must land atomically. Tests: `sideways-lr-main-axis`, `abs-pos-border-offset-003`.

- [ ] B5.1 Add `IsBlockSizeOverride` field + builder method to `pkg/layout/constraint_space.go`
- [ ] B5.2 Honor flag in `CalculateInitialFragmentGeometry` (`pkg/layout/fragment_geometry.go`)
- [ ] B5.3 Set flag in row + column branches of `buildItemConstraintSpace` (`pkg/layout/flex_layout.go`)
- [ ] B3.1 Switch on `InlineEdge`/`BlockEdge` in `pkg/layout/out_of_flow_layout.go`
- [ ] B3.2 Broaden `needsConversion` in `PropagateOOFCandidates` (`pkg/layout/block_layout.go`)
- [ ] B3.3 Extend `childContentPhys` computation for parallel-but-different WM
- [ ] ⚠ Apply B3.1+B3.2+B3.3 in a single commit (bugs cancel each other)
- [ ] Verify `sideways-lr-main-axis` and all 6 containers of `abs-pos-border-offset-003` at 0% diff
- [ ] Regression spot-check: `abs-pos-border-offset-001/002`, `flexbox-writing-mode-slr*`
- **Status:** pending

### Phase 5: Dispatch I4 — JS Engine

Low risk, no layout overlap. Tests: `orthogonal-root-resize-icb-001..007` (7 tests).

- [ ] B6.1 Add `onloadCallbacks` to `Engine`; register synchronous `requestAnimationFrame`/`cancelAnimationFrame` in `pkg/js/engine.go::New`
- [ ] B6.2 `domContext.engine` field; `elementAccessor.Set` handles `"onload"` (`pkg/js/dom.go`)
- [ ] B6.3 `Execute()` fires element-level `onload` + `<body onload>` attribute
- [ ] Verify all 7 `orthogonal-root-resize-icb-*` tests at 0% diff
- **Status:** pending

### Phase 6: Integration & Verification

- [ ] Merge I1 → I2 → I3 → I4 (in order) into the fix branch
- [ ] Full `TestWPTCSS3Reftests` css-writing-modes run
- [ ] Confirm all 7 targeted tests pass; document deferred `bidi-dynamic-iframe-001`
- [ ] Broader regression spot-check against any passing vertical/bidi tests
- **Status:** pending

## Failure → Plan Mapping

| # | Test | Plan | Severity | Phase |
|---|------|------|----------|-------|
| 1 | `inline-block-alignment-007` | B1 | High (8.4% diff) | 3 (after 2) |
| 2 | `text-orientation-script-001a` | B2 | Medium | 3 |
| 3 | `text-orientation-script-002a` | B2 | Medium | 3 |
| 4 | `abs-pos-border-offset-003` | B3 | Low (0.9%) | 4 |
| 5 | `block-plaintext-006` | B4 | Low (0.9%) | 2 |
| 6 | `sideways-lr-main-axis` | B5 | Low (0.6%) | 4 |
| 7-13 | `orthogonal-root-resize-icb-001..007` | B6 | Medium | 5 |
| defer | `bidi-dynamic-iframe-001` | needs `iframe.contentDocument` | — | — |

## Key Questions

1. Will B1.1 (`text-orientation` inherited) regress any currently-passing vertical-script tests? (Must spot-check during Phase 2/3.)
2. Does the `childContentPhys` computation need adjustment for parallel VRL↔VLR, or will Containers 1/5/6 of `abs-pos-border-offset-003` pass as-is? (Must verify all 6 containers during Phase 4.)
3. Does `IsBlockSizeOverride` interact with `IsFixedBlockSizeIndefinite` in any existing code paths? (Must verify during Phase 4.)

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| 4-agent dispatch (I1-I4), not 6 | Groups fixes by shared file surface to avoid merge conflicts; I1 groups pure-cascade, I3 groups constraint-space, I4 isolates JS. |
| Sonnet 4.6 for implementation agents | User directive; Opus reserved for plan synthesis. |
| Keep B1-B6 per-area docs | Detailed code traces would balloon `findings.md`; linked as resources instead. |
| Defer `bidi-dynamic-iframe-001` | Requires `iframe.contentDocument` + `Text.appendData` JS APIs not yet implemented; out of scope. |
| B3 bugs must land atomically | Containers 1/5/6 pass today via cancelling bugs; either fix alone regresses them. |

## Errors Encountered

| Error | Attempt | Resolution |
|-------|---------|------------|
| `go test` failed with `invalid go version '1.25.5'` | 1 | Use `GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test` |
| `TestReftest` returned no tests | 1 | Correct name is `TestWPTCSS3Reftests` |
| Plan agents (read-only) could not commit plan files | 1 | Parent agent saves plan content returned in agent result message |

## Notes

- Per CLAUDE.md: commit + push before launching any worktree agent (worktrees start from HEAD).
- Instruct implementation agents to commit at each B-step milestone, not just at the end.
- Worktree agents must use `model: "sonnet"`.
- Run only the 1-4 tests associated with each phase during feature work; full suite only at Phase 6.
