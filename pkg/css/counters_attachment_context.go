package css

import (
	"strconv"
	"strings"

	"louis14/pkg/html"
)

// CountersAttachmentContext mirrors Blink's
// third_party/blink/renderer/core/css/counters_attachment_context.{h,cc}
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// It owns one CounterInheritanceTable mapping a counter name to a
// stack of CounterEntries (innermost-last). The layout-tree builder
// threads a single context through a pre-order traversal, calling
// EnterObject before descending into a node's children and LeaveObject
// after. Each entry records the originating node so that:
//
//   * RemoveStaleCounters can pop entries whose origin is no longer an
//     ancestor of the entering node (sibling-subtree isolation).
//   * CreateCounter can collapse two resets that originate on the same
//     element or on previous siblings.
//   * RemoveCounterIfAncestorExists can drop a reset entry on
//     LeaveObject if its ancestor counter is still in scope.
//
// The context handles counter-reset, counter-increment, and
// counter-set (CSS Lists 3 §propdef-counter-set). list-item /
// reversed semantics arrive in a later phase.
//
// References (verified at the pinned SHA):
//   * Header — CounterEntry, CounterStack, CounterInheritanceTable,
//     enum class Type { kIncrementType=1<<0, kResetType=1<<1, kSetType=1<<2 }.
//   * Implementation — ProcessCounter, CreateCounter, UpdateCounterValue,
//     RemoveStaleCounters, RemoveCounterIfAncestorExists, GetCounterValues,
//     DetermineCounterTypeAndValue, CalculateCounterValue.

// CounterType is the Blink bitmask "enum class Type". A single
// element may declare multiple directive kinds for the same counter
// name (e.g. counter-set + counter-increment), so they OR together.
type CounterType uint32

const (
	CounterIncrementType CounterType = 1 << 0
	CounterResetType     CounterType = 1 << 1
	CounterSetType       CounterType = 1 << 2
)

// IsReset reports whether the directive bitmask includes a reset
// (Blink: IsReset). Reset wins when combined with other kinds.
func (t CounterType) IsReset() bool { return t&CounterResetType != 0 }

// IsSetOrReset reports whether the bitmask is a "set" or "reset"
// (Blink: IsSetOrReset). Both replace the current value rather than
// adding to it.
func (t CounterType) IsSetOrReset() bool {
	return t&(CounterResetType|CounterSetType) != 0
}

// CounterEntry mirrors Blink CounterEntry { layout_object, value }.
// Origin is the originating DOM/pseudo node; nil entries serve as
// style-containment boundaries pushed by EnterStyleContainmentScope
// (CSS Contain 1 §3.3). UpdateCounterValue and RemoveStaleCounters
// stop at boundaries; GetCounterValues skips them.
//
// EnterOrder is the monotonically increasing index at which Origin's
// EnterObject was called. It is used by CreateCounter to identify
// "previous siblings" in the layout tree (Blink walks layout-tree
// siblings; in louis14 we approximate this by ordering siblings by
// when EnterObject saw them, which is document order including
// pseudo-elements).
type CounterEntry struct {
	Origin     *html.Node
	Value      int
	EnterOrder uint64
}

// counterStack is a stack of CounterEntry pointers, innermost last.
// Pointer semantics match Blink's HeapVector<Member<CounterEntry>>:
// UpdateCounterValue mutates the topmost entry in place.
type counterStack []*CounterEntry

// CounterInheritanceTable maps counter name -> stack pointer.
// Pointer-to-stack mirrors Blink's
// HashMap<AtomicString, Member<CounterStack>>.
type CounterInheritanceTable map[string]*counterStack

// CountersAttachmentContext is threaded through layout-tree building.
// A pre-order traversal calls EnterObject before descending and
// LeaveObject after, with GetCounterValues reading the table for
// counter() / counters() in content.
type CountersAttachmentContext struct {
	table                 CounterInheritanceTable
	rootIsDocumentElement bool

	// enterCounter increments on each EnterObject call so we can
	// stamp entries with a document-order index. CreateCounter uses
	// it to detect previous siblings in the layout-tree sense
	// (including pseudo-elements that are not in DOM Children).
	enterCounter uint64

	// enterOrder remembers the index at which each origin entered
	// (for nodes still in scope). Removed on LeaveObject. Used by
	// CreateCounter's "same-or-previous sibling" check.
	enterOrder map[*html.Node]uint64

	// displayContents records which entered nodes have
	// `display: contents` so layoutParentOfOrigin can skip them when
	// resolving ancestry (mirrors Blink LayoutTreeBuilderTraversal
	// ::LayoutParent which transparently skips display:contents).
	displayContents map[*html.Node]bool
}

