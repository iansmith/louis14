# Bidi Support Continuation Prompt

Read `.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md` first for project principles.
Read `docs/WRITING-MODES-GAP-ANALYSIS.md` for the full categorized failure analysis (Category C).

## Current baseline: 522/788 WM passing, 32/82 bidi passing

## What was done (2026-04-04)

### Tier 1 fixes (complete, +117 tests):
1. **Paint tree fix** (`pkg/render/paint_layer.go`): `domOrderedChildren` was dropping OOF children that propagated up from descendants.
2. **vertical-align: top/bottom** (`pkg/layout/inline_layout.go`): Implemented for atomic inline elements.

### Bidi investigation (attempted, reverted):

The bidi system already has control character injection (`injectBidiControlChars` in `inline_item.go:328-406`) and UAX#9 resolution via `golang.org/x/text/unicode/bidi`. The 50 failing bidi tests are caused by **insufficient embedding level depth**.

#### Root cause
`golang.org/x/text/unicode/bidi` only exposes `Run.Direction()` (LTR/RTL), NOT the actual embedding level (0, 1, 2, ...). Our `ResolveBidiLevels` collapses everything to level 0 or 1. This means:
- LTR text inside an RTL embedding gets level 0 instead of level 2
- L2 reordering can't distinguish level-0 LTR from level-2 LTR
- Nested embeddings produce wrong visual order

#### What was tried and why it failed

**Approach 1: Compute embedding levels from control chars + split items**
I wrote `computeEmbeddingLevels()` that tracks the RLE/LRE/RLO/LRO/PDF/LRI/RLI/FSI/PDI control characters in the text to compute per-rune embedding levels. Combined with run directions from the bidi package: `resolved_level = embedding_level + (run_dir_parity != emb_parity ? 1 : 0)`. This is correct per UAX#9 rules I1/I2.

Then split text items at level boundaries so each item has a single bidi level.

**Problem**: bidi control characters (U+202A-E, U+2066-69) have non-zero width (1px) when measured as standalone text in our font engine (HarfBuzz via go-text). When items are split, a control character may end up in a sub-item measured alone, adding 1px. Additionally, the WPT reference files use raw LRO/PDF characters in their HTML text, triggering splitting in the REFERENCE rendering too.

**Approach 2: Strip control chars after bidi resolution**
Added `StripBidiControlsFromItems()` to remove control chars from `InlineItemsData.TextContent` and remap all item byte offsets. Called after `ResolveBidiLevels`.

**Problem**: Stripping changes byte offsets throughout the text content. The line breaker's whitespace trimming (`strings.TrimLeftFunc`) adjusts `textStart` by byte-length differences that are now wrong because the underlying text has been modified. This cascaded into 100+ test regressions across the entire WM suite — not just bidi tests.

## Blink's architecture (the reference implementation)

Blink's pipeline order:
```
1. CollectInlines → inject bidi control chars into TextContent
2. BidiParagraph (ICU wrapper) → compute per-character levels via ubidi_getLevelAt()
3. SegmentText (NGInlineItemSegmenter) → split items at level boundaries
4. Strip control chars (UBIDI_OPTION_REMOVE_CONTROLS or manual)
5. ShapeText (HarfBuzz)
6. LineBreaker
7. BidiReorder (UAX#9 L2)
```

Key difference: Blink uses **ICU's C API** which exposes `ubidi_getLevelAt(index)` for per-character levels. Go's `golang.org/x/text/unicode/bidi` doesn't expose this — see [golang/go#69819](https://github.com/golang/go/issues/69819).

Blink's `NGBidiParagraph` wraps ICU's `ubidi_open()/ubidi_setPara()/ubidi_getLevelAt()`. The full level array (0-125) enables correct L2 reordering and item segmentation.

## What needs to happen

### Option A: Use ICU via cgo
Import ICU's C bidi library via cgo to get `ubidi_getLevelAt()`. This gives exact Blink parity. Downside: cgo dependency.

