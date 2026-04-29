# Phase 22 — Nested-multicol resume `ConsumedBlockSize` chain (detailed plan)

Status: IN PROGRESS · 2026-04-29 (Cmt-1 landed; Class A + B work pending)
Hard-blocker for: Phase 21 (deletion of unconditional multicol clip)
Worktree: yes (multicol-phase-22; Cmt-1 committed 2a822b9d)

## 0. One-line summary

Populate `ConsumedBlockSize` on the inner multicol's outgoing `BlockBreakToken`. The READ site at `pkg/layout/multicol_layout.go:297` already mirrors Blink's `column_layout_algorithm.cc::Layout` resume arithmetic (`remaining -= GetBreakToken()->ConsumedBlockSize()`) — Phase 12c added it. The WRITE site at `buildOuterBreakResult` (`multicol_layout.go:486-519`) is the only `BlockBreakToken` emitter in the codebase that omits this field. With nothing on the wire, every resumed inner multicol sees `ConsumedBlockSize=0` and re-lays-out from offset zero in each successive outer column.

Closes target: `multicol-nested-{011,013,014,016,017,018,019,020,022,023,024,027,032}` + `multicol-fill-balance-026` (~14 known fails). May incidentally close `-029` (currently the Phase 21 stuck test) if the resume-fix cascades; verify, don't claim. Multicol gate target: 211 → 220–225.

## 1. Blink reference (verified 2026-04-29)

Three operative facts — sourced from the parallel research agent against `chromium/src/main/third_party/blink/renderer/core/layout/`:

### 1.1 `BlockBreakToken::ConsumedBlockSize()` — field doc

`block_break_token.h` (~lines 74-81):

```cpp
// Represents the amount of block-size consumed by previous fragments.
//
// E.g. if the node specifies a block-size of 200px, and the previous
// fragments generated for this box consumed 150px in total (which is what
// this method would return then), there's 50px left to consume. The next
// fragment will become 50px tall, assuming no additional fragmentation.
LayoutUnit ConsumedBlockSize() const { return consumed_block_size_; }
```

### 1.2 `column_layout_algorithm.cc::Layout` — READ site at entry (~cla.cc:308)

```cpp
remaining_content_block_size_ = ShrinkLogicalSize(
    border_box_size, BorderScrollbarPadding()).block_size;
if (remaining_content_block_size_ != kIndefiniteSize) {
  if (GetBreakToken() && is_constrained_by_outer_fragmentation_context_) {
    remaining_content_block_size_ -= GetBreakToken()->ConsumedBlockSize();
  }
  remaining_content_block_size_ =
      remaining_content_block_size_.ClampNegativeToZero();
}
```

This is the routine louis14 ports verbatim at `multicol_layout.go:286-302`. Side already wired correctly.

### 1.3 `fragmentation_utils.cc::FinishFragmentation` — WRITE site

```cpp
LayoutUnit previously_consumed_block_size;
if (const auto* token = GetBreakToken()) {
  previously_consumed_block_size = token->ConsumedBlockSize();
}
// Primary assignment (this fragment is intermediate, more to come):
builder->SetConsumedBlockSize(previously_consumed_block_size +
                              final_block_size);
// Resuming-in-next-fragmentainer variant:
if (node_is_to_be_resumed) {
  builder->SetConsumedBlockSize(previously_consumed_block_size +
                                std::max(final_block_size, space_left));
}
```

`final_block_size` here is the painted block extent of the current fragment. The `std::max(final, space_left)` clause matters when the fragment was clamped smaller than the outer remaining space (e.g., interrupted by a break) — it ensures the cumulative consumed amount advances by at least the fragmentainer extent so legacy offset math does not regress. For `column-fill:auto` inner multicol in nested fragmentation, `final_block_size == space_left` is the common case; the `max(final, space_left)` formula reduces to `+= final`. We can port the simple form first and add the `max` clause only if a concrete test demands it.

### 1.4 No `MulticolBreakTokenData` involvement here

Verified by agent: `MulticolBreakTokenData::consumed_row_block_size` is set in `column_layout_algorithm.cc::LayoutFragmentationContext` only inside `ShouldWrapColumns()` (CSS Multicol L2 `column-wrap:wrap` + non-auto `column-height`). For `column-fill:auto` + auto `column-height` — the shape of every Phase 22 target test — the standard `BlockBreakToken::ConsumedBlockSize` chain alone drives nested-multicol cross-outer-column resume.