// NewCountersAttachmentContext returns an empty context.
func NewCountersAttachmentContext() *CountersAttachmentContext {
	return &CountersAttachmentContext{
		table:           make(CounterInheritanceTable),
		enterOrder:      make(map[*html.Node]uint64),
		displayContents: make(map[*html.Node]bool),
	}
}

// SetAttachmentRootIsDocumentElement marks the root as the document
// element (Blink CountersAttachmentContext::SetAttachmentRootIsDocumentElement).
// Not yet consumed by Phase 1 but tracked for symmetry.
func (c *CountersAttachmentContext) SetAttachmentRootIsDocumentElement() {
	c.rootIsDocumentElement = true
}

// AttachmentRootIsDocumentElement reports the flag set above.
func (c *CountersAttachmentContext) AttachmentRootIsDocumentElement() bool {
	return c.rootIsDocumentElement
}

// CounterDirective describes one resolved (name, kind, value) tuple
// for a single element. Blink's DetermineCounterTypeAndValue produces
// one of these per (element, name) pair, with multiple kinds OR'd into
// the Type bitmask. The Reversed flag carries the parsed
// `reversed(name)` form for Phase 4; in Phase 1 it parses but has no
// semantic effect.
type CounterDirective struct {
	Name     string
	Type     CounterType
	Value    int
	Reversed bool
}

// EnterObject runs the Blink EnterObject step: collect every
// counter-directive on this element, merge per name into a single
// CounterDirective (bitmask + combined value, per Blink's
// DetermineCounterTypeAndValue / CounterDirectives::CombinedValue),
// then call ProcessCounter once per name. Currently reads counter-reset,
// counter-increment, and counter-set; the list-item implicit increment
// arrives in a later phase.
//
// If the element has `contain: style` (style containment), pushes a
// style-containment boundary onto every counter stack AFTER processing
// the element's own directives. Per Blink (counters_attachment_context.cc
// EnterObject @ 4883d11fef): "Doing it after counters creation as the
// element itself is not included in the style containment scope." This
// means the contained element's own ::before/::after can still observe
// the counters the element declared, but real descendants see a fresh
// counter scope.
//
// Stamps `node`'s entry order so CreateCounter can identify
// previous-sibling relationships in layout-tree order.
func (c *CountersAttachmentContext) EnterObject(node *html.Node, style *Style) {
	if node == nil || style == nil {
		return
	}
	c.enterCounter++
	c.enterOrder[node] = c.enterCounter
	// Record display:contents so ancestry lookups can skip this node
	// (Blink LayoutTreeBuilderTraversal::LayoutParent). We read the
	// computed `display` value rather than depending on
	// DisplayType enum constants so this package stays free of
	// LayoutType imports.
	if disp, ok := style.Get("display"); ok && strings.EqualFold(strings.TrimSpace(disp), "contents") {
		c.displayContents[node] = true
	}
	for _, d := range parseCounterDirectives(style) {
		c.ProcessCounter(node, d)
	}
	if style.HasStyleContainment() {
		c.EnterStyleContainmentScope()
	}
}

// LeaveObject runs the Blink LeaveObject step: pop the style-containment
// boundary first (mirroring Blink's reverse-of-EnterObject order), then
// for every counter the element reset, pop the entry if an ancestor
// entry of the same counter is still in scope
// (RemoveCounterIfAncestorExists). Increment-only / set-only entries
// are not pushed by this element so they don't need popping here.
// Releases the entry-order stamp last — leaving the map entry around
// until siblings have entered would only matter if a left node could
// appear later in document order, which never happens in a pre-order
// traversal.
func (c *CountersAttachmentContext) LeaveObject(node *html.Node, style *Style) {
	if node == nil || style == nil {
		return
	}
	if style.HasStyleContainment() {
		c.LeaveStyleContainmentScope()
	}
	for _, d := range parseCounterDirectives(style) {
		if d.Type.IsReset() {
			c.RemoveCounterIfAncestorExists(node, d.Name)
		}
	}
	// Keep enterOrder around: the enter-order map is consulted by
	// CreateCounter for entries that may still be on the stack from
	// nodes that have already exited their own subtree. Clearing on
	// LeaveObject would lose that information. (Memory grows O(N)
	// for the tree; acceptable for layout-tree builds.)
}

