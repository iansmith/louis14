# Task Plan: Pass the entire css-ruby category

## Goal
All css-ruby reftests under `pkg/visualtest/testdata/wpt-css3/css-ruby/` pass at 0% diff
via `TestWPTCSS3Reftests/css-ruby`. Baseline **24 passing / 75 failing / 99 run**. ruby
layout is essentially unimplemented in louis14: `<ruby>/<rt>/<rb>` are tagged with ruby
display enums but treated as plain inline boxes, so annotation text flows inline *after*
the base instead of being stacked *above* it. Close all 75 failures without regressing
the CSS2 (99/99) or css-writing-modes suites.

## Blink vetting log

**Vetted against Chromium `main` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f** on 2026-05-18.

### Citations verified
- `core/html/resources/html.css:1701-1720` (ruby UA stylesheet block) — ✓ unchanged
- `core/layout/layout_object.cc:430-435` (`CreateObject` ruby cases) — ✓ unchanged
- `core/layout/layout_object.cc:522-529` (`IsInlineRuby`, `IsInlineRubyText`) — ✓ unchanged
- `core/layout/layout_object.cc:1225-1228` (ruby-text fragment-item container) — ✓ unchanged (plan said `:1225-1227`; actual span 1225-1228)
- `core/layout/layout_ruby_as_block.{h,cc}` (`LayoutRubyAsBlock` two-box model) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:247` (`EquivalentBlockDisplay` start) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:272-273` (`kRuby` → `kBlockRuby`) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:294-295` (`kRubyText` → `kBlock`) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:303` (`EquivalentInlineDisplay` start) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:322-323` (`kBlockRuby` → `kRuby`) — ✓ unchanged
- `core/css/resolver/style_adjuster.cc:788-805` (`AdjustStyleForDisplay` inlinify + float drop) — ✓ unchanged
- `core/layout/inline/inline_items_builder.cc:74` (`kDisableForcedBreakInRubyColumn = true`) — ✓ unchanged
- `core/layout/inline/inline_items_builder.cc:801,1068` (`is_text_combine_ || ruby_text_nesting_level_ > 0` forced-break→space) — ✓ unchanged (plan said `:800-801,:1069-1070`; off-by-≤1)
- `core/layout/inline/inline_items_builder.cc:1556-1559,1583-1586,1628,1682-1690` (per-column bidi isolates LRI/RLI/PDI) — ✓ unchanged
- `core/layout/inline/inline_items_builder.cc:1617-1628` (almost-empty trailing column removal at `</ruby>`) — ✓ unchanged (plan said `:1617-1626`; actual 1617-1628)
- `core/layout/inline/inline_items_builder.cc:1682-1697` (new column reopened after `</rt>`) — ✓ unchanged (plan said `:1685-1691`; covers the core)
- `core/layout/inline/ruby_utils.cc:147-176` (`ParseRubyInInlineItems`, recursion at 169-172) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:178` (`AnnotationOverhang`/`GetOverhang` first overload) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:334` (`CanApplyStartOverhang`) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:390` (`CommitPendingEndOverhang`) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:504-594` (`ApplyRubyAlign`, `#line-edge` doubling at 549-555) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:595` (`ComputeAnnotationOverflow` / `AnnotationMetrics`) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:720-748` (`UpdateRubyColumnInlinePositions`) — ✓ unchanged
- `core/layout/inline/ruby_utils.cc:775-1037` (`RubyBlockPositionCalculator` impls) — ✓ unchanged
- `core/layout/inline/ruby_utils.h:128-244` (`RubyBlockPositionCalculator` class + `RubyLevel`/`RubyLine`/`AnnotationDepth`) — ✓ unchanged (plan said `:130-244`; class starts at 128)
- `core/layout/inline/line_breaker.cc:1059-1060` (`kCloseRubyColumn`/`kRubyLinePlaceholder` zero-width skip) — ✓ unchanged
- `core/layout/inline/line_breaker.cc:1082-1093` (`kOpenRubyColumn` dispatch + placeholder-only skip) — ✓ unchanged
- `core/layout/inline/line_breaker.cc:1190` (bidi controls produced by `kOpen/CloseRubyColumn` are ignorable) — ✓ unchanged
- `core/layout/inline/line_breaker.cc:2561-2615` (trailing-collapsible-space recurses into `ancestor_ruby_columns`) — ✓ unchanged (plan said `:2561-2608`; the relevant block extends a few lines past)
- `core/layout/inline/line_breaker.cc:3278-3449` (`HandleRuby(line_info, retry_size)`) — ✓ unchanged (plan said `:3278-3445`; function ends at 3449)
- `core/layout/inline/line_breaker.cc:3372-3438` (proportional break + `RubyBreakTokenData`) — ✓ unchanged (plan said `:3370-3445`; close)
- `core/layout/inline/inline_layout_algorithm.cc:381-389` (force text metrics on ruby line) — ✓ unchanged (plan said `:381-384`; the check+`EnsureTextMetrics` span is 383-389)
- `core/layout/inline/inline_layout_algorithm.cc:396-418` (`HasRuby` → `UpdateRubyColumnInlinePositions` → `RubyBlockPositionCalculator.GroupLines.PlaceLines.AddLinesTo` → `SetAnnotationBlockStartAdjustment`) — ✓ unchanged
- `core/layout/inline/inline_node.cc:1543-1544, 2209-2210` (intrinsic walk: `kOpen/CloseRubyColumn` cases + `IsRubyColumn() → ComputeFromMinSizeInternal(result.ruby_column->base_line)`) — ✓ unchanged (plan said `:2103-2210`; the `IsRubyColumn`/`base_line` recursion is at 2209-2210)
- `core/css/css_properties.json5:3416` (`display` keyword list contains only `ruby, ruby-text` — no `ruby-base*`) — ✓ corroborates plan claim that `EDisplay` has no `kRubyBase`/`kRubyBaseContainer`/`kRubyTextContainer`
- `core/css/css_properties.json5:7610-7618` (`ruby-align` keywords `space-around/start/center/space-between`, initial `space-around`, inherited) — ✓ matches plan Phase 5

### Citations updated
- Plan said `core/style/computed_style_constants.h` defines `EDisplay` (and `ERubyAlign`/`ERubyOverhang`/`RubyPosition`) → actually `EDisplay` and the ruby property enums are auto-generated into `core/style/computed_style_base_constants.h`, which `computed_style_constants.h` re-exports via `#include`. Plan now cites the base constants header where it matters; `computed_style_constants.h` references like `RubyPosition` at lines 511-516 are still valid.
- Plan said `inline_items_builder.cc:1550-1595` for `EnterInline` → the function body actually starts at line 1510; lines 1550-1595 contain the ruby-specific code inside it. Plan now scopes the range to the ruby logic, not the whole function.
- Plan said `inline_items_builder.cc:1668-1700` for `ExitInline` → the function actually starts at line 1608; lines 1677-1697 contain the post-`</rt>` close/reopen ruby logic. Plan now scopes the range correctly.

### Citations broken / missing in current Blink
- **`ruby-overhang` keyword set**: plan Phase 5 (line ~410) said keywords are `auto`/`none` per spec. Actual Blink @ 4883d11 (`css_properties.json5:7621-7630`) uses `auto`/`spaces` and gates the property behind the `CSSRubyOverhang` runtime flag. Recommended plan action: rename keyword set to `auto`/`spaces` (or accept both and map `none` → `auto` for spec compat); decide whether louis14 ships ruby-overhang enabled or behind a similar flag. This affects the `GetOverhang` port — `IsSpaceForRubyOverhang` exists in Blink and is consulted only when `RubyOverhang() == kSpaces`.
- **`ruby-position` keyword set**: plan Phase 11 (line ~563) said keywords are `over`/`under`/`alternate`/`inter-character` with initial `alternate per spec but over for horizontal`. Actual Blink @ 4883d11 (`css_properties.json5:7642-7654`) implements only `over`/`under` with initial `over`. The spec values `alternate` and `inter-character` are NOT implemented. Recommended plan action: ship `over`/`under` only with initial `over`; treat unknown spec keywords as `over`. Note Blink also has a `-webkit-ruby-position` surrogate at 7632-7640.
- **`rp` UA hiding** (plan-narrative, not a specific citation): plan repeatedly states that `rp` is hidden via `ruby > rp` / `rt > rp` rules ("only when inside a ruby"). Actual: `rp` is hidden by a flat element-only rule at `html.css:972-975` — `base, basefont, datalist, head, link, meta, noembed, noframes, param, rp, script, style, template, title { display: none; }`. `rp` is always `display:none` per UA, regardless of parent. Plan now reflects this.

