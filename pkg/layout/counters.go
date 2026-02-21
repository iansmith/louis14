package layout

import (
	"sort"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// customCounterStyles is a package-level registry populated from parsed stylesheets.
// It maps counter style names to their rules.
var customCounterStyles map[string]css.CounterStyleRule

// RegisterCounterStyles populates the global counter style registry from all parsed stylesheets.
// Call this after parsing stylesheets and before layout.
func RegisterCounterStyles(stylesheets []*css.Stylesheet) {
	customCounterStyles = make(map[string]css.CounterStyleRule)
	for _, ss := range stylesheets {
		for _, cs := range ss.CounterStyles {
			customCounterStyles[cs.Name] = cs
		}
	}
}

// GetCounterString converts a counter integer value to a string using the named list style.
// Custom @counter-style rules are checked first, then built-in styles.
func GetCounterString(value int, style string) string {
	if cs, ok := customCounterStyles[style]; ok {
		return applyCounterStyle(value, cs)
	}
	// Built-in styles
	switch style {
	case "decimal", "":
		return strconv.Itoa(value)
	case "disc":
		return "•"
	case "circle":
		return "○"
	case "square":
		return "■"
	case "lower-alpha", "lower-latin":
		return alphabeticCounter(value, "abcdefghijklmnopqrstuvwxyz")
	case "upper-alpha", "upper-latin":
		return alphabeticCounter(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	case "lower-roman":
		return romanCounter(value, false)
	case "upper-roman":
		return romanCounter(value, true)
	}
	return strconv.Itoa(value)
}

// applyCounterStyle applies a custom @counter-style rule to convert a counter value.
func applyCounterStyle(value int, cs css.CounterStyleRule) string {
	switch cs.System {
	case "cyclic":
		if len(cs.Symbols) == 0 {
			return strconv.Itoa(value)
		}
		n := len(cs.Symbols)
		idx := ((value - 1) % n + n) % n
		return cs.Prefix + cs.Symbols[idx] + cs.Suffix

	case "numeric":
		if len(cs.Symbols) == 0 {
			return strconv.Itoa(value)
		}
		base := len(cs.Symbols)
		if value == 0 {
			return cs.Prefix + cs.Symbols[0] + cs.Suffix
		}
		result := ""
		v := value
		for v > 0 {
			result = cs.Symbols[v%base] + result
			v /= base
		}
		return cs.Prefix + result + cs.Suffix

	case "alphabetic":
		if len(cs.Symbols) == 0 {
			return strconv.Itoa(value)
		}
		base := len(cs.Symbols)
		result := ""
		v := value
		for v > 0 {
			v-- // 1-based: subtract 1 before modding
			result = cs.Symbols[v%base] + result
			v /= base
		}
		return cs.Prefix + result + cs.Suffix

	case "fixed":
		if value >= 1 && value <= len(cs.Symbols) {
			return cs.Prefix + cs.Symbols[value-1] + cs.Suffix
		}
		// Out of range: fall back to decimal
		return strconv.Itoa(value)

	case "additive":
		if len(cs.AdditiveSymbols) == 0 {
			return strconv.Itoa(value)
		}
		// Sort additive symbols by value descending
		sorted := make([]css.AdditiveSymbol, len(cs.AdditiveSymbols))
		copy(sorted, cs.AdditiveSymbols)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value > sorted[j].Value
		})
		result := ""
		remaining := value
		for _, as := range sorted {
			if as.Value <= 0 {
				break
			}
			for remaining >= as.Value {
				result += as.Symbol
				remaining -= as.Value
			}
		}
		if remaining > 0 {
			// Could not represent — fall back to decimal
			return strconv.Itoa(value)
		}
		return cs.Prefix + result + cs.Suffix

	default: // "symbolic"
		if len(cs.Symbols) == 0 {
			return strconv.Itoa(value)
		}
		n := len(cs.Symbols)
		idx := ((value - 1) % n + n) % n
		count := ((value - 1) / n) + 1
		sym := cs.Symbols[idx]
		result := strings.Repeat(sym, count)
		return cs.Prefix + result + cs.Suffix
	}
}