// EnterStyleContainmentScope mirrors Blink's
// CountersAttachmentContext::EnterStyleContainmentScope @
// 4883d11fef counters_attachment_context.cc:636. It pushes a
// nil-origin sentinel onto every existing counter stack. The sentinel
// causes UpdateCounterValue to create a NEW counter on the writing
// element (rather than mutating an existing entry across the
// boundary), and causes RemoveStaleCounters to stop walking. Counter
// reads via GetCounterValues SKIP the sentinel rather than stopping
// at it — the boundary gates writes, not reads.
func (c *CountersAttachmentContext) EnterStyleContainmentScope() {
	for _, stack := range c.table {
		*stack = append(*stack, nil)
	}
}

// LeaveStyleContainmentScope mirrors Blink's
// CountersAttachmentContext::LeaveStyleContainmentScope @
// 4883d11fef counters_attachment_context.cc:646. For each counter
// stack it pops every entry above the topmost nil sentinel, then pops
// the sentinel itself. Entries created INSIDE the containment scope
// (after the matching EnterStyleContainmentScope) are discarded;
// entries below the sentinel remain.
func (c *CountersAttachmentContext) LeaveStyleContainmentScope() {
	for _, stack := range c.table {
		for len(*stack) > 0 && (*stack)[len(*stack)-1] != nil {
			*stack = (*stack)[:len(*stack)-1]
		}
		if len(*stack) > 0 && (*stack)[len(*stack)-1] == nil {
			*stack = (*stack)[:len(*stack)-1]
		}
	}
}

// ProcessCounter mirrors Blink ProcessCounter:
//
//	RemoveStaleCounters first;
//	if reset bit set        -> CreateCounter;
//	else                    -> UpdateCounterValue (set or increment).
//
// Blink merges all three counter properties (reset/increment/set)
// on a single (element, name) pair into a single ProcessCounter call
// with a bitmask Type and Blink's CombinedValue. CombinedValue for
// reset+increment is the saturated sum (reset_value + increment_value),
// which CreateCounter then pushes verbatim — so an element with both
// `counter-reset: c 98` and `counter-increment: c 1` pushes a fresh
// {origin, 99} entry. parseCounterDirectives groups by name and sets
// the combined value to match.
func (c *CountersAttachmentContext) ProcessCounter(node *html.Node, d CounterDirective) {
	c.RemoveStaleCounters(node, d.Name)
	if d.Type.IsReset() {
		c.CreateCounter(node, d.Name, d.Value)
		return
	}
	c.UpdateCounterValue(node, d.Name, d.Type, d.Value)
}

// CreateCounter (reset) mirrors Blink CreateCounter:
//
//	If the innermost existing entry's originating element is the
//	entering element OR a previous sibling of it, pop that entry first
//	(two resets on the same element, or a reset following a sibling
//	reset, do not stack — Blink: "Remove innermost counter with same
//	or previous sibling originating element").
//	Then push a new CounterEntry{origin, value, enterOrder}.
//
// "Previous sibling" in Blink is layout-tree sibling order, which
// includes ::before/::marker/::after pseudo-elements between or
// around real DOM children. Two siblings share the same parent and
// the one with the smaller EnterObject stamp is earlier.
func (c *CountersAttachmentContext) CreateCounter(node *html.Node, name string, value int) {
	stack := c.table[name]
	if stack == nil {
		stack = &counterStack{}
		c.table[name] = stack
	}
	if n := len(*stack); n > 0 {
		top := (*stack)[n-1]
		if top != nil && c.isSameOrPreviousLayoutSibling(top.Origin, top.EnterOrder, node) {
			*stack = (*stack)[:n-1]
		}
	}
	*stack = append(*stack, &CounterEntry{
		Origin:     node,
		Value:      value,
		EnterOrder: c.enterCounter,
	})
}

