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
// In Phase 1 the context handles counter-reset / counter-increment;
// counter-set is a Phase 2 extension (the bitmask Type already encodes
// it). list-item / reversed semantics arrive in Phases 3-4.
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
// style-containment boundaries (Phase 2+ feature, but the slot is
// present so GetCounterValues can stop at them).
type CounterEntry struct {
	Origin *html.Node
	Value  int
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
}

// NewCountersAttachmentContext returns an empty context.
func NewCountersAttachmentContext() *CountersAttachmentContext {
	return &CountersAttachmentContext{
		table: make(CounterInheritanceTable),
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
// then call ProcessCounter once per name. Phase 1 reads counter-reset
// and counter-increment; Phase 2 adds counter-set; Phase 3 adds the
// list-item implicit increment.
func (c *CountersAttachmentContext) EnterObject(node *html.Node, style *Style) {
	if node == nil || style == nil {
		return
	}
	for _, d := range parseCounterDirectives(style) {
		c.ProcessCounter(node, d)
	}
}

// LeaveObject runs the Blink LeaveObject step: for every counter the
// element reset, pop the entry if an ancestor entry of the same
// counter is still in scope (RemoveCounterIfAncestorExists).
// Increment-only / set-only entries are not pushed by this element so
// they don't need popping here.
func (c *CountersAttachmentContext) LeaveObject(node *html.Node, style *Style) {
	if node == nil || style == nil {
		return
	}
	for _, d := range parseCounterDirectives(style) {
		if d.Type.IsReset() {
			c.RemoveCounterIfAncestorExists(node, d.Name)
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
//	Then push a new CounterEntry{origin, value}.
func (c *CountersAttachmentContext) CreateCounter(node *html.Node, name string, value int) {
	stack := c.table[name]
	if stack == nil {
		stack = &counterStack{}
		c.table[name] = stack
	}
	if n := len(*stack); n > 0 {
		top := (*stack)[n-1]
		if top != nil && isSameOrPreviousSibling(top.Origin, node) {
			*stack = (*stack)[:n-1]
		}
	}
	*stack = append(*stack, &CounterEntry{Origin: node, Value: value})
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
// entry whose originating element's parent is not an ancestor of the
// entering element (the entering element is not in the same subtree
// as the entry's originator). Stops at a containment-boundary nil.
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
		// Blink: counter is stale when the entering element is NOT a
		// descendant of the originator's parent. We use a sibling-
		// subtree definition: the originator's parent must be an
		// ancestor of (or equal to) the entering element.
		if isAncestorOrSelf(parentNode(top.Origin), node) {
			return
		}
		*stack = (*stack)[:len(*stack)-1]
	}
}

// RemoveCounterIfAncestorExists mirrors Blink
// RemoveCounterIfAncestorExists: in LeaveObject for reset counters,
// if the previous stack entry is an ancestor of the entry being left,
// drop the leaving entry (ancestor counters always win for
// inheritance).
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
	if isAncestorOrSelf(prev.Origin, node) {
		*stack = (*stack)[:len(*stack)-1]
	}
}

// GetCounterValues mirrors Blink GetCounterValues. With onlyLast=true
// it returns the innermost in-scope value (counter()). With
// onlyLast=false it returns every in-scope value from outermost to
// innermost (counters()). An out-of-scope (stale) counter returns
// an empty slice — the caller treats that as "0" for counter() or
// as no output for counters().
func (c *CountersAttachmentContext) GetCounterValues(node *html.Node, name string, onlyLast bool) []int {
	stack := c.table[name]
	if stack == nil || len(*stack) == 0 {
		return nil
	}
	// Walk the stack in reverse, dropping entries that are not in
	// scope for the entering element (Blink: same staleness test as
	// RemoveStaleCounters, but read-only).
	var values []int
	// Collected innermost-first; reverse before returning if !onlyLast.
	for i := len(*stack) - 1; i >= 0; i-- {
		entry := (*stack)[i]
		if entry == nil { // containment boundary
			break
		}
		if !isAncestorOrSelf(parentNode(entry.Origin), node) {
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

// parseCounterDirectives reads counter-reset and counter-increment
// off a Style and returns one CounterDirective per counter NAME,
// matching Blink's CounterDirectiveMap shape. Multiple directives on
// the same name OR their type bits together and combine values per
// Blink's CounterDirectives::CombinedValue (reset_value +
// increment_value, saturated to int32). Phase 2 will fold counter-set
// into the same map and apply set-overrides-reset+increment
// precedence. The reversed(name) form on counter-reset is parsed and
// the Reversed flag preserved, but it has no semantic effect until
// Phase 4.
func parseCounterDirectives(style *Style) []CounterDirective {
	// Use a name-keyed accumulator so two declarations on the same
	// counter (e.g. counter-reset:c 98 + counter-increment:c 1)
	// merge into one CounterDirective with type=reset|increment and
	// value=99 — matching Blink CombinedValue exactly.
	type accum struct {
		hasReset, hasIncrement bool
		resetValue             int
		incrementValue         int
		reversed               bool
		order                  int
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
		// Blink CombinedValue for reset+increment is the saturated
		// sum; for reset-only it's reset_value; for increment-only
		// it's increment_value.
		var value int
		switch {
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

// parentNode returns the parent of a (possibly synthetic pseudo-element)
// node. Synthetic ::before/::after/::marker nodes have Parent set to
// their originating real element, so this works for them too.
func parentNode(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	return n.Parent
}

// isAncestorOrSelf reports whether ancestor is the entering node or
// any ancestor of it via Parent links. Blink uses
// LayoutTreeBuilderTraversal::ParentElement which behaves the same way
// for layout-tree traversal; in louis14 the DOM parent chain matches
// the layout-tree-building order, so the simple chain walk suffices.
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

// isSameOrPreviousSibling reports whether a is the same node as b OR a
// previous sibling of b under the same parent. Used by CreateCounter
// to collapse stacked resets on the same element or sibling chain
// (Blink: "Remove innermost counter with same or previous sibling
// originating element").
func isSameOrPreviousSibling(a, b *html.Node) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	if a.Parent == nil || a.Parent != b.Parent {
		return false
	}
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
