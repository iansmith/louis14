# HANDOFF — methodical reftest-section improvement

**Written:** 2026-05-14 20:37 UTC · **Branch:** `fix/LOU-114` @ `752e726a`
**Updated:** 2026-05-16 — LOU-128 SVG render-subsystem foundation **LANDED**
(branch `feat/LOU-128-svg-foundation`, commit range `752e726a..61d4f810`, +
Phase 8 delivery commit).

This is the source-of-truth handoff for the in-flight effort to methodically
improve louis14's WPT reftest pass rate, section by section. A fresh session
should read this first. It is **not** the LOU-114 "top 100" ticket — that work
is separate (see `~/.claude/active/LOU-114/`); this effort just shares the
`fix/LOU-114` branch.

## The mission

Pick a CSS section, study how Blink implements it, write a Blink-grounded
phased plan, implement it foundationally (no point fixes), land it
non-regressing. CLAUDE.md principles #1–#4 govern everything.

---

## Current state

### Tickets
- **LOU-114** "top 100" — Backlog/paused; unrelated to this effort, just shares the branch.
- **LOU-115–124** — random-10 sub-issues, done before this effort.
- **LOU-125 / LOU-126 / LOU-127** — created from CodeRabbit PR#5 findings that were skipped (pseudo-element list-item ordinal → needs counter tree; multicol marker `AddToBox` content-push → heavy lift; `drop-shadow()` input alpha → needs a new filters primitive).
- This reftest effort itself is **not ticketed** — it lives as plan docs in `docs/`.

### Reftest totals
- **CSS2: 99/99 (100%).**
- **CSS3: ~3,953 / 6,720.** Last full sweep (at `a2de9e3f`) = 3,952; `+1` from the `contain-animation` fix at `752e726a` (verified; full re-sweep not yet re-run).
- **Net this effort: ≈ +56** vs the `64e204b3` baseline.
- Survey: `docs/reftest-survey-2026-05-14.md` (reflects `a2de9e3f`; CSS3 table sorted worst-first; has a "Changes since baseline" section with gains + regressions). Raw per-test list: `docs/reftest-survey-2026-05-14-raw.txt` (this is the **old/baseline** raw list — useful for diffing).
- Regressions surfaced: `contain-animation-001` → **fixed**; `all-prop-revert-visited` → not a true regression (correct `:visited` fix exposed unimplemented `all: revert`, → `plan-css-cascade`); `counter-style-numeric-001` → minor, **unfixed** (marker/counter-style path).
- Biggest remaining buckets: css-multicol 215, css-transforms 187, css-backgrounds 183, css-color 179, css-fonts 174, css-text-decor 154, css-pseudo 146, filter-effects 140, css-contain 132, css-sizing 109.

### Branches
- **`fix/LOU-114` @ `752e726a`** — pushed; **PR #5** open → `master` (~55 commits: LOU-111 multicol + LOU-114 tooling + this effort's reftest work).
- Landed into it this effort: marker foundation Phases 1–6, css-masking, css-animations, filter-effects, 10 CodeRabbit fixes, the contain fix — all build-clean + regression-verified at each merge.
- **PR #5 CodeRabbit re-review: blocked** — CodeRabbit reported "out of usage credits." The original review (17 inline + summary) predates the fix commits. Resolving this is a user/billing action; deferred.
- **Do not touch:** `fix+LOU-109-marker` (24 commits) and the LOU-54…108 chore branches — a *separate* body of work, not landed, and **LOU-109 collides with the marker foundation** (both rework `::marker`). The css-masking **v1** worktree branch (`worktree-agent-a14ecbe1070ed64a0`, 1 commit) is superseded — prune it. ~15 agent worktrees are now fully landed and prunable.

### Work done vs not done
**Landed (in `fix/LOU-114`):**
- Marker foundation Phases 1–6 (real `::marker` box, `UnpositionedListMarker` carry/claim, inside+outside, vertical WM, `inline list-item`). Phase 7 (`list-style-image` box) deferred — environment-blocked.
- css-masking — clip-path basic shapes + gradient mask-mode (2/8 → 4/8). **Caveat: clip-path tests need the mazarin port to pass at runtime — see gotchas.**
- css-animations — static `@keyframes` engine (6 → 16).
- filter-effects — linearRGB pipeline + Blink-mirrored `FilterEffect` graph in `pkg/graphics/filters/`, CSS filter functions, backdrop-filter (92 → 130). SVG-dependent phases NOT done.