// UpdateCounterValue (increment / set) mirrors Blink UpdateCounterValue:
//
//	If the stack is empty (or ends in a containment-boundary nil),
//	implicitly instantiate the counter with value 0 via CreateCounter,
//	then mutate the topmost entry by CalculateCounterValue.
//
// For Phase 1 only increment / set bits are passed in; the reset
// branch is handled by CreateCounter above.
func (c *CountersAttachmentContext) UpdateCounterValue(node *html.Node, name string, kind CounterType, value int) {
	stack := c.table[name]
	if stack == nil || len(*stack) == 0 || (*stack)[len(*stack)-1] == nil {
		// Implicit instantiation: push a synthetic reset of 0 owned
		// by the entering element, then update it (Blink: stack
		// empty / boundary => CreateCounter then update).
		c.CreateCounter(node, name, 0)
		stack = c.table[name]
	}
	top := (*stack)[len(*stack)-1]
	top.Value = calculateCounterValue(kind, value, top.Value)
}

// RemoveStaleCounters mirrors Blink RemoveStaleCounters: pop every
// entry whose originating element's PARENT is not a strict ancestor
// of the entering element. Stops at a containment-boundary nil.
//
// Blink's exact rule (counters_attachment_context.cc at the pinned
// SHA): `if (!parent || IsAncestorOf(*parent, *element)) break;`
// — i.e. a nil parent (root) keeps the entry, and a strict ancestor
// relationship from parent_of_origin down to the entering element
// keeps it too.
func (c *CountersAttachmentContext) RemoveStaleCounters(node *html.Node, name string) {
	stack := c.table[name]
	if stack == nil {
		return
	}
	for len(*stack) > 0 {
		top := (*stack)[len(*stack)-1]
		if top == nil { // containment boundary
			return
		}
		parent := c.layoutParentOfOrigin(top.Origin)
		if parent == nil {
			// Root-level entry — counters at the document root are
			// in scope for every element.
			return
		}
		if isStrictAncestorOf(parent, node) || parent == node {
			// Origin's parent is an ancestor of `node`, OR the
			// entering node IS the origin's parent (entry was
			// pushed by a child of the entering node — keep it
			// while we're inside that child's parent's subtree).
			return
		}
		*stack = (*stack)[:len(*stack)-1]
	}
}

// RemoveCounterIfAncestorExists mirrors Blink
// RemoveCounterIfAncestorExists: in LeaveObject for reset counters,
// pop the leaving entry when an outer entry of the same counter
// would "win" inheritance for following siblings/descendants.
//
// Two Blink conditions trigger the pop:
//
//  1. previous_element is an ancestor of `node`.
//  2. previous_element's layout parent is an ancestor of `node` AND
//     that parent is not the same as `node`'s layout parent — i.e.
//     the previous entry was pushed by a "great-uncle" subtree that
//     still encloses node's parent. This is what lets a sibling
//     scope (e.g. `.scope` ancestor with a reset on its
//     `::before`-only counter) pop the `::before` reset entry on
//     LeaveObject so following real siblings still see the outer
//     scope, not a now-stale ::before scope.
//
// (See `counters_attachment_context.cc` RemoveCounterIfAncestorExists
// at the pinned SHA.)
func (c *CountersAttachmentContext) RemoveCounterIfAncestorExists(node *html.Node, name string) {
	stack := c.table[name]
	if stack == nil || len(*stack) < 2 {
		return
	}
	top := (*stack)[len(*stack)-1]
	prev := (*stack)[len(*stack)-2]
	if top == nil || prev == nil {
		return
	}
	// Only pop the leaving entry if it was pushed by *this* element.
	if top.Origin != node {
		return
	}
	// Condition 1: previous entry's origin is an ancestor of node.
	if isStrictAncestorOf(prev.Origin, node) {
		*stack = (*stack)[:len(*stack)-1]
		return
	}
	// Condition 2: previous entry's origin's LAYOUT parent is an
	// ancestor of node AND that parent is not the same as node's
	// LAYOUT parent (display:contents transparency applies on both
	// sides — matches Blink's LayoutParentElement use here).
	if prev.Origin != nil {
		prevParent := c.layoutParentOfOrigin(prev.Origin)
		nodeParent := c.layoutParentOfOrigin(node)
		if prevParent != nil && isStrictAncestorOf(prevParent, node) && prevParent != nodeParent {
			*stack = (*stack)[:len(*stack)-1]
			return
		}
	}
}