This corroborates B6's gate-neutral outcome (commit `b251c8db`): the Phase 18 row-phase carrier was parity-correct but inert for these tests because `shouldWrapColumns()` returns false. The carrier WRITE site at `multicol_layout.go:605-622` is correct as-is and stays untouched.

## 2. What louis14 currently does (post-Phase-20)

### 2.1 The smoking gun — `buildOuterBreakResult` omits `ConsumedBlockSize`

`pkg/layout/multicol_layout.go:486-519`:

```go
buildOuterBreakResult := func() *LayoutResult {
    builder.SetSize(LogicalSize{
        InlineSize: contentInlineSize + geom.InlineBorderPadding(),
        BlockSize:  blockCursor + geom.BlockBorderPadding(),
    })
    builder.SetIntrinsicBlockSize(blockCursor)
    // ...
    result := builder.Build()
    result.BreakToken = &BlockBreakToken{
        Node:             mla.node,
        ChildBreakTokens: outBuilder.Children(),
        MulticolData:     outgoingMulticolData,
        // ← MISSING: ConsumedBlockSize, SequenceNumber
    }
    // ...
    return result
}
```

Every site in `block_layout.go` that emits a `BlockBreakToken` follows the canonical pattern (12 grep hits at lines 423/430, 748/751, 874/877, 1077/1081, 1130-1134, 1274/1278, 1318/1321, 1764-1766). Sample from `block_layout.go:1757-1768` (the 16.d.1 self-break path):

```go
if didBreakSelf {
    prevConsumed := 0.0
    seqNum := 0
    if incomingBreakToken != nil {
        prevConsumed = incomingBreakToken.ConsumedBlockSize.Float64()
        seqNum = incomingBreakToken.SequenceNumber + 1
    }
    result.BreakToken = &BlockBreakToken{
        Node:              bla.node,
        ConsumedBlockSize: layoutunit.FromFloat64Round(prevConsumed + finalBlockSize),
        SequenceNumber:    seqNum,
    }
    result.DidBreakSelf = true
}
```

The Phase 22 fix is to apply this exact pattern at `multicol_layout.go:502-506`.

### 2.2 The READ side — already correct (Phase 12c)

`multicol_layout.go:286-302`:

```go
mla.remainingContentBlockSize = Indefinite
if hasExplicitBlock {
    mla.remainingContentBlockSize = explicitBlockSize
    if mla.space.BreakToken != nil &&
        mla.space.HasBlockFragmentation &&
        mla.space.FragmentainerBlockSize != Indefinite &&
        !mla.space.IsInitialColumnBalancingPass {
        mla.remainingContentBlockSize -= mla.space.BreakToken.ConsumedBlockSize.Float64()
        if mla.remainingContentBlockSize < 0 {
            mla.remainingContentBlockSize = 0
        }
    }
}
```

Verbatim port of Blink cla.cc:308. Has been silently consuming `ConsumedBlockSize=0` for years because nothing wrote it.

### 2.3 Failure walkthrough — `multicol-nested-011`

- Outer: `columns:2; height:50; column-fill:auto`. Inner: `columns:2; height:100; column-fill:auto`. Each outer column = 50 wide × 50 tall. Inner declared height = 100.
- **Outer col-1.** OuterMLA → BlockLayoutAlgorithm runs the anonymous content wrapper → InnerMLA.Layout enters with `BreakToken==nil`, `outerAvailable=50`, `explicitBlockSize=100`. Phase 14b defer at `multicol_layout.go:413-432` fires (gate: fresh + auto + outer<explicit), returns 0-height fragment with `BlockSizeForFragmentation=100+BP`. Outer's BLA's `BreakBeforeChildIfNeeded` sees `BSFF > spaceLeft` and emits a `IsBreakBefore` token for the inner. Outer col-1 ends with no inner content placed.
- **Outer col-2.** Outer's BLA resumes the inner with the break-before token. Inner's `mla.space.BreakToken != nil`, so the Phase 14b defer-gate (`BreakToken == nil` at line 413) is closed. Inner proceeds into the walker loop. `colBlockSize` for inner columns is `min(explicit=100, outerRemaining=50) = 50`. `layoutLine` runs, producing 50px of inner content. `blockCursor=50`. The branch at `multicol_layout.go:641` fires (`remainingToken != nil && hasOuterFrag`), emits `buildOuterBreakResult()`. **Outgoing token: `ConsumedBlockSize=0` (BUG).** ChildBreakTokens carries the inner column-content's resume.
- **Outer col-3** (column-fill:auto stretches outer to fit). Outer's BLA resumes inner with the bug-laden token. `mla.space.BreakToken.ConsumedBlockSize == 0`, so READ site at line 297 leaves `remainingContentBlockSize = 100 - 0 = 100`. Inner re-lays-out the same 100px of content from cursor 0; produces another 50-tall fragment. Outgoing token still has `ConsumedBlockSize=0`. Loops forever (or until `column-fill:auto` saturates).