**Plans written, NOT implemented** (all in `docs/`):
- ~~`plan-svg-foundation.md`~~ — **LANDED 2026-05-16** as LOU-128 on `feat/LOU-128-svg-foundation`. Phases 0–7 + Phase 8 delivery; orchestrator-verified at HEAD `61d4f810`. See "What LOU-128 landed" section below.
- `plan-css-ruby.md` — IS the ruby foundation (15 phases, Blink-grounded).
- `plan-css-pseudo.md`, `plan-css-lists.md` — original-batch category plans.
- `plan-css-will-change.md`, `plan-css-cascade.md`, `plan-css-text-decor.md` — second-batch plans.
- `plan-marker-foundation.md` — mostly landed (Phases 1–6); Phase 7 is the only remainder.
- `plan-css-animations.md`, `plan-css-masking.md`, `plan-filter-effects.md` — partially landed; the SVG-gated phases are **now unblocked / partially replaced by LOU-128** (see those plan docs for the surgical trims applied 2026-05-16).

---

## Dependency / sequencing graph

```
SVG foundation (plan-svg-foundation.md)  ← LANDED 2026-05-16 (LOU-128, branch
                                            feat/LOU-128-svg-foundation @ 61d4f810)
   ├─ css-masking SVG phases — DONE (mask-image-1d @ 0; mask-opacity-1d @ 1, in fuzz)
   ├─ filter-effects bucket I (Phase 7) REPLACED in plan-filter-effects.md
   ├─ filter-effects bucket H (Phase 6) element-model REPLACED; FilterRegion +
   │  external-URL fetch remain there
   ├─ filter-effects bucket J (Phase 8) — primitive correctness still owned there;
   │  three carry-over gaps documented (FillPaint/StrokePaint/BackgroundImage builtins
   │  aliased to Source*, content-clipper rect-fallback, color-convention shim)
   └─ css-animations test svg-transform-animation — PASS @ 0

Ruby foundation (plan-css-ruby.md)  ← NOT STARTED
   └─ unblocks css-text-decor's text-emphasis bucket (111 of its 154 fails).
   plan-css-text-decor Phase 5 should be trimmed to *defer to* plan-css-ruby
   rather than re-plan ruby (not yet done).

Marker foundation  ← Phases 1-6 LANDED
   └─ marker-foundation's "Downstream impact" section says plan-css-lists /
      plan-css-pseudo / plan-css-ruby should now be TRIMMED (e.g. css-pseudo's
      49-fail Phase 2 collapses to "verify"). Trims noted, NOT yet applied.
```

**Recommended next move (revised 2026-05-16):** with the SVG foundation landed,
the highest-leverage remaining moves are:
1. `plan-filter-effects.md` Phase 8 (bucket J primitive correctness) + Phase 6
   residue (FilterRegion + external-URL fetch). LOU-128 provides the host; the
   graph correctness is the volume here.
2. `plan-css-ruby.md` — unblocks 111 of 154 css-text-decor fails.
3. Apply marker-foundation downstream trims to `plan-css-lists` / `plan-css-pseudo`
   / `plan-css-ruby` (small).

## What LOU-128 landed — cumulative learnings for downstream foundations

Architecture (mirrors Blink `core/layout/svg/`, `core/paint/svg_*`,
`platform/graphics/filters/`, `core/svg/`):

- **`pkg/geometry/affine.go`** — `AffineTransform` (2×3), `PointF`/`SizeF`/`RectF`.
- **`pkg/layout/svg/`** — `svg_root.go` (`SVGRoot` + `BuildSVGRoot` +
  `buildSVGTreeWithResources`), `svg_node.go` (`SVGNode` interface — `.Paint(ctx)`
  is a no-op everywhere; the render-side painter walks the tree directly),
  `svg_length_context.go`, `viewbox.go` (`ParseViewBox`, `ParsePreserveAspectRatio`,
  `BuildViewBoxToViewportTransform`), `svg_shape.go`, `svg_path.go` (full
  `M m L l H h V v C c S s Q q T t A a Z z` parser), `svg_container.go` (`<g>` +
  nested `<svg>`), `transform_helper.go` (`ParseSVGTransform`,
  `ParseCSSTransformForSVG`), `svg_resource.go` + `svg_resource_registry.go`
  (single `map[string]SVGResource` + per-kind lookup wrappers),
  `svg_resources_cycle_solver.go` (DFS, 4-state),
  `svg_resource_{paint_server,clipper,masker,filter}.go`.