### Citations added
- Phase 1 UA-rules block now cites `html.css:972-975` for the `rp` global hide rule alongside `html.css:1701-1720` for the ruby block.
- Phase 5 ruby-overhang now cites `css_properties.json5:7621-7630` for the actual keyword set + runtime flag.
- Phase 11 ruby-position now cites `css_properties.json5:7642-7654` for the actual implemented keyword set.

### Cross-check note for css-text-decor agent
- **`LayoutRubyColumn` does NOT exist** in Blink @ 4883d11 — no file `layout_ruby_column.{h,cc}` and no such class. The closest names in current Blink are:
  - `LogicalRubyColumn` (a struct, not a `LayoutObject`),
  - `InlineItemResult::RubyColumn` (declared in `core/layout/inline/inline_item_result_ruby_column.h`).
- **`LayoutRubyRun` does NOT exist** either — that is the pre-2023 legacy name and has been removed. The plan-css-ruby document correctly identifies `LayoutRubyRun` as legacy (lines 42, 706) and does not cite `LayoutRubyColumn`. Any sibling plan that does cite either type as if it were a current Blink class needs to be corrected.

## Rules & Discipline (DO NOT DUPLICATE HERE)
Authoritative sources — re-read both before planning or coding:
1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first,
   0% diff required, test-execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — the
   auto-memory index pointing to the same rules.

If you are about to type a rule verbatim into this file or into code comments, stop and
link instead.

## Baseline snapshot (Phase 0 — already done, 2026-05-14)
- 75 failures / 99 run. ruby is the weakest large region in the suite.
- Root cause is singular and structural: **louis14 has no ruby layout algorithm.**
  - `pkg/css/cascade.go:162-183` sets `display: ruby / ruby-text / ruby-base` on
    `ruby/rt/rb` and `display:none` on `rp`, and `rt` gets `font-size: 0.5em`.
  - `pkg/css/style.go:4453-4532` defines `DisplayRuby / DisplayRubyText / DisplayRubyBase`
    enums and parses the keywords.
  - `pkg/layout/inline_layout.go:61-74` (`isInlineLevelDisplay`) treats all three ruby
    displays as inline-level — so they collect as ordinary `InlineItemOpenTag`/text/
    `InlineItemCloseTag` items and lay out as a normal inline run.
  - There is **no** ruby box-fixup in `layout_tree_builder.go`, **no** ruby item types in
    `inline_item.go`, **no** ruby handling in `line_breaker.go`, **no** annotation
    placement in `inline_layout.go`, and **no** `ruby-position/ruby-align/ruby-overhang`
    CSS support anywhere in `pkg/css/`.
- Because the gap is one missing subsystem, the phase order below is strictly
  foundational-first: build the inline ruby-column data model and base/annotation
  stacking once, correctly, and the bulk of failures fall out together. Property/edge
  phases follow.

## Blink reference model (study this BEFORE writing code)
Modern Blink (post-2023 "line-breakable ruby", `RubyLineBreakable`) implements ruby as a
**purely inline, line-based** subsystem — *not* the old table-style `LayoutRubyRun`. There
is no separate layout object for `<rb>/<rbc>/<rtc>`; the base/annotation structure is
**reconstructed at inline-item-collection time** as bracketed column items, and the
annotation is laid out as its own sub-line positioned above/below the base sub-line.

Key files (all under `third_party/blink/renderer/core/layout/`, fetched from the Chromium
main mirror on 2026-05-14):

- **UA stylesheet** `core/html/resources/html.css:1701-1720` — the *ruby-specific* UA
  block: `ruby { display: ruby }` and `ruby > rt { display: ruby-text; font-size: 50%;
  text-align: start }`. **`rb`, `rbc`, `rtc` get NO special UA display** — they are plain
  inline boxes. `rp` is hidden by the unrelated *element-only* rule at
  `core/html/resources/html.css:972-975` —
  `base, basefont, datalist, head, link, meta, noembed, noframes, param, rp, script,
  style, template, title { display: none; }`. So `rp` is **always** `display:none` per
  UA, regardless of parent (not via a `ruby > rp` / `rt > rp` selector). The single
  most important correction is still that louis14's `cascade.go` invents a `ruby-base`
  display and `rb`/`rbc`/`rtc` displays that modern Blink does not have.
- **`computed_style_base_constants.h`** (auto-generated; re-exported via
  `computed_style_constants.h`) — `EDisplay` has only `kRuby`, `kBlockRuby`, `kRubyText`.
  There is **no `kRubyBase` / `kRubyBaseContainer` / `kRubyTextContainer`**. rb/rbc/rtc
  resolve to `kInline` (or `kBlock` → inlinified, see below). Confirmed via
  `core/css/css_properties.json5:3416` where the `display` keyword list contains only
  `"ruby", "ruby-text"` among ruby values.
- **`layout_object.cc:430-435`** `LayoutObject::CreateObject` — `kRuby` →
  `LayoutInline`, `kRubyText` → `LayoutInline`, `kBlockRuby` → `LayoutRubyAsBlock`.
  `:522-529` — `IsInlineRuby()` = `IsLayoutInline() && Display()==kRuby`;
  `IsInlineRubyText()` = `IsLayoutInline() && Display()==kRubyText`.
- **`layout_ruby_as_block.{h,cc}`** — block-level `<ruby>` (`display: block ruby`)
  generates a `LayoutBlockFlow` principal box wrapping a single anonymous `LayoutInline`
  with `display: ruby`; all children are added into that inline ruby. This is the
  CSS-Ruby §"block ruby" two-box model.
