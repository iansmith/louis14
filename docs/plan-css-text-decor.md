# Task Plan: Pass the entire css-text-decor WPT reftest category

## Goal
All `css-text-decor` tests under `pkg/visualtest/testdata/wpt-css3/css-text-decor/` pass at 0% pixel
diff via `TestWPTCSS3Reftests/css-text-decor`. Baseline **96 passing / 154 failing / 0 skipped
(250 run)** → close the 154 failures without regressing adjacent text/inline categories (css-text,
css-inline, css-writing-modes, css-ruby, css-fonts, CSS2 text).

## Rules & Discipline (DO NOT DUPLICATE HERE)
Re-read both before planning or coding — non-negotiable project rules:

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness (no point fixes; every fix
   must generalize), study Blink first, 0% diff required, test-execution discipline (run only the
   1–4 tests tied to the phase in flight, never the full section), operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory
   index pointing at the same rules, plus the worktree-fonts symlink note (relevant: emphasis tests
   need CJK + Ahem fonts).

If you are about to type a rule verbatim into this file or into code comments, stop and link instead.

## Baseline snapshot (2026-05-14)
From `grep "FAIL: TestWPTCSS3Reftests/css-text-decor/" docs/reftest-survey-2026-05-14-raw.txt`
(154 failures), bucketed by prefix:

| Bucket | Count | Tests (prefix) |
|--------|------:|----------------|
| **A — Decoration model: line set + propagation + decorating box** | 19 | `text-decoration-line` (4), `text-decoration-propagation-*` (6), `text-decoration-subelements-*` (2), `text-decoration-color` (1), `text-decoration-skip-spaces-*` (4), `ruby-text-decoration-01` (1), `text-decoration-double-001` (1) |
| **B — Decoration geometry: thickness / underline-position / underline-offset / inset** | 24 | `text-decoration-inset-*` (11), `text-decoration-thickness-*` (10), `text-underline-offset-*` (2), `text-underline-position-from-font-variable` (1) |
| **C — text-emphasis: style, color, position, line-height, ruby** | 111 | `text-emphasis-position-*` (48), `text-emphasis-style-*` (29), `text-emphasis-line-height-*` (13), `text-emphasis-property-*` (6), `text-emphasis-ruby-*` (6), `text-emphasis-color-*` (5), `text-emphasis-punctuation-*` (3), `text-emphasis-dot-001` (1) |

Bucket C is ~72% of the failures, and almost every emphasis reference file is built from
`<ruby><rt>…</rt></ruby>` — so the emphasis bucket is gated on **ruby layout** being correct in
addition to emphasis painting itself. Buckets A and B share a single missing abstraction (a Blink
`TextDecorationInfo`-style geometry/applied-decoration model), so they are sequenced first as the
foundation; C builds on the same per-fragment paint plumbing plus a ruby/annotation phase.