- **`pkg/layout/svg_root_algorithm.go`** — `SVGRootAlgorithm` + `IsInlineSVG`
  (only outermost `<svg>` gets a `LayoutInputNode.SVGRoot`).
- **`pkg/layout/layout_input_node.go:123`** — `SVGRoot any` field.
- **Dispatch seam** `pkg/layout/block_layout.go:2504-2513` — `layoutElement`
  routes inline `<svg>` to `SVGRootAlgorithm`. (`ReplacedLayoutAlgorithm` is dead
  code for inline `<svg>` once LOU-128 landed; `<svg>` still uses replaced-element
  sizing but layout is owned by `SVGRootAlgorithm`.)
- **`pkg/graphics/filters/`** (already existed; LOU-128 added three files):
  `svg_filter_builder.go` (`BuildGraph` + `getEffectByID` +
  `ResolveInterpolationSpace`), `fe_blend.go` (normal/multiply/screen/darken/
  lighten), `fe_subregion_clip.go` (generic per-primitive subregion-clip wrapper).
- **`pkg/css/style.go`** — `SVGPaint` + `SVGPaint{Color,None,Server}Kind`,
  `ClipPathReference`, `ReferenceFilterOperation`, `ParseURLReference`, SVG-default
  accessors (`GetFill`, `GetStroke`, `GetFillRule`, `GetStrokeWidth`,
  `GetFillOpacity`, `GetStrokeOpacity`, `GetStrokeLinecap`, `GetStrokeLinejoin`),
  `color-interpolation-filters`, `flood-color`, `flood-opacity`.
- **`pkg/css/cascade.go:1508`** — `applyPresentationalAttributes` extended with
  SVG presentation attrs (unconditional on every element).
- **`pkg/render/`** — `svg_root_painter.go`, `svg_container_painter.go`,
  `svg_shape_painter.go`, `svg_object_painter.go`, `svg_paint_context.go`,
  `svg_paint_server*.go`, `svg_clip_painter.go`, `svg_mask_painter.go`,
  `svg_filter_painter.go`, `svg_filter_adapter.go`. Renderer carries
  `svgResources *SVGResourceRegistry`; per-frame `collectSVGResources` +
  `SolveResourceCycles`.
- **`pkg/render/render.go`** — `paintSelfForeground` (entry from CSS paint flow,
  approx line 1407 in HEAD `61d4f810`); `paintLayerWithMask` (approx line 621,
  with SVG fast-path at top).
- **`pkg/render/filter_effect_builder.go`** — `BuildReferenceFilter` for
  `filter: url(#id)`.

### Known limitations (LOU-128 carry-over — to be aware of for future work)

- **Color-convention compatibility shim** in
  `pkg/render/svg_mask_painter.go::compositeBufferWithOpacityOnto` reproduces
  louis14's project-wide straight-alpha `color.RGBA` convention. `mask-opacity-1d`
  passes at max diff 1 within authored fuzz `0-5 / 0-1000`. Project-wide
  color-convention cleanup is a future ticket — do not "fix" the shim in isolation.
- **`FillPaint`/`StrokePaint`/`BackgroundImage` SVG filter builtins** aliased to
  `SourceGraphic`/`SourceAlpha` in `pkg/graphics/filters/svg_filter_builder.go`.
  Proper implementations belong in filter-effects bucket J.
- **Content-based clipper fallback** in `pkg/render/svg_clip_painter.go` uses a
  bounding-rect approximation because mazarin lacks a generic alpha-mask clip.
- **`mask` CSS shorthand parser** is minimal; `GetMaskImage` falls back to the
  `mask` shorthand string. The natural fix lives in `plan-css-masking.md` Phase 3
  residue.
- **`SVGResourceReference` type** exists in `pkg/layout/svg/svg_resource.go` but
  is not yet plumbed at call sites — intentional clean follow-up.
- **Mazarin gaps:** `LineJoinMiter` falls back to Bevel; `stroke-dashoffset`
  parsed but not applied. These are mazarin-side, not louis14.

### Pre-existing failures confirmed unaffected by LOU-128

- `TestBlockLayout_FloatLeft` (Phase 2-era flaky / unrelated)
- `TestErrorRecovery_UnclosedBlocks`, `TestParseInlineStyle_BorderShorthand`
- `css-position/clear-001.xht` (small clear-handling diff, pre-existing)
- `css-backgrounds/background-size-043.html`, `-044.html` (pre-existing
  background-positioning failures; oksvg path itself confirmed intact by
  `css-overflow/overflow-img-svg.html` PASS @ 0)