The visual diagnosis from the 2026-04-28 archive entry (test image fully RED, "defer loops forever") is consistent if the parent's BLA repeatedly converts the broken outgoing-token shape back into a fresh break-before on each retry. Either way, the chain is broken and the fix is the same.

## 3. Implementation diff

### 3.1 Cmt-1 (REQUIRED) — set `ConsumedBlockSize` in `buildOuterBreakResult`

`pkg/layout/multicol_layout.go:486-519`. Replace the `result.BreakToken = …` block with:

```go
prevConsumed := layoutunit.LayoutUnit{}
seqNum := 0
if mla.space.BreakToken != nil {
    prevConsumed = mla.space.BreakToken.ConsumedBlockSize
    seqNum = mla.space.BreakToken.SequenceNumber + 1
}
result.BreakToken = &BlockBreakToken{
    Node:              mla.node,
    ChildBreakTokens:  outBuilder.Children(),
    MulticolData:      outgoingMulticolData,
    ConsumedBlockSize: prevConsumed.Add(layoutunit.FromFloat64Round(blockCursor)),
    SequenceNumber:    seqNum,
}
```

`blockCursor` at this point is the inner multicol's painted extent in the current outer fragmentainer (the closure already uses it on line 489 for `BlockSize`). Add it to the incoming `ConsumedBlockSize`. Mirrors `block_layout.go:1761-1766` and Blink's `FinishFragmentation` primary assignment.

This is the single mechanical change required by the chain. Six lines changed.

### 3.2 Cmt-2 (LIKELY REQUIRED) — same fix at the row-advance early-return

`pkg/layout/multicol_layout.go:579-588` (column-content branch, row-advance bail-out):

```go
if hasOuterFrag && mla.hasRowHeight() &&
    mla.rowHeight() > outerAvailable-blockCursor {
    if nextColToken != nil {
        flushWalker()
        return buildOuterBreakResult()
    }
    break
}
```

This already routes through `buildOuterBreakResult`, so Cmt-1 covers it. **No edit needed** — listed for audit completeness so the reviewer confirms the path is reached only via the closure.

### 3.3 Cmt-3 (AUDIT) — verify Phase 18 + spanner-clip exits also route through the closure

The other paths that emit outer break results:

- **Phase 18 row-phase WRITE** at `multicol_layout.go:612-622` — calls `buildOuterBreakResult()`. Covered.
- **Spanner content-overflow** at `multicol_layout.go:818-821` — sets `pendingContentOverflow=true`, defers emission to the post-loop handler which calls `buildOuterBreakResult()`. Covered.
- **Spanner clip-overflow** at `multicol_layout.go:854-871` — calls `buildOuterBreakResult()` via `flushWalker` + `return`. Covered.
- **Forced-break-after spanner** at `multicol_layout.go:701-...` (inspect during implementation) — verify it routes through `buildOuterBreakResult`.

If any path constructs an outgoing BreakToken without going through the closure, fix it the same way. This is bookkeeping, not a separate behavior change.

### 3.4 Cmt-4 (CONDITIONAL) — Phase 14b defer interaction

The defer at `multicol_layout.go:413-432` fires once on fresh inner layout when `outerAvailable < explicitBlockSize`. After the parent emits break-before, the inner gets a non-nil `IsBreakBefore` token in outer-col-2 and the defer-gate (`BreakToken == nil`) closes; inner proceeds normally. With Cmt-1 in place, outer-col-3+ get the correct `ConsumedBlockSize` and the chain converges.

