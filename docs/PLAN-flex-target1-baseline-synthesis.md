# Plan: Target 1 — Flex Baseline Synthesis (Blink Parity)

**Source**: `docs/PROMPT-flex-round11-improvements.md` Target 1.
**Branch**: `fix/flexbox-fast`.
**Goal**: Make louis14's flex baseline resolution match Blink/LayoutNG for multi-line, orthogonal, wrap-reverse, and fieldset scenarios. Close the 10 failing tests listed below without regressing the motivating-three.

## Project rules (load-bearing)

From `CLAUDE.md`:

1. **Foundational correctness over quick wins.** Every fix must work for ALL cases. No filtering by error %, no "near passes". Solve the category completely.
2. **Study Blink BEFORE writing any code.** Quote Blink source in every commit message.
3. **All tests must pass at 0% diff.** Exception: if a test also fails in Chrome (verify via wpt.fyi `status:"F"`), we accept the failure and record evidence.
4. **Test execution discipline.** Run only the 1–4 tests for the current phase during iteration. Full-suite runs are for commit checkpoints.
5. **Operational rules.** Never `open` files from agents. Commit+push before launching worktree agents. Agents commit at each milestone, not only at end. Worktrees commit to worktree branch only.

## Failing tests (Target 1)

| Test | Current diff |
|---|---|
| flexbox-align-self-baseline-horiz-006.xhtml | 8120 |
| flexbox-align-self-baseline-horiz-008.xhtml | 25862 |
| flexbox-align-self-horiz-002.xhtml | 235 |
| flexbox-baseline-align-self-baseline-horiz-001.html | 557 |
| flexbox-baseline-multi-item-horiz-001a.html | 122 |
| flexbox-baseline-multi-item-horiz-001b.html | 122 |
| flexbox-baseline-multi-line-horiz-002.html | 574 |
| flexbox-baseline-multi-line-vert-001.html | 1649 |
| flexbox-baseline-multi-line-vert-002.html | 1436 |
| fieldset-baseline-alignment.html | 251 |

**Verification command** (run after each phase):
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-baseline|flexbox-align-self-baseline|flexbox-align-self-horiz-002|fieldset-baseline)' \
  -count=1
```

**Regression guard** (must stay PASS after each phase):
```bash
GOTOOLCHAIN=local ~/sdk/go1.25.5/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox/(flexbox-align-self-baseline-horiz-001a|flexbox-align-self-baseline-horiz-001b|flexbox-align-self-horiz-001-table)' \
  -count=1
