# Studying Blink LayoutNG for Multi-Pass Inline Layout Rewrite

**Date**: 2026-02-05
**Status**: Planning phase
**Goal**: Understand Blink's architecture to properly implement multi-pass inline layout

---

## Why Study Blink?

Our multi-pass implementation has fundamental architectural issues:
- ❌ Recursive layoutNode() during dimension queries pollutes state
- ❌ Global float list has complex sharing across phases
- ❌ No clear separation of "query dimensions" vs "layout with side effects"
- ❌ Retry logic causes regressions despite being "correct"

Blink LayoutNG solved these problems - we need to learn how.

---

## Blink LayoutNG Architecture Overview

### Key Insight: Immutable Fragment Tree
Blink separates **layout algorithm** from **output tree**:
- **Input**: DOM tree (immutable)
- **Algorithm**: Layout code (produces fragments)
- **Output**: Fragment tree (immutable after creation)

This prevents the state pollution we're experiencing!

### Three-Phase Inline Layout Pipeline

#### Phase 1: NGInlineItemsBuilder (CollectInlines)
**Purpose**: Flatten DOM to sequential item list
**Input**: DOM subtree
**Output**: Vector of NGInlineItem

**Key aspects**:
- Pure transformation, no layout side effects
- Items have: type, node reference, style, text content
- Dimensions NOT computed here (deferred to Phase 2)
- Absolutely no float list manipulation

**Chromium source**:
- `third_party/blink/renderer/core/layout/ng/inline/ng_inline_items_builder.h`
- `third_party/blink/renderer/core/layout/ng/inline/ng_inline_items_builder.cc`

#### Phase 2: NGLineBreaker
**Purpose**: Decide what items go on each line
**Input**: Vector of NGInlineItem, constraint space
**Output**: Vector of NGLineInfo (line breaking decisions)

**Key aspects**:
- Uses **constraint space** abstraction for available width
- Accounts for floats via **exclusions** in constraint space
- Can RETRY when floats change available width
- Still pure - doesn't create positioned boxes yet
- Dimensions computed on-demand via **min-content/max-content sizing**

**Chromium source**:
- `third_party/blink/renderer/core/layout/ng/inline/ng_line_breaker.h`
- `third_party/blink/renderer/core/layout/ng/inline/ng_line_breaker.cc`

#### Phase 3: NGInlineLayoutAlgorithm
**Purpose**: Create positioned fragment tree
**Input**: NGLineInfo results
**Output**: NGPhysicalBoxFragment tree

**Key aspects**:
- This is where actual boxes get created with positions
- Floats positioned here and added to **exclusion space**
- Fragment tree is immutable once created
- Clear separation: this phase has side effects, previous don't

**Chromium source**:
- `third_party/blink/renderer/core/layout/ng/inline/ng_inline_layout_algorithm.h`
- `third_party/blink/renderer/core/layout/ng/inline/ng_inline_layout_algorithm.cc`

---

## Key Architectural Patterns to Learn

### 1. Constraint Space Abstraction
Instead of passing raw available width, Blink uses **NGConstraintSpace**:
```cpp
struct NGConstraintSpace {
  LayoutUnit available_size;
  NGExclusionSpace exclusion_space;  // Tracks floats!
  WritingMode writing_mode;
  // ... other constraints
};
```

**Benefits**:
- Floats represented as "exclusions" in constraint space
- Constraint space is passed, not global float list
- Easy to create modified constraint space for retry

### 2. Exclusion Space for Floats
Instead of `le.floats []FloatInfo`, Blink uses **NGExclusionSpace**:
- Immutable data structure
- Supports creating modified copies
- Provides query methods: `AvailableInlineSize(BfcOffset)`
- Clear API: `Add(NGExclusion)`, `GetDerivedGeometry()`

**Benefits**:
- No global mutable state
- Retry iterations get fresh exclusion space
- Thread-safe (immutable)

### 3. Fragment Pattern
Instead of modifying Box positions, Blink creates **NGPhysicalFragment**:
- Immutable once created
- Contains: position, size, children, style
- Clear ownership: parent owns children
- Can't accidentally mutate after creation

**Benefits**:
- No position deltas (calculate correct position once)
- No repositioning child boxes recursively
- Easier to reason about

### 4. Sizing Separate from Layout
Blink separates:
- **MinMaxSizesFunc**: Computes min/max content sizes (for dimension queries)
- **Layout()**: Actually creates positioned fragments

