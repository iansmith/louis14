package layout

import (
	"strings"
	"unicode/utf8"

	xbidi "golang.org/x/text/unicode/bidi"
)

// textHasBidiControls returns true if the text contains any Unicode bidi
// formatting characters (LRE, RLE, LRO, RLO, PDF, LRI, RLI, FSI, PDI).
func textHasBidiControls(text string) bool {
	for _, r := range text {
		if isBidiControlChar(r) {
			return true
		}
	}
	return false
}

// ResolveBidiLevelsSimple assigns bidi levels using the Go bidi package's
// run directions. This is the simple path for text without explicit bidi
// control characters — it only distinguishes level 0 (LTR) and level 1 (RTL).
func ResolveBidiLevelsSimple(itemsData *InlineItemsData, baseDir Direction) {
	if len(itemsData.TextContent) == 0 {
		return
	}

	defaultDir := xbidi.LeftToRight
	if baseDir == DirectionRTL {
		defaultDir = xbidi.RightToLeft
	}

	var para xbidi.Paragraph
	if _, err := para.SetString(itemsData.TextContent, xbidi.DefaultDirection(defaultDir)); err != nil {
		return
	}
	ordering, err := para.Order()
	if err != nil {
		return
	}

	textRunes := []rune(itemsData.TextContent)
	levels := make([]int, len(textRunes))
	for i := 0; i < ordering.NumRuns(); i++ {
		run := ordering.Run(i)
		start, end := run.Pos()
		lvl := 0
		if run.Direction() == xbidi.RightToLeft {
			lvl = 1
		}
		for j := start; j <= end && j < len(levels); j++ {
			levels[j] = lvl
		}
	}

	runeAtByte := make([]int, len(itemsData.TextContent)+1)
	ri := 0
	for bi := range itemsData.TextContent {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(itemsData.TextContent)] = ri

	for _, item := range itemsData.Items {
		offset := item.StartOffset
		if offset >= len(itemsData.TextContent) {
			if len(levels) > 0 {
				item.BidiLevel = levels[len(levels)-1]
			}
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < len(levels) {
			item.BidiLevel = levels[runeIdx]
		}
	}
}

// ResolveBidiLevels computes per-rune resolved bidi levels using a pure-Go
// implementation of UAX#9 (X1-X8, W1-W7, N1-N2, I1-I2). This replaces
// the Go bidi package's level resolution which has issues with neutral
// character resolution.
//
// After this call, itemsData.RuneLevels is populated with per-rune resolved
// levels and each item's BidiLevel is set from the level at its start offset.
func ResolveBidiLevels(itemsData *InlineItemsData, baseDir Direction) {
	if len(itemsData.TextContent) == 0 {
		return
	}

	baseLevel := 0
	if baseDir == DirectionRTL {
		baseLevel = 1
	}

	runes := []rune(itemsData.TextContent)
	nRunes := len(runes)

	// Step 1: Get bidi class for each rune.
	classes := make([]xbidi.Class, nRunes)
	for i, r := range runes {
		props, _ := xbidi.LookupRune(r)
		classes[i] = props.Class()
	}

	// Step 2: Compute explicit embedding levels and apply overrides (UAX#9 X1-X8).
	// computeEmbeddingLevels also applies X6: when an override is active
	// (LRO/RLO), character types are forced to L/R.
	embLevels := computeEmbeddingLevels(runes, baseLevel, classes)

	// Step 3: Resolve weak types (W1-W7) and neutral types (N1-N2) within
	// each embedding level context, then apply implicit levels (I1-I2).
	levels := resolveAllLevels(classes, embLevels, baseLevel)

	// Step 4: Apply UAX#9 rule L1 — reset trailing whitespace and isolate
	// formatting characters at the end of the paragraph to the paragraph
	// embedding level. This prevents trailing spaces from participating in
	// mid-level L2 reversals.
	applyL1(levels, runes, baseLevel)

	itemsData.RuneLevels = levels

	// Build byte→rune index map for assigning levels to items.
	runeAtByte := make([]int, len(itemsData.TextContent)+1)
	ri := 0
	for bi := range itemsData.TextContent {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(itemsData.TextContent)] = ri

	// Assign bidi levels to each InlineItem.
	for _, item := range itemsData.Items {
		offset := item.StartOffset
		if offset >= len(itemsData.TextContent) {
			// Items past end of text get the paragraph embedding level,
			// matching ICU's ubidi_getLevelAt() behavior.
			item.BidiLevel = baseLevel
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < nRunes {
			item.BidiLevel = levels[runeIdx]
		}
	}
}

// resolveAllLevels applies W1-W7, N1-N2, and I1-I2 to compute final per-rune
// levels. Characters at the same embedding level are processed together as
// an isolating run sequence.
func resolveAllLevels(classes []xbidi.Class, embLevels []int, paraLevel int) []int {
	n := len(classes)
	if n == 0 {
		return nil
	}

	// Work on a copy of classes so we can mutate during resolution.
	types := make([]xbidi.Class, n)
	copy(types, classes)

	// Process each level run independently.
	// Find contiguous runs at the same embedding level.
	i := 0
	for i < n {
		lvl := embLevels[i]
		start := i
		for i < n && embLevels[i] == lvl {
			i++
		}
		end := i

		// Determine sos and eos directions for this run.
		// sos = typeForLevel(max(level_of_preceding_char, level_of_run))
		// eos = typeForLevel(max(level_of_following_char, level_of_run))
		prevLevel := paraLevel
		if start > 0 {
			prevLevel = embLevels[start-1]
		}
		succLevel := paraLevel
		if end < n {
			succLevel = embLevels[end]
		}
		sos := typeForLvl(maxInt(prevLevel, lvl))
		eos := typeForLvl(maxInt(succLevel, lvl))

		resolveWeakTypes(types[start:end], sos)
		resolveNeutralTypes(types[start:end], lvl, sos, eos)
	}

	// Apply implicit levels (I1, I2).
	levels := make([]int, n)
	for j := range levels {
		emb := embLevels[j]
		t := types[j]
		if emb%2 == 0 { // even (LTR) embedding
			switch t {
			case xbidi.R:
				levels[j] = emb + 1
			case xbidi.AN, xbidi.EN:
				levels[j] = emb + 2
			default: // L
				levels[j] = emb
			}
		} else { // odd (RTL) embedding
			if t == xbidi.R {
				levels[j] = emb
			} else { // L, AN, EN
				levels[j] = emb + 1
			}
		}
	}

	return levels
}

// resolveWeakTypes applies UAX#9 rules W1-W7 to a run of characters.
func resolveWeakTypes(types []xbidi.Class, sos xbidi.Class) {
	n := len(types)

	// W1: NSM → type of preceding character (or sos).
	prev := sos
	for i, t := range types {
		if t == xbidi.NSM {
			types[i] = prev
		}
		prev = types[i]
	}

	// W2: EN → AN when preceded by AL.
	lastStrong := sos
	for i, t := range types {
		if t == xbidi.EN && lastStrong == xbidi.AL {
			types[i] = xbidi.AN
		}
		if t == xbidi.L || t == xbidi.R || t == xbidi.AL {
			lastStrong = t
		}
	}

	// W3: AL → R.
	for i, t := range types {
		if t == xbidi.AL {
			types[i] = xbidi.R
		}
	}

	// W4: ES between two ENs → EN; CS between matching number types → that type.
	for i := 1; i < n-1; i++ {
		t := types[i]
		prev := types[i-1]
		next := types[i+1]
		if t == xbidi.ES && prev == xbidi.EN && next == xbidi.EN {
			types[i] = xbidi.EN
		}
		if t == xbidi.CS {
			if prev == xbidi.EN && next == xbidi.EN {
				types[i] = xbidi.EN
			} else if prev == xbidi.AN && next == xbidi.AN {
				types[i] = xbidi.AN
			}
		}
	}

	// W5: ET adjacent to EN → EN.
	// First pass: propagate EN forward through ETs.
	for i := 0; i < n; i++ {
		if types[i] == xbidi.ET {
			// Check if preceded by EN
			if i > 0 && types[i-1] == xbidi.EN {
				types[i] = xbidi.EN
			}
		}
	}
	// Second pass: propagate EN backward through ETs.
	for i := n - 1; i >= 0; i-- {
		if types[i] == xbidi.ET {
			if i < n-1 && types[i+1] == xbidi.EN {
				types[i] = xbidi.EN
			}
		}
	}

	// W6: Remaining ES, ET, CS → ON.
	for i, t := range types {
		if t == xbidi.ES || t == xbidi.ET || t == xbidi.CS {
			types[i] = xbidi.ON
		}
	}

	// W7: EN → L when the last strong type (from sos) is L.
	lastStrong = sos
	for i, t := range types {
		if t == xbidi.EN && lastStrong == xbidi.L {
			types[i] = xbidi.L
		}
		if t == xbidi.L || t == xbidi.R {
			lastStrong = t
		}
	}
}

// resolveNeutralTypes applies UAX#9 rules N1-N2 to a run of characters.
func resolveNeutralTypes(types []xbidi.Class, level int, sos, eos xbidi.Class) {
	n := len(types)

	isNeutral := func(c xbidi.Class) bool {
		switch c {
		case xbidi.WS, xbidi.ON, xbidi.B, xbidi.S,
			xbidi.RLI, xbidi.LRI, xbidi.FSI, xbidi.PDI, xbidi.BN:
			return true
		}
		return false
	}

	// Treat EN and AN as R for purposes of N1/N2 surrounding context.
	strongDir := func(c xbidi.Class) xbidi.Class {
		if c == xbidi.EN || c == xbidi.AN {
			return xbidi.R
		}
		return c
	}

	embDir := typeForLvl(level)

	for i := 0; i < n; {
		if !isNeutral(types[i]) {
			i++
			continue
		}

		// Start of neutral run.
		start := i
		for i < n && isNeutral(types[i]) {
			i++
		}
		end := i

		// N1/N2: determine lead and trail strong types.
		var lead, trail xbidi.Class

		// Look backward for preceding strong type.
		lead = sos
		for j := start - 1; j >= 0; j-- {
			t := types[j]
			if t == xbidi.L || t == xbidi.R || t == xbidi.EN || t == xbidi.AN {
				lead = strongDir(t)
				break
			}
		}

		// Look forward for following strong type.
		trail = eos
		for j := end; j < n; j++ {
			t := types[j]
			if t == xbidi.L || t == xbidi.R || t == xbidi.EN || t == xbidi.AN {
				trail = strongDir(t)
				break
			}
		}

		var resolved xbidi.Class
		if lead == trail {
			// N1: same direction on both sides.
			resolved = lead
		} else {
			// N2: different directions → embedding direction.
			resolved = embDir
		}

		for j := start; j < end; j++ {
			types[j] = resolved
		}
	}
}

// typeForLvl returns L for even levels, R for odd levels.
func typeForLvl(level int) xbidi.Class {
	if level%2 == 0 {
		return xbidi.L
	}
	return xbidi.R
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// applyL1 implements UAX#9 Rule L1: reset embedding levels of trailing
// whitespace (WS, FSI, LRI, RLI, PDI) and segment/paragraph separators
// to the paragraph embedding level. This is critical for correct L2
// reordering — without it, trailing spaces in RTL context participate in
// L2 reversals, changing the visual order of preceding content.
func applyL1(levels []int, runes []rune, paraLevel int) {
	n := len(runes)

	isL1Whitespace := func(r rune) bool {
		props, _ := xbidi.LookupRune(r)
		cls := props.Class()
		switch cls {
		case xbidi.WS, xbidi.FSI, xbidi.LRI, xbidi.RLI, xbidi.PDI, xbidi.BN:
			return true
		// Also include bidi control characters that are treated like WS for L1.
		case xbidi.LRE, xbidi.RLE, xbidi.LRO, xbidi.RLO, xbidi.PDF:
			return true
		}
		return false
	}

	// L1 clauses 1-3: Reset levels for B, S types and preceding whitespace.
	for i := 0; i < n; i++ {
		props, _ := xbidi.LookupRune(runes[i])
		cls := props.Class()
		if cls == xbidi.B || cls == xbidi.S {
			levels[i] = paraLevel
			// Reset preceding whitespace.
			for j := i - 1; j >= 0; j-- {
				if isL1Whitespace(runes[j]) {
					levels[j] = paraLevel
				} else {
					break
				}
			}
		}
	}

	// L1 clause 4: Reset trailing whitespace at end of paragraph.
	for i := n - 1; i >= 0; i-- {
		if isL1Whitespace(runes[i]) {
			levels[i] = paraLevel
		} else {
			break
		}
	}
}

// computeEmbeddingLevels tracks explicit bidi control characters in the text
// to compute per-rune embedding levels per UAX#9 rules X1-X8, X5a-X5c, X6a.
// It also applies rule X6: when an override is active, non-control characters
// have their bidi class forced to L (for LRO) or R (for RLO).
// The classes array is modified in-place for overridden characters.
func computeEmbeddingLevels(runes []rune, baseLevel int, classes []xbidi.Class) []int {
	levels := make([]int, len(runes))

	const maxDepth = 125

	// Override status: 0 = neutral, 1 = LTR (force L), 2 = RTL (force R)
	type dirEntry struct {
		level    int
		override int // 0=neutral, 1=LTR, 2=RTL
		isolate  bool
	}

	stack := make([]dirEntry, 1, 64)
	stack[0] = dirEntry{level: baseLevel}

	for i, r := range runes {
		cur := stack[len(stack)-1]

		switch r {
		case '\u202A': // LRE — next even level, neutral override
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 0})
			}

		case '\u202B': // RLE — next odd level, neutral override
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 0})
			}

		case '\u202D': // LRO — next even level, LTR override
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 1})
			}

		case '\u202E': // RLO — next odd level, RTL override
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 2})
			}

		case '\u202C': // PDF — pop one non-isolate entry
			levels[i] = cur.level
			if len(stack) > 1 && !cur.isolate {
				stack = stack[:len(stack)-1]
			}

		case '\u2066': // LRI — next even level, isolate
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2067': // RLI — next odd level, isolate
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2068': // FSI
			levels[i] = cur.level
			dir := determineFSIDirection(runes[i+1:])
			var newLevel int
			if dir == 1 {
				newLevel = (cur.level + 1) | 1
			} else {
				newLevel = (cur.level | 1) + 1
			}
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2069': // PDI
			for len(stack) > 1 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.isolate {
					break
				}
			}
			levels[i] = stack[len(stack)-1].level

		default:
			levels[i] = cur.level
			// X6: apply override if active.
			if cur.override == 1 {
				classes[i] = xbidi.L
			} else if cur.override == 2 {
				classes[i] = xbidi.R
			}
		}
	}

	return levels
}

