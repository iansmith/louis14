# Plan B4: unicode-bidi: plaintext multi-paragraph (block-plaintext-006)

## Root Cause

`collectTextNode` in `pkg/layout/inline_item.go` (lines 271-284): for `white-space: pre`
(`collapseSpaces=false`), raw text including `\n` is written as ONE contiguous string
without emitting `InlineItemControl` items. The `preserveNewlines=true` flag is computed
but never consulted in this branch.

Consequence:
1. `<pre>` content for the 3 lines becomes one big `InlineItemText`.
2. `ResolveBidiLevelsPlaintext` correctly computes per-paragraph levels (LTR, RTL, LTR).
3. But the line breaker has no `kControl` items, so with `noWrap=true` (from `pre`)
   ALL text goes on ONE line.
4. `lineParagraphLevel` is taken from the FIRST item (LTR), so `ReorderLineVisual` is
   called with level=0 — RTL line 2 content is reordered incorrectly into the LTR line.

Blink reference: `inline_items_builder.cc::AppendText` always emits `InlineItem::kControl`
for `\n` when `preserveNewlines=true`, regardless of `collapseSpaces`.

## Changes

### `pkg/layout/inline_item.go` (primary)

In `collectTextNode`, replace lines 276-284 with a loop that splits `preservedContent`
at `\n` chars when `preserveNewlines=true`:

```
for each segment between \n in preservedContent:
    if segment non-empty: write to text builder + emit InlineItemText
    write \n to text builder + emit InlineItemControl
final segment after last \n: emit InlineItemText if non-empty
```

This mirrors Blink's `AppendText` behavior for preformatted whitespace.

### `pkg/html/parser.go` (secondary)

Lines 145-152 strip a leading `\n` from `<pre>` text nodes when
`len(parent.Children) == 0`. The WPT test uses an HTML comment to suppress this strip,
but the louis14 tokenizer discards comments without adding them to the DOM, so the
condition still passes and the leading `\n` is stripped (losing the first blank line).

Fix: track `commentSeenInPre` on the parser; gate the strip on
`!p.commentSeenInPre`. Reset when leaving `<pre>` context.

### No changes needed

- `pkg/layout/bidi.go::ResolveBidiLevelsPlaintext` — already splits paragraphs on
  class-B chars correctly.
- `pkg/layout/inline_layout.go` — `lineParagraphLevel` per-line detection works once
  control items terminate lines.
- `pkg/layout/line_breaker.go::handleControl` — already correctly breaks at `\n`.

## Tests Fixed

- `block-plaintext-006.html` (primary).

## Regression Risk

- `bidi-plaintext-*`, `block-plaintext-001..005` — single-paragraph or no `<pre>`,
  unaffected.
- `multicol-clip-scrolled-content-001` — already uses `pre`; the fix produces the
  spec-correct multiple lines (was previously coincidental).
- Risk: any test silently passing because broken single-line `pre` happened to fit
  in a clipped container. Verify with the named tests before committing.

## Deferred (separate fix)

`injectBlockBidiControls("plaintext")` injects FSI/PDI before content. Para 0's FSI
makes `determineFSIDirection` skip the entire para 0. Coincidentally fine for this
test (para 0 is LTR by default), but wrong for an RTL-leading first paragraph. Long
term: skip FSI/PDI injection for `plaintext` and let `ResolveBidiLevelsPlaintext`
own per-paragraph direction.
