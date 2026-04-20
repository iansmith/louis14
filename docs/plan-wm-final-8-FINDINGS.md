# Findings & Decisions: Final 8 WM Test Failures

## Requirements

- Fix 8 failing `css-writing-modes` WPT tests to 0% pixel diff.
- Per CLAUDE.md: foundational correctness (all cases, not point fixes); study Blink before coding; all tests pass (0% diff).
- Each fix must be aligned with Blink/Chromium LayoutNG algorithms.
- Worktree implementation agents use Sonnet 4.6 (not Opus).

## Research Findings

Per-area findings from parallel Blink-research agents. Detailed code traces are in the per-area docs under `docs/plan-B[1-6]-*.md`.

### B1 — Inline-block baseline in VLR + text-orientation:sideways

**Test:** `inline-block-alignment-007.xht` (8.4% diff)

Three coupled bugs:
1. `text-orientation` is missing from `inheritableProperties` in `pkg/css/cascade.go` (~line 680-690). CSS Writing Modes L3 §6.1 makes it inherited. Descendants of `#lr-sideways` see no `text-orientation`, so `UsesCentralBaselineWithStyle` wrongly returns central baseline.
2. `computeLineMetricsEx` (`pkg/layout/inline_layout.go` ~line 885) treats typographic ascent as alignment_ascent. Correct for `sideways-lr/rl` keywords; wrong for `vertical-lr/rl + text-orientation: sideways`. After 90° CW rotation, alignment_ascent should equal typographic_descent (0.2em for Ahem) and alignment_descent should equal typographic_ascent (0.8em).
3. `IsSidewaysLR` in `pkg/layout/engine.go::fragmentToBox` (~lines 296-298) only triggers on `WritingModeSidewaysLR`, missing the `vertical-lr + text-orientation:sideways` combination. Ahem squares mask the visual issue; real fonts would render wrong glyphs.

Blink references: `ComputedStyle::GetFontBaseline()`, `LogicalBoxFragment::BaselineMetrics()`, `font_baseline.h::FontBaseline` enum.

### B2 — Mongolian / vertical-script orientation

**Tests:** `text-orientation-script-001a`, `text-orientation-script-002a`

Root cause: `pkg/layout/engine.go` lines 411-419 makes a monolithic per-fragment orientation decision based on the first character. UTR#50 requires per-character classification; scripts native to vertical writing (Mongolian U+1800-U+18AF, Phags-Pa U+A840-U+A87F, Mongolian Supplement U+11660-U+1166C) stay upright in `mixed` orientation.

`pkg/text/orientation.go::ShouldRotateSideways` exists but is dead code — never called from the layout pipeline.

Secondary: `pkg/layout/line_breaker.go::isVerticalMeasurement` (lines 123-133) must use upright em-square advance for vertical-script characters.

### B3 — Abs-pos static position across writing modes

**Test:** `abs-pos-border-offset-003` (0.9% diff, 3 of 6 containers failing)

Two coupled bugs:
1. `pkg/layout/out_of_flow_layout.go` lines 213-215 and 242-244 ignore `StaticPosition.InlineEdge/BlockEdge`. Cross-WM conversion emits `BlockEdge=End` to mean "offset measures from CB-start to item's END". Treating `End` as `Start` places the item on the wrong side.
2. `pkg/layout/block_layout.go` line 785: `needsConversion := parentWDM.IsOrthogonalTo(childWDM)` is too narrow. VRL↔VLR (both vertical, opposite block direction) needs conversion but is skipped. Should be `childWDM.WM != parentWDM.WM || childWDM.Dir != parentWDM.Dir`.

**Critical coupling:** Containers 1/5/6 currently pass via cancelling bugs. Fixing one alone regresses them. Apply B3.1+B3.2+B3.3 in a single commit.

Blink references: `out_of_flow_layout_part.cc::PropagateOOFPositionedInfo`, `LogicalStaticPosition::ConvertToPhysical()`, `absolute_utils.cc::ComputeUnclampedIMCBInOneAxis`.

### B4 — unicode-bidi: plaintext multi-paragraph

**Test:** `block-plaintext-006` (0.9% diff)

`collectTextNode` in `pkg/layout/inline_item.go` (lines 271-284): for `white-space: pre` (`collapseSpaces=false`), raw text including `\n` is written as ONE contiguous string without emitting `InlineItemControl` items. `preserveNewlines=true` is computed but never consulted in this branch.

Consequence: all 3 paragraphs end up on ONE line because the line breaker has no `kControl` items to break at; `lineParagraphLevel` is taken from the first item (LTR), so RTL paragraph 2 is reordered incorrectly inside the LTR line.

Blink reference: `inline_items_builder.cc::AppendText` always emits `InlineItem::kControl` for `\n` when `preserveNewlines=true`, regardless of `collapseSpaces`.