// determineFSIDirection implements UAX#9 P2/P3 for FSI.
func determineFSIDirection(runes []rune) int {
	isolateDepth := 0
	for _, r := range runes {
		switch r {
		case '\u2066', '\u2067', '\u2068':
			isolateDepth++
		case '\u2069':
			if isolateDepth > 0 {
				isolateDepth--
			} else {
				return 0
			}
		default:
			if isolateDepth == 0 {
				props, _ := xbidi.LookupRune(r)
				cls := props.Class()
				if cls == xbidi.L {
					return 0
				}
				if cls == xbidi.R || cls == xbidi.AL {
					return 1
				}
			}
		}
	}
	return 0
}

// isBidiControlChar returns true for Unicode bidi formatting characters.
func isBidiControlChar(r rune) bool {
	return (r >= '\u202A' && r <= '\u202E') || // LRE, RLE, PDF, LRO, RLO
		(r >= '\u2066' && r <= '\u2069') // LRI, RLI, FSI, PDI
}

// StripBidiControls removes bidi control characters from TextContent,
// remaps all item byte offsets, and strips RuneLevels entries.
func StripBidiControls(itemsData *InlineItemsData) {
	text := itemsData.TextContent
	if len(text) == 0 {
		return
	}

	hasBidiControls := false
	for _, r := range text {
		if isBidiControlChar(r) {
			hasBidiControls = true
			break
		}
	}
	if !hasBidiControls {
		return
	}

	offsetMap := make([]int, len(text)+1)
	var stripped strings.Builder
	stripped.Grow(len(text))
	var strippedLevels []int
	if itemsData.RuneLevels != nil {
		strippedLevels = make([]int, 0, len(itemsData.RuneLevels))
	}
	newOff := 0
	runeIdx := 0

	for oldOff, r := range text {
		offsetMap[oldOff] = newOff
		if !isBidiControlChar(r) {
			stripped.WriteRune(r)
			newOff += utf8.RuneLen(r)
			if itemsData.RuneLevels != nil && runeIdx < len(itemsData.RuneLevels) {
				strippedLevels = append(strippedLevels, itemsData.RuneLevels[runeIdx])
			}
		}
		runeIdx++
	}
	offsetMap[len(text)] = newOff

	itemsData.TextContent = stripped.String()
	itemsData.RuneLevels = strippedLevels

	for _, item := range itemsData.Items {
		item.StartOffset = offsetMap[item.StartOffset]
		item.EndOffset = offsetMap[item.EndOffset]
	}
}

