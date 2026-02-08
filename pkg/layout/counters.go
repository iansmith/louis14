package layout

import (
	"strconv"
	"strings"

	"louis14/pkg/html"
)

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