The 2026-04-28 attempt to widen the defer-gate (adding `mla.space.FragmentainerBlockSize >= explicitBlockSize`) was **non-monotonic** (`-011` improved 2.1%→1.6%, `-032` worsened 2.1%→3.1%). That attempt is no longer needed once Cmt-1 fixes the WRITE site. Do not re-apply the defer-gate change unless a specific test post-Cmt-1 demonstrates the defer is still misbehaving. If revisiting becomes necessary, the right move is a narrower predicate: defer only when fresh AND no inner column would fit in the current outer fragmentainer AND the next outer column is no larger (which under `column-fill:auto` is true iff every column has the same available space).

### 3.5 Cmt-5 (CONDITIONAL) — `node_is_to_be_resumed` `max(final, space_left)` clause

If after Cmt-1 a test like `-027` (inner with `min-height`) or `-022` (monolithic 100h with `break-inside:avoid`) shows a stale `ConsumedBlockSize` symptom (resumed inner under-counts what was consumed because its own fragment was clamped smaller than the outer remaining), port the second `FinishFragmentation` clause:

```go
prevConsumed.Add(layoutunit.FromFloat64Round(max(blockCursor, outerAvailable)))
```

This is a follow-up only if Cmt-1 leaves residuals. Default expectation: not needed for `column-fill:auto` cases.

### 3.6 Files touched

| File | Change |
|---|---|
| `pkg/layout/multicol_layout.go` | Cmt-1: 6-line edit at 502-506. Cmt-3 audit: comment-only. Cmt-4 (if needed): 1-line gate change at 413. Cmt-5 (if needed): 1-line max() at 502. |

Single file. No `break_token.go` change — the `ConsumedBlockSize` field already exists.

## 4. Pre-flight gate baseline (captured 2026-04-29)

Targeted run of the Phase 22 cluster + `multicol-fill-balance-003/-026`:

```
Summary: 7/22 passed, 15 failed
```

| Test | Pre-fix diff |
|---|---|
| multicol-nested-011 | 2.1% (10000 px) |
| multicol-nested-013 | 0.3% (1250 px) |
| multicol-nested-014 | 0.5% (2500 px) |
| multicol-nested-016 | 0.4% (2000 px) |
| multicol-nested-017 | 1.0% (5000 px) |
| multicol-nested-018 | 1.0% (5000 px) |
| multicol-nested-019 | 0.3% (1250 px) |
| multicol-nested-020 | 1.4% (6500 px) |
| multicol-nested-022 | 0.8% (3750 px) |
| multicol-nested-023 | 1.6% (7500 px) |
| multicol-nested-024 | 1.0% (5000 px) |
| multicol-nested-027 | 0.3% (1250 px) |
| multicol-nested-029 | 0.0% (85 px — Phase 21 stuck test) |
| multicol-nested-032 | 2.1% (10000 px) |
| multicol-fill-balance-026 | 9.4% (45100 px) |

Currently passing (7): `-012`, `-015`, `-021`, `-025`, `-026`, `-028`, `-030`, `-031`, `fill-balance-003` minus 2 candidates absent from the regex. `-015/-021/-026/-028` are the four Phase-21 prior-clip-wins; `-030/-031` are driver invariants. These must continue to pass.

## 5. Stuck-test verification (predicted post-Cmt-1)