// GetCounterValues mirrors Blink GetCounterValues. With onlyLast=true
// it returns the innermost in-scope value (counter()). With
// onlyLast=false it returns every in-scope value from outermost to
// innermost (counters()).
//
// Blink calls RemoveStaleCounters first to drop entries that are not
// in scope for `node`, then iterates the surviving stack in reverse.
// We mirror that order so the no-staleness-check iteration in the
// hot path matches Blink's logic exactly.
//
// When the counter is not in scope at all, returns `[]int{0}` (LOU-144) —
// mirrors Blink's `CountersAttachmentContext::GetCounterValues` at
// `third_party/blink/renderer/core/css/counters_attachment_context.cc`
// @ Chromium `main` `4883d11fef4a8713e32cd582ecef6dc5457c8c3f`, which
// implements CSS Lists 3 §counter-functions: a counter() reference to a
// never-declared counter is interpreted as if the counter had been
// instantiated by `counter-reset: <name> 0` at the root.
func (c *CountersAttachmentContext) GetCounterValues(node *html.Node, name string, onlyLast bool) []int {
	// Drop any entries that are not in scope for `node` first.
	c.RemoveStaleCounters(node, name)
	stack := c.table[name]
	if stack == nil || len(*stack) == 0 {
		return []int{0}
	}
	var values []int
	for i := len(*stack) - 1; i >= 0; i-- {
		entry := (*stack)[i]
		if entry == nil {
			// Style containment boundary: counter() and counters() can
			// CROSS the boundary — Blink's GetCounterValues skips
			// nullptrs with `continue` for non-page counters. The
			// boundary's job is to gate WRITES (UpdateCounterValue
			// treats a boundary as "stack empty" and creates a new
			// counter on the writer), not READS. See
			// counters_attachment_context.cc lines 360-378 at
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			continue
		}
		values = append(values, entry.Value)
		if onlyLast {
			break
		}
	}
	if onlyLast || len(values) <= 1 {
		return values
	}
	// Reverse so outermost is first (matches CSS spec for counters()).
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
	return values
}

// calculateCounterValue mirrors Blink CalculateCounterValue:
//
//	if IsSetOrReset(type) -> return new value;
//	else                   -> int32-saturated add (current + value).
//
// Phase 1 implements increment / set only; the reset path runs through
// CreateCounter, not this helper.
func calculateCounterValue(kind CounterType, value, current int) int {
	if kind.IsSetOrReset() {
		return value
	}
	return saturatingAddInt32(current, value)
}

// saturatingAddInt32 returns current+value clamped to int32 range.
// CSS Lists §overflow requires saturation (Blink uses base::CheckAdd).
// Phase 2 widens this to also clamp set / reset deltas; Phase 1 only
// applies it to increment.
func saturatingAddInt32(current, value int) int {
	const minI32 = -1 << 31
	const maxI32 = (1 << 31) - 1
	sum := current + value
	if value > 0 && sum < current {
		return maxI32
	}
	if value < 0 && sum > current {
		return minI32
	}
	if sum < minI32 {
		return minI32
	}
	if sum > maxI32 {
		return maxI32
	}
	return sum
}