```

## Structural diff: Blink vs louis14 today

| # | Area | Blink | Louis14 | Fix location |
|---|---|---|---|---|
| 1 | First-baseline line selection | Always DOM-order `line[0]`; synthesize if no baseline-aligned item on it | Falls back to `lines[len-1]` (visual-first under wrap-reverse) when `line[0]` has no baseline item — flex_layout.go:1437-1453 | Container export block |
| 2 | Orthogonal items | **Excluded** from baseline accumulation | Synthesizes at `crossSize` and treats as participant — flex_layout.go:306-308, 322-325 | `resolvedFirstBaseline`/`resolvedLastBaseline` |
| 3 | Wrap-reverse position flip | `baseline = BlockSize − baseline` when `is_wrap_reverse != is_last_baseline` | Missing | Container export block |
| 4 | Synthesized edge | Fragment block-size (≈ border-box) | `crossSize` (border-box) | Already matches — no change |
| 5 | Fieldset baseline | `FieldsetLayoutAlgorithm` exports content block's baseline, ignores legend | No fieldset-specific code — generic block layout may pick legend | Block layout baseline export OR fieldset shim |
| 6 | Last-baseline priority | `minor` before `major` (reversed from first) | Falls back first → last → synth | Accumulator `lastBaseline()` |

## Answers to pre-coding questions

1. **`crossSize` box type**: border-box, per flex_layout.go:160 comment. Matches Blink's `SynthesizedBaseline` fragment block-size. No adjustment needed.
2. **Fieldset layout file**: does not exist. Fieldsets flow through generic block layout. Fix must live either in the block layout's baseline export or in a new fieldset shim that runs ahead of block layout.
3. **`BlockSize()` equivalent**: `LogicalFragment.BlockSize()` at layout_result.go:181. Wrap a `*PhysicalFragment` via `NewLogicalFragment(wdm, fragment)`.

## Implementation phases

Each phase is one commit checkpoint. Push after each commit so progress is inspectable.

**Revision note**: After fetching Blink's actual `BaselineAccumulator` source, we combine the originally-planned Phases A/B/F into a single structural refactor (Phase A). They were distinct only because I hadn't yet seen Blink's data model; once the accumulator is structured correctly, physical-order iteration and last-baseline priority fall out naturally and cannot be cleanly deferred.

### Phase A — Introduce `baselineAccumulator` with Blink-parity structure

**Scope**: Everything data-model-shaped. Mirror `BaselineAccumulator` from `flex_layout_algorithm.cc:80-152`.

**Data model** (verbatim-shape port):
```go
type baselineAccumulator struct {
    // First-line candidates (filled by accumulateLine/Item when isFirst=true)
    firstMajor    optFloat // cross-offset + line.majorBaseline
    firstMinor    optFloat // cross-offset + (line.crossSize - line.minorBaseline)
    firstFallback optFloat // block-offset + fragment.FirstBaselineOrSynthesize

    // Last-line candidates (filled when isLast=true)
    lastMajor    optFloat
    lastMinor    optFloat
    lastFallback optFloat
}
```

**Per-line tracking** (added to `flexLine`):
```go
majorBaseline float64 // max ascent across align-self:baseline items; -inf if none
minorBaseline float64 // max ascent across align-self:last-baseline items; -inf if none
crossAxisOffset float64 // set at positioning time
```

**Priorities** (mirror Blink's FirstBaseline/LastBaseline at flex_layout_algorithm.cc:130-145):
- `firstBaseline()`: firstMajor → firstMinor → firstFallback
- `lastBaseline()`: lastMinor → lastMajor → lastFallback

**Iteration order (equivalent of `ApplyReversals` for lines)**: Louis14 keeps `lines` in source order and flips per-line offsets in `computeAlignContent` (flex_layout.go:3469-3473). To match Blink's physical-first semantics, iterate `lines` in reverse under `wrap-reverse` when feeding the accumulator. This is functionally equivalent to Blink's `ApplyReversals`. `line.crossAxisOffset = lineOffsets[lineIdx]` is already the physical offset, so `crossAxisOffset + majorBaseline` yields the correct cross position regardless of wrap direction.

**Deferred**: `ApplyReversals` also reverses *items within a line* under `flex-direction: row-reverse` / `column-reverse` (Blink's `is_reverse_direction_`). That affects which item the fallback synthesis picks. None of Target 1's 10 failing tests use row/column-reverse, so we defer this to a later ticket.

```go
order := iota(len(lines))
if reverseCross {
    order = reversed(order)
}
for idx, lineIdx := range order {
    isFirst := idx == 0
    isLast := idx == len(order)-1
    accumulator.accumulateLine(lines[lineIdx], isFirst, isLast)
    // also accumulateItem for fallback, see below
}
```

**Fallback via `accumulateItem`**: for the first item on the first physical line, and the last item on the last physical line, record `block_offset + fragment.FirstBaselineOrSynthesize()` / `LastBaselineOrSynthesize()`. These fill `firstFallback` / `lastFallback` when no aligned baseline exists.

**Major/minor ascent tracking**: compute during the existing per-item loop that establishes line cross-size. For each item with `align-self: baseline` or `last-baseline`, compute ascent the same way Blink does (`BaselineAscent` — see Phase B for the wrap-reverse flip, Phase C for orthogonal exclusion; for now use the item's natural first-baseline without flip, excluding only those with no usable baseline).

**Expected changes to test set**: Moderate. Multi-line tests should move immediately because we now feed all lines into the accumulator (today's code only inspects `firstBLLine` and `lastBLLine`). horiz-008 may still fail until Phase B's flip lands. Orthogonal tests may still fail until Phase C.

**Regression guard**: motivating-three must stay PASS. If they break, the accumulator's fallback path has a bug.

### Phase B — Wrap-reverse baseline position flip

**Change**: In the per-item ascent computation used by the accumulator, apply Blink's flip:
```
if reverseCross != isLastBaseline {
    baseline = LogicalFragment{wdm, fragment}.BlockSize() - baseline
}
```
This lives at the point we record major/minor ascent into the line (not at export time).

**Expected close**: flexbox-align-self-baseline-horiz-008, flexbox-baseline-multi-item-horiz-001a/b.

### Phase C — Exclude orthogonal items from baseline accumulation

**Change**: In `accumulateItem`/ascent computation, skip items where `item.wdm.IsOrthogonalTo(containerWdm)`. Remove the synthesis fallbacks at flex_layout.go:306-308 and 322-325. Audit all callers of `resolvedFirstBaseline`/`resolvedLastBaseline` first — preserve orthogonal synthesis for cross-axis alignment paths if those rely on it (they shouldn't, but verify).

**Expected close**: flexbox-align-self-baseline-horiz-006, horiz-002, residuals from 008.

### Phase D — Fieldset baseline export

**Investigation first** (before coding):
- Locate block layout baseline export (where `PhysicalFragment`'s baseline fields are set for blocks).
- Check how `<legend>` is threaded — is it a normal child? Anonymous-box wrapped?
- Compare against Blink's `fieldset_layout_algorithm.cc`.

**Minimum viable fix**: In block baseline export, if the node is `<fieldset>`, skip the legend when computing baseline. Prefer the content block's baseline.

**Expected close**: fieldset-baseline-alignment.

## Checkpoint protocol

For each phase:

1. Study the Blink source files cited above in that phase. Quote the key 3–10 lines in the commit message.
2. Implement the phase. Keep the change small — one structural concept per commit.
3. Run the Target 1 verification command above.
4. Run the motivating-three regression guard.
5. If both pass (or show progress without regressions), `git commit` + `git push`.
6. Move to the next phase.

Do **not** run the full flex suite mid-phase. Save that for a final checkpoint after Phase F.

## Open items / risks

- **Block baseline export for fieldset (Phase E)**: implementation shape depends on how louis14 currently structures fieldset layout. Investigate before coding.
- **Orthogonal exclusion side effects (Phase D)**: `resolvedFirstBaseline`/`resolvedLastBaseline` may be called from cross-axis alignment code too. Audit all callers before changing the orthogonal branch.
- **Commit 26027552 interaction (Phase B)**: the commit added the visual-first picker. Phase B replaces that logic but should not reintroduce the bug it originally fixed — Phase C's flip is the Blink-correct version of the same intent.

## File-touch map

| Phase | File |
|---|---|
| A, B, C | `pkg/layout/flex_layout.go` |
| D | block layout / fieldset-adjacent file (TBD during Phase D investigation) |

No changes to `replaced_layout.go`, `writing_mode.go`, or test infrastructure expected.