### Option B: Compute levels in pure Go
The `computeEmbeddingLevels()` function I wrote (tracking the control char stack) already computes correct embedding levels. Combined with run directions from the Go bidi package, this gives accurate resolved levels. The issue was NOT the level computation — it was the downstream cascading from control char stripping.

### Option C: Implement a minimal UAX#9 level resolver in Go
Instead of relying on the Go bidi package + separate embedding level tracking, implement just the parts of UAX#9 needed: X1-X9 (explicit embeddings), W1-W7 (weak types), N1-N2 (neutral types), I1-I2 (implicit levels). The Go `bidi` package's `Properties` type gives per-character bidi class, which is all that's needed as input.

### The real fix (regardless of level source)

The level computation works. The problem is the **stripping/offset-remapping pipeline**. The fix must:

1. **Strip bidi control characters** from `TextContent` after level resolution
2. **Remap ALL byte offsets** in ALL items (StartOffset, EndOffset) to the new stripped text
3. **Handle the line breaker** correctly — the line breaker's whitespace trimming uses byte offsets that must be consistent with the stripped text. The key bug was in `handleText` where `textStart += len(content) - len(trimmed)` assumes `content` byte lengths match the original text, but after stripping they don't.

The specific code path that breaks:
```
line_breaker.go:191  content := lb.itemsData.TextContent[textStart:textEnd]
line_breaker.go:200  textStart += len(content) - len(trimmed)  // ← wrong if content was from stripped text
```

After stripping, `textStart` and `textEnd` are already in the stripped text's coordinate space (because `StripBidiControlsFromItems` remapped them). So `lb.itemsData.TextContent[textStart:textEnd]` gives the stripped content directly. The byte arithmetic should work correctly.

But the issue is more subtle: the items were split BEFORE stripping (in `ResolveBidiLevels`). The split items have offsets in the ORIGINAL (pre-strip) text. Then `StripBidiControlsFromItems` remaps those offsets. But the splitting created new items whose StartOffset/EndOffset may not be valid rune boundaries in the stripped text.

**The fix**: Move item splitting to AFTER stripping. The pipeline should be:
```
1. ResolveBidiLevels → compute per-rune levels, assign to items (NO splitting)
2. StripBidiControlsFromItems → strip control chars, remap offsets
3. SplitItemsAtLevelBoundaries → split text items using stripped text offsets
```

This way, the split items reference the clean (stripped) text, and all byte arithmetic in the line breaker works correctly.

## Key files

- `pkg/layout/bidi.go` — `ResolveBidiLevels`, `ReorderLineVisual` (current: levels 0/1 only)
- `pkg/layout/inline_item.go:328-406` — `injectBidiControlChars` (correct, don't change)
- `pkg/layout/inline_layout.go:70-73` — pipeline orchestration (Phase 1b)
- `pkg/layout/line_breaker.go:185-227` — `handleText` (sensitive to byte offset consistency)
- `pkg/layout/engine.go:286-288` — `reverseAndMirrorRunes` (rendering of odd-level text)

## Test commands

```bash
# Full bidi suite (current: 32/82)
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi' ./pkg/visualtest/ -timeout 120s

# Specific families
go test -v -run 'TestWPTCSS3Reftests/css-writing-modes/bidi-embed-001' ./pkg/visualtest/ -timeout 30s

# Full WM suite (current: 522/788) — MUST NOT REGRESS
go test -v -run TestWPTCSS3Reftests/css-writing-modes ./pkg/visualtest/ -timeout 600s 2>&1 | grep -c 'PASS: TestWPT'

# Quick regression check
go test ./pkg/layout/ ./pkg/css/ ./pkg/render/ -timeout 60s
```

## Debugging notes

- Control chars have 1px width when measured standalone by HarfBuzz, but 0px when part of a larger string
- The WPT reference files use raw LRO (U+202D) and PDF (U+202C) characters in their HTML text — these are NOT injected by our code but are literal text content
- `run.Pos()` from `golang.org/x/text/unicode/bidi` returns RUNE indices, not byte indices
- The `runeToByteOffset` helper function I wrote is correct and can be reused