### Symptoms observed in `output/reftests/*_{test,ref,diff}.png`
- **A — `text-decoration-line_test.png`**: lines 4–6 ("This text contains no decorations") are
  decorated in the test but not the ref; the decorating-element lines under-decorate. Confirms
  `text-decoration` is being **inherited** (so it leaks down *and* a child longhand can't reset it).
- **A — `text-decoration-propagation-01_test.png`**: "This text must not be underlined" inside an
  `<svg>` *is* underlined — decoration propagates through an atomic inline. Ref: no underline.
- **A — `text-decoration-double-001_{test,ref}.png`**: test paints a single green line; ref shows
  two 3px lines with a 2px gap. `drawDoubleLine` runs but the geometry/position is wrong, and the
  underline sits too high.
- **B — `text-decoration-inset-001_diff.png`**: only a few stray red pixels — `<u>` underline is
  mispositioned/clipped; `text-decoration-inset` (Text Decor L4) is unsupported entirely.
- **B — `text-decoration-thickness-fixed_diff.png`**: 2nd row underline is the wrong thickness *and*
  the wrong vertical position — `text-decoration-thickness` percent/`from-font` and
  `text-underline-position: from-font` are not resolved.
- **C — `text-emphasis-position-property-001_{test,ref}.png`**: ref renders `<ruby>` with 5 `<rt>`
  bullets stacked at the left margin across 5 lines (`line-height:5`) — i.e. **ruby annotations
  render in the wrong place** in the ref. Test renders 5 bullets clustered near the top with no CJK
  base text. Both sides are broken differently → ruby layout + emphasis-position both wrong.
- **C — `text-emphasis-style-002_{test,ref}.png`**: the *test* side actually looks correct ("Text
  sample" with a dot over each glyph). The *ref* side (`<ruby><rt>•</rt></ruby>`) renders the dots
  stacked vertically at the margin — pure ruby layout bug.
- **C — `text-emphasis-line-height-001a_{test,ref}.png`**: emphasis marks expand the line box in
  neither side correctly; test marks overflow above the border line, ref places them but with wrong
  ruby spacing.

## Root-cause analysis (done — read before coding)

### The shape of the existing code
- **Property storage**: `pkg/css/style.go` — `GetTextDecoration()` (style.go:3927) returns a
  *single* `TextDecoration` enum (`none|underline|overline|line-through`, style.go:3917-3923), so
  louis14 cannot represent the common `underline overline`, cannot represent per-ancestor
  accumulation, and folds `text-decoration-line` into the same key. `GetTextDecorationStyle`
  (style.go:8488), `GetTextDecorationThickness` (style.go:8496 — `auto`/`from-font` both hard-coded
  to `1`, no percent support), `GetTextUnderlineOffset` (style.go:4030). There is **no**
  `text-underline-position`, **no** `text-decoration-skip-ink`, **no** `text-decoration-skip-spaces`,
  **no** `text-decoration-inset`. Emphasis: `GetTextEmphasisStyle/Color/Position/Mark`
  (style.go:8524-8606).
- **Cascade**: `pkg/css/cascade.go:843` lists `"text-decoration"` in `inheritableProperties` —
  **this is the central Bucket-A bug**. Text decoration is *not* inherited in CSS; it propagates
  from a "decorating box" to its in-flow inline descendants, does not cross atomic-inline /
  block-in-inline boundaries, and a descendant cannot remove an ancestor's decoration.
  `cascade.go:30` force-sets `text-decoration: underline` for `<u>`/`<ins>`-type elements.
- **PaintLayer**: `pkg/render/paint_layer.go:129-148` carries `TextDecoration`,
  `TextDecorationColor/Thickness/Style`, `TextUnderlineOffset`, `TextShadows`, `TextEmphasisMark`,
  `TextEmphasisColor`, `TextEmphasisOver`. Populated at `paint_layer.go:506-529` straight from the
  text element's own style — so a text run only ever paints *its own* element's decoration, never
  an ancestor decorating box's.
- **Painting**: `pkg/render/render.go` — `drawTextDecoration` (render.go:3987) switches on the
  single enum, computes `lineY` with ad-hoc constants (`ascent + |descent|*0.25 + offset` for
  underline; `box.Y` for overline; `ascent*0.65` for line-through, render.go:4001-4006), then
  dispatches to `drawDashedLine`/`drawDottedLine`/`drawDoubleLine` (render.go:3004-3066) or
  `drawWavyLine` (render.go:4030). `drawTextEmphasis` (render.go:3608) opens a half-size font,
  positions the mark at `box.Y - emphFontSize*0.25` (over) or `box.Y + ascent + emphFontSize*0.5`
  (under), and draws it per-codepoint.
- **Layout**: `pkg/layout/inline_layout.go` has no decoration positioning and no ruby support;
  `text-decoration*` longhands appear only in a non-inheritance bookkeeping list
  (`inline_layout.go:1100-1102`). `grep -ri ruby pkg/layout` shows ruby is **not implemented** —
  `<rt>`/`<rp>`/`<ruby>` are laid out as ordinary inlines, which is why every emphasis *reference*
  renders annotations stacked at the margin.

### Bucket A — there is no decoration *model*; `text-decoration` is wrongly inherited
Blink does not inherit `text-decoration`. `ComputedStyle` holds an
**`AppliedTextDecorationVector` = `GCedHeapVector<AppliedTextDecoration, 1>`**
(`core/style/applied_text_decoration.h:52`). Each `AppliedTextDecoration` carries
`lines_` (a bitfield of underline|overline|line-through, applied_text_decoration.h:45), `style_`
(applied_text_decoration.h:46), `color_` (applied_text_decoration.h:47), `thickness_`
(`TextDecorationThickness`, applied_text_decoration.h:48), and `underline_offset_` (a `Length`,
applied_text_decoration.h:49). When a box that "establishes" a text decoration is encountered, the
style builder *appends* a new `AppliedTextDecoration` to the inherited vector — decorations
**accumulate** down the tree rather than being overwritten, and a child that sets
`text-decoration-line: none` simply contributes nothing (it cannot erase an ancestor's entry).
Propagation stops at atomic inlines / block containers: those reset the vector
(`core/paint/inline_paint_context.h` `DecoratingBoxList`, `PushDecoratingBox`/`PopDecoratingBox`,
`SyncDecoratingBox`). At paint time the decorating box's content origin is reconciled with the
target text origin via `TextDecorationInfo::OffsetFromDecoratingBox()`
(`core/paint/text_decoration_info.cc:344-352`).

louis14 must mirror this: stop inheriting `text-decoration`, give `Style` an accumulating
`[]AppliedTextDecoration`, and give each text `PaintLayer` the *list* of decorations in effect
(its decorating-box ancestors' entries), not just its own element's.

### Bucket B — no `TextDecorationInfo` geometry; thickness/position constants are guesses
Blink centralizes all decoration geometry in **`TextDecorationInfo`**
(`core/paint/text_decoration_info.{h,cc}`):
- **`ComputeThickness()`** (text_decoration_info.cc:417-432): `auto` → `font-size / 10`;
  `from-font` → font's `UnderlineThickness()` (fallback to auto); `<length>`/`<percentage>` →
  `FloatValueForLength()` then rounded (percent resolves against font-size). louis14's
  `GetTextDecorationThickness` hard-codes `auto`/`from-font` to `1` and never handles `%`.
- **`ComputeUnderlineLineData()` / `ComputeUnderlineOffset()`** (text_decoration_info.cc:354-370):
  resolves `text-underline-position` (`auto` → just under the alphabetic baseline using font
  metrics; `from-font` → font's underline position; `under` → below the text-bottom / em-box
  edge), then adds `text-underline-offset`, then adds `offset_from_decorating_box`. louis14 has
  *no* `text-underline-position` property and a single ad-hoc `ascent + |descent|*0.25` constant.
- **`ComputeOverlineLineData()`** (text_decoration_info.cc:373-390): `TextTop` position, with the
  flipped-for-vertical case handled.
- **`ComputeLineThroughLineData()`** (text_decoration_info.cc:393-400): centered at
  `2/3 * ascent`, then `-= thickness/2` so it stays centered as thickness grows.
- **double / wavy geometry** (text_decoration_info.cc:302-335): double-line gap is
  `thickness + 1.0` for underline/overline (floored for line-through); wavy uses the same control
  offsets. louis14's `drawDoubleLine`/`drawWavyLine` invent their own spacing.

`text-decoration-inset` (CSS Text Decor L4) trims/offsets the decoration's start/end *along* the
line; it is unsupported. It is a two-value `<length>{1,2}` that shrinks (or, negative, extends) the
painted segment — best modeled as `inlineStart`/`inlineEnd` insets applied to the decoration rect
inside the same geometry model.

### Bucket C — emphasis painting is close, but ruby layout is missing (and refs depend on it)
Two independent problems, both required:
1. **Emphasis mark placement** mirrors `TextPainter::SetEmphasisMark()`
   (`core/paint/text_painter.cc:539-567`): for **over**,
   `offset = -Ascent() - font.EmphasisMarkDescent(mark)`; for **under**,
   `offset = Descent() + font.EmphasisMarkAscent(mark)`. The mark glyph is the resolved
   `text-emphasis-style` shape/string; position is `text-emphasis-position` (over/under +
   left/right; the left/right axis only matters in vertical writing modes). Crucially the marks
   **expand the line box like ruby** — `text_item->HasOverAnnotation()/HasUnderAnnotation()` feed
   the line-box ascent/descent. louis14's `drawTextEmphasis` uses `emphFontSize*0.25`/`*0.5`
   guesses, never reserves line-box space (so `text-emphasis-line-height-*` all fail), and ignores
   the left/right axis.
2. **Ruby layout is absent.** Every emphasis *reference* is `<ruby>base<rt>mark</rt>…</ruby>`; with
   no ruby support louis14 stacks the `<rt>` content as ordinary inline boxes at the margin. The
   emphasis bucket cannot reach 0% until `<ruby>`/`<rt>`/`<rp>` lay out as a base line with an
   annotation line positioned over (or under) the base, and the annotation expands the line box.
   This mirrors Blink's `LayoutRubyColumn` / ruby line-box-expansion machinery; the minimum needed
   here is: pair `<rt>` with its base run, center the annotation over the base, reserve
   ascent space on the line box for the annotation, and inherit `font-variant-east-asian` into
   `<rt>` (the refs set `rt { font-variant-east-asian: inherit|normal }`).

CJK glyph coverage: emphasis tests use `試験テスト`. Confirm a CJK fallback font is wired (memory
note: worktrees only ship `Ahem.ttf` — symlink `fonts/` before any sweep). Several emphasis tests
*do* use Ahem (`text-emphasis-line-height-*`), so those are testable without CJK.

## Phased plan (foundational-first)

Ordering rationale: Phase 1 fixes the cascade bug and introduces the `AppliedTextDecoration` model
that Phases 2–4 all build on; Phase 2 is the `TextDecorationInfo` geometry core that every line
type/style needs; Phase 3 layers the L4 geometry knobs onto that core; Phase 4 does decorating-box
propagation through the inline tree; Phase 5 builds ruby layout (a prerequisite for the emphasis
references); Phase 6 does emphasis painting on top of the now-correct line-box + ruby plumbing;
Phase 7 mops up skip-ink/skip-spaces.

---

### Phase 1 — Decoration model: stop inheriting, introduce `AppliedTextDecoration`
**Goal.** Replace the single inherited `TextDecoration` enum with an accumulating list, so
`text-decoration-line` resets only its own longhands, `text-decoration` shorthand expands
correctly, and decorations from multiple ancestors coexist.
**Blink reference.** `core/style/applied_text_decoration.h` (class `AppliedTextDecoration`, fields
`lines_`/`style_`/`color_`/`thickness_`/`underline_offset_`, lines 20-56; `AppliedTextDecorationVector`
line 52); the style-builder behavior that *appends* rather than inherits; `TextDecorationLine`
bitfield enum.
**louis14 targets.** `pkg/css/style.go` (new type + getters), `pkg/css/cascade.go` (remove from
`inheritableProperties`, add accumulation step), `pkg/css/style.go` shorthand expansion for
`text-decoration`.
**New types.** `css.TextDecorationLine` (bitfield: `Underline|Overline|LineThrough`);
`css.AppliedTextDecoration{ Lines TextDecorationLine; Style string; Color Color; HasColor bool;
Thickness TextDecorationThickness; UnderlineOffset Length }`; `css.TextDecorationThickness`
(`{Kind: Auto|FromFont|Length; Value Length}`). `Style.GetAppliedTextDecorations() []AppliedTextDecoration`.
**Approach.** (a) Delete `"text-decoration"` from `cascade.go:843`. (b) During cascade, compute each
element's *own* contributed `AppliedTextDecoration` from its `text-decoration-line/style/color/
thickness` longhands + `text-underline-offset`; append it to the parent's resolved vector to form
this element's vector; reset the vector to empty at atomic-inline / block-container boundaries
(Phase 4 wires the boundary logic into layout — here just store the per-element contribution and
the inherited-vs-reset flag). (c) Expand the `text-decoration` shorthand into the four longhands.
(d) Keep `<u>`/`<ins>`/`<a>` UA defaults (`cascade.go:30`) but expressed as `text-decoration-line`.
**Tests fixed.** `text-decoration-line.html`, `text-decoration-line-011/012/013.xht`,
`text-decoration-color.html`.
**Gate.** Those 5 tests at 0%; no regression in css-text / css-inline / CSS2.

### Phase 2 — `TextDecorationInfo` geometry core (thickness, line positions, double/wavy)
**Goal.** One geometry model that computes, for each `AppliedTextDecoration` line, the correct
rect/path: resolved thickness, baseline-relative Y for underline/overline/line-through, and
double/wavy stroke geometry — replacing the ad-hoc constants in `drawTextDecoration`.
**Blink reference.** `core/paint/text_decoration_info.{h,cc}` — constructor
(text_decoration_info.h:169-176), `ComputeThickness()` (cc:417-432), `ComputeUnderlineLineData()`
/`ComputeUnderlineOffset()` (cc:354-370), `ComputeOverlineLineData()` (cc:373-390),
`ComputeLineThroughLineData()` (cc:393-400), double/wavy geometry (cc:302-335),
`DecorationGeometry`. `core/paint/decoration_line_painter.{h,cc}` for the stroke rasterization
(solid/dotted/dashed/double/wavy).
**louis14 targets.** New `pkg/render/text_decoration_info.go` (mirror Blink's file), rewrite
`pkg/render/render.go` `drawTextDecoration` (render.go:3987), `drawDoubleLine`/`drawWavyLine`
(render.go:3066, 4030); `pkg/css/style.go` `GetTextDecorationThickness` to return
`TextDecorationThickness` and resolve `%`.
**New types.** `render.textDecorationInfo` (origin, width, target font metrics, ascent, the
`[]AppliedTextDecoration`); `render.decorationGeometry` (line Y, thickness, doubleOffset, wavy
control points).
**Approach.** Port `ComputeThickness` exactly (`auto`→`fontSize/10`; `from-font`→font underline
thickness with auto fallback; length/percent→resolved+rounded). Port the three
`Compute*LineData` baseline-relative formulas. Port double gap = `thickness+1` and the wavy control
geometry. `drawTextDecoration` iterates the layer's `[]AppliedTextDecoration`, and for each set
line builds a `decorationGeometry` and strokes it per `style`.
**Tests fixed.** `text-decoration-double-001.html`, `text-decoration-thickness-fixed.html`,
`text-decoration-thickness-{underline,overline,linethrough}-001.html`,
`text-decoration-thickness-percent-001.html`, `text-decoration-thickness-calc`,
`text-decoration-thickness-from-zero-sized-font.html`, `text-decoration-thickness-scroll-001.html`.
**Gate.** Those tests + the Phase-1 set at 0%.

### Phase 3 — L4 geometry knobs: `text-underline-position`, `from-font`, `text-decoration-inset`
**Goal.** Layer the remaining position/offset knobs onto the Phase-2 geometry: full
`text-underline-position` (`auto|from-font|under|left|right`), `from-font` thickness/position read
from the variable-font's underline metrics, and `text-decoration-inset`.
**Blink reference.** `ComputeUnderlineOffset()` / `ComputeUnderlineOffsetForUnder()` and the
`text-underline-position` resolution (text_decoration_info.cc:354-390); font underline-position
metrics; `core/css/properties/longhands/` for `text-underline-position` parsing; CSS Text Decor L4
§ `text-decoration-skip-inset` / `text-decoration-inset` definition.
**louis14 targets.** `pkg/css/style.go` (new `GetTextUnderlinePosition`,
`GetTextDecorationInset`), `pkg/render/text_decoration_info.go` (consume them), font-metrics access
for underline position in `pkg/text/` / wherever `GetFontMetrics` lives.
**New types.** `css.TextUnderlinePosition` enum; inset stored as `{InlineStart, InlineEnd Length}`.
**Approach.** In `ComputeUnderlineLineData`, branch on `text-underline-position`: `auto` keeps the
Phase-2 baseline formula; `from-font` reads the font's underline-position metric; `under` drops to
the text-bottom edge. Wire `from-font` thickness in `ComputeThickness`. For `text-decoration-inset`,
trim the decoration rect's inline-start/inline-end by the resolved lengths (negative = extend).
**Tests fixed.** `text-decoration-inset-001..025` (the 11 failing ones + any latent),
`text-decoration-inset-orthogonal-block-001`, `text-underline-offset-{variable,zero-position}`,
`text-underline-position-from-font-variable`, `text-decoration-thickness-from-font-variable`,
`text-decoration-thickness-vertical-001/002`.
**Gate.** Bucket B (24) at 0%; Phases 1–2 still green.

### Phase 4 — Decorating-box propagation through the inline tree
**Goal.** A text run paints the decorations of *all* its decorating-box ancestors, positioned
relative to each decorating box's content origin — and propagation stops at atomic inlines
(`<svg>`, inline-block, replaced) and block boundaries.
**Blink reference.** `core/paint/inline_paint_context.h` — `DecoratingBoxList`, `DecoratingBox`,
`PushDecoratingBox`/`PushDecoratingBoxes`/`PopDecoratingBox`, `SyncDecoratingBox`;
`TextDecorationInfo::OffsetFromDecoratingBox()` (text_decoration_info.cc:344-352);
`TextDecorationPainter` Phase enum + `Begin`/`PaintExceptLineThrough`/`PaintOnlyLineThrough`
(text_decoration_painter.h:64-81).
**louis14 targets.** `pkg/layout/inline_layout.go` (carry the active decorating-box list while
walking inline items; reset at atomic-inline / block boundaries), `pkg/render/paint_layer.go:506-529`
(populate the text layer's `[]AppliedTextDecoration` *and* per-decoration origin offset from the
decorating-box list rather than from the element's own style), `pkg/render/render.go`
`drawTextDecoration` (apply the per-decoration `offsetFromDecoratingBox`).
**New types.** `layout.DecoratingBox{ Box *Box; ContentOriginY float64; Decorations
[]css.AppliedTextDecoration }`; a `[]DecoratingBox` stack threaded through inline layout.
**Approach.** During inline layout, push a decorating box when entering an inline box whose style
contributes a decoration; pop on exit; do **not** descend the stack into atomic inlines. Attach the
resolved list to each generated text fragment / text `PaintLayer`. In paint, each decoration's
`lineY` is computed by Phase 2's geometry plus `OffsetFromDecoratingBox` (decorating box content
top − this text run's line-over). Spelling-style "child can't remove" falls out for free because
the list is accumulated, not overridden.
**Tests fixed.** `text-decoration-propagation-01..04`,
`text-decoration-propagation-display-contents`, `text-decoration-propagation-dynamic-001`,
`text-decoration-subelements-002/003`, `ruby-text-decoration-01`.
**Gate.** Bucket A (19 total) at 0%; Phases 1–3 still green.

### Phase 5 — Ruby layout (`<ruby>`/`<rt>`/`<rp>`)
**Goal.** `<ruby>` lays out a base line with an annotation line positioned over (default) or under
the base; the annotation is centered over its base run and **expands the line box**; `<rp>` is
hidden when ruby is supported. This is the prerequisite for every emphasis *reference*.
**Blink reference.** `core/layout/inline/` ruby handling — `LayoutRubyColumn` / ruby column
pairing, ruby base vs. annotation line allocation, and the line-box ascent/descent expansion for
annotations; `ruby-position` (`over`/`under`); the `rt { font-variant-east-asian: inherit }` UA
interaction.
**louis14 targets.** `pkg/layout/inline_layout.go` (ruby item grouping + annotation line),
`pkg/layout/fragment_builder.go` (annotation fragment + line-box metric expansion — note the
existing comment at fragment_builder.go:142 "louis14 omits the annotation-adjustment term"),
`pkg/css/cascade.go` (`<rp>` default `display:none`, ruby-related defaults), `pkg/css/style.go`
(`ruby-position` getter).
**New types.** `layout.RubyColumn{ Base, Annotation []InlineItem }` or equivalent; line-box
metrics gain an `annotationAscent`/`annotationDescent` term.
**Approach.** Group consecutive base content + following `<rt>` into ruby columns; lay the base run
on the main line; lay the `<rt>` content on an annotation line at half-ish font-size, centered over
the base's inline extent; expand the line box's over-side (or under-side for `ruby-position:under`)
ascent by the annotation height so siblings/`line-height` interact correctly. Inherit
`font-variant-east-asian` into `<rt>`.
**Tests fixed.** Directly unblocks the *reference* side of the entire emphasis bucket; on its own
fixes `text-emphasis-ruby-001..004a` (6) where the test side is plain ruby vs. ruby ref. Also helps
adjacent css-ruby category (verify no regression).
**Gate.** `text-emphasis-ruby-*` (6) at 0%; representative emphasis *refs* now render annotations
in the right place (visual check of 2–3 `_ref.png`); Phases 1–4 still green.

### Phase 6 — text-emphasis painting on correct line-box + ruby plumbing
**Goal.** Emphasis marks: correct glyph/shape/string from `text-emphasis-style`, correct
over/under + left/right from `text-emphasis-position`, correct color from `text-emphasis-color`,
**and** the marks expand the line box exactly like a ruby annotation (so `line-height` tests pass).
**Blink reference.** `TextPainter::SetEmphasisMark()` (text_painter.cc:539-567 — over offset
`-Ascent() - EmphasisMarkDescent(mark)`, under offset `Descent() + EmphasisMarkAscent(mark)`,
with `HasOverAnnotation()/HasUnderAnnotation()` feeding line-box expansion);
`graphics_context_.DrawEmphasisMarks()` (text_painter.cc:567-571); `Font::EmphasisMarkAscent`/
`EmphasisMarkDescent`/`EmphasisMarkHeight`; `text-emphasis-style` shape→glyph mapping and
`text-emphasis-position` resolution in `core/css/`.
**louis14 targets.** `pkg/render/render.go` `drawTextEmphasis` (render.go:3608) — rewrite to use
emphasis-mark font-metric ascent/descent rather than `*0.25`/`*0.5` guesses; `pkg/render/paint_layer.go`
(carry full position incl. left/right + per-side); `pkg/layout/inline_layout.go` /
`fragment_builder.go` (reserve emphasis annotation space on the line box, reusing Phase-5's
annotation-metric term); `pkg/css/style.go` emphasis getters (punctuation handling for
`text-emphasis-punctuation-*`, `dot`/`circle`/`sesame`/`triangle` filled/open, string marks).
**New types.** `css.TextEmphasisPosition{ Over bool; Right bool }`; emphasis annotation metrics on
the line box (shared with Phase 5).
**Approach.** Resolve the mark string once (shape→glyph or literal string). Compute the per-mark
offset from emphasis-font ascent/descent like Blink. Center each mark over its base glyph cluster;
skip marks on punctuation/spaces per spec (`text-emphasis-punctuation-*`). Reserve line-box space
so `text-emphasis-line-height-*` match. Honor `text-emphasis-position` left/right only in vertical
modes.
**Tests fixed.** `text-emphasis-style-*` (29), `text-emphasis-position-*` (48),
`text-emphasis-line-height-*` (13), `text-emphasis-property-*` (6), `text-emphasis-color-*` (5),
`text-emphasis-punctuation-*` (3), `text-emphasis-dot-001`.
**Gate.** Bucket C (111) at 0%; Phases 1–5 still green.

### Phase 7 — skip-ink / skip-spaces
**Goal.** `text-decoration-skip-ink` (interrupt the line where glyph ink crosses it) and
`text-decoration-skip-spaces` (skip leading/trailing/all spaces).
**Blink reference.** `TextDecorationInfo::BaselineForInkSkip()` (text_decoration_info.h:225-227)
and the skip-ink dilation pass in `decoration_line_painter.cc`;
`text-decoration-skip-spaces` longhand handling.
**louis14 targets.** `pkg/render/text_decoration_info.go` + `drawTextDecoration` (ink-skip
gap computation against glyph bounds), `pkg/css/style.go` (`GetTextDecorationSkipInk`,
`GetTextDecorationSkipSpaces`), `pkg/layout/inline_layout.go` (identify leading/trailing space runs
for skip-spaces).
**Approach.** For skip-ink: per glyph, where its ink box overlaps the decoration band, split the
stroke into segments with small dilation gaps. For skip-spaces: trim the decoration rect to exclude
leading/trailing (or all) whitespace advance per the property value.
**Tests fixed.** `text-decoration-skip-spaces-001..004`, `text-decoration-skip-ink-003`,
`text-decoration-thickness-ink-skip-dilation`.
**Gate.** Those tests at 0%; full css-text-decor section run (allowed as the final
section-completion check) shows 250/250; spot-regression check on css-text, css-inline,
css-writing-modes, css-ruby, css-fonts, CSS2.

---

## Risks & sequencing notes
- **Ruby (Phase 5) is the biggest single unknown** and gates 111 of 154 failures via the reference
  side. If ruby proves larger than one phase, split it: 5a = base+annotation pairing & centering,
  5b = line-box expansion. Do not start Phase 6 before 5 renders annotations in the right place.
- **CJK font coverage**: confirm a CJK fallback is registered before relying on emphasis
  position/style tests that use `試験テスト`; the Ahem-based `text-emphasis-line-height-*` tests are
  the CJK-independent subset to validate Phase 6 first.
- **Cascade change (Phase 1) is high-blast-radius**: removing `text-decoration` from
  `inheritableProperties` touches every text element. Run css-text / css-inline / CSS2 regression
  spot-checks immediately after Phase 1, not just at the end.
- Keep `text_decoration_info.go` a faithful port of `text_decoration_info.cc` — the geometry
  constants (double gap `thickness+1`, line-through at `2/3 ascent − thickness/2`, thickness `auto`
  = `font-size/10`) are load-bearing and must match Blink exactly for 0% diff.

## Key Blink files this plan is grounded in
- `core/style/applied_text_decoration.h` — `AppliedTextDecoration`, `AppliedTextDecorationVector`.
- `core/paint/text_decoration_info.{h,cc}` — `TextDecorationInfo`: `ComputeThickness`,
  `Compute{Underline,Overline,LineThrough}LineData`, `ComputeUnderlineOffset`,
  `OffsetFromDecoratingBox`, `BaselineForInkSkip`, double/wavy geometry.
- `core/paint/text_decoration_painter.{h,cc}` — `TextDecorationPainter`: `Phase{kOriginating,
  kSelection}`, `Begin`/`PaintExceptLineThrough`/`PaintOnlyLineThrough`.
- `core/paint/decoration_line_painter.{h,cc}` — solid/dotted/dashed/double/wavy stroke raster.
- `core/paint/inline_paint_context.h` — `DecoratingBoxList`, decorating-box push/pop/sync.
- `core/paint/text_painter.cc` — paint order (shadow → decorations-except-line-through → text →
  line-through → emphasis), `SetEmphasisMark` / `DrawEmphasisMarks`.
- `core/layout/inline/` ruby column layout + line-box annotation expansion.