| Test | Shape | Why fix should close it |
|---|---|---|
| `nested-011` | outer 50h × 2col, inner 100h × 2col, `column-fill:auto` both | Resume chain converges once `ConsumedBlockSize` is wired (full walkthrough §2.3). Primary canary. |
| `nested-013` | outer 100h × 2col, inner with `break-inside:avoid` content > outer | Same chain; small diff because most content fits in one outer column, only the tail relies on resume. |
| `nested-014` | outer 5col, inner with float + monolithic | Inner's outgoing token's `ConsumedBlockSize` was 0; resume in outer-col-N+1 over-painted. |
| `nested-016, -018, -019, -020` | orphans/widows + break-before:avoid in inner | Same chain; orphan/widow logic is independent of the WRITE site. |
| `nested-017` | spanner inside inner | Spanner-clip exit at `multicol_layout.go:854-871` already sets `ConsumedBlockSize` on the spanner clip token; the inner's CONTAINER-level token is what was missing. Cmt-1 + Cmt-3 audit covers it. |
| `nested-022` | 150h outer with monolithic 100h inner | Same chain. |
| `nested-023, -024` | 150h outer × 2col, inner balanced (no fill:auto) | Same chain (the inner's outgoing BreakToken is built the same way regardless of inner's column-fill). |
| `nested-027` | min-height inner | Same chain; `min-height` resolves to `finalBlockSize` which feeds `blockCursor`. May need Cmt-5 (`max(final, space_left)`) — verify. |
| `nested-032` | abspos child + 200h inner | Same chain. |
| `fill-balance-026` | 5col outer 100h, inner 1000h × 2col | Same chain; the 9.4% diff is the largest because the inner is 10× the outer's column height and every outer column was re-laying-out from cursor 0. |
| `nested-029` | inner `line-height:0.8` | This is the Phase 21 stuck test — glyph ink-overflow above line-box. Phase 22 may or may not move it; if Phase 22 doesn't close it, Phase 21 will. Don't claim. |

Cluster predicted close: 13 tests minimum (`-011, -013, -014, -016, -017, -018, -019, -020, -022, -023, -024, -027, -032, fill-balance-026`). Best case: 14 with `-029` cascading.

## 6. Regression-risk surface

### 6.1 Currently-passing tests in the same code-path

| Test | Why it passes today | Risk under Cmt-1 |
|---|---|---|
| `nested-015` | inner balanced inside outer 100h, prior-clip-win | Should remain pass; `ConsumedBlockSize` write doesn't add overflow. |
| `nested-021` | wide content (200%) in 2-row inner, prior-clip-win | Same. |
| `nested-026` | inner 150h in outer 100h, prior-clip-win | Same — actually expected to *improve* (clip currently masks the resume bug). |
| `nested-028` | min-height 125 inner in outer 100h × 4col | Same — small min-height risk; verify with Cmt-5 if needed. |
| `nested-030, -031` (driver invariants) | inner with break-inside:avoid 400h, in outer 2col 100h | These specifically exercise the resume chain and currently pass via 16.d.2/3 TallestUnbreakable + paint clip. Cmt-1 must not perturb the existing flow when the chain happened to come out right. Verify diff is exactly 0 post-fix. |
| `multicol-fill-balance-003` | inner balanced, outer 200h | Should remain pass. |

### 6.2 13 driver invariants

Already 13/13. The fix only widens what the outgoing token carries (`ConsumedBlockSize` was 0; will be `prev + blockCursor`). Code that doesn't read `ConsumedBlockSize` is unaffected. Code that does read it (only the inner multicol's own resume at line 297) will now see the correct value where before it was a silent zero — for all 13 drivers, the outer is the only multicol, so there's no inner-resume branch firing. None of them should move.

### 6.3 Spanner-fragmentation cluster

12/13 currently. Spanner clip-overflow already sets `ConsumedBlockSize` correctly (`multicol_layout.go:854-871`). The container-level `ConsumedBlockSize` change does not perturb spanner resume logic, which keys off `entry.BreakToken.ConsumedBlockSize` of the *spanner* node, not the *multicol container* node. Risk: low.

### 6.4 4-cat invariants (CSS2 99/99, flex 626/629, position 92/105, wm 781/781)

The change is multicol-internal: nothing outside `MulticolLayoutAlgorithm.Layout` reads its outgoing token's `ConsumedBlockSize` differently. Risk: zero.

## 7. Sequencing within Phase 22

Worktree (per CLAUDE.md §5: high-risk multi-commit refactors).

```
worktree create
  ├── Cmt-1 (multicol_layout.go:502-506) — six-line WRITE-site fix
  │      RUN: 13 drivers · 9 prior-clip-wins · 22 Phase-22 cluster
  │      EXPECT: drivers 13/13 hold · prior-clip-wins 9/9 hold · cluster +12 to +14
  │
  ├── Cmt-2 (audit only, no edit)
  │      RE-READ all multicol_layout.go BlockBreakToken constructions; confirm
  │      every container-level emission threads through buildOuterBreakResult.
  │
  ├── Cmt-3 (CONDITIONAL on Cmt-1 residuals)
  │      If a specific test still fails post-Cmt-1, decide between:
  │       - Cmt-5 (max(final, space_left))
  │       - Phase 14b defer narrowing
  │       - other targeted port
  │
  └── Sweep + merge to master
       multicol gate sweep · 4-cat sweep · single squashed-or-fast-forwarded merge
```