Secondary: `pkg/html/parser.go` lines 145-152 strip a leading `\n` from `<pre>` when no prior children exist. The WPT test uses an HTML comment to suppress this; louis14 discards comments so the strip still fires. Fix: track `commentSeenInPre` on the parser.

### B5 — sideways-lr flex column main axis

**Test:** `sideways-lr-main-axis` (0.6% diff)

`buildItemConstraintSpace` in `pkg/layout/flex_layout.go` (column branch, lines 1600-1626) sets `IsFixedBlockSize=true` with the flex-resolved main size but does NOT set an override flag. `CalculateInitialFragmentGeometry` (`pkg/layout/fragment_geometry.go` line 523) then re-derives the block size from CSS `ResolveBlockSize`, which for sideways-lr reads CSS `width`.

Main repo has the fix already: `SetIsBlockSizeOverride(true)` at flex_layout.go:3418 and 3470. The worktree is missing both the `IsBlockSizeOverride` field on `ConstraintSpace` and the call sites.

`computeMainIsItemInline` is correct: sideways-lr container + sideways-lr item + `flex-direction: column` → `mainIsItemInline=false`.

### B6 — Iframe orthogonal root resize

**Tests:** `orthogonal-root-resize-icb-001..007` (7 tests)

Two missing JS engine features prevent the reftest mutation:
1. `requestAnimationFrame` / `cancelAnimationFrame` are not registered on the goja runtime (`pkg/js/engine.go::New`). The test's `iframe.onload = () => rAF(() => rAF(() => { iframe.style.height = "100px" }))` throws at the rAF call.
2. Element-level `onload` callbacks are stored via `elementAccessor.Set` (`pkg/js/dom.go` line 519) but never dispatched. Only `window.onload` fires today.

Layout pipeline is already correct: once `iframe.style.height = "100px"` lands, `layoutNestedDocument` with `vpHeight=100` produces the expected 100×100 green square.

Blink references: `LocalFrameView::UpdateStyleAndLayout`, `LayoutEmbeddedContent::UpdateGeometry`, `NGBlockLayoutAlgorithm::ComputeOrthogonalChildrenInlineSize`.

## Cross-Cutting Themes

Every plan independently surfaced one or more of:

1. **Per-character vs monolithic decisions** (B1, B2): orientation, baseline, and inheritance must be per-character or per-style-token, not bulk per-fragment.
2. **Constraint-space override flags missing** (B5): main-repo `IsBlockSizeOverride` pattern needs porting.
3. **Cross-WM coordinate conversion gaps** (B3): `IsOrthogonalTo` is too narrow anywhere it gates conversion; should be `WM != WM || Dir != Dir`.
4. **Inline control-item generation** (B4): `\n` in `pre` must emit `InlineItemControl`, mirroring Blink's `AppendText`.
5. **JS engine completeness** (B6): rAF + element-level `onload` block all dynamic-resize iframe tests.

## Technical Decisions

| Decision | Rationale |
|----------|-----------|
| 4-agent dispatch (I1-I4), not 6 | Groups by shared file surface; I1 pure-cascade, I3 constraint-space, I4 JS — prevents merge conflicts. |
| Sonnet 4.6 for implementation agents | User directive; Opus reserved for research/synthesis. |
| B3 bugs land atomically | Cancelling-bug pairs; either alone regresses currently-passing containers. |
| Defer `bidi-dynamic-iframe-001` | Needs `iframe.contentDocument` + `Text.appendData` JS APIs not yet implemented. |
| rAF synchronous fire | Single-threaded test environment; callbacks execute as a straight call stack. |
| `text-orientation` inheritance globally enabled | Spec-correct per CSS WM L3 §6.1; narrow `sidewaysVLR` guard isolates behavioral change. |

## Issues Encountered

| Issue | Resolution |
|-------|------------|
| Plan-type agents (read-only) cannot commit plan files to worktree | Parent agent saves plan content from agent result message into `docs/plan-B[1-6]-*.md`. |
| `go test` fails with `invalid go version '1.25.5'` | Use `GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go test`. |
| Test name `TestReftest` returns no tests | Correct name is `TestWPTCSS3Reftests`. |

## Integration Lessons (2026-04-20)

These are durable lessons from the I1/I2/I3/I4 dispatch round. Future agents MUST read this section.

### 1. Worktree agents start from HEAD, not the working directory

Per CLAUDE.md §5. If parent-side changes are not committed + pushed before a
worktree agent is launched, the agent won't see them. Always run
`git add -A && git commit && git push` on the authoritative branch before
dispatching. This is especially critical when multiple sequential dispatches
depend on the previous dispatch's output being merged first.

### 2. Enforce milestone commits — never "I'll commit everything at the end"