// alphabeticCounter converts value to an alphabetic counter string (a, b, ..., z, aa, ab, ...)
func alphabeticCounter(value int, alphabet string) string {
	base := len(alphabet)
	result := ""
	for value > 0 {
		value-- // 1-based
		result = string(alphabet[value%base]) + result
		value /= base
	}
	return result
}

// romanCounter converts value to a Roman numeral string.
func romanCounter(value int, upper bool) string {
	type romanVal struct {
		val int
		sym string
	}
	lower := []romanVal{
		{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"},
		{100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"},
		{10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"},
	}
	result := ""
	for _, rv := range lower {
		for value >= rv.val {
			result += rv.sym
			value -= rv.val
		}
	}
	if upper {
		return strings.ToUpper(result)
	}
	return result
}

// CSS Counter support functions

// counterReset resets a counter to the specified value (default 0)
// This creates a new scope for the counter.
func (le *LayoutEngine) counterReset(name string, value int) {
	if le.counters == nil {
		le.counters = make(map[string][]int)
	}
	// Push a new value onto the counter's stack
	le.counters[name] = append(le.counters[name], value)
}

// counterIncrement increments a counter by the specified value (default 1)
func (le *LayoutEngine) counterIncrement(name string, value int) {
	if le.counters == nil {
		return
	}
	stack := le.counters[name]
	if len(stack) == 0 {
		// Counter wasn't reset - implicitly create it at 0
		le.counters[name] = []int{value}
	} else {
		// Increment the top of the stack
		le.counters[name][len(stack)-1] += value
	}
}

// counterValue returns the current value of a counter
func (le *LayoutEngine) counterValue(name string) int {
	if le.counters == nil {
		return 0
	}
	stack := le.counters[name]
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

// counterPop removes the topmost scope of a counter (called when leaving an element that reset it)
func (le *LayoutEngine) counterPop(name string) {
	if le.counters == nil {
		return
	}
	stack := le.counters[name]
	if len(stack) > 0 {
		le.counters[name] = stack[:len(stack)-1]
	}
}

// saveCounterState returns a deep copy of the current counter state
func (le *LayoutEngine) saveCounterState() map[string][]int {
	if le.counters == nil {
		return nil
	}
	saved := make(map[string][]int, len(le.counters))
	for k, v := range le.counters {
		cp := make([]int, len(v))
		copy(cp, v)
		saved[k] = cp
	}
	return saved
}

// restoreCounterState restores a previously saved counter state
func (le *LayoutEngine) restoreCounterState(saved map[string][]int) {
	le.counters = saved
}

// parseCounterReset parses the counter-reset property value
// Format: "name [value] [name2 [value2] ...]" or "none"
func parseCounterReset(value string) map[string]int {
	result := make(map[string]int)
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return result
	}

	parts := strings.Fields(value)
	i := 0
	for i < len(parts) {
		name := parts[i]
		resetValue := 0
		if i+1 < len(parts) {
			// Check if next part is a number
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				resetValue = v
				i++
			}
		}
		result[name] = resetValue
		i++
	}
	return result
}

// parseCounterIncrement parses the counter-increment property value
// Format: "name [value] [name2 [value2] ...]" or "none"
func parseCounterIncrement(value string) map[string]int {
	result := make(map[string]int)
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return result
	}

	parts := strings.Fields(value)
	i := 0
	for i < len(parts) {
		name := parts[i]
		incValue := 1 // Default increment is 1
		if i+1 < len(parts) {
			// Check if next part is a number
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				incValue = v
				i++
			}
		}
		result[name] = incValue
		i++
	}
	return result
}

// getListItemNumber returns the item number for an <li> element
func (le *LayoutEngine) getListItemNumber(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}

	itemNumber := 1
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			break
		}
		if sibling.Type == html.ElementNode && sibling.TagName == "li" {
			itemNumber++
		}
	}

	return itemNumber
}