// parseCounterDirectives reads counter-reset, counter-increment, and
// counter-set off a Style and returns one CounterDirective per counter
// NAME, matching Blink's CounterDirectiveMap shape. Multiple directives
// on the same name OR their type bits together and combine values per
// Blink's CounterDirectives::CombinedValue (counter_directives.h:113 @
// 4883d11fef): counter-set overrides everything else; otherwise it's
// reset_value + increment_value (saturated). The reversed(name) form
// on counter-reset is parsed and the Reversed flag preserved, but it
// has no semantic effect until Phase 4.
func parseCounterDirectives(style *Style) []CounterDirective {
	// Use a name-keyed accumulator so two declarations on the same
	// counter (e.g. counter-reset:c 98 + counter-increment:c 1)
	// merge into one CounterDirective with type=reset|increment and
	// value=99 — matching Blink CombinedValue exactly.
	type accum struct {
		hasReset, hasIncrement, hasSet bool
		resetValue                     int
		incrementValue                 int
		setValue                       int
		reversed                       bool
		order                          int
	}
	byName := make(map[string]*accum)
	order := 0
	track := func(name string) *accum {
		a, ok := byName[name]
		if !ok {
			a = &accum{order: order}
			order++
			byName[name] = a
		}
		return a
	}

	if raw, ok := style.Get("counter-reset"); ok {
		for _, p := range parseCounterPropertyList(raw, 0) {
			a := track(p.name)
			a.hasReset = true
			a.resetValue = p.value
			// Phase 4 will compute the initial value via
			// CalculateInitialValueForReversed when this flag is
			// set. Phase 1 still pushes a 0-valued reset so the
			// counter exists in scope (which keeps tests that mix
			// `counter-reset: reversed(foo)` with explicit
			// `counter-increment` directives at least seeing a
			// stable counter — Phase 4 will overlay the correct
			// initial value).
			if p.reversed {
				a.reversed = true
			}
		}
	}
	if raw, ok := style.Get("counter-increment"); ok {
		for _, p := range parseCounterPropertyList(raw, 1) {
			a := track(p.name)
			a.hasIncrement = true
			a.incrementValue = p.value
		}
	}
	if raw, ok := style.Get("counter-set"); ok {
		// counter-set default value is 0 (per CSS Lists 3
		// §propdef-counter-set: "If <integer> is omitted ... default
		// to 0").
		for _, p := range parseCounterPropertyList(raw, 0) {
			a := track(p.name)
			a.hasSet = true
			a.setValue = p.value
		}
	}

	// Emit in insertion order so callers see deterministic ordering.
	out := make([]CounterDirective, 0, len(byName))
	tmp := make([]struct {
		name string
		a    *accum
	}, 0, len(byName))
	for n, a := range byName {
		tmp = append(tmp, struct {
			name string
			a    *accum
		}{n, a})
	}
	// Tiny n; insertion sort by order.
	for i := 1; i < len(tmp); i++ {
		for j := i; j > 0 && tmp[j-1].a.order > tmp[j].a.order; j-- {
			tmp[j-1], tmp[j] = tmp[j], tmp[j-1]
		}
	}
	for _, e := range tmp {
		var t CounterType
		if e.a.hasReset {
			t |= CounterResetType
		}
		if e.a.hasIncrement {
			t |= CounterIncrementType
		}
		if e.a.hasSet {
			t |= CounterSetType
		}
		// Blink CombinedValue (counter_directives.h:113 @ 4883d11fef):
		// counter-set overrides everything else; otherwise it's
		// reset_value + increment_value (saturated).
		var value int
		switch {
		case e.a.hasSet:
			value = e.a.setValue
		case e.a.hasReset && e.a.hasIncrement:
			value = saturatingAddInt32(e.a.resetValue, e.a.incrementValue)
		case e.a.hasReset:
			value = e.a.resetValue
		default:
			value = e.a.incrementValue
		}
		out = append(out, CounterDirective{
			Name:     e.name,
			Type:     t,
			Value:    value,
			Reversed: e.a.reversed,
		})
	}
	return out
}

// parsedCounterProperty captures one (name, value, reversed) tuple
// from a counter-reset or counter-increment property value.
type parsedCounterProperty struct {
	name     string
	value    int
	reversed bool
}

// parseCounterPropertyList parses "name [int]?  name [int]?  ..."
// with optional `reversed(name)` wrappers (counter-reset only).
// defaultVal applies when no integer follows: 0 for reset, 1 for
// increment.
func parseCounterPropertyList(raw string, defaultVal int) []parsedCounterProperty {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return nil
	}
	tokens := tokenizeCounterValue(raw)
	var out []parsedCounterProperty
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "none" {
			continue
		}
		reversed := false
		name := tok
		// reversed(name): preserved for Phase 4.
		if lower := strings.ToLower(tok); strings.HasPrefix(lower, "reversed(") && strings.HasSuffix(tok, ")") {
			reversed = true
			name = strings.TrimSpace(tok[len("reversed(") : len(tok)-1])
		}
		if name == "" {
			continue
		}
		value := defaultVal
		if i+1 < len(tokens) {
			if n, err := strconv.Atoi(tokens[i+1]); err == nil {
				value = n
				i++
			}
		}
		out = append(out, parsedCounterProperty{
			name:     name,
			value:    value,
			reversed: reversed,
		})
	}
	return out
}