I2 ran ~3 hours without a single commit. When stopped, the only artifact was a
14.8KB uncommitted diff in its worktree. Had B1.2/B1.3 been committed at
milestones, they would have been cherry-pickable directly. Every dispatch prompt
must say: *"Commit at every B-step milestone. Report the SHA of each milestone
commit back in your summary. Do not batch commits to the end."*

### 3. Agents will drift scope if the prompt is loose

I2 rabbit-holed into `mazzy/mazarin/textshape/draw_context.go` looking for an
unrelated transform matrix — nowhere in B1/B2's scope. Dispatch prompts must
explicitly enumerate the files the agent is permitted to modify, and forbid
exploration outside that set without first reporting back.

### 4. Check `subagents/<id>.jsonl` mtime, not `.output`, to tell if an agent is live

The parent's `.output` file gets stale while the agent continues to stream into
`subagents/<id>.jsonl`. I misread a stale `.output` (mtime 3h42m) as "hung"
when the agent was actively writing to the jsonl (mtime ~1 min). The jsonl mtime
is the source of truth for liveness.

### 5. Salvage, don't cherry-pick, when an agent never committed

If the agent has zero commits on its branch, `git cherry-pick` has nothing to
take. Capture `git diff > /tmp/<slug>.patch` from the worktree, audit it
line-by-line (debug prints, go.mod bumps, out-of-scope edits must be stripped),
and hand-apply the in-scope portion on the authoritative branch.

### 6. Merge in file-surface-grouped order, not timeline order

Dispatches I1/I2/I3/I4 were grouped by file surface (cascade/parser vs
baseline/orientation vs constraint-space vs JS) specifically so merge conflicts
would be minimal. Merging I1 → I3 → I4 (skipping the stopped I2) produced
3 + 5 + 6 manageable conflicts; an arbitrary order would have been worse. When
a dispatch is stopped, keep the original order for the remaining branches.

### 7. Build + vet after every merge

After each merge commit, before the next one, run
`GOTOOLCHAIN=go1.25.5 ~/sdk/go1.25.5/bin/go build ./... && go vet ./...`.
If something breaks, fix on the merge commit — don't pile on the next merge.

### 8. Dedupe overlapping work at merge time

I2 and I1 both added `text-orientation` to `inheritableProperties`. I1 and I4
both changed `OpenFontRequest` from `Path` to `Family/Variant`. At merge, take
the earlier-landing version and drop the duplicate; don't carry dual
implementations into the integrated branch.

### 9. Strip debug prints before committing merge resolutions

The I2 salvage patch had 4 `fmt.Printf` debug lines. They will happily build +
vet + test clean, but they're still debug code. Before every commit, grep the
diff for `fmt.Printf`, `fmt.Println`, `log.Printf` and remove anything that's
not gated behind a debug flag or logger.

## Resources

Per-area implementation plans (detailed code traces, line numbers, verification steps):

- [B1 — Inline-block baseline VLR+sideways](docs/plan-B1-inline-block-baseline.md)
- [B2 — Mongolian/vertical-script orientation](docs/plan-B2-mongolian-orientation.md)
- [B3 — Abs-pos static position across WM](docs/plan-B3-abs-pos-border-offset.md)
- [B4 — unicode-bidi plaintext multi-paragraph](docs/plan-B4-bidi-plaintext.md)
- [B5 — sideways-lr flex column main axis](docs/plan-B5-sideways-lr-flex.md)
- [B6 — Iframe orthogonal root resize](docs/plan-B6-iframe-orthogonal-relayout.md)

Blink references:
- LayoutNG: `third_party/blink/renderer/core/layout/`
- Inline formatting: `core/layout/inline/inline_items_builder.cc`, `bidi_paragraph.cc`
- OOF: `out_of_flow_layout_part.cc`, `absolute_utils.cc`
- Flex: `flex_layout_algorithm.cc`
- Frame/iframe: `core/frame/local_frame_view.cc`, `core/layout/layout_embedded_content.cc`

Project CLAUDE.md: `/Users/iansmith/louis14/CLAUDE.md` — foundational correctness, study Blink first, all tests pass.

## Visual/Browser Findings

- `inline-block-alignment-007`: blue polygon shifted ~210px to the right of reference position (8.4% diff = 40320px).
- `abs-pos-border-offset-003`: diff image shows red pixels only in Containers 2/3/4 (HTB parent inside VRL container); Containers 1/5/6 (VLR parent inside VRL) currently pass.
- `sideways-lr-main-axis`: 4 `flex-direction: column` containers render only 1-2 items instead of 3; items overlap.
- `block-plaintext-006`: 3 paragraphs collapse onto 1 line; RTL line-2 content interleaved/garbled within LTR context.
- `orthogonal-root-resize-icb-*`: iframe renders at 50px height (initial CSS) instead of post-mutation 100px; top half green + bottom half red vs expected solid green 100×100.

---

*Per-area plans under `docs/` are the authoritative detailed references. This file indexes themes and decisions.*