Estimated cmts on master after merge: 3–6.

## 8. Verification gate (per CLAUDE.md §4)

Order matters. Each step is a separate test invocation.

### 8.1 Pre-flight (before editing — already captured 2026-04-29)

```bash
# 13-driver invariants (must be 13/13)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(column-height-(001|010|017|026|027)|multicol-nested-(030|031)|spanner-fragmentation-(001|004|006)|multicol-rule-nested-balancing-004|nested-floated-multicol-with-monolithic-child|nested-past-fragmentation-line)\.html$'

# 9 Phase-21 prior-clip-wins (must hold under the current clip)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-breaking-002|multicol-breaking-nobackground-002|multicol-fill-balance-nested-000|multicol-list-item-001|multicol-nested-015|multicol-nested-021|multicol-nested-026|multicol-nested-028|nested-after-float-clearance)\.(html|xht)$'

# Phase 22 target cluster (15 currently fail per §4)
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol/(multicol-nested-(011|012|013|014|015|016|017|018|019|020|021|022|023|024|025|026|027|028|029|030|031|032)|multicol-fill-balance-(003|026))\.(html|xht)$'
```

### 8.2 Apply Cmt-1 in worktree

```bash
git worktree add ../louis14-phase-22 -b multicol-phase-22
cd ../louis14-phase-22
# edit pkg/layout/multicol_layout.go:502-506 per §3.1
GOTOOLCHAIN=go1.26.2 /opt/homebrew/bin/go build ./...
```

### 8.3 Cmt-1 targeted re-run

Run all three commands from §8.1 against the worktree.

