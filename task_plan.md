# Task Plan: css-multicol (active) → fragmentation fixes

## Current focus (2026-04-26)

css-multicol is the active layout-feature track at **188/455** (gate 2026-04-26 post-14b). 267 failing tests remain. The most recent work closed Phase 14a (IFC guard) and 14b (nested leaf-frag defer). Phase 14c (clear-001) is permanently deferred. Next candidates: F2 phase 2 (nested leaf sub-col placement), F3 residuals (column-wrap:nowrap overflow), or the 4 open F4 margin-family regressions.

**Gate invariants:** CSS2 99/99 · flex 626/629 · css-position 92/105 · wm 781/781 · multicol 188/455 · spanner-frag 12/13.

---

## css-multicol Phase 12 — PARTIAL (188/455)

### Completed milestones (one-liners)

| Milestone | Commit | Net | Gate |
|---|---|---|---|
| 12a: Blink-parity fragmentation infra (LayoutLine outer stretch loop) | `2a0d0a07` | +1 | 95/458 |
| 12b: All spanner-fragmentation-* tests | `931f48c5` | +13 | 108/458 |
| 12c: Nested multicol infra (PropagateSpaceShortage, resume-break) | `cccbd05e`+`b0825367` | +22 | 130/458 |
| 12d: Forced-break + break-inside:avoid-column | `6483bc7d` | +2 | 132/458 |
| 12e: max-height-imposes-on-columns | bundled | +1 | 133/458 |
| 12f: column-height/column-wrap (5 cla.cc sites, row-gap) | `35ce3dda` | +6 | 139/458 |
| 12g: balance-break-avoidance (break-appeal propagation) | `287c9fb3` | +3 | 142/458 |
| 12h step 1: Ahem font loader | `356a8b19` | +2 | 144/458 |
| F3a–F3e: row-gap, spanner row-advance, Blink-parity row-snap | multiple | +14 | 158/455 |
| F4: InlineBreakToken resume (item_index OR text_offset) | `617332ae` | +8 net | 166/455 |
| F5: Continuation-row terminal-shortage (list-item-003/004/005) | separate | +3 | 169/455 |
| F2 partial: ClipBlockAxisOnly | separate | +1 | 170/455 |
| F1: @font-face layout-time registration + bidi-level shape segmentation | `41b674ef`+mazzy | wm +2 | 172/455 |
| 12h.6: IFC guard + nested-010 defer + spanner row-gap | 3 commits | +16 | 188/455 |

### Phase 12h.6 — DONE (2026-04-26)

Three commits closing Phase 14a + 14b: IFC fragmentation guard (`87d06be5`), nested multicol leaf-frag defer (inline), HTML tokenizer EOF-recovery. multicol 179→188. Gate: CSS2 99/99 · flex 626/629 · position 92/105 · wm 781/781 · multicol 188/455.

### Open items

**F2 phase 2 (OPEN).** `multicol-nested-010` cluster (~7 tests). Nested multicol leaf is split across inner sub-cols instead of placed only in inner sub-col 1's continuation. Requires Blink research on inner-multicol child-break-token forwarding before coding.

**F3 residuals (OPEN, 19 tests).** Largest: `column-height-013` (6500px), `column-wrap-no-constraints-002` (6000px), `column-height-006` (5250px), nowrap cluster (`-005/-011/-030` ~5000px each). `column-wrap:nowrap` overflow requires a paint-layer change to let overflow columns paint past the declared border-box. `column-height-024` class needs a live-Blink build trace.

**F4 regressions (OPEN, 4 tests).** `multicol-inherit-001`, `multicol-margin-001`, `multicol-margin-child-001`, `multicol-nested-margin-001`. Root: break-before-child should fire when an anon block won't fit the column but currently doesn't, so the IFC emits a mid-text break token. Fix: detect this in outer `block_layout.go` and issue break-before-child.

**`multicol-rule-stacking-001` (OPEN, 32px).** Near-pass; column count now correct. Sub-pixel rule geometry difference remains.

---

## Phase 14: fragmentation fixes