- **`style_adjuster.cc`**
  - `EquivalentBlockDisplay` `:272-273` — `kRuby` → `kBlockRuby` when blockified;
    `:294` `kRubyText` → `kBlock`.
  - `EquivalentInlineDisplay` `:322-323` — `kBlockRuby` → `kRuby`.
  - `AdjustStyleForDisplay` `:788-805` — **inlinification**: when the layout parent
    `InlinifiesChildren()` (i.e. it's a `display:ruby` or `display:ruby-text` box), every
    non-OOF child has `SetDisplay(EquivalentInlineDisplay(...))` applied and floats are
    dropped to `float:none` (with a console message). This is the
    `#anon-gen-inlinize` rule — block-level boxes inside ruby become inline-level.
- **`inline/inline_items_builder.cc`** — ruby box-fixup at item-collection time:
  - `EnterInline` body starts at `:1510`; the ruby-specific code spans `:1550-1595`.
    `ExitInline` body starts at `:1608`; the post-`</rt>` close/reopen ruby logic spans
    `:1677-1697`. On `IsInlineRuby()`: emit `kOpenRubyColumn` (carrying an isolate bidi
    control) + `kRubyLinePlaceholder`. On `IsInlineRubyText()`: emit
    `kRubyLinePlaceholder` markers around the `<rt>` `kOpenTag`/`kCloseTag`. On
    `</ruby>` or `</rt>`: emit `kCloseRubyColumn`.
  - Critically, **a new `kOpenRubyColumn ... kCloseRubyColumn` pair is started after
    every `<rt>`'s `kCloseRubyColumn`** (`:1682-1697`) — that is how successive
    base/annotation pairs ("ruby columns") are produced from a flat
    `<rb><rb><rt><rt>` child list: each `<rt>` closes the current column and opens the
    next. Almost-empty columns produced this way are removed at `</ruby>`
    (`:1617-1628`). `kDisableForcedBreakInRubyColumn` (`:74`) replaces forced breaks
    inside a column with spaces for now (the actual rewrites are at `:801` for `<br>` in
    collapsed space and `:1068` for `\n` line feeds).
- **`inline/ruby_utils.{h,cc}`** — the heart of ruby layout. Types & functions:
  - `RubyItemIndexes` + `ParseRubyInInlineItems(items, start)` (`ruby_utils.cc:147-176`)
    — given a `kOpenRubyColumn` index, returns `{column_start, base_end,
    annotation_start, column_end}`; recurses into nested `kOpenRubyColumn`.
  - `AnnotationOverhang {start,end}` + `GetOverhang(...)` (`:178-323`) — computes how far
    a wide annotation may overhang adjacent content. Honors `ruby-overhang`,
    `ruby-align`, and (for `ruby-overhang: spaces`) collapsible spaces in the previous
    item. `CanApplyStartOverhang` / `CommitPendingEndOverhang` (`:334-502`) clamp the
    overhang against the previous/next item widths.
  - `ApplyRubyAlign(available_line_size, on_start_edge, on_end_edge, line_info)`
    (`:504-592`) — returns `{start_inset, end_inset}` to justify a base or annotation
    sub-line. `ruby-align: start` → all slack at end; `center` → split; `space-between`
    → justify both edges; `space-around` → justify respecting `text-align`. Edge cases
    `#line-edge`: at a line edge the inset is doubled and pushed inward.
  - `AnnotationMetrics` + `ComputeAnnotationOverflow(...)` (`:595-718`) — over/under
    overflow vs. space the *next* line may consume.
  - `UpdateRubyColumnInlinePositions(...)` (`:720-748`) — slides each annotation
    sub-line to its base column's inline offset.
  - `RubyBlockPositionCalculator` (`ruby_utils.h:130-244`, `ruby_utils.cc:775-1037`) —
    the block-axis stacker. `RubyLevel` is a `Vector<int32_t>` path: `[]` = base,
    `[1]` = first annotation over, `[-1]` = first under, `[1,-1]` = under-an-over, etc.
    `GroupLines()` buckets columns by level; `PlaceLines(base_line_items,
    line_box_metrics)` assigns each annotation `RubyLine` a block offset stacked away
    from the base baseline; `AddLinesTo()` appends them to the line container;
    `AnnotationMetrics()` returns the total `FontHeight` (ascent above base baseline,
    descent below). `ruby-position` selects the over/under sign per level.
- **`inline/line_breaker.cc`**
  - `HandleRuby(line_info, retry_size)` (`:3278-3445`) — on a `kOpenRubyColumn`:
    `ParseRubyInInlineItems`, build a **base sub-`LineInfo`** (`SetIsRubyBase()`,
    `OverrideLineStyle`) and one annotation sub-`LineInfo` per `<rt>`
    (`SetIsRubyText()`); `ruby_size = MaxLineWidth(base, annotations)`; compute overhang;
    if it fits, append one `InlineItemResult` of kind `RubyColumn` carrying both
    sub-lines and advance `position_` by `ruby_size`. If it does not fit, try to break
    the base + annotations proportionally and emit a `RubyBreakTokenData`.
  - Dispatch at `:1082-1093` — `kOpenRubyColumn` calls `HandleRuby`; placeholder-only
    columns (`<ruby>` with no `<rt>`) are skipped. `:1059-1060` — `kCloseRubyColumn` /
    `kRubyLinePlaceholder` are zero-width skip items.
  - `:2561-2608` — trailing-collapsible-space removal must reach *inside*
    `ancestor_ruby_columns` so intra-base whitespace collapses correctly.
- **`inline/inline_layout_algorithm.cc:396-418`** — after line breaking, for a node with
  ruby: `UpdateRubyColumnInlinePositions`, then `RubyBlockPositionCalculator
  .GroupLines().PlaceLines().AddLinesTo()`, then `SetAnnotationBlockStartAdjustment` so
  the line box grows to contain the annotations and adjacent lines don't overlap.
  `:381-384` — a line that contains ruby columns is forced to have text metrics even if
  "empty", so an annotation-only column still has height.
- **`inline/inline_node.cc:2103-2210`** — intrinsic (`min-content`/`max-content`) sizing
  walks `kOpenRubyColumn` and uses `result.ruby_column->base_line` for the contribution:
  ruby's intrinsic inline size is `max(base width, annotation width)` per column.

CSS spec: <https://drafts.csswg.org/css-ruby-1/> — §2 box model, §2.1 box-fixup, §2.2
anonymous box generation, §2.3 white-space, §3 base/annotation pairing & autohiding,
§4 line layout / overhang / `ruby-align`, §4.x bidi.

## Failure buckets (75 total)

| # | Bucket | Count | Representative tests |
|---|--------|-------|----------------------|
| B1 | **Core base/annotation stacking** — annotation is not placed above the base; ruby renders as inline text run | ~14 | ruby-inline-001, ruby-rt-fontsize-001, ruby-span-001, rb-display-001, rt-display-001, rbc-rtc-basic-001, ruby-box-model-001, empty-ruby-base-container, empty-ruby-text-container-{abs,float}, ruby-rp-hidden-001, ruby-no-transform, ruby-annotation-with-margin, pseudo-first-{letter,line} |
| B2 | **Box-fixup & base/annotation pairing** — flat `<rb><rb><rt><rt>`, `<rbc>/<rtc>`, `<span class=rb>`, anonymous bases, ruby-span | ~11 | ruby-box-generation-001..005, ruby-annotation-pairing-001, ruby-span-001, rb-display-001, rt-display-001, rbc-rtc-basic-001, ruby-intra-level-whitespace-003 |
| B3 | **Intra-base / intra-level white space** (§2.3) | ~4 | intra-base-white-space-001, ruby-whitespace-002, ruby-intra-level-whitespace-003, ruby-line-break-suppression-002 |
| B4 | **Autohiding** annotations identical to base (§3.2) | ~1 | ruby-autohide-002 |
| B5 | **`ruby-align`** justification of base vs. annotation | ~5 | ruby-align-001, ruby-align-001a, ruby-align-002, ruby-align-002a, ruby-align-space-around |
| B6 | **`ruby-overhang`** | ~2 | ruby-overhang, ruby-overhang-dynamic |
| B7 | **Line breaking & break suppression** inside/around ruby columns | ~7 | ruby-line-breaking-001..003, ruby-line-break-suppression-001/002/003/005 |
| B8 | **Intrinsic sizing** of ruby (`min-content`/`max-content`) | ~3 | ruby-intrinsic-isize-001..003 |
| B9 | **Block ruby** (`display: block ruby`) two-box model | ~6 | block-ruby-001..005, root-block-ruby.xhtml |
| B10 | **Inlinification** of block-level boxes / floats inside ruby (§2.2) | ~7 | ruby-inlinize-blocks-001..005, ruby-inlinize-recursive-simple, root-ruby.xhtml |
| B11 | **Nested ruby** pairing & block-axis stacking | ~1 | nested-ruby-pairing-001 |
| B12 | **Bidi** reordering of ruby columns | ~3 | ruby-bidi-002, ruby-bidi-003, ruby-bidi-004 |
| B13 | **Floats & abs-pos** in/around ruby; empty containers | ~8 | ruby-with-floats-001..003, ruby-float-handling-001, abs-in-ruby-base, abs-in-ruby-base-container, abs-in-ruby-container, ruby-base-container-{abs,float} |
| B14 | **Dynamic** insertion/removal of ruby boxes | ~2 | ruby-dynamic-insertion-005, ruby-dynamic-removal-003 |
| B15 | **`text-combine-upright`** base inside vertical ruby | ~4 | ruby-text-combine-upright-001a/001b/002a/002b |
| B16 | **Misc singletons** — lang-specific style, rt font-size, dynamic style | ~3 | ruby-lang-specific-style-001, ruby-rt-fontsize-001, ruby-no-transform |

(Counts are approximate where a test exercises more than one bucket; each test is gated in
exactly one phase below — the phase that owns its dominant root cause.)

### Diff evidence read (from `output/reftests/*_{test,ref,diff}.png`)
- **ruby-box-generation-001** — `_test.png` shows every base laid out on its *own line*
  stacked vertically (`a` / `b` / `c` …), `_ref.png` shows them inline left-to-right with
  small annotations above. louis14 is breaking after each ruby child instead of treating
  the ruby as one inline atom.
- **ruby-span-001 / empty-ruby-base-container** — `_test.png` puts the annotation text on
  the line *below* the base ("The Ruby Base" / "span" on two lines); `_ref.png` has the
  small "span" annotation *above* the base on one line. Confirms: no block-axis stacking.
- **ruby-align-001** — `_test.png` is blank (the `rt > div { width:160px }` annotation +
  `ruby { line-height:0 }` produced nothing); `_ref.png` shows three rows of Ahem blocks
  at start/center/justified positions. Confirms: annotation sub-line + `ruby-align` both
  missing.
- **block-ruby-001** — `_test.png` lays the `display:block ruby` content inline (no block
  principal box), columns wrong, last `<rbc>` column collapses to a vertical stack;
  `_ref.png` shows each block-ruby on its own block line.
- **ruby-with-floats-001 / abs-in-ruby-base** — `_test.png` is very close to `_ref.png`
  (only a sub-pixel annotation offset). These are near-passing *because* the test happens
  to have a trivial base; they will be swept by B1 + B13 once columns place correctly.

## Phases (foundational-first)

### Phase 0: Baseline & categorization — **DONE**
- [x] Full css-ruby fail list captured (`docs/reftest-survey-2026-05-14-raw.txt`).
- [x] Representative HTML/ref/diff sample read; buckets above derived.
- [x] Blink ruby model studied (files cited above).

---

### Phase 1: Correct ruby UA styles, display model, and box-fixup — **FOUNDATIONAL**
Fixes the modeling errors that block everything else. No layout yet — just the box tree
and display values must match Blink.

**Goal.** `ruby/rt/rb/rbc/rtc/rp` carry the correct display values; `<ruby>` and `<rt>`
are inline boxes; `rb/rbc/rtc` are plain inline boxes; `rp` is `display:none` inside a
supported ruby; `block ruby` produces the two-box principal/inline structure.

**Blink reference.**
- `core/html/resources/html.css:1701-1720` — exact UA rules for ruby itself. **Only**
  `ruby`→`ruby`, `ruby > rt`→`ruby-text` (+`font-size:50%`,`text-align:start`). No
  display on `rb/rbc/rtc`. `rp` is hidden by the *separate* global rule at
  `html.css:972-975` (`base, basefont, datalist, head, link, meta, noembed, noframes,
  param, rp, script, style, template, title { display: none; }`) — not via a
  parent-scoped `ruby > rp` selector.
- `style_adjuster.cc` `EquivalentBlockDisplay/EquivalentInlineDisplay` `:247-356` —
  `ruby`↔`block ruby`, `ruby-text`→`block` under blockify.
- `layout_object.cc:430-435` — object creation; `layout_ruby_as_block.cc` — block-ruby
  two-box model.

**louis14 targets.**
- `pkg/css/style.go:4453-4532` — keep `DisplayRuby`, `DisplayRubyText`; **add
  `DisplayBlockRuby`** (`"block ruby"`); **delete `DisplayRubyBase`** and any
  `ruby-base`/`ruby-base-container`/`ruby-text-container` parsing — they are not real
  display values in modern Blink. Update `ParseDisplay` to accept the
  two-keyword `display: block ruby` / `inline ruby` forms (CSS Display L3 two-value
  syntax) and `ruby` as inner display.
- `pkg/css/cascade.go:162-183` — rewrite the ruby UA block to mirror `html.css`
  exactly: `ruby`→`display:ruby`; `rt`→`display:ruby-text` **only when its parent is a
  ruby box** (otherwise no UA display) + `font-size:50%` + `text-align:start`;
  `rp`→`display:none` *unconditionally* (mirrors Blink's flat element-only rule at
  `html.css:972-975`, not a parent-scoped rule); **`rb/rbc/rtc` get NO display
  override**. Move the `font-size: 0.5em` → `50%` (Blink uses `50%`;
  `ruby-rt-fontsize-001` expects exactly half).
- `pkg/css/style.go` display-classification helpers — `IsInlineLevelDisplay`,
  `isBlockContainer`, `isBlockLevel` must treat `DisplayRuby`/`DisplayRubyText` as
  inline-level and `DisplayBlockRuby` as block-level.
- `pkg/layout/layout_tree_builder.go` — new `normalizeRubySubtrees(node)` invoked from
  `BuildLayoutTree`/`buildNode` analogous to `normalizeTableSubtrees` (`:44-63`):
  1. For `display: block ruby` elements, generate the two-box structure
     (`LayoutRubyAsBlock` model): a block-flow principal box whose single anonymous
     child is an inline `display:ruby` box holding the original children. Mirror
     `wrapAnonymousTableBoxes` style with a new `css.NewAnonymousInlineRubyStyle`.
  2. **Inlinification** (§2.2, `#anon-gen-inlinize`): when a node's layout parent is a
     `display:ruby` or `display:ruby-text` box, set each in-flow child's *used* display
     to its `EquivalentInlineDisplay` (block→inline-block, table→inline-table, …) and
     force `float:none`. Recurse — the rule is recursive per csswg-drafts #1341.
- `pkg/layout/inline_layout.go:61-74` — `isInlineLevelDisplay` already lists the ruby
  displays; drop `DisplayRubyBase`, keep `DisplayRuby`/`DisplayRubyText`.

**New types.** `css.DisplayBlockRuby`; `css.NewAnonymousInlineRubyStyle(parent)`;
`LayoutTreeBuilder.normalizeRubySubtrees`, `.inlinifyRubyChildren`.

**Tests this should fix on its own.** None fully (no layout yet) — but it unblocks all
75 and is verified by not regressing CSS2/wm.

**Gate.** CSS2 99/99 and css-writing-modes unchanged. `display` computed values for
`ruby/rt/rb/rbc/rtc/rp` match Blink in a hand-checked DOM. No css-ruby regressions below
24 passing.

---

### Phase 2: Ruby-column inline-item model + base/annotation stacking — **FOUNDATIONAL, biggest unblock (B1, B2, B11 core)**
Builds the inline ruby-column data model end-to-end: collection → line breaking →
block-axis stacking. This is the single largest payoff phase.

**Goal.** A `<ruby>base<rt>anno</rt></ruby>` lays out as one inline atom: the base on the
baseline, the (50%-size) annotation centered above it, the line box tall enough to hold
both. Flat `<rb><rb><rt><rt>` produces N successive base/annotation columns.

**Blink reference.**
- `inline_items_builder.cc:1510-1697` — `EnterInline` (body starts 1510, ruby logic at
  1550-1595) emits `kOpenRubyColumn` / `kRubyLinePlaceholder`; `ExitInline` (body
  starts 1608, ruby logic at 1612-1697) starts a fresh column after each `<rt>`'s close
  (`:1682-1697`) and removes almost-empty trailing columns at `</ruby>` (`:1617-1628`).
- `ruby_utils.cc:147-176` `ParseRubyInInlineItems` — column parsing → `RubyItemIndexes`.
- `line_breaker.cc:3278-3445` `HandleRuby` — build base + annotation sub-`LineInfo`s,
  `ruby_size = MaxLineWidth(...)`, emit one `RubyColumn` `InlineItemResult`.
- `line_breaker.cc:1082-1093` — dispatch; skip placeholder-only columns.
- `ruby_utils.{h,cc}` `RubyBlockPositionCalculator` — `RubyLevel`, `GroupLines`,
  `PlaceLines`, `AddLinesTo`, `AnnotationMetrics`. For Phase 2 implement the
  single-level (`[]` base, `[1]` over / `[-1]` under) case; multi-level is Phase 11.
- `ruby_utils.cc:720-748` `UpdateRubyColumnInlinePositions`.
- `inline_layout_algorithm.cc:381-418` — force text metrics on ruby lines;
  `UpdateRubyColumnInlinePositions` → `RubyBlockPositionCalculator` →
  `SetAnnotationBlockStartAdjustment`.

**louis14 targets.**
- `pkg/layout/inline_item.go` — add `InlineItemType` values `InlineItemOpenRubyColumn`,
  `InlineItemCloseRubyColumn`, `InlineItemRubyLinePlaceholder`. In
  `collectInlinesRecursive` (`:108-262`), when entering a `DisplayRuby` element emit
  `OpenRubyColumn` + `RubyLinePlaceholder`; when entering `DisplayRubyText` emit the
  placeholder pair; on exit of a `<rt>` inside a ruby emit `CloseRubyColumn` then
  `OpenRubyColumn`+`RubyLinePlaceholder` for the next column; on `</ruby>` emit
  `CloseRubyColumn` and strip the trailing almost-empty column. `rb/rbc/rtc` collect as
  ordinary `OpenTag/CloseTag` — they are transparent to pairing.
- **New file `pkg/layout/ruby_utils.go`** (mirrors Blink `core/layout/inline/ruby_utils.cc`
  per the file-placement rule) — port:
  - `RubyItemIndexes` struct + `ParseRubyInInlineItems(items []*InlineItem, start int)
    RubyItemIndexes` (recursive).
  - `RubyColumn` struct: `BaseLine *LineInfo`, `AnnotationLines []*LineInfo`,
    `StartIndex int`, `InlineSize float64`, `RubyPosition` (over/under per level).
  - `RubyBlockPositionCalculator` with `GroupLines`, `PlaceLines`, `AddLinesTo`,
    `AnnotationMetrics` — single-level first.
  - `UpdateRubyColumnInlinePositions(...)`.
- `pkg/layout/line_breaker.go` — in `NextLine` dispatch (`:160-224`) add a
  `case InlineItemOpenRubyColumn:` → new `func (lb *LineBreaker) handleRuby(item, line)
  bool`. `handleRuby` creates sub-`LineBreaker`s (a base one over the base item range,
  one per `<rt>` over the annotation range), takes `rubySize = max(base.Width,
  annoWidths...)`, appends a single `InlineItemResult` whose new field
  `RubyColumn *RubyColumn` carries the sub-lines, and advances `position` by `rubySize`.
  `CloseRubyColumn`/`RubyLinePlaceholder` are zero-width skips. Add a sub-line mode to
  `LineBreaker` (Blink's `CreateSubLineInfo` / `kMaxContent`).
- `pkg/layout/line_breaker.go` — extend `InlineItemResult` (`:49-69`) with
  `RubyColumn *RubyColumn`; extend `LineInfo` (`:73-88`) with `IsRubyBase bool`,
  `IsRubyText bool`, `RubyColumns []*RubyColumn` (the columns on this line, for
  inline-positioning).
- `pkg/layout/inline_layout.go` — in `createLineBoxEx` (`:1218-1715`): (a) iterate
  `RubyColumn` results, lay the base sub-line on the main baseline at the column's inline
  offset; (b) call `UpdateRubyColumnInlinePositions` then a `RubyBlockPositionCalculator`
  to assign each annotation sub-line a block offset above (over) / below (under) the base
  baseline and emit its glyphs as child fragments of the line box; (c) in
  `computeLineMetricsEx` (`:1766+`) make a `RubyColumn` result contribute
  `ascent = base.ascent + annotationMetrics.ascent` and likewise for descent, so the
  line box grows to contain annotations (Blink `SetAnnotationBlockStartAdjustment`).
  Force ruby lines to have text metrics even when "empty"
  (`inline_layout_algorithm.cc:381`).
- `pkg/render/` — ensure line-box child fragments produced for annotation sub-lines are
  painted (they should already be, as ordinary text fragments at an offset).

**New types.** `InlineItemOpenRubyColumn/CloseRubyColumn/RubyLinePlaceholder`;
`ruby_utils.go`: `RubyItemIndexes`, `RubyColumn`, `RubyBlockPositionCalculator`,
`ParseRubyInInlineItems`, `UpdateRubyColumnInlinePositions`;
`LineBreaker.handleRuby`, sub-line breaking support; `InlineItemResult.RubyColumn`;
`LineInfo.IsRubyBase/IsRubyText/RubyColumns`.

**Tests this should fix (~17): full B1** (ruby-inline-001, ruby-rt-fontsize-001,
ruby-span-001, ruby-box-model-001, empty-ruby-base-container, ruby-rp-hidden-001,
ruby-no-transform, ruby-annotation-with-margin, pseudo-first-letter, pseudo-first-line)
**+ the simple half of B2** (rb-display-001, rt-display-001, rbc-rtc-basic-001,
ruby-box-generation-001..005 partial). Also flips the near-passing B13 floats tests
(ruby-with-floats-001..003, ruby-float-handling-001) because the base column now
positions correctly.

**Gate.** ruby-inline-001, ruby-span-001, ruby-rt-fontsize-001, empty-ruby-base-container
at 0% diff. CSS2 99/99, wm unchanged. css-ruby ≥ 41 passing.

---

### Phase 3: Box-fixup completeness — pairing, `rbc/rtc`, anonymous bases, ruby-span (B2 remainder)
**Goal.** Every box-generation permutation pairs correctly: explicit `<rbc>`/`<rtc>`
containers, `<span style="display:ruby-base">`-style content (now just inline content),
single annotation paired with the first base of its segment, `ruby-span` (one `<rtc>`
spanning multiple bases), and pseudo-element `::before/::after` ruby content.

**Blink reference.**
- CSS Ruby §2.1 box-fixup, §3 base/annotation pairing.
- `inline_items_builder.cc:1668-1691` — `</rt>` followed by more bases keeps the same
  annotation associated until the next `<rt>`; `<rtc>` with one `<rt>` spans all bases of
  the segment (ruby-span).
- `ParseRubyInInlineItems` recursion handles `<rbc>`/`<rtc>` as transparent — they only
  group, the column items do the pairing.

**louis14 targets.**
- `pkg/layout/inline_item.go` `collectInlinesRecursive` — make `rbc`/`rtc` transparent
  (no column items of their own; they just contain `<rb>`/`<rt>` runs). Implement the
  "single `<rt>` / `<rtc>` spans the rest of the base segment" rule: if a base segment
  has more bases than annotations, the last annotation's column extends to cover them
  (Blink does this by *not* opening a new column until the next `<rt>`).
- `pkg/layout/layout_tree_builder.go` — anonymous base generation: consecutive
  inter-element content / bare inline content between ruby internals that is not in an
  `<rb>` is wrapped as an anonymous base for pairing purposes (CSS Ruby §2.1). For
  louis14's flat item model this is mostly handled by column items; only the pseudo
  `::before/::after` ruby content (`ruby-intra-level-whitespace-003`) needs care.
- `pkg/layout/layout_tree_builder.go:509-650` `createPseudoElement` — `::before/::after`
  with `display: ruby-base/ruby-text` content participates in pairing.

**Tests this should fix (~6):** ruby-annotation-pairing-001, ruby-span-001 (if not
already), ruby-box-generation-001..005 (full pass), rb-display-001, rt-display-001,
rbc-rtc-basic-001 (full pass), ruby-intra-level-whitespace-003.

**Gate.** ruby-box-generation-001..005, ruby-annotation-pairing-001 at 0% diff.
css-ruby ≥ 48 passing.

---

### Phase 4: Intra-base & intra-level white space (§2.3) — B3
**Goal.** A single collapsible white space between two adjacent ruby bases is kept as a
unit-box white space *unless* it begins/ends a base container; all-white-space
`::before/::after` content is not treated as intra-level white space.

**Blink reference.** CSS Ruby §2.3; `line_breaker.cc:2561-2608` — trailing collapsible
space removal recurses into `ancestor_ruby_columns`; `inline_items_builder.cc` collapses
intra-base whitespace at collection.

**louis14 targets.**
- `pkg/layout/inline_item.go` `collectInlinesRecursive` + the existing CSS whitespace
  collapsing — between two `<rb>` siblings keep one collapsible space; drop it when it
  leads/trails the rbc. All-whitespace pseudo content is dropped.
- `pkg/layout/line_breaker.go` `finishLine`/trailing-space logic — when the last item is
  inside a ruby column, collapse into the column's base sub-line.

**Tests this should fix (~4):** intra-base-white-space-001, ruby-whitespace-002,
ruby-intra-level-whitespace-003 (if not already), ruby-line-break-suppression-002.

**Gate.** intra-base-white-space-001 at 0% diff. css-ruby ≥ 51 passing.

---

### Phase 5: `ruby-align` + `ruby-overhang` — B5, B6
**Goal.** Base and annotation sub-lines justify per `ruby-align`
(`start`/`center`/`space-between`/`space-around`); a wide annotation overhangs adjacent
content per `ruby-overhang` (`auto`/`none`).

**Blink reference.**
- `ruby_utils.cc:504-594` `ApplyRubyAlign` — full algorithm incl. `#line-edge` doubling
  at `:549-555`.
- `ruby_utils.cc:178-323` `GetOverhang`, `:334-502` `CanApplyStartOverhang` /
  `:390-502` `CommitPendingEndOverhang`. `IsSpaceForRubyOverhang` helper at `:26`.
- `core/style/computed_style_base_constants.h` (auto-generated) `ERubyAlign` /
  `ERubyOverhang` enums; CSS property metadata at `core/css/css_properties.json5:7610`
  (`ruby-align`, keywords `space-around/start/center/space-between`, default
  `space-around`) and `:7621` (`ruby-overhang`, keywords **`auto`/`spaces`**, default
  `auto`, gated behind the `CSSRubyOverhang` runtime flag).

**louis14 targets.**
- `pkg/css/style.go` — add `RubyAlign` (`start`/`center`/`space-between`/`space-around`,
  initial `space-around`) and `RubyOverhang` (**`auto`/`spaces`**, initial `auto` —
  matching Blink, not the CSS Ruby L1 spec's `auto`/`none`) properties + parsing +
  inheritance (both inherited). Decide whether to gate `ruby-overhang` behind a louis14
  feature flag (Blink does via `CSSRubyOverhang`); the css-ruby reftests assume it is
  enabled.
- `pkg/layout/ruby_utils.go` — port `ApplyRubyAlign(availableLineSize, onStartEdge,
  onEndEdge, line)` and `GetOverhang` / `CanApplyStartOverhang` /
  `CommitPendingEndOverhang`.
- `pkg/layout/line_breaker.go` `handleRuby` — call `GetOverhang` before committing the
  column; reduce `rubySize` advance by the start overhang against the previous item.
- `pkg/layout/inline_layout.go` `createLineBoxEx` — after sizing base/annotation
  sub-lines, call `ApplyRubyAlign` per sub-line and apply the returned start/end insets
  before painting glyphs.

**New types.** `css.RubyAlign`, `css.RubyOverhang`; `ruby_utils.go`: `ApplyRubyAlign`,
`AnnotationOverhang`, `GetOverhang`, `CanApplyStartOverhang`, `CommitPendingEndOverhang`.

**Tests this should fix (~7):** ruby-align-001, ruby-align-001a, ruby-align-002,
ruby-align-002a, ruby-align-space-around, ruby-overhang, ruby-overhang-dynamic.

**Gate.** ruby-align-001/002, ruby-overhang at 0% diff. css-ruby ≥ 58 passing.

---

### Phase 6: Autohiding (§3.2) — B4
**Goal.** A ruby annotation whose text content is identical to its base's text content is
not rendered (the annotation box is suppressed but the base still occupies space).

**Blink reference.** CSS Ruby §3.2; Blink suppresses the `<rt>` content during item
collection when it equals the base content.

**louis14 targets.**
- `pkg/layout/inline_item.go` `collectInlinesRecursive` — when closing a ruby column,
  compare the trimmed text of the base run with the trimmed text of its annotation; if
  equal, mark the annotation column's annotation sub-line empty (no glyphs, zero height
  contribution) — mirror Blink's autohide.
- Must re-evaluate after dynamic `textContent` changes (`ruby-autohide-002` mutates
  content in `window.onload`) — see Phase 11 dependency on relayout.

**Tests this should fix (~1):** ruby-autohide-002.

**Gate.** ruby-autohide-002 at 0% diff. css-ruby ≥ 59 passing.

---

### Phase 7: Line breaking & break suppression (B7)
**Goal.** Forced breaks (`<br>`, newline) inside a ruby base/annotation are suppressed
(replaced by space, `#anon-gen-unbreak`); line breaking between ruby *bases* follows
§4 break-between rules; a ruby column that does not fit breaks proportionally across base
and annotations.

**Blink reference.**
- `inline_items_builder.cc:74` `kDisableForcedBreakInRubyColumn`, `:800-801`,
  `:1069-1070` — forced breaks inside ruby columns become spaces.
- `line_breaker.cc:3370-3445` — proportional column breaking + `RubyBreakTokenData`.
- `line_breaker.cc:1059-1060`, `:1190` — column bidi controls / placeholders are
  ignorable break-wise.
- CSS Ruby §4 "Breaking Between Bases".

**louis14 targets.**
- `pkg/layout/inline_item.go` — inside a ruby column, convert `InlineItemControl`
  forced-break items to spaces (or skip), matching `kDisableForcedBreakInRubyColumn`.
- `pkg/layout/line_breaker.go` `handleRuby` — if `rubySize` exceeds remaining width,
  break the base sub-line at a base boundary and the annotation sub-lines proportionally;
  carry a break token so `NextLine` resumes mid-column. Allow breaks *between* columns
  freely (each column is its own atom).

**Tests this should fix (~7):** ruby-line-breaking-001..003,
ruby-line-break-suppression-001/002/003/005.

**Gate.** ruby-line-breaking-001, ruby-line-break-suppression-001 at 0% diff.
css-ruby ≥ 66 passing.

---

### Phase 8: Intrinsic sizing of ruby (B8)
**Goal.** `min-content`/`max-content` of a container with ruby uses `max(base width,
annotation width)` per column; surrounding text contributes normally; columns are
non-breakable atoms for min-content.

**Blink reference.** `inline_node.cc:2103-2210` — intrinsic walk uses
`result.ruby_column->base_line` for the contribution; `ComputeFromMinSizeInternal`.

**louis14 targets.**
- `pkg/layout/min_max_sizing.go` / `pkg/layout/intrinsic_sizing.go` — when the intrinsic
  walk hits a `RubyColumn` `InlineItemResult`, contribute `rubySize` (= max of base /
  annotation sub-line widths) as a single unbreakable unit for min-content and as a
  normal run for max-content.

**Tests this should fix (~3):** ruby-intrinsic-isize-001..003.

**Gate.** ruby-intrinsic-isize-001..003 at 0% diff. css-ruby ≥ 69 passing.

---

### Phase 9: Block ruby (`display: block ruby`) (B9)
**Goal.** `display: block ruby` generates a block-level principal box wrapping an
inline-level ruby container; the principal box honors margins/padding/borders/`columns`
and lays out on its own block line.

**Blink reference.** `layout_ruby_as_block.{h,cc}` — two-box model;
`style_adjuster.cc:272-273,322-323` — `ruby`↔`block ruby`.

**louis14 targets.**
- `pkg/layout/layout_tree_builder.go` `normalizeRubySubtrees` (from Phase 1) — finalize
  the block-ruby two-box generation: principal `LayoutBlockFlow`-style node + single
  anonymous inline `display:ruby` child. The principal box takes the element's box
  properties; the anonymous inline ruby box is style-clean (anonymous style).
- `pkg/layout/block_layout.go` — block-ruby principal box lays out via the normal
  block-flow path; its inline ruby child goes through the Phase 2 inline ruby path.

**Tests this should fix (~6):** block-ruby-001..005, root-block-ruby.xhtml.

**Gate.** block-ruby-001..005 at 0% diff. css-ruby ≥ 75 passing.

---

### Phase 10: Inlinification of blocks & floats inside ruby (B10)
**Goal.** Block-level boxes inside a `display:ruby`/`ruby-text` box are inlinified
(`display: block` → `inline-block`, etc.); floats inside ruby are forced to `float:none`
(per `style_adjuster.cc:788-805`); the rule recurses.

**Blink reference.** `style_adjuster.cc` `AdjustStyleForDisplay:788-805` +
`EquivalentInlineDisplay:303-356`; csswg-drafts #1341 (recursion required).

**louis14 targets.**
- `pkg/layout/layout_tree_builder.go` `inlinifyRubyChildren` (from Phase 1) — finalize:
  walk the subtree of every `display:ruby`/`ruby-text` box, map each in-flow descendant's
  used display to its inline equivalent and clear `float`. Must run before anonymous
  block generation so no anonymous blocks are created inside ruby.
- `pkg/css/style.go` — a `EquivalentInlineDisplay(DisplayType) DisplayType` helper
  mirroring Blink's.

**Tests this should fix (~7):** ruby-inlinize-blocks-001..005,
ruby-inlinize-recursive-simple, root-ruby.xhtml.

**Gate.** ruby-inlinize-blocks-001..005 at 0% diff. css-ruby ≥ 82 passing.

---

### Phase 11: Nested ruby & multi-level block-axis stacking (B11)
**Goal.** A `<ruby>` whose base is itself a `<ruby>` stacks annotations at multiple levels
(`[1]`, `[1,-1]`, `[-2]`, …); `ruby-position` per level chooses over/under; the
`RubyBlockPositionCalculator` places each `RubyLine` at the correct cumulative offset.

**Blink reference.** `ruby_utils.h:130-244` `RubyBlockPositionCalculator` full
`RubyLevel`/`RubyLine` machinery; `ruby_utils.cc:775-1037`; `ParseRubyInInlineItems`
recursion `:170-173`.

**louis14 targets.**
- `pkg/layout/ruby_utils.go` — extend `RubyBlockPositionCalculator` from single-level
  (Phase 2) to the full `RubyLevel []int` path model: `GroupLines` buckets by level,
  `HandleRubyLine` recurses, `PlaceLines` stacks levels cumulatively away from the base
  baseline.
- Add `RubyPosition` CSS property to `pkg/css/style.go` — keywords **`over`/`under`
  only** with initial **`over`** (matching Blink @ 4883d11
  `core/css/css_properties.json5:7642-7654`); the CSS Ruby L1 spec keywords `alternate`
  and `inter-character` are NOT implemented in Blink. Treat any other inputs as `over`
  for now. Blink also exposes a `-webkit-ruby-position` surrogate at
  `css_properties.json5:7632-7640`. Needed to pick the sign of each level.
  `<rt>`/`<rtc>` `ruby-position` is taken from the *ruby* element (`ruby-position` test
  confirms `ruby-position` on `<rt>` is ignored).

**New types.** `css.RubyPosition`; `ruby_utils.go`: `RubyLevel`, `RubyLine`,
`AnnotationDepth`.

**Tests this should fix (~1):** nested-ruby-pairing-001.

**Gate.** nested-ruby-pairing-001 at 0% diff. css-ruby ≥ 83 passing.

---

### Phase 12: Bidi reordering of ruby columns (B12)
**Goal.** Ruby columns and the bases/annotations within them reorder correctly under
`dir`/`unicode-bidi`; the isolate controls Blink emits on `kOpenRubyColumn`/
`kCloseRubyColumn` are respected.

**Blink reference.** `inline_items_builder.cc:1556-1558,1583-1586,1628,1682-1690` — each
ruby column is wrapped in an isolate (`LRI`/`RLI` … `PDI`); `line_breaker.cc:1190` — the
controls are ignorable; CSS Ruby §"Bidi".

**louis14 targets.**
- `pkg/layout/inline_item.go` — when emitting `OpenRubyColumn`/`CloseRubyColumn`, inject
  the matching bidi isolate controls (louis14 already inserts FSI/PDI-style controls for
  `unicode-bidi: isolate` per `bidi.go` — reuse that path).
- `pkg/layout/bidi.go` — ensure the column-as-isolate runs reorder as a unit; base and
  annotation sub-lines each resolve bidi independently.

**Tests this should fix (~3):** ruby-bidi-002, ruby-bidi-003, ruby-bidi-004.

**Gate.** ruby-bidi-002..004 at 0% diff. css-ruby ≥ 86 passing.

---

### Phase 13: Floats, abs-pos, empty containers in/around ruby (B13)
**Goal.** Floats and abs-pos elements inside ruby bases/containers are positioned
correctly (abs-pos escapes to its containing block; floats were already inlinified to
non-floats by Phase 10 — verify); empty ruby base/text containers still occupy the
correct space.

**Blink reference.** CSS Ruby §"Formatting Context" — a ruby base establishes an inline
formatting context; abs-pos children use the nearest positioned ancestor;
`style_adjuster.cc:788-805` float drop.

**louis14 targets.**
- `pkg/layout/inline_item.go` — `InlineItemOutOfFlow` items inside a ruby base collect
  normally; their static position is captured relative to the base sub-line.
- `pkg/layout/out_of_flow_layout.go` — abs-pos descendants of a ruby base resolve against
  the correct containing block (a `position:relative` `<rb>` is the CB).
- `pkg/layout/ruby_utils.go` — an empty base or annotation sub-line still produces a
  zero-width-but-correct-height placeholder so columns line up
  (`inline_layout_algorithm.cc:381`).

**Tests this should fix (~8):** ruby-with-floats-001..003, ruby-float-handling-001,
abs-in-ruby-base, abs-in-ruby-base-container, abs-in-ruby-container,
ruby-base-container-{abs,float}, empty-ruby-text-container-{abs,float}.

**Gate.** abs-in-ruby-base, ruby-with-floats-001 at 0% diff. css-ruby ≥ 94 passing.

---

### Phase 14: Dynamic ruby + `text-combine-upright` + misc singletons (B14, B15, B16)
**Goal.** Close the long tail.

- **B14 dynamic** (ruby-dynamic-insertion-005, ruby-dynamic-removal-003) — inserting/
  removing ruby boxes triggers a correct relayout: the box-fixup (Phase 1/3) and column
  collection (Phase 2) re-run. Verify louis14's relayout-on-DOM-mutation path
  re-invokes `normalizeRubySubtrees` and `CollectInlines`.
- **B15 text-combine-upright** (ruby-text-combine-upright-001a/001b/002a/002b) — a ruby
  base with `text-combine-upright: all` in a vertical writing mode combines its glyphs
  into one upright square; the annotation then aligns to that square. Depends on
  louis14's existing `text-combine-upright` support (check `pkg/text/` / `pkg/layout/`
  writing-mode code); the ruby-specific part is just that the combined base is one atomic
  unit in the base sub-line.
- **B16 misc** (ruby-lang-specific-style-001, ruby-rt-fontsize-001, ruby-no-transform) —
  `ruby-rt-fontsize-001` should already pass after Phase 1 (50% font-size);
  `ruby-no-transform` verifies `transform` does not apply to `ruby/rbc/rb/rtc/rt`
  (CSS Transforms — `transform` does not apply to non-atomic inline boxes; ensure
  louis14's transform application skips ruby internals); `ruby-lang-specific-style-001`
  is a `:lang()` cascade interaction.

**Blink reference.** CSS Ruby §"text-combine-upright" interaction;
`layout_object.cc:1225-1227` — ruby-text fragment-item special-casing;
CSS Transforms §"transformable element".

**louis14 targets.** `pkg/layout/layout_tree_builder.go` relayout entry,
`pkg/render/` transform application gate, `pkg/text/` text-combine path,
`pkg/layout/ruby_utils.go` combined-base handling.

**Tests this should fix (~9):** ruby-dynamic-insertion-005, ruby-dynamic-removal-003,
ruby-text-combine-upright-001a/001b/002a/002b, ruby-lang-specific-style-001,
ruby-rt-fontsize-001 (if not earlier), ruby-no-transform.

**Gate.** All 99 css-ruby tests pass at 0% diff.

---

### Phase 15: Delivery & regression audit
- [ ] Confirm all 99 css-ruby tests pass at 0% diff via `TestWPTCSS3Reftests/css-ruby`.
- [ ] Re-run CSS2 (expect 99/99) and css-writing-modes (expect baseline unchanged) — the
      ruby work touches `inline_item.go`, `line_breaker.go`, `inline_layout.go`,
      `layout_tree_builder.go`, `cascade.go`, `style.go`, all shared with non-ruby paths.
- [ ] Spot-check css-position / css-display for incidental regressions from the display
      enum and `EquivalentInlineDisplay` changes.
- [ ] Final commit summary / report.

## Phase count
**15 phases** (Phase 0 baseline + 14 implementation/delivery phases).

## Key Blink files & classes the plan is grounded in
| Blink file | Classes / functions | Used by phase |
|---|---|---|
| `core/html/resources/html.css:1701-1720` (+ `:972-975` for the `rp` global hide) | ruby UA stylesheet | 1 |
| `core/css/resolver/style_adjuster.cc:247-356, 756-846` | `EquivalentBlockDisplay`, `EquivalentInlineDisplay`, `AdjustStyleForDisplay` (blockify/inlinify) | 1, 9, 10 |
| `core/layout/layout_object.cc:430-435, 522-529, 1225-1228` | `CreateObject`, `IsInlineRuby`, `IsInlineRubyText`, ruby-text fragment-item container | 1, 14 |
| `core/layout/layout_ruby_as_block.{h,cc}` | `LayoutRubyAsBlock` two-box model | 1, 9 |
| `core/layout/inline/inline_items_builder.cc:74, 801, 1068, 1510-1697` | `kOpenRubyColumn`/`kCloseRubyColumn`/`kRubyLinePlaceholder` emission (`EnterInline` body 1510; ruby logic 1550-1595), column-after-`<rt>` (`ExitInline` body 1608; ruby logic 1612-1697 with column reopen at 1682-1697 and almost-empty-column removal at 1617-1628), `kDisableForcedBreakInRubyColumn` (`:74`; rewrites at `:801` and `:1068`) | 2, 3, 4, 7, 12 |
| `core/layout/inline/ruby_utils.{h,cc}` | `RubyItemIndexes`, `ParseRubyInInlineItems`, `AnnotationOverhang`, `GetOverhang`, `CanApplyStartOverhang`, `CommitPendingEndOverhang`, `ApplyRubyAlign`, `AnnotationMetrics`, `ComputeAnnotationOverflow`, `UpdateRubyColumnInlinePositions`, `RubyBlockPositionCalculator` (`RubyLevel`/`RubyLine`/`AnnotationDepth`/`GroupLines`/`PlaceLines`/`AddLinesTo`); class declared at `ruby_utils.h:128-244` | 2, 3, 5, 11, 13 |
| `core/layout/inline/line_breaker.cc:1059-1093, 1190, 2561-2615, 3278-3449` | `HandleRuby` (3278-3449), ruby dispatch (1082-1093), column break tokens, intra-column trailing-space collapse (recurses into `ancestor_ruby_columns`) | 2, 4, 7 |
| `core/layout/inline/inline_layout_algorithm.cc:381-389, 396-418` | ruby line metrics force (381-389), `UpdateRubyColumnInlinePositions` → `RubyBlockPositionCalculator` → `SetAnnotationBlockStartAdjustment` (396-418) | 2, 13 |
| `core/layout/inline/inline_node.cc:1543-1544, 2209-2210` | ruby intrinsic sizing: `kOpen/CloseRubyColumn` cases (`:1543-1544`); `IsRubyColumn() → ComputeFromMinSizeInternal(result.ruby_column->base_line)` (`:2209-2210` inside `ComputeFromMinSizeInternal` at `:2173`) | 8 |
| `core/style/computed_style_base_constants.h` (auto-generated; re-exported via `computed_style_constants.h`) | `EDisplay` (`kRuby`/`kBlockRuby`/`kRubyText` only), `ERubyAlign`, `ERubyOverhang`, `RubyPosition` | 1, 5, 11 |
| `core/css/css_properties.json5:3416, 7610-7654` | CSS property metadata: `display` keyword list (3416, ruby keys = `"ruby","ruby-text"`); `ruby-align` (7610), `ruby-overhang` (7621, keywords `auto/spaces`, runtime-flagged), `-webkit-ruby-position` (7632), `ruby-position` (7642, keywords `over/under` only, default `over`) | 1, 5, 11 |

## louis14 files touched (summary)
| louis14 file | Phases |
|---|---|
| `pkg/css/style.go` (display enums, ruby props, `EquivalentInlineDisplay`) | 1, 5, 10, 11 |
| `pkg/css/cascade.go` (ruby UA styles) | 1 |
| `pkg/layout/layout_tree_builder.go` (`normalizeRubySubtrees`, `inlinifyRubyChildren`, block-ruby two-box, pseudo ruby content, relayout) | 1, 3, 9, 10, 14 |
| `pkg/layout/inline_item.go` (ruby column items, pairing, autohide, intra-base ws, forced-break suppression, bidi isolates, OOF) | 2, 3, 4, 6, 7, 12, 13 |
| **`pkg/layout/ruby_utils.go`** (NEW — mirrors `core/layout/inline/ruby_utils.cc`) | 2, 3, 5, 11, 13 |
| `pkg/layout/line_breaker.go` (`handleRuby`, sub-line breaking, `InlineItemResult.RubyColumn`, `LineInfo` ruby fields, column breaking, trailing ws) | 2, 4, 5, 7 |
| `pkg/layout/inline_layout.go` (`createLineBoxEx` base/annotation placement, `computeLineMetricsEx` annotation contribution, ruby-align insets) | 2, 5 |
| `pkg/layout/min_max_sizing.go` / `intrinsic_sizing.go` (ruby intrinsic) | 8 |
| `pkg/layout/block_layout.go` (block-ruby principal box) | 9 |
| `pkg/layout/out_of_flow_layout.go` (abs-pos in ruby base) | 13 |
| `pkg/layout/bidi.go` (column isolate reordering) | 12 |
| `pkg/render/` (annotation fragment paint, transform gate) | 2, 14 |
| `pkg/text/` (text-combine base in ruby) | 14 |

## Decisions Made
| Decision | Rationale |
|---|---|
| Model ruby as inline/line-based (Blink's modern `RubyLineBreakable`), NOT table-style `LayoutRubyRun` | Matches current Blink; the failing tests (ruby-align, line-breaking, overhang, bidi) are written against the inline model. The old table model cannot pass them. |
| Delete `DisplayRubyBase` / drop `rb/rbc/rtc` UA displays | Modern Blink `EDisplay` has no `kRubyBase`; `html.css` gives `rb/rbc/rtc` no UA display. louis14's current `cascade.go` invents these — a foundational modeling error. |
| `ruby_utils.go` placed in `pkg/layout/` mirroring `core/layout/inline/ruby_utils.cc` | File-placement rule: port a Blink primitive into the package mirroring Blink's source location. |
| Phase 2 builds single-level stacking; Phase 11 generalizes to `RubyLevel` paths | Single-level unblocks ~17 tests immediately; the full multi-level `RubyBlockPositionCalculator` is only needed by nested ruby (1 test) and is higher-risk. |
| Phase order is unblock-count-first, not %-diff-first | Per CLAUDE.md §1: the foundational fix (Phases 1-2) generalizes to all cases; property phases (5,6) and edge phases (12-14) layer on top. |
| One full css-ruby run per gate, not per test | CLAUDE.md §4: feature work runs only the 1-4 tests for the phase; the per-phase gate count is checked from the existing survey + targeted runs, with a single full-category run reserved for Phase 15 delivery. |

## Key Questions
1. Does louis14's sub-line breaking need a real recursive `LineBreaker` instance, or can
   `handleRuby` reuse the current `LineBreaker` with a bounded item range? Answer in
   Phase 2 research — Blink uses `CreateSubLineInfo`, a full sub-breaker.
2. Is louis14's relayout-on-DOM-mutation path already re-running `CollectInlines` and the
   tree builder, or does ruby box-fixup need an explicit invalidation hook? Answer in
   Phase 14.
3. Does louis14 already support `text-combine-upright` well enough that Phase 15's B15
   tests only need the ruby-atom wiring, or is there a deeper text-combine gap? Check
   `pkg/text/` and `pkg/layout/writing_mode*.go` at the start of Phase 14.

## Notes
- Test command template (single phase, 1-4 tests):
  `GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-ruby/<name>' -v`
- Pre-rendered diffs for every current failure are on disk under `output/reftests/`
  (`<name>_{test,ref,diff}.png`) — read these before and after each phase.
- Worktree note: css-ruby tests use Ahem and `mplus-1p-regular.woff`; symlink `fonts/`
  from the main dir before any broad run in a worktree (per memory
  `feedback_worktree_fonts.md`).
</content>
</invoke>
