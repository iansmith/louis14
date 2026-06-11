package css

import (
	"fmt"
	"sort"
	"strings"
)

// ApplyCounterStyle converts a counter value using a custom @counter-style
// rule. Mirrors Blink CounterStyle::GenerateRepresentation per system
// (counter_style.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Lives in
// pkg/css so layout-time ::marker content resolution and the renderer share
// one implementation.
func ApplyCounterStyle(value int, cs CounterStyleRule, allStyles map[string]CounterStyleRule) string {
	switch cs.System {
	case "cyclic":
		if len(cs.Symbols) == 0 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		n := len(cs.Symbols)
		idx := ((value-1)%n + n) % n
		return cs.Prefix + cs.Symbols[idx] + cs.Suffix

	case "numeric":
		if len(cs.Symbols) < 2 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		base := len(cs.Symbols)
		if value == 0 {
			return cs.Prefix + cs.Symbols[0] + cs.Suffix
		}
		negative := value < 0
		if negative {
			value = -value
		}
		result := ""
		v := value
		for v > 0 {
			result = cs.Symbols[v%base] + result
			v /= base
		}
		if negative {
			result = "-" + result
		}
		return cs.Prefix + result + cs.Suffix

	case "alphabetic":
		if len(cs.Symbols) < 2 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		if value <= 0 {
			// Alphabetic system not defined for zero or negative values.
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
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

	case "symbolic":
		if len(cs.Symbols) == 0 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		if value <= 0 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		n := len(cs.Symbols)
		idx := ((value-1)%n + n) % n
		count := ((value - 1) / n) + 1
		sym := cs.Symbols[idx]
		result := strings.Repeat(sym, count)
		return cs.Prefix + result + cs.Suffix

	case "fixed":
		if value >= 1 && value <= len(cs.Symbols) {
			return cs.Prefix + cs.Symbols[value-1] + cs.Suffix
		}
		// Out of range: use fallback
		return fallbackCounterStyle(value, cs.Fallback, allStyles)

	case "additive":
		if len(cs.AdditiveSymbols) == 0 {
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		// Sort additive symbols by value descending
		sorted := make([]AdditiveSymbol, len(cs.AdditiveSymbols))
		copy(sorted, cs.AdditiveSymbols)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value > sorted[j].Value
		})
		if value == 0 {
			// Special case: find symbol with value 0
			for _, as := range sorted {
				if as.Value == 0 {
					return cs.Prefix + as.Symbol + cs.Suffix
				}
			}
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
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
			// Could not represent — use fallback
			return fallbackCounterStyle(value, cs.Fallback, allStyles)
		}
		return cs.Prefix + result + cs.Suffix

	default:
		// Unknown system — use fallback
		return fallbackCounterStyle(value, cs.Fallback, allStyles)
	}
}

// fallbackCounterStyle applies the fallback counter style, defaulting to decimal.
func fallbackCounterStyle(value int, fallback string, allStyles map[string]CounterStyleRule) string {
	if fallback != "" && allStyles != nil {
		if cs, ok := allStyles[fallback]; ok {
			return ApplyCounterStyle(value, cs, allStyles)
		}
		// Check if fallback is a built-in style
		switch fallback {
		case "decimal":
			return fmt.Sprintf("%d", value) + DefaultCounterStyleSuffix
		case "lower-alpha", "lower-latin":
			return ToAlpha(value) + DefaultCounterStyleSuffix
		case "upper-alpha", "upper-latin":
			return strings.ToUpper(ToAlpha(value)) + DefaultCounterStyleSuffix
		case "lower-roman":
			return strings.ToLower(ToRoman(value)) + DefaultCounterStyleSuffix
		case "upper-roman":
			return ToRoman(value) + DefaultCounterStyleSuffix
		}
	}
	return fmt.Sprintf("%d", value) + DefaultCounterStyleSuffix
}