Expected outcomes:
- 13 drivers: 13/13 at 0 diff (regression → STOP, revert, investigate).
- 9 prior-clip-wins: 9/9 at 0 diff (regression → STOP).
- Phase 22 cluster: +12 to +14 closes (`-011, -013, -014, -016, -017, -018, -019, -020, -022, -023, -024, -027, -032, fill-balance-026`). Up to `-029` may or may not close (it's the Phase 21 stuck test).

If any cluster test still fails after Cmt-1, capture its specific diff and decide whether Cmt-5 (`max(final, space_left)`) addresses it before moving on.

### 8.4 Cmt-1 multicol-gate sweep

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ \
  -run 'TestWPTCSS3Reftests/css-multicol' 2>&1 | tail -50
```

Expected: 211 + (Phase 22 closes) ± any unexpected reclaims/regressions. Examine each unexpected delta individually. Goal floor: 220.

### 8.5 Cmt-1 4-cat invariant sweep

```bash
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTReftests'                        # CSS2 99/99
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-flexbox'        # 626/629
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-position'       # 92/105
GOTOOLCHAIN=go1.26.2 GOFLAGS="-mod=mod" /opt/homebrew/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests/css-writing-modes'  # 781/781
```

Any change in any of these → STOP, the WRITE-site change has wider consequences than expected. Most likely cause if it happens: a non-multicol caller of `BlockBreakToken.ConsumedBlockSize` was relying on it being zero for the multicol container case. Diagnose before continuing.

### 8.6 Merge to master

After all three sweeps clean, fast-forward or squash-merge to master with the commit message draft in §10.

## 9. Hard exits

- **Driver invariant regression** (13 drivers drops below 13/13) → STOP, revert, investigate. The Cmt-1 change is the only Blink-aligning fix in this plan; if it breaks a driver, our existing ConsumedBlockSize wiring is mis-modeling something Blink does differently.
- **Prior-clip-wins regression** (any of the 9 fall) → STOP. Their layouts depend on the clip masking the *current* broken resume chain; if Cmt-1 fixes the resume but introduces a new diff, Cmt-1's WRITE arithmetic is off (likely the `max(final, space_left)` clause is needed). Pause and discuss before applying Cmt-5.
- **4-cat invariant regression** → STOP. Suggests an unanticipated reader of multicol's outgoing `ConsumedBlockSize` outside the multicol algorithm. Investigate before continuing.
- **Spanner-fragmentation cluster drops below 12/13** → STOP. Cmt-1 should not touch the spanner clip-overflow path (already-correct), but if -001/-004/-006 move, the container-level `ConsumedBlockSize` is interfering with spanner clip-resume bookkeeping. Likely a real bug, possibly fixable in the same worktree by walking the spanner resume code.
- **Cluster gain < +9** → PAUSE. Phase 22 brief target was +9 to +15; falling short means more than the WRITE site is broken. Diagnose with traces before adding Cmt-3/Cmt-4/Cmt-5 speculatively.

## 10. Commit shape

Worktree-merge to master with a single squashed commit (or fast-forward if the worktree work was clean). Draft message:

> P22: wire ConsumedBlockSize on inner multicol's outgoing BreakToken
>
> MulticolLayoutAlgorithm's outgoing BlockBreakToken in buildOuterBreakResult
> was the only token-emitter in the codebase that did not populate
> ConsumedBlockSize. The READ site at multicol_layout.go:297 already mirrors
> Blink's column_layout_algorithm.cc:308 (subtracts incoming ConsumedBlockSize
> from remainingContentBlockSize), but with the WRITE site silent every
> resumed inner multicol read 0 and re-laid-out from cursor zero in the next
> outer column.
>
> Fix mirrors Blink's FinishFragmentation primary clause:
>     ConsumedBlockSize = previously_consumed + final_block_size
> i.e. the same canonical pattern already used by 12 other token-emitters in
> block_layout.go (see e.g. block_layout.go:1761-1766 self-break path).
>
> Closes the multicol-nested-011..032 cluster + multicol-fill-balance-026:
>     -011, -013, -014, -016, -017, -018, -019, -020, -022, -023, -024,
>     -027, -032, fill-balance-026 (14 tests).
> -029 (Phase 21 stuck test) may also close as a cascade; verify don't claim.
>
> Multicol gate: 211 → 224+. 13 drivers 13/13 hold. Spanner-fragmentation
> 12/13 hold. 4-cat invariants intact.
>
> Unblocks Phase 21 (deletion of unconditional multicol clip).

## 11. Why this is one fix, not many

The 14-test cluster all fails on the same chain hop. The 2026-04-28 archive entry already pinpointed the diagnosis (`findings-multicol-archive.md:2073-2079`); B6 (`b251c8db`) ruled out the row-phase carrier; the current plan ports the standard `ConsumedBlockSize` chain Blink uses for *all* nested fragmentation (not just multicol). It is one mechanism, one missing line, fourteen tests.

Per CLAUDE.md §1 ("solve the category completely"): the WRITE-site fix is the category. Don't chase any individual test that doesn't close after Cmt-1 with a point fix; instead, decide between Cmt-5 (Blink's resume-variant clause) and a deeper investigation of why the chain is still mis-seeded for that test.

## 12. Open questions to resolve during implementation

1. **Does the Phase 14b defer's outgoing fragment need `ConsumedBlockSize`?** It returns at line 429-431 with no `BreakToken` at all — the parent's `BreakBeforeChildIfNeeded` synthesizes the break-before token. If after Cmt-1 a defer-then-resume case (e.g. `nested-011` outer-col-1 → outer-col-2) still mis-seeds, examine whether the defer should emit its own break token with `ConsumedBlockSize=0` (correctly indicating no content was placed) and `IsBreakBefore=true`.

2. **Sequence-number monotonicity.** `seqNum := mla.space.BreakToken.SequenceNumber + 1` mirrors `block_layout.go`'s pattern. Verify no caller asserts a specific multicol sequence number. (Grep: `SequenceNumber` consumers.)

3. **`outgoingMulticolData` interaction with `ConsumedBlockSize`.** The Phase 18 row-phase carrier and the standard chain are independent fields on the same struct; they should compose cleanly. Confirm by inspecting the only test that exercises both: column-wrap:wrap with nested multicol (none exist in the named cluster, but verify the 13 drivers' column-height-* tests still pass).

Resolve each of these during the Cmt-1 verification, not before — they are conditional contingencies, not blockers.

## 13. Actual findings (2026-04-29) — HARD EXIT after Cmt-1

### 13.1 Cmt-1 result

Cmt-1 was committed as `2a822b9d` on `multicol-phase-22`. Pre/post-flight:

| | Pre | Post |
|---|---|---|
| 13 drivers | 13/13 | 13/13 (no regression) |
| 9 prior-clip-wins | 9/9 | 9/9 (no regression) |
| Phase 22 cluster | 8/22 | 8/22 (**zero tests closed**) |

Cluster baseline was 8/22, not 7/22 as predicted — `-029` also passes (it was the Phase 21 stuck
test; appears it self-healed or was already passing).

Hard exit fired: cluster gain = 0, below the +9 threshold. The plan's prediction that wiring
`ConsumedBlockSize` in `buildOuterBreakResult` would close ~14 tests was incorrect. Two distinct bug
classes explain why.

### 13.2 Class A — explicit-height inner multicols (`-011`, `-032`)

Phase 14b at `multicol_layout.go:413-414` fires when `outerAvailable < explicitBlockSize`. But when
`mla.space.FragmentainerOffset == 0` (fresh outer column, full space available),
`BreakBeforeChildIfNeeded` refuses to emit a break-before token (`refuseBreakBefore` is true when
`spaceLeft >= fragmentainerBlockSize`). The result: the inner multicol returns a 0-height fragment
with no break token and is placed in EVERY outer column without laying out any content.
`buildOuterBreakResult` is never called, so Cmt-1 has no effect.

Root cause: Phase 14b's comment says "defer to the next outer fragmentainer where it has full space",
but at `FragmentainerOffset=0` there IS no earlier partial column to defer from — the current
column IS the fresh one.

**Fix:** narrow the Phase 14b guard to only fire when there is a partial outer column to defer from:

```go
// old
if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
    columnFill == "auto" && outerAvailable < explicitBlockSize {

// new
if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
    columnFill == "auto" && outerAvailable < explicitBlockSize &&
    mla.space.FragmentainerOffset > 0 {
```

With this fix, explicit-height inner multicols at `FragmentainerOffset=0` proceed into the walker
loop, fragment in place, and emit `buildOuterBreakResult()` with Cmt-1's correct `ConsumedBlockSize`.
The defer still fires correctly when the inner starts mid-column.

**Tests expected to close:** `-011`, `-032`. Verify `-010` (the test that motivated Phase 14b) does
not regress.

### 13.3 Class B — auto-height inner multicols (12 tests)

The `ConsumedBlockSize` READ site at `multicol_layout.go:293-301` is gated on `hasExplicitBlock`:

```go
if hasExplicitBlock {   // ← auto-height skips this block entirely
    mla.remainingContentBlockSize = explicitBlockSize
    if mla.space.BreakToken != nil && ... {
        mla.remainingContentBlockSize -= mla.space.BreakToken.ConsumedBlockSize.Float64()
    }
}
```

For auto-height inner multicols, `ConsumedBlockSize` is never consulted. The `ConsumedBlockSize`
chain is irrelevant to their resume path. These tests fail for a different, as-yet-unknown reason.

**Tests affected:** `-013, -014, -016, -017, -018, -019, -020, -022, -023, -024, -027,
fill-balance-026`.

**`fill-balance-026`** has no outer fragmentation context at all (5-column outer with no explicit
height), so Phase 22's chain is irrelevant. Needs completely separate investigation.

**Next step:** trace `multicol-nested-013` with debug output to understand the actual vs expected
layout. Study the Blink reference for what this test exercises.

### 13.4 Revised implementation sequence

1. **Cmt-A (Class A fix)** — add `&& mla.space.FragmentainerOffset > 0` to Phase 14b guard at
   line 413. Targeted test set: `-010` (must not regress), `-011`, `-032` (expected to close), plus
   full cluster + 13 drivers + 9 prior-clip-wins.
2. **Investigation** — trace `multicol-nested-013` to identify Class B root cause. Study Blink's
   corresponding tests. Only then decide on Cmt-B.
3. **Cmt-B (Class B fix)** — implement once root cause is understood.
4. Full multicol sweep + 4-cat sweep + merge to master.