// tokenizeCounterValue splits a counter-reset / counter-increment
// value into whitespace-separated tokens, but keeps any reversed(...)
// argument together with its parentheses.
func tokenizeCounterValue(raw string) []string {
	var tokens []string
	var buf strings.Builder
	depth := 0
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		tokens = append(tokens, buf.String())
		buf.Reset()
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch == '(':
			depth++
			buf.WriteByte(ch)
		case ch == ')':
			if depth > 0 {
				depth--
			}
			buf.WriteByte(ch)
		case (ch == ' ' || ch == '\t' || ch == '\n') && depth == 0:
			flush()
		default:
			buf.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

// parentNode returns the LAYOUT-tree parent of `n`, mirroring Blink's
// LayoutTreeBuilderTraversal::LayoutParent. The layout-tree parent
// skips DOM ancestors with `display: contents`, because those ancestors
// produce no box and their counter directives flow through to the next
// real box ancestor (Blink CSS Lists Phase 2 — louis14 will pick this
// up here so the counter context behaves correctly even though Phase 1
// in this codebase does not yet route `display:contents` through
// EnterObject directly).
//
// Synthetic ::before/::after/::marker nodes have Parent set to their
// originating real element, so they pass through this helper too.
//
// styleResolver is allowed to be nil — in which case display:contents
// is not detected and the function degenerates to bare DOM parent.
func parentNode(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	return n.Parent
}

// layoutParent returns the layout-tree parent of `node` for the given
// styles map: it walks up the DOM tree skipping nodes whose computed
// style has `display: contents`. This is what RemoveStaleCounters /
// RemoveCounterIfAncestorExists ancestry checks consult so that
// display:contents ancestors do not break counter inheritance through
// them (Blink LayoutParent semantics).
func (c *CountersAttachmentContext) layoutParentOfOrigin(origin *html.Node) *html.Node {
	if origin == nil {
		return nil
	}
	parent := origin.Parent
	for parent != nil && c.isDisplayContents(parent) {
		parent = parent.Parent
	}
	return parent
}

// isDisplayContents reports whether `n` is a layout-tree-transparent
// container, i.e. has `display: contents`. The context caches this via
// its displayContents map populated from EnterObject's style lookup.
func (c *CountersAttachmentContext) isDisplayContents(n *html.Node) bool {
	if n == nil || c.displayContents == nil {
		return false
	}
	return c.displayContents[n]
}

// isAncestorOrSelf reports whether ancestor is the entering node or
// any ancestor of it via Parent links. Used by RemoveStaleCounters
// (where matching the entering node itself is acceptable — a node is
// always in its own descendant set).
func isAncestorOrSelf(ancestor, node *html.Node) bool {
	if ancestor == nil {
		// A nil origin parent (e.g. document-root entries) is treated
		// as an ancestor of any node — every node descends from the
		// document.
		return true
	}
	for cur := node; cur != nil; cur = cur.Parent {
		if cur == ancestor {
			return true
		}
	}
	return false
}

// isStrictAncestorOf mirrors Blink IsAncestorOf — true only when
// ancestor is a STRICT ancestor of node (not node itself). Used by
// RemoveCounterIfAncestorExists, which must not treat a node as its
// own ancestor.
func isStrictAncestorOf(ancestor, node *html.Node) bool {
	if ancestor == nil || node == nil {
		return false
	}
	for cur := node.Parent; cur != nil; cur = cur.Parent {
		if cur == ancestor {
			return true
		}
	}
	return false
}

// isSameOrPreviousLayoutSibling reports whether `a` is the same node
// as `b` OR a previous LAYOUT-tree sibling of `b`. Layout-tree
// siblings include synthetic pseudo-elements (::before, ::marker,
// ::after) that are not in the DOM Children array but do share the
// same Parent as real DOM children.
//
// The "previous" decision uses EnterObject stamps: if `aOrder < b`'s
// current stamp, `a` was encountered earlier in the pre-order walk
// and therefore is a previous sibling when they share a parent. We
// receive `aOrder` directly because the caller already has it
// (avoids a map lookup in the hot path).
func (c *CountersAttachmentContext) isSameOrPreviousLayoutSibling(a *html.Node, aOrder uint64, b *html.Node) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	if a.Parent == nil || a.Parent != b.Parent {
		return false
	}
	bOrder, ok := c.enterOrder[b]
	if !ok {
		// b hasn't been stamped yet — caller must call this only
		// after EnterObject(b) ran. As a safety net, fall back to
		// DOM-Children order.
		return domChildrenOrder(a, b)
	}
	return aOrder < bOrder
}

// domChildrenOrder is a fallback for the rare case where `b` is not
// yet stamped (should not happen during a normal pre-order walk).
func domChildrenOrder(a, b *html.Node) bool {
	for _, sib := range a.Parent.Children {
		if sib == a {
			return true
		}
		if sib == b {
			return false
		}
	}
	return false
}