Our temporary layoutNode() calls mix both, causing issues!

---

## Our Current Issues Mapped to Blink Solutions

| Our Issue | Blink Solution |
|-----------|---------------|
| Temporary layoutNode() pollutes le.floats | MinMaxSizesFunc doesn't touch exclusions |
| Global le.floats modified during retries | NGExclusionSpace is immutable, create copies |
| FloatBaseIndex complexity | No concept needed - exclusion space passed as parameter |
| Right float negative X | Proper exclusion space avoids overlaps |
| Repositioning box children after float | Fragments created with correct position from start |

---

## Key Chromium Source Files to Study

### Essential Reading (in order):
1. **ng_constraint_space.h** - Understand constraint abstraction
2. **ng_exclusion_space.h** - Understand float representation
3. **ng_inline_items_builder.cc** - Phase 1 implementation
4. **ng_line_breaker.cc** - Phase 2 line breaking with exclusions
5. **ng_inline_layout_algorithm.cc** - Phase 3 fragment construction

### Supporting Files:
- **ng_physical_fragment.h** - Fragment pattern
- **ng_box_fragment_builder.h** - Builder pattern for fragments
- **ng_inline_node.cc** - Inline formatting context entry point

### Where to Find:
Chromium source: https://source.chromium.org/chromium/chromium/src
Path: `third_party/blink/renderer/core/layout/ng/inline/`

---

## Rewrite Plan

### Step 1: Study Phase (This Document)
- [ ] Read key Chromium source files above
- [ ] Document Blink's constraint space abstraction
- [ ] Document Blink's exclusion space API
- [ ] Document fragment pattern
- [ ] Create architecture diagram

### Step 2: Design Phase
- [ ] Design our ConstraintSpace equivalent
- [ ] Design our ExclusionSpace (float tracking)
- [ ] Design Fragment vs Box relationship
- [ ] Design dimension query vs layout separation
- [ ] Write detailed design doc with diagrams

### Step 3: Implementation Phase
- [ ] Implement ExclusionSpace
- [ ] Implement ConstraintSpace
- [ ] Refactor CollectInlineItems (no side effects)
- [ ] Implement sizing functions (separate from layout)
- [ ] Refactor BreakLines to use ConstraintSpace
- [ ] Implement fragment-based construction
- [ ] Add extensive unit tests for each phase

### Step 4: Integration Phase
- [ ] Replace single-pass inline layout with multi-pass
- [ ] Run full WPT test suite
- [ ] Fix regressions
- [ ] Document new architecture

---

## Expected Outcomes

### Better Architecture
- ✅ Pure functions for phase 1 & 2 (no side effects)
- ✅ Clear separation of concerns
- ✅ Immutable data structures where possible
- ✅ Easier to test each phase in isolation

### Better Results
- ✅ box-generation-001: <1% error (from 5.4%)
- ✅ Float positioning correct for all cases
- ✅ No negative X coordinates
- ✅ Proper float stacking (horizontal and vertical)

### Better Maintainability
- ✅ Each phase understandable independently
- ✅ Easy to add new features (vertical-align, etc.)
- ✅ Thread-safe if needed later
- ✅ Matches industry-standard architecture

---

## Questions to Answer from Blink Study

### Constraint Space
1. How does Blink represent available width with floats?
2. How are exclusions added to constraint space?
3. How does retry work with modified constraint space?

### Exclusion Space
1. How are left vs right floats tracked?
2. How does AvailableInlineSize() work at a given Y?
3. How is stacking (horizontal/vertical) handled?

### Sizing vs Layout
1. How does MinMaxSizes work for floats?
2. When is actual layout triggered vs just sizing?
3. How to avoid recursive layout pollution?

### Fragment Construction
1. How are fragments positioned initially?
2. How do parent fragments own child fragments?
3. How to convert fragment tree to our Box tree?

---

## Notes Section

### Blink Study Notes
(To be filled in as we read source code)

---

## References

- Chromium source: https://source.chromium.org/chromium/chromium/src
- LayoutNG design docs: https://chromium.googlesource.com/chromium/src/+/main/third_party/blink/renderer/core/layout/ng/README.md
- Our WIP implementation: `pkg/layout/layout.go` lines 4946-5650
- Our bugs documentation: `docs/memory/multipass-float-bugs.md`