| Sub-phase | Status | Notes |
|---|---|---|
| 14a IFC guard | **DONE** (`87d06be5`) | multicol 179→186; closed 4 F4 regressions |
| 14b nested leaf-frag | **DONE** (2026-04-26) | multicol 186→188; `BlockSizeForFragmentation` signal |
| 14c clear-001 | **PERMANENTLY DEFERRED** | CoreText font metrics mismatch; no targeted fix possible |

---

## css-position (92/105 — effectively complete)

13 pre-existing residuals remain; no active work. Groups:
- 8 G-ABS-IN-TABLE (`position-relative-table-*-absolute-child`) — abspos in positioned table-internals
- 3 G-SEMI-REPLACED (`position-absolute-semi-replaced-stretch-*`) — abspos stretch on button/input/other
- `clear-001.xht` — permanently deferred (see Phase 14c)
- `containing-block-change-scrollframe.html` — needs `Element.scrollTop` JS setter + overflow scroll paint

---

## css-flexbox (626/629 — watch invariant)

Three pre-existing residuals; no active work.

| Test | Diff | Research done | Root cause |
|---|---|---|---|
| `auto-margins-001.html` | ~1024px (0.2%) | Yes (2026-04-21) | VRL cross-axis auto-margin resolution; `getItemAutoMargins` loses item-vs-container WM distinction |
| `content-height-with-scrollbars.html` | ~69200px (14.4%) | Yes (2026-04-21) | `classicScrollbarWidth()` returns 0 for `"auto"`; Blink default is 15px |
| `flexbox-align-self-vert-004.xhtml` | ~3664px (0.8%) | Yes (2026-04-21) | Column-direction baseline synthesis path disagrees between accumulation and placement passes |

---

## Deferred / out-of-scope

| Item | Notes |
|---|---|
| `column-wrap:nowrap` overflow columns | Paint-layer change to allow overflow columns past declared border-box |
| `MulticolBreakTokenData` row-carry | Safe default `consumedRowBlockSize=0` until needed |
| `drawColumnRules` content-area `render.go:2931-2933` | math.Round not migrated to SnapSizeToPixel |
| F1c paint-side shape sharing | `ShapeResult::CopyRange`; needed for non-level-0 cross-span kerning |
| `openFont` signature cleanup | `FontPathToFamilyVariant` + `resolveFamily` path fallback still present |
| G-ABS-IN-TABLE (8 tests) | abspos children of positioned thead/tbody/tfoot/tr |
| G-SEMI-REPLACED (3 tests) | abspos stretch on button/input/other |
| G-SCROLL (1 test) | `Element.scrollTop` JS setter + overflow:hidden scroll paint |
| spanner-fragmentation-005 | Pre-existing residual since Phase 12b |
| Anchor positioning | No WPT tests exercise it |
| StickyPositionScrollingConstraints | Scroll-time wiring deferred until scroll tests appear |

---

## Rules & Discipline

Authoritative sources (re-read at session start):

1. **`/Users/iansmith/louis14/CLAUDE.md`** — foundational correctness, study Blink first, 0% diff required, test execution discipline, operational rules.
2. **`/Users/iansmith/.claude/projects/-Users-iansmith-louis14/memory/MEMORY.md`** — auto-memory index.

**Per-target discipline (CLAUDE.md recap):**
1. Read Blink source BEFORE writing code.
2. Run only the 1–4 driver tests during feature work; gate sweep (all 6 invariants) before each commit.
3. Sub-pixel diffs (even 0.1%) are real bugs — fix at source.
4. If gate sweep regresses: STOP, ROLLBACK, re-read Blink before re-attempting.

## Archived wm work

css-writing-modes is complete (781/781). Planning/findings/progress archived to `docs/plan-wm.md`, `docs/findings-wm.md`, `docs/progress-wm.md`. Do not duplicate here.

## Test command templates

```bash
# Single multicol test
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/<name>' -v

# Full build check
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...

# Gate sweep invariants (run each separately)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTReftests' -v              # CSS2: expect 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-writing-modes'  # expect 781/781
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-flexbox'        # expect >=626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-position'       # expect 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol'       # active target: 188+/455
```