- Filter-effects bucket-I `svg-feimage-001/feoffset-001/multiple-filter-functions`
  (pre-existing primitive-correctness gaps — bucket J residue, not LOU-128 work)

---

## How implementation has been run (the working pattern)

- One worktree agent per plan/foundation, `isolation: worktree`, `run_in_background`.
- Agent reads its plan from the **main repo absolute path** (`/Users/iansmith/louis14/docs/plan-*.md`) — plans are untracked, so worktrees don't have them; absolute-path read works.
- Agent rules (used verbatim all session): study Blink first; foundational-correctness (STOP rather than commit a regressing phase); commit + report per phase; **git ops only on the agent's own worktree branch, never touch `~/louis14`**; fonts symlink before any reftest (`rm -rf fonts && ln -sfn /Users/iansmith/louis14/fonts fonts`); gofmt only the files you edit (the repo has pre-existing gofmt drift — package-wide `go fmt` sweeps it in).
- Landing is the orchestrator's job, from `~/louis14`: `git merge` the worktree branch, resolve conflicts, build, targeted-test, commit. Each landing was build-verified + targeted-regression-swept before the next.
- When two plans collide on a shared subsystem (happened with marker, SVG, ruby, and `pkg/css/animation.go`): pull the shared piece into a *foundation* plan, land it first, then the dependents build on it ("Option C").

---

## Gotchas that WILL bite a fresh session

1. **mazarin cross-repo dependency.** css-masking's clip-path basic shapes need a clip-as-mask change to `mazarin/textshape`'s `draw_impl.go`. That repo is `~/mazzy` (separate git repo, active use). The change was made in a `.local-mazarin/` copy inside the css-masking agent's worktree (gitignored, go.mod redirected, **not committed**) — a handoff diff was produced. **It still needs porting into `~/mazzy`.** Until then, clip-path tests build but don't pass at runtime on `fix/LOU-114`.
2. **go.mod `replace` is a relative path** (`../mazzy/mazarin/textshape`) — only resolves if the worktree is a sibling of `~/mazzy`. Temp worktrees for verification must go at `/Users/iansmith/louis14-*`, NOT `/tmp/*`.
3. **`docs/` is not gitignored but the convention is to leave plan/working docs untracked** — all 11 `plan-*.md`, the survey, and this handoff are untracked. They survive context-clear (on disk) and `git checkout` (untracked), but not `git clean`. Don't commit them unless asked.
4. **`CLAUDE.md` has a pre-existing uncommitted modification** unrelated to any of this — leave it; don't commit it.
5. **CodeRabbit re-review is credit-blocked** — don't burn time waiting on it.
6. **Foreign branches** `fix+LOU-109-marker` etc. are unrelated work; LOU-109 collides with the marker foundation. Don't merge them into `fix/LOU-114` without a deliberate reconciliation.
7. Test discipline: full reftest sweep is `cd pkg/visualtest && GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go test -run 'TestWPTReftests|TestWPTCSS3Reftests' -v -timeout 90m` — only run it when explicitly getting fresh totals; otherwise run the 1–4 tests for the feature.

---

## Open loose ends (small)

- `counter-style-numeric-001` regression — minor, unfixed (marker/counter-style path; `plan-css-lists` B5 territory).
- Apply the marker-foundation downstream trims to `plan-css-lists` / `plan-css-pseudo` / `plan-css-ruby`.
- Trim `plan-css-text-decor` Phase 5 to defer to `plan-css-ruby`.
- Prune the superseded css-masking v1 worktree + the landed agent worktrees.
- Re-run the full reftest sweep after the next landing to refresh `docs/reftest-survey-2026-05-14.md` — LOU-128 added ≥5 new passes (the 5 LOU-128 SVG gate tests previously failing).
- **LOU-128 follow-ups (small, optional):**
  - Plumb `SVGResourceReference` at lookup sites in `pkg/render/svg_*_painter.go`.
  - Replace `FillPaint`/`StrokePaint`/`BackgroundImage` alias-to-`Source*` in
    `pkg/graphics/filters/svg_filter_builder.go::getEffectByID` (belongs in
    filter-effects bucket J).
  - Add a real `mask` CSS shorthand parser so `GetMaskImage` doesn't fall back to
    the shorthand string (belongs in css-masking Phase 3 residue).
  - Address mazarin `LineJoinMiter`/`stroke-dashoffset` in `~/mazzy` and remove
    louis14's Bevel-fallback workaround.