// SplitItemsAtLevelBoundaries splits text items where bidi levels change.
func SplitItemsAtLevelBoundaries(itemsData *InlineItemsData) {
	if len(itemsData.RuneLevels) == 0 {
		return
	}

	text := itemsData.TextContent

	nRunes := utf8.RuneCountInString(text)
	runeAtByte := make([]int, len(text)+1)
	byteAtRune := make([]int, nRunes+1)
	ri := 0
	for bi := range text {
		runeAtByte[bi] = ri
		byteAtRune[ri] = bi
		ri++
	}
	runeAtByte[len(text)] = ri
	byteAtRune[ri] = len(text)

	var newItems []*InlineItem

	for _, item := range itemsData.Items {
		if item.Type != InlineItemText {
			// Non-text items: update level from RuneLevels if within text,
			// otherwise keep the level already assigned by ResolveBidiLevels
			// (which uses the paragraph embedding level for end-of-text items).
			if item.StartOffset < len(text) {
				rIdx := runeAtByte[item.StartOffset]
				if rIdx < len(itemsData.RuneLevels) {
					item.BidiLevel = itemsData.RuneLevels[rIdx]
				}
			}
			// Items past end of text keep their BidiLevel from ResolveBidiLevels.
			newItems = append(newItems, item)
			continue
		}

		startRune := runeAtByte[item.StartOffset]
		endRune := runeAtByte[item.EndOffset]

		if startRune >= endRune || startRune >= len(itemsData.RuneLevels) {
			item.BidiLevel = 0
			if startRune < len(itemsData.RuneLevels) {
				item.BidiLevel = itemsData.RuneLevels[startRune]
			}
			newItems = append(newItems, item)
			continue
		}

		currentLevel := itemsData.RuneLevels[startRune]
		runStart := startRune

		for r := startRune + 1; r < endRune && r < len(itemsData.RuneLevels); r++ {
			lvl := itemsData.RuneLevels[r]
			if lvl != currentLevel {
				newItems = append(newItems, &InlineItem{
					Type:            InlineItemText,
					StartOffset:     byteAtRune[runStart],
					EndOffset:       byteAtRune[r],
					Node:            item.Node,
					Style:           item.Style,
					BidiLevel:       currentLevel,
					IsFirstFragment: item.IsFirstFragment,
					IsLastFragment:  item.IsLastFragment,
				})
				currentLevel = lvl
				runStart = r
			}
		}

		newItems = append(newItems, &InlineItem{
			Type:            InlineItemText,
			StartOffset:     byteAtRune[runStart],
			EndOffset:       byteAtRune[endRune],
			Node:            item.Node,
			Style:           item.Style,
			BidiLevel:       currentLevel,
			IsFirstFragment: item.IsFirstFragment,
			IsLastFragment:  item.IsLastFragment,
		})
	}

	itemsData.Items = newItems
}

// ReorderLineVisual reorders a line's InlineItemResults from logical order
// to visual order using the UAX#9 L2 algorithm.
func ReorderLineVisual(results []InlineItemResult) {
	if len(results) == 0 {
		return
	}

	maxLevel := 0
	minOdd := 256
	for _, r := range results {
		lvl := r.Item.BidiLevel
		if lvl > maxLevel {
			maxLevel = lvl
		}
		if lvl%2 == 1 && lvl < minOdd {
			minOdd = lvl
		}
	}

	if maxLevel == 0 || minOdd == 256 {
		return
	}

	for lvl := maxLevel; lvl >= minOdd; lvl-- {
		for i := 0; i < len(results); {
			if results[i].Item.BidiLevel >= lvl {
				j := i + 1
				for j < len(results) && results[j].Item.BidiLevel >= lvl {
					j++
				}
				for a, b := i, j-1; a < b; a, b = a+1, b-1 {
					results[a], results[b] = results[b], results[a]
				}
				i = j
			} else {
				i++
			}
		}
	}
}
